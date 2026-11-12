// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

import (
	"context"
	"database/sql"
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
	// AccountID: wer sich das gewünscht hat — nur für die Prüfung der
	// Rechte, nie in der Antwort.
	AccountID string `json:"-"`

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
	// Auch: wer sich angeschlossen hat — nur in der Liste der Verwaltung,
	// und dort nur, wen sie verwaltet (die API filtert nach AuchIDs).
	Auch []string `json:"auch,omitempty"`
	// AuchIDs: die Konten zu Auch, in derselben Reihenfolge — nie in der
	// Antwort.
	AuchIDs []string `json:"-"`
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
	// Nur der Tag: Wann genau sich jemand etwas gewünscht hat, geht die
	// Verwaltung nichts an. Die Reihenfolge hält die Zeilennummer.
	tag := heute()
	w := &Wish{ID: newID("w"), Title: title, Year: year, State: "open",
		CreatedAt: time.Unix(tag, 0), Mine: true, AccountID: accountID}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO wish (id, account_id, title, year, state, created_at, updated_at)
		 VALUES (?,?,?,?,?,?,?)`,
		w.ID, accountID, title, nullInt(year), w.State, tag, tag)
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
	                w.created_at DESC, w.rowid DESC`

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
		// Auf den Tag gekürzt, auch alte Einträge mit Uhrzeit.
		created -= created % 86400
		w.CreatedAt, w.Mine, w.AccountID = time.Unix(created, 0).UTC(), owner == accountID, owner
		out = append(out, w)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return s.wunschErgaenzen(ctx, out, accountID, all)
}

// WunschVon: wer sich diesen Wunsch gewünscht hat (für die Rechte).
func (s *Store) WunschVon(ctx context.Context, id string) (string, error) {
	var acc string
	err := s.db.QueryRowContext(ctx, `SELECT account_id FROM wish WHERE id = ?`, id).Scan(&acc)
	if err != nil {
		return "", ErrNotFound
	}
	return acc, nil
}

func (s *Store) SetWishState(ctx context.Context, id, state, note string) error {
	switch state {
	case "open", "working", "done", "declined":
	default:
		return errors.New("unbekannter zustand")
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE wish SET state = ?, note = ?, updated_at = ? WHERE id = ?`,
		state, nullStr(note), heute(), id)
	return err
}

// WunschNotizErfuellt: die Notiz, die Lumen selbst an einen erfüllten
// Wunsch schreibt — ein allgemeiner Stand, keine Nachricht der Verwaltung.
const WunschNotizErfuellt = "Ist jetzt in der Bibliothek"

// WunschSicht wählt aus den Titeln, die einen Wunsch erfüllen würden,
// den ersten, den das Konto sehen darf ("" = keinen). Die Regeln dafür
// (Bibliotheken, Altersgrenze) kennt die API.
type WunschSicht func(ctx context.Context, kontoID string, itemIDs []string) string

// WuenscheErfuellen wie WuenscheErfuellenFuer, ohne Blick auf die Rechte
// (für Tests und Werkzeuge).
func (s *Store) WuenscheErfuellen(ctx context.Context) (int, error) {
	return s.WuenscheErfuellenFuer(ctx, nil)
}

// WuenscheErfuellenFuer schliesst offene Wünsche, deren Titel inzwischen
// in der Bibliothek steht — gleicher Titel (ohne Rücksicht auf Gross- und
// Kleinschreibung), und das Jahr, wenn der Wunsch eines nennt. Die Notiz
// sagt der Person, dass es da ist. Zurück kommt, wie viele es waren.
//
// Aber nur, wenn die Person den Titel auch sehen darf (sieht): Wünscht
// sich ein Kind einen Film, der in einer Bibliothek liegt, die es nicht
// sieht, verriete „Ist jetzt in der Bibliothek", dass es ihn gibt. Dann
// bleibt der Wunsch offen.
func (s *Store) WuenscheErfuellenFuer(ctx context.Context, sieht WunschSicht) (int, error) {
	// Zuerst über TMDb-/IMDb-Kennung (wunsch_kennung.go).
	nk, err := s.wuenscheNachKennungErfuellen(ctx, sieht)
	if err != nil {
		return 0, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT w.id, w.account_id, i.id FROM wish w
		   JOIN item i ON i.kind IN ('movie', 'series')
		    AND lower(i.title) = lower(trim(w.title))
		    AND (COALESCE(w.year, 0) = 0 OR i.year = w.year)
		  WHERE w.state IN ('open', 'working')
		  ORDER BY w.id, i.id`)
	if err != nil {
		return 0, err
	}
	kand, err := wunschKandidaten(rows)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, k := range kand {
		it := k.waehle(ctx, sieht)
		if it == "" {
			continue
		}
		res, err := s.db.ExecContext(ctx,
			`UPDATE wish SET state = 'done', note = 'Ist jetzt in der Bibliothek', updated_at = ?,
			                 erfuellt_item = COALESCE(erfuellt_item, ?)
			  WHERE id = ? AND state IN ('open', 'working')`, heute(), it, k.wunsch)
		if err != nil {
			return n + nk, err
		}
		if x, _ := res.RowsAffected(); x > 0 {
			n++
		}
	}
	if err := s.WuenscheVerknuepfen(ctx); err != nil {
		return n + nk, err
	}
	return n + nk, nil
}

// wunschKandidat: ein offener Wunsch, wer ihn hat, und die Titel, die ihn
// erfüllen würden.
type wunschKandidat struct {
	wunsch, konto string
	items         []string
}

// wunschKandidaten liest Zeilen (wunsch, konto, item), nach Wunsch sortiert.
func wunschKandidaten(rows *sql.Rows) ([]wunschKandidat, error) {
	defer rows.Close()
	var out []wunschKandidat
	for rows.Next() {
		var w, konto, it string
		if err := rows.Scan(&w, &konto, &it); err != nil {
			return nil, err
		}
		if n := len(out); n > 0 && out[n-1].wunsch == w {
			out[n-1].items = append(out[n-1].items, it)
			continue
		}
		out = append(out, wunschKandidat{wunsch: w, konto: konto, items: []string{it}})
	}
	return out, rows.Err()
}

func (k wunschKandidat) waehle(ctx context.Context, sieht WunschSicht) string {
	if len(k.items) == 0 {
		return ""
	}
	if sieht == nil {
		return k.items[0]
	}
	return sieht(ctx, k.konto, k.items)
}
