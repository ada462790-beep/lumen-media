// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

// Aufräumen nach Durchläufen: defekte Dateien, fehlende Dateien endgültig
// austragen.

import (
	"context"
	"fmt"
	"net/http"

	"lumen/internal/scan"
	"lumen/internal/store"
)

// defektText: die Meldung, wenn eine defekte Datei abgespielt werden soll
// — statt endlos „Wird umgewandelt" und HTTP 500.
const defektText = "die datei ist defekt und lässt sich nicht abspielen"

func init() { fehlerCodes[defektText] = "datei_defekt" }

// defektAbweisen antwortet mit 422 und dem Code datei_defekt, wenn die
// Datei als defekt bekannt ist.
func defektAbweisen(w http.ResponseWriter, f *store.MediaFile) bool {
	if f == nil || f.Defekt == "" {
		return false
	}
	fail(w, http.StatusUnprocessableEntity, defektText)
	return true
}

// defektGruppen: die defekten Dateien je Bibliothek und Grund, im Format
// der „ausgelassenen" Dateien unter Aufräumen (Grund „defekt_leer",
// „defekt_gesperrt", „defekt_kaputt").
func (s *Server) defektGruppen(ctx context.Context, darf map[string]bool) map[string][]scan.Auslassung {
	out := map[string][]scan.Auslassung{}
	liste, err := s.st.DefekteDateien(ctx, 1000)
	if err != nil {
		return out
	}
	je := map[string]map[string]*scan.Auslassung{}
	for _, d := range liste {
		if darf != nil && !darf[d.LibraryID] {
			continue
		}
		if je[d.LibraryID] == nil {
			je[d.LibraryID] = map[string]*scan.Auslassung{}
		}
		g := "defekt_" + d.Grund
		a := je[d.LibraryID][g]
		if a == nil {
			a = &scan.Auslassung{Grund: g, Pfade: []string{}}
			je[d.LibraryID][g] = a
		}
		a.Anzahl++
		if len(a.Pfade) < 200 {
			a.Pfade = append(a.Pfade, d.Path)
		}
	}
	for lib, m := range je {
		for _, g := range []string{"defekt_" + scan.DefektLeer, "defekt_" + scan.DefektGesperrt, "defekt_" + scan.DefektKaputt} {
			if a := m[g]; a != nil {
				out[lib] = append(out[lib], *a)
			}
		}
	}
	return out
}

// adminFehlendeEntfernen trägt fehlende Dateien endgültig aus: die
// genannten (fileIds) oder alle, die länger als einen Tag fehlen. Titel,
// an denen danach nichts mehr hängt, verschwinden mit allem, was daran
// hing. Die Dateien auf der Platte rührt Lumen nicht an.
func (s *Server) adminFehlendeEntfernen(w http.ResponseWriter, r *http.Request) {
	var in struct {
		FileIDs []string `json:"fileIds"`
	}
	if r.ContentLength != 0 && !readJSON(w, r, &in) {
		return
	}
	// Ein beschränkter Mitverwalter nur in seinen Mediatheken.
	if darf := s.verwalteteMediatheken(r); darf != nil {
		if len(in.FileIDs) == 0 {
			fail(w, http.StatusForbidden, "wähle eine bibliothek, die du verwaltest")
			return
		}
		for _, id := range in.FileIDs {
			f, err := s.st.File(r.Context(), id)
			if err != nil {
				failErr(w, err)
				return
			}
			it, err := s.st.Item(r.Context(), f.ItemID)
			if err != nil || !darf[it.LibraryID] {
				fail(w, http.StatusForbidden, "wähle eine bibliothek, die du verwaltest")
				return
			}
		}
	}
	var n int
	var err error
	// Unter der Arbeitssperre: Ein Durchlauf legt Titel an, bevor ihre
	// Dateien dazukommen — dazwischen sähe das Aufräumen sie als leer.
	s.Nacheinander(r.Context(), func(ctx context.Context) {
		n, err = s.st.FehlendeEntfernen(ctx, in.FileIDs)
		if err == nil {
			err = s.st.VerfuegbarkeitNachfuehren(ctx, "")
		}
	})
	if err != nil {
		failErr(w, err)
		return
	}
	if n > 0 {
		s.st.Log(r.Context(), "info", "aufraeumen", fmt.Sprintf("%d fehlende Dateien endgültig entfernt", n))
	}
	ok(w, map[string]int{"entfernt": n})
}
