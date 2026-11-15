// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lumen/internal/store"
)

// „Abtrennen" antwortet mit dem neuen Film samt richtigem addedAt —
// nicht 0001-01-01.
func TestAbtrennenMitAddedAt(t *testing.T) {
	h := newHarness(t)
	itemID, gross := h.film("American Psycho", 1)
	klein := &store.MediaFile{ItemID: itemID, Path: filepath.Join(h.root, "Anderer Film.avi"), SizeBytes: 1024, Container: "avi"}
	_ = os.WriteFile(klein.Path, []byte("x"), 0o644)
	_ = h.st.UpsertFile(t.Context(), klein)
	_ = gross
	resp, raw := h.do("POST", "/api/v1/admin/files/"+klein.ID+"/abtrennen", "", h.tok)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Abtrennen: %d %s", resp.StatusCode, raw)
	}
	if strings.Contains(string(raw), "0001-01-01") || !strings.Contains(string(raw), `"addedAt"`) {
		t.Errorf("Antwort ohne richtiges addedAt: %s", raw)
	}
}

// „Als Teile zusammenfügen": Hat Teil 1 kein Jahr, übernimmt der Film das
// Jahr eines anderen Teils.
func TestAlsTeileUebernimmtJahr(t *testing.T) {
	h := newHarness(t)
	h.film("Platzhalter", 1)
	ctx := t.Context()
	neuerFilm := func(titel string, jahr int, datei string) (string, string) {
		it := &store.Item{LibraryID: h.lib.ID, Kind: "movie", Title: titel, Year: jahr, MatchState: "pending"}
		_ = h.st.UpsertItem(ctx, it)
		p := filepath.Join(h.root, datei)
		_ = os.WriteFile(p, []byte("x"), 0o644)
		f := &store.MediaFile{ItemID: it.ID, Path: p, SizeBytes: 1, Container: "mkv"}
		_ = h.st.UpsertFile(ctx, f)
		return it.ID, f.ID
	}
	eins, f1 := neuerFilm("Der Zweiteiler: Teil 1", 0, "Der Zweiteiler - Teil 1.mkv")
	_, f2 := neuerFilm("Der Zweiteiler: Teil 2", 2018, "Der Zweiteiler - Teil 2 (2018).mkv")
	body, _ := json.Marshal(map[string]any{"fileIds": []string{f1, f2}})
	if resp, raw := h.do("POST", "/api/v1/admin/items/"+eins+"/als-teile", string(body), h.tok); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("als Teile: %d %s", resp.StatusCode, raw)
	}
	it, err := h.st.Item(ctx, eins)
	if err != nil {
		t.Fatal(err)
	}
	if it.Year != 2018 {
		t.Errorf("Jahr des Films %d, erwartet 2018 (Titel %q)", it.Year, it.Title)
	}
}
