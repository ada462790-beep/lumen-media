// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package sprache

import (
	"regexp"
	"strings"
	"testing"
)

func TestKatalogeLesbar(t *testing.T) {
	if err := Fehler(); err != nil {
		t.Fatal(err)
	}
	for _, s := range Sprachen {
		if _, ok := kataloge[s.ID]; !ok {
			t.Errorf("kein Katalog für %s", s.ID)
		}
	}
}

func TestNormalisieren(t *testing.T) {
	for ein, aus := range map[string]string{
		"de-CH": "de-CH", "de_ch": "de-CH", "DE-de": "de-DE", "de": "de-DE",
		"de-LI": "de-CH", "de-AT": "de-AT", "en": "en-US", "en-AU": "en-GB",
		"en-CA": "en-US", "fr-CH": "", "": "", "xx": "",
	} {
		if got := Normalisieren(ein); got != aus {
			t.Errorf("Normalisieren(%q) = %q, erwartet %q", ein, got, aus)
		}
	}
	if got := AusAcceptLanguage("fr-CH, fr;q=0.9, en-GB;q=0.8, de;q=0.7"); got != "en-GB" {
		t.Errorf("Accept-Language: %q", got)
	}
	if got := AusAcceptLanguage("fr-CH, it"); got != "" {
		t.Errorf("Accept-Language ohne Treffer: %q", got)
	}
}

func TestErbenUndMehrzahl(t *testing.T) {
	// de-CH: der Schlüssel ist der Text.
	if got := T("de-CH", "nicht gefunden"); got != "nicht gefunden" {
		t.Errorf("de-CH: %q", got)
	}
	// de-AT erbt von de-DE (ß), de-DE von de-CH.
	if got := T("de-AT", "das Bild liess sich nicht laden"); got != "das Bild ließ sich nicht laden" {
		t.Errorf("de-AT: %q", got)
	}
	if got := T("de-DE", "nicht gefunden"); got != "nicht gefunden" {
		t.Errorf("de-DE geerbt: %q", got)
	}
	// en-GB erbt von en-US.
	if got := T("en-GB", "nicht gefunden"); got != "Not found." {
		t.Errorf("en-GB geerbt: %q", got)
	}
	if got := T("en-GB", "Diese Altersfreigabe kennt Lumen nicht. Möglich sind etwa 0, 6, 12, 16, 18, FSK 12 oder PG-13."); !strings.Contains(got, "recognise") {
		t.Errorf("en-GB eigene Schreibweise: %q", got)
	}
	// Mehrzahl, auch im Schweizer Katalog.
	k := "zu viele Versuche — bitte {n} Sekunden warten"
	if got := T("de-CH", k, "n", 1); got != "zu viele Versuche — bitte 1 Sekunde warten" {
		t.Errorf("de-CH Einzahl: %q", got)
	}
	if got := T("de-DE", k, "n", 5); got != "zu viele Versuche — bitte 5 Sekunden warten" {
		t.Errorf("de-DE Mehrzahl: %q", got)
	}
	if got := T("en-US", k, "n", 1); got != "Too many attempts — please wait 1 second." {
		t.Errorf("en-US Einzahl: %q", got)
	}
	// Unbekannter Schlüssel: bleibt deutsch, Platzhalter trotzdem ersetzt.
	if got := T("en-US", "gibt es nicht {x}", "x", "7"); got != "gibt es nicht 7" {
		t.Errorf("unbekannt: %q", got)
	}
	if Mehrzahl("fr-CH", 0) != "one" || Mehrzahl("de-CH", 0) != "other" {
		t.Error("Mehrzahlregeln")
	}
}

// TestDeutschlandSchreibtEszett: Jeder Schlüssel mit einem Wort, das in
// Deutschland mit ß geschrieben wird, braucht einen Eintrag in de-DE —
// sonst sähe ein Profil in Deutschland „Strasse". Nur Wortstämme, bei
// denen das sicher gilt; „dass", „muss", „Fluss" bleiben.
func TestDeutschlandSchreibtEszett(t *testing.T) {
	stamm := regexp.MustCompile(`(?i)(gross|gröss|liess|heiss|weiss|strasse|schliess|fuss|ausser|äusser|aussen|bloss|gemäss|mässig|spass|grüss|gruss|stoss|fliess|süss|reiss|geniess)`)
	for _, id := range []string{"en-US"} {
		for _, k := range Schluessel(id) {
			if stamm.MatchString(k) {
				if _, ok := kataloge["de-DE"][k]; !ok {
					t.Errorf("de-DE fehlt für %q", k)
				}
			}
		}
	}
	for k, e := range kataloge["de-DE"] {
		if strings.Contains(e.text, "ss") && stamm.MatchString(e.text) {
			t.Errorf("de-DE noch mit ss: %q → %q", k, e.text)
		}
	}
}
