// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package scan

import (
	"path/filepath"

	"lumen/internal/meta"
	"lumen/internal/store"
)

// Die Zuordnung sucht einen schon einmal zugeordneten Film beim Neuladen
// nach dem Namen seiner Dateien (meta/nachpruefen.go) — mit derselben
// Lesart wie der Durchlauf.
func init() { meta.DateititelLesen = DateititelFuer }

// DateititelFuer: Titel und Jahr, die die Datei pfad in der Bibliothek lib
// nach ihrem Namen hätte („_Matrix/The Animatrix/The Animatrix - a.m2ts"
// → „The Animatrix", 0).
func DateititelFuer(lib store.Library, pfad string) (string, int) {
	rel, err := filepath.Rel(lib.Path, pfad)
	if err != nil {
		return "", 0
	}
	dirs := splitDirs(rel)
	g := teileKlaeren(ParseFuer(pfad, dirs, lib.Kind), pfad, dirs, lib.Kind, nil)
	if g.Kind != "movie" {
		return "", 0
	}
	return huebsch(store.NFC(g.Title)), g.Year
}
