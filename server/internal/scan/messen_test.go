// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package scan

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lumen/internal/store"
)

type zaehlProbe struct {
	n      int
	fehler bool
}

func (z *zaehlProbe) Probe(ctx context.Context, path string) (*Probe, error) {
	z.n++
	if z.fehler {
		return nil, errors.New("ffprobe " + path + ": Invalid data found when processing input")
	}
	return &Probe{DurationMS: 5_400_000, Width: 1920, Height: 1080, VideoCodec: "h264",
		Tracks: []Track{{Index: 0, Kind: "video", Codec: "h264"}}}, nil
}

// Ein zweiter Scan liest eine unveränderte Datei nicht noch einmal — die
// Platte darf schlafen. Ändert sich die Datei, wird neu gemessen.
func TestUnveraenderteDateiWirdNichtNeuGemessen(t *testing.T) {
	root := t.TempDir()
	film := filepath.Join(root, "Nordlicht (2025)", "Nordlicht.mkv")
	_ = os.MkdirAll(filepath.Dir(film), 0o755)
	_ = os.WriteFile(film, []byte("x"), 0o644)

	st, err := store.Open(filepath.Join(t.TempDir(), "l.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	lib, _ := st.CreateLibrary(ctx, "Filme", "movie", root, "de")

	p := &zaehlProbe{}
	sc := New(st, p)
	for i := 0; i < 3; i++ {
		if _, err := sc.Scan(ctx, *lib); err != nil {
			t.Fatal(err)
		}
	}
	if p.n != 1 {
		t.Fatalf("drei Scans, %d Messungen — erwartet 1", p.n)
	}

	// Neue Untertitel daneben: neu messen.
	_ = os.WriteFile(filepath.Join(filepath.Dir(film), "Nordlicht.de.srt"), []byte("1\n"), 0o644)
	_, _ = sc.Scan(ctx, *lib)
	if p.n != 2 {
		t.Fatalf("nach neuen Untertiteln %d Messungen — erwartet 2", p.n)
	}

	// Datei verändert: neu messen.
	_ = os.WriteFile(film, []byte("xx"), 0o644)
	later := time.Now().Add(time.Minute)
	_ = os.Chtimes(film, later, later)
	_, _ = sc.Scan(ctx, *lib)
	if p.n != 3 {
		t.Fatalf("nach Änderung %d Messungen — erwartet 3", p.n)
	}
}

// Scheitert eine Messung, bleiben die alten Werte stehen, und das
// Protokoll nennt weder Datei noch Pfad.
func TestGescheiterteMessungBehaeltWerte(t *testing.T) {
	root := t.TempDir()
	film := filepath.Join(root, "Geheimer Titel (2025)", "Geheimer.Titel.mkv")
	_ = os.MkdirAll(filepath.Dir(film), 0o755)
	_ = os.WriteFile(film, []byte("x"), 0o644)

	st, err := store.Open(filepath.Join(t.TempDir(), "l.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	lib, _ := st.CreateLibrary(ctx, "Filme", "movie", root, "de")

	p := &zaehlProbe{}
	sc := New(st, p)
	if _, err := sc.Scan(ctx, *lib); err != nil {
		t.Fatal(err)
	}

	// Datei ändert sich, und die neue Messung scheitert.
	_ = os.WriteFile(film, []byte("xy"), 0o644)
	later := time.Now().Add(time.Minute)
	_ = os.Chtimes(film, later, later)
	p.fehler = true
	if _, err := sc.Scan(ctx, *lib); err != nil {
		t.Fatal(err)
	}

	g := st.GemessenZuPfad(ctx, film)
	if g == nil || g.Datei.DurationMS != 5_400_000 || g.Datei.Width != 1920 {
		t.Fatalf("alte Werte verloren: %+v", g)
	}
	log, _ := st.RecentLog(ctx, "", 50)
	for _, l := range log {
		if strings.Contains(l.Message, "Geheimer") || strings.Contains(l.Message, "/") {
			t.Fatalf("Protokoll verrät die Datei: %q", l.Message)
		}
	}
}

// Ändern die Metadaten den Titel einer Serie, legt der nächste Durchlauf
// sie nicht neu an (Issue #7: „9-1-1 Notruf L.A." fünfzigmal).
func TestSerieBleibtNachUmbenennung(t *testing.T) {
	root := t.TempDir()
	for _, f := range []string{"9-1-1 (2018)/Staffel 1/9-1-1 S01E01.mkv", "9-1-1 (2018)/Staffel 1/9-1-1 S01E02.mkv"} {
		p := filepath.Join(root, f)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		_ = os.WriteFile(p, []byte("x"), 0o644)
	}
	st, err := store.Open(filepath.Join(t.TempDir(), "l.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	lib, _ := st.CreateLibrary(ctx, "Serien", "series", root, "de")
	sc := New(st, nil)
	if _, err := sc.Scan(ctx, *lib); err != nil {
		t.Fatal(err)
	}
	serien, _ := st.Query(ctx, store.Query{Libraries: []string{lib.ID}, Kinds: []string{"series"}, Limit: 50})
	if len(serien) != 1 {
		t.Fatalf("%d Serien nach dem ersten Scan", len(serien))
	}
	// TMDb benennt um.
	s := serien[0]
	s.Title, s.SortTitle, s.MatchState = "9-1-1 Notruf L.A.", store.SortKey("9-1-1 Notruf L.A."), "matched"
	_ = st.UpsertItem(ctx, &s)
	for i := 0; i < 3; i++ {
		if _, err := sc.Scan(ctx, *lib); err != nil {
			t.Fatal(err)
		}
	}
	serien, _ = st.Query(ctx, store.Query{Libraries: []string{lib.ID}, Kinds: []string{"series"}, Limit: 50})
	if len(serien) != 1 {
		t.Fatalf("%d Serien nach Umbenennung und drei Scans, erwartet 1", len(serien))
	}
}
