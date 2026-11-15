// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"bytes"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Entsteht das Standbild noch, antwortet der Server der Web-App
// (?spaeter=1) mit 200 und einem durchsichtigen Platzhalter von 2×1
// Punkten statt 503 — die Konsole bleibt leer, die App lädt später nach.
// Ohne den Parameter (Apps) bleibt es bei 503 mit Retry-After.
func TestStandbildSpaeterOhne503(t *testing.T) {
	r := httptest.NewRequest("GET", "/api/v1/art/x/frame?spaeter=1", nil)
	w := httptest.NewRecorder()
	standbildNochNicht(w, r)
	if w.Code != http.StatusOK || w.Header().Get("X-Lumen-Ersatz") != "spaeter" ||
		w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Retry-After") == "" {
		t.Fatalf("mit spaeter=1: %d %v", w.Code, w.Header())
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(w.Body.Bytes()))
	if err != nil || cfg.Width != 2 || cfg.Height != 1 {
		t.Fatalf("Platzhalter: %v %+v", err, cfg)
	}
	r = httptest.NewRequest("GET", "/api/v1/art/x/frame", nil)
	w = httptest.NewRecorder()
	standbildNochNicht(w, r)
	if w.Code != http.StatusServiceUnavailable || w.Header().Get("Retry-After") == "" {
		t.Fatalf("ohne spaeter: %d %v", w.Code, w.Header())
	}
}
