// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

// Arbeit im Hintergrund: Vorschaubilder beim Spulen (Trickplay) und
// Untertitel, die schon fertig daliegen, bevor jemand auf Play drückt.
//
// Beides liest eine Datei einmal ganz — bei einer Bibliothek von einigen
// Terabyte ist das viel Arbeit. Darum:
//
//   - immer nur eine Datei zugleich, mit niedrigster Rechen- und
//     Plattenpriorität (stream.Niedrig);
//   - Wiedergabe hat Vorrang: Läuft irgendeine Wiedergabe (auch direkt
//     abgespielt — die liest dieselbe Platte), wartet der Durchgang, auch
//     mitten in einer Datei;
//   - grosse Dateien werden nicht ganz gelesen: Für die Vorschaubilder
//     springt FFmpeg zu jeder Stelle (stream.TrickplayErzeugenMit);
//   - Neues zuerst: Die Liste beginnt mit den zuletzt eingelesenen
//     Dateien, und nach einem Durchlauf (hintergrundAnstossen) fängt der
//     laufende Durchgang mit der neuen Liste von vorn an — was schon
//     fertig ist, wird dabei nur kurz geprüft. Das hängt am Einlesen,
//     nicht am Abspielen, und verrät darum nichts;
//   - es gibt kein „Vorziehen" der Datei, die gerade jemand abspielt:
//     Früher kam sie beim ersten Abruf der Vorschaubilder sofort dran —
//     und der Zeitstempel des neu angelegten Ordners (ctime, auch nach
//     zeitlos) verriet, wann jemand welchen Film angefangen hat. Jetzt
//     entsteht alles nur im Durchgang über die ganze Mediathek, in fester
//     Reihenfolge, unabhängig davon, was jemand schaut;
//   - was fertig ist, liegt auf der Platte und wird nicht neu gemacht —
//     ausser die Datei wurde am selben Pfad ersetzt (dateiKennung:
//     Kennung, Grösse, Änderungszeit);
//   - was zu einer Datei gehört, die es nicht mehr gibt, wird weggeräumt.
//
// Dazwischen läuft die Erkennung von Vorspann und Abspann
// (vorspann_erkennung.go) nach denselben Regeln.
//
// Datenschutz: Die Dateien tragen keine Zeit (zeitlos) und Namen ohne
// Titel, nur die Kennung der Datei. Untertitel, die erst beim Abspielen
// entstehen, kommen nicht auf die Platte, sondern in den Arbeitsspeicher
// (spuren.go) — sonst hielte ihr Anlegen fest, wann jemand den Film
// angefangen hat.

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"lumen/internal/store"
	"lumen/internal/stream"
)

type hintergrundZustand struct {
	mu        sync.Mutex
	wecken    chan struct{}
	fehl      map[string]time.Time // wo es nicht ging: eine Weile nicht wieder
	gestartet bool
}

var hg = &hintergrundZustand{wecken: make(chan struct{}, 1), fehl: map[string]time.Time{}}

// hintergrundAnstossen weckt den Durchgang (nach einem Scan, nach dem
// Einschalten in der Verwaltung).
func (s *Server) hintergrundAnstossen() {
	select {
	case hg.wecken <- struct{}{}:
	default:
	}
}

// hintergrundArbeit läuft, bis ctx endet.
func (s *Server) hintergrundArbeit(ctx context.Context) {
	hg.mu.Lock()
	if hg.gestartet {
		hg.mu.Unlock()
		return
	}
	hg.gestartet = true
	hg.mu.Unlock()

	// Nicht gleich beim Start: erst soll der Server erreichbar sein, und
	// ein Scan kommt womöglich ohnehin gleich.
	select {
	case <-time.After(2 * time.Minute):
	case <-hg.wecken:
	case <-ctx.Done():
		return
	}
	tick := time.NewTicker(6 * time.Hour)
	defer tick.Stop()
	for {
		s.hintergrundDurchgang(ctx)
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		case <-hg.wecken:
		}
	}
}

// hintergrundDurchgang geht einmal über alle Dateien.
func (s *Server) hintergrundDurchgang(ctx context.Context) {
	s.vorratAufraeumen(ctx)
	// Vorspann und Abspann erkennen (vorspann_erkennung.go): zuerst ein
	// Stück, dann zwischendurch weiter, am Ende den Rest.
	s.erkennungDurchgang(ctx, erkennungStueck)
	ids, err := s.st.VorhandeneDateien(ctx)
	if err != nil {
		return
	}
	for i := 0; ; {
		if ctx.Err() != nil {
			return
		}
		if i >= len(ids) {
			s.erkennungDurchgang(ctx, 0)
			return
		}
		if i > 0 && i%erkennungAlle == 0 {
			s.erkennungDurchgang(ctx, erkennungStueck)
		}
		// Ein Durchlauf hat Neues gebracht: mit der neuen Liste von vorn
		// (neueste zuerst).
		select {
		case <-hg.wecken:
			if neu, err := s.st.VorhandeneDateien(ctx); err == nil {
				ids, i = neu, 0
				continue
			}
		default:
		}
		// Wiedergabe hat Vorrang: warten, bis nichts mehr abgespielt wird.
		if s.wiedergabeLaeuft() {
			select {
			case <-time.After(30 * time.Second):
			case <-ctx.Done():
				return
			}
			continue
		}
		s.dateiVorbereiten(ctx, ids[i])
		i++
	}
}

// wiedergabeLaeuft: Spielt gerade jemand etwas ab — umgewandelt oder
// direkt? Beides braucht die Platte.
func (s *Server) wiedergabeLaeuft() bool {
	return s.live != nil && s.live.Load().Total > 0
}

// aufRuheWarten hält an, solange abgespielt wird — zwischen zwei
// Vorschaubildern einer grossen Datei.
func (s *Server) aufRuheWarten(ctx context.Context) error {
	for s.wiedergabeLaeuft() {
		select {
		case <-time.After(15 * time.Second):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return ctx.Err()
}

// dateiKennung: welche Fassung einer Datei etwas Vorbereitetes gehört.
// Wird eine Datei am selben Pfad ersetzt, behält sie ihre Kennung, aber
// Grösse und Änderungszeit ändern sich (der Durchlauf trägt sie ein).
func dateiKennung(f *store.MediaFile) string {
	return fmt.Sprintf("%s:%d:%d", f.ID, f.SizeBytes, f.MTime)
}

// dateiVorbereiten macht, was für eine Datei noch fehlt.
func (s *Server) dateiVorbereiten(ctx context.Context, fileID string) {
	hg.mu.Lock()
	if wann, ok := hg.fehl[fileID]; ok && time.Since(wann) < 24*time.Hour {
		hg.mu.Unlock()
		return
	}
	hg.mu.Unlock()
	f, err := s.st.File(ctx, fileID)
	if err != nil || f.Missing || f.DurationMS <= 0 || f.Defekt != "" {
		return
	}
	if _, err := os.Stat(f.Path); err != nil {
		return
	}
	// Ging es mit genau dieser Fassung der Datei schon einmal nicht
	// („Invalid data found"), nicht wieder versuchen — auch nicht nach
	// einem Neustart. Erst wenn sich Grösse oder Änderungszeit ändern.
	if s.vorbereitenGescheitert(f) {
		return
	}
	var fehler []string
	if s.trickplayAn(ctx) && !trickplayFertig(s.trickplayOrdner(fileID), f) {
		d, _ := s.ff.Feinmessung(ctx, f.Path)
		if _, err := s.ff.TrickplayErzeugenMit(ctx, f.Path, f.DurationMS, d, s.trickplayOrdner(fileID),
			stream.TrickplayOptionen{Kennung: dateiKennung(f), Warten: s.aufRuheWarten}); err != nil {
			if ctx.Err() == nil {
				fehler = append(fehler, "Vorschaubilder: "+stream.Bereinigt(err.Error()))
			}
		} else {
			zeitlosAlles(s.trickplayOrdner(fileID))
			zeitlos(s.trickplayWurzel())
		}
	}
	if s.untertitelVorbereitenAn(ctx) {
		if err := s.untertitelVorbereiten(ctx, f); err != nil && ctx.Err() == nil {
			fehler = append(fehler, "Untertitel: "+stream.Bereinigt(err.Error()))
		}
	}
	if len(fehler) > 0 {
		hg.mu.Lock()
		hg.fehl[fileID] = time.Now()
		hg.mu.Unlock()
		s.vorbereitenGescheitertMerken(f)
		// Ins Protokoll: dass es nicht ging, nicht bei welchem Film.
		s.st.Log(ctx, "warn", "vorbereiten", "Eine Datei liess sich nicht vorbereiten — "+strings.Join(fehler, "; "))
	}
}

// vorbereitenFehlerOrdner: je Datei, bei der das Vorbereiten scheiterte,
// eine kleine Datei mit der Kennung der Fassung (dateiKennung). Im
// Arbeitsspeicher allein (hg.fehl) ging das Wissen mit jedem Neustart
// verloren, und dieselben kaputten Dateien standen wieder im Protokoll.
func (s *Server) vorbereitenFehlerOrdner() string {
	return filepath.Join(s.dataDir, "vorbereiten-fehler")
}

func (s *Server) vorbereitenGescheitert(f *store.MediaFile) bool {
	b, err := os.ReadFile(filepath.Join(s.vorbereitenFehlerOrdner(), f.ID))
	return err == nil && strings.TrimSpace(string(b)) == dateiKennung(f)
}

func (s *Server) vorbereitenGescheitertMerken(f *store.MediaFile) {
	if err := os.MkdirAll(s.vorbereitenFehlerOrdner(), 0o700); err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(s.vorbereitenFehlerOrdner(), f.ID), []byte(dateiKennung(f)), 0o600)
}

func zeitlosAlles(ordner string) {
	eintraege, _ := os.ReadDir(ordner)
	for _, e := range eintraege {
		zeitlos(filepath.Join(ordner, e.Name()))
	}
	zeitlos(ordner)
}

// ---------------------------------------------------------------- Trickplay

func (s *Server) trickplayWurzel() string { return filepath.Join(s.dataDir, "trickplay") }

func (s *Server) trickplayOrdner(fileID string) string {
	return filepath.Join(s.trickplayWurzel(), fileID)
}

// trickplayFertig: Gibt es Vorschaubilder, und gehören sie zu dieser
// Fassung der Datei? Ordner älterer Fassungen von Lumen tragen keine
// Kennung; bei ihnen muss wenigstens die Laufzeit passen (sonst wäre die
// ganze Mediathek nach dem Update noch einmal zu lesen).
func trickplayFertig(ordner string, f *store.MediaFile) bool {
	info, err := stream.TrickplayLesen(ordner)
	if err != nil {
		return false
	}
	if k := stream.TrickplayKennung(ordner); k != "" {
		return k == dateiKennung(f)
	}
	soll := int((f.DurationMS/1000 + stream.TrickplayIntervall - 1) / stream.TrickplayIntervall)
	return info.Bilder >= soll-2 && info.Bilder <= soll+2
}

// gueltigeKennung: Kennungen landen in Pfaden — nur Buchstaben, Ziffern,
// Unterstrich und Bindestrich.
func gueltigeKennung(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

// trickplay: GET /stream/{fileId}/trickplay.
//
//	200 — fertig: Masse und die Adressen der Blätter
//	202 {"code":"wird_erstellt"} — angestossen, später noch einmal fragen
//	404 — gibt es nicht (ausgeschaltet, kein FFmpeg, ging nicht)
func (s *Server) trickplay(w http.ResponseWriter, r *http.Request) {
	f, _, err := s.fileForRequest(r)
	if err != nil {
		failErr(w, err)
		return
	}
	if !gueltigeKennung(f.ID) {
		fail(w, http.StatusNotFound, "nicht gefunden")
		return
	}
	if !s.trickplayAn(r.Context()) {
		streamFehler(w, http.StatusNotFound, "trickplay_aus", "vorschaubilder sind ausgeschaltet")
		return
	}
	if info, err := stream.TrickplayLesen(s.trickplayOrdner(f.ID)); err == nil && trickplayFertig(s.trickplayOrdner(f.ID), f) {
		blaetter := make([]string, info.Blaetter)
		for i := range blaetter {
			blaetter[i] = fmt.Sprintf("/api/v1/stream/%s/trickplay/%d.jpg", f.ID, i)
		}
		w.Header().Set("Cache-Control", "private, max-age=3600")
		ok(w, map[string]any{"intervall": info.Intervall, "breite": info.Breite, "hoehe": info.Hoehe,
			"spalten": info.Spalten, "zeilen": info.Zeilen, "bilder": info.Bilder, "blaetter": blaetter})
		return
	}
	if s.ff == nil || !s.ff.Available() || f.Missing || f.DurationMS <= 0 {
		streamFehler(w, http.StatusNotFound, "keine_vorschaubilder", "für diese datei gibt es keine vorschaubilder")
		return
	}
	hg.mu.Lock()
	wann, gescheitert := hg.fehl[f.ID]
	hg.mu.Unlock()
	if gescheitert && time.Since(wann) < 24*time.Hour {
		streamFehler(w, http.StatusNotFound, "keine_vorschaubilder", "für diese datei gibt es keine vorschaubilder")
		return
	}
	// Noch nicht fertig: Der Durchgang im Hintergrund kommt von selbst
	// dazu (siehe oben — kein Vorziehen). Die Apps fragen später noch
	// einmal, wie bisher.
	w.Header().Set("Retry-After", "60")
	writeJSON(w, http.StatusAccepted, map[string]string{"code": "wird_erstellt"})
}

// trickplayBlatt: GET /stream/{fileId}/trickplay/{blatt} — ein Blatt.
func (s *Server) trickplayBlatt(w http.ResponseWriter, r *http.Request) {
	f, _, err := s.fileForRequest(r)
	if err != nil {
		failErr(w, err)
		return
	}
	name := r.PathValue("blatt")
	n, err := strconv.Atoi(strings.TrimSuffix(name, ".jpg"))
	if err != nil || n < 0 || !strings.HasSuffix(name, ".jpg") || !gueltigeKennung(f.ID) {
		fail(w, http.StatusNotFound, "nicht gefunden")
		return
	}
	pfad := filepath.Join(s.trickplayOrdner(f.ID), strconv.Itoa(n)+".jpg")
	fh, err := os.Open(pfad)
	if err != nil {
		fail(w, http.StatusNotFound, "nicht gefunden")
		return
	}
	defer fh.Close()
	info, err := fh.Stat()
	if err != nil {
		fail(w, http.StatusNotFound, "nicht gefunden")
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "private, max-age=86400")
	http.ServeContent(w, r, "", info.ModTime(), fh)
}

// ---------------------------------------------------------------- Untertitel

func (s *Server) untertitelVorrat() string { return filepath.Join(s.dataDir, "untertitel-vorrat") }

func (s *Server) vorratPfad(fileID string, idx int) string {
	return filepath.Join(s.untertitelVorrat(), fmt.Sprintf("%s-%d.vtt", fileID, idx))
}

// vorratVermerk: aus welcher Fassung der Datei der Vorrat stammt.
func (s *Server) vorratVermerk(fileID string) string {
	return filepath.Join(s.untertitelVorrat(), fileID+"-quelle")
}

// vorratPruefen wirft den Untertitel-Vorrat einer Datei weg, wenn er zu
// einer früheren Fassung gehört (Datei am selben Pfad ersetzt). Vorrat
// älterer Fassungen von Lumen hat keinen Vermerk und bleibt.
func (s *Server) vorratPruefen(f *store.MediaFile) {
	if !gueltigeKennung(f.ID) {
		return
	}
	b, err := os.ReadFile(s.vorratVermerk(f.ID))
	if err != nil || string(b) == dateiKennung(f) {
		return
	}
	alte, _ := filepath.Glob(filepath.Join(s.untertitelVorrat(), f.ID+"-*.vtt"))
	for _, a := range alte {
		_ = os.Remove(a)
	}
	_ = os.Remove(s.vorratVermerk(f.ID))
}

// untertitelVorbereiten zieht alle eingebetteten Text-Untertitel einer
// Datei in einem Durchgang heraus und legt sie ab.
func (s *Server) untertitelVorbereiten(ctx context.Context, f *store.MediaFile) error {
	if !s.st.SpurenGelesen(ctx, f.ID) {
		s.spurenNachmessen(ctx, f)
	}
	spuren, err := s.st.Spuren(ctx, f.ID)
	if err != nil {
		return err
	}
	s.vorratPruefen(f)
	var fehlt []store.Track
	for _, t := range spuren {
		if t.Kind == "subtitle" && !t.Bitmap && !t.External {
			if _, err := os.Stat(s.vorratPfad(f.ID, t.Index)); err != nil {
				fehlt = append(fehlt, t)
			}
		}
	}
	if len(fehlt) == 0 || !gueltigeKennung(f.ID) {
		return nil
	}
	inhalte, err := s.textUntertitelZiehen(ctx, f, fehlt, true, nil)
	if err != nil && len(fehlt) > 1 && ctx.Err() == nil {
		// Eine kaputte Spur soll die anderen nicht mitreissen: einzeln
		// versuchen und behalten, was geht.
		inhalte = map[int][]byte{}
		var erster error
		for _, t := range fehlt {
			einzeln, e := s.textUntertitelZiehen(ctx, f, []store.Track{t}, true, nil)
			if e != nil {
				if erster == nil {
					erster = e
				}
				continue
			}
			for k, v := range einzeln {
				inhalte[k] = v
			}
		}
		err = nil
		if len(inhalte) == 0 {
			err = erster
		}
	}
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.untertitelVorrat(), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(s.vorratVermerk(f.ID), []byte(dateiKennung(f)), 0o600); err == nil {
		zeitlos(s.vorratVermerk(f.ID))
	}
	for idx, b := range inhalte {
		ziel := s.vorratPfad(f.ID, idx)
		tmp := ziel + ".tmp"
		if err := os.WriteFile(tmp, b, 0o600); err != nil {
			return err
		}
		zeitlos(tmp)
		if err := os.Rename(tmp, ziel); err != nil {
			return err
		}
	}
	return nil
}

// vorratAufraeumen wirft weg, was zu Dateien gehört, die es nicht mehr gibt.
func (s *Server) vorratAufraeumen(ctx context.Context) {
	if eintraege, err := os.ReadDir(s.trickplayWurzel()); err == nil {
		for _, e := range eintraege {
			name := e.Name()
			if strings.HasPrefix(name, ".entsteht-") {
				_ = os.RemoveAll(filepath.Join(s.trickplayWurzel(), name))
				continue
			}
			if !s.st.DateiGibtEs(ctx, name) {
				_ = os.RemoveAll(filepath.Join(s.trickplayWurzel(), name))
			}
		}
	}
	// Vermerke über gescheitertes Vorbereiten zu verschwundenen Dateien.
	if eintraege, err := os.ReadDir(s.vorbereitenFehlerOrdner()); err == nil {
		for _, e := range eintraege {
			if !s.st.DateiGibtEs(ctx, e.Name()) {
				_ = os.Remove(filepath.Join(s.vorbereitenFehlerOrdner(), e.Name()))
			}
		}
	}
	// Geholte Untertitel (<datei>.<sprache>….srt) zu verschwundenen Dateien.
	if eintraege, err := os.ReadDir(s.geholtOrdner()); err == nil {
		weg := false
		for _, e := range eintraege {
			name := e.Name()
			datei, _, _ := strings.Cut(name, ".")
			if datei == "" || !s.st.DateiGibtEs(ctx, datei) {
				_ = os.Remove(filepath.Join(s.geholtOrdner(), name))
				weg = true
			}
		}
		if weg {
			zeitlos(s.geholtOrdner())
		}
	}
	if eintraege, err := os.ReadDir(s.untertitelVorrat()); err == nil {
		for _, e := range eintraege {
			name := e.Name()
			i := strings.LastIndex(name, "-")
			if strings.HasSuffix(name, ".tmp") || i <= 0 || !s.st.DateiGibtEs(ctx, name[:i]) {
				_ = os.Remove(filepath.Join(s.untertitelVorrat(), name))
			}
		}
	}
}

// vorratZaehlen: wie viel vorbereitet daliegt — für die Verwaltung.
func (s *Server) vorratZaehlen() map[string]int {
	n := func(ordner, endung string) int {
		e, err := os.ReadDir(ordner)
		if err != nil {
			return 0
		}
		z := 0
		for _, x := range e {
			// Vermerke (…-quelle) zählen nicht mit.
			if strings.HasSuffix(x.Name(), endung) {
				z++
			}
		}
		return z
	}
	return map[string]int{"trickplay": n(s.trickplayWurzel(), ""), "untertitel": n(s.untertitelVorrat(), ".vtt")}
}
