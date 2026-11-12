// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package scan

// Umbenannte und verschobene Dateien.
//
// Wer „Alien (1979)" in „Alien - Das unheimliche Wesen aus einer fremden
// Welt (1979)" umbenannte, bekam zwei Filme: den alten mit Gesehen-Haken
// und Merkliste, aber ohne Datei, und einen neuen ohne jeden Stand — und
// Lumen bildete daraus die Filmreihe „Alien · 2 Filme". Jetzt gleicht der
// Durchlauf am Ende ab: Eine Datei, die eben verschwunden ist, und eine,
// die eben neu aufgetaucht ist, mit derselben Grösse und derselben
// Laufzeit (oder im selben Ordner), sind dieselbe — so halten es auch Emby
// und Jellyfin. Gezählt wird nur, was eindeutig ist: Passen zwei neue
// Dateien zu einer verschwundenen, bleibt alles, wie es ist.

import (
	"context"
	"path/filepath"

	"lumen/internal/store"
)

// umbenennungenErkennen gleicht die eben fehlend gewordenen Dateien (weg)
// mit den in diesem Durchlauf neuen ab. Zurück: wie viele Dateien
// umgezogen sind und wie viele dabei neu angelegte Titel wieder
// verschwanden.
func (s *Scanner) umbenennungenErkennen(ctx context.Context, lib store.Library, weg []store.MediaFile) (umgezogen, titelWeg int) {
	m := merkerVon(ctx)
	if m == nil || len(weg) == 0 {
		return 0, 0
	}
	m.mu.Lock()
	pfade := append([]string(nil), m.neuePfade...)
	m.mu.Unlock()
	if len(pfade) == 0 {
		return 0, 0
	}
	var neu []store.MediaFile
	for _, p := range pfade {
		if g := s.st.GemessenZuPfad(ctx, p); g != nil && !g.Datei.Missing && g.Datei.SizeBytes > 0 {
			neu = append(neu, g.Datei)
		}
	}
	passt := func(a, b store.MediaFile) bool {
		if a.SizeBytes != b.SizeBytes || a.SizeBytes <= 0 {
			return false
		}
		if a.DurationMS > 0 && b.DurationMS > 0 {
			d := a.DurationMS - b.DurationMS
			return d < 1000 && d > -1000
		}
		// Ohne Laufzeit (nicht messbar, ohne FFmpeg) nur im selben Ordner.
		return filepath.Dir(a.Path) == filepath.Dir(b.Path)
	}
	// Je verschwundene Datei die passenden neuen — und umgekehrt.
	zuAlt := make([][]int, len(weg))
	zuNeu := make([]int, len(neu))
	for i := range weg {
		for j := range neu {
			if passt(weg[i], neu[j]) {
				zuAlt[i] = append(zuAlt[i], j)
				zuNeu[j]++
			}
		}
	}
	for i, kand := range zuAlt {
		if len(kand) != 1 || zuNeu[kand[0]] != 1 {
			continue
		}
		alt, n := weg[i], neu[kand[0]]
		altItem, err := s.st.Item(ctx, alt.ItemID)
		if err != nil {
			continue
		}
		neuItem, err := s.st.Item(ctx, n.ItemID)
		if err != nil || neuItem.LibraryID != lib.ID || altItem.LibraryID != lib.ID {
			continue
		}
		ok, err := s.st.DateiUmziehen(ctx, alt.ID, n.ID)
		if err != nil || !ok {
			continue
		}
		umgezogen++
		if neuItem.ID == altItem.ID {
			continue
		}
		titelWeg++
		// Der alte Titel bleibt — mit allem, was daran hängt. War er nie
		// erkannt, gilt der neue Name: Vielleicht wurde gerade deshalb
		// umbenannt.
		if !s.festerTitel(ctx, altItem) && neuItem.Title != altItem.Title {
			altItem.Title = neuItem.Title
			if neuItem.Year > 0 {
				altItem.Year = neuItem.Year
			}
			altItem.MatchState = "pending"
			_ = s.st.UpsertItem(ctx, altItem)
		}
	}
	return umgezogen, titelWeg
}
