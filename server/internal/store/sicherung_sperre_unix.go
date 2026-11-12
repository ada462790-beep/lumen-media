// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

//go:build unix

package store

import (
	"errors"
	"os"
	"syscall"
)

// sperreNehmen versucht einmal, die Datei exklusiv zu sperren (flock).
// nil ohne Fehler: gerade belegt. Die Sperre gilt je geöffneter Datei —
// also auch zwischen zwei Wegen im selben Prozess — und fällt von selbst,
// wenn ein Prozess endet.
func sperreNehmen(pfad string) (func(), error) {
	f, err := os.OpenFile(pfad, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, nil
		}
		return nil, err
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}, nil
}
