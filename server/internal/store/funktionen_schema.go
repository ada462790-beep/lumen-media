// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

import "database/sql"

// funktionenNachruesten legt die Tabellen für Handbearbeitung, eigene
// Sammlungen und das Holen von Untertiteln an. Eigene Funktion statt
// weiterer Zeilen in nachruesten(): So bleibt beieinander, was
// zusammengehört, und nachruesten() wächst nur um einen Aufruf.
func funktionenNachruesten(db *sql.DB) error {
	for _, stmt := range []string{
		// Von Hand bearbeitete Felder. Steht ein Feld hier, überschreibt
		// es weder „Metadaten neu laden" noch eine NFO-Datei.
		// anzeige: der eingetippte Sortiertitel (sort_title selbst ist ein
		// kleingeschriebener Schlüssel und taugt nicht zum Anzeigen).
		`CREATE TABLE IF NOT EXISTS feld_sperre (
		   item_id TEXT NOT NULL REFERENCES item(id) ON DELETE CASCADE,
		   feld    TEXT NOT NULL,
		   anzeige TEXT,
		   PRIMARY KEY (item_id, feld))`,
		// Eigene Sammlungen: von Hand zusammengestellt, für alle sichtbar
		// (soweit die Freigaben reichen).
		`CREATE TABLE IF NOT EXISTS sammlung (
		   id           TEXT PRIMARY KEY,
		   name         TEXT NOT NULL,
		   beschreibung TEXT NOT NULL DEFAULT '',
		   bild         TEXT,
		   sortierung   TEXT NOT NULL DEFAULT 'hand',
		   regel        TEXT NOT NULL DEFAULT '',
		   auf_start    INTEGER NOT NULL DEFAULT 0,
		   erstellt     INTEGER NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS sammlung_titel (
		   sammlung_id TEXT NOT NULL REFERENCES sammlung(id) ON DELETE CASCADE,
		   item_id     TEXT NOT NULL REFERENCES item(id) ON DELETE CASCADE,
		   ord         INTEGER NOT NULL DEFAULT 0,
		   PRIMARY KEY (sammlung_id, item_id))`,
		`CREATE INDEX IF NOT EXISTS sammlung_titel_by_item ON sammlung_titel (item_id)`,
		// Automatisch geholte Untertitel: welche Datei in welcher Sprache
		// schon versucht wurde — damit Lumen nicht jede Nacht dieselben
		// hundert Filme vergeblich anfragt. Auf den Tag genau, ohne Bezug
		// zu einem Profil: Das holt der Server, nicht jemand, der schaut.
		`CREATE TABLE IF NOT EXISTS untertitel_versuch (
		   file_id  TEXT NOT NULL REFERENCES media_file(id) ON DELETE CASCADE,
		   sprache  TEXT NOT NULL,
		   tag      INTEGER NOT NULL,
		   gefunden INTEGER NOT NULL DEFAULT 0,
		   PRIMARY KEY (file_id, sprache))`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}
