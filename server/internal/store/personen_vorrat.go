// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

// Die Namen aus der Besetzung, im Speicher.
//
// Jede Suche lud bis zu 5 000 Namen samt Rechte-Abfrage aus der
// Datenbank, um die Eingabe zu deuten („Filme mit Tom Hanks"), und
// PersonenSuchen ging mit lower(name) LIKE '%…%' über die ganze Besetzung
// (bei 77 000 Zeilen 13 → 50–75 ms je Suche). Und ab 5 000 Namen wurde
// eine Person womöglich gar nicht erkannt. Jetzt liegen die Namen im
// Speicher — neu gelesen, wenn sich eine Besetzung geändert hat, sonst
// höchstens alle zwei Minuten.

import (
	"context"
	"strings"
	"sync"
	"time"
)

type personenVorrat struct {
	mu       sync.Mutex
	namen    []string
	klein    []string // dieselben, klein geschrieben (strings.ToLower)
	um       time.Time
	veraltet bool
}

// vorraete: je Datenbank ein Vorrat — ein Stapel (store.Stapel) teilt
// die Datenbank und damit den Vorrat.
var vorraete sync.Map // *sql.DB → *personenVorrat

func (s *Store) personenVorrat() *personenVorrat {
	v, _ := vorraete.LoadOrStore(s.db, &personenVorrat{veraltet: true})
	return v.(*personenVorrat)
}

// personenVeraltet: Eine Besetzung hat sich geändert.
func (s *Store) personenVeraltet() {
	v := s.personenVorrat()
	v.mu.Lock()
	v.veraltet = true
	v.mu.Unlock()
}

// allePersonen liefert alle Namen (und klein geschrieben) — nicht
// verändern, die Listen sind geteilt.
func (s *Store) allePersonen(ctx context.Context) (namen, klein []string, err error) {
	v := s.personenVorrat()
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.veraltet && time.Since(v.um) < 2*time.Minute {
		return v.namen, v.klein, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT name FROM besetzung`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	n, k := []string{}, []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, nil, err
		}
		n = append(n, name)
		k = append(k, strings.ToLower(name))
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	v.namen, v.klein, v.um, v.veraltet = n, k, time.Now(), false
	return n, k, nil
}
