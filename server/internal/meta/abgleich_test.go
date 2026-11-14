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
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode"

	"lumen/internal/store"
)

// ---------------------------------------------------------------- Varianten

func TestSuchvarianten(t *testing.T) {
	texte := func(v []Variante) []string {
		var out []string
		for _, x := range v {
			out = append(out, x.Text)
		}
		return out
	}
	hat := func(v []Variante, s string, gleich bool) bool {
		for _, x := range v {
			if x.Text == s && x.Gleichwertig == gleich {
				return true
			}
		}
		return false
	}
	faelle := []struct {
		titel  string
		soll   string
		gleich bool
	}{
		{"Avatar Extended Collector's Edition 3D", "Avatar", true},
		// „Teil N" so, wie TMDb solche Filme nennt
		{"Dune - Der Wüstenplanet - Teil 3", "Dune - Der Wüstenplanet 3", true},
		{"Dune: Teil 2", "Dune: Part Two", true},
		{"Kill Bill - Teil 2", "Kill Bill: Vol. 2", true},
		{"Die Pfeiler der Macht: Teil 2", "Die Pfeiler der Macht 2", true},
		{"Die Pfeiler der Macht: Teil 2", "Die Pfeiler der Macht", false},
		{"Harry Potter und die Heiligtümer des Todes Part 2", "Harry Potter und die Heiligtümer des Todes: Teil 2", true},
		{"Das Boot Director's Cut", "Das Boot", true},
		{"Das Boot - Kinofassung", "Das Boot", true},
		{"Blade Runner Final Cut", "Blade Runner", true},
		{"Titanic Remastered", "Titanic", true},
		{"Herr der Ringe, Der: Die Gefährten", "Der Herr der Ringe: Die Gefährten", true},
		{"Bucht, Die", "Die Bucht", true},
		{"Fast & Furious", "Fast und Furious", true},
		{"Fast & Furious", "Fast and Furious", true},
		{"Fast and Furious 6", "Fast & Furious 6", true},
		{"Rocky 2", "Rocky II", true},
		{"Rocky II", "Rocky 2", true},
		{"Sissi Teil III", "Sissi Teil 3", true},
		{"Fack ju Goehte 2", "Fack ju Göhte 2", true},
		{"Die Schöne und das Biest", "Die Schoene und das Biest", true},
		{"Mission: Impossible", "Mission Impossible", true},
		{"Star Trek II - Der Zorn des Khan", "Star Trek II", false},
		{"Star Trek II: Der Zorn des Khan", "Star Trek II", false},
	}
	for _, f := range faelle {
		v := Suchvarianten(f.titel)
		if v[0].Text != f.titel || !v[0].Gleichwertig {
			t.Errorf("%q: der Titel selbst kommt nicht zuerst: %q", f.titel, texte(v))
		}
		if !hat(v, f.soll, f.gleich) {
			t.Errorf("%q: %q (gleichwertig=%v) fehlt in %q", f.titel, f.soll, f.gleich, texte(v))
		}
	}
	// Keine doppelten, und ein kurzer Titel bleibt ein kurzer Titel.
	v := Suchvarianten("Up")
	if len(v) != 1 || v[0].Text != "Up" {
		t.Errorf("Up: %q", texte(v))
	}
	// „Step Up 3D" verliert sein 3D nur als zusätzliche Schreibweise.
	if v := Suchvarianten("Step Up 3D"); v[0].Text != "Step Up 3D" || !hat(v, "Step Up", true) {
		t.Errorf("Step Up 3D: %q", texte(v))
	}
}

// Die Bewertung kennt römische Zahlen, umgestellte Artikel und „and".
func TestScoreSchreibweisen(t *testing.T) {
	faelle := []struct {
		datei, tmdb string
	}{
		{"Rocky 2", "Rocky II"},
		{"Herr der Ringe, Der: Die Gefährten", "Der Herr der Ringe: Die Gefährten"},
		{"Fast and Furious 6", "Fast & Furious 6"},
		{"Fack ju Goehte 2", "Fack ju Göhte 2"},
		{"Ocean's Eleven", "Oceans Eleven"},
	}
	for _, f := range faelle {
		if s := Score(f.datei, 2000, Candidate{Title: f.tmdb, Year: 2000}); s < minSure {
			t.Errorf("%q gegen %q: %.2f", f.datei, f.tmdb, s)
		}
	}
	// Eine andere Fortsetzung ist nicht dieselbe.
	if s := Score("Der Pate 4", 0, Candidate{Title: "Der Pate 2"}); s >= minSure {
		t.Errorf("Der Pate 4 gegen Der Pate 2: %.2f", s)
	}
	// Alternativtitel zählen.
	if s := Score("Late Bloomers", 2006, Candidate{Title: "Die Herbstzeitlosen", Year: 2006, Alternativ: []string{"Late Bloomers"}}); s < minSure {
		t.Errorf("Alternativtitel: %.2f", s)
	}
}

// ---------------------------------------------------------------- Kennungen

func TestKennungLesen(t *testing.T) {
	faelle := []struct {
		ein  string
		soll Eingabe
	}{
		{"tt0133093", Eingabe{Quelle: "imdb", ID: "tt0133093"}},
		{" TT0133093 ", Eingabe{Quelle: "imdb", ID: "tt0133093"}},
		{"https://www.imdb.com/title/tt0133093/", Eingabe{Quelle: "imdb", ID: "tt0133093"}},
		{"https://m.imdb.com/title/tt0133093/?ref_=nv_sr_srsg_0", Eingabe{Quelle: "imdb", ID: "tt0133093"}},
		{"imdb.com/de/title/tt0944947/episodes", Eingabe{Quelle: "imdb", ID: "tt0944947"}},
		{"603", Eingabe{Quelle: "tmdb", ID: "603"}},
		{"https://www.themoviedb.org/movie/603-the-matrix", Eingabe{Quelle: "tmdb", Art: "movie", ID: "603"}},
		{"https://www.themoviedb.org/movie/603", Eingabe{Quelle: "tmdb", Art: "movie", ID: "603"}},
		{"https://www.themoviedb.org/tv/1399-game-of-thrones?language=de", Eingabe{Quelle: "tmdb", Art: "tv", ID: "1399"}},
		{"https://www.themoviedb.org/de/tv/1399", Eingabe{Quelle: "tmdb", Art: "tv", ID: "1399"}},
		{"themoviedb.org/movie/603-the-matrix/cast", Eingabe{Quelle: "tmdb", Art: "movie", ID: "603"}},
		{"tmdb:603", Eingabe{Quelle: "tmdb", ID: "603"}},
		{"{tmdb-603}", Eingabe{Quelle: "tmdb", ID: "603"}},
		{"[tmdbid-603]", Eingabe{Quelle: "tmdb", ID: "603"}},
		{"imdb: tt0133093", Eingabe{Quelle: "imdb", ID: "tt0133093"}},
		{"https://thetvdb.com/?tab=series&id=121361", Eingabe{Quelle: "tvdb", ID: "121361"}},
		{"https://thetvdb.com/index.php?tab=series&id=121361&lid=14", Eingabe{Quelle: "tvdb", ID: "121361"}},
		{"https://thetvdb.com/dereferrer/series/121361", Eingabe{Quelle: "tvdb", ID: "121361"}},
		{"https://www.thetvdb.com/series/121361", Eingabe{Quelle: "tvdb", ID: "121361"}},
		{"https://thetvdb.com/series/game-of-thrones", Eingabe{Quelle: "tvdb", Slug: "game-of-thrones"}},
		{"https://thetvdb.com/series/game-of-thrones/seasons/official/1", Eingabe{Quelle: "tvdb", Slug: "game-of-thrones"}},
		{"tvdb:121361", Eingabe{Quelle: "tvdb", ID: "121361"}},
		{"{tvdb-121361}", Eingabe{Quelle: "tvdb", ID: "121361"}},
		{"[tvdbid-121361]", Eingabe{Quelle: "tvdb", ID: "121361"}},
	}
	for _, f := range faelle {
		e, ok := KennungLesen(f.ein)
		if !ok || e != f.soll {
			t.Errorf("%q: %+v %v, erwartet %+v", f.ein, e, ok, f.soll)
		}
	}
	// Gewöhnliche Titel sind keine Kennungen.
	for _, s := range []string{"1917", "2012", "Matrix", "Ocean's 11", "Apollo 13", "9", ""} {
		if e, ok := KennungLesen(s); ok {
			t.Errorf("%q als Kennung gelesen: %+v", s, e)
		}
	}
}

// ---------------------------------------------------------------- Nachbau

// tmdbFilm ist ein Eintrag im nachgebauten TMDb.
type tmdbFilm struct {
	id           int
	tv           bool
	de, en, orig string
	jahr         int
	alt          []string
	imdb, tvdb   string
	pop          float64
}

// tmdbNachbau verhält sich wie die Suche von TMDb, so weit es hier zählt:
// Ein Treffer braucht alle Wörter der Anfrage in einem Titel (deutsch,
// englisch, Original oder alternativ), Umlaute zählen genau, Fassungswörter
// und römische Zahlen kennt die Suche nicht. Zurück kommt der Titel in der
// gefragten Sprache.
type tmdbNachbau struct {
	filme    []tmdbFilm
	anfragen atomic.Int64
	mu       sync.Mutex
	pfade    []string
	// kaputt: Anfragen mit diesem Wort scheitern mit 500.
	kaputt string
	// bremsen: so viele Anfragen bekommen zuerst 429.
	bremsen atomic.Int64
}

func woerter(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

func enthaeltAlle(titel, anfrage string) bool {
	tw := map[string]bool{}
	for _, w := range woerter(titel) {
		tw[w] = true
	}
	aw := woerter(anfrage)
	if len(aw) == 0 {
		return false
	}
	for _, w := range aw {
		if !tw[w] {
			return false
		}
	}
	return true
}

func (n *tmdbNachbau) server(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.anfragen.Add(1)
		n.mu.Lock()
		n.pfade = append(n.pfade, r.URL.Path+"?"+r.URL.RawQuery)
		n.mu.Unlock()
		if n.bremsen.Load() > 0 {
			n.bremsen.Add(-1)
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		q := r.URL.Query()
		en := strings.HasPrefix(q.Get("language"), "en")
		titel := func(f tmdbFilm) string {
			if en && f.en != "" {
				return f.en
			}
			return f.de
		}
		eintrag := func(f tmdbFilm) map[string]any {
			datum := strconv.Itoa(f.jahr) + "-05-01"
			if f.tv {
				return map[string]any{"id": f.id, "name": titel(f), "original_name": f.orig,
					"first_air_date": datum, "popularity": f.pop}
			}
			return map[string]any{"id": f.id, "title": titel(f), "original_title": f.orig,
				"release_date": datum, "popularity": f.pop, "imdb_id": f.imdb}
		}
		pfad := r.URL.Path
		switch {
		case pfad == "/search/movie" || pfad == "/search/tv":
			anfrage := q.Get("query")
			if n.kaputt != "" && strings.Contains(anfrage, n.kaputt) {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			jahr, _ := strconv.Atoi(q.Get("year") + q.Get("first_air_date_year"))
			out := []map[string]any{}
			for _, f := range n.filme {
				if f.tv != (pfad == "/search/tv") || (jahr > 0 && f.jahr != jahr) {
					continue
				}
				passt := false
				for _, t := range append([]string{f.de, f.en, f.orig}, f.alt...) {
					passt = passt || (t != "" && enthaeltAlle(t, anfrage))
				}
				if passt {
					out = append(out, eintrag(f))
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"results": out})
		case strings.HasPrefix(pfad, "/find/"):
			kennung := strings.TrimPrefix(pfad, "/find/")
			filme, serien := []map[string]any{}, []map[string]any{}
			for _, f := range n.filme {
				if (q.Get("external_source") == "imdb_id" && f.imdb == kennung) ||
					(q.Get("external_source") == "tvdb_id" && f.tvdb == kennung) {
					if f.tv {
						serien = append(serien, map[string]any{"id": f.id})
					} else {
						filme = append(filme, map[string]any{"id": f.id})
					}
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"movie_results": filme, "tv_results": serien})
		case strings.HasSuffix(pfad, "/alternative_titles"):
			id, _ := strconv.Atoi(strings.Split(pfad, "/")[2])
			var titles []map[string]any
			for _, f := range n.filme {
				if f.id == id {
					for _, a := range f.alt {
						titles = append(titles, map[string]any{"title": a})
					}
				}
			}
			if strings.HasPrefix(pfad, "/tv/") {
				_ = json.NewEncoder(w).Encode(map[string]any{"results": titles})
			} else {
				_ = json.NewEncoder(w).Encode(map[string]any{"titles": titles})
			}
		case strings.HasPrefix(pfad, "/movie/") || strings.HasPrefix(pfad, "/tv/"):
			id, _ := strconv.Atoi(strings.Split(pfad, "/")[2])
			for _, f := range n.filme {
				if f.id == id && f.tv == strings.HasPrefix(pfad, "/tv/") {
					e := eintrag(f)
					if f.tv {
						e["external_ids"] = map[string]any{"imdb_id": f.imdb, "tvdb_id": atoiOder0(f.tvdb)}
					}
					_ = json.NewEncoder(w).Encode(e)
					return
				}
			}
			w.WriteHeader(http.StatusNotFound)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func atoiOder0(s string) int { n, _ := strconv.Atoi(s); return n }

func nachbauResolver(t *testing.T, st *store.Store, srv *httptest.Server) *Resolver {
	t.Helper()
	r, err := NewResolver(st, t.TempDir(), Config{TMDbKey: "schluessel", Language: "de-DE", Region: "CH"})
	if err != nil {
		t.Fatal(err)
	}
	r.tmdb.BaseURL = srv.URL
	r.tmdb.ImageURL = srv.URL + "/bild"
	r.tmdb.limiter = newLimiter(100000, time.Second) // der Nachbau verträgt alles
	return r
}

var nachbauKatalog = []tmdbFilm{
	{id: 603, de: "Matrix", en: "The Matrix", orig: "The Matrix", jahr: 1999, imdb: "tt0133093", pop: 80},
	{id: 120, de: "Der Herr der Ringe: Die Gefährten", en: "The Lord of the Rings: The Fellowship of the Ring",
		orig: "The Lord of the Rings: The Fellowship of the Ring", jahr: 2001, pop: 90},
	{id: 345, de: "Fack ju Göhte 2", orig: "Fack ju Göhte 2", jahr: 2015, pop: 20},
	{id: 1367, de: "Rocky II", en: "Rocky II", orig: "Rocky II", jahr: 1979, pop: 30},
	{id: 19995, de: "Avatar - Aufbruch nach Pandora", en: "Avatar", orig: "Avatar", jahr: 2009, pop: 100},
	{id: 129, de: "Chihiros Reise ins Zauberland", en: "Spirited Away", orig: "千と千尋の神隠し", jahr: 2001, pop: 60},
	{id: 82992, de: "Fast & Furious 6", en: "Fast & Furious 6", orig: "Fast & Furious 6", jahr: 2013, pop: 50},
	{id: 900, de: "Die Herbstzeitlosen", en: "Late Bloomers", orig: "Die Herbstzeitlosen", jahr: 2006,
		alt: []string{"Les mamies font pas dans la dentelle"}, pop: 3},
	{id: 238, de: "Der Pate", orig: "The Godfather", jahr: 1972, pop: 70},
	{id: 240, de: "Der Pate 2", orig: "The Godfather Part II", jahr: 1974, pop: 50},
	{id: 242, de: "Der Pate 3", orig: "The Godfather Part III", jahr: 1990, pop: 40},
	{id: 501, de: "Meridian", orig: "Meridian", jahr: 1990, pop: 2},
	{id: 502, de: "Meridian", orig: "Meridian", jahr: 2016, pop: 2},
	{id: 77, de: "Die Bucht", orig: "The Bay", jahr: 2020, pop: 4},
	{id: 1396, tv: true, de: "Breaking Bad", orig: "Breaking Bad", jahr: 2008, tvdb: "81189", imdb: "tt0903747", pop: 300},
	{id: 1399, tv: true, de: "Game of Thrones", orig: "Game of Thrones", jahr: 2011, tvdb: "121361", imdb: "tt0944947", pop: 400},
	{id: 4614, tv: true, de: "Navy CIS", en: "NCIS", orig: "NCIS", jahr: 2003, pop: 200},
	{id: 66732, tv: true, de: "Stranger Things", orig: "Stranger Things", jahr: 2016, pop: 250},
}

// Die Leiter mit vielen echten Schreibweisen: Was zu finden ist, wird
// gefunden — und was nicht sicher ist, bleibt „nicht erkannt".
func TestLeiterFindetUndRaetNicht(t *testing.T) {
	n := &tmdbNachbau{filme: nachbauKatalog}
	srv := n.server(t)
	st := testStore(t)
	ctx := context.Background()
	r := nachbauResolver(t, st, srv)

	faelle := []struct {
		kind, titel string
		jahr        int
		soll        string // "" = darf nicht zugeordnet werden
	}{
		{"movie", "Matrix", 1999, "603"},
		{"movie", "The Matrix", 1999, "603"},
		{"movie", "Herr der Ringe, Der: Die Gefährten", 2001, "120"},
		{"movie", "Fack ju Goehte 2", 2015, "345"},
		{"movie", "Rocky 2", 1979, "1367"},
		{"movie", "Avatar Extended Collector's Edition 3D", 2009, "19995"},
		{"movie", "Spirited Away", 2001, "129"},
		{"movie", "Fast and Furious 6", 2013, "82992"},
		{"movie", "Les mamies font pas dans la dentelle", 2006, "900"},
		{"movie", "Die Bucht", 2019, "77"}, // ein Jahr daneben
		{"movie", "Der Pate 4", 0, ""},     // gibt es nicht — nicht „Der Pate 2" nehmen
		{"movie", "Meridian", 0, ""},       // zwei gleich gute
		{"movie", "Matrix", 1962, ""},      // Jahr weit daneben
		{"movie", "Ein Film den es nicht gibt", 2020, ""},
		{"series", "Breaking Bad", 2008, "1396"},
		{"series", "NCIS", 0, "4614"},
		{"series", "Stranger Things", 0, "66732"},
	}
	for _, f := range faelle {
		m, g, _, err := r.finden(ctx, f.kind, f.titel, f.jahr, "")
		if err != nil {
			t.Fatalf("%q: %v", f.titel, err)
		}
		switch {
		case f.soll == "" && m.Sure:
			t.Errorf("%q (%d): fälschlich %q (%s) genommen, Wert %.2f", f.titel, f.jahr, m.Candidate.Title, m.Candidate.SourceID, m.Score)
		case f.soll != "" && (!m.Sure || m.Candidate.SourceID != f.soll):
			got := ""
			if m.Candidate != nil {
				got = m.Candidate.SourceID + " " + m.Candidate.Title
			}
			t.Errorf("%q (%d): %q sicher=%v Wert %.2f %s, erwartet %s; gesucht %q", f.titel, f.jahr, got, m.Sure, m.Score, m.Why, f.soll, g.Texte)
		}
		if g.Anfragen > maxAnfragen {
			t.Errorf("%q: %d Anfragen", f.titel, g.Anfragen)
		}
	}
	// Ein klarer Fall kostet höchstens zwei Anfragen (mit und ohne Jahr).
	vorher := n.anfragen.Load()
	if m, _, _, _ := r.finden(ctx, "movie", "Matrix", 1999, ""); !m.Sure {
		t.Fatal("Matrix nicht sicher")
	}
	if d := n.anfragen.Load() - vorher; d > 2 {
		t.Errorf("klarer Fall: %d Anfragen", d)
	}
}

// Ein Fehler bei TMDb macht den Titel nicht zu „nicht erkannt": Er bleibt
// ausstehend und kommt wieder dran.
func TestFehlerBleibtAusstehend(t *testing.T) {
	n := &tmdbNachbau{filme: nachbauKatalog, kaputt: "Zauberland"}
	srv := n.server(t)
	st := testStore(t)
	ctx := context.Background()
	lib, _ := st.CreateLibrary(ctx, "Filme", "movie", t.TempDir(), "de")
	it := &store.Item{LibraryID: lib.ID, Kind: "movie", Title: "Chihiros Reise ins Zauberland", Year: 2001, MatchState: "pending"}
	if err := st.UpsertItem(ctx, it); err != nil {
		t.Fatal(err)
	}
	r := nachbauResolver(t, st, srv)
	res, err := r.ResolvePending(ctx, lib.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed != 1 || res.Unmatched != 0 {
		t.Fatalf("%+v", res)
	}
	got, _ := st.Item(ctx, it.ID)
	if got.MatchState != "pending" {
		t.Errorf("Zustand %q — ein Netzfehler darf nicht als „nicht erkannt“ gelten", got.MatchState)
	}
	if g, ok := GrundZu(it.ID); !ok || g.Art != "fehler" {
		t.Errorf("Grund %+v %v", g, ok)
	}
	// Das Netz ist wieder da: beim nächsten Mal klappt es.
	n.kaputt = ""
	if res, _ := r.ResolvePending(ctx, lib.ID, 10); res.Matched != 1 {
		t.Fatalf("zweiter Durchgang: %+v", res)
	}
	if _, ok := GrundZu(it.ID); ok {
		t.Error("Grund nach dem Zuordnen nicht vergessen")
	}
}

// TMDb bremst (429): einmal warten, dann noch einmal fragen.
func TestBremseWirdAbgewartet(t *testing.T) {
	n := &tmdbNachbau{filme: nachbauKatalog}
	n.bremsen.Store(1)
	srv := n.server(t)
	r := nachbauResolver(t, testStore(t), srv)
	c, err := r.tmdb.SearchMovie(context.Background(), "Matrix", 1999)
	if err != nil || len(c) != 1 {
		t.Fatalf("%v %+v", err, c)
	}
	// Dauernd gebremst: ein klarer Fehler, kein leeres Ergebnis.
	n.bremsen.Store(5)
	if _, err := r.tmdb.SearchMovie(context.Background(), "Matrix", 1999); err == nil {
		t.Fatal("kein Fehler bei dauernder Bremse")
	}
	var g *gebremst
	if _, err := r.tmdb.SearchMovie(context.Background(), "Matrix", 0); !errors.As(err, &g) {
		t.Fatalf("Fehler %v", err)
	}
}

// Nicht erkannt: Der Grund steht für die Verwaltung bereit.
func TestGrundFuerNichtErkannt(t *testing.T) {
	n := &tmdbNachbau{filme: nachbauKatalog}
	srv := n.server(t)
	st := testStore(t)
	ctx := context.Background()
	lib, _ := st.CreateLibrary(ctx, "Filme", "movie", t.TempDir(), "de")
	zwei := &store.Item{LibraryID: lib.ID, Kind: "movie", Title: "Meridian", MatchState: "pending"}
	keins := &store.Item{LibraryID: lib.ID, Kind: "movie", Title: "Gibt es nicht", Year: 2001, MatchState: "pending"}
	for _, it := range []*store.Item{zwei, keins} {
		if err := st.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := nachbauResolver(t, st, srv).ResolvePending(ctx, lib.ID, 10); err != nil {
		t.Fatal(err)
	}
	if g, _ := GrundZu(zwei.ID); g.Art != "zu_nah" || !strings.HasPrefix(g.Vorschlag, "Meridian") || len(g.Gesucht) == 0 {
		t.Errorf("zwei Treffer: %+v", g)
	}
	if g, _ := GrundZu(keins.ID); g.Art != "keine_treffer" || g.Anfragen == 0 {
		t.Errorf("keine Treffer: %+v", g)
	}
	list, _ := st.OffeneZuordnungen(ctx, 10)
	if len(list) != 2 {
		t.Errorf("offene Zuordnungen: %d", len(list))
	}
}

// Kennungen im Datei- und Ordnernamen gelten vor jeder Suche.
func TestKennungImNamen(t *testing.T) {
	n := &tmdbNachbau{filme: nachbauKatalog}
	srv := n.server(t)
	st := testStore(t)
	ctx := context.Background()
	wurzel := t.TempDir()
	filme, _ := st.CreateLibrary(ctx, "Filme", "movie", filepath.Join(wurzel, "Filme"), "de")
	serien, _ := st.CreateLibrary(ctx, "Serien", "series", filepath.Join(wurzel, "Serien"), "de")

	datei := func(it *store.Item, pfad string) {
		t.Helper()
		if err := st.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
		if err := st.UpsertFile(ctx, &store.MediaFile{ItemID: it.ID, Path: pfad, SizeBytes: 1}); err != nil {
			t.Fatal(err)
		}
	}
	// Der Titel aus dem Dateinamen führt in die Irre — die Kennung nicht.
	imdb := &store.Item{LibraryID: filme.ID, Kind: "movie", Title: "Irgendwas", MatchState: "pending"}
	datei(imdb, filepath.Join(wurzel, "Filme", "Irgendwas (1999) {imdb-tt0133093}", "Irgendwas.mkv"))
	tmdb := &store.Item{LibraryID: filme.ID, Kind: "movie", Title: "Gefaehrten", MatchState: "pending"}
	datei(tmdb, filepath.Join(wurzel, "Filme", "Gefaehrten [tmdbid-120].mkv"))
	frei := &store.Item{LibraryID: filme.ID, Kind: "movie", Title: "Rocky Zwei", MatchState: "pending"}
	datei(frei, filepath.Join(wurzel, "Filme", "Rocky.Zwei.tt0133093x.German.mkv")) // keine gültige Kennung
	// „Filme {tmdb-…}" über der Bibliothek zählt nicht für jeden Film darin.
	serie := &store.Item{LibraryID: serien.ID, Kind: "series", Title: "BB", MatchState: "pending"}
	if err := st.UpsertItem(ctx, serie); err != nil {
		t.Fatal(err)
	}
	staffel := &store.Item{LibraryID: serien.ID, Kind: "season", ParentID: serie.ID, SeasonNo: 1, MatchState: "matched"}
	if err := st.UpsertItem(ctx, staffel); err != nil {
		t.Fatal(err)
	}
	// Die Folge nennt eine IMDb-Kennung (die der Folge) — die zählt nicht
	// für die Serie, der Ordner schon.
	folge := &store.Item{LibraryID: serien.ID, Kind: "episode", ParentID: staffel.ID, SeasonNo: 1, EpisodeNo: 1, MatchState: "pending"}
	datei(folge, filepath.Join(wurzel, "Serien", "BB (2008) [tvdbid-81189]", "Season 01", "BB S01E01 {imdb-tt0133093}.mkv"))

	r := nachbauResolver(t, st, srv)
	for _, lib := range []string{filme.ID, serien.ID} {
		if _, err := r.ResolvePending(ctx, lib, 20); err != nil {
			t.Fatal(err)
		}
	}
	pruef := func(it *store.Item, quelle, soll string) {
		t.Helper()
		got, _ := st.SourceID(ctx, it.ID, quelle)
		neu, _ := st.Item(ctx, it.ID)
		if got != soll {
			t.Errorf("%s: %s=%q (Zustand %s), erwartet %q", it.Title, quelle, got, neu.MatchState, soll)
		}
	}
	pruef(imdb, "tmdb", "603")
	pruef(tmdb, "tmdb", "120")
	pruef(serie, "tmdb", "1396")
	pruef(serie, "tvdb", "81189")
	if neu, _ := st.Item(ctx, frei.ID); neu.MatchState == "matched" {
		t.Errorf("ohne gültige Kennung zugeordnet: %+v", neu)
	}
}

// Zuweisen über IMDb, TMDb und TVDb — mit Link oder Kennung.
func TestZuweisenUeberKennung(t *testing.T) {
	n := &tmdbNachbau{filme: nachbauKatalog}
	srv := n.server(t)
	r := nachbauResolver(t, testStore(t), srv)
	ctx := context.Background()
	faelle := []struct {
		kind, ein, soll string
		fehler          error
	}{
		{"movie", "tt0133093", "603", nil},
		{"movie", "https://www.imdb.com/title/tt0133093/", "603", nil},
		{"movie", "https://www.themoviedb.org/movie/603-the-matrix", "603", nil},
		{"movie", "603", "603", nil},
		{"series", "1399", "1399", nil},
		{"series", "https://www.themoviedb.org/tv/1399-game-of-thrones?language=de", "1399", nil},
		{"series", "https://thetvdb.com/?tab=series&id=121361", "1399", nil},
		{"series", "https://thetvdb.com/dereferrer/series/121361", "1399", nil},
		{"series", "tvdb:81189", "1396", nil},
		{"series", "tt0944947", "1399", nil},
		{"series", "https://thetvdb.com/series/game-of-thrones", "", ErrTVDbOhneNummer},
		{"series", "https://www.themoviedb.org/movie/603", "", ErrFilmLinkBeiSerie},
		{"movie", "https://www.themoviedb.org/tv/1399", "", ErrSerienLinkBeiFilm},
		{"movie", "tt9999999", "", ErrKennungUnbekannt},
		{"movie", "tt0944947", "", ErrKennungUnbekannt}, // eine Serie, kein Film
		{"movie", "99999", "", ErrKennungUnbekannt},
	}
	for _, f := range faelle {
		c, err := r.Kandidaten(ctx, f.kind, f.ein, 0)
		if f.fehler != nil {
			if !errors.Is(err, f.fehler) {
				t.Errorf("%s %q: Fehler %v, erwartet %v", f.kind, f.ein, err, f.fehler)
			}
			continue
		}
		if err != nil || len(c) != 1 || c[0].SourceID != f.soll {
			t.Errorf("%s %q: %+v %v, erwartet %s", f.kind, f.ein, c, err, f.soll)
		}
	}
}
