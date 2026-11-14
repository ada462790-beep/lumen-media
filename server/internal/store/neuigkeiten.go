// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

// Neuigkeiten: „Deine Serie hat neue Folgen", „Ein neuer Film in deiner
// Reihe", „12 neue Titel". Was zählt, entscheidet sich allein aus dem
// eigenen Stand (angefangene Serien, fertige Filme, Merkliste) — nie aus
// dem der anderen. Darum nimmt jede Abfrage hier das Konto mit und
// zählt nichts über die private Tabelle hinweg.

import (
	"context"
	"strconv"
	"strings"
	"time"
)

// maxNeuTitel: so viele Titel trägt die Zeile „{n} neue Titel" mit.
const maxNeuTitel = 60

type Neuigkeit struct {
	Art    string    `json:"art"` // folgen | reihe | neu
	ItemID string    `json:"itemId,omitempty"`
	Titel  string    `json:"titel"`
	Text   string    `json:"text"`
	At     time.Time `json:"at"`
	Anzahl int       `json:"anzahl,omitempty"`
	// Titel: bei „{n} neue Titel" die Titel selbst (jüngste zuerst,
	// höchstens maxNeuTitel) — damit die Zeile aufklappen kann. Die API
	// reichert sie an (Poster) und gibt sie als „titelListe" aus.
	Liste []Item `json:"-"`
	// Vorlage: Titel und Text als Schlüssel für die Übersetzung (siehe
	// internal/sprache) samt Werten. Der Speicher kennt keine Sprache;
	// die API setzt daraus Titel und Text in der Sprache des Profils.
	// Leer heisst: Titel/Text sind Namen aus der Mediathek und bleiben.
	Vorlage Vorlage `json:"-"`
}

// Vorlage: siehe Neuigkeit.
type Vorlage struct {
	Titel, Text string
	Werte       []any // abwechselnd Name und Wert
}

// verfolgteSerien: Serien, in denen dieses Konto eine Folge angefangen
// hat, plus die auf der Merkliste — und je Serie, ab wann eine Folge als
// neu gilt: jünger als die jüngste Folge, die das Konto angefangen hat.
// Wann geschaut wurde, speichert Lumen nicht; es braucht es auch nicht.
func (s *Store) verfolgteSerien(ctx context.Context, accountID string) (map[string]time.Time, error) {
	out := map[string]time.Time{}
	rows, err := s.db.QueryContext(ctx,
		`SELECT st.parent_id, e.added_at FROM progress p
		   JOIN item e ON e.id = p.item_id JOIN item st ON st.id = e.parent_id
		  WHERE p.account_id = ? AND e.kind = 'episode'`, accountID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id string
		var at int64
		if err := rows.Scan(&id, &at); err != nil {
			rows.Close()
			return nil, err
		}
		if t := time.Unix(at, 0); t.After(out[id]) {
			out[id] = t
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows, err = s.db.QueryContext(ctx,
		`SELECT w.item_id, w.added_at FROM watchlist w JOIN item i ON i.id = w.item_id
		  WHERE w.account_id = ? AND i.kind = 'series'`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var at int64
		if err := rows.Scan(&id, &at); err != nil {
			return nil, err
		}
		if _, da := out[id]; !da {
			out[id] = time.Unix(at, 0)
		}
	}
	return out, rows.Err()
}

// Neuigkeiten stellt die Liste zusammen — nur innerhalb dessen, was das
// Konto sehen darf (q), und nur was jünger ist als 30 Tage.
func (s *Store) Neuigkeiten(ctx context.Context, q Query, accountID string) ([]Neuigkeit, error) {
	out := []Neuigkeit{}
	where, args, ok := bedingung(q)
	if !ok {
		return out, nil
	}
	grenze := time.Now().Add(-30 * 24 * time.Hour)

	// Neue Folgen zu verfolgten Serien.
	serien, err := s.verfolgteSerien(ctx, accountID)
	if err != nil {
		return nil, err
	}
	gemeldet := map[string]bool{} // Serien, die schon eine Zeile „neue Folgen" haben
	if len(serien) > 0 {
		ids := make([]string, 0, len(serien))
		for id := range serien {
			ids = append(ids, id)
		}
		// Sichtbarkeit: nur Serien, die die Freigabe erlaubt.
		sq := q
		sq.IDs, sq.Kinds, sq.Limit = ids, []string{"series"}, 500
		sichtbar, err := s.Query(ctx, sq)
		if err != nil {
			return nil, err
		}
		for _, serie := range sichtbar {
			seit := serien[serie.ID]
			if seit.Before(grenze) {
				seit = grenze
			}
			var n int
			var juengste int64
			rows, err := s.db.QueryContext(ctx,
				`SELECT e.added_at FROM item e JOIN item st ON st.id = e.parent_id
				  WHERE e.kind = 'episode' AND st.parent_id = ? AND e.added_at > ?`, serie.ID, seit.Unix())
			if err != nil {
				return nil, err
			}
			for rows.Next() {
				var at int64
				if err := rows.Scan(&at); err != nil {
					rows.Close()
					return nil, err
				}
				n++
				if at > juengste {
					juengste = at
				}
			}
			rows.Close()
			if n == 0 {
				continue
			}
			text := "1 neue Folge"
			if n > 1 {
				text = strconv.Itoa(n) + " neue Folgen"
			}
			gemeldet[serie.ID] = true
			out = append(out, Neuigkeit{Art: "folgen", ItemID: serie.ID, Titel: serie.Title, Text: text,
				At: time.Unix(juengste, 0), Anzahl: n,
				Vorlage: Vorlage{Text: "{n} neue Folgen", Werte: []any{"n", n}}})
		}
	}

	// Neue Filme in Reihen, aus denen dieses Konto schon etwas gesehen hat.
	fertig, err := s.FertigeIDs(ctx, accountID, 500)
	if err != nil {
		return nil, err
	}
	if len(fertig) > 0 {
		fertigSet := map[string]bool{}
		for _, id := range fertig {
			fertigSet[id] = true
		}
		rargs := append([]any{}, toAny(fertig)...)
		rargs = append(rargs, grenze.Unix())
		rargs = append(rargs, args...)
		rows, err := s.db.QueryContext(ctx,
			`SELECT DISTINCT c.id, c.name, i.id, i.title, i.added_at
			   FROM collection c
			   JOIN collection_item mine ON mine.collection_id = c.id AND mine.item_id IN (`+placeholders(len(fertig))+`)
			   JOIN collection_item neu ON neu.collection_id = c.id
			   JOIN item i ON i.id = neu.item_id AND i.added_at > ?
			  WHERE c.hidden = 0 AND `+strings.Join(where, " AND ")+`
			  ORDER BY i.added_at DESC LIMIT 50`, rargs...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var cid, cname, iid, ititle string
			var at int64
			if err := rows.Scan(&cid, &cname, &iid, &ititle, &at); err != nil {
				rows.Close()
				return nil, err
			}
			if fertigSet[iid] {
				continue
			}
			out = append(out, Neuigkeit{Art: "reihe", ItemID: iid, Titel: ititle,
				Text: "Neu in deiner Reihe „" + cname + "“", At: time.Unix(at, 0),
				Vorlage: Vorlage{Text: "Neu in deiner Reihe „{reihe}“", Werte: []any{"reihe", cname}}})
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}

	// Eigene Wünsche (und solche, denen sich das Profil angeschlossen hat),
	// die sich erfüllt haben — mit dem Titel, wenn das Profil ihn sehen
	// darf. Kennt der Wunsch seinen Titel (wunsch_kennung.go), gilt der;
	// sonst wird nach Titel und Jahr gesucht.
	erfuellte, err := s.erfuellteWuensche(ctx, accountID, grenze)
	if err != nil {
		return nil, err
	}
	for _, e := range erfuellte {
		wq := q
		if e.ItemID != "" {
			wq.IDs, wq.Limit = []string{e.ItemID}, 1
		} else {
			wq.Text, wq.Limit = e.Titel, 5
		}
		treffer, err := s.Query(ctx, wq)
		if err != nil {
			return nil, err
		}
		for _, it := range treffer {
			if e.ItemID != "" || strings.EqualFold(it.Title, strings.TrimSpace(e.Titel)) && (e.Jahr == 0 || it.Year == e.Jahr) {
				out = append(out, Neuigkeit{Art: "wunsch", ItemID: it.ID, Titel: it.Title,
					Text: "Dein Wunsch ist da", At: e.At,
					Vorlage: Vorlage{Text: "Dein Wunsch ist da"}})
				break
			}
		}
	}

	// Neu dazugekommen, als eine Zeile. Sortiert wird nach zuletzt_neu —
	// eine alte Serie mit neuen Folgen steht darum vorn, obwohl sie selbst
	// längst da ist. Darum nicht beim ersten alten Titel aufhören (so
	// fehlten vorher alle neuen Titel hinter einer solchen Serie), sondern
	// jeden ansehen.
	woche := time.Now().Add(-7 * 24 * time.Hour)
	nq := q
	nq.Sort, nq.Limit = "added", 500
	alle, err := s.Query(ctx, nq)
	if err != nil {
		return nil, err
	}
	var neueTitel []Item
	for _, it := range alle {
		if !it.AddedAt.Before(woche) {
			neueTitel = append(neueTitel, it)
		}
	}
	// Jüngste zuerst (die Abfrage sortiert nach zuletzt_neu, nicht nach
	// dem Hinzufügen des Titels selbst).
	for i := 1; i < len(neueTitel); i++ {
		for j := i; j > 0 && neueTitel[j].AddedAt.After(neueTitel[j-1].AddedAt); j-- {
			neueTitel[j], neueTitel[j-1] = neueTitel[j-1], neueTitel[j]
		}
	}
	if n := len(neueTitel); n == 1 {
		it := neueTitel[0]
		out = append(out, Neuigkeit{Art: "neu", ItemID: it.ID, Titel: it.Title, Text: "Neu dazugekommen", At: it.AddedAt, Anzahl: 1,
			Vorlage: Vorlage{Text: "Neu dazugekommen"}})
	} else if n > 1 {
		erster := neueTitel[0]
		liste := neueTitel
		if len(liste) > maxNeuTitel {
			liste = liste[:maxNeuTitel]
		}
		out = append(out, Neuigkeit{Art: "neu", Titel: strconv.Itoa(n) + " neue Titel", Text: "zuletzt: " + erster.Title, At: erster.AddedAt, Anzahl: n,
			Liste:   liste,
			Vorlage: Vorlage{Titel: "{n} neue Titel", Text: "zuletzt: {titel}", Werte: []any{"n", n, "titel", erster.Title}}})
	}

	// Neue Folgen von Serien, die schon länger da sind, auch wenn dieses
	// Konto sie (noch) nicht schaut: „Neue Folgen von X". Vorher kamen 27
	// neue Folgen an, und die Seite sagte nur „2 neue Titel".
	neuSet := map[string]bool{}
	for _, it := range neueTitel {
		neuSet[it.ID] = true
	}
	frq, err := s.db.QueryContext(ctx,
		`SELECT st.parent_id, count(*), max(e.added_at) FROM item e JOIN item st ON st.id = e.parent_id
		  WHERE e.kind = 'episode' AND e.added_at >= ? GROUP BY st.parent_id`, woche.Unix())
	if err != nil {
		return nil, err
	}
	type folgenNeu struct {
		n  int
		at int64
	}
	frisch := map[string]folgenNeu{}
	for frq.Next() {
		var id string
		var f folgenNeu
		if err := frq.Scan(&id, &f.n, &f.at); err != nil {
			frq.Close()
			return nil, err
		}
		if !gemeldet[id] && !neuSet[id] {
			frisch[id] = f
		}
	}
	frq.Close()
	if err := frq.Err(); err != nil {
		return nil, err
	}
	if len(frisch) > 0 {
		ids := make([]string, 0, len(frisch))
		for id := range frisch {
			ids = append(ids, id)
		}
		sq := q
		sq.IDs, sq.Kinds, sq.Limit = ids, []string{"series"}, len(ids)
		sichtbar, err := s.Query(ctx, sq)
		if err != nil {
			return nil, err
		}
		for _, serie := range sichtbar {
			f := frisch[serie.ID]
			text := "1 neue Folge"
			if f.n > 1 {
				text = strconv.Itoa(f.n) + " neue Folgen"
			}
			out = append(out, Neuigkeit{Art: "folgen", ItemID: serie.ID, Titel: "Neue Folgen von " + serie.Title, Text: text,
				At: time.Unix(f.at, 0), Anzahl: f.n,
				Vorlage: Vorlage{Titel: "Neue Folgen von {serie}", Text: "{n} neue Folgen", Werte: []any{"serie", serie.Title, "n", f.n}}})
		}
	}

	// Jüngste zuerst.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].At.After(out[j-1].At); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out, nil
}
