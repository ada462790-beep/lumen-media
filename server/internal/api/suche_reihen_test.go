// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSuchPasst(t *testing.T) {
	for _, f := range []struct {
		name, eingabe string
		passt         bool
	}{
		{"Harry Potter Filmreihe", "harry potter", true},
		{"Harry Potter Filmreihe", "potter harry", true},
		{"Harry Potter Filmreihe", "potter", true},
		{"Ärger im Paradies – Reihe", "aerger", true},
		{"Ärger im Paradies – Reihe", "ärger", true},
		{"Matrix Collection", "matrix", true},
		{"Matrix Collection", "harry", false},
		{"Matrix Collection", "", false},
	} {
		if g := suchPasst(f.name, f.eingabe); g != f.passt {
			t.Errorf("suchPasst(%q, %q) = %v, erwartet %v", f.name, f.eingabe, g, f.passt)
		}
	}
}

// Die Suche findet Filmreihen beim Namen — nicht nur die einzelnen Filme.
func TestSucheFindetFilmreihen(t *testing.T) {
	h := newHarness(t)
	h.film("Alien", 1)
	h.film("Alien 3", 1)
	h.film("Nordlicht", 1)
	h.svr().ReihenBilden(t.Context())

	_, raw := h.do("GET", "/api/v1/search?q=alien", "", h.tok)
	var antwort struct {
		Reihen []struct {
			Name   string
			Anzahl int
		}
	}
	if err := json.Unmarshal(raw, &antwort); err != nil {
		t.Fatal(err, string(raw))
	}
	if len(antwort.Reihen) != 1 || antwort.Reihen[0].Name != "Alien" || antwort.Reihen[0].Anzahl != 2 {
		t.Fatalf("Reihen in der Suche: %s", raw)
	}
	_, raw = h.do("GET", "/api/v1/search?q=nordlicht", "", h.tok)
	if strings.Contains(string(raw), `"reihen":[{`) {
		t.Fatalf("Reihe ohne passenden Namen gefunden: %s", raw)
	}
}
