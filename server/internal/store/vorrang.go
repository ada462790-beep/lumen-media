// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

// Vorrang der Bedienung vor der Arbeit im Hintergrund.
//
// Auf einer kleinen NAS (zwei Kerne, keine schnelle Platte) teilen sich
// Durchlauf, Metadaten und Nachträge Rechenzeit und Datenbank mit dem,
// der gerade eine Seite öffnet. Die Hintergrundarbeit fragt darum
// zwischen zwei Schritten HintergrundWarten: Läuft gerade eine Anfrage
// der Bedienung (oder kam eben eine), wartet sie kurz. Die Bedienung
// meldet sich mit BedienungBeginnt (Middleware in der API).
//
// Höchstens hoechstensWarten am Stück: Eine Seite, die alle paar
// Sekunden nachfragt (Verwaltung während eines Durchlaufs), darf die
// Arbeit verlangsamen, aber nicht anhalten.

import (
	"context"
	"sync/atomic"
	"time"
)

var (
	bedienungLaeuft  atomic.Int64
	bedienungZuletzt atomic.Int64 // UnixNano des letzten Endes
)

const (
	// ruhe: so lange nach der letzten Anfrage gilt die Bedienung als
	// beschäftigt — eine Seite schickt ihre Anfragen selten auf einmal
	// (erst die Liste, dann die Bilder).
	ruhe = 250 * time.Millisecond
	// hoechstensWarten: länger hält niemand die Hintergrundarbeit an.
	hoechstensWarten = 2 * time.Second
)

// BedienungBeginnt meldet eine Anfrage der Bedienung an; die
// zurückgegebene Funktion meldet sie ab.
func BedienungBeginnt() func() {
	bedienungLaeuft.Add(1)
	return func() {
		bedienungZuletzt.Store(time.Now().UnixNano())
		bedienungLaeuft.Add(-1)
	}
}

// BedienungAktiv: Läuft gerade eine Anfrage der Bedienung oder endete
// eben eine?
func BedienungAktiv() bool {
	if bedienungLaeuft.Load() > 0 {
		return true
	}
	return time.Since(time.Unix(0, bedienungZuletzt.Load())) < ruhe
}

// HintergrundWarten lässt der Bedienung den Vortritt: Es kehrt zurück,
// sobald gerade niemand eine Seite lädt — spätestens nach
// hoechstensWarten oder wenn ctx endet.
func HintergrundWarten(ctx context.Context) {
	if !BedienungAktiv() {
		return
	}
	bis := time.Now().Add(hoechstensWarten)
	t := time.NewTicker(20 * time.Millisecond)
	defer t.Stop()
	for BedienungAktiv() && time.Now().Before(bis) {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
