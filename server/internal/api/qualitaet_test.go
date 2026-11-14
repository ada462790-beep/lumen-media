// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"lumen/internal/store"
	"lumen/internal/stream"
)

// Die Qualität im Player („720p · 4 Mbit/s") schickt maxHeight und
// maxBitrate mit. Beides muss beim Plan ankommen — auch bei einer Datei,
// deren Bitrate nie gemessen wurde (dann aus Grösse und Laufzeit).
func TestQualitaetsStufeImPlan(t *testing.T) {
	h := newHarness(t)
	itemID, fileID := h.film("Nordlicht", 1) // 1080p, 8 Mbit/s

	// Eine zweite Fassung ohne gemessene Bitrate: 6,75 GB in 90 Minuten
	// sind 10 Mbit/s.
	alt := &store.MediaFile{ItemID: itemID, Path: filepath.Join(h.root, "Nordlicht.avi"),
		SizeBytes: 6_750_000_000, Container: "mkv", VideoCodec: "h264", Width: 1280, Height: 720,
		DurationMS: 5_400_000}
	if err := h.st.UpsertFile(t.Context(), alt); err != nil {
		t.Fatal(err)
	}

	plan := func(id, extra string) stream.Plan {
		t.Helper()
		resp, raw := h.do("GET", "/api/v1/stream/"+id+"/plan?codecs=h264&containers=mkv&audio=aac"+extra, "", h.tok)
		if resp.StatusCode != 200 {
			t.Fatalf("plan %s: %d %s", extra, resp.StatusCode, raw)
		}
		var out struct {
			Plan stream.Plan `json:"plan"`
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatal(err)
		}
		return out.Plan
	}

	if p := plan(fileID, "&maxHeight=2160"); !p.Direct {
		t.Fatalf("ohne Stufe sollte es direkt gehen: %+v", p)
	}
	if p := plan(fileID, "&maxHeight=720&maxBitrate=4000000"); p.Direct || p.Height != 720 || p.Bitrate != 4_000_000 {
		t.Fatalf("720p · 4 Mbit/s: %+v", p)
	}
	// Liegt die Datei schon unter der Stufe, bleibt sie, wie sie ist.
	if p := plan(fileID, "&maxHeight=2160&maxBitrate=12000000"); !p.Direct {
		t.Fatalf("Stufe über der Datei sollte direkt bleiben: %+v", p)
	}
	// Ohne gemessene Bitrate greift die Grenze trotzdem.
	if p := plan(alt.ID, "&maxHeight=2160&maxBitrate=4000000"); p.Direct || p.Bitrate != 4_000_000 {
		t.Fatalf("geschätzte Bitrate: %+v", p)
	}
	if p := plan(alt.ID, "&maxHeight=2160"); !p.Direct {
		t.Fatalf("Fassung ohne Stufe: %+v", p)
	}
}

func TestDateiBitrate(t *testing.T) {
	cases := []struct {
		f    store.MediaFile
		want int
	}{
		{store.MediaFile{Bitrate: 5_000_000, SizeBytes: 1, DurationMS: 1}, 5_000_000},
		{store.MediaFile{SizeBytes: 750_000_000, DurationMS: 600_000}, 10_000_000},
		{store.MediaFile{SizeBytes: 750_000_000}, 0},
	}
	for _, c := range cases {
		if got := dateiBitrate(&c.f); got != c.want {
			t.Errorf("%+v: %d, erwartet %d", c.f, got, c.want)
		}
	}
}
