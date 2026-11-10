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
)

// VerweisNachDraussen meldet einen symbolischen Verweis, der aus dem
// Medienordner hinauszeigt. Solche Dateien nimmt Lumen nicht auf: Ein
// „Lied" als Verweis auf /data/lumen.db hätte jedem Profil die ganze
// Datenbank zum Herunterladen gegeben. Verweise innerhalb des Ordners
// (aufgeräumte Sammlungen) bleiben erlaubt.
func VerweisNachDraussen(wurzel, pfad string, d fs.DirEntry) bool {
	if d == nil || d.Type()&fs.ModeSymlink == 0 {
		return false
	}
	return !InnerhalbVon(wurzel, pfad)
}

// InnerhalbVon prüft, ob pfad nach dem Auflösen aller Verweise in wurzel
// liegt. Was sich nicht auflösen lässt, gilt als draussen.
func InnerhalbVon(wurzel, pfad string) bool {
	ziel, err := filepath.EvalSymlinks(pfad)
	if err != nil {
		return false
	}
	w, err := filepath.EvalSymlinks(wurzel)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(w, ziel)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
