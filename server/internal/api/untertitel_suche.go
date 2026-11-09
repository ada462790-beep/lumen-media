// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

// Untertitel von OpenSubtitles suchen und holen.
//
//	GET  /api/v1/untertitel/verfuegbar           ob eingerichtet, Sprachen des Profils
//	GET  /api/v1/items/{id}/untertitel/suche     ?sprachen=de,en&datei=…&name=…
//	POST /api/v1/items/{id}/untertitel           {"datei","dateiId","sprache","hoergeschaedigt","erzwungen"}
//	GET  /api/v1/admin/untertitel                Einstellungen (ohne Schlüssel und Passwort)
//	PUT  /api/v1/admin/untertitel                Einstellungen ändern (auch POST)
//
// Eine geholte Datei liegt danach im Datenverzeichnis
// (untertitel-geholt/<datei>.de.srt) und ist sofort eine Spur:
// Store.Spuren nimmt sie dazu. Neben den Film schreibt Lumen nichts mehr —
// das änderte die Medienordner und deren Änderungszeit, und die
// verriet, wann jemand Untertitel für einen Film gesucht hat.
//
// Wer was sucht, steht nirgends: keine Protokollzeile mit Titel, und
// Datei und Ordner tragen ein festes Datum, nicht das des Holens.
// Kinderprofile holen nicht; jedes Profil höchstens utHolenJeTag am Tag,
// je Datei und Sprache höchstens untertitel.GeholtJeSprache.

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"lumen/internal/store"
	"lumen/internal/untertitel"
)

const (
	keyOSSchluessel = "untertitel.os_key"
	keyOSBenutzer   = "untertitel.os_user"
	keyOSPasswort   = "untertitel.os_pass"
	keyUTAuto       = "untertitel.auto"
	keyUTSprachen   = "untertitel.sprachen"
	keyUTTageslimit = "untertitel.tageslimit"
	keyUTHeute      = "untertitel.heute" // „2026-11-08:3" — nur automatisch Geholtes
)

// Ein Client für alle Anfragen, damit die Anmeldung bei OpenSubtitles
// (ein Tag gültig) nicht bei jeder Suche neu geschieht. Ändert die
// Verwaltung Schlüssel oder Konto, entsteht ein neuer.
var osClient struct {
	sync.Mutex
	c      *untertitel.Client
	fuer   string
	ladung sync.Mutex // Downloads nacheinander: Kontingent und Dateinamen
}

func (s *Server) untertitelClient(ctx context.Context) *untertitel.Client {
	key := s.st.Setting(ctx, keyOSSchluessel, "")
	user := s.st.Setting(ctx, keyOSBenutzer, "")
	pass := s.st.Setting(ctx, keyOSPasswort, "")
	adresse := os.Getenv("LUMEN_OPENSUBTITLES_URL")
	fuer := key + "\x00" + user + "\x00" + pass + "\x00" + adresse
	osClient.Lock()
	defer osClient.Unlock()
	if osClient.c == nil || osClient.fuer != fuer {
		osClient.c, osClient.fuer = untertitel.Neu(key, user, pass, adresse), fuer
	}
	return osClient.c
}

// untertitelFehler: ein Satz für den Bildschirm.
func untertitelFehler(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, untertitel.ErrKeinSchluessel):
		fail(w, http.StatusPreconditionFailed, "Die Verwaltung hat OpenSubtitles noch nicht eingerichtet.")
	case errors.Is(err, untertitel.ErrKontingent):
		fail(w, http.StatusTooManyRequests, "Für heute sind keine Downloads bei OpenSubtitles mehr übrig. Morgen geht es wieder.")
	case errors.Is(err, untertitel.ErrSchluessel):
		fail(w, http.StatusBadGateway, "OpenSubtitles nimmt den Schlüssel nicht an. Die Verwaltung muss ihn prüfen.")
	case errors.Is(err, untertitel.ErrAnmeldung):
		fail(w, http.StatusBadGateway, "Die Anmeldung bei OpenSubtitles ist gescheitert. Die Verwaltung muss das Konto prüfen.")
	case errors.Is(err, untertitel.ErrZuVieleGeholt):
		fail(w, http.StatusConflict, "Für diese Datei und Sprache sind schon genug Untertitel geholt.")
	default:
		fail(w, http.StatusBadGateway, err.Error())
	}
}

// profilSprachen: die Sprachen, die die Suche vorwählt — zuletzt
// gewählte, sonst die Untertitelsprache des Profils, sonst Deutsch und
// Englisch.
func (s *Server) profilSprachen(r *http.Request) []string {
	prefs, _ := s.st.Prefs(r.Context(), accountOf(r).ID)
	if v := prefs["utSuchSprachen"]; v != "" {
		if l := sprachListe(v); len(l) > 0 {
			return l
		}
	}
	var out []string
	if u := prefs["untertitel"]; untertitel.SpracheGueltig(u) && u != "und" {
		out = append(out, u)
	}
	for _, l := range []string{"de", "en"} {
		if len(out) < 2 && !enthaeltText(out, l) {
			out = append(out, l)
		}
	}
	return out
}

func sprachListe(v string) []string {
	var out []string
	for _, l := range strings.Split(v, ",") {
		l = strings.ToLower(strings.TrimSpace(l))
		if untertitel.SpracheGueltig(l) && !enthaeltText(out, l) && len(out) < 6 {
			out = append(out, l)
		}
	}
	return out
}

func enthaeltText(l []string, x string) bool {
	for _, y := range l {
		if y == x {
			return true
		}
	}
	return false
}

func (s *Server) untertitelVerfuegbar(w http.ResponseWriter, r *http.Request) {
	ok(w, map[string]any{
		// Kinderprofile holen keine Untertitel (untertitelDarf) — dann
		// bietet die Oberfläche die Suche gar nicht erst an.
		"verfuegbar": s.st.Setting(r.Context(), keyOSSchluessel, "") != "" && !accountOf(r).IsKid,
		"sprachen":   s.profilSprachen(r),
	})
}

// utHolenJeTag: so viele Untertitel holt ein Profil am Tag höchstens von
// Hand. OpenSubtitles hat ein eigenes Kontingent für den ganzen Server —
// ohne diese Grenze könnte ein Profil es allein aufbrauchen und den Ordner
// mit geholten Dateien füllen.
const utHolenJeTag = 20

// utHolenZaehler zählt je Konto und Tag, nur im Arbeitsspeicher: wie oft,
// nicht was. Nach einem Neustart beginnt es von vorn — das ist in Ordnung.
var utHolenZaehler = struct {
	sync.Mutex
	tag string
	je  map[string]int
}{je: map[string]int{}}

// utHolenErlaubt zählt einen Abruf und sagt, ob er noch drin ist.
func utHolenErlaubt(konto string) bool {
	utHolenZaehler.Lock()
	defer utHolenZaehler.Unlock()
	heute := time.Now().Format("2006-01-02")
	if utHolenZaehler.tag != heute {
		utHolenZaehler.tag, utHolenZaehler.je = heute, map[string]int{}
	}
	if utHolenZaehler.je[konto] >= utHolenJeTag {
		return false
	}
	utHolenZaehler.je[konto]++
	return true
}

// untertitelDarf: Kinderprofile suchen und holen keine Untertitel. Was
// OpenSubtitles zu einem Titel liefert (Dateinamen, Kommentare), ist
// nicht geprüft, und ein Kind soll nicht das Kontingent des Haushalts
// aufbrauchen.
func untertitelDarf(w http.ResponseWriter, r *http.Request) bool {
	if accountOf(r).IsKid {
		failCode(w, http.StatusForbidden, "kinderprofil", "In einem Kinderprofil lassen sich keine Untertitel holen.")
		return false
	}
	return true
}

// untertitelDatei: die gewünschte Datei des Titels, sonst die erste, die da ist.
func (s *Server) untertitelDatei(r *http.Request, it *store.Item, fileID string) (*store.MediaFile, error) {
	files, err := s.st.FilesFor(r.Context(), it.ID)
	if err != nil {
		return nil, err
	}
	for i := range files {
		if files[i].Missing {
			continue
		}
		if fileID == "" || files[i].ID == fileID {
			return &files[i], nil
		}
	}
	return nil, &apiFehler{status: http.StatusNotFound, text: "Zu diesem Titel gibt es keine Datei."}
}

// untertitelSuche baut, wonach gesucht wird: Hash der Datei, Kennungen
// des Films (bei Folgen die der Serie mit Staffel und Folge), Name.
func (s *Server) untertitelSuche(ctx context.Context, it *store.Item, f *store.MediaFile, sprachen []string) untertitel.Suche {
	su := untertitel.Suche{Sprachen: sprachen, Name: it.Title, Jahr: it.Year}
	su.Hash, _ = untertitel.Hash(f.Path)
	switch it.Kind {
	case "movie":
		su.IMDbID = it.IMDbID
		su.TMDbID, _ = s.st.SourceID(ctx, it.ID, "tmdb")
	case "episode":
		su.Serie, su.Staffel, su.Folge = true, it.SeasonNo, it.EpisodeNo
		if serie, err := s.st.SeriesOf(ctx, it.ID); err == nil {
			if x, err := s.st.Item(ctx, serie); err == nil {
				su.IMDbID, su.Name, su.Jahr = x.IMDbID, x.Title, x.Year
			}
			su.TMDbID, _ = s.st.SourceID(ctx, serie, "tmdb")
		}
	}
	return su
}

func (s *Server) untertitelSuchen(w http.ResponseWriter, r *http.Request) {
	it, err := s.visibleItem(r, r.PathValue("id"))
	if err != nil {
		failErr(w, err)
		return
	}
	if !untertitelDarf(w, r) {
		return
	}
	if it.Kind != "movie" && it.Kind != "episode" {
		fail(w, http.StatusBadRequest, "Untertitel gibt es für Filme und einzelne Folgen.")
		return
	}
	f, err := s.untertitelDatei(r, it, r.URL.Query().Get("datei"))
	if err != nil {
		failErr(w, err)
		return
	}
	sprachen := sprachListe(r.URL.Query().Get("sprachen"))
	if len(sprachen) == 0 {
		sprachen = s.profilSprachen(r)
	} else {
		// Fürs nächste Mal merken — im eigenen Profil.
		_ = s.st.SetPref(r.Context(), accountOf(r).ID, "utSuchSprachen", strings.Join(sprachen, ","))
	}
	c := s.untertitelClient(r.Context())
	if !c.Bereit() {
		untertitelFehler(w, untertitel.ErrKeinSchluessel)
		return
	}
	su := s.untertitelSuche(r.Context(), it, f, sprachen)
	if name := strings.TrimSpace(r.URL.Query().Get("name")); name != "" {
		// Eigener Suchbegriff: nur nach Name (und Hash).
		su.IMDbID, su.TMDbID, su.Name = "", "", kuerzen(name, 120)
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	treffer, err := c.Suchen(ctx, su)
	if err == nil && len(treffer) == 0 && (su.IMDbID != "" || su.TMDbID != "") {
		// Über die Kennung nichts — vielleicht über den Namen.
		su.IMDbID, su.TMDbID = "", ""
		treffer, err = c.Suchen(ctx, su)
	}
	if err != nil {
		untertitelFehler(w, err)
		return
	}
	if len(treffer) > 60 {
		treffer = treffer[:60]
	}
	ok(w, map[string]any{"treffer": treffer, "datei": f.ID, "sprachen": sprachen, "hash": su.Hash != ""})
}

func (s *Server) untertitelHolen(w http.ResponseWriter, r *http.Request) {
	it, err := s.visibleItem(r, r.PathValue("id"))
	if err != nil {
		failErr(w, err)
		return
	}
	var in struct {
		Datei           string `json:"datei"`
		DateiID         int    `json:"dateiId"`
		Sprache         string `json:"sprache"`
		Hoergeschaedigt bool   `json:"hoergeschaedigt"`
		Erzwungen       bool   `json:"erzwungen"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if !untertitelDarf(w, r) {
		return
	}
	if in.DateiID <= 0 || !untertitel.SpracheGueltig(in.Sprache) {
		fail(w, http.StatusBadRequest, "Unvollständige Angaben.")
		return
	}
	f, err := s.untertitelDatei(r, it, in.Datei)
	if err != nil {
		failErr(w, err)
		return
	}
	if !utHolenErlaubt(accountOf(r).ID) {
		fail(w, http.StatusTooManyRequests, "Für heute hast du genug Untertitel geholt. Morgen geht es wieder.")
		return
	}
	c := s.untertitelClient(r.Context())
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 60*time.Second)
	defer cancel()
	pfad, rest, err := s.untertitelAblegen(ctx, c, f, in.DateiID, in.Sprache, in.Hoergeschaedigt, in.Erzwungen)
	if err != nil {
		untertitelFehler(w, err)
		return
	}
	if !s.st.SpurenGelesen(r.Context(), f.ID) {
		s.spurenNachmessen(r.Context(), f)
	}
	spuren, _ := s.st.Spuren(r.Context(), f.ID)
	neu := -1
	for _, t := range spuren {
		if t.ExternalPath == pfad {
			neu = t.Index
		}
	}
	if spuren == nil {
		spuren = []store.Track{}
	}
	ok(w, map[string]any{"tracks": spuren, "neu": neu, "rest": rest})
}

// untertitelAblegen holt, legt ab und misst die Datei neu aus. Gemeinsam
// für die Suche von Hand und das automatische Holen.
func (s *Server) untertitelAblegen(ctx context.Context, c *untertitel.Client, f *store.MediaFile,
	dateiID int, sprache string, hi, forced bool) (pfad string, rest int, err error) {
	osClient.ladung.Lock()
	defer osClient.ladung.Unlock()
	inhalt, k, err := c.Herunterladen(ctx, dateiID)
	if err != nil {
		return "", k.Rest, err
	}
	// Ins Datenverzeichnis, nicht neben den Film (untertitel.AblegenGeholt):
	// Store.Spuren nimmt sie von dort dazu, ohne neu zu messen.
	pfad, err = untertitel.AblegenGeholt(s.geholtOrdner(), f.ID, sprache, hi, forced, inhalt)
	if err != nil {
		return "", k.Rest, err
	}
	// Die Nummern der geholten Spuren können sich verschoben haben —
	// gewandelte Untertitel dieser Datei nicht mehr verwenden.
	vttZwischen.vergessen(f.ID + "-")
	return pfad, k.Rest, nil
}

// geholtOrdner: wo geholte Untertitel liegen (siehe api.New).
func (s *Server) geholtOrdner() string { return filepath.Join(s.dataDir, "untertitel-geholt") }

// vergessen wirft alle Einträge weg, deren Schlüssel so anfängt.
func (v *vttSpeicher) vergessen(praefix string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	for k, b := range v.inhalt {
		if strings.HasPrefix(k, praefix) {
			v.bytes -= len(b)
			delete(v.inhalt, k)
		}
	}
}

// ---------------------------------------------------------------- Verwaltung

func (s *Server) adminUntertitel(w http.ResponseWriter, r *http.Request) {
	d := s.adminUntertitelDaten(r.Context())
	// Mitverwalter sehen die Einstellungen nur lesend und ohne den
	// Benutzernamen des OpenSubtitles-Kontos — der gehört dem Besitzer.
	// „darfAendern" sagt der Oberfläche, ob sie Felder anbieten soll.
	besitzer := accountOf(r).Besitzerrechte()
	if !besitzer {
		delete(d, "benutzer")
	}
	d["darfAendern"] = besitzer
	ok(w, d)
}

func (s *Server) adminUntertitelDaten(ctx context.Context) map[string]any {
	_, n := s.utHeute(ctx)
	// Schlüssel und Passwort gehen nie hinaus — nur, ob sie da sind.
	return map[string]any{
		"configured":  s.st.Setting(ctx, keyOSSchluessel, "") != "",
		"benutzer":    s.st.Setting(ctx, keyOSBenutzer, ""),
		"passwort":    s.st.Setting(ctx, keyOSPasswort, "") != "",
		"auto":        s.st.Setting(ctx, keyUTAuto, "0") == "1",
		"sprachen":    sprachListe(s.st.Setting(ctx, keyUTSprachen, "de")),
		"tageslimit":  s.utTageslimit(ctx),
		"heuteGeholt": n,
	}
}

func (s *Server) utTageslimit(ctx context.Context) int {
	n, err := strconv.Atoi(s.st.Setting(ctx, keyUTTageslimit, "10"))
	if err != nil || n < 0 {
		return 10
	}
	return n
}

// utHeute: wie viele Untertitel heute automatisch geholt wurden.
func (s *Server) utHeute(ctx context.Context) (string, int) {
	heute := time.Now().Format("2006-01-02")
	tag, zahl, _ := strings.Cut(s.st.Setting(ctx, keyUTHeute, ""), ":")
	if tag != heute {
		return heute, 0
	}
	n, _ := strconv.Atoi(zahl)
	return heute, n
}

func (s *Server) adminUntertitelSetzen(w http.ResponseWriter, r *http.Request) {
	var in struct {
		APIKey     *string  `json:"apiKey"`
		Benutzer   *string  `json:"benutzer"`
		Passwort   *string  `json:"passwort"`
		Auto       *bool    `json:"auto"`
		Sprachen   []string `json:"sprachen"`
		Tageslimit *int     `json:"tageslimit"`
		Pruefen    bool     `json:"pruefen"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	ctx := r.Context()
	set := func(k string, v *string, n int) bool {
		if v == nil {
			return true
		}
		t := strings.TrimSpace(*v)
		if len(t) > n {
			return false
		}
		_ = s.st.SetSetting(ctx, k, t)
		return true
	}
	if !set(keyOSSchluessel, in.APIKey, 200) || !set(keyOSBenutzer, in.Benutzer, 200) {
		fail(w, http.StatusBadRequest, "Eine Angabe ist zu lang.")
		return
	}
	if in.Passwort != nil {
		// Das Passwort nicht beschneiden — Leerzeichen können dazugehören.
		if len(*in.Passwort) > 500 {
			fail(w, http.StatusBadRequest, "Das Passwort ist zu lang.")
			return
		}
		_ = s.st.SetSetting(ctx, keyOSPasswort, *in.Passwort)
	}
	if in.Auto != nil {
		v := "0"
		if *in.Auto {
			v = "1"
		}
		_ = s.st.SetSetting(ctx, keyUTAuto, v)
	}
	if in.Sprachen != nil {
		l := sprachListe(strings.Join(in.Sprachen, ","))
		if len(l) == 0 && len(in.Sprachen) > 0 {
			fail(w, http.StatusBadRequest, "Unbekannte Sprache.")
			return
		}
		_ = s.st.SetSetting(ctx, keyUTSprachen, strings.Join(l, ","))
	}
	if in.Tageslimit != nil {
		if *in.Tageslimit < 0 || *in.Tageslimit > 1000 {
			fail(w, http.StatusBadRequest, "Das Tageslimit muss zwischen 0 und 1000 liegen.")
			return
		}
		_ = s.st.SetSetting(ctx, keyUTTageslimit, strconv.Itoa(*in.Tageslimit))
	}
	// Ohne Titel, ohne Schlüssel: nur, dass sich etwas geändert hat.
	s.st.Log(ctx, "info", "untertitel", "Einstellungen geändert")
	daten := s.adminUntertitelDaten(ctx)
	if in.Pruefen {
		c := s.untertitelClient(ctx)
		pctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		k, err := c.Pruefen(pctx)
		if err != nil {
			untertitelFehler(w, err)
			return
		}
		// Rest -1: ohne Konto sagt OpenSubtitles nicht, wie viele übrig sind.
		daten["geprueft"], daten["rest"] = true, k.Rest
	}
	ok(w, daten)
}
