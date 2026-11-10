// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package vorspann

// Eine Folge gegen ihre Nachbarn: Vorspann im Anfangsfenster, Abspann im
// Endfenster. Welche Folgen Nachbarn sind und woher die Abdrücke kommen,
// entscheidet der Aufrufer (api/vorspann_erkennung.go).

import "math"

// Grenzen für das, was als Vorspann oder Abspann gilt.
const (
	VorspannMin = 15.0  // Sekunden
	VorspannMax = 120.0 // länger ist kein Vorspann mehr, sondern Handlung
	AbspannMin  = 10.0
	// Ein Abspann reicht bis kurz vors Ende; danach darf höchstens noch
	// eine Szene oder eine Vorschau kommen.
	abspannRest = 180.0
	// Beginnt der Vorspann in den ersten Sekunden, beginnt er bei null.
	vorspannAnfang = 3.0
)

// AnfangFenster: wo nach dem Vorspann gesucht wird — die ersten zehn
// Minuten, bei kurzen Folgen das erste Drittel. Ein „Cold Open" vor dem
// Vorspann hat darin Platz.
func AnfangFenster(dauer float64) (ab, laenge float64) {
	return 0, math.Min(600, dauer*0.35)
}

// EndeFenster: die letzten fünf Minuten, bei kurzen Folgen das letzte
// Viertel.
func EndeFenster(dauer float64) (ab, laenge float64) {
	laenge = math.Min(300, dauer*0.25)
	return math.Max(0, dauer-laenge), laenge
}

// Folge ist, was die Erkennung von einer Folge braucht.
type Folge struct {
	Schluessel string
	Dauer      float64 // Sekunden
	Anfang     *Abdruck
	Ende       *Abdruck
}

// Befund einer Folge. Nil heisst: nichts Passendes gefunden.
type Befund struct {
	Vorspann *Ergebnis
	Abspann  *Ergebnis
	Partner  int // mit wie vielen Folgen verglichen wurde
}

// Untersucher merkt sich Vergleiche: Folge 3 gegen 4 braucht es für den
// Befund beider. Der Speicher ist begrenzt.
type Untersucher struct {
	paare map[[3]string][]Treffer
}

func NeuerUntersucher() *Untersucher { return &Untersucher{paare: map[[3]string][]Treffer{}} }

func (u *Untersucher) vergleich(a, b *Folge, teil string, abA, abB *Abdruck, minSek float64) []Spanne {
	umgedreht := a.Schluessel > b.Schluessel
	k := [3]string{a.Schluessel, b.Schluessel, teil}
	if umgedreht {
		k = [3]string{b.Schluessel, a.Schluessel, teil}
	}
	t, da := u.paare[k]
	if !da {
		if umgedreht {
			t = Vergleichen(abB, abA, minSek)
		} else {
			t = Vergleichen(abA, abB, minSek)
		}
		if len(u.paare) > 4096 {
			clear(u.paare)
		}
		u.paare[k] = t
	}
	out := make([]Spanne, 0, len(t))
	for _, x := range t {
		von, bis := x.AVon, x.ABis
		if umgedreht {
			von, bis = x.BVon, x.BBis
		}
		// Bis: das Ende des letzten Abdrucks, nicht seine Mitte.
		out = append(out, Spanne{Von: abA.Zeit(von), Bis: abA.Zeit(bis-1) + SchrittSek, Fehler: x.Fehler})
	}
	return out
}

// Untersuchen vergleicht ziel mit den Partnern. Fehlt einer Folge ein
// Abdruck (kein Ton, zu kurz), fällt sie für diesen Teil einfach weg.
func (u *Untersucher) Untersuchen(ziel *Folge, partner []*Folge) Befund {
	b := Befund{Partner: len(partner)}
	if ziel.Anfang != nil {
		var je [][]Spanne
		for _, p := range partner {
			if p.Anfang != nil && p.Schluessel != ziel.Schluessel {
				je = append(je, u.vergleich(ziel, p, "a", ziel.Anfang, p.Anfang, VorspannMin))
			}
		}
		passt := func(s Spanne) bool {
			return s.Dauer() >= VorspannMin && s.Dauer() <= VorspannMax && s.Bis <= ziel.Dauer*0.5
		}
		if e, ok := Abstimmen(je, passt); ok {
			if e.Von < vorspannAnfang {
				e.Von = 0
			}
			b.Vorspann = &e
		}
	}
	if ziel.Ende != nil {
		var je [][]Spanne
		for _, p := range partner {
			if p.Ende != nil && p.Schluessel != ziel.Schluessel {
				je = append(je, u.vergleich(ziel, p, "e", ziel.Ende, p.Ende, AbspannMin))
			}
		}
		passt := func(s Spanne) bool {
			return s.Dauer() >= AbspannMin && s.Von >= ziel.Dauer*0.5 && s.Bis >= ziel.Dauer-abspannRest
		}
		if e, ok := Abstimmen(je, passt); ok {
			b.Abspann = &e
		}
	}
	return b
}
