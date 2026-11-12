// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

// Was der Scanner neben dem Titel selbst weiss: das Land einer Serie
// („The Office (US)"), ob die Folgen durchgezählt sind (Anime), wie weit
// eine Doppelfolge reicht — und welche NFO-Dateien und Bilder neben den
// Videodateien liegen. In eigenen Tabellen, damit die Spalten von „item"
// so bleiben, wie sie sind: Es sind Angaben über die Ablage, nicht über
// den Titel.

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// lokalTabellen legt die Tabellen an; nachruesten ruft das auf.
var lokalTabellen = []string{
	`CREATE TABLE IF NOT EXISTS item_zusatz (
	   item_id   TEXT PRIMARY KEY REFERENCES item(id) ON DELETE CASCADE,
	   land      TEXT NOT NULL DEFAULT '',
	   absolut   INTEGER NOT NULL DEFAULT 0,
	   bis_folge INTEGER NOT NULL DEFAULT 0)`,
	// Die NFO eines Titels, wie sie beim letzten Lesen war — als JSON.
	// Die Änderungszeit sagt, ob sie neu gelesen werden muss.
	`CREATE TABLE IF NOT EXISTS lokal_nfo (
	   item_id TEXT PRIMARY KEY REFERENCES item(id) ON DELETE CASCADE,
	   pfad    TEXT NOT NULL,
	   mtime   INTEGER NOT NULL,
	   daten   TEXT NOT NULL)`,
	// Bilder neben der Datei, die in den Bilderordner übernommen wurden.
	`CREATE TABLE IF NOT EXISTS lokal_bild (
	   item_id TEXT NOT NULL REFERENCES item(id) ON DELETE CASCADE,
	   art     TEXT NOT NULL,
	   pfad    TEXT NOT NULL,
	   mtime   INTEGER NOT NULL,
	   groesse INTEGER NOT NULL,
	   PRIMARY KEY (item_id, art))`,
}

// Zusatz sind die Angaben aus item_zusatz.
type Zusatz struct {
	Land     string
	Absolut  bool
	BisFolge int
}

// ZusatzSetzen hält Land, durchgezählte Nummer und Ende einer Doppelfolge
// fest. Ein leerer Zusatz löscht die Zeile — die meisten Titel brauchen
// keine.
func (s *Store) ZusatzSetzen(ctx context.Context, itemID string, z Zusatz) error {
	if z == (Zusatz{}) {
		_, err := s.db.ExecContext(ctx, `DELETE FROM item_zusatz WHERE item_id = ?`, itemID)
		return err
	}
	abs := 0
	if z.Absolut {
		abs = 1
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO item_zusatz (item_id, land, absolut, bis_folge) VALUES (?,?,?,?)
		 ON CONFLICT (item_id) DO UPDATE SET land = excluded.land,
		   absolut = excluded.absolut, bis_folge = excluded.bis_folge`,
		itemID, z.Land, abs, z.BisFolge)
	return err
}

// ZusatzZu liest den Zusatz eines Titels (leer, wenn keiner da ist).
func (s *Store) ZusatzZu(ctx context.Context, itemID string) Zusatz {
	var z Zusatz
	var abs int
	err := s.db.QueryRowContext(ctx,
		`SELECT land, absolut, bis_folge FROM item_zusatz WHERE item_id = ?`, itemID).
		Scan(&z.Land, &abs, &z.BisFolge)
	if err != nil {
		return Zusatz{}
	}
	z.Absolut = abs == 1
	return z
}

// LokaleNFO ist die zuletzt gelesene NFO eines Titels.
type LokaleNFO struct {
	Pfad  string
	MTime int64
	Daten string // JSON von lokal.NFO
}

// LokaleNFOZu liest die gemerkte NFO eines Titels.
func (s *Store) LokaleNFOZu(ctx context.Context, itemID string) (LokaleNFO, bool) {
	var n LokaleNFO
	err := s.db.QueryRowContext(ctx,
		`SELECT pfad, mtime, daten FROM lokal_nfo WHERE item_id = ?`, itemID).
		Scan(&n.Pfad, &n.MTime, &n.Daten)
	return n, err == nil
}

// LokaleNFOSetzen merkt sich eine gelesene NFO.
func (s *Store) LokaleNFOSetzen(ctx context.Context, itemID string, n LokaleNFO) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO lokal_nfo (item_id, pfad, mtime, daten) VALUES (?,?,?,?)
		 ON CONFLICT (item_id) DO UPDATE SET pfad = excluded.pfad,
		   mtime = excluded.mtime, daten = excluded.daten`,
		itemID, n.Pfad, n.MTime, n.Daten)
	return err
}

// LokaleNFOWeg vergisst die NFO eines Titels (sie liegt nicht mehr da).
func (s *Store) LokaleNFOWeg(ctx context.Context, itemID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM lokal_nfo WHERE item_id = ?`, itemID)
	return err
}

// LokalesBild ist ein übernommenes Bild: woher, wie alt, wie gross.
type LokalesBild struct {
	Pfad    string
	MTime   int64
	Groesse int64
}

// LokalesBildZu liest, welches Bild zuletzt für diese Art übernommen wurde.
func (s *Store) LokalesBildZu(ctx context.Context, itemID, art string) (LokalesBild, bool) {
	var b LokalesBild
	err := s.db.QueryRowContext(ctx,
		`SELECT pfad, mtime, groesse FROM lokal_bild WHERE item_id = ? AND art = ?`, itemID, art).
		Scan(&b.Pfad, &b.MTime, &b.Groesse)
	return b, err == nil
}

// LokalesBildSetzen merkt sich ein übernommenes Bild und trägt es als
// Bild des Titels ein. Die Quelle heisst „lokal" — daran erkennt die
// Zuordnung, dass sie es nicht durch eines von TMDb ersetzen darf. Der
// Pfad auf der Platte gehört nicht dorthin: Die Bildquelle kann
// irgendwann über die Leitung gehen.
func (s *Store) LokalesBildSetzen(ctx context.Context, itemID, art, datei string, b LokalesBild) error {
	return s.tx(ctx, func(t *sql.Tx) error {
		if _, err := t.ExecContext(ctx,
			`INSERT INTO lokal_bild (item_id, art, pfad, mtime, groesse) VALUES (?,?,?,?,?)
			 ON CONFLICT (item_id, art) DO UPDATE SET pfad = excluded.pfad,
			   mtime = excluded.mtime, groesse = excluded.groesse`,
			itemID, art, b.Pfad, b.MTime, b.Groesse); err != nil {
			return err
		}
		_, err := t.ExecContext(ctx,
			`INSERT INTO artwork (item_id, kind, path, source_url) VALUES (?,?,?,'lokal')
			 ON CONFLICT (item_id, kind) DO UPDATE SET path = excluded.path, source_url = 'lokal'`,
			itemID, art, datei)
		return err
	})
}

// LokaleBildArten nennt die Bildarten eines Titels, die aus der
// Sammlung selbst stammen und darum Vorrang vor TMDb haben.
func (s *Store) LokaleBildArten(ctx context.Context, itemID string) map[string]bool {
	out := map[string]bool{}
	rows, err := s.db.QueryContext(ctx,
		`SELECT kind FROM artwork WHERE item_id = ? AND source_url = 'lokal'`, itemID)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var k string
		if rows.Scan(&k) == nil {
			out[k] = true
		}
	}
	return out
}

// DateienAustragenWo nimmt alle Dateien einer Bibliothek aus der
// Datenbank, für deren Pfad weg(pfad) gilt — etwa alles, was in einem
// Papierkorb des NAS liegt („#recycle"). Folgen und Staffeln, an denen
// danach keine Datei mehr hängt, verschwinden mit; Filme und Serien ohne
// Datei räumt der Hausmeister danach ohnehin auf (FilmeOhneDatei,
// SerienZusammenfuehren). Die Dateien selbst bleiben, wo sie sind.
func (s *Store) DateienAustragenWo(ctx context.Context, libraryID string, weg func(pfad string) bool) (int, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT f.id, f.path, f.item_id FROM media_file f JOIN item i ON i.id = f.item_id
		  WHERE i.library_id = ?`, libraryID)
	if err != nil {
		return 0, err
	}
	type datei struct{ id, item string }
	var raus []datei
	for rows.Next() {
		var d datei
		var p string
		if err := rows.Scan(&d.id, &p, &d.item); err != nil {
			rows.Close()
			return 0, err
		}
		if weg(p) {
			raus = append(raus, d)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(raus) == 0 {
		return 0, nil
	}
	err = s.tx(ctx, func(t *sql.Tx) error {
		for _, d := range raus {
			if _, err := t.ExecContext(ctx, `DELETE FROM media_file WHERE id = ?`, d.id); err != nil {
				return err
			}
			// Eine Folge ohne Datei ist ein leerer Eintrag in der Staffel;
			// die Staffel ohne Folge ebenso.
			var parent sql.NullString
			var kind string
			err := t.QueryRowContext(ctx, `SELECT kind, parent_id FROM item WHERE id = ?`, d.item).Scan(&kind, &parent)
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			if err != nil {
				return err
			}
			if kind != "episode" {
				continue
			}
			if _, err := t.ExecContext(ctx,
				`DELETE FROM item WHERE id = ? AND NOT EXISTS (SELECT 1 FROM media_file WHERE item_id = ?)`,
				d.item, d.item); err != nil {
				return err
			}
			if parent.Valid {
				if _, err := t.ExecContext(ctx,
					`DELETE FROM item WHERE id = ? AND kind = 'season'
					   AND NOT EXISTS (SELECT 1 FROM item WHERE parent_id = ?)`,
					parent.String, parent.String); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return len(raus), nil
}

// IstErkannt: Der Titel hat seine Angaben aus einer Quelle (TMDb, von
// Hand oder aus einer lokalen NFO) und behält seinen Namen, auch wenn der
// Dateiname etwas anderes sagt.
func IstErkannt(state string) bool {
	switch strings.TrimSpace(state) {
	case "matched", "manual", "lokal":
		return true
	}
	return false
}

// FreigabenNachrechnen rechnet die Altersgrenze für Titel neu, die ein
// Kennzeichen haben, aber keine Zahl dazu — etwa „DE-18" aus einer
// Emby-NFO, das ältere Fassungen nicht lesen konnten. alter ist
// meta.AgeYears. Gibt zurück, wie viele Titel jetzt eine Grenze haben.
func (s *Store) FreigabenNachrechnen(ctx context.Context, alter func(string) *int) (int, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, age_rating FROM item WHERE age_years IS NULL AND COALESCE(age_rating, '') != ''`)
	if err != nil {
		return 0, err
	}
	neu := map[string]int{}
	for rows.Next() {
		var id, kz string
		if err := rows.Scan(&id, &kz); err != nil {
			rows.Close()
			return 0, err
		}
		if j := alter(kz); j != nil {
			neu[id] = *j
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	for id, j := range neu {
		if _, err := s.db.ExecContext(ctx,
			`UPDATE item SET age_years = ? WHERE id = ? AND age_years IS NULL`, j, id); err != nil {
			return 0, err
		}
	}
	return len(neu), nil
}

// NFOZeile: ein Titel mit seiner gemerkten NFO (JSON von lokal.NFO).
type NFOZeile struct {
	ID, Kind, Daten string
}

// AusstehendMitNFO nennt Filme, Serien und Folgen, die auf die Zuordnung
// warten und eine NFO haben.
func (s *Store) AusstehendMitNFO(ctx context.Context) ([]NFOZeile, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT i.id, i.kind, n.daten FROM item i JOIN lokal_nfo n ON n.item_id = i.id
		  WHERE i.match_state = 'pending' AND i.kind IN ('movie','series','episode')`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []NFOZeile
	for rows.Next() {
		var z NFOZeile
		if err := rows.Scan(&z.ID, &z.Kind, &z.Daten); err != nil {
			return nil, err
		}
		out = append(out, z)
	}
	return out, rows.Err()
}

// ZuordnungsZustandSetzen setzt den Zustand der Zuordnung für Titel, die
// nicht von Hand zugewiesen sind.
func (s *Store) ZuordnungsZustandSetzen(ctx context.Context, ids []string, zustand string) (int64, error) {
	var n int64
	err := s.tx(ctx, func(t *sql.Tx) error {
		for _, id := range ids {
			res, err := t.ExecContext(ctx,
				`UPDATE item SET match_state = ? WHERE id = ? AND match_state NOT IN ('manual', ?)`, zustand, id, zustand)
			if err != nil {
				return err
			}
			k, _ := res.RowsAffected()
			n += k
		}
		return nil
	})
	return n, err
}

// LokaleMitKennungNachholen: Filme und Serien, die ohne TMDb-Schlüssel als
// „lokal" galten, aber eine Kennung haben, warten wieder auf die
// Zuordnung — jetzt, wo ein Schlüssel da ist. Ihre Folgen ebenso.
func (s *Store) LokaleMitKennungNachholen(ctx context.Context) (int64, error) {
	var n int64
	err := s.tx(ctx, func(t *sql.Tx) error {
		res, err := t.ExecContext(ctx, `UPDATE item SET match_state = 'pending'
			 WHERE match_state = 'lokal' AND kind IN ('movie','series')
			   AND (COALESCE(tmdb_id,'') != '' OR COALESCE(imdb_id,'') != '' OR COALESCE(tvdb_id,'') != '')`)
		if err != nil {
			return err
		}
		n, _ = res.RowsAffected()
		_, err = t.ExecContext(ctx, `UPDATE item SET match_state = 'pending'
			 WHERE match_state = 'lokal' AND kind = 'episode' AND EXISTS (
			   SELECT 1 FROM item st JOIN item se ON se.id = st.parent_id
			    WHERE st.id = item.parent_id AND se.match_state = 'pending')`)
		return err
	})
	return n, err
}

// PosterStand zählt Filme und Serien und wie viele davon ein Poster haben
// (aus der Sammlung oder von TMDb) — für den Hinweis in der Verwaltung.
func (s *Store) PosterStand(ctx context.Context) (titel, mitPoster int) {
	_ = s.db.QueryRowContext(ctx,
		`SELECT count(*), COALESCE(sum(EXISTS (SELECT 1 FROM artwork a
		          WHERE a.item_id = i.id AND a.kind = 'poster' AND COALESCE(a.path,'') != '')), 0)
		   FROM item i WHERE i.kind IN ('movie','series')`).Scan(&titel, &mitPoster)
	return titel, mitPoster
}

// BisFolgen nennt für Doppelfolgen das Ende („S01E01-E02" → 2). Folgen
// ohne Eintrag fehlen in der Antwort.
func (s *Store) BisFolgen(ctx context.Context, ids []string) map[string]int {
	out := map[string]int{}
	if len(ids) == 0 {
		return out
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT item_id, bis_folge FROM item_zusatz WHERE bis_folge > 0 AND item_id IN (`+placeholders(len(ids))+`)`, args...)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var n int
		if rows.Scan(&id, &n) == nil {
			out[id] = n
		}
	}
	return out
}
