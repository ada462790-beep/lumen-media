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

// Eine defekte Datei steht in FilesFor hinten — auch wenn sie die höchste
// Auflösung hat oder bevorzugt ist. Die Apps nehmen die erste Datei als
// Standard; sie darf nie eine sein, die sich nicht abspielen lässt.
func TestFilesForDefekteHinten(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	lib, _ := st.CreateLibrary(ctx, "Filme", "movie", t.TempDir(), "de")
	it := &Item{LibraryID: lib.ID, Kind: "movie", Title: "American Psycho", MatchState: "matched"}
	if err := st.UpsertItem(ctx, it); err != nil {
		t.Fatal(err)
	}
	dateien := []MediaFile{
		{Path: "/m/American Psycho/kaputt 2160p.mkv", SizeBytes: 20_000_000_000, Height: 2160, PartNo: 1, Defekt: "kaputt"},
		{Path: "/m/American Psycho/gut 1080p.mkv", SizeBytes: 9_000_000_000, Height: 1080, PartNo: 1},
		{Path: "/m/American Psycho/gut 400p.avi", SizeBytes: 700_000_000, Height: 400, PartNo: 1},
	}
	for i := range dateien {
		dateien[i].ItemID = it.ID
		if err := st.UpsertFile(ctx, &dateien[i]); err != nil {
			t.Fatal(err)
		}
	}
	reihe := func() []string {
		fs, err := st.FilesFor(ctx, it.ID)
		if err != nil {
			t.Fatal(err)
		}
		out := []string{}
		for _, f := range fs {
			out = append(out, f.Path)
		}
		return out
	}
	got := reihe()
	if len(got) != 3 || got[0] != dateien[1].Path || got[1] != dateien[2].Path || got[2] != dateien[0].Path {
		t.Fatalf("Reihenfolge: %v", got)
	}
	// Auch bevorzugt bleibt die defekte hinten.
	if _, err := st.db.ExecContext(ctx, `UPDATE item SET bevorzugte_datei = ? WHERE id = ?`, dateien[0].ID, it.ID); err != nil {
		t.Fatal(err)
	}
	if got := reihe(); got[0] != dateien[1].Path || got[2] != dateien[0].Path {
		t.Fatalf("bevorzugt und defekt: %v", got)
	}
}
