// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

// Der Stand eines Fortschritts — ohne Uhrzeit.
//
// Die Web-App hebt Stände, die unterwegs nicht ankamen, auf und reicht sie
// nach. Ein nachgereichter Stand kann aber veraltet sein: Inzwischen hat
// ein anderes Gerät weitergeschaut, oder jemand hat „Verlauf löschen"
// gedrückt. Mit einer Uhrzeit liesse sich das leicht entscheiden — aber
// eine Uhrzeit am Fortschritt sagte, wann jemand geschaut hat, und die
// speichert Lumen nicht. Eine Zahl, die bei jeder Meldung hochzählt, sagte
// wiederum, wie lange (so viele Meldungen, so viele Minuten).
//
// Darum ein zufälliger Marker: Jede Änderung an einer Zeile von progress
// gibt ihr einen neuen (über Trigger, damit kein Weg ihn vergisst). Die
// Web-App bekommt ihn nach jeder Meldung zurück (Kopf X-Lumen-Stand) und
// schickt ihn beim Nachreichen als „basis" mit. Steht beim Server noch
// derselbe Marker, hat niemand dazwischen geschrieben, und der Stand gilt.
// Sonst ist er veraltet und fällt weg. Gelöschter Verlauf hat gar keinen
// Marker mehr — auch das passt dann nicht. Der Marker selbst sagt nichts:
// keine Zeit, keine Reihenfolge, keine Dauer.
//
// Stand 16 (SchemaStand): die Spalte progress.stand und die Trigger.

import (
	"context"
	"database/sql"
	"strings"
)

func fortschrittStandNachruesten(db *sql.DB) error {
	if _, err := db.Exec(`ALTER TABLE progress ADD COLUMN stand TEXT`); err != nil &&
		!strings.Contains(err.Error(), "duplicate column") {
		return err
	}
	for _, stmt := range []string{
		`CREATE TRIGGER IF NOT EXISTS progress_stand_neu AFTER INSERT ON progress
		 BEGIN UPDATE progress SET stand = lower(hex(randomblob(8))) WHERE rowid = NEW.rowid; END`,
		// Nur wenn sich Stelle, Länge oder Haken ändern — das Setzen von
		// stand selbst löst ihn nicht wieder aus.
		`CREATE TRIGGER IF NOT EXISTS progress_stand_aendern AFTER UPDATE OF position_ms, duration_ms, finished ON progress
		 BEGIN UPDATE progress SET stand = lower(hex(randomblob(8))) WHERE rowid = NEW.rowid; END`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

// FortschrittStand: der Marker des Fortschritts ("" = keiner, etwa nach
// „Verlauf löschen").
func (s *Store) FortschrittStand(ctx context.Context, accountID, itemID string) string {
	var stand sql.NullString
	_ = s.db.QueryRowContext(ctx,
		`SELECT stand FROM progress WHERE account_id = ? AND item_id = ?`, accountID, itemID).Scan(&stand)
	return stand.String
}

// SaveProgressStand wie SaveProgress. Mit basis (nachgereicht) nur, wenn
// der Marker noch basis ist — sonst bleibt alles, wie es ist. Zurück: der
// Marker danach und ob der Stand galt.
func (s *Store) SaveProgressStand(ctx context.Context, accountID, itemID string, positionMS, durationMS int64, basis *string) (string, bool, error) {
	if basis != nil && s.FortschrittStand(ctx, accountID, itemID) != *basis {
		return s.FortschrittStand(ctx, accountID, itemID), false, nil
	}
	if err := s.SaveProgress(ctx, accountID, itemID, positionMS, durationMS); err != nil {
		return "", false, err
	}
	return s.FortschrittStand(ctx, accountID, itemID), true, nil
}
