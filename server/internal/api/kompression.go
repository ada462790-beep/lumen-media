// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

// Gepackte Antworten (gzip) für JSON und Text.
//
// Unterwegs zählt jedes Byte: Eine Rasterseite sind 26 KB JSON, gepackt
// 3 KB. Gepackt wird nur, was sich lohnt — JSON, Untertitel, Text — und
// nur, wenn der Browser es versteht. Bilder und Filme sind schon gepackt;
// sie (und alles mit Range, also jeder Strom) bleiben, wie sie sind.
//
// Die Wege für Filme, Ströme, HLS-Segmente, Downloads und Bilder laufen
// gar nicht erst durch den Packer (ohnePacken): Sonst bekäme
// http.ServeContent einen Schreiber ohne ReadFrom, und der Kern könnte die
// Datei nicht mehr mit sendfile direkt ins Netz geben — jedes Byte ginge
// durch den Speicher. Wo der Packer doch dazwischen steht und nicht packt,
// reicht er ReadFrom durch.

import (
	"bufio"
	"compress/gzip"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
)

// kompressionAb: kleinere Antworten bleiben ungepackt (der Kopf von gzip
// und die Rechenzeit lohnen sich nicht).
const kompressionAb = 1024

var gzipPool = sync.Pool{New: func() any {
	zw, _ := gzip.NewWriterLevel(nil, 5)
	return zw
}}

// packbarerTyp: Inhalte, die sich packen lassen.
func packbarerTyp(ct string) bool {
	ct = strings.ToLower(ct)
	return strings.HasPrefix(ct, "application/json") || strings.HasPrefix(ct, "text/") ||
		strings.HasPrefix(ct, "application/javascript") || strings.HasPrefix(ct, "image/svg+xml")
}

// nimmtGzip: versteht der Client gepackte Antworten? „gzip;q=0" heisst nein.
func nimmtGzip(r *http.Request) bool {
	for _, teil := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		name, param, _ := strings.Cut(strings.TrimSpace(teil), ";")
		if strings.EqualFold(strings.TrimSpace(name), "gzip") {
			param = strings.ReplaceAll(strings.TrimSpace(param), " ", "")
			if q, ok := strings.CutPrefix(param, "q="); ok {
				return strings.Trim(q, "0.") != ""
			}
			return true
		}
	}
	return false
}

// ohnePacken: Wege, deren Antworten nie gepackt werden — Filme, Ströme,
// HLS, Downloads, Vorschaubilder und Bilder. Untertitel und die kleinen
// JSON-Antworten unter /stream/ (Plan, Sprungpunkt, Grund) lohnen sich.
func ohnePacken(pfad string) bool {
	for _, p := range []string{"/api/v1/art/", "/api/v1/avatar/", "/api/v1/wishes/poster/"} {
		if strings.HasPrefix(pfad, p) {
			return true
		}
	}
	if strings.HasPrefix(pfad, "/api/v1/persons/") && strings.HasSuffix(pfad, "/bild") {
		return true
	}
	rest, ok := strings.CutPrefix(pfad, "/api/v1/stream/")
	if !ok {
		return false
	}
	if strings.Contains(rest, "/subtitles/") || strings.Contains(rest, "/untertitel/") {
		return false
	}
	for _, s := range []string{"/plan", "/seek", "/browser/grund"} {
		if strings.HasSuffix(rest, s) {
			return false
		}
	}
	return true
}

// kompression packt, wo es geht.
func kompression(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead || r.Header.Get("Range") != "" || ohnePacken(r.URL.Path) || !nimmtGzip(r) {
			next.ServeHTTP(w, r)
			return
		}
		gw := &gzipSchreiber{ResponseWriter: w}
		defer gw.schliessen()
		next.ServeHTTP(gw, r)
	})
}

// gzipSchreiber entscheidet beim ersten Schreiben: Ist der Typ packbar und
// steht nichts dagegen (schon gepackt, Teilinhalt, kein Inhalt), wird
// gepackt. Die ersten Bytes sammelt er, um kleine Antworten ungepackt zu
// lassen.
type gzipSchreiber struct {
	http.ResponseWriter
	status   int
	puffer   []byte
	zw       *gzip.Writer
	entschie bool // entschieden
	packen   bool
	kopfRaus bool
}

func (g *gzipSchreiber) WriteHeader(code int) {
	if g.status != 0 {
		return
	}
	g.status = code
	h := g.Header()
	// Ohne Inhalt, Teilinhalt, schon gepackt, unpassender Typ: durchreichen.
	if code < 200 || code == http.StatusNoContent || code == http.StatusNotModified ||
		code == http.StatusPartialContent || h.Get("Content-Encoding") != "" ||
		h.Get("Content-Range") != "" || !packbarerTyp(h.Get("Content-Type")) {
		g.entschie = true
		g.kopfSchreiben()
		return
	}
	h.Add("Vary", "Accept-Encoding")
}

func (g *gzipSchreiber) kopfSchreiben() {
	if g.kopfRaus {
		return
	}
	g.kopfRaus = true
	if g.status == 0 {
		g.status = http.StatusOK
	}
	g.ResponseWriter.WriteHeader(g.status)
}

func (g *gzipSchreiber) anfangen() {
	g.entschie = true
	g.packen = true
	h := g.Header()
	h.Set("Content-Encoding", "gzip")
	h.Del("Content-Length")
	// Ein ETag der ungepackten Fassung gilt für die gepackte nicht mehr.
	if et := h.Get("ETag"); et != "" && !strings.HasPrefix(et, "W/") {
		h.Set("ETag", "W/"+et)
	}
	g.kopfSchreiben()
	zw := gzipPool.Get().(*gzip.Writer)
	zw.Reset(g.ResponseWriter)
	g.zw = zw
}

func (g *gzipSchreiber) Write(b []byte) (int, error) {
	if g.status == 0 {
		if g.Header().Get("Content-Type") == "" {
			g.Header().Set("Content-Type", http.DetectContentType(b))
		}
		g.WriteHeader(http.StatusOK)
	}
	if g.entschie {
		if g.packen {
			return g.zw.Write(b)
		}
		return g.ResponseWriter.Write(b)
	}
	g.puffer = append(g.puffer, b...)
	if len(g.puffer) >= kompressionAb {
		g.anfangen()
		p := g.puffer
		g.puffer = nil
		if _, err := g.zw.Write(p); err != nil {
			return 0, err
		}
	}
	return len(b), nil
}

// ReadFrom: Packt der Schreiber nicht, geht der Inhalt direkt an den
// Schreiber darunter — mit dessen ReadFrom (sendfile). Sonst gewöhnlich
// über Write.
func (g *gzipSchreiber) ReadFrom(src io.Reader) (int64, error) {
	if g.status == 0 {
		g.WriteHeader(http.StatusOK)
	}
	if g.entschie && !g.packen {
		if rf, ok := g.ResponseWriter.(io.ReaderFrom); ok {
			return rf.ReadFrom(src)
		}
	}
	return io.Copy(nurSchreiber{g}, src)
}

// nurSchreiber versteckt ReadFrom, damit io.Copy nicht wieder hier landet.
type nurSchreiber struct{ io.Writer }

// Flush: Wer zwischendurch ausliefern will, bekommt, was bis dahin da ist.
func (g *gzipSchreiber) Flush() {
	if !g.entschie && g.status != 0 {
		if len(g.puffer) > 0 {
			g.anfangen()
			_, _ = g.zw.Write(g.puffer)
			g.puffer = nil
		} else {
			return
		}
	}
	if g.zw != nil {
		_ = g.zw.Flush()
	}
	if f, ok := g.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (g *gzipSchreiber) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := g.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, http.ErrNotSupported
}

func (g *gzipSchreiber) Unwrap() http.ResponseWriter { return g.ResponseWriter }

func (g *gzipSchreiber) schliessen() {
	if g.status == 0 {
		return // nichts geschrieben: der Server antwortet selbst
	}
	if !g.entschie {
		// Klein geblieben: ungepackt.
		g.entschie = true
		g.kopfSchreiben()
		if len(g.puffer) > 0 {
			_, _ = g.ResponseWriter.Write(g.puffer)
		}
		return
	}
	if g.zw != nil {
		_ = g.zw.Close()
		g.zw.Reset(nil)
		gzipPool.Put(g.zw)
		g.zw = nil
	}
}
