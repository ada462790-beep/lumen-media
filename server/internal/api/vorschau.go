// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

// Die bewegte Vorschau: zwölf Sekunden aus dem Film, klein und leise,
// für die Maus über einer Karte und den Hero der Startseite. Erzeugt
// beim ersten Abruf, dann aufbewahrt — wie das Standbild, mit denselben
// zwei Plätzen für FFmpeg, damit die Startseite den Rechner nicht
// lahmlegt. Rund eine Million Byte je Film.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"lumen/internal/store"
	"lumen/internal/stream"
)

const vorschauSekunden = 12

var (
	vorschauMu         sync.Mutex
	vorschauLaeuft     = map[string]*sync.WaitGroup{}
	vorschauFehlschlag = map[string]time.Time{}
)

func (s *Server) vorschauOrdner() string {
	return filepath.Join(s.dataDir, "vorschau")
}

// webm: für Browser ohne H.264 (manche Linux-Ausgaben) kommt VP9 und
// Opus — dieselben zwölf Sekunden, nur anders verpackt.
func (s *Server) vorschau(ctx context.Context, it *store.Item, webm bool) (string, error) {
	if s.ff == nil || !s.ff.Available() {
		return "", errors.New("kein ffmpeg")
	}
	if strings.ContainsAny(it.ID, `/\.`) {
		return "", errors.New("ungültige kennung")
	}
	endung := ".mp4"
	if webm {
		endung = ".webm"
	}
	ziel := filepath.Join(s.vorschauOrdner(), it.ID+endung)
	if _, err := os.Stat(ziel); err == nil {
		return ziel, nil
	}
	schluessel := it.ID + endung
	vorschauMu.Lock()
	if wann, ok := vorschauFehlschlag[schluessel]; ok && time.Since(wann) < standbildPause {
		vorschauMu.Unlock()
		return "", errors.New("keine vorschau")
	}
	if wg, laeuft := vorschauLaeuft[schluessel]; laeuft {
		vorschauMu.Unlock()
		wg.Wait()
		if _, err := os.Stat(ziel); err == nil {
			return ziel, nil
		}
		return "", errors.New("keine vorschau")
	}
	wg := &sync.WaitGroup{}
	wg.Add(1)
	vorschauLaeuft[schluessel] = wg
	vorschauMu.Unlock()
	defer func() {
		vorschauMu.Lock()
		delete(vorschauLaeuft, schluessel)
		vorschauMu.Unlock()
		wg.Done()
	}()
	fehlschlag := func(err error) (string, error) {
		vorschauMu.Lock()
		vorschauFehlschlag[schluessel] = time.Now()
		vorschauMu.Unlock()
		return "", err
	}

	datei, err := s.standbildDatei(ctx, it)
	if err != nil {
		return fehlschlag(err)
	}
	select {
	case standbildPlaetze <- struct{}{}:
		defer func() { <-standbildPlaetze }()
	case <-ctx.Done():
		return "", ctx.Err()
	}
	if err := os.MkdirAll(s.vorschauOrdner(), 0o755); err != nil {
		return "", err
	}
	// Die Stelle: ein Drittel hinein, dann ist der Vorspann vorbei und
	// die Handlung läuft; bei kurzen Dateien nahe am Anfang.
	stelle := float64(datei.DurationMS) / 1000 / 3
	if datei.DurationMS <= 0 {
		stelle = 60
	}
	if stelle > 1800 {
		stelle = 1800
	}
	tmp := ziel + ".tmp" + endung
	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 120*time.Second)
	defer cancel()
	args := []string{"-nostdin", "-loglevel", "error",
		"-ss", fmt.Sprintf("%.2f", stelle),
		"-i", datei.Path,
		"-t", fmt.Sprintf("%d", vorschauSekunden),
		"-map", "0:v:0", "-map", "0:a:0?",
		"-vf", "scale='min(640,iw)':-2:flags=bicubic"}
	if webm {
		args = append(args, "-c:v", "libvpx-vp9", "-deadline", "realtime", "-cpu-used", "8", "-b:v", "500k", "-pix_fmt", "yuv420p",
			"-c:a", "libopus", "-b:a", "64k", "-ac", "2")
	} else {
		args = append(args, "-c:v", "libx264", "-preset", "veryfast", "-crf", "27", "-profile:v", "main", "-pix_fmt", "yuv420p",
			"-c:a", "aac", "-b:a", "64k", "-ac", "2", "-movflags", "+faststart")
	}
	args = append(args, "-y", tmp)
	cmd := exec.CommandContext(cctx, s.ff.FFmpegPath, args...)
	// Der Ton bleibt drin — stumm geschaltet wird in der Oberfläche, und
	// wer ihn hören will, schaltet ihn dort ein.
	if out, err := stream.Niedrig(cmd, true); err != nil {
		_ = os.Remove(tmp)
		return fehlschlag(fmt.Errorf("ffmpeg: %v: %s", err, strings.TrimSpace(string(out))))
	}
	if st, err := os.Stat(tmp); err != nil || st.Size() == 0 {
		_ = os.Remove(tmp)
		return fehlschlag(errors.New("leere vorschau"))
	}
	zeitlos(tmp)
	if err := os.Rename(tmp, ziel); err != nil {
		return "", err
	}
	return ziel, nil
}

// vorschauSenden liefert den Schnipsel — oder 404, dann bleibt das Bild stehen.
func (s *Server) vorschauSenden(w http.ResponseWriter, r *http.Request, it *store.Item) {
	webm := r.URL.Query().Get("f") == "webm"
	pfad, err := s.vorschau(r.Context(), it, webm)
	if err != nil {
		fail(w, http.StatusNotFound, "keine vorschau")
		return
	}
	if webm {
		w.Header().Set("Content-Type", "video/webm")
	} else {
		w.Header().Set("Content-Type", "video/mp4")
	}
	w.Header().Set("Cache-Control", "private, max-age=604800")
	http.ServeFile(w, r, pfad)
}

// zeitlos stellt die Änderungszeit eines erzeugten Bildes auf einen festen
// Tag. Standbilder und Vorschau-Clips entstehen beim ersten Ansehen einer
// Karte — ihre Zeitstempel ergäben sonst eine Liste, wann jemand welchen
// Titel angesehen hat.
func zeitlos(pfad string) {
	t := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	_ = os.Chtimes(pfad, t, t)
}
