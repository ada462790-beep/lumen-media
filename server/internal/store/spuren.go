// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

// Spuren und Kapitel einer Datei: Tonspuren, Untertitel (eingebettet oder
// als .srt daneben) und Kapitel mit der Einordnung Vorspann/Abspann. Der
// Scanner misst sie beim Einlesen; was vorher eingelesen wurde, misst
// die API beim ersten Öffnen nach.

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sort"
	"strings"
)

type Track struct {
	Index           int    `json:"index"`
	Kind            string `json:"kind"` // audio | subtitle
	Codec           string `json:"codec,omitempty"`
	Language        string `json:"language,omitempty"`
	Title           string `json:"title,omitempty"`
	Channels        int    `json:"channels,omitempty"`
	Forced          bool   `json:"forced,omitempty"`
	HearingImpaired bool   `json:"hearingImpaired,omitempty"`
	// External: liegt als eigene Datei neben dem Film. Der Pfad bleibt im
	// Server; nach draussen geht nur, dass es so ist.
	External     bool   `json:"external,omitempty"`
	ExternalPath string `json:"-"`
	// Bitmap: Bild-Untertitel (PGS, DVD) — lassen sich nicht als Text
	// ausliefern, nur einbrennen. Die Oberfläche bietet sie nicht an.
	Bitmap bool `json:"bitmap,omitempty"`
}

type Chapter struct {
	Index   int    `json:"index"`
	StartMS int64  `json:"startMs"`
	EndMS   int64  `json:"endMs"`
	Title   string `json:"title,omitempty"`
	Kind    string `json:"kind"` // intro | credits | normal
}

// ExterneSpurAb ist der erste Index für Untertitel-Dateien neben dem
// Film, damit sie den eingebetteten Spuren nicht in die Quere kommen.
const ExterneSpurAb = 1000

// GeholtSpurAb ist der erste Index für Untertitel, die Lumen von
// OpenSubtitles geholt und im Datenverzeichnis abgelegt hat.
const GeholtSpurAb = 2000

// SetGeholteUntertitel nennt den Ordner der geholten Untertitel
// (<daten>/untertitel-geholt). Sie liegen nicht neben dem Film — das
// schriebe in die Medienordner und verriete über deren Änderungszeit,
// wann jemand Untertitel gesucht hat (untertitel.AblegenGeholt).
//
// Spuren nimmt sie beim Lesen dazu, statt sie in media_track zu
// schreiben: Ein Durchlauf schreibt die Spuren einer Datei neu und kennt
// nur, was neben dem Film liegt — die geholten gingen dabei verloren.
// So zählt einzig der Ordner, und alle Wege (Player, Spurliste,
// Download) sehen dieselben Spuren.
func (s *Store) SetGeholteUntertitel(ordner string) { s.geholtOrdner = ordner }

// GeholteUntertitel: die geholten Untertitel einer Datei, nach Namen
// sortiert, mit Indizes ab GeholtSpurAb.
func (s *Store) GeholteUntertitel(fileID string) []Track {
	if s.geholtOrdner == "" || !kennungSicher(fileID) {
		return nil
	}
	treffer, _ := filepath.Glob(filepath.Join(s.geholtOrdner, fileID+".*.srt"))
	sort.Strings(treffer)
	out := []Track{}
	for i, p := range treffer {
		// <datei>.<sprache>[.sdh|.forced][.n].srt
		teile := strings.Split(strings.TrimSuffix(strings.TrimPrefix(filepath.Base(p), fileID+"."), ".srt"), ".")
		if len(teile) == 0 || teile[0] == "" {
			continue
		}
		t := Track{Index: GeholtSpurAb + i, Kind: "subtitle", Codec: "subrip", Language: teile[0],
			Title: "OpenSubtitles", External: true, ExternalPath: p}
		for _, z := range teile[1:] {
			switch z {
			case "sdh":
				t.HearingImpaired = true
			case "forced":
				t.Forced = true
			}
		}
		out = append(out, t)
	}
	return out
}

func kennungSicher(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

var bitmapCodecs = map[string]bool{
	"hdmv_pgs_subtitle": true, "dvd_subtitle": true, "dvb_subtitle": true, "xsub": true,
}

// SpurenSetzen schreibt Spuren und Kapitel einer Datei neu und merkt sich,
// dass gemessen wurde — auch wenn nichts dabei herauskam.
func (s *Store) SpurenSetzen(ctx context.Context, fileID string, tracks []Track, chapters []Chapter) error {
	return s.tx(ctx, func(t *sql.Tx) error { return spurenSchreiben(ctx, t, fileID, tracks, chapters) })
}

func spurenSchreiben(ctx context.Context, t *sql.Tx, fileID string, tracks []Track, chapters []Chapter) error {
	if _, err := t.ExecContext(ctx, `DELETE FROM media_track WHERE file_id = ?`, fileID); err != nil {
		return err
	}
	if _, err := t.ExecContext(ctx, `DELETE FROM chapter WHERE file_id = ?`, fileID); err != nil {
		return err
	}
	extern := ExterneSpurAb
	for _, tr := range tracks {
		idx := tr.Index
		if tr.ExternalPath != "" {
			idx = extern
			extern++
		}
		var ext any
		if tr.ExternalPath != "" {
			ext = tr.ExternalPath
		}
		if _, err := t.ExecContext(ctx,
			`INSERT OR REPLACE INTO media_track (file_id, idx, kind, codec, language, title,
			   channels, forced, hearing_impaired, external_path) VALUES (?,?,?,?,?,?,?,?,?,?)`,
			fileID, idx, tr.Kind, tr.Codec, tr.Language, tr.Title, tr.Channels,
			b2i(tr.Forced), b2i(tr.HearingImpaired), ext); err != nil {
			return err
		}
	}
	for _, c := range chapters {
		if _, err := t.ExecContext(ctx,
			`INSERT OR REPLACE INTO chapter (file_id, idx, start_ms, end_ms, title, kind)
			 VALUES (?,?,?,?,?,?)`, fileID, c.Index, c.StartMS, c.EndMS, c.Title, c.Kind); err != nil {
			return err
		}
	}
	_, err := t.ExecContext(ctx, `UPDATE media_file SET spuren_gelesen = 1 WHERE id = ?`, fileID)
	return err
}

// SpurenGelesen sagt, ob diese Datei schon einmal ausgemessen wurde.
func (s *Store) SpurenGelesen(ctx context.Context, fileID string) bool {
	var n int
	_ = s.db.QueryRowContext(ctx, `SELECT spuren_gelesen FROM media_file WHERE id = ?`, fileID).Scan(&n)
	return n == 1
}

func (s *Store) Spuren(ctx context.Context, fileID string) ([]Track, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT idx, kind, COALESCE(codec,''), COALESCE(language,''), COALESCE(title,''),
		        COALESCE(channels,0), forced, hearing_impaired, COALESCE(external_path,'')
		   FROM media_track WHERE file_id = ? ORDER BY kind, idx`, fileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Track{}
	for rows.Next() {
		var t Track
		var forced, hi int
		if err := rows.Scan(&t.Index, &t.Kind, &t.Codec, &t.Language, &t.Title, &t.Channels,
			&forced, &hi, &t.ExternalPath); err != nil {
			return nil, err
		}
		t.Forced, t.HearingImpaired = forced == 1, hi == 1
		t.External = t.ExternalPath != ""
		t.Bitmap = bitmapCodecs[t.Codec]
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return append(out, s.GeholteUntertitel(fileID)...), nil
}

// Spur holt eine einzelne — für den Untertitel-Endpunkt.
func (s *Store) Spur(ctx context.Context, fileID string, idx int) (*Track, error) {
	alle, err := s.Spuren(ctx, fileID)
	if err != nil {
		return nil, err
	}
	for i := range alle {
		if alle[i].Index == idx {
			return &alle[i], nil
		}
	}
	return nil, ErrNotFound
}

func (s *Store) Kapitel(ctx context.Context, fileID string) ([]Chapter, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT idx, start_ms, end_ms, COALESCE(title,''), COALESCE(kind,'normal')
		   FROM chapter WHERE file_id = ? ORDER BY idx`, fileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Chapter{}
	for rows.Next() {
		var c Chapter
		if err := rows.Scan(&c.Index, &c.StartMS, &c.EndMS, &c.Title, &c.Kind); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------- Vorspann je Serie

// Vorspann ist die Spanne, die „Vorspann überspringen" überspringt. Hat
// die Datei ein Kapitel dafür, zählt das; sonst, was jemand mit Zugang
// zur Verwaltung für die ganze Serie markiert hat.
//
// Keiner: Von Hand „kein Vorspann" gesetzt (an der Serie oder an der
// Folge) — dann gibt es keinen Knopf, auch nicht aus Kapiteln oder der
// Erkennung. Gespeichert als Spanne 0–0.
type Vorspann struct {
	VonMS  int64  `json:"vonMs"`
	BisMS  int64  `json:"bisMs"`
	Quelle string `json:"quelle"` // folge | kapitel | serie | automatisch
	Keiner bool   `json:"keiner,omitempty"`
}

func (s *Store) VorspannSetzen(ctx context.Context, seriesID string, von, bis int64) error {
	if von < 0 || bis <= von || bis-von > 10*60*1000 {
		return errors.New("ein vorspann dauert zwischen einer sekunde und zehn minuten")
	}
	return s.vorspannSchreiben(ctx, seriesID, von, bis)
}

// VorspannKeiner merkt sich „kein Vorspann" für eine Serie oder Folge.
func (s *Store) VorspannKeiner(ctx context.Context, itemID string) error {
	return s.vorspannSchreiben(ctx, itemID, 0, 0)
}

func (s *Store) vorspannSchreiben(ctx context.Context, itemID string, von, bis int64) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO vorspann (item_id, von_ms, bis_ms) VALUES (?,?,?)
		 ON CONFLICT (item_id) DO UPDATE SET von_ms = excluded.von_ms, bis_ms = excluded.bis_ms`,
		itemID, von, bis)
	return err
}

func (s *Store) VorspannLoeschen(ctx context.Context, seriesID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM vorspann WHERE item_id = ?`, seriesID)
	return err
}

// VorspannDerSerie liefert die markierte Spanne — oder nil. „Kein
// Vorspann" kommt mit Keiner zurück.
func (s *Store) VorspannDerSerie(ctx context.Context, seriesID string) (*Vorspann, error) {
	var v Vorspann
	err := s.db.QueryRowContext(ctx,
		`SELECT von_ms, bis_ms FROM vorspann WHERE item_id = ?`, seriesID).Scan(&v.VonMS, &v.BisMS)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	v.Quelle = "serie"
	v.Keiner = v.VonMS == 0 && v.BisMS == 0
	return &v, err
}

// ---------------------------------------------------------------- Abspann von Hand

// abspannTabelle: von Hand gesetzter Beginn des Abspanns je Serie oder
// Folge; ab_ms = 0 heisst „kein Abspann" (nichts erkennen, „Als Nächstes"
// erst kurz vor Schluss). Schema-Stand 13.
const abspannTabelle = `CREATE TABLE IF NOT EXISTS abspann (
	   item_id TEXT PRIMARY KEY,
	   ab_ms   INTEGER NOT NULL)`

// AbspannSetzen: abMS > 0 setzt den Beginn von Hand (nur an einer Folge
// sinnvoll), 0 heisst „kein Abspann".
func (s *Store) AbspannSetzen(ctx context.Context, itemID string, abMS int64) error {
	if abMS < 0 || abMS > 24*3600*1000 {
		return errors.New("der abspann beginnt irgendwo in der folge")
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO abspann (item_id, ab_ms) VALUES (?,?)
		 ON CONFLICT (item_id) DO UPDATE SET ab_ms = excluded.ab_ms`, itemID, abMS)
	return err
}

func (s *Store) AbspannLoeschen(ctx context.Context, itemID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM abspann WHERE item_id = ?`, itemID)
	return err
}

// AbspannVonHand: gesetzt? und wenn ja, ab wann (0 = kein Abspann).
func (s *Store) AbspannVonHand(ctx context.Context, itemID string) (abMS int64, gesetzt bool, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT ab_ms FROM abspann WHERE item_id = ?`, itemID).Scan(&abMS)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	return abMS, err == nil, err
}

// Gemessen ist das, was beim letzten Ausmessen einer Datei herauskam.
// Der Scan braucht es, um eine unveränderte Datei nicht noch einmal zu
// lesen (die Platten des NAS dürfen dann schlafen) und um bei einer
// gescheiterten Messung die alten Werte zu behalten.
type Gemessen struct {
	Datei   MediaFile
	Gelesen bool     // Spuren und Kapitel wurden schon einmal gemessen
	Daneben []string // Pfade der Untertitel-Dateien neben dem Film
}

// GemessenZuPfad liefert die letzte Messung einer Datei, oder nil, wenn
// Lumen die Datei noch nicht kennt.
func (s *Store) GemessenZuPfad(ctx context.Context, path string) *Gemessen {
	var g Gemessen
	var gelesen int
	f := &g.Datei
	err := s.db.QueryRowContext(ctx,
		`SELECT id, item_id, path, size_bytes, mtime, part_no, COALESCE(edition,''),
		        COALESCE(container,''), COALESCE(duration_ms,0), COALESCE(bitrate,0),
		        COALESCE(width,0), COALESCE(height,0), COALESCE(video_codec,''),
		        COALESCE(hdr,''), spuren_gelesen, missing_since IS NOT NULL, COALESCE(defekt,'')
		   FROM media_file WHERE path = ?`, path).
		Scan(&f.ID, &f.ItemID, &f.Path, &f.SizeBytes, &f.MTime, &f.PartNo, &f.Edition,
			&f.Container, &f.DurationMS, &f.Bitrate, &f.Width, &f.Height, &f.VideoCodec,
			&f.HDR, &gelesen, &f.Missing, &f.Defekt)
	if err != nil {
		return nil
	}
	g.Gelesen = gelesen == 1
	rows, err := s.db.QueryContext(ctx,
		`SELECT external_path FROM media_track
		  WHERE file_id = ? AND external_path IS NOT NULL ORDER BY external_path`, f.ID)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var p string
			if rows.Scan(&p) == nil {
				g.Daneben = append(g.Daneben, p)
			}
		}
	}
	return &g
}
