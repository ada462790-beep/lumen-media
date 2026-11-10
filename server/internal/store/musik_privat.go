// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

// Was einem Profil an Musik gehört: wie oft es einen Titel gehört hat, in
// welcher Reihenfolge, seine Herzen und seine Wiedergabelisten.
//
// Dieselben Regeln wie in playback.go: jede Funktion nimmt das Konto als
// zweiten Parameter, jede Abfrage nennt account_id, nichts zählt über
// Profile hinweg. Es gibt keine Uhrzeit — nur eine laufende Nummer je
// Profil. privacy_test.go wacht darüber.

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// ---------------------------------------------------------------- Hörstand

// MusikGehoert vermerkt, dass das Profil einen Titel (fast) ganz gehört
// hat: die Zahl steigt um eins, und der Titel rückt in „Zuletzt gehört"
// nach vorn. Die laufende Nummer zählt pro Profil und kennt keine Zeit.
func (s *Store) MusikGehoert(ctx context.Context, accountID, titelID string) error {
	return s.tx(ctx, func(t *sql.Tx) error {
		if _, err := t.ExecContext(ctx,
			`INSERT INTO musik_hoerreihe (account_id, reihe) VALUES (?, 1)
			 ON CONFLICT (account_id) DO UPDATE SET reihe = reihe + 1`, accountID); err != nil {
			return err
		}
		var reihe int64
		if err := t.QueryRowContext(ctx, `SELECT reihe FROM musik_hoerreihe WHERE account_id = ?`, accountID).Scan(&reihe); err != nil {
			return err
		}
		_, err := t.ExecContext(ctx,
			`INSERT INTO musik_hoerstand (account_id, titel_id, anzahl, reihenfolge) VALUES (?, ?, 1, ?)
			 ON CONFLICT (account_id, titel_id) DO UPDATE SET anzahl = anzahl + 1, reihenfolge = excluded.reihenfolge`,
			accountID, titelID, reihe)
		return err
	})
}

// MusikZuletztGehoert: die Titel, die dieses Profil zuletzt gehört hat,
// neueste zuerst.
func (s *Store) MusikZuletztGehoert(ctx context.Context, accountID string, limit int) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT titel_id FROM musik_hoerstand WHERE account_id = ? ORDER BY reihenfolge DESC LIMIT ?`,
		accountID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectStrings(rows)
}

// MusikWieOft: wie oft dieses Profil die genannten Titel gehört hat. Für
// „Beliebt" auf der Seite eines Interpreten — beliebt bei dir, nicht im
// Haushalt.
func (s *Store) MusikWieOft(ctx context.Context, accountID string, titelIDs []string) (map[string]int, error) {
	out := map[string]int{}
	for a := 0; a < len(titelIDs); a += 400 {
		e := min(a+400, len(titelIDs))
		rows, err := s.db.QueryContext(ctx,
			`SELECT titel_id, anzahl FROM musik_hoerstand WHERE account_id = ? AND titel_id IN (`+placeholders(e-a)+`)`,
			append([]any{accountID}, toAny(titelIDs[a:e])...)...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id string
			var n int
			if err := rows.Scan(&id, &n); err != nil {
				rows.Close()
				return nil, err
			}
			out[id] = n
		}
		rows.Close()
	}
	return out, nil
}

// MusikHoerverlaufLoeschen vergisst alles, was dieses Profil gehört hat.
func (s *Store) MusikHoerverlaufLoeschen(ctx context.Context, accountID string) error {
	return s.tx(ctx, func(t *sql.Tx) error {
		if _, err := t.ExecContext(ctx, `DELETE FROM musik_hoerstand WHERE account_id = ?`, accountID); err != nil {
			return err
		}
		_, err := t.ExecContext(ctx, `DELETE FROM musik_hoerreihe WHERE account_id = ?`, accountID)
		return err
	})
}

// ---------------------------------------------------------------- Herzen

// MusikFavoritArten: was ein Herz bekommen kann.
var MusikFavoritArten = map[string]bool{"titel": true, "album": true, "interpret": true}

func (s *Store) MusikFavoritSetzen(ctx context.Context, accountID, art, zielID string, an bool) error {
	if !MusikFavoritArten[art] {
		return errors.New("unbekannte art")
	}
	if an {
		_, err := s.db.ExecContext(ctx,
			`INSERT INTO musik_favorit (account_id, art, ziel_id) VALUES (?,?,?)
			 ON CONFLICT DO NOTHING`, accountID, art, zielID)
		return err
	}
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM musik_favorit WHERE account_id = ? AND art = ? AND ziel_id = ?`, accountID, art, zielID)
	return err
}

// MusikFavoriten: alle Herzen des Profils je Art, neueste zuerst.
func (s *Store) MusikFavoriten(ctx context.Context, accountID string) (map[string][]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT art, ziel_id FROM musik_favorit WHERE account_id = ? ORDER BY rowid DESC`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]string{"titel": {}, "album": {}, "interpret": {}}
	for rows.Next() {
		var art, id string
		if err := rows.Scan(&art, &id); err != nil {
			return nil, err
		}
		out[art] = append(out[art], id)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------- Listen

// MusikListe: eine Wiedergabeliste für Musik. Aufbau und Regeln wie bei
// den Listen für Filme (listen.go) — eine eigene Tabelle nur, weil deren
// Einträge auf „item" zeigen und Musiktitel dort nicht stehen.
type MusikListe struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Von         ListenPerson `json:"von"`
	Meine       bool         `json:"meine"`
	Geteilt     bool         `json:"geteilt"`
	Gemeinsam   bool         `json:"gemeinsam"`
	DarfAendern bool         `json:"darfAendern"`
	Anzahl      int          `json:"anzahl"`
	TitelIDs    []string     `json:"-"`
}

func scanMusikListe(sc interface{ Scan(...any) error }, accountID string) (*MusikListe, error) {
	var l MusikListe
	var geteilt, gemeinsam int
	if err := sc.Scan(&l.ID, &l.Name, &l.Von.ID, &l.Von.Name, &l.Von.Avatar, &geteilt, &gemeinsam); err != nil {
		return nil, err
	}
	l.Geteilt, l.Gemeinsam = geteilt == 1, gemeinsam == 1
	l.Meine = l.Von.ID == accountID
	l.DarfAendern = l.Meine || (l.Geteilt && l.Gemeinsam)
	return &l, nil
}

// musikListenAnzahl zählt die Einträge — eigene Abfrage auf die
// Eintragstabelle, die selbst nicht verrät, wem eine Liste gehört.
func (s *Store) musikListenAnzahl(ctx context.Context, ls []MusikListe) error {
	if len(ls) == 0 {
		return nil
	}
	ids := make([]any, len(ls))
	for i, l := range ls {
		ids[i] = l.ID
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT liste_id, count(*) FROM musik_listeneintrag WHERE liste_id IN (`+placeholders(len(ids))+`) GROUP BY liste_id`, ids...)
	if err != nil {
		return err
	}
	defer rows.Close()
	n := map[string]int{}
	for rows.Next() {
		var id string
		var c int
		if err := rows.Scan(&id, &c); err != nil {
			return err
		}
		n[id] = c
	}
	for i := range ls {
		ls[i].Anzahl = n[ls[i].ID]
	}
	return rows.Err()
}

// MusikListen: die eigenen und die im Haushalt geteilten, eigene zuerst.
func (s *Store) MusikListen(ctx context.Context, accountID string) ([]MusikListe, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT l.id, l.name, l.account_id, a.name, a.avatar, l.geteilt, l.gemeinsam
		   FROM musik_wiedergabeliste l JOIN account a ON a.id = l.account_id
		  WHERE l.account_id = ? OR l.geteilt = 1`, accountID)
	if err != nil {
		return nil, err
	}
	out := []MusikListe{}
	for rows.Next() {
		l, err := scanMusikListe(rows, accountID)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, *l)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	nachNamenSortieren(out, func(l *MusikListe) string { return l.Name }, func(l *MusikListe) string { return l.ID },
		func(l *MusikListe) bool { return l.Meine })
	return out, s.musikListenAnzahl(ctx, out)
}

// MusikListeHolen: eine Liste samt Titeln — ErrNotFound, wenn es sie
// nicht gibt oder sie fremd und nicht geteilt ist.
func (s *Store) MusikListeHolen(ctx context.Context, accountID, id string) (*MusikListe, error) {
	l, err := scanMusikListe(s.db.QueryRowContext(ctx,
		`SELECT l.id, l.name, l.account_id, a.name, a.avatar, l.geteilt, l.gemeinsam
		   FROM musik_wiedergabeliste l JOIN account a ON a.id = l.account_id
		  WHERE l.id = ? AND (l.account_id = ? OR l.geteilt = 1)`, id, accountID), accountID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT titel_id FROM musik_listeneintrag WHERE liste_id = ? ORDER BY ord`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if l.TitelIDs, err = collectStrings(rows); err != nil {
		return nil, err
	}
	l.Anzahl = len(l.TitelIDs)
	return l, nil
}

func (s *Store) MusikListeAnlegen(ctx context.Context, accountID, name string) (*MusikListe, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("die liste braucht einen namen")
	}
	if len([]rune(name)) > 80 {
		return nil, errors.New("der name ist zu lang")
	}
	id := newID("ml")
	// Nur der Tag: wann jemand eine Liste angelegt hat, geht niemanden
	// etwas an, und zum Sortieren reicht der Name.
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO musik_wiedergabeliste (id, account_id, name, geteilt, gemeinsam, angelegt)
		 VALUES (?, ?, ?, 0, 0, ?)`, id, accountID, name, heute()); err != nil {
		return nil, err
	}
	return s.MusikListeHolen(ctx, accountID, id)
}

func (s *Store) musikListeEigene(ctx context.Context, accountID, id string) error {
	var wem string
	err := s.db.QueryRowContext(ctx,
		`SELECT account_id FROM musik_wiedergabeliste WHERE id = ? AND (account_id = ? OR geteilt = 1)`,
		id, accountID).Scan(&wem)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if wem != accountID {
		return ErrNichtErlaubt
	}
	return nil
}

func (s *Store) MusikListeAendern(ctx context.Context, accountID, id string, name *string, geteilt, gemeinsam *bool) error {
	if err := s.musikListeEigene(ctx, accountID, id); err != nil {
		return err
	}
	if name != nil {
		n := strings.TrimSpace(*name)
		if n == "" || len([]rune(n)) > 80 {
			return errors.New("ungültiger name")
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE musik_wiedergabeliste SET name = ? WHERE id = ? AND account_id = ?`,
			n, id, accountID); err != nil {
			return err
		}
	}
	if geteilt != nil {
		if _, err := s.db.ExecContext(ctx, `UPDATE musik_wiedergabeliste SET geteilt = ? WHERE id = ? AND account_id = ?`,
			b2i(*geteilt), id, accountID); err != nil {
			return err
		}
		// Nicht mehr geteilt heisst auch nicht mehr gemeinsam.
		if !*geteilt {
			f := false
			gemeinsam = &f
		}
	}
	if gemeinsam != nil {
		if _, err := s.db.ExecContext(ctx, `UPDATE musik_wiedergabeliste SET gemeinsam = ? WHERE id = ? AND account_id = ?`,
			b2i(*gemeinsam), id, accountID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) MusikListeLoeschen(ctx context.Context, accountID, id string) error {
	if err := s.musikListeEigene(ctx, accountID, id); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM musik_wiedergabeliste WHERE id = ? AND account_id = ?`, id, accountID)
	return err
}

func (s *Store) musikListeSchreiben(ctx context.Context, accountID, id string) error {
	l, err := s.MusikListeHolen(ctx, accountID, id)
	if err != nil {
		return err
	}
	if !l.DarfAendern {
		return ErrNichtErlaubt
	}
	return nil
}

// MusikListeHinzufuegen hängt Titel hinten an. Was schon drinsteht, bleibt
// an seiner Stelle.
func (s *Store) MusikListeHinzufuegen(ctx context.Context, accountID, id string, titelIDs []string) error {
	if err := s.musikListeSchreiben(ctx, accountID, id); err != nil {
		return err
	}
	return s.tx(ctx, func(t *sql.Tx) error {
		for _, tid := range titelIDs {
			if _, err := t.ExecContext(ctx,
				`INSERT INTO musik_listeneintrag (liste_id, titel_id, ord, von_konto)
				 VALUES (?, ?, COALESCE((SELECT MAX(ord) FROM musik_listeneintrag WHERE liste_id = ?), 0) + 1, ?)
				 ON CONFLICT (liste_id, titel_id) DO NOTHING`, id, tid, id, accountID); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Store) MusikListeEntfernen(ctx context.Context, accountID, id, titelID string) error {
	if err := s.musikListeSchreiben(ctx, accountID, id); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM musik_listeneintrag WHERE liste_id = ? AND titel_id = ?`, id, titelID)
	return err
}

// MusikListeOrdnen: neue Reihenfolge. Nicht genannte Titel rutschen ans
// Ende, damit nichts verloren geht, wenn zwei gleichzeitig ordnen.
func (s *Store) MusikListeOrdnen(ctx context.Context, accountID, id string, titelIDs []string) error {
	l, err := s.MusikListeHolen(ctx, accountID, id)
	if err != nil {
		return err
	}
	if !l.DarfAendern {
		return ErrNichtErlaubt
	}
	pos := map[string]int{}
	for i, t := range titelIDs {
		if _, schon := pos[t]; !schon {
			pos[t] = i + 1
		}
	}
	hinten := len(titelIDs) + 1
	return s.tx(ctx, func(t *sql.Tx) error {
		for _, tid := range l.TitelIDs {
			p, ok := pos[tid]
			if !ok {
				p = hinten
				hinten++
			}
			if _, err := t.ExecContext(ctx, `UPDATE musik_listeneintrag SET ord = ? WHERE liste_id = ? AND titel_id = ?`,
				p, id, tid); err != nil {
				return err
			}
		}
		return nil
	})
}
