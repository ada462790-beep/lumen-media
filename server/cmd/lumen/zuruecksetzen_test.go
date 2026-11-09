// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"lumen/internal/store"
)

// Der Besitzer hat seinen PIN vergessen: per Kommandozeile neu setzen,
// sein Verlauf bleibt. Bei einem anderen Profil fragt der Befehl nach und
// löscht den Verlauf — wie die Verwaltung in der App.
func TestZuruecksetzenPerKommandozeile(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "lumen.db"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	besitzer, err := st.CreateAccount(ctx, store.NewAccount{Name: "Alex", IsOwner: true, LockKind: "pin", Secret: "1234"})
	if err != nil {
		t.Fatal(err)
	}
	gast, err := st.CreateAccount(ctx, store.NewAccount{Name: "Lea", LockKind: "pin", Secret: "5555"})
	if err != nil {
		t.Fatal(err)
	}
	st.Close()

	var aus bytes.Buffer
	if err := zuruecksetzenBefehl([]string{"--data", dir}, strings.NewReader(""), &aus); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(aus.String(), "Alex") || !strings.Contains(aus.String(), "Besitzer") {
		t.Fatalf("Liste: %s", aus.String())
	}

	aus.Reset()
	if err := zuruecksetzenBefehl([]string{"--data", dir, "--konto", "alex", "--verwaltung"},
		strings.NewReader("9876\n9876\n"), &aus); err != nil {
		t.Fatalf("%v: %s", err, aus.String())
	}
	aus.Reset()
	// Ohne „ja" bleibt beim fremden Profil alles, wie es war.
	if err := zuruecksetzenBefehl([]string{"--data", dir, "--konto", "Lea"},
		strings.NewReader("nein\n"), &aus); err == nil {
		t.Fatal("ohne Bestätigung zurückgesetzt")
	}
	if err := zuruecksetzenBefehl([]string{"--data", dir, "--konto", "Lea"},
		strings.NewReader("ja\n2468\n2468\n"), &aus); err != nil {
		t.Fatalf("%v: %s", err, aus.String())
	}

	st, err = store.Open(filepath.Join(dir, "lumen.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if st.CheckSecret(ctx, besitzer.ID, "9876") != nil {
		t.Error("neuer Besitzer-PIN gilt nicht")
	}
	if st.CheckSecret(ctx, gast.ID, "2468") != nil {
		t.Error("neuer PIN von Lea gilt nicht")
	}
	if art, _ := st.VerwaltungsSperre(ctx, besitzer.ID); art != "none" {
		t.Errorf("Verwaltungssperre %q", art)
	}
}
