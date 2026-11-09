// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

//go:build windows

package update

import "errors"

func neuAusfuehren(pfad string, argv, env []string) error {
	return errors.New("auf Windows startet Lumen über einen neuen Prozess")
}
