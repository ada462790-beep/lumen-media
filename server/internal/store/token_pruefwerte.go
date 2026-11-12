// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

import "context"

// TokenPruefwerte gibt die Prüfwerte aller angemeldeten Geräte. Der
// Bildschlüssel (api/medienschluessel.go) leitet sich aus ihnen ab; nach
// einem Neustart findet der Server so zu einem Schlüssel das Gerät wieder,
// ohne irgendwo festzuhalten, wem welcher gehört.
func (s *Store) TokenPruefwerte(ctx context.Context) ([][32]byte, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT hash FROM token`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out [][32]byte
	for rows.Next() {
		var b []byte
		if err := rows.Scan(&b); err != nil {
			return nil, err
		}
		if len(b) != 32 {
			continue
		}
		var h [32]byte
		copy(h[:], b)
		out = append(out, h)
	}
	return out, rows.Err()
}
