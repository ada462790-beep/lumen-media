// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package main

// Die Web-App steckt in der Binärdatei.
//
// Das ist der Unterschied zwischen „ein Projekt" und „etwas, das man
// weitergeben kann": wer Lumen bekommt, bekommt eine Datei. Keine
// Verzeichnisse, die beim Kopieren verlorengehen, keine Pfade, die auf
// einem anderen Rechner nicht stimmen, keine Frage, welche Fassung der
// Oberfläche zu welcher Fassung des Servers gehört.
//
// Ausgeliefert wird sie so, dass ein Handy unterwegs möglichst wenig
// laden muss:
//
//   - Alle Adressen tragen eine Fassung (?v=<prüfsumme>), gerechnet aus
//     dem Inhalt aller Dateien — nicht aus der Startzeit. Mit Fassung darf
//     der Browser eine Datei ein Jahr lang behalten, ohne nachzufragen.
//     Ändert sich irgendeine Datei (ein Update), ändern sich alle Adressen
//     zugleich: Altes und Neues mischt sich nie, auch nicht bei Modulen,
//     die sich gegenseitig laden (import './x.js' bekommt die Fassung
//     beim Ausliefern angehängt).
//   - Nur index.html wird jedes Mal nachgefragt (no-cache, mit ETag) —
//     sie nennt die Fassung.
//   - Text (JS, CSS, JSON, HTML, SVG, Manifest) kommt einmal beim Start
//     gepackt (gzip) in den Speicher, wenn der Browser es versteht.
//     Rund 70 % weniger.
//   - index.html kündigt alle Module gleich an (modulepreload): Der
//     Browser holt sie parallel statt eins nach dem anderen.

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

//go:embed all:web
var webFS embed.FS

// Platzhalter in den Dateien der Oberfläche, die beim Ausliefern ersetzt
// werden: die Fassung und (im Service Worker) die Liste der Dateien.
const (
	platzhalterFassung = "__LUMEN_FASSUNG__"
	platzhalterDateien = `"__LUMEN_DATEIEN__"`
	// devFassung: mit --web <ordner> ändern sich die Dateien laufend —
	// dann gibt es keine lange Aufbewahrung und keinen Service Worker.
	devFassung = "dev"
	einJahr    = "public, max-age=31536000, immutable"
)

// webDatei ist eine Datei, wie sie ausgeliefert wird.
type webDatei struct {
	roh    []byte
	gz     []byte // nil: Packen lohnt sich nicht
	etag string
	typ  string
}

// Gepackt wird nur Text; Bilder sind es schon.
func packbar(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".js", ".mjs", ".css", ".json", ".html", ".svg", ".webmanifest", ".txt", ".md":
		return true
	}
	return false
}

func dateiTyp(name string) string {
	ext := filepath.Ext(name)
	if ext == ".webmanifest" {
		// Kennt Go nicht von sich aus; ohne Typ lehnen manche Browser
		// die Datei ab und die App lässt sich nicht als Verknüpfung
		// auf den Startbildschirm legen.
		return "application/manifest+json"
	}
	if ct := mime.TypeByExtension(ext); ct != "" {
		return ct
	}
	return "application/octet-stream"
}

// Modul-Importe mit relativem Pfad: from './x.js', import './x.js',
// import('./x.js'). Nur solche ohne eigene Abfrage.
var importMuster = regexp.MustCompile(`((?:\bfrom|\bimport)\s*\(?\s*)(['"])(\./[A-Za-z0-9_\-/]+\.js)(['"])`)

// Verweise in index.html auf eigene Dateien: href="/x.css", src="/app.js".
var htmlMuster = regexp.MustCompile(`((?:href|src)=")(/[A-Za-z0-9_\-/]+\.(?:js|css|png|webmanifest))(")`)

// fassen hängt die Fassung an die Verweise einer Datei.
func fassen(name string, b []byte, fassung string, module []string, dateien []string) []byte {
	ext := filepath.Ext(name)
	if ext != ".js" && ext != ".html" {
		return b
	}
	s := string(b)
	s = strings.ReplaceAll(s, platzhalterFassung, fassung)
	if ext == ".js" {
		s = importMuster.ReplaceAllString(s, "${1}${2}${3}?v="+fassung+"${4}")
		if strings.Contains(s, platzhalterDateien) {
			liste, _ := json.Marshal(dateien)
			s = strings.Replace(s, platzhalterDateien, string(liste), 1)
		}
		return []byte(s)
	}
	s = htmlMuster.ReplaceAllString(s, "${1}${2}?v="+fassung+"${3}")
	// Alle Module gleich ankündigen: ohne das findet der Browser api.js
	// erst, wenn app.js ganz da ist, und so weiter — auf einer langsamen
	// Verbindung viele Runden hintereinander.
	if len(module) > 0 {
		var vor strings.Builder
		for _, m := range module {
			vor.WriteString(`<link rel="modulepreload" href="` + m + `?v=` + fassung + `">` + "\n")
		}
		s = strings.Replace(s, "</head>", vor.String()+"</head>", 1)
	}
	return []byte(s)
}

func packen(b []byte) []byte {
	var buf bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	_, _ = zw.Write(b)
	_ = zw.Close()
	// Nur, wenn es sich lohnt.
	if buf.Len() >= len(b)-64 {
		return nil
	}
	return buf.Bytes()
}

func neueWebDatei(name string, b []byte) *webDatei {
	sum := sha256.Sum256(b)
	d := &webDatei{roh: b, etag: `"` + hex.EncodeToString(sum[:8]) + `"`, typ: dateiTyp(name)}
	if packbar(name) {
		d.gz = packen(b)
	}
	return d
}

// webBestand ist die ganze Oberfläche, fertig zum Ausliefern.
type webBestand struct {
	fassung string
	dateien map[string]*webDatei // Schlüssel: "/app.js"
}

// bestandBauen liest alle Dateien, rechnet die Fassung aus ihrem Inhalt
// und bereitet jede einmal vor (Verweise mit Fassung, gepackt).
func bestandBauen(fsys fs.FS, fassung string) (*webBestand, error) {
	roh := map[string][]byte{}
	var namen []string
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		roh["/"+p] = b
		namen = append(namen, "/"+p)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(namen)
	if fassung == "" {
		h := sha256.New()
		for _, n := range namen {
			h.Write([]byte(n))
			h.Write([]byte{0})
			h.Write(roh[n])
			h.Write([]byte{0})
		}
		fassung = hex.EncodeToString(h.Sum(nil))[:12]
	}
	// Module (für modulepreload) und die App-Hülle (für den Service
	// Worker): alles im obersten Ordner, ausser dem Service Worker selbst.
	var module, huelle []string
	for _, n := range namen {
		if strings.Count(n, "/") != 1 || n == "/sw.js" {
			continue
		}
		switch filepath.Ext(n) {
		case ".js":
			module = append(module, n)
			huelle = append(huelle, n+"?v="+fassung)
		case ".css", ".png", ".webmanifest":
			huelle = append(huelle, n+"?v="+fassung)
		}
	}
	// app.js lädt index.html ohnehin selbst.
	vor := module[:0:0]
	for _, m := range module {
		if m != "/app.js" {
			vor = append(vor, m)
		}
	}
	w := &webBestand{fassung: fassung, dateien: map[string]*webDatei{}}
	for _, n := range namen {
		w.dateien[n] = neueWebDatei(n, fassen(n, roh[n], fassung, vor, huelle))
	}
	return w, nil
}

// webApp liefert die eingebaute Oberfläche aus. Mit --web <ordner> lässt
// sie sich überschreiben — praktisch beim Entwickeln.
func webApp(override string) http.Handler {
	var bestand func() *webBestand

	if override != "" {
		// Beim Entwickeln: jede Anfrage liest den Ordner neu. Klein genug.
		bestand = func() *webBestand {
			b, err := bestandBauen(os.DirFS(override), devFassung)
			if err != nil {
				return &webBestand{fassung: devFassung, dateien: map[string]*webDatei{}}
			}
			return b
		}
	} else {
		sub, err := fs.Sub(webFS, "web")
		if err != nil {
			panic(err) // kann nur ein Fehler beim Bauen sein
		}
		// Eingebaute Dateien ändern sich nie: einmal vorbereiten. Der
		// Inhalt (ETag, Fassung) sagt, ob sich etwas geändert hat — ein
		// Neustart allein lässt nichts neu laden.
		fest, err := bestandBauen(sub, "")
		if err != nil {
			panic(err)
		}
		bestand = func() *webBestand { return fest }
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b := bestand()
		p := path.Clean("/" + r.URL.Path)

		d, ok := b.dateien[p]
		if !ok || p == "/" {
			// Jede Adresse, die keine Datei ist, bekommt die App selbst —
			// sie bringt ihre Ansichten mit. Ohne das wäre ein neu geladener
			// Unterpfad ein 404.
			d, ok = b.dateien["/index.html"]
			if !ok {
				http.Error(w, "die Oberfläche fehlt in diesem Build", http.StatusInternalServerError)
				return
			}
			p = "/index.html"
		}

		hd := w.Header()
		hd.Set("Content-Type", d.typ)
		// Die Oberfläche lädt nur, was von Lumen selbst kommt: kein fremdes
		// Skript, kein Einbetten in eine fremde Seite (gegen Clickjacking —
		// sonst liesse sich ein angemeldeter Besitzer unsichtbar auf
		// „Bibliothek entfernen" klicken lassen). Stil-Attribute bleiben
		// erlaubt, die Oberfläche setzt sie überall. Bilder dürfen
		// zusätzlich von TMDb kommen: Die Treffer bei „Anderen Titel
		// zuweisen" sind noch nicht gespeichert, ihre Poster liegen nur dort
		// (ohne Referer, siehe unten).
		hd.Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; "+
			"style-src 'self' 'unsafe-inline'; img-src 'self' data: blob: https://image.tmdb.org; media-src 'self' blob:; "+
			"connect-src 'self'; font-src 'self'; worker-src 'self'; object-src 'none'; base-uri 'none'; "+
			"form-action 'self'; frame-ancestors 'none'")
		hd.Set("X-Frame-Options", "DENY")
		hd.Set("X-Content-Type-Options", "nosniff")
		hd.Set("Referrer-Policy", "no-referrer")
		switch {
		case p == "/index.html" || p == "/sw.js" || b.fassung == devFassung:
			// Immer nachfragen; der ETag macht das billig (304).
			hd.Set("Cache-Control", "no-cache")
		case r.URL.Query().Get("v") == b.fassung:
			// Mit der richtigen Fassung ändert sich die Datei nie mehr.
			hd.Set("Cache-Control", einJahr)
		default:
			// Ohne oder mit einer alten Fassung (eine Seite von vor dem
			// Update): ausliefern, aber nicht aufbewahren lassen — sonst
			// klebte unter der alten Adresse der neue Inhalt.
			hd.Set("Cache-Control", "no-cache")
		}

		body, etag := d.roh, d.etag
		if d.gz != nil {
			hd.Add("Vary", "Accept-Encoding")
			if nimmtGzip(r) {
				body = d.gz
				etag = strings.TrimSuffix(d.etag, `"`) + `-gz"`
				hd.Set("Content-Encoding", "gzip")
			}
		}
		hd.Set("ETag", etag)
		// Kein Last-Modified: Ob sich etwas geändert hat, sagt allein der
		// Inhalt (ETag). Ältere Fassungen gaben die Startzeit an; ein
		// Browser mit so einem Stand bekäme sonst ein falsches 304.
		http.ServeContent(w, r, p, time.Time{}, bytes.NewReader(body))
	})
}

// nimmtGzip: versteht der Browser gepackte Antworten? „gzip;q=0" heisst nein.
func nimmtGzip(r *http.Request) bool {
	for _, teil := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		name, param, _ := strings.Cut(strings.TrimSpace(teil), ";")
		if strings.EqualFold(strings.TrimSpace(name), "gzip") {
			param = strings.ReplaceAll(strings.TrimSpace(param), " ", "")
			return param != "q=0" && param != "q=0.0" && param != "q=0.00" && param != "q=0.000"
		}
	}
	return false
}
