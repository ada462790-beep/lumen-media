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
	"strings"
	"sync"
	"testing"

	"lumen/internal/store"
)

// folgenQuelle stellt TMDb für durchgezählte, Tages- und Doppelfolgen,
// NFO-Kennungen und gleichnamige Serien nach. gefragt zählt die Pfade.
func folgenQuelle(t *testing.T) (*httptest.Server, func(string) int) {
	t.Helper()
	var mu sync.Mutex
	gefragt := map[string]int{}
	antworte := func(w http.ResponseWriter, v any) { _ = json.NewEncoder(w).Encode(v) }
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gefragt[r.URL.Path]++
		mu.Unlock()
		p := r.URL.Path
		switch {
		// One Piece: Specials, dann 61, 16, 14 Folgen.
		case p == "/tv/9":
			antworte(w, map[string]any{"id": 9, "name": "One Piece", "first_air_date": "1999-10-20",
				"seasons": []map[string]any{
					{"season_number": 0, "episode_count": 5},
					{"season_number": 1, "episode_count": 61},
					{"season_number": 2, "episode_count": 16},
					{"season_number": 3, "episode_count": 14}}})
		case p == "/tv/9/season/2/episode/9":
			antworte(w, map[string]any{"name": "Folge 70", "overview": "Abgebildet.", "air_date": "2001-05-01", "runtime": 24})
		case p == "/tv/9/season/1/episode/16":
			antworte(w, map[string]any{"name": "Teil A", "overview": "A.", "runtime": 22})
		case p == "/tv/9/season/1/episode/17":
			antworte(w, map[string]any{"name": "Teil B", "overview": "B.", "runtime": 22})
		// Tägliche Sendung, Staffeln nach Jahren.
		case p == "/tv/10":
			antworte(w, map[string]any{"id": 10, "name": "Tagesschau",
				"seasons": []map[string]any{
					{"season_number": 1, "episode_count": 300, "air_date": "2023-01-02", "name": "2023"},
					{"season_number": 2, "episode_count": 300, "air_date": "2024-01-02", "name": "2024"}}})
		case p == "/tv/10/season/2":
			antworte(w, map[string]any{"episodes": []map[string]any{
				{"episode_number": 51, "air_date": "2024-03-14"},
				{"episode_number": 52, "air_date": "2024-03-15"}}})
		case p == "/tv/10/season/2/episode/52":
			antworte(w, map[string]any{"name": "Ausgabe vom 15. März", "air_date": "2024-03-15"})
		// Film über die Kennung aus der NFO.
		case p == "/movie/603":
			antworte(w, map[string]any{"id": 603, "title": "Matrix", "release_date": "1999-03-31",
				"overview": "Neo.", "poster_path": "/m.png"})
		case p == "/search/movie":
			antworte(w, map[string]any{"results": []any{}})
		// Zwei Serien „The Office".
		case p == "/search/tv":
			antworte(w, map[string]any{"results": []map[string]any{
				{"id": 2316, "name": "The Office", "first_air_date": "2005-03-24", "origin_country": []string{"US"}, "popularity": 300},
				{"id": 2996, "name": "The Office", "first_air_date": "2001-07-09", "origin_country": []string{"GB"}, "popularity": 40}}})
		case p == "/tv/2316" || p == "/tv/2996":
			id := strings.TrimPrefix(p, "/tv/")
			antworte(w, map[string]any{"id": id, "name": "The Office"})
		case strings.HasPrefix(p, "/bild/"):
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte("PNG"))
		default:
			w.WriteHeader(http.StatusNotFound)
			antworte(w, map[string]string{"status_message": "nix"})
		}
	}))
	t.Cleanup(srv.Close)
	return srv, func(p string) int { mu.Lock(); defer mu.Unlock(); return gefragt[p] }
}

func folgenResolver(t *testing.T, st *store.Store, srv *httptest.Server) *Resolver {
	t.Helper()
	r, err := NewResolver(st, t.TempDir(), Config{TMDbKey: "schluessel", Language: "de-DE", Region: "CH"})
	if err != nil {
		t.Fatal(err)
	}
	r.tmdb.BaseURL, r.tmdb.ImageURL = srv.URL, srv.URL+"/bild"
	return r
}

// serieMitFolge legt Serie (schon zugeordnet), Staffel und Folge an.
func serieMitFolge(t *testing.T, st *store.Store, libID, tmdb string, staffel, folge int, premiere string) *store.Item {
	t.Helper()
	ctx := context.Background()
	se := &store.Item{LibraryID: libID, Kind: "series", Title: "S", MatchState: "matched"}
	_ = st.UpsertItem(ctx, se)
	_ = st.SetSourceID(ctx, se.ID, "tmdb", tmdb)
	sf := &store.Item{LibraryID: libID, Kind: "season", ParentID: se.ID, Title: "St", SeasonNo: staffel, MatchState: "matched"}
	_ = st.UpsertItem(ctx, sf)
	ep := &store.Item{LibraryID: libID, Kind: "episode", ParentID: sf.ID, Title: "Folge", SeasonNo: staffel,
		EpisodeNo: folge, Premiered: premiere, MatchState: "pending"}
	_ = st.UpsertItem(ctx, ep)
	return ep
}

func TestAbsolutAbbilden(t *testing.T) {
	staffeln := []StaffelInfo{{Nr: 0, Folgen: 5}, {Nr: 1, Folgen: 61}, {Nr: 2, Folgen: 16}, {Nr: 3, Folgen: 14}}
	for abs, want := range map[int][2]int{1: {1, 1}, 61: {1, 61}, 62: {2, 1}, 70: {2, 9}, 77: {2, 16}, 78: {3, 1}, 91: {3, 14}} {
		s, f, ok := AbsolutAbbilden(staffeln, abs)
		if !ok || s != want[0] || f != want[1] {
			t.Errorf("%d → S%dE%d (%v), erwartet S%dE%d", abs, s, f, ok, want[0], want[1])
		}
	}
	if _, _, ok := AbsolutAbbilden(staffeln, 92); ok {
		t.Error("92 gibt es nicht")
	}
}

// 1., 2. und 13.: Folgen, die bei TMDb anders heissen als in der Datei.
func TestFolgenAbbilden(t *testing.T) {
	ctx := context.Background()
	srv, _ := folgenQuelle(t)
	st := testStore(t)
	lib, _ := st.CreateLibrary(ctx, "Serien", "series", t.TempDir(), "de")
	r := folgenResolver(t, st, srv)

	anime := serieMitFolge(t, st, lib.ID, "9", 1, 70, "")
	_ = st.ZusatzSetzen(ctx, anime.ID, store.Zusatz{Absolut: true})
	tages := serieMitFolge(t, st, lib.ID, "10", 2024, 315, "2024-03-15")
	doppel := serieMitFolge(t, st, lib.ID, "9", 1, 16, "")
	_ = st.ZusatzSetzen(ctx, doppel.ID, store.Zusatz{BisFolge: 17})

	if _, err := r.ResolvePending(ctx, lib.ID, 50); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]string{anime.ID: "Folge 70", tages.ID: "Ausgabe vom 15. März", doppel.ID: "Teil A / Teil B"} {
		it, _ := st.Item(ctx, id)
		if it.Title != want || it.MatchState != "matched" {
			t.Errorf("%s: %q %s, erwartet %q", id, it.Title, it.MatchState, want)
		}
	}
	// Die Folge bleibt, wo der Dateiname sie hingestellt hat.
	if it, _ := st.Item(ctx, anime.ID); it.SeasonNo != 1 || it.EpisodeNo != 70 {
		t.Errorf("Anime-Folge verschoben: S%dE%d", it.SeasonNo, it.EpisodeNo)
	}
	if it, _ := st.Item(ctx, doppel.ID); it.RuntimeMS != 44*60_000 || it.Overview != "A.\n\nB." {
		t.Errorf("Doppelfolge: %d %q", it.RuntimeMS, it.Overview)
	}
}

// 15. Die Kennung aus der NFO ersetzt die Suche; die NFO-Werte gelten
// vor TMDb, wenn der Schalter an ist; lokale Bilder bleiben.
func TestNFOKennungUndVorrang(t *testing.T) {
	for _, bevorzugen := range []bool{true, false} {
		t.Run(map[bool]string{true: "bevorzugen", false: "TMDb"}[bevorzugen], func(t *testing.T) {
			ctx := context.Background()
			srv, gefragt := folgenQuelle(t)
			st := testStore(t)
			if !bevorzugen {
				_ = st.SetSetting(ctx, EinstellungNFOBevorzugen, "0")
			}
			lib, _ := st.CreateLibrary(ctx, "Filme", "movie", t.TempDir(), "de")
			it := &store.Item{LibraryID: lib.ID, Kind: "movie", Title: "Die Matrix", Year: 1999, MatchState: "pending"}
			_ = st.UpsertItem(ctx, it)
			_ = st.LokaleNFOSetzen(ctx, it.ID, store.LokaleNFO{Pfad: "/x/movie.nfo", MTime: 1,
				Daten: `{"art":"movie","titel":"Matrix (Kinofassung)","inhalt":"Aus der NFO.","tmdb":"603"}`})
			_ = st.LokalesBildSetzen(ctx, it.ID, "poster", it.ID+"-poster-lokal.jpg", store.LokalesBild{Pfad: "/x/poster.jpg", MTime: 1, Groesse: 1})

			r := folgenResolver(t, st, srv)
			if _, err := r.ResolvePending(ctx, lib.ID, 10); err != nil {
				t.Fatal(err)
			}
			if gefragt("/search/movie") != 0 || gefragt("/movie/603") == 0 {
				t.Errorf("gesucht statt Kennung genommen: Suche %d, Kennung %d", gefragt("/search/movie"), gefragt("/movie/603"))
			}
			got, _ := st.Item(ctx, it.ID)
			wantTitel, wantInhalt := "Matrix (Kinofassung)", "Aus der NFO."
			if !bevorzugen {
				wantTitel, wantInhalt = "Matrix", "Neo."
			}
			if got.MatchState != "matched" || got.Title != wantTitel || got.Overview != wantInhalt {
				t.Errorf("%s %q %q", got.MatchState, got.Title, got.Overview)
			}
			if id, _ := st.SourceID(ctx, it.ID, "tmdb"); id != "603" {
				t.Errorf("Kennung %q", id)
			}
			if a, _ := st.ArtworkFor(ctx, []string{it.ID}); a[it.ID]["poster"] != it.ID+"-poster-lokal.jpg" {
				t.Errorf("lokales Poster ersetzt: %v", a[it.ID])
			}
			if gefragt("/bild/w500/m.png") != 0 {
				t.Error("TMDb-Poster trotz lokalem Poster geholt")
			}
		})
	}
}

// 4. „The Office (UK)" findet die britische Serie, auch wenn die
// amerikanische bekannter ist.
func TestSerieNachLand(t *testing.T) {
	ctx := context.Background()
	srv, _ := folgenQuelle(t)
	st := testStore(t)
	lib, _ := st.CreateLibrary(ctx, "Serien", "series", t.TempDir(), "de")
	uk := &store.Item{LibraryID: lib.ID, Kind: "series", Title: "The Office", MatchState: "pending"}
	us := &store.Item{LibraryID: lib.ID, Kind: "series", Title: "The Office", MatchState: "pending"}
	_ = st.UpsertItem(ctx, uk)
	_ = st.UpsertItem(ctx, us)
	_ = st.ZusatzSetzen(ctx, uk.ID, store.Zusatz{Land: "GB"})
	_ = st.ZusatzSetzen(ctx, us.ID, store.Zusatz{Land: "US"})
	r := folgenResolver(t, st, srv)
	if _, err := r.ResolvePending(ctx, lib.ID, 10); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]string{uk.ID: "2996", us.ID: "2316"} {
		if got, _ := st.SourceID(ctx, id, "tmdb"); got != want {
			t.Errorf("%s: TMDb %q, erwartet %q", id, got, want)
		}
	}
}

// Findet TMDb nichts, bleibt ein Titel mit vollständiger NFO „lokal"
// erkannt statt „nicht erkannt".
func TestUnbekanntMitNFOBleibtLokal(t *testing.T) {
	ctx := context.Background()
	srv, _ := folgenQuelle(t)
	st := testStore(t)
	lib, _ := st.CreateLibrary(ctx, "Filme", "movie", t.TempDir(), "de")
	it := &store.Item{LibraryID: lib.ID, Kind: "movie", Title: "Urlaub", Year: 2019, MatchState: "pending"}
	_ = st.UpsertItem(ctx, it)
	_ = st.LokaleNFOSetzen(ctx, it.ID, store.LokaleNFO{Pfad: "/x/movie.nfo", MTime: 1,
		Daten: `{"art":"movie","titel":"Sommer in Ligurien","inhalt":"Am Meer.","jahr":2019}`})
	if _, err := folgenResolver(t, st, srv).ResolvePending(ctx, lib.ID, 10); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.Item(ctx, it.ID); got.MatchState != "lokal" || got.Title != "Sommer in Ligurien" {
		t.Errorf("%s %q", got.MatchState, got.Title)
	}
}
