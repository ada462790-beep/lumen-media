// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package stream

import (
	"context"
	"sync"
	"testing"
	"time"
)

// Nach dem Anlauf geht es nicht schneller als die Rate — auch nicht mit
// mehreren gleichzeitigen Anfragen derselben Wiedergabe.
func TestDrosselHaeltRate(t *testing.T) {
	d := NeueDrossel(8_000_000) // 2 × 8 Mbit/s = 2 MB/s, Anlauf 60 MB
	d.vorrat = 0                // Anlauf aufgebraucht (in Sekunden Film)
	start := time.Now()
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				_ = d.Nehmen(context.Background(), 25_000) // 4 × 10 × 25 kB = 1 MB
			}
		}()
	}
	wg.Wait()
	if dauer := time.Since(start); dauer < 400*time.Millisecond {
		t.Fatalf("1 MB in %v — schneller als 2 MB/s", dauer)
	}
}

// Der Anlauf geht ohne Warten; eine nil-Drossel bremst nie.
func TestDrosselAnlauf(t *testing.T) {
	d := NeueDrossel(0)
	start := time.Now()
	if err := d.Nehmen(context.Background(), 10<<20); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 50*time.Millisecond {
		t.Fatal("Anlauf gebremst")
	}
	var keine *Drossel
	if err := keine.Nehmen(context.Background(), 1<<30); err != nil {
		t.Fatal(err)
	}
	// Abbruch beim Warten.
	d.vorrat = 0
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := d.Nehmen(ctx, 100<<20); err == nil {
		t.Fatal("Warten nicht abgebrochen")
	}
}

// Zwei Geräte mit demselben Namen („Chrome · Linux") sind zwei Geräte:
// Sie verdrängen sich nicht mehr gegenseitig. Dieselbe Anmeldung ersetzt
// ihre eigene Wiedergabe weiterhin.
func TestGleicherNameIstNichtGleichesGeraet(t *testing.T) {
	l := NewLive()
	a, _ := l.Beitreten(&Session{AccountID: "k", DeviceName: "Chrome · Linux", Geraet: "t:1", FileID: "f1", Mode: "direct"}, 5)
	b, _ := l.Beitreten(&Session{AccountID: "k", DeviceName: "Chrome · Linux", Geraet: "t:2", FileID: "f2", Mode: "direct"}, 5)
	if a == nil || b == nil || !l.Laeuft(a.ID) || !l.Laeuft(b.ID) {
		t.Fatal("ein Gerät hat das andere verdrängt")
	}
	l.ReplaceFor("k", "t:2")
	if !l.Laeuft(a.ID) || l.Laeuft(b.ID) {
		t.Fatal("ReplaceFor traf das falsche Gerät")
	}
	if l.Grund(b.ID) != GrundErsetzt {
		t.Fatalf("Grund: %q", l.Grund(b.ID))
	}
}

// falscheUhr: eine Uhr, die nur beim Warten weiterläuft.
func falscheUhr(t *testing.T) *time.Time {
	t.Helper()
	jetzt := time.Unix(1_000_000, 0)
	altU, altW := uhr, warten
	uhr = func() time.Time { return jetzt }
	warten = func(_ context.Context, d time.Duration) error { jetzt = jetzt.Add(d); return nil }
	t.Cleanup(func() { uhr, warten = altU, altW })
	return &jetzt
}

// Ein Film mit wechselnder Bitrate (VBR) läuft für ein Kind ohne Stocken:
// Er beginnt mit drei Minuten Action bei doppelter mittlerer Bitrate, dann
// sechs ruhige Minuten bei der halben. Der Player holt sekundenweise und
// höchstens zwei Minuten voraus. Mit 1,25-fach und 20 Sekunden Anlauf
// stockte das schon nach einer halben Minute.
func TestKinderDrosselVBROhneStocken(t *testing.T) {
	jetzt := falscheUhr(t)
	const mittel = 4_000_000 // Bit je Sekunde im Mittel
	laufzeit := 9 * time.Minute
	d := NeueKinderDrossel(mittel, laufzeit, nil)
	bytesBei := func(sek int) int {
		if sek < 180 {
			return 2 * mittel / 8
		}
		return mittel / 8 / 2
	}
	var geholt, kopf float64 // Sekunden Film geholt / abgespielt
	gestockt := 0.0
	for geholt < laufzeit.Seconds() {
		if geholt-kopf >= 120 { // Puffer voll: eine Sekunde zuschauen
			*jetzt = jetzt.Add(time.Second)
			kopf++
			continue
		}
		vorher := *jetzt
		if err := d.Nehmen(context.Background(), bytesBei(int(geholt))); err != nil {
			t.Fatal(err)
		}
		gewartet := jetzt.Sub(vorher).Seconds()
		if geholt > 0 { // abgespielt wird erst, wenn die erste Sekunde da ist
			if kopf+gewartet > geholt {
				gestockt += kopf + gewartet - geholt
				kopf = geholt
			} else {
				kopf += gewartet
			}
		}
		geholt++
	}
	if gestockt > 0 {
		t.Fatalf("Wiedergabe stockte %.1f s", gestockt)
	}
}

// Schneller als das Doppelte der mittleren Bitrate holt ein Kind den Film
// trotzdem nicht: Ein Programm, das alles am Stück will, braucht für neun
// Minuten Film mindestens gut vier Minuten.
func TestKinderDrosselHortetNicht(t *testing.T) {
	jetzt := falscheUhr(t)
	start := *jetzt
	d := NeueKinderDrossel(4_000_000, 9*time.Minute, nil)
	_ = d.Nehmen(context.Background(), 540*4_000_000/8)
	if dauer := jetzt.Sub(start); dauer < 4*time.Minute {
		t.Fatalf("neun Minuten Film in %v", dauer)
	}
	if a := kinderAnlauf(0); a != kinderAnlaufMax {
		t.Fatalf("Anlauf ohne Laufzeit: %v", a)
	}
	if a := kinderAnlauf(30 * time.Second); a != kinderAnlaufMin {
		t.Fatalf("Anlauf kurzer Film: %v", a)
	}
}
