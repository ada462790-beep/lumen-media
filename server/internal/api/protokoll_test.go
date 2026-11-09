// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Das Protokoll liest auch ein Mitverwalter. Darum prüft dieser Test den
// Quelltext jeder Stelle, die hineinschreibt:
//   - Zeilen über Wiedergaben (Bereich „stream") sind feste Texte. Kein
//     Fehlertext, keine Zahl, kein Name — sie werden je Tag gezählt, und
//     was sich unterscheidet, wäre ein Merkmal einer Wiedergabe.
//   - Keine Zeile nennt einen Titel, eine Datei, ein Gerät oder den Namen
//     einer Person. „Entsperren fehlgeschlagen für Lea" um 22:13 sagt
//     schon, wer wann am Rechner sass.
func TestProtokollOhneTitelUndKonto(t *testing.T) {
	verboten := map[string]bool{"Title": true, "ItemID": true, "AccountID": true, "Path": true,
		"FileID": true, "DeviceName": true, "Avatar": true}
	kontoVariablen := map[string]bool{"a": true, "acc": true, "ich": true, "ziel": true, "konto": true, "account": true}

	var dateien []string
	for _, muster := range []string{"*.go", "../../cmd/lumen/*.go"} {
		d, _ := filepath.Glob(muster)
		dateien = append(dateien, d...)
	}
	geprueft := 0
	for _, datei := range dateien {
		if strings.HasSuffix(datei, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, datei, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) != 4 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Log" {
				return true
			}
			geprueft++
			wo := fset.Position(call.Pos()).String()
			bereich := ""
			if lit, ok := call.Args[2].(*ast.BasicLit); ok {
				bereich, _ = strconv.Unquote(lit.Value)
			}
			text := call.Args[3]
			if bereich == "stream" {
				if _, fest := text.(*ast.BasicLit); !fest {
					t.Errorf("%s: Zeile über Wiedergaben ist kein fester Text", wo)
				}
			}
			ast.Inspect(text, func(m ast.Node) bool {
				s, ok := m.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if verboten[s.Sel.Name] {
					t.Errorf("%s: Protokoll nennt %s", wo, s.Sel.Name)
				}
				if id, ok := s.X.(*ast.Ident); ok && s.Sel.Name == "Name" && kontoVariablen[id.Name] {
					t.Errorf("%s: Protokoll nennt den Namen eines Kontos", wo)
				}
				return true
			})
			return true
		})
	}
	if geprueft < 10 {
		t.Fatalf("nur %d Protokoll-Aufrufe gefunden — sucht der Test noch richtig?", geprueft)
	}
}
