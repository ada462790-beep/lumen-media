// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package stream

import (
	"testing"

	"lumen/internal/scan"
)

func TestKapitelArt(t *testing.T) {
	const folge = 42 * 60 * 1000
	cases := []struct {
		c      scan.Chapter
		total  int64
		anzahl int
		want   string
	}{
		{scan.Chapter{Index: 1, Title: "Intro", StartMS: 2000, EndMS: 12000}, 60_000, 4, "intro"},
		{scan.Chapter{Index: 3, Title: "Abspann", StartMS: 45_000, EndMS: 60_000}, 60_000, 4, "credits"},
		// Kurze Datei, nichtssagende Titel: nichts raten.
		{scan.Chapter{Index: 0, Title: "Kalter Anfang", StartMS: 0, EndMS: 2000}, 60_000, 4, "normal"},
		{scan.Chapter{Index: 2, Title: "Folge", StartMS: 12_000, EndMS: 45_000}, 60_000, 4, "normal"},
		// Lange Folge: ein kurzes Kapitel am Anfang ist der Vorspann …
		{scan.Chapter{Index: 1, Title: "Chapter 2", StartMS: 90_000, EndMS: 150_000}, folge, 6, "intro"},
		// … ein Kapitel bis zum Schluss der Abspann.
		{scan.Chapter{Index: 5, Title: "Chapter 6", StartMS: folge - 100_000, EndMS: folge}, folge, 6, "credits"},
		// Mitten drin: nichts.
		{scan.Chapter{Index: 3, Title: "Chapter 4", StartMS: 20 * 60 * 1000, EndMS: 30 * 60 * 1000}, folge, 6, "normal"},
		// „op" nur als ganzes Wort — nicht in „Open Sea".
		{scan.Chapter{Index: 2, Title: "Open Sea", StartMS: 10 * 60 * 1000, EndMS: 20 * 60 * 1000}, folge, 6, "normal"},
		{scan.Chapter{Index: 0, Title: "OP", StartMS: 0, EndMS: 90_000}, folge, 6, "intro"},
	}
	for _, c := range cases {
		if got := KapitelArt(c.c, c.total, c.anzahl); got != c.want {
			t.Errorf("%q (%d–%d von %d): %s, erwartet %s", c.c.Title, c.c.StartMS, c.c.EndMS, c.total, got, c.want)
		}
	}
}
