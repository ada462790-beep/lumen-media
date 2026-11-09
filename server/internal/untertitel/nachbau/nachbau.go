// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

// Package nachbau ahmt OpenSubtitles.com nach — nur für Tests (und die
// Oberflächentests, die einen laufenden Lumen dagegen prüfen).
package nachbau

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Untertitel ist ein Eintrag im Nachbau.
type Untertitel struct {
	DateiID         int
	Sprache         string
	Hash            string // trifft diese Datei
	IMDb            string // nur Ziffern
	Name            string
	Downloads       int
	Maschinell      bool
	Hoergeschaedigt bool
	Inhalt          string
}

// Server ist der Nachbau.
type Server struct {
	*httptest.Server
	Schluessel string
	Benutzer   string
	Passwort   string
	Rest       int
	Untertitel []Untertitel

	mu       sync.Mutex
	Anfragen []string
	// UnsortierteAnfrage: Wurde je eine Suche mit unsortierten Parametern
	// geschickt? OpenSubtitles leitet dann um.
	UnsortierteAnfrage bool
}

// Neu startet einen Nachbau mit Schlüssel „schluessel-123".
func Neu() *Server {
	s := &Server{Schluessel: "schluessel-123", Benutzer: "alex", Passwort: "os-geheim", Rest: 5}
	s.Server = httptest.NewServer(http.HandlerFunc(s.bedienen))
	return s
}

// Adresse ist die Basisadresse der Schnittstelle (…/api/v1).
func (s *Server) Adresse() string { return s.URL + "/api/v1" }

func schreiben(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) bedienen(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.Anfragen = append(s.Anfragen, r.Method+" "+r.URL.Path)
	s.mu.Unlock()
	if strings.HasPrefix(r.URL.Path, "/datei/") {
		id, _ := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/datei/"))
		for _, u := range s.Untertitel {
			if u.DateiID == id {
				_, _ = w.Write([]byte(u.Inhalt))
				return
			}
		}
		http.NotFound(w, r)
		return
	}
	if r.Header.Get("Api-Key") != s.Schluessel {
		schreiben(w, http.StatusForbidden, map[string]any{"message": "invalid api key"})
		return
	}
	if r.Header.Get("User-Agent") == "" {
		schreiben(w, http.StatusForbidden, map[string]any{"message": "user agent required"})
		return
	}
	switch strings.TrimPrefix(r.URL.Path, "/api/v1") {
	case "/login":
		var in struct{ Username, Password string }
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in.Username != s.Benutzer || in.Password != s.Passwort {
			schreiben(w, http.StatusUnauthorized, map[string]any{"message": "invalid credentials"})
			return
		}
		schreiben(w, http.StatusOK, map[string]any{"token": "tok", "base_url": "anderswo.example", "status": 200})
	case "/infos/formats":
		schreiben(w, http.StatusOK, map[string]any{"data": map[string]any{"output_formats": []string{"srt"}}})
	case "/infos/user":
		schreiben(w, http.StatusOK, map[string]any{"data": map[string]any{"allowed_downloads": 20, "remaining_downloads": s.Rest}})
	case "/subtitles":
		s.suchen(w, r)
	case "/download":
		var in struct {
			FileID int `json:"file_id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		s.mu.Lock()
		if s.Rest <= 0 {
			s.mu.Unlock()
			schreiben(w, http.StatusNotAcceptable, map[string]any{"message": "You have downloaded your allowed 5 subtitles"})
			return
		}
		s.Rest--
		rest := s.Rest
		s.mu.Unlock()
		schreiben(w, http.StatusOK, map[string]any{"link": fmt.Sprintf("%s/datei/%d", s.URL, in.FileID),
			"file_name": "x.srt", "requests": 1, "remaining": rest})
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) suchen(w http.ResponseWriter, r *http.Request) {
	roh := r.URL.RawQuery
	teile := strings.Split(roh, "&")
	namen := make([]string, len(teile))
	for i, t := range teile {
		namen[i], _, _ = strings.Cut(t, "=")
	}
	if !sort.StringsAreSorted(namen) {
		s.mu.Lock()
		s.UnsortierteAnfrage = true
		s.mu.Unlock()
	}
	q := r.URL.Query()
	sprachen := map[string]bool{}
	for _, l := range strings.Split(q.Get("languages"), ",") {
		if l != "" {
			sprachen[l] = true
		}
	}
	daten := []map[string]any{}
	for _, u := range s.Untertitel {
		if len(sprachen) > 0 && !sprachen[u.Sprache] {
			continue
		}
		passt := false
		switch {
		case q.Get("imdb_id") != "":
			passt = q.Get("imdb_id") == u.IMDb
		case q.Get("parent_imdb_id") != "":
			passt = q.Get("parent_imdb_id") == u.IMDb
		case q.Get("query") != "":
			passt = strings.Contains(strings.ToLower(u.Name), q.Get("query"))
		}
		hash := q.Get("moviehash") != "" && q.Get("moviehash") == u.Hash
		if !passt && !hash {
			continue
		}
		daten = append(daten, map[string]any{"id": strconv.Itoa(u.DateiID), "type": "subtitle",
			"attributes": map[string]any{"language": u.Sprache, "download_count": u.Downloads,
				"hearing_impaired": u.Hoergeschaedigt, "moviehash_match": hash,
				"machine_translated": u.Maschinell, "release": u.Name + ".1080p",
				"feature_details": map[string]any{"title": u.Name, "year": 2001},
				"files":           []map[string]any{{"file_id": u.DateiID, "file_name": u.Name + ".srt"}}}})
	}
	schreiben(w, http.StatusOK, map[string]any{"total_count": len(daten), "data": daten})
}
