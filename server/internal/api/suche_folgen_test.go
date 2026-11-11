// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"net/url"
	"testing"
)

// Die Suche findet Folgen über ihren Titel — und nur solche, die das
// Profil sehen darf.
func TestSucheFindetFolgen(t *testing.T) {
	h := newHarness(t)
	serie, _, folgen := h.serie()
	ctx := t.Context()
	f, _ := h.st.Item(ctx, folgen[1])
	f.Title = "Der Bunker im Wald"
	if err := h.st.UpsertItem(ctx, f); err != nil {
		t.Fatal(err)
	}
	suche := func(tok, q string) []string {
		_, raw := h.do("GET", "/api/v1/search?q="+url.QueryEscape(q), "", tok)
		var d struct {
			Folgen []struct{ ID, SeriesTitle string } `json:"folgen"`
		}
		_ = json.Unmarshal(raw, &d)
		var out []string
		for _, x := range d.Folgen {
			out = append(out, x.ID+"|"+x.SeriesTitle)
		}
		return out
	}
	if got := suche(h.tok, "bunker"); len(got) != 1 || got[0] != f.ID+"|Tiefsee" {
		t.Errorf("Folge nicht gefunden: %v", got)
	}
	// Zu kurz: keine Folgen (sonst passt „de" auf jede zweite Folge).
	if got := suche(h.tok, "bu"); len(got) != 0 {
		t.Errorf("zwei Buchstaben finden Folgen: %v", got)
	}
	// Ein Kinderprofil bis 6 sieht die Folge einer Serie ab 16 nicht.
	s, _ := h.st.Item(ctx, serie)
	sechzehn := 16
	s.AgeRating, s.AgeYears = "16", &sechzehn
	_ = h.st.UpsertItem(ctx, s)
	kind := h.gast("Kind", true, h.lib.ID)
	kid := h.idVon(kind)
	lim, _ := h.st.Limits(ctx, kid)
	lim.MaxAge = 6
	_ = h.st.SetLimits(ctx, kid, lim)
	if got := suche(kind, "bunker"); len(got) != 0 {
		t.Errorf("Kinderprofil findet die Folge einer Serie ab 16: %v", got)
	}
}
