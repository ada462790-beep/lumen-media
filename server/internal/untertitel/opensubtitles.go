// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

// Package untertitel holt Untertitel von OpenSubtitles.com (REST-API v1)
// und legt sie als Datei neben den Film.
//
// Den Schlüssel richtet die Verwaltung einmal ein, wie bei TMDb. Ohne
// Benutzerkonto erlaubt OpenSubtitles nur wenige Downloads am Tag; mit
// Konto mehr. Schlüssel und Passwort verlassen den Server nie und stehen
// in keiner Protokollzeile: Sie gehen nur als Kopfzeile bzw. im Körper
// der Anmeldung an OpenSubtitles.
package untertitel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"lumen/internal/netz"
)

// StandardURL ist die offizielle Adresse der Schnittstelle.
const StandardURL = "https://api.opensubtitles.com/api/v1"

// ErrKeinSchluessel: Die Verwaltung hat noch keinen Schlüssel hinterlegt.
var ErrKeinSchluessel = errors.New("OpenSubtitles ist nicht eingerichtet")

// ErrKontingent: Für heute sind keine Downloads mehr übrig.
var ErrKontingent = errors.New("für heute sind keine Downloads bei OpenSubtitles mehr übrig")

// ErrAnmeldung: Benutzername oder Passwort des OpenSubtitles-Kontos stimmen nicht.
var ErrAnmeldung = errors.New("die Anmeldung bei OpenSubtitles ist gescheitert")

// ErrSchluessel: OpenSubtitles nimmt den Schlüssel nicht an.
var ErrSchluessel = errors.New("OpenSubtitles nimmt den Schlüssel nicht an")

// Client spricht mit OpenSubtitles.
type Client struct {
	BaseURL   string
	APIKey    string
	Benutzer  string
	Passwort  string
	UserAgent string
	HTTP      *http.Client

	mu       sync.Mutex
	token    string
	tokenBis time.Time
	basis    string // nach der Anmeldung: die Adresse, die OpenSubtitles nennt
}

// Neu baut einen Client. url leer = die offizielle Adresse; dann geht
// keine Anfrage ins Heimnetz.
func Neu(apiKey, benutzer, passwort, adresse string) *Client {
	heim := adresse != ""
	if adresse == "" {
		adresse = StandardURL
	}
	return &Client{BaseURL: strings.TrimRight(adresse, "/"), APIKey: apiKey, Benutzer: benutzer,
		Passwort: passwort, UserAgent: "Lumen v1.0",
		// Eine eigene Adresse setzt nur der Betreiber (Umgebungsvariable,
		// Tests) — dort darf es auch der eigene Rechner sein.
		HTTP: netz.Client(netz.Regeln{Heimnetz: heim, Zeitlimit: 30 * time.Second})}
}

// Bereit: Ist ein Schlüssel da?
func (c *Client) Bereit() bool { return c != nil && c.APIKey != "" }

func (c *Client) anfrage(ctx context.Context, methode, basis, pfad string, q url.Values, body, ziel any, mitToken bool) error {
	if !c.Bereit() {
		return ErrKeinSchluessel
	}
	adresse := basis + pfad
	if len(q) > 0 {
		adresse += "?" + sortiert(q)
	}
	var rd *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	var req *http.Request
	var err error
	if rd != nil {
		req, err = http.NewRequestWithContext(ctx, methode, adresse, rd)
	} else {
		req, err = http.NewRequestWithContext(ctx, methode, adresse, nil)
	}
	if err != nil {
		return err
	}
	req.Header.Set("Api-Key", c.APIKey)
	req.Header.Set("User-Agent", c.UserAgent)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if mitToken {
		c.mu.Lock()
		tok := c.token
		c.mu.Unlock()
		if tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		// Nicht die Fehlermeldung weitergeben: Sie enthält die Adresse.
		return fmt.Errorf("OpenSubtitles ist nicht erreichbar")
	}
	defer resp.Body.Close()
	raw, err := netz.Lesen(resp.Body, 8<<20)
	if err != nil {
		return err
	}
	switch {
	case resp.StatusCode == http.StatusUnauthorized && pfad == "/login":
		return ErrAnmeldung
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return ErrSchluessel
	case resp.StatusCode == http.StatusNotAcceptable:
		return ErrKontingent
	case resp.StatusCode == http.StatusTooManyRequests:
		return fmt.Errorf("OpenSubtitles bittet um eine Pause — gleich noch einmal versuchen")
	case resp.StatusCode >= 300:
		return fmt.Errorf("OpenSubtitles antwortete mit %d", resp.StatusCode)
	}
	if ziel != nil {
		if err := json.Unmarshal(raw, ziel); err != nil {
			return fmt.Errorf("OpenSubtitles hat unverständlich geantwortet")
		}
	}
	return nil
}

// sortiert: OpenSubtitles verlangt die Parameter alphabetisch und klein
// geschrieben — sonst leitet es um, und jede Umleitung kostet eine Anfrage.
func sortiert(q url.Values) string {
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteByte('&')
		}
		b.WriteString(url.QueryEscape(k))
		b.WriteByte('=')
		b.WriteString(url.QueryEscape(strings.ToLower(q.Get(k))))
	}
	return b.String()
}

// anmelden holt ein Token, wenn ein Konto hinterlegt ist. Das Token gilt
// bei OpenSubtitles einen Tag; Lumen erneuert es nach 20 Stunden.
func (c *Client) anmelden(ctx context.Context) error {
	if c.Benutzer == "" || c.Passwort == "" {
		return nil
	}
	c.mu.Lock()
	gueltig := c.token != "" && time.Now().Before(c.tokenBis)
	c.mu.Unlock()
	if gueltig {
		return nil
	}
	var out struct {
		Token   string `json:"token"`
		BaseURL string `json:"base_url"`
	}
	if err := c.anfrage(ctx, http.MethodPost, c.BaseURL, "/login", nil,
		map[string]string{"username": c.Benutzer, "password": c.Passwort}, &out, false); err != nil {
		return err
	}
	if out.Token == "" {
		return ErrAnmeldung
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.token, c.tokenBis = out.Token, time.Now().Add(20*time.Hour)
	// Angemeldete Konten (etwa VIP) bekommen eine eigene Adresse. Nur bei
	// der offiziellen Schnittstelle folgen — eine eigene Adresse des
	// Betreibers bleibt, wie sie ist.
	c.basis = ""
	if c.BaseURL == StandardURL && out.BaseURL != "" && !strings.ContainsAny(out.BaseURL, "/?#@") {
		c.basis = "https://" + out.BaseURL + "/api/v1"
	}
	return nil
}

func (c *Client) basisURL() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.basis != "" {
		return c.basis
	}
	return c.BaseURL
}

// Pruefen meldet sich (falls ein Konto da ist) an und fragt das
// Kontingent ab — für „Verbindung prüfen" in der Verwaltung.
func (c *Client) Pruefen(ctx context.Context) (Kontingent, error) {
	if err := c.anmelden(ctx); err != nil {
		return Kontingent{}, err
	}
	var out struct {
		Data struct {
			AllowedDownloads   int  `json:"allowed_downloads"`
			RemainingDownloads int  `json:"remaining_downloads"`
			VIP                bool `json:"vip"`
		} `json:"data"`
	}
	if c.Benutzer == "" {
		// Ohne Konto gibt es keine Auskunft; eine Suche zeigt, ob der
		// Schlüssel gilt.
		err := c.anfrage(ctx, http.MethodGet, c.basisURL(), "/infos/formats", nil, nil, nil, false)
		return Kontingent{Rest: -1}, err
	}
	if err := c.anfrage(ctx, http.MethodGet, c.basisURL(), "/infos/user", nil, nil, &out, true); err != nil {
		return Kontingent{}, err
	}
	return Kontingent{Erlaubt: out.Data.AllowedDownloads, Rest: out.Data.RemainingDownloads}, nil
}

// Suche beschreibt, wonach gesucht wird. Je mehr bekannt ist, desto
// besser: der Hash trifft genau diese Datei, die Kennungen den Film.
type Suche struct {
	Hash     string
	IMDbID   string // „tt0133093" oder „133093"
	TMDbID   string
	Serie    bool // IMDb/TMDb sind die der Serie; Staffel und Folge gelten
	Staffel  int
	Folge    int
	Name     string
	Jahr     int
	Sprachen []string
}

// Treffer ist ein Untertitel, wie ihn die Suche liefert.
type Treffer struct {
	ID              string  `json:"id"`
	DateiID         int     `json:"dateiId"`
	Dateiname       string  `json:"dateiname"`
	Sprache         string  `json:"sprache"`
	Downloads       int     `json:"downloads"`
	HashTreffer     bool    `json:"hashTreffer"`
	Hoergeschaedigt bool    `json:"hoergeschaedigt"`
	Erzwungen       bool    `json:"erzwungen"`
	Maschinell      bool    `json:"maschinell"`
	Release         string  `json:"release,omitempty"`
	Bewertung       float64 `json:"bewertung,omitempty"`
	Titel           string  `json:"titel,omitempty"`
	Jahr            int     `json:"jahr,omitempty"`
}

func nurZiffern(s string) string {
	s = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(s)), "tt")
	if _, err := strconv.Atoi(s); err != nil {
		return ""
	}
	return strings.TrimLeft(s, "0")
}

// Suchen fragt OpenSubtitles. Treffer mit passendem Hash zuerst, dann von
// Menschen gemachte vor maschinell übersetzten, dann nach Downloads.
func (c *Client) Suchen(ctx context.Context, s Suche) ([]Treffer, error) {
	q := url.Values{}
	if len(s.Sprachen) > 0 {
		sp := append([]string(nil), s.Sprachen...)
		sort.Strings(sp)
		q.Set("languages", strings.Join(sp, ","))
	}
	if s.Hash != "" {
		q.Set("moviehash", s.Hash)
	}
	imdb, tmdb := nurZiffern(s.IMDbID), nurZiffern(s.TMDbID)
	switch {
	case s.Serie && (imdb != "" || tmdb != ""):
		if imdb != "" {
			q.Set("parent_imdb_id", imdb)
		} else {
			q.Set("parent_tmdb_id", tmdb)
		}
		q.Set("season_number", strconv.Itoa(s.Staffel))
		q.Set("episode_number", strconv.Itoa(s.Folge))
	case imdb != "":
		q.Set("imdb_id", imdb)
	case tmdb != "":
		q.Set("tmdb_id", tmdb)
	case strings.TrimSpace(s.Name) != "":
		q.Set("query", strings.TrimSpace(s.Name))
		if s.Serie {
			q.Set("season_number", strconv.Itoa(s.Staffel))
			q.Set("episode_number", strconv.Itoa(s.Folge))
		} else if s.Jahr > 0 {
			q.Set("year", strconv.Itoa(s.Jahr))
		}
	case s.Hash == "":
		return nil, fmt.Errorf("es gibt nichts, wonach sich suchen liesse")
	}
	if err := c.anmelden(ctx); err != nil {
		return nil, err
	}
	var out struct {
		Data []struct {
			ID         string `json:"id"`
			Attributes struct {
				Language          string  `json:"language"`
				DownloadCount     int     `json:"download_count"`
				HearingImpaired   bool    `json:"hearing_impaired"`
				ForeignPartsOnly  bool    `json:"foreign_parts_only"`
				MoviehashMatch    bool    `json:"moviehash_match"`
				AITranslated      bool    `json:"ai_translated"`
				MachineTranslated bool    `json:"machine_translated"`
				Release           string  `json:"release"`
				Ratings           float64 `json:"ratings"`
				FeatureDetails    struct {
					Title     string `json:"title"`
					MovieName string `json:"movie_name"`
					Year      int    `json:"year"`
				} `json:"feature_details"`
				Files []struct {
					FileID   int    `json:"file_id"`
					FileName string `json:"file_name"`
				} `json:"files"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := c.anfrage(ctx, http.MethodGet, c.basisURL(), "/subtitles", q, nil, &out, true); err != nil {
		return nil, err
	}
	treffer := []Treffer{}
	for _, d := range out.Data {
		a := d.Attributes
		if len(a.Files) == 0 {
			continue
		}
		t := Treffer{ID: d.ID, DateiID: a.Files[0].FileID, Dateiname: a.Files[0].FileName,
			Sprache: strings.ToLower(a.Language), Downloads: a.DownloadCount, HashTreffer: a.MoviehashMatch,
			Hoergeschaedigt: a.HearingImpaired, Erzwungen: a.ForeignPartsOnly,
			Maschinell: a.AITranslated || a.MachineTranslated, Release: a.Release, Bewertung: a.Ratings,
			Titel: a.FeatureDetails.Title, Jahr: a.FeatureDetails.Year}
		if t.Titel == "" {
			t.Titel = a.FeatureDetails.MovieName
		}
		treffer = append(treffer, t)
	}
	sort.SliceStable(treffer, func(i, j int) bool {
		a, b := treffer[i], treffer[j]
		if a.HashTreffer != b.HashTreffer {
			return a.HashTreffer
		}
		if a.Maschinell != b.Maschinell {
			return !a.Maschinell
		}
		return a.Downloads > b.Downloads
	})
	return treffer, nil
}

// Kontingent: wie viele Downloads heute noch gehen. Rest -1 = unbekannt.
type Kontingent struct {
	Erlaubt int `json:"erlaubt,omitempty"`
	Rest    int `json:"rest"`
}

// Herunterladen holt die Datei zu einem Treffer. Zurück kommt der Inhalt
// als UTF-8-SRT.
func (c *Client) Herunterladen(ctx context.Context, dateiID int) ([]byte, Kontingent, error) {
	if err := c.anmelden(ctx); err != nil {
		return nil, Kontingent{}, err
	}
	var out struct {
		Link      string `json:"link"`
		Remaining int    `json:"remaining"`
		Requests  int    `json:"requests"`
		Message   string `json:"message"`
	}
	if err := c.anfrage(ctx, http.MethodPost, c.basisURL(), "/download", nil,
		map[string]any{"file_id": dateiID, "sub_format": "srt"}, &out, true); err != nil {
		return nil, Kontingent{}, err
	}
	k := Kontingent{Rest: out.Remaining}
	if out.Link == "" {
		if out.Remaining <= 0 {
			return nil, k, ErrKontingent
		}
		return nil, k, fmt.Errorf("OpenSubtitles hat keinen Download-Link geschickt")
	}
	u, err := netz.AdressePruefen(out.Link)
	if err != nil {
		return nil, k, fmt.Errorf("OpenSubtitles hat einen ungültigen Link geschickt")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, k, err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, k, fmt.Errorf("die Untertiteldatei liess sich nicht laden")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, k, fmt.Errorf("die Untertiteldatei liess sich nicht laden (%d)", resp.StatusCode)
	}
	// Eine Untertiteldatei hat selten mehr als 200 KB; 5 MB sind reichlich.
	raw, err := netz.Lesen(resp.Body, 5<<20)
	if err != nil {
		return nil, k, fmt.Errorf("die Untertiteldatei ist zu gross")
	}
	srt, err := AlsSRT(raw)
	return srt, k, err
}

// AlsSRT prüft, dass es wirklich Untertitel sind, und bringt den Text nach
// UTF-8. Ältere Dateien sind oft in Latin-1 geschrieben — als UTF-8
// gelesen stünde dann „FÃ¼r" statt „Für".
func AlsSRT(raw []byte) ([]byte, error) {
	raw = bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF})
	if !utf8.Valid(raw) {
		var b strings.Builder
		for _, x := range raw {
			b.WriteRune(rune(x))
		}
		raw = []byte(b.String())
	}
	if !bytes.Contains(raw, []byte("-->")) {
		return nil, fmt.Errorf("die Datei enthält keine Untertitel")
	}
	if bytes.IndexByte(raw, 0) >= 0 {
		return nil, fmt.Errorf("die Datei enthält keine Untertitel")
	}
	return raw, nil
}
