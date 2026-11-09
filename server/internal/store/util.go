// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

import (
	"database/sql"
	"strings"
	"time"
)

// placeholders baut "?,?,?" für ein IN (…).
func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

func toAny[T any](in []T) []any {
	out := make([]any, len(in))
	for i, v := range in {
		out[i] = v
	}
	return out
}

func collectStrings(rows *sql.Rows) ([]string, error) {
	out := []string{}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// heute ist Mitternacht (UTC) des heutigen Tages. Wo Lumen ein Datum
// braucht, das mit Schauen zusammenhängt, genügt der Tag — eine Uhrzeit
// wäre schon ein Stück Verlauf.
func heute() int64 {
	return time.Now().UTC().Truncate(24 * time.Hour).Unix()
}
