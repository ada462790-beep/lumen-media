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
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// sperreHalten hält die Schreibsperre der Datenbank wie eine lange
// Arbeit im Hintergrund, bis die zurückgegebene Funktion gerufen wird.
func (h *harness) sperreHalten() func() {
	h.t.Helper()
	conn, err := h.st.DB().Conn(h.t.Context())
	if err != nil {
		h.t.Fatal(err)
	}
	if _, err := conn.ExecContext(h.t.Context(), `BEGIN IMMEDIATE`); err != nil {
		h.t.Fatal(err)
	}
	if _, err := conn.ExecContext(h.t.Context(), `UPDATE setting SET value = value WHERE key = 'gibt-es-nicht'`); err != nil {
		h.t.Fatal(err)
	}
	return func() {
		_, _ = conn.ExecContext(h.t.Context(), `COMMIT`)
		_ = conn.Close()
	}
}

// Der erste Aufruf des Tages schrieb „zuletzt gesehen" in der Anfrage
// selbst — hielt gerade eine Arbeit im Hintergrund die Schreibsperre,
// wartete jede Anfrage bis zu fünf Sekunden (busy_timeout). Jetzt
// schreibt Lumen das nebenbei, und die Seite kommt sofort.
func TestErsterAufrufWartetNichtAufSchreibsperre(t *testing.T) {
	h := newHarness(t)
	h.film("Ein Film", 1)
	// Gestern zuletzt gesehen.
	if _, err := h.st.DB().Exec(`UPDATE token SET last_seen_at = last_seen_at - 86400`); err != nil {
		t.Fatal(err)
	}
	h.api.antworten.leeren()
	los := h.sperreHalten()
	freigegeben := false
	defer func() {
		if !freigegeben {
			los()
		}
	}()
	for _, weg := range []string{"/api/v1/me", "/api/v1/home", "/api/v1/items?kind=movie"} {
		start := time.Now()
		resp, raw := h.do("GET", weg, "", h.tok)
		if resp.StatusCode != 200 {
			t.Fatalf("%s: %d %s", weg, resp.StatusCode, raw)
		}
		if d := time.Since(start); d > time.Second {
			t.Fatalf("%s wartete %v auf die Schreibsperre", weg, d)
		}
		if st := resp.Header.Get("Server-Timing"); !strings.Contains(st, "db;dur=") || !strings.Contains(st, "gesamt;dur=") {
			t.Fatalf("%s: Server-Timing fehlt: %q", weg, st)
		}
	}
	los()
	freigegeben = true
	// Nebenbei geschrieben, sobald die Sperre frei ist.
	var tag int64
	bis := time.Now().Add(10 * time.Second)
	for time.Now().Before(bis) {
		_ = h.st.DB().QueryRow(`SELECT max(last_seen_at) FROM token`).Scan(&tag)
		if tag == time.Now().UTC().Truncate(24*time.Hour).Unix() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("zuletzt gesehen wurde nicht nachgetragen: %d", tag)
}

// Startseite und Listen kommen aus dem Speicher, bis sich etwas ändert —
// dann sofort die neue Antwort.
func TestAntwortspeicherFolgtAenderungen(t *testing.T) {
	h := newHarness(t)
	id, _ := h.film("Erster Film", 1)
	merkliste := func() int {
		t.Helper()
		resp, raw := h.do("GET", "/api/v1/home", "", h.tok)
		if resp.StatusCode != 200 {
			t.Fatalf("home: %d", resp.StatusCode)
		}
		var b struct {
			Rows []struct {
				Key   string
				Items []struct{ ID string }
			}
		}
		_ = json.Unmarshal(raw, &b)
		for _, r := range b.Rows {
			if r.Key == "watchlist" {
				return len(r.Items)
			}
		}
		return 0
	}
	if n := merkliste(); n != 0 {
		t.Fatalf("Merkliste vorher: %d", n)
	}
	// Zweimal dasselbe: die zweite Antwort braucht die Datenbank nicht.
	resp, _ := h.do("GET", "/api/v1/home", "", h.tok)
	if st := resp.Header.Get("Server-Timing"); !strings.HasPrefix(st, "db;dur=0.0,") {
		t.Fatalf("zweite Startseite nicht aus dem Speicher: %q", st)
	}
	if resp, raw := h.do("PUT", "/api/v1/watchlist/"+id, "", h.tok); resp.StatusCode >= 300 {
		t.Fatalf("merken: %d %s", resp.StatusCode, raw)
	}
	if n := merkliste(); n != 1 {
		t.Fatalf("Merkliste nach dem Merken: %d (alte Antwort aus dem Speicher?)", n)
	}
	// Ein anderes Profil bekommt nie die Antwort des ersten.
	gast := h.gast("Gast", false)
	resp, raw := h.do("GET", "/api/v1/home", "", gast)
	if resp.StatusCode != 200 || strings.Contains(string(raw), `"watchlist"`) {
		t.Fatalf("Gast sieht fremde Merkliste: %d %s", resp.StatusCode, raw)
	}
}

// Poster kommen in der verlangten Breite, ohne Angabe höchstens 780
// Punkte breit; Logos bleiben, wie sie sind.
func TestPosterInPassenderBreite(t *testing.T) {
	h := newHarness(t)
	id, _ := h.film("Grosses Poster", 1)
	ordner := h.api.meta.ArtDir()
	bild := func(name string, b, hh int) {
		t.Helper()
		img := image.NewRGBA(image.Rect(0, 0, b, hh))
		for y := 0; y < hh; y++ {
			for x := 0; x < b; x++ {
				img.Set(x, y, color.RGBA{200, 40, 40, 255})
			}
		}
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(ordner, name), buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	bild(id+"-poster.jpg", 1000, 1500)
	bild(id+"-logo.jpg", 1000, 400)
	for _, k := range []string{"poster", "logo"} {
		if err := h.st.SetArtwork(t.Context(), id, k, id+"-"+k+".jpg", ""); err != nil {
			t.Fatal(err)
		}
	}
	breite := func(weg string) (int, string) {
		t.Helper()
		resp, raw := h.do("GET", weg, "", h.tok)
		if resp.StatusCode != 200 {
			t.Fatalf("%s: %d %s", weg, resp.StatusCode, raw)
		}
		cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
		if err != nil {
			t.Fatalf("%s: %v", weg, err)
		}
		return cfg.Width, resp.Header.Get("Content-Type")
	}
	for _, f := range []struct {
		weg  string
		soll int
	}{
		{"/api/v1/art/" + id + "/poster?w=342", 342},
		{"/api/v1/art/" + id + "/poster?w=300", 342}, // auf die nächste Stufe
		{"/api/v1/art/" + id + "/poster?w=99999", 1000},
		{"/api/v1/art/" + id + "/poster", 780},
		{"/api/v1/art/" + id + "/poster?w=342", 342}, // aus dem Speicher
		{"/api/v1/art/" + id + "/logo?w=185", 1000},
	} {
		if b, art := breite(f.weg); b != f.soll || art != "image/jpeg" {
			t.Fatalf("%s: %d Punkte (%s), soll %d", f.weg, b, art, f.soll)
		}
	}
	// Verkleinert liegt es im Datenordner — beim nächsten Start schnell da.
	if _, err := os.Stat(filepath.Join(h.daten, kleinOrdner, "342", id+"-poster.jpg")); err != nil {
		t.Fatalf("verkleinertes Bild nicht abgelegt: %v", err)
	}
}

// Die Farbe bleibt beim Verkleinern erhalten (Mittelwert, keine Ränder).
func TestVerkleinernBehaeltFarbe(t *testing.T) {
	src := image.NewYCbCr(image.Rect(0, 0, 300, 450), image.YCbCrSubsampleRatio420)
	y, cb, cr := color.RGBToYCbCr(30, 120, 220)
	for i := range src.Y {
		src.Y[i] = y
	}
	for i := range src.Cb {
		src.Cb[i], src.Cr[i] = cb, cr
	}
	klein := verkleinern(src, 100, 150)
	if klein.Rect.Dx() != 100 || klein.Rect.Dy() != 150 {
		t.Fatalf("Grösse %v", klein.Rect)
	}
	for _, p := range []image.Point{{0, 0}, {99, 149}, {50, 75}} {
		c := klein.RGBAAt(p.X, p.Y)
		if d := int(c.R) - 30; d < -3 || d > 3 || int(c.B) < 217 || int(c.B) > 223 {
			t.Fatalf("Farbe bei %v: %v", p, c)
		}
	}
}

// Langsame Anfragen kommen höchstens einmal je Weg und Minute ins
// Protokoll.
func TestLangsameAnfrageGedrosselt(t *testing.T) {
	jetzt := time.Now()
	if !langsamMelden("GET /test/{id}", jetzt) {
		t.Fatal("erste Meldung unterdrückt")
	}
	if langsamMelden("GET /test/{id}", jetzt.Add(30*time.Second)) {
		t.Fatal("zweite Meldung innerhalb einer Minute")
	}
	if !langsamMelden("GET /anders", jetzt.Add(30*time.Second)) {
		t.Fatal("anderer Weg unterdrückt")
	}
	if !langsamMelden("GET /test/{id}", jetzt.Add(61*time.Second)) {
		t.Fatal("nach einer Minute unterdrückt")
	}
}
