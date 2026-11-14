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
	"strings"
	"testing"

	"lumen/internal/meta"
)

// Ohne Kennung: Wünscht jemand anders denselben Titel (gleiches Jahr,
// andere Schreibweise), schliesst er sich an. Mit Kennung danach bekommt
// der alte Wunsch die Kennung. Eine andere Kennung bleibt ein eigener.
func TestWunschDoppeltUeberTitel(t *testing.T) {
	h, _ := mitTMDb(t)
	h.film("Etwas", 1)
	lea := h.gast("Lea", false, h.lib.ID)
	noa := h.gast("Noa", false, h.lib.ID)
	_, a := h.wuenschen(lea, `{"title":"By Any Means","year":0}`)
	code, b := h.wuenschen(noa, `{"title":"by any means!","year":0}`)
	if code != 200 || b.ID != a.ID || !b.Dabei {
		t.Fatalf("zweiter Wunsch: %d %+v (erster %s)", code, b, a.ID)
	}
	resp, raw := h.do("POST", "/api/v1/wishes", `{"title":"By Any Means","year":0}`, noa)
	if resp.StatusCode != 200 || !strings.Contains(string(raw), `"vorhanden":true`) {
		t.Fatalf("vorhanden fehlt: %s", raw)
	}
	// Dune 2021 mit Kennung, dann Dune 2021 nur mit Namen: derselbe.
	_, d := h.wuenschen(lea, `{"title":"Dune","year":2021,"art":"movie","tmdbId":"438631"}`)
	if _, e := h.wuenschen(noa, `{"title":"Dune","year":2021}`); e.ID != d.ID {
		t.Fatalf("Name nicht zum Wunsch mit Kennung: %+v", e)
	}
	// Der Wüstenplanet (841) ist ein anderer Film, auch mit Originaltitel Dune.
	_, f := h.wuenschen(noa, `{"title":"Dune","year":1984,"art":"movie","tmdbId":"841"}`)
	if f.ID == d.ID {
		t.Fatal("andere Kennung zusammengelegt")
	}
	// Den deutschen Titel nur mit Namen: derselbe wie 841 (Titel von TMDb).
	if _, g := h.wuenschen(lea, `{"title":"Der Wüstenplanet","year":1984}`); g.ID != f.ID || !g.Dabei {
		t.Fatalf("deutscher Titel: %+v (841: %s)", g, f.ID)
	}
}

func TestWunschLoeschen(t *testing.T) {
	h := newHarness(t)
	h.film("Etwas", 1)
	lea := h.gast("Lea", false, h.lib.ID)
	noa := h.gast("Noa", false, h.lib.ID)
	mia := h.gast("Mia", false, h.lib.ID)
	_, a := h.wuenschen(lea, `{"title":"Heat","year":1995}`)
	h.wuenschen(noa, `{"title":"Heat","year":1995}`)
	if resp, _ := h.do("DELETE", "/api/v1/wishes/"+a.ID, "", mia); resp.StatusCode != 404 {
		t.Fatalf("Fremde löscht: %d", resp.StatusCode)
	}
	// Noa hat sich angeschlossen: tritt aus, der Wunsch bleibt.
	if resp, _ := h.do("DELETE", "/api/v1/wishes/"+a.ID, "", noa); resp.StatusCode != 204 {
		t.Fatalf("austreten: %d", resp.StatusCode)
	}
	if l := h.wuensche(noa); len(l) != 0 {
		t.Fatalf("Noa sieht ihn noch: %+v", l)
	}
	if l := h.wuensche(lea); len(l) != 1 {
		t.Fatalf("Wunsch weg: %+v", l)
	}
	// In Arbeit: nicht mehr durch Lea, aber durch die Verwaltung.
	_ = h.st.SetWishState(t.Context(), a.ID, "working", "")
	if resp, _ := h.do("DELETE", "/api/v1/wishes/"+a.ID, "", lea); resp.StatusCode != 409 {
		t.Fatalf("in Arbeit gelöscht: %d", resp.StatusCode)
	}
	if resp, _ := h.do("DELETE", "/api/v1/admin/wishes/"+a.ID, "", lea); resp.StatusCode == 204 {
		t.Fatal("Gast nutzt den Weg der Verwaltung")
	}
	if resp, _ := h.do("DELETE", "/api/v1/admin/wishes/"+a.ID, "", h.tok); resp.StatusCode != 204 {
		t.Fatalf("Verwaltung löscht: %d", resp.StatusCode)
	}
	if l := h.wuensche(lea); len(l) != 0 {
		t.Fatalf("noch da: %+v", l)
	}
	_, b := h.wuenschen(lea, `{"title":"Ronin","year":1998}`)
	if resp, _ := h.do("DELETE", "/api/v1/wishes/"+b.ID, "", lea); resp.StatusCode != 204 {
		t.Fatalf("eigenen offenen löschen: %d", resp.StatusCode)
	}
}

// Dubletten von früher: Die Verwaltung sieht sie als Gruppe und führt sie
// zusammen; wer sie hatte, ist danach beim ältesten dabei.
func TestWunschDublettenZusammenfuehren(t *testing.T) {
	h := newHarness(t)
	h.film("Etwas", 1)
	ctx := t.Context()
	lea := h.gast("Lea", false, h.lib.ID)
	noa := h.gast("Noa", false, h.lib.ID)
	// Am Anlegen vorbei, wie es vor der Erkennung entstanden ist.
	a, _ := h.st.CreateWish(ctx, h.idVon(lea), "By Any Means", 0)
	b, _ := h.st.CreateWish(ctx, h.idVon(noa), "By any means", 0)
	c, _ := h.st.CreateWish(ctx, h.idVon(noa), "Anderes", 0)
	_, raw := h.do("GET", "/api/v1/admin/wishes", "", h.tok)
	var out struct {
		Wishes []struct {
			ID     string `json:"id"`
			Gruppe string `json:"gruppe"`
		} `json:"wishes"`
	}
	_ = json.Unmarshal(raw, &out)
	gruppe := map[string]string{}
	for _, w := range out.Wishes {
		gruppe[w.ID] = w.Gruppe
	}
	if gruppe[a.ID] != a.ID || gruppe[b.ID] != a.ID || gruppe[c.ID] != "" {
		t.Fatalf("Gruppen: %v", gruppe)
	}
	if resp, raw := h.do("POST", "/api/v1/admin/wishes/"+a.ID+"/zusammenfuehren", `{"ids":["`+b.ID+`","`+c.ID+`"]}`, h.tok); resp.StatusCode != 200 || !strings.Contains(string(raw), `"zusammengefuehrt":1`) {
		t.Fatalf("zusammenführen: %d %s", resp.StatusCode, raw)
	}
	l := h.wuensche(noa)
	if len(l) != 2 {
		t.Fatalf("Noa: %+v", l)
	}
	for _, w := range l {
		if w.Title == "By Any Means" && (!w.Dabei || w.ID != a.ID) {
			t.Fatalf("Noa nicht dabei: %+v", w)
		}
	}
}

func TestWunschEindeutig(t *testing.T) {
	treffer := []meta.Vorschlag{
		{Art: "movie", TMDbID: "1", Titel: "By Any Means", Jahr: 2017},
		{Art: "series", TMDbID: "2", Titel: "By Any Means", Jahr: 2013},
		{Art: "movie", TMDbID: "3", Titel: "By Any Means Necessary", Jahr: 2020},
	}
	if wunschEindeutig("By Any Means", 0, treffer) != nil {
		t.Fatal("zwei Treffer ohne Jahr gelten als eindeutig")
	}
	if v := wunschEindeutig("by any means", 2013, treffer); v == nil || v.TMDbID != "2" {
		t.Fatalf("mit Jahr: %+v", v)
	}
	if wunschEindeutig("Nichts", 0, treffer) != nil {
		t.Fatal("kein Treffer")
	}
}

// Alte Wünsche ohne Kennung bekommen Kennung und Poster, wenn TMDb genau
// einen Treffer kennt.
func TestWunschKennungNachtragen(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer eyJtest" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/authentication":
			_, _ = w.Write([]byte(`{"success":true}`))
		case "/search/multi":
			switch strings.ToLower(r.URL.Query().Get("query")) {
			case "by any means":
				_, _ = w.Write([]byte(`{"results":[{"media_type":"movie","id":77,"title":"By Any Means","original_title":"By Any Means","release_date":"2017-05-01","poster_path":"/byanymeans.jpg"}]}`))
			case "zwilling":
				_, _ = w.Write([]byte(`{"results":[{"media_type":"movie","id":5,"title":"Zwilling","release_date":"2001-01-01"},{"media_type":"tv","id":6,"name":"Zwilling","first_air_date":"2010-01-01"}]}`))
			default:
				_, _ = w.Write([]byte(`{"results":[]}`))
			}
		case "/movie/77":
			_, _ = w.Write([]byte(`{"id":77,"title":"By Any Means","release_date":"2017-05-01","poster_path":"/byanymeans.jpg","imdb_id":"tt0000077"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	t.Setenv("LUMEN_TMDB_URL", srv.URL)
	t.Setenv("LUMEN_TMDB_IMG_URL", srv.URL+"/bilder")
	h := newHarness(t)
	if resp, raw := h.do("PUT", "/api/v1/admin/metadata", `{"tmdbKey":"eyJtest"}`, h.tok); resp.StatusCode != 200 {
		t.Fatalf("schlüssel: %d %s", resp.StatusCode, raw)
	}
	h.film("Etwas", 1)
	lea := h.gast("Lea", false, h.lib.ID)
	ctx := t.Context()
	a, _ := h.st.CreateWish(ctx, h.idVon(lea), "By Any Means", 0)
	b, _ := h.st.CreateWish(ctx, h.idVon(lea), "Zwilling", 0)
	n, err := h.api.wuenscheNachtragen(ctx)
	if err != nil || n != 1 {
		t.Fatalf("nachgetragen: %d %v", n, err)
	}
	for _, w := range h.wuensche(lea) {
		switch w.ID {
		case a.ID:
			if w.TMDbID != "77" || w.IMDbID != "tt0000077" || w.Art != "movie" || w.Year != 2017 || w.Poster == "" {
				t.Fatalf("By Any Means: %+v", w)
			}
		case b.ID:
			if w.TMDbID != "" {
				t.Fatalf("mehrdeutig trotzdem gesetzt: %+v", w)
			}
		}
	}
}
