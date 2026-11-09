// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package stream

// Drossel für Profile ohne „Herunterladen".
//
// Ein Profil, das nicht herunterladen darf, sieht keinen Download-Knopf.
// Den Direktstrom kann es trotzdem abholen — er ist dieselbe Datei, nur
// über einen anderen Weg, und eine App, die abspielt, muss ihn bekommen.
// Ohne Bremse zog ein Werkzeug wie curl damit einen Film in Sekunden auf
// die Platte. Die Drossel hält die Auslieferung nahe an der Echtzeit:
// höchstens das Doppelte der Bitrate, mit einem Anlaufpuffer, damit Start
// und Springen nicht stocken. Schauen merkt niemand; einen Film zu
// kopieren dauert dann mindestens halb so lang wie ihn anzusehen.
//
// Ehrlich: Das verhindert das Speichern nicht, es macht es nur langsam.
// Wer technisch versiert ist, kann einen Strom mitschneiden. Die
// Verwaltung sagt das so.
//
// Eine Drossel gehört zu einer Wiedergabe (Session) und gilt für alle
// ihre Anfragen zusammen — parallele Bereichsanfragen teilen sich das
// Tempo, statt es zu vervielfachen.

import (
	"context"
	"sync"
	"time"
)

// Drossel ist ein Eimer mit Marken: Er füllt sich mit rate Bytes je
// Sekunde bis kap, jedes gesendete Byte nimmt eine Marke. Ist er leer,
// wartet der Sender.
type Drossel struct {
	mu      sync.Mutex
	rate    float64
	kap     float64
	vorrat  float64
	zuletzt time.Time
}

// DrosselFaktor: so viel schneller als die Bitrate darf es höchstens gehen.
const DrosselFaktor = 2

// drosselMindestBits: Untergrenze der Bitrate, mit der gerechnet wird —
// eine Datei ohne oder mit zu kleiner Angabe soll nicht ruckeln.
const drosselMindestBits = 8_000_000

// drosselAnlauf: so viele Sekunden Tempo liegen am Anfang bereit.
const drosselAnlauf = 30 * time.Second

// NeueDrossel für eine Datei mit dieser Bitrate (Bit je Sekunde; 0 =
// unbekannt).
func NeueDrossel(bitrate int) *Drossel {
	b := float64(max(bitrate, drosselMindestBits))
	rate := DrosselFaktor * b / 8
	kap := rate * drosselAnlauf.Seconds()
	return &Drossel{rate: rate, kap: kap, vorrat: kap, zuletzt: time.Now()}
}

// Nehmen bucht n Bytes und wartet, bis sie gedeckt sind. nil-Drossel:
// keine Grenze.
func (d *Drossel) Nehmen(ctx context.Context, n int) error {
	if d == nil || n <= 0 {
		return nil
	}
	d.mu.Lock()
	jetzt := time.Now()
	d.vorrat = min(d.kap, d.vorrat+jetzt.Sub(d.zuletzt).Seconds()*d.rate)
	d.zuletzt = jetzt
	d.vorrat -= float64(n)
	var warte time.Duration
	if d.vorrat < 0 {
		warte = time.Duration(-d.vorrat / d.rate * float64(time.Second))
	}
	d.mu.Unlock()
	if warte <= 0 {
		return nil
	}
	t := time.NewTimer(warte)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
