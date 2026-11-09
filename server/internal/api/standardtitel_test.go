// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"testing"

	"lumen/internal/store"
)

// TestStandardTitel: „Staffel 2"/„Folge 3" sind Nummern in Worten und
// erscheinen in der Sprache des Profils; echte Namen bleiben.
func TestStandardTitel(t *testing.T) {
	faelle := []struct {
		art, titel, spr, will string
	}{
		{"season", "Staffel 2", "en-US", "Season 2"},
		{"season", "Season 2", "de-CH", "Staffel 2"},
		{"episode", "Folge 3", "en-GB", "Episode 3"},
		{"episode", "Episode 3", "de-DE", "Folge 3"},
		{"episode", "Folge 3–4", "en-US", "Episodes 3–4"},
		{"episode", "Pilot", "en-US", "Pilot"},
		{"episode", "Folge 3 und mehr", "en-US", "Folge 3 und mehr"},
		{"movie", "Folge 3", "en-US", "Folge 3"},
	}
	for _, f := range faelle {
		it := &store.Item{Kind: f.art, Title: f.titel}
		if got := standardTitel(f.spr, it); got != f.will {
			t.Errorf("%s %q in %s: %q, will %q", f.art, f.titel, f.spr, got, f.will)
		}
	}
}
