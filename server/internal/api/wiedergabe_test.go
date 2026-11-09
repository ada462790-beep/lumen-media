// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lumen/internal/scan"
	"lumen/internal/stream"
)

// Eine erfundene oder fremde Sitzungskennung in der Adresse einer
// Umwandlung führt ins Leere: kein FFmpeg, kein Ordner, kein Segment —
// auch nicht mit „../" in der Kennung.
func TestHLSNurFuerDieEigeneSitzung(t *testing.T) {
	h := newHarness(t)
	_, fileID := h.film("Nordlicht", 1)

	for _, sid := range []string{"erfunden", "..%2F..%2Fetc", "%2E%2E"} {
		for _, pfad := range []string{"/media.m3u8", "/seg00000.ts"} {
			resp, _ := h.do("GET", "/api/v1/stream/"+fileID+"/hls/"+sid+pfad, "", h.tok)
			if resp.StatusCode != http.StatusGone && resp.StatusCode != http.StatusNotFound {
				t.Errorf("%s%s: %d, erwartet 410/404", sid, pfad, resp.StatusCode)
			}
		}
	}

	// Eine echte Sitzung von A ist für B nicht zu gebrauchen.
	lib := h.lib.ID
	b := h.gast("Gast", false, lib)
	s := h.live.Start(&stream.Session{AccountID: h.ownerID(), FileID: fileID, Mode: "transcode"})
	resp, _ := h.do("GET", "/api/v1/stream/"+fileID+"/hls/"+s.ID+"/media.m3u8", "", b)
	if resp.StatusCode != http.StatusGone {
		t.Errorf("fremde Sitzung: %d, erwartet 410", resp.StatusCode)
	}
	resp, _ = h.do("DELETE", "/api/v1/stream/"+fileID+"/hls/"+s.ID, "", b)
	if h.live.Load().Total != 1 {
		t.Error("B konnte die Wiedergabe von A beenden")
	}
	_ = resp
}

// Unter „Ich → Geräte" sieht jede Person nur ihre eigenen Wiedergaben und
// kann nur diese beenden.
func TestEigeneWiedergabenUeberDieSchnittstelle(t *testing.T) {
	h := newHarness(t)
	_, fileID := h.film("Nordlicht", 1)
	b := h.gast("Gast", false, h.lib.ID)
	h.live.Start(&stream.Session{AccountID: h.ownerID(), DeviceName: "Wohnzimmer", FileID: fileID, Mode: "direct"})

	_, raw := h.do("GET", "/api/v1/me/playbacks", "", b)
	var fremd []stream.EigeneWiedergabe
	_ = json.Unmarshal(raw, &fremd)
	if len(fremd) != 0 {
		t.Fatalf("B sieht fremde Wiedergaben: %s", raw)
	}
	_, raw = h.do("GET", "/api/v1/me/playbacks", "", h.tok)
	var eigene []stream.EigeneWiedergabe
	_ = json.Unmarshal(raw, &eigene)
	if len(eigene) != 1 || eigene[0].DeviceName != "Wohnzimmer" {
		t.Fatalf("eigene Liste falsch: %s", raw)
	}
	if resp, _ := h.do("DELETE", "/api/v1/me/playbacks/"+eigene[0].ID, "", b); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("B beendet fremde Wiedergabe: %d", resp.StatusCode)
	}
	if resp, _ := h.do("DELETE", "/api/v1/me/playbacks/"+eigene[0].ID, "", h.tok); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("eigene Wiedergabe beenden: %d", resp.StatusCode)
	}
	if h.live.Load().Total != 0 {
		t.Fatal("Wiedergabe läuft noch")
	}
}

// Nach dem Abspielen — direkt, mit Untertiteln — liegt auf der Platte
// nichts Neues, dessen Name auf den Film, die Datei oder den Titel zeigt.
// Ein Ordner „umwandlung/<Kennung>" oder eine Datei „<Datei>-3.vtt" wäre
// ein Verlauf, den jeder mit Zugriff aufs NAS lesen kann.
func TestAbspielenHinterlaesstKeineVerraeterischenDateien(t *testing.T) {
	h := newHarness(t)
	itemID, fileID := h.film("Peinlicher Film", 2)
	srt := filepath.Join(h.root, "Peinlicher Film.de.srt")
	_ = os.WriteFile(srt, []byte("1\n00:00:01,000 --> 00:00:02,000\nHallo\n"), 0o644)
	_ = h.st.SpurenSetzen(t.Context(), fileID,
		scan.SpurenFuerStore(scan.SidecarSubtitles(filepath.Join(h.root, "Peinlicher Film.mkv"))), nil)

	vorher := dateinamen(t, h.daten, h.arbeit)

	resp, _ := h.do("GET", "/api/v1/stream/"+fileID+"/subtitles/1000.vtt", "", h.tok)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Untertitel: %d", resp.StatusCode)
	}
	resp, _ = h.do("GET", "/api/v1/stream/"+fileID+"?codecs=h264&containers=mkv&maxHeight=2160", "", h.tok)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		t.Fatalf("Abspielen: %d", resp.StatusCode)
	}

	for name := range dateinamen(t, h.daten, h.arbeit) {
		if vorher[name] {
			continue
		}
		low := strings.ToLower(name)
		for _, verraet := range []string{strings.ToLower(fileID), strings.ToLower(itemID), "peinlich", ".vtt"} {
			if strings.Contains(low, verraet) {
				t.Errorf("nach dem Abspielen liegt %q auf der Platte (verrät %q)", name, verraet)
			}
		}
	}
}

func dateinamen(t *testing.T, ordner ...string) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for _, o := range ordner {
		_ = filepath.WalkDir(o, func(p string, d fs.DirEntry, err error) error {
			if err == nil {
				rel, _ := filepath.Rel(o, p)
				out[rel] = true
			}
			return nil
		})
	}
	return out
}

// Favoriten: eigenes Herz, eigene Liste in der Reihenfolge des Setzens;
// ein anderes Profil sieht davon nichts. Dazu die Buchstabenleiste.
func TestFavoritenUndBuchstaben(t *testing.T) {
	h := newHarness(t)
	a, _ := h.film("Alien", 1)
	b, _ := h.film("Blade Runner", 1)
	_, _ = h.film("3 Engel für Charlie", 1)
	for _, id := range []string{b, a} {
		if resp, _ := h.do("PUT", "/api/v1/favorites/"+id, "", h.tok); resp.StatusCode != http.StatusNoContent {
			t.Fatalf("Herz setzen: %d", resp.StatusCode)
		}
	}
	_, raw := h.do("GET", "/api/v1/items?liste=favoriten", "", h.tok)
	var favs []struct {
		ID       string `json:"id"`
		Favorite bool   `json:"favorite"`
	}
	_ = json.Unmarshal(raw, &favs)
	if len(favs) != 2 || favs[0].ID != a || !favs[0].Favorite {
		t.Fatalf("Favoriten: %s", raw)
	}
	gast := h.gast("Gast", false, h.lib.ID)
	_, raw = h.do("GET", "/api/v1/items?liste=favoriten", "", gast)
	if strings.Contains(string(raw), a) {
		t.Fatalf("Gast sieht fremde Favoriten: %s", raw)
	}
	_, raw = h.do("GET", "/api/v1/items/letters?kind=movie", "", h.tok)
	var bst map[string]int
	_ = json.Unmarshal(raw, &bst)
	if bst["A"] != 1 || bst["B"] != 1 || bst["#"] != 1 {
		t.Fatalf("Buchstaben: %s", raw)
	}
	_, raw = h.do("GET", "/api/v1/items?kind=movie&buchstabe=%23", "", h.tok)
	if !strings.Contains(string(raw), "3 Engel") || strings.Contains(string(raw), "Alien") {
		t.Fatalf("Buchstabe #: %s", raw)
	}
}
