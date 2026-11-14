// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package scheibe

// Blu-ray: der Hauptfilm aus den Playlists (BDMV/PLAYLIST/*.mpls).
//
// Eine Playlist nennt ihre Clips (BDMV/STREAM/nnnnn.m2ts) mit Ein- und
// Ausstieg (45-kHz-Takt), dazu die Spuren mit Sprache (STN-Tabelle, je
// Spur die PID) und die Kapitelmarken. Der Hauptfilm ist die längste
// Playlist, die keinen Clip zweimal nennt (so verstecken manche
// Scheiben den Film zwischen Hunderten Attrappen). Ohne lesbare
// Playlist: die grösste .m2ts-Datei, wie der Scan.

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type playItem struct {
	clip string
	ein  int64
	aus  int64
}

type playlist struct {
	name   string
	items  []playItem
	spuren []Spur
	marken []int64 // ms ab Beginn der Playlist
	ms     int64
}

func mplsLesen(b []byte) (*playlist, error) {
	if len(b) < 40 || string(b[:4]) != "MPLS" {
		return nil, errors.New("keine playlist")
	}
	pl := &playlist{}
	o := int(u32(b, 8))
	if o+10 > len(b) {
		return nil, errors.New("playlist zu kurz")
	}
	n := u16(b, o+6)
	p := o + 10
	for i := 0; i < n; i++ {
		if p+2 > len(b) {
			return nil, errors.New("playitem ausserhalb")
		}
		laenge := u16(b, p)
		e := p + 2
		if e+laenge > len(b) || laenge < 20 {
			return nil, errors.New("playitem kaputt")
		}
		it := playItem{clip: string(b[e : e+5]), ein: u32(b, e+12), aus: u32(b, e+16)}
		mehrWinkel := b[e+10]&0x10 != 0
		// STN-Tabelle: nur aus dem ersten Eintrag, die übrigen gleichen ihm.
		if i == 0 {
			s := e + 32 // nach Name, Codec, Flags, STC, Ein, Aus, UO-Maske, Flags, Standbild
			if mehrWinkel && s < len(b) {
				winkel := int(b[s])
				s += 2 + (winkel-1)*10
			}
			if s >= e && s+16 <= e+laenge {
				pl.spuren = stnLesen(b[s:min(e+laenge, len(b))])
			}
		}
		if it.aus > it.ein {
			pl.ms += (it.aus - it.ein) / 45
		}
		pl.items = append(pl.items, it)
		p = e + laenge
	}
	// Kapitelmarken.
	mo := int(u32(b, 12))
	if mo > 0 && mo+6 <= len(b) {
		nm := u16(b, mo+4)
		for i := 0; i < nm; i++ {
			m := mo + 6 + i*14
			if m+14 > len(b) {
				break
			}
			if b[m+1] != 1 { // nur Einstiegsmarken
				continue
			}
			ref, zeit := u16(b, m+2), u32(b, m+4)
			if ref >= len(pl.items) {
				continue
			}
			var vorher int64
			for _, it := range pl.items[:ref] {
				vorher += (it.aus - it.ein) / 45
			}
			if t := vorher + (zeit-pl.items[ref].ein)/45; t >= 0 {
				pl.marken = append(pl.marken, t)
			}
		}
	}
	return pl, nil
}

// stnLesen: die Spuren einer STN-Tabelle — Hauptbild, Ton, Bild-Untertitel (PGS).
func stnLesen(b []byte) []Spur {
	laenge := u16(b, 0)
	if laenge+2 > len(b) || len(b) < 16 {
		return nil
	}
	b = b[:laenge+2]
	nV, nA, nPG, nIG := int(b[4]), int(b[5]), int(b[6]), int(b[7])
	nA2, nV2 := int(b[8]), int(b[9])
	p := 16
	var out []Spur
	lies := func(art string, nehmen bool) bool {
		if p+1 > len(b) {
			return false
		}
		el := int(b[p])
		e := p + 1
		if e+el > len(b) {
			return false
		}
		pid := 0
		switch b[e] {
		case 1:
			pid = u16(b, e+1)
		case 2, 4:
			pid = u16(b, e+3)
		case 3:
			pid = u16(b, e+2)
		}
		p = e + el
		if p+1 > len(b) {
			return false
		}
		al := int(b[p])
		a := p + 1
		if a+al > len(b) {
			return false
		}
		spr := ""
		if al >= 1 {
			switch c := b[a]; {
			case c == 0x90 || c == 0x91: // PGS, IG
				if al >= 4 {
					spr = string(b[a+1 : a+4])
				}
			case c == 0x92: // Text-Untertitel
				if al >= 5 {
					spr = string(b[a+2 : a+5])
				}
			case art == "a":
				if al >= 5 {
					spr = string(b[a+2 : a+5])
				}
			}
		}
		p = a + al
		if nehmen && pid > 0 {
			out = append(out, Spur{Art: art, ID: pid, Sprache: sprachePruefen(spr)})
		}
		return true
	}
	for i := 0; i < nV; i++ {
		if !lies("v", i == 0) {
			return out
		}
	}
	for i := 0; i < nA; i++ {
		if !lies("a", true) {
			return out
		}
	}
	for i := 0; i < nPG; i++ {
		if !lies("s", true) {
			return out
		}
	}
	_, _, _ = nIG, nA2, nV2
	return out
}

// PlanBluRay bestimmt den Hauptfilm eines BDMV-Ordners. stream sind die
// Dateien in BDMV/STREAM (bei einer ISO aus der Liste von 7-Zip), die
// Playlists liegen schon lesbar in bdmv/PLAYLIST.
func PlanBluRay(bdmv string, stream []Datei) (*Plan, error) {
	streamOrdner := filepath.Join(bdmv, "STREAM")
	if d, ok := Unterordner(bdmv, "STREAM"); ok {
		streamOrdner = d
	}
	clip := func(name string) (Datei, bool) {
		for _, endung := range []string{".m2ts", ".mts"} {
			if d, ok := finden(stream, name+endung); ok {
				return d, true
			}
		}
		return Datei{}, false
	}
	var bester *playlist
	if pdir, ok := Unterordner(bdmv, "PLAYLIST"); ok {
		eintraege, _ := os.ReadDir(pdir)
		for _, e := range eintraege {
			if !strings.EqualFold(filepath.Ext(e.Name()), ".mpls") {
				continue
			}
			b, err := os.ReadFile(filepath.Join(pdir, e.Name()))
			if err != nil {
				continue
			}
			pl, err := mplsLesen(b)
			if err != nil || len(pl.items) == 0 {
				continue
			}
			pl.name = e.Name()
			doppelt, fehlt := false, false
			gesehen := map[string]bool{}
			for _, it := range pl.items {
				if gesehen[it.clip] {
					doppelt = true
				}
				gesehen[it.clip] = true
				if _, ok := clip(it.clip); !ok {
					fehlt = true
				}
			}
			if doppelt || fehlt {
				continue
			}
			if bester == nil || pl.ms > bester.ms || (pl.ms == bester.ms && pl.name < bester.name) {
				bester = pl
			}
		}
	}
	p := &Plan{Art: BluRay, Ordner: streamOrdner}
	if bester != nil && bester.ms > 0 {
		for _, it := range bester.items {
			d, _ := clip(it.clip)
			p.Abschnitte = append(p.Abschnitte, Abschnitt{Datei: d.Name, Bis: d.Groesse})
		}
		p.DauerMS = bester.ms
		p.Spuren = bester.spuren
		marken := append([]int64(nil), bester.marken...)
		sort.Slice(marken, func(i, j int) bool { return marken[i] < marken[j] })
		for _, m := range marken {
			if m < p.DauerMS && (len(p.Kapitel) == 0 || m-p.Kapitel[len(p.Kapitel)-1] >= 1000) {
				p.Kapitel = append(p.Kapitel, m)
			}
		}
		if len(p.Kapitel) > 0 && p.Kapitel[0] != 0 {
			p.Kapitel = append([]int64{0}, p.Kapitel...)
		}
		if len(p.Kapitel) < 2 {
			p.Kapitel = nil
		}
		return p, nil
	}
	// Ausweg: die grösste Datei.
	var groesste Datei
	for _, d := range stream {
		e := strings.ToLower(filepath.Ext(d.Name))
		if (e == ".m2ts" || e == ".mts") && (d.Groesse > groesste.Groesse ||
			(d.Groesse == groesste.Groesse && d.Name < groesste.Name)) {
			groesste = d
		}
	}
	if groesste.Name == "" {
		return nil, ErrKeinFilm
	}
	p.Ausweg = true
	p.Abschnitte = []Abschnitt{{Datei: groesste.Name, Bis: groesste.Groesse}}
	return p, nil
}
