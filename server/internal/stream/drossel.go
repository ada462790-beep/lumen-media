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

// Drossel ist ein Eimer mit Marken: Er füllt sich mit rate Marken je
// Sekunde bis kap, ist er leer, wartet der Sender. Gerechnet wird in
// Sekunden Film (Medienzeit): Bytes werden über die Bitrate umgerechnet,
// ein HLS-Segment zählt mit seiner Länge.
type Drossel struct {
	mu      sync.Mutex
	rate    float64 // Sekunden Film je Sekunde Uhrzeit
	kap     float64 // Sekunden Film, die sofort gehen
	vorrat  float64
	bps     float64 // Bytes je Sekunde Film
	zuletzt time.Time
	// geliefert: bekommt jede ausgelieferte Menge in Sekunden Film — für
	// die Bildschirmzeit, die nach ausgelieferter Filmzeit zählt (nil =
	// niemand fragt).
	geliefert func(sekunden float64)
	// kinder: Drossel eines Kinderprofils mit Bildschirmzeit — HLS-
	// Segmente zählen dann nach ihrer Länge, nicht nach Bytes.
	kinder bool
}

// DrosselFaktor: so viel schneller als die Bitrate darf es höchstens gehen.
const DrosselFaktor = 2

// drosselMindestBits: Untergrenze der Bitrate, mit der gerechnet wird —
// eine Datei ohne oder mit zu kleiner Angabe soll nicht ruckeln.
const drosselMindestBits = 8_000_000

// drosselAnlauf: so viele Sekunden Tempo liegen am Anfang bereit.
const drosselAnlauf = 30 * time.Second

// Für Kinderprofile mit Bildschirmzeit gilt eine eigene Drossel: Sonst
// holte ein eigenes Programm einen Film in Sekunden, und die Bildschirmzeit
// sähe nur die Sekunden der Übertragung. Gerechnet wird mit der mittleren
// Bitrate der Datei; Filme mit wechselnder Bitrate (VBR) haben in
// Actionszenen aber Spitzen weit darüber. Mit dem Faktor 1,25 und 20
// Sekunden Anlauf stockte dann das Abspielen. Darum das Doppelte der
// mittleren Bitrate und ein grösserer Anlauf. Horten verhindert das nicht
// allein — das tut die Bildschirmzeit, die nach ausgelieferter Filmzeit
// zählt (api/bildschirmzeit.go).
const (
	// KinderFaktor: so viel schneller als Echtzeit (mittlere Bitrate).
	KinderFaktor = 2.0
	// kinderAnlaufMax: so viele Sekunden Film gehen höchstens sofort —
	// ein Viertel der Laufzeit, aber nicht mehr als das.
	kinderAnlaufMax = 60.0
	// kinderAnlaufMin: so viele Sekunden gehen auch bei kurzen Filmen
	// (oder unbekannter Laufzeit) sofort.
	kinderAnlaufMin = 20.0
	// kinderMindestBits: kleiner wird die Bitrate nicht angenommen; ohne
	// Angabe gilt kinderOhneAngabe.
	kinderMindestBits = 32_000
	kinderOhneAngabe  = 2_000_000
)

// NeueDrossel für eine Datei mit dieser Bitrate (Bit je Sekunde; 0 =
// unbekannt).
func NeueDrossel(bitrate int) *Drossel {
	bps := float64(max(bitrate, drosselMindestBits)) / 8
	rate := float64(DrosselFaktor)
	return &Drossel{rate: rate, kap: rate * drosselAnlauf.Seconds(), vorrat: rate * drosselAnlauf.Seconds(),
		bps: bps, zuletzt: uhr()}
}

// kinderAnlauf: der Anlauf für einen Film dieser Laufzeit — ein Viertel
// davon, zwischen kinderAnlaufMin und kinderAnlaufMax. 0 = unbekannt.
func kinderAnlauf(laufzeit time.Duration) float64 {
	if laufzeit <= 0 {
		return kinderAnlaufMax
	}
	return min(kinderAnlaufMax, max(kinderAnlaufMin, laufzeit.Seconds()/4))
}

// NeueKinderDrossel: die Drossel für ein Kinderprofil mit Bildschirmzeit.
// laufzeit: Länge des Films (0 = unbekannt). geliefert bekommt die
// ausgelieferten Sekunden Film.
func NeueKinderDrossel(bitrate int, laufzeit time.Duration, geliefert func(float64)) *Drossel {
	if bitrate <= 0 {
		bitrate = kinderOhneAngabe
	}
	bps := float64(max(bitrate, kinderMindestBits)) / 8
	anlauf := kinderAnlauf(laufzeit)
	return &Drossel{rate: KinderFaktor, kap: anlauf, vorrat: anlauf, bps: bps,
		zuletzt: uhr(), geliefert: geliefert, kinder: true}
}

// Kinder: die Drossel eines Kinderprofils mit Bildschirmzeit.
func (d *Drossel) Kinder() bool { return d != nil && d.kinder }

// Nehmen bucht n Bytes und wartet, bis sie gedeckt sind. nil-Drossel:
// keine Grenze.
func (d *Drossel) Nehmen(ctx context.Context, n int) error {
	if d == nil || n <= 0 {
		return nil
	}
	return d.Sekunden(ctx, float64(n)/d.bps)
}

// Sekunden bucht sek Sekunden Film (etwa ein HLS-Segment) und wartet, bis
// sie gedeckt sind.
func (d *Drossel) Sekunden(ctx context.Context, sek float64) error {
	if d == nil || sek <= 0 {
		return nil
	}
	if d.geliefert != nil {
		d.geliefert(sek)
	}
	d.mu.Lock()
	jetzt := uhr()
	d.vorrat = min(d.kap, d.vorrat+jetzt.Sub(d.zuletzt).Seconds()*d.rate)
	d.zuletzt = jetzt
	d.vorrat -= sek
	var warte time.Duration
	if d.vorrat < 0 {
		warte = time.Duration(-d.vorrat / d.rate * float64(time.Second))
	}
	d.mu.Unlock()
	if warte <= 0 {
		return nil
	}
	return warten(ctx, warte)
}

// uhr und warten sind austauschbar, damit Tests eine Wiedergabe von
// Minuten in Millisekunden durchspielen.
var (
	uhr    = time.Now
	warten = echtWarten
)

func echtWarten(ctx context.Context, warte time.Duration) error {
	t := time.NewTimer(warte)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
