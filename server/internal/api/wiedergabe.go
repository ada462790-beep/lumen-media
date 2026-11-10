// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

// Was alle Wege der Wiedergabe gemeinsam brauchen: welche Tonspur, ob ein
// Bild-Untertitel eingebrannt wird, wie ein HLS-Auftrag aussieht, und wie
// ein Gerät erfährt, dass seine Wiedergabe vorbei ist.

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"lumen/internal/store"
	"lumen/internal/stream"
)

// streamFehler antwortet mit einem Fehler, der neben dem Text einen festen
// Code trägt. Der Text ist für Menschen und darf sich ändern; nach dem
// Code richten sich die Apps.
func streamFehler(w http.ResponseWriter, status int, code, text string) {
	writeJSON(w, status, map[string]string{"error": tr(w, text), "code": code})
}

// wiedergabeVorbei: 410 mit dem Grund, aus dem die Wiedergabe endete.
//
//   - wiedergabe_abgelaufen: zu lange nichts gehört (lange Pause, Netz
//     weg). Das Gerät setzt einfach neu an: master.m3u8 noch einmal holen
//     und an die Stelle springen, an der es stand.
//   - wiedergabe_beendet: ausdrücklich beendet — nicht von selbst neu
//     ansetzen, sondern es der Person sagen.
//   - wiedergabe_ersetzt: auf diesem Gerät läuft inzwischen etwas anderes.
//
// Ist der Grund nicht (mehr) bekannt, gilt „abgelaufen": dann ist es lange
// her, und neu ansetzen schadet nicht.
func (s *Server) wiedergabeVorbei(w http.ResponseWriter, r *http.Request, sid string) {
	grund := s.live.Grund(sid)
	if grund == stream.GrundBildschirmzeit {
		// Beendet wegen der Bildschirmzeit: dieselbe Meldung wie beim
		// Start. Hat die Verwaltung inzwischen Zeit geschenkt, darf das
		// Gerät einfach neu ansetzen.
		if l, gilt := s.bildschirmzeitLage(r.Context(), accountOf(r)); gilt && !l.urteil.Erlaubt {
			bildschirmzeitFehler(w, l)
			return
		}
		grund = stream.GrundAbgelaufen
	}
	text := "die wiedergabe war zu lange still und ist abgelaufen — sie lässt sich einfach neu starten"
	switch grund {
	case stream.GrundBeendet:
		text = "die wiedergabe wurde beendet"
	case stream.GrundErsetzt:
		text = "auf diesem gerät läuft inzwischen etwas anderes"
	default:
		grund = stream.GrundAbgelaufen
	}
	w.Header().Set("X-Lumen-Grund", grund)
	w.Header().Set("Cache-Control", "no-store")
	streamFehler(w, http.StatusGone, grund, text)
}

// ---------------------------------------------------------------- Spuren

// spurWahl ist, was ein Gerät für diese Wiedergabe gewählt hat.
type spurWahl struct {
	ton        *store.Track // die Tonspur, die läuft (nil: keine)
	tonIndex   int          // für -map; -1 = die erste
	einbrennen *store.Track // Bild-Untertitel zum Einbrennen (nil: keiner)
}

// spurWahlFuer liest ?spur= (Tonspur) und ?untertitel= (Untertitel).
//
// untertitel: fehlt der Parameter und darf automatisch gewählt werden
// (auto), brennt Lumen einen erzwungenen Bild-Untertitel in der Sprache
// der Tonspur ein — die Übersetzung fremdsprachiger Stellen, die sonst
// fehlte. „-1" oder „aus" heisst ausdrücklich keiner; eine Nummer wählt
// eine Spur. Text-Untertitel stellt das Gerät selbst dar, sie werden nie
// eingebrannt. Kann das Gerät Bild-Untertitel selbst (bildUntertitel=1),
// wird nichts automatisch eingebrannt.
func (s *Server) spurWahlFuer(ctx context.Context, f *store.MediaFile, r *http.Request, auto bool) spurWahl {
	w := spurWahl{tonIndex: -1}
	if !s.st.SpurenGelesen(ctx, f.ID) {
		s.spurenNachmessen(ctx, f)
	}
	spuren, _ := s.st.Spuren(ctx, f.ID)
	gewaehlt := qInt(r, "spur", -1)
	for i := range spuren {
		t := &spuren[i]
		if t.Kind != "audio" {
			continue
		}
		if gewaehlt >= 0 && t.Index == gewaehlt {
			w.ton, w.tonIndex = t, t.Index
			break
		}
		if w.ton == nil {
			w.ton = t // die erste, falls die gewählte nicht dabei ist
		}
	}
	if w.tonIndex < 0 {
		// Die gewählte gibt es nicht: die erste, und -map nimmt die erste.
		w.tonIndex = -1
	}

	q := r.URL.Query()
	ut := strings.TrimSpace(q.Get("untertitel"))
	switch {
	case ut == "-1" || strings.EqualFold(ut, "aus"):
		return w
	case ut != "":
		n, err := strconv.Atoi(ut)
		if err != nil {
			return w
		}
		for i := range spuren {
			t := &spuren[i]
			if t.Index == n && t.Kind == "subtitle" && t.Bitmap && !t.External {
				w.einbrennen = t
			}
		}
		return w
	}
	if !auto || q.Get("bildUntertitel") == "1" || w.ton == nil {
		return w
	}
	sprache := w.ton.Language
	if sprache == "" || sprache == "und" {
		return w
	}
	for i := range spuren {
		t := &spuren[i]
		if t.Kind == "subtitle" && t.Bitmap && t.Forced && !t.External && t.Language == sprache {
			w.einbrennen = t
			break
		}
	}
	return w
}

// dateiInfoFuer ist dateiInfo mit der gewählten Tonspur und dem
// Einbrennen — beides entscheidet mit, ob es direkt geht.
func (s *Server) dateiInfoFuer(f *store.MediaFile, w spurWahl) stream.FileInfoT {
	info := stream.FileInfo(f.VideoCodec, f.Container, f.Height, f.Bitrate, f.HDR)
	if w.ton != nil {
		info = info.MitTon(w.ton.Codec)
	}
	if w.einbrennen != nil {
		info = info.MitEinbrennen()
	}
	return info
}

// ---------------------------------------------------------------- HLS

// hlsAuftrag baut den Auftrag für eine HLS-Wiedergabe. Er wird bei jeder
// Anfrage (Master, Liste, Segment) aus Datei und Anfrage gleich neu
// gebaut — die teuren Teile (Feinmessung, Schlüsselbilder) kommen aus
// dem Zwischenspeicher.
func (s *Server) hlsAuftrag(r *http.Request, f *store.MediaFile, sid string) (j stream.Job) {
	ctx := r.Context()
	wahl := s.spurWahlFuer(ctx, f, r, true)
	dev := deviceFromQuery(r)
	plan := stream.Decide(s.dateiInfoFuer(f, wahl), dev, bitrateLimit(r, s)).FuerHLS()

	details, _ := s.ff.Feinmessung(ctx, f.Path)
	j = stream.Job{
		SessionID: sid, SourcePath: f.Path, DurationMS: f.DurationMS,
		Plan: plan, AudioIndex: wahl.tonIndex, QuellCodec: f.VideoCodec, Details: details,
	}
	if wahl.ton != nil {
		j.TonCodec = wahl.ton.Codec
		j.TonKopieren = stream.TonKopierbar(wahl.ton.Codec, dev)
	}
	defer func() {
		// fMP4 (kopiertes HEVC): AC3 und E-AC3 lassen sich dort nicht
		// kopieren — FFmpeg muss den Kopf schreiben, bevor es den Ton
		// gesehen hat, und bricht ab. Dann AAC, für ein Gerät mit
		// Mehrkanalton in 5.1.
		if j.FMP4() && j.TonKopieren && stream.TonName(j.TonCodec) != "aac" {
			j.TonKopieren = false
		}
		if j.FMP4() && !j.TonKopieren && wahl.ton != nil && wahl.ton.Channels >= 6 &&
			(stream.TonKopierbar("ac3", dev) || stream.TonKopierbar("eac3", dev)) {
			j.TonKanaele = 6
		}
	}()
	if wahl.einbrennen != nil {
		j.MitEinbrennen, j.Einbrennen = true, wahl.einbrennen.Index
	}
	if plan.VideoCodec == "copy" {
		// H.264 mit 10 Bit oder 4:2:2 heisst zwar „h264", spielt aber
		// kaum ein Gerät. Dann lieber neu rechnen.
		if details != nil && strings.EqualFold(details.Codec, "h264") &&
			details.PixFmt != "" && details.PixFmt != "yuv420p" && details.PixFmt != "yuvj420p" {
			plan.VideoCodec, plan.Reason = "h264", "Bildformat kann das Gerät nicht"
		} else {
			start := 0.0
			if details != nil {
				start = details.Start
			}
			j.Grenzen = stream.KopierGrenzen(f.Path, f.DurationMS, start)
			if j.Grenzen == nil {
				// Ohne Verzeichnis der Schlüsselbilder liessen sich die
				// Segmente nach einem Sprung nicht wieder genau treffen.
				plan.VideoCodec = "h264"
			}
		}
		j.Plan = plan
	}
	// Neu gerechnet wird immer in SDR: Ein HDR-Bild braucht dann
	// Tonemapping, sonst kommt es blass und grünstichig an.
	if plan.VideoCodec != "copy" && (f.HDR != "" || details.HDR()) {
		j.Tonemap = true
	}
	return j
}

// ---------------------------------------------------------------- Melden

// meldeAbstand: so oft meldet sich ein laufender Strom höchstens. Das
// Melden kostet eine Sperre; bei jedem 64-KB-Häppchen wäre das zu oft.
const meldeAbstand = 10 * time.Second

// melder hält eine Wiedergabe am Leben, solange ihre Anfrage ausliefert.
// Früher meldeten sich nur HLS-Segmente — ein Browser-Strom oder eine
// Direktwiedergabe galt nach zwei Minuten als verwaist und wurde mitten
// im Film abgebrochen („Strom ist abgerissen").
type melder struct {
	live  *stream.Live
	id    string
	mu    sync.Mutex
	letzt time.Time
}

func (m *melder) melden() {
	if m == nil || m.live == nil {
		return
	}
	m.mu.Lock()
	faellig := time.Since(m.letzt) >= meldeAbstand
	if faellig {
		m.letzt = time.Now()
	}
	m.mu.Unlock()
	if faellig {
		m.live.Melden(m.id)
	}
}
