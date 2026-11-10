// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"lumen/internal/sprache"
	"lumen/internal/store"
	"lumen/internal/stream"
)

// ---------------------------------------------------------------- offen

type helloBody struct {
	Server string `json:"server"`
	// Name: wie der Besitzer diesen Server nennt („Mein NAS") — für die
	// Serverliste in den Apps. Leer, solange keiner gesetzt ist.
	Name     string `json:"name,omitempty"`
	Version  string `json:"version"`
	SetUp    bool   `json:"setUp"`
	FFmpeg   bool   `json:"ffmpeg"`
	UptimeMS int64  `json:"uptimeMs"`

	// Lumen steht unter der AGPL. Deren §13 verlangt, dass wer den Dienst
	// über das Netz benutzt, an den Quelltext kommt — auch dann, wenn der
	// Betreiber etwas geändert hat. Darum stehen Lizenz und Fundstelle in
	// jeder Antwort auf die erste Frage, die eine App stellt.
	License   string `json:"license"`
	SourceURL string `json:"sourceUrl"`

	// Sprache des Servers und die Auswahl — schon vor der Anmeldung, damit
	// Profilwahl und Einrichtung in der richtigen Sprache erscheinen.
	Sprache  string            `json:"sprache"`
	Sprachen []sprache.Sprache `json:"sprachen"`
}

func (s *Server) hello(w http.ResponseWriter, r *http.Request) {
	ok(w, helloBody{
		Server: "Lumen", Version: s.version,
		Name:     s.st.Setting(r.Context(), "server.name", ""),
		SetUp:    s.st.HasOwner(r.Context()),
		FFmpeg:   s.ff != nil && s.ff.Available(),
		UptimeMS: time.Since(s.started).Milliseconds(),

		License:   "AGPL-3.0-only",
		SourceURL: s.sourceURL,

		Sprache:  s.serverSprache(r.Context()),
		Sprachen: sprachAuswahl(),
	})
}

// setup läuft genau einmal: der erste, der den Server erreicht, wird Besitzer.
func (s *Server) setup(w http.ResponseWriter, r *http.Request) {
	if s.st.HasOwner(r.Context()) {
		fail(w, http.StatusConflict, "dieser server ist schon eingerichtet")
		return
	}
	// Besitzer wird nur, wer zu Hause vor dem Server sitzt — nicht, wer
	// ihn zufällig zuerst von aussen erreicht.
	if vonAussen(r) {
		fail(w, http.StatusForbidden, "einrichten geht nur zu hause, nicht von unterwegs")
		return
	}
	var in struct {
		Name       string `json:"name"`
		Password   string `json:"password"`
		Avatar     string `json:"avatar"`
		DeviceName string `json:"deviceName"`
		DeviceKind string `json:"deviceKind"`
		// Sprache: optional, eine aus /hello. Bestimmt auch die Vorgabe
		// für die Metadaten (Sprache und Region bei TMDb).
		Sprache string `json:"sprache"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if in.Sprache != "" && !sprache.Gueltig(in.Sprache) {
		fail(w, http.StatusBadRequest, "Unbekannte Sprache.")
		return
	}
	acc, err := s.st.CreateAccount(r.Context(), store.NewAccount{
		Name: in.Name, Avatar: in.Avatar, IsOwner: true,
		LockKind: "password", Secret: in.Password,
	})
	if err != nil {
		failErr(w, err)
		return
	}
	tok, err := s.st.IssueToken(r.Context(), acc.ID, kuerzen(orElse(in.DeviceName, trSprache(orElse(in.Sprache, spracheVon(w)), "Erstes Gerät")), 60),
		kuerzen(orElse(in.DeviceKind, "browser"), 20))
	if err != nil {
		failErr(w, err)
		return
	}
	if in.Sprache != "" {
		s.einrichtungsSprache(r.Context(), in.Sprache)
		w.Header().Set(kopfSprache, in.Sprache)
	}
	s.st.Log(r.Context(), "info", "setup", "Server eingerichtet")
	ok(w, map[string]any{"token": tok, "account": acc})
}

func (s *Server) profiles(w http.ResponseWriter, r *http.Request) {
	// Von unterwegs keine Liste: Sie verriete die Namen im Haushalt, oft
	// auch die der Kinder. Angemeldet wird dann mit Name und PIN, mit
	// Code oder mit einer Einladung.
	if vonAussen(r) {
		writeJSON(w, http.StatusForbidden, map[string]any{
			"error": tr(w, "von unterwegs gibt es keine profilauswahl — melde dich mit deinem "+
				"profilnamen und pin oder passwort an, oder mit einem code"),
			"code": "unterwegs",
		})
		return
	}
	accs, err := s.st.Accounts(r.Context())
	if err != nil {
		failErr(w, err)
		return
	}
	// Vor der Anmeldung gibt es nur Name, Bild und ob ein Code nötig ist.
	type card struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Avatar   string `json:"avatar"`
		LockKind string `json:"lockKind"`
		IsKid    bool   `json:"isKid"`
	}
	out := make([]card, 0, len(accs))
	for _, a := range accs {
		out = append(out, card{a.ID, a.Name, a.Avatar, a.LockKind, a.IsKid})
	}
	ok(w, out)
}

func (s *Server) unlock(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ProfileID  string `json:"profileId"`
		Name       string `json:"name"` // von unterwegs: statt der Kennung
		Secret     string `json:"secret"`
		DeviceName string `json:"deviceName"`
		DeviceKind string `json:"deviceKind"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	aussen := vonAussen(r)
	falsch := "das stimmt nicht"
	if aussen {
		falsch = "das stimmt nicht — von unterwegs geht es nur mit profilname und pin oder passwort"
	}

	// Profil bestimmen: zu Hause über die Kennung aus der Auswahl, von
	// unterwegs (wo es keine Auswahl gibt) über den Namen.
	id := in.ProfileID
	if id == "" && in.Name != "" {
		id = s.profilZuName(r.Context(), in.Name)
	}
	// Gebremst wird je Profil — und zu Hause zusätzlich je Absender. Von
	// unterwegs kommen alle über denselben Weg; eine Bremse je Absender
	// träfe dort jeden, und ein Fremder könnte mit ein paar falschen
	// Versuchen alle anderen aussperren.
	//
	// Die Zähler für zu Hause und unterwegs sind getrennt: Wer von aussen
	// den PIN von „Alex" errät, sperrt damit nur die Anmeldung von
	// unterwegs — nicht die Profilauswahl im Wohnzimmer.
	schluessel := s.profilBremse(r, id, in.Name)
	frei, warte := s.bremse.anfangen(schluessel...)
	if !frei {
		fail(w, http.StatusTooManyRequests, wartetext(w, warte))
		return
	}
	erfolg := false
	defer func() { s.bremse.beenden(erfolg, schluessel...) }()

	// Von unterwegs öffnet sich nur ein Profil mit PIN oder Passwort. Die
	// Antwort ist dieselbe wie bei einem falschen PIN, damit sich von
	// aussen nicht herausfinden lässt, welche Profile ungeschützt sind.
	if aussen {
		if acc, err := s.st.Account(r.Context(), id); err != nil || acc.LockKind == "none" || acc.LockKind == "" {
			store.Blindrechnen(in.Secret)
			time.Sleep(400 * time.Millisecond)
			failCode(w, http.StatusUnauthorized, "falsch", falsch)
			return
		}
	}
	if err := s.st.CheckSecret(r.Context(), id, in.Secret); err != nil {
		// Gleiche Antwort für falschen Code und unbekanntes Profil, damit
		// sich Profile nicht durch Probieren aufzählen lassen.
		time.Sleep(400 * time.Millisecond)
		if aussen {
			s.st.Log(r.Context(), "warn", "anmeldung", "Fehlversuch beim Anmelden über den Fernzugriff")
			failCode(w, http.StatusUnauthorized, "falsch", falsch)
			return
		}
		// Zu Hause zeigt die Profilauswahl ohnehin, wer welche Sperre hat;
		// da darf die Meldung genau sagen, was nicht stimmt und wer hilft.
		art := "pin"
		if acc, e := s.st.Account(r.Context(), id); e == nil {
			art = acc.LockKind
		}
		s.entsperrenFalsch(w, r, art)
		return
	}
	erfolg = true
	in.ProfileID = id
	tok, err := s.st.IssueToken(r.Context(), in.ProfileID,
		kuerzen(orElse(in.DeviceName, tr(w, "Gerät")), 60), kuerzen(orElse(in.DeviceKind, "browser"), 20))
	if err != nil {
		failErr(w, err)
		return
	}
	acc, _ := s.st.Account(r.Context(), in.ProfileID)
	antwort := map[string]any{"token": tok, "account": acc}
	// Hat die Verwaltung den PIN zurückgesetzt, sagt Lumen das beim
	// nächsten Anmelden — einmal hier (die Web-App zeigt es sofort) und
	// als Neuigkeit, bis sie gelesen ist (das sehen alle Apps).
	if _, da := s.pinHinweis(r.Context(), id); da {
		antwort["hinweis"] = map[string]string{"code": "pin_zurueckgesetzt", "text": tr(w, pinHinweisText)}
	}
	ok(w, antwort)
}

func (s *Server) redeemInvite(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Code     string `json:"code"`
		Name     string `json:"name"`
		LockKind string `json:"lockKind"`
		Secret   string `json:"secret"`
		// Wie beim Entsperren: Name und Art des Geräts, optional.
		DeviceName string `json:"deviceName"`
		DeviceKind string `json:"deviceKind"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	frei, warte := s.bremse.anfangen("einladung:" + quelle(r))
	if !frei {
		fail(w, http.StatusTooManyRequests, wartetext(w, warte))
		return
	}
	acc, err := s.st.RedeemInvite(r.Context(), in.Code, in.Name, in.LockKind, in.Secret)
	// Nur eine unbekannte Einladung zählt als Fehlversuch — ein zu kurzer
	// PIN ist kein Raten.
	s.bremse.beenden(err == nil || err.Error() != "einladung unbekannt", "einladung:"+quelle(r))
	if err != nil {
		failErr(w, err)
		return
	}
	tok, err := s.st.IssueToken(r.Context(), acc.ID,
		kuerzen(orElse(in.DeviceName, tr(w, "Neues Gerät")), 60), kuerzen(orElse(in.DeviceKind, "browser"), 20))
	if err != nil {
		failErr(w, err)
		return
	}
	ok(w, map[string]any{"token": tok, "account": acc})
}

// ---------------------------------------------------------------- ich

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	acc := accountOf(r)
	lim, _ := s.st.Limits(r.Context(), acc.ID)
	prefs, _ := s.st.Prefs(r.Context(), acc.ID)
	libs, _ := s.st.GrantedLibraries(r.Context(), acc.ID)
	ok(w, map[string]any{
		"account": acc, "limits": lim, "prefs": prefs, "libraries": libs,
		// sprache: die wirksame — eigene Wahl oder die des Servers.
		"sprache":     s.wirksameSprache(r.Context(), acc.ID),
		"device":      deviceOf(r),
		"neuigkeiten": s.ungeleseneNeuigkeiten(r, acc),
		"figuren":     Figuren,
	})
}

func (s *Server) updateMe(w http.ResponseWriter, r *http.Request) {
	acc := accountOf(r)
	var in struct {
		Name   *string           `json:"name"`
		Avatar *string           `json:"avatar"`
		Prefs  map[string]string `json:"prefs"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	name, avatar := acc.Name, acc.Avatar
	if in.Name != nil && strings.TrimSpace(*in.Name) != "" {
		name = *in.Name
	}
	if in.Avatar != nil {
		a := *in.Avatar
		switch {
		case strings.HasPrefix(a, "av-") && len(a) <= 6, figurBekannt(a):
			avatar = a
		case a == "bild":
			if _, err := os.Stat(s.avatarPfad(acc.ID)); err == nil {
				avatar = a
			}
		default:
			fail(w, http.StatusBadRequest, "unbekanntes profilbild")
			return
		}
	}
	if err := s.st.RenameAccount(r.Context(), acc.ID, name, avatar); err != nil {
		failErr(w, err)
		return
	}
	if len(in.Prefs) > 50 {
		fail(w, http.StatusBadRequest, "zu viele einstellungen auf einmal")
		return
	}
	// Die Sprache prüft Lumen, bevor irgendetwas gespeichert ist: Eine
	// unbekannte Kennung im Profil hiesse, dass jede App raten müsste.
	if v, da := in.Prefs[prefSprache]; da && v != "" && !sprache.Gueltig(v) {
		fail(w, http.StatusBadRequest, "Unbekannte Sprache.")
		return
	}
	for k, v := range in.Prefs {
		if len(k) > 64 || len(v) > 2000 {
			fail(w, http.StatusBadRequest, "einstellung zu lang")
			return
		}
	}
	if err := s.prefsPruefen(r.Context(), acc.ID, in.Prefs); err != nil {
		failErr(w, err)
		return
	}
	for k, v := range in.Prefs {
		if err := s.st.SetPref(r.Context(), acc.ID, k, v); err != nil {
			failErr(w, err)
			return
		}
	}
	if v, da := in.Prefs[prefSprache]; da {
		s.merkeProfilSprache(acc.ID, v)
		// Die Antwort kommt schon in der neuen Sprache.
		w.Header().Set(kopfSprache, s.wirksameSprache(r.Context(), acc.ID))
	}
	s.me(w, s.mitFrischemKonto(r))
}

// geraetAnsicht ist ein Gerät, wie es das eigene Profil unter „Ich ›
// Geräte" sieht: mit einer Kennung zum Abmelden und dem Hinweis, ob es
// das Gerät ist, von dem die Anfrage kommt. Zuletzt aktiv steht nur
// tagesgenau da (siehe LookupTokenHash) — und nur hier, im eigenen Profil.
type geraetAnsicht struct {
	store.Device
	ID           string `json:"id"`
	DiesesGeraet bool   `json:"diesesGeraet,omitempty"`
}

func (s *Server) myDevices(w http.ResponseWriter, r *http.Request) {
	d, err := s.st.Devices(r.Context(), accountOf(r).ID)
	if err != nil {
		failErr(w, err)
		return
	}
	var eigenes []byte
	if dev := deviceOf(r); dev != nil {
		eigenes = dev.TokenHash
	}
	out := make([]geraetAnsicht, 0, len(d))
	for _, g := range d {
		out = append(out, geraetAnsicht{Device: g, ID: store.GeraeteKennung(g.TokenHash),
			DiesesGeraet: eigenes != nil && bytes.Equal(eigenes, g.TokenHash)})
	}
	ok(w, out)
}

// geraetAbmelden meldet ein einzelnes eigenes Gerät ab — etwa ein
// verlorenes Handy —, ohne gleich alle anderen mit abzumelden.
func (s *Server) geraetAbmelden(w http.ResponseWriter, r *http.Request) {
	if err := s.st.GeraetAbmelden(r.Context(), accountOf(r).ID, r.PathValue("id")); err != nil {
		failErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) signOutEverywhere(w http.ResponseWriter, r *http.Request) {
	if err := s.st.RevokeAllTokens(r.Context(), accountOf(r).ID); err != nil {
		failErr(w, err)
		return
	}
	ok(w, map[string]string{"status": "abgemeldet"})
}

func (s *Server) clearAllProgress(w http.ResponseWriter, r *http.Request) {
	if err := s.st.ClearAllProgress(r.Context(), accountOf(r).ID); err != nil {
		failErr(w, err)
		return
	}
	ok(w, map[string]string{"status": "verlauf gelöscht"}) // sprache: fest – eine Kennung
}

// ---------------------------------------------------------------- Mediathek

// view ist ein Titel, wie ihn eine App bekommt: die Angaben aus der
// Mediathek plus der eigene Stand. Der Stand kommt immer aus dem Profil,
// das gerade angemeldet ist — nie aus einem anderen.
type view struct {
	store.Item
	Progress float64 `json:"progress,omitempty"`
	Finished bool    `json:"finished,omitempty"`
	MyRating int     `json:"myRating,omitempty"`
	OnList   bool    `json:"onList,omitempty"`
	// Favorit: das Herz — nur für das eigene Profil.
	Favorit  bool   `json:"favorite,omitempty"`
	ResumeMS int64  `json:"resumeMs,omitempty"`
	Poster   string `json:"poster,omitempty"`
	Backdrop string `json:"backdrop,omitempty"`
	// Frame ist ein Bild aus dem Film selbst. Nur gesetzt, wenn Poster
	// oder Hintergrund fehlen — als Ersatz, nicht als Wahl.
	Frame string `json:"frame,omitempty"`
	// Bei Serien: wie viele Folgen dieses Konto gesehen hat, von wie vielen.
	Gesehen int `json:"gesehen,omitempty"`
	Folgen  int `json:"folgen,omitempty"`
	// Abzeichen: neu dazugekommen (7 Tage), neue Folgen (Zahl), Qualität
	// (SD/HD/4K), HDR-Art, Tonkanäle.
	Neu        bool   `json:"neu,omitempty"`
	NeueFolgen int    `json:"neueFolgen,omitempty"`
	Qualitaet  string `json:"qualitaet,omitempty"`
	HDR        string `json:"hdr,omitempty"`
	Kanaele    int    `json:"kanaele,omitempty"`
	// Bei Folgen: zu welcher Serie sie gehört, damit eine Karte
	// „Tiefsee · S1 F2 · Titel" sagen kann statt nur „Titel".
	SeriesID    string `json:"seriesId,omitempty"`
	SeriesTitle string `json:"seriesTitle,omitempty"`
	// NaechsteFolge: in „Weiterschauen" die Folge nach der zuletzt
	// gesehenen — noch nicht angefangen.
	NaechsteFolge bool `json:"naechsteFolge,omitempty"`
}

// neuSeit: so lange gilt ein Titel als „neu".
const neuSeit = 7 * 24 * time.Hour

func (s *Server) decorate(r *http.Request, items []store.Item) ([]view, error) {
	acc := accountOf(r)
	ids := make([]string, len(items))
	for i, it := range items {
		ids[i] = it.ID
	}
	prog, err := s.st.ProgressFor(r.Context(), acc.ID, ids)
	if err != nil {
		return nil, err
	}
	ratings, err := s.st.RatingsFor(r.Context(), acc.ID, ids)
	if err != nil {
		return nil, err
	}
	list, err := s.st.WatchlistIDs(r.Context(), acc.ID)
	if err != nil {
		return nil, err
	}
	onList := map[string]bool{}
	for _, id := range list {
		onList[id] = true
	}
	favs, err := s.st.FavoritIDs(r.Context(), acc.ID)
	if err != nil {
		return nil, err
	}
	favorit := map[string]bool{}
	for _, id := range favs {
		favorit[id] = true
	}

	art, err := s.st.ArtworkFor(r.Context(), ids)
	if err != nil {
		return nil, err
	}

	standbilder := s.ff != nil && s.ff.Available()

	var serien, folgen []string
	for _, it := range items {
		if it.Kind == "series" {
			serien = append(serien, it.ID)
		}
		if it.Kind == "episode" {
			folgen = append(folgen, it.ID)
		}
	}
	zuSerie, _ := s.st.SerienZuFolgen(r.Context(), folgen)
	staende, err := s.st.SerienStaende(r.Context(), acc.ID, serien)
	if err != nil {
		return nil, err
	}
	kenn, _ := s.st.Kennzahlen(r.Context(), ids)
	neueFolgen, _ := s.st.NeueFolgenSeit(r.Context(), acc.ID, serien, time.Now().Add(-neuSeit))

	spr := s.wirksameSprache(r.Context(), acc.ID)
	out := make([]view, len(items))
	for i, it := range items {
		it.Title = standardTitel(spr, &it)
		v := view{Item: it, MyRating: ratings[it.ID], OnList: onList[it.ID], Favorit: favorit[it.ID]}
		if p, okp := prog[it.ID]; okp {
			v.Progress, v.Finished, v.ResumeMS = p.Percent(), p.Finished, p.PositionMS
		}
		if k, okK := kenn[it.ID]; okK {
			v.Qualitaet, v.HDR, v.Kanaele = store.Qualitaet(k.Height), k.HDR, k.Channels
		}
		v.Neu = time.Since(it.AddedAt) < neuSeit
		if n := neueFolgen[it.ID]; n > 0 {
			v.NeueFolgen = n
		}
		if z, okZ := zuSerie[it.ID]; okZ {
			v.SeriesID, v.SeriesTitle = z.ID, z.Titel
		}
		if st, okS := staende[it.ID]; okS && st.Folgen > 0 {
			v.Gesehen, v.Folgen = st.Gesehen, st.Folgen
			// Eine Serie ist gesehen, wenn jede Folge es ist — ein alter
			// Haken an der Serie selbst zählt nicht mehr (wie GesehenIDs).
			v.Finished = st.Gesehen >= st.Folgen
			if v.Progress == 0 && st.Gesehen > 0 {
				v.Progress = float64(st.Gesehen) / float64(st.Folgen) * 100
			}
		}
		if a := art[it.ID]; a != nil {
			if a["poster"] != "" {
				v.Poster = "/api/v1/art/" + it.ID + "/poster"
			}
			if a["backdrop"] != "" {
				v.Backdrop = "/api/v1/art/" + it.ID + "/backdrop"
			}
		}
		// Ein Bild aus der Datei nur, wo das passende fehlt. Bei Folgen ist
		// das „Poster" das Standbild von TMDb (oder das eigene -thumb.jpg) —
		// es ist schon breit; einen Hintergrund haben Folgen nie. Vorher
		// bekam darum jede Folge ein zufälliges Bild aus der Datei, und das
		// verdrängte das richtige.
		folgeMitBild := it.Kind == "episode" && v.Poster != ""
		if standbilder && !folgeMitBild && (v.Poster == "" || v.Backdrop == "") {
			v.Frame = "/api/v1/art/" + it.ID + "/frame"
		}
		out[i] = v
	}
	return out, nil
}

// scope baut die Abfrage aus den Rechten des Kontos — was jemand nicht sehen
// darf, taucht gar nicht erst in der Antwort auf.
func (s *Server) scope(r *http.Request) (store.Query, error) {
	acc := accountOf(r)
	libs, err := s.st.GrantedLibraries(r.Context(), acc.ID)
	if err != nil {
		return store.Query{}, err
	}
	lim, err := s.st.Limits(r.Context(), acc.ID)
	if err != nil {
		return store.Query{}, err
	}
	q := store.Query{Libraries: libs, AllowUnrated: lim.AllowUnrated, Freigabe: true}
	if !acc.Besitzerrechte() && lim.MaxAge != store.OhneAltersgrenze {
		q.MaxAge, q.MitAltersgrenze = lim.MaxAge, true
	}
	return q, nil
}

func (s *Server) itemsSeite(w http.ResponseWriter, r *http.Request, q store.Query) {
	items, err := s.st.Query(r.Context(), q)
	if err != nil {
		failErr(w, err)
		return
	}
	out, err := s.decorate(r, items)
	if err != nil {
		failErr(w, err)
		return
	}
	gesamt, err := s.st.Count(r.Context(), q)
	if err != nil {
		failErr(w, err)
		return
	}
	// Die Genres über alle Titel der Art, nicht nur die gefilterten —
	// sonst verschwänden die anderen Knöpfe, sobald einer gedrückt ist.
	gq := q
	gq.Genre, gq.Text = "", ""
	genres, err := s.st.Genres(r.Context(), gq)
	if err != nil {
		failErr(w, err)
		return
	}
	ok(w, map[string]any{
		"items":  out,
		"gesamt": gesamt,
		"offset": q.Offset,
		"weiter": q.Offset+len(out) < gesamt,
		"genres": genres,
	})
}

func (s *Server) items(w http.ResponseWriter, r *http.Request) {
	q, err := s.scope(r)
	if err != nil {
		failErr(w, err)
		return
	}
	if k := r.URL.Query().Get("kind"); k != "" {
		q.Kinds = strings.Split(k, ",")
	}
	q.Text = suchEingabe(r)
	q.Genre = r.URL.Query().Get("genre")
	q.Person = r.URL.Query().Get("person")
	q.JahrVon = qInt(r, "jahrVon", 0)
	q.JahrBis = qInt(r, "jahrBis", 0)
	q.Sort = r.URL.Query().Get("sort")
	q.Buchstabe = r.URL.Query().Get("buchstabe")
	q.Limit = qInt(r, "limit", 100)
	// ?liste=merkliste oder ?liste=favoriten: nur die eigenen. Vorher holte
	// die Merkliste die ersten 200 Titel und siebte — wer mehr hatte, sah
	// gemerkte Titel weiter hinten nie.
	switch r.URL.Query().Get("liste") {
	case "merkliste", "favoriten", "gesehen":
		var ids []string
		switch r.URL.Query().Get("liste") {
		case "merkliste":
			ids, err = s.st.WatchlistIDs(r.Context(), accountOf(r).ID)
		case "favoriten":
			ids, err = s.st.FavoritIDs(r.Context(), accountOf(r).ID)
		default:
			ids, err = s.st.GesehenIDs(r.Context(), accountOf(r).ID)
		}
		if err != nil {
			failErr(w, err)
			return
		}
		if len(ids) == 0 {
			ids = []string{"-"}
		}
		q.IDs = ids
		if q.Sort == "" {
			// In der Reihenfolge, in der gemerkt wurde: zuletzt zuerst.
			q.Sort = "liste"
		}
	}
	q.Offset = qInt(r, "offset", 0)
	if lib := r.URL.Query().Get("library"); lib != "" {
		// Nur, wenn das Konto diese Bibliothek ohnehin sehen darf.
		for _, allowed := range q.Libraries {
			if allowed == lib {
				q.Libraries = []string{lib}
				break
			}
		}
	}

	// Mit ?seite=1 kommt eine Seite samt Gesamtzahl und Genres — für ein
	// Raster, das beim Herunterscrollen nachlädt. Vorher bekam der Browser
	// stillschweigend die ersten hundert, und wer mehr Filme hatte, sah
	// den Rest nie.
	if r.URL.Query().Get("seite") == "1" {
		s.itemsSeite(w, r, q)
		return
	}

	items, err := s.st.Query(r.Context(), q)
	if err != nil {
		failErr(w, err)
		return
	}
	out, err := s.decorate(r, items)
	if err != nil {
		failErr(w, err)
		return
	}
	ok(w, out)
}

func (s *Server) item(w http.ResponseWriter, r *http.Request) {
	it, err := s.visibleItem(r, r.PathValue("id"))
	if err != nil {
		failErr(w, err)
		return
	}
	out, err := s.decorate(r, []store.Item{*it})
	if err != nil {
		failErr(w, err)
		return
	}
	files, _ := s.st.FilesFor(r.Context(), it.ID)
	for i := range files {
		files[i].Path = pfadFuer(accountOf(r), files[i].Path)
	}
	s.mitSpuren(r.Context(), files)
	shared, _ := s.st.SharedRatings(r.Context(), it.ID)

	body := map[string]any{"item": out[0], "files": files, "sharedRatings": shared}
	if l := s.links(r.Context(), it); len(l) > 0 {
		body["links"] = l
	}
	if it.Kind == "episode" || it.Kind == "movie" {
		if vor, abspann := s.vorspannUndAbspann(r.Context(), it, files); vor != nil || abspann > 0 {
			if vor != nil {
				body["intro"] = vor
			}
			if abspann > 0 {
				body["creditsMs"] = abspann
			}
		}
		if it.Kind == "episode" {
			if staffel, err := s.st.Item(r.Context(), it.ParentID); err == nil {
				body["seriesId"] = staffel.ParentID
			}
		}
	}
	// Die Besetzung, Bilder über die eigene Bilderroute — nicht von TMDb,
	// damit der Browser nirgends anklopft.
	if leute, err := s.st.Besetzung(r.Context(), it.ID); err == nil && len(leute) > 0 {
		type person struct {
			Name  string `json:"name"`
			Rolle string `json:"rolle,omitempty"`
			Bild  string `json:"bild,omitempty"`
		}
		cast := make([]person, 0, len(leute))
		for i, p := range leute {
			x := person{Name: p.Name, Rolle: p.Rolle}
			if p.Bild != "" {
				x.Bild = fmt.Sprintf("/api/v1/art/%s/cast%02d", it.ID, i)
			}
			cast = append(cast, x)
		}
		body["cast"] = cast
	}
	if it.Kind == "series" {
		seasons, _ := s.st.Children(r.Context(), it.ID)
		s.standardTitelSetzen(r, seasons)
		body["seasons"] = seasons
	}
	if it.Kind == "movie" {
		if rh := s.reiheZumTitel(r, it.ID); rh != nil {
			body["collection"] = rh
		}
	}
	if it.Kind == "movie" || it.Kind == "series" {
		if q, err := s.scope(r); err == nil {
			if ids, err := s.st.Aehnliche(r.Context(), q, it.ID, 20); err == nil && len(ids) > 0 {
				aq := q
				aq.IDs, aq.Limit = ids, 20
				if its, err := s.st.Query(r.Context(), aq); err == nil && len(its) > 0 {
					if v, err := s.decorate(r, inOrder(its, ids)); err == nil {
						body["similar"] = v
					}
				}
			}
		}
	}
	ok(w, body)
}

func (s *Server) itemChildren(w http.ResponseWriter, r *http.Request) {
	if _, err := s.visibleItem(r, r.PathValue("id")); err != nil {
		// Für Staffeln steht die Freigabe am Elternteil; visibleItem folgt
		// der Kette nach oben.
		failErr(w, err)
		return
	}
	kids, err := s.st.Children(r.Context(), r.PathValue("id"))
	if err != nil {
		failErr(w, err)
		return
	}
	out, err := s.decorate(r, kids)
	if err != nil {
		failErr(w, err)
		return
	}
	ok(w, out)
}

// visibleItem holt einen Titel und prüft dabei, ob das Konto ihn sehen darf.
// Bei Folgen und Staffeln entscheidet die Serie darüber.
func (s *Server) visibleItem(r *http.Request, id string) (*store.Item, error) {
	it, err := s.st.Item(r.Context(), id)
	if err != nil {
		return nil, err
	}
	// Die ganze Kette: Folge, Staffel, Serie. Jede Freigabe darin zählt —
	// eine Folge mit eigener Freigabe ab 16 bleibt einem Kind mit 12 auch
	// dann verborgen, wenn die Serie ab 12 ist.
	kette := []*store.Item{it}
	root := it
	for root.ParentID != "" && len(kette) < 5 {
		p, err := s.st.Item(r.Context(), root.ParentID)
		if err != nil {
			break
		}
		root = p
		kette = append(kette, p)
	}
	q, err := s.scope(r)
	if err != nil {
		return nil, err
	}
	allowed := false
	for _, l := range q.Libraries {
		if l == root.LibraryID {
			allowed = true
			break
		}
	}
	if !allowed {
		return nil, store.ErrNotFound
	}
	if q.MaxAge > 0 || q.MitAltersgrenze {
		bewertet := false
		for _, k := range kette {
			if k.AgeYears == nil {
				continue
			}
			bewertet = true
			if *k.AgeYears > q.MaxAge {
				return nil, store.ErrNotFound
			}
		}
		if !bewertet && !q.AllowUnrated {
			return nil, store.ErrNotFound
		}
	}
	return it, nil
}

// ---------------------------------------------------------------- Startseite

type row struct {
	Key   string `json:"key"`
	Title string `json:"title"`
	Wide  bool   `json:"wide,omitempty"`
	Items []view `json:"items"`
}

func (s *Server) home(w http.ResponseWriter, r *http.Request) {
	acc := accountOf(r)
	q, err := s.scope(r)
	if err != nil {
		failErr(w, err)
		return
	}
	rows := []row{}

	// Weiterschauen kommt aus dem eigenen Profil — und nur daraus. Nach
	// einer fertigen Folge steht hier die nächste (wie bei Netflix).
	if weiter, err := s.st.Weiterschauen(r.Context(), acc.ID, 20); err == nil && len(weiter) > 0 {
		ids := make([]string, len(weiter))
		naechste := map[string]bool{}
		for i, x := range weiter {
			ids[i] = x.ItemID
			naechste[x.ItemID] = x.Naechste
		}
		rq := q
		rq.IDs, rq.Kinds = ids, []string{"movie", "series", "episode"}
		if its, err := s.st.Query(r.Context(), rq); err == nil && len(its) > 0 {
			its = inOrder(its, ids)
			if v, err := s.decorate(r, its); err == nil {
				for i := range v {
					v[i].NaechsteFolge = naechste[v[i].ID]
				}
				rows = append(rows, row{"resume", tr(w, "Weiterschauen"), true, v})
			}
		}
	}

	add := func(key, title string, sort string, kinds []string, wide bool) {
		nq := q
		nq.Sort, nq.Kinds, nq.Limit = sort, kinds, 20
		its, err := s.st.Query(r.Context(), nq)
		if err != nil || len(its) == 0 {
			return
		}
		if v, err := s.decorate(r, its); err == nil {
			rows = append(rows, row{key, tr(w, title), wide, v})
		}
	}
	add("new", "Neu dazugekommen", "added", nil, false)

	// Noch nicht angefangen: alles, wozu dieses Konto keinen Stand hat.
	angefangen, _ := s.st.AngefangeneIDs(r.Context(), acc.ID)
	if len(angefangen) > 0 {
		nq := q
		nq.NotIDs, nq.Sort, nq.Limit = angefangen, "random", 20
		if its, err := s.st.Query(r.Context(), nq); err == nil && len(its) > 0 {
			if v, err := s.decorate(r, its); err == nil {
				rows = append(rows, row{"unberuehrt", tr(w, "Noch nicht angefangen"), false, v})
			}
		}
	}

	add("top", "Gut bewertet", "rating", nil, false)

	// Eigene Sammlungen, die auf die Startseite sollen (sammlungen.go).
	rows = append(rows, s.sammlungsReihen(r)...)

	// Je ein Genre als eigene Reihe — die drei häufigsten, damit die
	// Startseite nicht nur „Filme" und „Serien" kennt.
	if genres, err := s.st.Genres(r.Context(), q); err == nil {
		for i, g := range genres {
			if i >= 3 {
				break
			}
			nq := q
			nq.Genre, nq.Sort, nq.Limit = g, "rating", 20
			if its, err := s.st.Query(r.Context(), nq); err == nil && len(its) >= 4 {
				if v, err := s.decorate(r, its); err == nil {
					rows = append(rows, row{"genre:" + g, g, false, v})
				}
			}
		}
	}

	// Nochmal ansehen: was dieses Konto zu Ende gesehen hat.
	if ids, err := s.st.FertigeIDs(r.Context(), acc.ID, 20); err == nil && len(ids) > 0 {
		rq := q
		rq.IDs, rq.Kinds = ids, []string{"movie", "series"}
		if its, err := s.st.Query(r.Context(), rq); err == nil && len(its) > 0 {
			its = inOrder(its, ids)
			if v, err := s.decorate(r, its); err == nil {
				rows = append(rows, row{"nochmal", tr(w, "Nochmal ansehen"), false, v})
			}
		}
	}

	add("movies", "Filme", "title", []string{"movie"}, false)
	add("series", "Serien", "title", []string{"series"}, false)
	add("zufall", "Zufällig", "random", nil, false)

	// Merkliste ganz unten, wenn etwas drin ist.
	if ids, err := s.st.WatchlistIDs(r.Context(), acc.ID); err == nil && len(ids) > 0 {
		nq := q
		nq.IDs, nq.Limit = ids, 20
		if its, err := s.st.Query(r.Context(), nq); err == nil && len(its) > 0 {
			if v, err := s.decorate(r, its); err == nil {
				rows = append(rows, row{"watchlist", tr(w, "Deine Merkliste"), false, v})
			}
		}
	}
	ok(w, map[string]any{"rows": rows})
}

func inOrder(items []store.Item, ids []string) []store.Item {
	pos := map[string]int{}
	for i, id := range ids {
		pos[id] = i
	}
	sort.SliceStable(items, func(i, j int) bool { return pos[items[i].ID] < pos[items[j].ID] })
	return items
}

// ---------------------------------------------------------------- Mein Stand

func (s *Server) saveProgress(w http.ResponseWriter, r *http.Request) {
	var in struct {
		PositionMS int64 `json:"positionMs"`
		DurationMS int64 `json:"durationMs"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if _, err := s.visibleItem(r, r.PathValue("id")); err != nil {
		failErr(w, err)
		return
	}
	if err := s.st.SaveProgress(r.Context(), accountOf(r).ID, r.PathValue("id"),
		in.PositionMS, in.DurationMS); err != nil {
		failErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// markFinished setzt den Haken. Bei einer Serie gilt er für alle Folgen
// (mit ?staffel=N nur für diese Staffel), bei einer Staffel für ihre
// Folgen — die Serie selbst gilt als gesehen, sobald jede Folge es ist,
// und nicht, weil jemand auf ihre Seite einen Haken gesetzt hat. Bei einer
// Folge markiert ?vorherige=1 auch alle Folgen davor („bis hier gesehen").
func (s *Server) markFinished(w http.ResponseWriter, r *http.Request) {
	it, err := s.visibleItem(r, r.PathValue("id"))
	if err != nil {
		failErr(w, err)
		return
	}
	folgen, err := s.betroffeneFolgen(r, it)
	if errors.Is(err, errStaffel) {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		failErr(w, err)
		return
	}
	acc := accountOf(r).ID
	if folgen == nil {
		err = s.st.MarkFinished(r.Context(), acc, it.ID, it.RuntimeMS)
	} else {
		ids := make([]string, len(folgen))
		dauer := map[string]int64{}
		for i, f := range folgen {
			ids[i] = f.ID
			dauer[f.ID] = f.RuntimeMS
		}
		err = s.st.MarkFinishedMehrere(r.Context(), acc, ids, dauer)
	}
	if err != nil {
		failErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// clearFinished nimmt den Haken zurück — und den Fortschritt gleich mit.
// Bei Serie und Staffel für alle betroffenen Folgen, wie markFinished.
func (s *Server) clearFinished(w http.ResponseWriter, r *http.Request) {
	it, err := s.visibleItem(r, r.PathValue("id"))
	if err != nil {
		failErr(w, err)
		return
	}
	folgen, err := s.betroffeneFolgen(r, it)
	if errors.Is(err, errStaffel) {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		failErr(w, err)
		return
	}
	acc := accountOf(r).ID
	if folgen == nil || it.Kind == "episode" {
		// „vorherige" gilt nur fürs Setzen; zurückgenommen wird die eine Folge.
		err = s.st.ClearProgress(r.Context(), acc, it.ID)
	} else {
		// Den alten Haken an der Serie selbst (von früher) gleich mit weg.
		ids := []string{it.ID}
		for _, f := range folgen {
			ids = append(ids, f.ID)
		}
		err = s.st.ClearProgressMehrere(r.Context(), acc, ids)
	}
	if err != nil {
		failErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// weiterInSerie beantwortet „Welche Folge startet ‚Weiterschauen'?" für
// eine Serie — die Folge nach der zuletzt geschauten, nicht die erste
// ungesehene irgendwo am Anfang. Alle Geräte fragen hier, damit Web und
// Apps dieselbe Folge wählen.
func (s *Server) weiterInSerie(w http.ResponseWriter, r *http.Request) {
	it, err := s.visibleItem(r, r.PathValue("id"))
	if err != nil {
		failErr(w, err)
		return
	}
	if it.Kind != "series" {
		fail(w, http.StatusBadRequest, "nur für serien")
		return
	}
	id, err := s.st.WeiterInSerie(r.Context(), accountOf(r).ID, it.ID)
	if err != nil {
		failErr(w, err)
		return
	}
	folge, err := s.st.Item(r.Context(), id)
	if err != nil {
		failErr(w, err)
		return
	}
	v, err := s.decorate(r, []store.Item{*folge})
	if err != nil {
		failErr(w, err)
		return
	}
	ok(w, map[string]any{"item": v[0]})
}

// errStaffel: ?staffel= ist keine Zahl ab 0.
var errStaffel = errors.New("staffel muss eine zahl ab 0 sein")

// betroffeneFolgen: welche Folgen ein Haken an diesem Titel meint. nil
// heisst: nur der Titel selbst (ein Film, eine einzelne Folge).
func (s *Server) betroffeneFolgen(r *http.Request, it *store.Item) ([]store.Item, error) {
	switch it.Kind {
	case "series":
		staffel := -1
		if v := r.URL.Query().Get("staffel"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < 0 {
				return nil, errStaffel
			}
			staffel = n
		}
		return s.st.FolgenVon(r.Context(), it.ID, staffel)
	case "season":
		return s.st.FolgenVon(r.Context(), it.ID, -1)
	case "episode":
		if r.URL.Query().Get("vorherige") != "1" || it.SeasonNo <= 0 {
			return nil, nil
		}
		serie, err := s.st.SeriesOf(r.Context(), it.ID)
		if err != nil {
			return nil, nil
		}
		alle, err := s.st.FolgenVon(r.Context(), serie, -1)
		if err != nil {
			return nil, err
		}
		var bis []store.Item
		for _, f := range alle {
			if f.SeasonNo <= 0 {
				continue // Specials gehören nicht zu „davor"
			}
			bis = append(bis, f)
			if f.ID == it.ID {
				return bis, nil
			}
		}
		return nil, nil
	}
	return nil, nil
}

func (s *Server) setRating(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Stars  int  `json:"stars"`
		Shared bool `json:"shared"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if _, err := s.visibleItem(r, r.PathValue("id")); err != nil {
		failErr(w, err)
		return
	}
	if err := s.st.SetRating(r.Context(), accountOf(r).ID, r.PathValue("id"),
		in.Stars, in.Shared); err != nil {
		failErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) addFavorit(w http.ResponseWriter, r *http.Request) {
	if _, err := s.visibleItem(r, r.PathValue("id")); err != nil {
		failErr(w, err)
		return
	}
	if err := s.st.AddFavorit(r.Context(), accountOf(r).ID, r.PathValue("id")); err != nil {
		failErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) removeFavorit(w http.ResponseWriter, r *http.Request) {
	if err := s.st.RemoveFavorit(r.Context(), accountOf(r).ID, r.PathValue("id")); err != nil {
		failErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// buchstaben: GET /items/letters?kind=movie — wie viele Titel unter jedem
// Anfangsbuchstaben stehen, für die Buchstabenleiste.
func (s *Server) buchstaben(w http.ResponseWriter, r *http.Request) {
	q, err := s.scope(r)
	if err != nil {
		failErr(w, err)
		return
	}
	if k := r.URL.Query().Get("kind"); k != "" {
		q.Kinds = strings.Split(k, ",")
	}
	q.Genre = r.URL.Query().Get("genre")
	b, err := s.st.Buchstaben(r.Context(), q)
	if err != nil {
		failErr(w, err)
		return
	}
	ok(w, b)
}

func (s *Server) addWatchlist(w http.ResponseWriter, r *http.Request) {
	if _, err := s.visibleItem(r, r.PathValue("id")); err != nil {
		failErr(w, err)
		return
	}
	if err := s.st.AddToWatchlist(r.Context(), accountOf(r).ID, r.PathValue("id")); err != nil {
		failErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) removeWatchlist(w http.ResponseWriter, r *http.Request) {
	if err := s.st.RemoveFromWatchlist(r.Context(), accountOf(r).ID, r.PathValue("id")); err != nil {
		failErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------- Wünsche

func (s *Server) wishes(w http.ResponseWriter, r *http.Request) {
	acc := accountOf(r)
	// Der Besitzer sieht alle Wünsche des Haushalts, alle anderen ihre eigenen.
	list, err := s.st.Wishes(r.Context(), acc.ID, acc.IsAdmin)
	if err != nil {
		failErr(w, err)
		return
	}
	ok(w, list)
}

func (s *Server) createWish(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Title string `json:"title"`
		Year  int    `json:"year"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	wish, err := s.st.CreateWish(r.Context(), accountOf(r).ID, in.Title, in.Year)
	if err != nil {
		failErr(w, err)
		return
	}
	ok(w, wish)
}

// ---------------------------------------------------------------- Verwaltung

func (s *Server) adminOverview(w http.ResponseWriter, r *http.Request) {
	libs, _ := s.st.Libraries(r.Context())
	accs, _ := s.st.Accounts(r.Context())
	// Die Übersicht zeigt nur die Zahlen — gezählt wird in der Datenbank,
	// statt alle Doppel samt Dateien und alle fehlenden Dateien zu laden.
	dups, _ := s.st.DoppelZahl(r.Context())
	missing, _ := s.st.FehlendZahl(r.Context())
	load := s.live.Load()

	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	ok(w, map[string]any{
		"version":   s.version,
		"uptimeMs":  time.Since(s.started).Milliseconds(),
		"libraries": libs,
		"accounts":  len(accs),
		"load":      load,
		"cleanup": map[string]int{
			"duplicates": dups,
			"missing":    missing,
			"unmatched":  s.st.UnmatchedCount(r.Context()),
			"pending":    s.st.PendingCount(r.Context()),
		},
		"ffmpeg": s.ff != nil && s.ff.Available(),
		// Freier Platz: für die Datenbank und für die Zwischendateien.
		// Läuft einer voll, steht hier eine Warnung, bevor es knallt.
		"platz": map[string]uint64{
			"datenMB":      stream.FreierPlatz(s.dataDir) / 1024 / 1024,
			"umwandlungMB": stream.FreierPlatz(orElse(s.umwandlungDir, s.dataDir)) / 1024 / 1024,
		},
		"sicherung":  s.st.Setting(r.Context(), "sicherung.zuletzt", ""),
		"memoryMB":   mem.Sys / 1024 / 1024,
		"goroutines": runtime.NumGoroutine(),
		"source":     s.adminSource(),
		// Bewusst nicht dabei: irgendetwas darüber, wer was geschaut hat.
		"privacyNote": tr(w, "Lumen zeigt nicht, wer was schaut."),
	})
}

// adminRemote sagt, ob der Fernzugriff steht.
//
// Was hier herauskommt, beschreibt die Leitung — verbunden oder nicht, seit
// wann, welcher Vermittler. Nicht, wer von aussen zuschaut oder was. Der
// Vermittler wüsste es selbst nicht; er reicht verschlüsselte Bytes durch.
func (s *Server) adminRemote(w http.ResponseWriter, r *http.Request) {
	if s.fernzugriff == nil {
		ok(w, map[string]any{
			"an": false,
			"hinweis": tr(w, "Von aussen ist Lumen gerade nicht erreichbar. "+
				"Dafür braucht es einen Vermittler — siehe README.md, Abschnitt „Von unterwegs schauen“."),
		})
		return
	}
	ok(w, s.fernzugriff())
}

// adminSource sagt, wo der Quelltext liegt und ob das eine eigene
// Fundstelle ist oder die Voreinstellung.
func (s *Server) adminSource() map[string]any {
	return map[string]any{
		"url":   s.sourceURL,
		"eigen": s.SourceURLIstEigen(),
	}
}

func (s *Server) adminLibraries(w http.ResponseWriter, r *http.Request) {
	libs, err := s.st.Libraries(r.Context())
	if err != nil {
		failErr(w, err)
		return
	}
	if darf := s.verwalteteMediatheken(r); darf != nil {
		sichtbar := libs[:0]
		for _, l := range libs {
			if darf[l.ID] {
				sichtbar = append(sichtbar, l)
			}
		}
		libs = sichtbar
	}
	out := make([]bibliothekAnsicht, len(libs))
	for i, l := range libs {
		e := bibliothekAnsicht{Library: l,
			Scanning: s.scanner.Running(l.ID), Stand: s.scanner.Stand(l.ID)}
		if _, w := wartend.Load(l.ID); w && !e.Scanning {
			e.Wartet = true
		}
		if s.meta != nil {
			if st := s.meta.Stand(); st.Laeuft && (st.Library == "" || st.Library == l.ID) {
				e.Metadaten = &st
			}
		}
		if err := pfadPruefen(l.Path); err != nil {
			e.Problem = err.Error()
		} else {
			e.Erreichbar = true
		}
		out[i] = e
	}
	ok(w, out)
}

func (s *Server) adminCreateLibrary(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
		Kind string `json:"kind"`
		Path string `json:"path"`
		Lang string `json:"language"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	// Den Ordner prüfen, bevor etwas angelegt wird. Vorher nahm der
	// Server jeden Pfad an — auch einen leeren — und heraus kam eine
	// Bibliothek, die stumm für immer leer blieb.
	if err := pfadPruefen(in.Path); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	// Nur die Arten, die der Durchlauf kennt. Eine unbekannte („photos",
	// „show") nahm der Server an und las den Ordner dann als Serien —
	// Fotos und Musik tauchten als Titel auf, die Bereiche fehlten.
	switch strings.TrimSpace(in.Kind) {
	case "movie", "series", "music", "photo":
	default:
		fail(w, http.StatusBadRequest, "unbekannte art — movie, series, music oder photo")
		return
	}

	// Derselbe Ordner zweimal (Doppelklick, nochmal versucht) oder ein
	// Ordner in einem anderen: Die Dateien gehörten dann zwei Bibliotheken,
	// und das Löschen der einen leerte die andere.
	if libs, err := s.st.Libraries(r.Context()); err == nil {
		for _, l := range libs {
			if ordnerUeberschneiden(l.Path, in.Path) {
				failW(w, http.StatusConflict,
					"dieser ordner gehört schon zur bibliothek „{name}“ (oder liegt darin)", "name", l.Name)
				return
			}
		}
	}
	lib, err := s.st.CreateLibrary(r.Context(), in.Name, strings.TrimSpace(in.Kind),
		strings.TrimSpace(in.Path), in.Lang)
	if err != nil {
		failErr(w, err)
		return
	}
	s.st.Log(r.Context(), "info", "library", fmt.Sprintf("Bibliothek %q angelegt", in.Name))
	ok(w, lib)
}

func (s *Server) adminScan(w http.ResponseWriter, r *http.Request) {
	libs, err := s.st.Libraries(r.Context())
	if err != nil {
		failErr(w, err)
		return
	}
	id := r.PathValue("id")
	for _, l := range libs {
		if l.ID != id {
			continue
		}
		// Vorher nachsehen, ob der Ordner da ist. Sonst liefe der
		// Durchlauf ins Leere und es stünde nur im Protokoll, warum —
		// hier kann es dem, der gerade auf „Prüfen" gedrückt hat, direkt
		// gesagt werden.
		if err := pfadPruefen(l.Path); err != nil {
			fail(w, http.StatusConflict, err.Error())
			return
		}
		// Läuft oder wartet schon ein Durchlauf, kommt kein zweiter dazu.
		// Nachsehen und Anmelden in einem Schritt (anmelden), damit zwei
		// schnelle Klicks nicht zwei Durchläufe einreihen.
		_, laeuft := inArbeit.Load(l.ID)
		if laeuft || s.scanner.Running(l.ID) {
			fail(w, http.StatusConflict, "für diese bibliothek läuft schon ein durchlauf")
			return
		}
		a, angemeldet := anmelden(l.ID)
		if !angemeldet {
			fail(w, http.StatusConflict, "für diese bibliothek läuft schon ein durchlauf")
			return
		}
		// Ein Scan über ein paar Terabyte dauert; die Anfrage wartet nicht.
		// Der Auftrag steht schon in wartend — sofort sichtbar, nicht erst,
		// wenn die Goroutine dran ist.
		go s.durchlaufMitAuftrag(s.hintergrund(), l, false, a)
		ok(w, map[string]string{"status": "läuft"})
		return
	}
	fail(w, http.StatusNotFound, "bibliothek nicht gefunden")
}

func (s *Server) adminSessions(w http.ResponseWriter, r *http.Request) {
	// Was hier zurückkommt, ist stream.AdminSession: direkt oder
	// umgewandelt, sonst nichts. Kein Titel, keine Person, kein Gerät, kein
	// Ort, keine Startzeit, kein Grund, keine Qualität — und die Kennungen
	// gelten nur für diese eine Antwort (stream/live.go).
	ok(w, map[string]any{
		"sessions": s.live.ForOwner(),
		"load":     s.live.Load(),
		"note": tr(w, "Nur wie viel der Server gerade rechnet — nicht wer, was, wo oder seit wann. "+
			"Die Liste zeigt bei jedem Öffnen neue Kennungen, damit sich keine Wiedergabe verfolgen lässt."),
	})
}

func (s *Server) adminStopSession(w http.ResponseWriter, r *http.Request) {
	if !s.live.EndAdmin(r.PathValue("id")) {
		fail(w, http.StatusNotFound, "diese wiedergabe läuft nicht mehr")
		return
	}
	s.st.Log(r.Context(), "info", "stream", "Wiedergabe vom Besitzer beendet")
	w.WriteHeader(http.StatusNoContent)
}

// meineWiedergaben: die eigenen laufenden Wiedergaben, damit man einen
// vergessenen Fernseher selbst beenden kann.
func (s *Server) meineWiedergaben(w http.ResponseWriter, r *http.Request) {
	ok(w, s.live.ForAccount(accountOf(r).ID))
}

func (s *Server) meineWiedergabeBeenden(w http.ResponseWriter, r *http.Request) {
	if !s.live.EndEigene(accountOf(r).ID, r.PathValue("id")) {
		fail(w, http.StatusNotFound, "diese wiedergabe läuft nicht mehr")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) adminLog(w http.ResponseWriter, r *http.Request) {
	lines, err := s.st.RecentLog(r.Context(), r.URL.Query().Get("level"), qInt(r, "limit", 200))
	if err != nil {
		failErr(w, err)
		return
	}
	ok(w, lines)
}

func (s *Server) adminCleanup(w http.ResponseWriter, r *http.Request) {
	dups, err := s.st.Duplicates(r.Context())
	if err != nil {
		failErr(w, err)
		return
	}
	missing, err := s.st.MissingFiles(r.Context())
	if err != nil {
		failErr(w, err)
		return
	}
	unmatched, err := s.st.UnmatchedItems(r.Context(), 100)
	if err != nil {
		failErr(w, err)
		return
	}
	// Zu jedem nicht erkannten Titel der Pfad einer Datei: Ordner und
	// Dateiname sagen oft schon, was es ist.
	type mitPfad struct {
		store.Item
		Pfad string `json:"pfad,omitempty"`
	}
	// Ein auf einzelne Mediatheken beschränkter Mitverwalter sieht nur
	// deren Titel — und deren Pfade.
	if darf := s.verwalteteMediatheken(r); darf != nil {
		inDarf := func(itemID string) bool {
			it, err := s.st.Item(r.Context(), itemID)
			return err == nil && darf[it.LibraryID]
		}
		d := dups[:0]
		for _, x := range dups {
			if inDarf(x.ItemID) {
				d = append(d, x)
			}
		}
		dups = d
		m := missing[:0]
		for _, f := range missing {
			if inDarf(f.ItemID) {
				m = append(m, f)
			}
		}
		missing = m
		u := unmatched[:0]
		for _, it := range unmatched {
			if darf[it.LibraryID] {
				u = append(u, it)
			}
		}
		unmatched = u
	}
	ohne := make([]mitPfad, 0, len(unmatched))
	for _, it := range unmatched {
		ohne = append(ohne, mitPfad{Item: it, Pfad: s.st.ErsterPfad(r.Context(), it.ID)})
	}
	ok(w, map[string]any{"duplicates": dups, "missing": missing,
		"unmatched": ohne, "pending": s.st.PendingCount(r.Context())})
}

func (s *Server) adminAccounts(w http.ResponseWriter, r *http.Request) {
	accs, err := s.st.Accounts(r.Context())
	if err != nil {
		failErr(w, err)
		return
	}
	type entry struct {
		store.Account
		Limits    store.Limits `json:"limits"`
		Libraries []string     `json:"libraries"`
		Devices   int          `json:"devices"`
	}
	out := make([]entry, 0, len(accs))
	for _, a := range accs {
		lim, _ := s.st.Limits(r.Context(), a.ID)
		libs, _ := s.st.GrantedLibraries(r.Context(), a.ID)
		devs, _ := s.st.Devices(r.Context(), a.ID)
		out = append(out, entry{a, lim, libs, len(devs)})
	}
	ok(w, out)
}

func (s *Server) adminSetLimits(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Limits    store.Limits `json:"limits"`
		Libraries []string     `json:"libraries"`
		// Kinderprofil: optional, macht ein Konto zum Kinderprofil oder
		// nimmt es zurück. Fehlt es, bleibt es, wie es ist.
		Kinderprofil *bool `json:"kinderprofil"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if err := s.mitverwalterDarf(r, r.PathValue("id"), in.Libraries); err != nil {
		fail(w, http.StatusForbidden, err.Error())
		return
	}
	// Unmögliche Werte (0 gleichzeitig, Altersgrenze 7) gar nicht erst
	// speichern — sonst zeigt die Oberfläche etwas anderes, als gilt.
	if in.Limits.RemoteQuality == "" {
		in.Limits.RemoteQuality = "1080p"
	}
	if err := store.PruefeGrenzen(in.Limits); err != nil {
		failErr(w, err)
		return
	}
	ziel, err := s.st.Account(r.Context(), r.PathValue("id"))
	if err != nil {
		failErr(w, err)
		return
	}
	kind := ziel.IsKid
	if in.Kinderprofil != nil {
		kind = *in.Kinderprofil
	}
	if kind {
		if err := store.PruefeKinderGrenzen(in.Limits); err != nil {
			failErr(w, err)
			return
		}
	}
	if in.Kinderprofil != nil && *in.Kinderprofil != ziel.IsKid {
		if err := s.st.SetKinderprofil(r.Context(), ziel.ID, *in.Kinderprofil); err != nil {
			if rechteFehler(w, err) {
				return
			}
			failErr(w, err)
			return
		}
	}
	if err := s.st.SetLimits(r.Context(), r.PathValue("id"), in.Limits); err != nil {
		failErr(w, err)
		return
	}
	if in.Libraries != nil {
		if err := s.st.SetGrants(r.Context(), r.PathValue("id"), in.Libraries); err != nil {
			failErr(w, err)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) adminCreateInvite(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Libraries []string     `json:"libraries"`
		Limits    store.Limits `json:"limits"`
		Days      int          `json:"days"`
		// Kinderprofil mit Altersgrenze (0, 6, 12 oder 16): Kinder-
		// Vorgaben, keine ungeprüften Titel, kein Herunterladen. Früher
		// legte jede Einladung ein Erwachsenenkonto an.
		Kinderprofil bool `json:"kinderprofil"`
		Altersgrenze *int `json:"altersgrenze"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if in.Kinderprofil {
		alter := 12
		if in.Altersgrenze != nil {
			alter = *in.Altersgrenze
		}
		lim, err := store.KinderGrenzen(alter)
		if err != nil {
			failErr(w, err)
			return
		}
		in.Limits = lim
	} else if in.Limits != (store.Limits{}) {
		// Leere Grenzen heissen „die Vorgabe"; alles andere muss stimmen.
		if in.Limits.RemoteQuality == "" {
			in.Limits.RemoteQuality = "1080p"
		}
		if err := store.PruefeGrenzen(in.Limits); err != nil {
			failErr(w, err)
			return
		}
	}
	if in.Days <= 0 {
		in.Days = 7
	}
	if in.Days > 30 {
		in.Days = 30
	}
	if err := s.mitverwalterDarf(r, "", in.Libraries); err != nil {
		fail(w, http.StatusForbidden, err.Error())
		return
	}
	inv, err := s.st.CreateInvite(r.Context(), accountOf(r).ID,
		time.Duration(in.Days)*24*time.Hour,
		map[string]any{"libraries": in.Libraries, "limits": in.Limits, "kinderprofil": in.Kinderprofil})
	if err != nil {
		failErr(w, err)
		return
	}
	ok(w, inv)
}

func (s *Server) adminSetWishState(w http.ResponseWriter, r *http.Request) {
	var in struct {
		State string `json:"state"`
		Note  string `json:"note"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if err := s.st.SetWishState(r.Context(), r.PathValue("id"), in.State, in.Note); err != nil {
		failErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------- Hilfen

func orElse(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

// meinGeraetAbmelden meldet das Gerät ab, das fragt — beim Abmelden im
// Browser und wenn eine App einen Server aus ihrer Liste entfernt.
func (s *Server) meinGeraetAbmelden(w http.ResponseWriter, r *http.Request) {
	if d := deviceOf(r); d != nil {
		if err := s.st.RevokeToken(r.Context(), d.TokenHash); err != nil {
			failErr(w, err)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// adminServerName setzt den Namen, unter dem die Apps diesen Server zeigen.
func (s *Server) adminServerName(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	name := strings.TrimSpace(in.Name)
	if len([]rune(name)) > 40 {
		fail(w, http.StatusBadRequest, "höchstens 40 Zeichen")
		return
	}
	if err := s.st.SetSetting(r.Context(), "server.name", name); err != nil {
		failErr(w, err)
		return
	}
	ok(w, map[string]any{"name": name})
}

// profilZuName sucht ein Profil über seinen Namen, ohne Rücksicht auf
// Gross- und Kleinschreibung. Leer, wenn es keins oder mehrere gibt.
func (s *Server) profilZuName(ctx context.Context, name string) string {
	accs, err := s.st.Accounts(ctx)
	if err != nil {
		return ""
	}
	name = strings.TrimSpace(name)
	id := ""
	for _, a := range accs {
		if strings.EqualFold(a.Name, name) {
			if id != "" {
				return ""
			}
			id = a.ID
		}
	}
	return id
}

// kuerzen schneidet auf höchstens n Zeichen, ohne ein Zeichen zu zerteilen.
func kuerzen(t string, n int) string {
	r := []rune(strings.TrimSpace(t))
	if len(r) > n {
		r = r[:n]
	}
	return string(r)
}

// mitverwalterDarf zieht die Grenze für Mitverwalter (Gruppe mit
// Verwaltung, nicht der Besitzer): Sie verwalten Konten ohne
// Verwaltungsrecht und geben nur Bibliotheken frei, die sie selbst sehen
// dürfen. Sonst könnte sich ein Mitverwalter per Einladung ein zweites
// Konto mit allen Bibliotheken anlegen, oder die Grenzen des Besitzers
// ändern.
func (s *Server) mitverwalterDarf(r *http.Request, zielID string, bibliotheken []string) error {
	ich := accountOf(r)
	if ich.IsOwner {
		return nil
	}
	if ich.Vollzugriff {
		// Alles wie der Besitzer — nur sein Konto selbst nicht. Sonst
		// könnte jemand mit Vollzugriff den Besitzer aussperren.
		if zielID != "" {
			if ziel, err := s.st.Account(r.Context(), zielID); err == nil && ziel.IsOwner {
				return errors.New("das konto des besitzers ändert nur er selbst")
			}
		}
		return nil
	}
	if zielID != "" {
		ziel, err := s.st.Account(r.Context(), zielID)
		if err != nil {
			return errors.New("dieses konto gibt es nicht")
		}
		if ziel.IsOwner || ziel.IsAdmin {
			return errors.New("konten mit verwaltungsrecht ändert nur der besitzer")
		}
	}
	if len(bibliotheken) == 0 {
		return nil
	}
	eigene, err := s.st.GrantedLibraries(r.Context(), ich.ID)
	if err != nil {
		return err
	}
	erlaubt := map[string]bool{}
	for _, l := range eigene {
		erlaubt[l] = true
	}
	for _, l := range bibliotheken {
		if !erlaubt[l] {
			return errors.New("du kannst nur bibliotheken freigeben, die du selbst sehen darfst")
		}
	}
	return nil
}

// pfadFuer: Den ganzen Pfad einer Datei sieht nur die Verwaltung. Alle
// anderen bekommen den Dateinamen — wie die Ordner auf dem NAS heissen,
// geht sie nichts an.
func pfadFuer(acc *store.Account, pfad string) string {
	if acc != nil && acc.IsAdmin {
		return pfad
	}
	return filepath.Base(pfad)
}

// gesund: Datenbank lesbar? Für die Gesundheitsprüfung des Containers.
func (s *Server) gesund(w http.ResponseWriter, r *http.Request) {
	if err := s.st.DB().PingContext(r.Context()); err != nil {
		fail(w, http.StatusServiceUnavailable, "datenbank antwortet nicht")
		return
	}
	var n int
	if err := s.st.DB().QueryRowContext(r.Context(), `SELECT 1`).Scan(&n); err != nil {
		fail(w, http.StatusServiceUnavailable, "datenbank antwortet nicht")
		return
	}
	ok(w, map[string]string{"status": "ok"})
}

// links: Wo es den Titel sonst noch gibt — IMDb, TheMovieDb, TheTVDB,
// Trakt (wie Emby). Nur Adressen aus den gespeicherten Kennungen; Lumen
// fragt dafür nirgends nach.
type quellLink struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

func (s *Server) links(ctx context.Context, it *store.Item) []quellLink {
	if it.Kind != "movie" && it.Kind != "series" {
		return nil
	}
	nurZiffern := func(v string) bool {
		if v == "" {
			return false
		}
		for _, c := range v {
			if c < '0' || c > '9' {
				return false
			}
		}
		return true
	}
	art, traktArt := "movie", "movie"
	if it.Kind == "series" {
		art, traktArt = "tv", "show"
	}
	var out []quellLink
	imdb := it.IMDbID
	if !strings.HasPrefix(imdb, "tt") || !nurZiffern(imdb[2:]) {
		imdb = ""
	}
	if imdb != "" {
		out = append(out, quellLink{"IMDb", "https://www.imdb.com/title/" + imdb + "/"})
	}
	tmdb, _ := s.st.SourceID(ctx, it.ID, "tmdb")
	if nurZiffern(tmdb) {
		out = append(out, quellLink{"TheMovieDb", "https://www.themoviedb.org/" + art + "/" + tmdb})
	}
	if tvdb, _ := s.st.SourceID(ctx, it.ID, "tvdb"); nurZiffern(tvdb) {
		out = append(out, quellLink{"TheTVDB", "https://thetvdb.com/dereferrer/series/" + tvdb})
	}
	switch {
	case nurZiffern(tmdb):
		out = append(out, quellLink{"Trakt", "https://trakt.tv/search/tmdb/" + tmdb + "?id_type=" + traktArt})
	case imdb != "":
		out = append(out, quellLink{"Trakt", "https://trakt.tv/search/imdb/" + imdb + "?id_type=" + traktArt})
	}
	return out
}

// verwalteteMediatheken: nil für Besitzerrechte (alles), sonst die
// Mediatheken, die dieses Konto sehen und darum verwalten darf.
func (s *Server) verwalteteMediatheken(r *http.Request) map[string]bool {
	acc := accountOf(r)
	if acc == nil || acc.Besitzerrechte() {
		return nil
	}
	ids, _ := s.st.GrantedLibraries(r.Context(), acc.ID)
	out := make(map[string]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out
}

// ordnerUeberschneiden: a und b sind derselbe Ordner, oder einer liegt im
// anderen (nach dem Auflösen von Verweisen).
func ordnerUeberschneiden(a, b string) bool {
	norm := func(p string) string {
		p = filepath.Clean(strings.TrimSpace(p))
		if echt, err := filepath.EvalSymlinks(p); err == nil {
			p = echt
		}
		return p
	}
	a, b = norm(a), norm(b)
	drin := func(innen, aussen string) bool {
		rel, err := filepath.Rel(aussen, innen)
		return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
	}
	return drin(a, b) || drin(b, a)
}
