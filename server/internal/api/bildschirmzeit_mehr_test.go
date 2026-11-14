// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"strings"
	"testing"
	"time"

	"lumen/internal/store"
	"lumen/internal/stream"
)

// „Heute einmalig mehr" ausserhalb des Fensters verlängert das Fenster ab
// jetzt — auch ohne Tagesgrenze —, lässt sich zurücknehmen und meldet
// nicht mehr Erfolg, während das Kind weiter gesperrt ist.
func TestHeuteEinmalMehrVerlaengertFenster(t *testing.T) {
	nurFenster := store.Bildschirmzeit{Aktiv: true,
		Schultage:  store.Tagesregel{Von: "07:00", Bis: "19:30"},
		Wochenende: store.Tagesregel{Von: "08:00", Bis: "20:30"}}
	k := neuesBzKind(t, nurFenster)
	h := k.h
	k.um("22:15")
	plan := "/api/v1/stream/" + k.fileID + "/plan"
	if resp, _ := h.do("GET", plan, "", k.tok); resp.StatusCode != 403 {
		t.Fatalf("ausserhalb des Fensters: %d", resp.StatusCode)
	}
	resp, raw := h.do("POST", "/api/v1/admin/accounts/"+k.id+"/bildschirmzeit/geschenk", `{"minuten":15}`, h.tok)
	if resp.StatusCode != 200 || !strings.Contains(string(raw), `"verlaengertBis":"22:30"`) {
		t.Fatalf("schenken: %d %s", resp.StatusCode, raw)
	}
	if resp, raw := h.do("GET", plan, "", k.tok); resp.StatusCode != 200 {
		t.Fatalf("nach dem Schenken: %d %s", resp.StatusCode, raw)
	}
	_, raw = h.do("GET", "/api/v1/me/bildschirmzeit", "", k.tok)
	if !strings.Contains(string(raw), `"restMinuten":15`) {
		t.Fatalf("Kind: %s", raw)
	}
	k.um("22:31")
	if resp, _ := h.do("GET", plan, "", k.tok); resp.StatusCode != 403 {
		t.Fatalf("nach der Verlängerung: %d", resp.StatusCode)
	}
	// Rückgängig.
	k.um("22:20")
	resp, raw = h.do("DELETE", "/api/v1/admin/accounts/"+k.id+"/bildschirmzeit/geschenk", "", h.tok)
	if resp.StatusCode != 200 || strings.Contains(string(raw), "verlaengertBis") {
		t.Fatalf("zurück: %d %s", resp.StatusCode, raw)
	}
	if resp, _ := h.do("GET", plan, "", k.tok); resp.StatusCode != 403 {
		t.Fatalf("nach dem Zurücknehmen: %d", resp.StatusCode)
	}
	// Ohne Tagesgrenze kommen keine Minuten dazu, die später still gälten.
	if g, gs, _ := h.st.BildschirmzeitHeute(t.Context(), k.id, "2026-11-09"); gs != 0 {
		t.Fatalf("geschenkte Minuten ohne Grenze: %v %d", g, gs)
	}
}

// Die Texte für das Kind: „morgen geht es weiter" statt „morgen ab
// 00:00", und nach dem Ende eines Fensters über Mitternacht „heute ab
// 23:00" statt „Gerade ist Pause".
func TestBildschirmzeitTexteFuerKinder(t *testing.T) {
	nacht := store.Bildschirmzeit{Aktiv: true,
		Schultage:  store.Tagesregel{Von: "23:00", Bis: "00:05"},
		Wochenende: store.Tagesregel{Von: "23:00", Bis: "00:05"}}
	k := neuesBzKind(t, nacht)
	h := k.h
	k.stellen(k.uhr.Add(14*time.Hour + 6*time.Minute)) // Dienstag 00:06
	_, raw := h.do("GET", "/api/v1/stream/"+k.fileID+"/plan", "", k.tok)
	if _, text := fehlerCode(raw); strings.Contains(text, "Pause") || !strings.Contains(text, "heute ab 23:00") {
		t.Fatalf("nach dem Nachtfenster: %s", raw)
	}
	k.um("06:30") // vor dem Fenster, ohne vorheriges Nachtfenster: Pause
	_, raw = h.do("GET", "/api/v1/stream/"+k.fileID+"/plan", "", k.tok)
	if _, text := fehlerCode(raw); !strings.Contains(text, "heute ab 23:00") {
		t.Fatalf("am Morgen: %s", raw)
	}
}

// Die Verwaltung sieht die Minuten nicht in dem Augenblick, in dem das
// Kind gerade schaut: Der Stand bleibt, solange es schaut, und kommt
// erst eine Weile danach (oder spätestens nach einer Stunde).
func TestVerwaltungSiehtBildschirmzeitVerzoegert(t *testing.T) {
	k := neuesBzKind(t, schulRegel())
	h := k.h
	ctx := t.Context()
	h.api.bz.mu.Lock()
	h.api.bz.verwaltungSofort = false
	h.api.bz.mu.Unlock()
	stand := func() string {
		_, raw := h.do("GET", "/api/v1/admin/accounts", "", h.tok)
		i := strings.Index(string(raw), `"genutztMinuten":`)
		return string(raw)[i : i+20]
	}
	vorher := stand()
	// Das Kind schaut: 16 Minuten kommen zusammen, während es schaut.
	h.api.bz.geliefertMelden(k.id, 16*60+30)
	h.api.bildschirmzeitTakt(ctx)
	if g, _, _ := h.st.BildschirmzeitHeute(ctx, k.id, "2026-11-09"); g < 15*time.Minute {
		t.Fatalf("nicht gezählt: %v", g)
	}
	if jetzt := stand(); jetzt != vorher {
		t.Fatalf("Stand sprang, während das Kind schaut: %s → %s", vorher, jetzt)
	}
	// Lange nichts mehr: jetzt darf der neue Stand kommen.
	h.api.bz.mu.Lock()
	h.api.bz.zuletztAktiv[k.id] = time.Now().Add(-time.Hour)
	h.api.bz.mu.Unlock()
	if jetzt := stand(); !strings.HasPrefix(jetzt, `"genutztMinuten":15`) {
		t.Fatalf("Stand danach: %s", jetzt)
	}
}

// Eine Minute heisst eine Minute: Was seit der letzten gezählten Meldung
// lief, rechnet der Wächter einer laufenden Wiedergabe vorab zu — sonst
// griff die Grenze erst einen Meldeabstand plus Takt zu spät. Länger
// Stilles (eine Pause) rechnet er nur bis bzOffenHoechstens zu.
func TestBildschirmzeitGreiftAufWenigeSekunden(t *testing.T) {
	regel := store.Bildschirmzeit{Aktiv: true,
		Schultage:  store.Tagesregel{Von: "07:00", Bis: "19:30", Minuten: 1},
		Wochenende: store.Tagesregel{Von: "08:00", Bis: "20:30", Minuten: 1}}
	k := neuesBzKind(t, regel)
	h := k.h
	ctx := t.Context()
	acc, _ := h.st.Account(ctx, k.id)
	_ = h.st.BildschirmzeitZaehlen(ctx, k.id, "2026-11-09", 52)

	setze := func(vor time.Duration) {
		h.api.bz.mu.Lock()
		h.api.bz.gezaehlt[k.id] = time.Now().Add(-vor)
		h.api.bz.mu.Unlock()
	}
	setze(9 * time.Second)
	l, _ := h.api.bildschirmzeitLage(ctx, acc)
	if !l.urteil.Erlaubt {
		t.Fatal("nach 52 gezählten Sekunden schon gesperrt")
	}
	h.api.bzMitOffen(&l, k.id, time.Now())
	if l.urteil.Erlaubt {
		t.Fatalf("52 s gezählt + 9 s offen sind über einer Minute, Urteil %+v", l.urteil)
	}

	// Lange still (Pause): höchstens bzOffenHoechstens zugerechnet.
	_ = h.st.BildschirmzeitZaehlen(ctx, k.id, "2026-11-09", -30)
	setze(40 * time.Second)
	l, _ = h.api.bildschirmzeitLage(ctx, acc)
	h.api.bzMitOffen(&l, k.id, time.Now())
	if !l.urteil.Erlaubt {
		t.Fatalf("22 s gezählt + gedeckelte Pause gesperrt: %+v", l.urteil)
	}

	// Der Wächter beendet die laufende Wiedergabe gleich im nächsten Takt.
	_ = h.st.BildschirmzeitZaehlen(ctx, k.id, "2026-11-09", 30)
	sess := h.live.Start(&stream.Session{AccountID: k.id, DeviceName: "iPad", FileID: k.fileID, Mode: "transcode"})
	setze(9 * time.Second)
	h.api.bildschirmzeitTakt(ctx)
	if h.live.Laeuft(sess.ID) {
		t.Fatal("läuft über die Minute hinaus weiter")
	}
	// Danach ist die Zeit auch ohne laufende Wiedergabe um: Was vorab
	// zugerechnet wurde, ist gezählt.
	if l, _ := h.api.bildschirmzeitLage(ctx, acc); l.urteil.Erlaubt {
		t.Fatalf("nach dem Ende wieder frei: genutzt %v", l.genutzt)
	}
	if bzTakt > 5*time.Second || bzNachfrist+bzNachfristZufall > 6*time.Second {
		t.Fatalf("Takt %v, Nachfrist %v + %v: zu viel Überzug", bzTakt, bzNachfrist, bzNachfristZufall)
	}
}
