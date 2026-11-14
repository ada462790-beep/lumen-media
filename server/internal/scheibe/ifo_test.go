// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package scheibe

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// Hilfen, die IFO-Dateien nach der DVD-Spezifikation bauen — so, wie ein
// Autorenprogramm sie schreibt (nur die Felder, die Lumen liest).

type testZelle struct {
	von, bis int64 // Sektoren
	sek      int   // Dauer in Sekunden
	winkel   byte  // 0 normal, sonst Blockmodus (1 erste, 2 mittlere, 3 letzte) im Winkelblock
}

type testPGC struct {
	zellen   []testZelle
	programm []int // erste Zelle je Programm (1-basiert)
	ton      []int // Strom-Nummer je logischer Tonspur
	unter    [][2]int
}

func bcd(n int) byte { return byte(n/10<<4 | n%10) }

func zeitBCD(sek int) []byte {
	return []byte{bcd(sek / 3600), bcd(sek / 60 % 60), bcd(sek % 60), 0x40} // 25 Bilder/s
}

func vmgBauen(titel [][2]int) []byte { // je Titel: VTS, TTN
	b := make([]byte, 2*sektor)
	copy(b, "DVDVIDEO-VMG")
	binary.BigEndian.PutUint32(b[0xC4:], 1)
	t := b[sektor:]
	binary.BigEndian.PutUint16(t, uint16(len(titel)))
	binary.BigEndian.PutUint32(t[4:], uint32(8+len(titel)*12-1))
	for i, x := range titel {
		e := t[8+i*12:]
		e[1] = 1
		binary.BigEndian.PutUint16(e[2:], 1)
		e[6], e[7] = byte(x[0]), byte(x[1])
	}
	return b
}

// vtsBauen: ttus je Titel die Kapitel (PGC, Programm).
func vtsBauen(breit bool, ton []string, unter []string, ttus [][][2]int, pgcs []testPGC) []byte {
	b := make([]byte, 3*sektor)
	copy(b, "DVDVIDEO-VTS")
	binary.BigEndian.PutUint32(b[0xC8:], 1)
	binary.BigEndian.PutUint32(b[0xCC:], 2)
	if breit {
		b[0x200] = 3 << 2
	}
	binary.BigEndian.PutUint16(b[0x202:], uint16(len(ton)))
	for i, s := range ton {
		a := b[0x204+i*8:]
		a[0] = 0<<5 | 1<<2 // AC-3, mit Sprache
		copy(a[2:4], s)
	}
	binary.BigEndian.PutUint16(b[0x254:], uint16(len(unter)))
	for i, s := range unter {
		a := b[0x256+i*6:]
		a[0] = 1
		copy(a[2:4], s)
	}
	// PTT_SRPT
	p := b[sektor:]
	binary.BigEndian.PutUint16(p, uint16(len(ttus)))
	o := 8 + 4*len(ttus)
	for i, kap := range ttus {
		binary.BigEndian.PutUint32(p[8+i*4:], uint32(o))
		for _, k := range kap {
			binary.BigEndian.PutUint16(p[o:], uint16(k[0]))
			binary.BigEndian.PutUint16(p[o+2:], uint16(k[1]))
			o += 4
		}
	}
	binary.BigEndian.PutUint32(p[4:], uint32(o-1))
	// PGCI
	c := b[2*sektor:]
	binary.BigEndian.PutUint16(c, uint16(len(pgcs)))
	o = 8 + 8*len(pgcs)
	for i, g := range pgcs {
		c[8+i*8] = 0x80 | byte(i+1)
		binary.BigEndian.PutUint32(c[8+i*8+4:], uint32(o))
		x := c[o:]
		x[2], x[3] = byte(len(g.programm)), byte(len(g.zellen))
		gesamt := 0
		for _, z := range g.zellen {
			if z.winkel <= 1 {
				gesamt += z.sek
			}
		}
		copy(x[4:], zeitBCD(gesamt))
		for i, s := range g.ton {
			binary.BigEndian.PutUint16(x[0x0C+i*2:], uint16(0x8000|s<<8))
		}
		for i, u := range g.unter {
			x[0x1C+i*4] = 0x80 | byte(u[0])
			x[0x1C+i*4+1] = byte(u[1])
		}
		progOff := 0xEC
		zellOff := progOff + len(g.programm)
		zellOff += zellOff % 2
		binary.BigEndian.PutUint16(x[0xE4:], uint16(progOff))
		binary.BigEndian.PutUint16(x[0xE6:], uint16(zellOff))
		for i, pr := range g.programm {
			x[progOff+i] = byte(pr)
		}
		for i, z := range g.zellen {
			e := x[zellOff+i*24:]
			if z.winkel > 0 {
				e[0] = z.winkel<<6 | 1<<4
			}
			copy(e[4:], zeitBCD(z.sek))
			binary.BigEndian.PutUint32(e[8:], uint32(z.von))
			binary.BigEndian.PutUint32(e[20:], uint32(z.bis))
		}
		o += zellOff + len(g.zellen)*24 + 16
	}
	binary.BigEndian.PutUint32(c[4:], uint32(o-1))
	return b
}

func schreiben(t *testing.T, dir, name string, b []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func leereDatei(t *testing.T, dir, name string, groesse int64) {
	t.Helper()
	f, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(groesse); err != nil {
		t.Fatal(err)
	}
	f.Close()
}

// Eine DVD mit Menü, einem kurzen Titel (Titelsatz 1) und dem Hauptfilm
// in Titelsatz 2 — dort gefolgt von einem Extra, das nicht mit soll. Der
// Hauptfilm geht über zwei VOB-Dateien.
func TestPlanDVDHauptfilm(t *testing.T) {
	dir := t.TempDir()
	schreiben(t, dir, "VIDEO_TS.IFO", vmgBauen([][2]int{{1, 1}, {2, 1}, {2, 2}}))
	leereDatei(t, dir, "VIDEO_TS.VOB", 50*sektor)
	schreiben(t, dir, "VTS_01_0.IFO", vtsBauen(false, []string{"de"}, nil,
		[][][2]int{{{1, 1}}},
		[]testPGC{{zellen: []testZelle{{0, 99, 60, 0}}, programm: []int{1}, ton: []int{0}}}))
	leereDatei(t, dir, "VTS_01_0.VOB", 30*sektor)
	leereDatei(t, dir, "VTS_01_1.VOB", 100*sektor)
	// Titelsatz 2: Hauptfilm Sektoren 0–1499 (drei Kapitel), Extra 1500–1599.
	schreiben(t, dir, "vts_02_0.ifo", vtsBauen(true, []string{"de", "en"}, []string{"de", "fr"},
		[][][2]int{{{1, 1}, {1, 2}, {1, 3}}, {{2, 1}}},
		[]testPGC{
			{zellen: []testZelle{{0, 499, 600, 0}, {500, 999, 700, 0}, {1000, 1499, 800, 0}},
				programm: []int{1, 2, 3}, ton: []int{0, 1}, unter: [][2]int{{0, 2}, {1, 3}}},
			{zellen: []testZelle{{1500, 1599, 120, 0}}, programm: []int{1}, ton: []int{0}},
		}))
	leereDatei(t, dir, "VTS_02_0.VOB", 20*sektor)
	leereDatei(t, dir, "VTS_02_1.VOB", 1000*sektor)
	leereDatei(t, dir, "VTS_02_2.VOB", 600*sektor)

	dateien, err := OrdnerLesen(dir)
	if err != nil {
		t.Fatal(err)
	}
	p, err := PlanDVD(dir, dateien)
	if err != nil {
		t.Fatal(err)
	}
	if p.Ausweg {
		t.Fatal("Ausweg statt IFO")
	}
	if p.DauerMS != 2100*1000 {
		t.Errorf("Dauer %d, erwartet 2100 s", p.DauerMS)
	}
	want := []Abschnitt{{"VTS_02_1.VOB", 0, 1000 * sektor}, {"VTS_02_2.VOB", 0, 500 * sektor}}
	if len(p.Abschnitte) != len(want) {
		t.Fatalf("Abschnitte %+v", p.Abschnitte)
	}
	for i := range want {
		if p.Abschnitte[i] != want[i] {
			t.Errorf("Abschnitt %d: %+v, erwartet %+v", i, p.Abschnitte[i], want[i])
		}
	}
	if got := p.Kapitel; len(got) != 3 || got[0] != 0 || got[1] != 600000 || got[2] != 1300000 {
		t.Errorf("Kapitel %v", got)
	}
	// Ton 0x80 (de), 0x81 (en); Untertitel im Breitbild: Ströme 2 und 3.
	wantSp := []Spur{{"v", 0x1e0, ""}, {"a", 0x80, "ger"}, {"a", 0x81, "eng"}, {"s", 0x22, "ger"}, {"s", 0x23, "fre"}}
	if len(p.Spuren) != len(wantSp) {
		t.Fatalf("Spuren %+v", p.Spuren)
	}
	for i := range wantSp {
		if p.Spuren[i] != wantSp[i] {
			t.Errorf("Spur %d: %+v, erwartet %+v", i, p.Spuren[i], wantSp[i])
		}
	}
	for _, f := range p.Dateien() {
		if f == "VTS_02_0.VOB" || f == "VIDEO_TS.VOB" || f == "VTS_01_1.VOB" {
			t.Errorf("Menü oder falscher Titel gelesen: %s", f)
		}
	}
}

// Blickwinkel: Nur der erste Blickwinkel eines Winkelblocks wird gelesen.
func TestPlanDVDBlickwinkel(t *testing.T) {
	dir := t.TempDir()
	schreiben(t, dir, "VIDEO_TS.IFO", vmgBauen([][2]int{{1, 1}}))
	schreiben(t, dir, "VTS_01_0.IFO", vtsBauen(false, nil, nil, [][][2]int{{{1, 1}}},
		[]testPGC{{zellen: []testZelle{{0, 9, 10, 0}, {10, 19, 5, 1}, {20, 29, 5, 3}, {30, 39, 10, 0}},
			programm: []int{1}}}))
	leereDatei(t, dir, "VTS_01_1.VOB", 40*sektor)
	dateien, _ := OrdnerLesen(dir)
	p, err := PlanDVD(dir, dateien)
	if err != nil {
		t.Fatal(err)
	}
	if p.Gesamt() != 30*sektor || p.DauerMS != 25000 {
		t.Errorf("Gesamt %d, Dauer %d", p.Gesamt(), p.DauerMS)
	}
	if len(p.Abschnitte) != 2 || p.Abschnitte[1].Von != 30*sektor {
		t.Errorf("Abschnitte %+v", p.Abschnitte)
	}
}

// Ohne IFO: der grösste Titelsatz, ohne Menüteil, wie der Scan.
func TestPlanDVDAusweg(t *testing.T) {
	dir := t.TempDir()
	leereDatei(t, dir, "VIDEO_TS.VOB", 500*sektor)
	leereDatei(t, dir, "VTS_01_0.VOB", 900*sektor)
	leereDatei(t, dir, "VTS_01_1.VOB", 100*sektor)
	leereDatei(t, dir, "VTS_02_1.VOB", 300*sektor)
	leereDatei(t, dir, "VTS_02_2.VOB", 200*sektor)
	leereDatei(t, dir, "VTS_02_10.VOB", 10*sektor)
	dateien, _ := OrdnerLesen(dir)
	p, err := PlanDVD(dir, dateien)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Ausweg || len(p.Abschnitte) != 3 || p.Abschnitte[0].Datei != "VTS_02_1.VOB" ||
		p.Abschnitte[1].Datei != "VTS_02_2.VOB" || p.Abschnitte[2].Datei != "VTS_02_10.VOB" {
		t.Errorf("Plan %+v", p)
	}
}

func TestPlanDVDLeer(t *testing.T) {
	dir := t.TempDir()
	leereDatei(t, dir, "VIDEO_TS.VOB", 5*sektor)
	dateien, _ := OrdnerLesen(dir)
	if _, err := PlanDVD(dir, dateien); err == nil {
		t.Fatal("ohne Titel kein Plan erwartet")
	}
}

func TestLeserAbschnitte(t *testing.T) {
	dir := t.TempDir()
	schreiben(t, dir, "a", []byte("0123456789"))
	schreiben(t, dir, "b", []byte("abcdefghij"))
	p := &Plan{Ordner: dir, Abschnitte: []Abschnitt{{"a", 2, 5}, {"b", 0, 3}, {"a", 8, 10}}}
	l := NeuerLeser(t.Context(), p)
	defer l.Close()
	b, err := readAll(l)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "234abc89" || l.Gelesen.Load() != 8 {
		t.Errorf("gelesen %q (%d)", b, l.Gelesen.Load())
	}
}

func readAll(l *Leser) ([]byte, error) {
	var out []byte
	buf := make([]byte, 3)
	for {
		n, err := l.Read(buf)
		out = append(out, buf[:n]...)
		if err != nil {
			if err.Error() == "EOF" {
				return out, nil
			}
			return out, err
		}
	}
}

func TestSprachen(t *testing.T) {
	for in, want := range map[string]string{"de": "ger", "EN": "eng", "xx": "", "": ""} {
		if got := sprache3(in); got != want {
			t.Errorf("sprache3(%q) = %q", in, got)
		}
	}
	for in, want := range map[string]string{"deu": "ger", "eng": "eng", "und": "", "d1": "", "fra": "fre"} {
		if got := sprachePruefen(in); got != want {
			t.Errorf("sprachePruefen(%q) = %q", in, got)
		}
	}
}
