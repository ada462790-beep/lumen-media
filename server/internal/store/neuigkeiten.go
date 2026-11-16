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
	"sort"
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
	// Die angefangenen Folgen aus dem eigenen Stand im Speicher
	// (fortschritt_abbild.go), ihre Serie und ihr Hinzufügen aus baum.
	a, b, err := s.abbildMitBaum(ctx, accountID)
	if err != nil {
		return nil, err
	}
	for _, z := range a.zeilen {
		e := b[z.id]
		if e.kind != "episode" || e.serie == "" {
			continue
		}
		if t := time.Unix(e.neu, 0); t.After(out[e.serie]) {
			out[e.serie] = t
		}
	}
	// Die Merkliste; welche davon Serien sind, sagt baum.
	rows, err := s.db.QueryContext(ctx,
		`SELECT item_id, added_at FROM watchlist WHERE account_id = ?`, accountID)
	if err != nil {
		return nil, err
	}
	merk := map[string]int64{}
	var ids []string
	for rows.Next() {
		var id string
		var at int64
		if err := rows.Scan(&id, &at); err != nil {
			rows.Close()
			return nil, err
		}
		merk[id] = at
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	mb, err := s.baum(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		if mb[id].kind != "series" {
			continue
		}
		if _, da := out[id]; !da {
			out[id] = time.Unix(merk[id], 0)
		}
	}
	return out, nil
}

// Neuigkeiten stellt die Liste zusammen — nur innerhalb dessen, was das
// Konto sehen darf (q), und nur was jünger ist als 30 Tage.
func (s *Store) Neuigkeiten(ctx context.Context, q Query, accountID string) ([]Neuigkeit, error) {
	out := []Neuigkeit{}
	_, _, ok := bedingung(q)
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
		// Die neuen Folgen aller dieser Serien in einer Abfrage (ab der
		// Grenze); je Serie zählt dann nur, was jünger ist als ihr „seit".
		sids := make([]string, len(sichtbar))
		for i, serie := range sichtbar {
			sids[i] = serie.ID
		}
		frische, err := s.neueFolgen(ctx, sids, grenze)
		if err != nil {
			return nil, err
		}
		jeSerie := map[string][]int64{}
		for _, f := range frische {
			jeSerie[f.serie] = append(jeSerie[f.serie], f.neu)
		}
		for _, serie := range sichtbar {
			seit := serien[serie.ID]
			if seit.Before(grenze) {
				seit = grenze
			}
			var n int
			var juengste int64
			for _, at := range jeSerie[serie.ID] {
				if at <= seit.Unix() {
					continue
				}
				n++
				if at > juengste {
					juengste = at
				}
			}
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
		// Die Reihen samt Filmen kommen aus dem Speicher (reihen.go), das
		// Hinzufügen der Filme aus baum (fortschritt_abbild.go). Gelesen
		// werden nur die Zeilen der wenigen neuen Filme — vorher ging das
		// über alle Filme aller Reihen, die man angefangen hat.
		reihen, err := s.Reihen(ctx, false)
		if err != nil {
			return nil, err
		}
		reiheVon := map[string]Reihe{} // neuer Film → seine Reihe
		var kandidaten []string
		for _, r := range reihen {
			mein := false
			for _, id := range r.ItemIDs {
				if fertigSet[id] {
					mein = true
					break
				}
			}
			if !mein {
				continue
			}
			for _, id := range r.ItemIDs {
				if !fertigSet[id] {
					if _, schon := reiheVon[id]; !schon {
						reiheVon[id] = r
						kandidaten = append(kandidaten, id)
					}
				}
			}
		}
		b, err := s.baum(ctx, kandidaten)
		if err != nil {
			return nil, err
		}
		var neue []string
		for _, id := range kandidaten {
			if e := b[id]; e.da && e.neu > grenze.Unix() {
				neue = append(neue, id)
			}
		}
		if len(neue) > 0 {
			// Sichtbarkeit: nur, was die Freigabe erlaubt.
			rq := q
			rq.IDs, rq.Kinds, rq.Limit = neue, []string{"movie", "series"}, len(neue)
			sichtbar, err := s.Query(ctx, rq)
			if err != nil {
				return nil, err
			}
			sort.SliceStable(sichtbar, func(i, j int) bool { return sichtbar[i].AddedAt.After(sichtbar[j].AddedAt) })
			if len(sichtbar) > 50 {
				sichtbar = sichtbar[:50]
			}
			for _, it := range sichtbar {
				cname := reiheVon[it.ID].Name
				out = append(out, Neuigkeit{Art: "reihe", ItemID: it.ID, Titel: it.Title,
					Text: "Neu in deiner Reihe „" + cname + "“", At: it.AddedAt,
					Vorlage: Vorlage{Text: "Neu in deiner Reihe „{reihe}“", Werte: []any{"reihe", cname}}})
			}
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
	nq.Sort, nq.Limit, nq.NeuSeit = "added", 500, woche.Unix()
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
	// Je Staffel über den Index item_neu_eltern gezählt (ohne die Zeilen
	// der Folgen), die Serie der Staffel kommt aus baum.
	frq, err := s.db.QueryContext(ctx,
		`SELECT parent_id, count(*), max(added_at) FROM item INDEXED BY item_neu_eltern
		  WHERE kind = 'episode' AND added_at >= ? AND parent_id IS NOT NULL GROUP BY parent_id`, woche.Unix())
	if err != nil {
		return nil, err
	}
	type folgenNeu struct {
		n  int
		at int64
	}
	jeStaffel := map[string]folgenNeu{}
	var staffeln []string
	for frq.Next() {
		var id string
		var f folgenNeu
		if err := frq.Scan(&id, &f.n, &f.at); err != nil {
			frq.Close()
			return nil, err
		}
		jeStaffel[id] = f
		staffeln = append(staffeln, id)
	}
	frq.Close()
	if err := frq.Err(); err != nil {
		return nil, err
	}
	sb, err := s.baum(ctx, staffeln)
	if err != nil {
		return nil, err
	}
	frisch := map[string]folgenNeu{}
	for _, st := range staffeln {
		e := sb[st]
		if e.kind != "season" || e.serie == "" || gemeldet[e.serie] || neuSet[e.serie] {
			continue
		}
		f, n := frisch[e.serie], jeStaffel[st]
		f.n += n.n
		f.at = max(f.at, n.at)
		frisch[e.serie] = f
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
