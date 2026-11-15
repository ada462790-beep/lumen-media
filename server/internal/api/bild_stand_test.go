// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testJPEG(t *testing.T, b, h int, c color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, b, h))
	for y := 0; y < h; y++ {
		for x := 0; x < b; x++ {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// Ein neues Poster hat eine neue Adresse (?v=…): Listen und Startseite
// zeigen es sofort, nicht erst nach zehn Minuten. Mit Stand darf der
// Browser das Bild lange behalten; die alte verkleinerte Fassung ist weg.
func TestNeuesPosterNeueAdresse(t *testing.T) {
	h := newHarness(t)
	id, _ := h.film("Neues Poster", 1)
	hochladen := func(c color.RGBA) {
		t.Helper()
		req, _ := http.NewRequest("POST", h.srv.URL+"/api/v1/admin/items/"+id+"/artwork/poster",
			bytes.NewReader(testJPEG(t, 1000, 1500, c)))
		req.Header.Set("Content-Type", "image/jpeg")
		req.Header.Set("Authorization", "Bearer "+h.tok)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("hochladen: %d", resp.StatusCode)
		}
	}
	poster := func() string {
		t.Helper()
		_, raw := h.do("GET", "/api/v1/items/"+id, "", h.tok)
		var v struct{ Item struct{ Poster string } }
		_ = json.Unmarshal(raw, &v)
		return v.Item.Poster
	}
	hochladen(color.RGBA{200, 0, 0, 255})
	erst := poster()
	if !strings.Contains(erst, "?v=") {
		t.Fatalf("Posteradresse ohne Stand: %q", erst)
	}
	resp, _ := h.do("GET", erst+"&w=342", "", h.tok)
	if cc := resp.Header.Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Errorf("mit Stand: Cache-Control %q, erwartet lange haltbar", cc)
	}
	// Ohne Stand (ältere Apps): wie bisher kurz.
	resp, _ = h.do("GET", "/api/v1/art/"+id+"/poster?w=342", "", h.tok)
	if cc := resp.Header.Get("Cache-Control"); strings.Contains(cc, "immutable") {
		t.Errorf("ohne Stand: Cache-Control %q", cc)
	}
	klein, _ := filepath.Glob(filepath.Join(h.daten, kleinOrdner, "*", id+"-poster-hand-*.jpg"))
	if len(klein) != 1 {
		t.Fatalf("verkleinerte Fassungen: %v", klein)
	}
	hochladen(color.RGBA{0, 0, 200, 255})
	zweit := poster()
	if zweit == erst || !strings.Contains(zweit, "?v=") {
		t.Fatalf("Adresse nach neuem Poster: %q (vorher %q)", zweit, erst)
	}
	if _, err := os.Stat(klein[0]); err == nil {
		t.Errorf("alte verkleinerte Fassung liegt noch da: %s", klein[0])
	}
}

// Verwaiste verkleinerte Bilder (das Original gibt es nicht mehr) räumt
// der Hausmeister weg, die übrigen bleiben.
func TestVerwaisteKleineBilderWeg(t *testing.T) {
	h := newHarness(t)
	id, _ := h.film("Klein", 1)
	ordner := h.api.meta.ArtDir()
	_ = os.WriteFile(filepath.Join(ordner, id+"-poster.jpg"), testJPEG(t, 10, 15, color.RGBA{1, 2, 3, 255}), 0o644)
	da := filepath.Join(h.daten, kleinOrdner, "342", id+"-poster.jpg")
	weg := filepath.Join(h.daten, kleinOrdner, "342", id+"-poster-hand-abc.jpg")
	for _, p := range []string{da, weg} {
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		_ = os.WriteFile(p, []byte("x"), 0o644)
	}
	h.api.verwaisteBilderWeg(t.Context())
	if _, err := os.Stat(da); err != nil {
		t.Errorf("gültige verkleinerte Fassung weg: %v", err)
	}
	if _, err := os.Stat(weg); err == nil {
		t.Errorf("verwaiste verkleinerte Fassung noch da")
	}
}
