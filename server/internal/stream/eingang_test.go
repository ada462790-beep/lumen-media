// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package stream

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// „Defekte Dateien" vom NAS: alte AVI und MP4, bei denen ffprobe „Invalid
// data found when processing input" meldete. Viele sind gar nicht kaputt —
// eine MKV mit Endung .avi, ein zerstörter Index, Müll vor dem Anfang
// (ein abgebrochener Kopiervorgang, ein Rekorder-Kopf). Erst ein zweiter,
// gründlicherer Versuch entscheidet.
func TestZweiterMessversuch(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ohne FFmpeg nicht prüfbar")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ohne ffprobe nicht prüfbar")
	}
	dir := t.TempDir()
	machen := func(name string, args ...string) []byte {
		t.Helper()
		ziel := filepath.Join(dir, name)
		a := append([]string{"-v", "error", "-y", "-f", "lavfi", "-i", "testsrc=size=160x120:rate=25:duration=3",
			"-f", "lavfi", "-i", "sine=duration=3"}, args...)
		if out, err := exec.Command("ffmpeg", append(a, ziel)...).CombinedOutput(); err != nil {
			t.Skipf("Testdatei nicht erzeugbar: %v %s", err, out)
		}
		b, _ := os.ReadFile(ziel)
		return b
	}
	avi := machen("ok.avi", "-c:v", "mpeg4", "-c:a", "mp3")
	mkv := machen("ok.mkv", "-c:v", "mpeg4", "-c:a", "mp3")
	schreiben := func(name string, b []byte) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	// Zerstörter Index (idx1) am Ende einer AVI.
	idx := append([]byte(nil), avi...)
	if i := bytes.Index(idx, []byte("idx1")); i > 0 {
		for j := i + 8; j < len(idx) && j < i+408; j++ {
			idx[j] = 0xff
		}
	}
	muell := bytes.Repeat([]byte{0x00, 0x13}, 300_000)
	// Feste Bytes statt echtem Zufall: Zufällige Daten enthielten ab und zu
	// zufällig etwas, das FFmpeg als Videostrom deutete (z. B. MPEG-TS-
	// Synchronbytes) — der Test schlug dann ohne Grund fehl.
	zufall := make([]byte, 200_000)
	z := uint32(0x9E3779B9)
	for i := range zufall {
		z ^= z << 13
		z ^= z >> 17
		z ^= z << 5
		b := byte(z)
		if b == 0x47 || b == 0x00 || b == 0x01 { // keine TS-/Startcode-Bytes
			b = 0xA5
		}
		zufall[i] = b
	}

	ff := NewFFmpeg("", "")
	ctx := context.Background()
	for _, c := range []struct {
		name   string
		daten  []byte
		lesbar bool
	}{
		{"index-kaputt.avi", idx, true},
		{"in-wahrheit-mkv.avi", mkv, true},
		{"muell-vor-mkv.avi", append(append([]byte(nil), muell...), mkv...), true},
		{"muell-vor-avi.avi", append(bytes.Repeat([]byte("junk"), 100_000), avi...), true},
		{"wirklich-kaputt.avi", zufall, false},
	} {
		p := schreiben(c.name, c.daten)
		pr, err := ff.Probe(ctx, p)
		if c.lesbar {
			if err != nil || pr.VideoCodec == "" || pr.DurationMS < 2000 {
				t.Errorf("%s: nicht lesbar (%v)", c.name, err)
				continue
			}
			// Was sich messen lässt, lässt sich auch umpacken: FFmpeg
			// bekommt dieselben Eingangsangaben.
			args := append(append([]string{"-v", "error"}, Eingang(p)...), "-i", p, "-t", "1", "-f", "null", "-")
			if out, err := exec.Command("ffmpeg", args...).CombinedOutput(); err != nil {
				t.Errorf("%s: FFmpeg liest sie nicht: %v %s", c.name, err, out)
			}
		} else if err == nil && pr.VideoCodec != "" {
			t.Errorf("%s: als lesbar gemessen", c.name)
		}
	}
	// Eine gewöhnliche Datei braucht keine Sonderangaben.
	if e := Eingang(filepath.Join(dir, "ok.mkv")); len(e) != 0 {
		t.Errorf("Eingang(ok.mkv) = %v, erwartet nichts", e)
	}
}
