package scan

import (
	"context"
	"path/filepath"
	"testing"

	"lumen/internal/store"
)

// fassungenAnEinemFilm legt die Dateien an, scannt, hängt sie (wie eine
// ältere Fassung oder eine NFO) an einen erkannten Eintrag und gibt jeder
// Datei die Laufzeit aus dauer (0 = unbekannt). vorher läuft vor den
// weiteren Durchläufen (etwa: von Hand bestätigen).
func fassungenAnEinemFilm(t *testing.T, titel, orig string, dauer []int64, vorher func(st *store.Store, id string),
	pfade ...string) (st *store.Store, lib *store.Library, root, id string) {
	t.Helper()
	root = t.TempDir()
	faelleAnlegen(t, root, pfade...)
	st = faelleStore(t)
	ctx := context.Background()
	lib, _ = st.CreateLibrary(ctx, "Filme", "movie", root, "de")
	sc := New(st, nil)
	if _, err := sc.Scan(ctx, *lib); err != nil {
		t.Fatal(err)
	}
	for i, p := range pfade {
		g := st.GemessenZuPfad(ctx, filepath.Join(root, p))
		if g == nil {
			t.Fatalf("%s unbekannt", p)
		}
		f := g.Datei
		if i == 0 {
			id = f.ItemID
		}
		f.ItemID = id
		f.DurationMS = dauer[i]
		if err := st.UpsertFile(ctx, &f); err != nil {
			t.Fatal(err)
		}
	}
	it, _ := st.Item(ctx, id)
	it.Title, it.OriginalTitle, it.MatchState = titel, orig, "matched"
	_ = st.UpsertItem(ctx, it)
	_ = st.SetSourceID(ctx, it.ID, "tmdb", "1")
	_ = filmeOhneDateiWeg(ctx, st)
	if vorher != nil {
		vorher(st, id)
	}
	for i := 0; i < 2; i++ {
		if _, err := sc.Scan(ctx, *lib); err != nil {
			t.Fatal(err)
		}
	}
	return st, lib, root, id
}

func bleibenZusammen(t *testing.T, titel, orig string, dauer []int64, vorher func(*store.Store, string), pfade ...string) {
	t.Helper()
	st, lib, root, id := fassungenAnEinemFilm(t, titel, orig, dauer, vorher, pfade...)
	ctx := context.Background()
	for _, p := range pfade {
		if g := st.GemessenZuPfad(ctx, filepath.Join(root, p)); g == nil || g.Datei.ItemID != id {
			t.Errorf("%s: abgetrennt: %v", titel, filmeMitDateien(t, st, lib.ID))
			return
		}
	}
	// Das Aufräumen zeigt sie nicht als „verschieden".
	it, _ := st.Item(ctx, id)
	dateien, _ := st.FilesFor(ctx, id)
	if New(st, nil).DateienVerschieden(ctx, *lib, it, dateien) {
		t.Errorf("%s: Aufräumen zeigt „verschieden“, der Scanner lässt sie zusammen", titel)
	}
}

func werdenGetrennt(t *testing.T, titel, orig string, dauer []int64, pfade ...string) {
	t.Helper()
	st, lib, root, _ := fassungenAnEinemFilm(t, titel, orig, dauer, nil, pfade...)
	ctx := context.Background()
	a := st.GemessenZuPfad(ctx, filepath.Join(root, pfade[0]))
	b := st.GemessenZuPfad(ctx, filepath.Join(root, pfade[1]))
	if a.Datei.ItemID == b.Datei.ItemID {
		t.Errorf("%s: nicht getrennt: %v", titel, filmeMitDateien(t, st, lib.ID))
	}
}

const zweiStunden = 7_200_000

// Sprachfassungen mit gleicher Laufzeit (±2 %) bleiben ein Film.
func TestSprachfassungenGleicherLaufzeitBleibenZusammen(t *testing.T) {
	bleibenZusammen(t, "Chihiros Reise ins Zauberland", "千と千尋の神隠し",
		[]int64{zweiStunden, zweiStunden + 60_000}, nil,
		"Chihiro (2001)/Chihiros Reise ins Zauberland (2001).mkv",
		"Chihiro (2001)/Spirited Away (2001).mkv")
	bleibenZusammen(t, "Das Leben der Anderen", "Das Leben der Anderen",
		[]int64{zweiStunden, zweiStunden}, nil,
		"Das Leben der Anderen (2006)/Das Leben der Anderen (2006).mkv",
		"Das Leben der Anderen (2006)/The Lives of Others (2006).mkv")
	// „Dune" neben „Dune - Part One": eine Seite ohne Zahl ist keine
	// verschiedene Zahl — bei gleicher Laufzeit derselbe Film.
	bleibenZusammen(t, "Dune: Part One", "Dune: Part One",
		[]int64{zweiStunden, zweiStunden}, nil,
		"Dune (2021)/Dune - Part One (2021).mkv",
		"Dune (2021)/Dune (2021).mkv")
}

// Szene-Namen mit Ausgabe („Extended Collectors Edition") sind eine
// Fassung — auch ohne bekannte Laufzeit.
func TestSzeneNameMitAusgabeBleibtFassung(t *testing.T) {
	bleibenZusammen(t, "Avatar - Aufbruch nach Pandora", "Avatar", []int64{0, 0}, nil,
		"Avatar (2009)/Avatar (2009).mkv",
		"Avatar (2009)/Avatar.Extended.Collectors.Edition.2009.1080p.mkv")
}

// Von Hand bestätigt („Diese Fassung", Kennung eingegeben): Der Scanner
// trennt nicht wieder auf, auch ohne bekannte Laufzeit.
func TestVonHandBestaetigteZuordnungBleibt(t *testing.T) {
	ctx := context.Background()
	bleibenZusammen(t, "Chihiros Reise ins Zauberland", "千と千尋の神隠し", []int64{0, 0},
		func(st *store.Store, id string) {
			dateien, _ := st.FilesFor(ctx, id)
			if err := st.FassungWaehlen(ctx, id, dateien[0].ID); err != nil {
				t.Fatal(err)
			}
		},
		"Chihiro (2001)/Chihiros Reise ins Zauberland (2001).mkv",
		"Chihiro (2001)/Spirited Away (2001).mkv")
	bleibenZusammen(t, "Das Leben der Anderen", "Das Leben der Anderen", []int64{0, 0},
		func(st *store.Store, id string) {
			it, _ := st.Item(ctx, id)
			it.MatchState = "manual"
			_ = st.UpsertItem(ctx, it)
		},
		"Das Leben der Anderen (2006)/Das Leben der Anderen (2006).mkv",
		"Das Leben der Anderen (2006)/The Lives of Others (2006).mkv")
}

// Verschiedene Zahlen bleiben verschiedene Filme — auch bei gleicher
// Laufzeit (Zweiteiler sind oft gleich lang).
func TestVerschiedeneZahlenTrotzGleicherLaufzeitGetrennt(t *testing.T) {
	werdenGetrennt(t, "Die Pfeiler der Macht", "", []int64{zweiStunden, zweiStunden},
		"_Die Pfeiler der Macht/Die Pfeiler der Macht - Teil 1 (2016).mkv",
		"_Die Pfeiler der Macht/Die Pfeiler der Macht - Teil 2 (2016).mkv")
	werdenGetrennt(t, "Dune: Part One", "", []int64{zweiStunden, zweiStunden},
		"_Dune/Dune - Part One (2021).mkv",
		"_Dune/Dune - Part Two (2021).mkv")
	// Ohne passende Laufzeit trennt eine Zahl auf nur einer Seite.
	werdenGetrennt(t, "Dune", "", []int64{zweiStunden, 9_900_000},
		"_Dune/Dune (2021).mkv",
		"_Dune/Dune 2 (2021).mkv")
	// Verschiedene Filme mit verschiedener Laufzeit.
	werdenGetrennt(t, "Batman v Superman: Dawn of Justice", "", []int64{9_060_000, 4_320_000},
		"_Batman/(2016) Batman vs. Superman - Dawn of Justice.mkv",
		"_Batman/(2016) Batman Anime - Bad Blood.mkv")
}

func TestZahlenIn(t *testing.T) {
	for in, soll := range map[string]string{
		"Teil 2 (2016)":        "2 2016",
		"Dune: Part One":       "1",
		"Dune - Part Two":      "2",
		"Dune":                 "",
		"Star Trek II":         "2",
		"Rocky IV":             "4",
		"I, Robot":             "",
		"V wie Vendetta":       "",
		"X-Men":                "",
		"Kill Bill Vol. 2":     "2",
		"Der Hobbit Teil drei": "3",
		"Film part2":           "2",
	} {
		if ist := zahlenIn(in); ist != soll {
			t.Errorf("zahlenIn(%q) = %q, erwartet %q", in, ist, soll)
		}
	}
}

// ordnerZusammenlegen: gleiche Kennung und gleiche Laufzeit genügen,
// auch wenn die Dateien verschieden heissen.
func TestOrdnerZusammenlegenBeiGleicherLaufzeit(t *testing.T) {
	root := t.TempDir()
	faelleAnlegen(t, root,
		"Chihiro (2001)/Chihiros Reise ins Zauberland (2001).mkv",
		"Chihiro (2001)/Spirited Away (2001).mkv",
	)
	st := faelleStore(t)
	ctx := context.Background()
	lib, _ := st.CreateLibrary(ctx, "Filme", "movie", root, "de")
	sc := New(st, nil)
	if _, err := sc.Scan(ctx, *lib); err != nil {
		t.Fatal(err)
	}
	filme, _ := st.Query(ctx, store.Query{Libraries: []string{lib.ID}, Kinds: []string{"movie"}, Limit: 10})
	if len(filme) != 2 {
		t.Fatalf("%d Filme nach dem Durchlauf, erwartet 2", len(filme))
	}
	for _, f := range filme {
		_ = st.SetSourceID(ctx, f.ID, "tmdb", "129")
		dateien, _ := st.FilesFor(ctx, f.ID)
		for _, d := range dateien {
			d.DurationMS = zweiStunden
			_ = st.UpsertFile(ctx, &d)
		}
	}
	sc.ordnerZusammenlegen(ctx, *lib, filepath.Join(root, "Chihiro (2001)"))
	if n := len(filmeMitDateien(t, st, lib.ID)); n != 1 {
		t.Fatalf("%d Filme nach dem Zusammenlegen, erwartet 1", n)
	}
}
