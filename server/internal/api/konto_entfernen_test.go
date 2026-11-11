// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"errors"
	"testing"

	"lumen/internal/store"
)

// Ein Gast lässt sich wieder aus dem Haushalt nehmen — vom Besitzer,
// bestätigt. Nicht der Besitzer selbst, nicht von einem Mitverwalter.
func TestKontoEntfernen(t *testing.T) {
	h := newHarness(t)
	gastTok := h.gast("Gast", false)
	gast := h.idVon(gastTok)

	// Ein Mitverwalter darf das nicht (nur Besitzerrechte).
	mvTok, _ := h.mitverwalter("Mitverwalter")
	if resp, _ := h.do("DELETE", "/api/v1/admin/accounts/"+gast, "", mvTok); resp.StatusCode < 400 {
		t.Fatalf("Mitverwalter entfernt ein Konto: %d", resp.StatusCode)
	}
	// Ohne Bestätigung nicht.
	h.ohneBestaetigung = true
	if resp, _ := h.do("DELETE", "/api/v1/admin/accounts/"+gast, "", h.tok); resp.StatusCode != 428 {
		t.Fatalf("ohne Bestätigung: %d, erwartet 428", resp.StatusCode)
	}
	h.ohneBestaetigung = false
	// Das eigene Konto nicht.
	if resp, _ := h.do("DELETE", "/api/v1/admin/accounts/"+h.ownerID(), "", h.tok); resp.StatusCode != 409 {
		t.Fatalf("eigenes Konto: %d, erwartet 409", resp.StatusCode)
	}
	if resp, raw := h.do("DELETE", "/api/v1/admin/accounts/"+gast, "", h.tok); resp.StatusCode != 204 {
		t.Fatalf("entfernen: %d %s", resp.StatusCode, raw)
	}
	if _, err := h.st.Account(t.Context(), gast); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("das Konto gibt es noch: %v", err)
	}
	// Seine Geräte sind abgemeldet.
	if resp, _ := h.do("GET", "/api/v1/me", "", gastTok); resp.StatusCode != 401 {
		t.Errorf("der Gast ist noch angemeldet: %d", resp.StatusCode)
	}
}
