// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package meta

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"lumen/internal/store"
)

func downloadResolver(t *testing.T) *Resolver {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "l.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	r, err := NewResolver(st, t.TempDir(), Config{})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// Ein Bild über der Obergrenze ist ein Fehler — kein halbes Bild auf der
// Platte, keine Zwischendatei, die liegen bleibt.
func TestZuGrossesBildWirdNichtAbgeschnitten(t *testing.T) {
	riesig := bytes.Repeat([]byte{0xFF}, maxBild+10)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(riesig)
	}))
	defer srv.Close()
	r := downloadResolver(t)
	dest := filepath.Join(r.ArtDir(), "it_x-poster.jpg")
	if err := r.download(t.Context(), srv.URL+"/p.jpg", dest); err == nil {
		t.Fatal("zu grosses Bild wurde ohne Fehler gespeichert")
	}
	reste, _ := os.ReadDir(r.ArtDir())
	if len(reste) != 0 {
		t.Fatalf("nach dem Fehler liegt noch etwas im Bilderordner: %v", reste)
	}
}

// Holen zwei dasselbe Bild zugleich, kommt ein ganzes Bild heraus — nicht
// zwei Schreiber in derselben Zwischendatei.
func TestGleichzeitigesHerunterladen(t *testing.T) {
	bild := bytes.Repeat([]byte("lumen-bild "), 200_000) // gut 2 MB
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		for i := 0; i < len(bild); i += 64 << 10 {
			_, _ = w.Write(bild[i:min(i+64<<10, len(bild))])
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
	}))
	defer srv.Close()
	r := downloadResolver(t)
	dest := filepath.Join(r.ArtDir(), "it_x-poster.jpg")
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := r.download(t.Context(), srv.URL+"/p.jpg", dest); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	got, err := os.ReadFile(dest)
	if err != nil || !bytes.Equal(got, bild) {
		t.Fatalf("Bild beschädigt: %d Bytes statt %d (%v)", len(got), len(bild), err)
	}
	reste, _ := filepath.Glob(filepath.Join(r.ArtDir(), "*.teil"))
	if len(reste) != 0 {
		t.Fatalf("Zwischendateien liegen geblieben: %v", reste)
	}
}
