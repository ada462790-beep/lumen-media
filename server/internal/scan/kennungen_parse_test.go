// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package scan

import (
	"path/filepath"
	"strings"
	"testing"

	"lumen/internal/lokal"
)

// Kennungen im Namen fallen aus dem Titel heraus — und lassen sich für die
// Zuordnung lesen. Die Namen stammen so aus Sammlungen, die Radarr,
// Sonarr, Jellyfin, Emby oder ein Mensch von Hand angelegt haben.
func TestKennungenImNamen(t *testing.T) {
	faelle := []struct {
		pfad  string // unter /volume1/video
		art   string
		titel string
		jahr  int
		k     lokal.Kennungen
	}{
		{"_filme/The Matrix (1999) {imdb-tt0133093}/The Matrix (1999) {imdb-tt0133093}.mkv", "movie", "The Matrix", 1999, lokal.Kennungen{IMDb: "tt0133093"}},
		{"_filme/Matrix (1999) [imdbid-tt0133093]/Matrix (1999).mkv", "movie", "Matrix", 1999, lokal.Kennungen{IMDb: "tt0133093"}},
		{"_filme/Matrix (1999) [tmdbid-603]/Matrix (1999) [tmdbid-603] - 1080p.mkv", "movie", "Matrix", 1999, lokal.Kennungen{TMDb: "603"}},
		{"_filme/Matrix.1999.tt0133093.German.DL.1080p.BluRay.x264-GRP.mkv", "movie", "Matrix", 1999, lokal.Kennungen{IMDb: "tt0133093"}},
		{"_filme/Matrix tt0133093.mkv", "movie", "Matrix", 0, lokal.Kennungen{IMDb: "tt0133093"}},
		{"_filme/Der Herr der Ringe - Die Gefährten (2001) {tmdb-120} Extended.mkv", "movie", "Der Herr der Ringe: Die Gefährten", 2001, lokal.Kennungen{TMDb: "120"}},
		{"_filme/Ziemlich beste Freunde (2011) {tmdb-77338}/Ziemlich beste Freunde.avi", "movie", "Ziemlich beste Freunde", 2011, lokal.Kennungen{TMDb: "77338"}},
		{"_filme/Heidi (2015) {imdb-tt3700392}/BDMV/STREAM/00001.m2ts", "movie", "Heidi", 2015, lokal.Kennungen{IMDb: "tt3700392"}},
		// Ohne Kennung bleibt alles wie bisher.
		{"_filme/Die Schweizermacher (1978)/Die Schweizermacher (1978).mkv", "movie", "Die Schweizermacher", 1978, lokal.Kennungen{}},
		{"_filme/Mein Name ist Eugen (2005).mkv", "movie", "Mein Name ist Eugen", 2005, lokal.Kennungen{}},
	}
	wurzel := "/volume1/video"
	for _, f := range faelle {
		pfad := filepath.Join(wurzel, f.pfad)
		dirs := splitDirs(strings.TrimPrefix(f.pfad, ""))
		dirs = dirs[1:] // ohne „_filme": die Bibliothek
		g := ParseFuer(pfad, dirs, f.art)
		if g.Title != f.titel || g.Year != f.jahr {
			t.Errorf("%s: %q (%d), erwartet %q (%d)", f.pfad, g.Title, g.Year, f.titel, f.jahr)
		}
		if strings.Contains(strings.ToLower(g.Title), "tt0") || strings.ContainsAny(g.Title, "{}[]") {
			t.Errorf("%s: Kennung im Titel: %q", f.pfad, g.Title)
		}
		if k := lokal.KennungenFilm(pfad, dirs); k != f.k {
			t.Errorf("%s: Kennungen %+v, erwartet %+v", f.pfad, k, f.k)
		}
	}

	// Serien: Titel ohne Kennung, die Kennung kommt aus dem Serienordner.
	serien := []struct {
		pfad  string
		titel string
		k     lokal.Kennungen
	}{
		{"_serien/Breaking Bad (2008) {tvdb-81189}/Season 01/Breaking Bad - S01E01 - Pilot.mkv", "Breaking Bad", lokal.Kennungen{TVDb: "81189"}},
		{"_serien/Breaking Bad [tvdbid-81189]/Staffel 1/S01E01.mkv", "Breaking Bad", lokal.Kennungen{TVDb: "81189"}},
		{"_serien/Der Bestatter (2013) {imdb-tt2724532}/Staffel 02/Der Bestatter - S02E03.mkv", "Der Bestatter", lokal.Kennungen{IMDb: "tt2724532"}},
		{"_serien/Tschugger [tmdbid-130392]/Season 1/Tschugger S01E01 {imdb-tt15346566}.mkv", "Tschugger", lokal.Kennungen{TMDb: "130392"}},
	}
	for _, f := range serien {
		pfad := filepath.Join(wurzel, f.pfad)
		dirs := splitDirs(f.pfad)[1:]
		g := ParseFuer(pfad, dirs, "series")
		if g.Kind != "episode" || g.Title != f.titel {
			t.Errorf("%s: %s %q, erwartet Folge von %q", f.pfad, g.Kind, g.Title, f.titel)
		}
		if k := lokal.KennungenSerie(dirs); k != f.k {
			t.Errorf("%s: Kennungen %+v, erwartet %+v", f.pfad, k, f.k)
		}
	}
}
