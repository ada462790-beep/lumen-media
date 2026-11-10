// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

import (
	"database/sql"
	"fmt"
)

// musikFotoTabellenAlt: die Tabellen, die frühere
// Fassungen für Musik und Fotos angelegt haben. Kinder vor Eltern, damit
// DROP TABLE mit eingeschalteten Fremdschlüsseln nicht an einer Zeile
// hängen bleibt, die noch auf die Elterntabelle zeigt.
var musikFotoTabellenAlt = []string{
	"musik_listeneintrag", "musik_wiedergabeliste", "musik_favorit",
	"musik_hoerstand", "musik_hoerreihe",
	"musik_album", "musik_interpret", "musik_titel",
	"foto", "foto_album",
}

// musikFotoIndizesAlt: ihre Indizes. Sie fallen mit den Tabellen weg; hier
// stehen sie trotzdem, damit nichts übrig bleibt, falls eine Tabelle von
// Hand schon entfernt wurde und nur der Index noch dasteht.
var musikFotoIndizesAlt = []string{
	"musik_titel_album", "musik_titel_lib", "musik_titel_sort",
	"musik_album_lib", "musik_album_interpret", "musik_album_neu",
	"musik_hoerstand_reihe", "musik_wiedergabeliste_konto",
	"foto_zeit", "foto_album_idx", "foto_lib",
}

// musikUndFotosEntfernen räumt weg, was von Musik und Fotos übrig ist
// (Umbau auf Stand 10): Lumen ist ein Server für Filme und Serien.
//
//   - Bibliotheken der Art „music" und „photo" fallen weg, mit allem, was
//     daran hängt: Freigaben (grant_library), Zuordnungen zu Gruppen
//     (gruppe_bibliothek, ohne Fremdschlüssel) und — über ON DELETE
//     CASCADE — Titel, Alben, Fotos, Hörstände und Listeneinträge.
//   - Die Tabellen und Indizes für Musik und Fotos werden gelöscht.
//   - Die Einstellung, ob Fotos ihren Aufnahmeort zeigen, ebenso.
//
// Filme, Serien und alles, was an ihnen hängt, bleiben unberührt. Alles
// in einer Transaktion: Bricht der Umbau ab, bleibt die Datenbank, wie
// sie war, und beim nächsten Start beginnt er von vorn. Die Sicherung
// davor legt vorDemNachruesten an.
//
// Die Dateien auf der Platte (Cover, Vorschaubilder) räumt die API beim
// Start weg (api.MediathekResteWeg) — die Datenbank kennt den
// Datenordner nicht. Damit das genau einmal geschieht und nur nach diesem
// Umbau, setzt er die Einstellung MediathekResteEinst.
func musikUndFotosEntfernen(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	weg := `SELECT id FROM library WHERE kind IN ('music','photo')`
	// Gab es hier je Musik oder Fotos? Nur dann merkt sich der Umbau für
	// die API, dass deren Ordner auf der Platte einmal wegzuräumen sind —
	// eine ganz neue Datenbank hatte nie welche.
	var spuren int
	if err := tx.QueryRow(`SELECT (SELECT count(*) FROM library WHERE kind IN ('music','photo'))
	       + (SELECT count(*) FROM sqlite_master WHERE type = 'table'
	           AND (name LIKE 'musik%' OR name LIKE 'foto%'))`).Scan(&spuren); err != nil {
		return err
	}
	if spuren > 0 {
		if _, err := tx.Exec(`INSERT INTO setting (key, value) VALUES (?, '1')
		   ON CONFLICT (key) DO UPDATE SET value = excluded.value`, MediathekResteEinst); err != nil {
			return err
		}
	}
	for _, q := range []string{
		`DELETE FROM gruppe_bibliothek WHERE library_id IN (` + weg + `)`,
		`DELETE FROM grant_library WHERE library_id IN (` + weg + `)`,
		// Titel in solchen Bibliotheken gab es nie; falls doch, gehen sie
		// über ihre eigenen Fremdschlüssel mit (Dateien, Spuren, Stand …).
		`DELETE FROM item WHERE library_id IN (` + weg + `)`,
		`DELETE FROM library WHERE kind IN ('music','photo')`,
		`DELETE FROM setting WHERE key = 'fotos.ort_zeigen'`,
	} {
		if _, err := tx.Exec(q); err != nil {
			return fmt.Errorf("%s: %w", q, err)
		}
	}
	for _, t := range musikFotoTabellenAlt {
		if _, err := tx.Exec(`DROP TABLE IF EXISTS ` + t); err != nil {
			return fmt.Errorf("tabelle %s löschen: %w", t, err)
		}
	}
	for _, i := range musikFotoIndizesAlt {
		if _, err := tx.Exec(`DROP INDEX IF EXISTS ` + i); err != nil {
			return fmt.Errorf("index %s löschen: %w", i, err)
		}
	}
	return tx.Commit()
}

// MediathekResteEinst: steht auf „1", solange die Ordner von Musik und
// Fotos nach dem Umbau auf Stand 10 noch wegzuräumen sind.
const MediathekResteEinst = "umbau.mediathek_reste"
