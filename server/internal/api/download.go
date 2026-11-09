// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

// Herunterladen: die Originaldatei, für unterwegs ohne Netz.
//
// Ob jemand darf, steht im Profil (allowDownload), wie viele Dateien in
// 24 Stunden auch (maxDownloads; 0 heisst ohne Grenze). Gezählt wird nur,
// was für diese Grenze nötig ist — im Arbeitsspeicher, nicht in der
// Datenbank, nicht im Protokoll: je Profil eine kurze Liste aus
// gesalzenen Prüfsummen der Datei und dem Zeitpunkt. Was jemand geladen
// hat, steht nirgends lesbar, und nach einem Neustart ist auch die Liste
// weg. Ein fortgesetzter Download derselben Datei (Bereichsanfrage nach
// einem Abbruch) zählt nicht noch einmal.

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"lumen/internal/store"
)

// downloadFenster: über diesen Zeitraum gilt maxDownloads.
const downloadFenster = 24 * time.Hour

type downloadZaehler struct {
	mu    sync.Mutex
	salz  []byte
	liste map[string]map[string]time.Time // Konto → Prüfsumme der Datei → wann
}

var downloads = func() *downloadZaehler {
	z := &downloadZaehler{salz: make([]byte, 32), liste: map[string]map[string]time.Time{}}
	_, _ = rand.Read(z.salz)
	return z
}()

func (z *downloadZaehler) summe(fileID string) string {
	h := sha256.New()
	h.Write(z.salz)
	h.Write([]byte(fileID))
	return hex.EncodeToString(h.Sum(nil)[:12])
}

// zaehlen nimmt einen Download auf, wenn die Grenze es zulässt. Gibt
// false zurück, wenn sie erreicht ist.
func (z *downloadZaehler) zaehlen(accountID, fileID string, max int) bool {
	z.mu.Lock()
	defer z.mu.Unlock()
	jetzt := time.Now()
	l := z.liste[accountID]
	if l == nil {
		l = map[string]time.Time{}
		z.liste[accountID] = l
	}
	for k, wann := range l {
		if jetzt.Sub(wann) > downloadFenster {
			delete(l, k)
		}
	}
	s := z.summe(fileID)
	if _, schon := l[s]; schon {
		return true // fortgesetzt: zählt nicht noch einmal
	}
	if max > 0 && len(l) >= max {
		return false
	}
	l[s] = jetzt
	return true
}

// downloadInfo: GET /items/{id}/download/info — darf ich, und welche
// Dateien gibt es (Fassungen, Teile)?
func (s *Server) downloadInfo(w http.ResponseWriter, r *http.Request) {
	it, err := s.visibleItem(r, r.PathValue("id"))
	if err != nil {
		failErr(w, err)
		return
	}
	lim, _ := s.st.Limits(r.Context(), accountOf(r).ID)
	type datei struct {
		FileID    string `json:"fileId"`
		Dateiname string `json:"dateiname"`
		Groesse   int64  `json:"groesse"`
		Hoehe     int    `json:"hoehe"`
	}
	dateien := []datei{}
	if lim.AllowDownload {
		files, _ := s.st.FilesFor(r.Context(), it.ID)
		for _, f := range files {
			if f.Missing {
				continue
			}
			dateien = append(dateien, datei{FileID: f.ID, Dateiname: filepath.Base(f.Path),
				Groesse: f.SizeBytes, Hoehe: f.Height})
		}
	}
	ok(w, map[string]any{"erlaubt": lim.AllowDownload, "dateien": dateien})
}

// download: GET /stream/{fileId}/download — die Originaldatei als Anhang.
// Anmeldung wie bei den Strömen (Kopfzeile oder Medienschlüssel).
func (s *Server) download(w http.ResponseWriter, r *http.Request) {
	acc := accountOf(r)
	f, _, err := s.fileForRequest(r)
	if err != nil {
		failErr(w, err)
		return
	}
	lim, _ := s.st.Limits(r.Context(), acc.ID)
	if !lim.AllowDownload {
		streamFehler(w, http.StatusForbidden, "kein_download", "für dein profil ist das herunterladen ausgeschaltet")
		return
	}
	if f.Missing {
		fail(w, http.StatusGone, "die datei ist nicht mehr da")
		return
	}
	fh, err := os.Open(f.Path)
	if err != nil {
		fail(w, http.StatusGone, "die datei ist nicht mehr da")
		return
	}
	defer fh.Close()
	info, err := fh.Stat()
	if err != nil || !info.Mode().IsRegular() {
		fail(w, http.StatusInternalServerError, "datei nicht lesbar")
		return
	}
	// Ein HEAD (wie gross ist es?) ist noch kein Download.
	if r.Method != http.MethodHead && !downloads.zaehlen(acc.ID, f.ID, lim.MaxDownloads) {
		streamFehler(w, http.StatusTooManyRequests, "zu_viele_downloads",
			fmt.Sprintf("du hast in den letzten 24 Stunden schon %d Dateien heruntergeladen — mehr erlaubt dein profil nicht", lim.MaxDownloads))
		return
	}
	w.Header().Set("Content-Type", downloadTyp(f))
	w.Header().Set("Content-Disposition", anhang(filepath.Base(f.Path)))
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, "", info.ModTime(), fh)
}

func downloadTyp(f *store.MediaFile) string {
	if t := mimeFor(f.Container); t != "application/octet-stream" {
		return t
	}
	return "application/octet-stream"
}

// anhang baut Content-Disposition mit einem Namen, den jeder Browser
// versteht: einfacher ASCII-Name für die alten, UTF-8 nach RFC 5987 für
// alle anderen (Umlaute im Titel).
func anhang(name string) string {
	einfach := strings.Map(func(r rune) rune {
		if r < 0x20 || r > 0x7e || r == '"' || r == '\\' {
			return '_'
		}
		return r
	}, name)
	return fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`, einfach,
		strings.ReplaceAll(url.QueryEscape(name), "+", "%20"))
}
