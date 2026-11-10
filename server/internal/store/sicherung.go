// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

// Sicherung der Datenbank — einmal am Tag, genau zwei Generationen.
//
// Warum nur zwei: Jede Sicherung ist ein Stand von „wo steht wer in
// welchem Film". Sieben tägliche Sicherungen nebeneinander wären ein
// Tagebuch — wer zwei Stände vergleicht, sieht, wer gestern welche Folge
// geschaut hat. Zwei (gestern, vorgestern) reichen, um nach einem Fehler
// zurückzukommen, und sind kaum ein Verlauf. Darum ist die Zahl fest und
// nicht einstellbar.
//
// Weitere Regeln:
//   - Vor jeder Sicherung wird die Datenbank geprüft. Eine kaputte
//     überschreibt nie eine gute Sicherung.
//   - Die Dateien sind nur für den Besitzer lesbar (0600): Darin stehen
//     die Prüfwerte der PINs und das Geheimnis der Authenticator-App.
//   - Wiederherstellen geht beim nächsten Start (siehe Open): Die
//     gewählte Sicherung liegt dann als lumen.db.wiederherstellen bereit.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// SicherungsGenerationen ist fest: siehe oben.
const SicherungsGenerationen = 2

// Sicherung beschreibt eine vorhandene Sicherung — nur der Tag, keine Uhrzeit.
type Sicherung struct {
	Nr  int    `json:"nr"`
	Tag string `json:"tag"` // JJJJ-MM-TT
	MB  int64  `json:"mb"`
}

func sicherungsPfad(ordner string, nr int) string {
	return filepath.Join(ordner, fmt.Sprintf("lumen-%d.db", nr))
}

// Sichern legt eine neue Sicherung an und schiebt die älteren eins weiter.
func (s *Store) Sichern(ctx context.Context, ordner string) error {
	if err := os.MkdirAll(ordner, 0o700); err != nil {
		return err
	}
	var ergebnis string
	if err := s.db.QueryRowContext(ctx, `PRAGMA quick_check`).Scan(&ergebnis); err != nil {
		return fmt.Errorf("prüfen vor der sicherung: %w", err)
	}
	if ergebnis != "ok" {
		return fmt.Errorf("die datenbank ist beschädigt (%s) — die alten sicherungen bleiben unberührt", ergebnis)
	}

	neu := filepath.Join(ordner, "lumen-neu.db")
	_ = os.Remove(neu)
	if _, err := s.db.ExecContext(ctx, `VACUUM INTO ?`, neu); err != nil {
		_ = os.Remove(neu)
		return fmt.Errorf("sichern: %w", err)
	}
	if err := os.Chmod(neu, 0o600); err != nil {
		return err
	}
	// Die neue Sicherung selbst noch einmal prüfen, bevor sie eine alte
	// verdrängt.
	if err := pruefeDatei(neu); err != nil {
		_ = os.Remove(neu)
		return err
	}
	for i := SicherungsGenerationen; i > 1; i-- {
		if _, err := os.Stat(sicherungsPfad(ordner, i-1)); err == nil {
			if err := os.Rename(sicherungsPfad(ordner, i-1), sicherungsPfad(ordner, i)); err != nil {
				return err
			}
		}
	}
	if err := os.Rename(neu, sicherungsPfad(ordner, 1)); err != nil {
		return err
	}
	// Ältere Generationen aus früheren Fassungen weg.
	for i := SicherungsGenerationen + 1; i <= 10; i++ {
		_ = os.Remove(sicherungsPfad(ordner, i))
	}
	return nil
}

// Sicherungen listet die vorhandenen, neueste zuerst.
func Sicherungen(ordner string) []Sicherung {
	var out []Sicherung
	for i := 1; i <= SicherungsGenerationen; i++ {
		fi, err := os.Stat(sicherungsPfad(ordner, i))
		if err != nil {
			continue
		}
		out = append(out, Sicherung{Nr: i, Tag: fi.ModTime().Format("2006-01-02"), MB: fi.Size() >> 20})
	}
	return out
}

// Wiederherstellen legt Sicherung nr als lumen.db.wiederherstellen neben
// die Datenbank. Beim nächsten Start nimmt Open sie.
func Wiederherstellen(ordner string, nr int, dbPfad string) error {
	if nr < 1 || nr > SicherungsGenerationen {
		return errors.New("diese sicherung gibt es nicht")
	}
	quelle := sicherungsPfad(ordner, nr)
	if err := pruefeDatei(quelle); err != nil {
		return err
	}
	// Kopieren statt ganz in den Speicher lesen: Auf einem NAS mit 1 GB
	// wäre eine grosse Datenbank sonst zu viel auf einmal.
	ziel := dbPfad + ".wiederherstellen"
	if err := dateiKopieren(quelle, ziel+".neu"); err != nil {
		_ = os.Remove(ziel + ".neu")
		return err
	}
	return os.Rename(ziel+".neu", ziel)
}

func dateiKopieren(von, nach string) error {
	ein, err := os.Open(von)
	if err != nil {
		return err
	}
	defer ein.Close()
	aus, err := os.OpenFile(nach, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(aus, ein); err != nil {
		aus.Close()
		return err
	}
	if err := aus.Sync(); err != nil {
		aus.Close()
		return err
	}
	return aus.Close()
}

// pruefeDatei öffnet eine Datenbankdatei nur lesend und prüft sie.
func pruefeDatei(pfad string) error {
	db, err := sql.Open("sqlite3", "file:"+pfad+"?mode=ro")
	if err != nil {
		return err
	}
	defer db.Close()
	var ergebnis string
	if err := db.QueryRow(`PRAGMA quick_check`).Scan(&ergebnis); err != nil {
		return fmt.Errorf("sicherung prüfen: %w", err)
	}
	if ergebnis != "ok" {
		return fmt.Errorf("die sicherung ist beschädigt (%s)", ergebnis)
	}
	return nil
}

// wiederherstellenWennBereit tauscht vor dem Öffnen die Datenbank gegen
// eine bereitgelegte Sicherung. Die alte bleibt als .vor-wiederherstellen
// liegen, bis das Öffnen geklappt hat.
func wiederherstellenWennBereit(pfad string) (string, error) {
	bereit := pfad + ".wiederherstellen"
	if _, err := os.Stat(bereit); err != nil {
		return "", nil
	}
	if err := pruefeDatei(bereit); err != nil {
		_ = os.Remove(bereit)
		return "", fmt.Errorf("wiederherstellen abgebrochen: %w", err)
	}
	alt := pfad + ".vor-wiederherstellen"
	_ = os.Remove(alt)
	if _, err := os.Stat(pfad); err == nil {
		if err := os.Rename(pfad, alt); err != nil {
			return "", err
		}
	}
	for _, endung := range []string{"-wal", "-shm"} {
		_ = os.Remove(alt + endung)
		_ = os.Rename(pfad+endung, alt+endung)
	}
	if err := os.Rename(bereit, pfad); err != nil {
		return "", err
	}
	return alt, nil
}

// TagHeute ist das Datum für „letzte Sicherung" — ohne Uhrzeit.
func TagHeute() string { return time.Now().Format("2006-01-02") }
