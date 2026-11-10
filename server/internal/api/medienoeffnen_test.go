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
	"testing"
)

// Wurde ein Verweis im Medienordner nach dem Durchlauf auf die Datenbank
// umgebogen, liefert Lumen sie trotzdem nicht aus.
func TestMedienOeffnenNieAusDemDatenordner(t *testing.T) {
	alt := datenOrdner.Load()
	t.Cleanup(func() { datenOrdner.Store(alt) })

	daten := t.TempDir()
	datenOrdnerSetzen(daten)
	db := filepath.Join(daten, "lumen.db")
	_ = os.WriteFile(db, []byte("geheim"), 0o600)
	medien := t.TempDir()
	lied := filepath.Join(medien, "lied.mp3")
	if err := os.Symlink(db, lied); err != nil {
		t.Skip("keine Verweise möglich:", err)
	}
	if f, err := medienOeffnen(lied); err == nil {
		f.Close()
		t.Fatal("Datenbank als Mediendatei ausgeliefert")
	}
	if f, err := medienOeffnen(db); err == nil {
		f.Close()
		t.Fatal("Datei im Datenordner direkt ausgeliefert")
	}
	echt := filepath.Join(medien, "echt.mp3")
	_ = os.WriteFile(echt, []byte("ton"), 0o644)
	f, err := medienOeffnen(echt)
	if err != nil {
		t.Fatalf("normale Mediendatei: %v", err)
	}
	f.Close()
}
