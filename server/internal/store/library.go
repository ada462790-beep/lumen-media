// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

type Library struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Kind      string     `json:"kind"` // movie | series
	Path      string     `json:"path"`
	Language  string     `json:"language"`
	ScannedAt *time.Time `json:"scannedAt,omitempty"`
	Count     int        `json:"count"`
	// Alle wie viele Minuten Lumen von selbst nachsieht; 0 = nie.
	IntervalMin int `json:"intervalMin"`
	// Oder zu einer festen Uhrzeit täglich, „HH:MM"; leer = nach Takt.
	ScanAt string `json:"scanAt"`
	// Was der letzte Durchlauf ergab — als Auskunft in der Verwaltung.
	LastResult *ScanErgebnis `json:"lastResult,omitempty"`
}

// ScanErgebnis ist, was ein Durchlauf hinterlässt.
type ScanErgebnis struct {
	At        time.Time `json:"at"`
	Added     int       `json:"added"`
	Updated   int       `json:"updated"`
	Missing   int       `json:"missing"`
	Unmatched int       `json:"unmatched"`
	TookMS    int64     `json:"tookMs"`
	Auto      bool      `json:"auto"` // von selbst, nicht auf Knopfdruck
}

type Item struct {
	ID        string `json:"id"`
	LibraryID string `json:"libraryId"`
	Kind      string `json:"kind"`
	ParentID  string `json:"parentId,omitempty"`
	Title     string `json:"title"`
	// SortTitle ist der Schlüssel, nach dem sortiert wird (sortieren.go).
	// UpsertItem rechnet ihn selbst; was hier vorher steht, zählt nicht.
	SortTitle string `json:"-"`
	// SortEigen: ein eigener Sortiertitel, von Hand oder aus der NFO
	// („Herr der Ringe 1"). Leer = nach dem Titel.
	SortEigen string `json:"-"`
	// Sprache: die Originalsprache, wenn eine Quelle sie kennt („en").
	Sprache       string `json:"-"`
	OriginalTitle string `json:"originalTitle,omitempty"`
	Year          int    `json:"year,omitempty"`
	SeasonNo      int    `json:"seasonNo,omitempty"`
	EpisodeNo     int    `json:"episodeNo,omitempty"`
	Overview      string `json:"overview,omitempty"`
	AgeRating     string `json:"ageRating,omitempty"`
	AgeYears      *int   `json:"ageYears,omitempty"`
	// Serien: Jahr der letzten Folge und ob noch neue kommen.
	// Leer, solange die Quelle es nicht gesagt hat.
	EndYear    *int  `json:"endYear,omitempty"`
	LaeuftNoch *bool `json:"laeuftNoch,omitempty"`
	// Rotten Tomatoes: Anteil guter Kritiken in Prozent, wenn bekannt.
	Tomaten    *int      `json:"rottenTomatoes,omitempty"`
	RuntimeMS  int64     `json:"runtimeMs,omitempty"`
	Premiered  string    `json:"premiered,omitempty"`
	IMDbID     string    `json:"imdbId,omitempty"`
	IMDbRating float64   `json:"imdbRating,omitempty"`
	IMDbVotes  int       `json:"imdbVotes,omitempty"`
	MatchState string    `json:"matchState"`
	AddedAt    time.Time `json:"addedAt"`
	Genres     []string  `json:"genres,omitempty"`
}

// ---------------------------------------------------------------- Bibliotheken

func (s *Store) CreateLibrary(ctx context.Context, name, kind, path, lang string) (*Library, error) {
	if lang == "" {
		lang = "de"
	}
	// Neue Bibliotheken sehen einmal am Tag nach, um 4 Uhr früh. Jede
	// Stunde hielte die Platten des NAS dauernd wach; wer es öfter will,
	// stellt es in der Verwaltung um.
	l := &Library{ID: newID("lib"), Name: name, Kind: kind, Path: path, Language: lang,
		IntervalMin: 1440, ScanAt: StandardScanZeit}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO library (id, name, kind, path, language, scan_interval_min, scan_at)
		 VALUES (?,?,?,?,?,?,?)`,
		l.ID, l.Name, l.Kind, l.Path, l.Language, l.IntervalMin, l.ScanAt)
	return l, err
}

func (s *Store) Libraries(ctx context.Context) ([]Library, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT l.id, l.name, l.kind, l.path, l.language, l.scanned_at,
		        (SELECT count(*) FROM item i
		          WHERE i.library_id = l.id AND i.kind IN ('movie','series')) AS n,
		        l.scan_interval_min, COALESCE(l.scan_at, ''), COALESCE(l.last_result, '')
		   FROM library l`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Library{}
	for rows.Next() {
		var l Library
		var scanned sql.NullInt64
		var ergebnis string
		if err := rows.Scan(&l.ID, &l.Name, &l.Kind, &l.Path, &l.Language, &scanned, &l.Count,
			&l.IntervalMin, &l.ScanAt, &ergebnis); err != nil {
			return nil, err
		}
		if scanned.Valid {
			t := time.Unix(scanned.Int64, 0)
			l.ScannedAt = &t
		}
		if ergebnis != "" {
			var e ScanErgebnis
			if json.Unmarshal([]byte(ergebnis), &e) == nil {
				l.LastResult = &e
			}
		}
		out = append(out, l)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	nachNamenSortieren(out, func(l *Library) string { return l.Name }, func(l *Library) string { return l.ID }, nil)
	return out, nil
}

func (s *Store) MarkScanned(ctx context.Context, libraryID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE library SET scanned_at = ? WHERE id = ?`, time.Now().Unix(), libraryID)
	return err
}

// ScanErgebnisSetzen vermerkt, was der Durchlauf ergab.
func (s *Store) ScanErgebnisSetzen(ctx context.Context, libraryID string, e ScanErgebnis) error {
	raw, err := json.Marshal(e)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`UPDATE library SET last_result = ?, scanned_at = ? WHERE id = ?`,
		string(raw), e.At.Unix(), libraryID)
	return err
}

// StandardScanZeit ist die Uhrzeit, zu der eine Bibliothek ohne eigene
// Einstellung nachsieht.
const StandardScanZeit = "04:00"

// SetScanInterval stellt ein, wann Lumen von selbst nachsieht: alle so
// viele Minuten (0 = nur auf Knopfdruck), oder — wenn scanAt „HH:MM"
// ist — täglich zu dieser Uhrzeit; dann zählt der Takt nicht.
func (s *Store) SetScanInterval(ctx context.Context, libraryID string, minuten int, scanAt string) error {
	if minuten < 0 {
		minuten = 0
	}
	if scanAt != "" {
		if _, err := time.Parse("15:04", scanAt); err != nil {
			return errors.New("uhrzeit bitte als HH:MM")
		}
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE library SET scan_interval_min = ?, scan_at = ? WHERE id = ?`, minuten, scanAt, libraryID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Faellig nennt die Bibliotheken, deren Durchlauf überfällig ist.
func (s *Store) Faellig(ctx context.Context) ([]Library, error) {
	alle, err := s.Libraries(ctx)
	if err != nil {
		return nil, err
	}
	var out []Library
	jetzt := time.Now()
	for _, l := range alle {
		if faellig(l, jetzt) {
			out = append(out, l)
		}
	}
	return out, nil
}

// faellig entscheidet für eine Bibliothek: Zur festen Uhrzeit ist sie
// fällig, sobald die Uhrzeit heute vorbei ist und der letzte Durchlauf
// davor lag — nicht nur in der einen Minute, damit ein Neustart um die
// Uhrzeit herum nichts verschluckt. Sonst nach Takt.
func faellig(l Library, jetzt time.Time) bool {
	if l.ScannedAt == nil && (l.ScanAt != "" || l.IntervalMin > 0) {
		return true // noch nie geprüft: gleich, nicht erst um 4 Uhr
	}
	if l.ScanAt != "" {
		t, err := time.Parse("15:04", l.ScanAt)
		if err != nil {
			return false
		}
		heute := time.Date(jetzt.Year(), jetzt.Month(), jetzt.Day(), t.Hour(), t.Minute(), 0, 0, jetzt.Location())
		if jetzt.Before(heute) {
			return false
		}
		return l.ScannedAt == nil || l.ScannedAt.Before(heute)
	}
	if l.IntervalMin <= 0 {
		return false
	}
	return l.ScannedAt == nil || jetzt.Sub(*l.ScannedAt) >= time.Duration(l.IntervalMin)*time.Minute
}

// MetadatenZuruecksetzen stellt Titel wieder auf „ausstehend", damit der
// nächste Durchlauf sie neu holt — alle einer Bibliothek, oder einen
// einzelnen. Von Hand zugewiesene (manual) bleiben, wie sie sind, es
// sei denn, genau der eine wird verlangt.
func (s *Store) MetadatenZuruecksetzen(ctx context.Context, libraryID, itemID string) (int64, error) {
	var res sql.Result
	var err error
	switch {
	case itemID != "":
		res, err = s.db.ExecContext(ctx,
			`UPDATE item SET match_state = 'pending' WHERE id = ? OR parent_id = ?
			    OR parent_id IN (SELECT id FROM item WHERE parent_id = ?)`, itemID, itemID, itemID)
	case libraryID != "":
		res, err = s.db.ExecContext(ctx,
			`UPDATE item SET match_state = 'pending' WHERE library_id = ? AND match_state != 'manual'`, libraryID)
	default:
		res, err = s.db.ExecContext(ctx,
			`UPDATE item SET match_state = 'pending' WHERE match_state != 'manual'`)
	}
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

func (s *Store) DeleteLibrary(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM library WHERE id = ?`, id)
	return err
}

// ---------------------------------------------------------------- Titel

const itemCols = `id, library_id, kind, COALESCE(parent_id,''), title, sort_title,
	COALESCE(original_title,''), COALESCE(year,0), COALESCE(season_no,0),
	COALESCE(episode_no,0), COALESCE(overview,''), COALESCE(age_rating,''),
	age_years, COALESCE(runtime_ms,0), COALESCE(premiered,''), COALESCE(imdb_id,''),
	COALESCE(imdb_rating,0), COALESCE(imdb_votes,0), match_state, added_at,
	end_year, laeuft_noch, rt_wertung, COALESCE(sort_eigen,''), COALESCE(sprache_original,'')`

func scanItem(sc interface{ Scan(...any) error }) (*Item, error) {
	var it Item
	var age, ende, laeuft, tomaten sql.NullInt64
	var added int64
	err := sc.Scan(&it.ID, &it.LibraryID, &it.Kind, &it.ParentID, &it.Title, &it.SortTitle,
		&it.OriginalTitle, &it.Year, &it.SeasonNo, &it.EpisodeNo, &it.Overview,
		&it.AgeRating, &age, &it.RuntimeMS, &it.Premiered, &it.IMDbID,
		&it.IMDbRating, &it.IMDbVotes, &it.MatchState, &added, &ende, &laeuft, &tomaten,
		&it.SortEigen, &it.Sprache)
	if err != nil {
		return nil, err
	}
	if ende.Valid && ende.Int64 > 0 {
		v := int(ende.Int64)
		it.EndYear = &v
	}
	if laeuft.Valid {
		v := laeuft.Int64 == 1
		it.LaeuftNoch = &v
	}
	if tomaten.Valid && tomaten.Int64 >= 0 {
		v := int(tomaten.Int64)
		it.Tomaten = &v
	}
	if age.Valid {
		v := int(age.Int64)
		it.AgeYears = &v
	}
	it.AddedAt = time.Unix(added, 0)
	return &it, nil
}

// UpsertItem legt einen Titel an oder bringt ihn auf Stand. Der Scanner
// ruft das für jede Datei auf, die er findet.
func (s *Store) UpsertItem(ctx context.Context, it *Item) error {
	if it.ID == "" {
		it.ID = newID("it")
	}
	if it.MatchState == "" {
		it.MatchState = "pending"
	}
	now := time.Now().Unix()

	return s.tx(ctx, func(t *sql.Tx) error {
		// Von Hand gesperrte Felder behalten ihren Wert (sperren.go).
		if err := sperrenAnwenden(ctx, t, it); err != nil {
			return err
		}
		if err := sortierungSetzen(ctx, t, it); err != nil {
			return err
		}
		_, err := t.ExecContext(ctx,
			`INSERT INTO item (id, library_id, kind, parent_id, title, sort_title,
			    original_title, year, season_no, episode_no, overview, age_rating,
			    age_years, runtime_ms, premiered, imdb_id, imdb_rating, imdb_votes,
			    added_at, updated_at, match_state, titel_klein, original_klein, zuletzt_neu,
			    sort_eigen, sprache_original)
			 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
			 ON CONFLICT (id) DO UPDATE SET
			   title=excluded.title, sort_title=excluded.sort_title,
			   sort_eigen=excluded.sort_eigen, sprache_original=excluded.sprache_original,
			   titel_klein=excluded.titel_klein, original_klein=excluded.original_klein,
			   original_title=excluded.original_title, year=excluded.year,
			   overview=excluded.overview, age_rating=excluded.age_rating,
			   age_years=excluded.age_years, runtime_ms=excluded.runtime_ms,
			   premiered=excluded.premiered, imdb_id=excluded.imdb_id,
			   imdb_rating=excluded.imdb_rating, imdb_votes=excluded.imdb_votes,
			   updated_at=excluded.updated_at, match_state=excluded.match_state`,
			it.ID, it.LibraryID, it.Kind, nullStr(it.ParentID), it.Title, it.SortTitle,
			nullStr(it.OriginalTitle), nullInt(it.Year), nullInt(it.SeasonNo),
			nullInt(it.EpisodeNo), nullStr(it.Overview), nullStr(it.AgeRating),
			it.AgeYears, nullInt64(it.RuntimeMS), nullStr(it.Premiered),
			nullStr(it.IMDbID), nullFloat(it.IMDbRating), nullInt(it.IMDbVotes),
			now, now, it.MatchState, titelKlein(it.Title), nullStr(titelKlein(it.OriginalTitle)), now,
			nullStr(strings.TrimSpace(it.SortEigen)), nullStr(it.Sprache))
		if err != nil {
			return err
		}
		// Eine neue Folge macht ihre Serie „neu": Sie rückt unter
		// „Zuletzt hinzugefügt" nach vorn. Die Bedingung am
		// Ende lässt die Zeile der Serie in Ruhe, wenn sich nichts ändert —
		// sonst schriebe jede nachgeschlagene Folge die Serie neu.
		if it.Kind == "episode" && it.ParentID != "" {
			if _, err := t.ExecContext(ctx,
				`UPDATE item SET zuletzt_neu = e.added_at
				   FROM item e, item st
				  WHERE e.id = ? AND st.id = e.parent_id AND item.id = st.parent_id
				    AND COALESCE(item.zuletzt_neu, 0) < e.added_at`, it.ID); err != nil {
				return err
			}
		}
		if len(it.Genres) > 0 {
			if _, err := t.ExecContext(ctx, `DELETE FROM genre WHERE item_id = ?`, it.ID); err != nil {
				return err
			}
			for _, g := range it.Genres {
				if _, err := t.ExecContext(ctx,
					`INSERT OR IGNORE INTO genre (item_id, name) VALUES (?,?)`, it.ID, g); err != nil {
					return err
				}
			}
		}
		hay := strings.ToLower(strings.Join([]string{it.Title, it.OriginalTitle,
			strings.Join(it.Genres, " ")}, " "))
		_, err = t.ExecContext(ctx,
			`INSERT INTO item_search (item_id, haystack) VALUES (?,?)
			 ON CONFLICT (item_id) DO UPDATE SET haystack = excluded.haystack`,
			it.ID, fold(hay)+" "+fold(ohnePunkte(hay)))
		return err
	})
}

func (s *Store) Item(ctx context.Context, id string) (*Item, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+itemCols+` FROM item WHERE id = ?`, id)
	it, err := scanItem(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	it.Genres, _ = s.genres(ctx, id)
	return it, nil
}

func (s *Store) genres(ctx context.Context, itemID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT name FROM genre WHERE item_id = ? ORDER BY name`, itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectStrings(rows)
}

// Query ist die eine Abfrage, aus der sich Mediathek, Suche und Reihen
// speisen. Die Rechte des Kontos stecken in allowedLibraries und maxAge —
// was jemand nicht sehen darf, kommt gar nicht erst zurück.
type Query struct {
	Libraries []string // leer = keine
	Kinds     []string // movie | series
	Genre     string
	Text      string
	// Person: jemand aus der Besetzung (Name, ganz oder als Anfang).
	Person string
	// Jahr von/bis, 0 = offen.
	JahrVon, JahrBis int
	MaxAge           int // 0 = ohne Grenze, ausser MitAltersgrenze ist gesetzt
	AllowUnrated     bool
	// Sort: eine der rasterSortierungen (sortieren.go), „liste" (in der
	// Reihenfolge von IDs) oder „relevanz" (Suchtreffer nach Güte, Text).
	Sort string
	// Richtung: „asc", „desc" oder leer für die Vorgabe der Sortierung.
	Richtung string
	// Seed: der Samen für „random" — dieselbe Zahl gibt dieselbe
	// Reihenfolge, damit Nachladen keine Titel doppelt bringt.
	Seed int64
	// Konto: das Profil, für das „lastwatched" und „unwatched" gelten.
	// Gelesen wird nur dessen eigener Stand (playback.go).
	Konto  string
	Limit  int
	Offset int
	IDs    []string
	// NotIDs: diese Titel bleiben draussen — etwa alles, was schon angefangen wurde.
	NotIDs []string
	// Buchstabe: nur Titel, die damit anfangen („a" … „z"), „#" für alles
	// andere (Zahlen, Zeichen).
	Buchstabe string
	// Freigabe: die Abfrage kommt aus den Rechten eines Kontos. Dann
	// heisst „keine Bibliotheken" auch „nichts" — auch wenn Kennungen
	// mitkommen. Ohne diesen Riegel lief eine Abfrage mit Kennungen und
	// leerer Bibliotheksliste ganz ohne Einschränkung, und ein Konto ohne
	// jede Freigabe sah über Reihen und geteilte Listen alles.
	Freigabe bool
	// MitAltersgrenze: MaxAge gilt auch, wenn es 0 ist („nur FSK 0").
	// Ohne diesen Schalter bleibt eine leere Query ohne Altersgrenze, wie
	// sie der Scanner und die Wartung brauchen.
	MitAltersgrenze bool
}

// bedingung baut die WHERE-Klausel einer Abfrage — für Query, Count und Genres
// dieselbe, damit die Zahl über dem Raster zu dem passt, was darin steht.
func bedingung(q Query) (where []string, args []any, ok bool) {
	if len(q.Libraries) == 0 && (len(q.IDs) == 0 || q.Freigabe) {
		return nil, nil, false
	}
	where = []string{}
	args = []any{}

	if len(q.IDs) > 0 {
		where = append(where, `i.id IN (`+placeholders(len(q.IDs))+`)`)
		args = append(args, toAny(q.IDs)...)
	}
	if len(q.Libraries) > 0 {
		where = append(where, `i.library_id IN (`+placeholders(len(q.Libraries))+`)`)
		args = append(args, toAny(q.Libraries)...)
	}
	if len(q.NotIDs) > 0 {
		where = append(where, `i.id NOT IN (`+placeholders(len(q.NotIDs))+`)`)
		args = append(args, toAny(q.NotIDs)...)
	}
	kinds := q.Kinds
	if len(kinds) == 0 {
		kinds = []string{"movie", "series"}
	}
	where = append(where, `i.kind IN (`+placeholders(len(kinds))+`)`)
	args = append(args, toAny(kinds)...)

	if q.MaxAge > 0 || q.MitAltersgrenze {
		// Folgen und Staffeln haben selten eine eigene Freigabe — dann gilt
		// die der Serie. Sonst sähe ein Kinderprofil über ?kind=episode
		// jede Folge einer Serie ab 18. Hat eine Folge eine eigene, zählt
		// die strengste in der Kette (Folge, Staffel, Serie): Eine Folge
		// ab 12 macht eine Serie ab 18 nicht kindertauglich. -1 heisst
		// „nirgends eine Freigabe".
		alter := `max(COALESCE(i.age_years, -1),
		    COALESCE((SELECT p.age_years FROM item p WHERE p.id = i.parent_id), -1),
		    COALESCE((SELECT gp.age_years FROM item p JOIN item gp ON gp.id = p.parent_id WHERE p.id = i.parent_id), -1))`
		if q.AllowUnrated {
			where = append(where, `(`+alter+` <= ?)`)
		} else {
			where = append(where, `(`+alter+` >= 0 AND `+alter+` <= ?)`)
		}
		args = append(args, q.MaxAge)
	}
	if b := strings.ToLower(strings.TrimSpace(q.Buchstabe)); b == "#" {
		where = append(where, `(substr(i.sort_title, 1, 1) < 'a' OR substr(i.sort_title, 1, 1) > 'z')`)
	} else if len(b) == 1 && b[0] >= 'a' && b[0] <= 'z' {
		where = append(where, `substr(i.sort_title, 1, 1) = ?`)
		args = append(args, b)
	}
	if q.Genre != "" {
		where = append(where, `EXISTS (SELECT 1 FROM genre g WHERE g.item_id = i.id AND g.name = ?)`)
		args = append(args, q.Genre)
	}
	if t := strings.TrimSpace(q.Text); t != "" {
		where = append(where,
			`EXISTS (SELECT 1 FROM item_search s WHERE s.item_id = i.id AND s.haystack LIKE ?)`)
		args = append(args, "%"+fold(strings.ToLower(t))+"%")
	}
	if p := strings.TrimSpace(q.Person); p != "" {
		where = append(where,
			`EXISTS (SELECT 1 FROM besetzung b WHERE b.item_id = i.id AND lower(b.name) LIKE ?)`)
		args = append(args, "%"+strings.ToLower(p)+"%")
	}
	if q.JahrVon > 0 {
		where = append(where, `i.year >= ?`)
		args = append(args, q.JahrVon)
	}
	if q.JahrBis > 0 {
		where = append(where, `i.year <= ?`)
		args = append(args, q.JahrBis)
	}
	return where, args, true
}

func (s *Store) Query(ctx context.Context, q Query) ([]Item, error) {
	where, args, ok := bedingung(q)
	if !ok {
		return []Item{}, nil
	}

	// Jede Sortierung hat für ihre übliche Richtung einen eigenen Index
	// (kind, …) — siehe nachruesten(). SQLite liest dann nur die eine
	// Seite, die gezeigt wird, statt alle Titel einer Art zu sortieren.
	// „Hinzugefügt" sortiert nach zuletzt_neu: Bei Serien zählt die
	// neueste Folge (UpsertItem führt es nach).
	schluessel, absteigend, _ := SortierungDeuten(q.Sort, q.Richtung)
	order := sqlOrdnung(schluessel, absteigend)
	if q.Sort == "relevanz" && strings.TrimSpace(q.Text) != "" {
		var rargs []any
		order, rargs = relevanzOrdnung(strings.TrimSpace(q.Text))
		args = append(args, rargs...)
	}
	if q.Sort == "liste" && len(q.IDs) > 0 && len(q.IDs) <= 2000 {
		// In der Reihenfolge der Kennungen (Merkliste, Favoriten).
		var b strings.Builder
		b.WriteString("CASE i.id")
		for n, id := range q.IDs {
			fmt.Fprintf(&b, " WHEN ? THEN %d", n)
			args = append(args, id)
		}
		b.WriteString(" END")
		order = b.String()
	}
	hoechstens := 500
	if len(q.IDs) > 0 {
		// Merkliste und Favoriten: alle auf einmal, die Liste ist begrenzt.
		hoechstens = 2000
	}
	if q.Limit <= 0 || q.Limit > hoechstens {
		q.Limit = 200
	}
	if q.Sort != "liste" && q.Sort != "relevanz" && rasterSortierungen[schluessel].inGo {
		return s.queryInGo(ctx, q, where, args, schluessel, absteigend)
	}

	sqlText := `SELECT ` + itemCols + ` FROM item i WHERE ` + strings.Join(where, " AND ") +
		` ORDER BY ` + order + ` LIMIT ? OFFSET ?`
	args = append(args, q.Limit, q.Offset)

	rows, err := s.db.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Item{}
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *it)
	}
	return out, rows.Err()
}

// TitelGleich sucht Titel einer Art in einer Bibliothek, die genau so
// heissen (ohne Rücksicht auf Gross- und Kleinschreibung) — über den
// Titel oder, bei mitSerienOriginal, auch über den Originaltitel.
//
// Der Scanner fragt das für jede neue Datei. Früher ging das über die
// Volltextsuche (LIKE '%titel%' auf item_search): Die kann keinen Index
// nutzen, und der erste Durchlauf über 20 000 Filme wurde quadratisch
// langsam. titel_klein hat einen Index (library_id, kind, titel_klein).
func (s *Store) TitelGleich(ctx context.Context, libraryID, kind, title string, mitOriginal bool) ([]Item, error) {
	k := titelKlein(title)
	q := `SELECT ` + itemCols + ` FROM item i
	       WHERE i.library_id = ? AND i.kind = ? AND i.titel_klein = ?`
	args := []any{libraryID, kind, k}
	if mitOriginal {
		// Der Originaltitel hat keinen eigenen Index: Gesucht wird nur
		// unter den Titeln dieser Art und Bibliothek (item_by_library).
		q = `SELECT ` + itemCols + ` FROM item i
		      WHERE i.library_id = ? AND i.kind = ? AND (i.titel_klein = ? OR i.original_klein = ?)`
		args = append(args, k)
	}
	rows, err := s.db.QueryContext(ctx, q+` ORDER BY i.sort_title, i.id LIMIT 100`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Item{}
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *it)
	}
	return out, rows.Err()
}

// titelKlein ist die Form, unter der TitelGleich sucht.
func titelKlein(t string) string { return strings.ToLower(strings.TrimSpace(t)) }

// Children liefert Staffeln einer Serie oder Folgen einer Staffel — in
// derselben Reihenfolge wie FolgenVon (folgenOrdnung).
func (s *Store) Children(ctx context.Context, parentID string) ([]Item, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+itemCols+` FROM item i WHERE parent_id = ?
		  ORDER BY `+folgenOrdnung, parentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Item{}
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *it)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------- Dateien

type MediaFile struct {
	ID         string `json:"id"`
	ItemID     string `json:"itemId"`
	Path       string `json:"path"`
	SizeBytes  int64  `json:"sizeBytes"`
	MTime      int64  `json:"-"`
	PartNo     int    `json:"partNo"`
	Edition    string `json:"edition,omitempty"`
	Container  string `json:"container,omitempty"`
	DurationMS int64  `json:"durationMs,omitempty"`
	Bitrate    int    `json:"bitrate,omitempty"`
	Width      int    `json:"width,omitempty"`
	Height     int    `json:"height,omitempty"`
	VideoCodec string `json:"videoCodec,omitempty"`
	HDR        string `json:"hdr,omitempty"`
	Missing    bool   `json:"missing,omitempty"`
	// Bevorzugt: Diese Fassung hat der Besitzer für den Titel gewählt; die
	// Geräte nehmen sie zuerst.
	Bevorzugt bool `json:"preferred,omitempty"`
	// Spuren und Kapitel — gefüllt, wenn die Datei ausgemessen ist.
	Spuren  []Track   `json:"tracks,omitempty"`
	Kapitel []Chapter `json:"chapters,omitempty"`
}

func (s *Store) UpsertFile(ctx context.Context, f *MediaFile) error {
	if f.ID == "" {
		f.ID = newID("f")
	}
	if f.PartNo == 0 {
		f.PartNo = 1
	}
	// Eine Transaktion für Datei, Kennung und Spuren — vorher waren es zwei
	// (und dazwischen eine Abfrage ausserhalb), also doppelt so viel WAL.
	return s.tx(ctx, func(t *sql.Tx) error { return dateiEintragen(ctx, t, f) })
}

func dateiEintragen(ctx context.Context, t *sql.Tx, f *MediaFile) error {
	_, err := t.ExecContext(ctx,
		`INSERT INTO media_file (id, item_id, path, size_bytes, mtime, part_no, edition,
		    container, duration_ms, bitrate, width, height, video_codec, hdr, missing_since)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,NULL)
		 ON CONFLICT (path) DO UPDATE SET
		   item_id=excluded.item_id, size_bytes=excluded.size_bytes, mtime=excluded.mtime,
		   part_no=excluded.part_no, edition=excluded.edition, container=excluded.container,
		   duration_ms=excluded.duration_ms, bitrate=excluded.bitrate, width=excluded.width,
		   height=excluded.height, video_codec=excluded.video_codec, hdr=excluded.hdr,
		   missing_since=NULL`,
		f.ID, f.ItemID, f.Path, f.SizeBytes, f.MTime, f.PartNo, nullStr(f.Edition),
		nullStr(f.Container), nullInt64(f.DurationMS), nullInt(f.Bitrate),
		nullInt(f.Width), nullInt(f.Height), nullStr(f.VideoCodec), nullStr(f.HDR))
	if err != nil {
		return err
	}
	// Gab es die Datei schon, bleibt ihre Kennung — die neue war umsonst.
	// Der Aufrufer soll mit der echten weiterarbeiten (Spuren speichern).
	if err := t.QueryRowContext(ctx, `SELECT id FROM media_file WHERE path = ?`, f.Path).Scan(&f.ID); err != nil {
		return err
	}
	if f.Spuren != nil || f.Kapitel != nil {
		return spurenSchreiben(ctx, t, f.ID, f.Spuren, f.Kapitel)
	}
	return nil
}

func (s *Store) FilesFor(ctx context.Context, itemID string) ([]MediaFile, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, item_id, path, size_bytes, mtime, part_no, COALESCE(edition,''),
		        COALESCE(container,''), COALESCE(duration_ms,0), COALESCE(bitrate,0),
		        COALESCE(width,0), COALESCE(height,0), COALESCE(video_codec,''),
		        COALESCE(hdr,''), missing_since,
		        id = COALESCE((SELECT bevorzugte_datei FROM item WHERE id = media_file.item_id), '')
		   FROM media_file WHERE item_id = ?
		  ORDER BY part_no,
		           id = COALESCE((SELECT bevorzugte_datei FROM item WHERE id = media_file.item_id), '') DESC,
		           COALESCE(height,0) DESC, size_bytes DESC`, itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []MediaFile{}
	for rows.Next() {
		var f MediaFile
		var missing sql.NullInt64
		if err := rows.Scan(&f.ID, &f.ItemID, &f.Path, &f.SizeBytes, &f.MTime, &f.PartNo,
			&f.Edition, &f.Container, &f.DurationMS, &f.Bitrate, &f.Width, &f.Height,
			&f.VideoCodec, &f.HDR, &missing, &f.Bevorzugt); err != nil {
			return nil, err
		}
		f.Missing = missing.Valid
		out = append(out, f)
	}
	return out, rows.Err()
}

func (s *Store) File(ctx context.Context, id string) (*MediaFile, error) {
	var f MediaFile
	var missing sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		`SELECT id, item_id, path, size_bytes, mtime, part_no, COALESCE(edition,''),
		        COALESCE(container,''), COALESCE(duration_ms,0), COALESCE(bitrate,0),
		        COALESCE(width,0), COALESCE(height,0), COALESCE(video_codec,''),
		        COALESCE(hdr,''), missing_since
		   FROM media_file WHERE id = ?`, id).
		Scan(&f.ID, &f.ItemID, &f.Path, &f.SizeBytes, &f.MTime, &f.PartNo, &f.Edition,
			&f.Container, &f.DurationMS, &f.Bitrate, &f.Width, &f.Height, &f.VideoCodec,
			&f.HDR, &missing)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	f.Missing = missing.Valid
	return &f, err
}

// BestFileFor wählt die Fassung, die zum Gerät passt: die grösste, die das
// Gerät noch direkt abspielen kann — sonst die beste und umwandeln.
func (s *Store) BestFileFor(ctx context.Context, itemID string) (*MediaFile, error) {
	files, err := s.FilesFor(ctx, itemID)
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		if !f.Missing {
			return &f, nil
		}
	}
	return nil, ErrNotFound
}

// MarkMissing merkt sich, dass eine Datei verschwunden ist, statt den Titel
// gleich zu löschen — eine abgehängte Platte soll die Mediathek nicht leeren.
func (s *Store) MarkMissing(ctx context.Context, libraryID string, seenPaths map[string]bool) (int, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT f.id, f.path FROM media_file f
		   JOIN item i ON i.id = f.item_id
		  WHERE i.library_id = ? AND f.missing_since IS NULL`, libraryID)
	if err != nil {
		return 0, err
	}
	type gone struct{ id, path string }
	list := []gone{}
	for rows.Next() {
		var g gone
		if err := rows.Scan(&g.id, &g.path); err != nil {
			rows.Close()
			return 0, err
		}
		if !seenPaths[g.path] {
			list = append(list, g)
		}
	}
	rows.Close()
	// Brach das Lesen mittendrin ab, fehlen in der Liste Dateien, die
	// gesehen wurden — und es fehlen andere. Lieber gar nichts vermerken.
	if err := rows.Err(); err != nil {
		return 0, err
	}

	// In einer Transaktion: Eine abgehängte Platte mit 10 000 Dateien
	// sind sonst 10 000 einzelne Schreibvorgänge.
	now := time.Now().Unix()
	err = s.tx(ctx, func(t *sql.Tx) error {
		for _, g := range list {
			if _, err := t.ExecContext(ctx,
				`UPDATE media_file SET missing_since = ? WHERE id = ?`, now, g.id); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return len(list), nil
}

// ---------------------------------------------------------------- Aufräumen

type Duplicate struct {
	ItemID string      `json:"itemId"`
	Title  string      `json:"title"`
	Year   int         `json:"year"`
	Files  []MediaFile `json:"files"`
}

// doppelVon findet Filme mit mehr als einer vorhandenen Datei.
//
// Erst die Kandidaten über den Index file_by_item allein (mehr als eine
// Datei, ob vorhanden oder nicht) — das liest keine einzige Tabellenzeile.
// Nur für diese wenigen wird dann nachgezählt, ohne die fehlenden. Vorher
// lief die Abfrage über jede Datei der Datenbank samt Zeile und
// sortierte mit count(DISTINCT); bei 120 000 Dateien dauerte die
// Übersicht der Verwaltung dadurch spürbar.
const doppelVon = `
	   FROM (SELECT item_id FROM media_file GROUP BY item_id HAVING count(*) > 1) d
	   JOIN item i ON i.id = d.item_id
	  WHERE i.kind = 'movie'
	    AND (SELECT count(*) FROM media_file f
	          WHERE f.item_id = d.item_id AND f.missing_since IS NULL) > 1`

// DoppelZahl zählt, was Duplicates liefern würde — für die Übersicht, die
// nur die Zahl zeigt.
func (s *Store) DoppelZahl(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) `+doppelVon).Scan(&n)
	return n, err
}

// FehlendZahl zählt die Dateien, die gerade fehlen (Index file_fehlt).
func (s *Store) FehlendZahl(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT count(*) FROM media_file WHERE missing_since IS NOT NULL`).Scan(&n)
	return n, err
}

func (s *Store) Duplicates(ctx context.Context) ([]Duplicate, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT i.id, i.title, COALESCE(i.year,0) `+doppelVon+`
		  ORDER BY i.sort_title`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Duplicate{}
	for rows.Next() {
		var d Duplicate
		if err := rows.Scan(&d.ItemID, &d.Title, &d.Year); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Files, _ = s.FilesFor(ctx, out[i].ItemID)
	}
	return out, nil
}

func (s *Store) MissingFiles(ctx context.Context) ([]MediaFile, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT f.id, f.item_id, f.path, f.size_bytes, f.mtime, f.part_no,
		        COALESCE(f.edition,''), COALESCE(f.container,''), COALESCE(f.duration_ms,0),
		        COALESCE(f.bitrate,0), COALESCE(f.width,0), COALESCE(f.height,0),
		        COALESCE(f.video_codec,''), COALESCE(f.hdr,''), f.missing_since
		   FROM media_file f WHERE f.missing_since IS NOT NULL
		  ORDER BY f.missing_since DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []MediaFile{}
	for rows.Next() {
		var f MediaFile
		var missing sql.NullInt64
		if err := rows.Scan(&f.ID, &f.ItemID, &f.Path, &f.SizeBytes, &f.MTime, &f.PartNo,
			&f.Edition, &f.Container, &f.DurationMS, &f.Bitrate, &f.Width, &f.Height,
			&f.VideoCodec, &f.HDR, &missing); err != nil {
			return nil, err
		}
		f.Missing = true
		out = append(out, f)
	}
	return out, rows.Err()
}

// PendingCount sind die Titel, für die noch keine Metadaten geholt wurden —
// entweder weil keine Quelle eingerichtet ist oder weil der Durchlauf noch
// läuft.
func (s *Store) PendingCount(ctx context.Context) int {
	var n int
	_ = s.db.QueryRowContext(ctx,
		`SELECT count(*) FROM item WHERE match_state = 'pending'
		   AND kind IN ('movie','series')`).Scan(&n)
	return n
}

func (s *Store) UnmatchedCount(ctx context.Context) int {
	var n int
	_ = s.db.QueryRowContext(ctx,
		`SELECT count(*) FROM item WHERE match_state = 'unmatched'`).Scan(&n)
	return n
}

// ---------------------------------------------------------------- Hilfen

// fold macht Umlaute und Akzente suchbar: „Grüsse" findet man auch als
// „grusse", und „Amélie" als „amelie". Nur für die Suche — sortiert wird
// nach Sortierschluessel (sortieren.go), dort zählt ä wie a.
func fold(s string) string {
	r := strings.NewReplacer(
		"ä", "ae", "ö", "oe", "ü", "ue", "ß", "ss",
		"à", "a", "á", "a", "â", "a", "ã", "a", "å", "a",
		"è", "e", "é", "e", "ê", "e", "ë", "e",
		"ì", "i", "í", "i", "î", "i", "ï", "i",
		"ò", "o", "ó", "o", "ô", "o", "õ", "o", "ø", "o",
		"ù", "u", "ú", "u", "û", "u",
		"ç", "c", "ñ", "n", "ý", "y",
	)
	return r.Replace(s)
}

// SuchFalten macht einen Namen vergleichbar wie die Titelsuche: klein, ohne
// Akzente, Umlaute als ae/oe/ue.
func SuchFalten(s string) string {
	return fold(strings.ToLower(s))
}

// ohnePunkte: „ärger" → „arger". Wer ohne Umlaut-Taste sucht, tippt das
// so; fold macht daraus „aerger". Im Suchtext stehen darum beide Formen.
func ohnePunkte(s string) string {
	return strings.NewReplacer("ä", "a", "ö", "o", "ü", "u").Replace(s)
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}
func nullInt(i int) any {
	if i == 0 {
		return nil
	}
	return i
}
func nullInt64(i int64) any {
	if i == 0 {
		return nil
	}
	return i
}
func nullFloat(f float64) any {
	if f == 0 {
		return nil
	}
	return f
}

// ---------------------------------------------------------------- Metadaten

// PendingItems sind die Titel, für die noch keine Metadaten geholt wurden.
// Serien kommen vor ihren Folgen, weil die Folgen die Kennung der Serie
// brauchen.
func (s *Store) PendingItems(ctx context.Context, libraryID string, limit int) ([]Item, error) {
	if limit <= 0 {
		limit = 200
	}
	// Staffeln holt niemand einzeln (die Serie bringt sie mit), und eine
	// Folge lässt sich erst nachschlagen, wenn ihre Serie zugeordnet ist.
	// Ohne diese Bedingung blieben solche Titel für immer „wartend“ und
	// belegten die vordersten Plätze: Jeder Durchlauf prüfte dieselben
	// Folgen einer unbekannten Serie, und neue Folgen kamen nie dran.
	q := `SELECT ` + itemCols + ` FROM item i WHERE i.match_state = 'pending'
	   AND (i.kind IN ('movie','series')
	        OR (i.kind = 'episode' AND EXISTS (
	              SELECT 1 FROM item st JOIN item se ON se.id = st.parent_id
	               WHERE st.id = i.parent_id AND COALESCE(se.tmdb_id,'') != '')))`
	args := []any{}
	if libraryID != "" {
		q += ` AND i.library_id = ?`
		args = append(args, libraryID)
	}
	q += ` ORDER BY CASE i.kind WHEN 'series' THEN 0 WHEN 'movie' THEN 1
	                            WHEN 'season' THEN 2 ELSE 3 END, i.added_at
	       LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Item{}
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *it)
	}
	return out, rows.Err()
}

// SeriesOf findet zu einer Folge die Serie — über die Staffel dazwischen.
func (s *Store) SeriesOf(ctx context.Context, episodeID string) (string, error) {
	var seasonID, seriesID string
	err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(parent_id,'') FROM item WHERE id = ?`, episodeID).Scan(&seasonID)
	if err != nil {
		return "", err
	}
	if seasonID == "" {
		return "", ErrNotFound
	}
	err = s.db.QueryRowContext(ctx,
		`SELECT COALESCE(parent_id,'') FROM item WHERE id = ?`, seasonID).Scan(&seriesID)
	if err != nil {
		return "", err
	}
	if seriesID == "" {
		return "", ErrNotFound
	}
	return seriesID, nil
}

// SetSourceID merkt sich, unter welcher Kennung ein Titel bei einer Quelle
// geführt wird — sonst müsste jede Folge die Serie neu suchen.
func (s *Store) SetSourceID(ctx context.Context, itemID, source, id string) error {
	col := map[string]string{"tmdb": "tmdb_id", "tvdb": "tvdb_id", "imdb": "imdb_id"}[source]
	if col == "" {
		return errors.New("unbekannte quelle")
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE item SET `+col+` = ? WHERE id = ?`, id, itemID)
	return err
}

func (s *Store) SourceID(ctx context.Context, itemID, source string) (string, error) {
	col := map[string]string{"tmdb": "tmdb_id", "tvdb": "tvdb_id", "imdb": "imdb_id"}[source]
	if col == "" {
		return "", errors.New("unbekannte quelle")
	}
	var v sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT `+col+` FROM item WHERE id = ?`, itemID).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return v.String, err
}

// ---------------------------------------------------------------- Bilder

type Artwork struct {
	Kind string `json:"kind"`
	Path string `json:"path"`
}

func (s *Store) SetArtwork(ctx context.Context, itemID, kind, path, sourceURL string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO artwork (item_id, kind, path, source_url) VALUES (?,?,?,?)
		 ON CONFLICT (item_id, kind) DO UPDATE SET
		   path = excluded.path, source_url = excluded.source_url
		 WHERE NOT EXISTS (SELECT 1 FROM feld_sperre x
		                    WHERE x.item_id = excluded.item_id AND x.feld = excluded.kind)`,
		itemID, kind, path, nullStr(sourceURL))
	return err
}

// PendingUnter sind die wartenden Titel eines Films oder einer Serie samt
// deren Folgen — für „Zuweisen" und „Neu laden", die genau diesen Titel
// meinen und nicht die ersten paar hundert der ganzen Mediathek.
func (s *Store) PendingUnter(ctx context.Context, id string) ([]Item, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+itemCols+` FROM item i
		WHERE i.match_state = 'pending' AND i.kind != 'season'
		  AND (i.id = ? OR i.parent_id = ?
		       OR i.parent_id IN (SELECT id FROM item WHERE parent_id = ?))
		ORDER BY CASE i.kind WHEN 'series' THEN 0 WHEN 'movie' THEN 1 ELSE 2 END,
		         i.season_no, i.episode_no, i.id`, id, id, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Item{}
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *it)
	}
	return out, rows.Err()
}

// BildQuelle ist die Adresse, von der das gespeicherte Bild stammt ("" wenn
// keins da ist).
func (s *Store) BildQuelle(ctx context.Context, itemID, kind string) string {
	var u sql.NullString
	_ = s.db.QueryRowContext(ctx,
		`SELECT source_url FROM artwork WHERE item_id = ? AND kind = ?`, itemID, kind).Scan(&u)
	return u.String
}

func (s *Store) ArtworkFor(ctx context.Context, itemIDs []string) (map[string]map[string]string, error) {
	out := map[string]map[string]string{}
	if len(itemIDs) == 0 {
		return out, nil
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT item_id, kind, path FROM artwork WHERE item_id IN (`+
			placeholders(len(itemIDs))+`)`, toAny(itemIDs)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var id, kind, path string
		if err := rows.Scan(&id, &kind, &path); err != nil {
			return nil, err
		}
		if out[id] == nil {
			out[id] = map[string]string{}
		}
		out[id][kind] = path
	}
	return out, rows.Err()
}

// UnmatchedItems sind die Titel, die der Besitzer von Hand zuordnen muss.
func (s *Store) UnmatchedItems(ctx context.Context, limit int) ([]Item, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+itemCols+` FROM item i WHERE i.match_state = 'unmatched'
		  ORDER BY i.added_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Item{}
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *it)
	}
	return out, rows.Err()
}

// ItemZuPfad nennt den Titel, an dem eine Datei schon hängt — oder "".
// Der Scanner fragt das zuerst: eine Datei, die Lumen kennt, gehört zu
// ihrem Titel, auch wenn eine neuere Fassung den Dateinamen etwas
// anders liest. Sonst entstünde ein zweiter Titel, und am alten blieben
// Merkliste, Stand und Listeneinträge ohne Datei zurück.
func (s *Store) ItemZuPfad(ctx context.Context, path string) (string, error) {
	var id string
	err := s.db.QueryRowContext(ctx, `SELECT item_id FROM media_file WHERE path = ?`, path).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return id, err
}

// SerieZuTitel nennt die Serie, zu der ein Titel gehört — er selbst, seine
// Staffel oder deren Serie —, oder "". Eine Abfrage statt drei: Der
// Scanner fragt das für jede Folge.
func (s *Store) SerieZuTitel(ctx context.Context, itemID string) (string, error) {
	var id sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT CASE WHEN e.kind = 'series' THEN e.id
		             WHEN st.kind = 'series' THEN st.id
		             WHEN sr.kind = 'series' THEN sr.id END
		   FROM item e
		   LEFT JOIN item st ON st.id = e.parent_id
		   LEFT JOIN item sr ON sr.id = st.parent_id
		  WHERE e.id = ?`, itemID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return id.String, err
}

// Count zählt, was Query liefern würde — ohne Limit.
func (s *Store) Count(ctx context.Context, q Query) (int, error) {
	where, args, ok := bedingung(q)
	if !ok {
		return 0, nil
	}
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM item i WHERE `+strings.Join(where, " AND "), args...).Scan(&n)
	return n, err
}

// Genres nennt die Genres aller Titel, die Query liefern würde, mit
// Häufigkeit — für die Filterknöpfe über dem Raster. Aus den ersten
// sechzig geladenen Titeln liessen sie sich nicht ableiten.
func (s *Store) Genres(ctx context.Context, q Query) ([]string, error) {
	return s.genresZu(ctx, q, 14)
}

// AlleGenres: ohne die Grenze von 14 — fürs Deuten einer Sucheingabe.
func (s *Store) AlleGenres(ctx context.Context, q Query) ([]string, error) {
	return s.genresZu(ctx, q, 500)
}

func (s *Store) genresZu(ctx context.Context, q Query, limit int) ([]string, error) {
	where, args, ok := bedingung(q)
	if !ok {
		return []string{}, nil
	}
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx,
		`SELECT g.name FROM genre g WHERE g.item_id IN
		   (SELECT i.id FROM item i WHERE `+strings.Join(where, " AND ")+`)
		 GROUP BY g.name ORDER BY COUNT(*) DESC, g.name LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Die Knöpfe nach dem Alphabet, wie es ein Mensch liest: „Émotion"
	// bei E, nicht hinter Z.
	sort.SliceStable(out, func(i, j int) bool {
		a, b := Sortierschluessel(out[i], ""), Sortierschluessel(out[j], "")
		if a != b {
			return a < b
		}
		return out[i] < out[j]
	})
	return out, nil
}

// Besetzung: wer in einem Titel mitspielt.
type Person struct {
	Name  string `json:"name"`
	Rolle string `json:"rolle,omitempty"`
	Bild  string `json:"bild,omitempty"` // Datei im Bilderordner
}

func (s *Store) BesetzungSetzen(ctx context.Context, itemID string, leute []Person) error {
	return s.tx(ctx, func(t *sql.Tx) error {
		// Von Hand gepflegt: keine Quelle schreibt darüber (sperren.go).
		if gesperrt(ctx, t, itemID, FeldBesetzung) {
			return nil
		}
		if _, err := t.ExecContext(ctx, `DELETE FROM besetzung WHERE item_id = ?`, itemID); err != nil {
			return err
		}
		for i, p := range leute {
			if _, err := t.ExecContext(ctx,
				`INSERT INTO besetzung (item_id, ord, name, rolle, bild) VALUES (?,?,?,?,?)`,
				itemID, i, p.Name, nullStr(p.Rolle), nullStr(p.Bild)); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Store) Besetzung(ctx context.Context, itemID string) ([]Person, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT name, COALESCE(rolle,''), COALESCE(bild,'') FROM besetzung WHERE item_id = ? ORDER BY ord`, itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Person{}
	for rows.Next() {
		var p Person
		if err := rows.Scan(&p.Name, &p.Rolle, &p.Bild); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// FassungWaehlen legt fest, welche Datei eines Titels die Geräte zuerst
// nehmen (etwa „American Psycho" als MKV in 1080p und als AVI in
// 400p). Leere Kennung = wieder die grösste.
func (s *Store) FassungWaehlen(ctx context.Context, itemID, fileID string) error {
	if fileID != "" {
		var n int
		_ = s.db.QueryRowContext(ctx, `SELECT count(*) FROM media_file WHERE id = ? AND item_id = ?`,
			fileID, itemID).Scan(&n)
		if n != 1 {
			return errors.New("diese datei gehört nicht zu diesem titel")
		}
	}
	_, err := s.db.ExecContext(ctx, `UPDATE item SET bevorzugte_datei = ? WHERE id = ?`, nullStr(fileID), itemID)
	return err
}

// DateiAbtrennen löst eine Datei von ihrem Titel und macht einen eigenen
// Film daraus — für zwei Filme, die fälschlich als zwei Fassungen eines
// Films erkannt wurden („Bibi & Tina: Voll verhext!" und „Bibi & Tina – Der
// Film", beide 2014). Der neue Film holt seine Metadaten beim nächsten
// Durchgang selbst.
func (s *Store) DateiAbtrennen(ctx context.Context, fileID, titel string, jahr int) (*Item, error) {
	f, err := s.File(ctx, fileID)
	if err != nil {
		return nil, err
	}
	alt, err := s.Item(ctx, f.ItemID)
	if err != nil {
		return nil, err
	}
	if alt.Kind != "movie" {
		return nil, errors.New("abtrennen geht nur bei filmen")
	}
	dateien, _ := s.FilesFor(ctx, alt.ID)
	if len(dateien) < 2 {
		return nil, errors.New("der titel hat nur diese eine datei")
	}
	if strings.TrimSpace(titel) == "" {
		titel = alt.Title
	}
	neu := &Item{LibraryID: alt.LibraryID, Kind: "movie", Title: titel, Year: jahr, MatchState: "pending"}
	if err := s.UpsertItem(ctx, neu); err != nil {
		return nil, err
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE media_file SET item_id = ?, part_no = 1 WHERE id = ?`, neu.ID, fileID); err != nil {
		return nil, err
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE item SET bevorzugte_datei = NULL WHERE id = ? AND bevorzugte_datei = ?`, alt.ID, fileID)
	return neu, nil
}

// DateiAustragen nimmt eine Datei aus der Datenbank (die Datei selbst
// bleibt, wo sie ist).
func (s *Store) DateiAustragen(ctx context.Context, fileID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM media_file WHERE id = ?`, fileID)
	return err
}

// TomatenSetzen hält die Wertung von Rotten Tomatoes fest (-1: keine).
func (s *Store) TomatenSetzen(ctx context.Context, id string, prozent int) error {
	if prozent < -1 || prozent > 100 {
		prozent = -1
	}
	_, err := s.db.ExecContext(ctx, `UPDATE item SET rt_wertung = ? WHERE id = ?`, prozent, id)
	return err
}

// ErsterPfad: der Pfad einer Datei zu einem Film oder einer Serie (bei
// Serien die erste Folge) — damit man beim Zuordnen sieht, welche Datei
// gemeint ist.
func (s *Store) ErsterPfad(ctx context.Context, id string) string {
	var p string
	_ = s.db.QueryRowContext(ctx,
		`SELECT f.path FROM media_file f JOIN item e ON e.id = f.item_id
		   LEFT JOIN item st ON st.id = e.parent_id
		  WHERE e.id = ?1 OR e.parent_id = ?1 OR st.parent_id = ?1
		  ORDER BY e.season_no, e.episode_no, f.path LIMIT 1`, id).Scan(&p)
	return p
}

// SerienOhneIMDb: zugeordnete Serien ohne IMDb-Kennung, bei denen noch
// nicht nachgefragt wurde.
func (s *Store) SerienOhneIMDb(ctx context.Context, limit int) ([]Item, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+itemCols+` FROM item WHERE kind = 'series' AND match_state IN ('matched','manual')
		   AND COALESCE(imdb_id,'') = '' AND rt_wertung IS NULL LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Item
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *it)
	}
	return out, rows.Err()
}

// WertungenSetzen hält IMDb-Kennung, -Note und Rotten Tomatoes fest.
// tomaten -2 heisst „nicht gefragt"; dann gilt der Titel trotzdem als
// erledigt (-1), damit er nicht bei jedem Durchlauf wiederkommt.
func (s *Store) WertungenSetzen(ctx context.Context, id, imdbID string, note float64, stimmen, tomaten int) error {
	if tomaten < -1 || tomaten > 100 {
		tomaten = -1
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE item SET imdb_id = NULLIF(?, ''), imdb_rating = CASE WHEN ? > 0 THEN ? ELSE imdb_rating END,
		        imdb_votes = CASE WHEN ? > 0 THEN ? ELSE imdb_votes END, rt_wertung = ? WHERE id = ?`,
		imdbID, note, note, stimmen, stimmen, tomaten, id)
	return err
}

// OhneTomaten: Filme und Serien mit IMDb-Kennung, bei denen noch nie nach
// Rotten Tomatoes gefragt wurde.
func (s *Store) OhneTomaten(ctx context.Context, limit int) ([]Item, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+itemCols+` FROM item WHERE kind IN ('movie','series')
		   AND COALESCE(imdb_id,'') <> '' AND rt_wertung IS NULL LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Item
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *it)
	}
	return out, rows.Err()
}

// SerienZeitraum hält fest, bis wann eine Serie lief und ob sie noch läuft.
func (s *Store) SerienZeitraum(ctx context.Context, id string, endYear int, laeuftNoch bool) error {
	_, err := s.db.ExecContext(ctx, `UPDATE item SET end_year = ?, laeuft_noch = ?, updated_at = ? WHERE id = ?`,
		nullInt(endYear), b2i(laeuftNoch), time.Now().Unix(), id)
	return err
}

// SerienOhneZeitraum: zugeordnete Serien, deren Zeitraum noch fehlt — und
// solche, die zuletzt noch liefen (vielleicht sind sie inzwischen zu Ende).
func (s *Store) SerienOhneZeitraum(ctx context.Context, limit int) ([]Item, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+itemCols+` FROM item WHERE kind = 'series' AND match_state IN ('matched','manual')
		   AND (laeuft_noch IS NULL OR (laeuft_noch = 1 AND updated_at < ?))
		 ORDER BY laeuft_noch IS NOT NULL, updated_at LIMIT ?`,
		time.Now().Add(-7*24*time.Hour).Unix(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Item
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *it)
	}
	return out, rows.Err()
}

// Buchstaben zählt die Titel je Anfangsbuchstaben („#" für Zahlen und
// Zeichen) — für die Buchstabenleiste, damit nur Buchstaben antippbar
// sind, unter denen auch etwas steht.
func (s *Store) Buchstaben(ctx context.Context, q Query) (map[string]int, error) {
	q.Buchstabe = ""
	where, args, ok := bedingung(q)
	out := map[string]int{}
	if !ok {
		return out, nil
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT substr(i.sort_title, 1, 1), count(*) FROM item i WHERE `+strings.Join(where, " AND ")+
			` GROUP BY 1`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var b string
		var n int
		if err := rows.Scan(&b, &n); err != nil {
			return nil, err
		}
		if len(b) != 1 || b[0] < 'a' || b[0] > 'z' {
			b = "#"
		} else {
			b = strings.ToUpper(b)
		}
		out[b] += n
	}
	return out, rows.Err()
}

// PersonInfo liest die zwischengespeicherten Angaben zu einer Person.
func (s *Store) PersonInfo(ctx context.Context, name string) (daten string, geholt time.Time, ok bool) {
	var t int64
	if err := s.db.QueryRowContext(ctx, `SELECT daten, geholt FROM person_info WHERE name = ?`,
		strings.ToLower(strings.TrimSpace(name))).Scan(&daten, &t); err != nil {
		return "", time.Time{}, false
	}
	return daten, time.Unix(t, 0), true
}

func (s *Store) SetPersonInfo(ctx context.Context, name, daten string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO person_info (name, daten, geholt) VALUES (?,?,?)
		 ON CONFLICT (name) DO UPDATE SET daten = excluded.daten, geholt = excluded.geholt`,
		strings.ToLower(strings.TrimSpace(name)), daten, time.Now().Unix())
	return err
}
