// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package scan

import (
	"testing"

	"lumen/internal/store"
)

// „Film a.avi", „Film b.avi" ohne Jahr im Namen (README): ein Film in
// Teilen, wenn beide im Ordner liegen. Ein einzelnes „Plan b" bleibt ein
// Titel.
func TestBuchstabenTeileOhneJahr(t *testing.T) {
	ctx := t.Context()
	st := testStore(t)
	wurzel := t.TempDir()
	dateienAnlegen(t, wurzel,
		"Die Schweizermacher (1978)/Die Schweizermacher a.avi",
		"Die Schweizermacher (1978)/Die Schweizermacher b.avi",
		"Grounding/Grounding a.avi",
		"Grounding/Grounding b.avi",
		"Grounding/Grounding c.avi",
		"Plan b (2021)/Plan b.mkv",
	)
	lib, _ := st.CreateLibrary(ctx, "Filme", "movie", wurzel, "de")
	sc := New(st, nil)
	if _, err := sc.Scan(ctx, *lib); err != nil {
		t.Fatal(err)
	}
	filme, _ := st.Query(ctx, store.Query{Libraries: []string{lib.ID}, Kinds: []string{"movie"}, Limit: 20})
	teile := map[string]int{}
	for _, f := range filme {
		files, _ := st.FilesFor(ctx, f.ID)
		teile[f.Title] = len(files)
	}
	want := map[string]int{"Die Schweizermacher": 2, "Grounding": 3, "Plan b": 1}
	if len(teile) != len(want) {
		t.Fatalf("Filme: %v", teile)
	}
	for k, v := range want {
		if teile[k] != v {
			t.Errorf("%s: %d Dateien, erwartet %d (alle: %v)", k, teile[k], v, teile)
		}
	}
	for _, f := range filme {
		if f.Title != "Die Schweizermacher" {
			continue
		}
		files, _ := st.FilesFor(ctx, f.ID)
		for _, x := range files {
			if x.PartNo < 1 || x.PartNo > 2 {
				t.Errorf("Teil %d", x.PartNo)
			}
		}
		if f.Year != 1978 {
			t.Errorf("Jahr %d", f.Year)
		}
	}
}
