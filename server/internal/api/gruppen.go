// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

// Gruppen und Verwaltungssperre (GitHub-Issue #6).
//
// Wer darf in die Verwaltung? Der Besitzer, und jede Person in einer
// Gruppe mit „Verwaltung". Für beide gilt dieselbe Grenze wie immer:
// die Verwaltung zeigt nicht, wer was schaut.
//
// Nur der Besitzer legt Gruppen an und steckt Konten hinein — sonst
// könnte ein Mitverwalter sich selbst oder andere befördern.
//
// Die Verwaltungssperre: Jede Person mit Zugang kann sich einen eigenen
// PIN, ein Passwort oder einen Code aus einer Authenticator-App setzen.
// Dann verlangt jede Verwaltungs-Anfrage ein Entsperr-Token, das Lumen
// nach dem Entsperren ausgibt und das nach einer Viertelstunde ohne
// Verwaltung wieder verfällt. Kein E-Mail, kein SMS.

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"lumen/internal/store"
	"lumen/internal/totp"
)

const (
	entsperrtDauer   = 15 * time.Minute
	kopfVerwaltung   = "X-Lumen-Admin"
	kopfBestaetigung = "X-Lumen-Bestaetigung"
)

// entsperrt hält die Verwaltungs-Sitzungen: Token → Konto, bis wann.
type entsperrung struct {
	accountID string
	bis       time.Time
}

type verwaltungsSperre struct {
	mu     sync.Mutex
	offen  map[string]entsperrung
	anlauf map[string]string      // Konto → TOTP-Geheimnis, das gerade eingerichtet wird
	fehl   map[string][]time.Time // Konto → Fehlversuche (Bremse)
	// benutzt: schon verwendete Authenticator-Codes, bis sie verfallen.
	benutzt map[string]time.Time
}

var sperre = verwaltungsSperre{offen: map[string]entsperrung{}, anlauf: map[string]string{}, fehl: map[string][]time.Time{}}

func (v *verwaltungsSperre) token(accountID string) string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	t := base64.RawURLEncoding.EncodeToString(b)
	v.mu.Lock()
	defer v.mu.Unlock()
	for k, e := range v.offen {
		if e.bis.Before(time.Now()) {
			delete(v.offen, k)
		}
	}
	v.offen[t] = entsperrung{accountID, time.Now().Add(entsperrtDauer)}
	return t
}

// gilt prüft ein Token für dieses Konto und verlängert es.
func (v *verwaltungsSperre) gilt(tok, accountID string) bool {
	if tok == "" {
		return false
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	e, ok := v.offen[tok]
	if !ok || e.accountID != accountID || e.bis.Before(time.Now()) {
		return false
	}
	e.bis = time.Now().Add(entsperrtDauer)
	v.offen[tok] = e
	return true
}

func (v *verwaltungsSperre) schliessen(accountID string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	for k, e := range v.offen {
		if e.accountID == accountID {
			delete(v.offen, k)
		}
	}
}

// codeFrisch: Ein Code aus der Authenticator-App gilt nur einmal. Er
// bleibt 90 Sekunden gültig (Uhren gehen nicht genau gleich); wer ihn in
// dieser Zeit mitliest, soll damit nicht selbst hineinkommen.
func (v *verwaltungsSperre) codeFrisch(accountID, code string) bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.benutzt == nil {
		v.benutzt = map[string]time.Time{}
	}
	jetzt := time.Now()
	for k, bis := range v.benutzt {
		if jetzt.After(bis) {
			delete(v.benutzt, k)
		}
	}
	k := accountID + ":" + code
	if _, da := v.benutzt[k]; da {
		return false
	}
	v.benutzt[k] = jetzt.Add(2 * time.Minute)
	return true
}

// requireAdmin lässt Besitzer und Mitverwalter durch — wenn ihre
// Verwaltungssperre offen ist.
func (s *Server) requireAdmin(h http.HandlerFunc) http.Handler {
	return s.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		a := accountOf(r)
		if a == nil || !a.IsAdmin {
			// Bewusst 404 statt 403: wer nicht darf, soll nicht einmal
			// erfahren, dass es diese Ansichten gibt.
			fail(w, http.StatusNotFound, "nicht gefunden")
			return
		}
		// Verwaltung nur am Desktop (Entscheidung des Besitzers): Ein Token,
		// das sich beim Anmelden als Handy, Tablet oder Fernseher ausgewiesen
		// hat, kommt nicht hinein — etwa das eines liegen gelassenen iPads.
		// Die Art meldet das Gerät selbst; ein Browser auf einem Tablet sagt
		// „browser" und lässt sich vom Server nicht sicher erkennen. Dort
		// bleibt es bei der Oberfläche.
		if d := deviceOf(r); d != nil && !verwaltungsGeraet(d.Kind) {
			failCode(w, http.StatusForbidden, "verwaltung_nur_desktop",
				"Die Verwaltung gibt es nur im Browser am Computer und in der Mac-App.")
			return
		}
		// Ein Verwaltungsprofil ohne PIN wäre zu Hause für jeden offen: ein
		// Klick in der Profilauswahl, und man steht in der Verwaltung. Die
		// Oberfläche fragt dann nach einer PIN (GET /admin/lock sagt es).
		if a.LockKind == "" || a.LockKind == "none" {
			failCode(w, http.StatusForbidden, "profil_sperre_fehlt",
				"Bevor du die Verwaltung öffnest, schütze dein Profil mit einer PIN oder einem Passwort.")
			return
		}
		if a.AdminLock != "" && a.AdminLock != "none" && !sperre.gilt(r.Header.Get(kopfVerwaltung), a.ID) {
			writeJSON(w, http.StatusLocked, map[string]any{
				"error":  tr(w, "die Verwaltung ist gesperrt"),
				"code":   "verwaltung_gesperrt",
				"sperre": a.AdminLock,
			})
			return
		}
		h(w, r)
	})
}

// verwaltungsGeraet: Gerätearten, die die Verwaltung öffnen dürfen. Die
// Apps melden tv, phone oder tablet (iOS, iPadOS, tvOS, Android, Fire TV),
// die Mac-App desktop, die Web-App browser. Unbekannte Arten bleiben
// erlaubt, damit eine ältere oder fremde Oberfläche nicht ausgesperrt wird.
func verwaltungsGeraet(art string) bool {
	switch strings.ToLower(strings.TrimSpace(art)) {
	case "tv", "phone", "tablet":
		return false
	}
	return true
}

// requireOwner: nur der Besitzer selbst — für Gruppen und Zuweisungen.
func (s *Server) requireOwner(h http.HandlerFunc) http.Handler {
	return s.requireAdmin(func(w http.ResponseWriter, r *http.Request) {
		if a := accountOf(r); !a.Besitzerrechte() {
			fail(w, http.StatusNotFound, "nicht gefunden")
			return
		}
		h(w, r)
	})
}

// adminSperre sagt der Oberfläche, welche Sperre diese Person hat. Läuft
// nur über requireAuth, damit sie auch gesperrt erreichbar ist.
func (s *Server) adminSperre(w http.ResponseWriter, r *http.Request) {
	a := accountOf(r)
	if !a.IsAdmin {
		fail(w, http.StatusNotFound, "nicht gefunden")
		return
	}
	ok(w, map[string]any{
		"sperre":    a.AdminLock,
		"entsperrt": a.AdminLock == "none" || a.AdminLock == "" || sperre.gilt(r.Header.Get(kopfVerwaltung), a.ID),
		// Ohne Profilsperre bleibt die Verwaltung zu, bis eine gesetzt ist.
		"profilSperre": a.LockKind,
	})
}

// adminEntsperren prüft PIN, Passwort oder Code und gibt ein Token.
func (s *Server) adminEntsperren(w http.ResponseWriter, r *http.Request) {
	a := accountOf(r)
	if !a.IsAdmin {
		fail(w, http.StatusNotFound, "nicht gefunden")
		return
	}
	var in struct {
		Code string `json:"code"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	// Ein Versuch nach dem anderen, mit wachsender Pause (bremse.go).
	// Früher wurden Prüfen und Zählen getrennt erledigt — viele
	// gleichzeitige Anfragen kamen alle durch.
	schluessel := []string{"verwaltung:" + a.ID, quelle(r)}
	frei, warte := s.bremse.anfangen(schluessel...)
	if !frei {
		fail(w, http.StatusTooManyRequests, wartetext(w, warte))
		return
	}
	err := s.codePruefen(r, a, in.Code, false)
	s.bremse.beenden(err == nil, schluessel...)
	if err != nil {
		s.st.Log(r.Context(), "warn", "verwaltung", "Entsperren der Verwaltung fehlgeschlagen")
		fail(w, http.StatusForbidden, "das stimmt nicht")
		return
	}
	ok(w, map[string]any{"token": sperre.token(a.ID), "gueltigMin": int(entsperrtDauer.Minutes())})
}

// codePruefen prüft den Code der Verwaltungssperre (PIN, Passwort oder
// Authenticator). Mit profilAlsErsatz und ohne Verwaltungssperre gilt
// stattdessen PIN oder Passwort des eigenen Profils.
func (s *Server) codePruefen(r *http.Request, a *store.Account, code string, profilAlsErsatz bool) error {
	switch a.AdminLock {
	case "", "none":
		if profilAlsErsatz {
			// Ohne jeden Riegel gäbe es nichts zu bestätigen — früher ging
			// dann jeder Text durch. Jetzt heisst es: erst eine PIN setzen.
			if a.LockKind == "" || a.LockKind == "none" {
				return errErstPin
			}
			return s.st.CheckSecret(r.Context(), a.ID, code)
		}
		return nil
	case "totp":
		g, e := s.st.VerwaltungsGeheimnis(r.Context(), a.ID)
		if e != nil || !totp.Stimmt(g, code, time.Now()) || !sperre.codeFrisch(a.ID, code) {
			return store.ErrLocked
		}
		return nil
	default:
		return s.st.VerwaltungsSperrePruefen(r.Context(), a.ID, code)
	}
}

// bestaetigt verlangt beim Ändern von Rechten (Gruppen, wer in welche
// Gruppe gehört, Einschränkungen eines Profils) den Code noch einmal —
// auch wenn die Verwaltung schon offen ist (Issue #6: „zusätzliche
// Passworteingabe beim Anpassen von Benutzerrechten"). Wer kurz vom
// Rechner weg ist, soll niemandem Adminrechte geben lassen.
//
// Ohne Code antwortet Lumen mit 428 und code "bestaetigen"; die
// Oberfläche fragt dann nach und schickt die Anfrage mit dem Kopf
// X-Lumen-Bestaetigung noch einmal.
func (s *Server) bestaetigt(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a := accountOf(r)
		code := r.Header.Get(kopfBestaetigung)
		if code == "" {
			writeJSON(w, http.StatusPreconditionRequired, map[string]string{
				"error": tr(w, "bitte zur bestätigung pin, passwort oder code eingeben"), "code": "bestaetigen"})
			return
		}
		schluessel := []string{"verwaltung:" + a.ID, quelle(r)}
		frei, warte := s.bremse.anfangen(schluessel...)
		if !frei {
			fail(w, http.StatusTooManyRequests, wartetext(w, warte))
			return
		}
		err := s.codePruefen(r, a, code, true)
		s.bremse.beenden(err == nil || errors.Is(err, errErstPin), schluessel...)
		if errors.Is(err, errErstPin) {
			failCode(w, http.StatusPreconditionFailed, "erst_pin_setzen",
				"Setze zuerst eine PIN oder ein Passwort für dein Profil — dann lassen sich Rechte bestätigen.")
			return
		}
		if err != nil {
			s.st.Log(r.Context(), "warn", "verwaltung", "Bestätigung für eine Rechte-Änderung fehlgeschlagen")
			writeJSON(w, http.StatusForbidden, map[string]string{"error": tr(w, "das stimmt nicht"), "code": "bestaetigen"})
			return
		}
		h(w, r)
	}
}

func (s *Server) adminSperren(w http.ResponseWriter, r *http.Request) {
	sperre.schliessen(accountOf(r).ID)
	w.WriteHeader(http.StatusNoContent)
}

// adminSperreTOTP beginnt die Einrichtung einer Authenticator-App: ein
// frisches Geheimnis, als QR-Code und zum Abtippen. Gespeichert wird es
// erst, wenn ein Code daraus stimmt.
func (s *Server) adminSperreTOTP(w http.ResponseWriter, r *http.Request) {
	a := accountOf(r)
	g, err := totp.NeuesGeheimnis()
	if err != nil {
		failErr(w, err)
		return
	}
	sperre.mu.Lock()
	sperre.anlauf[a.ID] = g
	sperre.mu.Unlock()
	png, err := totp.QR(totp.Adresse(g, a.Name))
	if err != nil {
		failErr(w, err)
		return
	}
	ok(w, map[string]any{
		"geheimnis": g,
		"qr":        "data:image/png;base64," + base64.StdEncoding.EncodeToString(png),
		"app":       "Google Authenticator, Microsoft Authenticator, Aegis, 1Password …",
	})
}

// adminSperreSetzen legt die eigene Sperre fest. Nur die eigene: niemand
// setzt die Sperre einer anderen Person.
func (s *Server) adminSperreSetzen(w http.ResponseWriter, r *http.Request) {
	a := accountOf(r)
	var in struct {
		Kind   string `json:"kind"`
		Secret string `json:"secret"`
		Code   string `json:"code"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	var err error
	switch in.Kind {
	case "none", "pin", "password":
		err = s.st.VerwaltungsSperreSetzen(r.Context(), a.ID, in.Kind, in.Secret)
	case "totp":
		sperre.mu.Lock()
		g := sperre.anlauf[a.ID]
		sperre.mu.Unlock()
		if g == "" {
			fail(w, http.StatusBadRequest, "zuerst den QR-Code holen")
			return
		}
		if !totp.Stimmt(g, in.Code, time.Now()) {
			fail(w, http.StatusForbidden, "der Code stimmt nicht — Uhrzeit auf dem Handy richtig?")
			return
		}
		err = s.st.VerwaltungsSperreSetzen(r.Context(), a.ID, "totp", g)
		sperre.mu.Lock()
		delete(sperre.anlauf, a.ID)
		sperre.mu.Unlock()
	default:
		fail(w, http.StatusBadRequest, "sperre: none, pin, password oder totp")
		return
	}
	if err != nil {
		failErr(w, err)
		return
	}
	s.st.Log(r.Context(), "info", "verwaltung", "Eine Verwaltungssperre wurde geändert")
	// Die Sitzung bleibt offen, damit die Person nicht gleich aussperrt wird.
	ok(w, map[string]any{"sperre": in.Kind, "token": sperre.token(a.ID)})
}

// ---------------------------------------------------------------- Gruppen

func (s *Server) adminGruppen(w http.ResponseWriter, r *http.Request) {
	g, err := s.st.Gruppen(r.Context())
	if err != nil {
		failErr(w, err)
		return
	}
	ok(w, g)
}

func (s *Server) adminGruppeAnlegen(w http.ResponseWriter, r *http.Request) {
	var in store.Gruppe
	if !readJSON(w, r, &in) {
		return
	}
	if in.Limits.MaxStreams != 0 {
		if err := store.PruefeGrenzen(in.Limits); err != nil {
			failErr(w, err)
			return
		}
	}
	g, err := s.st.GruppeAnlegen(r.Context(), in)
	if err != nil {
		failErr(w, err)
		return
	}
	s.st.Log(r.Context(), "info", "verwaltung", "Gruppe angelegt: "+g.Name)
	writeJSON(w, http.StatusCreated, g)
}

func (s *Server) adminGruppeAendern(w http.ResponseWriter, r *http.Request) {
	var in store.Gruppe
	if !readJSON(w, r, &in) {
		return
	}
	in.ID = r.PathValue("id")
	if _, rolle := store.RollenGruppe(in.ID); rolle {
		fail(w, http.StatusConflict, "Diese Gruppe gehört zu einer Rolle. Rechte vergibst du unter Konten und Rechte.")
		return
	}
	if in.Limits.MaxStreams != 0 {
		if err := store.PruefeGrenzen(in.Limits); err != nil {
			failErr(w, err)
			return
		}
	}
	if err := s.st.GruppeAendern(r.Context(), in); err != nil {
		if rechteFehler(w, err) {
			return
		}
		failErr(w, err)
		return
	}
	g, err := s.st.Gruppe(r.Context(), in.ID)
	if err != nil {
		failErr(w, err)
		return
	}
	ok(w, g)
}

func (s *Server) adminGruppeLoeschen(w http.ResponseWriter, r *http.Request) {
	if _, rolle := store.RollenGruppe(r.PathValue("id")); rolle {
		fail(w, http.StatusConflict, "Diese Gruppe gehört zu einer Rolle. Rechte vergibst du unter Konten und Rechte.")
		return
	}
	if err := s.st.GruppeLoeschen(r.Context(), r.PathValue("id")); err != nil {
		failErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// adminKontoGruppe steckt ein Konto in eine Gruppe ({"groupId": ""} = raus).
// adminKontoRolle: Rechte direkt am Konto vergeben — nur schauen,
// mitverwalten oder Vollzugriff (alle Rechte wie der Besitzer).
func (s *Server) adminKontoRolle(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Rolle string `json:"rolle"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if err := s.st.RolleSetzen(r.Context(), r.PathValue("id"), strings.TrimSpace(in.Rolle)); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			fail(w, http.StatusNotFound, "konto nicht gefunden")
			return
		}
		if rechteFehler(w, err) {
			return
		}
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	s.st.Log(r.Context(), "info", "verwaltung", "Rechte eines Kontos geändert: "+in.Rolle)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) adminKontoGruppe(w http.ResponseWriter, r *http.Request) {
	var in struct {
		GroupID string `json:"groupId"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if _, rolle := store.RollenGruppe(strings.TrimSpace(in.GroupID)); rolle {
		fail(w, http.StatusConflict, "Diese Gruppe gehört zu einer Rolle. Rechte vergibst du unter Konten und Rechte.")
		return
	}
	if err := s.st.GruppeZuweisen(r.Context(), r.PathValue("id"), strings.TrimSpace(in.GroupID)); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			fail(w, http.StatusNotFound, "gruppe nicht gefunden")
			return
		}
		if rechteFehler(w, err) {
			return
		}
		failErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
