// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

import (
	"sort"
	"strings"
	"testing"
)

func TestSortierschluessel(t *testing.T) {
	cases := []struct {
		titel, sprache, want, buchstabe string
	}{
		{"Ärger im Paradies", "", "arger im paradies", "A"},
		{"The Matrix", "", "matrix", "M"},
		{"The Matrix", "de", "matrix", "M"},
		{"Der Herr der Ringe", "de", "herr der ringe", "H"},
		{"Der Herr der Ringe", "", "herr der ringe", "H"},
		{"Die Hard", "en", "die hard", "D"},
		{"die hard", "", "hard", "H"},
		{"10 Cloverfield Lane", "", "0000000010 cloverfield lane", "#"},
		{"2 Fast 2 Furious", "en", "0000000002 fast 0000000002 furious", "#"},
		{"Élite", "es", "elite", "E"},
		{"Ocean's Eleven", "en", "ocean's eleven", "O"},
		{"Teil 2", "", "teil 0000000002", "T"},
		{"Teil 10", "", "teil 0000000010", "T"},
		{"Ice Age 10", "", "ice age 0000000010", "I"},
		{"ice age 2", "", "ice age 0000000002", "I"},
		{"L'Odyssée", "fr", "odyssee", "O"},
		{"L’Odyssée", "fr", "odyssee", "O"},
		{"Les Misérables", "fr", "miserables", "M"},
		{"Il Postino", "it", "postino", "P"},
		{"El Laberinto del Fauno", "es", "laberinto del fauno", "L"},
		{"Ötzi", "", "otzi", "O"},
		{"Øresund", "", "oresund", "O"},
		{"Æon Flux", "en", "aeon flux", "A"},
		{"Łódź", "", "lodz", "L"},
		{`"Weird" Al`, "en", `weird" al`, "W"},
		{"...und dann kam Polly", "de", "und dann kam polly", "U"},
		{"Über den Wolken", "de", "uber den wolken", "U"},
		{"Zorro", "", "zorro", "Z"},
		// Kein Artikel ohne Leerzeichen danach, und nie der ganze Titel.
		{"A-Team", "en", "a-team", "A"},
		{"The", "en", "the", "T"},
		{"Das ...", "de", "das ...", "D"},
		{"Einem geschenkten Gaul", "de", "geschenkten gaul", "G"},
		// Zerlegt geschrieben (macOS-Dateinamen): a + Trema.
		{"Ärger", "de", "arger", "A"},
		{"Straße", "de", "strasse", "S"},
		{"Œdipe", "fr", "oedipe", "O"},
		{"Þór", "", "thor", "T"},
		{"Ђорђе", "", "ђорђе", "#"},
	}
	for _, c := range cases {
		got := Sortierschluessel(c.titel, c.sprache)
		if got != c.want {
			t.Errorf("Sortierschluessel(%q, %q) = %q, erwartet %q", c.titel, c.sprache, got, c.want)
		}
		if b := Buchstabe(got); b != c.buchstabe {
			t.Errorf("Buchstabe(%q) = %q, erwartet %q", got, b, c.buchstabe)
		}
	}
}

// Die Reihenfolge, wie sie im Raster erscheinen soll.
func TestSortierschluesselReihenfolge(t *testing.T) {
	titel := []string{"Zorro", "Teil 10", "Øresund", "Ötzi", "Teil 2", "Ocean's Eleven",
		"Ice Age 10", "ice age 2", "Æon Flux", "Ärger im Paradies", "Über den Wolken",
		"10 Cloverfield Lane", "2 Fast 2 Furious", "Łódź", "The Matrix", "Der Herr der Ringe"}
	sort.SliceStable(titel, func(i, j int) bool {
		return Sortierschluessel(titel[i], "") < Sortierschluessel(titel[j], "")
	})
	want := []string{"2 Fast 2 Furious", "10 Cloverfield Lane", "Æon Flux", "Ärger im Paradies",
		"Der Herr der Ringe", "ice age 2", "Ice Age 10", "Łódź", "The Matrix", "Ocean's Eleven",
		"Øresund", "Ötzi", "Teil 2", "Teil 10", "Über den Wolken", "Zorro"}
	if strings.Join(titel, "|") != strings.Join(want, "|") {
		t.Fatalf("Reihenfolge\n%s\nerwartet\n%s", strings.Join(titel, " | "), strings.Join(want, " | "))
	}
}

func TestSortierSprache(t *testing.T) {
	if s := SortierSprache("de", "en", "Die Hard", "Die Hard"); s != "en" {
		t.Errorf("Originaltitel: %q", s)
	}
	if s := SortierSprache("de", "en", "Der Herr der Ringe", "The Lord of the Rings"); s != "de" {
		t.Errorf("übersetzter Titel: %q", s)
	}
	if s := SortierSprache("de", "", "Die Hard", "Die Hard"); s != "de" {
		t.Errorf("ohne Originalsprache: %q", s)
	}
}
