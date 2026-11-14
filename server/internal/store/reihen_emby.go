// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

// Filmreihen aus der Übernahme aus Emby oder Jellyfin (source 'emby').
//
// Wie die Reihen aus NFO-Dateien gehen sie vor der Reihe, die Lumen aus
// den Titeln errät, und vor der von TMDb: Wer seine Sammlungen in Emby
// gepflegt hat, will sie so wiederfinden. Anders als NFO-Reihen hängen
// sie an keiner Datei — kein Durchlauf nimmt einen Film wieder heraus.
// Umbenennen und Auflösen durch den Besitzer gehen wie bei jeder Reihe;
// eine Reihe ohne Filme verschwindet (ReihenNeuBilden).

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// EmbyReiheAnlegen legt eine übernommene Reihe an (oder findet sie
// wieder) und hängt die Filme an. Gibt die Kennung der Reihe zurück.
func (s *Store) EmbyReiheAnlegen(ctx context.Context, name, tmdbColID string, itemIDs []string) (string, error) {
	name, tmdbColID = strings.TrimSpace(name), strings.TrimSpace(tmdbColID)
	if name == "" {
		return "", errors.New("der name fehlt")
	}
	schluessel := "emby:" + normSchluessel(name)
	if tmdbColID != "" {
		schluessel = "emby:tmdb:" + tmdbColID
	}
	var id string
	err := s.tx(ctx, func(t *sql.Tx) error {
		err := t.QueryRowContext(ctx,
			`SELECT id FROM collection WHERE source = 'emby' AND schluessel = ?`, schluessel).Scan(&id)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			id = "col_" + kurzHash(schluessel)
			if _, err := t.ExecContext(ctx,
				`INSERT INTO collection (id, name, source, schluessel, tmdb_id) VALUES (?, ?, 'emby', ?, ?)
				 ON CONFLICT (id) DO UPDATE SET schluessel = excluded.schluessel`,
				id, name, schluessel, nullStr(tmdbColID)); err != nil {
				return err
			}
		case err != nil:
			return err
		}
		return reiheFilmeDazu(ctx, t, id, itemIDs)
	})
	return id, err
}

// ReiheErgaenzen hängt Filme an eine bestehende Reihe. Eine Reihe, die
// Lumen selbst erkannt hat, wird bei jedem Durchlauf neu gebildet — die
// Filme kämen dort wieder heraus. Darum nimmt das Ergänzen sie als
// übernommene Reihe auf (mit ihren bisherigen Filmen); die erratene
// verschwindet dann beim nächsten Neubilden von selbst.
func (s *Store) ReiheErgaenzen(ctx context.Context, id string, itemIDs []string) (string, error) {
	r, err := s.Reihe(ctx, id)
	if err != nil {
		return "", err
	}
	if r.Source == "auto" {
		alle := append(append([]string{}, r.ItemIDs...), itemIDs...)
		neu, err := s.EmbyReiheAnlegen(ctx, r.Name, "", alle)
		if err != nil {
			return "", err
		}
		// Hat der Besitzer die erkannte Reihe umbenannt, gilt sein Name.
		_, err = s.db.ExecContext(ctx,
			`UPDATE collection SET name = (SELECT name FROM collection WHERE id = ?),
			        eigener_name = (SELECT eigener_name FROM collection WHERE id = ?)
			  WHERE id = ?`, id, id, neu)
		if err != nil {
			return "", err
		}
		_, err = s.db.ExecContext(ctx, `DELETE FROM collection_item WHERE collection_id = ?`, id)
		return neu, err
	}
	return id, s.tx(ctx, func(t *sql.Tx) error { return reiheFilmeDazu(ctx, t, id, itemIDs) })
}

// reiheFilmeDazu hängt Filme an eine Reihe; nur Filme, die es gibt.
func reiheFilmeDazu(ctx context.Context, t *sql.Tx, id string, itemIDs []string) error {
	for _, item := range itemIDs {
		if _, err := t.ExecContext(ctx,
			`INSERT OR IGNORE INTO collection_item (collection_id, item_id, ord)
			 SELECT ?, i.id, COALESCE(i.year, 0) FROM item i WHERE i.id = ? AND i.kind = 'movie'`,
			id, item); err != nil {
			return err
		}
	}
	return nil
}
