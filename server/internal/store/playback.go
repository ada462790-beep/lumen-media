// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

// Alles in dieser Datei gehört genau einem Profil.
//
// Die Regel, die hier gilt und die privacy_test.go überprüft:
//
//   Jede Funktion nimmt accountID als ersten Parameter, und jede Abfrage
//   nennt account_id in der WHERE-Klausel. Es gibt keine Funktion, die
//   mehrere Konten zusammenfasst, zählt oder vergleicht — auch nicht für
//   den Besitzer. Wer wann wie lange was geschaut hat, verlässt dieses
//   Profil nicht.
//
// Darum steht hier auch nirgends ein Zeitstempel "angeschaut am". Gespeichert
// wird position_ms, damit man weiterschauen kann, und finished, damit der
// Haken auf dem Poster stimmt. reihenfolge ist keine Uhrzeit, sondern eine
// laufende Nummer je Profil, allein um "Weiterschauen" zu sortieren.

import (
	"context"
	"database/sql"
	"errors"
	"sort"
)

type Progress struct {
	ItemID     string `json:"itemId"`
	PositionMS int64  `json:"positionMs"`
	DurationMS int64  `json:"durationMs"`
	Finished   bool   `json:"finished"`
}

// Percent ist der Fortschrittsbalken auf dem Poster.
func (p Progress) Percent() float64 {
	if p.DurationMS <= 0 {
		return 0
	}
	v := float64(p.PositionMS) / float64(p.DurationMS) * 100
	if v > 100 {
		return 100
	}
	return v
}

// finishThreshold: Wer die letzten Minuten überspringt, hat den Film trotzdem
// gesehen. Abspann und Vorschau machen bei Serien gern fünf Prozent aus.
const finishThreshold = 0.92

// SaveProgress merkt sich, wo jemand stehen geblieben ist.
func (s *Store) SaveProgress(ctx context.Context, accountID, itemID string, positionMS, durationMS int64) error {
	finished := durationMS > 0 && float64(positionMS)/float64(durationMS) >= finishThreshold
	// Wer fertig ist, steht wieder am Anfang: Sonst böte die Titelseite
	// „Weiter bei 1:28:00" an, mitten im Abspann eines gesehenen Films.
	// Eine Stelle > 0 bei gesetztem Haken heisst damit: „schaut nochmal".
	if finished {
		positionMS = 0
	}
	return s.tx(ctx, func(t *sql.Tx) error {
		reihe, err := naechsteReihe(ctx, t, accountID, itemID)
		if err != nil {
			return err
		}
		_, err = t.ExecContext(ctx,
			`INSERT INTO progress (account_id, item_id, position_ms, duration_ms, finished, reihenfolge)
			 VALUES (?,?,?,?,?,?)
			 ON CONFLICT (account_id, item_id) DO UPDATE SET
			   position_ms = excluded.position_ms,
			   duration_ms = excluded.duration_ms,
			   finished    = CASE WHEN excluded.finished = 1 THEN 1 ELSE progress.finished END,
			   reihenfolge = excluded.reihenfolge`,
			accountID, itemID, positionMS, durationMS, b2i(finished), reihe)
		return err
	})
}

// naechsteReihe gibt die Nummer, unter der ein Titel in „Weiterschauen"
// einsortiert wird. Sie zählt nur weiter, wenn das Profil zu einem anderen
// Titel wechselt. Wer eine Stunde lang denselben Film schaut, verändert
// sie nicht — so lässt sich aus ihr keine Sehdauer ablesen.
func naechsteReihe(ctx context.Context, t *sql.Tx, accountID, itemID string) (int64, error) {
	var reihe int64
	var letzter string
	err := t.QueryRowContext(ctx,
		`SELECT reihe, letzter FROM progress_reihe WHERE account_id = ?`, accountID).
		Scan(&reihe, &letzter)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	if letzter == itemID && reihe > 0 {
		return reihe, nil
	}
	reihe++
	_, err = t.ExecContext(ctx,
		`INSERT INTO progress_reihe (account_id, reihe, letzter) VALUES (?,?,?)
		 ON CONFLICT (account_id) DO UPDATE SET reihe = excluded.reihe, letzter = excluded.letzter`,
		accountID, reihe, itemID)
	return reihe, err
}

// MarkFinished setzt den Haken von Hand — und ClearFinished nimmt ihn zurück,
// samt Fortschritt. Beides nur im eigenen Profil.
func (s *Store) MarkFinished(ctx context.Context, accountID, itemID string, durationMS int64) error {
	return s.tx(ctx, func(t *sql.Tx) error {
		reihe, err := naechsteReihe(ctx, t, accountID, itemID)
		if err != nil {
			return err
		}
		_, err = t.ExecContext(ctx,
			`INSERT INTO progress (account_id, item_id, position_ms, duration_ms, finished, reihenfolge)
			 VALUES (?,?,0,?,1,?)
			 ON CONFLICT (account_id, item_id) DO UPDATE SET
			   finished = 1, position_ms = 0, reihenfolge = excluded.reihenfolge`,
			accountID, itemID, durationMS, reihe)
		return err
	})
}

// MarkFinishedMehrere setzt den Haken für viele Titel auf einmal — eine
// ganze Serie, eine Staffel, „alle bis hier". Alle bekommen dieselbe
// Nummer in der Reihenfolge, die des letzten Titels: So rückt die Serie
// in „Weiterschauen" einmal nach vorn, und als zuletzt gesehen gilt die
// späteste Folge (danach kommt die nächste ungesehene).
func (s *Store) MarkFinishedMehrere(ctx context.Context, accountID string, itemIDs []string, dauerMS map[string]int64) error {
	if len(itemIDs) == 0 {
		return nil
	}
	return s.tx(ctx, func(t *sql.Tx) error {
		reihe, err := naechsteReihe(ctx, t, accountID, itemIDs[len(itemIDs)-1])
		if err != nil {
			return err
		}
		for _, id := range itemIDs {
			if _, err := t.ExecContext(ctx,
				`INSERT INTO progress (account_id, item_id, position_ms, duration_ms, finished, reihenfolge)
				 VALUES (?,?,0,?,1,?)
				 ON CONFLICT (account_id, item_id) DO UPDATE SET
				   finished = 1, position_ms = 0, reihenfolge = excluded.reihenfolge`,
				accountID, id, dauerMS[id], reihe); err != nil {
				return err
			}
		}
		return nil
	})
}

// ClearProgressMehrere nimmt Haken und Stand für viele Titel zurück.
func (s *Store) ClearProgressMehrere(ctx context.Context, accountID string, itemIDs []string) error {
	for i := 0; i < len(itemIDs); i += 400 {
		teil := itemIDs[i:min(i+400, len(itemIDs))]
		if _, err := s.db.ExecContext(ctx,
			`DELETE FROM progress WHERE account_id = ? AND item_id IN (`+placeholders(len(teil))+`)`,
			append([]any{accountID}, toAny(teil)...)...); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ClearProgress(ctx context.Context, accountID, itemID string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM progress WHERE account_id = ? AND item_id = ?`, accountID, itemID)
	return err
}

// ClearAllProgress ist der Knopf „Verlauf löschen" in den Einstellungen:
// Fortschritte und Haken, die Reihenfolge von „Weiterschauen", die je
// Serie gemerkten Spuren (verraten, welche Serien man schaut) und wann die
// Neuigkeiten zuletzt offen waren. Merkliste, Favoriten und Bewertungen
// sind kein Verlauf, sondern eigene Entscheidungen — sie bleiben.
func (s *Store) ClearAllProgress(ctx context.Context, accountID string) error {
	return s.tx(ctx, func(t *sql.Tx) error {
		for _, q := range []string{
			`DELETE FROM progress WHERE account_id = ?`,
			`DELETE FROM progress_reihe WHERE account_id = ?`,
			`DELETE FROM prefs WHERE account_id = ? AND (key LIKE 'spuren:%' OR key = 'newsSeen')`,
		} {
			if _, err := t.ExecContext(ctx, q, accountID); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Store) Progress(ctx context.Context, accountID, itemID string) (*Progress, error) {
	var p Progress
	var fin int
	err := s.db.QueryRowContext(ctx,
		`SELECT item_id, position_ms, duration_ms, finished
		   FROM progress WHERE account_id = ? AND item_id = ?`, accountID, itemID).
		Scan(&p.ItemID, &p.PositionMS, &p.DurationMS, &fin)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	p.Finished = fin == 1
	return &p, nil
}

// ProgressFor holt den Stand für eine Liste von Titeln auf einmal, damit die
// Startseite nicht hundert Einzelabfragen macht.
func (s *Store) ProgressFor(ctx context.Context, accountID string, itemIDs []string) (map[string]Progress, error) {
	out := map[string]Progress{}
	if len(itemIDs) == 0 {
		return out, nil
	}
	q := `SELECT item_id, position_ms, duration_ms, finished
	        FROM progress WHERE account_id = ? AND item_id IN (` + placeholders(len(itemIDs)) + `)`
	args := append([]any{accountID}, toAny(itemIDs)...)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var p Progress
		var fin int
		if err := rows.Scan(&p.ItemID, &p.PositionMS, &p.DurationMS, &fin); err != nil {
			return nil, err
		}
		p.Finished = fin == 1
		out[p.ItemID] = p
	}
	return out, rows.Err()
}

// Weiter ist ein Eintrag in „Weiterschauen". Naechste heisst: Das ist
// nicht angefangen, sondern die Folge nach der zuletzt gesehenen.
type Weiter struct {
	ItemID   string
	Naechste bool
}

// Weiterschauen ist die Reihe „Weiterschauen", wie man sie von Netflix
// kennt: Angefangene Filme, und je Serie genau ein Eintrag — die Folge,
// an der man zuletzt war, oder, wenn die fertig ist, die nächste
// ungesehene danach (auch über das Ende einer Staffel hinweg). Sortiert
// nach der letzten Aktivität im eigenen Profil.
//
// Gelesen wird nur der eigene Stand; der Aufbau der Serien kommt aus der
// Mediathek, zusammengelegt wird in Go.
func (s *Store) Weiterschauen(ctx context.Context, accountID string, limit int) ([]Weiter, error) {
	type stand struct {
		id, kind, serie string
		pos             int64
		fertig          bool
		r               int64
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT p.item_id, p.position_ms, p.finished, p.reihenfolge, i.kind, COALESCE(st.parent_id,'')
		   FROM progress p JOIN item i ON i.id = p.item_id
		   LEFT JOIN item st ON st.id = i.parent_id AND i.kind = 'episode'
		  WHERE p.account_id = ?
		  ORDER BY p.reihenfolge DESC`, accountID)
	if err != nil {
		return nil, err
	}
	var alle []stand
	jeItem := map[string]stand{}
	serienSet := map[string]bool{}
	for rows.Next() {
		var x stand
		var fin int
		if err := rows.Scan(&x.id, &x.pos, &fin, &x.r, &x.kind, &x.serie); err != nil {
			rows.Close()
			return nil, err
		}
		x.fertig = fin == 1
		alle = append(alle, x)
		jeItem[x.id] = x
		if x.kind == "episode" && x.serie != "" {
			serienSet[x.serie] = true
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	serien := make([]string, 0, len(serienSet))
	for id := range serienSet {
		serien = append(serien, id)
	}
	folgen, err := s.SerienFolgen(ctx, serien)
	if err != nil {
		return nil, err
	}
	stelle := map[string]int{} // Folge → Platz in ihrer Serie
	for _, fs := range folgen {
		for i, f := range fs {
			stelle[f.ID] = i
		}
	}

	// Je Serie die zuletzt berührte Folge. Haben mehrere dieselbe Nummer
	// (eine ganze Staffel auf einmal als gesehen markiert), gilt die
	// späteste davon.
	type letzte struct {
		x     stand
		platz int
	}
	jeSerie := map[string]*letzte{}
	for _, x := range alle {
		if x.kind != "episode" || x.serie == "" {
			continue
		}
		platz, ok := stelle[x.id]
		if !ok {
			continue // ein Special: zählt nicht für „Als Nächstes"
		}
		l := jeSerie[x.serie]
		if l == nil || x.r > l.x.r || (x.r == l.x.r && platz > l.platz) {
			jeSerie[x.serie] = &letzte{x, platz}
		}
	}

	out := []Weiter{}
	gesehen := map[string]bool{}
	for _, x := range alle {
		if len(out) >= limit {
			break
		}
		switch {
		case x.kind == "movie" || (x.kind == "episode" && x.serie == ""):
			// Ein Film: angefangen (auch beim zweiten Mal), mehr als eine Minute.
			if x.pos > 60000 {
				out = append(out, Weiter{ItemID: x.id})
			}
		case x.kind == "episode":
			l := jeSerie[x.serie]
			if l == nil || gesehen[x.serie] {
				continue
			}
			gesehen[x.serie] = true
			e := l.x
			if !e.fertig || e.pos > 0 {
				// Die Folge läuft noch (oder wieder) — dort geht es weiter.
				// Auch unter einer Minute: Wer die nächste Folge kurz
				// anschaut und schliesst, soll die Serie nicht verlieren.
				out = append(out, Weiter{ItemID: e.id})
				continue
			}
			fs := folgen[x.serie]
			for _, f := range fs[l.platz+1:] {
				if st, ok := jeItem[f.ID]; ok && st.fertig {
					continue
				}
				out = append(out, Weiter{ItemID: f.ID, Naechste: true})
				break
			}
		}
	}
	return out, nil
}

// WeiterInSerie: welche Folge „Weiterschauen" auf der Seite einer Serie
// startet. Dieselbe Antwort wie in der Reihe auf der Startseite — die
// Folge, an der man ist, oder die nach der zuletzt gesehenen. Ist dort
// nichts (nie angefangen, oder bis zur letzten Folge geschaut), die erste
// ungesehene von vorn, und wenn alles gesehen ist, die erste überhaupt.
func (s *Store) WeiterInSerie(ctx context.Context, accountID, serieID string) (string, error) {
	return s.WeiterInSerieSichtbar(ctx, accountID, serieID, nil)
}

// WeiterInSerieSichtbar wie WeiterInSerie, aber nur unter den Folgen, für
// die sichtbar wahr ist (nil: alle). Fiele die Wahl auf eine verborgene
// Folge, gilt die nächste sichtbare danach.
func (s *Store) WeiterInSerieSichtbar(ctx context.Context, accountID, serieID string, sichtbar func(string) bool) (string, error) {
	folgen, err := s.SerienFolgen(ctx, []string{serieID})
	if err != nil {
		return "", err
	}
	fs := folgen[serieID]
	pos := map[string]int{}
	alle := make([]string, len(fs))
	for i, f := range fs {
		pos[f.ID] = i
		alle[i] = f.ID
	}
	var ids []string
	for _, id := range alle {
		if sichtbar == nil || sichtbar(id) {
			ids = append(ids, id)
		}
	}
	weiter, err := s.Weiterschauen(ctx, accountID, 1<<20)
	if err != nil {
		return "", err
	}
	for _, w := range weiter {
		i, da := pos[w.ItemID]
		if !da {
			continue
		}
		for ; i < len(alle); i++ {
			if sichtbar == nil || sichtbar(alle[i]) {
				return alle[i], nil
			}
		}
	}
	fertig, err := s.FinishedIDs(ctx, accountID, ids)
	if err != nil {
		return "", err
	}
	for _, id := range ids {
		if !fertig[id] {
			return id, nil
		}
	}
	if len(ids) > 0 {
		return ids[0], nil
	}
	return "", ErrNotFound
}

// ResumeIDs ist die Reihe „Weiterschauen" — angefangen, nicht zu Ende.
func (s *Store) ResumeIDs(ctx context.Context, accountID string, limit int) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT item_id FROM progress
		  WHERE account_id = ? AND finished = 0 AND position_ms > 60000
		  ORDER BY reihenfolge DESC LIMIT ?`, accountID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectStrings(rows)
}

// FinishedIDs liefert die Haken auf den Postern.
func (s *Store) FinishedIDs(ctx context.Context, accountID string, itemIDs []string) (map[string]bool, error) {
	out := map[string]bool{}
	if len(itemIDs) == 0 {
		return out, nil
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT item_id FROM progress
		  WHERE account_id = ? AND finished = 1 AND item_id IN (`+placeholders(len(itemIDs))+`)`,
		append([]any{accountID}, toAny(itemIDs)...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------- Bewertungen

type Rating struct {
	ItemID string `json:"itemId"`
	Stars  int    `json:"stars"`
	Shared bool   `json:"shared"`
}

func (s *Store) SetRating(ctx context.Context, accountID, itemID string, stars int, shared bool) error {
	if stars < 1 || stars > 5 {
		return s.ClearRating(ctx, accountID, itemID)
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO rating (account_id, item_id, stars, shared, updated_at)
		 VALUES (?,?,?,?,?)
		 ON CONFLICT (account_id, item_id) DO UPDATE SET
		   stars = excluded.stars, shared = excluded.shared, updated_at = excluded.updated_at`,
		accountID, itemID, stars, b2i(shared), heute())
	return err
}

func (s *Store) ClearRating(ctx context.Context, accountID, itemID string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM rating WHERE account_id = ? AND item_id = ?`, accountID, itemID)
	return err
}

func (s *Store) RatingsFor(ctx context.Context, accountID string, itemIDs []string) (map[string]int, error) {
	out := map[string]int{}
	if len(itemIDs) == 0 {
		return out, nil
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT item_id, stars FROM rating
		  WHERE account_id = ? AND item_id IN (`+placeholders(len(itemIDs))+`)`,
		append([]any{accountID}, toAny(itemIDs)...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var id string
		var st int
		if err := rows.Scan(&id, &st); err != nil {
			return nil, err
		}
		out[id] = st
	}
	return out, rows.Err()
}

// SharedRatings sind die Bewertungen, die jemand ausdrücklich freigegeben hat.
// Ohne das Häkchen taucht hier nichts auf — auch nicht für den Besitzer.
// ich: wer fragt. Ein Gast (haushalt.go) sieht keine, und die eines Gasts
// sieht niemand.
func (s *Store) SharedRatings(ctx context.Context, ich, itemID string) ([]struct {
	Name   string `json:"name"`
	Avatar string `json:"avatar"`
	Stars  int    `json:"stars"`
}, error) {
	type row = struct {
		Name   string `json:"name"`
		Avatar string `json:"avatar"`
		Stars  int    `json:"stars"`
	}
	if s.IstGast(ctx, ich) {
		return []row{}, nil
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT a.name, a.avatar, r.stars
		   FROM rating r JOIN account a ON a.id = r.account_id
		  WHERE r.item_id = ? AND r.shared = 1 AND NOT `+gastSQL("r.account_id")+`
		  ORDER BY a.name`, itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []row{}
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.Name, &r.Avatar, &r.Stars); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------- Merkliste

func (s *Store) AddToWatchlist(ctx context.Context, accountID, itemID string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO watchlist (account_id, item_id, added_at) VALUES (?,?,?)`,
		accountID, itemID, heute()) // nur der Tag; die Reihenfolge hält rowid
	return err
}

func (s *Store) RemoveFromWatchlist(ctx context.Context, accountID, itemID string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM watchlist WHERE account_id = ? AND item_id = ?`, accountID, itemID)
	return err
}

func (s *Store) WatchlistIDs(ctx context.Context, accountID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT item_id FROM watchlist WHERE account_id = ? ORDER BY added_at DESC, rowid DESC`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectStrings(rows)
}

// ---------------------------------------------------------------- Einstellungen je Profil

func (s *Store) Prefs(ctx context.Context, accountID string) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT key, value FROM prefs WHERE account_id = ?`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

func (s *Store) SetPref(ctx context.Context, accountID, key, value string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO prefs (account_id, key, value) VALUES (?,?,?)
		 ON CONFLICT (account_id, key) DO UPDATE SET value = excluded.value`,
		accountID, key, value)
	return err
}

// SerienStand zählt je Serie, wie viele Folgen dieses Konto gesehen hat
// und wie viele es gibt — für den Balken an der Serienkarte. Specials
// zählen nicht mit, genau wie in GesehenIDs: Sonst hiesse es „alle
// gesehen" an der einen Stelle und „12 von 13" an der anderen. Nur für das
// eigene Konto; wie weit andere sind, verrät nichts.
type SerienStand struct {
	Gesehen int `json:"gesehen"`
	Folgen  int `json:"folgen"`
}

func (s *Store) SerienStaende(ctx context.Context, accountID string, seriesIDs []string) (map[string]SerienStand, error) {
	out := map[string]SerienStand{}
	if len(seriesIDs) == 0 {
		return out, nil
	}
	// Zwei Schritte, bewusst: Gezählt wird nur über die Titel (öffentlich),
	// und aus der privaten Tabelle kommen bloss die eigenen Haken — keine
	// Zusammenfassung über progress, das verbietet der Datenschutz-Test.
	zuSerie := map[string]string{} // Folge → Serie
	rows, err := s.db.QueryContext(ctx,
		`SELECT e.id, st.parent_id FROM item e JOIN item st ON st.id = e.parent_id
		  WHERE e.kind = 'episode' AND COALESCE(st.season_no,0) > 0
		    AND st.parent_id IN (`+placeholders(len(seriesIDs))+`)`,
		toAny(seriesIDs)...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var folge, serie string
		if err := rows.Scan(&folge, &serie); err != nil {
			rows.Close()
			return nil, err
		}
		zuSerie[folge] = serie
		st := out[serie]
		st.Folgen++
		out[serie] = st
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	folgen := make([]string, 0, len(zuSerie))
	for id := range zuSerie {
		folgen = append(folgen, id)
	}
	for i := 0; i < len(folgen); i += 400 {
		teil := folgen[i:min(i+400, len(folgen))]
		gesehen, err := s.FinishedIDs(ctx, accountID, teil)
		if err != nil {
			return nil, err
		}
		for id := range gesehen {
			st := out[zuSerie[id]]
			st.Gesehen++
			out[zuSerie[id]] = st
		}
	}
	return out, nil
}

// AngefangeneIDs: alles, wozu dieses Konto irgendeinen Stand hat —
// angefangen oder fertig. Für „Noch nicht angefangen".
//
// Bei Folgen kommt die Serie mit dazu: Wer Folge 3 geschaut hat, hat die
// Serie angefangen — auch wenn die Serie selbst keinen Stand trägt.
func (s *Store) AngefangeneIDs(ctx context.Context, accountID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT p.item_id, COALESCE(st.parent_id,'')
		   FROM progress p LEFT JOIN item e ON e.id = p.item_id AND e.kind = 'episode'
		   LEFT JOIN item st ON st.id = e.parent_id
		  WHERE p.account_id = ?`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	serien := map[string]bool{}
	for rows.Next() {
		var id, serie string
		if err := rows.Scan(&id, &serie); err != nil {
			return nil, err
		}
		out = append(out, id)
		if serie != "" && !serien[serie] {
			serien[serie] = true
			out = append(out, serie)
		}
	}
	return out, rows.Err()
}

// ZuletztGesehen: für jeden Titel, zu dem dieses Profil einen Stand hat,
// dessen laufende Nummer (grösser = später) — Serien über ihre zuletzt
// geschaute Folge. Für die Sortierung „Zuletzt gesehen". Keine Uhrzeit,
// nur die Reihenfolge, und nur die eigene; das Grösste je Serie wird in
// Go gesucht, nicht mit max() über die private Tabelle.
func (s *Store) ZuletztGesehen(ctx context.Context, accountID string) (map[string]int64, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT p.item_id, p.reihenfolge, COALESCE(st.parent_id,'')
		   FROM progress p LEFT JOIN item e ON e.id = p.item_id AND e.kind = 'episode'
		   LEFT JOIN item st ON st.id = e.parent_id
		  WHERE p.account_id = ?`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var id, serie string
		var r int64
		if err := rows.Scan(&id, &r, &serie); err != nil {
			return nil, err
		}
		if r > out[id] {
			out[id] = r
		}
		if serie != "" && r > out[serie] {
			out[serie] = r
		}
	}
	return out, rows.Err()
}

// FertigeIDs: was dieses Konto zu Ende gesehen hat, zuletzt zuerst —
// für „Nochmal ansehen".
func (s *Store) FertigeIDs(ctx context.Context, accountID string, limit int) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT item_id FROM progress WHERE account_id = ? AND finished = 1
		  ORDER BY reihenfolge DESC LIMIT ?`, accountID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------- Favoriten

// AddFavorit setzt das Herz. Nur im eigenen Profil.
func (s *Store) AddFavorit(ctx context.Context, accountID, itemID string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO favorit (account_id, item_id, added_at) VALUES (?,?,?)`,
		accountID, itemID, heute())
	return err
}

func (s *Store) RemoveFavorit(ctx context.Context, accountID, itemID string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM favorit WHERE account_id = ? AND item_id = ?`, accountID, itemID)
	return err
}

// FavoritIDs: die Favoriten eines Profils, zuletzt gesetzte zuerst.
func (s *Store) FavoritIDs(ctx context.Context, accountID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT item_id FROM favorit WHERE account_id = ? ORDER BY added_at DESC, rowid DESC`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectStrings(rows)
}

// GesehenIDs: was dieses Profil fertig geschaut hat — Filme und Serien,
// bei denen jede Folge (ohne Specials) gesehen ist. Zuletzt beendet
// zuerst. Gezählt wird in Go, nicht in SQL: Auf den privaten Tabellen
// gibt es bewusst keine zusammenfassenden Abfragen (privacy_test.go).
func (s *Store) GesehenIDs(ctx context.Context, accountID string) ([]string, error) {
	type eintrag struct {
		kind string
		r    int64
	}
	fertig := map[string]eintrag{}
	rows, err := s.db.QueryContext(ctx,
		`SELECT p.item_id, i.kind, p.reihenfolge FROM progress p JOIN item i ON i.id = p.item_id
		  WHERE p.account_id = ? AND p.finished = 1`, accountID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id string
		var e eintrag
		if err := rows.Scan(&id, &e.kind, &e.r); err != nil {
			rows.Close()
			return nil, err
		}
		fertig[id] = e
	}
	rows.Close()

	type treffer struct {
		id string
		r  int64
	}
	var out []treffer
	for id, e := range fertig {
		if e.kind == "movie" {
			out = append(out, treffer{id, e.r})
		}
	}
	// Serien: alle Folgen (Staffel > 0) müssen gesehen sein.
	folgen, err := s.db.QueryContext(ctx,
		`SELECT st.parent_id, e.id FROM item e JOIN item st ON st.id = e.parent_id
		  WHERE e.kind = 'episode' AND st.season_no > 0`)
	if err != nil {
		return nil, err
	}
	type stand struct {
		alle bool
		r    int64
	}
	serien := map[string]*stand{}
	for folgen.Next() {
		var serie, folge string
		if err := folgen.Scan(&serie, &folge); err != nil {
			folgen.Close()
			return nil, err
		}
		st := serien[serie]
		if st == nil {
			st = &stand{alle: true}
			serien[serie] = st
		}
		if e, ok := fertig[folge]; ok {
			if e.r > st.r {
				st.r = e.r
			}
		} else {
			st.alle = false
		}
	}
	folgen.Close()
	for id, st := range serien {
		if st.alle {
			out = append(out, treffer{id, st.r})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].r > out[j].r })
	ids := make([]string, len(out))
	for i, t := range out {
		ids[i] = t.id
	}
	return ids, nil
}
