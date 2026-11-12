// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

import (
	"context"
	"errors"
	"time"
)

// Ein Wunsch ist das Einzige, was den Weg von einem Profil zum Besitzer
// findet — und zwar, weil die Person ihn ausdrücklich abschickt. Mehr als
// Titel, Jahr und Absender steht nicht drin.
type Wish struct {
	ID        string    `json:"id"`
	By        string    `json:"by"` // Anzeigename
	Avatar    string    `json:"avatar"`
	Title     string    `json:"title"`
	Year      int       `json:"year,omitempty"`
	State     string    `json:"state"` // open | working | done | declined
	Note      string    `json:"note,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	Mine      bool      `json:"mine"`

	// Mit einem Vorschlag von TMDb gewünscht (wunsch_kennung.go) — sonst leer.
	Art           string `json:"art,omitempty"` // movie | series
	TMDbID        string `json:"tmdbId,omitempty"`
	IMDbID        string `json:"imdbId,omitempty"`
	Originaltitel string `json:"originaltitel,omitempty"`
	PosterPfad    string `json:"-"`
	Poster        string `json:"poster,omitempty"` // Adresse bei Lumen, setzt die API
	// ItemID: der Titel, der den Wunsch erfüllt hat (nur wenn sichtbar).
	ItemID string `json:"itemId,omitempty"`
	// Dabei: jemand anders hatte den Wunsch schon, dieses Profil hat sich
	// angeschlossen. Von wem er ist, steht dann nicht da.
	Dabei bool `json:"dabei,omitempty"`
	// Auch: wer sich angeschlossen hat — nur in der Liste des Besitzers.
	Auch []string `json:"auch,omitempty"`
}

func (s *Store) CreateWish(ctx context.Context, accountID, title string, year int) (*Wish, error) {
	if title == "" {
		return nil, errors.New("titel fehlt")
	}
	// Derselbe offene Wunsch noch einmal (zweimal getippt): den alten geben.
	var alt string
	if err := s.db.QueryRowContext(ctx,
		`SELECT id FROM wish WHERE account_id = ? AND lower(title) = lower(?)
		    AND COALESCE(year,0) = ? AND state IN ('open','working')`,
		accountID, title, year).Scan(&alt); err == nil {
		if liste, err := s.Wishes(ctx, accountID, false); err == nil {
			for i := range liste {
				if liste[i].ID == alt {
					return &liste[i], nil
				}
			}
		}
	}
	now := time.Now()
	w := &Wish{ID: newID("w"), Title: title, Year: year, State: "open",
		CreatedAt: now, Mine: true}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO wish (id, account_id, title, year, state, created_at, updated_at)
		 VALUES (?,?,?,?,?,?,?)`,
		w.ID, accountID, title, nullInt(year), w.State, now.Unix(), now.Unix())
	if err != nil {
		return nil, err
	}
	if acc, err := s.Account(ctx, accountID); err == nil {
		w.By, w.Avatar = acc.Name, acc.Avatar
	}
	return w, nil
}

// Wishes gibt dem Besitzer alle Wünsche des Haushalts, allen anderen die
// eigenen. Dass jemand etwas gewünscht hat, sagt nichts darüber, was er
// schaut — gewünscht wird, was noch nicht da ist.
func (s *Store) Wishes(ctx context.Context, accountID string, all bool) ([]Wish, error) {
	q := `SELECT w.id, a.name, a.avatar, w.title, COALESCE(w.year,0), w.state,
	             COALESCE(w.note,''), w.created_at, w.account_id
	        FROM wish w JOIN account a ON a.id = w.account_id`
	args := []any{}
	if !all {
		q += ` WHERE w.account_id = ?`
		args = append(args, accountID)
	}
	q += ` ORDER BY CASE w.state WHEN 'open' THEN 0 WHEN 'working' THEN 1 ELSE 2 END,
	                w.created_at DESC`

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Wish{}
	for rows.Next() {
		var w Wish
		var created int64
		var owner string
		if err := rows.Scan(&w.ID, &w.By, &w.Avatar, &w.Title, &w.Year, &w.State,
			&w.Note, &created, &owner); err != nil {
			return nil, err
		}
		w.CreatedAt, w.Mine = time.Unix(created, 0), owner == accountID
		out = append(out, w)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return s.wunschErgaenzen(ctx, out, accountID, all)
}

func (s *Store) SetWishState(ctx context.Context, id, state, note string) error {
	switch state {
	case "open", "working", "done", "declined":
	default:
		return errors.New("unbekannter zustand")
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE wish SET state = ?, note = ?, updated_at = ? WHERE id = ?`,
		state, nullStr(note), time.Now().Unix(), id)
	return err
}

// WuenscheErfuellen schliesst offene Wünsche, deren Titel inzwischen in
// der Bibliothek steht — gleicher Titel (ohne Rücksicht auf Gross- und
// Kleinschreibung), und das Jahr, wenn der Wunsch eines nennt. Die Notiz
// sagt der Person, dass es da ist. Zurück kommt, wie viele es waren.
func (s *Store) WuenscheErfuellen(ctx context.Context) (int, error) {
	// Zuerst über TMDb-/IMDb-Kennung (wunsch_kennung.go).
	nk, err := s.wuenscheNachKennungErfuellen(ctx)
	if err != nil {
		return 0, err
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE wish SET state = 'done', note = 'Ist jetzt in der Bibliothek', updated_at = ?
		  WHERE state IN ('open', 'working')
		    AND EXISTS (SELECT 1 FROM item i
		                 WHERE i.kind IN ('movie', 'series')
		                   AND lower(i.title) = lower(trim(wish.title))
		                   AND (COALESCE(wish.year, 0) = 0 OR i.year = wish.year))`,
		time.Now().Unix())
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	if err := s.WuenscheVerknuepfen(ctx); err != nil {
		return int(n) + nk, err
	}
	return int(n) + nk, nil
}
