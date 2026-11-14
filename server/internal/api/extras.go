// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

// Extras auf der Titelseite: Trailer, Featurettes, Interviews aus den
// Ordnern neben dem Film (scan.ExtrasZu). Die Datei selbst gibt es unter
// /extra/{nr}; für den Browser zusätzlich unter /extra/{nr}/browser.mp4
// umgepackt oder umgewandelt — derselbe Weg wie beim Film (stromSenden),
// nur ohne Fortschritt, Sprünge und Spurwahl: Extras sind kurz.
// Die Nummer in der Adresse ist ein Abdruck des Pfads:
// Sie verrät den Pfad nicht, und eine Anfrage kann nur Dateien aus der
// Liste dieses Films erreichen, nie eine beliebige.

import (
	"crypto/sha1"
	"encoding/hex"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"lumen/internal/scan"
	"lumen/internal/store"
	"lumen/internal/stream"
)

type extraAntwort struct {
	ID    string `json:"id"`
	Titel string `json:"titel,omitempty"`
	Art   string `json:"art"`
	URL   string `json:"url"`
}

func extraKennung(pfad string) string {
	sum := sha1.Sum([]byte(pfad))
	return hex.EncodeToString(sum[:8])
}

// extrasVon sucht die Extras eines Films (nur Filme).
func (s *Server) extrasVon(r *http.Request, it *store.Item) []scan.Extra {
	if it.Kind != "movie" {
		return nil
	}
	files, err := s.st.FilesFor(r.Context(), it.ID)
	if err != nil || len(files) == 0 {
		return nil
	}
	wurzel := s.st.BibliothekPfad(r.Context(), it.LibraryID)
	if wurzel == "" {
		return nil
	}
	pfade := make([]string, 0, len(files))
	for _, f := range files {
		pfade = append(pfade, f.Path)
	}
	return scan.ExtrasZu(pfade, wurzel)
}

// extrasZusatz legt die Extras unter „extras" in die Antwort der Titelseite.
func (s *Server) extrasZusatz(r *http.Request, it *store.Item, body map[string]any) {
	liste := s.extrasVon(r, it)
	if len(liste) == 0 {
		return
	}
	out := make([]extraAntwort, 0, len(liste))
	for _, e := range liste {
		id := extraKennung(e.Pfad)
		out = append(out, extraAntwort{ID: id, Titel: e.Titel, Art: e.Art,
			URL: "/api/v1/stream/" + it.ID + "/extra/" + id})
	}
	body["extras"] = out
}

// extraPfad: der Pfad des Extras mit dieser Nummer — nur aus der Liste
// dieses Films. "" = gibt es nicht.
func (s *Server) extraPfad(r *http.Request, it *store.Item, nr string) string {
	for _, e := range s.extrasVon(r, it) {
		if extraKennung(e.Pfad) == nr {
			return e.Pfad
		}
	}
	return ""
}

// extraStrom: GET /api/v1/stream/{item}/extra/{nr}/browser.mp4 — das
// Extra so, wie der Browser es abspielt (Parameter wie bei browser.mp4).
// Umgepackt, wenn es reicht, sonst umgewandelt — mit denselben Grenzen
// wie ein Film: Bildschirmzeit, Umwandeln erlaubt, Plätze frei.
func (s *Server) extraStrom(w http.ResponseWriter, r *http.Request) {
	acc, dev := accountOf(r), deviceOf(r)
	it, err := s.visibleItem(r, r.PathValue("item"))
	if err != nil {
		failErr(w, err)
		return
	}
	nr := r.PathValue("nr")
	pfad := s.extraPfad(r, it, nr)
	if pfad == "" {
		fail(w, http.StatusNotFound, "Dieses Extra gibt es nicht.")
		return
	}
	if s.bildschirmzeitGesperrt(w, r) {
		return
	}
	if !s.ff.Available() {
		failW(w, http.StatusServiceUnavailable, "ohne FFmpeg kann dieser server nichts für den browser vorbereiten")
		return
	}
	caps := browserDevice(r)
	if len(caps.Codecs) == 0 {
		failW(w, http.StatusNotAcceptable,
			"dieser browser bringt keinen video-decoder mit — nimm Chrome, Safari, Firefox oder Edge")
		return
	}
	probe, err := s.ff.Probe(r.Context(), pfad)
	if err != nil || probe.VideoCodec == "" {
		failW(w, http.StatusUnprocessableEntity, "die datei ist defekt und lässt sich nicht abspielen")
		return
	}
	f := &store.MediaFile{ID: "extra:" + it.ID + ":" + nr, ItemID: it.ID, Path: pfad,
		Container: probe.Container, DurationMS: probe.DurationMS, Bitrate: probe.Bitrate,
		Width: probe.Width, Height: probe.Height, VideoCodec: probe.VideoCodec, HDR: probe.HDR}
	wahl := spurWahl{tonIndex: -1}
	for _, t := range probe.Tracks {
		if t.Kind == "audio" {
			wahl.ton = &store.Track{Index: t.Index, Kind: "audio", Codec: t.Codec, Language: t.Language, Channels: t.Channels}
			break
		}
	}
	lim, _ := s.st.Limits(r.Context(), acc.ID)
	bp := s.browserPlanMit(r, f, caps, wahl)
	if bp.mode == "transcode" && !lim.AllowTranscode {
		failW(w, http.StatusForbidden, "dieses gerät bräuchte eine umwandlung, die für dein profil aus ist")
		return
	}
	s.live.ReplaceFor(acc.ID, geraetKennung(dev))
	if n := s.live.CountFor(acc.ID); n >= lim.MaxStreams {
		failW(w, http.StatusTooManyRequests,
			"du schaust schon auf {n} Geräten — in spätestens 2 Minuten ist wieder ein Platz frei, "+
				"oder beende eine Wiedergabe unter Ich → Geräte", "n", n)
		return
	}
	if bp.mode == "transcode" {
		if voll, n := s.umwandlungVoll(r.Context()); voll {
			failW(w, http.StatusServiceUnavailable, umwandlungVollText, "n", n)
			return
		}
	}
	sess := s.live.Start(&stream.Session{
		AccountID: acc.ID, Name: acc.Name, Avatar: acc.Avatar,
		DeviceName: dev.Name, DeviceKind: dev.Kind, Geraet: geraetKennung(dev), FileID: f.ID,
		Mode: bp.mode, Reason: bp.reason, Bitrate: bp.plan.Bitrate,
		FromCodec: f.VideoCodec, ToCodec: zielCodec(bp.plan, f),
		FromHeight: f.Height, ToHeight: zielHoehe(bp.plan, f),
		Drossel: s.drosselFuer(r, lim, f, &bp.plan),
	})
	endGrund := stream.GrundAbgelaufen
	defer func() { s.live.EndMit(sess.ID, endGrund) }()
	schliessen, _ := s.live.Oeffnen(sess.ID)
	defer schliessen()
	m := &melder{live: s.live, id: sess.ID, letzt: time.Now()}
	job := stream.RemuxJob{
		SourcePath: pfad, Plan: bp.plan, AudioIndex: wahl.tonIndex, WebM: bp.webm, TonKopieren: bp.tonKopieren,
		Details: bp.details, Tonemap: bp.tonemap, Einbrennen: -1,
	}
	scheitern := func(w http.ResponseWriter, status int, msg string, werte ...any) { failW(w, status, msg, werte...) }
	if s.stromSenden(w, r, job, sess, m, 0, bp.mode, scheitern) {
		endGrund = stream.GrundBeendet
	}
}

// extraAbspielen: GET /api/v1/stream/{item}/extra/{nr} — die Datei selbst,
// mit Range wie jede Videodatei.
func (s *Server) extraAbspielen(w http.ResponseWriter, r *http.Request) {
	it, err := s.visibleItem(r, r.PathValue("item"))
	if err != nil {
		failErr(w, err)
		return
	}
	nr := r.PathValue("nr")
	for _, e := range s.extrasVon(r, it) {
		if extraKennung(e.Pfad) != nr {
			continue
		}
		info, err := os.Lstat(e.Pfad)
		if err != nil || !info.Mode().IsRegular() {
			break
		}
		f, err := os.Open(e.Pfad)
		if err != nil {
			break
		}
		defer f.Close()
		typ := map[string]string{".mp4": "video/mp4", ".m4v": "video/mp4", ".mkv": "video/x-matroska",
			".webm": "video/webm", ".mov": "video/quicktime"}[strings.ToLower(filepath.Ext(e.Pfad))]
		if typ == "" {
			typ = "application/octet-stream"
		}
		w.Header().Set("Content-Type", typ)
		w.Header().Set("Cache-Control", "private, max-age=3600")
		http.ServeContent(w, r, "", info.ModTime(), f)
		return
	}
	fail(w, http.StatusNotFound, "Dieses Extra gibt es nicht.")
}
