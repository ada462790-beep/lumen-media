// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package uebernahme

import (
	"context"
	"errors"
	"testing"

	"lumen/internal/uebernahme/nachbau"
)

func gesehen() map[string]any {
	return map[string]any{"Played": true, "LastPlayedDate": "2025-03-01T20:00:00.0000000Z"}
}

func beispielServer(art string) *nachbau.Server {
	n := nachbau.Neu(art)
	n.Titel = []nachbau.Titel{
		// Über die TMDb-Kennung.
		nachbau.Film("e1", "Matrix", 1999, "/mnt/filme/Matrix.mkv", 1000, map[string]string{"Tmdb": "603"}, gesehen()),
		// Über Dateiname und Grösse (anderer Pfad als in Lumen).
		nachbau.Film("e2", "Heimvideo", 0, `D:\Videos\Sommer 2019.mp4`, 4242, nil,
			map[string]any{"PlaybackPositionTicks": 12_000_000_000, "LastPlayedDate": "2025-04-01T20:00:00Z"}),
		// Über Titel und Jahr, mit Bewertung 8/10 und Herz.
		nachbau.Film("e3", "Amélie", 2001, "/x/amelie.avi", 1, nil,
			map[string]any{"IsFavorite": true, "Rating": 8.0}),
		// Gibt es in Lumen nicht.
		nachbau.Film("e4", "Unbekannt", 2010, "/x/unbekannt.mkv", 9, nil, gesehen()),
		// Folge über Serie (TVDb) + Nummern.
		nachbau.Folge("f1", "s1", "Nordwand", 1, 2, "/serien/nw/s01e02.mkv", gesehen()),
		// Serie mit Herz.
		nachbau.Serie("s1", "Nordwand", 2020, map[string]string{"Tvdb": "777"}, map[string]any{"IsFavorite": true}),
	}
	n.Serien = []nachbau.Titel{nachbau.Serie("s1", "Nordwand", 2020, map[string]string{"Tvdb": "777"}, map[string]any{"IsFavorite": true})}
	n.SeitenGross = 2 // damit das Blättern geprüft wird
	return n
}

func beispielIndex() *Index {
	return NeuerIndex([]LumenTitel{
		{ID: "matrix", Kind: "movie", Titel: "Matrix", Jahr: 1999, TMDb: "603"},
		{ID: "heim", Kind: "movie", Titel: "Sommer 2019", LaengeMS: 3_600_000},
		{ID: "amelie", Kind: "movie", Titel: "Die fabelhafte Welt der Amélie", Original: "Amélie", Jahr: 2001},
		{ID: "nw", Kind: "series", Titel: "Nordwand", Jahr: 2020, TVDb: "777"},
		{ID: "nw-1-2", Kind: "episode", SerieID: "nw", Staffel: 1, Folge: 2},
	}, []LumenDatei{
		{ItemID: "heim", Pfad: "/volume1/video/Sommer 2019.mp4", Groesse: 4242},
		{ItemID: "matrix", Pfad: "/volume1/filme/Matrix.mkv", Groesse: 1000},
	})
}

func durchlauf(t *testing.T, art string) {
	n := beispielServer(art)
	defer n.Close()
	ctx := context.Background()

	s, err := Verbinden(n.Adresse(), nil)
	if err != nil {
		t.Fatal(err)
	}
	info, err := s.Info(ctx)
	if err != nil || info.Art != art {
		t.Fatalf("Info: %+v %v", info, err)
	}
	if b, err := s.Benutzer(ctx); err != nil || len(b) != 1 || b[0] != "alex" {
		t.Fatalf("Benutzer: %v %v", b, err)
	}
	if err := s.Anmelden(ctx, "alex", "falsch"); !errors.Is(err, ErrAnmeldung) {
		t.Fatalf("falsches Passwort: %v", err)
	}
	if err := s.Anmelden(ctx, "alex", "geheim"); err != nil {
		t.Fatalf("Anmelden: %v", err)
	}
	titel, serien, err := s.Stand(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	s.Abmelden(ctx)
	if !n.Abgemeldet {
		t.Fatal("das Token wurde bei Emby nicht abgemeldet")
	}
	if s.token != "" {
		t.Fatal("das Token blieb im Speicher")
	}

	p := Planen(titel, serien, beispielIndex())
	if p.Gesehen != 2 || p.Angefangen != 1 || p.Favoriten != 2 || p.Bewertungen != 1 || p.NichtGefunden != 1 {
		t.Fatalf("Vorschau: %+v", p)
	}
	if p.Wie["kennung"] < 2 || p.Wie["datei"] < 1 || p.Wie["name"] < 1 || p.Wie["folge"] < 1 {
		t.Fatalf("Zuordnungswege: %v", p.Wie)
	}
	if len(p.Fehlend) != 1 || p.Fehlend[0] != "Unbekannt (2010)" {
		t.Fatalf("Fehlend: %v", p.Fehlend)
	}
	je := map[string]Uebernahme{}
	for _, u := range p.Eintraege {
		je[u.ItemID] = u
	}
	if u := je["heim"]; u.PositionMS != 1_200_000 || u.Gesehen {
		t.Fatalf("Heimvideo: %+v", u)
	}
	if u := je["amelie"]; u.Sterne != 4 || !u.Favorit {
		t.Fatalf("Amélie: %+v", u)
	}
	if u := je["nw"]; !u.Favorit || u.Gesehen {
		t.Fatalf("Serie: %+v", u)
	}
	if u := je["nw-1-2"]; !u.Gesehen {
		t.Fatalf("Folge: %+v", u)
	}
	// Das zuletzt Gesehene steht hinten.
	if last := p.Eintraege[len(p.Eintraege)-1].ItemID; last != "heim" {
		t.Fatalf("Reihenfolge: zuletzt %s", last)
	}
}

func TestUebernahmeAusEmby(t *testing.T)     { durchlauf(t, "Emby") }
func TestUebernahmeAusJellyfin(t *testing.T) { durchlauf(t, "Jellyfin") }

// Ohne Anmeldung gibt es keinen Stand.
func TestStandVerlangtAnmeldung(t *testing.T) {
	n := beispielServer("Emby")
	defer n.Close()
	s, _ := Verbinden(n.Adresse(), nil)
	if _, _, err := s.Stand(context.Background(), nil); !errors.Is(err, ErrAnmeldung) {
		t.Fatalf("Stand ohne Anmeldung: %v", err)
	}
}

// Unter einer falschen Adresse antwortet kein Emby.
func TestKeinServer(t *testing.T) {
	n := beispielServer("Jellyfin")
	defer n.Close()
	s, _ := Verbinden(n.URL+"/falsch", nil)
	if _, err := s.Info(context.Background()); !errors.Is(err, ErrKeinServer) {
		t.Fatalf("falsche Adresse: %v", err)
	}
}

// Zwei gleichnamige Filme ohne Jahr sind zu unsicher.
func TestNameMehrdeutig(t *testing.T) {
	ix := NeuerIndex([]LumenTitel{
		{ID: "a", Kind: "movie", Titel: "Solaris", Jahr: 1972},
		{ID: "b", Kind: "movie", Titel: "Solaris", Jahr: 2002},
	}, nil)
	if id, _ := ix.Zuordnen(Eintrag{Typ: "Movie", Name: "Solaris"}, nil); id != "" {
		t.Fatalf("mehrdeutig zugeordnet: %s", id)
	}
	if id, _ := ix.Zuordnen(Eintrag{Typ: "Movie", Name: "Solaris", Jahr: 2002}, nil); id != "b" {
		t.Fatalf("mit Jahr: %s", id)
	}
}
