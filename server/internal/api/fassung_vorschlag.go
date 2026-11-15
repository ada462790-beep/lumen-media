// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

// Aufräumen › „Vielleicht Fassungen desselben Films" (store.FassungKandidaten)
// und „Als Fassungen zusammenlegen":
//
//	POST /api/v1/admin/cleanup/fassungen-zusammenlegen {"itemIds":["…","…"]}
//
// Der erste Eintrag (der erkannte, sonst der erste) nimmt die Dateien des
// anderen auf, samt Stand, Merkliste und Bewertung (FilmZusammenlegen),
// und bekommt die Sperre FeldZuordnung — der nächste Durchlauf trennt sie
// nicht wieder. Rückgängig: „Abtrennen" an der Datei.

import (
	"context"
	"net/http"

	"lumen/internal/store"
)

type fassungKandidat struct {
	Items []store.FassungSeite `json:"items"`
}

func (s *Server) fassungKandidaten(ctx context.Context, darf map[string]bool) []fassungKandidat {
	out := []fassungKandidat{}
	paare, err := s.st.FassungKandidaten(ctx, 100)
	if err != nil {
		return out
	}
	for _, p := range paare {
		if darf != nil && !darf[p[0].LibID] {
			continue
		}
		out = append(out, fassungKandidat{Items: []store.FassungSeite{p[0], p[1]}})
	}
	return out
}

func (s *Server) adminFassungenZusammenlegen(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ItemIDs []string `json:"itemIds"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if len(in.ItemIDs) != 2 || in.ItemIDs[0] == in.ItemIDs[1] {
		fail(w, http.StatusBadRequest, "es braucht genau zwei einträge")
		return
	}
	var items [2]*store.Item
	for i, id := range in.ItemIDs {
		it, err := s.visibleItem(r, id)
		if err != nil {
			failErr(w, err)
			return
		}
		if it.Kind != "movie" {
			fail(w, http.StatusBadRequest, "zusammenlegen geht nur bei filmen")
			return
		}
		items[i] = it
	}
	if items[0].LibraryID != items[1].LibraryID {
		fail(w, http.StatusBadRequest, "die einträge liegen in verschiedenen mediatheken")
		return
	}
	// Der erkannte Eintrag bleibt (seine Metadaten sind die besseren).
	ziel, quelle := items[0], items[1]
	if !store.IstErkannt(ziel.MatchState) && store.IstErkannt(quelle.MatchState) {
		ziel, quelle = quelle, ziel
	}
	var err error
	// Unter der Arbeitssperre: Ein Durchlauf schreibt dieselben Einträge.
	s.Nacheinander(r.Context(), func(ctx context.Context) {
		if err = s.st.FilmZusammenlegen(ctx, ziel.ID, quelle.ID); err == nil {
			err = s.st.ZuordnungSperren(ctx, ziel.ID)
		}
	})
	if err != nil {
		failErr(w, err)
		return
	}
	s.st.Log(r.Context(), "info", "aufraeumen", "Zwei Einträge von Hand als Fassungen eines Films zusammengelegt")
	ok(w, map[string]string{"itemId": ziel.ID})
}
