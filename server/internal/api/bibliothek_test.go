// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Was gemeldet wurde: eine Bibliothek liess sich mit einem Pfad anlegen,
// den es gar nicht gibt — auch mit einem leeren. Hinterher sah das aus
// wie eine Bibliothek, die stumm für immer leer bleibt, und niemand
// konnte sehen, woran es lag.
func TestBibliothekMitUntauglichemPfad(t *testing.T) {
	h := newHarness(t)
	datei := filepath.Join(t.TempDir(), "keinordner.txt")
	if err := os.WriteFile(datei, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	faelle := []struct {
		name, pfad, erwartet string
	}{
		{"leer", "", "ohne ordner"},
		{"nur Leerzeichen", "   ", "ohne ordner"},
		{"gibt es nicht", "/gibt/es/wirklich/nicht", "gibt es nicht"},
		{"nicht von ganz oben", "medien/filme", "vollständiger pfad"},
		{"eine Datei", datei, "kein ordner"},
	}
	for _, f := range faelle {
		t.Run(f.name, func(t *testing.T) {
			body, _ := json.Marshal(map[string]string{
				"name": "Filme", "kind": "movie", "path": f.pfad})
			resp, raw := h.do("POST", "/api/v1/admin/libraries", string(body), h.tok)
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("%s: angenommen statt abgelehnt (%d): %s", f.name, resp.StatusCode, raw)
			}
			if !strings.Contains(strings.ToLower(string(raw)), f.erwartet) {
				t.Fatalf("%s: die Begründung nennt nicht %q: %s", f.name, f.erwartet, raw)
			}
		})
	}

	// Eine unbekannte Art wird abgelehnt, statt still als Serien zu gelten.
	body, _ := json.Marshal(map[string]string{
		"name": "Fotos", "kind": "photos", "path": t.TempDir()})
	if resp, raw := h.do("POST", "/api/v1/admin/libraries", string(body), h.tok); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("art „photos“ angenommen (%d): %s", resp.StatusCode, raw)
	}

	// Und der taugliche Pfad geht weiterhin durch.
	body, _ = json.Marshal(map[string]string{
		"name": "Filme", "kind": "movie", "path": t.TempDir()})
	resp, raw := h.do("POST", "/api/v1/admin/libraries", string(body), h.tok)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("ein richtiger Pfad wurde abgelehnt (%d): %s", resp.StatusCode, raw)
	}
}

// Gemeldet: „Bibliotheken kann man nicht löschen auf dem Server."
func TestBibliothekLoeschen(t *testing.T) {
	h := newHarness(t)
	body, _ := json.Marshal(map[string]string{
		"name": "Filme", "kind": "movie", "path": t.TempDir()})
	_, raw := h.do("POST", "/api/v1/admin/libraries", string(body), h.tok)
	var lib struct{ ID string }
	if err := json.Unmarshal(raw, &lib); err != nil || lib.ID == "" {
		t.Fatalf("anlegen fehlgeschlagen: %s", raw)
	}

	resp, raw := h.do("DELETE", "/api/v1/admin/libraries/gibtsnicht", "", h.tok)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unbekannte Bibliothek: %d %s", resp.StatusCode, raw)
	}

	resp, raw = h.do("DELETE", "/api/v1/admin/libraries/"+lib.ID, "", h.tok)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("löschen fehlgeschlagen: %d %s", resp.StatusCode, raw)
	}

	_, raw = h.do("GET", "/api/v1/admin/libraries", "", h.tok)
	if strings.Contains(string(raw), lib.ID) {
		t.Fatalf("die Bibliothek steht noch in der Liste: %s", raw)
	}
}

// Mit der Bibliothek gehen auch ihre Titel. Bliebe etwas zurück, stünden
// im Haus Filme herum, die niemand mehr löschen kann — und die Dateien
// dazu gibt es womöglich gar nicht mehr.
func TestLoeschenNimmtDieTitelMit(t *testing.T) {
	h := newHarness(t)
	itemID, _ := h.film("Ein Film", 1)

	_, raw := h.do("GET", "/api/v1/items?limit=50", "", h.tok)
	if !strings.Contains(string(raw), itemID) {
		t.Fatalf("der Titel war nicht da: %s", raw)
	}

	resp, raw := h.do("DELETE", "/api/v1/admin/libraries/"+h.lib.ID, "", h.tok)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("löschen: %d %s", resp.StatusCode, raw)
	}

	_, raw = h.do("GET", "/api/v1/items?limit=50", "", h.tok)
	if strings.Contains(string(raw), itemID) {
		t.Fatalf("der Titel lebt weiter: %s", raw)
	}
}

// Gemeldet: „du sollst nicht nur files lesen sondern auch ordner."
// Der Wähler muss zeigen, was wirklich da ist — und einen Serienordner
// nicht als leer ausgeben, nur weil die Folgen zwei Ebenen tiefer liegen.
func TestOrdnerWaehler(t *testing.T) {
	h := newHarness(t)
	wurzel := t.TempDir()
	anlegen := func(teile ...string) {
		p := filepath.Join(append([]string{wurzel}, teile...)...)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	anlegen("Filme", "Die Bucht (2019)", "Die Bucht (2019).mkv")
	anlegen("Serien", "Nordlicht", "Staffel 01", "Nordlicht S01E01.mkv")
	anlegen("Serien", "Nordlicht", "Staffel 01", "Nordlicht S01E02.mkv")
	if err := os.MkdirAll(filepath.Join(wurzel, "Papiere"), 0o755); err != nil {
		t.Fatal(err)
	}
	anlegen("Papiere", "steuern.pdf")

	var aus struct {
		Pfad   string `json:"pfad"`
		Oben   string `json:"oben"`
		Hier   int    `json:"hier"`
		Ordner []struct {
			Name  string `json:"name"`
			Pfad  string `json:"pfad"`
			Leer  bool   `json:"leer"`
			Filme int    `json:"filme"`
		} `json:"ordner"`
	}
	resp, raw := h.do("GET", "/api/v1/admin/browse?pfad="+wurzel, "", h.tok)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("durchsuchen fehlgeschlagen: %d %s", resp.StatusCode, raw)
	}
	if err := json.Unmarshal(raw, &aus); err != nil {
		t.Fatal(err)
	}
	gefunden := map[string]int{}
	for _, o := range aus.Ordner {
		gefunden[o.Name] = o.Filme
	}
	if gefunden["Filme"] != 1 {
		t.Errorf("Filme: %d statt 1", gefunden["Filme"])
	}
	// Der eigentliche Fehler: zwei Ebenen tiefer wurde nicht mehr gezählt.
	if gefunden["Serien"] != 2 {
		t.Errorf("Serien: %d statt 2 — Folgen unter Staffelordnern werden nicht gesehen", gefunden["Serien"])
	}
	if _, da := gefunden["Papiere"]; !da || gefunden["Papiere"] != 0 {
		t.Errorf("Papiere sollte dastehen und null Videos haben: %v", gefunden)
	}
	if aus.Hier != 3 {
		t.Errorf("hier: %d statt 3", aus.Hier)
	}
	if aus.Oben == "" {
		t.Error("ohne „oben\" kommt man nicht mehr zurück")
	}

	// Ohne Pfad: Startpunkte, damit man überhaupt anfangen kann.
	_, raw = h.do("GET", "/api/v1/admin/browse", "", h.tok)
	aus.Ordner = nil
	if err := json.Unmarshal(raw, &aus); err != nil {
		t.Fatal(err)
	}
	if len(aus.Ordner) == 0 {
		t.Fatal("keine Startpunkte")
	}

	// Ein Pfad, der sich nicht öffnen lässt, gibt eine Begründung.
	resp, _ = h.do("GET", "/api/v1/admin/browse?pfad=/gibt/es/nicht", "", h.tok)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("unöffenbarer Pfad: %d", resp.StatusCode)
	}
}

// Gemeldet: „als admin sieht man die Wünsche nicht separat."
func TestWuenscheFuerDenBesitzer(t *testing.T) {
	h := newHarness(t)
	_, raw := h.do("POST", "/api/v1/wishes", `{"title":"Dune Teil Drei","year":2027}`, h.tok)
	if !strings.Contains(string(raw), "Dune") {
		t.Fatalf("wünschen fehlgeschlagen: %s", raw)
	}
	var aus struct {
		Offen  int `json:"offen"`
		Wishes []struct {
			ID    string `json:"id"`
			Title string `json:"title"`
			State string `json:"state"`
		} `json:"wishes"`
	}
	resp, raw := h.do("GET", "/api/v1/admin/wishes", "", h.tok)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("%d %s", resp.StatusCode, raw)
	}
	if err := json.Unmarshal(raw, &aus); err != nil {
		t.Fatal(err)
	}
	if aus.Offen != 1 || len(aus.Wishes) != 1 {
		t.Fatalf("offen=%d, Wünsche=%d: %s", aus.Offen, len(aus.Wishes), raw)
	}

	// Angenommen heisst nicht mehr offen.
	h.do("PUT", "/api/v1/admin/wishes/"+aus.Wishes[0].ID, `{"state":"working"}`, h.tok)
	_, raw = h.do("GET", "/api/v1/admin/wishes", "", h.tok)
	if err := json.Unmarshal(raw, &aus); err != nil {
		t.Fatal(err)
	}
	if aus.Offen != 0 {
		t.Fatalf("nach dem Annehmen immer noch offen: %s", raw)
	}
}

// Eine Platte, die nach einem Neustart nicht eingehängt wurde, darf
// nicht dazu führen, dass die halbe Sammlung als verschwunden gilt.
// Darum wird gar nicht erst geprüft, wenn der Ordner fehlt.
func TestPruefenOhneOrdnerWirdAbgelehnt(t *testing.T) {
	h := newHarness(t)
	weg := filepath.Join(t.TempDir(), "platte")
	if err := os.MkdirAll(weg, 0o755); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]string{"name": "Platte", "kind": "movie", "path": weg})
	_, raw := h.do("POST", "/api/v1/admin/libraries", string(body), h.tok)
	var lib struct{ ID string }
	if err := json.Unmarshal(raw, &lib); err != nil || lib.ID == "" {
		t.Fatalf("anlegen: %s", raw)
	}

	// Die Platte verschwindet.
	if err := os.RemoveAll(weg); err != nil {
		t.Fatal(err)
	}

	resp, raw := h.do("POST", "/api/v1/admin/libraries/"+lib.ID+"/scan", "", h.tok)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("geprüft, obwohl der Ordner fehlt: %d %s", resp.StatusCode, raw)
	}

	// Und die Liste sagt, was los ist.
	_, raw = h.do("GET", "/api/v1/admin/libraries", "", h.tok)
	if !strings.Contains(string(raw), `"erreichbar":false`) {
		t.Fatalf("die Bibliothek gilt noch als erreichbar: %s", raw)
	}
}

// Darf Lumen in einen Ordner nicht hinein, sagt der Wähler das — statt
// „keine Videos", worauf niemand die Rechte als Grund vermutet.
func TestOrdnerOhneZugriff(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("als root gibt es keine verschlossenen Ordner")
	}
	h := newHarness(t)
	wurzel := t.TempDir()
	zu := filepath.Join(wurzel, "_filme")
	if err := os.MkdirAll(zu, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(zu, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(zu, 0o755) })

	_, raw := h.do("GET", "/api/v1/admin/browse?pfad="+wurzel, "", h.tok)
	if !strings.Contains(string(raw), `"gesperrt":true`) {
		t.Fatalf("der verschlossene Ordner ist nicht als gesperrt markiert: %s", raw)
	}
	resp, raw := h.do("GET", "/api/v1/admin/browse?pfad="+zu, "", h.tok)
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(raw), "nicht lesen") {
		t.Fatalf("keine verständliche Begründung: %d %s", resp.StatusCode, raw)
	}
	body, _ := json.Marshal(map[string]string{"name": "Filme", "kind": "movie", "path": zu})
	_, raw = h.do("POST", "/api/v1/admin/libraries", string(body), h.tok)
	if !strings.Contains(string(raw), "nicht lesen") {
		t.Fatalf("beim Anlegen keine verständliche Begründung: %s", raw)
	}
}

// Der Hausmeister: eine Bibliothek mit Intervall wird fällig, sobald der
// letzte Durchlauf länger her ist — und was er ergab, steht an ihr.
func TestBibliothekHaeltSichSelbstAktuell(t *testing.T) {
	h := newHarness(t)
	wurzel := t.TempDir()
	body, _ := json.Marshal(map[string]string{"name": "Filme", "kind": "movie", "path": wurzel})
	_, raw := h.do("POST", "/api/v1/admin/libraries", string(body), h.tok)
	var lib struct{ ID string }
	_ = json.Unmarshal(raw, &lib)

	// Vorgabe: täglich um 4 Uhr. Noch nie geprüft → sofort fällig.
	f, _ := h.st.Faellig(t.Context())
	if len(f) != 1 || f[0].ID != lib.ID {
		t.Fatalf("neue Bibliothek nicht fällig: %+v", f)
	}

	// Ein Durchlauf — danach steht das Ergebnis an der Bibliothek, und
	// fällig ist sie erst am nächsten Morgen wieder.
	if err := os.WriteFile(filepath.Join(wurzel, "Die Bucht (2019).mkv"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	libs, _ := h.st.Libraries(t.Context())
	h.svr().Durchlauf(t.Context(), libs[0], true)
	libs, _ = h.st.Libraries(t.Context())
	if libs[0].LastResult == nil || libs[0].LastResult.Added != 1 || !libs[0].LastResult.Auto {
		t.Fatalf("kein Ergebnis am Durchlauf: %+v", libs[0].LastResult)
	}
	if f, _ := h.st.Faellig(t.Context()); len(f) != 0 {
		t.Fatalf("gerade geprüft, trotzdem fällig")
	}

	// Intervall abstellen: nie mehr fällig. Auf 15 Minuten: fällig, sobald
	// der letzte Durchlauf 15 Minuten her ist.
	resp, _ := h.do("POST", "/api/v1/admin/libraries/"+lib.ID, `{"intervalMin":0}`, h.tok)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("Intervall setzen: %d", resp.StatusCode)
	}
	h.do("POST", "/api/v1/admin/libraries/"+lib.ID, `{"intervalMin":15}`, h.tok)
	_, _ = h.st.DB().Exec(`UPDATE library SET scanned_at = ? WHERE id = ?`,
		time.Now().Add(-20*time.Minute).Unix(), lib.ID)
	if f, _ := h.st.Faellig(t.Context()); len(f) != 1 {
		t.Fatalf("nach 20 Minuten bei Intervall 15 nicht fällig")
	}
	_, raw = h.do("GET", "/api/v1/admin/libraries", "", h.tok)
	if !strings.Contains(string(raw), `"intervalMin":15`) || !strings.Contains(string(raw), `"lastResult"`) {
		t.Fatalf("die Verwaltung sieht Intervall und Ergebnis nicht: %s", raw)
	}
}
