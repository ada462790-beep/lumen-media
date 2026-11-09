// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package relay_test

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"lumen/internal/cert"
	"lumen/internal/relay"
)

// still hält die Testausgabe sauber: der Vermittler protokolliert sonst
// jedes Verbinden und Trennen.
func still() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

// ---------------------------------------------------------------- Aufbau

// aufbau stellt die ganze Kette hin, wie sie im Betrieb steht:
//
//	Besucher ──► Vermittler ◄── Heimseite ──► „Lumen"
//
// „Lumen" ist hier ein kleiner HTTPS-Server, der zurückgibt, was man ihm
// mitgibt. Entscheidend ist, dass er selbst verschlüsselt — genau deshalb
// bekommt der Vermittler nichts zu lesen.
type aufbau struct {
	BesucherAddr string
	TunnelAddr   string
	Mitschnitt   *mitschnitt
	Vermittler   *relay.Vermittler
	LumenZert    *x509.Certificate
	Tunnel       *tunnelGriff
	beenden      func()
}

const geheimnis = "ein-ausreichend-langes-geheimnis"

func stelleAuf(t *testing.T, antwort http.Handler) *aufbau {
	t.Helper()

	ordner := t.TempDir()
	zert, _, err := cert.Anlegen(ordner+"/c.pem", ordner+"/k.pem", []string{"localhost", "127.0.0.1"})
	if err != nil {
		t.Fatalf("zertifikat: %v", err)
	}
	x, err := x509.ParseCertificate(zert.Certificate[0])
	if err != nil {
		t.Fatalf("zertifikat lesen: %v", err)
	}

	// 1. „Lumen" — spricht TLS, wie der echte Server es durch den
	//    Vermittler hindurch täte.
	lumenL, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	lumen := &http.Server{
		Handler:   antwort,
		TLSConfig: &tls.Config{Certificates: []tls.Certificate{zert}, MinVersion: tls.VersionTLS12},
	}
	go func() { _ = lumen.ServeTLS(lumenL, "", "") }()

	// 2. Vermittler — Tunnelport mit TLS, Besucherport roh.
	v, err := relay.NeuerVermittler(geheimnis, still())
	if err != nil {
		t.Fatal(err)
	}
	rohTunnel, err := tls.Listen("tcp", "127.0.0.1:0", relay.TLSAufbau(zert))
	if err != nil {
		t.Fatal(err)
	}
	tunnelL := &tunnelGriff{Listener: rohTunnel}
	rohL, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	mit := &mitschnitt{Listener: rohL}

	stop := make(chan struct{})
	go v.Lauschen(tunnelL, mit, stop)

	// 3. Heimseite — wählt sich hinaus, gepinnt auf dasselbe Zertifikat.
	ctx, aus := context.WithCancel(context.Background())
	h := &relay.Heimseite{
		Vermittler:    tunnelL.Addr().String(),
		Geheimnis:     geheimnis,
		Ziel:          lumenL.Addr().String(),
		Fingerabdruck: relay.Fingerabdruck(x),
		Log:           still(),
	}
	go h.Laufen(ctx)

	wartenBisVerbunden(t, v)

	return &aufbau{
		BesucherAddr: rohL.Addr().String(),
		TunnelAddr:   rohTunnel.Addr().String(),
		Mitschnitt:   mit,
		Vermittler:   v,
		LumenZert:    x,
		Tunnel:       tunnelL,
		beenden: func() {
			aus()
			close(stop)
			_ = lumen.Close()
		},
	}
}

func wartenBisVerbunden(t *testing.T, v *relay.Vermittler) {
	t.Helper()
	for i := 0; i < 200; i++ {
		if v.Stand().Verbunden {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("die Heimseite hat sich nicht beim Vermittler gemeldet")
}

// holen ruft etwas über den Vermittler ab — also genau den Weg, den auch
// ein Gerät von unterwegs nimmt.
func (a *aufbau) holen(t *testing.T, pfad string) (int, string) {
	t.Helper()
	wurzel := x509.NewCertPool()
	wurzel.AddCert(a.LumenZert)

	c := &http.Client{
		Timeout: 20 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{RootCAs: wurzel, ServerName: "localhost"},
		},
	}
	u := url.URL{Scheme: "https", Host: a.BesucherAddr, Path: pfad}
	res, err := c.Get(u.String())
	if err != nil {
		t.Fatalf("über den Vermittler holen: %v", err)
	}
	defer res.Body.Close()
	leib, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(leib)
}

// ---------------------------------------------------------------- Griff

// tunnelGriff merkt sich die angenommenen Tunnelverbindungen, damit der
// Test sie kappen kann — so, wie es ein Router tut, der eine stille
// Verbindung vergisst. Im Betrieb steht an dieser Stelle der blosse
// Listener; der Griff existiert nur hier.
type tunnelGriff struct {
	net.Listener
	mu    sync.Mutex
	offen []net.Conn
}

func (g *tunnelGriff) Accept() (net.Conn, error) {
	c, err := g.Listener.Accept()
	if err != nil {
		return nil, err
	}
	g.mu.Lock()
	g.offen = append(g.offen, c)
	g.mu.Unlock()
	return c, nil
}

func (g *tunnelGriff) Kappen() {
	g.mu.Lock()
	offen := g.offen
	g.offen = nil
	g.mu.Unlock()
	for _, c := range offen {
		_ = c.Close()
	}
}

// ---------------------------------------------------------------- Mitschnitt

// mitschnitt hört mit, was der Vermittler zu sehen bekommt. Nicht als
// Angriff, sondern als Beweis: was hier landet, ist alles, was er je
// erfährt.
type mitschnitt struct {
	net.Listener
	mu   sync.Mutex
	alle bytes.Buffer
}

func (m *mitschnitt) Accept() (net.Conn, error) {
	c, err := m.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &mitschnittConn{Conn: c, m: m}, nil
}

func (m *mitschnitt) Gesehen() []byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]byte(nil), m.alle.Bytes()...)
}

type mitschnittConn struct {
	net.Conn
	m *mitschnitt
}

func (c *mitschnittConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	c.schreibeMit(p[:n])
	return n, err
}

func (c *mitschnittConn) Write(p []byte) (int, error) {
	c.schreibeMit(p)
	return c.Conn.Write(p)
}

func (c *mitschnittConn) schreibeMit(p []byte) {
	c.m.mu.Lock()
	c.m.alle.Write(p)
	c.m.mu.Unlock()
}

func (c *mitschnittConn) CloseWrite() error {
	type halb interface{ CloseWrite() error }
	if h, ok := c.Conn.(halb); ok {
		return h.CloseWrite()
	}
	return c.Conn.Close()
}

// ---------------------------------------------------------------- Tests

// TestVonAussenErreichbar ist der Grundfall: ohne Portfreigabe kommt
// jemand von draussen an Lumen heran.
func TestVonAussenErreichbar(t *testing.T) {
	a := stelleAuf(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "hallo von %s", r.URL.Path)
	}))
	defer a.beenden()

	status, leib := a.holen(t, "/api/v1/hello")
	if status != 200 {
		t.Fatalf("Status %d, erwartet 200", status)
	}
	if leib != "hallo von /api/v1/hello" {
		t.Fatalf("Antwort kam verändert an: %q", leib)
	}
}

// TestVermittlerSiehtNichts ist der Test, auf den es ankommt.
//
// Wir schicken einen Titel, einen Namen und eine Kennung durch — alles,
// was ein Vermittler gerne hätte — und sehen danach nach, was bei ihm
// angekommen ist. Es darf nichts davon dabei sein.
func TestVermittlerSiehtNichts(t *testing.T) {
	const titel = "Der Pate 2 — Kapitel 7"
	const konto = "acc_geheim_123"

	a := stelleAuf(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Konto", konto)
		fmt.Fprintf(w, `{"title":%q,"accountId":%q}`, titel, konto)
	}))
	defer a.beenden()

	_, leib := a.holen(t, "/api/v1/items/it_4711")
	if !strings.Contains(leib, titel) {
		t.Fatalf("der Titel kam gar nicht an: %q", leib)
	}

	gesehen := a.Mitschnitt.Gesehen()
	if len(gesehen) == 0 {
		t.Fatal("der Mitschnitt ist leer — dann prüft dieser Test nichts")
	}

	// Alles, was über eine Wiedergabe etwas verraten würde.
	for _, verboten := range []string{
		titel, konto, "it_4711", "/api/v1/items", "X-Konto",
		"HTTP/1.1", "GET ", "application/json",
	} {
		if bytes.Contains(gesehen, []byte(verboten)) {
			t.Errorf("der Vermittler hat %q zu sehen bekommen — "+
				"dann läuft etwas unverschlüsselt durch ihn hindurch", verboten)
		}
	}
	if !t.Failed() {
		t.Logf("%d Bytes liefen durch den Vermittler, nichts davon lesbar", len(gesehen))
	}
}

// TestFalschesGeheimnisKommtNichtDurch: der Tunnel ist das Einzige, was
// zwischen dem Heimnetz und dem offenen Netz steht.
func TestFalschesGeheimnisKommtNichtDurch(t *testing.T) {
	ordner := t.TempDir()
	zert, _, err := cert.Anlegen(ordner+"/c.pem", ordner+"/k.pem", []string{"localhost"})
	if err != nil {
		t.Fatal(err)
	}
	x, _ := x509.ParseCertificate(zert.Certificate[0])

	v, _ := relay.NeuerVermittler(geheimnis, still())
	tunnelL, _ := tls.Listen("tcp", "127.0.0.1:0", relay.TLSAufbau(zert))
	rohL, _ := net.Listen("tcp", "127.0.0.1:0")
	stop := make(chan struct{})
	go v.Lauschen(tunnelL, rohL, stop)
	defer close(stop)

	wurzel := x509.NewCertPool()
	wurzel.AddCert(x)
	c, err := tls.Dial("tcp", tunnelL.Addr().String(),
		&tls.Config{RootCAs: wurzel, ServerName: "localhost", MinVersion: tls.VersionTLS12})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	r := bufio.NewReader(c)
	err = relay.AnmeldungSenden(c, r, relay.Anmeldung{
		Rolle: relay.Kontrolle, Geheimnis: "daneben",
	})
	if err == nil {
		t.Fatal("ein falsches Geheimnis wurde angenommen")
	}
	if v.Stand().Verbunden {
		t.Fatal("der Vermittler hält die Leitung trotzdem für verbunden")
	}
}

// TestOhneHeimserverKeineStille: wer anklopft, während zu Hause niemand
// verbunden ist, soll eine Antwort bekommen statt ins Leere zu laufen.
func TestOhneHeimserverKeineStille(t *testing.T) {
	v, _ := relay.NeuerVermittler(geheimnis, still())
	ordner := t.TempDir()
	zert, _, _ := cert.Anlegen(ordner+"/c.pem", ordner+"/k.pem", []string{"localhost"})
	tunnelL, _ := tls.Listen("tcp", "127.0.0.1:0", relay.TLSAufbau(zert))
	rohL, _ := net.Listen("tcp", "127.0.0.1:0")
	stop := make(chan struct{})
	go v.Lauschen(tunnelL, rohL, stop)
	defer close(stop)

	c, err := net.DialTimeout("tcp", rohL.Addr().String(), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_, _ = c.Write([]byte("GET / HTTP/1.1\r\nHost: x\r\n\r\n"))

	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	antwort, err := io.ReadAll(c)
	if err != nil && len(antwort) == 0 {
		t.Fatalf("keine Antwort: %v", err)
	}
	if !bytes.Contains(antwort, []byte("502")) {
		t.Fatalf("erwartet wurde eine 502, bekommen: %q", string(antwort))
	}
}

// TestFremdeKennungWirdAbgelehnt: ein Datenweg zu einer Kennung, die es
// nicht gibt, darf nicht angenommen werden. Sonst könnte jemand mit dem
// richtigen Geheimnis, aber geratener Kennung, in einer fremden Sitzung
// landen.
func TestFremdeKennungWirdAbgelehnt(t *testing.T) {
	a := stelleAuf(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer a.beenden()

	wurzel := x509.NewCertPool()
	wurzel.AddCert(a.LumenZert)
	c, err := tls.Dial("tcp", a.TunnelAddr, &tls.Config{
		RootCAs: wurzel, ServerName: "localhost", MinVersion: tls.VersionTLS12,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	err = relay.AnmeldungSenden(c, bufio.NewReader(c), relay.Anmeldung{
		Rolle: relay.Daten, Geheimnis: geheimnis, Kennung: "ausgedacht",
	})
	if err == nil {
		t.Fatal("eine erfundene Kennung wurde angenommen")
	}
	if !strings.Contains(err.Error(), "wartet niemand") {
		t.Fatalf("unerwarteter Grund: %v", err)
	}
}

// TestWiederverbindenNachAbriss: fällt die Leitung aus, baut die Heimseite
// sie von selbst wieder auf. Ohne das wäre der Fernzugriff nach dem ersten
// Anschlusswechsel für immer weg.
func TestWiederverbindenNachAbriss(t *testing.T) {
	if testing.Short() {
		t.Skip("braucht ein paar Sekunden")
	}
	a := stelleAuf(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "da")
	}))
	defer a.beenden()

	if status, _ := a.holen(t, "/"); status != 200 {
		t.Fatalf("vorher schon kaputt: %d", status)
	}

	// Den Heimserver abwürgen, wie es ein Router tut, der die Verbindung
	// vergisst. Dafür nehmen wir dem Vermittler die Leitung weg, indem wir
	// warten, bis sie wieder steht.
	a.Tunnel.Kappen()

	for i := 0; i < 400; i++ {
		if a.Vermittler.Stand().Verbunden {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if !a.Vermittler.Stand().Verbunden {
		t.Fatal("die Heimseite hat nicht wieder gewählt")
	}
	if status, leib := a.holen(t, "/"); status != 200 || leib != "da" {
		t.Fatalf("nach dem Wiederverbinden: %d %q", status, leib)
	}
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
