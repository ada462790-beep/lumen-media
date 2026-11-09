// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

import (
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"
)

// Wie eine echte Sammlung aussieht: Reihen in allen Schreibweisen, dazu
// Filme, die nur zufällig ähnlich heissen. Was nicht zusammengehört, darf
// nicht zusammengeworfen werden — eine falsche Reihe ist schlimmer als
// eine fehlende.
func TestReihenAusTiteln(t *testing.T) {
	titel := []struct {
		t string
		j int
	}{
		{"Der Herr der Ringe: Die Gefährten", 2001},
		{"Der Herr der Ringe: Die zwei Türme", 2002},
		{"Der Herr der Ringe: Die Rückkehr des Königs", 2003},
		{"Harry Potter und der Stein der Weisen", 2001},
		{"Harry Potter und die Kammer des Schreckens", 2002},
		{"Harry Potter und der Gefangene von Askaban", 2004},
		{"Alien", 1979},
		{"Alien 3", 1992},
		{"Rocky", 1976},
		{"Rocky II", 1979},
		{"Rocky III", 1982},
		{"Mission: Impossible", 1996},
		{"Mission: Impossible II", 2000},
		{"Mission: Impossible – Fallout", 2018},
		{"Star Wars: Episode IV – Eine neue Hoffnung", 1977},
		{"Star Wars: Episode V – Das Imperium schlägt zurück", 1980},
		{"Die Tribute von Panem – Tödliche Spiele", 2012},
		{"Die Tribute von Panem – Catching Fire", 2013},
		{"Shrek", 2001},
		{"Shrek 2", 2004},
		// Das gehört nirgends hin:
		{"Star Trek", 2009},           // „Star" allein trägt keine Reihe
		{"Der König der Löwen", 1994}, // „Der" auch nicht
		{"Der kleine Prinz", 2015},
		{"Max", 2015},  // kein „Ma" aus „Max"
		{"1917", 2019}, // eine Zahl allein ist kein Name
		{"Dune", 1984}, // Neuverfilmungen …
		{"Dune", 2021}, // … sind keine Reihe
		{"New York, New York", 1977},
		{"New York Stories", 1989}, // zwei Wörter, nur zwei Filme: zu unsicher
		// Nur Nummern sind kein Beweis:
		{"Apollo 13", 1995},
		{"Apollo 18", 2011},
		{"District 9", 2009},
		{"District 13", 2004},
		{"Henry V", 1989},
		{"Henry VIII", 2003},
		{"Malcolm X", 1992},
		{"Malcolm & Marie", 2021},
		// Gleicher Anfang ist keine Reihe:
		{"The Man Who Knew Too Much", 1956},
		{"The Man Who Shot Liberty Valance", 1962},
		{"The Man Who Fell to Earth", 1976},
	}
	var filme []ReihenFilm
	for i, x := range titel {
		filme = append(filme, ReihenFilm{ID: fmt.Sprint(i), Titel: x.t, Jahr: x.j})
	}

	got := map[string][]string{}
	for _, g := range ReihenErkennen(filme) {
		var ts []string
		for _, f := range g.Filme {
			ts = append(ts, f.Titel)
		}
		got[g.Name] = ts
	}

	want := map[string]int{
		"Der Herr der Ringe":    3,
		"Harry Potter":          3,
		"Alien":                 2,
		"Rocky":                 3,
		"Mission: Impossible":   3,
		"Star Wars":             2,
		"Die Tribute von Panem": 2,
		"Shrek":                 2,
	}
	for name, n := range want {
		if len(got[name]) != n {
			t.Errorf("%q: %d Filme statt %d: %v", name, len(got[name]), n, got[name])
		}
	}
	for name, ts := range got {
		if _, ok := want[name]; !ok {
			t.Errorf("falsche Reihe %q: %v", name, ts)
		}
	}

	// In der Reihenfolge, in der sie erschienen sind.
	if r := got["Rocky"]; len(r) == 3 && (r[0] != "Rocky" || r[2] != "Rocky III") {
		t.Errorf("Rocky in falscher Reihenfolge: %v", r)
	}
}

// Dieselben Filme geben dieselben Reihen — egal in welcher Reihenfolge
// sie aus der Datenbank kommen.
func TestReihenSindStabil(t *testing.T) {
	titel := []string{"Alien", "Alien 3", "Shrek", "Shrek 2", "Shrek der Dritte"}
	ergebnis := func(reihenfolge []int) string {
		var filme []ReihenFilm
		for _, i := range reihenfolge {
			filme = append(filme, ReihenFilm{ID: fmt.Sprint(i), Titel: titel[i]})
		}
		var teile []string
		for _, g := range ReihenErkennen(filme) {
			var ids []string
			for _, f := range g.Filme {
				ids = append(ids, f.ID)
			}
			sort.Strings(ids)
			teile = append(teile, g.Schluessel+"="+strings.Join(ids, ","))
		}
		return strings.Join(teile, ";")
	}
	a := ergebnis([]int{0, 1, 2, 3, 4})
	b := ergebnis([]int{4, 3, 2, 1, 0})
	if a != b {
		t.Fatalf("Reihen hängen von der Reihenfolge ab:\n %s\n %s", a, b)
	}
}

// Was der Besitzer an einer Reihe ändert, bleibt — auch wenn Lumen die
// Reihen nach dem nächsten Durchlauf neu bildet. Und TMDb geht vor.
func TestReihenUeberlebenDasNeuBilden(t *testing.T) {
	ctx := t.Context()
	st, err := Open(t.TempDir() + "/l.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	lib, err := st.CreateLibrary(ctx, "Filme", "movie", t.TempDir(), "de")
	if err != nil {
		t.Fatal(err)
	}
	film := func(titel string, jahr int) string {
		it := &Item{LibraryID: lib.ID, Kind: "movie", Title: titel, Year: jahr, MatchState: "pending"}
		if err := st.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
		return it.ID
	}
	a1, a3 := film("Alien", 1979), film("Alien 3", 1992)
	r1, _, _ := film("Rocky", 1976), film("Rocky II", 1979), film("Rocky III", 1982)

	if n, err := st.ReihenNeuBilden(ctx); err != nil || n != 2 {
		t.Fatalf("%d Reihen, %v", n, err)
	}
	alien, err := st.ReiheVon(ctx, a1)
	if err != nil || alien == nil || alien.Name != "Alien" || len(alien.ItemIDs) != 2 {
		t.Fatalf("Alien-Reihe: %+v %v", alien, err)
	}
	if alien.ItemIDs[0] != a1 || alien.ItemIDs[1] != a3 {
		t.Errorf("nicht nach Erscheinen geordnet: %v", alien.ItemIDs)
	}

	// Umbenennen und auflösen …
	neu := "Die Alien-Filme"
	if err := st.ReiheAendern(ctx, alien.ID, &neu, nil); err != nil {
		t.Fatal(err)
	}
	rocky, _ := st.ReiheVon(ctx, r1)
	ja := true
	if err := st.ReiheAendern(ctx, rocky.ID, nil, &ja); err != nil {
		t.Fatal(err)
	}
	// … und neu bilden.
	if _, err := st.ReihenNeuBilden(ctx); err != nil {
		t.Fatal(err)
	}
	alien, _ = st.ReiheVon(ctx, a1)
	if alien == nil || alien.Name != neu {
		t.Errorf("der eigene Name ging verloren: %+v", alien)
	}
	if r, _ := st.ReiheVon(ctx, r1); r != nil {
		t.Errorf("die aufgelöste Reihe ist wieder da: %+v", r)
	}
	alle, _ := st.Reihen(ctx, true)
	if len(alle) != 2 {
		t.Errorf("%d Reihen insgesamt, die aufgelöste soll mitgezählt sein", len(alle))
	}

	// TMDb sagt etwas anderes — TMDb gewinnt.
	if err := st.TMDbReiheZuordnen(ctx, "8091", "Alien Collection", a3, 1992); err != nil {
		t.Fatal(err)
	}
	if err := st.TMDbReiheZuordnen(ctx, "8091", "Alien Collection", a1, 1979); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ReihenNeuBilden(ctx); err != nil {
		t.Fatal(err)
	}
	r, _ := st.ReiheVon(ctx, a1)
	if r == nil || r.Source != "tmdb" || r.Name != "Alien Collection" || len(r.ItemIDs) != 2 {
		t.Fatalf("TMDb-Reihe: %+v", r)
	}
	alle, _ = st.Reihen(ctx, true)
	for _, x := range alle {
		// Umbenannt bleibt sie stehen — aber ohne Filme, die gehören jetzt
		// zur TMDb-Reihe.
		if x.Source == "auto" && x.Name == neu && len(x.ItemIDs) > 0 {
			t.Errorf("die selbst erkannte Alien-Reihe steht noch daneben: %+v", x)
		}
	}
}

// Feste Uhrzeit: fällig, sobald sie heute vorbei ist und der letzte
// Durchlauf davor lag — und nicht zweimal am selben Tag.
func TestFaelligZurFestenUhrzeit(t *testing.T) {
	jetzt := time.Date(2026, 10, 29, 4, 30, 0, 0, time.Local)
	vorher := jetzt.Add(-5 * time.Hour)    // gestern 23:30
	danach := jetzt.Add(-10 * time.Minute) // heute 04:20
	l := Library{ScanAt: "04:00", IntervalMin: 60}
	if !faellig(l, jetzt) {
		t.Error("noch nie gelaufen: sollte fällig sein")
	}
	l.ScannedAt = &vorher
	if !faellig(l, jetzt) {
		t.Error("letzter Lauf vor 04:00: sollte fällig sein")
	}
	l.ScannedAt = &danach
	if faellig(l, jetzt) {
		t.Error("heute schon nach 04:00 gelaufen: nicht nochmal")
	}
	// Vor der Uhrzeit nie — auch wenn der Takt längst reif wäre.
	frueh := time.Date(2026, 10, 29, 3, 0, 0, 0, time.Local)
	l.ScannedAt = &vorher
	if faellig(l, frueh) {
		t.Error("vor 04:00 darf nichts laufen")
	}
	// Ohne Uhrzeit gilt der Takt.
	l.ScanAt = ""
	if !faellig(l, frueh) {
		t.Error("nach Takt: 3,5 Stunden sind mehr als 60 Minuten")
	}
}
