// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

// Anmeldung und „zuletzt gesehen" ohne Warten auf die Platte.
//
// Jede Anfrage einer App prüft ihr Token (LookupTokenHash). Früher hiess
// das je Anfrage zwei Abfragen — und die erste Anfrage eines Tages
// schrieb dazu „zuletzt gesehen" in die Datenbank, und zwar in der
// Anfrage selbst. Öffnet jemand Lumen am Abend, schicken Browser und
// Bilder gleichzeitig ein Dutzend Anfragen mit demselben Token; jede sah
// den alten Tag und wollte schreiben. Hielt gerade eine Arbeit im
// Hintergrund die Schreibsperre, wartete jede dieser Anfragen darauf —
// bis zu fünf Sekunden (busy_timeout).
//
// Jetzt:
//   - Geprüfte Tokens bleiben im Speicher, solange sich in der Datenbank
//     nichts geändert hat (Stand). Ein abgemeldetes Gerät, ein gesperrtes
//     Konto oder eine geänderte Gruppe rückt den Stand weiter.
//   - „Zuletzt gesehen" (nur der Tag) schreibt ein eigener Schreiber nach
//     ein paar Sekunden gesammelt; die Anfrage wartet nicht darauf.

import (
	"context"
	"sync"
	"time"
)

// tokenMerkHoechstens: so viele geprüfte Tokens bleiben im Speicher.
const tokenMerkHoechstens = 2000

type tokenMerk struct {
	acc   Account
	dev   Device
	stand int64
}

type tokenSpeicher struct {
	mu sync.Mutex
	m  map[[32]byte]tokenMerk
}

func (t *tokenSpeicher) holen(sum [32]byte) (tokenMerk, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	e, ok := t.m[sum]
	if !ok || e.stand != Stand() {
		return tokenMerk{}, false
	}
	return e, true
}

func (t *tokenSpeicher) ablegen(sum [32]byte, e tokenMerk) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.m == nil || len(t.m) >= tokenMerkHoechstens {
		t.m = map[[32]byte]tokenMerk{}
	}
	t.m[sum] = e
}

// ---------------------------------------------------------------- nebenbei

// zuletztAbstand: so lange sammelt der Schreiber, bevor er schreibt.
var zuletztAbstand = 3 * time.Second

// zuletztSchreiber schreibt „zuletzt gesehen" gesammelt und ausserhalb
// der Anfrage.
type zuletztSchreiber struct {
	mu     sync.Mutex
	offen  map[[32]byte]int64 // Token → Tag
	laeuft bool
}

// vormerken: Das Token wurde heute (tag) gesehen.
func (s *Store) zuletztVormerken(sum [32]byte, tag int64) {
	z := &s.zuletzt
	z.mu.Lock()
	defer z.mu.Unlock()
	if z.offen == nil {
		z.offen = map[[32]byte]int64{}
	}
	z.offen[sum] = tag
	if !z.laeuft {
		z.laeuft = true
		go s.zuletztSchreiben()
	}
}

func (s *Store) zuletztSchreiben() {
	time.Sleep(zuletztAbstand)
	s.zuletztJetzt()
}

// zuletztJetzt schreibt alles Vorgemerkte. Auch beim Schliessen.
func (s *Store) zuletztJetzt() {
	z := &s.zuletzt
	z.mu.Lock()
	offen := z.offen
	z.offen = nil
	z.laeuft = false
	z.mu.Unlock()
	if len(offen) == 0 {
		return
	}
	// „Zuletzt gesehen" zeigt keine Seite an, die zwischengespeichert
	// wird — es rückt den Stand nicht weiter.
	ctx := OhneStand(context.Background())
	for sum, tag := range offen {
		s.tokenGesehen(ctx, sum, tag)
	}
}
