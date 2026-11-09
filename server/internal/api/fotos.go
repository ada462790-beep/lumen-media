// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

// Fotos: Mediathek nach Monaten, Alben (Ordner), einzelne Fotos samt
// Vorschaubildern, Videos und — wo erlaubt — der Originaldatei.
//
// Der Aufnahmeort steht in vielen Fotos. Er geht nur hinaus, wenn die
// Verwaltung es ausdrücklich eingeschaltet hat; voreingestellt bleibt er
// auf dem Server. Die Vorschaubilder tragen gar keine Metadaten.

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"lumen/internal/store"
)

type fotoAntwort struct {
	store.Foto
	Klein    string `json:"klein"`
	Gross    string `json:"gross"`
	Video    string `json:"video,omitempty"`
	Original string `json:"original,omitempty"`
}

// fotoGroessen: klein fürs Raster, gross für den Betrachter.
var fotoGroessen = map[string]int{"klein": 400, "gross": 2048}

func (s *Server) fotoAntworten(r *http.Request, fotos []store.Foto, mitOrt, download bool) []fotoAntwort {
	out := make([]fotoAntwort, len(fotos))
	for i, f := range fotos {
		a := fotoAntwort{Foto: f,
			Klein: "/api/v1/medien/fotos/" + f.ID + "/bild?g=klein",
			Gross: "/api/v1/medien/fotos/" + f.ID + "/bild?g=gross"}
		if !mitOrt {
			a.Breitengrad, a.Laengengrad = nil, nil
		}
		if f.Art == "video" {
			a.Video = "/api/v1/medien/fotos/" + f.ID + "/video"
		}
		if download {
			a.Original = "/api/v1/medien/fotos/" + f.ID + "/original"
		}
		out[i] = a
	}
	return out
}

func (s *Server) fotoRechte(r *http.Request) (mitOrt, download bool) {
	lim, _ := s.st.Limits(r.Context(), accountOf(r).ID)
	return s.fotoOrtZeigen(r.Context()), lim.AllowDownload
}

// fotoUebersicht: GET /fotos/uebersicht — Jahre und Monate mit Anzahl und
// Titelbildern, neueste zuerst.
func (s *Server) fotoUebersicht(w http.ResponseWriter, r *http.Request) {
	v, err := s.fotoSicht(r)
	if err != nil {
		failErr(w, err)
		return
	}
	monate, err := s.st.FotoUebersicht(r.Context(), v)
	if err != nil {
		failErr(w, err)
		return
	}
	type jahr struct {
		Jahr      int    `json:"jahr"`
		Anzahl    int    `json:"anzahl"`
		Titelbild string `json:"titelbild,omitempty"`
	}
	jahre := []jahr{}
	gesamt := 0
	for _, m := range monate {
		gesamt += m.Anzahl
		if len(jahre) == 0 || jahre[len(jahre)-1].Jahr != m.Jahr {
			jahre = append(jahre, jahr{Jahr: m.Jahr})
		}
		j := &jahre[len(jahre)-1]
		j.Anzahl += m.Anzahl
		if j.Titelbild == "" && len(m.Titelbilder) > 0 {
			j.Titelbild = m.Titelbilder[0]
		}
	}
	ok(w, map[string]any{"jahre": jahre, "monate": monate, "gesamt": gesamt})
}

// fotoListe: GET /fotos/liste?jahr=&monat=&album=&offset=&limit=
func (s *Server) fotoListe(w http.ResponseWriter, r *http.Request) {
	v, err := s.fotoSicht(r)
	if err != nil {
		failErr(w, err)
		return
	}
	q := store.FotoAbfrage{Jahr: qInt(r, "jahr", 0), Monat: qInt(r, "monat", 0),
		AlbumID: r.URL.Query().Get("album"), Offset: qInt(r, "offset", 0), Limit: qInt(r, "limit", 300)}
	fotos, gesamt, err := s.st.FotoListe(r.Context(), v, q)
	if err != nil {
		failErr(w, err)
		return
	}
	mitOrt, download := s.fotoRechte(r)
	ok(w, map[string]any{"fotos": s.fotoAntworten(r, fotos, mitOrt, download), "gesamt": gesamt,
		"weiter": q.Offset+len(fotos) < gesamt})
}

type fotoAlbumAntwort struct {
	store.FotoAlbum
	Bild string `json:"bild,omitempty"`
}

func fotoAlbumMitBild(a store.FotoAlbum) fotoAlbumAntwort {
	out := fotoAlbumAntwort{FotoAlbum: a}
	if a.Titelbild != "" {
		out.Bild = "/api/v1/medien/fotos/" + a.Titelbild + "/bild?g=klein"
	}
	return out
}

// fotoAlben: GET /fotos/alben
func (s *Server) fotoAlben(w http.ResponseWriter, r *http.Request) {
	v, err := s.fotoSicht(r)
	if err != nil {
		failErr(w, err)
		return
	}
	alben, err := s.st.FotoAlben(r.Context(), v)
	if err != nil {
		failErr(w, err)
		return
	}
	out := make([]fotoAlbumAntwort, len(alben))
	for i, a := range alben {
		out[i] = fotoAlbumMitBild(a)
	}
	ok(w, map[string]any{"alben": out})
}

// fotoAlbum: GET /fotos/alben/{id} — Name und Umfang; die Fotos kommen
// über /fotos/liste?album=.
func (s *Server) fotoAlbum(w http.ResponseWriter, r *http.Request) {
	v, err := s.fotoSicht(r)
	if err != nil {
		failErr(w, err)
		return
	}
	a, err := s.st.FotoAlbumEinzeln(r.Context(), v, r.PathValue("id"))
	if err != nil {
		failErr(w, err)
		return
	}
	ok(w, fotoAlbumMitBild(*a))
}

// fotoEinzeln: GET /fotos/{id} — für die Infos im Betrachter.
func (s *Server) fotoEinzeln(w http.ResponseWriter, r *http.Request) {
	v, err := s.fotoSicht(r)
	if err != nil {
		failErr(w, err)
		return
	}
	f, err := s.st.FotoEinzeln(r.Context(), v, r.PathValue("id"))
	if err != nil {
		failErr(w, err)
		return
	}
	mitOrt, download := s.fotoRechte(r)
	a := s.fotoAntworten(r, []store.Foto{*f}, mitOrt, download)[0]
	ok(w, map[string]any{"foto": a, "dateiname": filepath.Base(f.Pfad), "ortErlaubt": mitOrt, "downloadErlaubt": download})
}

// ---------------------------------------------------------------- Medien

// fotoBild: GET /medien/fotos/{id}/bild?g=klein|gross — ein Vorschaubild.
// Fehlt es noch (der Hintergrund war noch nicht da), wird es jetzt
// gerechnet.
func (s *Server) fotoBild(w http.ResponseWriter, r *http.Request) {
	v, err := s.fotoSicht(r)
	if err != nil {
		failErr(w, err)
		return
	}
	f, err := s.st.FotoEinzeln(r.Context(), v, r.PathValue("id"))
	if err != nil {
		failErr(w, err)
		return
	}
	g := r.URL.Query().Get("g")
	if _, okG := fotoGroessen[g]; !okG {
		g = "klein"
	}
	ziel, err := s.fotoVorschauRechnen(r.Context(), f, g, false)
	if err != nil {
		fail(w, http.StatusNotFound, "kein vorschaubild")
		return
	}
	dateiSenden(w, r, ziel, "image/jpeg", "")
}

func (s *Server) fotoVorschauPfad(f *store.Foto, g string) string {
	return filepath.Join(s.mediathekOrdner("vorschau"), f.ID[len(f.ID)-2:],
		fmt.Sprintf("%s-%s-%d.jpg", f.ID, g, f.MTime))
}

// fotoVorschauRechnen legt das Vorschaubild einer Grösse ab und vermerkt
// es. Gelingt beides nicht, wird es als gescheitert vermerkt, damit der
// Hintergrund nicht jede Nacht daran hängen bleibt.
func (s *Server) fotoVorschauRechnen(ctx context.Context, f *store.Foto, g string, niedrig bool) (string, error) {
	if s.ff == nil {
		return "", fmt.Errorf("ffmpeg fehlt")
	}
	ziel := s.fotoVorschauPfad(f, g)
	err := zwischenspeichern(ctx, ziel, func() error {
		alteFassungenWeg(filepath.Dir(ziel), f.ID+"-"+g+"-", filepath.Base(ziel))
		c := context.WithoutCancel(ctx)
		if f.Art == "video" {
			return s.ff.VideoStandbild(c, f.Pfad, ziel, fotoGroessen[g], f.DauerMS, niedrig)
		}
		return s.ff.BildVerkleinern(c, f.Pfad, ziel, fotoGroessen[g], f.Ausrichtung, f.Format, niedrig)
	})
	bit := store.VorschauKlein
	if g == "gross" {
		bit = store.VorschauGross
	}
	if err != nil {
		_ = s.st.FotoVorschauVermerken(context.WithoutCancel(ctx), f.ID, store.VorschauGescheitert)
		return "", err
	}
	if f.Vorschau&bit == 0 {
		_ = s.st.FotoVorschauVermerken(context.WithoutCancel(ctx), f.ID, bit)
	}
	return ziel, nil
}

// fotoVideo: GET /medien/fotos/{id}/video[?als=h264] — ein Handyvideo, mit
// Range. HEVC (vom iPhone) können nicht alle Browser; die holen sich mit
// als=h264 eine umgewandelte Fassung (nur kurze Videos, bis 15 Minuten).
func (s *Server) fotoVideo(w http.ResponseWriter, r *http.Request) {
	v, err := s.fotoSicht(r)
	if err != nil {
		failErr(w, err)
		return
	}
	f, err := s.st.FotoEinzeln(r.Context(), v, r.PathValue("id"))
	if err != nil {
		failErr(w, err)
		return
	}
	if f.Art != "video" {
		fail(w, http.StatusBadRequest, "kein video")
		return
	}
	if r.URL.Query().Get("als") != "h264" {
		dateiSenden(w, r, f.Pfad, "video/mp4", "")
		return
	}
	lim, _ := s.st.Limits(r.Context(), accountOf(r).ID)
	if !lim.AllowTranscode || s.ff == nil {
		fail(w, http.StatusUnsupportedMediaType, "umwandeln ist für dein profil aus")
		return
	}
	if f.DauerMS > 15*60*1000 {
		fail(w, http.StatusRequestEntityTooLarge, "das video ist zu lang zum umwandeln")
		return
	}
	// Wie bei der Musik: nur kurz, unter einem Zufallsnamen (kurzlebig.go).
	schluessel := fmt.Sprintf("video:%s:%d", f.ID, f.MTime)
	pfad, fertig, err := s.kurzlebigeDatei(r.Context(), schluessel, "mp4", func(ziel string) error {
		return s.ff.VideoUmwandeln(context.WithoutCancel(r.Context()), f.Pfad, ziel)
	})
	if err != nil {
		s.st.Log(r.Context(), "warn", "stream", "ein Video aus der Fotobibliothek liess sich nicht umwandeln")
		fail(w, http.StatusInternalServerError, "umwandeln ging nicht")
		return
	}
	defer fertig()
	dateiSenden(w, r, pfad, "video/mp4", "")
}

// fotoOriginal: GET /medien/fotos/{id}/original — die Originaldatei zum
// Herunterladen, wenn das Profil herunterladen darf.
func (s *Server) fotoOriginal(w http.ResponseWriter, r *http.Request) {
	v, err := s.fotoSicht(r)
	if err != nil {
		failErr(w, err)
		return
	}
	f, err := s.st.FotoEinzeln(r.Context(), v, r.PathValue("id"))
	if err != nil {
		failErr(w, err)
		return
	}
	lim, _ := s.st.Limits(r.Context(), accountOf(r).ID)
	if !lim.AllowDownload {
		fail(w, http.StatusForbidden, "herunterladen ist für dein profil aus")
		return
	}
	typ := map[string]string{"jpeg": "image/jpeg", "png": "image/png", "heic": "image/heic", "webp": "image/webp",
		"gif": "image/gif", "mp4": "video/mp4", "mov": "video/quicktime"}[f.Format]
	dateiSenden(w, r, f.Pfad, typ, filepath.Base(f.Pfad))
}

// ---------------------------------------------------------------- Hintergrund

var (
	vorrechnenMu sync.Mutex
)

// mediathekVorrechnen: nach einem Durchlauf die Bilder rechnen, bevor
// jemand sie braucht — Cover bei Musik, Vorschaubilder bei Fotos. Mit
// niedriger Priorität, und Pause, solange gerade umgewandelt wird.
func (s *Server) mediathekVorrechnen(ctx context.Context, lib store.Library) {
	if s.ff == nil {
		return
	}
	switch lib.Kind {
	case "photo":
		s.fotoVorschauenRechnen(ctx)
	case "music":
		alben, err := s.st.MusikAlbenMitCover(ctx, lib.ID)
		if err != nil {
			return
		}
		for _, a := range alben {
			s.warteAufRuhe(ctx)
			if ctx.Err() != nil {
				return
			}
			_, _ = s.coverRechnen(ctx, a.ID, a.CoverQuelle, "mittel", coverGroessen["mittel"], true)
		}
	}
}

// warteAufRuhe hält an, solange jemand etwas umgewandelt schaut.
func (s *Server) warteAufRuhe(ctx context.Context) {
	for i := 0; i < 120 && s.live != nil && s.live.Load().Transcodes > 0; i++ {
		select {
		case <-time.After(5 * time.Second):
		case <-ctx.Done():
			return
		}
	}
}

func (s *Server) fotoVorschauenBeimStart() {
	time.Sleep(2 * time.Minute)
	if s.dataDir == "" {
		return
	}
	s.fotoVorschauenRechnen(context.Background())
}

// fotoVorschauenRechnen arbeitet ab, was an Vorschaubildern fehlt. Nur
// einer gleichzeitig; ein zweiter Aufruf geht sofort wieder.
func (s *Server) fotoVorschauenRechnen(ctx context.Context) {
	if s.ff == nil || !vorrechnenMu.TryLock() {
		return
	}
	defer vorrechnenMu.Unlock()
	for runde := 0; runde < 10000; runde++ {
		fotos, err := s.st.FotosOhneVorschau(ctx, 50)
		if err != nil || len(fotos) == 0 {
			break
		}
		for i := range fotos {
			s.warteAufRuhe(ctx)
			if ctx.Err() != nil {
				return
			}
			f := &fotos[i]
			for _, g := range []string{"klein", "gross"} {
				if _, err := s.fotoVorschauRechnen(ctx, f, g, true); err != nil {
					break
				}
			}
		}
	}
	s.verwaisteVorschauenWeg(ctx)
}

// verwaisteVorschauenWeg: Vorschaubilder von Fotos, die es nicht mehr gibt.
func (s *Server) verwaisteVorschauenWeg(ctx context.Context) {
	ids, err := s.st.FotoIDs(ctx)
	if err != nil {
		return
	}
	_ = filepath.WalkDir(s.mediathekOrdner("vorschau"), func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		id, _, _ := strings.Cut(d.Name(), "-")
		if !ids[id] {
			_ = os.Remove(p)
		}
		return nil
	})
}
