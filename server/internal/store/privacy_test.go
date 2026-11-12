// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

// Diese Tests bewachen eine Zusage an die Leute im Haushalt:
// der Besitzer des Servers sieht nicht, wer wann was wie lange schaut.
//
// Sie prüfen nicht die Oberfläche, sondern den Code. Wer die Zusage bricht,
// bricht einen Test — und zwar bevor es jemand merkt, der darauf vertraut.

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// privateTables sind die Tabellen, in denen steht, was jemand geschaut hat.
// Dazu gehören auch die Wiedergabelisten (was jemand sammelt), die
// Reihenfolge von „Weiterschauen", die eigenen Einstellungen und die
// Geräte eines Profils.
var privateTables = []string{"progress", "rating", "watchlist", "favorit",
	"playlist", "playlist_item", "progress_reihe", "prefs", "token",
	// Bildschirmzeit: wie lange ein Profil heute geschaut hat — kein Was,
	// aber trotzdem nur je Profil (bildschirmzeit.go).
	"bildschirmzeit_tag"}

// kontoGebunden: die Abfrage legt sich auf genau ein Konto fest. Ein
// blosses „account_id" im Text genügt nicht — „account_id IS NOT NULL"
// oder „account_id != ?" lesen über alle (anderen) Konten hinweg.
var (
	kontoGleich   = regexp.MustCompile(`(?i)\b(?:\w+\.)?(?:account_id|added_by)\s*(?:=\s*\?|IN\s*\()`)
	kontoUngleich = regexp.MustCompile(`(?i)\b(?:\w+\.)?account_id\s*(?:IS\s+NOT\s+NULL|!=|<>|NOT\s+IN)`)
	// eineListe: die Abfrage gilt genau einer Wiedergabeliste. Wer sie
	// lesen oder ändern darf, prüft listen.go vorher (eigene, geteilt,
	// gemeinsam) — die Liste ist dort die Grenze, nicht das Konto.
	eineListe = regexp.MustCompile(`(?i)\bplaylist_id\s*=\s*(?:\?|p\.id\b)`)
)

// erlaubtOhneKonto sind Abfragen, die bewusst über Konten hinweg gehen —
// jede mit Grund. Wer hier etwas einträgt, schreibt dazu, warum es
// niemandem verrät, wer was schaut.
var erlaubtOhneKonto = []struct{ datei, stueck, grund string }{
	{"accounts.go", "FROM token WHERE hash = ?", "Anmeldung: das Token selbst bestimmt das Konto"},
	{"accounts.go", "UPDATE token SET last_seen_at = ? WHERE hash = ?", "nur das eine Token, nur der Tag"},
	{"accounts.go", "DELETE FROM token WHERE hash = ?", "genau dieses Gerät abmelden"},
	{"accounts.go", "DELETE FROM token WHERE last_seen_at < ?", "Wartung: alte Anmeldungen löschen, liest nichts"},
	{"accounts.go", "UPDATE playlist_item SET added_by", "Konto löschen: Titel in fremden Listen bleiben dort"},
	{"store.go", "UPDATE token SET last_seen_at = (last_seen_at / 86400) * 86400", "Umbau: Uhrzeiten entfernen, liest nichts"},
	{"store.go", "UPDATE rating SET updated_at", "Umbau: Uhrzeiten entfernen, liest nichts"},
	{"playback.go", "WHERE r.item_id = ? AND r.shared = 1", "nur Bewertungen, die ihre Besitzer selbst freigegeben haben"},
	{"bildschirmzeit.go", "DELETE FROM bildschirmzeit_tag WHERE tag < ?", "Wartung: Zahlen vergangener Tage löschen, liest nichts"},
	{"zusammenfuehren.go", "UPDATE OR IGNORE playlist_item SET item_id = ? WHERE item_id = ?", "Wartung: Titel umhängen, liest nichts"},
	{"token_pruefwerte.go", "SELECT hash FROM token", "nur die Prüfwerte, um einen Bildschlüssel nachzurechnen; kein Konto, kein Gerät, keine Zeit"},
}

func erlaubterFall(datei, sql string) bool {
	flach := strings.Join(strings.Fields(sql), " ")
	for _, e := range erlaubtOhneKonto {
		if filepath.Base(datei) == e.datei && strings.Contains(flach, e.stueck) {
			return true
		}
	}
	return false
}

// TestPrivateQueriesAlwaysScopeToOneAccount liest den Quelltext dieses Pakets
// und besteht darauf, dass jede SQL-Abfrage auf eine private Tabelle das
// Konto nennt. Eine Abfrage ohne account_id würde über alle Profile hinweg
// lesen — genau das soll es nicht geben.
func TestPrivateQueriesAlwaysScopeToOneAccount(t *testing.T) {
	stmt := regexp.MustCompile(`(?is)\b(SELECT|UPDATE|DELETE)\b.*?(?:;|$)`)

	for _, file := range goFiles(t) {
		src := readFile(t, file)
		for _, raw := range stringLiterals(t, file) {
			if !mentionsPrivateTable(raw) {
				continue
			}
			if !stmt.MatchString(raw) {
				continue
			}
			// INSERT … VALUES schreibt immer mit account_id in der Spaltenliste.
			if strings.Contains(strings.ToUpper(raw), "INSERT") &&
				strings.Contains(raw, "account_id") {
				continue
			}
			if erlaubterFall(file, raw) {
				continue
			}
			if (!kontoGleich.MatchString(raw) && !eineListe.MatchString(raw)) || kontoUngleich.MatchString(raw) {
				t.Errorf("%s: Abfrage auf eine private Tabelle, die sich nicht auf ein Konto festlegt:\n%s",
					filepath.Base(file), trim(raw))
			}
		}
		_ = src
	}
}

// TestNoCrossAccountAggregation verbietet Abfragen, die private Tabellen
// zusammenzählen oder über Konten hinweg gruppieren. „Wer schaut am meisten"
// soll sich gar nicht erst beantworten lassen.
func TestNoCrossAccountAggregation(t *testing.T) {
	bad := regexp.MustCompile(`(?i)\b(count|sum|avg|max|min)\s*\(`)

	for _, file := range goFiles(t) {
		for _, raw := range stringLiterals(t, file) {
			if !mentionsPrivateTable(raw) {
				continue
			}
			up := strings.ToUpper(raw)
			// Zählen innerhalb eines Kontos oder einer einzelnen Liste
			// („wie viele Titel hat diese Liste") ist kein Vergleich
			// zwischen Personen.
			if bad.MatchString(raw) && !(kontoGleich.MatchString(raw) || eineListe.MatchString(raw)) {
				t.Errorf("%s: zusammenfassende Abfrage auf eine private Tabelle:\n%s",
					filepath.Base(file), trim(raw))
			}
			if strings.Contains(up, "GROUP BY") && !strings.Contains(raw, "account_id = ?") {
				t.Errorf("%s: GROUP BY über private Daten ohne festes Konto:\n%s",
					filepath.Base(file), trim(raw))
			}
			// Ein JOIN von progress auf account würde Namen an Fortschritt
			// knüpfen. Bewertungen dürfen das — aber nur die freigegebenen.
			if strings.Contains(raw, "progress") && strings.Contains(up, "JOIN ACCOUNT") {
				t.Errorf("%s: Fortschritt an Konten gehängt:\n%s",
					filepath.Base(file), trim(raw))
			}
		}
	}
}

// TestNoWatchHistoryTable besteht darauf, dass es keine Tabelle gibt, in der
// einzelne Wiedergaben mit Zeitpunkt landen. Fortschritt ja — Verlauf nein.
func TestNoWatchHistoryTable(t *testing.T) {
	schema := strings.ToLower(schemaSQL)
	for _, forbidden := range []string{
		"create table if not exists play_history",
		"create table if not exists watch_history",
		"create table if not exists playback_event",
		"create table if not exists view_log",
		"create table if not exists listen_history",
	} {
		if strings.Contains(schema, forbidden) {
			t.Errorf("das Schema legt einen Wiedergabe-Verlauf an: %q", forbidden)
		}
	}
}

// TestPrivateFunctionsTakeAccountFirst stellt sicher, dass jede ausgeführte
// Funktion in playback.go das Konto als ersten echten Parameter nimmt.
// Eine Funktion ohne accountID könnte nur alle meinen.
func TestPrivateFunctionsTakeAccountFirst(t *testing.T) {
	for _, datei := range []string{"playback.go", "neuigkeiten.go", "listen.go", "zusammenfuehren.go"} {
		privateFunktionenPruefen(t, datei)
	}
}

func privateFunktionenPruefen(t *testing.T, datei string) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filepath.Join(dir(t), datei), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Recv == nil || !fn.Name.IsExported() {
			continue
		}
		// Nur Methoden auf *Store führen Abfragen aus; Progress.Percent
		// rechnet bloss auf einer schon geladenen Zeile.
		if !isStoreMethod(fn) {
			continue
		}
		var names []string
		for _, p := range fn.Type.Params.List {
			for _, n := range p.Names {
				names = append(names, n.Name)
			}
		}
		// Wartung (Serien zusammenführen, Filme ohne Datei) gilt keinem
		// Konto — darf dann aber nichts herausgeben ausser einer Zahl.
		if len(names) == 1 && names[0] == "ctx" && nurZahlOderFehler(fn) {
			continue
		}
		// Erster Parameter ist immer der Kontext; danach muss das Konto
		// kommen (accountID, in listen.go „ich"). Neuigkeiten nimmt vorher
		// noch die Rechte-Abfrage.
		if len(names) < 2 || names[0] != "ctx" {
			t.Errorf("%s: erwartet (ctx, accountID, …), bekommen %v", fn.Name.Name, names)
			continue
		}
		konto := names[1]
		if fn.Name.Name == "Neuigkeiten" && len(names) > 2 {
			konto = names[2]
		}
		if konto != "accountID" && konto != "ich" && fn.Name.Name != "SharedRatings" {
			t.Errorf("%s: %s: Parameter heisst %q statt accountID — "+
				"private Abfragen gelten immer genau einem Profil",
				datei, fn.Name.Name, konto)
		}
	}
}

// ---------------------------------------------------------------- Hilfen

// nurZahlOderFehler: die Funktion gibt (int, error) oder error zurück.
func nurZahlOderFehler(fn *ast.FuncDecl) bool {
	if fn.Type.Results == nil {
		return true
	}
	for _, r := range fn.Type.Results.List {
		id, ok := r.Type.(*ast.Ident)
		if !ok || (id.Name != "int" && id.Name != "error") {
			return false
		}
	}
	return true
}

func isStoreMethod(fn *ast.FuncDecl) bool {
	for _, r := range fn.Recv.List {
		star, ok := r.Type.(*ast.StarExpr)
		if !ok {
			continue
		}
		if id, ok := star.X.(*ast.Ident); ok && id.Name == "Store" {
			return true
		}
	}
	return false
}

func dir(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return wd
}

func goFiles(t *testing.T) []string {
	t.Helper()
	all, err := filepath.Glob(filepath.Join(dir(t), "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	out := []string{}
	for _, f := range all {
		if !strings.HasSuffix(f, "_test.go") {
			out = append(out, f)
		}
	}
	return out
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// stringLiterals zieht alle Zeichenkettenliterale aus einer Datei — dort
// stehen die SQL-Abfragen.
func stringLiterals(t *testing.T, path string) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	out := []string{}
	ast.Inspect(f, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if ok && lit.Kind == token.STRING && len(lit.Value) > 2 {
			out = append(out, strings.Trim(lit.Value, "`\""))
		}
		return true
	})
	return out
}

func mentionsPrivateTable(sql string) bool {
	low := strings.ToLower(sql)
	for _, tbl := range privateTables {
		for _, kw := range []string{"from " + tbl, "into " + tbl, "update " + tbl,
			"join " + tbl, tbl + " r", tbl + " p"} {
			if strings.Contains(low, kw) {
				return true
			}
		}
	}
	return false
}

func trim(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 160 {
		s = s[:160] + " …"
	}
	return "  " + s
}

// ---------------------------------------------------------------- echter Lauf

// TestProgressStaysInItsProfile legt zwei Profile an, lässt beide etwas
// schauen und prüft, dass keines vom anderen etwas sieht.
func TestProgressStaysInItsProfile(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	owner, err := s.CreateAccount(ctx, NewAccount{Name: "Besitzer", IsOwner: true})
	if err != nil {
		t.Fatal(err)
	}
	guest, err := s.CreateAccount(ctx, NewAccount{Name: "Gast", LockKind: "pin", Secret: "4711"})
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, s, `INSERT INTO library (id,name,kind,path) VALUES ('lib','Filme','movie','/x')`)
	mustExec(t, s, `INSERT INTO item (id,library_id,kind,title,sort_title,added_at,updated_at)
	                VALUES ('it','lib','movie','Nordlicht','nordlicht',0,0)`)

	if err := s.SaveProgress(ctx, guest.ID, "it", 600_000, 6_000_000); err != nil {
		t.Fatal(err)
	}

	// Der Besitzer fragt denselben Titel ab und bekommt nichts.
	if _, err := s.Progress(ctx, owner.ID, "it"); err == nil {
		t.Fatal("der Besitzer sieht den Fortschritt eines anderen Profils")
	}
	got, err := s.Progress(ctx, guest.ID, "it")
	if err != nil {
		t.Fatal(err)
	}
	if got.PositionMS != 600_000 {
		t.Fatalf("eigener Fortschritt falsch: %d", got.PositionMS)
	}
	ids, err := s.ResumeIDs(ctx, owner.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 0 {
		t.Fatalf("„Weiterschauen“ des Besitzers enthält fremde Titel: %v", ids)
	}
}

// TestRatingsNeedConsentToBeShared: eine Bewertung wird erst sichtbar, wenn
// die Person sie freigibt.
func TestRatingsNeedConsentToBeShared(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	a, _ := s.CreateAccount(ctx, NewAccount{Name: "A", IsOwner: true})
	mustExec(t, s, `INSERT INTO library (id,name,kind,path) VALUES ('lib','Filme','movie','/x')`)
	mustExec(t, s, `INSERT INTO item (id,library_id,kind,title,sort_title,added_at,updated_at)
	                VALUES ('it','lib','movie','Kupfer','kupfer',0,0)`)

	if err := s.SetRating(ctx, a.ID, "it", 5, false); err != nil {
		t.Fatal(err)
	}
	shared, err := s.SharedRatings(ctx, "it")
	if err != nil {
		t.Fatal(err)
	}
	if len(shared) != 0 {
		t.Fatal("nicht freigegebene Bewertung taucht im Haushalt auf")
	}
	if err := s.SetRating(ctx, a.ID, "it", 5, true); err != nil {
		t.Fatal(err)
	}
	if shared, _ = s.SharedRatings(ctx, "it"); len(shared) != 1 {
		t.Fatal("freigegebene Bewertung fehlt")
	}
}

func testStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "lumen.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func mustExec(t *testing.T, s *Store, q string) {
	t.Helper()
	if _, err := s.DB().Exec(q); err != nil {
		t.Fatal(err)
	}
}

// Die Prüfung selbst: Abfragen, die nur so tun, als nennten sie ein Konto,
// fallen auf.
func TestKontoBindungErkennt(t *testing.T) {
	for _, q := range []string{
		`SELECT item_id FROM progress WHERE account_id IS NOT NULL`,
		`SELECT item_id FROM progress WHERE account_id != ?`,
		`SELECT item_id FROM progress p WHERE p.account_id <> ?`,
		`SELECT account_id FROM prefs`,
	} {
		if kontoGleich.MatchString(q) && !kontoUngleich.MatchString(q) {
			t.Errorf("nicht erkannt: %s", q)
		}
	}
	if !kontoGleich.MatchString(`SELECT * FROM progress p WHERE p.account_id = ? AND item_id = ?`) {
		t.Error("echte Bindung an ein Konto nicht erkannt")
	}
}
