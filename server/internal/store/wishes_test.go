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
