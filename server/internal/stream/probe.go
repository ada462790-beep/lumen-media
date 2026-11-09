// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

// Package stream misst Dateien aus und liefert sie ans Gerät — direkt, wenn
// es geht, umgewandelt, wenn es sein muss.
package stream

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"lumen/internal/scan"
)

// FFmpeg kapselt die beiden Programme. Beide Pfade sind einstellbar, weil
// ein NAS sie gern an ungewöhnlichen Stellen ablegt.
type FFmpeg struct {
	FFmpegPath  string
	FFprobePath string
	// HW ist die Hardware-Beschleunigung; nil heisst Software.
	HW *Hardware
}

func NewFFmpeg(ffmpeg, ffprobe string) *FFmpeg {
	if ffmpeg == "" {
		ffmpeg = "ffmpeg"
	}
	if ffprobe == "" {
		ffprobe = "ffprobe"
	}
	return &FFmpeg{FFmpegPath: ffmpeg, FFprobePath: ffprobe}
}

// Available sagt, ob FFmpeg überhaupt da ist. Ohne läuft Lumen weiter, kann
// aber nur direkt abspielen — und das sagt die Einrichtung dann auch.
func (f *FFmpeg) Available() bool {
	_, err := exec.LookPath(f.FFmpegPath)
	return err == nil
}

// ---------------------------------------------------------------- ffprobe

type probeJSON struct {
	Format struct {
		FormatName string `json:"format_name"`
		Duration   string `json:"duration"`
		BitRate    string `json:"bit_rate"`
	} `json:"format"`
	Streams []struct {
		Index          int    `json:"index"`
		CodecType      string `json:"codec_type"`
		CodecName      string `json:"codec_name"`
		Width          int    `json:"width"`
		Height         int    `json:"height"`
		Channels       int    `json:"channels"`
		ColorTransfer  string `json:"color_transfer"`
		ColorPrimaries string `json:"color_primaries"`
		Tags           struct {
			Language string `json:"language"`
			Title    string `json:"title"`
		} `json:"tags"`
		Disposition struct {
			Forced          int `json:"forced"`
			HearingImpaired int `json:"hearing_impaired"`
		} `json:"disposition"`
		SideDataList []struct {
			SideDataType string `json:"side_data_type"`
		} `json:"side_data_list"`
	} `json:"streams"`
	Chapters []struct {
		ID        int    `json:"id"`
		StartTime string `json:"start_time"`
		EndTime   string `json:"end_time"`
		Tags      struct {
			Title string `json:"title"`
		} `json:"tags"`
	} `json:"chapters"`
}

// Probe misst eine Datei aus.
func (f *FFmpeg) Probe(ctx context.Context, path string) (*scan.Probe, error) {
	cmd := exec.CommandContext(ctx, f.FFprobePath,
		"-v", "error",
		"-print_format", "json",
		"-show_format", "-show_streams", "-show_chapters",
		path)
	out, err := Niedrig(cmd, false)
	if err != nil {
		var ee *exec.ExitError
		if ok := asExitError(err, &ee); ok && len(ee.Stderr) > 0 {
			return nil, fmt.Errorf("ffprobe: %s", lastLine(string(ee.Stderr)))
		}
		return nil, fmt.Errorf("ffprobe: %w", err)
	}

	var p probeJSON
	if err := json.Unmarshal(out, &p); err != nil {
		return nil, fmt.Errorf("ffprobe-ausgabe: %w", err)
	}

	res := &scan.Probe{
		Container: normalizeContainer(p.Format.FormatName),
	}
	if d, err := strconv.ParseFloat(p.Format.Duration, 64); err == nil {
		res.DurationMS = int64(d * 1000)
	}
	if b, err := strconv.Atoi(p.Format.BitRate); err == nil {
		res.Bitrate = b
	}

	for _, s := range p.Streams {
		switch s.CodecType {
		case "video":
			if res.Width == 0 {
				res.Width, res.Height = s.Width, s.Height
				res.VideoCodec = s.CodecName
				res.HDR = hdrKind(s.ColorTransfer, s.SideDataList)
			}
		case "audio":
			res.Tracks = append(res.Tracks, scan.Track{
				Index: s.Index, Kind: "audio", Codec: s.CodecName,
				Language: lang(s.Tags.Language), Title: s.Tags.Title,
				Channels: s.Channels, Forced: s.Disposition.Forced == 1,
			})
		case "subtitle":
			res.Tracks = append(res.Tracks, scan.Track{
				Index: s.Index, Kind: "subtitle", Codec: s.CodecName,
				Language: lang(s.Tags.Language), Title: s.Tags.Title,
				Forced:          s.Disposition.Forced == 1,
				HearingImpaired: s.Disposition.HearingImpaired == 1,
			})
		}
	}

	for i, c := range p.Chapters {
		ch := scan.Chapter{Index: i, Title: c.Tags.Title}
		if v, err := strconv.ParseFloat(c.StartTime, 64); err == nil {
			ch.StartMS = int64(v * 1000)
		}
		if v, err := strconv.ParseFloat(c.EndTime, 64); err == nil {
			ch.EndMS = int64(v * 1000)
		}
		ch.Kind = KapitelArt(ch, res.DurationMS, len(p.Chapters))
		res.Chapters = append(res.Chapters, ch)
	}

	// Untertitel, die als eigene Datei danebenliegen.
	res.Tracks = append(res.Tracks, scan.SidecarSubtitles(path)...)
	return res, nil
}

// KapitelArt rät, ob ein Kapitel Vorspann oder Abspann ist — nach dem
// Titel, sonst nach der Lage. anzahl ist die Zahl aller Kapitel (-1 =
// unbekannt): die Lage zählt nur, wenn es mehrere gibt, und nur bei
// Dateien, die lang genug sind, dass ein Vorspann überhaupt Platz hat.
func KapitelArt(c scan.Chapter, totalMS int64, anzahl int) string {
	title := strings.ToLower(c.Title)
	woerter := strings.FieldsFunc(title, func(r rune) bool { return r == ' ' || r == '-' || r == '_' || r == ':' || r == '.' })
	hat := func(ganz ...string) bool {
		for _, w := range woerter {
			for _, g := range ganz {
				if w == g {
					return true
				}
			}
		}
		return false
	}
	if hat("intro", "vorspann", "opening", "titelsequenz", "op", "titles", "titel") {
		return "intro"
	}
	if hat("credits", "abspann", "ending", "outro", "ed", "end") {
		return "credits"
	}
	if totalMS < 10*60*1000 || (anzahl >= 0 && anzahl < 3) {
		return "normal"
	}
	length := c.EndMS - c.StartMS
	if c.Index <= 1 && c.StartMS < 120_000 && length > 20_000 && length < 150_000 {
		return "intro"
	}
	if c.StartMS > totalMS-180_000 && c.EndMS >= totalMS-2_000 {
		return "credits"
	}
	return "normal"
}

func hdrKind(transfer string, sideData []struct {
	SideDataType string `json:"side_data_type"`
}) string {
	for _, sd := range sideData {
		t := strings.ToLower(sd.SideDataType)
		if strings.Contains(t, "dolby vision") {
			return "DV"
		}
		if strings.Contains(t, "hdr dynamic metadata") {
			return "HDR10+"
		}
	}
	switch strings.ToLower(transfer) {
	case "smpte2084", "arib-std-b67":
		return "HDR"
	}
	return ""
}

// lastLine nimmt die aussagekräftigste Zeile aus der FFmpeg-Ausgabe. Alles
// davor sind Kopfzeilen, die im Protokoll nur stören.
func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			if i := strings.LastIndex(l, ": "); i > 0 && i < len(l)-2 {
				return l[i+2:]
			}
			return l
		}
	}
	return "unbekannter Fehler"
}

func lang(code string) string {
	code = strings.ToLower(strings.TrimSpace(code))
	switch code {
	case "ger", "deu":
		return "de"
	case "eng":
		return "en"
	case "", "und":
		return "und"
	}
	if len(code) > 2 {
		return code[:2]
	}
	return code
}

// normalizeContainer bringt die Namen von ffprobe auf die Kürzel, die auch
// die Geräte verwenden. ffprobe meldet „matroska,webm" und
// „mov,mp4,m4a,3gp,3g2,mj2" — damit kann kein Fernseher etwas anfangen.
func normalizeContainer(formatName string) string {
	for _, f := range strings.Split(formatName, ",") {
		switch strings.TrimSpace(f) {
		case "matroska":
			return "mkv"
		case "mp4":
			return "mp4"
		case "webm":
			return "webm"
		case "avi":
			return "avi"
		case "mpegts":
			return "ts"
		case "asf":
			return "wmv"
		}
	}
	if i := strings.Index(formatName, ","); i > 0 {
		return formatName[:i]
	}
	return formatName
}

func asExitError(err error, out **exec.ExitError) bool {
	ee, ok := err.(*exec.ExitError)
	if ok {
		*out = ee
	}
	return ok
}

// ---------------------------------------------------------------- Entscheidung

// Device beschreibt, was das Gerät am anderen Ende kann.
type Device struct {
	Codecs     []string // h264, hevc, av1 …
	Containers []string // mp4, mkv, hls
	MaxHeight  int
	MaxBitrate int
	HDR        bool
	Audio      []string // aac, ac3, eac3 …
}

// Plan sagt, wie eine Datei zu diesem Gerät kommt.
type Plan struct {
	Direct     bool   `json:"direct"`
	Reason     string `json:"reason,omitempty"`
	VideoCodec string `json:"videoCodec"`
	AudioCodec string `json:"audioCodec"`
	Height     int    `json:"height"`
	Bitrate    int    `json:"bitrate"`
}

// Decide wählt zwischen direkt ausliefern und umwandeln. Direkt ist immer
// besser: weniger Last, bessere Bildqualität, kein Warten.
//
// Geht es nicht direkt, passt aber das Bild selbst (Codec, Auflösung,
// Bitrate, HDR), heisst der Plan VideoCodec "copy": dann wird nur der
// Behälter gewechselt und allenfalls der Ton gewandelt — das Bild bleibt,
// wie es ist. Früher wurde es in diesem Fall trotzdem neu gerechnet.
func Decide(file fileInfo, dev Device, limitBitrate int) Plan {
	p := Plan{Direct: true, VideoCodec: file.VideoCodec,
		AudioCodec: "copy", Height: file.Height, Bitrate: file.Bitrate}
	bildPasst := true

	if !contains(dev.Codecs, file.VideoCodec) {
		p.Direct, p.Reason = false, "Bildformat kann das Gerät nicht"
		p.VideoCodec = "h264"
		bildPasst = false
	}
	if dev.MaxHeight > 0 && file.Height > dev.MaxHeight {
		p.Direct, p.Reason = false, "Auflösung zu hoch fürs Gerät"
		p.Height = dev.MaxHeight
		bildPasst = false
	}
	if limitBitrate > 0 && file.Bitrate > limitBitrate {
		p.Direct, p.Reason = false, "Bandbreite"
		p.Bitrate = limitBitrate
		bildPasst = false
	}
	if file.HDR != "" && !dev.HDR {
		p.Direct, p.Reason = false, "Gerät kann kein HDR"
		// HDR in 4K umzurechnen schafft ein NAS kaum in Echtzeit. 1080p
		// reicht für einen Bildschirm ohne HDR allemal.
		if p.Height == 0 || p.Height > 1080 {
			p.Height = 1080
		}
		bildPasst = false
	}
	if file.Einbrennen {
		p.Direct, p.Reason = false, "Untertitel werden ins Bild gerechnet"
		bildPasst = false
	}
	// Ton, den das Gerät nicht kann (DTS, TrueHD …): Bisher lief der Film
	// dann direkt — mit Bild, aber stumm. Jetzt wird umgewandelt, und der
	// Ton kommt als AAC.
	if a := TonName(file.AudioCodec); a != "" && len(dev.Audio) > 0 && !contains(dev.Audio, a) {
		if p.Direct {
			p.Reason = "Tonformat kann das Gerät nicht"
		}
		p.Direct = false
		p.AudioCodec = "aac"
	}
	if !contains(dev.Containers, file.Container) && !contains(dev.Containers, "hls") {
		// Bewusst ohne die Dateiendung: der Grund landet in der Verwaltung,
		// und dort hat nichts zu stehen, was auf die Datei zeigt.
		if p.Direct {
			p.Reason = "Dateiformat passt nicht zum Gerät"
		}
		p.Direct = false
	}
	if !p.Direct {
		if bildPasst {
			p.VideoCodec = "copy"
		} else if p.VideoCodec == "copy" || p.VideoCodec == file.VideoCodec {
			p.VideoCodec = "h264"
		}
	}
	return p
}

// FuerHLS macht aus einem Plan den für eine HLS-Wiedergabe. Ein Gerät, das
// „hls" als Behälter meldet, bekommt von Decide „direkt" — auch für eine
// MKV, die es selbst nicht öffnen kann (AVPlayer etwa). Landet es dann
// doch bei HLS, soll das Bild kopiert werden, nicht neu gerechnet.
func (p Plan) FuerHLS() Plan {
	if p.Direct {
		p.Direct = false
		p.VideoCodec = "copy"
		if p.Reason == "" {
			p.Reason = "nur umgepackt"
		}
	}
	return p
}

// TonKopierbar: Diesen Ton kann ein Gerät, das ihn meldet, in HLS
// unverändert bekommen. TS und fMP4 tragen AAC, AC3, E-AC3 und MP3
// zuverlässig; DTS oder TrueHD dort hinein zu packen, versteht kaum ein
// Player.
func TonKopierbar(codec string, dev Device) bool {
	n := TonName(codec)
	switch n {
	case "aac", "ac3", "eac3", "mp3":
		return contains(dev.Audio, n)
	}
	return false
}

// FileInfoT ist die Entscheidungsgrundlage für eine Datei.
type FileInfoT = fileInfo

type fileInfo struct {
	AudioCodec string // der gewählten (sonst ersten) Tonspur, so wie ffprobe ihn nennt
	VideoCodec string
	Container  string
	Height     int
	Bitrate    int
	HDR        string
	// Einbrennen: ein Bild-Untertitel soll ins Bild — das geht nur beim
	// Neurechnen.
	Einbrennen bool
}

// MitEinbrennen merkt vor, dass ein Bild-Untertitel eingebrannt wird.
func (f fileInfo) MitEinbrennen() fileInfo {
	f.Einbrennen = true
	return f
}

// FileInfo baut die Entscheidungsgrundlage aus einer Datei aus der Mediathek.
func FileInfo(videoCodec, container string, height, bitrate int, hdr string) fileInfo {
	if container == "" {
		container = "mkv"
	}
	return fileInfo{VideoCodec: videoCodec, Container: container,
		Height: height, Bitrate: bitrate, HDR: hdr}
}

// MitTon ergänzt den Ton der gewählten Tonspur.
func (f fileInfo) MitTon(codec string) fileInfo {
	f.AudioCodec = codec
	return f
}

// TonName bringt die Namen von ffprobe auf die der Geräte: „pcm_s16le"
// heisst dort „pcm", „mlp" ist TrueHD.
func TonName(codec string) string {
	c := strings.ToLower(strings.TrimSpace(codec))
	switch {
	case strings.HasPrefix(c, "pcm_"):
		return "pcm"
	case c == "mlp":
		return "truehd"
	case strings.HasPrefix(c, "dts"):
		return "dts"
	}
	return c
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if strings.EqualFold(x, v) {
			return true
		}
	}
	return false
}

// SafeJoin sorgt dafür, dass ein Pfad aus einer Anfrage die Bibliothek nicht
// verlassen kann. Ohne das wäre „../../etc/passwd" ein gültiger Film.
func SafeJoin(root, rel string) (string, error) {
	// Ein „..“ irgendwo im Pfad wird abgelehnt statt stillschweigend
	// zurechtgebogen — ein Aufrufer soll merken, dass er daneben greift.
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		if part == ".." {
			return "", fmt.Errorf("pfad liegt ausserhalb der bibliothek")
		}
	}
	clean := filepath.Clean("/" + rel)
	full := filepath.Join(root, clean)
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	fullAbs, err := filepath.Abs(full)
	if err != nil {
		return "", err
	}
	if fullAbs != rootAbs && !strings.HasPrefix(fullAbs, rootAbs+string(filepath.Separator)) {
		return "", fmt.Errorf("pfad liegt ausserhalb der bibliothek")
	}
	return fullAbs, nil
}
