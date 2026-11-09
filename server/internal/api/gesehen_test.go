// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"testing"

	"lumen/internal/store"
)

// serie legt „Tiefsee" mit zwei Staffeln zu je drei Folgen an.
func (h *harness) serie() (serie string, staffeln []string, folgen []string) {
	h.t.Helper()
	ctx := h.t.Context()
	if h.lib == nil {
		h.film("Nordlicht", 1)
	}
	se := &store.Item{LibraryID: h.lib.ID, Kind: "series", Title: "Tiefsee", MatchState: "matched"}
	_ = h.st.UpsertItem(ctx, se)
	for s := 1; s <= 2; s++ {
		st := &store.Item{LibraryID: h.lib.ID, Kind: "season", ParentID: se.ID, Title: "Staffel", SeasonNo: s, MatchState: "matched"}
		_ = h.st.UpsertItem(ctx, st)
		staffeln = append(staffeln, st.ID)
		for f := 1; f <= 3; f++ {
			e := &store.Item{LibraryID: h.lib.ID, Kind: "episode", ParentID: st.ID, Title: "Folge", SeasonNo: s, EpisodeNo: f,
				MatchState: "matched", RuntimeMS: 2_400_000}
			_ = h.st.UpsertItem(ctx, e)
			folgen = append(folgen, e.ID)
		}
	}
	return se.ID, staffeln, folgen
}

func (h *harness) haken(tok string, ids []string) []bool {
	h.t.Helper()
	got, err := h.st.FinishedIDs(h.t.Context(), h.idVon(tok), ids)
	if err != nil {
		h.t.Fatal(err)
	}
	out := make([]bool, len(ids))
	for i, id := range ids {
		out[i] = got[id]
	}
	return out
}

func (h *harness) idVon(tok string) string {
	h.t.Helper()
	acc, _, err := h.st.LookupToken(h.t.Context(), tok)
	if err != nil {
		h.t.Fatal(err)
	}
	return acc.ID
}

func alle(b []bool, von, bis int, soll bool) bool {
	for i := von; i < bis; i++ {
		if b[i] != soll {
			return false
		}
	}
	return true
}

// „Als gesehen markieren" an einer Serie meint alle Folgen, mit
// ?staffel=N nur eine Staffel; an einer Folge mit ?vorherige=1 auch alle
// davor. Zurücknehmen geht genauso — und nur im eigenen Profil.
func TestGesehenFuerSerieStaffelUndVorherige(t *testing.T) {
	h := newHarness(t)
	serie, staffeln, f := h.serie()
	b := h.gast("Gast", false, h.lib.ID)

	if r, _ := h.do("POST", "/api/v1/items/"+serie+"/finished?staffel=2", "", h.tok); r.StatusCode != 204 {
		t.Fatalf("Staffel 2 markieren: %d", r.StatusCode)
	}
	if hk := h.haken(h.tok, f); !alle(hk, 0, 3, false) || !alle(hk, 3, 6, true) {
		t.Fatalf("nur Staffel 2 erwartet: %v", hk)
	}
	if hk := h.haken(b, f); !alle(hk, 0, 6, false) {
		t.Fatalf("Haken im fremden Profil: %v", hk)
	}
	if r, _ := h.do("POST", "/api/v1/items/"+serie+"/finished?staffel=x", "", h.tok); r.StatusCode != 400 {
		t.Fatalf("Unsinn als Staffel: %d", r.StatusCode)
	}

	// Die Serie ist noch nicht gesehen: drei Folgen fehlen.
	_, raw := h.do("GET", "/api/v1/items/"+serie, "", h.tok)
	var d struct {
		Item struct {
			Finished bool
			Gesehen  int
			Folgen   int
		}
	}
	_ = json.Unmarshal(raw, &d)
	if d.Item.Finished || d.Item.Gesehen != 3 || d.Item.Folgen != 6 {
		t.Fatalf("Serie halb gesehen: %s", raw)
	}

	// Staffel 2 über die Staffel selbst zurücknehmen.
	if r, _ := h.do("DELETE", "/api/v1/items/"+staffeln[1]+"/finished", "", h.tok); r.StatusCode != 204 {
		t.Fatalf("Staffel zurücknehmen: %d", r.StatusCode)
	}
	if hk := h.haken(h.tok, f); !alle(hk, 0, 6, false) {
		t.Fatalf("nach Zurücknehmen: %v", hk)
	}

	// Bis S2F2 alles gesehen.
	if r, _ := h.do("POST", "/api/v1/items/"+f[4]+"/finished?vorherige=1", "", h.tok); r.StatusCode != 204 {
		t.Fatalf("vorherige: %d", r.StatusCode)
	}
	if hk := h.haken(h.tok, f); !alle(hk, 0, 5, true) || hk[5] {
		t.Fatalf("bis S2F2 erwartet: %v", hk)
	}

	// Startseite: Weiterschauen bietet S2F3 an, mit Serienname; die Serie
	// steht nicht mehr unter „Noch nicht angefangen".
	_, raw = h.do("GET", "/api/v1/home", "", h.tok)
	var home struct {
		Rows []struct {
			Key   string
			Items []struct {
				ID            string
				SeriesTitle   string
				NaechsteFolge bool
			}
		}
	}
	_ = json.Unmarshal(raw, &home)
	gefunden := false
	for _, r := range home.Rows {
		if r.Key == "resume" {
			if len(r.Items) != 1 || r.Items[0].ID != f[5] || r.Items[0].SeriesTitle != "Tiefsee" || !r.Items[0].NaechsteFolge {
				t.Fatalf("Weiterschauen: %+v", r.Items)
			}
			gefunden = true
		}
		if r.Key == "unberuehrt" {
			for _, it := range r.Items {
				if it.ID == serie {
					t.Fatal("angefangene Serie unter „Noch nicht angefangen“")
				}
			}
		}
	}
	if !gefunden {
		t.Fatalf("keine Reihe Weiterschauen: %s", raw)
	}

	// Die Serienseite startet dieselbe Folge.
	_, raw = h.do("GET", "/api/v1/items/"+serie+"/weiterschauen", "", h.tok)
	var weiter struct{ Item struct{ ID string } }
	_ = json.Unmarshal(raw, &weiter)
	if weiter.Item.ID != f[5] {
		t.Fatalf("weiterschauen in der Serie: %s", raw)
	}
	// Für jemanden, der nie angefangen hat: die erste Folge.
	_, raw = h.do("GET", "/api/v1/items/"+serie+"/weiterschauen", "", b)
	_ = json.Unmarshal(raw, &weiter)
	if weiter.Item.ID != f[0] {
		t.Fatalf("weiterschauen ohne Stand: %s", raw)
	}

	// Die ganze Serie: alle Folgen; dann gilt sie als gesehen.
	if r, _ := h.do("POST", "/api/v1/items/"+serie+"/finished", "", h.tok); r.StatusCode != 204 {
		t.Fatalf("Serie: %d", r.StatusCode)
	}
	_, raw = h.do("GET", "/api/v1/items/"+serie, "", h.tok)
	_ = json.Unmarshal(raw, &d)
	if !d.Item.Finished {
		t.Fatalf("Serie nicht gesehen: %s", raw)
	}
	if r, _ := h.do("DELETE", "/api/v1/items/"+serie+"/finished", "", h.tok); r.StatusCode != 204 {
		t.Fatalf("Serie zurück: %d", r.StatusCode)
	}
	if hk := h.haken(h.tok, f); !alle(hk, 0, 6, false) {
		t.Fatalf("Serie zurück: %v", hk)
	}
}
