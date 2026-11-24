// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"embed"
	"net/url"
	"path"
	"strings"
)

//go:embed seite
var seitenDateien embed.FS

//go:embed bruecke.js
var brueckeQuelle string

// brueckeSkript ist bruecke.js mit der Fassung dieser App und dem, was
// ihr Player kann: player = "mpv", wenn libmpv bereitliegt (dann übergibt
// die Web-App Filme an die App), sonst leer. faehigkeiten: was mpv dem
// Server als abspielbar meldet (mpvFaehigkeiten).
func brueckeSkript(player string) string {
	ersatz := strings.NewReplacer(
		"__LUMEN_VERSION__", strings.ReplaceAll(version, "'", ""),
		"__LUMEN_PLAYER__", map[bool]string{true: "mpv", false: ""}[player == "mpv"],
		"__LUMEN_FAEHIGKEITEN__", strings.ReplaceAll(mpvFaehigkeiten(), "'", ""),
	)
	return ersatz.Replace(brueckeQuelle)
}

// seitenKopf: Die eingebaute Seite lädt nur, was von ihr selbst kommt.
const seitenKopf = "Content-Type: %s\r\n" +
	"Content-Security-Policy: default-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'\r\n" +
	"Cache-Control: no-store\r\n" +
	"X-Content-Type-Options: nosniff"

// SeitenAntwort beantwortet eine Anfrage an http://lumen.localhost/…
// aus den eingebauten Dateien.
func SeitenAntwort(adresse string) (inhalt []byte, typ string, status int) {
	u, err := url.Parse(adresse)
	if err != nil || !strings.EqualFold(u.Host, seitenHost) {
		return []byte("nicht gefunden"), "text/plain; charset=utf-8", 404
	}
	p := path.Clean("/" + u.Path)
	if p == "/" {
		p = "/index.html"
	}
	b, err := seitenDateien.ReadFile("seite" + p)
	if err != nil {
		return []byte("nicht gefunden"), "text/plain; charset=utf-8", 404
	}
	switch path.Ext(p) {
	case ".html":
		typ = "text/html; charset=utf-8"
	case ".css":
		typ = "text/css; charset=utf-8"
	case ".js":
		typ = "text/javascript; charset=utf-8"
	case ".png":
		typ = "image/png"
	default:
		typ = "application/octet-stream"
	}
	return b, typ, 200
}
