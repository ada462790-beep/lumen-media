// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

// Der Medienschlüssel: was in Adressen steht, statt des Geräte-Tokens.
//
// Bilder, Videos und Untertitel lädt ein Player über eine Adresse — dort
// lässt sich kein Kopf „Authorization" mitschicken. Früher stand darum das
// Token des Geräts in der Adresse (?token=…). Das ist gefährlich: Viele
// NAS-Besitzer schalten einen Reverse-Proxy davor, und der schreibt jede
// Adresse in sein Protokoll. Mit dem Token daraus und der Datenbank liesse
// sich ablesen, welches Gerät wann welchen Film geladen hat — ausserhalb
// von Lumen und für immer.
//
// Der Medienschlüssel ist anders:
//   - Er öffnet nur Bilder, Videos und Untertitel (GET auf /art, /avatar,
//     /stream), nicht die übrige Schnittstelle.
//   - Er lebt nur im Arbeitsspeicher und läuft ab (6 Stunden nach der
//     letzten Benutzung, spätestens nach 48 Stunden). Nach einem Neustart
//     ist keiner mehr gültig.
//   - Nirgends steht, wem er gehörte. Ein Schlüssel aus einem alten
//     Protokoll führt zu niemandem.
//
// Die Apps holen ihn mit dem Token im Kopf (GET /me/media-key) und
// erneuern ihn vor jeder Wiedergabe.

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	medienRuhe = 6 * time.Hour
	medienMax  = 48 * time.Hour
)

type medienEintrag struct {
	tokenHash [32]byte
	bis       time.Time
	spaetest  time.Time
}

type medienSchluessel struct {
	mu   sync.Mutex
	nach map[string]*medienEintrag
}

var medien = &medienSchluessel{nach: map[string]*medienEintrag{}}

func (m *medienSchluessel) ausgeben(tokenHash [32]byte) (string, time.Time) {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	k := base64.RawURLEncoding.EncodeToString(b)
	jetzt := time.Now()
	e := &medienEintrag{tokenHash: tokenHash, bis: jetzt.Add(medienRuhe), spaetest: jetzt.Add(medienMax)}
	m.mu.Lock()
	defer m.mu.Unlock()
	for key, alt := range m.nach {
		if jetzt.After(alt.bis) {
			delete(m.nach, key)
		}
	}
	m.nach[k] = e
	return k, e.bis
}

// pruefen gibt das Token-Prüfwert zum Schlüssel und verlängert ihn.
func (m *medienSchluessel) pruefen(k string) ([32]byte, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.nach[k]
	jetzt := time.Now()
	if !ok || jetzt.After(e.bis) {
		delete(m.nach, k)
		return [32]byte{}, false
	}
	if neu := jetzt.Add(medienRuhe); neu.After(e.bis) {
		e.bis = neu
		if e.bis.After(e.spaetest) {
			e.bis = e.spaetest
		}
	}
	return e.tokenHash, true
}

// medienWeg: Wege, auf denen ein Schlüssel in der Adresse gilt — nur
// lesend, nur Bilder, Videos, Untertitel.
func medienWeg(r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	p := r.URL.Path
	return strings.HasPrefix(p, "/api/v1/art/") || strings.HasPrefix(p, "/api/v1/avatar/") ||
		strings.HasPrefix(p, "/api/v1/stream/") || strings.HasPrefix(p, "/api/v1/medien/") ||
		(strings.HasPrefix(p, "/api/v1/persons/") && strings.HasSuffix(p, "/bild"))
}

// meinMedienSchluessel: GET /me/media-key — nur mit dem Token im Kopf.
func (s *Server) meinMedienSchluessel(w http.ResponseWriter, r *http.Request) {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		fail(w, http.StatusUnauthorized, "den medienschlüssel gibt es nur mit dem token im kopf")
		return
	}
	sum := sha256.Sum256([]byte(strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))))
	k, bis := medien.ausgeben(sum)
	ok(w, map[string]any{"key": k, "gueltigBis": bis, "gueltigSek": int(time.Until(bis).Seconds())})
}
