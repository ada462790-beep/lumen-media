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
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"lumen/internal/store"
)

// Standbilder: ein Bild aus dem Film selbst, wenn es kein Poster gibt.
//
// Ohne Schlüssel für TMDb sah jede Seite gleich aus — graue Kästen mit
// einem Titel darin, und oben auf Home eine grosse leere Fläche. Ein
// Bild aus dem Film ist nicht so schön wie ein Poster, aber es zeigt,
// was das ist, und man erkennt seine Filme wieder.
//
// Erzeugt wird es beim ersten Abruf und dann aufbewahrt. Gleichzeitig
// laufen höchstens zwei FFmpeg dafür: eine Startseite fragt zwanzig
// Bilder auf einmal an, und der Rechner soll dabei noch abspielen können.

var (
	standbildPlaetze = make(chan struct{}, 2)
	standbildMu      sync.Mutex
	standbildLaeuft  = map[string]*sync.WaitGroup{}
	// Wo es nicht ging, wird es eine Weile nicht wieder versucht. Sonst
	// liefe FFmpeg bei jeder Startseite erneut über dieselbe kaputte
	// Datei, und die zwei Plätze wären für alle anderen besetzt.
	standbildFehlschlag = map[string]time.Time{}
)

const standbildPause = 6 * time.Hour

// standbildOrdner liegt neben den Daten, nicht bei den Postern: Poster
// gehören zu den Metadaten und werden mit ihnen neu geholt, Standbilder
// gehören zur Datei.
func (s *Server) standbildOrdner() string {
	return filepath.Join(s.dataDir, "standbilder")
}

// standbildDatei sucht die Datei, aus der das Bild kommt. Bei einer Serie
// oder Staffel ist das die erste Folge — sonst gäbe es nichts zum Zeigen.
func (s *Server) standbildDatei(ctx context.Context, it *store.Item) (*store.MediaFile, error) {
	ziel := it
	for tiefe := 0; tiefe < 2 && (ziel.Kind == "series" || ziel.Kind == "season"); tiefe++ {
		kinder, err := s.st.Children(ctx, ziel.ID)
		if err != nil {
			return nil, err
		}
		if len(kinder) == 0 {
			return nil, errors.New("keine folgen")
		}
		k := kinder[0]
		ziel = &k
	}
	dateien, err := s.st.FilesFor(ctx, ziel.ID)
	if err != nil {
		return nil, err
	}
	for i := range dateien {
		if !dateien[i].Missing {
			return &dateien[i], nil
		}
	}
	return nil, errors.New("keine datei")
}

// standbildStelle wählt, wo im Film das Bild genommen wird. Nicht ganz am
// Anfang — da stehen Firmenzeichen und Schwarzbilder —, sondern bei etwa
// einem Siebtel, höchstens zehn Minuten hinein.
func standbildStelle(dauerMS int64) float64 {
	if dauerMS <= 0 {
		return 30
	}
	sek := float64(dauerMS) / 1000 * 0.15
	if sek > 600 {
		sek = 600
	}
	return sek
}

func (s *Server) standbild(ctx context.Context, it *store.Item) (string, error) {
	if s.ff == nil || !s.ff.Available() {
		return "", errors.New("kein ffmpeg")
	}
	if strings.ContainsAny(it.ID, `/\.`) {
		return "", errors.New("ungültige kennung")
	}
	ziel := filepath.Join(s.standbildOrdner(), it.ID+".jpg")
	quelle := ziel + ".quelle" // aus welcher Datei das Bild stammt
	if _, err := os.Stat(ziel); err == nil {
		// Gehört die Datei inzwischen zu einem anderen Titel (ein neuer
		// Durchlauf hat sie umgehängt), ist das Bild das der alten Folge.
		// Ohne Vermerk (ältere Fassung) oder ohne Datei bleibt es stehen.
		vermerk, verr := os.ReadFile(quelle)
		jetzt, derr := s.standbildDatei(ctx, it)
		if verr != nil || derr != nil || string(vermerk) == jetzt.ID {
			return ziel, nil
		}
		_ = os.Remove(ziel)
	}
	standbildMu.Lock()
	if wann, ok := standbildFehlschlag[it.ID]; ok && time.Since(wann) < standbildPause {
		standbildMu.Unlock()
		return "", errors.New("kein standbild")
	}
	standbildMu.Unlock()

	// Fragen zwei gleichzeitig nach demselben Bild, rechnet nur einer.
	standbildMu.Lock()
	if wg, laeuft := standbildLaeuft[it.ID]; laeuft {
		standbildMu.Unlock()
		wg.Wait()
		if _, err := os.Stat(ziel); err == nil {
			return ziel, nil
		}
		return "", errors.New("kein standbild")
	}
	wg := &sync.WaitGroup{}
	wg.Add(1)
	standbildLaeuft[it.ID] = wg
	standbildMu.Unlock()
	defer func() {
		standbildMu.Lock()
		delete(standbildLaeuft, it.ID)
		standbildMu.Unlock()
		wg.Done()
	}()

	// Scheitert etwas Dauerhaftes — keine Datei, FFmpeg findet kein Bild —,
	// wird es vermerkt.
	fehlschlag := func(err error) (string, error) {
		standbildMu.Lock()
		standbildFehlschlag[it.ID] = time.Now()
		standbildMu.Unlock()
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

	if err := os.MkdirAll(s.standbildOrdner(), 0o755); err != nil {
		return "", err
	}
	tmp := ziel + ".tmp.jpg"
	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 45*time.Second)
	defer cancel()
	// Nur Schlüsselbilder dekodieren, ohne Ton und Untertitel: Auf einem NAS
	// mit Festplatten ist das um ein Vielfaches schneller. Und keine
	// niedrige Priorität — hier wartet jemand auf die Seite.
	machen := func(stelle string, nurSchluessel bool) error {
		args := []string{"-nostdin", "-loglevel", "error"}
		if nurSchluessel {
			args = append(args, "-skip_frame", "nokey")
		}
		args = append(args, "-ss", stelle, "-i", datei.Path, "-an", "-sn", "-dn",
			"-frames:v", "1", "-vf", "scale='min(1280,iw)':-2", "-q:v", "4", "-y", tmp)
		cmd := exec.CommandContext(cctx, s.ff.FFmpegPath, args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			_ = os.Remove(tmp)
			return fmt.Errorf("ffmpeg: %v: %s", err, strings.TrimSpace(string(out)))
		}
		if st, err := os.Stat(tmp); err != nil || st.Size() == 0 {
			_ = os.Remove(tmp)
			return errors.New("leeres standbild")
		}
		return nil
	}
	stelle := fmt.Sprintf("%.2f", standbildStelle(datei.DurationMS))
	err = machen(stelle, true)
	if err != nil && cctx.Err() == nil {
		// Kein Schlüsselbild ab dieser Stelle (kurze Datei, seltene
		// Schlüsselbilder): einmal alles dekodieren, dann ganz vorn.
		err = machen(stelle, false)
		if err != nil && cctx.Err() == nil {
			err = machen("0", false)
		}
	}
	if err != nil {
		if cctx.Err() != nil {
			// Zu langsam (NAS beschäftigt) ist kein dauerhafter Fehler —
			// beim nächsten Aufruf wieder versuchen, nicht erst in sechs
			// Stunden.
			return "", err
		}
		return fehlschlag(err)
	}
	zeitlos(tmp)
	if err := os.Rename(tmp, ziel); err != nil {
		return "", err
	}
	_ = os.WriteFile(quelle, []byte(datei.ID), 0o644)
	// Auch der Vermerk und der Ordner ohne echte Uhrzeit: Sonst stünde
	// daran, wann eine Karte zum ersten Mal offen war.
	zeitlos(quelle)
	zeitlos(filepath.Dir(ziel))
	return ziel, nil
}

// standbildSenden liefert das Bild aus — oder 404, dann bleibt in der
// App der Titel stehen, wie bisher.
func (s *Server) standbildSenden(w http.ResponseWriter, r *http.Request, it *store.Item) {
	pfad, err := s.standbild(r.Context(), it)
	if err != nil {
		fail(w, http.StatusNotFound, "kein bild")
		return
	}
	f, err := os.Open(pfad)
	if err != nil {
		fail(w, http.StatusNotFound, "kein bild")
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		fail(w, http.StatusNotFound, "kein bild")
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Header().Set("Content-Type", "image/jpeg")
	http.ServeContent(w, r, "standbild.jpg", info.ModTime(), f)
}
