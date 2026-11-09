// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package stream

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Was der Besitzer in der Verwaltung sieht, ist eine feste, kurze Liste
// von Feldern. Kein Feld darf dazukommen, ohne dass dieser Test bewusst
// geändert wird — nicht der Titel, aber auch nicht Person, Gerät, Ort oder
// Startzeit.
func TestAdminSessionNurErlaubteFelder(t *testing.T) {
	erlaubt := map[string]bool{"ID": true, "Mode": true}
	typ := reflect.TypeOf(AdminSession{})
	for i := 0; i < typ.NumField(); i++ {
		if f := typ.Field(i); !erlaubt[f.Name] {
			t.Errorf("AdminSession.%s ist nicht erlaubt. Der Besitzer sieht nur die Last — "+
				"keinen Titel, keine Person, kein Gerät, keinen Ort, keine Startzeit.", f.Name)
		}
	}
	if typ.NumField() != len(erlaubt) {
		t.Errorf("AdminSession hat %d Felder, erlaubt sind %d", typ.NumField(), len(erlaubt))
	}
}

// Auch durch JSON darf nichts durchrutschen: die ausgelieferte Form wird
// einmal wirklich erzeugt und auf verräterische Werte geprüft.
func TestOwnerListingCarriesNoTitle(t *testing.T) {
	l := NewLive()
	a := l.Start(&Session{
		AccountID: "acc1", Name: "Mara", Avatar: "av-b",
		DeviceName: "Wohnzimmer", DeviceKind: "tv", FileID: "f_geheim",
		itemID: "it_geheim", path: "/medien/filme/Peinlicher.Film.2009.mkv",
		Mode: "transcode", Reason: "Bandbreite", Bitrate: 6_000_000,
		FromCodec: "hevc", ToCodec: "h264", FromHeight: 2160, ToHeight: 1080,
	})
	b := l.Start(&Session{AccountID: "acc1", Name: "Mara", DeviceName: "Handy von Mara",
		DeviceKind: "phone", Mode: "direct", FromCodec: "h264", FromHeight: 1080})

	raw, err := json.Marshal(l.ForOwner())
	if err != nil {
		t.Fatal(err)
	}
	body := strings.ToLower(string(raw))
	for _, leak := range []string{"it_geheim", "f_geheim", "peinlicher", ".mkv", "/medien",
		"mara", "wohnzimmer", "handy", "acc1", "av-b", "\"tv\"", "phone",
		// Gründe und Qualität machen eine Wiedergabe einer Person zuordenbar.
		"bandbreite", "hevc", "h264", "4k", "1080", "6000000", "reason", "detail", "bitrate",
		strings.ToLower(a.ID), strings.ToLower(b.ID), strings.ToLower(a.adminID), strings.ToLower(b.adminID)} {
		if strings.Contains(body, leak) {
			t.Fatalf("die Besitzer-Ansicht enthält %q:\n%s", leak, raw)
		}
	}

	var got []AdminSession
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("erwartet zwei Wiedergaben, bekommen %d", len(got))
	}
	// Die Kennungen der Verwaltung haben nichts gemeinsam — man sieht
	// nicht, dass beide aus demselben Profil kommen.
	if got[0].ID == got[1].ID || len(got[0].ID) < 32 {
		t.Fatalf("Kennungen taugen nicht: %q %q", got[0].ID, got[1].ID)
	}
	// Was der Besitzer braucht, muss dagegen da sein.
	var umw *AdminSession
	for i := range got {
		if got[i].Mode == "transcode" {
			umw = &got[i]
		}
	}
	if umw == nil || got[0].Mode != "transcode" {
		t.Fatalf("Umwandlungen sollen zuerst stehen: %+v", got)
	}
	// Eine zweite Abfrage gibt neue Kennungen — keine Zeile lässt sich
	// über die Zeit verfolgen.
	nochmal := l.ForOwner()
	for _, n := range nochmal {
		for _, g := range got {
			if n.ID == g.ID {
				t.Fatalf("Kennung %q blieb zwischen zwei Abfragen gleich", n.ID)
			}
		}
	}
	// Beenden über die Kennung der Verwaltung.
	if !l.EndAdmin(umw.ID) || l.Load().Total != 1 {
		t.Fatal("Beenden über die Verwaltung greift nicht")
	}
}

// Eine Person sieht und beendet nur ihre eigenen Wiedergaben.
func TestEigeneWiedergaben(t *testing.T) {
	l := NewLive()
	l.Start(&Session{AccountID: "a", DeviceName: "Fernseher", Mode: "direct"})
	l.Start(&Session{AccountID: "b", DeviceName: "Handy von B", Mode: "direct"})
	eigene := l.ForAccount("a")
	if len(eigene) != 1 || eigene[0].DeviceName != "Fernseher" {
		t.Fatalf("eigene Liste falsch: %+v", eigene)
	}
	fremd := l.ForAccount("b")[0].ID
	if l.EndEigene("a", fremd) {
		t.Fatal("A konnte die Wiedergabe von B beenden")
	}
	if !l.EndEigene("a", eigene[0].ID) || len(l.ForAccount("a")) != 0 {
		t.Fatal("A konnte die eigene Wiedergabe nicht beenden")
	}
}

// Ein Segment gibt es nur für die eigene, laufende Wiedergabe dieser Datei.
func TestGehoert(t *testing.T) {
	l := NewLive()
	s := l.Start(&Session{AccountID: "a", FileID: "f1", Mode: "transcode"})
	if !l.Gehoert(s.ID, "a", "f1") {
		t.Fatal("eigene Wiedergabe nicht erkannt")
	}
	if l.Gehoert(s.ID, "b", "f1") || l.Gehoert(s.ID, "a", "f2") || l.Gehoert("../x", "a", "f1") {
		t.Fatal("fremde Wiedergabe durchgelassen")
	}
}

// Sitzungen verschwinden, wenn sie enden — nichts bleibt liegen.
func TestSessionsAreNotKept(t *testing.T) {
	l := NewLive()
	s := l.Start(&Session{AccountID: "a", Name: "A", DeviceName: "iPhone", Mode: "direct"})
	if len(l.ForOwner()) != 1 {
		t.Fatal("Sitzung fehlt")
	}
	l.End(s.ID)
	if len(l.ForOwner()) != 0 {
		t.Fatal("beendete Sitzung ist noch da")
	}
	if l.Load().Total != 0 {
		t.Fatal("Last zählt beendete Sitzungen mit")
	}
}

func TestStoppedClosesOnEnd(t *testing.T) {
	l := NewLive()
	s := l.Start(&Session{AccountID: "a", Name: "A"})
	done := l.Stopped(s.ID)
	l.End(s.ID)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Beenden erreichte die laufende Wiedergabe nicht")
	}
}

// Die Entscheidung direkt/umwandeln ist der Kern der Wiedergabe.
func TestDecide(t *testing.T) {
	appleTV := Device{Codecs: []string{"h264", "hevc"}, Containers: []string{"mp4", "mkv", "hls"},
		MaxHeight: 2160, HDR: true}
	altesTablet := Device{Codecs: []string{"h264"}, Containers: []string{"mp4", "hls"},
		MaxHeight: 1080}

	if p := Decide(FileInfo("hevc", "mkv", 2160, 40_000_000, "DV"), appleTV, 0); !p.Direct {
		t.Errorf("Apple TV sollte direkt abspielen: %+v", p)
	}
	p := Decide(FileInfo("hevc", "mkv", 2160, 40_000_000, "DV"), altesTablet, 0)
	if p.Direct {
		t.Error("altes Tablet kann kein HEVC — müsste umwandeln")
	}
	// Der Grund wandert in die Verwaltung: keine Dateiendung, kein Pfad.
	for _, bad := range []string{".mkv", ".mp4", "/", "mkv"} {
		if strings.Contains(p.Reason, bad) {
			t.Errorf("der Grund %q zeigt auf die Datei (%q)", p.Reason, bad)
		}
	}
	if p.Height != 1080 || p.VideoCodec != "h264" {
		t.Errorf("Umwandlungsplan falsch: %+v", p)
	}
	// Enge Leitung: auch ein passender Codec muss dann kleiner werden.
	p = Decide(FileInfo("h264", "mp4", 1080, 12_000_000, ""), appleTV, 6_000_000)
	if p.Direct || p.Bitrate != 6_000_000 || p.Reason != "Bandbreite" {
		t.Errorf("Bandbreitengrenze greift nicht: %+v", p)
	}
}

// Ein Pfad aus einer Anfrage darf die Bibliothek nie verlassen.
func TestSafeJoin(t *testing.T) {
	for _, bad := range []string{"../etc/passwd", "../../../../etc/shadow", "a/../../b"} {
		if _, err := SafeJoin("/medien/filme", bad); err == nil {
			t.Errorf("%q wurde durchgelassen", bad)
		}
	}
	got, err := SafeJoin("/medien/filme", "Nordlicht/film.mkv")
	if err != nil || got != "/medien/filme/Nordlicht/film.mkv" {
		t.Errorf("gültiger Pfad abgelehnt: %q %v", got, err)
	}
}

// ffprobe nennt Container anders als die Geräte — das muss zusammenpassen,
// sonst wird jede Matroska-Datei grundlos umgewandelt.
func TestContainerNamesMatchDevices(t *testing.T) {
	cases := map[string]string{
		"matroska,webm":           "mkv",
		"mov,mp4,m4a,3gp,3g2,mj2": "mp4",
		"avi":                     "avi",
		"mpegts":                  "ts",
	}
	for in, want := range cases {
		if got := normalizeContainer(in); got != want {
			t.Errorf("%q → %q, erwartet %q", in, got, want)
		}
	}
	// Und der Durchstich: eine Matroska-Datei auf einem Apple TV geht direkt.
	appleTV := Device{Codecs: []string{"h264", "hevc"},
		Containers: []string{"mp4", "mkv", "hls"}, MaxHeight: 2160, HDR: true}
	p := Decide(FileInfo("h264", normalizeContainer("matroska,webm"), 1080, 8_000_000, ""), appleTV, 0)
	if !p.Direct {
		t.Errorf("Matroska müsste direkt laufen, wurde aber umgewandelt: %q", p.Reason)
	}
}

// TestVerwaisteWiedergabeWirdGefunden hält den Fehler fest, der beim
// Fernzugriff auffiel: eine umgewandelte Wiedergabe, die sich nicht
// abmeldet, blieb für immer stehen — und belegte den Platz der Person
// unter „gleichzeitig schauen", bis der Server neu startete.
func TestVerwaisteWiedergabeWirdGefunden(t *testing.T) {
	l := NewLive()
	a := l.Start(&Session{AccountID: "acc_1", DeviceName: "Handy", Mode: "transcode"})
	b := l.Start(&Session{AccountID: "acc_1", DeviceName: "Fernseher", Mode: "transcode"})

	// Beide sind frisch — es ist noch nichts verwaist.
	if alt := l.Verwaiste(time.Minute); len(alt) != 0 {
		t.Fatalf("frische Wiedergaben gelten schon als verwaist: %v", alt)
	}

	// Das Handy verliert das Netz: es meldet sich nicht mehr. Der Fernseher
	// holt weiter Segmente.
	l.byID[a.ID].zuletzt = time.Now().Add(-5 * time.Minute)
	l.Melden(b.ID)

	alt := l.Verwaiste(2 * time.Minute)
	if len(alt) != 1 || alt[0] != a.ID {
		t.Fatalf("erwartet wurde genau das Handy, bekommen: %v", alt)
	}

	if l.CountFor("acc_1") != 2 {
		t.Fatal("vor dem Aufräumen zählen noch beide")
	}
	l.End(alt[0])
	if n := l.CountFor("acc_1"); n != 1 {
		t.Fatalf("nach dem Aufräumen sollte einer übrig sein, es sind %d", n)
	}
}

// TestMeldenHaeltAmLeben: solange Segmente abgeholt werden, darf nichts
// aufgeräumt werden — sonst bräche ein laufender Film mittendrin ab.
func TestMeldenHaeltAmLeben(t *testing.T) {
	l := NewLive()
	s := l.Start(&Session{AccountID: "acc_1", DeviceName: "Fernseher", Mode: "transcode"})

	l.byID[s.ID].zuletzt = time.Now().Add(-5 * time.Minute)
	l.Melden(s.ID) // das Gerät holt das nächste Segment

	if alt := l.Verwaiste(2 * time.Minute); len(alt) != 0 {
		t.Fatalf("eine Wiedergabe, die sich gerade gemeldet hat, wurde aufgeräumt: %v", alt)
	}
}

// Ton, den das Gerät nicht kann, heisst umwandeln — sonst läuft der Film
// stumm. HDR für ein Gerät ohne HDR höchstens in 1080p.
func TestDecideTonUndHDR(t *testing.T) {
	tv := Device{Codecs: []string{"h264", "hevc"}, Containers: []string{"mkv", "mp4", "hls"},
		MaxHeight: 2160, Audio: []string{"aac", "ac3", "eac3"}}
	p := Decide(FileInfo("h264", "mkv", 1080, 8_000_000, "").MitTon("dts"), tv, 0)
	if p.Direct || p.Reason != "Tonformat kann das Gerät nicht" {
		t.Errorf("DTS auf einem Gerät ohne DTS: %+v", p)
	}
	if p := Decide(FileInfo("h264", "mkv", 1080, 8_000_000, "").MitTon("eac3"), tv, 0); !p.Direct {
		t.Errorf("E-AC-3 sollte direkt gehen: %+v", p)
	}
	if p := Decide(FileInfo("h264", "mkv", 1080, 8_000_000, "").MitTon("mlp"), tv, 0); p.Direct {
		t.Errorf("TrueHD (mlp) sollte umgewandelt werden: %+v", p)
	}
	p = Decide(FileInfo("hevc", "mkv", 2160, 40_000_000, "HDR10"), tv, 0)
	if p.Direct || p.Height != 1080 {
		t.Errorf("HDR ohne HDR-Gerät: %+v", p)
	}
}

// Eine Kennung aus der Liste der Verwaltung gilt nur einmal.
func TestEinmalKennungNurEinmal(t *testing.T) {
	l := NewLive()
	l.Start(&Session{AccountID: "a", Mode: "transcode"})
	l.Start(&Session{AccountID: "b", Mode: "transcode"})
	liste := l.ForOwner()
	if !l.EndAdmin(liste[0].ID) {
		t.Fatal("Beenden über die Einmalkennung geht nicht")
	}
	if l.EndAdmin(liste[0].ID) {
		t.Fatal("dieselbe Kennung beendet ein zweites Mal")
	}
	if l.EndAdmin("erfunden") || l.Load().Total != 1 {
		t.Fatal("erfundene Kennung beendet etwas")
	}
	if ld := l.Load(); ld.Transcodes != 1 || ld.Last != "mittel" {
		t.Fatalf("Last falsch: %+v", ld)
	}
}
