// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package main

// Der eigene Player: mpv (libmpv-2.dll) in Lumen.exe.
//
// Warum: Im WebView2-<video> spielt nicht Lumen.exe den Ton, sondern
// msedgewebview2.exe — Mischer wie Logi MIXLINE, die nach Programm
// verteilen, sehen Lumen dann nicht (tonausgabe.go). Und der Browser kann
// vieles nicht selbst: AC3/E-AC3/DTS/TrueHD, MKV, HEVC mit 10 Bit, HDR,
// PGS- und ASS-Untertitel — das musste bisher das NAS umwandeln. mpv spielt
// die Originaldatei direkt vom Server.
//
// Aufbau:
//
//	Lumen-Fenster
//	├─ WebView2 (oben)      die Web-App; im Player durchsichtig, zeichnet
//	│                       nur die Bedienelemente (dieselben wie im Browser)
//	└─ Videofenster (unten) „LumenVideo", darin rendert mpv (--wid)
//
// Wem gehört der Ton? libmpv läuft im Prozess Lumen.exe; das Audiogerät
// (WASAPI) öffnet mpv also aus Lumen.exe heraus, und Windows führt die
// Audio-Sitzung unter Lumen.exe (IAudioSessionControl2::GetProcessId).
// Mit mpv.exe als Kindprozess (--wid, JSON-IPC) gehörte sie mpv.exe — genau
// das, was der Besitzer nicht will. Dazu bekommt die Sitzung den Namen
// „Lumen" (audio-client-name, und tonsitzung_windows.go setzt Name und
// Symbol noch einmal ausdrücklich).
//
// Fäden: Alles, was mpv anfasst, läuft in einer Goroutine je Wiedergabe
// (lauf.schleife) — sie wartet auf Ereignisse von mpv und führt dazwischen
// die Befehle der Web-App aus. Der Fenster-Thread ruft mpv nie auf: mpv
// hat ein eigenes Fenster im Videofenster, und wenn es das anlegt oder
// zerstört, schickt Windows dem Elternfenster Nachrichten — die muss der
// Fenster-Thread abholen können, sonst stünde alles.

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	videoKlasse = "LumenVideo"
	ebeneKlasse = "LumenEbene"
)

// Kennungen der beobachteten Eigenschaften.
const (
	eigZeit = iota + 1
	eigDauer
	eigPause
	eigEnde
	eigPuffer
	eigSpringt
	eigLautstaerke
	eigStumm
	eigSpuren
	eigGeraete
	eigTempo
	eigGeraet
)

type spieler struct {
	f      *fenster
	video  uintptr // Videofenster (Kind des Hauptfensters)
	ebene  uintptr // durchsichtiges Fenster über dem Video, darin WebView2
	ebenAn bool
	tasten bool // Medientasten angemeldet

	mu    sync.Mutex
	aktiv *lauf
	nr    int
}

// lauf ist eine Wiedergabe: eine mpv-Instanz von abspielen bis stoppen.
type lauf struct {
	s        *spieler
	nr       int
	ursprung string
	auftrag  PlayerAuftrag
	adresse  string
	kopf     string
	utAdr    string

	jobs chan func(*mpv)
	mu   sync.Mutex
	m    *mpv // nil, solange mpv nicht läuft oder schon weg ist
	zu   bool

	// Nur in der Schleife benutzt.
	spuren    []mpvSpur
	geladen   bool
	pos       float64
	dauer     float64
	gemeldet  time.Time
	ton       bool // Audio-Sitzung schon benannt
	pausiert  bool
	letzteLog []string

	// Für den Selbsttest lesbar (aus einem anderen Faden).
	zMu sync.Mutex
	z   spielStand
}

// spielStand: was mpv gerade tut — für den Selbsttest.
type spielStand struct {
	Pos        float64
	Ton, Ut    int
	Pausiert   bool
	Geladen    bool
	Ende       bool
	Fehler     string
	Lautstaerk float64
}

func (w *lauf) stand() spielStand {
	w.zMu.Lock()
	defer w.zMu.Unlock()
	return w.z
}

func (w *lauf) standSetzen(fn func(*spielStand)) {
	w.zMu.Lock()
	fn(&w.z)
	w.zMu.Unlock()
}

func neuerSpieler(f *fenster) *spieler { return &spieler{f: f} }

// mpvDa: libmpv-2.dll liegt bei und lässt sich laden.
func mpvDa() bool {
	_, err := ladeLibmpv()
	return err == nil
}

// ---------------------------------------------------------------- Videofenster

var videoKlasseDa bool

func (s *spieler) videoFenster() uintptr {
	if s.video != 0 {
		return s.video
	}
	inst, _, _ := pGetModuleHandleW.Call(0)
	if !videoKlasseDa {
		pinsel, _, _ := pCreateSolidBrush.Call(0x00000000)
		cursor, _, _ := pLoadCursorW.Call(0, idcArrow)
		wc := wndClassEx{
			Size:       uint32(unsafe.Sizeof(wndClassEx{})),
			WndProc:    windows.NewCallback(videoProc),
			Instance:   inst,
			Cursor:     cursor,
			Background: pinsel,
			ClassName:  utf16(videoKlasse),
		}
		if r, _, err := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
			log.Printf("Videofenster: RegisterClassEx: %v", err)
			return 0
		}
		videoKlasseDa = true
	}
	var r rechteck
	pGetClientRect.Call(s.f.hwnd, uintptr(unsafe.Pointer(&r)))
	h, _, err := pCreateWindowExW.Call(0,
		uintptr(unsafe.Pointer(utf16(videoKlasse))), uintptr(unsafe.Pointer(utf16("Lumen Video"))),
		wsChild|wsClipChildren|wsClipSiblings,
		0, 0, uintptr(r.Right-r.Left), uintptr(r.Bottom-r.Top),
		s.f.hwnd, 0, inst, 0)
	if h == 0 {
		log.Printf("Videofenster: CreateWindowEx: %v", err)
		return 0
	}
	s.video = h
	return h
}

func videoProc(hwnd, m, wp, lp uintptr) uintptr {
	r, _, _ := pDefWindowProcW.Call(hwnd, m, wp, lp)
	return r
}

// groesse: Das Videofenster füllt immer die ganze Fläche des Hauptfensters
// (auch im Vollbild); die Ebene mit WebView2 liegt genau darüber.
func (s *spieler) groesse() {
	if s.video == 0 {
		return
	}
	var r rechteck
	pGetClientRect.Call(s.f.hwnd, uintptr(unsafe.Pointer(&r)))
	pSetWindowPos.Call(s.video, hwndBottom, 0, 0, uintptr(r.Right-r.Left), uintptr(r.Bottom-r.Top), swpNoActivate)
	s.ebeneLage()
}

// ---------------------------------------------------------------- Ebene
//
// Durchsichtig über einem anderen Kindfenster ist WebView2 nicht: Mit
// durchsichtigem Hintergrund zeigt es, was das Elternfenster selbst
// gezeichnet hat, nicht seine Geschwister (im Selbsttest sichtbar: nur die
// Hintergrundfarbe des Hauptfensters, kein Video). Darum zieht WebView2
// während der Wiedergabe in ein eigenes Fenster um: ein randloses,
// vom Hauptfenster besessenes Fenster ohne eigene Bildfläche
// (WS_EX_NOREDIRECTIONBITMAP) genau über dessen Inhalt. Was die Seite dort
// nicht zeichnet, setzt Windows (DWM) mit dem darunterliegenden Video
// zusammen. Besessene Fenster bleiben über ihrem Besitzer, verschwinden mit
// ihm beim Minimieren und haben keinen eigenen Eintrag in der Taskleiste.

var ebeneKlasseDa bool

func (s *spieler) ebeneFenster() uintptr {
	if s.ebene != 0 {
		return s.ebene
	}
	inst, _, _ := pGetModuleHandleW.Call(0)
	if !ebeneKlasseDa {
		cursor, _, _ := pLoadCursorW.Call(0, idcArrow)
		wc := wndClassEx{
			Size:      uint32(unsafe.Sizeof(wndClassEx{})),
			WndProc:   windows.NewCallback(ebeneProc),
			Instance:  inst,
			Cursor:    cursor,
			ClassName: utf16(ebeneKlasse),
		}
		if r, _, err := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
			log.Printf("Ebene: RegisterClassEx: %v", err)
			return 0
		}
		ebeneKlasseDa = true
	}
	h, _, err := pCreateWindowExW.Call(wsExNoRedirectionBitmap,
		uintptr(unsafe.Pointer(utf16(ebeneKlasse))), uintptr(unsafe.Pointer(utf16("Lumen"))),
		wsPopup|wsClipChildren,
		0, 0, 100, 100, s.f.hwnd, 0, inst, 0)
	if h == 0 {
		log.Printf("Ebene: CreateWindowEx: %v", err)
		return 0
	}
	s.ebene = h
	return h
}

func ebeneProc(hwnd, m, wp, lp uintptr) uintptr {
	f := dasFenster
	switch m {
	case wmEraseBkgnd:
		return 1
	case wmClose:
		// Alt+F4 auf der Ebene schliesst Lumen wie das Hauptfenster.
		if f != nil {
			pPostMessageW.Call(f.hwnd, wmClose, 0, 0)
		}
		return 0
	case wmSysCommand:
		// Alt+Leertaste & Co. gehören dem Hauptfenster.
		if f != nil {
			r, _, _ := pSendMessageW.Call(f.hwnd, m, wp, lp)
			return r
		}
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, m, wp, lp)
	return r
}

// ebeneLage legt die Ebene genau über die Fläche des Hauptfensters.
func (s *spieler) ebeneLage() {
	if !s.ebenAn || s.ebene == 0 {
		return
	}
	var r rechteck
	pGetClientRect.Call(s.f.hwnd, uintptr(unsafe.Pointer(&r)))
	p := punkt{r.Left, r.Top}
	pClientToScreen.Call(s.f.hwnd, uintptr(unsafe.Pointer(&p)))
	pSetWindowPos.Call(s.ebene, 0, uintptr(p.X), uintptr(p.Y), uintptr(r.Right-r.Left), uintptr(r.Bottom-r.Top), swpNoActivate|swpNoZOrder|swpNoOwnerZOrder)
	if s.f.bereit {
		s.f.chromium.NotifyParentWindowPositionChanged()
	}
}

// ebeneSetzen zieht WebView2 in die Ebene (an) oder zurück ins Hauptfenster.
func (s *spieler) ebeneSetzen(an bool) {
	if an == s.ebenAn || !s.f.bereit {
		return
	}
	if an && s.ebeneFenster() == 0 {
		return
	}
	ctrl := com(unsafe.Pointer(s.f.chromium.GetController()))
	if an {
		s.ebenAn = true
		s.ebeneLage()
		pShowWindow.Call(s.ebene, swShow)
		if hr := comAufruf(ctrl, ctrlPutParentWindow, s.ebene); hr != 0 {
			log.Printf("Ebene: put_ParentWindow: %#x", hr)
		}
	} else {
		if hr := comAufruf(ctrl, ctrlPutParentWindow, s.f.hwnd); hr != 0 {
			log.Printf("Ebene: put_ParentWindow zurück: %#x", hr)
		}
		pShowWindow.Call(s.ebene, swHide)
		s.ebenAn = false
		pSetForegroundWindow.Call(s.f.hwnd)
	}
	// Die Grösse rechnet edge aus dem Hauptfenster — die Ebene ist genau so
	// gross, die Lage im neuen Elternfenster also dieselbe.
	s.f.chromium.Resize()
	s.f.chromium.Focus()
}

func (s *spieler) videoZeigen(an bool) {
	if an {
		if s.videoFenster() == 0 {
			return
		}
		s.groesse()
		pShowWindow.Call(s.video, swShowNoActivate)
		// Die Web-App wird durchsichtig: Was sie nicht selbst zeichnet,
		// zeigt das Videofenster darunter.
		s.f.chromium.SetBackgroundColour(0, 0, 0, 0)
		s.ebeneSetzen(true)
		s.tastenAnmelden(true)
		return
	}
	s.ebeneSetzen(false)
	s.f.chromium.SetBackgroundColour(5, 5, 7, 255)
	if s.video != 0 {
		pShowWindow.Call(s.video, swHide)
	}
	s.tastenAnmelden(false)
}

// ---------------------------------------------------------------- Medientasten
//
// Im Browser-Player meldet die Web-App ihre Wiedergabe über die Media
// Session API, und WebView2 leitet die Medientasten dorthin. Spielt mpv,
// gibt es in der Seite kein klingendes <video> — die Tasten kämen dort
// nicht an. Darum meldet die App sie selbst an, solange mpv spielt
// (RegisterHotKey; schlägt fehl, wenn ein anderes Programm sie schon
// belegt — dann bleibt WM_APPCOMMAND, solange Lumen vorn ist).

const (
	tasteAbspielen = 1
	tasteNaechste  = 2
	tasteStopp     = 3
	tasteZurueck   = 4
)

func (s *spieler) tastenAnmelden(an bool) {
	if an == s.tasten {
		return
	}
	s.tasten = an
	for id, vk := range map[uintptr]uintptr{tasteAbspielen: 0xB3, tasteNaechste: 0xB0, tasteStopp: 0xB2, tasteZurueck: 0xB1} {
		if an {
			if r, _, err := pRegisterHotKey.Call(s.f.hwnd, id, 0, vk); r == 0 {
				log.Printf("Medientaste %#x nicht angemeldet: %v", vk, err)
			}
		} else {
			pUnregisterHotKey.Call(s.f.hwnd, id)
		}
	}
}

// medientaste: WM_HOTKEY (id) oder WM_APPCOMMAND. Gibt true zurück, wenn
// sie dem Player gehört.
func (s *spieler) medientaste(art string) bool {
	if s.laufend() == nil {
		return false
	}
	s.f.Skript(playerSkript(map[string]any{"art": "taste", "taste": art}))
	return true
}

func appCommandTaste(lp uintptr) string {
	switch (lp >> 16) & 0x0FFF {
	case 14, 46, 47: // PLAY_PAUSE, PLAY, PAUSE
		return "abspielen"
	case 11:
		return "naechste"
	case 12:
		return "zurueck"
	case 13:
		return "stopp"
	}
	return ""
}

// ---------------------------------------------------------------- Abspielen

func (s *spieler) laufend() *lauf {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.aktiv
}

func playerSkript(e map[string]any) string {
	b, _ := json.Marshal(e)
	return "window.lumenDesktop&&window.lumenDesktop._player&&window.lumenDesktop._player(" + string(b) + ")"
}

// abspielen startet eine Wiedergabe (Fenster-Thread). Eine laufende endet.
func (s *spieler) abspielen(ursprung string, a PlayerAuftrag) error {
	l, err := ladeLibmpv()
	if err != nil {
		return err
	}
	a.Bereinigen()
	adresse, err := a.Adresse(ursprung)
	if err != nil {
		return err
	}
	kopf, err := a.Kopf()
	if err != nil {
		return err
	}
	utAdr, err := a.UntertitelAdresse(ursprung)
	if err != nil {
		return err
	}
	s.stoppen()
	if s.videoFenster() == 0 {
		return fmt.Errorf("kein Videofenster")
	}
	s.mu.Lock()
	s.nr++
	w := &lauf{s: s, nr: s.nr, ursprung: ursprung, auftrag: a, adresse: adresse, kopf: kopf, utAdr: utAdr,
		jobs: make(chan func(*mpv), 64), pausiert: a.Pausiert}
	s.aktiv = w
	s.mu.Unlock()
	s.videoZeigen(true)
	// Nur, was niemanden verrät: Kennungen und Art, kein Token, keine
	// Adresse mit Abfrage.
	log.Printf("Player: Wiedergabe %d startet (Datei %s, %s, ab %.1f s, libmpv %s)", w.nr, a.FileID,
		map[bool]string{true: "HLS", false: "direkt"}[a.HLS], a.Start, l.fassung())
	go w.schleife(l, s.f.hwnd)
	return nil
}

// stoppen beendet die laufende Wiedergabe (Fenster-Thread). Das Fenster
// geht sofort weg; mpv räumt danach in seiner Goroutine auf.
func (s *spieler) stoppen() {
	s.mu.Lock()
	w := s.aktiv
	s.aktiv = nil
	s.mu.Unlock()
	if w == nil {
		return
	}
	w.schicke(func(m *mpv) { _ = m.befehl("quit") })
	w.mu.Lock()
	if w.m == nil {
		w.zu = true // mpv lief noch gar nicht: die Schleife hört von selbst auf
	}
	w.mu.Unlock()
	s.videoZeigen(false)
	log.Printf("Player: Wiedergabe %d endet", w.nr)
}

// befehl kommt von der Web-App (Fenster-Thread).
func (s *spieler) befehl(ursprung string, b PlayerBefehl) error {
	w := s.laufend()
	if w == nil {
		return codeFehler{fehlerUnbekannt}
	}
	if w.ursprung != ursprung {
		return codeFehler{fehlerNichtErlaub}
	}
	switch b.Art {
	case "stopp":
		s.stoppen()
	case "pause":
		an := b.schalter()
		w.schicke(func(m *mpv) { _ = m.setzenSchalter("pause", an) })
	case "springen":
		sek, ok := b.zahl()
		if !ok {
			return codeFehler{fehlerUnbekannt}
		}
		w.schicke(func(m *mpv) {
			if err := m.befehl("seek", strconv.FormatFloat(sek, 'f', 3, 64), "absolute+exact"); err != nil {
				log.Printf("Player: %v", err)
			}
		})
	case "lautstaerke":
		v, ok := b.zahl()
		if !ok || v < 0 || v > 1 {
			return codeFehler{fehlerUnbekannt}
		}
		w.schicke(func(m *mpv) { _ = m.setzenKomma("volume", v*100) })
	case "stumm":
		an := b.schalter()
		w.schicke(func(m *mpv) { _ = m.setzenSchalter("mute", an) })
	case "tempo":
		v, ok := b.zahl()
		if !ok || v < 0.25 || v > 4 {
			return codeFehler{fehlerUnbekannt}
		}
		w.schicke(func(m *mpv) { _ = m.setzenKomma("speed", v) })
	case "ton":
		idx := b.Index
		w.schicke(func(m *mpv) { w.tonWaehlen(m, idx) })
	case "untertitel":
		adr := ""
		if b.Pfad != "" {
			a, err := streamAdresse(w.ursprung, b.Pfad)
			if err != nil {
				return err
			}
			adr = a
		}
		idx := b.Index
		w.schicke(func(m *mpv) { w.untertitelWaehlen(m, idx, adr) })
	case "tonausgabe":
		g := b.text()
		if !geraetMuster.MatchString(g) {
			return codeFehler{fehlerUnbekannt}
		}
		if g == "" {
			g = "auto"
		}
		w.schicke(func(m *mpv) {
			if err := m.setzen("audio-device", g); err != nil {
				log.Printf("Player: %v", err)
			}
			w.ton = false // neues Gerät, neue Sitzung: wieder benennen
		})
	case "geraete":
		w.schicke(func(m *mpv) {
			if t, ok := m.lesen("audio-device-list"); ok {
				w.melden(map[string]any{"art": "geraete", "liste": geraeteLesen(t)})
			}
		})
	default:
		return codeFehler{fehlerUnbekannt}
	}
	return nil
}

// schicke reiht einen Befehl für mpv ein (aus jedem Faden).
func (w *lauf) schicke(fn func(*mpv)) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.zu {
		return
	}
	select {
	case w.jobs <- fn:
	default:
		log.Printf("Player: Befehl verworfen (Warteschlange voll)")
		return
	}
	if w.m != nil {
		w.m.wecken()
	}
}

// melden schickt ein Ereignis an die Web-App — nur, solange dieser Lauf
// der aktuelle ist.
func (w *lauf) melden(e map[string]any) {
	if w.s.laufend() != w {
		return
	}
	e["lauf"] = w.nr
	js := playerSkript(e)
	w.s.f.Auftrag(func() {
		if w.s.laufend() == w {
			w.s.f.Skript(js)
		}
	})
}

func (w *lauf) aktuell() bool { return w.s.laufend() == w }

// vorpruefen fragt den Server einmal selbst, bevor mpv lädt: Sagt er nein
// (Bildschirmzeit, zu viele Geräte, Datei weg), steht sein Satz in der
// Web-App — mpv meldete nur „HTTP error 429". Nur für den direkten Strom;
// eine HLS-Anfrage würde schon eine Umwandlung starten.
func (w *lauf) vorpruefen() (string, bool) {
	if w.auftrag.HLS {
		return "", true
	}
	req, err := http.NewRequest("GET", w.adresse, nil)
	if err != nil {
		return "", true
	}
	req.Header.Set("Authorization", "Bearer "+w.auftrag.Token)
	req.Header.Set("Range", "bytes=0-0")
	c := http.Client{Timeout: 15 * time.Second}
	res, err := c.Do(req)
	if err != nil {
		return "", true // mpv soll es selbst versuchen und melden
	}
	defer res.Body.Close()
	if res.StatusCode < 400 {
		io.Copy(io.Discard, io.LimitReader(res.Body, 64))
		return "", true
	}
	var f struct {
		Error string `json:"error"`
		Code  string `json:"code"`
	}
	_ = json.NewDecoder(io.LimitReader(res.Body, 16<<10)).Decode(&f)
	log.Printf("Player: Server sagt %d (%s)", res.StatusCode, f.Code)
	if f.Error == "" {
		f.Error = res.Status
	}
	w.melden(map[string]any{"art": "fehler", "text": f.Error, "code": f.Code, "status": res.StatusCode})
	return f.Error, false
}

// schleife besitzt mpv: anlegen, laden, Ereignisse, Befehle, beenden.
func (w *lauf) schleife(l *libmpv, eltern uintptr) {
	// Sagt der Server nein, bleibt das Fenster stehen, bis die Web-App den
	// Player schliesst (sie zeigt den Satz des Servers).
	if _, ok := w.vorpruefen(); !ok {
		w.aufraeumen()
		return
	}
	if !w.aktuell() {
		w.aufraeumen()
		return
	}
	m, err := w.mpvAnlegen(l)
	if err != nil {
		// libmpv lässt sich nicht starten: Die Web-App nimmt ihren eigenen
		// Player (kaputt).
		log.Printf("Player: %v", err)
		w.melden(map[string]any{"art": "fehler", "text": err.Error(), "kaputt": true})
		w.aufraeumen()
		return
	}
	w.mu.Lock()
	if w.zu {
		// Schon wieder gestoppt, bevor mpv lief.
		w.mu.Unlock()
		m.beenden()
		w.aufraeumen()
		return
	}
	w.m = m
	w.mu.Unlock()

	w.laden(m, w.auftrag.Start, w.auftrag.Pausiert)
	for {
		w.jobsAbarbeiten(m)
		e := m.warten(-1)
		if e == nil {
			continue
		}
		switch e.ID {
		case mpvEreignisKeins:
		case mpvEreignisEnde:
			w.mu.Lock()
			w.zu = true
			w.m = nil
			w.mu.Unlock()
			m.beenden()
			log.Printf("Player: mpv %d beendet", w.nr)
			// Ohne „stoppen" ausgestiegen: Die Web-App schliesst den Player.
			if w.aktuell() {
				w.melden(map[string]any{"art": "geschlossen"})
			}
			w.aufraeumen()
			return
		case mpvEreignisLog:
			z := (*mpvLogZeile)(e.Daten)
			text := strings.TrimSpace(windows.BytePtrToString(z.Text))
			zeile := fmt.Sprintf("mpv[%s] %s: %s", windows.BytePtrToString(z.Stufe), windows.BytePtrToString(z.Vorsilbe), text)
			log.Print(zeile)
			w.letzteLog = append(w.letzteLog, text)
			if len(w.letzteLog) > 8 {
				w.letzteLog = w.letzteLog[1:]
			}
		case mpvEreignisDateiGeladen:
			w.dateiGeladen(m)
		case mpvEreignisNeustart:
			w.melden(map[string]any{"art": "bereit"})
			w.sitzungBenennen()
		case mpvEreignisDateiEnde:
			d := (*mpvDateiEnde)(e.Daten)
			if d.Grund == mpvEndeFehler {
				text := l.fehlerText(d.Fehler)
				log.Printf("Player: Datei nicht abspielbar: %s", text)
				w.standSetzen(func(z *spielStand) { z.Fehler = text })
				w.melden(map[string]any{"art": "fehler", "text": text, "mpv": w.letzteLog})
			} else if d.Grund == mpvEndeSchluss {
				w.melden(map[string]any{"art": "ende"})
			}
		case mpvEreignisEigenschaft:
			w.eigenschaft(m, e.Antwort, (*mpvEigenschaft)(e.Daten))
		}
	}
}

func (w *lauf) aufraeumen() {
	w.mu.Lock()
	w.zu = true
	w.mu.Unlock()
}

func (w *lauf) jobsAbarbeiten(m *mpv) {
	for {
		select {
		case fn := <-w.jobs:
			fn(m)
		default:
			return
		}
	}
}

func (w *lauf) mpvAnlegen(l *libmpv) (*mpv, error) {
	m, err := neuesMpv(l)
	if err != nil {
		return nil, err
	}
	a := w.auftrag
	pflicht := [][2]string{
		{"wid", strconv.FormatUint(uint64(w.s.video), 10)},
		// Nur abspielen: keine eigene Steuerung, keine Tasten, keine
		// Skripte, keine Einstellungsdateien. Die Bedienung macht die Web-App.
		{"config", "no"},
		{"terminal", "no"},
		{"osc", "no"},
		{"osd-level", "0"},
		{"input-default-bindings", "no"},
		{"input-vo-keyboard", "no"},
		{"input-cursor", "no"},
		{"input-media-keys", "no"},
		{"cursor-autohide", "no"},
		{"load-scripts", "no"},
		{"ytdl", "no"},
		{"idle", "yes"},
		{"keep-open", "yes"},
		{"force-window", "no"},
		{"taskbar-progress", "no"},
		// Nichts neben der Datei suchen (über http sinnlos und langsam).
		{"sub-auto", "no"},
		{"audio-file-auto", "no"},
		// Ton: im Prozess Lumen.exe, als „Lumen" im Mixer.
		{"audio-client-name", "Lumen"},
		{"title", "Lumen"},
		{"force-media-title", "Lumen"},
		// Netz: das Token im Kopf, nie in der Adresse.
		{"http-header-fields", w.kopf},
		{"user-agent", "Lumen/" + version + " (Windows; mpv)"},
		{"network-timeout", "30"},
		{"cache", "yes"},
		// Bild: Hardware-Dekodierung, wo sie sicher ist (HEVC, 10 Bit, 4K).
		{"hwdec", "auto-safe"},
		{"volume", strconv.FormatFloat(a.Lautstaerke*100, 'f', 1, 64)},
		{"volume-max", "100"},
		{"mute", map[bool]string{true: "yes", false: "no"}[a.Stumm]},
		{"speed", strconv.FormatFloat(a.Tempo, 'f', 3, 64)},
		{"sid", "no"},
	}
	if a.Tonausgabe != "" {
		pflicht = append(pflicht, [2]string{"audio-device", a.Tonausgabe})
	}
	for _, o := range pflicht {
		if err := m.option(o[0], o[1]); err != nil {
			// Ältere libmpv kennen nicht jede Option — weiter, aber merken.
			// (Den Kopf nicht ins Protokoll: o[1] bleibt weg.)
			log.Printf("Player: Option %s: %v", o[0], err)
		}
	}
	for _, o := range [][2]string{{"load-stats-overlay", "no"}, {"load-console", "no"}, {"load-auto-profiles", "no"},
		{"load-select", "no"}, {"load-commands", "no"}, {"load-context-menu", "no"}, {"load-positioning", "no"}} {
		_ = m.option(o[0], o[1])
	}
	if err := m.starten(); err != nil {
		m.beenden()
		return nil, err
	}
	stufe := "warn"
	if w.s.f.test != nil {
		stufe = "info"
	}
	m.protokoll(stufe)
	for _, b := range []struct {
		id     uint64
		name   string
		format int
	}{
		{eigZeit, "time-pos", mpvFormatKomma}, {eigDauer, "duration", mpvFormatKomma},
		{eigPause, "pause", mpvFormatSchalt}, {eigEnde, "eof-reached", mpvFormatSchalt},
		{eigPuffer, "paused-for-cache", mpvFormatSchalt}, {eigSpringt, "seeking", mpvFormatSchalt},
		{eigLautstaerke, "volume", mpvFormatKomma}, {eigStumm, "mute", mpvFormatSchalt},
		{eigSpuren, "track-list", mpvFormatText}, {eigGeraete, "audio-device-list", mpvFormatText},
		{eigTempo, "speed", mpvFormatKomma}, {eigGeraet, "audio-device", mpvFormatText},
	} {
		if err := m.beobachten(b.id, b.name, b.format); err != nil {
			log.Printf("Player: %v", err)
		}
	}
	return m, nil
}

// laden lädt die Adresse ab einer Stelle. Erst angehalten: Ton und
// Untertitel werden gewählt, sobald mpv die Spuren kennt (dateiGeladen),
// dann geht es los.
func (w *lauf) laden(m *mpv, ab float64, pausiert bool) {
	w.geladen = false
	w.pausiert = pausiert
	start := "none"
	if ab > 0 {
		start = strconv.FormatFloat(ab, 'f', 3, 64)
	}
	_ = m.setzen("start", start)
	_ = m.setzenSchalter("pause", true)
	if err := m.befehl("loadfile", w.adresse, "replace"); err != nil {
		log.Printf("Player: %v", err)
		w.melden(map[string]any{"art": "fehler", "text": err.Error()})
	}
}

func (w *lauf) dateiGeladen(m *mpv) {
	if t, ok := m.lesen("track-list"); ok {
		w.spuren = spurenLesen(t)
	}
	a := w.auftrag
	if a.Ton >= 0 {
		w.tonWaehlen(m, a.Ton)
	}
	w.untertitelWaehlen(m, a.Untertitel, w.utAdr)
	if !w.pausiert {
		_ = m.setzenSchalter("pause", false)
	}
	w.geladen = true
	w.standSetzen(func(z *spielStand) { z.Geladen = true; z.Pausiert = w.pausiert })
	d, _ := m.lesenKomma("duration")
	w.melden(map[string]any{"art": "geladen", "dauer": d, "pausiert": w.pausiert,
		"ton": gewaehlteSpur(w.spuren, "audio"), "untertitel": gewaehlteSpur(w.spuren, "sub")})
	log.Printf("Player: Datei geladen (%.0f s, %d Spuren)", d, len(w.spuren))
}

func (w *lauf) tonWaehlen(m *mpv, idx int) {
	if w.auftrag.HLS {
		// In HLS steckt nur die gewählte Tonspur — die Web-App lädt neu.
		return
	}
	if id, ok := mpvKennung(w.spuren, "audio", idx); ok {
		if err := m.setzen("aid", strconv.Itoa(id)); err != nil {
			log.Printf("Player: %v", err)
		}
	} else if idx >= 0 {
		log.Printf("Player: Tonspur %d nicht gefunden", idx)
	}
	w.auftrag.Ton = idx
}

func (w *lauf) untertitelWaehlen(m *mpv, idx int, adr string) {
	w.auftrag.Untertitel = idx
	if adr != "" {
		if id, ok := externKennung(w.spuren, adr); ok {
			_ = m.setzen("sid", strconv.Itoa(id))
			return
		}
		// Die Datei neben dem Film holt mpv selbst (mit demselben Kopf).
		if err := m.befehl("sub-add", adr, "select"); err != nil {
			log.Printf("Player: %v", err)
		}
		return
	}
	if idx < 0 {
		_ = m.setzen("sid", "no")
		return
	}
	if id, ok := mpvKennung(w.spuren, "sub", idx); ok {
		if err := m.setzen("sid", strconv.Itoa(id)); err != nil {
			log.Printf("Player: %v", err)
		}
		return
	}
	log.Printf("Player: Untertitel %d nicht gefunden", idx)
	_ = m.setzen("sid", "no")
}

func (w *lauf) eigenschaft(m *mpv, id uint64, e *mpvEigenschaft) {
	if e == nil {
		return
	}
	komma := func() (float64, bool) {
		if e.Format != mpvFormatKomma || e.Daten == nil {
			return 0, false
		}
		return *(*float64)(e.Daten), true
	}
	schalter := func() (bool, bool) {
		if e.Format != mpvFormatSchalt || e.Daten == nil {
			return false, false
		}
		return *(*int32)(e.Daten) != 0, true
	}
	text := func() (string, bool) {
		if e.Format != mpvFormatText || e.Daten == nil {
			return "", false
		}
		return goText(*(*unsafe.Pointer)(e.Daten)), true
	}
	switch id {
	case eigZeit:
		if v, ok := komma(); ok {
			w.pos = v
			w.standSetzen(func(z *spielStand) { z.Pos = v })
			// Ein Bild nach dem anderen meldet mpv die Zeit — die Web-App
			// braucht sie viermal pro Sekunde.
			if time.Since(w.gemeldet) >= 250*time.Millisecond {
				w.gemeldet = time.Now()
				w.melden(map[string]any{"art": "zeit", "pos": v, "dauer": w.dauer})
			}
		}
	case eigDauer:
		if v, ok := komma(); ok {
			w.dauer = v
			w.melden(map[string]any{"art": "dauer", "dauer": v})
		}
	case eigPause:
		if v, ok := schalter(); ok && w.geladen {
			w.pausiert = v
			w.standSetzen(func(z *spielStand) { z.Pausiert = v })
			w.melden(map[string]any{"art": "pause", "wert": v, "pos": w.pos})
		}
	case eigEnde:
		if v, ok := schalter(); ok && v {
			w.standSetzen(func(z *spielStand) { z.Ende = true })
			w.melden(map[string]any{"art": "ende", "pos": w.pos})
		}
	case eigPuffer, eigSpringt:
		if v, ok := schalter(); ok {
			w.melden(map[string]any{"art": "laedt", "wert": v})
			if id == eigSpringt && !v {
				w.melden(map[string]any{"art": "gesprungen", "pos": w.pos})
			}
		}
	case eigLautstaerke:
		if v, ok := komma(); ok {
			w.standSetzen(func(z *spielStand) { z.Lautstaerk = v / 100 })
			w.melden(map[string]any{"art": "lautstaerke", "wert": v / 100})
		}
	case eigStumm:
		if v, ok := schalter(); ok {
			w.melden(map[string]any{"art": "stumm", "wert": v})
		}
	case eigTempo:
		if v, ok := komma(); ok {
			w.melden(map[string]any{"art": "tempo", "wert": v})
		}
	case eigSpuren:
		if t, ok := text(); ok {
			w.spuren = spurenLesen(t)
			ton, ut := gewaehlteSpur(w.spuren, "audio"), gewaehlteSpur(w.spuren, "sub")
			w.standSetzen(func(z *spielStand) { z.Ton, z.Ut = ton, ut })
			w.melden(map[string]any{"art": "spuren", "ton": gewaehlteSpur(w.spuren, "audio"), "untertitel": gewaehlteSpur(w.spuren, "sub")})
		}
	case eigGeraete:
		if t, ok := text(); ok {
			w.melden(map[string]any{"art": "geraete", "liste": geraeteLesen(t)})
		}
	case eigGeraet:
		if t, ok := text(); ok {
			w.melden(map[string]any{"art": "geraet", "wert": t})
		}
	}
}

// sitzungBenennen: Sobald mpv das Audiogerät geöffnet hat, die Sitzung von
// Lumen.exe noch einmal ausdrücklich „Lumen" nennen und ihr das Symbol von
// Lumen.exe geben (mpv setzt ein Symbol, das es nur in mpv.exe gibt).
func (w *lauf) sitzungBenennen() {
	if w.ton {
		return
	}
	w.ton = true
	go func() {
		n, err := tonsitzungenBenennen("Lumen")
		if err != nil {
			log.Printf("Audio-Sitzung: %v", err)
			return
		}
		log.Printf("Audio-Sitzung: %d Sitzung(en) von Lumen.exe benannt", n)
	}()
}
