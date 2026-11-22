// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

//go:build !windows

package main

import (
	"fmt"
	"os"
)

// Auf anderen Systemen lässt sich das Paket übersetzen und testen (die
// Serverliste, die Adressen, der Update-Hinweis), aber kein Fenster öffnen.
func starten(optionen) int {
	fmt.Fprintln(os.Stderr, "Lumen für Windows läuft nur unter Windows. Im Browser geht Lumen überall.")
	return 2
}
