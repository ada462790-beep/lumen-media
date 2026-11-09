// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

import "testing"

func TestVerbessern(t *testing.T) {
	woerter := suchWoerter("Die Sonne Apocalypse Now Der Herr der Ringe Tod auf dem Nil Alien Inception")
	faelle := []struct {
		ein, will string
		ja        bool
	}{
		{"Sone", "Sonne", true},
		{"Apokalypse", "Apocalypse", true},
		{"apokalypse now", "apocalypse now", true},
		{"Incpetion", "Inception", true}, // vertauschte Buchstaben: ein Fehler
		{"Sonne", "", false},             // stimmt schon
		{"Ton", "", false},               // zu kurz für einen Vorschlag — nicht „Tod"
		{"Xylophon", "", false},          // nichts Ähnliches
	}
	for _, f := range faelle {
		got, ja := Verbessern(f.ein, woerter)
		if ja != f.ja || got != f.will {
			t.Errorf("Verbessern(%q) = %q, %v; will %q, %v", f.ein, got, ja, f.will, f.ja)
		}
	}
}

func TestAbstand(t *testing.T) {
	for _, f := range []struct {
		a, b string
		d    int
	}{{"sone", "sonne", 1}, {"abc", "abc", 0}, {"ab", "ba", 1}, {"", "abc", 3}, {"kitten", "sitting", 3}} {
		if got := abstand(f.a, f.b); got != f.d {
			t.Errorf("abstand(%q,%q) = %d, will %d", f.a, f.b, got, f.d)
		}
	}
}

func TestGeraeteKennung(t *testing.T) {
	a, b := GeraeteKennung([]byte{1, 2, 3}), GeraeteKennung([]byte{1, 2, 4})
	if a == b || len(a) != 24 {
		t.Fatalf("Kennungen %q und %q", a, b)
	}
	if a != GeraeteKennung([]byte{1, 2, 3}) {
		t.Fatal("Kennung nicht stabil")
	}
}
