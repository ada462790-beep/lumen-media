// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lumen/internal/store"
	"lumen/internal/stream"
)

// PIN ändern: mit dem alten Code, sonst 403 „falsch".
func TestEigenePinAendern(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()
	lea, _ := h.st.CreateAccount(ctx, store.NewAccount{Name: "Lea", LockKind: "pin", Secret: "1234"})
	ltok, _ := h.st.IssueToken(ctx, lea.ID, "iPad", "tablet")

	resp, raw := h.do("PUT", "/api/v1/me/sperre", `{"alt":"9999","art":"pin","neu":"5678"}`, ltok)
	if resp.StatusCode != 403 || !strings.Contains(string(raw), `"code":"falsch"`) {
		t.Fatalf("falscher alter PIN: %d %s", resp.StatusCode, raw)
	}
	if resp, raw := h.do("PUT", "/api/v1/me/sperre", `{"alt":"1234","art":"pin","neu":"5678"}`, ltok); resp.StatusCode != 204 {
		t.Fatalf("PIN ändern: %d %s", resp.StatusCode, raw)
	}
	if h.st.CheckSecret(ctx, lea.ID, "5678") != nil {
		t.Fatal("neue PIN gilt nicht")
	}
	// Ohne Schutz geht für ein gewöhnliches Profil …
	if resp, raw := h.do("PUT", "/api/v1/me/sperre", `{"alt":"5678","art":"none","neu":""}`, ltok); resp.StatusCode != 204 {
		t.Fatalf("Schutz entfernen: %d %s", resp.StatusCode, raw)
	}
	// … aber nicht für den Besitzer.
	resp, raw = h.do("PUT", "/api/v1/me/sperre", `{"alt":"einsehrgutespasswort","art":"none","neu":""}`, h.tok)
	if resp.StatusCode != 409 || !strings.Contains(string(raw), "verwaltung_braucht_sperre") {
		t.Fatalf("Besitzer ohne Schutz: %d %s", resp.StatusCode, raw)
	}
}

// Die Verwaltung setzt die PIN eines Haushaltsprofils zurück — mit
// Bestätigung, und nie die des Besitzers.
func TestPinZuruecksetzen(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()
	lea, _ := h.st.CreateAccount(ctx, store.NewAccount{Name: "Lea", LockKind: "pin", Secret: "1234"})
	h.ohneBestaetigung = true
	if resp, _ := h.do("PUT", "/api/v1/admin/accounts/"+lea.ID+"/sperre", `{"art":"pin","neu":"4321"}`, h.tok); resp.StatusCode != 428 {
		t.Fatalf("ohne Bestätigung: %d", resp.StatusCode)
	}
	h.ohneBestaetigung = false
	if resp, raw := h.do("PUT", "/api/v1/admin/accounts/"+lea.ID+"/sperre", `{"art":"pin","neu":"4321"}`, h.tok); resp.StatusCode != 200 {
		t.Fatalf("zurücksetzen: %d %s", resp.StatusCode, raw)
	}
	if h.st.CheckSecret(ctx, lea.ID, "4321") != nil {
		t.Fatal("zurückgesetzte PIN gilt nicht")
	}

	// Jemand mit Vollzugriff kann die des Besitzers nicht ändern.
	mia, _ := h.st.CreateAccount(ctx, store.NewAccount{Name: "Mia", LockKind: "pin", Secret: "2468"})
	_ = h.st.RolleSetzen(ctx, mia.ID, "voll")
	mtok, _ := h.st.IssueToken(ctx, mia.ID, "Mac", "desktop")
	besitzer := ""
	accs, _ := h.st.Accounts(ctx)
	for _, a := range accs {
		if a.IsOwner {
			besitzer = a.ID
		}
	}
	req, _ := http.NewRequest("PUT", h.srv.URL+"/api/v1/admin/accounts/"+besitzer+"/sperre",
		strings.NewReader(`{"art":"pin","neu":"0000"}`))
	req.Header.Set("Authorization", "Bearer "+mtok)
	req.Header.Set("X-Lumen-Bestaetigung", "2468")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Fatalf("Vollzugriff ändert die PIN des Besitzers: %d", resp.StatusCode)
	}
}

// Ein Verwaltungsprofil ohne PIN (aus alten Daten) kommt nicht in die
// Verwaltung, und eine Bestätigung ohne jeden Code geht nicht durch.
func TestVerwaltungOhneProfilsperre(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()
	jo, _ := h.st.CreateAccount(ctx, store.NewAccount{Name: "Jo", LockKind: "pin", Secret: "1357"})
	_ = h.st.RolleSetzen(ctx, jo.ID, "voll")
	// So sah es vor dieser Fassung aus: Verwaltung, aber kein PIN.
	if _, err := h.st.DB().Exec(`UPDATE account SET lock_kind='none', secret_hash=NULL, secret_salt=NULL WHERE id=?`, jo.ID); err != nil {
		t.Fatal(err)
	}
	// Zu Hause öffnet die leere PIN das Profil …
	resp, raw := h.do("POST", "/api/v1/unlock", fmt.Sprintf(`{"profileId":%q,"secret":""}`, jo.ID), "")
	var out struct{ Token string }
	_ = json.Unmarshal(raw, &out)
	if resp.StatusCode != 200 || out.Token == "" {
		t.Fatalf("Profil öffnen: %d %s", resp.StatusCode, raw)
	}
	// … aber nicht die Verwaltung.
	resp, raw = h.do("GET", "/api/v1/admin/overview", "", out.Token)
	if resp.StatusCode != 403 || !strings.Contains(string(raw), "profil_sperre_fehlt") {
		t.Fatalf("Verwaltung ohne PIN: %d %s", resp.StatusCode, raw)
	}
	_, raw = h.do("GET", "/api/v1/admin/lock", "", out.Token)
	if !strings.Contains(string(raw), `"profilSperre":"none"`) {
		t.Fatalf("Sperre-Auskunft: %s", raw)
	}
	// Rechte bestätigen mit irgendeinem Text: früher durch, jetzt nicht.
	lea, _ := h.st.CreateAccount(ctx, store.NewAccount{Name: "Lea"})
	req, _ := http.NewRequest("PUT", h.srv.URL+"/api/v1/admin/accounts/"+lea.ID+"/rolle", strings.NewReader(`{"rolle":"voll"}`))
	req.Header.Set("Authorization", "Bearer "+out.Token)
	req.Header.Set("X-Lumen-Bestaetigung", "irgendwas")
	r2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	r2.Body.Close()
	if r2.StatusCode == 204 {
		t.Fatal("Bestätigung ohne Code ging durch")
	}
	// Rolle für ein Profil ohne PIN: abgelehnt.
	resp, raw = h.do("PUT", "/api/v1/admin/accounts/"+lea.ID+"/rolle", `{"rolle":"verwalten"}`, h.tok)
	if resp.StatusCode != 409 || !strings.Contains(string(raw), "verwaltung_braucht_sperre") {
		t.Fatalf("Rolle ohne PIN: %d %s", resp.StatusCode, raw)
	}
}

// Falscher PIN zu Hause: eine Meldung, die sagt, was nicht stimmt — und
// nicht „Anmeldung abgelaufen".
func TestFalscherPinSagtWasLos(t *testing.T) {
	h := newHarness(t)
	lea, _ := h.st.CreateAccount(t.Context(), store.NewAccount{Name: "Lea", LockKind: "pin", Secret: "1234"})
	resp, raw := h.do("POST", "/api/v1/unlock", fmt.Sprintf(`{"profileId":%q,"secret":"0000"}`, lea.ID), "")
	var out map[string]string
	_ = json.Unmarshal(raw, &out)
	if resp.StatusCode != 401 || out["code"] != "falsch" || out["art"] != "pin" || out["hilfe"] != "Alex" {
		t.Fatalf("falscher PIN: %d %s", resp.StatusCode, raw)
	}
}

// Fehlversuche von aussen sperren die Anmeldung zu Hause nicht.
func TestBremseTrenntAussenUndZuHause(t *testing.T) {
	h := newHarness(t)
	lea, _ := h.st.CreateAccount(t.Context(), store.NewAccount{Name: "Lea", LockKind: "pin", Secret: "1234"})
	aussen := VonAussen(h.api.Handler())
	for i := 0; i < 5; i++ {
		r := httptest.NewRequest("POST", "/api/v1/unlock", strings.NewReader(`{"name":"Lea","secret":"0000"}`))
		r.Host = "localhost"
		w := httptest.NewRecorder()
		aussen.ServeHTTP(w, r)
	}
	resp, raw := h.do("POST", "/api/v1/unlock", fmt.Sprintf(`{"profileId":%q,"secret":"1234"}`, lea.ID), "")
	if resp.StatusCode != 200 {
		t.Fatalf("zu Hause nach Fehlversuchen von aussen: %d %s", resp.StatusCode, raw)
	}
}

// Codes von unterwegs: begrenzt, und die Freigabe sagt, woher sie kommen.
func TestCodeVonUnterwegs(t *testing.T) {
	h := newHarness(t)
	aussen := VonAussen(h.api.Handler())
	start := func() (int, map[string]any) {
		r := httptest.NewRequest("POST", "/api/v1/code/start", strings.NewReader(`{"deviceName":"Fremder TV"}`))
		r.Host = "localhost"
		w := httptest.NewRecorder()
		aussen.ServeHTTP(w, r)
		var out map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return w.Code, out
	}
	code, erst := start()
	if code != 200 {
		t.Fatalf("Code von unterwegs: %d", code)
	}
	resp, raw := h.do("POST", "/api/v1/code/approve", fmt.Sprintf(`{"code":%q,"pruefen":true}`, erst["code"]), h.tok)
	if resp.StatusCode != 200 || !strings.Contains(string(raw), `"unterwegs":true`) {
		t.Fatalf("Prüfen: %d %s", resp.StatusCode, raw)
	}
	// Prüfen gibt nicht frei.
	r := httptest.NewRequest("GET", "/api/v1/code/"+erst["geheim"].(string), nil)
	r.Host = "localhost"
	w := httptest.NewRecorder()
	aussen.ServeHTTP(w, r)
	if w.Code != 202 {
		t.Fatalf("nach dem Prüfen schon freigegeben: %d", w.Code)
	}
	gebremst := false
	for i := 0; i < 40; i++ {
		if c, _ := start(); c == 429 {
			gebremst = true
			break
		}
	}
	if !gebremst {
		t.Fatal("beliebig viele Codes von unterwegs")
	}
	codes.mu.Lock()
	codes.nachCode, codes.nachGeh, codes.vonAussen = map[string]*codeAnfrage{}, map[string]*codeAnfrage{}, nil
	codes.mu.Unlock()
}

// Einladung als Kinderprofil, und unmögliche Grenzen werden abgelehnt.
func TestEinladungKinderprofilUndGrenzen(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()
	resp, raw := h.do("POST", "/api/v1/admin/invites", `{"kinderprofil":true,"altersgrenze":7}`, h.tok)
	if resp.StatusCode != 400 {
		t.Fatalf("Altersgrenze 7: %d %s", resp.StatusCode, raw)
	}
	resp, raw = h.do("POST", "/api/v1/admin/invites", `{"kinderprofil":true,"altersgrenze":0}`, h.tok)
	var inv store.Invite
	_ = json.Unmarshal(raw, &inv)
	if resp.StatusCode != 200 || inv.Code == "" {
		t.Fatalf("Einladung: %d %s", resp.StatusCode, raw)
	}
	resp, raw = h.do("POST", "/api/v1/invite/redeem", fmt.Sprintf(`{"code":%q,"name":"alex","lockKind":"none"}`, inv.Code), "")
	if resp.StatusCode != 409 || !strings.Contains(string(raw), "name_vergeben") {
		t.Fatalf("Name des Besitzers: %d %s", resp.StatusCode, raw)
	}
	resp, raw = h.do("POST", "/api/v1/invite/redeem", fmt.Sprintf(`{"code":%q,"name":"Nina","lockKind":"none"}`, inv.Code), "")
	var red struct {
		Account store.Account
	}
	_ = json.Unmarshal(raw, &red)
	if resp.StatusCode != 200 || !red.Account.IsKid {
		t.Fatalf("Einlösen: %d %s", resp.StatusCode, raw)
	}
	if l, _ := h.st.Limits(ctx, red.Account.ID); l.MaxAge != 0 || l.AllowUnrated {
		t.Fatalf("Grenzen des Kindes: %+v", l)
	}
	// Grenzen am Konto: 0 gleichzeitig gibt es nicht; ein Kind bleibt Kind.
	resp, _ = h.do("PUT", "/api/v1/admin/accounts/"+red.Account.ID+"/limits",
		`{"limits":{"maxStreams":0,"maxAge":0}}`, h.tok)
	if resp.StatusCode != 400 {
		t.Fatalf("0 gleichzeitig: %d", resp.StatusCode)
	}
	resp, _ = h.do("PUT", "/api/v1/admin/accounts/"+red.Account.ID+"/limits",
		`{"limits":{"maxStreams":1,"maxAge":-1,"allowUnrated":true}}`, h.tok)
	if resp.StatusCode != 400 {
		t.Fatalf("Kind ohne Altersgrenze: %d", resp.StatusCode)
	}
	resp, raw = h.do("PUT", "/api/v1/admin/accounts/"+red.Account.ID+"/limits",
		`{"limits":{"maxStreams":1,"maxAge":-1,"allowUnrated":true},"kinderprofil":false}`, h.tok)
	if resp.StatusCode != 204 {
		t.Fatalf("Kinderprofil aufheben: %d %s", resp.StatusCode, raw)
	}
}

// Kinderschutz über die Schnittstelle: nur FSK 0, Folgen mit eigener
// Freigabe, Listen, Personenbilder.
func TestKinderschutzUeberDieSchnittstelle(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()
	h.film("Erster Film", 1)
	lib := h.lib
	klein, gross := 0, 16
	frei := &store.Item{LibraryID: lib.ID, Kind: "movie", Title: "Sandmann", AgeYears: &klein, MatchState: "matched"}
	hart := &store.Item{LibraryID: lib.ID, Kind: "movie", Title: "Hart", AgeYears: &gross, MatchState: "matched"}
	_ = h.st.UpsertItem(ctx, frei)
	_ = h.st.UpsertItem(ctx, hart)
	_ = h.st.BesetzungSetzen(ctx, hart.ID, []store.Person{{Name: "Geheime Person"}})
	// Ein Bild liegt bereit — ohne die Prüfung käme es durch.
	_ = h.st.SetPersonInfo(ctx, "Geheime Person", `{"bildDatei":"person-1.jpg"}`)
	_ = os.MkdirAll(h.api.meta.ArtDir(), 0o755)
	_ = os.WriteFile(filepath.Join(h.api.meta.ArtDir(), "person-1.jpg"), []byte("jpg"), 0o644)
	if resp, _ := h.do("GET", "/api/v1/persons/Geheime%20Person/bild", "", h.tok); resp.StatusCode != 200 {
		t.Fatalf("Besitzer sieht das Bild nicht: %d", resp.StatusCode)
	}

	lim, _ := store.KinderGrenzen(0)
	kind, _ := h.st.CreateAccount(ctx, store.NewAccount{Name: "Nina", IsKid: true, Limits: lim, Grants: []string{lib.ID}})
	ktok, _ := h.st.IssueToken(ctx, kind.ID, "iPad", "tablet")

	_, raw := h.do("GET", "/api/v1/items", "", ktok)
	if !strings.Contains(string(raw), "Sandmann") || strings.Contains(string(raw), "Hart") || strings.Contains(string(raw), "Erster Film") {
		t.Fatalf("FSK 0 sieht: %s", raw)
	}
	if resp, _ := h.do("GET", "/api/v1/items/"+hart.ID, "", ktok); resp.StatusCode != 404 {
		t.Fatalf("Titel ab 16: %d", resp.StatusCode)
	}
	if resp, _ := h.do("GET", "/api/v1/persons/Geheime%20Person/bild", "", ktok); resp.StatusCode != 404 {
		t.Fatalf("Personenbild aus verborgenem Titel: %d", resp.StatusCode)
	}

	// Eine geteilte, gemeinsame Liste des Besitzers mit einem Titel ab 16:
	// das Kind kann ihn nicht herausnehmen.
	_, raw = h.do("POST", "/api/v1/playlists", `{"name":"Alle"}`, h.tok)
	var l struct{ ID string }
	_ = json.Unmarshal(raw, &l)
	h.do("POST", "/api/v1/playlists/"+l.ID, `{"geteilt":true,"gemeinsam":true}`, h.tok)
	if resp, raw := h.do("POST", "/api/v1/playlists/"+l.ID+"/items", fmt.Sprintf(`{"itemId":%q}`, hart.ID), h.tok); resp.StatusCode != 204 {
		t.Fatalf("Liste füllen: %d %s", resp.StatusCode, raw)
	}
	if resp, _ := h.do("DELETE", "/api/v1/playlists/"+l.ID+"/items/"+hart.ID, "", ktok); resp.StatusCode != 404 {
		t.Fatalf("Kind nimmt verborgenen Titel heraus: %d", resp.StatusCode)
	}

	// Serie ab 0 mit einer Folge ab 16.
	serie := &store.Item{LibraryID: lib.ID, Kind: "series", Title: "Lieb", AgeYears: &klein, MatchState: "matched"}
	_ = h.st.UpsertItem(ctx, serie)
	folge := &store.Item{LibraryID: lib.ID, Kind: "episode", Title: "Wild", ParentID: serie.ID, AgeYears: &gross, MatchState: "matched"}
	_ = h.st.UpsertItem(ctx, folge)
	if resp, _ := h.do("GET", "/api/v1/items/"+folge.ID, "", ktok); resp.StatusCode != 404 {
		t.Fatalf("Folge ab 16 in Serie ab 0: %d", resp.StatusCode)
	}
}

// Mitverwalter stellen die Metadaten-Schlüssel nicht um; Kennungen für
// TMDb sind Zahlen.
func TestMetadatenNurBesitzer(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()
	jo, _ := h.st.CreateAccount(ctx, store.NewAccount{Name: "Jo", LockKind: "pin", Secret: "1357"})
	_ = h.st.RolleSetzen(ctx, jo.ID, "verwalten")
	jtok, _ := h.st.IssueToken(ctx, jo.ID, "Mac", "desktop")
	if resp, _ := h.do("PUT", "/api/v1/admin/metadata", `{"tmdbKey":"fremd"}`, jtok); resp.StatusCode != 404 {
		t.Fatalf("Mitverwalter setzt Schlüssel: %d", resp.StatusCode)
	}
	if resp, raw := h.do("GET", "/api/v1/admin/metadata", "", jtok); resp.StatusCode != 200 {
		t.Fatalf("lesen: %d %s", resp.StatusCode, raw)
	}
}

// Kein Token mehr in der Adresse; persönliche Antworten werden nicht
// zwischengespeichert.
func TestKeinTokenInDerAdresse(t *testing.T) {
	h := newHarness(t)
	_, fileID := h.film("Film", 1)
	if resp, _ := h.do("GET", "/api/v1/stream/"+fileID+"?token="+h.tok, "", ""); resp.StatusCode != 401 {
		t.Fatalf("?token= gilt noch: %d", resp.StatusCode)
	}
	resp, _ := h.do("GET", "/api/v1/me", "", h.tok)
	if resp.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control: %q", resp.Header.Get("Cache-Control"))
	}
}

// Zurücksetzen durch die Verwaltung leert das Profil: Sonst setzte die
// Verwaltung eine neue PIN, meldete sich als das Profil an und läse,
// was es geschaut hat. Das alte Token gilt danach nicht mehr, laufende
// Wiedergaben enden, und die Person erfährt es beim nächsten Anmelden.
func TestPinZuruecksetzenVergisstVerlauf(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()
	film, _ := h.film("Matrix", 1)
	zweiter, _ := h.film("Amélie", 1)
	lea, _ := h.st.CreateAccount(ctx, store.NewAccount{Name: "Lea", LockKind: "pin", Secret: "1234"})
	_ = h.st.SetGrants(ctx, lea.ID, []string{h.lib.ID})
	ltok, _ := h.st.IssueToken(ctx, lea.ID, "iPad", "tablet")
	// Ein anderes Profil zum Vergleich: Es behält alles.
	mia, _ := h.st.CreateAccount(ctx, store.NewAccount{Name: "Mia", LockKind: "pin", Secret: "2468"})

	for _, konto := range []string{lea.ID, mia.ID} {
		_ = h.st.SaveProgress(ctx, konto, film, 60_000, 600_000)
		_ = h.st.MarkFinished(ctx, konto, zweiter, 600_000)
		_ = h.st.AddToWatchlist(ctx, konto, film)
		_ = h.st.AddFavorit(ctx, konto, film)
		_ = h.st.SetRating(ctx, konto, film, 4, false)
		_ = h.st.SetPref(ctx, konto, "spuren:serie1", `{"ton":"de"}`)
		_ = h.st.SetPref(ctx, konto, "sprache", "de-CH")
	}
	privat, _ := h.st.ListeAnlegen(ctx, lea.ID, "Nur für mich")
	_ = h.st.ListeHinzufuegen(ctx, lea.ID, privat.ID, film)
	geteilt, _ := h.st.ListeAnlegen(ctx, lea.ID, "Filmabend")
	ja := true
	_ = h.st.ListeAendern(ctx, lea.ID, geteilt.ID, nil, &ja, &ja)
	_ = h.st.ListeHinzufuegen(ctx, mia.ID, geteilt.ID, zweiter)

	// Eine laufende Wiedergabe von Lea.
	h.live.Start(&stream.Session{AccountID: lea.ID, DeviceName: "iPad", FileID: "f", Mode: "direct"})

	beendenVerzug = func() time.Duration { return 0 }
	defer func() { beendenVerzug = func() time.Duration { return 20 * time.Second } }()
	// Die Verwaltung wählt keine PIN: Lumen vergibt einen Einmal-Code.
	resp, raw := h.do("PUT", "/api/v1/admin/accounts/"+lea.ID+"/sperre", `{"art":"pin"}`, h.tok)
	var zurueck struct{ EinmalCode string }
	_ = json.Unmarshal(raw, &zurueck)
	if resp.StatusCode != 200 || len(zurueck.EinmalCode) != 6 {
		t.Fatalf("zurücksetzen: %d %s", resp.StatusCode, raw)
	}
	ausschreibenWG.Wait()

	// Das alte Token gilt nicht mehr.
	if resp, _ := h.do("GET", "/api/v1/me", "", ltok); resp.StatusCode != 401 {
		t.Fatalf("altes Token gilt noch: %d", resp.StatusCode)
	}
	if n := len(h.live.ForAccount(lea.ID)); n != 0 {
		t.Fatalf("Wiedergabe läuft weiter: %d", n)
	}
	// Alles Private ist weg …
	if _, err := h.st.Progress(ctx, lea.ID, film); err == nil {
		t.Fatal("Fortschritt noch da")
	}
	if _, err := h.st.Progress(ctx, lea.ID, zweiter); err == nil {
		t.Fatal("Gesehen noch da")
	}
	if ids, _ := h.st.WatchlistIDs(ctx, lea.ID); len(ids) != 0 {
		t.Fatalf("Merkliste: %v", ids)
	}
	if ids, _ := h.st.FavoritIDs(ctx, lea.ID); len(ids) != 0 {
		t.Fatalf("Favoriten: %v", ids)
	}
	if r, _ := h.st.RatingsFor(ctx, lea.ID, []string{film}); len(r) != 0 {
		t.Fatalf("Bewertung: %v", r)
	}
	prefs, _ := h.st.Prefs(ctx, lea.ID)
	if _, da := prefs["spuren:serie1"]; da || prefs["sprache"] != "de-CH" {
		t.Fatalf("Prefs: %v", prefs)
	}
	if _, err := h.st.Liste(ctx, lea.ID, privat.ID); err == nil {
		t.Fatal("private Liste noch da")
	}
	// … die geteilte Liste mitsamt dem, was Mia hineingelegt hat, bleibt.
	if l, err := h.st.Liste(ctx, mia.ID, geteilt.ID); err != nil || len(l.ItemIDs) != 1 {
		t.Fatalf("geteilte Liste: %+v %v", l, err)
	}
	// Mia ist unberührt.
	if _, err := h.st.Progress(ctx, mia.ID, film); err != nil {
		t.Fatal("Mias Fortschritt ist weg")
	}

	// Mit dem Einmal-Code allein gibt es keine Anmeldung — erst die eigene
	// PIN. Wer den Code kennt (die Verwaltung), kommt so nie an ein
	// Token, ohne dass die Person es merkt, und der Hinweis bleibt für sie.
	code := zurueck.EinmalCode
	resp, raw = h.do("POST", "/api/v1/unlock", fmt.Sprintf(`{"profileId":%q,"secret":%q}`, lea.ID, code), "")
	if resp.StatusCode != 409 || !strings.Contains(string(raw), `"neue_pin_noetig"`) || strings.Contains(string(raw), "token") {
		t.Fatalf("Einmal-Code ohne neue PIN: %d %s", resp.StatusCode, raw)
	}
	// Der Einmal-Code als neue PIN: nein.
	if resp, raw := h.do("POST", "/api/v1/unlock", fmt.Sprintf(`{"profileId":%q,"secret":%q,"neu":%q}`, lea.ID, code, code), ""); resp.StatusCode != 409 {
		t.Fatalf("Einmal-Code als eigene PIN: %d %s", resp.StatusCode, raw)
	}
	// Beim Festlegen: Hinweis in der Antwort und als Neuigkeit.
	resp, raw = h.do("POST", "/api/v1/unlock", fmt.Sprintf(`{"profileId":%q,"secret":%q,"neu":"9753"}`, lea.ID, code), "")
	if resp.StatusCode != 200 || !strings.Contains(string(raw), `"pin_zurueckgesetzt"`) {
		t.Fatalf("Hinweis beim Anmelden: %d %s", resp.StatusCode, raw)
	}
	// Der Einmal-Code gilt nicht mehr.
	if resp, _ := h.do("POST", "/api/v1/unlock", fmt.Sprintf(`{"profileId":%q,"secret":%q}`, lea.ID, code), ""); resp.StatusCode != 401 {
		t.Fatalf("Einmal-Code gilt noch: %d", resp.StatusCode)
	}
	var out struct{ Token string }
	_ = json.Unmarshal(raw, &out)
	_, raw = h.do("GET", "/api/v1/news", "", out.Token)
	if !strings.Contains(string(raw), "Verwaltung zurückgesetzt") {
		t.Fatalf("keine Neuigkeit: %s", raw)
	}
	// Gelesen → weg, auch beim Anmelden.
	h.do("POST", "/api/v1/news/seen", "", out.Token)
	_, raw = h.do("GET", "/api/v1/news", "", out.Token)
	if strings.Contains(string(raw), "Verwaltung zurückgesetzt") {
		t.Fatalf("Hinweis bleibt: %s", raw)
	}
	_, raw = h.do("POST", "/api/v1/unlock", fmt.Sprintf(`{"profileId":%q,"secret":"9753"}`, lea.ID), "")
	if strings.Contains(string(raw), "pin_zurueckgesetzt") || !strings.Contains(string(raw), "token") {
		t.Fatalf("Hinweis zweimal: %s", raw)
	}
}

// Ein zu kurzer neuer PIN wird abgelehnt, bevor irgendetwas gelöscht ist.
func TestPinZuruecksetzenErstPruefen(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()
	film, _ := h.film("Matrix", 1)
	lea, _ := h.st.CreateAccount(ctx, store.NewAccount{Name: "Lea", LockKind: "pin", Secret: "1234"})
	ltok, _ := h.st.IssueToken(ctx, lea.ID, "iPad", "tablet")
	_ = h.st.SaveProgress(ctx, lea.ID, film, 60_000, 600_000)
	if resp, _ := h.do("PUT", "/api/v1/admin/accounts/"+lea.ID+"/sperre", `{"art":"pin","neu":"12"}`, h.tok); resp.StatusCode < 400 {
		t.Fatalf("zu kurzer PIN angenommen: %d", resp.StatusCode)
	}
	if _, err := h.st.Progress(ctx, lea.ID, film); err != nil {
		t.Fatal("Verlauf trotz Fehler gelöscht")
	}
	if resp, _ := h.do("GET", "/api/v1/me", "", ltok); resp.StatusCode != 200 {
		t.Fatal("Token trotz Fehler weg")
	}
}

// Eigene PIN ändern meldet alle anderen Geräte ab, das eigene bleibt.
func TestEigenePinMeldetAndereAb(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()
	lea, _ := h.st.CreateAccount(ctx, store.NewAccount{Name: "Lea", LockKind: "pin", Secret: "1234"})
	hier, _ := h.st.IssueToken(ctx, lea.ID, "Mac", "desktop")
	dort, _ := h.st.IssueToken(ctx, lea.ID, "Fernseher", "tv")
	if resp, raw := h.do("PUT", "/api/v1/me/sperre", `{"alt":"1234","art":"pin","neu":"5678"}`, hier); resp.StatusCode != 204 {
		t.Fatalf("ändern: %d %s", resp.StatusCode, raw)
	}
	if resp, _ := h.do("GET", "/api/v1/me", "", hier); resp.StatusCode != 200 {
		t.Fatal("eigenes Gerät abgemeldet")
	}
	if resp, _ := h.do("GET", "/api/v1/me", "", dort); resp.StatusCode != 401 {
		t.Fatal("anderes Gerät noch angemeldet")
	}
}

// PIN-Ändern und Anmelden teilen sich den Zähler: Wer an einem offenen
// Profil den alten PIN rät, bremst damit auch die Anmeldung.
func TestPinAendernTeiltBremse(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()
	lea, _ := h.st.CreateAccount(ctx, store.NewAccount{Name: "Lea", LockKind: "pin", Secret: "1234"})
	ltok, _ := h.st.IssueToken(ctx, lea.ID, "Mac", "desktop")
	gebremst := false
	for i := 0; i < 12 && !gebremst; i++ {
		resp, _ := h.do("PUT", "/api/v1/me/sperre", `{"alt":"0000","art":"pin","neu":"5678"}`, ltok)
		gebremst = resp.StatusCode == 429
	}
	if !gebremst {
		t.Fatal("keine Bremse beim PIN-Ändern")
	}
	resp, _ := h.do("POST", "/api/v1/unlock", fmt.Sprintf(`{"profileId":%q,"secret":"1234"}`, lea.ID), "")
	if resp.StatusCode != 429 {
		t.Fatalf("Anmelden ungebremst nach Rateversuchen beim Ändern: %d", resp.StatusCode)
	}
}

// Wer zu Hause ausgesperrt ist, kommt mit einem X-Forwarded-For-Kopf
// nicht in einen frischen Zähler (Sicherheitsbericht N-1).
func TestBremseOhneUmwegUeberKopf(t *testing.T) {
	h := newHarness(t)
	lea, _ := h.st.CreateAccount(t.Context(), store.NewAccount{Name: "Lea", LockKind: "pin", Secret: "1234"})
	versuch := func(geheim string, kopf bool) int {
		req, _ := http.NewRequest("POST", h.srv.URL+"/api/v1/unlock",
			strings.NewReader(fmt.Sprintf(`{"profileId":%q,"name":"Lea","secret":%q}`, lea.ID, geheim)))
		if kopf {
			req.Header.Set("X-Forwarded-For", "8.8.8.8")
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	for i := 0; i < 4; i++ {
		versuch("0000", false)
	}
	if c := versuch("1234", false); c != 429 {
		t.Fatalf("zu Hause nicht gebremst: %d", c)
	}
	if c := versuch("1234", true); c != 429 {
		t.Fatalf("mit X-Forwarded-For an der Bremse vorbei: %d", c)
	}
}

// Wer über den Reverse-Proxy (oder einen weitergeleiteten Port) den PIN
// eines Profils rät, sperrt damit nicht den Fernseher im Wohnzimmer: Zu
// Hause zählt ein eigener Zähler. Umgekehrt holt kein Kopf eine Anfrage
// von aussen in den Zähler von zu Hause zurück.
func TestBremseUnterwegsGetrenntVonZuHause(t *testing.T) {
	h := newHarness(t)
	mitGateways(t, "", "172.17.0.1")
	lea, _ := h.st.CreateAccount(t.Context(), store.NewAccount{Name: "Lea", LockKind: "pin", Secret: "1234"})
	versuch := func(von string, koepfe map[string]string, geheim string) int {
		req := httptest.NewRequest("POST", "/api/v1/unlock",
			strings.NewReader(fmt.Sprintf(`{"profileId":%q,"name":"Lea","secret":%q}`, lea.ID, geheim)))
		req.RemoteAddr = von
		req.Host = "192.168.1.10:8097"
		for k, v := range koepfe {
			req.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		h.srv.Config.Handler.ServeHTTP(rec, req)
		return rec.Code
	}
	proxy := map[string]string{"X-Forwarded-For": "203.0.113.50"}
	gebremst := false
	for i := 0; i < 12 && !gebremst; i++ {
		gebremst = versuch("172.17.0.1:40000", proxy, "0000") == 429
	}
	if !gebremst {
		t.Fatal("Rateversuche über den Reverse-Proxy ungebremst")
	}
	// Der Fernseher zu Hause meldet sich trotzdem an.
	if c := versuch("192.168.1.20:5000", nil, "1234"); c != 200 {
		t.Fatalf("Fremder über den Reverse-Proxy sperrt das Profil zu Hause: %d", c)
	}
	// Von einer anderen Adresse draussen (weitergeleiteter Port) bleibt der
	// Zähler „unterwegs" zu — auch mit einem Kopf, der „zu Hause" behauptet.
	if c := versuch("203.0.113.9:1", nil, "1234"); c != 429 {
		t.Fatalf("weitergeleiteter Port an der Bremse vorbei: %d", c)
	}
	if c := versuch("203.0.113.10:1", map[string]string{"X-Real-Ip": "192.168.1.20"}, "1234"); c != 429 {
		t.Fatalf("Kopf holt die Anfrage in den Zähler von zu Hause: %d", c)
	}
}
