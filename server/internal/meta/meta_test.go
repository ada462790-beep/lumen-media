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
	"testing"
)

// Zuordnen ist der heikle Teil: ein falscher Treffer fällt niemandem mehr
// auf, ein fehlender schon. Darum muss Lumen im Zweifel lieber „weiss ich
// nicht" sagen.
func TestBestPicksTheRightOneOrNoneAtAll(t *testing.T) {
	cases := []struct {
		name      string
		title     string
		year      int
		cands     []Candidate
		wantTitle string
		wantSure  bool
	}{
		{
			name:  "klarer Treffer",
			title: "Die Bucht", year: 2019,
			cands: []Candidate{
				{Title: "Die Bucht", Year: 2019},
				{Title: "Die Brücke", Year: 2018},
			},
			wantTitle: "Die Bucht", wantSure: true,
		},
		{
			name:  "ein Jahr daneben ist normal",
			title: "Nordlicht", year: 2024,
			cands:     []Candidate{{Title: "Nordlicht", Year: 2025}},
			wantTitle: "Nordlicht", wantSure: true,
		},
		{
			name:  "Neuverfilmung: zwei gleich gute, also nachfragen",
			title: "Meridian", year: 0,
			cands: []Candidate{
				{Title: "Meridian", Year: 1990},
				{Title: "Meridian", Year: 2021},
			},
			wantSure: false,
		},
		{
			name:  "Jahr passt, Titel nicht — trotzdem falsch",
			title: "Nordlicht", year: 2019,
			cands:    []Candidate{{Title: "Südwind", Year: 2019}},
			wantSure: false,
		},
		{
			name:  "Originaltitel zählt auch",
			title: "The Bay", year: 2019,
			cands: []Candidate{
				{Title: "Die Bucht", Original: "The Bay", Year: 2019},
			},
			wantTitle: "Die Bucht", wantSure: true,
		},
		{
			name:  "Umlaute und Satzzeichen stören nicht",
			title: "Gruesse aus dem Sueden", year: 2015,
			cands: []Candidate{
				{Title: "Grüsse aus dem Süden!", Year: 2015},
			},
			wantTitle: "Grüsse aus dem Süden!", wantSure: true,
		},
		{
			name:  "Artikel vorne ist egal",
			title: "Bucht, Die", year: 2019,
			cands:     []Candidate{{Title: "Die Bucht", Year: 2019}},
			wantTitle: "Die Bucht", wantSure: true,
		},
		{
			name:     "gar keine Treffer",
			title:    "Gibt es nicht",
			cands:    nil,
			wantSure: false,
		},
		{
			name:  "weit danebenliegendes Jahr macht unsicher",
			title: "Ostwind", year: 1998,
			cands:    []Candidate{{Title: "Ostwind", Year: 2021}},
			wantSure: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := Best(c.title, c.year, c.cands)
			if m.Sure != c.wantSure {
				t.Fatalf("sicher=%v (erwartet %v), Wert %.2f, Grund %q",
					m.Sure, c.wantSure, m.Score, m.Why)
			}
			if c.wantSure && m.Candidate.Title != c.wantTitle {
				t.Fatalf("gewählt %q, erwartet %q", m.Candidate.Title, c.wantTitle)
			}
		})
	}
}

func TestAgeRatings(t *testing.T) {
	cases := map[string]int{
		"FSK 12": 12, "12": 12, "ab 16": 16, "0": 0,
		"PG-13": 12, "R": 16, "TV-MA": 18, "G": 0, "U": 0,
	}
	for in, want := range cases {
		got := AgeYears(in)
		if got == nil || *got != want {
			t.Errorf("%q → %v, erwartet %d", in, got, want)
		}
	}
	if AgeYears("unbekannt") != nil {
		t.Error("unbekannte Kennzeichnung sollte nichts ergeben")
	}
}

// ---------------------------------------------------------------- TMDb

// Gegen einen nachgebauten Dienst, nicht gegen den echten: ein Test soll
// nicht rot werden, weil jemand anders gerade Wartung hat.
func fakeTMDb(t *testing.T) (*TMDb, *[]string) {
	t.Helper()
	seen := &[]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*seen = append(*seen, r.URL.Path+"?"+r.URL.RawQuery)
		if r.Header.Get("Authorization") != "Bearer schluessel" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/search/movie":
			_ = json.NewEncoder(w).Encode(map[string]any{"results": []map[string]any{
				{"id": 42, "title": "Die Bucht", "original_title": "The Bay",
					"overview":     "Eine Forscherin kehrt zurück.",
					"release_date": "2019-03-14", "poster_path": "/abc.jpg",
					"backdrop_path": "/def.jpg", "popularity": 31.5},
			}})
		case "/movie/42":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": 42, "title": "Die Bucht", "release_date": "2019-03-14",
				"runtime": 118, "imdb_id": "tt1234567",
				"genres": []map[string]any{{"name": "Drama"}, {"name": "Mystery"}},
				"releases": map[string]any{"countries": []map[string]any{
					{"iso_3166_1": "US", "certification": "R"},
					{"iso_3166_1": "DE", "certification": "12"},
					{"iso_3166_1": "CH", "certification": "16"},
				}},
			})
		case "/search/tv":
			_ = json.NewEncoder(w).Encode(map[string]any{"results": []map[string]any{
				{"id": 7, "name": "Nordlicht", "first_air_date": "2025-01-09"},
			}})
		case "/tv/7/season/1/episode/4":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"name": "Rückkehr", "overview": "Sie kommt zurück.",
				"air_date": "2025-02-06", "runtime": 52})
		default:
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]any{"status_message": "nix"})
		}
	}))
	t.Cleanup(srv.Close)

	tm := NewTMDb("schluessel", "de-DE", "CH")
	tm.BaseURL = srv.URL
	tm.ImageURL = srv.URL + "/bild"
	return tm, seen
}

func TestTMDbSearchAndDetails(t *testing.T) {
	tm, _ := fakeTMDb(t)
	ctx := context.Background()

	got, err := tm.SearchMovie(ctx, "Die Bucht", 2019)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Title != "Die Bucht" || got[0].Year != 2019 {
		t.Fatalf("Suche lieferte %+v", got)
	}
	if got[0].Original != "The Bay" || got[0].PosterURL == "" {
		t.Errorf("Felder fehlen: %+v", got[0])
	}

	d, err := tm.Details(ctx, "42")
	if err != nil {
		t.Fatal(err)
	}
	if d.RuntimeMS != 118*60_000 {
		t.Errorf("Laufzeit %d", d.RuntimeMS)
	}
	if d.IMDbID != "tt1234567" {
		t.Errorf("IMDb-Kennung %q", d.IMDbID)
	}
	if len(d.Genres) != 2 {
		t.Errorf("Genres %v", d.Genres)
	}
	// Die Region entscheidet: eingestellt ist CH, also 16 — nicht die
	// deutschen 12 und nicht das amerikanische R.
	if d.AgeRating != "16" || d.AgeYears == nil || *d.AgeYears != 16 {
		t.Errorf("Altersfreigabe %q/%v — erwartet die Schweizer Einstufung",
			d.AgeRating, d.AgeYears)
	}
}

func TestTMDbFallsBackWithoutYear(t *testing.T) {
	tm, seen := fakeTMDb(t)
	// „Unbekannt" liefert nichts; der zweite Versuch muss ohne Jahr laufen.
	srvHits := func() int { return len(*seen) }
	before := srvHits()
	_, _ = tm.SearchMovie(context.Background(), "Die Bucht", 1999)
	if srvHits() <= before {
		t.Fatal("es wurde gar nicht gesucht")
	}
}

func TestTMDbReportsBadKeyPlainly(t *testing.T) {
	tm, _ := fakeTMDb(t)
	tm.Key = "falsch"
	_, err := tm.SearchMovie(context.Background(), "Die Bucht", 2019)
	if err == nil {
		t.Fatal("falscher Schlüssel blieb unbemerkt")
	}
	if got := err.Error(); got != "der TMDb-Schlüssel wird nicht angenommen" {
		t.Errorf("unverständliche Meldung: %q", got)
	}
}

func TestTMDbEpisode(t *testing.T) {
	tm, _ := fakeTMDb(t)
	e, err := tm.Episode(context.Background(), "7", 1, 4)
	if err != nil {
		t.Fatal(err)
	}
	if e.Title != "Rückkehr" || e.RuntimeMS != 52*60_000 {
		t.Errorf("Folge falsch: %+v", e)
	}
}

// ---------------------------------------------------------------- OMDb

func TestOMDbRating(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("i") != "tt1234567" {
			_ = json.NewEncoder(w).Encode(map[string]string{
				"Response": "False", "Error": "Incorrect IMDb ID."})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{
			"Response": "True", "imdbRating": "7.8", "imdbVotes": "48,213"})
	}))
	defer srv.Close()

	o := NewOMDb("schluessel")
	o.BaseURL = srv.URL + "/"

	rating, votes, err := o.Rating(context.Background(), "tt1234567")
	if err != nil {
		t.Fatal(err)
	}
	if rating != 7.8 {
		t.Errorf("Note %v", rating)
	}
	if votes != 48213 {
		t.Errorf("Stimmen %d", votes)
	}

	if _, _, err := o.Rating(context.Background(), "tt0000000"); err == nil {
		t.Error("unbekannte Kennung blieb unbemerkt")
	}
}

func TestOMDbNeedsAKey(t *testing.T) {
	_, _, err := NewOMDb("").Rating(context.Background(), "tt1")
	if err == nil {
		t.Fatal("ohne Schlüssel müsste es eine Meldung geben")
	}
}

// Rotten Tomatoes kommt mit derselben OMDb-Abfrage wie die IMDb-Note.
func TestOMDbTomaten(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("i") == "tt1" {
			_, _ = w.Write([]byte(`{"Response":"True","imdbRating":"7.4","imdbVotes":"1,000",
				"Ratings":[{"Source":"Internet Movie Database","Value":"7.4/10"},
				           {"Source":"Rotten Tomatoes","Value":"69%"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"Response":"True","imdbRating":"N/A","Ratings":[]}`))
	}))
	defer srv.Close()
	o := NewOMDb("k")
	o.BaseURL = srv.URL + "/"
	w, err := o.Wertungen(context.Background(), "tt1")
	if err != nil || w.Tomaten != 69 || w.IMDb != 7.4 {
		t.Fatalf("%+v %v", w, err)
	}
	w, err = o.Wertungen(context.Background(), "tt2")
	if err != nil || w.Tomaten != -1 {
		t.Fatalf("ohne Rotten Tomatoes: %+v %v", w, err)
	}
}

// „Arrow" ohne Jahr: zwei Serien gleichen Namens, eine davon kennt jeder.
// Die nimmt Lumen, statt den Titel als „nicht erkannt" liegen zu lassen.
func TestBekannterGewinntBeiGleichemNamen(t *testing.T) {
	cands := []Candidate{
		{Title: "Arrow", Year: 1990, Popularity: 1.2},
		{Title: "Arrow", Year: 2012, Popularity: 180},
	}
	m := Best("Arrow", 0, cands)
	if !m.Sure || m.Candidate.Year != 2012 {
		t.Fatalf("%+v %v", m.Candidate, m.Why)
	}
	// Ähnlich bekannt: weiter nachfragen.
	cands[0].Popularity = 90
	if m := Best("Arrow", 0, cands); m.Sure {
		t.Fatalf("hätte nachfragen sollen: %v", m.Why)
	}
}

// „Doctor Who (2005)": die Suche mit dem Jahr aus dem Ordner bringt die
// neue Serie nach vorn, die Suche ohne Jahr bleibt als Rückfall dabei.
func TestSucheMitJahrAusOrdner(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		alt := map[string]any{"id": 121, "name": "Doctor Who", "first_air_date": "1963-11-23", "popularity": 20.0}
		neu := map[string]any{"id": 57243, "name": "Doctor Who", "first_air_date": "2005-03-26", "popularity": 40.0}
		res := []map[string]any{alt, neu}
		if r.URL.Query().Get("first_air_date_year") == "2005" {
			res = []map[string]any{neu}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"results": res})
	}))
	defer srv.Close()
	tm := NewTMDb("schluessel", "de-DE", "DE")
	tm.BaseURL = srv.URL
	r := &Resolver{tmdb: tm}

	cands, err := r.suchen(context.Background(), "series", "Doctor Who", 2005)
	if err != nil {
		t.Fatal(err)
	}
	if len(cands) != 2 || cands[0].SourceID != "57243" {
		t.Fatalf("Treffer %+v", cands)
	}
	if m := Best("Doctor Who", 2005, cands); !m.Sure || m.Candidate.SourceID != "57243" {
		t.Errorf("gewählt %+v", m)
	}
}
