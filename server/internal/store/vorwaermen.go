// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

// Vorwärmen: die kleinen, oft gebrauchten Teile der Datenbank einmal lesen,
// damit sie im Arbeitsspeicher der NAS liegen, bevor jemand sie braucht.
//
// Nach dem Start liegt nichts davon im Speicher, nach ein paar Stunden
// Ruhe oft auch nicht mehr (die NAS braucht ihn für anderes). Dann liest
// die erste Seite jeden Indexblock einzeln von der Platte. Vorgewärmt wird
// nur beim Start und beim ersten Zugriff nach langer Ruhe, nie von selbst
// in Abständen: Im Leerlauf sollen die Platten schlafen dürfen.

import (
	"context"
	"sync/atomic"
	"time"
)

// ruheAb: so lange ohne Anfrage gilt als Ruhe.
const ruheAb = 30 * time.Minute

// vorwaermIndizes: die schmalen Indizes (schmal.go) und was die Startseite
// sonst braucht — zusammen wenige Megabyte. Nichts aus den Tabellen eines
// Profils: Den eigenen Fortschritt liest jedes Profil für sich
// (abbildMitBaum).
var vorwaermIndizes = [][2]string{
	{"item", "item_baum"},
	{"item", "item_by_parent"},
	{"item", "item_ord_neu"},
	{"item", "item_ord_titel"},
	{"item", "item_ord_wertung"},
	{"media_file", "file_kennzahl"},
	{"collection_item", "collection_item_by_item"},
	{"genre", "genre_by_name"},
}

var (
	letzterZugriff atomic.Int64 // Unix-Sekunden
	waermtGerade   atomic.Bool
)

// Zugriff meldet eine Anfrage (api/messen.go). War vorher lange Ruhe,
// wärmt Lumen einmal im Hintergrund vor.
func (s *Store) Zugriff() {
	jetzt := time.Now().Unix()
	vorher := letzterZugriff.Swap(jetzt)
	if vorher != 0 && time.Duration(jetzt-vorher)*time.Second >= ruheAb {
		go s.vorwaermen(Hintergrund(context.Background()), false)
	}
}

// Vorwaermen beim Start: die Indizes und, was im Speicher bleibt (der
// Fortschritt jedes Profils samt Serien, die Filmreihen).
func (s *Store) Vorwaermen(ctx context.Context) {
	letzterZugriff.CompareAndSwap(0, time.Now().Unix())
	s.vorwaermen(Hintergrund(ctx), true)
}

func (s *Store) vorwaermen(ctx context.Context, mitSpeicher bool) {
	if !waermtGerade.CompareAndSwap(false, true) {
		return
	}
	defer waermtGerade.Store(false)
	for _, x := range vorwaermIndizes {
		if ctx.Err() != nil {
			return
		}
		var n int
		// Zählen über den Index liest jeden seiner Blöcke einmal.
		_ = s.db.QueryRowContext(ctx, `SELECT count(*) FROM `+x[0]+` INDEXED BY `+x[1]).Scan(&n)
	}
	if !mitSpeicher {
		return
	}
	_, _ = s.Reihen(ctx, true)
	// Filme und Serien: ihre Zeilen, Bilder und Abzeichen
	// (mediathek_merken.go, kennzahlen.go) — das, was Startseite und
	// Raster zeigen. Höchstens so viele, wie der Speicher behält.
	if rows, err := s.db.QueryContext(ctx,
		`SELECT id FROM item INDEXED BY item_ord_neu WHERE kind IN ('movie','series') LIMIT ?`, itemMerkHoechstens/2); err == nil {
		ids, err := collectStrings(rows)
		rows.Close()
		if err == nil && ctx.Err() == nil {
			if items, err := s.itemsInReihenfolge(ctx, ids); err == nil {
				_, _, _ = s.ArtworkMitStand(ctx, ids)
				_, _ = s.KennzahlenFuer(ctx, items)
			}
		}
	}
	konten, err := s.Accounts(ctx)
	if err != nil {
		return
	}
	for _, a := range konten {
		if ctx.Err() != nil {
			return
		}
		// Füllt das Abbild des eigenen Fortschritts und den Baum der
		// Serien (fortschritt_abbild.go) — je Profil für sich.
		_, _, _ = s.abbildMitBaum(ctx, a.ID)
	}
}
