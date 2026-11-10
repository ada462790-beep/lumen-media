// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"lumen/internal/store"
	"lumen/internal/stream"
)

// detach löst einen Vorgang von der Anfrage: der Scan läuft weiter, auch
// wenn der Browser die Verbindung schliesst.
func detach(r *http.Request) context.Context {
	return context.WithoutCancel(r.Context())
}

// streamPlan sagt der App vorab, ob direkt abgespielt werden kann oder
// umgewandelt werden muss — so kann sie ihre Anzeige vorbereiten.
func (s *Server) streamPlan(w http.ResponseWriter, r *http.Request) {
	f, _, err := s.fileForRequest(r)
	if err != nil {
		failErr(w, err)
		return
	}
	// Ausdrücklich gewählte Ton- und Untertitelspur zählen mit; ein
	// automatisch eingebrannter Untertitel nicht — er kommt nur dazu, wenn
	// ohnehin über HLS gespielt wird.
	wahl := s.spurWahlFuer(r.Context(), f, r, false)
	plan := stream.Decide(s.dateiInfoFuer(f, wahl), deviceFromQuery(r), bitrateLimit(r, s))
	f.Path = pfadFuer(accountOf(r), f.Path)
	ok(w, map[string]any{"plan": plan, "file": f})
}

// streamFile liefert die Datei aus. Für den Anfang direkt, mit Range-
// Unterstützung — damit Vorspulen sofort funktioniert.
func (s *Server) streamFile(w http.ResponseWriter, r *http.Request) {
	acc := accountOf(r)
	dev := deviceOf(r)

	f, item, err := s.fileForRequest(r)
	if err != nil {
		failErr(w, err)
		return
	}
	if f.Missing {
		fail(w, http.StatusGone, "die datei ist nicht mehr da")
		return
	}

	lim, _ := s.st.Limits(r.Context(), acc.ID)
	wahl := s.spurWahlFuer(r.Context(), f, r, false)
	plan := stream.Decide(s.dateiInfoFuer(f, wahl), deviceFromQuery(r), bitrateLimit(r, s))

	if !plan.Direct && !lim.AllowTranscode && plan.VideoCodec != "copy" {
		fail(w, http.StatusUnsupportedMediaType,
			"dieses gerät bräuchte eine umwandlung, die für dein profil aus ist")
		return
	}
	if !plan.Direct {
		// Umgewandelt wird über HLS, nicht über diesen Endpunkt: das Gerät
		// holt sich dort eine Abspielliste und die Segmente einzeln.
		w.Header().Set("Location", fmt.Sprintf("/api/v1/stream/%s/hls/master.m3u8", f.ID))
		fail(w, http.StatusSeeOther, "dieses gerät braucht die umgewandelte fassung")
		return
	}

	// Eine Wiedergabe je Gerät und Datei: Jede Bereichsanfrage (Spulen,
	// Player, die Anfang und Ende parallel holen) gehört zu derselben —
	// früher zählte jede einzeln, und Spulen endete in „du schaust schon
	// auf 2 Geräten".
	sess, n := s.live.Beitreten(&stream.Session{
		AccountID: acc.ID, Name: acc.Name, Avatar: acc.Avatar,
		DeviceName: dev.Name, DeviceKind: dev.Kind, FileID: f.ID,
		Mode: "direct", Reason: plan.Reason, Bitrate: f.Bitrate,
		FromCodec: f.VideoCodec, ToCodec: f.VideoCodec,
		FromHeight: f.Height, ToHeight: f.Height,
		Drossel: drosselFuer(lim, f),
	}, lim.MaxStreams)
	if sess == nil {
		fail(w, http.StatusTooManyRequests,
			fmt.Sprintf("du schaust schon auf %d Geräten — in spätestens 2 Minuten ist wieder ein Platz frei, "+
				"oder beende eine Wiedergabe unter Ich → Geräte", n))
		return
	}
	schliessen, _ := s.live.Oeffnen(sess.ID)
	defer schliessen()
	_ = item

	fh, err := medienOeffnen(f.Path)
	if err != nil {
		s.st.Log(r.Context(), "error", "stream", "Eine Datei liess sich zum Abspielen nicht öffnen")
		s.log.Debug("datei öffnen", "err", stream.Bereinigt(err.Error()))
		fail(w, http.StatusInternalServerError, "datei nicht lesbar")
		return
	}
	defer fh.Close()

	info, err := fh.Stat()
	if err != nil {
		fail(w, http.StatusInternalServerError, "datei nicht lesbar")
		return
	}

	// Beendet der Besitzer die Wiedergabe, bricht die Auslieferung ab.
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	go func() {
		select {
		case <-s.live.Stopped(sess.ID):
			cancel()
		case <-ctx.Done():
		}
	}()

	w.Header().Set("Content-Type", mimeFor(f.Container))
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Cache-Control", "private, max-age=0")

	// Die Antwort läuft bewusst durch einen eigenen Schreiber: nur so merkt
	// die Auslieferung mitten im Film, dass der Besitzer sie beendet hat.
	// Ein nackter ResponseWriter würde die Datei über sendfile am Kern
	// vorbeischicken — und dann liefe sie bis zum Ende durch.
	http.ServeContent(&stoppableWriter{ResponseWriter: w, ctx: ctx, drossel: sess.Drossel,
		melder: &melder{live: s.live, id: sess.ID, letzt: time.Now()}},
		r.WithContext(ctx), filepath.Base(f.Path), info.ModTime(), fh)
}

// fileForRequest holt die Datei und prüft dabei, ob das Konto den Titel
// überhaupt sehen darf. Ohne das wäre eine Datei-Kennung ein Generalschlüssel.
func (s *Server) fileForRequest(r *http.Request) (*store.MediaFile, *store.Item, error) {
	f, err := s.st.File(r.Context(), r.PathValue("fileId"))
	if err != nil {
		return nil, nil, err
	}
	item, err := s.visibleItem(r, f.ItemID)
	if err != nil {
		return nil, nil, store.ErrNotFound
	}
	return f, item, nil
}

// deviceFromQuery liest die Fähigkeiten, die die App mitschickt. Sagt sie
// nichts, nehmen wir an, was ein heutiger Browser kann.
func deviceFromQuery(r *http.Request) stream.Device {
	q := r.URL.Query()
	// Eine leere Liste bedeutet „kann gar nichts" und wird nicht
	// stillschweigend aufgefüllt — sonst schickt der Server etwas, das
	// beim Gerät nur als schwarzes Bild ankommt.
	dev := stream.Device{
		Codecs:     splitList(q.Get("codecs"), []string{"h264"}),
		Containers: splitList(q.Get("containers"), []string{"mp4"}),
		Audio:      splitList(q.Get("audio"), []string{"aac"}),
		MaxHeight:  qInt(r, "maxHeight", 1080),
		HDR:        q.Get("hdr") == "1",
	}
	if q.Has("codecs") && strings.TrimSpace(q.Get("codecs")) == "" {
		dev.Codecs = nil
	}
	return dev
}

func bitrateLimit(r *http.Request, s *Server) int {
	// Was das Gerät selbst wünscht (etwa „Datensparen"), gilt — aber nur
	// nach unten. Die Grenze für unterwegs legt der Besitzer fest, und
	// ob eine Anfrage von unterwegs kommt, entscheidet der Server, nicht
	// die App (früher genügte „home=1" in der Adresse).
	wunsch := qInt(r, "maxBitrate", 0)
	grenze := 0
	if vonAussen(r) {
		if lim, err := s.st.Limits(r.Context(), accountOf(r).ID); err == nil {
			switch lim.RemoteQuality {
			case "1080p":
				grenze = 12_000_000
			case "720p":
				grenze = 5_000_000
			case "480p":
				grenze = 2_000_000
			}
		}
	}
	switch {
	case wunsch > 0 && (grenze == 0 || wunsch < grenze):
		return wunsch
	default:
		return grenze
	}
}

func splitList(v string, def []string) []string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return def
	}
	return out
}

func mimeFor(container string) string {
	switch strings.ToLower(container) {
	case "mp4", "m4v", "mov":
		return "video/mp4"
	case "webm":
		return "video/webm"
	case "mkv", "matroska", "matroska,webm":
		return "video/x-matroska"
	case "avi":
		return "video/x-msvideo"
	case "ts", "mpegts":
		return "video/mp2t"
	}
	return "application/octet-stream"
}

// stoppableWriter bricht beim nächsten Schreibversuch ab, sobald der
// Zusammenhang beendet ist. ServeContent schreibt in Häppchen, darum greift
// das Beenden innerhalb eines Bruchteils einer Sekunde.
//
// Nebenbei meldet er die Wiedergabe: Wer Daten abholt, schaut noch.
type stoppableWriter struct {
	http.ResponseWriter
	ctx    context.Context
	melder *melder
	// drossel: nil, ausser das Profil darf nicht herunterladen (drosselFuer).
	drossel *stream.Drossel
}

func (w *stoppableWriter) Write(p []byte) (int, error) {
	select {
	case <-w.ctx.Done():
		return 0, w.ctx.Err()
	default:
	}
	if err := w.drossel.Nehmen(w.ctx, len(p)); err != nil {
		return 0, err
	}
	if w.melder != nil {
		w.melder.melden()
	}
	return w.ResponseWriter.Write(p)
}

// drosselFuer: Darf das Profil nicht herunterladen, wird die Auslieferung
// auf etwa die doppelte Bitrate begrenzt (stream/drossel.go) — sonst wäre
// der Direktstrom ein Download-Knopf ohne Knopf. Darf es, gibt es keine
// Bremse (nil).
func drosselFuer(lim store.Limits, f *store.MediaFile) *stream.Drossel {
	if lim.AllowDownload {
		return nil
	}
	bitrate := f.Bitrate
	if bitrate <= 0 && f.DurationMS > 0 && f.SizeBytes > 0 {
		bitrate = int(f.SizeBytes * 8 * 1000 / f.DurationMS)
	}
	return stream.NeueDrossel(bitrate)
}

// Flush durchreichen, damit der Server weiter zügig ausliefert.
func (w *stoppableWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// dateiInfo ist die Grundlage für „direkt oder umwandeln": Bild, Behälter,
// Auflösung, Bitrate, HDR — und der Ton der ersten Tonspur, damit ein Film
// mit DTS auf einem Gerät ohne DTS nicht stumm läuft. Wer eine Spur
// gewählt hat, nimmt spurWahlFuer und dateiInfoFuer.
func (s *Server) dateiInfo(ctx context.Context, f *store.MediaFile) stream.FileInfoT {
	info := stream.FileInfo(f.VideoCodec, f.Container, f.Height, f.Bitrate, f.HDR)
	if spuren, err := s.st.Spuren(ctx, f.ID); err == nil {
		for _, t := range spuren {
			if t.Kind == "audio" {
				return info.MitTon(t.Codec)
			}
		}
	}
	return info
}
