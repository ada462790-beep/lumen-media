// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

// Package scheibe macht aus einer DVD oder Blu-ray (als Ordner oder
// ISO-Abbild) eine MKV-Datei mit dem Hauptfilm — ohne neu zu rechnen.
//
// Der Ablauf:
//
//  1. Plan: Welcher Titel ist der Hauptfilm, welche Bytes welcher Dateien
//     gehören dazu, welche Spuren mit welcher Sprache, wo die Kapitel
//     beginnen. Bei der DVD aus den IFO-Dateien (VIDEO_TS.IFO nennt die
//     Titel, VTS_nn_0.IFO die Zellen, Spuren und Kapitel), bei der
//     Blu-ray aus den Playlists (BDMV/PLAYLIST/*.mpls).
//  2. Lesen: Die Bytes des Hauptfilms gehen in genau der Reihenfolge der
//     Zellen an FFmpeg (über die Standardeingabe). So bleiben das Menü
//     (VTS_nn_0.VOB, VIDEO_TS.VOB) und andere Titel desselben Titelsatzes
//     draussen, und der Fortschritt ist genau bekannt.
//  3. FFmpeg kopiert Bild, Ton und Untertitel in eine MKV (-c copy), mit
//     Sprachen und Kapiteln aus Schritt 1.
//
// Ein ISO-Abbild packt vorher 7-Zip aus (iso.go): erst die kleinen
// Steuerdateien, dann nur die Dateien des Hauptfilms.
package scheibe

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
)

// Art der Scheibe.
const (
	DVD    = "dvd"
	BluRay = "bluray"
)

// Abschnitt ist ein zusammenhängender Bereich einer Datei.
type Abschnitt struct {
	Datei string // Name relativ zum Ordner des Plans
	Von   int64  // erstes Byte
	Bis   int64  // erstes Byte danach
}

// Spur ist eine Spur des Hauptfilms, wie FFmpeg sie kennt: die Kennung
// im Datenstrom (DVD: 0x1e0, 0x80, 0x20 …; Blu-ray: die PID).
type Spur struct {
	Art     string // "v", "a", "s"
	ID      int
	Sprache string // ISO 639-2/B („ger"), leer wenn unbekannt
}

// Plan sagt, wie aus der Scheibe die MKV wird.
type Plan struct {
	Art        string
	Ordner     string // VIDEO_TS oder BDMV/STREAM
	Abschnitte []Abschnitt
	DauerMS    int64
	Kapitel    []int64 // Beginn je Kapitel in ms, aufsteigend, das erste 0
	Spuren     []Spur  // leer: alle Spuren nehmen, ohne Sprache
	// Ausweg: Die Steuerdateien liessen sich nicht lesen, genommen ist
	// der grösste Titelsatz bzw. die grösste Datei (wie der Scan).
	Ausweg bool
}

// Gesamt: so viele Bytes gehen an FFmpeg.
func (p *Plan) Gesamt() int64 {
	var n int64
	for _, a := range p.Abschnitte {
		n += a.Bis - a.Von
	}
	return n
}

// Dateien: die Dateien, die der Plan liest (für das Auspacken aus der ISO).
func (p *Plan) Dateien() []string {
	gesehen := map[string]bool{}
	var out []string
	for _, a := range p.Abschnitte {
		if !gesehen[a.Datei] {
			gesehen[a.Datei] = true
			out = append(out, a.Datei)
		}
	}
	return out
}

// Datei ist ein Eintrag eines Ordners der Scheibe mit Grösse — aus
// os.ReadDir oder aus der Liste von 7-Zip.
type Datei struct {
	Name    string
	Groesse int64
}

// OrdnerLesen liest die Dateien eines Ordners (ohne Unterordner).
func OrdnerLesen(dir string) ([]Datei, error) {
	eintraege, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []Datei
	for _, e := range eintraege {
		if !e.Type().IsRegular() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, Datei{e.Name(), info.Size()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// finden sucht einen Namen ohne Rücksicht auf Gross- und Kleinschreibung
// (manche Brennprogramme schreiben „video_ts.ifo").
func finden(dateien []Datei, name string) (Datei, bool) {
	for _, d := range dateien {
		if strings.EqualFold(d.Name, name) {
			return d, true
		}
	}
	return Datei{}, false
}

// Unterordner sucht einen Unterordner ohne Rücksicht auf die Schreibweise.
func Unterordner(dir, name string) (string, bool) {
	eintraege, err := os.ReadDir(dir)
	if err != nil {
		return "", false
	}
	for _, e := range eintraege {
		if strings.EqualFold(e.Name(), name) {
			p := filepath.Join(dir, e.Name())
			if info, err := os.Stat(p); err == nil && info.IsDir() {
				return p, true
			}
		}
	}
	return "", false
}

// ErrKeinFilm: In der Scheibe steckt nichts, was sich als Film lesen lässt.
var ErrKeinFilm = errors.New("kein hauptfilm gefunden")

// ---------------------------------------------------------------- Lesen

// Leser liefert die Bytes eines Plans hintereinander. Gelesen zählt mit
// (für den Fortschritt); ein beendeter Kontext bricht das Lesen ab.
type Leser struct {
	ctx     context.Context
	ordner  string
	teile   []Abschnitt
	i       int
	f       *os.File
	offen   string
	rest    int64
	Gelesen atomic.Int64
}

// NeuerLeser liest die Abschnitte des Plans aus dem Ordner.
func NeuerLeser(ctx context.Context, p *Plan) *Leser {
	return &Leser{ctx: ctx, ordner: p.Ordner, teile: p.Abschnitte}
}

func (l *Leser) Read(b []byte) (int, error) {
	if err := l.ctx.Err(); err != nil {
		return 0, err
	}
	for l.rest == 0 {
		if l.i >= len(l.teile) {
			return 0, io.EOF
		}
		a := l.teile[l.i]
		l.i++
		if l.f == nil || l.offen != a.Datei {
			if l.f != nil {
				l.f.Close()
			}
			f, err := os.Open(filepath.Join(l.ordner, a.Datei))
			if err != nil {
				l.f = nil
				return 0, err
			}
			l.f, l.offen = f, a.Datei
		}
		if _, err := l.f.Seek(a.Von, io.SeekStart); err != nil {
			return 0, err
		}
		l.rest = a.Bis - a.Von
	}
	if int64(len(b)) > l.rest {
		b = b[:l.rest]
	}
	n, err := l.f.Read(b)
	l.rest -= int64(n)
	l.Gelesen.Add(int64(n))
	if err == io.EOF {
		if l.rest > 0 {
			return n, fmt.Errorf("datei kürzer als erwartet: %w", io.ErrUnexpectedEOF)
		}
		err = nil
	}
	return n, err
}

// Close schliesst die offene Datei.
func (l *Leser) Close() error {
	if l.f != nil {
		return l.f.Close()
	}
	return nil
}

// ---------------------------------------------------------------- Sprachen

// sprache3 macht aus dem Kürzel der DVD (ISO 639-1, „de") das, was
// Matroska verlangt (ISO 639-2/B, „ger"). Unbekanntes bleibt leer —
// lieber keine Sprache als eine falsche.
func sprache3(zwei string) string {
	return sprachen[strings.ToLower(strings.TrimSpace(zwei))]
}

var sprachen = map[string]string{
	"de": "ger", "en": "eng", "fr": "fre", "it": "ita", "es": "spa", "pt": "por",
	"nl": "dut", "da": "dan", "sv": "swe", "no": "nor", "nb": "nor", "fi": "fin",
	"is": "ice", "pl": "pol", "cs": "cze", "sk": "slo", "hu": "hun", "ro": "rum",
	"bg": "bul", "hr": "hrv", "sr": "srp", "sl": "slv", "ru": "rus", "uk": "ukr",
	"el": "gre", "tr": "tur", "he": "heb", "iw": "heb", "ar": "ara", "fa": "per",
	"hi": "hin", "th": "tha", "vi": "vie", "id": "ind", "ms": "may", "zh": "chi",
	"ja": "jpn", "ko": "kor", "ca": "cat", "eu": "baq", "gl": "glg", "et": "est",
	"lv": "lav", "lt": "lit", "ga": "gle", "cy": "wel", "la": "lat", "rm": "roh",
}

// sprachePruefen nimmt ein Kürzel aus einer Blu-ray-Playlist (schon ISO
// 639-2, „deu" oder „ger") und gibt es Matroska-tauglich zurück.
func sprachePruefen(drei string) string {
	drei = strings.ToLower(strings.TrimSpace(drei))
	if len(drei) != 3 {
		return ""
	}
	for _, c := range drei {
		if c < 'a' || c > 'z' {
			return ""
		}
	}
	// Die „terminologischen" Kürzel auf die bibliografischen, die
	// Matroska nennt.
	switch drei {
	case "deu":
		return "ger"
	case "fra":
		return "fre"
	case "nld":
		return "dut"
	case "ces":
		return "cze"
	case "zho":
		return "chi"
	case "ell":
		return "gre"
	case "slk":
		return "slo"
	case "ron":
		return "rum"
	case "isl":
		return "ice"
	case "fas":
		return "per"
	case "und", "qaa", "mis", "zxx":
		return ""
	}
	return drei
}
