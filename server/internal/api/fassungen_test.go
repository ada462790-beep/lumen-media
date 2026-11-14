// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lumen/internal/store"
)

// Zwei Fassungen: Der Besitzer wählt eine, und sie steht vorn. Eine
// falsch zugeordnete Datei lässt sich als eigener Film abtrennen.
func TestFassungWaehlenUndAbtrennen(t *testing.T) {
	h := newHarness(t)
	itemID, gross := h.film("American Psycho", 2)
	klein := &store.MediaFile{ItemID: itemID, Path: filepath.Join(h.root, "American Psycho.avi"),
		SizeBytes: 1024, Container: "avi", Height: 400}
	_ = os.WriteFile(klein.Path, []byte("x"), 0o644)
	if err := h.st.UpsertFile(t.Context(), klein); err != nil {
		t.Fatal(err)
	}
	if resp, _ := h.do("PUT", "/api/v1/admin/items/"+itemID+"/fassung", `{"fileId":"`+klein.ID+`"}`, h.tok); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("Fassung wählen: %d", resp.StatusCode)
	}
	dateien, _ := h.st.FilesFor(t.Context(), itemID)
	if dateien[0].ID != klein.ID || !dateien[0].Bevorzugt {
		t.Fatalf("gewählte Fassung steht nicht vorn: %+v", dateien)
	}
	if resp, _ := h.do("PUT", "/api/v1/admin/items/"+itemID+"/fassung", `{"fileId":"fremd"}`, h.tok); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("fremde Datei als Fassung: %d", resp.StatusCode)
	}
	if resp, _ := h.do("POST", "/api/v1/admin/files/"+gross+"/abtrennen", "", h.tok); resp.StatusCode != http.StatusOK {
		t.Fatalf("Abtrennen: %d", resp.StatusCode)
	}
	if d, _ := h.st.FilesFor(t.Context(), itemID); len(d) != 1 {
		t.Fatalf("nach dem Abtrennen %d Dateien am Titel", len(d))
	}
}

// Drei Teile werden eine MKV-Datei, die Teile wandern beiseite.
func TestTeileZusammenfuehren(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ohne ffmpeg")
	}
	h := newHarness(t)
	h.film("Platzhalter", 1) // legt die Bibliothek an
	it := &store.Item{LibraryID: h.lib.ID, Kind: "movie", Title: "Apocalypse Now", Year: 1979, MatchState: "matched"}
	_ = h.st.UpsertItem(t.Context(), it)
	for i, b := range []string{"a", "b", "c"} {
		p := filepath.Join(h.root, "Apocalypse Now 1979"+b+".mkv")
		if out, err := exec.Command("ffmpeg", "-loglevel", "error", "-f", "lavfi", "-i", "testsrc=duration=1:size=160x90:rate=10",
			"-c:v", "libx264", "-y", p).CombinedOutput(); err != nil {
			t.Fatalf("%v %s", err, out)
		}
		_ = h.st.UpsertFile(t.Context(), &store.MediaFile{ItemID: it.ID, Path: p, PartNo: i + 1, SizeBytes: 1})
	}
	resp, _ := h.do("POST", "/api/v1/admin/items/"+it.ID+"/teile-zusammenfuehren", "", h.tok)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("Zusammenführen: %d", resp.StatusCode)
	}
	ziel := filepath.Join(h.root, "Apocalypse Now (1979).mkv")
	for i := 0; i < 100; i++ {
		if d, _ := h.st.FilesFor(t.Context(), it.ID); len(d) == 1 && d[0].Path == ziel {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	d, _ := h.st.FilesFor(t.Context(), it.ID)
	if len(d) != 1 || d[0].Path != ziel {
		t.Fatalf("nach dem Zusammenführen: %+v", d)
	}
	if _, err := os.Stat(filepath.Join(h.root, ".lumen-teile", "Apocalypse Now 1979b.mkv")); err != nil {
		t.Fatalf("Teil nicht beiseitegelegt: %v", err)
	}
}

// Teile in eigenen Ordnern („Doktor Schiwago (1965)/CD1/Doktor
// Schiwago.avi", „…/CD2/Doktor Schiwago.avi"): Der Film kommt in den
// Filmordner, und beide Teile bleiben beiseite erhalten. Vorher landete
// der Film in „CD1", und der zweite Teil ersetzte in „.lumen-teile" den
// ersten (gleicher Name).
func TestTeileAusCDOrdnernZusammenfuehren(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ohne ffmpeg")
	}
	h := newHarness(t)
	h.film("Platzhalter", 1)
	it := &store.Item{LibraryID: h.lib.ID, Kind: "movie", Title: "Doktor Schiwago", Year: 1965, MatchState: "matched"}
	_ = h.st.UpsertItem(t.Context(), it)
	filmOrdner := filepath.Join(h.root, "Doktor Schiwago (1965)")
	for i, cd := range []string{"CD1", "CD2"} {
		p := filepath.Join(filmOrdner, cd, "Doktor Schiwago.avi")
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if out, err := exec.Command("ffmpeg", "-loglevel", "error", "-f", "lavfi", "-i", "testsrc=duration=1:size=160x90:rate=10",
			"-c:v", "mpeg4", "-y", p).CombinedOutput(); err != nil {
			t.Fatalf("%v %s", err, out)
		}
		_ = h.st.UpsertFile(t.Context(), &store.MediaFile{ItemID: it.ID, Path: p, PartNo: i + 1, SizeBytes: 1})
	}
	resp, _ := h.do("POST", "/api/v1/admin/items/"+it.ID+"/teile-zusammenfuehren", "", h.tok)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("Zusammenführen: %d", resp.StatusCode)
	}
	ziel := filepath.Join(filmOrdner, "Doktor Schiwago (1965).mkv")
	bisWahr(t, "zusammengeführte Datei", func() bool {
		d, _ := h.st.FilesFor(t.Context(), it.ID)
		return len(d) == 1 && d[0].Path == ziel
	})
	for _, n := range []string{"CD1 - Doktor Schiwago.avi", "CD2 - Doktor Schiwago.avi"} {
		if _, err := os.Stat(filepath.Join(filmOrdner, ".lumen-teile", n)); err != nil {
			t.Errorf("Teil %s nicht beiseitegelegt: %v", n, err)
		}
	}
}

// Scheitert das Zusammenführen, steht der Grund im Protokoll — ohne Pfad.
func TestZusammenfuehrenNenntDenGrund(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ohne ffmpeg")
	}
	h := newHarness(t)
	h.film("Platzhalter", 1)
	it := &store.Item{LibraryID: h.lib.ID, Kind: "movie", Title: "Geheim", Year: 2001, MatchState: "matched"}
	_ = h.st.UpsertItem(t.Context(), it)
	for i, b := range []string{"cd1", "cd2"} {
		p := filepath.Join(h.root, "Geheim (2001) "+b+".avi")
		_ = os.WriteFile(p, []byte("kein video"), 0o644) // FFmpeg erkennt nichts darin
		_ = h.st.UpsertFile(t.Context(), &store.MediaFile{ItemID: it.ID, Path: p, PartNo: i + 1, SizeBytes: 1})
	}
	resp, _ := h.do("POST", "/api/v1/admin/items/"+it.ID+"/teile-zusammenfuehren", "", h.tok)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("Zusammenführen: %d", resp.StatusCode)
	}
	var zeile string
	bisWahr(t, "Protokollzeile", func() bool {
		log, _ := h.st.RecentLog(t.Context(), "", 50)
		for _, l := range log {
			if strings.HasPrefix(l.Message, "Zusammenführen gescheitert") {
				zeile = l.Message
				return true
			}
		}
		return false
	})
	if !strings.Contains(zeile, ": ffmpeg: ") || strings.Contains(zeile, "Geheim") || strings.Contains(zeile, "/") {
		t.Fatalf("Protokoll: %q", zeile)
	}
}
