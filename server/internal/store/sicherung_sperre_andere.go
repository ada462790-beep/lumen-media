// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

//go:build !unix

package store

import (
	"errors"
	"os"
	"time"
)

// sperreNehmen ohne flock: eine Datei, die nur einer anlegen kann. Bleibt
// sie nach einem Absturz liegen, gilt sie nach einer Stunde als verwaist.
func sperreNehmen(pfad string) (func(), error) {
	pfad += ".belegt"
	f, err := os.OpenFile(pfad, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		if fi, e := os.Stat(pfad); e == nil && time.Since(fi.ModTime()) > time.Hour {
			_ = os.Remove(pfad)
		}
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	f.Close()
	return func() { _ = os.Remove(pfad) }, nil
}
