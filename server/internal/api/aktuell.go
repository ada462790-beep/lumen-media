// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"errors"
	"net/http"
	"strings"

	"lumen/internal/meta"
	"lumen/internal/store"
)

// Was der Besitzer braucht, um die Mediathek aktuell zu halten: das
// Intervall je Bibliothek, Metadaten neu holen — für alles, für eine
// Bibliothek, für einen Titel —, und einem Titel von Hand den richtigen
// Eintrag der Quelle zuweisen, wenn Lumen danebenlag.

func (s *Server) adminLibrarySetzen(w http.ResponseWriter, r *http.Request) {
	if !s.mediathekPruefen(w, r, r.PathValue("id")) {
		return
	}
	var in struct {
		IntervalMin *int   `json:"intervalMin"`
		ScanAt      string `json:"scanAt"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if in.IntervalMin != nil {
		if err := s.st.SetScanInterval(r.Context(), r.PathValue("id"), *in.IntervalMin, strings.TrimSpace(in.ScanAt)); err != nil {
			failErr(w, err)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// adminMetadatenNeu stellt die Titel einer Bibliothek (oder aller) auf
// „ausstehend" und holt sie neu. Von Hand zugewiesene bleiben.
func (s *Server) adminMetadatenNeu(w http.ResponseWriter, r *http.Request) {
	if s.meta == nil || !s.meta.Ready() {
		s.ohneSchluessel(w, r)
		return
	}
	lib := r.URL.Query().Get("library")
	if !s.mediathekPruefen(w, r, lib) {
		return
	}
	n, err := s.st.MetadatenZuruecksetzen(r.Context(), lib, "")
	if err != nil {
		failErr(w, err)
		return
	}
	// In Häppchen zu 50 (metadatenNachholen): Dazwischen kommt ein
	// „Prüfen" dran. Vorher hielt das Neuladen von ein paar tausend Titeln
	// die Sperre eine Stunde lang, und jeder Scan wartete still.
	go s.metadatenNachholen(s.hintergrund(), lib, 0)
	ok(w, map[string]any{"status": "läuft", "titel": n})
}

// adminTitelNeu holt die Metadaten eines Titels noch einmal.
func (s *Server) adminTitelNeu(w http.ResponseWriter, r *http.Request) {
	if s.meta == nil || !s.meta.Ready() {
		s.ohneSchluessel(w, r)
		return
	}
	it, err := s.visibleItem(r, r.PathValue("id")) // nur in Mediatheken, die man verwalten darf
	if err != nil {
		failErr(w, err)
		return
	}
	if err := s.meta.NeuLaden(r.Context(), it); err != nil {
		failErr(w, err)
		return
	}
	// Eine lange Serie: der Rest der Folgen im Hintergrund.
	s.metadatenImHintergrund(it.LibraryID)
	s.ReihenBilden(r.Context())
	w.WriteHeader(http.StatusNoContent)
}

// adminTitelSuchen nennt, was die Quelle zu einem Suchwort findet.
func (s *Server) adminTitelSuchen(w http.ResponseWriter, r *http.Request) {
	if s.meta == nil || !s.meta.Ready() {
		s.ohneSchluessel(w, r)
		return
	}
	it, err := s.visibleItem(r, r.PathValue("id")) // nur in Mediatheken, die man verwalten darf
	if err != nil {
		failErr(w, err)
		return
	}
	suche := suchEingabe(r)
	if suche == "" {
		suche = it.Title
	}
	jahr := qInt(r, "year", 0)
	cands, err := s.meta.Kandidaten(r.Context(), it.Kind, suche, jahr)
	if err != nil {
		if !kennungFehler(w, err) {
			failErr(w, err)
		}
		return
	}
	type treffer struct {
		SourceID string `json:"sourceId"`
		Title    string `json:"title"`
		Original string `json:"original,omitempty"`
		Year     int    `json:"year,omitempty"`
		Overview string `json:"overview,omitempty"`
		Poster   string `json:"poster,omitempty"`
	}
	out := []treffer{}
	for _, c := range cands {
		out = append(out, treffer{c.SourceID, c.Title, c.Original, c.Year, c.Overview, c.PosterURL})
	}
	ok(w, out)
}

// adminTitelZuweisen hängt einen Titel an den gewählten Eintrag.
func (s *Server) adminTitelZuweisen(w http.ResponseWriter, r *http.Request) {
	if s.meta == nil || !s.meta.Ready() {
		s.ohneSchluessel(w, r)
		return
	}
	var in struct {
		SourceID string `json:"sourceId"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if strings.TrimSpace(in.SourceID) == "" {
		fail(w, http.StatusBadRequest, "welcher eintrag? die kennung fehlt")
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
	if err := s.meta.Zuweisen(r.Context(), it, in.SourceID); err != nil {
		failErr(w, err)
		return
	}
	// Von Hand zugewiesen: Die Dateien dieses Films gehören zusammen, auch
	// wenn sie verschieden heissen — der Scanner trennt sie nicht wieder.
	if it.Kind == "movie" {
		_ = s.st.ZuordnungSperren(r.Context(), it.ID)
	}
	s.metadatenImHintergrund(it.LibraryID)
	s.ReihenBilden(r.Context())
	neu, _ := s.st.Item(r.Context(), it.ID)
	if neu == nil {
		neu = &store.Item{}
	}
	ok(w, neu)
}

// kennungFehler beantwortet die Fehler beim Zuweisen über eine Kennung
// oder einen Link mit einem Satz, der sagt, was zu tun ist. false: kein
// solcher Fehler.
func kennungFehler(w http.ResponseWriter, err error) bool {
	switch {
	case errors.Is(err, meta.ErrTVDbOhneNummer):
		failCode(w, http.StatusBadRequest, "tvdb_ohne_nummer",
			"Dieser TheTVDB-Link nennt keine Nummer. Auf der Seite der Serie bei TheTVDB steht sie unter „TheTVDB.com Series ID“ – diese Zahl hier einfügen (oder tvdb:Nummer).")
	case errors.Is(err, meta.ErrFilmLinkBeiSerie):
		failCode(w, http.StatusBadRequest, "falsche_art", "Das ist ein Film-Link – hier wird eine Serie gesucht.")
	case errors.Is(err, meta.ErrSerienLinkBeiFilm):
		failCode(w, http.StatusBadRequest, "falsche_art", "Das ist ein Serien-Link – hier wird ein Film gesucht.")
	case errors.Is(err, meta.ErrTMDbSchluessel):
		failCode(w, http.StatusPreconditionFailed, "schluessel_abgelehnt",
			"TMDb lehnt den hinterlegten Schlüssel ab. Unter Verwaltung › Poster und Beschreibungen einen gültigen eintragen.")
	case errors.Is(err, meta.ErrKennungUnbekannt):
		failCode(w, http.StatusNotFound, "kennung_unbekannt",
			"Zu dieser Kennung kennt TMDb keinen passenden Eintrag. Stimmt die Nummer, und ist es die richtige Art (Film oder Serie)?")
	default:
		return false
	}
	return true
}
