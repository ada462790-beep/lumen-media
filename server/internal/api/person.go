// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

// Die Personenseite (Issue #9): Foto, Biografie, Quellen. Siehe
// meta/person.go.

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"lumen/internal/meta"
)

var (
	personBildName = regexp.MustCompile(`^person-\d+\.jpg$`)
	personHolenMu  sync.Mutex // eine Anfrage nach der anderen an TMDb
)

type personAntwort struct {
	meta.Person
	Bild string `json:"bild,omitempty"`
}

// person: GET /api/v1/persons/{name}
func (s *Server) person(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.PathValue("name"))
	if name == "" || len(name) > 120 {
		fail(w, http.StatusNotFound, "nicht gefunden")
		return
	}
	// Nur Personen, die in einem Titel stehen, den dieses Profil sehen
	// darf. Sonst wäre Lumen ein offener Durchgang zu TMDb.
	if !s.personSichtbar(r, name) {
		fail(w, http.StatusNotFound, "nicht gefunden")
		return
	}

	var p meta.Person
	daten, geholt, da := s.st.PersonInfo(r.Context(), name)
	if da {
		_ = json.Unmarshal([]byte(daten), &p)
	}
	if (!da || time.Since(geholt) > 30*24*time.Hour) && s.meta != nil && s.meta.Ready() {
		personHolenMu.Lock()
		neu, err := s.meta.PersonHolen(r.Context(), name)
		personHolenMu.Unlock()
		if err == nil {
			p = *neu
			if raw, err := json.Marshal(struct {
				meta.Person
				BildDatei string `json:"bildDatei,omitempty"`
			}{p, p.BildDatei}); err == nil {
				_ = s.st.SetPersonInfo(r.Context(), name, string(raw))
			}
		}
	} else if da {
		var mitBild struct {
			BildDatei string `json:"bildDatei"`
		}
		_ = json.Unmarshal([]byte(daten), &mitBild)
		p.BildDatei = mitBild.BildDatei
	}
	p.Name = name
	a := personAntwort{Person: p}
	if p.BildDatei != "" {
		a.Bild = "/api/v1/persons/" + url.PathEscape(name) + "/bild"
	}
	ok(w, a)
}

// personBild: GET /api/v1/persons/{name}/bild
func (s *Server) personBild(w http.ResponseWriter, r *http.Request) {
	// Wie bei der Personenseite: nur Personen aus Titeln, die dieses
	// Profil sehen darf. Sonst liesse sich über die Bilder abfragen, wer in
	// verborgenen Titeln mitspielt.
	if !s.personSichtbar(r, r.PathValue("name")) {
		fail(w, http.StatusNotFound, "kein bild")
		return
	}
	daten, _, da := s.st.PersonInfo(r.Context(), r.PathValue("name"))
	var mitBild struct {
		BildDatei string `json:"bildDatei"`
	}
	if da {
		_ = json.Unmarshal([]byte(daten), &mitBild)
	}
	if !da || !personBildName.MatchString(mitBild.BildDatei) || s.meta == nil {
		fail(w, http.StatusNotFound, "kein bild")
		return
	}
	f, err := os.Open(filepath.Join(s.meta.ArtDir(), mitBild.BildDatei))
	if err != nil {
		fail(w, http.StatusNotFound, "kein bild")
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		fail(w, http.StatusNotFound, "kein bild")
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=86400")
	http.ServeContent(w, r, "bild.jpg", info.ModTime(), f)
}

// personSichtbar: steht die Person in mindestens einem Titel, den dieses
// Profil sehen darf?
func (s *Server) personSichtbar(r *http.Request, name string) bool {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 120 {
		return false
	}
	q, err := s.scope(r)
	if err != nil {
		return false
	}
	// Die Abfrage sucht nach Teilen des Namens („Tom" fände „Tom Hanks");
	// hier zählt nur der genaue Name in der Besetzung.
	q.Person, q.Limit = name, 50
	treffer, err := s.st.Query(r.Context(), q)
	if err != nil {
		return false
	}
	for _, it := range treffer {
		leute, err := s.st.Besetzung(r.Context(), it.ID)
		if err != nil {
			continue
		}
		for _, p := range leute {
			if strings.EqualFold(strings.TrimSpace(p.Name), name) {
				return true
			}
		}
	}
	return false
}
