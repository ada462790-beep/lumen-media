// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

// Leere Doppel von Serien zusammenführen (Issue #7).
//
// Bis zur Fassung 0.1.12 legte jeder Durchlauf eine Serie neu an, sobald
// TMDb ihren Titel geändert hatte. Die Dateien wanderten dabei jedes Mal
// zur neuesten Serie; zurück blieben Serien ohne eine einzige Datei — aber
// mit dem Stand der Leute („bis Folge 5 gesehen"), mit Merkliste und
// Bewertungen.
//
// Hier wird aufgeräumt: Eine Serie ohne Dateien, zu der es in derselben
// Bibliothek eine Serie mit Dateien gibt (gleiche TMDb-Kennung oder
// gleicher Titel), geht in dieser auf. Fortschritt, Merkliste, Bewertung
// und Listen-Einträge wandern zur passenden Folge (gleiche Staffel, gleiche
// Nummer) bzw. zur Serie — für jedes Konto einzeln, ohne über Konten
// hinweg zu lesen. Dann verschwindet das leere Doppel.
//
// Eine Serie, deren Platte gerade nicht da ist, ist nicht betroffen: Ihre
// Dateien stehen weiter in der Datenbank (nur als fehlend markiert).

import (
	"context"
	"database/sql"
)

// dateienDerSerie zählt die Dateien einer Serie sr — an der Serie selbst,
// an ihren Staffeln und an deren Folgen.
//
// Drei getrennte Zählungen statt einer mit „e.id = sr.id OR e.parent_id =
// sr.id OR st.parent_id = sr.id": Mit dem OR fand SQLite keinen Index und
// lief für jede Serie einmal über alle Dateien der Datenbank. Bei 500
// Serien und 120 000 Dateien hielt das nach jedem Durchlauf drei Minuten
// lang die Arbeitssperre. So geht jede Zählung den Index entlang
// (Serie → Staffeln → Folgen → Dateien) und rührt nur die eigenen Zeilen an.
const dateienDerSerie = `(
	  (SELECT count(*) FROM media_file f WHERE f.item_id = sr.id)
	+ (SELECT count(*) FROM item e JOIN media_file f ON f.item_id = e.id
	    WHERE e.parent_id = sr.id)
	+ (SELECT count(*) FROM item st JOIN item e ON e.parent_id = st.id
	    JOIN media_file f ON f.item_id = e.id
	    WHERE st.parent_id = sr.id))`

// serieHatDateien ist 1, wenn an der Serie sr irgendwo eine Datei hängt.
// Für „leer oder nicht" muss nichts gezählt werden: EXISTS hört bei der
// ersten Datei auf.
const serieHatDateien = `(
	   EXISTS (SELECT 1 FROM media_file f WHERE f.item_id = sr.id)
	OR EXISTS (SELECT 1 FROM item e JOIN media_file f ON f.item_id = e.id
	            WHERE e.parent_id = sr.id)
	OR EXISTS (SELECT 1 FROM item st JOIN item e ON e.parent_id = st.id
	            JOIN media_file f ON f.item_id = e.id
	            WHERE st.parent_id = sr.id))`

// LeereSerienZusammenfuehren gibt zurück, wie viele Doppel verschwunden sind.
func (s *Store) LeereSerienZusammenfuehren(ctx context.Context) (int, error) {
	type serie struct {
		id, lib, titel, original, tmdb string
		dateien                        int
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT sr.id, sr.library_id, lower(sr.title), lower(COALESCE(sr.original_title,'')),
		        COALESCE(sr.tmdb_id,''),
		        `+serieHatDateien+`
		   FROM item sr WHERE sr.kind = 'series'`)
	if err != nil {
		return 0, err
	}
	var alle []serie
	for rows.Next() {
		var x serie
		if err := rows.Scan(&x.id, &x.lib, &x.titel, &x.original, &x.tmdb, &x.dateien); err != nil {
			rows.Close()
			return 0, err
		}
		alle = append(alle, x)
	}
	rows.Close()

	passt := func(a, b serie) bool {
		if a.lib != b.lib {
			return false
		}
		if a.tmdb != "" && a.tmdb == b.tmdb {
			return true
		}
		for _, x := range []string{a.titel, a.original} {
			if x != "" && (x == b.titel || x == b.original) {
				return true
			}
		}
		return false
	}
	konten, err := s.kontenIDs(ctx)
	if err != nil {
		return 0, err
	}

	n := 0
	for _, leer := range alle {
		if leer.dateien > 0 {
			continue
		}
		var ziel *serie
		for i := range alle {
			if alle[i].dateien > 0 && passt(leer, alle[i]) {
				ziel = &alle[i]
				break
			}
		}
		if ziel == nil {
			continue
		}
		if err := s.serieUeberfuehren(ctx, leer.id, ziel.id, konten); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// SerienZusammenfuehren räumt Doppel von Serien auf und gibt zurück, wie
// viele verschwunden sind.
//
// Erst die leeren Doppel (siehe oben). Dann Serien, die beide Dateien
// haben, aber laut TMDb dieselbe sind — etwa „Grey's Anatomy" Staffel 1–6
// und „Grey's Anatomy" Staffel 8–9, weil die Dateien früher verschieden
// hiessen. Sie gehen in der mit den meisten Dateien auf: Staffeln und
// Folgen ziehen um, liegt eine Folge doppelt vor, wird die zweite Datei
// eine weitere Fassung derselben Folge.
//
// Zuletzt verschwinden Serien, an denen keine einzige Datei mehr hängt und
// zu denen es kein Gegenstück gibt: Überbleibsel einer falschen Zuordnung
// („The" statt „The German", „Star Trek: Raumschiff Voyager: Staffel 01"),
// deren Dateien inzwischen bei der richtigen Serie liegen. Eine Datei, die
// gerade nur fehlt, steht weiter in der Datenbank — solche Serien bleiben.
func (s *Store) SerienZusammenfuehren(ctx context.Context) (int, error) {
	n, err := s.LeereSerienZusammenfuehren(ctx)
	if err != nil {
		return n, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT sr.id, sr.library_id, sr.tmdb_id,
		        `+dateienDerSerie+` AS n
		   FROM item sr
		  WHERE sr.kind = 'series' AND COALESCE(sr.tmdb_id,'') <> ''
		    -- Gezählt wird nur, wo es überhaupt ein Doppel gibt.
		    AND EXISTS (SELECT 1 FROM item d WHERE d.kind = 'series' AND d.tmdb_id = sr.tmdb_id
		                   AND d.library_id = sr.library_id AND d.id <> sr.id)
		  ORDER BY sr.library_id, sr.tmdb_id, n DESC, sr.id`)
	if err != nil {
		return n, err
	}
	type serie struct{ id, lib, tmdb string }
	var paare [][2]string // alt → neu
	var vorige serie
	for rows.Next() {
		var x serie
		var dateien int
		if err := rows.Scan(&x.id, &x.lib, &x.tmdb, &dateien); err != nil {
			rows.Close()
			return n, err
		}
		if x.lib == vorige.lib && x.tmdb == vorige.tmdb {
			paare = append(paare, [2]string{x.id, vorige.id})
			continue
		}
		vorige = x
	}
	rows.Close()
	if len(paare) > 0 {
		konten, err := s.kontenIDs(ctx)
		if err != nil {
			return n, err
		}
		for _, p := range paare {
			if err := s.serieUeberfuehren(ctx, p[0], p[1], konten); err != nil {
				return n, err
			}
			n++
		}
	}
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM item WHERE kind = 'series'
		    AND NOT EXISTS (SELECT 1 FROM media_file f WHERE f.item_id = item.id)
		    AND NOT EXISTS (SELECT 1 FROM item e JOIN media_file f ON f.item_id = e.id
		                     WHERE e.parent_id = item.id)
		    AND NOT EXISTS (SELECT 1 FROM item st JOIN item e ON e.parent_id = st.id
		                      JOIN media_file f ON f.item_id = e.id
		                     WHERE st.parent_id = item.id)`)
	if err != nil {
		return n, err
	}
	weg, _ := res.RowsAffected()
	return n + int(weg), nil
}

func (s *Store) kontenIDs(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM account`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectStrings(rows)
}

// serieUeberfuehren hängt alles Persönliche von alt an neu und löscht alt.
func (s *Store) serieUeberfuehren(ctx context.Context, alt, neu string, konten []string) error {
	return s.tx(ctx, func(t *sql.Tx) error {
		// Was nur alt hat, zieht um: ganze Staffeln, die neu fehlen, und
		// Folgen, die in der Staffel von neu fehlen. Liegt eine Folge in
		// beiden vor, wandern die Dateien von alt zur Folge von neu — als
		// weitere Fassung. Bei leeren Doppeln tut das alles nichts.
		for _, q := range []string{
			`UPDATE item SET parent_id = ?2
			  WHERE kind = 'season' AND parent_id = ?1
			    AND season_no NOT IN (SELECT season_no FROM item WHERE kind = 'season' AND parent_id = ?2)`,
			`UPDATE item SET parent_id = (SELECT nst.id FROM item nst, item ast
			                               WHERE ast.id = item.parent_id AND nst.parent_id = ?2
			                                 AND nst.kind = 'season' AND nst.season_no = ast.season_no)
			  WHERE kind = 'episode'
			    AND parent_id IN (SELECT id FROM item WHERE kind = 'season' AND parent_id = ?1)
			    AND NOT EXISTS (SELECT 1 FROM item ne, item nst, item ast
			                     WHERE ast.id = item.parent_id AND nst.parent_id = ?2
			                       AND nst.kind = 'season' AND nst.season_no = ast.season_no
			                       AND ne.parent_id = nst.id AND ne.episode_no = item.episode_no)`,
			`UPDATE media_file SET item_id = COALESCE((
			     SELECT ne.id FROM item ae, item ast, item nst, item ne
			      WHERE ae.id = media_file.item_id AND ast.id = ae.parent_id
			        AND nst.parent_id = ?2 AND nst.kind = 'season' AND nst.season_no = ast.season_no
			        AND ne.parent_id = nst.id AND ne.episode_no = ae.episode_no LIMIT 1), item_id)
			  WHERE item_id IN (SELECT ae.id FROM item ae JOIN item ast ON ast.id = ae.parent_id
			                     WHERE ast.parent_id = ?1 AND ae.kind = 'episode')`,
		} {
			if _, err := t.ExecContext(ctx, q, alt, neu); err != nil {
				return err
			}
		}

		// Paare (alte Kennung → neue): die Serie selbst und jede Folge mit
		// derselben Staffel- und Folgennummer.
		paare := map[string]string{alt: neu}
		rows, err := t.QueryContext(ctx,
			`SELECT ae.id, ne.id FROM item ae
			   JOIN item ast ON ast.id = ae.parent_id AND ast.parent_id = ?
			   JOIN item nst ON nst.parent_id = ? AND nst.season_no = ast.season_no
			   JOIN item ne ON ne.parent_id = nst.id AND ne.episode_no = ae.episode_no
			  WHERE ae.kind = 'episode' AND ne.kind = 'episode'`, alt, neu)
		if err != nil {
			return err
		}
		for rows.Next() {
			var a, b string
			if err := rows.Scan(&a, &b); err != nil {
				rows.Close()
				return err
			}
			paare[a] = b
		}
		rows.Close()

		for von, nach := range paare {
			for _, acc := range konten {
				for _, q := range []string{
					`UPDATE OR IGNORE progress SET item_id = ? WHERE account_id = ? AND item_id = ?`,
					`UPDATE OR IGNORE watchlist SET item_id = ? WHERE account_id = ? AND item_id = ?`,
					`UPDATE OR IGNORE rating SET item_id = ? WHERE account_id = ? AND item_id = ?`,
				} {
					if _, err := t.ExecContext(ctx, q, nach, acc, von); err != nil {
						return err
					}
				}
			}
			if _, err := t.ExecContext(ctx,
				`UPDATE OR IGNORE playlist_item SET item_id = ? WHERE item_id = ?`, nach, von); err != nil {
				return err
			}
		}
		// Was umgezogen ist, kann neuer sein als alles, was neu schon hatte
		// („Zuletzt hinzugefügt", siehe UpsertItem).
		if _, err := t.ExecContext(ctx,
			`UPDATE item SET zuletzt_neu = max(COALESCE(zuletzt_neu, 0), COALESCE((
			     SELECT max(e.added_at) FROM item st JOIN item e ON e.parent_id = st.id
			      WHERE st.parent_id = ?1), 0))
			  WHERE id = ?1`, neu); err != nil {
			return err
		}
		// Staffeln und Folgen hängen per ON DELETE CASCADE an der Serie.
		_, err = t.ExecContext(ctx, `DELETE FROM item WHERE id = ?`, alt)
		return err
	})
}

// FilmeOhneDatei entfernt Filme, an denen keine einzige Datei mehr hängt
// — etwa „1", „2", „3" aus älteren Fassungen, deren Dateien inzwischen
// richtig als Folgen einer Miniserie erkannt sind (Issue #12). Eine
// Datei, die nur gerade fehlt, steht weiter in der Datenbank; solche
// Filme bleiben.
func (s *Store) FilmeOhneDatei(ctx context.Context) (int, error) {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM item WHERE kind = 'movie'
		    AND NOT EXISTS (SELECT 1 FROM media_file f WHERE f.item_id = item.id)`)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}
