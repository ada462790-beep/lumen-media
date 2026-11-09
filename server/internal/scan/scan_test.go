// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package scan

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"lumen/internal/store"
)

// Ein echter Durchlauf über einen erfundenen Ordner: so sieht eine über
// Jahre gewachsene Sammlung aus.
func TestScanRealTree(t *testing.T) {
	root := t.TempDir()
	files := []string{
		"Die Bucht (2019)/Die.Bucht.2019.German.1080p.BluRay.x264-GRUPPE.mkv",
		"Nordlicht (2025)/movie.mkv",
		"Meridian/Meridian.2021.2160p.UHD.BluRay.DV.x265.mkv",
		"Meridian/Meridian.2021.1080p.BluRay.x264.mkv",
		"Sommerhaus (1998)/Sommerhaus CD1.avi",
		"Sommerhaus (1998)/Sommerhaus CD2.avi",
		"Die Bucht (2019)/sample.mkv",
		"Die Bucht (2019)/Extras/Interview.mkv",
		"Die Bucht (2019)/.DS_Store",
	}
	for _, f := range files {
		p := filepath.Join(root, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	st, err := store.Open(filepath.Join(t.TempDir(), "l.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ctx := context.Background()
	lib, err := st.CreateLibrary(ctx, "Filme", "movie", root, "de")
	if err != nil {
		t.Fatal(err)
	}

	res, err := New(st, nil).Scan(ctx, *lib)
	if err != nil {
		t.Fatal(err)
	}
	if res.Added != 4 {
		t.Errorf("erwartet 4 Filme, bekommen %d", res.Added)
	}

	items, err := st.Query(ctx, store.Query{Libraries: []string{lib.ID}, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	byTitle := map[string]store.Item{}
	for _, it := range items {
		byTitle[it.Title] = it
	}
	for _, want := range []string{"Die Bucht", "Nordlicht", "Meridian", "Sommerhaus"} {
		if _, ok := byTitle[want]; !ok {
			t.Errorf("%q fehlt; gefunden: %v", want, titles(items))
		}
	}
	if byTitle["Nordlicht"].Year != 2025 {
		t.Errorf("Jahr aus dem Ordner nicht übernommen: %d", byTitle["Nordlicht"].Year)
	}

	// Zwei Fassungen von Meridian müssen als ein Titel mit zwei Dateien
	// ankommen — das ist, was das Aufräumen später zeigt.
	mf, err := st.FilesFor(ctx, byTitle["Meridian"].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(mf) != 2 {
		t.Errorf("Meridian hat %d Dateien, erwartet 2", len(mf))
	}
	// Sommerhaus ist ein Mehrteiler: zwei Dateien, Teil 1 und 2.
	sf, _ := st.FilesFor(ctx, byTitle["Sommerhaus"].ID)
	if len(sf) != 2 || sf[0].PartNo != 1 || sf[1].PartNo != 2 {
		t.Errorf("Mehrteiler falsch: %+v", sf)
	}

	// Zweiter Durchlauf darf nichts doppelt anlegen.
	res2, err := New(st, nil).Scan(ctx, *lib)
	if err != nil {
		t.Fatal(err)
	}
	if res2.Added != 0 {
		t.Errorf("zweiter Durchlauf legte %d Titel neu an", res2.Added)
	}

	// Datei weg: der Titel bleibt, wird aber als fehlend gemeldet.
	os.Remove(filepath.Join(root, "Nordlicht (2025)/movie.mkv"))
	res3, _ := New(st, nil).Scan(ctx, *lib)
	if res3.Missing != 1 {
		t.Errorf("fehlende Datei nicht erkannt: %d", res3.Missing)
	}
	if _, err := st.Item(ctx, byTitle["Nordlicht"].ID); err != nil {
		t.Error("Titel wurde gelöscht, statt als fehlend markiert zu werden")
	}
}

func TestScanSeriesTree(t *testing.T) {
	root := t.TempDir()
	for _, f := range []string{
		"Nordlicht/Staffel 1/Nordlicht.S01E01.Der.erste.Schnee.mkv",
		"Nordlicht/Staffel 1/Nordlicht.S01E02.Unter.dem.Eis.mkv",
		"Nordlicht/Staffel 2/E01.mkv",
		"Tiefsee/Season 1/Tiefsee - 1x03 - Die Bucht.mkv",
	} {
		p := filepath.Join(root, f)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte("x"), 0o644)
	}

	st, _ := store.Open(filepath.Join(t.TempDir(), "l.db"))
	defer st.Close()
	ctx := context.Background()
	lib, _ := st.CreateLibrary(ctx, "Serien", "series", root, "de")

	if _, err := New(st, nil).Scan(ctx, *lib); err != nil {
		t.Fatal(err)
	}
	series, err := st.Query(ctx, store.Query{
		Libraries: []string{lib.ID}, Kinds: []string{"series"}, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(series) != 2 {
		t.Fatalf("erwartet 2 Serien, bekommen %d: %v", len(series), titles(series))
	}
	for _, s := range series {
		seasons, _ := st.Children(ctx, s.ID)
		if len(seasons) == 0 {
			t.Errorf("%s hat keine Staffeln", s.Title)
		}
		for _, se := range seasons {
			eps, _ := st.Children(ctx, se.ID)
			if len(eps) == 0 {
				t.Errorf("%s %s hat keine Folgen", s.Title, se.Title)
			}
		}
	}
}

func titles(items []store.Item) []string {
	out := []string{}
	for _, i := range items {
		out = append(out, i.Title)
	}
	return out
}

// Gemeldet: „Bibliotheken auf dem Server sieht man keinen Fortschritt."
// Der Stand muss während des Durchlaufs eine Gesamtzahl kennen — ohne
// sie liesse sich kein Anteil zeigen, nur ein Kreisel.
func TestFortschrittKenntDieGesamtzahl(t *testing.T) {
	wurzel := t.TempDir()
	for _, p := range []string{
		"Die Bucht (2019)/Die Bucht (2019).mkv",
		"Nachtzug (2021)/Nachtzug (2021).mp4",
		"Notizen/liste.txt",
	} {
		voll := filepath.Join(wurzel, p)
		if err := os.MkdirAll(filepath.Dir(voll), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(voll, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if n := zaehlenSchnell(wurzel); n != 2 {
		t.Fatalf("Vorlauf zählt %d statt 2 Videodateien", n)
	}

	// Ausserhalb eines Durchlaufs ist der Stand leer — und sagt das auch.
	if f := New(nil, nil).Stand("gibtsnicht"); f.Laeuft {
		t.Fatal("ohne Durchlauf soll nichts laufen")
	}
}

// Wer beim Anlegen „Filme" stehen lässt und auf einen Serienordner
// zeigt, soll trotzdem seine Serie bekommen. Vorher wurden aus fünf
// Folgen ein Film mit fünf Dateien — und nirgends stand, warum.
func TestFolgenBleibenFolgenAuchInEinerFilmbibliothek(t *testing.T) {
	ctx := t.Context()
	st, err := store.Open(filepath.Join(t.TempDir(), "l.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	wurzel := t.TempDir()
	for _, p := range []string{
		"Nordlicht/Staffel 01/Nordlicht S01E01 Ankunft.mkv",
		"Nordlicht/Staffel 01/Nordlicht S01E02 Sturm.mkv",
		"Nordlicht/Staffel 02/Nordlicht S02E01 Rückkehr.mkv",
	} {
		voll := filepath.Join(wurzel, p)
		if err := os.MkdirAll(filepath.Dir(voll), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(voll, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Art: „movie" — der Fehler, der gemeldet wurde.
	lib, err := st.CreateLibrary(ctx, "Vertan", "movie", wurzel, "de")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := New(st, nil).Scan(ctx, *lib); err != nil {
		t.Fatal(err)
	}

	serien, err := st.Query(ctx, store.Query{
		Libraries: []string{lib.ID}, Kinds: []string{"series"}, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(serien) != 1 || serien[0].Title != "Nordlicht" {
		t.Fatalf("keine Serie entstanden: %v", titles(serien))
	}
	filme, err := st.Query(ctx, store.Query{
		Libraries: []string{lib.ID}, Kinds: []string{"movie"}, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(filme) != 0 {
		t.Fatalf("aus den Folgen wurden Filme: %v", titles(filme))
	}
	staffeln, _ := st.Children(ctx, serien[0].ID)
	if len(staffeln) != 2 {
		t.Fatalf("%d Staffeln statt zwei", len(staffeln))
	}
}

// Der Ordner ist da, aber leer. Das kann stimmen — oder die Platte
// fehlt am Einhängepunkt. Im Zweifel wird nichts als verschwunden
// vermerkt: falsche Löschungen nehmen den Leuten ihre Merklisten mit.
func TestLeererOrdnerLoeschtNichts(t *testing.T) {
	ctx := t.Context()
	st, err := store.Open(filepath.Join(t.TempDir(), "l.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	wurzel := t.TempDir()
	film := filepath.Join(wurzel, "Die Bucht (2019).mkv")
	if err := os.WriteFile(film, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	lib, err := st.CreateLibrary(ctx, "Filme", "movie", wurzel, "de")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := New(st, nil).Scan(ctx, *lib); err != nil {
		t.Fatal(err)
	}

	// Der Einhängepunkt steht leer da.
	if err := os.Remove(film); err != nil {
		t.Fatal(err)
	}
	res, err := New(st, nil).Scan(ctx, *lib)
	if err != nil {
		t.Fatal(err)
	}
	if res.Missing != 0 {
		t.Fatalf("%d Titel für verschwunden erklärt, obwohl gar nichts gefunden wurde", res.Missing)
	}
	noch, err := st.Query(ctx, store.Query{Libraries: []string{lib.ID}, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(noch) != 1 {
		t.Fatalf("%d Titel übrig statt einem", len(noch))
	}

	// Fehlt der Ordner ganz, wird gar nicht erst gearbeitet.
	if err := os.RemoveAll(wurzel); err != nil {
		t.Fatal(err)
	}
	if _, err := New(st, nil).Scan(ctx, *lib); err == nil {
		t.Fatal("ein Durchlauf über einen fehlenden Ordner soll scheitern")
	}
}

// Liest eine neuere Fassung einen Dateinamen anders, bleibt die Datei bei
// ihrem Titel — mit Merkliste, Stand und Listeneinträgen. Vorher entstand
// ein zweiter Titel, und der alte blieb ohne Datei zurück.
func TestBekannteDateiBleibtBeiIhremTitel(t *testing.T) {
	ctx := t.Context()
	st, err := store.Open(filepath.Join(t.TempDir(), "l.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	wurzel := t.TempDir()
	pfad := filepath.Join(wurzel, "Der Herr der Ringe - Die zwei Türme (2002).mkv")
	if err := os.WriteFile(pfad, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	lib, _ := st.CreateLibrary(ctx, "Filme", "movie", wurzel, "de")

	// So, wie die ältere Fassung ihn angelegt hatte: ohne Doppelpunkt.
	alt := &store.Item{LibraryID: lib.ID, Kind: "movie", Title: "Der Herr der Ringe Die zwei Türme",
		Year: 2002, MatchState: "pending"}
	if err := st.UpsertItem(ctx, alt); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertFile(ctx, &store.MediaFile{ItemID: alt.ID, Path: pfad, Container: "mkv"}); err != nil {
		t.Fatal(err)
	}

	if _, err := New(st, nil).Scan(ctx, *lib); err != nil {
		t.Fatal(err)
	}
	alle, _ := st.Query(ctx, store.Query{Libraries: []string{lib.ID}, Limit: 50})
	if len(alle) != 1 {
		t.Fatalf("%d Titel statt einem: %v", len(alle), titles(alle))
	}
	if alle[0].ID != alt.ID {
		t.Fatal("die Datei ist zu einem neuen Titel gewandert")
	}
	if alle[0].Title != "Der Herr der Ringe: Die zwei Türme" {
		t.Errorf("Titel nicht nachgeführt: %q", alle[0].Title)
	}
}

// Die häufigste Ablage: „Serie/Staffel 1/01 - Pilot.mkv". Daraus muss eine
// Serie mit Staffel und Folgen werden — und nicht ein Film namens „01: Pilot"
// in der Filmübersicht. Eine Datei ohne Nummer bekommt die nächste freie.
func TestStaffelordnerMitNummernVorn(t *testing.T) {
	ctx := t.Context()
	st, err := store.Open(filepath.Join(t.TempDir(), "l.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	wurzel := t.TempDir()
	for _, p := range []string{
		"Morgengrau (2023)/Staffel 1/01 - Pilot.mkv",
		"Morgengrau (2023)/Staffel 1/02 - Der Fund.mkv",
		"Morgengrau (2023)/Staffel 1/Making-of.mkv",
		"Morgengrau (2023)/Staffel 2/Morgengrau - 01 - Rückkehr.mkv",
	} {
		voll := filepath.Join(wurzel, p)
		if err := os.MkdirAll(filepath.Dir(voll), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(voll, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	lib, _ := st.CreateLibrary(ctx, "Serien", "series", wurzel, "de")
	if _, err := New(st, nil).Scan(ctx, *lib); err != nil {
		t.Fatal(err)
	}
	filme, _ := st.Query(ctx, store.Query{Libraries: []string{lib.ID}, Kinds: []string{"movie"}, Limit: 50})
	if len(filme) != 0 {
		t.Fatalf("aus Folgen wurden Filme: %v", titles(filme))
	}
	serien, _ := st.Query(ctx, store.Query{Libraries: []string{lib.ID}, Kinds: []string{"series"}, Limit: 50})
	if len(serien) != 1 || serien[0].Title != "Morgengrau" || serien[0].Year != 2023 {
		t.Fatalf("Serie: %+v", serien)
	}
	staffeln, _ := st.Children(ctx, serien[0].ID)
	if len(staffeln) != 2 {
		t.Fatalf("%d Staffeln statt 2", len(staffeln))
	}
	folgen, _ := st.Children(ctx, staffeln[0].ID)
	if len(folgen) != 3 {
		t.Fatalf("%d Folgen in Staffel 1 statt 3: %v", len(folgen), titles(folgen))
	}
	var nummern []int
	for _, f := range folgen {
		nummern = append(nummern, f.EpisodeNo)
	}
	if nummern[0] != 1 || nummern[1] != 2 || nummern[2] != 3 {
		t.Errorf("Folgennummern %v statt 1 2 3", nummern)
	}
	if folgen[0].Title != "Pilot" || folgen[1].Title != "Der Fund" || folgen[2].Title != "Making-of" {
		t.Errorf("Folgennamen aus der Datei fehlen: %v", titles(folgen))
	}
}

// Die Sammlung, an der die Zuordnung Anfang November scheiterte, im
// Kleinen: Dateinamen, die innerhalb einer Serie verschieden geschrieben
// sind, Staffelordner mit dem Seriennamen davor, und Überbleibsel älterer
// Fassungen („The" statt „The German"). Nach einem Durchlauf und dem
// Aufräumen steht jede Serie genau einmal da.
func TestSammlungMitUneinheitlichenNamen(t *testing.T) {
	ctx := t.Context()
	st, err := store.Open(filepath.Join(t.TempDir(), "l.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	wurzel := t.TempDir()
	dateien := []string{
		"The Blacklist/The Blacklist - S01E01 - Pilot - mkv - by Videomann.mkv",
		"The Blacklist/The.Blacklist - S03E23 - Alexander Kirk.mkv",
		"Grey's Anatomy/Grey's Anatomy - S08E01.avi",
		"Grey's Anatomy/Greys Anatomy - Staffel 06/s06e16.avi",
		"Star Trek - Raumschiff Voyager - Staffel 01/1x01+1x02.001.Star.Trek.Voyager-Der.Fuersorger.avi",
		"Star Trek - Raumschiff Voyager - Staffel 02/2x01.Star.Trek.Voyager-Die.37er.avi",
		"The German/The German - S01E01.mkv",
		"The German/The German - S01E02.mkv",
		"Agents of S.H.I.E.L.D/Agents of S.H.I.E.L.D. - S01E01 - Pilot.mkv",
		"Agents of S.H.I.E.L.D/Marvel Agents of S.H.I.E.L.D - S04E01 - Ghost Rider.mkv",
	}
	for _, d := range dateien {
		p := filepath.Join(wurzel, d)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	lib, _ := st.CreateLibrary(ctx, "Serien", "series", wurzel, "de")

	// Was eine ältere Fassung angelegt hatte: „The" mit der ersten Folge.
	alt := &store.Item{LibraryID: lib.ID, Kind: "series", Title: "The", MatchState: "unmatched"}
	_ = st.UpsertItem(ctx, alt)
	altSt := &store.Item{LibraryID: lib.ID, Kind: "season", ParentID: alt.ID, Title: "Staffel 1", SeasonNo: 1, MatchState: "matched"}
	_ = st.UpsertItem(ctx, altSt)
	altEp := &store.Item{LibraryID: lib.ID, Kind: "episode", ParentID: altSt.ID, Title: "Folge 1", SeasonNo: 1, EpisodeNo: 1, MatchState: "pending"}
	_ = st.UpsertItem(ctx, altEp)
	_ = st.UpsertFile(ctx, &store.MediaFile{ItemID: altEp.ID, Path: filepath.Join(wurzel, dateien[6]), Container: "mkv"})

	if _, err := New(st, nil).Scan(ctx, *lib); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SerienZusammenfuehren(ctx); err != nil {
		t.Fatal(err)
	}
	serien, _ := st.Query(ctx, store.Query{Libraries: []string{lib.ID}, Kinds: []string{"series"}, Limit: 50})
	want := map[string]int{ // Titel → Zahl der Staffeln
		"The Blacklist": 2, "Grey's Anatomy": 2, "Star Trek: Raumschiff Voyager": 2,
		"The German": 1, "Agents of S.H.I.E.L.D": 2,
	}
	if len(serien) != len(want) {
		t.Fatalf("%d Serien statt %d: %v", len(serien), len(want), titles(serien))
	}
	for _, s := range serien {
		n, ok := want[s.Title]
		if !ok {
			t.Errorf("unerwartete Serie %q", s.Title)
			continue
		}
		staffeln, _ := st.Children(ctx, s.ID)
		if len(staffeln) != n {
			t.Errorf("%s: %d Staffeln statt %d", s.Title, len(staffeln), n)
		}
	}
}

// „(1982) Star Trek II - Der Zorn des Khan.avi": Ältere Fassungen lasen das
// Jahr vorne nicht und nannten alle Teile nach dem Ordner „Star Trek" —
// teils als ein Eintrag mit drei „Fassungen". Ein Durchlauf teilt das
// wieder auf, jeder Film bekommt seinen Namen und wird neu nachgeschlagen.
func TestJahrVorneTeiltAltenEintragAuf(t *testing.T) {
	ctx := t.Context()
	st, err := store.Open(filepath.Join(t.TempDir(), "l.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	wurzel := t.TempDir()
	namen := []string{
		"(1982) Star Trek II - Der Zorn des Khan.avi",
		"(1984) Star Trek III - Auf der Suche nach Mr. Spock.avi",
		"(1989) Star Trek V - Am Rande des Universums.avi",
	}
	_ = os.MkdirAll(filepath.Join(wurzel, "Star Trek"), 0o755)
	lib, _ := st.CreateLibrary(ctx, "Filme", "movie", wurzel, "de")
	alt := &store.Item{LibraryID: lib.ID, Kind: "movie", Title: "Star Trek", Year: 1982, MatchState: "unmatched"}
	_ = st.UpsertItem(ctx, alt)
	for _, n := range namen {
		p := filepath.Join(wurzel, "Star Trek", n)
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		_ = st.UpsertFile(ctx, &store.MediaFile{ItemID: alt.ID, Path: p, Container: "avi"})
	}
	if _, err := New(st, nil).Scan(ctx, *lib); err != nil {
		t.Fatal(err)
	}
	_, _ = st.FilmeOhneDatei(ctx)
	filme, _ := st.Query(ctx, store.Query{Libraries: []string{lib.ID}, Kinds: []string{"movie"}, Limit: 50})
	want := map[string]int{
		"Star Trek II: Der Zorn des Khan": 1982, "Star Trek III: Auf der Suche nach Mr. Spock": 1984,
		"Star Trek V: Am Rande des Universums": 1989,
	}
	if len(filme) != 3 {
		t.Fatalf("%d Filme statt 3: %v", len(filme), titles(filme))
	}
	for _, f := range filme {
		if j, ok := want[f.Title]; !ok || j != f.Year {
			t.Errorf("unerwartet: %q (%d)", f.Title, f.Year)
		}
		if f.MatchState != "pending" {
			t.Errorf("%s: %s — soll neu nachgeschlagen werden", f.Title, f.MatchState)
		}
	}
}
