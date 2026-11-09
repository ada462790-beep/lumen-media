// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

//go:build unix

package stream

import (
	"os"
	"syscall"
)

// anhalten friert FFmpeg ein (SIGSTOP), weiter lässt es weiterlaufen. Ein
// eingefrorener Prozess braucht keine Rechenzeit, behält aber alles, was
// er schon weiss — weiterrechnen geht ohne Neustart und ohne Ruckeln.
func anhalten(p *os.Process) bool { return p.Signal(syscall.SIGSTOP) == nil }
func weiter(p *os.Process) bool   { return p.Signal(syscall.SIGCONT) == nil }

// freierPlatz meldet die freien Bytes im Ordner (0, wenn unbekannt).
func freierPlatz(dir string) uint64 {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0
	}
	return st.Bavail * uint64(st.Bsize)
}

// FreierPlatz ist freierPlatz für die Übersicht der Verwaltung.
func FreierPlatz(dir string) uint64 { return freierPlatz(dir) }
