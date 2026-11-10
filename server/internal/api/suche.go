// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"fmt"
	"net/http"
	"strings"

	"lumen/internal/store"
)

// suche beantwortet eine Eingabe mit Titeln, Personen und Genres — und
// sagt, wie sie die Eingabe gedeutet hat („Komödie, 1990–1999").
func (s *Server) suche(w http.ResponseWriter, r *http.Request) {
	q, err := s.scope(r)
	if err != nil {
		failErr(w, err)
		return
	}
	eingabe := suchEingabe(r)
	if eingabe == "" {
		ok(w, map[string]any{"items": []any{}, "personen": []any{}, "genres": []any{}})
		return
	}
	genres, _ := s.st.AlleGenres(r.Context(), q)
	namen, _ := s.st.PersonenNamen(r.Context(), q)
	d := store.Deuten(eingabe, genres, namen)

	tq := q
	tq.Text, tq.Genre, tq.Person, tq.JahrVon, tq.JahrBis = d.Text, d.Genre, d.Person, d.JahrVon, d.JahrBis
	tq.Limit = min(max(qInt(r, "limit", 60), 1), 200)
	if d.Text != "" {
		// Genau der Titel zuerst, dann Titel, die so anfangen, dann der
		// Rest — „Matrix" vor „Animatrix".
		tq.Sort = "relevanz"
	}
	if k := r.URL.Query().Get("kind"); k != "" {
		tq.Kinds = strings.Split(k, ",")
	}
	items, err := s.st.Query(r.Context(), tq)
	if err != nil {
		failErr(w, err)
		return
	}
	// Nichts gefunden: vielleicht ein Tippfehler. Lumen sucht dann mit der
	// nächsten Schreibweise aus den Titeln, die dieses Profil sehen darf,
	// und sagt dazu, wonach („Meintest du …?").
	meintest := ""
	if len(items) == 0 && d.Text != "" {
		if woerter, err := s.st.TitelWoerter(r.Context(), tq); err == nil {
			if besser, ja := store.Verbessern(d.Text, woerter); ja {
				vq := tq
				vq.Text = besser
				if neu, err := s.st.Query(r.Context(), vq); err == nil && len(neu) > 0 {
					items = neu
					meintest = besser
					if strings.Contains(eingabe, d.Text) {
						meintest = strings.Replace(eingabe, d.Text, besser, 1)
					}
				}
			}
		}
	}
	out, err := s.decorate(r, items)
	if err != nil {
		failErr(w, err)
		return
	}

	// Personen, deren Name zur Eingabe passt — als Sprungbrett zu „alles mit …".
	gesucht := eingabe
	if d.Person != "" {
		gesucht = d.Person
	}
	personen, _ := s.st.PersonenSuchen(r.Context(), q, gesucht, 8)
	for i := range personen {
		p := &personen[i]
		if p.ItemID != "" {
			if ord := s.st.PersonBild(r.Context(), p.ItemID, p.Name); ord >= 0 {
				p.Bild = fmt.Sprintf("/api/v1/art/%s/cast%02d", p.ItemID, ord)
			} else {
				p.Bild = ""
			}
		}
	}
	// Genres, die zur Eingabe passen.
	passende := []string{}
	for _, g := range genres {
		if strings.Contains(strings.ToLower(g), strings.ToLower(eingabe)) || strings.EqualFold(g, d.Genre) {
			passende = append(passende, g)
		}
	}
	ok(w, map[string]any{
		"items":    out,
		"personen": personen,
		"genres":   passende,
		"deutung":  d,
		"meintest": meintest,
	})
}

// Grenzen für eine Sucheingabe. Die unscharfe Suche vergleicht jedes
// Wort der Eingabe mit jedem Titelwort der Mediathek (Levenshtein) — mit
// tausend Wörtern zu je tausend Zeichen beschäftigte eine einzige Anfrage
// den Server minutenlang. Niemand tippt mehr als ein paar Wörter.
const (
	suchZeichenHoechstens = 200
	suchWoerterHoechstens = 8
)

// suchEingabe liest ?q= und kürzt auf suchZeichenHoechstens Zeichen und
// suchWoerterHoechstens Wörter.
func suchEingabe(r *http.Request) string {
	return suchtextKuerzen(r.URL.Query().Get("q"))
}

// suchtextKuerzen: siehe suchEingabe.
func suchtextKuerzen(roh string) string {
	t := strings.TrimSpace(roh)
	if r := []rune(t); len(r) > suchZeichenHoechstens {
		t = string(r[:suchZeichenHoechstens])
	}
	if w := strings.Fields(t); len(w) > suchWoerterHoechstens {
		t = strings.Join(w[:suchWoerterHoechstens], " ")
	}
	return strings.TrimSpace(t)
}
