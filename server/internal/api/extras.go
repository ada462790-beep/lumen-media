// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

// Extras auf der Titelseite: Trailer, Featurettes, Interviews aus den
// Ordnern neben dem Film (scan.ExtrasZu). Abgespielt wird die Datei, wie
// sie ist — Extras sind kurz, meist MP4 oder MKV mit H.264, und laufen im
// Browser direkt. Die Nummer in der Adresse ist ein Abdruck des Pfads:
// Sie verrät den Pfad nicht, und eine Anfrage kann nur Dateien aus der
// Liste dieses Films erreichen, nie eine beliebige.

import (
	"crypto/sha1"
	"encoding/hex"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"lumen/internal/scan"
	"lumen/internal/store"
)

type extraAntwort struct {
	ID    string `json:"id"`
	Titel string `json:"titel,omitempty"`
	Art   string `json:"art"`
	URL   string `json:"url"`
}

func extraKennung(pfad string) string {
	sum := sha1.Sum([]byte(pfad))
	return hex.EncodeToString(sum[:8])
}

// extrasVon sucht die Extras eines Films (nur Filme).
func (s *Server) extrasVon(r *http.Request, it *store.Item) []scan.Extra {
	if it.Kind != "movie" {
		return nil
	}
	files, err := s.st.FilesFor(r.Context(), it.ID)
	if err != nil || len(files) == 0 {
		return nil
	}
	wurzel := s.st.BibliothekPfad(r.Context(), it.LibraryID)
	if wurzel == "" {
		return nil
	}
	pfade := make([]string, 0, len(files))
	for _, f := range files {
		pfade = append(pfade, f.Path)
	}
	return scan.ExtrasZu(pfade, wurzel)
}

// extrasZusatz legt die Extras unter „extras" in die Antwort der Titelseite.
func (s *Server) extrasZusatz(r *http.Request, it *store.Item, body map[string]any) {
	liste := s.extrasVon(r, it)
	if len(liste) == 0 {
		return
	}
	out := make([]extraAntwort, 0, len(liste))
	for _, e := range liste {
		id := extraKennung(e.Pfad)
		out = append(out, extraAntwort{ID: id, Titel: e.Titel, Art: e.Art,
			URL: "/api/v1/stream/" + it.ID + "/extra/" + id})
	}
	body["extras"] = out
}

// extraAbspielen: GET /api/v1/stream/{item}/extra/{nr} — die Datei selbst,
// mit Range wie jede Videodatei.
func (s *Server) extraAbspielen(w http.ResponseWriter, r *http.Request) {
	it, err := s.visibleItem(r, r.PathValue("item"))
	if err != nil {
		failErr(w, err)
		return
	}
	nr := r.PathValue("nr")
	for _, e := range s.extrasVon(r, it) {
		if extraKennung(e.Pfad) != nr {
			continue
		}
		info, err := os.Lstat(e.Pfad)
		if err != nil || !info.Mode().IsRegular() {
			break
		}
		f, err := os.Open(e.Pfad)
		if err != nil {
			break
		}
		defer f.Close()
		typ := map[string]string{".mp4": "video/mp4", ".m4v": "video/mp4", ".mkv": "video/x-matroska",
			".webm": "video/webm", ".mov": "video/quicktime"}[strings.ToLower(filepath.Ext(e.Pfad))]
		if typ == "" {
			typ = "application/octet-stream"
		}
		w.Header().Set("Content-Type", typ)
		w.Header().Set("Cache-Control", "private, max-age=3600")
		http.ServeContent(w, r, "", info.ModTime(), f)
		return
	}
	fail(w, http.StatusNotFound, "Dieses Extra gibt es nicht.")
}
