// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"
)

func holen(t *testing.T, h http.Handler, pfad string, kopf map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("GET", pfad, nil)
	for k, v := range kopf {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// Die eingebaute Oberfläche: Fassung in allen Verweisen, lange
// Aufbewahrung nur mit passender Fassung, index.html immer nachfragen.
func TestWebFassungUndAufbewahrung(t *testing.T) {
	h := webApp("")
	idx := holen(t, h, "/", nil)
	if idx.Code != 200 || idx.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("index: %d %q", idx.Code, idx.Header().Get("Cache-Control"))
	}
	m := regexp.MustCompile(`src="/app\.js\?v=([0-9a-f]{12})"`).FindStringSubmatch(idx.Body.String())
	if m == nil {
		t.Fatalf("app.js ohne Fassung in index.html")
	}
	v := m[1]
	if !strings.Contains(idx.Body.String(), `<link rel="modulepreload" href="/api.js?v=`+v+`">`) {
		t.Fatalf("modulepreload fehlt")
	}
	if !strings.Contains(idx.Body.String(), `href="/app.css?v=`+v+`"`) {
		t.Fatalf("app.css ohne Fassung")
	}

	app := holen(t, h, "/app.js?v="+v, nil)
	if got := app.Header().Get("Cache-Control"); got != einJahr {
		t.Fatalf("app.js mit Fassung: Cache-Control %q", got)
	}
	if !strings.Contains(app.Body.String(), "from './api.js?v="+v+"'") {
		t.Fatalf("Import in app.js ohne Fassung")
	}
	if strings.Contains(app.Body.String(), platzhalterFassung) {
		t.Fatalf("Platzhalter nicht ersetzt")
	}
	// Alte oder fehlende Fassung: ausliefern, aber nicht aufbewahren.
	for _, p := range []string{"/app.js", "/app.js?v=000000000000"} {
		if got := holen(t, h, p, nil).Header().Get("Cache-Control"); got != "no-cache" {
			t.Fatalf("%s: Cache-Control %q", p, got)
		}
	}
	// ETag aus dem Inhalt: 304 beim Nachfragen, auch nach einem Neustart.
	et := app.Header().Get("ETag")
	if et == "" || webApp("") == nil {
		t.Fatal("kein ETag")
	}
	if got := holen(t, webApp(""), "/app.js?v="+v, map[string]string{"If-None-Match": et}).Code; got != 304 {
		t.Fatalf("If-None-Match nach Neustart: %d", got)
	}
	// Service Worker: Fassung und Dateiliste eingesetzt, nie aufbewahrt.
	sw := holen(t, h, "/sw.js", nil)
	if sw.Header().Get("Cache-Control") != "no-cache" || !strings.Contains(sw.Body.String(), "'"+v+"'") ||
		strings.Contains(sw.Body.String(), platzhalterDateien) || !strings.Contains(sw.Body.String(), "/app.css?v="+v) {
		t.Fatalf("sw.js nicht vorbereitet")
	}
}

func TestWebGzip(t *testing.T) {
	h := webApp("")
	roh := holen(t, h, "/app.js", nil)
	gz := holen(t, h, "/app.js", map[string]string{"Accept-Encoding": "br, gzip"})
	if gz.Header().Get("Content-Encoding") != "gzip" || !strings.Contains(gz.Header().Get("Vary"), "Accept-Encoding") {
		t.Fatalf("nicht gepackt: %v", gz.Header())
	}
	if gz.Body.Len()*2 > roh.Body.Len() {
		t.Fatalf("gepackt %d, roh %d", gz.Body.Len(), roh.Body.Len())
	}
	zr, err := gzip.NewReader(bytes.NewReader(gz.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	ent, _ := io.ReadAll(zr)
	if !bytes.Equal(ent, roh.Body.Bytes()) {
		t.Fatal("entpackt anders als roh")
	}
	if gz.Header().Get("ETag") == roh.Header().Get("ETag") {
		t.Fatal("gleicher ETag für gepackt und roh")
	}
	if holen(t, h, "/app.js", map[string]string{"Accept-Encoding": "gzip;q=0"}).Header().Get("Content-Encoding") != "" {
		t.Fatal("gzip;q=0 trotzdem gepackt")
	}
	// Bilder nie.
	if holen(t, h, "/symbol-192.png", map[string]string{"Accept-Encoding": "gzip"}).Header().Get("Content-Encoding") != "" {
		t.Fatal("Bild gepackt")
	}
}

// Die Fassung hängt am Inhalt: eine geänderte Datei, eine neue Fassung.
func TestWebFassungAusInhalt(t *testing.T) {
	a := fstest.MapFS{"index.html": {Data: []byte(`<head></head><script type="module" src="/app.js"></script>`)},
		"app.js": {Data: []byte(`import { x } from './b.js';`)}, "b.js": {Data: []byte(`export const x = 1;`)}}
	b1, err := bestandBauen(a, "")
	if err != nil {
		t.Fatal(err)
	}
	b2, _ := bestandBauen(a, "")
	if b1.fassung != b2.fassung {
		t.Fatal("Fassung nicht stabil")
	}
	a["b.js"] = &fstest.MapFile{Data: []byte(`export const x = 2;`)}
	b3, _ := bestandBauen(a, "")
	if b3.fassung == b1.fassung {
		t.Fatal("Fassung ändert sich nicht mit dem Inhalt")
	}
	if got := string(b3.dateien["/app.js"].roh); got != `import { x } from './b.js?v=`+b3.fassung+`';` {
		t.Fatalf("Import: %s", got)
	}
}
