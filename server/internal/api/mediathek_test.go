// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"lumen/internal/store"
)

func TestMedienWegErlaubtMediathek(t *testing.T) {
	for p, want := range map[string]bool{
		"/api/v1/medien/musik/titel/x": true, "/api/v1/medien/fotos/x/bild": true,
		"/api/v1/musik/alben": false, "/api/v1/fotos/liste": false,
	} {
		r := httptest.NewRequest("GET", p, nil)
		if medienWeg(r) != want {
			t.Errorf("%s: %v", p, !want)
		}
	}
}

func TestMusikMime(t *testing.T) {
	if MusikMime("wmav2", "wma") != "" || MusikMime("mp3", "mp3") != "audio/mpeg" || !strings.Contains(MusikMime("alac", "m4a"), "alac") {
		t.Error("Mime falsch")
	}
}

// Musik und Fotos folgen den Freigaben pro Bibliothek; der Hörstand eines
// Gastes taucht weder beim Besitzer noch in der Verwaltung auf.
func TestMediathekFreigabenUndHoerstand(t *testing.T) {
	h := newHarness(t)
	ctx := h.t.Context()
	lib, _ := h.st.CreateLibrary(ctx, "Musik", "music", h.root, "de")
	mt := &store.MusikTitel{LibraryID: lib.ID, Pfad: h.root + "/a.mp3", Titel: "Geheimlied", Interpret: "Band", Album: "Platte", Codec: "mp3"}
	if _, err := h.st.MusikTitelSpeichern(ctx, mt); err != nil {
		t.Fatal(err)
	}
	al := store.MusikAlbum{ID: store.AlbumID(lib.ID, ".", "Platte"), Name: "Platte", Interpret: "Band", InterpretID: store.InterpretID("Band"), Anzahl: 1}
	if err := h.st.MusikAlbenSetzen(ctx, lib.ID, []store.MusikAlbum{al}, map[string]string{mt.ID: al.ID}); err != nil {
		t.Fatal(err)
	}

	gast, _ := h.st.CreateAccount(ctx, store.NewAccount{Name: "Lina"})
	gtok, _ := h.st.IssueToken(ctx, gast.ID, "iPad", "tablet")
	_, raw := h.do("GET", "/api/v1/mediathek", "", gtok)
	if !strings.Contains(string(raw), `"musik":false`) {
		t.Fatalf("ohne Freigabe Musik angeboten: %s", raw)
	}
	if resp, _ := h.do("GET", "/api/v1/musik/alben/"+al.ID, "", gtok); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("Album ohne Freigabe: %d", resp.StatusCode)
	}
	if resp, _ := h.do("GET", "/api/v1/medien/musik/titel/"+mt.ID, "", gtok); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("Lied ohne Freigabe: %d", resp.StatusCode)
	}

	_ = h.st.SetGrants(ctx, gast.ID, []string{lib.ID})
	_, raw = h.do("GET", "/api/v1/musik/alben/"+al.ID, "", gtok)
	if !strings.Contains(string(raw), "Geheimlied") {
		t.Fatalf("mit Freigabe nicht sichtbar: %s", raw)
	}
	if resp, raw := h.do("POST", "/api/v1/musik/titel/"+mt.ID+"/gehoert", "", gtok); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("gehört: %d %s", resp.StatusCode, raw)
	}
	_, raw = h.do("GET", "/api/v1/musik/start", "", gtok)
	var start struct {
		Zuletzt []json.RawMessage `json:"zuletztGehoert"`
	}
	_ = json.Unmarshal(raw, &start)
	if len(start.Zuletzt) != 1 {
		t.Fatalf("eigenes „Zuletzt gehört“ fehlt: %s", raw)
	}
	// Der Besitzer sieht davon nichts — weder bei sich noch in der Verwaltung.
	_, raw = h.do("GET", "/api/v1/musik/start", "", h.tok)
	_ = json.Unmarshal(raw, &start)
	if len(start.Zuletzt) != 0 {
		t.Fatalf("Besitzer sieht fremden Hörstand: %s", raw)
	}
	for _, p := range []string{"/api/v1/admin/log?level=all", "/api/v1/admin/sessions", "/api/v1/admin/overview", "/api/v1/admin/accounts"} {
		_, raw := h.do("GET", p, "", h.tok)
		if strings.Contains(string(raw), "Geheimlied") {
			t.Errorf("%s verrät, was gehört wurde: %s", p, raw)
		}
	}
}
