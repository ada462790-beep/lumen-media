// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	"lumen/internal/store"
)

// bestaetigtDo: wie do, mit eigenem Bestätigungscode.
func (h *harness) bestaetigtDo(method, path, body, tok, code string) (*http.Response, []byte) {
	h.t.Helper()
	req, _ := http.NewRequest(method, h.srv.URL+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("X-Lumen-Bestaetigung", code)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp, raw
}

// Eine Mitverwalterin nur für „Kinder" verwaltet nur Konten innerhalb
// ihres Bereichs — und dort nur ihre Bibliotheken. Früher entzog sie
// Erwachsenen die Filme, machte sie zu Kinderprofilen mit FSK 0 und
// setzte deren PIN zurück, um mit allen Bibliotheken zu schauen.
func TestMitverwalterinBleibtInIhremBereich(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()
	h.film("Horror Nacht", 1) // legt h.lib = „Filme" an
	kinderLib, err := h.st.CreateLibrary(ctx, "Kinder", "movie", t.TempDir(), "de")
	if err != nil {
		t.Fatal(err)
	}
	ctok, code := h.mitverwalter("Carla")
	carla := h.idVon(ctok)
	g, err := h.st.GruppeAnlegen(ctx, store.Gruppe{Name: "Kinderverwaltung", Verwaltung: true,
		Libraries: []string{kinderLib.ID}, Limits: store.VerwaltungsLimits()})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.st.GruppeZuweisen(ctx, carla, g.ID); err != nil {
		t.Fatal(err)
	}
	anna := h.idVon(h.gast("Anna", false, h.lib.ID, kinderLib.ID))
	kira := h.idVon(h.gast("Kira", true, kinderLib.ID))

	grenzen := `{"maxAge":18,"allowUnrated":true,"maxStreams":2,"allowTranscode":true,"allowDownload":true,"maxDownloads":20,"remoteQuality":"1080p"}`
	// Anna hat auch „Filme": nicht Carlas Bereich.
	for _, rumpf := range []string{
		`{"limits":` + grenzen + `,"libraries":["` + kinderLib.ID + `"]}`,
		`{"limits":{"maxAge":0,"maxStreams":2,"allowTranscode":true,"maxDownloads":20,"remoteQuality":"720p"},"kinderprofil":true}`,
	} {
		if resp, raw := h.bestaetigtDo("PUT", "/api/v1/admin/accounts/"+anna+"/limits", rumpf, ctok, code); resp.StatusCode != 403 {
			t.Fatalf("Carla ändert Anna: %d %s", resp.StatusCode, raw)
		}
	}
	if libs, _ := h.st.GrantedLibraries(ctx, anna); len(libs) != 2 {
		t.Fatalf("Annas Bibliotheken verändert: %v", libs)
	}
	if acc, _ := h.st.Account(ctx, anna); acc.IsKid {
		t.Fatal("Anna wurde zum Kinderprofil")
	}
	// PIN zurücksetzen bei Anna: nein.
	if resp, raw := h.bestaetigtDo("PUT", "/api/v1/admin/accounts/"+anna+"/sperre", `{"art":"pin","neu":"8888"}`, ctok, code); resp.StatusCode != 403 {
		t.Fatalf("Carla setzt Annas PIN zurück: %d %s", resp.StatusCode, raw)
	}
	if h.st.CheckSecret(ctx, anna, "4711") != nil {
		t.Fatal("Annas PIN wurde geändert")
	}

	// Kira liegt in ihrem Bereich: Streams ändern geht, Kinderschutz
	// aufheben nicht, eine fremde Bibliothek dazugeben auch nicht.
	kid := `{"maxAge":6,"allowUnrated":false,"maxStreams":1,"allowTranscode":true,"allowDownload":false,"maxDownloads":0,"remoteQuality":"720p"}`
	if resp, raw := h.bestaetigtDo("PUT", "/api/v1/admin/accounts/"+kira+"/limits", `{"limits":`+kid+`}`, ctok, code); resp.StatusCode != 204 {
		t.Fatalf("Carla ändert Kira: %d %s", resp.StatusCode, raw)
	}
	if resp, raw := h.bestaetigtDo("PUT", "/api/v1/admin/accounts/"+kira+"/limits", `{"limits":`+grenzen+`,"kinderprofil":false}`, ctok, code); resp.StatusCode != 403 {
		t.Fatalf("Carla hebt Kiras Kinderschutz auf: %d %s", resp.StatusCode, raw)
	}
	if resp, raw := h.bestaetigtDo("PUT", "/api/v1/admin/accounts/"+kira+"/limits", `{"limits":`+kid+`,"libraries":["`+h.lib.ID+`","`+kinderLib.ID+`"]}`, ctok, code); resp.StatusCode != 403 {
		t.Fatalf("Carla gibt Kira „Filme“: %d %s", resp.StatusCode, raw)
	}
	if acc, _ := h.st.Account(ctx, kira); !acc.IsKid {
		t.Fatal("Kira ist kein Kinderprofil mehr")
	}
	if libs, _ := h.st.GrantedLibraries(ctx, kira); !slices.Equal(libs, []string{kinderLib.ID}) {
		t.Fatalf("Kiras Bibliotheken: %v", libs)
	}
	// Die PIN eines Kontos in ihrem Bereich darf sie zurücksetzen.
	if resp, raw := h.bestaetigtDo("PUT", "/api/v1/admin/accounts/"+kira+"/sperre", `{"art":"pin"}`, ctok, code); resp.StatusCode != 200 {
		t.Fatalf("Carla setzt Kiras PIN zurück: %d %s", resp.StatusCode, raw)
	}
}
