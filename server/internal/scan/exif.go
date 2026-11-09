// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package scan

// Ein kleiner Leser für die Angaben, die Kameras in Fotos schreiben
// (EXIF): Aufnahmezeit, Ausrichtung, Kamera, Ort und Bildgrösse.
//
// Eigener Code statt einer Bibliothek, weil Lumen ohne Abhängigkeiten
// auskommen will und nur ein Dutzend Felder braucht. Gelesen wird nur der
// Kopf der Datei — nie das ganze Bild —, damit ein Durchlauf über
// zehntausend Fotos auf einem NAS nicht Stunden dauert.

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"strings"
	"time"
)

// Exif: was Lumen aus einem Foto liest. Leere Felder heissen „steht nicht
// drin".
type Exif struct {
	Aufnahme    time.Time // ohne Zone, als UTC — so, wie die Kamera es schrieb
	Ausrichtung int       // 1–8 wie in EXIF; 0 = unbekannt
	Kamera      string
	Breitengrad *float64
	Laengengrad *float64
	Breite      int // wie gespeichert, vor dem Drehen
	Hoehe       int
	// Drehung bei HEIC: dort dreht der Container (irot), nicht EXIF.
	HeifDrehung int
}

var errKeinExif = errors.New("keine exif-angaben")

// ExifLesen erkennt das Format am Dateianfang und sucht den EXIF-Block.
// Fehlt er, kommen wenigstens die Masse zurück, wenn sie sich finden.
func ExifLesen(r io.ReaderAt, groesse int64) (*Exif, error) {
	kopf := make([]byte, 16)
	if _, err := r.ReadAt(kopf, 0); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	e := &Exif{}
	var tiff []byte
	var err error
	switch {
	case kopf[0] == 0xFF && kopf[1] == 0xD8:
		tiff, err = jpegExif(r, groesse, e)
	case bytes.HasPrefix(kopf, []byte("\x89PNG\r\n\x1a\n")):
		tiff, err = pngExif(r, groesse, e)
	case bytes.Equal(kopf[0:4], []byte("RIFF")) && bytes.Equal(kopf[8:12], []byte("WEBP")):
		tiff, err = webpExif(r, groesse, e)
	case bytes.Equal(kopf[4:8], []byte("ftyp")):
		tiff, err = heifExif(r, groesse, e)
	case bytes.HasPrefix(kopf, []byte("GIF8")):
		e.Breite = int(binary.LittleEndian.Uint16(kopf[6:8]))
		e.Hoehe = int(binary.LittleEndian.Uint16(kopf[8:10]))
		return e, nil
	default:
		return nil, errKeinExif
	}
	if err != nil && tiff == nil {
		if e.Breite > 0 {
			return e, nil
		}
		return nil, err
	}
	if tiff != nil {
		tiffLesen(tiff, e)
	}
	return e, nil
}

// ---------------------------------------------------------------- JPEG

func jpegExif(r io.ReaderAt, groesse int64, e *Exif) ([]byte, error) {
	var tiff []byte
	pos := int64(2)
	seg := make([]byte, 4)
	for pos+4 <= groesse {
		if _, err := r.ReadAt(seg, pos); err != nil {
			break
		}
		if seg[0] != 0xFF {
			break
		}
		marker := seg[1]
		if marker == 0xD8 || marker == 0x01 || (marker >= 0xD0 && marker <= 0xD7) {
			pos += 2
			continue
		}
		if marker == 0xD9 || marker == 0xDA { // Bildende oder Bilddaten: kein Kopf mehr
			break
		}
		laenge := int64(binary.BigEndian.Uint16(seg[2:4]))
		if laenge < 2 {
			break
		}
		switch {
		case marker == 0xE1 && tiff == nil && laenge < 1<<16:
			daten := make([]byte, laenge-2)
			if _, err := r.ReadAt(daten, pos+4); err == nil && bytes.HasPrefix(daten, []byte("Exif\x00\x00")) {
				tiff = daten[6:]
			}
		case marker >= 0xC0 && marker <= 0xCF && marker != 0xC4 && marker != 0xC8 && marker != 0xCC:
			sof := make([]byte, 5)
			if _, err := r.ReadAt(sof, pos+4); err == nil {
				e.Hoehe = int(binary.BigEndian.Uint16(sof[1:3]))
				e.Breite = int(binary.BigEndian.Uint16(sof[3:5]))
			}
		}
		if tiff != nil && e.Breite > 0 {
			break
		}
		pos += 2 + laenge
	}
	if tiff == nil {
		return nil, errKeinExif
	}
	return tiff, nil
}

// ---------------------------------------------------------------- PNG

func pngExif(r io.ReaderAt, groesse int64, e *Exif) ([]byte, error) {
	pos := int64(8)
	kopf := make([]byte, 8)
	for pos+8 <= groesse {
		if _, err := r.ReadAt(kopf, pos); err != nil {
			break
		}
		n := int64(binary.BigEndian.Uint32(kopf[0:4]))
		typ := string(kopf[4:8])
		switch typ {
		case "IHDR":
			d := make([]byte, 8)
			if _, err := r.ReadAt(d, pos+8); err == nil {
				e.Breite = int(binary.BigEndian.Uint32(d[0:4]))
				e.Hoehe = int(binary.BigEndian.Uint32(d[4:8]))
			}
		case "eXIf":
			if n > 0 && n < 1<<20 {
				d := make([]byte, n)
				if _, err := r.ReadAt(d, pos+8); err == nil {
					return bytes.TrimPrefix(d, []byte("Exif\x00\x00")), nil
				}
			}
		case "IEND":
			return nil, errKeinExif
		}
		pos += 12 + n
	}
	return nil, errKeinExif
}

// ---------------------------------------------------------------- WebP

func webpExif(r io.ReaderAt, groesse int64, e *Exif) ([]byte, error) {
	pos := int64(12)
	kopf := make([]byte, 8)
	for pos+8 <= groesse {
		if _, err := r.ReadAt(kopf, pos); err != nil {
			break
		}
		typ := string(kopf[0:4])
		n := int64(binary.LittleEndian.Uint32(kopf[4:8]))
		switch typ {
		case "VP8X":
			d := make([]byte, 10)
			if _, err := r.ReadAt(d, pos+8); err == nil {
				e.Breite = int(uint32(d[4])|uint32(d[5])<<8|uint32(d[6])<<16) + 1
				e.Hoehe = int(uint32(d[7])|uint32(d[8])<<8|uint32(d[9])<<16) + 1
			}
		case "VP8 ":
			d := make([]byte, 10)
			if _, err := r.ReadAt(d, pos+8); err == nil && e.Breite == 0 {
				e.Breite = int(binary.LittleEndian.Uint16(d[6:8]) & 0x3FFF)
				e.Hoehe = int(binary.LittleEndian.Uint16(d[8:10]) & 0x3FFF)
			}
		case "VP8L":
			d := make([]byte, 5)
			if _, err := r.ReadAt(d, pos+8); err == nil && e.Breite == 0 {
				b := binary.LittleEndian.Uint32(d[1:5])
				e.Breite = int(b&0x3FFF) + 1
				e.Hoehe = int((b>>14)&0x3FFF) + 1
			}
		case "EXIF":
			if n > 0 && n < 1<<20 {
				d := make([]byte, n)
				if _, err := r.ReadAt(d, pos+8); err == nil {
					return bytes.TrimPrefix(d, []byte("Exif\x00\x00")), nil
				}
			}
		}
		pos += 8 + n + n%2
	}
	return nil, errKeinExif
}

// ---------------------------------------------------------------- HEIF

// heifBox liest Boxen (ISO-BMFF) innerhalb [von, bis).
type heifBox struct {
	typ        string
	daten, bis int64 // Beginn der Nutzdaten, Ende der Box
}

func heifBoxen(r io.ReaderAt, von, bis int64) []heifBox {
	var out []heifBox
	kopf := make([]byte, 16)
	for von+8 <= bis && len(out) < 4096 {
		if _, err := r.ReadAt(kopf[:8], von); err != nil {
			break
		}
		n := int64(binary.BigEndian.Uint32(kopf[0:4]))
		typ := string(kopf[4:8])
		start := von + 8
		if n == 1 {
			if _, err := r.ReadAt(kopf[8:16], von+8); err != nil {
				break
			}
			n = int64(binary.BigEndian.Uint64(kopf[8:16]))
			start = von + 16
		} else if n == 0 {
			n = bis - von
		}
		if n < 8 || von+n > bis {
			break
		}
		out = append(out, heifBox{typ: typ, daten: start, bis: von + n})
		von += n
	}
	return out
}

func heifExif(r io.ReaderAt, groesse int64, e *Exif) ([]byte, error) {
	var meta *heifBox
	for _, b := range heifBoxen(r, 0, groesse) {
		if b.typ == "meta" {
			b := b
			meta = &b
			break
		}
	}
	if meta == nil {
		return nil, errKeinExif
	}
	// meta ist eine „volle" Box: 4 Bytes Version und Flags.
	innen := heifBoxen(r, meta.daten+4, meta.bis)
	exifItem := uint32(0)
	var iloc *heifBox
	for _, b := range innen {
		switch b.typ {
		case "iinf":
			exifItem = heifExifItem(r, b)
		case "iloc":
			b := b
			iloc = &b
		case "iprp":
			for _, c := range heifBoxen(r, b.daten, b.bis) {
				if c.typ != "ipco" {
					continue
				}
				for _, p := range heifBoxen(r, c.daten, c.bis) {
					switch p.typ {
					case "ispe":
						d := make([]byte, 12)
						if _, err := r.ReadAt(d, p.daten); err == nil {
							w, h := int(binary.BigEndian.Uint32(d[4:8])), int(binary.BigEndian.Uint32(d[8:12]))
							// Das grösste ispe ist das Bild selbst; kleinere
							// gehören zu Kacheln oder zum Vorschaubild.
							if w*h > e.Breite*e.Hoehe {
								e.Breite, e.Hoehe = w, h
							}
						}
					case "irot":
						d := make([]byte, 1)
						if _, err := r.ReadAt(d, p.daten); err == nil {
							e.HeifDrehung = int(d[0]&3) * 90
						}
					}
				}
			}
		}
	}
	if exifItem == 0 || iloc == nil {
		return nil, errKeinExif
	}
	off, n, ok := heifItemOrt(r, *iloc, exifItem)
	if !ok || n <= 4 || n > 1<<20 || off+n > groesse {
		return nil, errKeinExif
	}
	d := make([]byte, n)
	if _, err := r.ReadAt(d, off); err != nil {
		return nil, err
	}
	// Die Nutzdaten beginnen mit dem Abstand bis zum TIFF-Kopf.
	skip := int(binary.BigEndian.Uint32(d[0:4]))
	if 4+skip >= len(d) {
		return nil, errKeinExif
	}
	return d[4+skip:], nil
}

// heifExifItem sucht in iinf den Eintrag vom Typ „Exif".
func heifExifItem(r io.ReaderAt, iinf heifBox) uint32 {
	v := make([]byte, 1)
	if _, err := r.ReadAt(v, iinf.daten); err != nil {
		return 0
	}
	start := iinf.daten + 4 + 2
	if v[0] != 0 {
		start = iinf.daten + 4 + 4
	}
	for _, b := range heifBoxen(r, start, iinf.bis) {
		if b.typ != "infe" {
			continue
		}
		d := make([]byte, 12)
		if _, err := r.ReadAt(d, b.daten); err != nil {
			continue
		}
		ver := d[0]
		var id uint32
		var typ string
		switch {
		case ver == 2:
			id = uint32(binary.BigEndian.Uint16(d[4:6]))
			typ = string(d[8:12])
		case ver >= 3:
			id = binary.BigEndian.Uint32(d[4:8])
			e := make([]byte, 4)
			if _, err := r.ReadAt(e, b.daten+10); err == nil {
				typ = string(e)
			}
		default:
			continue
		}
		if typ == "Exif" {
			return id
		}
	}
	return 0
}

// heifItemOrt liest aus iloc, wo die Daten eines Eintrags liegen (nur die
// erste Ausdehnung — bei Exif gibt es nur eine).
func heifItemOrt(r io.ReaderAt, iloc heifBox, item uint32) (int64, int64, bool) {
	n := iloc.bis - iloc.daten
	if n < 8 || n > 1<<22 {
		return 0, 0, false
	}
	d := make([]byte, n)
	if _, err := r.ReadAt(d, iloc.daten); err != nil {
		return 0, 0, false
	}
	ver := d[0]
	p := 4
	lies := func(groesse int) (uint64, bool) {
		if p+groesse > len(d) {
			return 0, false
		}
		var v uint64
		switch groesse {
		case 0:
		case 2:
			v = uint64(binary.BigEndian.Uint16(d[p:]))
		case 4:
			v = uint64(binary.BigEndian.Uint32(d[p:]))
		case 8:
			v = binary.BigEndian.Uint64(d[p:])
		default:
			return 0, false
		}
		p += groesse
		return v, true
	}
	if p+2 > len(d) {
		return 0, 0, false
	}
	offGr, lenGr := int(d[p]>>4), int(d[p]&15)
	baseGr, idxGr := int(d[p+1]>>4), 0
	if ver == 1 || ver == 2 {
		idxGr = int(d[p+1] & 15)
	}
	p += 2
	var anzahl uint64
	var ok bool
	if ver < 2 {
		anzahl, ok = lies(2)
	} else {
		anzahl, ok = lies(4)
	}
	if !ok {
		return 0, 0, false
	}
	for i := uint64(0); i < anzahl; i++ {
		var id uint64
		if ver < 2 {
			id, ok = lies(2)
		} else {
			id, ok = lies(4)
		}
		if !ok {
			return 0, 0, false
		}
		methode := uint64(0)
		if ver == 1 || ver == 2 {
			if methode, ok = lies(2); !ok {
				return 0, 0, false
			}
			methode &= 15
		}
		if _, ok = lies(2); !ok { // data_reference_index
			return 0, 0, false
		}
		basis, ok := lies(baseGr)
		if !ok {
			return 0, 0, false
		}
		ausd, ok := lies(2)
		if !ok {
			return 0, 0, false
		}
		var erstOff, erstLen uint64
		for j := uint64(0); j < ausd; j++ {
			if idxGr > 0 {
				if _, ok = lies(idxGr); !ok {
					return 0, 0, false
				}
			}
			o, ok1 := lies(offGr)
			l, ok2 := lies(lenGr)
			if !ok1 || !ok2 {
				return 0, 0, false
			}
			if j == 0 {
				erstOff, erstLen = o, l
			}
		}
		if uint32(id) == item {
			if methode != 0 {
				return 0, 0, false // Daten in idat — bei Exif nicht üblich
			}
			return int64(basis + erstOff), int64(erstLen), true
		}
	}
	return 0, 0, false
}

// ---------------------------------------------------------------- TIFF

func tiffLesen(t []byte, e *Exif) {
	if len(t) < 8 {
		return
	}
	var bo binary.ByteOrder
	switch string(t[0:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return
	}
	ifd0 := int(bo.Uint32(t[4:8]))
	var datumOriginal, datum string
	var exifIFD, gpsIFD int
	ifdLesen(t, bo, ifd0, func(tag, typ uint16, n uint32, wert []byte) {
		switch tag {
		case 0x0112:
			e.Ausrichtung = int(tiffZahl(bo, typ, wert))
		case 0x010F:
			hersteller := tiffText(t, bo, n, wert)
			if e.Kamera == "" {
				e.Kamera = hersteller
			} else if !strings.HasPrefix(strings.ToLower(e.Kamera), strings.ToLower(hersteller)) {
				e.Kamera = hersteller + " " + e.Kamera
			}
		case 0x0110:
			modell := tiffText(t, bo, n, wert)
			// „Apple iPhone 15" statt „Apple Apple iPhone 15"; viele
			// Hersteller wiederholen sich im Modellnamen.
			if e.Kamera != "" && !strings.HasPrefix(strings.ToLower(modell), strings.ToLower(e.Kamera)) {
				e.Kamera = e.Kamera + " " + modell
			} else {
				e.Kamera = modell
			}
		case 0x0132:
			datum = tiffText(t, bo, n, wert)
		case 0x8769:
			exifIFD = int(tiffZahl(bo, typ, wert))
		case 0x8825:
			gpsIFD = int(tiffZahl(bo, typ, wert))
		}
	})
	if exifIFD > 0 {
		ifdLesen(t, bo, exifIFD, func(tag, typ uint16, n uint32, wert []byte) {
			switch tag {
			case 0x9003:
				datumOriginal = tiffText(t, bo, n, wert)
			case 0xA002:
				if e.Breite == 0 {
					e.Breite = int(tiffZahl(bo, typ, wert))
				}
			case 0xA003:
				if e.Hoehe == 0 {
					e.Hoehe = int(tiffZahl(bo, typ, wert))
				}
			}
		})
	}
	if gpsIFD > 0 {
		var latRef, lonRef string
		var lat, lon []float64
		ifdLesen(t, bo, gpsIFD, func(tag, typ uint16, n uint32, wert []byte) {
			switch tag {
			case 1:
				latRef = tiffText(t, bo, n, wert)
			case 2:
				lat = tiffRationale(t, bo, n, wert)
			case 3:
				lonRef = tiffText(t, bo, n, wert)
			case 4:
				lon = tiffRationale(t, bo, n, wert)
			}
		})
		if len(lat) == 3 && len(lon) == 3 {
			b := lat[0] + lat[1]/60 + lat[2]/3600
			l := lon[0] + lon[1]/60 + lon[2]/3600
			if strings.HasPrefix(strings.ToUpper(latRef), "S") {
				b = -b
			}
			if strings.HasPrefix(strings.ToUpper(lonRef), "W") {
				l = -l
			}
			// 0/0 schreiben manche Kameras, wenn sie keinen Empfang hatten.
			if !(b == 0 && l == 0) && math.Abs(b) <= 90 && math.Abs(l) <= 180 {
				e.Breitengrad, e.Laengengrad = &b, &l
			}
		}
	}
	for _, d := range []string{datumOriginal, datum} {
		if t, ok := ExifZeit(d); ok {
			e.Aufnahme = t
			break
		}
	}
	e.Kamera = strings.TrimSpace(e.Kamera)
}

// ExifZeit: „2024:07:13 18:02:11" → Zeitpunkt (als UTC, ohne Umrechnen).
// Leere oder genullte Angaben („0000:00:00 00:00:00") zählen nicht.
func ExifZeit(s string) (time.Time, bool) {
	s = strings.TrimSpace(strings.TrimRight(s, "\x00"))
	for _, f := range []string{"2006:01:02 15:04:05", "2006-01-02 15:04:05", "2006:01:02 15:04", "2006-01-02T15:04:05"} {
		if t, err := time.ParseInLocation(f, s, time.UTC); err == nil && t.Year() > 1900 {
			return t, true
		}
	}
	return time.Time{}, false
}

func ifdLesen(t []byte, bo binary.ByteOrder, off int, fn func(tag, typ uint16, n uint32, wert []byte)) {
	if off <= 0 || off+2 > len(t) {
		return
	}
	anzahl := int(bo.Uint16(t[off:]))
	if anzahl > 512 {
		return
	}
	for i := 0; i < anzahl; i++ {
		p := off + 2 + i*12
		if p+12 > len(t) {
			return
		}
		fn(bo.Uint16(t[p:]), bo.Uint16(t[p+2:]), bo.Uint32(t[p+4:]), t[p+8:p+12])
	}
}

func tiffZahl(bo binary.ByteOrder, typ uint16, wert []byte) uint32 {
	switch typ {
	case 3: // SHORT
		return uint32(bo.Uint16(wert))
	case 4, 13: // LONG, IFD
		return bo.Uint32(wert)
	case 1, 7:
		return uint32(wert[0])
	}
	return 0
}

func tiffText(t []byte, bo binary.ByteOrder, n uint32, wert []byte) string {
	var b []byte
	if n <= 4 {
		b = wert[:n]
	} else {
		o := int(bo.Uint32(wert))
		if o < 0 || o+int(n) > len(t) || n > 4096 {
			return ""
		}
		b = t[o : o+int(n)]
	}
	return strings.TrimSpace(strings.TrimRight(string(b), "\x00"))
}

func tiffRationale(t []byte, bo binary.ByteOrder, n uint32, wert []byte) []float64 {
	o := int(bo.Uint32(wert))
	if n == 0 || n > 8 || o < 0 || o+int(n)*8 > len(t) {
		return nil
	}
	out := make([]float64, n)
	for i := range out {
		z := float64(bo.Uint32(t[o+i*8:]))
		nn := float64(bo.Uint32(t[o+i*8+4:]))
		if nn == 0 {
			return nil
		}
		out[i] = z / nn
	}
	return out
}
