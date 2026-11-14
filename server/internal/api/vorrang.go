// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"net/http"
	"strings"

	"lumen/internal/store"
)

// vorrang meldet Anfragen der Bedienung an (store.BedienungBeginnt):
// Durchlauf, Metadaten und Nachträge lassen ihnen zwischen zwei Schritten
// den Vortritt. Wiedergaben zählen nicht — sie dauern Stunden und würden
// die Hintergrundarbeit sonst ständig bremsen.
func vorrang(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/v1/stream/") {
			next.ServeHTTP(w, r)
			return
		}
		fertig := store.BedienungBeginnt()
		defer fertig()
		next.ServeHTTP(w, r)
	})
}
