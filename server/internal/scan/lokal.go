// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package scan

// NFO-Dateien und Bilder neben den Videodateien (Kodi, Emby, Jellyfin,
// tinyMediaManager). Der Scanner liest sie, weil er die Ordner ohnehin
// durchgeht und weil sie auch ohne TMDb-Schlüssel gelten sollen:
//
//   - Eine NFO mit Kennung legt fest, welcher TMDb-Eintrag gemeint ist;
//     die Zuordnung sucht dann nicht mehr (meta/lokal.go).
//   - Eine vollständige NFO ohne Kennung genügt für sich: Titel, Inhalt,
//     Jahr, Genres werden übernommen, der Titel gilt als „lokal" erkannt.
//   - Bilder („poster.jpg", „<Film>-fanart.jpg", „season01-poster.jpg",
//     „<Folge>-thumb.jpg") werden in den Bilderordner übernommen und
//     haben Vorrang vor TMDb. Ändert sich die Datei, kommt sie neu.
//
// Gelesen wird nur, was sich seit dem letzten Mal geändert hat
// (Änderungszeit und Grösse) — eine NAS-Platte soll nicht bei jedem
// Durchlauf jedes Bild hergeben.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"lumen/internal/lokal"
	"lumen/internal/meta"
	"lumen/internal/store"
)

// lokalLauf gilt für einen Durchlauf über eine Bibliothek.
type lokalLauf struct {
	s          *Scanner
	lib        store.Library
	bevorzugen bool
	ordner     map[string]*lokal.Ordner
	// eigen: Gehört der Ordner einem Film allein? (je Ordner einmal)
	eigen map[string]bool
}

func (s *Scanner) lokalLauf(ctx context.Context, lib store.Library) *lokalLauf {
	return &lokalLauf{s: s, lib: lib, bevorzugen: meta.NFOBevorzugt(ctx, s.st),
		ordner: map[string]*lokal.Ordner{}, eigen: map[string]bool{}}
}

func (l *lokalLauf) dir(p string) *lokal.Ordner {
	if o, ok := l.ordner[p]; ok {
		return o
	}
	o := lokal.LiesOrdner(p)
	l.ordner[p] = o
	return o
}

// eigenerOrdner: Der Ordner ist nicht die Bibliothek selbst, und alle
// Videodateien darin sind derselbe Film (Fassungen, Teile). Nur dann
// gehören „movie.nfo" und „poster.jpg" darin zu diesem Film.
func (l *lokalLauf) eigenerOrdner(video string, dirs []string, titel string) bool {
	dir := filepath.Dir(video)
	if filepath.Clean(dir) == filepath.Clean(l.lib.Path) {
		return false
	}
	if v, ok := l.eigen[dir]; ok {
		return v
	}
	// Dieselbe Ordnerliste wie für NFO und Bilder — der Ordner wird nur
	// einmal gelesen.
	eigen := true
	for _, n := range l.dir(dir).Namen() {
		if !IsVideo(n) || IsJunkFile(n) {
			continue
		}
		g := teileKlaeren(ParseFuer(filepath.Join(dir, n), dirs, l.lib.Kind), filepath.Join(dir, n), dirs, l.lib.Kind, nil)
		if g.Kind != "movie" || !strings.EqualFold(g.Title, titel) {
			eigen = false
			break
		}
	}
	l.eigen[dir] = eigen
	return eigen
}

// film liest NFO und Bilder eines Films.
func (l *lokalLauf) film(ctx context.Context, video string, dirs []string, g Guess) {
	id, err := l.s.st.ItemZuPfad(ctx, video)
	if err != nil || id == "" {
		return
	}
	o := l.dir(filepath.Dir(video))
	stamm := strings.TrimSuffix(filepath.Base(video), filepath.Ext(video))
	eigen := discOrdner(dirs) >= 0 || l.eigenerOrdner(video, dirs, g.Title)
	if k := discOrdner(dirs); k >= 0 {
		// Bei BDMV und VIDEO_TS liegen NFO und Poster im Filmordner darüber.
		o = l.dir(filepath.Join(append([]string{l.lib.Path}, dirs[:k]...)...))
		stamm = "movie"
	}
	l.nfo(ctx, id, o.NFOFuerFilm(stamm, eigen))
	l.bilder(ctx, id, o.BilderFuerFilm(stamm, eigen))
}

// serie liest tvshow.nfo und die Bilder im Serienordner.
func (l *lokalLauf) serie(ctx context.Context, id, ordner string) {
	if ordner == "" {
		return
	}
	o := l.dir(ordner)
	l.nfo(ctx, id, o.Finde("tvshow.nfo"))
	l.bilder(ctx, id, o.BilderFuerSerie())
}

// staffel liest season.nfo und das Poster einer Staffel.
func (l *lokalLauf) staffel(ctx context.Context, id string, nr int, serienOrdner, staffelOrdner string) {
	if serienOrdner == "" {
		return
	}
	var so *lokal.Ordner
	if staffelOrdner != "" {
		so = l.dir(staffelOrdner)
		l.nfo(ctx, id, so.Finde("season.nfo"))
	}
	if p := lokal.PosterFuerStaffel(l.dir(serienOrdner), so, nr); p != "" {
		l.bilder(ctx, id, map[string]string{"poster": p})
	}
}

// folge liest „<Folge>.nfo" und „<Folge>-thumb.jpg". Das Vorschaubild
// einer Folge führt Lumen wie TMDb als ihr „poster".
func (l *lokalLauf) folge(ctx context.Context, id, video string) {
	o := l.dir(filepath.Dir(video))
	stamm := strings.TrimSuffix(filepath.Base(video), filepath.Ext(video))
	l.nfo(ctx, id, o.Finde(stamm+".nfo"))
	if p := o.VorschauFuerFolge(stamm); p != "" {
		l.bilder(ctx, id, map[string]string{"poster": p})
	}
}

// nfo übernimmt eine NFO, wenn sie neu ist oder sich geändert hat.
func (l *lokalLauf) nfo(ctx context.Context, id, pfad string) {
	st := l.s.st
	if pfad == "" {
		return
	}
	info, err := os.Stat(pfad)
	if err != nil {
		return
	}
	alt, hatte := st.LokaleNFOZu(ctx, id)
	if hatte && alt.Pfad == pfad && alt.MTime == info.ModTime().Unix() {
		return
	}
	n, err := lokal.LeseNFO(pfad)
	if err != nil {
		st.Log(ctx, "warn", "scan", "eine NFO-Datei liess sich nicht lesen: "+grund(err))
		return
	}
	daten, _ := json.Marshal(n)
	if err := st.LokaleNFOSetzen(ctx, id, store.LokaleNFO{Pfad: pfad, MTime: info.ModTime().Unix(), Daten: string(daten)}); err != nil {
		return
	}
	if hatte {
		nfoGeaendert(ctx, id) // zählt im Ergebnis als aktualisiert
	}
	it, err := st.Item(ctx, id)
	if err != nil || it.MatchState == "manual" {
		// Von Hand zugewiesen: Das gilt, auch gegen eine NFO.
		return
	}
	if n.TMDb != "" && it.Kind != "episode" && it.Kind != "season" {
		alteKennung, _ := st.SourceID(ctx, id, "tmdb")
		if alteKennung != n.TMDb {
			_ = st.SetSourceID(ctx, id, "tmdb", n.TMDb)
			// Unter einer anderen Kennung erkannt? Dann neu holen.
			if it.MatchState != "pending" {
				it.MatchState = "pending"
			}
		}
	}
	if n.TVDb != "" && it.Kind == "series" {
		_ = st.SetSourceID(ctx, id, "tvdb", n.TVDb)
	}
	switch {
	case it.Kind == "episode" || it.Kind == "season":
		// Folgen und Staffeln hängen an der Serie; ihr Zustand bleibt.
		// Die Werte gelten, wenn der Besitzer es so will — oder solange
		// noch nichts Besseres da ist.
		if l.bevorzugen || !store.IstErkannt(it.MatchState) {
			n.Anwenden(it, meta.AgeYears)
		}
	case n.HatKennung():
		// TMDb holt den genauen Eintrag; „nicht erkannt" stimmt nicht mehr.
		if it.MatchState == "unmatched" || it.MatchState == "lokal" {
			it.MatchState = "pending"
		}
		if l.bevorzugen || it.MatchState == "pending" {
			n.Anwenden(it, meta.AgeYears)
		}
	case n.Vollstaendig():
		if l.bevorzugen || it.MatchState != "matched" {
			n.Anwenden(it, meta.AgeYears)
		}
		if it.MatchState != "matched" {
			it.MatchState = "lokal"
		}
	default:
		// Halbe NFO ohne Kennung: Titel und Jahr helfen wenigstens der
		// Suche; die Zuordnung legt die übrigen Werte später darüber.
		if l.bevorzugen || it.MatchState != "matched" {
			n.Anwenden(it, meta.AgeYears)
		}
	}
	_ = st.UpsertItem(ctx, it)
}

// bilder übernimmt Bilder in den Bilderordner, wenn sie neu sind oder
// sich geändert haben.
func (l *lokalLauf) bilder(ctx context.Context, id string, bilder map[string]string) {
	if l.s.BilderOrdner == "" || len(bilder) == 0 {
		return
	}
	for art, pfad := range bilder {
		// Ein Verweis in den Datenordner („poster.jpg" → /data/lumen.db)
		// wird nicht kopiert und dann als Bild ausgeliefert.
		if InDatenordner(pfad) {
			continue
		}
		info, err := os.Stat(pfad)
		if err != nil || !info.Mode().IsRegular() || info.Size() > 20<<20 {
			continue
		}
		jetzt := store.LokalesBild{Pfad: pfad, MTime: info.ModTime().Unix(), Groesse: info.Size()}
		if alt, ok := l.s.st.LokalesBildZu(ctx, id, art); ok && alt == jetzt {
			continue
		}
		name := fmt.Sprintf("%s-%s-lokal%s", id, art, strings.ToLower(filepath.Ext(pfad)))
		if err := kopieren(pfad, filepath.Join(l.s.BilderOrdner, name)); err != nil {
			l.s.st.Log(ctx, "warn", "scan", "ein Bild liess sich nicht übernehmen: "+grund(err))
			continue
		}
		// Ein Bild in anderem Format als vorher: das alte wegräumen.
		for _, e := range []string{".jpg", ".jpeg", ".png", ".webp"} {
			if alt := fmt.Sprintf("%s-%s-lokal%s", id, art, e); alt != name {
				_ = os.Remove(filepath.Join(l.s.BilderOrdner, alt))
			}
		}
		_ = l.s.st.LokalesBildSetzen(ctx, id, art, name, jetzt)
	}
}

// kopieren schreibt erst daneben und benennt dann um, wie beim
// Herunterladen: Es entsteht nie ein halbes Bild.
func kopieren(von, nach string) error {
	q, err := os.Open(von)
	if err != nil {
		return err
	}
	defer q.Close()
	tmp := nach + ".teil"
	z, err := os.Create(tmp)
	if err != nil {
		return err
	}
	_, err = io.Copy(z, io.LimitReader(q, 20<<20))
	if cerr := z.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, nach)
}

// serienOrdnerZu: der Ordner einer Serie — über dem Staffelordner, sonst
// der äusserste Ordner unter der Bibliothek. Liegt die Folge direkt in
// der Bibliothek, gibt es keinen.
func serienOrdnerZu(libPfad string, dirs []string, art string) (serie, staffel string) {
	for i := len(dirs) - 1; i >= 0; i-- {
		if _, _, ok := staffelOrdner(dirs[i], art); ok || reSpecialsDir.MatchString(strings.TrimSpace(dirs[i])) {
			staffel = filepath.Join(append([]string{libPfad}, dirs[:i+1]...)...)
			if i > 0 {
				serie = filepath.Join(append([]string{libPfad}, dirs[:i]...)...)
			}
			return serie, staffel
		}
	}
	if len(dirs) > 0 {
		serie = filepath.Join(libPfad, dirs[0])
	}
	return serie, ""
}
