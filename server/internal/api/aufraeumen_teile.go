// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

// Aufräumen: eine einzelne Datei anspielen, Teile von Hand zusammenfügen
// oder trennen, und zu fehlenden Dateien sagen, worum es geht.
//
//	GET  /api/v1/stream/{fileId}/anspielen.mp4      genau diese Datei (Verwalter)
//	POST /api/v1/admin/items/{id}/als-teile         {"fileIds":[…]} ein Film in Teilen
//	POST /api/v1/admin/items/{id}/teile-trennen     „Sind verschiedene Filme"

import (
	"context"
	"net/http"
	"os"

	"lumen/internal/scan"
	"lumen/internal/store"
)

// dateiAnspielen: GET /api/v1/stream/{fileId}/anspielen.mp4 — genau diese
// Datei im Browser, wie ein Extra: ohne Stand, ohne „Weiterschauen", ohne
// die anderen Teile. Für Verwalter unter Aufräumen („was ist da drin?") —
// auch eine Datei, die als defekt gilt („Trotzdem anspielen"): Gemessen
// wird hier noch einmal, gründlich. Unter /stream/, weil ein <video> den
// Medienschlüssel nur in der Adresse mitschicken kann.
func (s *Server) dateiAnspielen(w http.ResponseWriter, r *http.Request) {
	acc := accountOf(r)
	if acc == nil || !acc.IsAdmin {
		fail(w, http.StatusNotFound, "nicht gefunden")
		return
	}
	f, err := s.st.File(r.Context(), r.PathValue("fileId"))
	if err != nil {
		failErr(w, err)
		return
	}
	if _, err := s.visibleItem(r, f.ItemID); err != nil {
		failErr(w, err)
		return
	}
	if info, err := os.Stat(f.Path); f.Missing || err != nil || !info.Mode().IsRegular() {
		fail(w, http.StatusGone, "die datei ist nicht mehr da")
		return
	}
	s.pfadStrom(w, r, f.ItemID, "anspielen:"+f.ID, f.Path)
}

// adminAlsTeile: POST /api/v1/admin/items/{id}/als-teile {"fileIds":[…]}
// — die Dateien sind Teile eines Films (Reihenfolge nach „Teil N" im
// Namen, sonst nach dem Pfad). Dateien anderer Einträge kommen an diesen;
// ein Eintrag ohne Datei geht samt Stand und Merkliste darin auf. Der
// Scan trennt sie danach nicht wieder (store.FeldTeile).
func (s *Server) adminAlsTeile(w http.ResponseWriter, r *http.Request) {
	var in struct {
		FileIDs []string `json:"fileIds"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	ziel := r.PathValue("id")
	if _, err := s.visibleItem(r, ziel); err != nil {
		failErr(w, err)
		return
	}
	if len(in.FileIDs) < 2 {
		fail(w, http.StatusBadRequest, "es braucht mindestens zwei dateien")
		return
	}
	pfade := make([]string, len(in.FileIDs))
	jahre := make([]int, len(in.FileIDs)) // je Datei: Jahr ihres Eintrags, sonst aus dem Namen
	for i, id := range in.FileIDs {
		f, err := s.st.File(r.Context(), id)
		if err != nil {
			failErr(w, err)
			return
		}
		it, err := s.visibleItem(r, f.ItemID)
		if err != nil {
			failErr(w, err)
			return
		}
		pfade[i] = f.Path
		if jahre[i] = it.Year; jahre[i] == 0 {
			jahre[i] = scan.Parse(f.Path, nil).Year
		}
	}
	reihe := make([]string, 0, len(in.FileIDs))
	for _, i := range scan.TeileReihenfolge(pfade) {
		reihe = append(reihe, in.FileIDs[i])
	}
	var err error
	// Unter der Arbeitssperre: Ein Durchlauf schreibt dieselben Dateien.
	s.Nacheinander(r.Context(), func(ctx context.Context) {
		err = s.st.TeileFestlegen(ctx, ziel, reihe)
		if err == nil {
			err = s.st.VerfuegbarkeitNachfuehren(ctx, "")
		}
		// Ein nie erkannter Eintrag hiess nach seiner ersten Datei („Der
		// Zweiteiler: Teil 1") — jetzt nach dem Film.
		if it, e := s.st.Item(ctx, ziel); err == nil && e == nil && !store.IstErkannt(it.MatchState) {
			if t := scan.TeilBasis(pfade[scan.TeileReihenfolge(pfade)[0]]); t != "" && t != it.Title {
				it.Title = t
				_ = s.st.UpsertItem(ctx, it)
			}
		}
		// Hat der Film kein Jahr (Teil 1 ohne Jahr im Namen), gilt das
		// eines anderen Teils — in der Reihenfolge der Teile.
		if it, e := s.st.Item(ctx, ziel); err == nil && e == nil && it.Year == 0 {
			for _, i := range scan.TeileReihenfolge(pfade) {
				if jahre[i] > 0 {
					it.Year = jahre[i]
					_ = s.st.UpsertItem(ctx, it)
					break
				}
			}
		}
	})
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	s.st.Log(r.Context(), "info", "aufraeumen", "Dateien von Hand als Teile eines Films zusammengefügt")
	w.WriteHeader(http.StatusNoContent)
}

// adminTeileTrennen: POST /api/v1/admin/items/{id}/teile-trennen — „Sind
// verschiedene Filme": Teil 1 bleibt, jeder weitere Teil wird ein eigener
// Film mit dem Titel aus seinem Dateinamen. Der Scan legt sie danach nicht
// wieder zusammen (store.FeldEinzeln).
func (s *Server) adminTeileTrennen(w http.ResponseWriter, r *http.Request) {
	it, err := s.visibleItem(r, r.PathValue("id"))
	if err != nil {
		failErr(w, err)
		return
	}
	var neu []*store.Item
	s.Nacheinander(r.Context(), func(ctx context.Context) {
		neu, err = s.st.TeileTrennen(ctx, it.ID, func(f store.MediaFile) (string, int) {
			g := scan.Parse(f.Path, s.ordnerUeber(ctx, &f))
			return g.Title, g.Year
		})
	})
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	s.st.Log(r.Context(), "info", "aufraeumen", "Teile eines Films von Hand als verschiedene Filme getrennt")
	ok(w, map[string]int{"neu": len(neu)})
}

// teileKandidat: Teile eines Films, die als getrennte Filme dastehen
// (scan.TeileKandidaten) — unter Aufräumen mit „Als Teile zusammenfügen".
type teileKandidat struct {
	ItemID string            `json:"itemId"` // der Eintrag des ersten Teils
	Title  string            `json:"title"`
	Year   int               `json:"year,omitempty"`
	Files  []store.FilmDatei `json:"files"`
}

func (s *Server) teileKandidaten(ctx context.Context, darf map[string]bool) []teileKandidat {
	out := []teileKandidat{}
	if s.scanner == nil {
		return out
	}
	alle, err := s.st.FilmDateienVorhanden(ctx)
	if err != nil {
		return out
	}
	for _, g := range scan.TeileKandidaten(alle) {
		if darf != nil && !darf[g[0].LibraryID] {
			continue
		}
		// Wer „Sind verschiedene Filme" gesagt hat, bekommt sie nicht
		// wieder als Kandidaten vorgesetzt.
		einzeln := false
		for _, d := range g {
			einzeln = einzeln || s.st.SperreGesetzt(ctx, d.ItemID, store.FeldEinzeln)
		}
		if einzeln {
			continue
		}
		out = append(out, teileKandidat{ItemID: g[0].ItemID, Title: g[0].Titel, Year: g[0].Jahr, Files: g})
		if len(out) >= 100 {
			break
		}
	}
	return out
}

// fehlendZeile: eine fehlende Datei samt Titel und dem Hinweis, ob der
// Titel mit einer anderen Datei weiterhin da ist.
type fehlendZeile struct {
	store.MediaFile
	Angabe *store.FehlendAngabe `json:"angabe,omitempty"`
}

func (s *Server) fehlendeMitAngaben(ctx context.Context, missing []store.MediaFile) []fehlendZeile {
	ids := make([]string, len(missing))
	for i, f := range missing {
		ids[i] = f.ID
	}
	angaben, _ := s.st.FehlendeAngaben(ctx, ids)
	out := make([]fehlendZeile, len(missing))
	for i, f := range missing {
		out[i] = fehlendZeile{MediaFile: f}
		if a, ok := angaben[f.ID]; ok {
			out[i].Angabe = &a
		}
	}
	return out
}
