// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

// Die Bremse gegen Durchprobieren — für PIN und Passwort der Profile, die
// Verwaltungssperre, Einladungen und Codes.
//
// Ein vierstelliger PIN hat 10 000 Möglichkeiten. Mit 0,4 Sekunden
// Wartezeit je Versuch wäre er in gut einer Stunde durchprobiert, mit
// vielen gleichzeitigen Anfragen in Minuten. Darum:
//
//   - Je Schlüssel (ein Profil, ein Absender) läuft immer nur ein Versuch
//     gleichzeitig. Parallele Anfragen warten nicht, sie werden abgewiesen.
//   - Nach drei Fehlversuchen wächst die Pause: 30 Sekunden, 1, 2, 4 …
//     Minuten, höchstens eine Stunde. Ein Treffer setzt alles zurück.
//
// Das reicht, damit ein vierstelliger PIN Jahre statt Minuten hält, und
// wer sich einmal vertippt, merkt davon nichts.

import (
	"net/http"
	"sync"
	"time"
)

type bremse struct {
	mu   sync.Mutex
	nach map[string]*bremsStand
}

type bremsStand struct {
	fehl    int
	bis     time.Time
	laeuft  bool
	zuletzt time.Time
}

func neueBremse() *bremse { return &bremse{nach: map[string]*bremsStand{}} }

// pause ist die Wartezeit nach n Fehlversuchen.
func pause(n int) time.Duration {
	if n < 3 {
		return 0
	}
	d := 30 * time.Second << (n - 3)
	if d > time.Hour || d <= 0 {
		return time.Hour
	}
	return d
}

// anfangen reserviert einen Versuch für alle Schlüssel. Geht es nicht,
// kommt zurück, wie lange noch zu warten ist (0 = gerade läuft schon einer).
func (b *bremse) anfangen(schluessel ...string) (bool, time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()
	jetzt := time.Now()
	b.aufraeumen(jetzt)
	for _, k := range schluessel {
		st := b.nach[k]
		if st == nil {
			continue
		}
		if st.laeuft {
			return false, time.Second
		}
		if jetzt.Before(st.bis) {
			return false, st.bis.Sub(jetzt)
		}
	}
	for _, k := range schluessel {
		st := b.nach[k]
		if st == nil {
			st = &bremsStand{}
			b.nach[k] = st
		}
		st.laeuft, st.zuletzt = true, jetzt
	}
	return true, 0
}

// beenden gibt die Versuche frei und zählt einen Fehlversuch oder setzt
// zurück.
func (b *bremse) beenden(erfolg bool, schluessel ...string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	jetzt := time.Now()
	for _, k := range schluessel {
		st := b.nach[k]
		if st == nil {
			continue
		}
		st.laeuft, st.zuletzt = false, jetzt
		if erfolg {
			delete(b.nach, k)
			continue
		}
		st.fehl++
		st.bis = jetzt.Add(pause(st.fehl))
	}
}

// aufraeumen vergisst Schlüssel, die seit einem Tag ruhig sind.
func (b *bremse) aufraeumen(jetzt time.Time) {
	if len(b.nach) < 1000 {
		return
	}
	for k, st := range b.nach {
		if !st.laeuft && jetzt.Sub(st.zuletzt) > 24*time.Hour {
			delete(b.nach, k)
		}
	}
}

// wartetext sagt, wie lange es noch dauert, in Worten.
// Schon übersetzt, in der Sprache der Antwort w.
func wartetext(w http.ResponseWriter, d time.Duration) string {
	switch {
	case d <= 2*time.Second:
		return tr(w, "gerade läuft schon ein Versuch — kurz warten")
	case d < time.Minute:
		return tr(w, "zu viele Versuche — bitte {n} Sekunden warten", "n", itoaDauer(int(d.Seconds()+0.5)))
	default:
		return tr(w, "zu viele Versuche — bitte {n} Minuten warten", "n", itoaDauer(int(d.Minutes()+0.5)))
	}
}

func itoaDauer(n int) string {
	if n < 1 {
		n = 1
	}
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}
