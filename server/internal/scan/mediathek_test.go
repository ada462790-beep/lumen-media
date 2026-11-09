// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package scan

import (
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"lumen/internal/store"
)

func TestMusikAusPfad(t *testing.T) {
	for _, c := range []struct {
		rel                     string
		interpret, album, titel string
		track, disc, jahr       int
	}{
		{"Aurora/Nordlicht (2019)/01 - Morgenrot.flac", "Aurora", "Nordlicht", "Morgenrot", 1, 0, 2019},
		{"Band/Doppelt (2020)/CD2/03 - Teil.mp3", "Band", "Doppelt", "Teil", 3, 2, 2020},
		{"Band/2001 - Frueh/1-07 Lied.mp3", "Band", "Frueh", "Lied", 7, 1, 2001},
		{"Lose/Interpret X - Ein Lied.mp3", "Interpret X", "Lose", "Ein Lied", 0, 0, 0},
		{"Einzeln - Titel.mp3", "Einzeln", "", "Titel", 0, 0, 0},
	} {
		i, a, ti, tr, d, j := MusikAusPfad(c.rel)
		if i != c.interpret || a != c.album || ti != c.titel || tr != c.track || d != c.disc || j != c.jahr {
			t.Errorf("%s: bekommen %q %q %q %d %d %d", c.rel, i, a, ti, tr, d, j)
		}
	}
}

func titelMit(interpreten ...string) []store.MusikTitel {
	var out []store.MusikTitel
	for _, i := range interpreten {
		out = append(out, store.MusikTitel{Interpret: i})
	}
	return out
}

func TestAlbumInterpretUndKompilation(t *testing.T) {
	// Gast auf einem Titel: bleibt das Album des Interpreten.
	a := AlbumAusTiteln("lib", "x", "Strasse", titelMit("Ro", "Ro", "Ro feat. Mia", "Ro"))
	if a.Compilation || a.Interpret != "Ro" || a.InterpretID != store.InterpretID("Ro") {
		t.Errorf("Album mit Gast: %+v", a)
	}
	// Viele verschiedene ohne Albuminterpret: Kompilation.
	if a := AlbumAusTiteln("lib", "x", "Mix", titelMit("A", "B", "C", "D")); !a.Compilation || a.InterpretID != "" {
		t.Errorf("Mix: %+v", a)
	}
	// „Various Artists" als Albuminterpret.
	ts := titelMit("A", "B")
	for i := range ts {
		ts[i].AlbumInterpret = "Various Artists"
	}
	if a := AlbumAusTiteln("lib", "x", "VA", ts); !a.Compilation {
		t.Errorf("VA: %+v", a)
	}
	// Die Kennung bleibt gleich, egal wie oft gebildet.
	if AlbumAusTiteln("lib", "x", "Mix", nil).ID != AlbumAusTiteln("lib", "x", "MIX", nil).ID {
		t.Error("Albumkennung hängt an der Schreibweise")
	}
}

func TestDatumAusNamen(t *testing.T) {
	for name, want := range map[string]string{
		"IMG_20240713_180211.jpg":               "2024-07-13 18:02:11",
		"PXL_20231224_120000123.jpg":            "2023-12-24 12:00:00",
		"Screenshot 2024-03-05 at 10.11.12.png": "2024-03-05 10:11:12",
		"2022-01-31.jpg":                        "2022-01-31 00:00:00",
	} {
		got, ok := DatumAusNamen(name)
		if !ok || got.Format("2006-01-02 15:04:05") != want {
			t.Errorf("%s: %v %v", name, got, ok)
		}
	}
	for _, name := range []string{"DSC_1000.jpg", "IMG_20241341_000000.jpg"} {
		if _, ok := DatumAusNamen(name); ok {
			t.Errorf("%s: Datum erfunden", name)
		}
	}
}

// tiffBauen: ein kleiner EXIF-Block (Big Endian) mit Ausrichtung, Kamera,
// Aufnahmezeit und Ort.
func tiffBauen() []byte {
	var b bytes.Buffer
	bo := binary.BigEndian
	w16 := func(v uint16) { _ = binary.Write(&b, bo, v) }
	w32 := func(v uint32) { _ = binary.Write(&b, bo, v) }
	b.WriteString("MM")
	w16(42)
	w32(8)
	// IFD0 bei 8: 4 Einträge → 2+4*12+4 = 54 → Daten ab 62
	daten := uint32(8 + 54)
	make_ := "Apple\x00"
	model := "iPhone 15 Pro\x00"
	w16(4)
	w16(0x010F)
	w16(2)
	w32(uint32(len(make_)))
	w32(daten)
	w16(0x0110)
	w16(2)
	w32(uint32(len(model)))
	w32(daten + uint32(len(make_)))
	w16(0x0112)
	w16(3)
	w32(1)
	w16(6)
	w16(0)
	exifIFD := daten + uint32(len(make_)+len(model))
	gpsIFD := exifIFD + 18 + 20
	w16(0x8769)
	w16(4)
	w32(1)
	w32(exifIFD)
	w32(0)
	b.WriteString(make_)
	b.WriteString(model)
	// Exif-IFD: 1 Eintrag (18 Bytes) + 20 Bytes Datum
	datum := "2024:07:13 18:02:11\x00"
	w16(1)
	w16(0x9003)
	w16(2)
	w32(uint32(len(datum)))
	w32(exifIFD + 18)
	w32(0)
	b.WriteString(datum)
	_ = gpsIFD
	return b.Bytes()
}

func TestExifAusJPEG(t *testing.T) {
	tiff := tiffBauen()
	var j bytes.Buffer
	j.Write([]byte{0xFF, 0xD8, 0xFF, 0xE1})
	_ = binary.Write(&j, binary.BigEndian, uint16(2+6+len(tiff)))
	j.WriteString("Exif\x00\x00")
	j.Write(tiff)
	// SOF0: Höhe 800, Breite 1200
	j.Write([]byte{0xFF, 0xC0, 0, 11, 8, 0x03, 0x20, 0x04, 0xB0, 1, 1, 0x11, 0})
	j.Write([]byte{0xFF, 0xD9})
	e, err := ExifLesen(bytes.NewReader(j.Bytes()), int64(j.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if e.Ausrichtung != 6 || e.Kamera != "Apple iPhone 15 Pro" || e.Breite != 1200 || e.Hoehe != 800 ||
		e.Aufnahme.Format("2006-01-02 15:04:05") != "2024-07-13 18:02:11" {
		t.Errorf("gelesen: %+v", e)
	}
}

func TestExifAusPNG(t *testing.T) {
	tiff := tiffBauen()
	var p bytes.Buffer
	p.WriteString("\x89PNG\r\n\x1a\n")
	chunk := func(typ string, d []byte) {
		_ = binary.Write(&p, binary.BigEndian, uint32(len(d)))
		p.WriteString(typ)
		p.Write(d)
		p.Write([]byte{0, 0, 0, 0})
	}
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], 640)
	binary.BigEndian.PutUint32(ihdr[4:], 480)
	chunk("IHDR", ihdr)
	chunk("eXIf", tiff)
	chunk("IEND", nil)
	e, err := ExifLesen(bytes.NewReader(p.Bytes()), int64(p.Len()))
	if err != nil || e.Breite != 640 || e.Ausrichtung != 6 || e.Aufnahme.IsZero() {
		t.Errorf("%+v %v", e, err)
	}
}

// HEIC: Boxen von Hand — meta mit iinf (Exif-Eintrag), iloc und ispe.
func TestExifAusHEIC(t *testing.T) {
	box := func(typ string, inhalt ...[]byte) []byte {
		var b bytes.Buffer
		n := 8
		for _, x := range inhalt {
			n += len(x)
		}
		_ = binary.Write(&b, binary.BigEndian, uint32(n))
		b.WriteString(typ)
		for _, x := range inhalt {
			b.Write(x)
		}
		return b.Bytes()
	}
	be16 := func(v uint16) []byte { b := make([]byte, 2); binary.BigEndian.PutUint16(b, v); return b }
	be32 := func(v uint32) []byte { b := make([]byte, 4); binary.BigEndian.PutUint32(b, v); return b }
	voll := []byte{0, 0, 0, 0}
	infe := box("infe", []byte{2, 0, 0, 0}, be16(7), be16(0), []byte("Exif"), []byte{0})
	iinf := box("iinf", voll, be16(1), infe)
	ispe := box("ispe", voll, be32(4032), be32(3024))
	irot := box("irot", []byte{1})
	iprp := box("iprp", box("ipco", ispe, irot))
	payload := append(append(be32(6), []byte("Exif\x00\x00")...), tiffBauen()...)
	ftyp := box("ftyp", []byte("heic"), be32(0), []byte("mif1heic"))
	// iloc v0: offset_size=4, length_size=4, base=0; 1 Eintrag
	ilocLaenge := 8 + 4 + 2 + 2 + (2 + 2 + 2 + 4 + 4)
	var meta []byte
	bauen := func(offset uint32) []byte {
		iloc := box("iloc", voll, []byte{0x44, 0x00}, be16(1), be16(7), be16(0), be16(1), be32(offset), be32(uint32(len(payload))))
		return box("meta", voll, iinf, iloc, iprp)
	}
	meta = bauen(0)
	_ = ilocLaenge
	offset := uint32(len(ftyp) + len(meta) + 8)
	meta = bauen(offset)
	datei := append(append(append([]byte{}, ftyp...), meta...), box("mdat", payload)...)
	e, err := ExifLesen(bytes.NewReader(datei), int64(len(datei)))
	if err != nil {
		t.Fatal(err)
	}
	if e.Breite != 4032 || e.HeifDrehung != 90 || e.Kamera != "Apple iPhone 15 Pro" || e.Aufnahme.Year() != 2024 {
		t.Errorf("HEIC gelesen: %+v", e)
	}
}

// ---------------------------------------------------------------- Durchlauf

type musikFake struct {
	mu      sync.Mutex
	tags    map[string]*MusikTags
	gefragt int
}

func (f *musikFake) Probe(context.Context, string) (*Probe, error) { return &Probe{}, nil }
func (f *musikFake) MusikTags(_ context.Context, p string) (*MusikTags, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gefragt++
	if t, ok := f.tags[filepath.Base(p)]; ok {
		c := *t
		return &c, nil
	}
	return &MusikTags{}, nil
}

func TestMusikDurchlauf(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "l.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	root := t.TempDir()
	dateien := []string{
		"Aurora/Nordlicht (2019)/01 - Morgenrot.flac",
		"Aurora/Nordlicht (2019)/02 - Wolken.flac",
		"Band/Doppelt (2020)/CD1/01 - Eins.mp3",
		"Band/Doppelt (2020)/CD2/01 - Zwei.mp3",
		"Sampler/Hits/01.m4a",
		"Sampler/Hits/02.m4a",
		"@eaDir/x/SYNO.mp3",
		"Aurora/Nordlicht (2019)/cover.jpg",
	}
	for _, d := range dateien {
		p := filepath.Join(root, d)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	fake := &musikFake{tags: map[string]*MusikTags{
		"01.m4a": {Titel: "Sommer", Interpret: "Kalle", Album: "Hits", Compilation: true},
		"02.m4a": {Titel: "Meer", Interpret: "Aurora", Album: "Hits", Compilation: true},
	}}
	sc := New(st, fake)
	lib, _ := st.CreateLibrary(context.Background(), "Musik", "music", root, "de")
	res, err := sc.Scan(context.Background(), *lib)
	if err != nil {
		t.Fatal(err)
	}
	if res.Added != 6 {
		t.Fatalf("6 Titel erwartet (ohne @eaDir), bekommen %d", res.Added)
	}
	v := store.MusikSicht{Bibliotheken: []string{lib.ID}}
	alben, _, _ := st.MusikAlben(context.Background(), v, store.MusikAlbenAbfrage{})
	namen := map[string]store.MusikAlbum{}
	for _, a := range alben {
		namen[a.Name] = a
	}
	if len(alben) != 3 {
		t.Fatalf("3 Alben erwartet: %+v", alben)
	}
	if a := namen["Doppelt"]; a.Anzahl != 2 || a.Interpret != "Band" || a.Jahr != 2020 {
		t.Errorf("Doppelalbum: %+v", a)
	}
	if a := namen["Hits"]; !a.Compilation {
		t.Errorf("Kompilation: %+v", a)
	}
	if !strings.HasPrefix(namen["Nordlicht"].CoverQuelle, "datei:") {
		t.Errorf("cover.jpg nicht gefunden: %q", namen["Nordlicht"].CoverQuelle)
	}
	_, titel, _ := st.MusikAlbum(context.Background(), v, namen["Doppelt"].ID)
	if len(titel) != 2 || titel[0].Disc != 1 || titel[1].Disc != 2 {
		t.Errorf("Discs: %+v", titel)
	}
	// Aurora erscheint auf der Kompilation.
	auf, _ := st.MusikErscheintAuf(context.Background(), v, store.InterpretID("Aurora"), "Aurora")
	if len(auf) != 1 || auf[0].Name != "Hits" {
		t.Errorf("Erscheint auf: %+v", auf)
	}

	// Zweiter Durchlauf: nichts neu ausmessen.
	vorher := fake.gefragt
	res, _ = sc.Scan(context.Background(), *lib)
	if fake.gefragt != vorher || res.Added != 0 || res.Updated != 0 {
		t.Errorf("unveränderte Dateien neu gelesen: %d → %d, %+v", vorher, fake.gefragt, res)
	}
	// Eine Datei weg: vermerkt, nicht gelöscht.
	_ = os.Remove(filepath.Join(root, "Sampler/Hits/02.m4a"))
	res, _ = sc.Scan(context.Background(), *lib)
	if res.Missing != 1 {
		t.Errorf("fehlende Datei nicht vermerkt: %+v", res)
	}
	if n, _, _ := st.MusikZaehlen(context.Background(), v); n != 5 {
		t.Errorf("sichtbare Titel: %d", n)
	}
}

func TestFotoDurchlauf(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "l.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	root := t.TempDir()
	for _, d := range []string{"Ferien/IMG_20240713_180211.jpg", "Ferien/b.png", "lose.gif", "@eaDir/a.jpg", "notiz.txt"} {
		p := filepath.Join(root, d)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		_ = os.WriteFile(p, []byte("x"), 0o644)
	}
	alt := time.Date(2020, 5, 1, 12, 0, 0, 0, time.Local)
	_ = os.Chtimes(filepath.Join(root, "Ferien/b.png"), alt, alt)
	_ = os.Chtimes(filepath.Join(root, "lose.gif"), alt.AddDate(-1, 0, 0), alt.AddDate(-1, 0, 0))
	sc := New(st, nil)
	lib, _ := st.CreateLibrary(context.Background(), "Fotos", "photo", root, "de")
	res, err := sc.Scan(context.Background(), *lib)
	if err != nil || res.Added != 3 {
		t.Fatalf("%+v %v", res, err)
	}
	v := store.FotoSicht{Bibliotheken: []string{lib.ID}}
	fotos, _, _ := st.FotoListe(context.Background(), v, store.FotoAbfrage{})
	if len(fotos) != 3 || fotos[0].DatumQuelle != "name" || fotos[0].Aufnahme.Format("2006-01-02 15:04") != "2024-07-13 18:02" {
		t.Errorf("Fotos: %+v", fotos)
	}
	alben, _ := st.FotoAlben(context.Background(), v)
	if len(alben) != 1 || alben[0].Name != "Ferien" || alben[0].Anzahl != 2 {
		t.Errorf("Alben: %+v", alben)
	}
	monate, _ := st.FotoUebersicht(context.Background(), v)
	if len(monate) == 0 || monate[0].Jahr != 2024 || monate[0].Monat != 7 || len(monate[0].Titelbilder) != 1 {
		t.Errorf("Übersicht: %+v", monate)
	}
	// Ohne Freigabe: nichts.
	if f, _, _ := st.FotoListe(context.Background(), store.FotoSicht{}, store.FotoAbfrage{}); len(f) != 0 {
		t.Error("Fotos ohne Freigabe sichtbar")
	}
}
