// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

// Was eine Seite über einzelne Titel aus der Mediathek liest — die Zeile
// selbst, die Bilder, die Zahl der Folgen —, bleibt je Kennung im
// Speicher, bis sich die Mediathek ändert (StandMediathek). Ein Stand beim
// Schauen, ein Haken, die Merkliste ändern daran nichts.
//
// Auf einer NAS, deren Platten nach ein paar Stunden Ruhe kalt sind, kostet
// jede Zeile eines Titels einen Sprung auf der Platte. Die Startseite zeigt
// achtzig bis hundert Titel; mit dem Gemerkten braucht sie dafür keinen.

import "sync"

// merkEintrag: ein gemerkter Wert; da = false heisst „gibt es nicht".
type merkEintrag[T any] struct {
	v  T
	da bool
}

// merkSpeicher: Werte je Kennung, gültig für einen StandMediathek.
type merkSpeicher[T any] struct {
	mu    sync.Mutex
	stand int64
	m     map[string]merkEintrag[T]
}

// holen legt Gemerktes nach out und gibt zurück, was fehlt.
func (k *merkSpeicher[T]) holen(stand int64, ids []string, out map[string]T) []string {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.stand != stand || k.m == nil {
		return ids
	}
	var fehlt []string
	for _, id := range ids {
		e, ok := k.m[id]
		switch {
		case !ok:
			fehlt = append(fehlt, id)
		case e.da:
			out[id] = e.v
		}
	}
	return fehlt
}

// merken legt die Antwort auf gefragt ab — was in neu fehlt, als „gibt es
// nicht". Hat sich die Mediathek inzwischen geändert, passt sie nicht
// mehr und wird verworfen. Mehr als hoechstens Einträge: von vorn.
func (k *merkSpeicher[T]) merken(stand int64, gefragt []string, neu map[string]T, hoechstens int) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if StandMediathek() != stand {
		return
	}
	if k.stand != stand || k.m == nil || len(k.m)+len(gefragt) > hoechstens {
		k.stand, k.m = stand, make(map[string]merkEintrag[T], len(gefragt))
	}
	for _, id := range gefragt {
		v, da := neu[id]
		k.m[id] = merkEintrag[T]{v, da}
	}
}

// zahlGemerkt: eine Zahl (oder zwei) über die ganze Mediathek — Doppel,
// fehlende Dateien, offene Zuordnungen, Poster —, gemerkt bis sich die
// Mediathek ändert. Die Übersicht der Verwaltung fragt sie bei jedem
// Öffnen, und jede zählt über einen ganzen Index.
func (s *Store) zahlGemerkt(key string, rechnen func() ([2]int, error)) ([2]int, error) {
	stand := StandMediathek()
	out := map[string][2]int{}
	if len(s.zahlMerk.holen(stand, []string{key}, out)) == 0 {
		return out[key], nil
	}
	v, err := rechnen()
	if err != nil {
		return v, err
	}
	s.zahlMerk.merken(stand, []string{key}, map[string][2]int{key: v}, 100)
	return v, nil
}
