// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

// Metadaten von Hand — und was davor schützt, dass sie wieder verschwinden.
//
// Wer einen Titel von Hand bearbeitet, hat einen Grund: TMDb kennt das
// Heimvideo nicht, die deutsche Beschreibung ist falsch, das Poster
// gefällt nicht. „Metadaten neu laden" oder eine NFO-Datei dürfen das
// nicht still wieder überschreiben. Darum wird jedes bearbeitete Feld
// gesperrt (Tabelle feld_sperre). Die Rangfolge ist:
//
//	Handbearbeitung  >  NFO-Datei  >  TMDb
//
// Durchgesetzt wird das an der einen Stelle, durch die alle schreiben:
// UpsertItem (Scanner, Metadaten, NFO), SetArtwork und BesetzungSetzen.
// So muss keine Quelle selbst daran denken — auch keine, die erst
// später dazukommt.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Die Felder, die sich sperren lassen. Die Namen sind die der
// JSON-Schnittstelle, damit die Apps sie unverändert benutzen können.
const (
	FeldTitel         = "title"
	FeldOriginaltitel = "originalTitle"
	FeldSortiertitel  = "sortTitle"
	FeldJahr          = "year"
	FeldBeschreibung  = "overview"
	FeldGenres        = "genres"
	FeldFreigabe      = "ageRating"
	FeldErschienen    = "premiered"
	FeldBesetzung     = "cast"
	FeldPoster        = "poster"
	FeldHintergrund   = "backdrop"
	FeldStaffel       = "seasonNo"
	FeldFolge         = "episodeNo"
)

// SperrFelder sind alle Felder, die eine Sperre tragen können.
var SperrFelder = []string{FeldTitel, FeldOriginaltitel, FeldSortiertitel, FeldJahr,
	FeldBeschreibung, FeldGenres, FeldFreigabe, FeldErschienen, FeldBesetzung,
	FeldPoster, FeldHintergrund, FeldStaffel, FeldFolge}

// SperrFeldBekannt sagt, ob es dieses Feld gibt.
func SperrFeldBekannt(f string) bool {
	for _, x := range SperrFelder {
		if x == f {
			return true
		}
	}
	return false
}

type abfrager interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func sperrenLesen(ctx context.Context, q abfrager, itemID string) (map[string]bool, error) {
	rows, err := q.QueryContext(ctx, `SELECT feld FROM feld_sperre WHERE item_id = ?`, itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var f string
		if err := rows.Scan(&f); err != nil {
			return nil, err
		}
		out[f] = true
	}
	return out, rows.Err()
}

// Sperren nennt die gesperrten Felder eines Titels. Für die NFO-Übernahme
// und die Oberfläche (Schlosssymbol).
func (s *Store) Sperren(ctx context.Context, itemID string) (map[string]bool, error) {
	return sperrenLesen(ctx, s.db, itemID)
}

// Sortiertitel gibt den eigenen Sortiertitel — von Hand eingetragen oder
// aus der NFO — oder "".
func (s *Store) Sortiertitel(ctx context.Context, itemID string) string {
	var v sql.NullString
	_ = s.db.QueryRowContext(ctx, `SELECT sort_eigen FROM item WHERE id = ?`, itemID).Scan(&v)
	return v.String
}

// sperrenAnwenden setzt in it alle gesperrten Felder auf den Stand der
// Datenbank zurück, bevor UpsertItem schreibt. Was die Quelle mitbrachte,
// fällt für diese Felder still weg.
func sperrenAnwenden(ctx context.Context, t *sql.Tx, it *Item) error {
	if it.ID == "" {
		return nil
	}
	gesperrt, err := sperrenLesen(ctx, t, it.ID)
	if err != nil || len(gesperrt) == 0 {
		return err
	}
	alt, err := scanItem(t.QueryRowContext(ctx, `SELECT `+itemCols+` FROM item WHERE id = ?`, it.ID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if gesperrt[FeldTitel] {
		it.Title = alt.Title
	}
	if gesperrt[FeldOriginaltitel] {
		it.OriginalTitle = alt.OriginalTitle
	}
	if gesperrt[FeldSortiertitel] {
		// Der eigene Sortiertitel bleibt — auch gegen einen aus der NFO.
		// Den Schlüssel rechnet UpsertItem daraus neu.
		it.SortEigen = alt.SortEigen
	}
	if gesperrt[FeldJahr] {
		it.Year = alt.Year
	}
	if gesperrt[FeldBeschreibung] {
		it.Overview = alt.Overview
	}
	if gesperrt[FeldFreigabe] {
		it.AgeRating, it.AgeYears = alt.AgeRating, alt.AgeYears
	}
	if gesperrt[FeldErschienen] {
		it.Premiered = alt.Premiered
	}
	if gesperrt[FeldGenres] {
		// Die gespeicherten Genres einsetzen: UpsertItem schreibt sie dann
		// unverändert zurück, und der Suchtext stimmt. Sind es keine (von
		// Hand geleert), lässt UpsertItem die Genres ohnehin in Ruhe.
		g, err := genresTx(ctx, t, it.ID)
		if err != nil {
			return err
		}
		it.Genres = g
	}
	// Ein von Hand gepflegter Titel ist kein „nicht erkannt" mehr — auch
	// wenn TMDb beim nächsten Neuladen wieder nichts findet.
	if it.MatchState == "unmatched" {
		it.MatchState = "manual"
	}
	return nil
}

func genresTx(ctx context.Context, t *sql.Tx, itemID string) ([]string, error) {
	rows, err := t.QueryContext(ctx, `SELECT name FROM genre WHERE item_id = ? ORDER BY name`, itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectStrings(rows)
}

// gesperrt: ist dieses eine Feld gesperrt?
func gesperrt(ctx context.Context, q abfrager, itemID, feld string) bool {
	var n int
	_ = q.QueryRowContext(ctx,
		`SELECT 1 FROM feld_sperre WHERE item_id = ? AND feld = ?`, itemID, feld).Scan(&n)
	return n == 1
}

// ---------------------------------------------------------------- Bearbeiten

// Handbearbeitung trägt die Felder, die jemand geändert hat. nil heisst:
// nicht angefasst. Ein leerer Text ist eine Änderung („Beschreibung
// löschen") und wird genauso gesperrt.
type Handbearbeitung struct {
	Title         *string
	OriginalTitle *string
	SortTitle     *string
	Year          *int
	Overview      *string
	Genres        *[]string
	AgeRating     *string
	AgeYears      *int // aus AgeRating berechnet; nil = ungeprüft
	Premiered     *string
	Cast          *[]Person
	SeasonNo      *int
	EpisodeNo     *int
}

// ErrNummerBelegt: Eine Staffel- oder Folgennummer gibt es an dieser
// Stelle schon.
var ErrNummerBelegt = errors.New("diese Nummer ist schon vergeben")

// MetadatenVonHand schreibt die Änderungen und sperrt jedes geänderte
// Feld. Alles in einer Transaktion: Entweder gilt die ganze Bearbeitung
// oder nichts davon.
func (s *Store) MetadatenVonHand(ctx context.Context, itemID string, h Handbearbeitung) error {
	return s.tx(ctx, func(t *sql.Tx) error {
		it, err := scanItem(t.QueryRowContext(ctx, `SELECT `+itemCols+` FROM item WHERE id = ?`, itemID))
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		sperren := func(feld string, anzeige any) error {
			_, err := t.ExecContext(ctx,
				`INSERT INTO feld_sperre (item_id, feld, anzeige) VALUES (?,?,?)
				 ON CONFLICT (item_id, feld) DO UPDATE SET anzeige = excluded.anzeige`,
				itemID, feld, anzeige)
			return err
		}
		setze := func(spalte string, wert any, feld string) error {
			if _, err := t.ExecContext(ctx, `UPDATE item SET `+spalte+` = ? WHERE id = ?`, wert, itemID); err != nil {
				return err
			}
			return sperren(feld, nil)
		}

		if h.Title != nil {
			it.Title = *h.Title
			if err := setze("title", it.Title, FeldTitel); err != nil {
				return err
			}
		}
		// Ohne eigenen Sortiertitel folgt die Einordnung dem Titel; den
		// Schlüssel rechnet sortierungNachrechnen unten neu.
		if h.SortTitle != nil {
			eigen := strings.TrimSpace(*h.SortTitle)
			if _, err := t.ExecContext(ctx, `UPDATE item SET sort_eigen = ? WHERE id = ?`,
				nullStr(eigen), itemID); err != nil {
				return err
			}
			if eigen == "" {
				// Leer: zurück zur Einordnung nach dem Titel, ohne Sperre.
				if _, err := t.ExecContext(ctx, `DELETE FROM feld_sperre WHERE item_id = ? AND feld = ?`,
					itemID, FeldSortiertitel); err != nil {
					return err
				}
			} else if err := sperren(FeldSortiertitel, eigen); err != nil {
				return err
			}
		}
		if h.OriginalTitle != nil {
			it.OriginalTitle = *h.OriginalTitle
			if err := setze("original_title", nullStr(*h.OriginalTitle), FeldOriginaltitel); err != nil {
				return err
			}
		}
		if h.Year != nil {
			if err := setze("year", nullInt(*h.Year), FeldJahr); err != nil {
				return err
			}
		}
		if h.Overview != nil {
			if err := setze("overview", nullStr(*h.Overview), FeldBeschreibung); err != nil {
				return err
			}
		}
		if h.Premiered != nil {
			if err := setze("premiered", nullStr(*h.Premiered), FeldErschienen); err != nil {
				return err
			}
		}
		if h.AgeRating != nil {
			if _, err := t.ExecContext(ctx, `UPDATE item SET age_rating = ?, age_years = ? WHERE id = ?`,
				nullStr(*h.AgeRating), h.AgeYears, itemID); err != nil {
				return err
			}
			if err := sperren(FeldFreigabe, nil); err != nil {
				return err
			}
		}
		if h.Genres != nil {
			it.Genres = *h.Genres
			if _, err := t.ExecContext(ctx, `DELETE FROM genre WHERE item_id = ?`, itemID); err != nil {
				return err
			}
			for _, g := range *h.Genres {
				if _, err := t.ExecContext(ctx,
					`INSERT OR IGNORE INTO genre (item_id, name) VALUES (?,?)`, itemID, g); err != nil {
					return err
				}
			}
			if err := sperren(FeldGenres, nil); err != nil {
				return err
			}
		} else {
			it.Genres, _ = genresTx(ctx, t, itemID)
		}
		if h.Cast != nil {
			if err := besetzungVonHand(ctx, t, itemID, *h.Cast); err != nil {
				return err
			}
			if err := sperren(FeldBesetzung, nil); err != nil {
				return err
			}
		}
		if h.SeasonNo != nil {
			if err := staffelNummer(ctx, t, it, *h.SeasonNo); err != nil {
				return err
			}
			if err := sperren(FeldStaffel, nil); err != nil {
				return err
			}
		}
		if h.EpisodeNo != nil {
			if it.Kind != "episode" {
				return fmt.Errorf("eine Folgennummer gibt es nur bei Folgen")
			}
			var belegt int
			_ = t.QueryRowContext(ctx, `SELECT 1 FROM item WHERE parent_id = ? AND kind = 'episode'
			                             AND episode_no = ? AND id != ?`, it.ParentID, *h.EpisodeNo, itemID).Scan(&belegt)
			if belegt == 1 {
				return ErrNummerBelegt
			}
			if err := setze("episode_no", *h.EpisodeNo, FeldFolge); err != nil {
				return err
			}
		}

		// Ein Titel, den TMDb nicht kannte (Heimvideo, Konzert), gilt ab
		// jetzt als von Hand gepflegt — er steht nicht mehr unter „nicht
		// erkannt", und kein Durchlauf schlägt ihn neu nach. Bei einem
		// noch ausstehenden nur, wenn Titel oder Jahr gesetzt wurden:
		// Sonst würde ein neues Poster allein die Suche bei TMDb abstellen.
		neu := it.MatchState
		switch {
		case it.MatchState == "unmatched":
			neu = "manual"
		case it.MatchState == "pending" && (h.Title != nil || h.Year != nil):
			neu = "manual"
		}
		if _, err := t.ExecContext(ctx, `UPDATE item SET match_state = ?, updated_at = ? WHERE id = ?`,
			neu, time.Now().Unix(), itemID); err != nil {
			return err
		}
		if h.Title != nil || h.SortTitle != nil || h.OriginalTitle != nil {
			if err := sortierungNachrechnen(ctx, t, itemID); err != nil {
				return err
			}
		}

		// Suchtext nachführen — sonst fände die Suche den neuen Titel nicht.
		hay := strings.ToLower(strings.Join([]string{it.Title, it.OriginalTitle,
			strings.Join(it.Genres, " ")}, " "))
		_, err = t.ExecContext(ctx,
			`INSERT INTO item_search (item_id, haystack) VALUES (?,?)
			 ON CONFLICT (item_id) DO UPDATE SET haystack = excluded.haystack`,
			itemID, fold(hay)+" "+fold(ohnePunkte(hay)))
		return err
	})
}

// staffelNummer ändert die Nummer einer Staffel — und die der Folgen
// darin, denn TMDb fragt Folgen nach Staffel und Folge ab.
func staffelNummer(ctx context.Context, t *sql.Tx, it *Item, nr int) error {
	if it.Kind != "season" {
		return fmt.Errorf("eine Staffelnummer lässt sich nur an der Staffel ändern")
	}
	var belegt int
	_ = t.QueryRowContext(ctx, `SELECT 1 FROM item WHERE parent_id = ? AND kind = 'season'
	                             AND season_no = ? AND id != ?`, it.ParentID, nr, it.ID).Scan(&belegt)
	if belegt == 1 {
		return ErrNummerBelegt
	}
	if _, err := t.ExecContext(ctx, `UPDATE item SET season_no = ? WHERE id = ?`, nr, it.ID); err != nil {
		return err
	}
	_, err := t.ExecContext(ctx, `UPDATE item SET season_no = ? WHERE parent_id = ? AND kind = 'episode'`, nr, it.ID)
	return err
}

// besetzungVonHand schreibt die Besetzung neu. Wer schon dabei war,
// behält sein Bild — es hängt am Namen, nicht an der Stelle.
func besetzungVonHand(ctx context.Context, t *sql.Tx, itemID string, leute []Person) error {
	rows, err := t.QueryContext(ctx, `SELECT name, COALESCE(bild,'') FROM besetzung WHERE item_id = ?`, itemID)
	if err != nil {
		return err
	}
	bilder := map[string]string{}
	for rows.Next() {
		var n, b string
		if err := rows.Scan(&n, &b); err != nil {
			rows.Close()
			return err
		}
		bilder[strings.ToLower(n)] = b
	}
	rows.Close()
	if _, err := t.ExecContext(ctx, `DELETE FROM besetzung WHERE item_id = ?`, itemID); err != nil {
		return err
	}
	for i, p := range leute {
		bild := bilder[strings.ToLower(p.Name)]
		if _, err := t.ExecContext(ctx,
			`INSERT INTO besetzung (item_id, ord, name, rolle, bild) VALUES (?,?,?,?,?)`,
			itemID, i, p.Name, nullStr(p.Rolle), nullStr(bild)); err != nil {
			return err
		}
	}
	return nil
}

// BildVonHand hinterlegt ein Poster oder einen Hintergrund, den jemand
// hochgeladen oder verlinkt hat, und sperrt es. Gibt den Namen des
// vorigen Bildes zurück, damit es weggeräumt werden kann.
func (s *Store) BildVonHand(ctx context.Context, itemID, kind, datei string) (vorher string, err error) {
	if kind != FeldPoster && kind != FeldHintergrund {
		return "", fmt.Errorf("unbekannte bildart")
	}
	err = s.tx(ctx, func(t *sql.Tx) error {
		var v sql.NullString
		_ = t.QueryRowContext(ctx, `SELECT path FROM artwork WHERE item_id = ? AND kind = ?`, itemID, kind).Scan(&v)
		vorher = v.String
		if _, err := t.ExecContext(ctx,
			`INSERT INTO artwork (item_id, kind, path, source_url) VALUES (?,?,?,NULL)
			 ON CONFLICT (item_id, kind) DO UPDATE SET path = excluded.path, source_url = NULL`,
			itemID, kind, datei); err != nil {
			return err
		}
		_, err := t.ExecContext(ctx,
			`INSERT OR IGNORE INTO feld_sperre (item_id, feld) VALUES (?,?)`, itemID, kind)
		return err
	})
	return vorher, err
}

// SperreAufheben gibt ein Feld wieder frei. Der Wert bleibt, bis die
// nächste Quelle (NFO, TMDb) einen anderen bringt.
func (s *Store) SperreAufheben(ctx context.Context, itemID, feld string) error {
	if !SperrFeldBekannt(feld) {
		return fmt.Errorf("unbekanntes feld")
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM feld_sperre WHERE item_id = ? AND feld = ?`, itemID, feld)
	return err
}

// GesperrteFolgeZuPfad: Hängt die Datei an einer Folge dieser Serie,
// deren Nummer von Hand gesetzt ist, gibt sie deren Kennung. Der Scanner
// fragt das, bevor er nach den Nummern im Dateinamen sortiert — sonst
// zöge er die Datei bei jedem Durchlauf zurück an die alte Stelle.
func (s *Store) GesperrteFolgeZuPfad(ctx context.Context, path, seriesID string) string {
	var id string
	_ = s.db.QueryRowContext(ctx,
		`SELECT e.id FROM media_file f
		   JOIN item e  ON e.id = f.item_id AND e.kind = 'episode'
		   JOIN item st ON st.id = e.parent_id
		  WHERE f.path = ? AND st.parent_id = ?
		    AND (EXISTS (SELECT 1 FROM feld_sperre x WHERE x.item_id = e.id AND x.feld IN ('episodeNo','seasonNo'))
		      OR EXISTS (SELECT 1 FROM feld_sperre x WHERE x.item_id = st.id AND x.feld = 'seasonNo'))`,
		path, seriesID).Scan(&id)
	return id
}
