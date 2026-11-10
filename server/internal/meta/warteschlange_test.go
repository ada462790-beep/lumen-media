// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package meta

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"lumen/internal/store"
)

// Folgen einer Serie, die TMDb nicht kennt, dürfen die Warteschlange nicht
// verstopfen: Vorher blieben sie für immer „wartend" vorne stehen, und
// neue Folgen anderer Serien kamen nie dran. Staffeln (es gibt für sie
// nichts zu holen) ebenso.
func TestWarteschlangeOhneUnbekannteSerienUndStaffeln(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	lib, _ := st.CreateLibrary(ctx, "Serien", "series", t.TempDir(), "de")

	unbekannt := &store.Item{LibraryID: lib.ID, Kind: "series", Title: "Gibt es nicht", MatchState: "unmatched"}
	bekannt := &store.Item{LibraryID: lib.ID, Kind: "series", Title: "Nordlicht", MatchState: "matched"}
	for _, s := range []*store.Item{unbekannt, bekannt} {
		if err := st.UpsertItem(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.SetSourceID(ctx, bekannt.ID, "tmdb", "7"); err != nil {
		t.Fatal(err)
	}
	folge := func(serie *store.Item, nr int) *store.Item {
		staffel := &store.Item{LibraryID: lib.ID, Kind: "season", ParentID: serie.ID, Title: "Staffel 1",
			SeasonNo: 1, MatchState: "pending"}
		if err := st.UpsertItem(ctx, staffel); err != nil {
			t.Fatal(err)
		}
		f := &store.Item{LibraryID: lib.ID, Kind: "episode", ParentID: staffel.ID, Title: "Folge",
			SeasonNo: 1, EpisodeNo: nr, MatchState: "pending"}
		if err := st.UpsertItem(ctx, f); err != nil {
			t.Fatal(err)
		}
		return f
	}
	for i := 1; i <= 5; i++ {
		folge(unbekannt, i)
	}
	gesucht := folge(bekannt, 4)

	wartet, err := st.PendingItems(ctx, lib.ID, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(wartet) != 1 || wartet[0].ID != gesucht.ID {
		t.Fatalf("Warteschlange %+v — erwartet nur die Folge der bekannten Serie", wartet)
	}
}

// Wird ein Titel neu zugeordnet, kommt ein anderes Bild — das ersetzt das
// alte. Vorher blieb die Datei der falschen Serie liegen, weil sie schon da
// war.
func TestNeuesBildErsetztAltes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("bild:" + r.URL.Path))
	}))
	defer srv.Close()
	st := testStore(t)
	ctx := context.Background()
	lib, _ := st.CreateLibrary(ctx, "Serien", "series", t.TempDir(), "de")
	it := &store.Item{LibraryID: lib.ID, Kind: "series", Title: "Senna", MatchState: "matched"}
	if err := st.UpsertItem(ctx, it); err != nil {
		t.Fatal(err)
	}
	r, err := NewResolver(st, t.TempDir(), Config{TMDbKey: "x"})
	if err != nil {
		t.Fatal(err)
	}
	inhalt := func() string {
		b, _ := os.ReadFile(filepath.Join(r.ArtDir(), it.ID+"-poster.jpg"))
		return string(b)
	}
	if err := r.saveArtwork(ctx, it.ID, &Candidate{PosterURL: srv.URL + "/falsch.jpg"}); err != nil {
		t.Fatal(err)
	}
	if inhalt() != "bild:/falsch.jpg" {
		t.Fatalf("erstes Bild: %q", inhalt())
	}
	if err := r.saveArtwork(ctx, it.ID, &Candidate{PosterURL: srv.URL + "/richtig.jpg"}); err != nil {
		t.Fatal(err)
	}
	if inhalt() != "bild:/richtig.jpg" {
		t.Fatalf("nach dem Neuzuordnen noch das alte Bild: %q", inhalt())
	}
}
