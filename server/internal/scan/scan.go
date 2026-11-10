// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package scan

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"lumen/internal/store"
)

// Prober misst eine Datei aus. Der Scanner braucht nur diese eine Fähigkeit,
// damit er sich in Tests ohne FFmpeg prüfen lässt.
type Prober interface {
	Probe(ctx context.Context, path string) (*Probe, error)
}

type Probe struct {
	DurationMS int64
	Bitrate    int
	Width      int
	Height     int
	VideoCodec string
	HDR        string
	Container  string
	Tracks     []Track
	Chapters   []Chapter
}

type Track struct {
	Index           int
	Kind            string // audio | subtitle
	Codec           string
	Language        string
	Title           string
	Channels        int
	Forced          bool
	HearingImpaired bool
	ExternalPath    string
}

type Chapter struct {
	Index   int
	StartMS int64
	EndMS   int64
	Title   string
	Kind    string
}

// Result ist, was ein Durchlauf gebracht hat — das, was danach im Protokoll
// und in der Verwaltung steht.
type Result struct {
	Library   string        `json:"library"`
	Added     int           `json:"added"`
	Updated   int           `json:"updated"`
	Unmatched int           `json:"unmatched"`
	Missing   int           `json:"missing"`
	Took      time.Duration `json:"-"`
	TookMS    int64         `json:"tookMs"`
}

// Fortschritt ist, wie weit ein laufender Durchlauf ist.
//
// „Läuft" allein genügt nicht: bei einer grossen Sammlung dauert das
// Einlesen Minuten, und wer nur einen Kreisel sieht, weiss nicht, ob
// noch etwas passiert oder ob sich etwas aufgehängt hat.
type Fortschritt struct {
	Laeuft bool `json:"laeuft"`
	// Gesamt wird in einem schnellen Vorlauf gezählt, bevor die erste
	// Datei untersucht wird. Ohne diese Zahl liesse sich kein Anteil
	// angeben: „gefunden" wächst ja mit, und der Balken stünde die
	// ganze Zeit bei fast voll.
	Gesamt   int    `json:"gesamt"`
	Gefunden int    `json:"gefunden"` // Videodateien bisher
	Fertig   int    `json:"fertig"`   // davon eingelesen
	Datei    string `json:"datei"`    // woran gerade gearbeitet wird
	Seit     int64  `json:"seitMs"`
}

// zaehlenSchnell geht einmal über die Ordner und zählt nur, wie viele
// Videodateien darin liegen — ohne jede einzelne zu untersuchen. Das
// kostet fast nichts und macht aus dem Kreisel eine Angabe, mit der
// jemand etwas anfangen kann.
func zaehlenSchnell(wurzel string) int { return zaehlenFuer(wurzel, "") }

// zaehlenFuer zählt wie der Durchlauf selbst: Welche Ordner übersprungen
// werden, hängt von der Art der Bibliothek ab (Specials bei Serien).
func zaehlenFuer(wurzel, art string) int {
	n := 0
	disc := discWahl{}
	_ = filepath.WalkDir(wurzel, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != wurzel && ordnerUeberspringen(d.Name(), art) {
				return filepath.SkipDir
			}
			return nil
		}
		if IsVideo(path) && !IsJunkFile(path) {
			if rel, err := filepath.Rel(wurzel, path); err == nil && !disc.nimmt(path, splitDirs(rel)) {
				return nil
			}
			n++
		}
		return nil
	})
	return n
}

type Scanner struct {
	st     *store.Store
	probe  Prober
	mu     sync.Mutex
	active map[string]bool // läuft gerade für diese Bibliothek
	stand  map[string]*Fortschritt
	// BilderOrdner ist der Ordner, in dem Lumen Poster ablegt (der der
	// Metadaten). Lokale Bilder neben den Dateien („poster.jpg") werden
	// dorthin übernommen; leer heisst: keine lokalen Bilder.
	BilderOrdner string
	// Bremse sagt, ob gerade jemand zuschaut und der Rechner geschont
	// werden soll. Dann macht der Durchlauf nach jeder Datei eine kurze
	// Pause: auf einem NAS mit schwachem Prozessor hat die Wiedergabe
	// Vorrang, der Durchlauf darf länger dauern.
	Bremse func() bool
	// NachMediathek läuft nach dem Durchlauf einer Musik- oder
	// Fotobibliothek — dort stösst die API die Vorschaubilder an.
	NachMediathek func(lib store.Library)
}

// VonSelbst kennzeichnet einen Durchlauf, der von selbst läuft, nicht
// auf Knopfdruck — nur fürs Ergebnis, damit die Verwaltung es so nennt.
type vonSelbstKey struct{}

func VonSelbst(ctx context.Context) context.Context {
	return context.WithValue(ctx, vonSelbstKey{}, true)
}

func New(st *store.Store, p Prober) *Scanner {
	return &Scanner{st: st, probe: p, active: map[string]bool{},
		stand: map[string]*Fortschritt{}}
}

// Running sagt, ob für eine Bibliothek gerade ein Durchlauf läuft.
func (s *Scanner) Running(libraryID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active[libraryID]
}

// Stand sagt, wie weit der Durchlauf ist. Läuft keiner, kommt ein
// Fortschritt mit Laeuft=false zurück.
func (s *Scanner) Stand(libraryID string) Fortschritt {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.stand[libraryID]
	if !ok || !s.active[libraryID] {
		return Fortschritt{}
	}
	return *f
}

// melden schreibt den Stand fort. Der Name der Datei wird auf den
// blossen Dateinamen gekürzt — der ganze Pfad gehört nicht in eine
// Anzeige, die über die Leitung geht.
func (s *Scanner) melden(libraryID string, f func(*Fortschritt)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if st, ok := s.stand[libraryID]; ok {
		f(st)
	}
}

// Scan liest eine Bibliothek ein. Zwei Durchläufe für dieselbe Bibliothek
// gleichzeitig wären nur Arbeit doppelt — der zweite wartet nicht, er geht.
func (s *Scanner) Scan(ctx context.Context, lib store.Library) (*Result, error) {
	s.mu.Lock()
	if s.active[lib.ID] {
		s.mu.Unlock()
		return nil, fmt.Errorf("für %s läuft schon ein Durchlauf", lib.Name)
	}
	s.active[lib.ID] = true
	s.stand[lib.ID] = &Fortschritt{Laeuft: true, Seit: time.Now().UnixMilli()}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.active, lib.ID)
		delete(s.stand, lib.ID)
		s.mu.Unlock()
	}()

	// Ist der Ordner überhaupt da? Wäre er es nicht — eine Platte, die
	// nach einem Neustart nicht eingehängt wurde —, liefe der Durchlauf
	// über nichts und würde am Ende jede Datei als verschwunden
	// vermerken. Die Sammlung sähe hinterher leer aus, und niemand
	// wüsste, dass nur ein Einhängepunkt fehlte.
	if st, err := os.Stat(lib.Path); err != nil {
		return nil, fmt.Errorf("%s: der Ordner %q ist nicht erreichbar (%v)",
			lib.Name, lib.Path, err)
	} else if !st.IsDir() {
		return nil, fmt.Errorf("%s: %q ist kein Ordner", lib.Name, lib.Path)
	}

	// Musik und Fotos haben ihren eigenen Durchlauf (musik.go, fotos.go).
	if lib.Kind == "music" || lib.Kind == "photo" {
		return s.scanMediathek(ctx, lib)
	}

	// Erst zählen, dann arbeiten. Der Vorlauf ist in Sekundenbruchteilen
	// durch, und danach weiss die Anzeige, wovon sie einen Anteil zeigt.
	gesamt := zaehlenFuer(lib.Path, lib.Kind)
	s.melden(lib.ID, func(f *Fortschritt) { f.Gesamt = gesamt })
	ctx = mitDurchlauf(ctx) // siehe durchlauf.go

	start := time.Now()
	res := &Result{Library: lib.Name}
	seen := map[string]bool{}

	// Serien werden über zwei Durchgänge gebaut: erst alle Folgen einsammeln,
	// dann Serie und Staffeln daraus anlegen. Sonst gäbe es für jede Folge
	// einen Versuch, die Serie anzulegen.
	episodes := map[string][]episodeHit{}
	// Je Durchlauf: was in den Ordnern neben den Dateien liegt (NFO,
	// Bilder), welche Datei einer BluRay der Film ist, ob „Film - Teil 2"
	// ein Dateiteil ist.
	lauf := s.lokalLauf(ctx, lib)
	disc, teile := discWahl{}, teilOrdner{}

	walkErr := filepath.WalkDir(lib.Path, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// Ein unlesbarer Ordner darf den ganzen Durchlauf nicht abbrechen.
			s.st.Log(ctx, "warn", "scan", fmt.Sprintf("übersprungen: %s (%v)", path, err))
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() {
			if path != lib.Path && ordnerUeberspringen(d.Name(), lib.Kind) {
				return filepath.SkipDir
			}
			return nil
		}
		if !IsVideo(path) || IsJunkFile(path) || VerweisNachDraussen(lib.Path, path, d) {
			return nil
		}
		rel, _ := filepath.Rel(lib.Path, path)
		dirs := splitDirs(rel)
		if !disc.nimmt(path, dirs) {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return nil
		}
		if s.Bremse != nil && s.Bremse() {
			select {
			case <-time.After(400 * time.Millisecond):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		seen[path] = true
		s.melden(lib.ID, func(f *Fortschritt) {
			f.Gefunden++
			f.Datei = filepath.Base(path)
		})

		g := teileKlaeren(ParseFuer(path, dirs, lib.Kind), path, dirs, lib.Kind, teile)

		// Eine Folge bleibt eine Folge, auch wenn bei der Bibliothek
		// „Filme" steht. Vorher entschied die Art der Bibliothek darüber,
		// und wer sich beim Anlegen vertat, bekam aus fünf Folgen einen
		// Film mit fünf Dateien — ohne dass irgendwo stand, warum. Die
		// Art ist jetzt eine Angabe darüber, was jemand vorhat, keine
		// Anweisung, Offensichtliches zu übersehen: S01E02 im Namen oder
		// ein Staffelordner darüber sind deutlich genug.
		if g.Kind == "episode" {
			k := serienSchluessel(g)
			episodes[k] = append(episodes[k], episodeHit{guess: g, path: path, dirs: dirs,
				size: info.Size(), mtime: info.ModTime().Unix()})
			return nil
		}

		gleich := s.unveraendert(ctx, path, info.Size(), info.ModTime().Unix())
		added, err := s.upsertMovie(ctx, lib, g, path, info.Size(), info.ModTime().Unix())
		if err != nil {
			s.st.Log(ctx, "error", "scan", fmt.Sprintf("%s: %v", filepath.Base(path), err))
			return nil
		}
		if added {
			res.Added++
		} else if !gleich {
			res.Updated++
		}
		lauf.film(ctx, path, dirs, g)
		s.melden(lib.ID, func(f *Fortschritt) { f.Fertig++ })
		if !g.Confident {
			res.Unmatched++
		}
		return nil
	})
	if walkErr != nil && ctx.Err() == nil {
		return nil, walkErr
	}
	// Abgebrochen (Lumen fährt herunter): Was bis hierher gesehen wurde,
	// ist nur ein Teil der Sammlung. Damit weiterzumachen hiesse, alles
	// Übrige als verschwunden zu vermerken.
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("%s: durchlauf abgebrochen: %w", lib.Name, err)
	}

	// Folgen ohne Jahr oder Land gehören zur gleichnamigen Serie mit, wenn
	// es nur eine gibt.
	gruppenZusammenlegen(episodes)

	// Bei Serien geschieht die eigentliche Arbeit erst hier, im zweiten
	// Durchgang. Würde der Stand nur im ersten fortgeschrieben, stünde
	// der Balken bei einer Serienbibliothek die ganze Zeit auf Null —
	// und genau dort schaut jemand hin, weil Serien am längsten dauern.
	for key, hits := range episodes {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("%s: durchlauf abgebrochen: %w", lib.Name, err)
		}
		// Nur nummerierte Dateien („1.mkv", „2.mkv") in einem Ordner: Ab
		// drei sind es Folgen einer Miniserie, bei zwei eher ein Film in
		// zwei Teilen — der kommt als ein Film mit Teilen.
		if len(hits) <= 2 && alleNurNummer(hits) {
			for _, h := range hits {
				g := h.guess
				g.Kind, g.PartNo, g.SeasonNo, g.EpisodeNo, g.NurNummer = "movie", g.EpisodeNo, 0, 0, false
				if g.PartNo < 1 {
					g.PartNo = 1
				}
				gleich := s.unveraendert(ctx, h.path, h.size, h.mtime)
				if added, err := s.upsertMovie(ctx, lib, g, h.path, h.size, h.mtime); err == nil {
					if added {
						res.Added++
					} else if !gleich {
						res.Updated++
					}
					lauf.film(ctx, h.path, h.dirs, g)
				}
				s.melden(lib.ID, func(f *Fortschritt) { f.Fertig++ })
			}
			delete(episodes, key)
			continue
		}
		s.melden(lib.ID, func(f *Fortschritt) {
			if len(hits) > 0 {
				f.Datei = hits[0].guess.Title
			}
		})
		added, unmatched, err := s.upsertSeries(ctx, lib, hits, lauf)
		folgen := len(hits)
		s.melden(lib.ID, func(f *Fortschritt) { f.Fertig += folgen })
		if err != nil {
			s.st.Log(ctx, "error", "scan", err.Error())
			continue
		}
		res.Added += added
		res.Unmatched += unmatched
	}

	// Dasselbe eine Stufe feiner: der Ordner ist da, aber leer. Das kann
	// stimmen — oder der Einhängepunkt steht leer da, weil die Platte
	// fehlt. Im Zweifel lieber nichts anrühren: eine Bibliothek, die
	// wirklich leer ist, ist in einer Minute neu eingelesen; falsch als
	// verschwunden vermerkte Titel nehmen den Leuten ihre Merklisten mit.
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("%s: durchlauf abgebrochen: %w", lib.Name, err)
	}
	if len(seen) == 0 {
		s.st.Log(ctx, "warn", "scan", fmt.Sprintf(
			"%s: keine einzige Videodatei gefunden — nichts als verschwunden "+
				"vermerkt. Stimmt der Ordner, und ist die Platte eingehängt?", lib.Name))
	} else if gone, err := s.st.MarkMissing(ctx, lib.ID, seen); err == nil {
		res.Missing = gone
	}
	// Was früher aus einem Papierkorb oder Schnappschuss des NAS
	// eingelesen wurde („#recycle", „@eaDir"), fliegt ganz heraus — als
	// „fehlend" stünde es sonst ewig in der Verwaltung.
	if n, err := s.st.DateienAustragenWo(ctx, lib.ID, func(p string) bool {
		rel, err := filepath.Rel(lib.Path, p)
		return err == nil && !strings.HasPrefix(rel, "..") && IstSystemPfad(rel)
	}); err == nil && n > 0 {
		s.st.Log(ctx, "info", "scan", fmt.Sprintf("%s: %d Dateien aus Systemordnern ausgetragen", lib.Name, n))
	}
	res.Took = time.Since(start)
	res.TookMS = res.Took.Milliseconds()
	s.ergebnisSetzen(ctx, lib, store.ScanErgebnis{
		At: time.Now(), Added: res.Added, Updated: res.Updated, Missing: res.Missing,
		Unmatched: res.Unmatched, TookMS: res.TookMS, Auto: ctx.Value(vonSelbstKey{}) == true,
	})
	s.st.Log(ctx, "info", "scan", fmt.Sprintf(
		"%s geprüft · %d neu, %d aktualisiert, %d nicht erkannt, %d fehlen · %s",
		lib.Name, res.Added, res.Updated, res.Unmatched, res.Missing,
		res.Took.Round(time.Millisecond)))
	return res, nil
}

// ergebnisSetzen vermerkt das Ergebnis — und damit, wann zuletzt geprüft
// wurde. Ging das still schief (etwa „database is locked", weil gerade
// gesichert wird), hielt der Hausmeister die Bibliothek weiter für fällig
// und startete jede Minute einen neuen Durchlauf über die ganze
// Sammlung. Darum: ein paar Versuche, und ein Fehler steht im Protokoll.
func (s *Scanner) ergebnisSetzen(ctx context.Context, lib store.Library, e store.ScanErgebnis) {
	var err error
	for versuch := 0; versuch < 4; versuch++ {
		if versuch > 0 {
			select {
			case <-time.After(time.Duration(versuch) * 2 * time.Second):
			case <-ctx.Done():
				return
			}
		}
		if err = s.st.ScanErgebnisSetzen(ctx, lib.ID, e); err == nil {
			return
		}
	}
	s.st.Log(ctx, "error", "scan", fmt.Sprintf(
		"%s: Ergebnis des Durchlaufs liess sich nicht speichern (%v)", lib.Name, err))
}

// unveraendert: Diese Datei kennt Lumen schon, mit derselben Grösse und
// demselben Änderungsdatum. Sie zählt dann nicht als „geändert" — vorher
// meldete jedes „Prüfen" alle Filme als geändert, auch wenn nichts war.
func (s *Scanner) unveraendert(ctx context.Context, path string, size, mtime int64) bool {
	alt := s.gemessen(ctx, path)
	return alt != nil && alt.Datei.SizeBytes == size && alt.Datei.MTime == mtime
}

// ordnerUeberspringen: Beiwerk-Ordner und versteckte Ordner liest der
// Scanner nicht ein. Ausnahme „Specials" in einer Serienbibliothek: Dort
// ist das Staffel 0 — so halten es Emby und Plex, und „Doctor Who/
// Specials/Doctor Who - S00E39.mkv" ist eine richtige Folge. Bei Filmen
// bleibt „Specials" Beiwerk (Emby: Extras-Ordner neben dem Film).
func ordnerUeberspringen(name, art string) bool {
	if strings.HasPrefix(name, ".") || IstSystemOrdner(name) {
		return true
	}
	if art == "series" && reSpecialsDir.MatchString(strings.TrimSpace(name)) &&
		!strings.HasPrefix(strings.ToLower(strings.TrimSpace(name)), "extra") {
		return false
	}
	return IsExtrasDir(name)
}

type episodeHit struct {
	guess Guess
	path  string
	dirs  []string
	size  int64
	mtime int64
}

// ---------------------------------------------------------------- Filme

func (s *Scanner) upsertMovie(ctx context.Context, lib store.Library, g Guess,
	path string, size, mtime int64) (added bool, err error) {

	// „pulp fiction" aus einem kleingeschriebenen Dateinamen wird „Pulp
	// Fiction" — bis die Metadaten den richtigen Namen bringen.
	title := huebsch(g.Title)
	if title == "" {
		title = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}

	// Kennt Lumen die Datei schon, bleibt sie bei ihrem Titel. Hat sich nur
	// die Lesart des Namens geändert — etwa „ - " jetzt als Doppelpunkt —,
	// wird der Titel nachgeführt, solange er nicht von TMDb stammt.
	if id := s.itemZuPfad(ctx, path); id != "" {
		alt, err := s.st.Item(ctx, id)
		erkannt := err == nil && s.festerTitel(ctx, alt)
		// Ein nie erkannter Titel, an dem Dateien verschiedener Filme
		// hängen („Star Trek" mit Teil II, III und V, weil das Jahr vorne
		// stand), wird aufgeteilt: Diese Datei sucht sich ihren eigenen
		// Titel, statt den ganzen Eintrag umzubenennen.
		if err == nil && !erkannt && g.Title != "" && alt.Title != title &&
			!s.alleDateienSind(ctx, lib, alt.ID, title) {
			err = errAufteilen
		}
		if err == nil && alt.LibraryID == lib.ID && alt.Kind == "movie" {
			if !erkannt && g.Title != "" && alt.Title != title {
				alt.Title, alt.SortTitle = title, store.SortKey(title)
				if g.Year > 0 {
					alt.Year = g.Year
				}
				// Neu nachschlagen: unter dem alten Titel war nichts zu
				// finden, unter dem richtigen vielleicht schon.
				alt.MatchState = "pending"
				if err := s.st.UpsertItem(ctx, alt); err != nil {
					return false, err
				}
			}
			f := &store.MediaFile{
				ItemID: alt.ID, Path: path, SizeBytes: size, MTime: mtime,
				PartNo: g.PartNo, Edition: g.Edition, HDR: g.HDR,
				Container: strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), "."),
			}
			s.fillFromProbe(ctx, f)
			return false, s.dateiSchreiben(ctx, s.st, f)
		}
	}

	// Gibt es den Titel schon? Dann hängt die Datei als weitere Fassung dran —
	// so entstehen die Doppel, die das Aufräumen später zeigt.
	existing, err := s.st.TitelGleich(ctx, lib.ID, "movie", title, false)
	if err != nil {
		return false, err
	}
	var item *store.Item
	for i := range existing {
		if strings.EqualFold(existing[i].Title, title) &&
			(g.Year == 0 || existing[i].Year == 0 || existing[i].Year == g.Year) &&
			s.dateienPassen(ctx, lib, existing[i].ID, title) {
			item = &existing[i]
			break
		}
	}

	if item == nil {
		item = &store.Item{
			LibraryID: lib.ID, Kind: "movie", Title: title, Year: g.Year,
			MatchState: matchState(g),
		}
		if err := s.st.UpsertItem(ctx, item); err != nil {
			return false, err
		}
		added = true
	}

	f := &store.MediaFile{
		ItemID: item.ID, Path: path, SizeBytes: size, MTime: mtime,
		PartNo: g.PartNo, Edition: g.Edition, HDR: g.HDR,
		Container: strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), "."),
	}
	s.fillFromProbe(ctx, f)
	return added, s.dateiSchreiben(ctx, s.st, f)
}

// ---------------------------------------------------------------- Serien

// stapelGroesse: so viele Folgen höchstens in einer Transaktion. Solange
// sie läuft, wartet jeder andere Schreiber — auch der Stand einer
// laufenden Wiedergabe; 50 Folgen sind auf einem NAS deutlich unter einer
// Sekunde.
const stapelGroesse = 50

func (s *Scanner) upsertSeries(ctx context.Context, lib store.Library,
	hits []episodeHit, lauf *lokalLauf) (added, unmatched int, err error) {

	if len(hits) == 0 {
		return 0, 0, nil
	}
	title, jahr := serienTitel(hits)
	title = huebsch(title)
	land := hits[0].guess.Land

	// Zuerst: Kennt Lumen eine dieser Dateien schon, gehört sie zu einer
	// Serie — und dabei bleibt es. Nur nach dem Titel zu suchen ging
	// schief, sobald die Metadaten den Titel geändert hatten („9-1-1" im
	// Ordner, „9-1-1 Notruf L.A." von TMDb): Jeder Durchlauf fand die
	// Serie nicht und legte sie neu an — bei stündlichem Scan nach zwei
	// Tagen fünfzigmal.
	series := s.serieZuDateien(ctx, lib.ID, hits, title, jahr, land)

	if series == nil {
		existing, err := s.st.TitelGleich(ctx, lib.ID, "series", title, true)
		if err != nil {
			return 0, 0, err
		}
		for i := range existing {
			if (strings.EqualFold(existing[i].Title, title) || strings.EqualFold(existing[i].OriginalTitle, title)) &&
				s.passtZuSerie(ctx, &existing[i], jahr, land) {
				series = &existing[i]
				break
			}
		}
	}
	if series == nil {
		series = &store.Item{LibraryID: lib.ID, Kind: "series", Title: title,
			Year: jahr, MatchState: matchState(hits[0].guess)}
		if err := s.st.UpsertItem(ctx, series); err != nil {
			return 0, 0, err
		}
		added++
	} else if erkannt := s.festerTitel(ctx, series); !erkannt &&
		((jahr > 0 && series.Year != jahr) || series.Title != title) {
		// Eine nie erkannte Serie, die eine ältere Fassung ohne Jahr
		// angelegt hat („Fallout" statt „Fallout (2024)"): Jetzt wissen
		// wir mehr. Jahr und Schreibweise nachführen und neu nachschlagen
		// lassen — ohne Jahr fand TMDb womöglich die falsche oder gar
		// keine. Erkannte Serien bleiben, wie TMDb sie genannt hat.
		if jahr > 0 {
			series.Year = jahr
		}
		series.Title, series.SortTitle = title, store.SortKey(title)
		series.MatchState = "pending"
		if err := s.st.UpsertItem(ctx, series); err != nil {
			return 0, 0, err
		}
	}

	// Das Land gehört zur Serie: Daran trennt der nächste Durchlauf „The
	// Office (US)" von „(UK)", und die Zuordnung sucht im richtigen Land.
	if land != "" && s.st.ZusatzZu(ctx, series.ID).Land != land {
		_ = s.st.ZusatzSetzen(ctx, series.ID, store.Zusatz{Land: land})
	}
	serienOrdner, _ := serienOrdnerZu(lib.Path, hits[0].dirs, lib.Kind)
	lauf.serie(ctx, series.ID, serienOrdner)

	// Staffeln einmal anlegen, Folgen daran hängen.
	seasons := map[int]*store.Item{}
	for _, child := range mustChildren(ctx, s.st, series.ID) {
		if child.Kind == "season" {
			c := child
			seasons[child.SeasonNo] = &c
		}
	}

	sort.Slice(hits, func(i, j int) bool {
		a, b := hits[i].guess, hits[j].guess
		if a.SeasonNo != b.SeasonNo {
			return a.SeasonNo < b.SeasonNo
		}
		if a.EpisodeNo != b.EpisodeNo {
			return a.EpisodeNo < b.EpisodeNo
		}
		return hits[i].path < hits[j].path
	})

	// Folgen ohne Nummer im Namen („Pilot.mkv" im Staffelordner) bekommen
	// ihre Nummer nach der Reihenfolge der Dateinamen — hinter denen, die
	// eine haben. So steht die Datei als Folge da, nicht als Film.
	hoechste := map[int]int{}
	// Die Folgen jeder Staffel einmal lesen, nicht für jede Datei neu.
	folgen := map[string][]store.Item{}
	for sn, season := range seasons {
		folgen[season.ID] = mustChildren(ctx, s.st, season.ID)
		for _, c := range folgen[season.ID] {
			if c.EpisodeNo > hoechste[sn] {
				hoechste[sn] = c.EpisodeNo
			}
		}
	}
	for _, h := range hits {
		if h.guess.EpisodeNo > hoechste[h.guess.SeasonNo] {
			hoechste[h.guess.SeasonNo] = h.guess.EpisodeNo
		}
	}
	for i := range hits {
		if hits[i].guess.EpisodeNo == 0 && hits[i].guess.EpisodeTo == 0 {
			hoechste[hits[i].guess.SeasonNo]++
			hits[i].guess.EpisodeNo = hoechste[hits[i].guess.SeasonNo]
		}
	}

	// Erst messen, dann schreiben. Messen ist langsam (FFprobe, Platte);
	// geschrieben wird danach in Stapeln (store.Stapel) zu höchstens
	// stapelGroesse Folgen derselben Staffel — eine Transaktion statt
	// drei je Folge, und keine, die offen bleibt, während FFprobe läuft.
	dateien := make([]*store.MediaFile, len(hits))
	gesperrt := make([]string, len(hits))
	staffelGelesen := map[int]bool{}
	for i, h := range hits {
		if err := ctx.Err(); err != nil {
			return added, unmatched, err
		}
		g := h.guess
		// Hat jemand Staffel- oder Folgennummer von Hand gesetzt, bleibt
		// die Datei bei dieser Folge — die Nummer im Dateinamen gilt dann
		// nicht mehr (store/sperren.go).
		gesperrt[i] = s.st.GesperrteFolgeZuPfad(ctx, h.path, series.ID)
		f := &store.MediaFile{
			Path: h.path, SizeBytes: h.size, MTime: h.mtime,
			PartNo: g.PartNo, Edition: g.Edition, HDR: g.HDR,
			Container: strings.TrimPrefix(strings.ToLower(filepath.Ext(h.path)), "."),
		}
		s.fillFromProbe(ctx, f)
		dateien[i] = f
	}

	for start := 0; start < len(hits); {
		ende := start + 1
		for ende < len(hits) && ende-start < stapelGroesse &&
			hits[ende].guess.SeasonNo == hits[start].guess.SeasonNo {
			ende++
		}
		var nachher []func()
		err := s.st.Stapel(ctx, func(st *store.Store) error {
			for i := start; i < ende; i++ {
				g := hits[i].guess
				if gesperrt[i] != "" {
					dateien[i].ItemID = gesperrt[i]
					if err := s.dateiSchreiben(ctx, st, dateien[i]); err != nil {
						return err
					}
					continue
				}
				season, ok := seasons[g.SeasonNo]
				if !ok {
					season = &store.Item{
						LibraryID: lib.ID, Kind: "season", ParentID: series.ID,
						Title:    fmt.Sprintf("Staffel %d", g.SeasonNo),
						SeasonNo: g.SeasonNo, MatchState: "matched",
					}
					if err := st.UpsertItem(ctx, season); err != nil {
						return err
					}
					seasons[g.SeasonNo] = season
				}
				if !staffelGelesen[g.SeasonNo] {
					staffelGelesen[g.SeasonNo] = true
					if so, sto := serienOrdnerZu(lib.Path, hits[i].dirs, lib.Kind); so != "" {
						id, nr := season.ID, g.SeasonNo
						nachher = append(nachher, func() { lauf.staffel(ctx, id, nr, so, sto) })
					}
				}

				epTitle := fmt.Sprintf("Folge %d", g.EpisodeNo)
				if g.EpisodeTo > g.EpisodeNo {
					epTitle = fmt.Sprintf("Folge %d–%d", g.EpisodeNo, g.EpisodeTo)
				}
				// Steht ein Name in der Datei, ist er besser als „Folge 3" — bis
				// TMDb den richtigen bringt.
				if g.EpisodeTitle != "" {
					epTitle = g.EpisodeTitle
				}
				ep := folgeMitNummer(folgen[season.ID], g.EpisodeNo)
				if ep == nil {
					ep = &store.Item{
						LibraryID: lib.ID, Kind: "episode", ParentID: season.ID,
						Title: epTitle, SeasonNo: g.SeasonNo, EpisodeNo: g.EpisodeNo,
						// Tagesfolgen: Das Sendedatum ist, woran die Zuordnung sie findet.
						Premiered:  g.Datum,
						MatchState: "pending",
					}
					if err := st.UpsertItem(ctx, ep); err != nil {
						return err
					}
					folgen[season.ID] = append(folgen[season.ID], *ep)
					added++
				}
				if !g.Confident {
					unmatched++
				}
				dateien[i].ItemID = ep.ID
				if err := s.dateiSchreiben(ctx, st, dateien[i]); err != nil {
					return err
				}
				// Durchgezählt (Anime) oder Doppelfolge: Die Zuordnung muss es
				// wissen, um die richtige(n) Folge(n) bei TMDb zu holen.
				epID, pfad := ep.ID, hits[i].path
				z := store.Zusatz{Absolut: g.Absolut, BisFolge: g.EpisodeTo}
				nachher = append(nachher, func() {
					if z != s.st.ZusatzZu(ctx, epID) {
						_ = s.st.ZusatzSetzen(ctx, epID, z)
					}
					lauf.folge(ctx, epID, pfad)
				})
			}
			return nil
		})
		if err != nil {
			return added, unmatched, err
		}
		// NFO, Bilder und Zusatzangaben schreiben über die normale
		// Verbindung — erst nach dem Stapel, sonst warteten sie auf ihn.
		for _, n := range nachher {
			n()
		}
		start = ende
	}
	return added, unmatched, nil
}

var errAufteilen = errors.New("aufteilen")

// alleDateienSind: Heissen alle Dateien eines Eintrags nach ihrem eigenen
// Namen gleich?
func (s *Scanner) alleDateienSind(ctx context.Context, lib store.Library, itemID, title string) bool {
	dateien, err := s.st.FilesFor(ctx, itemID)
	if err != nil {
		return true
	}
	for _, f := range dateien {
		rel, err := filepath.Rel(lib.Path, f.Path)
		if err != nil {
			continue
		}
		if !strings.EqualFold(huebsch(s.dateiTitel(f.Path, splitDirs(rel), lib.Kind)), huebsch(title)) {
			return false
		}
	}
	return true
}

// dateienPassen prüft, ob die Dateien, die schon an einem Film hängen,
// nach ihrem eigenen Namen derselbe Film sind. Der Titel des Eintrags
// allein reicht nicht: TMDb kann ihn geändert haben, und dann landete
// „Bibi & Tina – Der Film" (2014) als zweite Fassung bei einem ganz
// anderen Film, der zufällig jetzt „Bibi & Tina" heisst.
func (s *Scanner) dateienPassen(ctx context.Context, lib store.Library, itemID, title string) bool {
	dateien, err := s.st.FilesFor(ctx, itemID)
	if err != nil || len(dateien) == 0 {
		return true
	}
	for _, f := range dateien {
		rel, err := filepath.Rel(lib.Path, f.Path)
		if err != nil {
			continue
		}
		if strings.EqualFold(huebsch(s.dateiTitel(f.Path, splitDirs(rel), lib.Kind)), huebsch(title)) {
			return true
		}
	}
	return false
}

// serienTitel wählt Titel und Jahr für eine Gruppe von Folgen. Gruppiert
// wird ohne Rücksicht auf Gross- und Kleinschreibung; „incorporated" aus
// einem Release-Ordner und „Incorporated" aus dem nächsten sind dieselbe
// Serie. Bisher entschied die erste Datei, wie sie heisst — jetzt die
// häufigste Schreibweise, und eine, die nicht ganz klein geschrieben ist,
// geht vor. Das Jahr ist das häufigste, das überhaupt eins nennt.
func serienTitel(hits []episodeHit) (string, int) {
	titel, jahre := map[string]int{}, map[int]int{}
	for _, h := range hits {
		titel[h.guess.Title]++
		if h.guess.Year > 0 {
			jahre[h.guess.Year]++
		}
	}
	bester, jahr := "", 0
	punkte := func(t string) int {
		p := titel[t] * 2
		if t != strings.ToLower(t) {
			p += 1 << 20
		}
		return p
	}
	for t := range titel {
		if bester == "" || punkte(t) > punkte(bester) || (punkte(t) == punkte(bester) && t < bester) {
			bester = t
		}
	}
	for j, n := range jahre {
		if jahr == 0 || n > jahre[jahr] || (n == jahre[jahr] && j < jahr) {
			jahr = j
		}
	}
	return bester, jahr
}

func alleNurNummer(hits []episodeHit) bool {
	for _, h := range hits {
		if !h.guess.NurNummer {
			return false
		}
	}
	return true
}

// serieZuDateien sucht die Serie, zu der diese Dateien schon gehören
// (Folge → Staffel → Serie).
//
// Gehören sie zu mehreren, gewinnt die, an der die meisten hängen — sonst
// entschied die erste Datei, und eine einzelne falsch zugeordnete Folge
// zog alle anderen mit. Eine Serie, die nie erkannt wurde und anders
// heisst als jetzt geraten, ist ein alter Fehlgriff (aus „The German"
// wurde einmal „The") — an ihr hält Lumen nicht fest. Erkannte Serien
// dagegen dürfen anders heissen: TMDb hat sie umbenannt.
func (s *Scanner) serieZuDateien(ctx context.Context, libID string, hits []episodeHit, titel string, jahr int, land string) *store.Item {
	zaehler := map[string]int{}
	serien := map[string]*store.Item{}
	// Je Datei eine Abfrage (Folge → Staffel → Serie in einem), und jede
	// Serie wird nur einmal gelesen — nicht dreimal je Folge.
	gelesen := map[string]bool{}
	for _, h := range hits {
		id := s.itemZuPfad(ctx, h.path)
		if id == "" {
			continue
		}
		sid, err := s.st.SerieZuTitel(ctx, id)
		if err != nil || sid == "" {
			continue
		}
		if !gelesen[sid] {
			gelesen[sid] = true
			if it, err := s.st.Item(ctx, sid); err == nil && it.LibraryID == libID {
				serien[sid] = it
			}
		}
		if serien[sid] != nil {
			zaehler[sid]++
		}
	}
	var beste *store.Item
	for id, n := range zaehler {
		it := serien[id]
		erkannt := s.festerTitel(ctx, it)
		if !erkannt && !strings.EqualFold(it.Title, titel) &&
			!strings.EqualFold(it.OriginalTitle, titel) {
			continue
		}
		// „Battlestar Galactica (1978)" hing früher an der Serie von 2003,
		// weil nur der Titel zählte. Jahr und Land müssen passen.
		if !s.passtZuSerie(ctx, it, jahr, land) {
			continue
		}
		if beste == nil || n > zaehler[beste.ID] || (n == zaehler[beste.ID] && id < beste.ID) {
			beste = it
		}
	}
	return beste
}

// folgeMitNummer sucht in den Folgen einer Staffel die mit dieser Nummer.
func folgeMitNummer(folgen []store.Item, no int) *store.Item {
	for i := range folgen {
		if folgen[i].EpisodeNo == no {
			c := folgen[i]
			return &c
		}
	}
	return nil
}

func mustChildren(ctx context.Context, st *store.Store, id string) []store.Item {
	c, err := st.Children(ctx, id)
	if err != nil {
		return nil
	}
	return c
}

// ---------------------------------------------------------------- Ausmessen

// fillFromProbe holt Laufzeit, Auflösung und Spuren. Scheitert FFmpeg, bleibt
// die Datei trotzdem in der Mediathek — sie lässt sich dann nur nicht
// vorsortiert umwandeln.
//
// Eine Datei, die sich seit dem letzten Mal nicht verändert hat (gleiche
// Grösse, gleiche Änderungszeit, schon ausgemessen, dieselben Untertitel
// daneben), liest Lumen nicht noch einmal: Die alten Werte bleiben, und
// die Platte darf weiterschlafen. Scheitert eine Messung, bleiben die
// alten Werte ebenso stehen — sonst stünde die Laufzeit auf 0, und der
// Film liesse sich nicht mehr abspielen.
func (s *Scanner) fillFromProbe(ctx context.Context, f *store.MediaFile) {
	alt := s.gemessen(ctx, f.Path)
	daneben := untertitelDaneben(ctx, f.Path)

	if alt != nil && alt.Gelesen && alt.Datei.SizeBytes == f.SizeBytes &&
		alt.Datei.MTime == f.MTime && alt.Datei.DurationMS > 0 &&
		gleichePfade(alt.Daneben, daneben) {
		altBehalten(f, &alt.Datei)
		return
	}

	if s.probe == nil {
		// Ohne FFmpeg bleiben wenigstens die Untertitel daneben nicht liegen.
		if alt != nil {
			altBehalten(f, &alt.Datei)
		}
		f.Spuren = SpurenFuerStore(daneben)
		f.Kapitel = []store.Chapter{}
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	p, err := s.probe.Probe(ctx, f.Path)
	if err != nil {
		// Nur der Grund, nicht der Dateiname: Das Protokoll beschreibt den
		// Server, nicht die Sammlung.
		s.st.Log(ctx, "warn", "scan", "eine Datei liess sich nicht ausmessen: "+grund(err))
		if alt != nil {
			altBehalten(f, &alt.Datei)
		}
		return
	}
	f.Spuren, f.Kapitel = SpurenFuerStore(p.Tracks), KapitelFuerStore(p.Chapters)
	f.DurationMS, f.Bitrate = p.DurationMS, p.Bitrate
	f.Width, f.Height, f.VideoCodec = p.Width, p.Height, p.VideoCodec
	if p.HDR != "" {
		f.HDR = p.HDR
	}
	if p.Container != "" {
		f.Container = p.Container
	}
}

// altBehalten übernimmt die Messwerte der letzten Messung. Spuren und
// Kapitel bleiben unangetastet in der Datenbank (nil = nicht neu schreiben).
func altBehalten(f, alt *store.MediaFile) {
	f.DurationMS, f.Bitrate = alt.DurationMS, alt.Bitrate
	f.Width, f.Height, f.VideoCodec = alt.Width, alt.Height, alt.VideoCodec
	if alt.HDR != "" {
		f.HDR = alt.HDR
	}
	if alt.Container != "" {
		f.Container = alt.Container
	}
	f.Spuren, f.Kapitel = nil, nil
}

func gleichePfade(gespeichert []string, daneben []Track) bool {
	if len(gespeichert) != len(daneben) {
		return false
	}
	jetzt := make(map[string]bool, len(daneben))
	for _, t := range daneben {
		jetzt[t.ExternalPath] = true
	}
	for _, p := range gespeichert {
		if !jetzt[p] {
			return false
		}
	}
	return true
}

// grund kürzt eine Fehlermeldung von FFmpeg auf das Technische: Pfade
// und Dateinamen fliegen raus.
func grund(err error) string {
	m := err.Error()
	if i := strings.LastIndex(m, ": "); i >= 0 && i+2 < len(m) {
		m = m[i+2:]
	}
	if strings.ContainsAny(m, "/\\") {
		return "FFmpeg konnte die Datei nicht lesen"
	}
	if len(m) > 120 {
		m = m[:120]
	}
	return m
}

// SpurenFuerStore und KapitelFuerStore übersetzen die Messung in das,
// was die Datenbank speichert.
func SpurenFuerStore(ts []Track) []store.Track {
	out := make([]store.Track, 0, len(ts))
	for _, t := range ts {
		out = append(out, store.Track{Index: t.Index, Kind: t.Kind, Codec: t.Codec,
			Language: t.Language, Title: t.Title, Channels: t.Channels, Forced: t.Forced,
			HearingImpaired: t.HearingImpaired, ExternalPath: t.ExternalPath})
	}
	return out
}

func KapitelFuerStore(cs []Chapter) []store.Chapter {
	out := make([]store.Chapter, 0, len(cs))
	for _, c := range cs {
		out = append(out, store.Chapter{Index: c.Index, StartMS: c.StartMS, EndMS: c.EndMS,
			Title: c.Title, Kind: c.Kind})
	}
	return out
}

// ---------------------------------------------------------------- Untertitel daneben

// SidecarSubtitles findet Untertitel, die neben dem Film liegen —
// „Nordlicht.de.srt", „Nordlicht.en.forced.ass" (Regeln: UntertitelDaneben).
func SidecarSubtitles(videoPath string) []Track {
	entries, err := os.ReadDir(filepath.Dir(videoPath))
	if err != nil {
		return nil
	}
	return sidecarAus(videoPath, entries)
}

// sidecarAus sucht die Untertitel in einem schon gelesenen Ordner — der
// Durchlauf liest den Ordner einer Staffel so nur einmal statt einmal je
// Folge (durchlauf.go).
func sidecarAus(videoPath string, entries []os.DirEntry) []Track {
	dir := filepath.Dir(videoPath)
	stem := strings.TrimSuffix(filepath.Base(videoPath), filepath.Ext(videoPath))

	out := []Track{}
	for _, e := range entries {
		// Nur gewöhnliche Dateien: Ein Verweis (Symlink) als „Untertitel"
		// könnte auf jede Datei des Rechners zeigen, und Lumen lieferte
		// sie aus.
		if !e.Type().IsRegular() {
			continue
		}
		n := e.Name()
		if !SubtitleExt[strings.ToLower(filepath.Ext(n))] {
			continue
		}
		if !strings.HasPrefix(n, stem) {
			continue
		}
		t, ok := UntertitelDaneben(n, stem, func(name string) bool {
			for _, x := range entries {
				if strings.EqualFold(x.Name(), name) {
					return true
				}
			}
			return false
		})
		if !ok {
			continue
		}
		t.ExternalPath = filepath.Join(dir, n)
		out = append(out, t)
	}
	return out
}

// ---------------------------------------------------------------- Hilfen

// dateiTitel: der Titel, den eine Datei nach ihrem Namen hätte — samt
// der Regel für „Film - Teil 1"/„Film - Teil 2" im selben Ordner.
func (s *Scanner) dateiTitel(path string, dirs []string, art string) string {
	return teileKlaeren(ParseFuer(path, dirs, art), path, dirs, art, nil).Title
}

func splitDirs(rel string) []string {
	rel = filepath.Dir(rel)
	if rel == "." || rel == string(filepath.Separator) {
		return nil
	}
	return strings.Split(rel, string(filepath.Separator))
}

// matchState: der Scanner hat nur geraten — ob ein Titel wirklich erkannt
// ist, entscheidet erst die Metadaten-Quelle. Auch ein wackeliger Fund
// bekommt darum eine Chance; ein Dateiname wie „Sommerhaus" ist für den
// Scanner dünn, für TMDb aber eindeutig.
func matchState(Guess) string { return "pending" }
