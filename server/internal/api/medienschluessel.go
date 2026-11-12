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
//
// Dazu kommt der Bildschlüssel, nur für Bilder (Poster, Hintergründe,
// Standbilder, Profil- und Personenbilder). Er soll über ein Neuladen der
// Seite und einen Neustart des Servers hinweg gleich bleiben — sonst hätte
// jedes Bild bei jedem Öffnen der App eine neue Adresse, der Browser
// könnte nichts aufbewahren, und unterwegs gingen bei jedem Start Hunderte
// Kilobyte für dieselben Poster drauf. Darum:
//   - Er ist abgeleitet: SHA-256 über den Prüfwert des Tokens und eine
//     Woche (bildFenster). Gespeichert wird nichts; nach einem Neustart
//     rechnet der Server ihn aus den Prüfwerten der Geräte nach.
//   - Er gilt höchstens zwei Wochen (diese und die letzte) und nur, solange
//     es das Token gibt: Gerät abgemeldet heisst Schlüssel ungültig.
//   - Er öffnet nur Bilder, keine Filme, keine Untertitel.
//   - Ohne Datenbank führt er zu niemandem: Den Prüfwert kennt nur der
//     Server.

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"net/http"
	"strconv"
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
		strings.HasPrefix(p, "/api/v1/stream/") || strings.HasPrefix(p, "/api/v1/wishes/poster/") ||
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
	bk, bbis := bilder.ausgeben(sum, time.Now())
	ok(w, map[string]any{"key": k, "gueltigBis": bis, "gueltigSek": int(time.Until(bis).Seconds()),
		"bildKey": bk, "bildGueltigSek": int(time.Until(bbis).Seconds())})
}

// ---------------------------------------------------------------- Bildschlüssel

// bildFenster: so lange bleibt der Bildschlüssel derselbe. Gültig ist er
// in seinem Fenster und im nächsten.
const bildFenster = 7 * 24 * time.Hour

// bildPraefix unterscheidet ihn vom Medienschlüssel (der hat nie einen Punkt).
const bildPraefix = "b."

func bildFensterNr(t time.Time) int64 { return t.Unix() / int64(bildFenster/time.Second) }

func bildSchluesselFuer(tokenHash [32]byte, fenster int64) string {
	h := sha256.New()
	h.Write([]byte("lumen-bildschluessel:"))
	h.Write(tokenHash[:])
	var f [8]byte
	binary.BigEndian.PutUint64(f[:], uint64(fenster))
	h.Write(f[:])
	return bildPraefix + strconv.FormatInt(fenster, 36) + "." + base64.RawURLEncoding.EncodeToString(h.Sum(nil)[:24])
}

type bildSchluessel struct {
	mu     sync.Mutex
	nach   map[string][32]byte
	gebaut time.Time // wann zuletzt aus allen Geräten nachgerechnet
}

var bilder = &bildSchluessel{nach: map[string][32]byte{}}

// ausgeben: der Schlüssel des laufenden Fensters und wann er abläuft.
func (b *bildSchluessel) ausgeben(tokenHash [32]byte, jetzt time.Time) (string, time.Time) {
	f := bildFensterNr(jetzt)
	k := bildSchluesselFuer(tokenHash, f)
	b.mu.Lock()
	b.nach[k] = tokenHash
	b.mu.Unlock()
	return k, time.Unix((f+2)*int64(bildFenster/time.Second), 0)
}

// pruefen gibt den Prüfwert des Tokens zum Schlüssel. Kennt der Server ihn
// nicht (nach einem Neustart), rechnet er die Schlüssel aller Geräte nach
// — höchstens alle zehn Sekunden, damit Raten nichts kostet.
func (b *bildSchluessel) pruefen(ctx context.Context, k string, alle func(context.Context) ([][32]byte, error)) ([32]byte, bool) {
	rest, ok := strings.CutPrefix(k, bildPraefix)
	if !ok {
		return [32]byte{}, false
	}
	fs, _, ok := strings.Cut(rest, ".")
	if !ok {
		return [32]byte{}, false
	}
	f, err := strconv.ParseInt(fs, 36, 64)
	jetzt := bildFensterNr(time.Now())
	if err != nil || (f != jetzt && f != jetzt-1) {
		return [32]byte{}, false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if h, ok := b.nach[k]; ok {
		return h, true
	}
	if time.Since(b.gebaut) < 10*time.Second || alle == nil {
		return [32]byte{}, false
	}
	b.gebaut = time.Now()
	werte, err := alle(ctx)
	if err != nil {
		return [32]byte{}, false
	}
	// Neu aufbauen: Abgemeldete Geräte fallen dabei heraus.
	b.nach = make(map[string][32]byte, 2*len(werte))
	for _, h := range werte {
		b.nach[bildSchluesselFuer(h, jetzt)] = h
		b.nach[bildSchluesselFuer(h, jetzt-1)] = h
	}
	h, ok := b.nach[k]
	return h, ok
}

// bildWeg: Wege, auf denen der Bildschlüssel gilt — nur Bilder.
func bildWeg(r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	p := r.URL.Path
	return strings.HasPrefix(p, "/api/v1/art/") || strings.HasPrefix(p, "/api/v1/avatar/") ||
		(strings.HasPrefix(p, "/api/v1/persons/") && strings.HasSuffix(p, "/bild"))
}
