// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

// Was eine NFO-Datei über einen Titel weiss und was TMDb nicht liefert:
// Tagline, Regie, Drehbuch, Studio, Schlagworte, Länder. Der Scanner hat
// die NFO schon gelesen und als JSON gemerkt (lokal_nfo); hier kommt sie
// nur auf die Titelseite.

import (
	"context"
	"encoding/json"

	"lumen/internal/lokal"
	"lumen/internal/store"
)

type nfoAngaben struct {
	Tagline  string   `json:"tagline,omitempty"`
	Regie    []string `json:"regie,omitempty"`
	Drehbuch []string `json:"drehbuch,omitempty"`
	Studios  []string `json:"studios,omitempty"`
	Tags     []string `json:"tags,omitempty"`
	Laender  []string `json:"laender,omitempty"`
}

// nfoZusatz legt die Angaben aus der NFO unter „nfo" in die Antwort.
func (s *Server) nfoZusatz(ctx context.Context, it *store.Item, body map[string]any) {
	roh, ok := s.st.LokaleNFOZu(ctx, it.ID)
	if !ok {
		return
	}
	var n lokal.NFO
	if json.Unmarshal([]byte(roh.Daten), &n) != nil {
		return
	}
	// Stammt die Note aus einer NFO ohne Quelle, ist sie keine IMDb-Note —
	// die App zeigt sie dann neutral („★ 7,4" statt „IMDb ★ 7,4"). Hat
	// OMDb sie inzwischen ersetzt, ist sie es wieder.
	if n.BewertungQuelle == "andere" && n.Bewertung > 0 && it.IMDbRating == n.Bewertung {
		body["bewertungQuelle"] = "nfo"
	}
	a := nfoAngaben{Tagline: n.Tagline, Regie: n.Regie, Drehbuch: n.Drehbuch,
		Studios: n.Studios, Tags: n.Tags, Laender: n.Laender}
	if a.Tagline == "" && len(a.Regie)+len(a.Drehbuch)+len(a.Studios)+len(a.Tags)+len(a.Laender) == 0 {
		return
	}
	body["nfo"] = a
}
