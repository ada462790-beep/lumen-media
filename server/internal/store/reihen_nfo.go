// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

// Filmreihen aus NFO-Dateien: <set>Matrix Collection</set>.
//
// Wer aus Emby oder Jellyfin kommt, hat seine Sammlungen dort gepflegt —
// mit Namen, die er kennt. Die gehen vor: vor der Reihe, die Lumen aus den
// Titeln errät, und vor der von TMDb. Ein Film steht in höchstens einer
// NFO-Reihe; nennt seine NFO keine mehr, fällt er heraus, und die Reihe
// verschwindet mit dem letzten Film (ReihenNeuBilden). Umbenennen und
// Auflösen durch den Besitzer bleiben wie bei jeder Reihe.

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// NFOReiheZuordnen hängt einen Film an die Reihe aus seiner NFO. tmdbColID
// ist die TMDb-Kennung der Sammlung, wenn die NFO sie nennt; Filme mit
// derselben Kennung landen in derselben Reihe, auch wenn eine NFO sie
// anders schreibt.
func (s *Store) NFOReiheZuordnen(ctx context.Context, name, tmdbColID, itemID string, jahr int) error {
	name, tmdbColID = strings.TrimSpace(name), strings.TrimSpace(tmdbColID)
	if name == "" {
		return s.NFOReiheLoesen(ctx, itemID)
	}
	schluessel := "nfo:" + normSchluessel(name)
	if tmdbColID != "" {
		schluessel = "nfo:tmdb:" + tmdbColID
	}
	return s.tx(ctx, func(t *sql.Tx) error {
		var id string
		err := t.QueryRowContext(ctx,
			`SELECT id FROM collection WHERE source = 'nfo' AND schluessel = ?`, schluessel).Scan(&id)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			id = "col_" + kurzHash(schluessel)
			if _, err := t.ExecContext(ctx,
				`INSERT INTO collection (id, name, source, schluessel, tmdb_id) VALUES (?, ?, 'nfo', ?, ?)
				 ON CONFLICT (id) DO UPDATE SET schluessel = excluded.schluessel`,
				id, name, schluessel, nullStr(tmdbColID)); err != nil {
				return err
			}
		case err != nil:
			return err
		default:
			// Schreiben zwei NFOs die Sammlung verschieden („Matrix
			// Collection", „Matrix Filmreihe"), gilt der Name aus dem
			// ältesten Film — sonst wechselte er mit jedem Durchlauf.
			if _, err := t.ExecContext(ctx,
				`UPDATE collection SET name = ? WHERE id = ? AND eigener_name = 0 AND NOT EXISTS
				   (SELECT 1 FROM collection_item ci WHERE ci.collection_id = collection.id
				     AND ci.item_id != ? AND (ci.ord < ? OR (ci.ord = ? AND ci.item_id < ?)))`,
				name, id, itemID, jahr, jahr, itemID); err != nil {
				return err
			}
		}
		// Aus jeder anderen Reihe heraus: die NFO weiss es am besten.
		if _, err := t.ExecContext(ctx,
			`DELETE FROM collection_item WHERE item_id = ? AND collection_id != ? AND collection_id IN
			   (SELECT id FROM collection WHERE source IN ('auto', 'tmdb', 'nfo'))`, itemID, id); err != nil {
			return err
		}
		_, err = t.ExecContext(ctx,
			`INSERT INTO collection_item (collection_id, item_id, ord) VALUES (?, ?, ?)
			 ON CONFLICT (collection_id, item_id) DO UPDATE SET ord = excluded.ord`, id, itemID, jahr)
		return err
	})
}

// NFOReiheLoesen nimmt einen Film aus seiner NFO-Reihe.
func (s *Store) NFOReiheLoesen(ctx context.Context, itemID string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM collection_item WHERE item_id = ? AND collection_id IN
		   (SELECT id FROM collection WHERE source = 'nfo')`, itemID)
	return err
}

// inNFOReihe: Steht der Film in einer Reihe aus seiner NFO oder aus der
// Übernahme aus Emby (reihen_emby.go)? Dann ändert TMDb nichts an ihm.
func inNFOReihe(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, itemID string) bool {
	var n int
	_ = q.QueryRowContext(ctx, `SELECT count(*) FROM collection_item ci JOIN collection c ON c.id = ci.collection_id
		 WHERE ci.item_id = ? AND c.source IN ('nfo', 'emby')`, itemID).Scan(&n)
	return n > 0
}
