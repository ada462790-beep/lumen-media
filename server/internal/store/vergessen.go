// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

// PIN zurücksetzen durch die Verwaltung — und was dabei vergessen wird.
//
// Die oberste Regel des Besitzers: Die Verwaltung sieht nie, wer was wann
// wie lange schaut oder hört. Bis hierhin gab es dafür ein Loch: Die
// Verwaltung setzte einem Profil eine neue PIN, meldete sich damit als
// dieses Profil an und las Fortschritt, Merkliste und Hörstand. Das alte
// Token des Profils galt sogar weiter.
//
// Darum gilt jetzt: Wer die PIN eines anderen zurücksetzt, bekommt ein
// leeres Profil. In einer einzigen Transaktion fallen der private Stand
// (siehe privatesVergessen), alle Anmeldungen des Profils, und der neue
// Riegel wird gesetzt. Entweder alles oder nichts — ein halb gelöschter
// Verlauf mit altem PIN wäre schlimmer als keiner der beiden Schritte.

import (
	"context"
	"database/sql"
	"strconv"
	"time"
)

// PrefPinZurueckgesetzt merkt sich am Profil, dass die Verwaltung den
// PIN zurückgesetzt hat. Wert: Unix-Sekunden des Zurücksetzens. Die API
// zeigt daraus beim nächsten Anmelden einmal einen Hinweis und eine
// Neuigkeit; gelesen, wird der Eintrag gelöscht. Es ist ein Zeitpunkt
// einer Handlung der Verwaltung, nichts über das Schauen.
const PrefPinZurueckgesetzt = "hinweis.pinZurueckgesetzt"

// Prefs, die beim Zurücksetzen bleiben dürfen: Einstellungen, die nichts
// darüber sagen, was jemand geschaut hat (Sprache, Untertitel-Aussehen,
// Schalter). Alles andere — vor allem „spuren:<serie>" (verrät, welche
// Serien jemand schaut) und „newsSeen" (wann die Neuigkeiten zuletzt
// offen waren) — fällt weg. Eine Erlaubnisliste statt einer Sperrliste:
// ein künftiger Pref mit privatem Inhalt verschwindet so von selbst.
var prefsBleiben = []string{
	"sprache", "tonSprache", "untertitel", "utGroesse", "utHintergrund",
	"autoplay", "skipIntro", "shareRatings", "nochDaFrage", "utSuchSprachen",
}

// PinZuruecksetzen setzt den Riegel eines Profils im Auftrag der
// Verwaltung neu und vergisst dabei alles Private des Profils und alle
// seine Anmeldungen. Die Prüfung des neuen Riegels (Länge, Ziffern,
// Verwaltungsprofil ohne Riegel) kommt zuerst — scheitert sie, bleibt
// alles, wie es war.
func (s *Store) PinZuruecksetzen(ctx context.Context, accountID, kind, secret string) error {
	sp, err := s.sperreVorbereiten(ctx, accountID, kind, secret)
	if err != nil {
		return err
	}
	return s.tx(ctx, func(t *sql.Tx) error {
		if err := privatesVergessen(ctx, t, accountID); err != nil {
			return err
		}
		if _, err := t.ExecContext(ctx, `DELETE FROM token WHERE account_id = ?`, accountID); err != nil {
			return err
		}
		if _, err := t.ExecContext(ctx,
			`UPDATE account SET lock_kind=?, secret_hash=?, secret_salt=? WHERE id=?`,
			sp.art, sp.hash, sp.salt, accountID); err != nil {
			return err
		}
		_, err := t.ExecContext(ctx,
			`INSERT INTO prefs (account_id, key, value) VALUES (?,?,?)
			 ON CONFLICT (account_id, key) DO UPDATE SET value = excluded.value`,
			accountID, PrefPinZurueckgesetzt, strconv.FormatInt(time.Now().Unix(), 10))
		return err
	})
}

// privatesVergessen löscht den privaten Stand eines Profils: Fortschritt,
// Gesehen, die Reihenfolge von „Weiterschauen", Merkliste, Favoriten,
// Bewertungen, den Hörstand samt Reihenfolge und Musik-Herzen, die
// eigenen privaten Wiedergabelisten (Filme und Musik) und die Prefs, die
// etwas über das Schauen verraten.
//
// Geteilte und gemeinsame Listen bleiben stehen, mitsamt Inhalt: Sie sind
// ohnehin für den ganzen Haushalt sichtbar — die Verwaltung erfährt
// daraus nichts, was sie nicht schon sehen konnte —, und andere haben
// womöglich etwas hineingelegt, das sonst still verschwände.
//
// Jede Abfrage nennt das Konto (privacy_test.go). Die Datenbank läuft mit
// secure_delete: Gelöschte Zeilen werden überschrieben, nicht nur
// freigegeben.
func privatesVergessen(ctx context.Context, t *sql.Tx, accountID string) error {
	for _, q := range []string{
		`DELETE FROM progress WHERE account_id = ?`,
		`DELETE FROM progress_reihe WHERE account_id = ?`,
		`DELETE FROM watchlist WHERE account_id = ?`,
		`DELETE FROM favorit WHERE account_id = ?`,
		`DELETE FROM rating WHERE account_id = ?`,
		`DELETE FROM musik_hoerstand WHERE account_id = ?`,
		`DELETE FROM musik_hoerreihe WHERE account_id = ?`,
		`DELETE FROM musik_favorit WHERE account_id = ?`,
		// Einträge fallen über ON DELETE CASCADE mit.
		`DELETE FROM playlist WHERE account_id = ? AND shared = 0 AND collaborative = 0`,
		`DELETE FROM musik_wiedergabeliste WHERE account_id = ? AND geteilt = 0 AND gemeinsam = 0`,
	} {
		if _, err := t.ExecContext(ctx, q, accountID); err != nil {
			return err
		}
	}
	args := []any{accountID}
	for _, k := range prefsBleiben {
		args = append(args, k)
	}
	_, err := t.ExecContext(ctx,
		`DELETE FROM prefs WHERE account_id = ? AND key NOT IN (`+placeholders(len(prefsBleiben))+`)`, args...)
	return err
}

// AndereGeraeteAbmelden meldet alle Geräte eines Kontos ab, ausser dem
// einen, von dem die Anfrage kommt — nach einer eigenen PIN-Änderung:
// Wer den alten PIN kannte und sich damit irgendwo angemeldet hat, ist
// danach draussen.
func (s *Store) AndereGeraeteAbmelden(ctx context.Context, accountID string, behalten []byte) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM token WHERE account_id = ? AND hash != ?`, accountID, behalten)
	return err
}

// PrefLoeschen entfernt einen Pref des eigenen Kontos.
func (s *Store) PrefLoeschen(ctx context.Context, accountID, key string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM prefs WHERE account_id = ? AND key = ?`, accountID, key)
	return err
}
