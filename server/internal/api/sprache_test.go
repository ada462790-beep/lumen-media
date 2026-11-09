// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"lumen/internal/sprache"
	"lumen/internal/store"
	"lumen/internal/stream"
)

// meldungsTexte sucht im Quelltext alle Texte, die als Meldung an ein
// Gerät gehen können: fail(…), failW(…), failCode(…), tr(…),
// streamFehler(…), apiFehler{text: …} und errors.New(…) — Letztere, weil
// failErr den Text eines Fehlers aus dem Speicher weiterreicht.
func meldungsTexte(t *testing.T, ordner ...string) map[string]string {
	t.Helper()
	out := map[string]string{}
	fset := token.NewFileSet()
	// literal liest einen Text, auch wenn er aus Stücken zusammengesetzt ist.
	var literal func(e ast.Expr) (string, bool)
	literal = func(e ast.Expr) (string, bool) {
		switch x := e.(type) {
		case *ast.BasicLit:
			if x.Kind == token.STRING {
				s, err := strconv.Unquote(x.Value)
				return s, err == nil
			}
		case *ast.BinaryExpr:
			if x.Op == token.ADD {
				a, ok1 := literal(x.X)
				b, ok2 := literal(x.Y)
				return a + b, ok1 && ok2
			}
		case *ast.ParenExpr:
			return literal(x.X)
		}
		return "", false
	}
	// An welcher Stelle der Text steht, je Funktion.
	stelle := map[string]int{"fail": 2, "failW": 2, "failCode": 3, "tr": 1, "streamFehler": 3, "scheitern": 2, "trSprache": 1}
	for _, o := range ordner {
		dateien, _ := filepath.Glob(filepath.Join(o, "*.go"))
		for _, d := range dateien {
			if strings.HasSuffix(d, "_test.go") {
				continue
			}
			f, err := parser.ParseFile(fset, d, nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			merke := func(e ast.Expr) {
				if s, ok := literal(e); ok && s != "" {
					out[s] = fset.Position(e.Pos()).String()
				}
			}
			ast.Inspect(f, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.CallExpr:
					name := ""
					switch fn := x.Fun.(type) {
					case *ast.Ident:
						name = fn.Name
					case *ast.SelectorExpr:
						if p, ok := fn.X.(*ast.Ident); ok && p.Name == "errors" && fn.Sel.Name == "New" {
							name = "errors.New"
						}
					}
					if name == "errors.New" && len(x.Args) == 1 {
						merke(x.Args[0])
					}
					if i, ok := stelle[name]; ok && len(x.Args) > i {
						merke(x.Args[i])
					}
				case *ast.CompositeLit:
					if id, ok := x.Type.(*ast.Ident); ok && id.Name == "apiFehler" {
						for _, el := range x.Elts {
							if kv, ok := el.(*ast.KeyValueExpr); ok {
								if k, ok := kv.Key.(*ast.Ident); ok && k.Name == "text" {
									merke(kv.Value)
								}
							}
						}
					}
				case *ast.FuncDecl:
					// Diese Funktionen geben fertige Meldungen zurück.
					switch x.Name.Name {
					case "uebernahmeFehler", "entsperrenFalsch":
						ast.Inspect(x.Body, func(m ast.Node) bool {
							if bl, ok := m.(*ast.BasicLit); ok && bl.Kind == token.STRING {
								merke(bl)
							}
							return true
						})
					}
				case *ast.ValueSpec:
					for i, nm := range x.Names {
						if strings.HasSuffix(nm.Name, "Text") && i < len(x.Values) {
							merke(x.Values[i])
						}
					}
				}
				return true
			})
		}
	}
	// Was nur im Protokoll landet oder Kennungen sind, braucht keine
	// Übersetzung.
	for k := range out {
		if !strings.Contains(k, " ") && strings.ToLower(k) == k {
			delete(out, k)
		}
	}
	return out
}

// TestKatalogVollstaendig: Jede Meldung, die ein Gerät zu sehen bekommen
// kann, steht im englischen Katalog. Fehlt eine, sähe ein Profil auf
// Englisch plötzlich einen deutschen Satz. LUMEN_KATALOG_FEHLEND=1 gibt
// die fehlenden als JSON aus, als Vorlage zum Übersetzen.
func TestKatalogVollstaendig(t *testing.T) {
	if err := sprache.Fehler(); err != nil {
		t.Fatal(err)
	}
	texte := meldungsTexte(t, ".", "../store")
	var fehlend []string
	for k := range texte {
		if !sprache.Bekannt("en-US", k) {
			fehlend = append(fehlend, k)
		}
	}
	sort.Strings(fehlend)
	if os.Getenv("LUMEN_KATALOG_FEHLEND") == "1" {
		m := map[string]string{}
		for _, k := range fehlend {
			m[k] = ""
		}
		b, _ := json.MarshalIndent(m, "", "  ")
		t.Log("\n" + string(b))
	}
	for _, k := range fehlend {
		t.Errorf("fehlt in en-US: %q (%s)", k, texte[k])
	}
}

// TestKatalogOhneWaisen: Was im Katalog steht, wird auch benutzt — sonst
// pflegt jemand Übersetzungen für Texte, die es nicht mehr gibt.
func TestKatalogOhneWaisen(t *testing.T) {
	texte := meldungsTexte(t, ".", "../store")
	// Texte, die nicht über die Aufrufe oben laufen (siehe sprache.go):
	// Neuigkeiten des Servers und Antworten auf ausdrückliche Felder.
	weitere := zusatzTexte()
	for _, id := range []string{"en-US", "en-GB", "de-DE", "de-AT"} {
		for _, k := range sprache.Schluessel(id) {
			if _, ok := texte[k]; !ok && !weitere[k] {
				t.Errorf("%s: %q wird nirgends benutzt", id, k)
			}
		}
	}
}

// TestSpracheAntwort: Fehler kommen in der Sprache des Profils, vor der
// Anmeldung in der des Geräts, und tragen einen Code.
func TestSpracheAntwort(t *testing.T) {
	h := newHarness(t)
	fehler := func(resp *http.Response, raw []byte) (string, string) {
		var b struct{ Error, Code string }
		_ = json.Unmarshal(raw, &b)
		return b.Error, b.Code
	}

	// Vor der Anmeldung: Accept-Language.
	req, _ := http.NewRequest("GET", h.srv.URL+"/api/v1/me", nil)
	req.Header.Set("Accept-Language", "en-GB,en;q=0.8")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw := make([]byte, 4096)
	n, _ := resp.Body.Read(raw)
	resp.Body.Close()
	if e, c := fehler(resp, raw[:n]); resp.StatusCode != 401 || e != "Not signed in." || c != "nicht_angemeldet" {
		t.Fatalf("vor der Anmeldung: %d %q %q", resp.StatusCode, e, c)
	}
	if cl := resp.Header.Get("Content-Language"); cl != "en-GB" {
		t.Fatalf("Content-Language %q", cl)
	}

	// Ohne Kopf: die Sprache des Servers, und die ist de-CH.
	resp, raw2 := h.do("GET", "/api/v1/hello", "", "")
	var hallo struct {
		Sprache  string
		Sprachen []struct{ ID, Name string }
	}
	_ = json.Unmarshal(raw2, &hallo)
	if hallo.Sprache != "de-CH" || len(hallo.Sprachen) != 5 || hallo.Sprachen[3].Name != "English (United States)" {
		t.Fatalf("hello: %s", raw2)
	}
	if cl := resp.Header.Get("Content-Language"); cl != "de-CH" {
		t.Fatalf("hello Content-Language %q", cl)
	}

	// Eine unbekannte Sprache nimmt das Profil nicht an.
	resp, raw2 = h.do("PATCH", "/api/v1/me", `{"prefs":{"sprache":"fr-FR"}}`, h.tok)
	if _, c := fehler(resp, raw2); resp.StatusCode != 400 || c != "unbekannte_sprache" {
		t.Fatalf("fr-FR: %d %s", resp.StatusCode, raw2)
	}

	// Das Profil wählt Englisch: /me sagt es, Fehler kommen englisch.
	resp, raw2 = h.do("PATCH", "/api/v1/me", `{"prefs":{"sprache":"en-US"}}`, h.tok)
	var ich struct {
		Sprache string
		Prefs   map[string]string
	}
	_ = json.Unmarshal(raw2, &ich)
	if resp.StatusCode != 200 || ich.Sprache != "en-US" || ich.Prefs["sprache"] != "en-US" {
		t.Fatalf("PATCH /me: %d %s", resp.StatusCode, raw2)
	}
	resp, raw2 = h.do("GET", "/api/v1/items/gibtsnicht", "", h.tok)
	if e, c := fehler(resp, raw2); e != "Not found." || c != "nicht_gefunden" {
		t.Fatalf("englischer Fehler: %d %s", resp.StatusCode, raw2)
	}

	// Die Sprache des Servers ändert der Besitzer. Ein Profil ohne eigene
	// Wahl folgt ihr; eines mit eigener bleibt.
	resp, raw2 = h.do("PUT", "/api/v1/admin/sprache", `{"sprache":"xx"}`, h.tok)
	if resp.StatusCode != 400 {
		t.Fatalf("admin/sprache xx: %d %s", resp.StatusCode, raw2)
	}
	resp, raw2 = h.do("PUT", "/api/v1/admin/sprache", `{"sprache":"de-AT"}`, h.tok)
	if resp.StatusCode != 200 {
		t.Fatalf("admin/sprache: %d %s", resp.StatusCode, raw2)
	}
	_, raw2 = h.do("GET", "/api/v1/hello", "", "")
	_ = json.Unmarshal(raw2, &hallo)
	if hallo.Sprache != "de-AT" {
		t.Fatalf("hello nach Ändern: %s", raw2)
	}
	_, raw2 = h.do("GET", "/api/v1/me", "", h.tok)
	_ = json.Unmarshal(raw2, &ich)
	if ich.Sprache != "en-US" {
		t.Fatalf("eigene Wahl überschrieben: %s", raw2)
	}
	_, raw2 = h.do("PATCH", "/api/v1/me", `{"prefs":{"sprache":""}}`, h.tok)
	_ = json.Unmarshal(raw2, &ich)
	if ich.Sprache != "de-AT" {
		t.Fatalf("leer = Server: %s", raw2)
	}
	if v := h.st.Setting(t.Context(), "sprache", ""); v != "de-AT" {
		t.Fatalf("Einstellung %q", v)
	}
}

// TestEinrichtungMitSprache: Die Wahl bei der Einrichtung gilt für den
// Server und belegt die Metadaten vor.
func TestEinrichtungMitSprache(t *testing.T) {
	for _, fall := range []struct{ wahl, metaSpr, metaReg string }{
		{"en-GB", "en-US", "GB"},
		{"de-AT", "de-DE", "AT"},
	} {
		st, err := store.Open(filepath.Join(t.TempDir(), "l.db"))
		if err != nil {
			t.Fatal(err)
		}
		api := New(st, nil, nil, nil, stream.NewLive(), nil, t.TempDir(),
			slog.New(slog.NewTextHandler(io.Discard, nil)), "test")
		srv := httptest.NewServer(api.Handler())
		resp, err := http.Post(srv.URL+"/api/v1/setup", "application/json", strings.NewReader(
			`{"name":"Alex","password":"einsehrgutespasswort","avatar":"av-a","sprache":"`+fall.wahl+`"}`))
		if err != nil || resp.StatusCode != 200 {
			t.Fatalf("%s: %v %v", fall.wahl, err, resp)
		}
		resp.Body.Close()
		srv.Close()
		ctx := t.Context()
		if v := st.Setting(ctx, "sprache", ""); v != fall.wahl {
			t.Errorf("%s: sprache %q", fall.wahl, v)
		}
		if v := st.Setting(ctx, keyLanguage, ""); v != fall.metaSpr {
			t.Errorf("%s: meta.language %q", fall.wahl, v)
		}
		if v := st.Setting(ctx, keyRegion, ""); v != fall.metaReg {
			t.Errorf("%s: meta.region %q", fall.wahl, v)
		}
		_ = st.Close()
	}
}
