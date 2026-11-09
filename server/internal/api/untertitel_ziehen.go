// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

// Eingebettete Text-Untertitel herausziehen.
//
// Untertitel stecken in einer MKV verstreut über die ganze Datei. Um eine
// Spur als WebVTT zu bekommen, muss FFmpeg die Datei einmal ganz lesen —
// bei einem 60-GB-Remux auf einem NAS dauert das Minuten. Früher geschah
// das in der Anfrage, mit einer Grenze von zwei Minuten: Bei grossen
// Dateien kamen die Untertitel nie.
//
// Jetzt:
//   - Bild und Ton werden schon beim Lesen verworfen (-discard), nur die
//     Untertitel gehen durch FFmpeg;
//   - alle Text-Spuren einer Datei in einem Durchgang — wer umschaltet,
//     wartet nicht noch einmal;
//   - es läuft im Hintergrund, ohne Zeitgrenze der Anfrage. Die Anfrage
//     wartet ein Weilchen; ist es dann nicht fertig, antwortet sie mit 202
//     und dem Fortschritt, und das Gerät fragt wieder;
//   - meist liegt die Spur ohnehin schon fertig da (hintergrund.go).

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"lumen/internal/store"
	"lumen/internal/stream"
)

// textUntertitelZiehen zieht die genannten Spuren in einem Durchgang.
// niedrig: mit niedrigster Priorität (Vorbereitung im Hintergrund).
// fortschritt bekommt Werte zwischen 0 und 1 (darf nil sein).
func (s *Server) textUntertitelZiehen(ctx context.Context, f *store.MediaFile, spuren []store.Track,
	niedrig bool, fortschritt func(float64)) (map[int][]byte, error) {
	if s.ff == nil || !s.ff.Available() {
		return nil, fmt.Errorf("ohne FFmpeg nur .srt-Dateien")
	}
	if len(spuren) == 0 {
		return map[int][]byte{}, nil
	}
	tmp, err := os.MkdirTemp(s.dataDir, "vtt-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)

	ctx, cancel := context.WithTimeout(ctx, 45*time.Minute)
	defer cancel()
	args := []string{"-hide_banner", "-loglevel", "error", "-nostdin", "-y",
		"-discard:v", "all", "-discard:a", "all", "-i", f.Path}
	for _, t := range spuren {
		args = append(args, "-map", fmt.Sprintf("0:%d", t.Index), "-c:s", "webvtt", "-f", "webvtt",
			filepath.Join(tmp, strconv.Itoa(t.Index)+".vtt"))
	}
	if niedrig {
		cmd := exec.CommandContext(ctx, s.ff.FFmpegPath, args...)
		if out, err := stream.Niedrig(cmd, true); err != nil {
			return nil, fmt.Errorf("ffmpeg: %s", strings.TrimSpace(string(out)))
		}
	} else {
		args = append(args, "-progress", "pipe:1", "-nostats")
		cmd := exec.CommandContext(ctx, s.ff.FFmpegPath, args...)
		var fehl strings.Builder
		cmd.Stderr = &fehl
		aus, err := cmd.StdoutPipe()
		if err != nil {
			return nil, err
		}
		if err := cmd.Start(); err != nil {
			return nil, err
		}
		dauer := float64(f.DurationMS) * 1000 // Mikrosekunden
		sc := bufio.NewScanner(aus)
		for sc.Scan() {
			z := sc.Text()
			if v, ok := strings.CutPrefix(z, "out_time_us="); ok && fortschritt != nil && dauer > 0 {
				if us, err := strconv.ParseFloat(v, 64); err == nil && us > 0 {
					fortschritt(min(us/dauer, 0.99))
				}
			}
		}
		if err := cmd.Wait(); err != nil {
			return nil, fmt.Errorf("ffmpeg: %s", strings.TrimSpace(fehl.String()))
		}
	}
	out := map[int][]byte{}
	for _, t := range spuren {
		b, err := os.ReadFile(filepath.Join(tmp, strconv.Itoa(t.Index)+".vtt"))
		if err != nil {
			return nil, err
		}
		out[t.Index] = b
	}
	return out, nil
}

// vttAuftrag ist ein laufendes Herausziehen für eine Datei.
type vttAuftrag struct {
	fertig chan struct{}
	mu     sync.Mutex
	anteil float64
	err    error
}

func (a *vttAuftrag) stand() float64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.anteil
}

var (
	vttAuftraegeMu sync.Mutex
	vttAuftraege   = map[string]*vttAuftrag{}
)

// vttAuftragFuer startet (oder findet) das Herausziehen aller eingebetteten
// Text-Untertitel einer Datei in den Arbeitsspeicher. niedrig: als
// Vorbereitung, wenn eine Wiedergabe beginnt — dann soll es ihr nicht in
// die Quere kommen.
func (s *Server) vttAuftragFuer(f *store.MediaFile, niedrig bool) *vttAuftrag {
	vttAuftraegeMu.Lock()
	defer vttAuftraegeMu.Unlock()
	if a, ok := vttAuftraege[f.ID]; ok {
		return a
	}
	a := &vttAuftrag{fertig: make(chan struct{})}
	vttAuftraege[f.ID] = a
	datei := *f
	go func() {
		defer func() {
			close(a.fertig)
			// Ein gescheiterter Auftrag bleibt eine Weile stehen (sonst
			// läuft jede Anfrage wieder in denselben Fehler), ein
			// gelungener wird gleich vergessen — sein Ergebnis liegt im
			// Zwischenspeicher.
			warten := time.Duration(0)
			if a.err != nil {
				warten = 10 * time.Minute
			}
			time.AfterFunc(warten, func() {
				vttAuftraegeMu.Lock()
				if vttAuftraege[datei.ID] == a {
					delete(vttAuftraege, datei.ID)
				}
				vttAuftraegeMu.Unlock()
			})
		}()
		ctx := context.Background()
		spuren, err := s.st.Spuren(ctx, datei.ID)
		if err != nil {
			a.err = err
			return
		}
		var text []store.Track
		for _, t := range spuren {
			if t.Kind == "subtitle" && !t.Bitmap && !t.External {
				if _, da := vttZwischen.holen(fmt.Sprintf("%s-%d", datei.ID, t.Index)); !da {
					text = append(text, t)
				}
			}
		}
		inhalte, err := s.textUntertitelZiehen(ctx, &datei, text, niedrig, func(v float64) {
			a.mu.Lock()
			a.anteil = v
			a.mu.Unlock()
		})
		if err != nil {
			a.err = err
			return
		}
		for idx, b := range inhalte {
			vttZwischen.ablegen(fmt.Sprintf("%s-%d", datei.ID, idx), b)
		}
		a.mu.Lock()
		a.anteil = 1
		a.mu.Unlock()
	}()
	return a
}

// untertitelVorab: Beginnt eine Wiedergabe über HLS, werden die
// eingebetteten Text-Untertitel schon einmal gezogen — leise im
// Hintergrund. Wählt jemand dann eine Spur, ist sie meist schon da.
func (s *Server) untertitelVorab(f *store.MediaFile) {
	if s.ff == nil || !s.ff.Available() {
		return
	}
	spuren, err := s.st.Spuren(context.Background(), f.ID)
	if err != nil {
		return
	}
	for _, t := range spuren {
		if t.Kind == "subtitle" && !t.Bitmap && !t.External {
			if _, err := os.Stat(s.vorratPfad(f.ID, t.Index)); err == nil {
				continue
			}
			if _, da := vttZwischen.holen(fmt.Sprintf("%s-%d", f.ID, t.Index)); da {
				continue
			}
			s.vttAuftragFuer(f, true)
			return
		}
	}
}

// eingebetteteUntertitel liefert eine eingebettete Text-Spur: vom Vorrat
// auf der Platte, aus dem Arbeitsspeicher oder frisch gezogen. Ist sie
// nach warten nicht fertig, antwortet sie selbst (202 bzw. 503) und gibt
// nil zurück.
func (s *Server) eingebetteteUntertitel(w http.ResponseWriter, r *http.Request, f *store.MediaFile, idx int) []byte {
	if b, err := os.ReadFile(s.vorratPfad(f.ID, idx)); err == nil {
		return b
	}
	schluessel := fmt.Sprintf("%s-%d", f.ID, idx)
	if b, da := vttZwischen.holen(schluessel); da {
		return b
	}
	// Der Player von Apple fragt für HLS-Untertitel nicht nach, wenn er
	// 202 bekommt — er liest die Antwort als Untertitel. Darum wartet die
	// Liste für HLS länger (warten=55) und bekommt am Ende 503.
	warten := 20 * time.Second
	hls := false
	if v := qInt(r, "warten", 0); v > 0 {
		warten, hls = time.Duration(min(v, 55))*time.Second, true
	}
	a := s.vttAuftragFuer(f, false)
	select {
	case <-a.fertig:
	case <-time.After(warten):
	case <-r.Context().Done():
		return nil
	}
	select {
	case <-a.fertig:
		if a.err != nil {
			s.log.Warn("untertitel", "err", stream.Bereinigt(a.err.Error()))
			streamFehler(w, http.StatusUnprocessableEntity, "untertitel_unlesbar", "diese untertitel lassen sich nicht lesen")
			return nil
		}
		if b, da := vttZwischen.holen(schluessel); da {
			return b
		}
		// Zu gross für den Zwischenspeicher oder gerade verdrängt: noch
		// einmal, diesmal nur diese Spur.
		t, err := s.st.Spur(r.Context(), f.ID, idx)
		if err != nil {
			fail(w, http.StatusNotFound, "diese untertitelspur gibt es nicht")
			return nil
		}
		inhalte, err := s.textUntertitelZiehen(r.Context(), f, []store.Track{*t}, false, nil)
		if err != nil {
			streamFehler(w, http.StatusUnprocessableEntity, "untertitel_unlesbar", "diese untertitel lassen sich nicht lesen")
			return nil
		}
		return inhalte[idx]
	default:
	}
	w.Header().Set("Retry-After", "3")
	w.Header().Set("Cache-Control", "no-store")
	status := http.StatusAccepted
	if hls {
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, map[string]any{"code": "wird_vorbereitet",
		"fortschritt": float64(int(a.stand()*100)) / 100})
	return nil
}
