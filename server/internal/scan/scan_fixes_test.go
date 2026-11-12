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
