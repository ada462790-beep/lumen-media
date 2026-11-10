// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"os"
	"path/filepath"
)

// MediathekResteWeg löscht beim Start, was frühere Fassungen für Musik und
// Fotos auf der Platte hinterlassen haben: Cover, Vorschaubilder und
// umgewandelte Lieder unter <Daten>/mediathek und die kurzlebigen
// Umwandlungen unter <Umwandlungsordner>/kurzlebig. Lumen ist ein Server
// für Filme und Serien; beide Ordner gehörten nur zu Musik und Fotos.
// Die Tabellen räumt der Datenbank-Umbau auf Stand 10 weg
// (store/musik_fotos_entfernen.go). Gibt es die Ordner nicht, geschieht
// nichts.
func (s *Server) MediathekResteWeg() {
	for _, ordner := range []string{
		filepath.Join(s.dataDir, "mediathek"),
		filepath.Join(orElse(s.umwandlungDir, s.dataDir), "kurzlebig"),
	} {
		if _, err := os.Stat(ordner); err == nil {
			_ = os.RemoveAll(ordner)
		}
	}
}
