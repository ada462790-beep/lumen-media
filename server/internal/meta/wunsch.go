// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package meta

// Vorschläge beim Wünschen: Wer „Dune" tippt, sieht darunter, was TMDb
// dazu kennt — mit Poster, Jahr und ob Film oder Serie —, und wählt eins
// aus. Die IMDb hat keine offene Schnittstelle; TMDb nennt zu jedem
// Eintrag die IMDb-Kennung.
//
// Hier steht nur das Gespräch mit TMDb. Zwischenspeicher, Bremse und
// Kinderschutz liegen in der API (wuensche_vorschlaege.go).

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// Vorschlag ist ein Treffer der Suche — klein gehalten: Was eine Zeile
// unter dem Eingabefeld braucht.
type Vorschlag struct {
	Art           string // movie | series
	TMDbID        string
	Titel         string
	Originaltitel string
	Jahr          int
	PosterPfad    string // „/abc.jpg" — ohne Grösse und Adresse
	Beliebtheit   float64

	// Erst nach Kurzdetails bekannt.
	IMDbID    string
	AgeRating string
	AgeYears  *int
}

// ErrOhneTMDb: Es ist kein TMDb-Schlüssel eingerichtet.
var ErrOhneTMDb = errors.New("ohne tmdb-schlüssel")

// MultiSuche fragt TMDb nach Filmen und Serien zugleich (/search/multi)
// und lässt Personen weg. Ohne Wartezeit bei einer Bremse: Wer tippt,
// will jetzt eine Antwort oder keine.
func (t *TMDb) MultiSuche(ctx context.Context, suche, sprache string) ([]Vorschlag, error) {
	if sprache == "" {
		sprache = t.Language
	}
	q := url.Values{"query": {suche}, "language": {sprache}, "include_adult": {"false"}, "page": {"1"}}
	var res struct {
		Results []struct {
			MediaType     string  `json:"media_type"`
			ID            int     `json:"id"`
			Title         string  `json:"title"`
			Name          string  `json:"name"`
			OriginalTitle string  `json:"original_title"`
			OriginalName  string  `json:"original_name"`
			ReleaseDate   string  `json:"release_date"`
			FirstAirDate  string  `json:"first_air_date"`
			PosterPath    string  `json:"poster_path"`
			Popularity    float64 `json:"popularity"`
			Adult         bool    `json:"adult"`
		} `json:"results"`
	}
	if err := t.getEinmal(ctx, "/search/multi", q, &res); err != nil {
		return nil, err
	}
	out := make([]Vorschlag, 0, len(res.Results))
	for _, m := range res.Results {
		if m.Adult || m.ID <= 0 {
			continue
		}
		v := Vorschlag{TMDbID: fmt.Sprint(m.ID), Beliebtheit: m.Popularity, PosterPfad: posterPfad(m.PosterPath)}
		switch m.MediaType {
		case "movie":
			v.Art, v.Titel, v.Originaltitel, v.Jahr = "movie", m.Title, m.OriginalTitle, yearOf(m.ReleaseDate)
		case "tv":
			v.Art, v.Titel, v.Originaltitel, v.Jahr = "series", m.Name, m.OriginalName, yearOf(m.FirstAirDate)
		default:
			continue // Personen, Sammlungen
		}
		if strings.TrimSpace(v.Titel) == "" {
			continue
		}
		out = append(out, v)
	}
	return out, nil
}

// Kurzdetails holt zu einem Eintrag die IMDb-Kennung und die
// Altersfreigabe — ohne Besetzung, das hält die Antwort klein. Die
// Freigabe wie beim Abgleich: eigene Region, sonst Deutschland, sonst USA.
func (t *TMDb) Kurzdetails(ctx context.Context, art, id, sprache string) (*Vorschlag, error) {
	if err := tmdbKennung(id); err != nil {
		return nil, err
	}
	if sprache == "" {
		sprache = t.Language
	}
	regionen := []string{t.Region, "DE", "US"}
	switch art {
	case "movie":
		var d struct {
			tmdbMovie
			IMDbID   string `json:"imdb_id"`
			Releases struct {
				Countries []struct {
					ISO3166 string `json:"iso_3166_1"`
					Cert    string `json:"certification"`
				} `json:"countries"`
			} `json:"releases"`
		}
		if err := t.getEinmal(ctx, "/movie/"+id,
			url.Values{"language": {sprache}, "append_to_response": {"releases"}}, &d); err != nil {
			return nil, err
		}
		v := &Vorschlag{Art: "movie", TMDbID: fmt.Sprint(d.ID), Titel: d.Title, Originaltitel: d.OriginalTitle,
			Jahr: yearOf(d.ReleaseDate), PosterPfad: posterPfad(d.PosterPath), Beliebtheit: d.Popularity,
			IMDbID: imdbKennung(d.IMDbID)}
	film:
		for _, want := range regionen {
			for _, r := range d.Releases.Countries {
				if r.ISO3166 == want && r.Cert != "" {
					if jahre := AgeYears(r.Cert); jahre != nil {
						v.AgeRating, v.AgeYears = r.Cert, jahre
						break film
					}
				}
			}
		}
		return v, nil
	case "series":
		var d struct {
			tmdbSeries
			ExternalIDs struct {
				IMDbID string `json:"imdb_id"`
			} `json:"external_ids"`
			Freigaben struct {
				Results []struct {
					ISO3166 string `json:"iso_3166_1"`
					Rating  string `json:"rating"`
				} `json:"results"`
			} `json:"content_ratings"`
		}
		if err := t.getEinmal(ctx, "/tv/"+id,
			url.Values{"language": {sprache}, "append_to_response": {"external_ids,content_ratings"}}, &d); err != nil {
			return nil, err
		}
		v := &Vorschlag{Art: "series", TMDbID: fmt.Sprint(d.ID), Titel: d.Name, Originaltitel: d.OriginalName,
			Jahr: yearOf(d.FirstAirDate), PosterPfad: posterPfad(d.PosterPath), Beliebtheit: d.Popularity,
			IMDbID: imdbKennung(d.ExternalIDs.IMDbID)}
	serie:
		for _, want := range regionen {
			for _, f := range d.Freigaben.Results {
				if f.ISO3166 == want && f.Rating != "" {
					if jahre := AgeYears(f.Rating); jahre != nil {
						v.AgeRating, v.AgeYears = f.Rating, jahre
						break serie
					}
				}
			}
		}
		return v, nil
	}
	return nil, fmt.Errorf("unbekannte art")
}

// PosterDatei: so sieht der Name eines TMDb-Posters aus („abc123.jpg").
// Alles andere wird nicht geholt — so kann über die Adresse niemand den
// Server etwas anderes laden lassen als ein Poster von TMDb.
var PosterDatei = regexp.MustCompile(`^[A-Za-z0-9_-]{4,64}\.(jpg|png)$`)

var imdbMuster = regexp.MustCompile(`^tt[0-9]{5,10}$`)

// IMDbGueltig: eine IMDb-Kennung wie tt0133093.
func IMDbGueltig(s string) bool { return imdbMuster.MatchString(s) }

func imdbKennung(s string) string {
	if IMDbGueltig(s) {
		return s
	}
	return ""
}

func posterPfad(p string) string {
	if PosterDatei.MatchString(strings.TrimPrefix(p, "/")) {
		return p
	}
	return ""
}

// ---------------------------------------------------------------- Resolver

// WunschSuche: MultiSuche, wenn TMDb eingerichtet ist.
func (r *Resolver) WunschSuche(ctx context.Context, suche, sprache string) ([]Vorschlag, error) {
	if r == nil || r.tmdb == nil {
		return nil, ErrOhneTMDb
	}
	return r.tmdb.MultiSuche(ctx, suche, sprache)
}

// WunschDetails: Kurzdetails, wenn TMDb eingerichtet ist.
func (r *Resolver) WunschDetails(ctx context.Context, art, id, sprache string) (*Vorschlag, error) {
	if r == nil || r.tmdb == nil {
		return nil, ErrOhneTMDb
	}
	return r.tmdb.Kurzdetails(ctx, art, id, sprache)
}

// MetaSprache: die Sprache, in der die Mediathek ihre Titel holt.
func (r *Resolver) MetaSprache() string {
	if r == nil || r.tmdb == nil {
		return ""
	}
	return r.tmdb.Language
}

// maxWunschPoster: ein kleines Poster (w92/w185) hat ein paar Dutzend KB.
const maxWunschPoster = 2 << 20

// WunschPoster holt ein kleines Poster von der Bilderadresse von TMDb.
// groesse ist w92 oder w185, datei passt auf PosterDatei — die Adresse
// setzt sich nur aus festen Teilen zusammen.
func (r *Resolver) WunschPoster(ctx context.Context, groesse, datei string) ([]byte, string, error) {
	if r == nil || r.tmdb == nil {
		return nil, "", ErrOhneTMDb
	}
	if (groesse != "w92" && groesse != "w185") || !PosterDatei.MatchString(datei) {
		return nil, "", fmt.Errorf("kein poster")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.tmdb.ImageURL+"/"+groesse+"/"+datei, nil)
	if err != nil {
		return nil, "", err
	}
	resp, err := r.http.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("antwortete mit %s", resp.Status)
	}
	art := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(art, "image/jpeg") && !strings.HasPrefix(art, "image/png") {
		return nil, "", fmt.Errorf("kein bild")
	}
	daten, err := io.ReadAll(io.LimitReader(resp.Body, maxWunschPoster+1))
	if err != nil {
		return nil, "", err
	}
	if len(daten) > maxWunschPoster {
		return nil, "", fmt.Errorf("poster zu gross")
	}
	return daten, art, nil
}
