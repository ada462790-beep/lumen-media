// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package vorspann

// Die Abstimmung: Eine Folge wird mit mehreren anderen verglichen. Der
// Vorspann ist der Abschnitt, den die meisten davon an derselben Stelle
// der Folge wiederfinden. Eine Rückblende („Was bisher geschah") teilt
// die Folge nur mit der einen Folge, aus der sie stammt — und verliert.

import (
	"math"
	"sort"
)

// Mindestsicherheit: Darunter wird ein erkanntes Ergebnis gespeichert,
// aber nicht benutzt. Lieber kein „Vorspann überspringen" als eines, das
// mitten in die Handlung springt.
const Mindestsicherheit = 0.5

// Spanne ist ein Abschnitt in Sekunden ab Dateianfang.
type Spanne struct {
	Von, Bis float64
	Fehler   float64
}

// Dauer in Sekunden.
func (s Spanne) Dauer() float64 { return s.Bis - s.Von }

// Ergebnis der Abstimmung.
type Ergebnis struct {
	Von, Bis   float64
	Sicherheit float64 // 0 … 1
	Stuetzen   int     // wie viele andere Folgen den Abschnitt teilen
}

// Abstimmen wählt aus den Abschnitten, die jede verglichene Folge mit
// dieser teilt (je Partner eine Liste), den, der am besten gestützt ist.
// passt sortiert vorher aus, was nach Lage und Länge nicht in Frage kommt.
// ok ist false, wenn gar nichts übrig bleibt; sonst kommt das beste
// Ergebnis auch dann, wenn es unter der Mindestsicherheit liegt.
func Abstimmen(jePartner [][]Spanne, passt func(Spanne) bool) (Ergebnis, bool) {
	partner := len(jePartner)
	gefiltert := make([][]Spanne, partner)
	for p, liste := range jePartner {
		for _, s := range liste {
			if passt == nil || passt(s) {
				gefiltert[p] = append(gefiltert[p], s)
			}
		}
	}
	var best Ergebnis
	bestFehler := 1.0
	gefunden := false
	for p, liste := range gefiltert {
		for _, saat := range liste {
			// Je Partner der Abschnitt, der am besten zur Saat passt.
			stuetzen := []Spanne{saat}
			for q, andere := range gefiltert {
				if q == p {
					continue
				}
				bester, gut := -1.0, Spanne{}
				for _, s := range andere {
					if v := deckung(saat, s); v > bester {
						bester, gut = v, s
					}
				}
				if bester >= 0 && dieselbe(saat, gut) {
					stuetzen = append(stuetzen, gut)
				}
			}
			var von, bis []float64
			fehlerSumme := 0.0
			for _, s := range stuetzen {
				von, bis = append(von, s.Von), append(bis, s.Bis)
				fehlerSumme += s.Fehler
			}
			f := fehlerSumme / float64(len(stuetzen))
			e := Ergebnis{Von: median(von), Bis: median(bis), Stuetzen: len(stuetzen),
				Sicherheit: sicherheit(len(stuetzen), partner, f)}
			if !gefunden || e.Sicherheit > best.Sicherheit ||
				(e.Sicherheit == best.Sicherheit && (e.Stuetzen > best.Stuetzen ||
					(e.Stuetzen == best.Stuetzen && f < bestFehler))) {
				best, bestFehler, gefunden = e, f, true
			}
		}
	}
	return best, gefunden
}

// sicherheit: Wie gut das Tonbild passt (Fehlerrate) mal wie viele Folgen
// den Abschnitt teilen. Zwei sind genug, bei vielen Partnern die Hälfte.
//
// Gibt es nur einen Partner (eine Serie aus zwei Folgen), reicht er bei
// sehr sauberem Treffer. Gibt es mehrere und nur einer teilt den
// Abschnitt, reicht es nie: Genau so sieht eine Rückblende aus — sie
// stammt aus einer einzigen Folge, oft Bit für Bit, und wäre sonst in
// jener Folge ein „Vorspann" mitten in der Handlung.
func sicherheit(stuetzen, partner int, fehler float64) float64 {
	guete := math.Max(0, math.Min(1, (0.47-fehler)/0.15))
	noetig := math.Max(2, math.Ceil(float64(partner)/2))
	s := guete * math.Min(1, float64(stuetzen)/noetig)
	if stuetzen < 2 && partner > 1 {
		s *= 0.5
	}
	return s
}

// deckung: Anteil der Überschneidung an der kürzeren Spanne.
func deckung(a, b Spanne) float64 {
	v := math.Min(a.Bis, b.Bis) - math.Max(a.Von, b.Von)
	k := math.Min(a.Dauer(), b.Dauer())
	if v <= 0 || k <= 0 {
		return 0
	}
	return v / k
}

// dieselbe: zwei Spannen meinen denselben Abschnitt der Folge — sie
// überschneiden sich weit, und keine ist mehr als dreimal so lang.
func dieselbe(a, b Spanne) bool {
	if deckung(a, b) < 0.6 {
		return false
	}
	l, k := math.Max(a.Dauer(), b.Dauer()), math.Min(a.Dauer(), b.Dauer())
	return k*3 >= l
}

func median(x []float64) float64 {
	if len(x) == 0 {
		return 0
	}
	y := append([]float64(nil), x...)
	sort.Float64s(y)
	m := len(y) / 2
	if len(y)%2 == 1 {
		return y[m]
	}
	return (y[m-1] + y[m]) / 2
}
