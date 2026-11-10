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

// altMusikFotoSchema: die Tabellen für Musik und Fotos, wie Stand 9 sie
// angelegt hat (mediathek_schema.go aus dem git-Verlauf), samt Indizes.
var altMusikFotoSchema = []string{
	`CREATE TABLE musik_titel (
	   id TEXT PRIMARY KEY,
	   library_id TEXT NOT NULL REFERENCES library(id) ON DELETE CASCADE,
	   album_id TEXT NOT NULL DEFAULT '', pfad TEXT NOT NULL UNIQUE,
	   groesse INTEGER NOT NULL, mtime INTEGER NOT NULL,
	   titel TEXT NOT NULL, titel_sort TEXT NOT NULL,
	   interpret TEXT NOT NULL DEFAULT '', albuminterpret TEXT NOT NULL DEFAULT '',
	   album TEXT NOT NULL DEFAULT '', track INTEGER NOT NULL DEFAULT 0,
	   disc INTEGER NOT NULL DEFAULT 0, jahr INTEGER NOT NULL DEFAULT 0,
	   genre TEXT NOT NULL DEFAULT '', komponist TEXT NOT NULL DEFAULT '',
	   dauer_ms INTEGER NOT NULL DEFAULT 0, compilation INTEGER NOT NULL DEFAULT 0,
	   explizit INTEGER NOT NULL DEFAULT 0, codec TEXT NOT NULL DEFAULT '',
	   container TEXT NOT NULL DEFAULT '', bitrate INTEGER NOT NULL DEFAULT 0,
	   abtastrate INTEGER NOT NULL DEFAULT 0, hat_cover INTEGER NOT NULL DEFAULT 0,
	   hinzugefuegt INTEGER NOT NULL, fehlt_seit INTEGER,
	   interpret_sort TEXT NOT NULL DEFAULT '', album_sort TEXT NOT NULL DEFAULT '',
	   albuminterpret_sort TEXT NOT NULL DEFAULT '')`,
	`CREATE INDEX musik_titel_album ON musik_titel (album_id, disc, track)`,
	`CREATE INDEX musik_titel_lib ON musik_titel (library_id)`,
	`CREATE INDEX musik_titel_sort ON musik_titel (titel_sort)`,
	`CREATE TABLE musik_album (
	   id TEXT PRIMARY KEY,
	   library_id TEXT NOT NULL REFERENCES library(id) ON DELETE CASCADE,
	   name TEXT NOT NULL, name_sort TEXT NOT NULL,
	   interpret TEXT NOT NULL DEFAULT '', interpret_id TEXT NOT NULL DEFAULT '',
	   jahr INTEGER NOT NULL DEFAULT 0, genre TEXT NOT NULL DEFAULT '',
	   compilation INTEGER NOT NULL DEFAULT 0, cover_quelle TEXT NOT NULL DEFAULT '',
	   titel_anzahl INTEGER NOT NULL DEFAULT 0, dauer_ms INTEGER NOT NULL DEFAULT 0,
	   hinzugefuegt INTEGER NOT NULL)`,
	`CREATE INDEX musik_album_lib ON musik_album (library_id)`,
	`CREATE INDEX musik_album_interpret ON musik_album (interpret_id)`,
	`CREATE INDEX musik_album_neu ON musik_album (hinzugefuegt DESC)`,
	`CREATE TABLE musik_interpret (id TEXT PRIMARY KEY, name TEXT NOT NULL, name_sort TEXT NOT NULL)`,
	`CREATE TABLE musik_hoerstand (
	   account_id TEXT NOT NULL REFERENCES account(id) ON DELETE CASCADE,
	   titel_id TEXT NOT NULL REFERENCES musik_titel(id) ON DELETE CASCADE,
	   anzahl INTEGER NOT NULL DEFAULT 0, reihenfolge INTEGER NOT NULL,
	   PRIMARY KEY (account_id, titel_id))`,
	`CREATE INDEX musik_hoerstand_reihe ON musik_hoerstand (account_id, reihenfolge DESC)`,
	`CREATE TABLE musik_hoerreihe (
	   account_id TEXT PRIMARY KEY REFERENCES account(id) ON DELETE CASCADE,
	   reihe INTEGER NOT NULL DEFAULT 0)`,
	`CREATE TABLE musik_favorit (
	   account_id TEXT NOT NULL REFERENCES account(id) ON DELETE CASCADE,
	   art TEXT NOT NULL, ziel_id TEXT NOT NULL,
	   PRIMARY KEY (account_id, art, ziel_id))`,
	`CREATE TABLE musik_wiedergabeliste (
	   id TEXT PRIMARY KEY,
	   account_id TEXT NOT NULL REFERENCES account(id) ON DELETE CASCADE,
	   name TEXT NOT NULL, geteilt INTEGER NOT NULL DEFAULT 0,
	   gemeinsam INTEGER NOT NULL DEFAULT 0, angelegt INTEGER NOT NULL)`,
	`CREATE INDEX musik_wiedergabeliste_konto ON musik_wiedergabeliste (account_id)`,
	`CREATE TABLE musik_listeneintrag (
	   liste_id TEXT NOT NULL REFERENCES musik_wiedergabeliste(id) ON DELETE CASCADE,
	   titel_id TEXT NOT NULL REFERENCES musik_titel(id) ON DELETE CASCADE,
	   ord INTEGER NOT NULL,
	   von_konto TEXT NOT NULL REFERENCES account(id) ON DELETE CASCADE,
	   PRIMARY KEY (liste_id, titel_id))`,
	`CREATE TABLE foto_album (
	   id TEXT PRIMARY KEY,
	   library_id TEXT NOT NULL REFERENCES library(id) ON DELETE CASCADE,
	   pfad TEXT NOT NULL, name TEXT NOT NULL, UNIQUE (library_id, pfad))`,
	`CREATE TABLE foto (
	   id TEXT PRIMARY KEY,
	   library_id TEXT NOT NULL REFERENCES library(id) ON DELETE CASCADE,
	   album_id TEXT NOT NULL DEFAULT '', pfad TEXT NOT NULL UNIQUE,
	   groesse INTEGER NOT NULL, mtime INTEGER NOT NULL,
	   art TEXT NOT NULL, format TEXT NOT NULL, aufnahme INTEGER NOT NULL,
	   datum_quelle TEXT NOT NULL DEFAULT 'datei',
	   breite INTEGER NOT NULL DEFAULT 0, hoehe INTEGER NOT NULL DEFAULT 0,
	   ausrichtung INTEGER NOT NULL DEFAULT 1, kamera TEXT NOT NULL DEFAULT '',
	   breitengrad REAL, laengengrad REAL, dauer_ms INTEGER NOT NULL DEFAULT 0,
	   videocodec TEXT NOT NULL DEFAULT '', vorschau INTEGER NOT NULL DEFAULT 0,
	   hinzugefuegt INTEGER NOT NULL, fehlt_seit INTEGER)`,
	`CREATE INDEX foto_zeit ON foto (aufnahme DESC)`,
	`CREATE INDEX foto_album_idx ON foto (album_id, aufnahme)`,
	`CREATE INDEX foto_lib ON foto (library_id)`,
}

// Eine Datenbank von Stand 9 mit einer Musik- und einer Fotobibliothek
// samt Liedern, Fotos, Hörstand, Herzen und Musikliste: Nach dem Umbau
// sind beide Bibliotheken und alle ihre Tabellen weg, vorher wurde
// gesichert — und Filme, Serien, Fortschritt, Merkliste, Listen,
// Freigaben und Gruppen stehen unverändert da.
func TestUmbauOhneMusikUndFotos(t *testing.T) {
	pfad := filepath.Join(t.TempDir(), "lumen.db")
	st, err := Open(pfad)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	filme, _ := st.CreateLibrary(ctx, "Filme", "movie", "/medien/filme", "de")
	serien, _ := st.CreateLibrary(ctx, "Serien", "series", "/medien/serien", "de")
	film := &Item{LibraryID: filme.ID, Kind: "movie", Title: "Matrix", Year: 1999}
	serie := &Item{LibraryID: serien.ID, Kind: "series", Title: "Dark", Year: 2017}
	for _, it := range []*Item{film, serie} {
		if err := st.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
	}
	lea, err := st.CreateAccount(ctx, NewAccount{Name: "Lea", LockKind: "pin", Secret: "1234"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SaveProgress(ctx, lea.ID, film.ID, 60_000, 600_000); err != nil {
		t.Fatal(err)
	}
	_ = st.AddToWatchlist(ctx, lea.ID, serie.ID)
	liste, _ := st.ListeAnlegen(ctx, lea.ID, "Filmabend")
	_ = st.ListeHinzufuegen(ctx, lea.ID, liste.ID, film.ID)

	// So sah es unter Stand 9 aus: die Tabellen für Musik und Fotos und
	// zwei Bibliotheken dieser Arten mit Inhalt.
	for _, q := range altMusikFotoSchema {
		mustExec(t, st, q)
	}
	for _, q := range []string{
		`INSERT INTO library (id,name,kind,path,language) VALUES
		   ('lib_mus','Musik','music','/medien/musik','de'),
		   ('lib_foto','Fotos','photo','/medien/fotos','de')`,
		`INSERT INTO musik_titel (id,library_id,album_id,pfad,groesse,mtime,titel,titel_sort,hinzugefuegt)
		   VALUES ('mt1','lib_mus','al1','/medien/musik/a/1.flac',1,1,'Lied','lied',1)`,
		`INSERT INTO musik_album (id,library_id,name,name_sort,hinzugefuegt) VALUES ('al1','lib_mus','Album','album',1)`,
		`INSERT INTO musik_interpret (id,name,name_sort) VALUES ('in1','Band','band')`,
		`INSERT INTO musik_hoerstand (account_id,titel_id,anzahl,reihenfolge) VALUES ('` + lea.ID + `','mt1',3,1)`,
		`INSERT INTO musik_hoerreihe (account_id,reihe) VALUES ('` + lea.ID + `',1)`,
		`INSERT INTO musik_favorit (account_id,art,ziel_id) VALUES ('` + lea.ID + `','album','al1')`,
		`INSERT INTO musik_wiedergabeliste (id,account_id,name,angelegt) VALUES ('ml1','` + lea.ID + `','Joggen',1)`,
		`INSERT INTO musik_listeneintrag (liste_id,titel_id,ord,von_konto) VALUES ('ml1','mt1',0,'` + lea.ID + `')`,
		`INSERT INTO foto_album (id,library_id,pfad,name) VALUES ('fa1','lib_foto','2023','2023')`,
		`INSERT INTO foto (id,library_id,album_id,pfad,groesse,mtime,art,format,aufnahme,hinzugefuegt)
		   VALUES ('f1','lib_foto','fa1','/medien/fotos/2023/a.jpg',1,1,'bild','jpeg',1,1)`,
		`INSERT INTO setting (key,value) VALUES ('fotos.ort_zeigen','1')`,
	} {
		mustExec(t, st, q)
	}
	if err := st.SetGrants(ctx, lea.ID, []string{filme.ID, serien.ID, "lib_mus", "lib_foto"}); err != nil {
		t.Fatal(err)
	}
	gruppe, err := st.GruppeAnlegen(ctx, Gruppe{Name: "Familie",
		Libraries: []string{filme.ID, "lib_mus", "lib_foto"}})
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, st, `PRAGMA user_version = 9`)
	_ = st.Close()

	st, err = Open(pfad)
	if err != nil {
		t.Fatalf("Lumen startet nach dem Umbau nicht: %v", err)
	}

	// Gesichert, bevor umgebaut wurde — und in der Sicherung ist die
	// Musik noch da.
	if _, err := os.Stat(pfad + ".vor-umbau"); err != nil {
		t.Fatalf("keine Sicherung vor dem Umbau: %v", err)
	}
	// Die API darf die Ordner von Musik und Fotos einmal wegräumen.
	if v := st.Setting(t.Context(), MediathekResteEinst, ""); v != "1" {
		t.Fatalf("Marke für die Reste auf der Platte: %q", v)
	}
	var stand int
	_ = st.DB().QueryRow(`PRAGMA user_version`).Scan(&stand)
	if stand != SchemaStand {
		t.Fatalf("Stand %d, erwartet %d", stand, SchemaStand)
	}

	// Musik und Fotos: Bibliotheken, Tabellen, Indizes, Einstellung weg.
	var n int
	_ = st.DB().QueryRow(`SELECT count(*) FROM library WHERE kind IN ('music','photo')`).Scan(&n)
	if n != 0 {
		t.Fatalf("%d Musik-/Fotobibliotheken übrig", n)
	}
	for _, name := range append(append([]string{}, musikFotoTabellenAlt...), musikFotoIndizesAlt...) {
		_ = st.DB().QueryRow(`SELECT count(*) FROM sqlite_master WHERE name = ?`, name).Scan(&n)
		if n != 0 {
			t.Errorf("%s steht noch in der Datenbank", name)
		}
	}
	_ = st.DB().QueryRow(`SELECT count(*) FROM sqlite_master WHERE name LIKE 'musik%' OR name LIKE 'foto%'`).Scan(&n)
	if n != 0 {
		t.Errorf("%d Einträge für Musik oder Fotos übrig", n)
	}
	if v := st.Setting(ctx, "fotos.ort_zeigen", "weg"); v != "weg" {
		t.Errorf("Einstellung fotos.ort_zeigen noch da: %q", v)
	}
	_ = st.DB().QueryRow(`SELECT count(*) FROM grant_library WHERE library_id IN ('lib_mus','lib_foto')`).Scan(&n)
	if n != 0 {
		t.Errorf("%d Freigaben für Musik oder Fotos übrig", n)
	}
	_ = st.DB().QueryRow(`SELECT count(*) FROM gruppe_bibliothek WHERE library_id IN ('lib_mus','lib_foto')`).Scan(&n)
	if n != 0 {
		t.Errorf("%d Gruppenzuordnungen für Musik oder Fotos übrig", n)
	}

	// Filme und Serien: alles da.
	libs, err := st.Libraries(ctx)
	if err != nil || len(libs) != 2 {
		t.Fatalf("Bibliotheken nach dem Umbau: %+v %v", libs, err)
	}
	for _, l := range libs {
		if l.Count != 1 {
			t.Errorf("%s zählt %d Titel, erwartet 1", l.Name, l.Count)
		}
	}
	if got, err := st.Item(ctx, film.ID); err != nil || got.Title != "Matrix" {
		t.Fatalf("Film nach dem Umbau: %+v %v", got, err)
	}
	if got, err := st.Item(ctx, serie.ID); err != nil || got.Title != "Dark" {
		t.Fatalf("Serie nach dem Umbau: %+v %v", got, err)
	}
	if _, err := st.Progress(ctx, lea.ID, film.ID); err != nil {
		t.Fatalf("Fortschritt ging verloren: %v", err)
	}
	if ids, _ := st.WatchlistIDs(ctx, lea.ID); len(ids) != 1 || ids[0] != serie.ID {
		t.Fatalf("Merkliste nach dem Umbau: %v", ids)
	}
	if l, err := st.Liste(ctx, lea.ID, liste.ID); err != nil || len(l.ItemIDs) != 1 {
		t.Fatalf("Wiedergabeliste nach dem Umbau: %+v %v", l, err)
	}
	erlaubt, _ := st.GrantedLibraries(ctx, lea.ID)
	if len(erlaubt) != 2 {
		t.Fatalf("Freigaben nach dem Umbau: %v", erlaubt)
	}
	g, err := st.Gruppe(ctx, gruppe.ID)
	if err != nil || len(g.Libraries) != 1 || g.Libraries[0] != filme.ID {
		t.Fatalf("Gruppe nach dem Umbau: %+v %v", g, err)
	}

	// Ein zweiter Start ändert nichts mehr und scheitert nicht.
	_ = st.Close()
	st2, err := Open(pfad)
	if err != nil {
		t.Fatalf("zweiter Start: %v", err)
	}
	_ = st2.Close()
}
