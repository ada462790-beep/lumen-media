// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package main

// Der Update-Hinweis. Lumen für Windows aktualisiert sich nicht selbst:
// Es fragt höchstens einmal am Tag bei GitHub, ob es eine neuere Fassung
// gibt, und bietet sie dezent an. Lädt jemand sie, prüft die App sie wie
// ein Lumen-Server sein eigenes Update (server/internal/update):
//
//  1. Die Unterschrift unter pruefsummen.txt muss vom Schlüssel der
//     Veröffentlichungen stammen (der öffentliche Teil steht unten).
//  2. pruefsummen.txt muss die Fassung nennen, unter der sie liegt — eine
//     alte, echt unterschriebene Veröffentlichung unter neuem Tag zählt
//     nicht.
//  3. Die Prüfsumme des Installers muss zu der in pruefsummen.txt passen.
//
// Erst dann liegt der Installer bereit, und gestartet wird er nur auf
// Knopfdruck — er zeigt dann seine eigenen Seiten.

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// signierSchluessel: derselbe wie in server/internal/update. Eine
// Variable nur, damit Tests einen eigenen einsetzen können.
var signierSchluessel = "YqAofSZ1cBwbDGujXUZ4I7ufwml5UKphGbgawreXiHM="

const fassungsPraefix = "# lumen-version:"

// Installer, die grösser sind, sind keine.
const maxInstaller = 200 << 20

// InstallerName ist der Name, unter dem release.yml den Installer
// anhängt: Lumen-Setup-0.4.0-windows-x64.exe.
func InstallerName(v string) string {
	return "Lumen-Setup-" + strings.TrimPrefix(v, "v") + "-windows-x64.exe"
}

type Aktualisierer struct {
	Repo    string
	Version string // die laufende, ohne „v"
	API     string // zum Testen; sonst https://api.github.com
	HTTP    *http.Client
}

// Veroeffentlichung ist die neueste Fassung auf GitHub.
type Veroeffentlichung struct {
	Version string            `json:"version"`
	Tag     string            `json:"tag"`
	Seite   string            `json:"seite"`
	dateien map[string]string // Name → Adresse zum Laden
}

func (u *Aktualisierer) client() *http.Client {
	if u.HTTP != nil {
		return u.HTTP
	}
	return &http.Client{Timeout: 60 * time.Second}
}

func (u *Aktualisierer) holen(ctx context.Context, adresse, accept string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", adresse, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("User-Agent", "LumenDesktop/"+u.Version)
	resp, err := u.client().Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		resp.Body.Close()
		return nil, fmt.Errorf("GitHub antwortet mit %d", resp.StatusCode)
	}
	return resp, nil
}

// Neueste fragt GitHub nach der neuesten Veröffentlichung.
func (u *Aktualisierer) Neueste(ctx context.Context) (*Veroeffentlichung, error) {
	api := u.API
	if api == "" {
		api = "https://api.github.com"
	}
	resp, err := u.holen(ctx, api+"/repos/"+u.Repo+"/releases/latest", "application/vnd.github+json")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var d struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
		Assets  []struct {
			Name string `json:"name"`
			DL   string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&d); err != nil {
		return nil, err
	}
	v := &Veroeffentlichung{Version: strings.TrimPrefix(d.TagName, "v"), Tag: d.TagName,
		Seite: d.HTMLURL, dateien: map[string]string{}}
	for _, a := range d.Assets {
		v.dateien[a.Name] = a.DL
	}
	return v, nil
}

// HatInstaller sagt, ob die Veröffentlichung einen Windows-Installer hat.
// Ältere haben keinen; dann gibt es auch nichts anzubieten.
func (v *Veroeffentlichung) HatInstaller() bool {
	_, ok := v.dateien[InstallerName(v.Version)]
	return ok
}

// InstallerLaden holt den Installer in den Ordner und prüft ihn. Zurück
// kommt der Pfad der geprüften Datei.
func (u *Aktualisierer) InstallerLaden(ctx context.Context, v *Veroeffentlichung, ordner string) (string, error) {
	name := InstallerName(v.Version)
	adresse, ok := v.dateien[name]
	if !ok {
		return "", fmt.Errorf("%s hat keinen Windows-Installer", v.Tag)
	}
	summenAdr, ok1 := v.dateien["pruefsummen.txt"]
	sigAdr, ok2 := v.dateien["pruefsummen.txt.sig"]
	if !ok1 || !ok2 {
		return "", errors.New("der Veröffentlichung fehlen Prüfsummen oder Unterschrift")
	}
	summen, err := u.klein(ctx, summenAdr)
	if err != nil {
		return "", err
	}
	sig, err := u.klein(ctx, sigAdr)
	if err != nil {
		return "", err
	}
	if err := pruefeSummen(summen, sig, v.Tag); err != nil {
		return "", err
	}
	erwartet, ok := summeFuer(summen, name)
	if !ok {
		return "", fmt.Errorf("in pruefsummen.txt steht nichts zu %s", name)
	}

	if err := os.MkdirAll(ordner, 0o700); err != nil {
		return "", err
	}
	ziel := filepath.Join(ordner, name)
	teil := ziel + ".teil"
	_ = os.Remove(teil)
	resp, err := u.holen(ctx, adresse, "application/octet-stream")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	f, err := os.OpenFile(teil, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, maxInstaller+1))
	if err == nil && n > maxInstaller {
		err = errors.New("der Installer ist unerwartet gross")
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil && hex.EncodeToString(h.Sum(nil)) != erwartet {
		err = errors.New("die Prüfsumme des Installers stimmt nicht")
	}
	if err != nil {
		_ = os.Remove(teil)
		return "", err
	}
	_ = os.Remove(ziel)
	if err := os.Rename(teil, ziel); err != nil {
		return "", err
	}
	return ziel, nil
}

func (u *Aktualisierer) klein(ctx context.Context, adresse string) ([]byte, error) {
	resp, err := u.holen(ctx, adresse, "application/octet-stream")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}

// pruefeSummen: Unterschrift gültig und für genau diese Fassung.
func pruefeSummen(summen, sigB64 []byte, tag string) error {
	if !unterschrieben(summen, sigB64) {
		return errors.New("die Unterschrift der Veröffentlichung stimmt nicht")
	}
	v := ""
	for _, z := range strings.Split(string(summen), "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(z), fassungsPraefix); ok {
			v = strings.TrimSpace(rest)
			break
		}
	}
	ohneV := func(s string) string { return strings.TrimPrefix(strings.TrimSpace(s), "v") }
	if v == "" || ohneV(tag) == "" || ohneV(v) != ohneV(tag) {
		return fmt.Errorf("die Unterschrift gilt für Fassung %q, die Veröffentlichung heisst %q", v, tag)
	}
	return nil
}

func unterschrieben(inhalt, sigB64 []byte) bool {
	pub, err := base64.StdEncoding.DecodeString(signierSchluessel)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return false
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sigB64)))
	if err != nil || len(sig) != ed25519.SignatureSize {
		return false
	}
	return ed25519.Verify(ed25519.PublicKey(pub), inhalt, sig)
}

// summeFuer sucht in pruefsummen.txt (Format von sha256sum) die Zeile
// zu name.
func summeFuer(summen []byte, name string) (string, bool) {
	for _, z := range strings.Split(string(summen), "\n") {
		f := strings.Fields(z)
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name {
			return strings.ToLower(f[0]), true
		}
	}
	return "", false
}

// Neuer sagt, ob a neuer ist als b — nach Zahlen: 0.1.10 > 0.1.9. „dev"
// oder Leeres ist nie neuer und nie älter.
func Neuer(a, b string) bool {
	if a == "" || b == "" || a == "dev" || b == "dev" {
		return false
	}
	pa, pb := fassungsTeile(a), fassungsTeile(b)
	for i := range pa {
		if pa[i] != pb[i] {
			return pa[i] > pb[i]
		}
	}
	return false
}

func fassungsTeile(v string) [3]int {
	var out [3]int
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	for i, t := range strings.SplitN(v, ".", 3) {
		n, _ := strconv.Atoi(strings.TrimFunc(t, func(r rune) bool { return r < '0' || r > '9' }))
		out[i] = n
	}
	return out
}

// ---------------------------------------------------------------- Stand

// updateDatei merkt sich, wann zuletzt gefragt wurde und was es gab —
// so fragt Lumen höchstens einmal am Tag.
type updateDatei struct {
	Geprueft        time.Time `json:"geprueft"`
	Neueste         string    `json:"neueste,omitempty"`
	Seite           string    `json:"seite,omitempty"`
	AusgeblendetBis time.Time `json:"ausgeblendetBis,omitempty"`
}

// UpdateStand ist, was die Seite über ein Update weiss.
type UpdateStand struct {
	Verfuegbar bool   `json:"verfuegbar"`
	Version    string `json:"version,omitempty"`
	Seite      string `json:"seite,omitempty"`
	// Phase: angeboten, laedt, bereit, fehler.
	Phase  string `json:"phase,omitempty"`
	Fehler string `json:"fehler,omitempty"`
	pfad   string
}

// faellig sagt, ob wieder bei GitHub gefragt werden darf.
func (d updateDatei) faellig(jetzt time.Time) bool {
	return d.Geprueft.IsZero() || jetzt.Sub(d.Geprueft) >= 24*time.Hour || jetzt.Before(d.Geprueft)
}
