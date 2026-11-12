// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

import "testing"

func TestNFC(t *testing.T) {
	for _, f := range []struct{ ein, aus string }{
		{"Schöne", "Schöne"},
		{"Amélie", "Amélie"},
		{"Grüezi Zäri", "Grüezi Zäri"},
		{"Çà et là", "Çà et là"},
		{"Dvořák", "Dvořák"},
		{"Schöne", "Schöne"},
		{"plain", "plain"},
		{"̈allein", "̈allein"},
		{"日本゙", "日本゙"},
	} {
		if got := NFC(f.ein); got != f.aus {
			t.Errorf("NFC(%q) = %q, erwartet %q", f.ein, got, f.aus)
		}
	}
}
