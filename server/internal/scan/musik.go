// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package scan

// Musikbibliotheken einlesen.
//
// Die Tags in den Dateien sind die erste Quelle. Wo sie fehlen — und bei
// gerippten CDs von früher fehlen sie oft —, sagt der Ablageort, was es
// ist: „Interpret/Album (Jahr)/01 - Titel.flac" ist die Ordnung, die fast
// alle Rip-Programme anlegen.

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"lumen/internal/store"
)

// MusikTags: was ffprobe aus einer Musikdatei liest.
type MusikTags struct {
	Titel          string
	Interpret      string
	AlbumInterpret string
	Album          string
	Track          int
	Disc           int
	Jahr           int
	Genre          string
	Komponist      string
	DauerMS        int64
	Compilation    bool
	Explizit       bool
	Codec          string
	Container      string
	Bitrate        int
	Abtastrate     int
	HatCover       bool
	// Sortier-Tags (TITLESORT, ARTISTSORT, ALBUMSORT, ALBUMARTISTSORT):
	// „Beatles, The" oder „Bowie, David" — leer, wenn die Datei keine hat.
	TitelSort, InterpretSort, AlbumSort, AlbumInterpretSort string
}

// MusikProber liest Tags. Der Scanner fragt per Typprüfung danach, damit
// sich New() und die Tests ohne FFmpeg nicht ändern müssen.
type MusikProber interface {
	MusikTags(ctx context.Context, path string) (*MusikTags, error)
}

// AudioExt: Endungen, die als Musik gelten. Auch Formate, die kaum ein
// Browser kann (APE, WMA, ALAC in .m4a) — die wandelt der Server beim
// Abspielen um.
var AudioExt = map[string]bool{
	".mp3": true, ".flac": true, ".m4a": true, ".aac": true, ".ogg": true, ".oga": true,
	".opus": true, ".wav": true, ".aif": true, ".aiff": true, ".wma": true, ".ape": true,
	".alac": true, ".wv": true, ".mka": true, ".dsf": true, ".m4b": true,
}

func IsAudio(path string) bool { return AudioExt[strings.ToLower(filepath.Ext(path))] }

// mediathekOrdnerUeberspringen: versteckte Ordner und was NAS-Systeme
// anlegen — Synology „@eaDir" mit eigenen Vorschaubildern, „#recycle"
// als Papierkorb. Die gehören nicht in die Sammlung.
func mediathekOrdnerUeberspringen(name string) bool {
	return strings.HasPrefix(name, ".") || strings.HasPrefix(name, "@") || strings.HasPrefix(name, "#") ||
		strings.EqualFold(name, "$RECYCLE.BIN") || strings.EqualFold(name, "System Volume Information")
}

func mediathekDateiUeberspringen(name string) bool {
	return strings.HasPrefix(name, ".") || strings.HasPrefix(name, "._")
}

var (
	reMusikDiscOrdner = regexp.MustCompile(`(?i)^(cd|disc|disk|scheibe)\s*[-_ ]?\s*(\d{1,2})$`)
	// „01 - Titel", „01. Titel", „1-03 Titel" (Disc-Track), „A1 Titel" nicht.
	reMusikTrackVorne = regexp.MustCompile(`^(?:(\d{1,2})[-.])?(\d{1,3})(?:\s*[-._)]\s*|\s+)(.+)$`)
	reMusikJahrHinten = regexp.MustCompile(`\s*[\(\[]((?:19|20)\d{2})[\)\]]\s*$`)
	reMusikJahrVorne  = regexp.MustCompile(`^((?:19|20)\d{2})\s*[-–]\s*(.+)$`)
	reMusikZahlVorne  = regexp.MustCompile(`^\s*(\d+)`)
)

// MusikAusPfad liest, was der Ablageort über ein Stück sagt. rel ist der
// Pfad relativ zur Bibliothek.
func MusikAusPfad(rel string) (interpret, album, titel string, track, disc, jahr int) {
	teile := strings.Split(filepath.ToSlash(rel), "/")
	name := strings.TrimSuffix(teile[len(teile)-1], filepath.Ext(teile[len(teile)-1]))
	ordner := teile[:len(teile)-1]

	// Disc-Ordner („CD2") zählen nicht als Album.
	if n := len(ordner); n > 0 {
		if m := reMusikDiscOrdner.FindStringSubmatch(strings.TrimSpace(ordner[n-1])); m != nil {
			disc, _ = strconv.Atoi(m[2])
			ordner = ordner[:n-1]
		}
	}
	if n := len(ordner); n > 0 {
		album = strings.TrimSpace(ordner[n-1])
		if m := reMusikJahrHinten.FindStringSubmatchIndex(album); m != nil {
			jahr, _ = strconv.Atoi(album[m[2]:m[3]])
			album = strings.TrimSpace(album[:m[0]])
		} else if m := reMusikJahrVorne.FindStringSubmatch(album); m != nil {
			jahr, _ = strconv.Atoi(m[1])
			album = strings.TrimSpace(m[2])
		}
		if n > 1 {
			interpret = strings.TrimSpace(ordner[n-2])
		}
	}

	titel = strings.TrimSpace(strings.ReplaceAll(name, "_", " "))
	if m := reMusikTrackVorne.FindStringSubmatch(titel); m != nil {
		if m[1] != "" {
			disc, _ = strconv.Atoi(m[1])
		}
		track, _ = strconv.Atoi(m[2])
		titel = strings.TrimSpace(m[3])
	}
	// „Interpret - Titel" im Dateinamen, wenn der Ordner den Interpreten
	// nicht hergibt oder er dort ohnehin steht.
	if vor, nach, ok := strings.Cut(titel, " - "); ok {
		vor = strings.TrimSpace(vor)
		if interpret == "" || strings.EqualFold(vor, interpret) {
			interpret, titel = vor, strings.TrimSpace(nach)
		}
	}
	return
}

// zahlVorne: „3/12" → 3, „03" → 3.
func zahlVorne(s string) int {
	m := reMusikZahlVorne.FindStringSubmatch(s)
	if m == nil {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	return n
}

// ZahlAusTag ist zahlVorne für die Prober in anderen Paketen.
func ZahlAusTag(s string) int { return zahlVorne(s) }

// JahrAusTag: „2001-05-14", „2001", „14.05.2001" → 2001.
func JahrAusTag(s string) int {
	if m := reJahrIrgendwo.FindString(s); m != "" {
		n, _ := strconv.Atoi(m)
		return n
	}
	return 0
}

var reJahrIrgendwo = regexp.MustCompile(`(?:19|20)\d{2}`)

// verschiedeneInterpreten: Albuminterpreten, die „keiner bestimmt" heissen.
func verschiedeneInterpreten(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "various artists", "various", "va", "v.a.", "verschiedene interpreten", "verschiedene",
		"diverse", "diverse interpreten", "artistes divers", "varios artistas", "artisti vari":
		return true
	}
	return false
}

// scanMusik liest eine Musikbibliothek ein. Unveränderte Dateien (gleiche
// Grösse, gleiches Änderungsdatum) werden nicht neu ausgemessen.
func (s *Scanner) scanMusik(ctx context.Context, lib store.Library, res *Result) error {
	mp, _ := s.probe.(MusikProber)
	bekannt, err := s.st.MusikDateien(ctx, lib.ID)
	if err != nil {
		return err
	}

	// Vorlauf: zählen, damit die Verwaltung einen Anteil zeigen kann.
	gesamt := 0
	_ = filepath.WalkDir(lib.Path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != lib.Path && mediathekOrdnerUeberspringen(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if IsAudio(p) && !mediathekDateiUeberspringen(d.Name()) {
			gesamt++
		}
		return nil
	})
	s.melden(lib.ID, func(f *Fortschritt) { f.Gesamt = gesamt })

	gesehen := map[string]bool{}
	walkErr := filepath.WalkDir(lib.Path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			s.st.Log(ctx, "warn", "scan", fmt.Sprintf("übersprungen: %s (%v)", p, err))
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() {
			if p != lib.Path && mediathekOrdnerUeberspringen(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !IsAudio(p) || mediathekDateiUeberspringen(d.Name()) || VerweisNachDraussen(lib.Path, p, d) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		gesehen[p] = true
		s.melden(lib.ID, func(f *Fortschritt) { f.Gefunden++; f.Datei = d.Name() })
		defer s.melden(lib.ID, func(f *Fortschritt) { f.Fertig++ })

		if alt, ok := bekannt[p]; ok && alt.Groesse == info.Size() && alt.MTime == info.ModTime().Unix() {
			if alt.Fehlt {
				_ = s.st.MusikWiederDa(ctx, alt.ID)
			}
			return nil
		}
		if s.Bremse != nil && s.Bremse() {
			select {
			case <-time.After(400 * time.Millisecond):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		rel, _ := filepath.Rel(lib.Path, p)
		t := musikTitelAus(ctx, mp, p, rel)
		t.LibraryID, t.Pfad, t.Groesse, t.MTime = lib.ID, p, info.Size(), info.ModTime().Unix()
		neu, err := s.st.MusikTitelSpeichern(ctx, t)
		if err != nil {
			s.st.Log(ctx, "error", "scan", fmt.Sprintf("%s: %v", d.Name(), err))
			return nil
		}
		if neu {
			res.Added++
		} else {
			res.Updated++
		}
		return nil
	})
	if walkErr != nil && ctx.Err() == nil {
		return walkErr
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	// Wie bei Filmen: Ein leerer Ordner kann eine fehlende Platte sein —
	// dann lieber nichts als verschwunden vermerken.
	if len(gesehen) == 0 {
		s.st.Log(ctx, "warn", "scan", fmt.Sprintf(
			"%s: keine einzige Musikdatei gefunden — nichts als verschwunden vermerkt.", lib.Name))
	} else if n, err := s.st.MusikFehlende(ctx, lib.ID, gesehen); err == nil {
		res.Missing = n
	}
	return s.musikAlbenBilden(ctx, lib)
}

// musikTitelAus: Tags lesen, Lücken aus dem Pfad füllen.
func musikTitelAus(ctx context.Context, mp MusikProber, pfad, rel string) *store.MusikTitel {
	t := &store.MusikTitel{Container: strings.TrimPrefix(strings.ToLower(filepath.Ext(pfad)), ".")}
	if mp != nil {
		if tags, err := mp.MusikTags(ctx, pfad); err == nil && tags != nil {
			t.Titel, t.Interpret, t.AlbumInterpret, t.Album = tags.Titel, tags.Interpret, tags.AlbumInterpret, tags.Album
			t.Track, t.Disc, t.Jahr, t.Genre, t.Komponist = tags.Track, tags.Disc, tags.Jahr, tags.Genre, tags.Komponist
			t.DauerMS, t.Compilation, t.Explizit = tags.DauerMS, tags.Compilation, tags.Explizit
			t.Codec, t.Bitrate, t.Abtastrate, t.HatCover = tags.Codec, tags.Bitrate, tags.Abtastrate, tags.HatCover
			t.TitelSortTag, t.InterpretSortTag = tags.TitelSort, tags.InterpretSort
			t.AlbumSortTag, t.AlbumInterpretSortTag = tags.AlbumSort, tags.AlbumInterpretSort
			if tags.Container != "" {
				t.Container = tags.Container
			}
		}
	}
	pi, pa, pt, ptr, pd, pj := MusikAusPfad(rel)
	if strings.TrimSpace(t.Titel) == "" {
		t.Titel = pt
	}
	if strings.TrimSpace(t.Interpret) == "" {
		t.Interpret = pi
		if t.Interpret == "" {
			t.Interpret = t.AlbumInterpret
		}
	}
	if strings.TrimSpace(t.Album) == "" {
		t.Album = pa
	}
	if t.Track == 0 {
		t.Track = ptr
	}
	if t.Disc == 0 {
		t.Disc = pd
	}
	if t.Jahr == 0 {
		t.Jahr = pj
	}
	if t.Titel == "" {
		t.Titel = filepath.Base(pfad)
	}
	for _, f := range []*string{&t.Titel, &t.Interpret, &t.AlbumInterpret, &t.Album, &t.Genre, &t.Komponist} {
		*f = strings.TrimSpace(*f)
	}
	return t
}

// albumOrdner: der Ordner, der für das Album steht — bei „Album/CD2/…"
// der Albumordner, nicht der Disc-Ordner.
func albumOrdner(libPfad, pfad string) string {
	dir := filepath.Dir(pfad)
	if reMusikDiscOrdner.MatchString(strings.TrimSpace(filepath.Base(dir))) && dir != libPfad {
		dir = filepath.Dir(dir)
	}
	return dir
}

// coverDateien: Namen, unter denen Rip-Programme und Leute das Cover
// neben die Lieder legen.
var coverNamen = []string{"cover", "folder", "front", "album", "albumart", "albumartlarge"}

func coverImOrdner(dir string) string {
	eintraege, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	beste, rang := "", len(coverNamen)
	for _, e := range eintraege {
		if e.IsDir() {
			continue
		}
		n := strings.ToLower(e.Name())
		ext := filepath.Ext(n)
		if ext != ".jpg" && ext != ".jpeg" && ext != ".png" && ext != ".webp" {
			continue
		}
		stamm := strings.TrimSuffix(n, ext)
		for i, c := range coverNamen {
			if stamm == c && i < rang {
				beste, rang = filepath.Join(dir, e.Name()), i
			}
		}
	}
	return beste
}

// MusikAlbumGruppe: die Titel eines Albums und was daraus folgt.
type albumGruppe struct {
	ordner string
	name   string
	titel  []store.MusikTitel
}

// musikAlbenBilden fasst die Titel einer Bibliothek zu Alben zusammen.
//
// Ein Album ist: derselbe Albumname im selben Ordner (Disc-Ordner zählen
// zum Album). Der Albuminterpret kommt aus dem Tag; fehlt er, entscheidet,
// wer auf dem Album singt — einer allein (oder fast allein, bei einem
// Gastauftritt) ist der Interpret, viele verschiedene machen eine
// Kompilation. So zerfällt ein Album mit „feat."-Titeln nicht in zehn.
func (s *Scanner) musikAlbenBilden(ctx context.Context, lib store.Library) error {
	titel, err := s.st.MusikTitelDerBibliothek(ctx, lib.ID)
	if err != nil {
		return err
	}
	gruppen := map[string]*albumGruppe{}
	var schluessel []string
	for _, t := range titel {
		if t.Fehlt {
			continue
		}
		name := t.Album
		if name == "" {
			name = "Unbekanntes Album"
		}
		ord := albumOrdner(lib.Path, t.Pfad)
		k := ord + "\x00" + strings.ToLower(name)
		g, ok := gruppen[k]
		if !ok {
			g = &albumGruppe{ordner: ord, name: name}
			gruppen[k] = g
			schluessel = append(schluessel, k)
		}
		g.titel = append(g.titel, t)
	}
	sort.Strings(schluessel)

	alben := make([]store.MusikAlbum, 0, len(gruppen))
	zuordnung := map[string]string{}
	for _, k := range schluessel {
		g := gruppen[k]
		rel, _ := filepath.Rel(lib.Path, g.ordner)
		a := AlbumAusTiteln(lib.ID, rel, g.name, g.titel)
		if c := coverImOrdner(g.ordner); c != "" {
			a.CoverQuelle = "datei:" + c
		} else {
			// Bei mehreren Discs liegt das Cover manchmal nur in „CD1".
			for _, t := range g.titel {
				if d := filepath.Dir(t.Pfad); d != g.ordner {
					if c := coverImOrdner(d); c != "" {
						a.CoverQuelle = "datei:" + c
						break
					}
				}
			}
		}
		if a.CoverQuelle == "" {
			for _, t := range g.titel {
				if t.HatCover {
					a.CoverQuelle = "eingebettet:" + t.Pfad
					break
				}
			}
		}
		for _, t := range g.titel {
			zuordnung[t.ID] = a.ID
		}
		alben = append(alben, a)
	}
	return s.st.MusikAlbenSetzen(ctx, lib.ID, alben, zuordnung)
}

// AlbumAusTiteln bestimmt Interpret, Jahr, Genre und Kennung eines Albums
// aus seinen Titeln. Ohne Cover — das sucht der Aufrufer.
func AlbumAusTiteln(libraryID, relOrdner, name string, titel []store.MusikTitel) store.MusikAlbum {
	a := store.MusikAlbum{ID: store.AlbumID(libraryID, relOrdner, name), LibraryID: libraryID, Name: name}
	haeufig := func(get func(store.MusikTitel) string) (string, int) {
		n := map[string]int{}
		orig := map[string]string{}
		for _, t := range titel {
			v := strings.TrimSpace(get(t))
			if v == "" {
				continue
			}
			k := strings.ToLower(v)
			n[k]++
			if _, ok := orig[k]; !ok {
				orig[k] = v
			}
		}
		beste, bn := "", 0
		for k, c := range n {
			if c > bn || (c == bn && k < strings.ToLower(beste)) {
				beste, bn = orig[k], c
			}
		}
		return beste, len(n)
	}
	albumInterpret, _ := haeufig(func(t store.MusikTitel) string { return t.AlbumInterpret })
	interpret, verschiedene := haeufig(func(t store.MusikTitel) string { return t.Interpret })
	comp := false
	for _, t := range titel {
		if t.Compilation {
			comp = true
		}
		if t.Jahr > a.Jahr {
			a.Jahr = t.Jahr
		}
		a.DauerMS += t.DauerMS
		if a.Hinzugefuegt.IsZero() || t.Hinzugefuegt.Before(a.Hinzugefuegt) {
			a.Hinzugefuegt = t.Hinzugefuegt
		}
	}
	a.Anzahl = len(titel)
	a.Genre, _ = haeufig(func(t store.MusikTitel) string { return t.Genre })

	switch {
	case albumInterpret != "" && !verschiedeneInterpreten(albumInterpret):
		a.Interpret = albumInterpret
	case albumInterpret != "" || comp:
		a.Compilation = true
	case verschiedene <= 1:
		a.Interpret = interpret
	default:
		// Mehrere Interpreten ohne Albuminterpret: dominiert einer (drei
		// Viertel der Titel), ist es sein Album mit Gästen.
		n := 0
		for _, t := range titel {
			if strings.EqualFold(strings.TrimSpace(t.Interpret), interpret) {
				n++
			}
		}
		if n*4 >= len(titel)*3 {
			a.Interpret = interpret
		} else {
			a.Compilation = true
		}
	}
	if !a.Compilation {
		a.InterpretID = store.InterpretID(a.Interpret)
	}
	// Sortieren nach den Sortier-Tags der Titel: der häufigste Schlüssel
	// des Albumnamens, und der des Interpreten von einem Titel, an dem er
	// als Albuminterpret (sonst als Interpret) steht.
	a.NameSchluessel, _ = haeufig(func(t store.MusikTitel) string { return t.AlbumSchluessel })
	if a.Interpret != "" {
		for _, t := range titel {
			if t.AlbumInterpretSchluessel != "" && strings.EqualFold(strings.TrimSpace(t.AlbumInterpret), a.Interpret) {
				a.InterpretSchluessel = t.AlbumInterpretSchluessel
				break
			}
		}
		if a.InterpretSchluessel == "" {
			for _, t := range titel {
				if t.InterpretSchluessel != "" && strings.EqualFold(strings.TrimSpace(t.Interpret), a.Interpret) {
					a.InterpretSchluessel = t.InterpretSchluessel
					break
				}
			}
		}
	}
	return a
}
