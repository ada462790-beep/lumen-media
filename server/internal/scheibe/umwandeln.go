// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package scheibe

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Wie weit FFmpeg in den Datenstrom schaut, um die Spuren zu finden. Auf
// einer DVD tauchen Untertitel oft erst nach Minuten auf; 400 MB sind bei
// DVD-Datenraten etwa zehn Minuten.
const (
	probeBytes = 400 << 20
	probeZeit  = "600000000" // µs
)

// Gefunden ist eine Spur, wie ffprobe sie im Datenstrom sieht.
type Gefunden struct {
	ID    int
	Art   string // "v", "a", "s", sonst ""
	Codec string
}

func eingabeFormat(art string) string {
	if art == BluRay {
		return "mpegts"
	}
	return "mpeg"
}

// SpurenFinden lässt ffprobe auf den Anfang des Hauptfilms schauen —
// dieselben Bytes, die FFmpeg nachher als Erstes bekommt, mit derselben
// Suchweite. So findet FFmpeg genau die Spuren, die hier stehen.
func SpurenFinden(ctx context.Context, ffprobe string, p *Plan) ([]Gefunden, error) {
	l := NeuerLeser(ctx, p)
	defer l.Close()
	cmd := exec.CommandContext(ctx, ffprobe, "-v", "error", "-probesize", strconv.Itoa(probeBytes),
		"-analyzeduration", probeZeit, "-f", eingabeFormat(p.Art), "-i", "pipe:0",
		"-show_entries", "stream=index,id,codec_type,codec_name", "-of", "json")
	cmd.Stdin = io.LimitReader(l, probeBytes+(64<<20))
	var fehl bytes.Buffer
	cmd.Stderr = &fehl
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("ffprobe: %s", letzteZeile(fehl.String(), err))
	}
	var j struct {
		Streams []struct {
			ID        string `json:"id"`
			CodecType string `json:"codec_type"`
			CodecName string `json:"codec_name"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(out, &j); err != nil {
		return nil, err
	}
	var liste []Gefunden
	for _, s := range j.Streams {
		id, err := strconv.ParseInt(strings.TrimPrefix(strings.ToLower(s.ID), "0x"), 16, 64)
		if err != nil {
			continue
		}
		art := ""
		switch s.CodecType {
		case "video":
			art = "v"
		case "audio":
			art = "a"
		case "subtitle":
			art = "s"
		}
		liste = append(liste, Gefunden{ID: int(id), Art: art, Codec: s.CodecName})
	}
	return liste, nil
}

// Auswahl: die Spuren, die in die MKV kommen, in dieser Reihenfolge — erst
// die aus dem Plan (mit Sprache), die ffprobe auch gefunden hat, dann was
// ffprobe sonst noch an Bild, Ton und Untertitel fand (ohne Sprache).
// Videos nur eines: Ein zweites Bild ist auf einer Scheibe ein
// Standbild-Menü oder Bild-im-Bild.
func Auswahl(p *Plan, gefunden []Gefunden) []Ausgewaehlt {
	da := map[int]Gefunden{}
	for _, g := range gefunden {
		if g.Art != "" {
			da[g.ID] = g
		}
	}
	var out []Ausgewaehlt
	genommen := map[int]bool{}
	video := false
	nimm := func(g Gefunden, spr string) {
		if genommen[g.ID] || (g.Art == "v" && video) {
			return
		}
		genommen[g.ID] = true
		if g.Art == "v" {
			video = true
		}
		out = append(out, Ausgewaehlt{Gefunden: g, Sprache: spr})
	}
	// Bild zuerst, dann Ton, dann Untertitel — wie man es erwartet.
	for _, art := range []string{"v", "a", "s"} {
		for _, s := range p.Spuren {
			if g, ok := da[s.ID]; ok && s.Art == art && g.Art == art {
				nimm(g, s.Sprache)
			}
		}
		for _, g := range gefunden {
			if g.Art == art {
				nimm(g, "")
			}
		}
	}
	return out
}

// Ausgewaehlt ist eine Spur der MKV.
type Ausgewaehlt struct {
	Gefunden
	Sprache string
}

// Argumente: der Aufruf von FFmpeg. meta ist die Datei mit den Kapiteln
// ("" ohne), ziel die (vorläufige) MKV.
func Argumente(p *Plan, spuren []Ausgewaehlt, meta, ziel string) []string {
	a := []string{"-hide_banner", "-loglevel", "error", "-nostdin", "-y",
		"-fflags", "+genpts", "-probesize", strconv.Itoa(probeBytes), "-analyzeduration", probeZeit,
		"-f", eingabeFormat(p.Art), "-i", "pipe:0"}
	if meta != "" {
		a = append(a, "-f", "ffmetadata", "-i", meta)
	}
	for _, s := range spuren {
		a = append(a, "-map", fmt.Sprintf("0:i:0x%x", s.ID))
	}
	if len(spuren) == 0 {
		a = append(a, "-map", "0:v:0", "-map", "0:a?", "-map", "0:s?")
	}
	if meta != "" {
		a = append(a, "-map_chapters", "1")
	} else {
		a = append(a, "-map_chapters", "-1")
	}
	a = append(a, "-map_metadata", "-1", "-c", "copy")
	// LPCM von DVD und Blu-ray nimmt Matroska nicht auf; FLAC ist
	// verlustfrei und spart dabei Platz.
	for i, s := range spuren {
		if s.Codec == "pcm_dvd" || s.Codec == "pcm_bluray" {
			a = append(a, fmt.Sprintf("-c:%d", i), "flac")
		}
		if s.Sprache != "" {
			a = append(a, fmt.Sprintf("-metadata:s:%d", i), "language="+s.Sprache)
		}
	}
	a = append(a, "-max_muxing_queue_size", "8192", "-f", "matroska", ziel)
	return a
}

// KapitelDatei schreibt die Kapitel als FFmetadata ("" wenn es keine gibt).
func KapitelDatei(p *Plan, dir string) (string, error) {
	if len(p.Kapitel) < 2 || p.DauerMS <= 0 {
		return "", nil
	}
	f, err := os.CreateTemp(dir, "lumen-kapitel-*.txt")
	if err != nil {
		return "", err
	}
	defer f.Close()
	fmt.Fprintln(f, ";FFMETADATA1")
	for i, start := range p.Kapitel {
		ende := p.DauerMS
		if i+1 < len(p.Kapitel) {
			ende = p.Kapitel[i+1]
		}
		if ende <= start {
			continue
		}
		fmt.Fprintf(f, "\n[CHAPTER]\nTIMEBASE=1/1000\nSTART=%d\nEND=%d\n", start, ende)
	}
	return f.Name(), nil
}

// Umwandeln führt FFmpeg aus: die Bytes des Plans über die
// Standardeingabe, die MKV nach ziel. ausfuehren startet den Befehl und
// wartet (nil: exec.Cmd.CombinedOutput; Lumen senkt dort die Priorität).
// melden bekommt etwa jede Sekunde die Zahl der gelesenen Bytes.
func Umwandeln(ctx context.Context, ffmpeg string, p *Plan, spuren []Ausgewaehlt, meta, ziel string,
	ausfuehren func(*exec.Cmd) ([]byte, error), melden func(gelesen int64)) error {
	l := NeuerLeser(ctx, p)
	defer l.Close()
	cmd := exec.CommandContext(ctx, ffmpeg, Argumente(p, spuren, meta, ziel)...)
	cmd.Stdin = l
	if ausfuehren == nil {
		ausfuehren = func(c *exec.Cmd) ([]byte, error) { return c.CombinedOutput() }
	}
	fertig := make(chan struct{})
	if melden != nil {
		go func() {
			t := time.NewTicker(time.Second)
			defer t.Stop()
			for {
				select {
				case <-fertig:
					return
				case <-t.C:
					melden(l.Gelesen.Load())
				}
			}
		}()
	}
	out, err := ausfuehren(cmd)
	close(fertig)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return fmt.Errorf("ffmpeg: %s", letzteZeile(string(out), err))
	}
	if melden != nil {
		melden(l.Gelesen.Load())
	}
	return nil
}
