// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

// „ID eingeben" ohne TMDb-Schlüssel. Wer von Emby kommt, hat oft keinen
// Schlüssel — aber eine IMDb- oder TMDb-Kennung kennt er. Die speichert
// Lumen auch ohne TMDb: Die Links auf der Titelseite und der Abgleich beim
// Umzug gehen sofort, Poster und Beschreibungen kommen, sobald ein
// gültiger Schlüssel eingetragen ist (meta.KennungenVormerken).

import (
	"net/http"

	"lumen/internal/lokal"
	"lumen/internal/meta"
)

// adminKennungVormerken: POST /admin/items/{id}/metadata/kennung
// {"eingabe": "tt0133093"}.
func (s *Server) adminKennungVormerken(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Eingabe string `json:"eingabe"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	it, err := s.visibleItem(r, r.PathValue("id")) // nur in Mediatheken, die man verwalten darf
	if err != nil {
		failErr(w, err)
		return
	}
	if it.Kind != "movie" && it.Kind != "series" {
		fail(w, http.StatusBadRequest, "zuweisen geht für filme und serien")
		return
	}
	e, istKennung := meta.KennungLesen(in.Eingabe)
	if !istKennung {
		failCode(w, http.StatusBadRequest, "keine_kennung",
			"Das sieht nicht nach einer Kennung aus. Möglich sind etwa tt0133093, eine TMDb-Nummer, tvdb:121361 oder ein Link.")
		return
	}
	var k lokal.Kennungen
	switch e.Quelle {
	case "tmdb":
		art := "movie"
		if it.Kind == "series" {
			art = "tv"
		}
		if e.Art != "" && e.Art != art {
			if art == "tv" {
				kennungFehler(w, meta.ErrFilmLinkBeiSerie)
			} else {
				kennungFehler(w, meta.ErrSerienLinkBeiFilm)
			}
			return
		}
		k.TMDb = e.ID
	case "imdb":
		k.IMDb = e.ID
	case "tvdb":
		if e.ID == "" {
			kennungFehler(w, meta.ErrTVDbOhneNummer)
			return
		}
		k.TVDb = e.ID
	}
	if err := meta.KennungenVormerken(r.Context(), s.st, it.ID, k); err != nil {
		failErr(w, err)
		return
	}
	neu, err := s.st.Item(r.Context(), it.ID)
	if err != nil {
		failErr(w, err)
		return
	}
	ok(w, map[string]any{"item": neu, "schluessel": meta.SchluesselStand(r.Context(), s.st)})
}

// ohneSchluessel beantwortet eine Anfrage, die TMDb braucht, wenn kein
// (gültiger) Schlüssel da ist — mit einem Satz, der sagt, was zu tun ist.
func (s *Server) ohneSchluessel(w http.ResponseWriter, r *http.Request) {
	if meta.SchluesselStand(r.Context(), s.st) == "abgelehnt" {
		failCode(w, http.StatusPreconditionFailed, "schluessel_abgelehnt",
			"TMDb lehnt den hinterlegten Schlüssel ab. Unter Verwaltung › Poster und Beschreibungen einen gültigen eintragen.")
		return
	}
	failCode(w, http.StatusPreconditionFailed, "ohne_schluessel",
		"Dafür braucht es einen TMDb-Schlüssel (Verwaltung › Poster und Beschreibungen). Eine Kennung lässt sich auch ohne ihn speichern.")
}
