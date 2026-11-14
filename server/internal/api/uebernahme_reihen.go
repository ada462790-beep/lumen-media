// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

// Filmreihen aus Emby oder Jellyfin übernehmen — als Teil des Umzugs.
//
// Emby nennt sie Sammlungen (BoxSet). Lumen gleicht ihre Filme ab
// (uebernahme.ReihenPlanen) und zeigt vorher, welche Reihen fehlen und
// welche ergänzt würden. Angelegt werden sie als übernommene Reihen
// (store/reihen_emby.go): Sie gehen vor den erratenen und denen von TMDb,
// und kein Durchlauf nimmt sie wieder auseinander.
//
// Wie die Zuordnungen: nur mit Verwaltungsrechten und nur mit Filmen aus
// Bibliotheken, die das Konto verwaltet. Ins Protokoll kommt nur die
// Anzahl.

import (
	"context"
	"fmt"

	"lumen/internal/store"
	"lumen/internal/uebernahme"
)

// reihenErgebnis: was das Übernehmen der Reihen gebracht hat.
type reihenErgebnis struct {
	Angelegt  int `json:"angelegt"`
	Ergaenzt  int `json:"ergaenzt"`
	FilmeDazu int `json:"filmeDazu"`
}

// reihenIndex: nur die Filme der erlaubten Bibliotheken.
func reihenIndex(katalog []store.KatalogTitel, dateien []store.KatalogDatei, darf map[string]bool) (*uebernahme.Index, map[string]bool) {
	erlaubt := map[string]bool{}
	var lt []uebernahme.LumenTitel
	for _, t := range katalog {
		if t.Kind != "movie" || (darf != nil && !darf[t.LibraryID]) {
			continue
		}
		erlaubt[t.ID] = true
		lt = append(lt, uebernahme.LumenTitel{ID: t.ID, Kind: t.Kind, Titel: t.Titel, Original: t.Original,
			Jahr: t.Jahr, TMDb: t.TMDb, IMDb: t.IMDb, TVDb: t.TVDb})
	}
	var ld []uebernahme.LumenDatei
	for _, d := range dateien {
		if erlaubt[d.ItemID] {
			ld = append(ld, uebernahme.LumenDatei{ItemID: d.ItemID, Pfad: d.Pfad, Groesse: d.Groesse})
		}
	}
	return uebernahme.NeuerIndex(lt, ld), erlaubt
}

// reihenPlanen vergleicht die Sammlungen aus Emby mit den Reihen in Lumen.
func (s *Server) reihenPlanen(ctx context.Context, emby []uebernahme.EmbyReihe, katalog []store.KatalogTitel,
	dateien []store.KatalogDatei, darf map[string]bool) (*uebernahme.ReihenPlan, error) {
	ix, _ := reihenIndex(katalog, dateien, darf)
	alle, err := s.st.Reihen(ctx, true)
	if err != nil {
		return nil, err
	}
	lumen := make([]uebernahme.LumenReihe, 0, len(alle))
	for _, r := range alle {
		lumen = append(lumen, uebernahme.LumenReihe{ID: r.ID, Name: r.Name, TMDb: r.TMDbID,
			Quelle: r.Source, Hidden: r.Hidden, ItemIDs: r.ItemIDs})
	}
	p := uebernahme.ReihenPlanen(emby, ix, lumen)
	return &p, nil
}

// reihenAnwenden legt die fehlenden Reihen an und ergänzt die übrigen.
// darf: die Bibliotheken, die das Konto jetzt verwaltet — sie können sich
// seit der Vorschau geändert haben.
func (s *Server) reihenAnwenden(ctx context.Context, p *uebernahme.ReihenPlan, darf map[string]bool) (*reihenErgebnis, error) {
	katalog, _, err := s.st.UebernahmeKatalog(ctx)
	if err != nil {
		return nil, err
	}
	erlaubt := map[string]bool{}
	for _, t := range katalog {
		if t.Kind == "movie" && (darf == nil || darf[t.LibraryID]) {
			erlaubt[t.ID] = true
		}
	}
	nur := func(ids []string) []string {
		var out []string
		for _, id := range ids {
			if erlaubt[id] {
				out = append(out, id)
			}
		}
		return out
	}
	erg := &reihenErgebnis{}
	for _, v := range p.Fehlen {
		filme := nur(v.Filme)
		if len(filme) < 2 {
			continue
		}
		if _, err := s.st.EmbyReiheAnlegen(ctx, v.Name, v.TMDb, filme); err != nil {
			return erg, err
		}
		erg.Angelegt++
		erg.FilmeDazu += len(filme)
	}
	for _, v := range p.Ergaenzen {
		neu := nur(v.Neu)
		if len(neu) == 0 {
			continue
		}
		if _, err := s.st.ReiheErgaenzen(ctx, v.LumenID, neu); err != nil {
			if err == store.ErrNotFound {
				continue // inzwischen gelöscht
			}
			return erg, err
		}
		erg.Ergaenzt++
		erg.FilmeDazu += len(neu)
	}
	if erg.Angelegt+erg.Ergaenzt > 0 {
		s.st.Log(ctx, "info", "reihen", fmt.Sprintf(
			"Filmreihen aus Emby übernommen: %d angelegt, %d ergänzt", erg.Angelegt, erg.Ergaenzt))
		// Erratene Reihen, deren Filme jetzt in einer übernommenen stehen,
		// verschwinden beim Neubilden.
		go s.ReihenBilden(s.hintergrund())
	}
	return erg, nil
}
