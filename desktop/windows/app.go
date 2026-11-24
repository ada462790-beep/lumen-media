// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// seitenAdresse ist die eingebaute Seite „Server hinzufügen". Sie kommt
// nicht aus dem Netz: Anfragen an diesen Host beantwortet die App selbst
// (seite.go). .localhost geht nach RFC 6761 nie ins Netz hinaus, und für
// den Browser ist es ein sicherer Ursprung.
const seitenHost = "lumen.localhost"
const seitenAdresse = "http://" + seitenHost + "/"

// Host ist, was die App vom Fenster braucht. Unter Windows ist das
// WebView2 (fenster_windows.go), in den Tests eine Attrappe.
type Host interface {
	// Auftrag führt f im Fenster-Thread aus. Alles, was WebView2 anfasst,
	// muss dort laufen.
	Auftrag(f func())
	// Die folgenden nur im Fenster-Thread aufrufen.
	Navigieren(adresse string)
	Skript(js string)
	Extern(adresse string)
	InstallerStarten(pfad string) error
	Schliessen()
	// TonausgabeErlauben erlaubt (an) oder nimmt zurück, dass die Seite
	// dieses Ursprungs die Audio-Ausgabegeräte sieht und wählt
	// (tonausgabe.go). fertig kommt im Fenster-Thread.
	TonausgabeErlauben(ursprung string, an bool, fertig func(error))
	// Der eigene Player (player.go, player_windows.go). PlayerDa sagt, ob
	// libmpv bereitliegt; ohne spielt die Web-App wie bisher selbst.
	PlayerDa() bool
	PlayerAbspielen(ursprung string, a PlayerAuftrag) error
	PlayerBefehl(ursprung string, b PlayerBefehl) error
}

type App struct {
	host     Host
	liste    *Serverliste
	upd      *Aktualisierer
	http     *http.Client
	ordner   string // %APPDATA%\Lumen
	lokal    string // %LOCALAPPDATA%\Lumen
	keinNetz bool   // Selbsttest: nicht bei GitHub fragen

	mu    sync.Mutex
	stand UpdateStand
	veroe *Veroeffentlichung
}

func NeueApp(host Host, ordner, lokal string) (*App, error) {
	liste, err := LadeServerliste(filepath.Join(ordner, "server.json"))
	if err != nil {
		// Eine kaputte Datei soll den Start nicht verhindern: Dann eben
		// mit leerer Liste, und die Datei wird beim nächsten Merken neu
		// geschrieben.
		log.Printf("Serverliste unlesbar, beginne leer: %v", err)
	}
	return &App{
		host:   host,
		liste:  liste,
		ordner: ordner,
		lokal:  lokal,
		http:   &http.Client{Timeout: 10 * time.Second},
		upd:    &Aktualisierer{Repo: repository, Version: version},
	}, nil
}

// StartAdresse: der zuletzt gewählte Server, sonst die Auswahl.
func (a *App) StartAdresse() string {
	if s, ok := a.liste.AktiverServer(); ok {
		return s.Adresse + "/"
	}
	return seitenAdresse
}

// ---------------------------------------------------------------- Navigation

// Weg sagt, was mit einer Navigation geschehen soll.
type Weg int

const (
	WegErlauben Weg = iota // im Fenster laden
	WegExtern              // im Standardbrowser öffnen
	WegSperren             // gar nicht
)

// Wohin entscheidet für eine Navigation im Hauptfenster: Die Web-App des
// gewählten Servers und die eingebaute Seite bleiben im Fenster; alles
// andere — IMDb, TMDb, GitHub, eine Anleitung — öffnet der Standardbrowser.
// So kann eine fremde Seite nie an die Brücke zur App (bruecke.js).
func (a *App) Wohin(adresse string) Weg {
	u, err := url.Parse(adresse)
	if err != nil {
		return WegSperren
	}
	switch strings.ToLower(u.Scheme) {
	case "about", "data", "blob":
		return WegErlauben
	case "http", "https":
	case "mailto":
		return WegExtern
	default:
		return WegSperren
	}
	if strings.EqualFold(u.Host, seitenHost) {
		return WegErlauben
	}
	if _, ok := a.liste.Gehoert(u); ok {
		return WegErlauben
	}
	return WegExtern
}

// eingebaut sagt, ob eine Nachricht von der eingebauten Seite kommt. Nur
// sie darf die Serverliste sehen und ändern.
func eingebaut(quelle string) bool {
	u, err := url.Parse(quelle)
	return err == nil && u.Scheme == "http" && strings.EqualFold(u.Host, seitenHost)
}

// LadenFehlgeschlagen: Die Web-App eines Servers liess sich nicht laden.
// Statt der Fehlerseite von Chromium erscheint die Auswahl mit einem Satz
// dazu und „Erneut versuchen".
func (a *App) LadenFehlgeschlagen(adresse, art string) {
	u, err := url.Parse(adresse)
	if err != nil {
		return
	}
	s, ok := a.liste.Gehoert(u)
	if !ok {
		return
	}
	q := url.Values{"fehler": {art}, "id": {s.ID}}
	a.host.Navigieren(seitenAdresse + "?" + q.Encode())
}

// ServerWechseln zeigt die Auswahl (Strg+Umschalt+S, Systemmenü oder
// „Server wechseln" in der Web-App).
func (a *App) ServerWechseln() {
	a.host.Navigieren(seitenAdresse + "?wechseln=1")
}

// ---------------------------------------------------------------- Brücke

type nachricht struct {
	ID    int             `json:"id"`
	Art   string          `json:"art"`
	Daten json.RawMessage `json:"daten"`
}

type antwort struct {
	ID     int    `json:"id"`
	OK     bool   `json:"ok"`
	Daten  any    `json:"daten,omitempty"`
	Fehler string `json:"fehler,omitempty"`
}

// Nachricht bearbeitet, was bruecke.js schickt. Läuft im Fenster-Thread;
// was dauert (Netz), läuft nebenher und antwortet über Auftrag.
func (a *App) Nachricht(quelle, roh string) {
	var n nachricht
	if err := json.Unmarshal([]byte(roh), &n); err != nil || n.Art == "" {
		return
	}
	intern := eingebaut(quelle)
	antworte := func(daten any, err error) {
		an := antwort{ID: n.ID, OK: err == nil, Daten: daten}
		if err != nil {
			an.Fehler = fehlerCode(err)
		}
		b, _ := json.Marshal(an)
		js := "window.__lumenAntwort&&window.__lumenAntwort(" + string(b) + ")"
		a.host.Auftrag(func() { a.host.Skript(js) })
	}
	var d struct {
		Adresse string `json:"adresse"`
		ID      string `json:"id"`
		An      bool   `json:"an"`
	}
	_ = json.Unmarshal(n.Daten, &d)

	switch n.Art {
	// Für jede Seite, auch die Web-App eines Servers.
	case "info":
		antworte(map[string]any{"version": version, "plattform": "windows"}, nil)
	case "playerAbspielen", "playerBefehl":
		// Wie die Tonausgabe: nur die Web-App eines gemerkten Servers, und
		// mpv holt nur von genau diesem Server (PlayerAuftrag.Adresse).
		ursprung, ok := a.serverUrsprung(quelle)
		if !ok {
			antworte(nil, codeFehler{fehlerNichtErlaub})
			return
		}
		if n.Art == "playerAbspielen" {
			var auftrag PlayerAuftrag
			if err := json.Unmarshal(n.Daten, &auftrag); err != nil {
				antworte(nil, codeFehler{fehlerUnbekannt})
				return
			}
			err := a.host.PlayerAbspielen(ursprung, auftrag)
			if err != nil {
				log.Printf("Player: %v", err)
			}
			antworte(nil, err)
			return
		}
		var b PlayerBefehl
		if err := json.Unmarshal(n.Daten, &b); err != nil {
			antworte(nil, codeFehler{fehlerUnbekannt})
			return
		}
		antworte(nil, a.host.PlayerBefehl(ursprung, b))
	case "serverWechseln":
		antworte(nil, nil)
		a.host.Auftrag(a.ServerWechseln)
	case "extern":
		if w := a.Wohin(d.Adresse); w == WegExtern || (w == WegErlauben && strings.HasPrefix(d.Adresse, "http")) {
			adr := d.Adresse
			a.host.Auftrag(func() { a.host.Extern(adr) })
		}
		antworte(nil, nil)
	case "updateStand":
		antworte(a.updateStand(), nil)
	case "updateLaden":
		antworte(nil, nil)
		go a.updateLaden()
	case "updateStarten":
		antworte(nil, nil)
		a.host.Auftrag(a.updateStarten)
	case "updateSpaeter":
		antworte(nil, nil)
		a.updateSpaeter()
	case "tonausgabe":
		// Nur die Web-App eines gemerkten Servers, für ihren eigenen
		// Ursprung — nie die eingebaute Seite, nie eine fremde.
		ursprung, ok := a.serverUrsprung(quelle)
		if !ok {
			antworte(nil, codeFehler{fehlerNichtErlaub})
			return
		}
		an := d.An
		a.host.Auftrag(func() {
			a.host.TonausgabeErlauben(ursprung, an, func(err error) {
				if err != nil {
					log.Printf("Tonausgabe für %s: %v", ursprung, err)
				}
				antworte(nil, err)
			})
		})

	// Nur für die eingebaute Seite.
	default:
		if !intern {
			antworte(nil, codeFehler{fehlerNichtErlaub})
			return
		}
		a.intern(n.Art, d.Adresse, d.ID, antworte)
	}
}

func (a *App) intern(art, adresse, id string, antworte func(any, error)) {
	switch art {
	case "liste":
		server, aktiv := a.liste.Kopie()
		antworte(map[string]any{"server": server, "aktiv": aktiv, "version": version}, nil)
	case "pruefen":
		go func() {
			g, err := Pruefen(context.Background(), a.http, adresse)
			antworte(g, err)
		}()
	case "hinzufuegen":
		go func() {
			g, err := Pruefen(context.Background(), a.http, adresse)
			if err != nil {
				antworte(nil, err)
				return
			}
			s, err := a.liste.Merken(Server{Adresse: g.Adresse, Name: g.Name, Version: g.Version})
			if err != nil {
				log.Printf("Serverliste nicht gespeichert: %v", err)
			}
			antworte(s, nil)
			// Die Seite zeigt kurz, was gefunden wurde, und lädt dann
			// selbst (waehlen) — so sieht man den Namen einen Moment.
		}()
	case "waehlen":
		s, ok, err := a.liste.Waehlen(id)
		if err != nil {
			log.Printf("Serverliste nicht gespeichert: %v", err)
		}
		if !ok {
			antworte(nil, codeFehler{fehlerUnbekannt})
			return
		}
		antworte(s, nil)
		a.host.Auftrag(func() { a.host.Navigieren(s.Adresse + "/") })
	case "entfernen":
		_, err := a.liste.Entfernen(id)
		if err != nil {
			log.Printf("Serverliste nicht gespeichert: %v", err)
		}
		server, aktiv := a.liste.Kopie()
		antworte(map[string]any{"server": server, "aktiv": aktiv, "version": version}, nil)
	default:
		antworte(nil, codeFehler{fehlerUnbekannt})
	}
}

// ---------------------------------------------------------------- Update

func (a *App) updatePfad() string { return filepath.Join(a.ordner, "update.json") }

// UpdatePruefen läuft nebenher beim Start. Höchstens einmal am Tag fragt
// es GitHub; dazwischen gilt, was beim letzten Mal herauskam.
func (a *App) UpdatePruefen(ctx context.Context) {
	if a.keinNetz || version == "dev" {
		return
	}
	var d updateDatei
	_ = jsonLesen(a.updatePfad(), &d)
	jetzt := time.Now()
	if d.faellig(jetzt) {
		v, err := a.upd.Neueste(ctx)
		d.Geprueft = jetzt.UTC()
		if err != nil {
			log.Printf("Update-Prüfung: %v", err)
		} else if v.HatInstaller() {
			d.Neueste, d.Seite = v.Version, v.Seite
		} else {
			d.Neueste, d.Seite = "", ""
		}
		if err := jsonSchreiben(a.updatePfad(), d); err != nil {
			log.Printf("Update-Stand nicht gespeichert: %v", err)
		}
	}
	if !Neuer(d.Neueste, version) || jetzt.Before(d.AusgeblendetBis) {
		return
	}
	a.mu.Lock()
	a.stand = UpdateStand{Verfuegbar: true, Version: d.Neueste, Seite: d.Seite, Phase: "angeboten"}
	a.mu.Unlock()
	a.updateMelden()
}

func (a *App) updateStand() UpdateStand {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.stand
}

// updateMelden schickt den Stand an die Seite, die gerade offen ist.
// Seiten, die später laden, fragen selbst (updateStand).
func (a *App) updateMelden() {
	b, _ := json.Marshal(a.updateStand())
	js := "window.lumenDesktop&&window.lumenDesktop._update(" + string(b) + ")"
	a.host.Auftrag(func() { a.host.Skript(js) })
}

func (a *App) updateLaden() {
	a.mu.Lock()
	if !a.stand.Verfuegbar || a.stand.Phase == "laedt" || a.stand.Phase == "bereit" {
		a.mu.Unlock()
		return
	}
	a.stand.Phase, a.stand.Fehler = "laedt", ""
	a.mu.Unlock()
	a.updateMelden()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	pfad, err := func() (string, error) {
		v, err := a.upd.Neueste(ctx)
		if err != nil {
			return "", err
		}
		if !Neuer(v.Version, version) {
			return "", fmt.Errorf("keine neuere Fassung mehr")
		}
		return a.upd.InstallerLaden(ctx, v, filepath.Join(a.lokal, "Updates"))
	}()
	a.mu.Lock()
	if err != nil {
		log.Printf("Update laden: %v", err)
		a.stand.Phase, a.stand.Fehler = "fehler", err.Error()
	} else {
		a.stand.Phase, a.stand.pfad = "bereit", pfad
	}
	a.mu.Unlock()
	a.updateMelden()
}

func (a *App) updateStarten() {
	a.mu.Lock()
	pfad := a.stand.pfad
	ok := a.stand.Phase == "bereit" && pfad != ""
	a.mu.Unlock()
	if !ok {
		return
	}
	if err := a.host.InstallerStarten(pfad); err != nil {
		a.mu.Lock()
		a.stand.Phase, a.stand.Fehler = "fehler", err.Error()
		a.mu.Unlock()
		a.updateMelden()
		return
	}
	// Der Installer ersetzt die laufende Datei — also Platz machen.
	a.host.Schliessen()
}

// updateSpaeter blendet den Hinweis für einen Tag aus.
func (a *App) updateSpaeter() {
	a.mu.Lock()
	a.stand.Verfuegbar = false
	a.mu.Unlock()
	var d updateDatei
	_ = jsonLesen(a.updatePfad(), &d)
	d.AusgeblendetBis = time.Now().Add(24 * time.Hour).UTC()
	if err := jsonSchreiben(a.updatePfad(), d); err != nil {
		log.Printf("Update-Stand nicht gespeichert: %v", err)
	}
}
