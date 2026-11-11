// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package uebernahme

import (
	"context"
	"testing"

	"lumen/internal/uebernahme/nachbau"
)

// Lumen auf dem NAS unter /volume1/video, Emby im Docker-Behälter unter
// /media — oder unter Windows mit Laufwerksbuchstaben.
func lumenOrte() []LumenOrt {
	return []LumenOrt{
		{ItemID: "matrix", Kind: "movie", Pfade: []string{"/volume1/video/_filme/Matrix (1999)/Matrix (1999).mkv"}, Groessen: []int64{1000}},
		{ItemID: "heidi", Kind: "movie", Pfade: []string{"/volume1/video/_filme/Heidi (2015)/BDMV/STREAM/00001.m2ts"}},
		// Zwei Filme mit demselben Dateinamen — nur die Grösse trennt.
		{ItemID: "film-a", Kind: "movie", Pfade: []string{"/volume1/video/_filme/A/movie.mkv"}, Groessen: []int64{111}},
		{ItemID: "film-b", Kind: "movie", Pfade: []string{"/volume1/video/_filme/B/movie.mkv"}, Groessen: []int64{222}},
		// Zwei gleich benannte Dateien ohne bekannte Grösse: mehrdeutig.
		{ItemID: "dopp-1", Kind: "movie", Pfade: []string{"/volume1/video/_filme/X1/film.avi"}},
		{ItemID: "dopp-2", Kind: "movie", Pfade: []string{"/volume1/video/_filme/X2/film.avi"}},
		{ItemID: "bb", Kind: "series", Pfade: []string{
			"/volume1/video/_serien/Breaking Bad (2008)/Season 01/Breaking Bad - S01E01.mkv",
			"/volume1/video/_serien/Breaking Bad (2008)/Season 02/Breaking Bad - S02E01.mkv"}},
		{ItemID: "bestatter", Kind: "series", Pfade: []string{
			"/volume1/video/_serien/Der Bestatter/Staffel 1/Der Bestatter S01E01.mkv"}},
	}
}

func TestZuordnungenPlanen(t *testing.T) {
	emby := []Eintrag{
		{Typ: "Movie", Name: "Matrix", Pfad: "/media/_filme/Matrix (1999)/Matrix (1999).mkv", Groesse: 1000,
			Kennungen: map[string]string{"tmdb": "603", "imdb": "tt0133093"}},
		// Anderer Name des Bibliotheksordners: zwei Teile genügen.
		{Typ: "Movie", Name: "Heidi", Pfad: `D:\Filme\Heidi (2015)`, Kennungen: map[string]string{"imdb": "tt3700392"}},
		{Typ: "Movie", Name: "B", Pfad: "/media/irgendwo/B2/movie.mkv", Groesse: 222, Kennungen: map[string]string{"tmdb": "2"}},
		{Typ: "Movie", Name: "Doppelt", Pfad: "/media/Y/film.avi", Kennungen: map[string]string{"tmdb": "3"}},
		{Typ: "Movie", Name: "Fehlt", Jahr: 2010, Pfad: "/media/_filme/Fehlt (2010)/Fehlt.mkv", Kennungen: map[string]string{"tmdb": "4"}},
		{Typ: "Movie", Name: "Ohne", Pfad: "/media/_filme/Matrix (1999)/Matrix (1999).mkv", Kennungen: map[string]string{}},
		{Typ: "Series", Name: "Breaking Bad", Pfad: "/media/serien/Breaking Bad (2008)",
			Kennungen: map[string]string{"tvdb": "81189", "tmdb": "1396", "imdb": "tt0903747"}},
		{Typ: "Series", Name: "Der Bestatter", Pfad: "/mnt/tv/Der Bestatter", Kennungen: map[string]string{"tvdb": "268270"}},
		// Folgen zählen hier nicht.
		{Typ: "Episode", Name: "Pilot", Pfad: "/media/serien/Breaking Bad (2008)/Season 01/Breaking Bad - S01E01.mkv"},
		// Unsinnige Kennungen werden nicht übernommen.
		{Typ: "Movie", Name: "Kaputt", Pfad: "/media/_filme/Matrix (1999)/Matrix (1999).mkv",
			Kennungen: map[string]string{"tmdb": "../account", "imdb": "nm123"}},
	}
	plan := ZuordnungenPlanen(emby, lumenOrte())
	je := map[string]Zuordnung{}
	for _, z := range plan.Zuordnungen {
		je[z.ItemID] = z
	}
	if z := je["matrix"]; z.TMDb != "603" || z.IMDb != "tt0133093" || z.Kind != "movie" {
		t.Errorf("matrix: %+v", z)
	}
	if z := je["heidi"]; z.IMDb != "tt3700392" {
		t.Errorf("heidi (BluRay-Ordner, Windows-Pfad): %+v", z)
	}
	if z := je["film-b"]; z.TMDb != "2" {
		t.Errorf("film-b (über die Grösse): %+v", z)
	}
	if _, da := je["film-a"]; da {
		t.Error("film-a fälschlich zugeordnet")
	}
	if _, da := je["dopp-1"]; da {
		t.Error("mehrdeutig, trotzdem zugeordnet")
	}
	if z := je["bb"]; z.TVDb != "81189" || z.TMDb != "1396" || z.Kind != "series" {
		t.Errorf("Breaking Bad: %+v", z)
	}
	if z := je["bestatter"]; z.TVDb != "268270" {
		t.Errorf("Der Bestatter (nur der Ordnername): %+v", z)
	}
	if len(plan.Zuordnungen) != 5 {
		t.Errorf("%d Zuordnungen: %+v", len(plan.Zuordnungen), plan.Zuordnungen)
	}
	// Doppelt (mehrdeutig) und Fehlt; „Kaputt" hat nach dem Prüfen keine
	// Kennung mehr, „Ohne" hatte keine.
	if plan.NichtGefunden != 2 || plan.OhneKennung != 2 {
		t.Errorf("nicht gefunden %d, ohne Kennung %d", plan.NichtGefunden, plan.OhneKennung)
	}
	if len(plan.Fehlend) != 2 || plan.Fehlend[1] != "Fehlt (2010)" {
		t.Errorf("fehlend %q", plan.Fehlend)
	}
}

// Über den Nachbau: Alle Filme und Serien kommen mit Pfad und Kennungen.
func TestFilmeUndSerienHolen(t *testing.T) {
	for _, art := range []string{"Emby", "Jellyfin"} {
		n := nachbau.Neu(art)
		serie := nachbau.Serie("s1", "Breaking Bad", 2008, map[string]string{"Tvdb": "81189", "Tmdb": "1396"}, nil)
		serie["Path"] = "/media/serien/Breaking Bad (2008)"
		n.Alle = []nachbau.Titel{
			nachbau.Film("e1", "Matrix", 1999, "/media/_filme/Matrix (1999)/Matrix (1999).mkv", 1000,
				map[string]string{"Tmdb": "603", "Imdb": "tt0133093"}, map[string]any{}),
			serie,
		}
		n.SeitenGross = 1
		sv, err := Verbinden(n.Adresse(), n.Client())
		if err != nil {
			t.Fatal(err)
		}
		ctx := context.Background()
		if _, err := sv.FilmeUndSerien(ctx, nil); err != ErrAnmeldung {
			t.Fatalf("%s: ohne Anmeldung: %v", art, err)
		}
		if err := sv.Anmelden(ctx, "alex", "geheim"); err != nil {
			t.Fatal(err)
		}
		liste, err := sv.FilmeUndSerien(ctx, nil)
		if err != nil || len(liste) != 2 {
			t.Fatalf("%s: %v %+v", art, err, liste)
		}
		plan := ZuordnungenPlanen(liste, lumenOrte())
		if len(plan.Zuordnungen) != 2 {
			t.Errorf("%s: %+v", art, plan)
		}
		n.Close()
	}
}
