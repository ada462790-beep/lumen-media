// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func beiseiteMitFrist(t *testing.T, beiseite, film, pfad string) (string, error) {
	t.Helper()
	type erg struct {
		z   string
		err error
	}
	fertig := make(chan erg, 1)
	go func() {
		z, err := beiseiteName(beiseite, film, pfad)
		fertig <- erg{z, err}
	}()
	select {
	case e := <-fertig:
		return e.z, e.err
	case <-time.After(3 * time.Second):
		t.Fatalf("beiseiteName kehrt nicht zurück (Endlosschleife)")
		return "", nil
	}
}

// „CD2 - " vor einem Namen von 254 Bytes ergab einen zu langen Namen
// (ENAMETOOLONG) — beiseiteName zählte dann endlos weiter.
func TestBeiseiteNameLangerName(t *testing.T) {
	root := t.TempDir()
	film := filepath.Join(root, "Film")
	cd2 := filepath.Join(film, "CD2")
	if err := os.MkdirAll(cd2, 0o755); err != nil {
		t.Fatal(err)
	}
	name := strings.Repeat("ä", 125) + ".avi" // 254 Bytes: erlaubt
	pfad := filepath.Join(cd2, name)
	if err := os.WriteFile(pfad, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	beiseite := filepath.Join(film, ".lumen-teile")
	if err := os.MkdirAll(beiseite, 0o755); err != nil {
		t.Fatal(err)
	}
	z, err := beiseiteMitFrist(t, beiseite, film, pfad)
	if err != nil {
		t.Fatal(err)
	}
	b := filepath.Base(z)
	if len(b) > 255 || !utf8.ValidString(b) || !strings.HasSuffix(b, ".avi") {
		t.Fatalf("Name %q (%d Bytes)", b, len(b))
	}
	if err := os.Rename(pfad, z); err != nil {
		t.Fatalf("verschieben: %v", err)
	}
	// Ein zweiter Teil gleichen Namens bekommt eine Nummer, ebenfalls kurz genug.
	if err := os.WriteFile(pfad, []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	z2, err := beiseiteMitFrist(t, beiseite, film, pfad)
	if err != nil || z2 == z || len(filepath.Base(z2)) > 255 {
		t.Fatalf("zweiter Teil: %q %v", z2, err)
	}
}

// Jeder andere Fehler als „gibt es nicht" kommt zurück, statt endlos
// weiterzuzählen (hier: „.lumen-teile" ist eine Datei, kein Ordner).
func TestBeiseiteNameAndererFehler(t *testing.T) {
	root := t.TempDir()
	beiseite := filepath.Join(root, ".lumen-teile")
	if err := os.WriteFile(beiseite, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := beiseiteMitFrist(t, beiseite, root, filepath.Join(root, "Film.avi")); err == nil {
		t.Fatal("kein Fehler")
	}
}
