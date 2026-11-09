// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

// Diese Tests stehen im Paket selbst und nicht in relay_test, weil sie an
// die Fristen und Grenzen heran müssen, die von aussen nicht einstellbar
// sein sollen. Sie prüfen, dass jemand ohne Geheimnis den Vermittler nicht
// mit Speicher, Goroutinen oder Verbindungen zuschütten kann.
package relay

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"log/slog"
	"net"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"lumen/internal/cert"
)

func leise() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

// endlos liefert ohne Ende Bytes ohne Zeilenende und zählt mit, wie viele
// abgeholt wurden.
type endlos struct{ gelesen int64 }

func (e *endlos) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'A'
	}
	e.gelesen += int64(len(p))
	return len(p), nil
}

// TestEndloseZeileBrichtFruehAb: eine Zeile ohne Ende darf nicht bis zur
// Begrüssungsfrist in den Speicher laufen.
func TestEndloseZeileBrichtFruehAb(t *testing.T) {
	quelle := &endlos{}
	r := bufio.NewReader(quelle)

	var vorher, nachher runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&vorher)
	_, err := zeileLesen(r)
	runtime.ReadMemStats(&nachher)

	if !errors.Is(err, ErrFremdesProtokoll) {
		t.Fatalf("erwartet ErrFremdesProtokoll, bekommen %v", err)
	}
	// Mehr als ein Pufferinhalt darf nicht abgeholt werden.
	if quelle.gelesen > int64(r.Size()) {
		t.Fatalf("%d Bytes gelesen — es hätte nach %d Schluss sein müssen",
			quelle.gelesen, maxZeile)
	}
	if neu := nachher.TotalAlloc - vorher.TotalAlloc; neu > 16<<10 {
		t.Fatalf("%d Bytes belegt für eine Zeile, die abgelehnt wird", neu)
	}
}

func TestZeileGenauAnDerGrenze(t *testing.T) {
	gerade := strings.Repeat("x", maxZeile)
	z, err := zeileLesen(bufio.NewReader(strings.NewReader(gerade + "\n")))
	if err != nil || z != gerade {
		t.Fatalf("eine Zeile von genau maxZeile Bytes muss durchgehen: %v", err)
	}
	_, err = zeileLesen(bufio.NewReader(strings.NewReader(gerade + "y\n")))
	if !errors.Is(err, ErrFremdesProtokoll) {
		t.Fatalf("ein Byte mehr muss abgelehnt werden, bekommen %v", err)
	}
}

// ---------------------------------------------------------------- Aufbau

type testVermittler struct {
	v            *Vermittler
	tunnelAddr   string
	besucherAddr string
	zert         *x509.Certificate
}

func vermittlerAufstellen(t *testing.T, einstellen func(*Vermittler)) *testVermittler {
	t.Helper()
	ordner := t.TempDir()
	zert, _, err := cert.Anlegen(ordner+"/c.pem", ordner+"/k.pem", []string{"localhost", "127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	x, err := x509.ParseCertificate(zert.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	v, err := NeuerVermittler("ein-ausreichend-langes-geheimnis", leise())
	if err != nil {
		t.Fatal(err)
	}
	if einstellen != nil {
		einstellen(v)
	}
	tl, err := tls.Listen("tcp", "127.0.0.1:0", TLSAufbau(zert))
	if err != nil {
		t.Fatal(err)
	}
	bl, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	go v.Lauschen(tl, bl, stop)
	t.Cleanup(func() { close(stop) })
	return &testVermittler{v: v, tunnelAddr: tl.Addr().String(), besucherAddr: bl.Addr().String(), zert: x}
}

// sofortZu sagt, ob die Gegenseite die Verbindung binnen kurzem schliesst
// — so sieht eine Abweisung aus. Eine angenommene Verbindung wartet
// dagegen still auf die ersten Bytes.
func sofortZu(t *testing.T, c net.Conn) bool {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(400 * time.Millisecond))
	_, err := c.Read(make([]byte, 1))
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return false
	}
	return true
}

func waehle(t *testing.T, von, nach string) net.Conn {
	t.Helper()
	d := net.Dialer{Timeout: 2 * time.Second}
	if von != "" {
		d.LocalAddr = &net.TCPAddr{IP: net.ParseIP(von)}
	}
	c, err := d.Dial("tcp", nach)
	if err != nil {
		if von != "" {
			t.Skipf("von %s aus lässt sich nicht wählen: %v", von, err)
		}
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// ---------------------------------------------------------------- Grenzen

// TestEndloseZeileAmTunnelport: dasselbe von aussen — wer nach dem Gruss
// ein Megabyte ohne Zeilenende schickt, fliegt sofort raus, nicht erst nach
// der Begrüssungsfrist.
func TestEndloseZeileAmTunnelport(t *testing.T) {
	tv := vermittlerAufstellen(t, nil)
	wurzel := x509.NewCertPool()
	wurzel.AddCert(tv.zert)
	c, err := tls.Dial("tcp", tv.tunnelAddr, &tls.Config{
		RootCAs: wurzel, ServerName: "localhost", MinVersion: tls.VersionTLS12,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	beginn := time.Now()
	go func() {
		_, _ = c.Write([]byte(Gruss + "\n"))
		_, _ = c.Write([]byte(strings.Repeat("A", 1<<20)))
	}()
	_ = c.SetReadDeadline(time.Now().Add(Begruessungsfrist / 2))
	_, err = c.Read(make([]byte, 1))
	if err == nil {
		t.Fatal("der Vermittler hat auf eine endlose Zeile geantwortet")
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		t.Fatal("die Verbindung blieb offen, bis die Frist ablief")
	}
	if d := time.Since(beginn); d > 3*time.Second {
		t.Fatalf("erst nach %v geschlossen", d)
	}
}

func TestGrenzeJeAdresse(t *testing.T) {
	tv := vermittlerAufstellen(t, func(v *Vermittler) {
		v.MaxJeAdresse = 2
		v.MaxBesucher = 10
	})
	for i := 0; i < 2; i++ {
		if sofortZu(t, waehle(t, "", tv.besucherAddr)) {
			t.Fatalf("Besucher %d wurde abgewiesen, obwohl Platz war", i+1)
		}
	}
	dritter := waehle(t, "", tv.besucherAddr)
	if !sofortZu(t, dritter) {
		t.Fatal("der dritte Besucher derselben Adresse kam durch")
	}
	if tv.v.Stand().Abgewiesen == 0 {
		t.Fatal("die Abweisung wurde nicht gezählt")
	}
	// Eine andere Adresse hat ihr eigenes Kontingent.
	if sofortZu(t, waehle(t, "127.0.0.2", tv.besucherAddr)) {
		t.Fatal("eine andere Adresse wurde mit abgewiesen")
	}
}

func TestGrenzeInsgesamtUndFreigabe(t *testing.T) {
	tv := vermittlerAufstellen(t, func(v *Vermittler) {
		v.MaxBesucher = 2
		v.MaxJeAdresse = 10
	})
	erster := waehle(t, "", tv.besucherAddr)
	if sofortZu(t, erster) {
		t.Fatal("der erste Besucher wurde abgewiesen")
	}
	if sofortZu(t, waehle(t, "", tv.besucherAddr)) {
		t.Fatal("der zweite Besucher wurde abgewiesen")
	}
	if !sofortZu(t, waehle(t, "", tv.besucherAddr)) {
		t.Fatal("ein dritter Besucher kam über die Gesamtgrenze")
	}

	// Geht einer, wird sein Platz frei.
	_ = erster.Close()
	for i := 0; i < 100 && tv.v.Stand().Offen >= 2; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if sofortZu(t, waehle(t, "", tv.besucherAddr)) {
		t.Fatal("der freigewordene Platz wurde nicht wieder vergeben")
	}
}

// TestGrenzeAmTunnelport: auch ungeprüfte Händedrücke am Tunnelport sind
// begrenzt.
func TestGrenzeAmTunnelport(t *testing.T) {
	tv := vermittlerAufstellen(t, func(v *Vermittler) {
		v.MaxAnmeldungen = 2
	})
	for i := 0; i < 2; i++ {
		if sofortZu(t, waehle(t, "", tv.tunnelAddr)) {
			t.Fatalf("Anmeldung %d wurde abgewiesen, obwohl Platz war", i+1)
		}
	}
	if !sofortZu(t, waehle(t, "", tv.tunnelAddr)) {
		t.Fatal("eine dritte stumme Anmeldung wurde angenommen")
	}
}

// TestStummerBesucherWecktKeinenHeimserver: wer nur verbindet und nichts
// sagt, darf zu Hause keinen Datenweg auslösen.
func TestStummerBesucherWecktKeinenHeimserver(t *testing.T) {
	tv := vermittlerAufstellen(t, nil)

	// Eine Leitung von Hand, die mitzählt, was ankommt.
	wurzel := x509.NewCertPool()
	wurzel.AddCert(tv.zert)
	k, err := tls.Dial("tcp", tv.tunnelAddr, &tls.Config{
		RootCAs: wurzel, ServerName: "localhost", MinVersion: tls.VersionTLS12,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	r := bufio.NewReader(k)
	if err := AnmeldungSenden(k, r, Anmeldung{Rolle: Kontrolle, Geheimnis: tv.v.Geheimnis}); err != nil {
		t.Fatal(err)
	}

	waehle(t, "", tv.besucherAddr) // verbindet und schweigt
	_ = k.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	if zeile, err := zeileLesen(r); err == nil {
		t.Fatalf("für einen stummen Besucher kam %q", zeile)
	}
}

// ---------------------------------------------------------------- Ruhefrist

func paar(t *testing.T) (net.Conn, net.Conn) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	type ergebnis struct {
		c   net.Conn
		err error
	}
	an := make(chan ergebnis, 1)
	go func() {
		c, err := l.Accept()
		an <- ergebnis{c, err}
	}()
	a, err := net.Dial("tcp", l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	e := <-an
	if e.err != nil {
		t.Fatal(e.err)
	}
	t.Cleanup(func() { _ = a.Close(); _ = e.c.Close() })
	return a, e.c
}

// TestRuhefristSchliesstStillePaare: wer nichts mehr schickt, gibt seinen
// Platz nach der Ruhefrist her.
func TestRuhefristSchliesstStillePaare(t *testing.T) {
	_, besucherInnen := paar(t)
	_, heimInnen := paar(t)

	const ruhe = 200 * time.Millisecond
	fertig := make(chan struct{})
	go func() {
		durchreichen(besucherInnen, heimInnen, ruhe)
		close(fertig)
	}()
	select {
	case <-fertig:
	case <-time.After(10 * ruhe):
		t.Fatal("ein stilles Paar blieb offen")
	}
}

// TestLaufenderFilmWirdNichtAbgerissen: fliesst nur in eine Richtung etwas
// — wie bei einer Wiedergabe —, darf die stille Gegenrichtung das Paar nicht
// schliessen.
//
// Der Test misst mit echter Zeit. Auf einem ausgelasteten Rechner (unter
// -race, viele Tests zugleich) kann der Sender selbst einmal länger als die
// Ruhefrist nicht drankommen — dann ist es richtig, dass das Paar zugeht,
// und der Durchgang sagt nichts. Darum zählt nur ein Abriss, obwohl die
// grösste tatsächliche Lücke zwischen zwei Häppchen deutlich unter der
// Ruhefrist lag; ein Durchgang mit zu grosser Lücke wird wiederholt.
func TestLaufenderFilmWirdNichtAbgerissen(t *testing.T) {
	const ruhe = 150 * time.Millisecond
	for versuch := 1; versuch <= 5; versuch++ {
		abgerissen, luecke := filmDurchgang(t, ruhe)
		if !abgerissen {
			return
		}
		if luecke < ruhe*3/4 {
			t.Fatalf("das Paar wurde mitten im Film geschlossen, obwohl höchstens %v "+
				"zwischen zwei Häppchen lag (Ruhefrist %v)", luecke, ruhe)
		}
		t.Logf("Durchgang %d: der Sender stockte %v (Rechner ausgelastet) — noch einmal", versuch, luecke)
	}
	t.Skip("der Rechner ist zu ausgelastet: in keinem Durchgang kam der Sender rechtzeitig dran")
}

// filmDurchgang schickt zehn Ruhefristen lang Häppchen in eine Richtung.
// Zurück kommt, ob das Paar dabei zuging, und die grösste Lücke, die
// zwischen zwei gesendeten Häppchen tatsächlich lag.
func filmDurchgang(t *testing.T, ruhe time.Duration) (abgerissen bool, groessteLuecke time.Duration) {
	t.Helper()
	besucher, besucherInnen := paar(t)
	heim, heimInnen := paar(t)

	var ende atomic.Bool
	fertig := make(chan struct{})
	go func() {
		durchreichen(besucherInnen, heimInnen, ruhe)
		ende.Store(true)
		close(fertig)
	}()

	var wg sync.WaitGroup
	var empfangen atomic.Int64
	wg.Add(1)
	go func() {
		defer wg.Done()
		puffer := make([]byte, 1024)
		for {
			n, err := besucher.Read(puffer)
			empfangen.Add(int64(n))
			if err != nil {
				return
			}
		}
	}()

	const happen = 100
	gesendet := 0
	letztes := time.Now()
	for beginn := time.Now(); time.Since(beginn) < 10*ruhe; {
		if _, err := heim.Write(make([]byte, happen)); err != nil {
			abgerissen = true
			break
		}
		jetzt := time.Now()
		groessteLuecke = max(groessteLuecke, jetzt.Sub(letztes))
		letztes = jetzt
		gesendet += happen
		time.Sleep(ruhe / 5)
		if ende.Load() {
			abgerissen = true
			break
		}
	}
	// Die Zeit seit dem letzten Häppchen zählt mit: Ging das Paar in ihr
	// zu, war sie die Lücke.
	if abgerissen {
		groessteLuecke = max(groessteLuecke, time.Since(letztes))
		_ = heim.Close()
		_ = besucher.Close()
		<-fertig
		wg.Wait()
		return true, groessteLuecke
	}

	// Danach schweigen beide — dann darf und soll es zugehen.
	select {
	case <-fertig:
	case <-time.After(10 * ruhe):
		t.Fatal("nach dem Film blieb das Paar offen")
	}
	wg.Wait()
	if int(empfangen.Load()) != gesendet {
		t.Fatalf("%d gesendet, %d angekommen", gesendet, empfangen.Load())
	}
	return false, groessteLuecke
}

// ---------------------------------------------------------------- Heimseite

// TestHeimseiteBegrenztDatenwege: ein Vermittler, der mit OEFFNE um sich
// wirft, bekommt nicht mehr Verbindungen aus dem Heimnetz, als erlaubt.
func TestHeimseiteBegrenztDatenwege(t *testing.T) {
	ordner := t.TempDir()
	zert, _, err := cert.Anlegen(ordner+"/c.pem", ordner+"/k.pem", []string{"localhost", "127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	x, _ := x509.ParseCertificate(zert.Certificate[0])
	l, err := tls.Listen("tcp", "127.0.0.1:0", TLSAufbau(zert))
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	// Der falsche Vermittler: nimmt die Leitung an, schickt zwanzig OEFFNE
	// und lässt danach jeden Datenweg einfach hängen.
	var datenwege, hoechstens atomic.Int64
	go func() {
		erste := true
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn, kontrolle bool) {
				defer c.Close()
				r := bufio.NewReader(c)
				if _, err := AnmeldungLesen(c, r); err != nil {
					return
				}
				if kontrolle {
					_ = Bereit(c)
					for i := 0; i < 20; i++ {
						_ = zeileSchreiben(c, "OEFFNE k%d", i)
					}
					_, _ = io.Copy(io.Discard, c)
					return
				}
				n := datenwege.Add(1)
				for {
					h := hoechstens.Load()
					if n <= h || hoechstens.CompareAndSwap(h, n) {
						break
					}
				}
				_, _ = io.Copy(io.Discard, c)
				datenwege.Add(-1)
			}(c, erste)
			erste = false
		}
	}()

	ctx, aus := context.WithCancel(context.Background())
	defer aus()
	h := &Heimseite{
		Vermittler:    l.Addr().String(),
		Geheimnis:     "ein-ausreichend-langes-geheimnis",
		Ziel:          "127.0.0.1:1",
		Fingerabdruck: Fingerabdruck(x),
		MaxDatenwege:  3,
		Log:           leise(),
	}
	go h.Laufen(ctx)

	time.Sleep(time.Second)
	if n := hoechstens.Load(); n != 3 {
		t.Fatalf("%d Datenwege gleichzeitig, erlaubt sind 3", n)
	}
}
