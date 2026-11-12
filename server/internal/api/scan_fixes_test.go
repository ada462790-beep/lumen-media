// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

// Regressionstests zu den Befunden aus dem Scan- und dem Leistungsbericht,
// soweit sie die API betreffen.

import (
	"context"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"lumen/internal/store"
)

// B13: Beim Herunterfahren schrieb der Hintergrund-Durchgang noch in die
// schon geschlossene Datenbank — ArbeitAbwarten wartete nicht auf ihn.
func TestHerunterfahrenWartetAufBegleiteteArbeit(t *testing.T) {
	h := newHarness(t)
	grund, beenden := context.WithCancel(context.Background())
	h.svr().SetzeGrund(grund)
	var fertig atomic.Bool
	h.svr().begleitet(func(ctx context.Context) {
		<-ctx.Done()
		time.Sleep(200 * time.Millisecond) // letzte Schreibarbeit
		fertig.Store(true)
	})
	beenden()
	if !h.svr().ArbeitAbwarten(5 * time.Second) {
		t.Fatal("ArbeitAbwarten kam nicht zurück")
	}
	if !fertig.Load() {
		t.Fatal("ArbeitAbwarten kam zurück, bevor die begleitete Arbeit fertig war")
	}
}

// B6: Nach „Zu einer Datei zusammenführen" startete der angekündigte
// Durchlauf nie (abgelaufener Kontext), und die neue Datei blieb ohne
// Laufzeit.
func TestNachDemZusammenfuehrenLaeuftDerDurchlauf(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ohne ffmpeg")
	}
	h := newHarness(t)
	h.film("Platzhalter", 1)
	it := &store.Item{LibraryID: h.lib.ID, Kind: "movie", Title: "Titanic", Year: 1997, MatchState: "matched"}
	_ = h.st.UpsertItem(t.Context(), it)
	for i, b := range []string{"cd1", "cd2"} {
		p := filepath.Join(h.root, "Titanic (1997) "+b+".mkv")
		if out, err := exec.Command("ffmpeg", "-loglevel", "error", "-f", "lavfi", "-i", "testsrc=duration=2:size=160x90:rate=10",
			"-c:v", "libx264", "-y", p).CombinedOutput(); err != nil {
			t.Fatalf("%v %s", err, out)
		}
		_ = h.st.UpsertFile(t.Context(), &store.MediaFile{ItemID: it.ID, Path: p, PartNo: i + 1, SizeBytes: 1})
	}
	vorher := durchlaeufeIm(t, h)
	resp, _ := h.do("POST", "/api/v1/admin/items/"+it.ID+"/teile-zusammenfuehren", "", h.tok)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("Zusammenführen: %d", resp.StatusCode)
	}
	bisWahr(t, "Durchlauf nach dem Zusammenführen", func() bool { return durchlaeufeIm(t, h) > vorher })
	d, _ := h.st.FilesFor(t.Context(), it.ID)
	if len(d) != 1 || d[0].DurationMS < 3000 {
		t.Fatalf("die zusammengeführte Datei hat keine Laufzeit: %+v", d)
	}
}

// B5: Eine defekte Datei abzuspielen gibt einen klaren Fehler (422,
// datei_defekt) statt endlos „Wird umgewandelt" und HTTP 500; unter
// Aufräumen steht sie mit Grund.
func TestDefekteDateiGibtKlarenFehler(t *testing.T) {
	h := newHarness(t)
	_, fileID := h.film("Kaputt", 1)
	f, _ := h.st.File(t.Context(), fileID)
	f.Defekt = "kaputt"
	if err := h.st.UpsertFile(t.Context(), f); err != nil {
		t.Fatal(err)
	}
	for _, pfad := range []string{"/plan", "/browser.mp4", "/hls/master.m3u8", ""} {
		resp, raw := h.do("GET", "/api/v1/stream/"+fileID+pfad, "", h.tok)
		if resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(string(raw), "datei_defekt") {
			t.Errorf("%s: %d %s", pfad, resp.StatusCode, raw)
		}
	}
	_, raw := h.do("GET", "/api/v1/admin/cleanup", "", h.tok)
	if !strings.Contains(string(raw), `"grund":"defekt_kaputt"`) {
		t.Errorf("Aufräumen nennt die defekte Datei nicht: %s", raw)
	}
}

// B4: Fehlende Dateien lassen sich unter Aufräumen endgültig entfernen;
// der Titel ohne Datei verschwindet mit.
func TestFehlendeEndgueltigEntfernen(t *testing.T) {
	h := newHarness(t)
	itemID, fileID := h.film("Up", 1)
	h.film("Bleibt", 1)
	if _, err := h.st.DB().Exec(`UPDATE media_file SET missing_since = 1 WHERE id = ?`, fileID); err != nil {
		t.Fatal(err)
	}
	_, raw := h.do("GET", "/api/v1/admin/cleanup", "", h.tok)
	if !strings.Contains(string(raw), `"missing":1`) {
		t.Errorf("Aufräumen zählt die fehlende Datei nicht: %s", raw)
	}
	resp, raw := h.do("POST", "/api/v1/admin/cleanup/fehlende-entfernen", `{"fileIds":["`+fileID+`"]}`, h.tok)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(raw), `"entfernt":1`) {
		t.Fatalf("entfernen: %d %s", resp.StatusCode, raw)
	}
	if _, err := h.st.Item(t.Context(), itemID); err == nil {
		t.Fatal("der Titel ohne Datei steht noch da")
	}
	if n, _ := h.st.Count(t.Context(), store.Query{Libraries: []string{h.lib.ID}}); n != 1 {
		t.Fatalf("%d Titel übrig, erwartet 1", n)
	}
}

// B12: Die Arbeitssperre vergibt der Reihe nach — wer zuerst wartet,
// kommt zuerst dran, auch wenn ein anderer sie immer wieder neu holt.
func TestArbeitssperreIstFair(t *testing.T) {
	var f faireSperre
	f.Lock()
	reihe := make(chan int, 10)
	for i := 1; i <= 3; i++ {
		go func() {
			f.Lock()
			reihe <- i
			f.Unlock()
		}()
		for f.Wartende() < i {
			time.Sleep(time.Millisecond)
		}
	}
	f.Unlock()
	for erwartet := 1; erwartet <= 3; erwartet++ {
		if got := <-reihe; got != erwartet {
			t.Fatalf("dran war %d, erwartet %d", got, erwartet)
		}
	}
}
