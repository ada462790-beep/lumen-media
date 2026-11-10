// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// Derselbe Ordner (oder einer darin) gibt keine zweite Bibliothek: Das
// Löschen der einen leerte sonst die andere.
func TestBibliothekNichtDoppelt(t *testing.T) {
	h := newHarness(t)
	unter := filepath.Join(h.root, "Serien")
	_ = os.MkdirAll(unter, 0o755)
	anlegen := func(pfad string) int {
		resp, _ := h.do("POST", "/api/v1/admin/libraries",
			`{"name":"X","kind":"series","path":"`+pfad+`","language":"de"}`, h.tok)
		return resp.StatusCode
	}
	if c := anlegen(h.root); c != http.StatusOK {
		t.Fatalf("erste Bibliothek: %d", c)
	}
	for _, p := range []string{h.root, h.root + "/", unter} {
		if c := anlegen(p); c != http.StatusConflict {
			t.Errorf("%s: %d, erwartet 409", p, c)
		}
	}
	if !ordnerUeberschneiden("/a/b", "/a") || ordnerUeberschneiden("/a/bc", "/a/b") {
		t.Error("ordnerUeberschneiden falsch")
	}
}
