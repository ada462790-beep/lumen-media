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
	"strconv"
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
	Library   string `json:"library"`
	Added     int    `json:"added"`
	Updated   int    `json:"updated"`
	Unmatched int    `json:"unmatched"`
	Missing   int    `json:"missing"`
	// Renamed: Dateien, die umbenannt oder verschoben wurden und ihren
	// Titel behalten haben (umbenannt.go).
	Renamed int           `json:"renamed"`
	Took    time.Duration `json:"-"`
	TookMS  int64         `json:"tookMs"`
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

// walkDir geht über die Ordner einer Bibliothek; Tests ersetzen es, um
// Lesefehler vorzutäuschen (als root wirkt chmod 000 nicht).
var walkDir = filepath.WalkDir

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
	// ausgelassen: was der letzte Durchlauf je Bibliothek ausgelassen hat
	// (ausgelassen.go).
	ausgelassen map[string][]Auslassung
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

	// Lumen kennt nur Filme und Serien. Eine Bibliothek anderer Art (aus
	// einer früheren Fassung) liest der Durchlauf nicht als Serien.
	if lib.Kind != "movie" && lib.Kind != "series" {
		return nil, fmt.Errorf("%s: unbekannte Art %q", lib.Name, lib.Kind)
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
	aus := neuerAuslassLauf()
	unlesbar := 0
	// Wo das Lesen scheiterte: Darunter wird nichts als fehlend vermerkt.
	// Eine Freigabe, die kurz aussetzt, oder ein Ordner, dessen Rechte
	// sich geändert haben, liess sonst Hunderte Filme als „fehlt" stehen
	// — nicht mehr abspielbar bis zum nächsten Durchlauf am nächsten Tag.
	var fehlerPfade []string
	var filme []filmAuftrag

	// Verweise auf Ordner (Symlinks) folgt der Durchlauf — jedem Ziel nur
	// einmal, nie in Lumens Datenordner und nie zurück in die Bibliothek
	// selbst (das wäre eine Schleife oder doppelt). WalkDir allein folgt
	// keinem Verweis, und ein verlinkter Filmordner fehlte still.
	wurzelEcht, _ := filepath.EvalSymlinks(lib.Path)
	verfolgt := map[string]bool{}
	var besuch fs.WalkDirFunc
	besuch = func(path string, d fs.DirEntry, err error) error {
		if err == nil && d.Type()&fs.ModeSymlink != 0 {
			if ziel, e := os.Stat(path); e == nil && ziel.IsDir() {
				echt, e := filepath.EvalSymlinks(path)
				switch {
				case e != nil || InDatenordner(path) || verfolgt[echt] ||
					unterOrdner(echt, wurzelEcht) || unterOrdner(wurzelEcht, echt):
					aus.merken(AuslassVerweis, path)
				case ordnerUeberspringen(d.Name(), lib.Kind):
				default:
					verfolgt[echt] = true
					// „Verweis/" mit Schrägstrich: So liest WalkDir das Ziel,
					// und die Pfade bleiben unter dem Verweis.
					return walkDir(path+string(filepath.Separator), besuch)
				}
				return nil
			}
		}
		if err != nil {
			// Ein unlesbarer Ordner darf den ganzen Durchlauf nicht
			// abbrechen. Welcher es war, steht unter „Aufräumen", nicht
			// im Protokoll (dort stehen keine Pfade).
			aus.merken(AuslassFehler, path)
			unlesbar++
			fehlerPfade = append(fehlerPfade, path)
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() {
			if path != lib.Path && ordnerUeberspringen(d.Name(), lib.Kind) {
				if IsExtrasDir(d.Name()) && !IstSystemOrdner(d.Name()) {
					aus.merken(AuslassOrdner, path)
				}
				return filepath.SkipDir
			}
			return nil
		}
		if !IsVideo(path) || IsJunkFile(path) || VerweisInDatenordner(path, d) {
			aus.datei(path)
			return nil
		}
		rel, _ := filepath.Rel(lib.Path, path)
		dirs := splitDirs(rel)
		if !disc.nimmt(path, dirs) {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			aus.merken(AuslassFehler, path)
			unlesbar++
			fehlerPfade = append(fehlerPfade, path)
			return nil
		}
		seen[path] = true
		s.melden(lib.ID, func(f *Fortschritt) {
			f.Gefunden++
			f.Datei = filepath.Base(path)
		})

		g := teileKlaeren(ParseFuer(path, dirs, lib.Kind), path, dirs, lib.Kind, teile)
		// Umlaute aus macOS-Dateinamen (NFD) zusammensetzen: Der Titel muss
		// beim nächsten Durchlauf gleich heissen wie der gespeicherte.
		g.Title, g.EpisodeTitle = store.NFC(g.Title), store.NFC(g.EpisodeTitle)

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

		// Filme werden erst gesammelt und nach dem Durchgehen der Ordner
		// eingetragen — gemessen wird dabei mit ein paar Helfern voraus
		// (vorausMessen), geschrieben der Reihe nach.
		filme = append(filme, filmAuftrag{g: g, path: path, dirs: dirs,
			size: info.Size(), mtime: info.ModTime().Unix()})
		return nil
	}
	walkErr := walkDir(lib.Path, besuch)
	if walkErr != nil && ctx.Err() == nil {
		return nil, walkErr
	}
	// Abgebrochen (Lumen fährt herunter): Was bis hierher gesehen wurde,
	// ist nur ein Teil der Sammlung. Damit weiterzumachen hiesse, alles
	// Übrige als verschwunden zu vermerken.
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("%s: durchlauf abgebrochen: %w", lib.Name, err)
	}

	// Die Filme: FFprobe läuft mit einigen Helfern voraus (niedrige
	// Priorität), eingetragen wird der Reihe nach — die Datenbank sieht
	// nur einen Schreiber.
	skelette := make([]*store.MediaFile, len(filme))
	for i, a := range filme {
		skelette[i] = dateiSkelett(a.g, a.path, a.size, a.mtime)
	}
	gemessen := s.vorausMessen(ctx, skelette)
	for i, a := range filme {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("%s: durchlauf abgebrochen: %w", lib.Name, err)
		}
		gleich := s.unveraendert(ctx, a.path, a.size, a.mtime)
		eigen := discOrdner(a.dirs) >= 0 || lauf.eigenerOrdner(a.path, a.dirs, a.g.Title)
		added, itemID, err := s.upsertMovieGemessen(ctx, lib, a.g, a.path, a.size, a.mtime, eigen, gemessen(i))
		s.melden(lib.ID, func(f *Fortschritt) { f.Fertig++; f.Datei = filepath.Base(a.path) })
		if err != nil {
			// Ohne Dateinamen: Das Protokoll verrät keine Titel (DATENSCHUTZ.md).
			s.st.Log(ctx, "error", "scan", "eine Datei liess sich nicht eintragen: "+ohnePfad(err))
			continue
		}
		zaehlen(ctx, res, itemID, added, gleich)
		lauf.film(ctx, a.path, a.dirs, a.g)
		if eigen {
			s.ordnerZusammenlegen(ctx, lib, filepath.Dir(a.path))
		}
		if !a.g.Confident {
			res.Unmatched++
		}
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
				if added, itemID, err := s.upsertMovie(ctx, lib, g, h.path, h.size, h.mtime, false); err == nil {
					zaehlen(ctx, res, itemID, added, gleich)
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
		// Fertig zählt upsertSeries je gemessene Folge hoch — bei einer
		// Serie mit 1 000 Folgen stand der Balken sonst minutenlang still.
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
	// War die Freigabe während des Durchlaufs weg (oder ist sie es noch),
	// oder liess sich ein grosser Teil nicht lesen, ist das, was gesehen
	// wurde, kein Abbild der Sammlung. Dann wird gar nichts als fehlend
	// vermerkt, und der Durchlauf gilt nicht als erledigt: Der Hausmeister
	// versucht es nach einer Viertelstunde wieder.
	if m := merkerVon(ctx); m != nil {
		m.mu.Lock()
		unlesbar += m.lesefehler
		m.mu.Unlock()
	}
	if unterbrochen := freigabeUnterbrochen(lib.Path, unlesbar, len(seen)); unterbrochen {
		s.auslassenSetzen(lib.ID, aus)
		s.st.Log(ctx, "warn", "scan", fmt.Sprintf(
			"%s: Der Ordner war während des Durchlaufs nicht vollständig lesbar "+
				"(%d Ordner oder Dateien mit Lesefehler) — nichts als fehlend vermerkt, "+
				"neuer Versuch in einer Viertelstunde (Liste unter Aufräumen)", lib.Name, unlesbar))
		return nil, fmt.Errorf("%s: durchlauf unvollständig, %d lesefehler", lib.Name, unlesbar)
	}
	verschont := 0
	if len(seen) == 0 {
		s.st.Log(ctx, "warn", "scan", fmt.Sprintf(
			"%s: keine einzige Videodatei gefunden — nichts als verschwunden "+
				"vermerkt. Stimmt der Ordner, und ist die Platte eingehängt?", lib.Name))
	} else if gone, n, err := s.st.FehlendVermerken(ctx, lib.ID, seen, unterFehlerPfad(fehlerPfade)); err == nil {
		res.Missing = len(gone)
		verschont = n
		// Umbenannt oder verschoben: dieselbe Datei unter neuem Namen
		// behält ihren Titel samt Stand und Merkliste (umbenannt.go).
		umgezogen, titelWeg := s.umbenennungenErkennen(ctx, lib, gone)
		res.Missing -= umgezogen
		res.Added -= titelWeg
		res.Renamed = umgezogen
		// Fehlt eine Datei, deren Folge oder Film noch eine andere hat,
		// wurde sie umbenannt oder ersetzt — auch wenn das nicht in diesem
		// Durchlauf geschah oder die Grösse nicht passt. Die alte Zeile
		// geht; Stand und Haken hängen am Titel (fehlendeMitErsatz).
		if n := s.fehlendeMitErsatz(ctx, lib); n > 0 {
			s.st.Log(ctx, "info", "scan", fmt.Sprintf(
				"%s: %d fehlende Dateien ausgetragen — ihr Titel ist mit einer anderen Datei da (umbenannt oder ersetzt)", lib.Name, n))
			res.Missing = max(0, res.Missing-n)
		}
	}
	// Welche Titel keine abspielbare Datei mehr haben (fehlt_seit): Nach
	// der Frist blendet die Mediathek sie aus.
	if err := s.st.VerfuegbarkeitNachfuehren(ctx, lib.ID); err != nil {
		s.st.Log(ctx, "warn", "scan", "Verfügbarkeit liess sich nicht nachführen: "+ohnePfad(err))
	}
	if m := merkerVon(ctx); m != nil {
		m.mu.Lock()
		defekt := m.neuDefekt
		m.mu.Unlock()
		if defekt > 0 {
			s.st.Log(ctx, "warn", "scan", fmt.Sprintf(
				"%s: %d Dateien liessen sich nicht ausmessen (Liste unter Aufräumen)", lib.Name, defekt))
		}
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
	s.auslassenSetzen(lib.ID, aus)
	if unlesbar > 0 {
		s.st.Log(ctx, "warn", "scan", fmt.Sprintf(
			"%s: %d Ordner oder Dateien liessen sich nicht lesen — %d bekannte Dateien darin "+
				"bleiben, wie sie waren (Liste unter Aufräumen)", lib.Name, unlesbar, verschont))
	}
	// „Aktualisiert": Titel, deren Datei sich geändert hat, die wieder da
	// ist, die eine weitere Fassung bekommen haben oder deren NFO neu ist
	// — ohne die, die dieser Durchlauf erst angelegt hat.
	res.Updated = aktualisiertZahl(ctx)
	res.Took = time.Since(start)
	res.TookMS = res.Took.Milliseconds()
	s.ergebnisSetzen(ctx, lib, store.ScanErgebnis{
		At: time.Now(), Added: res.Added, Updated: res.Updated, Missing: res.Missing, Renamed: res.Renamed,
		Unmatched: res.Unmatched, TookMS: res.TookMS, Auto: ctx.Value(vonSelbstKey{}) == true,
	})
	s.st.Log(ctx, "info", "scan", fmt.Sprintf(
		"%s geprüft · %d neu, %d aktualisiert, %d umbenannt, %d nicht erkannt, %d fehlen · %s",
		lib.Name, res.Added, res.Updated, res.Renamed, res.Unmatched, res.Missing,
		res.Took.Round(time.Millisecond)))
	return res, nil
}

// unterOrdner: pfad ist ordner selbst oder liegt darin.
func unterOrdner(pfad, ordner string) bool {
	if pfad == "" || ordner == "" {
		return false
	}
	rel, err := filepath.Rel(ordner, pfad)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// freigabeUnterbrochen: Ist der Stammordner jetzt nicht (mehr) lesbar,
// oder scheiterte das Lesen an so vielen Stellen, dass es kein einzelner
// gesperrter Ordner mehr sein kann, war die Freigabe weg — ein
// SMB-Aussetzer, eine USB-Platte, die kurz verschwand.
func freigabeUnterbrochen(wurzel string, unlesbar, gesehen int) bool {
	if _, err := os.ReadDir(wurzel); err != nil {
		return true
	}
	if unlesbar < 20 {
		return false
	}
	// Mehr als ein Zehntel von allem, was der Durchlauf angefasst hat.
	return unlesbar*10 > unlesbar+gesehen
}

// unterFehlerPfad sagt für einen Pfad, ob er an oder unter einer Stelle
// liegt, die sich nicht lesen liess.
func unterFehlerPfad(fehler []string) func(string) bool {
	if len(fehler) == 0 {
		return nil
	}
	return func(p string) bool {
		for _, f := range fehler {
			if p == f || strings.HasPrefix(p, f+string(filepath.Separator)) {
				return true
			}
		}
		return false
	}
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
// demselben Änderungsdatum, und sie fehlte nicht. Sie zählt dann nicht als
// „geändert" — vorher meldete jedes „Prüfen" alle Filme als geändert, auch
// wenn nichts war. Eine wieder aufgetauchte Datei zählt als geändert.
func (s *Scanner) unveraendert(ctx context.Context, path string, size, mtime int64) bool {
	alt := s.gemessen(ctx, path)
	return alt != nil && !alt.Datei.Missing && alt.Datei.SizeBytes == size && alt.Datei.MTime == mtime
}

// zaehlen merkt sich für das Ergebnis, was mit dem Titel einer Datei
// geschah: neu angelegt oder (Datei geändert, wieder da, weitere Fassung)
// aktualisiert.
func zaehlen(ctx context.Context, res *Result, itemID string, added, gleich bool) {
	m := merkerVon(ctx)
	if m == nil {
		if added {
			res.Added++
		} else if !gleich {
			res.Updated++
		}
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	switch {
	case added:
		res.Added++
		m.neueTitel[itemID] = true
	case !gleich && itemID != "":
		m.aktualisiert[itemID] = true
	}
}

// aktualisiertZahl: die Titel, die dieser Durchlauf aktualisiert hat,
// ohne die, die er neu angelegt hat — samt geänderter NFO-Dateien.
func aktualisiertZahl(ctx context.Context) int {
	m := merkerVon(ctx)
	if m == nil {
		return 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for id := range m.aktualisiert {
		if !m.neueTitel[id] {
			n++
		}
	}
	return n
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
	path string, size, mtime int64, ordnerEigen bool) (added bool, itemID string, err error) {
	return s.upsertMovieGemessen(ctx, lib, g, path, size, mtime, ordnerEigen, nil)
}

// upsertMovieGemessen ist upsertMovie mit einer schon gemachten Messung
// (vorausMessen); nil = hier messen.
func (s *Scanner) upsertMovieGemessen(ctx context.Context, lib store.Library, g Guess,
	path string, size, mtime int64, ordnerEigen bool, gemessen *store.MediaFile) (added bool, itemID string, err error) {

	// Von Hand entschieden (Aufräumen): „Sind verschiedene Filme" — die
	// Datei bleibt ein eigener Film mit „Teil 2" im Titel; „Als Teile
	// zusammenfügen" — die Teilnummer von Hand gilt.
	bekannt := s.itemZuPfad(ctx, path)
	if bekannt != "" {
		if g.TitelMitTeil != "" && s.st.SperreGesetzt(ctx, bekannt, store.FeldEinzeln) {
			g.Title, g.PartNo, g.TitelMitTeil = g.TitelMitTeil, 1, ""
		}
		if s.st.SperreGesetzt(ctx, bekannt, store.FeldTeile) {
			if alt := s.gemessen(ctx, path); alt != nil && alt.Datei.PartNo > 0 {
				g.PartNo = alt.Datei.PartNo
			}
		}
	}

	// „pulp fiction" aus einem kleingeschriebenen Dateinamen wird „Pulp
	// Fiction" — bis die Metadaten den richtigen Namen bringen.
	title := huebsch(g.Title)
	if title == "" {
		title = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}

	// Ein Teil eines Films (teileKlaeren): Er gehört zum Eintrag, an dem
	// der erste Teil hängt. Unter v0.1.26 waren „Die Pfeiler der Macht -
	// Teil 1 (2016)" und „- Teil 2 (2016)" zwei Filme — dann kommt der
	// zweite Eintrag (mit Stand und Merkliste) zum ersten.
	teilZiel := ""
	if g.TitelMitTeil != "" {
		teilZiel = s.teilEintrag(ctx, lib, path, g, bekannt)
		if teilZiel != "" && bekannt != "" && teilZiel != bekannt {
			if s.nurTeileVon(ctx, lib, bekannt, title) {
				if err := s.st.FilmZusammenlegen(ctx, teilZiel, bekannt); err == nil {
					s.st.Log(ctx, "info", "scan", "Teile eines Films zu einem Eintrag zusammengelegt")
				}
			}
			bekannt = teilZiel
		}
	}

	// Kennt Lumen die Datei schon, bleibt sie bei ihrem Titel. Hat sich nur
	// die Lesart des Namens geändert — etwa „ - " jetzt als Doppelpunkt —,
	// wird der Titel nachgeführt, solange er nicht von TMDb stammt.
	if id := bekannt; id != "" {
		alt, err := s.st.Item(ctx, id)
		erkannt := err == nil && s.festerTitel(ctx, alt)
		// Ein nie erkannter Titel, an dem Dateien verschiedener Filme
		// hängen („Star Trek" mit Teil II, III und V, weil das Jahr vorne
		// stand), wird aufgeteilt: Diese Datei sucht sich ihren eigenen
		// Titel, statt den ganzen Eintrag umzubenennen.
		// Von Hand zusammengefügt („Als Teile zusammenfügen", eine Fassung
		// gewählt): Dann bleibt alles am Eintrag, und sein Name auch.
		vonHand := err == nil && (s.vonHandBestaetigt(ctx, alt) || s.st.SperreGesetzt(ctx, alt.ID, store.FeldEinzeln))
		if err == nil && !erkannt && !vonHand && g.Title != "" && alt.Title != title &&
			!s.alleDateienSind(ctx, lib, alt.ID, title) {
			err = errAufteilen
		}
		// Auch ein erkannter Titel wird aufgeteilt, wenn an ihm Dateien
		// verschiedener Filme hängen („(2016) Batman Anime - Bad Blood" und
		// „(2016) Batman vs. Superman" aus einem Sammelordner, „Die Pfeiler
		// der Macht - Teil 1" und „- Teil 2"). Bleiben darf die Datei, deren
		// Name zum erkannten Titel passt; die anderen suchen sich ihren
		// eigenen. Fassungen desselben Films heissen nach ihrem Namen
		// gleich und bleiben zusammen.
		if err == nil && erkannt && g.Title != "" && alt.Kind == "movie" &&
			s.gehoertNichtDazu(ctx, lib, alt, path, title) {
			err = errAufteilen
		}
		if err == nil && alt.LibraryID == lib.ID && alt.Kind == "movie" {
			if !erkannt && !vonHand && g.Title != "" && alt.Title != title {
				alt.Title = title
				if g.Year > 0 {
					alt.Year = g.Year
				}
				// Neu nachschlagen: unter dem alten Titel war nichts zu
				// finden, unter dem richtigen vielleicht schon.
				alt.MatchState = "pending"
				if err := s.st.UpsertItem(ctx, alt); err != nil {
					return false, "", err
				}
			}
			f := &store.MediaFile{
				ItemID: alt.ID, Path: path, SizeBytes: size, MTime: mtime,
				PartNo: g.PartNo, Edition: g.Edition, HDR: g.HDR,
				Container: strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), "."),
			}
			s.messungNehmen(ctx, f, gemessen)
			return false, alt.ID, s.dateiSchreiben(ctx, s.st, f)
		}
	}

	var item *store.Item
	if teilZiel != "" {
		if it, err := s.st.Item(ctx, teilZiel); err == nil && it.LibraryID == lib.ID && it.Kind == "movie" {
			item = it
		}
	}
	// Gehört der Ordner einem Film allein (alle Videos darin heissen nach
	// ihrem Namen gleich) und hängt dort schon eine Datei an einem Film,
	// kommt diese dazu — als Fassung oder Teil. Nach dem Titel allein ging
	// das schief, sobald die NFO der ersten Datei den Titel umbenannt
	// hatte („The Matrix" → „Matrix"): Die zweite Fassung fand ihn nicht
	// und wurde ein eigener Film.
	if item == nil && ordnerEigen {
		if ids, err := s.st.FilmeImOrdner(ctx, lib.ID, filepath.Dir(path)); err == nil && len(ids) > 0 {
			if it, err := s.st.Item(ctx, ids[0]); err == nil {
				item = it
			}
		}
	}

	// Gibt es den Titel schon? Dann hängt die Datei als weitere Fassung dran —
	// so entstehen die Doppel, die das Aufräumen später zeigt.
	existing, err := s.st.TitelGleich(ctx, lib.ID, "movie", title, false)
	if err != nil {
		return false, "", err
	}
	for i := 0; item == nil && i < len(existing); i++ {
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
			return false, "", err
		}
		added = true
	}

	f := &store.MediaFile{
		ItemID: item.ID, Path: path, SizeBytes: size, MTime: mtime,
		PartNo: g.PartNo, Edition: g.Edition, HDR: g.HDR,
		Container: strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), "."),
	}
	s.messungNehmen(ctx, f, gemessen)
	return added, item.ID, s.dateiSchreiben(ctx, s.st, f)
}

// fehlendeMitErsatz trägt fehlende Dateien aus, deren Titel eine andere,
// vorhandene Datei hat (store.FehlendeMitErsatzAustragen). Damit eine
// abgehängte Platte nicht als Umbenennung gilt, nur wenn der Ordner der
// fehlenden Datei noch da ist oder beide Dateien im selben obersten
// Ordner der Bibliothek liegen (dieselbe Serie, derselbe Filmordner).
func (s *Scanner) fehlendeMitErsatz(ctx context.Context, lib store.Library) int {
	oberster := func(p string) string {
		rel, err := filepath.Rel(lib.Path, p)
		if err != nil || strings.HasPrefix(rel, "..") {
			return "\x00" + p
		}
		teile := strings.SplitN(filepath.ToSlash(rel), "/", 2)
		if len(teile) < 2 {
			return "" // direkt in der Bibliothek
		}
		return teile[0]
	}
	n, err := s.st.FehlendeMitErsatzAustragen(ctx, lib.ID, func(fehlt, da string) bool {
		if info, err := os.Stat(filepath.Dir(fehlt)); err == nil && info.IsDir() {
			return true
		}
		o := oberster(fehlt)
		return o != "" && o == oberster(da)
	})
	if err != nil {
		s.st.Log(ctx, "warn", "scan", "fehlende Dateien liessen sich nicht austragen: "+ohnePfad(err))
	}
	return n
}

// teilEintrag: der Eintrag, an dem der erste Teil dieses Films hängt — unter
// den Dateien im selben Ordner mit derselben Basis und demselben Jahr
// (teileKlaeren) die mit der kleinsten Teilnummer, diese Datei selbst
// eingeschlossen (eigen: ihr Eintrag, "" wenn neu). Einträge, die jemand
// als „verschiedene Filme" markiert hat, zählen nicht.
func (s *Scanner) teilEintrag(ctx context.Context, lib store.Library, path string, g Guess, eigen string) string {
	rel, err := filepath.Rel(lib.Path, path)
	if err != nil {
		return ""
	}
	dirs := splitDirs(rel)
	bester, besterTeil := eigen, g.PartNo
	if eigen == "" {
		besterTeil = 1 << 30
	}
	eintraege, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		return ""
	}
	schluessel := titelSchluessel(g.Title)
	for _, e := range eintraege {
		p := filepath.Join(filepath.Dir(path), e.Name())
		if p == path || !e.Type().IsRegular() || !IsVideo(e.Name()) || IsJunkFile(e.Name()) {
			continue
		}
		x := teileKlaeren(ParseFuer(p, dirs, lib.Kind), p, dirs, lib.Kind, nil)
		if x.TitelMitTeil == "" || titelSchluessel(x.Title) != schluessel || x.Year != g.Year || x.PartNo == g.PartNo {
			continue
		}
		id := s.itemZuPfad(ctx, p)
		if id == "" || s.st.SperreGesetzt(ctx, id, store.FeldEinzeln) {
			continue
		}
		if x.PartNo < besterTeil {
			bester, besterTeil = id, x.PartNo
		}
	}
	if bester == "" || s.st.SperreGesetzt(ctx, bester, store.FeldEinzeln) {
		return ""
	}
	return bester
}

// nurTeileVon: Heissen alle Dateien des Eintrags nach ihrem Namen wie
// dieser Film (title)? Dann darf der ganze Eintrag in den des ersten Teils
// aufgehen; sonst wandert nur die eine Datei.
func (s *Scanner) nurTeileVon(ctx context.Context, lib store.Library, itemID, title string) bool {
	it, err := s.st.Item(ctx, itemID)
	if err != nil || it.Kind != "movie" || it.LibraryID != lib.ID || s.st.SperreGesetzt(ctx, itemID, store.FeldEinzeln) {
		return false
	}
	return s.alleDateienSind(ctx, lib, itemID, title)
}

// ordnerZusammenlegen heilt, was ältere Fassungen in einem Ordner, der
// einem Film allein gehört, zerlegt haben: zwei Einträge mit derselben
// TMDb-Kennung (aus der NFO) werden einer — der ältere bleibt, Stand und
// Merkliste der Leute wandern mit. Ohne gemeinsame Kennung bleibt alles,
// wie es ist; so bleibt auch eine von Hand abgetrennte Fassung getrennt.
func (s *Scanner) ordnerZusammenlegen(ctx context.Context, lib store.Library, ordner string) {
	ids, err := s.st.FilmeImOrdner(ctx, lib.ID, ordner)
	if err != nil || len(ids) < 2 {
		return
	}
	type eintrag struct {
		id, titel  string
		name, orig string // Titel und Originaltitel des Eintrags
		dateiTitel []string
		dateien    []store.MediaFile
	}
	je := map[string][]eintrag{} // tmdb → Einträge, ältester zuerst
	for _, id := range ids {
		k, _ := s.st.SourceID(ctx, id, "tmdb")
		if k == "" {
			continue
		}
		e := eintrag{id: id, titel: s.titelDerDateien(ctx, lib, id)}
		if it, err := s.st.Item(ctx, id); err == nil {
			e.name, e.orig = it.Title, it.OriginalTitle
		}
		if dateien, err := s.st.FilesFor(ctx, id); err == nil {
			e.dateien = s.mitSpuren(ctx, dateien)
			for _, f := range dateien {
				if rel, err := filepath.Rel(lib.Path, f.Path); err == nil {
					e.dateiTitel = append(e.dateiTitel, s.dateiTitel(f.Path, splitDirs(rel), lib.Kind))
				}
			}
		}
		// Gleiche Kennung genügt nicht: Eine falsche NFO oder ein falscher
		// Treffer gibt zwei Filmen dieselbe. Zusammengelegt wird nur, was
		// nach denselben Regeln wie beim Abtrennen derselbe Film ist
		// (gleicherFilm).
		zusammen := false
		for _, ziel := range je[k] {
			if !gleicherFilm(ziel.titel, e.titel, ziel.dateien, e.dateien,
				ziel.dateiTitel, e.dateiTitel, []string{ziel.name, ziel.orig}, []string{e.name, e.orig}) {
				continue
			}
			if err := s.st.FilmZusammenlegen(ctx, ziel.id, id); err == nil {
				s.st.Log(ctx, "info", "scan", "zwei Einträge desselben Films zusammengelegt (gleicher Ordner, gleiche Kennung)")
			}
			zusammen = true
			break
		}
		if !zusammen {
			je[k] = append(je[k], e)
		}
	}
}

// gleicherFilm: Zwei Einträge mit derselben Kennung sind derselbe Film,
// wenn ihre Dateien gleich heissen (titelSchluessel), wenn die Namen jeder
// Seite zum Titel oder Originaltitel der anderen passen, oder wenn es eine
// Sprachfassung ist (andere Tonsprache, Laufzeit ±5 %) — ausser die Namen
// tragen verschiedene Zahlen („Teil 1" und „Teil 2"). Dieselbe Regel wie
// abtrennenNoetig.
func gleicherFilm(titelA, titelB string, dateienA, dateienB []store.MediaFile,
	namenA, namenB []string, titelVonA, titelVonB []string) bool {
	if titelA != "" && titelA == titelB {
		return true
	}
	za, zb := "", ""
	if len(namenA) > 0 {
		za = zahlenIn(namenA[0])
	}
	if len(namenB) > 0 {
		zb = zahlenIn(namenB[0])
	}
	if za != "" && zb != "" && za != zb {
		return false
	}
	passt := func(namen, titel []string) bool {
		if len(namen) == 0 {
			return false
		}
		for _, n := range namen {
			gut := false
			for _, t := range titel {
				if t != "" && titelNah(n, t) >= 0.5 {
					gut = true
				}
			}
			if !gut {
				return false
			}
		}
		return true
	}
	if passt(namenA, titelVonB) && passt(namenB, titelVonA) {
		return true
	}
	for _, a := range dateienA {
		for _, b := range dateienB {
			if sprachfassung(a, b) {
				return true
			}
		}
	}
	return false
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
		// Gezählt werden die Folgen, nicht Serie und Staffeln dazu —
		// „2443 neu" für 2400 neue Folgen war verwirrend.
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
		series.Title = title
		series.MatchState = "pending"
		if err := s.st.UpsertItem(ctx, series); err != nil {
			return 0, 0, err
		}
	}

	// Das Land gehört zur Serie: Daran trennt der nächste Durchlauf „The
	// Office (US)" von „(UK)", und die Zuordnung sucht im richtigen Land.
	//
	// Dazu das Ordnerjahr: Unter diesem Jahr hat die Serie jetzt Folgen
	// aufgenommen. Legt TMDb später eine zweite Gruppe mit anderem Jahr
	// mit ihr zusammen, wandert das Jahr mit (store.SerienZusammenfuehren),
	// und passtZuSerie lässt die Gruppe beim nächsten Durchlauf hier.
	if z := s.st.ZusatzZu(ctx, series.ID); (land != "" && z.Land != land) ||
		(jahr > 0 && !z.JahrBekannt(jahr)) {
		if land != "" {
			z.Land = land
		}
		if jahr > 0 {
			z.Jahre = store.JahreDazu(z.Jahre, strconv.Itoa(jahr))
		}
		_ = s.st.ZusatzSetzen(ctx, series.ID, z)
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
	// Kennt Lumen eine Datei ohne Nummer schon, behält sie ihre Folge.
	// Vorher wurde bei jedem Durchlauf ab der höchsten Nummer in der
	// Datenbank weitergezählt — und die gehörte schon der Datei selbst:
	// Aus drei Folgen wurden nach sieben Durchläufen 21, 18 davon leer,
	// und der Gesehen-Haken blieb an einer leeren Folge zurück.
	bekannt := map[string]store.Item{}
	for _, eps := range folgen {
		for _, e := range eps {
			bekannt[e.ID] = e
		}
	}
	//
	// Specials ohne eigene Nummer („S08E00", „Specials/Pilot.mkv") zählen
	// ab store.SpecialOhneNummer: Mit der nächsten freien Nummer der
	// Staffel 0 kollidierten sie mit einer später dazukommenden echten
	// Datei S00E02, und die wurde eine Fassung des falschen Specials.
	// Steht ein Special noch aus einer älteren Fassung im alten Bereich,
	// bekommt die Folge selbst eine neue Nummer (umnummern) — sie behält
	// ihre Kennung, Haken und Merkliste bleiben daran.
	type umnummer struct {
		hit int
		e   store.Item
	}
	var umnummern []umnummer
	for i := range hits {
		g := &hits[i].guess
		if g.EpisodeNo != 0 || g.EpisodeTo != 0 {
			continue
		}
		if g.SeasonNo == 0 {
			g.Special = true
		}
		e, ok := bekannt[s.itemZuPfad(ctx, hits[i].path)]
		if !ok || e.Kind != "episode" || e.SeasonNo != g.SeasonNo || e.EpisodeNo <= 0 {
			continue
		}
		if g.SeasonNo == 0 && e.EpisodeNo < store.SpecialOhneNummer {
			umnummern = append(umnummern, umnummer{i, e})
			continue
		}
		g.EpisodeNo = e.EpisodeNo
	}
	for _, h := range hits {
		if h.guess.EpisodeNo > hoechste[h.guess.SeasonNo] {
			hoechste[h.guess.SeasonNo] = h.guess.EpisodeNo
		}
	}
	if hoechste[0] < store.SpecialOhneNummer-1 {
		hoechste[0] = store.SpecialOhneNummer - 1
	}
	for _, u := range umnummern {
		hoechste[0]++
		nr := hoechste[0]
		if err := s.st.FolgeUmnummern(ctx, u.e.ID, nr); err != nil {
			return added, unmatched, err
		}
		hits[u.hit].guess.EpisodeNo = nr
		if st, ok := seasons[0]; ok {
			for k := range folgen[st.ID] {
				if folgen[st.ID][k].ID == u.e.ID {
					folgen[st.ID][k].EpisodeNo = nr
				}
			}
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
	gleich := make([]bool, len(hits))
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
		gleich[i] = s.unveraendert(ctx, h.path, h.size, h.mtime)
		dateien[i] = dateiSkelett(g, h.path, h.size, h.mtime)
	}
	// Mit einigen Helfern messen (vorausMessen) — geschrieben wird danach
	// in Stapeln, der Reihe nach.
	gemessen := s.vorausMessen(ctx, dateien)
	for i := range dateien {
		gemessen(i)
		s.melden(lib.ID, func(f *Fortschritt) { f.Fertig++ })
	}
	if err := ctx.Err(); err != nil {
		return added, unmatched, err
	}

	for start := 0; start < len(hits); {
		ende := start + 1
		for ende < len(hits) && ende-start < stapelGroesse &&
			hits[ende].guess.SeasonNo == hits[start].guess.SeasonNo {
			ende++
		}
		// Vor jedem Stapel (er hält die Schreibsperre) der Bedienung den
		// Vortritt lassen (store/vorrang.go).
		store.HintergrundWarten(ctx)
		var nachher []func()
		err := s.st.Stapel(ctx, func(st *store.Store) error {
			for i := start; i < ende; i++ {
				g := hits[i].guess
				if gesperrt[i] != "" {
					dateien[i].ItemID = gesperrt[i]
					if err := s.dateiSchreiben(ctx, st, dateien[i]); err != nil {
						return err
					}
					zaehlen(ctx, &Result{}, gesperrt[i], false, gleich[i])
					continue
				}
				season, ok := seasons[g.SeasonNo]
				if !ok {
					staffelTitel := fmt.Sprintf("Staffel %d", g.SeasonNo)
					if g.SeasonNo == 0 {
						staffelTitel = "Specials"
					}
					season = &store.Item{
						LibraryID: lib.ID, Kind: "season", ParentID: series.ID,
						Title:    staffelTitel,
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
				} else if g.Special {
					// „Knight Rider 2008 - S01E00 -.avi": ein Special ohne Namen.
					epTitle = "Special"
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
					zaehlen(ctx, &Result{}, ep.ID, true, false)
				} else {
					zaehlen(ctx, &Result{}, ep.ID, false, gleich[i])
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
				// Ein Special („S08E00") trägt eine selbst vergebene Nummer;
				// die Zuordnung sucht es bei TMDb nach dem Titel.
				z := store.Zusatz{Absolut: g.Absolut, BisFolge: g.EpisodeTo, Special: g.Special}
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

// titelSchluessel macht Titel aus Dateinamen vergleichbar: klein, ohne
// Akzente, Satzzeichen und Leerraum — „Batman: Bad Blood" und
// „batman bad blood" sind gleich, „…: Teil 1" und „…: Teil 2" nicht.
func titelSchluessel(t string) string {
	t = strings.ToLower(strings.NewReplacer("ä", "ae", "ö", "oe", "ü", "ue", "Ä", "ae", "Ö", "oe", "Ü", "ue", "ß", "ss").Replace(store.NFC(t)))
	var b strings.Builder
	for _, r := range t {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r > 127 {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// dateiSchluessel: der Titel einer Datei nach ihrem eigenen Namen, als
// titelSchluessel.
func (s *Scanner) dateiSchluessel(lib store.Library, path string) string {
	rel, err := filepath.Rel(lib.Path, path)
	if err != nil {
		return ""
	}
	return titelSchluessel(s.dateiTitel(path, splitDirs(rel), lib.Kind))
}

// titelDerDateien: der gemeinsame Titel (titelSchluessel) aller Dateien
// eines Eintrags — "" wenn sie verschieden heissen oder keine da ist.
func (s *Scanner) titelDerDateien(ctx context.Context, lib store.Library, itemID string) string {
	dateien, err := s.st.FilesFor(ctx, itemID)
	if err != nil {
		return ""
	}
	titel := ""
	for _, f := range dateien {
		k := s.dateiSchluessel(lib, f.Path)
		if k == "" || (titel != "" && k != titel) {
			return ""
		}
		titel = k
	}
	return titel
}

// DateienVerschieden: Sind die Dateien eines Eintrags nach ihrem eigenen
// Namen verschiedene Filme? Für die Seite Aufräumen — dort heisst das
// „falsch zugeordnet", nicht „Fassungen". Es gilt dieselbe Regel wie beim
// Durchlauf (abtrennen): Was der Scanner zusammenlässt — Sprachfassungen
// mit gleicher Laufzeit, von Hand bestätigte Zuordnungen —, zeigt das
// Aufräumen nicht als „verschieden".
func (s *Scanner) DateienVerschieden(ctx context.Context, lib store.Library, it *store.Item, dateien []store.MediaFile) bool {
	if it == nil || len(dateien) < 2 || s.vonHandBestaetigt(ctx, it) {
		return false
	}
	dateien = s.mitSpuren(ctx, dateien)
	for _, f := range dateien {
		rel, err := filepath.Rel(lib.Path, f.Path)
		if err != nil {
			continue
		}
		if s.abtrennenNoetig(lib, it, dateien, f.Path, s.dateiTitel(f.Path, splitDirs(rel), lib.Kind)) {
			return true
		}
	}
	return false
}

// vonHandBestaetigt: Hat jemand die Zuordnung dieses Films von Hand
// bestätigt (Kennung eingegeben, eine Fassung gewählt)? Dann trennt der
// Scanner nichts davon ab (store.FeldZuordnung).
func (s *Scanner) vonHandBestaetigt(ctx context.Context, it *store.Item) bool {
	return it.MatchState == "manual" || s.st.ZuordnungGesperrt(ctx, it.ID)
}

// gehoertNichtDazu: Hängen an einem Film Dateien, die nach ihrem Namen
// verschiedene Filme sind, und ist diese Datei nicht die, deren Name zum
// Titel des Eintrags passt? Dann wird sie abgetrennt (abtrennenNoetig).
func (s *Scanner) gehoertNichtDazu(ctx context.Context, lib store.Library, it *store.Item, path, title string) bool {
	if s.vonHandBestaetigt(ctx, it) {
		return false
	}
	dateien, err := s.st.FilesFor(ctx, it.ID)
	if err != nil || len(dateien) < 2 {
		return false
	}
	return s.abtrennenNoetig(lib, it, s.mitSpuren(ctx, dateien), path, title)
}

// abtrennenNoetig entscheidet für die Datei path (Titel nach ihrem Namen:
// title) unter den Dateien eines Eintrags. Welche bleibt: die mit dem
// Namen, der dem Titel (oder Originaltitel) des Eintrags am ähnlichsten
// ist (gemeinsame Wörter); bei Gleichstand die mit dem kleinsten Pfad —
// damit die Entscheidung bei jedem Durchlauf gleich ausfällt.
//
// Verschieden benannte Dateien sind nur dann Fassungen desselben Films,
// wenn
//
//   - ihr Name selbst zum Titel oder Originaltitel passt („Sag niemals
//     nie" und „Never Say Never Again"), oder
//   - sie eine andere Tonsprache haben (die erste Tonspur, wie gemessen)
//     und die Laufzeit passt (±5 %, wegen der PAL-Beschleunigung) —
//     „Chihiros Reise ins Zauberland" (deutsch) und „Spirited Away"
//     (englisch).
//
// Gleiche Sprache und verschiedene Titel sind verschiedene Filme, auch
// wenn sie gleich lang sind: „007 - Octopussy" und „007 - Sag niemals
// nie" (beide 1983, beide gut zwei Stunden) standen als ein Film da.
// Zahlen, die in beiden Namen vorkommen („007", das Jahr), zählen dabei
// nicht als Ähnlichkeit. Verschiedene Zahlen („Teil 1"/„Teil 2") sind
// immer verschiedene Filme — die Teile eines Films erkennt schon
// teileKlaeren, dann heissen beide Dateien gleich.
func (s *Scanner) abtrennenNoetig(lib store.Library, it *store.Item, dateien []store.MediaFile, path, title string) bool {
	if len(dateien) < 2 {
		return false
	}
	dateien = append([]store.MediaFile(nil), dateien...)
	sort.Slice(dateien, func(i, j int) bool { return dateien[i].Path < dateien[j].Path })
	eigen := titelSchluessel(title)
	titelZu := map[string]string{}           // Schlüssel → Titel, wie gelesen
	andere := map[string][]store.MediaFile{} // Schlüssel → die übrigen Dateien
	var selbst store.MediaFile
	reihe := []string{}
	for _, f := range dateien {
		t := title
		if f.Path != path {
			rel, err := filepath.Rel(lib.Path, f.Path)
			if err != nil {
				continue
			}
			t = s.dateiTitel(f.Path, splitDirs(rel), lib.Kind)
		} else {
			selbst = f
		}
		k := titelSchluessel(t)
		if k == "" {
			continue
		}
		if _, ok := titelZu[k]; !ok {
			titelZu[k] = t
			reihe = append(reihe, k)
		}
		if f.Path != path {
			andere[k] = append(andere[k], f)
		}
	}
	if len(reihe) < 2 || titelZu[eigen] == "" {
		return false
	}
	punkte := func(k string) float64 {
		return max(titelNah(titelZu[k], it.Title), titelNah(titelZu[k], it.OriginalTitle))
	}
	haupt := reihe[0]
	for _, k := range reihe[1:] {
		if punkte(k) > punkte(haupt) {
			haupt = k
		}
	}
	if eigen == haupt {
		return false
	}
	ze, zh := zahlenIn(titelZu[eigen]), zahlenIn(titelZu[haupt])
	// „Teil 1" und „Teil 2", „Dune: Part One" und „Part Two": verschiedene
	// Zahlen im Namen sind verschiedene Filme — auch bei gleicher Laufzeit
	// und auch wenn beide gut zum Titel passen.
	if ze != "" && zh != "" && ze != zh {
		return true
	}
	// Eine andere Sprache, gleich lang: dieselbe Fassung, synchronisiert.
	for _, d := range andere[haupt] {
		if sprachfassung(selbst, d) {
			return false
		}
	}
	// „Dune" neben „Dune 2": Zahl nur auf einer Seite und keine
	// Sprachfassung — verschiedene Filme.
	if ze != zh {
		return true
	}
	// Passt der eigene Name selbst gut zum Titel oder Originaltitel des
	// Eintrags (ein deutscher und ein englischer Dateiname, ein Szene-Name
	// mit Ausgabe), bleibt die Datei.
	return punkte(eigen) < 0.5
}

// sprachfassung: Zwei Dateien sind dieselbe Fassung in verschiedenen
// Sprachen — die erste Tonspur hat je eine bekannte, verschiedene
// Sprache, und die Laufzeiten liegen höchstens 5 % auseinander (PAL
// läuft mit 25 statt 23,976 Bildern und ist gut 4 % kürzer).
func sprachfassung(a, b store.MediaFile) bool {
	sa, sb := tonSprache(a), tonSprache(b)
	return sa != "" && sb != "" && sa != sb && laufzeitNah(a.DurationMS, b.DurationMS)
}

// tonSprache: die Sprache der ersten Tonspur ("" = unbekannt oder nicht
// gemessen).
func tonSprache(f store.MediaFile) string {
	erste := -1
	sprache := ""
	for _, t := range f.Spuren {
		if t.Kind != "audio" || t.External || (erste >= 0 && t.Index >= erste) {
			continue
		}
		erste, sprache = t.Index, strings.ToLower(strings.TrimSpace(t.Language))
	}
	if sprache == "und" || sprache == "unk" || sprache == "mis" || sprache == "zxx" {
		return ""
	}
	if len(sprache) > 2 {
		if k, ok := untertitelSprachen[sprache]; ok {
			sprache = k
		}
	}
	return sprache
}

// mitSpuren lädt die Spuren der Dateien nach, die noch keine dabeihaben
// (FilesFor liest sie nicht mit) — für tonSprache.
func (s *Scanner) mitSpuren(ctx context.Context, dateien []store.MediaFile) []store.MediaFile {
	out := append([]store.MediaFile(nil), dateien...)
	for i := range out {
		if out[i].Spuren == nil && out[i].ID != "" {
			if sp, err := s.st.Spuren(ctx, out[i].ID); err == nil {
				out[i].Spuren = sp
			}
		}
	}
	return out
}

// laufzeitGleich: beide Laufzeiten bekannt und höchstens 2 % auseinander.
func laufzeitGleich(a, b int64) bool {
	if a <= 0 || b <= 0 {
		return false
	}
	d := a - b
	if d < 0 {
		d = -d
	}
	return d*50 <= max(a, b)
}

// laufzeitNah: beide Laufzeiten bekannt und höchstens 5 % auseinander.
func laufzeitNah(a, b int64) bool {
	if a <= 0 || b <= 0 {
		return false
	}
	d := a - b
	if d < 0 {
		d = -d
	}
	return d*20 <= max(a, b)
}

// titelNah: wie wortAehnlich, aber ohne reine Zahlen — „007" und „1983"
// in beiden Namen machen „Octopussy" und „Sag niemals nie" nicht ähnlich.
func titelNah(a, b string) float64 {
	ohne := func(s string) string {
		var w []string
		for _, x := range strings.FieldsFunc(s, func(r rune) bool {
			return !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') && !(r >= '0' && r <= '9') && r <= 127
		}) {
			if strings.Trim(x, "0123456789") != "" {
				w = append(w, x)
			}
		}
		return strings.Join(w, " ")
	}
	return wortAehnlich(ohne(a), ohne(b))
}

// zahlenIn: die Zahlen in einem Titel, in Reihenfolge — „Teil 2 (2016)"
// ergibt „2 2016". Zahlwörter hinter „Part", „Teil", „Chapter",
// „Kapitel", „Volume" („Dune: Part One", „Teil zwei") und römische Zahlen
// ab II („Star Trek II", „Rocky IV") zählen als Zahl. Ein einzelnes „I",
// „V" oder „X" nicht — „I, Robot", „V wie Vendetta", „X-Men".
func zahlenIn(s string) string {
	var out []string
	woerter := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') && r <= 127
	})
	for i, w := range woerter {
		// Ziffern, auch mitten im Wort („part2", „cd1").
		for _, z := range strings.FieldsFunc(w, func(r rune) bool { return r < '0' || r > '9' }) {
			out = append(out, strings.TrimLeft(z, "0"))
		}
		if n, ok := roemisch[w]; ok && n > 1 && w != "v" && w != "x" {
			out = append(out, strconv.Itoa(n))
			continue
		}
		if i == 0 || !zahlVorwort[woerter[i-1]] {
			continue
		}
		if n, ok := zahlWort[w]; ok {
			out = append(out, strconv.Itoa(n))
		} else if n, ok := roemisch[w]; ok && n <= 1 {
			out = append(out, strconv.Itoa(n)) // „Part I"
		}
	}
	return strings.Join(out, " ")
}

var zahlVorwort = map[string]bool{"part": true, "teil": true, "chapter": true,
	"kapitel": true, "volume": true, "vol": true, "episode": true}

var zahlWort = map[string]int{
	"one": 1, "two": 2, "three": 3, "four": 4, "five": 5, "six": 6, "seven": 7, "eight": 8, "nine": 9, "ten": 10,
	"eins": 1, "zwei": 2, "drei": 3, "vier": 4, "fuenf": 5, "fünf": 5, "sechs": 6, "sieben": 7, "acht": 8, "neun": 9, "zehn": 10,
	"i": 1, "v": 5, "x": 10,
}

var roemisch = map[string]int{"i": 1, "ii": 2, "iii": 3, "iv": 4, "v": 5, "vi": 6,
	"vii": 7, "viii": 8, "ix": 9, "x": 10}

// wortAehnlich: Anteil gemeinsamer Wörter zweier Titel (0 bis 1).
func wortAehnlich(a, b string) float64 {
	woerter := func(s string) map[string]bool {
		m := map[string]bool{}
		for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
			return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') && r <= 127
		}) {
			m[titelSchluessel(w)] = true
		}
		delete(m, "")
		return m
	}
	wa, wb := woerter(a), woerter(b)
	if len(wa) == 0 || len(wb) == 0 {
		return 0
	}
	gemeinsam := 0
	for w := range wa {
		if wb[w] {
			gemeinsam++
		}
	}
	return float64(gemeinsam) / float64(len(wa)+len(wb)-gemeinsam)
}

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

	// Schon als defekt bekannt und unverändert: nicht bei jedem Durchlauf
	// neu messen (und neu ins Protokoll schreiben) — erst, wenn sich die
	// Datei ändert (fertig kopiert, ersetzt) oder die Rechte stimmen.
	// Das muss vor dem Fall „unverändert und gemessen" stehen: Hatte die
	// Datei vor dem Defekt eine gültige Messung, griff sonst jener, und
	// der Vermerk „defekt" ging beim nächsten Durchlauf verloren — die
	// Datei galt wieder als abspielbar, und das Vorbereiten im
	// Hintergrund scheiterte daran nach jedem Neustart von Neuem.
	if alt != nil && alt.Datei.Defekt != "" && alt.Datei.SizeBytes == f.SizeBytes &&
		alt.Datei.MTime == f.MTime && (alt.Datei.Defekt != DefektGesperrt || !lesbar(f.Path)) {
		altBehalten(f, &alt.Datei)
		f.Defekt = alt.Datei.Defekt
		return
	}
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
	// Wird gerade umgewandelt, hat die Wiedergabe Vorrang: vor jeder
	// Messung eine kurze Pause (nur vor echten Messungen — eine
	// unveränderte Datei kostet nichts).
	if s.Bremse != nil && s.Bremse() {
		select {
		case <-time.After(400 * time.Millisecond):
		case <-ctx.Done():
			return
		}
	}
	// Und wer gerade eine Seite öffnet, auch (store/vorrang.go): Eine
	// Messung kostet auf einer kleinen NAS spürbar Rechenzeit.
	store.HintergrundWarten(ctx)
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	p, err := s.probe.Probe(ctx, f.Path)
	if err != nil {
		if ctx.Err() != nil {
			// Abgebrochen (Lumen fährt herunter, Zeit um): kein Urteil
			// über die Datei.
			if alt != nil {
				altBehalten(f, &alt.Datei)
				f.Defekt = alt.Datei.Defekt
			}
			return
		}
		// Ist die Datei gerade gar nicht da (die Freigabe setzt aus,
		// gelöscht zwischen Finden und Messen), ist sie nicht defekt: alte
		// Werte behalten, als Lesefehler zählen (freigabeUnterbrochen).
		if _, e := os.Stat(f.Path); e != nil {
			if alt != nil {
				altBehalten(f, &alt.Datei)
				f.Defekt = alt.Datei.Defekt
			}
			if m := merkerVon(ctx); m != nil {
				m.mu.Lock()
				m.lesefehler++
				m.mu.Unlock()
			}
			return
		}
		// Die Datei kommt unter Aufräumen (mit Pfad, nur für Verwalter)
		// und wird nicht als abspielbar angeboten. Ins Protokoll kommt
		// am Ende des Durchlaufs nur die Zahl (keine Titel, DATENSCHUTZ.md).
		f.Defekt = defektGrund(f)
		if m := merkerVon(ctx); m != nil {
			m.mu.Lock()
			m.neuDefekt++
			m.mu.Unlock()
		} else {
			s.st.Log(ctx, "warn", "scan", "eine Datei liess sich nicht ausmessen: "+grund(err))
		}
		// Hatte sie früher eine gültige Messung (etwa nur kurz nicht
		// lesbar), bleibt die stehen — abspielen lässt sie sich erst, wenn
		// eine neue Messung gelingt.
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

// Gründe, warum eine Datei defekt ist (store.MediaFile.Defekt).
const (
	DefektLeer     = "leer"     // 0 Byte (oder noch beim Kopieren)
	DefektGesperrt = "gesperrt" // keine Leserechte
	DefektKaputt   = "kaputt"   // FFmpeg erkennt nichts darin
)

// defektGrund: warum sich eine Datei nicht ausmessen liess.
func defektGrund(f *store.MediaFile) string {
	switch {
	case f.SizeBytes == 0:
		return DefektLeer
	case !lesbar(f.Path):
		return DefektGesperrt
	}
	return DefektKaputt
}

// lesbar: Lumen darf die Datei öffnen. (Ein lstat beim Durchlaufen gelingt
// auch ohne Leserecht — darum zählte eine Datei mit chmod 000 früher
// nicht als unlesbar.)
func lesbar(pfad string) bool {
	h, err := os.Open(pfad)
	if err != nil {
		return false
	}
	h.Close()
	return true
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
	return store.NFC(teileKlaeren(ParseFuer(path, dirs, art), path, dirs, art, nil).Title)
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

// ohnePfad gibt eine Fehlermeldung fürs Protokoll weiter — aber nur, wenn
// kein Pfad darin steckt; der verriete den Titel.
func ohnePfad(err error) string {
	m := err.Error()
	if strings.ContainsAny(m, `/\`) {
		return "…"
	}
	return m
}
