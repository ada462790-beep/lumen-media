// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"strings"
	"testing"
)

// Eine Serie ab 6 mit einer Folge ab 16: Das Kind mit 6 sieht diese Folge
// weder in der Staffelliste noch als „Weiter mit …". Früher stand sie
// samt Titel und „FSK 16" in der Liste, und ein Tippen endete in
// „Nicht gefunden".
func TestFolgeMitHoehererFreigabeBleibtVerborgen(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()
	serie, staffeln, folgen := h.serie()
	sechs, sechzehn := 6, 16
	se, _ := h.st.Item(ctx, serie)
	se.AgeRating, se.AgeYears = "FSK 6", &sechs
	if err := h.st.UpsertItem(ctx, se); err != nil {
		t.Fatal(err)
	}
	f2, _ := h.st.Item(ctx, folgen[1])
	f2.Title, f2.AgeRating, f2.AgeYears = "Gruselfolge", "FSK 16", &sechzehn
	if err := h.st.UpsertItem(ctx, f2); err != nil {
		t.Fatal(err)
	}
	kind := h.gast("Lena", true, h.lib.ID)
	lena := h.idVon(kind)
	lim, _ := h.st.Limits(ctx, lena)
	lim.MaxAge = 6
	_ = h.st.SetLimits(ctx, lena, lim)

	resp, raw := h.do("GET", "/api/v1/items/"+staffeln[0]+"/children", "", kind)
	if resp.StatusCode != 200 {
		t.Fatalf("Staffel: %d %s", resp.StatusCode, raw)
	}
	if strings.Contains(string(raw), "Gruselfolge") || strings.Contains(string(raw), folgen[1]) {
		t.Fatalf("Folge ab 16 in der Staffelliste: %s", raw)
	}
	if !strings.Contains(string(raw), folgen[0]) || !strings.Contains(string(raw), folgen[2]) {
		t.Fatalf("die übrigen Folgen fehlen: %s", raw)
	}
	// Der Besitzer sieht alle.
	_, raw = h.do("GET", "/api/v1/items/"+staffeln[0]+"/children", "", h.tok)
	if !strings.Contains(string(raw), "Gruselfolge") {
		t.Fatalf("Besitzer sieht die Folge nicht: %s", raw)
	}

	// Folge 1 zu Ende geschaut: „Weiter mit" springt über Folge 2 zu Folge 3.
	if err := h.st.MarkFinished(ctx, lena, folgen[0], 2_400_000); err != nil {
		t.Fatal(err)
	}
	resp, raw = h.do("GET", "/api/v1/items/"+serie+"/weiterschauen", "", kind)
	if resp.StatusCode != 200 || strings.Contains(string(raw), folgen[1]) || !strings.Contains(string(raw), folgen[2]) {
		t.Fatalf("Weiter mit: %d %s", resp.StatusCode, raw)
	}
	// Angefangen (über einen alten Stand) zählt auch nicht.
	_ = h.st.SaveProgress(ctx, lena, folgen[1], 600_000, 2_400_000)
	resp, raw = h.do("GET", "/api/v1/items/"+serie+"/weiterschauen", "", kind)
	if resp.StatusCode != 200 || strings.Contains(string(raw), folgen[1]) {
		t.Fatalf("Weiter mit nach Fortschritt: %d %s", resp.StatusCode, raw)
	}
}

// Steuerzeichen für die Schreibrichtung verlassen den Server nicht
// (Sicherheitsbericht N-3): Ein Titel „Evil ‮cod" käme sonst verdreht an.
func TestTitelOhneRichtungszeichen(t *testing.T) {
	h := newHarness(t)
	id, _ := h.film("Evil ‮cod⁦x⁩ — „gut“", 1)
	_, raw := h.do("GET", "/api/v1/items/"+id, "", h.tok)
	if strings.ContainsAny(string(raw), "‪‫‬‭‮⁦⁧⁨⁩") {
		t.Fatalf("Richtungszeichen in der Antwort: %q", raw)
	}
	if !strings.Contains(string(raw), "Evil codx — „gut“") {
		t.Fatalf("Titel beschädigt: %s", raw)
	}
}
