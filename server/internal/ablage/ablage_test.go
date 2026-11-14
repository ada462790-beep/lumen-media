// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package ablage

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Viele Schreiber, ein Ziel: keiner scheitert am Umbenennen, und am Ende
// liegt ein ganzes Bild da und keine Zwischendatei.
func TestGleichzeitigInsSelbeZiel(t *testing.T) {
	dir := t.TempDir()
	ziel := filepath.Join(dir, "it_x-poster.jpg")
	inhalt := strings.Repeat("b", 64<<10)
	var wg sync.WaitGroup
	fehler := make(chan error, 32)
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			fehler <- Schreiben(ziel, strings.NewReader(inhalt), 1<<20)
		}()
	}
	wg.Wait()
	close(fehler)
	for err := range fehler {
		if err != nil {
			t.Fatalf("ein Schreiber scheiterte: %v", err)
		}
	}
	b, err := os.ReadFile(ziel)
	if err != nil || string(b) != inhalt {
		t.Fatalf("Ziel kaputt: %v (%d Bytes)", err, len(b))
	}
	reste, _ := filepath.Glob(filepath.Join(dir, "*"+Endung))
	if len(reste) != 0 {
		t.Fatalf("Zwischendateien liegen geblieben: %v", reste)
	}
}

func TestZuGrossLaesstNichtsLiegen(t *testing.T) {
	dir := t.TempDir()
	ziel := filepath.Join(dir, "gross.jpg")
	err := Schreiben(ziel, strings.NewReader(strings.Repeat("x", 2048)), 1024)
	if !errors.Is(err, ErrZuGross) {
		t.Fatalf("erwartet ErrZuGross, bekam %v", err)
	}
	if e, _ := os.ReadDir(dir); len(e) != 0 {
		t.Fatalf("es blieb etwas liegen: %v", e)
	}
}

// Einmal: Wer kommt, während derselbe Schlüssel läuft, wartet auf das
// Ergebnis statt selbst zu arbeiten.
func TestEinmalJeSchluessel(t *testing.T) {
	var aufrufe atomic.Int32
	start := make(chan struct{})
	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_ = Einmal("a", func() error {
				aufrufe.Add(1)
				time.Sleep(50 * time.Millisecond)
				return nil
			})
		}()
	}
	close(start)
	wg.Wait()
	if n := aufrufe.Load(); n < 1 || n > 2 {
		// Höchstens zwei: Ein Nachzügler darf nach dem Ende neu anfangen.
		t.Fatalf("fn lief %d-mal", n)
	}
}
