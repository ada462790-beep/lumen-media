// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package scan

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lumen/internal/store"
)

// Pfade, wie sie in deutschen Sammlungen tatsächlich herumliegen: Release-
// Ordner pro Folge, Staffel-Packs, Tippfehler im Staffelordner, Specials
// mit Seriennamen davor, Doppelfolgen in allen Schreibweisen, Filme mit
// dem Jahr vorne, hinten oder gar nicht. Jeder Pfad ist relativ zur
// Bibliothek — genau so, wie der Scanner ihn an Parse gibt.
//
// Wo die Erwartung strittig ist, steht dabei, wie Emby oder Plex es halten.
func TestWildnis(t *testing.T) {
	type fall struct {
		pfad    string
		art     string // movie | episode | skip (Beiwerk, wird gar nicht eingelesen)
		titel   string
		jahr    int
		staffel int
		folge   int
		bis     int // Doppelfolge: letzte Folge
		teil    int // 0 heisst Teil 1
	}
	ep := func(pfad, titel string, jahr, staffel, folge int) fall {
		return fall{pfad: pfad, art: "episode", titel: titel, jahr: jahr, staffel: staffel, folge: folge}
	}
	doppel := func(pfad, titel string, staffel, folge, bis int) fall {
		return fall{pfad: pfad, art: "episode", titel: titel, staffel: staffel, folge: folge, bis: bis}
	}
	film := func(pfad, titel string, jahr int) fall {
		return fall{pfad: pfad, art: "movie", titel: titel, jahr: jahr}
	}
	teil := func(pfad, titel string, jahr, n int) fall {
		return fall{pfad: pfad, art: "movie", titel: titel, jahr: jahr, teil: n}
	}
	weg := func(pfad string) fall { return fall{pfad: pfad, art: "skip"} }

	faelle := []fall{
		// ---------------------------------------- aus der Sammlung des Besitzers
		// Release-Ordner pro Folge, in einem Release-Ordner für mehrere Folgen.
		// Der Serientitel ist, was vor „S07E19" steht — nie der ganze Ordner.
		ep("FBI S07E13 - E15 GERMAN 720P WEB H264 - Saint/FBI S07E19 720p - Cosmo71/fbi.s07e19.german.720p.mkv", "FBI", 0, 7, 19),
		doppel("FBI S07E13 - E15 GERMAN 720P WEB H264 - Saint/fbi.s07e13-e15.german.720p.web.h264-saint.mkv", "FBI", 7, 13, 15),
		ep("The Great Game/The Great Game S01E05 - S01E06 (German DL) - Cosmo/The.Great.Game.S01E05.mkv", "The Great Game", 0, 1, 5),
		ep("The Great Game/The Great Game S01E05 - S01E06 (German DL) - Cosmo/The.Great.Game.S01E06.mkv", "The Great Game", 0, 1, 6),
		ep("Incorporated S01E07 German DL 1080p HDTV x264-AIDA/incorporated.s01e07.german.dl.1080p.hdtv.x264-aida.mkv", "Incorporated", 0, 1, 7),
		ep("Incorporated.S01E08.German.DL.1080p.HDTV.x264-AIDA/incorporated.s01e08.mkv", "Incorporated", 0, 1, 8),
		// „Stafel" mit einem f — der Tippfehler darf keine eigene Serie machen.
		ep("His Dark Materials/His Dark Materials - Stafel 03/His Dark Materials - S03E01.mkv", "His Dark Materials", 0, 3, 1),
		// „Doctor Who - Specials" ist Staffel 0 von Doctor Who, wie bei Emby
		// und Plex ein nackter „Specials"-Ordner.
		ep("Doctor Who/Doctor Who - Specials/Doctor Who - S00E39 - Das Monster von den Sternen.mkv", "Doctor Who", 0, 0, 39),
		ep("Arrow/Arrow - Staffel 01/Arrow - S01E01 - .mkv", "Arrow", 0, 1, 1),
		ep("Fallout (2024)/Fallout (2024) - Staffel 01/Fallout (2024) - S01E01.mkv", "Fallout", 2024, 1, 1),
		ep("Avatar - Herr der Elemente/Staffel 01/Avatar - S01E01.avi", "Avatar: Herr der Elemente", 0, 1, 1),

		// ---------------------------------------- Staffelordner aller Art
		ep("Breaking Bad/Season 2/Breaking.Bad.S02E03.720p.BluRay.x264-DEMAND.mkv", "Breaking Bad", 0, 2, 3),
		ep("Breaking Bad/Breaking.Bad.S05.German.DL.1080p.BluRay.x264-RSG/breaking.bad.s05e16.german.dl.1080p.bluray.x264-rsg.mkv", "Breaking Bad", 0, 5, 16),
		// Staffel-Pack direkt in der Bibliothek: der Name steht vor „S01".
		ep("Dark.S01.German.DL.1080p.WEB.x264-GRP/Dark.S01E01.German.DL.1080p.WEB.x264-GRP.mkv", "Dark", 0, 1, 1),
		ep("Dark/Staffel 1 (German DL 1080p)/Dark - S01E02.mkv", "Dark", 0, 1, 2),
		ep("Game of Thrones/S08/Game.of.Thrones.S08E06.mkv", "Game of Thrones", 0, 8, 6),
		ep("How I Met Your Mother/Staffel_02/HIMYM_S02E01.avi", "How I Met Your Mother", 0, 2, 1),
		ep("Akte X/Akte X - Season 5/Akte.X.S05E01.Rotes.Gras.German.DVDRip.avi", "Akte X", 0, 5, 1),
		// Kürzel vor der Staffel: der Serienordner darüber gilt.
		ep("Star Trek - Deep Space Nine/DS9 Staffel 4/DS9 - 4x01 - Der Weg des Kriegers.avi", "Star Trek: Deep Space Nine", 0, 4, 1),
		ep("Die Simpsons/Staffel 10/Die Simpsons - S10E05.mkv", "Die Simpsons", 0, 10, 5),
		// „Saison" — so heissen Staffeln in manchen Sammlungen (fr. Releases).
		ep("Futurama/Saison 1/Futurama 1x01.avi", "Futurama", 0, 1, 1),
		ep("Dexter/Season 1 Disc 2/Dexter S01E07.avi", "Dexter", 0, 1, 7),
		ep("Pastewka/Staffel 10/Pastewka.S10E01.German.1080p.WEB.h264-WvF.mkv", "Pastewka", 0, 10, 1),
		ep("Haus des Geldes/Staffel 2/Haus.des.Geldes.S02E03.German.DL.1080p.NF.WEB.x264.mkv", "Haus des Geldes", 0, 2, 3),
		// Der Serienordner zählt mehr als die Schreibweise im Dateinamen.
		ep("Greys Anatomy/Staffel 6/Grey's Anatomy S06E01.mkv", "Greys Anatomy", 0, 6, 1),
		ep("One Piece/Staffel 1/One Piece - S01E101.mkv", "One Piece", 0, 1, 101),

		// ---------------------------------------- Folge ohne S01E01
		ep("The Mandalorian/Staffel 1/E03.mkv", "The Mandalorian", 0, 1, 3),
		ep("Lost/Staffel 3/Lost - 305 - Der Pakt.avi", "Lost", 0, 3, 5),
		ep("Lost/Staffel 3/03 - Weitere Zwillinge.avi", "Lost", 0, 3, 3),
		ep("Friends/Season 01/Friends - 1x01 - Der Pilot.avi", "Friends", 0, 1, 1),
		ep("Babylon 5/Season 01/Babylon 5 - 1x01 - Midnight on the Firing Line.avi", "Babylon 5", 0, 1, 1),
		// Keine Staffel, aber „Folge 5": Emby legt solche Dateien als
		// Staffel 1 ab, wenn sie in einem Serienordner liegen.
		ep("Die Kinder vom Alstertal/Die Kinder vom Alstertal - Folge 05.avi", "Die Kinder vom Alstertal", 0, 1, 5),

		// ---------------------------------------- Doppelfolgen
		doppel("Stargate SG-1/Staffel 01/Stargate SG-1 - S01E01E02 - Das Tor zum Universum.avi", "Stargate SG-1", 1, 1, 2),
		doppel("The Walking Dead/Staffel 01/The.Walking.Dead.S01E01-E02.German.mkv", "The Walking Dead", 1, 1, 2),
		doppel("The Walking Dead/Staffel 01/The Walking Dead S01E03-04.mkv", "The Walking Dead", 1, 3, 4),
		doppel("Friends/Season 01/Friends - 1x16-1x17 - Der Zwilling.avi", "Friends", 1, 16, 17),
		doppel("Lost/Staffel 1/Lost.S01E01-S01E02.Pilot.German.mkv", "Lost", 1, 1, 2),

		// ---------------------------------------- Specials
		ep("Sherlock/Specials/Sherlock - S00E01 - Die Braut des Grauens.mkv", "Sherlock", 0, 0, 1),
		ep("Sherlock/Season 00/Sherlock.S00E02.mkv", "Sherlock", 0, 0, 2),

		// ---------------------------------------- Titel mit Zahlen und Punkten
		ep("9-1-1/Staffel 3/9-1-1 - S03E01 - Erdbeben.mkv", "9-1-1", 0, 3, 1),
		ep("24/Staffel 1/24 - S01E01 - 00.00 Uhr - 01.00 Uhr.avi", "24", 0, 1, 1),
		ep("24/Season 2/24.S02E01.German.DVDRip.XviD.avi", "24", 0, 2, 1),
		// „1883" ist der Titel, kein Jahr — Emby und Plex sehen das genauso,
		// weil der Ordner nur aus der Zahl besteht.
		ep("1883/1883.S01E01.German.DL.1080p.WEB.mkv", "1883", 0, 1, 1),
		ep("Yellowstone (2018)/Staffel 1/Yellowstone.2018.S01E01.mkv", "Yellowstone", 2018, 1, 1),
		ep("Doctor Who (2005)/Season 4/Doctor.Who.2005.S04E01.German.mkv", "Doctor Who", 2005, 4, 1),
		ep("Agents of S.H.I.E.L.D/Season 1/Marvels.Agents.of.S.H.I.E.L.D.S01E01.German.DL.mkv", "Agents of S.H.I.E.L.D", 0, 1, 1),
		ep("Marvels.Agents.of.S.H.I.E.L.D.S05E01.German.DL.720p.WEB.x264-GRP/marvels.agents.of.s.h.i.e.l.d.s05e01.mkv", "Marvels Agents of S.H.I.E.L.D.", 0, 5, 1),
		ep("Mr. Robot/Season 1/mr.robot.s01e01.720p.mkv", "Mr. Robot", 0, 1, 1),
		ep("S.W.A.T. (2017)/Staffel 1/S.W.A.T. - S01E01 - Pilot.mkv", "S.W.A.T.", 2017, 1, 1),
		// Strittig: Das Jahr im Dateinamen gehört hier zur Serie, der Ordner
		// nennt keins. Emby nimmt Titel und Jahr vom Serienordner — also
		// kein Jahr; TMDb findet „Twin Peaks" trotzdem.
		ep("Twin Peaks/Season 3/Twin.Peaks.2017.S03E01.mkv", "Twin Peaks", 0, 3, 1),

		// ---------------------------------------- Miniserien
		ep("Chernobyl/Chernobyl.S01E01.German.DL.mkv", "Chernobyl", 0, 1, 1),
		// Nur eine Nummer: Parse sagt „Folge"; ob es bei zwei Dateien doch
		// ein Film in Teilen ist, entscheidet der Scanner.
		ep("Der Tunnel/1.mkv", "Der Tunnel", 0, 1, 1),
		ep("Das Boot (1985)/Teil 3.avi", "Das Boot", 1985, 1, 3),

		// ---------------------------------------- Beiwerk
		weg("The Expanse/Staffel 1/Featurettes/Making of.mkv"),
		weg("Breaking Bad/Extras/Gag Reel.mkv"),
		weg("Breaking Bad/Behind The Scenes/Interview Cranston.mkv"),
		weg("Dark/Staffel 1/dark.s01e01-sample.mkv"),

		// ---------------------------------------- Filme
		film("Star Trek/(1982) Star Trek II - Der Zorn des Khan.avi", "Star Trek II: Der Zorn des Khan", 1982),
		film("Rambo/(1982) Rambo.avi", "Rambo", 1982),
		teil("Apocalypse Now (1979) a.avi", "Apocalypse Now", 1979, 1),
		teil("Apocalypse Now (1979) b.avi", "Apocalypse Now", 1979, 2),
		teil("Apocalypse Now (1979) c.avi", "Apocalypse Now", 1979, 3),
		teil("apocalypse.now.1979b.avi", "apocalypse now", 1979, 2),
		// Zwei Fassungen desselben Films: gleicher Titel, gleiches Jahr.
		film("American Psycho (2000).mkv", "American Psycho", 2000),
		film("American Psycho (2000).avi", "American Psycho", 2000),
		film("The Dark Knight (2008) 720p.mkv", "The Dark Knight", 2008),
		film("The Dark Knight (2008) 1080p.mkv", "The Dark Knight", 2008),
		teil("Matrix (1999) CD1.avi", "Matrix", 1999, 1),
		teil("Matrix (1999) CD2.avi", "Matrix", 1999, 2),
		teil("Es (1990) Teil 1.avi", "Es", 1990, 1),
		teil("Es (1990) Teil 2.avi", "Es", 1990, 2),
		film("Der Schuh des Manitu.avi", "Der Schuh des Manitu", 0),
		// Zahl als Titel und Jahr dahinter: Wie Emby gilt die letzte
		// Jahreszahl als Jahr, alles davor ist Titel.
		film("1917.2019.German.DL.1080p.BluRay.x264-GRP.mkv", "1917", 2019),
		film("Blade.Runner.2049.2017.German.DL.2160p.UHD.BluRay.x265-GRP.mkv", "Blade Runner 2049", 2017),
		film("2001 - Odyssee im Weltraum (1968).mkv", "2001: Odyssee im Weltraum", 1968),
		film("Blade Runner (1982) Director's Cut.mkv", "Blade Runner", 1982),
		film("Der Pate (1972)/Der Pate (1972) 1080p.mkv", "Der Pate", 1972),
		film("Herr der Ringe/Der Herr der Ringe - Die Gefährten (2001) Extended.mkv", "Der Herr der Ringe: Die Gefährten", 2001),
		film("Kill Bill Vol. 1 (2003).mkv", "Kill Bill Vol. 1", 2003),
		film("Mission Impossible/Mission.Impossible.Fallout.2018.German.DL.1080p.mkv", "Mission Impossible Fallout", 2018),
		film("Inception (2010)/movie.mkv", "Inception", 2010),
		// Release-Ordner mit kleingeschriebener Datei: Emby nimmt bei einem
		// Film im eigenen Ordner den Ordnernamen — die Schreibweise dort ist
		// die gepflegte.
		film("Inception.2010.German.DL.1080p.BluRay.x264-GRP/inception.2010.german.dl.1080p.bluray.x264-grp.mkv", "Inception", 2010),
		film("Fack ju Göhte 2 (2015).mkv", "Fack ju Göhte 2", 2015),
		film("Harry Potter und der Stein der Weisen [2001].mkv", "Harry Potter und der Stein der Weisen", 2001),
		film("Amélie (2001).mkv", "Amélie", 2001),
		film("Die fabelhafte Welt der Amélie 2001.avi", "Die fabelhafte Welt der Amélie", 2001),
		film("Terminator 2 - Tag der Abrechnung (1991).mkv", "Terminator 2: Tag der Abrechnung", 1991),
		film("Ocean's Eleven (2001).mkv", "Ocean's Eleven", 2001),
		film("Babylon A.D. (2008).mkv", "Babylon A.D.", 2008),
		// „Episode IV" ist Teil des Titels, keine Folge.
		film("Star Wars/Star Wars - Episode IV - Eine neue Hoffnung (1977).mkv", "Star Wars: Episode IV: Eine neue Hoffnung", 1977),
		film("24 - Redemption (2008).mkv", "24: Redemption", 2008),
		film("Stargate (1994).mkv", "Stargate", 1994),
		film("Die Feuerzangenbowle (1944).mkv", "Die Feuerzangenbowle", 1944),

		// ---------------------------------------- Befunde November 2026
		// (ausführlich, auch mit Art der Bibliothek: befunde_test.go)
		film("Blade Runner 2049.mkv", "Blade Runner 2049", 0),
		film("Zurück in die Zukunft Teil 2 (1989).mkv", "Zurück in die Zukunft Teil 2", 1989),
		film("Dune Part 2 (2024).mkv", "Dune Part 2", 2024),
		film("The Sample (2020).mkv", "The Sample", 2020),
		film("Dl (2020).mkv", "Dl", 2020),
		film("Matrix (1999)/BDMV/STREAM/00000.m2ts", "Matrix", 1999),
		ep("Trailer Park Boys/Season 1/Trailer.Park.Boys.S01E01.mkv", "Trailer Park Boys", 0, 1, 1),
		ep("Die Simpsons/Staffel 10/Die Simpsons - 1001 - Lisa.mkv", "Die Simpsons", 0, 10, 1),
		ep("Doctor Who/Series 4/01 - Partners in Crime.mkv", "Doctor Who", 0, 4, 1),
		ep("Haus des Geldes/Temporada 2/01 - Atraco.mkv", "Haus des Geldes", 0, 2, 1),
		ep("[SubsPlease] Frieren - 05 (1080p).mkv", "Frieren", 0, 1, 5),
		ep("The Office (US)/Season 1/The.Office.US.S01E01.mkv", "The Office", 0, 1, 1),
		weg("Inception (2010)/Inception (2010) - featurette.mkv"),
		weg("Inception (2010)/Trailer/Teaser.mkv"),
		weg("Inception (2010)/Samples/inception.mkv"),
	}

	if len(faelle) < 60 {
		t.Fatalf("nur %d Fälle", len(faelle))
	}
	for _, f := range faelle {
		t.Run(f.pfad, func(t *testing.T) {
			dirs := splitDirs(f.pfad)
			if f.art == "skip" {
				beiwerk := IsJunkFile(f.pfad)
				for _, d := range dirs {
					beiwerk = beiwerk || IsExtrasDir(d)
				}
				if !beiwerk {
					t.Errorf("sollte als Beiwerk übersprungen werden")
				}
				return
			}
			g := Parse(filepath.Join("/medien", f.pfad), dirs)
			wantTeil := f.teil
			if wantTeil == 0 {
				wantTeil = 1
			}
			var fehler []string
			pruefe := func(was string, ist, soll any) {
				if ist != soll {
					fehler = append(fehler, was+": "+fmtAny(ist)+" statt "+fmtAny(soll))
				}
			}
			pruefe("Art", g.Kind, f.art)
			pruefe("Titel", g.Title, f.titel)
			pruefe("Jahr", g.Year, f.jahr)
			pruefe("Staffel", g.SeasonNo, f.staffel)
			pruefe("Folge", g.EpisodeNo, f.folge)
			pruefe("bis", g.EpisodeTo, f.bis)
			pruefe("Teil", g.PartNo, wantTeil)
			if len(fehler) > 0 {
				t.Error(strings.Join(fehler, "; "))
			}
		})
	}
}

func fmtAny(v any) string {
	if x, ok := v.(string); ok {
		return "„" + x + "“"
	}
	return fmt.Sprint(v)
}

// dateienAnlegen legt leere Dateien unter wurzel an.
func dateienAnlegen(t *testing.T, wurzel string, pfade ...string) {
	t.Helper()
	for _, p := range pfade {
		voll := filepath.Join(wurzel, p)
		if err := os.MkdirAll(filepath.Dir(voll), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(voll, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// Die Serien-Bibliothek des Besitzers im Kleinen, einmal ganz durch den
// Scanner: Jede Serie steht genau einmal da, mit den richtigen Staffeln —
// auch Staffel 0 aus einem Ordner, der „Specials" heisst.
func TestWildnisSerienbibliothek(t *testing.T) {
	ctx := t.Context()
	st, err := store.Open(filepath.Join(t.TempDir(), "l.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	wurzel := t.TempDir()
	dateienAnlegen(t, wurzel,
		"FBI S07E13 - E15 GERMAN 720P WEB H264 - Saint/FBI S07E19 720p - Cosmo71/fbi.s07e19.german.720p.mkv",
		"FBI S07E13 - E15 GERMAN 720P WEB H264 - Saint/fbi.s07e13-e15.german.720p.web.h264-saint.mkv",
		"FBI/Staffel 06/FBI - S06E01.mkv",
		"The Great Game/The Great Game S01E05 - S01E06 (German DL) - Cosmo/The.Great.Game.S01E05.mkv",
		"The Great Game/The Great Game S01E05 - S01E06 (German DL) - Cosmo/The.Great.Game.S01E06.mkv",
		"Incorporated S01E07 German DL 1080p HDTV x264-AIDA/incorporated.s01e07.german.dl.1080p.hdtv.x264-aida.mkv",
		"incorporated.s01e08.german.dl.1080p.hdtv.x264-aida/incorporated.s01e08.german.dl.1080p.hdtv.x264-aida.mkv",
		"His Dark Materials/His Dark Materials - Stafel 03/His Dark Materials - S03E01.mkv",
		"His Dark Materials/His Dark Materials - Staffel 02/His Dark Materials - S02E01.mkv",
		"Doctor Who/Doctor Who - Specials/Doctor Who - S00E39 - Das Monster von den Sternen.mkv",
		"Doctor Who/Doctor Who - Staffel 01/Doctor Who - S01E01.mkv",
		"Sherlock/Specials/Sherlock - S00E01 - Die Braut des Grauens.mkv",
		"Sherlock/Staffel 1/Sherlock - S01E01.mkv",
		"Sherlock/Featurettes/Making of.mkv",
		"Arrow/Arrow - Staffel 01/Arrow - S01E01 - .mkv",
		"Fallout (2024)/Fallout (2024) - Staffel 01/Fallout (2024) - S01E01.mkv",
		"Avatar - Herr der Elemente/Staffel 01/Avatar - S01E01.avi",
	)
	lib, _ := st.CreateLibrary(ctx, "Serien", "series", wurzel, "de")
	if _, err := New(st, nil).Scan(ctx, *lib); err != nil {
		t.Fatal(err)
	}
	if filme, _ := st.Query(ctx, store.Query{Libraries: []string{lib.ID}, Kinds: []string{"movie"}, Limit: 50}); len(filme) != 0 {
		t.Errorf("Filme in der Serienbibliothek: %v", titles(filme))
	}
	want := map[string][]int{ // Titel → Staffelnummern
		"FBI": {6, 7}, "The Great Game": {1}, "Incorporated": {1},
		"His Dark Materials": {2, 3}, "Doctor Who": {0, 1}, "Sherlock": {0, 1},
		"Arrow": {1}, "Fallout": {1}, "Avatar: Herr der Elemente": {1},
	}
	serien, _ := st.Query(ctx, store.Query{Libraries: []string{lib.ID}, Kinds: []string{"series"}, Limit: 50})
	if len(serien) != len(want) {
		t.Errorf("%d Serien statt %d: %v", len(serien), len(want), titles(serien))
	}
	for _, se := range serien {
		nummern, ok := want[se.Title]
		if !ok {
			t.Errorf("unerwartete Serie %q", se.Title)
			continue
		}
		staffeln, _ := st.Children(ctx, se.ID)
		var ist []int
		for _, s := range staffeln {
			ist = append(ist, s.SeasonNo)
		}
		if fmt.Sprint(ist) != fmt.Sprint(nummern) {
			t.Errorf("%s: Staffeln %v statt %v", se.Title, ist, nummern)
		}
		if se.Title == "Fallout" && se.Year != 2024 {
			t.Errorf("Fallout: Jahr %d", se.Year)
		}
	}
}

// Eine ältere Fassung hatte „Fallout (2024)" als Serie „Fallout" ohne Jahr
// angelegt, und TMDb fand nichts. Liefert der Ordner jetzt das Jahr, wird
// es nachgetragen und die Serie neu nachgeschlagen. Eine erkannte Serie
// dagegen bleibt, wie sie ist — da hat TMDb das letzte Wort.
func TestUnerkannteSerieBekommtJahrNachgetragen(t *testing.T) {
	for _, zustand := range []string{"unmatched", "matched"} {
		t.Run(zustand, func(t *testing.T) {
			ctx := t.Context()
			st, err := store.Open(filepath.Join(t.TempDir(), "l.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close()
			wurzel := t.TempDir()
			datei := "Fallout (2024)/Fallout (2024) - Staffel 01/Fallout (2024) - S01E01.mkv"
			dateienAnlegen(t, wurzel, datei)
			lib, _ := st.CreateLibrary(ctx, "Serien", "series", wurzel, "de")

			alt := &store.Item{LibraryID: lib.ID, Kind: "series", Title: "Fallout", MatchState: zustand}
			_ = st.UpsertItem(ctx, alt)
			altSt := &store.Item{LibraryID: lib.ID, Kind: "season", ParentID: alt.ID, Title: "Staffel 1", SeasonNo: 1, MatchState: "matched"}
			_ = st.UpsertItem(ctx, altSt)
			altEp := &store.Item{LibraryID: lib.ID, Kind: "episode", ParentID: altSt.ID, Title: "Folge 1", SeasonNo: 1, EpisodeNo: 1, MatchState: "pending"}
			_ = st.UpsertItem(ctx, altEp)
			_ = st.UpsertFile(ctx, &store.MediaFile{ItemID: altEp.ID, Path: filepath.Join(wurzel, datei), Container: "mkv"})

			if _, err := New(st, nil).Scan(ctx, *lib); err != nil {
				t.Fatal(err)
			}
			serien, _ := st.Query(ctx, store.Query{Libraries: []string{lib.ID}, Kinds: []string{"series"}, Limit: 50})
			if len(serien) != 1 || serien[0].ID != alt.ID {
				t.Fatalf("Serie nicht wiederverwendet: %v", titles(serien))
			}
			se := serien[0]
			if zustand == "unmatched" {
				if se.Year != 2024 || se.Title != "Fallout" || se.MatchState != "pending" {
					t.Errorf("nicht nachgeführt: %q %d %s", se.Title, se.Year, se.MatchState)
				}
			} else if se.Year != 0 || se.MatchState != "matched" {
				t.Errorf("erkannte Serie verändert: %q %d %s", se.Title, se.Year, se.MatchState)
			}
		})
	}
}

// Die Filmbibliothek: Teile a/b/c sind ein Film, zwei Fassungen desselben
// Films ein Eintrag, und das Jahr vorne trennt die Teile einer Reihe.
func TestWildnisFilmbibliothek(t *testing.T) {
	ctx := t.Context()
	st, err := store.Open(filepath.Join(t.TempDir(), "l.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	wurzel := t.TempDir()
	dateienAnlegen(t, wurzel,
		"Star Trek/(1982) Star Trek II - Der Zorn des Khan.avi",
		"Star Trek/(1984) Star Trek III - Auf der Suche nach Mr. Spock.avi",
		"Apocalypse Now (1979) a.avi", "Apocalypse Now (1979) b.avi", "Apocalypse Now (1979) c.avi",
		"American Psycho (2000).mkv", "American Psycho (2000).avi",
		"1917.2019.German.DL.1080p.BluRay.x264-GRP.mkv",
		"Sherlock Holmes (2009)/Specials/Making of.mkv",
	)
	lib, _ := st.CreateLibrary(ctx, "Filme", "movie", wurzel, "de")
	if _, err := New(st, nil).Scan(ctx, *lib); err != nil {
		t.Fatal(err)
	}
	filme, _ := st.Query(ctx, store.Query{Libraries: []string{lib.ID}, Kinds: []string{"movie"}, Limit: 50})
	want := map[string]int{ // Titel → Zahl der Dateien
		"Star Trek II: Der Zorn des Khan": 1, "Star Trek III: Auf der Suche nach Mr. Spock": 1,
		"Apocalypse Now": 3, "American Psycho": 2, "1917": 1,
	}
	if len(filme) != len(want) {
		t.Errorf("%d Filme statt %d: %v", len(filme), len(want), titles(filme))
	}
	for _, f := range filme {
		n, ok := want[f.Title]
		if !ok {
			t.Errorf("unerwarteter Film %q", f.Title)
			continue
		}
		if d, _ := st.FilesFor(ctx, f.ID); len(d) != n {
			t.Errorf("%s: %d Dateien statt %d", f.Title, len(d), n)
		}
	}
	if serien, _ := st.Query(ctx, store.Query{Libraries: []string{lib.ID}, Kinds: []string{"series"}, Limit: 50}); len(serien) != 0 {
		t.Errorf("Specials neben einem Film wurden zur Serie: %v", titles(serien))
	}
}
