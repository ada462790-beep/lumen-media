package store

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
)

// Nimmt die Person, die einen Wunsch angelegt hat, ihn zurück, behalten
// ihn die Angeschlossenen: Er geht an die Person, die sich zuerst
// angeschlossen hat. Vorher löschte ON DELETE CASCADE ihn für alle.
func TestWunschZuruecknehmenGibtAnAngeschlosseneWeiter(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "l.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	a, _ := st.CreateAccount(ctx, NewAccount{Name: "Anna"})
	b, _ := st.CreateAccount(ctx, NewAccount{Name: "Ben"})
	c, _ := st.CreateAccount(ctx, NewAccount{Name: "Cleo"})
	wa, err := st.WunschAnlegen(ctx, a.ID, WunschDaten{Titel: "Dune", Jahr: 2021})
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{b.ID, c.ID} {
		w, err := st.WunschAnlegen(ctx, k, WunschDaten{Titel: "Dune!", Jahr: 2021})
		if err != nil || w.ID != wa.ID {
			t.Fatalf("nicht angeschlossen: %v %v", w, err)
		}
	}
	geloescht, err := st.WunschAbgeben(ctx, wa.ID, []string{a.ID})
	if err != nil || geloescht {
		t.Fatalf("abgeben: geloescht=%v err=%v", geloescht, err)
	}
	owner, dabei, err := st.WunschTeilnehmer(ctx, wa.ID)
	if err != nil {
		t.Fatalf("Wunsch weg: %v", err)
	}
	if owner != b.ID || len(dabei) != 1 || dabei[0] != c.ID {
		t.Errorf("owner=%s dabei=%v, erwartet Ben mit Cleo", owner, dabei)
	}
	// Annas Liste ist leer, Bens nicht.
	if l, _ := st.Wishes(ctx, a.ID, false); len(l) != 0 {
		t.Errorf("Anna sieht noch %d Wünsche", len(l))
	}
	if l, _ := st.Wishes(ctx, b.ID, false); len(l) != 1 {
		t.Errorf("Ben sieht %d Wünsche", len(l))
	}
	// Erst wenn niemand übrig ist, verschwindet er.
	if g, err := st.WunschAbgeben(ctx, wa.ID, []string{b.ID, c.ID}); err != nil || !g {
		t.Errorf("zuletzt nicht gelöscht: %v %v", g, err)
	}
	if _, _, err := st.WunschTeilnehmer(ctx, wa.ID); err != ErrNotFound {
		t.Errorf("Wunsch noch da: %v", err)
	}
}

// Ohne Angeschlossene wird der Wunsch wirklich gelöscht.
func TestWunschOhneAngeschlosseneWirdGeloescht(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "l.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	a, _ := st.CreateAccount(ctx, NewAccount{Name: "Anna"})
	w, _ := st.WunschAnlegen(ctx, a.ID, WunschDaten{Titel: "Alien", Jahr: 1979})
	if g, err := st.WunschAbgeben(ctx, w.ID, []string{a.ID}); err != nil || !g {
		t.Fatalf("nicht gelöscht: %v %v", g, err)
	}
}

// Wünschen viele Profile denselben Titel im selben Augenblick, entsteht
// ein Wunsch, dem sich die anderen anschliessen — kein Doppel (Suchen
// und Anlegen in einer Transaktion).
func TestWunschAnlegenGleichzeitigKeinDoppel(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "l.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	var konten []string
	for i := 0; i < 8; i++ {
		a, err := st.CreateAccount(ctx, NewAccount{Name: fmt.Sprintf("Profil %d", i)})
		if err != nil {
			t.Fatal(err)
		}
		konten = append(konten, a.ID)
	}
	var wg sync.WaitGroup
	for _, k := range konten {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := st.WunschAnlegen(ctx, k, WunschDaten{Titel: "Arrival", Jahr: 2016}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	var n, dabei int
	_ = st.db.QueryRow(`SELECT count(*) FROM wish`).Scan(&n)
	_ = st.db.QueryRow(`SELECT count(*) FROM wish_dabei`).Scan(&dabei)
	if n != 1 || dabei != len(konten)-1 {
		t.Fatalf("%d Wünsche, %d angeschlossen — erwartet 1 und %d", n, dabei, len(konten)-1)
	}
}
