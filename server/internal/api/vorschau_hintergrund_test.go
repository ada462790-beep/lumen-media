// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"net/http"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"lumen/internal/store"
)

// Die bewegte Vorschau entsteht im Hintergrund: Der erste Abruf wartet
// nicht auf FFmpeg (das dauerte bis 1,6 s, auf einem NAS länger), sondern
// sagt sofort „noch nicht" — der Web-App (?spaeter=1) mit 204 ohne
// Fehler, den Apps wie bisher mit 404. Kurz darauf ist sie da.
func TestVorschauImHintergrund(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ohne FFmpeg keine Vorschau")
	}
	h := newHarness(t)
	pfad := filepath.Join(h.root, "Vorschau (2020).mp4")
	cmd := exec.Command("ffmpeg", "-nostdin", "-loglevel", "error",
		"-f", "lavfi", "-i", "testsrc=duration=4:size=160x90:rate=10",
		"-c:v", "libx264", "-preset", "ultrafast", "-y", pfad)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("Testvideo nicht erzeugbar: %v %s", err, out)
	}
	lib, _ := h.st.CreateLibrary(t.Context(), "Filme", "movie", h.root, "de")
	it := &store.Item{LibraryID: lib.ID, Kind: "movie", Title: "Vorschau", Year: 2020, MatchState: "matched"}
	_ = h.st.UpsertItem(t.Context(), it)
	_ = h.st.UpsertFile(t.Context(), &store.MediaFile{ItemID: it.ID, Path: pfad, Container: "mp4",
		VideoCodec: "h264", Width: 160, Height: 90, DurationMS: 4000})

	weg := "/api/v1/art/" + it.ID + "/vorschau"
	beginn := time.Now()
	resp, _ := h.do("GET", weg+"?spaeter=1", "", h.tok)
	if d := time.Since(beginn); d > 500*time.Millisecond {
		t.Errorf("erster Abruf wartete %v auf FFmpeg", d)
	}
	if resp.StatusCode != http.StatusNoContent || resp.Header.Get("X-Lumen-Ersatz") != "spaeter" ||
		resp.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("erster Abruf (Web): %d %v", resp.StatusCode, resp.Header)
	}
	var ok bool
	for i := 0; i < 100 && !ok; i++ {
		time.Sleep(100 * time.Millisecond)
		resp, _ = h.do("GET", weg, "", h.tok)
		ok = resp.StatusCode == http.StatusOK && resp.Header.Get("Content-Type") == "video/mp4"
		if !ok && resp.StatusCode != http.StatusNotFound {
			t.Fatalf("während des Vorbereitens (App): %d", resp.StatusCode)
		}
	}
	if !ok {
		t.Fatal("Vorschau nach zehn Sekunden nicht da")
	}
}
