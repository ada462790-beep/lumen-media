// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

// Package nachbau ahmt einen Emby- oder Jellyfin-Server nach — nur für
// Tests. So prüfen die Tests die Übernahme, ohne von einem echten Server
// abzuhängen, und beide Varianten mit ihren Eigenheiten:
//
//   - Emby liest die Geräteangaben aus „X-Emby-Authorization" und das
//     Token aus „X-Emby-Token".
//   - Jellyfin (ab 10.9) besteht auf „Authorization: MediaBrowser …"
//     und läuft oft unter einem Unterpfad (/jellyfin).
package nachbau

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
)

// Titel ist ein Eintrag, wie ihn der nachgebaute Server liefert.
type Titel map[string]any

// Server ist der Nachbau.
type Server struct {
	*httptest.Server
	Art      string // "Emby" | "Jellyfin"
	Praefix  string
	Benutzer string
	Passwort string
	// Titel des angemeldeten Benutzers; Filter werden aus UserData gelesen.
	Titel []Titel
	// Serien: alle Serien der Mediathek.
	Serien []Titel

	mu          sync.Mutex
	Token       string
	Abgemeldet  bool
	Anfragen    []string
	SeitenGross int
}

// Neu startet einen Nachbau. art ist "Emby" oder "Jellyfin".
func Neu(art string) *Server {
	s := &Server{Art: art, Benutzer: "alex", Passwort: "geheim", SeitenGross: 0}
	if art == "Jellyfin" {
		s.Praefix = "/jellyfin"
	}
	s.Server = httptest.NewServer(http.HandlerFunc(s.bedienen))
	return s
}

// Adresse ist, was jemand in Lumen eintippen würde.
func (s *Server) Adresse() string { return s.URL + s.Praefix }

func (s *Server) json(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) tokenGueltig(r *http.Request) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Token == "" {
		return false
	}
	if s.Art == "Jellyfin" {
		return strings.Contains(r.Header.Get("Authorization"), `Token="`+s.Token+`"`)
	}
	return r.Header.Get("X-Emby-Token") == s.Token
}

func (s *Server) bedienen(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.Anfragen = append(s.Anfragen, r.Method+" "+r.URL.Path)
	s.mu.Unlock()
	if !strings.HasPrefix(r.URL.Path, s.Praefix+"/") {
		http.NotFound(w, r)
		return
	}
	pfad := strings.TrimPrefix(r.URL.Path, s.Praefix)
	switch {
	case pfad == "/System/Info/Public":
		s.json(w, map[string]any{"ServerName": "Wohnzimmer-NAS", "Version": "4.8.0",
			"ProductName": s.Art + " Server", "Id": "abc123"})
	case pfad == "/Users/Public":
		s.json(w, []map[string]any{{"Name": s.Benutzer, "Id": "u1"}})
	case pfad == "/Users/AuthenticateByName" && r.Method == http.MethodPost:
		kopf := r.Header.Get("X-Emby-Authorization")
		if s.Art == "Jellyfin" {
			kopf = r.Header.Get("Authorization")
		}
		if !strings.HasPrefix(kopf, "MediaBrowser ") || !strings.Contains(kopf, "DeviceId=") {
			http.Error(w, "fehlende Geräteangaben", http.StatusBadRequest)
			return
		}
		var in struct{ Username, Pw string }
		_ = json.NewDecoder(r.Body).Decode(&in)
		if !strings.EqualFold(in.Username, s.Benutzer) || in.Pw != s.Passwort {
			http.Error(w, "Invalid username or password", http.StatusUnauthorized)
			return
		}
		s.mu.Lock()
		s.Token, s.Abgemeldet = "tok-"+strconv.Itoa(len(s.Anfragen)), false
		tok := s.Token
		s.mu.Unlock()
		s.json(w, map[string]any{"AccessToken": tok, "User": map[string]any{"Id": "u1", "Name": s.Benutzer}})
	case pfad == "/Sessions/Logout" && r.Method == http.MethodPost:
		if !s.tokenGueltig(r) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		s.mu.Lock()
		s.Token, s.Abgemeldet = "", true
		s.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	case pfad == "/Users/u1/Items":
		if !s.tokenGueltig(r) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		s.titelListe(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) titelListe(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	typen := map[string]bool{}
	for _, t := range strings.Split(q.Get("IncludeItemTypes"), ",") {
		typen[t] = true
	}
	quelle := s.Titel
	if q.Get("IncludeItemTypes") == "Series" && q.Get("Filters") == "" {
		quelle = s.Serien
	}
	var passend []Titel
	for _, t := range quelle {
		if !typen[t["Type"].(string)] {
			continue
		}
		ud, _ := t["UserData"].(map[string]any)
		an := func(k string) bool { v, _ := ud[k].(bool); return v }
		switch q.Get("Filters") {
		case "IsPlayed":
			if !an("Played") {
				continue
			}
		case "IsResumable":
			if zahl(ud["PlaybackPositionTicks"]) <= 0 || an("Played") {
				continue
			}
		case "IsFavorite":
			if !an("IsFavorite") {
				continue
			}
		case "Likes":
			if !an("Likes") {
				continue
			}
		}
		passend = append(passend, t)
	}
	start, _ := strconv.Atoi(q.Get("StartIndex"))
	limit, _ := strconv.Atoi(q.Get("Limit"))
	if s.SeitenGross > 0 && (limit == 0 || limit > s.SeitenGross) {
		limit = s.SeitenGross
	}
	gesamt := len(passend)
	if start > len(passend) {
		start = len(passend)
	}
	ende := len(passend)
	if limit > 0 && start+limit < ende {
		ende = start + limit
	}
	s.json(w, map[string]any{"Items": passend[start:ende], "TotalRecordCount": gesamt})
}

// Film baut einen Film-Eintrag.
func Film(id, name string, jahr int, pfad string, groesse int64, kennungen map[string]string, ud map[string]any) Titel {
	return Titel{"Id": id, "Name": name, "Type": "Movie", "ProductionYear": jahr, "Path": pfad,
		"ProviderIds": kennungen, "MediaSources": []map[string]any{{"Size": groesse}},
		"RunTimeTicks": int64(6_000_000_000 * 9), "UserData": ud}
}

// Folge baut einen Folgen-Eintrag.
func Folge(id, serieID, serie string, staffel, folge int, pfad string, ud map[string]any) Titel {
	return Titel{"Id": id, "Name": "Folge", "Type": "Episode", "SeriesId": serieID, "SeriesName": serie,
		"ParentIndexNumber": staffel, "IndexNumber": folge, "Path": pfad,
		"RunTimeTicks": int64(6_000_000_000 * 4), "UserData": ud}
}

// Serie baut einen Serien-Eintrag.
func Serie(id, name string, jahr int, kennungen map[string]string, ud map[string]any) Titel {
	if ud == nil {
		ud = map[string]any{}
	}
	return Titel{"Id": id, "Name": name, "Type": "Series", "ProductionYear": jahr,
		"ProviderIds": kennungen, "UserData": ud}
}

func zahl(v any) int64 {
	switch x := v.(type) {
	case int:
		return int64(x)
	case int64:
		return x
	case float64:
		return int64(x)
	}
	return 0
}
