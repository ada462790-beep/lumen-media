// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

// Fehlende Dateien, deren Titel noch eine andere Datei hat.
//
// Auf dem NAS eines Besitzers stand unter „Fehlende Dateien" eine Folge
// von „The Big Bang Theory", die es weiterhin gab — nur unter neuem Namen.
// Die Umbenennung erkennt der Durchlauf nur, wenn die alte Datei im selben
// Durchlauf verschwindet, in dem die neue auftaucht, und beide gleich gross
// sind (scan/umbenannt.go). Brach ein Durchlauf ab, nachdem er die neue
// schon eingetragen hatte, oder wurde die Folge durch eine andere Fassung
// ersetzt, blieb die alte Zeile als „fehlend" stehen — für immer.

import (
	"context"
	"database/sql"
	"fmt"
)

// FehlendAngabe: wozu eine fehlende Datei gehört — für die Liste unter
// Aufräumen.
type FehlendAngabe struct {
	Kind    string `json:"kind"`
	Titel   string `json:"titel"`
	Jahr    int    `json:"jahr,omitempty"`
	Serie   string `json:"serie,omitempty"`
	Staffel int    `json:"staffel,omitempty"`
	Folge   int    `json:"folge,omitempty"`
	// Ersatz: Der Titel (Film oder Folge) hat noch eine vorhandene Datei
	// mit derselben Teilnummer — die fehlende wurde vermutlich umbenannt
	// oder ersetzt.
	Ersatz     bool   `json:"ersatz,omitempty"`
	ErsatzPfad string `json:"ersatzPfad,omitempty"`
}

// FehlendeAngaben nennt zu jeder fehlenden Datei (fileIDs) ihren Titel.
func (s *Store) FehlendeAngaben(ctx context.Context, fileIDs []string) (map[string]FehlendAngabe, error) {
	out := map[string]FehlendAngabe{}
	for _, id := range fileIDs {
		var a FehlendAngabe
		var jahr, staffel, folge sql.NullInt64
		var serie, ersatz sql.NullString
		err := s.db.QueryRowContext(ctx,
			`SELECT i.kind, i.title, i.year, i.season_no, i.episode_no,
			        (SELECT sr.title FROM item st JOIN item sr ON sr.id = st.parent_id
			          WHERE i.kind = 'episode' AND st.id = i.parent_id),
			        (SELECT g.path FROM media_file g
			          WHERE g.item_id = f.item_id AND g.id <> f.id AND g.missing_since IS NULL
			            AND g.part_no = f.part_no ORDER BY g.path LIMIT 1)
			   FROM media_file f JOIN item i ON i.id = f.item_id WHERE f.id = ?`, id).
			Scan(&a.Kind, &a.Titel, &jahr, &staffel, &folge, &serie, &ersatz)
		if err != nil {
			if err == sql.ErrNoRows {
				continue
			}
			return out, err
		}
		a.Jahr, a.Staffel, a.Folge = int(jahr.Int64), int(staffel.Int64), int(folge.Int64)
		a.Serie, a.ErsatzPfad, a.Ersatz = serie.String, ersatz.String, ersatz.Valid
		out[id] = a
	}
	return out, nil
}

// FehlendeMitErsatzAustragen trägt fehlende Dateien der Bibliothek aus,
// deren Titel noch eine vorhandene Datei mit derselben Teilnummer hat —
// wenn darf(fehlender Pfad, vorhandener Pfad) zustimmt (der Scanner prüft
// dort, dass es keine abgehängte Platte sein kann). Stand, Haken und
// Merkliste hängen am Titel und bleiben. Zurück: wie viele.
func (s *Store) FehlendeMitErsatzAustragen(ctx context.Context, libraryID string, darf func(fehlt, da string) bool) (int, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT f.id, f.path,
		        (SELECT g.path FROM media_file g
		          WHERE g.item_id = f.item_id AND g.id <> f.id AND g.missing_since IS NULL
		            AND g.part_no = f.part_no AND COALESCE(g.defekt,'') = '' ORDER BY g.path LIMIT 1)
		   FROM media_file f JOIN item i ON i.id = f.item_id
		  WHERE i.library_id = ? AND f.missing_since IS NOT NULL`, libraryID)
	if err != nil {
		return 0, err
	}
	var weg []string
	for rows.Next() {
		var id, pfad string
		var da sql.NullString
		if err := rows.Scan(&id, &pfad, &da); err != nil {
			rows.Close()
			return 0, err
		}
		if da.Valid && (darf == nil || darf(pfad, da.String)) {
			weg = append(weg, id)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(weg) == 0 {
		return 0, nil
	}
	err = s.tx(ctx, func(t *sql.Tx) error {
		for _, id := range weg {
			// Eine bevorzugte Fassung, die nicht mehr da ist, gilt nicht mehr.
			if _, err := t.ExecContext(ctx,
				`UPDATE item SET bevorzugte_datei = NULL WHERE bevorzugte_datei = ?`, id); err != nil {
				return err
			}
			if _, err := t.ExecContext(ctx, `DELETE FROM media_file WHERE id = ?`, id); err != nil {
				return fmt.Errorf("fehlende datei austragen: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return len(weg), nil
}
