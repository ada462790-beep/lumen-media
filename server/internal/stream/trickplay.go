// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package stream

// Vorschaubilder beim Spulen („Trickplay").
//
// Wer über die Zeitleiste fährt, soll sehen, wo er landet. Dafür braucht
// es ein Bild alle zehn Sekunden — bei einem Zweistundenfilm 720 Stück.
// Einzeln wären das 720 Dateien und 720 Anfragen; stattdessen kommen je
// hundert auf ein Blatt (10 × 10), wie bei Emby und Jellyfin. Das Gerät
// lädt ein Blatt und schneidet sich das Bild selbst heraus.
//
// Gerechnet wird nur aus Schlüsselbildern (-skip_frame nokey): Das ist
// ein Bruchteil der Arbeit eines vollen Decodierens, und auf zehn Sekunden
// genau braucht es niemand. HDR wird auch hier für SDR umgerechnet, sonst
// wären die Bildchen blass.

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Die Masse der Vorschaubilder.
const (
	TrickplayIntervall = 10 // Sekunden zwischen zwei Bildern
	TrickplayBreite    = 320
	TrickplaySpalten   = 10
	TrickplayZeilen    = 10
)

// TrickplayInfo beschreibt fertige Vorschaubilder.
type TrickplayInfo struct {
	Intervall int `json:"intervall"`
	Breite    int `json:"breite"`
	Hoehe     int `json:"hoehe"`
	Spalten   int `json:"spalten"`
	Zeilen    int `json:"zeilen"`
	Bilder    int `json:"bilder"`
	Blaetter  int `json:"blaetter"`
}

// TrickplayHoehe rechnet die Höhe eines Bildchens aus dem Seitenverhältnis
// (gerade, damit JPEG und Kachelung nicht stolpern).
func TrickplayHoehe(breite, hoehe int) int {
	if breite <= 0 || hoehe <= 0 {
		return 180
	}
	h := int(math.Round(float64(TrickplayBreite)*float64(hoehe)/float64(breite)/2)) * 2
	if h < 40 {
		h = 40
	}
	if h > 640 {
		h = 640
	}
	return h
}

// TrickplayLesen liest die Beschreibung fertiger Vorschaubilder. Fehlt sie,
// sind sie (noch) nicht fertig.
func TrickplayLesen(ordner string) (*TrickplayInfo, error) {
	b, err := os.ReadFile(filepath.Join(ordner, "info.json"))
	if err != nil {
		return nil, err
	}
	var i TrickplayInfo
	if err := json.Unmarshal(b, &i); err != nil {
		return nil, err
	}
	return &i, nil
}

// TrickplayErzeugen rechnet die Blätter in einen frischen Ordner und
// benennt ihn erst um, wenn alles da ist — ein halb fertiger Ordner wird
// nie ausgeliefert. Läuft mit niedrigster Priorität.
func (f *FFmpeg) TrickplayErzeugen(ctx context.Context, quelle string, dauerMS int64,
	d *BildDetails, ziel string) (*TrickplayInfo, error) {
	if dauerMS <= 0 {
		return nil, fmt.Errorf("laufzeit unbekannt")
	}
	breite, hoehe := 0, 0
	if d != nil {
		breite, hoehe = d.Breite, d.Hoehe
	}
	h := TrickplayHoehe(breite, hoehe)
	info := &TrickplayInfo{Intervall: TrickplayIntervall, Breite: TrickplayBreite, Hoehe: h,
		Spalten: TrickplaySpalten, Zeilen: TrickplayZeilen}
	info.Bilder = int(math.Ceil(float64(dauerMS) / 1000 / TrickplayIntervall))
	if info.Bilder < 1 {
		info.Bilder = 1
	}
	proBlatt := TrickplaySpalten * TrickplayZeilen
	info.Blaetter = (info.Bilder + proBlatt - 1) / proBlatt

	if err := os.MkdirAll(filepath.Dir(ziel), 0o755); err != nil {
		return nil, err
	}
	tmp, err := os.MkdirTemp(filepath.Dir(ziel), ".entsteht-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)

	// eof_action=pass: Sonst fällt bei kurzen Dateien das letzte (oder
	// einzige) Bild weg.
	filter := []string{fmt.Sprintf("fps=1/%d:eof_action=pass", TrickplayIntervall),
		fmt.Sprintf("scale=%d:%d", TrickplayBreite, h)}
	if d.HDR() {
		filter = append(filter, f.Faehigkeiten().TonemapKette(d.HLG()))
	} else {
		filter = append(filter, "format=yuvj420p")
	}
	filter = append(filter, fmt.Sprintf("tile=%dx%d", TrickplaySpalten, TrickplayZeilen))

	cmd := exec.CommandContext(ctx, f.FFmpegPath, "-hide_banner", "-loglevel", "error", "-nostdin",
		"-skip_frame", "nokey", "-i", quelle,
		"-map", "0:v:0", "-an", "-sn", "-dn",
		"-vf", strings.Join(filter, ","),
		"-q:v", "5", "-start_number", "0", "-y",
		filepath.Join(tmp, "%d.jpg"))
	if out, err := Niedrig(cmd, true); err != nil {
		return nil, fmt.Errorf("ffmpeg: %s", Bereinigt(lastLine(string(out))))
	}
	// Wie viele Blätter es wirklich wurden: Ist die Datei kürzer als ihre
	// Laufzeit vermuten liess, eben weniger.
	n := 0
	for ; ; n++ {
		if _, err := os.Stat(filepath.Join(tmp, fmt.Sprintf("%d.jpg", n))); err != nil {
			break
		}
	}
	if n == 0 {
		return nil, fmt.Errorf("kein einziges vorschaubild")
	}
	if n < info.Blaetter {
		info.Blaetter = n
		if max := n * proBlatt; info.Bilder > max {
			info.Bilder = max
		}
	}
	b, _ := json.Marshal(info)
	if err := os.WriteFile(filepath.Join(tmp, "info.json"), b, 0o644); err != nil {
		return nil, err
	}
	_ = os.RemoveAll(ziel)
	if err := os.Rename(tmp, ziel); err != nil {
		return nil, err
	}
	return info, nil
}
