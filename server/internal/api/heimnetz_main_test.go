// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"os"
	"testing"
)

// Die Tests sprechen Lumen über 127.0.0.1 an — wie die Gesundheitsprüfung
// im eigenen Container. Ohne Container (nativ) gälte 127.0.0.1 als „der
// Rechner selbst" und damit als unterwegs (heimnetz.go); darum laufen die
// Tests so, als läge Lumen in seinem eigenen Container-Netz. Wer etwas
// anderes prüft, setzt die Sicht mit mitSicht.
func TestMain(m *testing.M) {
	sichtFinden = func() netzSicht {
		s := eigeneSicht()
		s.bruecke, s.hostNetz = true, false
		return s
	}
	os.Exit(m.Run())
}
