// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

//go:build !linux

package main

import "io"

// Ausserhalb von Linux läuft Lumen nicht als Docker-Container mit fremdem
// Benutzer — nichts zu wechseln.
func alsBesitzerDer(string) error { return nil }

// ohneEcho: ausserhalb von Linux bleibt die Anzeige an.
func ohneEcho(io.Reader) func() { return func() {} }
