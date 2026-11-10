// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

// Package vorspann erkennt Vorspann und Abspann von Folgen am Ton.
//
// Folgen einer Staffel teilen sich die Titelmusik. Wo zwei Folgen über
// eine Viertelminute lang dasselbe hören lassen, ist mit grosser
// Wahrscheinlichkeit der Vorspann (am Anfang) oder der Abspann (am Ende).
// Dafür braucht es kein Chromaprint — das Alpine-FFmpeg bringt es nicht
// mit —, sondern nur rohen Ton aus FFmpeg und einen kleinen Abdruck, der
// hier in Go entsteht:
//
//   - Mono, 5512 Hz, 16 Bit. Mehr braucht die Erkennung nicht, und so ist
//     der Ton von zehn Minuten keine 7 MB — und wird ohnehin nur
//     durchgereicht, nicht aufbewahrt.
//   - Alle 93 ms ein Fenster von 0,37 s, davon das Spektrum (eigene FFT),
//     verteilt auf 33 Bänder zwischen 250 und 2500 Hz.
//   - Je Fenster 32 Bit: ob der Unterschied zwischen zwei benachbarten
//     Bändern gegenüber dem vorigen Fenster wächst oder schrumpft
//     (Haitsma/Kalker, „A Highly Robust Audio Fingerprinting System").
//     Lautstärke, Umwandlung in ein anderes Format und leichtes Rauschen
//     ändern daran wenig; anderer Inhalt kippt etwa die Hälfte der Bits.
//
// Das Paket weiss nichts von Datenbank und Titeln; es bekommt Ton und
// gibt Zeitspannen zurück (vergleich.go, abstimmung.go).
package vorspann

import (
	"bufio"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"math/bits"
)

// Die Masse des Abdrucks. Wer hier etwas ändert, erhöht Fassung — sonst
// vergliche Lumen Abdrücke, die nicht zusammenpassen, und die
// gespeicherten Ergebnisse würden nie neu gerechnet.
const (
	Abtastrate = 5512
	fenster    = 2048 // 0,37 s
	schritt    = 512  // 93 ms zwischen zwei Abdrücken
	baender    = 33
	untenHz    = 250.0
	obenHz     = 2500.0
	// Leiser als etwa -50 dBFS gilt als still: Stille sieht in jeder Folge
	// gleich aus und darf nie als „gemeinsamer Abschnitt" zählen.
	stilleRMS = 0.003

	// Fassung des Verfahrens. Ändert sich etwas an Abdruck, Vergleich oder
	// Abstimmung, wird alles einmal neu untersucht.
	Fassung = 1
)

// SchrittSek ist die Zeit zwischen zwei Abdrücken in Sekunden.
const SchrittSek = float64(schritt) / Abtastrate

// mitteSek: Ein Abdruck beschreibt die Mitte seines Fensters.
const mitteSek = float64(fenster) / 2 / Abtastrate

// Abdruck ist der Fingerabdruck eines Tonstücks.
type Abdruck struct {
	Bits  []uint32
	Still []bool  // dieser Abdruck taugt nicht zum Vergleichen
	Ab    float64 // wo das Stück in der Datei beginnt, in Sekunden
}

// Zeit rechnet einen Abdruck in Sekunden ab Dateianfang.
func (a *Abdruck) Zeit(i int) float64 { return a.Ab + float64(i)*SchrittSek + mitteSek }

// Sekunden ist die Länge, die der Abdruck abdeckt.
func (a *Abdruck) Sekunden() float64 { return float64(len(a.Bits)) * SchrittSek }

// rechner baut den Abdruck Fenster für Fenster, ohne den Ton aufzuheben.
type rechner struct {
	puf    [fenster]float64
	n      int // wie viele Werte im Puffer stehen
	fft    *fft
	hann   [fenster]float64
	grenze [baender + 1]int
	vorher [baender]float64
	erst   bool
	re, im []float64
	aus    *Abdruck
}

func neuerRechner(ab float64) *rechner {
	r := &rechner{fft: neueFFT(fenster), erst: true, aus: &Abdruck{Ab: ab},
		re: make([]float64, fenster), im: make([]float64, fenster)}
	for i := range r.hann {
		r.hann[i] = 0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/float64(fenster-1))
	}
	// Bandgrenzen logarithmisch: tiefe Töne liegen dichter.
	for b := 0; b <= baender; b++ {
		hz := untenHz * math.Pow(obenHz/untenHz, float64(b)/baender)
		r.grenze[b] = int(math.Round(hz * fenster / Abtastrate))
	}
	return r
}

// hinzu nimmt einen Abtastwert (-1 … 1).
func (r *rechner) hinzu(x float64) {
	r.puf[r.n] = x
	r.n++
	if r.n < fenster {
		return
	}
	r.fensterRechnen()
	copy(r.puf[:], r.puf[schritt:])
	r.n = fenster - schritt
}

func (r *rechner) fensterRechnen() {
	var summe float64
	for i := 0; i < fenster; i++ {
		summe += r.puf[i] * r.puf[i]
		r.re[i] = r.puf[i] * r.hann[i]
		r.im[i] = 0
	}
	still := math.Sqrt(summe/fenster) < stilleRMS
	r.fft.rechnen(r.re, r.im)
	var e [baender]float64
	for b := 0; b < baender; b++ {
		for k := r.grenze[b]; k < r.grenze[b+1]; k++ {
			e[b] += r.re[k]*r.re[k] + r.im[k]*r.im[k]
		}
	}
	var w uint32
	for b := 0; b < baender-1; b++ {
		if (e[b]-e[b+1])-(r.vorher[b]-r.vorher[b+1]) > 0 {
			w |= 1 << uint(b)
		}
	}
	// Der erste Abdruck hat keinen Vorgänger und taugt nicht.
	r.aus.Bits = append(r.aus.Bits, w)
	r.aus.Still = append(r.aus.Still, still || r.erst)
	r.vorher, r.erst = e, false
}

// AusPCM rechnet den Abdruck aus Abtastwerten (Mono, Abtastrate).
func AusPCM(werte []int16, ab float64) *Abdruck {
	r := neuerRechner(ab)
	for _, v := range werte {
		r.hinzu(float64(v) / 32768)
	}
	return r.aus
}

// Lesen rechnet den Abdruck aus rohem Ton (s16le, Mono, Abtastrate), wie
// ihn FFmpeg mit „-f s16le -ac 1 -ar 5512" schreibt. Höchstens
// hoechstens Sekunden; der Rest wird nicht mehr gelesen.
func Lesen(rd io.Reader, ab, hoechstens float64) (*Abdruck, error) {
	r := neuerRechner(ab)
	br := bufio.NewReaderSize(rd, 64<<10)
	grenze := int(hoechstens * Abtastrate)
	var paar [2]byte
	for n := 0; grenze <= 0 || n < grenze; n++ {
		if _, err := io.ReadFull(br, paar[:]); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				break
			}
			return nil, err
		}
		r.hinzu(float64(int16(binary.LittleEndian.Uint16(paar[:]))) / 32768)
	}
	return r.aus, nil
}

// fehler zählt die verschiedenen Bits zweier Abdrücke; stille zählen wie
// Zufall (16 von 32).
func fehler(a, b *Abdruck, i, j int) int {
	if a.Still[i] || b.Still[j] {
		return 16
	}
	return bits.OnesCount32(a.Bits[i] ^ b.Bits[j])
}

// ---------------------------------------------------------------- FFT

// fft ist eine schlichte Radix-2-FFT fester Länge (Zweierpotenz) mit
// vorgerechneten Drehfaktoren. Für 2048 Werte reicht das allemal.
type fft struct {
	n        int
	umkehr   []int
	cos, sin []float64
}

func neueFFT(n int) *fft {
	f := &fft{n: n, umkehr: make([]int, n), cos: make([]float64, n/2), sin: make([]float64, n/2)}
	stufen := bits.TrailingZeros(uint(n))
	for i := 0; i < n; i++ {
		f.umkehr[i] = int(bits.Reverse(uint(i)) >> (bits.UintSize - stufen))
	}
	for i := 0; i < n/2; i++ {
		w := -2 * math.Pi * float64(i) / float64(n)
		f.cos[i], f.sin[i] = math.Cos(w), math.Sin(w)
	}
	return f
}

// rechnen transformiert re/im an Ort und Stelle.
func (f *fft) rechnen(re, im []float64) {
	n := f.n
	for i := 0; i < n; i++ {
		if j := f.umkehr[i]; j > i {
			re[i], re[j] = re[j], re[i]
			im[i], im[j] = im[j], im[i]
		}
	}
	for laenge := 2; laenge <= n; laenge <<= 1 {
		halb := laenge / 2
		sprung := n / laenge
		for anfang := 0; anfang < n; anfang += laenge {
			for k := 0; k < halb; k++ {
				wr, wi := f.cos[k*sprung], f.sin[k*sprung]
				a, b := anfang+k, anfang+k+halb
				tr := re[b]*wr - im[b]*wi
				ti := re[b]*wi + im[b]*wr
				re[b], im[b] = re[a]-tr, im[a]-ti
				re[a], im[a] = re[a]+tr, im[a]+ti
			}
		}
	}
}
