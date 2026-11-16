// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

import (
	"context"
	"strings"
	"testing"
)

// Welche Tabelle eine Anweisung schreibt — danach richten sich
// StandMediathek und der Stand des Fortschritts.
func TestZielTabelle(t *testing.T) {
	for q, soll := range map[string]string{
		`INSERT INTO progress (account_id) VALUES (?)`:                      "progress",
		`INSERT OR REPLACE INTO media_track (file_id) VALUES (?)`:           "media_track",
		`UPDATE item SET title = ? WHERE id = ?`:                            "item",
		`UPDATE OR IGNORE playlist_item SET item_id = ?`:                    "playlist_item",
		`DELETE FROM watchlist WHERE account_id = ?`:                        "watchlist",
		"WITH x AS (SELECT 1) INSERT INTO\n  rating(account_id) VALUES (?)": "rating",
		`REPLACE INTO setting (key, value) VALUES (?, ?)`:                   "setting",
		`PRAGMA user_version = 18`:                                          "",
	} {
		if got := zielTabelle(q); got != soll {
			t.Errorf("%q: %q, erwartet %q", q, got, soll)
		}
	}
}

// Schreiben im Hintergrund rückt StandHart nicht weiter, Schreiben in die
// Tabellen eines Profils nicht StandMediathek — und der Fortschritt hat
// seinen eigenen Stand.
func TestStaendeJeArt(t *testing.T) {
	st := testStore(t)
	ctx := t.Context()
	lib, _ := st.CreateLibrary(ctx, "Filme", "movie", t.TempDir(), "de")
	film := &Item{LibraryID: lib.ID, Kind: "movie", Title: "Film"}
	if err := st.UpsertItem(ctx, film); err != nil {
		t.Fatal(err)
	}
	acc, err := st.CreateAccount(ctx, NewAccount{Name: "Eins", IsOwner: true})
	if err != nil {
		t.Fatal(err)
	}
	type staende struct{ alle, hart, med, fort int64 }
	jetzt := func() staende {
		return staende{Stand(), StandHart(), StandMediathek(), standFortschrittJetzt()}
	}

	a := jetzt()
	film.Title = "Film (neu)"
	if err := st.UpsertItem(Hintergrund(ctx), film); err != nil {
		t.Fatal(err)
	}
	b := jetzt()
	if b.alle == a.alle || b.hart != a.hart || b.med == a.med || b.fort != a.fort {
		t.Fatalf("Metadaten im Hintergrund: %+v → %+v", a, b)
	}
	if err := st.SaveProgress(ctx, acc.ID, film.ID, 120000, 5400000); err != nil {
		t.Fatal(err)
	}
	c := jetzt()
	if c.alle == b.alle || c.hart == b.hart || c.med != b.med || c.fort == b.fort {
		t.Fatalf("Fortschritt: %+v → %+v", b, c)
	}
	if err := st.AddToWatchlist(ctx, acc.ID, film.ID); err != nil {
		t.Fatal(err)
	}
	d := jetzt()
	if d.hart == c.hart || d.med != c.med || d.fort != c.fort {
		t.Fatalf("Merkliste: %+v → %+v", c, d)
	}
	// Ein Titel weg: sein Fortschritt geht mit (ON DELETE CASCADE).
	if _, err := st.DB().ExecContext(ctx, `DELETE FROM item WHERE id = ?`, film.ID); err != nil {
		t.Fatal(err)
	}
	if e := jetzt(); e.fort == d.fort || e.med == d.med {
		t.Fatalf("Titel gelöscht: %+v → %+v", d, e)
	}
}

// Das Abbild des eigenen Fortschritts folgt jedem Schreiben sofort.
func TestAbbildFolgtDemFortschritt(t *testing.T) {
	st := testStore(t)
	ctx := t.Context()
	lib, _ := st.CreateLibrary(ctx, "Filme", "movie", t.TempDir(), "de")
	film := &Item{LibraryID: lib.ID, Kind: "movie", Title: "Film"}
	_ = st.UpsertItem(ctx, film)
	acc, _ := st.CreateAccount(ctx, NewAccount{Name: "Eins", IsOwner: true})
	anderes, _ := st.CreateAccount(ctx, NewAccount{Name: "Zwei"})

	stand := func(konto string) (Progress, bool) {
		t.Helper()
		m, err := st.ProgressFor(ctx, konto, []string{film.ID})
		if err != nil {
			t.Fatal(err)
		}
		p, ok := m[film.ID]
		return p, ok
	}
	if _, ok := stand(acc.ID); ok {
		t.Fatal("Stand vor dem Schauen")
	}
	_ = st.SaveProgress(ctx, acc.ID, film.ID, 300000, 5400000)
	if p, ok := stand(acc.ID); !ok || p.PositionMS != 300000 || p.Finished {
		t.Fatalf("nach dem Schauen: %+v %v", p, ok)
	}
	if _, ok := stand(anderes.ID); ok {
		t.Fatal("das andere Profil sieht den Stand")
	}
	_ = st.MarkFinished(ctx, acc.ID, film.ID, 5400000)
	if f, _ := st.FinishedIDs(ctx, acc.ID, []string{film.ID}); !f[film.ID] {
		t.Fatal("Haken fehlt im Abbild")
	}
	_ = st.ClearProgress(ctx, acc.ID, film.ID)
	if _, ok := stand(acc.ID); ok {
		t.Fatal("Stand nach dem Zurücknehmen noch da")
	}
}

// Nach dem Umbau gibt es die schmalen Indizes, und jede Datei trägt die
// Kanäle und die Sprache ihrer ersten Tonspur.
func TestSchmaleIndizesUndTonspuren(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	var def string
	_ = st.DB().QueryRow(`SELECT sql FROM sqlite_master WHERE name = 'item_by_parent'`).Scan(&def)
	if !strings.Contains(def, "added_at") || !strings.Contains(def, "id)") {
		t.Fatalf("item_by_parent: %s", def)
	}
	for _, name := range []string{"item_baum", "file_kennzahl", "artwork_da", "item_neu_eltern", "progress_abbild"} {
		var n int
		_ = st.DB().QueryRow(`SELECT count(*) FROM sqlite_master WHERE type = 'index' AND name = ?`, name).Scan(&n)
		if n != 1 {
			t.Errorf("Index %s fehlt", name)
		}
	}
	_ = st.DB().QueryRow(`SELECT sql FROM sqlite_master WHERE name = 'item_ord_neu'`).Scan(&def)
	if !strings.Contains(def, "fehlt_seit") {
		t.Errorf("item_ord_neu ohne die Spalten der Freigabe: %s", def)
	}

	lib, _ := st.CreateLibrary(ctx, "Filme", "movie", t.TempDir(), "de")
	film := &Item{LibraryID: lib.ID, Kind: "movie", Title: "Film"}
	_ = st.UpsertItem(ctx, film)
	f := &MediaFile{ItemID: film.ID, Path: "/f/film.mkv", SizeBytes: 1,
		Spuren: []Track{
			{Index: 2, Kind: "audio", Language: "eng", Channels: 8},
			{Index: 1, Kind: "audio", Language: "ger", Channels: 2},
			{Index: 3, Kind: "subtitle", Language: "fre"},
		}}
	if err := st.UpsertFile(ctx, f); err != nil {
		t.Fatal(err)
	}
	pruefen := func(wann string) {
		t.Helper()
		var k int
		var s string
		if err := st.DB().QueryRow(`SELECT ton_kanaele, ton_sprache FROM media_file WHERE id = ?`, f.ID).Scan(&k, &s); err != nil {
			t.Fatal(err)
		}
		if k != 8 || s != "ger" {
			t.Fatalf("%s: %d Kanäle, Sprache %q — erwartet 8 und ger (erste Tonspur)", wann, k, s)
		}
	}
	pruefen("beim Schreiben")
	// Wie nach einem Update: leer, dann einmal aus den Spuren gefüllt.
	_, _ = st.DB().Exec(`UPDATE media_file SET ton_kanaele = NULL, ton_sprache = NULL`)
	if err := tonFuellen(st.DB()); err != nil {
		t.Fatal(err)
	}
	pruefen("nach dem Umbau")
	k, err := st.Kennzahlen(ctx, []string{film.ID})
	if err != nil || k[film.ID].Channels != 8 {
		t.Fatalf("Kennzahlen: %+v %v", k, err)
	}
}

// Eine Seite des Rasters liest erst die Kennungen nur über einen Index —
// ohne die Zeilen der Titel, die beim Sortieren durchgesehen werden.
func TestRasterSeiteNurUeberIndex(t *testing.T) {
	st := testStore(t)
	ctx := t.Context()
	lib, _ := st.CreateLibrary(ctx, "Filme", "movie", t.TempDir(), "de")
	for _, n := range []string{"B", "A", "C"} {
		_ = st.UpsertItem(ctx, &Item{LibraryID: lib.ID, Kind: "movie", Title: n})
	}
	_ = optimieren(ctx, st.db)
	for _, sortierung := range []string{"title", "added", "rating", "year"} {
		q := Query{Libraries: []string{lib.ID}, Kinds: []string{"movie"}, Sort: sortierung, Freigabe: true, Limit: 2}
		where, args, _ := bedingung(q)
		schluessel, ab, _ := SortierungDeuten(q.Sort, q.Richtung)
		rows, err := st.db.Query(`EXPLAIN QUERY PLAN SELECT i.id FROM item i WHERE `+strings.Join(where, " AND ")+
			` ORDER BY `+sqlOrdnung(schluessel, ab)+` LIMIT 2`, args...)
		if err != nil {
			t.Fatal(err)
		}
		var plan []string
		for rows.Next() {
			var a, b, c int
			var z string
			_ = rows.Scan(&a, &b, &c, &z)
			plan = append(plan, z)
		}
		rows.Close()
		if !strings.Contains(strings.Join(plan, "\n"), "COVERING INDEX item_ord_") {
			t.Errorf("%s: Plan ohne Sortier-Index, der alles enthält:\n  %s", sortierung, strings.Join(plan, "\n  "))
		}
	}
	got, err := st.Query(ctx, Query{Libraries: []string{lib.ID}, Sort: "title", Freigabe: true})
	if err != nil || len(got) != 3 || got[0].Title != "A" || got[2].Title != "C" {
		t.Fatalf("Query nach Titel: %v %v", got, err)
	}
}
