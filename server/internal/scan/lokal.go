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
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"lumen/internal/ablage"
	"lumen/internal/lokal"
	"lumen/internal/meta"
	"lumen/internal/store"
)

// freigabenStand zählt die Fassungen, in denen meta.AgeYears mehr
// Kennzeichen lesen lernte. Steigt er, rechnet der nächste Durchlauf die
// Altersgrenzen einmal nach (store.FreigabenNachrechnen).
const (
	freigabenStand       = "1"
	einstellungFreigaben = "nachtrag.freigaben"
)

// lokalLauf gilt für einen Durchlauf über eine Bibliothek.
type lokalLauf struct {
	s          *Scanner
	lib        store.Library
	bevorzugen bool
	// quelleBereit: Ist eine Metadaten-Quelle (TMDb) eingerichtet? Ohne
	// gelten vollständige NFOs mit Kennung als erkannt.
	quelleBereit bool
	ordner       map[string]*lokal.Ordner
	// eigen: Gehört der Ordner einem Film allein? (je Ordner einmal)
	eigen map[string]bool
}

func (s *Scanner) lokalLauf(ctx context.Context, lib store.Library) *lokalLauf {
	// Kennzeichen, die eine ältere Fassung nicht lesen konnte („DE-18"),
	// bekommen jetzt ihre Grenze — ohne dass die NFO sich ändern muss.
	// Einmal je Fassung des Lesens (freigabenStand), nicht bei jedem
	// Durchlauf jeder Bibliothek: Neue Titel bekommen ihre Grenze ohnehin
	// beim Übernehmen der NFO oder von TMDb.
	if s.st.Setting(ctx, einstellungFreigaben, "") != freigabenStand {
		if n, err := s.st.FreigabenNachrechnen(ctx, meta.AgeYears); err == nil {
			_ = s.st.SetSetting(ctx, einstellungFreigaben, freigabenStand)
			if n > 0 {
				s.st.Log(ctx, "info", "scan", fmt.Sprintf("Altersfreigabe bei %d Titeln nachgetragen", n))
			}
		}
	}
	bereit := meta.QuelleBereit(ctx, s.st)
	if !bereit {
		// Titel aus einem früheren Durchlauf, die noch auf TMDb warten,
		// obwohl ihre NFO genügt (die NFO selbst liest der Durchlauf nur
		// neu, wenn sie sich ändert).
		meta.OhneQuelleAbschliessen(ctx, s.st)
	}
	return &lokalLauf{s: s, lib: lib, bevorzugen: meta.NFOBevorzugt(ctx, s.st),
		quelleBereit: bereit,
		ordner:       map[string]*lokal.Ordner{}, eigen: map[string]bool{}}
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
	l.kennungenAusNamen(ctx, id, lokal.KennungenFilm(video, dirs))
}

// kennungenAusNamen speichert Kennungen aus dem Datei- oder Ordnernamen
// („Matrix (1999) {imdb-tt0133093}") gleich beim Durchlauf — auch ohne
// TMDb-Schlüssel: für die Links auf der Titelseite und den Abgleich beim
// Umzug. Nur, wo noch keine steht; was TMDb oder eine NFO sagt, bleibt.
func (l *lokalLauf) kennungenAusNamen(ctx context.Context, id string, k lokal.Kennungen) {
	if k.Leer() {
		return
	}
	st := l.s.st
	for _, x := range []struct{ quelle, wert string }{{"tmdb", k.TMDb}, {"imdb", k.IMDb}, {"tvdb", k.TVDb}} {
		if x.wert == "" {
			continue
		}
		if alt, err := st.SourceID(ctx, id, x.quelle); err == nil && alt == "" {
			_ = st.SetSourceID(ctx, id, x.quelle, x.wert)
		}
	}
}

// serie liest tvshow.nfo und die Bilder im Serienordner.
func (l *lokalLauf) serie(ctx context.Context, id, ordner string) {
	if ordner == "" {
		return
	}
	o := l.dir(ordner)
	l.nfo(ctx, id, o.Finde("tvshow.nfo"))
	l.bilder(ctx, id, o.BilderFuerSerie())
	if rel, err := filepath.Rel(l.lib.Path, ordner); err == nil && !strings.HasPrefix(rel, "..") && rel != "." {
		l.kennungenAusNamen(ctx, id, lokal.KennungenSerie(strings.Split(filepath.ToSlash(rel), "/")))
	}
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
	// Emby legt Folgenbilder auch in „metadata/<Folge>.jpg" ab.
	if p := lokal.VorschauFuerFolgeIn(o, l.dir(filepath.Join(filepath.Dir(video), "metadata")), stamm); p != "" {
		l.bilder(ctx, id, map[string]string{"poster": p})
	}
	// Doppelfolge laut NFO (<episodenumberend>): Der Durchlauf setzt das
	// Ende sonst jedes Mal aus dem Dateinamen zurück.
	if roh, ok := l.s.st.LokaleNFOZu(ctx, id); ok {
		var n lokal.NFO
		if json.Unmarshal([]byte(roh.Daten), &n) == nil && n.BisFolge > 0 {
			if z := l.s.st.ZusatzZu(ctx, id); n.BisFolge > z.BisFolge {
				z.BisFolge = n.BisFolge
				_ = l.s.st.ZusatzSetzen(ctx, id, z)
			}
		}
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
	// Unverändert — ausser die Daten stammen aus der Zeit vor der Quelle
	// der Bewertung (lokal.NFO.BewertungQuelle): dann einmal neu lesen.
	if hatte && alt.Pfad == pfad && alt.MTime == info.ModTime().Unix() && strings.Contains(alt.Daten, `"bewertungQuelle"`) {
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
	angewendet := false
	anwenden := func() { n.Anwenden(it, meta.AgeYears); angewendet = true }
	switch {
	case it.Kind == "episode" || it.Kind == "season":
		// Folgen und Staffeln hängen an der Serie; ihr Zustand bleibt.
		// Die Werte gelten, wenn der Besitzer es so will — oder solange
		// noch nichts Besseres da ist.
		if l.bevorzugen || !store.IstErkannt(it.MatchState) {
			anwenden()
		}
	case n.HatKennung() && n.Vollstaendig() && !l.quelleBereit:
		// Ohne TMDb-Schlüssel gibt es niemanden, der den Eintrag holen
		// könnte: Eine vollständige NFO genügt dann für sich. Die Kennung
		// bleibt gespeichert; kommt später ein Schlüssel dazu, holt die
		// Zuordnung diese Titel nach (meta.LokaleMitKennungNachholen).
		if l.bevorzugen || it.MatchState != "matched" {
			anwenden()
		}
		if it.MatchState != "matched" {
			it.MatchState = "lokal"
			meta.OhneSchluesselErkannt(ctx, st)
		}
	case n.HatKennung():
		// TMDb holt den genauen Eintrag; „nicht erkannt" stimmt nicht mehr.
		if it.MatchState == "unmatched" || it.MatchState == "lokal" {
			it.MatchState = "pending"
		}
		if l.bevorzugen || it.MatchState == "pending" {
			anwenden()
		}
	case n.Vollstaendig():
		if l.bevorzugen || it.MatchState != "matched" {
			anwenden()
		}
		if it.MatchState != "matched" {
			it.MatchState = "lokal"
		}
	default:
		// Halbe NFO ohne Kennung: Titel und Jahr helfen wenigstens der
		// Suche; die Zuordnung legt die übrigen Werte später darüber.
		if l.bevorzugen || it.MatchState != "matched" {
			anwenden()
		}
	}
	_ = st.UpsertItem(ctx, it)
	if it.Kind == "movie" || it.Kind == "series" {
		l.besetzung(ctx, it.ID, n, filepath.Dir(pfad), angewendet)
	}
	if it.Kind == "movie" {
		// <set>: die Sammlung aus Emby. Sie geht vor der selbst erkannten
		// Reihe; ohne <set> fällt der Film aus einer früheren NFO-Reihe.
		if n.Reihe != "" {
			_ = st.NFOReiheZuordnen(ctx, n.Reihe, n.ReiheTMDb, it.ID, it.Year)
		} else {
			_ = st.NFOReiheLoesen(ctx, it.ID)
		}
	}
}

// besetzung übernimmt <actor> aus der NFO — wenn die NFO-Werte gelten oder
// noch niemand anderes eine Besetzung eingetragen hat. Bilder kommen aus
// der Sammlung selbst: „.actors/Vorname_Nachname.jpg" (Kodi, Emby) oder ein
// <thumb>, der auf eine Datei neben der NFO zeigt. Adressen im Netz lädt
// der Scanner nicht; dafür steht der Name mit Initialen da.
func (l *lokalLauf) besetzung(ctx context.Context, id string, n *lokal.NFO, nfoOrdner string, angewendet bool) {
	if len(n.Besetzung) == 0 {
		return
	}
	st := l.s.st
	if !angewendet {
		if alt, err := st.Besetzung(ctx, id); err != nil || len(alt) > 0 {
			return
		}
	}
	actors := l.dir(filepath.Join(nfoOrdner, ".actors"))
	leute := make([]store.Person, 0, len(n.Besetzung))
	for i, p := range n.Besetzung {
		if i >= 30 {
			break
		}
		person := store.Person{Name: p.Name, Rolle: p.Rolle}
		if quelle := l.personenBild(actors, nfoOrdner, p); quelle != "" && l.s.BilderOrdner != "" {
			if info, err := os.Stat(quelle); err == nil {
				sum := sha1.Sum([]byte(fmt.Sprintf("%s|%d|%d", quelle, info.ModTime().Unix(), info.Size())))
				name := fmt.Sprintf("%s-cast-%s%s", id, hex.EncodeToString(sum[:5]), strings.ToLower(filepath.Ext(quelle)))
				ziel := filepath.Join(l.s.BilderOrdner, name)
				if _, err := os.Stat(ziel); err == nil || kopieren(quelle, ziel) == nil {
					person.Bild = name
				}
			}
		}
		leute = append(leute, person)
	}
	_ = st.BesetzungSetzen(ctx, id, leute)
	l.alteCastBilderWeg(ctx, id)
}

// personenBild sucht das Bild einer Person in der Sammlung. Nur
// gewöhnliche Dateien in der Bibliothek zählen — ein <thumb> mit
// „/etc/passwd" oder ein Verweis aus der Bibliothek hinaus nicht.
func (l *lokalLauf) personenBild(actors *lokal.Ordner, nfoOrdner string, p lokal.NFOPerson) string {
	name := strings.TrimSpace(p.Name)
	if name != "" {
		if b := actors.FindeBild(strings.ReplaceAll(name, " ", "_"), name); b != "" {
			return b
		}
	}
	t := strings.TrimSpace(p.Bild)
	if t == "" || strings.Contains(t, "://") {
		return ""
	}
	if !filepath.IsAbs(t) {
		t = filepath.Join(nfoOrdner, t)
	}
	t = filepath.Clean(t)
	rel, err := filepath.Rel(filepath.Clean(l.lib.Path), t)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ""
	}
	if !istBild(t) || InDatenordner(t) {
		return ""
	}
	if info, err := os.Lstat(t); err != nil || !info.Mode().IsRegular() || info.Size() > 20<<20 {
		return ""
	}
	return t
}

func istBild(p string) bool {
	switch strings.ToLower(filepath.Ext(p)) {
	case ".jpg", ".jpeg", ".png", ".webp":
		return true
	}
	return false
}

// alteCastBilderWeg: Bilder der Besetzung, auf die keine Zeile mehr zeigt.
func (l *lokalLauf) alteCastBilderWeg(ctx context.Context, id string) {
	if l.s.BilderOrdner == "" {
		return
	}
	leute, err := l.s.st.Besetzung(ctx, id)
	if err != nil {
		return
	}
	behalten := map[string]bool{}
	for _, p := range leute {
		behalten[p.Bild] = true
	}
	alle, _ := filepath.Glob(filepath.Join(l.s.BilderOrdner, id+"-cast*"))
	for _, pfad := range alle {
		if n := filepath.Base(pfad); !behalten[n] && !strings.Contains(n, ablage.Endung) {
			_ = os.Remove(pfad)
		}
	}
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
	return ablage.Einmal(nach, func() error { return ablage.Schreiben(nach, q, 20<<20) })
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
