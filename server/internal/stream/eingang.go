// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package stream

// Dateien mit seltsamem Anfang.
//
// Auf dem NAS eines Besitzers standen alte AVI und MP4 als „defekt",
// weil ffprobe „Invalid data found when processing input" meldete. Oft
// ist nur der Anfang verdorben: Müll vor dem eigentlichen Film (ein
// Rekorder-Kopf, ein abgebrochener Kopiervorgang, der neu angesetzt hat).
// FFmpeg sucht den Anfang nicht selbst. Eingang schaut in die ersten
// Megabytes, findet den Kopf eines bekannten Formats und sagt FFmpeg, ab
// wo und als was es lesen soll — beim Messen wie beim Abspielen.

import (
	"bytes"
	"io"
	"os"
	"sync"
)

// eingangSuche: so weit sucht Eingang nach einem bekannten Kopf.
const eingangSuche = 32 << 20

type eingangEintrag struct {
	groesse, zeit int64
	args          []string
}

var eingangGemerkt sync.Map // Pfad → eingangEintrag

// Eingang nennt die Angaben, die FFmpeg vor „-i" braucht, um die Datei zu
// lesen — nichts bei gewöhnlichen Dateien (bekannter Kopf ganz vorne),
// „-f <format> -skip_initial_bytes <n>", wenn der Kopf erst weiter hinten
// steht. Gemerkt je Pfad, Grösse und Änderungszeit.
func Eingang(pfad string) []string {
	info, err := os.Stat(pfad)
	if err != nil || !info.Mode().IsRegular() {
		return nil
	}
	if v, ok := eingangGemerkt.Load(pfad); ok {
		e := v.(eingangEintrag)
		if e.groesse == info.Size() && e.zeit == info.ModTime().UnixNano() {
			return e.args
		}
	}
	args := eingangSuchen(pfad)
	eingangGemerkt.Store(pfad, eingangEintrag{groesse: info.Size(), zeit: info.ModTime().UnixNano(), args: args})
	return args
}

func eingangSuchen(pfad string) []string {
	f, err := os.Open(pfad)
	if err != nil {
		return nil
	}
	defer f.Close()
	kopf := make([]byte, 64<<10)
	n, _ := io.ReadFull(f, kopf)
	kopf = kopf[:n]
	if n < 16 || bekannterKopf(kopf) {
		return nil
	}
	// Weiter hinten suchen, in Stücken mit Überlappung.
	const stueck = 1 << 20
	puffer := make([]byte, stueck+16)
	var ab int64
	for ab < eingangSuche {
		n, err := f.ReadAt(puffer, ab)
		if n <= 16 {
			break
		}
		if i, format := kopfFinden(puffer[:n]); i >= 0 {
			return []string{"-f", format, "-skip_initial_bytes", itoa64(ab + int64(i))}
		}
		if err != nil {
			break
		}
		ab += stueck
	}
	return nil
}

// bekannterKopf: Fängt die Datei wie ein Video an, das FFmpeg von selbst
// erkennt?
func bekannterKopf(b []byte) bool {
	switch {
	case bytes.HasPrefix(b, []byte{0x1a, 0x45, 0xdf, 0xa3}), // Matroska/WebM
		bytes.HasPrefix(b, []byte("RIFF")),
		len(b) >= 8 && (string(b[4:8]) == "ftyp" || string(b[4:8]) == "moov" ||
			string(b[4:8]) == "mdat" || string(b[4:8]) == "free" || string(b[4:8]) == "wide"),
		bytes.HasPrefix(b, []byte{0x00, 0x00, 0x01, 0xba}),               // MPEG-PS
		bytes.HasPrefix(b, []byte{0x00, 0x00, 0x01, 0xb3}),               // MPEG-Video
		len(b) > 376 && b[0] == 0x47 && b[188] == 0x47 && b[376] == 0x47, // MPEG-TS
		bytes.HasPrefix(b, []byte("FLV")),
		bytes.HasPrefix(b, []byte("OggS")),
		bytes.HasPrefix(b, []byte{0x30, 0x26, 0xb2, 0x75}): // ASF/WMV
		return true
	}
	return false
}

// kopfFinden sucht den ersten Kopf eines Formats, das sich mitten in einer
// Datei sicher erkennen lässt.
func kopfFinden(b []byte) (int, string) {
	bester, format := -1, ""
	nimm := func(i int, f string) {
		if i >= 0 && (bester < 0 || i < bester) {
			bester, format = i, f
		}
	}
	nimm(bytes.Index(b, []byte{0x1a, 0x45, 0xdf, 0xa3}), "matroska")
	if i := bytes.Index(b, []byte("RIFF")); i >= 0 && i+12 <= len(b) && string(b[i+8:i+12]) == "AVI " {
		nimm(i, "avi")
	}
	if i := bytes.Index(b, []byte("ftyp")); i >= 4 {
		nimm(i-4, "mov")
	}
	if i := bytes.Index(b, []byte{0x00, 0x00, 0x01, 0xba}); i >= 0 {
		nimm(i, "mpeg")
	}
	return bester, format
}

func itoa64(n int64) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
