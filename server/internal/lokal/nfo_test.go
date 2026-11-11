// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package lokal

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"lumen/internal/store"
)

// So schreiben Kodi, Emby/Jellyfin und tinyMediaManager ihre NFO-Dateien.
func TestNFOAus(t *testing.T) {
	cases := []struct {
		name string
		roh  string
		want NFO
	}{
		{"Kodi v18+ mit uniqueid und ratings", `<?xml version="1.0" encoding="UTF-8" standalone="yes" ?>
<movie>
  <title>Das Boot</title><originaltitle>Das Boot</originaltitle>
  <ratings><rating name="themoviedb" max="10"><value>7.9</value><votes>1200</votes></rating>
    <rating name="imdb" max="10" default="true"><value>8.4</value><votes>250000</votes></rating></ratings>
  <plot>U 96 läuft aus.</plot><runtime>149</runtime>
  <mpaa>de:FSK 12 / US:R</mpaa>
  <uniqueid type="imdb" default="true">tt0082096</uniqueid><uniqueid type="tmdb">387</uniqueid>
  <genre>Drama</genre><genre>Krieg</genre>
  <premiered>1981-09-17</premiered>
</movie>`, NFO{Art: "movie", Titel: "Das Boot", Originaltitel: "Das Boot", Jahr: 1981, Premiere: "1981-09-17",
			Inhalt: "U 96 läuft aus.", Genres: []string{"Drama", "Krieg"}, Freigabe: "FSK 12", Bewertung: 8.4,
			Stimmen: 250000, LaufzeitMin: 149, TMDb: "387", IMDb: "tt0082096"}},
		{"alte Kodi-Datei mit id und rating", `<movie><title>Alien</title><year>1979</year>
<rating>8.5</rating><votes>900</votes><id>tt0078748</id><tmdbid>348</tmdbid><plot>Im All.</plot>
<genre>Horror / Science-Fiction</genre><mpaa>Rated R</mpaa></movie>`,
			NFO{Art: "movie", Titel: "Alien", Jahr: 1979, Inhalt: "Im All.", Genres: []string{"Horror", "Science-Fiction"},
				Freigabe: "R", Bewertung: 8.5, Stimmen: 900, TMDb: "348", IMDb: "tt0078748"}},
		{"Serie mit TVDb in id", `<tvshow><title>Heimat</title><year>1984</year><plot>Hunsrück.</plot><id>78999</id></tvshow>`,
			NFO{Art: "tvshow", Titel: "Heimat", Jahr: 1984, Inhalt: "Hunsrück.", TVDb: "78999"}},
		{"Folge", `<episodedetails><title>Fernweh</title><season>1</season><episode>3</episode>
<aired>1984-09-16</aired><plot>Paul.</plot></episodedetails>
<episodedetails><title>Zweite</title></episodedetails>`,
			NFO{Art: "episodedetails", Titel: "Fernweh", Jahr: 1984, Premiere: "1984-09-16", Inhalt: "Paul.", Staffel: 1, Folge: 3}},
		{"XML mit angehängter Adresse", `<movie><title>Brazil</title></movie>
https://www.themoviedb.org/movie/68-brazil`, NFO{Art: "movie", Titel: "Brazil", TMDb: "68"}},
		{"mit Sortiertitel", `<movie><title>Der Herr der Ringe: Die Gefährten</title>
<sorttitle>Herr der Ringe 1</sorttitle></movie>`, NFO{Art: "movie", Titel: "Der Herr der Ringe: Die Gefährten",
			Sortiertitel: "Herr der Ringe 1"}},
		{"nur eine Adresse", `https://www.imdb.com/title/tt0088846/`, NFO{IMDb: "tt0088846"}},
		// So schreibt Emby mit eingeschaltetem „NFO-Speicher" (movie.nfo
		// oder <Dateiname>.nfo): eigene Felder und uniqueid mit grossem
		// Anfangsbuchstaben.
		{"Emby-Film", `<?xml version="1.0" encoding="utf-8" standalone="yes"?>
<movie>
  <plot><![CDATA[Ein Hacker erfährt die Wahrheit.]]></plot>
  <lockdata>false</lockdata>
  <title>Matrix</title><originaltitle>The Matrix</originaltitle>
  <year>1999</year>
  <imdbid>tt0133093</imdbid><tmdbid>603</tmdbid>
  <uniqueid type="Imdb">tt0133093</uniqueid><uniqueid type="Tmdb">603</uniqueid>
</movie>`, NFO{Art: "movie", Titel: "Matrix", Originaltitel: "The Matrix", Jahr: 1999,
			Inhalt: "Ein Hacker erfährt die Wahrheit.", TMDb: "603", IMDb: "tt0133093"}},
		{"Emby-Serie", `<tvshow><title>Breaking Bad</title><year>2008</year>
  <uniqueid type="Tvdb">81189</uniqueid><uniqueid type="Imdb">tt0903747</uniqueid><tvdbid>81189</tvdbid></tvshow>`,
			NFO{Art: "tvshow", Titel: "Breaking Bad", Jahr: 2008, TVDb: "81189", IMDb: "tt0903747"}},
		{"Adresse mitten im XML", `<movie><title>Brazil</title>
  <trailer>x</trailer><url>https://www.imdb.com/title/tt0088846/</url></movie>`,
			NFO{Art: "movie", Titel: "Brazil", IMDb: "tt0088846"}},
		{"tmdbid und imdb_id als eigene Felder", `<movie><title>Alien</title><imdb_id>tt0078748</imdb_id><tmdbid>348</tmdbid></movie>`,
			NFO{Art: "movie", Titel: "Alien", TMDb: "348", IMDb: "tt0078748"}},
		{"Latin-1", "<?xml version=\"1.0\" encoding=\"ISO-8859-1\"?><movie><title>Sch\xf6ne T\xfcte</title></movie>",
			NFO{Art: "movie", Titel: "Schöne Tüte"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			n, err := NFOAus([]byte(c.roh))
			if err != nil {
				t.Fatal(err)
			}
			if a, b := mustJSON(*n), mustJSON(c.want); a != b {
				t.Errorf("\n ist  %s\n soll %s", a, b)
			}
		})
	}
	if _, err := NFOAus([]byte("<movie><title>")); err == nil {
		t.Error("kaputte NFO ohne Fehler")
	}
	if _, err := NFOAus([]byte("Ripped by XYZ — greetz")); err == nil {
		t.Error("Release-Info als NFO gelesen")
	}
}

func TestVollstaendigUndAnwenden(t *testing.T) {
	n := NFO{Art: "movie", Titel: "A", Inhalt: "B", Jahr: 2000, Genres: []string{"Drama"}, Freigabe: "16", IMDb: "tt1"}
	if !n.Vollstaendig() || n.HatKennung() != true {
		t.Error("Vollstaendig/HatKennung")
	}
	if (&NFO{Art: "movie", Titel: "A", Inhalt: "B"}).Vollstaendig() {
		t.Error("ohne Jahr ist ein Film nicht vollständig")
	}
	it := &store.Item{Kind: "movie", Title: "alt", Overview: "alt", IMDbRating: 7}
	n.Anwenden(it, func(s string) *int { v := 16; return &v })
	if it.Title != "A" || it.Overview != "B" || it.Year != 2000 || it.AgeRating != "16" || *it.AgeYears != 16 || it.IMDbID != "tt1" {
		t.Errorf("%+v", it)
	}
	if it.SortEigen != "" {
		t.Errorf("ohne <sorttitle> kein eigener Sortiertitel: %q", it.SortEigen)
	}
	(&NFO{Sortiertitel: "Herr der Ringe 1"}).Anwenden(it, nil)
	if it.SortEigen != "Herr der Ringe 1" {
		t.Errorf("Sortiertitel aus der NFO: %q", it.SortEigen)
	}
}

func TestBilderSuchen(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"Film (2000).mkv", "Film (2000)-poster.JPG", "fanart.webp", "folder.jpg",
		"season01-poster.png", "season-specials-poster.jpg", "Film (2000)-thumb.jpg", "clearlogo.png"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	o := LiesOrdner(dir)
	b := o.BilderFuerFilm("Film (2000)", true)
	if filepath.Base(b["poster"]) != "Film (2000)-poster.JPG" || filepath.Base(b["backdrop"]) != "fanart.webp" ||
		filepath.Base(b["logo"]) != "clearlogo.png" || filepath.Base(b["landscape"]) != "Film (2000)-thumb.jpg" {
		t.Errorf("eigener Ordner: %v", b)
	}
	// Ohne eigenen Ordner gelten nur die Namen mit dem Dateinamen davor.
	b = o.BilderFuerFilm("Film (2000)", false)
	if _, ok := b["backdrop"]; ok || filepath.Base(b["poster"]) != "Film (2000)-poster.JPG" {
		t.Errorf("geteilter Ordner: %v", b)
	}
	if p := PosterFuerStaffel(o, nil, 1); filepath.Base(p) != "season01-poster.png" {
		t.Errorf("Staffel 1: %q", p)
	}
	if p := PosterFuerStaffel(o, nil, 0); filepath.Base(p) != "season-specials-poster.jpg" {
		t.Errorf("Specials: %q", p)
	}
	// Ein Verweis zählt nicht.
	_ = os.Symlink("/etc/passwd", filepath.Join(dir, "poster.jpg"))
	if p := LiesOrdner(dir).FindeBild("poster"); p != "" {
		t.Errorf("Verweis als Bild: %q", p)
	}
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
