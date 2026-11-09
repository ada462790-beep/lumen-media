// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

// Musik: was auf der Platte liegt — Titel, Alben, Interpreten. Nichts in
// dieser Datei weiss, wer was hört; das steht in musik_privat.go.

import (
	"context"
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

type MusikTitel struct {
	ID             string    `json:"id"`
	LibraryID      string    `json:"libraryId"`
	AlbumID        string    `json:"albumId"`
	Pfad           string    `json:"-"`
	Groesse        int64     `json:"-"`
	MTime          int64     `json:"-"`
	Titel          string    `json:"titel"`
	Interpret      string    `json:"interpret"`
	InterpretID    string    `json:"interpretId,omitempty"`
	AlbumInterpret string    `json:"albumInterpret,omitempty"`
	Album          string    `json:"album"`
	Track          int       `json:"track,omitempty"`
	Disc           int       `json:"disc,omitempty"`
	Jahr           int       `json:"jahr,omitempty"`
	Genre          string    `json:"genre,omitempty"`
	Komponist      string    `json:"komponist,omitempty"`
	DauerMS        int64     `json:"dauerMs"`
	Compilation    bool      `json:"compilation,omitempty"`
	Explizit       bool      `json:"explizit,omitempty"`
	Codec          string    `json:"codec"`
	Container      string    `json:"container"`
	Bitrate        int       `json:"bitrate,omitempty"`
	Abtastrate     int       `json:"abtastrate,omitempty"`
	HatCover       bool      `json:"-"`
	Hinzugefuegt   time.Time `json:"hinzugefuegt"`
	Fehlt          bool      `json:"fehlt,omitempty"`
}

type MusikAlbum struct {
	ID           string    `json:"id"`
	LibraryID    string    `json:"libraryId"`
	Name         string    `json:"name"`
	Interpret    string    `json:"interpret"`
	InterpretID  string    `json:"interpretId,omitempty"`
	Jahr         int       `json:"jahr,omitempty"`
	Genre        string    `json:"genre,omitempty"`
	Compilation  bool      `json:"compilation,omitempty"`
	CoverQuelle  string    `json:"-"`
	Anzahl       int       `json:"anzahl"`
	DauerMS      int64     `json:"dauerMs"`
	Hinzugefuegt time.Time `json:"hinzugefuegt"`
}

type MusikInterpret struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Alben int    `json:"alben"`
	// Das Album, dessen Cover für den Interpreten steht.
	CoverAlbum string `json:"coverAlbum,omitempty"`
}

type MusikGenre struct {
	Name       string `json:"name"`
	Alben      int    `json:"alben"`
	CoverAlbum string `json:"coverAlbum,omitempty"`
}

// MusikSicht ist, was eine Person sehen darf: ihre Bibliotheken — und ob
// Titel mit dem Vermerk „explizit" für sie ausgeblendet werden (Kinder
// und Profile mit einer Altersgrenze unter 16).
type MusikSicht struct {
	Bibliotheken []string
	OhneExplizit bool
}

// InterpretID macht aus einem Namen die Kennung. Derselbe Name ergibt
// immer dieselbe Kennung — auch über Bibliotheken und Durchläufe hinweg,
// darum halten Favoriten.
func InterpretID(name string) string {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "" {
		return ""
	}
	h := sha1.Sum([]byte("interpret\x00" + n))
	return "ar" + hex.EncodeToString(h[:8])
}

// AlbumID: aus Bibliothek, Ordner und Albumname.
func AlbumID(libraryID, ordner, album string) string {
	h := sha1.Sum([]byte("album\x00" + libraryID + "\x00" + ordner + "\x00" + strings.ToLower(strings.TrimSpace(album))))
	return "al" + hex.EncodeToString(h[:8])
}

// MusikSortKey: Artikel nach hinten, wie bei Filmen — „The Beatles" steht
// unter B.
func MusikSortKey(s string) string { return SortKey(s) }

// titelBedingung: sichtbar für diese Person, und die Datei ist da.
func (v MusikSicht) titelBedingung(alias string) (string, []any) {
	if len(v.Bibliotheken) == 0 {
		return "0", nil
	}
	w := alias + ".library_id IN (" + placeholders(len(v.Bibliotheken)) + ") AND " + alias + ".fehlt_seit IS NULL"
	if v.OhneExplizit {
		w += " AND " + alias + ".explizit = 0"
	}
	return w, toAny(v.Bibliotheken)
}

// albumBedingung: ein Album ist sichtbar, wenn mindestens einer seiner
// Titel sichtbar ist.
func (v MusikSicht) albumBedingung(alias string) (string, []any) {
	tw, ta := v.titelBedingung("tt")
	return "EXISTS (SELECT 1 FROM musik_titel tt WHERE tt.album_id = " + alias + ".id AND " + tw + ")", ta
}

// ---------------------------------------------------------------- Scanner

// MusikStand: was der Scanner über eine Datei schon weiss.
type MusikStand struct {
	ID      string
	Groesse int64
	MTime   int64
	Fehlt   bool
}

// MusikDateien nennt alle bekannten Dateien einer Bibliothek. Der Durchlauf
// fragt hier, bevor er eine Datei ausmisst: Grösse und Datum gleich heisst
// unverändert — dann wird sie nicht neu gelesen.
func (s *Store) MusikDateien(ctx context.Context, libraryID string) (map[string]MusikStand, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT pfad, id, groesse, mtime, fehlt_seit IS NOT NULL FROM musik_titel WHERE library_id = ?`, libraryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]MusikStand{}
	for rows.Next() {
		var p string
		var st MusikStand
		if err := rows.Scan(&p, &st.ID, &st.Groesse, &st.MTime, &st.Fehlt); err != nil {
			return nil, err
		}
		out[p] = st
	}
	return out, rows.Err()
}

// MusikTitelSpeichern legt einen Titel an oder bringt ihn auf Stand. Die
// Kennung und „hinzugefügt" bleiben, wenn der Pfad schon bekannt ist.
func (s *Store) MusikTitelSpeichern(ctx context.Context, t *MusikTitel) (neu bool, err error) {
	var id string
	err = s.db.QueryRowContext(ctx, `SELECT id FROM musik_titel WHERE pfad = ?`, t.Pfad).Scan(&id)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		neu = true
		t.ID = newID("mt")
		if t.Hinzugefuegt.IsZero() {
			t.Hinzugefuegt = time.Now()
		}
	case err != nil:
		return false, err
	default:
		t.ID = id
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO musik_titel (id, library_id, album_id, pfad, groesse, mtime, titel, titel_sort,
		   interpret, albuminterpret, album, track, disc, jahr, genre, komponist, dauer_ms,
		   compilation, explizit, codec, container, bitrate, abtastrate, hat_cover, hinzugefuegt, fehlt_seit)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,NULL)
		 ON CONFLICT (pfad) DO UPDATE SET
		   library_id=excluded.library_id, groesse=excluded.groesse, mtime=excluded.mtime,
		   titel=excluded.titel, titel_sort=excluded.titel_sort, interpret=excluded.interpret,
		   albuminterpret=excluded.albuminterpret, album=excluded.album, track=excluded.track,
		   disc=excluded.disc, jahr=excluded.jahr, genre=excluded.genre, komponist=excluded.komponist,
		   dauer_ms=excluded.dauer_ms, compilation=excluded.compilation, explizit=excluded.explizit,
		   codec=excluded.codec, container=excluded.container, bitrate=excluded.bitrate,
		   abtastrate=excluded.abtastrate, hat_cover=excluded.hat_cover, fehlt_seit=NULL`,
		t.ID, t.LibraryID, t.AlbumID, t.Pfad, t.Groesse, t.MTime, t.Titel, MusikSortKey(t.Titel),
		t.Interpret, t.AlbumInterpret, t.Album, t.Track, t.Disc, t.Jahr, t.Genre, t.Komponist, t.DauerMS,
		b2i(t.Compilation), b2i(t.Explizit), t.Codec, t.Container, t.Bitrate, t.Abtastrate,
		b2i(t.HatCover), t.Hinzugefuegt.Unix())
	return neu, err
}

// MusikWiederDa: Eine bekannte, unveränderte Datei ist (wieder) da.
func (s *Store) MusikWiederDa(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE musik_titel SET fehlt_seit = NULL WHERE id = ? AND fehlt_seit IS NOT NULL`, id)
	return err
}

// MusikFehlende vermerkt, welche Dateien nicht mehr da sind. Sie werden
// nicht gelöscht: eine Platte, die kurz fehlt, soll niemandem seine
// Wiedergabelisten nehmen. Nach 30 Tagen Abwesenheit fliegen sie raus.
func (s *Store) MusikFehlende(ctx context.Context, libraryID string, gesehen map[string]bool) (int, error) {
	bekannt, err := s.MusikDateien(ctx, libraryID)
	if err != nil {
		return 0, err
	}
	n := 0
	jetzt := time.Now().Unix()
	for p, st := range bekannt {
		if gesehen[p] || st.Fehlt {
			continue
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE musik_titel SET fehlt_seit = ? WHERE id = ?`, jetzt, st.ID); err != nil {
			return n, err
		}
		n++
	}
	_, err = s.db.ExecContext(ctx, `DELETE FROM musik_titel WHERE library_id = ? AND fehlt_seit IS NOT NULL AND fehlt_seit < ?`,
		libraryID, jetzt-30*86400)
	return n, err
}

// MusikTitelDerBibliothek: alle Titel einer Bibliothek — für das Bilden
// der Alben nach dem Durchlauf.
func (s *Store) MusikTitelDerBibliothek(ctx context.Context, libraryID string) ([]MusikTitel, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+musikTitelCols+` FROM musik_titel t WHERE t.library_id = ?`, libraryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMusikTitel(rows)
}

// MusikAlbenSetzen schreibt die Alben einer Bibliothek neu: legt an, was
// neu ist, bringt den Rest auf Stand und räumt Alben weg, zu denen kein
// Titel mehr gehört. zuordnung sagt, welcher Titel zu welchem Album gehört.
func (s *Store) MusikAlbenSetzen(ctx context.Context, libraryID string, alben []MusikAlbum, zuordnung map[string]string) error {
	return s.tx(ctx, func(t *sql.Tx) error {
		jetzt := time.Now().Unix()
		ids := make([]any, 0, len(alben))
		for _, a := range alben {
			ids = append(ids, a.ID)
			hinzu := a.Hinzugefuegt.Unix()
			if a.Hinzugefuegt.IsZero() {
				hinzu = jetzt
			}
			if _, err := t.ExecContext(ctx,
				`INSERT INTO musik_album (id, library_id, name, name_sort, interpret, interpret_id, jahr, genre,
				   compilation, cover_quelle, titel_anzahl, dauer_ms, hinzugefuegt)
				 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)
				 ON CONFLICT (id) DO UPDATE SET name=excluded.name, name_sort=excluded.name_sort,
				   interpret=excluded.interpret, interpret_id=excluded.interpret_id, jahr=excluded.jahr,
				   genre=excluded.genre, compilation=excluded.compilation, cover_quelle=excluded.cover_quelle,
				   titel_anzahl=excluded.titel_anzahl, dauer_ms=excluded.dauer_ms`,
				a.ID, libraryID, a.Name, MusikSortKey(a.Name), a.Interpret, a.InterpretID, a.Jahr, a.Genre,
				b2i(a.Compilation), a.CoverQuelle, a.Anzahl, a.DauerMS, hinzu); err != nil {
				return err
			}
			if a.InterpretID != "" {
				if _, err := t.ExecContext(ctx,
					`INSERT INTO musik_interpret (id, name, name_sort) VALUES (?,?,?)
					 ON CONFLICT (id) DO UPDATE SET name=excluded.name, name_sort=excluded.name_sort`,
					a.InterpretID, a.Interpret, MusikSortKey(a.Interpret)); err != nil {
					return err
				}
			}
		}
		for titel, album := range zuordnung {
			if _, err := t.ExecContext(ctx, `UPDATE musik_titel SET album_id = ? WHERE id = ? AND album_id != ?`,
				album, titel, album); err != nil {
				return err
			}
		}
		q := `DELETE FROM musik_album WHERE library_id = ?`
		args := []any{libraryID}
		if len(ids) > 0 {
			q += ` AND id NOT IN (` + placeholders(len(ids)) + `)`
			args = append(args, ids...)
		}
		if _, err := t.ExecContext(ctx, q, args...); err != nil {
			return err
		}
		_, err := t.ExecContext(ctx,
			`DELETE FROM musik_interpret WHERE id NOT IN (SELECT interpret_id FROM musik_album)`)
		return err
	})
}

// ---------------------------------------------------------------- Lesen

const musikTitelCols = `t.id, t.library_id, t.album_id, t.pfad, t.groesse, t.mtime, t.titel, t.interpret,
	t.albuminterpret, t.album, t.track, t.disc, t.jahr, t.genre, t.komponist, t.dauer_ms, t.compilation,
	t.explizit, t.codec, t.container, t.bitrate, t.abtastrate, t.hat_cover, t.hinzugefuegt,
	t.fehlt_seit IS NOT NULL`

func scanMusikTitel(rows *sql.Rows) ([]MusikTitel, error) {
	out := []MusikTitel{}
	for rows.Next() {
		var t MusikTitel
		var comp, expl, cover int
		var hinzu int64
		if err := rows.Scan(&t.ID, &t.LibraryID, &t.AlbumID, &t.Pfad, &t.Groesse, &t.MTime, &t.Titel,
			&t.Interpret, &t.AlbumInterpret, &t.Album, &t.Track, &t.Disc, &t.Jahr, &t.Genre, &t.Komponist,
			&t.DauerMS, &comp, &expl, &t.Codec, &t.Container, &t.Bitrate, &t.Abtastrate, &cover, &hinzu,
			&t.Fehlt); err != nil {
			return nil, err
		}
		t.Compilation, t.Explizit, t.HatCover = comp == 1, expl == 1, cover == 1
		t.Hinzugefuegt = time.Unix(hinzu, 0)
		t.InterpretID = InterpretID(t.Interpret)
		out = append(out, t)
	}
	return out, rows.Err()
}

const musikAlbumCols = `a.id, a.library_id, a.name, a.interpret, a.interpret_id, a.jahr, a.genre,
	a.compilation, a.cover_quelle, a.titel_anzahl, a.dauer_ms, a.hinzugefuegt`

func scanMusikAlben(rows *sql.Rows) ([]MusikAlbum, error) {
	out := []MusikAlbum{}
	for rows.Next() {
		var a MusikAlbum
		var comp int
		var hinzu int64
		if err := rows.Scan(&a.ID, &a.LibraryID, &a.Name, &a.Interpret, &a.InterpretID, &a.Jahr, &a.Genre,
			&comp, &a.CoverQuelle, &a.Anzahl, &a.DauerMS, &hinzu); err != nil {
			return nil, err
		}
		a.Compilation = comp == 1
		a.Hinzugefuegt = time.Unix(hinzu, 0)
		out = append(out, a)
	}
	return out, rows.Err()
}

// MusikAlbenAbfrage: wonach die Alben gefiltert und sortiert werden.
type MusikAlbenAbfrage struct {
	Sortierung  string // name | interpret | jahr | neu
	Genre       string
	InterpretID string
	IDs         []string
	Offset      int
	Limit       int
}

func musikAlbenOrdnung(s string) string {
	switch s {
	case "interpret":
		return `a.compilation, (SELECT name_sort FROM musik_interpret i WHERE i.id = a.interpret_id), a.jahr, a.name_sort`
	case "jahr":
		return `a.jahr DESC, a.name_sort`
	case "neu":
		return `a.hinzugefuegt DESC, a.name_sort`
	default:
		return `a.name_sort, a.id`
	}
}

// MusikAlben liefert eine Seite Alben und die Gesamtzahl.
func (s *Store) MusikAlben(ctx context.Context, v MusikSicht, q MusikAlbenAbfrage) ([]MusikAlbum, int, error) {
	w, args := v.albumBedingung("a")
	if q.Genre != "" {
		w += ` AND a.genre = ? COLLATE NOCASE`
		args = append(args, q.Genre)
	}
	if q.InterpretID != "" {
		w += ` AND a.interpret_id = ?`
		args = append(args, q.InterpretID)
	}
	if len(q.IDs) > 0 {
		w += ` AND a.id IN (` + placeholders(len(q.IDs)) + `)`
		args = append(args, toAny(q.IDs)...)
	}
	var gesamt int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM musik_album a WHERE `+w, args...).Scan(&gesamt); err != nil {
		return nil, 0, err
	}
	if q.Limit <= 0 || q.Limit > 1000 {
		q.Limit = 200
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+musikAlbumCols+` FROM musik_album a WHERE `+w+
		` ORDER BY `+musikAlbenOrdnung(q.Sortierung)+` LIMIT ? OFFSET ?`, append(args, q.Limit, max(0, q.Offset))...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	alben, err := scanMusikAlben(rows)
	return alben, gesamt, err
}

// MusikAlbum liefert ein Album samt seinen sichtbaren Titeln in der
// Reihenfolge Disc → Track → Name.
func (s *Store) MusikAlbum(ctx context.Context, v MusikSicht, id string) (*MusikAlbum, []MusikTitel, error) {
	w, args := v.albumBedingung("a")
	rows, err := s.db.QueryContext(ctx, `SELECT `+musikAlbumCols+` FROM musik_album a WHERE a.id = ? AND `+w,
		append([]any{id}, args...)...)
	if err != nil {
		return nil, nil, err
	}
	alben, err := scanMusikAlben(rows)
	rows.Close()
	if err != nil {
		return nil, nil, err
	}
	if len(alben) == 0 {
		return nil, nil, ErrNotFound
	}
	tw, targs := v.titelBedingung("t")
	trows, err := s.db.QueryContext(ctx, `SELECT `+musikTitelCols+` FROM musik_titel t WHERE t.album_id = ? AND `+tw+
		` ORDER BY t.disc, t.track, t.titel_sort`, append([]any{id}, targs...)...)
	if err != nil {
		return nil, nil, err
	}
	defer trows.Close()
	titel, err := scanMusikTitel(trows)
	if err != nil {
		return nil, nil, err
	}
	a := alben[0]
	// Anzahl und Dauer so, wie diese Person das Album sieht.
	a.Anzahl, a.DauerMS = len(titel), 0
	for _, t := range titel {
		a.DauerMS += t.DauerMS
	}
	return &a, titel, nil
}

// MusikCoverQuelle: woher das Bild eines Albums kommt — nur, wenn die
// Person das Album sehen darf.
func (s *Store) MusikCoverQuelle(ctx context.Context, v MusikSicht, albumID string) (string, error) {
	w, args := v.albumBedingung("a")
	var q string
	err := s.db.QueryRowContext(ctx, `SELECT a.cover_quelle FROM musik_album a WHERE a.id = ? AND `+w,
		append([]any{albumID}, args...)...).Scan(&q)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return q, err
}

// MusikTitelEinzeln: ein Titel, wenn die Person ihn sehen darf.
func (s *Store) MusikTitelEinzeln(ctx context.Context, v MusikSicht, id string) (*MusikTitel, error) {
	tw, args := v.titelBedingung("t")
	rows, err := s.db.QueryContext(ctx, `SELECT `+musikTitelCols+` FROM musik_titel t WHERE t.id = ? AND `+tw,
		append([]any{id}, args...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ts, err := scanMusikTitel(rows)
	if err != nil {
		return nil, err
	}
	if len(ts) == 0 {
		return nil, ErrNotFound
	}
	return &ts[0], nil
}

// MusikTitelNachIDs: die sichtbaren Titel zu Kennungen, in der Reihenfolge
// der Kennungen. Was die Person nicht sehen darf, fällt still heraus.
func (s *Store) MusikTitelNachIDs(ctx context.Context, v MusikSicht, ids []string) ([]MusikTitel, error) {
	out := []MusikTitel{}
	if len(ids) == 0 {
		return out, nil
	}
	nach := map[string]MusikTitel{}
	for a := 0; a < len(ids); a += 400 {
		e := min(a+400, len(ids))
		tw, args := v.titelBedingung("t")
		rows, err := s.db.QueryContext(ctx, `SELECT `+musikTitelCols+` FROM musik_titel t WHERE t.id IN (`+
			placeholders(e-a)+`) AND `+tw, append(toAny(ids[a:e]), args...)...)
		if err != nil {
			return nil, err
		}
		ts, err := scanMusikTitel(rows)
		rows.Close()
		if err != nil {
			return nil, err
		}
		for _, t := range ts {
			nach[t.ID] = t
		}
	}
	for _, id := range ids {
		if t, ok := nach[id]; ok {
			out = append(out, t)
		}
	}
	return out, nil
}

// MusikTitelAbfrage: die Titelliste mit Sortierung.
type MusikTitelAbfrage struct {
	Sortierung    string // titel | interpret | album | jahr | dauer | neu
	Absteigend    bool
	Genre         string
	InterpretName string // alle Titel, an denen dieser Name als Interpret steht
	Offset        int
	Limit         int
}

func musikTitelOrdnung(s string, ab bool) string {
	r := ""
	if ab {
		r = " DESC"
	}
	switch s {
	case "interpret":
		return `lower(t.interpret)` + r + `, lower(t.album), t.disc, t.track`
	case "album":
		return `lower(t.album)` + r + `, t.disc, t.track`
	case "jahr":
		return `t.jahr` + r + `, lower(t.album), t.disc, t.track`
	case "dauer":
		return `t.dauer_ms` + r + `, t.titel_sort`
	case "neu":
		if ab {
			return `t.hinzugefuegt ASC, t.titel_sort`
		}
		return `t.hinzugefuegt DESC, t.titel_sort`
	default:
		return `t.titel_sort` + r + `, t.id`
	}
}

func (s *Store) MusikTitelListe(ctx context.Context, v MusikSicht, q MusikTitelAbfrage) ([]MusikTitel, int, error) {
	w, args := v.titelBedingung("t")
	if q.Genre != "" {
		w += ` AND t.genre = ? COLLATE NOCASE`
		args = append(args, q.Genre)
	}
	if q.InterpretName != "" {
		w += ` AND (t.interpret = ? COLLATE NOCASE OR t.albuminterpret = ? COLLATE NOCASE)`
		args = append(args, q.InterpretName, q.InterpretName)
	}
	var gesamt int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM musik_titel t WHERE `+w, args...).Scan(&gesamt); err != nil {
		return nil, 0, err
	}
	if q.Limit <= 0 || q.Limit > 2000 {
		q.Limit = 300
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+musikTitelCols+` FROM musik_titel t WHERE `+w+
		` ORDER BY `+musikTitelOrdnung(q.Sortierung, q.Absteigend)+` LIMIT ? OFFSET ?`,
		append(args, q.Limit, max(0, q.Offset))...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	ts, err := scanMusikTitel(rows)
	return ts, gesamt, err
}

// MusikInterpreten: alle Albuminterpreten, die die Person sehen darf.
// Kompilationen stehen unter keinem Interpreten.
func (s *Store) MusikInterpreten(ctx context.Context, v MusikSicht) ([]MusikInterpret, error) {
	w, args := v.albumBedingung("a")
	rows, err := s.db.QueryContext(ctx,
		`SELECT i.id, i.name, count(a.id),
		        (SELECT a2.id FROM musik_album a2 WHERE a2.interpret_id = i.id AND a2.cover_quelle != ''
		          ORDER BY a2.jahr DESC LIMIT 1)
		   FROM musik_interpret i JOIN musik_album a ON a.interpret_id = i.id
		  WHERE a.compilation = 0 AND `+w+`
		  GROUP BY i.id ORDER BY i.name_sort`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MusikInterpret{}
	for rows.Next() {
		var m MusikInterpret
		var cover sql.NullString
		if err := rows.Scan(&m.ID, &m.Name, &m.Alben, &cover); err != nil {
			return nil, err
		}
		m.CoverAlbum = cover.String
		out = append(out, m)
	}
	return out, rows.Err()
}

// MusikInterpretName: der Name zu einer Kennung. Auch ein Interpret, der
// nur auf Kompilationen vorkommt, hat eine Seite — darum wird notfalls in
// den Titeln gesucht.
func (s *Store) MusikInterpretName(ctx context.Context, v MusikSicht, id string) (string, error) {
	var name string
	err := s.db.QueryRowContext(ctx, `SELECT name FROM musik_interpret WHERE id = ?`, id).Scan(&name)
	if err == nil {
		return name, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	tw, args := v.titelBedingung("t")
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT t.interpret FROM musik_titel t WHERE t.interpret != '' AND `+tw, args...)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return "", err
		}
		if InterpretID(n) == id {
			return n, nil
		}
	}
	return "", ErrNotFound
}

// MusikErscheintAuf: Alben anderer Interpreten (meist Kompilationen), auf
// denen dieser Interpret mit mindestens einem Titel vertreten ist.
func (s *Store) MusikErscheintAuf(ctx context.Context, v MusikSicht, interpretID, name string) ([]MusikAlbum, error) {
	tw, args := v.titelBedingung("t")
	rows, err := s.db.QueryContext(ctx, `SELECT `+musikAlbumCols+` FROM musik_album a
		 WHERE a.interpret_id != ? AND a.id IN (SELECT t.album_id FROM musik_titel t
		       WHERE t.interpret = ? COLLATE NOCASE AND `+tw+`)
		 ORDER BY a.jahr DESC, a.name_sort`, append([]any{interpretID, name}, args...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMusikAlben(rows)
}

// MusikGenres mit der Zahl der Alben.
func (s *Store) MusikGenres(ctx context.Context, v MusikSicht) ([]MusikGenre, error) {
	w, args := v.albumBedingung("a")
	rows, err := s.db.QueryContext(ctx,
		`SELECT a.genre, count(*), (SELECT a2.id FROM musik_album a2 WHERE a2.genre = a.genre AND a2.cover_quelle != ''
		          ORDER BY a2.hinzugefuegt DESC LIMIT 1)
		   FROM musik_album a WHERE a.genre != '' AND `+w+`
		  GROUP BY a.genre COLLATE NOCASE ORDER BY lower(a.genre)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MusikGenre{}
	for rows.Next() {
		var g MusikGenre
		var cover sql.NullString
		if err := rows.Scan(&g.Name, &g.Alben, &cover); err != nil {
			return nil, err
		}
		g.CoverAlbum = cover.String
		out = append(out, g)
	}
	return out, rows.Err()
}

// MusikZaehlen: wie viel Musik die Person sehen kann — für die Startseite.
func (s *Store) MusikZaehlen(ctx context.Context, v MusikSicht) (titel, alben int, err error) {
	tw, targs := v.titelBedingung("t")
	if err = s.db.QueryRowContext(ctx, `SELECT count(*), count(DISTINCT t.album_id) FROM musik_titel t WHERE `+tw,
		targs...).Scan(&titel, &alben); err != nil {
		return 0, 0, err
	}
	return titel, alben, nil
}

// MusikSuche findet Interpreten, Alben und Titel zu einem Suchtext.
func (s *Store) MusikSuche(ctx context.Context, v MusikSicht, text string, limit int) ([]MusikInterpret, []MusikAlbum, []MusikTitel, error) {
	muster := "%" + strings.NewReplacer("%", "", "_", "").Replace(strings.ToLower(strings.TrimSpace(text))) + "%"
	if muster == "%%" {
		return []MusikInterpret{}, []MusikAlbum{}, []MusikTitel{}, nil
	}
	alleInterpreten, err := s.MusikInterpreten(ctx, v)
	if err != nil {
		return nil, nil, nil, err
	}
	nadel := strings.Trim(muster, "%")
	interpreten := []MusikInterpret{}
	for _, i := range alleInterpreten {
		if strings.Contains(strings.ToLower(i.Name), nadel) && len(interpreten) < limit {
			interpreten = append(interpreten, i)
		}
	}
	w, args := v.albumBedingung("a")
	rows, err := s.db.QueryContext(ctx, `SELECT `+musikAlbumCols+` FROM musik_album a
		 WHERE (lower(a.name) LIKE ? OR lower(a.interpret) LIKE ?) AND `+w+` ORDER BY a.name_sort LIMIT ?`,
		append(append([]any{muster, muster}, args...), limit)...)
	if err != nil {
		return nil, nil, nil, err
	}
	alben, err := scanMusikAlben(rows)
	rows.Close()
	if err != nil {
		return nil, nil, nil, err
	}
	tw, targs := v.titelBedingung("t")
	trows, err := s.db.QueryContext(ctx, `SELECT `+musikTitelCols+` FROM musik_titel t
		 WHERE (lower(t.titel) LIKE ? OR lower(t.interpret) LIKE ? OR lower(t.komponist) LIKE ?) AND `+tw+`
		 ORDER BY CASE WHEN lower(t.titel) LIKE ? THEN 0 ELSE 1 END, t.titel_sort LIMIT ?`,
		append(append([]any{muster, muster, muster}, targs...), muster, limit)...)
	if err != nil {
		return nil, nil, nil, err
	}
	defer trows.Close()
	titel, err := scanMusikTitel(trows)
	return interpreten, alben, titel, err
}

// BibliothekenNachArt: die Kennungen aller Bibliotheken einer Art — ohne
// das Zählen, das Libraries() für die Verwaltung macht.
func (s *Store) BibliothekenNachArt(ctx context.Context, kind string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM library WHERE kind = ? ORDER BY name`, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectStrings(rows)
}

// MusikAlbenMitCover: alle Alben einer Bibliothek, die ein Cover haben —
// für das Vorrechnen der Bilder im Hintergrund.
func (s *Store) MusikAlbenMitCover(ctx context.Context, libraryID string) ([]MusikAlbum, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+musikAlbumCols+` FROM musik_album a
		 WHERE a.library_id = ? AND a.cover_quelle != ''`, libraryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMusikAlben(rows)
}
