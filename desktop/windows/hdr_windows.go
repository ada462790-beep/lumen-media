// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package main

// Ist HDR in Windows an — auf dem Bildschirm, auf dem Lumen gerade steht?
// (Einstellungen › Anzeige › „HDR verwenden"). Davon hängt ab, ob mpv ein
// HDR-Bild an Windows durchreicht (target-colorspace-hint) oder es selbst
// auf SDR abbildet (Tone-Mapping). Gefragt wird über QueryDisplayConfig
// und DISPLAYCONFIG_DEVICE_INFO_GET_ADVANCED_COLOR_INFO.

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	pGetDisplayConfigBufferSizes = user32.NewProc("GetDisplayConfigBufferSizes")
	pQueryDisplayConfig          = user32.NewProc("QueryDisplayConfig")
	pDisplayConfigGetDeviceInfo  = user32.NewProc("DisplayConfigGetDeviceInfo")
)

const (
	qdcNurAktive         = 0x2 // QDC_ONLY_ACTIVE_PATHS
	dcInfoQuellName      = 1   // DISPLAYCONFIG_DEVICE_INFO_GET_SOURCE_NAME
	dcInfoFarbe          = 9   // DISPLAYCONFIG_DEVICE_INFO_GET_ADVANCED_COLOR_INFO
	monitorInfoExGroesse = 104 // MONITORINFOEXW
)

type dcLUID struct {
	Low  uint32
	High int32
}

// dcPfad ist DISPLAYCONFIG_PATH_INFO (72 Bytes).
type dcPfad struct {
	QuelleAdapter dcLUID
	QuelleID      uint32
	QuelleMode    uint32
	QuelleFlags   uint32
	ZielAdapter   dcLUID
	ZielID        uint32
	ZielMode      uint32
	Technik       uint32
	Drehung       uint32
	Skalierung    uint32
	RateZaehler   uint32
	RateNenner    uint32
	Zeilen        uint32
	Verfuegbar    int32
	ZielFlags     uint32
	Flags         uint32
}

type dcKopf struct {
	Typ     uint32
	Groesse uint32
	Adapter dcLUID
	ID      uint32
}

type dcQuellName struct {
	Kopf dcKopf
	Name [32]uint16
}

type dcFarbe struct {
	Kopf      dcKopf
	Wert      uint32 // Bit 0: HDR möglich, Bit 1: HDR an
	Kodierung uint32
	Bits      uint32
}

// hdrStand: ob HDR auf dem Bildschirm des Fensters an ist. bekannt ist
// false, wenn Windows es nicht sagt (alte Fassung, kein Bildschirm).
func hdrStand(hwnd uintptr) (an, moeglich, bekannt bool, text string) {
	if err := pQueryDisplayConfig.Find(); err != nil {
		return false, false, false, "QueryDisplayConfig fehlt"
	}
	// Name des Bildschirms (\\.\DISPLAY1), auf dem das Fenster steht.
	var mi [monitorInfoExGroesse]byte
	*(*uint32)(unsafe.Pointer(&mi[0])) = monitorInfoExGroesse
	mon, _, _ := pMonitorFromWindow.Call(hwnd, monitorDefaultNah)
	pGetMonitorInfoW.Call(mon, uintptr(unsafe.Pointer(&mi[0])))
	name := windows.UTF16ToString((*[32]uint16)(unsafe.Pointer(&mi[40]))[:])

	var nPfade, nModi uint32
	if r, _, _ := pGetDisplayConfigBufferSizes.Call(qdcNurAktive, uintptr(unsafe.Pointer(&nPfade)), uintptr(unsafe.Pointer(&nModi))); r != 0 || nPfade == 0 {
		return false, false, false, fmt.Sprintf("GetDisplayConfigBufferSizes: %d", r)
	}
	pfade := make([]dcPfad, nPfade)
	modi := make([][64]byte, nModi+1)
	if r, _, _ := pQueryDisplayConfig.Call(qdcNurAktive, uintptr(unsafe.Pointer(&nPfade)), uintptr(unsafe.Pointer(&pfade[0])),
		uintptr(unsafe.Pointer(&nModi)), uintptr(unsafe.Pointer(&modi[0])), 0); r != 0 {
		return false, false, false, fmt.Sprintf("QueryDisplayConfig: %d", r)
	}
	gefunden := false
	for _, p := range pfade[:nPfade] {
		q := dcQuellName{Kopf: dcKopf{Typ: dcInfoQuellName, Groesse: uint32(unsafe.Sizeof(dcQuellName{})), Adapter: p.QuelleAdapter, ID: p.QuelleID}}
		if r, _, _ := pDisplayConfigGetDeviceInfo.Call(uintptr(unsafe.Pointer(&q))); r != 0 {
			continue
		}
		qn := windows.UTF16ToString(q.Name[:])
		f := dcFarbe{Kopf: dcKopf{Typ: dcInfoFarbe, Groesse: uint32(unsafe.Sizeof(dcFarbe{})), Adapter: p.ZielAdapter, ID: p.ZielID}}
		if r, _, _ := pDisplayConfigGetDeviceInfo.Call(uintptr(unsafe.Pointer(&f))); r != 0 {
			continue
		}
		dieser := name == "" || qn == name
		if dieser || !gefunden {
			an, moeglich, bekannt = f.Wert&2 != 0, f.Wert&1 != 0, true
			text = fmt.Sprintf("%s: HDR %s, %d Bit", qn, map[bool]string{true: "an", false: map[bool]string{true: "aus (möglich)", false: "nicht möglich"}[moeglich]}[an], f.Bits)
			gefunden = true
		}
		if dieser {
			break
		}
	}
	if !bekannt {
		text = "kein Bildschirm mit Farbangaben"
	}
	return
}
