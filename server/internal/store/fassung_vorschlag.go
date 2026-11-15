// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

// Vielleicht Fassungen desselben Films: „Das Boot (1981)" auf Deutsch und
// „The Boat (1981)" auf Englisch, im selben Ordner, gleich lang — ohne
// TMDb oder NFO weiss Lumen nicht, dass das ein Film ist, und legt zwei
// Einträge an. Aufräumen schlägt solche Paare vor; zusammengelegt wird
// nur von Hand. Von selbst nicht: In einem Sammelordner („_James Bond
// 007") stehen auch zwei verschiedene Filme desselben Jahres in
// verschiedenen Sprachen und fast gleich lang („Octopussy" und „Sag
// niemals nie", beide 1983).

import (
	"context"
	"path/filepath"
	"sort"
)

// FeldKeineFassung: Dieser Eintrag ist ein eigener Film (von Hand
// abgetrennt) — Aufräumen schlägt ihn nicht als Fassung eines anderen vor.
const FeldKeineFassung = "keinefassung"

// FassungSeite: ein Eintrag eines Vorschlags.
type FassungSeite struct {
	ItemID  string `json:"itemId"`
	Title   string `json:"title"`
	Year    int    `json:"year,omitempty"`
	Sprache string `json:"sprache"`
	DauerMS int64  `json:"dauerMs"`
	Path    string `json:"path"`
	LibID   string `json:"-"`
}

// FassungKandidaten: Paare von Filmen im selben Ordner, mit demselben
// Jahr, verschiedener Tonsprache und einer Laufzeit, die um höchstens
// 5 % abweicht (PAL-Beschleunigung: gut 4 %). Höchstens hoechstens Paare.
func (s *Store) FassungKandidaten(ctx context.Context, hoechstens int) ([][2]FassungSeite, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT i.id, i.library_id, i.title, i.year, f.path, f.duration_ms,
		        COALESCE((SELECT t.language FROM media_track t
		                   WHERE t.file_id = f.id AND t.kind = 'audio' ORDER BY t.idx LIMIT 1), '')
		   FROM media_file f JOIN item i ON i.id = f.item_id
		  WHERE i.kind = 'movie' AND i.year > 0 AND f.missing_since IS NULL
		    AND COALESCE(f.defekt,'') = '' AND f.duration_ms > 0
		    AND NOT EXISTS (SELECT 1 FROM feld_sperre x WHERE x.item_id = i.id AND x.feld = ?)
		  ORDER BY f.path`, FeldKeineFassung)
	if err != nil {
		return nil, err
	}
	type schluessel struct {
		ordner string
		jahr   int
	}
	gruppen := map[schluessel][]FassungSeite{}
	var reihe []schluessel
	for rows.Next() {
		var d FassungSeite
		if err := rows.Scan(&d.ItemID, &d.LibID, &d.Title, &d.Year, &d.Path, &d.DauerMS, &d.Sprache); err != nil {
			rows.Close()
			return nil, err
		}
		if d.Sprache == "" || d.Sprache == "und" {
			continue
		}
		k := schluessel{filepath.Dir(d.Path), d.Year}
		if _, ok := gruppen[k]; !ok {
			reihe = append(reihe, k)
		}
		gruppen[k] = append(gruppen[k], d)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(reihe, func(i, j int) bool { return reihe[i].ordner < reihe[j].ordner })
	var out [][2]FassungSeite
	schon := map[[2]string]bool{}
	for _, k := range reihe {
		g := gruppen[k]
		for i := range g {
			for j := i + 1; j < len(g); j++ {
				a, b := g[i], g[j]
				if a.ItemID == b.ItemID || a.Sprache == b.Sprache || a.LibID != b.LibID {
					continue
				}
				gross, klein := max(a.DauerMS, b.DauerMS), min(a.DauerMS, b.DauerMS)
				if (gross-klein)*20 > gross {
					continue
				}
				paar := [2]string{min(a.ItemID, b.ItemID), max(a.ItemID, b.ItemID)}
				if schon[paar] {
					continue
				}
				// Beide erkannt, mit verschiedener Kennung: verschiedene Filme.
				ta, _ := s.SourceID(ctx, a.ItemID, "tmdb")
				tb, _ := s.SourceID(ctx, b.ItemID, "tmdb")
				if ta != "" && tb != "" && ta != tb {
					continue
				}
				schon[paar] = true
				out = append(out, [2]FassungSeite{a, b})
				if len(out) >= hoechstens {
					return out, nil
				}
			}
		}
	}
	return out, nil
}

// KeineFassungSetzen merkt, dass itemID von Hand als eigener Film
// abgetrennt wurde (FeldKeineFassung).
func (s *Store) KeineFassungSetzen(ctx context.Context, itemID string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO feld_sperre (item_id, feld) VALUES (?, ?)`, itemID, FeldKeineFassung)
	return err
}
