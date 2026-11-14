package scan

import (
	"context"
	"path/filepath"
	"testing"

	"lumen/internal/store"
)

// Staffel 0 (Specials) steht als season_no NULL in der Datenbank. Beim
// Zusammenführen zweier Serien mit derselben TMDb-Kennung verglich
// serieUeberfuehren mit „NOT IN"/„=" — beides scheitert an NULL: Die
// Specials blieben bei der alten Serie und verschwanden mit ihr, eine
// echte Staffel zog nicht mehr um, sobald die Zielserie Specials hatte,
// und die Haken daran gingen verloren.
func TestSpecialsBeimSerienZusammenfuehren(t *testing.T) {
	root := t.TempDir()
	faelleAnlegen(t, root,
		// alt (weniger Dateien): Staffel 1 und zwei Specials
		"Sherlock (2010)/Staffel 1/Sherlock S01E01.mkv",
		"Sherlock (2010)/Specials/Sherlock S00E01 - Unaired Pilot.mkv",
		"Sherlock (2010)/Specials/Sherlock S00E02 - Many Happy Returns.mkv",
		// neu (mehr Dateien): Staffel 3 und Specials, eines davon doppelt
		"Sherlock (2014)/Staffel 3/Sherlock S03E01.mkv",
		"Sherlock (2014)/Staffel 3/Sherlock S03E02.mkv",
		"Sherlock (2014)/Specials/Sherlock S00E02 - Many Happy Returns (Kopie).mkv",
		"Sherlock (2014)/Specials/Sherlock S00E03 - The Abominable Bride.mkv",
		"Sherlock (2014)/Specials/Sherlock S00E04 - Making Of.mkv",
	)
	st := faelleStore(t)
	ctx := context.Background()
	lib, _ := st.CreateLibrary(ctx, "Serien", "series", root, "de")
	sc := New(st, nil)
	if _, err := sc.Scan(ctx, *lib); err != nil {
		t.Fatal(err)
	}
	serien, _ := st.Query(ctx, store.Query{Libraries: []string{lib.ID}, Kinds: []string{"series"}, Limit: 50})
	if len(serien) != 2 {
		t.Fatalf("%d Serien statt 2", len(serien))
	}
	for _, s := range serien {
		s.Title, s.Year, s.MatchState = "Sherlock", 2010, "matched"
		_ = st.UpsertItem(ctx, &s)
		_ = st.SetSourceID(ctx, s.ID, "tmdb", "19885")
	}
	acc, _ := st.CreateAccount(ctx, store.NewAccount{Name: "Mia"})
	item := func(rel string) string {
		t.Helper()
		g := st.GemessenZuPfad(ctx, filepath.Join(root, rel))
		if g == nil {
			t.Fatalf("Datei unbekannt: %s", rel)
		}
		return g.Datei.ItemID
	}
	pilot := item("Sherlock (2010)/Specials/Sherlock S00E01 - Unaired Pilot.mkv")
	returnsAlt := item("Sherlock (2010)/Specials/Sherlock S00E02 - Many Happy Returns.mkv")
	s1 := item("Sherlock (2010)/Staffel 1/Sherlock S01E01.mkv")
	for _, id := range []string{pilot, returnsAlt, s1} {
		if err := st.SaveProgress(ctx, acc.ID, id, 3_000_000, 3_000_000); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := st.SerienZusammenfuehren(ctx); err != nil {
		t.Fatal(err)
	}
	serien, _ = st.Query(ctx, store.Query{Libraries: []string{lib.ID}, Kinds: []string{"series"}, Limit: 50})
	if len(serien) != 1 {
		t.Fatalf("%d Serien nach dem Zusammenführen", len(serien))
	}
	ziel := serien[0].ID
	t.Logf("Folgen: %v", folgenMitStaffel(t, st, ziel))

	// Jede Datei hängt an einer Folge mit Staffel unter der Zielserie.
	for _, rel := range []string{
		"Sherlock (2010)/Staffel 1/Sherlock S01E01.mkv",
		"Sherlock (2010)/Specials/Sherlock S00E01 - Unaired Pilot.mkv",
		"Sherlock (2010)/Specials/Sherlock S00E02 - Many Happy Returns.mkv",
		"Sherlock (2014)/Specials/Sherlock S00E03 - The Abominable Bride.mkv",
	} {
		ep, err := st.Item(ctx, item(rel))
		if err != nil || ep.ParentID == "" {
			t.Fatalf("%s: verwaist (%v)", rel, err)
		}
		staffel, _ := st.Item(ctx, ep.ParentID)
		if staffel == nil || staffel.ParentID != ziel {
			t.Errorf("%s: Staffel hängt nicht an der Zielserie", rel)
		}
	}
	// Das doppelte Special ist eine zweite Fassung derselben Folge.
	if a, b := item("Sherlock (2010)/Specials/Sherlock S00E02 - Many Happy Returns.mkv"),
		item("Sherlock (2014)/Specials/Sherlock S00E02 - Many Happy Returns (Kopie).mkv"); a != b {
		t.Errorf("S00E02 doppelt: %s / %s", a, b)
	}
	// Nur eine Staffel 0 unter der Zielserie.
	staffeln, _ := st.Children(ctx, ziel)
	nullen := 0
	for _, s := range staffeln {
		if s.SeasonNo == 0 {
			nullen++
		}
	}
	if nullen != 1 {
		t.Errorf("%d Staffeln 0 unter der Zielserie", nullen)
	}
	// Die Haken sind noch da — an der Folge, die jetzt die Datei trägt.
	for _, rel := range []string{
		"Sherlock (2010)/Staffel 1/Sherlock S01E01.mkv",
		"Sherlock (2010)/Specials/Sherlock S00E01 - Unaired Pilot.mkv",
		"Sherlock (2010)/Specials/Sherlock S00E02 - Many Happy Returns.mkv",
	} {
		p, _ := st.Progress(ctx, acc.ID, item(rel))
		if p == nil || !p.Finished {
			t.Errorf("%s: Haken verloren", rel)
		}
	}
	// Ein weiterer Durchlauf ändert nichts mehr daran.
	for i := 0; i < 2; i++ {
		if _, err := sc.Scan(ctx, *lib); err != nil {
			t.Fatal(err)
		}
		_, _ = st.FolgenOhneDatei(ctx)
		_, _ = st.SerienZusammenfuehren(ctx)
	}
	for _, rel := range []string{
		"Sherlock (2010)/Specials/Sherlock S00E01 - Unaired Pilot.mkv",
		"Sherlock (2010)/Specials/Sherlock S00E02 - Many Happy Returns.mkv",
	} {
		p, _ := st.Progress(ctx, acc.ID, item(rel))
		if p == nil || !p.Finished {
			t.Errorf("%s: Haken nach erneutem Durchlauf verloren", rel)
		}
	}
}
