// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

// PIN und Passwort eines Profils ändern, und was dazugehört.
//
// Bis hierhin liess sich der Riegel eines Profils nach dem Anlegen nicht
// mehr ändern: Wer seinen PIN vergass, war draussen, und wer ihn
// weitergeben musste, konnte ihn nicht mehr wechseln.
//
//   - PUT /me/sperre: das eigene Profil, mit dem alten Code.
//   - PUT /admin/accounts/{id}/sperre: die Verwaltung setzt den Code eines
//     Haushaltsprofils neu („PIN zurücksetzen"), mit Bestätigung — nie den
//     des Besitzers, und ein Mitverwalter nur bei Konten ohne Verwaltung.
//
// Ein Konto mit Verwaltungsrecht kann den Riegel nicht entfernen
// (409 verwaltung_braucht_sperre): Ohne ihn öffnete zu Hause jeder mit
// einem Klick die Verwaltung.
//
// Zurücksetzen durch die Verwaltung leert das Profil (store/vergessen.go):
// Sonst könnte die Verwaltung eine neue PIN setzen, sich als das Profil
// anmelden und dessen Verlauf lesen — genau das, was Lumen nie zeigen
// soll. Die Person erfährt es beim nächsten Anmelden (pinHinweis).

import (
	"context"
	"crypto/rand"
	"errors"
	mrand "math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"lumen/internal/store"
)

// failCode antwortet mit Text und einem festen Code, an dem die Apps die
// Art des Fehlers erkennen, ohne den Text zu lesen.
func failCode(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]string{"error": tr(w, msg), "code": code})
}

// sperreFehler übersetzt die Fehler beim Setzen eines Riegels.
func sperreFehler(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrSperreNoetig):
		failCode(w, http.StatusConflict, "verwaltung_braucht_sperre",
			"Ein Profil mit Zugang zur Verwaltung braucht eine PIN oder ein Passwort.")
	default:
		failErr(w, err)
	}
}

// meineSperre: PUT /api/v1/me/sperre {"alt","art","neu"} → 204.
func (s *Server) meineSperre(w http.ResponseWriter, r *http.Request) {
	acc := accountOf(r)
	var in struct {
		Alt string `json:"alt"`
		Art string `json:"art"`
		Neu string `json:"neu"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if in.Art != "none" && in.Art != "pin" && in.Art != "password" {
		fail(w, http.StatusBadRequest, "art: pin, password oder none")
		return
	}
	// Der alte Code ist ein Ratespiel wie beim Entsperren — und zwar
	// dasselbe: gleicher Zähler wie bei der Anmeldung dieses Profils
	// (profilBremse). Mit einem eigenen Zähler bekäme, wer ein offenes
	// Profil vor sich hat, doppelt so viele Versuche am PIN.
	schluessel := s.profilBremse(r, acc.ID, "")
	frei, warte := s.bremse.anfangen(schluessel...)
	if !frei {
		fail(w, http.StatusTooManyRequests, wartetext(w, warte))
		return
	}
	err := s.st.CheckSecret(r.Context(), acc.ID, in.Alt)
	s.bremse.beenden(err == nil, schluessel...)
	if err != nil {
		if errors.Is(err, store.ErrLocked) {
			failCode(w, http.StatusForbidden, "falsch", altFalschText(acc.LockKind))
			return
		}
		failErr(w, err)
		return
	}
	if err := s.st.SetSecret(r.Context(), acc.ID, in.Art, in.Neu); err != nil {
		sperreFehler(w, err)
		return
	}
	// Wer den alten PIN kannte und sich damit auf einem anderen Gerät
	// angemeldet hat, ist jetzt draussen. Das Gerät, an dem gerade
	// geändert wird, bleibt angemeldet.
	if dev := deviceOf(r); dev != nil {
		if err := s.st.AndereGeraeteAbmelden(r.Context(), acc.ID, dev.TokenHash); err != nil {
			failErr(w, err)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func altFalschText(art string) string {
	if art == "password" {
		return "Das bisherige Passwort stimmt nicht."
	}
	return "Die bisherige PIN stimmt nicht."
}

// adminKontoSperre: PUT /api/v1/admin/accounts/{id}/sperre {"art","neu"}.
// Läuft hinter bestaetigt — wer kurz vom Rechner weg ist, soll niemanden
// den PIN eines anderen ändern lassen.
func (s *Server) adminKontoSperre(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Art string `json:"art"`
		Neu string `json:"neu"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	id := r.PathValue("id")
	ziel, err := s.st.Account(r.Context(), id)
	if err != nil {
		failErr(w, err)
		return
	}
	if ziel.IsOwner {
		fail(w, http.StatusForbidden, "die pin des besitzers ändert nur er selbst")
		return
	}
	if ziel.ID == accountOf(r).ID {
		fail(w, http.StatusBadRequest, "das eigene profil schützt du unter „Ich“")
		return
	}
	if err := s.mitverwalterDarf(r, id, nil); err != nil {
		fail(w, http.StatusForbidden, err.Error())
		return
	}
	// Die Verwaltung wählt keine PIN mehr: Lumen vergibt einen Einmal-Code,
	// mit dem sich die Person einmal anmeldet und dabei ihre eigene PIN
	// festlegen muss (unlock, „neue_pin_noetig"). Ältere Apps schicken
	// noch eine selbst gewählte PIN — sie gilt dann genauso nur einmal.
	art, code := in.Art, in.Neu
	if art == "" {
		art = "pin"
	}
	if art != "none" && code == "" {
		art, code = "pin", einmalCode()
	}
	if art == "none" {
		code = ""
	}
	// Neuer Riegel, Verlauf weg, alle Anmeldungen weg — in einem Schritt.
	if err := s.st.PinZuruecksetzen(r.Context(), id, art, code); err != nil {
		sperreFehler(w, err)
		return
	}
	// Auch aus der WAL-Datei und den Sicherungen: Gelöscht heisst gelöscht.
	s.privatesAusschreiben(id)
	// Was gerade läuft, endet auch: Die Anmeldung dazu gibt es nicht mehr.
	// Aber nicht sofort und nicht auf einen Schlag sichtbar: Fiele die Zahl
	// der Wiedergaben im selben Augenblick, wüsste die Verwaltung, dass
	// genau diese Person gerade schaute (siehe spaeterBeenden).
	s.spaeterBeenden(id)
	// Kein Name im Protokoll: wessen PIN neu ist, geht nur die Verwaltung
	// im Moment des Setzens etwas an.
	s.st.Log(r.Context(), "info", "verwaltung", "Der Schutz eines Profils wurde neu gesetzt")
	ok(w, map[string]any{"einmalCode": code, "art": art})
}

// ausschreibenWG: Tests warten damit auf privatesAusschreiben.
var ausschreibenWG sync.WaitGroup

// beendenVerzug: so lange wartet spaeterBeenden — zufällig zwischen 20 und
// 90 Sekunden. Tests setzen es auf null.
var beendenVerzug = func() time.Duration {
	return 20*time.Second + time.Duration(mrand.Int64N(int64(70*time.Second)))
}

// spaeterBeenden beendet die Wiedergaben eines Kontos nach einer
// zufälligen Weile. Die Anmeldungen sind schon weg, das Gerät bekommt
// also keine neuen Daten mehr; nur die Zählung „Aktive Wiedergaben" fällt
// nicht im selben Augenblick, in dem die Verwaltung den Schutz
// zurücksetzt. Sonst wäre das ein Weg zu prüfen, ob jemand gerade schaut.
func (s *Server) spaeterBeenden(kontoID string) {
	v := beendenVerzug()
	if v <= 0 {
		s.live.EndeKonto(kontoID)
		return
	}
	time.AfterFunc(v, func() { s.live.EndeKonto(kontoID) })
}

// einmalCode: sechs zufällige Ziffern.
func einmalCode() string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	out := make([]byte, 6)
	for i, x := range b {
		out[i] = '0' + x%10
	}
	return string(out)
}

// privatesAusschreiben: nach dem Löschen von Privatem die WAL-Datei
// zurückschreiben und die Sicherungen ebenso bereinigen. Im Hintergrund —
// die Sicherungen neu zu schreiben dauert bei einer grossen Datenbank.
func (s *Server) privatesAusschreiben(kontoID string) {
	ausschreibenWG.Add(1)
	go func() {
		defer ausschreibenWG.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		if err := s.st.Ausschreiben(ctx); err != nil && s.log != nil {
			s.log.Warn("WAL-Datei liess sich nicht zurückschreiben", "err", err)
		}
		if kontoID == "" {
			return
		}
		if err := store.SicherungenVergessen(ctx, s.sicherungsOrdner(), kontoID); err != nil {
			s.st.Log(ctx, "error", "sicherung", "Eine Sicherung liess sich nicht bereinigen: "+err.Error())
		}
	}()
}

// pinHinweisText: was die Person nach einem Zurücksetzen durch die
// Verwaltung einmal zu sehen bekommt.
const pinHinweisTitelText = "PIN zurückgesetzt"
const pinHinweisText = "Deine PIN wurde von der Verwaltung zurückgesetzt; dein Verlauf wurde dabei gelöscht."

// pinHinweis sagt, ob und wann die Verwaltung den PIN dieses Profils
// zurückgesetzt hat und die Person das noch nicht gelesen hat.
func (s *Server) pinHinweis(ctx context.Context, accountID string) (time.Time, bool) {
	prefs, err := s.st.Prefs(ctx, accountID)
	if err != nil {
		return time.Time{}, false
	}
	v, da := prefs[store.PrefPinZurueckgesetzt]
	if !da {
		return time.Time{}, false
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return time.Unix(0, 0), true
	}
	return time.Unix(n, 0), true
}

// profilBremse: die Schlüssel der Bremse für Rateversuche am PIN eines
// Profils. Derselbe Zähler für Anmelden und PIN-Ändern; zusätzlich je
// Absender.
//
// Welcher Zähler gilt, richtet sich nur nach dem, was sich nicht fälschen
// lässt: Kam die Anfrage über den Vermittler (VonAussen), zählt der Zähler
// „Vermittler" — so sperrt ein Fremder, der von aussen den PIN von „Alex"
// rät, nicht die Profilauswahl im Wohnzimmer. Alles andere zählt am
// Zähler der direkten Verbindung, und der Absender ist die echte Adresse
// der Verbindung. Köpfe wie X-Forwarded-For machen eine Anfrage zwar
// strenger zu „unterwegs" (vonAussen), wählen aber nie den Zähler: Früher
// schickte, wer zu Hause ausgesperrt war, einfach den Kopf mit und riet im
// frischen Zähler weiter.
// name: wenn das Profil (von unterwegs) nur über den Namen bekannt ist.
func (s *Server) profilBremse(r *http.Request, id, name string) []string {
	weg := ":direkt"
	if v, _ := r.Context().Value(ctxVonAussen).(bool); v {
		weg = ":vermittler"
	}
	profil := "profil:" + id
	if id == "" {
		profil = "profil:?" + strings.ToLower(strings.TrimSpace(name))
	}
	return []string{profil + weg, quelle(r)}
}

// besitzerName nennt, wer bei einem vergessenen PIN helfen kann.
func (s *Server) besitzerName(ctx context.Context) string {
	accs, err := s.st.Accounts(ctx)
	if err != nil {
		return ""
	}
	for _, a := range accs {
		if a.IsOwner {
			return a.Name
		}
	}
	return ""
}

// entsperrenFalsch: Antwort auf einen falschen PIN zu Hause. Text, Art
// und wer helfen kann — die Oberfläche baut daraus „Die PIN stimmt nicht.
// Bitte noch einmal." und den Hinweis auf den Besitzer. Code "falsch"
// unterscheidet das von einer abgelaufenen Anmeldung, die auch 401 ist.
func (s *Server) entsperrenFalsch(w http.ResponseWriter, r *http.Request, art string) {
	text := "Die PIN stimmt nicht. Bitte noch einmal."
	if art == "password" {
		text = "Das Passwort stimmt nicht. Bitte noch einmal."
	}
	writeJSON(w, http.StatusUnauthorized, map[string]string{
		"error": tr(w, text), "code": "falsch", "art": art, "hilfe": s.besitzerName(r.Context()),
	})
}

// errErstPin: bestätigen lässt sich nur, wer überhaupt einen Code hat.
var errErstPin = errors.New("erst eine pin setzen")

// rechteFehler beantwortet die Gründe, aus denen ein Konto keine
// Verwaltung bekommt, mit festen Codes. false: kein solcher Fehler.
func rechteFehler(w http.ResponseWriter, err error) bool {
	switch {
	case errors.Is(err, store.ErrSperreNoetig):
		failCode(w, http.StatusConflict, "verwaltung_braucht_sperre",
			"Verwaltung bekommt nur ein Profil mit PIN oder Passwort. Setze zuerst eine PIN für dieses Profil.")
	case errors.Is(err, store.ErrKindOhneVerwaltung):
		failCode(w, http.StatusConflict, "kind_ohne_verwaltung", "Ein Kinderprofil bekommt keine Verwaltung.")
	default:
		return false
	}
	return true
}
