// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package main

// Die paar WebView2-Schnittstellen, die das Paket edge nicht anbietet
// (Navigation abfangen, neue Fenster, Bilder für den Selbsttest), direkt
// über ihre Methodentabellen. Die Nummern stehen in WebView2.h von
// Microsoft; sie ändern sich nie, neue Methoden kommen in neue
// Schnittstellen.

import (
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Methoden von ICoreWebView2.
const (
	wvGetSource              = 4
	wvAddNavigationStarting  = 7
	wvAddNavigationCompleted = 15
	wvCapturePreview         = 30
	wvAddNewWindowRequested  = 44
	wvGetDocumentTitle       = 48
)

// Methoden von ICoreWebView2Settings2.
const (
	settingsGetUserAgent = 21
	settingsPutUserAgent = 22
)

// Methoden der Ereignis-Argumente.
const (
	navStartGetUri       = 3
	navStartIsRedirected = 5
	navStartPutCancel    = 8
	navEndeIsSuccess     = 3
	navEndeErrorStatus   = 4
	neuFensterGetUri     = 3
	neuFensterPutHandled = 6
)

// WebView2-Fehlerarten (COREWEBVIEW2_WEB_ERROR_STATUS).
const (
	webFehlerZertNameFalsch  = 1
	webFehlerZertAbgelaufen  = 2
	webFehlerZertWiderrufen  = 4
	webFehlerZertUngueltig   = 5
	webFehlerUnerreichbar    = 6
	webFehlerZeit            = 7
	webFehlerAbgebrochen     = 9
	webFehlerZurueckgesetzt  = 10
	webFehlerGetrennt        = 11
	webFehlerKeineVerbindung = 12
	webFehlerNameUnbekannt   = 13
)

// com ist ein COM-Objekt: ein Zeiger auf einen Zeiger auf die Tabelle.
type com unsafe.Pointer

func comAufruf(obj com, methode int, args ...uintptr) uintptr {
	tabelle := *(**[64]uintptr)(obj)
	r, _, _ := syscall.SyscallN(tabelle[methode], append([]uintptr{uintptr(obj)}, args...)...)
	return r
}

// comText ruft eine Methode, die einen mit CoTaskMemAlloc angelegten
// Text zurückgibt, und gibt ihn wieder frei.
func comText(obj com, methode int) (string, bool) {
	var p *uint16
	if hr := comAufruf(obj, methode, uintptr(unsafe.Pointer(&p))); hr != 0 || p == nil {
		return "", false
	}
	defer windows.CoTaskMemFree(unsafe.Pointer(p))
	return windows.UTF16PtrToString(p), true
}

func comBool(obj com, methode int) bool {
	var b int32
	if hr := comAufruf(obj, methode, uintptr(unsafe.Pointer(&b))); hr != 0 {
		return false
	}
	return b != 0
}

func comZahl(obj com, methode int) uint32 {
	var n uint32
	comAufruf(obj, methode, uintptr(unsafe.Pointer(&n)))
	return n
}

func comBoolSetzen(obj com, methode int, wert bool) {
	b := uintptr(0)
	if wert {
		b = 1
	}
	comAufruf(obj, methode, b)
}

// ---------------------------------------------------------------- Empfänger

// empfaenger ist ein Ereignis-Empfänger für WebView2: eine Tabelle mit
// QueryInterface, AddRef, Release, Invoke. Die Objekte bleiben in
// allenEmpfaengern, solange das Programm läuft — WebView2 hält Zeiger
// auf sie, und Go verschiebt Objekte auf dem Heap nicht.
type empfaenger struct {
	tabelle *[4]uintptr
	aufruf  func(a, b uintptr) uintptr
}

var (
	allenEmpfaengern []*empfaenger
	tabelleZwei      *[4]uintptr // Invoke(this, a, b)
	tabelleEins      *[4]uintptr // Invoke(this, a)
)

func init() {
	qi := windows.NewCallback(func(this *empfaenger, _ uintptr, ziel *uintptr) uintptr {
		if ziel != nil {
			*ziel = uintptr(unsafe.Pointer(this))
		}
		return 0
	})
	ref := windows.NewCallback(func(*empfaenger) uintptr { return 1 })
	tabelleZwei = &[4]uintptr{qi, ref, ref, windows.NewCallback(func(this *empfaenger, a, b uintptr) uintptr {
		return this.aufruf(a, b)
	})}
	tabelleEins = &[4]uintptr{qi, ref, ref, windows.NewCallback(func(this *empfaenger, a uintptr) uintptr {
		return this.aufruf(a, 0)
	})}
}

// ereignis legt einen Empfänger für Invoke(sender, args) an.
func ereignis(fn func(sender, args com)) *empfaenger {
	e := &empfaenger{tabelle: tabelleZwei, aufruf: func(a, b uintptr) uintptr {
		fn(*(*com)(unsafe.Pointer(&a)), *(*com)(unsafe.Pointer(&b)))
		return 0
	}}
	allenEmpfaengern = append(allenEmpfaengern, e)
	return e
}

// fertig legt einen Empfänger für Invoke(hresult) an.
func fertig(fn func(hresult uintptr)) *empfaenger {
	e := &empfaenger{tabelle: tabelleEins, aufruf: func(a, _ uintptr) uintptr {
		fn(a)
		return 0
	}}
	allenEmpfaengern = append(allenEmpfaengern, e)
	return e
}

// anmelden meldet einen Empfänger bei einem add_…-Ereignis an.
func anmelden(wv com, methode int, e *empfaenger) bool {
	var token int64
	return comAufruf(wv, methode, uintptr(unsafe.Pointer(e)), uintptr(unsafe.Pointer(&token))) == 0
}
