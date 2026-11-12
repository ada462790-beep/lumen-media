// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
	_ "time/tzdata" // Europe/Zurich auch ohne Zeitzonen-Datenbank auf dem Rechner
)

// „Zuletzt hinzugefügt": Eine alte Serie, zu der eine neue Folge kommt,
// steht wieder vorn — jetzt über zuletzt_neu statt über eine Unterabfrage
// je Serie.
func TestNeueFolgeHoltSerieNachVorn(t *testing.T) {
	st := testStore(t)
	ctx := t.Context()
	lib, _ := st.CreateLibrary(ctx, "Serien", "series", t.TempDir(), "de")
	anlegen := func(it *Item) *Item {
		t.Helper()
		if err := st.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
		return it
	}
	alt := anlegen(&Item{LibraryID: lib.ID, Kind: "series", Title: "Alt"})
	staffel := anlegen(&Item{LibraryID: lib.ID, Kind: "season", ParentID: alt.ID, Title: "Staffel 1", SeasonNo: 1})
	anlegen(&Item{LibraryID: lib.ID, Kind: "episode", ParentID: staffel.ID, Title: "Folge 1", SeasonNo: 1, EpisodeNo: 1})
	neu := anlegen(&Item{LibraryID: lib.ID, Kind: "series", Title: "Neu"})
	// Zeitstempel sind sekundengenau: die Reihenfolge von Hand festlegen.
	_, _ = st.DB().Exec(`UPDATE item SET added_at = 100, zuletzt_neu = 100 WHERE library_id = ? AND id <> ?`, lib.ID, neu.ID)
	_, _ = st.DB().Exec(`UPDATE item SET added_at = 200, zuletzt_neu = 200 WHERE id = ?`, neu.ID)

	reihe := func() string {
		got, err := st.Query(ctx, Query{Libraries: []string{lib.ID}, Kinds: []string{"series"}, Sort: "added"})
		if err != nil {
			t.Fatal(err)
		}
		var n []string
		for _, it := range got {
			n = append(n, it.Title)
		}
		return strings.Join(n, ",")
	}
	if r := reihe(); r != "Neu,Alt" {
		t.Fatalf("vorher: %s", r)
	}
	ep := anlegen(&Item{LibraryID: lib.ID, Kind: "episode", ParentID: staffel.ID, Title: "Folge 2", SeasonNo: 1, EpisodeNo: 2})
	if r := reihe(); r != "Alt,Neu" {
		t.Fatalf("nach neuer Folge: %s", r)
	}
	// Die Folge noch einmal speichern (Metadaten) ändert die Serie nicht.
	var vorher int64
	_ = st.DB().QueryRow(`SELECT zuletzt_neu FROM item WHERE id = ?`, alt.ID).Scan(&vorher)
	ep.Overview = "Jetzt mit Beschreibung"
	anlegen(ep)
	var nachher int64
	_ = st.DB().QueryRow(`SELECT zuletzt_neu FROM item WHERE id = ?`, alt.ID).Scan(&nachher)
	if nachher != vorher {
		t.Fatalf("Metadaten einer Folge haben die Serie nach vorn geholt: %d → %d", vorher, nachher)
	}
}

// Die Übersicht zählt Filme in Teilen und fehlende Dateien in der
// Datenbank; das muss zu den Listen passen, die „Aufräumen" zeigt.
// Fassungen eines Films (alle Teil 1) sind kein Doppel.
func TestDoppelUndFehlendZahl(t *testing.T) {
	st := testStore(t)
	ctx := t.Context()
	lib, _ := st.CreateLibrary(ctx, "Filme", "movie", t.TempDir(), "de")
	fassungen := false
	film := func(titel string, dateien ...bool) {
		it := &Item{LibraryID: lib.ID, Kind: "movie", Title: titel}
		if err := st.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
		for i, fehlt := range dateien {
			teil := i + 1
			if fassungen {
				teil = 1
			}
			f := &MediaFile{ItemID: it.ID, Path: "/f/" + titel + string(rune('a'+i)), SizeBytes: 1, PartNo: teil}
			if err := st.UpsertFile(ctx, f); err != nil {
				t.Fatal(err)
			}
			if fehlt {
				_, _ = st.DB().Exec(`UPDATE media_file SET missing_since = 1 WHERE id = ?`, f.ID)
			}
		}
	}
	film("Einer", false)
	film("Zwei", false, false)
	film("Drei mit einem fehlenden", false, false, true)
	film("Zwei, einer fehlt", false, true)
	film("Beide fehlen", true, true)
	fassungen = true
	film("MKV und AVI", false, false)

	dups, _ := st.Duplicates(ctx)
	n, err := st.DoppelZahl(ctx)
	if err != nil || n != len(dups) || n != 2 {
		t.Fatalf("DoppelZahl %d (%v), Duplicates %d, erwartet 2", n, err, len(dups))
	}
	fehlend, _ := st.MissingFiles(ctx)
	m, err := st.FehlendZahl(ctx)
	if err != nil || m != len(fehlend) || m != 4 {
		t.Fatalf("FehlendZahl %d (%v), MissingFiles %d, erwartet 4", m, err, len(fehlend))
	}
}

// Die feste Uhrzeit gilt in der Zeitzone, die Lumen hat (time.Local aus
// TZ) — nicht in UTC. Um 04:30 in Zürich ist es erst 03:30 UTC, und der
// Durchlauf „um 4 Uhr" ist trotzdem fällig; um 03:30 Zürcher Zeit
// (02:30 UTC, im Sommer 01:30 UTC) nicht. Auch am Tag der Zeitumstellung.
func TestFaelligInDerZeitzone(t *testing.T) {
	zuerich, err := time.LoadLocation("Europe/Zurich")
	if err != nil {
		t.Fatal(err)
	}
	for _, tag := range []int{29, 30} { // 29.3.2026: Umstellung auf Sommerzeit
		gestern := time.Date(2026, 3, tag-1, 23, 0, 0, 0, zuerich).UTC() // wie aus der Datenbank
		l := Library{ScanAt: "04:00", ScannedAt: &gestern}
		if !faellig(l, time.Date(2026, 3, tag, 4, 30, 0, 0, zuerich)) {
			t.Errorf("%d.3. 04:30 Zürich: sollte fällig sein", tag)
		}
		if faellig(l, time.Date(2026, 3, tag, 3, 30, 0, 0, zuerich)) {
			t.Errorf("%d.3. 03:30 Zürich: noch nicht fällig", tag)
		}
	}
}

// Schreiben zwei zugleich — ein Durchlauf und eine Wiedergabe, die den
// Stand speichert —, wartet der zweite, statt mit „database is locked"
// (SQLITE_BUSY) zu scheitern: WAL, busy_timeout und _txlock=immediate
// stehen auf jeder Verbindung des Pools.
func TestGleichzeitigesSchreibenWartet(t *testing.T) {
	st := testStore(t)
	ctx := t.Context()
	var modus string
	var warten int
	conn, err := st.DB().Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.QueryRowContext(ctx, `PRAGMA journal_mode`).Scan(&modus)
	_ = conn.QueryRowContext(ctx, `PRAGMA busy_timeout`).Scan(&warten)
	_ = conn.Close()
	if modus != "wal" || warten < 5000 {
		t.Fatalf("journal_mode=%s busy_timeout=%d, erwartet wal und mindestens 5000", modus, warten)
	}

	lib, _ := st.CreateLibrary(ctx, "Filme", "movie", t.TempDir(), "de")
	drin := make(chan struct{})
	fertig := make(chan error, 1)
	go func() {
		fertig <- st.tx(ctx, func(tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, `UPDATE library SET name = 'Filme!' WHERE id = ?`, lib.ID); err != nil {
				return err
			}
			close(drin)
			time.Sleep(700 * time.Millisecond) // ein langsamer Schreiber
			return nil
		})
	}()
	<-drin
	// Lesen geht derweil (WAL) …
	if _, err := st.Libraries(ctx); err != nil {
		t.Fatalf("Lesen während einer Schreib-Transaktion: %v", err)
	}
	// … und Schreiben wartet, bis die andere fertig ist.
	if err := st.UpsertItem(ctx, &Item{LibraryID: lib.ID, Kind: "movie", Title: "Nordlicht"}); err != nil {
		t.Fatalf("zweiter Schreiber scheiterte: %v", err)
	}
	if err := <-fertig; err != nil {
		t.Fatal(err)
	}
}

// Ein Stapel schreibt alles oder nichts — und was über s.tx schreibt
// (Titel, Datei samt Spuren), landet in seiner einen Transaktion.
func TestStapel(t *testing.T) {
	st := testStore(t)
	ctx := t.Context()
	lib, _ := st.CreateLibrary(ctx, "Filme", "movie", t.TempDir(), "de")
	schreiben := func(st *Store, titel string) error {
		it := &Item{LibraryID: lib.ID, Kind: "movie", Title: titel}
		if err := st.UpsertItem(ctx, it); err != nil {
			return err
		}
		return st.UpsertFile(ctx, &MediaFile{ItemID: it.ID, Path: "/f/" + titel, SizeBytes: 1,
			Spuren: []Track{{Index: 1, Kind: "audio", Language: "de"}}, Kapitel: []Chapter{}})
	}
	err := st.Stapel(ctx, func(st *Store) error {
		for _, titel := range []string{"A", "B", "C"} {
			if err := schreiben(st, titel); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := st.Count(ctx, Query{Libraries: []string{lib.ID}}); n != 3 {
		t.Fatalf("%d Titel nach dem Stapel, erwartet 3", n)
	}
	if g := st.GemessenZuPfad(ctx, "/f/B"); g == nil || !g.Gelesen {
		t.Fatalf("Datei samt Spuren fehlt: %+v", g)
	}
	// Scheitert etwas, bleibt nichts davon.
	err = st.Stapel(ctx, func(st *Store) error {
		if err := schreiben(st, "D"); err != nil {
			return err
		}
		return errors.New("mittendrin schiefgegangen")
	})
	if err == nil {
		t.Fatal("Fehler ging verloren")
	}
	if n, _ := st.Count(ctx, Query{Libraries: []string{lib.ID}}); n != 3 {
		t.Fatalf("nach dem gescheiterten Stapel %d Titel, erwartet 3", n)
	}
	if g := st.GemessenZuPfad(ctx, "/f/D"); g != nil {
		t.Fatal("Datei aus dem gescheiterten Stapel ist geblieben")
	}
}

// Nach dem Öffnen und nach Optimieren hat der Abfrageplaner eine
// Statistik — ohne sie wählte er für „neue Folgen dieser Serien" den
// Index über kind und las alle Folgen.
func TestStatistikFuerDenPlaner(t *testing.T) {
	pfad := filepath.Join(t.TempDir(), "lumen.db")
	st, err := Open(pfad)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	lib, _ := st.CreateLibrary(ctx, "Serien", "series", t.TempDir(), "de")
	for i := 0; i < 50; i++ {
		_ = st.UpsertItem(ctx, &Item{LibraryID: lib.ID, Kind: "series", Title: "S" + string(rune('A'+i%26)) + string(rune('a'+i/26))})
	}
	if err := st.Optimieren(ctx); err != nil {
		t.Fatal(err)
	}
	var n int
	_ = st.DB().QueryRow(`SELECT count(*) FROM sqlite_stat1 WHERE tbl = 'item'`).Scan(&n)
	if n == 0 {
		t.Fatal("keine Statistik für item")
	}
	_ = st.Close()
	if st, err = Open(pfad); err != nil {
		t.Fatal(err)
	}
	_ = st.Close()
}
