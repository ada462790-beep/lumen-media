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
)

// Die Reste von Musik und Fotos verschwinden genau einmal nach dem Umbau
// auf Stand 10 — und nur, was Lumen selbst angelegt hat.
func TestMediathekResteNurEinmalUndNurEigenes(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()
	mediathek := filepath.Join(h.daten, "mediathek")
	kurz := filepath.Join(orElse(h.api.umwandlungDir, h.daten), "kurzlebig")
	schreiben := func(p string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cover := filepath.Join(mediathek, "cover", "ab", "c.jpg")
	fremd := filepath.Join(mediathek, "meins", "notiz.txt")
	umgewandelt := filepath.Join(kurz, "0123456789abcdef0123456789abcdef.m4a")
	fremdKurz := filepath.Join(kurz, "urlaub.mp4")
	for _, p := range []string{cover, fremd, umgewandelt, fremdKurz} {
		schreiben(p)
	}

	// Ohne den Umbau: nichts geschieht.
	h.api.MediathekResteWeg()
	for _, p := range []string{cover, fremd, umgewandelt, fremdKurz} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("ohne Umbau gelöscht: %s", p)
		}
	}

	// Nach dem Umbau: Lumens eigene Dateien weg, fremde bleiben.
	_ = h.st.SetSetting(ctx, store.MediathekResteEinst, "1")
	h.api.MediathekResteWeg()
	for _, p := range []string{cover, umgewandelt} {
		if _, err := os.Stat(p); err == nil {
			t.Errorf("nicht weggeräumt: %s", p)
		}
	}
	for _, p := range []string{fremd, fremdKurz} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("fremde Datei gelöscht: %s", p)
		}
	}
	// Ein zweites Mal geschieht nichts mehr.
	schreiben(cover)
	h.api.MediathekResteWeg()
	if _, err := os.Stat(cover); err != nil {
		t.Fatal("beim zweiten Start wieder gelöscht")
	}
	// Ganz leere Ordner verschwinden mit.
	_ = os.RemoveAll(filepath.Join(mediathek, "meins"))
	_ = os.Remove(fremdKurz)
	_ = h.st.SetSetting(ctx, store.MediathekResteEinst, "1")
	h.api.MediathekResteWeg()
	for _, p := range []string{mediathek, kurz} {
		if _, err := os.Stat(p); err == nil {
			t.Errorf("leerer Ordner bleibt: %s", p)
		}
	}
}
