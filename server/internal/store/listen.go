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
	"strings"
	"time"
)

// Wiedergabelisten.
//
// Eine Liste gehört der Person, die sie angelegt hat. Sie entscheidet, ob
// der Haushalt sie sehen darf (geteilt) und ob andere darin mitschreiben
// dürfen (gemeinsam). Ohne beides sieht sie niemand sonst — auch nicht
// der Besitzer des Servers. Jede Abfrage hier trägt darum das Konto in
// der Bedingung; es gibt keine, die alle Listen aller Konten liest.

type ListenPerson struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Avatar string `json:"avatar"`
}

type Liste struct {
	ID              string            `json:"id"`
	Name            string            `json:"name"`
	Von             ListenPerson      `json:"von"`
	Meine           bool              `json:"meine"`
	Geteilt         bool              `json:"geteilt"`
	Gemeinsam       bool              `json:"gemeinsam"`
	DarfAendern     bool              `json:"darfAendern"`
	Anzahl          int               `json:"anzahl"`
	ErstelltAm      time.Time         `json:"erstelltAm"`
	ItemIDs         []string          `json:"-"`
	HinzugefuegtVon map[string]string `json:"-"` // Titel → Name
}

var ErrNichtErlaubt = errors.New("das darfst du hier nicht")

const listeCols = `p.id, p.name, p.account_id, a.name, a.avatar, p.shared, p.collaborative,
	p.created_at, (SELECT COUNT(*) FROM playlist_item pi WHERE pi.playlist_id = p.id)`

func scanListe(sc interface{ Scan(...any) error }, ich string) (*Liste, error) {
	var l Liste
	var geteilt, gemeinsam int
	var am int64
	if err := sc.Scan(&l.ID, &l.Name, &l.Von.ID, &l.Von.Name, &l.Von.Avatar,
		&geteilt, &gemeinsam, &am, &l.Anzahl); err != nil {
		return nil, err
	}
	l.Geteilt, l.Gemeinsam = geteilt == 1, gemeinsam == 1
	l.Meine = l.Von.ID == ich
	l.DarfAendern = l.Meine || (l.Geteilt && l.Gemeinsam)
	l.ErstelltAm = time.Unix(am, 0)
	return &l, nil
}

// Listen gibt die eigenen Listen und die, die andere im Haushalt geteilt
// haben. Eigene zuerst.
// geteiltFuer: eine fremde Liste ist sichtbar, wenn sie geteilt ist und
// weder die fragende Person (der Platzhalter) noch die Besitzerin der
// Liste ein Gast ist (haushalt.go).
var geteiltFuer = `(p.shared = 1 AND NOT EXISTS (SELECT 1 FROM setting
	  WHERE key IN ('gast:' || ?, 'gast:' || p.account_id)))`

func (s *Store) Listen(ctx context.Context, ich string) ([]Liste, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+listeCols+` FROM playlist p JOIN account a ON a.id = p.account_id
		  WHERE p.account_id = ? OR `+geteiltFuer+``, ich, ich)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Liste{}
	for rows.Next() {
		l, err := scanListe(rows, ich)
		if err != nil {
			return nil, err
		}
		out = append(out, *l)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	nachNamenSortieren(out, func(l *Liste) string { return l.Name }, func(l *Liste) string { return l.ID },
		func(l *Liste) bool { return l.Meine })
	return out, nil
}

// Liste holt eine Liste samt Titeln — oder ErrNotFound, wenn es sie
// nicht gibt oder sie nicht geteilt ist. Beides sieht von aussen gleich
// aus: wer eine fremde, private Liste errät, erfährt nicht einmal, dass
// es sie gibt.
func (s *Store) Liste(ctx context.Context, ich, id string) (*Liste, error) {
	l, err := scanListe(s.db.QueryRowContext(ctx,
		`SELECT `+listeCols+` FROM playlist p JOIN account a ON a.id = p.account_id
		  WHERE p.id = ? AND (p.account_id = ? OR `+geteiltFuer+`)`, id, ich, ich), ich)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT pi.item_id, a.name FROM playlist_item pi JOIN account a ON a.id = pi.added_by
		  WHERE pi.playlist_id = ? ORDER BY pi.ord`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	l.ItemIDs, l.HinzugefuegtVon = []string{}, map[string]string{}
	for rows.Next() {
		var item, wer string
		if err := rows.Scan(&item, &wer); err != nil {
			return nil, err
		}
		l.ItemIDs = append(l.ItemIDs, item)
		l.HinzugefuegtVon[item] = wer
	}
	return l, rows.Err()
}

func (s *Store) ListeAnlegen(ctx context.Context, ich, name string) (*Liste, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("die liste braucht einen namen")
	}
	if len([]rune(name)) > 80 {
		return nil, errors.New("der name ist zu lang")
	}
	id := newID("pl")
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO playlist (id, account_id, name, shared, collaborative, created_at)
		 VALUES (?, ?, ?, 0, 0, ?)`, id, ich, name, time.Now().Unix()); err != nil {
		return nil, err
	}
	return s.Liste(ctx, ich, id)
}

// eigene prüft, dass die Liste der Person gehört. Umbenennen, teilen und
// löschen darf nur, wer sie angelegt hat — auch bei einer gemeinsamen.
func (s *Store) eigene(ctx context.Context, ich, id string) error {
	var n int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM playlist WHERE id = ? AND account_id = ?`, id, ich).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		// Gibt es sie und ist sie geteilt, darf die Person sie sehen —
		// dann ist es ein „darfst du nicht", sonst ein „gibt es nicht".
		if l, err := s.Liste(ctx, ich, id); err == nil && l != nil {
			return ErrNichtErlaubt
		}
		return ErrNotFound
	}
	return nil
}

func (s *Store) ListeAendern(ctx context.Context, ich, id string, name *string, geteilt, gemeinsam *bool) error {
	if err := s.eigene(ctx, ich, id); err != nil {
		return err
	}
	if name != nil {
		n := strings.TrimSpace(*name)
		if n == "" {
			return errors.New("die liste braucht einen namen")
		}
		if len([]rune(n)) > 80 {
			return errors.New("der name ist zu lang")
		}
		if _, err := s.db.ExecContext(ctx,
			`UPDATE playlist SET name = ? WHERE id = ? AND account_id = ?`, n, id, ich); err != nil {
			return err
		}
	}
	if geteilt != nil {
		if _, err := s.db.ExecContext(ctx,
			`UPDATE playlist SET shared = ? WHERE id = ? AND account_id = ?`, b2i(*geteilt), id, ich); err != nil {
			return err
		}
		// Nicht mehr geteilt heisst auch: nicht mehr gemeinsam.
		if !*geteilt {
			if _, err := s.db.ExecContext(ctx,
				`UPDATE playlist SET collaborative = 0 WHERE id = ? AND account_id = ?`, id, ich); err != nil {
				return err
			}
		}
	}
	if gemeinsam != nil {
		// Gemeinsam geht nur geteilt — wer mitschreiben soll, muss die
		// Liste auch sehen.
		q := `UPDATE playlist SET collaborative = ? WHERE id = ? AND account_id = ?`
		args := []any{b2i(*gemeinsam), id, ich}
		if *gemeinsam {
			q = `UPDATE playlist SET collaborative = 1, shared = 1 WHERE id = ? AND account_id = ?`
			args = []any{id, ich}
		}
		if _, err := s.db.ExecContext(ctx, q, args...); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ListeLoeschen(ctx context.Context, ich, id string) error {
	if err := s.eigene(ctx, ich, id); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM playlist WHERE id = ? AND account_id = ?`, id, ich)
	return err
}

// darfSchreiben: die eigene Liste, oder eine geteilte, gemeinsame.
func (s *Store) darfSchreiben(ctx context.Context, ich, id string) error {
	l, err := s.Liste(ctx, ich, id)
	if err != nil {
		return err
	}
	if !l.DarfAendern {
		return ErrNichtErlaubt
	}
	return nil
}

func (s *Store) ListeHinzufuegen(ctx context.Context, ich, id, itemID string) error {
	if err := s.darfSchreiben(ctx, ich, id); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO playlist_item (playlist_id, item_id, ord, added_by)
		 VALUES (?, ?, COALESCE((SELECT MAX(ord) FROM playlist_item WHERE playlist_id = ?), 0) + 1, ?)
		 ON CONFLICT (playlist_id, item_id) DO NOTHING`, id, itemID, id, ich)
	return err
}

func (s *Store) ListeEntfernen(ctx context.Context, ich, id, itemID string) error {
	if err := s.darfSchreiben(ctx, ich, id); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM playlist_item WHERE playlist_id = ? AND item_id = ?`, id, itemID)
	return err
}

// ListeOrdnen setzt die Reihenfolge. Titel, die nicht genannt sind,
// rutschen ans Ende — so geht nichts verloren, wenn zwei gleichzeitig
// ordnen und einer einen Titel noch nicht kannte.
func (s *Store) ListeOrdnen(ctx context.Context, ich, id string, itemIDs []string) error {
	if err := s.darfSchreiben(ctx, ich, id); err != nil {
		return err
	}
	return s.tx(ctx, func(t *sql.Tx) error {
		pos := map[string]int{}
		for i, it := range itemIDs {
			if _, schon := pos[it]; !schon {
				pos[it] = i + 1
			}
		}
		rows, err := t.QueryContext(ctx,
			`SELECT item_id FROM playlist_item WHERE playlist_id = ? ORDER BY ord`, id)
		if err != nil {
			return err
		}
		var alle []string
		for rows.Next() {
			var it string
			if err := rows.Scan(&it); err != nil {
				rows.Close()
				return err
			}
			alle = append(alle, it)
		}
		rows.Close()
		hinten := len(itemIDs) + 1
		for _, it := range alle {
			p, ok := pos[it]
			if !ok {
				p = hinten
				hinten++
			}
			if _, err := t.ExecContext(ctx,
				`UPDATE playlist_item SET ord = ? WHERE playlist_id = ? AND item_id = ?`,
				p, id, it); err != nil {
				return err
			}
		}
		return nil
	})
}

// ListenMitTitel nennt, in welchen der für die Person sichtbaren Listen
// ein Titel schon steht — für das Häkchen beim „Zur Liste".
func (s *Store) ListenMitTitel(ctx context.Context, ich, itemID string) (map[string]bool, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT p.id FROM playlist p JOIN playlist_item pi ON pi.playlist_id = p.id
		  WHERE pi.item_id = ? AND (p.account_id = ? OR `+geteiltFuer+`)`, itemID, ich, ich)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}
