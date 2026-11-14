// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

// Der Aufbau von Serien: welche Folgen es gibt und in welcher Reihenfolge
// man sie schaut. Hier steht nichts Privates — nur die Mediathek. Wer was
// davon gesehen hat, kommt aus playback.go und wird erst in Go mit diesem
// Aufbau zusammengelegt (privacy_test.go verbietet das Zusammenfassen in SQL).

import (
	"context"
	"sort"
)

// FolgenStelle ist eine Folge mit ihrem Platz in der Serie.
type FolgenStelle struct {
	ID      string
	Staffel int
	Nr      int
}

// SerienFolgen liefert je Serie die Folgen in Sehreihenfolge: Staffel 1
// vor Staffel 2, darin nach Folgennummer. Specials (Staffel 0) fehlen
// bewusst — sie gehören nicht in „Als Nächstes", sonst schöbe sich ein
// Making-of zwischen zwei Folgen.
func (s *Store) SerienFolgen(ctx context.Context, seriesIDs []string) (map[string][]FolgenStelle, error) {
	out := map[string][]FolgenStelle{}
	for i := 0; i < len(seriesIDs); i += 400 {
		teil := seriesIDs[i:min(i+400, len(seriesIDs))]
		rows, err := s.db.QueryContext(ctx,
			`SELECT st.parent_id, e.id, COALESCE(st.season_no,0), COALESCE(e.episode_no,0)
			   FROM item e JOIN item st ON st.id = e.parent_id
			  WHERE e.kind = 'episode' AND COALESCE(st.season_no,0) > 0
			    AND st.parent_id IN (`+placeholders(len(teil))+`)`, toAny(teil)...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var serie string
			var f FolgenStelle
			if err := rows.Scan(&serie, &f.ID, &f.Staffel, &f.Nr); err != nil {
				rows.Close()
				return nil, err
			}
			out[serie] = append(out[serie], f)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	for _, folgen := range out {
		sort.SliceStable(folgen, func(i, j int) bool {
			if folgen[i].Staffel != folgen[j].Staffel {
				return folgen[i].Staffel < folgen[j].Staffel
			}
			// Folgen ohne Nummer zuletzt, wie überall (folgenOrdnung).
			if (folgen[i].Nr == 0) != (folgen[j].Nr == 0) {
				return folgen[j].Nr == 0
			}
			if folgen[i].Nr != folgen[j].Nr {
				return folgen[i].Nr < folgen[j].Nr
			}
			return folgen[i].ID < folgen[j].ID
		})
	}
	return out, nil
}

// FolgenVon liefert die Folgen unter einer Serie oder einer Staffel, in
// Sehreihenfolge. staffel >= 0 beschränkt eine Serie auf diese eine
// Staffel; -1 heisst alle, die Specials eingeschlossen — wer eine ganze
// Serie als gesehen markiert, meint auch die Extras.
func (s *Store) FolgenVon(ctx context.Context, id string, staffel int) ([]Item, error) {
	q := `SELECT ` + itemCols + ` FROM item i WHERE i.kind = 'episode' AND (i.parent_id = ? OR i.parent_id IN
	        (SELECT st.id FROM item st WHERE st.kind = 'season' AND st.parent_id = ?`
	args := []any{id, id}
	if staffel >= 0 {
		q += ` AND COALESCE(st.season_no,0) = ?`
		args = append(args, staffel)
	}
	q += `)) ORDER BY ` + folgenOrdnung
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Item{}
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *it)
	}
	return out, rows.Err()
}

// SerieZurFolge: zu Folgen die Serie mit Namen — damit „Weiterschauen"
// nicht bloss „Pilot" sagt, sondern „Tiefsee · S1 F1 · Pilot".
type SerieZurFolge struct {
	ID    string
	Titel string
}

func (s *Store) SerienZuFolgen(ctx context.Context, episodeIDs []string) (map[string]SerieZurFolge, error) {
	out := map[string]SerieZurFolge{}
	for i := 0; i < len(episodeIDs); i += 400 {
		teil := episodeIDs[i:min(i+400, len(episodeIDs))]
		rows, err := s.db.QueryContext(ctx,
			`SELECT e.id, se.id, se.title FROM item e
			   JOIN item st ON st.id = e.parent_id
			   JOIN item se ON se.id = st.parent_id
			  WHERE e.kind = 'episode' AND e.id IN (`+placeholders(len(teil))+`)`, toAny(teil)...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var folge string
			var z SerieZurFolge
			if err := rows.Scan(&folge, &z.ID, &z.Titel); err != nil {
				rows.Close()
				return nil, err
			}
			out[folge] = z
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// SpecialOhneNummer ist der Anfang des Bereichs, in dem Specials ohne
// eigene Nummer gezählt werden („Game of Thrones - S08E00 - The Last
// Watch", „Specials/Pilot.mkv"). Vorher bekamen sie die nächste freie
// Nummer der Staffel 0 — kam später die echte Datei S00E02 dazu, wurde
// sie eine zweite Fassung des falschen Specials. Ab 1000 kommt keine
// echte Nummer in die Quere; die Oberfläche zeigt dort „Special" statt
// einer Nummer.
const SpecialOhneNummer = 1000

// FolgeUmnummern gibt einer Folge eine andere Nummer, ohne sie zu
// sperren — gebraucht, wenn der Durchlauf ein Special aus dem alten
// Zählbereich in SpecialOhneNummer verschiebt. Die Folge behält ihre
// Kennung und damit alles Persönliche daran.
func (s *Store) FolgeUmnummern(ctx context.Context, id string, nr int) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE item SET episode_no = ? WHERE id = ? AND kind = 'episode'`, nullInt(nr), id)
	return err
}
