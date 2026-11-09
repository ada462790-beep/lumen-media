// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lumen/internal/meta"
	"lumen/internal/store"
	"lumen/internal/uebernahme/nachbau"
	osnachbau "lumen/internal/untertitel/nachbau"
)

// ------------------------------------------------------- Handbearbeitung

func pngBild(t *testing.T, b, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, b, h))
	for y := 0; y < h; y++ {
		for x := 0; x < b; x++ {
			img.Set(x, y, color.NRGBA{uint8(x), uint8(y), 200, 128})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func (h *harness) hochladen(pfad, tok string, inhalt []byte) (*http.Response, []byte) {
	h.t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("bild", "poster.png")
	_, _ = fw.Write(inhalt)
	_ = mw.Close()
	req, _ := http.NewRequest("POST", h.srv.URL+pfad, &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp, raw
}

func TestMetadatenVonHand(t *testing.T) {
	h := newHarness(t)
	id, _ := h.film("Konzert 2019", 1)
	it, _ := h.st.Item(t.Context(), id)
	it.MatchState = "unmatched"
	_ = h.st.UpsertItem(t.Context(), it)

	resp, raw := h.do("GET", "/api/v1/admin/items/"+id+"/metadata", "", h.tok)
	if resp.StatusCode != 200 || !strings.Contains(string(raw), `"locks":[]`) {
		t.Fatalf("lesen: %d %s", resp.StatusCode, raw)
	}

	resp, raw = h.do("PATCH", "/api/v1/admin/items/"+id+"/metadata",
		`{"title":"Konzert im Park","overview":"Sommerabend.\nMit Zugabe.","year":2019,
		  "genres":["Musik","Musik"," Live "],"ageRating":"FSK 6","premiered":"2019-07-14",
		  "cast":[{"name":"Die Band","rolle":"sich selbst"}]}`, h.tok)
	if resp.StatusCode != 200 {
		t.Fatalf("speichern: %d %s", resp.StatusCode, raw)
	}
	var a bearbeitenAntwort
	_ = json.Unmarshal(raw, &a)
	if a.Item.Title != "Konzert im Park" || len(a.Item.Genres) != 2 || a.Item.AgeYears == nil || *a.Item.AgeYears != 6 {
		t.Fatalf("Antwort: %+v", a.Item)
	}
	for _, f := range []string{"title", "overview", "year", "genres", "ageRating", "premiered", "cast"} {
		if !enthaeltText(a.Locks, f) {
			t.Errorf("Feld %s nicht gesperrt: %v", f, a.Locks)
		}
	}
	if enthaeltText(a.Locks, "originalTitle") {
		t.Error("ungeändertes Feld gesperrt")
	}
	// Nicht mehr „nicht erkannt".
	_, raw = h.do("GET", "/api/v1/admin/metadata/unmatched", "", h.tok)
	if strings.Contains(string(raw), id) {
		t.Fatalf("bearbeiteter Titel steht noch unter „nicht erkannt“: %s", raw)
	}

	// Fehler sind Sätze.
	for _, f := range []struct{ body, text string }{
		{`{"title":"   "}`, "nicht leer"},
		{`{"ageRating":"irgendwas"}`, "Altersfreigabe"},
		{`{"premiered":"14.07.2019"}`, "JJJJ-MM-TT"},
		{`{"year":12}`, "Jahr"},
		{`{"episodeNo":3}`, "gibt es hier nicht"},
	} {
		resp, raw = h.do("PATCH", "/api/v1/admin/items/"+id+"/metadata", f.body, h.tok)
		if resp.StatusCode != 400 || !strings.Contains(string(raw), f.text) {
			t.Errorf("%s: %d %s", f.body, resp.StatusCode, raw)
		}
	}

	// Poster hochladen: wird neu kodiert und verkleinert.
	resp, raw = h.hochladen("/api/v1/admin/items/"+id+"/artwork/poster", h.tok, pngBild(t, 2400, 3600))
	if resp.StatusCode != 200 || !strings.Contains(string(raw), `"poster"`) {
		t.Fatalf("Poster: %d %s", resp.StatusCode, raw)
	}
	art, _ := h.st.ArtworkFor(t.Context(), []string{id})
	name := art[id]["poster"]
	if !strings.Contains(name, "-hand-") {
		t.Fatalf("Postername: %q", name)
	}
	f, err := os.Open(filepath.Join(h.svr().meta.ArtDir(), name))
	if err != nil {
		t.Fatal(err)
	}
	kopf, format, err := image.DecodeConfig(f)
	f.Close()
	if err != nil || format != "jpeg" || kopf.Width != 2000 || kopf.Height != 3000 {
		t.Fatalf("neu kodiert: %s %dx%d %v", format, kopf.Width, kopf.Height, err)
	}
	// Ein zweites ersetzt das erste — die alte Datei verschwindet.
	h.hochladen("/api/v1/admin/items/"+id+"/artwork/poster", h.tok, pngBild(t, 100, 150))
	if _, err := os.Stat(filepath.Join(h.svr().meta.ArtDir(), name)); !os.IsNotExist(err) {
		t.Fatal("altes eigenes Poster liegt noch da")
	}
	// Kein Bild, sondern Text.
	resp, raw = h.hochladen("/api/v1/admin/items/"+id+"/artwork/poster", h.tok, []byte("<?php echo 1; ?>"))
	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("Text als Bild: %d %s", resp.StatusCode, raw)
	}

	// Bild von einer Adresse — die Adresse prüft netz.Client; im Test
	// steht ein eigener Client, der den lokalen Nachbau erreicht.
	bildSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(pngBild(t, 1920, 1080))
	}))
	defer bildSrv.Close()
	vorher := bildClient
	bildClient = bildSrv.Client()
	defer func() { bildClient = vorher }()
	resp, raw = h.do("POST", "/api/v1/admin/items/"+id+"/artwork/backdrop", `{"url":"`+bildSrv.URL+`/b.png"}`, h.tok)
	if resp.StatusCode != 200 || !strings.Contains(string(raw), `"backdrop"`) {
		t.Fatalf("Hintergrund per Adresse: %d %s", resp.StatusCode, raw)
	}
	resp, _ = h.do("POST", "/api/v1/admin/items/"+id+"/artwork/backdrop", `{"url":"file:///etc/passwd"}`, h.tok)
	if resp.StatusCode != 400 {
		t.Fatalf("file:// angenommen: %d", resp.StatusCode)
	}

	// Sperre einzeln aufheben.
	resp, raw = h.do("DELETE", "/api/v1/admin/items/"+id+"/lock/overview", "", h.tok)
	if resp.StatusCode != 200 || strings.Contains(string(raw), `"overview","`) && strings.Contains(string(raw), `"locks":["overview"`) {
		t.Fatalf("Sperre aufheben: %d %s", resp.StatusCode, raw)
	}
	sp, _ := h.st.Sperren(t.Context(), id)
	if sp["overview"] || !sp["title"] {
		t.Fatalf("Sperren danach: %v", sp)
	}

	// Ein Gast darf nichts davon.
	gtok := h.gast("Mara", false, h.lib.ID)
	for _, w := range [][2]string{
		{"GET", "/api/v1/admin/items/" + id + "/metadata"},
		{"PATCH", "/api/v1/admin/items/" + id + "/metadata"},
		{"DELETE", "/api/v1/admin/items/" + id + "/lock/title"},
	} {
		if resp, _ := h.do(w[0], w[1], `{"title":"Hack"}`, gtok); resp.StatusCode != 404 {
			t.Errorf("Gast %s %s: %d", w[0], w[1], resp.StatusCode)
		}
	}
	if resp, _ := h.hochladen("/api/v1/admin/items/"+id+"/artwork/poster", gtok, pngBild(t, 64, 64)); resp.StatusCode != 404 {
		t.Errorf("Gast lädt Poster hoch: %d", resp.StatusCode)
	}
}

// „Metadaten neu laden" über TMDb lässt gesperrte Felder stehen.
func TestNeuLadenAchtetSperren(t *testing.T) {
	h := newHarness(t)
	id, _ := h.film("Matrix", 1)
	if it, err := h.st.Item(t.Context(), id); err == nil {
		it.Year = 1999
		_ = h.st.UpsertItem(t.Context(), it)
	}
	tmdb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "/search/movie"):
			_, _ = w.Write([]byte(`{"results":[{"id":603,"title":"Matrix","original_title":"The Matrix","release_date":"1999-03-31","overview":"Von TMDb.","popularity":80,"vote_count":20000}]}`))
		case strings.Contains(r.URL.Path, "/movie/603"):
			_, _ = w.Write([]byte(`{"id":603,"title":"Matrix","original_title":"The Matrix","release_date":"1999-03-31","overview":"Von TMDb.","runtime":136,"genres":[{"name":"Action"}]}`))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	defer tmdb.Close()
	res, err := meta.NewResolver(h.st, h.daten, meta.Config{TMDbKey: "x", TMDbURL: tmdb.URL})
	if err != nil {
		t.Fatal(err)
	}
	h.svr().SetResolver(res)

	if resp, raw := h.do("PATCH", "/api/v1/admin/items/"+id+"/metadata",
		`{"overview":"Meine eigene Beschreibung."}`, h.tok); resp.StatusCode != 200 {
		t.Fatalf("%d %s", resp.StatusCode, raw)
	}
	it, _ := h.st.Item(t.Context(), id)
	if err := res.NeuLaden(t.Context(), it); err != nil {
		t.Fatal(err)
	}
	it, _ = h.st.Item(t.Context(), id)
	if it.Overview != "Meine eigene Beschreibung." {
		t.Fatalf("Beschreibung überschrieben: %q", it.Overview)
	}
	if it.OriginalTitle != "The Matrix" || len(it.Genres) != 1 {
		t.Fatalf("ungesperrte Felder nicht geladen: %+v", it)
	}
}

// ------------------------------------------------------- Sammlungen

func TestEigeneSammlungen(t *testing.T) {
	h := newHarness(t)
	a, _ := h.film("Kevin allein zu Haus", 1)
	b, _ := h.film("Der Polarexpress", 1)
	c, _ := h.film("Stirb langsam", 1)
	ctx := t.Context()
	// Freigaben: Stirb langsam ab 16, die anderen ab 6.
	for id, alter := range map[string]int{a: 6, b: 6, c: 16} {
		it, _ := h.st.Item(ctx, id)
		v := alter
		it.AgeYears, it.AgeRating = &v, fmt.Sprint(alter)
		it.Genres = []string{"Weihnachten"}
		_ = h.st.UpsertItem(ctx, it)
	}

	resp, raw := h.do("POST", "/api/v1/admin/sammlungen", `{"name":"Weihnachtsfilme","beschreibung":"Für den Dezember","aufStart":true}`, h.tok)
	if resp.StatusCode != 201 {
		t.Fatalf("anlegen: %d %s", resp.StatusCode, raw)
	}
	var x sammlungAntwort
	_ = json.Unmarshal(raw, &x)
	for _, id := range []string{c, a, b} {
		if resp, raw := h.do("POST", "/api/v1/admin/sammlungen/"+x.ID+"/titel", `{"itemId":"`+id+`"}`, h.tok); resp.StatusCode != 204 {
			t.Fatalf("hinzufügen: %d %s", resp.StatusCode, raw)
		}
	}
	// Von Hand sortiert: b, a, c.
	if resp, _ := h.do("POST", "/api/v1/admin/sammlungen/"+x.ID+"/reihenfolge", `{"ids":["`+b+`","`+a+`","`+c+`"]}`, h.tok); resp.StatusCode != 204 {
		t.Fatal("ordnen")
	}
	_, raw = h.do("GET", "/api/v1/sammlungen/"+x.ID, "", h.tok)
	_ = json.Unmarshal(raw, &x)
	if len(x.Items) != 3 || x.Items[0].ID != b || x.Items[1].ID != a || x.Items[2].ID != c {
		t.Fatalf("Reihenfolge: %s", raw)
	}
	// Nach Titel.
	h.do("POST", "/api/v1/admin/sammlungen/"+x.ID, `{"sortierung":"titel"}`, h.tok)
	_, raw = h.do("GET", "/api/v1/sammlungen/"+x.ID, "", h.tok)
	_ = json.Unmarshal(raw, &x)
	if x.Items[0].ID != a || x.Items[1].ID != b {
		t.Fatalf("nach Titel: %v", []string{x.Items[0].Title, x.Items[1].Title, x.Items[2].Title})
	}

	// Ein Kind (bis 12) sieht nur zwei, und die Startseite hat die Reihe.
	kind := h.gast("Lou", true, h.lib.ID)
	kid, _, _ := h.st.LookupToken(ctx, kind)
	lim, _ := h.st.Limits(ctx, kid.ID)
	lim.MaxAge = 12
	_ = h.st.SetLimits(ctx, kid.ID, lim)
	_, raw = h.do("GET", "/api/v1/sammlungen/"+x.ID, "", kind)
	var k sammlungAntwort
	_ = json.Unmarshal(raw, &k)
	if k.Anzahl != 2 || strings.Contains(string(raw), c) || k.VonHand != nil {
		t.Fatalf("Kind sieht: %s", raw)
	}
	_, raw = h.do("GET", "/api/v1/home", "", kind)
	if !strings.Contains(string(raw), `"sammlung:`+x.ID+`"`) || strings.Contains(string(raw), c) {
		t.Fatalf("Startseite des Kindes: %s", raw)
	}

	// Regel: alles mit Genre Weihnachten aus den Jahren 2019 (alle drei).
	h.do("POST", "/api/v1/admin/sammlungen", `{"name":"Nur Regel","regel":{"genre":"Weihnachten","jahrVon":2019,"jahrBis":2019}}`, h.tok)
	_, raw = h.do("GET", "/api/v1/sammlungen", "", h.tok)
	var alle []sammlungAntwort
	_ = json.Unmarshal(raw, &alle)
	if len(alle) != 2 {
		t.Fatalf("Liste: %s", raw)
	}
	for _, s := range alle {
		if s.Name == "Nur Regel" && s.Anzahl != 3 {
			t.Fatalf("Regel liefert %d", s.Anzahl)
		}
	}
	if resp, raw := h.do("POST", "/api/v1/admin/sammlungen", `{"name":"Falsch","regel":{"jahrVon":2020,"jahrBis":2010}}`, h.tok); resp.StatusCode != 400 {
		t.Fatalf("falscher Jahrbereich: %d %s", resp.StatusCode, raw)
	}

	// Bild hochladen und abrufen.
	resp, raw = h.hochladen("/api/v1/admin/sammlungen/"+x.ID+"/bild", h.tok, pngBild(t, 400, 600))
	if resp.StatusCode != 200 || !strings.Contains(string(raw), `/api/v1/art/`+x.ID+`/poster`) {
		t.Fatalf("Bild: %d %s", resp.StatusCode, raw)
	}
	if resp, _ := h.do("GET", "/api/v1/sammlungen/"+x.ID+"/bild", "", kind); resp.StatusCode != 200 {
		t.Fatalf("Bild für das Kind: %d", resp.StatusCode)
	}

	// „Zu Sammlung hinzufügen" kennt den Stand.
	_, raw = h.do("GET", "/api/v1/admin/sammlungen?item="+a, "", h.tok)
	if !strings.Contains(string(raw), `"enthaelt":true`) {
		t.Fatalf("enthält: %s", raw)
	}
	// Entfernen, löschen. Das Kind darf das alles nicht.
	if resp, _ := h.do("DELETE", "/api/v1/admin/sammlungen/"+x.ID+"/titel/"+a, "", kind); resp.StatusCode != 404 {
		t.Fatalf("Kind entfernt: %d", resp.StatusCode)
	}
	if resp, _ := h.do("DELETE", "/api/v1/admin/sammlungen/"+x.ID+"/titel/"+a, "", h.tok); resp.StatusCode != 204 {
		t.Fatal("entfernen")
	}
	if resp, _ := h.do("DELETE", "/api/v1/admin/sammlungen/"+x.ID, "", h.tok); resp.StatusCode != 204 {
		t.Fatal("löschen")
	}
	if resp, _ := h.do("GET", "/api/v1/sammlungen/"+x.ID, "", h.tok); resp.StatusCode != 404 {
		t.Fatal("gelöschte Sammlung noch da")
	}
}

// Eine Sammlung, aus der jemand nichts sehen darf, gibt es für ihn nicht.
func TestSammlungOhneSichtbareTitel(t *testing.T) {
	h := newHarness(t)
	id, _ := h.film("Nur für Erwachsene", 1)
	it, _ := h.st.Item(t.Context(), id)
	v := 18
	it.AgeYears = &v
	_ = h.st.UpsertItem(t.Context(), it)
	_, raw := h.do("POST", "/api/v1/admin/sammlungen", `{"name":"Geheim"}`, h.tok)
	var x sammlungAntwort
	_ = json.Unmarshal(raw, &x)
	h.do("POST", "/api/v1/admin/sammlungen/"+x.ID+"/titel", `{"itemId":"`+id+`"}`, h.tok)
	kind := h.gast("Lou", true, h.lib.ID)
	kid, _, _ := h.st.LookupToken(t.Context(), kind)
	lim, _ := h.st.Limits(t.Context(), kid.ID)
	lim.MaxAge = 12
	_ = h.st.SetLimits(t.Context(), kid.ID, lim)
	if _, raw := h.do("GET", "/api/v1/sammlungen", "", kind); strings.TrimSpace(string(raw)) != "[]" {
		t.Fatalf("Kind sieht Sammlung: %s", raw)
	}
	if resp, _ := h.do("GET", "/api/v1/sammlungen/"+x.ID, "", kind); resp.StatusCode != 404 {
		t.Fatalf("Kind öffnet Sammlung: %d", resp.StatusCode)
	}
}

// ------------------------------------------------------- Übernahme

func (h *harness) warteAufUebernahme(tok string) map[string]any {
	h.t.Helper()
	ende := time.Now().Add(10 * time.Second)
	for time.Now().Before(ende) {
		_, raw := h.do("GET", "/api/v1/me/uebernahme", "", tok)
		var st map[string]any
		_ = json.Unmarshal(raw, &st)
		if p := st["phase"]; p != "verbinden" && p != "laden" {
			return st
		}
		time.Sleep(20 * time.Millisecond)
	}
	h.t.Fatal("Übernahme wird nicht fertig")
	return nil
}

func uebernahmeDurchlauf(t *testing.T, art string) {
	h := newHarness(t)
	matrix, _ := h.film("Matrix", 1)
	_ = h.st.SetSourceID(t.Context(), matrix, "tmdb", "603")
	sommer, sommerDatei := h.film("Sommer 2019", 1)
	_ = sommerDatei
	gtok := h.gast("Mara", false, h.lib.ID)

	n := nachbau.Neu(art)
	defer n.Close()
	n.Titel = []nachbau.Titel{
		nachbau.Film("e1", "The Matrix", 1999, "/emby/Matrix.mkv", 5, map[string]string{"Tmdb": "603"},
			map[string]any{"Played": true}),
		nachbau.Film("e2", "Sommer", 0, "/emby/filme/Sommer 2019.mkv", 1024*1024, nil,
			map[string]any{"PlaybackPositionTicks": 30_000_000_000, "IsFavorite": true}),
		nachbau.Film("e3", "Fehlt", 2000, "/x/fehlt.mkv", 3, nil, map[string]any{"Played": true}),
	}

	// Server erkennen.
	resp, raw := h.do("POST", "/api/v1/me/uebernahme/server", `{"adresse":"`+n.Adresse()+`"}`, gtok)
	if resp.StatusCode != 200 || !strings.Contains(string(raw), `"art":"`+art+`"`) {
		t.Fatalf("Server: %d %s", resp.StatusCode, raw)
	}
	// Vor der Anmeldung nichts vom fremden Server: kein Name, keine
	// Fassung, keine Benutzerliste.
	if strings.Contains(string(raw), "alex") || strings.Contains(string(raw), "version") || strings.Contains(string(raw), `"name"`) {
		t.Fatalf("Server verrät vor der Anmeldung zu viel: %s", raw)
	}
	if resp, raw := h.do("POST", "/api/v1/me/uebernahme/server", `{"adresse":"file:///etc"}`, gtok); resp.StatusCode != 400 {
		t.Fatalf("file:// angenommen: %d %s", resp.StatusCode, raw)
	}

	// Falsches Passwort.
	h.do("POST", "/api/v1/me/uebernahme", `{"adresse":"`+n.Adresse()+`","benutzer":"alex","passwort":"falsch"}`, gtok)
	st := h.warteAufUebernahme(gtok)
	if st["phase"] != "fehler" || !strings.Contains(fmt.Sprint(st["meldung"]), "Passwort") {
		t.Fatalf("falsches Passwort: %v", st)
	}

	resp, raw = h.do("POST", "/api/v1/me/uebernahme", `{"adresse":"`+n.Adresse()+`","benutzer":"alex","passwort":"geheim"}`, gtok)
	if resp.StatusCode != 202 {
		t.Fatalf("starten: %d %s", resp.StatusCode, raw)
	}
	st = h.warteAufUebernahme(gtok)
	if st["phase"] != "bereit" {
		t.Fatalf("Stand: %v", st)
	}
	if !n.Abgemeldet {
		t.Fatal("Token bei Emby nicht abgemeldet")
	}
	v := st["vorschau"].(map[string]any)
	if v["gesehen"].(float64) != 1 || v["angefangen"].(float64) != 1 || v["favoriten"].(float64) != 1 || v["nichtGefunden"].(float64) != 1 {
		t.Fatalf("Vorschau: %v", v)
	}

	// Der Besitzer sieht von all dem nichts — weder über seinen eigenen
	// Stand noch über die Verwaltung.
	_, raw = h.do("GET", "/api/v1/me/uebernahme", "", h.tok)
	if !strings.Contains(string(raw), `"phase":"keine"`) {
		t.Fatalf("Besitzer sieht fremde Übernahme: %s", raw)
	}
	if resp, _ := h.do("POST", "/api/v1/me/uebernahme/anwenden", `{}`, h.tok); resp.StatusCode != 404 {
		t.Fatalf("Besitzer wendet fremde Übernahme an: %d", resp.StatusCode)
	}
	_, logRaw := h.do("GET", "/api/v1/admin/log", "", h.tok)
	if strings.Contains(string(logRaw), "Matrix") || strings.Contains(strings.ToLower(string(logRaw)), "übernahme") {
		t.Fatalf("Protokoll verrät die Übernahme: %s", logRaw)
	}

	resp, raw = h.do("POST", "/api/v1/me/uebernahme/anwenden", `{"gesehen":true,"angefangen":true,"favoriten":true,"bewertungen":true}`, gtok)
	if resp.StatusCode != 200 || !strings.Contains(string(raw), `"phase":"fertig"`) {
		t.Fatalf("anwenden: %d %s", resp.StatusCode, raw)
	}
	mara, _, _ := h.st.LookupToken(t.Context(), gtok)
	if p, err := h.st.Progress(t.Context(), mara.ID, matrix); err != nil || !p.Finished {
		t.Fatalf("Matrix nicht gesehen: %v", err)
	}
	if p, err := h.st.Progress(t.Context(), mara.ID, sommer); err != nil || p.PositionMS != 3_000_000 {
		t.Fatalf("Sommer: %+v %v", p, err)
	}
	if favs, _ := h.st.FavoritIDs(t.Context(), mara.ID); len(favs) != 1 {
		t.Fatalf("Favoriten: %v", favs)
	}
	besitzer, _, _ := h.st.LookupToken(t.Context(), h.tok)
	if _, err := h.st.Progress(t.Context(), besitzer.ID, matrix); err == nil {
		t.Fatal("der Besitzer hat Stand bekommen")
	}
	// Zweimal anwenden geht nicht.
	if resp, _ := h.do("POST", "/api/v1/me/uebernahme/anwenden", `{}`, gtok); resp.StatusCode != 409 {
		t.Fatalf("zweites Anwenden: %d", resp.StatusCode)
	}
	if resp, _ := h.do("DELETE", "/api/v1/me/uebernahme", "", gtok); resp.StatusCode != 204 {
		t.Fatal("verwerfen")
	}
}

func TestUebernahmeEmbyUeberDieSchnittstelle(t *testing.T)     { uebernahmeDurchlauf(t, "Emby") }
func TestUebernahmeJellyfinUeberDieSchnittstelle(t *testing.T) { uebernahmeDurchlauf(t, "Jellyfin") }

// Was ein Kind nicht sehen darf, wird nicht zugeordnet — es zählt als
// „nicht gefunden", ohne zu verraten, dass es den Titel gibt.
func TestUebernahmeAchtetFreigabe(t *testing.T) {
	h := newHarness(t)
	id, _ := h.film("Horror", 1)
	_ = h.st.SetSourceID(t.Context(), id, "tmdb", "999")
	it, _ := h.st.Item(t.Context(), id)
	v := 18
	it.AgeYears = &v
	_ = h.st.UpsertItem(t.Context(), it)
	kind := h.gast("Lou", true, h.lib.ID)
	kid, _, _ := h.st.LookupToken(t.Context(), kind)
	lim, _ := h.st.Limits(t.Context(), kid.ID)
	lim.MaxAge = 12
	_ = h.st.SetLimits(t.Context(), kid.ID, lim)

	n := nachbau.Neu("Emby")
	defer n.Close()
	n.Titel = []nachbau.Titel{nachbau.Film("e1", "Horror", 2019, "/x/Horror.mkv", 1024*1024,
		map[string]string{"Tmdb": "999"}, map[string]any{"Played": true})}
	h.do("POST", "/api/v1/me/uebernahme", `{"adresse":"`+n.Adresse()+`","benutzer":"alex","passwort":"geheim"}`, kind)
	st := h.warteAufUebernahme(kind)
	v2 := st["vorschau"].(map[string]any)
	if v2["gesehen"].(float64) != 0 || v2["nichtGefunden"].(float64) != 1 {
		t.Fatalf("Kind: %v", v2)
	}
}

// Ein Kinderprofil mit Altersgrenze 0 („nur FSK 0") bekommt nichts
// zugeordnet, was älter freigegeben ist — früher galt bei 0 gar keine
// Grenze. Auch das Anwenden schreibt nur Sichtbares.
func TestUebernahmeKindFSK0(t *testing.T) {
	h := newHarness(t)
	id, _ := h.film("Horror", 1)
	_ = h.st.SetSourceID(t.Context(), id, "tmdb", "999")
	it, _ := h.st.Item(t.Context(), id)
	v := 12
	it.AgeYears = &v
	_ = h.st.UpsertItem(t.Context(), it)
	frei, _ := h.film("Biene", 1)
	_ = h.st.SetSourceID(t.Context(), frei, "tmdb", "111")
	it2, _ := h.st.Item(t.Context(), frei)
	null := 0
	it2.AgeYears = &null
	_ = h.st.UpsertItem(t.Context(), it2)

	kind := h.gast("Lou", true, h.lib.ID)
	kid, _, _ := h.st.LookupToken(t.Context(), kind)
	lim, _ := h.st.Limits(t.Context(), kid.ID)
	lim.MaxAge = 0
	_ = h.st.SetLimits(t.Context(), kid.ID, lim)

	n := nachbau.Neu("Emby")
	defer n.Close()
	n.Titel = []nachbau.Titel{
		nachbau.Film("e1", "Horror", 2019, "/x/Horror.mkv", 1024*1024, map[string]string{"Tmdb": "999"}, map[string]any{"Played": true}),
		nachbau.Film("e2", "Biene", 2019, "/x/Biene.mkv", 1024*1024, map[string]string{"Tmdb": "111"}, map[string]any{"Played": true}),
	}
	h.do("POST", "/api/v1/me/uebernahme", `{"adresse":"`+n.Adresse()+`","benutzer":"alex","passwort":"geheim"}`, kind)
	st := h.warteAufUebernahme(kind)
	v2, _ := st["vorschau"].(map[string]any)
	if v2 == nil || v2["gesehen"].(float64) != 1 || v2["nichtGefunden"].(float64) != 1 {
		t.Fatalf("Kind mit FSK 0: %v", st)
	}
	if resp, raw := h.do("POST", "/api/v1/me/uebernahme/anwenden", `{}`, kind); resp.StatusCode != 200 {
		t.Fatalf("anwenden: %d %s", resp.StatusCode, raw)
	}
	if _, err := h.st.Progress(t.Context(), kid.ID, id); err == nil {
		t.Fatal("Stand für einen nicht freigegebenen Titel geschrieben")
	}
	if p, err := h.st.Progress(t.Context(), kid.ID, frei); err != nil || !p.Finished {
		t.Fatalf("freigegebener Titel fehlt: %v", err)
	}
}

// Missbrauch der Übernahme: leeres Passwort, von unterwegs, Bremse je
// Konto, und immer dieselbe Meldung für „nichts da" und „gesperrt".
func TestUebernahmeMissbrauch(t *testing.T) {
	h := newHarness(t)
	h.film("Matrix", 1)
	gtok := h.gast("Mara", false, h.lib.ID)
	n := nachbau.Neu("Emby")
	defer n.Close()

	if resp, raw := h.do("POST", "/api/v1/me/uebernahme", `{"adresse":"`+n.Adresse()+`","benutzer":"alex","passwort":""}`, gtok); resp.StatusCode != 400 {
		t.Fatalf("leeres Passwort: %d %s", resp.StatusCode, raw)
	}
	// Öffentliche Adresse und geschlossener Port: dieselbe Antwort.
	_, gesperrt := h.do("POST", "/api/v1/me/uebernahme/server", `{"adresse":"http://93.184.216.34:8920"}`, gtok)
	_, zu := h.do("POST", "/api/v1/me/uebernahme/server", `{"adresse":"http://127.0.0.1:1"}`, gtok)
	var a, b map[string]any
	_ = json.Unmarshal(gesperrt, &a)
	_ = json.Unmarshal(zu, &b)
	if a["error"] == nil || a["error"] != b["error"] {
		t.Fatalf("Meldungen unterscheiden sich: %s / %s", gesperrt, zu)
	}
	// Nach ein paar Fehlversuchen bremst Lumen — auch für eine gute Adresse.
	gebremst := false
	for i := 0; i < 6 && !gebremst; i++ {
		resp, _ := h.do("POST", "/api/v1/me/uebernahme/server", `{"adresse":"http://127.0.0.1:1"}`, gtok)
		gebremst = resp.StatusCode == 429
	}
	if !gebremst {
		t.Fatal("keine Bremse beim Abtasten")
	}
	if resp, _ := h.do("POST", "/api/v1/me/uebernahme/server", `{"adresse":"`+n.Adresse()+`"}`, gtok); resp.StatusCode != 429 {
		t.Fatalf("Bremse gilt nicht für die nächste Adresse: %d", resp.StatusCode)
	}
	// Von unterwegs gar nicht.
	andere := h.gast("Lou", false, h.lib.ID)
	req := httptest.NewRequest("POST", "/api/v1/me/uebernahme/server", strings.NewReader(`{"adresse":"`+n.Adresse()+`"}`))
	req.Host = "vermittler.example:8443"
	h.api.SetzeErlaubteHosts("vermittler.example:8444")
	req.Header.Set("Authorization", "Bearer "+andere)
	rec := httptest.NewRecorder()
	VonAussen(h.api.Handler()).ServeHTTP(rec, req)
	if rec.Code != 403 || !strings.Contains(rec.Body.String(), `"unterwegs"`) {
		t.Fatalf("von unterwegs: %d %s", rec.Code, rec.Body.String())
	}
}

// Höchstens ein Lauf auf dem ganzen Server.
func TestUebernahmeNurEinLauf(t *testing.T) {
	h := newHarness(t)
	h.film("Matrix", 1)
	a := h.gast("Mara", false, h.lib.ID)
	b := h.gast("Lou", false, h.lib.ID)
	n := nachbau.Neu("Emby")
	defer n.Close()
	uebernahmeAmLaufen <- struct{}{} // ein anderer Lauf ist gerade dran
	resp, raw := h.do("POST", "/api/v1/me/uebernahme", `{"adresse":"`+n.Adresse()+`","benutzer":"alex","passwort":"geheim"}`, a)
	<-uebernahmeAmLaufen
	if resp.StatusCode != 409 {
		t.Fatalf("zweiter Lauf angenommen: %d %s", resp.StatusCode, raw)
	}
	if resp, raw := h.do("POST", "/api/v1/me/uebernahme", `{"adresse":"`+n.Adresse()+`","benutzer":"alex","passwort":"geheim"}`, b); resp.StatusCode != 202 {
		t.Fatalf("nach dem Ende kein Lauf möglich: %d %s", resp.StatusCode, raw)
	}
	h.warteAufUebernahme(b)
}

// ------------------------------------------------------- Untertitel

func TestUntertitelSuchenUndHolen(t *testing.T) {
	h := newHarness(t)
	id, fileID := h.film("Matrix", 1)
	it, _ := h.st.Item(t.Context(), id)
	it.IMDbID = "tt0133093"
	_ = h.st.UpsertItem(t.Context(), it)
	// Das Video bekommt ein altes Datum; die Untertiteldatei soll es erben.
	alt := time.Date(2018, 1, 2, 3, 4, 5, 0, time.UTC)
	_ = os.Chtimes(filepath.Join(h.root, "Matrix.mkv"), alt, alt)
	_ = os.WriteFile(filepath.Join(h.root, "Matrix.de.srt"), []byte("1\n00:00:01,000 --> 00:00:02,000\nalt\n"), 0o644)

	n := osnachbau.Neu()
	defer n.Close()
	n.Untertitel = []osnachbau.Untertitel{
		{DateiID: 7, Sprache: "de", IMDb: "133093", Name: "Matrix", Downloads: 3,
			Inhalt: "1\n00:00:01,000 --> 00:00:02,000\nNeu von OpenSubtitles\n"},
		{DateiID: 8, Sprache: "fr", IMDb: "133093", Name: "Matrix", Inhalt: "x"},
	}
	t.Setenv("LUMEN_OPENSUBTITLES_URL", n.Adresse())

	gtok := h.gast("Mara", false, h.lib.ID)
	_, raw := h.do("GET", "/api/v1/untertitel/verfuegbar", "", gtok)
	if !strings.Contains(string(raw), `"verfuegbar":false`) {
		t.Fatalf("ohne Schlüssel: %s", raw)
	}
	resp, raw := h.do("GET", "/api/v1/items/"+id+"/untertitel/suche", "", gtok)
	if resp.StatusCode != http.StatusPreconditionFailed || !strings.Contains(string(raw), "noch nicht eingerichtet") {
		t.Fatalf("Suche ohne Schlüssel: %d %s", resp.StatusCode, raw)
	}

	// Die Verwaltung richtet ein. Der Schlüssel geht nie zurück.
	resp, raw = h.do("PUT", "/api/v1/admin/untertitel",
		`{"apiKey":"schluessel-123","benutzer":"alex","passwort":"os-geheim","pruefen":true}`, h.tok)
	if resp.StatusCode != 200 || strings.Contains(string(raw), "schluessel-123") || strings.Contains(string(raw), "os-geheim") {
		t.Fatalf("einrichten: %d %s", resp.StatusCode, raw)
	}
	_, logRaw := h.do("GET", "/api/v1/admin/log", "", h.tok)
	if strings.Contains(string(logRaw), "schluessel-123") || strings.Contains(string(logRaw), "os-geheim") {
		t.Fatal("Schlüssel im Protokoll")
	}
	// Ein Gast kommt an die Einstellungen nicht heran.
	if resp, _ := h.do("GET", "/api/v1/admin/untertitel", "", gtok); resp.StatusCode != 404 {
		t.Fatalf("Gast liest Einstellungen: %d", resp.StatusCode)
	}

	resp, raw = h.do("GET", "/api/v1/items/"+id+"/untertitel/suche?sprachen=de", "", gtok)
	if resp.StatusCode != 200 || !strings.Contains(string(raw), `"dateiId":7`) || strings.Contains(string(raw), `"dateiId":8`) {
		t.Fatalf("Suche: %d %s", resp.StatusCode, raw)
	}
	resp, raw = h.do("POST", "/api/v1/items/"+id+"/untertitel", fmt.Sprintf(`{"datei":%q,"dateiId":7,"sprache":"de"}`, fileID), gtok)
	if resp.StatusCode != 200 {
		t.Fatalf("holen: %d %s", resp.StatusCode, raw)
	}
	var out struct {
		Tracks []store.Track
		Neu    int
	}
	_ = json.Unmarshal(raw, &out)
	// Nicht neben den Film: Der Medienordner bleibt, wie er war.
	if _, err := os.Stat(filepath.Join(h.root, "Matrix.de.2.srt")); err == nil {
		t.Fatal("Untertitel neben den Film geschrieben")
	}
	neu := filepath.Join(h.daten, "untertitel-geholt", fileID+".de.srt")
	if b, err := os.ReadFile(neu); err != nil || !strings.Contains(string(b), "Neu von OpenSubtitles") {
		t.Fatalf("geholte Datei: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(h.root, "Matrix.de.srt")); !strings.Contains(string(b), "alt") {
		t.Fatal("vorhandene Untertitel überschrieben")
	}
	for _, p := range []string{neu, filepath.Dir(neu)} {
		if st, _ := os.Stat(p); st.ModTime().Year() != 2000 {
			t.Fatalf("Zeitstempel verrät das Holen: %s %v", p, st.ModTime())
		}
	}
	if out.Neu < store.GeholtSpurAb || len(out.Tracks) != 2 {
		t.Fatalf("Spuren: %s", raw)
	}
	// Sofort als Spur abrufbar.
	resp, raw = h.do("GET", fmt.Sprintf("/api/v1/stream/%s/subtitles/%d.vtt", fileID, out.Neu), "", gtok)
	if resp.StatusCode != 200 || !strings.Contains(string(raw), "Neu von OpenSubtitles") {
		t.Fatalf("neue Spur: %d %s", resp.StatusCode, raw)
	}
	// Ein Durchlauf, der die Spuren neu schreibt, verliert sie nicht.
	_ = h.st.SpurenSetzen(t.Context(), fileID, nil, nil)
	if sp, _ := h.st.Spuren(t.Context(), fileID); len(sp) != 1 || sp[0].Index != out.Neu { // nur die geholte bleibt
		t.Fatalf("nach neuem Messen: %+v", sp)
	}
	// Die Sprache ist fürs nächste Mal gemerkt.
	_, raw = h.do("GET", "/api/v1/untertitel/verfuegbar", "", gtok)
	if !strings.Contains(string(raw), `"sprachen":["de"]`) {
		t.Fatalf("Sprachen: %s", raw)
	}
	// Je Datei und Sprache höchstens drei.
	for i := 0; i < 2; i++ {
		if resp, raw := h.do("POST", "/api/v1/items/"+id+"/untertitel", `{"dateiId":7,"sprache":"de"}`, gtok); resp.StatusCode != 200 {
			t.Fatalf("weitere holen: %d %s", resp.StatusCode, raw)
		}
	}
	if resp, raw := h.do("POST", "/api/v1/items/"+id+"/untertitel", `{"dateiId":7,"sprache":"de"}`, gtok); resp.StatusCode != http.StatusConflict {
		t.Fatalf("vierte: %d %s", resp.StatusCode, raw)
	}
	// Ein Kinderprofil holt nicht und bekommt die Suche nicht angeboten.
	kind := h.gast("Lou", true, h.lib.ID)
	kid, _, _ := h.st.LookupToken(t.Context(), kind)
	klim, _ := h.st.Limits(t.Context(), kid.ID)
	klim.AllowUnrated = true // damit das Kind den Film überhaupt sieht
	_ = h.st.SetLimits(t.Context(), kid.ID, klim)
	if _, raw := h.do("GET", "/api/v1/untertitel/verfuegbar", "", kind); !strings.Contains(string(raw), `"verfuegbar":false`) {
		t.Fatalf("Kind: %s", raw)
	}
	if resp, _ := h.do("POST", "/api/v1/items/"+id+"/untertitel", `{"dateiId":7,"sprache":"en"}`, kind); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("Kind holt: %d", resp.StatusCode)
	}
	// Tagesgrenze je Profil.
	for i := 0; i < utHolenJeTag; i++ {
		utHolenErlaubt("zaehl-test")
	}
	if utHolenErlaubt("zaehl-test") {
		t.Fatal("keine Tagesgrenze")
	}
	// Kontingent aufgebraucht: verständliche Meldung.
	n.Rest = 0
	resp, raw = h.do("POST", "/api/v1/items/"+id+"/untertitel", `{"dateiId":7,"sprache":"de"}`, gtok)
	if resp.StatusCode != http.StatusTooManyRequests || !strings.Contains(string(raw), "Morgen") {
		t.Fatalf("Kontingent: %d %s", resp.StatusCode, raw)
	}
}

// Das automatische Holen nimmt nur Sicheres und hält das Tageslimit ein.
func TestUntertitelAutomatisch(t *testing.T) {
	h := newHarness(t)
	utPause = 0
	ids := []string{}
	for _, name := range []string{"Alpha", "Beta", "Gamma"} {
		id, fid := h.film(name, 1)
		_ = h.st.SpurenSetzen(t.Context(), fid, nil, nil)
		it, _ := h.st.Item(t.Context(), id)
		it.IMDbID = map[string]string{"Alpha": "tt1", "Beta": "tt2", "Gamma": "tt3"}[name]
		_ = h.st.UpsertItem(t.Context(), it)
		ids = append(ids, id)
	}
	n := osnachbau.Neu()
	defer n.Close()
	n.Rest = 50
	srt := "1\n00:00:01,000 --> 00:00:02,000\nHallo\n"
	n.Untertitel = []osnachbau.Untertitel{
		{DateiID: 1, Sprache: "de", IMDb: "1", Name: "Alpha", Inhalt: srt},
		{DateiID: 2, Sprache: "de", IMDb: "2", Name: "Beta", Maschinell: true, Inhalt: srt}, // unsicher
		{DateiID: 3, Sprache: "de", IMDb: "3", Name: "Gamma", Inhalt: srt},
	}
	t.Setenv("LUMEN_OPENSUBTITLES_URL", n.Adresse())
	h.do("PUT", "/api/v1/admin/untertitel", `{"apiKey":"schluessel-123","auto":true,"sprachen":["de"],"tageslimit":1}`, h.tok)

	if got := h.svr().untertitelDurchgang(t.Context()); got != 1 {
		t.Fatalf("erster Durchgang: %d", got)
	}
	// Tageslimit erreicht.
	if got := h.svr().untertitelDurchgang(t.Context()); got != 0 {
		t.Fatalf("über dem Tageslimit: %d", got)
	}
	h.do("PUT", "/api/v1/admin/untertitel", `{"tageslimit":10}`, h.tok)
	if got := h.svr().untertitelDurchgang(t.Context()); got != 1 {
		t.Fatalf("zweiter Durchgang: %d", got)
	}
	// Die maschinelle Übersetzung für „Beta" bleibt liegen.
	if files, _ := h.st.FilesFor(t.Context(), ids[1]); len(files) == 0 || geholtIn(h.st.GeholteUntertitel(files[0].ID), "de") {
		t.Fatal("unsicherer Treffer automatisch geholt")
	}
	for _, name := range []string{"Alpha", "Gamma"} {
		files, _ := h.st.FilesFor(t.Context(), map[string]string{"Alpha": ids[0], "Gamma": ids[2]}[name])
		if len(files) == 0 || !geholtIn(h.st.GeholteUntertitel(files[0].ID), "de") {
			t.Fatalf("%s fehlt", name)
		}
	}
	// Nichts mehr offen: „Beta" ist für zwei Wochen gemerkt.
	if got := h.svr().untertitelDurchgang(t.Context()); got != 0 {
		t.Fatalf("dritter Durchgang: %d", got)
	}
	_, raw := h.do("GET", "/api/v1/admin/log", "", h.tok)
	for _, name := range []string{"Alpha", "Gamma"} {
		if strings.Contains(string(raw), name) {
			t.Fatalf("Titel im Protokoll: %s", raw)
		}
	}
}
