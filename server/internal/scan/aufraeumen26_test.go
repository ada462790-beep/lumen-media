// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package scan

// Fälle vom NAS des Besitzers (v0.1.26): zwei Bond-Filme gleicher Länge
// an einem Eintrag, ein Zweiteiler als zwei Filme, Folgen, die nach dem
// Umbenennen als „fehlend" stehen blieben.

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"lumen/internal/store"
)

// Verschieden benannte Dateien in derselben Sprache sind verschiedene
// Filme — auch wenn sie fast gleich lang sind und dieselbe Zahl („007")
// und dasselbe Jahr im Namen tragen.
func TestGleicheSpracheVerschiedeneTitelGetrennt(t *testing.T) {
	werdenGetrenntSpr(t, "Sag niemals nie", "Never Say Never Again",
		[]int64{8_040_000, 7_920_000}, []string{"de", "de"},
		"_James Bond 007/(1983) 007 - Sag niemals nie.mkv",
		"_James Bond 007/(1983) 007 - Octopussy.mkv")
	// Ohne gemessene Spuren gilt die Laufzeit allein auch nicht mehr als
	// Beleg: „Spirited Away" und „Chihiros Reise" sind dann zwei Einträge,
	// bis die Spuren bekannt sind oder jemand sie von Hand zusammenlegt.
	werdenGetrenntSpr(t, "Sag niemals nie", "Never Say Never Again",
		[]int64{8_040_000, 7_920_000}, nil,
		"_James Bond 007/(1983) 007 - Sag niemals nie.mkv",
		"_James Bond 007/(1983) 007 - Octopussy.mkv")
}

// Titel und Originaltitel bleiben ein Film — gleiche Sprache und
// verschiedene Laufzeit hin oder her.
func TestTitelUndOriginaltitelBleibenZusammen(t *testing.T) {
	bleibenZusammenSpr(t, "Sag niemals nie", "Never Say Never Again",
		[]int64{8_040_000, 7_000_000}, []string{"de", "de"}, nil,
		"_James Bond 007/(1983) 007 - Sag niemals nie.mkv",
		"_James Bond 007/(1983) 007 - Never Say Never Again.mkv")
}

// Verschiedene Tonsprachen und passende Laufzeit: eine Sprachfassung —
// auch mit PAL-Beschleunigung (25 statt 23,976 Bilder: gut 4 % kürzer).
func TestSprachfassungMitPalBeschleunigung(t *testing.T) {
	bleibenZusammenSpr(t, "Chihiros Reise ins Zauberland", "千と千尋の神隠し",
		[]int64{7_500_000, 7_500_000 * 23976 / 25000}, []string{"de", "en"}, nil,
		"Chihiro (2001)/Chihiros Reise ins Zauberland (2001).mkv",
		"Chihiro (2001)/Spirited Away (2001).mkv")
	// Verschiedene Sprache, aber die Laufzeit passt gar nicht (> 5 %).
	werdenGetrenntSpr(t, "Sag niemals nie", "Never Say Never Again",
		[]int64{8_040_000, 7_200_000}, []string{"de", "en"},
		"_James Bond 007/(1983) 007 - Sag niemals nie.mkv",
		"_James Bond 007/(1983) 007 - Octopussy.mkv")
}

// Zahlen, die in beiden Namen gleich vorkommen, machen zwei Titel nicht
// ähnlicher.
func TestGemeinsameZahlenSindKeinBeleg(t *testing.T) {
	if w := titelNah("(1983) 007 - Octopussy", "007 - Sag niemals nie (1983)"); w != 0 {
		t.Errorf("titelNah mit nur gemeinsamen Zahlen = %v, erwartet 0", w)
	}
	if w := titelNah("007 - Never Say Never Again", "Never Say Never Again"); w < 0.99 {
		t.Errorf("titelNah(Never Say Never Again) = %v, erwartet 1", w)
	}
}

// filmeMitTeilen: Titel → Dateinamen in der Reihenfolge der Teile.
func filmeMitTeilen(t *testing.T, st *store.Store, libID string) map[string][]string {
	t.Helper()
	ctx := context.Background()
	out := map[string][]string{}
	filme, _ := st.Query(ctx, store.Query{Libraries: []string{libID}, Kinds: []string{"movie"}, Limit: 100})
	for _, f := range filme {
		dateien, _ := st.FilesFor(ctx, f.ID)
		if len(dateien) == 0 {
			continue
		}
		sort.Slice(dateien, func(i, j int) bool { return dateien[i].PartNo < dateien[j].PartNo })
		for _, d := range dateien {
			out[f.Title] = append(out[f.Title], filepath.Base(d.Path)+"#"+faelleZahl(d.PartNo))
		}
	}
	return out
}

// Gleicher Grundtitel, „Teil/Part/Vol. N" und gleiches Jahr: ein Film in
// Teilen. Verschiedene Jahre (oder eins fehlt): verschiedene Filme.
func TestTeileMitGleichemJahrSindEinFilm(t *testing.T) {
	root := t.TempDir()
	faelleAnlegen(t, root,
		"_Die Pfeiler der Macht/Die Pfeiler der Macht - Teil 1 (2016).mkv",
		"_Die Pfeiler der Macht/Die Pfeiler der Macht - Teil 2 (2016).mkv",
		"_Dune/Dune - Part One (2021).mkv",
		"_Dune/Dune - Part Two (2024).mkv",
		"_Kill Bill/Kill Bill Vol. 1 (2003).mkv",
		"_Kill Bill/Kill Bill Vol. 2 (2004).mkv",
		"_Harry Potter/Harry Potter und die Heiligtümer des Todes - Teil 1 (2010).mkv",
		"_Harry Potter/Harry Potter und die Heiligtümer des Todes - Teil 2 (2011).mkv",
		"_Ohne Jahr/Der Zweiteiler - Teil 1 (2016).mkv",
		"_Ohne Jahr/Der Zweiteiler - Teil 2.mkv",
		"_Roemisch/Das Werk - Teil I (2019).mkv",
		"_Roemisch/Das Werk - Teil II (2019).mkv",
		"_Saga/Die Saga Vol. 1 (2005).mkv",
		"_Saga/Die Saga Volume Two (2005).mkv",
	)
	st := faelleStore(t)
	ctx := context.Background()
	lib, _ := st.CreateLibrary(ctx, "Filme", "movie", root, "de")
	sc := New(st, nil)
	for i := 0; i < 2; i++ {
		if _, err := sc.Scan(ctx, *lib); err != nil {
			t.Fatal(err)
		}
	}
	ist := filmeMitTeilen(t, st, lib.ID)
	soll := map[string][]string{
		"Die Pfeiler der Macht": {"Die Pfeiler der Macht - Teil 1 (2016).mkv#1", "Die Pfeiler der Macht - Teil 2 (2016).mkv#2"},
		"Das Werk":              {"Das Werk - Teil I (2019).mkv#1", "Das Werk - Teil II (2019).mkv#2"},
		"Die Saga":              {"Die Saga Vol. 1 (2005).mkv#1", "Die Saga Volume Two (2005).mkv#2"},
	}
	for titel, dateien := range soll {
		if len(ist[titel]) != len(dateien) || ist[titel][0] != dateien[0] || ist[titel][1] != dateien[1] {
			t.Errorf("%q: %v, erwartet %v", titel, ist[titel], dateien)
		}
	}
	// Alle übrigen: je eine Datei, Teil 1.
	einzeln := 0
	for titel, dateien := range ist {
		if _, ok := soll[titel]; ok {
			continue
		}
		if len(dateien) != 1 {
			t.Errorf("%q: %v, erwartet eine Datei", titel, dateien)
		}
		einzeln++
	}
	if einzeln != 8 {
		t.Errorf("%d einzelne Filme, erwartet 8: %v", einzeln, ist)
	}
	// Das Aufräumen zeigt den Zweiteiler unter „Filme in Teilen".
	dups, _ := st.Duplicates(ctx)
	teile := 0
	for _, d := range dups {
		if d.Art == "teile" {
			teile++
		}
	}
	if teile != 3 {
		t.Errorf("%d Filme in Teilen, erwartet 3", teile)
	}
}

// So stand „Die Pfeiler der Macht" auf dem NAS: beide Teile an einem
// erkannten Eintrag, als „verschiedene Filme an einem Eintrag". Der
// nächste Durchlauf macht Teile daraus — ohne den Eintrag anzutasten.
func TestPfeilerAlsEinEintragWirdZuTeilen(t *testing.T) {
	ctx := context.Background()
	st, lib, root, id := fassungenAnEinemFilm(t, "Die Pfeiler der Macht", "", []int64{zweiStunden, zweiStunden}, nil, nil,
		"_Die Pfeiler der Macht/Die Pfeiler der Macht - Teil 1 (2016).mkv",
		"_Die Pfeiler der Macht/Die Pfeiler der Macht - Teil 2 (2016).mkv")
	for _, p := range []string{"Teil 1", "Teil 2"} {
		g := st.GemessenZuPfad(ctx, filepath.Join(root, "_Die Pfeiler der Macht/Die Pfeiler der Macht - "+p+" (2016).mkv"))
		if g == nil || g.Datei.ItemID != id {
			t.Fatalf("%s hat den Eintrag verlassen: %v", p, filmeMitTeilen(t, st, lib.ID))
		}
		if want := int(p[len(p)-1] - '0'); g.Datei.PartNo != want {
			t.Errorf("%s: Teil %d, erwartet %d", p, g.Datei.PartNo, want)
		}
	}
	it, _ := st.Item(ctx, id)
	dateien, _ := st.FilesFor(ctx, id)
	if New(st, nil).DateienVerschieden(ctx, *lib, it, dateien) {
		t.Error("Aufräumen zeigt den Zweiteiler weiter als „verschiedene Filme“")
	}
}

// Standen die Teile als zwei erkannte Einträge da, kommt Teil 2 zu Teil 1
// — samt allem, was daran hing.
func TestGetrennteTeileFindenZusammen(t *testing.T) {
	root := t.TempDir()
	faelleAnlegen(t, root,
		"_Die Pfeiler der Macht/Die Pfeiler der Macht - Teil 1 (2016).mkv",
		"_Die Pfeiler der Macht/Die Pfeiler der Macht - Teil 2 (2016).mkv")
	st := faelleStore(t)
	ctx := context.Background()
	lib, _ := st.CreateLibrary(ctx, "Filme", "movie", root, "de")
	sc := New(st, nil)
	if _, err := sc.Scan(ctx, *lib); err != nil {
		t.Fatal(err)
	}
	// Wie unter v0.1.26: zwei Einträge, beide erkannt.
	a := st.GemessenZuPfad(ctx, filepath.Join(root, "_Die Pfeiler der Macht/Die Pfeiler der Macht - Teil 1 (2016).mkv"))
	b := st.GemessenZuPfad(ctx, filepath.Join(root, "_Die Pfeiler der Macht/Die Pfeiler der Macht - Teil 2 (2016).mkv"))
	zweiter := &store.Item{LibraryID: lib.ID, Kind: "movie", Title: "Die Pfeiler der Macht: Teil 2", Year: 2016, MatchState: "matched"}
	if err := st.UpsertItem(ctx, zweiter); err != nil {
		t.Fatal(err)
	}
	f := b.Datei
	f.ItemID, f.PartNo = zweiter.ID, 1
	_ = st.UpsertFile(ctx, &f)
	erster, _ := st.Item(ctx, a.Datei.ItemID)
	erster.MatchState = "matched"
	_ = st.UpsertItem(ctx, erster)
	_ = filmeOhneDateiWeg(ctx, st)
	if n := len(filmeMitTeilen(t, st, lib.ID)); n != 2 {
		t.Fatalf("Ausgangslage: %d Einträge, erwartet 2", n)
	}
	if _, err := sc.Scan(ctx, *lib); err != nil {
		t.Fatal(err)
	}
	ist := filmeMitTeilen(t, st, lib.ID)
	if len(ist) != 1 || len(ist[erster.Title]) != 2 {
		t.Fatalf("nach dem Durchlauf: %v, erwartet ein Eintrag mit zwei Teilen", ist)
	}
}

// „Sind verschiedene Filme" und „Als Teile zusammenfügen" gelten auch
// nach dem nächsten Durchlauf.
func TestTeileVonHandBleiben(t *testing.T) {
	root := t.TempDir()
	faelleAnlegen(t, root,
		"_Die Pfeiler der Macht/Die Pfeiler der Macht - Teil 1 (2016).mkv",
		"_Die Pfeiler der Macht/Die Pfeiler der Macht - Teil 2 (2016).mkv",
		"_Shoah/Shoah - Erste Epoche (1985).mkv",
		"_Shoah/Shoah - Zweite Epoche (1985).mkv")
	st := faelleStore(t)
	ctx := context.Background()
	lib, _ := st.CreateLibrary(ctx, "Filme", "movie", root, "de")
	sc := New(st, nil)
	if _, err := sc.Scan(ctx, *lib); err != nil {
		t.Fatal(err)
	}
	// „Sind verschiedene Filme"
	p1 := st.GemessenZuPfad(ctx, filepath.Join(root, "_Die Pfeiler der Macht/Die Pfeiler der Macht - Teil 1 (2016).mkv"))
	if p1 == nil {
		t.Fatal("Teil 1 unbekannt")
	}
	if _, err := st.TeileTrennen(ctx, p1.Datei.ItemID, func(f store.MediaFile) (string, int) {
		g := Parse(f.Path, nil)
		return g.Title, g.Year
	}); err != nil {
		t.Fatal(err)
	}
	// „Als Teile zusammenfügen"
	e1 := st.GemessenZuPfad(ctx, filepath.Join(root, "_Shoah/Shoah - Erste Epoche (1985).mkv"))
	e2 := st.GemessenZuPfad(ctx, filepath.Join(root, "_Shoah/Shoah - Zweite Epoche (1985).mkv"))
	if e1.Datei.ItemID == e2.Datei.ItemID {
		t.Fatal("Ausgangslage: Shoah ist schon ein Eintrag")
	}
	if err := st.TeileFestlegen(ctx, e1.Datei.ItemID, []string{e1.Datei.ID, e2.Datei.ID}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := sc.Scan(ctx, *lib); err != nil {
			t.Fatal(err)
		}
	}
	ist := filmeMitTeilen(t, st, lib.ID)
	if len(ist) != 3 {
		t.Fatalf("nach den Durchläufen: %v, erwartet 3 Einträge", ist)
	}
	for titel, d := range ist {
		switch len(d) {
		case 1:
			if d[0][len(d[0])-2:] != "#1" {
				t.Errorf("%q: %v — abgetrennt sollte Teil 1 sein", titel, d)
			}
		case 2:
			if d[0] != "Shoah - Erste Epoche (1985).mkv#1" || d[1] != "Shoah - Zweite Epoche (1985).mkv#2" {
				t.Errorf("%q: %v", titel, d)
			}
		default:
			t.Errorf("%q: %v", titel, d)
		}
	}
}

func TestTeilAngabe(t *testing.T) {
	for in, soll := range map[string]int{
		"Die Pfeiler der Macht - Teil 1": 1,
		"Dune - Part Two":                2,
		"Kill Bill Vol. 2":               2,
		"Die Saga Volume Two":            2,
		"Das Werk - Teil II":             2,
		"Der Hobbit Teil drei":           3,
		"System Volume Information":      0,
		"Teilzeit":                       0,
		"Ein Teil eines Ganzen":          0,
	} {
		if _, nr := teilAngabe(in); nr != soll {
			t.Errorf("teilAngabe(%q) = %d, erwartet %d", in, nr, soll)
		}
	}
}

// Eine Folge, umbenannt und dabei durch eine andere Datei ersetzt (andere
// Grösse): Die alte Zeile stand bisher für immer unter „Fehlende
// Dateien". Jetzt trägt der Durchlauf sie aus — die Folge ist ja da.
func TestUmbenannteFolgeVerschwindetAusFehlend(t *testing.T) {
	root := t.TempDir()
	alt := "The Big Bang Theory/The Big Bang Theory - Staffel 01/The Big Bang Theory S01E05.mkv"
	faelleAnlegen(t, root, alt,
		"The Big Bang Theory/The Big Bang Theory - Staffel 01/The Big Bang Theory S01E06.mkv")
	st := faelleStore(t)
	ctx := context.Background()
	lib, _ := st.CreateLibrary(ctx, "Serien", "series", root, "de")
	sc := New(st, nil)
	if _, err := sc.Scan(ctx, *lib); err != nil {
		t.Fatal(err)
	}
	vorher := st.GemessenZuPfad(ctx, filepath.Join(root, alt))
	if vorher == nil {
		t.Fatal("Folge unbekannt")
	}
	// Umbenannt und ersetzt (andere Grösse).
	neu := "The Big Bang Theory/The Big Bang Theory - Staffel 01/The Big Bang Theory - S01E05 - The Luminous Fish Effect.mkv"
	if err := os.Remove(filepath.Join(root, alt)); err != nil {
		t.Fatal(err)
	}
	faelleAnlegen(t, root, neu)
	if _, err := sc.Scan(ctx, *lib); err != nil {
		t.Fatal(err)
	}
	if g := st.GemessenZuPfad(ctx, filepath.Join(root, alt)); g != nil {
		t.Errorf("die alte Datei steht noch da (fehlend=%v)", g.Datei.Missing)
	}
	g := st.GemessenZuPfad(ctx, filepath.Join(root, neu))
	if g == nil || g.Datei.ItemID != vorher.Datei.ItemID {
		t.Fatalf("die neue Datei hängt nicht an derselben Folge")
	}
	if fehlend, _ := st.MissingFiles(ctx); len(fehlend) != 0 {
		t.Errorf("%d fehlende Dateien, erwartet keine", len(fehlend))
	}
}

// Eine Fassung auf einer abgehängten Platte ist keine Umbenennung: Ihr
// Ordner fehlt, und sie liegt unter einem anderen obersten Ordner.
func TestFehlendeFassungAndererOrdnerBleibt(t *testing.T) {
	root := t.TempDir()
	faelleAnlegen(t, root,
		"Brust oder Keule (1976)/Brust oder Keule (1976).mkv",
		"_Louis de Funes/Brust oder Keule (1976).mkv")
	st := faelleStore(t)
	ctx := context.Background()
	lib, _ := st.CreateLibrary(ctx, "Filme", "movie", root, "de")
	sc := New(st, nil)
	if _, err := sc.Scan(ctx, *lib); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(root, "_Louis de Funes")); err != nil {
		t.Fatal(err)
	}
	if _, err := sc.Scan(ctx, *lib); err != nil {
		t.Fatal(err)
	}
	fehlend, _ := st.MissingFiles(ctx)
	if len(fehlend) != 1 {
		t.Fatalf("%d fehlende Dateien, erwartet 1", len(fehlend))
	}
	angaben, err := st.FehlendeAngaben(ctx, []string{fehlend[0].ID})
	if err != nil {
		t.Fatal(err)
	}
	a := angaben[fehlend[0].ID]
	if a.Titel != "Brust oder Keule" || !a.Ersatz {
		t.Errorf("Angaben: %+v", a)
	}
}

func TestTeileKandidaten(t *testing.T) {
	d := func(id, item, pfad string) store.FilmDatei { return store.FilmDatei{ID: id, ItemID: item, Path: pfad} }
	k := TeileKandidaten([]store.FilmDatei{
		d("1", "a", "/m/_P/Die Pfeiler der Macht - Teil 2 (2016).mkv"),
		d("2", "b", "/m/_P/Die Pfeiler der Macht - Teil 1 (2016).mkv"),
		d("3", "c", "/m/_D/Dune - Part One (2021).mkv"),
		d("4", "d", "/m/_D/Dune - Part Two (2024).mkv"),
		d("5", "e", "/m/_Z/Der Zweiteiler - Teil 1 (2016).mkv"),
		d("6", "f", "/m/_Z/Der Zweiteiler - Teil 2.mkv"),
		d("7", "g", "/m/_S/Saga Teil 1 (2001).mkv"),
		d("8", "g", "/m/_S/Saga Teil 2 (2001).mkv"),
	})
	if len(k) != 2 {
		t.Fatalf("%d Kandidaten, erwartet 2 (Pfeiler, Zweiteiler): %v", len(k), k)
	}
	if k[0][0].ID != "2" || k[0][1].ID != "1" {
		t.Errorf("Reihenfolge: %v", k[0])
	}
}
