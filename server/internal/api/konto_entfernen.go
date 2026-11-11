// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"net/http"
)

// adminKontoEntfernen: DELETE /api/v1/admin/accounts/{id} — eine Person
// aus dem Haushalt nehmen (der Gast von letztem Sommer, das alte Profil
// fürs Tablet). Der Speicher konnte das längst (store.DeleteAccount), die
// Schnittstelle nicht: Wer einmal eingeladen war, blieb für immer in der
// Profilauswahl.
//
// Nur mit Besitzerrechten und bestätigt. Nie der Besitzer, nie das eigene
// Konto (das wäre ein Aussperren aus Versehen). Verlauf, Merkliste,
// Bewertungen, Listen und Geräte des Kontos gehen mit (ON DELETE CASCADE);
// was die Person in fremde geteilte Listen gelegt hat, bleibt dort.
func (s *Server) adminKontoEntfernen(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == accountOf(r).ID {
		fail(w, http.StatusConflict, "dein eigenes Konto entfernst du hier nicht")
		return
	}
	if err := s.mitverwalterDarf(r, id, nil); err != nil {
		fail(w, http.StatusForbidden, err.Error())
		return
	}
	ziel, err := s.st.Account(r.Context(), id)
	if err != nil {
		fail(w, http.StatusNotFound, "konto nicht gefunden")
		return
	}
	if ziel.IsOwner {
		fail(w, http.StatusForbidden, "das konto des besitzers ändert nur er selbst")
		return
	}
	if err := s.st.DeleteAccount(r.Context(), id); err != nil {
		failErr(w, err)
		return
	}
	// Ohne Namen: Das Protokoll sagt, dass etwas geschah, nicht wem.
	s.st.Log(r.Context(), "info", "verwaltung", "Ein Konto wurde entfernt")
	w.WriteHeader(http.StatusNoContent)
}
