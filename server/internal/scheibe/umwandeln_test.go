// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package scheibe

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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

// vobBauen erzeugt ein DVD-taugliches MPEG-PS (wie auf einer Scheibe:
// MPEG-2-Bild, zwei AC-3-Tonspuren, 2048-Byte-Pakete).
func vobBauen(t *testing.T, ffmpeg, ziel string, sek int, ton int) {
	t.Helper()
	args := []string{"-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc=size=352x288:rate=25:duration=" + strconv.Itoa(sek),
		"-f", "lavfi", "-i", "sine=frequency=440:duration=" + strconv.Itoa(sek),
		"-f", "lavfi", "-i", "sine=frequency=660:duration=" + strconv.Itoa(sek),
		"-map", "0:v"}
	for i := 0; i < ton; i++ {
		args = append(args, "-map", strconv.Itoa(1+i)+":a")
	}
	args = append(args, "-c:v", "mpeg2video", "-b:v", "1500k", "-g", "12", "-c:a", "ac3", "-b:a", "128k",
		"-ar", "48000", "-f", "dvd", ziel)
	if out, err := exec.Command(ffmpeg, args...).CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg: %v %s", err, out)
	}
}

func groesse(t *testing.T, p string) int64 {
	t.Helper()
	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	return info.Size()
}

func dateiAnhaengen(t *testing.T, ziel string, quellen ...string) {
	t.Helper()
	var alles []byte
	for _, q := range quellen {
		b, err := os.ReadFile(q)
		if err != nil {
			t.Fatal(err)
		}
		alles = append(alles, b...)
	}
	if err := os.WriteFile(ziel, alles, 0o644); err != nil {
		t.Fatal(err)
	}
}

type probeErgebnis struct {
	Streams []struct {
		CodecType string            `json:"codec_type"`
		CodecName string            `json:"codec_name"`
		Tags      map[string]string `json:"tags"`
	} `json:"streams"`
	Chapters []struct{} `json:"chapters"`
	Format   struct {
		Duration string `json:"duration"`
	} `json:"format"`
}

func proben(t *testing.T, ffprobe, datei string) probeErgebnis {
	t.Helper()
	out, err := exec.Command(ffprobe, "-v", "error", "-show_streams", "-show_chapters", "-show_format",
		"-of", "json", datei).Output()
	if err != nil {
		t.Fatalf("ffprobe: %v", err)
	}
	var e probeErgebnis
	if err := json.Unmarshal(out, &e); err != nil {
		t.Fatal(err)
	}
	return e
}

// dvdOrdnerBauen: ein VIDEO_TS mit Menü, Titel 1 (4 s, Titelsatz 1) und
// dem Hauptfilm (8 s, zwei Kapitel, zwei Tonspuren) in Titelsatz 2,
// gefolgt von einem Extra (3 s). Der Hauptfilm geht über zwei VOBs.
func dvdOrdnerBauen(t *testing.T, ffmpeg, dir string) {
	t.Helper()
	roh := t.TempDir()
	vobBauen(t, ffmpeg, filepath.Join(roh, "menu.vob"), 1, 1)
	vobBauen(t, ffmpeg, filepath.Join(roh, "t1.vob"), 4, 1)
	vobBauen(t, ffmpeg, filepath.Join(roh, "haupt.vob"), 8, 2)
	vobBauen(t, ffmpeg, filepath.Join(roh, "extra.vob"), 3, 1)
	haupt, extra := groesse(t, filepath.Join(roh, "haupt.vob")), groesse(t, filepath.Join(roh, "extra.vob"))
	if haupt%sektor != 0 || extra%sektor != 0 {
		t.Fatalf("keine ganzen Sektoren: %d %d", haupt, extra)
	}
	hs, es := haupt/sektor, extra/sektor
	dateiAnhaengen(t, filepath.Join(dir, "VIDEO_TS.VOB"), filepath.Join(roh, "menu.vob"))
	dateiAnhaengen(t, filepath.Join(dir, "VTS_01_0.VOB"), filepath.Join(roh, "menu.vob"))
	dateiAnhaengen(t, filepath.Join(dir, "VTS_01_1.VOB"), filepath.Join(roh, "t1.vob"))
	dateiAnhaengen(t, filepath.Join(dir, "VTS_02_0.VOB"), filepath.Join(roh, "menu.vob"))
	// Titelsatz 2 = Hauptfilm + Extra, an einer Sektorgrenze in zwei
	// Dateien geteilt (auf der Scheibe je höchstens 1 GB).
	dateiAnhaengen(t, filepath.Join(roh, "vts2"), filepath.Join(roh, "haupt.vob"), filepath.Join(roh, "extra.vob"))
	b, _ := os.ReadFile(filepath.Join(roh, "vts2"))
	teil := (hs / 2) * sektor
	schreiben(t, dir, "VTS_02_1.VOB", b[:teil])
	schreiben(t, dir, "VTS_02_2.VOB", b[teil:])
	schreiben(t, dir, "VIDEO_TS.IFO", vmgBauen([][2]int{{1, 1}, {2, 1}, {2, 2}}))
	schreiben(t, dir, "VTS_01_0.IFO", vtsBauen(false, []string{"de"}, nil, [][][2]int{{{1, 1}}},
		[]testPGC{{zellen: []testZelle{{0, groesse(t, filepath.Join(dir, "VTS_01_1.VOB"))/sektor - 1, 4, 0}},
			programm: []int{1}, ton: []int{0}}}))
	schreiben(t, dir, "VTS_02_0.IFO", vtsBauen(false, []string{"de", "en"}, nil,
		[][][2]int{{{1, 1}, {1, 2}}, {{2, 1}}},
		[]testPGC{
			{zellen: []testZelle{{0, hs/2 - 1, 4, 0}, {hs / 2, hs - 1, 4, 0}}, programm: []int{1, 2}, ton: []int{0, 1}},
			{zellen: []testZelle{{hs, hs + es - 1, 3, 0}}, programm: []int{1}, ton: []int{0}},
		}))
}

func TestDVDOrdnerUmwandeln(t *testing.T) {
	ffmpeg, ffprobe := werkzeug(t, "ffmpeg"), werkzeug(t, "ffprobe")
	dir := filepath.Join(t.TempDir(), "VIDEO_TS")
	os.MkdirAll(dir, 0o755)
	dvdOrdnerBauen(t, ffmpeg, dir)

	dateien, _ := OrdnerLesen(dir)
	p, err := PlanDVD(dir, dateien)
	if err != nil {
		t.Fatal(err)
	}
	if p.Ausweg || p.DauerMS != 8000 {
		t.Fatalf("Plan: Ausweg %v, Dauer %d", p.Ausweg, p.DauerMS)
	}
	ctx := context.Background()
	gefunden, err := SpurenFinden(ctx, ffprobe, p)
	if err != nil {
		t.Fatal(err)
	}
	spuren := Auswahl(p, gefunden)
	if len(spuren) != 3 || spuren[0].Art != "v" || spuren[1].Sprache != "ger" || spuren[2].Sprache != "eng" {
		t.Fatalf("Auswahl %+v (gefunden %+v)", spuren, gefunden)
	}
	meta, err := KapitelDatei(p, t.TempDir())
	if err != nil || meta == "" {
		t.Fatalf("Kapitel: %v", err)
	}
	ziel := filepath.Join(t.TempDir(), "Film (2001).mkv.teil")
	var zuletzt int64
	if err := Umwandeln(ctx, ffmpeg, p, spuren, meta, ziel, nil, func(n int64) { zuletzt = n }); err != nil {
		t.Fatal(err)
	}
	if zuletzt != p.Gesamt() {
		t.Errorf("Fortschritt %d von %d", zuletzt, p.Gesamt())
	}
	e := proben(t, ffprobe, ziel)
	dauer, _ := strconv.ParseFloat(e.Format.Duration, 64)
	if dauer < 7 || dauer > 9.5 {
		t.Errorf("Dauer %.2f s, erwartet etwa 8 (ohne Extra und Menü)", dauer)
	}
	var ton []string
	for _, s := range e.Streams {
		if s.CodecType == "audio" {
			ton = append(ton, s.Tags["language"])
		}
		if s.CodecType == "video" && s.CodecName != "mpeg2video" {
			t.Errorf("Bild neu gerechnet: %s", s.CodecName)
		}
	}
	if strings.Join(ton, ",") != "ger,eng" {
		t.Errorf("Tonspuren %v", ton)
	}
	if len(e.Chapters) != 2 {
		t.Errorf("%d Kapitel, erwartet 2", len(e.Chapters))
	}
}

// Abbruch: Ein beendeter Kontext beendet FFmpeg und meldet den Abbruch.
func TestUmwandelnAbbruch(t *testing.T) {
	ffmpeg := werkzeug(t, "ffmpeg")
	dir := filepath.Join(t.TempDir(), "VIDEO_TS")
	os.MkdirAll(dir, 0o755)
	dvdOrdnerBauen(t, ffmpeg, dir)
	dateien, _ := OrdnerLesen(dir)
	p, err := PlanDVD(dir, dateien)
	if err != nil {
		t.Fatal(err)
	}
	ctx, ab := context.WithCancel(context.Background())
	ab()
	ziel := filepath.Join(t.TempDir(), "x.mkv.teil")
	err = Umwandeln(ctx, ffmpeg, p, nil, "", ziel, nil, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Fehler %v, erwartet Abbruch", err)
	}
	// Auch mitten im Lauf: der Leser hört auf.
	ctx2, ab2 := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer ab2()
	l := NeuerLeser(ctx2, p)
	time.Sleep(60 * time.Millisecond)
	if _, err := l.Read(make([]byte, 10)); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Leser nach Abbruch: %v", err)
	}
}
