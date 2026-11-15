// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package main

// Einzelne Funktionen der Web-App mit Node prüfen, ohne Browser: Die
// Funktionen werden aus app.js herausgelesen und mit kleinen Stellvertretern
// (S, tr, sprache …) ausgeführt. Ohne Node wird übersprungen.

import (
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

// jsFunktion liest eine Funktion („function name(“ bis zur passenden
// Klammer) oder eine einzeilige Konstante („const name =“) aus dem Quelltext.
func jsFunktion(t *testing.T, quelle, name string) string {
	t.Helper()
	if i := strings.Index(quelle, "\nfunction "+name+"("); i >= 0 {
		rest := quelle[i+1:]
		start := strings.Index(rest, "{")
		tiefe := 0
		for j := start; j < len(rest); j++ {
			switch rest[j] {
			case '{':
				tiefe++
			case '}':
				tiefe--
				if tiefe == 0 {
					return rest[:j+1]
				}
			}
		}
	}
	if i := strings.Index(quelle, "\nconst "+name+" ="); i >= 0 {
		rest := quelle[i+1:]
		if e := strings.Index(rest, "\n"); e >= 0 {
			return rest[:e]
		}
	}
	t.Fatalf("Funktion %s nicht gefunden", name)
	return ""
}

// jsAusfuehren führt vorspann + die genannten Funktionen aus app.js +
// skript in Node aus und gibt die Ausgabe zurück.
func jsAusfuehren(t *testing.T, vorspann string, namen []string, skript string) string {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("ohne node")
	}
	roh, err := os.ReadFile("web/app.js")
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	b.WriteString(vorspann + "\n")
	for _, n := range namen {
		b.WriteString(jsFunktion(t, string(roh), n) + "\n")
	}
	b.WriteString(skript)
	cmd := exec.Command(node, "--input-type=module", "-e", b.String())
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}

const spurenVorspann = `
let S = { me: { prefs: {} } };
let SPRACHE = 'de-CH';
const sprache = () => SPRACHE;
const tr = (s, w) => s.replace(/\{(\w+)\}/g, (_, k) => w && w[k] != null ? w[k] : '');
`

var spurenFunktionen = []string{"serienSpurKey", "serienSpuren", "tonspuren", "textspuren", "bildspuren", "spurenVorbelegen"}

// Ohne gemerkte Wahl läuft der Ton in der Sprache des Profils, wenn es
// ihn gibt — sonst bleibt die Standardspur. „Originalton" im Profil nimmt
// die als Original markierte, sonst die erste Spur. Eine Handwahl geht vor.
func TestWebTonNachProfilsprache(t *testing.T) {
	out := jsAusfuehren(t, spurenVorspann, spurenFunktionen, `
const spuren = () => [
  { index: 1, kind: 'audio', language: 'en', title: 'Original' },
  { index: 2, kind: 'audio', language: 'de' },
  { index: 3, kind: 'subtitle', language: 'de' },
  { index: 4, kind: 'subtitle', language: 'en', forced: true },
];
const wahl = prefs => { S.me.prefs = prefs; const p = { spur: -1, untertitel: null, tracks: spuren() }; spurenVorbelegen(p); return p.spur + '/' + p.untertitel; };
console.log(wahl({}));
console.log(wahl({ tonWahl: 'original' }));
console.log(wahl({ tonSprache: 'en' }));
SPRACHE = 'fr-CH';
console.log(wahl({}));
`)
	soll := "2/null\n1/3\n1/3\n-1/4"
	if out != soll {
		t.Errorf("Spurwahl:\n%s\nerwartet:\n%s", out, soll)
	}
}

// Die Suche sagt auch Personen, Filmreihen und Sammlungen als Treffer an
// — nicht „Keine Treffer", wenn nur eine Person passt. Beim Seitenwechsel
// wird eine anstehende Ansage verworfen.
func TestWebTrefferAnsage(t *testing.T) {
	out := jsAusfuehren(t, `
let S = { query: 'hanks', busy: false, error: null, data: {} };
const tr = (s, w) => s.replace(/\{(\w+)\}/g, (_, k) => w && w[k] != null ? w[k] : '');
const gesagt = [];
const ansagen = text => gesagt.push(text);
let ansageZeit = null, letzteAnsage = '';
globalThis.document = { getElementById: () => null };
`, []string{"trefferAnsagen", "ansageLeeren"}, `
S.data = { items: [], folgen: [], personen: [{ name: 'Tom Hanks' }] };
trefferAnsagen();
await new Promise(r => setTimeout(r, 600));
S.data = { items: [], reihen: [{}], sammlungen: [{}, {}] };
S.query = 'reihe';
trefferAnsagen();
await new Promise(r => setTimeout(r, 600));
S.query = 'weg';
S.data = {};
trefferAnsagen();
ansageLeeren();
await new Promise(r => setTimeout(r, 600));
console.log(gesagt.join('|'));
`)
	if soll := "1 Treffer|3 Treffer"; out != soll {
		t.Errorf("Ansagen %q, erwartet %q", out, soll)
	}
}

// Fassungen im Player-Menü: alle Tonsprachen einer Datei, und zwei
// gleiche Kopien (gleiche Grösse) unterscheidet der Ordner.
func TestWebFassungNamen(t *testing.T) {
	out := jsAusfuehren(t, `
const tr = (s, w) => s.replace(/\{(\w+)\}/g, (_, k) => w && w[k] != null ? w[k] : '');
const sprachName = l => ({ de: 'Deutsch', en: 'Englisch' })[l] || l;
const aufloesungVon = () => ({ name: '1080p' });
const groesse = b => b + ' B';
`, []string{"fassungTeile", "fassungNamen"}, `
const ton = (...l) => l.map(x => ({ kind: 'audio', language: x }));
console.log(fassungNamen([
  { tracks: ton('de', 'en', 'de'), container: 'mkv', sizeBytes: 5, path: '/m/Film (2000)/Film.mkv' },
  { tracks: ton('de'), container: 'mkv', sizeBytes: 7, path: '/m/Kopie/Film.mkv' },
  { tracks: ton('de'), container: 'mkv', sizeBytes: 7, path: '/m/Sicherung/Film.mkv' },
  { tracks: ton('de'), container: 'mkv', sizeBytes: 7 },
]).join('|'));
`)
	soll := "1080p · Deutsch, Englisch · MKV|1080p · Deutsch · MKV · 7 B · Kopie|1080p · Deutsch · MKV · 7 B · Sicherung|1080p · Deutsch · MKV · 7 B · Kopie 4"
	if out != soll {
		t.Errorf("Fassungen:\n%s\nerwartet:\n%s", out, soll)
	}
}

// Rechte-Änderungen (s.bestaetigt) fragt die Web-App vorher nach dem
// Code, statt erst eine 428 zu kassieren (rot in der Konsole) — die Liste
// in api.js deckt jeden solchen Weg aus api.go.
func TestWebBestaetigungVorher(t *testing.T) {
	roh, err := os.ReadFile("../../internal/api/api.go")
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`mux\.Handle\("(\w+) /api/v1(\S+)", \w+\(s\.bestaetigt\(`)
	var wege []string
	for _, m := range re.FindAllStringSubmatch(string(roh), -1) {
		pfad := regexp.MustCompile(`\{[^}]+\}`).ReplaceAllString(m[2], "x_1")
		wege = append(wege, m[1]+" "+pfad)
	}
	if len(wege) < 5 {
		t.Fatalf("nur %d Wege gefunden", len(wege))
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("ohne node")
	}
	js, _ := os.ReadFile("web/api.js")
	m := regexp.MustCompile(`(?s)export const BESTAETIGEN = \[.*?\];`).Find(js)
	if m == nil {
		t.Fatal("BESTAETIGEN fehlt in api.js")
	}
	skript := strings.TrimPrefix(string(m), "export ") + `
const braucht = (method, path) => BESTAETIGEN.some(([m, re]) => m === method && re.test(path));
const wege = ` + "`" + strings.Join(wege, "\n") + "`" + `.split('\n');
const fehlt = wege.filter(w => { const [m, p] = w.split(' '); return !braucht(m, p); });
const zuviel = ['GET /admin/groups', 'PUT /admin/accounts/x_1/avatar'].filter(w => { const [m, p] = w.split(' '); return braucht(m, p); });
console.log(JSON.stringify({ fehlt, zuviel }));
`
	out, err := exec.Command(node, "--input-type=module", "-e", skript).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v %s", err, out)
	}
	if s := strings.TrimSpace(string(out)); s != `{"fehlt":[],"zuviel":[]}` {
		t.Errorf("api.js BESTAETIGEN passt nicht zu api.go: %s", s)
	}
}
