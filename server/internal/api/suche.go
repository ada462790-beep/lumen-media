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
		ok(w, map[string]any{"items": []any{}, "personen": []any{}, "genres": []any{},
			"reihen": []any{}, "sammlungen": []any{}})
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
	reihen, sammlungen := s.reihenUndSammlungenSuchen(r, eingabe)

	// Folgen, deren Titel passt — „die Folge mit dem Bunker". Nur für
	// Text ab drei Zeichen und nur, wenn nicht ausdrücklich nach einer Art
	// gefragt wurde; was das Profil nicht sehen darf, bleibt auch hier weg
	// (scope gilt für Folgen über ihre Serie).
	folgen := []view{}
	if d.Text != "" && len([]rune(d.Text)) >= 3 && r.URL.Query().Get("kind") == "" {
		fq := q
		fq.Text, fq.JahrVon, fq.JahrBis = d.Text, d.JahrVon, d.JahrBis
		fq.Kinds, fq.Limit, fq.Sort = []string{"episode"}, 12, "relevanz"
		if its, err := s.st.Query(r.Context(), fq); err == nil && len(its) > 0 {
			if v, err := s.decorate(r, its); err == nil {
				folgen = v
			}
		}
	}
	ok(w, map[string]any{
		"folgen":     folgen,
		"items":      out,
		"personen":   personen,
		"genres":     passende,
		"reihen":     reihen,
		"sammlungen": sammlungen,
		"deutung":    d,
		"meintest":   meintest,
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

// suchPasst: Steckt die Eingabe im Namen? Ohne Gross/klein, Umlaute und
// Akzente; bei mehreren Wörtern muss jedes vorkommen („potter harry").
func suchPasst(name, eingabe string) bool {
	n := store.SuchFalten(name)
	for _, w := range strings.Fields(store.SuchFalten(eingabe)) {
		if !strings.Contains(n, w) {
			return false
		}
	}
	return strings.TrimSpace(eingabe) != ""
}

// reihenUndSammlungenSuchen: Filmreihen und eigene Sammlungen, deren Name
// zur Eingabe passt — mit denselben Sichtbarkeitsregeln wie ihre Seiten
// (eine Reihe ab zwei sichtbaren Filmen, eine Sammlung nicht leer).
func (s *Server) reihenUndSammlungenSuchen(r *http.Request, eingabe string) ([]reiheAntwort, []sammlungAntwort) {
	const hoechstens = 12
	reihen := []reiheAntwort{}
	// Ein einzelner Buchstabe passt auf fast jede Reihe — bei jedem
	// Tastendruck alle prüfen lohnt nicht.
	if len([]rune(strings.TrimSpace(eingabe))) < 2 {
		return reihen, []sammlungAntwort{}
	}
	if alle, err := s.st.Reihen(r.Context(), false); err == nil {
		for i := range alle {
			x := &alle[i]
			if !suchPasst(x.Name, eingabe) {
				continue
			}
			items, err := s.sichtbar(r, x.ItemIDs)
			if err != nil || len(items) < reiheMindestens {
				continue
			}
			a := reiheAntwort{ID: x.ID, Name: x.Name, Overview: x.Overview, Source: x.Source,
				Anzahl: len(items), Items: items[:min(len(items), 3)]}
			reiheBilder(x, &a)
			reihen = append(reihen, a)
			if len(reihen) >= hoechstens {
				break
			}
		}
	}
	sammlungen := []sammlungAntwort{}
	if alle, err := s.st.Sammlungen(r.Context()); err == nil {
		for i := range alle {
			if !suchPasst(alle[i].Name, eingabe) {
				continue
			}
			items, err := s.sammlungInhalt(r, &alle[i])
			if err != nil || len(items) == 0 {
				continue
			}
			a := s.sammlungAntwortBauen(r, &alle[i], items, false)
			a.VonHand = nil // nur für die Bearbeitung nötig, nicht in Treffern
			sammlungen = append(sammlungen, a)
			if len(sammlungen) >= hoechstens {
				break
			}
		}
	}
	return reihen, sammlungen
}
