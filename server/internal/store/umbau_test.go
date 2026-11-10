// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Vor einem Umbau der Datenbank liegt eine Sicherung daneben, nur für den
// Besitzer lesbar. Eine neue Datenbank braucht keine.
func TestSicherungVorDemUmbau(t *testing.T) {
	pfad := filepath.Join(t.TempDir(), "lumen.db")
	st, err := Open(pfad)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(pfad + ".vor-umbau"); err == nil {
		t.Fatal("neue Datenbank hat eine Sicherung bekommen")
	}
	// So sieht eine Datenbank einer älteren Fassung aus.
	if _, err := st.DB().Exec(`PRAGMA user_version = 0`); err != nil {
		t.Fatal(err)
	}
	_ = st.Close()

	st, err = Open(pfad)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	info, err := os.Stat(pfad + ".vor-umbau")
	if err != nil {
		t.Fatalf("keine Sicherung vor dem Umbau: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("Sicherung hat Rechte %v, erwartet 0600", info.Mode().Perm())
	}
	var stand int
	_ = st.DB().QueryRow(`PRAGMA user_version`).Scan(&stand)
	if stand != SchemaStand {
		t.Fatalf("Stand %d, erwartet %d", stand, SchemaStand)
	}
}

// Eine Datenbank einer neueren Fassung fasst eine ältere nicht an.
func TestNeuereDatenbankWirdNichtAngefasst(t *testing.T) {
	pfad := filepath.Join(t.TempDir(), "lumen.db")
	st, err := Open(pfad)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = st.DB().Exec(`PRAGMA user_version = 999`)
	_ = st.Close()
	if _, err := Open(pfad); err == nil || !strings.Contains(err.Error(), "neueren") {
		t.Fatalf("erwartet Fehler wegen neuerer Fassung, bekam %v", err)
	}
}

// Eine kaputte Datenbank: Lumen startet nicht.
func TestKaputteDatenbankStartetNicht(t *testing.T) {
	pfad := filepath.Join(t.TempDir(), "lumen.db")
	st, err := Open(pfad)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = st.DB().Exec(`PRAGMA wal_checkpoint(TRUNCATE)`)
	_ = st.Close()
	b, _ := os.ReadFile(pfad)
	// Seite 2 überschreiben (die Kopfseite bleibt heil, sonst ist es gar keine Datenbank).
	for i := 4096 + 100; i < len(b) && i < 4096*3; i++ {
		b[i] = 0xAB
	}
	_ = os.WriteFile(pfad, b, 0o644)
	if _, err := Open(pfad); err == nil {
		t.Fatal("kaputte Datenbank wurde geöffnet")
	}
}

// Eine Datenbank aus der Zeit, als der Fortschritt sekundengenau die
// Uhrzeit trug: Nach dem Umbau steht keine Uhrzeit mehr drin, und
// „Weiterschauen" hat dieselbe Reihenfolge wie vorher.
func TestFortschrittVerliertDieUhrzeit(t *testing.T) {
	pfad := filepath.Join(t.TempDir(), "lumen.db")
	st, err := Open(pfad)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	acc, err := st.CreateAccount(ctx, NewAccount{Name: "Lea"})
	if err != nil {
		t.Fatal(err)
	}
	lib, _ := st.CreateLibrary(ctx, "Filme", "movie", t.TempDir(), "de")
	var ids []string
	for _, titel := range []string{"A", "B", "C"} {
		it := &Item{LibraryID: lib.ID, Kind: "movie", Title: titel, MatchState: "pending"}
		if err := st.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, it.ID)
	}
	db := st.DB()
	// Zurück auf den alten Aufbau, mit Uhrzeiten: B zuletzt, dann C, dann A.
	for _, q := range []string{
		`ALTER TABLE progress RENAME COLUMN reihenfolge TO updated_at`,
		`DELETE FROM progress_reihe`,
		`PRAGMA user_version = 1`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	for i, at := range []int64{1_761_000_100, 1_761_000_900, 1_761_000_500} {
		if _, err := db.Exec(`INSERT INTO progress (account_id, item_id, position_ms, duration_ms, finished, updated_at)
		                      VALUES (?,?,120000,5400000,0,?)`, acc.ID, ids[i], at); err != nil {
			t.Fatal(err)
		}
	}
	_, _ = db.Exec(`UPDATE token SET last_seen_at = 1761000123`)
	_ = st.Close()

	st, err = Open(pfad)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	got, _ := st.ResumeIDs(ctx, acc.ID, 10)
	if len(got) != 3 || got[0] != ids[1] || got[1] != ids[2] || got[2] != ids[0] {
		t.Fatalf("Reihenfolge verloren: %v (erwartet B, C, A)", got)
	}
	rows, _ := st.DB().Query(`SELECT reihenfolge FROM progress WHERE account_id = ?`, acc.ID)
	for rows.Next() {
		var n int64
		_ = rows.Scan(&n)
		if n > 1000 {
			t.Fatalf("noch eine Uhrzeit im Fortschritt: %d", n)
		}
	}
	rows.Close()

	// Weiterschauen: A rückt nach vorn. Derselbe Titel immer wieder
	// gespeichert verändert die Nummer nicht.
	_ = st.SaveProgress(ctx, acc.ID, ids[0], 200000, 5400000)
	var r1, r2 int64
	_ = st.DB().QueryRow(`SELECT reihenfolge FROM progress WHERE account_id = ? AND item_id = ?`, acc.ID, ids[0]).Scan(&r1)
	for i := 0; i < 5; i++ {
		_ = st.SaveProgress(ctx, acc.ID, ids[0], int64(300000+i*1000), 5400000)
	}
	_ = st.DB().QueryRow(`SELECT reihenfolge FROM progress WHERE account_id = ? AND item_id = ?`, acc.ID, ids[0]).Scan(&r2)
	if r1 != r2 || r1 != 4 {
		t.Fatalf("Nummer zählt mit der Sehzeit: %d → %d (erwartet 4 → 4)", r1, r2)
	}
	got, _ = st.ResumeIDs(ctx, acc.ID, 10)
	if got[0] != ids[0] {
		t.Fatalf("zuletzt geschaut nicht vorn: %v", got)
	}
}

// Geräte merken sich nur den Tag, an dem sie zuletzt da waren.
func TestGeraetNurMitTag(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "lumen.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := t.Context()
	acc, _ := st.CreateAccount(ctx, NewAccount{Name: "Lea"})
	tok, _ := st.IssueToken(ctx, acc.ID, "iPad von Lea", "tablet")
	if _, _, err := st.LookupToken(ctx, tok); err != nil {
		t.Fatal(err)
	}
	var c, l int64
	_ = st.DB().QueryRow(`SELECT created_at, last_seen_at FROM token`).Scan(&c, &l)
	if c%86400 != 0 || l%86400 != 0 {
		t.Fatalf("Gerät mit Uhrzeit gespeichert: %d %d", c, l)
	}
}

// Eine Folge ohne eigene Freigabe erbt die der Serie — sonst sähe ein
// Profil mit Altersgrenze über „nur Folgen" alles.
func TestFolgeErbtFreigabe(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "lumen.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := t.Context()
	lib, _ := st.CreateLibrary(ctx, "Serien", "series", t.TempDir(), "de")
	ab18 := 18
	serie := &Item{LibraryID: lib.ID, Kind: "series", Title: "Hart", AgeYears: &ab18, MatchState: "matched"}
	_ = st.UpsertItem(ctx, serie)
	staffel := &Item{LibraryID: lib.ID, Kind: "season", Title: "Staffel 1", ParentID: serie.ID, MatchState: "matched"}
	_ = st.UpsertItem(ctx, staffel)
	folge := &Item{LibraryID: lib.ID, Kind: "episode", Title: "Folge 1", ParentID: staffel.ID, MatchState: "matched"}
	_ = st.UpsertItem(ctx, folge)
	got, err := st.Query(ctx, Query{Libraries: []string{lib.ID}, Kinds: []string{"episode", "season"},
		MaxAge: 12, AllowUnrated: true, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("Profil ab 12 sieht %d Folgen/Staffeln einer Serie ab 18", len(got))
	}
}

// Scheitert ein Umbau mittendrin, bleibt die Sicherung von vor dem Update
// beim nächsten Start, wie sie ist — sie wird nicht vom halb umgebauten
// Stand überschrieben. Der nächste Start macht beim erreichten Schritt
// weiter.
func TestGescheiterterUmbauBehaeltSicherung(t *testing.T) {
	pfad := filepath.Join(t.TempDir(), "lumen.db")
	st, err := Open(pfad)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB().Exec(`PRAGMA user_version = 8`); err != nil {
		t.Fatal(err)
	}
	_ = st.SetSetting(t.Context(), "probe", "vor dem update")
	_ = st.Close()

	umbauSchrittFertig = func(n int) error {
		if n == 9 {
			return os.ErrInvalid // nach Schritt 9 scheitert es
		}
		return nil
	}
	defer func() { umbauSchrittFertig = nil }()
	if _, err := Open(pfad); err == nil {
		t.Fatal("Umbau sollte scheitern")
	}
	sicherung, err := os.ReadFile(pfad + ".vor-umbau")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(pfad + umbauOffenEndung); err != nil {
		t.Fatalf("Marke fehlt nach dem gescheiterten Umbau: %v", err)
	}
	// Schritt 9 ist festgeschrieben.
	db, err := sql.Open("sqlite3", pfad)
	if err != nil {
		t.Fatal(err)
	}
	var stand int
	_ = db.QueryRow(`PRAGMA user_version`).Scan(&stand)
	_, _ = db.Exec(`INSERT INTO setting (key, value) VALUES ('probe', 'halb umgebaut')
	                ON CONFLICT (key) DO UPDATE SET value = excluded.value`)
	_ = db.Close()
	if stand != 9 {
		t.Fatalf("Stand nach dem gescheiterten Schritt 10: %d, erwartet 9", stand)
	}

	umbauSchrittFertig = nil
	st, err = Open(pfad)
	if err != nil {
		t.Fatalf("zweiter Anlauf: %v", err)
	}
	defer st.Close()
	nachher, err := os.ReadFile(pfad + ".vor-umbau")
	if err != nil {
		t.Fatal(err)
	}
	if string(nachher) != string(sicherung) {
		t.Fatal("die saubere Sicherung wurde überschrieben")
	}
	if _, err := os.Stat(pfad + umbauOffenEndung); err == nil {
		t.Fatal("Marke liegt nach dem gelungenen Umbau noch da")
	}
	alt, err := sql.Open("sqlite3", pfad+".vor-umbau")
	if err != nil {
		t.Fatal(err)
	}
	defer alt.Close()
	var probe string
	_ = alt.QueryRow(`SELECT value FROM setting WHERE key = 'probe'`).Scan(&probe)
	_ = alt.QueryRow(`PRAGMA user_version`).Scan(&stand)
	if probe != "vor dem update" || stand != 8 {
		t.Fatalf("Sicherung: %q Stand %d", probe, stand)
	}
}
