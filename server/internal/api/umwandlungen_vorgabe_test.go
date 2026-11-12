// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import "testing"

func TestUmwandlungenVorgabe(t *testing.T) {
	for _, f := range []struct {
		kerne int
		hw    bool
		will  int
	}{
		{1, false, 1}, {2, false, 1}, {4, false, 2}, {7, false, 2}, {8, false, 2}, {16, false, 4},
		{2, true, 2}, {4, true, 4},
	} {
		if got := umwandlungenVorgabe(f.kerne, f.hw); got != f.will {
			t.Errorf("%d Kerne, Hardware %v: %d, erwartet %d", f.kerne, f.hw, got, f.will)
		}
	}
}
