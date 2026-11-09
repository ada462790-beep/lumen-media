// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

import (
	"context"
	"database/sql"
	"strings"
	"testing"
)

// Die Hörtabellen kennen keine Uhrzeit — nur Anzahl und laufende Nummer.
func TestHoerstandOhneUhrzeit(t *testing.T) {
	s := testStore(t)
	for _, tbl := range []string{"musik_hoerstand", "musik_hoerreihe", "musik_favorit"} {
		rows, err := s.DB().Query(`SELECT name FROM pragma_table_info(?)`, tbl)
		if err != nil {
			t.Fatal(err)
		}
		spalten, _ := collectStrings(rows)
		rows.Close()
		for _, sp := range spalten {
			if strings.Contains(sp, "_at") || strings.Contains(sp, "zeit") || strings.Contains(sp, "datum") {
				t.Errorf("%s hat eine Zeitspalte: %s", tbl, sp)
			}
		}
	}
}

func musikTestdaten(t *testing.T, s *Store) (lib string, titel []string) {
	t.Helper()
	ctx := context.Background()
	mustExec(t, s, `INSERT INTO library (id,name,kind,path) VALUES ('mlib','Musik','music','/m')`)
	for i, n := range []string{"Eins", "Zwei", "Drei"} {
		mt := &MusikTitel{LibraryID: "mlib", Pfad: "/m/" + n + ".mp3", Titel: n, Interpret: "Band", Album: "A",
			Track: i + 1, Explizit: n == "Drei"}
		if _, err := s.MusikTitelSpeichern(ctx, mt); err != nil {
			t.Fatal(err)
		}
		titel = append(titel, mt.ID)
	}
	return "mlib", titel
}

func TestHoerstandBleibtImProfil(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	a, _ := s.CreateAccount(ctx, NewAccount{Name: "Besitzer", IsOwner: true})
	b, _ := s.CreateAccount(ctx, NewAccount{Name: "Gast"})
	_, titel := musikTestdaten(t, s)

	for i := 0; i < 3; i++ {
		_ = s.MusikGehoert(ctx, b.ID, titel[1])
	}
	_ = s.MusikGehoert(ctx, b.ID, titel[0])
	if ids, _ := s.MusikZuletztGehoert(ctx, a.ID, 10); len(ids) != 0 {
		t.Fatalf("der Besitzer sieht, was der Gast hörte: %v", ids)
	}
	ids, _ := s.MusikZuletztGehoert(ctx, b.ID, 10)
	if len(ids) != 2 || ids[0] != titel[0] {
		t.Fatalf("Reihenfolge falsch: %v", ids)
	}
	oft, _ := s.MusikWieOft(ctx, b.ID, titel)
	if oft[titel[1]] != 3 || oft[titel[0]] != 1 {
		t.Fatalf("Anzahl falsch: %v", oft)
	}
	if oft, _ := s.MusikWieOft(ctx, a.ID, titel); len(oft) != 0 {
		t.Fatalf("fremde Zahlen: %v", oft)
	}
	_ = s.MusikHoerverlaufLoeschen(ctx, b.ID)
	if ids, _ := s.MusikZuletztGehoert(ctx, b.ID, 10); len(ids) != 0 {
		t.Fatal("Hörverlauf nicht gelöscht")
	}
}

func TestMusikListenUndFreigaben(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	a, _ := s.CreateAccount(ctx, NewAccount{Name: "A", IsOwner: true})
	b, _ := s.CreateAccount(ctx, NewAccount{Name: "B"})
	lib, titel := musikTestdaten(t, s)

	l, err := s.MusikListeAnlegen(ctx, a.ID, "Lauf")
	if err != nil {
		t.Fatal(err)
	}
	_ = s.MusikListeHinzufuegen(ctx, a.ID, l.ID, titel)
	if _, err := s.MusikListeHolen(ctx, b.ID, l.ID); err != ErrNotFound {
		t.Fatalf("private Liste für andere sichtbar: %v", err)
	}
	geteilt := true
	_ = s.MusikListeAendern(ctx, a.ID, l.ID, nil, &geteilt, nil)
	lb, err := s.MusikListeHolen(ctx, b.ID, l.ID)
	if err != nil || lb.DarfAendern || len(lb.TitelIDs) != 3 {
		t.Fatalf("geteilte Liste: %+v %v", lb, err)
	}
	if err := s.MusikListeHinzufuegen(ctx, b.ID, l.ID, titel[:1]); err != ErrNichtErlaubt {
		t.Fatalf("B darf ohne „gemeinsam“ schreiben: %v", err)
	}
	if err := s.MusikListeLoeschen(ctx, b.ID, l.ID); err != ErrNichtErlaubt {
		t.Fatalf("B darf fremde Liste löschen: %v", err)
	}
	_ = s.MusikListeOrdnen(ctx, a.ID, l.ID, []string{titel[2], titel[0]})
	l, _ = s.MusikListeHolen(ctx, a.ID, l.ID)
	if strings.Join(l.TitelIDs, ",") != strings.Join([]string{titel[2], titel[0], titel[1]}, ",") {
		t.Fatalf("Reihenfolge: %v", l.TitelIDs)
	}

	// Freigaben: ohne Bibliothek nichts, mit Kindersperre ohne „explizit".
	if ts, _ := s.MusikTitelNachIDs(ctx, MusikSicht{}, titel); len(ts) != 0 {
		t.Fatal("Titel ohne Freigabe sichtbar")
	}
	ts, _ := s.MusikTitelNachIDs(ctx, MusikSicht{Bibliotheken: []string{lib}, OhneExplizit: true}, titel)
	if len(ts) != 2 {
		t.Fatalf("explizit nicht ausgeblendet: %d", len(ts))
	}

	// Favoriten gehören dem Profil.
	_ = s.MusikFavoritSetzen(ctx, b.ID, "titel", titel[0], true)
	if f, _ := s.MusikFavoriten(ctx, a.ID); len(f["titel"]) != 0 {
		t.Fatal("fremde Herzen sichtbar")
	}
	// Ein gelöschtes Profil nimmt alles mit.
	_ = s.DeleteAccount(ctx, b.ID)
	var n int
	if err := s.DB().QueryRow(`SELECT 1 FROM musik_favorit WHERE account_id = ?`, b.ID).Scan(&n); err != sql.ErrNoRows {
		t.Fatal("Herzen eines gelöschten Profils bleiben")
	}
}
