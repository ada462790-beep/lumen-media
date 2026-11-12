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
)

// Neue Profile ohne eigenes Bild bekommen verschiedene Farben; ein
// gewähltes Bild bleibt, wie es ist.
func TestNeueProfileBekommenVerschiedeneFarben(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	var farben, ids []string
	for _, n := range []string{"Anna", "Ben", "Cleo", "Dario", "Ella", "Fritz"} {
		a, err := st.CreateAccount(ctx, NewAccount{Name: n})
		if err != nil {
			t.Fatal(err)
		}
		farben = append(farben, a.Avatar)
		ids = append(ids, a.ID)
	}
	want := []string{"av-a", "av-b", "av-c", "av-d", "av-k", "av-a"}
	for i := range want {
		if farben[i] != want[i] {
			t.Fatalf("Farben %v, erwartet %v", farben, want)
		}
	}
	g, err := st.CreateAccount(ctx, NewAccount{Name: "Gina", Avatar: "fig-fuchs"})
	if err != nil {
		t.Fatal(err)
	}
	if g.Avatar != "fig-fuchs" {
		t.Fatalf("gewähltes Bild überschrieben: %q", g.Avatar)
	}
	// Auch aus der Datenbank gelesen stimmt die Farbe.
	b, err := st.Account(ctx, ids[1])
	if err != nil {
		t.Fatal(err)
	}
	if b.Avatar != "av-b" {
		t.Fatalf("gespeichert: %q", b.Avatar)
	}
}
