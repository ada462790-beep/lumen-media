// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

//go:build linux

package stream

import "syscall"

// prioritaetSenken: nice 19 und Platten-Klasse „idle" (wie ionice -c3).
// Geht es nicht (fehlende Rechte), läuft der Aufruf eben normal weiter.
func prioritaetSenken(pid int) {
	_ = syscall.Setpriority(syscall.PRIO_PROCESS, pid, 19)
	const (
		ioprioWhoProcess = 1
		ioprioClassIdle  = 3
		ioprioClassShift = 13
	)
	_, _, _ = syscall.Syscall(syscall.SYS_IOPRIO_SET, ioprioWhoProcess, uintptr(pid), ioprioClassIdle<<ioprioClassShift)
}
