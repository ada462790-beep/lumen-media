// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package stream

import (
	"strings"
	"testing"
)

func TestBereinigtNimmtPfadeHeraus(t *testing.T) {
	for _, in := range []string{
		"/medien/Filme/Peinlicher Film (2009)/Peinlicher.Film.mkv: Invalid data found when processing input",
		"open /medien/Filme/Peinlicher.Film.mkv: permission denied",
		`Error opening input file '/medien/x/Peinlicher Film.mkv'.`,
		`C:\Filme\Peinlicher.Film.mkv: No such file or directory`,
	} {
		out := Bereinigt(in)
		low := strings.ToLower(out)
		if strings.Contains(low, "peinlich") || strings.Contains(low, "film") || strings.Contains(out, "2009") {
			t.Errorf("%q → %q verrät die Datei", in, out)
		}
	}
	if got := Bereinigt("Conversion failed!"); got != "Conversion failed!" {
		t.Errorf("harmlose Meldung verändert: %q", got)
	}
}
