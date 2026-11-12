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
	"runtime"
	"strings"
	"sync"
	"time"

	"lumen/internal/store"
	"lumen/internal/stream"
)

// Standbilder: ein Bild aus dem Film selbst, wenn es kein Poster gibt.
//
// Ohne Schlüssel für TMDb sah jede Seite gleich aus — graue Kästen mit
// einem Titel darin, und oben auf Home eine grosse leere Fläche. Ein
// Bild aus dem Film ist nicht so schön wie ein Poster, aber es zeigt,
// was das ist, und man erkennt seine Filme wieder.
//
// Erzeugt wird es beim ersten Abruf und dann aufbewahrt. Ein ganzes Raster
// ohne Poster sind aber Tausende Bilder — früher wartete jede Anfrage, bis
// ihr FFmpeg fertig war, mit normaler Priorität. Die Anfragen belegten die
// sechs Verbindungen des Browsers, und das Nachladen der Liste stand
// sekundenlang dahinter; die Wiedergabe musste sich die CPU teilen. Jetzt:
//
//   - Die Bilder entstehen in einer Warteschlange, mit niedrigster
//     Priorität (wie alles im Hintergrund), höchstens eins (auf zwei
//     Kernen) oder zwei gleichzeitig.
//   - Zuerst kommt dran, was zuletzt angefragt wurde — die Karten, die
//     gerade auf dem Bildschirm sind (nach dem Scrollen die neuen). Was
//     zugleich kam (eine Seite), der Reihe nach: oben zuerst. Was seit
//     einer Minute niemand mehr wollte (weggescrollt, Seite zu), fällt
//     heraus, bevor es anfängt.
//   - Eine Anfrage wartet nur, wenn ihr Bild gleich drankommt, und auch
//     dann höchstens kurz (standbildWarten). Sonst kommt sofort 503 mit
//     Retry-After: Die Verbindung ist frei (der Browser hat nur sechs, und
//     die Liste soll nicht hinter den Bildern warten), die Karte zeigt so
//     lange den Titel, und die Web-App fragt später noch einmal.

var (
	standbildPlaetze = make(chan struct{}, standbildParallel(runtime.NumCPU()))
	standbildMu      sync.Mutex
	// Wo es nicht ging, wird es eine Weile nicht wieder versucht. Sonst
	// liefe FFmpeg bei jeder Startseite erneut über dieselbe kaputte
	// Datei, und die Plätze wären für alle anderen besetzt.
	standbildFehlschlag = map[string]time.Time{}
	// Die Warteschlange: je Titel ein Auftrag.
	standbildAuftraege = map[string]*standbildAuftrag{}
	standbildArbeiter  int
)

const (
	standbildPause = 6 * time.Hour
	// So lange wartet eine Anfrage höchstens auf ihr Bild.
	standbildWarten = 2 * time.Second
	// Was so lange niemand mehr wollte, wird nicht mehr angefangen.
	standbildVergessen = 60 * time.Second
)

// standbildParallel: wie viele Standbilder gleichzeitig entstehen dürfen —
// zwei, auf einem einzigen Kern eins. Mehr nicht: Sie laufen mit
// niedrigster Priorität und weichen der Wiedergabe, aber die Platte teilen
// sie sich trotzdem.
func standbildParallel(kerne int) int {
	if kerne <= 1 {
		return 1
	}
	return 2
}

type standbildAuftrag struct {
	it store.Item
	// erst: die erste Anfrage (bestimmt die Reihenfolge); gewollt: die
	// letzte (zeigt, dass noch jemand wartet).
	erst    time.Time
	gewollt time.Time
	laeuft  bool
	fertig  chan struct{}
	pfad    string
	err     error
}

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

// standbildZiel: wo das Bild eines Titels liegt.
func (s *Server) standbildZiel(it *store.Item) (string, error) {
	if strings.ContainsAny(it.ID, `/\.`) {
		return "", errors.New("ungültige kennung")
	}
	return filepath.Join(s.standbildOrdner(), it.ID+".jpg"), nil
}

// standbildDa: Liegt schon ein passendes Bild da? Neben dem Bild steht,
// aus welcher Fassung welcher Datei es stammt (dateiKennung). Hat ein
// neuer Durchlauf die Datei umgehängt oder wurde sie am selben Pfad
// ersetzt, ist es das Bild von etwas anderem. Ältere Fassungen von Lumen
// vermerkten nur die Kennung der Datei; das gilt weiter. Ohne Vermerk oder
// ohne Datei bleibt das Bild stehen.
func (s *Server) standbildDa(ctx context.Context, it *store.Item) (string, bool) {
	ziel, err := s.standbildZiel(it)
	if err != nil {
		return "", false
	}
	if _, err := os.Stat(ziel); err != nil {
		return "", false
	}
	vermerk, verr := os.ReadFile(ziel + ".quelle")
	jetzt, derr := s.standbildDatei(ctx, it)
	if verr != nil || derr != nil || string(vermerk) == dateiKennung(jetzt) || string(vermerk) == jetzt.ID {
		return ziel, true
	}
	_ = os.Remove(ziel)
	return "", false
}

// standbildAnfordern stellt den Titel in die Warteschlange (oder sagt
// einem wartenden Auftrag, dass er noch gewollt ist). bald: Er läuft schon
// oder kommt als Nächstes dran — dann lohnt sich kurzes Warten.
func (s *Server) standbildAnfordern(it *store.Item) (a *standbildAuftrag, bald bool) {
	standbildMu.Lock()
	defer standbildMu.Unlock()
	a, ok := standbildAuftraege[it.ID]
	if !ok {
		a = &standbildAuftrag{it: *it, erst: time.Now(), fertig: make(chan struct{})}
		standbildAuftraege[it.ID] = a
	}
	a.gewollt = time.Now()
	if standbildArbeiter < cap(standbildPlaetze) {
		standbildArbeiter++
		go s.standbildArbeiten()
	}
	if a.laeuft {
		return a, true
	}
	// Wie viele kommen vorher dran?
	vorher := 0
	for _, x := range standbildAuftraege {
		if !x.laeuft && x != a && standbildVor(x, a) {
			vorher++
		}
	}
	return a, vorher < cap(standbildPlaetze)
}

// standbildWelle: Anfragen innerhalb von zwei Sekunden gehören zu einer
// Seite.
const standbildWelle = 2 * time.Second

// standbildVor: Kommt x vor y dran? Die neuere Welle zuerst, in einer
// Welle die frühere Anfrage (oben auf der Seite).
func standbildVor(x, y *standbildAuftrag) bool {
	wx, wy := x.erst.Truncate(standbildWelle), y.erst.Truncate(standbildWelle)
	if !wx.Equal(wy) {
		return wx.After(wy)
	}
	return x.erst.Before(y.erst)
}

// standbildArbeiten nimmt Aufträge, bis keiner mehr da ist — zuletzt
// gewollte zuerst.
func (s *Server) standbildArbeiten() {
	for {
		standbildMu.Lock()
		var a *standbildAuftrag
		for id, x := range standbildAuftraege {
			if x.laeuft {
				continue
			}
			if time.Since(x.gewollt) > standbildVergessen {
				// Niemand wartet mehr darauf.
				x.err = errors.New("kein standbild")
				close(x.fertig)
				delete(standbildAuftraege, id)
				continue
			}
			if a == nil || standbildVor(x, a) {
				a = x
			}
		}
		if a == nil {
			standbildArbeiter--
			standbildMu.Unlock()
			return
		}
		a.laeuft = true
		standbildMu.Unlock()

		standbildPlaetze <- struct{}{}
		a.pfad, a.err = s.standbildMachen(&a.it)
		<-standbildPlaetze

		standbildMu.Lock()
		delete(standbildAuftraege, a.it.ID)
		close(a.fertig)
		standbildMu.Unlock()
	}
}

// standbild gibt das Bild eines Titels, wenn es bis zur Frist da ist.
// warte: ob es (noch) entsteht — dann später noch einmal fragen.
func (s *Server) standbild(ctx context.Context, it *store.Item) (pfad string, warte bool, err error) {
	if s.ff == nil || !s.ff.Available() {
		return "", false, errors.New("kein ffmpeg")
	}
	if _, err := s.standbildZiel(it); err != nil {
		return "", false, err
	}
	if p, ok := s.standbildDa(ctx, it); ok {
		return p, false, nil
	}
	standbildMu.Lock()
	wann, gescheitert := standbildFehlschlag[it.ID]
	standbildMu.Unlock()
	if gescheitert && time.Since(wann) < standbildPause {
		return "", false, errors.New("kein standbild")
	}
	a, bald := s.standbildAnfordern(it)
	if !bald {
		return "", true, errors.New("kein standbild")
	}
	frist := time.NewTimer(standbildWarten)
	defer frist.Stop()
	select {
	case <-a.fertig:
		if a.err != nil {
			standbildMu.Lock()
			_, dauerhaft := standbildFehlschlag[it.ID]
			standbildMu.Unlock()
			return "", !dauerhaft, a.err
		}
		return a.pfad, false, nil
	case <-frist.C:
		return "", true, errors.New("kein standbild")
	case <-ctx.Done():
		return "", false, ctx.Err()
	}
}

// standbildMachen rechnet das Bild (in der Warteschlange).
func (s *Server) standbildMachen(it *store.Item) (string, error) {
	ctx := context.Background()
	ziel, err := s.standbildZiel(it)
	if err != nil {
		return "", err
	}
	// Inzwischen da (eine zweite Anfrage war schneller)?
	if p, ok := s.standbildDa(ctx, it); ok {
		return p, nil
	}
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
	if err := os.MkdirAll(s.standbildOrdner(), 0o755); err != nil {
		return "", err
	}
	tmp := ziel + ".tmp.jpg"
	cctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	// Nur Schlüsselbilder dekodieren, ohne Ton und Untertitel: Auf einem NAS
	// mit Festplatten ist das um ein Vielfaches schneller. Niedrige
	// Priorität: Abspielen geht vor, das Bild darf warten (die Karte zeigt
	// so lange den Titel).
	machen := func(stelle string, nurSchluessel bool) error {
		args := []string{"-nostdin", "-loglevel", "error"}
		if nurSchluessel {
			args = append(args, "-skip_frame", "nokey")
		}
		args = append(args, "-ss", stelle, "-i", datei.Path, "-an", "-sn", "-dn",
			"-frames:v", "1", "-vf", "scale='min(1280,iw)':-2", "-q:v", "4", "-y", tmp)
		cmd := exec.CommandContext(cctx, s.ff.FFmpegPath, args...)
		if out, err := stream.Niedrig(cmd, true); err != nil {
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
	_ = os.WriteFile(ziel+".quelle", []byte(dateiKennung(datei)), 0o644)
	return ziel, nil
}

// standbildSenden liefert das Bild aus — 503, solange es noch entsteht
// (die Web-App fragt dann später noch einmal), oder 404, dann bleibt in
// der App der Titel stehen, wie bisher.
func (s *Server) standbildSenden(w http.ResponseWriter, r *http.Request, it *store.Item) {
	pfad, warte, err := s.standbild(r.Context(), it)
	if err != nil {
		if warte {
			w.Header().Set("Retry-After", "2")
			w.Header().Set("Cache-Control", "no-store")
			fail(w, http.StatusServiceUnavailable, "kein bild")
			return
		}
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
	vermerk, _ := os.ReadFile(pfad + ".quelle")
	bildAufbewahren(w, it.ID+":"+string(vermerk), info)
	w.Header().Set("Content-Type", "image/jpeg")
	http.ServeContent(w, r, "standbild.jpg", info.ModTime(), f)
}
