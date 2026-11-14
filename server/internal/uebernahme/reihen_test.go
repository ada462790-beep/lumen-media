// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package uebernahme

import "testing"

func TestReihenName(t *testing.T) {
	for _, n := range []string{"The Matrix Collection", "Matrix Filmreihe", "Matrix – Sammlung", "matrix"} {
		if ReihenName(n) != "matrix" {
			t.Fatalf("%q → %q", n, ReihenName(n))
		}
	}
	if ReihenName("Zurück in die Zukunft") != "zurueck in zukunft" {
		t.Fatalf("%q", ReihenName("Zurück in die Zukunft"))
	}
}

// Eine Reihe, die der Besitzer aufgelöst hat, kommt nicht zurück; eine
// mit denselben Filmen unter anderem Namen gilt als schon da.
func TestReihenPlanenAufgeloestUndGleicheFilme(t *testing.T) {
	ix := NeuerIndex([]LumenTitel{
		{ID: "a", Kind: "movie", Titel: "A", TMDb: "1"},
		{ID: "b", Kind: "movie", Titel: "B", TMDb: "2"},
		{ID: "c", Kind: "movie", Titel: "C", TMDb: "3"},
		{ID: "d", Kind: "movie", Titel: "D", TMDb: "4"},
	}, nil)
	film := func(id string) Eintrag { return Eintrag{Typ: "Movie", Kennungen: map[string]string{"tmdb": id}} }
	p := ReihenPlanen([]EmbyReihe{
		{Name: "Weg", Filme: []Eintrag{film("1"), film("2")}},
		{Name: "Anders benannt", Filme: []Eintrag{film("3"), film("4")}},
	}, ix, []LumenReihe{
		{ID: "r1", Name: "Weg", Hidden: true, ItemIDs: []string{"a"}},
		{ID: "r2", Name: "Lumen-Name", ItemIDs: []string{"c", "d"}},
	})
	if p.Aufgeloest != 1 || p.SchonDa != 1 || len(p.Fehlen) != 0 || len(p.Ergaenzen) != 0 {
		t.Fatalf("%+v", p)
	}
}
