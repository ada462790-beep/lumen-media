// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package stream

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Was hier herauskommt, muss ein Browser abspielen können. Prüfen lässt
// sich das stellvertretend mit ffprobe: findet der die Spuren, findet der
// Browser sie auch.
func TestRemuxProducesBrowserPlayableStream(t *testing.T) {
	src := testFilm(t, 12)
	ff := NewFFmpeg("", "")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	body, stop, err := ff.Remux(ctx, RemuxJob{
		SourcePath: src, Plan: Plan{VideoCodec: "copy"}, AudioIndex: -1})
	if err != nil {
		t.Fatal(err)
	}
	defer stop()

	out := filepath.Join(t.TempDir(), "strom.mp4")
	f, err := os.Create(out)
	if err != nil {
		t.Fatal(err)
	}
	n, err := io.Copy(f, body)
	f.Close()
	if err != nil && err != io.EOF {
		t.Fatal(err)
	}
	if n < 10_000 {
		t.Fatalf("nur %d Bytes gekommen", n)
	}

	probe, err := exec.Command("ffprobe", "-v", "error", "-show_entries",
		"stream=codec_type,codec_name", "-of", "csv=p=0", out).Output()
	if err != nil {
		t.Fatalf("der Strom ist nicht abspielbar: %v", err)
	}
	got := string(probe)
	if !strings.Contains(got, "video") || !strings.Contains(got, "audio") {
		t.Fatalf("Bild oder Ton fehlt: %q", got)
	}
	// Umgepackt heisst: das Bild wurde nicht neu gerechnet.
	if !strings.Contains(got, "h264") {
		t.Errorf("das Bild wurde offenbar neu codiert: %q", got)
	}
	if !strings.Contains(got, "aac") {
		t.Errorf("der Ton ist kein AAC: %q", got)
	}
}

// Beim Springen muss der Strom wirklich später anfangen.
func TestRemuxStartsAtTheGivenSecond(t *testing.T) {
	src := testFilm(t, 30)
	ff := NewFFmpeg("", "")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	body, stop, err := ff.Remux(ctx, RemuxJob{
		SourcePath: src, StartSec: 20,
		Plan: Plan{VideoCodec: "copy"}, AudioIndex: -1})
	if err != nil {
		t.Fatal(err)
	}
	defer stop()

	out := filepath.Join(t.TempDir(), "sprung.mp4")
	f, _ := os.Create(out)
	_, _ = io.Copy(f, body)
	f.Close()

	// Der Strom selbst beginnt bei null; entscheidend ist, dass er nur noch
	// den Rest des Films enthält. Wo er im Film steht, sagt KeyframeBefore —
	// das prüft TestKeyframeBeforeIsHonest.
	d, err := exec.Command("ffprobe", "-v", "error", "-show_entries",
		"format=duration", "-of", "csv=p=0", out).Output()
	if err != nil {
		t.Fatal(err)
	}
	dur := parseSeconds(strings.TrimSpace(string(d)))
	if dur > 16 {
		t.Errorf("der Strom ist %.1f s lang — aus 30 s ab Sekunde 20 "+
			"dürfen höchstens gut zehn übrig bleiben", dur)
	}
	if dur < 4 {
		t.Errorf("nur %.1f s gekommen — da ist zu viel abgeschnitten", dur)
	}
}

// Ein Browser bekommt nur, was er auch kann.
func TestBrowserDeviceIsConservative(t *testing.T) {
	d := BrowserDevice(0)
	p := Decide(FileInfo("hevc", "mkv", 2160, 40_000_000, "DV"), d, 0)
	if p.Direct {
		t.Error("HEVC dürfte im Browser nicht als direkt gelten")
	}
	if CanRemux(p) && p.VideoCodec == "copy" {
		t.Error("HEVC darf nicht bloss umgepackt werden")
	}
	// Der häufigste Fall: H.264 in MKV — umpacken genügt.
	p = Decide(FileInfo("h264", "mkv", 1080, 8_000_000, ""), d, 0)
	if !CanRemux(p) {
		t.Error("H.264 in MKV müsste sich umpacken lassen")
	}
}

func parseSeconds(s string) float64 {
	var v, frac float64
	seen := false
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			if seen {
				frac /= 10
				v += float64(r-'0') * frac
			} else {
				v = v*10 + float64(r-'0')
			}
		case r == '.':
			seen, frac = true, 1
		default:
			return v
		}
	}
	return v
}

// Wer auf eine Stelle springt, muss erfahren, wo der Strom wirklich anfängt —
// sonst zeigt die Oberfläche die falsche Minute.
func TestKeyframeBeforeIsHonest(t *testing.T) {
	src := testFilm(t, 40)
	ff := NewFFmpeg("", "")
	ctx := context.Background()

	for _, want := range []float64{0, 12, 25, 33} {
		got := ff.KeyframeBefore(ctx, src, want)
		if got > want {
			t.Errorf("für %.0f s kam %.2f s zurück — das liegt hinter der Stelle", want, got)
		}
		if want-got > 20 {
			t.Errorf("für %.0f s kam %.2f s zurück — das ist zu weit davor", want, got)
		}
	}
	// Eine kaputte Datei darf nicht hängen, sondern gibt die Stelle zurück.
	bad := filepath.Join(t.TempDir(), "kaputt.mkv")
	_ = os.WriteFile(bad, []byte("nix"), 0o644)
	if got := ff.KeyframeBefore(ctx, bad, 30); got != 30 {
		t.Errorf("bei einer kaputten Datei kam %.1f statt 30", got)
	}
}

// Der Durchstich: KeyframeBefore sagt die Stelle, Remux fängt genau dort an.
// Nur wenn beides zusammenpasst, stimmt die Zeitanzeige nach einem Sprung.
func TestSeekLandsExactlyWhereAnnounced(t *testing.T) {
	src := testFilm(t, 40)
	ff := NewFFmpeg("", "")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Wohin der Nutzer springen will.
	const wunsch = 24.0
	start := ff.KeyframeBefore(ctx, src, wunsch)

	body, stop, err := ff.Remux(ctx, RemuxJob{
		SourcePath: src, StartSec: start,
		Plan: Plan{VideoCodec: "copy"}, AudioIndex: -1})
	if err != nil {
		t.Fatal(err)
	}
	defer stop()

	out := filepath.Join(t.TempDir(), "s.mp4")
	f, _ := os.Create(out)
	_, _ = io.Copy(f, body)
	f.Close()

	d, err := exec.Command("ffprobe", "-v", "error", "-show_entries",
		"format=duration", "-of", "csv=p=0", out).Output()
	if err != nil {
		t.Fatal(err)
	}
	rest := parseSeconds(strings.TrimSpace(string(d)))
	istStart := 40 - rest

	// Der Strom muss dort beginnen, wo es dem Browser angekündigt wurde.
	// Mehr als eine Sekunde Abweichung sieht man beim Zuschauen.
	if diff := istStart - start; diff > 1 || diff < -1 {
		t.Errorf("angekündigt %.2f s, tatsächlich %.2f s — die Anzeige stünde "+
			"um %.2f s daneben", start, istStart, diff)
	}
}
