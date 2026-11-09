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
	rows, err := s.db.QueryContext(ctx,
		`SELECT t.id, MAX(COALESCE(f.height, 0)), MAX(COALESCE(f.hdr, '')),
		        MAX(COALESCE((SELECT MAX(channels) FROM media_track mt WHERE mt.file_id = f.id AND mt.kind = 'audio'), 0))
		   FROM item t
		   JOIN item e ON e.id = t.id OR e.parent_id = t.id
		     OR e.parent_id IN (SELECT id FROM item WHERE parent_id = t.id)
		   JOIN media_file f ON f.item_id = e.id AND f.missing_since IS NULL
		  WHERE t.id IN (`+placeholders(len(ids))+`)
		  GROUP BY t.id`, toAny(ids)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var k Kennzahl
		if err := rows.Scan(&id, &k.Height, &k.HDR, &k.Channels); err != nil {
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

// Qualitaet macht aus der Höhe ein Wort fürs Abzeichen.
func Qualitaet(height int) string {
	switch {
	case height >= 2000:
		return "4K"
	case height >= 1000:
		return "HD"
	case height > 0:
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
	args = append([]any{itemID, itemID, itemID, itemID}, args...)
	args = append(args, itemID, limit)
	rows, err := s.db.QueryContext(ctx,
		`SELECT i.id,
		        (SELECT count(*) FROM collection_item a JOIN collection_item b ON a.collection_id = b.collection_id
		          WHERE a.item_id = ? AND b.item_id = i.id) * 10
		      + (SELECT count(*) FROM besetzung a JOIN besetzung b ON a.name = b.name
		          WHERE a.item_id = ? AND b.item_id = i.id AND a.ord < 8 AND b.ord < 8) * 3
		      + (SELECT count(*) FROM genre a JOIN genre b ON a.name = b.name
		          WHERE a.item_id = ? AND b.item_id = i.id) * 2
		      + CASE WHEN i.kind = (SELECT kind FROM item WHERE id = ?) THEN 1 ELSE 0 END AS punkte
		   FROM item i WHERE `+strings.Join(where, " AND ")+` AND i.id != ? AND punkte > 2
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
