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
)

// Befunde aus der Prüfung mit 125 Pfaden (November 2026). Jeder Fall war
// einmal falsch: Anime wurden Filme, „Trailer Park Boys" flog als Trailer
// raus, „Blade Runner 2049" bekam das Jahr 2049. Die Fälle sind relativ
// zur Bibliothek; art ist die Art der Bibliothek.
func TestBefunde(t *testing.T) {
	type fall struct {
		pfad      string
		art       string // Art der Bibliothek: series | movie
		kind      string // movie | episode
		titel     string
		jahr      int
		staffel   int
		folge     int
		bis       int
		teil      int
		absolut   bool
		datum     string
		land      string
		folgentit string
	}
	faelle := []fall{
		// 1. Anime mit durchgezählter Nummer
		{pfad: "One Piece/[SubsPlease] One Piece - 1071 (1080p).mkv", art: "series", kind: "episode", titel: "One Piece", staffel: 1, folge: 1071, absolut: true},
		{pfad: "Frieren/[SubsPlease] Frieren - 05 (1080p) [ABCD1234].mkv", art: "series", kind: "episode", titel: "Frieren", staffel: 1, folge: 5, absolut: true},
		{pfad: "Frieren/Frieren - 12v2.mkv", art: "series", kind: "episode", titel: "Frieren", staffel: 1, folge: 12, absolut: true},
		{pfad: "Naruto Shippuden/Naruto Shippuden - 001.mkv", art: "series", kind: "episode", titel: "Naruto Shippuden", staffel: 1, folge: 1, absolut: true},
		{pfad: "[Erai-raws] Frieren - 07 [1080p].mkv", art: "series", kind: "episode", titel: "Frieren", staffel: 1, folge: 7, absolut: true},
		// Mit Gruppen-Tag auch in einer Filmbibliothek eindeutig …
		{pfad: "[SubsPlease] Frieren - 05 (1080p).mkv", art: "movie", kind: "episode", titel: "Frieren", staffel: 1, folge: 5, absolut: true},
		// … ohne bleibt „Titel - Untertitel" ein Film.
		{pfad: "Terminator 2 - Tag der Abrechnung (1991).mkv", art: "movie", kind: "movie", titel: "Terminator 2: Tag der Abrechnung", jahr: 1991},
		{pfad: "Frieren - 05.mkv", art: "movie", kind: "movie", titel: "Frieren: 05"},
		// Im Staffelordner gilt weiter die Nummer in der Staffel.
		{pfad: "One Piece/Staffel 1/One Piece - 05.mkv", art: "series", kind: "episode", titel: "One Piece", staffel: 1, folge: 5},

		// 2. Tagesfolgen
		{pfad: "The Daily Show/The.Daily.Show.2024.03.15.Jon.Stewart.720p.WEB.h264.mkv", art: "series", kind: "episode", titel: "The Daily Show", staffel: 2024, folge: 315, datum: "2024-03-15", folgentit: "Jon Stewart"},
		{pfad: "Last Week Tonight/Last Week Tonight - 2024-03-15.mkv", art: "series", kind: "episode", titel: "Last Week Tonight", staffel: 2024, folge: 315, datum: "2024-03-15", folgentit: "15.03.2024"},
		{pfad: "Tagesschau/Tagesschau 2024-03-15.mkv", art: "series", kind: "episode", titel: "Tagesschau", staffel: 2024, folge: 315, datum: "2024-03-15"},
		{pfad: "Tagesschau/Tagesschau 15.03.2024.mkv", art: "series", kind: "episode", titel: "Tagesschau", staffel: 2024, folge: 315, datum: "2024-03-15"},

		// 3. Wortteile sind kein Beiwerk
		{pfad: "Trailer Park Boys/Season 1/Trailer.Park.Boys.S01E01.mkv", art: "series", kind: "episode", titel: "Trailer Park Boys", staffel: 1, folge: 1},
		{pfad: "The Sample (2020).mkv", art: "movie", kind: "movie", titel: "The Sample", jahr: 2020},
		{pfad: "The Big Short (2015).mkv", art: "movie", kind: "movie", titel: "The Big Short", jahr: 2015},

		// 4. Land gehört zur Serie
		{pfad: "The Office (US)/Season 1/The.Office.US.S01E01.mkv", art: "series", kind: "episode", titel: "The Office", staffel: 1, folge: 1, land: "US"},
		{pfad: "The Office (UK)/Series 1/The Office S01E01.mkv", art: "series", kind: "episode", titel: "The Office", staffel: 1, folge: 1, land: "GB"},
		{pfad: "The.Office.US.S02E01.720p.mkv", art: "series", kind: "episode", titel: "The Office", staffel: 2, folge: 1, land: "US"},
		{pfad: "This.Is.Us.S01E01.mkv", art: "series", kind: "episode", titel: "This Is Us", staffel: 1, folge: 1},
		{pfad: "Battlestar Galactica (1978)/Season 1/Battlestar Galactica S01E01.mkv", art: "series", kind: "episode", titel: "Battlestar Galactica", jahr: 1978, staffel: 1, folge: 1},

		// 6. „Teil 2" im Titel oder als Dateiteil
		{pfad: "Zurück in die Zukunft Teil 2 (1989).mkv", art: "movie", kind: "movie", titel: "Zurück in die Zukunft Teil 2", jahr: 1989},
		{pfad: "Dune Part 2 (2024).mkv", art: "movie", kind: "movie", titel: "Dune Part 2", jahr: 2024},
		{pfad: "Dune.Part.2.2024.German.DL.2160p.mkv", art: "movie", kind: "movie", titel: "Dune Part 2", jahr: 2024},
		{pfad: "Kill Bill Vol 2.mkv", art: "movie", kind: "movie", titel: "Kill Bill Vol 2"},
		{pfad: "Harry Potter und die Heiligtümer des Todes Teil 1 (2010).mkv", art: "movie", kind: "movie", titel: "Harry Potter und die Heiligtümer des Todes Teil 1", jahr: 2010},
		{pfad: "Der Pate.part2.mkv", art: "movie", kind: "movie", titel: "Der Pate", teil: 2},
		{pfad: "Es (1990) Teil 2.avi", art: "movie", kind: "movie", titel: "Es", jahr: 1990, teil: 2},
		{pfad: "Sommerhaus (1998) CD2.avi", art: "movie", kind: "movie", titel: "Sommerhaus", jahr: 1998, teil: 2},

		// 7. Ein Jahr aus der Zukunft ist kein Jahr
		{pfad: "Blade Runner 2049.mkv", art: "movie", kind: "movie", titel: "Blade Runner 2049"},
		{pfad: "Blade.Runner.2049.2017.German.mkv", art: "movie", kind: "movie", titel: "Blade Runner 2049", jahr: 2017},
		{pfad: "Blade Runner 2049 (2017).mkv", art: "movie", kind: "movie", titel: "Blade Runner 2049", jahr: 2017},

		// 8. Staffelordner in anderen Sprachen
		{pfad: "Doctor Who/Series 4/01 - Partners in Crime.mkv", art: "series", kind: "episode", titel: "Doctor Who", staffel: 4, folge: 1, folgentit: "Partners in Crime"},
		{pfad: "Avatar/Book 2/Avatar - 03.mkv", art: "series", kind: "episode", titel: "Avatar", staffel: 2, folge: 3},
		{pfad: "Avatar/Buch 3/01 - Der Komet.mkv", art: "series", kind: "episode", titel: "Avatar", staffel: 3, folge: 1},
		{pfad: "Haus des Geldes/Temporada 2/01 - Atraco.mkv", art: "series", kind: "episode", titel: "Haus des Geldes", staffel: 2, folge: 1},
		{pfad: "Undercover/Seizoen 3/02.mkv", art: "series", kind: "episode", titel: "Undercover", staffel: 3, folge: 2},
		{pfad: "Gomorrha/Stagione 1/Gomorrha - 04.mkv", art: "series", kind: "episode", titel: "Gomorrha", staffel: 1, folge: 4},
		{pfad: "Das Boot/Teil 2/01 - Unter Wasser.mkv", art: "series", kind: "episode", titel: "Das Boot", staffel: 2, folge: 1},
		// „Teil 1" als Ordner in einer Filmbibliothek ist keine Staffel.
		{pfad: "Harry Potter/Teil 1/Harry Potter und der Stein der Weisen (2001).mkv", art: "movie", kind: "movie", titel: "Harry Potter und der Stein der Weisen", jahr: 2001},
		// „The Jungle Book 2" ist kein Staffelordner.
		{pfad: "The Jungle Book 2 (2003)/The Jungle Book 2 (2003).mkv", art: "movie", kind: "movie", titel: "The Jungle Book 2", jahr: 2003},

		// 9. Vierstellige Folgennummer bei zweistelliger Staffel
		{pfad: "Die Simpsons/Staffel 10/Die Simpsons - 1001 - Lisa.mkv", art: "series", kind: "episode", titel: "Die Simpsons", staffel: 10, folge: 1, folgentit: "Lisa"},
		{pfad: "Die Simpsons/Staffel 12/1203.mkv", art: "series", kind: "episode", titel: "Die Simpsons", staffel: 12, folge: 3},

		// 10. BluRay und DVD: der Film heisst wie der Ordner darüber
		{pfad: "Matrix (1999)/BDMV/STREAM/00000.m2ts", art: "movie", kind: "movie", titel: "Matrix", jahr: 1999},
		{pfad: "Matrix (1999)/VIDEO_TS/VTS_01_2.VOB", art: "movie", kind: "movie", titel: "Matrix", jahr: 1999, teil: 2},

		// 11. Miniserien in Teilen
		{pfad: "Chernobyl/Chernobyl.Part.1.mkv", art: "series", kind: "episode", titel: "Chernobyl", staffel: 1, folge: 1},
		{pfad: "Planet Erde II/Planet Erde II - Teil 3.mkv", art: "series", kind: "episode", titel: "Planet Erde II", staffel: 1, folge: 3},
		{pfad: "Chernobyl/Chernobyl.Part.1.mkv", art: "movie", kind: "movie", titel: "Chernobyl Part 1"},

		// 12. „E01" ohne Staffelordner
		{pfad: "Spartacus Gods of the Arena/Spartacus.Gods.of.the.Arena.E01.mkv", art: "series", kind: "episode", titel: "Spartacus Gods of the Arena", staffel: 1, folge: 1},
		{pfad: "Spartacus Gods of the Arena/Spartacus.Gods.of.the.Arena.E01.mkv", art: "movie", kind: "movie", titel: "Spartacus Gods of the Arena E01"},

		// 13. Doppelfolgen
		{pfad: "Friends/Season 1/Friends.S01E16E17.mkv", art: "series", kind: "episode", titel: "Friends", staffel: 1, folge: 16, bis: 17},

		// 14. Titel wie Technik-Wörter
		{pfad: "Dl (2020).mkv", art: "movie", kind: "movie", titel: "Dl", jahr: 2020},
		{pfad: "Multi (2019).mkv", art: "movie", kind: "movie", titel: "Multi", jahr: 2019},
		{pfad: "Hdr (2021).mkv", art: "movie", kind: "movie", titel: "Hdr", jahr: 2021},
		{pfad: "pulp_fiction_1994_1080p.mkv", art: "movie", kind: "movie", titel: "pulp fiction", jahr: 1994},
	}
	for _, f := range faelle {
		t.Run(f.art+"/"+f.pfad, func(t *testing.T) {
			g := ParseFuer(filepath.Join("/medien", f.pfad), splitDirs(f.pfad), f.art)
			teil := f.teil
			if teil == 0 {
				teil = 1
			}
			var fehler []string
			pruefe := func(was string, ist, soll any) {
				if ist != soll {
					fehler = append(fehler, was+": "+fmtAny(ist)+" statt "+fmtAny(soll))
				}
			}
			pruefe("Art", g.Kind, f.kind)
			pruefe("Titel", g.Title, f.titel)
			pruefe("Jahr", g.Year, f.jahr)
			pruefe("Staffel", g.SeasonNo, f.staffel)
			pruefe("Folge", g.EpisodeNo, f.folge)
			pruefe("bis", g.EpisodeTo, f.bis)
			pruefe("Teil", g.PartNo, teil)
			pruefe("absolut", g.Absolut, f.absolut)
			pruefe("Datum", g.Datum, f.datum)
			pruefe("Land", g.Land, f.land)
			if f.folgentit != "" {
				pruefe("Folgentitel", g.EpisodeTitle, f.folgentit)
			}
			if len(fehler) > 0 {
				t.Error(strings.Join(fehler, "; "))
			}
		})
	}
}

// 3. Beiwerk nur nach den Mustern von Emby, Plex und Kodi.
func TestBeiwerkMuster(t *testing.T) {
	for _, p := range []string{
		"Inception (2010) - featurette.mkv", "Inception (2010)-trailer.mkv",
		"Inception (2010) - Behindthescenes.mkv", "Inception (2010)-deleted.mkv",
		"Inception (2010)-interview.mkv", "Inception (2010)-scene.mkv", "Inception (2010)-short.mkv",
		"Inception (2010)-extra.mkv", "Inception (2010).sample.mkv", "inception_trailer.mp4",
		"sample.mkv", "Trailer.mkv", "dark.s01e01-sample.mkv", ".DS_Store", "._film.mkv",
	} {
		if !IsJunkFile(p) {
			t.Errorf("%q sollte Beiwerk sein", p)
		}
	}
	for _, p := range []string{
		"Trailer Park Boys - S01E01.mkv", "The Sample (2020).mkv", "The Big Short.mkv",
		"The Extra Man (2010).mkv", "Short Circuit (1986).mkv", "Movie.2019.1080p.WEBRip.x264-RARBG.mkv",
		"Interview with the Vampire (1994).mkv", "Deleted (2019).mkv",
	} {
		if IsJunkFile(p) {
			t.Errorf("%q ist kein Beiwerk", p)
		}
	}
	for _, d := range []string{"Trailers", "trailer", "Featurette", "Featurettes", "Samples", "Sample",
		"Extras", "Extra", "Other", "Behind The Scenes", "Deleted Scenes", "Interviews", "Scenes", "Shorts", "SHORTS"} {
		if !IsExtrasDir(d) {
			t.Errorf("Ordner %q sollte Beiwerk sein", d)
		}
	}
	for _, d := range []string{"Trailer Park Boys", "The Sample (2020)", "Extraction (2020)"} {
		if IsExtrasDir(d) {
			t.Errorf("Ordner %q ist kein Beiwerk", d)
		}
	}
}

// 5. Ordner des NAS und des Betriebssystems.
func TestSystemOrdner(t *testing.T) {
	for _, d := range []string{"#recycle", "@eaDir", "#snapshot", "$RECYCLE.BIN", "System Volume Information",
		"lost+found", ".@__thumb", "@Recycle", "@Recently-Snapshot", ".snapshot"} {
		if !IstSystemOrdner(d) || !ordnerUeberspringen(d, "movie") || !ordnerUeberspringen(d, "series") {
			t.Errorf("%q sollte übersprungen werden", d)
		}
	}
	for _, d := range []string{"#Alive (2020)", "Recycle", "Snapshot (2023)", "Filme"} {
		if IstSystemOrdner(d) {
			t.Errorf("%q ist kein Systemordner", d)
		}
	}
	if !IstSystemPfad("Filme/#recycle/Matrix (1999)/Matrix.mkv") || IstSystemPfad("Filme/Matrix (1999)/Matrix.mkv") {
		t.Error("IstSystemPfad")
	}
}

// 14. Kleinschreibung aus Dateinamen wird zur Anzeige gross geschrieben.
func TestHuebsch(t *testing.T) {
	for ein, aus := range map[string]string{
		"pulp fiction":           "Pulp Fiction",
		"der schuh des manitu":   "Der Schuh des Manitu",
		"the lord of the rings":  "The Lord of the Rings",
		"iCarly":                 "iCarly",
		"Die Bucht":              "Die Bucht",
		"1917":                   "1917",
		"über den dächern nizza": "Über den Dächern Nizza",
	} {
		if got := huebsch(ein); got != aus {
			t.Errorf("huebsch(%q) = %q, erwartet %q", ein, got, aus)
		}
	}
}

// 17. Untertitel daneben: Endungen und Sprache aus dem Namen.
func TestUntertitelDaneben(t *testing.T) {
	gibt := func(n string) bool { return n == "Film.idx" }
	cases := []struct {
		name          string
		ok            bool
		lang          string
		forced, hoerg bool
	}{
		{"Film.de.forced.srt", true, "de", true, false},
		{"Film.ger.srt", true, "de", false, false},
		{"Film.German.srt", true, "de", false, false},
		{"Film.en.sdh.ass", true, "en", false, true},
		{"Film.French.ssa", true, "fr", false, false},
		{"Film.spa.vtt", true, "es", false, false},
		{"Film.srt", true, "und", false, false},
		// .sub mit .idx ist VobSub (Bilder) — lässt sich nicht zeigen.
		{"Film.sub", false, "", false, false},
		{"Film.nl.sub", true, "nl", false, false},
		{"Film.idx", false, "", false, false},
		{"Film 2.srt", true, "und", false, false},
		{"Filmabend.srt", false, "", false, false},
		{"Andere.de.srt", false, "", false, false},
	}
	for _, c := range cases {
		tr, ok := UntertitelDaneben(c.name, "Film", gibt)
		if ok != c.ok {
			t.Errorf("%s: ok=%v", c.name, ok)
			continue
		}
		if ok && (tr.Language != c.lang || tr.Forced != c.forced || tr.HearingImpaired != c.hoerg) {
			t.Errorf("%s: %+v", c.name, tr)
		}
	}
}
