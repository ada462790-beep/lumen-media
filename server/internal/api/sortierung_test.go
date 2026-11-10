// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"lumen/internal/store"
)

type rasterSeite struct {
	Items []struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	} `json:"items"`
	Gesamt int   `json:"gesamt"`
	Seed   int64 `json:"seed"`
}

func (h *harness) raster(tok, abfrage string) rasterSeite {
	h.t.Helper()
	resp, raw := h.do("GET", "/api/v1/items?seite=1&limit=50&"+abfrage, "", tok)
	if resp.StatusCode != http.StatusOK {
		h.t.Fatalf("%s: %d %s", abfrage, resp.StatusCode, raw)
	}
	var s rasterSeite
	if err := json.Unmarshal(raw, &s); err != nil {
		h.t.Fatal(err)
	}
	return s
}

func (s rasterSeite) titel() string {
	var n []string
	for _, it := range s.Items {
		n = append(n, it.Title)
	}
	return strings.Join(n, "|")
}

// sort und dir: neue Werte, alte Werte als Ersatznamen, Unbekanntes als Titel.
func TestSortierungUeberDieSchnittstelle(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()
	h.film("Teil 10", 1)
	h.film("Teil 2", 1)
	h.film("Ärger", 1)
	// Erscheinungsjahre: Ärger 2001, Teil 2 1999, Teil 10 ohne.
	for titel, jahr := range map[string]int{"Ärger": 2001, "Teil 2": 1999} {
		_, err := h.st.DB().Exec(`UPDATE item SET year = ?, premiered = NULL WHERE title = ?`, jahr, titel)
		if err != nil {
			t.Fatal(err)
		}
	}
	_, _ = h.st.DB().Exec(`UPDATE item SET year = NULL WHERE title = 'Teil 10'`)

	faelle := []struct{ abfrage, want string }{
		{"sort=title", "Ärger|Teil 2|Teil 10"},
		{"sort=title&dir=asc", "Ärger|Teil 2|Teil 10"},
		{"sort=title&dir=desc", "Teil 10|Teil 2|Ärger"},
		{"sort=title-desc", "Teil 10|Teil 2|Ärger"},
		{"sort=premiered", "Ärger|Teil 2|Teil 10"},
		{"sort=year", "Ärger|Teil 2|Teil 10"},
		{"sort=premiered&dir=asc", "Teil 2|Ärger|Teil 10"},
		{"sort=year-asc", "Teil 2|Ärger|Teil 10"},
		{"sort=" + url.QueryEscape("title; DROP TABLE item"), "Ärger|Teil 2|Teil 10"},
		{"sort=title&dir=seitwaerts", "Ärger|Teil 2|Teil 10"},
	}
	for _, f := range faelle {
		if got := h.raster(h.tok, f.abfrage).titel(); got != f.want {
			t.Errorf("%s: %s, erwartet %s", f.abfrage, got, f.want)
		}
	}

	// Zufall: Der Server nennt den Samen; mit ihm kommt dieselbe Reihenfolge.
	z := h.raster(h.tok, "sort=random")
	if z.Seed == 0 || len(z.Items) != 3 {
		t.Fatalf("zufällig ohne Samen: %+v", z)
	}
	if nochmal := h.raster(h.tok, fmt.Sprintf("sort=random&seed=%d", z.Seed)); nochmal.titel() != z.titel() {
		t.Fatalf("derselbe Samen, andere Reihenfolge: %s / %s", nochmal.titel(), z.titel())
	}

	// „Zuletzt gesehen" gilt nur für das eigene Profil.
	gast, _ := h.st.CreateAccount(ctx, store.NewAccount{Name: "Mara", LockKind: "pin", Secret: "4711"})
	_ = h.st.SetGrants(ctx, gast.ID, []string{h.lib.ID})
	gtok, _ := h.st.IssueToken(ctx, gast.ID, "iPad", "tablet")
	var teil10 string
	for _, it := range h.raster(h.tok, "sort=title").Items {
		if it.Title == "Teil 10" {
			teil10 = it.ID
		}
	}
	if resp, _ := h.do("POST", "/api/v1/items/"+teil10+"/progress", `{"positionMs":1800000,"durationMs":5400000}`, gtok); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("Fortschritt: %d", resp.StatusCode)
	}
	if got := h.raster(gtok, "sort=lastwatched").titel(); got != "Teil 10|Ärger|Teil 2" {
		t.Errorf("zuletzt gesehen (Mara): %s", got)
	}
	if got := h.raster(h.tok, "sort=lastwatched").titel(); got != "Ärger|Teil 2|Teil 10" {
		t.Errorf("zuletzt gesehen (Besitzer) verrät Maras Stand: %s", got)
	}
	if got := h.raster(h.tok, "sort=unwatched").titel(); got != "Ärger|Teil 2|Teil 10" {
		t.Errorf("ungesehene zuerst (Besitzer): %s", got)
	}

	// Ein Kind ohne Freigabe sieht auch in den Go-Sortierungen nichts.
	kind, _ := h.st.CreateAccount(ctx, store.NewAccount{Name: "Lina", IsKid: true})
	ktok, _ := h.st.IssueToken(ctx, kind.ID, "iPad", "tablet")
	for _, s := range []string{"random", "lastwatched", "unwatched", "title"} {
		if got := h.raster(ktok, "sort="+s); len(got.Items) != 0 || got.Gesamt != 0 {
			t.Errorf("Kind ohne Freigabe sieht mit sort=%s: %s", s, got.titel())
		}
	}

	// Die Wahl lässt sich im Profil merken.
	if resp, raw := h.do("PATCH", "/api/v1/me", `{"prefs":{"sort.movie":"added:desc","sort.series":"title:asc"}}`, h.tok); resp.StatusCode != 200 {
		t.Fatalf("Sortierung merken: %d %s", resp.StatusCode, raw)
	}
}

// Die Suche ordnet nach Güte: genau der Titel zuerst.
func TestSucheNachRelevanz(t *testing.T) {
	h := newHarness(t)
	h.film("Animatrix", 1)
	h.film("Matrix", 1)
	resp, raw := h.do("GET", "/api/v1/search?q=matrix", "", h.tok)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("%d %s", resp.StatusCode, raw)
	}
	var s rasterSeite
	_ = json.Unmarshal(raw, &s)
	if got := s.titel(); got != "Matrix|Animatrix" {
		t.Fatalf("Suche: %s", got)
	}
}
