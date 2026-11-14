// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package uebernahme

// Filmreihen aus Emby oder Jellyfin übernehmen.
//
// Emby nennt sie „Sammlungen" (BoxSet). Lumen holt jede mit ihren Filmen,
// ordnet die Filme zu wie beim Rest der Übernahme (Kennung, Datei, Titel
// und Jahr) und vergleicht mit den Reihen, die Lumen schon hat:
//
//   - Gleiche TMDb-Sammlung, gleicher Name (ohne „Collection", „Filmreihe"
//     …) oder dieselben Filme: Die Reihe gibt es schon. Fehlen ihr Filme,
//     wird sie ergänzt.
//   - Hat der Besitzer die passende Reihe aufgelöst, bleibt sie aufgelöst.
//   - Sonst fehlt sie und wird angelegt.
//   - Eine Sammlung, von der kein Film in Lumen ist, fällt weg.

import (
	"context"
	"net/url"
	"sort"
	"strings"
	"unicode"
)

// EmbyReihe ist eine Sammlung (BoxSet) mit ihren Filmen.
type EmbyReihe struct {
	ID        string
	Name      string
	Kennungen map[string]string
	Filme     []Eintrag
}

// Reihen holt alle Sammlungen dieses Benutzers samt ihren Filmen.
// fertig meldet, wie viele Einträge schon gelesen sind.
func (s *Server) Reihen(ctx context.Context, fertig func(int)) ([]EmbyReihe, error) {
	if s.token == "" {
		return nil, ErrAnmeldung
	}
	gelesen := 0
	zaehlen := func(n int) {
		gelesen += n
		if fertig != nil {
			fertig(gelesen)
		}
	}
	boxen, err := s.titelHolen(ctx, "BoxSet", "", zaehlen)
	if err != nil {
		return nil, err
	}
	out := make([]EmbyReihe, 0, len(boxen))
	for _, b := range boxen {
		if b.ID == "" {
			continue
		}
		filme, err := s.titelHolenMit(ctx, "Movie", "", url.Values{"ParentId": {b.ID}}, zaehlen)
		if err != nil {
			return nil, err
		}
		out = append(out, EmbyReihe{ID: b.ID, Name: strings.TrimSpace(b.Name), Kennungen: b.Kennungen, Filme: filme})
	}
	return out, nil
}

// LumenReihe ist eine Reihe, wie Lumen sie schon hat.
type LumenReihe struct {
	ID      string
	Name    string
	TMDb    string
	Quelle  string // tmdb | nfo | auto | emby
	Hidden  bool
	ItemIDs []string
}

// ReihenVorhaben: eine Reihe, die angelegt oder ergänzt wird.
type ReihenVorhaben struct {
	Name string `json:"name"`
	// LumenID: die Reihe in Lumen, die ergänzt wird; leer = neu anlegen.
	LumenID string `json:"lumenId,omitempty"`
	// LumenQuelle der ergänzten Reihe.
	LumenQuelle string `json:"-"`
	TMDb        string `json:"-"`
	// Filme: alle zugeordneten Filme der Emby-Sammlung (Lumen-IDs).
	Filme []string `json:"-"`
	// Neu: die Filme, die der Lumen-Reihe fehlen.
	Neu    []string `json:"-"`
	Anzahl int      `json:"anzahl"` // Filme in Lumen
	Dazu   int      `json:"dazu"`   // davon neu in der Reihe
}

// ReihenPlan sagt, was das Übernehmen der Reihen ändern würde.
type ReihenPlan struct {
	Fehlen     []ReihenVorhaben `json:"fehlen"`
	Ergaenzen  []ReihenVorhaben `json:"ergaenzen"`
	SchonDa    int              `json:"schonDa"`
	Aufgeloest int              `json:"aufgeloest"`
	OhneFilme  int              `json:"ohneFilme"`
	// EinFilm: nur ein Film davon ist in Lumen — eine Reihe zeigt Lumen
	// erst ab zwei Filmen, darum legt es keine an.
	EinFilm int `json:"einFilm"`
	Gesamt  int `json:"gesamt"`
}

// reihenWorte fallen beim Vergleich der Namen weg: „Matrix Collection",
// „Matrix Filmreihe" und „Matrix" sind dieselbe Reihe.
var reihenWorte = map[string]bool{"collection": true, "sammlung": true, "filmreihe": true,
	"reihe": true, "kollektion": true, "filmsammlung": true, "the": true, "der": true, "die": true,
	"das": true, "saga": true, "trilogie": true, "trilogy": true, "films": true, "filme": true}

// ReihenName macht einen Namen vergleichbar.
func ReihenName(name string) string {
	var teile []string
	for _, w := range strings.FieldsFunc(strings.ToLower(name), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if w = normal(w); w != "" && !reihenWorte[w] {
			teile = append(teile, w)
		}
	}
	return strings.Join(teile, " ")
}

// ReihenPlanen vergleicht die Sammlungen aus Emby mit den Reihen in Lumen.
// ix enthält nur die Filme, die angefasst werden dürfen.
func ReihenPlanen(emby []EmbyReihe, ix *Index, lumen []LumenReihe) ReihenPlan {
	p := ReihenPlan{Fehlen: []ReihenVorhaben{}, Ergaenzen: []ReihenVorhaben{}, Gesamt: len(emby)}
	nachTMDb := map[string]*LumenReihe{}
	nachName := map[string]*LumenReihe{}
	reiheVon := map[string][]*LumenReihe{} // Film → Reihen
	for i := range lumen {
		r := &lumen[i]
		if r.TMDb != "" {
			nachTMDb[r.TMDb] = r
		}
		if n := ReihenName(r.Name); n != "" {
			if alt := nachName[n]; alt == nil || len(r.ItemIDs) > len(alt.ItemIDs) {
				nachName[n] = r
			}
		}
		for _, id := range r.ItemIDs {
			reiheVon[id] = append(reiheVon[id], r)
		}
	}
	// Zwei Emby-Sammlungen, die auf dieselbe neue Reihe hinauslaufen
	// (gleicher Name), werden eine.
	neuNachName := map[string]int{}
	for _, e := range emby {
		gesehen := map[string]bool{}
		var filme []string
		for _, f := range e.Filme {
			id, _ := ix.Zuordnen(f, nil)
			if id == "" || gesehen[id] {
				continue
			}
			if t := ix.Titel(id); t == nil || t.Kind != "movie" {
				continue
			}
			gesehen[id] = true
			filme = append(filme, id)
		}
		if len(filme) == 0 {
			p.OhneFilme++
			continue
		}
		tmdb := e.Kennungen["tmdb"]
		var treffer *LumenReihe
		if tmdb != "" {
			treffer = nachTMDb[tmdb]
		}
		if treffer == nil {
			treffer = nachName[ReihenName(e.Name)]
		}
		if treffer == nil {
			// Dieselben Filme: Die Reihe, die die meisten davon enthält —
			// mindestens zwei und mindestens die Hälfte.
			zahl := map[*LumenReihe]int{}
			for _, id := range filme {
				for _, r := range reiheVon[id] {
					zahl[r]++
				}
			}
			best := 0
			for r, n := range zahl {
				if n > best || (n == best && treffer != nil && r.ID < treffer.ID) {
					treffer, best = r, n
				}
			}
			if best < 2 || best*2 < len(filme) {
				treffer = nil
			}
		}
		if treffer != nil {
			if treffer.Hidden {
				p.Aufgeloest++
				continue
			}
			drin := map[string]bool{}
			for _, id := range treffer.ItemIDs {
				drin[id] = true
			}
			var neu []string
			for _, id := range filme {
				if !drin[id] {
					neu = append(neu, id)
				}
			}
			if len(neu) == 0 {
				p.SchonDa++
				continue
			}
			p.Ergaenzen = append(p.Ergaenzen, ReihenVorhaben{Name: treffer.Name, LumenID: treffer.ID,
				LumenQuelle: treffer.Quelle, TMDb: tmdb, Filme: filme, Neu: neu,
				Anzahl: len(filme), Dazu: len(neu)})
			continue
		}
		schluessel := ReihenName(e.Name)
		if schluessel == "" {
			schluessel = normal(e.Name)
		}
		if i, da := neuNachName[schluessel]; da {
			v := &p.Fehlen[i]
			for _, id := range filme {
				if !enthaelt(v.Filme, id) {
					v.Filme = append(v.Filme, id)
				}
			}
			v.Neu, v.Anzahl, v.Dazu = v.Filme, len(v.Filme), len(v.Filme)
			continue
		}
		if len(filme) < 2 {
			p.EinFilm++
			continue
		}
		neuNachName[schluessel] = len(p.Fehlen)
		name := e.Name
		if name == "" {
			name = "Sammlung"
		}
		p.Fehlen = append(p.Fehlen, ReihenVorhaben{Name: name, TMDb: tmdb, Filme: filme, Neu: filme,
			Anzahl: len(filme), Dazu: len(filme)})
	}
	sort.SliceStable(p.Fehlen, func(i, j int) bool { return normal(p.Fehlen[i].Name) < normal(p.Fehlen[j].Name) })
	sort.SliceStable(p.Ergaenzen, func(i, j int) bool { return normal(p.Ergaenzen[i].Name) < normal(p.Ergaenzen[j].Name) })
	return p
}
