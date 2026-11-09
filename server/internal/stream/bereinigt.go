// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package stream

import (
	"regexp"
	"strings"
)

// Fehlermeldungen von FFmpeg und vom Betriebssystem nennen gern den Pfad
// der Datei — und damit den Titel. Bereinigt nimmt alles heraus, was
// nach Pfad aussieht, bevor eine Meldung ins Protokoll oder an ein Gerät
// geht. Übrig bleibt der technische Grund („Invalid data found …").
// Ein Pfad beginnt bei einem Schrägstrich (oder „C:\") und reicht bis zum
// nächsten „: ", Anführungszeichen oder Zeilenende — Namen mit Leerzeichen
// („Mein Film (2009)/film.mkv") eingeschlossen.
var pfadMuster = regexp.MustCompile(`(?:\b[A-Za-z]:)?[/\\][^'"\n]*?(?:: |['"]|\n|$)`)

// Bereinigt entfernt Pfade und kürzt die Meldung.
func Bereinigt(msg string) string {
	msg = pfadMuster.ReplaceAllStringFunc(msg, func(m string) string {
		switch {
		case strings.HasSuffix(m, ": "):
			return "…: "
		case strings.HasSuffix(m, "\n"):
			return "…\n"
		case strings.HasSuffix(m, `'`), strings.HasSuffix(m, `"`):
			return "…" + m[len(m)-1:]
		}
		return "…"
	})
	// Was in Anführungszeichen steht, ist bei FFmpeg fast immer ein Name.
	for _, q := range []string{`'`, `"`} {
		for {
			a := strings.Index(msg, q)
			if a < 0 {
				break
			}
			b := strings.Index(msg[a+1:], q)
			if b < 0 {
				break
			}
			msg = msg[:a] + "…" + msg[a+1+b+1:]
		}
	}
	msg = strings.TrimSpace(msg)
	if len(msg) > 160 {
		msg = msg[:160] + "…"
	}
	return msg
}
