// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"lumen/internal/store"
)

// unterwegs schickt eine Anfrage, wie sie über den Vermittler ankommt.
func (h *harness) unterwegs(method, path, body string) (int, string) {
	h.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Host = "vermittler.example:8443"
	h.api.SetzeErlaubteHosts("vermittler.example:8444")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	VonAussen(h.api.Handler()).ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// Von unterwegs: keine Profilliste, kein Profil ohne PIN, keine Einrichtung.
func TestVonUnterwegs(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()
	if _, err := h.st.CreateAccount(ctx, store.NewAccount{Name: "Mia"}); err != nil { // Kinderprofil ohne PIN
		t.Fatal(err)
	}
	if _, err := h.st.CreateAccount(ctx, store.NewAccount{Name: "Lea", LockKind: "pin", Secret: "4711"}); err != nil {
		t.Fatal(err)
	}

	code, body := h.unterwegs("GET", "/api/v1/profiles", "")
	if code != http.StatusForbidden || strings.Contains(body, "Mia") || strings.Contains(body, "Lea") {
		t.Fatalf("Profilliste von unterwegs: %d %s", code, body)
	}
	// Zu Hause gibt es sie.
	if resp, raw := h.do("GET", "/api/v1/profiles", "", ""); resp.StatusCode != 200 || !strings.Contains(string(raw), "Mia") {
		t.Fatalf("Profilliste zu Hause: %d %s", resp.StatusCode, raw)
	}

	// Profil ohne PIN: von unterwegs nicht zu öffnen, auch mit leerem PIN.
	code, body = h.unterwegs("POST", "/api/v1/unlock", `{"name":"Mia","secret":""}`)
	if code != http.StatusUnauthorized || strings.Contains(body, "token") {
		t.Fatalf("Profil ohne PIN von unterwegs geöffnet: %d %s", code, body)
	}
	// Mit Name und PIN geht es.
	code, body = h.unterwegs("POST", "/api/v1/unlock", `{"name":"lea","secret":"4711"}`)
	if code != http.StatusOK || !strings.Contains(body, "token") {
		t.Fatalf("Name und PIN von unterwegs: %d %s", code, body)
	}

	// Über einen Reverse-Proxy (X-Forwarded-For) gilt dasselbe.
	req, _ := http.NewRequest("GET", h.srv.URL+"/api/v1/profiles", nil)
	req.Header.Set("X-Forwarded-For", "203.0.113.7")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("über Reverse-Proxy: %d", resp.StatusCode)
	}
}

// Ein PIN lässt sich nicht durchprobieren: nach drei Fehlversuchen wächst
// die Pause, und gleichzeitige Versuche kommen nicht durch.
func TestPinBremse(t *testing.T) {
	h := newHarness(t)
	acc, _ := h.st.CreateAccount(t.Context(), store.NewAccount{Name: "Lea", LockKind: "pin", Secret: "4711"})
	versuch := func(pin string) int {
		resp, _ := h.do("POST", "/api/v1/unlock", `{"profileId":"`+acc.ID+`","secret":"`+pin+`"}`, "")
		return resp.StatusCode
	}
	for i := 0; i < 3; i++ {
		if c := versuch("0000"); c != http.StatusUnauthorized {
			t.Fatalf("Fehlversuch %d: %d", i+1, c)
		}
	}
	// Jetzt ist Pause — auch der richtige PIN wartet.
	if c := versuch("4711"); c != http.StatusTooManyRequests {
		t.Fatalf("nach drei Fehlversuchen: %d, erwartet 429", c)
	}

	// Gleichzeitig: von 20 Versuchen läuft höchstens einer.
	h2 := newHarness(t)
	acc2, _ := h2.st.CreateAccount(t.Context(), store.NewAccount{Name: "Ben", LockKind: "pin", Secret: "4711"})
	var mu sync.Mutex
	geprueft := 0
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, _ := h2.do("POST", "/api/v1/unlock", `{"profileId":"`+acc2.ID+`","secret":"1111"}`, "")
			if resp.StatusCode == http.StatusUnauthorized {
				mu.Lock()
				geprueft++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if geprueft > 3 {
		t.Fatalf("%d gleichzeitige Versuche wurden geprüft", geprueft)
	}
}

func TestPause(t *testing.T) {
	if pause(2) != 0 || pause(3) != 30*time.Second || pause(5) != 2*time.Minute || pause(40) != time.Hour {
		t.Fatalf("Pausen: %v %v %v %v", pause(2), pause(3), pause(5), pause(40))
	}
}

// Wege ohne Anmeldung nehmen nur bekannte Hostnamen an (DNS-Rebinding).
func TestFremderHostname(t *testing.T) {
	h := newHarness(t)
	for host, erlaubt := range map[string]bool{
		"192.168.1.20:8097": true, "nas:8097": true, "nas.local": true, "[fd00::1]:8097": true,
		"localhost:8097": true, "boese.example.com": false, "rebind.attacker.net:8097": false,
	} {
		req := httptest.NewRequest("GET", "/api/v1/profiles", nil)
		req.Host = host
		rec := httptest.NewRecorder()
		h.api.Handler().ServeHTTP(rec, req)
		if got := rec.Code != http.StatusMisdirectedRequest; got != erlaubt {
			t.Errorf("%s: %d", host, rec.Code)
		}
	}
	t.Setenv("LUMEN_HOSTS", "kino.example.com")
	req := httptest.NewRequest("GET", "/api/v1/profiles", nil)
	req.Host = "kino.example.com"
	rec := httptest.NewRecorder()
	h.api.Handler().ServeHTTP(rec, req)
	if rec.Code == http.StatusMisdirectedRequest {
		t.Error("LUMEN_HOSTS wird nicht beachtet")
	}
}

// Ein Profil darf keinen erfundenen Sperr-Typ oder ein fremdes Bild tragen
// — beides landet in fremden Browsern.
func TestEinladungMitSkript(t *testing.T) {
	h := newHarness(t)
	_, raw := h.do("POST", "/api/v1/admin/invites", `{}`, h.tok)
	var inv struct{ Code string }
	_ = json.Unmarshal(raw, &inv)
	resp, body := h.do("POST", "/api/v1/invite/redeem",
		`{"code":"`+inv.Code+`","name":"Gast","lockKind":"\"><img src=x onerror=alert(1)>","secret":"12345678"}`, "")
	if resp.StatusCode == http.StatusOK {
		t.Fatalf("Einladung mit Skript als Sperre angenommen: %s", body)
	}
	// Die Einladung ist danach noch gültig.
	resp, body = h.do("POST", "/api/v1/invite/redeem",
		`{"code":"`+inv.Code+`","name":"Gast","lockKind":"pin","secret":"1234"}`, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Einladung danach: %d %s", resp.StatusCode, body)
	}
	// Und genau einmal.
	resp, _ = h.do("POST", "/api/v1/invite/redeem",
		`{"code":"`+inv.Code+`","name":"Gast2","lockKind":"pin","secret":"1234"}`, "")
	if resp.StatusCode == http.StatusOK {
		t.Fatal("Einladung zweimal eingelöst")
	}
}
