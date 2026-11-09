// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

// Störungen beim Abspielen, ohne Verlauf.
//
// „Umwandlung gescheitert", „Strom abgebrochen" — das muss der Besitzer
// erfahren, um den Server zu pflegen. Aber jede einzelne Zeile ist ein
// Wiedergabe-Ereignis: Im Protokoll, das auch Mitverwalter lesen, stand
// damit, dass um diese Zeit jemand geschaut hat, und neben den Zeilen mit
// Uhrzeit liess sich die Reihenfolge ablesen.
//
// Darum landen Zeilen aus dem Bereich „stream" nicht einzeln im Protokoll.
// Lumen zählt sie nur im Speicher, je Text und Tag, und schreibt beim
// ersten Eintrag eines neuen Tages eine Zusammenfassung für den Vortag:
// „Umwandlung gescheitert — an diesem Tag 3-mal". Ohne Uhrzeit, ohne
// Reihenfolge, ohne Titel und ohne Konto.

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

type stoerungsZaehler struct {
	mu     sync.Mutex
	tag    int64
	zahlen map[string]int
}

// zaehlen merkt sich eine Störung. Ist inzwischen ein neuer Tag, kommt
// zuerst zurück, was vom alten Tag zu schreiben ist.
func (z *stoerungsZaehler) zaehlen(heute int64, text string) (tag int64, alt map[string]int) {
	z.mu.Lock()
	defer z.mu.Unlock()
	tag, alt = z.abschliessen(heute)
	if z.zahlen == nil {
		z.zahlen = map[string]int{}
	}
	z.tag = heute
	z.zahlen[text]++
	return tag, alt
}

// abschliessen gibt die Zahlen eines vergangenen Tages heraus (unter z.mu).
func (z *stoerungsZaehler) abschliessen(heute int64) (int64, map[string]int) {
	if len(z.zahlen) == 0 || z.tag == heute {
		return 0, nil
	}
	tag, alt := z.tag, z.zahlen
	z.zahlen = map[string]int{}
	return tag, alt
}

// alles gibt alle Zahlen heraus — beim Beenden, damit nichts verloren geht.
func (z *stoerungsZaehler) alles() (int64, map[string]int) {
	z.mu.Lock()
	defer z.mu.Unlock()
	tag, alt := z.tag, z.zahlen
	z.zahlen = nil
	return tag, alt
}

func (s *Store) stoerungenSchreiben(ctx context.Context, tag int64, zahlen map[string]int) {
	texte := make([]string, 0, len(zahlen))
	for t := range zahlen {
		texte = append(texte, t)
	}
	sort.Strings(texte)
	for _, t := range texte {
		zeile := t
		if n := zahlen[t]; n > 1 {
			zeile = fmt.Sprintf("%s — an diesem Tag %d-mal", t, n)
		}
		_, _ = s.db.ExecContext(ctx,
			`INSERT INTO log_line (at, level, area, message) VALUES (?, 'warn', 'stream', ?)`, tag, zeile)
	}
}

// stoerungZaehlen ist der Weg von Log für den Bereich „stream".
func (s *Store) stoerungZaehlen(ctx context.Context, text string) {
	if tag, alt := s.stoerungen.zaehlen(heute(), text); alt != nil {
		s.stoerungenSchreiben(ctx, tag, alt)
	}
}

// stoerungenAbschliessen schreibt den Vortag, falls noch offen.
func (s *Store) stoerungenAbschliessen(ctx context.Context) {
	s.stoerungen.mu.Lock()
	tag, alt := s.stoerungen.abschliessen(heute())
	s.stoerungen.mu.Unlock()
	if alt != nil {
		s.stoerungenSchreiben(ctx, tag, alt)
	}
}
