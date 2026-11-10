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

// Ein Verweis in den Datenordner (etwa auf die Datenbank) wird nicht
// aufgenommen. Verweise auf eine andere Freigabe schon — auf einem NAS
// ist das üblich.
func TestVerweisInDatenordner(t *testing.T) {
	alt := geschuetzt.Load()
	t.Cleanup(func() { geschuetzt.Store(alt) })

	daten := t.TempDir()
	DatenordnerSchuetzen(daten)
	geheim := filepath.Join(daten, "lumen.db")
	if err := os.WriteFile(geheim, []byte("geheim"), 0o600); err != nil {
		t.Fatal(err)
	}
	andere := t.TempDir() // eine zweite Freigabe
	fremd := filepath.Join(andere, "film.mp3")
	_ = os.WriteFile(fremd, []byte("ton"), 0o644)

	medien := t.TempDir()
	echt := filepath.Join(medien, "Album", "01 - echt.mp3")
	_ = os.MkdirAll(filepath.Dir(echt), 0o755)
	_ = os.WriteFile(echt, []byte("ton"), 0o644)
	if err := os.Symlink(geheim, filepath.Join(medien, "Album", "02 - datenbank.mp3")); err != nil {
		t.Skip("keine Verweise möglich:", err)
	}
	_ = os.Symlink(fremd, filepath.Join(medien, "Album", "03 - andere-freigabe.mp3"))

	ergebnis := map[string]bool{}
	_ = filepath.WalkDir(medien, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			ergebnis[d.Name()] = VerweisInDatenordner(p, d)
		}
		return nil
	})
	want := map[string]bool{"01 - echt.mp3": false, "02 - datenbank.mp3": true, "03 - andere-freigabe.mp3": false}
	for name, w := range want {
		if ergebnis[name] != w {
			t.Errorf("%s: gesperrt=%v, erwartet %v", name, ergebnis[name], w)
		}
	}
}
