// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"net/http/httptest"
	"testing"
)

// Nur Ströme mit Medienschlüssel bekommen CORS — sonst bleibt es aus.
func TestCORSNurFuerCastStroeme(t *testing.T) {
	for _, f := range []struct {
		weg  string
		cors bool
	}{
		{"/api/v1/stream/f1/hls/master.m3u8?key=abc", true},
		{"/api/v1/stream/f1/hls/master.m3u8", false},
		{"/api/v1/home?key=abc", false},
	} {
		r := httptest.NewRequest("OPTIONS", f.weg, nil)
		if castStrom(r) != f.cors {
			t.Errorf("%s: cors=%v", f.weg, !f.cors)
		}
	}
}

func TestSegmenteMitSchluessel(t *testing.T) {
	liste := "#EXTM3U\n#EXT-X-MAP:URI=\"init.mp4\"\n#EXTINF:6.0,\nseg00000.m4s\n#EXT-X-ENDLIST\n"
	got := mitSchluessel(liste, "a b")
	want := "#EXTM3U\n#EXT-X-MAP:URI=\"init.mp4?key=a+b\"\n#EXTINF:6.0,\nseg00000.m4s?key=a+b\n#EXT-X-ENDLIST\n"
	if got != want {
		t.Errorf("%q", got)
	}
	if mitSchluessel(liste, "") != liste {
		t.Error("ohne Schlüssel verändert")
	}
}
