// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"strings"
	"testing"
)

// Geteilte Bewertungen: Ausschalten nimmt auch die bisherigen zurück, und
// ein Gast (von unterwegs eingeladen) sieht weder Bewertungen noch Listen
// des Haushalts — und der Haushalt nicht seine.
func TestGeteilteBewertungenUndGaeste(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()
	film, _ := h.film("Krimi Abend", 1)
	ben := h.gast("Ben", false, h.lib.ID)
	anna := h.gast("Anna", false, h.lib.ID)
	fritz := h.gast("Fritz", false, h.lib.ID)
	if err := h.st.GastSetzen(ctx, h.idVon(fritz), true); err != nil {
		t.Fatal(err)
	}
	h.do("PATCH", "/api/v1/me", `{"prefs":{"shareRatings":"1"}}`, ben)
	if resp, raw := h.do("PUT", "/api/v1/items/"+film+"/rating", `{"stars":5,"shared":true}`, ben); resp.StatusCode != 204 {
		t.Fatalf("bewerten: %d %s", resp.StatusCode, raw)
	}
	h.do("PUT", "/api/v1/items/"+film+"/rating", `{"stars":2,"shared":true}`, fritz)
	l, err := h.st.ListeAnlegen(ctx, h.idVon(ben), "Familienabend")
	if err != nil {
		t.Fatal(err)
	}
	ja := true
	_ = h.st.ListeAendern(ctx, h.idVon(ben), l.ID, nil, &ja, nil)

	_, raw := h.do("GET", "/api/v1/items/"+film, "", anna)
	if !strings.Contains(string(raw), `"name":"Ben"`) || strings.Contains(string(raw), `"name":"Fritz"`) {
		t.Fatalf("Anna sieht: %s", raw)
	}
	_, raw = h.do("GET", "/api/v1/items/"+film, "", fritz)
	if strings.Contains(string(raw), `"name":"Ben"`) {
		t.Fatalf("Gast sieht Bewertungen des Haushalts: %s", raw)
	}
	if ls, _ := h.st.Listen(ctx, h.idVon(fritz)); len(ls) != 0 {
		t.Fatalf("Gast sieht geteilte Listen: %+v", ls)
	}
	if ls, _ := h.st.Listen(ctx, h.idVon(anna)); len(ls) != 1 {
		t.Fatalf("Anna sieht die geteilte Liste nicht: %+v", ls)
	}

	// Ben schaltet das Teilen aus: auch die bisherige Bewertung geht zurück.
	if resp, raw := h.do("PATCH", "/api/v1/me", `{"prefs":{"shareRatings":"0"}}`, ben); resp.StatusCode != 200 {
		t.Fatalf("ausschalten: %d %s", resp.StatusCode, raw)
	}
	_, raw = h.do("GET", "/api/v1/items/"+film, "", anna)
	if strings.Contains(string(raw), `"name":"Ben"`) {
		t.Fatalf("nach dem Ausschalten noch geteilt: %s", raw)
	}
}

// Wer eine Einladung von unterwegs einlöst, ist Gast.
func TestEinladungVonUnterwegsIstGast(t *testing.T) {
	h := newHarness(t)
	h.film("Nordlicht", 1)
	_, raw := h.do("POST", "/api/v1/admin/invites", `{"libraries":["`+h.lib.ID+`"],"limits":{"maxAge":18,"maxStreams":2,"allowTranscode":true,"remoteQuality":"1080p"},"days":7}`, h.tok)
	code := ""
	if i := strings.Index(string(raw), `"code":"`); i >= 0 {
		code = strings.SplitN(string(raw)[i+8:], `"`, 2)[0]
	}
	if code == "" {
		t.Fatalf("Einladung: %s", raw)
	}
	status, body := h.unterwegs("POST", "/api/v1/invite/redeem", `{"code":"`+code+`","name":"Fritz","lockKind":"pin","secret":"4711"}`)
	if status != 200 {
		t.Fatalf("einlösen: %d %s", status, body)
	}
	_, raw = h.do("GET", "/api/v1/admin/accounts", "", h.tok)
	if !strings.Contains(string(raw), `"gast":true`) {
		t.Fatalf("Fritz ist kein Gast: %s", raw)
	}
}
