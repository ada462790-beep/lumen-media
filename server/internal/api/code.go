// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

// Anmelden mit Code — für den Fernseher, wo Tippen mit der Fernbedienung
// eine Qual ist.
//
// 1. Der Fernseher fragt einen Code an (POST /code/start) und zeigt ihn
//    gross an: „Gib 482 913 auf deinem Handy ein".
// 2. Wer schon angemeldet ist — am Handy, im Browser —, gibt ihn ein
//    (POST /code/approve). Damit gibt er sein eigenes Profil frei.
// 3. Der Fernseher fragt alle paar Sekunden nach (GET /code/{geheim})
//    und bekommt, sobald freigegeben, ein eigenes Gerät-Token.
//
// Der Fernseher kennt neben dem Code ein langes Geheimnis; nur damit
// lässt sich das Token abholen. Wer den Code am Bildschirm abliest,
// kann damit nichts anfangen, ausser ihn für das eigene Profil
// freizugeben. Codes gelten fünf Minuten und nur einmal; Fehlversuche
// beim Eingeben werden je Konto gebremst.

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"
)

const codeGueltig = 5 * time.Minute

type codeAnfrage struct {
	code      string
	geheim    string
	geraet    string
	art       string
	bis       time.Time
	accountID string // gesetzt, sobald freigegeben
	token     string
	abgeholt  bool
	// aussen: die Anfrage kam von unterwegs, nicht aus dem Heimnetz. Wer
	// den Code freigibt, bekommt das gesagt — „gib mal diesen Code ein"
	// ist ein beliebter Trick, um an ein fremdes Profil zu kommen.
	aussen bool
}

type codeStelle struct {
	mu       sync.Mutex
	nachCode map[string]*codeAnfrage
	nachGeh  map[string]*codeAnfrage
	fehl     map[string][]time.Time
	// vonAussen: wann zuletzt Codes von unterwegs angefragt wurden. Von
	// aussen kommen alle über denselben Weg; begrenzt wird darum gemeinsam.
	vonAussen []time.Time
}

// Von unterwegs: höchstens so viele offene Anfragen und so viele neue
// in zehn Minuten. Ein Fernseher braucht eine; wer Hunderte anfragt, will
// Codes erraten lassen oder die Liste füllen, bis zu Hause keiner mehr geht.
const (
	codeAussenOffen     = 10
	codeAussenFenster   = 10 * time.Minute
	codeAussenJeFenster = 30
)

// aussenFrei prüft (unter c.mu) die Grenzen für eine Anfrage von unterwegs.
func (c *codeStelle) aussenFrei(jetzt time.Time) bool {
	frisch := c.vonAussen[:0]
	for _, t := range c.vonAussen {
		if jetzt.Sub(t) < codeAussenFenster {
			frisch = append(frisch, t)
		}
	}
	c.vonAussen = frisch
	if len(frisch) >= codeAussenJeFenster {
		return false
	}
	offen := 0
	for _, a := range c.nachCode {
		if a.aussen && a.accountID == "" && jetzt.Before(a.bis) {
			offen++
		}
	}
	return offen < codeAussenOffen
}

var codes = codeStelle{nachCode: map[string]*codeAnfrage{}, nachGeh: map[string]*codeAnfrage{}, fehl: map[string][]time.Time{}}

func (c *codeStelle) aufraeumen() {
	for k, a := range c.nachCode {
		if time.Now().After(a.bis.Add(time.Minute)) {
			delete(c.nachCode, k)
			delete(c.nachGeh, a.geheim)
		}
	}
}

func sechsZiffern() string {
	n, _ := rand.Int(rand.Reader, big.NewInt(1_000_000))
	return fmt.Sprintf("%06d", n.Int64())
}

// codeStart: der Fernseher fragt einen Code an. Ohne Anmeldung.
func (s *Server) codeStart(w http.ResponseWriter, r *http.Request) {
	var in struct {
		DeviceName string `json:"deviceName"`
		DeviceKind string `json:"deviceKind"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	name := kuerzen(in.DeviceName, 60)
	if name == "" {
		name = "Fernseher"
	}
	kind := kuerzen(in.DeviceKind, 20)
	if kind == "" {
		kind = "tv"
	}
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	a := &codeAnfrage{geheim: base64.RawURLEncoding.EncodeToString(b), geraet: name, art: kind,
		bis: time.Now().Add(codeGueltig), aussen: vonAussen(r)}

	codes.mu.Lock()
	codes.aufraeumen()
	if len(codes.nachCode) > 500 {
		codes.mu.Unlock()
		fail(w, http.StatusTooManyRequests, "gerade zu viele offene Anfragen")
		return
	}
	if a.aussen {
		if !codes.aussenFrei(time.Now()) {
			codes.mu.Unlock()
			fail(w, http.StatusTooManyRequests, "gerade zu viele Anfragen von unterwegs — bitte später noch einmal")
			return
		}
		codes.vonAussen = append(codes.vonAussen, time.Now())
	}
	for {
		a.code = sechsZiffern()
		if _, belegt := codes.nachCode[a.code]; !belegt {
			break
		}
	}
	codes.nachCode[a.code] = a
	codes.nachGeh[a.geheim] = a
	codes.mu.Unlock()

	ok(w, map[string]any{"code": a.code, "geheim": a.geheim, "gueltigSek": int(codeGueltig.Seconds())})
}

// codeAbfragen: der Fernseher fragt nach. 202 = noch nicht, 200 = Token,
// 410 = abgelaufen oder schon abgeholt.
func (s *Server) codeAbfragen(w http.ResponseWriter, r *http.Request) {
	codes.mu.Lock()
	a, da := codes.nachGeh[r.PathValue("geheim")]
	if !da || time.Now().After(a.bis) && a.accountID == "" || a.abgeholt {
		codes.mu.Unlock()
		fail(w, http.StatusGone, "der code ist abgelaufen")
		return
	}
	if a.accountID == "" {
		codes.mu.Unlock()
		writeJSON(w, http.StatusAccepted, map[string]any{"status": "wartet"})
		return
	}
	a.abgeholt = true
	tok, accID := a.token, a.accountID
	delete(codes.nachCode, a.code)
	delete(codes.nachGeh, a.geheim)
	codes.mu.Unlock()

	acc, err := s.st.Account(r.Context(), accID)
	if err != nil {
		failErr(w, err)
		return
	}
	ok(w, map[string]any{"token": tok, "account": acc})
}

// codeFreigeben: wer angemeldet ist, gibt den Code für das eigene Profil frei.
func (s *Server) codeFreigeben(w http.ResponseWriter, r *http.Request) {
	acc := accountOf(r)
	var in struct {
		Code string `json:"code"`
		// Pruefen: nur nachsehen, welches Gerät das ist und ob es von
		// unterwegs fragt — noch nicht freigeben.
		Pruefen bool `json:"pruefen"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	code := strings.Map(func(c rune) rune {
		if c >= '0' && c <= '9' {
			return c
		}
		return -1
	}, in.Code)

	codes.mu.Lock()
	// Bremse: fünf falsche Codes in fünf Minuten, dann eine Minute Pause.
	var frisch []time.Time
	for _, t := range codes.fehl[acc.ID] {
		if time.Since(t) < 5*time.Minute {
			frisch = append(frisch, t)
		}
	}
	codes.fehl[acc.ID] = frisch
	if len(frisch) >= 5 && time.Since(frisch[len(frisch)-1]) < time.Minute {
		codes.mu.Unlock()
		fail(w, http.StatusTooManyRequests, "zu viele Versuche — eine Minute warten")
		return
	}
	a, da := codes.nachCode[code]
	if !da || time.Now().After(a.bis) || a.accountID != "" {
		codes.fehl[acc.ID] = append(codes.fehl[acc.ID], time.Now())
		codes.mu.Unlock()
		fail(w, http.StatusNotFound, "diesen code gibt es nicht oder er ist abgelaufen")
		return
	}
	geraet, art, aussen := a.geraet, a.art, a.aussen
	if in.Pruefen {
		codes.mu.Unlock()
		ok(w, map[string]any{"geraet": geraet, "art": art, "unterwegs": aussen})
		return
	}
	codes.mu.Unlock()

	tok, err := s.st.IssueToken(r.Context(), acc.ID, geraet, art)
	if err != nil {
		failErr(w, err)
		return
	}
	codes.mu.Lock()
	if a.accountID != "" {
		codes.mu.Unlock()
		// Das eben ausgegebene Token wird nie abgeholt — gleich wieder weg.
		if acc2, dev, err := s.st.LookupToken(r.Context(), tok); err == nil && acc2.ID == acc.ID {
			_ = s.st.RevokeToken(r.Context(), dev.TokenHash)
		}
		fail(w, http.StatusConflict, "schon freigegeben")
		return
	}
	a.accountID, a.token = acc.ID, tok
	a.bis = time.Now().Add(codeGueltig) // Zeit zum Abholen
	codes.mu.Unlock()

	s.st.Log(r.Context(), "info", "anmeldung", "ein Gerät hat sich mit Code angemeldet")
	ok(w, map[string]any{"geraet": geraet, "unterwegs": aussen})
}
