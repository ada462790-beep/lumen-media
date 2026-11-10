// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

// Package store hält die Datenbank und alle Abfragen.
//
// Der Zuschnitt dieses Pakets ist Absicht: alles, was einem einzelnen Profil
// gehört — Fortschritt, Bewertungen, Merkliste — liegt in playback.go, und
// jede Funktion dort verlangt eine Konto-Kennung als ersten Parameter. Es
// gibt keine Funktion, die über Konten hinweg zusammenzählt. Wer eine
// einbauen will, muss diese Regel bewusst brechen; privacy_test.go merkt es.
package store

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

//go:embed schema.sql
var schemaSQL string

// ErrNotFound kommt zurück, wenn eine Zeile fehlt. Die API macht daraus 404.
var ErrNotFound = errors.New("nicht gefunden")

type Store struct {
	db *sql.DB
	// stoerungen zählt Störungen beim Abspielen, statt sie einzeln ins
	// Protokoll zu schreiben (stoerungen.go).
	stoerungen stoerungsZaehler
	// stapel ist gesetzt, solange Stapel läuft: Alles, was über s.tx
	// schreibt, landet dann in dieser einen Transaktion.
	stapel *sql.Tx
	// geholtOrdner: wo von OpenSubtitles geholte Untertitel liegen
	// (spuren.go, SetGeholteUntertitel). Leer = keine.
	geholtOrdner string
}

// Open legt die Datenbank an oder öffnet sie und bringt das Schema auf Stand.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("ordner anlegen: %w", err)
	}
	// Hat der Besitzer eine Sicherung zum Wiederherstellen gewählt, wird
	// sie jetzt eingesetzt (sicherung.go).
	vorher, err := wiederherstellenWennBereit(path)
	if err != nil {
		return nil, err
	}
	// _txlock=immediate: eine Transaktion, die erst liest und dann schreibt,
	// meldet sich gleich zu Beginn als Schreiberin an. Sonst scheitert sie
	// im WAL-Betrieb sofort mit „database is locked", sobald ein anderer
	// Schreiber dazwischen fertig wurde — und _busy_timeout hilft dagegen
	// nicht. Das traf das Bilden der Filmreihen, wenn zwei Bibliotheken
	// gleichzeitig geprüft wurden.
	// _secure_delete=on: Gelöschtes wird mit Nullen überschrieben. Wer ein
	// Profil, eine Liste oder seinen Fortschritt löscht, soll es nicht aus
	// freien Seiten der Datei zurückholen können.
	dsn := path + "?_busy_timeout=5000&_journal_mode=WAL&_foreign_keys=on&_synchronous=NORMAL&_txlock=immediate&_secure_delete=on"
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}
	// SQLite schreibt nur aus einer Verbindung gleichzeitig sauber; Lesen
	// darf parallel laufen. Ein knapper Pool erspart gesperrte Datenbanken.
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(8)
	db.SetConnMaxLifetime(time.Hour)

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("datenbank öffnen: %w", err)
	}
	if err := vorDemNachruesten(db, path); err != nil {
		_ = db.Close()
		return nil, err
	}
	if _, err := db.Exec(schemaSQL); err != nil {
		return nil, fmt.Errorf("schema anlegen: %w", err)
	}
	if err := nachruesten(db); err != nil {
		return nil, fmt.Errorf("schema nachrüsten: %w", err)
	}
	if err := umbauen(db); err != nil {
		return nil, fmt.Errorf("datenbank umbauen: %w", err)
	}
	if err := nachUmbauIndizes(db); err != nil {
		return nil, fmt.Errorf("indizes anlegen: %w", err)
	}
	if _, err := db.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, SchemaStand)); err != nil {
		return nil, fmt.Errorf("schema-stand setzen: %w", err)
	}
	if err := optimieren(context.Background(), db); err != nil {
		return nil, fmt.Errorf("statistik: %w", err)
	}
	// Die Datenbank enthält Prüfwerte der PINs, das Geheimnis der
	// Authenticator-App und die Schlüssel für TMDb: nur für den Benutzer,
	// unter dem Lumen läuft.
	for _, p := range []string{path, path + "-wal", path + "-shm"} {
		_ = os.Chmod(p, 0o600)
	}
	// Die ersetzte Datenbank wird nicht aufbewahrt: Sie wäre eine dritte
	// Generation neben den zwei Sicherungen.
	if vorher != "" {
		for _, e := range []string{"", "-wal", "-shm"} {
			_ = os.Remove(vorher + e)
		}
	}
	return &Store{db: db}, nil
}

// SchemaStand zählt die Änderungen am Aufbau der Datenbank. Wer
// nachruesten() erweitert, zählt ihn um eins hoch — dann sichert Lumen
// die Datenbank, bevor es sie umbaut.
//
// Stand 11: Tabelle vorspann_erkannt (vorspann_erkennung.go,
// vorspannErkennungTabellen) — neu angelegt, nichts umzurechnen.
//
// Stand 12: Bildschirmzeit (bildschirmzeit.go, bildschirmzeitNachruesten)
// — zwei neue Tabellen, nichts umzurechnen.
const SchemaStand = 12

// umbauen bringt Daten einer älteren Fassung auf den neuen Stand — jeder
// Schritt genau einmal, je nach dem Stand, den die Datenbank mitbringt.
func umbauen(db *sql.DB) error {
	var stand int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&stand); err != nil {
		return err
	}

	// Stand 1: Bibliotheken sahen jede Stunde nach. Wer das nie verstellt
	// hat, bekommt einmal täglich um 4 Uhr — die Platten dürfen schlafen.
	// Wer selbst etwas eingestellt hat, behält es.
	if stand < 1 {
		if _, err := db.Exec(`UPDATE library SET scan_interval_min = 1440, scan_at = ?
		                       WHERE scan_interval_min = 60 AND scan_at = ''`, StandardScanZeit); err != nil {
			return fmt.Errorf("scan-takt umstellen: %w", err)
		}
	}

	// Stand 2: keine Uhrzeiten mehr, die mit Schauen zusammenhängen.
	//   - progress.updated_at (sekundengenau) wird zur laufenden Nummer je
	//     Profil; die Reihenfolge von „Weiterschauen" bleibt erhalten.
	//   - Geräte „zuletzt gesehen" und Bewertungen nur noch auf den Tag.
	if stand < 2 {
		if err := progressOhneUhrzeit(db); err != nil {
			return err
		}
		for _, q := range []string{
			`UPDATE token SET last_seen_at = (last_seen_at / 86400) * 86400,
			                  created_at   = (created_at / 86400) * 86400`,
			// Gilt für jedes Konto gleich und liest nichts aus.
			`UPDATE rating SET updated_at = (updated_at / 86400) * 86400 WHERE account_id IS NOT NULL`,
		} {
			if _, err := db.Exec(q); err != nil {
				return err
			}
		}
	}

	// Stand 4: Die Zuordnung kann mehr (Ordner vor Dateiname, bekannterer
	// Treffer gewinnt bei gleichem Namen). Was bisher nicht erkannt war,
	// wird einmal neu nachgeschlagen — von Hand zugewiesene bleiben.
	if stand < 4 {
		if _, err := db.Exec(`UPDATE item SET match_state = 'pending'
		                       WHERE match_state = 'unmatched' AND kind IN ('movie','series')`); err != nil {
			return fmt.Errorf("nicht erkannte neu nachschlagen: %w", err)
		}
	}

	// Stand 5: Der Scanner liest NFO-Dateien und Bilder neben den Dateien,
	// trennt gleichnamige Serien nach Jahr und Land und kennt durchgezählte
	// und Tagesfolgen (neue Tabellen item_zusatz, lokal_nfo, lokal_bild).
	// Was bisher nicht erkannt war, bekommt damit eine neue Chance —
	// wieder ohne die von Hand zugewiesenen anzurühren.
	if stand < 5 {
		if _, err := db.Exec(`UPDATE item SET match_state = 'pending'
		                       WHERE match_state = 'unmatched' AND kind IN ('movie','series')`); err != nil {
			return fmt.Errorf("nicht erkannte neu nachschlagen: %w", err)
		}
	}
	// Stand 6: „ohne Altersgrenze" heisst nicht mehr 0, sondern
	// OhneAltersgrenze — damit „nur FSK 0" einstellbar wird (schutz.go).
	if stand < 6 {
		if err := altersgrenzeUmstellen(db); err != nil {
			return err
		}
	}
	// Stand 7: titel_klein, original_klein und zuletzt_neu sind neu
	// (nachruesten) und werden einmal für alle Titel gefüllt.
	if stand < 7 {
		if err := titelKleinFuellen(db); err != nil {
			return fmt.Errorf("titel nachtragen: %w", err)
		}
	}
	// Stand 8: Kinder aus älteren Fassungen als Kinderprofile kennzeichnen
	// (schutz.go) — sonst erreichten die neuen Schutzregeln genau die
	// Kinder nicht, die es schon gibt.
	if stand < 8 {
		if err := kinderErkennen(db); err != nil {
			return fmt.Errorf("kinderprofile erkennen: %w", err)
		}
	}
	// Stand 9: Neue Sortierschlüssel (sortieren.go) — natürliche
	// Zahlenfolge, ä wie a, Artikel je Sprache — für alle Titel. Eigene
	// Sortiertitel aus feld_sperre werden zu sort_eigen.
	if stand < 9 {
		if err := sortierungUmbauen(db); err != nil {
			return fmt.Errorf("sortierschlüssel neu rechnen: %w", err)
		}
	}
	// Stand 10: Lumen ist ein Server für Filme und Serien. Musik- und
	// Fotobibliotheken samt allem, was daran hing, und ihre Tabellen
	// fallen weg (musik_fotos_entfernen.go).
	if stand < 10 {
		if err := musikUndFotosEntfernen(db); err != nil {
			return fmt.Errorf("musik und fotos entfernen: %w", err)
		}
	}
	// Stand 12: Bildschirmzeit (bildschirmzeit.go) — zwei neue Tabellen,
	// angelegt in bildschirmzeitNachruesten; an den Daten ändert sich
	// nichts. Der Stand zählt trotzdem hoch, damit vorher gesichert wird.
	return nil
}

// Optimieren bringt die Statistik für den Abfrageplaner auf Stand — nach
// einem Durchlauf, der viele Zeilen gebracht hat (Hausmeister).
func (s *Store) Optimieren(ctx context.Context) error { return optimieren(ctx, s.db) }

// optimieren lässt SQLite seine Statistik (sqlite_stat1) nachführen, wo
// sie fehlt oder veraltet ist.
//
// Ohne Statistik hält der Planer jeden Index für gleich gut. Seit es
// Indizes gibt, die mit kind beginnen (für das Sortieren), nahm er für
// „alle Folgen dieser 60 Serien" (SerienStaende, NeueFolgenSeit) den
// Index über kind und las alle 100 000 Folgen, statt von den Serien
// auszugehen: eine Seite Serien 2 s statt 0,02 s. Mit Statistik weiss
// er, dass kind nur vier Werte hat.
//
// Fehlt die Statistik für item ganz, wird einmal alles gezählt (ANALYZE;
// bei 125 000 Titeln etwa eine Sekunde, auf einem NAS ein paar). Danach
// genügt PRAGMA optimize: Es zählt nur Tabellen neu, die sich stark
// verändert haben. Eine Stichprobe (analysis_limit) reicht hier nicht —
// sie schätzte für kind tausend Zeilen je Wert statt dreissigtausend, und
// der Planer blieb beim falschen Index.
func optimieren(ctx context.Context, db *sql.DB) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	var da int
	_ = conn.QueryRowContext(ctx,
		`SELECT count(*) FROM sqlite_master WHERE name = 'sqlite_stat1'`).Scan(&da)
	if da > 0 {
		_ = conn.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_stat1 WHERE tbl = 'item'`).Scan(&da)
	}
	if da == 0 {
		_, err = conn.ExecContext(ctx, `ANALYZE`)
		return err
	}
	if _, err := conn.ExecContext(ctx, `PRAGMA analysis_limit = 0`); err != nil {
		return err
	}
	_, err = conn.ExecContext(ctx, `PRAGMA optimize`)
	return err
}

// nachUmbauIndizes legt Indizes an, die eine Spalte erst nach umbauen()
// sicher vorfinden. progress_resume stand früher in schema.sql — das läuft
// vor umbauen(), und auf einer Datenbank, deren Spalte noch updated_at
// hiess und die den Index nicht schon unter diesem Namen hatte, brach
// der Start mit „no such column: reihenfolge" ab.
func nachUmbauIndizes(db *sql.DB) error {
	_, err := db.Exec(`CREATE INDEX IF NOT EXISTS progress_resume ON progress (account_id, reihenfolge DESC)`)
	return err
}

// titelKleinFuellen trägt titel_klein, original_klein und zuletzt_neu für
// Titel nach, die eine ältere Fassung angelegt hat. In Go statt in SQL,
// weil lower() in SQLite nur A–Z kennt, „Ä" also nicht kleinschreibt —
// TitelGleich sucht mit strings.ToLower.
func titelKleinFuellen(db *sql.DB) error {
	rows, err := db.Query(`SELECT id, title, COALESCE(original_title,'') FROM item WHERE titel_klein IS NULL`)
	if err != nil {
		return err
	}
	type zeile struct{ id, titel, original string }
	var alle []zeile
	for rows.Next() {
		var z zeile
		if err := rows.Scan(&z.id, &z.titel, &z.original); err != nil {
			rows.Close()
			return err
		}
		alle = append(alle, z)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	for _, z := range alle {
		if _, err := tx.Exec(`UPDATE item SET titel_klein = ?, original_klein = ? WHERE id = ?`,
			titelKlein(z.titel), nullStr(titelKlein(z.original)), z.id); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	for _, q := range []string{
		`UPDATE item SET zuletzt_neu = added_at WHERE zuletzt_neu IS NULL`,
		`UPDATE item SET zuletzt_neu = neu.n
		   FROM (SELECT st.parent_id AS serie, max(e.added_at) AS n
		           FROM item st JOIN item e ON e.parent_id = st.id
		          WHERE st.kind = 'season' GROUP BY st.parent_id) AS neu
		  WHERE item.id = neu.serie AND item.kind = 'series' AND neu.n > item.zuletzt_neu`,
	} {
		if _, err := tx.Exec(q); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// progressOhneUhrzeit benennt die Spalte um und nummeriert je Profil neu:
// 1, 2, 3 … in der bisherigen Reihenfolge. Danach steht keine Uhrzeit
// mehr in der Tabelle.
func progressOhneUhrzeit(db *sql.DB) error {
	var alt int
	_ = db.QueryRow(`SELECT count(*) FROM pragma_table_info('progress') WHERE name = 'updated_at'`).Scan(&alt)
	if alt == 1 {
		if _, err := db.Exec(`ALTER TABLE progress RENAME COLUMN updated_at TO reihenfolge`); err != nil {
			return fmt.Errorf("fortschritt umbenennen: %w", err)
		}
	}
	konten, err := db.Query(`SELECT id FROM account`)
	if err != nil {
		return err
	}
	var ids []string
	for konten.Next() {
		var id string
		if err := konten.Scan(&id); err != nil {
			konten.Close()
			return err
		}
		ids = append(ids, id)
	}
	konten.Close()

	for _, acc := range ids {
		rows, err := db.Query(`SELECT item_id FROM progress WHERE account_id = ?
		                        ORDER BY reihenfolge, item_id`, acc)
		if err != nil {
			return err
		}
		var items []string
		for rows.Next() {
			var it string
			if err := rows.Scan(&it); err != nil {
				rows.Close()
				return err
			}
			items = append(items, it)
		}
		rows.Close()
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		for i, it := range items {
			if _, err := tx.Exec(`UPDATE progress SET reihenfolge = ? WHERE account_id = ? AND item_id = ?`,
				i+1, acc, it); err != nil {
				_ = tx.Rollback()
				return err
			}
		}
		letzter := ""
		if len(items) > 0 {
			letzter = items[len(items)-1]
		}
		if _, err := tx.Exec(`INSERT INTO progress_reihe (account_id, reihe, letzter) VALUES (?,?,?)
		                      ON CONFLICT (account_id) DO UPDATE SET reihe = excluded.reihe, letzter = excluded.letzter`,
			acc, len(items), letzter); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// vorDemNachruesten prüft eine bestehende Datenbank, bevor Lumen sie
// anfasst:
//
//   - Ist sie beschädigt, startet Lumen nicht. Weiterzuschreiben würde den
//     Schaden nur vergrössern.
//   - Stammt sie von einer neueren Lumen-Fassung, startet Lumen auch
//     nicht: Eine ältere Fassung kennt den neueren Aufbau nicht.
//   - Steht ein Umbau an, legt Lumen vorher eine Sicherung daneben
//     („lumen.db.vor-umbau"), nur für den Besitzer lesbar.
func vorDemNachruesten(db *sql.DB, path string) error {
	var tabellen int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type = 'table'`).Scan(&tabellen); err != nil {
		return fmt.Errorf("datenbank lesen: %w", err)
	}
	if tabellen == 0 {
		return nil // ganz neu angelegt, nichts zu schützen
	}

	var ergebnis string
	if err := db.QueryRow(`PRAGMA quick_check`).Scan(&ergebnis); err != nil {
		return fmt.Errorf("datenbank prüfen: %w", err)
	}
	if ergebnis != "ok" {
		return fmt.Errorf("die datenbank %s ist beschädigt (%s) — Lumen startet nicht, "+
			"damit nichts weiter kaputtgeht. Eine Sicherung liegt im selben Ordner "+
			"(lumen.db.vor-umbau oder unter sicherungen/)", path, ergebnis)
	}

	var stand int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&stand); err != nil {
		return fmt.Errorf("schema-stand lesen: %w", err)
	}
	if stand > SchemaStand {
		return fmt.Errorf("die datenbank stammt von einer neueren Lumen-Fassung "+
			"(Stand %d, diese Fassung kennt %d) — bitte die neuere Fassung starten", stand, SchemaStand)
	}
	if stand == SchemaStand {
		return nil
	}

	ziel := path + ".vor-umbau"
	neu := ziel + ".neu"
	_ = os.Remove(neu)
	if _, err := db.Exec(`VACUUM INTO ?`, neu); err != nil {
		return fmt.Errorf("sicherung vor dem umbau: %w", err)
	}
	if err := os.Chmod(neu, 0o600); err != nil {
		return fmt.Errorf("sicherung vor dem umbau: %w", err)
	}
	if err := os.Rename(neu, ziel); err != nil {
		return fmt.Errorf("sicherung vor dem umbau: %w", err)
	}
	slog.Info("Datenbank wird umgebaut", "von", stand, "auf", SchemaStand, "sicherung", ziel)
	return nil
}

// nachruesten ergänzt Spalten, die nach der ersten Fassung dazukamen.
//
// CREATE TABLE IF NOT EXISTS lässt eine bestehende Tabelle, wie sie ist —
// wer Lumen schon eingerichtet hat, bekäme neue Spalten sonst nie. Jede
// Ergänzung hier wird genau einmal wirksam; steht die Spalte schon da,
// meldet SQLite „duplicate column", und das ist dann kein Fehler.
func nachruesten(db *sql.DB) error {
	for _, stmt := range []string{
		// Filmreihen: woher sie kommen, woran sie erkannt wurden, ob der
		// Besitzer sie aufgelöst oder umbenannt hat.
		`ALTER TABLE collection ADD COLUMN source TEXT NOT NULL DEFAULT 'auto'`,
		`ALTER TABLE collection ADD COLUMN schluessel TEXT`,
		`ALTER TABLE collection ADD COLUMN hidden INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE collection ADD COLUMN eigener_name INTEGER NOT NULL DEFAULT 0`,
		// Bilder der Reihe selbst (von TMDb), nicht die des ersten Films.
		`ALTER TABLE collection ADD COLUMN poster TEXT`,
		`ALTER TABLE collection ADD COLUMN backdrop TEXT`,
		`ALTER TABLE collection ADD COLUMN bilder_geholt INTEGER NOT NULL DEFAULT 0`,
		// Bibliotheken halten sich selbst aktuell: alle wie viele Minuten
		// (0 = nie), und was beim letzten Mal herauskam.
		`ALTER TABLE library ADD COLUMN scan_interval_min INTEGER NOT NULL DEFAULT 60`,
		`ALTER TABLE library ADD COLUMN last_result TEXT`,
		`ALTER TABLE library ADD COLUMN scan_at TEXT NOT NULL DEFAULT ''`,
		// Ob Spuren und Kapitel dieser Datei schon gemessen wurden.
		`ALTER TABLE media_file ADD COLUMN spuren_gelesen INTEGER NOT NULL DEFAULT 0`,
		// Gruppen und Verwaltungssperre.
		`ALTER TABLE account ADD COLUMN group_id TEXT`,
		`ALTER TABLE account ADD COLUMN admin_lock_kind TEXT NOT NULL DEFAULT 'none'`,
		`ALTER TABLE account ADD COLUMN admin_hash BLOB`,
		`ALTER TABLE account ADD COLUMN admin_salt BLOB`,
		`ALTER TABLE account ADD COLUMN admin_totp TEXT`,
		// Die Fassung, die der Besitzer für einen Titel gewählt hat.
		`ALTER TABLE item ADD COLUMN bevorzugte_datei TEXT`,
		// Serien: das Jahr der letzten Folge und ob sie abgeschlossen ist.
		`ALTER TABLE item ADD COLUMN end_year INTEGER`,
		`ALTER TABLE item ADD COLUMN laeuft_noch INTEGER`,
		// Rotten Tomatoes in Prozent; -1 = gibt es nicht, NULL = nie gefragt.
		`ALTER TABLE item ADD COLUMN rt_wertung INTEGER`,
		// Titel und Originaltitel klein geschrieben: Darüber findet der
		// Scanner einen Titel, den es schon gibt, mit einem Index statt
		// über die Volltextsuche (TitelGleich).
		`ALTER TABLE item ADD COLUMN titel_klein TEXT`,
		`ALTER TABLE item ADD COLUMN original_klein TEXT`,
		// Wann zuletzt etwas Neues kam: bei Filmen das Hinzufügen, bei
		// Serien die neueste Folge. Danach sortiert „Zuletzt hinzugefügt".
		`ALTER TABLE item ADD COLUMN zuletzt_neu INTEGER`,
		// Ein eigener Sortiertitel (von Hand oder <sorttitle> aus der NFO)
		// und die Originalsprache — aus beiden rechnet sortieren.go den
		// Schlüssel in sort_title.
		`ALTER TABLE item ADD COLUMN sort_eigen TEXT`,
		`ALTER TABLE item ADD COLUMN sprache_original TEXT`,
	} {
		if _, err := db.Exec(stmt); err != nil &&
			!strings.Contains(err.Error(), "duplicate column") {
			return err
		}
	}
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS gruppe (
		   id         TEXT PRIMARY KEY,
		   name       TEXT NOT NULL,
		   verwaltung INTEGER NOT NULL DEFAULT 0,
		   limits     TEXT NOT NULL DEFAULT '')`,
		// Der markierte Vorspann einer Serie (Issue-Nachtrag „Intro überspringen").
		`CREATE TABLE IF NOT EXISTS vorspann (
		   item_id TEXT PRIMARY KEY,
		   von_ms  INTEGER NOT NULL,
		   bis_ms  INTEGER NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS gruppe_bibliothek (
		   group_id   TEXT NOT NULL,
		   library_id TEXT NOT NULL,
		   PRIMARY KEY (group_id, library_id))`,
		// Die Besetzung eines Titels — Name, Rolle, Bild im Bilderordner.
		`CREATE TABLE IF NOT EXISTS besetzung (
		   item_id TEXT NOT NULL REFERENCES item(id) ON DELETE CASCADE,
		   ord     INTEGER NOT NULL,
		   name    TEXT NOT NULL,
		   rolle   TEXT,
		   bild    TEXT,
		   PRIMARY KEY (item_id, ord))`,
		`CREATE INDEX IF NOT EXISTS collection_item_by_item ON collection_item (item_id)`,
		// Indizes auf Spalten, die erst nachgerüstet werden, stehen hier
		// und nicht in schema.sql: Dort liefen sie auf einer alten
		// Datenbank, bevor es die Spalte gibt, und Lumen startete nicht.
		`CREATE INDEX IF NOT EXISTS item_titel_klein ON item (library_id, kind, titel_klein)`,
		// Für das Raster: je Sortierung ein Index, damit SQLite nur die
		// gezeigte Seite liest, statt alle Titel einer Art zu sortieren
		// („USE TEMP B-TREE FOR ORDER BY"). Jeder endet mit sort_title und
		// id wie die Sortierung selbst (sqlOrdnung in sortieren.go); die
		// Ausdrücke müssen dort und hier gleich lauten.
		`DROP INDEX IF EXISTS item_sort_titel`,
		`DROP INDEX IF EXISTS item_sort_neu`,
		`DROP INDEX IF EXISTS item_sort_jahr`,
		`DROP INDEX IF EXISTS item_sort_wertung`,
		`CREATE INDEX IF NOT EXISTS item_ord_titel ON item (kind, sort_title, id)`,
		`CREATE INDEX IF NOT EXISTS item_ord_neu ON item (kind, zuletzt_neu DESC, sort_title, id)`,
		`CREATE INDEX IF NOT EXISTS item_ord_erschienen ON item (kind, COALESCE(premiered, CAST(year AS TEXT)) DESC, sort_title, id)`,
		`CREATE INDEX IF NOT EXISTS item_ord_wertung ON item (kind, imdb_rating DESC, sort_title, id)`,
		`CREATE INDEX IF NOT EXISTS item_ord_laufzeit ON item (kind, runtime_ms IS NULL, runtime_ms, sort_title, id)`,
		// Dateien, die gerade fehlen (Verwaltung → Aufräumen); fast alle
		// Zeilen haben hier NULL und stehen gar nicht im Index.
		// Was noch nachzuschlagen ist und was nicht erkannt wurde: Die
		// Verwaltung zählt das bei jedem Aufruf, der Metadaten-Durchgang
		// holt es in Häppchen. Ohne Index ging beides über alle Titel.
		`CREATE INDEX IF NOT EXISTS item_by_state ON item (match_state, kind)`,
		`CREATE INDEX IF NOT EXISTS file_fehlt ON media_file (missing_since) WHERE missing_since IS NOT NULL`,
		`CREATE INDEX IF NOT EXISTS playlist_by_account ON playlist (account_id)`,
		`CREATE INDEX IF NOT EXISTS playlist_item_by_list ON playlist_item (playlist_id, ord)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}
	// Land, Durchzählung, Doppelfolgen, NFO und lokale Bilder (lokal.go).
	for _, stmt := range lokalTabellen {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}
	// Automatisch erkannter Vorspann und Abspann (vorspann_erkennung.go).
	if err := vorspannErkennungTabellen(db); err != nil {
		return err
	}
	// Bildschirmzeit (bildschirmzeit.go, Stand 12).
	if err := bildschirmzeitNachruesten(db); err != nil {
		return err
	}
	// Handbearbeitung, eigene Sammlungen, Untertitel (funktionen_schema.go).
	return funktionenNachruesten(db)
}

func (s *Store) Close() error {
	if tag, alt := s.stoerungen.alles(); alt != nil {
		s.stoerungenSchreiben(context.Background(), tag, alt)
	}
	return s.db.Close()
}

// DB gibt die rohe Verbindung heraus. Nur für Wartungsaufgaben gedacht,
// nicht für Abfragen auf private Tabellen.
func (s *Store) DB() *sql.DB { return s.db }

// tx führt fn in einer Transaktion aus und räumt bei Fehlern auf. Innerhalb
// eines Stapels ist es dessen Transaktion.
func (s *Store) tx(ctx context.Context, fn func(*sql.Tx) error) error {
	if s.stapel != nil {
		return fn(s.stapel)
	}
	t, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(t); err != nil {
		_ = t.Rollback()
		return err
	}
	return t.Commit()
}

// Stapel schreibt vieles in einer Transaktion statt in vielen.
//
// Jede Transaktion schreibt beim Abschluss alle Seiten, die sie berührt
// hat, ins WAL — bei einer neuen Folge sind das Titel, Suchtext, Datei,
// Spuren und ein halbes Dutzend Indizes. Der erste Durchlauf über 100 000
// Folgen schrieb so über 11 GB ins WAL. In einem Stapel wird jede Seite
// einmal geschrieben, egal wie oft sie sich darin ändert.
//
// fn bekommt einen Store, dessen schreibende Methoden über s.tx in die
// Transaktion des Stapels gehen (UpsertItem, UpsertFile, SpurenSetzen …).
// Methoden, die direkt über s.db schreiben, gehören nicht hinein — sie
// warteten auf die Sperre, die der Stapel selbst hält. Lesen über s.db
// geht, sieht aber nur, was schon festgeschrieben ist. Ein Stapel soll
// kurz sein: Solange er läuft, wartet jeder andere Schreiber (etwa der
// Stand einer Wiedergabe). Nichts Langsames darin — kein FFprobe, kein
// Netz.
func (s *Store) Stapel(ctx context.Context, fn func(*Store) error) error {
	if s.stapel != nil {
		return fn(s)
	}
	t, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	innen := &Store{db: s.db, stapel: t, geholtOrdner: s.geholtOrdner}
	if err := fn(innen); err != nil {
		_ = t.Rollback()
		return err
	}
	return t.Commit()
}

// ---------------------------------------------------------------- Einstellungen

func (s *Store) Setting(ctx context.Context, key, fallback string) string {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM setting WHERE key = ?`, key).Scan(&v)
	if err != nil {
		return fallback
	}
	return v
}

func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO setting (key, value) VALUES (?, ?)
		 ON CONFLICT (key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// ---------------------------------------------------------------- Protokoll

type LogLine struct {
	At      time.Time `json:"at"`
	Level   string    `json:"level"`
	Area    string    `json:"area"`
	Message string    `json:"message"`
}

// Log schreibt eine Zeile ins Protokoll. Was hier landet, beschreibt den
// Server — Geräte, Platten, Umwandlungen. Niemals, welcher Titel läuft.
//
// Zeilen über Wiedergaben (Bereich „stream") kommen nicht einzeln hinein:
// „Umwandlung gescheitert um 01:47" wäre schon ein Stück Verlauf. Sie
// werden je Tag gezählt und als Zusammenfassung geschrieben
// (stoerungen.go). Ihr Text darf darum nichts Einzelnes enthalten.
func (s *Store) Log(ctx context.Context, level, area, message string) {
	if area == "stream" {
		s.stoerungZaehlen(ctx, message)
		return
	}
	at := time.Now().Unix()
	_, _ = s.db.ExecContext(ctx,
		`INSERT INTO log_line (at, level, area, message) VALUES (?, ?, ?, ?)`,
		at, level, area, message)
}

func (s *Store) RecentLog(ctx context.Context, level string, limit int) ([]LogLine, error) {
	s.stoerungenAbschliessen(ctx)
	q := `SELECT at, level, area, message FROM log_line`
	args := []any{}
	if level != "" && level != "all" {
		q += ` WHERE level = ?`
		args = append(args, level)
	}
	q += ` ORDER BY at DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []LogLine{}
	for rows.Next() {
		var l LogLine
		var at int64
		if err := rows.Scan(&at, &l.Level, &l.Area, &l.Message); err != nil {
			return nil, err
		}
		l.At = time.Unix(at, 0)
		out = append(out, l)
	}
	return out, rows.Err()
}

// TrimLog wirft alte Zeilen weg. Der Besitzer stellt die Aufbewahrung ein.
func (s *Store) TrimLog(ctx context.Context, keep time.Duration) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM log_line WHERE at < ?`, time.Now().Add(-keep).Unix())
	return err
}
