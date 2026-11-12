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
	"strconv"
	"strings"
	"testing"
	"time"

	"lumen/internal/store"
	"lumen/internal/stream"
)

// bzKind: ein Kinderprofil mit Download-Recht, ein Film ab 0 und eine Uhr,
// die der Test stellt.
type bzKind struct {
	h      *harness
	id     string
	tok    string
	itemID string
	fileID string
	uhr    time.Time
}

func neuesBzKind(t *testing.T, regel store.Bildschirmzeit) *bzKind {
	t.Helper()
	h := newHarness(t)
	ctx := t.Context()
	_, _ = h.film("Erwachsenenfilm", 1)
	itemID, fileID := h.film("Sandmann", 1)
	null := 0
	it, _ := h.st.Item(ctx, itemID)
	it.AgeYears = &null
	if err := h.st.UpsertItem(ctx, it); err != nil {
		t.Fatal(err)
	}
	lim, _ := store.KinderGrenzen(6)
	lim.AllowDownload = true
	kind, err := h.st.CreateAccount(ctx, store.NewAccount{Name: "Nina", IsKid: true, Limits: lim, Grants: []string{h.lib.ID}})
	if err != nil {
		t.Fatal(err)
	}
	tok, _ := h.st.IssueToken(ctx, kind.ID, "iPad", "tablet")
	if err := h.st.SetBildschirmzeit(ctx, kind.ID, regel); err != nil {
		t.Fatal(err)
	}
	k := &bzKind{h: h, id: kind.ID, tok: tok, itemID: itemID, fileID: fileID}
	loc, err := time.LoadLocation("Europe/Zurich")
	if err != nil {
		t.Skip(err)
	}
	k.stellen(time.Date(2026, 11, 9, 10, 0, 0, 0, loc)) // Montag
	h.api.bz.mu.Lock()
	h.api.bz.init()
	h.api.bz.nachfrist = 0
	h.api.bz.verwaltungSofort = true
	h.api.bz.mu.Unlock()
	return k
}

func (k *bzKind) stellen(t time.Time) {
	k.uhr = t
	k.h.api.jetzt = func() time.Time { return k.uhr }
}

func (k *bzKind) um(zeit string) {
	t, _ := time.ParseInLocation("15:04", zeit, k.uhr.Location())
	y, m, d := k.uhr.Date()
	k.stellen(time.Date(y, m, d, t.Hour(), t.Minute(), 0, 0, k.uhr.Location()))
}

// zurueck tut so, als hätte das letzte Zählen d früher stattgefunden —
// die Wiedergaben haben sich in der Zwischenzeit laufend gemeldet.
func (k *bzKind) zurueck(d time.Duration) {
	b := &k.h.api.bz
	b.mu.Lock()
	for id := range b.gezaehlt {
		b.gezaehlt[id] = b.gezaehlt[id].Add(-d)
	}
	b.mu.Unlock()
}

func fehlerCode(raw []byte) (code, text string) {
	var b struct{ Code, Error string }
	_ = json.Unmarshal(raw, &b)
	return b.Code, b.Error
}

func schulRegel() store.Bildschirmzeit {
	return store.Bildschirmzeit{Aktiv: true,
		Schultage:  store.Tagesregel{Von: "07:00", Bis: "19:30", Minuten: 90},
		Wochenende: store.Tagesregel{Von: "08:00", Bis: "20:30", Minuten: 120}}
}

// Ausserhalb des Fensters lehnt jeder Einstieg in ein Video ab — mit dem
// Code und der Zeit, ab der es weitergeht. Der Besitzer bleibt frei.
func TestBildschirmzeitSperrtJedenEinstieg(t *testing.T) {
	k := neuesBzKind(t, schulRegel())
	h := k.h
	k.um("20:00")
	wege := []string{
		"/api/v1/stream/" + k.fileID + "/plan",
		"/api/v1/stream/" + k.fileID + "?codecs=h264&containers=mkv",
		"/api/v1/stream/" + k.fileID + "/hls/master.m3u8",
		"/api/v1/stream/" + k.fileID + "/browser.mp4?lauf=kind1",
		"/api/v1/stream/" + k.fileID + "/download",
	}
	for _, weg := range wege {
		resp, raw := h.do("GET", weg, "", k.tok)
		code, text := fehlerCode(raw)
		if resp.StatusCode != http.StatusForbidden || code != "bildschirmzeit" {
			t.Errorf("%s: %d %s", weg, resp.StatusCode, raw)
			continue
		}
		if !strings.Contains(text, "morgen ab 07:00") && !strings.HasSuffix(weg, "/download") {
			t.Errorf("%s: Text %q", weg, text)
		}
	}
	if n := h.live.CountFor(k.id); n != 0 {
		t.Fatalf("%d Wiedergaben trotz Sperre", n)
	}
	// Der Browser fragt nach, warum sein <video> scheiterte.
	_, raw := h.do("GET", "/api/v1/stream/"+k.fileID+"/browser/grund?lauf=kind1", "", k.tok)
	if code, text := fehlerCode(raw); code != "bildschirmzeit" || !strings.Contains(text, "07:00") {
		t.Fatalf("browser/grund: %s", raw)
	}
	// Das Kind sieht, was los ist.
	_, raw = h.do("GET", "/api/v1/me/bildschirmzeit", "", k.tok)
	var stand struct {
		Aktiv, Erlaubt bool
		Meldung        string
		WeiterAb       string
	}
	_ = json.Unmarshal(raw, &stand)
	if !stand.Aktiv || stand.Erlaubt || !strings.Contains(stand.Meldung, "07:00") || !strings.HasPrefix(stand.WeiterAb, "2026-11-10T07:00:00") {
		t.Fatalf("me/bildschirmzeit: %s", raw)
	}
	// Vor dem Fenster am selben Tag: „ab 07:00", nicht „morgen".
	k.um("06:30")
	_, raw = h.do("GET", "/api/v1/stream/"+k.fileID+"/plan", "", k.tok)
	if code, text := fehlerCode(raw); code != "bildschirmzeit" || strings.Contains(text, "morgen") || !strings.Contains(text, "ab 07:00") {
		t.Fatalf("früh am Morgen: %s", raw)
	}
	// Englisch spricht das Profil auch.
	h.api.merkeProfilSprache(k.id, "en-US")
	_, raw = h.do("GET", "/api/v1/stream/"+k.fileID+"/plan", "", k.tok)
	if _, text := fehlerCode(raw); !strings.Contains(text, "from 07:00") {
		t.Fatalf("englisch: %s", raw)
	}
	// Im Fenster geht es.
	k.um("10:00")
	if resp, raw := h.do("GET", "/api/v1/stream/"+k.fileID+"/plan", "", k.tok); resp.StatusCode != 200 {
		t.Fatalf("im Fenster: %d %s", resp.StatusCode, raw)
	}
	_, raw = h.do("GET", "/api/v1/me/bildschirmzeit", "", k.tok)
	var frei struct {
		Erlaubt                    bool
		RestMinuten, BudgetMinuten int
	}
	_ = json.Unmarshal(raw, &frei)
	if !frei.Erlaubt || frei.RestMinuten != 90 || frei.BudgetMinuten != 90 {
		t.Fatalf("im Fenster: %s", raw)
	}
	// Der Besitzer schaut, wann er will.
	k.um("23:00")
	if resp, raw := h.do("GET", "/api/v1/stream/"+k.fileID+"/plan", "", h.tok); resp.StatusCode != 200 {
		t.Fatalf("Besitzer: %d %s", resp.StatusCode, raw)
	}
	if _, raw := h.do("GET", "/api/v1/me/bildschirmzeit", "", h.tok); !strings.Contains(string(raw), `"aktiv":false`) {
		t.Fatalf("Besitzer hat keine Bildschirmzeit: %s", raw)
	}
}

// Zwei Geräte gleichzeitig: eine Minute zählt einmal. Was nicht läuft,
// zählt nicht; Erwachsene zählen gar nicht.
func TestBildschirmzeitZaehltParalleleEinmal(t *testing.T) {
	k := neuesBzKind(t, schulRegel())
	h := k.h
	ctx := t.Context()
	h.live.Start(&stream.Session{AccountID: k.id, DeviceName: "iPad", FileID: k.fileID, Mode: "transcode"})
	h.live.Start(&stream.Session{AccountID: k.id, DeviceName: "Fernseher", FileID: k.fileID, Mode: "direct"})
	h.live.Start(&stream.Session{AccountID: h.ownerID(), DeviceName: "Mac", FileID: k.fileID, Mode: "direct"})

	h.api.bildschirmzeitTakt(ctx) // der Anfang zählt noch nichts
	h.api.bildschirmzeitTakt(ctx)
	if g, _, _ := h.st.BildschirmzeitHeute(ctx, k.id, "2026-11-09"); g != 0 {
		t.Fatalf("gleich nach dem Start: %v", g)
	}
	for i := 0; i < 4; i++ {
		k.zurueck(30 * time.Second)
		h.api.bildschirmzeitTakt(ctx)
	}
	if g, _, _ := h.st.BildschirmzeitHeute(ctx, k.id, "2026-11-09"); g != 2*time.Minute {
		t.Fatalf("nach zwei Minuten auf zwei Geräten: %v", g)
	}
	if g, _, _ := h.st.BildschirmzeitHeute(ctx, h.ownerID(), "2026-11-09"); g != 0 {
		t.Fatalf("Besitzer gezählt: %v", g)
	}
	// Die Verwaltung sieht ungefähr die Minuten — und keinen Titel.
	_, raw := h.do("GET", "/api/v1/admin/accounts", "", h.tok)
	if !strings.Contains(string(raw), `"genutztMinuten":0`) || strings.Contains(string(raw), "Sandmann") {
		t.Fatalf("Verwaltung: %s", raw)
	}

	// Ohne laufende Wiedergabe, aber mit Fortschritt, der sich bewegt
	// (Browser hat vorgeladen): zählt auch. Stehender Fortschritt nicht.
	h.live.EndeKontoMit(k.id, stream.GrundBeendet)
	for _, pos := range []int{1000, 1000} {
		h.do("POST", "/api/v1/items/"+k.itemID+"/progress", `{"positionMs":`+strconv.Itoa(pos)+`,"durationMs":5400000}`, k.tok)
	}
	k.zurueck(30 * time.Second)
	h.api.bildschirmzeitTakt(ctx)
	if g, _, _ := h.st.BildschirmzeitHeute(ctx, k.id, "2026-11-09"); g != 2*time.Minute {
		t.Fatalf("Pause gezählt: %v", g)
	}
	// Nach der langen Pause zählt die erste Meldung noch nichts, die Zeit
	// bis zur nächsten schon.
	h.do("POST", "/api/v1/items/"+k.itemID+"/progress", `{"positionMs":61000,"durationMs":5400000}`, k.tok)
	k.zurueck(30 * time.Second)
	h.api.bildschirmzeitTakt(ctx)
	h.do("POST", "/api/v1/items/"+k.itemID+"/progress", `{"positionMs":91000,"durationMs":5400000}`, k.tok)
	k.zurueck(30 * time.Second)
	h.api.bildschirmzeitTakt(ctx)
	if g, _, _ := h.st.BildschirmzeitHeute(ctx, k.id, "2026-11-09"); g < 2*time.Minute+30*time.Second || g > 2*time.Minute+32*time.Second {
		t.Fatalf("Fortschritt nicht gezählt: %v", g)
	}
}

// Ist die Tagesgrenze erreicht, endet eine laufende Wiedergabe nach der
// Nachfrist; das nächste Segment bekommt die Meldung. Schenkt die
// Verwaltung Zeit, darf das Gerät neu ansetzen.
func TestBildschirmzeitBeendetLaufende(t *testing.T) {
	k := neuesBzKind(t, schulRegel())
	h := k.h
	ctx := t.Context()
	sess := h.live.Start(&stream.Session{AccountID: k.id, DeviceName: "iPad", FileID: k.fileID, Mode: "transcode"})
	_ = h.st.BildschirmzeitZaehlen(ctx, k.id, "2026-11-09", 90*60)

	// Mit Nachfrist: der erste Takt merkt sich nur, dass die Zeit um ist.
	h.api.bz.nachfrist = time.Hour
	h.api.bildschirmzeitTakt(ctx)
	h.api.bildschirmzeitTakt(ctx)
	if !h.live.Laeuft(sess.ID) {
		t.Fatal("vor Ablauf der Nachfrist beendet")
	}
	h.api.bz.nachfrist = 0
	h.api.bildschirmzeitTakt(ctx)
	if h.live.Laeuft(sess.ID) {
		t.Fatal("läuft nach Ablauf weiter")
	}
	seg := "/api/v1/stream/" + k.fileID + "/hls/" + sess.ID + "/seg00001.ts"
	resp, raw := h.do("GET", seg, "", k.tok)
	if code, text := fehlerCode(raw); resp.StatusCode != 403 || code != "bildschirmzeit" || !strings.Contains(text, "morgen ab 07:00") {
		t.Fatalf("Segment nach dem Ende: %d %s", resp.StatusCode, raw)
	}
	if resp, _ := h.do("GET", "/api/v1/stream/"+k.fileID+"/hls/"+sess.ID+"/media.m3u8", "", k.tok); resp.StatusCode != 403 {
		t.Fatalf("Liste nach dem Ende: %d", resp.StatusCode)
	}

	// Heute einmalig 30 Minuten mehr.
	resp, raw = h.do("POST", "/api/v1/admin/accounts/"+k.id+"/bildschirmzeit/geschenk", `{"minuten":30}`, h.tok)
	if resp.StatusCode != 200 || !strings.Contains(string(raw), `"budgetMinuten":120`) || !strings.Contains(string(raw), `"geschenkt":30`) {
		t.Fatalf("schenken: %d %s", resp.StatusCode, raw)
	}
	resp, raw = h.do("GET", seg, "", k.tok)
	if code, _ := fehlerCode(raw); resp.StatusCode != http.StatusGone || code != stream.GrundAbgelaufen {
		t.Fatalf("nach dem Schenken neu ansetzen: %d %s", resp.StatusCode, raw)
	}
	if resp, _ := h.do("GET", "/api/v1/stream/"+k.fileID+"/plan", "", k.tok); resp.StatusCode != 200 {
		t.Fatalf("Plan nach dem Schenken: %d", resp.StatusCode)
	}
	// Das Kind kann sich selbst nichts schenken.
	if resp, _ := h.do("POST", "/api/v1/admin/accounts/"+k.id+"/bildschirmzeit/geschenk", `{"minuten":30}`, k.tok); resp.StatusCode < 400 {
		t.Fatalf("Kind schenkt sich Zeit: %d", resp.StatusCode)
	}
}

// Um Mitternacht beginnt ein neuer Tag — in der Zeitzone des Servers.
func TestBildschirmzeitMitternacht(t *testing.T) {
	nurGrenze := store.Bildschirmzeit{Aktiv: true,
		Schultage: store.Tagesregel{Minuten: 30}, Wochenende: store.Tagesregel{Minuten: 60}}
	k := neuesBzKind(t, nurGrenze)
	h := k.h
	ctx := t.Context()
	k.um("23:50")
	_ = h.st.BildschirmzeitZaehlen(ctx, k.id, "2026-11-09", 30*60)
	resp, raw := h.do("GET", "/api/v1/stream/"+k.fileID+"/plan", "", k.tok)
	if code, text := fehlerCode(raw); resp.StatusCode != 403 || code != "bildschirmzeit" || !strings.Contains(text, "morgen geht es weiter") || strings.Contains(text, "00:00") {
		t.Fatalf("aufgebraucht: %d %s", resp.StatusCode, raw)
	}
	h.api.bildschirmzeitTakt(ctx)
	k.stellen(k.uhr.Add(15 * time.Minute)) // Dienstag 00:05
	if resp, raw := h.do("GET", "/api/v1/stream/"+k.fileID+"/plan", "", k.tok); resp.StatusCode != 200 {
		t.Fatalf("nach Mitternacht: %d %s", resp.StatusCode, raw)
	}
	// Gestern bleibt bis zum nächsten Zählen des Profils (ein Nachtfenster
	// könnte noch dazugehören) — spätestens am Tag danach ist es weg.
	h.api.bildschirmzeitTakt(ctx)
	if g, _, _ := h.st.BildschirmzeitHeute(ctx, k.id, "2026-11-10"); g != 0 {
		t.Fatalf("neuer Tag nicht frisch: %v", g)
	}
	morgen := k.uhr
	k.stellen(morgen.AddDate(0, 0, 1))
	h.api.bildschirmzeitTakt(ctx)
	k.stellen(morgen)
	var n int
	_ = h.st.DB().QueryRow(`SELECT count(*) FROM bildschirmzeit_tag WHERE tag = '2026-11-09'`).Scan(&n)
	if n != 0 {
		t.Fatalf("vorgestern liegt noch da: %d", n)
	}
	// Dieselbe Sekunde in New York ist noch Montag: dort wäre die Grenze
	// noch aufgebraucht. Massgeblich ist die Zone der Uhr des Servers.
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip(err)
	}
	k.stellen(k.uhr.In(ny))
	_ = h.st.BildschirmzeitZaehlen(ctx, k.id, "2026-11-09", 30*60)
	if resp, _ := h.do("GET", "/api/v1/stream/"+k.fileID+"/plan", "", k.tok); resp.StatusCode != 403 {
		t.Fatalf("New York: %d", resp.StatusCode)
	}
}

// Die Verwaltung stellt die Regel ein — nur für Kinderprofile, nur mit
// gültigen Zeiten, und mit dem Code.
func TestBildschirmzeitVerwaltung(t *testing.T) {
	k := neuesBzKind(t, store.Bildschirmzeit{})
	h := k.h
	// Noch nie eingestellt: der Vorschlag, ausgeschaltet.
	_, raw := h.do("GET", "/api/v1/admin/accounts", "", h.tok)
	var konten []struct {
		ID             string
		Bildschirmzeit *bzVerwaltung
	}
	_ = json.Unmarshal(raw, &konten)
	gesehen := 0
	for _, a := range konten {
		if a.ID == k.id {
			gesehen++
			if a.Bildschirmzeit == nil || a.Bildschirmzeit.Regel.Aktiv || a.Bildschirmzeit.Regel.Schultage.Von != "07:00" {
				t.Fatalf("Vorschlag: %+v", a.Bildschirmzeit)
			}
		} else if a.Bildschirmzeit != nil {
			t.Fatalf("Erwachsene haben keine Bildschirmzeit: %s", raw)
		}
	}
	if gesehen != 1 {
		t.Fatalf("Kind fehlt: %s", raw)
	}
	weg := "/api/v1/admin/accounts/" + k.id + "/bildschirmzeit"
	h.ohneBestaetigung = true
	if resp, _ := h.do("PUT", weg, `{"aktiv":true,"schultage":{"von":"07:00","bis":"19:30","minuten":60},"wochenende":{"von":"","bis":"","minuten":0}}`, h.tok); resp.StatusCode != 428 {
		t.Fatalf("ohne Code: %d", resp.StatusCode)
	}
	h.ohneBestaetigung = false
	resp, raw := h.do("PUT", weg, `{"aktiv":true,"schultage":{"von":"07:00","bis":"19:30","minuten":60},"wochenende":{"von":"","bis":"","minuten":0}}`, h.tok)
	if resp.StatusCode != 200 || !strings.Contains(string(raw), `"budgetMinuten":60`) {
		t.Fatalf("speichern: %d %s", resp.StatusCode, raw)
	}
	if resp, raw := h.do("PUT", weg, `{"aktiv":true,"schultage":{"von":"7 Uhr","bis":"19:30","minuten":60},"wochenende":{}}`, h.tok); resp.StatusCode != 400 {
		t.Fatalf("ungültig: %d %s", resp.StatusCode, raw)
	}
	gast := h.gast("Gast", false, h.lib.ID)
	_ = gast
	var gastID string
	accs, _ := h.st.Accounts(t.Context())
	for _, a := range accs {
		if a.Name == "Gast" {
			gastID = a.ID
		}
	}
	if resp, _ := h.do("PUT", "/api/v1/admin/accounts/"+gastID+"/bildschirmzeit", `{"aktiv":true}`, h.tok); resp.StatusCode != 400 {
		t.Fatalf("Erwachsener: %d", resp.StatusCode)
	}
	if resp, _ := h.do("PUT", weg, `{"aktiv":false}`, k.tok); resp.StatusCode < 400 {
		t.Fatalf("Kind ändert seine Regel: %d", resp.StatusCode)
	}
	if b, _ := h.st.BildschirmzeitRegel(t.Context(), k.id); !b.Aktiv || b.Schultage.Minuten != 60 {
		t.Fatalf("gespeichert: %+v", b)
	}
}

// Mit einer Tagesgrenze gibt es keinen Download — auch im Fenster nicht:
// Was von der Download-Adresse läuft, zählt der Server nicht. Mit einem
// blossen Zeitfenster geht es im Fenster, ausserhalb nicht.
func TestBildschirmzeitDownload(t *testing.T) {
	k := neuesBzKind(t, schulRegel())
	h := k.h
	k.um("10:00")
	resp, raw := h.do("GET", "/api/v1/stream/"+k.fileID+"/download", "", k.tok)
	if code, text := fehlerCode(raw); resp.StatusCode != 403 || code != "bildschirmzeit" || !strings.Contains(text, "Tagesgrenze") {
		t.Fatalf("Download mit Tagesgrenze: %d %s", resp.StatusCode, raw)
	}
	_, raw = h.do("GET", "/api/v1/items/"+k.itemID+"/download/info", "", k.tok)
	if !strings.Contains(string(raw), `"erlaubt":false`) {
		t.Fatalf("download/info mit Tagesgrenze: %s", raw)
	}
	nurFenster := store.Bildschirmzeit{Aktiv: true,
		Schultage: store.Tagesregel{Von: "07:00", Bis: "19:30"}, Wochenende: store.Tagesregel{Von: "08:00", Bis: "20:30"}}
	if err := h.st.SetBildschirmzeit(t.Context(), k.id, nurFenster); err != nil {
		t.Fatal(err)
	}
	if resp, raw := h.do("GET", "/api/v1/stream/"+k.fileID+"/download", "", k.tok); resp.StatusCode != 200 {
		t.Fatalf("Download im Fenster: %d %s", resp.StatusCode, raw)
	}
	k.um("20:00")
	if resp, _ := h.do("GET", "/api/v1/stream/"+k.fileID+"/download", "", k.tok); resp.StatusCode != 403 {
		t.Fatalf("Download ausserhalb des Fensters: %d", resp.StatusCode)
	}
}

// Die Verwaltung sieht die Minuten nur in Viertelstunden: Wer die
// Kontenliste immer wieder lädt, erfährt nicht, in welcher Minute das Kind
// zu schauen begann oder aufhörte.
func TestBildschirmzeitVerwaltungNurViertelstunden(t *testing.T) {
	k := neuesBzKind(t, schulRegel())
	h := k.h
	ctx := t.Context()
	genutzt := func() int {
		_, raw := h.do("GET", "/api/v1/admin/accounts", "", h.tok)
		var konten []struct {
			ID             string
			Bildschirmzeit *bzVerwaltung
		}
		_ = json.Unmarshal(raw, &konten)
		for _, a := range konten {
			if a.ID == k.id && a.Bildschirmzeit != nil {
				return a.Bildschirmzeit.GenutztMinuten
			}
		}
		t.Fatalf("Kind fehlt: %s", raw)
		return -1
	}
	gesehen := map[int]bool{}
	for min := 0; min < 50; min++ {
		if min > 0 {
			_ = h.st.BildschirmzeitZaehlen(ctx, k.id, "2026-11-09", 60)
		}
		g := genutzt()
		if g%15 != 0 || g > min || min-g >= 15 {
			t.Fatalf("nach %d Minuten sieht die Verwaltung %d", min, g)
		}
		gesehen[g] = true
	}
	if len(gesehen) != 4 { // 0, 15, 30, 45
		t.Fatalf("Stufen: %v", gesehen)
	}
	// Das Kind selbst sieht seine Minuten genau.
	_, raw := h.do("GET", "/api/v1/me/bildschirmzeit", "", k.tok)
	if !strings.Contains(string(raw), `"genutztMinuten":49`) {
		t.Fatalf("Kind: %s", raw)
	}
}

// Ein Nachtfenster: Was vor Mitternacht verbraucht ist, ist nach
// Mitternacht noch verbraucht — auch für den Zähler und die Meldung.
func TestBildschirmzeitNachtfensterZaehler(t *testing.T) {
	nacht := store.Bildschirmzeit{Aktiv: true,
		Schultage:  store.Tagesregel{Von: "18:00", Bis: "02:00", Minuten: 90},
		Wochenende: store.Tagesregel{Von: "18:00", Bis: "02:00", Minuten: 90}}
	k := neuesBzKind(t, nacht)
	h := k.h
	ctx := t.Context()
	loc := k.uhr.Location()
	k.stellen(time.Date(2026, 11, 13, 23, 59, 0, 0, loc)) // Freitag
	_ = h.st.BildschirmzeitZaehlen(ctx, k.id, "2026-11-13", 90*60)
	k.stellen(time.Date(2026, 11, 14, 0, 1, 0, 0, loc)) // Samstag
	h.api.bildschirmzeitTakt(ctx)
	resp, raw := h.do("GET", "/api/v1/stream/"+k.fileID+"/plan", "", k.tok)
	if code, text := fehlerCode(raw); resp.StatusCode != 403 || code != "bildschirmzeit" || !strings.Contains(text, "ab 18:00") || strings.Contains(text, "morgen") || strings.Contains(text, "Pause") {
		t.Fatalf("Samstag 00:01: %d %s", resp.StatusCode, raw)
	}
	// Die Verwaltung schenkt Minuten auf den Freitag, der noch läuft.
	if resp, raw := h.do("POST", "/api/v1/admin/accounts/"+k.id+"/bildschirmzeit/geschenk", `{"minuten":30}`, h.tok); resp.StatusCode != 200 || !strings.Contains(string(raw), `"budgetMinuten":120`) || !strings.Contains(string(raw), `"genutztMinuten":90`) {
		t.Fatalf("schenken: %d %s", resp.StatusCode, raw)
	}
	if resp, _ := h.do("GET", "/api/v1/stream/"+k.fileID+"/plan", "", k.tok); resp.StatusCode != 200 {
		t.Fatalf("nach dem Schenken: %d", resp.StatusCode)
	}
}

// Eine Regel, die sich nicht lesen lässt, sperrt — sie gilt nicht still
// als „keine Regel".
func TestBildschirmzeitUnlesbarSperrt(t *testing.T) {
	k := neuesBzKind(t, schulRegel())
	h := k.h
	if _, err := h.st.DB().Exec(`UPDATE bildschirmzeit_regel SET regel = '{kaputt' WHERE account_id = ?`, k.id); err != nil {
		t.Fatal(err)
	}
	resp, raw := h.do("GET", "/api/v1/stream/"+k.fileID+"/plan", "", k.tok)
	if code, text := fehlerCode(raw); resp.StatusCode != 403 || code != "bildschirmzeit" || !strings.Contains(text, "nicht prüfen") {
		t.Fatalf("unlesbare Regel: %d %s", resp.StatusCode, raw)
	}
	if _, raw := h.do("GET", "/api/v1/me/bildschirmzeit", "", k.tok); !strings.Contains(string(raw), `"erlaubt":false`) {
		t.Fatalf("me: %s", raw)
	}
	// Die Verwaltung speichert neu, dann geht es wieder.
	if resp, raw := h.do("PUT", "/api/v1/admin/accounts/"+k.id+"/bildschirmzeit", `{"aktiv":true,"schultage":{"von":"07:00","bis":"19:30","minuten":60},"wochenende":{"von":"","bis":"","minuten":0}}`, h.tok); resp.StatusCode != 200 {
		t.Fatalf("neu speichern: %d %s", resp.StatusCode, raw)
	}
	if resp, _ := h.do("GET", "/api/v1/stream/"+k.fileID+"/plan", "", k.tok); resp.StatusCode != 200 {
		t.Fatalf("nach dem Speichern: %d", resp.StatusCode)
	}
}

// Endet eine Wiedergabe von selbst, während die Zeit schon um ist, bleibt
// keine alte Nachfrist liegen: Beim nächsten Überziehen gibt es wieder die
// ganze.
func TestBildschirmzeitNachfristAufgeraeumt(t *testing.T) {
	k := neuesBzKind(t, schulRegel())
	h := k.h
	ctx := t.Context()
	_ = h.st.BildschirmzeitZaehlen(ctx, k.id, "2026-11-09", 90*60)
	h.api.bz.nachfrist = time.Hour
	sess := h.live.Start(&stream.Session{AccountID: k.id, DeviceName: "iPad", FileID: k.fileID, Mode: "direct"})
	h.api.bildschirmzeitTakt(ctx)
	h.live.End(sess.ID)
	h.api.bildschirmzeitTakt(ctx)
	h.api.bz.mu.Lock()
	_, da := h.api.bz.ueberSeit[k.id]
	h.api.bz.mu.Unlock()
	if da {
		t.Fatal("Nachfrist liegt nach dem Ende noch da")
	}
}
