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

// Die Hintergrundarbeit wartet, solange die Bedienung arbeitet — aber
// nie länger als hoechstensWarten.
func TestHintergrundLaesstBedienungVor(t *testing.T) {
	ctx := context.Background()
	time.Sleep(ruhe) // was frühere Tests angemeldet haben, ist vorbei
	t0 := time.Now()
	HintergrundWarten(ctx)
	if d := time.Since(t0); d > 50*time.Millisecond {
		t.Fatalf("ohne Bedienung gewartet: %v", d)
	}

	fertig := BedienungBeginnt()
	go func() { time.Sleep(100 * time.Millisecond); fertig() }()
	t0 = time.Now()
	HintergrundWarten(ctx)
	if d := time.Since(t0); d < 100*time.Millisecond+ruhe-20*time.Millisecond {
		t.Fatalf("nicht auf die Bedienung gewartet: %v", d)
	}

	ewig := BedienungBeginnt()
	defer ewig()
	t0 = time.Now()
	HintergrundWarten(ctx)
	if d := time.Since(t0); d > hoechstensWarten+200*time.Millisecond {
		t.Fatalf("zu lange gewartet: %v", d)
	}
}

// Zugeordnete Titel ohne Poster kommen zum Nachholen, solche mit Poster
// oder ohne TMDb-Kennung nicht.
func TestTitelOhnePoster(t *testing.T) {
	st := testStore(t)
	ctx := t.Context()
	lib, _ := st.CreateLibrary(ctx, "Filme", "movie", t.TempDir(), "de")
	neu := func(titel, tmdb string) *Item {
		it := &Item{LibraryID: lib.ID, Kind: "movie", Title: titel, MatchState: "matched"}
		if err := st.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
		if tmdb != "" {
			_ = st.SetSourceID(ctx, it.ID, "tmdb", tmdb)
		}
		return it
	}
	ohne := neu("Ohne", "1")
	mit := neu("Mit", "2")
	neu("Ohne Kennung", "")
	_ = st.SetArtwork(ctx, mit.ID, "poster", "x.jpg", "http://bild")
	got, err := st.TitelOhnePoster(ctx, 10)
	if err != nil || len(got) != 1 || got[0].ID != ohne.ID {
		t.Fatalf("erwartet nur %q, bekam %+v %v", ohne.Title, got, err)
	}
}
