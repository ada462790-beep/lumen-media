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
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"lumen/internal/meta"
	"lumen/internal/scan"
	"lumen/internal/store"
	"lumen/internal/stream"
)

type harness struct {
	t    *testing.T
	srv  *httptest.Server
	st   *store.Store
	live *stream.Live
	tok  string
	lib  *store.Library
	// ohneBestaetigung: Rechte-Änderungen ohne den zweiten Code schicken.
	ohneBestaetigung bool
	root             string
	api              *Server
	// Wo der Server Dateien ablegt — für den Test, der nach dem Abspielen
	// nachsieht, was auf der Platte entstanden ist.
	daten, arbeit string
}

func (h *harness) svr() *Server { return h.api }

func newHarness(t *testing.T) *harness {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "l.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	live := stream.NewLive()
	ff := stream.NewFFmpeg("", "")
	arbeit := t.TempDir()
	tr, err := stream.NewTranscoder(ff, arbeit)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(tr.StopAll)
	sc := scan.New(st, nil) // ohne Ausmessen: der Test braucht kein FFmpeg
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	mt, err := meta.NewResolver(st, t.TempDir(), meta.Config{})
	if err != nil {
		t.Fatal(err)
	}
	daten := t.TempDir()
	api := New(st, sc, ff, tr, live, mt, daten, log, "test")

	srv := httptest.NewServer(api.Handler())
	t.Cleanup(srv.Close)

	h := &harness{t: t, srv: srv, st: st, live: live, root: t.TempDir(), api: api,
		daten: daten, arbeit: arbeit}
	h.setup()
	return h
}

func (h *harness) do(method, path, body, token string) (*http.Response, []byte) {
	h.t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, h.srv.URL+path, rdr)
	if err != nil {
		h.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	// Rechte-Änderungen verlangen den Code noch einmal (Issue #6); der
	// Besitzer der Tests gibt ihn mit, ausser ein Test will es anders.
	if token != "" && token == h.tok && !h.ohneBestaetigung {
		req.Header.Set("X-Lumen-Bestaetigung", "einsehrgutespasswort")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp, raw
}

func (h *harness) setup() {
	h.t.Helper()
	_, raw := h.do("POST", "/api/v1/setup",
		`{"name":"Alex","password":"einsehrgutespasswort","avatar":"av-a",
		  "deviceName":"Wohnzimmer","deviceKind":"desktop"}`, "")
	var out struct{ Token string }
	if err := json.Unmarshal(raw, &out); err != nil || out.Token == "" {
		h.t.Fatalf("Einrichtung fehlgeschlagen: %s", raw)
	}
	h.tok = out.Token
}

// film legt eine echte Datei an und hängt sie an einen Titel.
func (h *harness) film(name string, sizeMB int) (itemID, fileID string) {
	h.t.Helper()
	if h.lib == nil {
		lib, err := h.st.CreateLibrary(h.t.Context(), "Filme", "movie", h.root, "de")
		if err != nil {
			h.t.Fatal(err)
		}
		h.lib = lib
	}
	p := filepath.Join(h.root, name+".mkv")
	if err := os.WriteFile(p, make([]byte, sizeMB*1024*1024), 0o644); err != nil {
		h.t.Fatal(err)
	}
	it := &store.Item{LibraryID: h.lib.ID, Kind: "movie", Title: name, Year: 2019,
		MatchState: "matched", RuntimeMS: 5_400_000}
	if err := h.st.UpsertItem(h.t.Context(), it); err != nil {
		h.t.Fatal(err)
	}
	f := &store.MediaFile{ItemID: it.ID, Path: p, SizeBytes: int64(sizeMB) * 1024 * 1024,
		Container: "mkv", VideoCodec: "h264", Width: 1920, Height: 1080,
		Bitrate: 8_000_000, DurationMS: 5_400_000}
	if err := h.st.UpsertFile(h.t.Context(), f); err != nil {
		h.t.Fatal(err)
	}
	return it.ID, f.ID
}

// ------------------------------------------------- die eigentliche Zusage

// Während ein Film läuft, sieht der Besitzer Gerät und Last — und nichts,
// woraus sich der Titel ableiten liesse. Dieser Test spielt wirklich ab.
func TestOwnerSeesDeviceNotTitleWhilePlaying(t *testing.T) {
	h := newHarness(t)
	_, fileID := h.film("Ein sehr persoenlicher Film", 24)

	// Langsam lesen, wie ein Gerät, das nur holt, was es braucht.
	req, _ := http.NewRequest("GET",
		h.srv.URL+"/api/v1/stream/"+fileID+"?codecs=h264&containers=mkv&maxHeight=2160", nil)
	req.Header.Set("Authorization", "Bearer "+h.tok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Wiedergabe abgelehnt: %d", resp.StatusCode)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 32*1024)
		for {
			if _, err := resp.Body.Read(buf); err != nil {
				return
			}
			time.Sleep(3 * time.Millisecond)
		}
	}()

	// Jetzt fragt der Besitzer die Verwaltung.
	deadline := time.Now().Add(3 * time.Second)
	var body []byte
	for time.Now().Before(deadline) {
		_, body = h.do("GET", "/api/v1/admin/sessions", "", h.tok)
		if strings.Contains(string(body), `"sessions":[{`) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	var got struct {
		Sessions []stream.AdminSession `json:"sessions"`
		Load     stream.Load           `json:"load"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Sessions) != 1 {
		t.Fatalf("erwartet eine laufende Wiedergabe, bekommen %d:\n%s",
			len(got.Sessions), body)
	}

	s := got.Sessions[0]
	if s.Mode != "direct" || s.ID == "" {
		t.Errorf("der Besitzer sieht zu wenig, um den Server zu steuern: %+v", s)
	}
	for _, leak := range []string{"persoenlich", "Film", ".mkv", fileID, "Alex", "Wohnzimmer", "startedAt"} {
		if strings.Contains(string(body), leak) {
			t.Errorf("die Verwaltung verrät %q:\n%s", leak, body)
		}
	}
	resp.Body.Close()
	wg.Wait()
}

// Beendet der Besitzer eine Wiedergabe, bricht die Auslieferung wirklich ab.
func TestOwnerCanStopAPlayback(t *testing.T) {
	h := newHarness(t)
	_, fileID := h.film("Langer Film", 48)

	req, _ := http.NewRequest("GET",
		h.srv.URL+"/api/v1/stream/"+fileID+"?codecs=h264&containers=mkv&maxHeight=2160", nil)
	req.Header.Set("Authorization", "Bearer "+h.tok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	read := make(chan int64, 1)
	go func() {
		n, _ := io.Copy(slowSink{}, resp.Body)
		read <- n
	}()

	var sid string
	for i := 0; i < 150 && sid == ""; i++ {
		if list := h.live.ForOwner(); len(list) > 0 {
			sid = list[0].ID
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if sid == "" {
		t.Fatal("keine Sitzung registriert")
	}

	code, _ := h.do("DELETE", "/api/v1/admin/sessions/"+sid, "", h.tok)
	if code.StatusCode != http.StatusNoContent {
		t.Fatalf("Beenden gab %d", code.StatusCode)
	}

	select {
	case n := <-read:
		if n >= 48*1024*1024 {
			t.Errorf("die Datei lief trotz Beenden vollständig durch (%d Bytes)", n)
		}
	case <-time.After(5 * time.Second):
		t.Error("Beenden erreichte die laufende Wiedergabe nicht")
	}
	if len(h.live.ForOwner()) != 0 {
		t.Error("die beendete Sitzung steht noch in der Liste")
	}
}

type slowSink struct{}

func (slowSink) Write(p []byte) (int, error) {
	time.Sleep(2 * time.Millisecond)
	return len(p), nil
}

// Wer nicht Besitzer ist, findet die Verwaltung gar nicht erst.
func TestAdminIsInvisibleToOthers(t *testing.T) {
	h := newHarness(t)
	guest, err := h.st.CreateAccount(h.t.Context(), store.NewAccount{
		Name: "Jonas", LockKind: "pin", Secret: "4711"})
	if err != nil {
		t.Fatal(err)
	}
	tok, err := h.st.IssueToken(h.t.Context(), guest.ID, "iPhone", "phone")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		"/api/v1/admin/overview", "/api/v1/admin/sessions",
		"/api/v1/admin/log", "/api/v1/admin/accounts",
	} {
		resp, _ := h.do("GET", path, "", tok)
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s gab %d — erwartet 404, damit niemand erfährt, dass es das gibt",
				path, resp.StatusCode)
		}
	}
}

// Ein zweites Profil sieht den Fortschritt des ersten nicht — und umgekehrt
// der Besitzer den des zweiten auch nicht.
func TestProgressIsNotSharedBetweenProfiles(t *testing.T) {
	h := newHarness(t)
	itemID, _ := h.film("Nordlicht", 1)

	guest, _ := h.st.CreateAccount(h.t.Context(), store.NewAccount{
		Name: "Mara", LockKind: "pin", Secret: "4711"})
	_ = h.st.SetGrants(h.t.Context(), guest.ID, []string{h.lib.ID})
	gtok, _ := h.st.IssueToken(h.t.Context(), guest.ID, "iPad", "tablet")

	resp, _ := h.do("POST", "/api/v1/items/"+itemID+"/progress",
		`{"positionMs":1800000,"durationMs":5400000}`, gtok)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("Fortschritt speichern gab %d", resp.StatusCode)
	}

	// Mara sieht ihren Stand.
	_, raw := h.do("GET", "/api/v1/items?q=nordlicht", "", gtok)
	if !strings.Contains(string(raw), `"progress"`) {
		t.Fatalf("Mara sieht ihren eigenen Stand nicht: %s", raw)
	}
	// Der Besitzer sieht ihn nicht.
	_, raw = h.do("GET", "/api/v1/items?q=nordlicht", "", h.tok)
	if strings.Contains(string(raw), `"progress"`) || strings.Contains(string(raw), `"resumeMs"`) {
		t.Fatalf("der Besitzer sieht den Fortschritt eines anderen Profils: %s", raw)
	}
	// Auch nicht über die Startseite.
	_, raw = h.do("GET", "/api/v1/home", "", h.tok)
	if strings.Contains(string(raw), `"resume"`) {
		t.Fatalf("„Weiterschauen“ des Besitzers zeigt fremde Titel: %s", raw)
	}
}

// Was jemand nicht sehen darf, kommt gar nicht erst in der Antwort vor.
func TestLibraryGrantsAreEnforced(t *testing.T) {
	h := newHarness(t)
	itemID, fileID := h.film("Nur fuer Erwachsene", 1)

	guest, _ := h.st.CreateAccount(h.t.Context(), store.NewAccount{Name: "Lina", IsKid: true})
	ktok, _ := h.st.IssueToken(h.t.Context(), guest.ID, "iPad", "tablet")
	// keine Freigabe für die Bibliothek

	_, raw := h.do("GET", "/api/v1/items", "", ktok)
	if strings.Contains(string(raw), "Erwachsene") {
		t.Errorf("gesperrte Bibliothek taucht auf: %s", raw)
	}
	for _, p := range []string{"/api/v1/items/" + itemID, "/api/v1/stream/" + fileID} {
		resp, _ := h.do("GET", p, "", ktok)
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s gab %d statt 404", p, resp.StatusCode)
		}
	}
}

func TestUnlockWithWrongCode(t *testing.T) {
	h := newHarness(t)
	acc, _ := h.st.CreateAccount(h.t.Context(), store.NewAccount{
		Name: "Mara", LockKind: "pin", Secret: "4711"})

	resp, _ := h.do("POST", "/api/v1/unlock",
		fmt.Sprintf(`{"profileId":%q,"secret":"0000"}`, acc.ID), "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("falscher PIN gab %d", resp.StatusCode)
	}
	resp, raw := h.do("POST", "/api/v1/unlock",
		fmt.Sprintf(`{"profileId":%q,"secret":"4711"}`, acc.ID), "")
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(raw), `"token"`) {
		t.Errorf("richtiger PIN gab %d: %s", resp.StatusCode, raw)
	}
}

// Die Profilauswahl vor der Anmeldung darf nur Name und Bild zeigen.
func TestProfileListRevealsNothingPrivate(t *testing.T) {
	h := newHarness(t)
	_, _ = h.st.CreateAccount(h.t.Context(), store.NewAccount{
		Name: "Mara", LockKind: "pin", Secret: "4711"})

	_, raw := h.do("GET", "/api/v1/profiles", "", "")
	for _, leak := range []string{"secret", "hash", "salt", "createdAt", "expires"} {
		if strings.Contains(strings.ToLower(string(raw)), leak) {
			t.Errorf("Profilauswahl verrät %q: %s", leak, raw)
		}
	}
}
