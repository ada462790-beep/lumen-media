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
	"testing"
	"time"

	"lumen/internal/store"
)

func dateienHinlegen(t *testing.T, root string, dateien ...string) {
	t.Helper()
	for _, f := range dateien {
		p := filepath.Join(root, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// schreibZaehler zählt jede Zeile, die in den Tabellen der Mediathek
// geschrieben wird — über Trigger, damit es gleich ist, über welche
// Verbindung des Pools geschrieben wurde.
func schreibZaehler(t *testing.T, st *store.Store) func() int {
	t.Helper()
	db := st.DB()
	if _, err := db.Exec(`CREATE TABLE schreib_zaehler (n INTEGER NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO schreib_zaehler VALUES (0)`); err != nil {
		t.Fatal(err)
	}
	for _, tab := range []string{"item", "item_search", "media_file", "media_track", "chapter", "genre", "library"} {
		for _, art := range []string{"INSERT", "UPDATE", "DELETE"} {
			if _, err := db.Exec(`CREATE TRIGGER zaehl_` + tab + `_` + art + ` AFTER ` + art + ` ON ` + tab +
				` BEGIN UPDATE schreib_zaehler SET n = n + 1; END`); err != nil {
				t.Fatal(err)
			}
		}
	}
	return func() int {
		var n int
		_ = db.QueryRow(`SELECT n FROM schreib_zaehler`).Scan(&n)
		return n
	}
}

// mitUntertitelnProbe misst wie stream.FFmpeg: Die Untertitel daneben
// gehören zu den Spuren der Datei.
type mitUntertitelnProbe struct{ zaehlProbe }

func (m *mitUntertitelnProbe) Probe(ctx context.Context, path string) (*Probe, error) {
	p, err := m.zaehlProbe.Probe(ctx, path)
	if err == nil {
		p.Tracks = append(p.Tracks, SidecarSubtitles(path)...)
	}
	return p, err
}

// Ein Durchlauf über eine Sammlung, an der sich nichts geändert hat,
// schreibt keine einzige Zeile der Mediathek — weder Dateien noch Spuren
// noch Titel. Nur das Ergebnis des Durchlaufs (library) wird vermerkt.
func TestDurchlaufOhneAenderungSchreibtNichts(t *testing.T) {
	root := t.TempDir()
	dateienHinlegen(t, root,
		"Nordlicht (2025)/Nordlicht (2025).mkv",
		"Nordlicht (2025)/Nordlicht (2025).de.srt",
		"Meridian (2021)/Meridian.2021.2160p.mkv",
		"Meridian (2021)/Meridian.2021.1080p.mkv",
		"Fallout (2024)/Season 01/Fallout S01E01.mkv",
		"Fallout (2024)/Season 01/Fallout S01E02.mkv",
		"Fallout (2024)/Season 01/Fallout S01E02.en.srt",
		"Fallout (2024)/Season 02/Fallout S02E01.mkv",
	)
	st, err := store.Open(filepath.Join(t.TempDir(), "l.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	lib, _ := st.CreateLibrary(ctx, "Gemischt", "movie", root, "de")
	p := &mitUntertitelnProbe{}
	sc := New(st, p)
	if _, err := sc.Scan(ctx, *lib); err != nil {
		t.Fatal(err)
	}
	gemessen := p.n
	zaehler := schreibZaehler(t, st)
	for i := 0; i < 2; i++ {
		res, err := sc.Scan(ctx, *lib)
		if err != nil {
			t.Fatal(err)
		}
		if res.Added != 0 || res.Updated != 0 || res.Missing != 0 {
			t.Fatalf("Durchlauf ohne Änderung meldet %+v", res)
		}
	}
	if p.n != gemessen {
		t.Fatalf("unveränderte Dateien neu gemessen: %d statt %d", p.n, gemessen)
	}
	// Zwei Durchläufe: je eine Zeile für das Ergebnis in library.
	if n := zaehler(); n != 2 {
		t.Fatalf("Durchläufe ohne Änderung schrieben %d Zeilen, erwartet nur 2 (Ergebnis)", n)
	}

	// Eine fehlende Datei, die wieder da ist, wird sehr wohl geschrieben.
	film := filepath.Join(root, "Nordlicht (2025)", "Nordlicht (2025).mkv")
	_ = os.Rename(film, film+".weg")
	res, _ := sc.Scan(ctx, *lib)
	if res.Missing != 1 {
		t.Fatalf("fehlende Datei nicht vermerkt: %+v", res)
	}
	_ = os.Rename(film+".weg", film)
	if _, err := sc.Scan(ctx, *lib); err != nil {
		t.Fatal(err)
	}
	if g := st.GemessenZuPfad(ctx, film); g == nil || g.Datei.Missing {
		t.Fatalf("zurückgekehrte Datei steht noch als fehlend da: %+v", g)
	}

	// Und eine geänderte Datei auch.
	_ = os.WriteFile(film, []byte("anders"), 0o644)
	spaeter := time.Now().Add(time.Hour)
	_ = os.Chtimes(film, spaeter, spaeter)
	vorher := zaehler()
	if _, err := sc.Scan(ctx, *lib); err != nil {
		t.Fatal(err)
	}
	if zaehler() == vorher {
		t.Fatal("geänderte Datei wurde nicht geschrieben")
	}
	if g := st.GemessenZuPfad(ctx, film); g == nil || g.Datei.SizeBytes != int64(len("anders")) {
		t.Fatalf("neue Grösse nicht gespeichert: %+v", g)
	}
}

// Wird ein Durchlauf abgebrochen (Lumen fährt herunter), vermerkt er
// nichts als verschwunden — er hat ja nur einen Teil gesehen.
func TestAbgebrochenerDurchlaufVermerktNichtsAlsFehlend(t *testing.T) {
	root := t.TempDir()
	var dateien []string
	for _, n := range []string{"A", "B", "C", "D", "E", "F"} {
		dateien = append(dateien, "Film "+n+" (2020)/Film "+n+" (2020).mkv")
	}
	dateien = append(dateien, "Serie (2020)/Season 01/Serie S01E01.mkv", "Serie (2020)/Season 01/Serie S01E02.mkv")
	dateienHinlegen(t, root, dateien...)
	st, err := store.Open(filepath.Join(t.TempDir(), "l.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	lib, _ := st.CreateLibrary(context.Background(), "Filme", "movie", root, "de")
	sc := New(st, nil)
	if _, err := sc.Scan(context.Background(), *lib); err != nil {
		t.Fatal(err)
	}

	for _, nach := range []int{1, 3, 7} {
		ctx, cancel := context.WithCancel(context.Background())
		n := 0
		sc.Bremse = func() bool {
			if n++; n == nach {
				cancel()
			}
			return false
		}
		_, err := sc.Scan(ctx, *lib)
		cancel()
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("abgebrochen nach %d Dateien: Fehler %v, erwartet context.Canceled", nach, err)
		}
		fehlend, _ := st.MissingFiles(context.Background())
		if len(fehlend) != 0 {
			t.Fatalf("abgebrochen nach %d Dateien: %d Dateien als fehlend vermerkt", nach, len(fehlend))
		}
	}
	// Nach dem Abbruch läuft der nächste Durchlauf ganz normal.
	sc.Bremse = nil
	if _, err := sc.Scan(context.Background(), *lib); err != nil {
		t.Fatal(err)
	}
}

// Der Scanner findet einen Titel, den es schon gibt, auch wenn er mit
// anderen Umlauten in Gross- und Kleinschreibung daherkommt — wie früher
// über die Volltextsuche, jetzt über titel_klein.
func TestGleicherTitelMitUmlautWirdFassung(t *testing.T) {
	root := t.TempDir()
	dateienHinlegen(t, root,
		"Ärger im Paradies (2001)/Ärger im Paradies (2001).mkv",
		"ÄRGER IM PARADIES (2001)/ÄRGER IM PARADIES (2001) 720p.mkv",
	)
	st, err := store.Open(filepath.Join(t.TempDir(), "l.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	lib, _ := st.CreateLibrary(ctx, "Filme", "movie", root, "de")
	res, err := New(st, nil).Scan(ctx, *lib)
	if err != nil {
		t.Fatal(err)
	}
	if res.Added != 1 {
		t.Fatalf("erwartet ein Film mit zwei Fassungen, bekam %d neue Titel", res.Added)
	}
}
