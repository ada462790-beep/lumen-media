// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package main

// Der eigene Player (mpv) — der Teil, der ohne Windows auskommt und sich
// darum überall testen lässt: was die Web-App übergibt, wie Lumen es
// prüft, wie die Spuren der Datei auf die von mpv passen.
// Das Fenster und mpv selbst: player_windows.go.

import (
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// PlayerAuftrag übergibt die Web-App mit lumenDesktop.player.abspielen,
// wenn in der Windows-App ein Film startet (server/cmd/lumen/web/
// desktopplayer.js). Was gespielt wird und wie, hat die Web-App nach
// ihren Regeln entschieden (Fassung, Startstelle, Ton- und
// Untertitelsprache je Serie und Profil, Plan des Servers); die App
// spielt es nur ab.
type PlayerAuftrag struct {
	// Pfad beim Server, etwa /api/v1/stream/<datei>?codecs=… — die
	// Originaldatei (direkt, mit Bereichsanfragen) oder, wenn der Server
	// unterwegs eine Bandbreitengrenze setzt, die HLS-Liste. Der Ursprung
	// kommt nicht von der Seite, sondern ist der des gemerkten Servers,
	// von dem die Nachricht kam.
	Pfad string `json:"pfad"`
	// Token des Profils (wie im Kopf jeder Anfrage der Web-App). Geht nur
	// als Kopf „Authorization" an den Server, nie in eine Adresse oder ins
	// Protokoll.
	Token string `json:"token"`
	HLS   bool   `json:"hls"`

	Start    float64 `json:"start"`
	Pausiert bool    `json:"pausiert"`
	// Ton und Untertitel: Index der Spur in der Datei (wie in den Spuren
	// des Servers, also der Stream-Index von FFmpeg), -1 = Standard bzw.
	// aus. Eine Untertiteldatei neben dem Film (extern) holt mpv über
	// UntertitelPfad.
	Ton            int    `json:"ton"`
	Untertitel     int    `json:"untertitel"`
	UntertitelPfad string `json:"untertitelPfad,omitempty"`

	Lautstaerke float64 `json:"lautstaerke"` // 0 … 1
	Stumm       bool    `json:"stumm"`
	Tempo       float64 `json:"tempo"`
	Tonausgabe  string  `json:"tonausgabe,omitempty"` // Gerät aus audio-device-list, leer = Standard

	// Für Protokoll, Medientasten und Mixer — die Bedienelemente zeichnet
	// die Web-App selbst (Vorspann, Abspann, nächste Folge, Kapitel).
	ItemID    string         `json:"itemId"`
	FileID    string         `json:"fileId"`
	Titel     string         `json:"titel"`
	Unter     string         `json:"unter,omitempty"`
	Kapitel   []PlayerMarke  `json:"kapitel,omitempty"`
	Intro     *PlayerBereich `json:"intro,omitempty"`
	AbspannMs int64          `json:"abspannMs,omitempty"`

	// Für die Mediensteuerung von Windows (mediensteuerung_windows.go):
	// Pfad des Posters beim Server (/api/v1/art/<id>/poster?w=500, ohne
	// Schlüssel — die App holt es mit dem Token im Kopf) und ob es eine
	// nächste Folge gibt (fehlt die Angabe, bleibt „Weiter" an; die
	// Web-App entscheidet ohnehin selbst).
	Bild     string `json:"bild,omitempty"`
	Naechste *bool  `json:"naechste,omitempty"`

	// Hardware-Beschleunigung: "aus" schaltet die Dekodierung über die
	// Grafikkarte ab (Einstellung der Web-App, gilt für dieses Gerät);
	// sonst, leer oder "auto": wo sie sicher ist.
	Hardware string `json:"hardware,omitempty"`
}

type PlayerMarke struct {
	StartMs int64  `json:"startMs"`
	Titel   string `json:"titel,omitempty"`
}

type PlayerBereich struct {
	VonMs int64 `json:"vonMs"`
	BisMs int64 `json:"bisMs"`
}

var (
	fehlerPfad  = errors.New("pfad")
	fehlerToken = errors.New("token")
)

// Ein Token, wie Lumen es ausgibt (Base64/Hex) — vor allem ohne
// Zeilenumbruch, Doppelpunkt oder Komma: Es landet in einem Kopf, und mpv
// trennt die Liste http-header-fields an Kommas.
var tokenMuster = regexp.MustCompile(`^[A-Za-z0-9._~+/=\-]{8,512}$`)

// Pfade, die mpv holen darf: der Strom einer Datei und was dazugehört
// (HLS-Liste, Untertitel) — Kennungen, wie Lumen sie vergibt, keine
// kodierten Zeichen.
var pfadMuster = regexp.MustCompile(`^/api/v1/stream/[A-Za-z0-9_-]{1,64}(/[A-Za-z0-9_-][A-Za-z0-9_.-]{0,63}){0,3}$`)

// Adresse prüft den Auftrag gegen den Ursprung des Servers, von dem er
// kam, und gibt die Adresse für mpv zurück. Nur Pfade unter /api/v1/stream/
// — so kann keine Seite mpv mit dem Token irgendwohin schicken.
func (a *PlayerAuftrag) Adresse(ursprung string) (string, error) {
	return streamAdresse(ursprung, a.Pfad)
}

// UntertitelAdresse: die externe Untertiteldatei, gleiche Regeln.
func (a *PlayerAuftrag) UntertitelAdresse(ursprung string) (string, error) {
	if a.UntertitelPfad == "" {
		return "", nil
	}
	return streamAdresse(ursprung, a.UntertitelPfad)
}

func streamAdresse(ursprung, pfad string) (string, error) {
	o, err := url.Parse(ursprung)
	if err != nil || (o.Scheme != "http" && o.Scheme != "https") || o.Host == "" || o.Path != "" {
		return "", fehlerPfad
	}
	weg, _, _ := strings.Cut(pfad, "?")
	if !pfadMuster.MatchString(weg) || strings.Contains(weg, "..") || strings.ContainsAny(pfad, "\\\r\n\t #") {
		return "", fehlerPfad
	}
	u, err := url.Parse(o.Scheme + "://" + o.Host + pfad)
	if err != nil || u.Host != o.Host || u.Path != weg {
		return "", fehlerPfad
	}
	// Ein Medienschlüssel hat in der Adresse nichts zu suchen: Lumen
	// schickt das Token im Kopf.
	q := u.Query()
	if q.Has("key") || q.Has("token") {
		return "", fehlerPfad
	}
	return u.String(), nil
}

// Bilder, die die App für die Mediensteuerung holen darf: Bilder eines
// Titels beim Server, höchstens mit Breite und Stand in der Abfrage.
var bildMuster = regexp.MustCompile(`^/api/v1/art/[A-Za-z0-9_-]{1,64}/[a-z0-9]{1,16}$`)

// BildAdresse: das Poster für die Mediensteuerung, gleiche Regeln wie
// beim Strom (nur dieser Server, kein Schlüssel in der Adresse). Leer,
// wenn es keins gibt oder der Pfad nicht passt.
func (a *PlayerAuftrag) BildAdresse(ursprung string) string {
	if a.Bild == "" || len(a.Bild) > 300 {
		return ""
	}
	o, err := url.Parse(ursprung)
	if err != nil || (o.Scheme != "http" && o.Scheme != "https") || o.Host == "" || o.Path != "" {
		return ""
	}
	weg, abfrage, _ := strings.Cut(a.Bild, "?")
	if !bildMuster.MatchString(weg) || strings.ContainsAny(a.Bild, "\\\r\n\t #%") {
		return ""
	}
	q, err := url.ParseQuery(abfrage)
	if err != nil {
		return ""
	}
	for k, v := range q {
		if len(v) != 1 || (k != "w" && k != "v") || !standMuster.MatchString(v[0]) {
			return ""
		}
	}
	u, err := url.Parse(o.Scheme + "://" + o.Host + a.Bild)
	if err != nil || u.Host != o.Host || u.Path != weg {
		return ""
	}
	return u.String()
}

var standMuster = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// HardwareAn: ob mpv über die Grafikkarte dekodieren soll.
func (a *PlayerAuftrag) HardwareAn() bool { return a.Hardware != "aus" }

// mpvBildOptionen: wie mpv das Bild ausgibt. gpu-next über Direct3D 11
// (gpu als Rückfall); Dekodieren über die Grafikkarte (d3d11va, nvdec …),
// wo mpv es für sicher hält — schlägt es fehl, dekodiert mpv von selbst
// in Software weiter. Ist HDR in Windows an, reicht mpv ein HDR-Bild
// durch (target-colorspace-hint); sonst bildet es HDR selbst auf SDR ab
// (BT.2446a, gedacht genau dafür).
func mpvBildOptionen(hardware, hdr bool) [][2]string {
	o := [][2]string{
		{"vo", "gpu-next,gpu,"},
		{"gpu-api", "d3d11"},
		{"hwdec", map[bool]string{true: "auto-safe", false: "no"}[hardware]},
	}
	if hdr {
		o = append(o, [2]string{"target-colorspace-hint", "yes"})
	} else {
		o = append(o, [2]string{"target-colorspace-hint", "no"}, [2]string{"tone-mapping", "bt.2446a"})
	}
	return o
}

// Kopf ist der Wert für mpv --http-header-fields.
func (a *PlayerAuftrag) Kopf() (string, error) {
	if !tokenMuster.MatchString(a.Token) {
		return "", fehlerToken
	}
	return "Authorization: Bearer " + a.Token, nil
}

// Bereinigen setzt Grenzen, damit nichts Unsinniges an mpv geht.
func (a *PlayerAuftrag) Bereinigen() {
	if a.Start < 0 || a.Start != a.Start {
		a.Start = 0
	}
	if a.Lautstaerke < 0 || a.Lautstaerke > 1 || a.Lautstaerke != a.Lautstaerke {
		a.Lautstaerke = 1
	}
	if a.Tempo < 0.25 || a.Tempo > 4 || a.Tempo != a.Tempo {
		a.Tempo = 1
	}
	if len(a.Titel) > 300 {
		a.Titel = a.Titel[:300]
	}
	if !geraetMuster.MatchString(a.Tonausgabe) {
		a.Tonausgabe = ""
	}
	if len(a.Unter) > 300 {
		a.Unter = a.Unter[:300]
	}
	if a.Hardware != "aus" {
		a.Hardware = "auto"
	}
}

// Gerätenamen von mpv: „auto", „wasapi/{0.0.0.00000000}.{guid}" — nie
// mit Komma oder Zeilenumbruch.
var geraetMuster = regexp.MustCompile(`^$|^[A-Za-z0-9_./{}\-]{1,200}$`)

// ---------------------------------------------------------------- Spuren

// mpvSpur ist ein Eintrag aus mpv track-list (als JSON gelesen).
type mpvSpur struct {
	ID          int    `json:"id"`
	Typ         string `json:"type"` // video | audio | sub
	FFIndex     *int   `json:"ff-index"`
	Extern      bool   `json:"external"`
	ExternDatei string `json:"external-filename"`
	Gewaehlt    bool   `json:"selected"`
	Sprache     string `json:"lang"`
	Titel       string `json:"title"`
	Codec       string `json:"codec"`
}

// spurenLesen liest track-list.
func spurenLesen(text string) []mpvSpur {
	var s []mpvSpur
	if err := json.Unmarshal([]byte(text), &s); err != nil {
		return nil
	}
	return s
}

// mpvKennung sucht die mpv-Spur zu einem Stream-Index der Datei. Externe
// Spuren (von mpv dazugeladen) haben keinen Index der Datei.
func mpvKennung(spuren []mpvSpur, typ string, index int) (int, bool) {
	for _, s := range spuren {
		if s.Typ == typ && !s.Extern && s.FFIndex != nil && *s.FFIndex == index {
			return s.ID, true
		}
	}
	return 0, false
}

// externKennung: die von mpv dazugeladene Untertiteldatei mit dieser Adresse.
func externKennung(spuren []mpvSpur, adresse string) (int, bool) {
	for _, s := range spuren {
		if s.Typ == "sub" && s.Extern && s.ExternDatei == adresse {
			return s.ID, true
		}
	}
	return 0, false
}

// gewaehlteSpur: Stream-Index der gewählten Spur eines Typs (-1: keine).
func gewaehlteSpur(spuren []mpvSpur, typ string) int {
	for _, s := range spuren {
		if s.Typ == typ && s.Gewaehlt {
			if s.FFIndex != nil && !s.Extern {
				return *s.FFIndex
			}
			return -2 // extern
		}
	}
	return -1
}

// ---------------------------------------------------------------- Geräte

// tonGeraet ist ein Eintrag aus mpv audio-device-list.
type tonGeraet struct {
	Name         string `json:"name"`
	Beschreibung string `json:"description"`
}

func geraeteLesen(text string) []tonGeraet {
	var g []tonGeraet
	if err := json.Unmarshal([]byte(text), &g); err != nil {
		return nil
	}
	return g
}

// ---------------------------------------------------------------- Fähigkeiten

// mpvFaehigkeiten sagt dem Server, was mpv abspielt: im Grunde alles, was
// FFmpeg kennt. Dann ist der Plan „direkt", ausser der Server setzt unterwegs
// eine Bandbreitengrenze — dann kommt HLS, das mpv ebenso spielt. „hls" bei
// den Behältern heisst für den Server: Jeder Behälter passt.
func mpvFaehigkeiten() string {
	q := url.Values{}
	q.Set("codecs", strings.Join([]string{"h264", "hevc", "av1", "vp9", "vp8", "mpeg2video", "mpeg1video", "mpeg4",
		"msmpeg4v3", "msmpeg4v2", "vc1", "wmv3", "wmv2", "wmv1", "h263", "theora", "mjpeg", "prores", "dnxhd", "ffv1", "vvc"}, ","))
	q.Set("containers", strings.Join([]string{"mkv", "mp4", "webm", "avi", "ts", "wmv", "mpeg", "mov", "flv", "ogg", "m2ts", "hls"}, ","))
	q.Set("audio", strings.Join([]string{"aac", "ac3", "eac3", "dts", "truehd", "flac", "opus", "mp3", "mp2", "vorbis",
		"pcm", "alac", "wmav2", "wmapro", "wmav1", "amr_nb", "ape", "wavpack"}, ","))
	q.Set("maxHeight", strconv.Itoa(4320))
	q.Set("hdr", "1")
	return q.Encode()
}

// ---------------------------------------------------------------- Befehle

// PlayerBefehl schickt die Web-App während der Wiedergabe
// (lumenDesktop.player.befehl).
type PlayerBefehl struct {
	Art   string          `json:"art"`
	Wert  json.RawMessage `json:"wert"`
	Pfad  string          `json:"pfad,omitempty"`
	Index int             `json:"index"`
}

func (b PlayerBefehl) zahl() (float64, bool) {
	var f float64
	if err := json.Unmarshal(b.Wert, &f); err != nil || f != f {
		return 0, false
	}
	return f, true
}

func (b PlayerBefehl) schalter() bool {
	var v bool
	_ = json.Unmarshal(b.Wert, &v)
	return v
}

func (b PlayerBefehl) text() string {
	var s string
	_ = json.Unmarshal(b.Wert, &s)
	return s
}
