// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"net/http"
	"strings"
	"testing"

	"lumen/internal/store"
)

// mitverwalter legt ein Profil mit Verwaltung (ohne Vollzugriff) an und
// gibt ein Token samt Bestätigungscode zurück.
func (h *harness) mitverwalter(name string) (tok, code string) {
	h.t.Helper()
	ctx := h.t.Context()
	acc, err := h.st.CreateAccount(ctx, store.NewAccount{Name: name, LockKind: "pin", Secret: "2468"})
	if err != nil {
		h.t.Fatal(err)
	}
	if err := h.st.RolleSetzen(ctx, acc.ID, "verwalten"); err != nil {
		h.t.Fatal(err)
	}
	tok, _ = h.st.IssueToken(ctx, acc.ID, "Mac", "desktop")
	return tok, "2468"
}

// Besitzer-Einstellungen ändert ein Mitverwalter nicht: OpenSubtitles
// (Schlüssel, Konto). Lesen darf er, aber ohne den Benutzernamen des
// OpenSubtitles-Kontos.
func TestMitverwalterNurLesend(t *testing.T) {
	h := newHarness(t)
	if resp, raw := h.do("PUT", "/api/v1/admin/untertitel", `{"benutzer":"alex-os"}`, h.tok); resp.StatusCode != 200 {
		t.Fatalf("Besitzer setzt: %d %s", resp.StatusCode, raw)
	}
	mtok, _ := h.mitverwalter("Jonas")
	for _, weg := range []struct{ methode, pfad, rumpf string }{
		{"PUT", "/api/v1/admin/untertitel", `{"benutzer":"jonas"}`},
		{"POST", "/api/v1/admin/untertitel", `{"benutzer":"jonas"}`},
	} {
		if resp, _ := h.do(weg.methode, weg.pfad, weg.rumpf, mtok); resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s %s für Mitverwalter: %d", weg.methode, weg.pfad, resp.StatusCode)
		}
	}
	resp, raw := h.do("GET", "/api/v1/admin/untertitel", "", mtok)
	if resp.StatusCode != 200 || strings.Contains(string(raw), "alex-os") || !strings.Contains(string(raw), `"darfAendern":false`) {
		t.Fatalf("Mitverwalter liest: %d %s", resp.StatusCode, raw)
	}
	_, raw = h.do("GET", "/api/v1/admin/untertitel", "", h.tok)
	if !strings.Contains(string(raw), "alex-os") || !strings.Contains(string(raw), `"darfAendern":true`) {
		t.Fatalf("Besitzer liest: %s", raw)
	}
}

// Verwaltung nur am Desktop: Ein Token, das sich als Handy, Tablet oder
// Fernseher angemeldet hat, kommt auch beim Besitzer nicht hinein. Was
// der Person selbst gehört, bleibt auf jedem Gerät.
func TestVerwaltungNurDesktop(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()
	besitzer := h.ownerID()
	for _, art := range []string{"phone", "tablet", "tv"} {
		tok, _ := h.st.IssueToken(ctx, besitzer, "Gerät", art)
		req, _ := http.NewRequest("GET", h.srv.URL+"/api/v1/admin/overview", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s öffnet die Verwaltung: %d", art, resp.StatusCode)
		}
		if resp, _ := h.do("GET", "/api/v1/me/devices", "", tok); resp.StatusCode != 200 {
			t.Errorf("%s: eigene Geräte nicht erreichbar: %d", art, resp.StatusCode)
		}
	}
	for _, art := range []string{"desktop", "browser"} {
		tok, _ := h.st.IssueToken(ctx, besitzer, "Rechner", art)
		req, _ := http.NewRequest("GET", h.srv.URL+"/api/v1/admin/overview", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("%s ausgesperrt: %d", art, resp.StatusCode)
		}
	}
}
