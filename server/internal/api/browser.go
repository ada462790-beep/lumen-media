// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"io"
	"net/http"
	"strconv"
	"time"

	"lumen/internal/store"
	"lumen/internal/stream"
)

// browserStream ist der Weg für die Web-App: ein Strom, den jeder Browser
// von sich aus abspielt — ohne Fremdbibliothek.
//
// Gesprungen wird, indem die App den Strom an der neuen Stelle neu anfordert
// (?t=Sekunden). Die Oberfläche merkt sich den Versatz und rechnet ihn auf
// die angezeigte Zeit auf, sodass es sich anfühlt wie normales Spulen.
func (s *Server) browserStream(w http.ResponseWriter, r *http.Request) {
	acc, dev := accountOf(r), deviceOf(r)
	// Scheitert der Strom, sieht das <video> nur „error". Den Text merkt
	// sich Lumen zur Kennung des Players, damit die App ihn zeigen kann
	// (laeufe.go).
	lauf := laufVon(r)
	scheitern := func(w http.ResponseWriter, status int, msg string, werte ...any) {
		s.laeufe.setzen(acc.ID, lauf, laufEintrag{fehler: tr(w, msg, werte...)})
		failW(w, status, msg, werte...)
	}

	f, _, err := s.fileForRequest(r)
	if err != nil {
		failErr(w, err)
		return
	}
	if f.Missing {
		scheitern(w, http.StatusGone, "die datei ist nicht mehr da")
		return
	}
	// Bildschirmzeit: Der Browser sieht den Status nicht — darum merkt
	// sich der Lauf den Code, und /browser/grund sagt ihn.
	if l, gilt := s.bildschirmzeitLage(r.Context(), acc); gilt && !l.urteil.Erlaubt {
		s.laeufe.setzen(acc.ID, lauf, laufEintrag{fehler: bildschirmzeitText(w, l), code: stream.GrundBildschirmzeit})
		bildschirmzeitFehler(w, l)
		return
	}
	if !s.ff.Available() {
		scheitern(w, http.StatusServiceUnavailable,
			"ohne FFmpeg kann dieser server nichts für den browser vorbereiten")
		return
	}

	lim, _ := s.st.Limits(r.Context(), acc.ID)
	caps := browserDevice(r)
	if len(caps.Codecs) == 0 {
		scheitern(w, http.StatusNotAcceptable,
			"dieser browser bringt keinen video-decoder mit — "+
				"nimm Chrome, Safari, Firefox oder Edge")
		return
	}
	bp := s.browserPlan(r, f, caps)
	if bp.mode == "transcode" && !lim.AllowTranscode {
		scheitern(w, http.StatusForbidden,
			"dieses gerät bräuchte eine umwandlung, die für dein profil aus ist")
		return
	}

	start := float64(qInt(r, "t", 0))
	if start < 0 {
		start = 0
	}
	// Beim Umpacken steigt FFmpeg auf einem Schlüsselbild ein, nicht exakt
	// an der gewünschten Sekunde. Wo das sein wird, bestimmen wir vorher —
	// und sagen es dem Browser, damit er die richtige Zeit anzeigt.
	if start > 0 && bp.plan.VideoCodec == "copy" {
		start = s.ff.KeyframeBefore(r.Context(), f.Path, start)
	}

	// Dasselbe Gerät spielt immer nur eine Sache: ein Sprung im Film startet
	// den Strom neu, ist aber keine zweite Wiedergabe.
	s.live.ReplaceFor(acc.ID, dev.Name)
	// Erst nach dem Ersetzen zählen: Ein Sprung im eigenen Film ist keine
	// weitere Wiedergabe und keine weitere Umwandlung.
	if n := s.live.CountFor(acc.ID); n >= lim.MaxStreams {
		scheitern(w, http.StatusTooManyRequests,
			"du schaust schon auf {n} Geräten — in spätestens 2 Minuten ist wieder ein Platz frei, "+
				"oder beende eine Wiedergabe unter Ich → Geräte", "n", n)
		return
	}
	if bp.mode == "transcode" {
		if voll, n := s.umwandlungVoll(r.Context()); voll {
			scheitern(w, http.StatusServiceUnavailable, umwandlungVollText, "n", n)
			return
		}
	}

	sess := s.live.Start(&stream.Session{
		AccountID: acc.ID, Name: acc.Name, Avatar: acc.Avatar,
		DeviceName: dev.Name, DeviceKind: dev.Kind, FileID: f.ID,
		Mode: bp.mode, Reason: bp.reason, Bitrate: bp.plan.Bitrate,
		FromCodec: f.VideoCodec, ToCodec: zielCodec(bp.plan, f),
		FromHeight: f.Height, ToHeight: zielHoehe(bp.plan, f),
		Drossel: drosselFuer(lim, f),
	})
	s.laeufe.setzen(acc.ID, lauf, laufEintrag{sid: sess.ID})
	// Endet der Strom, ohne dass jemand ihn beendet hat (FFmpeg bricht ab,
	// eine Minute nichts), darf der Player neu ansetzen: „abgelaufen".
	// Nur wer ganz bis zum Ende kam, ist „beendet". Wurde die Wiedergabe
	// von aussen beendet oder ersetzt, ist der Grund schon gemerkt — EndMit
	// überschreibt ihn nicht.
	endGrund := stream.GrundAbgelaufen
	defer func() { s.live.EndMit(sess.ID, endGrund) }()
	schliessen, _ := s.live.Oeffnen(sess.ID)
	defer schliessen()
	m := &melder{live: s.live, id: sess.ID, letzt: time.Now()}
	if start == 0 {
		go s.untertitelVorab(f)
	}

	job := stream.RemuxJob{
		SourcePath: f.Path, StartSec: start, Plan: bp.plan,
		AudioIndex: bp.wahl.tonIndex, WebM: bp.webm,
		Details: bp.details, Tonemap: bp.tonemap, Einbrennen: -1,
	}
	if bp.wahl.einbrennen != nil {
		job.MitEinbrennen, job.Einbrennen = true, bp.wahl.einbrennen.Index
	}

	// Mit Hardware kann der Start an einer bestimmten Datei scheitern (ein
	// Codec, den der Videoteil nicht kennt). Kommt dann gar nichts, wird es
	// eine Stufe einfacher noch einmal versucht — bis Software.
	var body io.ReadCloser
	var stop func()
	var erstes []byte
	for {
		body, stop, err = s.ff.Remux(r.Context(), job)
		if err != nil {
			// Ins Protokoll nur, dass es geschah; die Einzelheit nur in die
			// Fehlersuche — sie hängt an einer Datei.
			s.st.Log(r.Context(), "error", "stream", "Browser-Strom gescheitert")
			s.log.Debug("browser-strom", "err", stream.Bereinigt(err.Error()))
			scheitern(w, http.StatusInternalServerError, "wiedergabe lässt sich nicht vorbereiten")
			return
		}
		verf, stufe := stream.RemuxVerfahren(body)
		buf := make([]byte, 64*1024)
		n, rerr := io.ReadAtLeast(body, buf, 1)
		if n > 0 {
			erstes = buf[:n]
			if verf != "" {
				s.ff.HW.Erfolg(verf)
			}
			break
		}
		stop()
		if verf == "" || stufe >= stream.StufeSoftware || r.Context().Err() != nil {
			if r.Context().Err() == nil {
				msg := "kein Bild"
				if rerr != nil {
					msg = rerr.Error()
				}
				s.st.Log(r.Context(), "error", "stream", "Browser-Strom gescheitert")
				s.log.Debug("browser-strom", "err", stream.Bereinigt(msg))
				scheitern(w, http.StatusInternalServerError, "wiedergabe lässt sich nicht vorbereiten")
			}
			return
		}
		// Nur in die Fehlersuche: der Zeitpunkt hinge an einer Wiedergabe.
		// Was die Hardware kann, zeigt die Verwaltung unter „Umwandeln".
		if msg := s.ff.HW.Fehlschlag(verf); msg != "" {
			s.log.Debug("hardware", "meldung", msg)
		}
		job.Stufe = stufe + 1
	}
	defer stop()

	// Dieser Strom hat keine Länge und keine Sprungmarken — gesprungen wird
	// über ?t=. Darum ausdrücklich keine Bereichsanfragen anbieten.
	w.Header().Set("Content-Type", job.ContentType())
	w.Header().Set("Accept-Ranges", "none")
	w.Header().Set("Cache-Control", "no-store")
	// Wo der Strom im Film steht. Die Oberfläche rechnet das auf die
	// angezeigte Zeit auf — sonst stünde sie nach jedem Sprung daneben.
	w.Header().Set("X-Lumen-Start", strconv.FormatFloat(start, 'f', 3, 64))
	w.Header().Set("X-Lumen-Mode", bp.mode)
	w.WriteHeader(http.StatusOK)

	flusher, _ := w.(http.Flusher)
	// Ohne „Herunterladen" nicht schneller als etwa die doppelte Bitrate —
	// ein reines Umpacken liefe sonst mit Plattentempo durch.
	if sess.Drossel.Nehmen(r.Context(), len(erstes)) != nil {
		return
	}
	if _, werr := w.Write(erstes); werr != nil {
		return
	}
	buf := make([]byte, 64*1024)
	idle := time.Now()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-s.live.Stopped(sess.ID):
			// Der Besitzer hat beendet.
			return
		default:
		}

		n, err := body.Read(buf)
		if n > 0 {
			if sess.Drossel.Nehmen(r.Context(), n) != nil {
				return
			}
			if _, werr := w.Write(buf[:n]); werr != nil {
				return // Browser ist weg
			}
			if flusher != nil {
				flusher.Flush()
			}
			idle = time.Now()
			m.melden()
		}
		if err == io.EOF {
			endGrund = stream.GrundBeendet
			return
		}
		if err != nil {
			s.st.Log(r.Context(), "warn", "stream", "Strom abgebrochen")
			s.log.Debug("strom abgebrochen", "err", stream.Bereinigt(err.Error()))
			return
		}
		if time.Since(idle) > 60*time.Second {
			s.st.Log(r.Context(), "warn", "stream", "Strom lieferte eine Minute nichts")
			return
		}
	}
}

// browserWahl ist die Entscheidung für den Browser-Strom: einmal hier,
// damit browser.mp4 und /seek garantiert dasselbe meinen. Früher prüfte
// /seek die Höhe nicht — die Anzeige stand dann bis zu 20 s daneben, weil
// es mit einem Umpacken (Einstieg am Schlüsselbild) rechnete, während der
// Strom neu gerechnet wurde (Einstieg genau an der Sekunde).
type browserWahl struct {
	plan    stream.Plan
	webm    bool
	mode    string
	reason  string
	wahl    spurWahl
	details *stream.BildDetails
	tonemap bool
}

func (s *Server) browserPlan(r *http.Request, f *store.MediaFile, caps stream.Device) browserWahl {
	b := browserWahl{webm: stream.WebM(caps)}
	b.wahl = s.spurWahlFuer(r.Context(), f, r, true)
	b.plan = stream.Decide(s.dateiInfoFuer(f, b.wahl), caps, bitrateLimit(r, s))
	b.details, _ = s.ff.Feinmessung(r.Context(), f.Path)

	// Ein reines Umpacken ist keine Umwandlung: Bild und Ton bleiben, wie
	// sie sind. Das soll der Besitzer in der Auslastung auch so sehen.
	// Umgepackt wird nur H.264 — das, was Browser zuverlässig können —,
	// und nur, wenn auch Höhe, Bitrate (unterwegs!) und HDR passen; das
	// sagt Decide mit „copy".
	repack := !b.webm && f.VideoCodec == "h264" && (b.plan.Direct || b.plan.VideoCodec == "copy")
	if repack && b.details != nil && b.details.PixFmt != "" &&
		b.details.PixFmt != "yuv420p" && b.details.PixFmt != "yuvj420p" {
		repack = false // 10 Bit oder 4:2:2 kann kaum ein Browser
	}
	b.mode, b.reason = "transcode", b.plan.Reason
	switch {
	case repack:
		b.mode, b.reason = "direct", "nur umgepackt für den Browser"
		b.plan.VideoCodec = "copy"
	case b.webm:
		b.plan.VideoCodec = "vp9"
		if b.reason == "" {
			b.reason = "Browser ohne H.264"
		}
	default:
		b.plan.VideoCodec = "h264"
		if b.reason == "" {
			b.reason = "Bildformat kann der Browser nicht"
		}
	}
	b.tonemap = b.plan.VideoCodec != "copy" && (f.HDR != "" || b.details.HDR())
	return b
}

// browserDevice baut die Gerätebeschreibung aus dem, was die App gemeldet
// hat. Meldet sie nichts, nehmen wir H.264 an — das kann fast jeder Browser.
func browserDevice(r *http.Request) stream.Device {
	dev := deviceFromQuery(r)
	if r.URL.Query().Get("codecs") == "" {
		return stream.BrowserDevice(qInt(r, "maxHeight", 0))
	}
	if dev.MaxHeight <= 0 {
		dev.MaxHeight = 2160
	}
	return dev
}

// seekPoint sagt, wo ein Strom wirklich beginnen würde, wenn man an diese
// Stelle springt — ohne dafür etwas zu starten.
//
// Ohne diesen Endpunkt müsste die Oberfläche den Strom selbst anfragen, um
// die Kopfzeile zu lesen. Genau das hat anfangs als zweite Wiedergabe
// gezählt und die eigene Grenze ausgelöst.
func (s *Server) seekPoint(w http.ResponseWriter, r *http.Request) {
	f, _, err := s.fileForRequest(r)
	if err != nil {
		failErr(w, err)
		return
	}
	want := float64(qInt(r, "t", 0))
	if want < 0 {
		want = 0
	}

	bp := s.browserPlan(r, f, browserDevice(r))
	start := want
	// Nur beim Umpacken ist der Einstieg an Schlüsselbilder gebunden; wird
	// neu gerechnet, beginnt der Strom genau an der gewünschten Stelle.
	if want > 0 && bp.plan.VideoCodec == "copy" && s.ff.Available() {
		// Höchstens vier Messungen gleichzeitig — wer wild spult, soll den
		// Rechner nicht mit ffprobe-Prozessen fluten.
		select {
		case sprungPlaetze <- struct{}{}:
			start = s.ff.KeyframeBefore(r.Context(), f.Path, want)
			<-sprungPlaetze
		case <-r.Context().Done():
			return
		}
	}
	ok(w, map[string]any{"requested": want, "start": start, "mode": bp.mode})
}

var sprungPlaetze = make(chan struct{}, 4)
