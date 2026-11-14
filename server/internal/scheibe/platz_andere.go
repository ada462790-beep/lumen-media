// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

//go:build !unix

package scheibe

// FreierPlatz: ohne Statfs unbekannt (0) — die Prüfung entfällt dann.
func FreierPlatz(dir string) uint64 { return 0 }
