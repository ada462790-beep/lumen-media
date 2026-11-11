// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package uebernahme

// Zuordnungen aus Emby übernehmen: Welcher TMDb-, IMDb- oder TVDb-Eintrag
// gehört zu welchem Film und welcher Serie?
//
// Wer jahrelang in Emby von Hand „Identifizieren" gewählt hat, soll das in
// Lumen nicht wiederholen müssen. Zugeordnet wird über die Datei, nicht
// über den Titel — der Titel ist ja gerade das, was in Lumen vielleicht
// noch falsch ist.
//
// Emby und Lumen sehen dieselbe Platte unter verschiedenen Pfaden
// („/media/Filme/Matrix (1999)/Matrix.mkv" gegen „/volume1/video/Filme/
// Matrix (1999)/Matrix.mkv"). Darum zählt das längste gemeinsame Ende des
// Pfads: Dateiname und bis zu zwei Ordner darüber. Passt ein Ende auf
// mehrere Lumen-Titel, entscheidet die Dateigrösse; sonst gilt der Titel
// als nicht gefunden, statt zu raten. Serien: Emby nennt den Ordner der
// Serie, Lumen kennt die Ordner über den Folgen.

import (
	"strings"
)

// LumenOrt sind die Dateien eines Lumen-Films oder die Dateien der Folgen
// einer Lumen-Serie.
type LumenOrt struct {
	ItemID   string
	Kind     string // movie | series
	Pfade    []string
	Groessen []int64 // je Pfad, 0 = unbekannt
}

// Zuordnung ist ein Lumen-Titel mit den Kennungen, die Emby für ihn kennt.
type Zuordnung struct {
	ItemID string
	Kind   string
	TMDb   string
	IMDb   string
	TVDb   string
}

// ZuordnungsPlan ist das Ergebnis des Abgleichs.
type ZuordnungsPlan struct {
	Zuordnungen []Zuordnung
	// NichtGefunden: Emby-Titel mit Kennung, zu denen es in Lumen keinen
	// (eindeutigen) Titel gibt. Fehlend nennt bis zu 200 davon.
	NichtGefunden int
	Fehlend       []string
	// OhneKennung: Emby-Titel ohne jede Kennung — da gibt es nichts zu
	// übernehmen.
	OhneKennung int
}

// tiefe: so viele Teile des Pfads zählen höchstens (Datei und zwei Ordner).
const tiefe = 3

type ortsIndex struct {
	// je Art: Pfadende → Lumen-Titel
	enden map[string]map[string][]string
	// Grössen je Titel
	groessen map[string]map[int64]bool
}

func pfadTeile(p string) []string {
	p = strings.ToLower(strings.ReplaceAll(p, `\`, "/"))
	var out []string
	for _, t := range strings.Split(p, "/") {
		if t = strings.TrimSpace(t); t != "" && t != "." {
			out = append(out, t)
		}
	}
	return out
}

// enden: die Enden eines Pfads mit 1 bis tiefe Teilen.
func enden(teile []string) []string {
	var out []string
	for k := 1; k <= tiefe && k <= len(teile); k++ {
		out = append(out, strings.Join(teile[len(teile)-k:], "/"))
	}
	return out
}

func neuerOrtsIndex(orte []LumenOrt) *ortsIndex {
	ix := &ortsIndex{enden: map[string]map[string][]string{"movie": {}, "series": {}},
		groessen: map[string]map[int64]bool{}}
	dazu := func(kind, ende, id string) {
		m := ix.enden[kind]
		for _, x := range m[ende] {
			if x == id {
				return
			}
		}
		m[ende] = append(m[ende], id)
	}
	for _, o := range orte {
		if o.Kind != "movie" && o.Kind != "series" {
			continue
		}
		for i, p := range o.Pfade {
			teile := pfadTeile(p)
			if len(teile) == 0 {
				continue
			}
			if o.Kind == "movie" {
				// Die Datei selbst — und ihre Ordner, für Emby-Einträge,
				// die einen Ordner nennen (BluRay- und DVD-Ordner).
				for _, e := range enden(teile) {
					dazu("movie", e, o.ItemID)
				}
				if i < len(o.Groessen) && o.Groessen[i] > 0 {
					if ix.groessen[o.ItemID] == nil {
						ix.groessen[o.ItemID] = map[int64]bool{}
					}
					ix.groessen[o.ItemID][o.Groessen[i]] = true
				}
			}
			// Die Ordner über der Datei (bei Serien: Staffel- und
			// Serienordner, bei Filmen der Filmordner).
			for j := len(teile) - 1; j >= 1; j-- {
				for _, e := range enden(teile[:j]) {
					dazu(o.Kind, e, o.ItemID)
				}
			}
		}
	}
	return ix
}

// finden sucht den Lumen-Titel zu einem Emby-Pfad: das längste Ende, das
// genau einen Titel trifft — bei mehreren entscheidet die Grösse.
func (ix *ortsIndex) finden(kind, pfad string, groesse int64) string {
	teile := pfadTeile(pfad)
	es := enden(teile)
	for i := len(es) - 1; i >= 0; i-- {
		ids := ix.enden[kind][es[i]]
		switch {
		case len(ids) == 1:
			return ids[0]
		case len(ids) > 1:
			if groesse > 0 {
				var passend []string
				for _, id := range ids {
					if ix.groessen[id][groesse] {
						passend = append(passend, id)
					}
				}
				if len(passend) == 1 {
					return passend[0]
				}
			}
			// Mehrdeutig — ein kürzeres Ende wäre es noch mehr.
			return ""
		}
	}
	return ""
}

// ZuordnungenPlanen gleicht die Filme und Serien aus Emby mit den Orten in
// Lumen ab. Folgen zählen nicht: Ihre Zuordnung folgt der Serie.
func ZuordnungenPlanen(emby []Eintrag, orte []LumenOrt) ZuordnungsPlan {
	ix := neuerOrtsIndex(orte)
	var plan ZuordnungsPlan
	schon := map[string]bool{}
	for _, e := range emby {
		kind := map[string]string{"Movie": "movie", "Series": "series"}[e.Typ]
		if kind == "" {
			continue
		}
		z := Zuordnung{Kind: kind, TMDb: nurZiffern(e.Kennungen["tmdb"]),
			IMDb: imdbKennung(e.Kennungen["imdb"]), TVDb: nurZiffern(e.Kennungen["tvdb"])}
		if z.TMDb == "" && z.IMDb == "" && z.TVDb == "" {
			plan.OhneKennung++
			continue
		}
		id := ""
		if e.Pfad != "" {
			id = ix.finden(kind, e.Pfad, e.Groesse)
		}
		if id == "" || schon[id] {
			// Zwei Emby-Einträge für denselben Lumen-Titel (etwa eine
			// Fassung doppelt): der erste gilt.
			if id == "" {
				plan.NichtGefunden++
				if len(plan.Fehlend) < 200 {
					n := e.Name
					if e.Jahr > 0 {
						n += " (" + itoa(int64(e.Jahr)) + ")"
					}
					plan.Fehlend = append(plan.Fehlend, n)
				}
			}
			continue
		}
		schon[id] = true
		z.ItemID = id
		plan.Zuordnungen = append(plan.Zuordnungen, z)
	}
	return plan
}

func nurZiffern(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > 12 {
		return ""
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return ""
		}
	}
	return s
}

func imdbKennung(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if len(s) < 8 || len(s) > 12 || !strings.HasPrefix(s, "tt") || nurZiffern(s[2:]) == "" {
		return ""
	}
	return s
}
