// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"
)

// Die Übernahme verschlechtert nichts: gesehen bleibt gesehen, eine
// weitere Stelle bleibt, eine Bewertung bleibt — und sie gilt nur für
// das eine Profil.
func TestUebernahmeVerschlechtertNichts(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	ich, _ := s.CreateAccount(ctx, NewAccount{Name: "Ich", IsOwner: true})
	andere, _ := s.CreateAccount(ctx, NewAccount{Name: "Andere", LockKind: "pin", Secret: "1234"})
	mustExec(t, s, `INSERT INTO library (id,name,kind,path) VALUES ('lib','Filme','movie','/x')`)
	for _, id := range []string{"a", "b", "c", "d", "e"} {
		mustExec(t, s, `INSERT INTO item (id,library_id,kind,title,sort_title,added_at,updated_at)
		                VALUES ('`+id+`','lib','movie','`+id+`','`+id+`',0,0)`)
	}
	// Vorhanden: a gesehen, b bei 50 Minuten, c mit 2 Sternen.
	_ = s.MarkFinished(ctx, ich.ID, "a", 6_000_000)
	_ = s.SaveProgress(ctx, ich.ID, "b", 3_000_000, 6_000_000)
	_ = s.SetRating(ctx, ich.ID, "c", 2, false)

	liste := []UebernahmeEintrag{
		{ItemID: "a", PositionMS: 60_000, DauerMS: 6_000_000},               // wäre schlechter
		{ItemID: "b", PositionMS: 1_000_000, DauerMS: 6_000_000},            // weniger weit
		{ItemID: "c", Sterne: 5, Favorit: true},                             // Bewertung bleibt, Herz kommt
		{ItemID: "d", Gesehen: true, DauerMS: 6_000_000},                    // neu gesehen
		{ItemID: "e", PositionMS: 2_000_000, DauerMS: 6_000_000, Sterne: 4}, // neu angefangen
	}
	alle := UebernahmeArten{true, true, true, true}

	// Vorschau ändert nichts.
	probe, err := s.UebernahmeAnwenden(ctx, ich.ID, liste, alle, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Progress(ctx, ich.ID, "d"); err == nil {
		t.Fatal("die Vorschau hat geschrieben")
	}
	erg, err := s.UebernahmeAnwenden(ctx, ich.ID, liste, alle, false)
	if err != nil {
		t.Fatal(err)
	}
	if erg != probe {
		t.Fatalf("Vorschau %+v und Ergebnis %+v weichen ab", probe, erg)
	}
	if erg.Gesehen != 1 || erg.Angefangen != 1 || erg.Favoriten != 1 || erg.Bewertungen != 1 || erg.Unveraendert != 3 {
		t.Fatalf("Ergebnis: %+v", erg)
	}
	if p, _ := s.Progress(ctx, ich.ID, "a"); !p.Finished {
		t.Fatal("„gesehen“ wurde zurückgenommen")
	}
	if p, _ := s.Progress(ctx, ich.ID, "b"); p.PositionMS != 3_000_000 {
		t.Fatalf("weitere Stelle überschrieben: %d", p.PositionMS)
	}
	if r, _ := s.RatingsFor(ctx, ich.ID, []string{"c", "e"}); r["c"] != 2 || r["e"] != 4 {
		t.Fatalf("Bewertungen: %v", r)
	}
	if p, _ := s.Progress(ctx, ich.ID, "d"); !p.Finished {
		t.Fatal("neu gesehen fehlt")
	}
	// Das andere Profil bleibt unberührt.
	if ids, _ := s.ResumeIDs(ctx, andere.ID, 10); len(ids) != 0 {
		t.Fatalf("fremdes Profil hat Stand bekommen: %v", ids)
	}
	if ids, _ := s.FavoritIDs(ctx, andere.ID); len(ids) != 0 {
		t.Fatalf("fremdes Profil hat Favoriten bekommen: %v", ids)
	}

	// Nur Favoriten gewählt: kein Fortschritt.
	mustExec(t, s, `INSERT INTO item (id,library_id,kind,title,sort_title,added_at,updated_at) VALUES ('f','lib','movie','f','f',0,0)`)
	erg, _ = s.UebernahmeAnwenden(ctx, ich.ID, []UebernahmeEintrag{{ItemID: "f", Gesehen: true, Favorit: true}},
		UebernahmeArten{Favoriten: true}, false)
	if erg.Gesehen != 0 || erg.Favoriten != 1 {
		t.Fatalf("Auswahl nicht beachtet: %+v", erg)
	}
	if _, err := s.Progress(ctx, ich.ID, "f"); err == nil {
		t.Fatal("trotz Abwahl als gesehen übernommen")
	}
}

// Wie playback.go: Jede ausgeführte Funktion in uebernahme.go, die
// private Tabellen anfasst, nimmt das Konto als zweiten Parameter.
func TestUebernahmeNimmtKontoZuerst(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filepath.Join(dir(t), "uebernahme.go"), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	gefunden := false
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Recv == nil || fn.Name.Name != "UebernahmeAnwenden" {
			continue
		}
		gefunden = true
		var names []string
		for _, p := range fn.Type.Params.List {
			for _, n := range p.Names {
				names = append(names, n.Name)
			}
		}
		if len(names) < 2 || names[0] != "ctx" || names[1] != "accountID" {
			t.Fatalf("UebernahmeAnwenden: erwartet (ctx, accountID, …), bekommen %v", names)
		}
	}
	if !gefunden {
		t.Fatal("UebernahmeAnwenden fehlt")
	}
}
