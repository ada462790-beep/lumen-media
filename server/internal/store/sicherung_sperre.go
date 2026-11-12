// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

// Eine Sperre für den Ordner der Sicherungen.
//
// Sichern (jede Nacht), Bereinigen nach „PIN zurücksetzen" (im Server im
// Hintergrund, oder von der Kommandozeile mit „lumen zuruecksetzen",
// während der Server läuft) und Wiederherstellen fassen dieselben Dateien
// an. Liefen zwei davon gleichzeitig, könnte das Bereinigen eine Datei
// bearbeiten, die das Sichern gerade umbenennt — und die neue Sicherung
// enthielte den gelöschten Verlauf wieder. Darum nimmt jeder dieser Wege
// zuerst die Sperre: eine Datei neben dem Ordner („sicherungen.sperre",
// nicht darin — dort liegen genau die Sicherungen), über das
// Betriebssystem gesperrt (sperreNehmen), damit es auch zwischen dem
// Server und der Kommandozeile gilt.

import (
	"context"
	"os"
	"path/filepath"
	"time"
)

// sicherungSperren wartet, bis der Ordner frei ist (oder ctx endet), und
// gibt die Funktion zum Freigeben zurück.
func sicherungSperren(ctx context.Context, ordner string) (func(), error) {
	ordner = filepath.Clean(ordner)
	if err := os.MkdirAll(filepath.Dir(ordner), 0o700); err != nil {
		return nil, err
	}
	pfad := ordner + ".sperre"
	for {
		frei, err := sperreNehmen(pfad)
		if err != nil {
			return nil, err
		}
		if frei != nil {
			return frei, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}
