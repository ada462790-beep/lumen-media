// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

// Aufräumen nach einem Durchlauf: leere Folgen und Staffeln, die ältere
// Fassungen zurückgelassen haben.

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

// FolgenOhneDatei entfernt Folgen, an denen keine einzige Datei mehr
// hängt, und danach Staffeln ohne Folgen.
//
// Solche Folgen entstanden bis zu dieser Fassung bei jedem Durchlauf:
// Dateien ohne Nummer im Namen („Staffel 1/Pilot.mkv") bekamen jedes Mal
// neue Nummern hinter den alten, die Datei wanderte zur neuen Folge, und
// die alte blieb leer stehen — mit „Ansehen"-Knopf und dem Gesehen-Haken
// der Leute. Was an einer leeren Folge hängt (Stand, Merkliste,
// Bewertung, Listen), wandert zur Folge derselben Staffel, die heute
// dieselbe Datei hat: gleicher Titel und als einzige so benannt. Gibt es
// keine solche, geht es mit der Folge.
//
// Eine Datei, die nur gerade fehlt, steht weiter in der Datenbank
// (missing_since); ihre Folge bleibt.
func (s *Store) FolgenOhneDatei(ctx context.Context) (int, error) {
	type leer struct{ id, staffel, titel string }
	rows, err := s.db.QueryContext(ctx,
		`SELECT e.id, COALESCE(e.parent_id,''), e.title FROM item e
		  WHERE e.kind = 'episode'
		    AND NOT EXISTS (SELECT 1 FROM media_file f WHERE f.item_id = e.id)`)
	if err != nil {
		return 0, err
	}
	var liste []leer
	for rows.Next() {
		var l leer
		if err := rows.Scan(&l.id, &l.staffel, &l.titel); err != nil {
			rows.Close()
			return 0, err
		}
		liste = append(liste, l)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	n := 0
	if len(liste) > 0 {
		konten, err := s.kontenIDs(ctx)
		if err != nil {
			return 0, err
		}
		err = s.tx(ctx, func(t *sql.Tx) error {
			for _, l := range liste {
				var ziel string
				// Genau eine Folge derselben Staffel mit gleichem Titel und
				// mindestens einer Datei.
				_ = t.QueryRowContext(ctx,
					`SELECT min(e.id) FROM item e
					  WHERE e.parent_id = ? AND e.kind = 'episode' AND e.title = ? AND e.id != ?
					    AND EXISTS (SELECT 1 FROM media_file f WHERE f.item_id = e.id)
					 HAVING count(*) = 1`, l.staffel, l.titel, l.id).Scan(&ziel)
				if ziel == "" {
					// Ein Special („S08E00 - The Last Watch") bekam früher die
					// nächste freie Nummer seiner Staffel; jetzt steht es in
					// Staffel 0 derselben Serie. Der Haken zieht mit.
					_ = t.QueryRowContext(ctx,
						`SELECT min(e.id) FROM item e
						   JOIN item sp ON sp.id = e.parent_id AND sp.kind = 'season' AND COALESCE(sp.season_no, 0) = 0
						   JOIN item st ON st.id = ? AND st.parent_id = sp.parent_id
						  WHERE e.kind = 'episode' AND e.title = ? AND e.id != ?
						    AND EXISTS (SELECT 1 FROM media_file f WHERE f.item_id = e.id)
						 HAVING count(*) = 1`, l.staffel, l.titel, l.id).Scan(&ziel)
				}
				if ziel != "" {
					if err := persoenlichesUmhaengen(ctx, t, l.id, ziel, konten); err != nil {
						return err
					}
				}
				if _, err := t.ExecContext(ctx, `DELETE FROM item WHERE id = ?`, l.id); err != nil {
					return err
				}
				n++
			}
			return nil
		})
		if err != nil {
			return 0, err
		}
	}
	// Staffeln ohne eine einzige Folge (und ohne eigene Datei).
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM item WHERE kind = 'season'
		    AND NOT EXISTS (SELECT 1 FROM item e WHERE e.parent_id = item.id)
		    AND NOT EXISTS (SELECT 1 FROM media_file f WHERE f.item_id = item.id)`)
	if err == nil {
		weg, _ := res.RowsAffected()
		n += int(weg)
	}
	return n, err
}

// FehlendVermerken vermerkt jede vorhandene Datei der Bibliothek als
// fehlend, die der Durchlauf nicht gesehen hat — ausser denen, für die
// verschonen true sagt (sie liegen unter einem Ordner, der sich nicht
// lesen liess). Zurück kommen die eben fehlend gewordenen Dateien und wie
// viele verschont blieben.
func (s *Store) FehlendVermerken(ctx context.Context, libraryID string, gesehen map[string]bool,
	verschonen func(string) bool) ([]MediaFile, int, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT f.id, f.item_id, f.path, f.size_bytes, f.mtime, f.part_no,
		        COALESCE(f.duration_ms, 0), COALESCE(f.edition, '')
		   FROM media_file f
		   JOIN item i ON i.id = f.item_id
		  WHERE i.library_id = ? AND f.missing_since IS NULL`, libraryID)
	if err != nil {
		return nil, 0, err
	}
	var weg []MediaFile
	verschont := 0
	for rows.Next() {
		var f MediaFile
		if err := rows.Scan(&f.ID, &f.ItemID, &f.Path, &f.SizeBytes, &f.MTime, &f.PartNo,
			&f.DurationMS, &f.Edition); err != nil {
			rows.Close()
			return nil, 0, err
		}
		if gesehen[f.Path] {
			continue
		}
		if verschonen != nil && verschonen(f.Path) {
			verschont++
			continue
		}
		weg = append(weg, f)
	}
	rows.Close()
	// Brach das Lesen mittendrin ab, fehlen in der Liste Dateien, die
	// gesehen wurden — und es fehlen andere. Lieber gar nichts vermerken.
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	// In einer Transaktion: Eine abgehängte Platte mit 10 000 Dateien
	// sind sonst 10 000 einzelne Schreibvorgänge.
	now := time.Now().Unix()
	err = s.tx(ctx, func(t *sql.Tx) error {
		for i := range weg {
			if _, err := t.ExecContext(ctx,
				`UPDATE media_file SET missing_since = ? WHERE id = ?`, now, weg[i].ID); err != nil {
				return err
			}
			weg[i].Missing = true
		}
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	return weg, verschont, nil
}

// FilmeImOrdner nennt die Filme einer Bibliothek, von denen eine Datei
// direkt in diesem Ordner liegt (nicht in Unterordnern) — älteste zuerst.
// Gesucht wird über den Index auf path (Bereich „ordner/…").
func (s *Store) FilmeImOrdner(ctx context.Context, libraryID, ordner string) ([]string, error) {
	ordner = strings.TrimRight(ordner, "/") + "/"
	rows, err := s.db.QueryContext(ctx,
		`SELECT f.path, i.id FROM media_file f JOIN item i ON i.id = f.item_id
		  WHERE f.path >= ? AND f.path < ? AND i.library_id = ? AND i.kind = 'movie'
		  ORDER BY i.added_at, i.id`, ordner, ordner[:len(ordner)-1]+"0", libraryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	schon := map[string]bool{}
	for rows.Next() {
		var p, id string
		if err := rows.Scan(&p, &id); err != nil {
			return nil, err
		}
		if strings.Contains(p[len(ordner):], "/") || schon[id] {
			continue
		}
		schon[id] = true
		out = append(out, id)
	}
	return out, rows.Err()
}

// FilmZusammenlegen hängt die Dateien von quelle an ziel, dazu je Konto
// Stand, Merkliste, Favoriten und Bewertung, und entfernt quelle.
func (s *Store) FilmZusammenlegen(ctx context.Context, ziel, quelle string) error {
	if ziel == quelle {
		return nil
	}
	konten, err := s.kontenIDs(ctx)
	if err != nil {
		return err
	}
	return s.tx(ctx, func(t *sql.Tx) error {
		if _, err := t.ExecContext(ctx, `UPDATE media_file SET item_id = ? WHERE item_id = ?`, ziel, quelle); err != nil {
			return err
		}
		if _, err := t.ExecContext(ctx,
			`UPDATE OR IGNORE collection_item SET item_id = ? WHERE item_id = ?`, ziel, quelle); err != nil {
			return err
		}
		if err := persoenlichesUmhaengen(ctx, t, quelle, ziel, konten); err != nil {
			return err
		}
		_, err := t.ExecContext(ctx, `DELETE FROM item WHERE id = ?`, quelle)
		return err
	})
}

// ---------------------------------------------------------------- Stand 14

// scanVerfuegbarkeitNachruesten legt die Spalten für defekte Dateien und
// fehlende Titel an (Schema-Stand 14). Steht eine Spalte schon da, ist das
// kein Fehler — wie in nachruesten.
func scanVerfuegbarkeitNachruesten(db *sql.DB) error {
	for _, stmt := range []string{
		// Warum sich eine Datei nicht ausmessen liess: leer, gesperrt,
		// kaputt. NULL = in Ordnung.
		`ALTER TABLE media_file ADD COLUMN defekt TEXT`,
		// Seit wann ein Film oder eine Serie keine abspielbare Datei mehr
		// hat (alle fehlen oder sind defekt). NULL = abspielbar.
		`ALTER TABLE item ADD COLUMN fehlt_seit INTEGER`,
	} {
		if _, err := db.Exec(stmt); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			return err
		}
	}
	for _, stmt := range []string{
		`CREATE INDEX IF NOT EXISTS file_defekt ON media_file (defekt) WHERE defekt IS NOT NULL`,
		// Personen- und Genre-Suche, ähnliche Titel (kennzahlen.go,
		// suche.go): von einem Namen bzw. Genre zu den Titeln.
		`CREATE INDEX IF NOT EXISTS besetzung_by_name ON besetzung (name, ord)`,
		`CREATE INDEX IF NOT EXISTS genre_by_name ON genre (name, item_id)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

// FehlendFrist: So lange bleibt ein Titel ohne abspielbare Datei für die
// Leute sichtbar (als „fehlt"). Danach blendet ihn die Mediathek aus; der
// Besitzer findet ihn unter Aufräumen und kann ihn endgültig entfernen.
// Eine Platte, die nur über Nacht weg war, nimmt so niemandem etwas weg.
const FehlendFrist = 24 * time.Hour

// spielbar: SQL-Bedingung — am Titel item (Film oder Serie) hängt eine
// Datei, die da und nicht defekt ist. Drei EXISTS statt eines OR-Joins,
// damit jeder Zweig den Index file_by_item bzw. item_by_parent nimmt.
const spielbar = `(
	   EXISTS (SELECT 1 FROM media_file f WHERE f.item_id = item.id
	            AND f.missing_since IS NULL AND f.defekt IS NULL)
	OR EXISTS (SELECT 1 FROM item e JOIN media_file f ON f.item_id = e.id
	            WHERE e.parent_id = item.id AND f.missing_since IS NULL AND f.defekt IS NULL)
	OR EXISTS (SELECT 1 FROM item st JOIN item e ON e.parent_id = st.id
	            JOIN media_file f ON f.item_id = e.id
	            WHERE st.parent_id = item.id AND f.missing_since IS NULL AND f.defekt IS NULL))`

// nurDefekt: SQL-Bedingung für einen Titel ohne abspielbare Datei
// (fehlt_seit gesetzt) — an ihm hängt eine vorhandene, aber defekte
// Datei. Zusammen mit fehlt_seit heisst das: Was da ist, ist defekt.
const nurDefekt = `(
	   EXISTS (SELECT 1 FROM media_file f WHERE f.item_id = i.id
	            AND f.missing_since IS NULL AND f.defekt IS NOT NULL)
	OR EXISTS (SELECT 1 FROM item e JOIN media_file f ON f.item_id = e.id
	            WHERE e.parent_id = i.id AND f.missing_since IS NULL AND f.defekt IS NOT NULL)
	OR EXISTS (SELECT 1 FROM item st JOIN item e ON e.parent_id = st.id
	            JOIN media_file f ON f.item_id = e.id
	            WHERE st.parent_id = i.id AND f.missing_since IS NULL AND f.defekt IS NOT NULL))`

// VerfuegbarkeitNachfuehren setzt fehlt_seit für die Filme und Serien
// einer Bibliothek (leer = alle): ab jetzt, wenn keine Datei mehr
// abspielbar ist, und zurück auf NULL, sobald wieder eine da ist.
func (s *Store) VerfuegbarkeitNachfuehren(ctx context.Context, libraryID string) error {
	// Nur Zeilen, deren Zustand wechselt: Ein Durchlauf ohne Änderungen
	// schreibt nichts (die Platte der Datenbank darf schlafen).
	q := `UPDATE item SET fehlt_seit = CASE WHEN fehlt_seit IS NULL THEN ? ELSE NULL END
	       WHERE kind IN ('movie','series')
	         AND CASE WHEN ` + spielbar + ` THEN fehlt_seit IS NOT NULL ELSE fehlt_seit IS NULL END`
	args := []any{time.Now().Unix()}
	if libraryID != "" {
		q += ` AND library_id = ?`
		args = append(args, libraryID)
	}
	_, err := s.db.ExecContext(ctx, q, args...)
	return err
}

// ---------------------------------------------------------------- Defekte Dateien

// DefekteDatei ist eine Zeile der Liste unter Aufräumen.
type DefekteDatei struct {
	ID        string `json:"id"`
	ItemID    string `json:"itemId"`
	LibraryID string `json:"libraryId"`
	Path      string `json:"path"`
	Grund     string `json:"grund"`
}

// DefekteDateien: vorhandene Dateien, die sich nicht ausmessen liessen
// (höchstens grenze).
func (s *Store) DefekteDateien(ctx context.Context, grenze int) ([]DefekteDatei, error) {
	if grenze <= 0 {
		grenze = 500
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT f.id, f.item_id, i.library_id, f.path, f.defekt FROM media_file f
		   JOIN item i ON i.id = f.item_id
		  WHERE f.defekt IS NOT NULL AND f.missing_since IS NULL
		  ORDER BY f.path LIMIT ?`, grenze)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DefekteDatei{}
	for rows.Next() {
		var d DefekteDatei
		if err := rows.Scan(&d.ID, &d.ItemID, &d.LibraryID, &d.Path, &d.Grund); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------- Umbenannt

// DateiUmziehen: neu ist eine umbenannte oder verschobene alt. Hängen
// beide am selben Titel, verschwindet nur die alte Zeile. Hat der
// Durchlauf für neu einen eigenen Film angelegt (nur diese eine Datei),
// kommt neu an den alten Film — mit Merkliste, Stand und Bewertungen —,
// und der neue Eintrag verschwindet. Nur Filme; bei Folgen nur der erste
// Fall. umgezogen sagt, ob etwas geschah.
func (s *Store) DateiUmziehen(ctx context.Context, altID, neuID string) (umgezogen bool, err error) {
	konten, err := s.kontenIDs(ctx)
	if err != nil {
		return false, err
	}
	err = s.tx(ctx, func(t *sql.Tx) error {
		var altItem, neuItem, altArt, neuArt string
		if err := t.QueryRowContext(ctx,
			`SELECT f.item_id, i.kind FROM media_file f JOIN item i ON i.id = f.item_id
			  WHERE f.id = ? AND f.missing_since IS NOT NULL`, altID).Scan(&altItem, &altArt); err != nil {
			return nil // nicht (mehr) fehlend: nichts zu tun
		}
		if err := t.QueryRowContext(ctx,
			`SELECT f.item_id, i.kind FROM media_file f JOIN item i ON i.id = f.item_id
			  WHERE f.id = ? AND f.missing_since IS NULL`, neuID).Scan(&neuItem, &neuArt); err != nil {
			return nil
		}
		if altItem == neuItem {
			_, err := t.ExecContext(ctx, `DELETE FROM media_file WHERE id = ?`, altID)
			umgezogen = err == nil
			return err
		}
		if altArt != "movie" || neuArt != "movie" {
			return nil
		}
		var neuDateien, altSonst int
		_ = t.QueryRowContext(ctx, `SELECT count(*) FROM media_file WHERE item_id = ?`, neuItem).Scan(&neuDateien)
		_ = t.QueryRowContext(ctx, `SELECT count(*) FROM media_file WHERE item_id = ? AND id != ?
		                             AND missing_since IS NULL`, altItem, altID).Scan(&altSonst)
		// Hat der alte Film noch andere Fassungen, war es vielleicht
		// Absicht, die eine als eigenen Film zu führen — dann bleibt es.
		if neuDateien != 1 || altSonst != 0 {
			return nil
		}
		if _, err := t.ExecContext(ctx, `UPDATE media_file SET item_id = ? WHERE id = ?`, altItem, neuID); err != nil {
			return err
		}
		if _, err := t.ExecContext(ctx, `DELETE FROM media_file WHERE id = ?`, altID); err != nil {
			return err
		}
		if err := persoenlichesUmhaengen(ctx, t, neuItem, altItem, konten); err != nil {
			return err
		}
		if _, err := t.ExecContext(ctx, `DELETE FROM item WHERE id = ?`, neuItem); err != nil {
			return err
		}
		umgezogen = true
		return nil
	})
	return umgezogen, err
}

// ---------------------------------------------------------------- Endgültig entfernen

// FehlendeEntfernen trägt fehlende Dateien endgültig aus — die genannten,
// oder (ids leer) alle, die länger als die Frist fehlen — und danach
// Filme, Folgen und Serien, an denen nichts mehr hängt. Zurück kommt die
// Zahl der ausgetragenen Dateien. Die Dateien selbst rührt Lumen nicht an.
func (s *Store) FehlendeEntfernen(ctx context.Context, ids []string) (int, error) {
	var res sql.Result
	var err error
	if len(ids) == 0 {
		res, err = s.db.ExecContext(ctx,
			`DELETE FROM media_file WHERE missing_since IS NOT NULL AND missing_since < ?`,
			time.Now().Add(-FehlendFrist).Unix())
	} else {
		res, err = s.db.ExecContext(ctx,
			`DELETE FROM media_file WHERE missing_since IS NOT NULL AND id IN (`+placeholders(len(ids))+`)`,
			toAny(ids)...)
	}
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	if _, err := s.FilmeOhneDatei(ctx); err != nil {
		return int(n), err
	}
	if _, err := s.FolgenOhneDatei(ctx); err != nil {
		return int(n), err
	}
	// Serien, an denen keine Staffel mehr hängt.
	if _, err := s.db.ExecContext(ctx,
		`DELETE FROM item WHERE kind = 'series'
		    AND NOT EXISTS (SELECT 1 FROM item st WHERE st.parent_id = item.id)
		    AND NOT EXISTS (SELECT 1 FROM media_file f WHERE f.item_id = item.id)`); err != nil {
		return int(n), err
	}
	return int(n), nil
}
