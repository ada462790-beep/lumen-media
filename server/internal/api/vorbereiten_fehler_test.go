// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// vorbereitenVergessen: was sich der Arbeitsspeicher gemerkt hat, wie
// nach einem Neustart.
func vorbereitenVergessen(fileID string) {
	hg.mu.Lock()
	for k := range hg.fehl {
		if strings.HasPrefix(k, fileID+".") {
			delete(hg.fehl, k)
		}
	}
	hg.mu.Unlock()
}

// Eine kaputte Datei („Invalid data found") wird beim Vorbereiten einmal
// versucht und einmal gemeldet — auch über einen Neustart hinweg (vorher
// merkte sich Lumen das nur im Arbeitsspeicher). Ändert sich die Datei,
// kommt sie wieder dran. Als defekt vermerkte Dateien versucht der
// Hintergrund gar nicht erst.
func TestKaputteDateiWirdNurEinmalVorbereitet(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ohne ffmpeg")
	}
	h := newHarness(t)
	_, fileID := h.film("Another Earth", 1) // lauter Nullen: FFmpeg erkennt nichts
	meldungen := func() int {
		log, _ := h.st.RecentLog(t.Context(), "", 100)
		n := 0
		for _, l := range log {
			if strings.Contains(l.Message, "liess sich nicht vorbereiten") {
				n++
			}
		}
		return n
	}
	neustart := func() {
		vorbereitenVergessen(fileID)
	}
	h.api.dateiVorbereiten(t.Context(), fileID)
	if n := meldungen(); n != 1 {
		t.Fatalf("%d Meldungen nach dem ersten Versuch, erwartet 1", n)
	}
	neustart()
	h.api.dateiVorbereiten(t.Context(), fileID)
	if n := meldungen(); n != 1 {
		t.Fatalf("%d Meldungen nach dem Neustart, erwartet weiter 1", n)
	}
	// Datei geändert (neue Grösse): wieder versuchen.
	f, _ := h.st.File(t.Context(), fileID)
	f.SizeBytes++
	_ = h.st.UpsertFile(t.Context(), f)
	neustart()
	h.api.dateiVorbereiten(t.Context(), fileID)
	if n := meldungen(); n != 2 {
		t.Fatalf("%d Meldungen nach der Änderung, erwartet 2", n)
	}
	// Defekt vermerkt: kein Versuch, nicht in der Liste des Hintergrunds.
	f, _ = h.st.File(t.Context(), fileID)
	f.SizeBytes++
	f.Defekt = "kaputt"
	_ = h.st.UpsertFile(t.Context(), f)
	neustart()
	h.api.dateiVorbereiten(t.Context(), fileID)
	if n := meldungen(); n != 2 {
		t.Fatalf("defekte Datei wurde vorbereitet: %d Meldungen", n)
	}
	ids, _ := h.st.VorhandeneDateien(t.Context())
	for _, id := range ids {
		if id == fileID {
			t.Fatal("defekte Datei in der Liste des Hintergrunds")
		}
	}
}

// Nur eindeutige Formatfehler merkt Lumen sich dauerhaft; ein
// vorübergehender Fehler (Platte voll, FFmpeg beendet, Laufwerk weg)
// ruht 24 Stunden. Vorschaubilder und Untertitel merken sich ihr
// Scheitern getrennt.
func TestVorbereitenFehlerDauerhaftOderVoruebergehend(t *testing.T) {
	h := newHarness(t)
	_, fileID := h.film("Heat", 1)
	f, err := h.st.File(t.Context(), fileID)
	if err != nil {
		t.Fatal(err)
	}
	altern := func(art string) {
		t.Helper()
		vorbereitenVergessen(fileID)
		alt := time.Now().Add(-25 * time.Hour)
		if err := os.Chtimes(filepath.Join(h.api.vorbereitenFehlerOrdner(), fileID+"."+art), alt, alt); err != nil {
			t.Fatal(err)
		}
	}
	for meldung, dauerhaft := range map[string]bool{
		"ffmpeg: Invalid data found when processing input": true,
		"moov atom not found":                              true,
		"ffmpeg: No space left on device":                  false,
		"signal: killed":                                   false,
		"open /media/film.mkv: no such file or directory":  false,
	} {
		if istFormatFehler(meldung) != dauerhaft {
			t.Errorf("istFormatFehler(%q) = %v", meldung, !dauerhaft)
		}
	}

	h.api.vorbereitenGescheitertMerken(f, artTrickplay, false)
	if !h.api.vorbereitenGescheitert(f, artTrickplay) {
		t.Fatal("vorübergehender Fehler nicht gemerkt")
	}
	if h.api.vorbereitenGescheitert(f, artUntertitel) {
		t.Fatal("Vorschaubilder sperren die Untertitel")
	}
	altern(artTrickplay)
	if h.api.vorbereitenGescheitert(f, artTrickplay) {
		t.Fatal("vorübergehender Fehler nach 25 Stunden noch gesperrt")
	}

	h.api.vorbereitenGescheitertMerken(f, artUntertitel, true)
	altern(artUntertitel)
	if !h.api.vorbereitenGescheitert(f, artUntertitel) {
		t.Fatal("dauerhafter Fehler vergessen")
	}
	// Datei geändert: auch der dauerhafte Vermerk gilt nicht mehr.
	f.SizeBytes++
	if h.api.vorbereitenGescheitert(f, artUntertitel) {
		t.Fatal("geänderte Datei bleibt gesperrt")
	}
	f.SizeBytes--

	// Vermerk einer älteren Fassung (ohne Art): nur noch eine Pause.
	vorbereitenVergessen(fileID)
	_ = os.Remove(filepath.Join(h.api.vorbereitenFehlerOrdner(), fileID+"."+artTrickplay))
	altPfad := filepath.Join(h.api.vorbereitenFehlerOrdner(), fileID)
	if err := os.WriteFile(altPfad, []byte(dateiKennung(f)), 0o600); err != nil {
		t.Fatal(err)
	}
	if !h.api.vorbereitenGescheitert(f, artTrickplay) {
		t.Fatal("frischer alter Vermerk gilt nicht")
	}
	alt := time.Now().Add(-25 * time.Hour)
	_ = os.Chtimes(altPfad, alt, alt)
	if h.api.vorbereitenGescheitert(f, artTrickplay) {
		t.Fatal("alter Vermerk sperrt für immer")
	}
}
