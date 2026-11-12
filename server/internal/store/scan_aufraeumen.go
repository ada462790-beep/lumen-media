// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

// Aufräumen nach einem Durchlauf: leere Folgen und Staffeln, die ältere
// Fassungen zurückgelassen haben.

import (
	"context"
	"database/sql"
)

// FolgenOhneDatei entfernt Folgen, an denen keine einzige Datei mehr
// hängt, und danach Staffeln ohne Folgen.
//
// Solche Folgen entstanden bis zu dieser Fassung bei jedem Durchlauf:
// Dateien ohne Nummer im Namen („Staffel 1/Pilot.mkv") bekamen jedes Mal
// neue Nummern hinter den alten, die Datei wanderte zur neuen Folge, und
// die alte blieb leer stehen — mit „Ansehen"-Knopf und dem Gesehen-Haken
// der Leute. Was an einer leeren Folge hängt (Stand, Merkliste,
// Bewertung, Listen), wandert zur Folge derselben Staffel, die heute
// dieselbe Datei hat: gleicher Titel und als einzige so benannt. Gibt es
// keine solche, geht es mit der Folge.
//
// Eine Datei, die nur gerade fehlt, steht weiter in der Datenbank
// (missing_since); ihre Folge bleibt.
func (s *Store) FolgenOhneDatei(ctx context.Context) (int, error) {
	type leer struct{ id, staffel, titel string }
	rows, err := s.db.QueryContext(ctx,
		`SELECT e.id, COALESCE(e.parent_id,''), e.title FROM item e
		  WHERE e.kind = 'episode'
		    AND NOT EXISTS (SELECT 1 FROM media_file f WHERE f.item_id = e.id)`)
	if err != nil {
		return 0, err
	}
	var liste []leer
	for rows.Next() {
		var l leer
		if err := rows.Scan(&l.id, &l.staffel, &l.titel); err != nil {
			rows.Close()
			return 0, err
		}
		liste = append(liste, l)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	n := 0
	if len(liste) > 0 {
		konten, err := s.kontenIDs(ctx)
		if err != nil {
			return 0, err
		}
		err = s.tx(ctx, func(t *sql.Tx) error {
			for _, l := range liste {
				var ziel string
				// Genau eine Folge derselben Staffel mit gleichem Titel und
				// mindestens einer Datei.
				_ = t.QueryRowContext(ctx,
					`SELECT min(e.id) FROM item e
					  WHERE e.parent_id = ? AND e.kind = 'episode' AND e.title = ? AND e.id != ?
					    AND EXISTS (SELECT 1 FROM media_file f WHERE f.item_id = e.id)
					 HAVING count(*) = 1`, l.staffel, l.titel, l.id).Scan(&ziel)
				if ziel != "" {
					if err := persoenlichesUmhaengen(ctx, t, l.id, ziel, konten); err != nil {
						return err
					}
				}
				if _, err := t.ExecContext(ctx, `DELETE FROM item WHERE id = ?`, l.id); err != nil {
					return err
				}
				n++
			}
			return nil
		})
		if err != nil {
			return 0, err
		}
	}
	// Staffeln ohne eine einzige Folge (und ohne eigene Datei).
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM item WHERE kind = 'season'
		    AND NOT EXISTS (SELECT 1 FROM item e WHERE e.parent_id = item.id)
		    AND NOT EXISTS (SELECT 1 FROM media_file f WHERE f.item_id = item.id)`)
	if err == nil {
		weg, _ := res.RowsAffected()
		n += int(weg)
	}
	return n, err
}
