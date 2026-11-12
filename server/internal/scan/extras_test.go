// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package scan

import (
	"path/filepath"
	"strings"
	"testing"

	"lumen/internal/store"
)

// Extras nach Emby-Art werden nie Filme oder Fassungen — und die
// Titelseite findet sie wieder.
func TestExtrasWerdenKeineFilme(t *testing.T) {
	ctx := t.Context()
	st := testStore(t)
	wurzel := t.TempDir()
	dateienAnlegen(t, wurzel,
		"Inception (2010)/Inception (2010).mkv",
		"Inception (2010)/Inception (2010)-clip.mkv",
		"Inception (2010)/Inception (2010)-trailer.mp4",
		"Inception (2010)/theme.mkv",
		"Inception (2010)/clips/Hallway Fight.mkv",
		"Inception (2010)/backdrops/theme.mkv",
		"Inception (2010)/featurettes/Dreams.mkv",
		"Inception (2010)/behind the scenes/Making of.mkv",
		"Inception (2010)/deleted scenes/Cut.mkv",
		"Inception (2010)/interviews/Nolan.mkv",
		"Inception (2010)/scenes/Kick.mkv",
		"Inception (2010)/shorts/Short.mkv",
		"Inception (2010)/other/Other.mkv",
		"Inception (2010)/extras/Extra.mkv",
		"Inception (2010)/trailers/Teaser.mkv",
		"Inception (2010)/Inception (2010)-featurette.mkv",
		"Inception (2010)/Inception (2010)-behindthescenes.mkv",
		"Inception (2010)/Inception (2010)-deleted.mkv",
		"Inception (2010)/Inception (2010)-interview.mkv",
		"Inception (2010)/Inception (2010)-scene.mkv",
		"Inception (2010)/Inception (2010)-short.mkv",
		"Inception (2010)/Inception (2010)-other.mkv",
		"Inception (2010)/Inception (2010)-sample.mkv",
		"Heat (1995).mkv",
		"Heat (1995)-trailer.mkv",
		"Alien (1979)-trailer.mkv",
	)
	lib, _ := st.CreateLibrary(ctx, "Filme", "movie", wurzel, "de")
	sc := New(st, nil)
	if _, err := sc.Scan(ctx, *lib); err != nil {
		t.Fatal(err)
	}
	filme, _ := st.Query(ctx, store.Query{Libraries: []string{lib.ID}, Kinds: []string{"movie"}, Limit: 20})
	if len(filme) != 2 {
		t.Fatalf("Filme: %v", titles(filme))
	}
	for _, f := range filme {
		files, _ := st.FilesFor(ctx, f.ID)
		if len(files) != 1 {
			t.Errorf("%s: %d Dateien (Extra als Fassung?)", f.Title, len(files))
		}
		var pfade []string
		for _, x := range files {
			pfade = append(pfade, x.Path)
		}
		extras := ExtrasZu(pfade, wurzel)
		switch f.Title {
		case "Inception":
			// Alles ausser theme.mkv, backdrops/ (Themenvideos) und dem Sample.
			if len(extras) != 19 {
				var n []string
				for _, e := range extras {
					n = append(n, e.Art+":"+e.Titel)
				}
				t.Errorf("Inception: %d Extras: %s", len(extras), strings.Join(n, ", "))
			}
			if extras[0].Art != "trailer" {
				t.Errorf("Trailer zuerst: %+v", extras[0])
			}
		case "Heat":
			// Lose in der Bibliothek: nur das Extra mit dem eigenen Namen.
			if len(extras) != 1 || filepath.Base(extras[0].Pfad) != "Heat (1995)-trailer.mkv" {
				t.Errorf("Heat: %+v", extras)
			}
		}
	}
}
