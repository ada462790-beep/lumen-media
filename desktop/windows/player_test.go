// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"net/url"
	"strings"
	"testing"
)

func TestPlayerAdresse(t *testing.T) {
	gut := map[string]string{
		"/api/v1/stream/f1?codecs=h264%2Chevc&hdr=1": "http://192.168.1.5:8097/api/v1/stream/f1?codecs=h264%2Chevc&hdr=1",
		"/api/v1/stream/f_2/hls/master.m3u8?spur=1":  "http://192.168.1.5:8097/api/v1/stream/f_2/hls/master.m3u8?spur=1",
		"/api/v1/stream/f1/subtitles/12.vtt":         "http://192.168.1.5:8097/api/v1/stream/f1/subtitles/12.vtt",
		"/api/v1/stream/f1":                          "http://192.168.1.5:8097/api/v1/stream/f1",
	}
	for pfad, will := range gut {
		a := PlayerAuftrag{Pfad: pfad}
		got, err := a.Adresse("http://192.168.1.5:8097")
		if err != nil || got != will {
			t.Errorf("%s: %q %v", pfad, got, err)
		}
	}
	for _, pfad := range []string{
		"", "/", "http://anders/api/v1/stream/f1", "//anders/api/v1/stream/f1", "/api/v1/items/x",
		"/api/v1/stream/../me", "/api/v1/stream/f1/..%2F..%2Fme", "/api/v1/stream/f1?key=geheim",
		"/api/v1/stream/f1?token=x", "/api/v1/stream/f1\r\nX: y", "/api/v1/stream/f1#x", "/api/v1/stream/%2e%2e",
		"/api/v1/stream//f1", "/api/v1/stream/f1@anders",
	} {
		a := PlayerAuftrag{Pfad: pfad}
		if got, err := a.Adresse("http://192.168.1.5:8097"); err == nil {
			t.Errorf("%q ging durch: %s", pfad, got)
		}
	}
	// Ein Ursprung mit Pfad oder fremdem Schema ist keiner.
	for _, u := range []string{"file://x", "http://x/y", "javascript:alert(1)", ""} {
		a := PlayerAuftrag{Pfad: "/api/v1/stream/f1"}
		if _, err := a.Adresse(u); err == nil {
			t.Errorf("Ursprung %q ging durch", u)
		}
	}
}

func TestPlayerKopf(t *testing.T) {
	a := PlayerAuftrag{Token: "abcDEF0123456789_-.~+/="}
	k, err := a.Kopf()
	if err != nil || k != "Authorization: Bearer abcDEF0123456789_-.~+/=" {
		t.Errorf("%q %v", k, err)
	}
	for _, tok := range []string{"", "kurz", "a,b,c,d,e,f,g", "abcdefgh\r\nX-Evil: 1", "abcdefgh ijk", "abcdefgh:ijk"} {
		a := PlayerAuftrag{Token: tok}
		if _, err := a.Kopf(); err == nil {
			t.Errorf("Token %q ging durch", tok)
		}
	}
}

func TestPlayerBereinigen(t *testing.T) {
	a := PlayerAuftrag{Start: -3, Lautstaerke: 7, Tempo: 0, Tonausgabe: "wasapi/x,y"}
	a.Bereinigen()
	if a.Start != 0 || a.Lautstaerke != 1 || a.Tempo != 1 || a.Tonausgabe != "" {
		t.Errorf("%+v", a)
	}
	b := PlayerAuftrag{Tonausgabe: "wasapi/{0.0.0.00000000}.{8a2c3d4e-1111-2222-3333-444455556666}", Tempo: 1.5, Lautstaerke: .4}
	b.Bereinigen()
	if b.Tonausgabe == "" || b.Tempo != 1.5 || b.Lautstaerke != .4 {
		t.Errorf("%+v", b)
	}
}

func TestPlayerSpuren(t *testing.T) {
	// So sieht track-list von mpv aus (gekürzt): Kennungen je Typ ab 1,
	// ff-index ist der Stream-Index der Datei — wie in den Spuren von Lumen.
	text := `[{"id":1,"type":"video","ff-index":0,"selected":true},
	{"id":1,"type":"audio","ff-index":1,"lang":"ger","selected":false},
	{"id":2,"type":"audio","ff-index":2,"lang":"eng","selected":true},
	{"id":1,"type":"sub","ff-index":3,"lang":"ger","codec":"ass"},
	{"id":2,"type":"sub","ff-index":4,"lang":"eng","codec":"hdmv_pgs_subtitle"},
	{"id":3,"type":"sub","external":true,"external-filename":"http://s/api/v1/stream/f/subtitles/9.vtt","selected":true}]`
	s := spurenLesen(text)
	if len(s) != 6 {
		t.Fatalf("%d Spuren", len(s))
	}
	if id, ok := mpvKennung(s, "audio", 2); !ok || id != 2 {
		t.Errorf("Ton 2 → %d %v", id, ok)
	}
	if id, ok := mpvKennung(s, "sub", 4); !ok || id != 2 {
		t.Errorf("Untertitel 4 → %d %v", id, ok)
	}
	if _, ok := mpvKennung(s, "audio", 3); ok {
		t.Error("Index 3 ist kein Ton")
	}
	if id, ok := externKennung(s, "http://s/api/v1/stream/f/subtitles/9.vtt"); !ok || id != 3 {
		t.Errorf("extern → %d %v", id, ok)
	}
	if g := gewaehlteSpur(s, "audio"); g != 2 {
		t.Errorf("gewählter Ton %d", g)
	}
	if g := gewaehlteSpur(s, "sub"); g != -2 {
		t.Errorf("gewählter Untertitel %d", g)
	}
	if spurenLesen("kaputt") != nil {
		t.Error("kaputtes JSON")
	}
}

func TestMpvFaehigkeiten(t *testing.T) {
	q, err := url.ParseQuery(mpvFaehigkeiten())
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []struct{ feld, wert string }{{"codecs", "hevc"}, {"audio", "truehd"}, {"audio", "dts"}, {"audio", "eac3"}, {"containers", "mkv"}, {"containers", "hls"}} {
		if !strings.Contains(","+q.Get(f.feld)+",", ","+f.wert+",") {
			t.Errorf("%s ohne %s", f.feld, f.wert)
		}
	}
	if q.Get("hdr") != "1" {
		t.Error("ohne HDR")
	}
	// Kommt als Text in die Brücke: kein Hochkomma.
	if strings.Contains(mpvFaehigkeiten(), "'") {
		t.Error("Hochkomma")
	}
}

func TestBrueckePlayer(t *testing.T) {
	mit, ohne := brueckeSkript("mpv"), brueckeSkript("")
	if !strings.Contains(mit, "const PLAYER = 'mpv'") || !strings.Contains(ohne, "const PLAYER = ''") {
		t.Error("Player-Fähigkeit fehlt in der Brücke")
	}
	if strings.Contains(mit, "__LUMEN_") {
		t.Error("Platzhalter übrig")
	}
}

func TestPlayerNachricht(t *testing.T) {
	d := t.TempDir()
	h := &attrappe{}
	app, _ := NeueApp(h, d, d)
	app.liste.Merken(Server{Adresse: "http://192.168.1.5:8097", Name: "NAS"})
	quelle := "http://192.168.1.5:8097/#/titel/x"
	app.Nachricht(quelle, `{"id":1,"art":"playerAbspielen","daten":{"pfad":"/api/v1/stream/f1","token":"abcdefgh12","fileId":"f1"}}`)
	if an := h.letzteAntwort(t); !an.OK || h.spielt != "http://192.168.1.5:8097 f1" {
		t.Errorf("abspielen: %+v %q", an, h.spielt)
	}
	// Eine fremde Seite (oder die eingebaute) darf den Player nicht starten.
	for _, q := range []string{"http://fremd.example/", seitenAdresse} {
		app.Nachricht(q, `{"id":2,"art":"playerAbspielen","daten":{"pfad":"/api/v1/stream/f1","token":"abcdefgh12"}}`)
		if an := h.letzteAntwort(t); an.OK || an.Fehler != fehlerNichtErlaub {
			t.Errorf("%s: %+v", q, an)
		}
	}
	app.Nachricht(quelle, `{"id":3,"art":"playerBefehl","daten":{"art":"pause","wert":true}}`)
	if an := h.letzteAntwort(t); !an.OK || len(h.befehle) != 1 || h.befehle[0] != "pause" {
		t.Errorf("befehl: %+v %v", an, h.befehle)
	}
}

func TestPlayerBildAdresse(t *testing.T) {
	gut := map[string]string{
		"/api/v1/art/it1/poster?w=500":        "http://nas:8097/api/v1/art/it1/poster?w=500",
		"/api/v1/art/col_x/poster":            "http://nas:8097/api/v1/art/col_x/poster",
		"/api/v1/art/it1/poster?v=a1b2&w=500": "http://nas:8097/api/v1/art/it1/poster?v=a1b2&w=500",
		"/api/v1/art/it-1/backdrop?w=780":     "http://nas:8097/api/v1/art/it-1/backdrop?w=780",
	}
	for pfad, will := range gut {
		a := PlayerAuftrag{Bild: pfad}
		if got := a.BildAdresse("http://nas:8097"); got != will {
			t.Errorf("%s: %q", pfad, got)
		}
	}
	for _, pfad := range []string{
		"", "/api/v1/stream/f1", "https://image.tmdb.org/t/p/w500/x.jpg", "//anders/api/v1/art/it1/poster",
		"/api/v1/art/it1/poster?key=geheim", "/api/v1/art/it1/poster?w=500&token=x", "/api/v1/art/../me/poster",
		"/api/v1/art/it1/poster/x", "/api/v1/art/it1/POSTER", "/api/v1/art/it1/poster?w=5%0a", "/api/v1/art/it1/poster#x",
		"/api/v1/art/it1/poster?w=1&w=2", "/api/v1/art/it1@anders/poster",
	} {
		a := PlayerAuftrag{Bild: pfad}
		if got := a.BildAdresse("http://nas:8097"); got != "" {
			t.Errorf("%q ging durch: %s", pfad, got)
		}
	}
	if got := (&PlayerAuftrag{Bild: "/api/v1/art/it1/poster"}).BildAdresse("http://nas:8097/pfad"); got != "" {
		t.Errorf("Ursprung mit Pfad: %s", got)
	}
}

func TestMpvBildOptionen(t *testing.T) {
	alsKarte := func(o [][2]string) map[string]string {
		m := map[string]string{}
		for _, x := range o {
			m[x[0]] = x[1]
		}
		return m
	}
	m := alsKarte(mpvBildOptionen(true, false))
	if m["hwdec"] != "auto-safe" || m["vo"] != "gpu-next,gpu," || m["gpu-api"] != "d3d11" ||
		m["target-colorspace-hint"] != "no" || m["tone-mapping"] != "bt.2446a" {
		t.Errorf("SDR mit Hardware: %v", m)
	}
	m = alsKarte(mpvBildOptionen(false, true))
	if m["hwdec"] != "no" || m["target-colorspace-hint"] != "yes" || m["tone-mapping"] != "" {
		t.Errorf("HDR ohne Hardware: %v", m)
	}
	a := PlayerAuftrag{Hardware: "irgendwas"}
	a.Bereinigen()
	if a.Hardware != "auto" || !a.HardwareAn() {
		t.Errorf("Hardware: %q", a.Hardware)
	}
	a = PlayerAuftrag{Hardware: "aus"}
	a.Bereinigen()
	if a.HardwareAn() {
		t.Error("aus bleibt nicht aus")
	}
}
