// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

//go:build !unix

package stream

import "os"

// Unter Windows gibt es kein Einfrieren. Die Bremse beendet FFmpeg dann
// stattdessen (siehe bremsen), und die nächste Anfrage setzt neu an.
func anhalten(p *os.Process) bool { return false }
func weiter(p *os.Process) bool   { return false }

func freierPlatz(dir string) uint64 { return 0 }

func FreierPlatz(dir string) uint64 { return 0 }
