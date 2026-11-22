// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Server ist ein gemerkter Lumen-Server.
type Server struct {
	ID string `json:"id"`
	// Adresse: Schema, Host und Anschluss, ohne Pfad —
	// http://192.168.10.10:8097. Die Web-App liegt dort unter „/".
	Adresse string    `json:"adresse"`
	Name    string    `json:"name"`
	Version string    `json:"version,omitempty"`
	Zuletzt time.Time `json:"zuletzt,omitempty"`
}

// Serverliste hält die gemerkten Server und welcher gerade gilt — wie in
// Emby und in den Lumen-Apps für Apple und Android.
type Serverliste struct {
	mu     sync.Mutex
	pfad   string
	Server []Server `json:"server"`
	Aktiv  string   `json:"aktiv,omitempty"`
}

func LadeServerliste(pfad string) (*Serverliste, error) {
	l := &Serverliste{pfad: pfad}
	err := jsonLesen(pfad, l)
	if l.Server == nil {
		l.Server = []Server{}
	}
	return l, err
}

func (l *Serverliste) speichern() error {
	if l.pfad == "" {
		return nil
	}
	return jsonSchreiben(l.pfad, l)
}

// Kopie gibt die Liste zurück, ohne dass der Aufrufer sie verändern kann.
func (l *Serverliste) Kopie() ([]Server, string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]Server(nil), l.Server...), l.Aktiv
}

// Merken nimmt einen Server auf oder frischt ihn auf (gleiche Adresse =
// derselbe Server) und macht ihn zum aktiven.
func (l *Serverliste) Merken(s Server) (Server, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	s.Zuletzt = time.Now().UTC()
	for i, alt := range l.Server {
		if strings.EqualFold(alt.Adresse, s.Adresse) {
			s.ID = alt.ID
			l.Server[i] = s
			l.Aktiv = s.ID
			return s, l.speichern()
		}
	}
	if s.ID == "" {
		s.ID = neueKennung()
	}
	l.Server = append(l.Server, s)
	l.Aktiv = s.ID
	return s, l.speichern()
}

// Waehlen macht einen gemerkten Server zum aktiven.
func (l *Serverliste) Waehlen(id string) (Server, bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for i, s := range l.Server {
		if s.ID == id {
			l.Server[i].Zuletzt = time.Now().UTC()
			l.Aktiv = id
			return l.Server[i], true, l.speichern()
		}
	}
	return Server{}, false, nil
}

// Entfernen vergisst einen Server. War er der aktive, gilt keiner mehr —
// beim nächsten Start erscheint die Auswahl.
func (l *Serverliste) Entfernen(id string) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for i, s := range l.Server {
		if s.ID == id {
			l.Server = append(l.Server[:i], l.Server[i+1:]...)
			if l.Aktiv == id {
				l.Aktiv = ""
			}
			return true, l.speichern()
		}
	}
	return false, nil
}

// AktiverServer ist der Server, mit dem das Fenster startet.
func (l *Serverliste) AktiverServer() (Server, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, s := range l.Server {
		if s.ID == l.Aktiv {
			return s, true
		}
	}
	return Server{}, false
}

// Gehoert sagt, ob eine Adresse zu einem gemerkten Server gehört (gleicher
// Ursprung: Schema, Host, Anschluss).
func (l *Serverliste) Gehoert(u *url.URL) (Server, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, s := range l.Server {
		if gleicherUrsprung(s.Adresse, u) {
			return s, true
		}
	}
	return Server{}, false
}

func gleicherUrsprung(adresse string, u *url.URL) bool {
	a, err := url.Parse(adresse)
	if err != nil || u == nil {
		return false
	}
	return strings.EqualFold(a.Scheme, u.Scheme) && strings.EqualFold(hostMitPort(a), hostMitPort(u))
}

// hostMitPort ergänzt den Standardanschluss des Schemas, damit
// https://x und https://x:443 als gleich gelten.
func hostMitPort(u *url.URL) string {
	if u.Port() != "" {
		return strings.ToLower(u.Host)
	}
	p := "80"
	if strings.EqualFold(u.Scheme, "https") {
		p = "443"
	}
	return strings.ToLower(net.JoinHostPort(u.Hostname(), p))
}

func neueKennung() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ---------------------------------------------------------------- Adresse

// Fehlercodes für die eingebaute Seite. Sie übersetzt sie selbst (Deutsch
// oder Englisch, je nach Windows).
const (
	fehlerAdresse     = "adresse_ungueltig"
	fehlerErreichbar  = "nicht_erreichbar"
	fehlerKeinLumen   = "kein_lumen"
	fehlerZertifikat  = "zertifikat"
	fehlerUnbekannt   = "unbekannt"
	fehlerNichtErlaub = "nicht_erlaubt"
)

type codeFehler struct{ code string }

func (f codeFehler) Error() string { return f.code }

func fehlerCode(err error) string {
	var c codeFehler
	if errors.As(err, &c) {
		return c.code
	}
	return fehlerUnbekannt
}

// Kandidaten macht aus dem, was jemand eintippt, die Adressen, unter denen
// Lumen nachsehen soll — in dieser Reihenfolge:
//
//	192.168.10.10           → http://192.168.10.10:8097, https://192.168.10.10
//	192.168.10.10:8097      → http://192.168.10.10:8097
//	https://lumen.example   → https://lumen.example, (kein zweiter Versuch)
//	http://nas              → http://nas, http://nas:8097
//
// Pfad, Suche und Anker fallen weg: Die Web-App liegt immer unter „/".
func Kandidaten(eingabe string) ([]string, error) {
	e := strings.TrimSpace(eingabe)
	e = strings.TrimRight(e, "/")
	if e == "" || strings.ContainsAny(e, " \t\r\n") || strings.HasSuffix(e, ":") {
		return nil, codeFehler{fehlerAdresse}
	}
	mitSchema := strings.Contains(e, "://")
	if !mitSchema {
		e = "http://" + e
	}
	u, err := url.Parse(e)
	if err != nil || u.Hostname() == "" || u.User != nil {
		return nil, codeFehler{fehlerAdresse}
	}
	schema := strings.ToLower(u.Scheme)
	if schema != "http" && schema != "https" {
		return nil, codeFehler{fehlerAdresse}
	}
	host := u.Hostname()
	if strings.HasPrefix(host, "[") || strings.ContainsAny(host, "/\\@") {
		return nil, codeFehler{fehlerAdresse}
	}
	if p := u.Port(); p != "" {
		return []string{schema + "://" + net.JoinHostPort(host, p)}, nil
	}
	ohnePort := schema + "://" + bracket(host)
	mitPort := "http://" + net.JoinHostPort(host, standardPort)
	switch {
	case !mitSchema:
		return []string{mitPort, "https://" + bracket(host)}, nil
	case schema == "http":
		return []string{ohnePort, mitPort}, nil
	default:
		return []string{ohnePort}, nil
	}
}

// bracket setzt IPv6-Adressen in eckige Klammern (http://[fe80::1]).
func bracket(host string) string {
	if strings.Contains(host, ":") {
		return "[" + host + "]"
	}
	return host
}

// Gefunden ist die Antwort eines Lumen-Servers auf /api/v1/hello.
type Gefunden struct {
	Adresse      string `json:"adresse"`
	Name         string `json:"name"`
	Version      string `json:"version"`
	Eingerichtet bool   `json:"eingerichtet"`
}

// Pruefen fragt jede Kandidaten-Adresse nach /api/v1/hello — der Weg, den
// auch die Apps beim Verbinden nehmen. Er ist ohne Anmeldung offen und
// verrät nur, dass hier ein Lumen läuft, wie er heisst und welche Fassung.
func Pruefen(ctx context.Context, client *http.Client, eingabe string) (Gefunden, error) {
	kandidaten, err := Kandidaten(eingabe)
	if err != nil {
		return Gefunden{}, err
	}
	var letzter error = codeFehler{fehlerErreichbar}
	for _, k := range kandidaten {
		g, err := hallo(ctx, client, k)
		if err == nil {
			return g, nil
		}
		// „Kein Lumen" und „Zertifikat" sagen mehr als „nicht erreichbar" —
		// sie sollen nicht vom nächsten Versuch überschrieben werden.
		if c := fehlerCode(err); c == fehlerKeinLumen || c == fehlerZertifikat || fehlerCode(letzter) == fehlerErreichbar {
			letzter = err
		}
		if ctx.Err() != nil {
			break
		}
	}
	return Gefunden{}, letzter
}

func hallo(ctx context.Context, client *http.Client, basis string) (Gefunden, error) {
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", basis+"/api/v1/hello", nil)
	if err != nil {
		return Gefunden{}, codeFehler{fehlerAdresse}
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "LumenDesktop/"+version)
	resp, err := client.Do(req)
	if err != nil {
		var zert *tls.CertificateVerificationError
		var unbekannt x509.UnknownAuthorityError
		var name x509.HostnameError
		if errors.As(err, &zert) || errors.As(err, &unbekannt) || errors.As(err, &name) {
			return Gefunden{}, codeFehler{fehlerZertifikat}
		}
		return Gefunden{}, codeFehler{fehlerErreichbar}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Gefunden{}, codeFehler{fehlerKeinLumen}
	}
	var h struct {
		Server  string `json:"server"`
		Name    string `json:"name"`
		Version string `json:"version"`
		SetUp   bool   `json:"setUp"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&h); err != nil || h.Server != "Lumen" {
		return Gefunden{}, codeFehler{fehlerKeinLumen}
	}
	// Hat der Besitzer keinen Namen vergeben, steht der Host da — so sieht
	// man in der Liste wenigstens, welcher Rechner es ist.
	name := strings.TrimSpace(h.Name)
	if name == "" {
		if u, err := url.Parse(basis); err == nil {
			name = u.Hostname()
		}
	}
	return Gefunden{Adresse: basis, Name: kuerzen(name, 80), Version: kuerzen(h.Version, 40), Eingerichtet: h.SetUp}, nil
}

func kuerzen(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}
