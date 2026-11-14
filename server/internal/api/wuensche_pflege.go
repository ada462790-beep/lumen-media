// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

// Wünsche pflegen (store/wunsch_doppelt.go).
//
//	DELETE /api/v1/wishes/{id}                       eigenen offenen löschen (angeschlossen: austreten)
//	DELETE /api/v1/admin/wishes/{id}                 die Verwaltung: jeden aus ihrem Bereich
//	POST   /api/v1/admin/wishes/{id}/zusammenfuehren {"ids":[…]} Doppel in diesen legen
//
// Dazu trägt Lumen alten Wünschen ohne Kennung TMDb- und IMDb-Kennung und
// Poster nach, wenn TMDb zu Titel (und Jahr) genau einen Treffer kennt —
// angestossen, wenn die Verwaltung die Wünsche öffnet, sparsam: höchstens
// alle zehn Minuten ein paar, jeder Wunsch höchstens einmal am Tag.

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"lumen/internal/meta"
	"lumen/internal/store"
)

// wunschLoeschen: Wer den Wunsch hat, löscht ihn, solange er offen ist;
// wer sich nur angeschlossen hat, tritt aus. Alles andere: nicht gefunden.
func (s *Server) wunschLoeschen(w http.ResponseWriter, r *http.Request) {
	acc := accountOf(r)
	id := r.PathValue("id")
	owner, state, dabei, err := s.st.WunschZustand(r.Context(), id, acc.ID)
	if err != nil {
		fail(w, http.StatusNotFound, "nicht gefunden")
		return
	}
	switch {
	case owner == acc.ID && state == "open":
		err = s.st.WunschLoeschen(r.Context(), id)
	case owner == acc.ID:
		failCode(w, http.StatusConflict, "nicht_offen",
			"Dieser Wunsch ist schon in Arbeit oder erledigt und lässt sich nicht mehr löschen.")
		return
	case dabei:
		err = s.st.WunschVerlassen(r.Context(), id, acc.ID)
	default:
		fail(w, http.StatusNotFound, "nicht gefunden")
		return
	}
	if err != nil {
		failErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// wunschImBereich: Mitverwaltung nur mit Wünschen aus dem eigenen Bereich
// (wie adminSetWishState).
func (s *Server) wunschImBereich(r *http.Request, id string) bool {
	acc := accountOf(r)
	von, err := s.st.WunschVon(r.Context(), id)
	if err != nil {
		return false
	}
	return acc.Besitzerrechte() || von == acc.ID || s.verwaltetKonto(r, von)
}

func (s *Server) adminWunschLoeschen(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !s.wunschImBereich(r, id) {
		fail(w, http.StatusNotFound, "nicht gefunden")
		return
	}
	if err := s.st.WunschLoeschen(r.Context(), id); err != nil {
		failErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) adminWuenscheZusammenfuehren(w http.ResponseWriter, r *http.Request) {
	var in struct {
		IDs []string `json:"ids"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	haupt := r.PathValue("id")
	if !s.wunschImBereich(r, haupt) || len(in.IDs) == 0 || len(in.IDs) > 100 {
		fail(w, http.StatusNotFound, "nicht gefunden")
		return
	}
	// Nur, was wirklich dasselbe meint und im Bereich liegt.
	gruppen, err := s.st.WunschGruppen(r.Context())
	if err != nil {
		failErr(w, err)
		return
	}
	var andere []string
	for _, id := range in.IDs {
		if id != haupt && gruppen[id] != "" && gruppen[id] == gruppen[haupt] && s.wunschImBereich(r, id) {
			andere = append(andere, id)
		}
	}
	n, err := s.st.WuenscheZusammenfuehren(r.Context(), haupt, andere)
	if err != nil {
		failErr(w, err)
		return
	}
	ok(w, map[string]int{"zusammengefuehrt": n})
}

// ---------------------------------------------------------------- Nachtragen

var wunschNachtrag struct {
	mu       sync.Mutex
	laeuft   bool
	zuletzt  time.Time
	versucht map[string]time.Time
}

const (
	wunschNachtragAbstand = 10 * time.Minute
	wunschNachtragJeLauf  = 15
)

// wuenscheNachtragenAnstossen: im Hintergrund, wenn es Zeit ist.
func (s *Server) wuenscheNachtragenAnstossen() {
	if s.meta == nil || !s.meta.Ready() {
		return
	}
	wunschNachtrag.mu.Lock()
	if wunschNachtrag.laeuft || time.Since(wunschNachtrag.zuletzt) < wunschNachtragAbstand {
		wunschNachtrag.mu.Unlock()
		return
	}
	wunschNachtrag.laeuft, wunschNachtrag.zuletzt = true, time.Now()
	wunschNachtrag.mu.Unlock()
	go func() {
		defer func() {
			wunschNachtrag.mu.Lock()
			wunschNachtrag.laeuft = false
			wunschNachtrag.mu.Unlock()
		}()
		ctx, fertig := context.WithTimeout(s.hintergrund(), 2*time.Minute)
		defer fertig()
		_, _ = s.wuenscheNachtragen(ctx)
	}()
}

// wunschEindeutig wählt aus den Treffern von TMDb den einen, der zum
// Wunsch passt: gleicher Titel oder Originaltitel (ohne Satzzeichen), und
// das Jahr, wenn der Wunsch eines hat. Mehrere oder keiner: nil.
func wunschEindeutig(titel string, jahr int, treffer []meta.Vorschlag) *meta.Vorschlag {
	n := store.WunschNorm(titel)
	if n == "" {
		return nil
	}
	var passend []meta.Vorschlag
	for _, v := range treffer {
		if store.WunschNorm(v.Titel) != n && store.WunschNorm(v.Originaltitel) != n {
			continue
		}
		if jahr > 0 && v.Jahr != jahr {
			continue
		}
		passend = append(passend, v)
	}
	if len(passend) != 1 {
		return nil
	}
	return &passend[0]
}

// wuenscheNachtragen sucht für alte Wünsche ohne Kennung den Titel bei
// TMDb. Zurück kommt, wie viele eine Kennung bekommen haben.
func (s *Server) wuenscheNachtragen(ctx context.Context) (int, error) {
	if s.meta == nil || !s.meta.Ready() {
		return 0, nil
	}
	liste, err := s.st.WuenscheOhneKennung(ctx, 200)
	if err != nil {
		return 0, err
	}
	spr := s.meta.MetaSprache()
	n, gefragt := 0, 0
	for _, wu := range liste {
		if gefragt >= wunschNachtragJeLauf || ctx.Err() != nil {
			break
		}
		wunschNachtrag.mu.Lock()
		if wunschNachtrag.versucht == nil {
			wunschNachtrag.versucht = map[string]time.Time{}
		}
		zuletzt := wunschNachtrag.versucht[wu.ID]
		if time.Since(zuletzt) < 24*time.Hour {
			wunschNachtrag.mu.Unlock()
			continue
		}
		wunschNachtrag.versucht[wu.ID] = time.Now()
		wunschNachtrag.mu.Unlock()
		gefragt++

		titel := strings.TrimSpace(wu.Titel)
		schluessel := spr + "|" + strings.ToLower(titel)
		treffer, da := s.wuensche.sucheHolen(schluessel)
		if !da {
			tctx, fertig := context.WithTimeout(ctx, 8*time.Second)
			treffer, err = s.meta.WunschSuche(tctx, titel, spr)
			if err == nil && len(treffer) == 0 && !strings.HasPrefix(spr, "en") {
				treffer, err = s.meta.WunschSuche(tctx, titel, "en-US")
			}
			fertig()
			if err != nil {
				continue
			}
			s.wuensche.sucheMerken(schluessel, treffer)
		}
		v := wunschEindeutig(titel, wu.Jahr, treffer)
		if v == nil {
			continue
		}
		d := store.WunschDaten{Titel: v.Titel, Jahr: v.Jahr, Art: v.Art, TMDbID: v.TMDbID,
			Originaltitel: wennAnders(v.Originaltitel, v.Titel), PosterPfad: v.PosterPfad}
		tctx, fertig := context.WithTimeout(ctx, 8*time.Second)
		if genau, err := s.meta.WunschDetails(tctx, v.Art, v.TMDbID, spr); err == nil && genau != nil {
			d.IMDbID = genau.IMDbID
			if genau.PosterPfad != "" {
				d.PosterPfad = genau.PosterPfad
			}
		}
		fertig()
		if err := s.st.WunschKennungNachtragen(ctx, wu.ID, d); err != nil {
			return n, err
		}
		n++
		if d.PosterPfad != "" {
			bctx, fertig := context.WithTimeout(ctx, 20*time.Second)
			_, _, _ = s.wunschPosterLaden(bctx, "w185", strings.TrimPrefix(d.PosterPfad, "/"))
			fertig()
		}
	}
	if n > 0 {
		_, _ = s.st.WuenscheErfuellenFuer(ctx, s.wunschSicht)
	}
	return n, nil
}
