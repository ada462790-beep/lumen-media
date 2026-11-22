// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package main

// Die WebView2-Seite der Tonausgabe (warum und wie: tonausgabe.go).

import (
	"errors"
	"fmt"
	"log"
	"unsafe"

	"golang.org/x/sys/windows"
)

// TonausgabeErlauben setzt die Erlaubnis „Mikrofon" für einen Ursprung im
// Profil von WebView2: an = erlaubt, sonst zurück auf den Standard. Mehr
// als die Geräteliste und die Wahl des Lautsprechers bekommt die Seite
// damit nicht — Aufnahmen lehnt berechtigungGefragt ab.
func (f *fenster) TonausgabeErlauben(ursprung string, an bool, weiter func(error)) {
	if f.wv == nil {
		weiter(errors.New("WebView2 nicht bereit"))
		return
	}
	wv13, ok := comFrage(f.wv, &iidWebView2_13)
	if !ok {
		weiter(errors.New("WebView2 zu alt (ICoreWebView2_13)"))
		return
	}
	defer comLos(wv13)
	var profil unsafe.Pointer
	if hr := comAufruf(wv13, wv13GetProfile, uintptr(unsafe.Pointer(&profil))); hr != 0 || profil == nil {
		weiter(fmt.Errorf("get_Profile: %#x", hr))
		return
	}
	defer comLos(com(profil))
	p4, ok := comFrage(com(profil), &iidProfil4)
	if !ok {
		weiter(errors.New("WebView2 zu alt (ICoreWebView2Profile4)"))
		return
	}
	defer comLos(p4)

	zustand := uintptr(berechtigungStandard)
	if an {
		zustand = berechtigungErlaubt
	}
	text, err := windows.UTF16PtrFromString(ursprung)
	if err != nil {
		weiter(err)
		return
	}
	h := fertig(func(hr uintptr) {
		if hr != 0 {
			weiter(fmt.Errorf("SetPermissionState: %#x", hr))
			return
		}
		log.Printf("Tonausgabe für %s: %v", ursprung, map[bool]string{true: "erlaubt", false: "zurückgenommen"}[an])
		weiter(nil)
	})
	if hr := comAufruf(p4, profil4SetPermissionState, berechtigungMikrofon, uintptr(unsafe.Pointer(text)), zustand, uintptr(unsafe.Pointer(h))); hr != 0 {
		weiter(fmt.Errorf("SetPermissionState: %#x", hr))
	}
}

// berechtigungGefragt: Eine Seite will Mikrofon oder Kamera (getUserMedia).
// Lumen nimmt nie etwas auf — ablehnen, und zwar nur für diese eine
// Anfrage (SavesInProfile = false). Sonst überschriebe die Ablehnung die
// Erlaubnis, mit der die Tonausgabe die Geräte sieht. Alles andere bleibt,
// wie WebView2 es von sich aus hält.
//
// Läuft nach dem Empfänger, den edge selbst anmeldet (der setzt immer
// „Standard"); was hier gesetzt wird, gilt.
func (f *fenster) berechtigungGefragt(_, args com) {
	art := comZahl(args, berechtGetKind)
	if art != berechtigungMikrofon && art != berechtigungKamera {
		return
	}
	adresse, _ := comText(args, berechtGetUri)
	if a3, ok := comFrage(args, &iidBerechtArgs3); ok {
		comBoolSetzen(a3, berecht3PutSavesInProfile, false)
		comLos(a3)
	}
	comAufruf(args, berechtPutState, berechtigungAbgelehnt)
	log.Printf("Mikrofon/Kamera abgelehnt (%d): %s", art, adresse)
	if f.test != nil {
		f.test.aufnahmeAbgelehnt()
	}
}
