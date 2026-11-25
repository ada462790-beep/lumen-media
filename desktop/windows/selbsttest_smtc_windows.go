// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package main

// Selbsttest der Mediensteuerung (mediensteuerung_windows.go): Was sieht
// Windows? Gelesen wird von der anderen Seite, so wie das Medien-Overlay
// es tut — über GlobalSystemMediaTransportControlsSessionManager: Gibt es
// eine Sitzung mit dem Titel des Films, spielt sie, hat sie ein Bild, und
// schaltet „Abspielen/Pause" von dort mpv um? Und es soll keine zweite
// Sitzung von WebView2 geben.

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	iidGsmtcManagerStatics = windows.GUID{Data1: 0x2050c4ee, Data2: 0x11a0, Data3: 0x57de, Data4: [8]byte{0xae, 0xd7, 0xc9, 0x7c, 0x70, 0x33, 0x82, 0x45}}
	iidGsmtcSession        = windows.GUID{Data1: 0x7148c835, Data2: 0x9b14, Data3: 0x5ae2, Data4: [8]byte{0xab, 0x85, 0xdc, 0x9b, 0x1c, 0x14, 0xe1, 0xa8}}
)

// Methoden (Windows.Media.Control).
const (
	gsmtcStaticsRequestAsync  = 6
	gsmtcManagerGetSessions   = 7
	gsmtcSessionAumid         = 6
	gsmtcSessionMediaProps    = 7
	gsmtcSessionPlaybackInfo  = 9
	gsmtcSessionTogglePlay    = 20
	gsmtcPropsTitle           = 6
	gsmtcPropsSubtitle        = 7
	gsmtcPropsThumbnail       = 15
	gsmtcInfoPlaybackStatus   = 7
	vectorViewGetAt           = 6
	vectorViewSize            = 7
	asyncOperationGetResults  = 8
	gsmtcStatusSpielt         = 4
	gsmtcStatusPausiert       = 5
	scheinPosterBreite        = 200
	scheinPosterHoehe         = 300
	scheinPosterPfad          = "/api/v1/art/" + filmID + "/poster"
	mediensteuerungFrist      = 10 * time.Second
	mediensteuerungUmschalten = 6 * time.Second
)

// smtcSitzung: was Windows über eine Sitzung weiss.
type smtcSitzung struct {
	App      string
	Titel    string
	Unter    string
	Status   int32
	MitBild  bool
	Fehler   string
	sitzung  com
	Spielend bool
}

func (s smtcSitzung) String() string {
	return fmt.Sprintf("%s „%s“ / „%s“, Status %d, Bild %v", s.App, s.Titel, s.Unter, s.Status, s.MitBild)
}

// scheinPoster: Anfragen der App an das Poster beim Schein-Server.
type scheinPoster struct {
	mu            sync.Mutex
	vonApp        int // mit User-Agent „Lumen/“ (die App selbst, nicht WebView2)
	mitKopf       int
	mitSchluessel int
}

var posterZaehler scheinPoster

func (t *selbsttest) posterRoute(mux *http.ServeMux, angemeldet func(*http.Request) bool) {
	mux.HandleFunc(scheinPosterPfad, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.UserAgent(), "Lumen/") {
			posterZaehler.mu.Lock()
			posterZaehler.vonApp++
			if angemeldet(r) {
				posterZaehler.mitKopf++
			}
			if r.URL.Query().Has("key") || r.URL.Query().Has("token") {
				posterZaehler.mitSchluessel++
			}
			posterZaehler.mu.Unlock()
		}
		if !angemeldet(r) {
			w.WriteHeader(401)
			return
		}
		// Ein Verlauf in den Farben von Lumen, gross genug für das Overlay.
		img := image.NewRGBA(image.Rect(0, 0, scheinPosterBreite, scheinPosterHoehe))
		for y := 0; y < scheinPosterHoehe; y++ {
			for x := 0; x < scheinPosterBreite; x++ {
				img.Set(x, y, color.RGBA{uint8(40 + y*180/scheinPosterHoehe), uint8(20 + x*60/scheinPosterBreite), 200, 255})
			}
		}
		w.Header().Set("Content-Type", "image/png")
		_ = png.Encode(w, img)
	})
}

// smtcSitzungen liest alle Sitzungen, die Windows kennt. Läuft in einem
// eigenen Faden (MTA): Die asynchronen Aufrufe von WinRT warten dort.
func smtcSitzungen(fn func(liste []smtcSitzung)) error {
	fertig := make(chan error, 1)
	go func() {
		runtime.LockOSThread() // Faden endet mit der Goroutine, COM mit ihm
		if err := winrtStarten(roInitMulti); err != nil {
			fertig <- err
			return
		}
		statics, err := fabrik("Windows.Media.Control.GlobalSystemMediaTransportControlsSessionManager", &iidGsmtcManagerStatics)
		if err != nil {
			fertig <- err
			return
		}
		defer comLos(statics)
		op, hr := comObjekt(statics, gsmtcStaticsRequestAsync)
		if op == nil {
			fertig <- fmt.Errorf("RequestAsync: %#x", uint32(hr))
			return
		}
		defer comLos(op)
		if err := asyncWarten(op, mediensteuerungFrist); err != nil {
			fertig <- fmt.Errorf("RequestAsync: %w", err)
			return
		}
		manager, hr := comObjekt(op, asyncOperationGetResults)
		if manager == nil {
			fertig <- fmt.Errorf("GetResults: %#x", uint32(hr))
			return
		}
		defer comLos(manager)
		vektor, hr := comObjekt(manager, gsmtcManagerGetSessions)
		if vektor == nil {
			fertig <- fmt.Errorf("GetSessions: %#x", uint32(hr))
			return
		}
		defer comLos(vektor)
		n := comZahl(vektor, vectorViewSize)
		var liste []smtcSitzung
		for i := uint32(0); i < n && i < 32; i++ {
			s, hr := comObjekt(vektor, vectorViewGetAt, uintptr(i))
			if s == nil {
				liste = append(liste, smtcSitzung{Fehler: fmt.Sprintf("GetAt: %#x", uint32(hr))})
				continue
			}
			liste = append(liste, smtcSitzungLesen(s))
		}
		fn(liste)
		for _, s := range liste {
			comLos(s.sitzung)
		}
		fertig <- nil
	}()
	return <-fertig
}

func smtcSitzungLesen(s com) smtcSitzung {
	z := smtcSitzung{sitzung: s}
	z.App, _ = comHText(s, gsmtcSessionAumid)
	if info, _ := comObjekt(s, gsmtcSessionPlaybackInfo); info != nil {
		comAufruf(info, gsmtcInfoPlaybackStatus, uintptr(unsafe.Pointer(&z.Status)))
		comLos(info)
	}
	z.Spielend = z.Status == gsmtcStatusSpielt
	op, hr := comObjekt(s, gsmtcSessionMediaProps)
	if op == nil {
		z.Fehler = fmt.Sprintf("TryGetMediaPropertiesAsync: %#x", uint32(hr))
		return z
	}
	defer comLos(op)
	if err := asyncWarten(op, mediensteuerungFrist); err != nil {
		z.Fehler = "TryGetMediaPropertiesAsync: " + err.Error()
		return z
	}
	props, _ := comObjekt(op, asyncOperationGetResults)
	if props == nil {
		return z
	}
	defer comLos(props)
	z.Titel, _ = comHText(props, gsmtcPropsTitle)
	z.Unter, _ = comHText(props, gsmtcPropsSubtitle)
	if bild, _ := comObjekt(props, gsmtcPropsThumbnail); bild != nil {
		z.MitBild = true
		comLos(bild)
	}
	return z
}

// smtcUmschalten: „Abspielen/Pause" in der Sitzung mit diesem Titel, wie
// ein Klick im Overlay.
func smtcUmschalten(titel string) error {
	var ergebnis error = fmt.Errorf("keine Sitzung „%s“", titel)
	err := smtcSitzungen(func(liste []smtcSitzung) {
		for _, s := range liste {
			if s.Titel != titel || s.sitzung == nil {
				continue
			}
			op, hr := comObjekt(s.sitzung, gsmtcSessionTogglePlay)
			if op == nil {
				ergebnis = fmt.Errorf("TryTogglePlayPauseAsync: %#x", uint32(hr))
				return
			}
			defer comLos(op)
			if err := asyncWarten(op, mediensteuerungFrist); err != nil {
				ergebnis = err
				return
			}
			var ok uint8
			comAufruf(op, asyncOperationGetResults, uintptr(unsafe.Pointer(&ok)))
			if ok == 0 {
				ergebnis = fmt.Errorf("Windows sagt: nicht umgeschaltet")
				return
			}
			ergebnis = nil
			return
		}
	})
	if err != nil {
		return err
	}
	return ergebnis
}

// mediensteuerungPruefen: während mpv den Testfilm spielt.
func (t *selbsttest) mediensteuerungPruefen(w *lauf) {
	var da, tasten bool
	t.ui(func() { da, tasten = t.f.spieler.smtc != nil, t.f.spieler.tasten })
	if !t.notiz(da, "Mediensteuerung von Windows angemeldet (GetForWindow)") {
		return
	}
	t.notiz(!tasten, "Medientasten nicht zusätzlich über RegisterHotKey (sonst doppelt)")

	var unsere smtcSitzung
	var alle []string
	var webview []string
	gefunden := t.warte(mediensteuerungFrist, func() bool {
		alle, webview = nil, nil
		err := smtcSitzungen(func(liste []smtcSitzung) {
			for _, s := range liste {
				alle = append(alle, s.String())
				if s.Titel == "Testfilm" {
					unsere = s
				}
				if strings.Contains(strings.ToLower(s.App), "webview") && s.Spielend {
					webview = append(webview, s.String())
				}
			}
		})
		if err != nil {
			log.Printf("Selbsttest: Mediensitzungen: %v", err)
			alle = []string{"Fehler: " + err.Error()}
			return false
		}
		return unsere.Titel != "" && unsere.MitBild && unsere.Spielend
	})
	log.Printf("Selbsttest: Mediensitzungen in Windows: %v", alle)
	if !t.notiz(unsere.Titel == "Testfilm", "Windows kennt die Sitzung „Testfilm“ (%v)", alle) {
		return
	}
	t.notiz(unsere.Spielend, "Sitzung meldet „spielt“ (Status %d)", unsere.Status)
	posterZaehler.mu.Lock()
	vonApp, mitKopf, mitSchl := posterZaehler.vonApp, posterZaehler.mitKopf, posterZaehler.mitSchluessel
	posterZaehler.mu.Unlock()
	t.notiz(unsere.MitBild && vonApp > 0 && mitKopf == vonApp && mitSchl == 0,
		"Poster in der Mediensteuerung, Token nur im Kopf (%d Anfragen der App, %d mit Kopf, %d mit key=)", vonApp, mitKopf, mitSchl)
	t.notiz(len(webview) == 0, "keine zweite Sitzung von WebView2 (%v)", webview)
	_ = gefunden

	// Abspielen/Pause von der anderen Seite: wie ein Klick im Overlay.
	if err := smtcUmschalten("Testfilm"); !t.notiz(err == nil, "Windows schickt „Pause“ an Lumen (%v)", err) {
		return
	}
	t.notiz(t.warte(mediensteuerungUmschalten, func() bool { return w.stand().Pausiert }), "Knopf in der Mediensteuerung hält mpv an")
	var status int32
	t.warte(mediensteuerungFrist, func() bool {
		_ = smtcSitzungen(func(liste []smtcSitzung) {
			for _, s := range liste {
				if s.Titel == "Testfilm" {
					status = s.Status
				}
			}
		})
		return status == gsmtcStatusPausiert
	})
	t.notiz(status == gsmtcStatusPausiert, "Sitzung meldet danach „pausiert“ (Status %d)", status)
	if err := smtcUmschalten("Testfilm"); t.notiz(err == nil, "Windows schickt „Abspielen“ an Lumen (%v)", err) {
		t.notiz(t.warte(mediensteuerungUmschalten, func() bool { return !w.stand().Pausiert }), "mpv spielt weiter")
	}
}

// mediensteuerungNachher: Nach dem Schliessen ist Lumen aus dem Overlay weg.
func (t *selbsttest) mediensteuerungNachher() {
	var noch []string
	weg := t.warte(mediensteuerungFrist, func() bool {
		noch = nil
		_ = smtcSitzungen(func(liste []smtcSitzung) {
			for _, s := range liste {
				if s.Titel == "Testfilm" && (s.Status == gsmtcStatusSpielt || s.Status == gsmtcStatusPausiert) {
					noch = append(noch, s.String())
				}
			}
		})
		return len(noch) == 0
	})
	t.notiz(weg, "nach dem Schliessen keine laufende Sitzung mehr (%v)", noch)
}
