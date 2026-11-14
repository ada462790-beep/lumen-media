// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"testing"

	"lumen/internal/store"
)

// Löscht die Person, die den Wunsch angelegt hat, ihn, behalten ihn die
// Angeschlossenen — vorher verschwand er für alle.
func TestWunschLoeschenGibtAnAngeschlosseneWeiter(t *testing.T) {
	h := newHarness(t)
	h.film("Etwas", 1)
	lea := h.gast("Lea", false, h.lib.ID)
	noa := h.gast("Noa", false, h.lib.ID)
	_, a := h.wuenschen(lea, `{"title":"Heat","year":1995}`)
	h.wuenschen(noa, `{"title":"Heat","year":1995}`)
	if resp, _ := h.do("DELETE", "/api/v1/wishes/"+a.ID, "", lea); resp.StatusCode != 204 {
		t.Fatalf("löschen: %d", resp.StatusCode)
	}
	if l := h.wuensche(lea); len(l) != 0 {
		t.Fatalf("Lea sieht ihn noch: %+v", l)
	}
	if l := h.wuensche(noa); len(l) != 1 || l[0].Dabei {
		t.Fatalf("Noa hat ihn nicht übernommen: %+v", l)
	}
}

// Eine Mitverwalterin nur für „Kinder" löscht einen Wunsch nicht für
// Konten ausserhalb ihres Bereichs: Wer sich von dort angeschlossen hat,
// behält ihn.
func TestMitverwalterinLoeschtWunschNurInIhremBereich(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()
	h.film("Etwas", 1)
	kinderLib, err := h.st.CreateLibrary(ctx, "Kinder", "movie", t.TempDir(), "de")
	if err != nil {
		t.Fatal(err)
	}
	ctok, _ := h.mitverwalter("Carla")
	g, err := h.st.GruppeAnlegen(ctx, store.Gruppe{Name: "Kinderverwaltung", Verwaltung: true,
		Libraries: []string{kinderLib.ID}, Limits: store.VerwaltungsLimits()})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.st.GruppeZuweisen(ctx, h.idVon(ctok), g.ID); err != nil {
		t.Fatal(err)
	}
	kira := h.gast("Kira", true, kinderLib.ID)
	anna := h.gast("Anna", false, h.lib.ID, kinderLib.ID)
	_, w := h.wuenschen(kira, `{"title":"Bluey","year":2018}`)
	h.wuenschen(anna, `{"title":"Bluey","year":2018}`)
	if resp, raw := h.do("DELETE", "/api/v1/admin/wishes/"+w.ID, "", ctok); resp.StatusCode != 204 {
		t.Fatalf("Carla löscht: %d %s", resp.StatusCode, raw)
	}
	if l := h.wuensche(kira); len(l) != 0 {
		t.Fatalf("Kira sieht ihn noch: %+v", l)
	}
	if l := h.wuensche(anna); len(l) != 1 {
		t.Fatalf("Annas Wunsch ist mit gelöscht: %+v", l)
	}
	// Die Verwaltung mit allen Rechten löscht ihn ganz.
	if resp, _ := h.do("DELETE", "/api/v1/admin/wishes/"+w.ID, "", h.tok); resp.StatusCode != 204 {
		t.Fatalf("Verwaltung löscht: %d", resp.StatusCode)
	}
	if l := h.wuensche(anna); len(l) != 0 {
		t.Fatalf("noch da: %+v", l)
	}
}
