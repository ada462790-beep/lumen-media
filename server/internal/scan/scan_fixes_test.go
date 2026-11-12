// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package scan

// Regressionstests zu den Befunden aus dem Scan-Testbericht (Folgen ohne
// Nummer, Aussetzer der Freigabe, umbenannte und defekte Dateien …).

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"lumen/internal/store"
)

// folgenDerSerie: je Folge „Nummer:Titel:Dateien".
func folgenDerSerie(t *testing.T, st *store.Store, libID string) (nummern []int, ohneDatei int) {
	t.Helper()
	ctx := context.Background()
	serien, _ := st.Query(ctx, store.Query{Libraries: []string{libID}, Kinds: []string{"series"}, Limit: 50})
	for _, se := range serien {
		staffeln, _ := st.Children(ctx, se.ID)
		for _, sa := range staffeln {
			folgen, _ := st.Children(ctx, sa.ID)
			for _, f := range folgen {
				nummern = append(nummern, f.EpisodeNo)
				if d, _ := st.FilesFor(ctx, f.ID); len(d) == 0 {
					ohneDatei++
				}
			}
		}
	}
	sort.Ints(nummern)
	return nummern, ohneDatei
}

// B1: Folgen ohne Nummer im Namen bekamen bei jedem Durchlauf neue
// Nummern — nach sieben Durchläufen 21 statt 3 Folgen, 18 davon leer.
func TestFolgenOhneNummerBleibenBeiIhrerNummer(t *testing.T) {
	ctx := context.Background()
	st := testStore(t)
	wurzel := t.TempDir()
	for _, n := range []string{"Pilot.mkv", "Der Anfang.mkv", "Das Ende.mkv"} {
		dateiMitGroesse(t, wurzel, filepath.Join("Ohne Nummern", "Staffel 1", n), 10)
	}
	lib, _ := st.CreateLibrary(ctx, "Serien", "series", wurzel, "de")
	sc := New(st, nil)
	var letzte *Result
	for i := 0; i < 4; i++ {
		res, err := sc.Scan(ctx, *lib)
		if err != nil {
			t.Fatal(err)
		}
		letzte = res
	}
	nummern, leer := folgenDerSerie(t, st, lib.ID)
	if len(nummern) != 3 || leer != 0 {
		t.Fatalf("nach 4 Durchläufen Folgen %v, %d ohne Datei — erwartet 3 Folgen mit Datei", nummern, leer)
	}
	if letzte.Added != 0 {
		t.Errorf("ein Durchlauf ohne Änderungen meldet %d neu", letzte.Added)
	}
}

// Leere Folgen, die ältere Fassungen hinterlassen haben, verschwinden —
// der Stand der Leute wandert zur Folge mit derselben Datei (gleicher
// Titel in derselben Staffel).
func TestLeereFolgenWerdenAufgeraeumt(t *testing.T) {
	ctx := context.Background()
	st := testStore(t)
	wurzel := t.TempDir()
	dateiMitGroesse(t, wurzel, filepath.Join("Ohne Nummern", "Staffel 1", "Pilot.mkv"), 10)
	lib, _ := st.CreateLibrary(ctx, "Serien", "series", wurzel, "de")
	if _, err := New(st, nil).Scan(ctx, *lib); err != nil {
		t.Fatal(err)
	}
	serien, _ := st.Query(ctx, store.Query{Libraries: []string{lib.ID}, Kinds: []string{"series"}, Limit: 5})
	staffeln, _ := st.Children(ctx, serien[0].ID)
	folgen, _ := st.Children(ctx, staffeln[0].ID)
	echt := folgen[0]
	// So hinterliess es die alte Fassung: eine leere Folge „Pilot" mit
	// dem Gesehen-Haken.
	geist := &store.Item{LibraryID: lib.ID, Kind: "episode", ParentID: staffeln[0].ID,
		Title: "Pilot", SeasonNo: 1, EpisodeNo: 7, MatchState: "pending"}
	if err := st.UpsertItem(ctx, geist); err != nil {
		t.Fatal(err)
	}
	acc, err := st.CreateAccount(ctx, store.NewAccount{Name: "Mia"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.MarkFinished(ctx, acc.ID, geist.ID, 1000); err != nil {
		t.Fatal(err)
	}
	n, err := st.FolgenOhneDatei(ctx)
	if err != nil || n != 1 {
		t.Fatalf("FolgenOhneDatei = %d, %v — erwartet 1", n, err)
	}
	if _, err := st.Item(ctx, geist.ID); err == nil {
		t.Fatal("die leere Folge steht noch da")
	}
	p, err := st.Progress(ctx, acc.ID, echt.ID)
	if err != nil || p == nil || !p.Finished {
		t.Fatalf("der Gesehen-Haken ist nicht zur Folge mit der Datei gewandert: %+v %v", p, err)
	}
}

// lesefehlerIn täuscht für die genannten Ordner (relativ zur Wurzel)
// einen Lesefehler vor, wie ihn WalkDir bei fehlenden Rechten meldet.
func lesefehlerIn(t *testing.T, wurzel string, ordner ...string) {
	t.Helper()
	kaputt := map[string]bool{}
	for _, o := range ordner {
		kaputt[filepath.Join(wurzel, o)] = true
	}
	alt := walkDir
	walkDir = func(root string, fn fs.WalkDirFunc) error {
		return filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err == nil && d.IsDir() && kaputt[p] {
				if r := fn(p, d, nil); r != nil {
					return r
				}
				if r := fn(p, d, fs.ErrPermission); r != nil {
					return r
				}
				return filepath.SkipDir
			}
			return fn(p, d, err)
		})
	}
	t.Cleanup(func() { walkDir = alt })
}

// B3: Dateien in einem Ordner, der sich nicht lesen lässt (ACL auf dem
// NAS geändert), wurden als fehlend vermerkt und waren gesperrt.
func TestUnlesbarerOrdnerVermerktNichtsAlsFehlend(t *testing.T) {
	ctx := context.Background()
	st := testStore(t)
	wurzel := t.TempDir()
	for _, f := range []string{"Sammlungen/Heat (1995)/Heat (1995).mkv",
		"Sammlungen/Harry Potter 1 (2001)/Harry Potter 1 (2001).mkv", "Up (2009)/Up (2009).mkv"} {
		dateiMitGroesse(t, wurzel, f, 10)
	}
	lib, _ := st.CreateLibrary(ctx, "Filme", "movie", wurzel, "de")
	if _, err := New(st, nil).Scan(ctx, *lib); err != nil {
		t.Fatal(err)
	}
	lesefehlerIn(t, wurzel, "Sammlungen")
	res, err := New(st, nil).Scan(ctx, *lib)
	if err != nil {
		t.Fatal(err)
	}
	if res.Missing != 0 {
		t.Fatalf("%d Dateien als fehlend vermerkt, obwohl ihr Ordner nur nicht lesbar war", res.Missing)
	}
	if n, _ := st.FehlendZahl(ctx); n != 0 {
		t.Fatalf("FehlendZahl = %d", n)
	}
}

// B2: Fällt die Freigabe mitten im Durchlauf aus, gilt nicht der Rest der
// Bibliothek als fehlend — der Durchlauf zählt als unvollständig.
func TestAussetzerDerFreigabeVermerktNichtsAlsFehlend(t *testing.T) {
	ctx := context.Background()
	st := testStore(t)
	wurzel := t.TempDir()
	var ordner []string
	for i := 0; i < 40; i++ {
		o := fmt.Sprintf("Film %02d (20%02d)", i, i%25)
		ordner = append(ordner, o)
		dateiMitGroesse(t, wurzel, filepath.Join(o, o+".mkv"), 10+i)
	}
	lib, _ := st.CreateLibrary(ctx, "Filme", "movie", wurzel, "de")
	if _, err := New(st, nil).Scan(ctx, *lib); err != nil {
		t.Fatal(err)
	}
	// Ab dem fünften Ordner ist die Freigabe weg (ENOENT/EIO beim Lesen).
	lesefehlerIn(t, wurzel, ordner[5:]...)
	if _, err := New(st, nil).Scan(ctx, *lib); err == nil {
		t.Fatal("ein Durchlauf mit 35 Lesefehlern soll als unvollständig gelten")
	}
	if n, _ := st.FehlendZahl(ctx); n != 0 {
		t.Fatalf("%d Dateien als fehlend vermerkt", n)
	}

	// Ist der Stammordner am Ende weg, wird ebenso nichts vermerkt.
	walkDir = filepath.WalkDir
	weg := wurzel + "-weg"
	alt := walkDir
	walkDir = func(root string, fn fs.WalkDirFunc) error {
		err := filepath.WalkDir(root, fn)
		_ = os.Rename(wurzel, weg)
		return err
	}
	t.Cleanup(func() { walkDir = alt; _ = os.Rename(weg, wurzel) })
	if err := os.Remove(filepath.Join(wurzel, ordner[0], ordner[0]+".mkv")); err != nil {
		t.Fatal(err)
	}
	if _, err := New(st, nil).Scan(ctx, *lib); err == nil {
		t.Fatal("verschwindet der Stammordner während des Durchlaufs, soll er scheitern")
	}
	if n, _ := st.FehlendZahl(ctx); n != 0 {
		t.Fatalf("%d Dateien als fehlend vermerkt, obwohl die Freigabe weg war", n)
	}
}

// B7: Umlaute aus macOS-Dateinamen (NFD) — der Titel liess sich nicht
// suchen, und eine NFC- und eine NFD-Kopie desselben Films wurden zwei.
func TestZerlegteUmlauteWerdenZusammengesetzt(t *testing.T) {
	ctx := context.Background()
	st := testStore(t)
	wurzel := t.TempDir()
	nfd := "Die Schöne und das Biest (1991)"
	nfc := "Die Schöne und das Biest (1991)"
	dateiMitGroesse(t, wurzel, filepath.Join(nfd, nfd+".mkv"), 10)
	dateiMitGroesse(t, wurzel, filepath.Join(nfc, nfc+" - 720p.avi"), 20)
	lib, _ := st.CreateLibrary(ctx, "Filme", "movie", wurzel, "de")
	for i := 0; i < 2; i++ {
		if _, err := New(st, nil).Scan(ctx, *lib); err != nil {
			t.Fatal(err)
		}
	}
	alle, _ := st.Query(ctx, store.Query{Libraries: []string{lib.ID}, Limit: 10})
	if len(alle) != 1 || alle[0].Title != "Die Schöne und das Biest" {
		t.Fatalf("Titel: %v — erwartet ein Film in NFC", titles(alle))
	}
	for _, q := range []string{"Schöne", "Schöne", "schoene"} {
		treffer, _ := st.Query(ctx, store.Query{Libraries: []string{lib.ID}, Text: q, Limit: 10})
		if len(treffer) != 1 {
			t.Errorf("Suche %q findet %d Titel", q, len(treffer))
		}
	}
}

// B8: „CD1/"- und „CD2/"-Ordner wurden zwei Fassungen eines Films ohne
// Jahr — gemeint ist ein Film in zwei Teilen.
func TestTeileInCDOrdnern(t *testing.T) {
	ctx := context.Background()
	st := testStore(t)
	wurzel := t.TempDir()
	dateiMitGroesse(t, wurzel, "Doktor Schiwago (1965)/CD1/Doktor Schiwago.avi", 10)
	dateiMitGroesse(t, wurzel, "Doktor Schiwago (1965)/CD2/Doktor Schiwago.avi", 11)
	lib, _ := st.CreateLibrary(ctx, "Filme", "movie", wurzel, "de")
	if _, err := New(st, nil).Scan(ctx, *lib); err != nil {
		t.Fatal(err)
	}
	alle, _ := st.Query(ctx, store.Query{Libraries: []string{lib.ID}, Limit: 10})
	if len(alle) != 1 || alle[0].Title != "Doktor Schiwago" || alle[0].Year != 1965 {
		t.Fatalf("Titel %v, Jahr %d — erwartet ein Film „Doktor Schiwago“ (1965)", titles(alle), alle[0].Year)
	}
	d, _ := st.FilesFor(ctx, alle[0].ID)
	if len(d) != 2 || d[0].PartNo != 1 || d[1].PartNo != 2 {
		t.Fatalf("Teile: %+v", d)
	}
}

// Emby-Bericht B1: Fassungen und Teile in einem Ordner mit movie.nfo,
// deren NFO-Titel anders lautet als der Dateiname, wurden getrennte Filme.
func TestNFOTitelTrenntFassungenNicht(t *testing.T) {
	ctx := context.Background()
	st := testStore(t)
	wurzel := t.TempDir()
	dateiMitGroesse(t, wurzel, "The Matrix (1999)/The Matrix (1999) - 1080p.mkv", 10)
	dateiMitGroesse(t, wurzel, "The Matrix (1999)/The Matrix (1999) - 4K.mkv", 20)
	textDatei(t, wurzel, "The Matrix (1999)/movie.nfo",
		`<movie><title>Matrix</title><originaltitle>The Matrix</originaltitle><uniqueid type="tmdb">603</uniqueid></movie>`)
	dateiMitGroesse(t, wurzel, "Die Hard (1988)/Die Hard (1988)-pt1.mkv", 10)
	dateiMitGroesse(t, wurzel, "Die Hard (1988)/Die Hard (1988)-pt2.mkv", 11)
	textDatei(t, wurzel, "Die Hard (1988)/movie.nfo", `<movie><title>Stirb langsam</title></movie>`)
	lib, _ := st.CreateLibrary(ctx, "Filme", "movie", wurzel, "de")
	for i := 0; i < 2; i++ {
		if _, err := New(st, nil).Scan(ctx, *lib); err != nil {
			t.Fatal(err)
		}
	}
	alle, _ := st.Query(ctx, store.Query{Libraries: []string{lib.ID}, Limit: 10})
	if len(alle) != 2 {
		t.Fatalf("%d Filme statt 2: %v", len(alle), titles(alle))
	}
	for _, it := range alle {
		if d, _ := st.FilesFor(ctx, it.ID); len(d) != 2 {
			t.Errorf("%s: %d Dateien statt 2", it.Title, len(d))
		}
	}
}

// Was ältere Fassungen schon zerlegt haben, wird wieder ein Film, wenn
// beide Einträge im selben Ordner dieselbe Kennung tragen.
func TestZerlegterFilmImOrdnerWirdGeheilt(t *testing.T) {
	ctx := context.Background()
	st := testStore(t)
	wurzel := t.TempDir()
	a := filepath.Join(wurzel, "The Matrix (1999)", "The Matrix (1999) - 1080p.mkv")
	b := filepath.Join(wurzel, "The Matrix (1999)", "The Matrix (1999) - 4K.mkv")
	dateiMitGroesse(t, wurzel, "The Matrix (1999)/The Matrix (1999) - 1080p.mkv", 10)
	dateiMitGroesse(t, wurzel, "The Matrix (1999)/The Matrix (1999) - 4K.mkv", 20)
	textDatei(t, wurzel, "The Matrix (1999)/movie.nfo",
		`<movie><title>Matrix</title><uniqueid type="tmdb">603</uniqueid></movie>`)
	lib, _ := st.CreateLibrary(ctx, "Filme", "movie", wurzel, "de")
	var ids []string
	for _, p := range []string{a, b} {
		it := &store.Item{LibraryID: lib.ID, Kind: "movie", Title: "Matrix", Year: 1999, MatchState: "pending"}
		_ = st.UpsertItem(ctx, it)
		_ = st.UpsertFile(ctx, &store.MediaFile{ItemID: it.ID, Path: p, Container: "mkv"})
		_ = st.SetSourceID(ctx, it.ID, "tmdb", "603")
		ids = append(ids, it.ID)
	}
	acc, _ := st.CreateAccount(ctx, store.NewAccount{Name: "Mia"})
	_ = st.AddToWatchlist(ctx, acc.ID, ids[1])
	if _, err := New(st, nil).Scan(ctx, *lib); err != nil {
		t.Fatal(err)
	}
	alle, _ := st.Query(ctx, store.Query{Libraries: []string{lib.ID}, Limit: 10})
	if len(alle) != 1 {
		t.Fatalf("%d Filme statt 1: %v", len(alle), titles(alle))
	}
	if d, _ := st.FilesFor(ctx, alle[0].ID); len(d) != 2 {
		t.Fatalf("%d Dateien statt 2", len(d))
	}
	if w, _ := st.WatchlistIDs(ctx, acc.ID); len(w) != 1 || w[0] != alle[0].ID {
		t.Error("die Merkliste ist nicht mitgewandert")
	}
}

// groessenProbe misst eine Laufzeit, die aus der Grösse folgt; Dateien
// mit „kaputt" im Namen lassen sich nicht messen.
type groessenProbe struct {
	mu sync.Mutex
	n  int
}

func (g *groessenProbe) Probe(ctx context.Context, path string) (*Probe, error) {
	g.mu.Lock()
	g.n++
	g.mu.Unlock()
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if fi.Size() == 0 || strings.Contains(filepath.Base(path), "Kaputt") {
		return nil, errors.New("ffprobe " + path + ": Invalid data found when processing input")
	}
	return &Probe{DurationMS: fi.Size() * 1000, Width: 1920, Height: 1080, VideoCodec: "h264"}, nil
}

func (g *groessenProbe) zahl() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.n
}

// B4: Ein umbenannter Film behält seinen Eintrag samt Gesehen-Haken und
// Merkliste — kein zweiter Film, keine Filmreihe „Alien · 2 Filme". Ein
// gelöschter verschwindet nach der Frist aus der Mediathek.
func TestUmbenannterFilmBehaeltSeinenEintrag(t *testing.T) {
	ctx := context.Background()
	st := testStore(t)
	wurzel := t.TempDir()
	dateiMitGroesse(t, wurzel, "Alien (1979)/Alien (1979).mkv", 117)
	dateiMitGroesse(t, wurzel, "Matrix (1999)/Matrix (1999).avi", 136)
	dateiMitGroesse(t, wurzel, "Up (2009)/Up (2009).mkv", 96)
	lib, _ := st.CreateLibrary(ctx, "Filme", "movie", wurzel, "de")
	sc := New(st, &groessenProbe{})
	if _, err := sc.Scan(ctx, *lib); err != nil {
		t.Fatal(err)
	}
	alle, _ := st.Query(ctx, store.Query{Libraries: []string{lib.ID}, Limit: 10})
	var alien string
	for _, it := range alle {
		if it.Title == "Alien" {
			alien = it.ID
		}
	}
	acc, _ := st.CreateAccount(ctx, store.NewAccount{Name: "Mia"})
	_ = st.MarkFinished(ctx, acc.ID, alien, 1000)
	_ = st.AddToWatchlist(ctx, acc.ID, alien)

	neu := filepath.Join(wurzel, "Alien - Das unheimliche Wesen aus einer fremden Welt (1979)")
	if err := os.Rename(filepath.Join(wurzel, "Alien (1979)"), neu); err != nil {
		t.Fatal(err)
	}
	_ = os.Rename(filepath.Join(neu, "Alien (1979).mkv"), filepath.Join(neu, filepath.Base(neu)+".mkv"))
	_ = os.Rename(filepath.Join(wurzel, "Matrix (1999)", "Matrix (1999).avi"), filepath.Join(wurzel, "Matrix (1999)", "Matrix (1999) German.avi"))
	_ = os.RemoveAll(filepath.Join(wurzel, "Up (2009)"))

	res, err := sc.Scan(ctx, *lib)
	if err != nil {
		t.Fatal(err)
	}
	if res.Renamed != 2 || res.Missing != 1 || res.Added != 0 {
		t.Errorf("Ergebnis %+v — erwartet 2 umbenannt, 1 fehlt, 0 neu", res)
	}
	alle, _ = st.Query(ctx, store.Query{Libraries: []string{lib.ID}, Limit: 10})
	if len(alle) != 3 {
		t.Fatalf("%d Titel statt 3: %v", len(alle), titles(alle))
	}
	if d, _ := st.FilesFor(ctx, alien); len(d) != 1 || d[0].Missing {
		t.Fatalf("Alien: Dateien %+v", d)
	}
	if p, _ := st.Progress(ctx, acc.ID, alien); p == nil || !p.Finished {
		t.Error("der Gesehen-Haken ging verloren")
	}
	if w, _ := st.WatchlistIDs(ctx, acc.ID); len(w) != 1 || w[0] != alien {
		t.Error("die Merkliste ging verloren")
	}
	for _, it := range alle {
		if it.Title == "Matrix" {
			if d, _ := st.FilesFor(ctx, it.ID); len(d) != 1 {
				t.Errorf("Matrix hat %d Dateien (die alte blieb als fehlend stehen)", len(d))
			}
		}
	}
	if _, err := st.ReihenNeuBilden(ctx); err != nil {
		t.Fatal(err)
	}
	if r, _ := st.Reihen(ctx, false); len(r) != 0 {
		t.Errorf("aus dem Umbenennen wurde eine Filmreihe: %+v", r)
	}

	// „Up" fehlt: einen Tag lang noch sichtbar, danach nicht mehr.
	sicht := store.Query{Libraries: []string{lib.ID}, Limit: 10, Freigabe: true}
	if n, _ := st.Count(ctx, sicht); n != 3 {
		t.Fatalf("gleich nach dem Löschen %d sichtbar, erwartet 3", n)
	}
	if _, err := st.DB().Exec(`UPDATE item SET fehlt_seit = fehlt_seit - 90000 WHERE fehlt_seit IS NOT NULL`); err != nil {
		t.Fatal(err)
	}
	if n, _ := st.Count(ctx, sicht); n != 2 {
		t.Fatalf("nach der Frist %d sichtbar, erwartet 2", n)
	}
	// Kommt die Datei zurück, ist der Titel wieder da.
	dateiMitGroesse(t, wurzel, "Up (2009)/Up (2009).mkv", 96)
	if _, err := sc.Scan(ctx, *lib); err != nil {
		t.Fatal(err)
	}
	if n, _ := st.Count(ctx, sicht); n != 3 {
		t.Fatalf("nach der Rückkehr %d sichtbar, erwartet 3", n)
	}
}

// B5: Kaputte, leere und gesperrte Dateien sind defekt: nicht abspielbar,
// mit Grund vermerkt — und unverändert nicht bei jedem Durchlauf neu
// gemessen.
func TestDefekteDateien(t *testing.T) {
	ctx := context.Background()
	st := testStore(t)
	wurzel := t.TempDir()
	dateiMitGroesse(t, wurzel, "Kaputt (2019).mkv", 50)
	dateiMitGroesse(t, wurzel, "Leere Datei (2020).mkv", 0)
	dateiMitGroesse(t, wurzel, "Heil (2021).mkv", 50)
	lib, _ := st.CreateLibrary(ctx, "Filme", "movie", wurzel, "de")
	p := &groessenProbe{}
	sc := New(st, p)
	if _, err := sc.Scan(ctx, *lib); err != nil {
		t.Fatal(err)
	}
	vorher := p.zahl()
	if _, err := sc.Scan(ctx, *lib); err != nil {
		t.Fatal(err)
	}
	if p.zahl() != vorher {
		t.Errorf("unveränderte defekte Dateien wurden neu gemessen (%d → %d)", vorher, p.zahl())
	}
	defekt, _ := st.DefekteDateien(ctx, 10)
	gruende := map[string]string{}
	for _, d := range defekt {
		gruende[filepath.Base(d.Path)] = d.Grund
	}
	if gruende["Kaputt (2019).mkv"] != DefektKaputt || gruende["Leere Datei (2020).mkv"] != DefektLeer || len(gruende) != 2 {
		t.Fatalf("defekt: %v", gruende)
	}
	for _, d := range defekt {
		if _, err := st.BestFileFor(ctx, d.ItemID); err == nil {
			t.Errorf("%s wird zum Abspielen angeboten", d.Path)
		}
	}
	// Ist die Datei fertig kopiert, ist sie nicht mehr defekt.
	dateiMitGroesse(t, wurzel, "Leere Datei (2020).mkv", 30)
	if _, err := sc.Scan(ctx, *lib); err != nil {
		t.Fatal(err)
	}
	if defekt, _ = st.DefekteDateien(ctx, 10); len(defekt) != 1 {
		t.Fatalf("nach dem Kopieren noch %d defekt", len(defekt))
	}
}

// B9: Verlinkte Ordner wurden still übersprungen. Jetzt folgt der
// Durchlauf ihnen — aber nicht in eine Schleife und nicht in Lumens
// Datenordner.
func TestVerlinkteOrdner(t *testing.T) {
	ctx := context.Background()
	st := testStore(t)
	wurzel := t.TempDir()
	draussen := t.TempDir()
	daten := t.TempDir()
	dateiMitGroesse(t, wurzel, "Heat (1995)/Heat (1995).mkv", 10)
	dateiMitGroesse(t, draussen, "Verlinkt (2015)/Verlinkt (2015).mkv", 10)
	dateiMitGroesse(t, daten, "Geheim (2000)/Geheim (2000).mkv", 10)
	for ziel, name := range map[string]string{
		filepath.Join(draussen, "Verlinkt (2015)"): "Verlinkt (2015)",
		wurzel:                                "Schleife",
		filepath.Join(daten, "Geheim (2000)"): "Geheim (2000)",
	} {
		if err := os.Symlink(ziel, filepath.Join(wurzel, name)); err != nil {
			t.Fatal(err)
		}
	}
	DatenordnerSchuetzen(daten)
	t.Cleanup(func() { geschuetzt.Store(nil) })
	lib, _ := st.CreateLibrary(ctx, "Filme", "movie", wurzel, "de")
	sc := New(st, nil)
	if _, err := sc.Scan(ctx, *lib); err != nil {
		t.Fatal(err)
	}
	alle, _ := st.Query(ctx, store.Query{Libraries: []string{lib.ID}, Limit: 10})
	if got := strings.Join(titles(alle), ","); got != "Heat,Verlinkt" {
		t.Fatalf("Titel %q — erwartet Heat und Verlinkt", got)
	}
	verweise := 0
	for _, a := range sc.Ausgelassen(lib.ID) {
		if a.Grund == AuslassVerweis {
			verweise = a.Anzahl
		}
	}
	if verweise != 2 {
		t.Errorf("%d Verweise als ausgelassen gelistet, erwartet 2 (Schleife, Datenordner)", verweise)
	}
}

// B10: Die Zählung „neu / aktualisiert" stimmt — Fassungen eines eben
// angelegten Films sind nicht „aktualisiert", wieder aufgetauchte Dateien
// schon, und bei Serien zählen die Folgen.
func TestZaehlungNeuUndAktualisiert(t *testing.T) {
	ctx := context.Background()
	st := testStore(t)
	wurzel := t.TempDir()
	dateiMitGroesse(t, wurzel, "Avatar (2009)/Avatar (2009) - 1080p.mkv", 10)
	dateiMitGroesse(t, wurzel, "Avatar (2009)/Avatar (2009) - 2160p.mkv", 20)
	dateiMitGroesse(t, wurzel, "Up (2009)/Up (2009).mkv", 30)
	lib, _ := st.CreateLibrary(ctx, "Filme", "movie", wurzel, "de")
	sc := New(st, nil)
	res, err := sc.Scan(ctx, *lib)
	if err != nil {
		t.Fatal(err)
	}
	if res.Added != 2 || res.Updated != 0 {
		t.Errorf("erster Durchlauf: %d neu, %d aktualisiert — erwartet 2 und 0", res.Added, res.Updated)
	}
	up := filepath.Join(wurzel, "Up (2009)")
	_ = os.Rename(up, up+".weg")
	if res, _ = sc.Scan(ctx, *lib); res.Missing != 1 {
		t.Fatalf("Up fehlt nicht: %+v", res)
	}
	_ = os.Rename(up+".weg", up)
	if res, _ = sc.Scan(ctx, *lib); res.Updated != 1 || res.Added != 0 {
		t.Errorf("wieder aufgetaucht: %+v — erwartet 1 aktualisiert", res)
	}

	serien := t.TempDir()
	for e := 1; e <= 3; e++ {
		dateiMitGroesse(t, serien, fmt.Sprintf("Dark (2017)/Staffel 1/Dark S01E0%d.mkv", e), 10)
	}
	slib, _ := st.CreateLibrary(ctx, "Serien", "series", serien, "de")
	if res, _ = sc.Scan(ctx, *slib); res.Added != 3 {
		t.Errorf("Serien: %d neu — erwartet 3 Folgen", res.Added)
	}
	dateiMitGroesse(t, serien, "Dark (2017)/Staffel 1/Dark S01E02.mkv", 99)
	if res, _ = sc.Scan(ctx, *slib); res.Updated != 1 || res.Added != 0 {
		t.Errorf("geänderte Folge: %+v — erwartet 1 aktualisiert", res)
	}
}

// gleichzeitigProbe zählt, wie viele Messungen gleichzeitig laufen.
type gleichzeitigProbe struct {
	mu          sync.Mutex
	jetzt, max  int
	reihenfolge []string
}

func (g *gleichzeitigProbe) Probe(ctx context.Context, path string) (*Probe, error) {
	g.mu.Lock()
	g.jetzt++
	g.max = max(g.max, g.jetzt)
	g.mu.Unlock()
	time.Sleep(20 * time.Millisecond)
	g.mu.Lock()
	g.jetzt--
	g.mu.Unlock()
	return &Probe{DurationMS: 60_000, Width: 1280, Height: 720, VideoCodec: "h264"}, nil
}

// Leistungsbericht 6: Beim ersten Durchlauf lief FFprobe streng
// nacheinander. Jetzt messen mehrere Helfer — eingetragen wird trotzdem
// alles, und in der Reihenfolge der Ordner.
func TestErsterDurchlaufMisstMitHelfern(t *testing.T) {
	ctx := context.Background()
	st := testStore(t)
	wurzel := t.TempDir()
	for i := 0; i < 12; i++ {
		dateiMitGroesse(t, wurzel, fmt.Sprintf("Film %02d (2001)/Film %02d (2001).mkv", i, i), 10)
	}
	for e := 1; e <= 8; e++ {
		dateiMitGroesse(t, wurzel, fmt.Sprintf("Dark (2017)/Staffel 1/Dark S01E%02d.mkv", e), 10)
	}
	lib, _ := st.CreateLibrary(ctx, "Gemischt", "movie", wurzel, "de")
	p := &gleichzeitigProbe{}
	start := time.Now()
	res, err := New(st, p).Scan(ctx, *lib)
	if err != nil {
		t.Fatal(err)
	}
	if p.max < 2 {
		t.Errorf("höchstens %d Messung(en) gleichzeitig — erwartet mindestens 2", p.max)
	}
	if res.Added != 20 {
		t.Errorf("%d neu, erwartet 20 (12 Filme, 8 Folgen)", res.Added)
	}
	t.Logf("20 Dateien à 20 ms in %v, bis zu %d gleichzeitig", time.Since(start).Round(time.Millisecond), p.max)
	alle, _ := st.Query(ctx, store.Query{Libraries: []string{lib.ID}, Kinds: []string{"movie"}, Limit: 50})
	for _, it := range alle {
		if d, _ := st.FilesFor(ctx, it.ID); len(d) != 1 || d[0].DurationMS != 60_000 {
			t.Errorf("%s: Messung fehlt: %+v", it.Title, d)
		}
	}
}
