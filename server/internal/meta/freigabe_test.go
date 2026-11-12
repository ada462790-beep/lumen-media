// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package meta

import (
	"testing"

	"lumen/internal/lokal"
)

// Die Schreibweisen, mit denen Emby, Jellyfin, Kodi, tinyMediaManager und
// TMDb Altersfreigaben ablegen. Ein Profil „bis 16" darf keinen Film mit
// „DE-18" sehen — und ein unbekanntes Kennzeichen ist „ungeprüft", nie 0.
func TestAgeYearsSchreibweisen(t *testing.T) {
	faelle := []struct {
		ein   string
		jahre int // -1: ungeprüft (nil)
	}{
		// Emby/Jellyfin
		{"DE-18", 18}, {"DE-16", 16}, {"DE-12", 12}, {"DE-6", 6}, {"DE-0", 0},
		{"de-16", 16}, {"DE-FSK 16", 16}, {"DE-FSK16", 16},
		{"CH-16", 16}, {"CH-7", 7}, {"AT-14", 14}, {"AT-10", 10},
		{"GB-15", 15}, {"GB-12A", 12}, {"GB-U", 0}, {"GB-18", 18}, {"UK-15", 15},
		{"US-PG-13", 12}, {"US-R", 16}, {"US-TV-MA", 18}, {"US-G", 0},
		{"de/16", 16}, {"DE/18", 18},
		// Kodi und tinyMediaManager
		{"de:FSK 16", 16}, {"DE:16", 16}, {"US:R / DE:16", 16}, {"US:PG-13 / GB:15", 12},
		// FSK von Hand
		{"FSK 12", 12}, {"FSK-16", 16}, {"FSK16", 16}, {"FSK 0", 0}, {"fsk ab 6", 6},
		{"ab 12", 12}, {"ab 12 Jahren", 12}, {"Freigegeben ab 16 Jahren", 16},
		{"16+", 16}, {"18+", 18}, {"o.Al.", 0}, {"KJ", 18},
		// USA
		{"PG-13", 12}, {"PG", 6}, {"R", 16}, {"Rated R", 16}, {"NC-17", 18}, {"G", 0},
		{"TV-MA", 18}, {"TV-14", 14}, {"TV-Y7", 7}, {"TV-PG", 6}, {"TV-G", 0},
		// TMDb und Zahlen
		{"0", 0}, {"6", 6}, {"12", 12}, {"14", 14}, {"16", 16}, {"18", 18}, {"U", 0}, {"12A", 12},
		// Unbekannt: ungeprüft, nie 0
		{"", -1}, {"NR", -1}, {"Not Rated", -1}, {"Unrated", -1}, {"unbekannt", -1},
		{"DE-", -1}, {"DE-XYZ", -1}, {"99", -1}, {"Approved", -1}, {"CH", -1},
	}
	for _, f := range faelle {
		got := AgeYears(f.ein)
		switch {
		case f.jahre < 0 && got != nil:
			t.Errorf("%q → %d, erwartet ungeprüft", f.ein, *got)
		case f.jahre >= 0 && (got == nil || *got != f.jahre):
			t.Errorf("%q → %v, erwartet %d", f.ein, got, f.jahre)
		}
	}
}

// Der ganze Weg: <mpaa> aus einer Emby-NFO bis zur Zahl für die Grenze.
func TestFreigabeAusNFO(t *testing.T) {
	for ein, want := range map[string]int{
		"DE-18": 18, "CH-16": 16, "AT-14": 14, "GB-15": 15, "de/16": 16,
		"US:R / DE:16": 16, "de:FSK 12": 12, "Rated PG-13": 12, "FSK 16": 16,
	} {
		n, err := lokal.NFOAus([]byte("<movie><title>X</title><mpaa>" + ein + "</mpaa></movie>"))
		if err != nil {
			t.Fatal(err)
		}
		got := AgeYears(n.Freigabe)
		if got == nil || *got != want {
			t.Errorf("<mpaa>%s</mpaa> → %q → %v, erwartet %d", ein, n.Freigabe, got, want)
		}
	}
}
