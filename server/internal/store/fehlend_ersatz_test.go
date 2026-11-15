// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

import (
	"path/filepath"
	"testing"
)

// Nach „In MKV umwandeln" hängt die MKV am Film der Scheibe, und alle
// VOB-Teile sind ausgetragen — nicht erst beim nächsten Durchlauf und
// nicht nur Teil 1.
func TestScheibeErsetzen(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "l.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := t.Context()
	wurzel := t.TempDir()
	lib, _ := st.CreateLibrary(ctx, "Filme", "movie", wurzel, "de")
	it := &Item{LibraryID: lib.ID, Kind: "movie", Title: "Heidi", Year: 2015, MatchState: "matched"}
	_ = st.UpsertItem(ctx, it)
	anderer := &Item{LibraryID: lib.ID, Kind: "movie", Title: "Anderer", MatchState: "matched"}
	_ = st.UpsertItem(ctx, anderer)
	vts := filepath.Join(wurzel, "Heidi (2015)", "VIDEO_TS")
	for i, n := range []string{"VTS_01_1.VOB", "VTS_01_2.VOB", "VTS_01_3.VOB"} {
		if err := st.UpsertFile(ctx, &MediaFile{ItemID: it.ID, Path: filepath.Join(vts, n), SizeBytes: 1000, PartNo: i + 1}); err != nil {
			t.Fatal(err)
		}
	}
	_ = st.UpsertFile(ctx, &MediaFile{ItemID: anderer.ID, Path: filepath.Join(wurzel, "Anderer.mkv"), SizeBytes: 5, PartNo: 1})
	neu := &MediaFile{Path: filepath.Join(wurzel, "Heidi (2015)", "Heidi (2015).mkv"), SizeBytes: 2900, PartNo: 1, Container: "mkv"}
	weg, err := st.ScheibeErsetzen(ctx, lib.ID, vts, neu)
	if err != nil {
		t.Fatal(err)
	}
	if weg != 3 || neu.ItemID != it.ID {
		t.Fatalf("ausgetragen %d, Titel %q (erwartet 3, %q)", weg, neu.ItemID, it.ID)
	}
	dateien, _ := st.FilesFor(ctx, it.ID)
	if len(dateien) != 1 || dateien[0].Path != neu.Path {
		t.Errorf("Dateien des Films: %+v", dateien)
	}
	if d, _ := st.FilesFor(ctx, anderer.ID); len(d) != 1 {
		t.Errorf("fremde Datei angefasst: %+v", d)
	}
}
