// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package meta

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"lumen/internal/store"
)

// PosterNachholen geht reihum: Jede Runde fragt andere Titel, bis alle
// einmal dran waren — vorher lieferte die Abfrage ohne Reihenfolge immer
// dieselben ersten, und wer dahinter stand, bekam nie ein Poster. Wer
// schon dran war, kommt erst nach posterPause wieder.
func TestPosterNachholenReihum(t *testing.T) {
	posterVersucht = sync.Map{}
	alt := posterHappen
	posterHappen = 0 // jede Runde liest nur so viele, wie sie fragt
	t.Cleanup(func() { posterHappen = alt })
	posterMu.Lock()
	posterAb = ""
	posterMu.Unlock()

	var mu sync.Mutex
	gefragt := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gefragt[r.URL.Path]++
		mu.Unlock()
		w.WriteHeader(http.StatusNotFound) // TMDb kennt kein Poster
	}))
	defer srv.Close()
	st, err := store.Open(filepath.Join(t.TempDir(), "l.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := t.Context()
	lib, _ := st.CreateLibrary(ctx, "Filme", "movie", t.TempDir(), "de")
	for i := 1; i <= 6; i++ {
		it := &store.Item{LibraryID: lib.ID, Kind: "movie", Title: fmt.Sprintf("Film %d", i), MatchState: "matched"}
		if err := st.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
		_ = st.SetSourceID(ctx, it.ID, "tmdb", fmt.Sprint(100+i))
	}
	r := resolverFor(t, st, srv)
	anzahl := func() int {
		mu.Lock()
		defer mu.Unlock()
		n := 0
		for _, v := range gefragt {
			n += v
		}
		return n
	}
	for i := 0; i < 3; i++ {
		r.PosterNachholen(ctx, 2)
	}
	mu.Lock()
	if len(gefragt) != 6 {
		t.Fatalf("nach drei Runden zu je 2: %d verschiedene Titel gefragt: %v", len(gefragt), gefragt)
	}
	mu.Unlock()
	vorher := anzahl()
	r.PosterNachholen(ctx, 2)
	if anzahl() != vorher {
		t.Fatalf("schon versuchte Titel gleich wieder gefragt")
	}
	// Nach der Pause kommen sie wieder dran.
	posterVersucht.Range(func(k, _ any) bool {
		posterVersucht.Store(k, time.Now().Add(-posterPause-time.Minute))
		return true
	})
	r.PosterNachholen(ctx, 2)
	if anzahl() != vorher+2 {
		t.Fatalf("nach der Pause: %d Anfragen statt %d", anzahl(), vorher+2)
	}
}
