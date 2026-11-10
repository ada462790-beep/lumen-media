// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

// Automatisch erkannter Vorspann und Abspann je Datei (internal/vorspann,
// api/vorspann_erkennung.go).
//
// Je Datei eine Zeile, mit Grösse und Änderungszeit der Datei, wie sie
// untersucht wurde, und der Fassung des Verfahrens: Ändert sich eines
// davon, gilt die Zeile nicht mehr und die Datei wird neu untersucht.
// Sonst wird jede Datei genau einmal gelesen. Die Zeile verschwindet mit
// der Datei (ON DELETE CASCADE).
//
// Gespeichert wird auch, was unter der Mindestsicherheit blieb — benutzt
// wird es nicht (api: vorspannUndAbspann). So lässt sich die Schwelle
// ändern, ohne alles neu zu lesen.
//
// Keine Zeitstempel: Wann eine Datei untersucht wurde, hängt nur an der
// festen Reihenfolge des Durchgangs, nicht daran, was jemand schaut —
// trotzdem steht es nirgends.

import (
	"context"
	"database/sql"
	"errors"
)

// vorspannErkennungTabellen legt die Tabelle an (Schema-Stand 12). Eigene
// Funktion, damit sie beim Zusammenführen mit anderen Änderungen am
// Schema als Ganzes umgezählt werden kann.
func vorspannErkennungTabellen(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS vorspann_erkannt (
	   file_id        TEXT PRIMARY KEY REFERENCES media_file(id) ON DELETE CASCADE,
	   groesse        INTEGER NOT NULL,
	   mtime          INTEGER NOT NULL,
	   fassung        INTEGER NOT NULL,
	   stand          TEXT NOT NULL,              -- fertig | einsam | ohne_ton | fehler
	   partner        INTEGER NOT NULL DEFAULT 0, -- mit wie vielen Folgen verglichen
	   folgen         INTEGER NOT NULL DEFAULT 0, -- so viele Folgen hatte die Serie da
	   intro_von_ms   INTEGER,
	   intro_bis_ms   INTEGER,
	   intro_sicher   REAL,
	   abspann_ab_ms  INTEGER,
	   abspann_sicher REAL)`)
	return err
}

// Stände einer Untersuchung.
const (
	ErkanntFertig  = "fertig"
	ErkanntEinsam  = "einsam"   // keine andere Folge zum Vergleichen — wieder, wenn eine dazukommt
	ErkanntOhneTon = "ohne_ton" // keine Tonspur
	ErkanntFehler  = "fehler"   // liess sich nicht lesen — erst wieder, wenn die Datei sich ändert
)

// Erkennung ist das Ergebnis für eine Datei.
type Erkennung struct {
	FileID        string
	Groesse       int64
	MTime         int64
	Fassung       int
	Stand         string
	Partner       int
	Folgen        int
	VorspannVonMS int64
	VorspannBisMS int64
	VorspannSich  float64 // 0 = keiner gefunden
	AbspannAbMS   int64
	AbspannSich   float64
}

// ErkennungSetzen speichert das Ergebnis einer Untersuchung.
func (s *Store) ErkennungSetzen(ctx context.Context, e Erkennung) error {
	nullWenn := func(sicher float64, v int64) any {
		if sicher <= 0 {
			return nil
		}
		return v
	}
	nullSicher := func(sicher float64) any {
		if sicher <= 0 {
			return nil
		}
		return sicher
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO vorspann_erkannt (file_id, groesse, mtime, fassung, stand, partner, folgen,
		    intro_von_ms, intro_bis_ms, intro_sicher, abspann_ab_ms, abspann_sicher)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?)
		 ON CONFLICT (file_id) DO UPDATE SET groesse = excluded.groesse, mtime = excluded.mtime,
		    fassung = excluded.fassung, stand = excluded.stand, partner = excluded.partner,
		    folgen = excluded.folgen, intro_von_ms = excluded.intro_von_ms,
		    intro_bis_ms = excluded.intro_bis_ms, intro_sicher = excluded.intro_sicher,
		    abspann_ab_ms = excluded.abspann_ab_ms, abspann_sicher = excluded.abspann_sicher`,
		e.FileID, e.Groesse, e.MTime, e.Fassung, e.Stand, e.Partner, e.Folgen,
		nullWenn(e.VorspannSich, e.VorspannVonMS), nullWenn(e.VorspannSich, e.VorspannBisMS), nullSicher(e.VorspannSich),
		nullWenn(e.AbspannSich, e.AbspannAbMS), nullSicher(e.AbspannSich))
	return err
}

// Erkannt liefert das Ergebnis für eine Datei — nur, wenn es zur Datei,
// wie sie jetzt ist, und zur Fassung des Verfahrens passt. Sonst nil.
func (s *Store) Erkannt(ctx context.Context, fileID string, groesse, mtime int64, fassung int) (*Erkennung, error) {
	var e Erkennung
	var von, bis, ab sql.NullInt64
	var vs, as sql.NullFloat64
	err := s.db.QueryRowContext(ctx,
		`SELECT file_id, groesse, mtime, fassung, stand, partner, folgen,
		        intro_von_ms, intro_bis_ms, intro_sicher, abspann_ab_ms, abspann_sicher
		   FROM vorspann_erkannt WHERE file_id = ?`, fileID).
		Scan(&e.FileID, &e.Groesse, &e.MTime, &e.Fassung, &e.Stand, &e.Partner, &e.Folgen,
			&von, &bis, &vs, &ab, &as)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if e.Groesse != groesse || e.MTime != mtime || e.Fassung != fassung {
		return nil, nil
	}
	e.VorspannVonMS, e.VorspannBisMS, e.VorspannSich = von.Int64, bis.Int64, vs.Float64
	e.AbspannAbMS, e.AbspannSich = ab.Int64, as.Float64
	return &e, nil
}

// erkennungOffen ist die Bedingung „diese Datei muss (wieder) untersucht
// werden" — f: media_file, v: vorspann_erkannt, st: die Staffel.
const erkennungOffen = `(v.file_id IS NULL OR v.groesse <> f.size_bytes OR v.mtime <> f.mtime OR v.fassung <> ?
	OR (v.stand = 'einsam' AND v.folgen <> (SELECT COUNT(*) FROM item e2 JOIN item s2 ON s2.id = e2.parent_id
	                                        WHERE s2.parent_id = st.parent_id AND e2.kind = 'episode')))`

// erkennungDateien: Folgen-Dateien, die für die Erkennung in Frage kommen —
// vorhanden, erster Teil, mindestens fünf Minuten lang.
const erkennungDateien = `FROM media_file f
	JOIN item e ON e.id = f.item_id AND e.kind = 'episode'
	JOIN item st ON st.id = e.parent_id
	LEFT JOIN vorspann_erkannt v ON v.file_id = f.id
	WHERE f.missing_since IS NULL AND COALESCE(f.duration_ms, 0) >= 300000 AND f.part_no = 1`

// ErkennungOffeneSerien nennt bis zu limit Serien, in denen mindestens
// eine Datei noch zu untersuchen ist — die mit den neuesten Dateien zuerst.
func (s *Store) ErkennungOffeneSerien(ctx context.Context, fassung, limit int) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT st.parent_id `+erkennungDateien+` AND st.parent_id IS NOT NULL AND `+erkennungOffen+`
		  GROUP BY st.parent_id ORDER BY MAX(f.rowid) DESC LIMIT ?`, fassung, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// ErkennungsDatei ist eine Folgen-Datei einer Serie, wie sie die
// Erkennung braucht.
type ErkennungsDatei struct {
	FileID     string
	ItemID     string // die Folge
	StaffelID  string
	Path       string
	DurationMS int64
	Groesse    int64
	MTime      int64
	Offen      bool // muss untersucht werden
}

// ErkennungSerie liefert alle in Frage kommenden Dateien einer Serie in
// der Reihenfolge der Folgen; je Folge die bevorzugte Fassung zuerst.
func (s *Store) ErkennungSerie(ctx context.Context, serieID string, fassung int) ([]ErkennungsDatei, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT f.id, e.id, st.id, f.path, f.duration_ms, f.size_bytes, f.mtime, `+erkennungOffen+`
		 `+erkennungDateien+` AND st.parent_id = ?
		  ORDER BY COALESCE(st.season_no, 0), COALESCE(e.episode_no, 0), e.id,
		           f.id = COALESCE(e.bevorzugte_datei, '') DESC, COALESCE(f.height, 0) DESC, f.id`,
		fassung, serieID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ErkennungsDatei
	for rows.Next() {
		var d ErkennungsDatei
		if err := rows.Scan(&d.FileID, &d.ItemID, &d.StaffelID, &d.Path, &d.DurationMS, &d.Groesse, &d.MTime, &d.Offen); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// ErkennungStand zählt für die Verwaltung: untersuchte Dateien, davon mit
// Vorspann und mit Abspann (ab der Mindestsicherheit), und was noch offen ist.
type ErkennungStand struct {
	Untersucht  int `json:"untersucht"`
	MitVorspann int `json:"mitVorspann"`
	MitAbspann  int `json:"mitAbspann"`
	Offen       int `json:"offen"`
}

func (s *Store) ErkennungZaehlen(ctx context.Context, fassung int, mindest float64) (ErkennungStand, error) {
	var z ErkennungStand
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*), COALESCE(SUM(intro_sicher >= ?), 0), COALESCE(SUM(abspann_sicher >= ?), 0)
		   FROM vorspann_erkannt WHERE fassung = ?`, mindest, mindest, fassung).
		Scan(&z.Untersucht, &z.MitVorspann, &z.MitAbspann)
	if err != nil {
		return z, err
	}
	err = s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) `+erkennungDateien+` AND `+erkennungOffen, fassung).Scan(&z.Offen)
	return z, err
}

// VorspannDerFolge liefert den von Hand markierten Vorspann einer
// einzelnen Folge — oder nil. Er steht in derselben Tabelle wie der einer
// Serie, mit der Kennung der Folge.
func (s *Store) VorspannDerFolge(ctx context.Context, folgeID string) (*Vorspann, error) {
	v, err := s.VorspannDerSerie(ctx, folgeID)
	if v != nil {
		v.Quelle = "folge"
	}
	return v, err
}

// FolgenDerSerie zählt die Folgen einer Serie, genauso wie erkennungOffen
// es für „einsam" tut.
func (s *Store) FolgenDerSerie(ctx context.Context, serieID string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM item e2 JOIN item s2 ON s2.id = e2.parent_id
		  WHERE s2.parent_id = ? AND e2.kind = 'episode'`, serieID).Scan(&n)
	return n, err
}
