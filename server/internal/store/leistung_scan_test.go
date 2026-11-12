// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

// Regressionstests zum Leistungsbericht: „erst Filme, dann Serien" machte
// die Mediathek unbenutzbar (Startseite 245 s), weil die Statistik nach
// dem Film-Durchlauf parent_id für wertlos hielt und Kennzahlen mit
// einem OR-Join keinen Index fand.

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// mediathekFuellen legt filme Filme und serien Serien mit je folgen
// Folgen an, jede mit einer Datei — direkt in SQL, damit es schnell geht.
func mediathekFuellen(t *testing.T, st *Store, libID, vorsilbe string, filme, serien, folgen int) (filmIDs, serienIDs []string) {
	t.Helper()
	tx, err := st.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	item := func(id, art, eltern string, sn, en int) {
		var p any
		if eltern != "" {
			p = eltern
		}
		if _, err := tx.Exec(`INSERT INTO item (id, library_id, kind, parent_id, title, sort_title,
		    season_no, episode_no, added_at, updated_at) VALUES (?,?,?,?,?,?,?,?,1,1)`,
			id, libID, art, p, id, id, sn, en); err != nil {
			t.Fatal(err)
		}
	}
	datei := func(item string) {
		if _, err := tx.Exec(`INSERT INTO media_file (id, item_id, path, size_bytes, mtime, height, width)
		    VALUES (?,?,?,1,1,1080,1920)`, "f"+item, item, "/m/"+item+".mkv"); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < filme; i++ {
		id := fmt.Sprintf("%sfilm%05d", vorsilbe, i)
		item(id, "movie", "", 0, 0)
		datei(id)
		filmIDs = append(filmIDs, id)
	}
	for s := 0; s < serien; s++ {
		sid := fmt.Sprintf("%sserie%04d", vorsilbe, s)
		item(sid, "series", "", 0, 0)
		st1 := sid + "s1"
		item(st1, "season", sid, 1, 0)
		for e := 1; e <= folgen; e++ {
			eid := fmt.Sprintf("%se%03d", st1, e)
			item(eid, "episode", st1, 1, e)
			datei(eid)
		}
		serienIDs = append(serienIDs, sid)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return filmIDs, serienIDs
}

// planOhneScan prüft, dass eine Abfrage keine ganze Tabelle liest. Das
// Durchgehen des eigenen Zwischenergebnisses („SCAN z" für den CTE z, „SCAN
// x" für die Unterabfrage x) ist erlaubt.
func planOhneScan(t *testing.T, st *Store, name, q string, args ...any) {
	t.Helper()
	rows, err := st.db.Query(`EXPLAIN QUERY PLAN `+q, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var id, eltern, frei int
		var text string
		if err := rows.Scan(&id, &eltern, &frei, &text); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, text)
	}
	for _, z := range plan {
		if strings.HasPrefix(z, "SCAN ") && z != "SCAN z" && z != "SCAN x" && !strings.Contains(z, "CONSTANT ROW") {
			t.Errorf("%s liest eine ganze Tabelle:\n  %s", name, strings.Join(plan, "\n  "))
			return
		}
	}
}

func TestKennzahlenNachFilmenDannSerien(t *testing.T) {
	if testing.Short() {
		t.Skip("braucht ein paar tausend Zeilen")
	}
	pfad := filepath.Join(t.TempDir(), "l.db")
	st, err := Open(pfad)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	lib, _ := st.CreateLibrary(ctx, "Alles", "movie", t.TempDir(), "de")
	// Erst die Filme — danach die Statistik, wie nach dem ersten Durchlauf.
	filme, _ := mediathekFuellen(t, st, lib.ID, "a", 4000, 0, 0)
	if err := st.Optimieren(ctx); err != nil {
		t.Fatal(err)
	}
	var alteStatistik string
	_ = st.db.QueryRow(`SELECT stat FROM sqlite_stat1 WHERE idx = 'item_by_parent'`).Scan(&alteStatistik)
	// Dann die Serien (weniger als das Zehnfache an Zeilen).
	_, serien := mediathekFuellen(t, st, lib.ID, "b", 0, 80, 50)
	ids := append(append([]string{}, filme[:60]...), serien[:20]...)
	statistikFrisch := func(wann string) {
		t.Helper()
		var stat string
		var n int
		_ = st.db.QueryRow(`SELECT stat FROM sqlite_stat1 WHERE idx = 'item_by_parent'`).Scan(&stat)
		_ = st.db.QueryRow(`SELECT count(*) FROM item`).Scan(&n)
		if !strings.HasPrefix(stat, fmt.Sprintf("%d ", n)) {
			t.Errorf("%s: Statistik %q passt nicht zu %d Zeilen", wann, stat, n)
		}
	}

	pruefen := func(wann string) {
		t.Helper()
		start := time.Now()
		k, err := st.Kennzahlen(ctx, ids)
		dauer := time.Since(start)
		if err != nil {
			t.Fatal(err)
		}
		if len(k) != len(ids) || k[serien[0]].Height != 1080 {
			t.Fatalf("%s: %d Kennzahlen, Serie %+v", wann, len(k), k[serien[0]])
		}
		if dauer > 500*time.Millisecond {
			t.Errorf("%s: Kennzahlen für %d Titel brauchten %v", wann, len(ids), dauer)
		}
		planOhneScan(t, st, wann+": Kennzahlen", kennzahlenSQL(len(ids)), kennzahlenArgs(ids)...)
		planOhneScan(t, st, wann+": ErstePfade", erstePfadeSQL(len(ids)), erstePfadeArgs(ids)...)
	}
	// Auch mit der veralteten Statistik vom Film-Durchlauf.
	pruefen("mit veralteter Statistik")
	// Das Optimieren nach dem Serien-Durchlauf zählt neu (PRAGMA optimize
	// allein tat es erst bei zehnfacher Grösse).
	if err := st.Optimieren(ctx); err != nil {
		t.Fatal(err)
	}
	statistikFrisch("nach dem Serien-Durchlauf")
	pruefen("nach Filmen und Serien")
	// Ein Neustart heilt eine veraltete Statistik ebenso (bestehende
	// Installationen, die schon so dastehen).
	if _, err := st.db.Exec(`UPDATE sqlite_stat1 SET stat = ? WHERE idx = 'item_by_parent'`, alteStatistik); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(`UPDATE sqlite_stat1 SET stat = '4000 1' WHERE idx = 'sqlite_autoindex_item_1'`); err != nil {
		t.Fatal(err)
	}
	st.Close()
	if st, err = Open(pfad); err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	statistikFrisch("nach dem Neustart")
	pruefen("nach dem Neustart")
}

// Ähnliche Titel gehen von den eigenen Reihen, Personen und Genres aus;
// die Personensuche findet Namen aus dem Speicher (auch mit Umlaut vorn).
func TestAehnlicheUndPersonen(t *testing.T) {
	st := testStore(t)
	ctx := t.Context()
	lib, _ := st.CreateLibrary(ctx, "Filme", "movie", t.TempDir(), "de")
	neu := func(titel string, genres []string, leute ...string) string {
		it := &Item{LibraryID: lib.ID, Kind: "movie", Title: titel, Genres: genres}
		if err := st.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
		var p []Person
		for _, l := range leute {
			p = append(p, Person{Name: l})
		}
		if err := st.BesetzungSetzen(ctx, it.ID, p); err != nil {
			t.Fatal(err)
		}
		return it.ID
	}
	a := neu("Gegen die Wand", []string{"Drama"}, "Birol Ünel", "Sibel Kekilli")
	b := neu("Auf der anderen Seite", []string{"Drama"}, "Nurgül Yeşilçay", "Birol Ünel")
	c := neu("Nur Drama", []string{"Drama"}, "Niemand")
	neu("Komödie", []string{"Komödie"}, "Jemand")
	q := Query{Libraries: []string{lib.ID}}
	ids, err := st.Aehnliche(ctx, q, a, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != b || ids[1] != c {
		t.Fatalf("ähnlich zu %s: %v — erwartet %s, dann %s", a, ids, b, c)
	}
	treffer, err := st.PersonenSuchen(ctx, q, "ünel", 5)
	if err != nil || len(treffer) != 1 || treffer[0].Name != "Birol Ünel" || treffer[0].Anzahl != 2 {
		t.Fatalf("PersonenSuchen: %+v %v", treffer, err)
	}
	// Eine neue Besetzung ist gleich suchbar (der Vorrat veraltet).
	neu("Soul Kitchen", []string{"Komödie"}, "Adam Bousdoukos")
	if treffer, _ = st.PersonenSuchen(ctx, q, "bousd", 5); len(treffer) != 1 {
		t.Fatalf("neue Person nicht gefunden: %+v", treffer)
	}
	namen, _ := st.PersonenNamen(ctx, q)
	if len(namen) != 6 {
		t.Fatalf("%d Namen, erwartet 6", len(namen))
	}
}

// Ohne nennenswerte Änderung bleibt es bei PRAGMA optimize — kein volles
// ANALYZE bei jedem Start (Teil-Indizes wie file_fehlt zählen nicht).
func TestStatistikNichtBeiJedemStartNeu(t *testing.T) {
	pfad := filepath.Join(t.TempDir(), "l.db")
	st, err := Open(pfad)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	lib, _ := st.CreateLibrary(ctx, "Alles", "movie", t.TempDir(), "de")
	mediathekFuellen(t, st, lib.ID, "a", 300, 0, 0)
	_, _ = st.db.Exec(`UPDATE media_file SET missing_since = 1 WHERE id IN (SELECT id FROM media_file LIMIT 3)`)
	if err := st.Optimieren(ctx); err != nil {
		t.Fatal(err)
	}
	conn, _ := st.db.Conn(ctx)
	defer conn.Close()
	if statistikVeraltet(ctx, conn) {
		t.Fatal("gleich nach ANALYZE gilt die Statistik als veraltet")
	}
	mediathekFuellen(t, st, lib.ID, "b", 30, 0, 0)
	if statistikVeraltet(ctx, conn) {
		t.Fatal("zehn Prozent mehr Zeilen machen noch kein ANALYZE nötig")
	}
	mediathekFuellen(t, st, lib.ID, "c", 100, 0, 0)
	if !statistikVeraltet(ctx, conn) {
		t.Fatal("ein Drittel mehr Zeilen: die Statistik soll neu gezählt werden")
	}
	conn.Close()
	st.Close()
}
