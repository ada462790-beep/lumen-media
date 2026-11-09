// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

import "testing"

func TestDeuten(t *testing.T) {
	genres := []string{"Komödie", "Thriller", "Science-Fiction"}
	leute := []string{"Tom Hanks", "Meg Ryan", "Al Pacino"}
	cases := []struct {
		in   string
		want Deutung
	}{
		{"Filme mit Tom Hanks aus den 90ern", Deutung{Person: "Tom Hanks", JahrVon: 1990, JahrBis: 1999}},
		{"Komödie 2019", Deutung{Genre: "Komödie", JahrVon: 2019, JahrBis: 2019}},
		{"thriller 80er", Deutung{Genre: "Thriller", JahrVon: 1980, JahrBis: 1989}},
		{"2000er science-fiction", Deutung{Genre: "Science-Fiction", JahrVon: 2000, JahrBis: 2009}},
		{"Die Bucht", Deutung{Text: "Die Bucht"}},
		{"meg ryan komödie", Deutung{Person: "Meg Ryan", Genre: "Komödie"}},
		{"Alien 1979", Deutung{Text: "Alien", JahrVon: 1979, JahrBis: 1979}},
		{"Hanks", Deutung{Text: "Hanks"}}, // kein ganzer Name — bleibt Text (und findet die Person über die Trefferliste)
		{"20er", Deutung{JahrVon: 2020, JahrBis: 2029}},
	}
	for _, c := range cases {
		got := Deuten(c.in, genres, leute)
		if got != c.want {
			t.Errorf("%q: %+v, erwartet %+v", c.in, got, c.want)
		}
	}
}
