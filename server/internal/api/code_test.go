// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"testing"
)

// Der Fernseher holt einen Code, das Handy gibt ihn frei, der Fernseher
// bekommt sein eigenes Token — und nur einmal.
func TestAnmeldenMitCode(t *testing.T) {
	h := newHarness(t)
	resp, raw := h.do("POST", "/api/v1/code/start", `{"deviceName":"Wohnzimmer-TV","deviceKind":"tv"}`, "")
	if resp.StatusCode != 200 {
		t.Fatalf("start: %d %s", resp.StatusCode, raw)
	}
	var st struct{ Code, Geheim string }
	_ = json.Unmarshal(raw, &st)
	if len(st.Code) != 6 || len(st.Geheim) < 20 {
		t.Fatalf("Antwort: %s", raw)
	}
	// Noch nicht freigegeben.
	resp, _ = h.do("GET", "/api/v1/code/"+st.Geheim, "", "")
	if resp.StatusCode != 202 {
		t.Fatalf("wartend: %d", resp.StatusCode)
	}
	// Freigeben braucht eine Anmeldung.
	resp, _ = h.do("POST", "/api/v1/code/approve", `{"code":"`+st.Code+`"}`, "")
	if resp.StatusCode != 401 {
		t.Fatalf("ohne Anmeldung freigegeben: %d", resp.StatusCode)
	}
	// Falscher Code.
	resp, _ = h.do("POST", "/api/v1/code/approve", `{"code":"000000x"}`, h.tok)
	if resp.StatusCode != 404 {
		t.Fatalf("falscher Code: %d", resp.StatusCode)
	}
	// Richtiger Code, mit Leerzeichen getippt.
	resp, raw = h.do("POST", "/api/v1/code/approve", `{"code":"`+st.Code[:3]+` `+st.Code[3:]+`"}`, h.tok)
	if resp.StatusCode != 200 {
		t.Fatalf("freigeben: %d %s", resp.StatusCode, raw)
	}
	resp, raw = h.do("GET", "/api/v1/code/"+st.Geheim, "", "")
	if resp.StatusCode != 200 {
		t.Fatalf("abholen: %d %s", resp.StatusCode, raw)
	}
	var fertig struct {
		Token   string
		Account struct{ Name string }
	}
	_ = json.Unmarshal(raw, &fertig)
	if fertig.Token == "" || fertig.Account.Name != "Alex" {
		t.Fatalf("Token: %s", raw)
	}
	// Das Token taugt.
	resp, _ = h.do("GET", "/api/v1/me", "", fertig.Token)
	if resp.StatusCode != 200 {
		t.Fatalf("me mit neuem Token: %d", resp.StatusCode)
	}
	// Ein zweites Abholen geht nicht.
	resp, _ = h.do("GET", "/api/v1/code/"+st.Geheim, "", "")
	if resp.StatusCode != 410 {
		t.Fatalf("zweimal abgeholt: %d", resp.StatusCode)
	}
	// Der Code lässt sich nicht nochmal freigeben.
	resp, _ = h.do("POST", "/api/v1/code/approve", `{"code":"`+st.Code+`"}`, h.tok)
	if resp.StatusCode != 404 {
		t.Fatalf("nochmal freigegeben: %d", resp.StatusCode)
	}
}

// Servername für die Apps, und Abmelden genau dieses Geräts.
func TestServernameUndGeraetAbmelden(t *testing.T) {
	h := newHarness(t)
	resp, _ := h.do("PUT", "/api/v1/admin/server-name", `{"name":"Mein NAS"}`, h.tok)
	if resp.StatusCode != 200 {
		t.Fatalf("Name setzen: %d", resp.StatusCode)
	}
	_, raw := h.do("GET", "/api/v1/hello", "", "")
	var hello struct{ Server, Name string }
	_ = json.Unmarshal(raw, &hello)
	if hello.Server != "Lumen" || hello.Name != "Mein NAS" {
		t.Fatalf("hello: %s", raw)
	}
	// Ein zweites Gerät, dann dieses abmelden: das erste bleibt angemeldet.
	_, raw = h.do("POST", "/api/v1/code/start", `{"deviceName":"TV"}`, "")
	var st struct{ Code, Geheim string }
	_ = json.Unmarshal(raw, &st)
	h.do("POST", "/api/v1/code/approve", `{"code":"`+st.Code+`"}`, h.tok)
	_, raw = h.do("GET", "/api/v1/code/"+st.Geheim, "", "")
	var tv struct{ Token string }
	_ = json.Unmarshal(raw, &tv)
	resp, _ = h.do("DELETE", "/api/v1/me/device", "", tv.Token)
	if resp.StatusCode != 204 {
		t.Fatalf("abmelden: %d", resp.StatusCode)
	}
	if resp, _ = h.do("GET", "/api/v1/me", "", tv.Token); resp.StatusCode != 401 {
		t.Errorf("TV noch angemeldet: %d", resp.StatusCode)
	}
	if resp, _ = h.do("GET", "/api/v1/me", "", h.tok); resp.StatusCode != 200 {
		t.Errorf("erstes Gerät mit abgemeldet: %d", resp.StatusCode)
	}
}
