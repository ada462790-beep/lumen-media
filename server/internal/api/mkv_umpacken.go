// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

// „In MKV umwandeln" für Filme und Folgen in älteren Behältern (AVI, WMV,
// MPG, VOB, FLV, MOV, MP4, OGM, 3GP, TS …) unter Aufräumen › Ältere
// Dateiformate und im Menü „Verwaltung" eines Titels. Gleiche Bauweise wie
// das Umwandeln von Scheiben (scheiben.go): ein Auftrag zur Zeit, im
// Hintergrund, mit niedriger Priorität, abbrechbar — hier mit einer
// Warteschlange, damit „Alle umwandeln" eine Datei nach der anderen
// abarbeitet.
//
//	GET    /admin/cleanup/mkv[?itemId=…]   Liste, Zählung je Format, Stand
//	GET    /admin/cleanup/mkv/stand        nur der Stand (zum Nachfragen)
//	POST   /admin/cleanup/mkv              {"fileIds": […]} | {"itemId": "…"} | {"alle": true},
//	                                       "original": "loeschen" (Standard) | "behalten"
//	DELETE /admin/cleanup/mkv              abbrechen (laufende Datei und Warteschlange)
//
// Der sichere Ablauf je Datei (mkvEineDatei):
//
//  1. Prüfen: FFmpeg da, Ordner beschreibbar, MKV gibt es noch nicht,
//     genug Platz (Grösse des Originals und ein Rand).
//  2. Mit ffmpeg -c copy in „.Name.lumen-tmp.mkv" im selben Ordner
//     schreiben (versteckt: Der Scanner sieht sie nicht). Die Datei steht
//     in einem Vermerk im Datenordner; läuft Lumen neu an, räumt
//     mkvVerwaisteAufraeumen sie weg.
//  3. Die fertige Datei mit ffprobe prüfen (umpacken.Pruefen): dieselben
//     Spuren, dieselbe Laufzeit ±1 s, plausible Grösse. Sonst weg damit —
//     das Original ist unberührt.
//  4. Unter der Arbeitssperre (kein Durchlauf dazwischen): MKV an ihren
//     Platz, das Original beiseite („.lumen-original"), die Datei in der
//     Datenbank umhängen. Scheitert ein Schritt, geht alles zurück.
//  5. „Original löschen": erst jetzt das beiseitegelegte Original weg.
//     „Original behalten": Es bleibt in „.lumen-original" (der Scan
//     liest den Ordner nicht — kein Doppel unter Aufräumen).

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"lumen/internal/scan"
	"lumen/internal/store"
	"lumen/internal/stream"
	"lumen/internal/umpacken"
)

// schwerMu: Umwandeln von Scheiben und Umpacken in MKV lesen und schreiben
// ganze Filme — nie zwei davon gleichzeitig auf dem NAS.
var schwerMu sync.Mutex

// mkvPruefen prüft die fertige MKV (in Tests ersetzbar).
var mkvPruefen = umpacken.Pruefen

// mkvOriginalOrdner: hierhin kommt das Original („Original behalten",
// und kurz auch beim Löschen, damit sich alles zurückrollen lässt).
const mkvOriginalOrdner = ".lumen-original"

// mkvAuftrag: eine Datei in der Warteschlange.
type mkvAuftrag struct {
	FileID   string `json:"fileId"`
	libID    string
	pfad     string
	Name     string `json:"name"` // Dateiname, ohne Ordner
	behalten bool
}

// mkvErgebnis: wie es für eine Datei ausging.
type mkvErgebnis struct {
	ergebnis string // fertig, fehler, abgebrochen
	fehler   *apiFehler
	libID    string
}

// mkvLauf: die Warteschlange und ihr Stand.
type mkvLauf struct {
	mu          sync.Mutex
	laeuft      bool // der Arbeiter läuft
	aktuell     *mkvAuftrag
	schritt     string // warten, pruefen, umpacken, kontrollieren, eintragen
	anteil      float64
	restSek     int
	beginn      time.Time
	schlange    []mkvAuftrag
	ergebnis    map[string]mkvErgebnis
	fertig      int // seit dem Start der Warteschlange
	gescheitert int
	abbrechen   context.CancelFunc
	gestoppt    bool
}

var mkvAuftraege = mkvLauf{ergebnis: map[string]mkvErgebnis{}}

// mkvStand ist die Antwort auf GET …/stand.
type mkvStand struct {
	Laeuft      bool        `json:"laeuft"`
	Aktuell     *mkvAuftrag `json:"aktuell,omitempty"`
	Schritt     string      `json:"schritt,omitempty"`
	Anteil      float64     `json:"anteil"`
	RestSek     int         `json:"restSek,omitempty"`
	Wartend     []string    `json:"wartend"` // Kennungen der Dateien in der Warteschlange
	Fertig      int         `json:"fertig"`
	Gescheitert int         `json:"gescheitert"`
	Ergebnisse  []mkvZeile  `json:"ergebnisse"`
}

type mkvZeile struct {
	FileID   string `json:"fileId"`
	Ergebnis string `json:"ergebnis"`
	Fehler   string `json:"fehler,omitempty"`
}

func (l *mkvLauf) setzen(f func(l *mkvLauf)) {
	l.mu.Lock()
	defer l.mu.Unlock()
	f(l)
}

func (l *mkvLauf) fortschritt(schritt string, anteil float64) {
	l.setzen(func(l *mkvLauf) {
		l.schritt = schritt
		l.anteil = min(max(anteil, 0), 1)
		l.restSek = 0
		if l.anteil > 0.01 {
			vergangen := time.Since(l.beginn).Seconds()
			l.restSek = int(vergangen * (1 - l.anteil) / l.anteil)
		}
	})
}

// mkvStandFuer: der Stand, so weit das Konto die Mediatheken verwalten darf.
func (s *Server) mkvStandFuer(w http.ResponseWriter, r *http.Request) mkvStand {
	l := &mkvAuftraege
	l.mu.Lock()
	st := mkvStand{Laeuft: l.laeuft, Fertig: l.fertig, Gescheitert: l.gescheitert, Wartend: []string{}, Ergebnisse: []mkvZeile{}}
	if a := l.aktuell; a != nil && s.darfMediathek(r, a.libID) {
		kopie := *a
		st.Aktuell, st.Schritt, st.Anteil, st.RestSek = &kopie, l.schritt, l.anteil, l.restSek
	}
	for _, a := range l.schlange {
		if s.darfMediathek(r, a.libID) {
			st.Wartend = append(st.Wartend, a.FileID)
		}
	}
	type roh struct {
		id string
		e  mkvErgebnis
	}
	var ergebnisse []roh
	for id, e := range l.ergebnis {
		if s.darfMediathek(r, e.libID) {
			ergebnisse = append(ergebnisse, roh{id, e})
		}
	}
	l.mu.Unlock()
	sort.Slice(ergebnisse, func(i, j int) bool { return ergebnisse[i].id < ergebnisse[j].id })
	for _, e := range ergebnisse {
		z := mkvZeile{FileID: e.id, Ergebnis: e.e.ergebnis}
		if e.e.fehler != nil {
			z.Fehler = satzAnfang(tr(w, e.e.fehler.text, e.e.fehler.werte...))
		}
		st.Ergebnisse = append(st.Ergebnisse, z)
	}
	return st
}

// mkvFormat: Zählung je Endung.
type mkvFormat struct {
	Endung  string `json:"endung"` // ".avi"
	Anzahl  int    `json:"anzahl"`
	Bytes   int64  `json:"bytes"`
	InAllen bool   `json:"inAllen"`
}

// mkvZeilenGrenze: so viele Dateien zeigt die Liste höchstens.
const mkvZeilenGrenze = 2000

// mkvDateien: die Dateien in älteren Behältern, die das Konto verwalten darf.
func (s *Server) mkvDateien(r *http.Request, itemID string) ([]store.EndungDatei, error) {
	var endungen []string
	for e := range umpacken.Endungen {
		endungen = append(endungen, e)
	}
	sort.Strings(endungen)
	alle, err := s.st.DateienMitEndung(r.Context(), endungen, itemID)
	if err != nil {
		return nil, err
	}
	out := alle[:0]
	for _, d := range alle {
		if s.darfMediathek(r, d.LibraryID) && !scan.InDatenordner(d.Pfad) {
			out = append(out, d)
		}
	}
	return out, nil
}

// adminMkvListe: GET /admin/cleanup/mkv.
func (s *Server) adminMkvListe(w http.ResponseWriter, r *http.Request) {
	dateien, err := s.mkvDateien(r, strings.TrimSpace(r.URL.Query().Get("itemId")))
	if err != nil {
		failErr(w, err)
		return
	}
	je := map[string]*mkvFormat{}
	type zeile struct {
		store.EndungDatei
		Endung  string `json:"endung"`
		InAllen bool   `json:"inAllen"`
	}
	zeilen := []zeile{}
	for _, d := range dateien {
		e := umpacken.Endung(d.Pfad)
		f := je[e]
		if f == nil {
			f = &mkvFormat{Endung: e, InAllen: umpacken.InAllen(e)}
			je[e] = f
		}
		f.Anzahl++
		f.Bytes += d.Groesse
		if len(zeilen) < mkvZeilenGrenze {
			zeilen = append(zeilen, zeile{EndungDatei: d, Endung: e, InAllen: umpacken.InAllen(e)})
		}
	}
	formate := []mkvFormat{}
	for _, f := range je {
		formate = append(formate, *f)
	}
	sort.Slice(formate, func(i, j int) bool {
		if formate[i].Anzahl != formate[j].Anzahl {
			return formate[i].Anzahl > formate[j].Anzahl
		}
		return formate[i].Endung < formate[j].Endung
	})
	ok(w, map[string]any{
		"formate": formate, "dateien": zeilen, "gesamt": len(dateien),
		"ffmpeg": s.ff != nil && s.ff.Available(),
		"stand":  s.mkvStandFuer(w, r),
	})
}

// adminMkvStand: GET /admin/cleanup/mkv/stand.
func (s *Server) adminMkvStand(w http.ResponseWriter, r *http.Request) {
	ok(w, s.mkvStandFuer(w, r))
}

// adminMkvAbbrechen: DELETE /admin/cleanup/mkv — die laufende Datei
// abbrechen und die Warteschlange leeren (nur, was das Konto verwalten darf).
func (s *Server) adminMkvAbbrechen(w http.ResponseWriter, r *http.Request) {
	l := &mkvAuftraege
	l.mu.Lock()
	etwas := false
	rest := l.schlange[:0]
	for _, a := range l.schlange {
		if s.darfMediathek(r, a.libID) {
			etwas = true
			continue
		}
		rest = append(rest, a)
	}
	l.schlange = rest
	if l.aktuell != nil && s.darfMediathek(r, l.aktuell.libID) && l.abbrechen != nil {
		l.abbrechen()
		etwas = true
	}
	l.mu.Unlock()
	if !etwas {
		fail(w, http.StatusNotFound, "gerade wird nichts umgewandelt")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// adminMkvUmwandeln: POST /admin/cleanup/mkv.
func (s *Server) adminMkvUmwandeln(w http.ResponseWriter, r *http.Request) {
	var in struct {
		FileIDs  []string `json:"fileIds"`
		ItemID   string   `json:"itemId"`
		Alle     bool     `json:"alle"`
		Original string   `json:"original"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	behalten := false
	switch in.Original {
	case "", "loeschen":
	case "behalten":
		behalten = true
	default:
		fail(w, http.StatusBadRequest, "original: loeschen oder behalten")
		return
	}
	if s.ff == nil || !s.ff.Available() {
		fail(w, http.StatusServiceUnavailable, "ohne FFmpeg geht das nicht")
		return
	}
	if len(in.FileIDs) > 0 && (in.Alle || in.ItemID != "") || len(in.FileIDs) > 10000 {
		fail(w, http.StatusBadRequest, "entweder dateien, ein titel oder alle")
		return
	}
	// Welche Dateien: immer aus der Liste der Datenbank — nie ein Pfad vom Gerät.
	var dateien []store.EndungDatei
	if len(in.FileIDs) > 0 {
		alle, err := s.mkvDateien(r, "")
		if err != nil {
			failErr(w, err)
			return
		}
		gewuenscht := map[string]bool{}
		for _, id := range in.FileIDs {
			gewuenscht[id] = true
		}
		for _, d := range alle {
			if gewuenscht[d.FileID] {
				dateien = append(dateien, d)
			}
		}
		if len(dateien) == 0 {
			failErr(w, store.ErrNotFound)
			return
		}
	} else if in.ItemID != "" || in.Alle {
		if in.ItemID != "" {
			if _, err := s.visibleItem(r, in.ItemID); err != nil {
				failErr(w, err)
				return
			}
		}
		alle, err := s.mkvDateien(r, in.ItemID)
		if err != nil {
			failErr(w, err)
			return
		}
		for _, d := range alle {
			// „Alle umwandeln" lässt MP4/M4V aus (umpacken.InAllen); wer
			// beim Titel ausdrücklich umwandelt, bekommt alle.
			if in.ItemID != "" || umpacken.InAllen(umpacken.Endung(d.Pfad)) {
				dateien = append(dateien, d)
			}
		}
		if len(dateien) == 0 {
			fail(w, http.StatusNotFound, "hier ist nichts mehr umzuwandeln")
			return
		}
	} else {
		fail(w, http.StatusBadRequest, "entweder dateien, ein titel oder alle")
		return
	}

	l := &mkvAuftraege
	l.mu.Lock()
	drin := map[string]bool{}
	if l.aktuell != nil {
		drin[l.aktuell.FileID] = true
	}
	for _, a := range l.schlange {
		drin[a.FileID] = true
	}
	if !l.laeuft {
		// Eine neue Warteschlange: Zähler und alte Ergebnisse weg.
		l.fertig, l.gescheitert = 0, 0
		l.ergebnis = map[string]mkvErgebnis{}
	}
	neu := 0
	for _, d := range dateien {
		if drin[d.FileID] {
			continue
		}
		drin[d.FileID] = true
		delete(l.ergebnis, d.FileID)
		l.schlange = append(l.schlange, mkvAuftrag{FileID: d.FileID, libID: d.LibraryID, pfad: d.Pfad,
			Name: filepath.Base(d.Pfad), behalten: behalten})
		neu++
	}
	starten := !l.laeuft && len(l.schlange) > 0
	if starten {
		l.laeuft = true
	}
	l.mu.Unlock()
	if starten {
		s.begleitet(s.mkvArbeiter)
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"anzahl": neu,
		"status": tr(w, "Läuft – das kann je nach Film eine Weile dauern.")})
}

// mkvArbeiter arbeitet die Warteschlange ab, eine Datei nach der anderen.
// Er endet, wenn sie leer ist oder Lumen herunterfährt.
func (s *Server) mkvArbeiter(ctx context.Context) {
	l := &mkvAuftraege
	for {
		l.mu.Lock()
		if len(l.schlange) == 0 || ctx.Err() != nil {
			l.laeuft, l.aktuell, l.abbrechen, l.schritt, l.anteil, l.restSek = false, nil, nil, "", 0, 0
			l.schlange = nil
			l.mu.Unlock()
			return
		}
		a := l.schlange[0]
		l.schlange = l.schlange[1:]
		jobCtx, ab := context.WithCancel(ctx)
		l.aktuell, l.abbrechen = &a, ab
		l.schritt, l.anteil, l.restSek, l.beginn = "warten", 0, 0, time.Now()
		l.mu.Unlock()

		err := s.mkvEineDatei(jobCtx, a)
		ab()
		erg := mkvErgebnis{libID: a.libID}
		switch {
		case err == nil:
			erg.ergebnis = "fertig"
		case errors.Is(err, context.Canceled):
			erg.ergebnis = "abgebrochen"
		default:
			erg.ergebnis = "fehler"
			var af *apiFehler
			if !errors.As(err, &af) {
				af = &apiFehler{status: 500, text: "umwandeln gescheitert: {grund}",
					werte: []any{"grund", stream.Bereinigt(err.Error())}}
			}
			erg.fehler = af
		}
		l.setzen(func(l *mkvLauf) {
			l.ergebnis[a.FileID] = erg
			switch erg.ergebnis {
			case "fertig":
				l.fertig++
			case "fehler":
				l.gescheitert++
			}
			l.aktuell, l.abbrechen = nil, nil
		})
		// Ins Protokoll mit Grund, ohne Pfad und Titel (Datenschutz).
		bg := s.hintergrund()
		switch erg.ergebnis {
		case "abgebrochen":
			s.st.Log(bg, "info", "aufraeumen", "Umpacken in MKV abgebrochen")
		case "fehler":
			s.st.Log(bg, "error", "aufraeumen", "Umpacken in MKV gescheitert: "+stream.Bereinigt(fehlerText(erg.fehler)))
		}
	}
}

// fehlerText setzt die Werte in den Text einer Meldung (fürs Protokoll).
func fehlerText(af *apiFehler) string {
	grund := af.text
	for i := 0; i+1 < len(af.werte); i += 2 {
		grund = strings.ReplaceAll(grund, "{"+fmt.Sprint(af.werte[i])+"}", fmt.Sprint(af.werte[i+1]))
	}
	return grund
}

// Meldungen, wenn die Prüfung der neuen Datei scheitert. Das Original
// bleibt dann, wie es war.
const (
	mkvSpurenText  = "die neue datei hat nicht dieselben spuren wie das original – das original bleibt unverändert"
	mkvDauerText   = "die neue datei ist {neu} s lang, das original {alt} s – das original bleibt unverändert"
	mkvGroesseText = "die neue datei ist mit {prozent} % der grösse des originals verdächtig klein – das original bleibt unverändert"
)

// mkvVermerk: Datei im Datenordner, in der die halbfertige MKV steht,
// solange FFmpeg schreibt (siehe mkvVerwaisteAufraeumen).
func (s *Server) mkvVermerk() string {
	basis := s.dataDir
	if basis == "" {
		basis = os.TempDir()
	}
	return filepath.Join(basis, "mkv-umpacken.offen")
}

// mkvVerwaisteAufraeumen: Brach Lumen mitten im Umpacken ab (Absturz,
// Stromausfall, Neustart), liegt die halbfertige MKV noch im Medienordner.
// Beim Start weg damit — es ist nur die vorläufige Datei, nie das Original.
func (s *Server) mkvVerwaisteAufraeumen() {
	b, err := os.ReadFile(s.mkvVermerk())
	if err != nil {
		return
	}
	for _, z := range strings.Split(string(b), "\n") {
		if z = strings.TrimSpace(z); z != "" && filepath.IsAbs(z) && umpacken.IstTemp(z) {
			_ = os.Remove(z)
		}
	}
	_ = os.Remove(s.mkvVermerk())
}

// mkvEineDatei: der Ablauf für eine Datei (siehe oben). Zurück kommt nil,
// context.Canceled oder ein Fehler (oft *apiFehler mit fertiger Meldung).
func (s *Server) mkvEineDatei(ctx context.Context, a mkvAuftrag) error {
	l := &mkvAuftraege
	// Nie neben dem Umwandeln einer Scheibe her.
	warten := make(chan struct{})
	go func() { schwerMu.Lock(); close(warten) }()
	select {
	case <-warten:
	case <-ctx.Done():
		go func() { <-warten; schwerMu.Unlock() }()
		return context.Canceled
	}
	defer schwerMu.Unlock()
	l.setzen(func(l *mkvLauf) { l.schritt, l.beginn = "pruefen", time.Now() })

	f, err := s.st.File(ctx, a.FileID)
	if err != nil || f.Path != a.pfad || f.Missing {
		return &apiFehler{status: http.StatusConflict, text: "die datei hat sich in der zwischenzeit verändert – bitte die liste neu laden"}
	}
	info, err := os.Stat(f.Path)
	if err != nil || !info.Mode().IsRegular() {
		return &apiFehler{status: http.StatusNotFound, text: "die datei ist nicht mehr da"}
	}
	ziel := umpacken.Ziel(f.Path)
	if err := zielPruefen(ziel); err != nil {
		return err
	}
	if err := platzPruefen(filepath.Dir(f.Path), info.Size(), platzMedienText); err != nil {
		return err
	}

	eingang := stream.Eingang(f.Path)
	pctx, pab := context.WithTimeout(ctx, 5*time.Minute)
	bestand, err := umpacken.Lesen(pctx, s.ff.FFprobePath, f.Path, eingang)
	pab()
	if err != nil {
		if ctx.Err() != nil {
			return context.Canceled
		}
		return &apiFehler{status: http.StatusBadRequest, text: "die datei lässt sich nicht lesen: {grund}",
			werte: []any{"grund", stream.Bereinigt(err.Error())}}
	}
	plan, err := umpacken.Planen(f.Path, bestand)
	if err != nil {
		var pn *umpacken.SpurPasstNicht
		switch {
		case errors.As(err, &pn):
			return &apiFehler{status: http.StatusBadRequest,
				text:  "die datei enthält untertitel im format {codec}, die nicht in eine mkv passen – sie bleibt, wie sie ist",
				werte: []any{"codec", pn.Codec}}
		case errors.Is(err, umpacken.ErrKeinBild):
			return &apiFehler{status: http.StatusBadRequest, text: "in der datei ist kein bild zu finden"}
		}
		return err
	}

	tmp := umpacken.Temp(f.Path)
	_ = os.Remove(tmp) // Rest eines früheren Versuchs
	if err := os.WriteFile(s.mkvVermerk(), []byte(tmp+"\n"), 0o600); err != nil {
		return err
	}
	defer os.Remove(s.mkvVermerk())
	defer os.Remove(tmp) // nach dem Umbenennen gibt es sie nicht mehr

	l.fortschritt("umpacken", 0)
	uctx, uab := context.WithTimeout(ctx, 6*time.Hour)
	err = umpacken.Umpacken(uctx, s.ff.FFmpegPath, plan, eingang, tmp,
		func(c *exec.Cmd) ([]byte, error) { return stream.Niedrig(c, true) },
		func(n int64) { l.fortschritt("umpacken", 0.97*float64(n)/float64(max(info.Size(), 1))) })
	uab()
	if err != nil {
		if ctx.Err() != nil {
			return context.Canceled
		}
		if pe := (*os.PathError)(nil); errors.As(err, &pe) {
			return ordnerFehler(err)
		}
		return err
	}

	l.fortschritt("kontrollieren", 0.97)
	kctx, kab := context.WithTimeout(ctx, 5*time.Minute)
	err = mkvPruefen(kctx, s.ff.FFprobePath, plan, tmp, info.Size())
	kab()
	if err != nil {
		if ctx.Err() != nil {
			return context.Canceled
		}
		return mkvPruefFehler(err)
	}
	if ctx.Err() != nil {
		return context.Canceled
	}

	// Ab hier nicht mehr abbrechen: Umhängen und Aufräumen gehen in einem
	// Zug, am Hintergrund-Kontext, unter der Arbeitssperre.
	l.fortschritt("eintragen", 0.99)
	neu := s.mkvMessen(s.hintergrund(), tmp, ziel)
	var beiseite string
	s.Nacheinander(s.hintergrund(), func(bg context.Context) {
		beiseite, err = s.mkvErsetzen(bg, f, tmp, ziel, neu)
	})
	if err != nil {
		return err
	}
	if beiseite == "" { // Lumen fährt herunter: nichts verändert
		return context.Canceled
	}
	hinweis := ""
	if !a.behalten {
		if e := os.Remove(beiseite); e == nil {
			_ = os.Remove(filepath.Dir(beiseite)) // nur, wenn leer
		} else {
			hinweis = " (Original liess sich nicht löschen, liegt in " + mkvOriginalOrdner + ")"
		}
	}
	s.mkvKennungenNachziehen(f, neu)

	was := fmt.Sprintf("%s → MKV, %d Spuren", strings.ToUpper(strings.TrimPrefix(umpacken.Endung(f.Path), ".")), len(plan.Spuren))
	if len(plan.Gewandelt) > 0 {
		was += ", angepasst: " + strings.Join(plan.Gewandelt, ", ")
	}
	if len(plan.Weggelassen) > 0 {
		was += ", weggelassen: " + strings.Join(plan.Weggelassen, ", ")
	}
	if a.behalten {
		was += ", Original in " + mkvOriginalOrdner
	}
	s.st.Log(s.hintergrund(), "info", "aufraeumen", "In MKV umgepackt ("+was+")"+hinweis)
	return nil
}

// mkvPruefFehler macht aus einer Abweichung eine Meldung fürs Gerät.
func mkvPruefFehler(err error) error {
	var ab *umpacken.Abweichung
	if !errors.As(err, &ab) {
		return &apiFehler{status: 500, text: "die neue datei liess sich nicht prüfen: {grund} – das original bleibt unverändert",
			werte: []any{"grund", stream.Bereinigt(err.Error())}}
	}
	switch ab.Art {
	case "dauer":
		return &apiFehler{status: 500, text: mkvDauerText,
			werte: []any{"neu", fmt.Sprintf("%.1f", float64(ab.Ist)/1000), "alt", fmt.Sprintf("%.1f", float64(ab.Soll)/1000)}}
	case "groesse":
		return &apiFehler{status: 500, text: mkvGroesseText,
			werte: []any{"prozent", fmt.Sprintf("%.0f", 100*float64(ab.Ist)/float64(max(ab.Soll, 1)))}}
	}
	return &apiFehler{status: 500, text: mkvSpurenText}
}

// mkvMessen misst die neue Datei aus (Laufzeit, Bild, Spuren, Kapitel) —
// noch unter dem vorläufigen Namen tmp, eingetragen wird sie als ziel.
func (s *Server) mkvMessen(ctx context.Context, tmp, ziel string) *store.MediaFile {
	neu := &store.MediaFile{Path: ziel, Container: "mkv"}
	if info, err := os.Stat(tmp); err == nil {
		neu.SizeBytes = info.Size()
	}
	mctx, mab := context.WithTimeout(ctx, time.Minute)
	defer mab()
	if p, err := s.ff.Probe(mctx, tmp); err == nil {
		neu.DurationMS, neu.Bitrate = p.DurationMS, p.Bitrate
		neu.Width, neu.Height, neu.VideoCodec, neu.HDR = p.Width, p.Height, p.VideoCodec, p.HDR
		// Untertitel neben der Datei gehören zum künftigen Namen.
		neu.Spuren = scan.SpurenFuerStore(append(p.Tracks, scan.SidecarSubtitles(ziel)...))
		neu.Kapitel = scan.KapitelFuerStore(p.Chapters)
		if neu.Spuren == nil {
			neu.Spuren = []store.Track{}
		}
	}
	return neu
}

// mkvErsetzen: die MKV an ihren Platz, das Original beiseite, die Datei in
// der Datenbank umhängen — oder alles zurück. Läuft unter der
// Arbeitssperre. Zurück kommt, wo das Original jetzt liegt.
func (s *Server) mkvErsetzen(ctx context.Context, f *store.MediaFile, tmp, ziel string, neu *store.MediaFile) (string, error) {
	// Nicht überschreiben, falls inzwischen jemand eine Datei dorthin gelegt hat.
	if _, err := os.Lstat(ziel); err == nil {
		return "", &apiFehler{status: http.StatusConflict, text: "dort liegt schon eine datei „{name}“ – schon umgewandelt?",
			werte: []any{"name", filepath.Base(ziel)}}
	}
	if err := os.Rename(tmp, ziel); err != nil {
		return "", ordnerFehler(err)
	}
	info, err := os.Stat(ziel)
	if err != nil {
		_ = os.Remove(ziel)
		return "", err
	}
	neu.SizeBytes, neu.MTime = info.Size(), info.ModTime().Unix()

	ordner := filepath.Join(filepath.Dir(f.Path), mkvOriginalOrdner)
	if err := os.MkdirAll(ordner, 0o755); err != nil {
		_ = os.Remove(ziel)
		return "", ordnerFehler(err)
	}
	beiseite, err := beiseiteName(ordner, filepath.Dir(f.Path), f.Path)
	if err == nil {
		err = os.Rename(f.Path, beiseite)
	}
	if err != nil {
		_ = os.Remove(ziel)
		_ = os.Remove(ordner) // nur, wenn leer
		return "", ordnerFehler(err)
	}
	if err := s.st.DateiUmhaengen(ctx, f.ID, f.Path, neu); err != nil {
		// Zurück: Original an seinen Platz, MKV weg.
		if e := os.Rename(beiseite, f.Path); e != nil {
			// Dann bleibt die MKV — sie ist der ganze Film; der nächste
			// Durchlauf nimmt sie auf.
			return "", fmt.Errorf("datenbank: %v — das original liegt in %s", err, mkvOriginalOrdner)
		}
		_ = os.Remove(ziel)
		_ = os.Remove(ordner)
		if errors.Is(err, store.ErrDateiVeraendert) {
			return "", &apiFehler{status: http.StatusConflict, text: "die datei hat sich in der zwischenzeit verändert – bitte die liste neu laden"}
		}
		return "", err
	}
	return beiseite, nil
}

// mkvKennungenNachziehen: Vorschaubilder und Standbild hängen an der
// Fassung der Datei (Kennung, Grösse, Zeit). Bild und Laufzeit sind nach
// dem Umpacken dieselben — sie gelten weiter, statt neu gerechnet zu
// werden. Den Untertitel-Vorrat dagegen neu ziehen: Die Spurnummern
// können sich verschoben haben.
func (s *Server) mkvKennungenNachziehen(alt, neu *store.MediaFile) {
	if s.dataDir == "" || !gueltigeKennung(alt.ID) {
		return
	}
	vorher, nachher := *alt, *alt
	nachher.SizeBytes, nachher.MTime = neu.SizeBytes, neu.MTime
	kAlt, kNeu := dateiKennung(&vorher), dateiKennung(&nachher)
	umschreiben := func(pfad string) {
		if b, err := os.ReadFile(pfad); err == nil && string(b) == kAlt {
			if os.WriteFile(pfad, []byte(kNeu), 0o644) == nil {
				zeitlos(pfad)
			}
		}
	}
	umschreiben(filepath.Join(s.trickplayOrdner(alt.ID), stream.TrickplayQuelle))
	if z, err := s.standbildZiel(&store.Item{ID: alt.ItemID}); err == nil {
		umschreiben(z + ".quelle")
	}
	alte, _ := filepath.Glob(filepath.Join(s.untertitelVorrat(), alt.ID+"-*.vtt"))
	for _, p := range alte {
		_ = os.Remove(p)
	}
	_ = os.Remove(s.vorratVermerk(alt.ID))
}
