// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package scan

import (
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"lumen/internal/store"
)

// TeilNummer: die „Teil/Part/Vol. N"-Angabe im Dateinamen (0 = keine) —
// für die Reihenfolge beim Zusammenfügen von Hand.
func TeilNummer(pfad string) int {
	_, nr := teilAngabe(strings.TrimSuffix(filepath.Base(pfad), filepath.Ext(pfad)))
	return nr
}

// TeilBasis: der Titel ohne „Teil N" und ohne Jahr, wie ihn der Scanner
// einem Film in Teilen gibt („Die Pfeiler der Macht - Teil 1 (2016)" →
// „Die Pfeiler der Macht"); "" ohne Teilangabe.
func TeilBasis(pfad string) string {
	g := Parse(filepath.Base(pfad), nil)
	name := strings.TrimSuffix(filepath.Base(pfad), filepath.Ext(pfad))
	basis, nr := teilAngabe(name)
	if nr <= 0 {
		return ""
	}
	if g.Year > 0 {
		basis = strings.TrimPrefix(strings.TrimSpace(basis), "("+strconv.Itoa(g.Year)+")")
	}
	return huebsch(clean(basis))
}

// TeileReihenfolge sortiert Dateien nach ihrer Teilnummer im Namen, ohne
// Nummer nach dem Pfad.
func TeileReihenfolge(pfade []string) []int {
	idx := make([]int, len(pfade))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool {
		na, nb := TeilNummer(pfade[idx[a]]), TeilNummer(pfade[idx[b]])
		if na != nb && na > 0 && nb > 0 {
			return na < nb
		}
		return pfade[idx[a]] < pfade[idx[b]]
	})
	return idx
}

// TeileKandidaten findet Teile eines Films, die als getrennte Filme
// dastehen: im selben Ordner dieselbe Basis mit „Teil/Part/Vol. N",
// verschiedene Nummern, verschiedene Einträge — und kein Widerspruch beim
// Jahr (gleich oder auf einer Seite unbekannt). Fortsetzungen mit
// verschiedenen Jahren („Dune: Part One" 2021, „Part Two" 2024) sind
// keine Kandidaten. Unter Aufräumen gibt es dafür „Als Teile
// zusammenfügen"; der Besitzer entscheidet.
func TeileKandidaten(dateien []store.FilmDatei) [][]store.FilmDatei {
	type schluessel struct{ ordner, basis string }
	gruppen := map[schluessel][]store.FilmDatei{}
	var reihe []schluessel
	for _, d := range dateien {
		name := strings.TrimSuffix(filepath.Base(d.Path), filepath.Ext(d.Path))
		basis, nr := teilAngabe(name)
		if nr <= 0 {
			continue
		}
		g := Parse(filepath.Base(d.Path), nil)
		k := schluessel{filepath.Dir(d.Path), titelSchluessel(clean(strings.TrimPrefix(basis, "("+strconv.Itoa(g.Year)+")")))}
		if k.basis == "" {
			continue
		}
		if _, ok := gruppen[k]; !ok {
			reihe = append(reihe, k)
		}
		gruppen[k] = append(gruppen[k], d)
	}
	var out [][]store.FilmDatei
	for _, k := range reihe {
		g := gruppen[k]
		items, nummern, jahre := map[string]bool{}, map[int]bool{}, map[int]bool{}
		for _, d := range g {
			items[d.ItemID] = true
			nummern[TeilNummer(d.Path)] = true
			if j := Parse(filepath.Base(d.Path), nil).Year; j > 0 {
				jahre[j] = true
			}
		}
		if len(items) < 2 || len(nummern) < 2 || len(jahre) > 1 {
			continue
		}
		pfade := make([]string, len(g))
		for i, d := range g {
			pfade[i] = d.Path
		}
		sortiert := make([]store.FilmDatei, 0, len(g))
		for _, i := range TeileReihenfolge(pfade) {
			sortiert = append(sortiert, g[i])
		}
		out = append(out, sortiert)
	}
	return out
}
