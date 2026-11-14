// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package scan

// Fälle vom NAS eines Besitzers: Folgen, die unter „Aufräumen" statt in
// ihrer Serie landeten, Filme eines Sammelordners, die als Fassungen
// eines Films galten, und Serien, die nach jedem Durchlauf doppelt
// entstanden.

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"lumen/internal/store"
)

func faelleAnlegen(t *testing.T, root string, pfade ...string) {
	t.Helper()
	for _, p := range pfade {
		voll := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(voll), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(voll, []byte(p), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func faelleStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "l.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

// folgenMitStaffel: „Staffeltitel/S<nr>E<nr> Titel" je Folge mit Datei.
func folgenMitStaffel(t *testing.T, st *store.Store, serieID string) []string {
	t.Helper()
	ctx := context.Background()
	var out []string
	staffeln, _ := st.Children(ctx, serieID)
	for _, s := range staffeln {
		folgen, _ := st.Children(ctx, s.ID)
		for _, f := range folgen {
			if dateien, _ := st.FilesFor(ctx, f.ID); len(dateien) == 0 {
				continue
			}
			out = append(out, s.Title+"|"+faelleZahl(f.SeasonNo)+"x"+faelleZahl(f.EpisodeNo)+"|"+f.Title)
		}
	}
	sort.Strings(out)
	return out
}

func faelleZahl(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for ; n > 0; n /= 10 {
		s = string(rune('0'+n%10)) + s
	}
	return s
}

func TestSpecialsBleibenInIhrerSerie(t *testing.T) {
	root := t.TempDir()
	faelleAnlegen(t, root,
		"Game of Thrones/Game of Thrones - Staffel 08/Game of Thrones - S08E01 - Winterfell.mkv",
		"Game of Thrones/Game of Thrones - Staffel 08/Game of Thrones - S08E02 - A Knight.mkv",
		"Game of Thrones/Game of Thrones - Staffel 08/Game of Thrones - S08E00 - The Last Watch.mkv",
		"Knight Rider 2008/Knight Rider 2008 - Staffel 01/Knight Rider 2008 - S01E01 - Knight of the Living Dead.avi",
		"Knight Rider 2008/Knight Rider 2008 - Staffel 01/Knight Rider 2008 - S01E00 -.avi",
		"Inside Man/Inside Man - Staffel 01/Inside Man - SS01E01.mkv",
		"Inside Man/Inside Man - Staffel 01/Inside Man - SS01E02.mkv",
	)
	st := faelleStore(t)
	ctx := context.Background()
	lib, _ := st.CreateLibrary(ctx, "Serien", "series", root, "de")
	sc := New(st, nil)
	for i := 0; i < 2; i++ { // der zweite Durchlauf darf nichts umnummerieren
		if _, err := sc.Scan(ctx, *lib); err != nil {
			t.Fatal(err)
		}
	}
	serien, _ := st.Query(ctx, store.Query{Libraries: []string{lib.ID}, Kinds: []string{"series"}, Limit: 50})
	if len(serien) != 3 {
		t.Fatalf("%d Serien, erwartet 3", len(serien))
	}
	erwartet := map[string][]string{
		"Game of Thrones": {"Specials|0x1|The Last Watch", "Staffel 8|8x1|Winterfell", "Staffel 8|8x2|A Knight"},
		"Knight Rider":    {"Specials|0x1|Special", "Staffel 1|1x1|Knight of the Living Dead"},
		"Inside Man":      {"Staffel 1|1x1|Folge 1", "Staffel 1|1x2|Folge 2"},
	}
	for _, s := range serien {
		ist := folgenMitStaffel(t, st, s.ID)
		soll := erwartet[s.Title]
		sort.Strings(soll)
		if len(ist) != len(soll) {
			t.Errorf("%s: %v, erwartet %v", s.Title, ist, soll)
			continue
		}
		for i := range ist {
			if ist[i] != soll[i] {
				t.Errorf("%s: %v, erwartet %v", s.Title, ist, soll)
				break
			}
		}
	}
	// Das Special ist als solches vermerkt — die Zuordnung sucht es am Titel.
	g := st.GemessenZuPfad(ctx, filepath.Join(root, "Game of Thrones/Game of Thrones - Staffel 08/Game of Thrones - S08E00 - The Last Watch.mkv"))
	if g == nil || !st.ZusatzZu(ctx, g.Datei.ItemID).Special {
		t.Fatalf("Special nicht vermerkt")
	}
	// Kennt TMDb eine Folge nicht, ist sie trotzdem kein Fall fürs Aufräumen.
	ep, _ := st.Item(ctx, g.Datei.ItemID)
	ep.MatchState = "unmatched"
	_ = st.UpsertItem(ctx, ep)
	if liste, _ := st.UnmatchedItems(ctx, 50); len(liste) != 0 {
		t.Fatalf("Folge unter „nicht erkannt“: %+v", liste)
	}
	if n := st.UnmatchedCount(ctx); n != 0 {
		t.Fatalf("UnmatchedCount %d", n)
	}
}

// filmeMitDateien: Titel → Dateinamen (nur Filme mit Dateien).
func filmeMitDateien(t *testing.T, st *store.Store, libID string) map[string][]string {
	t.Helper()
	ctx := context.Background()
	filme, _ := st.Query(ctx, store.Query{Libraries: []string{libID}, Kinds: []string{"movie"}, Limit: 100})
	out := map[string][]string{}
	for _, f := range filme {
		dateien, _ := st.FilesFor(ctx, f.ID)
		for _, d := range dateien {
			out[f.Title] = append(out[f.Title], filepath.Base(d.Path))
		}
		sort.Strings(out[f.Title])
	}
	return out
}

// Ein Sammelordner mit verschiedenen Filmen desselben Jahres: Eine ältere
// Fassung hatte alle Dateien an einen Film gehängt, und weil der erkannt
// war, blieb es dabei — die Seite Aufräumen zeigte „2 Fassungen". Der
// nächste Durchlauf trennt sie wieder; die Datei, die zum erkannten Titel
// passt, bleibt bei ihm (samt Stand der Leute).
func TestFalschZusammengelegteFilmeWerdenGetrennt(t *testing.T) {
	root := t.TempDir()
	faelleAnlegen(t, root,
		"_Batman/(2016) Batman Anime - Bad Blood.mkv",
		"_Batman/(2016) Batman vs. Superman - Dawn of Justice.mkv",
		"_Die Pfeiler der Macht/Die Pfeiler der Macht - Teil 1 (2016).mkv",
		"_Die Pfeiler der Macht/Die Pfeiler der Macht - Teil 2 (2016).mkv",
		"Brust oder Keule (1976)/Brust oder Keule (1976).mkv",
		"_Louis de Funes/Brust oder Keule (1976).mkv",
	)
	st := faelleStore(t)
	ctx := context.Background()
	lib, _ := st.CreateLibrary(ctx, "Filme", "movie", root, "de")
	sc := New(st, nil)
	if _, err := sc.Scan(ctx, *lib); err != nil {
		t.Fatal(err)
	}
	if n := len(filmeMitDateien(t, st, lib.ID)); n != 5 {
		t.Fatalf("nach dem ersten Durchlauf %d Filme, erwartet 5: %v", n, filmeMitDateien(t, st, lib.ID))
	}

	// So sah es auf dem NAS aus: je zwei Filme an einem erkannten Eintrag.
	zusammen := func(titel, tmdb string, pfade ...string) string {
		var id string
		for i, p := range pfade {
			g := st.GemessenZuPfad(ctx, filepath.Join(root, p))
			if g == nil {
				t.Fatalf("%s unbekannt", p)
			}
			f := g.Datei
			if i == 0 {
				id = f.ItemID
				continue
			}
			f.ItemID = id
			if err := st.UpsertFile(ctx, &f); err != nil {
				t.Fatal(err)
			}
		}
		it, _ := st.Item(ctx, id)
		it.Title, it.MatchState = titel, "matched"
		_ = st.UpsertItem(ctx, it)
		_ = st.SetSourceID(ctx, id, "tmdb", tmdb)
		return id
	}
	batman := zusammen("Batman v Superman: Dawn of Justice", "209112",
		"_Batman/(2016) Batman Anime - Bad Blood.mkv", "_Batman/(2016) Batman vs. Superman - Dawn of Justice.mkv")
	zusammen("Die Pfeiler der Macht", "111",
		"_Die Pfeiler der Macht/Die Pfeiler der Macht - Teil 1 (2016).mkv",
		"_Die Pfeiler der Macht/Die Pfeiler der Macht - Teil 2 (2016).mkv")
	_ = filmeOhneDateiWeg(ctx, st)

	for i := 0; i < 2; i++ {
		if _, err := sc.Scan(ctx, *lib); err != nil {
			t.Fatal(err)
		}
	}
	ist := filmeMitDateien(t, st, lib.ID)
	soll := map[string][]string{
		"Batman v Superman: Dawn of Justice": {"(2016) Batman vs. Superman - Dawn of Justice.mkv"},
		"Batman Anime: Bad Blood":            {"(2016) Batman Anime - Bad Blood.mkv"},
		"Die Pfeiler der Macht":              {"Die Pfeiler der Macht - Teil 1 (2016).mkv"},
		"Die Pfeiler der Macht: Teil 2":      {"Die Pfeiler der Macht - Teil 2 (2016).mkv"},
		// Echte Doppel (gleicher Name) bleiben ein Film mit zwei Dateien.
		"Brust oder Keule": {"Brust oder Keule (1976).mkv", "Brust oder Keule (1976).mkv"},
	}
	if len(ist) != len(soll) {
		t.Fatalf("Filme: %v\nerwartet %v", ist, soll)
	}
	for titel, dateien := range soll {
		if len(ist[titel]) != len(dateien) || ist[titel][0] != dateien[0] {
			t.Errorf("%q: %v, erwartet %v", titel, ist[titel], dateien)
		}
	}
	if g := st.GemessenZuPfad(ctx, filepath.Join(root, "_Batman/(2016) Batman vs. Superman - Dawn of Justice.mkv")); g == nil || g.Datei.ItemID != batman {
		t.Fatalf("die passende Datei hat den erkannten Eintrag verlassen")
	}
}

// ordnerZusammenlegen legt zwei Einträge mit derselben TMDb-Kennung nur
// zusammen, wenn auch die Dateinamen derselbe Film sind — eine falsche
// Kennung (NFO, Treffer) allein genügt nicht.
func TestOrdnerZusammenlegenNurBeiGleichemTitel(t *testing.T) {
	root := t.TempDir()
	faelleAnlegen(t, root,
		"_Batman/(2016) Batman Anime - Bad Blood.mkv",
		"_Batman/(2016) Batman vs. Superman - Dawn of Justice.mkv",
	)
	st := faelleStore(t)
	ctx := context.Background()
	lib, _ := st.CreateLibrary(ctx, "Filme", "movie", root, "de")
	sc := New(st, nil)
	if _, err := sc.Scan(ctx, *lib); err != nil {
		t.Fatal(err)
	}
	filme, _ := st.Query(ctx, store.Query{Libraries: []string{lib.ID}, Kinds: []string{"movie"}, Limit: 10})
	for _, f := range filme {
		_ = st.SetSourceID(ctx, f.ID, "tmdb", "209112")
	}
	sc.ordnerZusammenlegen(ctx, *lib, filepath.Join(root, "_Batman"))
	if n := len(filmeMitDateien(t, st, lib.ID)); n != 2 {
		t.Fatalf("%d Filme nach dem Zusammenlegen, erwartet 2", n)
	}
}

// filmeOhneDateiWeg räumt Filme ohne Datei weg (wie der Hausmeister).
func filmeOhneDateiWeg(ctx context.Context, st *store.Store) error {
	_, err := st.FilmeOhneDatei(ctx)
	return err
}

// Zwei Ordner derselben Serie mit verschiedenen Jahren: Der Scanner legt
// zwei Serien an, TMDb erkennt beide als dieselbe, der Hausmeister führt
// sie zusammen. Vorher legte der nächste Durchlauf die zweite wieder an
// (das Jahr passte nicht zur zusammengeführten Serie) — „2 doppelte
// Serien zusammengeführt" nach jedem Durchlauf, und die Folgen zogen hin
// und her. Jetzt bleibt es bei einer Serie, und der Stand bleibt stehen.
func TestZusammengefuehrteSerieBleibtEine(t *testing.T) {
	root := t.TempDir()
	faelleAnlegen(t, root,
		"Sherlock (2010)/Staffel 1/Sherlock S01E01.mkv",
		"Sherlock (2010)/Staffel 1/Sherlock S01E02.mkv",
		"Sherlock (2014)/Staffel 3/Sherlock S03E01.mkv",
	)
	st := faelleStore(t)
	ctx := context.Background()
	lib, _ := st.CreateLibrary(ctx, "Serien", "series", root, "de")
	sc := New(st, nil)
	if _, err := sc.Scan(ctx, *lib); err != nil {
		t.Fatal(err)
	}
	serien := func() []store.Item {
		x, _ := st.Query(ctx, store.Query{Libraries: []string{lib.ID}, Kinds: []string{"series"}, Limit: 50})
		return x
	}
	if n := len(serien()); n != 2 {
		t.Fatalf("%d Serien nach dem ersten Durchlauf, erwartet 2", n)
	}
	// TMDb erkennt beide als dieselbe Serie (wie die Zuordnung es täte).
	erkennen := func() {
		for _, s := range serien() {
			if s.MatchState == "matched" {
				continue
			}
			s.Title, s.Year, s.MatchState = "Sherlock", 2010, "matched"
			_ = st.UpsertItem(ctx, &s)
			_ = st.SetSourceID(ctx, s.ID, "tmdb", "19885")
		}
	}
	erkennen()
	if n, err := st.SerienZusammenfuehren(ctx); err != nil || n != 1 {
		t.Fatalf("erstes Zusammenführen: %d %v", n, err)
	}
	if len(serien()) != 1 {
		t.Fatalf("nach dem Zusammenführen %d Serien", len(serien()))
	}
	acc, err := st.CreateAccount(ctx, store.NewAccount{Name: "Mia"})
	if err != nil {
		t.Fatal(err)
	}
	g := st.GemessenZuPfad(ctx, filepath.Join(root, "Sherlock (2014)/Staffel 3/Sherlock S03E01.mkv"))
	if g == nil {
		t.Fatal("Datei unbekannt")
	}
	folge := g.Datei.ItemID
	if err := st.SaveProgress(ctx, acc.ID, folge, 600_000, 3_000_000); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 3; i++ {
		if _, err := sc.Scan(ctx, *lib); err != nil {
			t.Fatal(err)
		}
		erkennen()
		if n, _ := st.SerienZusammenfuehren(ctx); n != 0 {
			t.Fatalf("Durchlauf %d: wieder %d Serien zusammengeführt", i+2, n)
		}
	}
	if n := len(serien()); n != 1 {
		t.Fatalf("%d Serien nach drei weiteren Durchläufen, erwartet 1", n)
	}
	if g := st.GemessenZuPfad(ctx, filepath.Join(root, "Sherlock (2014)/Staffel 3/Sherlock S03E01.mkv")); g == nil || g.Datei.ItemID != folge {
		t.Fatalf("die Folge ist umgezogen")
	}
	if p, err := st.Progress(ctx, acc.ID, folge); err != nil || p == nil || p.PositionMS != 600_000 {
		t.Fatalf("Stand verloren: %+v %v", p, err)
	}
}

// Bestehende Daten: Eine ältere Fassung hatte „S08E00 - The Last Watch"
// als S08E07 abgelegt, und jemand hat sie gesehen. Nach dem Durchlauf
// steht sie unter Specials — und der Haken mit ihr.
func TestAltesSpecialNimmtDenHakenMit(t *testing.T) {
	root := t.TempDir()
	pfad := "Game of Thrones/Game of Thrones - Staffel 08/Game of Thrones - S08E00 - The Last Watch.mkv"
	faelleAnlegen(t, root, "Game of Thrones/Game of Thrones - Staffel 08/Game of Thrones - S08E01 - Winterfell.mkv", pfad)
	st := faelleStore(t)
	ctx := context.Background()
	lib, _ := st.CreateLibrary(ctx, "Serien", "series", root, "de")
	serie := &store.Item{LibraryID: lib.ID, Kind: "series", Title: "Game of Thrones", MatchState: "matched"}
	_ = st.UpsertItem(ctx, serie)
	staffel := &store.Item{LibraryID: lib.ID, Kind: "season", ParentID: serie.ID, Title: "Staffel 8", SeasonNo: 8, MatchState: "matched"}
	_ = st.UpsertItem(ctx, staffel)
	alt := &store.Item{LibraryID: lib.ID, Kind: "episode", ParentID: staffel.ID, Title: "The Last Watch",
		SeasonNo: 8, EpisodeNo: 7, MatchState: "unmatched"}
	_ = st.UpsertItem(ctx, alt)
	if err := st.UpsertFile(ctx, &store.MediaFile{ItemID: alt.ID, Path: filepath.Join(root, pfad), SizeBytes: 1, PartNo: 1}); err != nil {
		t.Fatal(err)
	}
	acc, _ := st.CreateAccount(ctx, store.NewAccount{Name: "Mia"})
	if err := st.SaveProgress(ctx, acc.ID, alt.ID, 3_000_000, 3_000_000); err != nil {
		t.Fatal(err)
	}
	sc := New(st, nil)
	if _, err := sc.Scan(ctx, *lib); err != nil {
		t.Fatal(err)
	}
	if _, err := st.FolgenOhneDatei(ctx); err != nil {
		t.Fatal(err)
	}
	g := st.GemessenZuPfad(ctx, filepath.Join(root, pfad))
	if g == nil || g.Datei.ItemID == alt.ID {
		t.Fatalf("die Datei hängt noch an S08E07")
	}
	neu, _ := st.Item(ctx, g.Datei.ItemID)
	if neu.SeasonNo != 0 {
		t.Fatalf("Special in Staffel %d", neu.SeasonNo)
	}
	if p, err := st.Progress(ctx, acc.ID, neu.ID); err != nil || p == nil || !p.Finished {
		t.Fatalf("Haken nicht mitgezogen: %+v %v", p, err)
	}
}

// Ein deutscher und ein englischer Dateiname derselben Fassung bleiben
// zusammen, wenn beide zum Titel oder Originaltitel passen.
func TestDeutscherUndEnglischerNameBleibenEinFilm(t *testing.T) {
	root := t.TempDir()
	faelleAnlegen(t, root,
		"Der Pate (1972)/Der Pate (1972).mkv",
		"Der Pate (1972)/The Godfather (1972).mkv",
	)
	st := faelleStore(t)
	ctx := context.Background()
	lib, _ := st.CreateLibrary(ctx, "Filme", "movie", root, "de")
	sc := New(st, nil)
	if _, err := sc.Scan(ctx, *lib); err != nil {
		t.Fatal(err)
	}
	a := st.GemessenZuPfad(ctx, filepath.Join(root, "Der Pate (1972)/Der Pate (1972).mkv"))
	b := st.GemessenZuPfad(ctx, filepath.Join(root, "Der Pate (1972)/The Godfather (1972).mkv"))
	if a == nil || b == nil {
		t.Fatal("Dateien unbekannt")
	}
	f := b.Datei
	f.ItemID = a.Datei.ItemID
	if err := st.UpsertFile(ctx, &f); err != nil {
		t.Fatal(err)
	}
	it, _ := st.Item(ctx, a.Datei.ItemID)
	it.Title, it.OriginalTitle, it.MatchState = "Der Pate", "The Godfather", "matched"
	_ = st.UpsertItem(ctx, it)
	_ = st.SetSourceID(ctx, it.ID, "tmdb", "238")
	_ = filmeOhneDateiWeg(ctx, st)
	for i := 0; i < 2; i++ {
		if _, err := sc.Scan(ctx, *lib); err != nil {
			t.Fatal(err)
		}
	}
	b = st.GemessenZuPfad(ctx, filepath.Join(root, "Der Pate (1972)/The Godfather (1972).mkv"))
	if b == nil || b.Datei.ItemID != it.ID {
		t.Fatalf("die englische Fassung wurde abgetrennt: %v", filmeMitDateien(t, st, lib.ID))
	}
}
