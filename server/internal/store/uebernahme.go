// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

// Übernahme aus Emby oder Jellyfin.
//
// Zwei Hälften mit verschiedenen Regeln:
//
//   - UebernahmeKatalog liest die Mediathek (Titel, Kennungen, Dateien)
//     — das beschreibt die Sammlung, nicht wer was schaut.
//   - UebernahmeAnwenden schreibt in Fortschritt, Favoriten und
//     Bewertungen. Das gehört genau einem Profil: Die Funktion nimmt das
//     Konto als ersten Parameter, und jede Abfrage nennt account_id — wie
//     in playback.go (privacy_test.go prüft beides auch für diese Datei).

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// KatalogTitel ist ein Titel, wie die Zuordnung ihn braucht.
type KatalogTitel struct {
	ID, Kind, Titel, Original string
	Jahr                      int
	TMDb, IMDb, TVDb          string
	SerieID                   string
	Staffel, Folge            int
	LaengeMS                  int64
	// Für die Freigabe: Bibliothek und Altersfreigabe des obersten Titels
	// (bei Folgen die der Serie).
	LibraryID string
	AgeYears  *int
}

// KatalogDatei ist eine Datei samt Grösse.
type KatalogDatei struct {
	ItemID  string
	Pfad    string
	Groesse int64
}

// UebernahmeKatalog liest alle Filme, Serien und Folgen samt Dateien.
func (s *Store) UebernahmeKatalog(ctx context.Context) ([]KatalogTitel, []KatalogDatei, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT i.id, i.kind, i.title, COALESCE(i.original_title,''), COALESCE(i.year,0),
		       COALESCE(i.tmdb_id,''), COALESCE(i.imdb_id,''), COALESCE(i.tvdb_id,''),
		       COALESCE(st.parent_id,''), COALESCE(i.season_no,0), COALESCE(i.episode_no,0),
		       COALESCE(i.runtime_ms, (SELECT max(f.duration_ms) FROM media_file f WHERE f.item_id = i.id), 0),
		       COALESCE(se.library_id, i.library_id),
		       CASE WHEN i.kind = 'episode' THEN se.age_years ELSE i.age_years END
		  FROM item i
		  LEFT JOIN item st ON st.id = i.parent_id AND i.kind = 'episode'
		  LEFT JOIN item se ON se.id = st.parent_id
		 WHERE i.kind IN ('movie','series','episode')`)
	if err != nil {
		return nil, nil, err
	}
	var titel []KatalogTitel
	for rows.Next() {
		var t KatalogTitel
		var alter sql.NullInt64
		if err := rows.Scan(&t.ID, &t.Kind, &t.Titel, &t.Original, &t.Jahr, &t.TMDb, &t.IMDb, &t.TVDb,
			&t.SerieID, &t.Staffel, &t.Folge, &t.LaengeMS, &t.LibraryID, &alter); err != nil {
			rows.Close()
			return nil, nil, err
		}
		if alter.Valid {
			v := int(alter.Int64)
			t.AgeYears = &v
		}
		titel = append(titel, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	frows, err := s.db.QueryContext(ctx,
		`SELECT item_id, path, size_bytes FROM media_file WHERE missing_since IS NULL`)
	if err != nil {
		return nil, nil, err
	}
	defer frows.Close()
	var dateien []KatalogDatei
	for frows.Next() {
		var d KatalogDatei
		if err := frows.Scan(&d.ItemID, &d.Pfad, &d.Groesse); err != nil {
			return nil, nil, err
		}
		dateien = append(dateien, d)
	}
	return titel, dateien, frows.Err()
}

// UebernahmeEintrag ist der Stand eines Titels aus Emby.
type UebernahmeEintrag struct {
	ItemID     string
	Gesehen    bool
	PositionMS int64
	DauerMS    int64
	Favorit    bool
	Sterne     int
}

// UebernahmeErgebnis zählt, was sich geändert hat — und was schon so war.
type UebernahmeErgebnis struct {
	Gesehen     int `json:"gesehen"`
	Angefangen  int `json:"angefangen"`
	Favoriten   int `json:"favoriten"`
	Bewertungen int `json:"bewertungen"`
	// Unveraendert: schon in Lumen (gesehen, weiter oder bewertet) —
	// nichts überschrieben.
	Unveraendert int `json:"unveraendert"`
}

// UebernahmeArten wählt, was übernommen wird.
type UebernahmeArten struct {
	Gesehen, Angefangen, Favoriten, Bewertungen bool
}

// UebernahmeAnwenden übernimmt den Stand in dieses eine Profil — alles
// oder nichts, in einer Transaktion. Mit probe = true wird nur gezählt
// und am Ende zurückgerollt (für die Vorschau).
//
// Was in Lumen schon steht, wird nie verschlechtert:
//   - „gesehen" wird nie zurückgenommen;
//   - eine Stelle wird nur übernommen, wenn sie weiter ist als die in
//     Lumen — Lumen merkt sich keine Uhrzeiten, also gilt die weitere
//     Stelle als die neuere;
//   - Favoriten kommen dazu, keiner geht weg;
//   - eine vorhandene Bewertung bleibt.
func (s *Store) UebernahmeAnwenden(ctx context.Context, accountID string, liste []UebernahmeEintrag,
	arten UebernahmeArten, probe bool) (UebernahmeErgebnis, error) {
	var erg UebernahmeErgebnis
	t, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return erg, err
	}
	defer func() { _ = t.Rollback() }()

	for _, e := range liste {
		if e.ItemID == "" {
			continue
		}
		var pos, dauer int64
		var fertig int
		err := t.QueryRowContext(ctx,
			`SELECT position_ms, duration_ms, finished FROM progress WHERE account_id = ? AND item_id = ?`,
			accountID, e.ItemID).Scan(&pos, &dauer, &fertig)
		vorhanden := err == nil
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return erg, err
		}
		if dauer <= 0 {
			dauer = e.DauerMS
		}

		switch {
		case e.Gesehen && arten.Gesehen:
			if vorhanden && fertig == 1 {
				erg.Unveraendert++
				break
			}
			reihe, err := naechsteReihe(ctx, t, accountID, e.ItemID)
			if err != nil {
				return erg, err
			}
			if _, err := t.ExecContext(ctx,
				`INSERT INTO progress (account_id, item_id, position_ms, duration_ms, finished, reihenfolge)
				 VALUES (?,?,0,?,1,?)
				 ON CONFLICT (account_id, item_id) DO UPDATE SET finished = 1, reihenfolge = excluded.reihenfolge`,
				accountID, e.ItemID, dauer, reihe); err != nil {
				return erg, err
			}
			erg.Gesehen++
		case !e.Gesehen && e.PositionMS > 0 && arten.Angefangen:
			if vorhanden && (fertig == 1 || pos >= e.PositionMS) {
				erg.Unveraendert++
				break
			}
			reihe, err := naechsteReihe(ctx, t, accountID, e.ItemID)
			if err != nil {
				return erg, err
			}
			if _, err := t.ExecContext(ctx,
				`INSERT INTO progress (account_id, item_id, position_ms, duration_ms, finished, reihenfolge)
				 VALUES (?,?,?,?,0,?)
				 ON CONFLICT (account_id, item_id) DO UPDATE SET
				   position_ms = excluded.position_ms, duration_ms = excluded.duration_ms,
				   reihenfolge = excluded.reihenfolge`,
				accountID, e.ItemID, e.PositionMS, dauer, reihe); err != nil {
				return erg, err
			}
			erg.Angefangen++
		}

		if e.Favorit && arten.Favoriten {
			res, err := t.ExecContext(ctx,
				`INSERT OR IGNORE INTO favorit (account_id, item_id, added_at) VALUES (?,?,?)`,
				accountID, e.ItemID, heute())
			if err != nil {
				return erg, err
			}
			if n, _ := res.RowsAffected(); n > 0 {
				erg.Favoriten++
			}
		}
		if e.Sterne > 0 && e.Sterne <= 5 && arten.Bewertungen {
			// Nur wenn es noch keine gibt — und nicht im Haushalt geteilt:
			// Ob jemand Bewertungen zeigt, entscheidet die Person in Lumen.
			res, err := t.ExecContext(ctx,
				`INSERT OR IGNORE INTO rating (account_id, item_id, stars, shared, updated_at) VALUES (?,?,?,0,?)`,
				accountID, e.ItemID, e.Sterne, heute())
			if err != nil {
				return erg, err
			}
			if n, _ := res.RowsAffected(); n > 0 {
				erg.Bewertungen++
			} else {
				erg.Unveraendert++
			}
		}
	}
	if probe {
		return erg, nil // Rollback im defer
	}
	return erg, t.Commit()
}

// SichtbareIDs sind alle Filme, Serien und Folgen, die ein Profil mit
// dieser Abfrage sehen darf — über dieselbe Bedingung wie das Raster
// (bedingung): Freigaben, Altersgrenze samt Kette Folge–Staffel–Serie,
// „nur FSK 0" bei Kinderprofilen. Für die Übernahme aus Emby: Zugeordnet
// und geschrieben wird nur, was hier steht. Eine eigene, zweite Regel
// dafür wich früher ab (ein Kinderprofil mit Grenze 0 sah alles).
func (s *Store) SichtbareIDs(ctx context.Context, q Query) (map[string]bool, error) {
	q.Kinds = []string{"movie", "series", "episode"}
	q.IDs, q.NotIDs = nil, nil
	where, args, ok := bedingung(q)
	out := map[string]bool{}
	if !ok {
		return out, nil
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT i.id FROM item i WHERE `+strings.Join(where, " AND "), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}
