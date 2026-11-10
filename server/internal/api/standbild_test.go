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
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"lumen/internal/store"
)

// Ohne Poster soll ein Bild aus dem Film selbst kommen — sonst sieht
// ohne TMDb-Schlüssel jede Seite aus wie eine Reihe grauer Kästen.
func TestStandbildAlsErsatzFuersPoster(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ohne FFmpeg kein Standbild")
	}
	h := newHarness(t)

	// Eine echte, kurze Videodatei.
	pfad := filepath.Join(h.root, "Testbild (2020).mp4")
	cmd := exec.Command("ffmpeg", "-nostdin", "-loglevel", "error",
		"-f", "lavfi", "-i", "testsrc=duration=3:size=320x180:rate=10",
		"-c:v", "libx264", "-preset", "ultrafast", "-y", pfad)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("Testvideo nicht erzeugbar: %v %s", err, out)
	}
	lib, err := h.st.CreateLibrary(t.Context(), "Filme", "movie", h.root, "de")
	if err != nil {
		t.Fatal(err)
	}
	it := &store.Item{LibraryID: lib.ID, Kind: "movie", Title: "Testbild", Year: 2020,
		MatchState: "matched"}
	if err := h.st.UpsertItem(t.Context(), it); err != nil {
		t.Fatal(err)
	}
	f := &store.MediaFile{ItemID: it.ID, Path: pfad, Container: "mp4",
		VideoCodec: "h264", Width: 320, Height: 180, DurationMS: 3000}
	if err := h.st.UpsertFile(t.Context(), f); err != nil {
		t.Fatal(err)
	}

	// Die Liste nennt das Standbild, weil es kein Poster gibt.
	_, raw := h.do("GET", "/api/v1/items/"+it.ID, "", h.tok)
	if !strings.Contains(string(raw), `"frame":"/api/v1/art/`+it.ID+`/frame"`) {
		t.Fatalf("kein Standbild angeboten: %s", raw)
	}

	resp, body := h.do("GET", "/api/v1/art/"+it.ID+"/frame", "", h.tok)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Standbild: %d %s", resp.StatusCode, body)
	}
	if resp.Header.Get("Content-Type") != "image/jpeg" || len(body) < 500 {
		t.Fatalf("kein brauchbares Bild: %s, %d Bytes", resp.Header.Get("Content-Type"), len(body))
	}

	// Beim zweiten Mal kommt es aus dem Speicher, ohne neu zu rechnen.
	if err := os.Remove(pfad); err != nil {
		t.Fatal(err)
	}
	resp, _ = h.do("GET", "/api/v1/art/"+it.ID+"/frame", "", h.tok)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("aufbewahrtes Standbild fehlt: %d", resp.StatusCode)
	}

	// Ohne Anmeldung gibt es nichts.
	resp, _ = h.do("GET", "/api/v1/art/"+it.ID+"/frame", "", "")
	if resp.StatusCode == http.StatusOK {
		t.Fatal("Standbild ohne Anmeldung ausgeliefert")
	}
}

// Wo sich kein Bild machen lässt — etwa weil die Datei kaputt ist —,
// kommt ein sauberes 404, und die App zeigt weiter den Titel.
func TestStandbildAusKaputterDatei(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ohne FFmpeg kein Standbild")
	}
	h := newHarness(t)
	itemID, _ := h.film("Kaputt", 1) // nur Nullbytes, kein Video
	resp, raw := h.do("GET", "/api/v1/art/"+itemID+"/frame", "", h.tok)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("erwartet 404, bekommen %d %s", resp.StatusCode, raw)
	}
}

// Folgen: Das Standbild von TMDb liegt als „Poster" da. Dann bietet Lumen
// kein Bild aus der Datei an — vorher bekam jede Folge eins, und die Web-App
// zeigte es statt des richtigen.
func TestFolgeMitTMDbBildOhneStandbildAusDerDatei(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ohne FFmpeg gibt es nie ein Standbild")
	}
	h := newHarness(t)
	ctx := t.Context()
	lib, err := h.st.CreateLibrary(ctx, "Serien", "series", h.root, "de")
	if err != nil {
		t.Fatal(err)
	}
	serie := &store.Item{LibraryID: lib.ID, Kind: "series", Title: "Nordlicht", MatchState: "matched"}
	if err := h.st.UpsertItem(ctx, serie); err != nil {
		t.Fatal(err)
	}
	staffel := &store.Item{LibraryID: lib.ID, Kind: "season", ParentID: serie.ID, Title: "Staffel 1", SeasonNo: 1, MatchState: "matched"}
	if err := h.st.UpsertItem(ctx, staffel); err != nil {
		t.Fatal(err)
	}
	mitBild := &store.Item{LibraryID: lib.ID, Kind: "episode", ParentID: staffel.ID, Title: "Eins", SeasonNo: 1, EpisodeNo: 1, MatchState: "matched"}
	ohneBild := &store.Item{LibraryID: lib.ID, Kind: "episode", ParentID: staffel.ID, Title: "Zwei", SeasonNo: 1, EpisodeNo: 2, MatchState: "matched"}
	for _, it := range []*store.Item{mitBild, ohneBild} {
		if err := h.st.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
	}
	if err := h.st.SetArtwork(ctx, mitBild.ID, "poster", mitBild.ID+"-poster.jpg", "https://image.tmdb.org/t/p/original/x.jpg"); err != nil {
		t.Fatal(err)
	}
	_, raw := h.do("GET", "/api/v1/items/"+staffel.ID+"/children", "", h.tok)
	var folgen []struct {
		ID     string `json:"id"`
		Poster string `json:"poster"`
		Frame  string `json:"frame"`
	}
	if err := json.Unmarshal(raw, &folgen); err != nil {
		t.Fatalf("%v: %s", err, raw)
	}
	gesehen := 0
	for _, f := range folgen {
		switch f.ID {
		case mitBild.ID:
			gesehen++
			if f.Poster == "" || f.Frame != "" {
				t.Errorf("Folge mit TMDb-Bild: poster=%q frame=%q", f.Poster, f.Frame)
			}
		case ohneBild.ID:
			gesehen++
			if f.Frame == "" {
				t.Errorf("Folge ohne Bild bekommt kein Standbild")
			}
		}
	}
	if gesehen != 2 {
		t.Fatalf("Folgen fehlen: %s", raw)
	}
}
