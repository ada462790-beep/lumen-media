// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

// Die häufigsten Antworten im Speicher: Startseite, Listen, /me.
//
// Auf einer NAS schlafen die Platten, wenn niemand schaut. Wer abends
// Lumen öffnet, wartete dann, bis die Platten angelaufen sind — 10 bis
// 30 Sekunden —, denn die Startseite liest Dutzende Seiten der
// Datenbank, und die liegen nicht mehr im Arbeitsspeicher des NAS.
//
// Darum behält Lumen die zuletzt gelieferte Antwort je Profil, Gerät und
// Adresse. Sie gilt, solange sich in der Datenbank nichts geändert hat
// (store.Stand): Ein neuer Film, ein gesehener Haken, eine geänderte
// Freigabe — alles, was geschrieben wird, rückt den Stand weiter, und die
// nächste Anfrage rechnet neu. Ist eine Antwort älter als antwortFrisch,
// kommt sie trotzdem sofort, und Lumen rechnet sie für das nächste Mal im
// Hintergrund neu (Zufallsreihen, „neu seit"). Was persönlich ist, bleibt
// beim Profil: Der Schlüssel enthält Konto und Gerät.

import (
	"bytes"
	"context"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"time"

	"lumen/internal/store"
)

const (
	// antwortFrisch: so lange gilt eine Antwort ohne Neurechnen.
	antwortFrisch = 10 * time.Minute
	// antwortHoechstens: so viel Speicher nehmen die Antworten zusammen.
	antwortHoechstens = 16 << 20
)

type antwortEintrag struct {
	status int // 0 = 200
	stand  int64
	zeit   time.Time
	kopf   http.Header
	inhalt []byte
}

type antwortSpeicher struct {
	mu      sync.Mutex
	m       map[string]*antwortEintrag
	groesse int
	neu     map[string]bool // wird gerade im Hintergrund neu gerechnet
}

func (a *antwortSpeicher) holen(k string) *antwortEintrag {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.m[k]
}

func (a *antwortSpeicher) ablegen(k string, e *antwortEintrag) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.m == nil {
		a.m = map[string]*antwortEintrag{}
	}
	if alt := a.m[k]; alt != nil {
		a.groesse -= len(alt.inhalt)
	}
	a.m[k] = e
	a.groesse += len(e.inhalt)
	// Zu viel: die ältesten weg, bis es wieder passt.
	for a.groesse > antwortHoechstens && len(a.m) > 1 {
		var aeltester string
		var t time.Time
		for kk, x := range a.m {
			if aeltester == "" || x.zeit.Before(t) {
				aeltester, t = kk, x.zeit
			}
		}
		a.groesse -= len(a.m[aeltester].inhalt)
		delete(a.m, aeltester)
	}
}

// neuRechnen meldet, ob für k schon jemand im Hintergrund rechnet.
func (a *antwortSpeicher) neuRechnen(k string, an bool) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.neu == nil {
		a.neu = map[string]bool{}
	}
	if an && a.neu[k] {
		return false
	}
	if an {
		a.neu[k] = true
	} else {
		delete(a.neu, k)
	}
	return true
}

// leeren: für Tests.
func (a *antwortSpeicher) leeren() {
	a.mu.Lock()
	a.m, a.groesse = nil, 0
	a.mu.Unlock()
}

// gemerkt umgibt einen Weg, dessen Antwort nur aus der Datenbank kommt.
// Nur hinter requireAuth.
func (s *Server) gemerkt(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || strings.Contains(r.URL.RawQuery, "random") {
			h(w, r)
			return
		}
		k := antwortSchluessel(w, r)
		if e := s.antworten.holen(k); e != nil && e.stand == store.Stand() {
			if time.Since(e.zeit) >= antwortFrisch && s.antworten.neuRechnen(k, true) {
				// Sofort liefern, für das nächste Mal neu rechnen.
				r2 := r.Clone(context.WithoutCancel(r.Context()))
				kopf := w.Header().Clone()
				s.begleitet(func(context.Context) {
					defer s.antworten.neuRechnen(k, false)
					s.antwortRechnen(h, kopf, r2, k)
				})
			}
			antwortSenden(w, e)
			return
		}
		e := s.antwortRechnen(h, w.Header().Clone(), r, k)
		antwortSenden(w, e)
	}
}

// antwortRechnen ruft den Weg und legt die Antwort ab, wenn sie gelang
// und sich während des Rechnens nichts geändert hat.
func (s *Server) antwortRechnen(h http.HandlerFunc, kopf http.Header, r *http.Request, k string) *antwortEintrag {
	vorher := store.Stand()
	rec := &antwortAufnahme{kopf: kopf, status: http.StatusOK}
	h(rec, r)
	e := &antwortEintrag{stand: vorher, zeit: time.Now(), kopf: rec.kopf, inhalt: rec.inhalt.Bytes()}
	if rec.status != http.StatusOK {
		// Fehler werden nicht aufbewahrt.
		e.status = rec.status
		return e
	}
	if store.Stand() == vorher {
		s.antworten.ablegen(k, e)
	}
	return e
}

func antwortSenden(w http.ResponseWriter, e *antwortEintrag) {
	h := w.Header()
	for k, v := range e.kopf {
		h[k] = v
	}
	if e.status != 0 && e.status != http.StatusOK {
		w.WriteHeader(e.status)
	}
	_, _ = w.Write(e.inhalt)
}

// antwortSchluessel: Konto, Gerät, Sprache der Antwort und Adresse.
func antwortSchluessel(w http.ResponseWriter, r *http.Request) string {
	var b strings.Builder
	if a := accountOf(r); a != nil {
		b.WriteString(a.ID)
	}
	b.WriteByte(0)
	if d := deviceOf(r); d != nil && len(d.TokenHash) > 8 {
		b.WriteString(hex.EncodeToString(d.TokenHash[:8]))
	}
	b.WriteByte(0)
	b.WriteString(spracheVon(w))
	b.WriteByte(0)
	b.WriteString(r.URL.Path)
	b.WriteByte('?')
	b.WriteString(r.URL.RawQuery)
	return b.String()
}

// antwortAufnahme nimmt eine Antwort auf, statt sie zu senden.
type antwortAufnahme struct {
	kopf   http.Header
	status int
	inhalt bytes.Buffer
	raus   bool
}

func (a *antwortAufnahme) Header() http.Header { return a.kopf }
func (a *antwortAufnahme) WriteHeader(code int) {
	if !a.raus {
		a.status, a.raus = code, true
	}
}
func (a *antwortAufnahme) Write(b []byte) (int, error) {
	a.raus = true
	return a.inhalt.Write(b)
}
