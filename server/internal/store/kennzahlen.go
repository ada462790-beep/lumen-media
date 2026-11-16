// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

// Kennzahlen für die Abzeichen auf den Karten und die Technik-Zeile:
// beste Auflösung und HDR einer Datei, und ob eine Serie frische Folgen
// hat. Alles aus öffentlichen Tabellen — nichts davon hängt an einem Konto.

import (
	"context"
	"database/sql"
	"sort"
	"strings"
	"sync"
	"time"
)

type Kennzahl struct {
	Height   int    `json:"height,omitempty"`
	Width    int    `json:"width,omitempty"`
	HDR      string `json:"hdr,omitempty"`
	Channels int    `json:"channels,omitempty"` // meiste Kanäle einer Tonspur
}

// Kennzahlen liefert je Titel die beste Datei: bei Filmen die eigene, bei
// Serien die ihrer Folgen.
//
// Für eine Serie heisst das: alle Dateien aller Folgen ansehen. Die
// Startseite fragt das bei jedem Aufruf für Dutzende Serien — darum
// bleiben die Ergebnisse im Speicher, bis sich in der Mediathek etwas
// ändert (StandMediathek — ein gespeicherter Stand beim Schauen zählt
// nicht). Auch Titel ohne Datei werden gemerkt.
func (s *Store) Kennzahlen(ctx context.Context, ids []string) (map[string]Kennzahl, error) {
	return s.kennzahlenRechnen(ctx, ids, func(string) bool { return true })
}

// KennzahlenFuer wie Kennzahlen, aber nur Serien und Staffeln sehen bei
// ihren Kindern nach: Ein Film hat keine Folgen, und die Suche danach
// kostete bei 800 Filmen 1600 Sprünge in item_by_parent.
func (s *Store) KennzahlenFuer(ctx context.Context, items []Item) (map[string]Kennzahl, error) {
	ids := make([]string, len(items))
	kinder := map[string]bool{}
	for i, it := range items {
		ids[i] = it.ID
		if it.Kind == "series" || it.Kind == "season" {
			kinder[it.ID] = true
		}
	}
	return s.kennzahlenRechnen(ctx, ids, func(id string) bool { return kinder[id] })
}

func (s *Store) kennzahlenRechnen(ctx context.Context, ids []string, mitKindern func(string) bool) (map[string]Kennzahl, error) {
	out := map[string]Kennzahl{}
	if len(ids) == 0 {
		return out, nil
	}
	stand := StandMediathek()
	fehlt := s.kennzahlenAusSpeicher(stand, ids, out)
	if len(fehlt) == 0 {
		return out, nil
	}
	var eltern []string
	for _, id := range fehlt {
		if mitKindern(id) {
			eltern = append(eltern, id)
		}
	}
	neu := map[string]Kennzahl{}
	rows, err := s.db.QueryContext(ctx, kennzahlenSQL(len(fehlt), len(eltern)), kennzahlenArgs(fehlt, eltern)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var k Kennzahl
		if err := rows.Scan(&id, &k.Height, &k.Width, &k.HDR, &k.Channels); err != nil {
			return nil, err
		}
		neu[id] = k
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for id, k := range neu {
		out[id] = k
	}
	s.kennzahlenMerken(stand, fehlt, neu)
	return out, nil
}

// kennzahlenSpeicher: Kennzahlen je Titel, gültig für einen Stand.
type kennzahlenSpeicher struct {
	mu    sync.Mutex
	stand int64
	m     map[string]Kennzahl // ohne Datei: die leere Kennzahl
}

// kennzahlenAusSpeicher füllt out mit dem Gemerkten und gibt zurück, was
// noch fehlt.
func (s *Store) kennzahlenAusSpeicher(stand int64, ids []string, out map[string]Kennzahl) []string {
	k := &s.kennzahlen
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.stand != stand || k.m == nil {
		return ids
	}
	var fehlt []string
	for _, id := range ids {
		v, ok := k.m[id]
		switch {
		case !ok:
			fehlt = append(fehlt, id)
		case v != (Kennzahl{}):
			out[id] = v
		}
	}
	return fehlt
}

func (s *Store) kennzahlenMerken(stand int64, gefragt []string, neu map[string]Kennzahl) {
	k := &s.kennzahlen
	k.mu.Lock()
	defer k.mu.Unlock()
	// Erst gerechnet, dann gemerkt: Hat sich der Stand inzwischen
	// geändert, passt das Ergebnis nicht mehr.
	if StandMediathek() != stand {
		return
	}
	if k.stand != stand || k.m == nil || len(k.m) > 50000 {
		k.stand, k.m = stand, map[string]Kennzahl{}
	}
	for _, id := range gefragt {
		k.m[id] = neu[id]
	}
}

// NeueFolgenSeit nennt die Serien, zu denen seit dem Zeitpunkt Folgen
// dazugekommen sind — und wie viele.
//
// Gezählt wird nur, was dieses Profil (accountID) noch nicht gesehen hat:
// „5 neue Folgen" an einer Serie, deren neue Folgen man längst geschaut
// hat, wäre falsch. Leere accountID zählt alle.
func (s *Store) NeueFolgenSeit(ctx context.Context, accountID string, seriesIDs []string, seit time.Time) (map[string]int, error) {
	out := map[string]int{}
	if len(seriesIDs) == 0 {
		return out, nil
	}
	neu, err := s.neueFolgen(ctx, seriesIDs, seit)
	if err != nil {
		return nil, err
	}
	var a *abbild
	if accountID != "" {
		// Was dieses Profil schon gesehen hat, aus seinem Stand im
		// Speicher (fortschritt_abbild.go) — nicht je Folge in progress.
		if a, err = s.fortschrittAbbild(ctx, accountID); err != nil {
			return nil, err
		}
	}
	for _, f := range neu {
		if a != nil {
			if z, ok := a.zeile(f.id); ok && z.fertig {
				continue
			}
		}
		out[f.serie]++
	}
	return out, nil
}

// neueFolge: eine Folge, jünger als ein Zeitpunkt.
type neueFolge struct {
	serie, id string
	neu       int64
}

// neueFolgen: die Folgen dieser Serien, die nach seit dazukamen — über
// item_by_parent (Serie → Staffeln → Folgen) ohne die Zeilen selbst.
func (s *Store) neueFolgen(ctx context.Context, seriesIDs []string, seit time.Time) ([]neueFolge, error) {
	alle, err := s.serienFolgenKurz(ctx, seriesIDs)
	if err != nil {
		return nil, err
	}
	var out []neueFolge
	for _, serie := range seriesIDs {
		for _, f := range alle[serie] {
			if f.neu > seit.Unix() {
				out = append(out, neueFolge{serie: serie, id: f.id, neu: f.neu})
			}
		}
	}
	return out, nil
}

// kennzahlenSQL: drei Zweige mit UNION ALL (der Titel selbst, seine
// Kinder, seine Enkel) statt eines Joins mit „e.id = t.id OR e.parent_id
// = t.id OR e.parent_id IN (…)". Mit dem OR hing der Plan an der
// Statistik: Nach „erst Filme, dann Serien" hielt sie parent_id für
// wertlos (beim ersten ANALYZE war es überall NULL), und SQLite las für
// jede Datei einer Folge die ganze Tabelle item — die Startseite brauchte
// 245 s. INDEXED BY und CROSS JOIN (z zuerst, dann die Dateien) legen
// die Wege fest, unabhängig von der Statistik — auch zwischen Staffel und
// Folge: Seit item_by_parent die Kennung mitführt (schmal.go), hätte SQLite
// mit veralteter Statistik sonst alle Folgen durchgesehen und die Staffel
// dazu gesucht.
func kennzahlenSQL(n, eltern int) string {
	pe := "NULL"
	if eltern > 0 {
		pe = placeholders(eltern)
	}
	// Der erste Zweig liest die Kennungen nicht aus item (sie kommen ja
	// mit): Eine Kennung ohne Titel hat auch keine Datei.
	werte := strings.TrimSuffix(strings.Repeat("(?),", n), ",")
	return `WITH z(t, e) AS (
		     SELECT column1, column1 FROM (VALUES ` + werte + `)
		     UNION ALL
		     SELECT parent_id, id FROM item INDEXED BY item_by_parent WHERE parent_id IN (` + pe + `)
		     UNION ALL
		     SELECT st.parent_id, e.id FROM item st INDEXED BY item_by_parent
		       CROSS JOIN item e INDEXED BY item_by_parent ON e.parent_id = st.id
		      WHERE st.parent_id IN (` + pe + `))
		 SELECT z.t, MAX(COALESCE(f.height, 0)), MAX(COALESCE(f.width, 0)), MAX(COALESCE(f.hdr, '')),
		        MAX(COALESCE(f.ton_kanaele, 0))
		   FROM z CROSS JOIN media_file f INDEXED BY file_kennzahl
		  WHERE f.item_id = z.e AND f.missing_since IS NULL
		  GROUP BY z.t`
}

func kennzahlenArgs(ids, eltern []string) []any {
	return append(append(toAny(ids), toAny(eltern)...), toAny(eltern)...)
}

// Qualitaet macht aus Breite und Höhe ein Wort fürs Abzeichen.
//
// Die Breite zählt mit: Kinofilme im Breitformat sind niedriger als ihre
// Stufe — ein 1080p-Film in 2,39:1 hat 1920×800, ein 4K-Film 3840×1600.
// Nach der Höhe allein wäre der eine „SD", der andere „HD". Und 720p
// (1280×720) ist HD, nicht SD.
func Qualitaet(width, height int) string {
	switch {
	case height >= 2000 || width >= 3400:
		return "4K"
	case height >= 700 || width >= 1200:
		return "HD"
	case height > 0 || width > 0:
		return "SD"
	}
	return ""
}

// Aehnliche findet Titel, die zu einem passen: dieselbe Reihe zuerst,
// dann geteilte Besetzung und Genres. Nur innerhalb dessen, was das
// Konto sehen darf (q). Zurück kommen die Kennungen, beste zuerst.
func (s *Store) Aehnliche(ctx context.Context, q Query, itemID string, limit int) ([]string, error) {
	where, args, ok := bedingung(q)
	if !ok {
		return []string{}, nil
	}
	if limit <= 0 {
		limit = 20
	}
	// Kandidaten nur von den eigenen Reihen, Personen und Genres aus
	// (Index je Name) — vorher rechnete die Abfrage für jeden Titel der
	// Mediathek drei Unterabfragen (6 → 82 ms je Titelseite mit Besetzung,
	// und wachsend mit der Sammlung). Gezählt wird nur über die Indizes;
	// die Zeilen der Kandidaten (oft Tausende über ein gemeinsames Genre)
	// werden nicht gelesen.
	rows, err := s.db.QueryContext(ctx,
		`WITH k(id, p) AS (
		     SELECT b.item_id, 10 FROM collection_item a
		       JOIN collection_item b ON b.collection_id = a.collection_id
		      WHERE a.item_id = ?1 AND b.item_id != ?1
		     UNION ALL
		     SELECT b.item_id, 3 FROM besetzung a
		       JOIN besetzung b INDEXED BY besetzung_by_name ON b.name = a.name AND b.ord < 8
		      WHERE a.item_id = ?1 AND a.ord < 8 AND b.item_id != ?1
		     UNION ALL
		     SELECT b.item_id, 2 FROM genre a
		       JOIN genre b INDEXED BY genre_by_name ON b.name = a.name
		      WHERE a.item_id = ?1 AND b.item_id != ?1)
		 SELECT id, sum(p) FROM k GROUP BY id`, itemID)
	if err != nil {
		return nil, err
	}
	punkte := map[string]int{}
	for rows.Next() {
		var id string
		var p int
		if err := rows.Scan(&id, &p); err != nil {
			rows.Close()
			return nil, err
		}
		punkte[id] = p
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(punkte) == 0 {
		return []string{}, nil
	}
	var eigeneArt string
	_ = s.db.QueryRowContext(ctx, `SELECT kind FROM item WHERE id = ?`, itemID).Scan(&eigeneArt)
	// Was das Konto sehen darf, samt Wertung und Sortiertitel — alles aus
	// dem Sortier-Index (schmal.go, sichtSpalten), ohne eine Zeile.
	vrows, err := s.db.QueryContext(ctx,
		`SELECT i.id, i.kind, i.imdb_rating, i.sort_title FROM item i INDEXED BY item_ord_wertung
		  WHERE `+strings.Join(where, " AND "), args...)
	if err != nil {
		return nil, err
	}
	type treffer struct {
		id, titel string
		punkte    int
		wertung   sql.NullFloat64
	}
	var alle []treffer
	for vrows.Next() {
		var t treffer
		var art string
		if err := vrows.Scan(&t.id, &art, &t.wertung, &t.titel); err != nil {
			vrows.Close()
			return nil, err
		}
		p, ok := punkte[t.id]
		if !ok {
			continue
		}
		if art == eigeneArt {
			p++
		}
		if t.punkte = p; p > 2 {
			alle = append(alle, t)
		}
	}
	vrows.Close()
	if err := vrows.Err(); err != nil {
		return nil, err
	}
	// Wie zuvor in SQL: Punkte, dann Wertung (ohne Wertung zuletzt), dann
	// Titel.
	sort.Slice(alle, func(i, j int) bool {
		a, b := alle[i], alle[j]
		if a.punkte != b.punkte {
			return a.punkte > b.punkte
		}
		if a.wertung.Valid != b.wertung.Valid {
			return a.wertung.Valid
		}
		if a.wertung.Float64 != b.wertung.Float64 {
			return a.wertung.Float64 > b.wertung.Float64
		}
		if a.titel != b.titel {
			return a.titel < b.titel
		}
		return a.id < b.id
	})
	out := make([]string, 0, min(limit, len(alle)))
	for _, t := range alle[:min(limit, len(alle))] {
		out = append(out, t.id)
	}
	return out, nil
}
