// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

// lumen-nachbau startet nachgebaute Gegenstellen für Oberflächentests:
// OpenSubtitles und einen Jellyfin-Server. Nur für Tests — es gehört in
// kein ausgeliefertes Abbild. Die Adressen schreibt es in die Datei, die
// als erstes Argument kommt (OS=… und EMBY=…); Lumen bekommt die
// OpenSubtitles-Adresse über LUMEN_OPENSUBTITLES_URL.
//
//	go run ./cmd/lumen-nachbau /tmp/nachbau.env
//
// Siehe tools/oberflaeche_funktionen.py.
package main

import (
	"fmt"
	"os"

	emby "lumen/internal/uebernahme/nachbau"
	opensub "lumen/internal/untertitel/nachbau"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Aufruf: lumen-nachbau <datei für die Adressen>")
		os.Exit(2)
	}
	o := opensub.Neu()
	o.Rest = 20
	srt := "1\n00:00:01,000 --> 00:00:04,000\nGuten Morgen, Sonne.\n\n2\n00:00:05,000 --> 00:00:09,000\nZweite Zeile.\n"
	o.Untertitel = []opensub.Untertitel{
		{DateiID: 101, Sprache: "de", Name: "Sonne", Downloads: 1234, Inhalt: srt},
		{DateiID: 102, Sprache: "de", Name: "Sonne", Downloads: 99, Maschinell: true, Inhalt: srt},
		{DateiID: 103, Sprache: "en", Name: "Sonne", Downloads: 50, Hoergeschaedigt: true, Inhalt: srt},
	}
	e := emby.Neu("Jellyfin")
	gesehen := map[string]any{"Played": true, "LastPlayedDate": "2025-01-01T20:00:00Z"}
	e.Titel = []emby.Titel{
		emby.Film("e1", "American Psycho", 2000, "/media/American Psycho (2000).mkv", 1, nil, gesehen),
		emby.Film("e2", "Sonne", 2020, "/media/Sonne (2020).mkv", 1, nil, map[string]any{"PlaybackPositionTicks": 300_000_000, "IsFavorite": true}),
		emby.Film("e3", "Apocalypse Now", 1979, "/media/x.avi", 1, nil, map[string]any{"Rating": 10.0}),
		emby.Film("e4", "Gibt es nicht", 1990, "/media/y.mkv", 1, nil, gesehen),
	}
	// Alle Filme der Mediathek mit ihren Kennungen — für „Zuordnungen
	// übernehmen". Die Pfade sehen aus wie im Docker-Behälter von Emby.
	e.Alle = []emby.Titel{
		emby.Film("a1", "Meridian", 2016, "/media/filme/Meridian/Meridian.mkv", 0,
			map[string]string{"Tmdb": "502"}, map[string]any{}),
		emby.Film("a2", "Der Fund", 2003, "/media/filme/Der Fund (2003)/Der Fund (2003).mkv", 0,
			map[string]string{"Imdb": "tt0120737"}, map[string]any{}),
		emby.Film("a3", "Matrix", 1999, "/media/filme/Matrix (1999)/Matrix (1999).mkv", 0,
			map[string]string{"Tmdb": "603", "Imdb": "tt0133093"}, map[string]any{}),
		emby.Film("a4", "Gibt es nicht", 1990, "/media/filme/y.mkv", 0,
			map[string]string{"Tmdb": "1"}, map[string]any{}),
	}
	if err := os.WriteFile(os.Args[1], []byte(fmt.Sprintf("OS=%s\nEMBY=%s\n", o.Adresse(), e.Adresse())), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	select {}
}
