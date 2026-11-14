// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

import (
	"crypto/sha256"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"
)

// TestSperrenGrosseMediathek misst, wie lange Wartungsarbeiten an einer
// grossen Mediathek (100 000 Folgen) die Schreibsperre halten. Nur mit
// LUMEN_LEISTUNG=1 — es dauert.
func TestSperrenGrosseMediathek(t *testing.T) {
	if os.Getenv("LUMEN_LEISTUNG") == "" {
		t.Skip("nur mit LUMEN_LEISTUNG=1")
	}
	st := testStore(t)
	ctx := t.Context()
	lib, _ := st.CreateLibrary(ctx, "Alles", "series", t.TempDir(), "de")
	mediathekFuellen(t, st, lib.ID, "", 2000, 2000, 50)
	_, _ = st.db.Exec(`UPDATE item SET age_rating = 'DE-12', age_years = NULL, match_state = 'unmatched'`)
	alter := func(string) *int { v := 12; return &v }
	schritte := []struct {
		name string
		fn   func() error
	}{
		{"FreigabenNachrechnen", func() error { _, err := st.FreigabenNachrechnen(ctx, alter); return err }},
		{"NichtErkannteZuruecksetzen", func() error { _, err := st.NichtErkannteZuruecksetzen(ctx); return err }},
		{"SerienZusammenfuehren", func() error { _, err := st.SerienZusammenfuehren(ctx); return err }},
		{"FilmeOhneDatei", func() error { _, err := st.FilmeOhneDatei(ctx); return err }},
		{"FolgenOhneDatei", func() error { _, err := st.FolgenOhneDatei(ctx); return err }},
		{"Optimieren (ANALYZE)", func() error { _, _ = st.db.Exec(`DELETE FROM sqlite_stat1`); return st.Optimieren(ctx) }},
		{"TrimLog", func() error { return st.TrimLog(ctx, time.Hour) }},
	}
	for _, s := range schritte {
		SchreibsperrenZuruecksetzen()
		start := time.Now()
		if err := s.fn(); err != nil {
			t.Fatalf("%s: %v", s.name, err)
		}
		sp := Schreibsperren()
		t.Logf("%-28s gesamt %6d ms, längste Sperre %6d ms, %d Transaktionen", s.name,
			time.Since(start).Milliseconds(), sp.Max.Milliseconds(), sp.Anzahl)
	}
}

// Jedes Schreiben rückt den Stand weiter, das Protokoll nicht.
func TestStandZaehltSchreiben(t *testing.T) {
	st := testStore(t)
	ctx := t.Context()
	vorher := Stand()
	st.Log(ctx, "info", "test", "nur eine Zeile im Protokoll")
	if Stand() != vorher {
		t.Fatal("das Protokoll hat den Stand weitergerückt")
	}
	if err := st.SetSetting(ctx, "test.wert", "1"); err != nil {
		t.Fatal(err)
	}
	if Stand() == vorher {
		t.Fatal("eine Einstellung hat den Stand nicht weitergerückt")
	}
	vorher = Stand()
	if _, err := st.Accounts(ctx); err != nil {
		t.Fatal(err)
	}
	if Stand() != vorher {
		t.Fatal("Lesen hat den Stand weitergerückt")
	}
}

// Grosse Nachträge halten die Sperre in Häppchen, nicht am Stück.
func TestInHaeppchen(t *testing.T) {
	st := testStore(t)
	ctx := t.Context()
	alt := haeppchenDauer
	haeppchenDauer = time.Millisecond
	t.Cleanup(func() { haeppchenDauer = alt })
	SchreibsperrenZuruecksetzen()
	n := 0
	err := st.inHaeppchen(ctx, 40, func(tx *sql.Tx, i int) error {
		n++
		time.Sleep(300 * time.Microsecond)
		_, err := tx.ExecContext(ctx, `INSERT INTO setting (key, value) VALUES (?, '1')
		    ON CONFLICT (key) DO UPDATE SET value = excluded.value`, fmt.Sprintf("h.%d", i))
		return err
	})
	if err != nil || n != 40 {
		t.Fatalf("%v, %d Schritte", err, n)
	}
	if sp := Schreibsperren(); sp.Anzahl < 5 {
		t.Fatalf("nur %d Transaktionen — nicht in Häppchen", sp.Anzahl)
	}
	var zahl int
	_ = st.db.QueryRow(`SELECT count(*) FROM setting WHERE key LIKE 'h.%'`).Scan(&zahl)
	if zahl != 40 {
		t.Fatalf("%d von 40 geschrieben", zahl)
	}
}

// Ein Token bleibt im Speicher, bis sich etwas ändert; ein abgemeldetes
// Gerät gilt sofort nicht mehr.
func TestTokenImSpeicher(t *testing.T) {
	st := testStore(t)
	ctx := t.Context()
	acc, err := st.CreateAccount(ctx, NewAccount{Name: "Eins", LockKind: "none"})
	if err != nil {
		t.Fatal(err)
	}
	tok, err := st.IssueToken(ctx, acc.ID, "Gerät", "browser")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.LookupToken(ctx, tok); err != nil {
		t.Fatal(err)
	}
	a, d, err := st.LookupToken(ctx, tok)
	if err != nil || a.ID != acc.ID || d.Name != "Gerät" {
		t.Fatalf("aus dem Speicher: %v %v %v", a, d, err)
	}
	a.Name = "verändert"
	if b, _, _ := st.LookupToken(ctx, tok); b.Name != "Eins" {
		t.Fatal("Änderung am Ergebnis landete im Speicher")
	}
	sum := sha256.Sum256([]byte(tok))
	if err := st.RevokeToken(ctx, sum[:]); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.LookupToken(ctx, tok); err == nil {
		t.Fatal("abgemeldetes Gerät gilt noch")
	}
}
