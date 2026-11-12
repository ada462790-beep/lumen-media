// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package meta

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"lumen/internal/lokal"
	"lumen/internal/store"
)

// Ein falscher Schlüssel (401): einmal im Protokoll, nicht je Titel; die
// Titel mit vollständiger NFO gelten als erkannt; ein späterer gültiger
// Schlüssel holt sie und die vorgemerkten Kennungen nach.
func TestSchluesselAbgelehnt(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	lib, _ := st.CreateLibrary(ctx, "Filme", "movie", t.TempDir(), "de")
	var anfragen atomic.Int32
	abgelehnt := atomic.Bool{}
	abgelehnt.Store(true)
	echt := fakeSources(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		anfragen.Add(1)
		if abgelehnt.Load() {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		// Weiter an die nachgebauten Quellen.
		req, _ := http.NewRequest(r.Method, echt.URL+r.URL.String(), nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		w.WriteHeader(resp.StatusCode)
		buf := make([]byte, 1<<16)
		for {
			n, err := resp.Body.Read(buf)
			_, _ = w.Write(buf[:n])
			if err != nil {
				break
			}
		}
	}))
	t.Cleanup(srv.Close)

	var ids []string
	for _, titel := range []string{"Die Bucht", "Matrix", "Alien"} {
		it := &store.Item{LibraryID: lib.ID, Kind: "movie", Title: titel, Year: 2019, MatchState: "pending"}
		if err := st.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, it.ID)
	}
	// „Matrix" hat eine vollständige NFO mit Kennung.
	_ = st.LokaleNFOSetzen(ctx, ids[1], store.LokaleNFO{Pfad: "/x/movie.nfo", MTime: 1,
		Daten: `{"art":"movie","titel":"Matrix","jahr":1999,"inhalt":"Neo.","tmdb":"42"}`})
	_ = st.SetSourceID(ctx, ids[1], "tmdb", "42")
	// „Alien": Kennung ohne Schlüssel eingegeben.
	if err := KennungenVormerken(ctx, st, ids[2], lokal.Kennungen{TMDb: "42"}); err != nil {
		t.Fatal(err)
	}
	if it, _ := st.Item(ctx, ids[2]); it.MatchState != "lokal" || VorgemerktZahl(ctx, st) != 1 {
		t.Fatalf("vorgemerkt: %q %d", it.MatchState, VorgemerktZahl(ctx, st))
	}

	_ = st.SetSetting(ctx, EinstellungTMDbSchluessel, "falsch")
	r := resolverFor(t, st, srv)
	for i := 0; i < 2; i++ {
		if _, err := r.ResolvePending(ctx, lib.ID, 10); !errors.Is(err, ErrTMDbSchluessel) {
			t.Fatalf("Fehler %v", err)
		}
	}
	if anfragen.Load() != 2 {
		t.Errorf("%d Anfragen an TMDb, erwartet eine je Durchgang", anfragen.Load())
	}
	if SchluesselStand(ctx, st) != "abgelehnt" {
		t.Errorf("Stand %q", SchluesselStand(ctx, st))
	}
	zeilen, _ := st.RecentLog(ctx, "", 50)
	warnungen := 0
	for _, z := range zeilen {
		if strings.Contains(z.Message, "Schlüssel") {
			warnungen++
		}
	}
	if warnungen != 1 {
		t.Errorf("%d Meldungen zum Schlüssel, erwartet 1", warnungen)
	}
	if it, _ := st.Item(ctx, ids[1]); it.MatchState != "lokal" {
		t.Errorf("Matrix: %q statt lokal", it.MatchState)
	}
	if it, _ := st.Item(ctx, ids[0]); it.MatchState != "pending" {
		t.Errorf("ohne NFO: %q", it.MatchState)
	}

	// Der Schlüssel gilt wieder: Matrix (über die NFO) und Alien (über die
	// vorgemerkte Kennung) werden geholt.
	abgelehnt.Store(false)
	if _, err := r.ResolvePending(ctx, lib.ID, 10); err != nil {
		t.Fatal(err)
	}
	for _, id := range ids[1:] {
		if it, _ := st.Item(ctx, id); it.MatchState != "matched" {
			t.Errorf("%s: %q statt matched", it.Title, it.MatchState)
		}
	}
	if SchluesselStand(ctx, st) != "ok" || VorgemerktZahl(ctx, st) != 0 {
		t.Errorf("danach: %q, %d vorgemerkt", SchluesselStand(ctx, st), VorgemerktZahl(ctx, st))
	}
}
