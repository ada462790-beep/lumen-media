// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

// Teile eines Films von Hand: „Als Teile zusammenfügen" und „Sind
// verschiedene Filme" unter Aufräumen. Beides merkt sich eine Sperre ohne
// eigenes Feld (wie FeldZuordnung), damit der nächste Durchlauf es nicht
// wieder umwirft.

import (
	"context"
	"database/sql"
	"errors"
	"sort"
)

const (
	// FeldTeile: Die Teilnummern der Dateien dieses Films hat jemand von
	// Hand festgelegt. Der Scanner überschreibt sie nicht.
	FeldTeile = "teile"
	// FeldEinzeln: Dieser Eintrag ist ein eigener Film, auch wenn sein
	// Name nach „Teil 2" eines anderen aussieht. Der Scanner legt ihn
	// nicht mit den anderen Teilen zusammen.
	FeldEinzeln = "einzeln"
)

// SperreGesetzt sagt, ob am Titel die Sperre feld steht — auch für die
// Sperren ohne eigenes Feld (FeldZuordnung, FeldTeile, FeldEinzeln).
func (s *Store) SperreGesetzt(ctx context.Context, itemID, feld string) bool {
	return gesperrt(ctx, s.db, itemID, feld)
}

// TeileFestlegen macht aus den Dateien dateiIDs (in dieser Reihenfolge
// Teil 1, 2, …) einen Film in Teilen am Titel ziel. Dateien anderer Titel
// wandern mit; ein Titel, dessen Dateien alle dabei sind, geht samt Stand,
// Merkliste und Bewertung in ziel auf. ziel bekommt die Sperren
// FeldZuordnung und FeldTeile.
func (s *Store) TeileFestlegen(ctx context.Context, ziel string, dateiIDs []string) error {
	if len(dateiIDs) < 2 {
		return errors.New("es braucht mindestens zwei dateien")
	}
	it, err := s.Item(ctx, ziel)
	if err != nil {
		return err
	}
	if it.Kind != "movie" {
		return errors.New("teile gibt es nur bei filmen")
	}
	konten, err := s.kontenIDs(ctx)
	if err != nil {
		return err
	}
	return s.tx(ctx, func(t *sql.Tx) error {
		dabei := map[string]bool{}
		quellen := map[string]bool{}
		for _, id := range dateiIDs {
			if dabei[id] {
				return errors.New("eine datei steht zweimal in der liste")
			}
			dabei[id] = true
			var item, art, lib string
			if err := t.QueryRowContext(ctx,
				`SELECT f.item_id, i.kind, i.library_id FROM media_file f JOIN item i ON i.id = f.item_id WHERE f.id = ?`,
				id).Scan(&item, &art, &lib); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return ErrNotFound
				}
				return err
			}
			if art != "movie" || lib != it.LibraryID {
				return errors.New("die dateien gehören nicht zu filmen derselben bibliothek")
			}
			if item != ziel {
				quellen[item] = true
			}
		}
		for i, id := range dateiIDs {
			if _, err := t.ExecContext(ctx, `UPDATE media_file SET item_id = ?, part_no = ? WHERE id = ?`,
				ziel, i+1, id); err != nil {
				return err
			}
		}
		// Ein Titel ohne Datei geht ganz in ziel auf (mit dem Persönlichen).
		for q := range quellen {
			var rest int
			if err := t.QueryRowContext(ctx, `SELECT count(*) FROM media_file WHERE item_id = ?`, q).Scan(&rest); err != nil {
				return err
			}
			if rest > 0 {
				continue
			}
			if _, err := t.ExecContext(ctx,
				`UPDATE OR IGNORE collection_item SET item_id = ? WHERE item_id = ?`, ziel, q); err != nil {
				return err
			}
			if err := persoenlichesUmhaengen(ctx, t, q, ziel, konten); err != nil {
				return err
			}
			if _, err := t.ExecContext(ctx, `DELETE FROM item WHERE id = ?`, q); err != nil {
				return err
			}
		}
		if _, err := t.ExecContext(ctx, `UPDATE item SET bevorzugte_datei = NULL WHERE id = ?`, ziel); err != nil {
			return err
		}
		if _, err := t.ExecContext(ctx, `DELETE FROM feld_sperre WHERE item_id = ? AND feld = ?`, ziel, FeldEinzeln); err != nil {
			return err
		}
		for _, f := range []string{FeldZuordnung, FeldTeile} {
			if _, err := t.ExecContext(ctx,
				`INSERT OR IGNORE INTO feld_sperre (item_id, feld) VALUES (?, ?)`, ziel, f); err != nil {
				return err
			}
		}
		return nil
	})
}

// TeileTrennen macht aus einem Film in Teilen wieder einzelne Filme: Teil 1
// bleibt am Titel, jeder weitere wird ein eigener Titel (Name und Jahr
// liefert titelFuer, aus dem Dateinamen). Alle bekommen die Sperre
// FeldEinzeln, damit der Scanner sie nicht wieder zusammenlegt.
func (s *Store) TeileTrennen(ctx context.Context, itemID string, titelFuer func(MediaFile) (string, int)) ([]*Item, error) {
	it, err := s.Item(ctx, itemID)
	if err != nil {
		return nil, err
	}
	if it.Kind != "movie" {
		return nil, errors.New("teile gibt es nur bei filmen")
	}
	dateien, err := s.FilesFor(ctx, itemID)
	if err != nil {
		return nil, err
	}
	if len(dateien) < 2 {
		return nil, errors.New("der titel hat nur diese eine datei")
	}
	sort.SliceStable(dateien, func(i, j int) bool {
		if dateien[i].PartNo != dateien[j].PartNo {
			return dateien[i].PartNo < dateien[j].PartNo
		}
		return dateien[i].Path < dateien[j].Path
	})
	var neu []*Item
	for _, f := range dateien[1:] {
		titel, jahr := titelFuer(f)
		n, err := s.DateiAbtrennen(ctx, f.ID, titel, jahr)
		if err != nil {
			return neu, err
		}
		neu = append(neu, n)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE media_file SET part_no = 1 WHERE item_id = ?`, itemID); err != nil {
		return neu, err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM feld_sperre WHERE item_id = ? AND feld = ?`, itemID, FeldTeile); err != nil {
		return neu, err
	}
	for _, x := range append([]*Item{it}, neu...) {
		if _, err := s.db.ExecContext(ctx,
			`INSERT OR IGNORE INTO feld_sperre (item_id, feld) VALUES (?, ?)`, x.ID, FeldEinzeln); err != nil {
			return neu, err
		}
	}
	return neu, nil
}

// FilmDatei: eine vorhandene Datei eines Films, mit Titel — für die Suche
// nach Teilen, die als getrennte Filme dastehen (Aufräumen).
type FilmDatei struct {
	ID        string `json:"id"`
	ItemID    string `json:"itemId"`
	LibraryID string `json:"libraryId"`
	Path      string `json:"path"`
	SizeBytes int64  `json:"sizeBytes"`
	Height    int    `json:"height,omitempty"`
	Container string `json:"container,omitempty"`
	PartNo    int    `json:"partNo"`
	Titel     string `json:"titel"`
	Jahr      int    `json:"jahr,omitempty"`
}

// FilmDateienVorhanden: alle vorhandenen Dateien aller Filme.
func (s *Store) FilmDateienVorhanden(ctx context.Context) ([]FilmDatei, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT f.id, f.item_id, i.library_id, f.path, f.size_bytes, COALESCE(f.height,0),
		        COALESCE(f.container,''), f.part_no, i.title, COALESCE(i.year,0)
		   FROM media_file f JOIN item i ON i.id = f.item_id
		  WHERE i.kind = 'movie' AND f.missing_since IS NULL
		  ORDER BY f.path`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FilmDatei{}
	for rows.Next() {
		var d FilmDatei
		if err := rows.Scan(&d.ID, &d.ItemID, &d.LibraryID, &d.Path, &d.SizeBytes, &d.Height,
			&d.Container, &d.PartNo, &d.Titel, &d.Jahr); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
