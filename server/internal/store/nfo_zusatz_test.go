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

// „DE-18" aus einer Emby-NFO war bisher ungeprüft. Ohne dass sich die NFO
// ändert, bekommt der Titel seine Grenze nachgetragen.
func TestFreigabenNachrechnen(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	mustExec(t, s, `INSERT INTO library (id,name,kind,path) VALUES ('lib','Filme','movie','/x')`)
	a := &Item{LibraryID: "lib", Kind: "movie", Title: "Kill Bill", AgeRating: "DE-18", MatchState: "lokal"}
	b := &Item{LibraryID: "lib", Kind: "movie", Title: "Rätsel", AgeRating: "NR", MatchState: "lokal"}
	for _, it := range []*Item{a, b} {
		if err := s.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
	}
	alter := func(k string) *int {
		if k == "DE-18" {
			v := 18
			return &v
		}
		return nil
	}
	n, err := s.FreigabenNachrechnen(ctx, alter)
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if got, _ := s.Item(ctx, a.ID); got.AgeYears == nil || *got.AgeYears != 18 {
		t.Fatalf("Kill Bill: %v", got.AgeYears)
	}
	if got, _ := s.Item(ctx, b.ID); got.AgeYears != nil {
		t.Fatalf("NR muss ungeprüft bleiben: %v", *got.AgeYears)
	}
}
