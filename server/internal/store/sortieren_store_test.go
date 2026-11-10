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
