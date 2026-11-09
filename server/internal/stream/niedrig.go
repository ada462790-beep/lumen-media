// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package stream

import (
	"bytes"
	"os/exec"
)

// Niedrig führt einen Hintergrund-Aufruf (ffprobe beim Scan, Standbilder,
// Vorschau-Clips, Teile zusammenfügen) mit der niedrigsten Rechen- und
// Platten-Priorität aus und gibt die Ausgabe zurück — wie
// cmd.CombinedOutput() bzw. cmd.Output().
//
// „Wiedergabe hat Vorrang". Auf einem NAS mit schwachem
// Prozessor soll ein Scan nie das Bild ruckeln lassen; das Umwandeln für
// die Wiedergabe läuft mit normaler Priorität und gewinnt damit immer.
func Niedrig(cmd *exec.Cmd, mitFehlerausgabe bool) ([]byte, error) {
	var out, fehl bytes.Buffer
	cmd.Stdout = &out
	if mitFehlerausgabe {
		cmd.Stderr = &out
	} else {
		cmd.Stderr = &fehl
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	prioritaetSenken(cmd.Process.Pid)
	err := cmd.Wait()
	if err != nil && !mitFehlerausgabe {
		if ee, ok := err.(*exec.ExitError); ok {
			ee.Stderr = fehl.Bytes()
		}
	}
	return out.Bytes(), err
}
