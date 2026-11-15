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
func bildAufbewahren(w http.ResponseWriter, r *http.Request, name string, info os.FileInfo) {
	h := sha256.Sum256([]byte(name + "\x00" + strconv.FormatInt(info.Size(), 10) + "\x00" +
		strconv.FormatInt(info.ModTime().UnixNano(), 10)))
	w.Header().Set("ETag", `"`+base64.RawURLEncoding.EncodeToString(h[:12])+`"`)
	w.Header().Set("Cache-Control", bildHaltbarkeit(r))
}

// bildHaltbarkeit: Trägt die Adresse den Stand des Bildes (?v=…, siehe
// bildAdresse), ändert sie sich mit jedem neuen Bild — dann darf der
// Browser es ein Jahr behalten, ohne nachzufragen. Ohne Stand (ältere
// Apps, Bilder aus dem Film) wie bisher: zehn Minuten, dann im
// Hintergrund nachfragen.
func bildHaltbarkeit(r *http.Request) string {
	if r != nil && r.URL.Query().Get("v") != "" {
		return "private, max-age=31536000, immutable"
	}
	return "private, max-age=600, stale-while-revalidate=2592000"
}

// bildAdresse: die Adresse eines Bildes, mit Stand, wenn es einen gibt.
// Der Parameter ist freiwillig: Der Server liefert dasselbe Bild mit und
// ohne, nur die Haltbarkeit unterscheidet sich (bildHaltbarkeit).
func bildAdresse(id, kind, stand string) string {
	u := "/api/v1/art/" + id + "/" + kind
	if stand != "" {
		u += "?v=" + stand
	}
	return u
}

// dateiStand: ein Stand aus dem Dateinamen allein — für Bilder, deren
// Name sich mit jedem neuen Bild ändert (von Hand gesetzt: „…-hand-<zufall>").
func dateiStand(name string) string {
	if name == "" {
		return ""
	}
	h := sha256.Sum256([]byte(name))
	return base64.RawURLEncoding.EncodeToString(h[:5])
}
