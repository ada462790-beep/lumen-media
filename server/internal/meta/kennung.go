// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package meta

// Zuweisen über eine Kennung oder einen Link.
//
// Was jemand aus dem Browser kopiert, soll genügen:
//
//	tt0133093                                   IMDb
//	https://www.imdb.com/title/tt0133093/       IMDb (auch m.imdb.com, /de/title/…)
//	603                                         TMDb — Film oder Serie je nach Titel
//	https://www.themoviedb.org/movie/603-the-matrix
//	https://www.themoviedb.org/tv/1399-game-of-thrones?language=de
//	tmdb:603, tmdb-603, {tmdb-603}, [tmdbid-603]
//	https://thetvdb.com/?tab=series&id=121361   TheTVDB
//	https://thetvdb.com/dereferrer/series/121361
//	tvdb:121361, tvdb-121361, {tvdb-121361}
//
// IMDb- und TVDb-Kennungen löst TMDb selbst auf (/find/{kennung}) — für
// TheTVDB braucht es keinen eigenen Schlüssel. Ein TVDb-Link mit Namen
// statt Nummer („thetvdb.com/series/game-of-thrones") lässt sich so nicht
// auflösen: Dann sagt Lumen, wo die Nummer steht.

import (
	"context"
	"errors"
	"regexp"
	"strings"
)

// Eingabe ist eine erkannte Kennung.
type Eingabe struct {
	Quelle string // tmdb | imdb | tvdb
	// Art nur bei TMDb-Links: movie | tv. "" heisst: nach dem Titel.
	Art string
	ID  string
	// Slug: ein TVDb-Link ohne Nummer — ID ist dann leer.
	Slug string
}

var (
	// Fehler, die die Verwaltung in verständliche Sätze übersetzt
	// (api: kennungFehler).
	ErrTVDbOhneNummer    = errors.New("tvdb-link ohne nummer")
	ErrFilmLinkBeiSerie  = errors.New("film-link bei einer serie")
	ErrSerienLinkBeiFilm = errors.New("serien-link bei einem film")
	ErrKennungUnbekannt  = errors.New("kennung bei tmdb unbekannt")
)

var (
	reEinTMDbLink = regexp.MustCompile(`(?i)themoviedb\.org/(?:[a-z]{2}(?:-[a-z]{2})?/)?(movie|tv)/(\d{1,10})`)
	reEinIMDb     = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])(tt\d{6,10})(?:[^0-9]|$)`)
	reEinTVDbID   = regexp.MustCompile(`(?i)thetvdb\.com/.*?[?&](?:id|seriesid)=(\d{1,10})`)
	reEinTVDbPfad = regexp.MustCompile(`(?i)thetvdb\.com/(?:dereferrer/)?(?:series|movies?)/(\d{1,10})(?:[/?#]|$)`)
	reEinTVDbSlug = regexp.MustCompile(`(?i)thetvdb\.com/(?:series|movies?)/([a-z0-9][a-z0-9\-]*)`)
	reEinMarke    = regexp.MustCompile(`(?i)^[\[{(]?\s*(tmdb|tvdb|imdb)(?:id)?\s*[-=:\s]\s*(tt\d{6,10}|\d{1,10})\s*[\]})]?$`)
	reEinZahl     = regexp.MustCompile(`^\d{1,10}$`)
)

// KennungLesen erkennt in einer Eingabe eine Kennung oder einen Link.
// ok ist false, wenn es nach einem gewöhnlichen Suchtitel aussieht.
func KennungLesen(s string) (Eingabe, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Eingabe{}, false
	}
	if m := reEinTMDbLink.FindStringSubmatch(s); m != nil {
		return Eingabe{Quelle: "tmdb", Art: strings.ToLower(m[1]), ID: m[2]}, true
	}
	if strings.Contains(strings.ToLower(s), "thetvdb.com") {
		if m := reEinTVDbID.FindStringSubmatch(s); m != nil {
			return Eingabe{Quelle: "tvdb", ID: m[1]}, true
		}
		if m := reEinTVDbPfad.FindStringSubmatch(s); m != nil {
			return Eingabe{Quelle: "tvdb", ID: m[1]}, true
		}
		if m := reEinTVDbSlug.FindStringSubmatch(s); m != nil {
			return Eingabe{Quelle: "tvdb", Slug: strings.ToLower(m[1])}, true
		}
		return Eingabe{Quelle: "tvdb"}, true
	}
	if m := reEinMarke.FindStringSubmatch(s); m != nil {
		q := strings.ToLower(m[1])
		id := strings.ToLower(m[2])
		if q == "imdb" || strings.HasPrefix(id, "tt") {
			if !strings.HasPrefix(id, "tt") {
				return Eingabe{}, false
			}
			return Eingabe{Quelle: "imdb", ID: id}, true
		}
		return Eingabe{Quelle: q, ID: id}, true
	}
	if m := reEinIMDb.FindStringSubmatch(s); m != nil && (strings.Contains(strings.ToLower(s), "imdb.") || len(s) <= 12) {
		return Eingabe{Quelle: "imdb", ID: strings.ToLower(m[1])}, true
	}
	if reEinZahl.MatchString(s) && len(s) >= 2 && !istJahreszahl(s) {
		return Eingabe{Quelle: "tmdb", ID: s}, true
	}
	return Eingabe{}, false
}

// TMDbKennungZu löst eine Eingabe in die TMDb-Kennung für einen Titel
// der Art kind („movie" oder „series") auf.
func (r *Resolver) TMDbKennungZu(ctx context.Context, kind string, e Eingabe) (string, error) {
	art := "movie"
	if kind == "series" {
		art = "tv"
	}
	switch e.Quelle {
	case "tmdb":
		if e.Art != "" && e.Art != art {
			if art == "tv" {
				return "", ErrFilmLinkBeiSerie
			}
			return "", ErrSerienLinkBeiFilm
		}
		return e.ID, nil
	case "imdb", "tvdb":
		if e.ID == "" {
			return "", ErrTVDbOhneNummer
		}
		quelle := "imdb_id"
		if e.Quelle == "tvdb" {
			quelle = "tvdb_id"
		}
		filme, serien, err := r.tmdb.FindExtern(ctx, e.ID, quelle)
		if err != nil {
			return "", err
		}
		ids := filme
		if art == "tv" {
			ids = serien
		}
		if len(ids) == 0 {
			return "", ErrKennungUnbekannt
		}
		return ids[0], nil
	}
	return "", ErrKennungUnbekannt
}

// direkterTreffer holt zu einer Kennung oder einem Link genau diesen einen
// Eintrag. ok ist false, wenn es nach einem gewöhnlichen Titel aussieht.
func (r *Resolver) direkterTreffer(ctx context.Context, kind, s string) (*Candidate, bool, error) {
	e, ok := KennungLesen(s)
	if !ok {
		return nil, false, nil
	}
	id, err := r.TMDbKennungZu(ctx, kind, e)
	if err != nil {
		return nil, true, err
	}
	var c *Candidate
	if kind == "series" {
		c, err = r.tmdb.SeriesDetails(ctx, id)
	} else {
		c, err = r.tmdb.Details(ctx, id)
	}
	if err != nil && strings.Contains(err.Error(), "nicht gefunden") {
		return nil, true, ErrKennungUnbekannt
	}
	return c, true, err
}

// „1917" ist eher ein Filmtitel als eine TMDb-Kennung.
func istJahreszahl(s string) bool {
	return len(s) == 4 && (strings.HasPrefix(s, "19") || strings.HasPrefix(s, "20"))
}
