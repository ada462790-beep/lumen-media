// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func zuerich(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Zurich")
	if err != nil {
		t.Skip("keine Zeitzonen-Daten:", err)
	}
	return loc
}

// 2026-11-09 ist ein Montag, 2026-11-13 ein Freitag, 2026-11-14 ein Samstag.
func um(loc *time.Location, tag, zeit string) time.Time {
	t, err := time.ParseInLocation("2006-01-02 15:04", tag+" "+zeit, loc)
	if err != nil {
		panic(err)
	}
	return t
}

func regelSchule() Bildschirmzeit {
	return Bildschirmzeit{Aktiv: true,
		Schultage:  Tagesregel{Von: "07:00", Bis: "19:30", Minuten: 90},
		Wochenende: Tagesregel{Von: "08:00", Bis: "20:30", Minuten: 120}}
}

func TestBildschirmzeitFensterSchultagWochenende(t *testing.T) {
	loc := zuerich(t)
	b := regelSchule()
	for _, f := range []struct {
		tag, zeit string
		erlaubt   bool
		weiter    string // erwarteter Beginn, wenn nicht erlaubt
	}{
		{"2026-11-09", "06:59", false, "2026-11-09 07:00"},
		{"2026-11-09", "07:00", true, ""},
		{"2026-11-09", "19:29", true, ""},
		{"2026-11-09", "19:30", false, "2026-11-10 07:00"},
		{"2026-11-13", "21:00", false, "2026-11-14 08:00"}, // Freitag → Samstag
		{"2026-11-14", "07:30", false, "2026-11-14 08:00"}, // Samstag früh
		{"2026-11-14", "20:00", true, ""},                  // Samstag später als Schultag
		{"2026-11-15", "22:00", false, "2026-11-16 07:00"}, // Sonntag → Montag
	} {
		u := b.Pruefen(um(loc, f.tag, f.zeit), 0, 0)
		if u.Erlaubt != f.erlaubt {
			t.Errorf("%s %s: erlaubt=%v, erwartet %v", f.tag, f.zeit, u.Erlaubt, f.erlaubt)
			continue
		}
		if !f.erlaubt {
			if u.Grund != "fenster" {
				t.Errorf("%s %s: Grund %q", f.tag, f.zeit, u.Grund)
			}
			if got := u.WeiterAb.Format("2006-01-02 15:04"); got != f.weiter {
				t.Errorf("%s %s: weiter ab %s, erwartet %s", f.tag, f.zeit, got, f.weiter)
			}
		}
	}
}

func TestBildschirmzeitTagesgrenze(t *testing.T) {
	loc := zuerich(t)
	b := regelSchule()
	jetzt := um(loc, "2026-11-09", "16:00")
	u := b.Pruefen(jetzt, 60*time.Minute, 0)
	if !u.Erlaubt || u.Rest != 30*time.Minute || u.Budget != 90*time.Minute {
		t.Fatalf("nach 60 von 90 Minuten: %+v", u)
	}
	// Kurz vor Fensterende zählt das Fenster, nicht das Budget.
	u = b.Pruefen(um(loc, "2026-11-09", "19:20"), 0, 0)
	if !u.Erlaubt || u.Rest != 10*time.Minute {
		t.Fatalf("Rest bis Fensterende: %+v", u)
	}
	u = b.Pruefen(jetzt, 90*time.Minute, 0)
	if u.Erlaubt || u.Grund != "budget" {
		t.Fatalf("nach 90 von 90: %+v", u)
	}
	if got := u.WeiterAb.Format("2006-01-02 15:04"); got != "2026-11-10 07:00" {
		t.Errorf("morgen ab 07:00 erwartet, bekommen %s", got)
	}
	// Geschenkte Minuten verlängern die Tagesgrenze.
	u = b.Pruefen(jetzt, 90*time.Minute, 30)
	if !u.Erlaubt || u.Rest != 30*time.Minute {
		t.Fatalf("mit 30 geschenkten: %+v", u)
	}
	// Ohne Fenster, nur Budget: nach dem Aufbrauchen geht es um Mitternacht weiter.
	nurBudget := Bildschirmzeit{Aktiv: true, Schultage: Tagesregel{Minuten: 60}, Wochenende: Tagesregel{Minuten: 60}}
	u = nurBudget.Pruefen(um(loc, "2026-11-09", "22:00"), time.Hour, 0)
	if u.Erlaubt || u.WeiterAb.Format("2006-01-02 15:04") != "2026-11-10 00:00" {
		t.Fatalf("Mitternacht: %+v", u)
	}
	// Ganz ohne Grenze: Rest unbegrenzt.
	if u := nurBudget.Pruefen(um(loc, "2026-11-09", "22:00"), 0, 0); !u.Erlaubt || u.Rest != time.Hour {
		t.Fatalf("Rest nur Budget: %+v", u)
	}
	aus := regelSchule()
	aus.Aktiv = false
	if u := aus.Pruefen(um(loc, "2026-11-09", "03:00"), 10*time.Hour, 0); !u.Erlaubt || u.Rest >= 0 {
		t.Fatalf("ausgeschaltet: %+v", u)
	}
}

func TestBildschirmzeitUeberMitternacht(t *testing.T) {
	loc := zuerich(t)
	b := Bildschirmzeit{Aktiv: true,
		Schultage:  Tagesregel{Von: "18:00", Bis: "02:00"},
		Wochenende: Tagesregel{Von: "10:00", Bis: "12:00"}}
	for _, f := range []struct {
		tag, zeit string
		erlaubt   bool
	}{
		{"2026-11-09", "17:59", false},
		{"2026-11-09", "18:00", true},
		{"2026-11-09", "23:59", true},
		{"2026-11-10", "01:59", true}, // Montagsfenster nach Mitternacht
		{"2026-11-10", "02:00", false},
		{"2026-11-14", "01:00", true},  // Samstag 01:00: noch Freitagsfenster
		{"2026-11-14", "18:30", false}, // Samstag gilt das Wochenende
		{"2026-11-16", "01:00", false}, // Montag 01:00: Sonntag hatte kein Nachtfenster
	} {
		if u := b.Pruefen(um(loc, f.tag, f.zeit), 0, 0); u.Erlaubt != f.erlaubt {
			t.Errorf("%s %s: erlaubt=%v, erwartet %v", f.tag, f.zeit, u.Erlaubt, f.erlaubt)
		}
	}
	if u := b.Pruefen(um(loc, "2026-11-09", "23:00"), 0, 0); u.Rest != 3*time.Hour {
		t.Errorf("Rest über Mitternacht: %v", u.Rest)
	}
}

func TestBildschirmzeitSommerzeit(t *testing.T) {
	loc := zuerich(t)
	// 2026-03-29: Umstellung auf Sommerzeit (02:00 → 03:00), ein Sonntag.
	b := Bildschirmzeit{Aktiv: true,
		Schultage: Tagesregel{Von: "07:00", Bis: "19:30"}, Wochenende: Tagesregel{Von: "08:00", Bis: "20:30"}}
	u := b.Pruefen(um(loc, "2026-03-29", "21:00"), 0, 0)
	if u.Erlaubt || u.WeiterAb.Format("2006-01-02 15:04 -0700") != "2026-03-30 07:00 +0200" {
		t.Fatalf("nach der Umstellung: %+v", u)
	}
	// Dieselbe Regel in einer anderen Zone gilt in deren Ortszeit.
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip(err)
	}
	if u := b.Pruefen(time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC).In(ny), 0, 0); !u.Erlaubt {
		t.Error("07:00 New York ist im Fenster")
	}
}

func TestPruefeBildschirmzeit(t *testing.T) {
	gut := regelSchule()
	if _, err := PruefeBildschirmzeit(gut); err != nil {
		t.Fatal(err)
	}
	ganz := Bildschirmzeit{Aktiv: true, Schultage: Tagesregel{Von: "00:00", Bis: "24:00", Minuten: 60}}
	b, err := PruefeBildschirmzeit(ganz)
	if err != nil || b.Schultage.Von != "" || b.Schultage.Bis != "" {
		t.Fatalf("00:00–24:00 wird zu ganzer Tag: %+v %v", b, err)
	}
	for _, schlecht := range []Tagesregel{
		{Von: "7:00", Bis: "19:00"}, {Von: "07:00", Bis: ""}, {Von: "25:00", Bis: "26:00"},
		{Von: "08:00", Bis: "08:00"}, {Von: "24:00", Bis: "08:00"}, {Von: "07:60", Bis: "08:00"},
		{Minuten: -1}, {Minuten: 2000},
	} {
		if _, err := PruefeBildschirmzeit(Bildschirmzeit{Aktiv: true, Schultage: schlecht}); err == nil {
			t.Errorf("%+v sollte abgelehnt werden", schlecht)
		}
	}
}

func TestBildschirmzeitSpeicher(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "l.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	owner, _ := st.CreateAccount(ctx, NewAccount{Name: "Besitz", IsOwner: true, LockKind: "password", Secret: "einsehrgutespasswort"})
	_ = owner
	kind, err := st.CreateAccount(ctx, NewAccount{Name: "Kind", IsKid: true})
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := st.BildschirmzeitRegel(ctx, kind.ID); b.Aktiv {
		t.Fatal("ohne Regel ist sie aus")
	}
	if err := st.SetBildschirmzeit(ctx, kind.ID, regelSchule()); err != nil {
		t.Fatal(err)
	}
	if b, _ := st.BildschirmzeitRegel(ctx, kind.ID); !b.Aktiv || b.Schultage.Minuten != 90 {
		t.Fatalf("gelesen: %+v", b)
	}
	if err := st.SetBildschirmzeit(ctx, kind.ID, Bildschirmzeit{Aktiv: true, Schultage: Tagesregel{Von: "x"}}); err == nil {
		t.Fatal("ungültige Regel gespeichert")
	}

	// Zählen: gestern verschwindet, heute summiert sich.
	_ = st.BildschirmzeitZaehlen(ctx, kind.ID, "2026-11-09", 600)
	_ = st.BildschirmzeitZaehlen(ctx, kind.ID, "2026-11-10", 60)
	_ = st.BildschirmzeitZaehlen(ctx, kind.ID, "2026-11-10", 60)
	if g, _, _ := st.BildschirmzeitHeute(ctx, kind.ID, "2026-11-10"); g != 2*time.Minute {
		t.Fatalf("heute: %v", g)
	}
	if g, _, _ := st.BildschirmzeitHeute(ctx, kind.ID, "2026-11-09"); g != 0 {
		t.Fatalf("gestern ist weg: %v", g)
	}
	var n int
	_ = st.DB().QueryRow(`SELECT count(*) FROM bildschirmzeit_tag WHERE account_id = ?`, kind.ID).Scan(&n)
	if n != 1 {
		t.Fatalf("%d Zeilen, erwartet eine", n)
	}
	if neu, err := st.BildschirmzeitSchenken(ctx, kind.ID, "2026-11-10", 30); err != nil || neu != 30 {
		t.Fatalf("schenken: %d %v", neu, err)
	}
	if neu, _ := st.BildschirmzeitSchenken(ctx, kind.ID, "2026-11-10", MaxGeschenkt); neu != MaxGeschenkt {
		t.Fatalf("Obergrenze: %d", neu)
	}
	if g, gs, _ := st.BildschirmzeitHeute(ctx, kind.ID, "2026-11-10"); g != 2*time.Minute || gs != MaxGeschenkt {
		t.Fatalf("nach schenken: %v %d", g, gs)
	}
	// Am nächsten Tag ist alles frisch.
	_ = st.BildschirmzeitAufraeumen(ctx, "2026-11-11")
	if g, gs, _ := st.BildschirmzeitHeute(ctx, kind.ID, "2026-11-10"); g != 0 || gs != 0 {
		t.Fatalf("aufgeräumt: %v %d", g, gs)
	}
	// Konto weg → Regel und Zahlen weg.
	_ = st.BildschirmzeitZaehlen(ctx, kind.ID, "2026-11-11", 60)
	if err := st.DeleteAccount(ctx, kind.ID); err != nil {
		t.Fatal(err)
	}
	for _, tbl := range []string{"bildschirmzeit_regel", "bildschirmzeit_tag"} {
		_ = st.DB().QueryRow(`SELECT count(*) FROM ` + tbl).Scan(&n)
		if n != 0 {
			t.Errorf("%s: %d Zeilen nach dem Löschen", tbl, n)
		}
	}
}

// Ein Nachtfenster zählt bis zu seinem Ende zum Tag, an dem es begann:
// um Mitternacht gibt es kein zweites Tagesbudget.
func TestBildschirmzeitNachtfensterEinBudget(t *testing.T) {
	loc := zuerich(t)
	b := Bildschirmzeit{Aktiv: true,
		Schultage:  Tagesregel{Von: "18:00", Bis: "02:00", Minuten: 90},
		Wochenende: Tagesregel{Von: "18:00", Bis: "02:00", Minuten: 120}}
	// Freitag → Samstag: 90 Minuten bis 23:59 verbraucht.
	fr := um(loc, "2026-11-13", "23:59")
	if got := b.Schautag(fr); got != "2026-11-13" {
		t.Fatalf("Schautag Freitag: %s", got)
	}
	sa := um(loc, "2026-11-14", "00:01")
	if got := b.Schautag(sa); got != "2026-11-13" {
		t.Fatalf("Samstag 00:01 gehört zum Freitag: %s", got)
	}
	u := b.Pruefen(sa, 90*time.Minute, 0)
	if u.Erlaubt || u.Grund != "budget" || u.Budget != 90*time.Minute {
		t.Fatalf("Samstag 00:01 nach 90 Minuten am Freitag: %+v", u)
	}
	if got := u.WeiterAb.Format("2006-01-02 15:04"); got != "2026-11-14 18:00" {
		t.Errorf("weiter ab %s, erwartet Samstag 18:00", got)
	}
	// Nach dem Ende des Fensters beginnt der Samstag.
	if got := b.Schautag(um(loc, "2026-11-14", "02:00")); got != "2026-11-14" {
		t.Errorf("Samstag 02:00: %s", got)
	}
	// Sonntag → Montag: Nach Mitternacht gilt noch das Wochenende (120).
	mo := um(loc, "2026-11-16", "01:00")
	if got := b.Schautag(mo); got != "2026-11-15" {
		t.Fatalf("Montag 01:00 gehört zum Sonntag: %s", got)
	}
	if u := b.Pruefen(mo, 100*time.Minute, 0); !u.Erlaubt || u.Budget != 120*time.Minute || u.Rest != 20*time.Minute {
		t.Fatalf("Montag 01:00 mit 100 Minuten vom Sonntag: %+v", u)
	}
	if u := b.Pruefen(mo, 120*time.Minute, 0); u.Erlaubt || u.WeiterAb.Format("2006-01-02 15:04") != "2026-11-16 18:00" {
		t.Fatalf("Sonntagsbudget aufgebraucht: %+v", u)
	}
	// Ganzer Tag am Samstag, Freitag mit Nachtfenster: bis 02:00 zählt der
	// Freitag; danach ist der Samstag frei.
	c := Bildschirmzeit{Aktiv: true,
		Schultage:  Tagesregel{Von: "18:00", Bis: "02:00", Minuten: 90},
		Wochenende: Tagesregel{Minuten: 60}}
	u = c.Pruefen(um(loc, "2026-11-14", "00:30"), 90*time.Minute, 0)
	if u.Erlaubt || u.WeiterAb.Format("2006-01-02 15:04") != "2026-11-14 02:00" {
		t.Fatalf("ganzer Samstag nach Freitagsfenster: %+v", u)
	}
}

// Umstellung auf Sommerzeit: Ein Fenster bis 02:30 endet um 03:00 (der
// Augenblick der Umstellung), nicht erst um 03:30.
func TestBildschirmzeitSommerzeitLuecke(t *testing.T) {
	loc := zuerich(t)
	// 2027-03-28 ist ein Sonntag mit Umstellung um 02:00.
	b := Bildschirmzeit{Aktiv: true,
		Schultage:  Tagesregel{Von: "07:00", Bis: "19:30"},
		Wochenende: Tagesregel{Von: "20:00", Bis: "02:30"}}
	if u := b.Pruefen(um(loc, "2027-03-28", "01:59"), 0, 0); !u.Erlaubt || u.Rest != time.Minute {
		t.Fatalf("01:59 vor der Umstellung: %+v", u)
	}
	if u := b.Pruefen(time.Date(2027, 3, 28, 3, 10, 0, 0, loc), 0, 0); u.Erlaubt {
		t.Fatalf("03:10 nach der Umstellung noch im Fenster: %+v", u)
	}
	if got := tagUm(time.Date(2027, 3, 28, 12, 0, 0, 0, loc), 150); got.Format("15:04 -0700") != "03:00 +0200" {
		t.Fatalf("02:30 in der Lücke: %s", got)
	}
}

// Eine Regel, die sich nicht lesen lässt, meldet einen Fehler — sie gilt
// nicht still als „keine Regel".
func TestBildschirmzeitRegelUnlesbar(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "l.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	kind, _ := st.CreateAccount(ctx, NewAccount{Name: "Kind", IsKid: true})
	if _, err := st.DB().Exec(`INSERT INTO bildschirmzeit_regel (account_id, regel) VALUES (?, '{kaputt')`, kind.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.BildschirmzeitRegel(ctx, kind.ID); err != ErrRegelUnlesbar {
		t.Fatalf("kaputte Regel: %v", err)
	}
}
