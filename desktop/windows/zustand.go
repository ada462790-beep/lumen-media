// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package main

import "path/filepath"

// Fensterzustand: wo das Fenster zuletzt stand und wie gross es war —
// in der Lage „normal" (nicht maximiert), dazu, ob es maximiert war.
type Fensterzustand struct {
	Links     int32 `json:"links"`
	Oben      int32 `json:"oben"`
	Rechts    int32 `json:"rechts"`
	Unten     int32 `json:"unten"`
	Maximiert bool  `json:"maximiert"`
}

// Gueltig: Eine Lage, die sich so nicht herstellen lässt (leer, verdreht,
// winzig), zählt nicht. Ob sie auf einem Bildschirm liegt, prüft das
// Fenster selbst (MonitorFromRect).
func (z Fensterzustand) Gueltig() bool {
	return z.Rechts-z.Links >= 300 && z.Unten-z.Oben >= 200 &&
		z.Rechts-z.Links <= 20000 && z.Unten-z.Oben <= 20000
}

func fensterPfad(ordner string) string { return filepath.Join(ordner, "fenster.json") }

func LadeFenster(ordner string) (Fensterzustand, bool) {
	var z Fensterzustand
	if err := jsonLesen(fensterPfad(ordner), &z); err != nil || !z.Gueltig() {
		return Fensterzustand{}, false
	}
	return z, true
}

func SpeichereFenster(ordner string, z Fensterzustand) error {
	if !z.Gueltig() {
		return nil
	}
	return jsonSchreiben(fensterPfad(ordner), z)
}
