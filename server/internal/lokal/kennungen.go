// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package lokal

// Kennungen im Datei- oder Ordnernamen.
//
// Radarr, Sonarr, Jellyfin und Emby kennen eine Schreibweise, mit der man
// den richtigen Eintrag gleich in den Namen schreibt — dann braucht es
// keine Suche nach dem Titel:
//
//	The Matrix (1999) {imdb-tt0133093}      Radarr, Emby
//	The Matrix (1999) [imdbid-tt0133093]    Jellyfin, Emby
//	The Matrix (1999) {tmdb-603}            Radarr, Emby
//	The Matrix (1999) [tmdbid-603]          Jellyfin
//	Breaking Bad (2008) {tvdb-81189}        Sonarr, Emby
//	Breaking Bad (2008) [tvdbid-81189]      Jellyfin
//	Matrix.1999.tt0133093.German.1080p      eine IMDb-Kennung frei im Namen
//
// Gross- und Kleinschreibung und „-" oder „=" als Trenner sind egal.

import (
	"regexp"
	"strings"
)

// Kennungen sind die Kennungen eines Titels bei den drei Quellen.
type Kennungen struct {
	TMDb string `json:"tmdb,omitempty"`
	IMDb string `json:"imdb,omitempty"`
	TVDb string `json:"tvdb,omitempty"`
}

// Leer: keine Kennung gefunden.
func (k Kennungen) Leer() bool { return k.TMDb == "" && k.IMDb == "" && k.TVDb == "" }

var (
	// {imdb-tt0133093}, [imdbid-tt0133093], {imdbid=tt0133093}
	reNameIMDb = regexp.MustCompile(`(?i)[\[{(]\s*imdb(?:id)?\s*[-=:]\s*(tt\d{6,10})\s*[\]})]`)
	reNameTMDb = regexp.MustCompile(`(?i)[\[{(]\s*tmdb(?:id)?\s*[-=:]\s*(\d{1,10})\s*[\]})]`)
	reNameTVDb = regexp.MustCompile(`(?i)[\[{(]\s*tvdb(?:id)?\s*[-=:]\s*(\d{1,10})\s*[\]})]`)
	// „tt0133093" frei im Namen — als eigenes Wort, nicht in „Watt1234567".
	reNameTT = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])(tt\d{7,9})(?:[^a-z0-9]|$)`)
)

// KennungenAusName liest die Kennungen aus einem einzelnen Datei- oder
// Ordnernamen (ohne Pfad).
func KennungenAusName(name string) Kennungen {
	var k Kennungen
	if m := reNameIMDb.FindStringSubmatch(name); m != nil {
		k.IMDb = strings.ToLower(m[1])
	} else if m := reNameTT.FindStringSubmatch(name); m != nil {
		k.IMDb = strings.ToLower(m[1])
	}
	if m := reNameTMDb.FindStringSubmatch(name); m != nil {
		k.TMDb = strings.TrimLeft(m[1], "0")
	}
	if m := reNameTVDb.FindStringSubmatch(name); m != nil {
		k.TVDb = strings.TrimLeft(m[1], "0")
	}
	return k
}

// OhneKennungen nimmt die Kennungen aus einem Namen heraus — damit aus
// „Matrix tt0133093" nicht der Titel „Matrix tt0133093" wird.
func OhneKennungen(name string) string {
	for _, re := range []*regexp.Regexp{reNameIMDb, reNameTMDb, reNameTVDb} {
		name = re.ReplaceAllString(name, " ")
	}
	return reNameTT.ReplaceAllStringFunc(name, func(m string) string {
		// Die Trenner davor und dahinter bleiben stehen.
		i := strings.Index(strings.ToLower(m), "tt")
		j := i + 2
		for j < len(m) && m[j] >= '0' && m[j] <= '9' {
			j++
		}
		return m[:i] + " " + m[j:]
	})
}

// discOrdner sind die Ordner einer BluRay- oder DVD-Struktur. Ihre Namen
// sagen nichts über den Film; der Film heisst wie der Ordner darüber.
var discOrdner = map[string]bool{"bdmv": true, "stream": true, "video_ts": true,
	"playlist": true, "clipinf": true, "certificate": true, "backup": true, "audio_ts": true}

// KennungenFilm liest die Kennungen eines Films aus seinem Pfad: zuerst
// aus dem Dateinamen, sonst aus dem Ordner, in dem er liegt (bei einer
// BluRay der Ordner über BDMV). Weiter hinauf nicht — „Filme {tmdb-…}"
// meinte nicht jeden Film darin. dirs sind die Ordner unter der
// Bibliothek, von aussen nach innen.
func KennungenFilm(datei string, dirs []string) Kennungen {
	if k := KennungenAusName(basisName(datei)); !k.Leer() {
		return k
	}
	for i := len(dirs) - 1; i >= 0; i-- {
		if discOrdner[strings.ToLower(strings.TrimSpace(dirs[i]))] {
			continue
		}
		return KennungenAusName(dirs[i])
	}
	return Kennungen{}
}

// KennungenSerie liest die Kennungen einer Serie aus den Ordnern über
// einer Folge — nie aus dem Dateinamen der Folge: Dort stünde die
// Kennung der Folge, nicht die der Serie. Der äusserste Ordner mit einer
// Kennung gewinnt (der Serienordner liegt über den Staffelordnern).
func KennungenSerie(dirs []string) Kennungen {
	for _, d := range dirs {
		if k := KennungenAusName(d); !k.Leer() {
			return k
		}
	}
	return Kennungen{}
}

func basisName(p string) string {
	p = strings.ReplaceAll(p, `\`, "/")
	if i := strings.LastIndex(p, "/"); i >= 0 {
		p = p[i+1:]
	}
	if i := strings.LastIndex(p, "."); i > 0 && len(p)-i <= 6 {
		p = p[:i]
	}
	return p
}
