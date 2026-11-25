// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package meta

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"lumen/internal/store"
)

// Die Antworten von TMDb zu „The Animatrix", wie sie die Suche (de-DE)
// am 25.11.2026 lieferte: die Sammlung (55931) und ihre Kurzfilme, alle
// 2003. Die Kurzfilme führen „The Animatrix - …" als Alternativtitel.
var animatrixKatalog = []tmdbFilm{
	{id: 55931, de: "Animatrix", en: "The Animatrix", orig: "The Animatrix", jahr: 2003, pop: 3.5},
	{id: 24357, de: "Der Letzte Flug der Osiris", en: "Final Flight of the Osiris", orig: "Final Flight of the Osiris",
		jahr: 2003, pop: 2.5, alt: []string{"The Animatrix - Final Flight of the Osiris", "Animatrix - Le Dernier Vol de l'Osiris"}},
	{id: 24358, de: "The Second Renaissance Part I", orig: "The Second Renaissance Part I", jahr: 2003, pop: 1.5,
		alt: []string{"The Animatrix - The Second Renaissance Part I"}},
	{id: 668247, de: "The Animatrix: Aki Ross", orig: "The Animatrix: Aki Ross", jahr: 2003, pop: 0.6},
	{id: 793641, de: "Executions: The Making of 'The Animatrix'", orig: "Executions: The Making of 'The Animatrix'", jahr: 2003, pop: 0.8},
	{id: 1, de: "M - Eine Stadt sucht einen Mörder", en: "M", orig: "M", jahr: 1931, pop: 12},
}

// Die Suche selbst nimmt die Sammlung, nicht einen Kurzfilm — mit dem
// Titel, den der Scanner aus „_Matrix/The Animatrix/The Animatrix -
// a.m2ts" liest („The Animatrix", ohne Jahr), wie mit den Lesarten
// älterer Fassungen („The Animatrix: a"), deutsch und englisch.
func TestAnimatrixSucheNimmtSammlung(t *testing.T) {
	n := &tmdbNachbau{filme: animatrixKatalog}
	srv := n.server(t)
	for _, sprache := range []string{"de-DE", "en-US"} {
		r := nachbauResolver(t, testStore(t), srv)
		r.tmdb.Language = sprache
		for _, titel := range []string{"The Animatrix", "Animatrix", "Animatrix, The", "The Animatrix: a", "The Animatrix - b"} {
			for _, jahr := range []int{0, 2003} {
				m, g, _, err := r.finden(context.Background(), "movie", titel, jahr, "")
				if err != nil {
					t.Fatal(err)
				}
				if !m.Sure || m.Candidate.SourceID != "55931" {
					got := ""
					if m.Candidate != nil {
						got = m.Candidate.SourceID + " " + m.Candidate.Title
					}
					t.Errorf("%s %q (%d): %q sicher=%v %.2f %s, gesucht %q", sprache, titel, jahr, got, m.Sure, m.Score, m.Why, g.Texte)
				}
			}
		}
	}
}

// Ein Titel aus einem einzigen Zeichen („M", „9") brachte die Leiter mit
// Jahr zum Absturz (Index -1), ohne Jahr wurde gar nicht gesucht.
func TestEinZeichenTitel(t *testing.T) {
	n := &tmdbNachbau{filme: animatrixKatalog}
	r := nachbauResolver(t, testStore(t), n.server(t))
	for _, jahr := range []int{1931, 0} {
		m, _, _, err := r.finden(context.Background(), "movie", "M", jahr, "")
		if err != nil {
			t.Fatal(err)
		}
		if jahr > 0 && (!m.Sure || m.Candidate.SourceID != "1") {
			t.Errorf("„M (1931)“: %+v", m)
		}
	}
	if m, _, _, err := r.finden(context.Background(), "movie", "", 2000, ""); err != nil || m.Sure {
		t.Errorf("leerer Titel: %+v %v", m, err)
	}
}

// falschZugeordnet legt den Befund vom NAS an: „The Animatrix - a/b.m2ts"
// als ein Film in Teilen, von Lumen selbst (match_state „matched") dem
// Kurzfilm 24357 zugeordnet — mit dessen Titel.
func falschZugeordnet(t *testing.T, st *store.Store, zustand string) (*store.Library, *store.Item) {
	t.Helper()
	ctx := context.Background()
	wurzel := t.TempDir()
	lib, err := st.CreateLibrary(ctx, "Filme", "movie", wurzel, "de")
	if err != nil {
		t.Fatal(err)
	}
	it := &store.Item{LibraryID: lib.ID, Kind: "movie", Title: "Der Letzte Flug der Osiris",
		OriginalTitle: "Final Flight of the Osiris", Year: 2003, MatchState: zustand}
	if err := st.UpsertItem(ctx, it); err != nil {
		t.Fatal(err)
	}
	for i, teil := range []string{"a", "b"} {
		pfad := filepath.Join(wurzel, "_Matrix", "The Animatrix", "The Animatrix - "+teil+".m2ts")
		if err := st.UpsertFile(ctx, &store.MediaFile{ItemID: it.ID, Path: pfad, SizeBytes: 1, PartNo: i + 1}); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.SetSourceID(ctx, it.ID, "tmdb", "24357"); err != nil {
		t.Fatal(err)
	}
	return lib, it
}

// dateititelAttrappe liest wie der Scanner (scan.DateititelFuer) — hier
// nur für die Dateien dieses Tests.
func dateititelAttrappe(t *testing.T, titel string) {
	t.Helper()
	alt := DateititelLesen
	DateititelLesen = func(_ store.Library, pfad string) (string, int) {
		if strings.Contains(pfad, "The Animatrix - ") {
			return titel, 0
		}
		return "", 0
	}
	t.Cleanup(func() { DateititelLesen = alt })
}

func tmdbVon(t *testing.T, st *store.Store, id string) (string, *store.Item) {
	t.Helper()
	src, _ := st.SourceID(context.Background(), id, "tmdb")
	it, err := st.Item(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return src, it
}

// Der Befund: Ein einmal falscher Treffer bestätigte sich beim Neuladen
// selbst — gesucht wurde mit dem Titel des falschen Treffers. Jetzt sucht
// Lumen mit dem Namen der Dateien und kommt zur Sammlung.
func TestNeuladenKorrigiertEigenenFalschenTreffer(t *testing.T) {
	n := &tmdbNachbau{filme: animatrixKatalog}
	srv := n.server(t)
	ctx := context.Background()

	// Ohne die Lesart der Dateien (wie bisher): bleibt beim Kurzfilm.
	t.Run("bisher", func(t *testing.T) {
		alt := DateititelLesen
		DateititelLesen = nil
		t.Cleanup(func() { DateititelLesen = alt })
		st := testStore(t)
		lib, it := falschZugeordnet(t, st, "matched")
		r := nachbauResolver(t, st, srv)
		if _, err := st.MetadatenZuruecksetzen(ctx, lib.ID, ""); err != nil {
			t.Fatal(err)
		}
		if _, err := r.ResolvePending(ctx, lib.ID, 10); err != nil {
			t.Fatal(err)
		}
		if src, _ := tmdbVon(t, st, it.ID); src != "24357" {
			t.Fatalf("erwartet den alten (falschen) Treffer, bekam %s", src)
		}
	})

	t.Run("Bibliothek neu laden", func(t *testing.T) {
		dateititelAttrappe(t, "The Animatrix")
		st := testStore(t)
		lib, it := falschZugeordnet(t, st, "matched")
		r := nachbauResolver(t, st, srv)
		if _, err := st.MetadatenZuruecksetzen(ctx, lib.ID, ""); err != nil {
			t.Fatal(err)
		}
		if _, err := r.ResolvePending(ctx, lib.ID, 10); err != nil {
			t.Fatal(err)
		}
		src, got := tmdbVon(t, st, it.ID)
		if src != "55931" || got.Title != "Animatrix" || got.MatchState != "matched" {
			t.Fatalf("Quelle %s, Titel %q, Zustand %q — erwartet 55931 „Animatrix“", src, got.Title, got.MatchState)
		}
		files, _ := st.FilesFor(ctx, it.ID)
		if len(files) != 2 {
			t.Errorf("Teile: %d Dateien", len(files))
		}
	})

	t.Run("ein Titel neu laden", func(t *testing.T) {
		dateititelAttrappe(t, "The Animatrix")
		st := testStore(t)
		_, it := falschZugeordnet(t, st, "matched")
		r := nachbauResolver(t, st, srv)
		if err := r.NeuLaden(ctx, it); err != nil {
			t.Fatal(err)
		}
		if src, _ := tmdbVon(t, st, it.ID); src != "55931" {
			t.Fatalf("Quelle %s", src)
		}
	})

	// Findet der Dateiname nichts Sicheres, bleibt es beim bisherigen
	// Treffer — Neuladen macht keinen erkannten Film zu „nicht erkannt".
	t.Run("Dateiname findet nichts", func(t *testing.T) {
		dateititelAttrappe(t, "Gibt es nicht bei TMDb")
		st := testStore(t)
		lib, it := falschZugeordnet(t, st, "matched")
		r := nachbauResolver(t, st, srv)
		_, _ = st.MetadatenZuruecksetzen(ctx, lib.ID, "")
		if _, err := r.ResolvePending(ctx, lib.ID, 10); err != nil {
			t.Fatal(err)
		}
		src, got := tmdbVon(t, st, it.ID)
		if src != "24357" || got.MatchState != "matched" {
			t.Fatalf("Quelle %s, Zustand %q", src, got.MatchState)
		}
	})
}

// Was jemand entschieden hat, bleibt: von Hand zugewiesen (oder aus Emby
// übernommen), ein von Hand geänderter Titel, eine NFO.
func TestNeuladenLaesstHandarbeitStehen(t *testing.T) {
	n := &tmdbNachbau{filme: animatrixKatalog}
	srv := n.server(t)
	ctx := context.Background()
	dateititelAttrappe(t, "The Animatrix")

	t.Run("von Hand zugewiesen", func(t *testing.T) {
		st := testStore(t)
		lib, it := falschZugeordnet(t, st, "manual")
		r := nachbauResolver(t, st, srv)
		_, _ = st.MetadatenZuruecksetzen(ctx, lib.ID, "")
		if _, err := r.ResolvePending(ctx, lib.ID, 10); err != nil {
			t.Fatal(err)
		}
		// Auch „Metadaten neu laden" am Titel selbst hängt nicht um.
		it, _ = st.Item(ctx, it.ID)
		if err := r.NeuLaden(ctx, it); err != nil {
			t.Fatal(err)
		}
		src, got := tmdbVon(t, st, it.ID)
		if src != "24357" || got.MatchState != "manual" {
			t.Fatalf("Quelle %s, Zustand %q — von Hand gewählt muss bleiben", src, got.MatchState)
		}
	})

	t.Run("Titel von Hand geändert", func(t *testing.T) {
		st := testStore(t)
		lib, it := falschZugeordnet(t, st, "matched")
		titel := "Der Letzte Flug der Osiris"
		if err := st.MetadatenVonHand(ctx, it.ID, store.Handbearbeitung{Title: &titel}); err != nil {
			t.Fatal(err)
		}
		r := nachbauResolver(t, st, srv)
		_, _ = st.MetadatenZuruecksetzen(ctx, lib.ID, "")
		if _, err := r.ResolvePending(ctx, lib.ID, 10); err != nil {
			t.Fatal(err)
		}
		if src, _ := tmdbVon(t, st, it.ID); src != "24357" {
			t.Fatalf("Quelle %s", src)
		}
	})

	t.Run("NFO daneben", func(t *testing.T) {
		st := testStore(t)
		lib, it := falschZugeordnet(t, st, "matched")
		if err := st.LokaleNFOSetzen(ctx, it.ID, store.LokaleNFO{Daten: `{"titel":"Der Letzte Flug der Osiris"}`}); err != nil {
			t.Fatal(err)
		}
		r := nachbauResolver(t, st, srv)
		_, _ = st.MetadatenZuruecksetzen(ctx, lib.ID, "")
		if _, err := r.ResolvePending(ctx, lib.ID, 10); err != nil {
			t.Fatal(err)
		}
		if src, _ := tmdbVon(t, st, it.ID); src != "24357" {
			t.Fatalf("Quelle %s", src)
		}
	})
}
