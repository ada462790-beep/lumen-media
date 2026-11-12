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
	"strings"
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
func (s *Store) Kennzahlen(ctx context.Context, ids []string) (map[string]Kennzahl, error) {
	out := map[string]Kennzahl{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := s.db.QueryContext(ctx, kennzahlenSQL(len(ids)), kennzahlenArgs(ids)...)
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
		out[id] = k
	}
	return out, rows.Err()
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
	args := append([]any{seit.Unix(), accountID}, toAny(seriesIDs)...)
	rows, err := s.db.QueryContext(ctx,
		`SELECT st.parent_id, count(*) FROM item e JOIN item st ON st.id = e.parent_id
		  WHERE e.kind = 'episode' AND e.added_at > ?
		    AND NOT EXISTS (SELECT 1 FROM progress p WHERE p.account_id = ? AND p.item_id = e.id AND p.finished = 1)
		    AND st.parent_id IN (`+placeholders(len(seriesIDs))+`)
		  GROUP BY st.parent_id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}

// kennzahlenSQL: drei Zweige mit UNION ALL (der Titel selbst, seine
// Kinder, seine Enkel) statt eines Joins mit „e.id = t.id OR e.parent_id
// = t.id OR e.parent_id IN (…)". Mit dem OR hing der Plan an der
// Statistik: Nach „erst Filme, dann Serien" hielt sie parent_id für
// wertlos (beim ersten ANALYZE war es überall NULL), und SQLite las für
// jede Datei einer Folge die ganze Tabelle item — die Startseite brauchte
// 245 s. INDEXED BY und CROSS JOIN (z zuerst, dann die Dateien) legen
// die Wege fest, unabhängig von der Statistik.
func kennzahlenSQL(n int) string {
	ph := placeholders(n)
	return `WITH z(t, e) AS (
		     SELECT id, id FROM item WHERE id IN (` + ph + `)
		     UNION ALL
		     SELECT parent_id, id FROM item INDEXED BY item_by_parent WHERE parent_id IN (` + ph + `)
		     UNION ALL
		     SELECT st.parent_id, e.id FROM item st INDEXED BY item_by_parent
		       JOIN item e INDEXED BY item_by_parent ON e.parent_id = st.id
		      WHERE st.parent_id IN (` + ph + `))
		 SELECT z.t, MAX(COALESCE(f.height, 0)), MAX(COALESCE(f.width, 0)), MAX(COALESCE(f.hdr, '')),
		        MAX(COALESCE((SELECT MAX(channels) FROM media_track mt WHERE mt.file_id = f.id AND mt.kind = 'audio'), 0))
		   FROM z CROSS JOIN media_file f INDEXED BY file_by_item
		  WHERE f.item_id = z.e AND f.missing_since IS NULL
		  GROUP BY z.t`
}

func kennzahlenArgs(ids []string) []any {
	return append(append(toAny(ids), toAny(ids)...), toAny(ids)...)
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
	// und wachsend mit der Sammlung). ?1 ist der Titel; die Platzhalter
	// der Rechte (bedingung) zählen danach weiter.
	args = append(append([]any{itemID}, args...), limit)
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
		 SELECT i.id, sum(k.p) + CASE WHEN i.kind = (SELECT kind FROM item WHERE id = ?1) THEN 1 ELSE 0 END AS punkte
		   FROM k CROSS JOIN item i
		  WHERE i.id = k.id AND `+strings.Join(where, " AND ")+`
		  GROUP BY i.id HAVING punkte > 2
		  ORDER BY punkte DESC, i.imdb_rating DESC, i.sort_title LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		var punkte int
		if err := rows.Scan(&id, &punkte); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
