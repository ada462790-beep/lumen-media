// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"os"
	"path/filepath"
	"regexp"

	"lumen/internal/store"
)

// mediathekUnterordner: was frühere Fassungen unter <Daten>/mediathek
// angelegt haben — Vorschaubilder der Fotos, Cover, umgewandelte Lieder
// und Videos.
var mediathekUnterordner = []string{"vorschau", "cover", "musik-umgewandelt", "video-umgewandelt"}

// kurzlebigName: so hiessen die kurzlebigen Umwandlungen früherer
// Fassungen — 32 Zeichen Zufall in Hex und eine Endung („….m4a").
var kurzlebigName = regexp.MustCompile(`^[0-9a-f]{32}\.[a-z0-9]{2,5}$`)

// MediathekResteWeg löscht, was frühere Fassungen für Musik und Fotos auf
// der Platte hinterlassen haben: die bekannten Unterordner unter
// <Daten>/mediathek und die kurzlebigen Umwandlungen unter
// <Umwandlungsordner>/kurzlebig. Lumen ist ein Server für Filme und Serien.
//
// Das geschieht genau einmal, nach dem Datenbank-Umbau auf Stand 10
// (store/musik_fotos_entfernen.go setzt dafür eine Einstellung), und nur
// mit dem, was Lumen selbst angelegt hat: fremde Dateien in diesen Ordnern
// bleiben, und ein Ordner verschwindet nur, wenn er danach leer ist.
func (s *Server) MediathekResteWeg() {
	ctx := context.Background()
	if s.st.Setting(ctx, store.MediathekResteEinst, "") != "1" {
		return
	}
	mediathek := filepath.Join(s.dataDir, "mediathek")
	for _, u := range mediathekUnterordner {
		_ = os.RemoveAll(filepath.Join(mediathek, u))
	}
	_ = os.Remove(mediathek) // nur, wenn leer

	kurz := filepath.Join(orElse(s.umwandlungDir, s.dataDir), "kurzlebig")
	if eintraege, err := os.ReadDir(kurz); err == nil {
		for _, e := range eintraege {
			if e.Type().IsRegular() && kurzlebigName.MatchString(e.Name()) {
				_ = os.Remove(filepath.Join(kurz, e.Name()))
			}
		}
		_ = os.Remove(kurz) // nur, wenn leer
	}
	_ = s.st.SetSetting(ctx, store.MediathekResteEinst, "")
}
