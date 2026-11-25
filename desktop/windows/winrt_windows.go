// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package main

// WinRT ohne cgo: Laufzeitklassen sind COM-Objekte, deren Tabellen nach
// IUnknown (0–2) die drei Methoden von IInspectable (3–5) haben; die
// eigentlichen Methoden beginnen bei 6. Texte sind HSTRING (combase.dll),
// Fabriken holt RoGetActivationFactory. Die Nummern der Methoden stehen
// in den Metadaten von Windows (und gleich geordnet in windows-rs); sie
// ändern sich nie, neue Methoden kommen in neue Schnittstellen.
//
// Benutzt für die Mediensteuerung von Windows (mediensteuerung_windows.go).

import (
	"errors"
	"fmt"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	combase = windows.NewLazySystemDLL("combase.dll")
	shcore  = windows.NewLazySystemDLL("shcore.dll")

	pRoInitialize              = combase.NewProc("RoInitialize")
	pRoGetActivationFactory    = combase.NewProc("RoGetActivationFactory")
	pRoActivateInstance        = combase.NewProc("RoActivateInstance")
	pWindowsCreateString       = combase.NewProc("WindowsCreateString")
	pWindowsDeleteString       = combase.NewProc("WindowsDeleteString")
	pWindowsGetStringRawBuffer = combase.NewProc("WindowsGetStringRawBuffer")
	pCreateRAStreamOverStream  = shcore.NewProc("CreateRandomAccessStreamOverStream")
)

const (
	roInitSingle = 0 // RO_INIT_SINGLETHREADED
	roInitMulti  = 1 // RO_INIT_MULTITHREADED

	hrOK           = 0
	hrFalse        = 1
	eNoInterface   = 0x80004002
	rpcEChangedMod = 0x80010106
)

var (
	iidIUnknown    = windows.GUID{Data1: 0x00000000, Data2: 0x0000, Data3: 0x0000, Data4: [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidIAgile      = windows.GUID{Data1: 0x94ea2b94, Data2: 0xe9cc, Data3: 0x49e0, Data4: [8]byte{0xc0, 0xff, 0xee, 0x64, 0xca, 0x8f, 0x5b, 0x90}}
	iidIAsyncInfo  = windows.GUID{Data1: 0x00000036, Data2: 0x0000, Data3: 0x0000, Data4: [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidRAStream    = windows.GUID{Data1: 0x905a0fe1, Data2: 0xbc53, Data3: 0x11df, Data4: [8]byte{0x8c, 0x49, 0x00, 0x1e, 0x4f, 0xc6, 0x86, 0xda}}
	iidRAStreamRef = windows.GUID{Data1: 0x857309dc, Data2: 0x3fbf, Data3: 0x4e7d, Data4: [8]byte{0x98, 0x6f, 0xef, 0x3b, 0x1a, 0x07, 0xa9, 0x64}} // IRandomAccessStreamReferenceStatics
)

// winrtStarten: RoInitialize für diesen Faden. Ist COM schon eingerichtet
// (WebView2 macht den Fenster-Thread zum STA), ist das kein Fehler.
func winrtStarten(art uintptr) error {
	if err := combase.Load(); err != nil {
		return err
	}
	r, _, _ := pRoInitialize.Call(art)
	switch uint32(r) {
	case hrOK, hrFalse, rpcEChangedMod:
		return nil
	}
	return fmt.Errorf("RoInitialize: %#x", uint32(r))
}

// hstring legt einen HSTRING an; mit hstringLos wieder freigeben.
func hstring(s string) (uintptr, error) {
	u, err := windows.UTF16FromString(s)
	if err != nil {
		return 0, err
	}
	var h uintptr
	r, _, _ := pWindowsCreateString.Call(uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)-1), uintptr(unsafe.Pointer(&h)))
	if r != 0 {
		return 0, fmt.Errorf("WindowsCreateString: %#x", uint32(r))
	}
	return h, nil
}

func hstringLos(h uintptr) {
	if h != 0 {
		pWindowsDeleteString.Call(h)
	}
}

// hstringText liest einen HSTRING (der leere ist 0).
func hstringText(h uintptr) string {
	if h == 0 {
		return ""
	}
	var n uint32
	p, _, _ := pWindowsGetStringRawBuffer.Call(h, uintptr(unsafe.Pointer(&n)))
	if p == 0 || n == 0 {
		return ""
	}
	return windows.UTF16ToString(unsafe.Slice((*uint16)(zeigerVon(p)), n))
}

// comHText ruft eine Methode, die einen HSTRING zurückgibt.
func comHText(obj com, methode int) (string, bool) {
	var h uintptr
	if hr := comAufruf(obj, methode, uintptr(unsafe.Pointer(&h))); hr != 0 {
		return "", false
	}
	defer hstringLos(h)
	return hstringText(h), true
}

// comHTextSetzen ruft eine Methode, die einen HSTRING nimmt (put_Title …).
func comHTextSetzen(obj com, methode int, s string) uintptr {
	h, err := hstring(s)
	if err != nil {
		return 0x80070057 // E_INVALIDARG
	}
	defer hstringLos(h)
	return comAufruf(obj, methode, h)
}

// comObjekt ruft eine Methode, die eine Schnittstelle zurückgibt.
func comObjekt(obj com, methode int, args ...uintptr) (com, uintptr) {
	var p unsafe.Pointer
	hr := comAufruf(obj, methode, append(args, uintptr(unsafe.Pointer(&p)))...)
	if hr != 0 || p == nil {
		if hr == 0 {
			hr = 0x80004003 // E_POINTER
		}
		return nil, hr
	}
	return com(p), 0
}

// fabrik holt die Fabrik (oder die statischen Methoden) einer Laufzeitklasse.
func fabrik(klasse string, iid *windows.GUID) (com, error) {
	if err := combase.Load(); err != nil {
		return nil, err
	}
	h, err := hstring(klasse)
	if err != nil {
		return nil, err
	}
	defer hstringLos(h)
	var p unsafe.Pointer
	r, _, _ := pRoGetActivationFactory.Call(h, uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&p)))
	if r != 0 || p == nil {
		return nil, fmt.Errorf("RoGetActivationFactory(%s): %#x", klasse, uint32(r))
	}
	return com(p), nil
}

// anlegen legt ein Objekt einer Laufzeitklasse mit leerem Konstruktor an
// und gibt die gewünschte Schnittstelle zurück.
func anlegen(klasse string, iid *windows.GUID) (com, error) {
	h, err := hstring(klasse)
	if err != nil {
		return nil, err
	}
	defer hstringLos(h)
	var p unsafe.Pointer
	r, _, _ := pRoActivateInstance.Call(h, uintptr(unsafe.Pointer(&p)))
	if r != 0 || p == nil {
		return nil, fmt.Errorf("RoActivateInstance(%s): %#x", klasse, uint32(r))
	}
	insp := com(p)
	defer comLos(insp)
	o, ok := comFrage(insp, iid)
	if !ok {
		return nil, fmt.Errorf("%s: Schnittstelle fehlt", klasse)
	}
	return o, nil
}

// asyncWarten wartet, bis eine IAsyncOperation fertig ist (IAsyncInfo:
// get_Status = 7, get_ErrorCode = 8). Danach liefert GetResults (8 der
// Operation selbst) das Ergebnis. Nur in einem Faden, der warten darf —
// nie im Fenster-Thread.
func asyncWarten(op com, frist time.Duration) error {
	info, ok := comFrage(op, &iidIAsyncInfo)
	if !ok {
		return errors.New("keine IAsyncInfo")
	}
	defer comLos(info)
	ende := time.Now().Add(frist)
	for {
		var status int32
		if hr := comAufruf(info, 7, uintptr(unsafe.Pointer(&status))); hr != 0 {
			return fmt.Errorf("IAsyncInfo.Status: %#x", uint32(hr))
		}
		switch status {
		case 1: // Completed
			return nil
		case 2:
			return errors.New("abgebrochen")
		case 3:
			var code uint32
			comAufruf(info, 8, uintptr(unsafe.Pointer(&code)))
			return fmt.Errorf("Fehler %#x", code)
		}
		if time.Now().After(ende) {
			return errors.New("Zeit abgelaufen")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// ---------------------------------------------------------------- Delegat
//
// Ein WinRT-Delegat (TypedEventHandler<Sender, Args>): IUnknown plus
// Invoke(sender, args). Anders als die Empfänger für WebView2
// (com_windows.go) antwortet er nur auf IUnknown, IAgileObject und seine
// eigene Kennung — WinRT fragt auch nach IMarshal & Co., und darauf mit
// „ja" zu antworten, ginge schief. IAgileObject sagt: aus jedem Faden
// aufrufbar. Das stimmt, Invoke reicht nur an den Fenster-Thread weiter.
// Die Objekte leben, solange das Programm läuft (alleDelegaten).

type delegat struct {
	tabelle *[4]uintptr
	iid     windows.GUID
	aufruf  func(sender, args com)
}

var (
	alleDelegaten   []*delegat
	delegatMu       sync.Mutex
	delegatTabelle  *[4]uintptr
	delegatTabelleE sync.Once
)

func neuerDelegat(iid windows.GUID, fn func(sender, args com)) *delegat {
	delegatTabelleE.Do(func() {
		qi := windows.NewCallback(func(this *delegat, iid *windows.GUID, ziel *uintptr) uintptr {
			if ziel == nil {
				return 0x80004003
			}
			if iid != nil && (*iid == iidIUnknown || *iid == iidIAgile || *iid == this.iid) {
				*ziel = uintptr(unsafe.Pointer(this))
				return 0
			}
			*ziel = 0
			return eNoInterface
		})
		ref := windows.NewCallback(func(*delegat) uintptr { return 1 })
		delegatTabelle = &[4]uintptr{qi, ref, ref, windows.NewCallback(func(this *delegat, sender, args uintptr) uintptr {
			this.aufruf(*(*com)(unsafe.Pointer(&sender)), *(*com)(unsafe.Pointer(&args)))
			return 0
		})}
	})
	d := &delegat{tabelle: delegatTabelle, iid: iid, aufruf: fn}
	delegatMu.Lock()
	alleDelegaten = append(alleDelegaten, d)
	delegatMu.Unlock()
	return d
}

// ---------------------------------------------------------------- Bilder

// raStromAusBytes macht aus Bilddaten einen IRandomAccessStream (über
// einen IStream im Speicher, wie Chromium es für seine Mediensteuerung
// tut). Mit comLos freigeben.
func raStromAusBytes(daten []byte) (com, error) {
	if len(daten) == 0 {
		return nil, errors.New("keine Daten")
	}
	if err := shcore.Load(); err != nil {
		return nil, err
	}
	var p unsafe.Pointer
	if r, _, _ := pCreateStreamOnHGlob.Call(0, 1, uintptr(unsafe.Pointer(&p))); r != 0 || p == nil {
		return nil, fmt.Errorf("CreateStreamOnHGlobal: %#x", uint32(r))
	}
	strom := com(p)
	defer comLos(strom)
	var geschrieben uint32
	// ISequentialStream::Write = 4, IStream::Seek = 5.
	if hr := comAufruf(strom, 4, uintptr(unsafe.Pointer(&daten[0])), uintptr(len(daten)), uintptr(unsafe.Pointer(&geschrieben))); hr != 0 || int(geschrieben) != len(daten) {
		return nil, fmt.Errorf("IStream.Write: %#x", uint32(hr))
	}
	if hr := comAufruf(strom, 5, 0, 0, 0); hr != 0 { // an den Anfang (STREAM_SEEK_SET)
		return nil, fmt.Errorf("IStream.Seek: %#x", uint32(hr))
	}
	var ra unsafe.Pointer
	if r, _, _ := pCreateRAStreamOverStream.Call(uintptr(strom), 0, uintptr(unsafe.Pointer(&iidRAStream)), uintptr(unsafe.Pointer(&ra))); r != 0 || ra == nil {
		return nil, fmt.Errorf("CreateRandomAccessStreamOverStream: %#x", uint32(r))
	}
	return com(ra), nil
}
