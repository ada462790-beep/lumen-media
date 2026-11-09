// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Bestehende Daten: „0" hiess bisher „ohne Grenze". Nach dem Umbau steht
// dort OhneAltersgrenze — niemand sieht nach dem Update plötzlich nur FSK 0.
func TestAltersgrenzeWirdUmgestellt(t *testing.T) {
	pfad := filepath.Join(t.TempDir(), "lumen.db")
	st, err := Open(pfad)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	besitzer, _ := st.CreateAccount(ctx, NewAccount{Name: "Besitzer", IsOwner: true, LockKind: "password", Secret: "geheimgeheim"})
	frei, _ := st.CreateAccount(ctx, NewAccount{Name: "Frei"})
	streng, _ := st.CreateAccount(ctx, NewAccount{Name: "Streng"})
	mustExec(t, st, `UPDATE account_limit SET max_age = 0 WHERE account_id = '`+frei.ID+`'`)
	mustExec(t, st, `UPDATE account_limit SET max_age = 12 WHERE account_id = '`+streng.ID+`'`)
	mustExec(t, st, `INSERT INTO gruppe (id, name, verwaltung, limits) VALUES ('g1','Alle',0,'{"maxAge":0,"maxStreams":3}')`)
	if _, err := st.CreateInvite(ctx, besitzer.ID, time.Hour, map[string]any{
		"limits": map[string]any{"maxAge": 0, "maxStreams": 2}}); err != nil {
		t.Fatal(err)
	}
	mustExec(t, st, `PRAGMA user_version = 4`)
	_ = st.Close()

	st, err = Open(pfad)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if l, _ := st.Limits(ctx, frei.ID); l.MaxAge != OhneAltersgrenze {
		t.Fatalf("„alle“ wurde zu %d", l.MaxAge)
	}
	if l, _ := st.Limits(ctx, streng.ID); l.MaxAge != 12 {
		t.Fatalf("12 wurde zu %d", l.MaxAge)
	}
	if g, _ := st.Gruppe(ctx, "g1"); g.Limits.MaxAge != OhneAltersgrenze || g.Limits.MaxStreams != 3 {
		t.Fatalf("Gruppe: %+v", g.Limits)
	}
	var preset string
	_ = st.DB().QueryRow(`SELECT preset FROM invite`).Scan(&preset)
	var p struct{ Limits Limits }
	_ = json.Unmarshal([]byte(preset), &p)
	if p.Limits.MaxAge != OhneAltersgrenze {
		t.Fatalf("Einladung: %s", preset)
	}
}

// „Nur FSK 0" lässt sich einstellen — und meint nicht „alles".
func TestNurFSK0(t *testing.T) {
	st := testStore(t)
	ctx := t.Context()
	lib, _ := st.CreateLibrary(ctx, "Filme", "movie", t.TempDir(), "de")
	for i, a := range []*int{ptr(0), ptr(6), nil} {
		_ = st.UpsertItem(ctx, &Item{LibraryID: lib.ID, Kind: "movie", Title: string(rune('A' + i)),
			AgeYears: a, MatchState: "matched"})
	}
	got, err := st.Query(ctx, Query{Libraries: []string{lib.ID}, MaxAge: 0, MitAltersgrenze: true, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].AgeYears == nil || *got[0].AgeYears != 0 {
		t.Fatalf("FSK 0 sieht %d Titel", len(got))
	}
	if err := PruefeGrenzen(Limits{MaxAge: 7, MaxStreams: 1}); err == nil {
		t.Fatal("Altersgrenze 7 angenommen")
	}
	if err := PruefeGrenzen(Limits{MaxAge: 0, MaxStreams: 0}); err == nil {
		t.Fatal("0 gleichzeitig angenommen")
	}
	if err := PruefeGrenzen(Limits{MaxAge: OhneAltersgrenze, MaxStreams: 2, RemoteQuality: "720p"}); err != nil {
		t.Fatal(err)
	}
}

// Hat eine Folge eine eigene, strengere Freigabe als ihre Serie, zählt die.
func TestFolgeMitEigenerFreigabe(t *testing.T) {
	st := testStore(t)
	ctx := t.Context()
	lib, _ := st.CreateLibrary(ctx, "Serien", "series", t.TempDir(), "de")
	serie := &Item{LibraryID: lib.ID, Kind: "series", Title: "Sanft", AgeYears: ptr(6), MatchState: "matched"}
	_ = st.UpsertItem(ctx, serie)
	staffel := &Item{LibraryID: lib.ID, Kind: "season", Title: "Staffel 1", ParentID: serie.ID, MatchState: "matched"}
	_ = st.UpsertItem(ctx, staffel)
	_ = st.UpsertItem(ctx, &Item{LibraryID: lib.ID, Kind: "episode", Title: "Harte Folge", ParentID: staffel.ID,
		AgeYears: ptr(16), MatchState: "matched"})
	_ = st.UpsertItem(ctx, &Item{LibraryID: lib.ID, Kind: "episode", Title: "Normale Folge", ParentID: staffel.ID,
		MatchState: "matched"})
	got, _ := st.Query(ctx, Query{Libraries: []string{lib.ID}, Kinds: []string{"episode"}, MaxAge: 12,
		MitAltersgrenze: true, Limit: 10})
	if len(got) != 1 || got[0].Title != "Normale Folge" {
		t.Fatalf("Kind mit 12 sieht: %v", titel(got))
	}
}

// Gruppe auflösen oder verlassen öffnet nie mehr, als in der Gruppe galt.
func TestGruppeVerlassenNieLockerer(t *testing.T) {
	for _, weg := range []string{"auflösen", "herausnehmen"} {
		t.Run(weg, func(t *testing.T) {
			st := testStore(t)
			ctx := t.Context()
			a, _ := st.CreateLibrary(ctx, "Kinderfilme", "movie", t.TempDir(), "de")
			b, _ := st.CreateLibrary(ctx, "Horror", "movie", t.TempDir(), "de")
			// Eigene Einträge: alles, wie eine Einladung sie früher anlegte.
			kind, err := st.CreateAccount(ctx, NewAccount{Name: "Lea", Grants: []string{a.ID, b.ID}})
			if err != nil {
				t.Fatal(err)
			}
			g, err := st.GruppeAnlegen(ctx, Gruppe{Name: "Kinder", Libraries: []string{a.ID},
				Limits: Limits{MaxAge: 6, MaxStreams: 1, RemoteQuality: "720p"}})
			if err != nil {
				t.Fatal(err)
			}
			if err := st.GruppeZuweisen(ctx, kind.ID, g.ID); err != nil {
				t.Fatal(err)
			}
			if weg == "auflösen" {
				err = st.GruppeLoeschen(ctx, g.ID)
			} else {
				err = st.GruppeZuweisen(ctx, kind.ID, "")
			}
			if err != nil {
				t.Fatal(err)
			}
			l, _ := st.Limits(ctx, kind.ID)
			if l.MaxAge != 6 || l.AllowUnrated || l.MaxStreams != 1 || l.AllowDownload || l.RemoteQuality != "720p" {
				t.Fatalf("Grenzen nach dem %s: %+v", weg, l)
			}
			libs, _ := st.GrantedLibraries(ctx, kind.ID)
			if len(libs) != 1 || libs[0] != a.ID {
				t.Fatalf("Bibliotheken nach dem %s: %v", weg, libs)
			}
		})
	}
}

// Ein Kinderprofil in einer lockeren Gruppe bleibt bei seinen Grenzen.
func TestKindInLockererGruppe(t *testing.T) {
	st := testStore(t)
	ctx := t.Context()
	kinder, _ := KinderGrenzen(6)
	kind, err := st.CreateAccount(ctx, NewAccount{Name: "Ben", IsKid: true, Limits: kinder})
	if err != nil {
		t.Fatal(err)
	}
	g, _ := st.GruppeAnlegen(ctx, Gruppe{Name: "Familie", Limits: Limits{MaxAge: OhneAltersgrenze, MaxStreams: 4, AllowUnrated: true}})
	_ = st.GruppeZuweisen(ctx, kind.ID, g.ID)
	if l, _ := st.Limits(ctx, kind.ID); l.MaxAge != 6 || l.AllowUnrated {
		t.Fatalf("Kind in der Familie: %+v", l)
	}
	if _, err := st.CreateAccount(ctx, NewAccount{Name: "Mia", IsKid: true, Limits: DefaultLimits(false)}); err == nil {
		t.Fatal("Kinderprofil mit Erwachsenen-Grenzen angelegt")
	}
}

// Einladung als Kinderprofil: Kinder-Vorgaben, erkennbar als Kind.
func TestEinladungAlsKinderprofil(t *testing.T) {
	st := testStore(t)
	ctx := t.Context()
	besitzer, _ := st.CreateAccount(ctx, NewAccount{Name: "Besitzer", IsOwner: true})
	lim, _ := KinderGrenzen(0)
	inv, _ := st.CreateInvite(ctx, besitzer.ID, time.Hour, map[string]any{"limits": lim, "kinderprofil": true})
	acc, err := st.RedeemInvite(ctx, inv.Code, "Nina", "none", "")
	if err != nil {
		t.Fatal(err)
	}
	if !acc.IsKid {
		t.Fatal("eingeladenes Kind ist kein Kinderprofil")
	}
	if l, _ := st.Limits(ctx, acc.ID); l.MaxAge != 0 || l.AllowUnrated || l.AllowDownload {
		t.Fatalf("Grenzen des Kindes: %+v", l)
	}
	// Ohne Angaben: Erwachsener wie bisher.
	inv, _ = st.CreateInvite(ctx, besitzer.ID, time.Hour, map[string]any{})
	gross, _ := st.RedeemInvite(ctx, inv.Code, "Tom", "none", "")
	if gross.IsKid {
		t.Fatal("Einladung ohne Kinderprofil legt ein Kind an")
	}
}

// Namen sind eindeutig — ohne Rücksicht auf Gross/Klein.
func TestNamenEindeutig(t *testing.T) {
	st := testStore(t)
	ctx := t.Context()
	alex, _ := st.CreateAccount(ctx, NewAccount{Name: "Alex", IsOwner: true})
	if _, err := st.CreateAccount(ctx, NewAccount{Name: " alex "}); !errors.Is(err, ErrNameVergeben) {
		t.Fatalf("zweite „alex“: %v", err)
	}
	if _, err := st.CreateAccount(ctx, NewAccount{Name: "Ändi"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateAccount(ctx, NewAccount{Name: "äNDI"}); !errors.Is(err, ErrNameVergeben) {
		t.Fatalf("„äNDI“: %v", err)
	}
	lea, _ := st.CreateAccount(ctx, NewAccount{Name: "Lea"})
	if err := st.RenameAccount(ctx, lea.ID, "ALEX", "av-a"); !errors.Is(err, ErrNameVergeben) {
		t.Fatalf("umbenennen: %v", err)
	}
	if err := st.RenameAccount(ctx, alex.ID, "alex", "av-a"); err != nil {
		t.Fatalf("eigenen Namen anders schreiben: %v", err)
	}
	besitzer, _ := st.CreateAccount(ctx, NewAccount{Name: "Besitzerin"})
	inv, _ := st.CreateInvite(ctx, besitzer.ID, time.Hour, map[string]any{})
	if _, err := st.RedeemInvite(ctx, inv.Code, "LEA", "none", ""); !errors.Is(err, ErrNameVergeben) {
		t.Fatalf("Einladung mit vergebenem Namen: %v", err)
	}
}

// Verwaltung nur mit PIN oder Passwort — und nie für Kinder.
func TestVerwaltungBrauchtSperre(t *testing.T) {
	st := testStore(t)
	ctx := t.Context()
	offen, _ := st.CreateAccount(ctx, NewAccount{Name: "Offen"})
	if err := st.RolleSetzen(ctx, offen.ID, "verwalten"); !errors.Is(err, ErrSperreNoetig) {
		t.Fatalf("Rolle ohne PIN: %v", err)
	}
	g, _ := st.GruppeAnlegen(ctx, Gruppe{Name: "Helfer", Verwaltung: true})
	if err := st.GruppeZuweisen(ctx, offen.ID, g.ID); !errors.Is(err, ErrSperreNoetig) {
		t.Fatalf("Gruppe mit Verwaltung ohne PIN: %v", err)
	}
	normal, _ := st.GruppeAnlegen(ctx, Gruppe{Name: "Familie"})
	_ = st.GruppeZuweisen(ctx, offen.ID, normal.ID)
	if err := st.GruppeAendern(ctx, Gruppe{ID: normal.ID, Name: "Familie", Verwaltung: true}); !errors.Is(err, ErrSperreNoetig) {
		t.Fatalf("Gruppe hochstufen mit offenem Mitglied: %v", err)
	}

	mit, _ := st.CreateAccount(ctx, NewAccount{Name: "Mit", LockKind: "pin", Secret: "2580"})
	if err := st.RolleSetzen(ctx, mit.ID, "voll"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetSecret(ctx, mit.ID, "none", ""); !errors.Is(err, ErrSperreNoetig) {
		t.Fatalf("Sperre entfernen mit Verwaltung: %v", err)
	}
	if err := st.SetSecret(ctx, mit.ID, "pin", "12ab"); err == nil {
		t.Fatal("PIN mit Buchstaben angenommen")
	}
	if err := st.SetSecret(ctx, mit.ID, "pin", "9876"); err != nil || st.CheckSecret(ctx, mit.ID, "9876") != nil {
		t.Fatalf("neue PIN: %v", err)
	}

	kinder, _ := KinderGrenzen(12)
	kind, _ := st.CreateAccount(ctx, NewAccount{Name: "Kind", IsKid: true, Limits: kinder, LockKind: "pin", Secret: "1111"})
	if err := st.RolleSetzen(ctx, kind.ID, "verwalten"); !errors.Is(err, ErrKindOhneVerwaltung) {
		t.Fatalf("Kind bekommt Verwaltung: %v", err)
	}
}

// Störungen beim Abspielen landen gezählt und ohne Uhrzeit im Protokoll.
func TestStoerungenNurAlsTageszahl(t *testing.T) {
	st := testStore(t)
	ctx := t.Context()
	st.Log(ctx, "error", "stream", "Umwandlung gescheitert")
	st.Log(ctx, "error", "stream", "Umwandlung gescheitert")
	zeilen, _ := st.RecentLog(ctx, "all", 50)
	for _, z := range zeilen {
		if z.Area == "stream" {
			t.Fatalf("Störung von heute steht schon einzeln da: %+v", z)
		}
	}
	// Ein Tag vergeht.
	st.stoerungen.mu.Lock()
	st.stoerungen.tag -= 86400
	gestern := st.stoerungen.tag
	st.stoerungen.mu.Unlock()
	zeilen, _ = st.RecentLog(ctx, "all", 50)
	n := 0
	for _, z := range zeilen {
		if z.Area == "stream" {
			n++
			if z.At.Unix() != gestern || !strings.Contains(z.Message, "2-mal") {
				t.Fatalf("Zusammenfassung falsch: %+v", z)
			}
		}
	}
	if n != 1 {
		t.Fatalf("%d Zeilen statt einer Zusammenfassung", n)
	}
}

func ptr(n int) *int { return &n }

func titel(items []Item) []string {
	out := []string{}
	for _, it := range items {
		out = append(out, it.Title)
	}
	return out
}

// Wer verwaltet, hat keine Grenzen — der Besitzer sah sonst „höchstens 2
// gleichzeitig" bei sich selbst.
func TestVerwaltungOhneGrenzen(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	besitzer, err := st.CreateAccount(ctx, NewAccount{Name: "Besitzerin", IsOwner: true})
	if err != nil {
		t.Fatal(err)
	}
	lim, err := st.Limits(ctx, besitzer.ID)
	if err != nil {
		t.Fatal(err)
	}
	if lim.MaxStreams < 20 || lim.MaxAge != OhneAltersgrenze || lim.RemoteQuality != "original" {
		t.Errorf("Besitzer hat Grenzen: %+v", lim)
	}
}

// Kinder aus älteren Fassungen werden beim Umbau zu Kinderprofilen —
// Erwachsene ohne Grenze und Verwaltende nicht.
func TestKinderErkennen(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	kind, _ := st.CreateAccount(ctx, NewAccount{Name: "Max"})
	gross, _ := st.CreateAccount(ctx, NewAccount{Name: "Oma"})
	lk := DefaultLimits(false)
	lk.MaxAge = 12
	if err := st.SetLimits(ctx, kind.ID, lk); err != nil {
		t.Fatal(err)
	}
	lg := DefaultLimits(false)
	lg.MaxAge = OhneAltersgrenze
	if err := st.SetLimits(ctx, gross.ID, lg); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(`UPDATE account SET is_kid = 0`); err != nil {
		t.Fatal(err)
	}
	if err := kinderErkennen(st.db); err != nil {
		t.Fatal(err)
	}
	k, _ := st.Account(ctx, kind.ID)
	g, _ := st.Account(ctx, gross.ID)
	if !k.IsKid || g.IsKid {
		t.Errorf("Max Kind=%v, Oma Kind=%v", k.IsKid, g.IsKid)
	}
}
