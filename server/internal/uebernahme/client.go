// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

// Package uebernahme holt den eigenen Stand aus Emby oder Jellyfin:
// was gesehen ist, wo man stehen geblieben ist, Favoriten, Bewertungen.
//
// Wer von Emby zu Lumen wechselt, soll nicht bei null anfangen. Jedes
// Profil holt dabei nur seine eigenen Daten — mit dem eigenen Emby-
// Benutzernamen und Passwort. Lumen merkt sich davon nichts: Das Passwort
// wird nur für die Anmeldung gebraucht, das Token von Emby lebt nur im
// Arbeitsspeicher, bis die Daten geholt sind, und wird danach bei Emby
// abgemeldet.
//
// Emby und Jellyfin sprechen fast dieselbe Sprache (Jellyfin ist aus Emby
// hervorgegangen). Unterschiede, die hier zählen: Jellyfin verlangt die
// Angaben zum Gerät im Kopf „Authorization", ältere Emby-Fassungen nur in
// „X-Emby-Authorization". Lumen schickt beide.
package uebernahme

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"lumen/internal/netz"
)

// Grenzen gegen einen Server, der den Speicher von Lumen füllen will —
// die Adresse tippt jemand ein, und was dort antwortet, muss nicht Emby
// sein. Eine Seite mit 500 Titeln samt Pfaden und Kennungen ist ein paar
// hundert Kilobyte gross; die Grenzen lassen echten Mediatheken viel Luft.
const (
	// antwortHoechstens: eine einzelne Antwort.
	antwortHoechstens = 4 << 20
	// gesamtHoechstens: alle Antworten eines Laufs zusammen. Ohne diese
	// Grenze könnte ein Server 100 Seiten zu je 4 MB schicken.
	gesamtHoechstens = 64 << 20
	// eintraegeJeSeite: mehr als verlangt (seite) nimmt Lumen hin, aber
	// nicht beliebig viel mehr.
	eintraegeJeSeite = 2 * seite
	// EintraegeHoechstens: alle Titel eines Laufs zusammen.
	EintraegeHoechstens = 50_000
	// namenHoechstens: Namen auf der Anmeldeseite.
	namenHoechstens = 200
)

// seite: so viele Titel je Abfrage.
const seite = 500

// ErrZuViel: Der Server hat mehr Titel geschickt, als Lumen annimmt.
var ErrZuViel = errors.New("der Server hat zu viele Einträge geschickt")

// ErrAnmeldung: Benutzername oder Passwort stimmen nicht.
var ErrAnmeldung = errors.New("Benutzername oder Passwort stimmt nicht")

// ErrKeinServer: Unter der Adresse antwortet kein Emby oder Jellyfin.
var ErrKeinServer = errors.New("unter dieser Adresse antwortet kein Emby- oder Jellyfin-Server")

// Server ist eine Verbindung zu einem Emby- oder Jellyfin-Server.
type Server struct {
	basis    *url.URL
	http     *http.Client
	geraet   string
	token    string
	benutzer string
	Art      string // "Emby" | "Jellyfin"

	// Was dieser Lauf schon gelesen hat (gegen gesamtHoechstens und
	// EintraegeHoechstens). Ein Server wird nur von einem Lauf benutzt.
	gelesenBytes int64
	eintraege    int
}

// seitenLeser liest eine Antwort selbst, Stück für Stück, statt sie als
// Ganzes zu dekodieren — für die Titellisten, deren Einträge gezählt
// werden, während sie ankommen.
type seitenLeser interface {
	lesen(io.Reader) error
}

// begrenzt liest höchstens rest Bytes und meldet danach ErrZuGross statt
// eines stillen Endes — abgeschnittenes JSON soll nicht als „kein
// Server" durchgehen, und der Dekoder bricht sofort ab.
type begrenzt struct {
	r    io.Reader
	rest int64
	n    int64
}

func (b *begrenzt) Read(p []byte) (int, error) {
	if b.rest <= 0 {
		// Ist die Antwort genau hier zu Ende, ist alles gut.
		var eins [1]byte
		if n, _ := b.r.Read(eins[:]); n > 0 {
			return 0, netz.ErrZuGross
		}
		return 0, io.EOF
	}
	if int64(len(p)) > b.rest {
		p = p[:b.rest]
	}
	n, err := b.r.Read(p)
	b.rest -= int64(n)
	b.n += int64(n)
	return n, err
}

// Info ist, was der Server ohne Anmeldung über sich sagt.
type Info struct {
	Art     string `json:"art"`
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Verbinden prüft die Adresse und bereitet die Verbindung vor. Verbunden
// wird erst mit der ersten Anfrage. client darf nil sein; dann gilt der
// sichere Standard (Heimnetz erlaubt, keine Weiterleitung auf andere
// Rechner, 30 Sekunden je Anfrage).
func Verbinden(adresse string, client *http.Client) (*Server, error) {
	u, err := netz.AdressePruefen(adresse)
	if err != nil {
		return nil, err
	}
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawQuery, u.Fragment = "", ""
	if client == nil {
		// Nur das Heimnetz: Emby und Jellyfin laufen dort. Eine Adresse
		// im Internet hiesse, dass jemand Lumen als Werkzeug benutzt, um
		// fremde Rechner abzutasten oder Passwörter durchzuprobieren.
		client = netz.Client(netz.Regeln{NurHeimnetz: true, GleicherHost: true, Zeitlimit: 30 * time.Second})
	}
	var g [8]byte
	_, _ = rand.Read(g[:])
	return &Server{basis: u, http: client, geraet: "lumen-" + hex.EncodeToString(g[:]), Art: "Emby"}, nil
}

func (s *Server) kopf() string {
	k := fmt.Sprintf(`MediaBrowser Client="Lumen", Device="Lumen", DeviceId="%s", Version="1.0"`, s.geraet)
	if s.token != "" {
		k += fmt.Sprintf(`, Token="%s"`, s.token)
	}
	return k
}

// anfrage schickt eine Anfrage und liest die Antwort als JSON in ziel.
func (s *Server) anfrage(ctx context.Context, methode, pfad string, query url.Values, body, ziel any) error {
	u := *s.basis
	u.Path += pfad
	if query != nil {
		u.RawQuery = query.Encode()
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
		req, err = http.NewRequestWithContext(ctx, methode, u.String(), rd)
	} else {
		req, err = http.NewRequestWithContext(ctx, methode, u.String(), nil)
	}
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", s.kopf())
	req.Header.Set("X-Emby-Authorization", s.kopf())
	if s.token != "" {
		req.Header.Set("X-Emby-Token", s.token)
	}
	resp, err := s.http.Do(req)
	if err != nil {
		if errors.Is(err, netz.ErrAdresse) {
			return err
		}
		return fmt.Errorf("%w (keine Verbindung)", ErrKeinServer)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return ErrAnmeldung
	case resp.StatusCode == http.StatusNotFound:
		return ErrKeinServer
	case resp.StatusCode >= 300:
		return fmt.Errorf("der Server antwortete mit %d", resp.StatusCode)
	}
	if ziel == nil {
		return nil
	}
	rest := min(int64(antwortHoechstens), gesamtHoechstens-s.gelesenBytes)
	if rest <= 0 {
		return netz.ErrZuGross
	}
	limitiert := &begrenzt{r: resp.Body, rest: rest}
	defer func() { s.gelesenBytes += limitiert.n }()
	if l, ok := ziel.(seitenLeser); ok {
		if err := l.lesen(limitiert); err != nil {
			if errors.Is(err, netz.ErrZuGross) || errors.Is(err, ErrZuViel) || errors.Is(err, context.DeadlineExceeded) {
				return err
			}
			return ErrKeinServer
		}
		return nil
	}
	if err := json.NewDecoder(limitiert).Decode(ziel); err != nil {
		if errors.Is(err, netz.ErrZuGross) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return ErrKeinServer
	}
	return nil
}

// Info fragt, wer da antwortet — ohne Anmeldung.
func (s *Server) Info(ctx context.Context) (Info, error) {
	var in struct {
		ServerName  string
		Version     string
		ProductName string
		ID          string `json:"Id"`
	}
	if err := s.anfrage(ctx, http.MethodGet, "/System/Info/Public", nil, nil, &in); err != nil {
		return Info{}, err
	}
	if in.ID == "" && in.Version == "" {
		return Info{}, ErrKeinServer
	}
	s.Art = "Emby"
	if strings.Contains(strings.ToLower(in.ProductName), "jellyfin") {
		s.Art = "Jellyfin"
	}
	return Info{Art: s.Art, Name: in.ServerName, Version: in.Version}, nil
}

// Benutzer nennt die Namen, die der Server auf seiner Anmeldeseite zeigt.
// Viele Server zeigen keine — dann tippt man den Namen selbst ein.
func (s *Server) Benutzer(ctx context.Context) ([]string, error) {
	var liste []struct{ Name string }
	if err := s.anfrage(ctx, http.MethodGet, "/Users/Public", nil, nil, &liste); err != nil {
		return nil, err
	}
	out := []string{}
	for _, b := range liste {
		if b.Name != "" && len(b.Name) <= 200 && len(out) < namenHoechstens {
			out = append(out, b.Name)
		}
	}
	return out, nil
}

// Anmelden meldet sich als dieser Benutzer an. Das Passwort wird nur hier
// gebraucht und nirgends aufbewahrt.
func (s *Server) Anmelden(ctx context.Context, name, passwort string) error {
	var out struct {
		AccessToken string
		User        struct {
			ID string `json:"Id"`
		}
	}
	// Emby und Jellyfin nehmen das Passwort im Feld „Pw".
	body := map[string]string{"Username": name, "Pw": passwort}
	if err := s.anfrage(ctx, http.MethodPost, "/Users/AuthenticateByName", nil, body, &out); err != nil {
		if errors.Is(err, ErrKeinServer) {
			return ErrAnmeldung
		}
		return err
	}
	if out.AccessToken == "" || out.User.ID == "" {
		return ErrAnmeldung
	}
	s.token, s.benutzer = out.AccessToken, out.User.ID
	return nil
}

// Abmelden macht das Token bei Emby ungültig und vergisst es. Fehler
// zählen nicht: Das Token ist danach auf jeden Fall aus Lumen weg.
func (s *Server) Abmelden(ctx context.Context) {
	if s.token != "" {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		_ = s.anfrage(ctx, http.MethodPost, "/Sessions/Logout", nil, nil, nil)
		cancel()
	}
	s.token, s.benutzer = "", ""
}

// ---------------------------------------------------------------- Titel

// Eintrag ist ein Titel mit dem Stand dieses Benutzers.
type Eintrag struct {
	ID        string
	Typ       string // Movie | Episode | Series
	Name      string
	Jahr      int
	Pfad      string
	Groesse   int64
	Kennungen map[string]string // tmdb | imdb | tvdb → Kennung
	SerieID   string
	SerieName string
	Staffel   int
	Folge     int
	LaengeMS  int64

	Gesehen    bool
	PositionMS int64
	Favorit    bool
	// Sterne 1–5, 0 = keine Bewertung.
	Sterne int
	// Zuletzt: wann Emby es zuletzt abgespielt hat. Nur, um die
	// Reihenfolge von „Weiterschauen" zu übernehmen — Lumen speichert
	// den Zeitpunkt nicht.
	Zuletzt time.Time
}

type rohTitel struct {
	ID                string `json:"Id"`
	Name              string
	Type              string
	ProductionYear    int
	Path              string
	ProviderIds       map[string]string
	SeriesID          string `json:"SeriesId"`
	SeriesName        string
	ParentIndexNumber int
	IndexNumber       int
	RunTimeTicks      int64
	MediaSources      []struct{ Size int64 }
	UserData          struct {
		Played                bool
		PlaybackPositionTicks int64
		IsFavorite            bool
		Likes                 *bool
		Rating                *float64
		LastPlayedDate        string
	}
}

func (r rohTitel) eintrag() Eintrag {
	e := Eintrag{ID: r.ID, Typ: r.Type, Name: r.Name, Jahr: r.ProductionYear, Pfad: r.Path,
		SerieID: r.SeriesID, SerieName: r.SeriesName, Staffel: r.ParentIndexNumber, Folge: r.IndexNumber,
		LaengeMS: r.RunTimeTicks / 10_000, Kennungen: map[string]string{},
		Gesehen: r.UserData.Played, PositionMS: r.UserData.PlaybackPositionTicks / 10_000,
		Favorit: r.UserData.IsFavorite}
	for k, v := range r.ProviderIds {
		if v = strings.TrimSpace(v); v != "" {
			e.Kennungen[strings.ToLower(k)] = v
		}
	}
	for _, m := range r.MediaSources {
		if m.Size > e.Groesse {
			e.Groesse = m.Size
		}
	}
	// Emby bewertet von 0 bis 10, Lumen mit 1 bis 5 Sternen. Ein
	// „Daumen hoch" ohne Note gilt als vier Sterne.
	switch {
	case r.UserData.Rating != nil && *r.UserData.Rating > 0:
		e.Sterne = min(5, max(1, int(*r.UserData.Rating/2+0.5)))
	case r.UserData.Likes != nil && *r.UserData.Likes:
		e.Sterne = 4
	}
	if t, err := time.Parse(time.RFC3339Nano, r.UserData.LastPlayedDate); err == nil {
		e.Zuletzt = t
	}
	return e
}

// titelHolen holt alle Titel einer Abfrage, Seite für Seite.
func (s *Server) titelHolen(ctx context.Context, typen, filter string, fertig func(int)) ([]Eintrag, error) {
	var out []Eintrag
	// Weiter nach dem, was wirklich kam: Manche Server liefern weniger als
	// verlangt (eigene Obergrenze je Seite).
	for start := 0; ; {
		q := url.Values{
			"Recursive":        {"true"},
			"IncludeItemTypes": {typen},
			"Fields":           {"ProviderIds,Path,MediaSources,ProductionYear"},
			"EnableImages":     {"false"},
			"EnableUserData":   {"true"},
			"StartIndex":       {fmt.Sprint(start)},
			"Limit":            {fmt.Sprint(seite)},
		}
		if filter != "" {
			q.Set("Filters", filter)
		}
		antwort := &titelSeite{}
		if err := s.anfrage(ctx, http.MethodGet, "/Users/"+url.PathEscape(s.benutzer)+"/Items", q, nil, antwort); err != nil {
			return nil, err
		}
		s.eintraege += len(antwort.Items)
		if s.eintraege > EintraegeHoechstens {
			return nil, ErrZuViel
		}
		for _, r := range antwort.Items {
			out = append(out, r.eintrag())
		}
		if fertig != nil {
			fertig(len(antwort.Items))
		}
		start += len(antwort.Items)
		if len(antwort.Items) == 0 || start >= antwort.TotalRecordCount {
			return out, nil
		}
	}
}

// titelSeite ist eine Antwort von /Users/{id}/Items. Sie wird gelesen,
// während sie ankommt, und die Titel werden dabei gezählt: Bei mehr als
// eintraegeJeSeite bricht Lumen ab, statt erst alles in den Speicher zu
// holen und dann nachzuzählen.
type titelSeite struct {
	Items            []rohTitel
	TotalRecordCount int
}

func (t *titelSeite) lesen(r io.Reader) error {
	dec := json.NewDecoder(r)
	if tok, err := dec.Token(); err != nil {
		return err
	} else if d, ok := tok.(json.Delim); !ok || d != '{' {
		return ErrKeinServer
	}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		schluessel, _ := tok.(string)
		switch schluessel {
		case "Items":
			if tok, err := dec.Token(); err != nil {
				return err
			} else if tok == nil {
				continue // "Items": null
			} else if d, ok := tok.(json.Delim); !ok || d != '[' {
				return ErrKeinServer
			}
			for dec.More() {
				if len(t.Items) >= eintraegeJeSeite {
					return ErrZuViel
				}
				var x rohTitel
				if err := dec.Decode(&x); err != nil {
					return err
				}
				t.Items = append(t.Items, x)
			}
			if _, err := dec.Token(); err != nil { // ']'
				return err
			}
		case "TotalRecordCount":
			if err := dec.Decode(&t.TotalRecordCount); err != nil {
				return err
			}
		default:
			// Alles andere überspringen; die Grösse begrenzt der Leser.
			var weg json.RawMessage
			if err := dec.Decode(&weg); err != nil {
				return err
			}
		}
	}
	_, err := dec.Token() // '}'
	return err
}

// Stand holt alles, was dieser Benutzer gesehen, angefangen, favorisiert
// oder bewertet hat, und dazu alle Serien (für die Zuordnung der Folgen).
// fertig meldet, wie viele Titel schon gelesen sind.
func (s *Server) Stand(ctx context.Context, fertig func(int)) (titel []Eintrag, serien map[string]Eintrag, err error) {
	if s.token == "" {
		return nil, nil, ErrAnmeldung
	}
	gelesen := 0
	zaehlen := func(n int) {
		gelesen += n
		if fertig != nil {
			fertig(gelesen)
		}
	}
	nachID := map[string]int{}
	// Gesehen, angefangen, Favoriten, „gefällt mir" — vier Abfragen statt
	// der ganzen Mediathek. Was in mehreren steht, zählt einmal.
	for _, filter := range []string{"IsPlayed", "IsResumable", "IsFavorite", "Likes"} {
		liste, err := s.titelHolen(ctx, "Movie,Episode,Series", filter, zaehlen)
		if err != nil {
			return nil, nil, err
		}
		for _, e := range liste {
			if _, da := nachID[e.ID]; da {
				continue
			}
			nachID[e.ID] = len(titel)
			titel = append(titel, e)
		}
	}
	alle, err := s.titelHolen(ctx, "Series", "", zaehlen)
	if err != nil {
		return nil, nil, err
	}
	serien = map[string]Eintrag{}
	for _, e := range alle {
		serien[e.ID] = e
	}
	return titel, serien, nil
}

// FilmeUndSerien holt alle Filme und Serien, die dieser Benutzer sieht —
// mit Pfad, Grösse und Kennungen, für das Übernehmen der Zuordnungen
// (kennungen.go). Den Stand des Benutzers braucht es dafür nicht.
func (s *Server) FilmeUndSerien(ctx context.Context, fertig func(int)) ([]Eintrag, error) {
	if s.token == "" {
		return nil, ErrAnmeldung
	}
	return s.titelHolen(ctx, "Movie,Series", "", fertig)
}
