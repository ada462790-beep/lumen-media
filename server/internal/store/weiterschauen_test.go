// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

import (
	"context"
	"slices"
	"testing"
)

// testSerie legt eine Serie mit zwei Staffeln zu je zwei Folgen und
// einem Special an. Zurück kommen Serie und Folgen in Sehreihenfolge
// (S1F1, S1F2, S2F1, S2F2) sowie das Special.
func testSerie(t *testing.T, s *Store, libID, titel string) (serie string, folgen []string, special string) {
	t.Helper()
	ctx := context.Background()
	se := &Item{LibraryID: libID, Kind: "series", Title: titel, MatchState: "matched"}
	if err := s.UpsertItem(ctx, se); err != nil {
		t.Fatal(err)
	}
	for _, nr := range []int{0, 2, 1} { // absichtlich durcheinander angelegt
		st := &Item{LibraryID: libID, Kind: "season", ParentID: se.ID, Title: "Staffel", SeasonNo: nr, MatchState: "matched"}
		if err := s.UpsertItem(ctx, st); err != nil {
			t.Fatal(err)
		}
		if nr == 0 {
			sp := &Item{LibraryID: libID, Kind: "episode", ParentID: st.ID, Title: "Making-of", SeasonNo: 0, EpisodeNo: 1, MatchState: "matched"}
			if err := s.UpsertItem(ctx, sp); err != nil {
				t.Fatal(err)
			}
			special = sp.ID
			continue
		}
		for _, f := range []int{2, 1} {
			e := &Item{LibraryID: libID, Kind: "episode", ParentID: st.ID, Title: "Folge", SeasonNo: nr, EpisodeNo: f,
				MatchState: "matched", RuntimeMS: 2_400_000}
			if err := s.UpsertItem(ctx, e); err != nil {
				t.Fatal(err)
			}
			folgen = append(folgen, e.ID)
		}
	}
	// In Sehreihenfolge bringen: angelegt wurden S2F2, S2F1, S1F2, S1F1.
	folgen = []string{folgen[3], folgen[2], folgen[1], folgen[0]}
	return se.ID, folgen, special
}

func weiterIDs(t *testing.T, s *Store, acc string) []Weiter {
	t.Helper()
	w, err := s.Weiterschauen(context.Background(), acc, 20)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

// Nach einer fertigen Folge steht die nächste ungesehene in
// „Weiterschauen" — auch über das Ende der Staffel hinweg —, und zwar
// sortiert nach der letzten Aktivität.
func TestWeiterschauenNaechsteFolge(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	acc, _ := s.CreateAccount(ctx, NewAccount{Name: "A", IsOwner: true})
	lib, _ := s.CreateLibrary(ctx, "Serien", "tv", t.TempDir(), "de")
	serie, f, special := testSerie(t, s, lib.ID, "Tiefsee")
	film := &Item{LibraryID: lib.ID, Kind: "movie", Title: "Nordlicht", MatchState: "matched"}
	_ = s.UpsertItem(ctx, film)

	// S1F2 zu Ende geschaut (über die 92 %) → als Nächstes S2F1.
	if err := s.SaveProgress(ctx, acc.ID, f[1], 2_300_000, 2_400_000); err != nil {
		t.Fatal(err)
	}
	w := weiterIDs(t, s, acc.ID)
	if len(w) != 1 || w[0].ItemID != f[2] || !w[0].Naechste {
		t.Fatalf("nach S1F2 erwartet S2F1 als Nächste, bekommen %+v", w)
	}
	// Der Stand einer fertigen Folge steht wieder bei 0 — kein „Weiter bei"
	// mitten im Abspann.
	if p, _ := s.Progress(ctx, acc.ID, f[1]); p == nil || !p.Finished || p.PositionMS != 0 {
		t.Fatalf("fertige Folge: %+v", p)
	}

	// Danach ein Film angefangen: der steht vorn, die Serie dahinter.
	_ = s.SaveProgress(ctx, acc.ID, film.ID, 600_000, 6_000_000)
	w = weiterIDs(t, s, acc.ID)
	if len(w) != 2 || w[0].ItemID != film.ID || w[1].ItemID != f[2] {
		t.Fatalf("Reihenfolge nach letzter Aktivität falsch: %+v", w)
	}

	// Das Special dazwischen ändert nichts an „Als Nächstes" — die Serie
	// rückt nur nach vorn, weil dort zuletzt etwas lief.
	_ = s.SaveProgress(ctx, acc.ID, special, 2_300_000, 2_400_000)
	w = weiterIDs(t, s, acc.ID)
	if len(w) != 2 || w[0].ItemID != f[2] || w[1].ItemID != film.ID {
		t.Fatalf("Special stört: %+v", w)
	}

	// Die nächste Folge kurz angeschaut (20 s) und geschlossen: die Serie
	// bleibt drin, mit genau dieser Folge — ein Eintrag je Serie.
	_ = s.SaveProgress(ctx, acc.ID, f[2], 20_000, 2_400_000)
	w = weiterIDs(t, s, acc.ID)
	if len(w) != 2 || w[0].ItemID != f[2] || w[0].Naechste {
		t.Fatalf("angefangene Folge: %+v", w)
	}

	// Die letzte Folge fertig: Die Serie ist durch und verschwindet.
	_ = s.MarkFinished(ctx, acc.ID, f[2], 0)
	_ = s.MarkFinished(ctx, acc.ID, f[3], 0)
	w = weiterIDs(t, s, acc.ID)
	if len(w) != 1 || w[0].ItemID != film.ID {
		t.Fatalf("fertige Serie noch in Weiterschauen: %+v", w)
	}
	_ = serie
}

// Eine ganze Staffel als gesehen markiert: Als Nächstes kommt die erste
// Folge der nächsten Staffel — nicht eine beliebige aus der markierten.
// Und eine übersprungene frühere Folge zählt nicht als „als Nächstes".
func TestWeiterschauenNachStaffelMarkierung(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	acc, _ := s.CreateAccount(ctx, NewAccount{Name: "A", IsOwner: true})
	lib, _ := s.CreateLibrary(ctx, "Serien", "tv", t.TempDir(), "de")
	serie, f, _ := testSerie(t, s, lib.ID, "Tiefsee")

	st1, err := s.FolgenVon(ctx, serie, 1)
	if err != nil || len(st1) != 2 || st1[0].ID != f[0] || st1[1].ID != f[1] {
		t.Fatalf("FolgenVon Staffel 1: %v %v", st1, err)
	}
	if err := s.MarkFinishedMehrere(ctx, acc.ID, []string{f[0], f[1]}, nil); err != nil {
		t.Fatal(err)
	}
	w := weiterIDs(t, s, acc.ID)
	if len(w) != 1 || w[0].ItemID != f[2] {
		t.Fatalf("nach Staffel 1 erwartet S2F1: %+v", w)
	}

	// S2F2 gesehen, S2F1 nicht: Die Serie ist nach der letzten Folge zu
	// Ende — nichts mehr anzubieten (Netflix springt auch nicht zurück).
	_ = s.MarkFinished(ctx, acc.ID, f[3], 0)
	if w := weiterIDs(t, s, acc.ID); len(w) != 0 {
		t.Fatalf("nach der letzten Folge: %+v", w)
	}

	// Alles zurück: auch der Stand ist weg.
	all, _ := s.FolgenVon(ctx, serie, -1)
	if len(all) != 5 {
		t.Fatalf("FolgenVon alle: %d, erwartet 5 (mit Special)", len(all))
	}
	ids := []string{}
	for _, x := range all {
		ids = append(ids, x.ID)
	}
	if err := s.ClearProgressMehrere(ctx, acc.ID, ids); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.FinishedIDs(ctx, acc.ID, ids); len(got) != 0 {
		t.Fatalf("Haken übrig: %v", got)
	}
}

// Eine angefangene Folge macht die Serie „angefangen" — sie gehört dann
// nicht mehr in „Noch nicht angefangen".
func TestAngefangeneSerie(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	acc, _ := s.CreateAccount(ctx, NewAccount{Name: "A", IsOwner: true})
	b, _ := s.CreateAccount(ctx, NewAccount{Name: "B", LockKind: "pin", Secret: "4711"})
	lib, _ := s.CreateLibrary(ctx, "Serien", "tv", t.TempDir(), "de")
	serie, f, _ := testSerie(t, s, lib.ID, "Tiefsee")
	_ = s.SaveProgress(ctx, acc.ID, f[1], 100_000, 2_400_000)

	ids, err := s.AngefangeneIDs(ctx, acc.ID)
	if err != nil || !slices.Contains(ids, serie) || !slices.Contains(ids, f[1]) {
		t.Fatalf("Serie fehlt unter angefangen: %v %v", ids, err)
	}
	// Für ein anderes Profil bleibt sie unberührt.
	if ids, _ := s.AngefangeneIDs(ctx, b.ID); len(ids) != 0 {
		t.Fatalf("fremder Stand sichtbar: %v", ids)
	}
	if w := weiterIDs(t, s, b.ID); len(w) != 0 {
		t.Fatalf("fremdes Weiterschauen: %+v", w)
	}

	// Der Balken an der Serie zählt ohne Special — wie GesehenIDs.
	st, _ := s.SerienStaende(ctx, acc.ID, []string{serie})
	if st[serie].Folgen != 4 {
		t.Fatalf("Folgen gezählt: %d, erwartet 4", st[serie].Folgen)
	}
}
