// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

import "testing"

// Breitformat-Filme sind niedriger als ihre Stufe; 720p ist HD.
func TestQualitaetNachBreiteUndHoehe(t *testing.T) {
	for _, f := range []struct {
		w, h int
		want string
	}{
		{0, 0, ""},
		{720, 576, "SD"},
		{1024, 576, "SD"},
		{1280, 720, "HD"},
		{1280, 536, "HD"},
		{1920, 1080, "HD"},
		{1920, 800, "HD"},
		{3840, 2160, "4K"},
		{3840, 1600, "4K"},
	} {
		if got := Qualitaet(f.w, f.h); got != f.want {
			t.Errorf("Qualitaet(%d, %d) = %q, erwartet %q", f.w, f.h, got, f.want)
		}
	}
}
