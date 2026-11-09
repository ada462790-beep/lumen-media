// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"fmt"
	"testing"
)

func prefsRumpf(n, ab int, praefix string) string {
	m := map[string]string{}
	for i := 0; i < n; i++ {
		m[fmt.Sprintf("%s%d", praefix, ab+i)] = "1"
	}
	b, _ := json.Marshal(map[string]any{"prefs": m})
	return string(b)
}

// Ein Profil kann nicht beliebig viele Einstellungen anlegen; bekannte
// gehen immer, bestehende dürfen sich ändern, Server-Hinweise nie.
func TestPrefsBegrenzt(t *testing.T) {
	h := newHarness(t)
	if resp, raw := h.do("PATCH", "/api/v1/me", `{"prefs":{"sprache":"de-CH","autoplay":"0"}}`, h.tok); resp.StatusCode != 200 {
		t.Fatalf("bekannte: %d %s", resp.StatusCode, raw)
	}
	if resp, raw := h.do("PATCH", "/api/v1/me", prefsRumpf(prefsFremdeHoechstens, 0, "eigen."), h.tok); resp.StatusCode != 200 {
		t.Fatalf("bis zur Grenze: %d %s", resp.StatusCode, raw)
	}
	if resp, _ := h.do("PATCH", "/api/v1/me", prefsRumpf(1, 1000, "eigen."), h.tok); resp.StatusCode != 400 {
		t.Fatalf("über der Grenze: %d", resp.StatusCode)
	}
	// Bestehende ändern geht weiter.
	if resp, raw := h.do("PATCH", "/api/v1/me", `{"prefs":{"eigen.3":"2"}}`, h.tok); resp.StatusCode != 200 {
		t.Fatalf("bestehende ändern: %d %s", resp.StatusCode, raw)
	}
	for _, k := range []string{"hinweis.pinZurueckgesetzt", "a b", "<x>", ""} {
		b, _ := json.Marshal(map[string]any{"prefs": map[string]string{k: "1"}})
		if resp, _ := h.do("PATCH", "/api/v1/me", string(b), h.tok); resp.StatusCode != 400 {
			t.Errorf("Schlüssel %q angenommen: %d", k, resp.StatusCode)
		}
	}
	// Spuren je Serie: eigene, grössere Grenze.
	for ab := 0; ab < prefsSpurenHoechstens; ab += 50 {
		if resp, raw := h.do("PATCH", "/api/v1/me", prefsRumpf(50, ab, "spuren:s"), h.tok); resp.StatusCode != 200 {
			t.Fatalf("spuren %d: %d %s", ab, resp.StatusCode, raw)
		}
	}
	if resp, _ := h.do("PATCH", "/api/v1/me", prefsRumpf(1, 9999, "spuren:s"), h.tok); resp.StatusCode != 400 {
		t.Fatalf("spuren über der Grenze: %d", resp.StatusCode)
	}
}
