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
	"strings"
	"testing"
	"time"
)

// Umwandlungen beim Abspielen liegen unter einem Zufallsnamen, ohne
// Titelkennung, mit festem Datum — und verschwinden nach der Frist. Die
// alten Ordner mit Titelkennungen im Namen räumt Lumen beim Start weg.
func TestKurzlebigeUmwandlung(t *testing.T) {
	h := newHarness(t)
	s := h.svr()
	alt := s.mediathekOrdner("musik-umgewandelt")
	_ = os.MkdirAll(alt, 0o755)
	_ = os.WriteFile(filepath.Join(alt, "titel_123-1700000000.m4a"), []byte("x"), 0o644)

	aufrufe := 0
	rechnen := func(ziel string) error {
		aufrufe++
		return os.WriteFile(ziel, []byte("ton"), 0o600)
	}
	pfad, fertig, err := s.kurzlebigeDatei(t.Context(), "musik:titel_123:1:aac", "m4a", rechnen)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(alt); !os.IsNotExist(err) {
		t.Fatal("alter Ordner mit Titelkennungen ist noch da")
	}
	if strings.Contains(pfad, "titel_123") || filepath.Dir(pfad) != s.kurzlebigOrdner() {
		t.Fatalf("Name verrät den Titel oder liegt falsch: %s", pfad)
	}
	for _, p := range []string{pfad, filepath.Dir(pfad)} {
		if st, err := os.Stat(p); err != nil || st.ModTime().Year() != 2000 {
			t.Fatalf("%s trägt eine echte Zeit: %v %v", p, st.ModTime(), err)
		}
	}
	// Ein zweiter Abruf rechnet nicht noch einmal.
	pfad2, fertig2, _ := s.kurzlebigeDatei(t.Context(), "musik:titel_123:1:aac", "m4a", rechnen)
	if pfad2 != pfad || aufrufe != 1 {
		t.Fatalf("neu gerechnet: %d", aufrufe)
	}
	// Solange gesendet wird, bleibt sie — danach geht sie nach der Frist.
	s.kurz.aufraeumen(time.Now().Add(time.Hour), kurzlebigFrist)
	if _, err := os.Stat(pfad); err != nil {
		t.Fatal("während des Sendens gelöscht")
	}
	fertig()
	fertig2()
	s.kurz.aufraeumen(time.Now().Add(time.Hour), kurzlebigFrist)
	if _, err := os.Stat(pfad); !os.IsNotExist(err) {
		t.Fatal("nach der Frist noch da")
	}
}
