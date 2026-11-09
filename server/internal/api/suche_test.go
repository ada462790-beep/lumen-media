// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"

	"lumen/internal/store"
)

// „Komödie 2019" findet die Komödie von 2019, „Filme mit Meg Ryan" ihre
// Filme — und die Person taucht als Sprungbrett auf.
func TestSucheVerstehtGenreJahrUndPerson(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()
	h.film("Nordlicht", 1)
	lib := h.lib
	anlegen := func(titel string, jahr int, genres []string, leute ...string) string {
		it := &store.Item{LibraryID: lib.ID, Kind: "movie", Title: titel, Year: jahr, MatchState: "matched", Genres: genres}
		if err := h.st.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
		var p []store.Person
		for _, l := range leute {
			p = append(p, store.Person{Name: l})
		}
		if err := h.st.BesetzungSetzen(ctx, it.ID, p); err != nil {
			t.Fatal(err)
		}
		return it.ID
	}
	anlegen("Sommerhaus", 2019, []string{"Komödie"}, "Meg Ryan", "Tom Hanks")
	anlegen("Waldrand", 2019, []string{"Thriller"}, "Al Pacino")
	anlegen("Halbmond", 1994, []string{"Komödie"}, "Meg Ryan")

	suche := func(q string) (titel []string, personen []string, d store.Deutung) {
		_, raw := h.do("GET", "/api/v1/search?q="+url.QueryEscape(q), "", h.tok)
		var out struct {
			Items    []struct{ Title string }
			Personen []struct{ Name string }
			Deutung  store.Deutung
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("%s: %s", q, raw)
		}
		for _, i := range out.Items {
			titel = append(titel, i.Title)
		}
		for _, p := range out.Personen {
			personen = append(personen, p.Name)
		}
		return titel, personen, out.Deutung
	}

	titel, _, d := suche("Komödie 2019")
	if len(titel) != 1 || titel[0] != "Sommerhaus" || d.Genre != "Komödie" || d.JahrVon != 2019 {
		t.Errorf("Komödie 2019: %v %+v", titel, d)
	}
	titel, personen, d := suche("Filme mit Meg Ryan")
	if len(titel) != 2 || d.Person != "Meg Ryan" || d.Text != "" {
		t.Errorf("mit Meg Ryan: %v %+v", titel, d)
	}
	if len(personen) != 1 || personen[0] != "Meg Ryan" {
		t.Errorf("Personen: %v", personen)
	}
	titel, _, d = suche("meg ryan 90er")
	if len(titel) != 1 || titel[0] != "Halbmond" || d.JahrVon != 1990 || d.JahrBis != 1999 {
		t.Errorf("90er: %v %+v", titel, d)
	}
	// Ein halber Name ist kein Filter, aber ein Treffer in der Personenliste.
	titel, personen, d = suche("ryan")
	if d.Person != "" || len(personen) != 1 {
		t.Errorf("ryan: %v %v %+v", titel, personen, d)
	}
	// Titelsuche wie bisher.
	titel, _, _ = suche("nordlicht")
	if len(titel) != 1 || titel[0] != "Nordlicht" {
		t.Errorf("Titel: %v", titel)
	}
	// ?person= am Raster.
	_, raw := h.do("GET", "/api/v1/items?person="+url.QueryEscape("Meg Ryan"), "", h.tok)
	var items []struct{ Title string }
	_ = json.Unmarshal(raw, &items)
	if len(items) != 2 {
		t.Errorf("items?person: %s", raw)
	}
}

// Der Balken an der Serie zählt nur die eigenen Folgen — was andere im
// Haushalt gesehen haben, bleibt unsichtbar.
func TestSerienStandIstPrivat(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()
	h.film("Nordlicht", 1)
	serie := &store.Item{LibraryID: h.lib.ID, Kind: "series", Title: "Tiefsee", MatchState: "matched"}
	if err := h.st.UpsertItem(ctx, serie); err != nil {
		t.Fatal(err)
	}
	staffel := &store.Item{LibraryID: h.lib.ID, Kind: "season", ParentID: serie.ID, Title: "Staffel 1", SeasonNo: 1, MatchState: "matched"}
	if err := h.st.UpsertItem(ctx, staffel); err != nil {
		t.Fatal(err)
	}
	var folgen []string
	for i := 1; i <= 3; i++ {
		f := &store.Item{LibraryID: h.lib.ID, Kind: "episode", ParentID: staffel.ID, Title: "Folge", SeasonNo: 1, EpisodeNo: i, MatchState: "matched"}
		if err := h.st.UpsertItem(ctx, f); err != nil {
			t.Fatal(err)
		}
		folgen = append(folgen, f.ID)
	}
	jonas, _ := h.st.CreateAccount(ctx, store.NewAccount{Name: "Jonas", Grants: []string{h.lib.ID}})
	jtok, _ := h.st.IssueToken(ctx, jonas.ID, "iPhone", "phone")
	// Jonas hat alle drei gesehen, Alex eine.
	for _, id := range folgen {
		_ = h.st.MarkFinished(ctx, jonas.ID, id, 0)
	}
	_ = h.st.MarkFinished(ctx, h.ownerID(), folgen[0], 0)

	stand := func(tok string) (int, int, bool) {
		_, raw := h.do("GET", "/api/v1/items?kind=series", "", tok)
		var out []struct {
			Gesehen, Folgen int
			Finished        bool
		}
		if err := json.Unmarshal(raw, &out); err != nil || len(out) != 1 {
			t.Fatalf("Serien: %s", raw)
		}
		return out[0].Gesehen, out[0].Folgen, out[0].Finished
	}
	if g, f, fin := stand(h.tok); g != 1 || f != 3 || fin {
		t.Errorf("Alex: %d von %d, fertig=%v", g, f, fin)
	}
	if g, f, fin := stand(jtok); g != 3 || f != 3 || !fin {
		t.Errorf("Jonas: %d von %d, fertig=%v", g, f, fin)
	}
}

// Eine Sucheingabe wird auf 200 Zeichen und acht Wörter gekürzt — die
// unscharfe Suche vergleicht jedes Wort mit jedem Titelwort.
func TestSuchEingabeBegrenzt(t *testing.T) {
	lang := strings.Repeat("abcdefghij", 100)
	if got := suchtextKuerzen(lang); len([]rune(got)) != suchZeichenHoechstens {
		t.Fatalf("Länge: %d", len([]rune(got)))
	}
	viele := strings.Repeat("wort ", 50)
	if got := strings.Fields(suchtextKuerzen(viele)); len(got) != suchWoerterHoechstens {
		t.Fatalf("Wörter: %d", len(got))
	}
	h := newHarness(t)
	h.film("Matrix", 1)
	start := time.Now()
	resp, _ := h.do("GET", "/api/v1/search?q="+strings.Repeat("xqzv"+strings.Repeat("y", 300)+"+", 500), "", h.tok)
	if resp.StatusCode != 200 || time.Since(start) > 3*time.Second {
		t.Fatalf("lange Eingabe: %d nach %v", resp.StatusCode, time.Since(start))
	}
}
