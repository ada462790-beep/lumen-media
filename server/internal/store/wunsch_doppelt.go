// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

// Doppelte Wünsche, Löschen und das Nachtragen von Kennungen.
//
//   - Wünscht jemand, was schon offen gewünscht ist — gleiche TMDb- oder
//     IMDb-Kennung, sonst gleicher Titel (ohne Satzzeichen, Gross- und
//     Kleinschreibung) und gleiches Jahr —, entsteht kein zweiter
//     Eintrag. Die Person schliesst sich dem bestehenden an (wish_dabei).
//   - Was schon doppelt da ist (von früher), zeigt die Verwaltung als eine
//     Gruppe (WunschGruppen) und kann es zusammenführen.
//   - Löschen: die Verwaltung jeden Wunsch, wer ihn hat, den eigenen
//     offenen. Wer sich nur angeschlossen hat, tritt aus.
//   - Alte Wünsche ohne Kennung (vor den Vorschlägen von TMDb angelegt)
//     bekommen sie nachträglich, wenn TMDb genau einen Treffer kennt
//     (die Suche macht die API).

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"unicode"
)

// WunschNorm macht Titel vergleichbar: klein, ohne Satzzeichen, Wörter
// durch ein Leerzeichen getrennt. „By Any Means" = „by any means!".
func WunschNorm(t string) string {
	return strings.Join(strings.FieldsFunc(strings.ToLower(NFC(t)), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}), " ")
}

// offenerWunschZeile: was zum Vergleichen gebraucht wird.
type offenerWunschZeile struct {
	id, konto, titel, art, tmdb, imdb string
	jahr                              int
}

func (s *Store) offeneWuensche(ctx context.Context) ([]offenerWunschZeile, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, account_id, title, COALESCE(year,0), COALESCE(art,''), COALESCE(tmdb_id,''), COALESCE(imdb_id,'')
		   FROM wish WHERE state IN ('open','working') ORDER BY created_at, rowid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []offenerWunschZeile
	for rows.Next() {
		var z offenerWunschZeile
		if err := rows.Scan(&z.id, &z.konto, &z.titel, &z.jahr, &z.art, &z.tmdb, &z.imdb); err != nil {
			return nil, err
		}
		out = append(out, z)
	}
	return out, rows.Err()
}

// gleicherWunsch: Ist z derselbe Wunsch wie d? Kennungen gehen vor; ohne
// gemeinsame Kennung zählen Titel (oder Originaltitel) und Jahr — aber
// nie gegen eine andere TMDb- oder IMDb-Kennung oder eine andere Art.
func gleicherWunsch(z offenerWunschZeile, d WunschDaten) bool {
	if d.TMDbID != "" && z.tmdb == d.TMDbID && (z.art == "" || d.Art == "" || z.art == d.Art) {
		return true
	}
	if d.IMDbID != "" && z.imdb == d.IMDbID {
		return true
	}
	if (d.TMDbID != "" && z.tmdb != "" && z.tmdb != d.TMDbID) ||
		(d.IMDbID != "" && z.imdb != "" && z.imdb != d.IMDbID) ||
		(d.Art != "" && z.art != "" && d.Art != z.art) {
		return false
	}
	if z.jahr != d.Jahr {
		return false
	}
	n := WunschNorm(z.titel)
	return n != "" && (n == WunschNorm(d.Titel) || n == WunschNorm(d.Originaltitel))
}

// gleicherOffenerWunsch sucht einen offenen Wunsch, der derselbe ist —
// von wem auch immer, eigene zuerst.
func (s *Store) gleicherOffenerWunsch(ctx context.Context, accountID string, d WunschDaten) (id, owner string, err error) {
	alle, err := s.offeneWuensche(ctx)
	if err != nil {
		return "", "", err
	}
	for _, eigen := range []bool{true, false} {
		for _, z := range alle {
			if (z.konto == accountID) == eigen && gleicherWunsch(z, d) {
				return z.id, z.konto, nil
			}
		}
	}
	return "", "", nil
}

// wunschKennungErgaenzen trägt fehlende Kennungen und das Poster nach;
// was der Wunsch schon hat, bleibt.
func (s *Store) wunschKennungErgaenzen(ctx context.Context, id string, d WunschDaten) error {
	if !d.mitKennung() && d.PosterPfad == "" {
		return nil
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE wish SET art = COALESCE(art, ?), tmdb_id = COALESCE(tmdb_id, ?), imdb_id = COALESCE(imdb_id, ?),
		                 originaltitel = COALESCE(originaltitel, ?), poster = COALESCE(poster, ?)
		  WHERE id = ?`,
		nullStr(d.Art), nullStr(d.TMDbID), nullStr(d.IMDbID), nullStr(d.Originaltitel), nullStr(d.PosterPfad), id)
	return err
}

// WunschLoeschen löscht einen Wunsch samt allen, die sich angeschlossen
// haben.
func (s *Store) WunschLoeschen(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM wish WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// WunschZustand: wem der Wunsch gehört, wie er steht und ob sich das
// Konto angeschlossen hat (für die Rechte beim Löschen).
func (s *Store) WunschZustand(ctx context.Context, id, accountID string) (owner, state string, dabei bool, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT account_id, state FROM wish WHERE id = ?`, id).Scan(&owner, &state)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", false, ErrNotFound
	}
	if err != nil {
		return "", "", false, err
	}
	var n int
	_ = s.db.QueryRowContext(ctx, `SELECT count(*) FROM wish_dabei WHERE wish_id = ? AND account_id = ?`,
		id, accountID).Scan(&n)
	return owner, state, n > 0, nil
}

// WunschVerlassen: Wer sich angeschlossen hat, tritt aus.
func (s *Store) WunschVerlassen(ctx context.Context, id, accountID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM wish_dabei WHERE wish_id = ? AND account_id = ?`, id, accountID)
	return err
}

// WunschGruppen ordnet offene Wünsche, die dasselbe meinen, einer Gruppe
// zu: Wunsch → Kennung des ältesten Wunschs der Gruppe. Wünsche ohne
// Doppel fehlen.
func (s *Store) WunschGruppen(ctx context.Context) (map[string]string, error) {
	alle, err := s.offeneWuensche(ctx)
	if err != nil {
		return nil, err
	}
	// alle ist nach Alter sortiert: Der erste passende ist der älteste.
	haupt := map[string]string{}
	var haeupter []offenerWunschZeile
	for _, z := range alle {
		d := WunschDaten{Titel: z.titel, Jahr: z.jahr, Art: z.art, TMDbID: z.tmdb, IMDbID: z.imdb}
		gefunden := false
		for i := range haeupter {
			if gleicherWunsch(haeupter[i], d) {
				haupt[z.id] = haeupter[i].id
				haupt[haeupter[i].id] = haeupter[i].id
				// Die Gruppe kennt danach alle Kennungen.
				if haeupter[i].tmdb == "" {
					haeupter[i].tmdb, haeupter[i].art = z.tmdb, orStr(haeupter[i].art, z.art)
				}
				if haeupter[i].imdb == "" {
					haeupter[i].imdb = z.imdb
				}
				gefunden = true
				break
			}
		}
		if !gefunden {
			haeupter = append(haeupter, z)
		}
	}
	return haupt, nil
}

func orStr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// WuenscheZusammenfuehren legt doppelte Wünsche in den ältesten (haupt):
// Wer sie hatte oder sich angeschlossen hatte, ist danach bei haupt
// dabei; fehlende Kennungen und Poster wandern mit. Die anderen
// verschwinden. Zurück kommt, wie viele es waren.
func (s *Store) WuenscheZusammenfuehren(ctx context.Context, haupt string, andere []string) (int, error) {
	var hauptKonto string
	if err := s.db.QueryRowContext(ctx, `SELECT account_id FROM wish WHERE id = ?`, haupt).Scan(&hauptKonto); err != nil {
		return 0, ErrNotFound
	}
	n := 0
	err := s.tx(ctx, func(t *sql.Tx) error {
		for _, id := range andere {
			if id == haupt {
				continue
			}
			var konto, art, tmdb, imdb, orig, poster string
			err := t.QueryRowContext(ctx,
				`SELECT account_id, COALESCE(art,''), COALESCE(tmdb_id,''), COALESCE(imdb_id,''),
				        COALESCE(originaltitel,''), COALESCE(poster,'') FROM wish WHERE id = ?`, id).
				Scan(&konto, &art, &tmdb, &imdb, &orig, &poster)
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			if err != nil {
				return err
			}
			if konto != hauptKonto {
				if _, err := t.ExecContext(ctx,
					`INSERT OR IGNORE INTO wish_dabei (wish_id, account_id) VALUES (?, ?)`, haupt, konto); err != nil {
					return err
				}
			}
			if _, err := t.ExecContext(ctx,
				`INSERT OR IGNORE INTO wish_dabei (wish_id, account_id)
				 SELECT ?, account_id FROM wish_dabei WHERE wish_id = ? AND account_id != ?`,
				haupt, id, hauptKonto); err != nil {
				return err
			}
			if _, err := t.ExecContext(ctx,
				`UPDATE wish SET art = COALESCE(art, ?), tmdb_id = COALESCE(tmdb_id, ?), imdb_id = COALESCE(imdb_id, ?),
				                 originaltitel = COALESCE(originaltitel, ?), poster = COALESCE(poster, ?)
				  WHERE id = ?`, nullStr(art), nullStr(tmdb), nullStr(imdb), nullStr(orig), nullStr(poster), haupt); err != nil {
				return err
			}
			if _, err := t.ExecContext(ctx, `DELETE FROM wish WHERE id = ?`, id); err != nil {
				return err
			}
			n++
		}
		return nil
	})
	return n, err
}

// WunschOhneKennung: ein offener Wunsch, dem TMDb- und IMDb-Kennung
// fehlen.
type WunschOhneKennung struct {
	ID    string
	Titel string
	Jahr  int
}

// WuenscheOhneKennung nennt offene Wünsche ohne Kennung, die neusten
// zuerst (höchstens wieviele).
func (s *Store) WuenscheOhneKennung(ctx context.Context, wieviele int) ([]WunschOhneKennung, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, title, COALESCE(year,0) FROM wish
		  WHERE state IN ('open','working') AND tmdb_id IS NULL AND imdb_id IS NULL
		  ORDER BY created_at DESC LIMIT ?`, wieviele)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WunschOhneKennung
	for rows.Next() {
		var w WunschOhneKennung
		if err := rows.Scan(&w.ID, &w.Titel, &w.Jahr); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// WunschKennungNachtragen gibt einem Wunsch ohne Kennung die von TMDb —
// nur, wenn er noch keine hat. Jahr und Originaltitel kommen mit, wenn
// sie fehlen.
func (s *Store) WunschKennungNachtragen(ctx context.Context, id string, d WunschDaten) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE wish SET art = ?, tmdb_id = ?, imdb_id = ?, originaltitel = COALESCE(originaltitel, ?),
		                 poster = COALESCE(poster, ?), year = COALESCE(year, ?)
		  WHERE id = ? AND tmdb_id IS NULL AND imdb_id IS NULL`,
		nullStr(d.Art), nullStr(d.TMDbID), nullStr(d.IMDbID), nullStr(d.Originaltitel), nullStr(d.PosterPfad),
		nullInt(d.Jahr), id)
	return err
}
