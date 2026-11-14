// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package scheibe

import (
	"context"
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

type testItem struct {
	clip string
	ms   int64
}

type testStrom struct {
	art byte // 0x1b Bild, 0x81 Ton, 0x90 PGS
	pid uint16
	spr string
}

// mplsBauen schreibt eine Playlist nach der Blu-ray-Spezifikation (nur die
// Felder, die Lumen liest). marken: je Marke (PlayItem, ms ab Ein).
func mplsBauen(items []testItem, stroeme []testStrom, marken [][2]int64) []byte {
	var stn []byte
	zahl := map[string]int{}
	for _, s := range stroeme {
		switch {
		case s.art == 0x1b:
			zahl["v"]++
		case s.art == 0x90:
			zahl["s"]++
		default:
			zahl["a"]++
		}
	}
	stn = append(stn, 0, 0, 0, 0, byte(zahl["v"]), byte(zahl["a"]), byte(zahl["s"]), 0, 0, 0, 0, 0, 0, 0, 0, 0)
	for _, s := range stroeme {
		stn = append(stn, 3, 1, byte(s.pid>>8), byte(s.pid))
		switch {
		case s.art == 0x1b:
			stn = append(stn, 2, s.art, 0x31)
		case s.art == 0x90:
			stn = append(stn, 4, s.art)
			stn = append(stn, s.spr...)
		default:
			stn = append(stn, 5, s.art, 0x31)
			stn = append(stn, s.spr...)
		}
	}
	binary.BigEndian.PutUint16(stn, uint16(len(stn)-2))

	var pl []byte
	pl = append(pl, 0, 0, 0, 0, 0, 0, byte(len(items)>>8), byte(len(items)), 0, 0)
	var ein int64 = 27000000
	for _, it := range items {
		e := make([]byte, 32)
		copy(e, it.clip)
		copy(e[5:], "M2TS")
		binary.BigEndian.PutUint32(e[12:], uint32(ein))
		binary.BigEndian.PutUint32(e[16:], uint32(ein+it.ms*45))
		e = append(e, stn...)
		pl = append(pl, byte(len(e)>>8), byte(len(e)))
		pl = append(pl, e...)
	}
	binary.BigEndian.PutUint32(pl, uint32(len(pl)-4))

	mk := []byte{0, 0, 0, 0, byte(len(marken) >> 8), byte(len(marken))}
	for _, m := range marken {
		e := make([]byte, 14)
		e[1] = 1
		binary.BigEndian.PutUint16(e[2:], uint16(m[0]))
		binary.BigEndian.PutUint32(e[4:], uint32(ein+m[1]*45))
		mk = append(mk, e...)
	}
	kopf := make([]byte, 40)
	copy(kopf, "MPLS0200")
	binary.BigEndian.PutUint32(kopf[8:], 40)
	binary.BigEndian.PutUint32(kopf[12:], uint32(40+len(pl)))
	return append(append(kopf, pl...), mk...)
}

func TestPlanBluRay(t *testing.T) {
	bdmv := filepath.Join(t.TempDir(), "BDMV")
	for _, d := range []string{"PLAYLIST", "STREAM"} {
		os.MkdirAll(filepath.Join(bdmv, d), 0o755)
	}
	stroeme := []testStrom{{0x1b, 0x1011, ""}, {0x81, 0x1100, "deu"}, {0x81, 0x1101, "eng"}, {0x90, 0x1200, "deu"}}
	schreiben(t, filepath.Join(bdmv, "PLAYLIST"), "00000.mpls",
		mplsBauen([]testItem{{"00001", 4000}, {"00002", 4000}}, stroeme, [][2]int64{{0, 0}, {1, 0}, {1, 2000}}))
	schreiben(t, filepath.Join(bdmv, "PLAYLIST"), "00001.mpls", mplsBauen([]testItem{{"00003", 2000}}, stroeme[:2], nil))
	// Attrappe: derselbe Clip zehnmal — länger, aber kein Film.
	var schleife []testItem
	for i := 0; i < 10; i++ {
		schleife = append(schleife, testItem{"00001", 4000})
	}
	schreiben(t, filepath.Join(bdmv, "PLAYLIST"), "00800.mpls", mplsBauen(schleife, stroeme, nil))
	// Verweist auf einen fehlenden Clip.
	schreiben(t, filepath.Join(bdmv, "PLAYLIST"), "00900.mpls", mplsBauen([]testItem{{"09999", 99000}}, stroeme, nil))
	leereDatei(t, filepath.Join(bdmv, "STREAM"), "00001.m2ts", 1000*192)
	leereDatei(t, filepath.Join(bdmv, "STREAM"), "00002.m2ts", 1200*192)
	leereDatei(t, filepath.Join(bdmv, "STREAM"), "00003.m2ts", 9000*192)

	stream, _ := OrdnerLesen(filepath.Join(bdmv, "STREAM"))
	p, err := PlanBluRay(bdmv, stream)
	if err != nil {
		t.Fatal(err)
	}
	if p.Ausweg || p.DauerMS != 8000 || len(p.Abschnitte) != 2 || p.Abschnitte[0].Datei != "00001.m2ts" ||
		p.Abschnitte[1].Datei != "00002.m2ts" {
		t.Fatalf("Plan %+v", p)
	}
	if got := p.Kapitel; len(got) != 3 || got[1] != 4000 || got[2] != 6000 {
		t.Errorf("Kapitel %v", got)
	}
	want := []Spur{{"v", 0x1011, ""}, {"a", 0x1100, "ger"}, {"a", 0x1101, "eng"}, {"s", 0x1200, "ger"}}
	if len(p.Spuren) != len(want) {
		t.Fatalf("Spuren %+v", p.Spuren)
	}
	for i := range want {
		if p.Spuren[i] != want[i] {
			t.Errorf("Spur %d: %+v", i, p.Spuren[i])
		}
	}

	// Ohne Playlists: die grösste Datei.
	os.RemoveAll(filepath.Join(bdmv, "PLAYLIST"))
	p, err = PlanBluRay(bdmv, stream)
	if err != nil || !p.Ausweg || p.Abschnitte[0].Datei != "00003.m2ts" {
		t.Fatalf("Ausweg %+v %v", p, err)
	}
}

func m2tsBauen(t *testing.T, ffmpeg, ziel string, sek int) {
	t.Helper()
	out, err := exec.Command(ffmpeg, "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc=size=320x240:rate=25:duration="+strconv.Itoa(sek),
		"-f", "lavfi", "-i", "sine=duration="+strconv.Itoa(sek),
		"-c:v", "mpeg2video", "-c:a", "ac3", "-streamid", "0:0x1011", "-streamid", "1:0x1100",
		"-mpegts_m2ts_mode", "1", "-f", "mpegts", ziel).CombinedOutput()
	if err != nil {
		t.Fatalf("ffmpeg: %v %s", err, out)
	}
}

func TestBluRayOrdnerUmwandeln(t *testing.T) {
	ffmpeg, ffprobe := werkzeug(t, "ffmpeg"), werkzeug(t, "ffprobe")
	bdmv := filepath.Join(t.TempDir(), "BDMV")
	for _, d := range []string{"PLAYLIST", "STREAM"} {
		os.MkdirAll(filepath.Join(bdmv, d), 0o755)
	}
	m2tsBauen(t, ffmpeg, filepath.Join(bdmv, "STREAM", "00001.m2ts"), 6)
	m2tsBauen(t, ffmpeg, filepath.Join(bdmv, "STREAM", "00002.m2ts"), 2)
	stroeme := []testStrom{{0x1b, 0x1011, ""}, {0x81, 0x1100, "fra"}}
	schreiben(t, filepath.Join(bdmv, "PLAYLIST"), "00000.mpls",
		mplsBauen([]testItem{{"00001", 6000}}, stroeme, [][2]int64{{0, 0}, {0, 3000}}))
	schreiben(t, filepath.Join(bdmv, "PLAYLIST"), "00001.mpls", mplsBauen([]testItem{{"00002", 2000}}, stroeme, nil))
	stream, _ := OrdnerLesen(filepath.Join(bdmv, "STREAM"))
	p, err := PlanBluRay(bdmv, stream)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	gefunden, err := SpurenFinden(ctx, ffprobe, p)
	if err != nil {
		t.Fatal(err)
	}
	spuren := Auswahl(p, gefunden)
	meta, _ := KapitelDatei(p, t.TempDir())
	ziel := filepath.Join(t.TempDir(), "film.mkv")
	if err := Umwandeln(ctx, ffmpeg, p, spuren, meta, ziel, nil, nil); err != nil {
		t.Fatal(err)
	}
	e := proben(t, ffprobe, ziel)
	var arten []string
	for _, s := range e.Streams {
		arten = append(arten, s.CodecType+":"+s.Tags["language"])
	}
	if strings.Join(arten, ",") != "video:,audio:fre" {
		t.Errorf("Spuren %v", arten)
	}
	if d, _ := strconv.ParseFloat(e.Format.Duration, 64); d < 5 || d > 7 {
		t.Errorf("Dauer %s", e.Format.Duration)
	}
	if len(e.Chapters) != 2 {
		t.Errorf("%d Kapitel", len(e.Chapters))
	}
}
