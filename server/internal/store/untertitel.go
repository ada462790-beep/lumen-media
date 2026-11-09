// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

import "context"

// DateienOhneUntertitel: Dateien von Filmen und Folgen, die in dieser
// Sprache noch keinen Untertitel haben und seit dem Tag „seit" nicht
// vergeblich angefragt wurden — die neuesten zuerst. Nur ausgemessene
// Dateien: Bei den anderen weiss Lumen noch nicht, was schon drin ist.
func (s *Store) DateienOhneUntertitel(ctx context.Context, sprache string, seit int64, limit int) ([]MediaFile, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT f.id, f.item_id, f.path, f.size_bytes FROM media_file f
		  JOIN item i ON i.id = f.item_id
		 WHERE i.kind IN ('movie','episode') AND f.missing_since IS NULL AND f.spuren_gelesen = 1
		   AND NOT EXISTS (SELECT 1 FROM media_track t WHERE t.file_id = f.id
		                    AND t.kind = 'subtitle' AND t.language = ?)
		   AND NOT EXISTS (SELECT 1 FROM untertitel_versuch v WHERE v.file_id = f.id
		                    AND v.sprache = ? AND v.tag >= ?)
		 ORDER BY i.added_at DESC, f.id
		 LIMIT ?`, sprache, sprache, seit, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MediaFile{}
	for rows.Next() {
		var f MediaFile
		if err := rows.Scan(&f.ID, &f.ItemID, &f.Path, &f.SizeBytes); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// UntertitelVersuchMerken hält fest, dass für diese Datei in dieser
// Sprache gesucht wurde — auf den Tag, ohne Profil.
func (s *Store) UntertitelVersuchMerken(ctx context.Context, fileID, sprache string, gefunden bool) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO untertitel_versuch (file_id, sprache, tag, gefunden) VALUES (?,?,?,?)
		 ON CONFLICT (file_id, sprache) DO UPDATE SET tag = excluded.tag, gefunden = excluded.gefunden`,
		fileID, sprache, heute(), b2i(gefunden))
	return err
}
