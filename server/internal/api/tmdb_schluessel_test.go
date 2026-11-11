// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Ein falscher TMDb-Schlüssel wird beim Speichern abgelehnt, ein richtiger
// angenommen — und ist TMDb nicht erreichbar, wird trotzdem gespeichert.
func TestTMDbSchluesselWirdGeprueft(t *testing.T) {
	tmdb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer eyJrichtig" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer tmdb.Close()
	t.Setenv("LUMEN_TMDB_URL", tmdb.URL)
	h := newHarness(t)

	resp, raw := h.do("PUT", "/api/v1/admin/metadata", `{"tmdbKey":"abc123kurz"}`, h.tok)
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(raw), "Read Access Token") {
		t.Fatalf("falscher Schlüssel: %d %s", resp.StatusCode, raw)
	}
	if v := h.st.Setting(t.Context(), keyTMDb, ""); v != "" {
		t.Errorf("der falsche Schlüssel wurde trotzdem gespeichert: %q", v)
	}
	if resp, raw := h.do("PUT", "/api/v1/admin/metadata", `{"tmdbKey":"eyJrichtig"}`, h.tok); resp.StatusCode != 200 {
		t.Fatalf("richtiger Schlüssel: %d %s", resp.StatusCode, raw)
	}
	tmdb.Close()
	if resp, raw := h.do("PUT", "/api/v1/admin/metadata", `{"tmdbKey":"eyJanders"}`, h.tok); resp.StatusCode != 200 {
		t.Fatalf("TMDb nicht erreichbar: %d %s", resp.StatusCode, raw)
	}
}
