// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

// Warum ein Browser-Strom endete.
//
// Die Web-App spielt browser.mp4 über ein schlichtes <video>. Das sieht
// weder den Status einer Antwort noch die Kopfzeilen: Ein 410, ein 429 oder
// ein Strom, den der Besitzer beendet hat, sind für den Browser alle nur
// „error" oder ein zu frühes „ended". Ohne Nachfrage hielte die App einen
// abgelaufenen Strom (lange Pause) für das Ende des Films und meldete ihn
// als gesehen.
//
// Darum gibt die App jedem Player eine Zufallskennung mit (?lauf=). Lumen
// merkt sich dazu die jüngste Wiedergabe oder den Fehler, an dem sie
// scheiterte — und unter /browser/grund fragt die App danach. Gemerkt
// werden nur Konto, Zufallskennung, Sitzungskennung und der Fehlertext:
// kein Titel, keine Datei. Nach einer Stunde ist der Eintrag weg; ins
// Protokoll kommt nichts davon.

import (
	"net/http"
	"sync"
	"time"

	"lumen/internal/stream"
)

type laufEintrag struct {
	sid    string // die jüngste Wiedergabe dieses Players
	fehler string // woran die letzte Anfrage scheiterte ("" = an nichts)
	code   string // fester Code zum Fehler, falls es einen gibt (bildschirmzeit)
	wann   time.Time
}

type laufBuch struct {
	mu sync.Mutex
	m  map[string]laufEintrag
}

// laufVon liest die Zufallskennung des Players. Was nicht wie eine
// Kennung aussieht, wird übergangen — dann gibt es eben keine Auskunft.
func laufVon(r *http.Request) string {
	l := r.URL.Query().Get("lauf")
	if !gueltigeKennung(l) {
		return ""
	}
	return l
}

func (b *laufBuch) setzen(kontoID, lauf string, e laufEintrag) {
	if lauf == "" {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.m == nil {
		b.m = map[string]laufEintrag{}
	}
	e.wann = time.Now()
	b.m[kontoID+"\x00"+lauf] = e
	// Aufräumen, sobald es mehr werden: Nach einer Stunde fragt kein
	// Player mehr nach.
	if len(b.m) > 256 {
		for k, v := range b.m {
			if time.Since(v.wann) > time.Hour {
				delete(b.m, k)
			}
		}
	}
}

func (b *laufBuch) holen(kontoID, lauf string) (laufEintrag, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	e, ok := b.m[kontoID+"\x00"+lauf]
	if ok && time.Since(e.wann) > time.Hour {
		return laufEintrag{}, false
	}
	return e, ok
}

// browserGrund: GET /stream/{fileId}/browser/grund?lauf=<kennung>
//
// Antwortet mit einem festen Code:
//   - laeuft: die Wiedergabe läuft noch (der Browser hat sich verschluckt)
//   - wiedergabe_abgelaufen / wiedergabe_beendet / wiedergabe_ersetzt:
//     wie bei HLS (wiedergabe.go)
//   - fehler: die letzte Anfrage scheiterte; der Text steht in „error"
//   - unbekannt: nichts gemerkt (Neustart des Servers, zu lange her)
func (s *Server) browserGrund(w http.ResponseWriter, r *http.Request) {
	if _, _, err := s.fileForRequest(r); err != nil {
		failErr(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	lauf := laufVon(r)
	e, da := s.laeufe.holen(accountOf(r).ID, lauf)
	switch {
	case !da || lauf == "":
		ok(w, map[string]string{"code": "unbekannt"})
	case e.fehler != "" && e.code != "":
		// Schon übersetzt (bildschirmzeitText).
		ok(w, map[string]string{"code": e.code, "error": e.fehler})
	case e.fehler != "":
		ok(w, map[string]string{"code": "fehler", "error": tr(w, e.fehler)})
	case e.sid != "" && s.live.Laeuft(e.sid):
		ok(w, map[string]string{"code": "laeuft"})
	default:
		grund := s.live.Grund(e.sid)
		if grund == stream.GrundBildschirmzeit {
			// Wie bei HLS: die Meldung, ausser es gibt inzwischen wieder Zeit.
			if l, gilt := s.bildschirmzeitLage(r.Context(), accountOf(r)); gilt && !l.urteil.Erlaubt {
				ok(w, map[string]string{"code": grund, "error": bildschirmzeitText(w, l)})
				return
			}
			grund = stream.GrundAbgelaufen
		}
		if grund == "" {
			grund = stream.GrundAbgelaufen
		}
		ok(w, map[string]string{"code": grund})
	}
}
