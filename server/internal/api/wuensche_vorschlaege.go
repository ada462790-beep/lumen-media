// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

// Wünschen mit Vorschlägen.
//
// GET /api/v1/wishes/vorschlaege?q=dune — was TMDb zu einem Namen kennt,
// höchstens acht Filme und Serien, mit Poster, Jahr und IMDb-Kennung
// (soweit schon bekannt), und je Treffer:
//
//   - schonDa: steht schon in einer Bibliothek, die dieses Profil sieht
//     (dann gibt es „Ansehen" statt „Wünschen");
//   - schonGewuenscht: jemand hat ihn schon gewünscht — wer, steht nicht
//     da. Wer ihn noch einmal wünscht, schliesst sich an.
//
// Kinderprofile (und jedes Profil mit Altersgrenze) sehen nur Titel, deren
// Freigabe bei TMDb passt; ohne bekannte Freigabe nur, wenn das Profil
// ungeprüfte Titel sehen darf. Ohne TMDb-Schlüssel kommt
// {"ohneSchluessel": true}, und gewünscht wird mit dem Namen allein.
//
// TMDb wird sparsam gefragt: gleiche Suchen kommen eine Weile aus dem
// Speicher, die Einzelheiten eines Eintrags (IMDb, Freigabe) einen Tag,
// und je Profil gibt es eine Bremse. Die Poster laufen über Lumen
// (GET /api/v1/wishes/poster/w92/abc.jpg) — der Browser fragt TMDb nie
// selbst, und Poster von Wünschen bleiben auf der Platte.

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"lumen/internal/ablage"
	"lumen/internal/meta"
	"lumen/internal/sprache"
	"lumen/internal/store"
)

const (
	wunschVorschlaegeMax  = 8
	wunschSucheGilt       = 10 * time.Minute
	wunschDetailsGelten   = 24 * time.Hour
	wunschSucheEintraege  = 300
	wunschDetailEintraege = 2000
	wunschBildEintraege   = 400
	wunschBildBytes       = 12 << 20
	// Bremse: so viele Anfragen an TMDb je Profil im Fenster. Die Web-App
	// wartet 300 ms nach dem letzten Tastendruck; wer zügig tippt, kommt
	// nicht an die Grenze.
	wunschBremseAnzahl  = 12
	wunschBremseFenster = 10 * time.Second
)

// wunschSpeicher hält die Zwischenspeicher der Vorschläge.
type wunschSpeicher struct {
	mu       sync.Mutex
	suche    map[string]wunschSucheEintrag
	details  map[string]wunschDetailEintrag
	bremse   map[string][]time.Time
	bilder   map[string]wunschBildEintrag
	bildSumm int
}

type wunschSucheEintrag struct {
	bis   time.Time
	liste []meta.Vorschlag
}

type wunschDetailEintrag struct {
	bis  time.Time
	v    meta.Vorschlag
	voll bool // mit Kurzdetails (IMDb, Freigabe), nicht nur aus der Suche
}

type wunschBildEintrag struct {
	zuletzt time.Time
	daten   []byte
	art     string
}

func (ws *wunschSpeicher) sucheHolen(schluessel string) ([]meta.Vorschlag, bool) {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	e, da := ws.suche[schluessel]
	if !da || time.Now().After(e.bis) {
		return nil, false
	}
	return e.liste, true
}

func (ws *wunschSpeicher) sucheMerken(schluessel string, liste []meta.Vorschlag) {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	if ws.suche == nil {
		ws.suche = map[string]wunschSucheEintrag{}
	}
	jetzt := time.Now()
	if len(ws.suche) >= wunschSucheEintraege {
		for k, e := range ws.suche {
			if jetzt.After(e.bis) {
				delete(ws.suche, k)
			}
		}
		for k := range ws.suche { // immer noch voll: irgendeinen weg
			if len(ws.suche) < wunschSucheEintraege {
				break
			}
			delete(ws.suche, k)
		}
	}
	ws.suche[schluessel] = wunschSucheEintrag{bis: jetzt.Add(wunschSucheGilt), liste: liste}
	// Was die Suche bringt, kennt auch „Wünschen" — falls TMDb danach
	// gerade nicht antwortet.
	if ws.details == nil {
		ws.details = map[string]wunschDetailEintrag{}
	}
	for _, v := range liste {
		k := v.Art + ":" + v.TMDbID
		if _, da := ws.details[k]; !da && len(ws.details) < wunschDetailEintraege {
			ws.details[k] = wunschDetailEintrag{bis: jetzt.Add(wunschDetailsGelten), v: v}
		}
	}
}

func (ws *wunschSpeicher) detailHolen(k string) (wunschDetailEintrag, bool) {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	e, da := ws.details[k]
	if !da || time.Now().After(e.bis) {
		return wunschDetailEintrag{}, false
	}
	return e, true
}

func (ws *wunschSpeicher) detailMerken(v meta.Vorschlag) {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	if ws.details == nil {
		ws.details = map[string]wunschDetailEintrag{}
	}
	jetzt := time.Now()
	if len(ws.details) >= wunschDetailEintraege {
		for k, e := range ws.details {
			if jetzt.After(e.bis) || !e.voll {
				delete(ws.details, k)
			}
		}
	}
	if len(ws.details) < wunschDetailEintraege {
		ws.details[v.Art+":"+v.TMDbID] = wunschDetailEintrag{bis: jetzt.Add(wunschDetailsGelten), v: v, voll: true}
	}
}

// darf: noch eine Anfrage an TMDb für dieses Profil?
func (ws *wunschSpeicher) darf(konto string) bool {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	if ws.bremse == nil {
		ws.bremse = map[string][]time.Time{}
	}
	jetzt := time.Now()
	alt := ws.bremse[konto]
	frisch := alt[:0]
	for _, t := range alt {
		if jetzt.Sub(t) < wunschBremseFenster {
			frisch = append(frisch, t)
		}
	}
	if len(frisch) >= wunschBremseAnzahl {
		ws.bremse[konto] = frisch
		return false
	}
	ws.bremse[konto] = append(frisch, jetzt)
	// Profile, die lange nichts gesucht haben, nicht ewig mitschleppen.
	if len(ws.bremse) > 500 {
		for k, l := range ws.bremse {
			if len(l) == 0 || jetzt.Sub(l[len(l)-1]) > wunschBremseFenster {
				delete(ws.bremse, k)
			}
		}
	}
	return true
}

func (ws *wunschSpeicher) bildHolen(k string) (wunschBildEintrag, bool) {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	e, da := ws.bilder[k]
	if da {
		e.zuletzt = time.Now()
		ws.bilder[k] = e
	}
	return e, da
}

func (ws *wunschSpeicher) bildMerken(k string, daten []byte, art string) {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	if ws.bilder == nil {
		ws.bilder = map[string]wunschBildEintrag{}
	}
	if alt, da := ws.bilder[k]; da {
		ws.bildSumm -= len(alt.daten)
	}
	ws.bilder[k] = wunschBildEintrag{zuletzt: time.Now(), daten: daten, art: art}
	ws.bildSumm += len(daten)
	// Das am längsten nicht gebrauchte zuerst hinaus.
	for len(ws.bilder) > wunschBildEintraege || ws.bildSumm > wunschBildBytes {
		var aeltester string
		var wann time.Time
		for k, e := range ws.bilder {
			if aeltester == "" || e.zuletzt.Before(wann) {
				aeltester, wann = k, e.zuletzt
			}
		}
		ws.bildSumm -= len(ws.bilder[aeltester].daten)
		delete(ws.bilder, aeltester)
	}
}

// ---------------------------------------------------------------- Antworten

type wunschVorschlag struct {
	Art             string `json:"art"` // movie | series
	Titel           string `json:"titel"`
	Originaltitel   string `json:"originaltitel,omitempty"`
	Jahr            int    `json:"jahr,omitempty"`
	TMDbID          string `json:"tmdbId"`
	IMDbID          string `json:"imdbId,omitempty"`
	Poster          string `json:"poster,omitempty"`
	SchonDa         bool   `json:"schonDa"`
	ItemID          string `json:"itemId,omitempty"`
	SchonGewuenscht bool   `json:"schonGewuenscht"`
}

// wunschPosterAdresse: die Adresse eines Posters bei Lumen.
func wunschPosterAdresse(pfad, groesse string) string {
	datei := strings.TrimPrefix(pfad, "/")
	if !meta.PosterDatei.MatchString(datei) {
		return ""
	}
	return "/api/v1/wishes/poster/" + groesse + "/" + datei
}

// jahrAmEnde: „Dune 2021" oder „Dune (2021)" → „Dune", 2021.
var jahrAmEnde = regexp.MustCompile(`^(.*\S)\s+\(?((?:19|20)\d\d)\)?$`)

func (s *Server) wunschSprache(r *http.Request) string {
	spr, _ := sprache.Metadaten(s.wirksameSprache(r.Context(), accountOf(r).ID))
	if spr == "" {
		spr = s.meta.MetaSprache()
	}
	return spr
}

// wunschVorschlaege: GET /api/v1/wishes/vorschlaege?q=…
func (s *Server) wunschVorschlaege(w http.ResponseWriter, r *http.Request) {
	leer := map[string]any{"vorschlaege": []wunschVorschlag{}}
	suche := strings.Join(strings.Fields(r.URL.Query().Get("q")), " ")
	if len([]rune(suche)) > 100 {
		suche = string([]rune(suche)[:100])
	}
	if s.meta == nil || !s.meta.Ready() {
		ok(w, map[string]any{"vorschlaege": []wunschVorschlag{}, "ohneSchluessel": true})
		return
	}
	if len([]rune(suche)) < 2 {
		ok(w, leer)
		return
	}
	acc := accountOf(r)
	ctx := r.Context()
	scope, err := s.scope(r)
	if err != nil {
		failErr(w, err)
		return
	}
	text, jahr := suche, 0
	if m := jahrAmEnde.FindStringSubmatch(suche); m != nil {
		text = m[1]
		jahr, _ = strconv.Atoi(m[2])
	}
	spr := s.wunschSprache(r)
	schluessel := spr + "|" + strings.ToLower(text)

	liste, da := s.wuensche.sucheHolen(schluessel)
	if !da {
		if !s.wuensche.darf(acc.ID) {
			wunschGebremst(w)
			return
		}
		tctx, fertig := context.WithTimeout(ctx, 6*time.Second)
		liste, err = s.meta.WunschSuche(tctx, text, spr)
		// Nichts in der eigenen Sprache gefunden: auf Englisch.
		if err == nil && len(liste) == 0 && !strings.HasPrefix(spr, "en") {
			liste, err = s.meta.WunschSuche(tctx, text, "en-US")
		}
		fertig()
		if err != nil {
			if errors.Is(err, meta.ErrTMDbSchluessel) {
				ok(w, map[string]any{"vorschlaege": []wunschVorschlag{}, "ohneSchluessel": true})
				return
			}
			// TMDb gerade nicht erreichbar: keine Vorschläge, wünschen
			// geht trotzdem — mit dem Namen.
			s.log.Warn("wunsch-vorschläge", "err", err)
			ok(w, map[string]any{"vorschlaege": []wunschVorschlag{}, "nichtErreichbar": true})
			return
		}
		s.wuensche.sucheMerken(schluessel, liste)
	}
	liste = append([]meta.Vorschlag(nil), liste...)
	if jahr > 0 {
		sort.SliceStable(liste, func(i, j int) bool { return liste[i].Jahr == jahr && liste[j].Jahr != jahr })
	}

	// Mit Altersgrenze: nur, was die Freigabe erlaubt. Dafür braucht es
	// die Einzelheiten — für höchstens zwölf Kandidaten, die meisten aus
	// dem Speicher.
	mitGrenze := scope.MitAltersgrenze || scope.MaxAge > 0
	if mitGrenze {
		if len(liste) > 12 {
			liste = liste[:12]
		}
		voll := s.wunschDetailsFuer(ctx, acc.ID, liste, spr)
		passend := liste[:0]
		for i, v := range liste {
			d := voll[i]
			if d == nil {
				continue // unbekannt, ob es passt: lieber nicht
			}
			if d.AgeYears == nil {
				if !scope.AllowUnrated {
					continue
				}
			} else if *d.AgeYears > scope.MaxAge {
				continue
			}
			v.IMDbID = d.IMDbID
			passend = append(passend, v)
		}
		liste = passend
	} else {
		// IMDb-Kennungen, die schon im Speicher liegen, kommen mit.
		for i := range liste {
			if e, da := s.wuensche.detailHolen(liste[i].Art + ":" + liste[i].TMDbID); da && e.voll {
				liste[i].IMDbID = e.v.IMDbID
			}
		}
	}
	if len(liste) > wunschVorschlaegeMax {
		liste = liste[:wunschVorschlaegeMax]
	}

	kand := make([]store.TitelKandidat, len(liste))
	for i, v := range liste {
		kand[i] = store.TitelKandidat{Art: v.Art, TMDbID: v.TMDbID, IMDbID: v.IMDbID,
			Titel: v.Titel, Originaltitel: v.Originaltitel, Jahr: v.Jahr}
	}
	daIDs, err := s.st.TitelDa(ctx, scope, kand)
	if err != nil {
		failErr(w, err)
		return
	}
	gewuenscht, err := s.st.WunschKennungen(ctx)
	if err != nil {
		failErr(w, err)
		return
	}
	out := make([]wunschVorschlag, 0, len(liste))
	for i, v := range liste {
		out = append(out, wunschVorschlag{
			Art: v.Art, Titel: v.Titel, Originaltitel: wennAnders(v.Originaltitel, v.Titel), Jahr: v.Jahr,
			TMDbID: v.TMDbID, IMDbID: v.IMDbID, Poster: wunschPosterAdresse(v.PosterPfad, "w92"),
			SchonDa: daIDs[i] != "", ItemID: daIDs[i],
			SchonGewuenscht: daIDs[i] == "" && (gewuenscht[v.Art+":"+v.TMDbID] || (v.IMDbID != "" && gewuenscht["imdb:"+v.IMDbID])),
		})
	}
	ok(w, map[string]any{"vorschlaege": out})
}

func wennAnders(a, b string) string {
	if strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b)) {
		return ""
	}
	return a
}

func wunschGebremst(w http.ResponseWriter) {
	writeJSON(w, http.StatusTooManyRequests, errBody{
		Error: satzAnfang(tr(w, "Zu viele Anfragen – bitte einen Moment warten.")), Code: "gebremst"})
}

// wunschDetailsFuer holt die Kurzdetails zu den Vorschlägen — aus dem
// Speicher oder, höchstens vier zugleich, von TMDb. nil, wo es nicht ging.
func (s *Server) wunschDetailsFuer(ctx context.Context, konto string, liste []meta.Vorschlag, spr string) []*meta.Vorschlag {
	out := make([]*meta.Vorschlag, len(liste))
	var fehlt []int
	for i, v := range liste {
		if e, da := s.wuensche.detailHolen(v.Art + ":" + v.TMDbID); da && e.voll {
			d := e.v
			out[i] = &d
		} else {
			fehlt = append(fehlt, i)
		}
	}
	if len(fehlt) == 0 {
		return out
	}
	// Einzelheiten zu einer Suche zählen als eine Anfrage, nicht als zwölf.
	if !s.wuensche.darf(konto) {
		return out
	}
	tctx, fertig := context.WithTimeout(ctx, 8*time.Second)
	defer fertig()
	var wg sync.WaitGroup
	plaetze := make(chan struct{}, 4)
	for _, i := range fehlt {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			plaetze <- struct{}{}
			defer func() { <-plaetze }()
			d, err := s.meta.WunschDetails(tctx, liste[i].Art, liste[i].TMDbID, spr)
			if err != nil || d == nil {
				return
			}
			s.wuensche.detailMerken(*d)
			out[i] = d
		}(i)
	}
	wg.Wait()
	return out
}

// ---------------------------------------------------------------- Wünschen

// wunschAnlegen: POST /api/v1/wishes. Der alte Aufbau {title, year} gilt
// weiter (die Apps schicken ihn); neu und freiwillig sind art, tmdbId,
// imdbId und originaltitel aus einem Vorschlag. Mit tmdbId holt Lumen
// Titel, Jahr, Poster und IMDb-Kennung selbst bei TMDb — was die App
// schickt, gilt nur, wenn TMDb gerade nicht antwortet.
func (s *Server) wunschAnlegen(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Title         string `json:"title"`
		Year          int    `json:"year"`
		Art           string `json:"art"`
		TMDbID        string `json:"tmdbId"`
		IMDbID        string `json:"imdbId"`
		Originaltitel string `json:"originaltitel"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	ctx := r.Context()
	acc := accountOf(r)
	d := store.WunschDaten{Titel: kuerzen(strings.TrimSpace(in.Title), 200), Jahr: in.Year}
	if d.Jahr < 0 || d.Jahr > 3000 {
		d.Jahr = 0
	}
	art := map[string]string{"movie": "movie", "series": "series", "tv": "series"}[in.Art]
	tmdb := strings.TrimSpace(in.TMDbID)
	if !nurZiffern(tmdb, 12) {
		tmdb = ""
	}
	imdb := strings.TrimSpace(in.IMDbID)
	if !meta.IMDbGueltig(imdb) {
		imdb = ""
	}
	if art == "" {
		tmdb = "" // ohne Art ist eine TMDb-Kennung mehrdeutig
	}
	if tmdb != "" || imdb != "" {
		d.Art, d.TMDbID, d.IMDbID = art, tmdb, imdb
		d.Originaltitel = kuerzen(strings.TrimSpace(in.Originaltitel), 200)
		var genau *meta.Vorschlag
		if tmdb != "" {
			if e, da := s.wuensche.detailHolen(art + ":" + tmdb); da && e.voll {
				v := e.v
				genau = &v
			} else if s.meta != nil && s.meta.Ready() {
				tctx, fertig := context.WithTimeout(ctx, 8*time.Second)
				v, err := s.meta.WunschDetails(tctx, art, tmdb, s.wunschSprache(r))
				fertig()
				if err == nil && v != nil {
					s.wuensche.detailMerken(*v)
					genau = v
				}
			}
		}
		scope, err := s.scope(r)
		if err != nil {
			failErr(w, err)
			return
		}
		mitGrenze := scope.MitAltersgrenze || scope.MaxAge > 0
		switch {
		case genau != nil:
			if mitGrenze && (genau.AgeYears == nil && !scope.AllowUnrated ||
				genau.AgeYears != nil && *genau.AgeYears > scope.MaxAge) {
				fail(w, http.StatusForbidden, "Dieser Titel ist für dein Profil nicht freigegeben.")
				return
			}
			d.Titel, d.Jahr, d.Originaltitel = genau.Titel, genau.Jahr, wennAnders(genau.Originaltitel, genau.Titel)
			d.PosterPfad = genau.PosterPfad
			if genau.IMDbID != "" {
				d.IMDbID = genau.IMDbID
			}
		case mitGrenze:
			// Ohne Freigabe lässt sich nichts prüfen: dann eben der Name allein.
			d = store.WunschDaten{Titel: d.Titel, Jahr: d.Jahr}
		default:
			if e, da := s.wuensche.detailHolen(art + ":" + tmdb); da {
				d.PosterPfad = e.v.PosterPfad
			}
		}
	}
	if d.Titel == "" {
		fail(w, http.StatusBadRequest, "titel fehlt")
		return
	}
	if s.wunschSchonDa(w, r, d) {
		return
	}
	wish, err := s.st.WunschAnlegen(ctx, acc.ID, d)
	if err != nil {
		failErr(w, err)
		return
	}
	// Das Poster gleich auf die Platte — dann bleibt es, auch wenn TMDb
	// später nicht antwortet.
	if d.PosterPfad != "" {
		pfad := d.PosterPfad
		go func() {
			bctx, fertig := context.WithTimeout(s.hintergrund(), 20*time.Second)
			defer fertig()
			_, _, _ = s.wunschPosterLaden(bctx, "w185", strings.TrimPrefix(pfad, "/"))
		}()
	}
	liste := []store.Wish{*wish}
	s.wuenscheAufbereiten(r, liste)
	ok(w, liste[0])
}

func nurZiffern(s string, max int) bool {
	if s == "" || len(s) > max {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// wuenscheAufbereiten setzt die Posteradresse und lässt „Ansehen" nur
// dort, wo das Profil den Titel auch sehen darf.
func (s *Server) wuenscheAufbereiten(r *http.Request, liste []store.Wish) {
	var ids []string
	for i := range liste {
		liste[i].Poster = wunschPosterAdresse(liste[i].PosterPfad, "w185")
		if liste[i].ItemID != "" {
			ids = append(ids, liste[i].ItemID)
		}
	}
	if len(ids) == 0 {
		return
	}
	sichtbar := map[string]bool{}
	if scope, err := s.scope(r); err == nil {
		scope.IDs, scope.Limit = ids, len(ids)
		if items, err := s.st.Query(r.Context(), scope); err == nil {
			for _, it := range items {
				sichtbar[it.ID] = true
			}
		}
	}
	for i := range liste {
		if !sichtbar[liste[i].ItemID] {
			liste[i].ItemID = ""
			// Angeschlossen an einen Wunsch, der mit einem Titel erfüllt
			// ist, den dieses Profil nicht sehen darf: „erfüllt" verriete,
			// dass es ihn gibt. Für dieses Profil bleibt er offen.
			if liste[i].Dabei && liste[i].State == "done" {
				liste[i].State, liste[i].Note = "open", ""
			}
		}
	}
}

// wunschSicht: der erste der Titel, die dieses Konto sehen darf — für
// store.WuenscheErfuellenFuer. Dieselben Regeln wie scope.
func (s *Server) wunschSicht(ctx context.Context, kontoID string, itemIDs []string) string {
	acc, err := s.st.Account(ctx, kontoID)
	if err != nil {
		return ""
	}
	q, err := s.sichtVon(ctx, acc)
	if err != nil {
		return ""
	}
	q.IDs, q.Limit = itemIDs, len(itemIDs)
	items, err := s.st.Query(ctx, q)
	if err != nil || len(items) == 0 {
		return ""
	}
	da := map[string]bool{}
	for _, it := range items {
		da[it.ID] = true
	}
	for _, id := range itemIDs {
		if da[id] {
			return id
		}
	}
	return ""
}

// ---------------------------------------------------------------- Poster

func (s *Server) wunschBildOrdner() string {
	if s.dataDir == "" {
		return ""
	}
	return filepath.Join(s.dataDir, "wunschbilder")
}

// wunschPosterLaden: aus dem Speicher, von der Platte oder von TMDb.
// Poster, die zu einem Wunsch gehören, bleiben auf der Platte.
func (s *Server) wunschPosterLaden(ctx context.Context, groesse, datei string) ([]byte, string, error) {
	k := groesse + "-" + datei
	if e, da := s.wuensche.bildHolen(k); da {
		return e.daten, e.art, nil
	}
	ordner := s.wunschBildOrdner()
	if ordner != "" {
		if daten, err := os.ReadFile(filepath.Join(ordner, k)); err == nil {
			art := "image/jpeg"
			if strings.HasSuffix(datei, ".png") {
				art = "image/png"
			}
			s.wuensche.bildMerken(k, daten, art)
			return daten, art, nil
		}
	}
	if s.meta == nil {
		return nil, "", meta.ErrOhneTMDb
	}
	daten, art, err := s.meta.WunschPoster(ctx, groesse, datei)
	if err != nil {
		return nil, "", err
	}
	s.wuensche.bildMerken(k, daten, art)
	if ordner != "" && s.st.WunschPosterBekannt(ctx, "/"+datei) {
		if err := os.MkdirAll(ordner, 0o755); err == nil {
			ziel := filepath.Join(ordner, k)
			_ = ablage.Einmal(ziel, func() error { return ablage.Schreiben(ziel, bytes.NewReader(daten), 0) })
		}
	}
	return daten, art, nil
}

// wunschPoster: GET /api/v1/wishes/poster/{groesse}/{datei}
func (s *Server) wunschPoster(w http.ResponseWriter, r *http.Request) {
	groesse, datei := r.PathValue("groesse"), r.PathValue("datei")
	if (groesse != "w92" && groesse != "w185") || !meta.PosterDatei.MatchString(datei) {
		fail(w, http.StatusNotFound, "kein bild")
		return
	}
	tctx, fertig := context.WithTimeout(r.Context(), 10*time.Second)
	defer fertig()
	daten, art, err := s.wunschPosterLaden(tctx, groesse, datei)
	if err != nil {
		fail(w, http.StatusNotFound, "kein bild")
		return
	}
	w.Header().Set("Content-Type", art)
	w.Header().Set("Cache-Control", "private, max-age=604800")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(daten)
}
