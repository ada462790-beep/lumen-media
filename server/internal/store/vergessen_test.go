// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

import (
	"bytes"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

// Nach einem Zurücksetzen steht der private Stand weder in der WAL-Datei
// noch in den Sicherungen. Früher lag „Heimlich" (eine private Liste)
// nach dem Zurücksetzen noch dreimal in lumen.db-wal, auch nach dem
// Beenden des Servers, und die Sicherung von heute Nacht hatte alles.
func TestZuruecksetzenLoeschtAuchWalUndSicherung(t *testing.T) {
	ordner := t.TempDir()
	pfad := filepath.Join(ordner, "lumen.db")
	st, err := Open(pfad)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := t.Context()
	anna, _ := st.CreateAccount(ctx, NewAccount{Name: "Anna", LockKind: "pin", Secret: "1234"})
	lib, _ := st.CreateLibrary(ctx, "Filme", "movie", t.TempDir(), "de")
	it := &Item{LibraryID: lib.ID, Kind: "movie", Title: "Peinlicher Liebesfilm", MatchState: "matched"}
	_ = st.UpsertItem(ctx, it)
	_ = st.SaveProgress(ctx, anna.ID, it.ID, 33_000, 600_000)
	liste, err := st.ListeAnlegen(ctx, anna.ID, "Heimlich")
	if err != nil {
		t.Fatal(err)
	}
	_ = st.ListeHinzufuegen(ctx, anna.ID, liste.ID, it.ID)

	sicherungen := filepath.Join(ordner, "sicherungen")
	if err := st.Sichern(ctx, sicherungen); err != nil {
		t.Fatal(err)
	}
	// Noch ein paar Schreibvorgänge, damit die WAL-Datei die Liste trägt.
	_ = st.SetPref(ctx, anna.ID, "spuren:x", "1")

	if err := st.PinZuruecksetzen(ctx, anna.ID, "pin", "4321"); err != nil {
		t.Fatal(err)
	}
	if err := st.Ausschreiben(ctx); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(pfad + "-wal"); err == nil && fi.Size() != 0 {
		t.Fatalf("WAL-Datei nach dem Ausschreiben: %d Byte", fi.Size())
	}
	roh, _ := os.ReadFile(pfad)
	if bytes.Contains(roh, []byte("Heimlich")) {
		t.Fatal("die private Liste steht noch in der Datenbankdatei")
	}

	if err := SicherungenVergessen(ctx, sicherungen, anna.ID); err != nil {
		t.Fatal(err)
	}
	s1 := sicherungsPfad(sicherungen, 1)
	roh, _ = os.ReadFile(s1)
	if bytes.Contains(roh, []byte("Heimlich")) {
		t.Fatal("die private Liste steht noch in der Sicherung")
	}
	db, err := sql.Open("sqlite3", s1)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	_ = db.QueryRow(`SELECT count(*) FROM progress WHERE account_id = ?`, anna.ID).Scan(&n)
	if n != 0 {
		t.Fatalf("Fortschritt in der Sicherung: %d", n)
	}
	// Die Sicherung ist danach noch gültig.
	if err := pruefeDatei(s1); err != nil {
		t.Fatal(err)
	}
}
