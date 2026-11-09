// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package scan

import "testing"

// Die Fälle hier stammen aus echten Sammlungen: so sehen Dateien aus, die
// über Jahre von verschiedenen Quellen zusammengekommen sind.
func TestParse(t *testing.T) {
	cases := []struct {
		name string
		path string
		dirs []string
		want Guess
	}{
		{
			name: "Film mit Jahr in Klammern",
			path: "Die Bucht (2019).mkv",
			want: Guess{Kind: "movie", Title: "Die Bucht", Year: 2019, Confident: true},
		},
		{
			// „ - " steht im Dateinamen für den Doppelpunkt.
			path: "Der Herr der Ringe - Die zwei Türme (2002).mkv",
			want: Guess{Kind: "movie", Title: "Der Herr der Ringe: Die zwei Türme", Year: 2002, Confident: true},
		},
		{
			name: "Film im Release-Format",
			path: "Die.Bucht.2019.German.1080p.BluRay.x264-GRUPPE.mkv",
			want: Guess{Kind: "movie", Title: "Die Bucht", Year: 2019,
				Quality: "1080p", Language: "de", Confident: true},
		},
		{
			name: "Film in eigenem Ordner, Datei heisst nichts",
			path: "movie.mkv",
			dirs: []string{"Filme", "Nordlicht (2025)"},
			want: Guess{Kind: "movie", Title: "Nordlicht", Year: 2025, Confident: true},
		},
		{
			name: "4K mit Dolby Vision",
			path: "Meridian.2021.2160p.UHD.BluRay.DV.HDR.x265-XYZ.mkv",
			want: Guess{Kind: "movie", Title: "Meridian", Year: 2021,
				Quality: "2160p", HDR: "DV", Confident: true},
		},
		{
			name: "Director's Cut",
			path: "Waldrand (2020) Director's Cut.mkv",
			want: Guess{Kind: "movie", Title: "Waldrand", Year: 2020,
				Edition: "Director's Cut", Confident: true},
		},
		{
			name: "Mehrteiler",
			path: "Sommerhaus (1998) CD2.avi",
			want: Guess{Kind: "movie", Title: "Sommerhaus", Year: 1998,
				PartNo: 2, Confident: true},
		},
		{
			name: "Serie, Standardform",
			path: "Nordlicht.S01E04.Rueckkehr.1080p.WEB-DL.mkv",
			want: Guess{Kind: "episode", Title: "Nordlicht", SeasonNo: 1, EpisodeNo: 4,
				Quality: "1080p", Confident: true},
		},
		{
			name: "Serie, Kreuzform",
			path: "Ostwind - 2x07 - Die Bucht.mkv",
			want: Guess{Kind: "episode", Title: "Ostwind", SeasonNo: 2, EpisodeNo: 7,
				Confident: true},
		},
		{
			name: "Doppelfolge",
			path: "Halbmond.S03E05E06.mkv",
			want: Guess{Kind: "episode", Title: "Halbmond", SeasonNo: 3,
				EpisodeNo: 5, EpisodeTo: 6, Confident: true},
		},
		{
			name: "Folge nur mit Nummer, Staffel steht im Ordner",
			path: "E07.mkv",
			dirs: []string{"Serien", "Tiefsee", "Staffel 2"},
			want: Guess{Kind: "episode", Title: "Tiefsee", SeasonNo: 2, EpisodeNo: 7,
				Confident: true},
		},
		{
			name: "Dreistellige Folgennummer im Staffelordner",
			path: "203 - Unter dem Eis.mkv",
			dirs: []string{"Serien", "Stille Wasser", "Season 2"},
			want: Guess{Kind: "episode", Title: "Stille Wasser", SeasonNo: 2, EpisodeNo: 3,
				Confident: true},
		},
		{
			name: "Serientitel steht nur im Ordner",
			path: "S01E02.mkv",
			dirs: []string{"Serien", "Morgengrau (2023)", "S01"},
			// Das Jahr am Serienordner gehört zur Serie.
			want: Guess{Kind: "episode", Title: "Morgengrau", Year: 2023, SeasonNo: 1, EpisodeNo: 2,
				Confident: true},
		},
		{
			// Die häufigste Ablage überhaupt — und bisher ein Film namens „01: Pilot".
			name: "Nummer am Anfang im Staffelordner",
			path: "01 - Pilot.mkv",
			dirs: []string{"Serien", "Morgengrau", "Staffel 1"},
			want: Guess{Kind: "episode", Title: "Morgengrau", SeasonNo: 1, EpisodeNo: 1,
				EpisodeTitle: "Pilot", Confident: true},
		},
		{
			name: "Nummer zwischen Serie und Folgenname",
			path: "Morgengrau - 03 - Der Fund.mkv",
			dirs: []string{"Serien", "Morgengrau", "Staffel 2"},
			want: Guess{Kind: "episode", Title: "Morgengrau", SeasonNo: 2, EpisodeNo: 3,
				EpisodeTitle: "Der Fund", Confident: true},
		},
		{
			name: "Folgenname hinter S01E01",
			path: "Morgengrau - S01E04 - Rückkehr.mkv",
			dirs: []string{"Serien", "Morgengrau", "Staffel 1"},
			want: Guess{Kind: "episode", Title: "Morgengrau", SeasonNo: 1, EpisodeNo: 4,
				EpisodeTitle: "Rückkehr", Confident: true},
		},
		{
			// Ohne Nummer, aber im Staffelordner: eine Folge, keine Filmdatei.
			name: "Ohne Nummer im Staffelordner",
			path: "Pilot.mkv",
			dirs: []string{"Serien", "Morgengrau", "Staffel 1"},
			want: Guess{Kind: "episode", Title: "Morgengrau", SeasonNo: 1, EpisodeNo: 0,
				EpisodeTitle: "Pilot", Confident: false},
		},
		{
			name: "Specials-Ordner ist Staffel 0",
			path: "Morgengrau - S00E01 - Making-of.mkv",
			dirs: []string{"Serien", "Morgengrau", "Specials"},
			want: Guess{Kind: "episode", Title: "Morgengrau", SeasonNo: 0, EpisodeNo: 1,
				EpisodeTitle: "Making-of", Confident: true},
		},
		{
			name: "Ein Wort ohne Jahr bleibt unsicher",
			path: "irgendwas.mkv",
			want: Guess{Kind: "movie", Title: "irgendwas", Confident: false},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Parse(c.path, c.dirs)
			check := func(what string, got, want any) {
				t.Helper()
				if got != want {
					t.Errorf("%s: %v, erwartet %v", what, got, want)
				}
			}
			check("Art", got.Kind, c.want.Kind)
			check("Titel", got.Title, c.want.Title)
			check("Jahr", got.Year, c.want.Year)
			check("Staffel", got.SeasonNo, c.want.SeasonNo)
			check("Folge", got.EpisodeNo, c.want.EpisodeNo)
			check("sicher", got.Confident, c.want.Confident)
			if c.want.EpisodeTo != 0 {
				check("bis Folge", got.EpisodeTo, c.want.EpisodeTo)
			}
			if c.want.Quality != "" {
				check("Qualität", got.Quality, c.want.Quality)
			}
			if c.want.HDR != "" {
				check("HDR", got.HDR, c.want.HDR)
			}
			if c.want.Edition != "" {
				check("Fassung", got.Edition, c.want.Edition)
			}
			if c.want.PartNo > 1 {
				check("Teil", got.PartNo, c.want.PartNo)
			}
			if c.want.Language != "" {
				check("Sprache", got.Language, c.want.Language)
			}
		})
	}
}

// Bindestriche mitten im Titel dürfen nicht verschwinden.
func TestCleanKeepsHyphenatedTitles(t *testing.T) {
	g := Parse("Spider-Man.2002.1080p.BluRay.x264.mkv", nil)
	if g.Title != "Spider-Man" {
		t.Fatalf("Titel: %q", g.Title)
	}
}

// „dl" als Sprachkürzel darf nicht in einem Wort zuschlagen.
func TestJunkMatchesWholeWordsOnly(t *testing.T) {
	g := Parse("Waldrand (2020).mkv", nil)
	if g.Title != "Waldrand" {
		t.Fatalf("Titel: %q", g.Title)
	}
}

func TestJunkFiles(t *testing.T) {
	for _, p := range []string{"sample.mkv", "Nordlicht-trailer.mp4", "._film.mkv", ".DS_Store"} {
		if !IsJunkFile(p) {
			t.Errorf("%q sollte aussortiert werden", p)
		}
	}
	if IsJunkFile("Nordlicht (2025).mkv") {
		t.Error("echte Datei wurde aussortiert")
	}
}

// Das Jahr steht oft nur im Ordnernamen — es wäre ein Jammer, es
// wegzuwerfen, bloss weil die Datei selbst einen Titel hat.
func TestYearComesFromFolderWhenFilenameHasNone(t *testing.T) {
	g := Parse("Sommerhaus CD1.avi", []string{"Filme", "Sommerhaus (1998)"})
	if g.Title != "Sommerhaus" {
		t.Errorf("Titel %q", g.Title)
	}
	if g.Year != 1998 {
		t.Errorf("Jahr %d — das stand im Ordner", g.Year)
	}
	if g.PartNo != 1 {
		t.Errorf("Teil %d", g.PartNo)
	}
	if !g.Confident {
		t.Error("mit Jahr aus dem Ordner sollte der Fund sicher sein")
	}
	// Das Jahr im Dateinamen wiegt schwerer als das im Ordner.
	g = Parse("Waldrand (2020).mkv", []string{"Filme", "Waldrand (1999)"})
	if g.Year != 2020 {
		t.Errorf("Jahr %d — der Dateiname hat Vorrang", g.Year)
	}
}

// Fälle aus der Sammlung, an der die Zuordnung Anfang November scheiterte:
// Serien wurden mehrfach angelegt, Staffelordner wurden selbst zu Serien,
// und Filme mit dem Jahr vorne hiessen alle wie ihr Ordner.
func TestZuordnungAusEchterSammlung(t *testing.T) {
	for i, n := range []string{"a", "b", "c"} {
		if g := Parse("Apocalypse Now (1979) "+n+".avi", nil); g.PartNo != i+1 {
			t.Errorf("Teil %s: %d", n, g.PartNo)
		}
	}
	cases := []struct {
		path    string
		dirs    []string
		kind    string
		title   string
		year    int
		season  int
		episode int
	}{
		// Jahr vorne
		{"(1982) Star Trek II - Der Zorn des Khan.avi", []string{"Star Trek"}, "movie", "Star Trek II: Der Zorn des Khan", 1982, 0, 0},
		{"(1976) Rocky 1.mkv", []string{"Rockys"}, "movie", "Rocky 1", 1976, 0, 0},
		{"(1967) Gott vergibt - Django nie.avi", []string{"Bud Spencer & Terence Hill"}, "movie", "Gott vergibt: Django nie", 1967, 0, 0},
		{"(2009) 2012.mkv", []string{"Katastrophen"}, "movie", "2012", 2009, 0, 0},
		{"Apocalypse Now (1979) a.avi", nil, "movie", "Apocalypse Now", 1979, 0, 0},
		{"(2014) Bibi und Tina - Voll verhext.avi", []string{"Bibi und Tina"}, "movie", "Bibi und Tina: Voll verhext", 2014, 0, 0},
		{"Babylon A.D. (2008).mkv", nil, "movie", "Babylon A.D.", 2008, 0, 0},
		// Release-Ordner, der auf „Special" endet, gehört zur Serie darüber
		{"solar.opposites.s03e00.german.dl.720p.web.h264-wvf.mkv", []string{"Solar Opposites", "Solar Opposites - Staffel 03",
			"Solar.Opposites.S03E00 Ein finsteres, grusliges Halloween-Opposites-Solar-Special"}, "episode", "Solar Opposites", 0, 3, 0},
		// „German" im Titel
		{"The German - S01E01.mkv", []string{"The German"}, "episode", "The German", 0, 1, 1},
		{"The German - S01E01.mkv", nil, "episode", "The German", 0, 1, 1},
		// Staffelordner mit Namen
		{"s06e16.avi", []string{"Grey's Anatomy", "Greys Anatomy - Staffel 06"}, "episode", "Grey's Anatomy", 0, 6, 16},
		{"s05e01.avi", []string{"Warehouse 13 - Staffel 05"}, "episode", "Warehouse 13", 0, 5, 1},
		{"1x01+1x02.001.Star.Trek.Voyager-Der.Fuersorger.avi", []string{"Star Trek - Raumschiff Voyager - Staffel 01"}, "episode", "Star Trek: Raumschiff Voyager", 0, 1, 1},
		{"The.Blacklist.S01E02.German.1080p.mkv", []string{"The Blacklist", "The.Blacklist.S01.German.1080p.WEB"}, "episode", "The Blacklist", 0, 1, 2},
		// Der Ordner zählt mehr als der Dateiname
		{"Star Wars - Ahsok - S01E01.mkv", []string{"Star Wars Ahsoka"}, "episode", "Star Wars Ahsoka", 0, 1, 1},
		{"The.Blacklist - S03E23 - Alexander Kirk.mkv", []string{"The Blacklist"}, "episode", "The Blacklist", 0, 3, 23},
		{"The Blacklist - S01E01 - Pilot - mkv - by Videomann.mkv", []string{"The Blacklist"}, "episode", "The Blacklist", 0, 1, 1},
		// Abkürzungen mit Punkten
		{"Agents of S.H.I.E.L.D. - S01E01 - Pilot.mkv", nil, "episode", "Agents of S.H.I.E.L.D.", 0, 1, 1},
		{"S.W.A.T.2017.S01E01.German.mkv", nil, "episode", "S.W.A.T.", 2017, 1, 1},
		// Jahr am Ende des Serientitels
		{"The Office 2024 - S01E01.mkv", []string{"The Office 2024"}, "episode", "The Office", 2024, 1, 1},
		{"1883 - S01E01.mkv", []string{"1883"}, "episode", "1883", 0, 1, 1},
		// Staffelordner nackt wie bisher
		{"Folge 3.mkv", []string{"Nordlicht (2025)", "Staffel 2"}, "episode", "Nordlicht", 2025, 2, 3},
		// Kein Staffelordner: „Seasons 4" ist ein Name
		{"Seasons 4 - S01E01.mkv", nil, "episode", "Seasons 4", 0, 1, 1},
	}
	for _, c := range cases {
		g := Parse(c.path, c.dirs)
		if g.Kind != c.kind || g.Title != c.title || g.Year != c.year || g.SeasonNo != c.season || g.EpisodeNo != c.episode {
			t.Errorf("%s in %v:\n  %s %q %d S%dE%d\n  erwartet %s %q %d S%dE%d", c.path, c.dirs,
				g.Kind, g.Title, g.Year, g.SeasonNo, g.EpisodeNo, c.kind, c.title, c.year, c.season, c.episode)
		}
	}
}

func TestFolgentitelAusReleaseOrdner(t *testing.T) {
	g := Parse("solar.opposites.s03e00.german.dl.720p.web.h264-wvf.mkv", []string{"Solar Opposites", "Solar Opposites - Staffel 03",
		"Solar.Opposites.S03E00 Ein finsteres, grusliges Halloween-Opposites-Solar-Special"})
	if g.EpisodeTitle != "Ein finsteres, grusliges Halloween-Opposites-Solar-Special" {
		t.Fatalf("Folgentitel %q", g.EpisodeTitle)
	}
	if g := Parse("serie.s01e02.german.dl.720p.web.h264-x.mkv", []string{"Serie"}); g.EpisodeTitle != "" {
		t.Fatalf("Beiwerk als Folgentitel: %q", g.EpisodeTitle)
	}
}

// Ein Ordner, der eine Folge nennt, aber viele enthält, gibt seinen Titel
// nicht allen Dateien darin.
func TestOrdnerTitelNurFuerDieseFolge(t *testing.T) {
	dirs := []string{"Senna", "Senna.S01E06.Ayrton.Senna.Tragic.Accident.1994"}
	for _, f := range []string{"Senna.S01E07.mkv", "Senna.S01E08.mkv"} {
		if g := Parse(f, dirs); g.EpisodeTitle != "" {
			t.Errorf("%s: Folgentitel %q aus fremdem Ordner", f, g.EpisodeTitle)
		}
	}
	if g := Parse("Senna.S01E06.mkv", dirs); g.EpisodeTitle == "" {
		t.Error("die genannte Folge bekommt ihren Titel nicht")
	}
}
