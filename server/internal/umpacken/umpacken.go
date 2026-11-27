// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

// Package umpacken legt Filme aus älteren Behältern (AVI, WMV, MPG, VOB,
// FLV, MOV, MP4, OGM, 3GP, TS …) ohne Neurechnen in MKV um.
//
// Bild und Ton bleiben Byte für Byte, wie sie sind (`-c copy`). Nur was
// Matroska nicht aufnimmt, wird angepasst:
//
//   - Text-Untertitel aus MP4/MOV (mov_text) und andere Textformate werden
//     SubRip — Text bleibt Text, nichts geht verloren.
//   - LPCM von DVDs (pcm_dvd, pcm_bluray) wird FLAC — verlustfrei, wie beim
//     Umwandeln von Scheiben (internal/scheibe).
//   - Daten- und Zeitcode-Spuren (tmcd, DVD-Navigation), Teletext,
//     Closed Captions und eingebettete Titelbilder fallen weg: Matroska
//     kennt sie nicht (Titelbilder würden eine zweite Bildspur), und
//     Lumen kann sie ohnehin nicht zeigen.
//   - Eine Untertitelspur, die Lumen zeigen könnte, die aber nicht in MKV
//     passt (XSUB aus DivX-Dateien), hält das Umpacken auf: Die Datei
//     bleibt dann, wie sie ist.
//
// Das Paket plant und prüft nur; wohin geschrieben wird, was mit dem
// Original geschieht und wie die Datenbank nachgeführt wird, regelt der
// Aufrufer (internal/api, mkv_umpacken.go).
package umpacken

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Endungen: die älteren Behälter, die sich umpacken lassen — dieselben,
// die der Scanner als Film nimmt (scan.VideoExt), ohne MKV und WebM (das
// schon Matroska ist).
var Endungen = map[string]bool{
	".avi": true, ".divx": true, ".xvid": true, ".wmv": true, ".asf": true,
	".mpg": true, ".mpeg": true, ".vob": true, ".flv": true, ".mov": true,
	".mp4": true, ".m4v": true, ".ogm": true, ".3gp": true,
	".ts": true, ".m2ts": true, ".mts": true,
}

// Endung: die Endung in Kleinbuchstaben, wenn die Datei umgepackt werden
// kann, sonst "".
func Endung(pfad string) string {
	e := strings.ToLower(filepath.Ext(pfad))
	if Endungen[e] {
		return e
	}
	return ""
}

// InAllen: Gehört die Endung zu „Alle umwandeln"? MP4 und M4V nicht: Sie
// laufen in jedem Browser und auf jedem Apple-Gerät direkt, MKV dagegen
// muss Lumen für Safari und iOS erst umpacken — „Alle" würde dort nichts
// besser und manches langsamer machen. Einzeln lassen sie sich trotzdem
// umwandeln (etwa wegen mov_text-Untertiteln).
func InAllen(endung string) bool {
	return Endungen[endung] && endung != ".mp4" && endung != ".m4v"
}

// Ziel: die MKV neben der Datei — gleicher Name, nur die Endung anders.
// Untertitel, NFO und Poster daneben (gleicher Basisname) passen weiter.
func Ziel(pfad string) string {
	return strings.TrimSuffix(pfad, filepath.Ext(pfad)) + ".mkv"
}

// TempEndung kennzeichnet die halbfertige MKV. Der Punkt vorne versteckt
// sie vor dem Scanner (versteckte Dateien liest er nicht).
const TempEndung = ".lumen-tmp.mkv"

// Temp: die vorläufige Datei im selben Ordner wie das Original — damit
// das Umbenennen am Ende auf demselben Dateisystem bleibt (atomar).
func Temp(pfad string) string {
	basis := strings.TrimSuffix(filepath.Base(pfad), filepath.Ext(pfad))
	return filepath.Join(filepath.Dir(pfad), "."+basis+TempEndung)
}

// IstTemp: ist das eine vorläufige Datei dieses Pakets?
func IstTemp(pfad string) bool {
	b := filepath.Base(pfad)
	return strings.HasPrefix(b, ".") && strings.HasSuffix(b, TempEndung)
}

// Spur ist ein Strom der Quelle, wie ffprobe ihn sieht.
type Spur struct {
	Index     int
	Art       string // video, audio, subtitle, data, attachment
	Codec     string
	Titelbild bool // eingebettetes Titelbild (attached_pic)
}

// Bestand: was in einer Datei steckt.
type Bestand struct {
	Format  string
	DauerMS int64
	Spuren  []Spur
}

// schwerZuFinden: In MPEG-Programm- und Transportströmen tauchen
// Untertitel und zweite Tonspuren oft erst nach Minuten auf. Dort schauen
// ffprobe und FFmpeg weiter hinein — beide gleich weit, damit FFmpeg genau
// die Spuren findet, die der Plan nennt.
func suchweite(pfad string) []string {
	switch strings.ToLower(filepath.Ext(pfad)) {
	case ".mpg", ".mpeg", ".vob", ".ts", ".m2ts", ".mts":
		return []string{"-probesize", "400M", "-analyzeduration", "600M"}
	}
	return []string{"-probesize", "50M", "-analyzeduration", "100M"}
}

// Lesen lässt ffprobe die Spuren und die Laufzeit nennen. eingang sind die
// Angaben vor „-i" (stream.Eingang: Dateien mit Müll am Anfang).
func Lesen(ctx context.Context, ffprobe, pfad string, eingang []string) (*Bestand, error) {
	args := []string{"-v", "error"}
	args = append(args, suchweite(pfad)...)
	args = append(args, eingang...)
	args = append(args, "-show_entries",
		"format=format_name,duration:stream=index,codec_type,codec_name:stream_disposition=attached_pic",
		"-of", "json", "-i", pfad)
	cmd := exec.CommandContext(ctx, ffprobe, args...)
	var fehl bytes.Buffer
	cmd.Stderr = &fehl
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("ffprobe: %s", letzteZeile(fehl.String(), err))
	}
	return lesenJSON(out)
}

func lesenJSON(out []byte) (*Bestand, error) {
	var j struct {
		Format struct {
			FormatName string `json:"format_name"`
			Duration   string `json:"duration"`
		} `json:"format"`
		Streams []struct {
			Index       int    `json:"index"`
			CodecType   string `json:"codec_type"`
			CodecName   string `json:"codec_name"`
			Disposition struct {
				AttachedPic int `json:"attached_pic"`
			} `json:"disposition"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(out, &j); err != nil {
		return nil, err
	}
	b := &Bestand{Format: j.Format.FormatName}
	if d, err := strconv.ParseFloat(j.Format.Duration, 64); err == nil && d > 0 && !math.IsInf(d, 0) {
		b.DauerMS = int64(math.Round(d * 1000))
	}
	for _, s := range j.Streams {
		b.Spuren = append(b.Spuren, Spur{Index: s.Index, Art: s.CodecType, Codec: s.CodecName,
			Titelbild: s.CodecType == "video" && s.Disposition.AttachedPic == 1})
	}
	return b, nil
}

// Untertitel, die Matroska so aufnimmt, wie sie sind.
var mkvUntertitel = map[string]bool{
	"subrip": true, "srt": true, "ass": true, "ssa": true, "webvtt": true, "text": true,
	"dvd_subtitle": true, "dvb_subtitle": true, "hdmv_pgs_subtitle": true,
	"hdmv_text_subtitle": true, "arib_caption": true,
}

// Text-Untertitel, die FFmpeg lesen und als SubRip schreiben kann.
var textUntertitel = map[string]bool{
	"mov_text": true, "microdvd": true, "subviewer": true, "subviewer1": true, "sami": true,
	"realtext": true, "mpl2": true, "pjs": true, "vplayer": true, "jacosub": true, "stl": true,
}

// Untertitel, die Lumen zeigen könnte, die aber in kein MKV passen. Dann
// lieber gar nicht umpacken als die Spur verlieren.
var unpassendeUntertitel = map[string]bool{"xsub": true}

// PlanSpur: eine Spur der neuen MKV.
type PlanSpur struct {
	Spur
	// Neu: "" = unverändert kopieren, sonst der Codec, in den gewandelt
	// wird ("srt" für Text-Untertitel, "flac" für LPCM).
	Neu string
}

// Codec, den ffprobe in der MKV für die Spur nennen wird.
func (p PlanSpur) codecDanach() string {
	switch p.Neu {
	case "srt":
		return "subrip"
	case "":
		if p.Codec == "srt" {
			return "subrip"
		}
		return p.Codec
	}
	return p.Neu
}

// Plan: wie aus der Quelle die MKV wird.
type Plan struct {
	Quelle  string
	Bestand *Bestand
	Spuren  []PlanSpur
	// Weggelassen: Spuren, die nicht in die MKV kommen (Daten, Teletext,
	// Titelbild …) — als kurze Beschreibung fürs Protokoll.
	Weggelassen []string
	// Gewandelt: Spuren, die angepasst werden (Text-Untertitel → SubRip,
	// LPCM → FLAC).
	Gewandelt []string
}

// SpurPasstNicht: Die Datei hat eine Spur, die Lumen zeigen kann, die
// aber nicht in eine MKV passt.
type SpurPasstNicht struct{ Codec string }

func (e *SpurPasstNicht) Error() string {
	return "spur passt nicht in mkv: " + e.Codec
}

// ErrKeinBild: Die Quelle hat keine Bildspur (oder ffprobe fand keine).
var ErrKeinBild = errors.New("keine bildspur gefunden")

// Planen legt fest, welche Spuren wie in die MKV kommen.
func Planen(quelle string, b *Bestand) (*Plan, error) {
	p := &Plan{Quelle: quelle, Bestand: b}
	bild := false
	for _, s := range b.Spuren {
		ps := PlanSpur{Spur: s}
		switch s.Art {
		case "video":
			if s.Titelbild {
				p.Weggelassen = append(p.Weggelassen, "Titelbild ("+s.Codec+")")
				continue
			}
			bild = true
		case "audio":
			if s.Codec == "pcm_dvd" || s.Codec == "pcm_bluray" {
				ps.Neu = "flac"
				p.Gewandelt = append(p.Gewandelt, s.Codec+" → FLAC")
			}
		case "subtitle":
			switch {
			case mkvUntertitel[s.Codec]:
			case textUntertitel[s.Codec]:
				ps.Neu = "srt"
				p.Gewandelt = append(p.Gewandelt, s.Codec+" → SubRip")
			case unpassendeUntertitel[s.Codec]:
				return nil, &SpurPasstNicht{Codec: s.Codec}
			default:
				// Teletext, Closed Captions, Unbekanntes: Lumen bietet
				// solche Spuren auch heute nicht an (store: unbrauchbar).
				name := s.Codec
				if name == "" {
					name = "unbekannt"
				}
				p.Weggelassen = append(p.Weggelassen, "Untertitel ("+name+")")
				continue
			}
		case "attachment":
			// Schriften und Ähnliches (OGM kennt das kaum) nimmt Matroska
			// als Anhang auf.
		default:
			name := s.Codec
			if name == "" {
				name = s.Art
			}
			p.Weggelassen = append(p.Weggelassen, "Daten ("+name+")")
			continue
		}
		p.Spuren = append(p.Spuren, ps)
	}
	if !bild {
		return nil, ErrKeinBild
	}
	return p, nil
}

// Argumente: der Aufruf von FFmpeg, der den Plan nach ziel schreibt.
func Argumente(p *Plan, eingang []string, ziel string) []string {
	a := []string{"-hide_banner", "-loglevel", "error", "-nostdin", "-y",
		// Alte AVI (XviD mit „packed bitstream", kaputter Index) und MPG
		// haben oft Pakete ohne Zeitstempel; Matroska verlangt welche.
		"-fflags", "+genpts"}
	a = append(a, suchweite(p.Quelle)...)
	a = append(a, eingang...)
	a = append(a, "-i", p.Quelle)
	for _, s := range p.Spuren {
		a = append(a, "-map", fmt.Sprintf("0:%d", s.Index))
	}
	// Titel, Jahr, Sprache je Spur, Standard- und Zwangsmarken bleiben.
	a = append(a, "-map_metadata", "0", "-map_chapters", "0", "-c", "copy")
	for i, s := range p.Spuren {
		if s.Neu != "" {
			a = append(a, fmt.Sprintf("-c:%d", i), s.Neu)
		}
		// XviD/DivX mit „packed B-frames" spielt in MKV ruckelig; der
		// Filter packt die Bilder nur um (kein Neurechnen) und lässt
		// alles andere durch.
		if s.Art == "video" && s.Codec == "mpeg4" {
			a = append(a, fmt.Sprintf("-bsf:%d", i), "mpeg4_unpack_bframes")
		}
	}
	a = append(a, "-max_muxing_queue_size", "8192", "-f", "matroska", ziel)
	return a
}

// Umpacken führt FFmpeg aus. ausfuehren startet den Befehl und wartet
// (nil: CombinedOutput; Lumen senkt dort die Priorität). melden bekommt
// etwa jede Sekunde die Grösse der neuen Datei.
func Umpacken(ctx context.Context, ffmpeg string, p *Plan, eingang []string, ziel string,
	ausfuehren func(*exec.Cmd) ([]byte, error), melden func(geschrieben int64)) error {
	cmd := exec.CommandContext(ctx, ffmpeg, Argumente(p, eingang, ziel)...)
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
					if info, err := os.Stat(ziel); err == nil {
						melden(info.Size())
					}
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
	return nil
}

// Abweichung: Die neue Datei passt nicht zum Plan. Art sagt, was nicht
// stimmt ("spuren", "dauer", "groesse"); die Zahlen sind für die Meldung.
type Abweichung struct {
	Art          string
	Soll, Ist    int64 // Spuren: Anzahl; Dauer: ms; Grösse: Bytes
	Beschreibung string
}

func (a *Abweichung) Error() string { return "prüfung: " + a.Beschreibung }

// DauerSpiel: so weit darf die Laufzeit der MKV von der des Originals
// abweichen. Matroska rundet auf Millisekunden und misst bis zum Ende des
// letzten Pakets; mehr als eine Sekunde heisst, dass etwas fehlt.
const DauerSpiel = 1000

// Mindestanteil: so gross muss die MKV im Verhältnis zum Original
// mindestens sein. Bild und Ton werden kopiert, nur der Behälter wird
// schlanker: AVI, WMV und MPEG-PS tragen ein paar Prozent Verwaltung,
// MPEG-TS deutlich mehr (188-Byte-Pakete, Füllpakete bei
// Fernsehaufnahmen — dort sind 20–30 % weniger normal).
func Mindestanteil(quelle string) float64 {
	switch strings.ToLower(filepath.Ext(quelle)) {
	case ".ts", ".m2ts", ".mts":
		return 0.60
	}
	return 0.85
}

// Pruefen vergleicht die fertige MKV mit dem Plan: dieselbe Zahl an Bild-,
// Ton- und Untertitelspuren mit den erwarteten Codecs, dieselbe Laufzeit
// (±DauerSpiel) und eine plausible Grösse. Erst danach darf die MKV das
// Original ersetzen.
func Pruefen(ctx context.Context, ffprobe string, p *Plan, mkv string, quelleGroesse int64) error {
	info, err := os.Stat(mkv)
	if err != nil {
		return err
	}
	neu, err := Lesen(ctx, ffprobe, mkv, nil)
	if err != nil {
		return err
	}
	return Vergleichen(p, neu, info.Size(), quelleGroesse)
}

// Vergleichen ist der Kern von Pruefen (ohne ffprobe, zum Testen).
func Vergleichen(p *Plan, neu *Bestand, neuGroesse, quelleGroesse int64) error {
	soll := map[string][]string{}
	for _, s := range p.Spuren {
		soll[s.Art] = append(soll[s.Art], s.codecDanach())
	}
	ist := map[string][]string{}
	for _, s := range neu.Spuren {
		ist[s.Art] = append(ist[s.Art], s.Codec)
	}
	for _, art := range []string{"video", "audio", "subtitle"} {
		a, b := append([]string(nil), soll[art]...), append([]string(nil), ist[art]...)
		sort.Strings(a)
		sort.Strings(b)
		if strings.Join(a, ",") != strings.Join(b, ",") {
			return &Abweichung{Art: "spuren", Soll: int64(len(a)), Ist: int64(len(b)),
				Beschreibung: fmt.Sprintf("%s: erwartet [%s], gefunden [%s]", art, strings.Join(a, ","), strings.Join(b, ","))}
		}
	}
	if d := p.Bestand.DauerMS; d > 0 {
		if neu.DauerMS <= 0 || abs(neu.DauerMS-d) > DauerSpiel {
			return &Abweichung{Art: "dauer", Soll: d, Ist: neu.DauerMS,
				Beschreibung: fmt.Sprintf("laufzeit %d ms statt %d ms", neu.DauerMS, d)}
		}
	} else if neu.DauerMS <= 0 {
		return &Abweichung{Art: "dauer", Beschreibung: "die neue datei hat keine laufzeit"}
	}
	if quelleGroesse > 0 && float64(neuGroesse) < Mindestanteil(p.Quelle)*float64(quelleGroesse) {
		return &Abweichung{Art: "groesse", Soll: quelleGroesse, Ist: neuGroesse,
			Beschreibung: fmt.Sprintf("%d bytes statt etwa %d", neuGroesse, quelleGroesse)}
	}
	return nil
}

func abs(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}

func letzteZeile(s string, err error) string {
	zeilen := strings.Split(strings.TrimSpace(s), "\n")
	for i := len(zeilen) - 1; i >= 0; i-- {
		if z := strings.TrimSpace(zeilen[i]); z != "" {
			return z
		}
	}
	return err.Error()
}
