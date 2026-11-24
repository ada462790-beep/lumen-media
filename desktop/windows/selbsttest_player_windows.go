// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package main

// Selbsttest des eigenen Players (player_windows.go): Der Schein-Server
// liefert einen kleinen Testfilm (MKV mit HEVC 10 Bit, AC3 5.1 und Stereo,
// zwei ASS-Untertiteln — im CI von FFmpeg erzeugt) wie Lumen aus; die echte
// Web-App startet ihn, mpv spielt ihn im Lumen-Fenster. Geprüft wird, was
// der Besitzer sieht und hört: Bild unter den Bedienelementen, laufende
// Zeit, Spurwechsel, Fortschritt beim Server, Esc zurück, „gesehen" am Ende
// — und welchem Prozess die Audio-Sitzung gehört.

import (
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unsafe"
)

var (
	pFindWindowExW    = user32.NewProc("FindWindowExW")
	pIsWindowVisible  = user32.NewProc("IsWindowVisible")
	pGetWindowRect    = user32.NewProc("GetWindowRect")
	pKeybdEvent       = user32.NewProc("keybd_event")
	pGetClassNameW    = user32.NewProc("GetClassNameW")
	pGetForegroundWnd = user32.NewProc("GetForegroundWindow")
)

// scheinPlayer zählt, was beim Schein-Server ankommt.
type scheinPlayer struct {
	mu            sync.Mutex
	strom         int // Anfragen an den Strom
	mitKopf       int // … mit „Authorization: Bearer selbsttest"
	ohneKopf      int
	mitSchluessel int // … mit key= in der Adresse (soll nie vorkommen)
	bereiche      int // … mit Range
	plaene        []string
	staende       []string
	gesehen       int
}

const (
	filmID  = "film1"
	dateiID = "datei1"
)

func (t *selbsttest) playerRouten(mux *http.ServeMux, angemeldet func(*http.Request) bool) {
	antwort := func(w http.ResponseWriter, status int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	datei := func() map[string]any {
		var groesse int64
		if st, err := os.Stat(t.film); err == nil {
			groesse = st.Size()
		}
		return map[string]any{
			"id": dateiID, "itemId": filmID, "path": "/filme/Testfilm (2026)/Testfilm (2026).mkv", "sizeBytes": groesse,
			"partNo": 1, "container": "mkv", "durationMs": 60000, "bitrate": 2000000, "width": 1280, "height": 720,
			"videoCodec": "hevc",
			"tracks": []map[string]any{
				{"index": 1, "kind": "audio", "codec": "ac3", "language": "de", "channels": 6},
				{"index": 2, "kind": "audio", "codec": "ac3", "language": "en", "channels": 2},
				{"index": 3, "kind": "subtitle", "codec": "ass", "language": "de"},
				{"index": 4, "kind": "subtitle", "codec": "ass", "language": "en"},
			},
			"chapters": []map[string]any{
				{"index": 0, "startMs": 0, "endMs": 20000, "title": "Anfang", "kind": "normal"},
				{"index": 1, "startMs": 20000, "endMs": 40000, "title": "Mitte", "kind": "normal"},
				{"index": 2, "startMs": 40000, "endMs": 60000, "title": "Schluss", "kind": "normal"},
			},
		}
	}
	mux.HandleFunc("/api/v1/items/"+filmID, func(w http.ResponseWriter, r *http.Request) {
		if !angemeldet(r) {
			antwort(w, 401, map[string]string{"error": "nicht angemeldet"})
			return
		}
		antwort(w, 200, map[string]any{
			"item": map[string]any{"id": filmID, "kind": "movie", "title": "Testfilm", "year": 2026, "runtimeMs": 60000,
				"overview": "Ein Film aus Testbild und Sinuston, erzeugt von FFmpeg.", "genres": []string{}},
			"files": []any{datei()},
		})
	})
	mux.HandleFunc("/api/v1/items/"+filmID+"/", func(w http.ResponseWriter, r *http.Request) {
		if !angemeldet(r) {
			antwort(w, 401, map[string]string{"error": "nicht angemeldet"})
			return
		}
		rest := strings.TrimPrefix(r.URL.Path, "/api/v1/items/"+filmID+"/")
		switch {
		case rest == "progress" && r.Method == http.MethodPost:
			var b struct {
				PositionMs int64 `json:"positionMs"`
				DurationMs int64 `json:"durationMs"`
			}
			_ = json.NewDecoder(r.Body).Decode(&b)
			t.srv.mu.Lock()
			t.srv.staende = append(t.srv.staende, fmt.Sprintf("%d/%d", b.PositionMs, b.DurationMs))
			t.srv.mu.Unlock()
			log.Printf("Selbsttest: Fortschritt %d von %d ms", b.PositionMs, b.DurationMs)
			w.Header().Set("X-Lumen-Stand", "1")
			antwort(w, 200, map[string]any{})
		case rest == "finished" && r.Method == http.MethodPost:
			t.srv.mu.Lock()
			t.srv.gesehen++
			t.srv.mu.Unlock()
			log.Printf("Selbsttest: als gesehen gemeldet")
			w.Header().Set("X-Lumen-Stand", "2")
			antwort(w, 200, map[string]any{})
		default:
			antwort(w, 404, map[string]string{"error": "gibt es im Selbsttest nicht"})
		}
	})
	// Der Strom: die Datei selbst, mit Bereichsanfragen — wie streamFile.
	mux.HandleFunc("/api/v1/stream/"+dateiID, func(w http.ResponseWriter, r *http.Request) {
		t.srv.mu.Lock()
		t.srv.strom++
		if r.URL.Query().Has("key") || r.URL.Query().Has("token") {
			t.srv.mitSchluessel++
		}
		if r.Header.Get("Range") != "" {
			t.srv.bereiche++
		}
		ok := angemeldet(r)
		if ok {
			t.srv.mitKopf++
		} else {
			t.srv.ohneKopf++
		}
		t.srv.mu.Unlock()
		if !ok {
			antwort(w, 401, map[string]string{"error": "nicht angemeldet"})
			return
		}
		f, err := os.Open(t.film)
		if err != nil {
			antwort(w, 410, map[string]string{"error": "die datei ist nicht mehr da"})
			return
		}
		defer f.Close()
		st, _ := f.Stat()
		w.Header().Set("Content-Type", "video/x-matroska")
		w.Header().Set("Accept-Ranges", "bytes")
		http.ServeContent(w, r, "Testfilm.mkv", st.ModTime(), f)
	})
	mux.HandleFunc("/api/v1/stream/"+dateiID+"/", func(w http.ResponseWriter, r *http.Request) {
		if !angemeldet(r) {
			antwort(w, 401, map[string]string{"error": "nicht angemeldet"})
			return
		}
		switch strings.TrimPrefix(r.URL.Path, "/api/v1/stream/"+dateiID+"/") {
		case "plan":
			// Direkt nur, wenn das Gerät HEVC und AC3 meldet — so sieht der
			// Test, dass die Web-App mit den Fähigkeiten von mpv fragt.
			q := r.URL.Query()
			direkt := strings.Contains(","+q.Get("codecs")+",", ",hevc,") && strings.Contains(","+q.Get("audio")+",", ",ac3,")
			t.srv.mu.Lock()
			t.srv.plaene = append(t.srv.plaene, fmt.Sprintf("direkt=%v", direkt))
			t.srv.mu.Unlock()
			antwort(w, 200, map[string]any{"plan": map[string]any{"direct": direkt, "videoCodec": "hevc", "audioCodec": "copy", "height": 720, "bitrate": 2000000}, "file": datei()})
		case "browser/grund":
			antwort(w, 200, map[string]string{"code": "unbekannt"})
		default:
			antwort(w, 404, map[string]string{"error": "gibt es im Selbsttest nicht"})
		}
	})
}

func (t *selbsttest) warte(frist time.Duration, fn func() bool) bool {
	ende := time.Now().Add(frist)
	for time.Now().Before(ende) {
		if fn() {
			return true
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fn()
}

func (t *selbsttest) taste(vk uintptr) {
	t.ui(func() {
		pSetForegroundWindow.Call(t.f.hwnd)
		pKeybdEvent.Call(vk, 0, 0, 0)
		pKeybdEvent.Call(vk, 0, 2, 0) // KEYEVENTF_KEYUP
	})
}

func (t *selbsttest) abspielenKlicken() {
	// Wie ein Knopf „Abspielen" der Web-App (data-play).
	t.skript(`(()=>{const b=document.createElement('button');b.dataset.play='` + filmID + `';b.hidden=true;document.getElementById('app').appendChild(b);b.click();b.remove()})()`)
}

func (t *selbsttest) eigenerPlayer(server string) {
	if t.film == "" {
		t.notiz(true, "kein Testfilm angegeben — eigener Player nicht geprüft")
		return
	}
	if !t.notiz(mpvDa(), "libmpv geladen (%v)", libmpvFehler) {
		return
	}
	t.notiz(true, "libmpv %s aus %s", dieLibmpv.fassung(), dieLibmpv.pfad)

	t.skript(`localStorage.setItem('lumen.token','selbsttest');localStorage.removeItem('lumen.tonausgabe')`)
	// Mit Abfrage: sonst wäre es dieselbe Adresse wie eben (Einstellungen),
	// und WebView2 lüde nicht neu.
	t.ui(func() { t.f.Navigieren(server + "/?player#/ich") })
	if !t.notiz(t.warteGeladen(server+"/?player"), "Web-App angemeldet geladen") {
		return
	}
	time.Sleep(2500 * time.Millisecond)
	t.notiz(t.frage(`window.lumenDesktop.faehigkeiten.player`) == "mpv", "Brücke meldet player: mpv")

	t.abspielenKlicken()
	var w *lauf
	ok := t.warte(30*time.Second, func() bool {
		w = t.f.spieler.laufend()
		return w != nil && w.stand().Geladen && w.stand().Pos > 1.5
	})
	if w == nil {
		w = t.f.spieler.laufend()
	}
	if !t.notiz(ok, "mpv spielt den Film (%s)", t.standText(w)) {
		t.bildschirmWarte("9-mpv-fehler.png")
		return
	}

	// Eingebettet: Im Videofenster (Kind des Lumen-Fensters) liegt das
	// Fenster von mpv.
	var kind, sichtbar uintptr
	var klasse string
	t.ui(func() {
		kind, _, _ = pFindWindowExW.Call(t.f.spieler.video, 0, 0, 0)
		sichtbar, _, _ = pIsWindowVisible.Call(t.f.spieler.video)
		if kind != 0 {
			buf := make([]uint16, 128)
			pGetClassNameW.Call(kind, uintptr(unsafe.Pointer(&buf[0])), 128)
			klasse = windowsText(buf)
		}
	})
	t.notiz(kind != 0 && sichtbar != 0, "mpv zeichnet im Lumen-Fenster (Videofenster sichtbar, darin %q)", klasse)

	// Zum Nachsehen im Protokoll: Hintergründe der Seite im Player.
	log.Printf("Selbsttest: Hintergründe %s", t.frage(`['html','body','#app','.player','#v'].map(s=>{const e=document.querySelector(s);return s+'='+(e?getComputedStyle(e).backgroundColor:'-')}).join(' ')+' mpv-spielt='+document.documentElement.classList.contains('mpv-spielt')`))
	var ebene uintptr
	t.ui(func() { ebene, _, _ = pIsWindowVisible.Call(t.f.spieler.ebene) })
	t.notiz(ebene != 0, "Bedienelemente in der durchsichtigen Ebene über dem Video")

	p1 := w.stand().Pos
	time.Sleep(2 * time.Second)
	p2 := w.stand().Pos
	t.notiz(p2 > p1+1, "Position läuft (%.1f → %.1f s)", p1, p2)
	t.notiz(t.frage(`(()=>{const v=document.querySelector('#v');return v&&v.currentTime>1&&v.duration>50?'ja':'nein '+(v&&v.currentTime)+' '+(v&&v.duration)})()`) == "ja",
		"Web-App kennt Zeit und Länge von mpv")

	t.srv.mu.Lock()
	strom, mitKopf, ohneKopf, mitSchl, bereiche, plaene := t.srv.strom, t.srv.mitKopf, t.srv.ohneKopf, t.srv.mitSchluessel, t.srv.bereiche, strings.Join(t.srv.plaene, ",")
	t.srv.mu.Unlock()
	t.notiz(strings.Contains(plaene, "direkt=true"), "Plan mit den Fähigkeiten von mpv: direkt (%s)", plaene)
	t.notiz(strom > 0 && mitKopf == strom && ohneKopf == 0 && mitSchl == 0,
		"Originaldatei direkt, Token nur im Kopf (%d Anfragen, %d mit Range, %d ohne Kopf, %d mit key=)", strom, bereiche, ohneKopf, mitSchl)

	// Vorbelegt nach den Regeln der Web-App: Ton in der Sprache des
	// Profils (de, Spur 1), Untertitel „de" (Spur 3, so im Profil).
	z := w.stand()
	t.notiz(z.Ton == 1 && z.Ut == 3, "Spuren nach den Regeln der Web-App (Ton %d, Untertitel %d)", z.Ton, z.Ut)

	// Spurwechsel über die Bedienelemente.
	t.skript(`document.querySelector('[data-spuren="ton"]')?.click()`)
	time.Sleep(700 * time.Millisecond)
	t.bildschirmWarte("9-mpv-tonspuren.png")
	t.skript(`document.querySelector('[data-spur="2"]')?.click()`)
	t.notiz(t.warte(6*time.Second, func() bool { return w.stand().Ton == 2 }), "Tonspur gewechselt, ohne neu zu laden (jetzt %d)", w.stand().Ton)
	t.skript(`document.querySelector('[data-spuren="untertitel"]')?.click()`)
	time.Sleep(500 * time.Millisecond)
	t.skript(`document.querySelector('[data-untertitel="4"]')?.click()`)
	t.notiz(t.warte(6*time.Second, func() bool { return w.stand().Ut == 4 }), "Untertitel gewechselt (jetzt %d)", w.stand().Ut)
	time.Sleep(1500 * time.Millisecond)

	// Pause: Die Bedienelemente bleiben stehen, der Stand geht an den Server.
	t.srv.mu.Lock()
	vorher := len(t.srv.staende)
	t.srv.mu.Unlock()
	t.skript(`document.querySelector('[data-toggle]')?.click()`)
	t.notiz(t.warte(6*time.Second, func() bool { return w.stand().Pausiert }), "Pause über die Bedienelemente")
	t.notiz(t.warte(6*time.Second, func() bool {
		t.srv.mu.Lock()
		defer t.srv.mu.Unlock()
		return len(t.srv.staende) > vorher
	}), "Fortschritt bei Pause an den Server gemeldet (%s)", t.letzterStand())
	time.Sleep(1200 * time.Millisecond)
	if img := t.bildschirmBild("10-mpv-pause.png"); img != nil {
		anteil := t.bunterAnteil(img)
		t.notiz(anteil > 0.08, "Bild von mpv unter den Bedienelementen sichtbar (%.0f %% bunte Punkte in der Bildmitte)", anteil*100)
	}

	// Vollbild (F11): Das Videofenster wächst mit.
	t.ui(t.f.f11)
	time.Sleep(1500 * time.Millisecond)
	var vr, mr rechteck
	t.ui(func() {
		pGetWindowRect.Call(t.f.spieler.video, uintptr(unsafe.Pointer(&vr)))
		if mi, ok := monitorVon(t.f.hwnd); ok {
			mr = mi.Monitor
		}
	})
	t.notiz(t.f.vollbild && vr == mr, "Vollbild: Video füllt den Bildschirm (%v, Bildschirm %v)", vr, mr)
	if img := t.bildschirmBild("11-mpv-vollbild.png"); img != nil {
		t.notiz(t.bunterAnteil(img) > 0.08, "Bild auch im Vollbild sichtbar")
	}
	t.ui(t.f.f11)
	time.Sleep(800 * time.Millisecond)

	// Tonausgabe: Die Geräte nennt mpv (Zahnrad).
	t.skript(`document.querySelector('[data-spuren="qualitaet"]')?.click()`)
	time.Sleep(1500 * time.Millisecond)
	geraete := t.frage(`[...document.querySelectorAll('[data-mpv-tonausgabe]')].map(b=>b.textContent.trim()).join(' | ')`)
	t.notiz(strings.Contains(geraete, "Systemstandard"), "Tonausgabe im Player: %s", geraete)
	t.bildschirmWarte("12-mpv-tonausgabe.png")
	t.skript(`document.querySelector('[data-spuren="qualitaet"]')?.click()`)

	// Weiter abspielen, dann: wem gehört der Ton?
	t.skript(`document.querySelector('[data-toggle]')?.click()`)
	t.warte(4*time.Second, func() bool { return !w.stand().Pausiert })
	time.Sleep(1500 * time.Millisecond)
	t.tonsitzungPruefen()

	// Esc: zurück zur Web-Oberfläche, der Stand geht an den Server.
	t.srv.mu.Lock()
	vorher = len(t.srv.staende)
	t.srv.mu.Unlock()
	t.taste(0x1B)
	if !t.warte(4*time.Second, func() bool { return t.f.spieler.laufend() == nil }) {
		// Kam die Taste nicht an (Fenster nicht vorn), wie die Web-App selbst.
		log.Printf("Selbsttest: Esc kam nicht an (vorn: %#x), schicke sie der Seite", t.vorn())
		t.skript(`document.dispatchEvent(new KeyboardEvent('keydown',{key:'Escape',bubbles:true}))`)
	}
	t.notiz(t.warte(8*time.Second, func() bool { return t.f.spieler.laufend() == nil }), "Esc beendet die Wiedergabe")
	time.Sleep(1500 * time.Millisecond)
	ansicht := t.frage(`document.querySelector('.player')?'player':(location.hash||'/')`)
	var videoSichtbar uintptr
	t.ui(func() { videoSichtbar, _, _ = pIsWindowVisible.Call(t.f.spieler.video) })
	t.notiz(ansicht != "player" && videoSichtbar == 0, "zurück in der Web-Oberfläche (%s), Videofenster weg", ansicht)
	t.notiz(t.warte(5*time.Second, func() bool {
		t.srv.mu.Lock()
		defer t.srv.mu.Unlock()
		return len(t.srv.staende) > vorher
	}), "Stand beim Schliessen gemeldet (%s)", t.letzterStand())
	t.bildschirmWarte("13-nach-dem-film.png")

	// Noch einmal, und ans Ende springen: gesehen, und zurück zur Titelseite.
	t.abspielenKlicken()
	ok = t.warte(25*time.Second, func() bool {
		w = t.f.spieler.laufend()
		return w != nil && w.stand().Geladen && w.stand().Pos > 0.5
	})
	if !t.notiz(ok, "zweite Wiedergabe startet (%s)", t.standText(w)) {
		return
	}
	t.skript(`(()=>{const v=document.querySelector('#v');v.currentTime=v.duration-3})()`)
	t.notiz(t.warte(25*time.Second, func() bool {
		t.srv.mu.Lock()
		defer t.srv.mu.Unlock()
		return t.srv.gesehen > 0
	}), "am Ende als gesehen gemeldet")
	t.notiz(t.warte(10*time.Second, func() bool { return t.f.spieler.laufend() == nil }), "nach dem Ende zurück zur Titelseite")
	time.Sleep(1200 * time.Millisecond)
	t.bildschirmWarte("14-nach-dem-ende.png")
}

func (t *selbsttest) vorn() uintptr {
	var h uintptr
	t.ui(func() { h, _, _ = pGetForegroundWnd.Call() })
	return h
}

func (t *selbsttest) standText(w *lauf) string {
	if w == nil {
		return "kein Lauf"
	}
	z := w.stand()
	return fmt.Sprintf("geladen %v, %.1f s, Ton %d, Untertitel %d, Fehler %q", z.Geladen, z.Pos, z.Ton, z.Ut, z.Fehler)
}

func (t *selbsttest) letzterStand() string {
	t.srv.mu.Lock()
	defer t.srv.mu.Unlock()
	if len(t.srv.staende) == 0 {
		return "keiner"
	}
	return t.srv.staende[len(t.srv.staende)-1] + " ms"
}

// tonsitzungPruefen: Welchem Prozess gehört die Audio-Sitzung, während mpv
// spielt? Auf den Rechnern von GitHub gibt es oft kein Audiogerät — dann
// spielt mpv ohne Ton (das sagt es im Protokoll), und es gibt nichts zu
// prüfen. Der Ablauf im CI installiert darum vorher ein virtuelles Gerät,
// wenn es geht.
func (t *selbsttest) tonsitzungPruefen() {
	liste, err := tonsitzungenListe()
	if err != nil {
		t.notiz(true, "Audio-Sitzungen nicht prüfbar: %v (kein Audiogerät auf diesem Rechner)", err)
		return
	}
	eigen := uint32(os.Getpid())
	var meine, fremd []string
	for _, s := range liste {
		zeile := fmt.Sprintf("%s (PID %d) „%s“ Zustand %d", s.Prozess, s.PID, s.Name, s.Zustand)
		log.Printf("Selbsttest: Audio-Sitzung %s, Symbol %s, Gerät %s", zeile, s.Symbol, s.Geraet)
		switch {
		case s.PID == eigen:
			meine = append(meine, zeile)
		case strings.EqualFold(s.Prozess, "mpv.exe") || strings.EqualFold(s.Prozess, "msedgewebview2.exe"):
			if s.Zustand == 1 {
				fremd = append(fremd, zeile)
			}
		}
	}
	if len(liste) == 0 {
		t.notiz(true, "Audiogerät da, aber keine Sitzungen — mpv hat das Gerät nicht geöffnet (siehe lumen.log)")
		return
	}
	t.notiz(len(meine) > 0 && len(fremd) == 0, "Ton gehört Lumen.exe: %s; aktive fremde: %v", strings.Join(meine, "; "), fremd)
}

// bildschirmBild macht ein Bildschirmfoto, speichert es und gibt es zurück.
func (t *selbsttest) bildschirmBild(name string) image.Image {
	var img image.Image
	var err error
	t.ui(func() { img, err = bildschirmfoto() })
	if err != nil {
		t.notiz(false, "Bildschirm %s: %v", name, err)
		return nil
	}
	f, err := os.Create(filepath.Join(t.ordner, name))
	if err == nil {
		png.Encode(f, img)
		f.Close()
	}
	t.notiz(err == nil, "Bildschirm %s (%dx%d)", name, img.Bounds().Dx(), img.Bounds().Dy())
	return img
}

// bunterAnteil: Anteil kräftig farbiger Punkte im mittleren Band des
// Lumen-Fensters. Das Testbild (testsrc2) ist bunt; die Oberfläche von
// Lumen ist dunkelgrau mit weisser Schrift. Ist die Seite nicht
// durchsichtig, bleibt der Anteil nahe null.
func (t *selbsttest) bunterAnteil(img image.Image) float64 {
	var r rechteck
	t.ui(func() { pGetWindowRect.Call(t.f.hwnd, uintptr(unsafe.Pointer(&r))) })
	b := img.Bounds()
	x0, x1 := int(r.Left)+int(r.Right-r.Left)*15/100, int(r.Left)+int(r.Right-r.Left)*85/100
	y0, y1 := int(r.Top)+int(r.Bottom-r.Top)*20/100, int(r.Top)+int(r.Bottom-r.Top)*40/100
	gesamt, bunt := 0, 0
	for y := max(y0, b.Min.Y); y < min(y1, b.Max.Y); y += 3 {
		for x := max(x0, b.Min.X); x < min(x1, b.Max.X); x += 3 {
			cr, cg, cb, _ := img.At(x, y).RGBA()
			hi, lo := max(cr, cg, cb)>>8, min(cr, cg, cb)>>8
			gesamt++
			if hi-lo > 60 {
				bunt++
			}
		}
	}
	if gesamt == 0 {
		return 0
	}
	return float64(bunt) / float64(gesamt)
}

func windowsText(b []uint16) string {
	for i, c := range b {
		if c == 0 {
			b = b[:i]
			break
		}
	}
	r := make([]rune, len(b))
	for i, c := range b {
		r[i] = rune(c)
	}
	return string(r)
}
