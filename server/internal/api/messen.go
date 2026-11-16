// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

// Wie lange eine Anfrage dauert — und wie viel davon die Datenbank war.
//
// Jede Antwort der Schnittstelle trägt den Kopf Server-Timing (db, warten,
// gesamt): Die Entwicklerwerkzeuge des Browsers zeigen ihn bei jeder
// Anfrage an, so sieht man ohne Protokoll, ob die Zeit im Server oder im
// Netz liegt. Dauert eine Anfrage länger als langsamAb, kommt eine Zeile
// ins Protokoll (Verwaltung › Protokoll) — mit dem Muster des Wegs
// („GET /api/v1/items/{id}"), nie mit einer Kennung oder einem Titel, und
// höchstens einmal je Weg und Minute.

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"lumen/internal/store"
)

const (
	// langsamAb: ab hier kommt eine Anfrage ins Protokoll.
	langsamAb = time.Second
	// langsamAbstand: so oft höchstens je Weg.
	langsamAbstand = time.Minute
)

// langsamGemeldet: Muster → wann zuletzt gemeldet.
var langsamGemeldet struct {
	mu sync.Mutex
	m  map[string]time.Time
}

// langsamMelden sagt, ob eine langsame Anfrage auf diesem Weg jetzt
// gemeldet werden darf, und merkt es sich.
func langsamMelden(muster string, jetzt time.Time) bool {
	l := &langsamGemeldet
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.m == nil {
		l.m = map[string]time.Time{}
	}
	if t, ok := l.m[muster]; ok && jetzt.Sub(t) < langsamAbstand {
		return false
	}
	// Die Liste bleibt klein: Es gibt nur so viele Muster, wie es Wege gibt.
	l.m[muster] = jetzt
	return true
}

// messen umgibt den Wegweiser (mux) direkt: Nach dem Weiterreichen steht
// in r.Pattern das Muster, unter dem die Anfrage lief.
func (s *Server) messen(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Wiedergaben dauern Stunden und brauchen den rohen Schreiber
		// (Flush, sendfile) — nicht messen.
		if strings.HasPrefix(r.URL.Path, "/api/v1/stream/") {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		// Nach langer Ruhe wärmt der Speicher einmal vor (store/vorwaermen.go).
		s.st.Zugriff()
		ctx, m := store.MitMessung(r.Context())
		r2 := r.WithContext(ctx)
		mw := &messSchreiber{ResponseWriter: w, start: start, m: m}
		next.ServeHTTP(mw, r2)
		dauer := time.Since(start)
		if dauer < langsamAb {
			return
		}
		muster := r2.Pattern
		if muster == "" {
			muster = r.Method + " " + pfadMuster(r.URL.Path)
		}
		if !langsamMelden(muster, time.Now()) {
			return
		}
		text := fmt.Sprintf("Langsame Anfrage: %s dauerte %d ms (Datenbank %d ms, davon %d ms auf die Schreibsperre gewartet, %d Abfragen)",
			muster, dauer.Milliseconds(), m.DB().Milliseconds(), m.Warten().Milliseconds(), m.Abfragen())
		s.log.Warn("langsame anfrage", "weg", muster, "dauer", dauer.Round(time.Millisecond),
			"db", m.DB().Round(time.Millisecond), "warten", m.Warten().Round(time.Millisecond))
		// Eigener Kontext: Die Anfrage kann schon abgebrochen sein.
		s.st.Log(context.Background(), "warn", "server", text)
	})
}

// pfadMuster: Ohne Muster (404) nur die ersten drei Teile — wie im
// Protokoll von main.go, damit keine Kennung hineingerät.
func pfadMuster(p string) string {
	teile := strings.SplitN(strings.TrimPrefix(p, "/"), "/", 4)
	if len(teile) <= 3 {
		return p
	}
	return "/" + strings.Join(teile[:3], "/") + "/…"
}

// messSchreiber setzt Server-Timing, bevor der Kopf hinausgeht.
type messSchreiber struct {
	http.ResponseWriter
	start    time.Time
	m        *store.Messung
	kopfRaus bool
}

func (w *messSchreiber) kopf() {
	if w.kopfRaus {
		return
	}
	w.kopfRaus = true
	ms := func(d time.Duration) string { return strconv.FormatFloat(float64(d)/1e6, 'f', 1, 64) }
	w.Header().Set("Server-Timing", "db;dur="+ms(w.m.DB())+", warten;dur="+ms(w.m.Warten())+
		", gesamt;dur="+ms(time.Since(w.start)))
}

func (w *messSchreiber) WriteHeader(code int) {
	w.kopf()
	w.ResponseWriter.WriteHeader(code)
}

func (w *messSchreiber) Write(b []byte) (int, error) {
	w.kopf()
	return w.ResponseWriter.Write(b)
}

func (w *messSchreiber) ReadFrom(r io.Reader) (int64, error) {
	w.kopf()
	if rf, ok := w.ResponseWriter.(io.ReaderFrom); ok {
		return rf.ReadFrom(r)
	}
	return io.Copy(struct{ io.Writer }{w.ResponseWriter}, r)
}

func (w *messSchreiber) Flush() {
	w.kopf()
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *messSchreiber) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := w.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, http.ErrNotSupported
}

func (w *messSchreiber) Unwrap() http.ResponseWriter { return w.ResponseWriter }
