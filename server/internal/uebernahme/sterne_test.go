// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package uebernahme

import "testing"

// Jellyfin setzt beim „Gefällt mir" Rating 10 — das sind vier Sterne wie
// bei Emby, nicht fünf.
func TestSterne(t *testing.T) {
	ja, nein := true, false
	zahl := func(f float64) *float64 { return &f }
	faelle := []struct {
		name   string
		likes  *bool
		rating *float64
		want   int
	}{
		{"Jellyfin Gefällt mir", &ja, zahl(10), 4},
		{"Emby Daumen hoch", &ja, nil, 4},
		{"Gefällt mir nicht", &nein, zahl(0), 0},
		{"eigene Note 10 ohne Daumen", nil, zahl(10), 5},
		{"eigene Note 7", nil, zahl(7), 4},
		{"Note 6 mit Daumen", &ja, zahl(6), 3},
		{"nichts", nil, nil, 0},
	}
	for _, f := range faelle {
		if got := sterne(f.likes, f.rating); got != f.want {
			t.Errorf("%s: %d Sterne, erwartet %d", f.name, got, f.want)
		}
	}
}
