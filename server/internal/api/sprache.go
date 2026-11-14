// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

// Sprache und Region.
//
// Es gibt zwei Ebenen: die Sprache des Servers (bei der Einrichtung
// gewählt, Einstellung „sprache") und je Profil eine eigene (Pref
// „sprache", leer = die des Servers). Welche für eine Anfrage gilt, steht
// danach im Kopf Content-Language der Antwort. fail() liest sie dort —
// so muss keiner der rund zweihundert Aufrufe die Anfrage durchreichen,
// und die App sieht auch selbst, in welcher Sprache die Antwort kommt.
//
// Vor der Anmeldung gilt Accept-Language (die Web-App schickt dort die
// Sprache, die sie gerade zeigt), sonst die des Servers.

import (
	"context"
	"net/http"
	"strings"
	"sync"

	"lumen/internal/sprache"
)

const (
	keySprache     = "sprache" // Einstellung des Servers
	prefSprache    = "sprache" // Pref eines Profils
	kopfSprache    = "Content-Language"
	kopfAnfrageSpr = "Accept-Language"
)

// sprachSpeicher hält die Sprache des Servers und der Profile im
// Speicher: Sie wird bei jeder Anfrage gebraucht (auch bei jedem
// Video-Segment), und dafür jedes Mal die Datenbank zu fragen, wäre
// Verschwendung. Geändert wird sie nur über diesen Server, darum reicht
// es, beim Ändern den Eintrag zu ersetzen.
type sprachSpeicher struct {
	mu      sync.Mutex
	server  string
	profile map[string]string // Konto → Pref (auch "", damit nicht neu gefragt wird)
}

// serverSprache: die Vorgabe des Servers.
func (s *Server) serverSprache(ctx context.Context) string {
	sprachen := &s.sprachen
	sprachen.mu.Lock()
	defer sprachen.mu.Unlock()
	if sprachen.server == "" {
		sprachen.server = sprache.Vorgabe
		if s.st != nil {
			if v := s.st.Setting(ctx, keySprache, sprache.Vorgabe); sprache.Gueltig(v) {
				sprachen.server = v
			}
		}
	}
	return sprachen.server
}

// setzeServerSprache speichert die Vorgabe des Servers.
func (s *Server) setzeServerSprache(ctx context.Context, id string) error {
	if err := s.st.SetSetting(ctx, keySprache, id); err != nil {
		return err
	}
	sprachen := &s.sprachen
	sprachen.mu.Lock()
	sprachen.server = id
	sprachen.mu.Unlock()
	return nil
}

// profilSprache: die eigene Wahl eines Profils, "" für „wie der Server".
func (s *Server) profilSprache(ctx context.Context, kontoID string) string {
	sprachen := &s.sprachen
	sprachen.mu.Lock()
	v, ok := sprachen.profile[kontoID]
	sprachen.mu.Unlock()
	if ok {
		return v
	}
	v = ""
	if prefs, err := s.st.Prefs(ctx, kontoID); err == nil && sprache.Gueltig(prefs[prefSprache]) {
		v = prefs[prefSprache]
	}
	sprachen.mu.Lock()
	if sprachen.profile == nil {
		sprachen.profile = map[string]string{}
	}
	sprachen.profile[kontoID] = v
	sprachen.mu.Unlock()
	return v
}

// merkeProfilSprache: nach PATCH /me, damit die nächste Anfrage sie hat.
func (s *Server) merkeProfilSprache(kontoID, id string) {
	sprachen := &s.sprachen
	sprachen.mu.Lock()
	if sprachen.profile == nil {
		sprachen.profile = map[string]string{}
	}
	sprachen.profile[kontoID] = id
	sprachen.mu.Unlock()
}

// wirksameSprache eines Profils: die eigene oder die des Servers.
func (s *Server) wirksameSprache(ctx context.Context, kontoID string) string {
	if v := s.profilSprache(ctx, kontoID); v != "" {
		return v
	}
	return s.serverSprache(ctx)
}

// anfrageSprache: vor der Anmeldung.
func (s *Server) anfrageSprache(r *http.Request) string {
	if v := sprache.AusAcceptLanguage(r.Header.Get(kopfAnfrageSpr)); v != "" {
		return v
	}
	return s.serverSprache(r.Context())
}

// spracheVon liest die Sprache, die für diese Antwort gilt.
func spracheVon(w http.ResponseWriter) string {
	if v := w.Header().Get(kopfSprache); v != "" {
		return v
	}
	return sprache.Vorgabe
}

// tr übersetzt für die Antwort w. werte: abwechselnd Name und Wert.
func tr(w http.ResponseWriter, schluessel string, werte ...any) string {
	return sprache.T(spracheVon(w), schluessel, werte...)
}

// fehlerCodes geben häufigen Meldungen eine feste Kennung, an der die
// Apps die Art des Fehlers erkennen, ohne den (jetzt übersetzten) Text zu
// lesen. Wer einen Text vergleicht, bricht beim ersten Englisch.
var fehlerCodes = map[string]string{
	"nicht gefunden":                                    "nicht_gefunden",
	"nicht angemeldet":                                  "nicht_angemeldet",
	"anmeldung abgelaufen":                              "anmeldung_abgelaufen",
	"medienschlüssel abgelaufen":                        "medienschluessel_abgelaufen",
	"falscher code":                                     "falscher_code",
	"kein bild":                                         "kein_bild",
	"die datei ist nicht mehr da":                       "datei_fehlt",
	"datei nicht lesbar":                                "datei_nicht_lesbar",
	"dieser server ist schon eingerichtet":              "schon_eingerichtet",
	"einrichten geht nur zu hause, nicht von unterwegs": "nur_zu_hause",
	"einrichten geht nicht vom rechner selbst aus — öffne lumen von einem anderen gerät im heimnetz oder setze LUMEN_HEIMNETZ=127.0.0.1": "nur_zu_hause",
	"Unbekannte Sprache.":                   "unbekannte_sprache",
	"herunterladen ist für dein profil aus": "herunterladen_aus",
	"umwandeln ist für dein profil aus":     "umwandeln_aus",
	"diese wiedergabe läuft nicht mehr":     "wiedergabe_beendet",
	"datenbank antwortet nicht":             "datenbank",
	"die Verwaltung ist gesperrt":           "verwaltung_gesperrt",
}

// einrichtungsSprache: bei der Einrichtung gewählt. Setzt die Sprache
// des Servers und belegt die Metadaten vor — aber nur, wo noch nichts
// steht: Wer vor der Einrichtung schon Umgebungswerte oder eine alte
// Datenbank mitbringt, behält seine Wahl.
func (s *Server) einrichtungsSprache(ctx context.Context, id string) {
	_ = s.setzeServerSprache(ctx, id)
	metaSpr, metaReg := sprache.Metadaten(id)
	if s.st.Setting(ctx, keyLanguage, "") == "" {
		_ = s.st.SetSetting(ctx, keyLanguage, metaSpr)
	}
	if s.st.Setting(ctx, keyRegion, "") == "" {
		_ = s.st.SetSetting(ctx, keyRegion, metaReg)
	}
}

// sprachAuswahl: was /hello als Liste schickt.
func sprachAuswahl() []sprache.Sprache { return sprache.Sprachen }

// adminSprache: PUT /api/v1/admin/sprache {"sprache":"de-DE"} — die
// Vorgabe des Servers. Nur Besitzer (oder Vollzugriff): Sie gilt für
// alle Profile, die keine eigene gewählt haben.
func (s *Server) adminSprache(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Sprache string `json:"sprache"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	id := strings.TrimSpace(in.Sprache)
	if !sprache.Gueltig(id) {
		fail(w, http.StatusBadRequest, "Unbekannte Sprache.")
		return
	}
	if err := s.setzeServerSprache(r.Context(), id); err != nil {
		failErr(w, err)
		return
	}
	s.st.Log(r.Context(), "info", "sprache", "Sprache des Servers: "+id)
	// Die Antwort selbst gilt schon in der neuen Sprache, sofern das
	// Profil keine eigene hat.
	w.Header().Set(kopfSprache, s.wirksameSprache(r.Context(), accountOf(r).ID))
	ok(w, map[string]any{"sprache": id, "sprachen": sprachAuswahl()})
}

// zusatzTexte: Schlüssel, die nicht über fail()/tr() mit festem Text
// laufen, sondern zusammengesetzt werden (Neuigkeiten, Felder für die
// Verwaltung). Der Test TestKatalogOhneWaisen kennt sie von hier.
func zusatzTexte() map[string]bool {
	out := map[string]bool{}
	for _, k := range zusatzSchluessel {
		out[k] = true
	}
	return out
}

var zusatzSchluessel = []string{
	// Neuigkeiten (store.Vorlage, profil.go)
	"{n} neue Folgen", "Neu in deiner Reihe „{reihe}“", "Dein Wunsch ist da", "Neu dazugekommen",
	"{n} neue Titel", "zuletzt: {titel}", "Neue Folgen von {serie}", "Updates kommen nicht durch",
	"GitHub nimmt das Token nicht mehr an — unter Verwaltung › Updates ein neues eintragen",
	"Sicherung gescheitert", "Platz wird knapp", "Nur noch {gb} GB frei ({ort})", "Datenbank", "Umwandlung",
	// Reihen der Startseite (handlers.go, add)
	"Neu dazugekommen", "Gut bewertet", "Filme", "Serien", "Zufällig",
}
