// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"os"
	"strconv"
)

// bildAufbewahren setzt die Köpfe, mit denen ein Browser ein Bild behält.
//
// Die Adresse eines Posters bleibt gleich, auch wenn jemand ein anderes
// zuweist. Darum: zehn Minuten ohne Nachfrage, danach zeigt der Browser
// das Bild sofort und fragt im Hintergrund nach (stale-while-revalidate)
// — ist es gleich, kommt ein 304 von ein paar hundert Byte statt des
// ganzen Bildes. Ob es gleich ist, sagt der ETag aus Name, Grösse und
// Änderungszeit der Datei: Ein neues Bild hat immer einen anderen.
func bildAufbewahren(w http.ResponseWriter, name string, info os.FileInfo) {
	h := sha256.Sum256([]byte(name + "\x00" + strconv.FormatInt(info.Size(), 10) + "\x00" +
		strconv.FormatInt(info.ModTime().UnixNano(), 10)))
	w.Header().Set("ETag", `"`+base64.RawURLEncoding.EncodeToString(h[:12])+`"`)
	w.Header().Set("Cache-Control", "private, max-age=600, stale-while-revalidate=2592000")
}
