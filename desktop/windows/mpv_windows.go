// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package main

// libmpv, ohne cgo: libmpv-2.dll liegt neben Lumen.exe und wird zur
// Laufzeit geladen; die Funktionen ruft Lumen über ihre Adressen auf
// (syscall), wie die Win32-Aufrufe in w32_windows.go. Die Schnittstelle
// steht in mpv/client.h und ist seit Jahren stabil (MPV_CLIENT_API 2.x).
//
// Warum in Lumen.exe und nicht mpv.exe als eigener Prozess: Den Ton spielt
// dann Lumen.exe. Windows ordnet eine Audio-Sitzung dem Prozess zu, der
// das Gerät öffnet — mit mpv.exe als Kindprozess hiesse die Sitzung im
// Lautstärkemixer „mpv", und Mischer wie Logi MIXLINE, die nach Programm
// verteilen, sähen Lumen gar nicht (dasselbe Problem wie mit
// msedgewebview2.exe, tonausgabe.go). Mehr dazu in player_windows.go.

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Ereignisse (mpv_event_id).
const (
	mpvEreignisKeins        = 0
	mpvEreignisEnde         = 1 // MPV_EVENT_SHUTDOWN
	mpvEreignisLog          = 2
	mpvEreignisDateiStart   = 6
	mpvEreignisDateiEnde    = 7
	mpvEreignisDateiGeladen = 8
	mpvEreignisVideoNeu     = 17 // MPV_EVENT_VIDEO_RECONFIG
	mpvEreignisSprung       = 20
	mpvEreignisNeustart     = 21 // MPV_EVENT_PLAYBACK_RESTART
	mpvEreignisEigenschaft  = 22
)

// Formate (mpv_format).
const (
	mpvFormatKeins  = 0
	mpvFormatText   = 1
	mpvFormatSchalt = 3 // MPV_FORMAT_FLAG
	mpvFormatZahl   = 4 // MPV_FORMAT_INT64
	mpvFormatKomma  = 5 // MPV_FORMAT_DOUBLE
)

// Gründe für das Ende einer Datei (mpv_end_file_reason).
const (
	mpvEndeSchluss = 0 // EOF
	mpvEndeStopp   = 2
	mpvEndeBeendet = 3
	mpvEndeFehler  = 4
)

// mpvEreignis ist mpv_event (64 Bit): id, error, reply_userdata, data.
type mpvEreignis struct {
	ID      int32
	Fehler  int32
	Antwort uint64
	Daten   unsafe.Pointer
}

// mpvEigenschaft ist mpv_event_property.
type mpvEigenschaft struct {
	Name   *byte
	Format int32
	_      int32
	Daten  unsafe.Pointer
}

// mpvDateiEnde ist mpv_event_end_file (nur der Anfang).
type mpvDateiEnde struct {
	Grund  int32
	Fehler int32
}

// mpvLogZeile ist mpv_event_log_message.
type mpvLogZeile struct {
	Vorsilbe *byte
	Stufe    *byte
	Text     *byte
}

// libmpv sind die Funktionen aus libmpv-2.dll.
type libmpv struct {
	pfad                                       string
	create, initialize, terminateDestroy       *windows.Proc
	setOptionString, setPropertyString         *windows.Proc
	setProperty, getPropertyString, getProp    *windows.Proc
	command, observeProperty, waitEvent        *windows.Proc
	wakeup, errorString, free, requestLog, ver *windows.Proc
}

var (
	dieLibmpv      *libmpv
	libmpvFehler   error
	libmpvGeprueft bool
)

// libmpvPfad: libmpv-2.dll neben Lumen.exe. LUMEN_LIBMPV ersetzt den Pfad
// (zum Ausprobieren einer anderen Fassung).
func libmpvPfad() string {
	if p := os.Getenv("LUMEN_LIBMPV"); p != "" {
		return p
	}
	exe, err := os.Executable()
	if err != nil {
		return "libmpv-2.dll"
	}
	return filepath.Join(filepath.Dir(exe), "libmpv-2.dll")
}

// ladeLibmpv lädt die DLL einmal. Fehlt sie, spielt die Web-App wie bisher
// im eingebauten Player (bruecke.js meldet dann keinen mpv).
func ladeLibmpv() (*libmpv, error) {
	if libmpvGeprueft {
		return dieLibmpv, libmpvFehler
	}
	libmpvGeprueft = true
	pfad := libmpvPfad()
	if _, err := os.Stat(pfad); err != nil {
		libmpvFehler = fmt.Errorf("libmpv-2.dll fehlt (%s)", pfad)
		return nil, libmpvFehler
	}
	// Nur dieser eine Pfad, keine Suche: Eine DLL gleichen Namens im
	// Arbeitsordner soll nie geladen werden. Abhängigkeiten sucht Windows
	// im Ordner der DLL (LOAD_WITH_ALTERED_SEARCH_PATH).
	h, err := windows.LoadLibraryEx(pfad, 0, windows.LOAD_WITH_ALTERED_SEARCH_PATH)
	if err != nil {
		libmpvFehler = fmt.Errorf("libmpv-2.dll lässt sich nicht laden: %w", err)
		return nil, libmpvFehler
	}
	d := &windows.DLL{Name: pfad, Handle: h}
	l := &libmpv{pfad: pfad}
	for _, f := range []struct {
		ziel **windows.Proc
		name string
	}{
		{&l.create, "mpv_create"}, {&l.initialize, "mpv_initialize"}, {&l.terminateDestroy, "mpv_terminate_destroy"},
		{&l.setOptionString, "mpv_set_option_string"}, {&l.setPropertyString, "mpv_set_property_string"},
		{&l.setProperty, "mpv_set_property"}, {&l.getPropertyString, "mpv_get_property_string"},
		{&l.getProp, "mpv_get_property"},
		{&l.command, "mpv_command"}, {&l.observeProperty, "mpv_observe_property"}, {&l.waitEvent, "mpv_wait_event"},
		{&l.wakeup, "mpv_wakeup"}, {&l.errorString, "mpv_error_string"}, {&l.free, "mpv_free"},
		{&l.requestLog, "mpv_request_log_messages"}, {&l.ver, "mpv_client_api_version"},
	} {
		p, err := d.FindProc(f.name)
		if err != nil {
			libmpvFehler = fmt.Errorf("libmpv-2.dll ohne %s: %w", f.name, err)
			return nil, libmpvFehler
		}
		*f.ziel = p
	}
	// Hauptnummer 2: Bis dahin bleibt alles, was Lumen benutzt, gleich.
	v, _, _ := syscall.SyscallN(l.ver.Addr())
	if v>>16 != 2 {
		libmpvFehler = fmt.Errorf("libmpv-2.dll hat Schnittstelle %d.%d, Lumen braucht 2.x", v>>16, v&0xffff)
		return nil, libmpvFehler
	}
	dieLibmpv = l
	return l, nil
}

func (l *libmpv) fassung() string {
	v, _, _ := syscall.SyscallN(l.ver.Addr())
	return fmt.Sprintf("%d.%d", v>>16, v&0xffff)
}

// cText legt einen nullterminierten Text auf den Heap (bleibt liegen,
// solange der Zeiger gebraucht wird; Go verschiebt Heap-Objekte nicht).
func cText(s string) *byte {
	p, err := windows.BytePtrFromString(s)
	if err != nil {
		// Ein Nullbyte im Text: abschneiden statt abstürzen.
		p, _ = windows.BytePtrFromString("")
	}
	return p
}

// zeigerVon macht aus einer Rückgabe von syscall einen Zeiger, ohne dass
// go vet eine uintptr-Umwandlung bemängelt (gleicher Kniff wie zeiger[T]).
func zeigerVon(r uintptr) unsafe.Pointer { return *(*unsafe.Pointer)(unsafe.Pointer(&r)) }

func goText(p unsafe.Pointer) string {
	if p == nil {
		return ""
	}
	return windows.BytePtrToString((*byte)(p))
}

// mpvFehlerText: mpv_error_string.
func (l *libmpv) fehlerText(code int32) string {
	r, _, _ := syscall.SyscallN(l.errorString.Addr(), uintptr(code))
	return goText(zeigerVon(r))
}

// mpv ist eine laufende Instanz (mpv_handle).
type mpv struct {
	l *libmpv
	h uintptr
}

func neuesMpv(l *libmpv) (*mpv, error) {
	h, _, _ := syscall.SyscallN(l.create.Addr())
	if h == 0 {
		return nil, errors.New("mpv_create fehlgeschlagen")
	}
	return &mpv{l: l, h: h}, nil
}

func (m *mpv) fehler(was string, r uintptr) error {
	code := int32(r)
	if code >= 0 {
		return nil
	}
	return fmt.Errorf("%s: %s (%d)", was, m.l.fehlerText(code), code)
}

func (m *mpv) option(name, wert string) error {
	n, w := cText(name), cText(wert)
	r, _, _ := syscall.SyscallN(m.l.setOptionString.Addr(), m.h, uintptr(unsafe.Pointer(n)), uintptr(unsafe.Pointer(w)))
	runtime.KeepAlive(n)
	runtime.KeepAlive(w)
	return m.fehler("Option "+name, r)
}

func (m *mpv) starten() error {
	r, _, _ := syscall.SyscallN(m.l.initialize.Addr(), m.h)
	return m.fehler("mpv_initialize", r)
}

// setzen setzt eine Eigenschaft als Text (mpv wandelt selbst um).
func (m *mpv) setzen(name, wert string) error {
	n, w := cText(name), cText(wert)
	r, _, _ := syscall.SyscallN(m.l.setPropertyString.Addr(), m.h, uintptr(unsafe.Pointer(n)), uintptr(unsafe.Pointer(w)))
	runtime.KeepAlive(n)
	runtime.KeepAlive(w)
	return m.fehler("Eigenschaft "+name, r)
}

func (m *mpv) setzenKomma(name string, wert float64) error {
	n := cText(name)
	w := new(float64)
	*w = wert
	r, _, _ := syscall.SyscallN(m.l.setProperty.Addr(), m.h, uintptr(unsafe.Pointer(n)), mpvFormatKomma, uintptr(unsafe.Pointer(w)))
	runtime.KeepAlive(n)
	runtime.KeepAlive(w)
	return m.fehler("Eigenschaft "+name, r)
}

func (m *mpv) setzenSchalter(name string, an bool) error {
	n := cText(name)
	w := new(int32)
	if an {
		*w = 1
	}
	r, _, _ := syscall.SyscallN(m.l.setProperty.Addr(), m.h, uintptr(unsafe.Pointer(n)), mpvFormatSchalt, uintptr(unsafe.Pointer(w)))
	runtime.KeepAlive(n)
	runtime.KeepAlive(w)
	return m.fehler("Eigenschaft "+name, r)
}

// lesen liest eine Eigenschaft als Text; Listen (track-list,
// audio-device-list) kommen dabei als JSON.
func (m *mpv) lesen(name string) (string, bool) {
	n := cText(name)
	r, _, _ := syscall.SyscallN(m.l.getPropertyString.Addr(), m.h, uintptr(unsafe.Pointer(n)))
	runtime.KeepAlive(n)
	if r == 0 {
		return "", false
	}
	s := goText(zeigerVon(r))
	syscall.SyscallN(m.l.free.Addr(), r)
	return s, true
}

func (m *mpv) lesenKomma(name string) (float64, bool) {
	n := cText(name)
	w := new(float64)
	r, _, _ := syscall.SyscallN(m.l.getProp.Addr(), m.h, uintptr(unsafe.Pointer(n)), mpvFormatKomma, uintptr(unsafe.Pointer(w)))
	runtime.KeepAlive(n)
	if int32(r) < 0 {
		return 0, false
	}
	return *w, true
}

// befehl führt einen mpv-Befehl aus (mpv_command mit einer Liste von
// Texten, die mit NULL endet). Kein Quoting nötig, anders als bei
// mpv_command_string.
func (m *mpv) befehl(teile ...string) error {
	texte := make([]*byte, len(teile))
	liste := make([]uintptr, len(teile)+1)
	for i, t := range teile {
		texte[i] = cText(t)
		liste[i] = uintptr(unsafe.Pointer(texte[i]))
	}
	r, _, _ := syscall.SyscallN(m.l.command.Addr(), m.h, uintptr(unsafe.Pointer(&liste[0])))
	runtime.KeepAlive(texte)
	runtime.KeepAlive(liste)
	name := ""
	if len(teile) > 0 {
		name = teile[0]
	}
	return m.fehler("Befehl "+name, r)
}

// beobachten meldet eine Eigenschaft an; ihre Änderungen kommen als
// Ereignis mit kennung zurück.
func (m *mpv) beobachten(kennung uint64, name string, format int) error {
	n := cText(name)
	r, _, _ := syscall.SyscallN(m.l.observeProperty.Addr(), m.h, uintptr(kennung), uintptr(unsafe.Pointer(n)), uintptr(format))
	runtime.KeepAlive(n)
	return m.fehler("beobachten "+name, r)
}

func (m *mpv) protokoll(stufe string) {
	s := cText(stufe)
	syscall.SyscallN(m.l.requestLog.Addr(), m.h, uintptr(unsafe.Pointer(s)))
	runtime.KeepAlive(s)
}

// warten wartet auf das nächste Ereignis. Der Zeiger gilt bis zum
// nächsten Aufruf. Die Frist ist ein double — unter Windows x64 kommt er
// im zweiten XMM-Register an; SyscallN legt jedes der ersten vier
// Argumente auch dorthin.
func (m *mpv) warten(sekunden float64) *mpvEreignis {
	r, _, _ := syscall.SyscallN(m.l.waitEvent.Addr(), m.h, uintptr(math.Float64bits(sekunden)))
	return (*mpvEreignis)(zeigerVon(r))
}

func (m *mpv) wecken() { syscall.SyscallN(m.l.wakeup.Addr(), m.h) }

// beenden beendet mpv und gibt alles frei. Blockiert, bis das Video-
// fenster von mpv weg ist — darum nie im Fenster-Thread aufrufen (das
// Fenster schickt seinem Elternfenster dabei Nachrichten).
func (m *mpv) beenden() {
	syscall.SyscallN(m.l.terminateDestroy.Addr(), m.h)
	m.h = 0
}
