// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

// Fehlende Dateien, deren Titel noch eine andere Datei hat.
//
// Auf dem NAS eines Besitzers stand unter „Fehlende Dateien" eine Folge
// von „The Big Bang Theory", die es weiterhin gab — nur unter neuem Namen.
// Die Umbenennung erkennt der Durchlauf nur, wenn die alte Datei im selben
// Durchlauf verschwindet, in dem die neue auftaucht, und beide gleich gross
// sind (scan/umbenannt.go). Brach ein Durchlauf ab, nachdem er die neue
// schon eingetragen hatte, oder wurde die Folge durch eine andere Fassung
// ersetzt, blieb die alte Zeile als „fehlend" stehen — für immer.

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
)

// FehlendAngabe: wozu eine fehlende Datei gehört — für die Liste unter
// Aufräumen.
type FehlendAngabe struct {
	Kind    string `json:"kind"`
	Titel   string `json:"titel"`
	Jahr    int    `json:"jahr,omitempty"`
	Serie   string `json:"serie,omitempty"`
	Staffel int    `json:"staffel,omitempty"`
	Folge   int    `json:"folge,omitempty"`
	// Ersatz: Der Titel (Film oder Folge) hat noch eine vorhandene Datei
	// mit derselben Teilnummer oder eine ganze Datei, die alle fehlenden
	// Teile deckt (ersatzFuer) — die fehlende wurde vermutlich umbenannt
	// oder ersetzt.
	Ersatz     bool   `json:"ersatz,omitempty"`
	ErsatzPfad string `json:"ersatzPfad,omitempty"`
}

// FehlendeAngaben nennt zu jeder fehlenden Datei (fileIDs) ihren Titel.
func (s *Store) FehlendeAngaben(ctx context.Context, fileIDs []string) (map[string]FehlendAngabe, error) {
	out := map[string]FehlendAngabe{}
	for _, id := range fileIDs {
		var a FehlendAngabe
		var jahr, staffel, folge sql.NullInt64
		var serie sql.NullString
		var itemID string
		err := s.db.QueryRowContext(ctx,
			`SELECT i.kind, i.title, i.year, i.season_no, i.episode_no,
			        (SELECT sr.title FROM item st JOIN item sr ON sr.id = st.parent_id
			          WHERE i.kind = 'episode' AND st.id = i.parent_id),
			        f.item_id
			   FROM media_file f JOIN item i ON i.id = f.item_id WHERE f.id = ?`, id).
			Scan(&a.Kind, &a.Titel, &jahr, &staffel, &folge, &serie, &itemID)
		if err != nil {
			if err == sql.ErrNoRows {
				continue
			}
			return out, err
		}
		a.Jahr, a.Staffel, a.Folge = int(jahr.Int64), int(staffel.Int64), int(folge.Int64)
		a.Serie = serie.String
		dateien, err := s.ersatzDateien(ctx, `f.item_id = ?`, itemID)
		if err != nil {
			return out, err
		}
		for _, d := range dateien {
			if d.id == id {
				a.ErsatzPfad = ersatzFuer(d, dateien, false)
				a.Ersatz = a.ErsatzPfad != ""
			}
		}
		out[id] = a
	}
	return out, nil
}

// FehlendeMitErsatzAustragen trägt fehlende Dateien der Bibliothek aus,
// deren Titel noch eine vorhandene Datei mit derselben Teilnummer hat —
// wenn darf(fehlender Pfad, vorhandener Pfad) zustimmt (der Scanner prüft
// dort, dass es keine abgehängte Platte sein kann). Stand, Haken und
// Merkliste hängen am Titel und bleiben. Zurück: wie viele.
func (s *Store) FehlendeMitErsatzAustragen(ctx context.Context, libraryID string, darf func(fehlt, da string) bool) (int, error) {
	dateien, err := s.ersatzDateien(ctx, `i.library_id = ?`, libraryID)
	if err != nil {
		return 0, err
	}
	nachTitel := map[string][]ersatzDatei{}
	for _, d := range dateien {
		nachTitel[d.item] = append(nachTitel[d.item], d)
	}
	var weg []string
	for _, d := range dateien {
		if !d.fehlt {
			continue
		}
		if da := ersatzFuer(d, nachTitel[d.item], true); da != "" && (darf == nil || darf(d.pfad, da)) {
			weg = append(weg, d.id)
		}
	}
	if len(weg) == 0 {
		return 0, nil
	}
	err = s.tx(ctx, func(t *sql.Tx) error {
		for _, id := range weg {
			// Eine bevorzugte Fassung, die nicht mehr da ist, gilt nicht mehr.
			if _, err := t.ExecContext(ctx,
				`UPDATE item SET bevorzugte_datei = NULL WHERE bevorzugte_datei = ?`, id); err != nil {
				return err
			}
			if _, err := t.ExecContext(ctx, `DELETE FROM media_file WHERE id = ?`, id); err != nil {
				return fmt.Errorf("fehlende datei austragen: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return len(weg), nil
}

// ersatzDatei: was ersatzFuer von einer Datei wissen muss.
type ersatzDatei struct {
	id, item, pfad string
	teil           int
	fehlt, defekt  bool
	dauerMS, bytes int64
}

// ersatzDateien liest die Dateien der Titel, die wo (über f und i) trifft.
func (s *Store) ersatzDateien(ctx context.Context, wo string, arg any) ([]ersatzDatei, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT f.id, f.item_id, f.path, f.part_no, f.missing_since IS NOT NULL,
		        COALESCE(f.defekt,'') <> '', COALESCE(f.duration_ms,0), f.size_bytes
		   FROM media_file f JOIN item i ON i.id = f.item_id
		  WHERE `+wo+` ORDER BY f.path`, arg)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ersatzDatei
	for rows.Next() {
		var d ersatzDatei
		if err := rows.Scan(&d.id, &d.item, &d.pfad, &d.teil, &d.fehlt, &d.defekt, &d.dauerMS, &d.bytes); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// ersatzFuer: der Pfad einer vorhandenen Datei desselben Titels, die die
// fehlende f ersetzt — "" wenn keine. Ersatz ist
//
//   - eine vorhandene Datei mit derselben Teilnummer (umbenannt, andere
//     Fassung), oder
//   - eine vorhandene Datei ohne Teile (der Titel hat nur noch Teil 0/1),
//     die alle fehlenden Teile zusammen deckt: Eine DVD (VTS_01_1..3.VOB)
//     wurde in eine MKV umgewandelt, CD1/CD2 zu einer Datei gefügt. Die
//     ganze Datei ist mindestens 90 % so lang wie die fehlenden Teile
//     zusammen — fehlt Teil 1 nicht (er ist die vorhandene Datei, oder er
//     wurde schon ausgetragen), zählt für ihn ein Teil mittlerer Länge
//     dazu. Sonst bliebe ein Film, dessen Teile 2 und 3 versehentlich
//     gelöscht wurden, ohne Meldung mit Teil 1 allein stehen. Ohne
//     gemessene Laufzeiten gilt dasselbe für die Grösse (mit 80 %, weil
//     ein Umpacken etwas Platz spart).
//
// ohneDefekt: defekte Dateien zählen nicht als Ersatz.
func ersatzFuer(f ersatzDatei, alle []ersatzDatei, ohneDefekt bool) string {
	if !f.fehlt {
		return ""
	}
	var da []ersatzDatei
	for _, g := range alle {
		if g.id == f.id || g.item != f.item || g.fehlt || (ohneDefekt && g.defekt) {
			continue
		}
		if g.teil == f.teil {
			return g.pfad
		}
		da = append(da, g)
	}
	if len(da) == 0 || f.teil <= 1 {
		return ""
	}
	for _, g := range da {
		if g.teil > 1 {
			return "" // noch ein Film in Teilen: kein ganzer Ersatz
		}
	}
	// Die fehlenden Teile (je Teilnummer die längste).
	dauer, groesse := map[int]int64{}, map[int]int64{}
	mitDauer := true
	for _, g := range alle {
		if g.item != f.item || !g.fehlt {
			continue
		}
		if g.dauerMS <= 0 {
			mitDauer = false
		}
		dauer[g.teil] = max(dauer[g.teil], g.dauerMS)
		groesse[g.teil] = max(groesse[g.teil], g.bytes)
	}
	gebraucht := func(je map[int]int64) int64 {
		var summe int64
		for _, n := range je {
			summe += n
		}
		if _, ok := je[1]; !ok {
			if _, ok := je[0]; !ok && len(je) > 0 {
				summe += summe / int64(len(je))
			}
		}
		return summe
	}
	for _, g := range da {
		if mitDauer && g.dauerMS > 0 {
			if g.dauerMS*10 >= gebraucht(dauer)*9 {
				return g.pfad
			}
			continue
		}
		if g.bytes*10 >= gebraucht(groesse)*8 {
			return g.pfad
		}
	}
	return ""
}

// ScheibeErsetzen: Nach „In MKV umwandeln" ersetzt die MKV (neu) die
// Dateien der Scheibe — alle unter inhalt (VIDEO_TS bzw. BDMV), nicht nur
// Teil 1. neu kommt an den Titel, an dem die meisten dieser Dateien
// hängen (Stand, Haken und Merkliste bleiben so erhalten), die Dateien
// der Scheibe werden ausgetragen. Hängt keine Datei der Scheibe an einem
// Titel, bleibt neu ohne Titel (ItemID leer) und nichts geschieht — der
// Durchlauf erkennt die MKV dann wie jeden Film. Zurück: wie viele
// Dateien ausgetragen wurden.
func (s *Store) ScheibeErsetzen(ctx context.Context, libraryID, inhalt string, neu *MediaFile) (int, error) {
	dateien, err := s.ersatzDateien(ctx, `i.library_id = ?`, libraryID)
	if err != nil {
		return 0, err
	}
	praefix := filepath.Clean(inhalt) + string(filepath.Separator)
	zahl := map[string]int{}
	var scheibe []ersatzDatei
	for _, d := range dateien {
		if strings.HasPrefix(d.pfad, praefix) {
			scheibe = append(scheibe, d)
			zahl[d.item]++
		}
	}
	titel := ""
	for id, n := range zahl {
		if titel == "" || n > zahl[titel] || (n == zahl[titel] && id < titel) {
			titel = id
		}
	}
	if titel == "" {
		return 0, nil
	}
	neu.ItemID = titel
	if err := s.UpsertFile(ctx, neu); err != nil {
		return 0, err
	}
	weg := 0
	err = s.tx(ctx, func(t *sql.Tx) error {
		for _, d := range scheibe {
			if d.item != titel {
				continue
			}
			if _, err := t.ExecContext(ctx,
				`UPDATE item SET bevorzugte_datei = NULL WHERE bevorzugte_datei = ?`, d.id); err != nil {
				return err
			}
			if _, err := t.ExecContext(ctx, `DELETE FROM media_file WHERE id = ?`, d.id); err != nil {
				return err
			}
			weg++
		}
		return nil
	})
	return weg, err
}
