// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

// Übernahme aus Emby oder Jellyfin — für das eigene Profil.
//
//	POST   /api/v1/me/uebernahme/server    {"adresse"}  → Art, Name, Benutzer zur Auswahl
//	POST   /api/v1/me/uebernahme           {"adresse","benutzer","passwort"} → startet
//	GET    /api/v1/me/uebernahme           Stand und Vorschau
//	POST   /api/v1/me/uebernahme/anwenden  {"gesehen","angefangen","favoriten","bewertungen"}
//	DELETE /api/v1/me/uebernahme           verwerfen
//
// Datenschutz, weil es hier darum geht, was jemand geschaut hat:
//   - Nur über requireAuth, nie über die Verwaltung. Jede Übernahme hängt
//     am Konto, das sie gestartet hat; kein anderes Konto — auch nicht der
//     Besitzer — kann sie lesen oder anwenden.
//   - Nichts davon landet im Protokoll, auch kein „X hat 1240 Titel
//     übernommen".
//   - Passwort und Emby-Token werden nirgends gespeichert. Das Passwort
//     gibt es nur in der laufenden Anfrage, das Token nur, bis die Daten
//     geholt sind; danach meldet Lumen es bei Emby ab.
//   - Die Vorschau liegt nur im Arbeitsspeicher und verfällt nach einer
//     halben Stunde.
//
// Gegen Missbrauch, denn die Adresse tippt jemand ein:
//   - Nur Ziele im Heimnetz (uebernahme.Verbinden), nur von zu Hause aus,
//     mit einer Bremse je Konto (wie beim PIN) und höchstens ein Lauf
//     gleichzeitig auf dem ganzen Server.
//   - Bis eine Anmeldung bei Emby gelungen ist, sagt Lumen über das Ziel
//     nur „passt" oder eine immer gleiche Meldung — kein Name, keine
//     Fassung, keine Benutzerliste, keine Unterscheidung zwischen „Port
//     zu", „kein Emby" und „gesperrte Adresse". Sonst taugte die
//     Übernahme zum Abtasten des Heimnetzes.
//   - Ein leeres Passwort nimmt Lumen nicht an.

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"lumen/internal/netz"
	"lumen/internal/store"
	"lumen/internal/uebernahme"
)

// uebernahmeClient: nil = der sichere Standard. Tests setzen keinen
// eigenen — der Nachbau läuft auf 127.0.0.1, und das Heimnetz ist hier
// ausdrücklich erlaubt.
var uebernahmeClient *http.Client

const uebernahmeGueltig = 30 * time.Minute

type uebernahmeVorschau struct {
	Gesehen       int `json:"gesehen"`
	Angefangen    int `json:"angefangen"`
	Favoriten     int `json:"favoriten"`
	Bewertungen   int `json:"bewertungen"`
	NichtGefunden int `json:"nichtGefunden"`
	// Was sich in Lumen wirklich ändern würde — ohne das, was schon da ist.
	Aenderungen store.UebernahmeErgebnis `json:"aenderungen"`
	// Die nicht gefundenen Titel (höchstens 200), damit man sieht, woran es lag.
	Fehlend []string `json:"fehlend"`
}

type uebernahmeLauf struct {
	mu         sync.Mutex
	Phase      string                    `json:"phase"` // verbinden | laden | bereit | fehler | fertig
	Meldung    string                    `json:"meldung,omitempty"`
	Gelesen    int                       `json:"gelesen"`
	Server     string                    `json:"server,omitempty"`
	ServerName string                    `json:"serverName,omitempty"`
	Vorschau   *uebernahmeVorschau       `json:"vorschau,omitempty"`
	Ergebnis   *store.UebernahmeErgebnis `json:"ergebnis,omitempty"`

	eintraege []store.UebernahmeEintrag
	bis       time.Time
	abbruch   context.CancelFunc
}

func (l *uebernahmeLauf) setzen(f func(*uebernahmeLauf)) {
	l.mu.Lock()
	defer l.mu.Unlock()
	f(l)
	l.bis = time.Now().Add(uebernahmeGueltig)
}

// Kopie für die Antwort, ohne Sperre und ohne die Einträge.
func (l *uebernahmeLauf) stand() map[string]any {
	l.mu.Lock()
	defer l.mu.Unlock()
	m := map[string]any{"phase": l.Phase, "gelesen": l.Gelesen}
	if l.Meldung != "" {
		m["meldung"] = l.Meldung
	}
	if l.Server != "" {
		m["server"], m["serverName"] = l.Server, l.ServerName
	}
	if l.Vorschau != nil {
		m["vorschau"] = l.Vorschau
	}
	if l.Ergebnis != nil {
		m["ergebnis"] = l.Ergebnis
	}
	return m
}

var uebernahmen = struct {
	sync.Mutex
	je map[string]*uebernahmeLauf // Konto → Lauf
}{je: map[string]*uebernahmeLauf{}}

// uebernahmeVon gibt den Lauf dieses Kontos — und räumt verfallene weg.
func uebernahmeVon(konto string) *uebernahmeLauf {
	uebernahmen.Lock()
	defer uebernahmen.Unlock()
	jetzt := time.Now()
	for k, l := range uebernahmen.je {
		l.mu.Lock()
		weg := jetzt.After(l.bis)
		l.mu.Unlock()
		if weg {
			if l.abbruch != nil {
				l.abbruch()
			}
			delete(uebernahmen.je, k)
		}
	}
	return uebernahmen.je[konto]
}

// uebernahmeFehler macht aus einem Fehler einen Satz für den Bildschirm.
// Nie die rohe Meldung: Sie könnte Adressen oder Antworten des fremden
// Servers enthalten.
//
// angemeldet: ob die Anmeldung bei Emby schon gelungen ist. Vorher gibt
// es ausser „Passwort falsch" nur einen einzigen Satz — ob unter der
// Adresse nichts lauscht, etwas anderes, ob sie gesperrt ist oder zu
// lange braucht, soll sich daran nicht ablesen lassen.
func uebernahmeFehler(err error, angemeldet bool) string {
	if errors.Is(err, uebernahme.ErrAnmeldung) {
		return "Benutzername oder Passwort stimmt nicht."
	}
	if !angemeldet {
		return uebernahmeNichtErreichbarText
	}
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "Der Server hat zu lange nicht geantwortet."
	case errors.Is(err, netz.ErrZuGross), errors.Is(err, uebernahme.ErrZuViel):
		return "Der Server hat eine unerwartet grosse Antwort geschickt."
	}
	return "Die Verbindung zum Server hat nicht geklappt."
}

const uebernahmeNichtErreichbarText = "Unter dieser Adresse war kein Emby- oder Jellyfin-Server im Heimnetz zu erreichen. Stimmen Adresse und Port?"

// uebernahmeAdresseText: Die Adresse ist schon der Form nach falsch —
// das lässt sich ohne Netz sagen und verrät nichts über das Ziel.
const uebernahmeAdresseText = "Bitte die Adresse von Emby oder Jellyfin im Heimnetz angeben, mit http:// oder https://."

// uebernahmeAmLaufen: höchstens ein Lauf auf dem ganzen Server. Ein Lauf
// liest bis zu 64 MB von einem fremden Server; mehrere gleichzeitig
// könnten einen kleinen NAS in die Knie zwingen.
var uebernahmeAmLaufen = make(chan struct{}, 1)

// uebernahmeVorab: was für beide Wege gilt, bevor Lumen irgendwohin
// verbindet. false: Antwort ist schon geschrieben.
func (s *Server) uebernahmeVorab(w http.ResponseWriter, r *http.Request) bool {
	if vonAussen(r) {
		failCode(w, http.StatusForbidden, "unterwegs",
			"Die Übernahme aus Emby oder Jellyfin geht nur zu Hause im Heimnetz.")
		return false
	}
	return true
}

func (s *Server) uebernahmeServer(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Adresse string `json:"adresse"`
	}
	if !readJSON(w, r, &in) || !s.uebernahmeVorab(w, r) {
		return
	}
	sv, err := uebernahme.Verbinden(in.Adresse, uebernahmeClient)
	if err != nil {
		fail(w, http.StatusBadRequest, uebernahmeAdresseText)
		return
	}
	// Jeder Versuch, eine Adresse zu prüfen, zählt wie ein Rateversuch:
	// Wer der Reihe nach Adressen und Ports durchgeht, wird gebremst.
	schluessel := "uebernahme:" + accountOf(r).ID
	frei, warte := s.bremse.anfangen(schluessel)
	if !frei {
		fail(w, http.StatusTooManyRequests, wartetext(w, warte))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	info, err := sv.Info(ctx)
	s.bremse.beenden(err == nil, schluessel)
	if err != nil {
		fail(w, http.StatusBadGateway, uebernahmeFehler(err, false))
		return
	}
	// Nur die Art — für „Dein Benutzername bei Emby". Name, Fassung und
	// die Benutzer der Anmeldeseite erst gar nicht: Vor einer gelungenen
	// Anmeldung geht nichts vom fremden Server zurück.
	ok(w, map[string]any{"art": info.Art})
}

func (s *Server) uebernahmeStarten(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Adresse  string `json:"adresse"`
		Benutzer string `json:"benutzer"`
		Passwort string `json:"passwort"`
	}
	if !readJSON(w, r, &in) || !s.uebernahmeVorab(w, r) {
		return
	}
	in.Benutzer = strings.TrimSpace(in.Benutzer)
	if in.Benutzer == "" || len(in.Benutzer) > 200 || len(in.Passwort) > 1000 {
		fail(w, http.StatusBadRequest, "Bitte den Benutzernamen angeben.")
		return
	}
	// Ohne Passwort wäre jeder Versuch ein Treffer bei jedem Konto ohne
	// Passwort — und Lumen ein Werkzeug, um solche Konten zu finden.
	if in.Passwort == "" {
		fail(w, http.StatusBadRequest, "Bitte das Passwort angeben. Ein Konto ohne Passwort lässt sich nicht übernehmen – setze dort zuerst eines.")
		return
	}
	sv, err := uebernahme.Verbinden(in.Adresse, uebernahmeClient)
	if err != nil {
		fail(w, http.StatusBadRequest, uebernahmeAdresseText)
		return
	}
	acc := accountOf(r)
	// Die Freigaben jetzt festhalten: Zugeordnet wird nur, was dieses
	// Profil sehen darf.
	q, err := s.scope(r)
	if err != nil {
		failErr(w, err)
		return
	}

	if alt := uebernahmeVon(acc.ID); alt != nil {
		alt.mu.Lock()
		laeuft := alt.Phase == "verbinden" || alt.Phase == "laden"
		alt.mu.Unlock()
		if laeuft {
			fail(w, http.StatusConflict, "Es läuft schon eine Übernahme.")
			return
		}
	}
	// Ein Lauf auf dem ganzen Server. Wessen, sagt die Meldung nicht.
	select {
	case uebernahmeAmLaufen <- struct{}{}:
	default:
		fail(w, http.StatusConflict, "Gerade läuft schon eine Übernahme. Bitte in ein paar Minuten noch einmal.")
		return
	}
	// Das Passwort ist ein Rateversuch wie ein PIN: dieselbe Bremse je
	// Konto wie beim Prüfen der Adresse. Freigegeben wird erst, wenn die
	// Anmeldung bei Emby entschieden ist (im Hintergrund).
	schluessel := "uebernahme:" + acc.ID
	frei, warte := s.bremse.anfangen(schluessel)
	if !frei {
		<-uebernahmeAmLaufen
		fail(w, http.StatusTooManyRequests, wartetext(w, warte))
		return
	}
	ctx, abbruch := context.WithTimeout(context.WithoutCancel(r.Context()), 10*time.Minute)
	lauf := &uebernahmeLauf{Phase: "verbinden", bis: time.Now().Add(uebernahmeGueltig), abbruch: abbruch}
	uebernahmen.Lock()
	uebernahmen.je[acc.ID] = lauf
	uebernahmen.Unlock()

	passwort := in.Passwort
	go func() {
		defer func() { <-uebernahmeAmLaufen }()
		defer abbruch()
		angemeldet := s.uebernahmeHolen(ctx, lauf, sv, in.Benutzer, passwort, acc.ID, q)
		s.bremse.beenden(angemeldet, schluessel)
	}()
	writeJSON(w, http.StatusAccepted, lauf.stand())
}

// uebernahmeHolen: anmelden, Stand holen, abmelden, zuordnen, Vorschau.
// Gibt zurück, ob die Anmeldung bei Emby gelungen ist (für die Bremse).
func (s *Server) uebernahmeHolen(ctx context.Context, lauf *uebernahmeLauf, sv *uebernahme.Server,
	benutzer, passwort, konto string, q store.Query) (angemeldet bool) {
	scheitern := func(err error) {
		lauf.setzen(func(l *uebernahmeLauf) { l.Phase, l.Meldung = "fehler", uebernahmeFehler(err, angemeldet) })
	}
	info, err := sv.Info(ctx)
	if err != nil {
		scheitern(err)
		return false
	}
	if err := sv.Anmelden(ctx, benutzer, passwort); err != nil {
		scheitern(err)
		return false
	}
	angemeldet = true
	// Erst jetzt, angemeldet, nennt Lumen den Namen des Servers.
	lauf.setzen(func(l *uebernahmeLauf) { l.Server, l.ServerName = info.Art, info.Name; l.Phase = "laden" })
	titel, serien, err := sv.Stand(ctx, func(n int) {
		lauf.setzen(func(l *uebernahmeLauf) { l.Gelesen = n })
	})
	// Das Token wird nicht mehr gebraucht — abmelden, bevor irgendetwas
	// anderes passiert, auch wenn das Holen scheiterte.
	sv.Abmelden(ctx)
	if err != nil {
		scheitern(err)
		return true
	}

	katalog, dateien, err := s.st.UebernahmeKatalog(ctx)
	if err != nil {
		scheitern(err)
		return true
	}
	// Was das Profil sehen darf, entscheidet dieselbe Bedingung wie im
	// Raster (Freigaben, Altersgrenze samt „nur FSK 0", Kette Folge–
	// Staffel–Serie). Eine eigene Regel hier wich früher ab: Bei einem
	// Kinderprofil mit Grenze 0 galt gar keine.
	sichtbar, err := s.st.SichtbareIDs(ctx, q)
	if err != nil {
		scheitern(err)
		return true
	}
	var lt []uebernahme.LumenTitel
	for _, t := range katalog {
		if !sichtbar[t.ID] {
			continue
		}
		lt = append(lt, uebernahme.LumenTitel{ID: t.ID, Kind: t.Kind, Titel: t.Titel, Original: t.Original,
			Jahr: t.Jahr, TMDb: t.TMDb, IMDb: t.IMDb, TVDb: t.TVDb, SerieID: t.SerieID,
			Staffel: t.Staffel, Folge: t.Folge, LaengeMS: t.LaengeMS})
	}
	var ld []uebernahme.LumenDatei
	for _, d := range dateien {
		if !sichtbar[d.ItemID] {
			continue
		}
		ld = append(ld, uebernahme.LumenDatei{ItemID: d.ItemID, Pfad: d.Pfad, Groesse: d.Groesse})
	}
	plan := uebernahme.Planen(titel, serien, uebernahme.NeuerIndex(lt, ld))

	eintraege := make([]store.UebernahmeEintrag, 0, len(plan.Eintraege))
	for _, u := range plan.Eintraege {
		eintraege = append(eintraege, store.UebernahmeEintrag{ItemID: u.ItemID, Gesehen: u.Gesehen,
			PositionMS: u.PositionMS, DauerMS: u.LaengeMS, Favorit: u.Favorit, Sterne: u.Sterne})
	}
	// Probelauf: was würde sich in Lumen tatsächlich ändern?
	probe, err := s.st.UebernahmeAnwenden(ctx, konto, eintraege, store.UebernahmeArten{
		Gesehen: true, Angefangen: true, Favoriten: true, Bewertungen: true}, true)
	if err != nil {
		scheitern(err)
		return true
	}
	fehlend := plan.Fehlend
	if fehlend == nil {
		fehlend = []string{}
	}
	lauf.setzen(func(l *uebernahmeLauf) {
		l.Phase = "bereit"
		l.eintraege = eintraege
		l.Vorschau = &uebernahmeVorschau{Gesehen: plan.Gesehen, Angefangen: plan.Angefangen,
			Favoriten: plan.Favoriten, Bewertungen: plan.Bewertungen, NichtGefunden: plan.NichtGefunden,
			Aenderungen: probe, Fehlend: fehlend}
	})
	return true
}

func (s *Server) uebernahmeStand(w http.ResponseWriter, r *http.Request) {
	l := uebernahmeVon(accountOf(r).ID)
	if l == nil {
		ok(w, map[string]any{"phase": "keine"})
		return
	}
	ok(w, l.stand())
}

func (s *Server) uebernahmeAnwenden(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Gesehen     *bool `json:"gesehen"`
		Angefangen  *bool `json:"angefangen"`
		Favoriten   *bool `json:"favoriten"`
		Bewertungen *bool `json:"bewertungen"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	ja := func(b *bool) bool { return b == nil || *b }
	acc := accountOf(r)
	l := uebernahmeVon(acc.ID)
	if l == nil {
		fail(w, http.StatusNotFound, "Die Vorschau ist abgelaufen. Bitte noch einmal verbinden.")
		return
	}
	l.mu.Lock()
	if l.Phase != "bereit" {
		l.mu.Unlock()
		fail(w, http.StatusConflict, "Es gibt nichts zu übernehmen.")
		return
	}
	eintraege := l.eintraege
	l.Phase = "anwenden"
	l.mu.Unlock()

	// Geschrieben wird nur, was das Profil jetzt sehen darf — die
	// Freigaben können sich seit der Vorschau geändert haben.
	q, err := s.scope(r)
	if err != nil {
		l.setzen(func(l *uebernahmeLauf) { l.Phase = "bereit" })
		failErr(w, err)
		return
	}
	sichtbar, err := s.st.SichtbareIDs(r.Context(), q)
	if err != nil {
		l.setzen(func(l *uebernahmeLauf) { l.Phase = "bereit" })
		failErr(w, err)
		return
	}
	erlaubt := make([]store.UebernahmeEintrag, 0, len(eintraege))
	for _, e := range eintraege {
		if sichtbar[e.ItemID] {
			erlaubt = append(erlaubt, e)
		}
	}

	erg, err := s.st.UebernahmeAnwenden(r.Context(), acc.ID, erlaubt, store.UebernahmeArten{
		Gesehen: ja(in.Gesehen), Angefangen: ja(in.Angefangen),
		Favoriten: ja(in.Favoriten), Bewertungen: ja(in.Bewertungen)}, false)
	if err != nil {
		l.setzen(func(l *uebernahmeLauf) { l.Phase = "bereit" })
		failErr(w, err)
		return
	}
	l.setzen(func(l *uebernahmeLauf) {
		l.Phase, l.Ergebnis, l.eintraege = "fertig", &erg, nil
	})
	ok(w, l.stand())
}

func (s *Server) uebernahmeVerwerfen(w http.ResponseWriter, r *http.Request) {
	konto := accountOf(r).ID
	uebernahmen.Lock()
	if l := uebernahmen.je[konto]; l != nil && l.abbruch != nil {
		l.abbruch()
	}
	delete(uebernahmen.je, konto)
	uebernahmen.Unlock()
	w.WriteHeader(http.StatusNoContent)
}
