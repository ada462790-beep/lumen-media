// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lumen/internal/meta"
	"lumen/internal/store"
	"lumen/internal/uebernahme/nachbau"
)

// zuordnungsTMDb stellt TMDb nach: zwei Filme und eine Serie, auffindbar
// über ihre IMDb- und TVDb-Kennungen.
func zuordnungsTMDb(t *testing.T, h *harness) *meta.Resolver {
	t.Helper()
	tmdb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		q := r.URL.Query()
		switch r.URL.Path {
		case "/movie/603":
			_, _ = w.Write([]byte(`{"id":603,"title":"Matrix","original_title":"The Matrix","release_date":"1999-03-31","overview":"Von TMDb.","imdb_id":"tt0133093"}`))
		case "/movie/120":
			_, _ = w.Write([]byte(`{"id":120,"title":"Der Herr der Ringe: Die Gefährten","release_date":"2001-12-19","imdb_id":"tt0120737"}`))
		case "/tv/1396":
			_, _ = w.Write([]byte(`{"id":1396,"name":"Breaking Bad","first_air_date":"2008-01-20","external_ids":{"imdb_id":"tt0903747","tvdb_id":81189}}`))
		case "/find/tt0120737":
			_, _ = w.Write([]byte(`{"movie_results":[{"id":120}],"tv_results":[]}`))
		case "/find/81189":
			if q.Get("external_source") == "tvdb_id" {
				_, _ = w.Write([]byte(`{"movie_results":[],"tv_results":[{"id":1396}]}`))
				return
			}
			_, _ = w.Write([]byte(`{}`))
		case "/search/movie", "/search/tv":
			_, _ = w.Write([]byte(`{"results":[]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	t.Cleanup(tmdb.Close)
	res, err := meta.NewResolver(h.st, h.daten, meta.Config{TMDbKey: "x", TMDbURL: tmdb.URL, TMDbImgURL: tmdb.URL})
	if err != nil {
		t.Fatal(err)
	}
	h.svr().SetResolver(res)
	return res
}

// Die Zuordnungen aus Emby: über das Ende des Pfads gefunden, wie von Hand
// zugewiesen, von Hand Zugewiesenes bleibt — ausser mit „überschreiben".
func TestUmzugUebernimmtZuordnungen(t *testing.T) {
	h := newHarness(t)
	zuordnungsTMDb(t, h)
	ctx := t.Context()

	// Lumen: zwei Filme, nicht erkannt bzw. falsch, ein von Hand
	// zugewiesener, eine Serie.
	matrix, _ := h.film("Matrix (1999)", 1)
	ringe, _ := h.film("Herr der Ringe", 1)
	hand, _ := h.film("Von Hand", 1)
	for id, z := range map[string]string{matrix: "unmatched", ringe: "matched", hand: "manual"} {
		it, _ := h.st.Item(ctx, id)
		it.MatchState = z
		_ = h.st.UpsertItem(ctx, it)
	}
	_ = h.st.SetSourceID(ctx, ringe, "tmdb", "999") // falsch zugeordnet
	_ = h.st.SetSourceID(ctx, hand, "tmdb", "777")
	serien, _ := h.st.CreateLibrary(ctx, "Serien", "series", filepath.Join(h.root, "Serien"), "de")
	serie := &store.Item{LibraryID: serien.ID, Kind: "series", Title: "BB", MatchState: "unmatched"}
	_ = h.st.UpsertItem(ctx, serie)
	staffel := &store.Item{LibraryID: serien.ID, Kind: "season", ParentID: serie.ID, SeasonNo: 1, MatchState: "matched"}
	_ = h.st.UpsertItem(ctx, staffel)
	folge := &store.Item{LibraryID: serien.ID, Kind: "episode", ParentID: staffel.ID, SeasonNo: 1, EpisodeNo: 1, MatchState: "pending"}
	_ = h.st.UpsertItem(ctx, folge)
	_ = h.st.UpsertFile(ctx, &store.MediaFile{ItemID: folge.ID, SizeBytes: 5,
		Path: filepath.Join(h.root, "Serien", "Breaking Bad (2008)", "Season 01", "BB S01E01.mkv")})

	// Emby sieht dieselben Dateien unter /media.
	n := nachbau.Neu("Emby")
	defer n.Close()
	bb := nachbau.Serie("s1", "Breaking Bad", 2008, map[string]string{"Tvdb": "81189"}, nil)
	bb["Path"] = "/media/serien/Breaking Bad (2008)"
	n.Alle = []nachbau.Titel{
		nachbau.Film("e1", "The Matrix", 1999, "/media/filme/Matrix (1999).mkv", 1024*1024,
			map[string]string{"Tmdb": "603", "Imdb": "tt0133093"}, map[string]any{}),
		nachbau.Film("e2", "LOTR", 2001, "/media/filme/Herr der Ringe.mkv", 0,
			map[string]string{"Imdb": "tt0120737"}, map[string]any{}),
		nachbau.Film("e3", "Hand", 2001, "/media/filme/Von Hand.mkv", 0,
			map[string]string{"Tmdb": "603"}, map[string]any{}),
		nachbau.Film("e4", "Fehlt", 2010, "/media/filme/Fehlt.mkv", 0,
			map[string]string{"Tmdb": "5"}, map[string]any{}),
		bb,
	}

	// Ein Profil ohne Verwaltungsrechte darf das nicht.
	gtok := h.gast("Mara", false, h.lib.ID)
	if resp, raw := h.do("POST", "/api/v1/me/uebernahme", `{"adresse":"`+n.Adresse()+`","benutzer":"alex","passwort":"geheim","zuordnungen":true}`, gtok); resp.StatusCode != 403 {
		t.Fatalf("Gast: %d %s", resp.StatusCode, raw)
	}

	resp, raw := h.do("POST", "/api/v1/me/uebernahme", `{"adresse":"`+n.Adresse()+`","benutzer":"alex","passwort":"geheim","zuordnungen":true}`, h.tok)
	if resp.StatusCode != 202 {
		t.Fatalf("starten: %d %s", resp.StatusCode, raw)
	}
	st := h.warteAufUebernahme(h.tok)
	if st["phase"] != "bereit" {
		t.Fatalf("Stand: %v", st)
	}
	z, _ := st["zuordnungen"].(map[string]any)
	if z == nil || z["neu"].(float64) != 3 || z["gesperrt"].(float64) != 1 || z["nichtGefunden"].(float64) != 1 {
		t.Fatalf("Vorschau: %v", st["zuordnungen"])
	}

	resp, raw = h.do("POST", "/api/v1/me/uebernahme/anwenden", `{"gesehen":false,"angefangen":false,"favoriten":false,"bewertungen":false,"zuordnungen":true}`, h.tok)
	if resp.StatusCode != 200 {
		t.Fatalf("anwenden: %d %s", resp.StatusCode, raw)
	}
	stand := warteAufZuordnungen(t, h)
	if stand["uebernommen"].(float64) != 3 || stand["gesperrt"].(float64) != 1 || stand["fehler"].(float64) != 0 {
		t.Fatalf("Ergebnis: %v", stand)
	}
	pruef := func(id, soll string) {
		t.Helper()
		it, _ := h.st.Item(ctx, id)
		src, _ := h.st.SourceID(ctx, id, "tmdb")
		if src != soll || it.MatchState != "manual" {
			t.Errorf("%s: tmdb=%q Zustand %q, erwartet %q/manual", it.Title, src, it.MatchState, soll)
		}
	}
	pruef(matrix, "603")
	pruef(ringe, "120")
	pruef(serie.ID, "1396")
	if src, _ := h.st.SourceID(ctx, hand, "tmdb"); src != "777" {
		t.Errorf("von Hand Zugewiesenes überschrieben: %q", src)
	}
	// Die Folge wartet auf ihre Metadaten (sie kommen im Hintergrund).
	if f, _ := h.st.Item(ctx, folge.ID); f.MatchState == "unmatched" {
		t.Errorf("Folge: %q", f.MatchState)
	}
	// Im Protokoll: Anzahl, kein Titel.
	_, logRaw := h.do("GET", "/api/v1/admin/log", "", h.tok)
	if strings.Contains(string(logRaw), "Matrix") || strings.Contains(string(logRaw), "Breaking") {
		t.Errorf("Titel im Protokoll: %s", logRaw)
	}

	// Noch einmal, mit „überschreiben": jetzt auch der von Hand
	// zugewiesene; die anderen sind schon richtig.
	h.do("POST", "/api/v1/me/uebernahme", `{"adresse":"`+n.Adresse()+`","benutzer":"alex","passwort":"geheim","zuordnungen":true}`, h.tok)
	st = h.warteAufUebernahme(h.tok)
	if z := st["zuordnungen"].(map[string]any); z["schonRichtig"].(float64) != 3 {
		t.Fatalf("zweite Vorschau: %v", z)
	}
	h.do("POST", "/api/v1/me/uebernahme/anwenden", `{"zuordnungen":true,"ueberschreiben":true}`, h.tok)
	stand = warteAufZuordnungen(t, h)
	if stand["uebernommen"].(float64) != 1 || stand["schonRichtig"].(float64) != 3 {
		t.Fatalf("überschreiben: %v", stand)
	}
	pruef(hand, "603")
}

func warteAufZuordnungen(t *testing.T, h *harness) map[string]any {
	t.Helper()
	for i := 0; i < 500; i++ {
		_, raw := h.do("GET", "/api/v1/me/uebernahme", "", h.tok)
		var st struct {
			ZuordnungStand map[string]any `json:"zuordnungStand"`
		}
		_ = json.Unmarshal(raw, &st)
		if st.ZuordnungStand != nil && st.ZuordnungStand["laeuft"] == false {
			return st.ZuordnungStand
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("Zuordnungen werden nicht fertig")
	return nil
}

// Die Liste in „Aufräumen": nicht erkannte und wartende Titel mit Pfad,
// dazu, was der letzte Durchlauf ausgelassen hat.
func TestAufraeumenNichtErkannt(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()
	nicht, _ := h.film("Ein unbekannter Film", 1)
	wartet, _ := h.film("Wartet noch", 1)
	for id, z := range map[string]string{nicht: "unmatched", wartet: "pending"} {
		it, _ := h.st.Item(ctx, id)
		it.MatchState = z
		_ = h.st.UpsertItem(ctx, it)
	}
	_, raw := h.do("GET", "/api/v1/admin/cleanup", "", h.tok)
	var d struct {
		Unmatched  []map[string]any `json:"unmatched"`
		Ausstehend []map[string]any `json:"ausstehend"`
		Offen      map[string]int   `json:"offen"`
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatal(err)
	}
	if len(d.Unmatched) != 1 || d.Unmatched[0]["title"] != "Ein unbekannter Film" || !strings.HasSuffix(d.Unmatched[0]["pfad"].(string), "Ein unbekannter Film.mkv") {
		t.Errorf("nicht erkannt: %s", raw)
	}
	if len(d.Ausstehend) != 1 || d.Offen["unmatched"] != 1 || d.Offen["pending"] != 1 {
		t.Errorf("ausstehend/offen: %s", raw)
	}
	// Ein Gast sieht das nicht.
	gtok := h.gast("Mara", false, h.lib.ID)
	if resp, _ := h.do("GET", "/api/v1/admin/cleanup", "", gtok); resp.StatusCode != 403 && resp.StatusCode != 404 {
		t.Errorf("Gast: %d", resp.StatusCode)
	}
}

// Zuweisen über eine Kennung: klare Meldungen, wenn es so nicht geht.
func TestZuweisenKennungMeldungen(t *testing.T) {
	h := newHarness(t)
	zuordnungsTMDb(t, h)
	id, _ := h.film("Irgendwas", 1)
	such := func(q string) (int, string) {
		resp, raw := h.do("GET", "/api/v1/admin/items/"+id+"/metadata/search?q="+q, "", h.tok)
		return resp.StatusCode, string(raw)
	}
	if code, raw := such("tt0120737"); code != 200 || !strings.Contains(raw, `"sourceId":"120"`) {
		t.Errorf("IMDb: %d %s", code, raw)
	}
	if code, raw := such("https%3A%2F%2Fthetvdb.com%2Fseries%2Fgame-of-thrones"); code != 400 || !strings.Contains(raw, "tvdb_ohne_nummer") {
		t.Errorf("TVDb ohne Nummer: %d %s", code, raw)
	}
	if code, raw := such("https%3A%2F%2Fwww.themoviedb.org%2Ftv%2F1396"); code != 400 || !strings.Contains(raw, "falsche_art") {
		t.Errorf("Serien-Link bei Film: %d %s", code, raw)
	}
	if code, raw := such("tt0000001"); code != 404 || !strings.Contains(raw, "kennung_unbekannt") {
		t.Errorf("unbekannt: %d %s", code, raw)
	}
	// Zuweisen selbst nimmt nur eine TMDb-Kennung (Zahl).
	if resp, raw := h.do("POST", "/api/v1/admin/items/"+id+"/metadata/assign", `{"sourceId":"../account"}`, h.tok); resp.StatusCode != 400 {
		t.Errorf("unsinnige Kennung: %d %s", resp.StatusCode, raw)
	}
	resp, raw := h.do("POST", "/api/v1/admin/items/"+id+"/metadata/assign", `{"sourceId":"603"}`, h.tok)
	if resp.StatusCode != 200 || !strings.Contains(string(raw), `"matchState":"manual"`) {
		t.Errorf("zuweisen: %d %s", resp.StatusCode, raw)
	}
}

// Ohne TMDb-Schlüssel: Die Kennungen aus Emby werden vorgemerkt statt
// „nicht möglich"; was die NFO schon mit derselben Kennung hat, ist
// „schon richtig".
func TestUmzugZuordnungenOhneSchluessel(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()
	matrix, _ := h.film("Matrix (1999)", 1)
	alien, _ := h.film("Alien", 1)
	for id, z := range map[string]string{matrix: "pending", alien: "lokal"} {
		it, _ := h.st.Item(ctx, id)
		it.MatchState = z
		_ = h.st.UpsertItem(ctx, it)
	}
	_ = h.st.SetSourceID(ctx, alien, "tmdb", "348") // aus der NFO
	n := nachbau.Neu("Jellyfin")
	defer n.Close()
	n.Alle = []nachbau.Titel{
		nachbau.Film("e1", "The Matrix", 1999, "/media/filme/Matrix (1999).mkv", 1024*1024,
			map[string]string{"Tmdb": "603", "Imdb": "tt0133093"}, map[string]any{}),
		nachbau.Film("e2", "Alien", 1979, "/media/filme/Alien.mkv", 1024*1024,
			map[string]string{"Tmdb": "348"}, map[string]any{}),
	}
	resp, raw := h.do("POST", "/api/v1/me/uebernahme", `{"adresse":"`+n.Adresse()+`","benutzer":"alex","passwort":"geheim","zuordnungen":true}`, h.tok)
	if resp.StatusCode != 202 {
		t.Fatalf("starten: %d %s", resp.StatusCode, raw)
	}
	st := h.warteAufUebernahme(h.tok)
	z, _ := st["zuordnungen"].(map[string]any)
	if z == nil || z["neu"].(float64) != 1 || z["schonRichtig"].(float64) != 1 || z["ohneQuelle"] != true || z["schluessel"] != "fehlt" {
		t.Fatalf("Vorschau: %v", st["zuordnungen"])
	}
	resp, raw = h.do("POST", "/api/v1/me/uebernahme/anwenden", `{"gesehen":false,"angefangen":false,"favoriten":false,"bewertungen":false,"zuordnungen":true}`, h.tok)
	if resp.StatusCode != 200 {
		t.Fatalf("anwenden: %d %s", resp.StatusCode, raw)
	}
	stand := warteAufZuordnungen(t, h)
	if stand["vorgemerkt"].(float64) != 1 || stand["schonRichtig"].(float64) != 1 || stand["fehler"].(float64) != 0 || stand["grund"] != "fehlt" {
		t.Fatalf("Ergebnis: %v", stand)
	}
	it, _ := h.st.Item(ctx, matrix)
	src, _ := h.st.SourceID(ctx, matrix, "tmdb")
	if src != "603" || it.IMDbID != "tt0133093" || it.MatchState != "lokal" {
		t.Errorf("Matrix: tmdb %q imdb %q Zustand %q", src, it.IMDbID, it.MatchState)
	}
}
