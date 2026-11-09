// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

// Package update hält Lumen auf dem neuesten Stand: nachsehen, ob es auf
// GitHub eine neuere Fassung gibt, und sie einsetzen.
//
// Einsetzen heisst: die Unterschrift unter pruefsummen.txt prüfen, das
// Paket für diese Plattform laden, die Prüfsumme gegen pruefsummen.txt
// halten, die Binärdatei neben die laufende legen, umbenennen, neu
// starten.
//
// Die Unterschrift ist der eigentliche Schutz. Die Prüfsumme allein kommt
// aus derselben Veröffentlichung wie das Paket — wer dort ein Paket
// austauschen kann (etwa mit einem gestohlenen GitHub-Token), tauscht die
// Prüfsumme gleich mit. Die Unterschrift dagegen entsteht mit einem
// Schlüssel, der nur im Veröffentlichungs-Ablauf liegt; der öffentliche
// Gegenpart steht unten fest im Code. Ohne gültige Unterschrift setzt
// Lumen nichts ein. Geht irgendwo etwas schief, bleibt die alte
// Datei unberührt — ein halb geschriebenes Lumen gibt es nicht.
//
// Im Container geht das nicht: dort ist die Binärdatei Teil des Abbilds,
// und das Abbild tauscht man als Ganzes. Lumen sagt dann, dass es eine
// neue Fassung gibt, und wie man sie holt.
package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
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
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Release ist eine Veröffentlichung auf GitHub, soweit es hier zählt.
type Release struct {
	Version   string    `json:"version"` // ohne „v"
	Tag       string    `json:"tag"`
	URL       string    `json:"url"` // die Seite zum Lesen
	Published time.Time `json:"published"`
	Notizen   string    `json:"notizen,omitempty"`
	assets    map[string]asset
}

type asset struct {
	name, browserURL, apiURL string
	size                     int64
}

type Pruefer struct {
	Repo    string // owner/name
	Version string // die laufende, ohne „v"
	Token   string // nur bei einem privaten Repository (etwa einem privaten Fork); sonst leer
	HTTP    *http.Client
	API     string // zum Testen; sonst https://api.github.com
}

func (p *Pruefer) api() string {
	if p.API != "" {
		return p.API
	}
	return "https://api.github.com"
}

func (p *Pruefer) client() *http.Client {
	if p.HTTP != nil {
		return p.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func (p *Pruefer) anfrage(ctx context.Context, url, accept string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("User-Agent", "Lumen/"+p.Version)
	if p.Token != "" {
		req.Header.Set("Authorization", "Bearer "+p.Token)
	}
	resp, err := p.client().Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == 404 && p.Token == "" {
		resp.Body.Close()
		// Ein öffentliches Repository braucht kein Token. 404 heisst dann:
		// noch keine Veröffentlichung — oder doch ein privates (Fork).
		return nil, errors.New("Auf GitHub gibt es zu " + p.Repo + " noch keine Veröffentlichung. " +
			"Ist das Repository privat (etwa ein eigener Fork), braucht Lumen ein Token (LUMEN_GITHUB_TOKEN)")
	}
	// 403 mit aufgebrauchtem Kontingent ist kein kaputtes Token, sondern
	// „zu oft gefragt" — das soll nicht als „Token ungültig" erscheinen.
	if resp.StatusCode == 403 || resp.StatusCode == 429 {
		if resp.Header.Get("X-RateLimit-Remaining") == "0" || resp.StatusCode == 429 {
			resp.Body.Close()
			return nil, errors.New("GitHub lässt gerade keine weiteren Abfragen zu (Kontingent aufgebraucht) — in einer Stunde nochmal")
		}
	}
	if resp.StatusCode == 401 || resp.StatusCode == 403 || (resp.StatusCode == 404 && p.Token != "") {
		resp.Body.Close()
		// Was die Antwort heisst, genauer sagen: 401 = abgelaufen oder
		// falsch abgeschrieben; 404 mit Token = gültig, sieht aber das
		// Repository nicht (beim Erstellen nicht ausgewählt).
		grund := ""
		switch resp.StatusCode {
		case 401:
			grund = "Das Token ist abgelaufen oder falsch eingefügt."
		case 404:
			grund = "Das Token gilt, sieht das Repository aber nicht: Beim Erstellen unter „Repository access“ das Repository " +
				p.Repo + " auswählen und bei „Contents“ Lesezugriff geben — oder ein klassisches Token mit dem Bereich „repo“ nehmen."
		}
		return nil, fmt.Errorf("%w (GitHub antwortet %d). %s", ErrTokenUngueltig, resp.StatusCode, grund)
	}
	if resp.StatusCode/100 != 2 {
		resp.Body.Close()
		return nil, fmt.Errorf("GitHub antwortet mit %d", resp.StatusCode)
	}
	return resp, nil
}

// ErrTokenUngueltig: GitHub nimmt das Token nicht an — meist ist es
// abgelaufen oder hat keinen Zugriff auf das Repository.
var ErrTokenUngueltig = errors.New("GitHub nimmt das Token nicht an — es ist abgelaufen oder hat keinen Zugriff auf das Repository. " +
	"Wer nicht Besitzer des Repositorys ist, braucht ein klassisches Token mit dem Bereich „repo“ (feingranulare Tokens gelten nur für eigene Repositories). " +
	"In der Verwaltung unter „Updates“ ein neues eintragen")

// Neueste fragt GitHub nach der neuesten Veröffentlichung.
func (p *Pruefer) Neueste(ctx context.Context) (*Release, error) {
	resp, err := p.anfrage(ctx, p.api()+"/repos/"+p.Repo+"/releases/latest", "application/vnd.github+json")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var d struct {
		TagName     string    `json:"tag_name"`
		HTMLURL     string    `json:"html_url"`
		PublishedAt time.Time `json:"published_at"`
		Body        string    `json:"body"`
		Assets      []struct {
			Name string `json:"name"`
			URL  string `json:"url"`
			DL   string `json:"browser_download_url"`
			Size int64  `json:"size"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&d); err != nil {
		return nil, err
	}
	r := &Release{Version: strings.TrimPrefix(d.TagName, "v"), Tag: d.TagName, URL: d.HTMLURL,
		Published: d.PublishedAt, Notizen: d.Body, assets: map[string]asset{}}
	for _, a := range d.Assets {
		r.assets[a.Name] = asset{name: a.Name, browserURL: a.DL, apiURL: a.URL, size: a.Size}
	}
	return r, nil
}

// Neuer sagt, ob a neuer ist als b — nach Zahlen, nicht nach Buchstaben:
// 0.1.10 ist neuer als 0.1.9. „dev" ist nie neuer und nie älter: wer
// aus dem Quelltext baut, bekommt keine Aufforderung.
func Neuer(a, b string) bool {
	if a == "dev" || b == "dev" || a == "" || b == "" {
		return false
	}
	pa, pb := teile(a), teile(b)
	for i := 0; i < 3; i++ {
		if pa[i] != pb[i] {
			return pa[i] > pb[i]
		}
	}
	return false
}

func teile(v string) [3]int {
	var out [3]int
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	for i, t := range strings.SplitN(v, ".", 3) {
		if i >= 3 {
			break
		}
		n, _ := strconv.Atoi(strings.TrimFunc(t, func(r rune) bool { return r < '0' || r > '9' }))
		out[i] = n
	}
	return out
}

// Paketname für diese Plattform, so wie die Veröffentlichung ihn nennt.
func Paketname() string {
	ext := ".tar.gz"
	if runtime.GOOS == "windows" {
		ext = ".zip"
	}
	return "lumen-" + runtime.GOOS + "-" + runtime.GOARCH + ext
}

// Herunterladen holt das Paket für diese Plattform in den Ordner dir,
// prüft die Prüfsumme und packt die Binärdatei aus. Zurück kommt der
// Pfad der ausgepackten Datei.
func (p *Pruefer) Herunterladen(ctx context.Context, r *Release, dir string) (string, error) {
	a, ok := r.assets[Paketname()]
	if !ok {
		return "", fmt.Errorf("für %s/%s gibt es in %s kein Paket", runtime.GOOS, runtime.GOARCH, r.Tag)
	}
	summen, ok := r.assets["pruefsummen.txt"]
	if !ok {
		return "", errors.New("der Veröffentlichung fehlen die Prüfsummen — so wird nichts eingesetzt")
	}

	sig, ok := r.assets["pruefsummen.txt.sig"]
	if !ok {
		return "", errors.New("die Veröffentlichung ist nicht unterschrieben — so wird nichts eingesetzt")
	}

	erwartet, err := p.pruefsumme(ctx, r, summen, sig, a.name)
	if err != nil {
		return "", err
	}

	paket := filepath.Join(dir, a.name)
	if err := p.laden(ctx, a, paket); err != nil {
		return "", err
	}
	ist, err := sha256Datei(paket)
	if err != nil {
		return "", err
	}
	if ist != erwartet {
		_ = os.Remove(paket)
		return "", errors.New("die Prüfsumme des Pakets stimmt nicht — es wird nicht eingesetzt")
	}

	bin := filepath.Join(dir, "lumen-neu"+exeExt())
	if err := auspacken(paket, "lumen"+exeExt(), bin); err != nil {
		return "", err
	}
	_ = os.Remove(paket)
	return bin, nil
}

func (p *Pruefer) pruefsumme(ctx context.Context, r *Release, summen, sig asset, name string) (string, error) {
	var buf bytes.Buffer
	raw, err := p.ladenKlein(ctx, summen)
	if err != nil {
		return "", err
	}
	sigRaw, err := p.ladenKlein(ctx, sig)
	if err != nil {
		return "", err
	}
	if err := PruefeSummen(raw, sigRaw, r.Tag); err != nil {
		return "", err
	}
	buf.Write(raw)
	for _, zeile := range strings.Split(buf.String(), "\n") {
		f := strings.Fields(zeile)
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name {
			return strings.ToLower(f[0]), nil
		}
	}
	return "", fmt.Errorf("in pruefsummen.txt steht nichts zu %s", name)
}

// laden holt eine Datei — ohne Token über die gewöhnliche Adresse (der
// Normalfall, das Repository ist öffentlich), mit Token über die API
// (bei einem privaten Fork der einzige Weg).
func (p *Pruefer) laden(ctx context.Context, a asset, ziel string) error {
	url, accept := a.browserURL, "application/octet-stream"
	if p.Token != "" && a.apiURL != "" {
		url = a.apiURL
	}
	resp, err := p.anfrage(ctx, url, accept)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	// O_EXCL: Liegt dort schon etwas (auch ein Verweis, den jemand
	// hingelegt hat), wird nicht hineingeschrieben.
	_ = os.Remove(ziel)
	f, err := os.OpenFile(ziel, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	// Ein Lumen-Paket ist ein paar Dutzend MB gross; mehr als 300 MB ist
	// kein Paket, sondern ein Versuch, die Platte zu füllen.
	n, err := io.Copy(f, io.LimitReader(resp.Body, maxPaket+1))
	if err == nil && n > maxPaket {
		err = errors.New("das paket ist unerwartet gross")
	}
	if err != nil {
		f.Close()
		_ = os.Remove(ziel)
		return err
	}
	return f.Close()
}

const maxPaket = 300 << 20

// ladenKlein holt eine kleine Datei (Prüfsummen, Unterschrift) in den
// Speicher, ohne Umweg über eine Datei mit vorhersagbarem Namen.
func (p *Pruefer) ladenKlein(ctx context.Context, a asset) ([]byte, error) {
	url, accept := a.browserURL, "application/octet-stream"
	if p.Token != "" && a.apiURL != "" {
		url = a.apiURL
	}
	resp, err := p.anfrage(ctx, url, accept)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}

// signierSchluessel ist der öffentliche Teil des Schlüssels, mit dem jede
// Veröffentlichung unterschrieben wird (cmd/lumen-signieren).
// (Eine Variable nur, damit Tests einen eigenen Schlüssel einsetzen können.)
var signierSchluessel = "YqAofSZ1cBwbDGujXUZ4I7ufwml5UKphGbgawreXiHM="

// FassungsPraefix leitet die Zeile in pruefsummen.txt ein, die sagt, zu
// welcher Fassung die Datei gehört:
//
//	# lumen-version: v0.1.20
//
// Warum: Die Unterschrift deckt pruefsummen.txt. Stünde die Fassung nicht
// darin, liesse sich eine ältere, echt unterschriebene Veröffentlichung
// samt Unterschrift unter einem neuen Tag noch einmal hochladen — und
// jeder Server spielte die alte Fassung (mit ihren alten Fehlern) als
// „Update" ein. Mit der Zeile gehört die Unterschrift zu genau einer
// Fassung.
//
// Ältere Lumen-Fassungen lesen pruefsummen.txt Zeile für Zeile und nehmen
// nur Zeilen mit genau zwei Feldern, deren zweites der Paketname ist. Die
// Kommentarzeile hat drei Felder und fällt dort durch — sie prüfen die
// Unterschrift (über die ganze Datei, Zeile eingeschlossen) und setzen
// neue Veröffentlichungen weiter ein.
const FassungsPraefix = "# lumen-version:"

// SignierteFassung liest die Fassung aus pruefsummen.txt — leer, wenn
// keine Zeile sie nennt (Veröffentlichungen vor dieser Regel).
func SignierteFassung(summen []byte) string {
	for _, zeile := range strings.Split(string(summen), "\n") {
		zeile = strings.TrimSpace(zeile)
		if v, ok := strings.CutPrefix(zeile, FassungsPraefix); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// PruefeSummen prüft pruefsummen.txt einer Veröffentlichung: Die
// Unterschrift muss vom eingebauten Schlüssel stammen, und die
// unterschriebene Fassung muss die sein, unter der die Veröffentlichung
// steht (tag, mit oder ohne „v").
//
// Fehlt die Fassungszeile, wird nichts eingesetzt. Das bricht nichts: Ein
// Server mit diesem Code kommt aus einer Veröffentlichung, die die Zeile
// schon trägt, und aktualisiert nur auf neuere — die tragen sie auch.
// Fehlt sie, ist es eine ältere Veröffentlichung unter neuem Namen.
func PruefeSummen(summen, sigB64 []byte, tag string) error {
	if !Unterschrieben(summen, sigB64) {
		return errors.New("die Unterschrift der Veröffentlichung stimmt nicht — es wird nichts eingesetzt")
	}
	v := SignierteFassung(summen)
	if v == "" {
		return errors.New("pruefsummen.txt nennt keine Fassung — womöglich eine ältere Veröffentlichung unter neuem Namen; es wird nichts eingesetzt")
	}
	ohneV := func(s string) string { return strings.TrimPrefix(strings.TrimSpace(s), "v") }
	if ohneV(tag) == "" || ohneV(v) != ohneV(tag) {
		return fmt.Errorf("die Unterschrift gilt für Fassung %s, die Veröffentlichung heisst aber %s — es wird nichts eingesetzt", v, tag)
	}
	return nil
}

// Unterschrieben prüft die Unterschrift (Base64) über inhalt.
func Unterschrieben(inhalt, sigB64 []byte) bool {
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

func sha256Datei(pfad string) (string, error) {
	f, err := os.Open(pfad)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func exeExt() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

// auspacken holt genau eine Datei aus dem Paket — die Binärdatei. Alles
// andere (Lizenz, Lesemich) liegt beim Benutzer schon.
func auspacken(paket, gesucht, ziel string) error {
	schreiben := func(r io.Reader) error {
		f, err := os.OpenFile(ziel, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
		if err != nil {
			return err
		}
		// Auch ausgepackt höchstens 300 MB — ein Paket, das beim Auspacken
		// wächst und wächst, ist kein Lumen.
		n, err := io.Copy(f, io.LimitReader(r, maxPaket+1))
		if err == nil && n > maxPaket {
			err = errors.New("die ausgepackte datei ist unerwartet gross")
		}
		if err != nil {
			f.Close()
			_ = os.Remove(ziel)
			return err
		}
		return f.Close()
	}
	if strings.HasSuffix(paket, ".zip") {
		z, err := zip.OpenReader(paket)
		if err != nil {
			return err
		}
		defer z.Close()
		for _, f := range z.File {
			if filepath.Base(f.Name) == gesucht && !f.FileInfo().IsDir() {
				rc, err := f.Open()
				if err != nil {
					return err
				}
				defer rc.Close()
				return schreiben(rc)
			}
		}
		return fmt.Errorf("im Paket liegt kein %s", gesucht)
	}
	f, err := os.Open(paket)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if h.Typeflag == tar.TypeReg && filepath.Base(h.Name) == gesucht {
			return schreiben(tr)
		}
	}
	return fmt.Errorf("im Paket liegt kein %s", gesucht)
}

// eigenerPfad wird beim Start festgehalten. Nach dem Umbenennen zeigt
// /proc/self/exe nämlich auf die beiseitegelegte alte Datei — und ein
// Neustart darüber würde die alte Fassung starten.
var eigenerPfad, _ = os.Executable()

// Einsetzen ersetzt die laufende Binärdatei durch die neue. Die alte
// bleibt als lumen.alt liegen — für den Fall, dass die neue nicht
// startet.
func Einsetzen(neu string) (alt string, err error) {
	self := eigenerPfad
	if self == "" {
		return "", errors.New("der eigene Pfad ist unbekannt")
	}
	if self, err = filepath.EvalSymlinks(self); err != nil {
		return "", err
	}
	// Muss auf derselben Platte liegen, sonst geht das Umbenennen nicht.
	nebenan := self + ".neu"
	if err := kopieren(neu, nebenan); err != nil {
		return "", err
	}
	alt = self + ".alt"
	_ = os.Remove(alt)
	if err := os.Rename(self, alt); err != nil {
		_ = os.Remove(nebenan)
		return "", fmt.Errorf("die laufende Datei lässt sich nicht beiseitelegen: %w", err)
	}
	if err := os.Rename(nebenan, self); err != nil {
		// Zurück, wie es war.
		_ = os.Rename(alt, self)
		return "", fmt.Errorf("die neue Datei lässt sich nicht einsetzen: %w", err)
	}
	return alt, nil
}

func kopieren(von, nach string) error {
	in, err := os.Open(von)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(nach, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// Neustart startet Lumen neu — mit denselben Argumenten. Auf Unix wird
// der laufende Prozess ersetzt; auf Windows startet ein neuer, und dieser
// hier geht.
func Neustart() error {
	self := eigenerPfad
	if self == "" {
		return errors.New("der eigene Pfad ist unbekannt")
	}
	if runtime.GOOS == "windows" {
		cmd := exec.Command(self, os.Args[1:]...)
		cmd.Stdout, cmd.Stderr, cmd.Stdin = os.Stdout, os.Stderr, os.Stdin
		if err := cmd.Start(); err != nil {
			return err
		}
		os.Exit(0)
		return nil
	}
	return neuAusfuehren(self, os.Args, os.Environ())
}
