// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"lumen/internal/store"
)

// Unter „Ich › Geräte" steht, welches Gerät das eigene ist, und jedes
// lässt sich einzeln abmelden — nur die eigenen, nie die eines anderen.
func TestGeraetEinzelnAbmelden(t *testing.T) {
	h := newHarness(t)
	_, raw := h.do("GET", "/api/v1/me", "", h.tok)
	var me struct{ Account struct{ ID string } }
	if err := json.Unmarshal(raw, &me); err != nil {
		t.Fatal(err)
	}
	// Ein zweites Gerät desselben Profils.
	_, raw = h.do("POST", "/api/v1/unlock", fmt.Sprintf(
		`{"profileId":%q,"secret":"einsehrgutespasswort","deviceName":"Safari · iPhone","deviceKind":"browser"}`,
		me.Account.ID), "")
	var zweit struct{ Token string }
	if err := json.Unmarshal(raw, &zweit); err != nil || zweit.Token == "" {
		t.Fatalf("zweite Anmeldung: %s", raw)
	}

	type geraet struct {
		ID           string `json:"id"`
		Name         string `json:"name"`
		DiesesGeraet bool   `json:"diesesGeraet"`
	}
	liste := func(tok string) []geraet {
		_, raw := h.do("GET", "/api/v1/me/devices", "", tok)
		var g []geraet
		if err := json.Unmarshal(raw, &g); err != nil {
			t.Fatalf("%s", raw)
		}
		return g
	}
	g := liste(h.tok)
	if len(g) != 2 {
		t.Fatalf("zwei Geräte erwartet: %+v", g)
	}
	var iphone string
	for _, x := range g {
		if x.Name == "Wohnzimmer" && !x.DiesesGeraet {
			t.Error("das eigene Gerät ist nicht markiert")
		}
		if x.Name == "Safari · iPhone" {
			if x.DiesesGeraet {
				t.Error("das andere Gerät gilt als dieses")
			}
			iphone = x.ID
		}
		if strings.Contains(string(raw), "tokenHash") {
			t.Error("der Prüfwert des Tokens verlässt den Server")
		}
	}

	// Ein anderes Profil kann das Gerät nicht abmelden.
	acc, _ := h.st.CreateAccount(t.Context(), store.NewAccount{Name: "Mara", LockKind: "pin", Secret: "4711"})
	_, raw = h.do("POST", "/api/v1/unlock", fmt.Sprintf(`{"profileId":%q,"secret":"4711"}`, acc.ID), "")
	var mara struct{ Token string }
	_ = json.Unmarshal(raw, &mara)
	if resp, _ := h.do("DELETE", "/api/v1/me/devices/"+url.PathEscape(iphone), "", mara.Token); resp.StatusCode != http.StatusNotFound {
		t.Errorf("fremdes Gerät abmelden gab %d", resp.StatusCode)
	}

	if resp, _ := h.do("DELETE", "/api/v1/me/devices/"+url.PathEscape(iphone), "", h.tok); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("abmelden gab %d", resp.StatusCode)
	}
	if resp, _ := h.do("GET", "/api/v1/me", "", zweit.Token); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("abgemeldetes Gerät kommt noch hinein: %d", resp.StatusCode)
	}
	if len(liste(h.tok)) != 1 {
		t.Error("das Gerät steht noch in der Liste")
	}
}

// Ein Tippfehler findet trotzdem etwas — mit „Meintest du …?".
func TestSucheVerzeihtTippfehler(t *testing.T) {
	h := newHarness(t)
	h.film("Die Sonne", 1)
	h.film("Apocalypse Now", 1)
	for _, f := range []struct{ q, titel, meintest string }{
		{"Sone", "Die Sonne", "Sonne"},
		{"Apokalypse", "Apocalypse Now", "Apocalypse"},
	} {
		_, raw := h.do("GET", "/api/v1/search?q="+url.QueryEscape(f.q), "", h.tok)
		var out struct {
			Items    []struct{ Title string }
			Meintest string
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatal(err)
		}
		if len(out.Items) != 1 || out.Items[0].Title != f.titel || out.Meintest != f.meintest {
			t.Errorf("%q: %s", f.q, raw)
		}
	}
	// Was genau passt, bekommt keinen Vorschlag.
	_, raw := h.do("GET", "/api/v1/search?q=Sonne", "", h.tok)
	if strings.Contains(string(raw), `"meintest":"S`) {
		t.Errorf("Vorschlag trotz Treffer: %s", raw)
	}
}
