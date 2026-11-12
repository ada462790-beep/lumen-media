// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

// Package lokal liest, was neben den Videodateien liegt: NFO-Dateien und
// Bilder, wie Kodi, Emby, Jellyfin und tinyMediaManager sie schreiben.
//
// Wer seine Sammlung über Jahre mit einem dieser Programme gepflegt hat,
// hat Titel, Beschreibungen, Kennungen und Poster schon auf der Platte —
// oft von Hand berichtigt. Das soll Lumen nicht überschreiben, sondern
// übernehmen: Eine Kennung in der NFO spart die unsichere Suche nach dem
// Titel, und ein selbst gewähltes Poster ist besser als das, was TMDb
// gerade vorne hat.
package lokal

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"

	"lumen/internal/store"
)

// NFO ist, was Lumen aus einer NFO-Datei braucht.
type NFO struct {
	Art           string `json:"art"` // movie | tvshow | episodedetails | season
	Titel         string `json:"titel,omitempty"`
	Originaltitel string `json:"originaltitel,omitempty"`
	// Sortiertitel: <sorttitle>, wie Kodi und Emby ihn schreiben.
	Sortiertitel string   `json:"sortiertitel,omitempty"`
	Jahr         int      `json:"jahr,omitempty"`
	Premiere     string   `json:"premiere,omitempty"` // JJJJ-MM-TT
	Inhalt       string   `json:"inhalt,omitempty"`
	Genres       []string `json:"genres,omitempty"`
	Freigabe     string   `json:"freigabe,omitempty"`
	Bewertung    float64  `json:"bewertung,omitempty"`
	Stimmen      int      `json:"stimmen,omitempty"`
	LaufzeitMin  int      `json:"laufzeitMin,omitempty"`
	Staffel      int      `json:"staffel,omitempty"`
	Folge        int      `json:"folge,omitempty"`
	TMDb         string   `json:"tmdb,omitempty"`
	IMDb         string   `json:"imdb,omitempty"`
	TVDb         string   `json:"tvdb,omitempty"`
}

// HatKennung: Mit einer Kennung lässt sich der Titel ohne Suche zuordnen.
func (n *NFO) HatKennung() bool { return n.TMDb != "" || n.IMDb != "" || n.TVDb != "" }

// Vollstaendig: Titel, Inhalt und (bei Filmen und Serien) ein Jahr. Dann
// genügt die NFO für sich — auch ohne TMDb.
func (n *NFO) Vollstaendig() bool {
	if n.Titel == "" || n.Inhalt == "" {
		return false
	}
	if n.Art == "episodedetails" {
		return true
	}
	return n.Jahr > 0 || n.Premiere != ""
}

// Anwenden legt die Werte der NFO über einen Titel. Leere Felder in der
// NFO lassen den Titel, wie er ist. alter rechnet eine Freigabe in Jahre
// um (meta.AgeYears) — das Paket hier soll nicht von den Quellen abhängen.
func (n *NFO) Anwenden(it *store.Item, alter func(string) *int) {
	if n.Titel != "" {
		it.Title = n.Titel
	}
	if n.Sortiertitel != "" {
		it.SortEigen = n.Sortiertitel
	}
	if n.Originaltitel != "" {
		it.OriginalTitle = n.Originaltitel
	}
	if n.Jahr > 0 && it.Kind != "episode" {
		it.Year = n.Jahr
	}
	if n.Premiere != "" {
		it.Premiered = n.Premiere
	}
	if n.Inhalt != "" {
		it.Overview = n.Inhalt
	}
	if len(n.Genres) > 0 {
		it.Genres = n.Genres
	}
	if n.Freigabe != "" {
		it.AgeRating = n.Freigabe
		if alter != nil {
			it.AgeYears = alter(n.Freigabe)
		}
	}
	if n.Bewertung > 0 && it.IMDbRating == 0 {
		it.IMDbRating, it.IMDbVotes = n.Bewertung, n.Stimmen
	}
	if n.LaufzeitMin > 0 && it.RuntimeMS == 0 {
		it.RuntimeMS = int64(n.LaufzeitMin) * 60_000
	}
	if n.IMDb != "" {
		it.IMDbID = n.IMDb
	}
}

// roh ist der Aufbau, den Kodi, Emby, Jellyfin und tinyMediaManager
// gemeinsam haben. Ältere Fassungen schreiben <id>, <imdbid>, <tmdbid>
// und ein einzelnes <rating>; neuere <uniqueid type="…"> und <ratings>.
type roh struct {
	XMLName       xml.Name
	Title         string   `xml:"title"`
	OriginalTitle string   `xml:"originaltitle"`
	SortTitle     string   `xml:"sorttitle"`
	Year          string   `xml:"year"`
	Premiered     string   `xml:"premiered"`
	Aired         string   `xml:"aired"`
	ReleaseDate   string   `xml:"releasedate"`
	Plot          string   `xml:"plot"`
	Outline       string   `xml:"outline"`
	Genres        []string `xml:"genre"`
	MPAA          string   `xml:"mpaa"`
	Certification string   `xml:"certification"`
	Rating        string   `xml:"rating"`
	Votes         string   `xml:"votes"`
	Ratings       struct {
		Rating []struct {
			Name    string `xml:"name,attr"`
			Default string `xml:"default,attr"`
			Value   string `xml:"value"`
			Votes   string `xml:"votes"`
		} `xml:"rating"`
	} `xml:"ratings"`
	Runtime  string `xml:"runtime"`
	Season   string `xml:"season"`
	Episode  string `xml:"episode"`
	ID       string `xml:"id"`
	IMDbID   string `xml:"imdbid"`
	IMDb     string `xml:"imdb_id"`
	TMDbID   string `xml:"tmdbid"`
	TVDbID   string `xml:"tvdbid"`
	UniqueID []struct {
		Type  string `xml:"type,attr"`
		Value string `xml:",chardata"`
	} `xml:"uniqueid"`
}

var (
	reIMDb     = regexp.MustCompile(`\b(tt\d{6,10})\b`)
	reTMDbLink = regexp.MustCompile(`themoviedb\.org/(?:movie|tv)/(\d+)`)
	reTVDbLink = regexp.MustCompile(`thetvdb\.com/.*?(?:series|id)[/=](\d+)`)
	reZahl     = regexp.MustCompile(`^\d{1,10}$`)
	reDatum    = regexp.MustCompile(`^((?:19|20)\d{2})-(\d{2})-(\d{2})`)
)

// LeseNFO liest eine NFO-Datei. Neben XML kennt Kodi die „Link-NFO": eine
// Datei, in der nur die Adresse bei IMDb oder TMDb steht — auch die gilt.
// Grosse Dateien sind keine NFO; mehr als ein MiB liest Lumen nicht.
func LeseNFO(pfad string) (*NFO, error) {
	f, err := os.Open(pfad)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	daten, err := io.ReadAll(io.LimitReader(f, 1<<20))
	if err != nil {
		return nil, err
	}
	return NFOAus(daten)
}

// NFOAus deutet den Inhalt einer NFO-Datei.
func NFOAus(daten []byte) (*NFO, error) {
	var r roh
	dec := xml.NewDecoder(bytes.NewReader(daten))
	dec.CharsetReader = zeichensatz
	dec.Strict = false
	// Nur das erste Element zählt: Kodi hängt hinter </movie> gern noch
	// eine Adresse an, und Doppelfolgen haben mehrere <episodedetails>.
	if err := dec.Decode(&r); err != nil {
		n := linkNFO(string(daten))
		if n == nil {
			return nil, fmt.Errorf("keine lesbare NFO: %w", err)
		}
		return n, nil
	}
	n := &NFO{
		Art:           strings.ToLower(r.XMLName.Local),
		Titel:         strings.TrimSpace(r.Title),
		Originaltitel: strings.TrimSpace(r.OriginalTitle),
		Sortiertitel:  strings.TrimSpace(r.SortTitle),
		Inhalt:        strings.TrimSpace(r.Plot),
	}
	switch n.Art {
	case "movie", "tvshow", "episodedetails", "season":
	default:
		return nil, fmt.Errorf("unbekannte NFO-Art %q", r.XMLName.Local)
	}
	if n.Inhalt == "" {
		n.Inhalt = strings.TrimSpace(r.Outline)
	}
	for _, d := range []string{r.Premiered, r.Aired, r.ReleaseDate} {
		if m := reDatum.FindStringSubmatch(strings.TrimSpace(d)); m != nil {
			n.Premiere = m[0]
			break
		}
	}
	n.Jahr, _ = strconv.Atoi(strings.TrimSpace(r.Year))
	if n.Jahr == 0 && len(n.Premiere) >= 4 {
		n.Jahr, _ = strconv.Atoi(n.Premiere[:4])
	}
	for _, g := range r.Genres {
		// Manche Programme schreiben alle Genres in ein Feld: „Drama / Krimi".
		for _, t := range strings.FieldsFunc(g, func(c rune) bool { return c == '/' || c == '|' || c == ',' }) {
			if t = strings.TrimSpace(t); t != "" {
				n.Genres = append(n.Genres, t)
			}
		}
	}
	n.Freigabe = freigabe(r.Certification)
	if n.Freigabe == "" {
		n.Freigabe = freigabe(r.MPAA)
	}
	n.Bewertung, n.Stimmen = zahl(r.Rating), ganz(r.Votes)
	for _, b := range r.Ratings.Rating {
		// Die IMDb-Note zuerst, sonst die als Vorgabe markierte.
		if strings.EqualFold(b.Name, "imdb") || (n.Bewertung == 0 && b.Default == "true") {
			n.Bewertung, n.Stimmen = zahl(b.Value), ganz(b.Votes)
		}
	}
	if n.Bewertung < 0 || n.Bewertung > 10 {
		n.Bewertung, n.Stimmen = 0, 0
	}
	n.LaufzeitMin = ganz(r.Runtime)
	n.Staffel, n.Folge = ganz(r.Season), ganz(r.Episode)

	for _, u := range r.UniqueID {
		v := strings.TrimSpace(u.Value)
		switch strings.ToLower(u.Type) {
		case "tmdb", "themoviedb":
			n.TMDb = nurZahl(v)
		case "imdb":
			n.IMDb = imdb(v)
		case "tvdb", "thetvdb":
			n.TVDb = nurZahl(v)
		}
	}
	if n.IMDb == "" {
		n.IMDb = imdb(r.IMDbID + " " + r.IMDb)
	}
	if n.TMDb == "" {
		n.TMDb = nurZahl(r.TMDbID)
	}
	if n.TVDb == "" {
		n.TVDb = nurZahl(r.TVDbID)
	}
	// <id> ist in alten Film-NFOs die IMDb-, in alten Serien-NFOs die
	// TVDb-Kennung.
	if id := strings.TrimSpace(r.ID); id != "" {
		if t := imdb(id); t != "" && n.IMDb == "" {
			n.IMDb = t
		} else if n.Art == "tvshow" && n.TVDb == "" {
			n.TVDb = nurZahl(id)
		}
	}
	// Eine angehängte Adresse hinter dem XML gilt auch.
	if !n.HatKennung() {
		if l := linkNFO(string(daten)); l != nil {
			n.TMDb, n.IMDb, n.TVDb = l.TMDb, l.IMDb, l.TVDb
		}
	}
	return n, nil
}

// linkNFO liest eine NFO, in der nur eine Adresse steht.
func linkNFO(s string) *NFO {
	n := &NFO{}
	if m := reTMDbLink.FindStringSubmatch(s); m != nil {
		n.TMDb = m[1]
	}
	if m := reIMDb.FindStringSubmatch(s); m != nil {
		n.IMDb = m[1]
	}
	if m := reTVDbLink.FindStringSubmatch(s); m != nil {
		n.TVDb = m[1]
	}
	if !n.HatKennung() {
		return nil
	}
	return n
}

// freigabe macht aus „Rated R", „de:FSK 16", „DE:16" ein Kennzeichen, das
// meta.AgeYears versteht („R", „FSK 16", „16"). Emby-Formen wie „DE-16"
// oder „de/16" bleiben stehen, wie sie sind — die liest AgeYears selbst.
func freigabe(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	// tinyMediaManager schreibt mehrere Länder: „US:R / DE:16". Das
	// deutschsprachige gilt zuerst. „de/16" ist dagegen nur ein Land.
	teile := strings.Split(s, "/")
	wahl := strings.TrimSpace(teile[0])
	if len(teile) == 2 && reLandKurz.MatchString(strings.TrimSpace(teile[0])) {
		wahl = strings.TrimSpace(teile[0]) + "-" + strings.TrimSpace(teile[1])
	} else {
	suche:
		for _, t := range teile {
			t = strings.TrimSpace(t)
			l := strings.ToLower(t)
			for _, v := range []string{"de:", "ch:", "at:", "germany:", "de-", "ch-", "at-"} {
				if strings.HasPrefix(l, v) {
					wahl = t
					break suche
				}
			}
		}
	}
	if i := strings.Index(wahl, ":"); i >= 0 && i <= 8 {
		wahl = strings.TrimSpace(wahl[i+1:])
	}
	wahl = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(wahl, "Rated "), "rated "))
	return wahl
}

var reLandKurz = regexp.MustCompile(`^[A-Za-z]{2,3}$`)

func imdb(s string) string {
	if m := reIMDb.FindStringSubmatch(s); m != nil {
		return m[1]
	}
	return ""
}

func nurZahl(s string) string {
	s = strings.TrimSpace(s)
	if reZahl.MatchString(s) && s != "0" {
		return s
	}
	return ""
}

func zahl(s string) float64 {
	f, _ := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(s), ",", "."), 64)
	return f
}

func ganz(s string) int {
	n, _ := strconv.Atoi(strings.ReplaceAll(strings.TrimSpace(s), "'", ""))
	return n
}

// zeichensatz versteht die Angaben, mit denen ältere NFO-Dateien kommen.
// Latin-1 und Windows-1252 lassen sich Byte für Byte umsetzen (die
// Sonderzeichen von 1252 zwischen 0x80 und 0x9F fallen dabei auf ihre
// Latin-1-Stelle — für Titel und Beschreibungen ist das gut genug).
func zeichensatz(name string, r io.Reader) (io.Reader, error) {
	switch strings.ToLower(name) {
	case "utf-8", "utf8":
		return r, nil
	case "iso-8859-1", "iso8859-1", "latin1", "latin-1", "windows-1252", "cp1252", "iso-8859-15":
		roh, err := io.ReadAll(r)
		if err != nil {
			return nil, err
		}
		var b strings.Builder
		for _, c := range roh {
			b.WriteRune(rune(c))
		}
		return strings.NewReader(b.String()), nil
	}
	return nil, fmt.Errorf("zeichensatz %q unbekannt", name)
}
