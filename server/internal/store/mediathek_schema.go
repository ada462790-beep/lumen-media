// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

import "database/sql"

// mediathekTabellen legt die Tabellen für Musik und Fotos an. Sie hängt an
// nachruesten(): wer Lumen schon eingerichtet hat, bekommt sie beim
// nächsten Start dazu, ohne dass an den bestehenden Tabellen etwas
// geschieht.
//
// Warum eigene Tabellen und nicht „item"? Ein Musiktitel hat keine
// Altersfreigabe, keine TMDb-Kennung, keinen Fortschritt zum Weiterschauen
// — und ein Foto noch viel weniger. In „item" stünden sie nur im Weg:
// Neuigkeiten, Doppel-Erkennung, Filmreihen, Metadaten-Abgleich und die
// Suche müssten überall „aber keine Musik" dazuschreiben, und wer das
// einmal vergisst, bekommt 4000 Lieder unter „Neu hinzugefügt". Getrennt
// berühren sich die Welten nur über die Bibliothek — und damit über die
// Freigaben, die pro Bibliothek gelten.
func mediathekTabellen(db *sql.DB) error {
	for _, stmt := range []string{
		// ------------------------------------------------------ Musik
		// Ein Musikstück auf der Platte. groesse und mtime entscheiden, ob
		// die Datei beim nächsten Durchlauf neu gelesen werden muss.
		`CREATE TABLE IF NOT EXISTS musik_titel (
		   id             TEXT PRIMARY KEY,
		   library_id     TEXT NOT NULL REFERENCES library(id) ON DELETE CASCADE,
		   album_id       TEXT NOT NULL DEFAULT '',
		   pfad           TEXT NOT NULL UNIQUE,
		   groesse        INTEGER NOT NULL,
		   mtime          INTEGER NOT NULL,
		   titel          TEXT NOT NULL,
		   titel_sort     TEXT NOT NULL,
		   interpret      TEXT NOT NULL DEFAULT '',
		   albuminterpret TEXT NOT NULL DEFAULT '',
		   album          TEXT NOT NULL DEFAULT '',
		   track          INTEGER NOT NULL DEFAULT 0,
		   disc           INTEGER NOT NULL DEFAULT 0,
		   jahr           INTEGER NOT NULL DEFAULT 0,
		   genre          TEXT NOT NULL DEFAULT '',
		   komponist      TEXT NOT NULL DEFAULT '',
		   dauer_ms       INTEGER NOT NULL DEFAULT 0,
		   compilation    INTEGER NOT NULL DEFAULT 0,
		   explizit       INTEGER NOT NULL DEFAULT 0,
		   codec          TEXT NOT NULL DEFAULT '',
		   container      TEXT NOT NULL DEFAULT '',
		   bitrate        INTEGER NOT NULL DEFAULT 0,
		   abtastrate     INTEGER NOT NULL DEFAULT 0,
		   hat_cover      INTEGER NOT NULL DEFAULT 0,
		   hinzugefuegt   INTEGER NOT NULL,
		   fehlt_seit     INTEGER)`,
		`CREATE INDEX IF NOT EXISTS musik_titel_album ON musik_titel (album_id, disc, track)`,
		`CREATE INDEX IF NOT EXISTS musik_titel_lib ON musik_titel (library_id)`,
		`CREATE INDEX IF NOT EXISTS musik_titel_sort ON musik_titel (titel_sort)`,
		// Ein Album wird aus seinen Titeln gebildet, nach jedem Durchlauf
		// neu. Die Kennung ergibt sich aus Ordner und Albumname — so bleibt
		// sie über Durchläufe gleich, und Favoriten und Listen halten.
		`CREATE TABLE IF NOT EXISTS musik_album (
		   id             TEXT PRIMARY KEY,
		   library_id     TEXT NOT NULL REFERENCES library(id) ON DELETE CASCADE,
		   name           TEXT NOT NULL,
		   name_sort      TEXT NOT NULL,
		   interpret      TEXT NOT NULL DEFAULT '',
		   interpret_id   TEXT NOT NULL DEFAULT '',
		   jahr           INTEGER NOT NULL DEFAULT 0,
		   genre          TEXT NOT NULL DEFAULT '',
		   compilation    INTEGER NOT NULL DEFAULT 0,
		   cover_quelle   TEXT NOT NULL DEFAULT '',
		   titel_anzahl   INTEGER NOT NULL DEFAULT 0,
		   dauer_ms       INTEGER NOT NULL DEFAULT 0,
		   hinzugefuegt   INTEGER NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS musik_album_lib ON musik_album (library_id)`,
		`CREATE INDEX IF NOT EXISTS musik_album_interpret ON musik_album (interpret_id)`,
		`CREATE INDEX IF NOT EXISTS musik_album_neu ON musik_album (hinzugefuegt DESC)`,
		// Interpreten über alle Musikbibliotheken: derselbe Name ist
		// dieselbe Person. Was jemand davon sehen darf, entscheidet sich
		// über die Alben in seinen Bibliotheken.
		`CREATE TABLE IF NOT EXISTS musik_interpret (
		   id             TEXT PRIMARY KEY,
		   name           TEXT NOT NULL,
		   name_sort      TEXT NOT NULL)`,

		// ------------------------------------------------------ privat
		// Ab hier gehört jede Zeile einem Profil. Wie bei progress: keine
		// Uhrzeit, nur eine laufende Nummer je Profil, damit „Zuletzt
		// gehört" die Reihenfolge kennt — und eine Zahl, wie oft, für
		// „Beliebt". Wann und wie lange jemand gehört hat, steht nirgends.
		`CREATE TABLE IF NOT EXISTS musik_hoerstand (
		   account_id     TEXT NOT NULL REFERENCES account(id) ON DELETE CASCADE,
		   titel_id       TEXT NOT NULL REFERENCES musik_titel(id) ON DELETE CASCADE,
		   anzahl         INTEGER NOT NULL DEFAULT 0,
		   reihenfolge    INTEGER NOT NULL,
		   PRIMARY KEY (account_id, titel_id))`,
		`CREATE INDEX IF NOT EXISTS musik_hoerstand_reihe ON musik_hoerstand (account_id, reihenfolge DESC)`,
		`CREATE TABLE IF NOT EXISTS musik_hoerreihe (
		   account_id     TEXT PRIMARY KEY REFERENCES account(id) ON DELETE CASCADE,
		   reihe          INTEGER NOT NULL DEFAULT 0)`,
		// Herzen für Titel, Alben und Interpreten. Die Reihenfolge ergibt
		// sich aus der rowid — kein Zeitpunkt nötig.
		`CREATE TABLE IF NOT EXISTS musik_favorit (
		   account_id     TEXT NOT NULL REFERENCES account(id) ON DELETE CASCADE,
		   art            TEXT NOT NULL,
		   ziel_id        TEXT NOT NULL,
		   PRIMARY KEY (account_id, art, ziel_id))`,
		// Eigene Wiedergabelisten für Musik. Wie die Listen für Filme:
		// gehört einer Person, auf Wunsch geteilt und gemeinsam.
		`CREATE TABLE IF NOT EXISTS musik_wiedergabeliste (
		   id             TEXT PRIMARY KEY,
		   account_id     TEXT NOT NULL REFERENCES account(id) ON DELETE CASCADE,
		   name           TEXT NOT NULL,
		   geteilt        INTEGER NOT NULL DEFAULT 0,
		   gemeinsam      INTEGER NOT NULL DEFAULT 0,
		   angelegt       INTEGER NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS musik_wiedergabeliste_konto ON musik_wiedergabeliste (account_id)`,
		`CREATE TABLE IF NOT EXISTS musik_listeneintrag (
		   liste_id       TEXT NOT NULL REFERENCES musik_wiedergabeliste(id) ON DELETE CASCADE,
		   titel_id       TEXT NOT NULL REFERENCES musik_titel(id) ON DELETE CASCADE,
		   ord            INTEGER NOT NULL,
		   von_konto      TEXT NOT NULL REFERENCES account(id) ON DELETE CASCADE,
		   PRIMARY KEY (liste_id, titel_id))`,

		// ------------------------------------------------------ Fotos
		// Ein Ordner mit Fotos ist ein Album — so, wie die Leute ihre
		// Bilder ohnehin ablegen („2023 Sommerferien").
		`CREATE TABLE IF NOT EXISTS foto_album (
		   id             TEXT PRIMARY KEY,
		   library_id     TEXT NOT NULL REFERENCES library(id) ON DELETE CASCADE,
		   pfad           TEXT NOT NULL,
		   name           TEXT NOT NULL,
		   UNIQUE (library_id, pfad))`,
		`CREATE TABLE IF NOT EXISTS foto (
		   id             TEXT PRIMARY KEY,
		   library_id     TEXT NOT NULL REFERENCES library(id) ON DELETE CASCADE,
		   album_id       TEXT NOT NULL DEFAULT '',
		   pfad           TEXT NOT NULL UNIQUE,
		   groesse        INTEGER NOT NULL,
		   mtime          INTEGER NOT NULL,
		   art            TEXT NOT NULL,
		   format         TEXT NOT NULL,
		   aufnahme       INTEGER NOT NULL,
		   datum_quelle   TEXT NOT NULL DEFAULT 'datei',
		   breite         INTEGER NOT NULL DEFAULT 0,
		   hoehe          INTEGER NOT NULL DEFAULT 0,
		   ausrichtung    INTEGER NOT NULL DEFAULT 1,
		   kamera         TEXT NOT NULL DEFAULT '',
		   breitengrad    REAL,
		   laengengrad    REAL,
		   dauer_ms       INTEGER NOT NULL DEFAULT 0,
		   videocodec     TEXT NOT NULL DEFAULT '',
		   vorschau       INTEGER NOT NULL DEFAULT 0,
		   hinzugefuegt   INTEGER NOT NULL,
		   fehlt_seit     INTEGER)`,
		`CREATE INDEX IF NOT EXISTS foto_zeit ON foto (aufnahme DESC)`,
		`CREATE INDEX IF NOT EXISTS foto_album_idx ON foto (album_id, aufnahme)`,
		`CREATE INDEX IF NOT EXISTS foto_lib ON foto (library_id)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}
