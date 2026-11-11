// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package meta

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// TMDb liefert Filme und Serien: Titel, Beschreibung, Poster, Freigabe.
type TMDb struct {
	Key      string
	Language string // „de-DE"
	Region   string // „CH" — entscheidet, welche Altersfreigabe gilt
	BaseURL  string // für Tests
	ImageURL string
	http     *http.Client
	limiter  *limiter
}

func NewTMDb(key, language, region string) *TMDb {
	if language == "" {
		language = "de-DE"
	}
	if region == "" {
		region = "DE"
	}
	return &TMDb{
		Key: key, Language: language, Region: region,
		BaseURL:  "https://api.themoviedb.org/3",
		ImageURL: "https://image.tmdb.org/t/p",
		http:     &http.Client{Timeout: 15 * time.Second},
		// TMDb verträgt rund 50 Anfragen pro Sekunde. Wir bleiben deutlich
		// darunter: ein Scan über ein paar hundert Titel hat es nicht eilig,
		// und eine Sperre wäre viel teurer als ein paar Sekunden mehr.
		limiter: newLimiter(20, time.Second),
	}
}

func (t *TMDb) Name() string { return "tmdb" }

func (t *TMDb) SearchMovie(ctx context.Context, title string, year int) ([]Candidate, error) {
	out, err := t.SearchMovieSprache(ctx, title, year, "")
	if err != nil {
		return nil, err
	}
	// Ohne Jahr sucht TMDb breiter; dann lohnt ein zweiter Versuch ohne
	// Jahresfilter, falls nichts kam.
	if len(out) == 0 && year > 0 {
		if ohne, err := t.SearchMovieSprache(ctx, title, 0, ""); err == nil {
			out = ohne
		}
	}
	return out, nil
}

// SearchMovieSprache sucht wie SearchMovie, aber mit den Titeln in einer
// anderen Sprache ("" = die eingestellte). Eine englische Suche bringt
// zu „Chihiros Reise ins Zauberland" den Titel „Spirited Away" — den,
// unter dem die Datei oft liegt.
func (t *TMDb) SearchMovieSprache(ctx context.Context, title string, year int, sprache string) ([]Candidate, error) {
	if sprache == "" {
		sprache = t.Language
	}
	q := url.Values{"query": {title}, "language": {sprache},
		"include_adult": {"false"}}
	if year > 0 {
		q.Set("year", fmt.Sprint(year))
	}
	var res struct {
		Results []tmdbMovie `json:"results"`
	}
	if err := t.get(ctx, "/search/movie", q, &res); err != nil {
		return nil, err
	}
	out := make([]Candidate, 0, len(res.Results))
	for _, m := range res.Results {
		out = append(out, t.movie(m))
	}
	return out, nil
}

// SeriesDetails holt zu einer Serie, was die Suche nicht mitbringt — vor
// allem die Besetzung.
func (t *TMDb) SeriesDetails(ctx context.Context, seriesID string) (*Candidate, error) {
	if err := tmdbKennung(seriesID); err != nil {
		return nil, err
	}
	var d struct {
		tmdbSeries
		Credits struct {
			Cast []tmdbCast `json:"cast"`
		} `json:"credits"`
		Genres []struct {
			Name string `json:"name"`
		} `json:"genres"`
		LastAirDate  string `json:"last_air_date"`
		Status       string `json:"status"` // Returning Series | Ended | Canceled | In Production …
		InProduction bool   `json:"in_production"`
		// Bei Serien steht die IMDb-Kennung nicht in den Einzelheiten,
		// sondern bei den fremden Kennungen. Ohne sie gab es für keine
		// Serie eine IMDb-Note.
		ExternalIDs struct {
			IMDbID string `json:"imdb_id"`
			TVDBID int    `json:"tvdb_id"`
		} `json:"external_ids"`
		// Die Altersfreigabe je Land. Ohne sie galt jede Serie als
		// ungeprüft — und ein Kinderprofil sah keine einzige, nicht einmal
		// „Peppa Wutz".
		Freigaben struct {
			Results []struct {
				ISO3166 string `json:"iso_3166_1"`
				Rating  string `json:"rating"`
			} `json:"results"`
		} `json:"content_ratings"`
	}
	err := t.get(ctx, "/tv/"+seriesID,
		url.Values{"language": {t.Language}, "append_to_response": {"credits,external_ids,content_ratings"}}, &d)
	if err != nil {
		return nil, err
	}
	c := Candidate{
		SourceID: fmt.Sprint(d.ID), Source: "tmdb",
		Title: d.Name, Original: d.OriginalName, Sprache: d.OriginalLang, Overview: d.Overview,
		Premiered: d.FirstAirDate, Year: yearOf(d.FirstAirDate), Popularity: d.Popularity,
		PosterURL:   t.image(d.PosterPath, "w500"),
		BackdropURL: t.image(d.BackdropPath, "w1280"),
		Cast:        t.cast(d.Credits.Cast),
		IMDbID:      d.ExternalIDs.IMDbID,
		Laender:     d.OriginCountry,
	}
	if d.ExternalIDs.TVDBID > 0 {
		c.TVDBID = strconv.Itoa(d.ExternalIDs.TVDBID)
	}
	for _, g := range d.Genres {
		c.Genres = append(c.Genres, g.Name)
	}
	// Wie bei Filmen: die eigene Region, sonst Deutschland, sonst USA.
	// Nur eine Einstufung, die Lumen versteht — sonst lieber keine.
freigabe:
	for _, want := range []string{t.Region, "DE", "US"} {
		for _, f := range d.Freigaben.Results {
			if f.ISO3166 == want && f.Rating != "" {
				if jahre := AgeYears(f.Rating); jahre != nil {
					c.AgeRating, c.AgeYears = f.Rating, jahre
					break freigabe
				}
			}
		}
	}
	if d.Status != "" {
		c.ZeitraumBekannt = true
		c.LaeuftNoch = d.InProduction || (d.Status != "Ended" && d.Status != "Canceled")
		c.EndYear = yearOf(d.LastAirDate)
	}
	return &c, nil
}

func (t *TMDb) SearchSeries(ctx context.Context, title string) ([]Candidate, error) {
	return t.SearchSeriesJahr(ctx, title, 0)
}

// SearchSeriesJahr sucht nur unter den Serien, die in diesem Jahr
// angefangen haben — mit 0 unter allen. „Doctor Who (2005)" im Ordnernamen
// trennt so die neue Serie von der aus 1963.
func (t *TMDb) SearchSeriesJahr(ctx context.Context, title string, year int) ([]Candidate, error) {
	return t.SearchSeriesSprache(ctx, title, year, "")
}

// SearchSeriesSprache: SearchSeriesJahr mit den Titeln in einer anderen
// Sprache ("" = die eingestellte).
func (t *TMDb) SearchSeriesSprache(ctx context.Context, title string, year int, sprache string) ([]Candidate, error) {
	if sprache == "" {
		sprache = t.Language
	}
	var res struct {
		Results []tmdbSeries `json:"results"`
	}
	q := url.Values{"query": {title}, "language": {sprache}}
	if year > 0 {
		q.Set("first_air_date_year", fmt.Sprint(year))
	}
	err := t.get(ctx, "/search/tv", q, &res)
	if err != nil {
		return nil, err
	}
	out := make([]Candidate, 0, len(res.Results))
	for _, s := range res.Results {
		out = append(out, Candidate{
			SourceID: fmt.Sprint(s.ID), Source: "tmdb",
			Title: s.Name, Original: s.OriginalName, Sprache: s.OriginalLang,
			Overview: s.Overview, Premiered: s.FirstAirDate,
			Year: yearOf(s.FirstAirDate), Popularity: s.Popularity,
			PosterURL:   t.image(s.PosterPath, "w500"),
			BackdropURL: t.image(s.BackdropPath, "w1280"),
			Laender:     s.OriginCountry,
		})
	}
	return out, nil
}

func (t *TMDb) Episode(ctx context.Context, seriesID string, season, episode int) (*Candidate, error) {
	if err := tmdbKennung(seriesID); err != nil {
		return nil, err
	}
	var e struct {
		Name      string  `json:"name"`
		Overview  string  `json:"overview"`
		AirDate   string  `json:"air_date"`
		Runtime   int     `json:"runtime"`
		StillPath string  `json:"still_path"`
		Rating    float64 `json:"vote_average"`
	}
	path := fmt.Sprintf("/tv/%s/season/%d/episode/%d", seriesID, season, episode)
	if err := t.get(ctx, path, url.Values{"language": {t.Language}}, &e); err != nil {
		return nil, err
	}
	return &Candidate{
		Source: "tmdb", Title: e.Name, Overview: e.Overview,
		Premiered: e.AirDate, Year: yearOf(e.AirDate),
		RuntimeMS: int64(e.Runtime) * 60_000,
		PosterURL: t.image(e.StillPath, "w780"),
	}, nil
}

// Details holt nach, was die Suche nicht mitliefert: Laufzeit, Genres und
// vor allem die Altersfreigabe für die eingestellte Region.
func (t *TMDb) Details(ctx context.Context, movieID string) (*Candidate, error) {
	if err := tmdbKennung(movieID); err != nil {
		return nil, err
	}
	var d struct {
		tmdbMovie
		Runtime int `json:"runtime"`
		Genres  []struct {
			Name string `json:"name"`
		} `json:"genres"`
		IMDbID string `json:"imdb_id"`
		// Die Filmreihe. TMDb liefert sie bei den Einzelheiten ohnehin
		// mit; daraus baut Lumen die Reihen, ohne eigens nachzufragen.
		Reihe *struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		} `json:"belongs_to_collection"`
		Credits struct {
			Cast []tmdbCast `json:"cast"`
		} `json:"credits"`
		Releases struct {
			Countries []struct {
				ISO3166 string `json:"iso_3166_1"`
				Cert    string `json:"certification"`
			} `json:"countries"`
		} `json:"releases"`
	}
	err := t.get(ctx, "/movie/"+movieID,
		url.Values{"language": {t.Language}, "append_to_response": {"releases,credits"}}, &d)
	if err != nil {
		return nil, err
	}
	c := t.movie(d.tmdbMovie)
	c.RuntimeMS = int64(d.Runtime) * 60_000
	c.IMDbID = d.IMDbID
	c.Cast = t.cast(d.Credits.Cast)
	if d.Reihe != nil && d.Reihe.ID > 0 && d.Reihe.Name != "" {
		c.ReiheID = strconv.Itoa(d.Reihe.ID)
		c.ReiheName = reihenName(d.Reihe.Name)
	}
	for _, g := range d.Genres {
		c.Genres = append(c.Genres, g.Name)
	}
	// Erst die eigene Region, sonst Deutschland, sonst USA — in dieser
	// Reihenfolge, damit eine Schweizer Mediathek keine US-Einstufung bekommt.
	for _, want := range []string{t.Region, "DE", "US"} {
		for _, r := range d.Releases.Countries {
			if r.ISO3166 == want && r.Cert != "" {
				c.AgeRating = r.Cert
				c.AgeYears = AgeYears(r.Cert)
				return &c, nil
			}
		}
	}
	return &c, nil
}

// ReiheDetails ist, was TMDb über eine Filmreihe weiss: ihr eigenes
// Poster und ihr eigener Hintergrund, nicht die eines Films daraus.
type ReiheDetails struct {
	Name        string
	Overview    string
	PosterURL   string
	BackdropURL string
}

func (t *TMDb) Reihe(ctx context.Context, collectionID string) (*ReiheDetails, error) {
	if err := tmdbKennung(collectionID); err != nil {
		return nil, err
	}
	var d struct {
		Name         string `json:"name"`
		Overview     string `json:"overview"`
		PosterPath   string `json:"poster_path"`
		BackdropPath string `json:"backdrop_path"`
	}
	if err := t.get(ctx, "/collection/"+collectionID, url.Values{"language": {t.Language}}, &d); err != nil {
		return nil, err
	}
	return &ReiheDetails{
		Name: reihenName(d.Name), Overview: d.Overview,
		PosterURL:   t.image(d.PosterPath, "w500"),
		BackdropURL: t.image(d.BackdropPath, "w1280"),
	}, nil
}

type tmdbCast struct {
	Name        string `json:"name"`
	Character   string `json:"character"`
	ProfilePath string `json:"profile_path"`
	Order       int    `json:"order"`
}

// cast nimmt die ersten zwölf — die Hauptrollen. Mehr passt in keine
// Zeile, und hundert Bilder je Film wären hundert Downloads.
func (t *TMDb) cast(roh []tmdbCast) []CastEintrag {
	sort.SliceStable(roh, func(i, j int) bool { return roh[i].Order < roh[j].Order })
	var out []CastEintrag
	for _, c := range roh {
		if c.Name == "" {
			continue
		}
		out = append(out, CastEintrag{Name: c.Name, Rolle: c.Character, BildURL: t.image(c.ProfilePath, "w185")})
		if len(out) == 12 {
			break
		}
	}
	return out
}

type tmdbMovie struct {
	ID            int     `json:"id"`
	Title         string  `json:"title"`
	OriginalTitle string  `json:"original_title"`
	OriginalLang  string  `json:"original_language"`
	Overview      string  `json:"overview"`
	ReleaseDate   string  `json:"release_date"`
	PosterPath    string  `json:"poster_path"`
	BackdropPath  string  `json:"backdrop_path"`
	Popularity    float64 `json:"popularity"`
}

type tmdbSeries struct {
	ID           int     `json:"id"`
	Name         string  `json:"name"`
	OriginalName string  `json:"original_name"`
	OriginalLang string  `json:"original_language"`
	Overview     string  `json:"overview"`
	FirstAirDate string  `json:"first_air_date"`
	PosterPath   string  `json:"poster_path"`
	BackdropPath string  `json:"backdrop_path"`
	Popularity   float64 `json:"popularity"`
	// Herkunftsländer — für gleichnamige Serien aus verschiedenen Ländern.
	OriginCountry []string `json:"origin_country"`
}

func (t *TMDb) movie(m tmdbMovie) Candidate {
	return Candidate{
		SourceID: fmt.Sprint(m.ID), Source: "tmdb",
		Title: m.Title, Original: m.OriginalTitle, Sprache: m.OriginalLang, Overview: m.Overview,
		Premiered: m.ReleaseDate, Year: yearOf(m.ReleaseDate),
		Popularity:  m.Popularity,
		PosterURL:   t.image(m.PosterPath, "w500"),
		BackdropURL: t.image(m.BackdropPath, "w1280"),
	}
}

func (t *TMDb) image(path, size string) string {
	if path == "" {
		return ""
	}
	return t.ImageURL + "/" + size + path
}

func (t *TMDb) get(ctx context.Context, path string, q url.Values, out any) error {
	err := t.getEinmal(ctx, path, q, out)
	var b *gebremst
	if errors.As(err, &b) {
		// TMDb bremst: so lange warten, wie TMDb sagt (höchstens zehn
		// Sekunden), dann ein zweites Mal. Klappt auch das nicht, bleibt
		// der Titel „ausstehend" und kommt beim nächsten Durchgang dran —
		// er wird nicht als „nicht erkannt" abgelegt.
		warte := min(max(b.warte, time.Second), 10*time.Second)
		select {
		case <-ctx.Done():
			return err
		case <-time.After(warte):
		}
		err = t.getEinmal(ctx, path, q, out)
	}
	return err
}

// ErrTMDbSchluessel: TMDb antwortet mit 401 — der Schlüssel stimmt nicht
// (meist die kurze API-Kennung statt des langen Read Access Token).
var ErrTMDbSchluessel = errors.New("der TMDb-Schlüssel wird nicht angenommen")

// gebremst: TMDb hat mit 429 geantwortet.
type gebremst struct{ warte time.Duration }

func (g *gebremst) Error() string { return "TMDb bremst gerade — später noch einmal" }

func (t *TMDb) getEinmal(ctx context.Context, path string, q url.Values, out any) error {
	if t.Key == "" {
		return fmt.Errorf("für TMDb fehlt der Schlüssel")
	}
	t.limiter.wait(ctx)

	u := t.BaseURL + path + "?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	// TMDb nimmt den Schlüssel als Kopfzeile — so steht er nicht in
	// Protokollen oder im Verlauf eines Zwischenspeichers.
	req.Header.Set("Authorization", "Bearer "+t.Key)
	req.Header.Set("Accept", "application/json")

	resp, err := t.http.Do(req)
	if err != nil {
		return fmt.Errorf("TMDb nicht erreichbar: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized:
		return ErrTMDbSchluessel
	case http.StatusNotFound:
		return fmt.Errorf("bei TMDb nicht gefunden")
	case http.StatusTooManyRequests:
		sek, _ := strconv.Atoi(strings.TrimSpace(resp.Header.Get("Retry-After")))
		return &gebremst{warte: time.Duration(sek) * time.Second}
	default:
		return fmt.Errorf("TMDb antwortete mit %s", resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func yearOf(date string) int {
	if len(date) < 4 {
		return 0
	}
	y := 0
	for _, r := range date[:4] {
		if r < '0' || r > '9' {
			return 0
		}
		y = y*10 + int(r-'0')
	}
	return y
}

// ---------------------------------------------------------------- Bremse

// limiter hält die Anfragerate ein. Lieber ein paar Sekunden länger scannen
// als vom Dienst ausgesperrt werden.
type limiter struct {
	mu       sync.Mutex
	interval time.Duration
	next     time.Time
}

func newLimiter(perWindow int, window time.Duration) *limiter {
	return &limiter{interval: window / time.Duration(max(perWindow, 1))}
}

func (l *limiter) wait(ctx context.Context) {
	l.mu.Lock()
	now := time.Now()
	if l.next.Before(now) {
		l.next = now
	}
	wait := l.next.Sub(now)
	l.next = l.next.Add(l.interval)
	l.mu.Unlock()

	if wait <= 0 {
		return
	}
	t := time.NewTimer(wait)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

// ---------------------------------------------------------------- OMDb

// OMDb ist der Weg an die IMDb-Wertung. IMDb selbst hat keine offene
// Schnittstelle; OMDb gibt die Note und die Zahl der Stimmen heraus.
type OMDb struct {
	Key     string
	BaseURL string
	http    *http.Client
	limiter *limiter
}

func NewOMDb(key string) *OMDb {
	return &OMDb{Key: key, BaseURL: "https://www.omdbapi.com/",
		http:    &http.Client{Timeout: 15 * time.Second},
		limiter: newLimiter(8, time.Second)}
}

func (o *OMDb) Name() string { return "omdb" }

// Rating holt Note und Stimmen zu einer IMDb-Kennung.
func (o *OMDb) Rating(ctx context.Context, imdbID string) (rating float64, votes int, err error) {
	w, err := o.Wertungen(ctx, imdbID)
	return w.IMDb, w.Stimmen, err
}

// Wertung ist, was OMDb zu einem Titel weiss: die IMDb-Note und, wenn es
// eine gibt, den Anteil guter Kritiken bei Rotten Tomatoes.
type Wertung struct {
	IMDb    float64
	Stimmen int
	// Tomaten: 0–100 Prozent; -1, wenn Rotten Tomatoes nichts hat.
	Tomaten int
}

// Wertungen holt alle Wertungen zu einer IMDb-Kennung mit einer Abfrage.
func (o *OMDb) Wertungen(ctx context.Context, imdbID string) (Wertung, error) {
	w := Wertung{Tomaten: -1}
	if o.Key == "" {
		return w, fmt.Errorf("für die IMDb-Wertung fehlt der Schlüssel")
	}
	if imdbID == "" {
		return w, fmt.Errorf("ohne IMDb-Kennung geht es nicht")
	}
	o.limiter.wait(ctx)

	u := o.BaseURL + "?" + url.Values{"i": {imdbID}, "apikey": {o.Key}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return w, err
	}
	resp, err := o.http.Do(req)
	if err != nil {
		return w, fmt.Errorf("OMDb nicht erreichbar: %w", err)
	}
	defer resp.Body.Close()

	var body struct {
		Response   string `json:"Response"`
		Error      string `json:"Error"`
		IMDbRating string `json:"imdbRating"`
		IMDbVotes  string `json:"imdbVotes"`
		Ratings    []struct {
			Source string `json:"Source"`
			Value  string `json:"Value"`
		} `json:"Ratings"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return w, err
	}
	if body.Response != "True" {
		return w, fmt.Errorf("OMDb: %s", orUnknown(body.Error))
	}
	w.IMDb = parseFloat(body.IMDbRating)
	w.Stimmen = parseInt(strings.ReplaceAll(body.IMDbVotes, ",", ""))
	for _, r := range body.Ratings {
		if r.Source == "Rotten Tomatoes" && strings.HasSuffix(r.Value, "%") {
			if p := parseInt(strings.TrimSuffix(r.Value, "%")); p >= 0 && p <= 100 {
				w.Tomaten = p
			}
		}
	}
	return w, nil
}

func orUnknown(s string) string {
	if s == "" {
		return "unbekannter Fehler"
	}
	return s
}

func parseFloat(s string) float64 {
	var v float64
	var frac float64 = 0
	seen := false
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			if seen {
				frac /= 10
				v += float64(r-'0') * frac
			} else {
				v = v*10 + float64(r-'0')
			}
		case r == '.' && !seen:
			seen, frac = true, 1
		default:
			return v
		}
	}
	return v
}

func parseInt(s string) int {
	v := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			break
		}
		v = v*10 + int(r-'0')
	}
	return v
}

// reihenName räumt den Namen auf, den TMDb einer Reihe gibt. Dort heisst
// sie „Alien Collection" oder „Der Herr der Ringe - Filmreihe"; in einer
// Mediathek, in der die Reihen ohnehin unter „Filmreihen" stehen, ist
// der Zusatz nur Lärm.
func reihenName(n string) string {
	n = strings.TrimSpace(n)
	// Die längeren zuerst — sonst bliebe von „Alien - Collection" ein
	// „Alien -" übrig.
	for _, weg := range []string{" - Filmreihe", " – Filmreihe", " - Collection",
		" – Collection", " - Sammlung", " – Sammlung", " - Reihe", " – Reihe",
		" Filmreihe", " Collection", " Sammlung", " Reihe"} {
		if strings.HasSuffix(strings.ToLower(n), strings.ToLower(weg)) && len(n) > len(weg)+1 {
			return strings.TrimSpace(n[:len(n)-len(weg)])
		}
	}
	return n
}

// FindIMDb sucht zu einer IMDb-Kennung die TMDb-Kennungen — als Film und
// als Serie, je nachdem, was es ist.
func (t *TMDb) FindIMDb(ctx context.Context, imdbID string) (filme, serien []string, err error) {
	return t.FindExtern(ctx, imdbID, "imdb_id")
}

// FindExtern sucht zu einer fremden Kennung (quelle: imdb_id | tvdb_id)
// die TMDb-Kennungen von Filmen und Serien.
func (t *TMDb) FindExtern(ctx context.Context, kennung, quelle string) (filme, serien []string, err error) {
	if quelle != "imdb_id" && quelle != "tvdb_id" {
		return nil, nil, errors.New("unbekannte quelle")
	}
	// Nur Buchstaben und Ziffern — die Kennung kommt in den Pfad.
	if kennung == "" || len(kennung) > 16 {
		return nil, nil, errors.New("ungültige kennung")
	}
	for _, c := range kennung {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'z') {
			return nil, nil, errors.New("ungültige kennung")
		}
	}
	var d struct {
		Movie []struct {
			ID int `json:"id"`
		} `json:"movie_results"`
		TV []struct {
			ID int `json:"id"`
		} `json:"tv_results"`
	}
	if err := t.get(ctx, "/find/"+url.PathEscape(kennung),
		url.Values{"external_source": {quelle}, "language": {t.Language}}, &d); err != nil {
		return nil, nil, err
	}
	for _, m := range d.Movie {
		filme = append(filme, strconv.Itoa(m.ID))
	}
	for _, s := range d.TV {
		serien = append(serien, strconv.Itoa(s.ID))
	}
	return filme, serien, nil
}

// Alternativtitel holt die weiteren Titel eines Eintrags (andere Länder,
// frühere Verleihtitel). Nur für knappe Fälle: Es ist eine Anfrage mehr.
func (t *TMDb) Alternativtitel(ctx context.Context, kind, id string) ([]string, error) {
	if err := tmdbKennung(id); err != nil {
		return nil, err
	}
	var d struct {
		Titles []struct {
			Title string `json:"title"`
		} `json:"titles"` // Filme
		Results []struct {
			Title string `json:"title"`
		} `json:"results"` // Serien
	}
	pfad := "/movie/" + id + "/alternative_titles"
	if kind == "series" {
		pfad = "/tv/" + id + "/alternative_titles"
	}
	if err := t.get(ctx, pfad, url.Values{}, &d); err != nil {
		return nil, err
	}
	var out []string
	for _, x := range append(d.Titles, d.Results...) {
		if x.Title != "" && len(out) < 40 {
			out = append(out, x.Title)
		}
	}
	return out, nil
}

// tmdbKennung prüft eine TMDb-Kennung, bevor sie in einen Pfad kommt.
// Kennungen von TMDb sind immer Zahlen. Was sonst ankommt — etwa beim
// Zuweisen von Hand „../account" —, würde die Anfrage an einen anderen
// Endpunkt lenken, mit dem Schlüssel des Besitzers.
func tmdbKennung(id string) error {
	if id == "" || len(id) > 12 {
		return errors.New("ungültige tmdb-kennung")
	}
	for _, c := range id {
		if c < '0' || c > '9' {
			return errors.New("ungültige tmdb-kennung")
		}
	}
	return nil
}
