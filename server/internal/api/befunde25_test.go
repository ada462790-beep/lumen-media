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

// Ein Titel, dessen einzige Datei defekt ist, verschwindet sofort aus
// Liste und Suche — nicht erst nach der Frist für fehlende Dateien.
func TestDefekterTitelSofortWeg(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()
	gut, _ := h.film("Heilfilm", 1)
	kaputt, fileID := h.film("Kaputtfilm", 1)
	if _, err := h.st.DB().ExecContext(ctx, `UPDATE media_file SET defekt = 'kaputt' WHERE id = ?`, fileID); err != nil {
		t.Fatal(err)
	}
	if err := h.st.VerfuegbarkeitNachfuehren(ctx, ""); err != nil {
		t.Fatal(err)
	}
	_, raw := h.do("GET", "/api/v1/items?limit=50", "", h.tok)
	if strings.Contains(string(raw), kaputt) || !strings.Contains(string(raw), gut) {
		t.Fatalf("Liste: %s", raw)
	}
	_, raw = h.do("GET", "/api/v1/search?q=kaputtfilm", "", h.tok)
	if strings.Contains(string(raw), kaputt) {
		t.Fatalf("Suche zeigt den defekten Titel: %s", raw)
	}
}

// Ein Wunsch mit dem Namen eines Titels, der schon da ist: 409 „schon_da"
// mit dem Titel, kein neuer Wunsch.
func TestWunschNachVorhandenemTitel(t *testing.T) {
	h := newHarness(t)
	id, _ := h.film("Matrix", 1)
	resp, raw := h.do("POST", "/api/v1/wishes", `{"title":"matrix"}`, h.tok)
	var b struct {
		Code, ItemID, Error string
	}
	_ = json.Unmarshal(raw, &b)
	if resp.StatusCode != http.StatusConflict || b.Code != "schon_da" || b.ItemID != id || !strings.Contains(b.Error, "Matrix (2019)") {
		t.Fatalf("%d %s", resp.StatusCode, raw)
	}
	_, raw = h.do("GET", "/api/v1/wishes", "", h.tok)
	if strings.Contains(string(raw), "atrix") {
		t.Fatalf("Wunsch trotzdem angelegt: %s", raw)
	}
	// Ein anderes Jahr ist ein anderer Titel.
	if resp, raw := h.do("POST", "/api/v1/wishes", `{"title":"Matrix","year":1999}`, h.tok); resp.StatusCode != 200 {
		t.Fatalf("anderes Jahr: %d %s", resp.StatusCode, raw)
	}
}

// Folgen in der Suche: nach Serie, Staffel, Folge — Specials zuletzt.
func TestFolgenOrdnen(t *testing.T) {
	v := func(serie string, st, f int) view {
		x := view{Item: store.Item{SeasonNo: st, EpisodeNo: f}}
		x.SeriesTitle, x.SeriesID = serie, serie
		return x
	}
	l := []view{v("Tiefsee", 2, 1), v("Nordlicht", 0, 3), v("Nordlicht", 1, 4), v("Nordlicht", 1, 2), v("Tiefsee", 1, 9)}
	folgenOrdnen(l)
	var got []string
	for _, x := range l {
		got = append(got, x.SeriesTitle+string(rune('0'+x.SeasonNo))+string(rune('0'+x.EpisodeNo)))
	}
	if strings.Join(got, " ") != "Nordlicht12 Nordlicht14 Nordlicht03 Tiefsee19 Tiefsee21" {
		t.Fatal(got)
	}
}

// Der Ordnerwähler sagt „mindestens", wenn er nicht alles gezählt hat.
func TestVideosDarinAbgeschnitten(t *testing.T) {
	wurzel := t.TempDir()
	flach := filepath.Join(wurzel, "a", "b")
	tief := filepath.Join(wurzel, "a", "b", "c", "d")
	for _, d := range []string{flach, tief} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "x.mkv"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if n, mehr := videosDarin(filepath.Join(wurzel, "a")); n != 1 || !mehr {
		t.Fatalf("tief: %d %v", n, mehr)
	}
	if n, mehr := videosDarin(filepath.Join(wurzel, "a", "b", "c")); n != 1 || mehr {
		t.Fatalf("flach: %d %v", n, mehr)
	}
}
