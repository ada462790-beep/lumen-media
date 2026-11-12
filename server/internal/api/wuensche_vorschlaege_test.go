// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"lumen/internal/store"
)

// falschesTMDb spielt TMDb für die Wunsch-Vorschläge: drei Treffer zu
// „Dune" (ein Film ab 12, einer ab 16, eine Serie ohne Freigabe), eine
// Person, und kleine Poster. anfragen zählt die Suchen.
func falschesTMDb(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var anfragen atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/bilder/") {
			if !strings.HasSuffix(r.URL.Path, "/dune.jpg") {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write([]byte("\xff\xd8\xff\xe0 kleines poster"))
			return
		}
		if r.Header.Get("Authorization") != "Bearer eyJtest" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/authentication":
			_, _ = w.Write([]byte(`{"success":true}`))
		case "/search/multi":
			anfragen.Add(1)
			q := strings.ToLower(r.URL.Query().Get("query"))
			switch {
			case q == "dune":
				_, _ = w.Write([]byte(`{"results":[
				  {"media_type":"movie","id":841,"title":"Der Wüstenplanet","original_title":"Dune","release_date":"1984-12-14","poster_path":"/alt.jpg","popularity":20},
				  {"media_type":"person","id":5,"name":"Denis Villeneuve"},
				  {"media_type":"movie","id":438631,"title":"Dune","original_title":"Dune","release_date":"2021-09-15","poster_path":"/dune.jpg","popularity":90},
				  {"media_type":"tv","id":90228,"name":"Dune: Prophecy","original_name":"Dune: Prophecy","first_air_date":"2024-11-17","poster_path":"/prophecy.jpg","popularity":50}]}`))
			case q == "chihiro" && r.URL.Query().Get("language") == "en-US":
				_, _ = w.Write([]byte(`{"results":[{"media_type":"movie","id":129,"title":"Spirited Away","original_title":"千と千尋の神隠し","release_date":"2001-07-20"}]}`))
			default:
				_, _ = w.Write([]byte(`{"results":[]}`))
			}
		case "/movie/438631":
			_, _ = w.Write([]byte(`{"id":438631,"title":"Dune","original_title":"Dune","release_date":"2021-09-15",
			  "poster_path":"/dune.jpg","imdb_id":"tt1160419","releases":{"countries":[{"iso_3166_1":"CH","certification":"12"}]}}`))
		case "/movie/841":
			_, _ = w.Write([]byte(`{"id":841,"title":"Der Wüstenplanet","original_title":"Dune","release_date":"1984-12-14",
			  "imdb_id":"tt0087182","releases":{"countries":[{"iso_3166_1":"DE","certification":"16"}]}}`))
		case "/tv/90228":
			_, _ = w.Write([]byte(`{"id":90228,"name":"Dune: Prophecy","original_name":"Dune: Prophecy","first_air_date":"2024-11-17",
			  "external_ids":{"imdb_id":"tt10466872"},"content_ratings":{"results":[]}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &anfragen
}

// mitTMDb: Harness mit eingerichtetem (falschem) TMDb.
func mitTMDb(t *testing.T) (*harness, *atomic.Int32) {
	t.Helper()
	tmdb, anfragen := falschesTMDb(t)
	t.Setenv("LUMEN_TMDB_URL", tmdb.URL)
	t.Setenv("LUMEN_TMDB_IMG_URL", tmdb.URL+"/bilder")
	h := newHarness(t)
	if resp, raw := h.do("PUT", "/api/v1/admin/metadata", `{"tmdbKey":"eyJtest"}`, h.tok); resp.StatusCode != 200 {
		t.Fatalf("schlüssel: %d %s", resp.StatusCode, raw)
	}
	return h, anfragen
}

type vorschlagAntwort struct {
	OhneSchluessel bool `json:"ohneSchluessel"`
	Vorschlaege    []struct {
		Art             string `json:"art"`
		Titel           string `json:"titel"`
		Originaltitel   string `json:"originaltitel"`
		Jahr            int    `json:"jahr"`
		TMDbID          string `json:"tmdbId"`
		IMDbID          string `json:"imdbId"`
		Poster          string `json:"poster"`
		SchonDa         bool   `json:"schonDa"`
		ItemID          string `json:"itemId"`
		SchonGewuenscht bool   `json:"schonGewuenscht"`
	} `json:"vorschlaege"`
}

func (h *harness) vorschlaege(tok, q string) (int, vorschlagAntwort) {
	h.t.Helper()
	resp, raw := h.do("GET", "/api/v1/wishes/vorschlaege?q="+url.QueryEscape(q), "", tok)
	var v vorschlagAntwort
	if resp.StatusCode == 200 {
		if err := json.Unmarshal(raw, &v); err != nil {
			h.t.Fatalf("%v: %s", err, raw)
		}
	}
	return resp.StatusCode, v
}

type wunschZeile struct {
	ID     string   `json:"id"`
	By     string   `json:"by"`
	Title  string   `json:"title"`
	Year   int      `json:"year"`
	State  string   `json:"state"`
	Art    string   `json:"art"`
	TMDbID string   `json:"tmdbId"`
	IMDbID string   `json:"imdbId"`
	Poster string   `json:"poster"`
	ItemID string   `json:"itemId"`
	Mine   bool     `json:"mine"`
	Dabei  bool     `json:"dabei"`
	Auch   []string `json:"auch"`
	Note   string   `json:"note"`
}

func (h *harness) wuensche(tok string) []wunschZeile {
	h.t.Helper()
	_, raw := h.do("GET", "/api/v1/wishes", "", tok)
	var out []wunschZeile
	if err := json.Unmarshal(raw, &out); err != nil {
		h.t.Fatalf("%v: %s", err, raw)
	}
	return out
}

func (h *harness) wuenschen(tok, body string) (int, wunschZeile) {
	h.t.Helper()
	resp, raw := h.do("POST", "/api/v1/wishes", body, tok)
	var w wunschZeile
	if resp.StatusCode == 200 {
		if err := json.Unmarshal(raw, &w); err != nil {
			h.t.Fatalf("%v: %s", err, raw)
		}
	}
	return resp.StatusCode, w
}

// Ohne TMDb-Schlüssel gibt es keine Vorschläge und keinen Fehler — und
// wünschen geht mit dem Namen allein.
func TestWunschVorschlaegeOhneSchluessel(t *testing.T) {
	h := newHarness(t)
	code, v := h.vorschlaege(h.tok, "Dune")
	if code != 200 || !v.OhneSchluessel || len(v.Vorschlaege) != 0 {
		t.Fatalf("%d %+v", code, v)
	}
	if code, w := h.wuenschen(h.tok, `{"title":"Dune","year":0}`); code != 200 || w.Title != "Dune" {
		t.Fatalf("wünschen ohne Schlüssel: %d %+v", code, w)
	}
}

// Filme und Serien, keine Personen; das Jahr in der Suche zieht den
// passenden Treffer nach vorn; das Poster kommt über Lumen.
func TestWunschVorschlaege(t *testing.T) {
	h, _ := mitTMDb(t)
	code, v := h.vorschlaege(h.tok, "Dune 2021")
	if code != 200 || len(v.Vorschlaege) != 3 {
		t.Fatalf("%d %+v", code, v)
	}
	erster := v.Vorschlaege[0]
	if erster.TMDbID != "438631" || erster.Art != "movie" || erster.Jahr != 2021 ||
		erster.Poster != "/api/v1/wishes/poster/w92/dune.jpg" || erster.Originaltitel != "" {
		t.Errorf("erster Vorschlag: %+v", erster)
	}
	if v.Vorschlaege[1].Originaltitel != "Dune" || v.Vorschlaege[2].Art != "series" {
		t.Errorf("Vorschläge: %+v", v.Vorschlaege)
	}
	for _, x := range v.Vorschlaege {
		if x.SchonDa || x.SchonGewuenscht {
			t.Errorf("nichts ist da oder gewünscht: %+v", x)
		}
	}

	// Das Poster: über Lumen, auch mit dem Medienschlüssel in der Adresse.
	resp, raw := h.do("GET", erster.Poster, "", h.tok)
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "image/jpeg" || !strings.Contains(string(raw), "kleines poster") {
		t.Fatalf("poster: %d %q %s", resp.StatusCode, resp.Header.Get("Content-Type"), raw)
	}
	for _, falsch := range []string{"/api/v1/wishes/poster/w500/dune.jpg", "/api/v1/wishes/poster/w92/..%2Fetc.jpg",
		"/api/v1/wishes/poster/w92/nicht.gif"} {
		if resp, _ := h.do("GET", falsch, "", h.tok); resp.StatusCode != 404 {
			t.Errorf("%s: %d", falsch, resp.StatusCode)
		}
	}

	// Nichts auf Deutsch: dann auf Englisch.
	if _, v := h.vorschlaege(h.tok, "Chihiro"); len(v.Vorschlaege) != 1 || v.Vorschlaege[0].Titel != "Spirited Away" {
		t.Errorf("englischer Rückgriff: %+v", v)
	}
}

// Steht der Film schon in der Bibliothek (gleiche TMDb-Kennung), heisst
// der Vorschlag „schon da" — mit dem Titel zum Ansehen. Hat jemand die
// Serie gewünscht, heisst sie „schon gewünscht" — ohne zu sagen, wer.
func TestWunschVorschlaegeSchonDaUndGewuenscht(t *testing.T) {
	h, _ := mitTMDb(t)
	id, _ := h.film("Dune", 1)
	if err := h.st.SetSourceID(t.Context(), id, "tmdb", "438631"); err != nil {
		t.Fatal(err)
	}
	// Der alte Film ist über Titel und Jahr da (eine NFO ohne Kennung).
	alt := &store.Item{LibraryID: h.lib.ID, Kind: "movie", Title: "Der Wüstenplanet", Year: 1984, MatchState: "lokal"}
	if err := h.st.UpsertItem(t.Context(), alt); err != nil {
		t.Fatal(err)
	}
	lea := h.gast("Lea", false, h.lib.ID)
	if code, _ := h.wuenschen(lea, `{"title":"Dune: Prophecy","year":2024,"art":"series","tmdbId":"90228"}`); code != 200 {
		t.Fatal(code)
	}
	_, v := h.vorschlaege(h.tok, "Dune")
	treffer := map[string]int{}
	for i, x := range v.Vorschlaege {
		treffer[x.TMDbID] = i
	}
	if x := v.Vorschlaege[treffer["438631"]]; !x.SchonDa || x.ItemID != id || x.SchonGewuenscht {
		t.Errorf("Dune ist da: %+v", x)
	}
	if x := v.Vorschlaege[treffer["841"]]; !x.SchonDa || x.ItemID != alt.ID {
		t.Errorf("über Titel und Jahr da: %+v", x)
	}
	if x := v.Vorschlaege[treffer["90228"]]; x.SchonDa || !x.SchonGewuenscht {
		t.Errorf("Serie gewünscht: %+v", x)
	}
	_, raw := h.do("GET", "/api/v1/wishes/vorschlaege?q=Dune", "", h.tok)
	if strings.Contains(string(raw), "Lea") {
		t.Errorf("verrät, wer gewünscht hat: %s", raw)
	}

	// Ein Profil ohne Zugang zur Bibliothek: für es ist nichts „da".
	ohne := h.gast("Noa", false)
	if _, v := h.vorschlaege(ohne, "Dune"); len(v.Vorschlaege) == 0 || v.Vorschlaege[0].SchonDa {
		t.Errorf("ohne Freigabe: %+v", v)
	}
}

// Ein Kinderprofil (bis 12, Ungeprüftes nicht) sieht nur den Film ab 12.
// Den ab 16 kann es auch über die Kennung nicht wünschen.
func TestWunschVorschlaegeKinder(t *testing.T) {
	h, _ := mitTMDb(t)
	h.film("Etwas", 1)
	kind := h.gast("Mia", true, h.lib.ID)
	code, v := h.vorschlaege(kind, "Dune")
	if code != 200 || len(v.Vorschlaege) != 1 || v.Vorschlaege[0].TMDbID != "438631" {
		t.Fatalf("Kinderprofil: %d %+v", code, v)
	}
	if v.Vorschlaege[0].IMDbID != "tt1160419" {
		t.Errorf("IMDb aus den Einzelheiten: %+v", v.Vorschlaege[0])
	}
	if code, _ := h.wuenschen(kind, `{"title":"Der Wüstenplanet","year":1984,"art":"movie","tmdbId":"841"}`); code != http.StatusForbidden {
		t.Errorf("ab 16 gewünscht: %d", code)
	}
	if code, w := h.wuenschen(kind, `{"title":"Dune","year":2021,"art":"movie","tmdbId":"438631"}`); code != 200 || w.IMDbID != "tt1160419" {
		t.Errorf("ab 12: %d %+v", code, w)
	}
	// Erwachsene sehen alle drei.
	if _, v := h.vorschlaege(h.tok, "Dune"); len(v.Vorschlaege) != 3 {
		t.Errorf("Erwachsene: %+v", v)
	}
}

// Derselbe Titel über die Kennung: kein zweiter Wunsch. Ein anderes
// Profil schliesst sich an und sieht nicht, von wem der Wunsch ist; der
// Besitzer sieht beide. Der alte Aufbau {title, year} geht weiter.
func TestWunschMitKennungOhneDoppel(t *testing.T) {
	h, _ := mitTMDb(t)
	h.film("Etwas", 1)
	lea := h.gast("Lea", false, h.lib.ID)
	noa := h.gast("Noa", false, h.lib.ID)

	code, a := h.wuenschen(lea, `{"title":"Dune","year":2021,"art":"movie","tmdbId":"438631","imdbId":"tt1160419"}`)
	if code != 200 || a.TMDbID != "438631" || a.IMDbID != "tt1160419" || a.Art != "movie" ||
		a.Poster != "/api/v1/wishes/poster/w185/dune.jpg" {
		t.Fatalf("%d %+v", code, a)
	}
	// Ohne IMDb geschickt — Lumen holt sie selbst; anderer Titel getippt — gilt TMDb.
	_, b := h.wuenschen(lea, `{"title":"dune teil eins","art":"movie","tmdbId":"438631"}`)
	if b.ID != a.ID {
		t.Errorf("zweiter eigener Wunsch: %s / %s", a.ID, b.ID)
	}
	_, c := h.wuenschen(noa, `{"title":"Dune","year":2021,"art":"movie","tmdbId":"438631"}`)
	if c.ID != a.ID || !c.Dabei || c.Mine || c.By != "" {
		t.Errorf("angeschlossen: %+v", c)
	}
	if l := h.wuensche(noa); len(l) != 1 || l[0].By != "" || !l[0].Dabei {
		t.Errorf("Liste von Noa: %+v", l)
	}
	if l := h.wuensche(lea); len(l) != 1 || !l[0].Mine {
		t.Errorf("Liste von Lea: %+v", l)
	}
	_, raw := h.do("GET", "/api/v1/admin/wishes", "", h.tok)
	var adm struct{ Wishes []wunschZeile }
	if err := json.Unmarshal(raw, &adm); err != nil || len(adm.Wishes) != 1 || len(adm.Wishes[0].Auch) != 1 || adm.Wishes[0].Auch[0] != "Noa" {
		t.Errorf("Besitzer: %s", raw)
	}

	// Alt: nur Titel und Jahr — und ohne Art bleibt eine TMDb-Kennung weg.
	if code, w := h.wuenschen(lea, `{"title":"Nordlicht","year":2019}`); code != 200 || w.TMDbID != "" || w.Title != "Nordlicht" {
		t.Errorf("alter Aufbau: %d %+v", code, w)
	}
	if code, w := h.wuenschen(lea, `{"title":"Rätsel","tmdbId":"77"}`); code != 200 || w.TMDbID != "" {
		t.Errorf("Kennung ohne Art: %d %+v", code, w)
	}
	if code, _ := h.wuenschen(lea, `{"title":"","art":"movie","tmdbId":"abc"}`); code != 400 {
		t.Errorf("ohne Titel: %d", code)
	}

	// Das Poster des Wunsches liegt auf der Platte (wird im Hintergrund geholt).
	pfad := filepath.Join(h.daten, "wunschbilder", "w185-dune.jpg")
	for i := 0; i < 50; i++ {
		if _, err := os.Stat(pfad); err == nil {
			break
		}
		h.do("GET", a.Poster, "", lea)
	}
	if _, err := os.Stat(pfad); err != nil {
		t.Errorf("Poster nicht aufbewahrt: %v", err)
	}
}

// Kommt ein Titel mit derselben Kennung in die Bibliothek, erfüllt sich
// der Wunsch von selbst — für die Person, die ihn hatte, und alle, die
// sich angeschlossen haben, mit dem Titel zum Ansehen und als Neuigkeit.
func TestWunschErfuelltSichUeberKennung(t *testing.T) {
	h, _ := mitTMDb(t)
	h.film("Etwas", 1)
	lea := h.gast("Lea", false, h.lib.ID)
	noa := h.gast("Noa", false, h.lib.ID)
	_, a := h.wuenschen(lea, `{"title":"Dune","year":2021,"art":"movie","tmdbId":"438631"}`)
	h.wuenschen(noa, `{"title":"Dune","year":2021,"art":"movie","tmdbId":"438631"}`)
	// Die Serie gleichen Namens erfüllt den Film-Wunsch nicht.
	serie := &store.Item{LibraryID: h.lib.ID, Kind: "series", Title: "Irgendwas", MatchState: "matched"}
	if err := h.st.UpsertItem(t.Context(), serie); err != nil {
		t.Fatal(err)
	}
	_ = h.st.SetSourceID(t.Context(), serie.ID, "tmdb", "438631")
	if n, err := h.st.WuenscheErfuellen(t.Context()); err != nil || n != 0 {
		t.Fatalf("Serie erfüllt Film-Wunsch: %d %v", n, err)
	}

	// Der Film kommt — unter einem anderen Dateinamen.
	id, _ := h.film("Dune Part One", 1)
	_ = h.st.SetSourceID(t.Context(), id, "tmdb", "438631")
	if n, err := h.st.WuenscheErfuellen(t.Context()); err != nil || n != 1 {
		t.Fatalf("erfüllt: %d %v", n, err)
	}
	for _, tok := range []string{lea, noa} {
		l := h.wuensche(tok)
		if len(l) != 1 || l[0].ID != a.ID || l[0].State != "done" || l[0].ItemID != id {
			t.Errorf("Wunschliste: %+v", l)
		}
		_, raw := h.do("GET", "/api/v1/news", "", tok)
		if !strings.Contains(string(raw), `"art":"wunsch"`) || !strings.Contains(string(raw), id) {
			t.Errorf("Neuigkeit fehlt: %s", raw)
		}
	}
	// Wer den Titel nicht sehen darf, bekommt kein „Ansehen".
	ohne := h.gast("Kim", false)
	h.wuenschen(ohne, `{"title":"Dune: Prophecy","year":2024,"art":"series","tmdbId":"90228"}`)
	pro := &store.Item{LibraryID: h.lib.ID, Kind: "series", Title: "Dune: Prophecy", Year: 2024, MatchState: "matched"}
	_ = h.st.UpsertItem(t.Context(), pro)
	_ = h.st.SetSourceID(t.Context(), pro.ID, "tmdb", "90228")
	_, _ = h.st.WuenscheErfuellenFuer(t.Context(), h.api.wunschSicht)
	if l := h.wuensche(ohne); len(l) != 1 || l[0].State != "open" || l[0].ItemID != "" || l[0].Note != "" {
		t.Errorf("unsichtbarer Titel verlinkt oder als erfüllt verraten: %+v", l)
	}
}

// Ein Kind wünscht sich einen Film, der in einer Bibliothek liegt, die es
// nicht sieht: Der Wunsch bleibt offen — „Ist jetzt in der Bibliothek"
// verriete, dass es ihn gibt. Auch wer sich einem erfüllten Wunsch
// angeschlossen hat und den Titel nicht sieht, sieht ihn offen.
func TestWunschErfuelltNurWennSichtbar(t *testing.T) {
	h, _ := mitTMDb(t)
	h.film("Etwas", 1)
	ctx := t.Context()
	kinderLib, err := h.st.CreateLibrary(ctx, "Kinder", "movie", t.TempDir(), "de")
	if err != nil {
		t.Fatal(err)
	}
	kira := h.gast("Kira", true, kinderLib.ID)
	lea := h.gast("Lea", false, h.lib.ID, kinderLib.ID)
	// Nur nach Titel und Jahr.
	h.wuenschen(kira, `{"title":"Horror Nacht","year":2020}`)
	// Über die Kennung, Kira schliesst sich Lea an.
	h.wuenschen(lea, `{"title":"Dune","year":2021,"art":"movie","tmdbId":"438631"}`)
	h.wuenschen(kira, `{"title":"Dune","year":2021,"art":"movie","tmdbId":"438631"}`)

	horror := &store.Item{LibraryID: h.lib.ID, Kind: "movie", Title: "Horror Nacht", Year: 2020, MatchState: "matched"}
	dune := &store.Item{LibraryID: h.lib.ID, Kind: "movie", Title: "Dune", Year: 2021, MatchState: "matched"}
	for _, it := range []*store.Item{horror, dune} {
		if err := h.st.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
	}
	_ = h.st.SetSourceID(ctx, dune.ID, "tmdb", "438631")
	if n, err := h.st.WuenscheErfuellenFuer(ctx, h.api.wunschSicht); err != nil || n != 1 {
		t.Fatalf("erfüllt: %d %v (erwartet nur Leas Dune)", n, err)
	}
	for _, w := range h.wuensche(kira) {
		if w.State != "open" || w.Note != "" || w.ItemID != "" {
			t.Errorf("Kira sieht %q als %q („%s“, %q)", w.Title, w.State, w.Note, w.ItemID)
		}
	}
	if l := h.wuensche(lea); len(l) != 1 || l[0].State != "done" || l[0].ItemID != dune.ID {
		t.Errorf("Lea: %+v", l)
	}
}

// Wer sich angeschlossen hat, sieht nicht die Notiz der Verwaltung an die
// Person, die den Wunsch hatte — nur den allgemeinen Stand. Wird ihr Konto
// gelöscht, geht der Wunsch an die erste, die sich angeschlossen hat.
func TestWunschAngeschlossenNotizUndLoeschen(t *testing.T) {
	h, _ := mitTMDb(t)
	h.film("Etwas", 1)
	ctx := t.Context()
	lea := h.gast("Lea", false, h.lib.ID)
	noa := h.gast("Noa", false, h.lib.ID)
	mia := h.gast("Mia", false, h.lib.ID)
	_, a := h.wuenschen(lea, `{"title":"Dune","year":2021,"art":"movie","tmdbId":"438631"}`)
	h.wuenschen(noa, `{"title":"Dune","year":2021,"art":"movie","tmdbId":"438631"}`)
	h.wuenschen(mia, `{"title":"Dune","year":2021,"art":"movie","tmdbId":"438631"}`)
	_ = h.st.SetWishState(ctx, a.ID, "declined", "Lea, den haben wir schon auf DVD")
	if l := h.wuensche(noa); len(l) != 1 || l[0].Note != "" || l[0].State != "declined" {
		t.Errorf("Noa sieht die Notiz an Lea: %+v", l)
	}
	_ = h.st.SetWishState(ctx, a.ID, "done", store.WunschNotizErfuellt)
	if l := h.wuensche(noa); len(l) != 1 || l[0].Note != store.WunschNotizErfuellt {
		t.Errorf("allgemeiner Stand fehlt: %+v", l)
	}
	_ = h.st.SetWishState(ctx, a.ID, "open", "Lea, frag mich morgen")
	if err := h.st.DeleteAccount(ctx, h.idVon(lea)); err != nil {
		t.Fatal(err)
	}
	l := h.wuensche(noa)
	if len(l) != 1 || l[0].ID != a.ID || !l[0].Mine || l[0].Dabei || l[0].Note != "" {
		t.Fatalf("Wunsch nach dem Löschen: %+v", l)
	}
	if m := h.wuensche(mia); len(m) != 1 || m[0].ID != a.ID || !m[0].Dabei {
		t.Errorf("Mia: %+v", m)
	}
}

// Eine Mitverwalterin „nur Kinder" sieht unter „auch" nur Namen aus ihrem
// Bereich — und den Wunsch von jemandem ausserhalb, dem sich ein Kind aus
// ihrem Bereich angeschlossen hat, ohne den Namen der Person.
func TestWunschAuchNurImBereich(t *testing.T) {
	h, _ := mitTMDb(t)
	h.film("Etwas", 1)
	ctx := t.Context()
	kinderLib, err := h.st.CreateLibrary(ctx, "Kinder", "movie", t.TempDir(), "de")
	if err != nil {
		t.Fatal(err)
	}
	ctok, _ := h.mitverwalter("Carla")
	g, _ := h.st.GruppeAnlegen(ctx, store.Gruppe{Name: "Kinderverwaltung", Verwaltung: true,
		Libraries: []string{kinderLib.ID}, Limits: store.VerwaltungsLimits()})
	_ = h.st.GruppeZuweisen(ctx, h.idVon(ctok), g.ID)
	anna := h.gast("Anna", false, h.lib.ID, kinderLib.ID)
	kira := h.gast("Kira", true, kinderLib.ID)
	h.wuenschen(anna, `{"title":"Dune","year":2021,"art":"movie","tmdbId":"438631"}`)
	h.wuenschen(kira, `{"title":"Dune","year":2021,"art":"movie","tmdbId":"438631"}`)
	h.wuenschen(kira, `{"title":"Paddington","year":2014,"art":"movie","tmdbId":"116149"}`)
	h.wuenschen(anna, `{"title":"Paddington","year":2014,"art":"movie","tmdbId":"116149"}`)
	h.wuenschen(anna, `{"title":"Erwachsenenwunsch"}`)

	for _, weg := range []string{"/api/v1/admin/wishes", "/api/v1/wishes"} {
		_, raw := h.do("GET", weg, "", ctok)
		if strings.Contains(string(raw), "Anna") || strings.Contains(string(raw), "Erwachsenenwunsch") {
			t.Errorf("%s zeigt Anna: %s", weg, raw)
		}
		var liste []wunschZeile
		if weg == "/api/v1/admin/wishes" {
			var adm struct{ Wishes []wunschZeile }
			_ = json.Unmarshal(raw, &adm)
			liste = adm.Wishes
		} else {
			_ = json.Unmarshal(raw, &liste)
		}
		titel := map[string]wunschZeile{}
		for _, w := range liste {
			titel[w.Title] = w
		}
		if d, da := titel["Dune"]; !da || len(d.Auch) != 1 || d.Auch[0] != "Kira" || d.By == "" {
			t.Errorf("%s: Annas Wunsch, dem Kira sich anschloss: %+v", weg, d)
		}
		if p, da := titel["Paddington"]; !da || p.By != "Kira" || len(p.Auch) != 0 {
			t.Errorf("%s: Kiras Wunsch: %+v", weg, p)
		}
	}
	// Der Besitzer sieht alle Namen.
	_, raw := h.do("GET", "/api/v1/admin/wishes", "", h.tok)
	if !strings.Contains(string(raw), `"Anna"`) {
		t.Errorf("Besitzer: %s", raw)
	}
}

// Gleiche Suchen kommen aus dem Speicher; wer zu schnell neue schickt,
// wird gebremst.
func TestWunschVorschlaegeBremse(t *testing.T) {
	h, anfragen := mitTMDb(t)
	h.vorschlaege(h.tok, "Dune")
	h.vorschlaege(h.tok, "dune")
	if n := anfragen.Load(); n != 1 {
		t.Errorf("%d Suchen bei TMDb, erwartet 1", n)
	}
	gebremst := false
	for i := 0; i < wunschBremseAnzahl+2; i++ {
		if code, _ := h.vorschlaege(h.tok, fmt.Sprintf("Titel %d", i)); code == http.StatusTooManyRequests {
			gebremst = true
			break
		}
	}
	if !gebremst {
		t.Error("nie gebremst")
	}
	// Aus dem Speicher geht weiter.
	if code, v := h.vorschlaege(h.tok, "Dune"); code != 200 || len(v.Vorschlaege) != 3 {
		t.Errorf("aus dem Speicher: %d %+v", code, v)
	}
}
