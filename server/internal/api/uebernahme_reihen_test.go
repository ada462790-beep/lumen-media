// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"lumen/internal/uebernahme/nachbau"
)

// Sammlungen aus Emby: Was fehlt, wird angelegt, was es schon gibt,
// ergänzt; vorher zeigt die Vorschau, welche fehlen. Nur die Verwaltung.
func TestUmzugUebernimmtReihen(t *testing.T) {
	h := newHarness(t)
	ids := map[string]string{}
	for i, name := range []string{"Matrix", "Matrix Reloaded", "Matrix Revolutions", "Heat", "Ronin",
		"Rocky", "Rocky II", "Creed", "Solo"} {
		id, _ := h.film(name, 1)
		_ = h.st.SetSourceID(t.Context(), id, "tmdb", fmt.Sprint(1000+i))
		ids[name] = id
	}
	ctx := t.Context()
	_ = h.st.TMDbReiheZuordnen(ctx, "2344", "Matrix Filmreihe", ids["Matrix"], 1999)
	_ = h.st.TMDbReiheZuordnen(ctx, "2344", "Matrix Filmreihe", ids["Matrix Reloaded"], 2003)
	if _, err := h.st.ReihenNeuBilden(ctx); err != nil {
		t.Fatal(err)
	}

	tm := func(name string) map[string]string {
		for i, n := range []string{"Matrix", "Matrix Reloaded", "Matrix Revolutions", "Heat", "Ronin",
			"Rocky", "Rocky II", "Creed", "Solo"} {
			if n == name {
				return map[string]string{"Tmdb": fmt.Sprint(1000 + i)}
			}
		}
		return nil
	}
	n := nachbau.Neu("Emby")
	defer n.Close()
	for _, name := range []string{"Matrix", "Matrix Reloaded", "Matrix Revolutions", "Heat", "Ronin", "Rocky", "Rocky II", "Creed", "Solo"} {
		n.Alle = append(n.Alle, nachbau.Film("e-"+name, name, 2000, "/emby/"+name+".mkv", 1, tm(name), map[string]any{}))
	}
	n.Alle = append(n.Alle, nachbau.Film("e-fremd", "Fremd", 2000, "/emby/fremd.mkv", 1, map[string]string{"Tmdb": "1"}, map[string]any{}))
	n.Reihen = []nachbau.Titel{
		nachbau.Reihe("b1", "The Matrix Collection", map[string]string{"Tmdb": "2344"}),
		nachbau.Reihe("b2", "Lieblingsfilme", nil),
		nachbau.Reihe("b3", "Einzel", nil),
		nachbau.Reihe("b4", "Nicht in Lumen", nil),
	}
	n.ReihenInhalt = map[string][]string{
		"b1": {"e-Matrix", "e-Matrix Reloaded", "e-Matrix Revolutions"},
		"b2": {"e-Heat", "e-Ronin"},
		"b3": {"e-Solo"},
		"b4": {"e-fremd"},
	}
	// Rocky: Lumen hat die Reihe am Titel erkannt (wenn ja, wird sie
	// ergänzt), sonst fehlt sie.
	n.Reihen = append(n.Reihen, nachbau.Reihe("b5", "Rocky Collection", nil))
	n.ReihenInhalt["b5"] = []string{"e-Rocky", "e-Rocky II", "e-Creed"}

	// Ohne Verwaltung keine Reihen.
	gtok := h.gast("Mara", false, h.lib.ID)
	if resp, raw := h.do("POST", "/api/v1/me/uebernahme", `{"adresse":"`+n.Adresse()+`","benutzer":"alex","passwort":"geheim","reihen":true}`, gtok); resp.StatusCode != 403 {
		t.Fatalf("Gast darf Reihen übernehmen: %d %s", resp.StatusCode, raw)
	}

	resp, raw := h.do("POST", "/api/v1/me/uebernahme", `{"adresse":"`+n.Adresse()+`","benutzer":"alex","passwort":"geheim","reihen":true}`, h.tok)
	if resp.StatusCode != 202 {
		t.Fatalf("starten: %d %s", resp.StatusCode, raw)
	}
	st := h.warteAufUebernahme(h.tok)
	if st["phase"] != "bereit" {
		t.Fatalf("Stand: %v", st)
	}
	r := st["reihen"].(map[string]any)
	namen := func(k string) []string {
		var out []string
		for _, x := range r[k].([]any) {
			out = append(out, x.(map[string]any)["name"].(string))
		}
		sort.Strings(out)
		return out
	}
	fehlen, ergaenzen := strings.Join(namen("fehlen"), ","), strings.Join(namen("ergaenzen"), ",")
	if !strings.Contains(fehlen, "Lieblingsfilme") || !strings.Contains(ergaenzen, "Matrix Filmreihe") ||
		r["einFilm"].(float64) != 1 || r["ohneFilme"].(float64) != 1 || r["gesamt"].(float64) != 5 {
		t.Fatalf("Vorschau: %v", r)
	}
	if strings.Contains(fehlen, "Matrix") {
		t.Fatalf("Matrix doppelt angelegt: %v", r)
	}
	if !strings.Contains(fehlen, "Rocky") && !strings.Contains(ergaenzen, "Rocky") {
		t.Fatalf("Rocky fehlt: %v", r)
	}

	resp, raw = h.do("POST", "/api/v1/me/uebernahme/anwenden", `{"reihen":true}`, h.tok)
	if resp.StatusCode != 200 {
		t.Fatalf("anwenden: %d %s", resp.StatusCode, raw)
	}
	if !strings.Contains(string(raw), `"reihenErgebnis"`) {
		t.Fatalf("kein Ergebnis: %s", raw)
	}
	reihe, _ := h.st.ReiheVon(ctx, ids["Matrix Revolutions"])
	if reihe == nil || reihe.Name != "Matrix Filmreihe" || len(reihe.ItemIDs) != 3 {
		t.Fatalf("Matrix nicht ergänzt: %+v", reihe)
	}
	reihe, _ = h.st.ReiheVon(ctx, ids["Heat"])
	if reihe == nil || reihe.Name != "Lieblingsfilme" || reihe.Source != "emby" || len(reihe.ItemIDs) != 2 {
		t.Fatalf("Lieblingsfilme nicht angelegt: %+v", reihe)
	}
	reihe, _ = h.st.ReiheVon(ctx, ids["Creed"])
	if reihe == nil || len(reihe.ItemIDs) != 3 || reihe.Source != "emby" {
		t.Fatalf("Rocky: %+v", reihe)
	}
	if reihe, _ := h.st.ReiheVon(ctx, ids["Solo"]); reihe != nil {
		t.Fatalf("Reihe mit einem Film angelegt: %+v", reihe)
	}
	// Ein Durchlauf nimmt die übernommenen Reihen nicht auseinander, und
	// keine erratene Rocky-Reihe kommt doppelt zurück.
	if _, err := h.st.ReihenNeuBilden(ctx); err != nil {
		t.Fatal(err)
	}
	alle, _ := h.st.Reihen(ctx, false)
	rocky := 0
	for _, x := range alle {
		if strings.Contains(x.Name, "Rocky") && len(x.ItemIDs) > 0 {
			rocky++
		}
	}
	if rocky != 1 {
		t.Fatalf("Rocky-Reihen: %d (%+v)", rocky, alle)
	}
	if reihe, _ := h.st.ReiheVon(ctx, ids["Ronin"]); reihe == nil || reihe.Name != "Lieblingsfilme" {
		t.Fatalf("nach dem Neubilden: %+v", reihe)
	}
}
