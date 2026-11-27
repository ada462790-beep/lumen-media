// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

// Dateien in älteren Behältern (AVI, WMV, MPG …) und das Umhängen einer
// Datei nach dem Umpacken in MKV (internal/api, mkv_umpacken.go).
//
// Die Datei behält ihre Kennung: Stand, Gesehen, Merkliste und
// Bewertungen hängen am Titel, die gewählte Fassung (bevorzugte_datei),
// die Vorspann-Erkennung, Vorschaubilder und Standbild an der Kennung der
// Datei. Neu gemessen werden nur die Spuren — ihre Nummern können sich
// ändern, wenn Daten- oder Zeitcode-Spuren wegfallen.

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// EndungDatei: eine vorhandene Datei mit einer der gesuchten Endungen,
// mit dem Titel, zu dem sie gehört.
type EndungDatei struct {
	FileID    string `json:"fileId"`
	ItemID    string `json:"itemId"`
	LibraryID string `json:"-"`
	Pfad      string `json:"pfad"`
	Groesse   int64  `json:"sizeBytes"`
	Kind      string `json:"kind"`
	Titel     string `json:"titel"`
	Jahr      int    `json:"jahr,omitempty"`
	Serie     string `json:"serie,omitempty"`
	Staffel   int    `json:"staffel,omitempty"`
	Folge     int    `json:"folge,omitempty"`
	PartNo    int    `json:"partNo,omitempty"`
}

// DateienMitEndung: alle vorhandenen, lesbaren Dateien, deren Pfad auf
// eine der Endungen (klein geschrieben, mit Punkt) endet. itemID schränkt
// auf einen Film, eine Folge oder alle Folgen einer Serie ein ("" = alle).
func (s *Store) DateienMitEndung(ctx context.Context, endungen []string, itemID string) ([]EndungDatei, error) {
	if len(endungen) == 0 {
		return nil, nil
	}
	var bed []string
	var args []any
	for _, e := range endungen {
		e = strings.ToLower(e)
		// substr mit negativem Anfang: die letzten n Zeichen. Pfade sind
		// UTF-8, die Endungen ASCII — gezählt wird in Zeichen, das passt.
		bed = append(bed, `lower(substr(f.path, ?)) = ?`)
		args = append(args, -len(e), e)
	}
	q := `SELECT f.id, f.item_id, i.library_id, f.path, f.size_bytes, i.kind, i.title,
	             COALESCE(i.year,0), COALESCE(i.season_no,0), COALESCE(i.episode_no,0), f.part_no,
	             COALESCE((SELECT sr.title FROM item st JOIN item sr ON sr.id = st.parent_id
	                        WHERE i.kind = 'episode' AND st.id = i.parent_id), '')
	        FROM media_file f JOIN item i ON i.id = f.item_id
	       WHERE f.missing_since IS NULL AND COALESCE(f.defekt,'') = ''
	         AND (` + strings.Join(bed, " OR ") + `)`
	if itemID != "" {
		q += ` AND (i.id = ? OR i.parent_id = ?
		           OR i.parent_id IN (SELECT st.id FROM item st WHERE st.parent_id = ?))`
		args = append(args, itemID, itemID, itemID)
	}
	q += ` ORDER BY f.path`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EndungDatei
	for rows.Next() {
		var d EndungDatei
		if err := rows.Scan(&d.FileID, &d.ItemID, &d.LibraryID, &d.Pfad, &d.Groesse, &d.Kind, &d.Titel,
			&d.Jahr, &d.Staffel, &d.Folge, &d.PartNo, &d.Serie); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// ErrDateiVeraendert: Die Datei steht nicht mehr so in der Datenbank wie
// beim Start des Umpackens (umbenannt, ausgetragen, schon umgehängt).
var ErrDateiVeraendert = errors.New("die datei hat sich in der zwischenzeit verändert")

// DateiUmhaengen trägt für die Datei fileID den neuen Pfad ein — nur wenn
// sie noch unter altPfad steht — samt Grösse, Änderungszeit, Behälter und
// den neu gemessenen Angaben aus neu (Laufzeit, Bild, Spuren, Kapitel).
// Kennung, Titel, Teilnummer und Fassung bleiben. Die Vorspann-Erkennung
// gilt weiter (Bild und Ton sind dieselben): Ihre Grösse und Zeit werden
// mitgezogen, damit sie nicht von vorn beginnt.
func (s *Store) DateiUmhaengen(ctx context.Context, fileID, altPfad string, neu *MediaFile) error {
	return s.tx(ctx, func(t *sql.Tx) error {
		var altGroesse, altZeit int64
		err := t.QueryRowContext(ctx, `SELECT size_bytes, mtime FROM media_file WHERE id = ? AND path = ?`,
			fileID, altPfad).Scan(&altGroesse, &altZeit)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrDateiVeraendert
		}
		if err != nil {
			return err
		}
		var schonDa int
		if err := t.QueryRowContext(ctx, `SELECT count(*) FROM media_file WHERE path = ? AND id <> ?`,
			neu.Path, fileID).Scan(&schonDa); err != nil {
			return err
		}
		if schonDa > 0 {
			return ErrDateiVeraendert
		}
		if _, err := t.ExecContext(ctx,
			`UPDATE media_file SET path = ?, size_bytes = ?, mtime = ?, container = ?,
			    duration_ms = COALESCE(?, duration_ms), bitrate = COALESCE(?, bitrate),
			    width = COALESCE(?, width), height = COALESCE(?, height),
			    video_codec = COALESCE(?, video_codec), hdr = COALESCE(?, hdr),
			    missing_since = NULL, defekt = NULL, probed_at = ?
			  WHERE id = ?`,
			neu.Path, neu.SizeBytes, neu.MTime, nullStr(neu.Container),
			nullInt64(neu.DurationMS), nullInt(neu.Bitrate), nullInt(neu.Width), nullInt(neu.Height),
			nullStr(neu.VideoCodec), nullStr(neu.HDR), time.Now().Unix(), fileID); err != nil {
			return err
		}
		if _, err := t.ExecContext(ctx,
			`UPDATE vorspann_erkannt SET groesse = ?, mtime = ? WHERE file_id = ? AND groesse = ? AND mtime = ?`,
			neu.SizeBytes, neu.MTime, fileID, altGroesse, altZeit); err != nil {
			return err
		}
		if neu.Spuren != nil || neu.Kapitel != nil {
			return spurenSchreiben(ctx, t, fileID, neu.Spuren, neu.Kapitel)
		}
		// Ohne Messung: Die alten Spurnummern stimmen vielleicht nicht
		// mehr — beim nächsten Abspielen neu messen.
		_, err = t.ExecContext(ctx, `UPDATE media_file SET spuren_gelesen = 0 WHERE id = ?`, fileID)
		return err
	})
}
