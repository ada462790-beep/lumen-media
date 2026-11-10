// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"fmt"
	"io"
	"os"
	"syscall"
	"unsafe"
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
	// Erst die Zusatzgruppen von root ablegen (disk, wheel …) — sonst
	// behielte der Befehl sie, obwohl er nur die Datenbank braucht.
	if err := syscall.Setgroups([]int{}); err != nil {
		return fmt.Errorf("zusatzgruppen ablegen: %w", err)
	}
	if err := syscall.Setgid(int(st.Gid)); err != nil {
		return fmt.Errorf("auf die Gruppe der Datenbank wechseln: %w", err)
	}
	if err := syscall.Setuid(int(st.Uid)); err != nil {
		return fmt.Errorf("auf den Besitzer der Datenbank wechseln: %w", err)
	}
	return nil
}

// ohneEcho schaltet die Anzeige der Eingabe ab, wenn ein ein Terminal ist —
// der neue PIN soll nicht auf dem Bildschirm stehen. Zurück gibt es eine
// Funktion, die den alten Zustand wiederherstellt.
func ohneEcho(ein io.Reader) func() {
	f, ok := ein.(*os.File)
	if !ok {
		return func() {}
	}
	fd := f.Fd()
	var alt syscall.Termios
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TCGETS, uintptr(unsafe.Pointer(&alt))); e != 0 {
		return func() {} // kein Terminal (Eingabe aus einer Datei oder Pipe)
	}
	neu := alt
	neu.Lflag &^= syscall.ECHO
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TCSETS, uintptr(unsafe.Pointer(&neu))); e != 0 {
		return func() {}
	}
	return func() {
		_, _, _ = syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TCSETS, uintptr(unsafe.Pointer(&alt)))
	}
}
