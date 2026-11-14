// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

import (
	"context"
	"testing"
)

// Die Seite Aufräumen unterscheidet Teile, echte Doppel („Brust oder
// Keule" zweimal mit 1,7 GB in zwei Ordnern) und Fassungen (American
// Psycho als 1080p-MKV und 400p-AVI). Vorher kam alles als „Fassungen".
func TestDoppelArt(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	lib, _ := st.CreateLibrary(ctx, "Filme", "movie", t.TempDir(), "de")
	film := func(titel string, dateien ...MediaFile) string {
		it := &Item{LibraryID: lib.ID, Kind: "movie", Title: titel, MatchState: "matched"}
		if err := st.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
		for i := range dateien {
			dateien[i].ItemID = it.ID
			if err := st.UpsertFile(ctx, &dateien[i]); err != nil {
				t.Fatal(err)
			}
		}
		return it.ID
	}
	brust := film("Brust oder Keule",
		MediaFile{Path: "/m/Brust oder Keule (1976)/Brust oder Keule (1976).mkv", SizeBytes: 1_700_000_000, DurationMS: 5_400_000, PartNo: 1},
		MediaFile{Path: "/m/_Louis de Funes/Brust oder Keule (1976).mkv", SizeBytes: 1_700_000_000, DurationMS: 5_400_500, PartNo: 1})
	psycho := film("American Psycho",
		MediaFile{Path: "/m/American Psycho/American Psycho 1080p.mkv", SizeBytes: 9_000_000_000, DurationMS: 6_120_000, PartNo: 1},
		MediaFile{Path: "/m/American Psycho/American Psycho.avi", SizeBytes: 700_000_000, DurationMS: 6_118_000, PartNo: 1})
	teile := film("Apocalypse Now",
		MediaFile{Path: "/m/Apocalypse Now/a.avi", SizeBytes: 700_000_000, PartNo: 1},
		MediaFile{Path: "/m/Apocalypse Now/b.avi", SizeBytes: 700_000_000, PartNo: 2})

	dups, err := st.Duplicates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	art := map[string]Duplicate{}
	for _, d := range dups {
		art[d.ItemID] = d
	}
	if d := art[brust]; d.Art != "identisch" || len(d.Identisch) != 2 {
		t.Errorf("Brust oder Keule: %q %v", d.Art, d.Identisch)
	}
	if d := art[psycho]; d.Art != "fassungen" || len(d.Identisch) != 0 {
		t.Errorf("American Psycho: %q %v", d.Art, d.Identisch)
	}
	// Teile gleicher Grösse sind Teile, keine Doppel.
	if d := art[teile]; d.Art != "teile" || len(d.Identisch) != 0 {
		t.Errorf("Apocalypse Now: %q %v", d.Art, d.Identisch)
	}
}
