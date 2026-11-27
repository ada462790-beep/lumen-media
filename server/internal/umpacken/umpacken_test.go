// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package umpacken

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func werkzeug(t *testing.T, name string) string {
	t.Helper()
	p, err := exec.LookPath(name)
	if err != nil {
		t.Skipf("%s fehlt", name)
	}
	return p
}

// bauen erzeugt eine kleine Testdatei: 4 s Testbild und Ton, dazu was
// args sonst noch verlangt (Codecs, Untertitel).
func bauen(t *testing.T, ziel string, vorher []string, args ...string) {
	t.Helper()
	ffmpeg := werkzeug(t, "ffmpeg")
	a := []string{"-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc=size=320x240:rate=25:duration=4",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=4"}
	a = append(a, vorher...)
	a = append(a, args...)
	a = append(a, ziel)
	if out, err := exec.Command(ffmpeg, a...).CombinedOutput(); err != nil {
		t.Skipf("ffmpeg kann die Testdatei nicht bauen: %v %s", err, out)
	}
}

type spurInfo struct {
	Art, Codec, Sprache string
}

func spurenVon(t *testing.T, pfad string) []spurInfo {
	t.Helper()
	out, err := exec.Command(werkzeug(t, "ffprobe"), "-v", "error", "-show_entries",
		"stream=codec_type,codec_name:stream_tags=language", "-of", "json", pfad).Output()
	if err != nil {
		t.Fatal(err)
	}
	var j struct {
		Streams []struct {
			CodecType string            `json:"codec_type"`
			CodecName string            `json:"codec_name"`
			Tags      map[string]string `json:"tags"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(out, &j); err != nil {
		t.Fatal(err)
	}
	var s []spurInfo
	for _, x := range j.Streams {
		s = append(s, spurInfo{x.CodecType, x.CodecName, x.Tags["language"]})
	}
	return s
}

// umpackenUndPruefen geht den ganzen Weg: lesen, planen, umpacken, prüfen.
func umpackenUndPruefen(t *testing.T, quelle string) (*Plan, string) {
	t.Helper()
	ffmpeg, ffprobe := werkzeug(t, "ffmpeg"), werkzeug(t, "ffprobe")
	ctx, ab := context.WithTimeout(context.Background(), time.Minute)
	defer ab()
	b, err := Lesen(ctx, ffprobe, quelle, nil)
	if err != nil {
		t.Fatal(err)
	}
	p, err := Planen(quelle, b)
	if err != nil {
		t.Fatal(err)
	}
	tmp := Temp(quelle)
	var gemeldet bool
	if err := Umpacken(ctx, ffmpeg, p, nil, tmp, nil, func(int64) { gemeldet = true }); err != nil {
		t.Fatal(err)
	}
	_ = gemeldet // bei 4 s Testbild ist FFmpeg oft schneller als eine Sekunde
	info, _ := os.Stat(quelle)
	if err := Pruefen(ctx, ffprobe, p, tmp, info.Size()); err != nil {
		t.Fatalf("prüfung: %v", err)
	}
	return p, tmp
}

func TestNamen(t *testing.T) {
	if got := Ziel("/m/Film (1999)/Film (1999).AVI"); got != "/m/Film (1999)/Film (1999).mkv" {
		t.Error(got)
	}
	if got := Temp("/m/Film/Film.cd1.avi"); got != "/m/Film/.Film.cd1.lumen-tmp.mkv" {
		t.Error(got)
	}
	if !IstTemp("/m/.Film.lumen-tmp.mkv") || IstTemp("/m/Film.mkv") || IstTemp("/m/Film.lumen-tmp.mkv") {
		t.Error("IstTemp")
	}
	if Endung("/a/b.MPEG") != ".mpeg" || Endung("/a/b.mkv") != "" || Endung("/a/b.webm") != "" {
		t.Error("Endung")
	}
	if InAllen(".mp4") || InAllen(".m4v") || !InAllen(".avi") || !InAllen(".ts") || InAllen(".mkv") {
		t.Error("InAllen")
	}
}

func TestAVIMitMP3(t *testing.T) {
	dir := t.TempDir()
	q := filepath.Join(dir, "Film.avi")
	bauen(t, q, nil, "-c:v", "mpeg4", "-vtag", "XVID", "-c:a", "libmp3lame")
	p, tmp := umpackenUndPruefen(t, q)
	got := spurenVon(t, tmp)
	if len(got) != 2 || got[0].Codec != "mpeg4" || got[1].Codec != "mp3" {
		t.Errorf("spuren: %+v", got)
	}
	if strings.Join(Argumente(p, nil, "x.mkv"), " ") == "" || !strings.Contains(strings.Join(Argumente(p, nil, "x.mkv"), " "), "mpeg4_unpack_bframes") {
		t.Error("XviD ohne mpeg4_unpack_bframes")
	}
}

func TestWMV(t *testing.T) {
	dir := t.TempDir()
	q := filepath.Join(dir, "Film.wmv")
	bauen(t, q, nil, "-c:v", "wmv2", "-c:a", "wmav2")
	_, tmp := umpackenUndPruefen(t, q)
	if got := spurenVon(t, tmp); len(got) != 2 || got[0].Codec != "wmv2" || got[1].Codec != "wmav2" {
		t.Errorf("spuren: %+v", got)
	}
}

func TestMPGMitZweiTonspuren(t *testing.T) {
	dir := t.TempDir()
	q := filepath.Join(dir, "Film.mpg")
	bauen(t, q, []string{"-f", "lavfi", "-i", "sine=frequency=660:duration=4"},
		"-map", "0:v", "-map", "1:a", "-map", "2:a", "-c:v", "mpeg2video", "-c:a", "mp2",
		"-metadata:s:a:0", "language=deu", "-metadata:s:a:1", "language=eng")
	_, tmp := umpackenUndPruefen(t, q)
	got := spurenVon(t, tmp)
	if len(got) != 3 || got[0].Codec != "mpeg2video" || got[1].Codec != "mp2" || got[2].Codec != "mp2" {
		t.Errorf("spuren: %+v", got)
	}
}

// MOV mit mov_text: Der Untertitel wird SubRip, die Sprache bleibt; Bild
// und Ton werden nicht angefasst.
func TestMOVMitMovText(t *testing.T) {
	dir := t.TempDir()
	srt := filepath.Join(dir, "u.srt")
	if err := os.WriteFile(srt, []byte("1\n00:00:01,000 --> 00:00:03,000\nGrüezi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	q := filepath.Join(dir, "Film.mov")
	bauen(t, q, []string{"-i", srt}, "-map", "0:v", "-map", "1:a", "-map", "2:s",
		"-c:v", "mpeg4", "-c:a", "aac", "-c:s", "mov_text", "-metadata:s:s:0", "language=ger")
	p, tmp := umpackenUndPruefen(t, q)
	if len(p.Gewandelt) != 1 || !strings.Contains(p.Gewandelt[0], "mov_text") {
		t.Errorf("gewandelt: %v", p.Gewandelt)
	}
	got := spurenVon(t, tmp)
	if len(got) != 3 || got[2].Art != "subtitle" || got[2].Codec != "subrip" || got[2].Sprache != "ger" {
		t.Errorf("spuren: %+v", got)
	}
	if got[0].Codec != "mpeg4" || got[1].Codec != "aac" {
		t.Errorf("bild/ton neu gerechnet? %+v", got)
	}
}

// Eine AVI, deren Index (idx1) unbrauchbar ist: FFmpeg liest sie mit
// +genpts trotzdem vollständig, und die Prüfung findet dieselbe Laufzeit.
func TestAVIMitKaputtemIndex(t *testing.T) {
	dir := t.TempDir()
	heil := filepath.Join(dir, "heil.avi")
	bauen(t, heil, nil, "-c:v", "mpeg4", "-c:a", "libmp3lame")
	b, err := os.ReadFile(heil)
	if err != nil {
		t.Fatal(err)
	}
	i := bytes.LastIndex(b, []byte("idx1"))
	if i < 0 {
		t.Skip("keine idx1 in der Testdatei")
	}
	copy(b[i:], "JUNK")
	q := filepath.Join(dir, "Film.avi")
	if err := os.WriteFile(q, b, 0o644); err != nil {
		t.Fatal(err)
	}
	_, tmp := umpackenUndPruefen(t, q)
	if got := spurenVon(t, tmp); len(got) != 2 {
		t.Errorf("spuren: %+v", got)
	}
}

func TestPlanen(t *testing.T) {
	b := &Bestand{DauerMS: 4000, Spuren: []Spur{
		{Index: 0, Art: "video", Codec: "h264"},
		{Index: 1, Art: "audio", Codec: "pcm_dvd"},
		{Index: 2, Art: "data", Codec: "bin_data"},
		{Index: 3, Art: "subtitle", Codec: "dvb_teletext"},
		{Index: 4, Art: "video", Codec: "mjpeg", Titelbild: true},
		{Index: 5, Art: "subtitle", Codec: "dvd_subtitle"},
	}}
	p, err := Planen("/m/a.vob", b)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Spuren) != 3 || p.Spuren[1].Neu != "flac" || p.Spuren[2].Codec != "dvd_subtitle" {
		t.Errorf("spuren: %+v", p.Spuren)
	}
	if len(p.Weggelassen) != 3 {
		t.Errorf("weggelassen: %v", p.Weggelassen)
	}
	a := strings.Join(Argumente(p, []string{"-f", "mpeg", "-skip_initial_bytes", "2048"}, "/m/.a.lumen-tmp.mkv"), " ")
	for _, soll := range []string{"-fflags +genpts", "-probesize 400M", "-f mpeg -skip_initial_bytes 2048 -i /m/a.vob",
		"-map 0:0 -map 0:1 -map 0:5", "-c copy", "-c:1 flac", "-map_chapters 0", "-f matroska /m/.a.lumen-tmp.mkv"} {
		if !strings.Contains(a, soll) {
			t.Errorf("%q fehlt in %s", soll, a)
		}
	}
	if strings.Contains(a, "0:2") || strings.Contains(a, "0:3") || strings.Contains(a, "0:4") {
		t.Errorf("weggelassene spur gemappt: %s", a)
	}

	// XSUB (DivX-Untertitel) nimmt Matroska nicht: lieber nicht umpacken.
	b.Spuren = append(b.Spuren, Spur{Index: 6, Art: "subtitle", Codec: "xsub"})
	var pn *SpurPasstNicht
	if _, err := Planen("/m/a.avi", b); !errors.As(err, &pn) || pn.Codec != "xsub" {
		t.Errorf("xsub: %v", err)
	}
	if _, err := Planen("/m/a.avi", &Bestand{Spuren: []Spur{{Art: "audio", Codec: "mp3"}}}); !errors.Is(err, ErrKeinBild) {
		t.Errorf("ohne bild: %v", err)
	}
}

// Vergleichen: Was die Prüfung verwirft — das Original bleibt dann.
func TestVergleichen(t *testing.T) {
	p, _ := Planen("/m/a.mov", &Bestand{DauerMS: 60000, Spuren: []Spur{
		{Index: 0, Art: "video", Codec: "h264"}, {Index: 1, Art: "audio", Codec: "aac"},
		{Index: 2, Art: "subtitle", Codec: "mov_text"}}})
	gut := &Bestand{DauerMS: 60400, Spuren: []Spur{
		{Art: "video", Codec: "h264"}, {Art: "audio", Codec: "aac"}, {Art: "subtitle", Codec: "subrip"}}}
	if err := Vergleichen(p, gut, 95, 100); err != nil {
		t.Errorf("gut: %v", err)
	}
	var ab *Abweichung
	ohneTon := &Bestand{DauerMS: 60000, Spuren: []Spur{{Art: "video", Codec: "h264"}, {Art: "subtitle", Codec: "subrip"}}}
	if err := Vergleichen(p, ohneTon, 95, 100); !errors.As(err, &ab) || ab.Art != "spuren" {
		t.Errorf("ohne ton: %v", err)
	}
	kurz := *gut
	kurz.DauerMS = 58000
	if err := Vergleichen(p, &kurz, 95, 100); !errors.As(err, &ab) || ab.Art != "dauer" {
		t.Errorf("zu kurz: %v", err)
	}
	if err := Vergleichen(p, gut, 50, 100); !errors.As(err, &ab) || ab.Art != "groesse" {
		t.Errorf("zu klein: %v", err)
	}
	// Transportströme schrumpfen stärker — 70 % sind dort in Ordnung.
	pts, _ := Planen("/m/a.ts", &Bestand{DauerMS: 60000, Spuren: []Spur{{Art: "video", Codec: "h264"}}})
	if err := Vergleichen(pts, &Bestand{DauerMS: 60000, Spuren: []Spur{{Art: "video", Codec: "h264"}}}, 70, 100); err != nil {
		t.Errorf("ts: %v", err)
	}
}
