// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

// Musik: Alben, Interpreten, Titel, Genres, Suche, Hörstand, Herzen und
// Wiedergabelisten — und das Ausliefern der Lieder selbst.
//
// Nichts hier schreibt ins Protokoll, was jemand hört, und keine Antwort
// an die Verwaltung nennt es. Der Hörstand gehört dem Profil allein.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"lumen/internal/store"
)

type musikAlbumAntwort struct {
	store.MusikAlbum
	Cover   string `json:"cover,omitempty"`
	Favorit bool   `json:"favorit,omitempty"`
}

type musikTitelAntwort struct {
	store.MusikTitel
	Cover   string `json:"cover,omitempty"`
	Favorit bool   `json:"favorit,omitempty"`
	// Stream: die Adresse der Originaldatei. Kann das Gerät das Format
	// nicht (Mime leer oder canPlayType sagt nein), hängt es ?als=aac
	// (oder ?als=opus) an und bekommt eine umgewandelte Fassung.
	Stream string `json:"stream"`
	Mime   string `json:"mime,omitempty"`
}

type musikInterpretAntwort struct {
	store.MusikInterpret
	Cover   string `json:"cover,omitempty"`
	Favorit bool   `json:"favorit,omitempty"`
}

func coverAdresse(albumID string) string {
	if albumID == "" {
		return ""
	}
	return "/api/v1/medien/musik/cover/" + albumID
}

// MusikMime: der Typ, den ein Browser für canPlayType braucht. Leer heisst:
// so gut wie kein Browser kann das — gleich umwandeln.
func MusikMime(codec, container string) string {
	switch codec {
	case "mp3":
		return "audio/mpeg"
	case "aac":
		return `audio/mp4; codecs="mp4a.40.2"`
	case "alac":
		return `audio/mp4; codecs="alac"`
	case "flac":
		if container == "ogg" || container == "oga" {
			return `audio/ogg; codecs="flac"`
		}
		return "audio/flac"
	case "vorbis":
		return `audio/ogg; codecs="vorbis"`
	case "opus":
		if container == "webm" || container == "mka" {
			return `audio/webm; codecs="opus"`
		}
		return `audio/ogg; codecs="opus"`
	case "pcm_s16le", "pcm_s24le", "pcm_u8":
		if container == "wav" {
			return `audio/wav; codecs="1"`
		}
	}
	return ""
}

// musikFavoritenMenge: die Herzen des Profils als Menge je Art.
func (s *Server) musikFavoritenMenge(r *http.Request) map[string]map[string]bool {
	out := map[string]map[string]bool{"titel": {}, "album": {}, "interpret": {}}
	fav, err := s.st.MusikFavoriten(r.Context(), accountOf(r).ID)
	if err != nil {
		return out
	}
	for art, ids := range fav {
		for _, id := range ids {
			out[art][id] = true
		}
	}
	return out
}

func (s *Server) albenAntwort(r *http.Request, alben []store.MusikAlbum, fav map[string]map[string]bool) []musikAlbumAntwort {
	if fav == nil {
		fav = s.musikFavoritenMenge(r)
	}
	out := make([]musikAlbumAntwort, len(alben))
	for i, a := range alben {
		out[i] = musikAlbumAntwort{MusikAlbum: a, Favorit: fav["album"][a.ID]}
		if a.CoverQuelle != "" {
			out[i].Cover = coverAdresse(a.ID)
		}
	}
	return out
}

func (s *Server) titelAntwort(r *http.Request, titel []store.MusikTitel, fav map[string]map[string]bool) []musikTitelAntwort {
	if fav == nil {
		fav = s.musikFavoritenMenge(r)
	}
	// Welche Alben ein Cover haben — einmal nachsehen, nicht je Titel.
	mitCover := map[string]bool{}
	var albumIDs []string
	for _, t := range titel {
		if _, ok := mitCover[t.AlbumID]; !ok && t.AlbumID != "" {
			mitCover[t.AlbumID] = false
			albumIDs = append(albumIDs, t.AlbumID)
		}
	}
	if len(albumIDs) > 0 {
		if v, err := s.musikSicht(r); err == nil {
			if alben, _, err := s.st.MusikAlben(r.Context(), v, store.MusikAlbenAbfrage{IDs: albumIDs, Limit: 1000}); err == nil {
				for _, a := range alben {
					mitCover[a.ID] = a.CoverQuelle != ""
				}
			}
		}
	}
	out := make([]musikTitelAntwort, len(titel))
	for i, t := range titel {
		out[i] = musikTitelAntwort{MusikTitel: t, Favorit: fav["titel"][t.ID],
			Stream: "/api/v1/medien/musik/titel/" + t.ID, Mime: MusikMime(t.Codec, t.Container)}
		if mitCover[t.AlbumID] {
			out[i].Cover = coverAdresse(t.AlbumID)
		}
	}
	return out
}

// ---------------------------------------------------------------- Lesen

// musikStart: GET /musik/start — die Startseite: zuletzt gehört (eigenes
// Profil), zuletzt hinzugefügt, und wie viel es gibt.
func (s *Server) musikStart(w http.ResponseWriter, r *http.Request) {
	v, err := s.musikSicht(r)
	if err != nil {
		failErr(w, err)
		return
	}
	fav := s.musikFavoritenMenge(r)
	neu, _, err := s.st.MusikAlben(r.Context(), v, store.MusikAlbenAbfrage{Sortierung: "neu", Limit: 24})
	if err != nil {
		failErr(w, err)
		return
	}
	zuletzt, err := s.zuletztGehoerteAlben(r, v, 18)
	if err != nil {
		failErr(w, err)
		return
	}
	titel, alben, _ := s.st.MusikZaehlen(r.Context(), v)
	interpreten, _ := s.st.MusikInterpreten(r.Context(), v)
	listen, _ := s.st.MusikListen(r.Context(), accountOf(r).ID)
	ok(w, map[string]any{
		"zuletztGehoert":    s.albenAntwort(r, zuletzt, fav),
		"neu":               s.albenAntwort(r, neu, fav),
		"anzahlTitel":       titel,
		"anzahlAlben":       alben,
		"anzahlInterpreten": len(interpreten),
		"listen":            listen,
	})
}

// zuletztGehoerteAlben: aus den zuletzt gehörten Titeln die Alben, in
// dieser Reihenfolge, jedes einmal.
func (s *Server) zuletztGehoerteAlben(r *http.Request, v store.MusikSicht, n int) ([]store.MusikAlbum, error) {
	ids, err := s.st.MusikZuletztGehoert(r.Context(), accountOf(r).ID, 400)
	if err != nil {
		return nil, err
	}
	titel, err := s.st.MusikTitelNachIDs(r.Context(), v, ids)
	if err != nil {
		return nil, err
	}
	var albumIDs []string
	gesehen := map[string]bool{}
	for _, t := range titel {
		if t.AlbumID != "" && !gesehen[t.AlbumID] {
			gesehen[t.AlbumID] = true
			albumIDs = append(albumIDs, t.AlbumID)
			if len(albumIDs) >= n {
				break
			}
		}
	}
	if len(albumIDs) == 0 {
		return []store.MusikAlbum{}, nil
	}
	alben, _, err := s.st.MusikAlben(r.Context(), v, store.MusikAlbenAbfrage{IDs: albumIDs, Limit: n})
	if err != nil {
		return nil, err
	}
	pos := map[string]int{}
	for i, id := range albumIDs {
		pos[id] = i
	}
	sort.Slice(alben, func(i, j int) bool { return pos[alben[i].ID] < pos[alben[j].ID] })
	return alben, nil
}

// musikAlben: GET /musik/alben?sort=name|interpret|jahr|neu&genre=&offset=&limit=
func (s *Server) musikAlben(w http.ResponseWriter, r *http.Request) {
	v, err := s.musikSicht(r)
	if err != nil {
		failErr(w, err)
		return
	}
	q := r.URL.Query()
	alben, gesamt, err := s.st.MusikAlben(r.Context(), v, store.MusikAlbenAbfrage{
		Sortierung: q.Get("sort"), Genre: q.Get("genre"),
		Offset: qInt(r, "offset", 0), Limit: qInt(r, "limit", 200)})
	if err != nil {
		failErr(w, err)
		return
	}
	off := qInt(r, "offset", 0)
	ok(w, map[string]any{"alben": s.albenAntwort(r, alben, nil), "gesamt": gesamt,
		"weiter": off+len(alben) < gesamt})
}

// musikAlbum: GET /musik/alben/{id} — das Album mit Titeln, nach Disc.
func (s *Server) musikAlbum(w http.ResponseWriter, r *http.Request) {
	v, err := s.musikSicht(r)
	if err != nil {
		failErr(w, err)
		return
	}
	a, titel, err := s.st.MusikAlbum(r.Context(), v, r.PathValue("id"))
	if err != nil {
		failErr(w, err)
		return
	}
	fav := s.musikFavoritenMenge(r)
	discs := map[int]bool{}
	for _, t := range titel {
		discs[t.Disc] = true
	}
	// Mehr von diesem Interpreten — unter dem Album, wie bei Apple Music.
	var mehr []store.MusikAlbum
	if a.InterpretID != "" {
		alle, _, _ := s.st.MusikAlben(r.Context(), v, store.MusikAlbenAbfrage{InterpretID: a.InterpretID, Sortierung: "jahr", Limit: 30})
		for _, x := range alle {
			if x.ID != a.ID {
				mehr = append(mehr, x)
			}
		}
	}
	ok(w, map[string]any{
		"album":        s.albenAntwort(r, []store.MusikAlbum{*a}, fav)[0],
		"titel":        s.titelAntwort(r, titel, fav),
		"mehrereDiscs": len(discs) > 1,
		"mehr":         s.albenAntwort(r, mehr, fav),
	})
}

// musikInterpreten: GET /musik/interpreten
func (s *Server) musikInterpreten(w http.ResponseWriter, r *http.Request) {
	v, err := s.musikSicht(r)
	if err != nil {
		failErr(w, err)
		return
	}
	alle, err := s.st.MusikInterpreten(r.Context(), v)
	if err != nil {
		failErr(w, err)
		return
	}
	fav := s.musikFavoritenMenge(r)
	out := make([]musikInterpretAntwort, len(alle))
	for i, m := range alle {
		out[i] = musikInterpretAntwort{MusikInterpret: m, Cover: coverAdresse(m.CoverAlbum), Favorit: fav["interpret"][m.ID]}
	}
	ok(w, map[string]any{"interpreten": out})
}

// musikInterpret: GET /musik/interpreten/{id} — Alben, „Erscheint auf" und
// „Beliebt": die Titel, die dieses Profil von ihm am häufigsten gehört
// hat. Niemand sonst fliesst da ein.
func (s *Server) musikInterpret(w http.ResponseWriter, r *http.Request) {
	v, err := s.musikSicht(r)
	if err != nil {
		failErr(w, err)
		return
	}
	id := r.PathValue("id")
	name, err := s.st.MusikInterpretName(r.Context(), v, id)
	if err != nil {
		failErr(w, err)
		return
	}
	fav := s.musikFavoritenMenge(r)
	alben, _, err := s.st.MusikAlben(r.Context(), v, store.MusikAlbenAbfrage{InterpretID: id, Sortierung: "jahr", Limit: 500})
	if err != nil {
		failErr(w, err)
		return
	}
	auf, err := s.st.MusikErscheintAuf(r.Context(), v, id, name)
	if err != nil {
		failErr(w, err)
		return
	}
	alleTitel, _, err := s.st.MusikTitelListe(r.Context(), v, store.MusikTitelAbfrage{InterpretName: name, Limit: 2000})
	if err != nil {
		failErr(w, err)
		return
	}
	ids := make([]string, len(alleTitel))
	for i, t := range alleTitel {
		ids[i] = t.ID
	}
	oft, err := s.st.MusikWieOft(r.Context(), accountOf(r).ID, ids)
	if err != nil {
		failErr(w, err)
		return
	}
	beliebt := []store.MusikTitel{}
	for _, t := range alleTitel {
		if oft[t.ID] > 0 {
			beliebt = append(beliebt, t)
		}
	}
	sort.SliceStable(beliebt, func(i, j int) bool { return oft[beliebt[i].ID] > oft[beliebt[j].ID] })
	if len(beliebt) > 10 {
		beliebt = beliebt[:10]
	}
	cover := ""
	for _, a := range alben {
		if a.CoverQuelle != "" {
			cover = coverAdresse(a.ID)
			break
		}
	}
	ok(w, map[string]any{
		"interpret": musikInterpretAntwort{MusikInterpret: store.MusikInterpret{ID: id, Name: name, Alben: len(alben)},
			Cover: cover, Favorit: fav["interpret"][id]},
		"alben":        s.albenAntwort(r, alben, fav),
		"erscheintAuf": s.albenAntwort(r, auf, fav),
		"beliebt":      s.titelAntwort(r, beliebt, fav),
		"anzahlTitel":  len(alleTitel),
	})
}

// musikTitel: GET /musik/titel?sort=titel|interpret|album|jahr|dauer|neu&ab=1&genre=&offset=&limit=
func (s *Server) musikTitel(w http.ResponseWriter, r *http.Request) {
	v, err := s.musikSicht(r)
	if err != nil {
		failErr(w, err)
		return
	}
	q := r.URL.Query()
	titel, gesamt, err := s.st.MusikTitelListe(r.Context(), v, store.MusikTitelAbfrage{
		Sortierung: q.Get("sort"), Absteigend: q.Get("ab") == "1", Genre: q.Get("genre"),
		Offset: qInt(r, "offset", 0), Limit: qInt(r, "limit", 300)})
	if err != nil {
		failErr(w, err)
		return
	}
	off := qInt(r, "offset", 0)
	ok(w, map[string]any{"titel": s.titelAntwort(r, titel, nil), "gesamt": gesamt,
		"weiter": off+len(titel) < gesamt})
}

// musikGenres: GET /musik/genres
func (s *Server) musikGenres(w http.ResponseWriter, r *http.Request) {
	v, err := s.musikSicht(r)
	if err != nil {
		failErr(w, err)
		return
	}
	g, err := s.st.MusikGenres(r.Context(), v)
	if err != nil {
		failErr(w, err)
		return
	}
	type genreAntwort struct {
		store.MusikGenre
		Cover string `json:"cover,omitempty"`
	}
	out := make([]genreAntwort, len(g))
	for i, x := range g {
		out[i] = genreAntwort{MusikGenre: x, Cover: coverAdresse(x.CoverAlbum)}
	}
	ok(w, map[string]any{"genres": out})
}

// musikSuche: GET /musik/suche?q= — der Abschnitt „Musik" in der Suche.
func (s *Server) musikSuche(w http.ResponseWriter, r *http.Request) {
	v, err := s.musikSicht(r)
	if err != nil {
		failErr(w, err)
		return
	}
	interpreten, alben, titel, err := s.st.MusikSuche(r.Context(), v, suchEingabe(r), min(max(qInt(r, "limit", 12), 1), 100))
	if err != nil {
		failErr(w, err)
		return
	}
	fav := s.musikFavoritenMenge(r)
	ia := make([]musikInterpretAntwort, len(interpreten))
	for i, m := range interpreten {
		ia[i] = musikInterpretAntwort{MusikInterpret: m, Cover: coverAdresse(m.CoverAlbum), Favorit: fav["interpret"][m.ID]}
	}
	ok(w, map[string]any{"interpreten": ia, "alben": s.albenAntwort(r, alben, fav), "titel": s.titelAntwort(r, titel, fav)})
}

// ---------------------------------------------------------------- Eigenes

// musikGehoert: POST /musik/titel/{id}/gehoert — das Gerät meldet, dass
// ein Titel (fast) ganz gehört wurde. Ohne Zeitpunkt, nur fürs eigene
// Profil.
func (s *Server) musikGehoert(w http.ResponseWriter, r *http.Request) {
	v, err := s.musikSicht(r)
	if err != nil {
		failErr(w, err)
		return
	}
	t, err := s.st.MusikTitelEinzeln(r.Context(), v, r.PathValue("id"))
	if err != nil {
		failErr(w, err)
		return
	}
	if err := s.st.MusikGehoert(r.Context(), accountOf(r).ID, t.ID); err != nil {
		failErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) musikHoerverlaufLoeschen(w http.ResponseWriter, r *http.Request) {
	if err := s.st.MusikHoerverlaufLoeschen(r.Context(), accountOf(r).ID); err != nil {
		failErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// musikFavoriten: GET /musik/favoriten — Titel, Alben und Interpreten mit
// Herz, neueste zuerst.
func (s *Server) musikFavoriten(w http.ResponseWriter, r *http.Request) {
	v, err := s.musikSicht(r)
	if err != nil {
		failErr(w, err)
		return
	}
	fav, err := s.st.MusikFavoriten(r.Context(), accountOf(r).ID)
	if err != nil {
		failErr(w, err)
		return
	}
	menge := s.musikFavoritenMenge(r)
	titel, err := s.st.MusikTitelNachIDs(r.Context(), v, fav["titel"])
	if err != nil {
		failErr(w, err)
		return
	}
	alben, _, err := s.st.MusikAlben(r.Context(), v, store.MusikAlbenAbfrage{IDs: fav["album"], Limit: 1000})
	if err != nil {
		failErr(w, err)
		return
	}
	pos := map[string]int{}
	for i, id := range fav["album"] {
		pos[id] = i
	}
	sort.Slice(alben, func(i, j int) bool { return pos[alben[i].ID] < pos[alben[j].ID] })
	alleInterpreten, _ := s.st.MusikInterpreten(r.Context(), v)
	interpreten := []musikInterpretAntwort{}
	for _, id := range fav["interpret"] {
		for _, m := range alleInterpreten {
			if m.ID == id {
				interpreten = append(interpreten, musikInterpretAntwort{MusikInterpret: m, Cover: coverAdresse(m.CoverAlbum), Favorit: true})
			}
		}
	}
	ok(w, map[string]any{"titel": s.titelAntwort(r, titel, menge), "alben": s.albenAntwort(r, alben, menge),
		"interpreten": interpreten})
}

func (s *Server) musikFavoritSetzen(w http.ResponseWriter, r *http.Request, an bool) {
	art, id := r.PathValue("art"), r.PathValue("id")
	if !store.MusikFavoritArten[art] {
		fail(w, http.StatusBadRequest, "art ist titel, album oder interpret")
		return
	}
	// Ein Herz nur für etwas, das die Person sehen darf.
	if an {
		v, err := s.musikSicht(r)
		if err != nil {
			failErr(w, err)
			return
		}
		switch art {
		case "titel":
			_, err = s.st.MusikTitelEinzeln(r.Context(), v, id)
		case "album":
			_, _, err = s.st.MusikAlbum(r.Context(), v, id)
		case "interpret":
			_, err = s.st.MusikInterpretName(r.Context(), v, id)
		}
		if err != nil {
			failErr(w, err)
			return
		}
	}
	if err := s.st.MusikFavoritSetzen(r.Context(), accountOf(r).ID, art, id, an); err != nil {
		failErr(w, err)
		return
	}
	ok(w, map[string]bool{"favorit": an})
}

func (s *Server) musikFavoritAn(w http.ResponseWriter, r *http.Request) {
	s.musikFavoritSetzen(w, r, true)
}
func (s *Server) musikFavoritAus(w http.ResponseWriter, r *http.Request) {
	s.musikFavoritSetzen(w, r, false)
}

// ---------------------------------------------------------------- Listen

func (s *Server) musikListen(w http.ResponseWriter, r *http.Request) {
	l, err := s.st.MusikListen(r.Context(), accountOf(r).ID)
	if err != nil {
		failErr(w, err)
		return
	}
	ok(w, map[string]any{"listen": l})
}

func (s *Server) musikListeAnlegen(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name  string   `json:"name"`
		Titel []string `json:"titel"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	acc := accountOf(r)
	l, err := s.st.MusikListeAnlegen(r.Context(), acc.ID, in.Name)
	if err != nil {
		failErr(w, err)
		return
	}
	if len(in.Titel) > 0 {
		if err := s.musikListeFuellen(r, l.ID, in.Titel); err != nil {
			failErr(w, err)
			return
		}
		l, _ = s.st.MusikListeHolen(r.Context(), acc.ID, l.ID)
	}
	ok(w, l)
}

// musikListe: GET /musik/listen/{id} — die Titel, die diese Person sehen
// darf. In einer geteilten Liste sieht ein Kind nur, was es ohnehin darf.
func (s *Server) musikListe(w http.ResponseWriter, r *http.Request) {
	l, err := s.st.MusikListeHolen(r.Context(), accountOf(r).ID, r.PathValue("id"))
	if err != nil {
		failErr(w, err)
		return
	}
	v, err := s.musikSicht(r)
	if err != nil {
		failErr(w, err)
		return
	}
	titel, err := s.st.MusikTitelNachIDs(r.Context(), v, l.TitelIDs)
	if err != nil {
		failErr(w, err)
		return
	}
	var dauer int64
	for _, t := range titel {
		dauer += t.DauerMS
	}
	// Die Cover der ersten vier Alben als Mosaik.
	var mosaik []string
	gesehen := map[string]bool{}
	ta := s.titelAntwort(r, titel, nil)
	for _, t := range ta {
		if t.Cover != "" && !gesehen[t.Cover] && len(mosaik) < 4 {
			gesehen[t.Cover] = true
			mosaik = append(mosaik, t.Cover)
		}
	}
	ok(w, map[string]any{"liste": l, "titel": ta, "dauerMs": dauer, "mosaik": mosaik})
}

func (s *Server) musikListeAendern(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name      *string `json:"name"`
		Geteilt   *bool   `json:"geteilt"`
		Gemeinsam *bool   `json:"gemeinsam"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	acc := accountOf(r)
	if err := s.st.MusikListeAendern(r.Context(), acc.ID, r.PathValue("id"), in.Name, in.Geteilt, in.Gemeinsam); err != nil {
		failErr(w, err)
		return
	}
	l, err := s.st.MusikListeHolen(r.Context(), acc.ID, r.PathValue("id"))
	if err != nil {
		failErr(w, err)
		return
	}
	ok(w, l)
}

func (s *Server) musikListeLoeschen(w http.ResponseWriter, r *http.Request) {
	if err := s.st.MusikListeLoeschen(r.Context(), accountOf(r).ID, r.PathValue("id")); err != nil {
		failErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// musikListeFuellen: nur Titel, die die Person sehen darf, kommen hinein.
func (s *Server) musikListeFuellen(r *http.Request, id string, titelIDs []string) error {
	if len(titelIDs) > 5000 {
		return &apiFehler{status: http.StatusBadRequest, text: "zu viele titel auf einmal"}
	}
	v, err := s.musikSicht(r)
	if err != nil {
		return err
	}
	sichtbar, err := s.st.MusikTitelNachIDs(r.Context(), v, titelIDs)
	if err != nil {
		return err
	}
	ids := make([]string, len(sichtbar))
	for i, t := range sichtbar {
		ids[i] = t.ID
	}
	return s.st.MusikListeHinzufuegen(r.Context(), accountOf(r).ID, id, ids)
}

// musikListeHinzufuegen: POST /musik/listen/{id}/titel {"titel": [...]} —
// oder {"album": "id"} für ein ganzes Album.
func (s *Server) musikListeHinzufuegen(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Titel []string `json:"titel"`
		Album string   `json:"album"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	ids := in.Titel
	if in.Album != "" {
		v, err := s.musikSicht(r)
		if err != nil {
			failErr(w, err)
			return
		}
		_, titel, err := s.st.MusikAlbum(r.Context(), v, in.Album)
		if err != nil {
			failErr(w, err)
			return
		}
		for _, t := range titel {
			ids = append(ids, t.ID)
		}
	}
	if err := s.musikListeFuellen(r, r.PathValue("id"), ids); err != nil {
		failErr(w, err)
		return
	}
	l, err := s.st.MusikListeHolen(r.Context(), accountOf(r).ID, r.PathValue("id"))
	if err != nil {
		failErr(w, err)
		return
	}
	ok(w, l)
}

func (s *Server) musikListeEntfernen(w http.ResponseWriter, r *http.Request) {
	if err := s.st.MusikListeEntfernen(r.Context(), accountOf(r).ID, r.PathValue("id"), r.PathValue("titel")); err != nil {
		failErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) musikListeOrdnen(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Titel []string `json:"titel"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if err := s.st.MusikListeOrdnen(r.Context(), accountOf(r).ID, r.PathValue("id"), in.Titel); err != nil {
		failErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------- Medien

// musikDatei: GET /medien/musik/titel/{id}[?als=aac|opus] — das Lied, mit
// Range. Ohne „als" die Originaldatei; mit „als" eine umgewandelte
// Fassung, die einmal ganz gerechnet und dann zwischengespeichert wird —
// so kann der Browser darin springen, und das nächste Mal geht es sofort.
func (s *Server) musikDatei(w http.ResponseWriter, r *http.Request) {
	v, err := s.musikSicht(r)
	if err != nil {
		failErr(w, err)
		return
	}
	t, err := s.st.MusikTitelEinzeln(r.Context(), v, r.PathValue("id"))
	if err != nil {
		failErr(w, err)
		return
	}
	als := r.URL.Query().Get("als")
	if als == "" {
		dateiSenden(w, r, t.Pfad, musikTyp(t), "")
		return
	}
	if als != "aac" && als != "opus" {
		fail(w, http.StatusBadRequest, "als ist aac oder opus")
		return
	}
	lim, _ := s.st.Limits(r.Context(), accountOf(r).ID)
	if !lim.AllowTranscode {
		fail(w, http.StatusUnsupportedMediaType, "dieses gerät bräuchte eine umwandlung, die für dein profil aus ist")
		return
	}
	if s.ff == nil {
		fail(w, http.StatusServiceUnavailable, "ffmpeg fehlt")
		return
	}
	endung, typ := "m4a", "audio/mp4"
	if als == "opus" {
		endung, typ = "ogg", "audio/ogg"
	}
	// Nur kurz und unter einem Zufallsnamen (kurzlebig.go): Ein dauerhafter
	// Zwischenspeicher mit der Titelkennung im Namen hielte fest, wann
	// welches Lied gehört wurde.
	schluessel := fmt.Sprintf("musik:%s:%d:%s", t.ID, t.MTime, als)
	pfad, fertig, err := s.kurzlebigeDatei(r.Context(), schluessel, endung, func(ziel string) error {
		return s.ff.MusikUmwandeln(context.WithoutCancel(r.Context()), t.Pfad, ziel, als)
	})
	if err != nil {
		// Kein Titel, kein Pfad: was gespielt werden sollte, gehört nicht
		// ins Protokoll.
		s.st.Log(r.Context(), "warn", "stream", "eine Musikdatei liess sich nicht umwandeln")
		fail(w, http.StatusInternalServerError, "umwandeln ging nicht")
		return
	}
	defer fertig()
	dateiSenden(w, r, pfad, typ, "")
}

func musikTyp(t *store.MusikTitel) string {
	switch strings.ToLower(filepath.Ext(t.Pfad)) {
	case ".mp3":
		return "audio/mpeg"
	case ".m4a", ".m4b", ".aac", ".alac":
		return "audio/mp4"
	case ".flac":
		return "audio/flac"
	case ".ogg", ".oga", ".opus":
		return "audio/ogg"
	case ".wav":
		return "audio/wav"
	case ".mka":
		return "audio/webm"
	}
	return "application/octet-stream"
}

// coverGroessen: klein für Listen, mittel für Raster, gross für die
// Albumseite und den grossen Player.
var coverGroessen = map[string]int{"klein": 160, "mittel": 480, "gross": 1200}

// musikCover: GET /medien/musik/cover/{album}?g=klein|mittel|gross
func (s *Server) musikCover(w http.ResponseWriter, r *http.Request) {
	v, err := s.musikSicht(r)
	if err != nil {
		failErr(w, err)
		return
	}
	id := r.PathValue("id")
	quelle, err := s.st.MusikCoverQuelle(r.Context(), v, id)
	if err != nil {
		failErr(w, err)
		return
	}
	g := r.URL.Query().Get("g")
	breite, okG := coverGroessen[g]
	if !okG {
		g, breite = "mittel", coverGroessen["mittel"]
	}
	ziel, err := s.coverRechnen(r.Context(), id, quelle, g, breite, false)
	if err != nil {
		fail(w, http.StatusNotFound, "kein cover")
		return
	}
	dateiSenden(w, r, ziel, "image/jpeg", "")
}

// coverRechnen legt das Cover eines Albums in einer Grösse ab. Der Name
// trägt die Änderungszeit der Quelle: ein neues cover.jpg ergibt eine neue
// Datei.
func (s *Server) coverRechnen(ctx context.Context, albumID, quelle, g string, breite int, niedrig bool) (string, error) {
	if s.ff == nil || quelle == "" {
		return "", errors.New("kein cover")
	}
	art, pfad, _ := strings.Cut(quelle, ":")
	st, err := os.Stat(pfad)
	if err != nil {
		return "", err
	}
	ordner := s.mediathekOrdner("cover")
	vorsilbe := albumID + "-" + g + "-"
	ziel := filepath.Join(ordner, vorsilbe+kurz(quelle+strconv.FormatInt(st.ModTime().Unix(), 10))+".jpg")
	err = zwischenspeichern(ctx, ziel, func() error {
		alteFassungenWeg(ordner, vorsilbe, filepath.Base(ziel))
		if art == "eingebettet" {
			return s.ff.CoverAusDatei(context.WithoutCancel(ctx), pfad, ziel, breite, niedrig)
		}
		return s.ff.BildVerkleinern(context.WithoutCancel(ctx), pfad, ziel, breite, 1, strings.TrimPrefix(filepath.Ext(pfad), "."), niedrig)
	})
	return ziel, err
}
