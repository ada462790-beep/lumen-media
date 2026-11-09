// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

// Fehlende Untertitel von selbst holen — wenn die Verwaltung es will.
//
// Läuft im Hintergrund und mit niedriger Priorität: höchstens alle
// zwanzig Minuten ein Durchgang, nie während etwas umgewandelt wird,
// eine Pause zwischen den Anfragen, und nie mehr als das Tageslimit. Eine
// Datei, für die es nichts gab, wird zwei Wochen lang nicht wieder
// angefragt. Genommen wird nur, was sicher passt: ein Treffer über den
// Hash genau dieser Datei, oder über die Kennung des Films und von einem
// Menschen übersetzt.

import (
	"context"
	"errors"
	"fmt"
	"lumen/internal/store"
	"strings"
	"time"

	"lumen/internal/untertitel"
)

// utPause: zwischen zwei Anfragen an OpenSubtitles.
var utPause = 2 * time.Second

// UntertitelWaechter läuft, bis ctx endet.
func (s *Server) UntertitelWaechter(ctx context.Context) {
	warte := 3 * time.Minute
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(warte):
		}
		warte = 20 * time.Minute
		s.untertitelDurchgang(ctx)
	}
}

// untertitelDurchgang holt, was das Tageslimit erlaubt. Gibt zurück, wie
// viele Dateien es wurden.
func (s *Server) untertitelDurchgang(ctx context.Context) int {
	if s.st.Setting(ctx, keyUTAuto, "0") != "1" {
		return 0
	}
	c := s.untertitelClient(ctx)
	if !c.Bereit() {
		return 0
	}
	// Die Wiedergabe geht vor.
	if s.live != nil && s.live.Load().Transcodes > 0 {
		return 0
	}
	tag, schon := s.utHeute(ctx)
	budget := s.utTageslimit(ctx) - schon
	if budget <= 0 {
		return 0
	}
	sprachen := sprachListe(s.st.Setting(ctx, keyUTSprachen, "de"))
	seit := time.Now().Add(-14 * 24 * time.Hour).UTC().Truncate(24 * time.Hour).Unix()
	geholt := 0
	for _, sprache := range sprachen {
		dateien, err := s.st.DateienOhneUntertitel(ctx, sprache, seit, budget*3)
		if err != nil {
			return geholt
		}
		for i := range dateien {
			if budget <= 0 || ctx.Err() != nil {
				break
			}
			f := &dateien[i]
			// Geholte Untertitel stehen nicht in media_track (siehe
			// Store.GeholteUntertitel) — die Abfrage oben sieht sie nicht.
			if geholtIn(s.st.GeholteUntertitel(f.ID), sprache) {
				continue
			}
			it, err := s.st.Item(ctx, f.ItemID)
			if err != nil {
				continue
			}
			su := s.untertitelSuche(ctx, it, f, []string{sprache})
			treffer, err := c.Suchen(ctx, su)
			time.Sleep(utPause)
			if err != nil {
				if errors.Is(err, untertitel.ErrSchluessel) || errors.Is(err, untertitel.ErrAnmeldung) {
					s.st.Log(ctx, "warn", "untertitel", "automatisches Holen angehalten: "+err.Error())
					return geholt
				}
				continue
			}
			wahl := sichererTreffer(treffer, sprache, su.IMDbID != "" || su.TMDbID != "")
			if wahl == nil {
				_ = s.st.UntertitelVersuchMerken(ctx, f.ID, sprache, false)
				continue
			}
			_, rest, err := s.untertitelAblegen(ctx, c, f, wahl.DateiID, sprache, wahl.Hoergeschaedigt, wahl.Erzwungen)
			time.Sleep(utPause)
			if errors.Is(err, untertitel.ErrKontingent) {
				// Das Kontingent bei OpenSubtitles ist aufgebraucht — auch für
				// die Suche von Hand. Heute nichts mehr.
				budget = 0
				break
			}
			if err != nil {
				_ = s.st.UntertitelVersuchMerken(ctx, f.ID, sprache, false)
				continue
			}
			_ = s.st.UntertitelVersuchMerken(ctx, f.ID, sprache, true)
			geholt++
			budget--
			schon++
			_ = s.st.SetSetting(ctx, keyUTHeute, fmt.Sprintf("%s:%d", tag, schon))
			// Ein paar Downloads für die Suche von Hand übrig lassen.
			if rest >= 0 && rest <= 2 {
				budget = 0
			}
		}
	}
	if geholt > 0 {
		// Nur die Zahl, kein Titel: Das Protokoll beschreibt den Server.
		s.st.Log(ctx, "info", "untertitel", fmt.Sprintf("%d Untertitel automatisch geholt", geholt))
	}
	return geholt
}

// sichererTreffer wählt nur, was sicher zu dieser Datei passt.
func sichererTreffer(treffer []untertitel.Treffer, sprache string, mitKennung bool) *untertitel.Treffer {
	for i := range treffer {
		t := &treffer[i]
		if !strings.EqualFold(t.Sprache, sprache) || t.Erzwungen {
			continue
		}
		if t.HashTreffer || (mitKennung && !t.Maschinell) {
			return t
		}
	}
	return nil
}

// geholtIn: ob unter den geholten Untertiteln einer in dieser Sprache ist.
func geholtIn(spuren []store.Track, sprache string) bool {
	for _, t := range spuren {
		if t.Language == sprache {
			return true
		}
	}
	return false
}
