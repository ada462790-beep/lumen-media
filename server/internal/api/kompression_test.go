// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestKompressionNurWoEsPasst(t *testing.T) {
	gross := strings.Repeat(`{"titel":"Ein Film"},`, 200)
	h := kompression(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, gross)
		case "/klein":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"a":1}`)
		case "/bild":
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = io.WriteString(w, gross)
		case "/leer":
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	hol := func(pfad, ae string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", pfad, nil)
		if ae != "" {
			req.Header.Set("Accept-Encoding", ae)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	r := hol("/json", "gzip, deflate")
	if r.Header().Get("Content-Encoding") != "gzip" || !strings.Contains(r.Header().Get("Vary"), "Accept-Encoding") {
		t.Fatalf("JSON nicht gepackt: %v", r.Header())
	}
	zr, err := gzip.NewReader(r.Body)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(zr)
	if string(b) != gross {
		t.Fatal("entpackt nicht gleich")
	}
	if r := hol("/json", ""); r.Header().Get("Content-Encoding") != "" || r.Body.String() != gross {
		t.Fatal("ohne Accept-Encoding gepackt")
	}
	if r := hol("/json", "gzip;q=0"); r.Header().Get("Content-Encoding") != "" {
		t.Fatal("gzip;q=0 gepackt")
	}
	if r := hol("/klein", "gzip"); r.Header().Get("Content-Encoding") != "" || r.Body.String() != `{"a":1}` {
		t.Fatal("kleine Antwort gepackt")
	}
	if r := hol("/bild", "gzip"); r.Header().Get("Content-Encoding") != "" {
		t.Fatal("Bild gepackt")
	}
	if r := hol("/leer", "gzip"); r.Code != 204 || r.Header().Get("Content-Encoding") != "" {
		t.Fatal("204 verändert")
	}
}

// Der Bildschlüssel bleibt gleich (auch nach einem Neustart), gilt nur für
// Bilder und nur, solange es das Gerät gibt.
func TestBildschluessel(t *testing.T) {
	h := newHarness(t)
	schluessel := func() (string, string) {
		_, raw := h.do("GET", "/api/v1/me/media-key", "", h.tok)
		var out struct {
			Key     string `json:"key"`
			BildKey string `json:"bildKey"`
			Sek     int    `json:"bildGueltigSek"`
		}
		if err := json.Unmarshal(raw, &out); err != nil || out.BildKey == "" || out.Sek <= 0 {
			t.Fatalf("media-key: %s", raw)
		}
		return out.Key, out.BildKey
	}
	k1, b1 := schluessel()
	k2, b2 := schluessel()
	if k1 == k2 {
		t.Fatal("Medienschlüssel soll bei jedem Abruf neu sein")
	}
	if b1 != b2 {
		t.Fatal("Bildschlüssel ändert sich zwischen zwei Abrufen")
	}
	status := func(pfad string) int {
		resp, _ := h.do("GET", pfad, "", "")
		return resp.StatusCode
	}
	// Ein Bild, das es nicht gibt: 404 heisst, die Anmeldung ging durch.
	if got := status("/api/v1/persons/niemand/bild?key=" + b1); got != 404 {
		t.Fatalf("Bild mit Bildschlüssel: %d", got)
	}
	if got := status("/api/v1/stream/f_x/trickplay?key=" + b1); got != 401 {
		t.Fatalf("Strom mit Bildschlüssel: %d", got)
	}
	if got := status("/api/v1/persons/niemand/bild?key=b.zz.falsch"); got != 401 {
		t.Fatalf("falscher Bildschlüssel: %d", got)
	}
	// Neustart: Der Server kennt keinen Schlüssel mehr und rechnet nach.
	bilder.mu.Lock()
	bilder.nach = map[string][32]byte{}
	bilder.gebaut = time.Time{}
	bilder.mu.Unlock()
	if got := status("/api/v1/persons/niemand/bild?key=" + b1); got != 404 {
		t.Fatalf("nach Neustart: %d", got)
	}
	// Abgemeldet: Schlüssel ungültig.
	h.do("POST", "/api/v1/me/sign-out-everywhere", "", h.tok)
	if got := status("/api/v1/persons/niemand/bild?key=" + b1); got != 401 {
		t.Fatalf("nach Abmelden: %d", got)
	}
}

// Filme, Ströme, HLS und Bilder laufen nicht durch den Packer: Der Handler
// bekommt den Schreiber des Servers (mit ReadFrom, also sendfile).
// Untertitel und kleine JSON-Antworten unter /stream/ werden weiter gepackt.
func TestKompressionNichtFuerMedien(t *testing.T) {
	var gepackt bool
	h := kompression(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, gepackt = w.(*gzipSchreiber)
	}))
	for pfad, soll := range map[string]bool{
		"/api/v1/stream/f1":                   false,
		"/api/v1/stream/f1/browser.mp4":       false,
		"/api/v1/stream/f1/hls/s1/seg3.ts":    false,
		"/api/v1/stream/f1/hls/s1/media.m3u8": false,
		"/api/v1/stream/f1/download":          false,
		"/api/v1/stream/f1/trickplay/2":       false,
		"/api/v1/art/it1/poster":              false,
		"/api/v1/avatar/acc1":                 false,
		"/api/v1/persons/Name/bild":           false,
		"/api/v1/wishes/poster/w185/a.jpg":    false,
		"/api/v1/stream/f1/subtitles/2":       true,
		"/api/v1/stream/f1/plan":              true,
		"/api/v1/items":                       true,
	} {
		req := httptest.NewRequest("GET", pfad, nil)
		req.Header.Set("Accept-Encoding", "gzip")
		h.ServeHTTP(httptest.NewRecorder(), req)
		if gepackt != soll {
			t.Errorf("%s: durch den Packer %v, erwartet %v", pfad, gepackt, soll)
		}
	}
}

// leserZaehler: ein Schreiber mit ReadFrom, der zählt, ob es benutzt wurde.
type leserZaehler struct {
	*httptest.ResponseRecorder
	readFrom int
}

func (l *leserZaehler) ReadFrom(r io.Reader) (int64, error) {
	l.readFrom++
	return io.Copy(l.ResponseRecorder, r)
}

// Packt der Schreiber nicht (Bild, Teilinhalt), reicht er ReadFrom durch.
func TestKompressionReichtReadFromDurch(t *testing.T) {
	h := kompression(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		if _, ok := w.(io.ReaderFrom); !ok {
			t.Fatal("kein ReadFrom")
		}
		_, _ = w.(io.ReaderFrom).ReadFrom(strings.NewReader(strings.Repeat("x", 5000)))
	}))
	req := httptest.NewRequest("GET", "/api/v1/sonstwas", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := &leserZaehler{ResponseRecorder: httptest.NewRecorder()}
	h.ServeHTTP(rec, req)
	if rec.readFrom != 1 || rec.Body.Len() != 5000 || rec.Header().Get("Content-Encoding") != "" {
		t.Fatalf("ReadFrom %d, %d Bytes, %v", rec.readFrom, rec.Body.Len(), rec.Header())
	}
	// Gepackt: über Write, und was ankommt, ist gzip.
	h2 := kompression(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.(io.ReaderFrom).ReadFrom(strings.NewReader(strings.Repeat(`{"a":1}`, 500)))
	}))
	rec2 := &leserZaehler{ResponseRecorder: httptest.NewRecorder()}
	h2.ServeHTTP(rec2, req)
	if rec2.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("JSON über ReadFrom nicht gepackt: %v", rec2.Header())
	}
	zr, err := gzip.NewReader(rec2.Body)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := io.ReadAll(zr); len(b) != 3500 {
		t.Fatalf("entpackt %d Bytes", len(b))
	}
}
