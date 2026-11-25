// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package main

// Die Mediensteuerung von Windows (System Media Transport Controls): Titel,
// Folge, Poster und Abspielstand im Medien-Overlay (Lautstärketasten,
// Schnelleinstellungen), auf dem Sperrbildschirm und für die Medientasten
// der Tastatur — während mpv spielt.
//
// Im Browser-Player erledigt das WebView2 (Media Session API der Web-App).
// Spielt mpv, klingt in der Seite nichts; Chromium meldet dann keine
// Sitzung. Darum meldet Lumen.exe sie selbst an, über
// ISystemMediaTransportControlsInterop::GetForWindow — der Weg für
// Win32-Programme, die kein CoreWindow haben. Die Knöpfe (auch die
// Medientasten, die Windows an die laufende Sitzung schickt) kommen als
// ButtonPressed in einem Faden von Windows an und gehen von dort an den
// Fenster-Thread (spieler.smtcKnopf).
//
// Alle Methoden ausser dem Ereignis laufen im Fenster-Thread.

import (
	"fmt"
	"log"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Schnittstellen-Kennungen (Windows.Media).
var (
	iidSMTCInterop   = windows.GUID{Data1: 0xddb0472d, Data2: 0xc911, Data3: 0x4a1f, Data4: [8]byte{0x86, 0xd9, 0xdc, 0x3d, 0x71, 0xa9, 0x5f, 0x5a}}
	iidSMTC          = windows.GUID{Data1: 0x99fa3ff4, Data2: 0x1742, Data3: 0x42a6, Data4: [8]byte{0x90, 0x2e, 0x08, 0x7d, 0x41, 0xf9, 0x65, 0xec}}
	iidSMTC2         = windows.GUID{Data1: 0xea98d2f6, Data2: 0x7f3c, Data3: 0x4af2, Data4: [8]byte{0xa5, 0x86, 0x72, 0x88, 0x98, 0x08, 0xef, 0xb1}}
	iidSMTCZeitleist = windows.GUID{Data1: 0x5125316a, Data2: 0xc3a2, Data3: 0x475b, Data4: [8]byte{0x85, 0x07, 0x93, 0x53, 0x4d, 0xc8, 0x8f, 0x15}}
	// TypedEventHandler<SystemMediaTransportControls,
	// SystemMediaTransportControlsButtonPressedEventArgs> — die Kennung
	// ergibt sich aus der Signatur (pinterface, SHA-1), wie in windows.media.h.
	iidSMTCKnopfDelegat = windows.GUID{Data1: 0x0557e996, Data2: 0x7b23, Data3: 0x5bae, Data4: [8]byte{0xaa, 0x81, 0xea, 0x0d, 0x67, 0x11, 0x43, 0xa4}}
)

// Methoden (nach den sechs von IUnknown und IInspectable).
const (
	smtcPutPlaybackStatus  = 7
	smtcGetDisplayUpdater  = 8
	smtcPutIsEnabled       = 11
	smtcPutIsPlayEnabled   = 13
	smtcPutIsStopEnabled   = 15
	smtcPutIsPauseEnabled  = 17
	smtcPutIsPrevEnabled   = 25
	smtcPutIsNextEnabled   = 27
	smtcAddButtonPressed   = 32
	smtc2UpdateTimeline    = 12
	anzeigePutType         = 7
	anzeigePutThumbnail    = 11
	anzeigeGetVideo        = 13
	anzeigeClearAll        = 16
	anzeigeUpdate          = 17
	videoPutTitle          = 7
	videoPutSubtitle       = 9
	knopfArgsGetButton     = 6
	interopGetForWindow    = 6
	raRefCreateFromStream  = 8
	zeitleistePutStart     = 7
	zeitleistePutEnde      = 9
	zeitleistePutMinSprung = 11
	zeitleistePutMaxSprung = 13
	zeitleistePutPosition  = 15
)

// MediaPlaybackStatus.
const (
	smtcGeschlossen = 0
	smtcGestoppt    = 2
	smtcSpielt      = 3
	smtcPausiert    = 4
)

// SystemMediaTransportControlsButton.
const (
	smtcKnopfPlay     = 0
	smtcKnopfPause    = 1
	smtcKnopfStop     = 2
	smtcKnopfNext     = 6
	smtcKnopfPrevious = 7
)

const mediaTypVideo = 2 // MediaPlaybackType.Video

type mediensteuerung struct {
	smtc       com // ISystemMediaTransportControls
	smtc2      com // ISystemMediaTransportControls2 (Zeitleiste), kann fehlen
	refStatics com // IRandomAccessStreamReferenceStatics
	zeitleiste com // SystemMediaTransportControlsTimelineProperties, kann fehlen
	an         bool
	status     int
}

// neueMediensteuerung meldet Lumen beim Fenster hwnd an. knopf läuft in
// einem Faden von Windows, nicht im Fenster-Thread.
func neueMediensteuerung(hwnd uintptr, knopf func(int)) (*mediensteuerung, error) {
	if err := winrtStarten(roInitSingle); err != nil {
		return nil, err
	}
	interop, err := fabrik("Windows.Media.SystemMediaTransportControls", &iidSMTCInterop)
	if err != nil {
		return nil, err
	}
	defer comLos(interop)
	smtc, hr := comObjekt(interop, interopGetForWindow, hwnd, uintptr(unsafe.Pointer(&iidSMTC)))
	if smtc == nil {
		return nil, fmt.Errorf("GetForWindow: %#x", uint32(hr))
	}
	m := &mediensteuerung{smtc: smtc, status: smtcGeschlossen}
	m.refStatics, err = fabrik("Windows.Storage.Streams.RandomAccessStreamReference", &iidRAStreamRef)
	if err != nil {
		log.Printf("Mediensteuerung: ohne Bilder (%v)", err)
	}
	if s2, ok := comFrage(smtc, &iidSMTC2); ok {
		m.smtc2 = s2
		if z, err := anlegen("Windows.Media.SystemMediaTransportControlsTimelineProperties", &iidSMTCZeitleist); err == nil {
			m.zeitleiste = z
		} else {
			log.Printf("Mediensteuerung: ohne Zeitleiste (%v)", err)
		}
	}
	d := neuerDelegat(iidSMTCKnopfDelegat, func(_, args com) {
		if args == nil {
			return
		}
		var k int32
		if comAufruf(args, knopfArgsGetButton, uintptr(unsafe.Pointer(&k))) == 0 {
			knopf(int(k))
		}
	})
	var token int64
	if hr := comAufruf(smtc, smtcAddButtonPressed, uintptr(unsafe.Pointer(d)), uintptr(unsafe.Pointer(&token))); hr != 0 {
		comLos(smtc)
		return nil, fmt.Errorf("add_ButtonPressed: %#x", uint32(hr))
	}
	// Bis etwas spielt: aus.
	comBoolSetzen(smtc, smtcPutIsEnabled, false)
	return m, nil
}

// zeigen: Eine Wiedergabe beginnt — Titel und Zeile darunter (Serie,
// Staffel und Folge), Knöpfe an.
func (m *mediensteuerung) zeigen(titel, unter string, naechste bool) {
	if m == nil {
		return
	}
	comBoolSetzen(m.smtc, smtcPutIsEnabled, true)
	comBoolSetzen(m.smtc, smtcPutIsPlayEnabled, true)
	comBoolSetzen(m.smtc, smtcPutIsPauseEnabled, true)
	comBoolSetzen(m.smtc, smtcPutIsStopEnabled, true)
	comBoolSetzen(m.smtc, smtcPutIsPrevEnabled, true)
	comBoolSetzen(m.smtc, smtcPutIsNextEnabled, naechste)
	m.an = true
	anzeige, hr := comObjekt(m.smtc, smtcGetDisplayUpdater)
	if anzeige == nil {
		log.Printf("Mediensteuerung: DisplayUpdater: %#x", uint32(hr))
		return
	}
	defer comLos(anzeige)
	comAufruf(anzeige, anzeigeClearAll)
	comAufruf(anzeige, anzeigePutType, mediaTypVideo)
	if video, hr := comObjekt(anzeige, anzeigeGetVideo); video != nil {
		if titel == "" {
			titel = "Lumen"
		}
		comHTextSetzen(video, videoPutTitle, titel)
		comHTextSetzen(video, videoPutSubtitle, unter)
		comLos(video)
	} else {
		log.Printf("Mediensteuerung: VideoProperties: %#x", uint32(hr))
	}
	if hr := comAufruf(anzeige, anzeigeUpdate); hr != 0 {
		log.Printf("Mediensteuerung: Update: %#x", uint32(hr))
	}
}

// bild setzt das Poster (Bilddaten, wie vom Server geladen).
func (m *mediensteuerung) bild(daten []byte) error {
	if m == nil || !m.an {
		return nil
	}
	if m.refStatics == nil {
		return fmt.Errorf("keine RandomAccessStreamReference")
	}
	ra, err := raStromAusBytes(daten)
	if err != nil {
		return err
	}
	defer comLos(ra)
	ref, hr := comObjekt(m.refStatics, raRefCreateFromStream, uintptr(ra))
	if ref == nil {
		return fmt.Errorf("CreateFromStream: %#x", uint32(hr))
	}
	defer comLos(ref)
	anzeige, hr := comObjekt(m.smtc, smtcGetDisplayUpdater)
	if anzeige == nil {
		return fmt.Errorf("DisplayUpdater: %#x", uint32(hr))
	}
	defer comLos(anzeige)
	if hr := comAufruf(anzeige, anzeigePutThumbnail, uintptr(ref)); hr != 0 {
		return fmt.Errorf("put_Thumbnail: %#x", uint32(hr))
	}
	if hr := comAufruf(anzeige, anzeigeUpdate); hr != 0 {
		return fmt.Errorf("Update: %#x", uint32(hr))
	}
	return nil
}

// stand: spielt, pausiert, gestoppt.
func (m *mediensteuerung) stand(status int) {
	if m == nil || !m.an || status == m.status {
		return
	}
	m.status = status
	if hr := comAufruf(m.smtc, smtcPutPlaybackStatus, uintptr(status)); hr != 0 {
		log.Printf("Mediensteuerung: PlaybackStatus: %#x", uint32(hr))
	}
}

// zeit: Stelle und Länge für die Leiste im Overlay (Windows 11). TimeSpan
// zählt in 100 ns.
func (m *mediensteuerung) zeit(pos, dauer float64) {
	if m == nil || !m.an || m.smtc2 == nil || m.zeitleiste == nil || dauer <= 0 {
		return
	}
	if pos < 0 {
		pos = 0
	}
	if pos > dauer {
		pos = dauer
	}
	t := func(s float64) uintptr { return uintptr(int64(s * 1e7)) }
	z := m.zeitleiste
	comAufruf(z, zeitleistePutStart, 0)
	comAufruf(z, zeitleistePutEnde, t(dauer))
	comAufruf(z, zeitleistePutMinSprung, 0)
	comAufruf(z, zeitleistePutMaxSprung, t(dauer))
	comAufruf(z, zeitleistePutPosition, t(pos))
	if hr := comAufruf(m.smtc2, smtc2UpdateTimeline, uintptr(z)); hr != 0 {
		log.Printf("Mediensteuerung: Zeitleiste: %#x", uint32(hr))
		m.smtc2 = nil // nicht bei jeder Meldung wieder
	}
}

// aus: Die Wiedergabe ist vorbei — Lumen verschwindet aus dem Overlay.
func (m *mediensteuerung) aus() {
	if m == nil || !m.an {
		return
	}
	m.an = false
	comAufruf(m.smtc, smtcPutPlaybackStatus, smtcGeschlossen)
	m.status = smtcGeschlossen
	if anzeige, _ := comObjekt(m.smtc, smtcGetDisplayUpdater); anzeige != nil {
		comAufruf(anzeige, anzeigeClearAll)
		comAufruf(anzeige, anzeigeUpdate)
		comLos(anzeige)
	}
	comBoolSetzen(m.smtc, smtcPutIsEnabled, false)
}
