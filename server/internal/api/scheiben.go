// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

// „In MKV umwandeln" unter Aufräumen › Ausgelassene Dateien: aus einer
// ISO-Datei oder einem DVD-/Blu-ray-Ordner wird der Hauptfilm als MKV
// daneben — ohne neu zu rechnen, mit allen Ton- und Untertitelspuren und
// den Kapiteln (internal/scheibe). Die ISO bzw. der Ordner bleibt, wie er
// ist. Ein Auftrag zur Zeit, im Hintergrund, abbrechbar.
//
//	POST   /admin/cleanup/umwandeln {"pfad": "…"}  starten
//	GET    /admin/cleanup/umwandeln                Stand
//	DELETE /admin/cleanup/umwandeln                abbrechen

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
	"syscall"
	"time"

	"lumen/internal/scan"
	"lumen/internal/scheibe"
	"lumen/internal/store"
	"lumen/internal/stream"
)

// Platz, der nach dem Umwandeln frei bleiben soll: mindestens 512 MB,
// bei grossen Filmen 2 %.
const scheibenRand = 512 << 20

func scheibenReserve(n int64) int64 { return max(scheibenRand, n/50) }

// Meldungen, wenn der Platz nicht reicht.
const (
	platzMedienText = "zu wenig platz im medienordner: gebraucht {gb} GB, frei {frei} GB"
	platzDatenText  = "zu wenig platz im datenordner von lumen: gebraucht {gb} GB, frei {frei} GB"
)

// scheibenQuelle ist, was umgewandelt wird.
type scheibenQuelle struct {
	art    string // "iso", scheibe.DVD, scheibe.BluRay
	pfad   string // die ISO-Datei oder der Filmordner (über VIDEO_TS/BDMV)
	inhalt string // VIDEO_TS- bzw. BDMV-Ordner (nicht bei ISO)
	ziel   string
	lib    store.Library
}

// scheibenLauf: der eine Auftrag und sein Stand.
type scheibenLauf struct {
	mu        sync.Mutex
	laeuft    bool
	libID     string
	pfad      string
	ziel      string
	schritt   string
	anteil    float64
	restSek   int
	ergebnis  string
	fehler    *apiFehler
	abbrechen context.CancelFunc
	beginn    time.Time
}

var scheibenAuftrag scheibenLauf

// scheibenStand ist die Antwort auf GET.
type scheibenStand struct {
	Laeuft   bool    `json:"laeuft"`
	Pfad     string  `json:"pfad,omitempty"`
	Ziel     string  `json:"ziel,omitempty"`
	Schritt  string  `json:"schritt,omitempty"` // vorbereiten, auspacken, umwandeln
	Anteil   float64 `json:"anteil"`
	RestSek  int     `json:"restSek,omitempty"`
	Ergebnis string  `json:"ergebnis,omitempty"` // fertig, fehler, abgebrochen
	Fehler   string  `json:"fehler,omitempty"`
}

func (l *scheibenLauf) setzen(f func(l *scheibenLauf)) {
	l.mu.Lock()
	defer l.mu.Unlock()
	f(l)
}

// fortschritt setzt Anteil und eine grobe Restzeit (aus dem bisherigen Tempo).
func (l *scheibenLauf) fortschritt(schritt string, anteil float64) {
	l.setzen(func(l *scheibenLauf) {
		l.schritt = schritt
		l.anteil = min(max(anteil, 0), 1)
		l.restSek = 0
		if l.anteil > 0.01 {
			vergangen := time.Since(l.beginn).Seconds()
			l.restSek = int(vergangen * (1 - l.anteil) / l.anteil)
		}
	})
}

// adminScheibeStand: GET /admin/cleanup/umwandeln.
func (s *Server) adminScheibeStand(w http.ResponseWriter, r *http.Request) {
	l := &scheibenAuftrag
	l.mu.Lock()
	st := scheibenStand{Laeuft: l.laeuft, Schritt: l.schritt, Anteil: l.anteil, RestSek: l.restSek, Ergebnis: l.ergebnis}
	if l.pfad != "" && s.darfMediathek(r, l.libID) {
		st.Pfad, st.Ziel = l.pfad, filepath.Base(l.ziel)
	}
	fehler := l.fehler
	l.mu.Unlock()
	if fehler != nil && st.Pfad != "" {
		st.Fehler = satzAnfang(tr(w, fehler.text, fehler.werte...))
	}
	ok(w, st)
}

// adminScheibeAbbrechen: DELETE /admin/cleanup/umwandeln.
func (s *Server) adminScheibeAbbrechen(w http.ResponseWriter, r *http.Request) {
	l := &scheibenAuftrag
	l.mu.Lock()
	ab, darf := l.abbrechen, l.laeuft && s.darfMediathek(r, l.libID)
	l.mu.Unlock()
	if !darf || ab == nil {
		fail(w, http.StatusNotFound, "gerade wird nichts umgewandelt")
		return
	}
	ab()
	w.WriteHeader(http.StatusNoContent)
}

// adminScheibeUmwandeln: POST /admin/cleanup/umwandeln {"pfad": "…"}.
func (s *Server) adminScheibeUmwandeln(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Pfad string `json:"pfad"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	q, err := s.scheibeFinden(r.Context(), in.Pfad)
	if err != nil {
		failErr(w, err)
		return
	}
	if !s.mediathekPruefen(w, r, q.lib.ID) {
		return
	}
	if s.ff == nil || !s.ff.Available() {
		fail(w, http.StatusServiceUnavailable, "ohne FFmpeg geht das nicht")
		return
	}
	siebenZip := ""
	if q.art == "iso" {
		if siebenZip = scheibe.SiebenZip(); siebenZip == "" {
			fail(w, http.StatusServiceUnavailable, "zum auspacken von iso-dateien fehlt 7-zip (im docker-abbild von lumen ist es dabei)")
			return
		}
	}
	if err := zielPruefen(q.ziel); err != nil {
		failErr(w, err)
		return
	}

	l := &scheibenAuftrag
	mkvAuftraege.mu.Lock()
	mkvLaeuft := mkvAuftraege.laeuft
	mkvAuftraege.mu.Unlock()
	l.mu.Lock()
	if l.laeuft || mkvLaeuft {
		l.mu.Unlock()
		fail(w, http.StatusConflict, "es läuft schon eine umwandlung – warte, bis sie fertig ist, oder brich sie ab")
		return
	}
	// Am Hintergrund-Kontext: Fährt Lumen herunter, hört auch das
	// Umwandeln auf, und das Herunterfahren wartet darauf (begleitet).
	ctx, abbrechen := context.WithCancel(s.hintergrund())
	l.laeuft, l.libID, l.pfad, l.ziel = true, q.lib.ID, q.pfad, q.ziel
	l.schritt, l.anteil, l.restSek, l.ergebnis, l.fehler = "vorbereiten", 0, 0, "", nil
	l.abbrechen, l.beginn = abbrechen, time.Now()
	l.mu.Unlock()

	s.begleitetMit(ctx, func(ctx context.Context) {
		defer abbrechen()
		// Nie neben dem Umpacken in MKV her (mkv_umpacken.go).
		schwerMu.Lock()
		info, err := s.scheibeUmwandeln(ctx, q, siebenZip)
		schwerMu.Unlock()
		l.setzen(func(l *scheibenLauf) {
			l.laeuft, l.abbrechen, l.restSek = false, nil, 0
			switch {
			case err == nil:
				l.ergebnis, l.anteil = "fertig", 1
			case errors.Is(err, context.Canceled):
				l.ergebnis = "abgebrochen"
			default:
				l.ergebnis = "fehler"
				var af *apiFehler
				if !errors.As(err, &af) {
					af = &apiFehler{status: 500, text: "umwandeln gescheitert: {grund}",
						werte: []any{"grund", stream.Bereinigt(err.Error())}}
				}
				l.fehler = af
			}
		})
		// Ins Protokoll mit Grund, aber ohne Pfad und Titel (Datenschutz) —
		// wie beim Zusammenführen.
		bg := s.hintergrund()
		switch {
		case err == nil:
			s.st.Log(bg, "info", "aufraeumen", "Hauptfilm einer Scheibe als MKV abgelegt ("+info+")")
			// Danach erkennt der Durchlauf die MKV wie jeden Film.
			s.begleitet(func(ctx context.Context) { s.Durchlauf(ctx, q.lib, true) })
		case errors.Is(err, context.Canceled):
			s.st.Log(bg, "info", "aufraeumen", "Umwandeln in MKV abgebrochen")
		default:
			grund := err.Error()
			var af *apiFehler
			if errors.As(err, &af) {
				grund = af.text
				for i := 0; i+1 < len(af.werte); i += 2 {
					grund = strings.ReplaceAll(grund, "{"+fmt.Sprint(af.werte[i])+"}", fmt.Sprint(af.werte[i+1]))
				}
			}
			s.st.Log(bg, "error", "aufraeumen", "Umwandeln in MKV gescheitert: "+stream.Bereinigt(grund))
		}
	})
	writeJSON(w, http.StatusAccepted, map[string]string{"status": tr(w, "Läuft – das kann je nach Film eine Weile dauern.")})
}

// scheibeFinden prüft den Pfad aus der Liste und macht daraus eine
// Quelle: eine ISO-Datei, ein Filmordner mit VIDEO_TS bzw. BDMV, der
// VIDEO_TS-/BDMV-Ordner selbst oder eine Datei darin.
func (s *Server) scheibeFinden(ctx context.Context, pfad string) (*scheibenQuelle, error) {
	pfad = strings.TrimSpace(pfad)
	if pfad == "" || !filepath.IsAbs(pfad) || strings.ContainsRune(pfad, 0) {
		return nil, &apiFehler{status: http.StatusBadRequest, text: "pfad fehlt"}
	}
	pfad = filepath.Clean(pfad)
	// Nur innerhalb einer Bibliothek — und nie in Lumens Datenordner.
	var lib *store.Library
	libs, _ := s.st.Libraries(ctx)
	for i := range libs {
		if rel, err := filepath.Rel(filepath.Clean(libs[i].Path), pfad); err == nil && rel != ".." &&
			!strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			if lib == nil || len(libs[i].Path) > len(lib.Path) {
				lib = &libs[i]
			}
		}
	}
	if lib == nil || scan.InDatenordner(pfad) {
		return nil, store.ErrNotFound
	}
	info, err := os.Stat(pfad)
	if err != nil {
		return nil, store.ErrNotFound
	}
	q := &scheibenQuelle{lib: *lib}
	if info.Mode().IsRegular() {
		if strings.EqualFold(filepath.Ext(pfad), ".iso") {
			q.art, q.pfad, q.ziel = "iso", pfad, scan.ScheibeZiel(pfad, false)
			return q, nil
		}
		pfad = filepath.Dir(pfad) // eine Datei in VIDEO_TS oder BDMV/STREAM
		if strings.EqualFold(filepath.Base(pfad), "STREAM") {
			pfad = filepath.Dir(pfad)
		}
	}
	// Der Ordner selbst, oder sein Unterordner VIDEO_TS / BDMV.
	switch strings.ToUpper(filepath.Base(pfad)) {
	case "VIDEO_TS":
		q.art, q.inhalt, q.pfad = scheibe.DVD, pfad, filepath.Dir(pfad)
	case "BDMV":
		q.art, q.inhalt, q.pfad = scheibe.BluRay, pfad, filepath.Dir(pfad)
	default:
		if d, ok := scheibe.Unterordner(pfad, "VIDEO_TS"); ok {
			q.art, q.inhalt, q.pfad = scheibe.DVD, d, pfad
		} else if d, ok := scheibe.Unterordner(pfad, "BDMV"); ok {
			q.art, q.inhalt, q.pfad = scheibe.BluRay, d, pfad
		}
	}
	if q.art == "" {
		return nil, &apiFehler{status: http.StatusBadRequest, text: "das ist keine iso-datei und kein dvd- oder blu-ray-ordner"}
	}
	q.ziel = scan.ScheibeZiel(q.pfad, true)
	return q, nil
}

// zielPruefen: Das Ziel darf es noch nicht geben, und Lumen muss in den
// Ordner schreiben dürfen — lieber jetzt merken als nach einer Stunde.
func zielPruefen(ziel string) error {
	if _, err := os.Lstat(ziel); err == nil {
		return &apiFehler{status: http.StatusConflict, text: "dort liegt schon eine datei „{name}“ – schon umgewandelt?",
			werte: []any{"name", filepath.Base(ziel)}}
	}
	probe, err := os.CreateTemp(filepath.Dir(ziel), ".lumen-probe-*")
	if err != nil {
		return ordnerFehler(err)
	}
	probe.Close()
	_ = os.Remove(probe.Name())
	return nil
}

func ordnerFehler(err error) error {
	if errors.Is(err, syscall.EROFS) || errors.Is(err, os.ErrPermission) {
		return &apiFehler{status: http.StatusForbidden, text: "der ordner ist schreibgeschützt – lumen darf dort keine datei anlegen (medien mit :rw einbinden oder die rechte auf dem nas prüfen)"}
	}
	return err
}

// platzPruefen: Reicht der Platz im Ordner für n Bytes und den Rand?
// text ist der Schlüssel der Meldung (Medien- oder Datenordner).
func platzPruefen(ordner string, n int64, text string) error {
	frei := freierPlatz(ordner)
	if frei == 0 { // unbekannt
		return nil
	}
	if brauche := n + scheibenReserve(n); int64(frei) < brauche {
		return &apiFehler{status: http.StatusInsufficientStorage, text: text,
			werte: []any{"gb", gbText(brauche), "frei", gbText(int64(frei))}}
	}
	return nil
}

// freierPlatz: freie Bytes im Ordner, 0 = unbekannt (in Tests ersetzbar).
var freierPlatz = scheibe.FreierPlatz

func gbText(n int64) string { return fmt.Sprintf("%.1f", float64(n)/(1<<30)) }

// scheibenArbeitsordner: Arbeitsordner für ausgepackte ISO-Dateien, unter
// Lumens Datenordner (nie im Medienordner).
func (s *Server) scheibenArbeitsordner() string {
	basis := s.dataDir
	if basis == "" {
		basis = os.TempDir()
	}
	return filepath.Join(basis, "scheiben-arbeit")
}

// scheibeUmwandeln ist der Auftrag selbst. Er räumt immer hinter sich auf:
// Arbeitsordner und halbe MKV verschwinden auch bei Fehler und Abbruch.
// Zurück kommt eine kurze Beschreibung fürs Protokoll.
func (s *Server) scheibeUmwandeln(ctx context.Context, q *scheibenQuelle, siebenZip string) (string, error) {
	l := &scheibenAuftrag
	arbeit := s.scheibenArbeitsordner()
	// Reste eines abgebrochenen Laufs (Absturz, Stromausfall) zuerst weg —
	// es läuft immer nur ein Auftrag.
	_ = os.RemoveAll(arbeit)
	defer os.RemoveAll(arbeit)
	if err := os.MkdirAll(arbeit, 0o700); err != nil {
		return "", err
	}

	var plan *scheibe.Plan
	var err error
	var auspacken int64 // Bytes, die aus der ISO kommen
	var inhalt *scheibe.IsoInhalt
	art := q.art
	switch q.art {
	case scheibe.DVD:
		dateien, e := scheibe.OrdnerLesen(q.inhalt)
		if e != nil {
			return "", e
		}
		plan, err = scheibe.PlanDVD(q.inhalt, dateien)
	case scheibe.BluRay:
		var stream []scheibe.Datei
		if d, ok := scheibe.Unterordner(q.inhalt, "STREAM"); ok {
			stream, _ = scheibe.OrdnerLesen(d)
		}
		plan, err = scheibe.PlanBluRay(q.inhalt, stream)
	case "iso":
		plan, inhalt, auspacken, err = s.isoVorbereiten(ctx, q, siebenZip, arbeit)
		if inhalt != nil {
			art = inhalt.Art
		}
	}
	if err != nil {
		if errors.Is(err, scheibe.ErrKeinFilm) {
			return "", &apiFehler{status: http.StatusBadRequest, text: "auf dieser scheibe ist kein hauptfilm zu finden"}
		}
		return "", err
	}
	gesamt := plan.Gesamt()
	if err := platzPruefen(filepath.Dir(q.ziel), gesamt, platzMedienText); err != nil {
		return "", err
	}

	// Aus der ISO nur die Dateien des Hauptfilms auspacken.
	if q.art == "iso" {
		if err := s.isoAuspacken(ctx, q, siebenZip, arbeit, inhalt, plan, auspacken, gesamt); err != nil {
			return "", err
		}
	}

	l.fortschritt("umwandeln", float64(auspacken)/float64(auspacken+gesamt))
	gefunden, err := scheibe.SpurenFinden(ctx, s.ff.FFprobePath, plan)
	if err != nil {
		return "", err
	}
	spuren := scheibe.Auswahl(plan, gefunden)
	meta, err := scheibe.KapitelDatei(plan, arbeit)
	if err != nil {
		return "", err
	}
	tmp := q.ziel + ".teil"
	defer os.Remove(tmp) // nach dem Umbenennen gibt es sie nicht mehr
	ctxZeit, ende := context.WithTimeout(ctx, 6*time.Hour)
	defer ende()
	err = scheibe.Umwandeln(ctxZeit, s.ff.FFmpegPath, plan, spuren, meta, tmp,
		func(c *exec.Cmd) ([]byte, error) { return stream.Niedrig(c, true) },
		func(n int64) { l.fortschritt("umwandeln", float64(auspacken+n)/float64(auspacken+gesamt)) })
	if err != nil {
		if ctx.Err() != nil {
			return "", context.Canceled
		}
		if pe := (*os.PathError)(nil); errors.As(err, &pe) {
			return "", ordnerFehler(err)
		}
		return "", err
	}
	if info, e := os.Stat(tmp); e != nil || info.Size() == 0 {
		return "", errors.New("ffmpeg hat keine datei geschrieben")
	}
	// Nicht überschreiben, falls inzwischen jemand eine Datei dorthin gelegt hat.
	if _, e := os.Lstat(q.ziel); e == nil {
		return "", &apiFehler{status: http.StatusConflict, text: "dort liegt schon eine datei „{name}“ – schon umgewandelt?",
			werte: []any{"name", filepath.Base(q.ziel)}}
	}
	if err := os.Rename(tmp, q.ziel); err != nil {
		return "", err
	}
	if q.inhalt != "" {
		s.scheibeEintragen(ctx, q)
	}
	was := "DVD"
	if art == scheibe.BluRay {
		was = "Blu-ray"
	}
	if q.art == "iso" {
		was += " aus ISO"
	}
	ton, unter := 0, 0
	for _, sp := range spuren {
		switch sp.Art {
		case "a":
			ton++
		case "s":
			unter++
		}
	}
	return fmt.Sprintf("%s, %d Min., %d Tonspuren, %d Untertitel, %d Kapitel",
		was, (plan.DauerMS+30000)/60000, ton, unter, len(plan.Kapitel)), nil
}

// scheibeEintragen hängt die neue MKV gleich an den Film der Scheibe und
// trägt alle Dateien der Scheibe aus (VTS_01_1..3.VOB, nicht nur Teil 1):
// Hier steht sicher fest, dass die MKV sie ersetzt. Vorher blieben Teil 2
// und 3 für immer als „fehlend" am Film stehen. Gleich ausgemessen wie
// beim Zusammenführen der Teile.
func (s *Server) scheibeEintragen(ctx context.Context, q *scheibenQuelle) {
	info, err := os.Stat(q.ziel)
	if err != nil {
		return
	}
	neu := &store.MediaFile{Path: q.ziel, SizeBytes: info.Size(), MTime: info.ModTime().Unix(),
		PartNo: 1, Container: "mkv"}
	if s.ff != nil && s.ff.Available() {
		mctx, mab := context.WithTimeout(ctx, 30*time.Second)
		if p, err := s.ff.Probe(mctx, q.ziel); err == nil {
			neu.DurationMS, neu.Bitrate = p.DurationMS, p.Bitrate
			neu.Width, neu.Height, neu.VideoCodec, neu.HDR = p.Width, p.Height, p.VideoCodec, p.HDR
			neu.Spuren = scan.SpurenFuerStore(append(p.Tracks, scan.SidecarSubtitles(q.ziel)...))
			neu.Kapitel = scan.KapitelFuerStore(p.Chapters)
		}
		mab()
	}
	if _, err := s.st.ScheibeErsetzen(ctx, q.lib.ID, q.inhalt, neu); err != nil {
		s.st.Log(ctx, "warn", "aufraeumen", "Dateien der Scheibe liessen sich nicht austragen: "+stream.Bereinigt(err.Error()))
	}
}

// isoVorbereiten listet die ISO, packt die Steuerdateien aus und plant.
func (s *Server) isoVorbereiten(ctx context.Context, q *scheibenQuelle, siebenZip, arbeit string) (*scheibe.Plan, *scheibe.IsoInhalt, int64, error) {
	inhalt, err := scheibe.IsoLesen(ctx, siebenZip, q.pfad)
	if err != nil {
		if ctx.Err() != nil {
			return nil, nil, 0, context.Canceled
		}
		return nil, nil, 0, &apiFehler{status: http.StatusBadRequest, text: "die iso-datei lässt sich nicht lesen: {grund}",
			werte: []any{"grund", stream.Bereinigt(err.Error())}}
	}
	if err := scheibe.Auspacken(ctx, siebenZip, q.pfad, arbeit, inhalt.Steuerdateien()); err != nil {
		return nil, nil, 0, err
	}
	var plan *scheibe.Plan
	if inhalt.Art == scheibe.DVD {
		dir, ok := scheibe.Unterordner(arbeit, "VIDEO_TS")
		if !ok {
			dir = filepath.Join(arbeit, "VIDEO_TS")
		}
		plan, err = scheibe.PlanDVD(dir, inhalt.Ordner("VIDEO_TS"))
	} else {
		dir, ok := scheibe.Unterordner(arbeit, "BDMV")
		if !ok {
			dir = filepath.Join(arbeit, "BDMV")
		}
		plan, err = scheibe.PlanBluRay(dir, inhalt.Ordner("BDMV/STREAM"))
	}
	if err != nil {
		return nil, nil, 0, err
	}
	var n int64
	for _, d := range plan.Dateien() {
		for _, e := range inhalt.Ordner(isoUnterordner(inhalt.Art)) {
			if e.Name == d {
				n += e.Groesse
			}
		}
	}
	return plan, inhalt, n, nil
}

func isoUnterordner(art string) string {
	if art == scheibe.DVD {
		return "VIDEO_TS"
	}
	return "BDMV/STREAM"
}

// isoAuspacken packt die Dateien des Hauptfilms aus — nach der
// Platzprüfung im Arbeitsordner — und meldet den Fortschritt an der Grösse
// der ausgepackten Dateien.
func (s *Server) isoAuspacken(ctx context.Context, q *scheibenQuelle, siebenZip, arbeit string,
	inhalt *scheibe.IsoInhalt, plan *scheibe.Plan, n, gesamt int64) error {
	art := inhalt.Art
	if err := platzPruefen(arbeit, n, platzDatenText); err != nil {
		return err
	}
	var pfade []string
	for _, d := range plan.Dateien() {
		pfade = append(pfade, inhalt.Pfad(isoUnterordner(art), d))
	}
	l := &scheibenAuftrag
	l.fortschritt("auspacken", 0)
	fertig := make(chan struct{})
	go func() {
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for {
			select {
			case <-fertig:
				return
			case <-t.C:
				var da int64
				for _, d := range plan.Dateien() {
					if info, err := os.Stat(filepath.Join(plan.Ordner, d)); err == nil {
						da += info.Size()
					}
				}
				l.fortschritt("auspacken", float64(min(da, n))/float64(n+gesamt))
			}
		}
	}()
	err := scheibe.Auspacken(ctx, siebenZip, q.pfad, arbeit, pfade)
	close(fertig)
	if err != nil {
		if ctx.Err() != nil {
			return context.Canceled
		}
		return err
	}
	// Die Datenströme liegen jetzt im Arbeitsordner (VIDEO_TS bzw.
	// BDMV/STREAM, in der Schreibweise des Abbilds).
	if art == scheibe.BluRay {
		if d, ok := scheibe.Unterordner(filepath.Dir(plan.Ordner), "STREAM"); ok {
			plan.Ordner = d
		}
	}
	for _, d := range plan.Dateien() {
		if _, err := os.Stat(filepath.Join(plan.Ordner, d)); err != nil {
			return fmt.Errorf("7-zip hat %s nicht ausgepackt", d)
		}
	}
	return nil
}

// isoUmgewandelt: die ISO-Dateien unter pfade, neben denen die MKV schon liegt.
func isoUmgewandelt(pfade []string) []string {
	var out []string
	for _, p := range pfade {
		if !strings.EqualFold(filepath.Ext(p), ".iso") {
			continue
		}
		if info, err := os.Stat(scan.ScheibeZiel(p, false)); err == nil && info.Mode().IsRegular() {
			out = append(out, p)
		}
	}
	return out
}
