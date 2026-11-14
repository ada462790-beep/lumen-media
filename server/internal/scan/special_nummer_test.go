package scan

import (
	"context"
	"path/filepath"
	"testing"

	"lumen/internal/store"
)

// „S08E00 - The Last Watch" ist ein Special ohne eigene Nummer. Bekam es
// die nächste freie Nummer der Staffel 0 (hier 2), wurde eine später
// dazukommende echte Datei S00E02 eine Fassung davon.
func TestSpecialOhneNummerKollidiertNicht(t *testing.T) {
	root := t.TempDir()
	lastWatch := "Game of Thrones/Staffel 08/Game of Thrones - S08E00 - The Last Watch.mkv"
	faelleAnlegen(t, root,
		"Game of Thrones/Specials/Game of Thrones - S00E01 - Inside.mkv",
		lastWatch,
		"Game of Thrones/Staffel 08/Game of Thrones - S08E01 - Winterfell.mkv",
	)
	st := faelleStore(t)
	ctx := context.Background()
	lib, _ := st.CreateLibrary(ctx, "Serien", "series", root, "de")
	sc := New(st, nil)
	if _, err := sc.Scan(ctx, *lib); err != nil {
		t.Fatal(err)
	}
	lw := st.GemessenZuPfad(ctx, filepath.Join(root, lastWatch))
	it, _ := st.Item(ctx, lw.Datei.ItemID)
	if it.SeasonNo != 0 || it.EpisodeNo < store.SpecialOhneNummer {
		t.Errorf("The Last Watch: S%02dE%02d, erwartet Staffel 0 ab %d", it.SeasonNo, it.EpisodeNo, store.SpecialOhneNummer)
	}
	faelleAnlegen(t, root, "Game of Thrones/Specials/Game of Thrones - S00E02 - Histories.mkv")
	if _, err := sc.Scan(ctx, *lib); err != nil {
		t.Fatal(err)
	}
	neu := st.GemessenZuPfad(ctx, filepath.Join(root, "Game of Thrones/Specials/Game of Thrones - S00E02 - Histories.mkv"))
	lw = st.GemessenZuPfad(ctx, filepath.Join(root, lastWatch))
	if neu.Datei.ItemID == lw.Datei.ItemID {
		t.Errorf("S00E02 hängt als Fassung an „The Last Watch“")
	}
	if a, _ := st.Item(ctx, neu.Datei.ItemID); a == nil || a.EpisodeNo != 2 {
		t.Errorf("Histories nicht als S00E02: %+v", a)
	}
}

// Ein Special, das eine ältere Fassung schon im alten Bereich nummeriert
// hat (S00E02), zieht beim nächsten Durchlauf in den eigenen Bereich um —
// als dieselbe Folge, mit Haken. Die echte S00E02 bekommt eine eigene.
func TestSpecialAusAltemBereichUmnummeriert(t *testing.T) {
	root := t.TempDir()
	lastWatch := "Game of Thrones/Staffel 08/Game of Thrones - S08E00 - The Last Watch.mkv"
	faelleAnlegen(t, root,
		"Game of Thrones/Specials/Game of Thrones - S00E01 - Inside.mkv",
		lastWatch,
	)
	st := faelleStore(t)
	ctx := context.Background()
	lib, _ := st.CreateLibrary(ctx, "Serien", "series", root, "de")
	sc := New(st, nil)
	if _, err := sc.Scan(ctx, *lib); err != nil {
		t.Fatal(err)
	}
	lw := st.GemessenZuPfad(ctx, filepath.Join(root, lastWatch))
	alt := lw.Datei.ItemID
	// Stand einer älteren Fassung: das Special als S00E02.
	if err := st.FolgeUmnummern(ctx, alt, 2); err != nil {
		t.Fatal(err)
	}
	acc, _ := st.CreateAccount(ctx, store.NewAccount{Name: "Mia"})
	_ = st.SaveProgress(ctx, acc.ID, alt, 3_000_000, 3_000_000)

	faelleAnlegen(t, root, "Game of Thrones/Specials/Game of Thrones - S00E02 - Histories.mkv")
	if _, err := sc.Scan(ctx, *lib); err != nil {
		t.Fatal(err)
	}
	lw = st.GemessenZuPfad(ctx, filepath.Join(root, lastWatch))
	neu := st.GemessenZuPfad(ctx, filepath.Join(root, "Game of Thrones/Specials/Game of Thrones - S00E02 - Histories.mkv"))
	if lw.Datei.ItemID != alt {
		t.Errorf("The Last Watch hat die Folge gewechselt: %s statt %s", lw.Datei.ItemID, alt)
	}
	it, _ := st.Item(ctx, alt)
	if it == nil || it.EpisodeNo < store.SpecialOhneNummer {
		t.Errorf("The Last Watch nicht umnummeriert: %+v", it)
	}
	if neu.Datei.ItemID == alt {
		t.Errorf("S00E02 hängt an „The Last Watch“")
	}
	if p, _ := st.Progress(ctx, acc.ID, alt); p == nil || !p.Finished {
		t.Errorf("Haken am Special verloren")
	}
	// Ein weiterer Durchlauf lässt die Nummer stehen.
	vorher := it.EpisodeNo
	if _, err := sc.Scan(ctx, *lib); err != nil {
		t.Fatal(err)
	}
	if it, _ = st.Item(ctx, alt); it == nil || it.EpisodeNo != vorher {
		t.Errorf("Nummer gewandert: %+v (vorher %d)", it, vorher)
	}
}
