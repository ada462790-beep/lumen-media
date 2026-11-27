// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

import (
	"errors"
	"testing"
)

// Umhängen nach dem Umpacken: gleiche Kennung, neuer Pfad, neue Spuren;
// Stand, Fassung und Vorspann-Erkennung bleiben.
func TestDateiUmhaengen(t *testing.T) {
	s := testStore(t)
	ctx := t.Context()
	lib, err := s.CreateLibrary(ctx, "Serien", "series", "/m", "de")
	if err != nil {
		t.Fatal(err)
	}
	sr := &Item{LibraryID: lib.ID, Kind: "series", Title: "Alt"}
	_ = s.UpsertItem(ctx, sr)
	st := &Item{LibraryID: lib.ID, Kind: "season", Title: "Staffel 1", ParentID: sr.ID, SeasonNo: 1}
	_ = s.UpsertItem(ctx, st)
	ep := &Item{LibraryID: lib.ID, Kind: "episode", Title: "Pilot", ParentID: st.ID, SeasonNo: 1, EpisodeNo: 1}
	if err := s.UpsertItem(ctx, ep); err != nil {
		t.Fatal(err)
	}
	f := &MediaFile{ItemID: ep.ID, Path: "/m/Alt/S01/Alt S01E01.avi", SizeBytes: 1000, MTime: 10, Container: "avi",
		DurationMS: 60000, Spuren: []Track{{Index: 2, Kind: "audio", Codec: "mp3"}}}
	if err := s.UpsertFile(ctx, f); err != nil {
		t.Fatal(err)
	}
	if err := s.ErkennungSetzen(ctx, Erkennung{FileID: f.ID, Groesse: 1000, MTime: 10, Fassung: 1, Stand: "fertig",
		VorspannVonMS: 1000, VorspannBisMS: 30000, VorspannSich: 0.9}); err != nil {
		t.Fatal(err)
	}
	acc, _ := s.CreateAccount(ctx, NewAccount{Name: "Mara", LockKind: "pin", Secret: "4711"})
	_ = s.SaveProgress(ctx, acc.ID, ep.ID, 12000, 60000)

	// Die Liste findet die Datei über die Serie und über die Folge.
	for _, id := range []string{"", sr.ID, ep.ID} {
		d, err := s.DateienMitEndung(ctx, []string{".avi", ".mpeg"}, id)
		if err != nil || len(d) != 1 || d[0].FileID != f.ID || d[0].Serie != "Alt" || d[0].Folge != 1 {
			t.Errorf("DateienMitEndung(%q): %+v %v", id, d, err)
		}
	}
	if d, _ := s.DateienMitEndung(ctx, []string{".wmv"}, ""); len(d) != 0 {
		t.Errorf(".wmv: %+v", d)
	}

	neu := &MediaFile{Path: "/m/Alt/S01/Alt S01E01.mkv", SizeBytes: 950, MTime: 20, Container: "mkv",
		DurationMS: 60010, Spuren: []Track{{Index: 1, Kind: "audio", Codec: "mp3"}}}
	// Steht die Datei nicht mehr unter dem alten Pfad: nichts tun.
	if err := s.DateiUmhaengen(ctx, f.ID, "/m/anders.avi", neu); !errors.Is(err, ErrDateiVeraendert) {
		t.Errorf("anderer Pfad: %v", err)
	}
	if err := s.DateiUmhaengen(ctx, f.ID, f.Path, neu); err != nil {
		t.Fatal(err)
	}
	g, err := s.File(ctx, f.ID)
	if err != nil || g.Path != neu.Path || g.SizeBytes != 950 || g.Container != "mkv" || g.ItemID != ep.ID || g.DurationMS != 60010 {
		t.Errorf("Datei: %+v %v", g, err)
	}
	spuren, _ := s.Spuren(ctx, f.ID)
	if len(spuren) != 1 || spuren[0].Index != 1 {
		t.Errorf("Spuren: %+v", spuren)
	}
	if e, err := s.Erkannt(ctx, f.ID, 950, 20, 1); err != nil || e == nil || e.VorspannBisMS != 30000 || e.Groesse != 950 || e.MTime != 20 {
		t.Errorf("Vorspann-Erkennung gilt nicht mehr: %+v %v", e, err)
	}
	if p, _ := s.Progress(ctx, acc.ID, ep.ID); p == nil || p.PositionMS != 12000 {
		t.Errorf("Stand: %+v", p)
	}
	// Ein zweites Umhängen auf einen belegten Pfad wird abgelehnt.
	f2 := &MediaFile{ItemID: ep.ID, Path: "/m/Alt/S01/x.avi", SizeBytes: 1, MTime: 1, PartNo: 2}
	_ = s.UpsertFile(ctx, f2)
	if err := s.DateiUmhaengen(ctx, f2.ID, f2.Path, &MediaFile{Path: neu.Path, Container: "mkv"}); !errors.Is(err, ErrDateiVeraendert) {
		t.Errorf("belegter Pfad: %v", err)
	}
}
