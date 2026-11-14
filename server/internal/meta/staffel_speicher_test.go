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
	"sync"
	"sync/atomic"
	"testing"
)

// Viele Folgen derselben Staffel kosten eine Anfrage bei TMDb, nicht eine
// je Folge. Fehlt eine Folge in der Staffel, wird sie einzeln gefragt.
func TestFolgenStaffelweise(t *testing.T) {
	var staffel, einzeln atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/tv/5/season/2":
			staffel.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"episodes": []map[string]any{
				{"episode_number": 1, "name": "Eins", "overview": "A", "air_date": "2020-01-01", "runtime": 40, "still_path": "/e1.jpg"},
				{"episode_number": 2, "name": "Zwei", "overview": "B", "air_date": "2020-01-08", "runtime": 41},
			}})
		case "/tv/5/season/2/episode/9":
			einzeln.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"name": "Neun", "runtime": 30})
		default:
			einzeln.Add(1)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	tm := NewTMDb("k", "de-DE", "CH")
	tm.BaseURL, tm.ImageURL = srv.URL, srv.URL+"/bild"
	ctx := context.Background()

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if c, err := tm.FolgeAusStaffel(ctx, "5", 2, 1); err != nil || c.Title != "Eins" || c.RuntimeMS != 40*60_000 || c.PosterURL == "" {
				t.Errorf("Folge 1: %+v %v", c, err)
			}
		}()
	}
	wg.Wait()
	if c, err := tm.FolgeAusStaffel(ctx, "5", 2, 2); err != nil || c.Title != "Zwei" {
		t.Fatalf("Folge 2: %+v %v", c, err)
	}
	if n := staffel.Load(); n != 1 {
		t.Fatalf("Staffel %d-mal geholt, erwartet einmal", n)
	}
	if n := einzeln.Load(); n != 0 {
		t.Fatalf("%d Einzelanfragen, erwartet keine", n)
	}
	// Nicht in der Staffel: einzeln nachfragen.
	if c, err := tm.FolgeAusStaffel(ctx, "5", 2, 9); err != nil || c.Title != "Neun" {
		t.Fatalf("Folge 9: %+v %v", c, err)
	}
	// Unbekannte Staffel: Einzelanfrage sagt „nicht gefunden".
	if _, err := tm.FolgeAusStaffel(ctx, "5", 7, 1); err == nil {
		t.Fatal("unbekannte Staffel sollte einen Fehler geben")
	}
}
