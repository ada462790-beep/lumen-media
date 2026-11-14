// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"os/exec"
	"strings"
	"testing"
)

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
		hg.mu.Lock()
		delete(hg.fehl, fileID)
		hg.mu.Unlock()
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
