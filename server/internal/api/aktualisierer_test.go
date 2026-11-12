// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"lumen/internal/stream"
	"lumen/internal/update"
)

const testAktToken = "geheim-0123456789abcdef0123456789abcdef"

// falscherAktualisierer spielt Watchtower: POST /v1/update mit Bearer-Token.
type falscherAktualisierer struct {
	srv     *httptest.Server
	status  int    // Antwort bei passendem Token
	rumpf   string // JSON-Rumpf dazu
	aufrufe atomic.Int32
	mu      sync.Mutex
	koepfe  []string
}

func neuerFalscherAktualisierer(t *testing.T, status int, rumpf string) *falscherAktualisierer {
	t.Helper()
	f := &falscherAktualisierer{status: status, rumpf: rumpf}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/update" {
			http.NotFound(w, r)
			return
		}
		f.aufrufe.Add(1)
		f.mu.Lock()
		f.koepfe = append(f.koepfe, r.Header.Get("Authorization"))
		f.mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer "+testAktToken {
			http.Error(w, "missing or malformed API key", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(f.status)
		_, _ = w.Write([]byte(f.rumpf))
	}))
	t.Cleanup(f.srv.Close)
	return f
}

const aktErfolg = `{"summary":{"scanned":1,"updated":1,"failed":0,"restarted":0,"skipped":0},"api_version":"v1"}`
const aktNichts = `{"summary":{"scanned":1,"updated":0,"failed":0,"restarted":0,"skipped":0},"api_version":"v1"}`

// aktUmgebung: Container vorgetäuscht, Aktualisierer eingetragen, neuere
// Fassung bekannt. Der Stand im Speicher ist global — vorher und nachher
// leeren.
func aktUmgebung(t *testing.T, adresse string, container bool) {
	t.Helper()
	alt := updateImContainer
	updateImContainer = func() bool { return container }
	t.Setenv("LUMEN_AKTUALISIERER_URL", adresse)
	t.Setenv("LUMEN_AKTUALISIERER_TOKEN", testAktToken)
	updStand.mu.Lock()
	updStand.laeuft, updStand.akt, updStand.verschoben = false, aktStand{}, time.Time{}
	updStand.neueste = &update.Release{Version: "9.9.9", Tag: "v9.9.9"}
	updStand.geprueft = time.Now()
	updStand.mu.Unlock()
	t.Cleanup(func() {
		updateImContainer = alt
		updStand.mu.Lock()
		updStand.laeuft, updStand.akt, updStand.neueste = false, aktStand{}, nil
		updStand.geprueft = time.Time{}
		updStand.mu.Unlock()
	})
}

// warteAkt wartet, bis der Auftrag im Hintergrund eine Antwort hat.
func warteAkt(t *testing.T) aktStand {
	t.Helper()
	for i := 0; i < 200; i++ {
		updStand.mu.Lock()
		a := updStand.akt
		updStand.mu.Unlock()
		if a.Code != "" {
			return a
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("der Aktualisierer wurde nicht gerufen")
	return aktStand{}
}

func TestAktualisiererErfolg(t *testing.T) {
	h := newHarness(t)
	f := neuerFalscherAktualisierer(t, 200, aktErfolg)
	aktUmgebung(t, f.srv.URL+"/", true)
	var logPuffer bytes.Buffer
	h.api.log = slog.New(slog.NewTextHandler(&logPuffer, &slog.HandlerOptions{Level: slog.LevelDebug}))
	aktualisiererNachfrist = time.Hour
	t.Cleanup(func() { aktualisiererNachfrist = 10 * time.Minute })

	// Die Seite sagt, dass der Knopf geht — und nichts über Adresse und Token.
	_, raw := h.do("GET", "/api/v1/admin/update", "", h.tok)
	var u map[string]any
	_ = json.Unmarshal(raw, &u)
	if u["aktualisierer"] != true || u["container"] != true {
		t.Fatalf("Updates-Seite: %s", raw)
	}
	if strings.Contains(string(raw), testAktToken) || strings.Contains(string(raw), f.srv.URL) {
		t.Fatalf("Token oder Adresse in der Antwort: %s", raw)
	}

	resp, raw := h.do("POST", "/api/v1/admin/update/container", "", h.tok)
	if resp.StatusCode != http.StatusAccepted || strings.Contains(string(raw), testAktToken) {
		t.Fatalf("anstossen: %d %s", resp.StatusCode, raw)
	}
	if a := warteAkt(t); a.Code != aktOk || a.Nach != "9.9.9" {
		t.Fatalf("Stand: %+v", a)
	}
	if f.aufrufe.Load() != 1 || f.koepfe[0] != "Bearer "+testAktToken {
		t.Fatalf("Aufrufe %d, Köpfe %v", f.aufrufe.Load(), f.koepfe)
	}
	// Läuft — ein zweiter Druck stösst nichts Neues an.
	if resp, raw := h.do("POST", "/api/v1/admin/update/container", "", h.tok); resp.StatusCode != http.StatusConflict {
		t.Fatalf("zweimal: %d %s", resp.StatusCode, raw)
	}
	_, raw = h.do("GET", "/api/v1/admin/update", "", h.tok)
	_ = json.Unmarshal(raw, &u)
	proto, _ := u["protokoll"].([]any)
	if u["laeuft"] != true || len(proto) == 0 || proto[0].(map[string]any)["ergebnis"] != "angestossen" {
		t.Fatalf("nach dem Anstossen: %s", raw)
	}
	if strings.Contains(string(raw), testAktToken) {
		t.Fatalf("Token in der Antwort: %s", raw)
	}

	// Weder im Protokoll der Datenbank noch im Log steht das Token.
	zeilen, _ := h.st.RecentLog(h.t.Context(), "all", 200)
	for _, z := range zeilen {
		if strings.Contains(z.Message, testAktToken) {
			t.Fatalf("Token im Protokoll: %q", z.Message)
		}
	}
	if strings.Contains(logPuffer.String(), testAktToken) {
		t.Fatalf("Token im Log: %s", logPuffer.String())
	}

	// Nach dem Neustart mit neuer Fassung: „ok" im Protokoll.
	h.api.version = "9.9.9"
	h.api.aktualisiererNachStart(h.t.Context())
	if p := h.api.updateProtokoll(h.t.Context()); p[0].Ergebnis != "ok" || p[0].Von != "test" || p[0].Nach != "9.9.9" {
		t.Fatalf("nach dem Start: %+v", p[0])
	}
}

func TestAktualisiererFehler(t *testing.T) {
	besetzt, _ := net.Listen("tcp", "127.0.0.1:0")
	tot := "http://" + besetzt.Addr().String()
	besetzt.Close() // niemand hört mehr: Verbindung abgelehnt

	faelle := []struct {
		name    string
		status  int
		rumpf   string
		token   string
		adresse string
		code    string
	}{
		{"falsches Token", 200, aktErfolg, "falsch", "", aktTokenFalsch},
		{"nicht erreichbar", 0, "", testAktToken, tot, aktNichtErreicht},
		{"Name unbekannt", 0, "", testAktToken, "http://aktualisierer.invalid:8080", aktNichtErreicht},
		{"nichts Neues", 200, aktNichts, testAktToken, "", aktNichtsNeues},
		{"beschäftigt", 429, `{"error":"another update is already running"}`, testAktToken, "", aktBeschaeftigt},
		{"fehlgeschlagen", 200, `{"summary":{"updated":0,"failed":1}}`, testAktToken, "", aktFehlgeschlagen},
		{"falsche Adresse", 404, "", testAktToken, "", aktAntwortFalsch},
	}
	for _, fall := range faelle {
		t.Run(fall.name, func(t *testing.T) {
			h := newHarness(t)
			f := neuerFalscherAktualisierer(t, fall.status, fall.rumpf)
			adresse := fall.adresse
			if adresse == "" {
				adresse = f.srv.URL
			}
			aktUmgebung(t, adresse, true)
			t.Setenv("LUMEN_AKTUALISIERER_TOKEN", fall.token)
			if fall.status == 404 {
				t.Setenv("LUMEN_AKTUALISIERER_URL", f.srv.URL+"/falsch")
			}
			resp, raw := h.do("POST", "/api/v1/admin/update/container", "", h.tok)
			if resp.StatusCode != http.StatusAccepted {
				t.Fatalf("anstossen: %d %s", resp.StatusCode, raw)
			}
			if a := warteAkt(t); a.Code != fall.code {
				t.Fatalf("Code %q, erwartet %q", a.Code, fall.code)
			}
			// Der Knopf ist wieder frei, und das Protokoll sagt warum.
			for i := 0; i < 100; i++ {
				if p := h.api.updateProtokoll(h.t.Context()); len(p) > 0 && p[0].Ergebnis == "fehler" {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			_, raw = h.do("GET", "/api/v1/admin/update", "", h.tok)
			var u struct {
				Laeuft             bool
				AktualisiererStand aktStand
				Protokoll          []updateEintrag
			}
			_ = json.Unmarshal(raw, &u)
			if u.Laeuft || u.AktualisiererStand.Code != fall.code || len(u.Protokoll) < 2 ||
				u.Protokoll[0].Ergebnis != "fehler" || u.Protokoll[0].Notiz != aktualisiererNotiz(fall.code) {
				t.Fatalf("danach: %s", raw)
			}
			if strings.Contains(string(raw), testAktToken) {
				t.Fatalf("Token in der Antwort: %s", raw)
			}
		})
	}
}

func TestAktualisiererRechteUndBestaetigung(t *testing.T) {
	h := newHarness(t)
	f := neuerFalscherAktualisierer(t, 200, aktErfolg)
	aktUmgebung(t, f.srv.URL, true)

	// Ein Mitverwalter sieht den Weg gar nicht.
	mtok, mcode := h.mitverwalter("Jonas")
	if resp, raw := h.bestaetigtDo("POST", "/api/v1/admin/update/container", "", mtok, mcode); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("Mitverwalter: %d %s", resp.StatusCode, raw)
	}
	// Ohne Anmeldung auch nicht.
	if resp, _ := h.do("POST", "/api/v1/admin/update/container", "", ""); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("ohne Anmeldung: %d", resp.StatusCode)
	}
	// Der Besitzer ohne Code: 428, mit falschem: 403.
	h.ohneBestaetigung = true
	resp, raw := h.do("POST", "/api/v1/admin/update/container", "", h.tok)
	if resp.StatusCode != http.StatusPreconditionRequired || !strings.Contains(string(raw), `"bestaetigen"`) {
		t.Fatalf("ohne Code: %d %s", resp.StatusCode, raw)
	}
	if resp, raw := h.bestaetigtDo("POST", "/api/v1/admin/update/container", "", h.tok, "falsch"); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("falscher Code: %d %s", resp.StatusCode, raw)
	}
	if f.aufrufe.Load() != 0 {
		t.Fatal("ohne Bestätigung gerufen")
	}

	// Läuft eine Wiedergabe: erst 409 mit der Zahl, noch vor dem Code.
	sitzung := h.live.Start(&stream.Session{AccountID: h.ownerID(), DeviceName: "Fernseher", FileID: "f", Mode: "direct"})
	resp, raw = h.do("POST", "/api/v1/admin/update/container", "", h.tok)
	if resp.StatusCode != http.StatusConflict || !strings.Contains(string(raw), "wiedergaben_laufen") {
		t.Fatalf("Wiedergabe: %d %s", resp.StatusCode, raw)
	}
	// „Trotzdem" plus Code: angestossen.
	resp, raw = h.bestaetigtDo("POST", "/api/v1/admin/update/container?trotzdem=1", "", h.tok, "einsehrgutespasswort")
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("trotzdem: %d %s", resp.StatusCode, raw)
	}
	warteAkt(t)
	h.live.End(sitzung.ID)
	if f.aufrufe.Load() != 1 {
		t.Fatalf("Aufrufe: %d", f.aufrufe.Load())
	}
}

func TestAktualisiererOhneBegleiter(t *testing.T) {
	h := newHarness(t)
	// Im Container, aber ohne Token: kein Knopf, ein Hinweis, was fehlt.
	aktUmgebung(t, "http://aktualisierer:8080", true)
	t.Setenv("LUMEN_AKTUALISIERER_TOKEN", "")
	_, raw := h.do("GET", "/api/v1/admin/update", "", h.tok)
	var u map[string]any
	_ = json.Unmarshal(raw, &u)
	if u["aktualisierer"] != false || u["aktualisiererHalb"] != true || u["container"] != true {
		t.Fatalf("halb eingerichtet: %s", raw)
	}
	resp, raw := h.do("POST", "/api/v1/admin/update/container", "", h.tok)
	if resp.StatusCode != http.StatusPreconditionFailed || !strings.Contains(string(raw), "aktualisierer_fehlt") {
		t.Fatalf("ohne Token: %d %s", resp.StatusCode, raw)
	}
	// Ohne Container hilft auch ein vollständiger Eintrag nichts.
	aktUmgebung(t, "http://aktualisierer:8080", false)
	_, raw = h.do("GET", "/api/v1/admin/update", "", h.tok)
	_ = json.Unmarshal(raw, &u)
	if u["aktualisierer"] != false || u["aktualisiererHalb"] != false {
		t.Fatalf("ohne Container: %s", raw)
	}
	// Eine Adresse, die keine ist, zählt nicht.
	for _, a := range []string{"ftp://x", "aktualisierer:8080", "http://nutzer:pw@aktualisierer:8080", "http://"} {
		t.Setenv("LUMEN_AKTUALISIERER_URL", a)
		if aktualisiererAdresse() != "" {
			t.Errorf("%q sollte nicht gelten", a)
		}
	}
	t.Setenv("LUMEN_AKTUALISIERER_URL", " http://aktualisierer:8080/ ")
	if g := aktualisiererAdresse(); g != "http://aktualisierer:8080" {
		t.Errorf("Adresse %q", g)
	}
}

// Der Wächter im Fenster: im Container nur mit Begleiter, nur mit neuerer
// Fassung und nur, wenn niemand schaut.
func TestAktualisiererImFenster(t *testing.T) {
	h := newHarness(t)
	f := neuerFalscherAktualisierer(t, 200, aktNichts)
	e := updateEinstellung{Auto: "fenster"}.mitVorgaben()
	nachts := time.Date(2026, 11, 12, 3, 30, 0, 0, time.Local)
	tags := time.Date(2026, 11, 12, 14, 0, 0, 0, time.Local)
	schritt := func(jetzt time.Time, z *waechterZustand) string {
		return h.api.fensterSchritt(h.t.Context(), jetzt, e, z)
	}

	// Ohne Begleiter im Container: nichts — und schon gar kein Einsetzen.
	aktUmgebung(t, "", true)
	if a := schritt(nachts, &waechterZustand{}); a != fensterNichts {
		t.Fatalf("ohne Begleiter: %q", a)
	}

	aktUmgebung(t, f.srv.URL, true)
	// Ausserhalb des Fensters: nichts.
	if a := schritt(tags, &waechterZustand{}); a != fensterNichts {
		t.Fatalf("tagsüber: %q", a)
	}
	// Von selbst ist aus: nichts.
	if a := h.api.fensterSchritt(h.t.Context(), nachts, updateEinstellung{}.mitVorgaben(), &waechterZustand{}); a != fensterNichts {
		t.Fatalf("aus: %q", a)
	}
	// Nichts Neueres bekannt: nichts.
	updStand.mu.Lock()
	updStand.neueste = &update.Release{Version: "test"}
	updStand.mu.Unlock()
	if a := schritt(nachts, &waechterZustand{}); a != fensterNichts {
		t.Fatalf("nichts Neueres: %q", a)
	}
	updStand.mu.Lock()
	updStand.neueste = &update.Release{Version: "9.9.9"}
	updStand.mu.Unlock()

	// Jemand schaut: verschoben, der Begleiter bleibt ungefragt.
	sitzung := h.live.Start(&stream.Session{AccountID: h.ownerID(), DeviceName: "Fernseher", FileID: "f", Mode: "direct"})
	z := &waechterZustand{}
	if a := schritt(nachts, z); a != fensterVerschoben || f.aufrufe.Load() != 0 {
		t.Fatalf("jemand schaut: %q, %d Aufrufe", a, f.aufrufe.Load())
	}
	if a := schritt(nachts.Add(15*time.Minute), z); a != fensterNichts {
		t.Fatalf("nach dem Verschieben: %q", a)
	}
	h.live.End(sitzung.ID)

	// Niemand schaut: der Begleiter wird gerufen.
	z = &waechterZustand{}
	if a := schritt(nachts, z); a != fensterContainer || f.aufrufe.Load() != 1 {
		t.Fatalf("niemand schaut: %q, %d Aufrufe", a, f.aufrufe.Load())
	}
	// Er fand nichts Neues: erst in der nächsten Nacht wieder.
	if a := schritt(nachts.Add(30*time.Minute), z); a != fensterNichts || f.aufrufe.Load() != 1 {
		t.Fatalf("gleich nochmal: %q, %d Aufrufe", a, f.aufrufe.Load())
	}
	if a := schritt(nachts.Add(24*time.Hour), z); a != fensterContainer || f.aufrufe.Load() != 2 {
		t.Fatalf("nächste Nacht: %q, %d Aufrufe", a, f.aufrufe.Load())
	}
}
