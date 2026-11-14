// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

import (
	"context"
	"testing"
	"time"
)

// TestNeuigkeitenNeueTitelVollstaendig: Eine alte Serie mit neuen Folgen
// steht unter „Zuletzt hinzugefügt" vorn. Früher hörte das Zählen der
// neuen Titel bei ihr auf — die Seite sagte „0 neue Titel", obwohl zwei
// Filme neu waren, und die neuen Folgen fehlten ganz. Jetzt: „2 neue
// Titel" mit beiden Titeln und „Neue Folgen von Tiefsee".
func TestNeuigkeitenNeueTitelVollstaendig(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	a, _ := s.CreateAccount(ctx, NewAccount{Name: "A", IsOwner: true})
	lib, _ := s.CreateLibrary(ctx, "Mediathek", "movie", t.TempDir(), "de")
	alt := time.Now().Add(-60 * 24 * time.Hour).Unix()

	setze := func(id string, at int64) {
		t.Helper()
		if _, err := s.db.ExecContext(ctx, `UPDATE item SET added_at = ?, zuletzt_neu = ? WHERE id = ?`, at, at, id); err != nil {
			t.Fatal(err)
		}
	}
	film := func(titel string, at int64) string {
		t.Helper()
		it := &Item{LibraryID: lib.ID, Kind: "movie", Title: titel, MatchState: "matched"}
		if err := s.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
		setze(it.ID, at)
		return it.ID
	}
	film("Altbekannt", alt)
	serie, folgen, special := testSerie(t, s, lib.ID, "Tiefsee")
	// Die Serie, ihre Staffeln und zwei Folgen sind alt; zwei Folgen kamen
	// eben dazu (die Serie rückt damit nach vorn).
	setze(serie, alt)
	if _, err := s.db.ExecContext(ctx, `UPDATE item SET added_at = ? WHERE parent_id = ?`, alt, serie); err != nil {
		t.Fatal(err)
	}
	for _, id := range append([]string{special}, folgen[:2]...) {
		setze(id, alt)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE item SET zuletzt_neu = ? WHERE id = ?`, time.Now().Unix(), serie); err != nil {
		t.Fatal(err)
	}
	n1 := film("Nordlicht", time.Now().Add(-2*time.Hour).Unix())
	n2 := film("Sonne", time.Now().Add(-time.Hour).Unix())

	liste, err := s.Neuigkeiten(ctx, Query{Libraries: []string{lib.ID}}, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	var neu, folgenZeile *Neuigkeit
	for i := range liste {
		switch liste[i].Art {
		case "neu":
			neu = &liste[i]
		case "folgen":
			folgenZeile = &liste[i]
		}
	}
	if neu == nil || neu.Anzahl != 2 || len(neu.Liste) != 2 || neu.Liste[0].ID != n2 || neu.Liste[1].ID != n1 {
		t.Fatalf("neue Titel: %+v", neu)
	}
	if folgenZeile == nil || folgenZeile.ItemID != serie || folgenZeile.Anzahl != 2 || folgenZeile.Vorlage.Titel != "Neue Folgen von {serie}" {
		t.Fatalf("neue Folgen: %+v (alle: %+v)", folgenZeile, liste)
	}

	// Nur ein neuer Titel: Die Zeile führt direkt zu ihm.
	setze(n1, alt)
	liste, _ = s.Neuigkeiten(ctx, Query{Libraries: []string{lib.ID}}, a.ID)
	for _, n := range liste {
		if n.Art == "neu" && (n.ItemID != n2 || n.Anzahl != 1) {
			t.Fatalf("ein neuer Titel: %+v", n)
		}
	}
}

// TestTMDbWertung: Die Note der TMDb-Nutzer kommt als Prozent zurück;
// ohne Stimmen gilt sie nicht.
func TestTMDbWertung(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	lib, _ := s.CreateLibrary(ctx, "Filme", "movie", t.TempDir(), "de")
	it := &Item{LibraryID: lib.ID, Kind: "movie", Title: "Nordlicht", MatchState: "matched"}
	if err := s.UpsertItem(ctx, it); err != nil {
		t.Fatal(err)
	}
	if err := s.TMDbWertungSetzen(ctx, it.ID, 0, 0); err != nil {
		t.Fatal(err)
	}
	x, _ := s.Item(ctx, it.ID)
	if x.TMDbProzent != nil {
		t.Fatalf("ohne Stimmen: %v", *x.TMDbProzent)
	}
	if err := s.TMDbWertungSetzen(ctx, it.ID, 78, 1234); err != nil {
		t.Fatal(err)
	}
	// Ein späteres Aufnehmen (Durchlauf) lässt die Wertung stehen.
	if err := s.UpsertItem(ctx, it); err != nil {
		t.Fatal(err)
	}
	x, _ = s.Item(ctx, it.ID)
	if x.TMDbProzent == nil || *x.TMDbProzent != 78 || x.TMDbStimmen != 1234 {
		t.Fatalf("TMDb: %+v %d", x.TMDbProzent, x.TMDbStimmen)
	}
}
