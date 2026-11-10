// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package scan

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// Ein Verweis aus dem Medienordner hinaus (etwa auf die Datenbank) wird
// nicht aufgenommen; einer innerhalb schon.
func TestVerweisNachDraussen(t *testing.T) {
	daten := t.TempDir()
	geheim := filepath.Join(daten, "lumen.db")
	if err := os.WriteFile(geheim, []byte("geheim"), 0o600); err != nil {
		t.Fatal(err)
	}
	medien := t.TempDir()
	echt := filepath.Join(medien, "Album", "01 - echt.mp3")
	_ = os.MkdirAll(filepath.Dir(echt), 0o755)
	if err := os.WriteFile(echt, []byte("ton"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(geheim, filepath.Join(medien, "Album", "02 - draussen.mp3")); err != nil {
		t.Skip("keine Verweise möglich:", err)
	}
	if err := os.Symlink(echt, filepath.Join(medien, "Album", "03 - drinnen.mp3")); err != nil {
		t.Fatal(err)
	}
	ergebnis := map[string]bool{}
	_ = filepath.WalkDir(medien, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			ergebnis[d.Name()] = VerweisNachDraussen(medien, p, d)
		}
		return nil
	})
	want := map[string]bool{"01 - echt.mp3": false, "02 - draussen.mp3": true, "03 - drinnen.mp3": false}
	for name, w := range want {
		if ergebnis[name] != w {
			t.Errorf("%s: draussen=%v, erwartet %v", name, ergebnis[name], w)
		}
	}
}
