// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package scan

// Ein Film in Teilen, der durch eine einzige Datei ersetzt wird (DVD in
// MKV umgewandelt, CD1/CD2 von Hand zusammengefügt): Die alten Teile 2, 3
// … standen bisher für immer als „fehlend" am Film.

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestDVDUmgewandeltKeineFehlendenTeile(t *testing.T) {
	root := t.TempDir()
	for _, f := range []string{"VIDEO_TS.VOB", "VTS_01_0.VOB"} {
		dateiMitGroesse(t, root, "Heidi (2015)/VIDEO_TS/"+f, 10)
	}
	for _, f := range []string{"VTS_01_1.VOB", "VTS_01_2.VOB", "VTS_01_3.VOB"} {
		dateiMitGroesse(t, root, "Heidi (2015)/VIDEO_TS/"+f, 1000)
	}
	st := faelleStore(t)
	ctx := context.Background()
	lib, _ := st.CreateLibrary(ctx, "Filme", "movie", root, "de")
	sc := New(st, nil)
	if _, err := sc.Scan(ctx, *lib); err != nil {
		t.Fatal(err)
	}
	vorher := filmeMitTeilen(t, st, lib.ID)
	if len(vorher["Heidi"]) != 3 {
		t.Fatalf("vorher: %v, erwartet Heidi in 3 Teilen", vorher)
	}
	// „In MKV umwandeln": die MKV daneben, die Scheibe bleibt liegen.
	dateiMitGroesse(t, root, "Heidi (2015)/Heidi (2015).mkv", 2900)
	if _, err := sc.Scan(ctx, *lib); err != nil {
		t.Fatal(err)
	}
	if fehlend, _ := st.MissingFiles(ctx); len(fehlend) != 0 {
		var namen []string
		for _, f := range fehlend {
			namen = append(namen, filepath.Base(f.Path))
		}
		t.Errorf("fehlende Dateien: %v, erwartet keine", namen)
	}
	if nach := filmeMitTeilen(t, st, lib.ID); len(nach["Heidi"]) != 1 {
		t.Errorf("nachher: %v", nach)
	}
}

// CD1/CD2 von Hand zu einer Datei zusammengefügt, die Ordner gelöscht.
func TestCDTeileVonHandZusammengefuegt(t *testing.T) {
	root := t.TempDir()
	dateiMitGroesse(t, root, "Doktor Schiwago (1965)/CD1/Doktor Schiwago (1965).avi", 700)
	dateiMitGroesse(t, root, "Doktor Schiwago (1965)/CD2/Doktor Schiwago (1965).avi", 700)
	st := faelleStore(t)
	ctx := context.Background()
	lib, _ := st.CreateLibrary(ctx, "Filme", "movie", root, "de")
	sc := New(st, nil)
	if _, err := sc.Scan(ctx, *lib); err != nil {
		t.Fatal(err)
	}
	if v := filmeMitTeilen(t, st, lib.ID); len(v["Doktor Schiwago"]) != 2 {
		t.Fatalf("vorher: %v", v)
	}
	os.RemoveAll(filepath.Join(root, "Doktor Schiwago (1965)/CD1"))
	os.RemoveAll(filepath.Join(root, "Doktor Schiwago (1965)/CD2"))
	dateiMitGroesse(t, root, "Doktor Schiwago (1965)/Doktor Schiwago (1965).mkv", 1390)
	if _, err := sc.Scan(ctx, *lib); err != nil {
		t.Fatal(err)
	}
	if fehlend, _ := st.MissingFiles(ctx); len(fehlend) != 0 {
		t.Errorf("%d fehlende Dateien, erwartet keine", len(fehlend))
	}
}

// Gegenprobe: Fehlen bei einem Film in drei Teilen Teil 2 und 3, ohne
// dass etwas sie ersetzt, bleiben sie als „fehlend" stehen (versehentlich
// gelöscht?) — Teil 1 allein deckt den Film nicht.
func TestFehlendeTeileOhneErsatzBleiben(t *testing.T) {
	root := t.TempDir()
	for _, f := range []string{"VTS_01_1.VOB", "VTS_01_2.VOB", "VTS_01_3.VOB"} {
		dateiMitGroesse(t, root, "Heidi (2015)/VIDEO_TS/"+f, 1000)
	}
	st := faelleStore(t)
	ctx := context.Background()
	lib, _ := st.CreateLibrary(ctx, "Filme", "movie", root, "de")
	sc := New(st, nil)
	if _, err := sc.Scan(ctx, *lib); err != nil {
		t.Fatal(err)
	}
	os.Remove(filepath.Join(root, "Heidi (2015)/VIDEO_TS/VTS_01_2.VOB"))
	os.Remove(filepath.Join(root, "Heidi (2015)/VIDEO_TS/VTS_01_3.VOB"))
	if _, err := sc.Scan(ctx, *lib); err != nil {
		t.Fatal(err)
	}
	fehlend, _ := st.MissingFiles(ctx)
	if len(fehlend) != 2 {
		t.Fatalf("%d fehlende Dateien, erwartet 2", len(fehlend))
	}
	angaben, _ := st.FehlendeAngaben(ctx, []string{fehlend[0].ID})
	if angaben[fehlend[0].ID].Ersatz {
		t.Errorf("Teil 1 allein gilt als Ersatz: %+v", angaben[fehlend[0].ID])
	}
}
