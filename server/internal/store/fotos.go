// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

// Fotos und kurze Videos aus einer Fotobibliothek. Ein Ordner ist ein
// Album; die Mediathek ordnet nach dem Aufnahmedatum.
//
// Das Aufnahmedatum steht so in der Datenbank, wie die Kamera es
// aufgeschrieben hat — „2024:07:13 18:02:11", ohne Zeitzone — und wird
// als UTC gespeichert. So zeigt jedes Gerät dieselbe Uhrzeit, die auf
// der Kamera stand, statt sie in die eigene Zone umzurechnen.

import (
	"context"
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"
)

type Foto struct {
	ID           string    `json:"id"`
	LibraryID    string    `json:"libraryId"`
	AlbumID      string    `json:"albumId,omitempty"`
	Pfad         string    `json:"-"`
	Groesse      int64     `json:"groesse"`
	MTime        int64     `json:"-"`
	Art          string    `json:"art"` // bild | video
	Format       string    `json:"format"`
	Aufnahme     time.Time `json:"aufnahme"`
	DatumQuelle  string    `json:"datumQuelle"` // exif | video | datei
	Breite       int       `json:"breite,omitempty"`
	Hoehe        int       `json:"hoehe,omitempty"`
	Ausrichtung  int       `json:"-"`
	Kamera       string    `json:"kamera,omitempty"`
	Breitengrad  *float64  `json:"breitengrad,omitempty"`
	Laengengrad  *float64  `json:"laengengrad,omitempty"`
	DauerMS      int64     `json:"dauerMs,omitempty"`
	Videocodec   string    `json:"videocodec,omitempty"`
	Vorschau     int       `json:"-"`
	Hinzugefuegt time.Time `json:"-"`
}

type FotoAlbum struct {
	ID        string `json:"id"`
	LibraryID string `json:"libraryId"`
	Name      string `json:"name"`
	Pfad      string `json:"pfad"`
	Anzahl    int    `json:"anzahl"`
	Titelbild string `json:"titelbild,omitempty"`
	Von       int64  `json:"von,omitempty"`
	Bis       int64  `json:"bis,omitempty"`
}

// FotoMonat: ein Monat in der Mediathek, mit bis zu vier Titelbildern.
type FotoMonat struct {
	Jahr        int      `json:"jahr"`
	Monat       int      `json:"monat"`
	Anzahl      int      `json:"anzahl"`
	Titelbilder []string `json:"titelbilder"`
}

type FotoSicht struct {
	Bibliotheken []string
}

func (v FotoSicht) bedingung(alias string) (string, []any) {
	if len(v.Bibliotheken) == 0 {
		return "0", nil
	}
	return alias + ".library_id IN (" + placeholders(len(v.Bibliotheken)) + ") AND " + alias + ".fehlt_seit IS NULL",
		toAny(v.Bibliotheken)
}

// FotoAlbumID: aus Bibliothek und Ordner — gleich über Durchläufe hinweg.
func FotoAlbumID(libraryID, relPfad string) string {
	h := sha1.Sum([]byte("fotoalbum\x00" + libraryID + "\x00" + relPfad))
	return "fa" + hex.EncodeToString(h[:8])
}

// Vorschau-Bits: welche Grössen schon im Zwischenspeicher liegen.
const (
	VorschauKlein = 1
	VorschauGross = 2
	// VorschauGescheitert: FFmpeg konnte die Datei nicht lesen (etwa HEIC
	// ohne passende Unterstützung). Nicht bei jedem Durchgang neu probieren.
	VorschauGescheitert = 4
)

// ---------------------------------------------------------------- Scanner

type FotoStand struct {
	ID      string
	Groesse int64
	MTime   int64
	Fehlt   bool
}

func (s *Store) FotoDateien(ctx context.Context, libraryID string) (map[string]FotoStand, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT pfad, id, groesse, mtime, fehlt_seit IS NOT NULL FROM foto WHERE library_id = ?`, libraryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]FotoStand{}
	for rows.Next() {
		var p string
		var st FotoStand
		if err := rows.Scan(&p, &st.ID, &st.Groesse, &st.MTime, &st.Fehlt); err != nil {
			return nil, err
		}
		out[p] = st
	}
	return out, rows.Err()
}

// FotoAlbumSichern legt das Album zu einem Ordner an, falls es fehlt.
func (s *Store) FotoAlbumSichern(ctx context.Context, libraryID, relPfad, name string) (string, error) {
	id := FotoAlbumID(libraryID, relPfad)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO foto_album (id, library_id, pfad, name) VALUES (?,?,?,?)
		 ON CONFLICT (id) DO UPDATE SET name = excluded.name`, id, libraryID, relPfad, name)
	return id, err
}

// FotoSpeichern legt ein Foto an oder bringt es auf Stand. Ändert sich die
// Datei, müssen auch die Vorschaubilder neu.
func (s *Store) FotoSpeichern(ctx context.Context, f *Foto) (neu bool, err error) {
	var id string
	err = s.db.QueryRowContext(ctx, `SELECT id FROM foto WHERE pfad = ?`, f.Pfad).Scan(&id)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		neu = true
		f.ID = newID("fo")
		f.Hinzugefuegt = time.Now()
	case err != nil:
		return false, err
	default:
		f.ID = id
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO foto (id, library_id, album_id, pfad, groesse, mtime, art, format, aufnahme, datum_quelle,
		   breite, hoehe, ausrichtung, kamera, breitengrad, laengengrad, dauer_ms, videocodec, vorschau,
		   hinzugefuegt, fehlt_seit)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,0,?,NULL)
		 ON CONFLICT (pfad) DO UPDATE SET library_id=excluded.library_id, album_id=excluded.album_id,
		   groesse=excluded.groesse, mtime=excluded.mtime, art=excluded.art, format=excluded.format,
		   aufnahme=excluded.aufnahme, datum_quelle=excluded.datum_quelle, breite=excluded.breite,
		   hoehe=excluded.hoehe, ausrichtung=excluded.ausrichtung, kamera=excluded.kamera,
		   breitengrad=excluded.breitengrad, laengengrad=excluded.laengengrad, dauer_ms=excluded.dauer_ms,
		   videocodec=excluded.videocodec, vorschau=0, fehlt_seit=NULL`,
		f.ID, f.LibraryID, f.AlbumID, f.Pfad, f.Groesse, f.MTime, f.Art, f.Format, f.Aufnahme.Unix(),
		f.DatumQuelle, f.Breite, f.Hoehe, f.Ausrichtung, f.Kamera, f.Breitengrad, f.Laengengrad,
		f.DauerMS, f.Videocodec, f.Hinzugefuegt.Unix())
	return neu, err
}

func (s *Store) FotoWiederDa(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE foto SET fehlt_seit = NULL WHERE id = ? AND fehlt_seit IS NOT NULL`, id)
	return err
}

// FotoFehlende: wie bei der Musik — vermerken, nach 30 Tagen vergessen.
// Leere Alben verschwinden sofort.
func (s *Store) FotoFehlende(ctx context.Context, libraryID string, gesehen map[string]bool) (int, error) {
	bekannt, err := s.FotoDateien(ctx, libraryID)
	if err != nil {
		return 0, err
	}
	n := 0
	jetzt := time.Now().Unix()
	for p, st := range bekannt {
		if gesehen[p] || st.Fehlt {
			continue
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE foto SET fehlt_seit = ? WHERE id = ?`, jetzt, st.ID); err != nil {
			return n, err
		}
		n++
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM foto WHERE library_id = ? AND fehlt_seit IS NOT NULL AND fehlt_seit < ?`,
		libraryID, jetzt-30*86400); err != nil {
		return n, err
	}
	_, err = s.db.ExecContext(ctx, `DELETE FROM foto_album WHERE library_id = ?
		AND id NOT IN (SELECT album_id FROM foto WHERE library_id = ? AND fehlt_seit IS NULL)`, libraryID, libraryID)
	return n, err
}

// ---------------------------------------------------------------- Lesen

const fotoCols = `f.id, f.library_id, f.album_id, f.pfad, f.groesse, f.mtime, f.art, f.format, f.aufnahme,
	f.datum_quelle, f.breite, f.hoehe, f.ausrichtung, f.kamera, f.breitengrad, f.laengengrad, f.dauer_ms,
	f.videocodec, f.vorschau, f.hinzugefuegt`

func scanFotos(rows *sql.Rows) ([]Foto, error) {
	out := []Foto{}
	for rows.Next() {
		var f Foto
		var auf, hinzu int64
		var br, la sql.NullFloat64
		if err := rows.Scan(&f.ID, &f.LibraryID, &f.AlbumID, &f.Pfad, &f.Groesse, &f.MTime, &f.Art, &f.Format,
			&auf, &f.DatumQuelle, &f.Breite, &f.Hoehe, &f.Ausrichtung, &f.Kamera, &br, &la, &f.DauerMS,
			&f.Videocodec, &f.Vorschau, &hinzu); err != nil {
			return nil, err
		}
		f.Aufnahme = time.Unix(auf, 0).UTC()
		f.Hinzugefuegt = time.Unix(hinzu, 0)
		if br.Valid && la.Valid {
			b, l := br.Float64, la.Float64
			f.Breitengrad, f.Laengengrad = &b, &l
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// FotoUebersicht: alle Monate mit Fotos, neueste zuerst, je mit bis zu
// vier Titelbildern (Bilder vor Videos).
func (s *Store) FotoUebersicht(ctx context.Context, v FotoSicht) ([]FotoMonat, error) {
	w, args := v.bedingung("f")
	rows, err := s.db.QueryContext(ctx,
		`SELECT CAST(strftime('%Y', f.aufnahme, 'unixepoch') AS INTEGER),
		        CAST(strftime('%m', f.aufnahme, 'unixepoch') AS INTEGER), count(*)
		   FROM foto f WHERE `+w+` GROUP BY 1, 2 ORDER BY 1 DESC, 2 DESC`, args...)
	if err != nil {
		return nil, err
	}
	out := []FotoMonat{}
	index := map[[2]int]int{}
	for rows.Next() {
		var m FotoMonat
		if err := rows.Scan(&m.Jahr, &m.Monat, &m.Anzahl); err != nil {
			rows.Close()
			return nil, err
		}
		m.Titelbilder = []string{}
		index[[2]int{m.Jahr, m.Monat}] = len(out)
		out = append(out, m)
	}
	rows.Close()
	trows, err := s.db.QueryContext(ctx,
		`SELECT j, m, id FROM (
		   SELECT f.id,
		          CAST(strftime('%Y', f.aufnahme, 'unixepoch') AS INTEGER) AS j,
		          CAST(strftime('%m', f.aufnahme, 'unixepoch') AS INTEGER) AS m,
		          row_number() OVER (PARTITION BY strftime('%Y-%m', f.aufnahme, 'unixepoch')
		                             ORDER BY f.art = 'video', f.aufnahme DESC) AS rn
		     FROM foto f WHERE `+w+`) WHERE rn <= 4`, args...)
	if err != nil {
		return nil, err
	}
	defer trows.Close()
	for trows.Next() {
		var j, m int
		var id string
		if err := trows.Scan(&j, &m, &id); err != nil {
			return nil, err
		}
		if i, ok := index[[2]int{j, m}]; ok {
			out[i].Titelbilder = append(out[i].Titelbilder, id)
		}
	}
	return out, trows.Err()
}

type FotoAbfrage struct {
	Jahr    int
	Monat   int
	AlbumID string
	Offset  int
	Limit   int
}

// FotoListe: eine Seite Fotos, neueste zuerst — oder im Album in der
// Reihenfolge der Aufnahme, wie man eine Reise erzählt.
func (s *Store) FotoListe(ctx context.Context, v FotoSicht, q FotoAbfrage) ([]Foto, int, error) {
	w, args := v.bedingung("f")
	ordnung := `f.aufnahme DESC, f.id`
	if q.AlbumID != "" {
		w += ` AND f.album_id = ?`
		args = append(args, q.AlbumID)
		ordnung = `f.aufnahme ASC, f.id`
	}
	if q.Jahr > 0 {
		von := time.Date(q.Jahr, 1, 1, 0, 0, 0, 0, time.UTC)
		bis := von.AddDate(1, 0, 0)
		if q.Monat >= 1 && q.Monat <= 12 {
			von = time.Date(q.Jahr, time.Month(q.Monat), 1, 0, 0, 0, 0, time.UTC)
			bis = von.AddDate(0, 1, 0)
		}
		w += ` AND f.aufnahme >= ? AND f.aufnahme < ?`
		args = append(args, von.Unix(), bis.Unix())
	}
	var gesamt int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM foto f WHERE `+w, args...).Scan(&gesamt); err != nil {
		return nil, 0, err
	}
	if q.Limit <= 0 || q.Limit > 2000 {
		q.Limit = 300
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+fotoCols+` FROM foto f WHERE `+w+` ORDER BY `+ordnung+` LIMIT ? OFFSET ?`,
		append(args, q.Limit, max(0, q.Offset))...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	fs, err := scanFotos(rows)
	return fs, gesamt, err
}

func (s *Store) FotoEinzeln(ctx context.Context, v FotoSicht, id string) (*Foto, error) {
	w, args := v.bedingung("f")
	rows, err := s.db.QueryContext(ctx, `SELECT `+fotoCols+` FROM foto f WHERE f.id = ? AND `+w, append([]any{id}, args...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	fs, err := scanFotos(rows)
	if err != nil {
		return nil, err
	}
	if len(fs) == 0 {
		return nil, ErrNotFound
	}
	return &fs[0], nil
}

// FotoAlben: alle Ordner mit sichtbaren Fotos, nach Name.
func (s *Store) FotoAlben(ctx context.Context, v FotoSicht) ([]FotoAlbum, error) {
	w, args := v.bedingung("f")
	rows, err := s.db.QueryContext(ctx,
		`SELECT a.id, a.library_id, a.name, a.pfad, count(f.id), min(f.aufnahme), max(f.aufnahme),
		        (SELECT f2.id FROM foto f2 WHERE f2.album_id = a.id AND f2.fehlt_seit IS NULL
		          ORDER BY f2.art = 'video', f2.aufnahme DESC LIMIT 1)
		   FROM foto_album a JOIN foto f ON f.album_id = a.id
		  WHERE `+w+` GROUP BY a.id ORDER BY lower(a.pfad)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FotoAlbum{}
	for rows.Next() {
		var a FotoAlbum
		var titel sql.NullString
		if err := rows.Scan(&a.ID, &a.LibraryID, &a.Name, &a.Pfad, &a.Anzahl, &a.Von, &a.Bis, &titel); err != nil {
			return nil, err
		}
		a.Titelbild = titel.String
		out = append(out, a)
	}
	return out, rows.Err()
}

// FotoAlbumEinzeln: Name und Pfad eines Albums, wenn es sichtbare Fotos hat.
func (s *Store) FotoAlbumEinzeln(ctx context.Context, v FotoSicht, id string) (*FotoAlbum, error) {
	alle, err := s.FotoAlben(ctx, v)
	if err != nil {
		return nil, err
	}
	for _, a := range alle {
		if a.ID == id {
			return &a, nil
		}
	}
	return nil, ErrNotFound
}

// ---------------------------------------------------------------- Vorschau

// FotosOhneVorschau: Fotos, deren Vorschaubilder noch fehlen — für die
// Arbeit im Hintergrund, unabhängig davon, wer sie sehen darf.
func (s *Store) FotosOhneVorschau(ctx context.Context, limit int) ([]Foto, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+fotoCols+` FROM foto f
		 WHERE f.fehlt_seit IS NULL AND f.vorschau & ? = 0 AND f.vorschau & ? != ?
		 ORDER BY f.aufnahme DESC LIMIT ?`,
		VorschauGescheitert, VorschauKlein|VorschauGross, VorschauKlein|VorschauGross, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanFotos(rows)
}

// FotoVorschauVermerken setzt Bits (welche Grössen fertig sind).
func (s *Store) FotoVorschauVermerken(ctx context.Context, id string, bits int) error {
	_, err := s.db.ExecContext(ctx, `UPDATE foto SET vorschau = vorschau | ? WHERE id = ?`, bits, id)
	return err
}

// FotoIDs: alle Kennungen — zum Aufräumen verwaister Vorschaubilder.
func (s *Store) FotoIDs(ctx context.Context) (map[string]bool, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM foto`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids, err := collectStrings(rows)
	out := make(map[string]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out, err
}

// FotoIntern: ein Foto ohne Rücksicht auf Freigaben — nur für die Arbeit
// im Hintergrund.
func (s *Store) FotoIntern(ctx context.Context, id string) (*Foto, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+fotoCols+` FROM foto f WHERE f.id = ?`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	fs, err := scanFotos(rows)
	if err != nil {
		return nil, err
	}
	if len(fs) == 0 {
		return nil, ErrNotFound
	}
	return &fs[0], nil
}
