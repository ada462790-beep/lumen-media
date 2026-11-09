// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package stream

// Feinmessung: was die Datenbank über das Bild nicht weiss.
//
// Beim Scan merkt sich Lumen Codec, Auflösung und HDR — genug für „direkt
// oder umwandeln". Für die Master-Liste von HLS braucht das Gerät aber
// mehr: das genaue Profil und Level (CODECS), das Seitenverhältnis
// (RESOLUTION) und ob das Bild HDR ist (VIDEO-RANGE). Steht dort etwas
// Falsches, lehnt AVPlayer eine an sich abspielbare Fassung ab oder wählt
// den falschen Decoder. Die Messung liest nur den Kopf der Datei und
// dauert einen Bruchteil einer Sekunde; sie wird im Arbeitsspeicher
// gehalten, nicht in der Datenbank — kein Umbau, und nach einem Neustart
// misst Lumen eben noch einmal.

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// BildDetails beschreibt die erste Bildspur genauer.
type BildDetails struct {
	Codec        string
	Profil       string // „High", „Main 10" …
	Level        int    // wie ffprobe es nennt: 41 bei H.264, 153 bei HEVC
	PixFmt       string
	Breite       int
	Hoehe        int
	FPS          float64
	Transfer     string // smpte2084, arib-std-b67, bt709 …
	Primaries    string
	Matrix       string
	Bittiefe     int
	Zeilensprung bool
	// Start ist, wo die Zeitachse der Datei beginnt (meist 0; bei
	// Aufnahmen aus dem Fernsehen oft irgendwo). -ss zählt ab hier, die
	// Zeitstempel im Strom nicht.
	Start float64
}

// HDR sagt, ob das Bild einen erweiterten Helligkeitsumfang hat.
func (d *BildDetails) HDR() bool {
	if d == nil {
		return false
	}
	switch strings.ToLower(d.Transfer) {
	case "smpte2084", "arib-std-b67":
		return true
	}
	return false
}

// HLG unterscheidet die beiden HDR-Arten für VIDEO-RANGE.
func (d *BildDetails) HLG() bool {
	return d != nil && strings.EqualFold(d.Transfer, "arib-std-b67")
}

type feinEintrag struct {
	d       *BildDetails
	groesse int64
	mtime   time.Time
}

var (
	feinMu    sync.Mutex
	feinCache = map[string]feinEintrag{}
)

// Feinmessung misst die Bildspur aus — einmal je Datei und Fassung auf der
// Platte (Grösse und Änderungszeit), danach aus dem Speicher.
func (f *FFmpeg) Feinmessung(ctx context.Context, pfad string) (*BildDetails, error) {
	fi, err := os.Stat(pfad)
	if err != nil {
		return nil, err
	}
	feinMu.Lock()
	if e, ok := feinCache[pfad]; ok && e.groesse == fi.Size() && e.mtime.Equal(fi.ModTime()) {
		feinMu.Unlock()
		return e.d, nil
	}
	feinMu.Unlock()

	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, f.FFprobePath, "-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=codec_name,profile,level,pix_fmt,width,height,r_frame_rate,avg_frame_rate,"+
			"color_transfer,color_primaries,color_space,field_order,bits_per_raw_sample:format=start_time",
		"-of", "json", pfad).Output()
	if err != nil {
		return nil, fmt.Errorf("feinmessung: %w", err)
	}
	d, err := feinAuswerten(out)
	if err != nil {
		return nil, err
	}
	feinMu.Lock()
	if len(feinCache) > 500 {
		// Einfach leeren statt aufwendig verdrängen: die Messung ist billig.
		feinCache = map[string]feinEintrag{}
	}
	feinCache[pfad] = feinEintrag{d: d, groesse: fi.Size(), mtime: fi.ModTime()}
	feinMu.Unlock()
	return d, nil
}

func feinAuswerten(out []byte) (*BildDetails, error) {
	var p struct {
		Streams []struct {
			CodecName      string `json:"codec_name"`
			Profile        string `json:"profile"`
			Level          int    `json:"level"`
			PixFmt         string `json:"pix_fmt"`
			Width          int    `json:"width"`
			Height         int    `json:"height"`
			RFrameRate     string `json:"r_frame_rate"`
			AvgFrameRate   string `json:"avg_frame_rate"`
			ColorTransfer  string `json:"color_transfer"`
			ColorPrimaries string `json:"color_primaries"`
			ColorSpace     string `json:"color_space"`
			FieldOrder     string `json:"field_order"`
			Bits           string `json:"bits_per_raw_sample"`
		} `json:"streams"`
		Format struct {
			StartTime string `json:"start_time"`
		} `json:"format"`
	}
	if err := json.Unmarshal(out, &p); err != nil {
		return nil, fmt.Errorf("feinmessung: %w", err)
	}
	if len(p.Streams) == 0 {
		return nil, fmt.Errorf("feinmessung: keine bildspur")
	}
	s := p.Streams[0]
	d := &BildDetails{Codec: s.CodecName, Profil: s.Profile, Level: s.Level, PixFmt: s.PixFmt,
		Breite: s.Width, Hoehe: s.Height, Transfer: s.ColorTransfer, Primaries: s.ColorPrimaries,
		Matrix: s.ColorSpace}
	d.FPS = bruch(s.AvgFrameRate)
	if d.FPS <= 0 || d.FPS > 240 {
		d.FPS = bruch(s.RFrameRate)
	}
	if b, err := strconv.Atoi(s.Bits); err == nil {
		d.Bittiefe = b
	} else if strings.Contains(d.PixFmt, "10") {
		d.Bittiefe = 10
	} else {
		d.Bittiefe = 8
	}
	if v, err := strconv.ParseFloat(p.Format.StartTime, 64); err == nil && v > 0 {
		d.Start = v
	}
	switch s.FieldOrder {
	case "tt", "bb", "tb", "bt":
		d.Zeilensprung = true
	}
	return d, nil
}

func bruch(s string) float64 {
	a, b, ok := strings.Cut(s, "/")
	if !ok {
		v, _ := strconv.ParseFloat(s, 64)
		return v
	}
	z, err1 := strconv.ParseFloat(a, 64)
	n, err2 := strconv.ParseFloat(b, 64)
	if err1 != nil || err2 != nil || n == 0 {
		return 0
	}
	return z / n
}

// ---------------------------------------------------------------- CODECS

// CodecKennung baut den Eintrag für CODECS in der Master-Liste (RFC 6381).
// Kennt Lumen das Profil nicht genau, nimmt es den häufigsten Fall — eine
// zu hohe Angabe ist harmloser als eine zu niedrige.
func CodecKennung(codec, profil string, level int) string {
	switch strings.ToLower(codec) {
	case "h264", "avc", "avc1":
		pid := "64" // High
		switch strings.ToLower(profil) {
		case "baseline":
			pid = "42"
		case "constrained baseline":
			return fmt.Sprintf("avc1.42E0%02X", levelOder(level, 30))
		case "main":
			pid = "4D"
		case "high 10", "high 10 intra":
			pid = "6E"
		case "high 4:2:2", "high 4:2:2 intra":
			pid = "7A"
		case "high 4:4:4 predictive", "high 4:4:4":
			pid = "F4"
		}
		return fmt.Sprintf("avc1.%s00%02X", pid, levelOder(level, 41))
	case "hevc", "h265":
		// hvc1.<Profil>.<Kompatibilität>.L<Level>.B0 — Main oder Main 10.
		l := levelOder(level, 120)
		if strings.Contains(strings.ToLower(profil), "10") {
			return fmt.Sprintf("hvc1.2.4.L%d.B0", l)
		}
		return fmt.Sprintf("hvc1.1.6.L%d.B0", l)
	case "av1":
		return "av01.0.08M.08"
	}
	return ""
}

func levelOder(l, vorgabe int) int {
	if l <= 0 {
		return vorgabe
	}
	return l
}

// TonKennung ist der CODECS-Eintrag für den Ton.
func TonKennung(codec string) string {
	switch TonName(codec) {
	case "ac3":
		return "ac-3"
	case "eac3":
		return "ec-3"
	case "mp3":
		return "mp4a.40.34"
	case "flac":
		return "fLaC"
	case "opus":
		return "Opus"
	}
	return "mp4a.40.2"
}

// AusgabeGroesse rechnet die Bildgrösse nach dem Verkleinern aus, so wie
// scale=-2:h es tut: Seitenverhältnis bleibt, Breite gerade.
func AusgabeGroesse(breite, hoehe, zielHoehe int) (int, int) {
	if breite <= 0 || hoehe <= 0 {
		if zielHoehe <= 0 {
			zielHoehe = 1080
		}
		return zielHoehe * 16 / 9 / 2 * 2, zielHoehe
	}
	if zielHoehe <= 0 || zielHoehe >= hoehe {
		return breite, hoehe
	}
	w := int(math.Round(float64(breite)*float64(zielHoehe)/float64(hoehe)/2)) * 2
	return w, zielHoehe
}
