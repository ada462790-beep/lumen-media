// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

// Für die Arbeit im Hintergrund (Vorschaubilder beim Spulen, Untertitel
// vorbereiten): welche Dateien es gibt. Nur lesend, kein neues Feld in
// der Datenbank — was vorbereitet ist, steht auf der Platte.

import "context"

// VorhandeneDateien sind die Kennungen aller Dateien, die da sind und eine
// bekannte Laufzeit haben, die neuesten zuerst (die schaut man am ehesten
// als Nächstes).
func (s *Store) VorhandeneDateien(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id FROM media_file
		 WHERE missing_since IS NULL AND COALESCE(duration_ms,0) > 0 AND defekt IS NULL
		 ORDER BY rowid DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// DateiGibtEs sagt, ob eine Datei noch eingetragen ist (auch wenn sie
// gerade fehlt — eine abgesteckte Platte soll nicht alles Vorbereitete
// wegwerfen).
func (s *Store) DateiGibtEs(ctx context.Context, id string) bool {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM media_file WHERE id = ?`, id).Scan(&n)
	// Im Zweifel (Fehler) gilt sie als vorhanden: lieber nichts löschen.
	return err != nil || n > 0
}
