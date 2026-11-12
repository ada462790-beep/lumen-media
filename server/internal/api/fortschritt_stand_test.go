// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"fmt"
	"net/url"
	"testing"
)

// Ein nachgereichter Stand gilt nur, wenn seither niemand an diesem Titel
// etwas geändert hat: Er überschreibt nicht, was ein anderes Gerät
// inzwischen gemeldet hat, und kommt nach „Verlauf löschen" nicht zurück.
// Der Marker ist zufällig — keine Uhrzeit, keine Reihenfolge.
func TestNachgereichterStandNurMitPassendemMarker(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()
	film, _ := h.film("Matrix", 1)
	melden := func(pos int, basis *string) (string, bool) {
		body := fmt.Sprintf(`{"positionMs":%d,"durationMs":600000`, pos)
		if basis != nil {
			body += fmt.Sprintf(`,"basis":%q`, *basis)
		}
		resp, raw := h.do("POST", "/api/v1/items/"+film+"/progress", body+"}", h.tok)
		if resp.StatusCode != 204 {
			t.Fatalf("melden: %d %s", resp.StatusCode, raw)
		}
		return resp.Header.Get("X-Lumen-Stand"), resp.Header.Get("X-Lumen-Veraltet") == ""
	}
	stelle := func() int64 {
		p, err := h.st.Progress(ctx, h.ownerID(), film)
		if err != nil {
			return -1
		}
		return p.PositionMS
	}
	s1, galt := melden(60_000, nil)
	if !galt || len(s1) != 16 {
		t.Fatalf("erste Meldung: %q %v", s1, galt)
	}
	// Ein anderes Gerät meldet weiter.
	s2, _ := melden(300_000, nil)
	if s2 == s1 {
		t.Fatal("Marker blieb gleich")
	}
	// Das erste Gerät reicht seinen alten Stand nach: verworfen.
	if s, galt := melden(90_000, &s1); galt || s != s2 || stelle() != 300_000 {
		t.Fatalf("veralteter Stand überschrieb: %q %v %d", s, galt, stelle())
	}
	// Mit dem passenden Marker gilt er.
	if _, galt := melden(320_000, &s2); !galt || stelle() != 320_000 {
		t.Fatalf("passender Marker verworfen: %d", stelle())
	}
	// Verlauf gelöscht: Nichts, was nachgereicht wird, kommt zurück.
	s3 := h.st.FortschrittStand(ctx, h.ownerID(), film)
	if resp, _ := h.do("DELETE", "/api/v1/me/progress", "", h.tok); resp.StatusCode >= 300 {
		t.Fatalf("Verlauf löschen: %d", resp.StatusCode)
	}
	if _, galt := melden(330_000, &s3); galt || stelle() != -1 {
		t.Fatalf("nach dem Löschen zurückgekommen: %d", stelle())
	}
	// Auch ein nachgereichter Haken.
	resp, _ := h.do("POST", "/api/v1/items/"+film+"/finished?basis="+url.QueryEscape(s3), "", h.tok)
	if resp.StatusCode != 204 || resp.Header.Get("X-Lumen-Veraltet") != "1" || stelle() != -1 {
		t.Fatalf("nachgereichter Haken nach dem Löschen: %d %v", resp.StatusCode, resp.Header)
	}
}
