// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package main

// Audio-Sitzungen (Core Audio): Wem gehört der Ton, den Lumen spielt?
//
// Windows führt für jeden Prozess, der ein Audiogerät öffnet, eine Sitzung
// (Lautstärkemixer, und daran halten sich Programme wie Logi MIXLINE). Mit
// libmpv in Lumen.exe (player_windows.go) gehört sie Lumen.exe. Diese Datei
// listet die Sitzungen aller Ausgabegeräte (IAudioSessionManager2) — für den
// Selbsttest, der nachsieht, welchem Prozess sie gehört — und gibt den
// Sitzungen von Lumen.exe Name und Symbol.

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	pCoCreateInstance = ole32.NewProc("CoCreateInstance")

	clsidMMDeviceEnumerator = windows.GUID{Data1: 0xBCDE0395, Data2: 0xE52F, Data3: 0x467C, Data4: [8]byte{0x8E, 0x3D, 0xC4, 0x57, 0x92, 0x91, 0x69, 0x2E}}
	iidIMMDeviceEnumerator  = windows.GUID{Data1: 0xA95664D2, Data2: 0x9614, Data3: 0x4F35, Data4: [8]byte{0xA7, 0x46, 0xDE, 0x8D, 0xB6, 0x36, 0x17, 0xE6}}
	iidIAudioSessionMgr2    = windows.GUID{Data1: 0x77AA99A0, Data2: 0x1BD6, Data3: 0x484F, Data4: [8]byte{0x8B, 0xC7, 0x2C, 0x65, 0x4C, 0x9A, 0x9B, 0x6F}}
	iidIAudioSessionCtl2    = windows.GUID{Data1: 0xBFB7FF88, Data2: 0x7239, Data3: 0x4FC9, Data4: [8]byte{0x8F, 0xA2, 0x07, 0xC9, 0x50, 0xBE, 0x9C, 0x6D}}
)

// Methodennummern (Audioclient.h, Audiopolicy.h, Mmdeviceapi.h).
const (
	enumEnumAudioEndpoints = 3 // IMMDeviceEnumerator
	sammlungAnzahl         = 3 // IMMDeviceCollection::GetCount
	sammlungEintrag        = 4 // IMMDeviceCollection::Item
	geraetActivate         = 3 // IMMDevice::Activate
	geraetGetID            = 5
	mgr2GetSessionEnum     = 5 // IAudioSessionManager2::GetSessionEnumerator
	sitzEnumAnzahl         = 3 // IAudioSessionEnumerator::GetCount
	sitzEnumSitzung        = 4 // IAudioSessionEnumerator::GetSession
	sitzGetState           = 3 // IAudioSessionControl
	sitzGetDisplayName     = 4
	sitzSetDisplayName     = 5
	sitzGetIconPath        = 6
	sitzSetIconPath        = 7
	sitz2GetProcessID      = 14 // IAudioSessionControl2
	sitz2IsSystemSounds    = 15

	eRender     = 0
	deviceAktiv = 1
	clsctxAll   = 0x17
	coinitMulti = 0x0
	sOK         = 0
	sFalse      = 1
)

// tonsitzung: eine Sitzung, wie der Mixer sie kennt.
type tonsitzung struct {
	Geraet  string
	PID     uint32
	Prozess string
	Name    string
	Symbol  string
	Zustand uint32 // 0 inaktiv, 1 aktiv, 2 abgelaufen
	System  bool
}

// mitSitzungen ruft fn für jede Sitzung jedes aktiven Ausgabegeräts.
// Läuft in einem eigenen, für COM vorbereiteten Faden.
func mitSitzungen(fn func(geraet string, s com)) error {
	fertig := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		// Ein frischer Faden: S_OK. (S_FALSE hiesse „schon da" — auch gut.)
		if err := windows.CoInitializeEx(0, coinitMulti); err == nil || errors.Is(err, windows.Errno(sFalse)) {
			defer windows.CoUninitialize()
		}
		fertig <- sitzungenDurchgehen(fn)
	}()
	return <-fertig
}

func sitzungenDurchgehen(fn func(string, com)) error {
	var p unsafe.Pointer
	if hr, _, _ := pCoCreateInstance.Call(uintptr(unsafe.Pointer(&clsidMMDeviceEnumerator)), 0, clsctxAll,
		uintptr(unsafe.Pointer(&iidIMMDeviceEnumerator)), uintptr(unsafe.Pointer(&p))); hr != sOK || p == nil {
		return fmt.Errorf("MMDeviceEnumerator: %#x", hr)
	}
	aufzaehler := com(p)
	defer comLos(aufzaehler)

	var sp unsafe.Pointer
	if hr := comAufruf(aufzaehler, enumEnumAudioEndpoints, eRender, deviceAktiv, uintptr(unsafe.Pointer(&sp))); hr != sOK || sp == nil {
		return fmt.Errorf("EnumAudioEndpoints: %#x", hr)
	}
	sammlung := com(sp)
	defer comLos(sammlung)
	var n uint32
	comAufruf(sammlung, sammlungAnzahl, uintptr(unsafe.Pointer(&n)))
	if n == 0 {
		return errors.New("kein Ausgabegerät")
	}
	for i := uint32(0); i < n; i++ {
		var gp unsafe.Pointer
		if hr := comAufruf(sammlung, sammlungEintrag, uintptr(i), uintptr(unsafe.Pointer(&gp))); hr != sOK || gp == nil {
			continue
		}
		geraet := com(gp)
		name, _ := comText(geraet, geraetGetID)
		var mp unsafe.Pointer
		if hr := comAufruf(geraet, geraetActivate, uintptr(unsafe.Pointer(&iidIAudioSessionMgr2)), clsctxAll, 0, uintptr(unsafe.Pointer(&mp))); hr == sOK && mp != nil {
			mgr := com(mp)
			var ep unsafe.Pointer
			if hr := comAufruf(mgr, mgr2GetSessionEnum, uintptr(unsafe.Pointer(&ep))); hr == sOK && ep != nil {
				liste := com(ep)
				var k int32
				comAufruf(liste, sitzEnumAnzahl, uintptr(unsafe.Pointer(&k)))
				for j := int32(0); j < k; j++ {
					var cp unsafe.Pointer
					if hr := comAufruf(liste, sitzEnumSitzung, uintptr(j), uintptr(unsafe.Pointer(&cp))); hr != sOK || cp == nil {
						continue
					}
					ctl := com(cp)
					if c2, ok := comFrage(ctl, &iidIAudioSessionCtl2); ok {
						fn(name, c2)
						comLos(c2)
					}
					comLos(ctl)
				}
				comLos(liste)
			}
			comLos(mgr)
		}
		comLos(geraet)
	}
	return nil
}

// tonsitzungenListe: alle Sitzungen aller Ausgabegeräte.
func tonsitzungenListe() ([]tonsitzung, error) {
	var aus []tonsitzung
	err := mitSitzungen(func(geraet string, s com) {
		t := tonsitzung{Geraet: geraet}
		comAufruf(s, sitz2GetProcessID, uintptr(unsafe.Pointer(&t.PID)))
		t.Name, _ = comText(s, sitzGetDisplayName)
		t.Symbol, _ = comText(s, sitzGetIconPath)
		comAufruf(s, sitzGetState, uintptr(unsafe.Pointer(&t.Zustand)))
		t.System = comAufruf(s, sitz2IsSystemSounds) == sOK
		t.Prozess = prozessName(t.PID)
		aus = append(aus, t)
	})
	return aus, err
}

// tonsitzungenBenennen gibt allen Sitzungen dieses Prozesses Name und
// Symbol von Lumen.
func tonsitzungenBenennen(name string) (int, error) {
	eigen := uint32(os.Getpid())
	exe, _ := os.Executable()
	symbol, _ := windows.UTF16PtrFromString(exe + ",0")
	titel, _ := windows.UTF16PtrFromString(name)
	n := 0
	err := mitSitzungen(func(_ string, s com) {
		var pid uint32
		comAufruf(s, sitz2GetProcessID, uintptr(unsafe.Pointer(&pid)))
		if pid != eigen {
			return
		}
		comAufruf(s, sitzSetDisplayName, uintptr(unsafe.Pointer(titel)), 0)
		comAufruf(s, sitzSetIconPath, uintptr(unsafe.Pointer(symbol)), 0)
		n++
	})
	return n, err
}

// prozessName: der Name der .exe zu einer Prozessnummer.
func prozessName(pid uint32) string {
	if pid == 0 {
		return "System"
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return "?"
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, windows.MAX_PATH)
	n := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &n); err != nil {
		return "?"
	}
	p := windows.UTF16ToString(buf[:n])
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '\\' {
			return p[i+1:]
		}
	}
	return p
}
