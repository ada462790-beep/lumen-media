// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

// Grosse Nachträge in Häppchen.
//
// Eine Transaktion hält die Schreibsperre, bis sie fertig ist. Wer in der
// Zeit schreiben will — der Stand einer Wiedergabe, ein Haken, eine
// Anmeldung —, wartet. „Altersfreigaben nachrechnen" über 100 000 Titel
// hielt sie in einer Transaktion 3,5 Sekunden lang (auf einer NAS ein
// Vielfaches). In Häppchen von höchstens haeppchenDauer kommen andere
// Schreiber zwischendurch dran; dazwischen lässt HintergrundWarten der
// Bedienung den Vortritt.

import (
	"context"
	"database/sql"
	"time"
)

// haeppchenDauer: so lange hält ein Häppchen die Sperre höchstens (etwa —
// geprüft wird nach jedem Schritt).
var haeppchenDauer = 50 * time.Millisecond

// haeppchenPause: so lange ruht die Arbeit zwischen zwei Häppchen.
var haeppchenPause = 5 * time.Millisecond

// inHaeppchen ruft schritt für i = 0 … n-1, in Transaktionen von
// höchstens haeppchenDauer. Innerhalb eines Stapels läuft alles in dessen
// Transaktion. Bricht ctx ab, bleibt das bisher Geschriebene stehen —
// schritt muss sich darum gefahrlos wiederholen lassen.
func (s *Store) inHaeppchen(ctx context.Context, n int, schritt func(t *sql.Tx, i int) error) error {
	if s.stapel != nil {
		for i := 0; i < n; i++ {
			if err := schritt(s.stapel, i); err != nil {
				return err
			}
		}
		return nil
	}
	for i := 0; i < n; {
		if i > 0 {
			HintergrundWarten(ctx)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(haeppchenPause):
			}
		}
		t, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		start := time.Now()
		for ; i < n; i++ {
			if err := schritt(t, i); err != nil {
				_ = t.Rollback()
				return err
			}
			if time.Since(start) >= haeppchenDauer {
				i++
				break
			}
		}
		if err := t.Commit(); err != nil {
			return err
		}
	}
	return nil
}
