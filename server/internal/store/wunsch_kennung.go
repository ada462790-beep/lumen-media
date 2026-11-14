// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

// Wünsche mit Kennung: Wer beim Wünschen einen Vorschlag von TMDb wählt,
// gibt dem Wunsch Art (Film/Serie), TMDb- und IMDb-Kennung, Originaltitel
// und Poster mit. Damit
//
//   - entsteht derselbe Wunsch nicht zweimal, auch nicht von zwei
//     Personen — die zweite schliesst sich dem ersten an (wish_dabei),
//     ohne zu erfahren, wer ihn schon hatte;
//   - erfüllt er sich von selbst, sobald ein Titel mit derselben Kennung
//     in einer Bibliothek steht, und weiss, welcher Titel das ist.
//
// Wünsche ohne Kennung (nur der Name) gibt es weiter wie bisher.

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"strings"
	"time"
)

// wunschKennungNachruesten: Stand 15 (store.go). Neue Spalten an wish,
// die Tabelle wish_dabei — nichts umzurechnen.
func wunschKennungNachruesten(db *sql.DB) error {
	for _, stmt := range []string{
		`ALTER TABLE wish ADD COLUMN art TEXT`,           // movie | series
		`ALTER TABLE wish ADD COLUMN imdb_id TEXT`,       // tt…
		`ALTER TABLE wish ADD COLUMN originaltitel TEXT`, //
		`ALTER TABLE wish ADD COLUMN poster TEXT`,        // „/abc.jpg" bei TMDb
		`ALTER TABLE wish ADD COLUMN erfuellt_item TEXT`, // der Titel, der ihn erfüllt hat
	} {
		if _, err := db.Exec(stmt); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			return err
		}
	}
	for _, stmt := range []string{
		// Wer sich einem Wunsch angeschlossen hat, den jemand anders
		// schon hatte.
		`CREATE TABLE IF NOT EXISTS wish_dabei (
		   wish_id    TEXT NOT NULL REFERENCES wish(id) ON DELETE CASCADE,
		   account_id TEXT NOT NULL REFERENCES account(id) ON DELETE CASCADE,
		   PRIMARY KEY (wish_id, account_id))`,
		`CREATE INDEX IF NOT EXISTS wish_dabei_konto ON wish_dabei (account_id)`,
		`CREATE INDEX IF NOT EXISTS wish_tmdb ON wish (tmdb_id) WHERE tmdb_id IS NOT NULL`,
		`CREATE INDEX IF NOT EXISTS wish_imdb ON wish (imdb_id) WHERE imdb_id IS NOT NULL`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

// WunschDaten: was beim Wünschen mitkommt. Nur Titel ist Pflicht.
type WunschDaten struct {
	Titel         string
	Jahr          int
	Art           string // movie | series | ""
	TMDbID        string
	IMDbID        string
	Originaltitel string
	PosterPfad    string
}

func (d WunschDaten) mitKennung() bool { return d.TMDbID != "" || d.IMDbID != "" }

// WunschAnlegen legt einen Wunsch an — mit Kennung, wenn eine mitkommt.
// Gibt es schon einen offenen mit derselben Kennung, entsteht kein
// zweiter: Der eigene kommt zurück, der von jemand anderem bekommt dieses
// Profil dazu.
//
// Suchen und Anlegen laufen in einer Transaktion (_txlock=immediate):
// Wünschten zwei Profile denselben Titel im selben Augenblick, fanden
// vorher beide keinen und legten beide einen an — ein Doppel, das erst
// die Verwaltung wieder zusammenführen musste.
func (s *Store) WunschAnlegen(ctx context.Context, accountID string, d WunschDaten) (*Wish, error) {
	d.Titel = strings.TrimSpace(d.Titel)
	if d.Titel == "" {
		return nil, errors.New("titel fehlt")
	}
	var id string
	vorhanden := false
	err := s.tx(ctx, func(t *sql.Tx) error {
		// Derselbe Wunsch, offen, von wem auch immer (wunsch_doppelt.go):
		// über die Kennung, sonst über Titel und Jahr.
		gleich, owner, err := gleicherOffenerWunsch(ctx, t, accountID, d)
		if err != nil {
			return err
		}
		if gleich != "" {
			id, vorhanden = gleich, true
			if owner != accountID {
				if _, err := t.ExecContext(ctx,
					`INSERT OR IGNORE INTO wish_dabei (wish_id, account_id) VALUES (?, ?)`, id, accountID); err != nil {
					return err
				}
			}
			// Kommt jetzt eine Kennung mit, bekommt der alte Wunsch sie.
			return wunschKennungErgaenzen(ctx, t, id, d)
		}
		// Nur der Tag: Wann genau sich jemand etwas gewünscht hat, geht die
		// Verwaltung nichts an. Die Reihenfolge hält die Zeilennummer.
		tag := heute()
		id = newID("w")
		if _, err := t.ExecContext(ctx,
			`INSERT INTO wish (id, account_id, title, year, state, created_at, updated_at)
			 VALUES (?,?,?,?,'open',?,?)`,
			id, accountID, d.Titel, nullInt(d.Jahr), tag, tag); err != nil {
			return err
		}
		return wunschKennungErgaenzen(ctx, t, id, d)
	})
	if err != nil {
		return nil, err
	}
	w, err := s.wunschFuer(ctx, accountID, id)
	if err == nil && vorhanden {
		w.Vorhanden = true
	}
	return w, err
}

// wunschFuer: ein Wunsch so, wie ihn dieses Profil in seiner Liste sieht.
func (s *Store) wunschFuer(ctx context.Context, accountID, id string) (*Wish, error) {
	liste, err := s.Wishes(ctx, accountID, false)
	if err != nil {
		return nil, err
	}
	for i := range liste {
		if liste[i].ID == id {
			return &liste[i], nil
		}
	}
	return nil, ErrNotFound
}

// wunschErgaenzen füllt die Felder mit Kennung in die Liste von
// Wishes und nimmt — für die eigene Liste — die Wünsche dazu, denen sich
// das Profil angeschlossen hat. Bei denen steht nicht, von wem sie sind,
// und auch nicht die Notiz der Verwaltung — die galt der Person, die ihn
// sich zuerst gewünscht hat; nur der allgemeine Stand („Ist jetzt in der
// Bibliothek") bleibt.
func (s *Store) wunschErgaenzen(ctx context.Context, out []Wish, accountID string, all bool) ([]Wish, error) {
	if !all {
		rows, err := s.db.QueryContext(ctx,
			`SELECT w.id, w.title, COALESCE(w.year,0), w.state,
			        CASE WHEN w.note = ? THEN w.note ELSE '' END, w.created_at
			   FROM wish w JOIN wish_dabei d ON d.wish_id = w.id
			  WHERE d.account_id = ? AND w.account_id <> ?`, WunschNotizErfuellt, accountID, accountID)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var w Wish
			var created int64
			if err := rows.Scan(&w.ID, &w.Title, &w.Year, &w.State, &w.Note, &created); err != nil {
				rows.Close()
				return nil, err
			}
			w.CreatedAt, w.Dabei = time.Unix(created, 0), true
			out = append(out, w)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	if len(out) == 0 {
		return out, nil
	}
	index := make(map[string]int, len(out))
	ids := make([]string, 0, len(out))
	for i := range out {
		index[out[i].ID] = i
		ids = append(ids, out[i].ID)
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, COALESCE(art,''), COALESCE(tmdb_id,''), COALESCE(imdb_id,''), COALESCE(originaltitel,''),
		        COALESCE(poster,''), COALESCE(erfuellt_item,'')
		   FROM wish WHERE id IN (`+placeholders(len(ids))+`)`, toAny(ids)...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id string
		var z Wish
		if err := rows.Scan(&id, &z.Art, &z.TMDbID, &z.IMDbID, &z.Originaltitel, &z.PosterPfad, &z.ItemID); err != nil {
			rows.Close()
			return nil, err
		}
		w := &out[index[id]]
		w.Art, w.TMDbID, w.IMDbID, w.Originaltitel, w.PosterPfad, w.ItemID =
			z.Art, z.TMDbID, z.IMDbID, z.Originaltitel, z.PosterPfad, z.ItemID
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if all {
		// Der Besitzer sieht ohnehin, wer was wünscht — also auch, wer
		// sich angeschlossen hat.
		rows, err := s.db.QueryContext(ctx,
			`SELECT d.wish_id, a.id, a.name FROM wish_dabei d JOIN account a ON a.id = d.account_id
			  ORDER BY a.name`)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id, konto, name string
			if err := rows.Scan(&id, &konto, &name); err != nil {
				rows.Close()
				return nil, err
			}
			if i, da := index[id]; da {
				out[i].Auch = append(out[i].Auch, name)
				out[i].AuchIDs = append(out[i].AuchIDs, konto)
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	rang := map[string]int{"open": 0, "working": 1}
	sort.SliceStable(out, func(i, j int) bool {
		ri, ok := rang[out[i].State]
		if !ok {
			ri = 2
		}
		rj, ok := rang[out[j].State]
		if !ok {
			rj = 2
		}
		if ri != rj {
			return ri < rj
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out, nil
}

// WunschKennungen: die Kennungen aller offenen Wünsche — „movie:603",
// „series:1399", „imdb:tt0133093". Für „wurde schon gewünscht", ohne
// zu sagen, von wem.
func (s *Store) WunschKennungen(ctx context.Context) (map[string]bool, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT COALESCE(art,''), COALESCE(tmdb_id,''), COALESCE(imdb_id,'') FROM wish
		  WHERE state IN ('open','working') AND (tmdb_id IS NOT NULL OR imdb_id IS NOT NULL)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var art, tmdb, imdb string
		if err := rows.Scan(&art, &tmdb, &imdb); err != nil {
			return nil, err
		}
		if tmdb != "" {
			out[art+":"+tmdb] = true
		}
		if imdb != "" {
			out["imdb:"+imdb] = true
		}
	}
	return out, rows.Err()
}

// WunschPosterBekannt: gehört dieses Poster zu einem Wunsch? Nur solche
// werden auf der Platte aufbewahrt.
func (s *Store) WunschPosterBekannt(ctx context.Context, pfad string) bool {
	var n int
	_ = s.db.QueryRowContext(ctx, `SELECT count(*) FROM wish WHERE poster = ?`, pfad).Scan(&n)
	return n > 0
}

// TitelKandidat: ein Vorschlag, für den gefragt wird, ob es ihn schon gibt.
type TitelKandidat struct {
	Art           string // movie | series
	TMDbID        string
	IMDbID        string
	Titel         string
	Originaltitel string
	Jahr          int
}

// TitelDa sagt je Kandidat, welcher Titel es schon ist ("" = keiner) —
// nur unter dem, was q sehen lässt. Zuerst über die Kennung, sonst über
// Titel (oder Originaltitel) und Jahr bei gleicher Art.
func (s *Store) TitelDa(ctx context.Context, q Query, kand []TitelKandidat) ([]string, error) {
	out := make([]string, len(kand))
	if len(kand) == 0 {
		return out, nil
	}
	q.Kinds, q.IDs, q.NotIDs, q.Text, q.Genre, q.Person, q.Buchstabe = nil, nil, nil, "", "", "", ""
	where, args, ok := bedingung(q)
	if !ok {
		return out, nil
	}
	var tmdb, imdb, titel []string
	for _, k := range kand {
		if k.TMDbID != "" {
			tmdb = append(tmdb, k.TMDbID)
		}
		if k.IMDbID != "" {
			imdb = append(imdb, k.IMDbID)
		}
		for _, t := range []string{k.Titel, k.Originaltitel} {
			if t = titelKlein(t); t != "" {
				titel = append(titel, t)
			}
		}
	}
	var oder []string
	var oargs []any
	if len(tmdb) > 0 {
		oder = append(oder, `i.tmdb_id IN (`+placeholders(len(tmdb))+`)`)
		oargs = append(oargs, toAny(tmdb)...)
	}
	if len(imdb) > 0 {
		oder = append(oder, `i.imdb_id IN (`+placeholders(len(imdb))+`)`)
		oargs = append(oargs, toAny(imdb)...)
	}
	if len(titel) > 0 {
		oder = append(oder, `i.titel_klein IN (`+placeholders(len(titel))+`)`,
			`i.original_klein IN (`+placeholders(len(titel))+`)`)
		oargs = append(oargs, toAny(titel)...)
		oargs = append(oargs, toAny(titel)...)
	}
	if len(oder) == 0 {
		return out, nil
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT i.id, i.kind, COALESCE(i.tmdb_id,''), COALESCE(i.imdb_id,''), COALESCE(i.titel_klein,''),
		        COALESCE(i.original_klein,''), COALESCE(i.year,0)
		   FROM item i WHERE (`+strings.Join(oder, " OR ")+`) AND `+strings.Join(where, " AND ")+`
		  LIMIT 400`, append(oargs, args...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type zeile struct {
		id, kind, tmdb, imdb, titel, original string
		jahr                                  int
	}
	var alle []zeile
	for rows.Next() {
		var z zeile
		if err := rows.Scan(&z.id, &z.kind, &z.tmdb, &z.imdb, &z.titel, &z.original, &z.jahr); err != nil {
			return nil, err
		}
		alle = append(alle, z)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i, k := range kand {
		// Erst die Kennung …
		for _, z := range alle {
			if (k.TMDbID != "" && z.tmdb == k.TMDbID && (k.Art == "" || z.kind == k.Art)) ||
				(k.IMDbID != "" && z.imdb == k.IMDbID) {
				out[i] = z.id
				break
			}
		}
		if out[i] != "" || k.Jahr == 0 {
			continue
		}
		// … dann Titel und Jahr — aber nicht gegen einen Titel, der eine
		// andere TMDb-Kennung hat: „Dune" (1984) ist nicht „Dune" (2021),
		// auch wenn TMDb das Jahr anders zählt.
		t, o := titelKlein(k.Titel), titelKlein(k.Originaltitel)
		for _, z := range alle {
			if z.kind != k.Art || z.jahr != k.Jahr || (z.tmdb != "" && k.TMDbID != "" && z.tmdb != k.TMDbID) {
				continue
			}
			if (t != "" && (z.titel == t || z.original == t)) || (o != "" && (z.titel == o || z.original == o)) {
				out[i] = z.id
				break
			}
		}
	}
	return out, nil
}

// wuenscheNachKennungErfuellen schliesst offene Wünsche, zu deren Kennung
// jetzt ein Titel in einer Bibliothek steht, und merkt sich den Titel.
func (s *Store) wuenscheNachKennungErfuellen(ctx context.Context, sieht WunschSicht) (int, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT w.id, w.account_id, i.id FROM wish w
		   JOIN item i ON i.kind IN ('movie','series')
		    AND ((COALESCE(w.tmdb_id,'') <> '' AND i.tmdb_id = w.tmdb_id AND (COALESCE(w.art,'') = '' OR i.kind = w.art))
		      OR (COALESCE(w.imdb_id,'') <> '' AND i.imdb_id = w.imdb_id))
		  WHERE w.state IN ('open','working')
		  ORDER BY w.id, i.id`)
	if err != nil {
		return 0, err
	}
	kand, err := wunschKandidaten(rows)
	if err != nil {
		return 0, err
	}
	n := 0
	jetzt := heute() // nur der Tag (Datenschutz)
	for _, k := range kand {
		// Nur mit einem Titel, den die Person sehen darf (WuenscheErfuellenFuer).
		it := k.waehle(ctx, sieht)
		if it == "" {
			continue
		}
		res, err := s.db.ExecContext(ctx,
			`UPDATE wish SET state = 'done', note = 'Ist jetzt in der Bibliothek', erfuellt_item = ?, updated_at = ?
			  WHERE id = ? AND state IN ('open','working')`, it, jetzt, k.wunsch)
		if err != nil {
			return n, err
		}
		if x, _ := res.RowsAffected(); x > 0 {
			n++
		}
	}
	return n, nil
}

// WuenscheVerknuepfen sucht zu erfüllten Wünschen ohne Titel den Titel,
// der sie erfüllt — über die Kennung, sonst über Titel und Jahr. Dann
// kann die Wunschliste „Ansehen" anbieten, auch wenn der Besitzer den
// Wunsch von Hand auf „erfüllt" gestellt hat.
func (s *Store) WuenscheVerknuepfen(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, title, COALESCE(year,0), COALESCE(art,''), COALESCE(tmdb_id,''), COALESCE(imdb_id,'')
		   FROM wish WHERE state = 'done' AND erfuellt_item IS NULL`)
	if err != nil {
		return err
	}
	type offen struct {
		id, titel       string
		jahr            int
		art, tmdb, imdb string
	}
	var liste []offen
	for rows.Next() {
		var o offen
		if err := rows.Scan(&o.id, &o.titel, &o.jahr, &o.art, &o.tmdb, &o.imdb); err != nil {
			rows.Close()
			return err
		}
		liste = append(liste, o)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, o := range liste {
		var it string
		err := s.db.QueryRowContext(ctx,
			`SELECT id FROM item WHERE kind IN ('movie','series')
			    AND ((? <> '' AND tmdb_id = ? AND (? = '' OR kind = ?)) OR (? <> '' AND imdb_id = ?))
			  ORDER BY id LIMIT 1`, o.tmdb, o.tmdb, o.art, o.art, o.imdb, o.imdb).Scan(&it)
		if errors.Is(err, sql.ErrNoRows) {
			err = s.db.QueryRowContext(ctx,
				`SELECT id FROM item WHERE kind IN ('movie','series') AND (? = '' OR kind = ?)
				    AND (titel_klein = ? OR original_klein = ?) AND (? = 0 OR year = ?)
				  ORDER BY id LIMIT 1`, o.art, o.art, titelKlein(o.titel), titelKlein(o.titel), o.jahr, o.jahr).Scan(&it)
		}
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE wish SET erfuellt_item = ? WHERE id = ?`, it, o.id); err != nil {
			return err
		}
	}
	return nil
}

// ErfuellteWuensche: die eigenen und mitgewünschten erfüllten Wünsche
// seit grenze — für die Neuigkeiten.
type ErfuellterWunsch struct {
	Titel  string
	Jahr   int
	ItemID string
	At     time.Time
}

func (s *Store) erfuellteWuensche(ctx context.Context, accountID string, grenze time.Time) ([]ErfuellterWunsch, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT title, COALESCE(year, 0), COALESCE(erfuellt_item, ''), updated_at FROM wish
		  WHERE state = 'done' AND updated_at > ?
		    AND (account_id = ? OR id IN (SELECT wish_id FROM wish_dabei WHERE account_id = ?))`,
		grenze.Unix(), accountID, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ErfuellterWunsch
	for rows.Next() {
		var e ErfuellterWunsch
		var at int64
		if err := rows.Scan(&e.Titel, &e.Jahr, &e.ItemID, &at); err != nil {
			return nil, err
		}
		e.At = time.Unix(at, 0)
		out = append(out, e)
	}
	return out, rows.Err()
}
