// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

import (
	"context"
	"testing"
	"time"
)

// TestNeueFolgenNurUngesehene: „4 neue Folgen" zählt nur, was das eigene
// Profil noch nicht gesehen hat – ein anderes Profil zählt weiter alle.
func TestNeueFolgenNurUngesehene(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	a, _ := s.CreateAccount(ctx, NewAccount{Name: "A", IsOwner: true})
	b, _ := s.CreateAccount(ctx, NewAccount{Name: "B"})
	lib, _ := s.CreateLibrary(ctx, "Serien", "tv", t.TempDir(), "de")
	serie, f, _ := testSerie(t, s, lib.ID, "Tiefsee")
	seit := time.Now().Add(-time.Hour)
	zahl := func(acc string) int {
		t.Helper()
		m, err := s.NeueFolgenSeit(ctx, acc, []string{serie}, seit)
		if err != nil {
			t.Fatal(err)
		}
		return m[serie]
	}
	if n := zahl(a.ID); n != 5 { // vier Folgen und das Special
		t.Fatalf("vorher %d neue Folgen, will 5", n)
	}
	for _, id := range f[:3] {
		if err := s.MarkFinished(ctx, a.ID, id, 2_400_000); err != nil {
			t.Fatal(err)
		}
	}
	if n := zahl(a.ID); n != 2 {
		t.Fatalf("nach drei gesehenen %d neue Folgen, will 2", n)
	}
	if n := zahl(b.ID); n != 5 {
		t.Fatalf("anderes Profil: %d neue Folgen, will 5", n)
	}
}
