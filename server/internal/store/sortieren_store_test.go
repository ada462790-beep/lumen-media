// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Der Schlüssel folgt der Sprache: in einer deutschen Bibliothek fällt
// „Die" weg — ausser der Titel ist der englische Originaltitel.
func TestSortierungNachSprache(t *testing.T) {
	s := testStore(t)
	ctx := t.Context()
	lib, _ := s.CreateLibrary(ctx, "Filme", "movie", t.TempDir(), "de")
	hard := &Item{LibraryID: lib.ID, Kind: "movie", Title: "Die Hard", OriginalTitle: "Die Hard", Sprache: "en"}
	bucht := &Item{LibraryID: lib.ID, Kind: "movie", Title: "Die Bucht", OriginalTitle: "The Cove", Sprache: "en"}
	for _, it := range []*Item{hard, bucht} {
		if err := s.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
	}
	if hard.SortTitle != "die hard" || bucht.SortTitle != "bucht" {
		t.Fatalf("Schlüssel %q / %q", hard.SortTitle, bucht.SortTitle)
	}
	// Der Scanner kennt die Originalsprache nicht — sie bleibt trotzdem.
	nochmal := &Item{ID: hard.ID, LibraryID: lib.ID, Kind: "movie", Title: "Die Hard", OriginalTitle: "Die Hard"}
	if err := s.UpsertItem(ctx, nochmal); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Item(ctx, hard.ID); got.SortTitle != "die hard" || got.Sprache != "en" {
		t.Fatalf("nach erneutem Speichern %q (%q)", got.SortTitle, got.Sprache)
	}
	// Ein eigener Sortiertitel (etwa aus der NFO) gilt vor dem Titel.
	nochmal.SortEigen = "Hard 1"
	_ = s.UpsertItem(ctx, nochmal)
	if got, _ := s.Item(ctx, hard.ID); got.SortTitle != "hard 0000000001" {
		t.Fatalf("eigener Sortiertitel: %q", got.SortTitle)
	}
}

// Eine Datenbank von Stand 8: Nach dem Umbau stimmen die Schlüssel, ein
// von Hand eingetragener Sortiertitel ist erhalten, und vorher wurde
// gesichert.
func TestUmbauAufStand9(t *testing.T) {
	pfad := filepath.Join(t.TempDir(), "lumen.db")
	st, err := Open(pfad)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO library (id,name,kind,path,language) VALUES ('lib','Filme','movie','/x','de'),
		   ('mus','Musik','music','/m','de')`,
		// So schrieb die alte Fassung: ä → ae, Zahlen als Text.
		`INSERT INTO item (id,library_id,kind,title,sort_title,added_at,updated_at) VALUES
		   ('a','lib','movie','Teil 10','teil 10',1,1), ('b','lib','movie','Teil 2','teil 2',1,1),
		   ('c','lib','movie','Ötzi','oetzi',1,1), ('d','lib','movie','Zorro','aaa zuerst',1,1)`,
		`INSERT INTO feld_sperre (item_id, feld, anzeige) VALUES ('d','sortTitle','Aaa zuerst')`,
		`INSERT INTO musik_titel (id,library_id,pfad,groesse,mtime,titel,titel_sort,interpret,album,hinzugefuegt)
		   VALUES ('m1','mus','/m/1.flac',1,1,'Über','ueber','Die Ärzte','Ab 18',1)`,
		`INSERT INTO musik_album (id,library_id,name,name_sort,hinzugefuegt) VALUES ('al','mus','The Wall','wall',1)`,
		`PRAGMA user_version = 8`,
	} {
		if _, err := st.DB().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	_ = st.Close()

	st, err = Open(pfad)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := os.Stat(pfad + ".vor-umbau"); err != nil {
		t.Fatalf("keine Sicherung vor dem Umbau: %v", err)
	}
	want := map[string]string{"a": "teil 0000000010", "b": "teil 0000000002", "c": "otzi", "d": "aaa zuerst"}
	for id, w := range want {
		var got string
		_ = st.DB().QueryRow(`SELECT sort_title FROM item WHERE id = ?`, id).Scan(&got)
		if got != w {
			t.Errorf("%s: %q, erwartet %q", id, got, w)
		}
	}
	if e := st.Sortiertitel(t.Context(), "d"); e != "Aaa zuerst" {
		t.Errorf("eigener Sortiertitel nach dem Umbau: %q", e)
	}
	var ts, is, as, an string
	_ = st.DB().QueryRow(`SELECT titel_sort, interpret_sort, album_sort FROM musik_titel WHERE id = 'm1'`).Scan(&ts, &is, &as)
	_ = st.DB().QueryRow(`SELECT name_sort FROM musik_album WHERE id = 'al'`).Scan(&an)
	if ts != "uber" || is != "arzte" || as != "ab 0000000018" || an != "wall" {
		t.Errorf("Musik nach dem Umbau: %q %q %q %q", ts, is, as, an)
	}
}

// seitenweise holt alle Titel in Seiten zu fünf — so, wie das Raster beim
// Herunterscrollen nachlädt.
func seitenweise(t *testing.T, s *Store, q Query) []string {
	t.Helper()
	var ids []string
	for off := 0; ; off += 5 {
		q.Offset, q.Limit = off, 5
		seite, err := s.Query(t.Context(), q)
		if err != nil {
			t.Fatal(err)
		}
		for _, it := range seite {
			ids = append(ids, it.ID)
		}
		if len(seite) < 5 {
			return ids
		}
	}
}

// Das Raster ist natürlich sortiert und über Seiten hinweg stabil — auch
// wenn viele Titel denselben Zeitpunkt oder denselben Namen haben.
func TestRasterNatuerlichUndStabil(t *testing.T) {
	s := testStore(t)
	ctx := t.Context()
	lib, _ := s.CreateLibrary(ctx, "Filme", "movie", t.TempDir(), "de")
	titel := map[string]string{}
	for _, n := range []string{"Teil 10", "Teil 2", "Teil 1", "Teil 11", "Teil 3", "Gleich", "Gleich", "Gleich",
		"Ötzi", "Øresund", "Ocean's Eleven", "Zorro", "10 Cloverfield Lane", "2 Fast 2 Furious"} {
		it := &Item{LibraryID: lib.ID, Kind: "movie", Title: n}
		if err := s.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
		titel[it.ID] = n
	}
	// Alle zur selben Sekunde hinzugefügt, ohne Jahr und Wertung.
	mustExec(t, s, `UPDATE item SET added_at = 100, zuletzt_neu = 100`)

	alle, _ := s.Query(ctx, Query{Libraries: []string{lib.ID}, Sort: "title", Limit: 100})
	var namen []string
	for _, it := range alle {
		namen = append(namen, it.Title)
	}
	want := "2 Fast 2 Furious|10 Cloverfield Lane|Gleich|Gleich|Gleich|Ocean's Eleven|Øresund|Ötzi|" +
		"Teil 1|Teil 2|Teil 3|Teil 10|Teil 11|Zorro"
	if got := strings.Join(namen, "|"); got != want {
		t.Fatalf("Reihenfolge\n%s\nerwartet\n%s", got, want)
	}
	for _, q := range []Query{
		{Sort: "title"}, {Sort: "title", Richtung: "desc"}, {Sort: "added"}, {Sort: "added", Richtung: "asc"},
		{Sort: "premiered"}, {Sort: "rating", Richtung: "asc"}, {Sort: "runtime"},
		{Sort: "random", Seed: 42}, {Sort: "year-asc"}, {Sort: "title-desc"},
	} {
		q.Libraries = []string{lib.ID}
		ids := seitenweise(t, s, q)
		voll := q
		voll.Limit = 100
		ganz, _ := s.Query(ctx, voll)
		if len(ids) != len(titel) || len(ganz) != len(ids) {
			t.Fatalf("%s/%s: %d Titel seitenweise, %d auf einmal, erwartet %d", q.Sort, q.Richtung, len(ids), len(ganz), len(titel))
		}
		schon := map[string]bool{}
		for i, id := range ids {
			if schon[id] {
				t.Fatalf("%s/%s: %s doppelt", q.Sort, q.Richtung, titel[id])
			}
			schon[id] = true
			if ganz[i].ID != id {
				t.Fatalf("%s/%s: Seiten weichen an Stelle %d von der ganzen Liste ab", q.Sort, q.Richtung, i)
			}
		}
	}
	// Derselbe Samen gibt dieselbe Reihenfolge, ein anderer eine andere.
	a := seitenweise(t, s, Query{Libraries: []string{lib.ID}, Sort: "random", Seed: 7})
	b := seitenweise(t, s, Query{Libraries: []string{lib.ID}, Sort: "random", Seed: 7})
	c := seitenweise(t, s, Query{Libraries: []string{lib.ID}, Sort: "random", Seed: 8})
	if strings.Join(a, ",") != strings.Join(b, ",") || strings.Join(a, ",") == strings.Join(c, ",") {
		t.Fatal("Zufall mit Samen ist nicht wiederholbar")
	}
}

// Leere Werte stehen in beiden Richtungen am Ende.
func TestLeereWerteZuletzt(t *testing.T) {
	s := testStore(t)
	ctx := t.Context()
	lib, _ := s.CreateLibrary(ctx, "Filme", "movie", t.TempDir(), "de")
	ohne := &Item{LibraryID: lib.ID, Kind: "movie", Title: "Ohne"}
	alt := &Item{LibraryID: lib.ID, Kind: "movie", Title: "Alt", Year: 1980, IMDbRating: 5, RuntimeMS: 90 * 60000}
	neu := &Item{LibraryID: lib.ID, Kind: "movie", Title: "Neu", Year: 2020, Premiered: "2020-05-01", IMDbRating: 8, RuntimeMS: 120 * 60000}
	for _, it := range []*Item{ohne, alt, neu} {
		_ = s.UpsertItem(ctx, it)
	}
	for _, k := range []string{"premiered", "rating", "runtime"} {
		for _, r := range []string{"asc", "desc"} {
			l, err := s.Query(ctx, Query{Libraries: []string{lib.ID}, Sort: k, Richtung: r})
			if err != nil || len(l) != 3 {
				t.Fatalf("%s/%s: %v %v", k, r, l, err)
			}
			if l[2].ID != ohne.ID {
				t.Errorf("%s/%s: ohne Wert nicht am Ende: %s %s %s", k, r, l[0].Title, l[1].Title, l[2].Title)
			}
			if erst := map[string]string{"asc": alt.ID, "desc": neu.ID}[r]; l[0].ID != erst {
				t.Errorf("%s/%s: zuerst %s", k, r, l[0].Title)
			}
		}
	}
}

// Staffeln und Folgen: Specials zuletzt, Folgen ohne Nummer zuletzt —
// bei Children und FolgenVon gleich.
func TestFolgenReihenfolge(t *testing.T) {
	s := testStore(t)
	ctx := t.Context()
	lib, _ := s.CreateLibrary(ctx, "Serien", "series", t.TempDir(), "de")
	serie := &Item{LibraryID: lib.ID, Kind: "series", Title: "Tiefsee"}
	_ = s.UpsertItem(ctx, serie)
	staffel := map[int]*Item{}
	for _, nr := range []int{0, 2, 1} {
		st := &Item{LibraryID: lib.ID, Kind: "season", ParentID: serie.ID, Title: "Staffel", SeasonNo: nr}
		_ = s.UpsertItem(ctx, st)
		staffel[nr] = st
	}
	folge := func(st, nr int, titel, erst string) {
		_ = s.UpsertItem(ctx, &Item{LibraryID: lib.ID, Kind: "episode", ParentID: staffel[st].ID,
			Title: titel, SeasonNo: st, EpisodeNo: nr, Premiered: erst})
	}
	folge(0, 1, "Making-of", "")
	folge(1, 0, "Ohne Nummer", "2001-01-09")
	folge(1, 2, "Zwei", "")
	folge(1, 1, "Eins", "")
	folge(2, 1, "Zwei-Eins", "")
	folge(1, 0, "Ohne Nummer früher", "2001-01-02")

	var namen []string
	alle, _ := s.FolgenVon(ctx, serie.ID, -1)
	for _, f := range alle {
		namen = append(namen, f.Title)
	}
	want := "Eins|Zwei|Ohne Nummer früher|Ohne Nummer|Zwei-Eins|Making-of"
	if got := strings.Join(namen, "|"); got != want {
		t.Fatalf("FolgenVon\n%s\nerwartet\n%s", got, want)
	}
	staffeln, _ := s.Children(ctx, serie.ID)
	if len(staffeln) != 3 || staffeln[0].SeasonNo != 1 || staffeln[1].SeasonNo != 2 || staffeln[2].SeasonNo != 0 {
		t.Fatalf("Staffeln: %+v", staffeln)
	}
	namen = nil
	kinder, _ := s.Children(ctx, staffel[1].ID)
	for _, f := range kinder {
		namen = append(namen, f.Title)
	}
	if got := strings.Join(namen, "|"); got != "Eins|Zwei|Ohne Nummer früher|Ohne Nummer" {
		t.Fatalf("Children der Staffel: %s", got)
	}
}

// „Zuletzt gesehen" und „Ungesehene zuerst" gelten nur für das eigene
// Profil: Was ein anderes geschaut hat, ändert nichts.
func TestSortierungJeProfil(t *testing.T) {
	s := testStore(t)
	ctx := t.Context()
	lea, _ := s.CreateAccount(ctx, NewAccount{Name: "Lea"})
	ben, _ := s.CreateAccount(ctx, NewAccount{Name: "Ben"})
	lib, _ := s.CreateLibrary(ctx, "Filme", "movie", t.TempDir(), "de")
	ids := map[string]string{}
	for _, n := range []string{"A", "B", "C", "D"} {
		it := &Item{LibraryID: lib.ID, Kind: "movie", Title: n}
		_ = s.UpsertItem(ctx, it)
		ids[n] = it.ID
	}
	_ = s.MarkFinished(ctx, lea.ID, ids["C"], 1000)
	_ = s.SaveProgress(ctx, lea.ID, ids["A"], 100, 1000)
	_ = s.MarkFinished(ctx, ben.ID, ids["D"], 1000)
	titel := func(q Query) string {
		q.Libraries = []string{lib.ID}
		l, err := s.Query(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		var n []string
		for _, it := range l {
			n = append(n, it.Title)
		}
		return strings.Join(n, "")
	}
	if got := titel(Query{Sort: "lastwatched", Konto: lea.ID}); got != "ACBD" {
		t.Errorf("zuletzt gesehen (Lea): %s", got)
	}
	if got := titel(Query{Sort: "lastwatched", Konto: lea.ID, Richtung: "asc"}); got != "CABD" {
		t.Errorf("zuletzt gesehen aufsteigend (Lea): %s", got)
	}
	if got := titel(Query{Sort: "unwatched", Konto: lea.ID}); got != "ABDC" {
		t.Errorf("ungesehene zuerst (Lea): %s", got)
	}
	if got := titel(Query{Sort: "unwatched", Konto: ben.ID}); got != "ABCD" {
		t.Errorf("ungesehene zuerst (Ben): %s", got)
	}
	if got := titel(Query{Sort: "lastwatched"}); got != "ABCD" {
		t.Errorf("ohne Profil nach Titel: %s", got)
	}
}

// Suchtreffer: genau der Titel vor dem Anfang vor dem Wortanfang vor dem Rest.
func TestRelevanz(t *testing.T) {
	s := testStore(t)
	ctx := t.Context()
	lib, _ := s.CreateLibrary(ctx, "Filme", "movie", t.TempDir(), "de")
	for _, n := range []string{"Animatrix", "Matrix Reloaded", "The Matrix", "Die Matrix-Akte", "Matrix"} {
		_ = s.UpsertItem(ctx, &Item{LibraryID: lib.ID, Kind: "movie", Title: n})
	}
	l, _ := s.Query(ctx, Query{Libraries: []string{lib.ID}, Text: "matrix", Sort: "relevanz"})
	var n []string
	for _, it := range l {
		n = append(n, it.Title)
	}
	if got := strings.Join(n, "|"); got != "Matrix|The Matrix|Matrix Reloaded|Die Matrix-Akte|Animatrix" {
		t.Fatalf("Relevanz: %s", got)
	}
}
