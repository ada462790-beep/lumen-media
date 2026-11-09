// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

import (
	"context"
	"errors"
	"testing"
)

func zeiger[T any](v T) *T { return &v }

// Was von Hand bearbeitet ist, überlebt „Metadaten neu laden" — alles
// andere bringt die Quelle wie gewohnt.
func TestHandbearbeitungUeberlebtNeuLaden(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	mustExec(t, s, `INSERT INTO library (id,name,kind,path) VALUES ('lib','Filme','movie','/x')`)
	it := &Item{LibraryID: "lib", Kind: "movie", Title: "Sommerfest 2019", MatchState: "unmatched"}
	if err := s.UpsertItem(ctx, it); err != nil {
		t.Fatal(err)
	}

	if err := s.MetadatenVonHand(ctx, it.ID, Handbearbeitung{
		Title:     zeiger("Sommerfest bei Oma"),
		Overview:  zeiger("Das grosse Familienfest im Garten."),
		Genres:    &[]string{"Familie"},
		Year:      zeiger(2019),
		AgeRating: zeiger("0"), AgeYears: zeiger(0),
		Cast: &[]Person{{Name: "Oma Ruth", Rolle: "Gastgeberin"}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BildVonHand(ctx, it.ID, FeldPoster, "x-poster-hand.jpg"); err != nil {
		t.Fatal(err)
	}

	// Ein Durchlauf der Quelle bringt andere Werte für alles.
	quelle, _ := s.Item(ctx, it.ID)
	quelle.Title, quelle.Overview, quelle.Year = "Summer Fest", "Falsch", 1999
	quelle.Genres = []string{"Horror"}
	quelle.Premiered = "1999-05-01"
	quelle.AgeRating, quelle.AgeYears = "18", zeiger(18)
	quelle.MatchState = "unmatched"
	quelle.SortTitle = SortKey(quelle.Title)
	if err := s.UpsertItem(ctx, quelle); err != nil {
		t.Fatal(err)
	}
	_ = s.SetArtwork(ctx, it.ID, "poster", "x-poster.jpg", "https://tmdb/x.jpg")
	_ = s.SetArtwork(ctx, it.ID, "backdrop", "x-backdrop.jpg", "https://tmdb/y.jpg")
	_ = s.BesetzungSetzen(ctx, it.ID, []Person{{Name: "Fremde Person"}})

	got, _ := s.Item(ctx, it.ID)
	if got.Title != "Sommerfest bei Oma" || got.Overview != "Das grosse Familienfest im Garten." || got.Year != 2019 {
		t.Fatalf("gesperrte Felder überschrieben: %+v", got)
	}
	if len(got.Genres) != 1 || got.Genres[0] != "Familie" {
		t.Fatalf("Genres überschrieben: %v", got.Genres)
	}
	if got.AgeYears == nil || *got.AgeYears != 0 {
		t.Fatalf("Altersfreigabe überschrieben: %v", got.AgeYears)
	}
	// Nicht bearbeitet: kommt von der Quelle.
	if got.Premiered != "1999-05-01" {
		t.Fatalf("ungesperrtes Feld nicht übernommen: %q", got.Premiered)
	}
	// Kein „nicht erkannt" mehr.
	if got.MatchState != "manual" {
		t.Fatalf("Zustand %q statt manual", got.MatchState)
	}
	if n := s.UnmatchedCount(ctx); n != 0 {
		t.Fatalf("bearbeiteter Titel zählt noch als nicht erkannt: %d", n)
	}
	art, _ := s.ArtworkFor(ctx, []string{it.ID})
	if art[it.ID]["poster"] != "x-poster-hand.jpg" {
		t.Fatalf("Poster überschrieben: %v", art[it.ID])
	}
	if art[it.ID]["backdrop"] != "x-backdrop.jpg" {
		t.Fatalf("ungesperrter Hintergrund fehlt: %v", art[it.ID])
	}
	leute, _ := s.Besetzung(ctx, it.ID)
	if len(leute) != 1 || leute[0].Name != "Oma Ruth" {
		t.Fatalf("Besetzung überschrieben: %v", leute)
	}
	// Die Suche findet den neuen Titel.
	treffer, _ := s.Query(ctx, Query{Libraries: []string{"lib"}, Text: "oma"})
	if len(treffer) != 1 {
		t.Fatalf("Suche findet den bearbeiteten Titel nicht: %v", treffer)
	}

	// Sperre aufheben: Beim nächsten Mal gilt wieder die Quelle.
	if err := s.SperreAufheben(ctx, it.ID, FeldBeschreibung); err != nil {
		t.Fatal(err)
	}
	quelle, _ = s.Item(ctx, it.ID)
	quelle.Overview = "Neu von der Quelle"
	_ = s.UpsertItem(ctx, quelle)
	got, _ = s.Item(ctx, it.ID)
	if got.Overview != "Neu von der Quelle" {
		t.Fatalf("aufgehobene Sperre wirkt noch: %q", got.Overview)
	}
	sp, _ := s.Sperren(ctx, it.ID)
	if sp[FeldBeschreibung] || !sp[FeldTitel] || !sp[FeldPoster] {
		t.Fatalf("Sperren: %v", sp)
	}
	if err := s.SperreAufheben(ctx, it.ID, "quatsch"); err == nil {
		t.Fatal("unbekanntes Feld angenommen")
	}
}

// Ein eigener Sortiertitel bestimmt die Stelle im Alphabet, auch wenn
// TMDb den Titel ändert.
func TestSortiertitelVonHand(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	mustExec(t, s, `INSERT INTO library (id,name,kind,path) VALUES ('lib','Filme','movie','/x')`)
	a := &Item{LibraryID: "lib", Kind: "movie", Title: "Zorro", MatchState: "matched"}
	b := &Item{LibraryID: "lib", Kind: "movie", Title: "Mitte", MatchState: "matched"}
	_ = s.UpsertItem(ctx, a)
	_ = s.UpsertItem(ctx, b)
	if err := s.MetadatenVonHand(ctx, a.ID, Handbearbeitung{SortTitle: zeiger("Aaa zuerst")}); err != nil {
		t.Fatal(err)
	}
	a2, _ := s.Item(ctx, a.ID)
	a2.Title, a2.SortTitle = "Zorro (Neu)", SortKey("Zorro (Neu)")
	_ = s.UpsertItem(ctx, a2)
	liste, _ := s.Query(ctx, Query{Libraries: []string{"lib"}, Sort: "title"})
	if len(liste) != 2 || liste[0].ID != a.ID {
		t.Fatalf("eigener Sortiertitel wirkt nicht: %v", liste)
	}
	if s.Sortiertitel(ctx, a.ID) != "Aaa zuerst" {
		t.Fatalf("Sortiertitel zum Anzeigen: %q", s.Sortiertitel(ctx, a.ID))
	}
	// Leer heisst: wieder nach dem Titel.
	_ = s.MetadatenVonHand(ctx, a.ID, Handbearbeitung{SortTitle: zeiger("")})
	liste, _ = s.Query(ctx, Query{Libraries: []string{"lib"}, Sort: "title"})
	if liste[0].ID != b.ID {
		t.Fatalf("Sortiertitel nicht zurückgesetzt: %v", liste)
	}
}

// Folgennummern lassen sich ändern, aber nicht doppelt vergeben; eine
// geänderte Staffelnummer gilt auch für ihre Folgen.
func TestNummernVonHand(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	mustExec(t, s, `INSERT INTO library (id,name,kind,path) VALUES ('lib','Serien','series','/x')`)
	serie := &Item{LibraryID: "lib", Kind: "series", Title: "Nordwand"}
	_ = s.UpsertItem(ctx, serie)
	st := &Item{LibraryID: "lib", Kind: "season", ParentID: serie.ID, Title: "Staffel 1", SeasonNo: 1}
	_ = s.UpsertItem(ctx, st)
	e1 := &Item{LibraryID: "lib", Kind: "episode", ParentID: st.ID, Title: "Eins", SeasonNo: 1, EpisodeNo: 1}
	e2 := &Item{LibraryID: "lib", Kind: "episode", ParentID: st.ID, Title: "Zwei", SeasonNo: 1, EpisodeNo: 2}
	_ = s.UpsertItem(ctx, e1)
	_ = s.UpsertItem(ctx, e2)

	if err := s.MetadatenVonHand(ctx, e2.ID, Handbearbeitung{EpisodeNo: zeiger(1)}); !errors.Is(err, ErrNummerBelegt) {
		t.Fatalf("doppelte Folgennummer angenommen: %v", err)
	}
	if err := s.MetadatenVonHand(ctx, e2.ID, Handbearbeitung{EpisodeNo: zeiger(5)}); err != nil {
		t.Fatal(err)
	}
	if err := s.MetadatenVonHand(ctx, st.ID, Handbearbeitung{SeasonNo: zeiger(2)}); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Item(ctx, e2.ID)
	if got.EpisodeNo != 5 || got.SeasonNo != 2 {
		t.Fatalf("Nummern: S%d F%d", got.SeasonNo, got.EpisodeNo)
	}
	if err := s.MetadatenVonHand(ctx, e1.ID, Handbearbeitung{SeasonNo: zeiger(3)}); err == nil {
		t.Fatal("Staffelnummer an einer Folge angenommen")
	}

	// Der Scanner findet die Datei über ihren Pfad bei der gesperrten Folge.
	mustExec(t, s, `INSERT INTO media_file (id,item_id,path,size_bytes,mtime) VALUES ('f2','`+e2.ID+`','/x/Nordwand S01E02.mkv',1,1)`)
	if id := s.GesperrteFolgeZuPfad(ctx, "/x/Nordwand S01E02.mkv", serie.ID); id != e2.ID {
		t.Fatalf("gesperrte Folge zum Pfad: %q", id)
	}
	if id := s.GesperrteFolgeZuPfad(ctx, "/x/Nordwand S01E02.mkv", "andere-serie"); id != "" {
		t.Fatalf("Folge einer anderen Serie gefunden: %q", id)
	}
}
