// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package scan

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"lumen/internal/lokal"
	"lumen/internal/meta"
	"lumen/internal/store"
)

const embyMatrix = `<movie>
  <title>Matrix</title><originaltitle>The Matrix</originaltitle><year>1999</year>
  <plot>Ein Hacker erfährt die Wahrheit.</plot><mpaa>DE-16</mpaa>
  <tagline>Willkommen in der Wirklichkeit.</tagline><director>Lana Wachowski</director>
  <studio>Warner Bros.</studio><tag>Kult</tag><country>USA</country>
  <set tmdbcolid="2344">Matrix Collection</set>
  <uniqueid type="tmdb">603</uniqueid><uniqueid type="imdb">tt0133093</uniqueid>
  <actor><name>Keanu Reeves</name><role>Neo</role><order>0</order></actor>
  <actor><name>Laurence Fishburne</name><role>Morpheus</role><order>1</order><thumb>https://image.tmdb.org/t/p/w185/x.jpg</thumb></actor>
  <actor><name>Carrie-Anne Moss</name><role>Trinity</role><thumb>../../../etc/passwd.jpg</thumb></actor>
</movie>`

// Ein Emby-Umsteiger ohne TMDb-Schlüssel: Die NFO beschreibt den Film
// vollständig. Er gilt als erkannt, Kennungen, Freigabe, Besetzung samt
// Bild aus „.actors" und die Sammlung aus <set> sind da.
func TestEmbyNFOOhneSchluessel(t *testing.T) {
	ctx := t.Context()
	st := testStore(t)
	wurzel, bilder := t.TempDir(), t.TempDir()
	dateienAnlegen(t, wurzel,
		"The Matrix (1999)/The Matrix (1999).mkv",
		"The Matrix Reloaded (2003)/The Matrix Reloaded (2003).mkv")
	textDatei(t, wurzel, "The Matrix (1999)/movie.nfo", embyMatrix)
	textDatei(t, wurzel, "The Matrix (1999)/.actors/Keanu_Reeves.jpg", "KEANU")
	textDatei(t, wurzel, "The Matrix Reloaded (2003)/movie.nfo", `<movie><title>Matrix Reloaded</title><year>2003</year>
<plot>Zion.</plot><set tmdbcolid="2344">Matrix Filmreihe</set><uniqueid type="tmdb">604</uniqueid></movie>`)

	lib, _ := st.CreateLibrary(ctx, "Filme", "movie", wurzel, "de")
	sc := New(st, nil)
	sc.BilderOrdner = bilder
	if _, err := sc.Scan(ctx, *lib); err != nil {
		t.Fatal(err)
	}
	filme, _ := st.Query(ctx, store.Query{Libraries: []string{lib.ID}, Kinds: []string{"movie"}, Limit: 5})
	if len(filme) != 2 {
		t.Fatalf("Filme: %v", titles(filme))
	}
	var m store.Item
	for _, f := range filme {
		if f.Title == "Matrix" {
			m = f
		}
	}
	if m.MatchState != "lokal" {
		t.Errorf("ohne Schlüssel: %q statt lokal", m.MatchState)
	}
	if m.AgeYears == nil || *m.AgeYears != 16 {
		t.Errorf("DE-16 → %v", m.AgeYears)
	}
	if id, _ := st.SourceID(ctx, m.ID, "tmdb"); id != "603" || m.IMDbID != "tt0133093" {
		t.Errorf("Kennungen: tmdb %q imdb %q", id, m.IMDbID)
	}
	leute, _ := st.Besetzung(ctx, m.ID)
	if len(leute) != 3 || leute[0].Name != "Keanu Reeves" || leute[0].Rolle != "Neo" {
		t.Fatalf("Besetzung %+v", leute)
	}
	if b, err := os.ReadFile(filepath.Join(bilder, leute[0].Bild)); err != nil || string(b) != "KEANU" {
		t.Errorf("Bild aus .actors: %q %v", leute[0].Bild, err)
	}
	// Eine Adresse wird nicht geladen, ein Pfad aus der Bibliothek hinaus
	// nicht kopiert.
	if leute[1].Bild != "" || leute[2].Bild != "" {
		t.Errorf("fremde Bilder übernommen: %+v", leute)
	}
	roh, _ := st.LokaleNFOZu(ctx, m.ID)
	var n lokal.NFO
	_ = json.Unmarshal([]byte(roh.Daten), &n)
	if n.Tagline == "" || len(n.Regie) != 1 || len(n.Studios) != 1 {
		t.Errorf("Zusatz nicht gemerkt: %+v", n)
	}
	// Beide Filme in einer Reihe (gleiche tmdbcolid), mit dem Emby-Namen;
	// die Reihe überlebt das Neubilden.
	if _, err := st.ReihenNeuBilden(ctx); err != nil {
		t.Fatal(err)
	}
	r, err := st.ReiheVon(ctx, m.ID)
	if err != nil || r == nil {
		t.Fatalf("keine Reihe: %v", err)
	}
	if r.Source != "nfo" || r.Name != "Matrix Collection" || len(r.ItemIDs) != 2 {
		t.Errorf("Reihe %+v", r)
	}
	// TMDb nennt später eine andere Reihe — die NFO geht vor.
	_ = st.TMDbReiheZuordnen(ctx, "999", "Andere", m.ID, 1999)
	if r2, _ := st.ReiheVon(ctx, m.ID); r2 == nil || r2.ID != r.ID {
		t.Errorf("TMDb hat die NFO-Reihe verdrängt: %+v", r2)
	}
}

// Mit Schlüssel holt TMDb den genauen Eintrag (pending). Lehnt TMDb den
// Schlüssel ab, gilt die NFO wieder für sich — ohne dass sie sich ändert.
func TestNFOMitKennungOhneSchluessel(t *testing.T) {
	ctx := t.Context()
	st := testStore(t)
	wurzel := t.TempDir()
	dateienAnlegen(t, wurzel, "The Matrix (1999)/The Matrix (1999).mkv")
	textDatei(t, wurzel, "The Matrix (1999)/movie.nfo", embyMatrix)
	_ = st.SetSetting(ctx, meta.EinstellungTMDbSchluessel, "eyJ-test")
	lib, _ := st.CreateLibrary(ctx, "Filme", "movie", wurzel, "de")
	sc := New(st, nil)
	if _, err := sc.Scan(ctx, *lib); err != nil {
		t.Fatal(err)
	}
	filme, _ := st.Query(ctx, store.Query{Libraries: []string{lib.ID}, Kinds: []string{"movie"}, Limit: 5})
	if len(filme) != 1 || filme[0].MatchState != "pending" {
		t.Fatalf("mit Schlüssel: %+v", filme)
	}
	_ = st.SetSetting(ctx, meta.EinstellungTMDbAbgelehnt, "1")
	if _, err := sc.Scan(ctx, *lib); err != nil {
		t.Fatal(err)
	}
	if it, _ := st.Item(ctx, filme[0].ID); it.MatchState != "lokal" {
		t.Errorf("Schlüssel abgelehnt: %q statt lokal", it.MatchState)
	}
	// Ein gültiger Schlüssel holt sie nach.
	if n, err := st.LokaleMitKennungNachholen(ctx); err != nil || n != 1 {
		t.Errorf("nachholen: %d %v", n, err)
	}
}

// Emby-Folgenbilder in „metadata/" und eine Doppelfolge laut NFO, die
// auch den zweiten Durchlauf übersteht.
func TestEmbyFolgenbildUndDoppelfolge(t *testing.T) {
	ctx := t.Context()
	st := testStore(t)
	wurzel, bilder := t.TempDir(), t.TempDir()
	dateienAnlegen(t, wurzel, "Dark/Season 01/Dark - S01E01.mkv", "Dark/Season 01/Dark - S01E03.mkv")
	textDatei(t, wurzel, "Dark/Season 01/metadata/Dark - S01E01.jpg", "THUMB")
	textDatei(t, wurzel, "Dark/Season 01/Dark - S01E01.nfo", `<episodedetails><title>Geheimnisse</title>
<season>1</season><episode>1</episode><episodenumberend>2</episodenumberend><plot>Winden.</plot></episodedetails>`)
	lib, _ := st.CreateLibrary(ctx, "Serien", "series", wurzel, "de")
	sc := New(st, nil)
	sc.BilderOrdner = bilder
	for i := 0; i < 2; i++ {
		if _, err := sc.Scan(ctx, *lib); err != nil {
			t.Fatal(err)
		}
	}
	serien, _ := st.Query(ctx, store.Query{Libraries: []string{lib.ID}, Kinds: []string{"series"}, Limit: 5})
	if len(serien) != 1 {
		t.Fatalf("Serien %v", titles(serien))
	}
	staffeln, _ := st.Children(ctx, serien[0].ID)
	folgen, _ := st.Children(ctx, staffeln[0].ID)
	gefunden := false
	for _, f := range folgen {
		if f.EpisodeNo != 1 {
			continue
		}
		gefunden = true
		if b, _ := os.ReadFile(filepath.Join(bilder, art2(t, st, f.ID, "poster"))); string(b) != "THUMB" {
			t.Errorf("Bild aus metadata/: %q", b)
		}
		if z := st.ZusatzZu(ctx, f.ID); z.BisFolge != 2 {
			t.Errorf("Doppelfolge bis %d", z.BisFolge)
		}
		if bis := st.BisFolgen(ctx, []string{f.ID}); bis[f.ID] != 2 {
			t.Errorf("BisFolgen %v", bis)
		}
	}
	if !gefunden {
		t.Fatal("Folge 1 fehlt")
	}
}
