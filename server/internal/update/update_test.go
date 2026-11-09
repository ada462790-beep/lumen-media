// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNeuer(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"0.1.10", "0.1.9", true},
		{"0.1.9", "0.1.10", false},
		{"v0.2.0", "0.1.99", true},
		{"0.1.7", "0.1.7", false},
		{"1.0.0", "0.9.9", true},
		{"0.1.7", "dev", false}, // wer aus dem Quelltext baut, wird nicht gedrängt
		{"dev", "0.1.7", false},
	}
	for _, c := range cases {
		if got := Neuer(c.a, c.b); got != c.want {
			t.Errorf("Neuer(%q,%q)=%v, erwartet %v", c.a, c.b, got, c.want)
		}
	}
}

func paket(t *testing.T, inhalt []byte) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "lumen-linux-amd64/lumen", Mode: 0o755, Size: int64(len(inhalt)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	tw.Write(inhalt)
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

// testSchluessel unterschreibt im Test, wie es der Veröffentlichungs-
// Ablauf mit dem echten Schlüssel tut.
var testSchluessel ed25519.PrivateKey

func init() {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	testSchluessel = priv
	signierSchluessel = base64.StdEncoding.EncodeToString(pub)
}

// Ein kleiner GitHub: eine Veröffentlichung mit Paket, Prüfsummen und
// Unterschrift.
func fakeGitHub(t *testing.T, pk []byte, summe string) *httptest.Server {
	return fakeGitHubMit(t, pk, summe, true)
}

func fakeGitHubMit(t *testing.T, pk []byte, summe string, gutSigniert bool) *httptest.Server {
	summen := fmt.Sprintf("%s v9.9.9\n%s  %s\nabc  lumen-anders.tar.gz\n", FassungsPraefix, summe, Paketname())
	return fakeGitHubSummen(t, pk, summen, gutSigniert)
}

// fakeGitHubSummen: wie oben, aber mit frei vorgegebener pruefsummen.txt —
// die Veröffentlichung heisst immer v9.9.9.
func fakeGitHubSummen(t *testing.T, pk []byte, summen string, gutSigniert bool) *httptest.Server {
	mux := http.NewServeMux()
	sig := ed25519.Sign(testSchluessel, []byte(summen))
	if !gutSigniert {
		_, fremd, _ := ed25519.GenerateKey(rand.Reader)
		sig = ed25519.Sign(fremd, []byte(summen))
	}
	var srv *httptest.Server
	mux.HandleFunc("/repos/x/y/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer geheim" {
			w.WriteHeader(404)
			return
		}
		fmt.Fprintf(w, `{"tag_name":"v9.9.9","html_url":"%s/rel","assets":[
			{"name":"%s","url":"%s/a/paket","browser_download_url":"%s/dl/paket","size":%d},
			{"name":"pruefsummen.txt","url":"%s/a/summen","browser_download_url":"%s/dl/summen","size":10},
			{"name":"pruefsummen.txt.sig","url":"%s/a/sig","browser_download_url":"%s/dl/sig","size":10}]}`,
			srv.URL, Paketname(), srv.URL, srv.URL, len(pk), srv.URL, srv.URL, srv.URL, srv.URL)
	})
	mux.HandleFunc("/a/paket", func(w http.ResponseWriter, r *http.Request) { w.Write(pk) })
	mux.HandleFunc("/a/summen", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, summen) })
	mux.HandleFunc("/a/sig", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, base64.StdEncoding.EncodeToString(sig))
	})
	srv = httptest.NewServer(mux)
	return srv
}

func TestHerunterladenPrueftSumme(t *testing.T) {
	if Paketname() != "lumen-linux-amd64.tar.gz" {
		t.Skip("Test ist für linux/amd64 geschrieben")
	}
	inhalt := []byte("#!/bin/sh\necho neu\n")
	pk := paket(t, inhalt)
	h := sha256.Sum256(pk)
	richtig := hex.EncodeToString(h[:])

	srv := fakeGitHub(t, pk, richtig)
	defer srv.Close()
	p := &Pruefer{Repo: "x/y", Version: "0.0.1", Token: "geheim", API: srv.URL}

	rel, err := p.Neueste(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if rel.Version != "9.9.9" || !Neuer(rel.Version, p.Version) {
		t.Fatalf("Release %+v", rel)
	}
	dir := t.TempDir()
	bin, err := p.Herunterladen(context.Background(), rel, dir)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(bin)
	if !bytes.Equal(got, inhalt) {
		t.Fatalf("ausgepackt: %q", got)
	}
	if fi, _ := os.Stat(bin); fi.Mode()&0o111 == 0 {
		t.Error("die neue Datei ist nicht ausführbar")
	}
	if _, err := os.Stat(filepath.Join(dir, Paketname())); !os.IsNotExist(err) {
		t.Error("das Paket sollte nach dem Auspacken weg sein")
	}

	// Falsche Prüfsumme: nichts einsetzen.
	srv2 := fakeGitHub(t, pk, "00ff")
	defer srv2.Close()
	p2 := &Pruefer{Repo: "x/y", Version: "0.0.1", Token: "geheim", API: srv2.URL}
	rel2, _ := p2.Neueste(context.Background())
	if _, err := p2.Herunterladen(context.Background(), rel2, t.TempDir()); err == nil {
		t.Fatal("falsche Prüfsumme wurde durchgelassen")
	}

	// Richtige Prüfsumme, aber mit fremdem Schlüssel unterschrieben (wer
	// Releases anlegen kann, aber den Schlüssel nicht hat): nichts einsetzen.
	srv3 := fakeGitHubMit(t, pk, richtig, false)
	defer srv3.Close()
	p3 := &Pruefer{Repo: "x/y", Version: "0.0.1", Token: "geheim", API: srv3.URL}
	rel3, _ := p3.Neueste(context.Background())
	if _, err := p3.Herunterladen(context.Background(), rel3, t.TempDir()); err == nil {
		t.Fatal("fremd unterschriebenes Paket wurde eingesetzt")
	}
}

// Das öffentliche Repository: kein Token, keine Anmeldung. Lumen fragt
// ohne Authorization-Kopf und lädt über die gewöhnlichen Adressen
// (browser_download_url), nicht über die API.
func TestOeffentlichOhneToken(t *testing.T) {
	inhalt := []byte("#!/bin/sh\necho neu\n")
	pk := paket(t, inhalt)
	h := sha256.Sum256(pk)
	summen := fmt.Sprintf("%s v9.9.9\n%s  %s\n", FassungsPraefix, hex.EncodeToString(h[:]), Paketname())
	sig := ed25519.Sign(testSchluessel, []byte(summen))

	mux := http.NewServeMux()
	var srv *httptest.Server
	ohneKopf := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "" {
				t.Errorf("%s: Authorization gesendet, obwohl kein Token gesetzt ist", r.URL.Path)
			}
			next(w, r)
		}
	}
	mux.HandleFunc("/repos/x/y/releases/latest", ohneKopf(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"tag_name":"v9.9.9","html_url":"%s/rel","assets":[
			{"name":"%s","url":"%s/a/paket","browser_download_url":"%s/dl/paket","size":%d},
			{"name":"pruefsummen.txt","url":"%s/a/summen","browser_download_url":"%s/dl/summen","size":10},
			{"name":"pruefsummen.txt.sig","url":"%s/a/sig","browser_download_url":"%s/dl/sig","size":10}]}`,
			srv.URL, Paketname(), srv.URL, srv.URL, len(pk), srv.URL, srv.URL, srv.URL, srv.URL)
	}))
	mux.HandleFunc("/a/", func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("ohne Token über die API geladen: %s", r.URL.Path)
		w.WriteHeader(500)
	})
	mux.HandleFunc("/dl/paket", ohneKopf(func(w http.ResponseWriter, r *http.Request) { w.Write(pk) }))
	mux.HandleFunc("/dl/summen", ohneKopf(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, summen) }))
	mux.HandleFunc("/dl/sig", ohneKopf(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, base64.StdEncoding.EncodeToString(sig))
	}))
	srv = httptest.NewServer(mux)
	defer srv.Close()

	p := &Pruefer{Repo: "x/y", Version: "0.0.1", API: srv.URL}
	rel, err := p.Neueste(context.Background())
	if err != nil {
		t.Fatalf("öffentliches Repository ohne Token: %v", err)
	}
	if rel.Version != "9.9.9" {
		t.Fatalf("Release %+v", rel)
	}
	if Paketname() != "lumen-linux-amd64.tar.gz" {
		return // das Testpaket ist für linux/amd64 gebaut
	}
	bin, err := p.Herunterladen(context.Background(), rel, t.TempDir())
	if err != nil {
		t.Fatalf("Herunterladen ohne Token: %v", err)
	}
	if got, _ := os.ReadFile(bin); !bytes.Equal(got, inhalt) {
		t.Fatalf("ausgepackt: %q", got)
	}
}

// Ohne Veröffentlichung (oder bei einem privaten Fork ohne Token) sagt
// die Meldung, was los ist, und nennt das Token als Ausweg.
func TestOhneVeroeffentlichungErklaert(t *testing.T) {
	srv := fakeGitHub(t, nil, "")
	defer srv.Close()
	p := &Pruefer{Repo: "x/y", Version: "0.0.1", API: srv.URL}
	_, err := p.Neueste(context.Background())
	if err == nil || !strings.Contains(err.Error(), "Token") || !strings.Contains(err.Error(), "noch keine Veröffentlichung") {
		t.Fatalf("erwartet Hinweis auf fehlende Veröffentlichung und aufs Token, bekam %v", err)
	}
}

func TestEinsetzenTauschtDatei(t *testing.T) {
	// Einsetzen arbeitet auf os.Executable — das ist hier der Testlauf.
	// Darum nur der Kern: Umbenennen in dieselbe Richtung mit kopieren().
	dir := t.TempDir()
	alt := filepath.Join(dir, "lumen")
	neu := filepath.Join(dir, "lumen-neu")
	os.WriteFile(alt, []byte("alt"), 0o755)
	os.WriteFile(neu, []byte("neu"), 0o755)
	if err := kopieren(neu, alt+".neu"); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(alt, alt+".alt"); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(alt+".neu", alt); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(alt)
	if string(got) != "neu" {
		t.Fatalf("Datei: %q", got)
	}
}

func TestKontingentIstKeinUngueltigesToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()
	p := &Pruefer{Repo: "a/b", Version: "0.1.0", Token: "x", API: srv.URL}
	_, err := p.anfrage(context.Background(), srv.URL+"/repos/a/b/releases/latest", "application/json")
	if err == nil || errors.Is(err, ErrTokenUngueltig) {
		t.Fatalf("Kontingent als ungültiges Token gemeldet: %v", err)
	}
}

// Eine ältere, echt unterschriebene Veröffentlichung unter neuem Tag
// wieder hochgeladen: Die Unterschrift stimmt, die Fassung nicht.
func TestAlteVeroeffentlichungUnterNeuemTag(t *testing.T) {
	if Paketname() != "lumen-linux-amd64.tar.gz" {
		t.Skip("Test ist für linux/amd64 geschrieben")
	}
	pk := paket(t, []byte("alt"))
	h := sha256.Sum256(pk)
	summe := hex.EncodeToString(h[:])

	faelle := map[string]string{
		"andere Fassung": fmt.Sprintf("%s v0.1.3\n%s  %s\n", FassungsPraefix, summe, Paketname()),
		"ohne Fassung":   fmt.Sprintf("%s  %s\n", summe, Paketname()),
	}
	for name, summen := range faelle {
		srv := fakeGitHubSummen(t, pk, summen, true)
		p := &Pruefer{Repo: "x/y", Version: "0.0.1", Token: "geheim", API: srv.URL}
		rel, err := p.Neueste(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := p.Herunterladen(context.Background(), rel, t.TempDir()); err == nil {
			t.Errorf("%s: wurde eingesetzt", name)
		}
		srv.Close()
	}
}

func TestPruefeSummen(t *testing.T) {
	summen := []byte(FassungsPraefix + " v0.1.20\nabc  lumen-linux-amd64.tar.gz\n")
	sig := []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(testSchluessel, summen)))
	for _, tag := range []string{"v0.1.20", "0.1.20"} {
		if err := PruefeSummen(summen, sig, tag); err != nil {
			t.Errorf("%s: %v", tag, err)
		}
	}
	for _, tag := range []string{"v0.1.21", "", "v0.1.2"} {
		if err := PruefeSummen(summen, sig, tag); err == nil {
			t.Errorf("%q wurde angenommen", tag)
		}
	}
	// Die Fassung lässt sich nicht nachträglich ändern, ohne dass die
	// Unterschrift bricht.
	gefaelscht := []byte(strings.Replace(string(summen), "v0.1.20", "v0.1.21", 1))
	if err := PruefeSummen(gefaelscht, sig, "v0.1.21"); err == nil {
		t.Error("geänderte Fassungszeile wurde angenommen")
	}
}

// Ältere Lumen-Fassungen müssen neue Veröffentlichungen weiter einsetzen
// können. Das hier ist ihr Leser, wörtlich aus der Fassung vor der
// Fassungszeile: Er muss die Prüfsumme trotz der neuen Zeile finden.
func TestAlterLeserVerstehtNeueSummen(t *testing.T) {
	summen := FassungsPraefix + " v0.1.20\n" +
		"0a1b  lumen-linux-amd64.tar.gz\n" +
		"2c3d  lumen-windows-amd64.zip\n"
	alterLeser := func(raw, name string) string {
		for _, zeile := range strings.Split(raw, "\n") {
			f := strings.Fields(zeile)
			if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name {
				return strings.ToLower(f[0])
			}
		}
		return ""
	}
	if got := alterLeser(summen, "lumen-linux-amd64.tar.gz"); got != "0a1b" {
		t.Fatalf("alter Leser fand %q", got)
	}
	// Die Zeile darf nie wie eine Prüfsummenzeile aussehen (zwei Felder).
	if n := len(strings.Fields(FassungsPraefix + " v1.2.3")); n == 2 {
		t.Fatal("die Fassungszeile hat zwei Felder — ein alter Leser könnte sie für eine Prüfsumme halten")
	}
}
