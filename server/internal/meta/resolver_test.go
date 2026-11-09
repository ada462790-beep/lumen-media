// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package meta

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"lumen/internal/store"
)

// Der ganze Weg: der Scanner hat einen Dateinamen geraten, die Quelle
// kennt den Film, und am Ende steht ein vollständiger Eintrag mit Poster
// in der Mediathek.
func TestResolveMovieEndToEnd(t *testing.T) {
	srv := fakeSources(t)
	st := testStore(t)
	ctx := context.Background()

	lib, err := st.CreateLibrary(ctx, "Filme", "movie", t.TempDir(), "de")
	if err != nil {
		t.Fatal(err)
	}
	// So, wie der Scanner den Titel hinterlässt: geraten, noch ohne alles.
	it := &store.Item{LibraryID: lib.ID, Kind: "movie",
		Title: "Die Bucht", Year: 2019, MatchState: "pending"}
	if err := st.UpsertItem(ctx, it); err != nil {
		t.Fatal(err)
	}

	r := resolverFor(t, st, srv)
	res, err := r.ResolvePending(ctx, lib.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if res.Matched != 1 {
		t.Fatalf("%d zugeordnet, erwartet 1 (%+v)", res.Matched, res)
	}

	got, err := st.Item(ctx, it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.MatchState != "matched" {
		t.Errorf("Zustand %q", got.MatchState)
	}
	if got.Overview == "" {
		t.Error("die Beschreibung fehlt")
	}
	if got.RuntimeMS != 118*60_000 {
		t.Errorf("Laufzeit %d", got.RuntimeMS)
	}
	if got.OriginalTitle != "The Bay" {
		t.Errorf("Originaltitel %q", got.OriginalTitle)
	}
	if got.IMDbID != "tt1234567" {
		t.Errorf("IMDb-Kennung %q", got.IMDbID)
	}
	// Die IMDb-Wertung richtet der Besitzer einmal ein und sie gilt für alle.
	if got.IMDbRating != 7.8 || got.IMDbVotes != 48213 {
		t.Errorf("IMDb-Wertung %v / %d", got.IMDbRating, got.IMDbVotes)
	}
	// Die Schweiz ist eingestellt, also die Schweizer Einstufung.
	if got.AgeYears == nil || *got.AgeYears != 16 {
		t.Errorf("Altersfreigabe %v", got.AgeYears)
	}
	if len(got.Genres) != 2 {
		t.Errorf("Genres %v", got.Genres)
	}

	// Die Besetzung kommt mit — samt Bild, lokal abgelegt.
	leute, _ := st.Besetzung(ctx, it.ID)
	if len(leute) != 2 || leute[0].Name != "Lea Marti" || leute[0].Rolle != "Nora" || leute[0].Bild == "" {
		t.Errorf("Besetzung: %+v", leute)
	}
	if _, err := os.Stat(filepath.Join(r.ArtDir(), leute[0].Bild)); err != nil {
		t.Errorf("Bild der Besetzung fehlt: %v", err)
	}

	// TMDb nennt die Reihe; Lumen legt sie an, ohne den Zusatz „Filmreihe".
	reihe, err := st.ReiheVon(ctx, it.ID)
	if err != nil || reihe == nil {
		t.Fatalf("keine Reihe angelegt: %v", err)
	}
	if reihe.Name != "Die Bucht" || reihe.Source != "tmdb" {
		t.Errorf("Reihe %q aus %q", reihe.Name, reihe.Source)
	}
	// Und die Reihe hat ihre eigenen Bilder, nicht die des Films.
	if reihe.PosterDatei == "" || reihe.BackdropDatei == "" || reihe.Overview != "Alle Teile." {
		t.Errorf("Bilder der Reihe fehlen: %+v", reihe)
	}
	if _, err := os.Stat(filepath.Join(r.ArtDir(), reihe.PosterDatei)); err != nil {
		t.Errorf("Posterdatei der Reihe fehlt: %v", err)
	}

	// Das Poster muss wirklich auf der Platte liegen — sonst zeigt die
	// Mediathek beim nächsten Netzausfall Löcher.
	art, err := st.ArtworkFor(ctx, []string{it.ID})
	if err != nil {
		t.Fatal(err)
	}
	name := art[it.ID]["poster"]
	if name == "" {
		t.Fatal("kein Poster hinterlegt")
	}
	info, err := os.Stat(filepath.Join(r.ArtDir(), name))
	if err != nil || info.Size() == 0 {
		t.Fatalf("Posterdatei fehlt oder ist leer: %v", err)
	}
}

// Was nicht sicher zuzuordnen ist, landet bei „nicht erkannt" — und nicht
// als falscher Film in der Mediathek.
func TestUnsureStaysUnmatched(t *testing.T) {
	srv := fakeSources(t)
	st := testStore(t)
	ctx := context.Background()

	lib, _ := st.CreateLibrary(ctx, "Filme", "movie", t.TempDir(), "de")
	it := &store.Item{LibraryID: lib.ID, Kind: "movie",
		Title: "Voellig anderer Film", Year: 1977, MatchState: "pending"}
	if err := st.UpsertItem(ctx, it); err != nil {
		t.Fatal(err)
	}

	res, err := resolverFor(t, st, srv).ResolvePending(ctx, lib.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if res.Unmatched != 1 || res.Matched != 0 {
		t.Fatalf("%+v — ein unsicherer Treffer wurde übernommen", res)
	}
	got, _ := st.Item(ctx, it.ID)
	if got.MatchState != "unmatched" {
		t.Errorf("Zustand %q", got.MatchState)
	}
	if got.Title != "Voellig anderer Film" {
		t.Errorf("der Titel wurde überschrieben: %q", got.Title)
	}
	// Und er taucht dort auf, wo der Besitzer ihn richtigstellen kann.
	list, err := st.UnmatchedItems(ctx, 10)
	if err != nil || len(list) != 1 {
		t.Fatalf("nicht in der Liste zum Zuordnen: %v %d", err, len(list))
	}
}

// Serie, Staffel, Folge: die Folge findet ihre Kennung über die Serie.
func TestResolveSeriesThenEpisodes(t *testing.T) {
	srv := fakeSources(t)
	st := testStore(t)
	ctx := context.Background()

	lib, _ := st.CreateLibrary(ctx, "Serien", "series", t.TempDir(), "de")
	series := &store.Item{LibraryID: lib.ID, Kind: "series",
		Title: "Nordlicht", MatchState: "pending"}
	if err := st.UpsertItem(ctx, series); err != nil {
		t.Fatal(err)
	}
	season := &store.Item{LibraryID: lib.ID, Kind: "season", ParentID: series.ID,
		Title: "Staffel 1", SeasonNo: 1, MatchState: "matched"}
	if err := st.UpsertItem(ctx, season); err != nil {
		t.Fatal(err)
	}
	ep := &store.Item{LibraryID: lib.ID, Kind: "episode", ParentID: season.ID,
		Title: "Folge 4", SeasonNo: 1, EpisodeNo: 4, MatchState: "pending"}
	if err := st.UpsertItem(ctx, ep); err != nil {
		t.Fatal(err)
	}

	r := resolverFor(t, st, srv)
	if _, err := r.ResolvePending(ctx, lib.ID, 10); err != nil {
		t.Fatal(err)
	}

	gotSeries, _ := st.Item(ctx, series.ID)
	if gotSeries.MatchState != "matched" {
		t.Errorf("Serie: %q", gotSeries.MatchState)
	}
	gotEp, _ := st.Item(ctx, ep.ID)
	if gotEp.Title != "Rückkehr" {
		t.Errorf("Folgentitel %q — erwartet den aus der Quelle", gotEp.Title)
	}
	if gotEp.RuntimeMS != 52*60_000 {
		t.Errorf("Folgenlaufzeit %d", gotEp.RuntimeMS)
	}
}

// Ohne eingerichtete Quelle sagt Lumen das klar, statt stillzuhalten.
func TestWithoutSourceItSaysSo(t *testing.T) {
	st := testStore(t)
	r, err := NewResolver(st, t.TempDir(), Config{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Ready() {
		t.Fatal("ohne Schlüssel sollte nichts bereit sein")
	}
	if _, err := r.ResolvePending(context.Background(), "", 10); err == nil {
		t.Fatal("es kam keine Meldung")
	}
}

// ---------------------------------------------------------------- Gerüst

func testStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "l.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

// fakeSources stellt TMDb, OMDb und den Bilderdienst in einem nach.
func fakeSources(t *testing.T) *httptest.Server {
	t.Helper()
	// Ein winziges, gültiges PNG — damit wirklich eine Bilddatei ankommt.
	png := []byte{
		0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a,
		0, 0, 0, 0x0d, 'I', 'H', 'D', 'R', 0, 0, 0, 1, 0, 0, 0, 1,
		8, 6, 0, 0, 0, 0x1f, 0x15, 0xc4, 0x89,
		0, 0, 0, 0x0a, 'I', 'D', 'A', 'T', 0x78, 0x9c, 0x63, 0, 1, 0, 0, 5, 0, 1,
		0x0d, 0x0a, 0x2d, 0xb4, 0, 0, 0, 0, 'I', 'E', 'N', 'D', 0xae, 0x42, 0x60, 0x82,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/search/movie":
			if r.URL.Query().Get("query") != "Die Bucht" {
				_ = json.NewEncoder(w).Encode(map[string]any{"results": []any{}})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"results": []map[string]any{
				{"id": 42, "title": "Die Bucht", "original_title": "The Bay",
					"overview":     "Eine Forscherin kehrt in ihr Heimatdorf zurück.",
					"release_date": "2019-03-14", "poster_path": "/abc.png",
					"backdrop_path": "/def.png", "popularity": 31.5}}})
		case r.URL.Path == "/movie/42":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": 42, "title": "Die Bucht", "original_title": "The Bay",
				"overview":     "Eine Forscherin kehrt in ihr Heimatdorf zurück.",
				"release_date": "2019-03-14", "runtime": 118, "imdb_id": "tt1234567",
				"poster_path": "/abc.png", "backdrop_path": "/def.png",
				"genres":                []map[string]any{{"name": "Drama"}, {"name": "Mystery"}},
				"belongs_to_collection": map[string]any{"id": 777, "name": "Die Bucht – Filmreihe"},
				"credits": map[string]any{"cast": []map[string]any{
					{"name": "Lea Marti", "character": "Nora", "profile_path": "/lea.png", "order": 0},
					{"name": "Jon Keller", "character": "Ben", "order": 1}}},
				"releases": map[string]any{"countries": []map[string]any{
					{"iso_3166_1": "DE", "certification": "12"},
					{"iso_3166_1": "CH", "certification": "16"}}}})
		case r.URL.Path == "/collection/777":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": 777, "name": "Die Bucht – Filmreihe",
				"overview":      "Alle Teile.",
				"poster_path":   "/reihe.png",
				"backdrop_path": "/reihe-breit.png"})
		case r.URL.Path == "/search/tv":
			_ = json.NewEncoder(w).Encode(map[string]any{"results": []map[string]any{
				{"id": 7, "name": "Nordlicht", "first_air_date": "2025-01-09",
					"overview": "Ein Dorf im Norden.", "poster_path": "/n.png"}}})
		case r.URL.Path == "/tv/7":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": 7, "name": "Nordlicht", "first_air_date": "2025-01-09",
				"overview": "Ein Dorf im Norden.", "poster_path": "/n.png",
				"genres":  []map[string]any{{"name": "Drama"}},
				"credits": map[string]any{"cast": []map[string]any{{"name": "Mia Roth", "character": "Elin"}}}})
		case r.URL.Path == "/tv/7/season/1/episode/4":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"name": "Rückkehr", "overview": "Sie kommt zurück.",
				"air_date": "2025-02-06", "runtime": 52})
		case r.URL.Path == "/find/tt1234567":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"movie_results": []map[string]any{{"id": 42}}, "tv_results": []any{}})
		case r.URL.Path == "/omdb":
			_ = json.NewEncoder(w).Encode(map[string]string{
				"Response": "True", "imdbRating": "7.8", "imdbVotes": "48,213"})
		case len(r.URL.Path) > 5 && r.URL.Path[:6] == "/bild/":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(png)
		default:
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"status_message": "nix"})
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func resolverFor(t *testing.T, st *store.Store, srv *httptest.Server) *Resolver {
	t.Helper()
	r, err := NewResolver(st, t.TempDir(), Config{
		TMDbKey: "schluessel", OMDbKey: "schluessel",
		Language: "de-DE", Region: "CH", UseIMDb: true})
	if err != nil {
		t.Fatal(err)
	}
	r.tmdb.BaseURL = srv.URL
	r.tmdb.ImageURL = srv.URL + "/bild"
	r.omdb.BaseURL = srv.URL + "/omdb"
	return r
}

// Statt zu suchen, den Link von TMDb oder IMDb einfügen.
func TestZuweisenMitLink(t *testing.T) {
	srv := fakeSources(t)
	st := testStore(t)
	r := resolverFor(t, st, srv)
	ctx := context.Background()
	for _, eingabe := range []string{
		"https://www.themoviedb.org/movie/42-die-bucht",
		"https://www.imdb.com/title/tt1234567/",
		"tt1234567",
		"42",
	} {
		c, err := r.Kandidaten(ctx, "movie", eingabe, 0)
		if err != nil || len(c) != 1 || c[0].SourceID != "42" {
			t.Errorf("%s: %+v %v", eingabe, c, err)
		}
	}
	if _, err := r.Kandidaten(ctx, "series", "https://www.themoviedb.org/movie/42", 0); err == nil {
		t.Error("Film-Link bei einer Serie sollte eine klare Meldung geben")
	}
	// Ein gewöhnlicher Titel wird weiter gesucht.
	if c, err := r.Kandidaten(ctx, "movie", "Die Bucht", 0); err != nil || len(c) != 1 {
		t.Errorf("Suche: %+v %v", c, err)
	}
}
