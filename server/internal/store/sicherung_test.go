// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

import (
	"os"
	"path/filepath"
	"testing"
)

// Genau zwei Generationen, nur für den Besitzer lesbar — und eine davon
// lässt sich beim nächsten Start wiederherstellen.
func TestSicherungZweiGenerationenUndWiederherstellen(t *testing.T) {
	dir := t.TempDir()
	pfad := filepath.Join(dir, "lumen.db")
	ordner := filepath.Join(dir, "sicherungen")
	st, err := Open(pfad)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	for _, name := range []string{"Erste", "Zweite", "Dritte", "Vierte"} {
		if _, err := st.CreateAccount(ctx, NewAccount{Name: name}); err != nil {
			t.Fatal(err)
		}
		if err := st.Sichern(ctx, ordner); err != nil {
			t.Fatal(err)
		}
	}
	liste := Sicherungen(ordner)
	if len(liste) != 2 {
		t.Fatalf("%d Sicherungen, erwartet genau 2", len(liste))
	}
	eintraege, _ := os.ReadDir(ordner)
	if len(eintraege) != 2 {
		t.Fatalf("im Ordner liegen %d Dateien, erwartet 2", len(eintraege))
	}
	for _, e := range eintraege {
		fi, _ := e.Info()
		if fi.Mode().Perm() != 0o600 {
			t.Fatalf("%s hat Rechte %v", e.Name(), fi.Mode().Perm())
		}
	}
	// Nach der Sicherung kommt noch ein Profil dazu …
	_, _ = st.CreateAccount(ctx, NewAccount{Name: "Fünfte"})
	// … dann die vorletzte Sicherung (mit drei Profilen) wiederherstellen.
	if err := Wiederherstellen(ordner, 2, pfad); err != nil {
		t.Fatal(err)
	}
	_ = st.Close()
	st, err = Open(pfad)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	accs, _ := st.Accounts(ctx)
	if len(accs) != 3 {
		t.Fatalf("nach dem Wiederherstellen %d Profile, erwartet 3", len(accs))
	}
	if _, err := os.Stat(pfad + ".vor-wiederherstellen"); err == nil {
		t.Fatal("die ersetzte Datenbank liegt noch herum")
	}
}

// Ein leeres Doppel einer Serie geht in der Serie mit den Dateien auf, und
// der Stand der Person wandert mit (Issue #7).
func TestLeereSerieGehtAuf(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "lumen.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := t.Context()
	lib, _ := st.CreateLibrary(ctx, "Serien", "series", t.TempDir(), "de")
	acc, _ := st.CreateAccount(ctx, NewAccount{Name: "Lea"})
	baue := func(titel string, mitDatei bool) (serie, folge string) {
		s := &Item{LibraryID: lib.ID, Kind: "series", Title: titel, OriginalTitle: "9-1-1", MatchState: "matched"}
		_ = st.UpsertItem(ctx, s)
		sn := &Item{LibraryID: lib.ID, Kind: "season", Title: "Staffel 1", ParentID: s.ID, SeasonNo: 1, MatchState: "matched"}
		_ = st.UpsertItem(ctx, sn)
		e := &Item{LibraryID: lib.ID, Kind: "episode", Title: "Pilot", ParentID: sn.ID, SeasonNo: 1, EpisodeNo: 1, MatchState: "matched"}
		_ = st.UpsertItem(ctx, e)
		if mitDatei {
			_ = st.UpsertFile(ctx, &MediaFile{ItemID: e.ID, Path: "/serien/9-1-1/S01E01.mkv", DurationMS: 2_400_000})
		}
		return s.ID, e.ID
	}
	_, alteFolge := baue("9-1-1", false)
	neueSerie, neueFolge := baue("9-1-1 Notruf L.A.", true)
	if err := st.SaveProgress(ctx, acc.ID, alteFolge, 600_000, 2_400_000); err != nil {
		t.Fatal(err)
	}
	n, err := st.LeereSerienZusammenfuehren(ctx)
	if err != nil || n != 1 {
		t.Fatalf("zusammengeführt: %d, %v", n, err)
	}
	serien, _ := st.Query(ctx, Query{Libraries: []string{lib.ID}, Kinds: []string{"series"}, Limit: 10})
	if len(serien) != 1 || serien[0].ID != neueSerie {
		t.Fatalf("übrig: %+v", serien)
	}
	p, err := st.Progress(ctx, acc.ID, neueFolge)
	if err != nil || p.PositionMS != 600_000 {
		t.Fatalf("Stand nicht mitgewandert: %+v %v", p, err)
	}
}

// Zwei Serien mit Dateien, laut TMDb dieselbe: Sie werden eine, Staffeln
// und Folgen ziehen um, eine doppelte Folge wird eine zweite Fassung, und
// der Stand der Person wandert mit.
func TestSerienMitDateienGleicherKennungWerdenEine(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "lumen.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := t.Context()
	lib, _ := st.CreateLibrary(ctx, "Serien", "series", t.TempDir(), "de")
	acc, _ := st.CreateAccount(ctx, NewAccount{Name: "Lea"})
	folge := func(serie string, staffel, nr int, pfad string) string {
		var sn *Item
		kinder, _ := st.Children(ctx, serie)
		for i := range kinder {
			if kinder[i].SeasonNo == staffel {
				sn = &kinder[i]
			}
		}
		if sn == nil {
			sn = &Item{LibraryID: lib.ID, Kind: "season", ParentID: serie, SeasonNo: staffel, Title: "Staffel", MatchState: "matched"}
			_ = st.UpsertItem(ctx, sn)
		}
		e := &Item{LibraryID: lib.ID, Kind: "episode", ParentID: sn.ID, SeasonNo: staffel, EpisodeNo: nr, Title: "F", MatchState: "matched"}
		_ = st.UpsertItem(ctx, e)
		_ = st.UpsertFile(ctx, &MediaFile{ItemID: e.ID, Path: pfad, DurationMS: 1000})
		return e.ID
	}
	gross := &Item{LibraryID: lib.ID, Kind: "series", Title: "Grey's Anatomy", MatchState: "matched"}
	klein := &Item{LibraryID: lib.ID, Kind: "series", Title: "Grey's Anatomy", MatchState: "matched"}
	_ = st.UpsertItem(ctx, gross)
	_ = st.UpsertItem(ctx, klein)
	for _, id := range []string{gross.ID, klein.ID} {
		if _, err := st.db.ExecContext(ctx, `UPDATE item SET tmdb_id = '1416' WHERE id = ?`, id); err != nil {
			t.Fatal(err)
		}
	}
	folge(gross.ID, 1, 1, "/s/a/s01e01.mkv")
	folge(gross.ID, 1, 2, "/s/a/s01e02.mkv")
	doppelt := folge(klein.ID, 1, 2, "/s/b/s01e02.avi")
	folge(klein.ID, 1, 3, "/s/b/s01e03.avi")
	folge(klein.ID, 8, 1, "/s/b/s08e01.avi")
	if err := st.SaveProgress(ctx, acc.ID, doppelt, 500, 1000); err != nil {
		t.Fatal(err)
	}

	if _, err := st.SerienZusammenfuehren(ctx); err != nil {
		t.Fatal(err)
	}
	serien, _ := st.Query(ctx, Query{Libraries: []string{lib.ID}, Kinds: []string{"series"}, Limit: 10})
	// Die mit mehr Dateien (klein hat drei, gross zwei) bleibt.
	if len(serien) != 1 || serien[0].ID != klein.ID {
		t.Fatalf("erwartet nur die Serie mit mehr Dateien, da sind %d", len(serien))
	}
	staffeln, _ := st.Children(ctx, klein.ID)
	if len(staffeln) != 2 {
		t.Fatalf("%d Staffeln statt 2", len(staffeln))
	}
	var dateien int
	_ = st.db.QueryRowContext(ctx, `SELECT count(*) FROM media_file`).Scan(&dateien)
	if dateien != 5 {
		t.Errorf("%d Dateien statt 5 — beim Zusammenführen ging etwas verloren", dateien)
	}
	for _, s := range staffeln {
		folgen, _ := st.Children(ctx, s.ID)
		if s.SeasonNo == 1 && len(folgen) != 3 {
			t.Errorf("Staffel 1: %d Folgen statt 3", len(folgen))
		}
		for _, f := range folgen {
			if f.EpisodeNo == 2 && s.SeasonNo == 1 {
				fs, _ := st.FilesFor(ctx, f.ID)
				if len(fs) != 2 {
					t.Errorf("Folge 2 hat %d Fassungen statt 2", len(fs))
				}
				var pos int64
				_ = st.db.QueryRowContext(ctx, `SELECT position_ms FROM progress WHERE account_id = ? AND item_id = ?`, acc.ID, f.ID).Scan(&pos)
				if pos != 500 {
					t.Errorf("Stand nicht mitgewandert: %d", pos)
				}
			}
		}
	}
}

func TestTomatenWerdenGemerktUndNurEinmalGefragt(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "lumen.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := t.Context()
	lib, _ := st.CreateLibrary(ctx, "Filme", "movie", t.TempDir(), "de")
	a := &Item{LibraryID: lib.ID, Kind: "movie", Title: "A", IMDbID: "tt1", MatchState: "matched"}
	b := &Item{LibraryID: lib.ID, Kind: "movie", Title: "B", IMDbID: "tt2", MatchState: "matched"}
	_ = st.UpsertItem(ctx, a)
	_ = st.UpsertItem(ctx, b)
	if l, _ := st.OhneTomaten(ctx, 10); len(l) != 2 {
		t.Fatalf("%d statt 2 offen", len(l))
	}
	_ = st.TomatenSetzen(ctx, a.ID, 93)
	_ = st.TomatenSetzen(ctx, b.ID, -1)
	if l, _ := st.OhneTomaten(ctx, 10); len(l) != 0 {
		t.Fatalf("%d noch offen", len(l))
	}
	_ = st.UpsertItem(ctx, a) // ein Scan darf die Wertung nicht löschen
	x, _ := st.Item(ctx, a.ID)
	if x.Tomaten == nil || *x.Tomaten != 93 {
		t.Fatalf("Wertung weg: %v", x.Tomaten)
	}
	y, _ := st.Item(ctx, b.ID)
	if y.Tomaten != nil {
		t.Fatal("„keine Wertung“ wird als Zahl gezeigt")
	}
}

func TestSerienBekommenIMDbNachgetragen(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "lumen.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := t.Context()
	lib, _ := st.CreateLibrary(ctx, "Serien", "series", t.TempDir(), "de")
	s := &Item{LibraryID: lib.ID, Kind: "series", Title: "Bosch: Legacy", MatchState: "matched"}
	_ = st.UpsertItem(ctx, s)
	if l, _ := st.SerienOhneIMDb(ctx, 10); len(l) != 1 {
		t.Fatalf("%d offen", len(l))
	}
	if err := st.WertungenSetzen(ctx, s.ID, "tt14582582", 7.9, 30000, 95); err != nil {
		t.Fatal(err)
	}
	x, _ := st.Item(ctx, s.ID)
	if x.IMDbID != "tt14582582" || x.IMDbRating != 7.9 || x.Tomaten == nil || *x.Tomaten != 95 {
		t.Fatalf("%+v", x)
	}
	if l, _ := st.SerienOhneIMDb(ctx, 10); len(l) != 0 {
		t.Fatal("kommt wieder")
	}
}

func TestGesehenListe(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "lumen.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := t.Context()
	lib, _ := st.CreateLibrary(ctx, "M", "movie", t.TempDir(), "de")
	a, _ := st.CreateAccount(ctx, NewAccount{Name: "Lea"})
	b, _ := st.CreateAccount(ctx, NewAccount{Name: "Tom"})
	film := &Item{LibraryID: lib.ID, Kind: "movie", Title: "F"}
	serie := &Item{LibraryID: lib.ID, Kind: "series", Title: "S"}
	_ = st.UpsertItem(ctx, film)
	_ = st.UpsertItem(ctx, serie)
	stf := &Item{LibraryID: lib.ID, Kind: "season", ParentID: serie.ID, SeasonNo: 1, Title: "1"}
	_ = st.UpsertItem(ctx, stf)
	e1 := &Item{LibraryID: lib.ID, Kind: "episode", ParentID: stf.ID, SeasonNo: 1, EpisodeNo: 1, Title: "1"}
	e2 := &Item{LibraryID: lib.ID, Kind: "episode", ParentID: stf.ID, SeasonNo: 1, EpisodeNo: 2, Title: "2"}
	_ = st.UpsertItem(ctx, e1)
	_ = st.UpsertItem(ctx, e2)
	_ = st.MarkFinished(ctx, a.ID, film.ID, 1000)
	_ = st.MarkFinished(ctx, a.ID, e1.ID, 1000)
	if ids, _ := st.GesehenIDs(ctx, a.ID); len(ids) != 1 || ids[0] != film.ID {
		t.Fatalf("halbe Serie zählt mit: %v", ids)
	}
	_ = st.MarkFinished(ctx, a.ID, e2.ID, 1000)
	if ids, _ := st.GesehenIDs(ctx, a.ID); len(ids) != 2 || ids[0] != serie.ID {
		t.Fatalf("Serie fehlt oder falsche Reihenfolge: %v", ids)
	}
	if ids, _ := st.GesehenIDs(ctx, b.ID); len(ids) != 0 {
		t.Fatalf("fremdes Konto sieht etwas: %v", ids)
	}
}
