// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

// Updates in der Verwaltung: Lumen sieht nach, ob es
// eine neuere Fassung gibt, sagt es dem Besitzer, und setzt sie auf
// Wunsch selbst ein — von Hand oder nachts von selbst, aber nie, während
// jemand schaut.
//
// Der Besitzer sieht hier Versionen, Zeiten und ein Protokoll der
// Aktualisierungen. Wer wann was geschaut hat, steht nirgends — die
// Entscheidung „jemand schaut gerade" fällt an der Zahl der laufenden
// Wiedergaben, nicht an Namen.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"lumen/internal/update"
)

const updateRepo = "ada462790-beep/lumen-media"

// updateEinstellung ist, was der Besitzer festlegt.
type updateEinstellung struct {
	// Pruefen: "aus" oder "taeglich".
	Pruefen string `json:"pruefen"`
	// PruefZeit: "HH:MM", wann täglich nachgesehen wird.
	PruefZeit string `json:"pruefZeit"`
	// Auto: "aus" oder "fenster" — von selbst aktualisieren, aber nur im Fenster.
	Auto string `json:"auto"`
	// FensterVon/FensterBis: "HH:MM", z. B. 03:00–05:00.
	FensterVon string `json:"fensterVon"`
	FensterBis string `json:"fensterBis"`
}

func (e updateEinstellung) mitVorgaben() updateEinstellung {
	if e.Pruefen == "" {
		e.Pruefen = "taeglich"
	}
	if e.PruefZeit == "" {
		e.PruefZeit = "04:00"
	}
	if e.Auto == "" {
		e.Auto = "aus"
	}
	if e.FensterVon == "" {
		e.FensterVon = "03:00"
	}
	if e.FensterBis == "" {
		e.FensterBis = "05:00"
	}
	return e
}

// updateEintrag ist eine Zeile im Aktualisierungs-Protokoll.
type updateEintrag struct {
	At       time.Time `json:"at"`
	Von      string    `json:"von"`
	Nach     string    `json:"nach"`
	Ergebnis string    `json:"ergebnis"` // "ok", "fehler", "verschoben"
	Notiz    string    `json:"notiz,omitempty"`
	Auto     bool      `json:"auto"`
}

// updateStand hält, was der letzte Blick auf GitHub ergeben hat.
type updateStand struct {
	mu       sync.Mutex
	geprueft time.Time
	neueste  *update.Release
	fehler   string
	laeuft   bool // gerade ein Einsetzen im Gang
	// tokenUngueltig: GitHub hat das Token beim letzten Prüfen abgelehnt.
	tokenUngueltig bool
	verschoben     time.Time
	// akt: der letzte Auftrag an den Aktualisierer (aktualisierer.go).
	akt aktStand
}

var updStand updateStand

func (s *Server) updatePruefer() *update.Pruefer {
	// Ein Token aus der Verwaltung hat Vorrang vor LUMEN_GITHUB_TOKEN:
	// Ist das aus der docker-compose.yml abgelaufen, lässt es
	// sich so ersetzen, ohne den Container anzufassen. Vorher galt das
	// alte aus der Umgebung weiter, und das Feld war gesperrt.
	token := s.st.Setting(context.Background(), "update.token", "")
	if token == "" {
		token = os.Getenv("LUMEN_GITHUB_TOKEN")
	}
	// LUMEN_UPDATE_API zeigt auf einen anderen Server — zum Ausprobieren
	// des ganzen Ablaufs ohne GitHub.
	return &update.Pruefer{Repo: updateRepo, Version: s.version, Token: token, API: os.Getenv("LUMEN_UPDATE_API")}
}

func (s *Server) updateEinstellungen(ctx context.Context) updateEinstellung {
	var e updateEinstellung
	_ = json.Unmarshal([]byte(s.st.Setting(ctx, "update.einstellung", "{}")), &e)
	return e.mitVorgaben()
}

func (s *Server) updateProtokoll(ctx context.Context) []updateEintrag {
	var out []updateEintrag
	_ = json.Unmarshal([]byte(s.st.Setting(ctx, "update.protokoll", "[]")), &out)
	return out
}

func (s *Server) updateProtokollieren(ctx context.Context, e updateEintrag) {
	e.At = time.Now()
	liste := append([]updateEintrag{e}, s.updateProtokoll(ctx)...)
	if len(liste) > 50 {
		liste = liste[:50]
	}
	raw, _ := json.Marshal(liste)
	_ = s.st.SetSetting(ctx, "update.protokoll", string(raw))
	level := "info"
	if e.Ergebnis == "fehler" {
		level = "error"
	}
	s.st.Log(ctx, level, "update", fmt.Sprintf("Aktualisierung %s → %s: %s %s", e.Von, e.Nach, e.Ergebnis, e.Notiz))
}

// UpdatePruefen sieht auf GitHub nach. Das Ergebnis bleibt im Speicher,
// damit die Verwaltung nicht bei jedem Öffnen GitHub fragt.
func (s *Server) UpdatePruefen(ctx context.Context) (*update.Release, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	r, err := s.updatePruefer().Neueste(ctx)
	updStand.mu.Lock()
	defer updStand.mu.Unlock()
	updStand.geprueft = time.Now()
	if err != nil {
		updStand.fehler = err.Error()
		updStand.tokenUngueltig = errors.Is(err, update.ErrTokenUngueltig)
		return nil, err
	}
	updStand.fehler = ""
	updStand.tokenUngueltig = false
	updStand.neueste = r
	_ = s.st.SetSetting(ctx, "update.zuletzt", strconv.FormatInt(time.Now().Unix(), 10))
	return r, nil
}

// updateAntwort ist, was die Verwaltung anzeigt.
func (s *Server) updateAntwort(ctx context.Context) map[string]any {
	updStand.mu.Lock()
	defer updStand.mu.Unlock()
	out := map[string]any{
		"installiert": s.version,
		"container":   updateImContainer(),
		// Updates per Knopfdruck über den Aktualisierer (aktualisierer.go).
		// Nur ob — Adresse und Token bleiben auf dem Server.
		"aktualisierer":     aktualisiererBereit(),
		"aktualisiererHalb": aktualisiererHalb(),
		"einstellung":       s.updateEinstellungen(ctx),
		"protokoll":         s.updateProtokoll(ctx),
		"laeuft":            updStand.laeuft,
		"wiedergaben":       s.live.Load().Total,
		"tokenGesetzt":      s.updatePruefer().Token != "",
		// Woher das Token kommt: aus der Umgebung (LUMEN_GITHUB_TOKEN, dann
		// lässt es sich hier nicht ändern) oder aus der Verwaltung.
		"tokenQuelle":    tokenQuelle(s),
		"tokenUngueltig": updStand.tokenUngueltig,
		"releaseSeite":   "https://github.com/" + updateRepo + "/releases",
	}
	if !updStand.geprueft.IsZero() {
		out["geprueft"] = updStand.geprueft
	} else if z := s.st.Setting(ctx, "update.zuletzt", ""); z != "" {
		if n, err := strconv.ParseInt(z, 10, 64); err == nil {
			out["geprueft"] = time.Unix(n, 0)
		}
	}
	if updStand.fehler != "" {
		out["fehler"] = updStand.fehler
	}
	if r := updStand.neueste; r != nil {
		out["neueste"] = r
		out["neuer"] = update.Neuer(r.Version, s.version)
	}
	if !updStand.verschoben.IsZero() {
		out["verschoben"] = updStand.verschoben
	}
	if !updStand.akt.Seit.IsZero() {
		out["aktualisiererStand"] = updStand.akt
	}
	return out
}

func (s *Server) adminUpdate(w http.ResponseWriter, r *http.Request) {
	ok(w, s.updateAntwort(r.Context()))
}

func (s *Server) adminUpdatePruefen(w http.ResponseWriter, r *http.Request) {
	if _, err := s.UpdatePruefen(r.Context()); err != nil {
		s.log.Warn("update", "err", err)
	}
	ok(w, s.updateAntwort(r.Context()))
}

func (s *Server) adminUpdateEinstellen(w http.ResponseWriter, r *http.Request) {
	var in struct {
		updateEinstellung
		Token *string `json:"token"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	e := in.updateEinstellung.mitVorgaben()
	for _, z := range []string{e.PruefZeit, e.FensterVon, e.FensterBis} {
		if _, err := time.Parse("15:04", z); err != nil {
			fail(w, http.StatusBadRequest, "uhrzeit bitte als HH:MM")
			return
		}
	}
	if e.Pruefen != "aus" && e.Pruefen != "taeglich" {
		fail(w, http.StatusBadRequest, "pruefen: aus oder taeglich")
		return
	}
	if e.Auto != "aus" && e.Auto != "fenster" {
		fail(w, http.StatusBadRequest, "auto: aus oder fenster")
		return
	}
	raw, _ := json.Marshal(e)
	if err := s.st.SetSetting(r.Context(), "update.einstellung", string(raw)); err != nil {
		failErr(w, err)
		return
	}
	if in.Token != nil {
		// Das Token wird gespeichert, aber nie zurückgegeben.
		_ = s.st.SetSetting(r.Context(), "update.token", strings.TrimSpace(*in.Token))
		// Gleich ausprobieren: Ein neues Token soll die Warnung sofort
		// verschwinden lassen — oder sofort sagen, dass auch dieses nicht geht.
		_, _ = s.UpdatePruefen(r.Context())
	}
	ok(w, s.updateAntwort(r.Context()))
}

// adminUpdateJetzt setzt die neueste Fassung ein. Laufen Wiedergaben,
// braucht es ?trotzdem=1 — die Verwaltung fragt vorher nach.
func (s *Server) adminUpdateJetzt(w http.ResponseWriter, r *http.Request) {
	if updateImContainer() {
		fail(w, http.StatusPreconditionFailed, "im Container aktualisiert man das Abbild: docker compose pull && docker compose up -d")
		return
	}
	if n := s.live.Load().Total; n > 0 && r.URL.Query().Get("trotzdem") != "1" {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":       tr(w, "es laufen Wiedergaben"),
			"code":        "wiedergaben_laufen",
			"wiedergaben": n,
		})
		return
	}
	updStand.mu.Lock()
	rel := updStand.neueste
	updStand.mu.Unlock()
	if rel == nil || !update.Neuer(rel.Version, s.version) {
		if r2, err := s.UpdatePruefen(r.Context()); err == nil {
			rel = r2
		}
	}
	if rel == nil || !update.Neuer(rel.Version, s.version) {
		fail(w, http.StatusPreconditionFailed, "es gibt nichts Neueres")
		return
	}
	go s.updateEinsetzen(detach(r), rel, false)
	ok(w, map[string]any{"status": "läuft", "nach": rel.Version})
}

// updateEinsetzen lädt, prüft, tauscht die Binärdatei und startet neu.
// Geht etwas schief, bleibt alles wie es war, und das Protokoll sagt warum.
func (s *Server) updateEinsetzen(ctx context.Context, rel *update.Release, auto bool) {
	updStand.mu.Lock()
	if updStand.laeuft {
		updStand.mu.Unlock()
		return
	}
	updStand.laeuft = true
	updStand.mu.Unlock()
	defer func() {
		updStand.mu.Lock()
		updStand.laeuft = false
		updStand.mu.Unlock()
	}()

	eintrag := updateEintrag{Von: s.version, Nach: rel.Version, Auto: auto}
	dir := filepath.Join(s.dataDir, "update")
	_ = os.MkdirAll(dir, 0o755)
	defer os.RemoveAll(dir)

	s.log.Info("update", "von", s.version, "nach", rel.Version, "auto", auto)
	bin, err := s.updatePruefer().Herunterladen(ctx, rel, dir)
	if err != nil {
		eintrag.Ergebnis, eintrag.Notiz = "fehler", err.Error()
		s.updateProtokollieren(ctx, eintrag)
		return
	}
	if _, err := update.Einsetzen(bin); err != nil {
		eintrag.Ergebnis, eintrag.Notiz = "fehler", err.Error()
		s.updateProtokollieren(ctx, eintrag)
		return
	}
	eintrag.Ergebnis, eintrag.Notiz = "ok", "eingesetzt, Neustart"
	s.updateProtokollieren(ctx, eintrag)
	// Kurz warten, damit die Antwort an die Verwaltung noch rausgeht.
	time.Sleep(time.Second)
	if err := s.neustarten(); err != nil {
		s.log.Error("update neustart", "err", err)
		// Mit eigenem Zusammenhang: der des Hintergrunds ist beim Neustart
		// schon beendet, der Eintrag ginge sonst verloren.
		s.updateProtokollieren(context.Background(), updateEintrag{Von: s.version, Nach: rel.Version, Auto: auto,
			Ergebnis: "fehler", Notiz: "Neustart klappte nicht — bitte Lumen von Hand neu starten: " + err.Error()})
	}
}

// UpdateWaechter läuft im Hintergrund: täglich zur eingestellten Zeit
// nachsehen, und im Fenster von selbst aktualisieren — wenn gerade
// niemand schaut. Schaut jemand, wird verschoben und später nochmal
// versucht, solange das Fenster offen ist.
func (s *Server) UpdateWaechter(ctx context.Context) {
	// Hat vor dem Neustart der Aktualisierer übernommen? Dann steht das
	// Ergebnis jetzt fest.
	s.aktualisiererNachStart(ctx)
	select {
	case <-ctx.Done():
		return
	case <-time.After(2 * time.Minute):
	}
	var zuletztGeprueft string // Tag
	var z waechterZustand
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		e := s.updateEinstellungen(ctx)
		jetzt := time.Now()
		tag := jetzt.Format("2006-01-02")
		if e.Pruefen == "taeglich" && jetzt.Format("15:04") == e.PruefZeit && zuletztGeprueft != tag {
			zuletztGeprueft = tag
			if _, err := s.UpdatePruefen(ctx); err != nil {
				s.log.Warn("update prüfen", "err", err)
			}
		}
		s.fensterSchritt(ctx, jetzt, e, &z)
	}
}

// waechterZustand merkt sich zwischen zwei Minuten des Wächters, wann
// zuletzt versucht und bis wann verschoben wurde.
type waechterZustand struct {
	zuletztVersucht time.Time
	verschobenBis   time.Time
}

// Was fensterSchritt getan hat — für die Tests.
const (
	fensterNichts     = ""
	fensterVerschoben = "verschoben"
	fensterEinsetzen  = "einsetzen"
	fensterContainer  = "container"
)

// fensterSchritt: eine Minute des Wächters im Update-Fenster. Ohne
// Container setzt Lumen die neue Fassung selbst ein; im Container bittet
// es den Aktualisierer darum — aber nur, wenn der eingerichtet ist. Schaut
// jemand, wird auf das Fenster des nächsten Tages verschoben.
func (s *Server) fensterSchritt(ctx context.Context, jetzt time.Time, e updateEinstellung, z *waechterZustand) string {
	if e.Auto != "fenster" || !imFenster(jetzt, e.FensterVon, e.FensterBis) {
		return fensterNichts
	}
	container := updateImContainer()
	if container && !aktualisiererBereit() {
		return fensterNichts
	}
	updStand.mu.Lock()
	rel := updStand.neueste
	laeuft := updStand.laeuft
	updStand.mu.Unlock()
	// Verschoben: bis zum Fenster des nächsten Tages Ruhe (20 Stunden
	// reichen, auch wenn das Fenster über Mitternacht geht).
	if laeuft || rel == nil || !update.Neuer(rel.Version, s.version) || jetzt.Before(z.verschobenBis) {
		return fensterNichts
	}
	// Höchstens alle zehn Minuten ein Versuch, sonst füllt sich das Protokoll.
	if !z.zuletztVersucht.IsZero() && jetzt.Sub(z.zuletztVersucht) < 10*time.Minute {
		return fensterNichts
	}
	z.zuletztVersucht = jetzt
	if n := s.live.Load().Total; n > 0 {
		// Wie gewünscht: nicht alle zehn Minuten neu
		// anklopfen, sondern auf das Fenster am nächsten Tag
		// verschieben — und das einmal ins Protokoll schreiben.
		updStand.mu.Lock()
		updStand.verschoben = jetzt
		updStand.mu.Unlock()
		z.verschobenBis = jetzt.Add(20 * time.Hour)
		s.updateProtokollieren(ctx, updateEintrag{Von: s.version, Nach: rel.Version, Auto: true,
			Ergebnis: "verschoben", Notiz: fmt.Sprintf("%d Wiedergabe(n) laufen — nächster Versuch morgen im Zeitfenster", n)})
		return fensterVerschoben
	}
	if container {
		// Klappt es nicht (Abbild noch nicht gebaut, Token falsch), erst
		// in der nächsten Nacht wieder — ein Fehler je Nacht im Protokoll
		// reicht.
		if code := s.aktualisiererAnstossen(ctx, rel, true); code != aktOk {
			z.verschobenBis = jetzt.Add(20 * time.Hour)
		}
		return fensterContainer
	}
	s.updateEinsetzen(ctx, rel, true)
	return fensterEinsetzen
}

// imFenster sagt, ob die Uhrzeit zwischen von und bis liegt — auch über
// Mitternacht hinweg (23:00–01:00).
func imFenster(t time.Time, von, bis string) bool {
	m := func(s string) int {
		h, _ := strconv.Atoi(s[:2])
		mi, _ := strconv.Atoi(s[3:])
		return h*60 + mi
	}
	if len(von) != 5 || len(bis) != 5 {
		return false
	}
	jetzt, a, b := t.Hour()*60+t.Minute(), m(von), m(bis)
	if a <= b {
		return jetzt >= a && jetzt < b
	}
	return jetzt >= a || jetzt < b
}

func tokenQuelle(s *Server) string {
	switch {
	case s.st.Setting(context.Background(), "update.token", "") != "":
		return "verwaltung"
	case os.Getenv("LUMEN_GITHUB_TOKEN") != "":
		return "umgebung"
	}
	return ""
}

// UpdateTokenUngueltig sagt, ob GitHub das Token zuletzt abgelehnt hat —
// für die Meldung an den Besitzer.
func (s *Server) updateTokenUngueltig() bool {
	updStand.mu.Lock()
	defer updStand.mu.Unlock()
	return updStand.tokenUngueltig
}
