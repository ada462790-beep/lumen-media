// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

// Updates im Container per Knopfdruck.
//
// Im Container kann sich Lumen nicht selbst ersetzen — das Abbild wird als
// Ganzes getauscht. Das übernimmt ein kleiner Begleiter, der „Aktualisierer"
// (Watchtower, siehe docker-compose.yml): Er allein hat Zugriff auf Docker,
// holt das neue Abbild und startet Lumen damit neu. Lumen selbst bekommt
// keinen Docker-Zugriff; es kann dem Begleiter nur sagen „jetzt bitte" —
// über dessen HTTP-Schnittstelle, geschützt mit einem geheimen Token.
//
// Adresse und Token kommen nur aus der Umgebung (LUMEN_AKTUALISIERER_URL,
// LUMEN_AKTUALISIERER_TOKEN). Über die Oberfläche lässt sich keine Adresse
// eintragen — sonst könnte, wer die Verwaltung offen vorfindet, Lumen
// Anfragen an beliebige Rechner im Heimnetz schicken lassen. Das Token
// steht in keiner Antwort, keinem Protokoll und keiner Fehlermeldung.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"lumen/internal/update"
)

// Kennungen für den Ausgang einer Anfrage an den Aktualisierer. Die
// Oberfläche macht daraus den Satz in der Sprache des Profils.
const (
	aktOk             = "ok"               // angenommen, Lumen startet gleich neu
	aktTokenFalsch    = "token_falsch"     // 401/403: Token passt nicht
	aktNichtErreicht  = "nicht_erreichbar" // Name unbekannt, Verbindung abgelehnt
	aktBeschaeftigt   = "beschaeftigt"     // 429: ein anderes Update läuft schon
	aktZeitUm         = "zeit_um"          // keine Antwort in der Frist
	aktNichtsNeues    = "nichts_neues"     // lief durch, aber kein neueres Abbild
	aktFehlgeschlagen = "fehlgeschlagen"   // Watchtower meldet einen Fehler beim Tauschen
	aktAntwortFalsch  = "antwort_falsch"   // anderer Status — falsche Adresse?
)

// aktualisiererFrist: So lange darf das Holen des Abbilds dauern. Watchtower
// hält die Antwort, bis alles getauscht ist (Vorgabe dort: 10 Minuten).
// Läuft das Update, beendet Watchtower Lumen mittendrin — die Antwort
// kommt dann nie an, und das ist der gute Fall.
var aktualisiererFrist = 12 * time.Minute

// aktualisiererNachfrist: Meldet der Aktualisierer Erfolg und Lumen läuft
// danach noch so lange, ist etwas faul.
var aktualisiererNachfrist = 10 * time.Minute

// aktStand: der letzte Auftrag an den Aktualisierer, für die Oberfläche.
type aktStand struct {
	Seit time.Time `json:"seit"`
	Nach string    `json:"nach"`
	Code string    `json:"code,omitempty"` // leer, solange die Antwort aussteht
}

// updateImContainer: läuft Lumen in einem Container? Wie imContainer();
// LUMEN_UPDATE_ALS_CONTAINER=1 tut nur für diese Seite so — zum Ausprobieren
// des ganzen Ablaufs ohne Docker, wie LUMEN_UPDATE_API ohne GitHub. Im
// Betrieb setzt das niemand, und die Erkennung selbst bleibt unverändert.
var updateImContainer = func() bool {
	return imContainer() || os.Getenv("LUMEN_UPDATE_ALS_CONTAINER") == "1"
}

// aktualisiererAdresse liest LUMEN_AKTUALISIERER_URL. Nur http(s) mit
// Rechnername, ohne Zugangsdaten in der Adresse; sonst leer.
func aktualisiererAdresse() string {
	roh := strings.TrimSpace(os.Getenv("LUMEN_AKTUALISIERER_URL"))
	if roh == "" {
		return ""
	}
	u, err := url.Parse(roh)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return ""
	}
	return strings.TrimRight(u.Scheme+"://"+u.Host+u.Path, "/")
}

func aktualisiererToken() string {
	return strings.TrimSpace(os.Getenv("LUMEN_AKTUALISIERER_TOKEN"))
}

// aktualisiererBereit: Der Knopf erscheint nur im Container und nur, wenn
// Adresse und Token gesetzt sind.
func aktualisiererBereit() bool {
	return updateImContainer() && aktualisiererAdresse() != "" && aktualisiererToken() != ""
}

// aktualisiererHalb: Eines von beiden fehlt oder die Adresse taugt nicht —
// dann sagt die Seite, was fehlt, statt den Knopf still wegzulassen.
func aktualisiererHalb() bool {
	gesetzt := strings.TrimSpace(os.Getenv("LUMEN_AKTUALISIERER_URL")) != ""
	return updateImContainer() && !aktualisiererBereit() && (gesetzt || aktualisiererToken() != "")
}

// aktualisiererClient geht ohne Proxy: Der Begleiter steht im selben
// Docker-Netz, und das Token gehört nicht durch einen fremden Rechner.
var aktualisiererClient = &http.Client{
	Transport: &http.Transport{
		Proxy:                 nil,
		DialContext:           (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
		ResponseHeaderTimeout: 0, // die Antwort kommt erst nach dem Tauschen
	},
}

// aktualisiererAntwort ist die Zusammenfassung, die Watchtower nach einem
// Lauf zurückgibt (POST /v1/update ohne async).
type aktualisiererAntwort struct {
	Summary struct {
		Scanned int `json:"scanned"`
		Updated int `json:"updated"`
		Failed  int `json:"failed"`
	} `json:"summary"`
}

// aktualisiererRufen schickt POST {url}/v1/update. Die Antwort sagt, wie es
// ausging; der Fehler (ohne Token) steht nur im Protokoll.
func aktualisiererRufen(ctx context.Context) (string, error) {
	adresse, token := aktualisiererAdresse(), aktualisiererToken()
	if adresse == "" || token == "" {
		return aktNichtErreicht, fmt.Errorf("aktualisierer: adresse oder token fehlt")
	}
	ctx, cancel := context.WithTimeout(ctx, aktualisiererFrist)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, adresse+"/v1/update", nil)
	if err != nil {
		return aktNichtErreicht, fmt.Errorf("aktualisierer: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := aktualisiererClient.Do(req)
	if err != nil {
		// Die Meldung von net/http nennt die Adresse, nie den Kopf.
		if errors.Is(err, context.DeadlineExceeded) {
			return aktZeitUm, fmt.Errorf("aktualisierer: keine antwort in %s", aktualisiererFrist)
		}
		return aktNichtErreicht, fmt.Errorf("aktualisierer: %w", err)
	}
	defer resp.Body.Close()
	roh, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return aktTokenFalsch, fmt.Errorf("aktualisierer: status %d", resp.StatusCode)
	case resp.StatusCode == http.StatusTooManyRequests:
		return aktBeschaeftigt, fmt.Errorf("aktualisierer: status %d", resp.StatusCode)
	case resp.StatusCode == http.StatusServiceUnavailable || resp.StatusCode == http.StatusGatewayTimeout:
		return aktZeitUm, fmt.Errorf("aktualisierer: status %d", resp.StatusCode)
	case resp.StatusCode == http.StatusAccepted:
		return aktOk, nil
	case resp.StatusCode != http.StatusOK:
		return aktAntwortFalsch, fmt.Errorf("aktualisierer: status %d", resp.StatusCode)
	}
	var a aktualisiererAntwort
	if err := json.Unmarshal(roh, &a); err != nil {
		return aktAntwortFalsch, fmt.Errorf("aktualisierer: antwort unlesbar")
	}
	switch {
	case a.Summary.Failed > 0:
		return aktFehlgeschlagen, fmt.Errorf("aktualisierer: %d fehlgeschlagen", a.Summary.Failed)
	case a.Summary.Updated == 0:
		return aktNichtsNeues, nil
	}
	return aktOk, nil
}

// aktualisiererNotiz: was im Update-Protokoll steht (wie die übrigen
// Einträge dort auf Deutsch gespeichert).
func aktualisiererNotiz(code string) string {
	switch code {
	case aktTokenFalsch:
		return "Der Aktualisierer lehnt das Token ab — LUMEN_AKTUALISIERER_TOKEN und WATCHTOWER_HTTP_API_TOKEN müssen gleich sein."
	case aktNichtErreicht:
		return "Der Aktualisierer ist nicht erreichbar — läuft er, und stimmt LUMEN_AKTUALISIERER_URL?"
	case aktBeschaeftigt:
		return "Der Aktualisierer ist schon mit einem Update beschäftigt."
	case aktZeitUm:
		return "Der Aktualisierer hat nicht rechtzeitig geantwortet."
	case aktNichtsNeues:
		return "Der Aktualisierer fand kein neueres Abbild — vielleicht ist es noch nicht fertig gebaut, oder Lumen trägt das Label nicht."
	case aktFehlgeschlagen:
		return "Der Aktualisierer konnte das Abbild nicht tauschen — Näheres in seinem Protokoll im Container Manager."
	case aktAntwortFalsch:
		return "Der Aktualisierer antwortet unerwartet — zeigt LUMEN_AKTUALISIERER_URL auf Watchtower?"
	}
	return ""
}

// aktualisiererAnstossen bittet den Begleiter um das Update. Vorher wird die
// Datenbank ausgeschrieben (WAL zurück in die Datei): Watchtower beendet
// Lumen gleich darauf mit SIGTERM und gibt ihm 30 Sekunden — Lumen fährt
// zwar sauber herunter, aber so ist der Stand schon vorher auf der Platte.
//
// Läuft synchron; der Aufrufer entscheidet, ob im Hintergrund.
func (s *Server) aktualisiererAnstossen(ctx context.Context, rel *update.Release, auto bool) string {
	updStand.mu.Lock()
	if updStand.laeuft {
		updStand.mu.Unlock()
		return aktBeschaeftigt
	}
	updStand.laeuft = true
	updStand.akt = aktStand{Seit: time.Now(), Nach: rel.Version}
	updStand.mu.Unlock()

	if err := s.st.Ausschreiben(ctx); err != nil {
		s.log.Warn("update: datenbank ausschreiben", "err", err)
	}
	s.log.Info("update über den aktualisierer", "von", s.version, "nach", rel.Version, "auto", auto)
	s.updateProtokollieren(ctx, updateEintrag{Von: s.version, Nach: rel.Version, Auto: auto,
		Ergebnis: "angestossen", Notiz: "Der Aktualisierer holt das neue Abbild und startet Lumen neu."})

	code, err := aktualisiererRufen(ctx)
	if err != nil {
		s.log.Warn("update über den aktualisierer", "code", code, "err", err)
	}
	updStand.mu.Lock()
	updStand.akt.Code = code
	if code != aktOk {
		updStand.laeuft = false
	}
	updStand.mu.Unlock()
	if code == aktOk {
		// Eigentlich ist Lumen jetzt schon beendet. Läuft es nach der Frist
		// immer noch, hat der Aktualisierer etwas anderes getauscht (oder
		// nichts) — dann den Knopf wieder freigeben und es sagen.
		seit := updStand.akt.Seit
		time.AfterFunc(aktualisiererNachfrist, func() {
			updStand.mu.Lock()
			haengt := updStand.laeuft && updStand.akt.Seit.Equal(seit)
			if haengt {
				updStand.laeuft = false
				updStand.akt.Code = aktNichtsNeues
			}
			updStand.mu.Unlock()
			if haengt {
				s.updateProtokollieren(context.Background(), updateEintrag{Von: s.version, Nach: rel.Version, Auto: auto,
					Ergebnis: "fehler", Notiz: "Der Aktualisierer hat angenommen, aber Lumen läuft noch mit der alten Fassung — trägt Lumen das Label com.centurylinklabs.watchtower.enable=true?"})
			}
		})
	}
	if code != aktOk {
		s.updateProtokollieren(ctx, updateEintrag{Von: s.version, Nach: rel.Version, Auto: auto,
			Ergebnis: "fehler", Notiz: aktualisiererNotiz(code)})
	}
	return code
}

// aktualisiererNachStart: Hat der Aktualisierer vor dem Neustart übernommen
// und läuft jetzt die neue Fassung, kommt das ins Protokoll. Der letzte
// Eintrag sagt, ob es so war.
func (s *Server) aktualisiererNachStart(ctx context.Context) {
	liste := s.updateProtokoll(ctx)
	if len(liste) == 0 || liste[0].Ergebnis != "angestossen" {
		return
	}
	alt := liste[0]
	if alt.Von == s.version {
		// Gleiche Fassung wie vorher: Neu gestartet ohne neues Abbild, oder
		// der Aufruf ist gar nicht angekommen. Dann steht dazu schon ein
		// Fehler — oder eben keiner, weil Lumen mittendrin beendet wurde.
		s.updateProtokollieren(ctx, updateEintrag{Von: alt.Von, Nach: alt.Nach, Auto: alt.Auto,
			Ergebnis: "fehler", Notiz: "Lumen wurde neu gestartet, läuft aber weiter mit der alten Fassung."})
		return
	}
	s.updateProtokollieren(ctx, updateEintrag{Von: alt.Von, Nach: s.version, Auto: alt.Auto,
		Ergebnis: "ok", Notiz: "eingesetzt vom Aktualisierer"})
}

// adminUpdateContainer: POST /api/v1/admin/update/container — „Jetzt
// aktualisieren" im Container. Erst die Frage nach laufenden Wiedergaben
// (409, die Oberfläche fragt nach), dann die Bestätigung mit PIN/Passwort
// (bestaetigt) — so fragt die Oberfläche höchstens einmal nach dem Code.
func (s *Server) adminUpdateContainer(w http.ResponseWriter, r *http.Request) {
	if !aktualisiererBereit() {
		failCode(w, http.StatusPreconditionFailed, "aktualisierer_fehlt",
			"Für Updates per Knopfdruck braucht es den Aktualisierer — siehe README.md, Abschnitt „Updates per Knopfdruck“.")
		return
	}
	if n := s.live.Load().Total; n > 0 && r.URL.Query().Get("trotzdem") != "1" {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":       tr(w, "es laufen Wiedergaben"),
			"code":        "wiedergaben_laufen",
			"wiedergaben": n,
		})
		return
	}
	s.bestaetigt(s.adminUpdateContainerLos)(w, r)
}

func (s *Server) adminUpdateContainerLos(w http.ResponseWriter, r *http.Request) {
	updStand.mu.Lock()
	rel, laeuft := updStand.neueste, updStand.laeuft
	updStand.mu.Unlock()
	if laeuft {
		failCode(w, http.StatusConflict, "update_laeuft", "Ein Update läuft schon.")
		return
	}
	if rel == nil || !update.Neuer(rel.Version, s.version) {
		if r2, err := s.UpdatePruefen(r.Context()); err == nil {
			rel = r2
		}
	}
	if rel == nil || !update.Neuer(rel.Version, s.version) {
		fail(w, http.StatusPreconditionFailed, "es gibt nichts Neueres")
		return
	}
	// Im Hintergrund: Watchtower hält die Antwort, bis das Abbild geholt
	// ist, und beendet Lumen dabei. Die Oberfläche fragt derweil nach.
	go s.aktualisiererAnstossen(detach(r), rel, false)
	writeJSON(w, http.StatusAccepted, map[string]any{"status": "angestossen", "nach": rel.Version})
}
