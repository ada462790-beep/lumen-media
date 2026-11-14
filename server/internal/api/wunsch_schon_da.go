// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

// Ein Wunsch nach etwas, das schon da ist: „Matrix" steht in der
// Bibliothek, und jemand wünscht es sich mit dem Namen. Statt eines
// Wunschs, der sich gleich wieder erfüllt (oder offen bleibt, weil der
// Name allein keine Kennung hat), antwortet der Server mit 409 und dem
// Code „schon_da" — samt Titel, damit die App „Schon da" mit einem
// Verweis zeigen kann. Ältere Apps zeigen den Satz als Fehler; auch das
// stimmt.

import (
	"net/http"
	"strconv"

	"lumen/internal/store"
)

func init() { fehlerCodes[schonDaText] = "schon_da" }

const schonDaText = "„{titel}“ steht schon in der Bibliothek."

// wunschSchonDa: true, wenn der gewünschte Titel schon in einer
// Bibliothek steht, die dieses Profil sieht — dann ist die Antwort
// geschrieben.
func (s *Server) wunschSchonDa(w http.ResponseWriter, r *http.Request, d store.WunschDaten) bool {
	ctx := r.Context()
	scope, err := s.scope(r)
	if err != nil {
		return false
	}
	id := ""
	if d.TMDbID != "" || d.IMDbID != "" {
		ids, err := s.st.TitelDa(ctx, scope, []store.TitelKandidat{{Art: d.Art, TMDbID: d.TMDbID, IMDbID: d.IMDbID,
			Titel: d.Titel, Originaltitel: d.Originaltitel, Jahr: d.Jahr}})
		if err == nil && len(ids) == 1 {
			id = ids[0]
		}
	} else if it, err := s.st.TitelMitNamen(ctx, scope, d.Titel, d.Jahr); err == nil && it != nil {
		id = it.ID
	}
	if id == "" {
		return false
	}
	it, err := s.st.Item(ctx, id)
	if err != nil || it == nil {
		return false
	}
	titel := standardTitel(s.wirksameSprache(ctx, accountOf(r).ID), it)
	name := titel
	if it.Year > 0 {
		name += " (" + strconv.Itoa(it.Year) + ")"
	}
	writeJSON(w, http.StatusConflict, map[string]any{
		"error":  satzAnfang(tr(w, schonDaText, "titel", name)),
		"code":   "schon_da",
		"itemId": it.ID,
		"titel":  titel,
		"jahr":   it.Year,
		"art":    it.Kind,
	})
	return true
}
