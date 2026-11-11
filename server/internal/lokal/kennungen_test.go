// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package lokal

import "testing"

func TestKennungenAusName(t *testing.T) {
	faelle := []struct {
		name string
		soll Kennungen
	}{
		{"The Matrix (1999) {imdb-tt0133093}", Kennungen{IMDb: "tt0133093"}},
		{"The Matrix (1999) [imdbid-tt0133093]", Kennungen{IMDb: "tt0133093"}},
		{"The Matrix (1999) {IMDB-TT0133093}", Kennungen{IMDb: "tt0133093"}},
		{"The Matrix (1999) {tmdb-603}", Kennungen{TMDb: "603"}},
		{"The Matrix (1999) [tmdbid-603]", Kennungen{TMDb: "603"}},
		{"The Matrix (1999) [tmdbid=603]", Kennungen{TMDb: "603"}},
		{"Breaking Bad (2008) {tvdb-81189}", Kennungen{TVDb: "81189"}},
		{"Breaking Bad [tvdbid-81189]", Kennungen{TVDb: "81189"}},
		{"Matrix.1999.tt0133093.German.DL.1080p.BluRay.x264-GRP", Kennungen{IMDb: "tt0133093"}},
		{"Matrix tt0133093", Kennungen{IMDb: "tt0133093"}},
		{"Der Herr der Ringe (2001) {tmdb-120} {imdb-tt0120737}", Kennungen{TMDb: "120", IMDb: "tt0120737"}},
		// Keine Kennungen:
		{"Matrix (1999)", Kennungen{}},
		{"Watt1234567", Kennungen{}},
		{"tt12", Kennungen{}},
		{"Ein Film über tmdb (2020)", Kennungen{}},
		{"Ocean's 11 [1080p]", Kennungen{}},
	}
	for _, f := range faelle {
		if k := KennungenAusName(f.name); k != f.soll {
			t.Errorf("%q: %+v, erwartet %+v", f.name, k, f.soll)
		}
	}
}

func TestOhneKennungen(t *testing.T) {
	faelle := map[string]string{
		"The Matrix (1999) {imdb-tt0133093}": "The Matrix (1999)  ",
		"Matrix.1999.tt0133093.German":       "Matrix.1999. .German",
		"Breaking Bad [tvdbid-81189] (2008)": "Breaking Bad   (2008)",
		"Matrix (1999)":                      "Matrix (1999)",
	}
	for ein, soll := range faelle {
		if got := OhneKennungen(ein); got != soll {
			t.Errorf("%q: %q, erwartet %q", ein, got, soll)
		}
	}
}

func TestKennungenFilmUndSerie(t *testing.T) {
	// Der Dateiname zuerst, dann sein Ordner, nicht weiter hinauf.
	if k := KennungenFilm("/v/Filme/Matrix (1999) {tmdb-603}/Matrix.mkv", []string{"Matrix (1999) {tmdb-603}"}); k.TMDb != "603" {
		t.Errorf("Ordner: %+v", k)
	}
	if k := KennungenFilm("/v/Filme/Sammlung/Matrix {imdb-tt0133093}.mkv", []string{"Sammlung {tmdb-1}"}); k.IMDb != "tt0133093" || k.TMDb != "" {
		t.Errorf("Datei vor Ordner: %+v", k)
	}
	if k := KennungenFilm("/v/Filme/Reihe {tmdb-1}/Teil 1/film.mkv", []string{"Reihe {tmdb-1}", "Teil 1"}); !k.Leer() {
		t.Errorf("zwei Ordner hinauf: %+v", k)
	}
	// BluRay: der Ordner über BDMV.
	if k := KennungenFilm("/v/Filme/Matrix [tmdbid-603]/BDMV/STREAM/00001.m2ts", []string{"Matrix [tmdbid-603]", "BDMV", "STREAM"}); k.TMDb != "603" {
		t.Errorf("BluRay: %+v", k)
	}
	// Serie: nur Ordner, nie die Folge.
	if k := KennungenSerie([]string{"Breaking Bad (2008) {tvdb-81189}", "Season 01"}); k.TVDb != "81189" {
		t.Errorf("Serie: %+v", k)
	}
	if k := KennungenSerie([]string{"Breaking Bad", "Season 01"}); !k.Leer() {
		t.Errorf("Serie ohne: %+v", k)
	}
}
