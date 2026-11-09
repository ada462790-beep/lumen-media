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
}

func (s *Store) CreateWish(ctx context.Context, accountID, title string, year int) (*Wish, error) {
	if title == "" {
		return nil, errors.New("titel fehlt")
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
	return out, rows.Err()
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
	return int(n), nil
}
