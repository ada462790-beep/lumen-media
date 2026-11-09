// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package stream

import (
	"context"
	"sync"
	"testing"
	"time"
)

// Nach dem Anlauf geht es nicht schneller als die Rate — auch nicht mit
// mehreren gleichzeitigen Anfragen derselben Wiedergabe.
func TestDrosselHaeltRate(t *testing.T) {
	d := NeueDrossel(8_000_000) // 2 × 8 Mbit/s = 2 MB/s, Anlauf 60 MB
	d.vorrat = 0                // Anlauf aufgebraucht
	start := time.Now()
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				_ = d.Nehmen(context.Background(), 25_000) // 4 × 10 × 25 kB = 1 MB
			}
		}()
	}
	wg.Wait()
	if dauer := time.Since(start); dauer < 400*time.Millisecond {
		t.Fatalf("1 MB in %v — schneller als 2 MB/s", dauer)
	}
}

// Der Anlauf geht ohne Warten; eine nil-Drossel bremst nie.
func TestDrosselAnlauf(t *testing.T) {
	d := NeueDrossel(0)
	start := time.Now()
	if err := d.Nehmen(context.Background(), 10<<20); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 50*time.Millisecond {
		t.Fatal("Anlauf gebremst")
	}
	var keine *Drossel
	if err := keine.Nehmen(context.Background(), 1<<30); err != nil {
		t.Fatal(err)
	}
	// Abbruch beim Warten.
	d.vorrat = 0
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := d.Nehmen(ctx, 100<<20); err == nil {
		t.Fatal("Warten nicht abgebrochen")
	}
}
