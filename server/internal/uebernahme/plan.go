// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package uebernahme

import (
	"fmt"
	"sort"
	"time"
)

// Uebernahme ist, was für einen Lumen-Titel übernommen werden soll.
type Uebernahme struct {
	ItemID     string
	Gesehen    bool
	PositionMS int64
	LaengeMS   int64
	Favorit    bool
	Sterne     int
	zuletzt    time.Time
}

// Plan ist die Vorschau: was gefunden wurde und was nicht.
type Plan struct {
	Eintraege     []Uebernahme
	Gesehen       int
	Angefangen    int
	Favoriten     int
	Bewertungen   int
	NichtGefunden int
	// Fehlend: die Namen der nicht gefundenen Titel, höchstens 200 —
	// damit man sieht, woran es lag.
	Fehlend []string
	// Wie zugeordnet wurde: "kennung" → 812, "datei" → 300 …
	Wie map[string]int
}

// Planen ordnet alle Einträge zu und fasst zusammen. Mehrere Emby-Einträge
// für denselben Lumen-Titel (zwei Fassungen eines Films) werden eins: Was
// in einer gesehen ist, gilt als gesehen; die weiteste Stelle gewinnt.
func Planen(titel []Eintrag, serien map[string]Eintrag, ix *Index) Plan {
	p := Plan{Wie: map[string]int{}}
	nach := map[string]*Uebernahme{}
	var reihe []string
	for _, e := range titel {
		relevant := e.Gesehen || e.PositionMS > 0 || e.Favorit || e.Sterne > 0
		if e.Typ == "Series" {
			// Eine Serie ist „gesehen", wenn alle Folgen es sind — das
			// steht schon an den Folgen. Hier zählen nur Herz und Sterne.
			relevant = e.Favorit || e.Sterne > 0
		}
		if !relevant {
			continue
		}
		id, wie := ix.Zuordnen(e, serien)
		if id == "" {
			p.NichtGefunden++
			if len(p.Fehlend) < 200 {
				p.Fehlend = append(p.Fehlend, anzeigeName(e))
			}
			continue
		}
		p.Wie[wie]++
		u := nach[id]
		if u == nil {
			u = &Uebernahme{ItemID: id}
			nach[id] = u
			reihe = append(reihe, id)
		}
		if e.Typ != "Series" {
			u.Gesehen = u.Gesehen || e.Gesehen
			if e.PositionMS > u.PositionMS {
				u.PositionMS = e.PositionMS
			}
		}
		if e.LaengeMS > u.LaengeMS {
			u.LaengeMS = e.LaengeMS
		}
		if l := ix.Titel(id); l != nil && u.LaengeMS == 0 {
			u.LaengeMS = l.LaengeMS
		}
		u.Favorit = u.Favorit || e.Favorit
		if e.Sterne > u.Sterne {
			u.Sterne = e.Sterne
		}
		if e.Zuletzt.After(u.zuletzt) {
			u.zuletzt = e.Zuletzt
		}
	}
	for _, id := range reihe {
		u := nach[id]
		// Gesehen geht vor angefangen: eine Stelle in einem gesehenen
		// Titel ist kein „Weiterschauen".
		if u.Gesehen {
			p.Gesehen++
		} else if u.PositionMS > 0 {
			p.Angefangen++
		}
		if u.Favorit {
			p.Favoriten++
		}
		if u.Sterne > 0 {
			p.Bewertungen++
		}
		p.Eintraege = append(p.Eintraege, *u)
	}
	// Das zuletzt Gesehene zuletzt: So steht es in Lumens „Weiterschauen"
	// vorn, wie in Emby.
	sort.SliceStable(p.Eintraege, func(i, j int) bool {
		return p.Eintraege[i].zuletzt.Before(p.Eintraege[j].zuletzt)
	})
	return p
}

func anzeigeName(e Eintrag) string {
	if e.Typ == "Episode" {
		return fmt.Sprintf("%s · S%02dE%02d %s", e.SerieName, e.Staffel, e.Folge, e.Name)
	}
	if e.Jahr > 0 {
		return fmt.Sprintf("%s (%d)", e.Name, e.Jahr)
	}
	return e.Name
}
