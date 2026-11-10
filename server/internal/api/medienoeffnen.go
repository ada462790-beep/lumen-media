// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
)

// datenOrdner ist der Ordner mit Datenbank, Schlüsseln und Sicherungen.
// Aus ihm liefert Lumen nie eine „Mediendatei" aus — auch nicht, wenn ein
// Verweis im Medienordner nachträglich dorthin umgebogen wurde.
var datenOrdner atomic.Pointer[string]

func datenOrdnerSetzen(pfad string) {
	if pfad == "" {
		return
	}
	if echt, err := filepath.EvalSymlinks(pfad); err == nil {
		pfad = echt
	}
	if abs, err := filepath.Abs(pfad); err == nil {
		pfad = abs
	}
	datenOrdner.Store(&pfad)
}

// medienOeffnen öffnet eine Mediendatei zum Ausliefern.
func medienOeffnen(pfad string) (*os.File, error) {
	if d := datenOrdner.Load(); d != nil {
		echt, err := filepath.EvalSymlinks(pfad)
		if err != nil {
			return nil, err
		}
		if rel, err := filepath.Rel(*d, echt); err == nil && rel != ".." &&
			!strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil, os.ErrPermission
		}
	}
	return os.Open(pfad)
}
