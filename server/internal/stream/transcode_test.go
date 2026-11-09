// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package stream

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// testFilm baut mit FFmpeg einen kurzen Film, in dem die Sekunde eingeblendet
// ist — so lässt sich später prüfen, ob wirklich die richtige Stelle kommt.
func testFilm(t *testing.T, seconds int) string {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ohne FFmpeg nicht prüfbar")
	}
	path := filepath.Join(t.TempDir(), "film.mkv")
	cmd := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "testsrc=size=640x360:rate=25:duration="+itoa(seconds),
		"-f", "lavfi", "-i", "sine=frequency=440:duration="+itoa(seconds),
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p",
		"-c:a", "aac", path, "-y")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Testfilm liess sich nicht bauen: %v\n%s", err, out)
	}
	return path
}

func newTr(t *testing.T) *Transcoder {
	t.Helper()
	tr, err := NewTranscoder(NewFFmpeg("", ""), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(tr.StopAll)
	return tr
}

// Die Abspielliste steht vollständig da, bevor irgendetwas gerechnet wurde.
// Nur deshalb kann ein Gerät sofort an jede Stelle springen.
func TestPlaylistIsCompleteUpfront(t *testing.T) {
	tr := newTr(t)
	// Ein Film von 1:30:07 — also 1352 ganze Segmente und ein Rest.
	pl := tr.Playlist(Job{DurationMS: (90*60 + 7) * 1000})

	if !strings.Contains(pl, "#EXT-X-PLAYLIST-TYPE:VOD") {
		t.Error("die Liste ist nicht als vollständig gekennzeichnet")
	}
	if !strings.HasSuffix(strings.TrimSpace(pl), "#EXT-X-ENDLIST") {
		t.Error("die Liste hat kein Ende — dann lässt sich nicht springen")
	}
	n := strings.Count(pl, ".ts")
	if n != 1352 {
		t.Errorf("%d Segmente, erwartet 1352", n)
	}
	// Das letzte Segment ist der Rest, nicht volle vier Sekunden.
	if !strings.Contains(pl, "#EXTINF:3.000000,\nseg01351.ts") {
		t.Errorf("Restsegment fehlt oder stimmt nicht:\n%s", tail(pl, 4))
	}
}

// Der Durchstich: umwandeln, Segmente abholen, prüfen dass sie abspielbar
// sind — und dass jedes für sich beginnt, sonst gäbe es kein Spulen.
func TestTranscodeProducesPlayableSegments(t *testing.T) {
	tr := newTr(t)
	src := testFilm(t, 24)

	j := Job{SessionID: "s1", SourcePath: src, DurationMS: 24_000,
		Plan:       Plan{VideoCodec: "h264", Height: 360, Bitrate: 1_500_000},
		AudioIndex: -1}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	for _, idx := range []int{0, 1, 2} {
		path, err := tr.Segment(ctx, j, idx)
		if err != nil {
			t.Fatalf("Segment %d: %v", idx, err)
		}
		info, err := os.Stat(path)
		if err != nil || info.Size() == 0 {
			t.Fatalf("Segment %d ist leer", idx)
		}
		// Wirklich abspielbar? FFprobe muss es alleine öffnen können.
		out, err := exec.Command("ffprobe", "-v", "error", "-show_entries",
			"format=duration", "-of", "csv=p=0", path).Output()
		if err != nil {
			t.Fatalf("Segment %d ist nicht abspielbar: %v", idx, err)
		}
		if strings.TrimSpace(string(out)) == "" {
			t.Fatalf("Segment %d hat keine Laufzeit", idx)
		}
	}
}

// Springt jemand mitten in den Film, darf nicht alles davor gerechnet
// werden. FFmpeg muss direkt dort ansetzen.
func TestSeekRestartsAtTheRightPlace(t *testing.T) {
	tr := newTr(t)
	src := testFilm(t, 120)

	j := Job{SessionID: "s2", SourcePath: src, DurationMS: 120_000,
		Plan:       Plan{VideoCodec: "h264", Height: 360, Bitrate: 1_500_000},
		AudioIndex: -1}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// Erst ganz am Anfang anfangen …
	if _, err := tr.Segment(ctx, j, 0); err != nil {
		t.Fatalf("Anfang: %v", err)
	}
	// … dann auf Minute 1:36 springen (Segment 24).
	start := time.Now()
	path, err := tr.Segment(ctx, j, 24)
	if err != nil {
		t.Fatalf("Sprung: %v", err)
	}
	took := time.Since(start)

	// Wäre der Film bis dahin durchgerechnet worden, dauerte das ein
	// Vielfaches. Die Grenze ist grosszügig, damit der Test auf einem
	// lahmen Rechner nicht grundlos rot wird.
	if took > 40*time.Second {
		t.Errorf("der Sprung dauerte %s — da wurde wohl alles davor gerechnet", took)
	}
	if info, err := os.Stat(path); err != nil || info.Size() == 0 {
		t.Fatalf("Segment 24 fehlt: %v", err)
	}
	if !strings.HasSuffix(path, "seg00024.ts") {
		t.Errorf("falsches Segment geliefert: %s", path)
	}
}

// Beim Beenden muss alles weg sein: kein laufendes FFmpeg, keine Segmente
// auf der Platte.
func TestStopCleansUp(t *testing.T) {
	tr := newTr(t)
	src := testFilm(t, 20)

	j := Job{SessionID: "s3", SourcePath: src, DurationMS: 20_000,
		Plan: Plan{VideoCodec: "h264", Height: 360}, AudioIndex: -1}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if _, err := tr.Segment(ctx, j, 0); err != nil {
		t.Fatal(err)
	}
	if tr.Active() != 1 {
		t.Fatalf("erwartet eine laufende Umwandlung, %d gezählt", tr.Active())
	}

	dir := filepath.Join(tr.workDir, "s3")
	tr.Stop("s3")

	if tr.Active() != 0 {
		t.Error("die Umwandlung läuft weiter")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Error("die Segmente liegen noch auf der Platte")
	}
}

// Eine kaputte Datei darf nicht in eine Endlosschleife laufen, sondern muss
// einen verständlichen Fehler geben.
func TestBrokenFileFailsClearly(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ohne FFmpeg nicht prüfbar")
	}
	tr := newTr(t)
	bad := filepath.Join(t.TempDir(), "kaputt.mkv")
	if err := os.WriteFile(bad, []byte("das ist kein film"), 0o644); err != nil {
		t.Fatal(err)
	}
	j := Job{SessionID: "s4", SourcePath: bad, DurationMS: 60_000,
		Plan: Plan{VideoCodec: "h264", Height: 360}, AudioIndex: -1}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	start := time.Now()
	_, err := tr.Segment(ctx, j, 0)
	if err == nil {
		t.Fatal("eine kaputte Datei lieferte ein Segment")
	}
	if time.Since(start) > 20*time.Second {
		t.Errorf("der Fehler kam erst nach %s", time.Since(start))
	}
	if !strings.Contains(err.Error(), "umwandlung fehlgeschlagen") {
		t.Errorf("unverständlicher Fehler: %v", err)
	}
}

func tail(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
