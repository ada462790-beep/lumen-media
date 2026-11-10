// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

// Musik und Fotos: was beide Bereiche teilen — die Routen, wer welche
// Bibliotheken sieht, der Zwischenspeicher für Bilder und umgewandelte
// Dateien und das Ausliefern mit Range.

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"lumen/internal/store"
)

// mediathekRouten hängt die Wege für Musik und Fotos ein. Handler() ruft
// das genau einmal auf.
func (s *Server) mediathekRouten(mux *http.ServeMux) {
	auth, admin := s.requireAuth, s.requireAdmin

	mux.Handle("GET /api/v1/mediathek", auth(s.mediathekBereiche))

	// Musik
	mux.Handle("GET /api/v1/musik/start", auth(s.musikStart))
	mux.Handle("GET /api/v1/musik/alben", auth(s.musikAlben))
	mux.Handle("GET /api/v1/musik/alben/{id}", auth(s.musikAlbum))
	mux.Handle("GET /api/v1/musik/interpreten", auth(s.musikInterpreten))
	mux.Handle("GET /api/v1/musik/interpreten/{id}", auth(s.musikInterpret))
	mux.Handle("GET /api/v1/musik/titel", auth(s.musikTitel))
	mux.Handle("GET /api/v1/musik/genres", auth(s.musikGenres))
	mux.Handle("GET /api/v1/musik/suche", auth(s.musikSuche))
	mux.Handle("POST /api/v1/musik/titel/{id}/gehoert", auth(s.musikGehoert))
	mux.Handle("DELETE /api/v1/musik/hoerverlauf", auth(s.musikHoerverlaufLoeschen))
	mux.Handle("GET /api/v1/musik/favoriten", auth(s.musikFavoriten))
	mux.Handle("PUT /api/v1/musik/favoriten/{art}/{id}", auth(s.musikFavoritAn))
	mux.Handle("DELETE /api/v1/musik/favoriten/{art}/{id}", auth(s.musikFavoritAus))
	mux.Handle("GET /api/v1/musik/listen", auth(s.musikListen))
	mux.Handle("POST /api/v1/musik/listen", auth(s.musikListeAnlegen))
	mux.Handle("GET /api/v1/musik/listen/{id}", auth(s.musikListe))
	mux.Handle("POST /api/v1/musik/listen/{id}", auth(s.musikListeAendern))
	mux.Handle("DELETE /api/v1/musik/listen/{id}", auth(s.musikListeLoeschen))
	mux.Handle("POST /api/v1/musik/listen/{id}/titel", auth(s.musikListeHinzufuegen))
	mux.Handle("DELETE /api/v1/musik/listen/{id}/titel/{titel}", auth(s.musikListeEntfernen))
	mux.Handle("POST /api/v1/musik/listen/{id}/reihenfolge", auth(s.musikListeOrdnen))

	// Fotos
	mux.Handle("GET /api/v1/fotos/uebersicht", auth(s.fotoUebersicht))
	mux.Handle("GET /api/v1/fotos/liste", auth(s.fotoListe))
	mux.Handle("GET /api/v1/fotos/alben", auth(s.fotoAlben))
	mux.Handle("GET /api/v1/fotos/alben/{id}", auth(s.fotoAlbum))
	mux.Handle("GET /api/v1/fotos/{id}", auth(s.fotoEinzeln))

	// Medien: Ton, Cover, Vorschaubilder. Auch mit dem Medienschlüssel in
	// der Adresse (medienWeg), weil <audio> und <img> keinen Kopf schicken.
	mux.Handle("GET /api/v1/medien/musik/titel/{id}", auth(s.musikDatei))
	mux.Handle("GET /api/v1/medien/musik/cover/{id}", auth(s.musikCover))
	mux.Handle("GET /api/v1/medien/fotos/{id}/bild", auth(s.fotoBild))
	mux.Handle("GET /api/v1/medien/fotos/{id}/video", auth(s.fotoVideo))
	mux.Handle("GET /api/v1/medien/fotos/{id}/original", auth(s.fotoOriginal))

	// Verwaltung
	mux.Handle("GET /api/v1/admin/mediathek", admin(s.adminMediathek))
	// Ändern nur der Besitzer: Ob Fotos ihren Aufnahmeort zeigen, ist
	// eine Entscheidung über die Privatsphäre aller — kein Mitverwalter
	// soll sie still umstellen. Lesen darf jede Verwaltung.
	mux.Handle("PUT /api/v1/admin/mediathek", s.requireOwner(s.adminMediathekSetzen))

	// Nach einem Durchlauf über Musik oder Fotos die Bilder vorrechnen —
	// im Hintergrund, mit niedriger Priorität.
	if s.scanner != nil {
		s.scanner.NachMediathek = func(lib store.Library) { go s.mediathekVorrechnen(context.Background(), lib) }
	}
	go s.fotoVorschauenBeimStart()
}

// ---------------------------------------------------------------- Sicht

// sichtbareBibliotheken: die Bibliotheken einer Art, die die Person sehen
// darf. Die Freigaben gelten pro Bibliothek — für Musik und Fotos genau so
// wie für Filme.
func (s *Server) sichtbareBibliotheken(r *http.Request, art string) ([]string, error) {
	acc := accountOf(r)
	erlaubt, err := s.st.GrantedLibraries(r.Context(), acc.ID)
	if err != nil {
		return nil, err
	}
	alle, err := s.st.BibliothekenNachArt(r.Context(), art)
	if err != nil {
		return nil, err
	}
	darf := map[string]bool{}
	for _, id := range erlaubt {
		darf[id] = true
	}
	out := []string{}
	for _, id := range alle {
		if darf[id] {
			out = append(out, id)
		}
	}
	return out, nil
}

func (s *Server) musikSicht(r *http.Request) (store.MusikSicht, error) {
	libs, err := s.sichtbareBibliotheken(r, "music")
	if err != nil {
		return store.MusikSicht{}, err
	}
	acc := accountOf(r)
	v := store.MusikSicht{Bibliotheken: libs}
	// Die Altersgrenze des Profils gilt auch hier: Titel mit dem Vermerk
	// „explizit" sieht nicht, wer ein Kinderprofil hat oder unter 16
	// eingestellt ist.
	if !acc.Besitzerrechte() {
		lim, err := s.st.Limits(r.Context(), acc.ID)
		if err != nil {
			return v, err
		}
		v.OhneExplizit = acc.IsKid || lim.MaxAge < 16
	}
	return v, nil
}

func (s *Server) fotoSicht(r *http.Request) (store.FotoSicht, error) {
	libs, err := s.sichtbareBibliotheken(r, "photo")
	return store.FotoSicht{Bibliotheken: libs}, err
}

// mediathekBereiche: GET /mediathek — ob es für diese Person Musik und
// Fotos gibt. Danach richten die Apps ihre Navigation: kein Reiter „Musik"
// ohne Musik.
func (s *Server) mediathekBereiche(w http.ResponseWriter, r *http.Request) {
	musik, err := s.sichtbareBibliotheken(r, "music")
	if err != nil {
		failErr(w, err)
		return
	}
	fotos, err := s.sichtbareBibliotheken(r, "photo")
	if err != nil {
		failErr(w, err)
		return
	}
	ok(w, map[string]any{
		"musik": len(musik) > 0, "fotos": len(fotos) > 0,
		"fotoOrt": s.fotoOrtZeigen(r.Context()),
	})
}

// ---------------------------------------------------------------- Verwaltung

const einstellungFotoOrt = "fotos.ort_zeigen"

func (s *Server) fotoOrtZeigen(ctx context.Context) bool {
	return s.st.Setting(ctx, einstellungFotoOrt, "0") == "1"
}

func (s *Server) adminMediathek(w http.ResponseWriter, r *http.Request) {
	ok(w, map[string]any{"fotoOrtZeigen": s.fotoOrtZeigen(r.Context()),
		// Ändern darf nur der Besitzer; die Oberfläche zeigt es sonst nur.
		"darfAendern": accountOf(r).Besitzerrechte()})
}

func (s *Server) adminMediathekSetzen(w http.ResponseWriter, r *http.Request) {
	var in struct {
		FotoOrtZeigen *bool `json:"fotoOrtZeigen"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if in.FotoOrtZeigen != nil {
		v := "0"
		if *in.FotoOrtZeigen {
			v = "1"
		}
		if err := s.st.SetSetting(r.Context(), einstellungFotoOrt, v); err != nil {
			failErr(w, err)
			return
		}
	}
	s.adminMediathek(w, r)
}

// ---------------------------------------------------------------- Zwischenspeicher

func (s *Server) mediathekOrdner(teil string) string {
	return filepath.Join(s.dataDir, "mediathek", teil)
}

// kurz: ein kurzer, fester Name für einen beliebig langen Schlüssel.
func kurz(s string) string {
	h := sha1.Sum([]byte(s))
	return hex.EncodeToString(h[:6])
}

// einmalig sorgt dafür, dass dieselbe Datei nicht zweimal gleichzeitig
// gerechnet wird: Wer als Zweiter kommt, wartet auf den Ersten.
type einmalig struct {
	mu     sync.Mutex
	laeuft map[string]chan struct{}
}

func (e *einmalig) tun(schluessel string, fn func() error) error {
	e.mu.Lock()
	if e.laeuft == nil {
		e.laeuft = map[string]chan struct{}{}
	}
	if ch, ok := e.laeuft[schluessel]; ok {
		e.mu.Unlock()
		<-ch
		return nil
	}
	ch := make(chan struct{})
	e.laeuft[schluessel] = ch
	e.mu.Unlock()
	defer func() {
		e.mu.Lock()
		delete(e.laeuft, schluessel)
		e.mu.Unlock()
		close(ch)
	}()
	return fn()
}

var (
	mediathekEinmal einmalig
	// Was jemand gerade angeklickt hat, wird sofort gerechnet — aber nie
	// mehr als zwei Dinge gleichzeitig, damit ein schnell durchgeblättertes
	// Album den Server nicht in die Knie zwingt.
	mediathekPlaetze = make(chan struct{}, 2)
)

// zwischenspeichern liefert die Datei unter ziel — und rechnet sie vorher,
// wenn sie fehlt. Neue Dateien bekommen ein festes, altes Datum
// (zeitlos): Am Zwischenspeicher soll niemand ablesen können, wann jemand
// ein Lied gehört oder ein Foto angesehen hat.
func zwischenspeichern(ctx context.Context, ziel string, rechnen func() error) error {
	if st, err := os.Stat(ziel); err == nil && st.Size() > 0 {
		return nil
	}
	return mediathekEinmal.tun(ziel, func() error {
		if st, err := os.Stat(ziel); err == nil && st.Size() > 0 {
			return nil
		}
		select {
		case mediathekPlaetze <- struct{}{}:
			defer func() { <-mediathekPlaetze }()
		case <-ctx.Done():
			return ctx.Err()
		}
		if err := os.MkdirAll(filepath.Dir(ziel), 0o755); err != nil {
			return err
		}
		if err := rechnen(); err != nil {
			return err
		}
		zeitlos(ziel)
		return nil
	})
}

// alteFassungenWeg räumt frühere Fassungen derselben Datei weg (andere
// Änderungszeit der Quelle im Namen).
func alteFassungenWeg(ordner, vorsilbe, behalten string) {
	treffer, _ := filepath.Glob(filepath.Join(ordner, vorsilbe+"*"))
	for _, t := range treffer {
		if filepath.Base(t) != behalten {
			_ = os.Remove(t)
		}
	}
}

// ordnerBegrenzen hält einen Zwischenspeicher unter grenze Bytes. Weil
// alle Dateien dasselbe Datum tragen, fliegen die grössten zuerst — das
// gibt am schnellsten Platz und sagt nichts darüber, was zuletzt lief.
func ordnerBegrenzen(ordner string, grenze int64) {
	eintraege, err := os.ReadDir(ordner)
	if err != nil {
		return
	}
	type datei struct {
		pfad  string
		bytes int64
	}
	var alle []datei
	var summe int64
	for _, e := range eintraege {
		if !e.Type().IsRegular() || strings.HasSuffix(e.Name(), ".tmp") {
			continue
		}
		if info, err := e.Info(); err == nil {
			alle = append(alle, datei{filepath.Join(ordner, e.Name()), info.Size()})
			summe += info.Size()
		}
	}
	if summe <= grenze {
		return
	}
	sort.Slice(alle, func(i, j int) bool { return alle[i].bytes > alle[j].bytes })
	for _, d := range alle {
		if summe <= grenze {
			break
		}
		if os.Remove(d.pfad) == nil {
			summe -= d.bytes
		}
	}
}

// medienSenden ist dateiSenden für Dateien aus einer Mediathek: nie aus dem
// Datenordner (medienOeffnen). Eigene Zwischendateien (Vorschaubilder,
// Umgewandeltes) liegen genau dort und gehen über dateiSenden.
func medienSenden(w http.ResponseWriter, r *http.Request, pfad, typ, downloadName string) {
	if fh, err := medienOeffnen(pfad); err != nil {
		fail(w, http.StatusGone, "die datei ist nicht mehr da")
		return
	} else {
		fh.Close()
	}
	dateiSenden(w, r, pfad, typ, downloadName)
}

// dateiSenden liefert eine Datei mit Range — Springen im Lied, Wischen im
// Video. Der Name kommt nur bei Downloads mit; sonst verrät die Antwort
// nicht, wie die Datei auf der Platte heisst.
func dateiSenden(w http.ResponseWriter, r *http.Request, pfad, typ, downloadName string) {
	fh, err := os.Open(pfad)
	if err != nil {
		fail(w, http.StatusGone, "die datei ist nicht mehr da")
		return
	}
	defer fh.Close()
	info, err := fh.Stat()
	if err != nil {
		fail(w, http.StatusInternalServerError, "datei nicht lesbar")
		return
	}
	if typ != "" {
		w.Header().Set("Content-Type", typ)
	}
	if downloadName != "" {
		w.Header().Set("Content-Disposition", `attachment; filename*=UTF-8''`+urlName(downloadName))
	}
	// Privat zwischenspeichern: Bilder und Lieder ändern sich unter
	// derselben Adresse nicht (neue Fassung, neuer Schlüssel in der
	// Adresse), aber sie gehören nur diesem Browser.
	w.Header().Set("Cache-Control", "private, max-age=86400")
	http.ServeContent(w, r, "", info.ModTime(), fh)
}

func urlName(s string) string {
	var b strings.Builder
	for _, c := range []byte(s) {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '.' || c == '-' || c == '_' {
			b.WriteByte(c)
		} else {
			b.WriteString("%" + strings.ToUpper(hex.EncodeToString([]byte{c})))
		}
	}
	return b.String()
}
