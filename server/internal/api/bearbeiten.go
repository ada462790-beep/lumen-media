// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

// Metadaten von Hand bearbeiten (Verwaltung).
//
//	GET    /api/v1/admin/items/{id}/metadata         was sich bearbeiten lässt, samt Sperren
//	PATCH  /api/v1/admin/items/{id}/metadata         Felder ändern (auch POST, für Android)
//	POST   /api/v1/admin/items/{id}/artwork/{kind}   Poster/Hintergrund: Upload oder {"url"}
//	DELETE /api/v1/admin/items/{id}/lock/{feld}      Sperre eines Feldes aufheben
//
// Jedes geänderte Feld wird gesperrt (store/sperren.go): „Metadaten neu
// laden" und NFO-Dateien lassen es danach in Ruhe. Die Apps schicken nur,
// was sich wirklich geändert hat — sonst würde ein Speichern ohne
// Änderung alle Felder auf einmal sperren.

import (
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"lumen/internal/meta"
	"lumen/internal/store"
)

// Welche Felder es bei welcher Art gibt. Staffeln und Folgen haben weniger
// — eine Folge hat keine eigene Altersfreigabe, eine Staffel kein Genre.
var felderJeArt = map[string][]string{
	"movie": {store.FeldTitel, store.FeldOriginaltitel, store.FeldSortiertitel, store.FeldJahr,
		store.FeldBeschreibung, store.FeldGenres, store.FeldFreigabe, store.FeldErschienen,
		store.FeldBesetzung, store.FeldPoster, store.FeldHintergrund},
	"series": {store.FeldTitel, store.FeldOriginaltitel, store.FeldSortiertitel, store.FeldJahr,
		store.FeldBeschreibung, store.FeldGenres, store.FeldFreigabe, store.FeldErschienen,
		store.FeldBesetzung, store.FeldPoster, store.FeldHintergrund},
	"season":  {store.FeldTitel, store.FeldBeschreibung, store.FeldStaffel, store.FeldPoster},
	"episode": {store.FeldTitel, store.FeldBeschreibung, store.FeldFolge, store.FeldErschienen, store.FeldPoster},
}

type bearbeitenAntwort struct {
	Item struct {
		ID            string   `json:"id"`
		Kind          string   `json:"kind"`
		Title         string   `json:"title"`
		OriginalTitle string   `json:"originalTitle"`
		SortTitle     string   `json:"sortTitle"`
		Year          int      `json:"year,omitempty"`
		Overview      string   `json:"overview"`
		Genres        []string `json:"genres"`
		AgeRating     string   `json:"ageRating"`
		AgeYears      *int     `json:"ageYears,omitempty"`
		Premiered     string   `json:"premiered"`
		SeasonNo      int      `json:"seasonNo,omitempty"`
		EpisodeNo     int      `json:"episodeNo,omitempty"`
		MatchState    string   `json:"matchState"`
	} `json:"item"`
	Cast []struct {
		Name  string `json:"name"`
		Rolle string `json:"rolle,omitempty"`
	} `json:"cast"`
	Poster   string   `json:"poster,omitempty"`
	Backdrop string   `json:"backdrop,omitempty"`
	Locks    []string `json:"locks"`
	Felder   []string `json:"felder"`
	// Vorschläge für das Genre-Feld: was es in der Mediathek schon gibt.
	GenreVorschlaege []string `json:"genreVorschlaege"`
}

func (s *Server) adminBearbeitenLesen(w http.ResponseWriter, r *http.Request) {
	it, err := s.visibleItem(r, r.PathValue("id"))
	if err != nil {
		failErr(w, err)
		return
	}
	a, err := s.bearbeitenStand(r, it)
	if err != nil {
		failErr(w, err)
		return
	}
	ok(w, a)
}

func (s *Server) bearbeitenStand(r *http.Request, it *store.Item) (*bearbeitenAntwort, error) {
	ctx := r.Context()
	a := &bearbeitenAntwort{}
	a.Item.ID, a.Item.Kind = it.ID, it.Kind
	// Ein Standardtitel („Folge 3") steht in der Sprache dessen, der bearbeitet.
	a.Item.Title = standardTitel(s.wirksameSprache(ctx, accountOf(r).ID), it)
	a.Item.OriginalTitle, a.Item.Year, a.Item.Overview = it.OriginalTitle, it.Year, it.Overview
	a.Item.Genres = it.Genres
	if a.Item.Genres == nil {
		a.Item.Genres = []string{}
	}
	a.Item.AgeRating, a.Item.AgeYears, a.Item.Premiered = it.AgeRating, it.AgeYears, it.Premiered
	a.Item.SeasonNo, a.Item.EpisodeNo, a.Item.MatchState = it.SeasonNo, it.EpisodeNo, it.MatchState
	a.Item.SortTitle = s.st.Sortiertitel(ctx, it.ID)

	a.Cast = []struct {
		Name  string `json:"name"`
		Rolle string `json:"rolle,omitempty"`
	}{}
	if leute, err := s.st.Besetzung(ctx, it.ID); err == nil {
		for _, p := range leute {
			a.Cast = append(a.Cast, struct {
				Name  string `json:"name"`
				Rolle string `json:"rolle,omitempty"`
			}{p.Name, p.Rolle})
		}
	}
	if art, err := s.st.ArtworkFor(ctx, []string{it.ID}); err == nil {
		if n := art[it.ID]["poster"]; n != "" {
			a.Poster = "/api/v1/art/" + it.ID + "/poster"
		}
		if n := art[it.ID]["backdrop"]; n != "" {
			a.Backdrop = "/api/v1/art/" + it.ID + "/backdrop"
		}
	}
	sperren, err := s.st.Sperren(ctx, it.ID)
	if err != nil {
		return nil, err
	}
	a.Locks = []string{}
	for _, f := range store.SperrFelder {
		if sperren[f] {
			a.Locks = append(a.Locks, f)
		}
	}
	a.Felder = felderJeArt[it.Kind]
	if a.Felder == nil {
		a.Felder = []string{}
	}
	a.GenreVorschlaege = []string{}
	if it.Kind == "movie" || it.Kind == "series" {
		if q, err := s.scope(r); err == nil {
			if g, err := s.st.AlleGenres(ctx, q); err == nil {
				a.GenreVorschlaege = g
			}
		}
	}
	return a, nil
}

type bearbeitenEingabe struct {
	Title         *string `json:"title"`
	OriginalTitle *string `json:"originalTitle"`
	SortTitle     *string `json:"sortTitle"`
	// Jahr als Zahl; 0 löscht es.
	Year      *int      `json:"year"`
	Overview  *string   `json:"overview"`
	Genres    *[]string `json:"genres"`
	AgeRating *string   `json:"ageRating"`
	Premiered *string   `json:"premiered"`
	Cast      *[]struct {
		Name  string `json:"name"`
		Rolle string `json:"rolle"`
	} `json:"cast"`
	SeasonNo  *int `json:"seasonNo"`
	EpisodeNo *int `json:"episodeNo"`
}

var reDatum = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// bereinigt: Steuerzeichen raus, aussen kein Leerraum. Zeilenumbrüche
// bleiben, wo sie hingehören (Beschreibung).
func bereinigt(s string, mitZeilen bool) string {
	s = strings.Map(func(r rune) rune {
		if r == '\n' && mitZeilen {
			return r
		}
		if r == '\r' {
			return -1
		}
		if r < 0x20 || r == 0x7f {
			if r == '\t' || r == '\n' {
				return ' '
			}
			return -1
		}
		return r
	}, s)
	return strings.TrimSpace(s)
}

func zuLang(s string, n int) bool { return utf8.RuneCountInString(s) > n }

// handbearbeitungAus prüft die Eingabe und übersetzt sie. Fehler sind
// Sätze für die Person am Bildschirm.
func handbearbeitungAus(it *store.Item, in bearbeitenEingabe) (store.Handbearbeitung, error) {
	var h store.Handbearbeitung
	erlaubt := map[string]bool{}
	for _, f := range felderJeArt[it.Kind] {
		erlaubt[f] = true
	}
	nicht := func(feld string) error {
		return &apiFehler{status: http.StatusBadRequest, text: "dieses Feld gibt es hier nicht: {feld}", werte: []any{"feld", feld}}
	}
	if in.Title != nil {
		if !erlaubt[store.FeldTitel] {
			return h, nicht(store.FeldTitel)
		}
		t := bereinigt(*in.Title, false)
		if t == "" {
			return h, &apiFehler{status: http.StatusBadRequest, text: "Der Titel darf nicht leer sein."}
		}
		if zuLang(t, 300) {
			return h, &apiFehler{status: http.StatusBadRequest, text: "Der Titel ist zu lang (höchstens 300 Zeichen)."}
		}
		h.Title = &t
	}
	text := func(feld string, v *string, n int, ziel **string, zeilen bool) error {
		if v == nil {
			return nil
		}
		if !erlaubt[feld] {
			return nicht(feld)
		}
		t := bereinigt(*v, zeilen)
		if zuLang(t, n) {
			return &apiFehler{status: http.StatusBadRequest, text: "Ein Text ist zu lang."}
		}
		*ziel = &t
		return nil
	}
	if err := text(store.FeldOriginaltitel, in.OriginalTitle, 300, &h.OriginalTitle, false); err != nil {
		return h, err
	}
	if err := text(store.FeldSortiertitel, in.SortTitle, 300, &h.SortTitle, false); err != nil {
		return h, err
	}
	if err := text(store.FeldBeschreibung, in.Overview, 10000, &h.Overview, true); err != nil {
		return h, err
	}
	if in.Year != nil {
		if !erlaubt[store.FeldJahr] {
			return h, nicht(store.FeldJahr)
		}
		if y := *in.Year; y != 0 && (y < 1870 || y > time.Now().Year()+10) {
			return h, &apiFehler{status: http.StatusBadRequest, text: "Das Jahr sieht nicht richtig aus."}
		}
		h.Year = in.Year
	}
	if in.Premiered != nil {
		if !erlaubt[store.FeldErschienen] {
			return h, nicht(store.FeldErschienen)
		}
		d := strings.TrimSpace(*in.Premiered)
		if d != "" {
			if !reDatum.MatchString(d) {
				return h, &apiFehler{status: http.StatusBadRequest, text: "Das Erscheinungsdatum bitte als JJJJ-MM-TT."}
			}
			if _, err := time.Parse("2006-01-02", d); err != nil {
				return h, &apiFehler{status: http.StatusBadRequest, text: "Dieses Erscheinungsdatum gibt es nicht."}
			}
		}
		h.Premiered = &d
	}
	if in.AgeRating != nil {
		if !erlaubt[store.FeldFreigabe] {
			return h, nicht(store.FeldFreigabe)
		}
		a := bereinigt(*in.AgeRating, false)
		if a != "" {
			jahre := meta.AgeYears(a)
			if jahre == nil {
				// Eine blosse Zahl gilt als „ab … Jahren".
				if n, err := strconv.Atoi(a); err == nil && n >= 0 && n <= 21 {
					jahre = &n
				}
			}
			if jahre == nil {
				return h, &apiFehler{status: http.StatusBadRequest, text: "Diese Altersfreigabe kennt Lumen nicht. Möglich sind etwa 0, 6, 12, 16, 18, FSK 12 oder PG-13."}
			}
			h.AgeYears = jahre
		}
		h.AgeRating = &a
	}
	if in.Genres != nil {
		if !erlaubt[store.FeldGenres] {
			return h, nicht(store.FeldGenres)
		}
		if len(*in.Genres) > 20 {
			return h, &apiFehler{status: http.StatusBadRequest, text: "Höchstens 20 Genres."}
		}
		g := []string{}
		doppelt := map[string]bool{}
		for _, x := range *in.Genres {
			x = bereinigt(x, false)
			if x == "" || doppelt[strings.ToLower(x)] {
				continue
			}
			if zuLang(x, 40) {
				return h, &apiFehler{status: http.StatusBadRequest, text: "Ein Genre ist zu lang (höchstens 40 Zeichen)."}
			}
			doppelt[strings.ToLower(x)] = true
			g = append(g, x)
		}
		h.Genres = &g
	}
	if in.Cast != nil {
		if !erlaubt[store.FeldBesetzung] {
			return h, nicht(store.FeldBesetzung)
		}
		if len(*in.Cast) > 100 {
			return h, &apiFehler{status: http.StatusBadRequest, text: "Höchstens 100 Personen in der Besetzung."}
		}
		leute := []store.Person{}
		for _, p := range *in.Cast {
			n, rolle := bereinigt(p.Name, false), bereinigt(p.Rolle, false)
			if n == "" {
				continue
			}
			if zuLang(n, 120) || zuLang(rolle, 120) {
				return h, &apiFehler{status: http.StatusBadRequest, text: "Ein Name oder eine Rolle ist zu lang."}
			}
			leute = append(leute, store.Person{Name: n, Rolle: rolle})
		}
		h.Cast = &leute
	}
	if in.SeasonNo != nil {
		if !erlaubt[store.FeldStaffel] {
			return h, nicht(store.FeldStaffel)
		}
		if *in.SeasonNo < 0 || *in.SeasonNo > 9999 {
			return h, &apiFehler{status: http.StatusBadRequest, text: "Die Staffelnummer sieht nicht richtig aus."}
		}
		h.SeasonNo = in.SeasonNo
	}
	if in.EpisodeNo != nil {
		if !erlaubt[store.FeldFolge] {
			return h, nicht(store.FeldFolge)
		}
		if *in.EpisodeNo < 0 || *in.EpisodeNo > 99999 {
			return h, &apiFehler{status: http.StatusBadRequest, text: "Die Folgennummer sieht nicht richtig aus."}
		}
		h.EpisodeNo = in.EpisodeNo
	}
	return h, nil
}

func (s *Server) adminBearbeitenSpeichern(w http.ResponseWriter, r *http.Request) {
	it, err := s.visibleItem(r, r.PathValue("id"))
	if err != nil {
		failErr(w, err)
		return
	}
	var in bearbeitenEingabe
	if !readJSON(w, r, &in) {
		return
	}
	h, err := handbearbeitungAus(it, in)
	if err != nil {
		failErr(w, err)
		return
	}
	if err := s.st.MetadatenVonHand(r.Context(), it.ID, h); err != nil {
		if errors.Is(err, store.ErrNummerBelegt) {
			fail(w, http.StatusConflict, "Diese Nummer ist in der Staffel schon vergeben.")
			return
		}
		failErr(w, err)
		return
	}
	// Ein neuer Titel kann eine Filmreihe bilden oder auflösen.
	if it.Kind == "movie" && (h.Title != nil || h.Year != nil) {
		go s.ReihenBilden(detach(r))
	}
	neu, err := s.st.Item(r.Context(), it.ID)
	if err != nil {
		failErr(w, err)
		return
	}
	a, err := s.bearbeitenStand(r, neu)
	if err != nil {
		failErr(w, err)
		return
	}
	ok(w, a)
}

func (s *Server) adminBildSetzen(w http.ResponseWriter, r *http.Request) {
	it, err := s.visibleItem(r, r.PathValue("id"))
	if err != nil {
		failErr(w, err)
		return
	}
	kind := r.PathValue("kind")
	erlaubt := false
	for _, f := range felderJeArt[it.Kind] {
		if f == kind && (kind == store.FeldPoster || kind == store.FeldHintergrund) {
			erlaubt = true
		}
	}
	if !erlaubt {
		fail(w, http.StatusBadRequest, "diese Bildart gibt es hier nicht")
		return
	}
	if s.meta == nil {
		fail(w, http.StatusServiceUnavailable, "der Bilderordner ist nicht bereit")
		return
	}
	raw, err := bildLesen(w, r)
	if err != nil {
		failErr(w, err)
		return
	}
	// Poster im Hochformat, Hintergrund in 4K-Breite: mehr zeigt kein Gerät.
	maxB, maxH := 2000, 3000
	if kind == store.FeldHintergrund {
		maxB, maxH = 3840, 2160
	}
	fertig, err := bildNeuKodieren(r.Context(), raw, maxB, maxH)
	if err != nil {
		failErr(w, err)
		return
	}
	name, err := bildAblegen(s.meta.ArtDir(), it.ID+"-"+kind, fertig)
	if err != nil {
		failErr(w, err)
		return
	}
	vorher, err := s.st.BildVonHand(r.Context(), it.ID, kind, name)
	if err != nil {
		eigenesBildWeg(s.meta.ArtDir(), name)
		failErr(w, err)
		return
	}
	eigenesBildWeg(s.meta.ArtDir(), vorher)
	neu, _ := s.st.Item(r.Context(), it.ID)
	a, err := s.bearbeitenStand(r, neu)
	if err != nil {
		failErr(w, err)
		return
	}
	ok(w, a)
}

func (s *Server) adminSperreAufheben(w http.ResponseWriter, r *http.Request) {
	it, err := s.visibleItem(r, r.PathValue("id"))
	if err != nil {
		failErr(w, err)
		return
	}
	feld := r.PathValue("feld")
	if !store.SperrFeldBekannt(feld) {
		fail(w, http.StatusBadRequest, "unbekanntes Feld")
		return
	}
	if err := s.st.SperreAufheben(r.Context(), it.ID, feld); err != nil {
		failErr(w, err)
		return
	}
	a, err := s.bearbeitenStand(r, it)
	if err != nil {
		failErr(w, err)
		return
	}
	ok(w, a)
}
