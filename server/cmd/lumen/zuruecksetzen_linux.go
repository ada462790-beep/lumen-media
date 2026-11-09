// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"fmt"
	"os"
	"syscall"
)

// alsBesitzerDer wechselt als root auf Benutzer und Gruppe, denen die
// Datenbank gehört. „docker exec" läuft als root, Lumen selbst als PUID —
// legte root die Hilfsdateien der Datenbank an, könnte Lumen sie danach
// nicht mehr öffnen.
func alsBesitzerDer(pfad string) error {
	if os.Geteuid() != 0 {
		return nil
	}
	info, err := os.Stat(pfad)
	if err != nil {
		return err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || st.Uid == 0 {
		return nil
	}
	if err := syscall.Setgid(int(st.Gid)); err != nil {
		return fmt.Errorf("auf die Gruppe der Datenbank wechseln: %w", err)
	}
	if err := syscall.Setuid(int(st.Uid)); err != nil {
		return fmt.Errorf("auf den Besitzer der Datenbank wechseln: %w", err)
	}
	return nil
}
