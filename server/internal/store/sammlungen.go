// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

// Eigene Sammlungen.
//
// Filmreihen bildet Lumen selbst. Sammlungen stellt die Verwaltung von
// Hand zusammen: „Weihnachtsfilme", „Marvel in Reihenfolge", „Für
// verregnete Sonntage". Sie können Filme und Serien enthalten, von Hand
// sortiert sein oder nach Jahr und Titel, und optional eine Regel
// tragen (Genre, Jahre, Stichwort), die passende Titel von selbst
// dazunimmt.
//
// Wie Filmreihen gehören Sammlungen niemandem und sagen nichts darüber,
// wer was schaut. Was jemand daraus sehen darf, entscheidet wie überall
// die Freigabe — das prüft die API. Die Regel wird darum erst beim Lesen
// angewendet, mit den Rechten der Person, die liest.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// SammlungsRegel nimmt passende Titel von selbst auf. Leere Felder
// schränken nicht ein; eine Regel ohne ein einziges gesetztes Feld gilt
// als keine Regel — sonst stünde plötzlich die ganze Mediathek darin.
type SammlungsRegel struct {
	Genre     string `json:"genre,omitempty"`
	JahrVon   int    `json:"jahrVon,omitempty"`
	JahrBis   int    `json:"jahrBis,omitempty"`
	Stichwort string `json:"stichwort,omitempty"`
	// Art: "movie", "series" oder leer für beides.
	Art string `json:"art,omitempty"`
}

// Leer: keine einzige Einschränkung, also keine Regel.
func (r SammlungsRegel) Leer() bool {
	return r.Genre == "" && r.JahrVon == 0 && r.JahrBis == 0 && strings.TrimSpace(r.Stichwort) == ""
}

type Sammlung struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Beschreibung string `json:"beschreibung"`
	// Bild: Datei im Bilderordner; leer = aus den Postern zusammengesetzt.
	Bild       string          `json:"-"`
	Sortierung string          `json:"sortierung"` // hand | jahr | titel
	Regel      *SammlungsRegel `json:"regel,omitempty"`
	AufStart   bool            `json:"aufStart"`
	// Die von Hand hinzugefügten Titel in ihrer Reihenfolge.
	ItemIDs []string `json:"-"`
}

// Sortierungen, die es gibt.
var Sammlungssortierungen = []string{"hand", "jahr", "titel"}

func sortierungGueltig(s string) bool {
	for _, x := range Sammlungssortierungen {
		if x == s {
			return true
		}
	}
	return false
}

const sammlungCols = `id, name, beschreibung, COALESCE(bild,''), sortierung, regel, auf_start`

func scanSammlung(sc interface{ Scan(...any) error }) (*Sammlung, error) {
	var x Sammlung
	var regel string
	var start int
	if err := sc.Scan(&x.ID, &x.Name, &x.Beschreibung, &x.Bild, &x.Sortierung, &regel, &start); err != nil {
		return nil, err
	}
	x.AufStart = start == 1
	if regel != "" {
		var r SammlungsRegel
		if json.Unmarshal([]byte(regel), &r) == nil && !r.Leer() {
			x.Regel = &r
		}
	}
	return &x, nil
}

// Sammlungen liefert alle Sammlungen samt ihrer von Hand gewählten Titel,
// nach Namen sortiert.
func (s *Store) Sammlungen(ctx context.Context) ([]Sammlung, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+sammlungCols+` FROM sammlung ORDER BY lower(name), id`)
	if err != nil {
		return nil, err
	}
	out := []Sammlung{}
	for rows.Next() {
		x, err := scanSammlung(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, *x)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Alle Titel auf einmal, nicht Sammlung für Sammlung.
	titel, err := s.db.QueryContext(ctx,
		`SELECT sammlung_id, item_id FROM sammlung_titel ORDER BY sammlung_id, ord, item_id`)
	if err != nil {
		return nil, err
	}
	defer titel.Close()
	je := map[string][]string{}
	for titel.Next() {
		var sid, iid string
		if err := titel.Scan(&sid, &iid); err != nil {
			return nil, err
		}
		je[sid] = append(je[sid], iid)
	}
	for i := range out {
		out[i].ItemIDs = je[out[i].ID]
	}
	return out, titel.Err()
}

func (s *Store) Sammlung(ctx context.Context, id string) (*Sammlung, error) {
	x, err := scanSammlung(s.db.QueryRowContext(ctx, `SELECT `+sammlungCols+` FROM sammlung WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT item_id FROM sammlung_titel WHERE sammlung_id = ? ORDER BY ord, item_id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	x.ItemIDs, err = collectStrings(rows)
	return x, err
}

// SammlungAenderung: nil = nicht angefasst.
type SammlungAenderung struct {
	Name         *string
	Beschreibung *string
	Sortierung   *string
	Regel        *SammlungsRegel // Leer() = Regel entfernen
	AufStart     *bool
}

func regelText(r *SammlungsRegel) (string, error) {
	if r == nil || r.Leer() {
		return "", nil
	}
	if r.Art != "" && r.Art != "movie" && r.Art != "series" {
		return "", fmt.Errorf("unbekannte art")
	}
	b, err := json.Marshal(r)
	return string(b), err
}

func (s *Store) SammlungAnlegen(ctx context.Context, a SammlungAenderung) (*Sammlung, error) {
	if a.Name == nil || strings.TrimSpace(*a.Name) == "" {
		return nil, fmt.Errorf("die Sammlung braucht einen Namen")
	}
	id := newID("sam")
	sort := "hand"
	if a.Sortierung != nil {
		if !sortierungGueltig(*a.Sortierung) {
			return nil, fmt.Errorf("unbekannte sortierung")
		}
		sort = *a.Sortierung
	}
	beschr := ""
	if a.Beschreibung != nil {
		beschr = *a.Beschreibung
	}
	regel, err := regelText(a.Regel)
	if err != nil {
		return nil, err
	}
	start := a.AufStart != nil && *a.AufStart
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO sammlung (id, name, beschreibung, sortierung, regel, auf_start, erstellt)
		 VALUES (?,?,?,?,?,?,?)`, id, strings.TrimSpace(*a.Name), beschr, sort, regel, b2i(start),
		time.Now().Unix()); err != nil {
		return nil, err
	}
	return s.Sammlung(ctx, id)
}

func (s *Store) SammlungAendern(ctx context.Context, id string, a SammlungAenderung) error {
	return s.tx(ctx, func(t *sql.Tx) error {
		var da int
		if err := t.QueryRowContext(ctx, `SELECT 1 FROM sammlung WHERE id = ?`, id).Scan(&da); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		set := func(spalte string, v any) error {
			_, err := t.ExecContext(ctx, `UPDATE sammlung SET `+spalte+` = ? WHERE id = ?`, v, id)
			return err
		}
		if a.Name != nil {
			if strings.TrimSpace(*a.Name) == "" {
				return fmt.Errorf("die Sammlung braucht einen Namen")
			}
			if err := set("name", strings.TrimSpace(*a.Name)); err != nil {
				return err
			}
		}
		if a.Beschreibung != nil {
			if err := set("beschreibung", *a.Beschreibung); err != nil {
				return err
			}
		}
		if a.Sortierung != nil {
			if !sortierungGueltig(*a.Sortierung) {
				return fmt.Errorf("unbekannte sortierung")
			}
			if err := set("sortierung", *a.Sortierung); err != nil {
				return err
			}
		}
		if a.Regel != nil {
			r, err := regelText(a.Regel)
			if err != nil {
				return err
			}
			if err := set("regel", r); err != nil {
				return err
			}
		}
		if a.AufStart != nil {
			if err := set("auf_start", b2i(*a.AufStart)); err != nil {
				return err
			}
		}
		return nil
	})
}

// SammlungLoeschen gibt den Namen des eigenen Bildes zurück, damit die
// Datei mit weg kann.
func (s *Store) SammlungLoeschen(ctx context.Context, id string) (bild string, err error) {
	var b sql.NullString
	if err := s.db.QueryRowContext(ctx, `SELECT bild FROM sammlung WHERE id = ?`, id).Scan(&b); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", err
	}
	_, err = s.db.ExecContext(ctx, `DELETE FROM sammlung WHERE id = ?`, id)
	return b.String, err
}

// SammlungTitelHinzu hängt einen Titel hinten an. Steht er schon drin,
// bleibt er an seiner Stelle.
func (s *Store) SammlungTitelHinzu(ctx context.Context, id, itemID string) error {
	return s.tx(ctx, func(t *sql.Tx) error {
		var da int
		if err := t.QueryRowContext(ctx, `SELECT 1 FROM sammlung WHERE id = ?`, id).Scan(&da); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		var naechste int
		_ = t.QueryRowContext(ctx,
			`SELECT COALESCE(max(ord), -1) + 1 FROM sammlung_titel WHERE sammlung_id = ?`, id).Scan(&naechste)
		_, err := t.ExecContext(ctx,
			`INSERT OR IGNORE INTO sammlung_titel (sammlung_id, item_id, ord) VALUES (?,?,?)`, id, itemID, naechste)
		return err
	})
}

func (s *Store) SammlungTitelWeg(ctx context.Context, id, itemID string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM sammlung_titel WHERE sammlung_id = ? AND item_id = ?`, id, itemID)
	return err
}

// SammlungOrdnen setzt die Reihenfolge. Kennungen, die nicht in der
// Sammlung stehen, fallen still weg; was fehlt, rückt hinten an.
func (s *Store) SammlungOrdnen(ctx context.Context, id string, ids []string) error {
	return s.tx(ctx, func(t *sql.Tx) error {
		rows, err := t.QueryContext(ctx,
			`SELECT item_id FROM sammlung_titel WHERE sammlung_id = ? ORDER BY ord, item_id`, id)
		if err != nil {
			return err
		}
		jetzt, err := collectStrings(rows)
		rows.Close()
		if err != nil {
			return err
		}
		drin := map[string]bool{}
		for _, x := range jetzt {
			drin[x] = true
		}
		neu := make([]string, 0, len(jetzt))
		gesetzt := map[string]bool{}
		for _, x := range ids {
			if drin[x] && !gesetzt[x] {
				neu = append(neu, x)
				gesetzt[x] = true
			}
		}
		for _, x := range jetzt {
			if !gesetzt[x] {
				neu = append(neu, x)
			}
		}
		for i, x := range neu {
			if _, err := t.ExecContext(ctx,
				`UPDATE sammlung_titel SET ord = ? WHERE sammlung_id = ? AND item_id = ?`, i, id, x); err != nil {
				return err
			}
		}
		return nil
	})
}

// SammlungBild setzt das eigene Bild (leer = wieder aus Postern) und
// nennt das vorige.
func (s *Store) SammlungBild(ctx context.Context, id, datei string) (vorher string, err error) {
	var b sql.NullString
	if err := s.db.QueryRowContext(ctx, `SELECT bild FROM sammlung WHERE id = ?`, id).Scan(&b); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE sammlung SET bild = ? WHERE id = ?`, nullStr(datei), id)
	return b.String, err
}

// SammlungenMitTitel: in welchen Sammlungen ein Titel von Hand steht —
// für „Zu Sammlung hinzufügen".
func (s *Store) SammlungenMitTitel(ctx context.Context, itemID string) (map[string]bool, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT sammlung_id FROM sammlung_titel WHERE item_id = ?`, itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids, err := collectStrings(rows)
	out := map[string]bool{}
	for _, x := range ids {
		out[x] = true
	}
	return out, err
}
