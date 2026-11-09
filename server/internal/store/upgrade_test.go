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

// Datenbanken älterer Fassungen, so wie diese sie angelegt haben
// (testdata/alt_*.sql: schema.sql und nachruesten() aus dem git-Verlauf,
// dazu ein paar Zeilen Inhalt). Lumen muss mit jeder davon starten, und
// danach muss alles da sein: Fortschritt in der alten Reihenfolge,
// Merkliste, Titel — und die neuen Spalten gefüllt.
func TestUpgradeAlterDatenbanken(t *testing.T) {
	dumps, _ := filepath.Glob("testdata/alt_*.sql")
	if len(dumps) < 5 {
		t.Fatalf("nur %d alte Fassungen in testdata gefunden", len(dumps))
	}
	for _, dump := range dumps {
		name := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(dump), "alt_"), ".sql")
		t.Run(name, func(t *testing.T) { upgradePruefen(t, dump, nil) })
		// Dieselbe Datenbank, aber ohne den Index progress_resume — so,
		// wie sie etwa nach einem Zurückspielen von Hand aussehen kann.
		// Früher scheiterte der Start dann an schema.sql („no such
		// column: reihenfolge"), weil der Index vor dem Umbau angelegt
		// wurde.
		t.Run(name+"-ohne-index", func(t *testing.T) {
			upgradePruefen(t, dump, []string{`DROP INDEX IF EXISTS progress_resume`})
		})
	}
}

func upgradePruefen(t *testing.T, dump string, vorher []string) {
	raw, err := os.ReadFile(dump)
	if err != nil {
		t.Fatal(err)
	}
	pfad := filepath.Join(t.TempDir(), "lumen.db")
	db, err := sql.Open("sqlite3", pfad)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(raw)); err != nil {
		t.Fatalf("alte Datenbank anlegen: %v", err)
	}
	for _, q := range vorher {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	_ = db.Close()

	st, err := Open(pfad)
	if err != nil {
		t.Fatalf("Lumen startet nicht mit dieser Datenbank: %v", err)
	}
	defer st.Close()
	ctx := t.Context()

	var stand int
	_ = st.DB().QueryRow(`PRAGMA user_version`).Scan(&stand)
	if stand != SchemaStand {
		t.Fatalf("Stand %d, erwartet %d", stand, SchemaStand)
	}
	// Fortschritt: in der alten Reihenfolge, ohne Uhrzeit.
	weiter, err := st.DB().Query(`SELECT item_id FROM progress WHERE account_id = 'acc_lea' ORDER BY reihenfolge DESC`)
	if err != nil {
		t.Fatal(err)
	}
	ids, _ := collectStrings(weiter)
	if strings.Join(ids, ",") != "it_e1,it_f0" {
		t.Fatalf("Weiterschauen in falscher Reihenfolge: %v", ids)
	}
	var idx int
	_ = st.DB().QueryRow(`SELECT count(*) FROM sqlite_master WHERE type = 'index' AND name = 'progress_resume'
	                       AND sql LIKE '%reihenfolge%'`).Scan(&idx)
	if idx != 1 {
		t.Fatal("Index progress_resume fehlt nach dem Umbau")
	}
	var merk int
	_ = st.DB().QueryRow(`SELECT count(*) FROM watchlist WHERE account_id = 'acc_ben'`).Scan(&merk)
	if merk != 1 {
		t.Fatal("Merkliste ging beim Umbau verloren")
	}
	// Die neuen Spalten sind gefüllt: Der Scanner findet die alten Titel.
	gleich, err := st.TitelGleich(ctx, "lib_f", "movie", "ÄRGER IM PARADIES", false)
	if err != nil || len(gleich) != 1 || gleich[0].ID != "it_f1" {
		t.Fatalf("TitelGleich nach dem Umbau: %v %v", gleich, err)
	}
	var neu int64
	_ = st.DB().QueryRow(`SELECT zuletzt_neu FROM item WHERE id = 'it_s'`).Scan(&neu)
	if neu != 1700000102 {
		t.Fatalf("zuletzt_neu der Serie %d, erwartet die neueste Folge (1700000102)", neu)
	}
	// Und das Raster lässt sich in jeder Sortierung abfragen.
	for _, sortierung := range []string{"title", "added", "year", "rating", "year-asc", "title-desc", "random"} {
		got, err := st.Query(ctx, Query{Libraries: []string{"lib_f", "lib_s"}, Sort: sortierung, Limit: 10})
		if err != nil || len(got) != 4 {
			t.Fatalf("Query sort=%s nach dem Umbau: %d Titel, %v", sortierung, len(got), err)
		}
	}
	// Ein zweiter Start ändert nichts mehr und scheitert nicht.
	_ = st.Close()
	st2, err := Open(pfad)
	if err != nil {
		t.Fatalf("zweiter Start: %v", err)
	}
	_ = st2.Close()
}
