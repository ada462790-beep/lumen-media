// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

// Messen, wie lange die Datenbank braucht — und worauf sie wartet.
//
// Der Besitzer einer NAS meldete: „Beim ersten Öffnen 10–30 s, dann
// vereinzelt wieder." Ohne Zahlen bleibt so etwas Raten. Darum läuft
// jede Verbindung über einen dünnen Mantel um den SQLite-Treiber:
//
//   - Jede Anfrage der Bedienung bekommt einen Zähler in ihren Kontext
//     (MitMessung). Der Mantel zählt dort die Zeit in der Datenbank und
//     — getrennt — die Zeit, die ein Schreiben auf die Schreibsperre
//     wartete (BEGIN IMMEDIATE). Die API meldet beides im Kopf
//     Server-Timing und schreibt langsame Anfragen ins Protokoll.
//   - Wie lange eine Transaktion die Schreibsperre hält, wird für alle
//     gemessen (Schreibsperren). Lange Haltezeiten sind genau das, worauf
//     die Bedienung warten müsste.
//   - Jedes Schreiben ausserhalb des Protokolls zählt Stand() hoch. Daran
//     erkennen Zwischenspeicher (Startseite, Listen), dass sich etwas
//     geändert hat. StandHart() lässt die Arbeit im Hintergrund weg,
//     StandMediathek() das Schreiben in die Tabellen eines Profils.
//
// Gemessen wird nur Zeit und Anzahl — nie, was gelesen oder geschrieben
// wurde.

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	sqlite3 "github.com/mattn/go-sqlite3"
)

// treiberName: unter diesem Namen meldet sich der Mantel bei database/sql.
const treiberName = "sqlite3_lumen"

func init() {
	sql.Register(treiberName, &messTreiber{innen: &sqlite3.SQLiteDriver{}})
}

// Messung zählt für eine Anfrage, wie lange sie in der Datenbank war.
type Messung struct {
	db     atomic.Int64 // Nanosekunden in der Datenbank insgesamt
	warten atomic.Int64 // davon: Warten auf die Schreibsperre
	zuege  atomic.Int64 // Anzahl Abfragen
}

// DB: Zeit in der Datenbank. Warten: davon auf die Schreibsperre gewartet.
func (m *Messung) DB() time.Duration     { return time.Duration(m.db.Load()) }
func (m *Messung) Warten() time.Duration { return time.Duration(m.warten.Load()) }
func (m *Messung) Abfragen() int64       { return m.zuege.Load() }

type messKey struct{}

// MitMessung hängt einen neuen Zähler an den Kontext.
func MitMessung(ctx context.Context) (context.Context, *Messung) {
	m := &Messung{}
	return context.WithValue(ctx, messKey{}, m), m
}

func messungVon(ctx context.Context) *Messung {
	if ctx == nil {
		return nil
	}
	m, _ := ctx.Value(messKey{}).(*Messung)
	return m
}

func (m *Messung) zaehlen(d time.Duration) {
	if m != nil {
		m.db.Add(int64(d))
		m.zuege.Add(1)
	}
}

func (m *Messung) gewartet(d time.Duration) {
	if m != nil {
		m.warten.Add(int64(d))
	}
}

// ---------------------------------------------------------------- Stand

var (
	stand            atomic.Int64
	standHart        atomic.Int64
	standMediathek   atomic.Int64
	standFortschritt atomic.Int64
)

// Stand zählt jedes Schreiben in die Datenbank, ausser ins Protokoll und
// in nebensächliche Merker (OhneStand). Wer eine Antwort zwischenspeichert,
// merkt sich den Stand dazu: Ist er weitergerückt, gilt die Antwort nicht
// mehr.
func Stand() int64 { return stand.Load() }

// StandHart zählt nur, was nicht aus der Arbeit im Hintergrund kommt
// (Hintergrund): Haken, Merkliste, Einstellungen — alles, was jemand
// selbst getan hat und sofort sehen soll. Ein Durchlauf oder das
// Nachschlagen von Metadaten rückt nur Stand weiter; eine gemerkte Antwort
// darf dann noch einmal kommen, während sie im Hintergrund neu entsteht
// (api/antwortspeicher.go).
func StandHart() int64 { return standHart.Load() }

// StandMediathek zählt nur Schreiben ausserhalb der Tabellen, die einem
// Profil gehören (privateTabellen), und ausserhalb von nebenTabellen: Ein
// gespeicherter Stand beim Schauen
// ändert nichts an Dateien, Reihen oder Abzeichen. Danach richten sich
// Zwischenspeicher, die nur von der Mediathek abhängen (kennzahlen.go,
// reihen.go).
func StandMediathek() int64 { return standMediathek.Load() }

// standFortschrittJetzt zählt Schreiben in progress — und das Löschen von
// Titeln, Bibliotheken und Konten, das Zeilen dort mitnimmt. Danach
// richtet sich das Abbild des eigenen Fortschritts (fortschritt_abbild.go).
func standFortschrittJetzt() int64 { return standFortschritt.Load() }

type ohneStandKey struct{}

// OhneStand: Schreiben mit diesem Kontext ändert nichts, was eine Seite
// zeigt (Protokoll, „zuletzt gesehen"). Es rückt Stand nicht weiter.
func OhneStand(ctx context.Context) context.Context {
	return context.WithValue(ctx, ohneStandKey{}, true)
}

type hintergrundKey struct{}

// Hintergrund kennzeichnet Arbeit, die niemand gerade angestossen hat und
// auf die niemand wartet: Durchläufe, Metadaten, Wartung. Was sie
// schreibt, rückt Stand weiter, aber nicht StandHart.
func Hintergrund(ctx context.Context) context.Context {
	if imHintergrund(ctx) {
		return ctx
	}
	return context.WithValue(ctx, hintergrundKey{}, true)
}

func imHintergrund(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	v, _ := ctx.Value(hintergrundKey{}).(bool)
	return v
}

func zaehltFuerStand(ctx context.Context, query string) bool {
	if ctx != nil {
		if v, _ := ctx.Value(ohneStandKey{}).(bool); v {
			return false
		}
	}
	return schreibend(query)
}

// privateTabellen gehören einem Profil (schema.sql, playback.go). Was dort
// geschrieben wird, ändert die Mediathek nicht (StandMediathek).
var privateTabellen = map[string]bool{
	"progress": true, "progress_reihe": true, "rating": true, "favorit": true,
	"watchlist": true, "prefs": true, "playlist": true, "playlist_item": true,
	"token": true, "bildschirmzeit_tag": true, "bildschirmzeit_regel": true,
}

// nebenTabellen: Was dort steht, zeigt keine Liste und kein Abzeichen —
// erkannter Vorspann, Versuche bei OpenSubtitles, Einstellungen, Wünsche,
// Mitteilungen, Angaben zu Personen. Die Erkennung im Hintergrund schreibt
// laufend in vorspann_erkannt; StandMediathek bleibt davon stehen, damit
// das Gemerkte (mediathek_merken.go) nicht bei jeder Folge verfällt.
var nebenTabellen = map[string]bool{
	"vorspann_erkannt": true, "untertitel_versuch": true, "setting": true,
	"wish": true, "wish_dabei": true, "notice": true, "person_info": true, "log_line": true,
}

// zielTabelle: die Tabelle, in die eine schreibende Anweisung schreibt
// (INSERT … INTO t, REPLACE INTO t, UPDATE t, DELETE FROM t), klein
// geschrieben; leer, wenn sie sich nicht erkennen lässt.
func zielTabelle(q string) string {
	w := strings.Fields(strings.ToLower(q))
	for i := 0; i < len(w); i++ {
		switch w[i] {
		case "into":
			if i+1 < len(w) {
				return tabellenName(w[i+1])
			}
		case "update":
			j := i + 1
			if j+1 < len(w) && w[j] == "or" {
				j += 2
			}
			if j < len(w) {
				return tabellenName(w[j])
			}
		case "delete":
			if i+2 < len(w) && w[i+1] == "from" {
				return tabellenName(w[i+2])
			}
		}
	}
	return ""
}

func tabellenName(w string) string {
	if k := strings.IndexAny(w, "( "); k >= 0 {
		w = w[:k]
	}
	return strings.Trim(w, `"'`+"`")
}

// standArt: welche Stände eine schreibende Anweisung weiterrückt.
type standArt struct {
	hart, mediathek, fortschritt bool
}

func (a *standArt) dazu(b standArt) {
	a.hart = a.hart || b.hart
	a.mediathek = a.mediathek || b.mediathek
	a.fortschritt = a.fortschritt || b.fortschritt
}

// standArtVon: Was diese schreibende Anweisung ändert. Die Mediathek
// alles ausser privateTabellen und nebenTabellen; den Fortschritt progress
// selbst und das Löschen dessen, woran er hängt (ON DELETE CASCADE).
func standArtVon(ctx context.Context, q string) standArt {
	ziel := zielTabelle(q)
	loeschen := strings.HasPrefix(strings.ToUpper(strings.TrimLeft(q, " \t\r\n")), "DELETE")
	return standArt{
		hart:        !imHintergrund(ctx),
		mediathek:   !privateTabellen[ziel] && !nebenTabellen[ziel],
		fortschritt: ziel == "progress" || (loeschen && (ziel == "item" || ziel == "library" || ziel == "account")),
	}
}

// standWeiter rückt die Stände nach einem Schreiben ausserhalb einer
// Transaktion (oder beim Abschluss einer) weiter.
func standWeiter(a standArt) {
	stand.Add(1)
	if a.hart {
		standHart.Add(1)
	}
	if a.mediathek {
		standMediathek.Add(1)
	}
	if a.fortschritt {
		standFortschritt.Add(1)
	}
}

// schreibend: grob, ob eine Anweisung schreibt. SELECT, PRAGMA (lesend)
// und EXPLAIN zählen nicht; im Zweifel zählt es — ein Zwischenspeicher
// wird dann eben einmal zu oft verworfen.
func schreibend(q string) bool {
	q = strings.TrimLeft(q, " \t\r\n(")
	if len(q) < 6 {
		return false
	}
	switch strings.ToUpper(q[:6]) {
	case "SELECT", "EXPLAI":
		return false
	case "PRAGMA":
		// PRAGMA user_version = 3 schreibt; PRAGMA table_info(…) nicht.
		return strings.Contains(q, "=")
	}
	// Mit vorangestelltem CTE: schreibend nur, wenn ein INSERT, UPDATE
	// oder DELETE darin steht.
	o := strings.ToUpper(q)
	if strings.HasPrefix(o, "WITH") {
		return strings.Contains(o, "INSERT") || strings.Contains(o, "UPDATE") || strings.Contains(o, "DELETE")
	}
	return true
}

// ---------------------------------------------------------------- Sperre

// sperrStatistik: wie lange Transaktionen die Schreibsperre hielten.
var sperrStatistik struct {
	mu      sync.Mutex
	anzahl  int64
	laenger int64 // länger als langeSperre
	max     time.Duration
	summe   time.Duration
}

// langeSperre: Ab hier gilt eine Transaktion als lang — so lange müsste
// eine Anfrage der Bedienung, die schreiben will, im schlimmsten Fall
// warten.
const langeSperre = 100 * time.Millisecond

// SperrStatistik: Anzahl Schreib-Transaktionen seit dem Start, wie viele
// davon länger als 100 ms die Sperre hielten, und die längste.
type SperrStatistik struct {
	Anzahl  int64         `json:"anzahl"`
	Laenger int64         `json:"laenger"`
	Max     time.Duration `json:"max"`
	Summe   time.Duration `json:"summe"`
}

// Schreibsperren gibt die Statistik seit dem Start (oder seit
// SchreibsperrenZuruecksetzen) zurück.
func Schreibsperren() SperrStatistik {
	s := &sperrStatistik
	s.mu.Lock()
	defer s.mu.Unlock()
	return SperrStatistik{Anzahl: s.anzahl, Laenger: s.laenger, Max: s.max, Summe: s.summe}
}

// SchreibsperrenZuruecksetzen: für Messungen und Tests.
func SchreibsperrenZuruecksetzen() {
	s := &sperrStatistik
	s.mu.Lock()
	s.anzahl, s.laenger, s.max, s.summe = 0, 0, 0, 0
	s.mu.Unlock()
}

// LangeSperreMelden wird (wenn gesetzt) für jede Transaktion gerufen, die
// die Sperre länger als langeSperre hielt — mit den Funktionen von Lumen,
// aus denen sie kam. Für die Fehlersuche (LUMEN_DB_MESSEN=1).
var LangeSperreMelden func(d time.Duration, wo string)

// aufrufer: die ersten Funktionen von Lumen ausserhalb dieser Datei.
func aufrufer() string {
	pcs := make([]uintptr, 32)
	n := runtime.Callers(3, pcs)
	frames := runtime.CallersFrames(pcs[:n])
	var wo []string
	for {
		f, mehr := frames.Next()
		if strings.HasPrefix(f.Function, "lumen/") && !strings.HasSuffix(f.File, "/messung.go") {
			wo = append(wo, strings.TrimPrefix(f.Function, "lumen/internal/"))
			if len(wo) == 4 {
				break
			}
		}
		if !mehr {
			break
		}
	}
	return strings.Join(wo, " < ")
}

func sperreGehalten(d time.Duration) {
	s := &sperrStatistik
	s.mu.Lock()
	s.anzahl++
	s.summe += d
	if d > s.max {
		s.max = d
	}
	lang := d > langeSperre
	if lang {
		s.laenger++
	}
	s.mu.Unlock()
	if lang && LangeSperreMelden != nil {
		LangeSperreMelden(d, aufrufer())
	}
}

// ---------------------------------------------------------------- Treiber

type messTreiber struct{ innen driver.Driver }

func (t *messTreiber) Open(name string) (driver.Conn, error) {
	c, err := t.innen.Open(name)
	if err != nil {
		return nil, err
	}
	return &messConn{innen: c}, nil
}

// messConn ummantelt eine SQLite-Verbindung. Jede Verbindung gehört zu
// einem Zeitpunkt genau einer Goroutine (database/sql), darum brauchen
// txBeginn und standTx keine Sperre.
type messConn struct {
	innen    driver.Conn
	txBeginn time.Time // gesetzt, solange eine Transaktion offen ist
	standTx  bool      // in der offenen Transaktion wurde etwas geschrieben, das für Stand zählt
	artTx    standArt  // … und welche Stände es weiterrückt
}

func (c *messConn) Prepare(q string) (driver.Stmt, error) { return c.innen.Prepare(q) }
func (c *messConn) Close() error                          { return c.innen.Close() }
func (c *messConn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}

func (c *messConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	b, ok := c.innen.(driver.ConnBeginTx)
	if !ok {
		return nil, driver.ErrSkip
	}
	start := time.Now()
	tx, err := b.BeginTx(ctx, opts)
	d := time.Since(start)
	m := messungVon(ctx)
	m.zaehlen(d)
	// BEGIN IMMEDIATE (_txlock=immediate) wartet hier auf die Sperre.
	m.gewartet(d)
	if err != nil {
		return nil, err
	}
	c.txBeginn, c.standTx, c.artTx = time.Now(), false, standArt{}
	return &messTx{innen: tx, conn: c, m: m}, nil
}

func (c *messConn) ExecContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	e, ok := c.innen.(driver.ExecerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	start := time.Now()
	res, err := e.ExecContext(ctx, q, args)
	d := time.Since(start)
	m := messungVon(ctx)
	m.zaehlen(d)
	if err == nil && zaehltFuerStand(ctx, q) {
		a := standArtVon(ctx, q)
		if c.txBeginn.IsZero() {
			standWeiter(a)
		} else {
			c.standTx = true
			c.artTx.dazu(a)
		}
	}
	// Ein Schreiben ausserhalb einer Transaktion hält die Sperre für die
	// Dauer der Anweisung — und wartet vorher auf sie (busy_timeout).
	if c.txBeginn.IsZero() && schreibend(q) {
		m.gewartet(d)
		sperreGehalten(d)
	}
	return res, err
}

func (c *messConn) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	qc, ok := c.innen.(driver.QueryerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	start := time.Now()
	rows, err := qc.QueryContext(ctx, q, args)
	m := messungVon(ctx)
	m.zaehlen(time.Since(start))
	if err != nil {
		return nil, err
	}
	if m == nil {
		return rows, nil
	}
	return &messRows{innen: rows, m: m}, nil
}

func (c *messConn) Ping(ctx context.Context) error {
	if p, ok := c.innen.(driver.Pinger); ok {
		return p.Ping(ctx)
	}
	return nil
}

type messTx struct {
	innen driver.Tx
	conn  *messConn
	m     *Messung
}

func (t *messTx) ende() {
	if !t.conn.txBeginn.IsZero() {
		sperreGehalten(time.Since(t.conn.txBeginn))
	}
	if t.conn.standTx {
		standWeiter(t.conn.artTx)
	}
	t.conn.txBeginn, t.conn.standTx, t.conn.artTx = time.Time{}, false, standArt{}
}

func (t *messTx) Commit() error {
	start := time.Now()
	err := t.innen.Commit()
	t.m.zaehlen(time.Since(start))
	t.ende()
	return err
}

func (t *messTx) Rollback() error {
	err := t.innen.Rollback()
	t.conn.standTx, t.conn.artTx = false, standArt{}
	t.ende()
	return err
}

// messRows zählt die Zeit beim Weiterlesen (SQLite rechnet beim Schritt).
type messRows struct {
	innen driver.Rows
	m     *Messung
}

func (r *messRows) Columns() []string { return r.innen.Columns() }
func (r *messRows) Close() error      { return r.innen.Close() }
func (r *messRows) Next(dest []driver.Value) error {
	start := time.Now()
	err := r.innen.Next(dest)
	r.m.db.Add(int64(time.Since(start)))
	return err
}
