// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

// Grenzen für die Einstellungen eines Profils (PATCH /me {"prefs"}).
//
// Prefs sind freie Schlüssel/Wert-Paare, damit eine App eine neue
// Einstellung speichern kann, ohne dass der Server sie kennt. Ohne Grenze
// konnte ein Profil damit aber beliebig viel in die Datenbank schreiben
// (je 50 Einträge pro Anfrage, beliebig viele Anfragen). Darum:
//
//   - Schlüssel nur aus Buchstaben, Ziffern und „._:-".
//   - Bekannte Schlüssel gehen immer; „spuren:<serie>" (die gemerkten
//     Spuren je Serie) bis prefsSpurenHoechstens; alles andere, was eine
//     App neu erfindet, bis prefsFremdeHoechstens.
//   - Was der Server selbst setzt („hinweis.…"), kann keine App schreiben.
//
// Ein Schlüssel, den es schon gibt, darf immer geändert werden — die
// Grenzen gelten nur für neue.

import (
	"context"
	"net/http"
	"strings"
)

const (
	prefsSpurenHoechstens = 500
	prefsFremdeHoechstens = 50
)

// prefsBekannt: was Web und Apps heute schreiben.
var prefsBekannt = map[string]bool{
	"sprache": true, "tonSprache": true, "untertitel": true, "utGroesse": true, "utHintergrund": true,
	"autoplay": true, "skipIntro": true, "shareRatings": true, "nochDaFrage": true,
	"utSuchSprachen": true, "newsSeen": true,
	// Die gewählte Sortierung je Ansicht, als „schlüssel:richtung"
	// („title:asc", „added:desc").
	"sort.movie": true, "sort.series": true, "sort.watchlist": true, "sort.favorites": true,
}

func prefSchluesselGueltig(k string) bool {
	if k == "" {
		return false
	}
	for _, c := range k {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9',
			c == '.', c == '_', c == ':', c == '-':
		default:
			return false
		}
	}
	return true
}

// prefsPruefen sagt, ob neu zu den Prefs dieses Profils passt.
func (s *Server) prefsPruefen(ctx context.Context, accountID string, neu map[string]string) error {
	if len(neu) == 0 {
		return nil
	}
	alt, err := s.st.Prefs(ctx, accountID)
	if err != nil {
		return err
	}
	spuren, fremde := 0, 0
	zaehlen := func(k string) {
		switch {
		case prefsBekannt[k]:
		case strings.HasPrefix(k, "spuren:"):
			spuren++
		default:
			fremde++
		}
	}
	for k := range alt {
		zaehlen(k)
	}
	for k := range neu {
		if !prefSchluesselGueltig(k) {
			return &apiFehler{status: http.StatusBadRequest, text: "Unbekannte Einstellung."}
		}
		if strings.HasPrefix(k, "hinweis.") || strings.HasPrefix(k, "pflicht.") {
			return &apiFehler{status: http.StatusBadRequest, text: "Unbekannte Einstellung."}
		}
		if _, da := alt[k]; !da {
			zaehlen(k)
		}
	}
	if spuren > prefsSpurenHoechstens || fremde > prefsFremdeHoechstens {
		return &apiFehler{status: http.StatusBadRequest, text: "Zu viele Einstellungen in diesem Profil."}
	}
	return nil
}
