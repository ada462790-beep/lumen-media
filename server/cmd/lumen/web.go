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

import (
	"bytes"
	"embed"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

//go:embed all:web
var webFS embed.FS

// webApp liefert die eingebaute Oberfläche aus. Mit --web <ordner> lässt
// sie sich überschreiben — praktisch beim Entwickeln.
func webApp(override string) http.Handler {
	var open func(string) ([]byte, time.Time, bool)

	if override != "" {
		open = func(p string) ([]byte, time.Time, bool) {
			full := filepath.Join(override, filepath.FromSlash(p))
			// Ein Pfad aus einer Anfrage darf den Ordner nicht verlassen.
			if rel, err := filepath.Rel(override, full); err != nil ||
				strings.HasPrefix(rel, "..") {
				return nil, time.Time{}, false
			}
			st, err := os.Stat(full)
			if err != nil || st.IsDir() {
				return nil, time.Time{}, false
			}
			b, err := os.ReadFile(full)
			if err != nil {
				return nil, time.Time{}, false
			}
			return b, st.ModTime(), true
		}
	} else {
		sub, err := fs.Sub(webFS, "web")
		if err != nil {
			panic(err) // kann nur ein Fehler beim Bauen sein
		}
		// Eingebaute Dateien ändern sich nie; als Zeitstempel dient der
		// Start, damit Browser sie nach einem Update neu holen.
		started := time.Now()
		open = func(p string) ([]byte, time.Time, bool) {
			b, err := fs.ReadFile(sub, strings.TrimPrefix(p, "/"))
			if err != nil {
				return nil, time.Time{}, false
			}
			return b, started, true
		}
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := path.Clean("/" + r.URL.Path)

		body, mod, ok := open(p)
		if !ok || p == "/" {
			// Jede Adresse, die keine Datei ist, bekommt die App selbst —
			// sie bringt ihre Ansichten mit. Ohne das wäre ein neu geladener
			// Unterpfad ein 404.
			body, mod, ok = open("/index.html")
			if !ok {
				http.Error(w, "die Oberfläche fehlt in diesem Build", http.StatusInternalServerError)
				return
			}
			p = "/index.html"
		}

		if ct := mime.TypeByExtension(filepath.Ext(p)); ct != "" {
			w.Header().Set("Content-Type", ct)
		} else if filepath.Ext(p) == ".webmanifest" {
			// Kennt Go nicht von sich aus; ohne Typ lehnen manche Browser
			// die Datei ab und die App lässt sich nicht als Verknüpfung
			// auf den Startbildschirm legen.
			w.Header().Set("Content-Type", "application/manifest+json")
		}
		// Die Oberfläche lädt nur, was von Lumen selbst kommt: kein fremdes
		// Skript, kein Einbetten in eine fremde Seite (gegen Clickjacking —
		// sonst liesse sich ein angemeldeter Besitzer unsichtbar auf
		// „Bibliothek entfernen" klicken lassen). Stil-Attribute bleiben
		// erlaubt, die Oberfläche setzt sie überall. Bilder dürfen
		// zusätzlich von TMDb kommen: Die Treffer bei „Anderen Titel
		// zuweisen" sind noch nicht gespeichert, ihre Poster liegen nur dort
		// (ohne Referer, siehe unten).
		hd := w.Header()
		hd.Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; "+
			"style-src 'self' 'unsafe-inline'; img-src 'self' data: blob: https://image.tmdb.org; media-src 'self' blob:; "+
			"connect-src 'self'; font-src 'self'; object-src 'none'; base-uri 'none'; "+
			"form-action 'self'; frame-ancestors 'none'")
		hd.Set("X-Frame-Options", "DENY")
		hd.Set("X-Content-Type-Options", "nosniff")
		hd.Set("Referrer-Policy", "no-referrer")
		switch filepath.Ext(p) {
		case ".html":
			w.Header().Set("Cache-Control", "no-store")
		default:
			// Nachladen lassen, aber nur wenn sich etwas geändert hat.
			w.Header().Set("Cache-Control", "no-cache")
		}
		http.ServeContent(w, r, p, mod, bytes.NewReader(body))
	})
}
