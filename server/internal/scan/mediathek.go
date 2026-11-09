// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package scan

import (
	"context"
	"fmt"
	"time"

	"lumen/internal/store"
)

// scanMediathek ist der Rahmen um die Durchläufe für Musik und Fotos:
// Ergebnis festhalten und ins Protokoll schreiben wie bei Filmen —
// Anzahl und Dauer, keine Titel.
func (s *Scanner) scanMediathek(ctx context.Context, lib store.Library) (*Result, error) {
	start := time.Now()
	res := &Result{Library: lib.Name}
	var err error
	if lib.Kind == "music" {
		err = s.scanMusik(ctx, lib, res)
	} else {
		err = s.scanFotos(ctx, lib, res)
	}
	if err != nil {
		return nil, err
	}
	res.Took = time.Since(start)
	res.TookMS = res.Took.Milliseconds()
	_ = s.st.ScanErgebnisSetzen(ctx, lib.ID, store.ScanErgebnis{
		At: time.Now(), Added: res.Added, Updated: res.Updated, Missing: res.Missing,
		TookMS: res.TookMS, Auto: ctx.Value(vonSelbstKey{}) == true,
	})
	s.st.Log(ctx, "info", "scan", fmt.Sprintf("%s geprüft · %d neu, %d aktualisiert, %d fehlen · %s",
		lib.Name, res.Added, res.Updated, res.Missing, res.Took.Round(time.Millisecond)))
	if s.NachMediathek != nil {
		s.NachMediathek(lib)
	}
	return res, nil
}
