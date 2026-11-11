// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

// Zuordnung: was die Verwaltung über nicht erkannte Titel braucht und was
// das Übernehmen der Zuordnungen aus Emby liest.

import "context"

// NichtErkannteZuruecksetzen stellt nicht erkannte Filme und Serien
// wieder auf „ausstehend" — nach einer besseren Zuordnung sollen sie
// noch einmal versucht werden. Von Hand zugewiesene sind nie „nicht
// erkannt" und bleiben so unberührt.
func (s *Store) NichtErkannteZuruecksetzen(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE item SET match_state = 'pending'
		 WHERE match_state = 'unmatched' AND kind IN ('movie','series')`)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// OffeneZuordnungen sind die Filme und Serien, die (noch) keinen Eintrag
// bei der Quelle haben: nicht erkannt oder noch nicht geprüft. Die nicht
// erkannten zuerst, dann die neusten.
func (s *Store) OffeneZuordnungen(ctx context.Context, limit int) ([]Item, error) {
	if limit <= 0 {
		limit = 500
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+itemCols+` FROM item i
		  WHERE i.match_state IN ('unmatched','pending') AND i.kind IN ('movie','series')
		  ORDER BY CASE i.match_state WHEN 'unmatched' THEN 0 ELSE 1 END, i.added_at DESC
		  LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Item{}
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *it)
	}
	return out, rows.Err()
}

// OffeneZuordnungenZahl zählt, was OffeneZuordnungen ohne Grenze liefern
// würde — getrennt nach nicht erkannt und noch nicht geprüft.
func (s *Store) OffeneZuordnungenZahl(ctx context.Context) (unmatched, pending int) {
	_ = s.db.QueryRowContext(ctx,
		`SELECT COALESCE(sum(match_state = 'unmatched'),0), COALESCE(sum(match_state = 'pending'),0)
		   FROM item WHERE kind IN ('movie','series') AND match_state IN ('unmatched','pending')`).
		Scan(&unmatched, &pending)
	return unmatched, pending
}

// ZuordnungsZustaende nennt für alle Filme und Serien den Zustand der
// Zuordnung (matched, manual, unmatched, pending, lokal).
func (s *Store) ZuordnungsZustaende(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, match_state FROM item WHERE kind IN ('movie','series')`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var id, z string
		if err := rows.Scan(&id, &z); err != nil {
			return nil, err
		}
		out[id] = z
	}
	return out, rows.Err()
}
