// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

// Standardtitel von Staffeln und Folgen.
//
// Hat eine Staffel oder Folge keinen eigenen Namen, schreibt der Scanner
// „Staffel 2" bzw. „Folge 3" in die Datenbank (und TMDb liefert je nach
// Metadaten-Sprache „Season 2" oder „Episode 3"). Das ist kein Titel,
// sondern eine Nummer in Worten — und die gehört in die Sprache dessen,
// der sie liest. Beim Ausliefern erkennt Lumen genau diese Muster und
// bildet den Text in der Sprache des Profils neu. Ein echter Name
// („Pilot") bleibt, wie er ist. So brauchen bestehende Datenbanken keinen
// Umbau, und die Apps zeigen einfach, was der Server schickt.

import (
	"net/http"
	"regexp"

	"lumen/internal/sprache"
	"lumen/internal/store"
)

var (
	reStandardStaffel = regexp.MustCompile(`^(?i:staffel|season)\s+(\d{1,4})$`)
	reStandardFolge   = regexp.MustCompile(`^(?i:folgen?|episodes?)\s+(\d{1,5})(?:\s*[–-]\s*(\d{1,5}))?$`)
)

// trSprache übersetzt in eine ausdrücklich genannte Sprache (statt in
// die der Antwort) — für Texte, die schon vor dem Schreiben der Antwort
// feststehen müssen.
func trSprache(id, schluessel string, werte ...any) string {
	return sprache.T(id, schluessel, werte...)
}

// standardTitel gibt den Titel in der Sprache id zurück: einen
// Standardtitel neu gebildet, jeden anderen unverändert.
func standardTitel(id string, it *store.Item) string {
	switch it.Kind {
	case "season":
		if m := reStandardStaffel.FindStringSubmatch(it.Title); m != nil {
			return trSprache(id, "Staffel {n}", "n", m[1])
		}
	case "episode":
		if m := reStandardFolge.FindStringSubmatch(it.Title); m != nil {
			if m[2] != "" {
				return trSprache(id, "Folgen {von}–{bis}", "von", m[1], "bis", m[2])
			}
			return trSprache(id, "Folge {n}", "n", m[1])
		}
	}
	return it.Title
}

// standardTitelSetzen ersetzt in items die Standardtitel für das Profil
// der Anfrage.
func (s *Server) standardTitelSetzen(r *http.Request, items []store.Item) {
	id := s.wirksameSprache(r.Context(), accountOf(r).ID)
	for i := range items {
		items[i].Title = standardTitel(id, &items[i])
	}
}
