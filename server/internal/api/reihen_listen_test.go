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
	"strings"
	"testing"

	"lumen/internal/store"
)

func (h *harness) gast(name string, kind bool, libs ...string) string {
	h.t.Helper()
	acc, err := h.st.CreateAccount(h.t.Context(), store.NewAccount{
		Name: name, LockKind: "pin", Secret: "4711", IsKid: kind})
	if err != nil {
		h.t.Fatal(err)
	}
	if err := h.st.SetGrants(h.t.Context(), acc.ID, libs); err != nil {
		h.t.Fatal(err)
	}
	tok, err := h.st.IssueToken(h.t.Context(), acc.ID, "iPad", "tablet")
	if err != nil {
		h.t.Fatal(err)
	}
	return tok
}

// Lumen bildet die Reihen selbst, und die Detailseite eines Films nennt
// seine Reihe samt der übrigen Teile.
func TestReihenUeberDieSchnittstelle(t *testing.T) {
	h := newHarness(t)
	a1, _ := h.film("Alien", 1)
	a3, _ := h.film("Alien 3", 1)
	h.film("Nordlicht", 1)
	h.svr().ReihenBilden(t.Context())

	_, raw := h.do("GET", "/api/v1/collections", "", h.tok)
	var reihen []struct {
		ID     string
		Name   string
		Anzahl int
		Items  []struct{ ID string }
	}
	if err := json.Unmarshal(raw, &reihen); err != nil {
		t.Fatal(err, string(raw))
	}
	if len(reihen) != 1 || reihen[0].Name != "Alien" || reihen[0].Anzahl != 2 {
		t.Fatalf("Reihen: %s", raw)
	}

	_, raw = h.do("GET", "/api/v1/items/"+a3, "", h.tok)
	if !strings.Contains(string(raw), `"collection"`) || !strings.Contains(string(raw), a1) {
		t.Fatalf("die Detailseite nennt die Reihe nicht: %s", raw)
	}

	// Der Besitzer löst sie auf — dann ist sie für alle weg.
	resp, raw := h.do("POST", "/api/v1/admin/collections/"+reihen[0].ID, `{"aufgeloest":true}`, h.tok)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("auflösen: %d %s", resp.StatusCode, raw)
	}
	_, raw = h.do("GET", "/api/v1/collections", "", h.tok)
	if strings.TrimSpace(string(raw)) != "[]" {
		t.Fatalf("aufgelöste Reihe wird noch gezeigt: %s", raw)
	}
	// Ein Gast darf sie gar nicht ändern.
	gtok := h.gast("Mara", false, h.lib.ID)
	resp, _ = h.do("POST", "/api/v1/admin/collections/"+reihen[0].ID, `{"aufgeloest":false}`, gtok)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("ein Gast konnte eine Reihe ändern: %d", resp.StatusCode)
	}
}

// Die Startseite holt nur so viele Reihen, wie sie zeigt (?limit=), und
// limit/offset zählen nur Reihen, die die Person auch sieht.
func TestReihenMitLimit(t *testing.T) {
	h := newHarness(t)
	for _, name := range []string{"Alien", "Matrix", "Rocky", "Shrek", "Toy Story"} {
		h.film(name, 1)
		h.film(name+" 2", 1)
	}
	h.svr().ReihenBilden(t.Context())
	namen := func(pfad string) []string {
		_, raw := h.do("GET", pfad, "", h.tok)
		var reihen []struct{ Name string }
		if err := json.Unmarshal(raw, &reihen); err != nil {
			t.Fatal(err, string(raw))
		}
		var out []string
		for _, r := range reihen {
			out = append(out, r.Name)
		}
		return out
	}
	alle := namen("/api/v1/collections")
	if len(alle) != 5 {
		t.Fatalf("erwartet 5 Reihen, bekam %v", alle)
	}
	if got := namen("/api/v1/collections?limit=2"); strings.Join(got, "|") != strings.Join(alle[:2], "|") {
		t.Fatalf("limit=2: %v, erwartet %v", got, alle[:2])
	}
	if got := namen("/api/v1/collections?limit=2&offset=3"); strings.Join(got, "|") != strings.Join(alle[3:5], "|") {
		t.Fatalf("limit=2&offset=3: %v, erwartet %v", got, alle[3:5])
	}
	if got := namen("/api/v1/collections?limit=2&offset=9"); len(got) != 0 {
		t.Fatalf("offset hinter dem Ende: %v", got)
	}
}

// Eine Reihe zeigt einem Kind nur, was es sehen darf — und wenn das
// weniger als zwei Filme sind, gar keine Reihe.
func TestReihenAchtenDieFreigabe(t *testing.T) {
	h := newHarness(t)
	h.film("Alien", 1)
	h.film("Alien 3", 1)
	h.svr().ReihenBilden(t.Context())
	ktok := h.gast("Lina", true) // keine Freigabe

	_, raw := h.do("GET", "/api/v1/collections", "", ktok)
	if strings.Contains(string(raw), "Alien") {
		t.Fatalf("Kind sieht eine Reihe aus einer gesperrten Bibliothek: %s", raw)
	}
}

// Listen: anlegen, füllen, teilen, gemeinsam bearbeiten.
func TestListenTeilenUndGemeinsamBearbeiten(t *testing.T) {
	h := newHarness(t)
	f1, _ := h.film("Die Bucht", 1)
	f2, _ := h.film("Nachtzug", 1)
	mtok := h.gast("Mara", false, h.lib.ID)

	// Mara legt eine Liste an und füllt sie.
	_, raw := h.do("POST", "/api/v1/playlists", `{"name":"Filmabend"}`, mtok)
	var l struct{ ID string }
	if err := json.Unmarshal(raw, &l); err != nil || l.ID == "" {
		t.Fatalf("anlegen: %s", raw)
	}
	for _, f := range []string{f1, f2} {
		resp, raw := h.do("POST", "/api/v1/playlists/"+l.ID+"/items", `{"itemId":"`+f+`"}`, mtok)
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("hinzufügen: %d %s", resp.StatusCode, raw)
		}
	}

	// Privat: der Besitzer des Servers sieht sie nicht — weder in der
	// Übersicht noch, wenn er die Kennung kennt.
	_, raw = h.do("GET", "/api/v1/playlists", "", h.tok)
	if strings.Contains(string(raw), "Filmabend") {
		t.Fatalf("der Besitzer sieht eine private Liste: %s", raw)
	}
	resp, _ := h.do("GET", "/api/v1/playlists/"+l.ID, "", h.tok)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("private Liste über die Kennung lesbar: %d", resp.StatusCode)
	}
	resp, _ = h.do("POST", "/api/v1/playlists/"+l.ID+"/items", `{"itemId":"`+f1+`"}`, h.tok)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("in eine fremde private Liste schreiben: %d", resp.StatusCode)
	}

	// Geteilt: jetzt sieht der Haushalt sie — aber schreibt nicht hinein.
	h.do("POST", "/api/v1/playlists/"+l.ID, `{"geteilt":true}`, mtok)
	_, raw = h.do("GET", "/api/v1/playlists/"+l.ID, "", h.tok)
	if !strings.Contains(string(raw), "Die Bucht") || !strings.Contains(string(raw), `"hinzugefuegtVon":"Mara"`) {
		t.Fatalf("geteilte Liste nicht lesbar: %s", raw)
	}
	resp, _ = h.do("DELETE", "/api/v1/playlists/"+l.ID+"/items/"+f1, "", h.tok)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("nur geteilt, trotzdem verändert: %d", resp.StatusCode)
	}

	// Gemeinsam: jetzt darf der Haushalt mitschreiben — aber nicht
	// umbenennen oder löschen.
	h.do("POST", "/api/v1/playlists/"+l.ID, `{"gemeinsam":true}`, mtok)
	resp, _ = h.do("DELETE", "/api/v1/playlists/"+l.ID+"/items/"+f1, "", h.tok)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("gemeinsam, trotzdem nicht veränderbar: %d", resp.StatusCode)
	}
	resp, _ = h.do("POST", "/api/v1/playlists/"+l.ID, `{"name":"Meins"}`, h.tok)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("fremde Liste umbenannt: %d", resp.StatusCode)
	}
	resp, _ = h.do("DELETE", "/api/v1/playlists/"+l.ID, "", h.tok)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("fremde Liste gelöscht: %d", resp.StatusCode)
	}

	// Reihenfolge.
	h.do("POST", "/api/v1/playlists/"+l.ID+"/items", `{"itemId":"`+f1+`"}`, mtok)
	h.do("POST", "/api/v1/playlists/"+l.ID+"/order", `{"itemIds":["`+f1+`","`+f2+`"]}`, mtok)
	_, raw = h.do("GET", "/api/v1/playlists/"+l.ID, "", mtok)
	if strings.Index(string(raw), "Die Bucht") > strings.Index(string(raw), "Nachtzug") {
		t.Fatalf("Reihenfolge nicht übernommen: %s", raw)
	}

	// Nicht mehr geteilt: wieder privat, und gemeinsam ist damit auch aus.
	h.do("POST", "/api/v1/playlists/"+l.ID, `{"geteilt":false}`, mtok)
	resp, _ = h.do("GET", "/api/v1/playlists/"+l.ID, "", h.tok)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("nach dem Zurücknehmen noch lesbar: %d", resp.StatusCode)
	}

	// Mara löscht ihre Liste.
	resp, _ = h.do("DELETE", "/api/v1/playlists/"+l.ID, "", mtok)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("eigene Liste löschen: %d", resp.StatusCode)
	}
}

// Eine geteilte Liste zeigt einem Kind nur, was es sehen darf. Und was
// man nicht sehen darf, kann man auch nicht hineinlegen.
func TestListenAchtenDieFreigabe(t *testing.T) {
	h := newHarness(t)
	f1, _ := h.film("Nur fuer Erwachsene", 1)
	_, raw := h.do("POST", "/api/v1/playlists", `{"name":"Abends"}`, h.tok)
	var l struct{ ID string }
	_ = json.Unmarshal(raw, &l)
	h.do("POST", "/api/v1/playlists/"+l.ID+"/items", `{"itemId":"`+f1+`"}`, h.tok)
	h.do("POST", "/api/v1/playlists/"+l.ID, `{"gemeinsam":true}`, h.tok)

	ktok := h.gast("Lina", true) // keine Freigabe
	_, raw = h.do("GET", "/api/v1/playlists/"+l.ID, "", ktok)
	if strings.Contains(string(raw), "Erwachsene") {
		t.Fatalf("Kind sieht einen gesperrten Titel in einer geteilten Liste: %s", raw)
	}
	_, raw = h.do("GET", "/api/v1/playlists", "", ktok)
	if strings.Contains(string(raw), "Erwachsene") {
		t.Fatalf("… oder in der Vorschau: %s", raw)
	}
	resp, _ := h.do("POST", "/api/v1/playlists/"+l.ID+"/items", `{"itemId":"`+f1+`"}`, ktok)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("Kind legt einen gesperrten Titel in die Liste: %d", resp.StatusCode)
	}
}

// Ein Erwachsener ohne jede Freigabe sieht über Reihen und geteilte
// Listen nichts. Vorher lief die Abfrage mit Kennungen und leerer
// Bibliotheksliste ganz ohne Einschränkung — ein Kinderprofil fiel nur
// zufällig durch die Altersgrenze.
func TestOhneFreigabeNichtsUeberReihenUndListen(t *testing.T) {
	h := newHarness(t)
	f1, _ := h.film("Alien", 1)
	h.film("Alien 3", 1)
	h.svr().ReihenBilden(t.Context())
	_, raw := h.do("POST", "/api/v1/playlists", `{"name":"Abends"}`, h.tok)
	var l struct{ ID string }
	_ = json.Unmarshal(raw, &l)
	h.do("POST", "/api/v1/playlists/"+l.ID+"/items", `{"itemId":"`+f1+`"}`, h.tok)
	h.do("POST", "/api/v1/playlists/"+l.ID, `{"geteilt":true}`, h.tok)

	tok := h.gast("Jonas", false) // erwachsen, aber keine Bibliothek
	for _, p := range []string{"/api/v1/collections", "/api/v1/playlists/" + l.ID, "/api/v1/playlists"} {
		_, raw := h.do("GET", p, "", tok)
		if strings.Contains(string(raw), "Alien") {
			t.Errorf("%s zeigt einen Titel ohne Freigabe: %s", p, raw)
		}
	}
	_, raw = h.do("GET", "/api/v1/playlists", "", tok)
	if !strings.Contains(string(raw), `"anzahl":0`) {
		t.Errorf("die Zahl verrät verborgene Titel: %s", raw)
	}
}
