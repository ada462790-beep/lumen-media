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
	"strings"
	"testing"

	"lumen/internal/stream"
)

// Herunterladen: nur mit Recht, mit Anhang-Kopfzeile, Bereichsanfragen
// gehen, und die Grenze zählt Dateien, nicht Anfragen.
func TestHerunterladen(t *testing.T) {
	h := newHarness(t)
	itemID, fileID := h.film("Nordlicht", 1)
	_, zweite := h.film("Südlicht", 1)

	_, raw := h.do("GET", "/api/v1/items/"+itemID+"/download/info", "", h.tok)
	var info struct {
		Erlaubt bool `json:"erlaubt"`
		Dateien []struct {
			FileID    string `json:"fileId"`
			Dateiname string `json:"dateiname"`
			Groesse   int64  `json:"groesse"`
			Hoehe     int    `json:"hoehe"`
		} `json:"dateien"`
	}
	if err := json.Unmarshal(raw, &info); err != nil || !info.Erlaubt || len(info.Dateien) != 1 ||
		info.Dateien[0].FileID != fileID || info.Dateien[0].Dateiname != "Nordlicht.mkv" || info.Dateien[0].Hoehe != 1080 {
		t.Fatalf("info: %s", raw)
	}

	// Ein Gast mit Grenze 1.
	b := h.gast("Gast", false, h.lib.ID)
	accs, _ := h.st.Accounts(h.t.Context())
	for _, a := range accs {
		if a.Name == "Gast" {
			lim, _ := h.st.Limits(h.t.Context(), a.ID)
			lim.AllowDownload, lim.MaxDownloads = true, 1
			if err := h.st.SetLimits(h.t.Context(), a.ID, lim); err != nil {
				t.Fatal(err)
			}
		}
	}
	resp, body := h.do("GET", "/api/v1/stream/"+fileID+"/download", "", b)
	if resp.StatusCode != 200 || len(body) != 1<<20 ||
		!strings.HasPrefix(resp.Header.Get("Content-Disposition"), "attachment;") {
		t.Fatalf("download: %d %d %q", resp.StatusCode, len(body), resp.Header.Get("Content-Disposition"))
	}
	// Fortsetzen derselben Datei zählt nicht noch einmal.
	req, _ := http.NewRequest("GET", h.srv.URL+"/api/v1/stream/"+fileID+"/download", nil)
	req.Header.Set("Authorization", "Bearer "+b)
	req.Header.Set("Range", "bytes=1000-")
	r2, err := http.DefaultClient.Do(req)
	if err != nil || r2.StatusCode != http.StatusPartialContent {
		t.Fatalf("fortsetzen: %v %v", err, r2.StatusCode)
	}
	r2.Body.Close()
	// Eine zweite Datei: über der Grenze.
	resp, body = h.do("GET", "/api/v1/stream/"+zweite+"/download", "", b)
	if resp.StatusCode != http.StatusTooManyRequests || !strings.Contains(string(body), `"zu_viele_downloads"`) {
		t.Fatalf("grenze: %d %s", resp.StatusCode, body)
	}

	// Ohne Recht.
	kind := h.gast("Ohne", false, h.lib.ID)
	accs, _ = h.st.Accounts(h.t.Context())
	for _, a := range accs {
		if a.Name == "Ohne" {
			lim, _ := h.st.Limits(h.t.Context(), a.ID)
			lim.AllowDownload = false
			_ = h.st.SetLimits(h.t.Context(), a.ID, lim)
		}
	}
	resp, body = h.do("GET", "/api/v1/stream/"+fileID+"/download", "", kind)
	if resp.StatusCode != http.StatusForbidden || !strings.Contains(string(body), `"kein_download"`) {
		t.Fatalf("ohne recht: %d %s", resp.StatusCode, body)
	}
	_, raw = h.do("GET", "/api/v1/items/"+itemID+"/download/info", "", kind)
	if !strings.Contains(string(raw), `"erlaubt":false`) || !strings.Contains(string(raw), `"dateien":[]`) {
		t.Fatalf("info ohne recht: %s", raw)
	}
}

// Nach einer langen Pause sagt der Server, warum die Wiedergabe weg ist —
// damit ein Gerät weiss, ob es neu ansetzen darf.
func TestWiedergabeVorbeiMitGrund(t *testing.T) {
	h := newHarness(t)
	_, fileID := h.film("Nordlicht", 1)
	for _, f := range []struct{ grund, code string }{
		{stream.GrundAbgelaufen, "wiedergabe_abgelaufen"},
		{stream.GrundBeendet, "wiedergabe_beendet"},
		{stream.GrundErsetzt, "wiedergabe_ersetzt"},
	} {
		s := h.live.Start(&stream.Session{AccountID: h.ownerID(), FileID: fileID, Mode: "transcode"})
		h.live.EndMit(s.ID, f.grund)
		resp, body := h.do("GET", "/api/v1/stream/"+fileID+"/hls/"+s.ID+"/seg00003.ts", "", h.tok)
		if resp.StatusCode != http.StatusGone || !strings.Contains(string(body), `"code":"`+f.code+`"`) ||
			resp.Header.Get("X-Lumen-Grund") != f.code {
			t.Errorf("%s: %d %s", f.grund, resp.StatusCode, body)
		}
	}
	// Unbekannte Kennung: gilt als abgelaufen.
	resp, body := h.do("GET", "/api/v1/stream/"+fileID+"/hls/unbekannt/media.m3u8", "", h.tok)
	if resp.StatusCode != http.StatusGone || !strings.Contains(string(body), "wiedergabe_abgelaufen") {
		t.Errorf("unbekannt: %d %s", resp.StatusCode, body)
	}
}

// Spulen in einer Direktwiedergabe (viele Bereichsanfragen) ist keine
// zweite Wiedergabe — auch mit der Grenze 1.
func TestDirektwiedergabeSpulenZaehltNichtDoppelt(t *testing.T) {
	h := newHarness(t)
	_, fileID := h.film("Nordlicht", 2)
	accs, _ := h.st.Accounts(h.t.Context())
	for _, a := range accs {
		if a.IsOwner {
			lim, _ := h.st.Limits(h.t.Context(), a.ID)
			lim.MaxStreams = 1
			_ = h.st.SetLimits(h.t.Context(), a.ID, lim)
		}
	}
	for _, bereich := range []string{"bytes=0-1000", "bytes=1000000-", "bytes=500-600"} {
		req, _ := http.NewRequest("GET", h.srv.URL+"/api/v1/stream/"+fileID+"?codecs=h264&containers=mkv&maxHeight=2160", nil)
		req.Header.Set("Authorization", "Bearer "+h.tok)
		req.Header.Set("Range", bereich)
		resp, err := http.DefaultClient.Do(req)
		if err != nil || resp.StatusCode != http.StatusPartialContent {
			t.Fatalf("%s: %v %v", bereich, err, resp.StatusCode)
		}
		resp.Body.Close()
	}
	if n := h.live.Load().Total; n != 1 {
		t.Fatalf("%d Wiedergaben statt einer", n)
	}
}

// Vorschaubilder: ausgeschaltet 404, sonst angestossen (202).
func TestTrickplaySchnittstelle(t *testing.T) {
	h := newHarness(t)
	_, fileID := h.film("Nordlicht", 1)
	resp, body := h.do("GET", "/api/v1/stream/"+fileID+"/trickplay", "", h.tok)
	if h.api.ff.Available() {
		if resp.StatusCode != http.StatusAccepted || !strings.Contains(string(body), "wird_erstellt") {
			t.Fatalf("an: %d %s", resp.StatusCode, body)
		}
	}
	_ = h.st.SetSetting(h.t.Context(), einstTrickplay, "0")
	resp, _ = h.do("GET", "/api/v1/stream/"+fileID+"/trickplay", "", h.tok)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("aus: %d", resp.StatusCode)
	}
	resp, _ = h.do("GET", "/api/v1/stream/"+fileID+"/trickplay/../../lumen.db", "", h.tok)
	if resp.StatusCode == http.StatusOK {
		t.Fatal("Pfad aus dem Ordner heraus")
	}
}

// Die Einstellungen ums Umwandeln: nur der Besitzer, nur gültige Werte.
func TestUmwandlungEinstellungen(t *testing.T) {
	h := newHarness(t)
	h.film("Nordlicht", 1)
	b := h.gast("Gast", false, h.lib.ID)
	if resp, _ := h.do("GET", "/api/v1/admin/umwandlung", "", b); resp.StatusCode == http.StatusOK {
		t.Fatal("Gast sieht die Einstellungen")
	}
	if resp, _ := h.do("PUT", "/api/v1/admin/umwandlung", `{"hardware":"turbo"}`, h.tok); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("ungültiges Verfahren: %d", resp.StatusCode)
	}
	resp, raw := h.do("PUT", "/api/v1/admin/umwandlung", `{"hardware":"aus","trickplay":false}`, h.tok)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(raw), `"trickplay":false`) {
		t.Fatalf("setzen: %d %s", resp.StatusCode, raw)
	}
	if h.st.Setting(h.t.Context(), einstHardware, "") != "aus" {
		t.Fatal("nicht gespeichert")
	}
}

// Der Browser-Player sieht nur „error" oder ein zu frühes Ende. Unter
// /browser/grund erfährt er, was war — je Player-Kennung, je Konto.
func TestBrowserGrund(t *testing.T) {
	h := newHarness(t)
	_, fileID := h.film("Nordlicht", 1)
	frag := func(lauf, tok string) string {
		resp, body := h.do("GET", "/api/v1/stream/"+fileID+"/browser/grund?lauf="+lauf, "", tok)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("grund: %d %s", resp.StatusCode, body)
		}
		var r struct{ Code, Error string }
		_ = json.Unmarshal(body, &r)
		return r.Code + "|" + r.Error
	}
	if got := frag("nie-gesehen", h.tok); got != "unbekannt|" {
		t.Fatalf("unbekannt: %s", got)
	}
	// Eine Anfrage, die scheitert (ohne Platz oder ohne FFmpeg): der Text
	// steht danach zur Kennung bereit.
	accs, _ := h.st.Accounts(h.t.Context())
	for _, a := range accs {
		if a.IsOwner {
			lim, _ := h.st.Limits(h.t.Context(), a.ID)
			lim.MaxStreams = 0
			_ = h.st.SetLimits(h.t.Context(), a.ID, lim)
		}
	}
	resp, _ := h.do("GET", "/api/v1/stream/"+fileID+"/browser.mp4?lauf=p1", "", h.tok)
	if resp.StatusCode < 400 {
		t.Fatalf("browser.mp4 sollte scheitern: %d", resp.StatusCode)
	}
	if got := frag("p1", h.tok); !strings.HasPrefix(got, "fehler|") || len(got) < 10 {
		t.Fatalf("fehler: %s", got)
	}
	// Laufende, ersetzte und beendete Wiedergaben.
	s := h.live.Start(&stream.Session{AccountID: h.ownerID(), FileID: fileID, Mode: "transcode"})
	h.api.laeufe.setzen(h.ownerID(), "p2", laufEintrag{sid: s.ID})
	if got := frag("p2", h.tok); got != "laeuft|" {
		t.Fatalf("läuft: %s", got)
	}
	h.live.EndMit(s.ID, stream.GrundErsetzt)
	if got := frag("p2", h.tok); got != "wiedergabe_ersetzt|" {
		t.Fatalf("ersetzt: %s", got)
	}
	// Ein anderes Konto erfährt über dieselbe Kennung nichts.
	gast := h.gast("Gast", false, h.lib.ID)
	if got := frag("p2", gast); got != "unbekannt|" {
		t.Fatalf("fremd: %s", got)
	}
}

// Ohne „Herunterladen" bekommt die Direktwiedergabe eine Drossel; mit
// nicht. Der Download-Weg selbst bleibt gesperrt.
func TestDirektstromGedrosseltOhneDownload(t *testing.T) {
	h := newHarness(t)
	_, fileID := h.film("Nordlicht", 2)
	ohne := h.gast("Mara", false, h.lib.ID)
	mit := h.gast("Lou", false, h.lib.ID)
	mara, _, _ := h.st.LookupToken(t.Context(), ohne)
	lim, _ := h.st.Limits(t.Context(), mara.ID)
	lim.AllowDownload = false
	lim.AllowUnrated = true
	_ = h.st.SetLimits(t.Context(), mara.ID, lim)
	lou, _, _ := h.st.LookupToken(t.Context(), mit)
	lim2, _ := h.st.Limits(t.Context(), lou.ID)
	lim2.AllowUnrated = true
	_ = h.st.SetLimits(t.Context(), lou.ID, lim2)

	for _, tok := range []string{ohne, mit} {
		req, _ := http.NewRequest("GET", h.srv.URL+"/api/v1/stream/"+fileID+"?codecs=h264&containers=mkv&maxHeight=2160", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("Range", "bytes=0-1000")
		resp, err := http.DefaultClient.Do(req)
		if err != nil || resp.StatusCode != http.StatusPartialContent {
			t.Fatalf("Direktstrom: %v %v", err, resp.StatusCode)
		}
		resp.Body.Close()
	}
	if id := h.live.Finden(mara.ID, "iPad", fileID, "direct"); id == "" || h.live.DrosselVon(id) == nil {
		t.Fatal("ohne Herunterladen keine Drossel")
	}
	if id := h.live.Finden(lou.ID, "iPad", fileID, "direct"); id == "" || h.live.DrosselVon(id) != nil {
		t.Fatal("mit Herunterladen gedrosselt")
	}
	if resp, _ := h.do("GET", "/api/v1/stream/"+fileID+"/download", "", ohne); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("Download ohne Erlaubnis: %d", resp.StatusCode)
	}
}
