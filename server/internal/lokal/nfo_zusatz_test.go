// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package lokal

import (
	"reflect"
	"testing"
)

// Besetzung, Regie, Studio, Tags, Tagline, Land und Sammlung, wie Emby und
// Kodi sie schreiben.
func TestNFOZusatz(t *testing.T) {
	n, err := NFOAus([]byte(`<movie>
  <title>Matrix</title><originaltitle>The Matrix</originaltitle><sorttitle>Matrix 1</sorttitle>
  <tagline>Willkommen in der Wirklichkeit.</tagline>
  <director>Lana Wachowski</director><director>Lilly Wachowski</director>
  <credits>Lana Wachowski / Lilly Wachowski</credits>
  <studio>Warner Bros.</studio><studio>Village Roadshow</studio>
  <tag>Cyberpunk</tag><tag>Kult</tag>
  <country>USA</country><country>Australien</country>
  <set tmdbcolid="2344">Matrix Collection</set>
  <ratings><rating name="themoviedb" max="10"><value>8.2</value><votes>25000</votes></rating></ratings>
  <actor><name>Laurence Fishburne</name><role>Morpheus</role><order>1</order><thumb>https://image.tmdb.org/t/p/w185/a.jpg</thumb></actor>
  <actor><name>Keanu Reeves</name><role>Neo</role><order>0</order></actor>
  <actor><name>Keanu Reeves</name><role>doppelt</role></actor>
  <actor><name>Joel Silver</name><type>Producer</type><role></role></actor>
  <actor><name>Bill Pope</name><type>Director</type></actor>
</movie>`))
	if err != nil {
		t.Fatal(err)
	}
	if n.Tagline != "Willkommen in der Wirklichkeit." {
		t.Errorf("Tagline %q", n.Tagline)
	}
	if !reflect.DeepEqual(n.Regie, []string{"Lana Wachowski", "Lilly Wachowski", "Bill Pope"}) &&
		!reflect.DeepEqual(n.Regie, []string{"Bill Pope", "Lana Wachowski", "Lilly Wachowski"}) {
		t.Errorf("Regie %v", n.Regie)
	}
	if !reflect.DeepEqual(n.Drehbuch, []string{"Lana Wachowski", "Lilly Wachowski"}) {
		t.Errorf("Drehbuch %v", n.Drehbuch)
	}
	if !reflect.DeepEqual(n.Studios, []string{"Warner Bros.", "Village Roadshow"}) ||
		!reflect.DeepEqual(n.Tags, []string{"Cyberpunk", "Kult"}) ||
		!reflect.DeepEqual(n.Laender, []string{"USA", "Australien"}) {
		t.Errorf("Studio %v Tags %v Länder %v", n.Studios, n.Tags, n.Laender)
	}
	if n.Reihe != "Matrix Collection" || n.ReiheTMDb != "2344" {
		t.Errorf("Reihe %q/%q", n.Reihe, n.ReiheTMDb)
	}
	if n.Bewertung != 8.2 || n.Stimmen != 25000 {
		t.Errorf("Bewertung %v/%d", n.Bewertung, n.Stimmen)
	}
	if len(n.Besetzung) != 2 || n.Besetzung[0].Name != "Keanu Reeves" || n.Besetzung[0].Rolle != "Neo" ||
		n.Besetzung[1].Name != "Laurence Fishburne" || n.Besetzung[1].Bild == "" {
		t.Errorf("Besetzung %+v", n.Besetzung)
	}

	// Kodi-Form der Sammlung und Doppelfolge.
	k, _ := NFOAus([]byte(`<movie><title>Die Gefährten</title><set><name>Der Herr der Ringe Filmreihe</name><overview/></set></movie>`))
	if k.Reihe != "Der Herr der Ringe Filmreihe" || k.ReiheTMDb != "" {
		t.Errorf("Kodi-Set %q/%q", k.Reihe, k.ReiheTMDb)
	}
	f, _ := NFOAus([]byte(`<episodedetails><title>Pilot</title><season>1</season><episode>1</episode><episodenumberend>2</episodenumberend></episodedetails>`))
	if f.BisFolge != 2 {
		t.Errorf("BisFolge %d", f.BisFolge)
	}
}
