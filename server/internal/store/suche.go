// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

// Die Suche versteht mehr als Titel: „Filme mit Tom Hanks aus den 90ern",
// „Komödie 2019", „Thriller 80er". Lumen zerlegt die Eingabe in Jahr,
// Jahrzehnt, Genre und Person — und was übrig bleibt, sucht es im Titel.
// Daneben nennt sie Personen und Genres, die zur Eingabe passen, damit man
// mit einem Klick zu „alle Filme mit …" kommt.

import (
	"context"
	"regexp"
	"strconv"
	"strings"
)

// Deutung ist, was Lumen aus einer Eingabe gemacht hat.
type Deutung struct {
	Text    string `json:"text,omitempty"`
	Genre   string `json:"genre,omitempty"`
	Person  string `json:"person,omitempty"`
	JahrVon int    `json:"jahrVon,omitempty"`
	JahrBis int    `json:"jahrBis,omitempty"`
}

// PersonTreffer ist jemand aus der Besetzung, auf den die Suche passt.
type PersonTreffer struct {
	Name   string `json:"name"`
	Anzahl int    `json:"anzahl"`
	// ItemID und Bild: ein Titel, in dem die Person ein Bild hat — für
	// das Porträt in der Trefferliste.
	ItemID string `json:"itemId,omitempty"`
	Bild   string `json:"bild,omitempty"`
}

var (
	reJahr           = regexp.MustCompile(`^(19|20)\d\d$`)
	reJahrzehnt      = regexp.MustCompile(`^((?:19|20)?\d)0(?:er|ern|s|ies)?$`) // 90er, 1990er, 80s, 2000er
	suchFuellwoerter = map[string]bool{"film": true, "filme": true, "serie": true, "serien": true, "mit": true,
		"aus": true, "den": true, "der": true, "die": true, "das": true, "von": true, "und": true,
		"alle": true, "in": true, "im": true, "mit:": true}
)

// Deuten zerlegt die Eingabe. genres sind die bekannten Genres, personen
// die Namen aus der Besetzung — nur was es wirklich gibt, wird als Genre
// oder Person gedeutet; alles andere bleibt Titeltext.
func Deuten(eingabe string, genres []string, personen []string) Deutung {
	var d Deutung
	woerter := strings.Fields(strings.TrimSpace(eingabe))
	rest := []string{}
	for i := 0; i < len(woerter); i++ {
		w := strings.ToLower(strings.Trim(woerter[i], ",.;!?"))
		if reJahr.MatchString(w) {
			n, _ := strconv.Atoi(w)
			d.JahrVon, d.JahrBis = n, n
			continue
		}
		if m := reJahrzehnt.FindStringSubmatch(w); m != nil && (strings.HasSuffix(w, "er") || strings.HasSuffix(w, "ern") || strings.HasSuffix(w, "s") || strings.HasSuffix(w, "ies")) {
			n, _ := strconv.Atoi(m[1] + "0")
			if n < 100 {
				if n < 30 {
					n += 2000
				} else {
					n += 1900
				}
			}
			d.JahrVon, d.JahrBis = n, n+9
			continue
		}
		rest = append(rest, woerter[i])
	}

	// Person: der längste Name aus der Besetzung, der in der Eingabe steht
	// — als Folge ganzer Wörter, Gross/Klein egal.
	klein := make([]string, len(rest))
	for i, w := range rest {
		klein[i] = strings.ToLower(strings.Trim(w, ",.;!?"))
	}
	weg := make([]bool, len(rest))
	finde := func(name string) (int, int, bool) {
		teile := strings.Fields(strings.ToLower(name))
		if len(teile) == 0 {
			return 0, 0, false
		}
	aussen:
		for i := 0; i+len(teile) <= len(klein); i++ {
			for j, t := range teile {
				if weg[i+j] || klein[i+j] != t {
					continue aussen
				}
			}
			return i, i + len(teile), true
		}
		return 0, 0, false
	}
	bester, bVon, bBis := "", 0, 0
	for _, p := range personen {
		if len(p) < 3 {
			continue
		}
		if von, bis, ok := finde(p); ok && len(p) > len(bester) {
			bester, bVon, bBis = p, von, bis
		}
	}
	if bester != "" {
		d.Person = bester
		for i := bVon; i < bBis; i++ {
			weg[i] = true
		}
	}
	for _, g := range genres {
		if von, bis, ok := finde(g); ok {
			d.Genre = g
			for i := von; i < bis; i++ {
				weg[i] = true
			}
			break
		}
	}
	// Füllwörter nur weg, wenn etwas anderes gedeutet wurde — „Die Bucht"
	// soll „Die Bucht" bleiben.
	gedeutet := d.Genre != "" || d.Person != "" || d.JahrVon > 0
	uebrig := []string{}
	for i, w := range rest {
		if weg[i] || (gedeutet && suchFuellwoerter[klein[i]]) {
			continue
		}
		uebrig = append(uebrig, w)
	}
	d.Text = strings.Join(uebrig, " ")
	return d
}

// PersonenNamen nennt alle Namen aus der Besetzung sichtbarer Titel —
// fürs Deuten. Mehr als ein paar tausend werden es in einem Haushalt nicht.
func (s *Store) PersonenNamen(ctx context.Context, q Query) ([]string, error) {
	where, args, ok := bedingung(q)
	if !ok {
		return []string{}, nil
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT DISTINCT b.name FROM besetzung b JOIN item i ON i.id = b.item_id
		  WHERE `+strings.Join(where, " AND ")+` LIMIT 5000`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// PersonenSuchen findet Leute aus der Besetzung, deren Name die Eingabe
// enthält — mit der Zahl ihrer sichtbaren Titel.
func (s *Store) PersonenSuchen(ctx context.Context, q Query, text string, limit int) ([]PersonTreffer, error) {
	text = strings.ToLower(strings.TrimSpace(text))
	if len(text) < 2 {
		return []PersonTreffer{}, nil
	}
	where, args, ok := bedingung(q)
	if !ok {
		return []PersonTreffer{}, nil
	}
	if limit <= 0 {
		limit = 8
	}
	args = append(args, "%"+text+"%", limit)
	rows, err := s.db.QueryContext(ctx,
		`SELECT b.name, count(DISTINCT i.id),
		        COALESCE(MAX(CASE WHEN b.bild != '' THEN b.item_id END), ''),
		        COALESCE(MAX(CASE WHEN b.bild != '' THEN b.bild END), '')
		   FROM besetzung b JOIN item i ON i.id = b.item_id
		  WHERE `+strings.Join(where, " AND ")+` AND lower(b.name) LIKE ?
		  GROUP BY b.name ORDER BY count(DISTINCT i.id) DESC, b.name LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PersonTreffer{}
	for rows.Next() {
		var p PersonTreffer
		if err := rows.Scan(&p.Name, &p.Anzahl, &p.ItemID, &p.Bild); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// PersonBild nennt die Nummer des Bilds einer Person in einem Titel —
// die Bilderroute braucht sie (cast00, cast01, …).
func (s *Store) PersonBild(ctx context.Context, itemID, name string) int {
	var ord int
	if err := s.db.QueryRowContext(ctx,
		`SELECT ord FROM besetzung WHERE item_id = ? AND name = ? AND bild != '' LIMIT 1`, itemID, name).Scan(&ord); err != nil {
		return -1
	}
	return ord
}
