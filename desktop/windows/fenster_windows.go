// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"unsafe"

	"github.com/wailsapp/go-webview2/pkg/edge"
	"github.com/wailsapp/go-webview2/webviewloader"
	"golang.org/x/sys/windows"
)

func init() {
	// Fenster und WebView2 gehören einem Thread. Der Hauptthread bleibt es.
	runtime.LockOSThread()
}

const (
	fensterKlasse = "LumenDesktopFenster"
	wmAuftrag     = wmApp + 1

	// Einträge im Systemmenü (Alt+Leertaste). Unter 0xF000 und durch 16
	// teilbar, so will es WM_SYSCOMMAND.
	idWechseln = 0x0010
	idVollbild = 0x0020
	idUeber    = 0x0030

	// Mindestgrösse in Punkten bei 100 % Skalierung.
	minBreite = 960
	minHoehe  = 600

	webview2Download = "https://go.microsoft.com/fwlink/p/?LinkId=2124703"
)

type fenster struct {
	hwnd     uintptr
	chromium *edge.Chromium
	wv       com // ICoreWebView2, für das, was edge nicht anbietet
	app      *App
	bereit   bool
	debug    bool
	ordner   string

	mu        sync.Mutex
	auftraege []func()

	vollbild        bool // das Fenster füllt den Bildschirm
	durchF11        bool // … weil F11 gedrückt wurde (nicht der Player)
	domVollbild     bool // die Seite hat ein Element im Vollbild
	vorVollbild     fensterLage
	stilVorVollbild uintptr

	test *selbsttest
}

var dasFenster *fenster

func starten(opt optionen) int {
	if opt.selbsttest != "" {
		abs, err := filepath.Abs(opt.selbsttest)
		if err == nil {
			opt.selbsttest = abs
		}
		os.MkdirAll(opt.selbsttest, 0o755)
		os.Setenv("LUMEN_DESKTOP_DATEN", filepath.Join(opt.selbsttest, "daten"))
	}
	ordner, lokal := einstellungsOrdner(), lokalerOrdner()
	os.MkdirAll(ordner, 0o700)
	os.MkdirAll(lokal, 0o700)
	logEinrichten(lokal, opt.selbsttest)
	log.Printf("Lumen für Windows %s startet", version)

	// Nur ein Fenster: Ein zweiter Start holt das erste nach vorn.
	if opt.selbsttest == "" {
		name, _ := windows.UTF16PtrFromString("Local\\LumenDesktopEinzeln")
		h, err := windows.CreateMutex(nil, false, name)
		if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
			if hwnd, _, _ := pFindWindowW.Call(uintptr(unsafe.Pointer(utf16(fensterKlasse))), 0); hwnd != 0 {
				if ikon, _, _ := pIsIconic.Call(hwnd); ikon != 0 {
					pShowWindow.Call(hwnd, swRestore)
				}
				pSetForegroundWindow.Call(hwnd)
			}
			return 0
		}
		defer windows.CloseHandle(h)
	}

	// Ohne WebView2 geht nichts. Auf Windows 11 ist es immer da, auf
	// Windows 10 fast immer; sonst bringt es der Installer mit. Fehlt es
	// trotzdem (etwa nach einer Bereinigung), sagt Lumen es klar.
	laufzeit, err := webviewloader.GetAvailableCoreWebView2BrowserVersionString("")
	if err != nil || laufzeit == "" {
		log.Printf("WebView2 fehlt: %v", err)
		text := "Lumen needs the Microsoft Edge WebView2 Runtime, which isn't installed on this computer.\n\nDownload it now from Microsoft?"
		if deutsch() {
			text = "Lumen braucht die Microsoft-Edge-WebView2-Laufzeit, und die fehlt auf diesem Rechner.\n\nJetzt bei Microsoft herunterladen?"
		}
		if meldungsfenster(0, text, "Lumen", mbYesNo|mbIconWarning) == idYes {
			shellOeffnen(webview2Download, "")
		}
		return 1
	}
	log.Printf("WebView2 %s", laufzeit)

	f := &fenster{debug: opt.debug, ordner: ordner}
	dasFenster = f
	app, _ := NeueApp(f, ordner, lokal)
	f.app = app
	if opt.selbsttest != "" {
		f.test = neuerSelbsttest(f, opt.selbsttest)
		app.keinNetz = true
		if err := f.test.serverStarten(); err != nil {
			log.Printf("Selbsttest: %v", err)
			return 1
		}
	}

	if err := f.erstellen(); err != nil {
		log.Printf("Fenster: %v", err)
		return 1
	}
	if err := f.webviewEinrichten(lokal); err != nil {
		log.Printf("WebView2: %v", err)
		return 1
	}
	if f.test != nil {
		f.test.beginnen()
	}
	go app.UpdatePruefen(context.Background())

	var m msg
	for {
		r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
	if f.test != nil {
		return f.test.ergebnis()
	}
	return 0
}

// logEinrichten schreibt das Protokoll nach %LOCALAPPDATA%\Lumen\lumen.log
// (ein Fensterprogramm hat keine Konsole). Wird es zu gross, beginnt es neu.
func logEinrichten(lokal, selbsttest string) {
	pfad := filepath.Join(lokal, "lumen.log")
	if selbsttest != "" {
		pfad = filepath.Join(selbsttest, "lumen.log")
	}
	if st, err := os.Stat(pfad); err == nil && st.Size() > 1<<20 {
		os.Remove(pfad)
	}
	f, err := os.OpenFile(pfad, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	log.SetOutput(io.MultiWriter(f, os.Stderr))
}

// ---------------------------------------------------------------- Fenster

func (f *fenster) erstellen() error {
	inst, _, _ := pGetModuleHandleW.Call(0)
	cx, _, _ := pGetSystemMetrics.Call(smCxIcon)
	cy, _, _ := pGetSystemMetrics.Call(smCyIcon)
	sx, _, _ := pGetSystemMetrics.Call(smCxSmIcon)
	sy, _, _ := pGetSystemMetrics.Call(smCySmIcon)
	// Das Symbol steht unter der Nummer 1 in den Ressourcen der .exe
	// (werkzeuge/ressourcen).
	gross, _, _ := pLoadImageW.Call(inst, 1, imageIcon, cx, cy, lrDefaultColor)
	klein, _, _ := pLoadImageW.Call(inst, 1, imageIcon, sx, sy, lrDefaultColor)
	cursor, _, _ := pLoadCursorW.Call(0, idcArrow)
	// Dunkler Hintergrund von Anfang an: kein weisser Blitz, bevor WebView2
	// das erste Bild zeigt.
	pinsel, _, _ := pCreateSolidBrush.Call(0x00070505)

	wc := wndClassEx{
		Size:       uint32(unsafe.Sizeof(wndClassEx{})),
		WndProc:    windows.NewCallback(wndProc),
		Instance:   inst,
		Icon:       gross,
		IconSm:     klein,
		Cursor:     cursor,
		Background: pinsel,
		ClassName:  utf16(fensterKlasse),
	}
	if r, _, err := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		return fmt.Errorf("RegisterClassEx: %w", err)
	}

	dpi := dpiVon(0)
	breite, hoehe := uintptr(1280*dpi/96), uintptr(800*dpi/96)
	hwnd, _, err := pCreateWindowExW.Call(0,
		uintptr(unsafe.Pointer(utf16(fensterKlasse))),
		uintptr(unsafe.Pointer(utf16("Lumen"))),
		wsOverlappedWindow|wsClipChildren,
		0x80000000, 0x80000000, breite, hoehe, // CW_USEDEFAULT
		0, 0, inst, 0)
	if hwnd == 0 {
		return fmt.Errorf("CreateWindowEx: %w", err)
	}
	f.hwnd = hwnd
	dunklerRahmen(hwnd)
	f.systemmenue()

	zeigen := uintptr(swShowNormal)
	if z, ok := LadeFenster(f.ordner); ok && f.test == nil {
		r := rechteck{z.Links, z.Oben, z.Rechts, z.Unten}
		if aufEinemBildschirm(r) {
			l := lage(hwnd)
			l.NormalPos = r
			l.ShowCmd = swShowNormal
			if z.Maximiert {
				l.ShowCmd = swShowMaximized
			}
			l.Flags = 0
			lageSetzen(hwnd, l)
			zeigen = uintptr(l.ShowCmd)
		}
	}
	pShowWindow.Call(hwnd, zeigen)
	pUpdateWindow.Call(hwnd)
	return nil
}

func (f *fenster) systemmenue() {
	m, _, _ := pGetSystemMenu.Call(f.hwnd, 0)
	if m == 0 {
		return
	}
	wechseln, vollbild, ueber := "Switch Server…\tCtrl+Shift+S", "Full Screen\tF11", "About Lumen"
	if deutsch() {
		wechseln, vollbild, ueber = "Server wechseln …\tStrg+Umschalt+S", "Vollbild\tF11", "Über Lumen"
	}
	pAppendMenuW.Call(m, mfSeparator, 0, 0)
	pAppendMenuW.Call(m, mfString, idWechseln, uintptr(unsafe.Pointer(utf16(wechseln))))
	pAppendMenuW.Call(m, mfString, idVollbild, uintptr(unsafe.Pointer(utf16(vollbild))))
	pAppendMenuW.Call(m, mfString, idUeber, uintptr(unsafe.Pointer(utf16(ueber))))
}

func (f *fenster) ueber() {
	laufzeit, _ := webviewloader.GetAvailableCoreWebView2BrowserVersionString("")
	text := fmt.Sprintf("Lumen for Windows %s\nWebView2 %s\n\nFree software under the GNU Affero General Public License v3.\nSource code: https://github.com/%s", version, laufzeit, repository)
	titel := "About Lumen"
	if deutsch() {
		text = fmt.Sprintf("Lumen für Windows %s\nWebView2 %s\n\nFreie Software unter der GNU Affero General Public License v3.\nQuelltext: https://github.com/%s", version, laufzeit, repository)
		titel = "Über Lumen"
	}
	meldungsfenster(f.hwnd, text, titel, mbOK|mbIconInfo)
}

// zeiger liest einen Zeiger, den Windows in lParam mitgibt.
func zeiger[T any](lp *uintptr) *T { return *(**T)(unsafe.Pointer(lp)) }

func wndProc(hwnd, m, wp, lp uintptr) uintptr {
	f := dasFenster
	if f == nil || (f.hwnd != 0 && hwnd != f.hwnd) {
		r, _, _ := pDefWindowProcW.Call(hwnd, m, wp, lp)
		return r
	}
	switch m {
	case wmAuftrag:
		f.auftraegeAbarbeiten()
		return 0
	case wmSize:
		if f.bereit && wp != sizeMinimized {
			f.chromium.Resize()
		}
		return 0
	case wmMove:
		if f.bereit {
			f.chromium.NotifyParentWindowPositionChanged()
		}
	case wmSetFocus:
		if f.bereit {
			f.chromium.Focus()
		}
	case wmGetMinMaxInfo:
		mmi := zeiger[minMaxInfo](&lp)
		dpi := int32(dpiVon(hwnd))
		mmi.MinTrackSize = punkt{minBreite * dpi / 96, minHoehe * dpi / 96}
		return 0
	case wmDpiChanged:
		r := zeiger[rechteck](&lp)
		pSetWindowPos.Call(hwnd, 0, uintptr(r.Left), uintptr(r.Top), uintptr(r.Right-r.Left), uintptr(r.Bottom-r.Top), swpNoZOrder|swpNoActivate)
		return 0
	case wmSysCommand:
		switch wp &^ 0xF {
		case idWechseln:
			f.app.ServerWechseln()
			return 0
		case idVollbild:
			f.f11()
			return 0
		case idUeber:
			f.ueber()
			return 0
		}
	case wmClose:
		f.zustandSichern()
		pDestroyWindow.Call(hwnd)
		return 0
	case wmDestroy:
		pPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, m, wp, lp)
	return r
}

func (f *fenster) zustandSichern() {
	if f.test != nil {
		return
	}
	l := lage(f.hwnd)
	if f.vollbild {
		l = f.vorVollbild
	}
	z := Fensterzustand{
		Links: l.NormalPos.Left, Oben: l.NormalPos.Top, Rechts: l.NormalPos.Right, Unten: l.NormalPos.Bottom,
		// Minimiert geschlossen: maximiert, wenn es vorher maximiert war
		// (WPF_RESTORETOMAXIMIZED).
		Maximiert: l.ShowCmd == swShowMaximized || (l.ShowCmd == 2 && l.Flags&2 != 0),
	}
	if err := SpeichereFenster(f.ordner, z); err != nil {
		log.Printf("Fensterlage nicht gespeichert: %v", err)
	}
}

// ---------------------------------------------------------------- Vollbild

// vollbildSetzen lässt das Fenster den ganzen Bildschirm füllen, ohne
// Rahmen und über der Taskleiste — so wie es der Player im Browser auch
// tut (Element.requestFullscreen).
func (f *fenster) vollbildSetzen(an bool) {
	if an == f.vollbild {
		return
	}
	if an {
		mi, ok := monitorVon(f.hwnd)
		if !ok {
			return
		}
		f.vorVollbild = lage(f.hwnd)
		f.stilVorVollbild = getWindowLongPtr(f.hwnd, gwlStyle)
		setWindowLongPtr(f.hwnd, gwlStyle, f.stilVorVollbild&^wsOverlappedWindow)
		r := mi.Monitor
		pSetWindowPos.Call(f.hwnd, 0, uintptr(r.Left), uintptr(r.Top), uintptr(r.Right-r.Left), uintptr(r.Bottom-r.Top),
			swpNoOwnerZOrder|swpFrameChanged)
	} else {
		setWindowLongPtr(f.hwnd, gwlStyle, f.stilVorVollbild|wsOverlappedWindow)
		lageSetzen(f.hwnd, f.vorVollbild)
		pSetWindowPos.Call(f.hwnd, 0, 0, 0, 0, 0, swpNoMove|swpNoSize|swpNoZOrder|swpNoOwnerZOrder|swpFrameChanged)
	}
	f.vollbild = an
	log.Printf("Vollbild: %v", an)
}

// f11 schaltet das Fenster-Vollbild um. Ist gerade der Player im
// Vollbild, beendet F11 beides.
func (f *fenster) f11() {
	if f.vollbild {
		f.durchF11 = false
		if f.domVollbild {
			f.Skript("document.fullscreenElement&&document.exitFullscreen()")
		}
		f.vollbildSetzen(false)
		return
	}
	f.durchF11 = true
	f.vollbildSetzen(true)
}

func (f *fenster) domVollbildGeaendert(an bool) {
	f.domVollbild = an
	if an {
		f.vollbildSetzen(true)
	} else if !f.durchF11 {
		f.vollbildSetzen(false)
	}
}

// taste: Tastenkürzel der App, bevor die Seite sie sieht.
func (f *fenster) taste(vk uint) bool {
	strg, umschalt, alt := tasteGedrueckt(vkControl), tasteGedrueckt(vkShift), tasteGedrueckt(vkMenu)
	switch {
	case vk == vkF11 && !strg && !alt:
		f.f11()
		return true
	case vk == vkReturn && alt && !strg:
		f.f11()
		return true
	case vk == vkS && strg && umschalt && !alt:
		f.app.ServerWechseln()
		return true
	case !f.debug && (vk == vkF12 || (vk == vkI && strg && umschalt)):
		return true
	}
	return false
}

// ---------------------------------------------------------------- WebView2

func (f *fenster) webviewEinrichten(lokal string) error {
	daten := filepath.Join(lokal, "WebView2")
	os.MkdirAll(daten, 0o700)
	c := edge.NewChromium()
	c.DataPath = daten
	// Medientasten und die Mediensteuerung von Windows (Lautstärke-Overlay,
	// Sperrbildschirm) folgen der Media Session API der Web-App. In
	// WebView2 ist das aus, solange man es nicht einschaltet.
	c.AdditionalBrowserArgs = []string{"--enable-features=HardwareMediaKeyHandling"}
	c.SetErrorCallback(func(err error) { log.Printf("WebView2-Fehler: %v", err) })
	c.MessageCallback = func(nachricht string, sender *edge.ICoreWebView2, args *edge.ICoreWebView2WebMessageReceivedEventArgs) {
		quelle, _ := args.GetSource()
		f.app.Nachricht(quelle, nachricht)
	}
	c.WebResourceRequestedCallback = f.eingebauteSeite
	c.ContainsFullScreenElementChangedCallback = func(sender *edge.ICoreWebView2, _ *edge.ICoreWebView2ContainsFullScreenElementChangedEventArgs) {
		an, err := sender.GetContainsFullScreenElement()
		if err == nil {
			f.domVollbildGeaendert(an)
		}
	}
	c.AcceleratorKeyCallback = f.taste
	f.chromium = c

	if !c.Embed(f.hwnd) {
		return errors.New("WebView2 liess sich nicht einbetten")
	}
	c.SetBackgroundColour(5, 5, 7, 255)

	if s, err := c.GetSettings(); err == nil {
		s.PutAreDevToolsEnabled(f.debug)
		s.PutIsStatusBarEnabled(false)
		s.PutIsZoomControlEnabled(true)
		// Die Web-App erkennt die Hülle auch am User-Agent. (Direkt über
		// die Tabelle: GetUserAgent in edge gibt den Zeiger falsch weiter.)
		if s2 := s.GetICoreWebView2Settings2(); s2 != nil {
			o := com(unsafe.Pointer(s2))
			ua, ok := comText(o, settingsGetUserAgent)
			if ok && !strings.Contains(ua, "LumenDesktop/") {
				neu, _ := windows.UTF16PtrFromString(ua + " LumenDesktop/" + version)
				if hr := comAufruf(o, settingsPutUserAgent, uintptr(unsafe.Pointer(neu))); hr != 0 {
					log.Printf("User-Agent: %#x", hr)
				}
			} else if !ok {
				log.Printf("User-Agent nicht lesbar")
			}
		}
	}

	// Die übrigen Ereignisse direkt über die Methodentabelle
	// (com_windows.go) — edge bietet sie nicht an.
	ew, err := c.GetController().GetCoreWebView2()
	if err != nil {
		return err
	}
	f.wv = com(unsafe.Pointer(ew))
	if !anmelden(f.wv, wvAddNavigationStarting, ereignis(f.navigationBeginnt)) ||
		!anmelden(f.wv, wvAddNavigationCompleted, ereignis(f.navigationFertig)) ||
		!anmelden(f.wv, wvAddNewWindowRequested, ereignis(f.neuesFenster)) {
		return errors.New("WebView2-Ereignisse liessen sich nicht anmelden")
	}

	c.AddWebResourceRequestedFilter("http://"+seitenHost+"/*", edge.COREWEBVIEW2_WEB_RESOURCE_CONTEXT_ALL)
	c.Init(brueckeSkript())

	f.bereit = true
	c.Resize()
	c.Navigate(f.app.StartAdresse())
	c.Focus()
	return nil
}

// eingebauteSeite beantwortet http://lumen.localhost/… aus der .exe.
func (f *fenster) eingebauteSeite(req *edge.ICoreWebView2WebResourceRequest, args *edge.ICoreWebView2WebResourceRequestedEventArgs) {
	adresse, err := req.GetUri()
	if err != nil {
		return
	}
	inhalt, typ, status := SeitenAntwort(adresse)
	text := "OK"
	if status != 200 {
		text = "Not Found"
	}
	env := f.chromium.Environment()
	antwort, err := env.CreateWebResourceResponse(nil, status, text, fmt.Sprintf(seitenKopf, typ))
	if err != nil {
		log.Printf("Antwort für %s: %v", adresse, err)
		return
	}
	defer antwort.Release()
	if err := antwort.PutByteContent(inhalt); err != nil {
		log.Printf("Inhalt für %s: %v", adresse, err)
		return
	}
	args.PutResponse(antwort)
}

// navigationBeginnt: Was nicht zur Web-App des Servers oder zur
// eingebauten Seite gehört, öffnet der Standardbrowser (App.Wohin).
func (f *fenster) navigationBeginnt(_, args com) {
	adresse, ok := comText(args, navStartGetUri)
	if !ok {
		return
	}
	switch f.app.Wohin(adresse) {
	case WegErlauben:
		return
	case WegExtern:
		comBoolSetzen(args, navStartPutCancel, true)
		f.Extern(adresse)
	default:
		comBoolSetzen(args, navStartPutCancel, true)
		log.Printf("Navigation gesperrt: %s", adresse)
	}
}

// navigationFertig: Ging das Laden schief, weil der Server nicht
// antwortet, erscheint die Auswahl mit Hinweis statt einer Fehlerseite.
func (f *fenster) navigationFertig(sender, args com) {
	adresse, _ := comText(sender, wvGetSource)
	if comBool(args, navEndeIsSuccess) {
		if f.test != nil {
			f.test.geladen(adresse)
		}
		return
	}
	status := comZahl(args, navEndeErrorStatus)
	log.Printf("Laden fehlgeschlagen (%d): %s", status, adresse)
	switch status {
	case webFehlerUnerreichbar, webFehlerZeit, webFehlerAbgebrochen, webFehlerZurueckgesetzt,
		webFehlerGetrennt, webFehlerKeineVerbindung, webFehlerNameUnbekannt:
		f.app.LadenFehlgeschlagen(adresse, fehlerErreichbar)
	case webFehlerZertNameFalsch, webFehlerZertAbgelaufen, webFehlerZertWiderrufen, webFehlerZertUngueltig:
		f.app.LadenFehlgeschlagen(adresse, fehlerZertifikat)
	}
}

// neuesFenster: target="_blank" und window.open. Ein zweites Fenster gibt
// es nicht — Fremdes öffnet der Standardbrowser, Eigenes lädt im Fenster.
func (f *fenster) neuesFenster(_, args com) {
	adresse, ok := comText(args, neuFensterGetUri)
	comBoolSetzen(args, neuFensterPutHandled, true)
	if !ok || adresse == "" {
		return
	}
	switch f.app.Wohin(adresse) {
	case WegExtern:
		f.Extern(adresse)
	case WegErlauben:
		if strings.HasPrefix(adresse, "http") {
			f.Navigieren(adresse)
		}
	}
}

// ---------------------------------------------------------------- Host

func (f *fenster) Auftrag(fn func()) {
	f.mu.Lock()
	f.auftraege = append(f.auftraege, fn)
	f.mu.Unlock()
	pPostMessageW.Call(f.hwnd, wmAuftrag, 0, 0)
}

func (f *fenster) auftraegeAbarbeiten() {
	f.mu.Lock()
	liste := f.auftraege
	f.auftraege = nil
	f.mu.Unlock()
	for _, fn := range liste {
		fn()
	}
}

func (f *fenster) Navigieren(adresse string) {
	if f.bereit {
		f.chromium.Navigate(adresse)
	}
}

func (f *fenster) Skript(js string) {
	if f.bereit {
		f.chromium.Eval(js)
	}
}

// Extern öffnet im Standardbrowser — nur http, https und mailto, nie
// eine Datei oder ein anderes Programm.
func (f *fenster) Extern(adresse string) {
	u, err := url.Parse(adresse)
	if err != nil {
		return
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https", "mailto":
	default:
		return
	}
	if f.test != nil {
		f.test.extern(adresse)
		return
	}
	if err := shellOeffnen(u.String(), ""); err != nil {
		log.Printf("Öffnen von %s: %v", adresse, err)
	}
}

func (f *fenster) InstallerStarten(pfad string) error {
	return shellOeffnen(pfad, "")
}

func (f *fenster) Schliessen() {
	pPostMessageW.Call(f.hwnd, wmClose, 0, 0)
}
