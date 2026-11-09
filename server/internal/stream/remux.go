// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package stream

// Für den Browser.
//
// HLS ist der richtige Weg für Fernseher und Telefone — dort kann das
// Betriebssystem es von Haus aus. Im Browser nicht: Chrome und Firefox
// bräuchten dafür eine Fremdbibliothek, die mitgeliefert, aktuell gehalten
// und mitverantwortet werden will. Für einen Server, der bei anderen Leuten
// zu Hause steht, ist das die falsche Abhängigkeit.
//
// Stattdessen schickt Lumen dem Browser einen Strom, den er von sich aus
// versteht: fragmentiertes MP4. Zwei Fälle:
//
//   - Bild und Ton kann der Browser schon, nur der Behälter passt nicht
//     (MKV ist der Normalfall). Dann wird nur umgepackt — kein Neurechnen,
//     praktisch keine Last, volle Bildqualität.
//   - Der Browser kann das Bildformat nicht (HEVC etwa). Dann wird wirklich
//     umgerechnet, wie bei HLS auch.
//
// Gesprungen wird, indem der Strom an der neuen Stelle neu beginnt. Das ist
// genau das, was HLS unter der Haube auch tut — nur ohne Abspielliste.

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
)

// RemuxJob beschreibt einen Strom für den Browser.
type RemuxJob struct {
	SourcePath string
	StartSec   float64 // genau die Sekunde eines Schlüsselbildes
	Plan       Plan
	AudioIndex int
	WebM       bool // VP9 und Opus statt H.264 und AAC

	// Wie bei HLS: genaue Angaben zum Bild, Tonemapping, Einbrennen und
	// die Stufe der Hardware (siehe bildkette.go).
	Details       *BildDetails
	Tonemap       bool
	MitEinbrennen bool
	Einbrennen    int
	Stufe         int
}

// ContentType nennt den Medientyp des Stroms.
func (j RemuxJob) ContentType() string {
	if j.WebM {
		return "video/webm"
	}
	return "video/mp4"
}

// seekEpsilon: Trifft -ss eine Sekunde, auf der ein Schlüsselbild liegt,
// nimmt FFmpeg das Schlüsselbild davor — und der Strom begänne einen
// ganzen Abstand zu früh. Bei Bildern mit B-Frames setzt FFmpeg den
// Sprung sogar 0,13 s vor die verlangte Stelle (3/23 s, eine feste
// Annahme in FFmpeg). 0,2 s Versatz landet darum sicher auf dem
// gewünschten Schlüsselbild; so nah liegen zwei Schlüsselbilder nie.
// Gilt nur beim Kopieren — beim Neurechnen beginnt der Strom genau dort.
const seekEpsilon = 0.2

// CanRemux sagt, ob sich das Umpacken lohnt — also ob Bild und Ton so
// bleiben können, wie sie sind.
func CanRemux(plan Plan) bool {
	return plan.VideoCodec == "copy" || plan.VideoCodec == "h264"
}

// ErrNoCodec meldet, dass dieser Browser gar kein Video abspielen kann.
var ErrNoCodec = fmt.Errorf("der browser bringt keinen video-decoder mit")

// WebM sagt, ob für dieses Gerät VP9 in WebM gebaut werden muss.
//
// Nicht jeder Browser bringt H.264 mit: manche Linux-Ausgaben von Chromium
// und Firefox lassen die lizenzpflichtigen Decoder weg. VP9 und Opus sind
// frei und überall dabei, wo H.264 fehlt — damit läuft Lumen auch dort.
func WebM(dev Device) bool {
	return !contains(dev.Codecs, "h264") &&
		(contains(dev.Codecs, "vp9") || contains(dev.Codecs, "vp8"))
}

// BrowserDevice baut aus dem, was die App gemeldet hat, die Beschreibung
// des Geräts. Meldet sie nichts, wird H.264 angenommen — das kann der
// weitaus grösste Teil der Browser.
func BrowserDevice(maxHeight int) Device {
	if maxHeight <= 0 {
		maxHeight = 2160
	}
	return Device{
		Codecs:     []string{"h264"},
		Containers: []string{"mp4"},
		Audio:      []string{"aac", "mp3"},
		MaxHeight:  maxHeight,
	}
}

// Remux startet FFmpeg und gibt den Strom zurück. Der Aufrufer muss ihn
// schliessen — dann endet auch FFmpeg.
func (f *FFmpeg) Remux(ctx context.Context, j RemuxJob) (io.ReadCloser, func(), error) {
	ctx, cancel := context.WithCancel(ctx)

	args := []string{"-hide_banner", "-loglevel", "error", "-nostdin"}
	var bild BildBefehl
	neuRechnen := !j.WebM && j.Plan.VideoCodec != "copy"
	if neuRechnen {
		auftrag := BildAuftrag{Quelle: j.Details, ZielHoehe: j.Plan.Height, Bitrate: j.Plan.Bitrate,
			Tonemap: j.Tonemap, Einbrennen: -1, Stufe: j.Stufe}
		if j.Details != nil && j.Plan.Height >= j.Details.Hoehe {
			auftrag.ZielHoehe = 0
		}
		if j.MitEinbrennen {
			auftrag.Einbrennen = j.Einbrennen
		}
		bild = f.BildBefehl(auftrag)
		args = append(args, bild.VorEingang...)
	}
	if j.StartSec > 0 {
		// Vor -i springt FFmpeg ohne Decodieren an die Stelle.
		ss := j.StartSec
		if !neuRechnen && !j.WebM {
			ss += seekEpsilon
		}
		args = append(args, "-ss", strconv.FormatFloat(ss, 'f', 3, 64))
	}
	args = append(args, "-i", j.SourcePath)
	// Der Strom selbst beginnt immer bei null — ein fragmentiertes MP4
	// führt keine Lage im Film mit sich. Wo er im Film steht, steht in der
	// Kopfzeile X-Lumen-Start; die Oberfläche rechnet es auf die Anzeige auf.
	if neuRechnen {
		args = append(args, "-filter_complex", bild.Filter, "-map", "[v]")
	} else {
		args = append(args, "-map", "0:v:0")
	}
	if j.AudioIndex >= 0 {
		args = append(args, "-map", fmt.Sprintf("0:%d", j.AudioIndex))
	} else {
		args = append(args, "-map", "0:a:0?")
	}

	switch {
	case j.WebM:
		// VP9 braucht spürbar mehr Rechenzeit als H.264. „realtime" mit
		// niedriger Stufe hält den Server trotzdem bei der Sache — lieber
		// etwas weniger Feinheit als eine Wiedergabe, die stockt.
		args = append(args,
			"-c:v", "libvpx-vp9", "-deadline", "realtime", "-cpu-used", "8",
			"-row-mt", "1", "-tile-columns", "2", "-pix_fmt", "yuv420p",
			"-g", "100")
		var filter []string
		if j.Plan.Height > 0 && (j.Details == nil || j.Plan.Height < j.Details.Hoehe) {
			filter = append(filter, fmt.Sprintf("scale=-2:%d", j.Plan.Height))
		}
		if j.Tonemap {
			filter = append(filter, f.Faehigkeiten().TonemapKette(j.Details.HLG()))
			args = append(args, FarbKennzeichnung()...)
		}
		if len(filter) > 0 {
			args = append(args, "-vf", strings.Join(filter, ","))
		}
		rate := j.Plan.Bitrate
		if rate <= 0 {
			rate = 4_000_000
		}
		args = append(args, "-b:v", strconv.Itoa(rate))
	case !neuRechnen:
		args = append(args, "-c:v", "copy")
	default:
		args = append(args, bild.Kodierer...)
		args = append(args, "-g", strconv.Itoa(gop(fpsVon(j.Details))/2))
	}

	// AC3 und DTS kann kein Browser; den Ton neu zu codieren kostet fast
	// nichts und erspart stumme Filme.
	if j.WebM {
		args = append(args, "-c:a", "libopus", "-b:a", "160k", "-ac", "2")
	} else {
		args = append(args, "-c:a", "aac", "-b:a", "192k", "-ac", "2")
	}

	if j.WebM {
		args = append(args, "-f", "webm", "-cluster_size_limit", "2M",
			"-cluster_time_limit", "2000", "-live", "1", "pipe:1")
	} else {
		args = append(args,
			"-f", "mp4",
			// Ohne diese Flaggen will FFmpeg ans Ende der Datei zurück-
			// springen, um das Inhaltsverzeichnis zu schreiben — in einem
			// Strom geht das nicht, und die Antwort bliebe leer.
			"-movflags", "frag_keyframe+empty_moov+default_base_moof",
			"-frag_duration", "2000000",
			"pipe:1")
	}

	cmd := exec.CommandContext(ctx, f.FFmpegPath, args...)
	stderr := &ringBuffer{limit: 2000}
	cmd.Stderr = stderr

	out, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, nil, err
	}
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, nil, fmt.Errorf("ffmpeg startet nicht: %w", err)
	}

	stop := func() {
		cancel()
		_ = out.Close()
		_ = cmd.Wait()
	}
	return &remuxStream{ReadCloser: out, stop: stop, errOut: stderr, Verfahren: bild.Verfahren,
		Stufe: bild.Stufe}, stop, nil
}

func fpsVon(d *BildDetails) float64 {
	if d == nil {
		return 25
	}
	return d.FPS
}

// RemuxVerfahren sagt, mit welcher Hardware ein Strom läuft ("" =
// Software) und auf welcher Stufe — damit der Aufrufer bei einem frühen
// Fehler auf die nächste zurückfallen kann.
func RemuxVerfahren(r io.Reader) (string, int) {
	if s, ok := r.(*remuxStream); ok {
		return s.Verfahren, s.Stufe
	}
	return "", StufeSoftware
}

type remuxStream struct {
	io.ReadCloser
	stop      func()
	errOut    *ringBuffer
	Verfahren string
	Stufe     int
}

func (r *remuxStream) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	if err != nil && err != io.EOF && n == 0 {
		if msg := r.errOut.String(); msg != "" {
			return n, fmt.Errorf("%w (%s)", err, lastLine(msg))
		}
	}
	return n, err
}

func (r *remuxStream) Close() error {
	r.stop()
	return nil
}

// Error gibt die letzte Meldung von FFmpeg heraus — fürs Protokoll.
func (r *remuxStream) Error() string { return lastLine(r.errOut.String()) }

// KeyframeBefore sagt, an welcher Sekunde der Strom wirklich beginnen wird.
//
// Beim blossen Umpacken kann FFmpeg nur auf ein Schlüsselbild genau
// einsteigen — bei einem Film mit zehn Sekunden Abstand kann das spürbar
// vor der gewünschten Stelle liegen. Wer auf 1:20:00 springt, soll aber
// nicht bei 1:19:52 stehen und die falsche Zeit angezeigt bekommen.
//
// Darum wird die Stelle vorher bestimmt und dem Gerät mitgeteilt. Gesucht
// wird nur in einem kurzen Fenster davor, nicht im ganzen Film.
func (f *FFmpeg) KeyframeBefore(ctx context.Context, path string, sec float64) float64 {
	if sec <= 0 {
		return 0
	}
	const window = 20 // Sekunden rückwärts — mehr Abstand hat kaum ein Film
	from := sec - window
	if from < 0 {
		from = 0
	}

	cmd := exec.CommandContext(ctx, f.FFprobePath,
		"-v", "error",
		"-select_streams", "v:0",
		"-skip_frame", "nokey",
		"-show_entries", "frame=pts_time",
		"-read_intervals", fmt.Sprintf("%.0f%%%.0f", from, sec+1),
		"-of", "csv=p=0",
		path)
	out, err := cmd.Output()
	if err != nil {
		return sec // im Zweifel die gewünschte Stelle nehmen
	}

	best := 0.0
	found := false
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(line), ","))
		if line == "" || line == "N/A" {
			continue
		}
		v, err := strconv.ParseFloat(line, 64)
		if err != nil || v > sec+0.001 {
			continue
		}
		if !found || v > best {
			best, found = v, true
		}
	}
	if !found {
		return from
	}
	return best
}
