// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"lumen/internal/store"
	"lumen/internal/stream"
)

// Der Weg einer umgewandelten Wiedergabe:
//
//   1. Das Gerät holt master.m3u8 — dabei entsteht die Sitzung.
//   2. Darin steht media.m3u8 mit der Sitzungskennung. Diese Liste ist
//      vollständig: alle Segmente von Anfang bis Ende, berechnet aus der
//      Laufzeit. Deshalb kann das Gerät sofort an jede Stelle springen.
//   3. Für jedes Segment fragt es einzeln nach. Liegt es weit weg von dem,
//      was gerade gerechnet wird, setzt FFmpeg dort neu an.
//
// Der Besitzer sieht von all dem nur: „wird umgewandelt", von welcher
// Qualität in welche, Bandbreite. Keine Person, kein Gerät, kein Titel.
//
// Die Sitzungskennung in der Adresse ist kein Ausweis: Jede Anfrage an
// media.m3u8, die Segmente und stop wird gegen die laufenden Wiedergaben
// geprüft — sie muss dem anfragenden Konto gehören und genau diese Datei
// ausliefern. Eine erfundene Kennung startet keine Umwandlung.

func (s *Server) hlsMaster(w http.ResponseWriter, r *http.Request) {
	f, _, err := s.fileForRequest(r)
	if err != nil {
		failErr(w, err)
		return
	}
	if f.Missing {
		fail(w, http.StatusGone, "die datei ist nicht mehr da")
		return
	}
	if defektAbweisen(w, f) {
		return
	}
	// Bildschirmzeit vor allem anderen: Ausserhalb der Zeit entsteht
	// keine Sitzung (bildschirmzeit.go).
	if s.bildschirmzeitGesperrt(w, r) {
		return
	}
	if s.tr == nil || !s.ff.Available() {
		fail(w, http.StatusServiceUnavailable,
			"ohne FFmpeg kann dieser server nicht umwandeln")
		return
	}
	acc, dev := accountOf(r), deviceOf(r)
	if f.DurationMS <= 0 {
		fail(w, http.StatusUnprocessableEntity,
			"die laufzeit dieser datei ist unbekannt — sie lässt sich nicht umwandeln")
		return
	}

	job := s.hlsAuftrag(r, f, "")
	plan := job.Plan
	// Nur umgepackt (Bild kopiert, allenfalls der Ton gewandelt) ist keine
	// Umwandlung: kaum Last, und es braucht das Recht dazu nicht — wie
	// beim Umpacken für den Browser.
	mode, reason := "transcode", plan.Reason
	if job.Kopie() {
		mode = "direct"
		if reason == "" || reason == "Dateiformat passt nicht zum Gerät" {
			reason = "nur umgepackt"
		}
	}

	lim, _ := s.st.Limits(r.Context(), acc.ID)
	if mode == "transcode" && !lim.AllowTranscode {
		fail(w, http.StatusForbidden,
			"für dein profil ist das umwandeln ausgeschaltet")
		return
	}
	// Ein Gerät spielt eine Sache: Fragt es neu an (anderer Film, andere
	// Tonspur, nach einer Pause), endet seine alte Wiedergabe — und zählt
	// weder bei der eigenen Grenze noch bei der des Servers mit.
	// Prüfen und Anmelden in einem Zug: Sonst kämen zwei gleichzeitige
	// Anfragen beide an der Grenze vorbei (Profil-Grenze wie Server-Grenze
	// fürs Umwandeln).
	hlsStartMu.Lock()
	gesperrt := true
	defer func() {
		if gesperrt {
			hlsStartMu.Unlock()
		}
	}()
	s.live.ReplaceFor(acc.ID, geraetKennung(dev))
	if n := s.live.CountFor(acc.ID); n >= lim.MaxStreams {
		failW(w, http.StatusTooManyRequests,
			"du schaust schon auf {n} Geräten — in spätestens 2 Minuten ist wieder ein Platz frei, "+
				"oder beende eine Wiedergabe unter Ich → Geräte", "n", n)
		return
	}
	if mode == "transcode" {
		if voll, n := s.umwandlungVoll(r.Context()); voll {
			failW(w, http.StatusServiceUnavailable, umwandlungVollText, "n", n)
			return
		}
	}
	sess := s.live.Start(&stream.Session{
		AccountID: acc.ID, Name: acc.Name, Avatar: acc.Avatar,
		DeviceName: dev.Name, DeviceKind: dev.Kind, Geraet: geraetKennung(dev), FileID: f.ID,
		Mode: mode, Reason: reason, Bitrate: plan.Bitrate,
		FromCodec: f.VideoCodec, ToCodec: zielCodec(plan, f),
		FromHeight: f.Height, ToHeight: zielHoehe(plan, f),
		Drossel: s.drosselFuer(r, lim, f, &plan),
	})
	hlsStartMu.Unlock()
	gesperrt = false

	// Text-Untertitel schon einmal leise vorbereiten.
	go s.untertitelVorab(f)

	job.SessionID = sess.ID
	s.tr.Merken(job)

	media := fmt.Sprintf("/api/v1/stream/%s/hls/%s/media.m3u8?%s",
		f.ID, sess.ID, r.URL.RawQuery)
	body := s.tr.Master(job, media, s.hlsSpurenZeilen(r.Context(), f, r.URL.RawQuery))

	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(body))
}

// zielCodec ist, was in der Verwaltung als Ziel steht: beim Kopieren das
// Bildformat der Quelle, nicht das Wort „copy".
func zielCodec(p stream.Plan, f *store.MediaFile) string {
	if p.VideoCodec == "copy" {
		return f.VideoCodec
	}
	return p.VideoCodec
}

func zielHoehe(p stream.Plan, f *store.MediaFile) int {
	if p.VideoCodec == "copy" || p.Height <= 0 || p.Height > f.Height {
		return f.Height
	}
	return p.Height
}

func (s *Server) hlsMedia(w http.ResponseWriter, r *http.Request) {
	f, _, err := s.fileForRequest(r)
	if err != nil {
		failErr(w, err)
		return
	}
	sid := r.PathValue("session")
	if !s.live.Gehoert(sid, accountOf(r).ID, f.ID) {
		s.wiedergabeVorbei(w, r, sid)
		return
	}
	j := s.gemerkterAuftrag(r, f, sid)
	s.live.Melden(sid)
	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(mitSchluessel(s.tr.Playlist(j), r.URL.Query().Get("key"))))
}

// mitSchluessel hängt den Medienschlüssel an jede Segment-Adresse der
// Liste. Apps schicken ihr Token im Kopf mit; ein Chromecast kann das
// nicht und holt die Segmente nur mit dem, was in der Liste steht.
func mitSchluessel(liste, key string) string {
	if key == "" {
		return liste
	}
	q := "key=" + url.QueryEscape(key)
	zeilen := strings.Split(liste, "\n")
	for i, z := range zeilen {
		switch {
		case z == "":
		case strings.HasPrefix(z, "#EXT-X-MAP:URI=\""):
			name := strings.TrimSuffix(strings.TrimPrefix(z, "#EXT-X-MAP:URI=\""), "\"")
			zeilen[i] = "#EXT-X-MAP:URI=\"" + anhaengen(name, q) + "\""
		case !strings.HasPrefix(z, "#"):
			zeilen[i] = anhaengen(z, q)
		}
	}
	return strings.Join(zeilen, "\n")
}

func anhaengen(adresse, q string) string {
	if strings.Contains(adresse, "?") {
		return adresse + "&" + q
	}
	return adresse + "?" + q
}

func (s *Server) hlsSegment(w http.ResponseWriter, r *http.Request) {
	f, _, err := s.fileForRequest(r)
	if err != nil {
		failErr(w, err)
		return
	}
	name := r.PathValue("segment")
	sid := r.PathValue("session")
	if !s.live.Gehoert(sid, accountOf(r).ID, f.ID) {
		s.wiedergabeVorbei(w, r, sid)
		return
	}
	j := s.gemerkterAuftrag(r, f, sid)
	// Das Gerät holt sich das nächste Stück — also schaut noch jemand zu.
	s.live.Melden(sid)

	if name == "init.mp4" && j.FMP4() {
		kopf, err := s.tr.Init(r.Context(), j)
		if err != nil {
			s.segmentFehler(w, r, sid, err)
			return
		}
		w.Header().Set("Content-Type", "video/mp4")
		w.Header().Set("Cache-Control", "private, max-age=600")
		_, _ = w.Write(kopf)
		return
	}
	endung := ".ts"
	if j.FMP4() {
		endung = ".m4s"
	}
	if !strings.HasSuffix(name, endung) {
		fail(w, http.StatusNotFound, "nicht gefunden")
		return
	}
	idx, err := stream.SegmentIndex(name)
	if err != nil {
		fail(w, http.StatusNotFound, "nicht gefunden")
		return
	}

	// Beendet der Besitzer die Wiedergabe, bricht auch das Warten auf das
	// nächste Segment sofort ab.
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	go func() {
		select {
		case <-s.live.Stopped(sid):
			cancel()
		case <-ctx.Done():
		}
	}()
	path, err := s.tr.Segment(ctx, j, idx)
	if err != nil {
		s.segmentFehler(w, r, sid, err)
		return
	}

	fh, err := os.Open(path)
	if err != nil {
		fail(w, http.StatusInternalServerError, "segment nicht lesbar")
		return
	}
	defer fh.Close()
	info, err := fh.Stat()
	if err != nil {
		fail(w, http.StatusInternalServerError, "segment nicht lesbar")
		return
	}

	if endung == ".m4s" {
		w.Header().Set("Content-Type", "video/iso.segment")
	} else {
		w.Header().Set("Content-Type", "video/mp2t")
	}
	// Segmente sind unveränderlich: einmal gerechnet, immer gleich.
	w.Header().Set("Cache-Control", "private, max-age=600")
	// Ohne „Herunterladen" mit der Drossel der Wiedergabe (drosselFuer);
	// sonst direkt — dann darf der Kern die Datei per sendfile schicken.
	// Ein Kinderprofil mit Bildschirmzeit bucht das Segment mit seiner
	// Länge: Schneller als ungefähr in Echtzeit gibt es keine Segmente,
	// und jedes zählt als Bildschirmzeit.
	if d := s.live.DrosselVon(sid); d.Kinder() {
		if err := d.Sekunden(ctx, segmentLaenge(j, idx)); err != nil {
			return
		}
		http.ServeContent(&stoppableWriter{ResponseWriter: w, ctx: ctx},
			r.WithContext(ctx), name, info.ModTime(), fh)
		return
	} else if d != nil {
		http.ServeContent(&stoppableWriter{ResponseWriter: w, ctx: ctx, drossel: d},
			r.WithContext(ctx), name, info.ModTime(), fh)
		return
	}
	http.ServeContent(w, r, name, info.ModTime(), fh)
}

// gemerkterAuftrag: der beim Master festgelegte Auftrag, sonst (nach
// einem Neustart des Servers gibt es die Wiedergabe ohnehin nicht mehr)
// aus der Anfrage neu gebaut.
func (s *Server) gemerkterAuftrag(r *http.Request, f *store.MediaFile, sid string) stream.Job {
	if j, ok := s.tr.Gemerkt(sid); ok && j.SourcePath == f.Path {
		return j
	}
	j := s.hlsAuftrag(r, f, sid)
	s.tr.Merken(j)
	return j
}

// segmentFehler übersetzt, warum ein Segment nicht kam.
func (s *Server) segmentFehler(w http.ResponseWriter, r *http.Request, sid string, err error) {
	select {
	case <-s.live.Stopped(sid):
		s.wiedergabeVorbei(w, r, sid)
		return
	default:
	}
	switch {
	case errors.Is(err, stream.ErrUeberholt):
		// Das Gerät ist inzwischen woanders; diese Anfrage hat es meist
		// schon aufgegeben. Wenn nicht, versucht es es gleich noch einmal.
		w.Header().Set("Retry-After", "1")
		streamFehler(w, http.StatusServiceUnavailable, "segment_ueberholt", "das gerät ist inzwischen woanders im film")
	case errors.Is(err, stream.ErrKeinPlatz):
		streamFehler(w, http.StatusInsufficientStorage, "kein_platz", err.Error())
	case r.Context().Err() != nil:
		// Das Gerät hat aufgegeben; niemand liest die Antwort.
	default:
		s.st.Log(r.Context(), "error", "stream", "Umwandlung gescheitert")
		s.log.Debug("umwandlung", "err", stream.Bereinigt(err.Error()))
		streamFehler(w, http.StatusInternalServerError, "umwandlung_gescheitert", "die umwandlung ist gescheitert")
	}
}

// hlsStop räumt auf, wenn das Gerät sauber aufhört — sonst läuft FFmpeg
// weiter, bis die Vorauslauf-Bremse greift.
func (s *Server) hlsStop(w http.ResponseWriter, r *http.Request) {
	sid := r.PathValue("session")
	if !s.live.Gehoert(sid, accountOf(r).ID, r.PathValue("fileId")) {
		w.WriteHeader(http.StatusNoContent) // schon vorbei, oder nicht deine
		return
	}
	s.tr.Stop(sid)
	s.live.End(sid)
	w.WriteHeader(http.StatusNoContent)
}

// umwandlungVoll: Wie viele Filme der Server gleichzeitig umwandelt, ist
// begrenzt (LUMEN_MAX_UMWANDLUNGEN). Ein NAS mit vier schwachen Kernen
// schafft in Software selten mehr als zwei, und eine dritte Umwandlung
// liesse alle drei ruckeln.
//
// Zurück kommt, wie viele schon laufen; der Text dazu ist umwandlungVollText.
func (s *Server) umwandlungVoll(ctx context.Context) (bool, int) {
	max := s.maxUmwandlungen()
	if n := s.live.Load().Transcodes; n >= max {
		return true, n
	}
	return false, 0
}

const umwandlungVollText = "der server wandelt gerade schon {n} filme um — mehr schafft er nicht gleichzeitig. " +
	"versuch es gleich nochmal, oder schau auf einem gerät, das die datei direkt abspielen kann"

// maxUmwandlungen ist die Grenze für gleichzeitige Umwandlungen. Ohne
// Angabe richtet sie sich nach dem Rechner (umwandlungenVorgabe).
func (s *Server) maxUmwandlungen() int {
	if v, err := strconv.Atoi(os.Getenv("LUMEN_MAX_UMWANDLUNGEN")); err == nil && v > 0 {
		return v
	}
	return umwandlungenVorgabe(runtime.NumCPU(), s.ff != nil && s.ff.HW.Aktiv() != "")
}

// hlsStartMu schützt den Schritt „Grenzen prüfen, Wiedergabe anmelden".
var hlsStartMu sync.Mutex

// segmentLaenge: wie viele Sekunden Film Segment idx trägt.
func segmentLaenge(j stream.Job, idx int) float64 {
	g := j.SegmentGrenzen()
	if idx >= 0 && idx+1 < len(g) {
		return g[idx+1] - g[idx]
	}
	if idx >= 0 && idx < len(g) && j.DurationMS > 0 {
		if rest := float64(j.DurationMS)/1000 - g[idx]; rest > 0 {
			return rest
		}
	}
	return stream.SegmentSeconds
}
