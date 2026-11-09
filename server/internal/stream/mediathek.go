// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package stream

// Musik und Fotos: Tags lesen, Cover herausziehen, Lieder für Browser
// umwandeln, die ein Format nicht können, Vorschaubilder rechnen.
//
// Alles, was im Hintergrund läuft (Durchlauf, Vorschaubilder), geht über
// Niedrig — die Wiedergabe hat Vorrang. Was jemand gerade angeklickt hat
// und worauf er wartet (ein Lied umwandeln, ein fehlendes Vorschaubild),
// läuft mit normaler Priorität, aber nie mehr als zwei gleichzeitig.

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"lumen/internal/scan"
)

type mediathekProbe struct {
	Format struct {
		FormatName string            `json:"format_name"`
		Duration   string            `json:"duration"`
		BitRate    string            `json:"bit_rate"`
		Tags       map[string]string `json:"tags"`
	} `json:"format"`
	Streams []struct {
		CodecType  string            `json:"codec_type"`
		CodecName  string            `json:"codec_name"`
		SampleRate string            `json:"sample_rate"`
		BitRate    string            `json:"bit_rate"`
		Width      int               `json:"width"`
		Height     int               `json:"height"`
		Duration   string            `json:"duration"`
		Tags       map[string]string `json:"tags"`
		SideData   []struct {
			Rotation float64 `json:"rotation"`
		} `json:"side_data_list"`
		Disposition struct {
			AttachedPic int `json:"attached_pic"`
		} `json:"disposition"`
	} `json:"streams"`
}

func (f *FFmpeg) mediathekProben(ctx context.Context, path string) (*mediathekProbe, error) {
	cmd := exec.CommandContext(ctx, f.FFprobePath, "-v", "error", "-print_format", "json",
		"-show_format", "-show_streams", path)
	out, err := Niedrig(cmd, false)
	if err != nil {
		var ee *exec.ExitError
		if asExitError(err, &ee) && len(ee.Stderr) > 0 {
			return nil, fmt.Errorf("ffprobe: %s", lastLine(string(ee.Stderr)))
		}
		return nil, fmt.Errorf("ffprobe: %w", err)
	}
	var p mediathekProbe
	if err := json.Unmarshal(out, &p); err != nil {
		return nil, fmt.Errorf("ffprobe-ausgabe: %w", err)
	}
	return &p, nil
}

// tagsZusammen: Format- und Spur-Tags in einer Tabelle mit kleinen
// Schlüsseln. Bei Ogg und Opus stehen die Tags an der Spur, bei MP3 und
// MP4 am Format; die Schreibweise der Schlüssel ist je nach Programm, das
// getaggt hat, eine andere.
func tagsZusammen(p *mediathekProbe) map[string]string {
	out := map[string]string{}
	add := func(m map[string]string) {
		for k, v := range m {
			k = strings.ToLower(strings.TrimSpace(k))
			if _, schon := out[k]; !schon && strings.TrimSpace(v) != "" {
				out[k] = strings.TrimSpace(v)
			}
		}
	}
	add(p.Format.Tags)
	for _, s := range p.Streams {
		if s.CodecType == "audio" {
			add(s.Tags)
		}
	}
	return out
}

func ersterTag(t map[string]string, keys ...string) string {
	for _, k := range keys {
		if v := t[k]; v != "" {
			return v
		}
	}
	return ""
}

func wahr(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "ja", "explicit":
		return true
	}
	return false
}

// MusikTags liest die Tags eines Musikstücks (scan.MusikProber).
func (f *FFmpeg) MusikTags(ctx context.Context, path string) (*scan.MusikTags, error) {
	p, err := f.mediathekProben(ctx, path)
	if err != nil {
		return nil, err
	}
	t := tagsZusammen(p)
	m := &scan.MusikTags{
		Titel:          ersterTag(t, "title"),
		Interpret:      ersterTag(t, "artist", "artists", "performer"),
		AlbumInterpret: ersterTag(t, "album_artist", "albumartist", "album artist", "band"),
		Album:          ersterTag(t, "album"),
		Track:          scan.ZahlAusTag(ersterTag(t, "track", "tracknumber")),
		Disc:           scan.ZahlAusTag(ersterTag(t, "disc", "discnumber", "tpos")),
		Jahr:           scan.JahrAusTag(ersterTag(t, "date", "year", "originaldate", "tdrc", "tyer", "original_year")),
		Genre:          ersterTag(t, "genre"),
		Komponist:      ersterTag(t, "composer"),
		Compilation:    wahr(ersterTag(t, "compilation", "tcmp", "cpil")),
		Explizit:       ersterTag(t, "itunesadvisory", "rtng") == "1" || wahr(ersterTag(t, "explicit")),
		Container:      strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), "."),
	}
	if d, err := strconv.ParseFloat(p.Format.Duration, 64); err == nil {
		m.DauerMS = int64(d * 1000)
	}
	m.Bitrate, _ = strconv.Atoi(p.Format.BitRate)
	for _, s := range p.Streams {
		switch {
		case s.CodecType == "audio" && m.Codec == "":
			m.Codec = s.CodecName
			m.Abtastrate, _ = strconv.Atoi(s.SampleRate)
			if b, err := strconv.Atoi(s.BitRate); err == nil && b > 0 {
				m.Bitrate = b
			}
		case s.CodecType == "video" && s.Disposition.AttachedPic == 1:
			m.HatCover = true
		}
	}
	// Genre-Nummern aus ID3v1-Zeiten („(17)") sagen niemandem etwas.
	if strings.HasPrefix(m.Genre, "(") {
		m.Genre = ""
	}
	return m, nil
}

var reISO6709 = regexp.MustCompile(`^([+-]\d+(?:\.\d+)?)([+-]\d+(?:\.\d+)?)`)

// VideoInfo misst ein Handyvideo aus (scan.FotoProber).
func (f *FFmpeg) VideoInfo(ctx context.Context, path string) (*scan.VideoInfo, error) {
	p, err := f.mediathekProben(ctx, path)
	if err != nil {
		return nil, err
	}
	v := &scan.VideoInfo{}
	if d, err := strconv.ParseFloat(p.Format.Duration, 64); err == nil {
		v.DauerMS = int64(d * 1000)
	}
	t := map[string]string{}
	for k, val := range p.Format.Tags {
		t[strings.ToLower(k)] = val
	}
	// Das iPhone schreibt die Ortszeit mit Zone; creation_time ist UTC.
	// Wie bei Fotos zählt die Uhrzeit, die auf dem Gerät stand.
	if s := t["com.apple.quicktime.creationdate"]; s != "" {
		if z, err := time.Parse("2006-01-02T15:04:05-0700", s); err == nil {
			v.Aufnahme = time.Date(z.Year(), z.Month(), z.Day(), z.Hour(), z.Minute(), z.Second(), 0, time.UTC)
		}
	}
	if v.Aufnahme.IsZero() {
		if s := t["creation_time"]; s != "" {
			if z, err := time.Parse(time.RFC3339Nano, s); err == nil && z.Year() > 1970 {
				l := z.Local()
				v.Aufnahme = time.Date(l.Year(), l.Month(), l.Day(), l.Hour(), l.Minute(), l.Second(), 0, time.UTC)
			}
		}
	}
	v.Kamera = strings.TrimSpace(ersterTag(t, "com.apple.quicktime.model", "model"))
	if mk := ersterTag(t, "com.apple.quicktime.make", "make"); mk != "" && !strings.HasPrefix(strings.ToLower(v.Kamera), strings.ToLower(mk)) {
		v.Kamera = strings.TrimSpace(mk + " " + v.Kamera)
	}
	if loc := ersterTag(t, "com.apple.quicktime.location.iso6709", "location"); loc != "" {
		if m := reISO6709.FindStringSubmatch(loc); m != nil {
			b, _ := strconv.ParseFloat(m[1], 64)
			l, _ := strconv.ParseFloat(m[2], 64)
			if !(b == 0 && l == 0) {
				v.Breitengrad, v.Laengengrad = &b, &l
			}
		}
	}
	for _, s := range p.Streams {
		if s.CodecType != "video" || s.Disposition.AttachedPic == 1 {
			continue
		}
		v.Codec, v.Breite, v.Hoehe = s.CodecName, s.Width, s.Height
		rot := 0.0
		for _, sd := range s.SideData {
			if sd.Rotation != 0 {
				rot = sd.Rotation
			}
		}
		if r, err := strconv.Atoi(s.Tags["rotate"]); err == nil && rot == 0 {
			rot = float64(r)
		}
		if int(rot)%180 != 0 {
			v.Breite, v.Hoehe = v.Hoehe, v.Breite
		}
		break
	}
	return v, nil
}

// ---------------------------------------------------------------- umwandeln

// MusikUmwandeln wandelt ein Musikstück in ein Format, das jeder Browser
// kann: AAC (in MP4) oder Opus (in Ogg). Das Ergebnis wird erst unter
// einem Zwischennamen geschrieben und dann umbenannt — wer gleichzeitig
// liest, sieht nie eine halbe Datei.
func (f *FFmpeg) MusikUmwandeln(ctx context.Context, quelle, ziel, als string) error {
	tmp := ziel + ".tmp"
	args := []string{"-nostdin", "-loglevel", "error", "-i", quelle, "-map", "0:a:0", "-vn", "-map_metadata", "-1"}
	switch als {
	case "opus":
		args = append(args, "-c:a", "libopus", "-b:a", "160k", "-f", "ogg")
	default:
		args = append(args, "-c:a", "aac", "-b:a", "256k", "-movflags", "+faststart", "-f", "mp4")
	}
	args = append(args, "-y", tmp)
	cmd := exec.CommandContext(ctx, f.FFmpegPath, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("ffmpeg: %v: %s", err, lastLine(string(out)))
	}
	return os.Rename(tmp, ziel)
}

// CoverAusDatei zieht das eingebettete Cover aus einem Musikstück und
// legt es verkleinert als JPEG ab.
func (f *FFmpeg) CoverAusDatei(ctx context.Context, quelle, ziel string, breite int, niedrig bool) error {
	return f.bildRechnen(ctx, []string{"-i", quelle, "-map", "0:v:0"}, ziel, breite, "", niedrig)
}

// BildVerkleinern rechnet ein Vorschaubild: höchstens breite×breite,
// Seitenverhältnis bleibt, EXIF-Ausrichtung angewandt. HEIC kann FFmpeg
// erst ab Fassung 7; davor helfen heif-convert (libheif) oder
// ImageMagick, falls sie da sind.
func (f *FFmpeg) BildVerkleinern(ctx context.Context, quelle, ziel string, breite, ausrichtung int, format string, niedrig bool) error {
	drehen := AusrichtungFilter(ausrichtung)
	err := f.bildRechnen(ctx, []string{"-noautorotate", "-i", quelle}, ziel, breite, drehen, niedrig)
	if err == nil || format != "heic" {
		return err
	}
	// HEIC über heif-convert: erst ganz, dann verkleinern.
	if pfad, lerr := exec.LookPath("heif-convert"); lerr == nil {
		tmp := ziel + ".heif.jpg"
		defer os.Remove(tmp)
		cmd := exec.CommandContext(ctx, pfad, "-q", "92", quelle, tmp)
		if _, e := f.laufen(cmd, niedrig); e == nil {
			if e := f.bildRechnen(ctx, []string{"-i", tmp}, ziel, breite, "", niedrig); e == nil {
				return nil
			}
		}
	}
	// ImageMagick nur für eine Datei, die wirklich mit einer HEIF-Kennung
	// beginnt, und mit erzwungenem Format („heic:"): Sonst entschiede
	// ImageMagick selbst am Inhalt, welchen seiner vielen Leser es nimmt —
	// eine Datei, die sich nur „.heic" nennt, könnte so etwa als SVG, MVG
	// oder PostScript gelesen werden, mit allem, was diese Leser können.
	if !IstHEIF(quelle) {
		return err
	}
	for _, name := range []string{"magick", "convert"} {
		pfad, lerr := exec.LookPath(name)
		if lerr != nil {
			continue
		}
		tmp := ziel + ".tmp.jpg"
		cmd := exec.CommandContext(ctx, pfad, "heic:"+quelle+"[0]", "-auto-orient", "-resize",
			fmt.Sprintf("%dx%d>", breite, breite), "-quality", "85", "-strip", tmp)
		if _, e := f.laufen(cmd, niedrig); e == nil {
			if st, e := os.Stat(tmp); e == nil && st.Size() > 0 {
				return os.Rename(tmp, ziel)
			}
		}
		_ = os.Remove(tmp)
	}
	return err
}

// heifMarken: die Marken im „ftyp"-Kasten, die eine HEIF-Bilddatei
// ausweisen (ISO/IEC 23008-12): HEVC-Bilder und -Folgen sowie die
// allgemeinen „mif1"/„msf1".
var heifMarken = map[string]bool{
	"heic": true, "heix": true, "heim": true, "heis": true,
	"hevc": true, "hevx": true, "hevm": true, "hevs": true,
	"mif1": true, "msf1": true,
}

// IstHEIF sagt, ob eine Datei mit einem ISO-BMFF-Kasten „ftyp" beginnt,
// dessen Hauptmarke oder eine der verträglichen Marken HEIF ist.
func IstHEIF(pfad string) bool {
	f, err := os.Open(pfad)
	if err != nil {
		return false
	}
	defer f.Close()
	kopf := make([]byte, 64)
	n, _ := io.ReadFull(f, kopf)
	kopf = kopf[:n]
	if len(kopf) < 16 || string(kopf[4:8]) != "ftyp" {
		return false
	}
	laenge := int(binary.BigEndian.Uint32(kopf[0:4]))
	if laenge < 16 || laenge > len(kopf) {
		laenge = len(kopf)
	}
	if heifMarken[string(kopf[8:12])] {
		return true
	}
	// Verträgliche Marken ab Byte 16, je vier Zeichen.
	for i := 16; i+4 <= laenge; i += 4 {
		if heifMarken[string(kopf[i:i+4])] {
			return true
		}
	}
	return false
}

// VideoStandbild: ein Bild aus einem Handyvideo, kurz nach dem Anfang.
func (f *FFmpeg) VideoStandbild(ctx context.Context, quelle, ziel string, breite int, dauerMS int64, niedrig bool) error {
	stelle := "0.5"
	if dauerMS > 0 && dauerMS < 1000 {
		stelle = "0"
	}
	if err := f.bildRechnen(ctx, []string{"-ss", stelle, "-i", quelle}, ziel, breite, "", niedrig); err == nil {
		return nil
	}
	return f.bildRechnen(ctx, []string{"-i", quelle}, ziel, breite, "", niedrig)
}

// VideoUmwandeln macht aus einem kurzen Handyvideo (etwa HEVC vom iPhone)
// eine H.264-Datei, die jeder Browser abspielt.
func (f *FFmpeg) VideoUmwandeln(ctx context.Context, quelle, ziel string) error {
	tmp := ziel + ".tmp"
	cmd := exec.CommandContext(ctx, f.FFmpegPath, "-nostdin", "-loglevel", "error", "-i", quelle,
		"-map", "0:v:0", "-map", "0:a:0?", "-c:v", "libx264", "-preset", "veryfast", "-crf", "22",
		"-pix_fmt", "yuv420p", "-vf", "scale='min(1920,iw)':-2", "-c:a", "aac", "-b:a", "160k",
		"-movflags", "+faststart", "-map_metadata", "-1", "-f", "mp4", "-y", tmp)
	if out, err := cmd.CombinedOutput(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("ffmpeg: %v: %s", err, lastLine(string(out)))
	}
	return os.Rename(tmp, ziel)
}

// AusrichtungFilter: die EXIF-Ausrichtung (1–8) als FFmpeg-Filter.
func AusrichtungFilter(a int) string {
	switch a {
	case 2:
		return "hflip"
	case 3:
		return "hflip,vflip"
	case 4:
		return "vflip"
	case 5:
		return "transpose=0"
	case 6:
		return "transpose=1"
	case 7:
		return "transpose=3"
	case 8:
		return "transpose=2"
	}
	return ""
}

func (f *FFmpeg) bildRechnen(ctx context.Context, eingabe []string, ziel string, breite int, vorher string, niedrig bool) error {
	if err := os.MkdirAll(filepath.Dir(ziel), 0o755); err != nil {
		return err
	}
	vf := fmt.Sprintf("scale='min(%d,iw)':'min(%d,ih)':force_original_aspect_ratio=decrease", breite, breite)
	if vorher != "" {
		vf = vorher + "," + vf
	}
	tmp := ziel + ".tmp.jpg"
	args := append([]string{"-nostdin", "-loglevel", "error"}, eingabe...)
	args = append(args, "-frames:v", "1", "-vf", vf+",format=yuvj420p", "-q:v", "3", "-map_metadata", "-1", "-y", tmp)
	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, f.FFmpegPath, args...)
	out, err := f.laufen(cmd, niedrig)
	if err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("ffmpeg: %v: %s", err, lastLine(string(out)))
	}
	if st, err := os.Stat(tmp); err != nil || st.Size() == 0 {
		_ = os.Remove(tmp)
		return errors.New("ffmpeg lieferte kein bild")
	}
	return os.Rename(tmp, ziel)
}

func (f *FFmpeg) laufen(cmd *exec.Cmd, niedrig bool) ([]byte, error) {
	if niedrig {
		return Niedrig(cmd, true)
	}
	return cmd.CombinedOutput()
}
