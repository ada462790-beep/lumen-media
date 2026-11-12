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
	"os"
	"strconv"
	"testing"
	"time"
)

// holen liest einen Bereich der Datei über den Direktstrom und misst.
func (k *bzKind) holen(t *testing.T, tok string, bis int) (int, time.Duration) {
	t.Helper()
	req, _ := http.NewRequest("GET", k.h.srv.URL+"/api/v1/stream/"+k.fileID+"?codecs=h264&containers=mkv", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Range", "bytes=0-"+strconv.Itoa(bis-1))
	start := time.Now()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	n, _ := io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusPartialContent && resp.StatusCode != http.StatusOK {
		t.Fatalf("Strom: %d", resp.StatusCode)
	}
	return int(n), time.Since(start)
}

// Ein Kinderprofil mit Bildschirmzeit holt den Direktstrom nicht schneller
// als ungefähr in Echtzeit — auch mit Download-Recht und über ein eigenes
// Programm. Früher kamen drei 25-Minuten-Filme in 0,03 s, und die
// Bildschirmzeit zählte 0 Minuten.
func TestKinderDrosselHaeltEchtzeit(t *testing.T) {
	k := neuesBzKind(t, schulRegel())
	h := k.h
	ctx := t.Context()
	// 5 MB mit 1,6 Mbit/s: 200 KB je Sekunde Film, also gut 26 Sekunden.
	f, err := h.st.File(ctx, k.fileID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.Path, make([]byte, 5<<20), 0o644); err != nil {
		t.Fatal(err)
	}
	f.SizeBytes, f.Bitrate, f.DurationMS = 5<<20, 1_600_000, 26_200
	if err := h.st.UpsertFile(ctx, f); err != nil {
		t.Fatal(err)
	}
	// Der Besitzer: ohne Bremse.
	if _, d := k.holen(t, h.tok, 4_718_592); d > time.Second {
		t.Fatalf("Besitzer gebremst: %v", d)
	}
	// Das Kind: 20 Sekunden Film gehen sofort (ein Viertel der Laufzeit,
	// mindestens 20 Sekunden), die übrigen gut 6 Sekunden mit höchstens
	// doppelter Echtzeit.
	n, d := k.holen(t, k.tok, 5<<20)
	if n != 5<<20 {
		t.Fatalf("nur %d Byte", n)
	}
	if d < 2500*time.Millisecond {
		t.Fatalf("26,2 Sekunden Film in %v — schneller als doppelte Echtzeit", d)
	}
	// Was ausgeliefert wurde, ist für die Bildschirmzeit gemeldet.
	h.api.bz.mu.Lock()
	geliefert := h.api.bz.geliefert[k.id]
	h.api.bz.mu.Unlock()
	if geliefert < 26 {
		t.Fatalf("gemeldete Filmzeit: %.1f s", geliefert)
	}
}

// Ausgelieferte Filmzeit zählt als Bildschirmzeit, auch ohne einen
// einzigen Fortschritt: 25 Minuten Segmente in 44 Sekunden zählen fast 25
// Minuten, nicht 44 Sekunden.
func TestBildschirmzeitZaehltAusgelieferteFilmzeit(t *testing.T) {
	k := neuesBzKind(t, schulRegel())
	h := k.h
	ctx := t.Context()
	h.api.bz.geliefertMelden(k.id, 1500)
	h.api.bz.geliefertMelden(h.ownerID(), 1500) // Erwachsene zählen nie
	h.api.bildschirmzeitTakt(ctx)
	g, _, _ := h.st.BildschirmzeitHeute(ctx, k.id, "2026-11-09")
	if g < 24*time.Minute || g > 25*time.Minute {
		t.Fatalf("gezählt: %v", g)
	}
	// Ein zweiter Takt ohne Neues zählt nichts dazu.
	h.api.bildschirmzeitTakt(ctx)
	if g2, _, _ := h.st.BildschirmzeitHeute(ctx, k.id, "2026-11-09"); g2 != g {
		t.Fatalf("doppelt gezählt: %v → %v", g, g2)
	}
	if g, _, _ := h.st.BildschirmzeitHeute(ctx, h.ownerID(), "2026-11-09"); g != 0 {
		t.Fatalf("Besitzer gezählt: %v", g)
	}
}
