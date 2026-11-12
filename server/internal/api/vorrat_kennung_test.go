// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"os"
	"path/filepath"
	"testing"

	"lumen/internal/store"
	"lumen/internal/stream"
)

// Eine Datei am selben Pfad ersetzt: Vorschaubilder und Untertitel der
// alten Fassung gelten nicht mehr.
func TestVorratGehoertZurFassung(t *testing.T) {
	ordner := t.TempDir()
	_ = os.WriteFile(filepath.Join(ordner, "info.json"), []byte(`{"intervall":10,"bilder":18,"blaetter":1}`), 0o644)
	alt := &store.MediaFile{ID: "f_1", SizeBytes: 100, MTime: 5, DurationMS: 170_000}
	neu := &store.MediaFile{ID: "f_1", SizeBytes: 200, MTime: 9, DurationMS: 410_000}

	// Ältere Ordner ohne Vermerk: die Laufzeit muss passen.
	if !trickplayFertig(ordner, alt) {
		t.Fatal("alter Ordner zur passenden Laufzeit gilt nicht")
	}
	if trickplayFertig(ordner, neu) {
		t.Fatal("alter Ordner gilt für die neue, längere Datei")
	}
	// Mit Vermerk: genau diese Fassung.
	_ = os.WriteFile(filepath.Join(ordner, stream.TrickplayQuelle), []byte(dateiKennung(alt)), 0o644)
	if !trickplayFertig(ordner, alt) {
		t.Fatal("Vermerk passt, gilt aber nicht")
	}
	gleichLang := *alt
	gleichLang.SizeBytes = 101
	if trickplayFertig(ordner, &gleichLang) {
		t.Fatal("ersetzte Datei gleicher Länge erkannt nicht")
	}

	s := &Server{dataDir: t.TempDir()}
	_ = os.MkdirAll(s.untertitelVorrat(), 0o700)
	_ = os.WriteFile(s.vorratPfad("f_1", 2), []byte("WEBVTT"), 0o600)
	_ = os.WriteFile(s.vorratVermerk("f_1"), []byte(dateiKennung(alt)), 0o600)
	s.vorratPruefen(alt)
	if _, err := os.Stat(s.vorratPfad("f_1", 2)); err != nil {
		t.Fatal("passender Vorrat weggeworfen")
	}
	s.vorratPruefen(neu)
	if _, err := os.Stat(s.vorratPfad("f_1", 2)); err == nil {
		t.Fatal("Vorrat der alten Fassung bleibt")
	}
}
