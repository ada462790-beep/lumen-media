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
	"strings"
	"testing"

	"lumen/internal/store"
)

// Was der Durchlauf mit Absicht auslässt, steht danach in einer Liste —
// mit Grund und Pfad, nicht im Protokoll.
func TestAusgelassen(t *testing.T) {
	root := t.TempDir()
	for _, f := range []string{
		"Die Bucht (2019)/Die Bucht (2019).mkv",
		"Die Bucht (2019)/Die Bucht (2019)-trailer.mkv",
		"Die Bucht (2019)/sample.mkv",
		"Die Bucht (2019)/Extras/Interview.mkv",
		"Die Bucht (2019)/Featurettes/Making of.mkv",
		"Heidi (2015)/Heidi.iso",
		"Alt/Film.rmvb",
		"Die Bucht (2019)/._Die Bucht (2019).mkv", // macOS — kein Fall
		"Die Bucht (2019)/poster.jpg",             // kein Video — kein Fall
		"@eaDir/Die Bucht (2019).mkv",             // Synology — kein Fall
	} {
		p := filepath.Join(root, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
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
	if s.Ausgelassen(lib.ID) != nil {
		t.Fatal("vor dem ersten Durchlauf schon eine Liste")
	}
	if _, err := s.Scan(ctx, *lib); err != nil {
		t.Fatal(err)
	}
	je := map[string]Auslassung{}
	for _, a := range s.Ausgelassen(lib.ID) {
		je[a.Grund] = a
	}
	pruef := func(grund string, anzahl int, enthaelt string) {
		t.Helper()
		a := je[grund]
		if a.Anzahl != anzahl {
			t.Errorf("%s: %d, erwartet %d (%v)", grund, a.Anzahl, anzahl, a.Pfade)
		}
		gefunden := false
		for _, p := range a.Pfade {
			gefunden = gefunden || strings.HasSuffix(p, enthaelt)
		}
		if !gefunden {
			t.Errorf("%s: %q fehlt in %v", grund, enthaelt, a.Pfade)
		}
	}
	pruef(AuslassBeiwerk, 2, "Die Bucht (2019)-trailer.mkv")
	pruef(AuslassOrdner, 2, "Extras")
	pruef(AuslassEndung, 2, "Heidi.iso")
	if _, da := je[AuslassFehler]; da {
		t.Errorf("unlesbar ohne Grund: %+v", je[AuslassFehler])
	}
	// Nichts davon im Protokoll.
	zeilen, _ := st.RecentLog(ctx, "", 200)
	for _, z := range zeilen {
		if strings.Contains(z.Message, root) || strings.Contains(z.Message, "trailer") {
			t.Errorf("Pfad im Protokoll: %q", z.Message)
		}
	}
}
