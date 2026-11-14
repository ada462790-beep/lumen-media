// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

import (
	"context"
	"strings"
)

// TitelMitNamen: der eine Film oder die eine Serie, die das Konto (q)
// sieht und die genau so heisst — über Titel oder Originaltitel, ohne
// Rücksicht auf Gross- und Kleinschreibung, mit jahr > 0 auch im selben
// Jahr. Passen mehrere („Dune" 1984 und 2021) oder keiner, kommt nil:
// Dann ist ein Wunsch mit dem Namen allein nicht eindeutig schon erfüllt.
func (s *Store) TitelMitNamen(ctx context.Context, q Query, titel string, jahr int) (*Item, error) {
	k := titelKlein(titel)
	if k == "" {
		return nil, nil
	}
	q.Kinds, q.IDs, q.NotIDs, q.Text, q.Genre, q.Person, q.Buchstabe = nil, nil, nil, "", "", "", ""
	where, args, ok := bedingung(q)
	if !ok {
		return nil, nil
	}
	sqlText := `SELECT ` + itemCols + ` FROM item i
	   WHERE (i.titel_klein = ? OR i.original_klein = ?) AND ` + strings.Join(where, " AND ")
	args = append([]any{k, k}, args...)
	if jahr > 0 {
		sqlText += ` AND i.year = ?`
		args = append(args, jahr)
	}
	rows, err := s.db.QueryContext(ctx, sqlText+` LIMIT 2`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var treffer []*Item
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		treffer = append(treffer, it)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(treffer) != 1 {
		return nil, nil
	}
	return treffer[0], nil
}
