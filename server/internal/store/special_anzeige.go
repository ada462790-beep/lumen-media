// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

import "context"

// Specials: welche dieser Folgen ein Special mit selbst vergebener Nummer
// sind (item_zusatz.special, etwa aus „S01E00"). Ihre Nummer hat der
// Scanner vergeben, nicht die Datei — die Apps zeigen sie darum ohne
// Nummer, als „Special". Echte S00Exx-Folgen behalten ihre.
func (s *Store) Specials(ctx context.Context, ids []string) map[string]bool {
	out := map[string]bool{}
	if len(ids) == 0 {
		return out
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT item_id FROM item_zusatz WHERE special = 1 AND item_id IN (`+placeholders(len(ids))+`)`, toAny(ids)...)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			out[id] = true
		}
	}
	return out
}
