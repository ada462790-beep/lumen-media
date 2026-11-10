// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package vorspann

import (
	"errors"
	"math"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestAusDatei(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("kein FFmpeg")
	}
	dir := t.TempDir()
	stumm := filepath.Join(dir, "stumm.mkv")
	mitTon := filepath.Join(dir, "ton.mkv")
	for _, c := range [][]string{
		{"-f", "lavfi", "-i", "color=c=black:s=32x32:r=5:d=3", "-c:v", "libx264", "-preset", "ultrafast", stumm},
		{"-f", "lavfi", "-i", "color=c=black:s=32x32:r=5:d=20", "-f", "lavfi", "-i", "anoisesrc=d=20:c=pink:r=22050",
			"-map", "0:v", "-map", "1:a", "-c:v", "libx264", "-preset", "ultrafast", "-c:a", "aac", "-shortest", mitTon},
	} {
		if out, err := exec.Command("ffmpeg", append([]string{"-hide_banner", "-loglevel", "error", "-y"}, c...)...).CombinedOutput(); err != nil {
			t.Skipf("FFmpeg baut kein Testvideo: %v %s", err, out)
		}
	}
	if _, err := AusDatei(t.Context(), "ffmpeg", stumm, 0, 3); !errors.Is(err, ErrOhneTon) {
		t.Errorf("ohne Ton: %v", err)
	}
	a, err := AusDatei(t.Context(), "ffmpeg", mitTon, 5, 10)
	if err != nil {
		t.Fatal(err)
	}
	if a.Ab != 5 || math.Abs(a.Sekunden()-10) > 0.5 {
		t.Errorf("Stück: ab %.1f, %.1f s", a.Ab, a.Sekunden())
	}
	if _, err := AusDatei(t.Context(), "ffmpeg", filepath.Join(dir, "gibtsnicht.mkv"), 0, 3); err == nil || errors.Is(err, ErrOhneTon) {
		t.Errorf("fehlende Datei: %v", err)
	}
}
