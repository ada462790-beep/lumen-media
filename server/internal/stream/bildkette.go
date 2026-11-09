// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package stream

// Die Bildkette: was mit dem Bild geschieht, wenn es neu gerechnet wird.
//
// Bisher war das eine feste Zeile libx264. Jetzt hängt sie von vier Dingen
// ab, die sich frei kombinieren: verkleinern, HDR auf SDR umrechnen,
// Bild-Untertitel einbrennen — und ob das in Software oder im Videoteil
// des Prozessors geschieht. Alles an einer Stelle, damit HLS und der
// Browser-Strom dasselbe tun.
//
// Für die Hardware gibt es drei Stufen:
//
//   - voll: decodieren, verkleinern (und wenn möglich Tonemapping) und
//     codieren im Videoteil. Am schnellsten; der Prozessor schläft.
//   - gemischt: decodieren und codieren im Videoteil, dazwischen Filter in
//     Software. Nötig fürs Einbrennen von Untertiteln und für Tonemapping,
//     wo der Videoteil es nicht kann.
//   - Software: alles im Prozessor.
//
// Scheitert eine Stufe, nimmt die Umwandlung die nächste (Rueckfall).

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Stufen der Beschleunigung, vom schnellsten zum sichersten.
const (
	StufeVoll     = 0
	StufeGemischt = 1
	StufeSoftware = 2
)

// BildAuftrag beschreibt, was mit dem Bild geschehen soll.
type BildAuftrag struct {
	Quelle     *BildDetails // darf nil sein
	ZielHoehe  int          // 0 = wie die Quelle
	Bitrate    int          // Obergrenze in Bit/s, 0 = frei
	Tonemap    bool         // HDR auf SDR umrechnen
	Einbrennen int          // Index (in der Datei) eines Bild-Untertitels, -1 = keiner
	Schluessel string       // Ausdruck für -force_key_frames, "" = keiner
	Stufe      int          // StufeVoll, StufeGemischt, StufeSoftware
}

// BildBefehl sind die Teile des FFmpeg-Aufrufs für das Bild.
type BildBefehl struct {
	VorEingang []string // vor -i (Geräte, Decoder)
	Filter     string   // für -filter_complex; endet immer in [v]
	Kodierer   []string
	Verfahren  string // tatsächlich benutzt, "" = Software
	Stufe      int
}

// BildBefehl baut die Bildkette. Ohne Hardware (oder auf StufeSoftware)
// ist es die bewährte libx264-Kette.
func (f *FFmpeg) BildBefehl(a BildAuftrag) BildBefehl {
	q := a.Quelle
	if q == nil {
		q = &BildDetails{}
	}
	kann := f.Faehigkeiten()
	breite, hoehe := AusgabeGroesse(q.Breite, q.Hoehe, a.ZielHoehe)
	verkleinern := a.ZielHoehe > 0 && q.Hoehe > 0 && a.ZielHoehe < q.Hoehe
	if a.ZielHoehe > 0 && q.Hoehe == 0 {
		verkleinern = true // Quelle unbekannt: lieber ausdrücklich skalieren
	}

	v := ""
	if a.Stufe != StufeSoftware {
		v = f.HW.Aktiv()
	}
	stufe := a.Stufe
	if v == "" {
		stufe = StufeSoftware
	}
	// Was der Videoteil allein nicht kann, verlangt die gemischte Stufe.
	if stufe == StufeVoll {
		hwTonemap := f.HW.TonemapImVideoteil(v)
		if a.Einbrennen >= 0 || (a.Tonemap && !hwTonemap) || (q.Zeilensprung && v == HWNVENC) {
			stufe = StufeGemischt
		}
	}

	b := BildBefehl{Verfahren: v, Stufe: stufe}
	geraet := f.HW.Geraet()
	eingang := "[0:v:0]"

	// ------------------------------------------------ Software-Filter
	swKette := func() []string {
		var k []string
		if q.Zeilensprung {
			k = append(k, "yadif=0:-1:0")
		}
		if verkleinern {
			k = append(k, fmt.Sprintf("scale=%d:%d", breite, hoehe))
		}
		if a.Tonemap {
			k = append(k, kann.TonemapKette(q.HLG()))
		} else {
			k = append(k, "format=yuv420p")
		}
		return k
	}
	// Bild-Untertitel liegen auf einer eigenen Leinwand (PGS meist so
	// gross wie der Film, DVD 720×576). scale2ref bringt sie auf die
	// Grösse des Bildes, wie es gerade ist — auch nach dem Verkleinern.
	einbrennen := func(vorher string) string {
		return fmt.Sprintf("%s[basis];[0:%d][basis]scale2ref=w=main_w:h=main_h[ut][basis2];"+
			"[basis2][ut]overlay=eof_action=pass:repeatlast=0", vorher, a.Einbrennen)
	}

	switch {
	case stufe == StufeSoftware:
		g := eingang + strings.Join(swKette(), ",")
		if a.Einbrennen >= 0 {
			g = einbrennen(g)
		}
		b.Filter = g + "[v]"
		b.Kodierer = x264Kodierer(hoehe, q.FPS, a.Bitrate)

	case v == HWVAAPI || v == HWQSV:
		b.VorEingang = []string{"-init_hw_device", "vaapi=va:" + geraet}
		if v == HWQSV {
			b.VorEingang = append(b.VorEingang, "-init_hw_device", "qsv=qs@va")
		}
		b.VorEingang = append(b.VorEingang, "-filter_hw_device", "va", "-hwaccel", "vaapi", "-hwaccel_device", "va")
		var g string
		if stufe == StufeVoll {
			b.VorEingang = append(b.VorEingang, "-hwaccel_output_format", "vaapi")
			var k []string
			if q.Zeilensprung {
				k = append(k, "deinterlace_vaapi")
			}
			if a.Tonemap {
				if verkleinern {
					k = append(k, fmt.Sprintf("scale_vaapi=w=%d:h=%d", breite, hoehe))
				}
				k = append(k, "tonemap_vaapi=format=nv12:t=bt709:m=bt709:p=bt709")
			} else {
				// Auch ohne Verkleinern: ein 10-Bit-Bild muss auf 8 Bit.
				if verkleinern {
					k = append(k, fmt.Sprintf("scale_vaapi=w=%d:h=%d:format=nv12", breite, hoehe))
				} else {
					k = append(k, "scale_vaapi=format=nv12")
				}
			}
			g = eingang + strings.Join(k, ",")
		} else {
			// Gemischt: die decodierten Bilder kommen im Arbeitsspeicher
			// an (ohne -hwaccel_output_format), werden dort gefiltert und
			// für den Kodierer wieder hochgeladen.
			g = eingang + strings.Join(swKette(), ",")
			if a.Einbrennen >= 0 {
				g = einbrennen(g)
			}
			g += ",format=nv12,hwupload"
		}
		if v == HWQSV {
			g += ",hwmap=derive_device=qsv,format=qsv"
			b.Kodierer = qsvKodierer(hoehe, q.FPS, a.Bitrate)
		} else {
			b.Kodierer = vaapiKodierer(hoehe, q.FPS, a.Bitrate)
		}
		b.Filter = g + "[v]"

	case v == HWNVENC:
		var g string
		if stufe == StufeVoll {
			b.VorEingang = []string{"-init_hw_device", "cuda=cu:0", "-filter_hw_device", "cu",
				"-hwaccel", "cuda", "-hwaccel_device", "cu", "-hwaccel_output_format", "cuda"}
			if verkleinern {
				g = eingang + fmt.Sprintf("scale_cuda=w=%d:h=%d:format=yuv420p", breite, hoehe)
			} else {
				g = eingang + "scale_cuda=format=yuv420p"
			}
		} else {
			b.VorEingang = []string{"-hwaccel", "cuda"}
			g = eingang + strings.Join(swKette(), ",")
			if a.Einbrennen >= 0 {
				g = einbrennen(g)
			}
		}
		b.Filter = g + "[v]"
		b.Kodierer = nvencKodierer(hoehe, q.FPS, a.Bitrate)
	}

	if a.Schluessel != "" {
		b.Kodierer = append(b.Kodierer, "-force_key_frames", a.Schluessel)
		switch v {
		case HWNVENC:
			if stufe != StufeSoftware {
				b.Kodierer = append(b.Kodierer, "-forced-idr", "1")
			}
		case HWQSV:
			if stufe != StufeSoftware {
				b.Kodierer = append(b.Kodierer, "-forced_idr", "1")
			}
		}
		// Kein Schlüsselbild ausserhalb des Rasters: sonst schneidet der
		// Segmentierer womöglich dort statt an der Grenze.
		b.Kodierer = append(b.Kodierer, "-g", strconv.Itoa(gop(q.FPS)))
		if stufe == StufeSoftware {
			b.Kodierer = append(b.Kodierer, "-sc_threshold", "0")
		}
	}
	// Ein umgerechnetes Bild ist immer SDR. Gekennzeichnet wird es, wenn
	// es aus HDR kommt oder HD ist — bei SD bleibt die Quelle massgeblich
	// (dort ist BT.601 üblich, und „709" wäre schlicht falsch).
	if a.Tonemap || hoehe >= 720 {
		b.Kodierer = append(b.Kodierer, FarbKennzeichnung()...)
	}
	return b
}

func gop(fps float64) int {
	if fps <= 0 || fps > 120 {
		fps = 25
	}
	return int(math.Ceil(fps * SegmentSeconds * 2))
}

// H264Level ist das Level, mit dem Lumen ein Bild dieser Grösse codiert.
func H264Level(hoehe int, fps float64) int {
	switch {
	case hoehe > 1088:
		return 51
	case fps > 31:
		return 42
	}
	return 41
}

func levelText(l int) string { return fmt.Sprintf("%d.%d", l/10, l%10) }

// ZielBitrate schätzt, was ein Hardware-Kodierer anpeilen soll — der kennt
// kein „gleichbleibende Qualität" wie -crf. Die Obergrenze gewinnt.
func ZielBitrate(hoehe, grenze int) int {
	var z int
	switch {
	case hoehe > 1088:
		z = 20_000_000
	case hoehe > 720:
		z = 8_000_000
	case hoehe > 576:
		z = 4_500_000
	default:
		z = 2_000_000
	}
	if grenze > 0 && grenze < z {
		z = grenze
	}
	return z
}

func rate(grenze, hoehe int) []string {
	z := ZielBitrate(hoehe, grenze)
	max := z
	if grenze <= 0 {
		max = z * 3 / 2
	}
	ziel := z * 85 / 100
	return []string{"-b:v", strconv.Itoa(ziel), "-maxrate", strconv.Itoa(max), "-bufsize", strconv.Itoa(max * 2)}
}

func x264Kodierer(hoehe int, fps float64, grenze int) []string {
	a := []string{"-c:v", "libx264", "-preset", "veryfast", "-crf", "20",
		"-profile:v", "high", "-level", levelText(H264Level(hoehe, fps)), "-pix_fmt", "yuv420p"}
	if grenze > 0 {
		a = append(a, "-maxrate", strconv.Itoa(grenze), "-bufsize", strconv.Itoa(grenze*2))
	}
	return a
}

func vaapiKodierer(hoehe int, fps float64, grenze int) []string {
	a := []string{"-c:v", "h264_vaapi", "-profile:v", "high", "-level", levelText(H264Level(hoehe, fps))}
	return append(a, rate(grenze, hoehe)...)
}

func qsvKodierer(hoehe int, fps float64, grenze int) []string {
	a := []string{"-c:v", "h264_qsv", "-preset", "veryfast", "-profile:v", "high",
		"-level", levelText(H264Level(hoehe, fps))}
	return append(a, rate(grenze, hoehe)...)
}

func nvencKodierer(hoehe int, fps float64, grenze int) []string {
	a := []string{"-c:v", "h264_nvenc", "-preset", "p4", "-profile:v", "high",
		"-level", levelText(H264Level(hoehe, fps)), "-rc", "vbr"}
	return append(a, rate(grenze, hoehe)...)
}
