// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import "net/http"

// routenFunktionen hängt die Wege für Handbearbeitung, eigene
// Sammlungen, die Übernahme aus Emby/Jellyfin und das Holen von
// Untertiteln ein. Eigene Datei, damit Handler() nur um einen Aufruf
// wächst.
func (s *Server) routenFunktionen(mux *http.ServeMux) {
	auth, admin := s.requireAuth, s.requireAdmin

	// Metadaten von Hand (bearbeiten.go). Mitverwalter dürfen das.
	mux.Handle("GET /api/v1/admin/items/{id}/metadata", admin(s.adminBearbeitenLesen))
	mux.Handle("PATCH /api/v1/admin/items/{id}/metadata", admin(s.adminBearbeitenSpeichern))
	// Android kennt kein PATCH (siehe POST /api/v1/me).
	mux.Handle("POST /api/v1/admin/items/{id}/metadata", admin(s.adminBearbeitenSpeichern))
	mux.Handle("POST /api/v1/admin/items/{id}/artwork/{kind}", admin(s.adminBildSetzen))
	mux.Handle("DELETE /api/v1/admin/items/{id}/lock/{feld}", admin(s.adminSperreAufheben))

	// Eigene Sammlungen (sammlungen.go).
	mux.Handle("GET /api/v1/sammlungen", auth(s.sammlungenListe))
	mux.Handle("GET /api/v1/sammlungen/{id}", auth(s.sammlungZeigen))
	mux.Handle("GET /api/v1/sammlungen/{id}/bild", auth(s.sammlungBildZeigen))
	mux.Handle("GET /api/v1/admin/sammlungen", admin(s.adminSammlungen))
	mux.Handle("POST /api/v1/admin/sammlungen", admin(s.adminSammlungAnlegen))
	mux.Handle("POST /api/v1/admin/sammlungen/{id}", admin(s.adminSammlungAendern))
	mux.Handle("PATCH /api/v1/admin/sammlungen/{id}", admin(s.adminSammlungAendern))
	mux.Handle("DELETE /api/v1/admin/sammlungen/{id}", admin(s.adminSammlungLoeschen))
	mux.Handle("POST /api/v1/admin/sammlungen/{id}/titel", admin(s.adminSammlungTitelHinzu))
	mux.Handle("DELETE /api/v1/admin/sammlungen/{id}/titel/{item}", admin(s.adminSammlungTitelWeg))
	mux.Handle("POST /api/v1/admin/sammlungen/{id}/reihenfolge", admin(s.adminSammlungOrdnen))
	mux.Handle("POST /api/v1/admin/sammlungen/{id}/bild", admin(s.adminSammlungBild))
	mux.Handle("DELETE /api/v1/admin/sammlungen/{id}/bild", admin(s.adminSammlungBildWeg))

	// Übernahme aus Emby/Jellyfin (uebernahme.go): jedes Profil nur für
	// sich selbst. Bewusst kein admin-Weg — die Verwaltung sieht davon
	// nichts.
	mux.Handle("POST /api/v1/me/uebernahme/server", auth(s.uebernahmeServer))
	mux.Handle("POST /api/v1/me/uebernahme", auth(s.uebernahmeStarten))
	mux.Handle("GET /api/v1/me/uebernahme", auth(s.uebernahmeStand))
	mux.Handle("POST /api/v1/me/uebernahme/anwenden", auth(s.uebernahmeAnwenden))
	mux.Handle("DELETE /api/v1/me/uebernahme", auth(s.uebernahmeVerwerfen))

	// Untertitel von OpenSubtitles (untertitel_suche.go).
	mux.Handle("GET /api/v1/untertitel/verfuegbar", auth(s.untertitelVerfuegbar))
	mux.Handle("GET /api/v1/items/{id}/untertitel/suche", auth(s.untertitelSuchen))
	mux.Handle("POST /api/v1/items/{id}/untertitel", auth(s.untertitelHolen))
	// Die Einstellungen für OpenSubtitles (Schlüssel, Konto, Tageslimit)
	// ändert nur der Besitzer: Ein Mitverwalter könnte sonst das Konto
	// des Besitzers gegen ein eigenes tauschen. Lesen darf jede
	// Verwaltung — ohne den Benutzernamen (adminUntertitel).
	mux.Handle("GET /api/v1/admin/untertitel", admin(s.adminUntertitel))
	mux.Handle("PUT /api/v1/admin/untertitel", s.requireOwner(s.adminUntertitelSetzen))
	mux.Handle("POST /api/v1/admin/untertitel", s.requireOwner(s.adminUntertitelSetzen))
}
