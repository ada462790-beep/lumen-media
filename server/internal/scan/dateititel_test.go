// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package scan

import (
	"path/filepath"
	"testing"

	"lumen/internal/meta"
	"lumen/internal/store"
)

// Die Lesart für das Neuladen (meta/nachpruefen.go) ist die des
// Durchlaufs: Teile „- a"/„- b" gehören zum Titel ohne Buchstaben, ein
// Sammelordner („_Matrix") zählt nicht zum Titel.
//
// „Dune - Der Wüstenplanet Teil 1/2 (2000)" bleibt bewusst ein Film in
// Teilen: gleiches Jahr, gleicher Name, nur die Teilnummer verschieden
// (parse.go, teilMitJahr). Der Zweiteiler steht bei TMDb als ein Eintrag;
// wer zwei Filme will, trennt sie unter Aufräumen („Sind verschiedene
// Filme").
func TestDateititelFuer(t *testing.T) {
	if meta.DateititelLesen == nil {
		t.Fatal("scan trägt die Lesart nicht bei meta ein")
	}
	wurzel := t.TempDir()
	dateienAnlegen(t, wurzel,
		"_Matrix/The Animatrix/The Animatrix - a.m2ts",
		"_Matrix/The Animatrix/The Animatrix - b.m2ts",
		"Dune - Der Wüstenplanet (2000)/Dune - Der Wüstenplanet Teil 1 (2000).mkv",
		"Dune - Der Wüstenplanet (2000)/Dune - Der Wüstenplanet Teil 2 (2000).mkv",
		"Plan b (2021)/Plan b.mkv",
	)
	lib := store.Library{Path: wurzel, Kind: "movie"}
	for pfad, soll := range map[string]struct {
		titel string
		jahr  int
	}{
		"_Matrix/The Animatrix/The Animatrix - a.m2ts":                             {"The Animatrix", 0},
		"_Matrix/The Animatrix/The Animatrix - b.m2ts":                             {"The Animatrix", 0},
		"Dune - Der Wüstenplanet (2000)/Dune - Der Wüstenplanet Teil 2 (2000).mkv": {"Dune: Der Wüstenplanet", 2000},
		"Plan b (2021)/Plan b.mkv":                                                 {"Plan b", 2021},
	} {
		titel, jahr := DateititelFuer(lib, filepath.Join(wurzel, pfad))
		if titel != soll.titel || jahr != soll.jahr {
			t.Errorf("%s: %q (%d), erwartet %q (%d)", pfad, titel, jahr, soll.titel, soll.jahr)
		}
	}
}
