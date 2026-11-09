// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Eine .srt neben dem Film wird als Spur gefunden und als WebVTT
// ausgeliefert — auch ohne FFmpeg.
func TestUntertitelDanebenAlsVTT(t *testing.T) {
	h := newHarness(t)
	itemID, fileID := h.film("Nordlicht", 1)
	srt := "\ufeff1\r\n00:00:01,000 --> 00:00:02,500\r\nHallo\r\n\r\n2\r\n00:01:00,250 --> 00:01:02,000\r\nZwei\r\nZeilen\r\n"
	if err := os.WriteFile(filepath.Join(h.root, "Nordlicht.de.srt"), []byte(srt), 0o644); err != nil {
		t.Fatal(err)
	}

	// Der Titel liefert die Spur mit.
	_, raw := h.do("GET", "/api/v1/items/"+itemID, "", h.tok)
	var d struct {
		Files []struct {
			Tracks []struct {
				Index    int
				Kind     string
				Language string
				External bool
			}
		}
	}
	_ = json.Unmarshal(raw, &d)
	if len(d.Files) != 1 || len(d.Files[0].Tracks) != 1 {
		t.Fatalf("Spuren: %s", raw)
	}
	tr := d.Files[0].Tracks[0]
	if tr.Kind != "subtitle" || tr.Language != "de" || !tr.External || tr.Index < 1000 {
		t.Fatalf("Spur: %+v", tr)
	}
	if strings.Contains(string(raw), "Nordlicht.de.srt") {
		t.Error("der Pfad der Untertiteldatei steht in der Antwort")
	}

	resp, body := h.do("GET", "/api/v1/stream/"+fileID+"/subtitles/1000.vtt", "", h.tok)
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/vtt") {
		t.Fatalf("VTT: %d %s %s", resp.StatusCode, resp.Header.Get("Content-Type"), body)
	}
	vtt := string(body)
	for _, want := range []string{"WEBVTT\n", "00:00:01.000 --> 00:00:02.500\nHallo\n", "00:01:00.250 --> 00:01:02.000\nZwei\nZeilen\n"} {
		if !strings.Contains(vtt, want) {
			t.Errorf("fehlt %q in:\n%s", want, vtt)
		}
	}
	if strings.Contains(vtt, "\r") || strings.Contains(vtt, "\ufeff") || strings.Contains(vtt, "\n1\n") {
		t.Errorf("Wagenrücklauf, BOM oder Nummer übrig:\n%q", vtt)
	}
	// Eine Spur, die es nicht gibt: 404.
	resp, _ = h.do("GET", "/api/v1/stream/"+fileID+"/subtitles/7.vtt", "", h.tok)
	if resp.StatusCode != 404 {
		t.Errorf("unbekannte Spur: %d", resp.StatusCode)
	}
}
