// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

// Eigene Sammlungen (store/sammlungen.go).
//
// Lesen darf jedes Profil — es sieht aber nur die Titel, die es ohnehin
// sehen darf (Bibliotheksfreigabe, Altersgrenze). Eine Sammlung, aus der
// jemand gar nichts sehen darf, gibt es für diese Person nicht.
// Zusammenstellen darf die Verwaltung (Besitzer und Mitverwalter).
//
//	GET    /api/v1/sammlungen                          alle sichtbaren
//	GET    /api/v1/sammlungen/{id}                     eine, mit Titeln
//	GET    /api/v1/sammlungen/{id}/bild                eigenes Bild (auch /api/v1/art/{id}/poster)
//	GET    /api/v1/admin/sammlungen?item=…             zum Hinzufügen, mit „enthaelt"
//	POST   /api/v1/admin/sammlungen                    anlegen
//	POST   /api/v1/admin/sammlungen/{id}               ändern (auch PATCH)
//	DELETE /api/v1/admin/sammlungen/{id}               löschen
//	POST   /api/v1/admin/sammlungen/{id}/titel         {"itemId"} hinzufügen
//	DELETE /api/v1/admin/sammlungen/{id}/titel/{item}  entfernen
//	POST   /api/v1/admin/sammlungen/{id}/reihenfolge   {"ids":[…]}
//	POST   /api/v1/admin/sammlungen/{id}/bild          Upload oder {"url"}
//	DELETE /api/v1/admin/sammlungen/{id}/bild          wieder aus Postern

import (
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"lumen/internal/store"
)

type sammlungAntwort struct {
	ID           string                `json:"id"`
	Name         string                `json:"name"`
	Beschreibung string                `json:"beschreibung,omitempty"`
	Sortierung   string                `json:"sortierung"`
	Regel        *store.SammlungsRegel `json:"regel,omitempty"`
	AufStart     bool                  `json:"aufStart"`
	// Bild: eigenes Bild der Sammlung. Fehlt es, setzen die Apps eines
	// aus den Postern der ersten Titel zusammen.
	Bild   string `json:"bild,omitempty"`
	Anzahl int    `json:"anzahl"`
	Items  []view `json:"items"`
	// VonHand: die von Hand hinzugefügten Titel (nur für die Verwaltung)
	// — der Rest kommt über die Regel und lässt sich nicht einzeln
	// entfernen oder verschieben.
	VonHand []string `json:"vonHand,omitempty"`
}

// sammlungInhalt stellt zusammen, was diese Person aus der Sammlung sieht.
func (s *Server) sammlungInhalt(r *http.Request, x *store.Sammlung) ([]view, error) {
	hand, err := s.sichtbar(r, x.ItemIDs)
	if err != nil {
		return nil, err
	}
	var regel []view
	if x.Regel != nil && !x.Regel.Leer() {
		q, err := s.scope(r)
		if err != nil {
			return nil, err
		}
		q.Genre, q.JahrVon, q.JahrBis = x.Regel.Genre, x.Regel.JahrVon, x.Regel.JahrBis
		q.Text = strings.TrimSpace(x.Regel.Stichwort)
		q.Kinds = []string{"movie", "series"}
		if x.Regel.Art != "" {
			q.Kinds = []string{x.Regel.Art}
		}
		q.Sort, q.Limit = "title", 500
		items, err := s.st.Query(r.Context(), q)
		if err != nil {
			return nil, err
		}
		if regel, err = s.decorate(r, items); err != nil {
			return nil, err
		}
	}
	schon := map[string]bool{}
	out := make([]view, 0, len(hand)+len(regel))
	for _, v := range hand {
		schon[v.ID] = true
		out = append(out, v)
	}
	for _, v := range regel {
		if !schon[v.ID] {
			schon[v.ID] = true
			out = append(out, v)
		}
	}
	switch x.Sortierung {
	case "jahr":
		// Nach Erscheinen (Datum, sonst Jahr) wie die Filmreihen; ohne
		// beides ans Ende, bei Gleichstand nach Titel.
		sort.SliceStable(out, func(i, j int) bool {
			a, b := erschienen(out[i].Item), erschienen(out[j].Item)
			if (a == "") != (b == "") {
				return b == ""
			}
			if a != b {
				return a < b
			}
			if out[i].SortTitle != out[j].SortTitle {
				return out[i].SortTitle < out[j].SortTitle
			}
			return out[i].ID < out[j].ID
		})
	case "titel":
		sort.SliceStable(out, func(i, j int) bool {
			if out[i].SortTitle != out[j].SortTitle {
				return out[i].SortTitle < out[j].SortTitle
			}
			return out[i].ID < out[j].ID
		})
	}
	return out, nil
}

// erschienen: das Erscheinungsdatum, ersatzweise das Jahr — als Text wie
// in der Datenbank (store.erschienenAusdruck), damit „2001" vor
// „2001-12-19" steht.
func erschienen(it store.Item) string {
	if it.Premiered != "" {
		return it.Premiered
	}
	if it.Year > 0 {
		return strconv.Itoa(it.Year)
	}
	return ""
}

func (s *Server) sammlungAntwortBauen(r *http.Request, x *store.Sammlung, items []view, alle bool) sammlungAntwort {
	a := sammlungAntwort{ID: x.ID, Name: x.Name, Beschreibung: x.Beschreibung,
		Sortierung: x.Sortierung, Regel: x.Regel, AufStart: x.AufStart,
		Anzahl: len(items), Items: items}
	if x.Bild != "" {
		// Unter /art/, damit Bilder-Tags es mit dem Medienschlüssel laden.
		a.Bild = "/api/v1/art/" + x.ID + "/poster"
	}
	if !alle && len(a.Items) > 4 {
		// In der Übersicht reichen die ersten für den Stapel.
		a.Items = a.Items[:4]
	}
	if accountOf(r).IsAdmin {
		a.VonHand = x.ItemIDs
		if a.VonHand == nil {
			a.VonHand = []string{}
		}
	}
	return a
}

func (s *Server) sammlungenListe(w http.ResponseWriter, r *http.Request) {
	alle, err := s.st.Sammlungen(r.Context())
	if err != nil {
		failErr(w, err)
		return
	}
	admin := accountOf(r).IsAdmin
	out := []sammlungAntwort{}
	for i := range alle {
		items, err := s.sammlungInhalt(r, &alle[i])
		if err != nil {
			failErr(w, err)
			return
		}
		// Leere zeigt nur die Verwaltung — sonst fände sie eine frisch
		// angelegte Sammlung nicht wieder.
		if len(items) == 0 && !admin {
			continue
		}
		out = append(out, s.sammlungAntwortBauen(r, &alle[i], items, false))
	}
	ok(w, out)
}

func (s *Server) sammlungZeigen(w http.ResponseWriter, r *http.Request) {
	x, err := s.st.Sammlung(r.Context(), r.PathValue("id"))
	if err != nil {
		failErr(w, err)
		return
	}
	items, err := s.sammlungInhalt(r, x)
	if err != nil {
		failErr(w, err)
		return
	}
	if len(items) == 0 && !accountOf(r).IsAdmin {
		failErr(w, store.ErrNotFound)
		return
	}
	ok(w, s.sammlungAntwortBauen(r, x, items, true))
}

func (s *Server) sammlungBildZeigen(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		id = r.PathValue("item") // über /api/v1/art/{item}/poster
	}
	x, err := s.st.Sammlung(r.Context(), id)
	if err != nil || x.Bild == "" || s.meta == nil {
		fail(w, http.StatusNotFound, "kein bild")
		return
	}
	if !accountOf(r).IsAdmin {
		if items, err := s.sammlungInhalt(r, x); err != nil || len(items) == 0 {
			fail(w, http.StatusNotFound, "kein bild")
			return
		}
	}
	if strings.ContainsAny(x.Bild, `/\`) || strings.Contains(x.Bild, "..") {
		fail(w, http.StatusNotFound, "kein bild")
		return
	}
	f, err := os.Open(filepath.Join(s.meta.ArtDir(), x.Bild))
	if err != nil {
		fail(w, http.StatusNotFound, "kein bild")
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		fail(w, http.StatusNotFound, "kein bild")
		return
	}
	// Der Name wechselt mit jedem neuen Bild; die Adresse bleibt aber
	// gleich. Darum kurz zwischenspeichern und dann nachfragen.
	bildAufbewahren(w, x.Bild, info)
	w.Header().Set("Content-Type", "image/jpeg")
	http.ServeContent(w, r, x.Bild, info.ModTime(), f)
}

// sammlungsReihen: die Sammlungen, die auf die Startseite sollen, als
// Reihen — nur mit dem, was diese Person sehen darf.
func (s *Server) sammlungsReihen(r *http.Request) []row {
	alle, err := s.st.Sammlungen(r.Context())
	if err != nil {
		return nil
	}
	var out []row
	for i := range alle {
		if !alle[i].AufStart {
			continue
		}
		items, err := s.sammlungInhalt(r, &alle[i])
		if err != nil || len(items) == 0 {
			continue
		}
		if len(items) > 20 {
			items = items[:20]
		}
		out = append(out, row{Key: "sammlung:" + alle[i].ID, Title: alle[i].Name, Items: items})
	}
	return out
}

// ---------------------------------------------------------------- Verwaltung

func (s *Server) adminSammlungen(w http.ResponseWriter, r *http.Request) {
	alle, err := s.st.Sammlungen(r.Context())
	if err != nil {
		failErr(w, err)
		return
	}
	var drin map[string]bool
	if item := r.URL.Query().Get("item"); item != "" {
		if _, err := s.visibleItem(r, item); err == nil {
			drin, _ = s.st.SammlungenMitTitel(r.Context(), item)
		}
	}
	type zeile struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		VonHand  int    `json:"vonHand"`
		Regel    bool   `json:"regel"`
		Enthaelt bool   `json:"enthaelt"`
	}
	out := []zeile{}
	for _, x := range alle {
		out = append(out, zeile{x.ID, x.Name, len(x.ItemIDs), x.Regel != nil, drin[x.ID]})
	}
	ok(w, out)
}

type sammlungEingabe struct {
	Name         *string               `json:"name"`
	Beschreibung *string               `json:"beschreibung"`
	Sortierung   *string               `json:"sortierung"`
	Regel        *store.SammlungsRegel `json:"regel"`
	AufStart     *bool                 `json:"aufStart"`
}

func (in sammlungEingabe) pruefen() (store.SammlungAenderung, error) {
	a := store.SammlungAenderung{Sortierung: in.Sortierung, AufStart: in.AufStart}
	if in.Name != nil {
		n := bereinigt(*in.Name, false)
		if n == "" {
			return a, &apiFehler{status: http.StatusBadRequest, text: "Die Sammlung braucht einen Namen."}
		}
		if zuLang(n, 80) {
			return a, &apiFehler{status: http.StatusBadRequest, text: "Der Name ist zu lang (höchstens 80 Zeichen)."}
		}
		a.Name = &n
	}
	if in.Beschreibung != nil {
		b := bereinigt(*in.Beschreibung, true)
		if zuLang(b, 2000) {
			return a, &apiFehler{status: http.StatusBadRequest, text: "Die Beschreibung ist zu lang (höchstens 2000 Zeichen)."}
		}
		a.Beschreibung = &b
	}
	if in.Regel != nil {
		rg := *in.Regel
		rg.Genre, rg.Stichwort = bereinigt(rg.Genre, false), bereinigt(rg.Stichwort, false)
		if zuLang(rg.Genre, 40) || zuLang(rg.Stichwort, 80) {
			return a, &apiFehler{status: http.StatusBadRequest, text: "Genre oder Stichwort sind zu lang."}
		}
		if rg.JahrVon < 0 || rg.JahrBis < 0 || (rg.JahrVon > 0 && rg.JahrBis > 0 && rg.JahrVon > rg.JahrBis) {
			return a, &apiFehler{status: http.StatusBadRequest, text: "Der Jahrbereich stimmt nicht: „von“ muss vor „bis“ liegen."}
		}
		if rg.Art != "" && rg.Art != "movie" && rg.Art != "series" {
			return a, &apiFehler{status: http.StatusBadRequest, text: "Unbekannte Art — nur Filme, Serien oder beides."}
		}
		a.Regel = &rg
	}
	if in.Sortierung != nil {
		ok := false
		for _, x := range store.Sammlungssortierungen {
			ok = ok || x == *in.Sortierung
		}
		if !ok {
			return a, &apiFehler{status: http.StatusBadRequest, text: "Unbekannte Sortierung."}
		}
	}
	return a, nil
}

func (s *Server) adminSammlungAnlegen(w http.ResponseWriter, r *http.Request) {
	var in sammlungEingabe
	if !readJSON(w, r, &in) {
		return
	}
	a, err := in.pruefen()
	if err != nil {
		failErr(w, err)
		return
	}
	if a.Name == nil {
		fail(w, http.StatusBadRequest, "Die Sammlung braucht einen Namen.")
		return
	}
	x, err := s.st.SammlungAnlegen(r.Context(), a)
	if err != nil {
		failErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, s.sammlungAntwortBauen(r, x, []view{}, true))
}

func (s *Server) adminSammlungAendern(w http.ResponseWriter, r *http.Request) {
	var in sammlungEingabe
	if !readJSON(w, r, &in) {
		return
	}
	a, err := in.pruefen()
	if err != nil {
		failErr(w, err)
		return
	}
	if err := s.st.SammlungAendern(r.Context(), r.PathValue("id"), a); err != nil {
		failErr(w, err)
		return
	}
	s.sammlungZeigen(w, r)
}

func (s *Server) adminSammlungLoeschen(w http.ResponseWriter, r *http.Request) {
	bild, err := s.st.SammlungLoeschen(r.Context(), r.PathValue("id"))
	if err != nil {
		failErr(w, err)
		return
	}
	if s.meta != nil {
		eigenesBildWeg(s.meta.ArtDir(), bild)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) adminSammlungTitelHinzu(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ItemID string `json:"itemId"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	it, err := s.visibleItem(r, in.ItemID)
	if err != nil {
		failErr(w, err)
		return
	}
	if it.Kind != "movie" && it.Kind != "series" {
		fail(w, http.StatusBadRequest, "In eine Sammlung kommen Filme und Serien, keine einzelnen Folgen.")
		return
	}
	if err := s.st.SammlungTitelHinzu(r.Context(), r.PathValue("id"), it.ID); err != nil {
		failErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) adminSammlungTitelWeg(w http.ResponseWriter, r *http.Request) {
	if err := s.st.SammlungTitelWeg(r.Context(), r.PathValue("id"), r.PathValue("item")); err != nil {
		failErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) adminSammlungOrdnen(w http.ResponseWriter, r *http.Request) {
	var in struct {
		IDs []string `json:"ids"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if len(in.IDs) > 5000 {
		fail(w, http.StatusBadRequest, "zu viele Titel")
		return
	}
	if _, err := s.st.Sammlung(r.Context(), r.PathValue("id")); err != nil {
		failErr(w, err)
		return
	}
	if err := s.st.SammlungOrdnen(r.Context(), r.PathValue("id"), in.IDs); err != nil {
		failErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) adminSammlungBild(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := s.st.Sammlung(r.Context(), id); err != nil {
		failErr(w, err)
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
	// Wie ein Poster: Hochformat.
	fertig, err := bildNeuKodieren(r.Context(), raw, 2000, 3000)
	if err != nil {
		failErr(w, err)
		return
	}
	name, err := bildAblegen(s.meta.ArtDir(), id, fertig)
	if err != nil {
		failErr(w, err)
		return
	}
	vorher, err := s.st.SammlungBild(r.Context(), id, name)
	if err != nil {
		eigenesBildWeg(s.meta.ArtDir(), name)
		failErr(w, err)
		return
	}
	eigenesBildWeg(s.meta.ArtDir(), vorher)
	s.sammlungZeigen(w, r)
}

func (s *Server) adminSammlungBildWeg(w http.ResponseWriter, r *http.Request) {
	vorher, err := s.st.SammlungBild(r.Context(), r.PathValue("id"), "")
	if err != nil {
		failErr(w, err)
		return
	}
	if s.meta != nil {
		eigenesBildWeg(s.meta.ArtDir(), vorher)
	}
	w.WriteHeader(http.StatusNoContent)
}
