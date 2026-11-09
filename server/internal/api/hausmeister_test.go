// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// bisWahr wartet, bis f wahr wird — höchstens zehn Sekunden.
func bisWahr(t *testing.T, was string, f func() bool) {
	t.Helper()
	ende := time.Now().Add(10 * time.Second)
	for !f() {
		if time.Now().After(ende) {
			t.Fatalf("nach zehn Sekunden nicht erreicht: %s", was)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func durchlaeufeIm(t *testing.T, h *harness) int {
	t.Helper()
	log, _ := h.st.RecentLog(t.Context(), "", 500)
	n := 0
	for _, l := range log {
		if l.Area == "scan" && strings.Contains(l.Message, " geprüft ") {
			n++
		}
	}
	return n
}

// Viele, die gleichzeitig „Prüfen" wollen — der Hausmeister, Knöpfe in
// zwei Browsern, das Zusammenfügen von Teilen —, während gerade etwas
// anderes läuft: Es wartet genau ein Durchlauf, und es läuft genau einer.
// Früher lagen Nachsehen und Eintragen auseinander, und wer zuerst dran
// kam, löschte den Merker der anderen, die noch warteten.
func TestNurEinDurchlaufWartet(t *testing.T) {
	h := newHarness(t)
	h.film("Nordlicht", 1)
	libs, _ := h.st.Libraries(t.Context())
	lib := libs[0]

	// Etwas anderes hält die Arbeitssperre (ein Durchlauf einer anderen
	// Bibliothek, Metadaten …).
	arbeitMu.Lock()
	gesperrt := true
	defer func() {
		if gesperrt {
			arbeitMu.Unlock()
		}
	}()

	const aufrufer, knoepfe = 8, 4
	var zurueck, angenommen, abgelehnt atomic.Int32
	var alle sync.WaitGroup
	for i := 0; i < aufrufer; i++ {
		alle.Add(1)
		go func() {
			defer alle.Done()
			h.svr().Durchlauf(context.Background(), lib, true)
			zurueck.Add(1)
		}()
	}
	for i := 0; i < knoepfe; i++ {
		alle.Add(1)
		go func() {
			defer alle.Done()
			resp, _ := h.do("POST", "/api/v1/admin/libraries/"+lib.ID+"/scan", "", h.tok)
			switch resp.StatusCode {
			case http.StatusOK:
				angenommen.Add(1)
			case http.StatusConflict:
				abgelehnt.Add(1)
			default:
				t.Errorf("Prüfen: %d", resp.StatusCode)
			}
		}()
	}
	// Alle bis auf einen sind sofort fertig: abgelehnt oder gar nicht erst
	// eingereiht. Würden zwei angenommen, käme man hier nicht an.
	wartende := func() int { return aufrufer - int(zurueck.Load()) + int(angenommen.Load()) }
	bisWahr(t, "alle Knöpfe beantwortet und genau ein Durchlauf eingereiht", func() bool {
		return int(abgelehnt.Load()+angenommen.Load()) == knoepfe && wartende() == 1
	})

	// Die Verwaltung zeigt, dass er wartet.
	_, raw := h.do("GET", "/api/v1/admin/libraries", "", h.tok)
	var ansicht []struct {
		ID     string
		Wartet bool `json:"wartet"`
	}
	_ = json.Unmarshal(raw, &ansicht)
	if len(ansicht) != 1 || !ansicht[0].Wartet {
		t.Fatalf("wartender Durchlauf nicht sichtbar: %s", raw)
	}

	arbeitMu.Unlock()
	gesperrt = false
	alle.Wait()
	bisWahr(t, "Durchlauf fertig", func() bool { return !h.svr().laeuftOderWartet(lib.ID) })
	if n := durchlaeufeIm(t, h); n != 1 {
		t.Fatalf("%d Durchläufe gelaufen, erwartet genau einer", n)
	}

	// Danach lässt sich wieder prüfen.
	resp, _ := h.do("POST", "/api/v1/admin/libraries/"+lib.ID+"/scan", "", h.tok)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Prüfen nach dem Durchlauf: %d", resp.StatusCode)
	}
	bisWahr(t, "zweiter Durchlauf fertig", func() bool { return !h.svr().laeuftOderWartet(lib.ID) && durchlaeufeIm(t, h) == 2 })
}

// Fährt Lumen herunter, fängt ein wartender Durchlauf nicht mehr an, und
// ArbeitAbwarten kommt zurück, sobald die laufende Arbeit aufgehört hat.
func TestHerunterfahrenBeendetDieArbeit(t *testing.T) {
	h := newHarness(t)
	h.film("Nordlicht", 1)
	libs, _ := h.st.Libraries(t.Context())
	grund, beenden := context.WithCancel(context.Background())
	h.svr().SetzeGrund(grund)

	arbeitMu.Lock()
	resp, _ := h.do("POST", "/api/v1/admin/libraries/"+libs[0].ID+"/scan", "", h.tok)
	if resp.StatusCode != http.StatusOK {
		arbeitMu.Unlock()
		t.Fatalf("Prüfen: %d", resp.StatusCode)
	}
	beenden()
	arbeitMu.Unlock()
	if !h.svr().ArbeitAbwarten(5 * time.Second) {
		t.Fatal("ArbeitAbwarten kam nicht zurück")
	}
	bisWahr(t, "Auftrag ausgetragen", func() bool { return !h.svr().laeuftOderWartet(libs[0].ID) })
	if n := durchlaeufeIm(t, h); n != 0 {
		t.Fatalf("nach dem Herunterfahren lief noch ein Durchlauf an (%d)", n)
	}
}
