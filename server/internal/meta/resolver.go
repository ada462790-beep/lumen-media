// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package meta

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"lumen/internal/ablage"
	"lumen/internal/store"
)

// Resolver bringt die Treffer in die Mediathek.
//
// Er geht die Titel durch, die der Scanner hinterlassen hat, fragt die
// Quellen und schreibt das Ergebnis zurück. Was er nicht sicher zuordnen
// kann, markiert er als „nicht erkannt" — das landet in der Verwaltung
// unter „Aufräumen", wo der Besitzer es in Ruhe richtigstellt.
// Stand eines laufenden Metadaten-Durchlaufs — für die Verwaltung, damit
// dort nicht nur „läuft" steht.
type Stand struct {
	Laeuft  bool   `json:"laeuft"`
	Library string `json:"library,omitempty"`
	Gesamt  int    `json:"gesamt"`
	Fertig  int    `json:"fertig"`
	Titel   string `json:"titel,omitempty"`
}

type Resolver struct {
	standMu sync.Mutex
	stand   Stand
	// schluesselOK: wann TMDb den Schlüssel zuletzt angenommen hat
	// (schluesselPruefen) — geschützt durch standMu.
	schluesselOK time.Time

	st      *store.Store
	tmdb    *TMDb
	omdb    *OMDb
	artDir  string
	http    *http.Client
	useIMDb bool
}

type Config struct {
	TMDbKey  string
	OMDbKey  string
	Language string
	Region   string
	UseIMDb  bool

	// Abweichende Adressen — für einen eigenen Spiegel im Netz und für
	// Tests, die nicht von einem fremden Dienst abhängen sollen.
	TMDbURL    string
	TMDbImgURL string
	OMDbURL    string
}

func NewResolver(st *store.Store, dataDir string, cfg Config) (*Resolver, error) {
	artDir := filepath.Join(dataDir, "bilder")
	if err := os.MkdirAll(artDir, 0o755); err != nil {
		return nil, err
	}
	r := &Resolver{st: st, artDir: artDir, useIMDb: cfg.UseIMDb,
		http: &http.Client{Timeout: 30 * time.Second}}
	if cfg.TMDbKey != "" {
		r.tmdb = NewTMDb(cfg.TMDbKey, cfg.Language, cfg.Region)
		if cfg.TMDbURL != "" {
			r.tmdb.BaseURL = cfg.TMDbURL
		}
		if cfg.TMDbImgURL != "" {
			r.tmdb.ImageURL = cfg.TMDbImgURL
		}
	}
	if cfg.OMDbKey != "" {
		r.omdb = NewOMDb(cfg.OMDbKey)
		if cfg.OMDbURL != "" {
			r.omdb.BaseURL = cfg.OMDbURL
		}
	}
	return r, nil
}

// TMDbPruefen fragt TMDb einmal, ob der Schlüssel angenommen wird. Nur
// ErrTMDbSchluessel heisst „falsch"; ist TMDb gerade nicht erreichbar,
// gibt es keinen Grund, den Schlüssel abzulehnen.
func (r *Resolver) TMDbPruefen(ctx context.Context) error {
	if r.tmdb == nil {
		return nil
	}
	var antwort map[string]any
	if err := r.tmdb.getEinmal(ctx, "/authentication", url.Values{}, &antwort); errors.Is(err, ErrTMDbSchluessel) {
		return err
	}
	return nil
}

// Ready sagt, ob überhaupt eine Quelle eingerichtet ist. Ohne läuft Lumen
// weiter — nur eben mit Dateinamen statt mit Beschreibungen.
func (r *Resolver) Ready() bool { return r.tmdb != nil }

// Result ist, was ein Durchlauf gebracht hat.
type Result struct {
	Checked   int `json:"checked"`
	Matched   int `json:"matched"`
	Unmatched int `json:"unmatched"`
	Artwork   int `json:"artwork"`
	Failed    int `json:"failed"`
}

// ResolvePending arbeitet alle Titel ab, die noch keine Metadaten haben.
// Der Aufruf kann lange dauern; er bricht sauber ab, wenn der Zusammenhang
// endet — beim Herunterfahren etwa.
func (r *Resolver) ResolvePending(ctx context.Context, libraryID string, limit int) (*Result, error) {
	defer r.ReihenBilderHolen(ctx)
	if !r.Ready() {
		return nil, fmt.Errorf("es ist keine Metadaten-Quelle eingerichtet")
	}
	// Ein abgelehnter Schlüssel: einmal sagen, nicht je Titel.
	if err := r.schluesselPruefen(ctx); err != nil {
		return &Result{}, err
	}
	r.einmalNeuVersuchen(ctx)
	pending, err := r.st.PendingItems(ctx, libraryID, limit)
	if err != nil {
		return nil, err
	}
	// Folgen kommen erst in die Liste, wenn ihre Serie zugeordnet ist. Wurde
	// in diesem Häppchen eine Serie zugeordnet, holen wir ihre Folgen gleich
	// mit — sonst erst beim nächsten Durchlauf.
	mitSerien := false
	for i := range pending {
		mitSerien = mitSerien || pending[i].Kind == "series"
	}

	r.standMu.Lock()
	r.stand = Stand{Laeuft: true, Library: libraryID, Gesamt: len(pending)}
	r.standMu.Unlock()
	defer func() {
		r.standMu.Lock()
		r.stand = Stand{}
		r.standMu.Unlock()
	}()

	res := &Result{}
	for i := 0; i < len(pending); i++ { // wächst unterwegs (Folgen frisch zugeordneter Serien)
		if ctx.Err() != nil {
			return res, ctx.Err()
		}
		// Wer gerade eine Seite öffnet, geht vor (store/vorrang.go).
		store.HintergrundWarten(ctx)
		r.standMu.Lock()
		r.stand.Fertig, r.stand.Titel = i, pending[i].Title
		r.standMu.Unlock()
		res.Checked++
		err := r.resolveOne(ctx, &pending[i])
		if errors.Is(err, ErrTMDbSchluessel) {
			// Mitten im Durchgang abgelehnt (Schlüssel widerrufen): Jeder
			// weitere Titel bekäme dieselbe Antwort.
			grundMerken(pending[i].ID, Grund{Art: "schluessel"})
			r.schluesselFehlt(ctx)
			res.Failed++
			return res, err
		}
		switch {
		case err == errUnmatched:
			res.Unmatched++
		case err != nil:
			res.Failed++
			r.st.Log(ctx, "warn", "metadaten",
				fmt.Sprintf("%s: %v", pending[i].Title, err))
		default:
			res.Matched++
		}
		if i == len(pending)-1 && mitSerien && len(pending) < limit {
			mitSerien = false
			if mehr, err := r.st.PendingItems(ctx, libraryID, limit); err == nil {
				// Nur Neues: Was eben schon versucht wurde (etwa ohne
				// Internet gescheitert), kommt nicht gleich nochmal dran.
				schon := make(map[string]bool, len(pending))
				for _, p := range pending {
					schon[p.ID] = true
				}
				for _, m := range mehr {
					if !schon[m.ID] && len(pending) < limit {
						pending = append(pending, m)
					}
				}
				r.standMu.Lock()
				r.stand.Gesamt = len(pending)
				r.standMu.Unlock()
			}
		}
	}
	r.st.Log(ctx, "info", "metadaten", fmt.Sprintf(
		"%d Titel geprüft · %d zugeordnet, %d nicht erkannt, %d fehlgeschlagen",
		res.Checked, res.Matched, res.Unmatched, res.Failed))
	return res, nil
}

var errUnmatched = fmt.Errorf("nicht sicher zuzuordnen")

// abgleichStand zählt die Fassungen der Zuordnung. Steigt er, kommen alle
// bisher nicht erkannten Filme und Serien einmal neu dran — die bessere
// Zuordnung (abgleich.go, Kennungen im Namen) soll auch für sie gelten.
const (
	abgleichStand       = "2"
	einstellungAbgleich = "meta.abgleich_stand"
)

func (r *Resolver) einmalNeuVersuchen(ctx context.Context) {
	if r.st.Setting(ctx, einstellungAbgleich, "1") == abgleichStand {
		return
	}
	if n, err := r.st.NichtErkannteZuruecksetzen(ctx); err == nil {
		_ = r.st.SetSetting(ctx, einstellungAbgleich, abgleichStand)
		if n > 0 {
			r.st.Log(ctx, "info", "metadaten", fmt.Sprintf(
				"%d bisher nicht erkannte Titel werden mit der verbesserten Zuordnung neu versucht", n))
		}
	}
}

func (r *Resolver) resolveOne(ctx context.Context, it *store.Item) error {
	switch it.Kind {
	case "movie":
		return r.resolveMovie(ctx, it)
	case "series":
		return r.resolveSeries(ctx, it)
	case "episode":
		return r.resolveEpisode(ctx, it)
	}
	return nil
}

// suchen fragt TMDb mit dem Jahr aus Datei- oder Ordnername und ohne.
//
// Mit Jahr allein ginge ein Film verloren, dessen Datei ein Jahr daneben
// liegt (Kinostart gegen Heimkino) — TMDb filtert streng. Ohne Jahr allein
// käme bei „Doctor Who (2005)" die Serie aus 1963 mit gleichem Namen
// gleichauf. Darum beides, die Treffer mit passendem Jahr zuerst; doppelte
// fallen weg. Das Jahr entscheidet danach in Best mit.
func (r *Resolver) suchen(ctx context.Context, kind, titel string, jahr int) ([]Candidate, error) {
	such := func(j int) ([]Candidate, error) {
		if kind == "series" {
			return r.tmdb.SearchSeriesJahr(ctx, titel, j)
		}
		return r.tmdb.SearchMovie(ctx, titel, j)
	}
	if jahr <= 0 {
		return such(0)
	}
	mit, err := such(jahr)
	if err != nil {
		return nil, err
	}
	ohne, err := such(0)
	if err != nil {
		// Die Suche mit Jahr hat geklappt — damit lässt sich arbeiten.
		return mit, nil
	}
	gesehen := make(map[string]bool, len(mit))
	for _, c := range mit {
		gesehen[c.SourceID] = true
	}
	for _, c := range ohne {
		if !gesehen[c.SourceID] {
			gesehen[c.SourceID] = true
			mit = append(mit, c)
		}
	}
	return mit, nil
}

func (r *Resolver) resolveMovie(ctx context.Context, it *store.Item) error {
	// Nennt eine NFO neben der Datei die Kennung, wird nicht gesucht.
	full, quelle := r.lokalerTreffer(ctx, it, "movie")
	if full == nil {
		// Eine Kennung im Datei- oder Ordnernamen („{imdb-tt0133093}").
		full, quelle = r.dateiTreffer(ctx, it, "movie")
	}
	if full == nil {
		// Ohne Schlüssel eingegeben oder aus Emby übernommen.
		full, quelle = r.vorgemerkteKennung(ctx, it, "movie")
	}
	if full == nil {
		m, gesucht, _, err := r.finden(ctx, "movie", it.Title, it.Year, "")
		if err != nil {
			// Kein Netz, TMDb bremst: bleibt „ausstehend" und kommt beim
			// nächsten Durchgang wieder dran.
			grundMerken(it.ID, Grund{Art: "fehler", Gesucht: gesucht.Texte, Anfragen: gesucht.Anfragen})
			return err
		}
		if !m.Sure {
			grundMerken(it.ID, grundAus(m, gesucht))
			return r.markUnmatched(ctx, it, m)
		}
		quelle = m.Candidate.SourceID

		// Die Suche liefert nicht alles — Laufzeit, Genres und Altersfreigabe
		// kommen erst mit den Einzelheiten.
		full, err = r.tmdb.Details(ctx, m.Candidate.SourceID)
		if err != nil {
			full = m.Candidate
		}
	}
	r.apply(it, full)

	tomaten := -2 // nicht gefragt
	if r.useIMDb && r.omdb != nil && full.IMDbID != "" {
		if w, err := r.omdb.Wertungen(ctx, full.IMDbID); err == nil {
			it.IMDbRating, it.IMDbVotes, tomaten = w.IMDb, w.Stimmen, w.Tomaten
		}
		// Eine fehlende Wertung ist kein Grund, den ganzen Titel zu verwerfen.
	}

	r.lokalUeberlagern(ctx, it)
	it.MatchState = "matched"
	if err := r.st.UpsertItem(ctx, it); err != nil {
		return err
	}
	grundVergessen(it.ID)
	r.vormerkungErledigt(ctx, it.ID)
	if tomaten > -2 {
		_ = r.st.TomatenSetzen(ctx, it.ID, tomaten)
	}
	// Für die Links auf der Titelseite (TMDb, Trakt).
	_ = r.st.SetSourceID(ctx, it.ID, "tmdb", quelle)
	// Die Reihe ist eine Zugabe. Scheitert sie, ist der Film trotzdem
	// erkannt — und sein Poster soll trotzdem kommen.
	if full.ReiheID != "" {
		_ = r.st.TMDbReiheZuordnen(ctx, full.ReiheID, full.ReiheName, it.ID, it.Year)
	}
	return r.saveArtwork(ctx, it.ID, full)
}

func (r *Resolver) resolveSeries(ctx context.Context, it *store.Item) error {
	full, quelle := r.lokalerTreffer(ctx, it, "series")
	if full == nil {
		full, quelle = r.dateiTreffer(ctx, it, "series")
	}
	if full == nil {
		// Ohne Schlüssel eingegeben oder aus Emby übernommen.
		full, quelle = r.vorgemerkteKennung(ctx, it, "series")
	}
	if full == nil {
		// „The Office (US)": nur die Kandidaten aus diesem Land.
		m, gesucht, _, err := r.finden(ctx, "series", it.Title, it.Year, r.st.ZusatzZu(ctx, it.ID).Land)
		if err != nil {
			grundMerken(it.ID, Grund{Art: "fehler", Gesucht: gesucht.Texte, Anfragen: gesucht.Anfragen})
			return err
		}
		if !m.Sure {
			grundMerken(it.ID, grundAus(m, gesucht))
			return r.markUnmatched(ctx, it, m)
		}
		quelle = m.Candidate.SourceID
		// Die Suche kennt keine Besetzung und keine Genres; die kommen mit
		// den Einzelheiten.
		full, err = r.tmdb.SeriesDetails(ctx, m.Candidate.SourceID)
		if err != nil {
			full = m.Candidate
		}
	}
	r.apply(it, full)
	tomaten := r.wertungenHolen(ctx, it, full.IMDbID)
	r.lokalUeberlagern(ctx, it)
	it.MatchState = "matched"
	if err := r.st.UpsertItem(ctx, it); err != nil {
		return err
	}
	grundVergessen(it.ID)
	r.vormerkungErledigt(ctx, it.ID)
	if tomaten > -2 {
		_ = r.st.TomatenSetzen(ctx, it.ID, tomaten)
	}
	// Die Kennung merken, damit die Folgen sie finden.
	if err := r.st.SetSourceID(ctx, it.ID, "tmdb", quelle); err != nil {
		return err
	}
	if full.TVDBID != "" {
		_ = r.st.SetSourceID(ctx, it.ID, "tvdb", full.TVDBID)
	}
	if full.ZeitraumBekannt {
		_ = r.st.SerienZeitraum(ctx, it.ID, full.EndYear, full.LaeuftNoch)
	}
	return r.saveArtwork(ctx, it.ID, full)
}

func (r *Resolver) resolveEpisode(ctx context.Context, it *store.Item) error {
	// Eine Folge hängt an einer Staffel, die Staffel an der Serie — und nur
	// die Serie kennt die Kennung der Quelle.
	seriesID, err := r.st.SeriesOf(ctx, it.ID)
	if err != nil {
		return err
	}
	src, err := r.st.SourceID(ctx, seriesID, "tmdb")
	if err != nil || src == "" {
		// Die Serie ist selbst noch nicht zugeordnet — beim nächsten
		// Durchlauf noch einmal.
		return errUnmatched
	}

	// Durchgezählte Folgen („One Piece - 1071") und Tagesfolgen
	// („Tagesschau 2024-03-15") stehen bei TMDb unter anderer Staffel und
	// Nummer; Doppelfolgen („S01E16E17") holen die zweite Folge mit.
	zusatz := r.st.ZusatzZu(ctx, it.ID)
	var e *Candidate
	switch {
	case zusatz.Absolut:
		e, _, _, err = r.absolutAufloesen(ctx, src, it.EpisodeNo)
	case it.SeasonNo >= 1900 && len(it.Premiered) >= 10:
		e, _, _, err = r.datumAufloesen(ctx, src, it.Premiered[:10])
	default:
		// Über die ganze Staffel: eine Anfrage für alle ihre Folgen
		// (staffel_speicher.go) statt einer je Folge.
		e, err = r.tmdb.FolgeAusStaffel(ctx, src, it.SeasonNo, it.EpisodeNo)
		if err == nil && zusatz.BisFolge > it.EpisodeNo {
			r.doppelfolgeErgaenzen(ctx, src, it.SeasonNo, it.EpisodeNo, zusatz.BisFolge, e)
		}
	}
	if err != nil {
		if strings.Contains(err.Error(), "nicht gefunden") {
			// Diese Folge kennt die Quelle nicht — kein Fehler, nur nichts
			// zu holen. Der Dateiname bleibt stehen.
			it.MatchState = "unmatched"
			return r.st.UpsertItem(ctx, it)
		}
		return err
	}
	if e.Title != "" {
		it.Title = e.Title
	}
	it.Overview, it.Premiered = e.Overview, e.Premiered
	if e.RuntimeMS > 0 {
		it.RuntimeMS = e.RuntimeMS
	}
	r.lokalUeberlagern(ctx, it)
	it.MatchState = "matched"
	if err := r.st.UpsertItem(ctx, it); err != nil {
		return err
	}
	return r.saveArtwork(ctx, it.ID, e)
}

func (r *Resolver) markUnmatched(ctx context.Context, it *store.Item, m Match) error {
	// Kennt TMDb den Titel nicht, aber eine vollständige NFO liegt daneben:
	// dann ist er eben aus der NFO erkannt, nicht „nicht erkannt".
	if n := r.nfoZu(ctx, it.ID); n != nil && n.Vollstaendig() {
		n.Anwenden(it, AgeYears)
		it.MatchState = "lokal"
		return r.st.UpsertItem(ctx, it)
	}
	it.MatchState = "unmatched"
	if err := r.st.UpsertItem(ctx, it); err != nil {
		return err
	}
	// Kein Eintrag im Protokoll mit dem Titel: Warum es nicht geklappt
	// hat, steht in der Verwaltung unter „Aufräumen" (GrundZu).
	_ = m
	return errUnmatched
}

// apply überträgt einen Treffer auf den Titel. Was die Quelle nicht weiss,
// bleibt wie es war — ein leeres Feld soll nichts überschreiben.
func (r *Resolver) apply(it *store.Item, c *Candidate) {
	if c.Title != "" {
		it.Title = c.Title
	}
	it.OriginalTitle = c.Original
	it.Overview = c.Overview
	if c.Year > 0 {
		it.Year = c.Year
	}
	if c.Premiered != "" {
		it.Premiered = c.Premiered
	}
	if c.RuntimeMS > 0 {
		it.RuntimeMS = c.RuntimeMS
	}
	if len(c.Genres) > 0 {
		it.Genres = c.Genres
	}
	if c.AgeRating != "" {
		it.AgeRating, it.AgeYears = c.AgeRating, c.AgeYears
	}
	if c.IMDbID != "" {
		it.IMDbID = c.IMDbID
	}
	if c.Sprache != "" {
		it.Sprache = c.Sprache
	}
	// Den Sortierschlüssel rechnet UpsertItem — aus dem eigenen
	// Sortiertitel (von Hand oder aus der NFO), wenn es einen gibt.
}

// ---------------------------------------------------------------- Bilder

// saveArtwork legt Poster und Hintergrund lokal ab. Das ist nicht nur
// schneller — es heisst auch, dass die Mediathek weiter Bilder zeigt, wenn
// der Server gerade kein Internet hat.
// ReihenBilderHolen holt für jede TMDb-Reihe, die noch keine hat,
// Beschreibung, Poster und Hintergrund — die der Reihe selbst. Läuft nach
// jedem Metadaten-Durchlauf.
func (r *Resolver) ReihenBilderHolen(ctx context.Context) {
	if r.tmdb == nil {
		return
	}
	offen, err := r.st.TMDbReihenOhneBilder(ctx)
	if err != nil {
		return
	}
	for id, tmdbID := range offen {
		d, err := r.tmdb.Reihe(ctx, tmdbID)
		if err != nil {
			r.st.Log(ctx, "warn", "metadaten", fmt.Sprintf("Reihe %s: %v", tmdbID, err))
			continue
		}
		poster, backdrop := "", ""
		for _, art := range []struct {
			kind, url string
			ziel      *string
		}{
			{"poster", d.PosterURL, &poster}, {"backdrop", d.BackdropURL, &backdrop},
		} {
			if art.url == "" {
				continue
			}
			name := fmt.Sprintf("%s-%s%s", id, art.kind, extOf(art.url))
			dest := filepath.Join(r.artDir, name)
			if _, err := os.Stat(dest); err != nil {
				if err := r.download(ctx, art.url, dest); err != nil {
					continue
				}
			}
			*art.ziel = name
		}
		_ = r.st.ReiheBilderSetzen(ctx, id, d.Overview, poster, backdrop)
	}
}

// saveCast legt die Besetzung ab, samt kleinen Bildern — lokal, wie die
// Poster: der Browser soll dafür nicht bei TMDb anklopfen müssen.
//
// Der Dateiname trägt einen Abdruck der Bildadresse. Früher hiess das Bild
// nur nach der Stelle („…-cast00.jpg") und wurde wiederverwendet, sobald
// es die Datei gab — nach dem Zuweisen eines anderen Films trug die neue
// Hauptrolle dann das Gesicht der alten.
func (r *Resolver) saveCast(ctx context.Context, itemID string, c *Candidate) {
	// Die Besetzung aus der NFO bleibt, wenn NFO-Werte vorgehen (Vorgabe):
	// Wer sie in Emby gepflegt hat, will sie behalten.
	if n := r.nfoZu(ctx, itemID); n != nil && len(n.Besetzung) > 0 && NFOBevorzugt(ctx, r.st) {
		return
	}
	leute := make([]store.Person, 0, len(c.Cast))
	for _, p := range c.Cast {
		person := store.Person{Name: p.Name, Rolle: p.Rolle}
		if p.BildURL != "" {
			sum := sha1.Sum([]byte(p.BildURL))
			name := fmt.Sprintf("%s-cast-%s%s", itemID, hex.EncodeToString(sum[:5]), extOf(p.BildURL))
			dest := filepath.Join(r.artDir, name)
			if _, err := os.Stat(dest); err == nil || r.download(ctx, p.BildURL, dest) == nil {
				person.Bild = name
			}
		}
		leute = append(leute, person)
	}
	// Keine Besetzung heisst hier meist nur: die Suche brachte keine mit.
	// Leeren tut das Zuweisen ausdrücklich (besetzungLeeren).
	if len(leute) == 0 {
		return
	}
	if err := r.st.BesetzungSetzen(ctx, itemID, leute); err != nil {
		return
	}
	r.alteCastBilderWeg(ctx, itemID)
}

// besetzungLeeren: Beim Zuweisen eines Eintrags, zu dem TMDb niemanden
// kennt, soll nicht die Besetzung des falschen stehen bleiben. Von Hand
// gepflegte bleibt (gesperrt, siehe BesetzungSetzen).
func (r *Resolver) besetzungLeeren(ctx context.Context, itemID string, c *Candidate) {
	if len(c.Cast) > 0 {
		return
	}
	if n := r.nfoZu(ctx, itemID); n != nil && len(n.Besetzung) > 0 && NFOBevorzugt(ctx, r.st) {
		return
	}
	if r.st.BesetzungSetzen(ctx, itemID, nil) == nil {
		r.alteCastBilderWeg(ctx, itemID)
	}
}

// alteCastBilderWeg löscht Bilder der Besetzung eines Titels, auf die
// keine Zeile mehr zeigt — etwa die der vorigen Zuordnung.
func (r *Resolver) alteCastBilderWeg(ctx context.Context, itemID string) {
	leute, err := r.st.Besetzung(ctx, itemID)
	if err != nil {
		return
	}
	behalten := map[string]bool{}
	for _, p := range leute {
		behalten[p.Bild] = true
	}
	alle, _ := filepath.Glob(filepath.Join(r.artDir, itemID+"-cast*"))
	for _, pfad := range alle {
		name := filepath.Base(pfad)
		if !behalten[name] && !strings.Contains(name, ablage.Endung) {
			_ = os.Remove(pfad)
		}
	}
}

func (r *Resolver) saveArtwork(ctx context.Context, itemID string, c *Candidate) error {
	r.saveCast(ctx, itemID, c)
	return r.bilderSpeichern(ctx, itemID, c)
}

// bilderSpeichern legt Poster und Hintergrund ab (ohne die Besetzung).
func (r *Resolver) bilderSpeichern(ctx context.Context, itemID string, c *Candidate) error {
	// Bilder aus der Sammlung selbst („poster.jpg" neben dem Film) hat
	// jemand bewusst dort hingelegt — die ersetzt TMDb nicht.
	lokaleArten := r.st.LokaleBildArten(ctx, itemID)
	// Von Hand gesetzte Bilder (gesperrt) bleiben ebenso.
	sperren, _ := r.st.Sperren(ctx, itemID)
	for _, art := range []struct{ kind, url string }{
		{"poster", c.PosterURL},
		{"backdrop", c.BackdropURL},
	} {
		if art.url == "" || lokaleArten[art.kind] || sperren[art.kind] {
			continue
		}
		name := fmt.Sprintf("%s-%s%s", itemID, art.kind, extOf(art.url))
		dest := filepath.Join(r.artDir, name)

		// Schon da und von derselben Adresse — nicht noch einmal holen.
		// Kommt eine andere Adresse (der Titel wurde neu zugeordnet, TMDb
		// hat ein anderes Bild), wird ersetzt: Sonst blieben nach dem
		// Zuweisen der richtigen Serie Poster und Folgenbilder der falschen.
		if _, err := os.Stat(dest); err == nil && r.st.BildQuelle(ctx, itemID, art.kind) == art.url {
			continue
		}
		if err := r.download(ctx, art.url, dest); err != nil {
			r.st.Log(ctx, "warn", "metadaten",
				fmt.Sprintf("Bild nicht geladen (%s): %v", art.kind, err))
			continue
		}
		if err := r.st.SetArtwork(ctx, itemID, art.kind, name, art.url); err != nil {
			return err
		}
	}
	return nil
}

func (r *Resolver) download(ctx context.Context, url, dest string) error {
	// Dasselbe Ziel nur einmal gleichzeitig (ablage.Einmal): Ein Durchgang
	// und „Neu laden" holten sonst dasselbe Poster zweimal.
	return ablage.Einmal(dest, func() error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		resp, err := r.http.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("antwortete mit %s", resp.Status)
		}
		// Erst daneben in eine eigene Zwischendatei, dann umbenennen: So
		// entsteht nie eine halbe Datei, und zwei Schreiber kommen sich
		// nicht in die Quere. Eine Obergrenze, damit eine falsche Adresse
		// nicht die Platte füllt — ein grösseres Bild ist ein Fehler, kein
		// halbes Poster.
		if err := ablage.Schreiben(dest, resp.Body, maxBild); err != nil {
			if errors.Is(err, ablage.ErrZuGross) {
				return fmt.Errorf("bild grösser als %d MB", maxBild>>20)
			}
			return err
		}
		return nil
	})
}

// maxBild: grösser ist kein Poster, sondern eine falsche Adresse.
const maxBild = 12 << 20

func extOf(url string) string {
	if i := strings.LastIndex(url, "."); i > 0 && len(url)-i <= 5 {
		return url[i:]
	}
	return ".jpg"
}

// ArtDir sagt, wo die Bilder liegen — die API liefert sie von dort aus.
func (r *Resolver) ArtDir() string { return r.artDir }

// Stand sagt, ob und wie weit gerade Metadaten geholt werden.
func (r *Resolver) Stand() Stand {
	r.standMu.Lock()
	defer r.standMu.Unlock()
	return r.stand
}

// ---------------------------------------------------------------- von Hand

// Kandidaten sucht bei der Quelle, was zu einem Titel passen könnte — für
// „Anderen Titel zuweisen" in der Verwaltung. Die Suche ist frei: wer
// weiss, dass „Der Fund" eigentlich „The Find" heisst, tippt das ein.
func (r *Resolver) Kandidaten(ctx context.Context, kind, suche string, jahr int) ([]Candidate, error) {
	if !r.Ready() {
		return nil, fmt.Errorf("es ist keine Metadaten-Quelle eingerichtet")
	}
	// Ein Link oder eine Kennung statt eines Titels: direkt
	// diesen einen Eintrag holen, statt zu suchen.
	if c, ok, err := r.direkterTreffer(ctx, kind, suche); ok || err != nil {
		if err != nil {
			return nil, err
		}
		return []Candidate{*c}, nil
	}
	var cands []Candidate
	var err error
	cands, err = r.suchen(ctx, kind, suche, jahr)
	if err != nil {
		return nil, err
	}
	// Die wahrscheinlichsten zuerst, aber alle dabei: entschieden wird
	// von Hand.
	sort.SliceStable(cands, func(i, j int) bool {
		return Score(suche, jahr, cands[i]) > Score(suche, jahr, cands[j])
	})
	if len(cands) > 12 {
		cands = cands[:12]
	}
	return cands, nil
}

// Zuweisen hängt einen Titel an genau den Eintrag der Quelle, den jemand
// ausgesucht hat. Der Titel gilt danach als von Hand zugeordnet und wird
// von keinem Durchlauf mehr umgehängt. Bei einer Serie werden die Folgen
// danach neu geholt — sie hängen ja an der Kennung der Serie.
func (r *Resolver) Zuweisen(ctx context.Context, it *store.Item, sourceID string) error {
	return r.zuweisen(ctx, it, sourceID, true)
}

// ZuweisenOhneFolgen weist zu wie Zuweisen, holt bei einer Serie die
// Folgen aber nicht gleich: Sie stehen danach auf „ausstehend" und kommen
// mit dem nächsten Durchgang (für viele Titel auf einmal, etwa beim
// Übernehmen der Zuordnungen aus Emby).
func (r *Resolver) ZuweisenOhneFolgen(ctx context.Context, it *store.Item, sourceID string) error {
	return r.zuweisen(ctx, it, sourceID, false)
}

func (r *Resolver) zuweisen(ctx context.Context, it *store.Item, sourceID string, folgenGleich bool) error {
	if !r.Ready() {
		return fmt.Errorf("es ist keine Metadaten-Quelle eingerichtet")
	}
	if err := tmdbKennung(sourceID); err != nil {
		return err
	}
	grundVergessen(it.ID)
	r.vormerkungErledigt(ctx, it.ID)
	switch it.Kind {
	case "movie":
		full, err := r.tmdb.Details(ctx, sourceID)
		if err != nil {
			return err
		}
		r.apply(it, full)
		tomaten := -2
		if r.useIMDb && r.omdb != nil && full.IMDbID != "" {
			if w, err := r.omdb.Wertungen(ctx, full.IMDbID); err == nil {
				it.IMDbRating, it.IMDbVotes, tomaten = w.IMDb, w.Stimmen, w.Tomaten
			}
		}
		it.MatchState = "manual"
		if err := r.st.UpsertItem(ctx, it); err != nil {
			return err
		}
		if tomaten > -2 {
			_ = r.st.TomatenSetzen(ctx, it.ID, tomaten)
		}
		_ = r.st.SetSourceID(ctx, it.ID, "tmdb", sourceID)
		if full.ReiheID != "" {
			_ = r.st.TMDbReiheZuordnen(ctx, full.ReiheID, full.ReiheName, it.ID, it.Year)
		} else {
			// Der neue Film gehört zu keiner Reihe: aus der des alten heraus.
			// Sonst stand „Alien vs. Predator" nach dem Zuweisen noch in der
			// Alien-Reihe, nur weil vorher „Alien 3" zugeordnet war.
			_ = r.st.TMDbReiheLoesen(ctx, it.ID)
		}
		r.besetzungLeeren(ctx, it.ID, full)
		return r.saveArtwork(ctx, it.ID, full)
	case "series":
		gewaehlt, err := r.tmdb.SeriesDetails(ctx, sourceID)
		if err != nil {
			return err
		}
		r.apply(it, gewaehlt)
		tomaten := r.wertungenHolen(ctx, it, gewaehlt.IMDbID)
		it.MatchState = "manual"
		if err := r.st.UpsertItem(ctx, it); err != nil {
			return err
		}
		if tomaten > -2 {
			_ = r.st.TomatenSetzen(ctx, it.ID, tomaten)
		}
		if err := r.st.SetSourceID(ctx, it.ID, "tmdb", sourceID); err != nil {
			return err
		}
		if gewaehlt.TVDBID != "" {
			_ = r.st.SetSourceID(ctx, it.ID, "tvdb", gewaehlt.TVDBID)
		}
		if gewaehlt.ZeitraumBekannt {
			_ = r.st.SerienZeitraum(ctx, it.ID, gewaehlt.EndYear, gewaehlt.LaeuftNoch)
		}
		if _, err := r.st.MetadatenZuruecksetzen(ctx, "", it.ID); err != nil {
			return err
		}
		// Die Serie selbst soll „manual" bleiben; das Zurücksetzen hat
		// sie mit auf „pending" gestellt.
		it.MatchState = "manual"
		if err := r.st.UpsertItem(ctx, it); err != nil {
			return err
		}
		r.besetzungLeeren(ctx, it.ID, gewaehlt)
		if err := r.saveArtwork(ctx, it.ID, gewaehlt); err != nil {
			return err
		}
		if !folgenGleich {
			return nil
		}
		return r.titelNachholen(ctx, it.ID)
	}
	return fmt.Errorf("für %s lässt sich nichts zuweisen", it.Kind)
}

// NeuLaden holt die Metadaten eines Titels noch einmal — bei einer Serie
// samt Staffeln und Folgen.
func (r *Resolver) NeuLaden(ctx context.Context, it *store.Item) error {
	if _, err := r.st.MetadatenZuruecksetzen(ctx, "", it.ID); err != nil {
		return err
	}
	return r.titelNachholen(ctx, it.ID)
}

// titelNachholen holt die Metadaten eines Titels und seiner Folgen — alle,
// nicht nur die, die zufällig unter den ersten 500 der Mediathek stehen.
//
// Die ersten 50 gleich (die Seite zeigt danach den richtigen Stand). Den
// Rest einer langen Serie holt der Aufrufer im Hintergrund nach (api:
// metadatenImHintergrund) — die Anfrage soll nicht minutenlang hängen, und
// der Rest läuft unter derselben Arbeitssperre wie jeder Durchgang.
func (r *Resolver) titelNachholen(ctx context.Context, id string) error {
	wartet, err := r.st.PendingUnter(ctx, id)
	if err != nil {
		return err
	}
	holen := func(ctx context.Context, liste []store.Item) {
		for i := range liste {
			if ctx.Err() != nil {
				return
			}
			if err := r.resolveOne(ctx, &liste[i]); err != nil && err != errUnmatched {
				// Ohne Titel: Das Protokoll verrät nicht, was in der
				// Mediathek steht (DATENSCHUTZ.md).
				r.st.Log(ctx, "warn", "metadaten", fmt.Sprintf("ein Titel liess sich nicht nachladen: %v", err))
			}
		}
	}
	const sofort = 50
	holen(ctx, wartet[:min(len(wartet), sofort)])
	r.ReihenBilderHolen(ctx)
	return ctx.Err()
}

// SerienZeitraeumeNachholen fragt für Serien, deren Zeitraum fehlt (aus
// älteren Fassungen) oder die zuletzt noch liefen, bei TMDb nach — wenige
// je Durchgang, damit kein Durchlauf an TMDb hängen bleibt.
func (r *Resolver) SerienZeitraeumeNachholen(ctx context.Context, n int) int {
	if !r.Ready() || r.tmdb == nil {
		return 0
	}
	serien, err := r.st.SerienOhneZeitraum(ctx, n)
	if err != nil {
		return 0
	}
	fertig := 0
	for _, it := range serien {
		store.HintergrundWarten(ctx)
		src, err := r.st.SourceID(ctx, it.ID, "tmdb")
		if err != nil || src == "" {
			continue
		}
		d, err := r.tmdb.SeriesDetails(ctx, src)
		if err != nil || !d.ZeitraumBekannt {
			continue
		}
		if r.st.SerienZeitraum(ctx, it.ID, d.EndYear, d.LaeuftNoch) == nil {
			fertig++
		}
	}
	return fertig
}

// einstellungFreigabeVersucht: Serien, für die TMDb schon gefragt wurde
// und keine Freigabe kannte (Kennung → Tag der Anfrage). Früher stand das
// nur im Speicher: Nach jedem Neustart (also nach jedem Update) fragte
// Lumen für all diese Serien wieder bei TMDb nach. Jetzt erst nach
// freigabeWieder.
const (
	einstellungFreigabeVersucht = "meta.freigabe_versucht"
	freigabeWieder              = 30 * 24 * time.Hour
)

// SerienFreigabenNachholen holt für zugeordnete Serien die Altersfreigabe
// nach. Frühere Fassungen fragten TMDb gar nicht danach; ein Kinderprofil
// sah darum keine Serie. Höchstens n auf einmal.
func (r *Resolver) SerienFreigabenNachholen(ctx context.Context, n int) int {
	if !r.Ready() || r.tmdb == nil {
		return 0
	}
	serien, err := r.st.SerienOhneFreigabe(ctx, n+200)
	if err != nil {
		return 0
	}
	versucht := map[string]int64{}
	_ = json.Unmarshal([]byte(r.st.Setting(ctx, einstellungFreigabeVersucht, "{}")), &versucht)
	geaendert := false
	defer func() {
		if geaendert {
			// Nur behalten, was noch gilt — die Liste wächst nicht ewig.
			for id, tag := range versucht {
				if time.Since(time.Unix(tag, 0)) >= freigabeWieder {
					delete(versucht, id)
				}
			}
			b, _ := json.Marshal(versucht)
			_ = r.st.SetSetting(context.WithoutCancel(ctx), einstellungFreigabeVersucht, string(b))
		}
	}()
	fertig, gefragt := 0, 0
	for _, it := range serien {
		if ctx.Err() != nil || gefragt >= n {
			break
		}
		if tag, schon := versucht[it.ID]; schon && time.Since(time.Unix(tag, 0)) < freigabeWieder {
			continue
		}
		src, err := r.st.SourceID(ctx, it.ID, "tmdb")
		if err != nil || src == "" {
			continue
		}
		store.HintergrundWarten(ctx)
		gefragt++
		d, err := r.tmdb.SeriesDetails(ctx, src)
		if err != nil {
			continue
		}
		if d.AgeYears == nil {
			versucht[it.ID] = time.Now().Unix()
			geaendert = true
			continue
		}
		if r.st.FreigabeSetzen(ctx, it.ID, d.AgeRating, *d.AgeYears) == nil {
			fertig++
		}
	}
	return fertig
}

// posterVersucht: Titel, für die das Nachholen schon einmal lief und TMDb
// kein Poster kannte (oder es wieder scheiterte) — bis zum nächsten Start
// nicht noch einmal.
var posterVersucht sync.Map

// PosterNachholen holt Poster und Hintergrund für zugeordnete Titel, deren
// Download scheiterte (store.TitelOhnePoster). Höchstens n auf einmal;
// jeder Titel kostet eine Anfrage bei TMDb.
func (r *Resolver) PosterNachholen(ctx context.Context, n int) int {
	if !r.Ready() || r.tmdb == nil {
		return 0
	}
	titel, err := r.st.TitelOhnePoster(ctx, n+200)
	if err != nil {
		return 0
	}
	fertig, gefragt := 0, 0
	for _, it := range titel {
		if ctx.Err() != nil || gefragt >= n {
			break
		}
		if _, schon := posterVersucht.LoadOrStore(it.ID, true); schon {
			continue
		}
		src, err := r.st.SourceID(ctx, it.ID, "tmdb")
		if err != nil || src == "" {
			continue
		}
		store.HintergrundWarten(ctx)
		gefragt++
		var c *Candidate
		if it.Kind == "series" {
			c, err = r.tmdb.SeriesDetails(ctx, src)
		} else {
			c, err = r.tmdb.Details(ctx, src)
		}
		if err != nil || c.PosterURL == "" {
			continue
		}
		if r.bilderSpeichern(ctx, it.ID, c) == nil && r.st.BildQuelle(ctx, it.ID, "poster") != "" {
			fertig++
		}
	}
	if fertig > 0 {
		r.st.Log(ctx, "info", "metadaten", fmt.Sprintf("%d fehlende Poster nachgeholt", fertig))
	}
	return fertig
}

// TomatenNachholen holt für schon zugeordnete Titel die Wertung von
// Rotten Tomatoes nach — höchstens n auf einmal, damit der kostenlose
// OMDb-Schlüssel (1000 Abfragen am Tag) reicht.
func (r *Resolver) TomatenNachholen(ctx context.Context, n int) int {
	if !r.useIMDb || r.omdb == nil {
		return 0
	}
	titel, err := r.st.OhneTomaten(ctx, n)
	if err != nil {
		return 0
	}
	fertig := 0
	for _, it := range titel {
		store.HintergrundWarten(ctx)
		if ctx.Err() != nil {
			break
		}
		w, err := r.omdb.Wertungen(ctx, it.IMDbID)
		if err != nil {
			continue
		}
		if r.st.TomatenSetzen(ctx, it.ID, w.Tomaten) == nil {
			fertig++
		}
	}
	return fertig
}

// wertungenHolen setzt IMDb-Note und Stimmen am Titel und gibt die
// Rotten-Tomatoes-Wertung zurück (-2: nicht gefragt, -1: keine).
func (r *Resolver) wertungenHolen(ctx context.Context, it *store.Item, imdbID string) int {
	if !r.useIMDb || r.omdb == nil || imdbID == "" {
		return -2
	}
	w, err := r.omdb.Wertungen(ctx, imdbID)
	if err != nil {
		return -2
	}
	it.IMDbRating, it.IMDbVotes = w.IMDb, w.Stimmen
	return w.Tomaten
}

// SerienWertungenNachholen: Serien, die vor dieser Fassung zugeordnet
// wurden, haben keine IMDb-Kennung — TMDb nennt sie bei Serien nur auf
// Nachfrage. Hier wird sie geholt, samt IMDb-Note und Rotten Tomatoes.
func (r *Resolver) SerienWertungenNachholen(ctx context.Context, n int) int {
	if !r.Ready() || r.tmdb == nil || !r.useIMDb || r.omdb == nil {
		return 0
	}
	serien, err := r.st.SerienOhneIMDb(ctx, n)
	if err != nil {
		return 0
	}
	fertig := 0
	for _, it := range serien {
		store.HintergrundWarten(ctx)
		if ctx.Err() != nil {
			break
		}
		src, err := r.st.SourceID(ctx, it.ID, "tmdb")
		if err != nil || src == "" {
			_ = r.st.TomatenSetzen(ctx, it.ID, -1)
			continue
		}
		d, err := r.tmdb.SeriesDetails(ctx, src)
		if err != nil {
			continue
		}
		if d.TVDBID != "" {
			_ = r.st.SetSourceID(ctx, it.ID, "tvdb", d.TVDBID)
		}
		x := it
		x.IMDbID = d.IMDbID
		tomaten := r.wertungenHolen(ctx, &x, d.IMDbID)
		if err := r.st.WertungenSetzen(ctx, it.ID, x.IMDbID, x.IMDbRating, x.IMDbVotes, tomaten); err == nil {
			fertig++
		}
	}
	return fertig
}
