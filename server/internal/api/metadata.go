// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"lumen/internal/meta"
)

// Die Metadaten-Quellen richtet der Besitzer einmal ein — dann gelten sie
// für alle. Niemand im Haushalt braucht ein eigenes Konto bei TMDb oder
// IMDb, und niemand muss seine Schlüssel herausgeben.

const (
	keyTMDb     = "meta.tmdb_key"
	keyOMDb     = "meta.omdb_key"
	keyLanguage = "meta.language"
	keyRegion   = "meta.region"
	keyUseIMDb  = "meta.use_imdb"
)

func (s *Server) adminMetaSources(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	titel, mitPoster := s.st.PosterStand(ctx)
	// Die Schlüssel selbst gehen nie wieder heraus — nur, ob einer da ist.
	ok(w, map[string]any{
		"tmdb": map[string]any{
			"configured": s.st.Setting(ctx, keyTMDb, "") != "",
			"name":       "TMDb",
			"what":       tr(w, "Filme und Serien: Poster, Beschreibungen, Altersfreigabe"),
		},
		"imdb": map[string]any{
			"configured": s.st.Setting(ctx, keyOMDb, "") != "",
			"enabled":    s.st.Setting(ctx, keyUseIMDb, "1") == "1",
			"name":       "IMDb-Wertung",
			"what":       tr(w, "Die öffentliche Note am Titel (über OMDb)"),
		},
		// NFO-Dateien neben den Filmen (Kodi, Emby, tinyMediaManager):
		// gelten ihre Werte vor denen von TMDb?
		"nfoBevorzugen": meta.NFOBevorzugt(ctx, s.st),
		"language":      s.st.Setting(ctx, keyLanguage, "de-DE"),
		"region":        s.st.Setting(ctx, keyRegion, "CH"),
		"ready":         s.meta != nil && s.meta.Ready(),
		// Wie es um den Schlüssel steht (fehlt, abgelehnt, ok), wie viele
		// Titel trotzdem ein Poster haben (aus der Sammlung) und wie viele
		// Kennungen auf einen Schlüssel warten — damit die Verwaltung den
		// echten Stand sagt statt „Poster fehlen".
		"schluessel": meta.SchluesselStand(ctx, s.st),
		"titel":      titel,
		"mitPoster":  mitPoster,
		"vorgemerkt": meta.VorgemerktZahl(ctx, s.st),
		"note":       tr(w, "Gilt für alle Profile. Niemand braucht dafür ein eigenes Konto."),
	})
}

func (s *Server) adminSetMetaSources(w http.ResponseWriter, r *http.Request) {
	var in struct {
		TMDbKey  *string `json:"tmdbKey"`
		OMDbKey  *string `json:"omdbKey"`
		Language *string `json:"language"`
		Region   *string `json:"region"`
		UseIMDb  *bool   `json:"useImdb"`
		// NFOBevorzugen: „Lokale Metadaten (NFO) bevorzugen".
		NFOBevorzugen *bool `json:"nfoBevorzugen"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	ctx := r.Context()
	// Ein neuer TMDb-Schlüssel wird einmal ausprobiert, bevor er gilt.
	// Sonst merkte man einen falsch kopierten Schlüssel (die kurze
	// API-Kennung statt des langen Tokens) erst daran, dass nach dem
	// Durchlauf alle Poster fehlen.
	if in.TMDbKey != nil && strings.TrimSpace(*in.TMDbKey) != "" {
		cfg := s.metaConfig(r)
		cfg.TMDbKey = strings.TrimSpace(*in.TMDbKey)
		if probe, err := meta.NewResolver(s.st, s.dataDir, cfg); err == nil {
			pctx, abbrechen := context.WithTimeout(ctx, 8*time.Second)
			err := probe.TMDbPruefen(pctx)
			abbrechen()
			if errors.Is(err, meta.ErrTMDbSchluessel) {
				fail(w, http.StatusBadRequest, "TMDb nimmt diesen Schlüssel nicht an. Es braucht den langen API Read Access Token (er fängt mit eyJ an), nicht die kurze API-Kennung.")
				return
			}
		}
	}
	set := func(key string, v *string) {
		if v != nil {
			_ = s.st.SetSetting(ctx, key, strings.TrimSpace(*v))
		}
	}
	set(keyTMDb, in.TMDbKey)
	if in.TMDbKey != nil {
		// Ein neuer Schlüssel: Was TMDb über den alten gesagt hat, gilt
		// nicht mehr.
		meta.SchluesselAngenommen(ctx, s.st)
	}
	set(keyOMDb, in.OMDbKey)
	set(keyLanguage, in.Language)
	set(keyRegion, in.Region)
	if in.UseIMDb != nil {
		v := "0"
		if *in.UseIMDb {
			v = "1"
		}
		_ = s.st.SetSetting(ctx, keyUseIMDb, v)
	}

	if in.NFOBevorzugen != nil {
		v := "0"
		if *in.NFOBevorzugen {
			v = "1"
		}
		_ = s.st.SetSetting(ctx, meta.EinstellungNFOBevorzugen, v)
	}

	// Neu aufbauen, damit die Änderung sofort gilt.
	res, err := meta.NewResolver(s.st, s.dataDir, s.metaConfig(r))
	if err != nil {
		failErr(w, err)
		return
	}
	s.SetResolver(res)
	s.st.Log(ctx, "info", "metadaten", "Quellen geändert")
	s.adminMetaSources(w, r)
}

func (s *Server) metaConfig(r *http.Request) meta.Config {
	ctx := r.Context()
	return meta.Config{
		TMDbKey:  s.st.Setting(ctx, keyTMDb, ""),
		OMDbKey:  s.st.Setting(ctx, keyOMDb, ""),
		Language: s.st.Setting(ctx, keyLanguage, "de-DE"),
		Region:   s.st.Setting(ctx, keyRegion, "CH"),
		UseIMDb:  s.st.Setting(ctx, keyUseIMDb, "1") == "1",

		TMDbURL:    os.Getenv("LUMEN_TMDB_URL"),
		TMDbImgURL: os.Getenv("LUMEN_TMDB_IMG_URL"),
		OMDbURL:    os.Getenv("LUMEN_OMDB_URL"),
	}
}

func (s *Server) adminResolve(w http.ResponseWriter, r *http.Request) {
	if s.meta == nil || !s.meta.Ready() {
		s.ohneSchluessel(w, r)
		return
	}
	lib := r.URL.Query().Get("library")
	if !s.mediathekPruefen(w, r, lib) {
		return
	}
	limit := qInt(r, "limit", 200)

	// Das kann dauern; die Anfrage wartet nicht darauf. Läuft schon ein
	// Durchgang, startet kein zweiter daneben.
	if !metaLaeuft.CompareAndSwap(false, true) {
		ok(w, map[string]string{"status": "läuft schon"}) // sprache: fest – eine Kennung
		return
	}
	go func() {
		defer metaLaeuft.Store(false)
		s.metadatenNachholen(s.hintergrund(), lib, limit)
	}()
	ok(w, map[string]string{"status": "läuft"})
}

// metadatenNachholen holt, was noch wartet — bis nichts mehr zu tun ist
// oder grenze Titel erledigt sind (0 = keine Grenze). Unter der
// Arbeitssperre, in Häppchen zu 50: nie neben einem Durchlauf her (sonst
// doppelte Anfragen bei TMDb und zwei Downloads ins selbe Bild), aber ein
// „Prüfen" kommt dazwischen dran.
func (s *Server) metadatenNachholen(ctx context.Context, lib string, grenze int) {
	for rest := grenze; ctx.Err() == nil && (grenze <= 0 || rest > 0); {
		n := 50
		if grenze > 0 {
			n = min(rest, 50)
		}
		var res *meta.Result
		var err error
		s.Nacheinander(ctx, func(ctx context.Context) {
			res, err = s.meta.ResolvePending(ctx, lib, n)
		})
		if err != nil {
			s.log.Error("metadaten", "err", err)
			break
		}
		// Nichts hat seinen Zustand geändert (nur Fehler, etwa kein
		// Internet): aufhören statt im Kreis zu fragen.
		if res == nil || res.Matched+res.Unmatched == 0 {
			break
		}
		rest -= res.Matched + res.Unmatched
	}
	// Mit den Metadaten kommen die TMDb-Reihen; die selbst erkannten
	// rücken danach zurecht.
	s.Nacheinander(ctx, s.ReihenBilden)
	// Mit den Kennungen erfüllen sich Wünsche (store/wunsch_kennung.go).
	if n, err := s.st.WuenscheErfuellen(ctx); err == nil && n > 0 {
		s.st.Log(ctx, "info", "wuensche", fmt.Sprintf("%d Wunsch/Wünsche von selbst erfüllt", n))
	}
}

// metadatenImHintergrund: wie metadatenNachholen ohne Grenze, aber nur
// einmal gleichzeitig.
func (s *Server) metadatenImHintergrund(lib string) {
	if !metaLaeuft.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer metaLaeuft.Store(false)
		s.metadatenNachholen(s.hintergrund(), lib, 0)
	}()
}

func (s *Server) adminUnmatched(w http.ResponseWriter, r *http.Request) {
	items, err := s.st.UnmatchedItems(r.Context(), qInt(r, "limit", 100))
	if err != nil {
		failErr(w, err)
		return
	}
	if darf := s.verwalteteMediatheken(r); darf != nil {
		sichtbar := items[:0]
		for _, it := range items {
			if darf[it.LibraryID] {
				sichtbar = append(sichtbar, it)
			}
		}
		items = sichtbar
	}
	ok(w, items)
}

// artwork liefert Poster und Hintergründe aus dem lokalen Zwischenspeicher.
// Dass sie hier liegen heisst: die Mediathek zeigt weiter Bilder, auch wenn
// der Server gerade kein Internet hat.
func (s *Server) artwork(w http.ResponseWriter, r *http.Request) {
	// Bilder einer Filmreihe — nur für den, der die Reihe sieht.
	if strings.HasPrefix(r.PathValue("item"), "col_") {
		s.reihenBild(w, r)
		return
	}
	// Bild einer eigenen Sammlung (sammlungen.go) — hier, damit es wie
	// jedes Bild mit dem Medienschlüssel in der Adresse geht.
	if strings.HasPrefix(r.PathValue("item"), "sam_") {
		s.sammlungBildZeigen(w, r)
		return
	}
	it, err := s.visibleItem(r, r.PathValue("item"))
	if err != nil {
		failErr(w, err)
		return
	}
	// Ein Bild aus dem Film selbst, wo es kein Poster gibt.
	if r.PathValue("kind") == "vorschau" {
		s.vorschauSenden(w, r, it)
		return
	}
	if r.PathValue("kind") == "frame" {
		s.standbildSenden(w, r, it)
		return
	}
	var name string
	if kind := r.PathValue("kind"); strings.HasPrefix(kind, "cast") {
		// Bild aus der Besetzung: cast03 ist die vierte Person.
		if leute, err := s.st.Besetzung(r.Context(), it.ID); err == nil {
			if i, err := strconv.Atoi(strings.TrimPrefix(kind, "cast")); err == nil && i >= 0 && i < len(leute) {
				name = leute[i].Bild
			}
		}
	} else {
		art, err := s.st.ArtworkFor(r.Context(), []string{it.ID})
		if err != nil {
			failErr(w, err)
			return
		}
		name = art[it.ID][kind]
	}
	if name == "" || s.meta == nil {
		fail(w, http.StatusNotFound, "kein bild")
		return
	}
	// Der Name kommt aus der Datenbank, aber geprüft wird trotzdem: eine
	// Kennung aus einer Anfrage darf den Bilderordner nie verlassen.
	if strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
		fail(w, http.StatusNotFound, "kein bild")
		return
	}
	path := filepath.Join(s.meta.ArtDir(), name)
	f, err := os.Open(path)
	if err != nil {
		fail(w, http.StatusNotFound, "kein bild")
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		fail(w, http.StatusNotFound, "kein bild")
		return
	}
	// Kurz zwischenspeichern, danach fragt der Browser nach (304, wenn gleich):
	// Beim Neuzuordnen bleibt die Adresse dieselbe, mit einem Tag sah man das
	// falsche Bild nach der Korrektur noch bis morgen.
	w.Header().Set("Cache-Control", "private, max-age=600")
	w.Header().Set("Content-Type", fmt.Sprintf("image/%s",
		strings.TrimPrefix(strings.ToLower(filepath.Ext(name)), ".")))
	http.ServeContent(w, r, name, info.ModTime(), f)
}

func (s *Server) reihenBild(w http.ResponseWriter, r *http.Request) {
	x, err := s.st.Reihe(r.Context(), r.PathValue("item"))
	if err != nil || s.meta == nil {
		fail(w, http.StatusNotFound, "kein bild")
		return
	}
	// Nur wer die Reihe sehen darf, bekommt ihr Bild — dieselbe Regel wie
	// für die Reihe selbst.
	if !accountOf(r).IsAdmin {
		items, err := s.sichtbar(r, x.ItemIDs)
		if x.Hidden || err != nil || len(items) < reiheMindestens {
			fail(w, http.StatusNotFound, "kein bild")
			return
		}
	}
	name := x.PosterDatei
	if r.PathValue("kind") == "backdrop" {
		name = x.BackdropDatei
	}
	if name == "" || strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
		fail(w, http.StatusNotFound, "kein bild")
		return
	}
	f, err := os.Open(filepath.Join(s.meta.ArtDir(), name))
	if err != nil {
		fail(w, http.StatusNotFound, "kein bild")
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		fail(w, http.StatusNotFound, "kein bild")
		return
	}
	// Kurz zwischenspeichern, danach fragt der Browser nach (304, wenn gleich):
	// Beim Neuzuordnen bleibt die Adresse dieselbe, mit einem Tag sah man das
	// falsche Bild nach der Korrektur noch bis morgen.
	w.Header().Set("Cache-Control", "private, max-age=600")
	w.Header().Set("Content-Type", fmt.Sprintf("image/%s",
		strings.TrimPrefix(strings.ToLower(filepath.Ext(name)), ".")))
	http.ServeContent(w, r, name, info.ModTime(), f)
}

var metaLaeuft atomic.Bool
