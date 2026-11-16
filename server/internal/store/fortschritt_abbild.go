// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

// Der eigene Fortschritt eines Profils im Speicher — und zu jeder Folge
// ihre Serie.
//
// „Weiterschauen", „Noch nicht angefangen", die Balken an den Serien, die
// Neuigkeiten: Alles fragte die Datenbank „welche Folgen hat dieses Profil
// angefangen, und zu welcher Serie gehören sie?" — bei viertausend
// gesehenen Folgen viertausend Wege von progress zu item, auf der
// Startseite fünfmal. Nach ein paar Stunden Ruhe liegen diese Seiten nicht
// mehr im Arbeitsspeicher der NAS, und jeder Weg ist ein Sprung auf der
// Platte (9,7 s für die Startseite).
//
// Jetzt liest Lumen den Fortschritt eines Profils einmal (eine Abfrage,
// nur dieses Konto) und behält ihn, bis in progress geschrieben wird
// (standFortschrittJetzt). Zu welcher Serie eine Folge gehört, steht in
// baum, bis sich die Mediathek ändert (StandMediathek). Beides überdauert
// die Ruhe: Wer abends öffnet, braucht dafür die Platte nicht.
//
// Das Abbild gehört genau einem Konto — wie jede Abfrage auf progress.
// Es wird nicht über Konten hinweg zusammengezählt oder verglichen.

import (
	"context"
	"sync"
)

// fortschrittZeile: ein Eintrag des eigenen Fortschritts.
type fortschrittZeile struct {
	id     string
	pos    int64
	dauer  int64
	reihe  int64
	fertig bool
}

type abbild struct {
	stand int64
	// zeilen nach Reihenfolge, das zuletzt Berührte zuerst.
	zeilen []fortschrittZeile
	je     map[string]int // Kennung → Platz in zeilen
}

type abbildSpeicher struct {
	mu sync.Mutex
	m  map[string]*abbild // je Konto
}

// fortschrittAbbild: der Fortschritt dieses Kontos, aus dem Speicher oder
// einmal gelesen. Das Ergebnis nicht verändern.
func (s *Store) fortschrittAbbild(ctx context.Context, accountID string) (*abbild, error) {
	stand := standFortschrittJetzt()
	sp := &s.abbilder
	sp.mu.Lock()
	if a := sp.m[accountID]; a != nil && a.stand == stand {
		sp.mu.Unlock()
		return a, nil
	}
	sp.mu.Unlock()

	rows, err := s.db.QueryContext(ctx,
		`SELECT item_id, position_ms, duration_ms, finished, reihenfolge FROM progress
		  WHERE account_id = ? ORDER BY reihenfolge DESC`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	a := &abbild{stand: stand, je: map[string]int{}}
	for rows.Next() {
		var z fortschrittZeile
		var fin int
		if err := rows.Scan(&z.id, &z.pos, &z.dauer, &fin, &z.reihe); err != nil {
			return nil, err
		}
		z.fertig = fin == 1
		a.je[z.id] = len(a.zeilen)
		a.zeilen = append(a.zeilen, z)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sp.mu.Lock()
	if sp.m == nil {
		sp.m = map[string]*abbild{}
	}
	// Nur ablegen, wenn sich derweil nichts geändert hat.
	if standFortschrittJetzt() == stand {
		sp.m[accountID] = a
	}
	sp.mu.Unlock()
	return a, nil
}

// zeile: der eigene Stand zu einem Titel.
func (a *abbild) zeile(id string) (fortschrittZeile, bool) {
	if i, ok := a.je[id]; ok {
		return a.zeilen[i], true
	}
	return fortschrittZeile{}, false
}

// ---------------------------------------------------------------- Baum

// baumEintrag: wo ein Titel in der Mediathek hängt.
type baumEintrag struct {
	kind    string
	serie   string // bei Folgen die Serie (über die Staffel), bei Staffeln ihre Serie, sonst leer
	staffel int    // bei Folgen: die Nummer ihrer Staffel
	neu     int64  // added_at
	da      bool   // den Titel gibt es
}

type baumSpeicher struct {
	mu    sync.Mutex
	stand int64
	m     map[string]baumEintrag
}

// baumHoechstens: mehr Einträge behält der Speicher nicht (etwa 150 Byte
// je Eintrag).
const baumHoechstens = 300000

// baum: zu jeder Kennung Art, Serie, Staffel und Hinzufügen — aus dem
// Speicher oder über den schmalen Index item_baum (schmal.go).
func (s *Store) baum(ctx context.Context, ids []string) (map[string]baumEintrag, error) {
	out := make(map[string]baumEintrag, len(ids))
	stand := StandMediathek()
	b := &s.baumMerk
	var fehlt []string
	b.mu.Lock()
	if b.stand != stand || b.m == nil {
		b.stand, b.m = stand, map[string]baumEintrag{}
	}
	for _, id := range ids {
		if e, ok := b.m[id]; ok {
			out[id] = e
		} else {
			fehlt = append(fehlt, id)
		}
	}
	b.mu.Unlock()
	if len(fehlt) == 0 {
		return out, nil
	}
	neu := make(map[string]baumEintrag, len(fehlt))
	for a := 0; a < len(fehlt); a += 500 {
		teil := fehlt[a:min(a+500, len(fehlt))]
		rows, err := s.db.QueryContext(ctx,
			`SELECT e.id, e.kind,
			        CASE e.kind WHEN 'episode' THEN COALESCE(st.parent_id, '')
			                    WHEN 'season' THEN COALESCE(e.parent_id, '') ELSE '' END,
			        COALESCE(st.season_no, 0), e.added_at
			   FROM item e LEFT JOIN item st ON st.id = e.parent_id AND e.kind = 'episode'
			  WHERE e.id IN (`+placeholders(len(teil))+`)`, toAny(teil)...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id string
			e := baumEintrag{da: true}
			if err := rows.Scan(&id, &e.kind, &e.serie, &e.staffel, &e.neu); err != nil {
				rows.Close()
				return nil, err
			}
			neu[id] = e
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	for _, id := range fehlt {
		out[id] = neu[id] // fehlt er ganz: da = false
	}
	b.mu.Lock()
	if b.stand == stand && StandMediathek() == stand {
		if len(b.m)+len(fehlt) > baumHoechstens {
			b.m = map[string]baumEintrag{}
		}
		for _, id := range fehlt {
			b.m[id] = neu[id]
		}
	}
	b.mu.Unlock()
	return out, nil
}

// abbildMitBaum: der eigene Fortschritt und zu jedem Eintrag sein Platz
// in der Mediathek.
func (s *Store) abbildMitBaum(ctx context.Context, accountID string) (*abbild, map[string]baumEintrag, error) {
	a, err := s.fortschrittAbbild(ctx, accountID)
	if err != nil {
		return nil, nil, err
	}
	ids := make([]string, len(a.zeilen))
	for i, z := range a.zeilen {
		ids[i] = z.id
	}
	b, err := s.baum(ctx, ids)
	if err != nil {
		return nil, nil, err
	}
	return a, b, nil
}
