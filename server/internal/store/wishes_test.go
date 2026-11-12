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

// Zweimal „Matrix wünschen" ergibt einen Wunsch, nicht zwei.
func TestWunschNichtDoppelt(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	acc, err := st.CreateAccount(ctx, NewAccount{Name: "Lea", LockKind: "none"})
	if err != nil {
		t.Fatal(err)
	}
	a, err := st.CreateWish(ctx, acc.ID, "Matrix", 1999)
	if err != nil {
		t.Fatal(err)
	}
	b, err := st.CreateWish(ctx, acc.ID, "matrix", 1999)
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != b.ID {
		t.Errorf("zweiter Wunsch angelegt: %s / %s", a.ID, b.ID)
	}
	liste, _ := st.Wishes(ctx, acc.ID, false)
	if len(liste) != 1 {
		t.Errorf("%d Wünsche", len(liste))
	}
}

// Stand 15: Eine Datenbank von vorher bekommt die Spalten für Wünsche
// mit Kennung; ein Wunsch mit Kennung gleicht einen eigenen mit gleichem
// Titel nicht doppelt an, sondern ergänzt ihn.
func TestWunschKennungNachgeruestet(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	for _, spalte := range []string{"art", "imdb_id", "originaltitel", "poster", "erfuellt_item"} {
		var n int
		if err := st.db.QueryRow(`SELECT count(*) FROM pragma_table_info('wish') WHERE name = ?`, spalte).Scan(&n); err != nil || n != 1 {
			t.Errorf("Spalte %s fehlt (%v)", spalte, err)
		}
	}
	// Zweimal nachrüsten schadet nicht.
	if err := wunschKennungNachruesten(st.db); err != nil {
		t.Fatal(err)
	}
	acc, err := st.CreateAccount(ctx, NewAccount{Name: "Lea", LockKind: "none"})
	if err != nil {
		t.Fatal(err)
	}
	a, err := st.CreateWish(ctx, acc.ID, "Dune", 2021)
	if err != nil {
		t.Fatal(err)
	}
	b, err := st.WunschAnlegen(ctx, acc.ID, WunschDaten{Titel: "Dune", Jahr: 2021, Art: "movie", TMDbID: "438631"})
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != b.ID || b.TMDbID != "438631" || b.Art != "movie" {
		t.Errorf("nicht ergänzt: %+v / %+v", a, b)
	}
	k, _ := st.WunschKennungen(ctx)
	if !k["movie:438631"] || k["series:438631"] {
		t.Errorf("Kennungen: %v", k)
	}
}
