// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package main

// Tonausgabe: über welches Gerät der Player spielt.
//
// Seit es den eigenen Player gibt (player_windows.go), spielt Lumen.exe den
// Ton von Filmen selbst, und die Geräte nennt mpv. Was hier steht, gilt
// noch für alles, was die Web-App selbst abspielt — Trailer und Extras, und
// Filme, wenn libmpv fehlt.
//
// Warum: Den Ton einer WebView2-App spielt nicht Lumen.exe, sondern ein
// Hilfsprozess von WebView2 (msedgewebview2.exe), und Windows führt ihn als
// „Microsoft Edge WebView2" — nicht als Lumen. Programme, die den Ton nach
// Programm verteilen oder abgreifen (Logi MIXLINE, Game Bar, Discord, OBS),
// bekommen ihn deshalb oft nicht (WebView2Feedback #2236, offen). Chrome ist
// selbst das Programm, darum geht es dort. Logitech rät für solche Fälle,
// MIXLINE in der App selbst als Ausgabe zu wählen — genau das bietet die
// Web-App jetzt an (server/cmd/lumen/web/tonausgabe.js, HTMLMediaElement.
// setSinkId): Der Player öffnet dann ausdrücklich dieses Gerät.
//
// Was die App dafür tut:
//
//  1. Chromium nennt die Ausgabegeräte (und erlaubt setSinkId auf ein
//     bestimmtes Gerät) nur, wenn die Seite das Mikrofon benutzen darf.
//     Statt eine Mikrofon-Frage zu zeigen und kurz ein Mikrofon zu öffnen,
//     setzt die App diese Erlaubnis selbst — nur für den Ursprung des
//     Servers und nur, solange dort ein Gerät gewählt ist
//     (ICoreWebView2Profile4::SetPermissionState). Ein Mikrofon öffnet
//     dabei niemand. Fragt eine Seite trotzdem nach Mikrofon oder Kamera
//     (getUserMedia), lehnt die App ab, ohne das zu speichern
//     (berechtigungGefragt) — Lumen nimmt nie etwas auf.
//
//  2. Geräteliste und setSinkId gibt es nur in einem sicheren Kontext.
//     Lumen läuft im Heimnetz meist über http://<Adresse>:8097 — das ist
//     für Chromium nicht sicher. Die App startet WebView2 darum mit
//     --unsafely-treat-insecure-origin-as-secure für die gemerkten
//     http-Server (Liste der Flags von WebView2). Das gilt erst ab dem
//     nächsten Start; die Web-App sagt das.
//
// Nicht gewählt wurde: das Standardgerät nur für msedgewebview2.exe
// umstellen (IAudioPolicyConfig) — undokumentiert, und genau das, was beim
// Besitzer im Lautstärkemixer nicht half. Die Gerätenamen selbst über
// IMMDeviceEnumerator holen — die Kennungen, die setSinkId braucht, leitet
// Chromium mit einem geheimen Salz pro Profil ab; zuordnen liesse sich das
// nicht verlässlich.

import (
	"net"
	"net/url"
	"regexp"
	"strings"
)

// serverUrsprung gibt den Ursprung (Schema://Host:Anschluss) einer Seite
// zurück, wenn sie zu einem gemerkten Server gehört.
func (a *App) serverUrsprung(quelle string) (string, bool) {
	u, err := url.Parse(quelle)
	if err != nil || eingebaut(quelle) {
		return "", false
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	default:
		return "", false
	}
	if _, ok := a.liste.Gehoert(u); !ok {
		return "", false
	}
	return strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host), true
}

// Nur, was eine Adresse aus der Serverliste sein kann: Name oder IPv4,
// IPv6 in Klammern, Anschluss. Vor allem kein Komma — es trennt im Flag.
var ursprungMuster = regexp.MustCompile(`^http://[a-z0-9.\-]+(:[0-9]{1,5})?$|^http://\[[0-9a-f:.]+\](:[0-9]{1,5})?$`)

// UnsichereUrspruenge: die Ursprünge der gemerkten Server, die Chromium
// nicht von sich aus für sicher hält — http, aber nicht localhost oder
// 127.0.0.1 (die gelten schon als sicher). Für
// --unsafely-treat-insecure-origin-as-secure.
func UnsichereUrspruenge(server []Server, dazu ...string) []string {
	var aus []string
	gesehen := map[string]bool{}
	nimm := func(adresse string) {
		u, err := url.Parse(strings.TrimSpace(adresse))
		if err != nil || !strings.EqualFold(u.Scheme, "http") || u.Host == "" {
			return
		}
		host := strings.ToLower(u.Hostname())
		if host == "localhost" || strings.HasSuffix(host, ".localhost") {
			return
		}
		if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
			return
		}
		o := "http://" + strings.ToLower(u.Host)
		if !ursprungMuster.MatchString(o) || gesehen[o] {
			return
		}
		gesehen[o] = true
		aus = append(aus, o)
	}
	for _, s := range server {
		nimm(s.Adresse)
	}
	for _, d := range dazu {
		nimm(d)
	}
	return aus
}
