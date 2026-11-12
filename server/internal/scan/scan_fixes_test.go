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
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"testing"

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

var _ = os.Remove

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
