// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------- Adressen

func TestKandidaten(t *testing.T) {
	faelle := map[string][]string{
		"192.168.10.10":             {"http://192.168.10.10:8097", "https://192.168.10.10"},
		" 192.168.10.10:8097/ ":     {"http://192.168.10.10:8097"},
		"nas:9000":                  {"http://nas:9000"},
		"http://nas":                {"http://nas", "http://nas:8097"},
		"https://lumen.example.org": {"https://lumen.example.org"},
		"https://lumen.example.org/web/index.html?x#y": {"https://lumen.example.org"},
		"HTTP://NAS:8097":  {"http://NAS:8097"},
		"[fe80::1]:8097":   {"http://[fe80::1]:8097"},
		"http://[fe80::1]": {"http://[fe80::1]", "http://[fe80::1]:8097"},
	}
	for ein, soll := range faelle {
		ist, err := Kandidaten(ein)
		if err != nil || !reflect.DeepEqual(ist, soll) {
			t.Errorf("Kandidaten(%q) = %v, %v; soll %v", ein, ist, err, soll)
		}
	}
	for _, schlecht := range []string{"", "   ", "ftp://nas", "javascript:alert(1)", "nas mit leerzeichen", "http://user:pw@nas", "http://"} {
		if k, err := Kandidaten(schlecht); err == nil {
			t.Errorf("Kandidaten(%q) = %v, soll Fehler", schlecht, k)
		} else if fehlerCode(err) != fehlerAdresse {
			t.Errorf("Kandidaten(%q): Code %q", schlecht, fehlerCode(err))
		}
	}
}

func helloServer(t *testing.T, antwort string, status int) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/hello" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(status)
		fmt.Fprint(w, antwort)
	}))
	t.Cleanup(s.Close)
	return s
}

func TestPruefen(t *testing.T) {
	ctx := context.Background()
	c := &http.Client{Timeout: 3 * time.Second}

	s := helloServer(t, `{"server":"Lumen","name":"Mein NAS","version":"0.4.0","setUp":true}`, 200)
	g, err := Pruefen(ctx, c, strings.TrimPrefix(s.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	if g.Name != "Mein NAS" || g.Version != "0.4.0" || !g.Eingerichtet || g.Adresse != s.URL {
		t.Errorf("gefunden: %+v", g)
	}

	// Ohne Namen steht der Host da.
	s2 := helloServer(t, `{"server":"Lumen","version":"0.4.0"}`, 200)
	g, err = Pruefen(ctx, c, s2.URL)
	if err != nil || g.Name != "127.0.0.1" {
		t.Errorf("ohne Namen: %+v %v", g, err)
	}

	// Etwas anderes als Lumen.
	s3 := helloServer(t, `{"server":"Emby"}`, 200)
	if _, err := Pruefen(ctx, c, s3.URL); fehlerCode(err) != fehlerKeinLumen {
		t.Errorf("Emby: %v", err)
	}
	s4 := helloServer(t, `nicht json`, 404)
	if _, err := Pruefen(ctx, c, s4.URL); fehlerCode(err) != fehlerKeinLumen {
		t.Errorf("404: %v", err)
	}

	// Niemand da.
	tot := httptest.NewServer(http.NotFoundHandler())
	adr := tot.URL
	tot.Close()
	if _, err := Pruefen(ctx, c, adr); fehlerCode(err) != fehlerErreichbar {
		t.Errorf("tot: %v", err)
	}

	// Selbst signiertes Zertifikat.
	tls := httptest.NewTLSServer(http.NotFoundHandler())
	defer tls.Close()
	if _, err := Pruefen(ctx, c, tls.URL); fehlerCode(err) != fehlerZertifikat {
		t.Errorf("Zertifikat: %v", err)
	}
}

// ---------------------------------------------------------------- Liste

func TestServerliste(t *testing.T) {
	pfad := filepath.Join(t.TempDir(), "server.json")
	l, err := LadeServerliste(pfad)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := l.AktiverServer(); ok {
		t.Fatal("leere Liste hat einen aktiven Server")
	}
	a, _ := l.Merken(Server{Adresse: "http://192.168.10.10:8097", Name: "NAS"})
	b, _ := l.Merken(Server{Adresse: "https://lumen.example.org", Name: "Ferien"})
	if akt, _ := l.AktiverServer(); akt.ID != b.ID {
		t.Errorf("aktiv nach Merken: %v", akt)
	}
	// Dieselbe Adresse noch einmal: derselbe Eintrag, neuer Name.
	a2, _ := l.Merken(Server{Adresse: "HTTP://192.168.10.10:8097", Name: "NAS neu"})
	if a2.ID != a.ID {
		t.Errorf("doppelt angelegt: %v %v", a, a2)
	}
	server, aktiv := l.Kopie()
	if len(server) != 2 || aktiv != a.ID || server[0].Name != "NAS neu" {
		t.Errorf("Liste: %+v aktiv %s", server, aktiv)
	}

	// Wieder laden: alles noch da.
	l2, err := LadeServerliste(pfad)
	if err != nil {
		t.Fatal(err)
	}
	if s, ok := l2.AktiverServer(); !ok || s.ID != a.ID {
		t.Errorf("nach Laden: %v %v", s, ok)
	}
	if _, ok, _ := l2.Waehlen(b.ID); !ok {
		t.Error("Waehlen")
	}
	if ok, _ := l2.Entfernen(b.ID); !ok {
		t.Error("Entfernen")
	}
	if _, ok := l2.AktiverServer(); ok {
		t.Error("entfernter Server ist noch aktiv")
	}
	if _, ok, _ := l2.Waehlen("gibtsnicht"); ok {
		t.Error("unbekannter Server gewählt")
	}
}

func TestKaputteServerliste(t *testing.T) {
	pfad := filepath.Join(t.TempDir(), "server.json")
	os.WriteFile(pfad, []byte("{kaputt"), 0o600)
	app, _ := NeueApp(&attrappe{}, filepath.Dir(pfad), t.TempDir())
	if app.StartAdresse() != seitenAdresse {
		t.Errorf("Start mit kaputter Liste: %s", app.StartAdresse())
	}
}

// ---------------------------------------------------------------- Navigation

func TestWohin(t *testing.T) {
	app, _ := NeueApp(&attrappe{}, t.TempDir(), t.TempDir())
	app.liste.Merken(Server{Adresse: "http://192.168.10.10:8097", Name: "NAS"})
	app.liste.Merken(Server{Adresse: "https://lumen.example.org", Name: "Ferien"})
	faelle := map[string]Weg{
		"http://lumen.localhost/":                   WegErlauben,
		"http://lumen.localhost/?wechseln=1":        WegErlauben,
		"http://192.168.10.10:8097/":                WegErlauben,
		"http://192.168.10.10:8097/#/detail/1":      WegErlauben,
		"https://lumen.example.org:443/":            WegErlauben,
		"https://lumen.example.org/api/v1/stream/1": WegErlauben,
		"about:blank":                           WegErlauben,
		"http://192.168.10.10:8098/":            WegExtern,
		"https://192.168.10.10:8097/":           WegExtern,
		"https://www.imdb.com/title/tt0133093/": WegExtern,
		"https://www.themoviedb.org/movie/603":  WegExtern,
		"mailto:jemand@example.org":             WegExtern,
		"file:///C:/Windows/System32/calc.exe":  WegSperren,
		"ms-settings:display":                   WegSperren,
		"javascript:alert(1)":                   WegSperren,
	}
	for adr, soll := range faelle {
		if ist := app.Wohin(adr); ist != soll {
			t.Errorf("Wohin(%q) = %d, soll %d", adr, ist, soll)
		}
	}
	if app.StartAdresse() != "https://lumen.example.org/" {
		t.Errorf("Start: %s", app.StartAdresse())
	}
}

// ---------------------------------------------------------------- Brücke

type attrappe struct {
	mu        sync.Mutex
	navigiert []string
	skripte   []string
	extern    []string
	gestartet string
	zu        bool
	ton       []string // „ursprung an/aus"
}

func (a *attrappe) Auftrag(f func()) { f() }
func (a *attrappe) Navigieren(s string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.navigiert = append(a.navigiert, s)
}
func (a *attrappe) Skript(s string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.skripte = append(a.skripte, s)
}
func (a *attrappe) Extern(s string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.extern = append(a.extern, s)
}
func (a *attrappe) InstallerStarten(p string) error { a.gestartet = p; return nil }
func (a *attrappe) Schliessen()                     { a.zu = true }
func (a *attrappe) TonausgabeErlauben(ursprung string, an bool, weiter func(error)) {
	a.mu.Lock()
	a.ton = append(a.ton, fmt.Sprintf("%s %v", ursprung, an))
	a.mu.Unlock()
	weiter(nil)
}

func (a *attrappe) letzteAntwort(t *testing.T) antwort {
	t.Helper()
	a.mu.Lock()
	defer a.mu.Unlock()
	for i := len(a.skripte) - 1; i >= 0; i-- {
		s := a.skripte[i]
		if j := strings.Index(s, "__lumenAntwort("); j >= 0 && strings.HasPrefix(s, "window.__lumenAntwort") {
			var an antwort
			if err := json.Unmarshal([]byte(strings.TrimSuffix(s[j+len("__lumenAntwort("):], ")")), &an); err != nil {
				t.Fatal(err)
			}
			return an
		}
	}
	t.Fatal("keine Antwort")
	return antwort{}
}

func warte(t *testing.T, bedingung func() bool) {
	t.Helper()
	for i := 0; i < 200; i++ {
		if bedingung() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("Zeit abgelaufen")
}

func TestBruecke(t *testing.T) {
	h := &attrappe{}
	app, _ := NeueApp(h, t.TempDir(), t.TempDir())
	s := helloServer(t, `{"server":"Lumen","name":"Testserver","version":"0.4.0"}`, 200)
	fremd := "http://192.168.10.10:8097/"

	// Die Web-App eines Servers darf die Liste nicht sehen.
	app.Nachricht(fremd, `{"id":1,"art":"liste"}`)
	if an := h.letzteAntwort(t); an.OK || an.Fehler != fehlerNichtErlaub {
		t.Errorf("fremde Seite bekam die Liste: %+v", an)
	}
	app.Nachricht(fremd, `{"id":2,"art":"hinzufuegen","daten":{"adresse":"`+s.URL+`"}}`)
	if an := h.letzteAntwort(t); an.OK {
		t.Errorf("fremde Seite durfte hinzufügen: %+v", an)
	}

	// Die eingebaute Seite schon.
	app.Nachricht(seitenAdresse, `{"id":3,"art":"hinzufuegen","daten":{"adresse":"`+s.URL+`"}}`)
	warte(t, func() bool { return h.letzteAntwort(t).ID == 3 })
	an := h.letzteAntwort(t)
	if !an.OK {
		t.Fatalf("hinzufügen: %+v", an)
	}
	id := an.Daten.(map[string]any)["id"].(string)
	app.Nachricht(seitenAdresse, `{"id":4,"art":"waehlen","daten":{"id":"`+id+`"}}`)
	if len(h.navigiert) != 1 || h.navigiert[0] != s.URL+"/" {
		t.Errorf("gewählt, aber navigiert nach %v", h.navigiert)
	}

	// Jetzt gehört der Server dazu; „Server wechseln" geht von dort aus.
	app.Nachricht(s.URL+"/", `{"id":5,"art":"serverWechseln"}`)
	if h.navigiert[len(h.navigiert)-1] != seitenAdresse+"?wechseln=1" {
		t.Errorf("wechseln: %v", h.navigiert)
	}
	// Externe Links nur im Browser, nie etwas anderes.
	app.Nachricht(s.URL+"/", `{"id":6,"art":"extern","daten":{"adresse":"https://www.imdb.com/title/tt0133093/"}}`)
	app.Nachricht(s.URL+"/", `{"id":7,"art":"extern","daten":{"adresse":"file:///C:/Windows/System32/calc.exe"}}`)
	if len(h.extern) != 1 || h.extern[0] != "https://www.imdb.com/title/tt0133093/" {
		t.Errorf("extern: %v", h.extern)
	}
	// Unsinn wird übergangen.
	app.Nachricht(seitenAdresse, `kein json`)
	app.Nachricht(seitenAdresse, `{"id":8}`)

	// Fehlgeschlagenes Laden führt zur Auswahl mit Hinweis.
	app.LadenFehlgeschlagen(s.URL+"/", fehlerErreichbar)
	if last := h.navigiert[len(h.navigiert)-1]; !strings.Contains(last, "fehler=nicht_erreichbar") || !strings.Contains(last, "id="+id) {
		t.Errorf("Fehlerseite: %s", last)
	}
	n := len(h.navigiert)
	app.LadenFehlgeschlagen("https://www.imdb.com/", fehlerErreichbar)
	if len(h.navigiert) != n {
		t.Error("fremde Seite löste die Fehlerseite aus")
	}
}

func TestEingebaut(t *testing.T) {
	for adr, soll := range map[string]bool{
		"http://lumen.localhost/":                  true,
		"http://lumen.localhost/?x=1":              true,
		"https://lumen.localhost/":                 false,
		"http://lumen.localhost.evil.org/":         false,
		"http://evil.org/?http://lumen.localhost/": false,
	} {
		if eingebaut(adr) != soll {
			t.Errorf("eingebaut(%q) != %v", adr, soll)
		}
	}
}

func TestSeite(t *testing.T) {
	for _, p := range []string{"/", "/seite.js", "/seite.css", "/symbol.png"} {
		b, typ, st := SeitenAntwort("http://lumen.localhost" + p)
		if st != 200 || len(b) == 0 || typ == "" {
			t.Errorf("%s: %d %s", p, st, typ)
		}
	}
	for _, p := range []string{"http://lumen.localhost/../app.go", "http://lumen.localhost/gibtsnicht", "http://anders/"} {
		if _, _, st := SeitenAntwort(p); st != 404 {
			t.Errorf("%s: %d", p, st)
		}
	}
	if !strings.Contains(brueckeSkript(), "version: '"+version+"'") {
		t.Error("Fassung fehlt in der Brücke")
	}
}

func TestFensterzustand(t *testing.T) {
	d := t.TempDir()
	if _, ok := LadeFenster(d); ok {
		t.Error("ohne Datei ein Zustand")
	}
	z := Fensterzustand{Links: 100, Oben: 80, Rechts: 1380, Unten: 880, Maximiert: true}
	if err := SpeichereFenster(d, z); err != nil {
		t.Fatal(err)
	}
	if z2, ok := LadeFenster(d); !ok || z2 != z {
		t.Errorf("gelesen %+v", z2)
	}
	if (Fensterzustand{Rechts: 10, Unten: 10}).Gueltig() {
		t.Error("winziges Fenster gilt")
	}
}

// ---------------------------------------------------------------- Update

func TestNeuer(t *testing.T) {
	for _, f := range []struct {
		a, b string
		soll bool
	}{
		{"0.1.10", "0.1.9", true}, {"v1.0.0", "0.9.9", true}, {"0.4.0", "0.4.0", false},
		{"0.3.9", "0.4.0", false}, {"dev", "0.1.0", false}, {"0.1.0", "dev", false}, {"", "0.1.0", false},
	} {
		if Neuer(f.a, f.b) != f.soll {
			t.Errorf("Neuer(%q, %q) != %v", f.a, f.b, f.soll)
		}
	}
}

// githubAttrappe stellt eine Veröffentlichung mit Installer, Prüfsummen
// und Unterschrift bereit.
func githubAttrappe(t *testing.T, tag string, installer []byte, verfaelschen string) (*httptest.Server, func()) {
	t.Helper()
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	alt := signierSchluessel
	signierSchluessel = base64.StdEncoding.EncodeToString(pub)
	name := InstallerName(tag)
	sum := sha256.Sum256(installer)
	summen := fmt.Sprintf("# lumen-version: %s\n%s  lumen-linux-amd64.tar.gz\n%s  %s\n", tag, strings.Repeat("0", 64), hex.EncodeToString(sum[:]), name)
	if verfaelschen == "fassung" {
		summen = strings.Replace(summen, tag, "v0.0.1", 1)
	}
	sig := base64.StdEncoding.EncodeToString(ed25519.Sign(priv, []byte(summen)))
	if verfaelschen == "unterschrift" {
		summen += "\n"
	}
	inhalt := installer
	if verfaelschen == "installer" {
		inhalt = append([]byte("x"), installer...)
	}
	var s *httptest.Server
	s = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/" + repository + "/releases/latest":
			fmt.Fprintf(w, `{"tag_name":%q,"html_url":"https://github.com/x/releases/%s","assets":[
				{"name":%q,"browser_download_url":%q},
				{"name":"pruefsummen.txt","browser_download_url":%q},
				{"name":"pruefsummen.txt.sig","browser_download_url":%q}]}`,
				tag, tag, name, s.URL+"/dl/"+name, s.URL+"/dl/pruefsummen.txt", s.URL+"/dl/pruefsummen.txt.sig")
		case "/dl/" + name:
			w.Write(inhalt)
		case "/dl/pruefsummen.txt":
			fmt.Fprint(w, summen)
		case "/dl/pruefsummen.txt.sig":
			fmt.Fprint(w, sig)
		default:
			http.NotFound(w, r)
		}
	}))
	return s, func() { s.Close(); signierSchluessel = alt }
}

func TestInstallerLaden(t *testing.T) {
	installer := []byte("MZ das ist ein Installer")
	for _, f := range []struct {
		verfaelschen string
		ok           bool
	}{{"", true}, {"installer", false}, {"unterschrift", false}, {"fassung", false}} {
		s, aufraeumen := githubAttrappe(t, "v0.5.0", installer, f.verfaelschen)
		u := &Aktualisierer{Repo: repository, Version: "0.4.0", API: s.URL}
		v, err := u.Neueste(context.Background())
		if err != nil || v.Version != "0.5.0" || !v.HatInstaller() {
			t.Fatalf("Neueste: %+v %v", v, err)
		}
		ordner := t.TempDir()
		pfad, err := u.InstallerLaden(context.Background(), v, ordner)
		if f.ok {
			if err != nil {
				t.Errorf("%q: %v", f.verfaelschen, err)
			} else if b, _ := os.ReadFile(pfad); string(b) != string(installer) || filepath.Base(pfad) != "Lumen-Setup-0.5.0-windows-x64.exe" {
				t.Errorf("geladen: %s %q", pfad, b)
			}
		} else {
			if err == nil {
				t.Errorf("%q: angenommen", f.verfaelschen)
			}
			if eintraege, _ := os.ReadDir(ordner); len(eintraege) != 0 {
				t.Errorf("%q: Reste im Ordner: %v", f.verfaelschen, eintraege)
			}
		}
		aufraeumen()
	}
}

func TestUpdateHoechstensTaeglich(t *testing.T) {
	alt := version
	version = "0.4.0"
	defer func() { version = alt }()
	s, aufraeumen := githubAttrappe(t, "v0.5.0", []byte("MZ"), "")
	defer aufraeumen()

	h := &attrappe{}
	app, _ := NeueApp(h, t.TempDir(), t.TempDir())
	app.upd.API = s.URL
	app.upd.Version = version
	app.UpdatePruefen(context.Background())
	st := app.updateStand()
	if !st.Verfuegbar || st.Version != "0.5.0" || st.Phase != "angeboten" {
		t.Fatalf("Stand: %+v", st)
	}
	// Zweiter Start am selben Tag: GitHub ist weg, der Hinweis bleibt.
	s.Close()
	app2, _ := NeueApp(h, app.ordner, app.lokal)
	app2.UpdatePruefen(context.Background())
	if !app2.updateStand().Verfuegbar {
		t.Error("Hinweis ohne erneute Abfrage verloren")
	}
	// „Später" blendet ihn aus, auch beim nächsten Start.
	app2.updateSpaeter()
	app3, _ := NeueApp(h, app.ordner, app.lokal)
	app3.UpdatePruefen(context.Background())
	if app3.updateStand().Verfuegbar {
		t.Error("Hinweis trotz „Später\"")
	}
}

func TestUpdateLadenUndStarten(t *testing.T) {
	alt := version
	version = "0.4.0"
	defer func() { version = alt }()
	s, aufraeumen := githubAttrappe(t, "v0.5.0", []byte("MZ installer"), "")
	defer aufraeumen()
	h := &attrappe{}
	app, _ := NeueApp(h, t.TempDir(), t.TempDir())
	app.upd.API = s.URL
	app.UpdatePruefen(context.Background())
	app.updateLaden()
	if st := app.updateStand(); st.Phase != "bereit" {
		t.Fatalf("nach Laden: %+v", st)
	}
	app.Nachricht("http://lumen.localhost/", `{"id":1,"art":"updateStarten"}`)
	if !strings.HasSuffix(h.gestartet, "Lumen-Setup-0.5.0-windows-x64.exe") || !h.zu {
		t.Errorf("gestartet %q, zu %v", h.gestartet, h.zu)
	}
}

// ---------------------------------------------------------------- Tonausgabe

func TestTonausgabeNurFuerDenEigenenServer(t *testing.T) {
	h := &attrappe{}
	app, _ := NeueApp(h, t.TempDir(), t.TempDir())
	if _, err := app.liste.Merken(Server{Adresse: "http://192.168.10.10:8097", Name: "NAS"}); err != nil {
		t.Fatal(err)
	}

	// Die eingebaute Seite und fremde Seiten dürfen das nicht.
	for i, quelle := range []string{seitenAdresse, "http://192.168.10.11:8097/", "https://example.org/"} {
		app.Nachricht(quelle, fmt.Sprintf(`{"id":%d,"art":"tonausgabe","daten":{"an":true}}`, i+1))
		if an := h.letzteAntwort(t); an.OK || an.Fehler != fehlerNichtErlaub {
			t.Errorf("%s durfte die Tonausgabe freigeben: %+v", quelle, an)
		}
	}
	if len(h.ton) != 0 {
		t.Fatalf("Erlaubnis gesetzt: %v", h.ton)
	}

	// Die Web-App des Servers schon — für ihren Ursprung, ohne Pfad.
	app.Nachricht("http://192.168.10.10:8097/#/einstellungen", `{"id":10,"art":"tonausgabe","daten":{"an":true}}`)
	if an := h.letzteAntwort(t); !an.OK {
		t.Errorf("freigeben: %+v", an)
	}
	app.Nachricht("http://192.168.10.10:8097/", `{"id":11,"art":"tonausgabe","daten":{"an":false}}`)
	if an := h.letzteAntwort(t); !an.OK || an.ID != 11 {
		t.Errorf("zurücknehmen: %+v", an)
	}
	soll := []string{"http://192.168.10.10:8097 true", "http://192.168.10.10:8097 false"}
	if strings.Join(h.ton, "|") != strings.Join(soll, "|") {
		t.Errorf("ton = %v, soll %v", h.ton, soll)
	}
}

func TestUnsichereUrspruenge(t *testing.T) {
	server := []Server{
		{Adresse: "http://192.168.10.10:8097"},
		{Adresse: "HTTP://NAS.fritz.box:8097"},
		{Adresse: "https://lumen.example.org"},
		{Adresse: "http://localhost:8097"},
		{Adresse: "http://127.0.0.1:8097"},
		{Adresse: "http://[fe80::1]:8097"},
		{Adresse: "http://192.168.10.10:8097"},
		{Adresse: "http://a,b:1"},
		{Adresse: "kaputt"},
	}
	ist := strings.Join(UnsichereUrspruenge(server, "http://10.1.0.5:4711"), ",")
	soll := "http://192.168.10.10:8097,http://nas.fritz.box:8097,http://[fe80::1]:8097,http://10.1.0.5:4711"
	if ist != soll {
		t.Errorf("UnsichereUrspruenge = %s\nsoll                %s", ist, soll)
	}
	if len(UnsichereUrspruenge(nil)) != 0 {
		t.Error("ohne Server soll nichts dabei sein")
	}
}
