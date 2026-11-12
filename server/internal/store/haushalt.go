// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

// Haushalt und Gäste.
//
// „Im Haushalt geteilt" (Listen, Bewertungen) hiess bisher: alle Profile
// auf diesem Server — auch der Freund, der von unterwegs eine Einladung
// eingelöst hat. Er sah dann, wer im Haushalt wie bewertet und welche
// Listen führt, mit Namen — genau die Namen, die die Profilauswahl von
// unterwegs bewusst verbirgt.
//
// Ein Gast gehört nicht zum Haushalt: Er sieht keine geteilten Listen und
// Bewertungen anderer, und seine eigenen geteilten sieht der Haushalt
// nicht. Gast wird, wer eine Einladung von unterwegs einlöst; die
// Verwaltung kann das am Konto ändern. Vermerkt ist es als Einstellung
// „gast:<konto>" — keine neue Spalte, nichts über das Schauen.

import "context"

// gastSQL: SQL-Ausdruck, wahr, wenn das Konto in spalte ein Gast ist.
func gastSQL(spalte string) string {
	return `EXISTS (SELECT 1 FROM setting WHERE key = 'gast:' || ` + spalte + `)`
}

// IstGast sagt, ob ein Konto als Gast gilt.
func (s *Store) IstGast(ctx context.Context, accountID string) bool {
	return s.Setting(ctx, "gast:"+accountID, "") == "1"
}

// GastSetzen macht ein Konto zum Gast oder nimmt es in den Haushalt auf.
func (s *Store) GastSetzen(ctx context.Context, accountID string, gast bool) error {
	if gast {
		return s.SetSetting(ctx, "gast:"+accountID, "1")
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM setting WHERE key = ?`, "gast:"+accountID)
	return err
}

// BewertungenNichtMehrTeilen nimmt alle geteilten Bewertungen eines
// Profils zurück — wer „Bewertungen im Haushalt zeigen" ausschaltet,
// meint auch die bisherigen.
func (s *Store) BewertungenNichtMehrTeilen(ctx context.Context, accountID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE rating SET shared = 0 WHERE account_id = ?`, accountID)
	return err
}
