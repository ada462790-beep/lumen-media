// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package meta

// Was der Scanner neben den Dateien gefunden hat (NFO, Bilder), fliesst
// hier in die Zuordnung ein:
//
//   - Nennt die NFO eine Kennung (TMDb, IMDb, TVDb), wird nicht gesucht,
//     sondern genau dieser Eintrag geholt — wie bei einem eingefügten Link.
//   - Ist „Lokale Metadaten (NFO) bevorzugen" an (Vorgabe), gelten die
//     Werte der NFO vor denen von TMDb.
//   - Lokale Bilder ersetzt TMDb nie (siehe saveArtwork).

import (
	"context"
	"encoding/json"

	"lumen/internal/lokal"
	"lumen/internal/store"
)

// EinstellungNFOBevorzugen ist der Schlüssel des Schalters in der
// Verwaltung; ohne Eintrag gilt er als an.
const EinstellungNFOBevorzugen = "meta.nfo_bevorzugen"

// NFOBevorzugt sagt, ob die Werte aus NFO-Dateien vor TMDb gelten.
func NFOBevorzugt(ctx context.Context, st *store.Store) bool {
	return st.Setting(ctx, EinstellungNFOBevorzugen, "1") == "1"
}

// nfoZu liest die NFO, die der Scanner zu einem Titel gemerkt hat.
func (r *Resolver) nfoZu(ctx context.Context, itemID string) *lokal.NFO {
	roh, ok := r.st.LokaleNFOZu(ctx, itemID)
	if !ok {
		return nil
	}
	var n lokal.NFO
	if json.Unmarshal([]byte(roh.Daten), &n) != nil {
		return nil
	}
	return &n
}

// lokalerTreffer holt den Eintrag, den die NFO nennt — ohne Suche. kind
// ist „movie" oder „series". Gibt nil zurück, wenn die NFO keine Kennung
// hat oder TMDb sie nicht kennt; dann wird wie gewohnt gesucht.
func (r *Resolver) lokalerTreffer(ctx context.Context, it *store.Item, kind string) (*Candidate, string) {
	n := r.nfoZu(ctx, it.ID)
	if n == nil || !n.HatKennung() {
		return nil, ""
	}
	holen := func(id string) (*Candidate, string) {
		var c *Candidate
		var err error
		if kind == "series" {
			c, err = r.tmdb.SeriesDetails(ctx, id)
		} else {
			c, err = r.tmdb.Details(ctx, id)
		}
		if err != nil || c == nil {
			return nil, ""
		}
		return c, id
	}
	if n.TMDb != "" {
		if c, id := holen(n.TMDb); c != nil {
			return c, id
		}
	}
	if n.IMDb != "" {
		if filme, serien, err := r.tmdb.FindIMDb(ctx, n.IMDb); err == nil {
			ids := filme
			if kind == "series" {
				ids = serien
			}
			if len(ids) > 0 {
				if c, id := holen(ids[0]); c != nil {
					return c, id
				}
			}
		}
	}
	if n.TVDb != "" && kind == "series" {
		if ids, err := r.tmdb.FindTVDb(ctx, n.TVDb); err == nil && len(ids) > 0 {
			if c, id := holen(ids[0]); c != nil {
				return c, id
			}
		}
	}
	return nil, ""
}

// lokalUeberlagern legt die Werte der NFO über das, was TMDb gebracht
// hat — wenn der Besitzer das so will.
func (r *Resolver) lokalUeberlagern(ctx context.Context, it *store.Item) {
	if !NFOBevorzugt(ctx, r.st) {
		return
	}
	if n := r.nfoZu(ctx, it.ID); n != nil {
		n.Anwenden(it, AgeYears)
	}
}

// nachLand behält von den Kandidaten die aus dem Land, das am Ordner
// stand („The Office (US)"). Steht bei keinem das Land, bleiben alle —
// dann entscheidet wie bisher der Titel.
func nachLand(cands []Candidate, land string) []Candidate {
	if land == "" {
		return cands
	}
	var passend []Candidate
	for _, c := range cands {
		for _, l := range c.Laender {
			if l == land {
				passend = append(passend, c)
				break
			}
		}
	}
	if len(passend) == 0 {
		return cands
	}
	return passend
}
