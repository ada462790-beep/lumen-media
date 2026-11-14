// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package scan

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"lumen/internal/store"
)

// DVD- und Blu-ray-Ordner stehen unter „Ausgelassene Dateien" (für „In
// MKV umwandeln"), einmal je Filmordner. Liegt die MKV schon darin, liest
// der Durchlauf die Scheibe nicht mehr und nennt sie auch nicht.
func TestScheibenOrdner(t *testing.T) {
	root := t.TempDir()
	for _, f := range []string{
		"Heidi (2015)/VIDEO_TS/VIDEO_TS.VOB",
		"Heidi (2015)/VIDEO_TS/VTS_01_0.VOB",
		"Heidi (2015)/VIDEO_TS/VTS_01_1.VOB",
		"Heidi (2015)/VIDEO_TS/VTS_01_2.VOB",
		"Avatar (2009)/BDMV/STREAM/00001.m2ts",
		"Avatar (2009)/BDMV/STREAM/00002.m2ts",
		"Die Bucht (2019)/VIDEO_TS/VTS_01_1.VOB",
		"Die Bucht (2019)/Die Bucht (2019).mkv",
	} {
		p := filepath.Join(root, f)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte("xx"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	st, err := store.Open(filepath.Join(t.TempDir(), "l.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	lib, _ := st.CreateLibrary(ctx, "Filme", "movie", root, "de")
	s := New(st, nil)
	if _, err := s.Scan(ctx, *lib); err != nil {
		t.Fatal(err)
	}
	var scheiben []string
	for _, a := range s.Ausgelassen(lib.ID) {
		if a.Grund == AuslassScheibe {
			scheiben = a.Pfade
		}
	}
	if len(scheiben) != 2 || scheiben[0] != filepath.Join(root, "Avatar (2009)") ||
		scheiben[1] != filepath.Join(root, "Heidi (2015)") {
		t.Errorf("Scheiben %v", scheiben)
	}
	// Die umgewandelte DVD: nur die MKV ist eingelesen.
	if id, err := st.ItemZuPfad(ctx, filepath.Join(root, "Die Bucht (2019)/VIDEO_TS/VTS_01_1.VOB")); err == nil && id != "" {
		t.Errorf("umgewandelte DVD trotzdem eingelesen")
	}
	if id, err := st.ItemZuPfad(ctx, filepath.Join(root, "Die Bucht (2019)/Die Bucht (2019).mkv")); err != nil || id == "" {
		t.Errorf("MKV der umgewandelten DVD fehlt: %v", err)
	}
	if id, err := st.ItemZuPfad(ctx, filepath.Join(root, "Heidi (2015)/VIDEO_TS/VTS_01_1.VOB")); err != nil || id == "" {
		t.Errorf("DVD-Ordner nicht mehr eingelesen: %v", err)
	}
	if ScheibeZiel("/m/Film (2001).iso", false) != "/m/Film (2001).mkv" ||
		ScheibeZiel("/m/Film (2001)", true) != "/m/Film (2001)/Film (2001).mkv" {
		t.Error("ScheibeZiel")
	}
}
