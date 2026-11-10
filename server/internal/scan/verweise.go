// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package scan

import (
	"io/fs"
	"path/filepath"
	"strings"
	"sync/atomic"
)

// geschuetzt ist Lumens Datenordner (Datenbank, Schlüssel, Sicherungen).
var geschuetzt atomic.Pointer[string]

// DatenordnerSchuetzen nennt den Ordner, in den kein Verweis aus einer
// Mediathek zeigen darf.
func DatenordnerSchuetzen(ordner string) {
	if ordner == "" {
		return
	}
	if echt, err := filepath.EvalSymlinks(ordner); err == nil {
		ordner = echt
	}
	if abs, err := filepath.Abs(ordner); err == nil {
		ordner = abs
	}
	geschuetzt.Store(&ordner)
}

// VerweisInDatenordner meldet einen symbolischen Verweis, der in Lumens
// Datenordner zeigt. Solche Dateien nimmt Lumen nicht auf: Ein „Film" als
// Verweis auf /data/lumen.db hätte jedem Profil die ganze Datenbank zum
// Herunterladen gegeben. Verweise anderswohin (auf eine zweite Freigabe,
// ein anderes Volume) sind auf einem NAS üblich und bleiben erlaubt.
func VerweisInDatenordner(pfad string, d fs.DirEntry) bool {
	if d == nil || d.Type()&fs.ModeSymlink == 0 {
		return false
	}
	return InDatenordner(pfad)
}

// InDatenordner prüft, ob pfad nach dem Auflösen aller Verweise im
// Datenordner liegt. Was sich nicht auflösen lässt, gilt als verdächtig.
func InDatenordner(pfad string) bool {
	d := geschuetzt.Load()
	if d == nil {
		return false
	}
	ziel, err := filepath.EvalSymlinks(pfad)
	if err != nil {
		return true
	}
	rel, err := filepath.Rel(*d, ziel)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
