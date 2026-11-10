// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package vorspann

// Der Vergleich zweier Abdrücke: Wo hören beide dasselbe?
//
// Jeder mögliche Versatz zwischen den beiden ist eine Diagonale. Auf ihr
// zählt Lumen je Abdruck die verschiedenen Bits; Zufall ergibt etwa die
// Hälfte (16 von 32), dieselbe Musik deutlich weniger. Gesucht sind
// zusammenhängende Strecken, auf denen der gleitende Mittelwert unter der
// Schwelle bleibt.
//
// Alle Diagonalen voll durchzurechnen wären bei zehn Minuten 6460 × 6460
// Vergleiche — auf einem schwachen NAS gut eine Sekunde je Paar. Darum
// zwei Stufen: grob nur jeder zweite Abdruck in Blöcken von 3 s, und nur
// Diagonalen, auf denen grob etwas zu sehen war, werden fein gerechnet.

import (
	"math"
	"sort"
)

const (
	glatt          = 32   // gleitender Mittelwert über 3 s
	schwelle       = 0.40 // mittlere Bitfehlerrate, unter der „dasselbe" gilt
	grobBlock      = 32   // Abdrücke je Block in der groben Stufe
	grobSchritt    = 2
	grobSchwelle   = 0.43
	hoechstTreffer = 3
)

// Treffer ist ein gemeinsamer Abschnitt zweier Abdrücke, in Abdrücken
// gezählt: [AVon, ABis) in a, [BVon, BBis) in b.
type Treffer struct {
	AVon, ABis int
	BVon, BBis int
	Fehler     float64 // mittlere Bitfehlerrate darin (0 … 0,5)
}

// Laenge in Abdrücken.
func (t Treffer) Laenge() int { return t.ABis - t.AVon }

// Vergleichen sucht die längsten gemeinsamen Abschnitte von mindestens
// minSek Sekunden — höchstens drei, die sich weder in a noch in b
// überschneiden. Ein Abschnitt im Vorspannfenster einer Folge kann ja
// auch eine Rückblende auf die vorige Folge sein; welcher davon der
// Vorspann ist, entscheidet erst die Abstimmung über mehrere Folgen.
func Vergleichen(a, b *Abdruck, minSek float64) []Treffer {
	minN := int(minSek / SchrittSek)
	if minN < glatt {
		minN = glatt
	}
	na, nb := len(a.Bits), len(b.Bits)
	if na < minN || nb < minN {
		return nil
	}
	// Grob: welche Diagonalen lohnen sich? d = i - j.
	kandidat := map[int]bool{}
	grobGrenze := int(math.Round(grobSchwelle * 32 * grobBlock / grobSchritt))
	brauchtBloecke := minN/grobBlock - 1
	if brauchtBloecke < 1 {
		brauchtBloecke = 1
	}
	for d := -(nb - minN); d <= na-minN; d++ {
		i0 := max(0, d)
		i1 := min(na, nb+d)
		folge := 0
		for blk := i0; blk+grobBlock <= i1; blk += grobBlock {
			summe := 0
			for i := blk; i < blk+grobBlock; i += grobSchritt {
				summe += fehler(a, b, i, i-d)
			}
			if summe < grobGrenze {
				folge++
				if folge >= brauchtBloecke {
					kandidat[d-1], kandidat[d], kandidat[d+1] = true, true, true
					break
				}
			} else {
				folge = 0
			}
		}
	}
	// Fein: auf jeder Kandidaten-Diagonale die Strecken unter der Schwelle.
	var alle []Treffer
	e := make([]int, 0, min(na, nb))
	summe := make([]int, 0, min(na, nb)+1)
	for d := range kandidat {
		i0 := max(0, d)
		i1 := min(na, nb+d)
		if i1-i0 < minN {
			continue
		}
		e = e[:0]
		summe = append(summe[:0], 0)
		for i := i0; i < i1; i++ {
			f := fehler(a, b, i, i-d)
			e = append(e, f)
			summe = append(summe, summe[len(summe)-1]+f)
		}
		for _, s := range strecken(summe, minN) {
			alle = append(alle, Treffer{AVon: i0 + s[0], ABis: i0 + s[1], BVon: i0 + s[0] - d, BBis: i0 + s[1] - d,
				Fehler: float64(summe[s[1]]-summe[s[0]]) / float64(32*(s[1]-s[0]))})
		}
	}
	return auswaehlen(alle)
}

// strecken findet auf einer Diagonale (als Präfixsummen der Fehler) die
// Abschnitte, deren gleitender Mittelwert unter der Schwelle bleibt.
// Kurze Unterbrechungen (ein Geräusch, ein Satz über der Musik) werden
// überbrückt, wenn der Abschnitt im Ganzen noch unter der Schwelle liegt.
func strecken(summe []int, minN int) [][2]int {
	n := len(summe) - 1
	grenze := schwelle * 32
	var roh [][2]int
	anfang := -1
	for k := 0; k <= n; k++ {
		gut := false
		if k < n {
			von, bis := max(0, k-glatt/2), min(n, k+glatt/2)
			gut = float64(summe[bis]-summe[von])/float64(bis-von) < grenze
		}
		switch {
		case gut && anfang < 0:
			anfang = k
		case !gut && anfang >= 0:
			roh = append(roh, [2]int{anfang, k})
			anfang = -1
		}
	}
	// Überbrücken: Lücken bis 6 s.
	var out [][2]int
	for _, s := range roh {
		if m := len(out); m > 0 && s[0]-out[m-1][1] <= 2*glatt {
			von := out[m-1][0]
			if float64(summe[s[1]]-summe[von])/float64(s[1]-von) < grenze {
				out[m-1][1] = s[1]
				continue
			}
		}
		out = append(out, s)
	}
	gefiltert := out[:0]
	for _, s := range out {
		if s[1]-s[0] >= minN {
			gefiltert = append(gefiltert, s)
		}
	}
	return gefiltert
}

// auswaehlen nimmt die längsten Treffer, die sich nicht überschneiden —
// benachbarte Diagonalen finden denselben Abschnitt mehrmals.
func auswaehlen(alle []Treffer) []Treffer {
	sort.Slice(alle, func(x, y int) bool {
		if alle[x].Laenge() != alle[y].Laenge() {
			return alle[x].Laenge() > alle[y].Laenge()
		}
		if alle[x].Fehler != alle[y].Fehler {
			return alle[x].Fehler < alle[y].Fehler
		}
		return alle[x].AVon < alle[y].AVon
	})
	var out []Treffer
	for _, t := range alle {
		frei := true
		for _, o := range out {
			if ueberlapp(t.AVon, t.ABis, o.AVon, o.ABis)*2 > t.Laenge() ||
				ueberlapp(t.BVon, t.BBis, o.BVon, o.BBis)*2 > t.Laenge() {
				frei = false
				break
			}
		}
		if frei {
			out = append(out, t)
			if len(out) == hoechstTreffer {
				break
			}
		}
	}
	return out
}

func ueberlapp(a0, a1, b0, b1 int) int {
	if v := min(a1, b1) - max(a0, b0); v > 0 {
		return v
	}
	return 0
}
