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
