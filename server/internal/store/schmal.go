// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

// Schmale Indizes: Was die Seiten über den Aufbau der Mediathek wissen
// müssen, steht in Indizes, die alles Nötige selbst enthalten.
//
// Ein NAS meldete nach einigen Stunden Ruhe 9,7 s für die Startseite,
// 5,7 s für „Poster und Beschreibungen" — fast nur Lesezeit. Die Zeile
// eines Titels trägt seine Beschreibung mit; in eine Seite der Datei
// passen acht bis zehn davon. Fragte eine Abfrage „zu welcher Serie gehört
// diese Folge?" für die viertausend Folgen eines Profils, las SQLite dafür
// die Zeilen der Folgen — verstreut über die ganze Tabelle, auf einer
// schlafenden Platte jede Seite 5–10 ms. Die Indizes hier enthalten nur
// Kennung, Art, Eltern, Nummern, Hinzufügen und Freigabe: dieselbe Frage
// liest dann eine Handvoll Seiten eines Index, der kaum ein Megabyte gross
// ist.
//
// Dazu bekommt jede Datei die meisten Kanäle und die Sprache ihrer ersten
// Tonspur als Spalten (ton_kanaele, ton_sprache). Die Abzeichen der Karten
// (kennzahlen.go) und die Sprachvorschläge beim Aufräumen
// (fassung_vorschlag.go) mussten sonst für jede Datei die Spuren lesen.

import (
	"database/sql"
	"strings"
)

// itemByParentSQL: Kinder eines Titels in der Reihenfolge der Folgen, mit
// Art, Hinzufügen und Kennung — Staffeln, Folgen und deren Zahl lassen
// sich so ohne die Zeilen selbst lesen. Der Name bleibt (INDEXED BY in
// kennzahlen.go und playback.go); ältere Datenbanken haben ihn noch mit
// drei Spalten (schmalNachruesten).
const itemByParentSQL = `CREATE INDEX IF NOT EXISTS item_by_parent ON item (parent_id, season_no, episode_no, kind, added_at, id)`

// sichtSpalten: was die Bedingung eines Profils prüft (bedingung in
// library.go) — Bibliothek, fehlende Datei, Freigabe samt Eltern. Hängen
// sie hinten an einem Sortier-Index, liest eine Seite des Rasters nur den
// Index und danach die Zeilen der gezeigten Titel; vorher las SQLite für
// jeden Titel, den es beim Sortieren ansah, die ganze Zeile.
const sichtSpalten = `library_id, fehlt_seit, age_years, parent_id`

// ordIndizes: je Sortierung des Rasters ein Index (sortieren.go,
// sqlOrdnung; die Ausdrücke müssen dort und hier gleich lauten), jeder mit
// sichtSpalten.
var ordIndizes = [][2]string{
	{"item_ord_titel", `(kind, sort_title, id, ` + sichtSpalten + `)`},
	{"item_ord_neu", `(kind, zuletzt_neu DESC, sort_title, id, ` + sichtSpalten + `)`},
	{"item_ord_erschienen", `(kind, COALESCE(premiered, CAST(year AS TEXT)) DESC, sort_title, id, ` + sichtSpalten + `)`},
	{"item_ord_wertung", `(kind, imdb_rating DESC, sort_title, id, ` + sichtSpalten + `)`},
	{"item_ord_laufzeit", `(kind, runtime_ms IS NULL, runtime_ms, sort_title, id, ` + sichtSpalten + `)`},
}

// ordIndizesAnlegen legt die Sortier-Indizes an und ersetzt solche ohne
// sichtSpalten (bis Stand 17).
func ordIndizesAnlegen(db *sql.DB) error {
	for _, x := range ordIndizes {
		var def string
		_ = db.QueryRow(`SELECT COALESCE(sql, '') FROM sqlite_master WHERE type = 'index' AND name = ?`, x[0]).Scan(&def)
		if def != "" && !strings.Contains(def, "fehlt_seit") {
			if _, err := db.Exec(`DROP INDEX ` + x[0]); err != nil {
				return err
			}
		}
		if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS ` + x[0] + ` ON item ` + x[1]); err != nil {
			return err
		}
	}
	return nil
}

// schmalNachruesten: Stand 18 (store.go). Spalten und Indizes; steht
// etwas schon da, ist das kein Fehler.
func schmalNachruesten(db *sql.DB) error {
	for _, stmt := range []string{
		`ALTER TABLE media_file ADD COLUMN ton_kanaele INTEGER`,
		`ALTER TABLE media_file ADD COLUMN ton_sprache TEXT`,
	} {
		if _, err := db.Exec(stmt); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			return err
		}
	}
	// item_by_parent mit drei Spalten (schema.sql bis 0.1.28) ersetzen.
	var def string
	_ = db.QueryRow(`SELECT COALESCE(sql, '') FROM sqlite_master WHERE type = 'index' AND name = 'item_by_parent'`).Scan(&def)
	if def != "" && !strings.Contains(def, "added_at") {
		if _, err := db.Exec(`DROP INDEX item_by_parent`); err != nil {
			return err
		}
	}
	for _, stmt := range []string{
		itemByParentSQL,
		// Von der Kennung zu Art, Eltern, Nummern, Hinzufügen, Freigabe,
		// Bibliothek und Sortiertitel — für „zu welcher Serie gehört diese
		// Folge", die Altersgrenze über Staffel und Serie, die Wege vom
		// Fortschritt eines Profils zur Mediathek und „welche dieser Titel
		// darf das Profil sehen" (Merkliste, Weiterschauen, Reihen).
		`CREATE INDEX IF NOT EXISTS item_baum ON item (id, kind, parent_id, season_no, episode_no, added_at, library_id, fehlt_seit, age_years, sort_title)`,
		// Die Abzeichen einer Datei: von der Kennung des Titels aus, ohne
		// die Zeile der Datei.
		`CREATE INDEX IF NOT EXISTS file_kennzahl ON media_file (item_id, missing_since, height, width, hdr, ton_kanaele)`,
		// Hat ein Titel ein Poster? (Verwaltung › Poster und
		// Beschreibungen zählt das für jeden Film und jede Serie.) Die
		// Zeilen von artwork tragen lange Adressen und den Blurhash; dieser
		// Index nur Art, Kennung und Dateiname.
		`CREATE INDEX IF NOT EXISTS artwork_da ON artwork (kind, item_id, path)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

// tonFuellen trägt ton_kanaele und ton_sprache für alle Dateien nach,
// deren Spuren schon gemessen sind (umbauen, Stand 18). Danach hält
// spurenSchreiben sie aktuell.
func tonFuellen(db *sql.DB) error {
	_, err := db.Exec(`UPDATE media_file SET
	    ton_kanaele = (SELECT MAX(channels) FROM media_track t WHERE t.file_id = media_file.id AND t.kind = 'audio'),
	    ton_sprache = (SELECT t.language FROM media_track t WHERE t.file_id = media_file.id AND t.kind = 'audio'
	                    ORDER BY t.idx LIMIT 1)`)
	return err
}

// tonAusSpuren: die meisten Kanäle einer Tonspur und die Sprache der
// ersten (kleinster Index) — wie tonFuellen, für spurenSchreiben. Ohne
// Tonspur beide NULL.
func tonAusSpuren(tracks []Track) (kanaele, sprache any) {
	erste := -1
	for _, tr := range tracks {
		if tr.Kind != "audio" || tr.ExternalPath != "" {
			continue
		}
		if k, ok := kanaele.(int); !ok || tr.Channels > k {
			kanaele = tr.Channels
		}
		if erste < 0 || tr.Index < erste {
			erste, sprache = tr.Index, tr.Language
		}
	}
	return kanaele, sprache
}
