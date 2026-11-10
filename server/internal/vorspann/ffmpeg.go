// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package vorspann

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"lumen/internal/stream"
)

// ErrOhneTon: Die Datei hat keine Tonspur — da gibt es nichts zu erkennen,
// und es lohnt sich auch nicht, es noch einmal zu versuchen.
var ErrOhneTon = errors.New("keine tonspur")

// begrenzt hält höchstens 4 KB Fehlerausgabe fest.
type begrenzt struct{ bytes.Buffer }

func (b *begrenzt) Write(p []byte) (int, error) {
	if rest := 4096 - b.Len(); rest > 0 {
		b.Buffer.Write(p[:min(len(p), rest)])
	}
	return len(p), nil
}

// AusDatei rechnet den Abdruck eines Stücks der Datei: ab Sekunde ab,
// laenge Sekunden lang, erste Tonspur. FFmpeg läuft mit niedrigster
// Priorität und einem Faden; der Ton wird gleich verarbeitet und nicht
// aufbewahrt. Video, Untertitel und Daten werden gar nicht erst gelesen
// (-vn -sn -dn) — bleibt das Entpacken des Containers und das Decodieren
// eines Tons, auch auf einem schwachen NAS ein Vielfaches schneller als
// in Echtzeit.
func AusDatei(ctx context.Context, ffmpeg, pfad string, ab, laenge float64) (*Abdruck, error) {
	if laenge <= 0 {
		return nil, fmt.Errorf("leeres stück")
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Minute)
	defer cancel()
	args := []string{"-hide_banner", "-loglevel", "error", "-nostdin", "-threads", "1"}
	if ab > 0 {
		args = append(args, "-ss", strconv.FormatFloat(ab, 'f', 3, 64))
	}
	args = append(args, "-i", pfad, "-t", strconv.FormatFloat(laenge, 'f', 3, 64),
		"-map", "0:a:0", "-vn", "-sn", "-dn", "-ac", "1", "-ar", strconv.Itoa(Abtastrate),
		"-f", "s16le", "pipe:1")
	cmd := exec.CommandContext(ctx, ffmpeg, args...)
	var fehl begrenzt
	cmd.Stderr = &fehl
	aus, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := stream.NiedrigStarten(cmd); err != nil {
		return nil, err
	}
	a, leseFehler := Lesen(aus, ab, laenge+1)
	// Den Rest wegwerfen, damit FFmpeg nicht an einer vollen Leitung hängt.
	_, _ = io.Copy(io.Discard, aus)
	err = cmd.Wait()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		text := strings.TrimSpace(fehl.String())
		if strings.Contains(text, "matches no streams") {
			return nil, ErrOhneTon
		}
		return nil, fmt.Errorf("ffmpeg: %s", text)
	}
	if leseFehler != nil {
		return nil, leseFehler
	}
	return a, nil
}
