// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"lumen/internal/store"
	"lumen/internal/totp"
)

// Ein Konto in einer Gruppe mit Verwaltung kommt in die Verwaltung — aber
// nicht an die Gruppen selbst. Und es sieht genauso wenig wie der
// Besitzer, wer was schaut.
func TestGruppeMitVerwaltung(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()
	lib, _ := h.st.CreateLibrary(ctx, "Filme", "movie", h.root, "de")
	jonas, _ := h.st.CreateAccount(ctx, store.NewAccount{Name: "Jonas", LockKind: "pin", Secret: "1357"})
	jtok, _ := h.st.IssueToken(ctx, jonas.ID, "Mac", "desktop")

	// Ohne Gruppe: nichts.
	resp, _ := h.do("GET", "/api/v1/admin/overview", "", jtok)
	if resp.StatusCode != 404 {
		t.Fatalf("ohne Gruppe: %d", resp.StatusCode)
	}

	// Besitzer legt die Gruppe an und steckt Jonas hinein.
	resp, raw := h.do("POST", "/api/v1/admin/groups",
		fmt.Sprintf(`{"name":"Mitverwalter","verwaltung":true,"libraries":[%q],"limits":{"maxStreams":3,"maxAge":18,"allowUnrated":true}}`, lib.ID), h.tok)
	if resp.StatusCode != 201 {
		t.Fatalf("Gruppe anlegen: %d %s", resp.StatusCode, raw)
	}
	var g store.Gruppe
	_ = json.Unmarshal(raw, &g)
	resp, raw = h.do("PUT", "/api/v1/admin/accounts/"+jonas.ID+"/group", `{"groupId":"`+g.ID+`"}`, h.tok)
	if resp.StatusCode != 204 {
		t.Fatalf("zuweisen: %d %s", resp.StatusCode, raw)
	}

	// Jetzt darf Jonas in die Verwaltung …
	resp, raw = h.do("GET", "/api/v1/admin/overview", "", jtok)
	if resp.StatusCode != 200 {
		t.Fatalf("mit Gruppe: %d %s", resp.StatusCode, raw)
	}
	if strings.Contains(string(raw), "Jonas") || strings.Contains(string(raw), "history") {
		t.Error("die Übersicht verrät etwas über Personen")
	}
	// … aber nicht an die Gruppen.
	resp, _ = h.do("POST", "/api/v1/admin/groups", `{"name":"Noch eine","verwaltung":true}`, jtok)
	if resp.StatusCode != 404 {
		t.Errorf("Mitverwalter legt Gruppe an: %d", resp.StatusCode)
	}
	resp, _ = h.do("PUT", "/api/v1/admin/accounts/"+jonas.ID+"/group", `{"groupId":""}`, jtok)
	if resp.StatusCode != 404 {
		t.Errorf("Mitverwalter ändert Zuweisung: %d", resp.StatusCode)
	}
	// Auch Updates gehören dem Besitzer.
	resp, _ = h.do("GET", "/api/v1/admin/update", "", jtok)
	if resp.StatusCode != 404 {
		t.Errorf("Mitverwalter sieht Updates: %d", resp.StatusCode)
	}

	// Die Grenzen und Bibliotheken kommen aus der Gruppe.
	libs, _ := h.st.GrantedLibraries(ctx, jonas.ID)
	if len(libs) != 1 || libs[0] != lib.ID {
		t.Errorf("Bibliotheken aus der Gruppe: %v", libs)
	}
	// Grenzen gibt es für Verwaltende keine (store.VerwaltungsLimits).
	lim, _ := h.st.Limits(ctx, jonas.ID)
	if lim.MaxStreams != 20 || lim.RemoteQuality != "original" {
		t.Errorf("Mitverwalter hat Grenzen: %+v", lim)
	}
	// /me sagt, dass Jonas verwalten darf — und nicht, dass er Besitzer ist.
	_, raw = h.do("GET", "/api/v1/me", "", jtok)
	if !strings.Contains(string(raw), `"isAdmin":true`) || strings.Contains(string(raw), `"isOwner":true`) {
		t.Errorf("me: %s", raw)
	}

	// Gruppe weg: Jonas ist wieder draussen, seine eigenen Einträge gelten.
	resp, _ = h.do("DELETE", "/api/v1/admin/groups/"+g.ID, "", h.tok)
	if resp.StatusCode != 204 {
		t.Fatalf("löschen: %d", resp.StatusCode)
	}
	resp, _ = h.do("GET", "/api/v1/admin/overview", "", jtok)
	if resp.StatusCode != 404 {
		t.Errorf("nach dem Auflösen: %d", resp.StatusCode)
	}
	if libs, _ = h.st.GrantedLibraries(ctx, jonas.ID); len(libs) != 0 {
		t.Errorf("eigene Bibliotheken nach dem Auflösen: %v", libs)
	}
}

// Die Verwaltungssperre: mit PIN ist die Verwaltung zu, bis man entsperrt;
// das Token gilt nur für diese Person; nach dem Sperren ist wieder zu.
func TestVerwaltungssperreMitPIN(t *testing.T) {
	h := newHarness(t)
	// Sperre setzen — die Antwort bringt gleich ein Token mit.
	resp, raw := h.do("PUT", "/api/v1/admin/lock", `{"kind":"pin","secret":"2468"}`, h.tok)
	if resp.StatusCode != 200 {
		t.Fatalf("Sperre setzen: %d %s", resp.StatusCode, raw)
	}
	// Ohne Token: 423 mit der Art der Sperre.
	resp, raw = h.do("GET", "/api/v1/admin/overview", "", h.tok)
	if resp.StatusCode != http.StatusLocked || !strings.Contains(string(raw), `"sperre":"pin"`) {
		t.Fatalf("gesperrt: %d %s", resp.StatusCode, raw)
	}
	// Falscher PIN.
	resp, _ = h.do("POST", "/api/v1/admin/unlock", `{"code":"0000"}`, h.tok)
	if resp.StatusCode != 403 {
		t.Fatalf("falscher PIN: %d", resp.StatusCode)
	}
	// Richtiger PIN → Token.
	resp, raw = h.do("POST", "/api/v1/admin/unlock", `{"code":"2468"}`, h.tok)
	if resp.StatusCode != 200 {
		t.Fatalf("entsperren: %d %s", resp.StatusCode, raw)
	}
	var out struct{ Token string }
	_ = json.Unmarshal(raw, &out)
	req, _ := http.NewRequest("GET", h.srv.URL+"/api/v1/admin/overview", nil)
	req.Header.Set("Authorization", "Bearer "+h.tok)
	req.Header.Set(kopfVerwaltung, out.Token)
	r2, _ := http.DefaultClient.Do(req)
	if r2.StatusCode != 200 {
		t.Fatalf("mit Token: %d", r2.StatusCode)
	}
	r2.Body.Close()
	// Sperren: Token weg.
	h.do("POST", "/api/v1/admin/relock", "", h.tok)
	r3, _ := http.DefaultClient.Do(req)
	if r3.StatusCode != http.StatusLocked {
		t.Fatalf("nach dem Sperren: %d", r3.StatusCode)
	}
	r3.Body.Close()
	// Fünf Fehlversuche bremsen.
	for i := 0; i < 5; i++ {
		h.do("POST", "/api/v1/admin/unlock", `{"code":"0000"}`, h.tok)
	}
	resp, _ = h.do("POST", "/api/v1/admin/unlock", `{"code":"2468"}`, h.tok)
	if resp.StatusCode != 429 {
		t.Errorf("Bremse: %d", resp.StatusCode)
	}
}

// Authenticator-App: QR holen, Code bestätigen, dann gilt nur der Code.
func TestVerwaltungssperreMitApp(t *testing.T) {
	h := newHarness(t)
	resp, raw := h.do("POST", "/api/v1/admin/lock/totp", "", h.tok)
	if resp.StatusCode != 200 {
		t.Fatalf("QR: %d %s", resp.StatusCode, raw)
	}
	var qr struct{ Geheimnis, QR string }
	_ = json.Unmarshal(raw, &qr)
	if qr.Geheimnis == "" || !strings.HasPrefix(qr.QR, "data:image/png;base64,") {
		t.Fatalf("QR-Antwort: %s", raw[:60])
	}
	// Falscher Code: nichts gespeichert.
	resp, _ = h.do("PUT", "/api/v1/admin/lock", `{"kind":"totp","code":"000000"}`, h.tok)
	if resp.StatusCode != 403 {
		t.Fatalf("falscher Code beim Einrichten: %d", resp.StatusCode)
	}
	if k, _ := h.st.VerwaltungsSperre(t.Context(), h.ownerID()); k != "none" {
		t.Fatalf("Sperre nach falschem Code: %s", k)
	}
	code, _ := totp.Code(qr.Geheimnis, time.Now())
	resp, raw = h.do("PUT", "/api/v1/admin/lock", `{"kind":"totp","code":"`+code+`"}`, h.tok)
	if resp.StatusCode != 200 {
		t.Fatalf("einrichten: %d %s", resp.StatusCode, raw)
	}
	resp, _ = h.do("GET", "/api/v1/admin/overview", "", h.tok)
	if resp.StatusCode != http.StatusLocked {
		t.Fatalf("gesperrt: %d", resp.StatusCode)
	}
	code, _ = totp.Code(qr.Geheimnis, time.Now())
	resp, raw = h.do("POST", "/api/v1/admin/unlock", `{"code":"`+code+`"}`, h.tok)
	if resp.StatusCode != 200 {
		t.Fatalf("entsperren mit App: %d %s", resp.StatusCode, raw)
	}
	// Das Geheimnis taucht nirgends in den Konten auf.
	_, raw = h.do("GET", "/api/v1/me", "", h.tok)
	if strings.Contains(string(raw), qr.Geheimnis) {
		t.Error("das Geheimnis steht in /me")
	}
}

func (h *harness) ownerID() string {
	accs, _ := h.st.Accounts(h.t.Context())
	for _, a := range accs {
		if a.IsOwner {
			return a.ID
		}
	}
	return ""
}

// Issue #6: Rechte ändern verlangt den Code noch einmal — auch bei offener
// Verwaltung. Ohne: 428, falsch: 403, richtig: durch.
func TestRechteAendernVerlangtBestaetigung(t *testing.T) {
	h := newHarness(t)
	h.ohneBestaetigung = true
	resp, raw := h.do("POST", "/api/v1/admin/groups", `{"name":"Kinder"}`, h.tok)
	if resp.StatusCode != 428 || !strings.Contains(string(raw), "bestaetigen") {
		t.Fatalf("ohne Bestätigung: %d %s", resp.StatusCode, raw)
	}
	req, _ := http.NewRequest("POST", h.srv.URL+"/api/v1/admin/groups", strings.NewReader(`{"name":"Kinder"}`))
	req.Header.Set("Authorization", "Bearer "+h.tok)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Lumen-Bestaetigung", "falsch")
	r2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	r2.Body.Close()
	if r2.StatusCode != 403 {
		t.Fatalf("falscher Code: %d", r2.StatusCode)
	}
	h.ohneBestaetigung = false
	if resp, raw := h.do("POST", "/api/v1/admin/groups", `{"name":"Kinder"}`, h.tok); resp.StatusCode != 201 {
		t.Fatalf("mit Bestätigung: %d %s", resp.StatusCode, raw)
	}
}

// Vollzugriff: alle Rechte wie der Besitzer — Gruppen, Updates, alle
// Bibliotheken. Nur das Konto des Besitzers selbst bleibt unantastbar.
func TestVollzugriffWieDerBesitzer(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()
	lib, _ := h.st.CreateLibrary(ctx, "Filme", "movie", h.root, "de")
	_ = lib
	mia, _ := h.st.CreateAccount(ctx, store.NewAccount{Name: "Mia", LockKind: "pin", Secret: "1357"})
	mtok, _ := h.st.IssueToken(ctx, mia.ID, "Mac", "desktop")

	// Ohne Rechte: keine Verwaltung.
	if resp, _ := h.do("GET", "/api/v1/admin/overview", "", mtok); resp.StatusCode != 404 {
		t.Fatalf("ohne Rechte: %d", resp.StatusCode)
	}
	// Mitverwalten: Verwaltung ja, Updates nein.
	if resp, raw := h.do("PUT", "/api/v1/admin/accounts/"+mia.ID+"/rolle", `{"rolle":"verwalten"}`, h.tok); resp.StatusCode != 204 {
		t.Fatalf("verwalten: %d %s", resp.StatusCode, raw)
	}
	if resp, _ := h.do("GET", "/api/v1/admin/overview", "", mtok); resp.StatusCode != 200 {
		t.Fatalf("mitverwalten, Übersicht: %d", resp.StatusCode)
	}
	if resp, _ := h.do("GET", "/api/v1/admin/update", "", mtok); resp.StatusCode != 404 {
		t.Fatalf("mitverwalten sieht Updates: %d", resp.StatusCode)
	}
	// Vollzugriff: auch Updates und Gruppen.
	if resp, raw := h.do("PUT", "/api/v1/admin/accounts/"+mia.ID+"/rolle", `{"rolle":"voll"}`, h.tok); resp.StatusCode != 204 {
		t.Fatalf("voll: %d %s", resp.StatusCode, raw)
	}
	if resp, raw := h.do("GET", "/api/v1/admin/update", "", mtok); resp.StatusCode != 200 {
		t.Fatalf("Vollzugriff, Updates: %d %s", resp.StatusCode, raw)
	}
	if resp, raw := h.do("GET", "/api/v1/admin/groups", "", mtok); resp.StatusCode != 200 {
		t.Fatalf("Vollzugriff, Gruppen: %d %s", resp.StatusCode, raw)
	}
	acc, _ := h.st.Account(ctx, mia.ID)
	if !acc.Vollzugriff || acc.IsOwner {
		t.Fatalf("Konto: voll=%v owner=%v", acc.Vollzugriff, acc.IsOwner)
	}
	// Den Besitzer kann sie nicht in eine Gruppe stecken.
	owner, _ := h.st.Accounts(ctx)
	for _, o := range owner {
		if o.IsOwner {
			req, _ := http.NewRequest("PUT", h.srv.URL+"/api/v1/admin/accounts/"+o.ID+"/group", strings.NewReader(`{"groupId":"grp_mitverwalter"}`))
			req.Header.Set("Authorization", "Bearer "+mtok)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Lumen-Bestaetigung", "egal") // falscher Code
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode == 204 {
				t.Fatal("Vollzugriff konnte den Besitzer umhängen")
			}
		}
	}
	// Zurück auf „nur schauen".
	if resp, _ := h.do("PUT", "/api/v1/admin/accounts/"+mia.ID+"/rolle", `{"rolle":"schauen"}`, h.tok); resp.StatusCode != 204 {
		t.Fatal("schauen")
	}
	if resp, _ := h.do("GET", "/api/v1/admin/overview", "", mtok); resp.StatusCode != 404 {
		t.Fatalf("nach „schauen“ noch Verwaltung: %d", resp.StatusCode)
	}
}
