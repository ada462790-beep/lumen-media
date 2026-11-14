// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package scheibe

// DVD-Video: Hauptfilm, Zellen, Spuren und Kapitel aus den IFO-Dateien.
//
// Aufbau (DVD-Video-Spezifikation, wie libdvdread sie liest):
//
//   - VIDEO_TS.IFO, Tabelle TT_SRPT: je Titel der Titelsatz (VTS) und die
//     Nummer des Titels darin.
//   - VTS_nn_0.IFO: die Spuren des Titelsatzes (Ton- und
//     Untertitelsprachen), je Titel die Kapitel (PTT: Programmkette und
//     Programm) und die Programmketten (PGC) mit ihren Zellen. Eine Zelle
//     nennt ihre Sektoren in VTS_nn_1.VOB, VTS_nn_2.VOB … (aneinander
//     gehängt; ein Sektor sind 2048 Bytes) und ihre Dauer.
//
// Der Hauptfilm ist der längste Titel. Gelesen werden genau seine Zellen —
// nicht der ganze Titelsatz, in dem oft noch Extras oder ein zweiter
// Titel stecken.

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
)

const sektor = 2048

var reVOB = regexp.MustCompile(`(?i)^vts_(\d{2})_(\d{1,2})\.vob$`)

// dvdTitel ist ein Eintrag aus VIDEO_TS.IFO.
type dvdTitel struct {
	nr, vts, ttn int
}

// zelle einer Programmkette.
type zelle struct {
	von, bis int64 // Sektoren, bis einschliesslich
	ms       int64
}

// pgc: eine Programmkette — die Zellen (nur der erste Blickwinkel) und
// je Programm die erste Zelle.
type pgc struct {
	zellen   []zelle
	programm []int // Programm → Index in zellen (nach dem Weglassen)
	ton      []int // logische Tonspur → Strom-Nummer (-1: nicht da)
	unter    [][4]int
	ms       int64
}

// vts ist ein gelesener Titelsatz.
type vts struct {
	breit    bool
	tonArt   []int    // je Tonspur die Codierung (0 AC3, 2/3 MPEG, 4 LPCM, 6 DTS)
	tonSpr   []string // je Tonspur die Sprache (ISO 639-1)
	unterSpr []string
	ptt      [][][2]int // je Titel: Kapitel → (PGC-Nr., Programm-Nr.), 1-basiert
	pgcs     []*pgc
}

func u16(b []byte, o int) int {
	if o+2 > len(b) {
		return 0
	}
	return int(binary.BigEndian.Uint16(b[o:]))
}

func u32(b []byte, o int) int64 {
	if o+4 > len(b) {
		return 0
	}
	return int64(binary.BigEndian.Uint32(b[o:]))
}

// bcdZeit: Dauer im Format der DVD (Stunde, Minute, Sekunde, Bild als BCD;
// die oberen zwei Bits des Bilds nennen die Bildrate).
func bcdZeit(b []byte) int64 {
	if len(b) < 4 {
		return 0
	}
	bcd := func(x byte) int64 { return int64(x>>4)*10 + int64(x&0x0f) }
	ms := bcd(b[0])*3600000 + bcd(b[1])*60000 + bcd(b[2])*1000
	bilder := bcd(b[3] & 0x3f)
	switch b[3] >> 6 {
	case 1:
		ms += bilder * 40 // 25 Bilder/s
	case 3:
		ms += bilder * 1000 / 30 // 29,97 Bilder/s
	}
	return ms
}

// titelLesen liest die Titelliste aus VIDEO_TS.IFO.
func titelLesen(b []byte) ([]dvdTitel, error) {
	if len(b) < 0x100 || string(b[:12]) != "DVDVIDEO-VMG" {
		return nil, errors.New("VIDEO_TS.IFO ist keine DVD-Steuerdatei")
	}
	o := int(u32(b, 0xC4)) * sektor
	n := u16(b, o)
	if o == 0 || n == 0 || o+8+n*12 > len(b) {
		return nil, errors.New("VIDEO_TS.IFO: keine Titelliste")
	}
	out := make([]dvdTitel, 0, n)
	for i := 0; i < n; i++ {
		e := b[o+8+i*12:]
		out = append(out, dvdTitel{nr: i + 1, vts: int(e[6]), ttn: int(e[7])})
	}
	return out, nil
}

// vtsLesen liest VTS_nn_0.IFO.
func vtsLesen(b []byte) (*vts, error) {
	if len(b) < 0x400 || string(b[:12]) != "DVDVIDEO-VTS" {
		return nil, errors.New("keine Titelsatz-Steuerdatei")
	}
	v := &vts{breit: (b[0x200]>>2)&3 == 3}
	nTon := u16(b, 0x202)
	if nTon > 8 {
		nTon = 8
	}
	for i := 0; i < nTon; i++ {
		a := b[0x204+i*8:]
		spr := ""
		if (a[0]>>2)&3 == 1 {
			spr = string(a[2:4])
		}
		v.tonArt = append(v.tonArt, int(a[0]>>5))
		v.tonSpr = append(v.tonSpr, spr)
	}
	nUnter := u16(b, 0x254)
	if nUnter > 32 {
		nUnter = 32
	}
	for i := 0; i < nUnter; i++ {
		a := b[0x256+i*6:]
		spr := ""
		if a[0]&3 == 1 {
			spr = string(a[2:4])
		}
		v.unterSpr = append(v.unterSpr, spr)
	}

	// Kapitel je Titel.
	po := int(u32(b, 0xC8)) * sektor
	if po > 0 && po+8 <= len(b) {
		n := u16(b, po)
		ende := po + int(u32(b, po+4)) + 1
		if ende > len(b) {
			ende = len(b)
		}
		for i := 0; i < n && po+8+i*4+4 <= len(b); i++ {
			von := po + int(u32(b, po+8+i*4))
			bis := ende
			if i+1 < n {
				bis = po + int(u32(b, po+8+(i+1)*4))
			}
			var kap [][2]int
			for o := von; o+4 <= bis && o+4 <= len(b); o += 4 {
				kap = append(kap, [2]int{u16(b, o), u16(b, o+2)})
			}
			v.ptt = append(v.ptt, kap)
		}
	}

	// Programmketten.
	co := int(u32(b, 0xCC)) * sektor
	if co == 0 || co+8 > len(b) {
		return nil, errors.New("keine Programmketten")
	}
	n := u16(b, co)
	for i := 0; i < n && co+8+i*8+8 <= len(b); i++ {
		o := co + int(u32(b, co+8+i*8+4))
		p, err := pgcLesen(b, o)
		if err != nil {
			return nil, err
		}
		v.pgcs = append(v.pgcs, p)
	}
	return v, nil
}

func pgcLesen(b []byte, o int) (*pgc, error) {
	if o+0xEC > len(b) {
		return nil, errors.New("programmkette ausserhalb der datei")
	}
	p := &pgc{ms: bcdZeit(b[o+4:])}
	nProg, nZellen := int(b[o+2]), int(b[o+3])
	for i := 0; i < 8; i++ {
		c := u16(b, o+0x0C+i*2)
		if c&0x8000 != 0 {
			p.ton = append(p.ton, (c>>8)&7)
		} else {
			p.ton = append(p.ton, -1)
		}
	}
	for i := 0; i < 32; i++ {
		e := b[o+0x1C+i*4:]
		if e[0]&0x80 != 0 {
			p.unter = append(p.unter, [4]int{int(e[0] & 0x1f), int(e[1] & 0x1f), int(e[2] & 0x1f), int(e[3] & 0x1f)})
		} else {
			p.unter = append(p.unter, [4]int{-1, -1, -1, -1})
		}
	}
	progOff, zellOff := u16(b, o+0xE4), u16(b, o+0xE6)
	if nZellen == 0 || zellOff == 0 {
		return p, nil // eine Kette ohne Zellen (nur Befehle)
	}
	// Zelle (1-basiert, wie in der Datei) → Index nach dem Weglassen der
	// weiteren Blickwinkel.
	index := make([]int, nZellen+2)
	for i := 0; i < nZellen; i++ {
		z := o + zellOff + i*24
		if z+24 > len(b) {
			return nil, errors.New("zelle ausserhalb der datei")
		}
		index[i+1] = len(p.zellen)
		art, modus := (b[z]>>4)&3, b[z]>>6
		if art == 1 && modus != 1 {
			continue // Blickwinkel 2, 3 …: nur der erste zählt
		}
		zl := zelle{von: u32(b, z+8), bis: u32(b, z+20), ms: bcdZeit(b[z+4:])}
		if zl.bis < zl.von {
			return nil, errors.New("zelle mit verkehrten sektoren")
		}
		p.zellen = append(p.zellen, zl)
	}
	index[nZellen+1] = len(p.zellen)
	if progOff > 0 {
		for i := 0; i < nProg && o+progOff+i < len(b); i++ {
			c := int(b[o+progOff+i])
			if c < 1 || c > nZellen {
				c = 1
			}
			p.programm = append(p.programm, index[c])
		}
	}
	return p, nil
}

// PlanDVD bestimmt den Hauptfilm eines VIDEO_TS-Ordners. dateien sind die
// Dateien darin mit Grösse (bei einer ISO aus der Liste von 7-Zip — die
// VOBs müssen dann noch nicht ausgepackt sein, die IFOs schon).
func PlanDVD(dir string, dateien []Datei) (*Plan, error) {
	if p, err := dvdAusIFO(dir, dateien); err == nil {
		return p, nil
	}
	// Ohne lesbare IFO-Dateien wie der Scan: der Titelsatz mit den
	// meisten Bytes, alle seine Teile ab 1.
	return dvdAusweg(dir, dateien)
}

// vobsVon: die Teile 1, 2 … eines Titelsatzes in ihrer Reihenfolge.
func vobsVon(dateien []Datei, nr int) []Datei {
	type teil struct {
		k int
		d Datei
	}
	var t []teil
	for _, d := range dateien {
		m := reVOB.FindStringSubmatch(d.Name)
		if m == nil {
			continue
		}
		n, _ := strconv.Atoi(m[1])
		k, _ := strconv.Atoi(m[2])
		if n == nr && k >= 1 {
			t = append(t, teil{k, d})
		}
	}
	sort.Slice(t, func(i, j int) bool { return t[i].k < t[j].k })
	out := make([]Datei, len(t))
	for i, x := range t {
		out[i] = x.d
	}
	return out
}

func dvdAusIFO(dir string, dateien []Datei) (*Plan, error) {
	vmg, ok := finden(dateien, "VIDEO_TS.IFO")
	if !ok {
		return nil, errors.New("VIDEO_TS.IFO fehlt")
	}
	b, err := os.ReadFile(filepath.Join(dir, vmg.Name))
	if err != nil {
		return nil, err
	}
	titel, err := titelLesen(b)
	if err != nil {
		return nil, err
	}
	saetze := map[int]*vts{}
	var bester *Plan
	for _, t := range titel {
		v, ok := saetze[t.vts]
		if !ok {
			v = nil
			if d, gefunden := finden(dateien, fmt.Sprintf("VTS_%02d_0.IFO", t.vts)); gefunden {
				if b, err := os.ReadFile(filepath.Join(dir, d.Name)); err == nil {
					v, _ = vtsLesen(b)
				}
			}
			saetze[t.vts] = v
		}
		if v == nil {
			continue
		}
		p, err := titelPlan(dir, dateien, t, v)
		if err != nil {
			continue
		}
		// Der längste Titel; bei gleicher Länge der mit mehr Bytes, dann
		// der erste (eine „Schutz"-DVD hat Dutzende gleich langer Titel,
		// der echte steht meist vorne).
		if bester == nil || p.DauerMS > bester.DauerMS ||
			(p.DauerMS == bester.DauerMS && p.Gesamt() > bester.Gesamt()) {
			bester = p
		}
	}
	if bester == nil {
		return nil, ErrKeinFilm
	}
	return bester, nil
}

// titelPlan: Zellen, Kapitel und Spuren eines Titels.
func titelPlan(dir string, dateien []Datei, t dvdTitel, v *vts) (*Plan, error) {
	if t.ttn < 1 || t.ttn > len(v.ptt) || len(v.ptt[t.ttn-1]) == 0 {
		return nil, errors.New("titel ohne kapitel")
	}
	kap := v.ptt[t.ttn-1]
	// Die Programmketten des Titels, in der Reihenfolge der Kapitel.
	var ketten []int
	for _, k := range kap {
		if len(ketten) == 0 || ketten[len(ketten)-1] != k[0] {
			ketten = append(ketten, k[0])
		}
	}
	vobs := vobsVon(dateien, t.vts)
	if len(vobs) == 0 {
		return nil, errors.New("titelsatz ohne vob")
	}
	var gesamt int64
	for _, d := range vobs {
		gesamt += d.Groesse
	}
	p := &Plan{Art: DVD, Ordner: dir}
	beginn := map[int]int64{} // PGC → Beginn im Film (ms)
	zellenBeginn := map[int][]int64{}
	var ms int64
	for _, nr := range ketten {
		if nr < 1 || nr > len(v.pgcs) {
			return nil, errors.New("kapitel zeigt auf eine fehlende programmkette")
		}
		if _, schon := beginn[nr]; schon {
			continue // dieselbe Kette zweimal: eine Schleife, nicht zweimal lesen
		}
		g := v.pgcs[nr-1]
		beginn[nr] = ms
		var zb []int64
		for _, z := range g.zellen {
			zb = append(zb, ms)
			von, bis := z.von*sektor, (z.bis+1)*sektor
			if bis > gesamt {
				return nil, errors.New("zelle hinter dem ende der vob-dateien")
			}
			if err := abschnitteAnhaengen(p, vobs, von, bis); err != nil {
				return nil, err
			}
			if z.ms > 0 {
				ms += z.ms
			}
		}
		zellenBeginn[nr] = zb
		// Manche Ketten nennen Zellen ohne Dauer; dann gilt die der Kette.
		if ms-beginn[nr] == 0 {
			ms += g.ms
		}
	}
	p.DauerMS = ms
	if len(p.Abschnitte) == 0 || ms <= 0 {
		return nil, errors.New("titel ohne inhalt")
	}
	// Kapitel: Beginn des Programms in seiner Kette.
	gesehen := map[int64]bool{}
	for _, k := range kap {
		g := v.pgcs[k[0]-1]
		if k[1] < 1 || k[1] > len(g.programm) {
			continue
		}
		zi := g.programm[k[1]-1]
		zb := zellenBeginn[k[0]]
		if zi >= len(zb) {
			continue
		}
		if t := zb[zi]; !gesehen[t] {
			gesehen[t] = true
			p.Kapitel = append(p.Kapitel, t)
		}
	}
	sort.Slice(p.Kapitel, func(i, j int) bool { return p.Kapitel[i] < p.Kapitel[j] })
	if len(p.Kapitel) < 2 {
		p.Kapitel = nil
	}
	// Spuren: die der ersten Kette (die übrigen gleichen ihr).
	g := v.pgcs[ketten[0]-1]
	p.Spuren = append(p.Spuren, Spur{Art: "v", ID: 0x1e0})
	for i, strom := range g.ton {
		if strom < 0 || i >= len(v.tonArt) {
			continue
		}
		id := 0
		switch v.tonArt[i] {
		case 0:
			id = 0x80 + strom // AC-3
		case 2, 3:
			id = 0x1c0 + strom // MPEG-Audio
		case 4:
			id = 0xa0 + strom // LPCM
		case 6:
			id = 0x88 + strom // DTS
		default:
			continue
		}
		p.Spuren = append(p.Spuren, Spur{Art: "a", ID: id, Sprache: sprache3(v.tonSpr[i])})
	}
	gesehenU := map[int]bool{}
	for i, u := range g.unter {
		if i >= len(v.unterSpr) || u[0] < 0 {
			continue
		}
		// Welcher Strom gilt, hängt vom Bildformat ab: 4:3 der erste
		// Eintrag, 16:9 der zweite („breit").
		strom := u[0]
		if v.breit {
			strom = u[1]
		}
		if gesehenU[strom] {
			continue
		}
		gesehenU[strom] = true
		p.Spuren = append(p.Spuren, Spur{Art: "s", ID: 0x20 + strom, Sprache: sprache3(v.unterSpr[i])})
	}
	return p, nil
}

// abschnitteAnhaengen verteilt den Bereich [von, bis) der aneinander
// gehängten VOB-Dateien auf die einzelnen Dateien; schliesst er an den
// letzten an, wird dieser länger.
func abschnitteAnhaengen(p *Plan, vobs []Datei, von, bis int64) error {
	var start int64
	for _, d := range vobs {
		ende := start + d.Groesse
		if von < ende && bis > start {
			a := Abschnitt{Datei: d.Name, Von: max(von, start) - start, Bis: min(bis, ende) - start}
			if n := len(p.Abschnitte); n > 0 && p.Abschnitte[n-1].Datei == a.Datei && p.Abschnitte[n-1].Bis == a.Von {
				p.Abschnitte[n-1].Bis = a.Bis
			} else {
				p.Abschnitte = append(p.Abschnitte, a)
			}
		}
		start = ende
	}
	if bis > start {
		return errors.New("bereich hinter dem ende")
	}
	return nil
}

func dvdAusweg(dir string, dateien []Datei) (*Plan, error) {
	saetze := map[int]int64{}
	for _, d := range dateien {
		if m := reVOB.FindStringSubmatch(d.Name); m != nil && m[2] != "0" {
			n, _ := strconv.Atoi(m[1])
			saetze[n] += d.Groesse
		}
	}
	bester := -1
	for n, g := range saetze {
		if bester < 0 || g > saetze[bester] || (g == saetze[bester] && n < bester) {
			bester = n
		}
	}
	if bester < 0 {
		return nil, ErrKeinFilm
	}
	p := &Plan{Art: DVD, Ordner: dir, Ausweg: true}
	for _, d := range vobsVon(dateien, bester) {
		p.Abschnitte = append(p.Abschnitte, Abschnitt{Datei: d.Name, Von: 0, Bis: d.Groesse})
	}
	return p, nil
}
