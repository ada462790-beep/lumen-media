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
	"time"
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

// Ein Fehler bei der Staffel-Anfrage („TMDb bremst", Server-Fehler) gilt
// nur Sekunden, nicht 15 Minuten wie eine geholte Staffel.
func TestStaffelFehlerGiltNurKurz(t *testing.T) {
	var kaputt atomic.Bool
	var staffel atomic.Int32
	kaputt.Store(true)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/tv/5/season/1" {
			staffel.Add(1)
			if kaputt.Load() {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"episodes": []map[string]any{
				{"episode_number": 1, "name": "Eins"}}})
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	tm := NewTMDb("k", "de-DE", "CH")
	tm.BaseURL, tm.ImageURL = srv.URL, srv.URL+"/bild"
	ctx := context.Background()
	if _, err := tm.staffelHolen(ctx, "5", 1); err == nil {
		t.Fatal("kein Fehler")
	}
	// Gleich danach: derselbe Fehler, keine zweite Anfrage.
	if _, err := tm.staffelHolen(ctx, "5", 1); err == nil || staffel.Load() != 1 {
		t.Fatalf("sofort: err=%v, %d Anfragen", err, staffel.Load())
	}
	kaputt.Store(false)
	// Nach staffelFehlerHaltbar (hier: zurückgestellt) wird neu gefragt.
	tm.staffelMu.Lock()
	tm.staffelDaten["5/1"].geholt = time.Now().Add(-staffelFehlerHaltbar - time.Second)
	tm.staffelMu.Unlock()
	if c, err := tm.FolgeAusStaffel(ctx, "5", 1, 1); err != nil || c.Title != "Eins" {
		t.Fatalf("nach der Pause: %+v %v", c, err)
	}
	// Eine geholte Staffel gilt weiter lange.
	tm.staffelMu.Lock()
	tm.staffelDaten["5/1"].geholt = time.Now().Add(-time.Minute)
	tm.staffelMu.Unlock()
	n := staffel.Load()
	if _, err := tm.FolgeAusStaffel(ctx, "5", 1, 1); err != nil || staffel.Load() != n {
		t.Fatalf("geholte Staffel neu gefragt: %v", err)
	}
}

// Derselbe Fehler bei vielen Titeln steht einmal im Protokoll.
func TestFehlerSammlerZaehlt(t *testing.T) {
	f := fehlerSammler{}
	e := context.DeadlineExceeded
	if !f.erstesMal(e) || f.erstesMal(e) || f.erstesMal(e) {
		t.Fatal("erstesMal zählt falsch")
	}
	if f[e.Error()] != 3 {
		t.Fatalf("gezählt: %d", f[e.Error()])
	}
}
