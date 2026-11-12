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
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func schreiben(t *testing.T, pfad, inhalt string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(pfad), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pfad, []byte(inhalt), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Ein Film aus einer Emby-Sammlung, ohne TMDb-Schlüssel: Die Titelseite
// zeigt Besetzung, Regie, Tagline, Logo, Querformat und die Extras — und
// ein Extra lässt sich abspielen.
func TestTitelseiteMitNFOUndExtras(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()
	ordner := filepath.Join(h.root, "The Matrix (1999)")
	schreiben(t, filepath.Join(ordner, "The Matrix (1999).mkv"), "FILM")
	schreiben(t, filepath.Join(ordner, "trailers", "Teaser.mp4"), "TRAILERDATEN")
	schreiben(t, filepath.Join(ordner, "logo.png"), "LOGO")
	schreiben(t, filepath.Join(ordner, "landscape.jpg"), "QUER")
	schreiben(t, filepath.Join(ordner, "movie.nfo"), `<movie><title>Matrix</title><year>1999</year>
<plot>Neo.</plot><tagline>Willkommen in der Wirklichkeit.</tagline><director>Lana Wachowski</director>
<uniqueid type="imdb">tt0133093</uniqueid>
<actor><name>Keanu Reeves</name><role>Neo</role></actor></movie>`)
	lib, err := h.st.CreateLibrary(ctx, "Filme", "movie", h.root, "de")
	if err != nil {
		t.Fatal(err)
	}
	h.api.scanner.BilderOrdner = h.api.meta.ArtDir()
	if _, err := h.api.scanner.Scan(ctx, *lib); err != nil {
		t.Fatal(err)
	}
	_, raw := h.do("GET", "/api/v1/items?kind=movie", "", h.tok)
	var liste []struct {
		ID, Title, MatchState, Logo, Landscape string
	}
	_ = json.Unmarshal(raw, &liste)
	if len(liste) != 1 || liste[0].MatchState != "lokal" || liste[0].Logo == "" || liste[0].Landscape == "" {
		t.Fatalf("Liste: %s", raw)
	}
	resp, raw := h.do("GET", "/api/v1/items/"+liste[0].ID, "", h.tok)
	if resp.StatusCode != 200 {
		t.Fatalf("%d %s", resp.StatusCode, raw)
	}
	var d struct {
		Cast []struct{ Name, Rolle string }
		NFO  struct {
			Tagline string
			Regie   []string
		}
		Extras []struct{ ID, Titel, Art, URL string }
	}
	_ = json.Unmarshal(raw, &d)
	if len(d.Cast) != 1 || d.Cast[0].Name != "Keanu Reeves" || d.NFO.Tagline == "" || len(d.NFO.Regie) != 1 {
		t.Errorf("NFO-Angaben fehlen: %s", raw)
	}
	if len(d.Extras) != 1 || d.Extras[0].Art != "trailer" || strings.Contains(d.Extras[0].URL, "Teaser") {
		t.Fatalf("Extras: %+v", d.Extras)
	}
	if resp, raw := h.do("GET", d.Extras[0].URL, "", h.tok); resp.StatusCode != 200 || string(raw) != "TRAILERDATEN" {
		t.Errorf("Extra abspielen: %d %q", resp.StatusCode, raw)
	}
	if resp, _ := h.do("GET", "/api/v1/stream/"+liste[0].ID+"/extra/0000000000000000", "", h.tok); resp.StatusCode != http.StatusNotFound {
		t.Errorf("fremdes Extra: %d", resp.StatusCode)
	}
	if resp, raw := h.do("GET", liste[0].Logo, "", h.tok); resp.StatusCode != 200 || string(raw) != "LOGO" {
		t.Errorf("Logo: %d %q", resp.StatusCode, raw)
	}
}

// „ID eingeben" ohne Schlüssel: Laden sagt mit Code, was fehlt; Speichern
// merkt die Kennung vor.
func TestKennungOhneSchluessel(t *testing.T) {
	h := newHarness(t)
	id, _ := h.film("Se7en", 1)
	if _, err := h.st.ZuordnungsZustandSetzen(t.Context(), []string{id}, "pending"); err != nil {
		t.Fatal(err)
	}
	resp, raw := h.do("GET", "/api/v1/admin/items/"+id+"/metadata/search?q=tt0114369", "", h.tok)
	if resp.StatusCode != http.StatusPreconditionFailed || !strings.Contains(string(raw), `"ohne_schluessel"`) {
		t.Fatalf("Suchen ohne Schlüssel: %d %s", resp.StatusCode, raw)
	}
	resp, raw = h.do("POST", "/api/v1/admin/items/"+id+"/metadata/kennung", `{"eingabe":"https://www.imdb.com/title/tt0114369/"}`, h.tok)
	if resp.StatusCode != 200 {
		t.Fatalf("Kennung speichern: %d %s", resp.StatusCode, raw)
	}
	it, _ := h.st.Item(t.Context(), id)
	if it.IMDbID != "tt0114369" || it.MatchState != "lokal" {
		t.Errorf("IMDb %q, Zustand %q", it.IMDbID, it.MatchState)
	}
	resp, raw = h.do("POST", "/api/v1/admin/items/"+id+"/metadata/kennung", `{"eingabe":"https://www.themoviedb.org/tv/1399"}`, h.tok)
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(raw), "falsche_art") {
		t.Errorf("Serien-Link bei Film: %d %s", resp.StatusCode, raw)
	}
	_, raw = h.do("GET", "/api/v1/admin/cleanup", "", h.tok)
	if !strings.Contains(string(raw), `"schluessel":"fehlt"`) || !strings.Contains(string(raw), `"vorgemerkt":1`) {
		t.Errorf("Aufräumen: %s", raw)
	}
}
