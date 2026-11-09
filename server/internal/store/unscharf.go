// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

// Unscharfe Suche: Wer „Sone" tippt, meint fast immer „Sonne", und wer
// „Apokalypse" schreibt, sucht „Apocalypse Now". Findet die genaue Suche
// nichts, vergleicht Lumen jedes Wort der Eingabe mit den Wörtern aus den
// Titeln, die das Profil ohnehin sehen darf, und schlägt die nächste
// Schreibweise vor („Meintest du …?").
//
// Datenschutzneutral: Verglichen wird nur mit Titeln der Mediathek. Die
// Eingabe wird nirgends gespeichert, und kein anderes Profil erfährt davon.

import (
	"context"
	"strings"
	"unicode"
)

// TitelWoerter nennt alle Wörter aus Titel und Originaltitel der Titel,
// die die Abfrage sehen darf — klein geschrieben, ohne Satzzeichen, jedes
// nur einmal. Mehr als ein paar zehntausend werden es in einem Haushalt
// nicht; der Vergleich läuft nur, wenn die genaue Suche leer blieb.
func (s *Store) TitelWoerter(ctx context.Context, q Query) ([]string, error) {
	q.Text, q.Person, q.Genre, q.JahrVon, q.JahrBis, q.Buchstabe = "", "", "", 0, 0, ""
	where, args, ok := bedingung(q)
	if !ok {
		return []string{}, nil
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT i.title, COALESCE(i.original_title, '') FROM item i
		  WHERE `+strings.Join(where, " AND ")+` LIMIT 50000`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	gesehen := map[string]bool{}
	out := []string{}
	for rows.Next() {
		var t, o string
		if err := rows.Scan(&t, &o); err != nil {
			return nil, err
		}
		for _, w := range suchWoerter(t + " " + o) {
			if !gesehen[w] {
				gesehen[w] = true
				out = append(out, w)
			}
		}
	}
	return out, rows.Err()
}

// suchWoerter zerlegt einen Text in klein geschriebene Wörter ohne
// Satzzeichen. Ein- und zweibuchstabige Wörter („a", „of") taugen nicht
// als Vorschlag und bleiben weg.
func suchWoerter(text string) []string {
	felder := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	out := felder[:0]
	for _, f := range felder {
		if len([]rune(f)) >= 3 {
			out = append(out, f)
		}
	}
	return out
}

// erlaubterAbstand: wie viele Tippfehler ein Wort haben darf, um noch als
// gemeint zu gelten. Kurze Wörter vertragen wenig — aus „Ton" soll nicht
// „Tod" werden —, lange etwas mehr.
func erlaubterAbstand(laenge int) int {
	switch {
	case laenge <= 3:
		return 0
	case laenge <= 5:
		return 1
	case laenge <= 9:
		return 2
	default:
		return 3
	}
}

// Verbessern schlägt für eine Eingabe die nächste Schreibweise vor. Jedes
// Wort, das in keinem Titelwort vorkommt, wird durch das ähnlichste
// Titelwort ersetzt, sofern es nah genug liegt. Gibt false zurück, wenn
// nichts zu verbessern war.
func Verbessern(eingabe string, woerter []string) (string, bool) {
	teile := strings.Fields(strings.TrimSpace(eingabe))
	if len(teile) == 0 || len(woerter) == 0 {
		return "", false
	}
	// Auch hier eine Grenze, nicht nur in der API: Jedes Wort wird mit
	// jedem Titelwort verglichen, der Aufwand wächst mit Länge × Länge.
	if len(teile) > 8 {
		teile = teile[:8]
	}
	geaendert := false
	for i, t := range teile {
		klein := strings.ToLower(strings.Trim(t, ",.;:!?\"'„“”"))
		r := []rune(klein)
		if len(r) < 3 || len(r) > 40 {
			// Länger als 40 Zeichen ist kein Wort, das jemand vertippt hat.
			continue
		}
		passt := false
		for _, w := range woerter {
			if strings.Contains(w, klein) || strings.Contains(fold(w), fold(klein)) {
				passt = true
				break
			}
		}
		if passt {
			continue
		}
		bestes, bester := "", erlaubterAbstand(len(r))+1
		for _, w := range woerter {
			// Ein Wort, das viel länger oder kürzer ist, kann nicht gemeint sein —
			// und das spart den teuren Vergleich.
			if d := len([]rune(w)) - len(r); d > bester || -d > bester {
				continue
			}
			if a := abstand(fold(klein), fold(w)); a < bester || (a == bester && bestes != "" && w < bestes) {
				bestes, bester = w, a
			}
		}
		if bestes != "" && bester <= erlaubterAbstand(len(r)) {
			teile[i] = grossWieVorlage(bestes, t)
			geaendert = true
		}
	}
	if !geaendert {
		return "", false
	}
	return strings.Join(teile, " "), true
}

// grossWieVorlage übernimmt den grossen Anfangsbuchstaben der Eingabe —
// „Sone" wird zu „Sonne", „sone" zu „sonne".
func grossWieVorlage(wort, vorlage string) string {
	v := []rune(vorlage)
	if len(v) == 0 || !unicode.IsUpper(v[0]) {
		return wort
	}
	w := []rune(wort)
	w[0] = unicode.ToUpper(w[0])
	return string(w)
}

// abstand ist die Levenshtein-Distanz zweier Wörter: wie viele Zeichen
// man einfügen, löschen oder ersetzen muss, um vom einen zum anderen zu
// kommen. Zwei vertauschte Nachbarbuchstaben zählen als ein Fehler —
// „Snone" ist ein Tippfehler, keine zwei.
func abstand(a, b string) int {
	x, y := []rune(a), []rune(b)
	vorvorher := make([]int, len(y)+1)
	vorher := make([]int, len(y)+1)
	jetzt := make([]int, len(y)+1)
	for j := range vorher {
		vorher[j] = j
	}
	for i := 1; i <= len(x); i++ {
		jetzt[0] = i
		for j := 1; j <= len(y); j++ {
			kosten := 1
			if x[i-1] == y[j-1] {
				kosten = 0
			}
			jetzt[j] = min(vorher[j]+1, jetzt[j-1]+1, vorher[j-1]+kosten)
			if i > 1 && j > 1 && x[i-1] == y[j-2] && x[i-2] == y[j-1] {
				jetzt[j] = min(jetzt[j], vorvorher[j-2]+1)
			}
		}
		vorvorher, vorher, jetzt = vorher, jetzt, vorvorher
	}
	return vorher[len(y)]
}
