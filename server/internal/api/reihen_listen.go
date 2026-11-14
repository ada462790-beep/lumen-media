// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"net/http"
	"sync"

	"lumen/internal/store"
)

// ReihenBilden lässt Lumen die Filmreihen neu erkennen. Läuft nach jedem
// Durchlauf, nach dem Holen der Metadaten und einmal beim Start.
// Nie zweimal gleichzeitig: beim Start, nach dem Prüfen zweier
// Bibliotheken und nach den Metadaten kann es sich überschneiden.
var reihenMu sync.Mutex

func (s *Server) ReihenBilden(ctx context.Context) {
	reihenMu.Lock()
	defer reihenMu.Unlock()
	n, err := s.st.ReihenNeuBilden(ctx)
	if err != nil {
		s.log.Error("filmreihen", "err", err)
		return
	}
	s.log.Info("filmreihen", "selbst erkannt", n)
}

// sichtbar macht aus Kennungen die Titel, die diese Person sehen darf —
// in der Reihenfolge der Kennungen. Was die Freigabe nicht erlaubt,
// fällt still heraus: eine geteilte Liste oder eine Reihe zeigt einem
// Kind nur, was es ohnehin sehen dürfte.
func (s *Server) sichtbar(r *http.Request, ids []string) ([]view, error) {
	if len(ids) == 0 {
		return []view{}, nil
	}
	q, err := s.scope(r)
	if err != nil {
		return nil, err
	}
	// In Stücken: eine Abfrage liefert höchstens 500 Titel, und über alle
	// Reihen zusammen kommen schnell mehr zusammen. Sonst fielen die
	// Filme hinten im Alphabet still heraus.
	const stueck = 400
	var items []store.Item
	for a := 0; a < len(ids); a += stueck {
		e := min(a+stueck, len(ids))
		qq := q
		qq.IDs = ids[a:e]
		qq.Kinds = []string{"movie", "series"}
		qq.Limit = 500
		teil, err := s.st.Query(r.Context(), qq)
		if err != nil {
			return nil, err
		}
		items = append(items, teil...)
	}
	views, err := s.decorate(r, items)
	if err != nil {
		return nil, err
	}
	nach := map[string]view{}
	for _, v := range views {
		nach[v.ID] = v
	}
	out := make([]view, 0, len(views))
	for _, id := range ids {
		if v, ok := nach[id]; ok {
			out = append(out, v)
		}
	}
	return out, nil
}

// ---------------------------------------------------------- Filmreihen

type reiheAntwort struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Overview string `json:"overview,omitempty"`
	Source   string `json:"source"`
	Hidden   bool   `json:"hidden,omitempty"`
	Anzahl   int    `json:"anzahl"`
	// Die Bilder der Reihe selbst (von TMDb). Fehlen sie, nehmen die Apps
	// das Poster des ersten Films.
	Poster   string `json:"poster,omitempty"`
	Backdrop string `json:"backdrop,omitempty"`
	Items    []view `json:"items"`
	// ZuletztNeu: wann zuletzt ein Titel dazukam (Unix-Sekunden) — zum
	// Sortieren nach „Zuletzt hinzugefügt".
	ZuletztNeu int64 `json:"zuletztNeu,omitempty"`
}

// zuletztNeu: das jüngste Hinzufügen unter den Titeln.
func zuletztNeu(items []view) int64 {
	var j int64
	for _, v := range items {
		if t := v.AddedAt.Unix(); t > j {
			j = t
		}
	}
	return j
}

func reiheBilder(x *store.Reihe, a *reiheAntwort) {
	if x.PosterDatei != "" {
		a.Poster = "/api/v1/art/" + x.ID + "/poster"
	}
	if x.BackdropDatei != "" {
		a.Backdrop = "/api/v1/art/" + x.ID + "/backdrop"
	}
}

// Eine Reihe zeigt sich nur, wenn die Person mindestens zwei Filme
// daraus sehen darf. Ein einzelner Film ist keine Reihe — und einem
// Kind soll keine Reihe mit Lücken angeboten werden, die es nie füllen
// darf.
const reiheMindestens = 2

// reiheMindestensFuer: Eine Reihe aus einer NFO-Datei (<set> aus Emby,
// Kodi, Jellyfin) hat jemand selbst angelegt — sie zeigt sich wie bei
// Emby auch mit nur einem vorhandenen Film. Vorher fehlten solche Reihen
// in Lumen (Emby 239 Reihen, Lumen 233). Selbst erkannte und TMDb-Reihen
// brauchen weiter zwei Filme.
func reiheMindestensFuer(source string) int {
	// Reihen, die der Besitzer selbst gepflegt hat (NFO-Sets, aus Emby
	// übernommen), zeigt Lumen wie Emby schon ab einem Film.
	if source == "nfo" || source == "emby" {
		return 1
	}
	return reiheMindestens
}

// reihen liefert die Filmreihen, die die Person sieht.
//
// ?limit= und ?offset= zählen nur die Reihen, die tatsächlich gezeigt
// werden (mindestens zwei sichtbare Filme). Die Startseite zeigt zwölf;
// vorher kamen bei ihr alle Reihen samt allen Filmen über die Leitung —
// bei 900 Reihen fast 5 MB, auf jedem Gerät, bei jedem Öffnen. Ohne
// limit kommt wie bisher alles (Seite „Filmreihen").
func (s *Server) reihen(w http.ResponseWriter, r *http.Request) {
	alle, err := s.st.Reihen(r.Context(), false)
	if err != nil {
		failErr(w, err)
		return
	}
	limit := max(qInt(r, "limit", 0), 0)
	offset := max(qInt(r, "offset", 0), 0)
	out := []reiheAntwort{}
	// In Stücken: Wer nur zwölf will, bekommt die Titel von etwas mehr
	// als zwölf Reihen nachgeschlagen, nicht die von allen.
	stueck := len(alle)
	if limit > 0 {
		stueck = max(2*(offset+limit), 16)
	}
	uebersprungen := 0
	for ab := 0; ab < len(alle); ab += stueck {
		teil := alle[ab:min(ab+stueck, len(alle))]
		var ids []string
		for _, x := range teil {
			ids = append(ids, x.ItemIDs...)
		}
		views, err := s.sichtbar(r, ids)
		if err != nil {
			failErr(w, err)
			return
		}
		nach := make(map[string]view, len(views))
		for _, v := range views {
			nach[v.ID] = v
		}
		for i, x := range teil {
			a := reiheAntwort{ID: x.ID, Name: x.Name, Overview: x.Overview, Source: x.Source,
				Items: []view{}}
			reiheBilder(&teil[i], &a)
			for _, id := range x.ItemIDs {
				if v, ok := nach[id]; ok {
					a.Items = append(a.Items, v)
				}
			}
			a.ZuletztNeu = zuletztNeu(a.Items)
			if a.Anzahl = len(a.Items); a.Anzahl < reiheMindestensFuer(x.Source) {
				continue
			}
			if uebersprungen < offset {
				uebersprungen++
				continue
			}
			out = append(out, a)
			if limit > 0 && len(out) >= limit {
				ok(w, out)
				return
			}
		}
	}
	ok(w, out)
}

func (s *Server) reihe(w http.ResponseWriter, r *http.Request) {
	x, err := s.st.Reihe(r.Context(), r.PathValue("id"))
	if err != nil {
		failErr(w, err)
		return
	}
	// Eine aufgelöste Reihe sieht nur noch der Besitzer — damit er sie
	// wiederherstellen kann.
	if x.Hidden && !accountOf(r).IsAdmin {
		failErr(w, store.ErrNotFound)
		return
	}
	items, err := s.sichtbar(r, x.ItemIDs)
	if err != nil {
		failErr(w, err)
		return
	}
	if len(items) < reiheMindestensFuer(x.Source) && !accountOf(r).IsAdmin {
		failErr(w, store.ErrNotFound)
		return
	}
	a := reiheAntwort{ID: x.ID, Name: x.Name, Overview: x.Overview, Source: x.Source,
		Hidden: x.Hidden, Anzahl: len(items), Items: items, ZuletztNeu: zuletztNeu(items)}
	reiheBilder(x, &a)
	ok(w, a)
}

// reiheZumTitel gehört in die Detailseite: die Reihe, in der ein Film
// steht, mit den übrigen Filmen daraus.
func (s *Server) reiheZumTitel(r *http.Request, itemID string) *reiheAntwort {
	x, err := s.st.ReiheVon(r.Context(), itemID)
	if err != nil || x == nil {
		return nil
	}
	items, err := s.sichtbar(r, x.ItemIDs)
	if err != nil || len(items) < reiheMindestensFuer(x.Source) {
		return nil
	}
	a := &reiheAntwort{ID: x.ID, Name: x.Name, Source: x.Source,
		Anzahl: len(items), Items: items}
	reiheBilder(x, a)
	return a
}

func (s *Server) adminReihen(w http.ResponseWriter, r *http.Request) {
	alle, err := s.st.Reihen(r.Context(), true)
	if err != nil {
		failErr(w, err)
		return
	}
	type zeile struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Source string `json:"source"`
		Hidden bool   `json:"hidden"`
		Anzahl int    `json:"anzahl"`
	}
	out := []zeile{}
	for _, x := range alle {
		out = append(out, zeile{x.ID, x.Name, x.Source, x.Hidden, len(x.ItemIDs)})
	}
	ok(w, out)
}

func (s *Server) adminReiheAendern(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name       *string `json:"name"`
		Aufgeloest *bool   `json:"aufgeloest"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if err := s.st.ReiheAendern(r.Context(), r.PathValue("id"), in.Name, in.Aufgeloest); err != nil {
		failErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------- Listen

type listenEintrag struct {
	view
	HinzugefuegtVon string `json:"hinzugefuegtVon,omitempty"`
}

type listeAntwort struct {
	store.Liste
	Enthaelt bool            `json:"enthaelt,omitempty"`
	Vorschau []view          `json:"vorschau,omitempty"`
	Items    []listenEintrag `json:"items,omitempty"`
}

func (s *Server) listen(w http.ResponseWriter, r *http.Request) {
	ich := accountOf(r).ID
	alle, err := s.st.Listen(r.Context(), ich)
	if err != nil {
		failErr(w, err)
		return
	}
	var drin map[string]bool
	// Nur für einen Titel, den die Person sehen darf — sonst liesse sich
	// mit einer erratenen Kennung prüfen, ob er in einer Liste steht.
	if item := r.URL.Query().Get("item"); item != "" {
		if _, err := s.visibleItem(r, item); err == nil {
			if drin, err = s.st.ListenMitTitel(r.Context(), ich, item); err != nil {
				failErr(w, err)
				return
			}
		}
	}
	out := []listeAntwort{}
	for _, l := range alle {
		a := listeAntwort{Liste: l, Enthaelt: drin[l.ID]}
		// Gezählt wird, was diese Person sehen darf — sonst verriete die
		// Zahl, dass in der Liste etwas steht, das ihr verborgen ist. Die
		// ersten vier davon sind das Titelbild.
		a.Anzahl = 0
		if voll, err := s.st.Liste(r.Context(), ich, l.ID); err == nil {
			if v, err := s.sichtbar(r, voll.ItemIDs); err == nil {
				a.Anzahl = len(v)
				if len(v) > 4 {
					v = v[:4]
				}
				a.Vorschau = v
			}
		}
		out = append(out, a)
	}
	ok(w, out)
}

func (s *Server) liste(w http.ResponseWriter, r *http.Request) {
	l, err := s.st.Liste(r.Context(), accountOf(r).ID, r.PathValue("id"))
	if err != nil {
		failErr(w, err)
		return
	}
	views, err := s.sichtbar(r, l.ItemIDs)
	if err != nil {
		failErr(w, err)
		return
	}
	a := listeAntwort{Liste: *l, Items: []listenEintrag{}}
	for _, v := range views {
		a.Items = append(a.Items, listenEintrag{view: v, HinzugefuegtVon: l.HinzugefuegtVon[v.ID]})
	}
	// Gezählt wird, was die Person sehen kann — nicht, was in der Liste
	// steht. Sonst hiesse es „7 Titel" über einer Liste mit fünf.
	a.Anzahl = len(a.Items)
	ok(w, a)
}

func (s *Server) listeAnlegen(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	l, err := s.st.ListeAnlegen(r.Context(), accountOf(r).ID, in.Name)
	if err != nil {
		failErr(w, err)
		return
	}
	ok(w, l)
}

func (s *Server) listeAendern(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name      *string `json:"name"`
		Geteilt   *bool   `json:"geteilt"`
		Gemeinsam *bool   `json:"gemeinsam"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if err := s.st.ListeAendern(r.Context(), accountOf(r).ID, r.PathValue("id"),
		in.Name, in.Geteilt, in.Gemeinsam); err != nil {
		failErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listeLoeschen(w http.ResponseWriter, r *http.Request) {
	if err := s.st.ListeLoeschen(r.Context(), accountOf(r).ID, r.PathValue("id")); err != nil {
		failErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listeHinzufuegen(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ItemID string `json:"itemId"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	// Nur, was man selbst sehen darf. Sonst liesse sich über eine
	// gemeinsame Liste herausfinden, welche Titel es gibt.
	it, err := s.visibleItem(r, in.ItemID)
	if err != nil {
		failErr(w, err)
		return
	}
	if it.Kind != "movie" && it.Kind != "series" {
		fail(w, http.StatusBadRequest, "in eine liste kommen filme und serien")
		return
	}
	if err := s.st.ListeHinzufuegen(r.Context(), accountOf(r).ID, r.PathValue("id"), it.ID); err != nil {
		failErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listeEntfernen(w http.ResponseWriter, r *http.Request) {
	// Wie beim Hinzufügen: nur, was man selbst sehen darf. Sonst verriete
	// die Antwort (204 oder 404), ob ein verborgener Titel in einer
	// gemeinsamen Liste steht — und man könnte ihn anderen herausnehmen.
	if _, err := s.visibleItem(r, r.PathValue("item")); err != nil {
		failErr(w, err)
		return
	}
	if err := s.st.ListeEntfernen(r.Context(), accountOf(r).ID, r.PathValue("id"),
		r.PathValue("item")); err != nil {
		failErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listeOrdnen(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ItemIDs []string `json:"itemIds"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if err := s.st.ListeOrdnen(r.Context(), accountOf(r).ID, r.PathValue("id"), in.ItemIDs); err != nil {
		failErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
