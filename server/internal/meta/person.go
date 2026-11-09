// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package meta

// Angaben zu einer Person aus der Besetzung (Issue #9): Foto, kurze
// Biografie, Geburtstag — mit Quelle.
//
// Zuerst TMDb in der eingestellten Sprache. Fehlt dort die Biografie,
// kommt sie auf Englisch von TMDb, und ergänzend (oder wenn TMDb nichts
// hat) die Zusammenfassung aus der Wikipedia in derselben Sprache. Jede
// verwendete Quelle steht mit Link dabei.
//
// Gefragt wird nur, wer eine Personenseite öffnet, und das Ergebnis
// bleibt 30 Tage im Zwischenspeicher. Die Anfrage stellt der Server, nicht
// das Gerät; TMDb und Wikipedia sehen den Server, keine Person.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"
)

// Person ist, was die Personenseite zeigt.
type Person struct {
	Name       string   `json:"name"`
	Bio        string   `json:"bio,omitempty"`
	BioSprache string   `json:"bioSprache,omitempty"`
	Geboren    string   `json:"geboren,omitempty"`   // JJJJ-MM-TT
	Gestorben  string   `json:"gestorben,omitempty"` // JJJJ-MM-TT
	Geburtsort string   `json:"geburtsort,omitempty"`
	BildDatei  string   `json:"-"` // im Bilderordner
	Quellen    []Quelle `json:"quellen,omitempty"`
}

// Quelle: woher die Angaben stammen.
type Quelle struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// PersonHolen fragt TMDb (und, wo nötig, die Wikipedia) nach einer Person.
func (r *Resolver) PersonHolen(ctx context.Context, name string) (*Person, error) {
	if r.tmdb == nil {
		return nil, fmt.Errorf("keine metadaten-quelle eingerichtet")
	}
	p := &Person{Name: name}
	var such struct {
		Results []struct {
			ID          int     `json:"id"`
			Name        string  `json:"name"`
			ProfilePath string  `json:"profile_path"`
			Popularity  float64 `json:"popularity"`
			Department  string  `json:"known_for_department"`
		} `json:"results"`
	}
	if err := r.tmdb.get(ctx, "/search/person", url.Values{"query": {name}, "language": {r.tmdb.Language}}, &such); err != nil {
		return nil, err
	}
	// Der Treffer mit genau diesem Namen; bei mehreren der bekannteste.
	id, profil, beste := 0, "", -1.0
	for _, x := range such.Results {
		if strings.EqualFold(strings.TrimSpace(x.Name), strings.TrimSpace(name)) && x.Popularity > beste {
			id, profil, beste = x.ID, x.ProfilePath, x.Popularity
		}
	}
	if id != 0 {
		var d struct {
			Biography    string `json:"biography"`
			Birthday     string `json:"birthday"`
			Deathday     string `json:"deathday"`
			PlaceOfBirth string `json:"place_of_birth"`
		}
		if err := r.tmdb.get(ctx, fmt.Sprintf("/person/%d", id), url.Values{"language": {r.tmdb.Language}}, &d); err == nil {
			p.Bio, p.Geboren, p.Gestorben, p.Geburtsort = strings.TrimSpace(d.Biography), d.Birthday, d.Deathday, d.PlaceOfBirth
			p.BioSprache = sprache(r.tmdb.Language)
		}
		if p.Bio == "" && sprache(r.tmdb.Language) != "en" {
			var en struct {
				Biography string `json:"biography"`
			}
			if err := r.tmdb.get(ctx, fmt.Sprintf("/person/%d", id), url.Values{"language": {"en-US"}}, &en); err == nil && en.Biography != "" {
				p.Bio, p.BioSprache = strings.TrimSpace(en.Biography), "en"
			}
		}
		p.Quellen = append(p.Quellen, Quelle{"TMDb", fmt.Sprintf("https://www.themoviedb.org/person/%d", id)})
		if profil != "" {
			ziel := filepath.Join(r.artDir, fmt.Sprintf("person-%d.jpg", id))
			if err := r.download(ctx, r.tmdb.image(profil, "w342"), ziel); err == nil {
				p.BildDatei = filepath.Base(ziel)
			}
		}
	}
	// Die Wikipedia: wenn TMDb keine Biografie in der eigenen Sprache hat.
	if p.Bio == "" || p.BioSprache != sprache(r.tmdb.Language) {
		// Gleichnamige gibt es viele. Kennt TMDb das Geburtsjahr, muss es
		// im Wikipedia-Text stehen — sonst ist es wohl eine andere Person.
		if text, link := r.wikipedia(ctx, name, sprache(r.tmdb.Language)); text != "" &&
			(len(p.Geboren) < 4 || strings.Contains(text, p.Geboren[:4])) {
			p.Bio, p.BioSprache = text, sprache(r.tmdb.Language)
			p.Quellen = append(p.Quellen, Quelle{"Wikipedia", link})
		}
	}
	return p, nil
}

func sprache(tag string) string {
	if i := strings.IndexAny(tag, "-_"); i > 0 {
		return strings.ToLower(tag[:i])
	}
	if tag == "" {
		return "de"
	}
	return strings.ToLower(tag)
}

// wikipedia holt die Zusammenfassung des Artikels zu einem Namen. Nur
// echte Personenartikel zählen (keine Begriffsklärung).
func (r *Resolver) wikipedia(ctx context.Context, name, lang string) (text, link string) {
	if len(lang) != 2 {
		return "", ""
	}
	u := "https://" + lang + ".wikipedia.org/api/rest_v1/page/summary/" + url.PathEscape(strings.ReplaceAll(name, " ", "_"))
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", ""
	}
	req.Header.Set("User-Agent", "Lumen (eigener Medienserver)")
	resp, err := r.http.Do(req)
	if err != nil {
		return "", ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", ""
	}
	var d struct {
		Type        string `json:"type"`
		Extract     string `json:"extract"`
		ContentURLs struct {
			Desktop struct {
				Page string `json:"page"`
			} `json:"desktop"`
		} `json:"content_urls"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(nil, resp.Body, 1<<20)).Decode(&d); err != nil {
		return "", ""
	}
	if d.Type != "standard" || d.Extract == "" {
		return "", ""
	}
	return strings.TrimSpace(d.Extract), d.ContentURLs.Desktop.Page
}
