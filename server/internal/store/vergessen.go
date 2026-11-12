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
// wie lange schaut. Bis hierhin gab es dafür ein Loch: Die
// Verwaltung setzte einem Profil eine neue PIN, meldete sich damit als
// dieses Profil an und las Fortschritt und Merkliste. Das alte
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
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// PrefPinZurueckgesetzt merkt sich am Profil, dass die Verwaltung den
// PIN zurückgesetzt hat. Wert: Unix-Sekunden des Zurücksetzens. Die API
// zeigt daraus beim nächsten Anmelden einmal einen Hinweis und eine
// Neuigkeit; gelesen, wird der Eintrag gelöscht. Es ist ein Zeitpunkt
// einer Handlung der Verwaltung, nichts über das Schauen.
const PrefPinZurueckgesetzt = "hinweis.pinZurueckgesetzt"

// PrefNeueSperreNoetig: Die Verwaltung hat den Schutz zurückgesetzt und
// dabei nur einen Einmal-Code vergeben. Wer sich damit anmeldet, muss
// zuerst eine eigene PIN (oder ein Passwort) festlegen — erst dann gibt
// es eine Anmeldung. So kennt die Verwaltung den Schutz danach nicht.
// Wert: bis wann der Einmal-Code gilt (Unix-Sekunden, auf die volle
// Stunde aufgerundet — wann genau die Verwaltung handelte, steht so
// nirgends). „1" stammt aus älteren Versionen und läuft nicht ab.
const PrefNeueSperreNoetig = "pflicht.neueSperre"

// EinmalCodeGueltig: so lange gilt ein Einmal-Code nach dem Zurücksetzen.
// Danach muss die Verwaltung einen neuen vergeben — ein Code, der Wochen
// auf einem Zettel liegt, soll nicht mehr in das Profil führen.
const EinmalCodeGueltig = 72 * time.Hour

// einmalCodeBis: Ablauf eines jetzt vergebenen Einmal-Codes, auf die
// volle Stunde aufgerundet. Austauschbar für Tests.
var einmalCodeBis = func() int64 {
	return time.Now().Add(EinmalCodeGueltig).Truncate(time.Hour).Add(time.Hour).Unix()
}

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
		// Nur der Tag: wann die Verwaltung handelte, auf die Sekunde, geht
		// niemanden etwas an.
		if _, err := t.ExecContext(ctx,
			`INSERT INTO prefs (account_id, key, value) VALUES (?,?,?)
			 ON CONFLICT (account_id, key) DO UPDATE SET value = excluded.value`,
			accountID, PrefPinZurueckgesetzt, strconv.FormatInt(heute(), 10)); err != nil {
			return err
		}
		if sp.art == "none" {
			return nil
		}
		_, err := t.ExecContext(ctx,
			`INSERT INTO prefs (account_id, key, value) VALUES (?,?,?)
			 ON CONFLICT (account_id, key) DO UPDATE SET value = excluded.value`,
			accountID, PrefNeueSperreNoetig, strconv.FormatInt(einmalCodeBis(), 10))
		return err
	})
}

// NeueSperreNoetig: true, solange das Profil nach einem Zurücksetzen noch
// keinen eigenen Schutz gesetzt hat.
func (s *Store) NeueSperreNoetig(ctx context.Context, accountID string) bool {
	var n int
	_ = s.db.QueryRowContext(ctx,
		`SELECT count(*) FROM prefs WHERE account_id = ? AND key = ?`, accountID, PrefNeueSperreNoetig).Scan(&n)
	return n > 0
}

// EinmalCodeAbgelaufen: true, wenn das Profil noch einen eigenen Schutz
// festlegen müsste, der Einmal-Code dafür aber nicht mehr gilt.
func (s *Store) EinmalCodeAbgelaufen(ctx context.Context, accountID string) bool {
	var v string
	if err := s.db.QueryRowContext(ctx,
		`SELECT value FROM prefs WHERE account_id = ? AND key = ?`, accountID, PrefNeueSperreNoetig).Scan(&v); err != nil {
		return false
	}
	bis, err := strconv.ParseInt(v, 10, 64)
	if err != nil || bis < 1_000_000_000 {
		return false // ältere Version: ohne Ablauf
	}
	return time.Now().Unix() >= bis
}

// EigeneSperreNachZuruecksetzen setzt den Schutz, den die Person selbst
// gewählt hat, und löscht die Pflicht dazu — in einem Schritt. „Ohne
// Schutz" gibt es hier nicht: Dann hätte die Verwaltung gleich „Ohne
// Schutz" wählen können.
func (s *Store) EigeneSperreNachZuruecksetzen(ctx context.Context, accountID, kind, secret string) error {
	if kind == "none" {
		return errors.New("Bitte eine PIN oder ein Passwort wählen.")
	}
	sp, err := s.sperreVorbereiten(ctx, accountID, kind, secret)
	if err != nil {
		return err
	}
	return s.tx(ctx, func(t *sql.Tx) error {
		if _, err := t.ExecContext(ctx,
			`UPDATE account SET lock_kind=?, secret_hash=?, secret_salt=? WHERE id=?`,
			sp.art, sp.hash, sp.salt, accountID); err != nil {
			return err
		}
		_, err := t.ExecContext(ctx, `DELETE FROM prefs WHERE account_id = ? AND key = ?`, accountID, PrefNeueSperreNoetig)
		return err
	})
}

// Ausschreiben schreibt die WAL-Datei in die Datenbank zurück und kürzt
// sie auf null. Nach dem Löschen von Privatem (PIN zurückgesetzt, Konto
// entfernt, Verlauf gelöscht): secure_delete überschreibt nur die Seiten
// der Datenbank — in der WAL-Datei stünden die alten Zeilen sonst weiter,
// bis sie irgendwann überschrieben werden. Liest gerade jemand, klappt das
// Kürzen nicht beim ersten Mal; dann ein paar Versuche.
func (s *Store) Ausschreiben(ctx context.Context) error {
	var err error
	for i := 0; i < 20; i++ {
		var busy, log, done int
		err = s.db.QueryRowContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`).Scan(&busy, &log, &done)
		if err == nil && busy == 0 {
			return nil
		}
		if err == nil {
			err = fmt.Errorf("wal: belegt (%d)", busy)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
	return err
}

// privatesVergessen löscht den privaten Stand eines Profils: Fortschritt,
// Gesehen, die Reihenfolge von „Weiterschauen", Merkliste, Favoriten,
// Bewertungen, die eigenen privaten Wiedergabelisten und die Prefs, die
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
	for _, q := range privateAbfragen() {
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

// privateAbfragen: was privatesVergessen löscht, ohne die Prefs.
func privateAbfragen() []string {
	return []string{
		`DELETE FROM progress WHERE account_id = ?`,
		`DELETE FROM progress_reihe WHERE account_id = ?`,
		`DELETE FROM watchlist WHERE account_id = ?`,
		`DELETE FROM favorit WHERE account_id = ?`,
		`DELETE FROM rating WHERE account_id = ?`,
		// Einträge fallen über ON DELETE CASCADE mit. (In einer Sicherung
		// gilt ON DELETE CASCADE nur mit foreign_keys; die Einträge einer
		// privaten Liste nennen ohnehin kein Konto.)
		`DELETE FROM playlist_item WHERE playlist_id IN
		   (SELECT id FROM playlist WHERE account_id = ? AND shared = 0 AND collaborative = 0)`,
		`DELETE FROM playlist WHERE account_id = ? AND shared = 0 AND collaborative = 0`,
	}
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

// SicherungenVergessen löscht den privaten Stand eines Profils auch aus
// den Sicherungen im Ordner. Sonst stünde nach einem Zurücksetzen alles
// Gelöschte in lumen-1.db weiter — lesbar für jeden mit Zugriff auf die
// Dateien, und mit „Wiederherstellen" sogar wieder in Lumen. Danach wird
// jede Sicherung neu geschrieben (VACUUM), damit auch freie Seiten leer
// sind, und geprüft.
//
// Eine Sicherung aus einer älteren Fassung kennt vielleicht nicht jede
// Tabelle: Was fehlt, wird übersprungen.
//
// Sie nimmt dafür die Sperre der Sicherungen (sicherung_sperre.go): Läuft
// gerade das nächtliche Sichern, wartet sie, bis es fertig ist.
func SicherungenVergessen(ctx context.Context, ordner, accountID string) error {
	if _, err := os.Stat(ordner); err != nil {
		return nil // noch keine Sicherungen
	}
	frei, err := sicherungSperren(ctx, ordner)
	if err != nil {
		return err
	}
	defer frei()
	var fehler error
	for i := 1; i <= SicherungsGenerationen; i++ {
		pfad := sicherungsPfad(ordner, i)
		if _, err := os.Stat(pfad); err != nil {
			continue
		}
		if err := sicherungVergessen(ctx, pfad, accountID); err != nil {
			fehler = errors.Join(fehler, err)
		}
	}
	return fehler
}

func sicherungVergessen(ctx context.Context, pfad, accountID string) error {
	db, err := sql.Open("sqlite3", pfad+"?_busy_timeout=5000&_journal_mode=DELETE&_secure_delete=on")
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	for _, q := range append(privateAbfragen(),
		`DELETE FROM token WHERE account_id = ?`) {
		if _, err := db.ExecContext(ctx, q, accountID); err != nil && !keineTabelle(err) {
			return err
		}
	}
	args := []any{accountID}
	for _, k := range prefsBleiben {
		args = append(args, k)
	}
	if _, err := db.ExecContext(ctx,
		`DELETE FROM prefs WHERE account_id = ? AND key NOT IN (`+placeholders(len(prefsBleiben))+`)`, args...); err != nil && !keineTabelle(err) {
		return err
	}
	if _, err := db.ExecContext(ctx, `VACUUM`); err != nil {
		return err
	}
	if err := db.Close(); err != nil {
		return err
	}
	return pruefeDatei(pfad)
}

func keineTabelle(err error) bool {
	return err != nil && strings.Contains(err.Error(), "no such table")
}
