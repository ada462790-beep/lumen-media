// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package meta

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"lumen/internal/store"
)

// „Game of Thrones - S08E00 - The Last Watch": Der Scanner legt das
// Special in Staffel 0 unter einer selbst vergebenen Nummer ab (hier 1).
// Bei TMDb steht es in Staffel 0 unter Nummer 3 — die Zuordnung muss es
// am Titel finden und darf nicht Staffel 0, Folge 1 nehmen (eine ganz
// andere Sonderfolge). Ein Special ohne passenden Titel bleibt beim Namen
// aus der Datei.
func TestSpecialWirdAmTitelGefunden(t *testing.T) {
	var gefragt []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gefragt = append(gefragt, r.URL.Path)
		switch r.URL.Path {
		case "/search/tv":
			_ = json.NewEncoder(w).Encode(map[string]any{"results": []map[string]any{
				{"id": 9, "name": "Throne", "first_air_date": "2011-04-17"}}})
		case "/tv/9":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 9, "name": "Throne", "first_air_date": "2011-04-17"})
		case "/tv/9/season/0":
			_ = json.NewEncoder(w).Encode(map[string]any{"episodes": []map[string]any{
				{"episode_number": 1, "name": "Inside Episode 1"},
				{"episode_number": 2, "name": "A Day in the Life"},
				{"episode_number": 3, "name": "The Last Watch"}}})
		case "/tv/9/season/0/episode/3":
			_ = json.NewEncoder(w).Encode(map[string]any{"name": "The Last Watch",
				"overview": "Hinter den Kulissen der letzten Staffel.", "runtime": 115})
		case "/tv/9/season/0/episode/1":
			_ = json.NewEncoder(w).Encode(map[string]any{"name": "Inside Episode 1", "overview": "Falsch."})
		default:
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"status_message": "nix"})
		}
	}))
	t.Cleanup(srv.Close)
	st := testStore(t)
	ctx := context.Background()
	lib, _ := st.CreateLibrary(ctx, "Serien", "series", t.TempDir(), "de")
	serie := &store.Item{LibraryID: lib.ID, Kind: "series", Title: "Throne", MatchState: "pending"}
	_ = st.UpsertItem(ctx, serie)
	staffel := &store.Item{LibraryID: lib.ID, Kind: "season", ParentID: serie.ID, Title: "Specials", MatchState: "matched"}
	_ = st.UpsertItem(ctx, staffel)
	mit := &store.Item{LibraryID: lib.ID, Kind: "episode", ParentID: staffel.ID,
		Title: "The Last Watch", SeasonNo: 0, EpisodeNo: 1, MatchState: "pending"}
	ohne := &store.Item{LibraryID: lib.ID, Kind: "episode", ParentID: staffel.ID,
		Title: "Special", SeasonNo: 0, EpisodeNo: 2, MatchState: "pending"}
	for _, e := range []*store.Item{mit, ohne} {
		if err := st.UpsertItem(ctx, e); err != nil {
			t.Fatal(err)
		}
		_ = st.ZusatzSetzen(ctx, e.ID, store.Zusatz{Special: true})
	}

	r := resolverFor(t, st, srv)
	if _, err := r.ResolvePending(ctx, lib.ID, 10); err != nil {
		t.Fatal(err)
	}
	got, _ := st.Item(ctx, mit.ID)
	if got.MatchState != "matched" || got.Overview != "Hinter den Kulissen der letzten Staffel." {
		t.Fatalf("Special nicht am Titel gefunden: %q %q", got.MatchState, got.Overview)
	}
	if got.SeasonNo != 0 || got.EpisodeNo != 1 {
		t.Fatalf("Nummer verändert: S%dE%d", got.SeasonNo, got.EpisodeNo)
	}
	got, _ = st.Item(ctx, ohne.ID)
	if got.Title != "Special" || got.Overview != "" {
		t.Fatalf("Special ohne Titel bekam fremde Angaben: %q %q", got.Title, got.Overview)
	}
	for _, p := range gefragt {
		if p == "/tv/9/season/0/episode/1" || p == "/tv/9/season/0/episode/2" {
			t.Fatalf("nach der selbst vergebenen Nummer gefragt: %s", p)
		}
	}
}
