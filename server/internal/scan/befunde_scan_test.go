// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package scan

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"lumen/internal/store"
)

func testStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "l.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

// dateiMitGroesse legt eine Datei mit n Bytes an (BluRay/DVD: die
// grösste Datei ist der Film).
func dateiMitGroesse(t *testing.T, wurzel, pfad string, n int) {
	t.Helper()
	voll := filepath.Join(wurzel, pfad)
	if err := os.MkdirAll(filepath.Dir(voll), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(voll, make([]byte, n), 0o644); err != nil {
		t.Fatal(err)
	}
}

func textDatei(t *testing.T, wurzel, pfad, inhalt string) {
	t.Helper()
	voll := filepath.Join(wurzel, pfad)
	if err := os.MkdirAll(filepath.Dir(voll), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(voll, []byte(inhalt), 0o644); err != nil {
		t.Fatal(err)
	}
}

// serienBaum gibt je Serie (Titel, Jahr, Land) die Staffeln mit ihren
// Folgennummern zurück: „The Office|0|US" → „1:[1 2]".
func serienBaum(t *testing.T, st *store.Store, libID string) map[string]string {
	t.Helper()
	ctx := context.Background()
	serien, _ := st.Query(ctx, store.Query{Libraries: []string{libID}, Kinds: []string{"series"}, Limit: 200})
	out := map[string]string{}
	for _, se := range serien {
		var teile []string
		staffeln, _ := st.Children(ctx, se.ID)
		for _, s := range staffeln {
			folgen, _ := st.Children(ctx, s.ID)
			var nr []int
			for _, f := range folgen {
				nr = append(nr, f.EpisodeNo)
			}
			sort.Ints(nr)
			teile = append(teile, fmt.Sprintf("%d:%v", s.SeasonNo, nr))
		}
		sort.Strings(teile)
		k := fmt.Sprintf("%s|%d|%s", se.Title, se.Year, st.ZusatzZu(ctx, se.ID).Land)
		if _, doppelt := out[k]; doppelt {
			k += "#2"
		}
		out[k] = strings.Join(teile, " ")
	}
	return out
}

// Ein echter Durchlauf über eine Serienbibliothek mit allen Befunden.
func TestBefundeSerienbibliothek(t *testing.T) {
	ctx := t.Context()
	st := testStore(t)
	wurzel := t.TempDir()
	dateienAnlegen(t, wurzel,
		// 1. Anime
		"One Piece/[SubsPlease] One Piece - 1071 (1080p).mkv",
		"One Piece/[SubsPlease] One Piece - 1072 (1080p).mkv",
		"Frieren/[SubsPlease] Frieren - 05 (1080p) [ABCD1234].mkv",
		"Frieren/Frieren - 12v2.mkv",
		"Naruto Shippuden/Naruto Shippuden - 001.mkv",
		// 2. Tagesfolgen
		"The Daily Show/The.Daily.Show.2024.03.15.Guest.720p.WEB.mkv",
		"Last Week Tonight/Last Week Tonight - 2024-03-15.mkv",
		"Tagesschau/Tagesschau 2024-03-15.mkv",
		"Tagesschau/Tagesschau 2024-03-16.mkv",
		// 3. Wortteile
		"Trailer Park Boys/Season 1/Trailer.Park.Boys.S01E01.mkv",
		"Trailer Park Boys/Season 1/Trailer.Park.Boys.S01E01-sample.mkv",
		// 4. gleichnamige Serien
		"Battlestar Galactica (2003)/Season 1/Battlestar Galactica S01E01.mkv",
		"Battlestar Galactica (1978)/Season 1/Battlestar Galactica S01E01.mkv",
		"The Office (US)/Season 1/The.Office.US.S01E01.mkv",
		"The Office (UK)/Series 1/The Office S01E01.mkv",
		// 5. Systemordner
		"#recycle/Dexter/Season 1/Dexter.S01E01.mkv",
		"Dexter/Season 1/@eaDir/Dexter.S01E01.mkv/SYNOVIDEO_VIDEO_SCREENSHOT.mkv",
		"$RECYCLE.BIN/Lost.S01E01.mkv",
		// 8. Staffelordner
		"Avatar/Book 2/Avatar - 03.mkv",
		"Das Boot/Teil 2/01 - Unter Wasser.mkv",
		// 9. Simpsons
		"Die Simpsons/Staffel 10/Die Simpsons - 1001 - Lisa.mkv",
		// 11. Miniserien
		"Chernobyl/Chernobyl.Part.1.mkv",
		"Chernobyl/Chernobyl.Part.2.mkv",
		"Chernobyl/Chernobyl.Part.3.mkv",
		"Planet Erde II/Planet Erde II - Teil 1.mkv",
		"Planet Erde II/Planet Erde II - Teil 2.mkv",
		// 12. E01 ohne Staffel
		"Spartacus Gods of the Arena/Spartacus.Gods.of.the.Arena.E01.mkv",
		"Spartacus Gods of the Arena/Spartacus.Gods.of.the.Arena.E02.mkv",
		// 13. Doppelfolge
		"Friends/Season 1/Friends.S01E16E17.mkv",
		"Friends/Season 1/Friends.S01E18.mkv",
	)
	lib, _ := st.CreateLibrary(ctx, "Serien", "series", wurzel, "de")
	if _, err := New(st, nil).Scan(ctx, *lib); err != nil {
		t.Fatal(err)
	}
	if filme, _ := st.Query(ctx, store.Query{Libraries: []string{lib.ID}, Kinds: []string{"movie"}, Limit: 50}); len(filme) != 0 {
		t.Errorf("Filme in der Serienbibliothek: %v", titles(filme))
	}
	want := map[string]string{
		"One Piece|0|":                   "1:[1071 1072]",
		"Frieren|0|":                     "1:[5 12]",
		"Naruto Shippuden|0|":            "1:[1]",
		"The Daily Show|0|":              "2024:[315]",
		"Last Week Tonight|0|":           "2024:[315]",
		"Tagesschau|0|":                  "2024:[315 316]",
		"Trailer Park Boys|0|":           "1:[1]",
		"Battlestar Galactica|2003|":     "1:[1]",
		"Battlestar Galactica|1978|":     "1:[1]",
		"The Office|0|US":                "1:[1]",
		"The Office|0|GB":                "1:[1]",
		"Avatar|0|":                      "2:[3]",
		"Das Boot|0|":                    "2:[1]",
		"Die Simpsons|0|":                "10:[1]",
		"Chernobyl|0|":                   "1:[1 2 3]",
		"Planet Erde II|0|":              "1:[1 2]",
		"Spartacus Gods of the Arena|0|": "1:[1 2]",
		"Friends|0|":                     "1:[16 18]",
	}
	ist := serienBaum(t, st, lib.ID)
	for k, v := range want {
		if ist[k] != v {
			t.Errorf("%s: %q statt %q", k, ist[k], v)
		}
	}
	for k := range ist {
		if _, ok := want[k]; !ok {
			t.Errorf("unerwartete Serie %s: %s", k, ist[k])
		}
	}

	// Tagesfolge: das Sendedatum steht an der Folge; durchgezählte und
	// Doppelfolgen sind vermerkt — die Zuordnung braucht es.
	pruefeFolge := func(serie string, staffel, folge int, f func(store.Item, store.Zusatz)) {
		t.Helper()
		serien, _ := st.Query(ctx, store.Query{Libraries: []string{lib.ID}, Kinds: []string{"series"}, Text: serie, Limit: 5})
		for _, se := range serien {
			if se.Title != serie {
				continue
			}
			staffeln, _ := st.Children(ctx, se.ID)
			for _, s := range staffeln {
				folgen, _ := st.Children(ctx, s.ID)
				for _, e := range folgen {
					if e.SeasonNo == staffel && e.EpisodeNo == folge {
						f(e, st.ZusatzZu(ctx, e.ID))
						return
					}
				}
			}
		}
		t.Errorf("%s S%dE%d nicht gefunden", serie, staffel, folge)
	}
	pruefeFolge("Tagesschau", 2024, 315, func(e store.Item, _ store.Zusatz) {
		if e.Premiered != "2024-03-15" {
			t.Errorf("Sendedatum %q", e.Premiered)
		}
	})
	pruefeFolge("One Piece", 1, 1071, func(_ store.Item, z store.Zusatz) {
		if !z.Absolut {
			t.Error("One Piece 1071 nicht als durchgezählt vermerkt")
		}
	})
	pruefeFolge("Friends", 1, 16, func(e store.Item, z store.Zusatz) {
		if z.BisFolge != 17 {
			t.Errorf("Doppelfolge bis %d", z.BisFolge)
		}
		if e.Title != "Folge 16–17" {
			t.Errorf("Titel der Doppelfolge %q", e.Title)
		}
	})

	// Ein zweiter Durchlauf ändert nichts (keine neuen Serien, keine
	// Doppel durch Jahr oder Land).
	if _, err := New(st, nil).Scan(ctx, *lib); err != nil {
		t.Fatal(err)
	}
	if nochmal := serienBaum(t, st, lib.ID); fmt.Sprint(nochmal) != fmt.Sprint(ist) {
		t.Errorf("zweiter Durchlauf verändert:\n%v\n%v", ist, nochmal)
	}
}

// 4. Bestehende Daten: Eine ältere Fassung hatte „Battlestar Galactica"
// 1978 und 2003 zu einer Serie zusammengelegt (die von TMDb als 2003
// erkannt war). Der nächste Durchlauf trennt sie.
func TestZusammengelegteSerienWerdenGetrennt(t *testing.T) {
	ctx := t.Context()
	st := testStore(t)
	wurzel := t.TempDir()
	alt := "Battlestar Galactica (1978)/Season 1/Battlestar Galactica S01E01.mkv"
	neu := "Battlestar Galactica (2003)/Season 1/Battlestar Galactica S01E02.mkv"
	dateienAnlegen(t, wurzel, alt, neu)
	lib, _ := st.CreateLibrary(ctx, "Serien", "series", wurzel, "de")
	se := &store.Item{LibraryID: lib.ID, Kind: "series", Title: "Battlestar Galactica", Year: 2003, MatchState: "matched"}
	_ = st.UpsertItem(ctx, se)
	sf := &store.Item{LibraryID: lib.ID, Kind: "season", ParentID: se.ID, Title: "Staffel 1", SeasonNo: 1, MatchState: "matched"}
	_ = st.UpsertItem(ctx, sf)
	for i, p := range []string{alt, neu} {
		ep := &store.Item{LibraryID: lib.ID, Kind: "episode", ParentID: sf.ID, Title: "x", SeasonNo: 1, EpisodeNo: i + 1, MatchState: "matched"}
		_ = st.UpsertItem(ctx, ep)
		_ = st.UpsertFile(ctx, &store.MediaFile{ItemID: ep.ID, Path: filepath.Join(wurzel, p), Container: "mkv"})
	}
	if _, err := New(st, nil).Scan(ctx, *lib); err != nil {
		t.Fatal(err)
	}
	ist := serienBaum(t, st, lib.ID)
	if ist["Battlestar Galactica|1978|"] != "1:[1]" || !strings.Contains(ist["Battlestar Galactica|2003|"], "2") {
		t.Errorf("nicht getrennt: %v", ist)
	}
}

// 5. Was früher aus einem Papierkorb eingelesen wurde, verschwindet.
func TestSystemordnerWerdenAusgetragen(t *testing.T) {
	ctx := t.Context()
	st := testStore(t)
	wurzel := t.TempDir()
	dateienAnlegen(t, wurzel,
		"Matrix (1999)/Matrix (1999).mkv",
		"#recycle/Matrix (1999)/Matrix (1999).mkv",
		"#recycle/Alien (1979)/Alien (1979).mkv",
		"Matrix (1999)/@eaDir/Matrix (1999).mkv/SYNOVIDEO_VIDEO_SCREENSHOT.mkv",
		".@__thumb/Brazil (1985).mkv",
		"System Volume Information/Brazil (1985).mkv",
	)
	lib, _ := st.CreateLibrary(ctx, "Filme", "movie", wurzel, "de")
	// Eine ältere Fassung hatte den Papierkorb mit eingelesen.
	altFilm := &store.Item{LibraryID: lib.ID, Kind: "movie", Title: "Alien", Year: 1979, MatchState: "pending"}
	_ = st.UpsertItem(ctx, altFilm)
	_ = st.UpsertFile(ctx, &store.MediaFile{ItemID: altFilm.ID, Path: filepath.Join(wurzel, "#recycle/Alien (1979)/Alien (1979).mkv"), Container: "mkv"})

	if n := zaehlenFuer(wurzel, "movie"); n != 1 {
		t.Errorf("Vorlauf zählt %d Dateien", n)
	}
	if _, err := New(st, nil).Scan(ctx, *lib); err != nil {
		t.Fatal(err)
	}
	_, _ = st.FilmeOhneDatei(ctx)
	filme, _ := st.Query(ctx, store.Query{Libraries: []string{lib.ID}, Kinds: []string{"movie"}, Limit: 50})
	if len(filme) != 1 || filme[0].Title != "Matrix" {
		t.Errorf("Filme: %v", titles(filme))
	}
	if fehlend, _ := st.MissingFiles(ctx); len(fehlend) != 0 {
		t.Errorf("als fehlend statt ausgetragen: %d", len(fehlend))
	}
}

// Eine Filmbibliothek mit Teilen, BluRay, DVD, Beiwerk und Titeln, die
// wie Technik heissen.
func TestBefundeFilmbibliothek(t *testing.T) {
	ctx := t.Context()
	st := testStore(t)
	wurzel := t.TempDir()
	dateienAnlegen(t, wurzel,
		// 3. Beiwerk neben dem Film wird keine zweite Fassung
		"Inception (2010)/Inception (2010).mkv",
		"Inception (2010)/Inception (2010) - featurette.mkv",
		"Inception (2010)/Inception (2010)-trailer.mkv",
		"Inception (2010)/Trailers/Teaser.mkv",
		"Inception (2010)/Featurette/Making of.mkv",
		"Inception (2010)/Other/Gag Reel.mkv",
		"The Sample (2020)/The Sample (2020).mkv",
		// 6. Teile
		"Zurück in die Zukunft Teil 2 (1989).mkv",
		"Dune Part 2 (2024).mkv",
		"Kill Bill Vol 2.mkv",
		"Der lange Film (2001)/Der lange Film - Teil 1.mkv",
		"Der lange Film (2001)/Der lange Film - Teil 2.mkv",
		"Kill Bill/Kill Bill - Teil 1.mkv",
		"Es (1990) Teil 1.avi", "Es (1990) Teil 2.avi",
		// 7. Jahr aus der Zukunft
		"Blade Runner 2049.mkv",
		// 14. Technik-Wörter und Kleinschreibung
		"Dl (2020).mkv",
		"pulp_fiction_1994_1080p.mkv",
		// .iso und .strm bleiben draussen
		"Brazil (1985)/Brazil.iso",
		"Brazil (1985)/Brazil.strm",
	)
	// 10. BluRay: nur die grösste .m2ts; DVD: der grösste Titelsatz in Teilen.
	dateiMitGroesse(t, wurzel, "Matrix (1999)/BDMV/STREAM/00000.m2ts", 100)
	dateiMitGroesse(t, wurzel, "Matrix (1999)/BDMV/STREAM/00001.m2ts", 5000)
	dateiMitGroesse(t, wurzel, "Matrix (1999)/BDMV/STREAM/00002.m2ts", 300)
	dateiMitGroesse(t, wurzel, "Alien (1979)/VIDEO_TS/VIDEO_TS.VOB", 50)
	dateiMitGroesse(t, wurzel, "Alien (1979)/VIDEO_TS/VTS_01_0.VOB", 50)
	dateiMitGroesse(t, wurzel, "Alien (1979)/VIDEO_TS/VTS_01_1.VOB", 400)
	dateiMitGroesse(t, wurzel, "Alien (1979)/VIDEO_TS/VTS_02_0.VOB", 50)
	dateiMitGroesse(t, wurzel, "Alien (1979)/VIDEO_TS/VTS_02_1.VOB", 1000)
	dateiMitGroesse(t, wurzel, "Alien (1979)/VIDEO_TS/VTS_02_2.VOB", 600)

	lib, _ := st.CreateLibrary(ctx, "Filme", "movie", wurzel, "de")
	if _, err := New(st, nil).Scan(ctx, *lib); err != nil {
		t.Fatal(err)
	}
	filme, _ := st.Query(ctx, store.Query{Libraries: []string{lib.ID}, Kinds: []string{"movie"}, Limit: 50})
	want := map[string][]string{ // Titel (Jahr) → Dateien, nach Teil
		"Inception (2010)":                    {"Inception (2010).mkv"},
		"The Sample (2020)":                   {"The Sample (2020).mkv"},
		"Zurück in die Zukunft Teil 2 (1989)": {"Zurück in die Zukunft Teil 2 (1989).mkv"},
		"Dune Part 2 (2024)":                  {"Dune Part 2 (2024).mkv"},
		"Kill Bill Vol 2 (0)":                 {"Kill Bill Vol 2.mkv"},
		"Der lange Film (2001)":               {"Der lange Film - Teil 1.mkv", "Der lange Film - Teil 2.mkv"},
		"Kill Bill: Teil 1 (0)":               {"Kill Bill - Teil 1.mkv"},
		"Es (1990)":                           {"Es (1990) Teil 1.avi", "Es (1990) Teil 2.avi"},
		"Blade Runner 2049 (0)":               {"Blade Runner 2049.mkv"},
		"Dl (2020)":                           {"Dl (2020).mkv"},
		"Pulp Fiction (1994)":                 {"pulp_fiction_1994_1080p.mkv"},
		"Matrix (1999)":                       {"00001.m2ts"},
		"Alien (1979)":                        {"VTS_02_1.VOB", "VTS_02_2.VOB"},
	}
	ist := map[string][]string{}
	for _, f := range filme {
		dateien, _ := st.FilesFor(ctx, f.ID)
		sort.Slice(dateien, func(i, j int) bool { return dateien[i].PartNo < dateien[j].PartNo })
		var namen []string
		for i, d := range dateien {
			namen = append(namen, filepath.Base(d.Path))
			if len(dateien) > 1 && d.PartNo != i+1 {
				t.Errorf("%s: Teil %d an Stelle %d", f.Title, d.PartNo, i+1)
			}
		}
		ist[fmt.Sprintf("%s (%d)", f.Title, f.Year)] = namen
	}
	if fmt.Sprint(ist) != fmt.Sprint(want) {
		for k, v := range want {
			if fmt.Sprint(ist[k]) != fmt.Sprint(v) {
				t.Errorf("%s: %v statt %v", k, ist[k], v)
			}
		}
		for k, v := range ist {
			if _, ok := want[k]; !ok {
				t.Errorf("unerwartet: %s %v", k, v)
			}
		}
	}
	if n := zaehlenFuer(wurzel, "movie"); n != len(filme)+3 { // drei Filme haben zwei Teile
		t.Errorf("Vorlauf zählt %d Dateien", n)
	}
}

// 15. und 16. NFO-Dateien und Bilder neben den Dateien.
func TestLokaleMetadatenUndBilder(t *testing.T) {
	ctx := t.Context()
	st := testStore(t)
	wurzel, bilder := t.TempDir(), t.TempDir()
	dateienAnlegen(t, wurzel,
		"Mein Urlaub (2019)/Mein Urlaub (2019).mkv",
		"Matrix (1999)/Matrix (1999).mkv",
		"Filme ohne Ordner.mkv",
		"Unbekannt (2005)/Unbekannt.mkv",
	)
	// Vollständige NFO ohne Kennung: gilt für sich, „lokal" erkannt.
	textDatei(t, wurzel, "Mein Urlaub (2019)/movie.nfo", `<?xml version="1.0" encoding="UTF-8"?>
<movie><title>Unser Sommer in Ligurien</title><year>2019</year>
<plot>Zwei Wochen am Meer.</plot><genre>Dokumentation</genre><mpaa>FSK 0</mpaa></movie>`)
	textDatei(t, wurzel, "Mein Urlaub (2019)/poster.jpg", "BILD1")
	textDatei(t, wurzel, "Mein Urlaub (2019)/fanart.png", "BILD2")
	textDatei(t, wurzel, "Mein Urlaub (2019)/clearlogo.png", "BILD3")
	// NFO mit Kennung: TMDb holt genau diesen Eintrag.
	textDatei(t, wurzel, "Matrix (1999)/Matrix (1999).nfo", `<movie><title>Matrix</title>
<uniqueid type="tmdb" default="true">603</uniqueid><uniqueid type="imdb">tt0133093</uniqueid></movie>`)
	textDatei(t, wurzel, "Matrix (1999)/Matrix (1999)-poster.jpg", "MATRIX")
	// Ein „poster.jpg" direkt in der Bibliothek gehört keinem Film.
	textDatei(t, wurzel, "poster.jpg", "FALSCH")
	// Kaputte NFO: kein Abbruch.
	textDatei(t, wurzel, "Unbekannt (2005)/movie.nfo", "<movie><title>")

	lib, _ := st.CreateLibrary(ctx, "Filme", "movie", wurzel, "de")
	sc := New(st, nil)
	sc.BilderOrdner = bilder
	if _, err := sc.Scan(ctx, *lib); err != nil {
		t.Fatal(err)
	}
	finde := func(titel string) *store.Item {
		filme, _ := st.Query(ctx, store.Query{Libraries: []string{lib.ID}, Kinds: []string{"movie"}, Limit: 50})
		for i := range filme {
			if filme[i].Title == titel {
				it, _ := st.Item(ctx, filme[i].ID)
				return it
			}
		}
		t.Fatalf("%q fehlt: %v", titel, titles(filme))
		return nil
	}
	urlaub := finde("Unser Sommer in Ligurien")
	if urlaub.MatchState != "lokal" || urlaub.Overview != "Zwei Wochen am Meer." ||
		fmt.Sprint(urlaub.Genres) != "[Dokumentation]" || urlaub.AgeYears == nil || *urlaub.AgeYears != 0 {
		t.Errorf("NFO nicht übernommen: %+v", urlaub)
	}
	art, _ := st.ArtworkFor(ctx, []string{urlaub.ID})
	for _, k := range []string{"poster", "backdrop", "logo"} {
		name := art[urlaub.ID][k]
		if name == "" {
			t.Errorf("%s fehlt", k)
			continue
		}
		if _, err := os.Stat(filepath.Join(bilder, name)); err != nil {
			t.Errorf("%s nicht übernommen: %v", k, err)
		}
	}
	if !strings.HasSuffix(art[urlaub.ID]["backdrop"], ".png") {
		t.Errorf("Endung des Hintergrunds: %s", art[urlaub.ID]["backdrop"])
	}

	matrix := finde("Matrix")
	if matrix.MatchState != "pending" || matrix.IMDbID != "tt0133093" {
		t.Errorf("Matrix: %s %s", matrix.MatchState, matrix.IMDbID)
	}
	if id, _ := st.SourceID(ctx, matrix.ID, "tmdb"); id != "603" {
		t.Errorf("TMDb-Kennung %q", id)
	}
	if b, _ := os.ReadFile(filepath.Join(bilder, art2(t, st, matrix.ID, "poster"))); string(b) != "MATRIX" {
		t.Errorf("Matrix-Poster: %q", b)
	}
	ohne := finde("Filme ohne Ordner")
	if a, _ := st.ArtworkFor(ctx, []string{ohne.ID}); len(a[ohne.ID]) != 0 {
		t.Errorf("Poster aus der Bibliothek übernommen: %v", a[ohne.ID])
	}

	// Ein zweiter Durchlauf behält den Titel aus der NFO — er wird nicht
	// nach dem Dateinamen zurückbenannt.
	if _, err := sc.Scan(ctx, *lib); err != nil {
		t.Fatal(err)
	}
	if it, _ := st.Item(ctx, urlaub.ID); it.Title != "Unser Sommer in Ligurien" || it.MatchState != "lokal" {
		t.Errorf("nach zweitem Durchlauf: %q %s", it.Title, it.MatchState)
	}

	// Ein geändertes Bild kommt neu; eine geänderte NFO auch.
	zukunft := time.Now().Add(time.Hour)
	textDatei(t, wurzel, "Mein Urlaub (2019)/poster.jpg", "NEUES BILD")
	_ = os.Chtimes(filepath.Join(wurzel, "Mein Urlaub (2019)/poster.jpg"), zukunft, zukunft)
	textDatei(t, wurzel, "Mein Urlaub (2019)/movie.nfo", `<movie><title>Ligurien 2019</title><year>2019</year><plot>Neu.</plot></movie>`)
	_ = os.Chtimes(filepath.Join(wurzel, "Mein Urlaub (2019)/movie.nfo"), zukunft, zukunft)
	if _, err := sc.Scan(ctx, *lib); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(bilder, art2(t, st, urlaub.ID, "poster"))); string(b) != "NEUES BILD" {
		t.Errorf("geändertes Poster nicht übernommen: %q", b)
	}
	if it, _ := st.Item(ctx, urlaub.ID); it.Title != "Ligurien 2019" || it.Overview != "Neu." {
		t.Errorf("geänderte NFO nicht übernommen: %q %q", it.Title, it.Overview)
	}
}

func art2(t *testing.T, st *store.Store, id, kind string) string {
	t.Helper()
	a, _ := st.ArtworkFor(context.Background(), []string{id})
	return a[id][kind]
}

// 15. und 16. bei Serien: tvshow.nfo, Staffelposter, Folgenbild und -NFO.
func TestLokaleSerienMetadaten(t *testing.T) {
	ctx := t.Context()
	st := testStore(t)
	wurzel, bilder := t.TempDir(), t.TempDir()
	dateienAnlegen(t, wurzel,
		"Heimat/Staffel 1/Heimat - S01E01.mkv",
		"Heimat/Staffel 1/Heimat - S01E02.mkv",
		"Heimat/Specials/Heimat - S00E01.mkv",
	)
	textDatei(t, wurzel, "Heimat/tvshow.nfo", `<tvshow><title>Heimat – Eine deutsche Chronik</title>
<year>1984</year><plot>Hunsrück.</plot><uniqueid type="tvdb">78999</uniqueid></tvshow>`)
	textDatei(t, wurzel, "Heimat/poster.jpg", "SERIE")
	textDatei(t, wurzel, "Heimat/season01-poster.jpg", "STAFFEL1")
	textDatei(t, wurzel, "Heimat/season-specials-poster.jpg", "SPECIALS")
	textDatei(t, wurzel, "Heimat/Staffel 1/Heimat - S01E01-thumb.jpg", "FOLGE1")
	textDatei(t, wurzel, "Heimat/Staffel 1/Heimat - S01E01.nfo", `<episodedetails><title>Fernweh</title>
<season>1</season><episode>1</episode><plot>Paul kehrt heim.</plot><aired>1984-09-16</aired></episodedetails>`)

	lib, _ := st.CreateLibrary(ctx, "Serien", "series", wurzel, "de")
	sc := New(st, nil)
	sc.BilderOrdner = bilder
	if _, err := sc.Scan(ctx, *lib); err != nil {
		t.Fatal(err)
	}
	serien, _ := st.Query(ctx, store.Query{Libraries: []string{lib.ID}, Kinds: []string{"series"}, Limit: 5})
	if len(serien) != 1 {
		t.Fatalf("Serien: %v", titles(serien))
	}
	se := serien[0]
	// Ohne TMDb-Schlüssel genügt die vollständige NFO: „lokal", nicht
	// „wartet auf die Zuordnung" (mit Schlüssel siehe TestNFOMitKennungOhneSchluessel).
	if se.Title != "Heimat – Eine deutsche Chronik" || se.MatchState != "lokal" {
		t.Errorf("Serie: %q %s", se.Title, se.MatchState)
	}
	if id, _ := st.SourceID(ctx, se.ID, "tvdb"); id != "78999" {
		t.Errorf("TVDb %q", id)
	}
	lies := func(id, kind string) string {
		b, _ := os.ReadFile(filepath.Join(bilder, art2(t, st, id, kind)))
		return string(b)
	}
	if lies(se.ID, "poster") != "SERIE" {
		t.Error("Serienposter fehlt")
	}
	staffeln, _ := st.Children(ctx, se.ID)
	for _, s := range staffeln {
		want := map[int]string{0: "SPECIALS", 1: "STAFFEL1"}[s.SeasonNo]
		if got := lies(s.ID, "poster"); got != want {
			t.Errorf("Staffel %d: Poster %q", s.SeasonNo, got)
		}
		if s.SeasonNo != 1 {
			continue
		}
		folgen, _ := st.Children(ctx, s.ID)
		for _, f := range folgen {
			if f.EpisodeNo == 1 {
				if f.Title != "Fernweh" || f.Overview != "Paul kehrt heim." || f.Premiered != "1984-09-16" {
					t.Errorf("Folge 1: %+v", f)
				}
				if lies(f.ID, "poster") != "FOLGE1" {
					t.Error("Folgenbild fehlt")
				}
			}
		}
	}
	// Zweiter Durchlauf: dieselbe Serie, nicht eine zweite „Heimat".
	if _, err := sc.Scan(ctx, *lib); err != nil {
		t.Fatal(err)
	}
	if serien, _ := st.Query(ctx, store.Query{Libraries: []string{lib.ID}, Kinds: []string{"series"}, Limit: 5}); len(serien) != 1 {
		t.Errorf("nach zweitem Durchlauf: %v", titles(serien))
	}
}
