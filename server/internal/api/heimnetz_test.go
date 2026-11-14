// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func mitGateways(t *testing.T, heimnetz string, gw ...string) {
	t.Helper()
	alt := gatewaysFinden
	gatewaysFinden = func() []net.IP {
		var out []net.IP
		for _, g := range gw {
			out = append(out, net.ParseIP(g))
		}
		return out
	}
	t.Setenv("LUMEN_HEIMNETZ", heimnetz)
	netzeNeuLesen()
	t.Cleanup(func() { gatewaysFinden = alt; netzeNeuLesen() })
}

func anfrageVon(adresse string) bool {
	r := httptest.NewRequest("GET", "/api/v1/profiles", nil)
	r.RemoteAddr = adresse
	return vonAussen(r)
}

// Die Brücke von Docker steht für Anfragen von irgendwo — nie zu Hause.
func TestGatewayIstNichtZuHause(t *testing.T) {
	mitGateways(t, "", "172.17.0.1", "fd00::1")
	if !anfrageVon("172.17.0.1:5555") {
		t.Fatal("Docker-Gateway gilt als zu Hause")
	}
	if !anfrageVon("[fd00::1]:5555") {
		t.Fatal("IPv6-Gateway gilt als zu Hause")
	}
	if anfrageVon("192.168.1.20:5555") || anfrageVon("127.0.0.1:1") {
		t.Fatal("echter Absender im Heimnetz gilt als unterwegs")
	}
	if !anfrageVon("203.0.113.9:1") {
		t.Fatal("öffentliche Adresse gilt als zu Hause")
	}
}

// LUMEN_HEIMNETZ legt ausdrücklich fest, was zu Hause ist.
func TestHeimnetzFestgelegt(t *testing.T) {
	mitGateways(t, "192.168.1.0/24, 10.0.0.7", "172.17.0.1")
	if anfrageVon("192.168.1.20:1") || anfrageVon("10.0.0.7:1") {
		t.Fatal("festgelegtes Heimnetz gilt als unterwegs")
	}
	if !anfrageVon("192.168.2.20:1") || !anfrageVon("10.0.0.8:1") {
		t.Fatal("Adresse ausserhalb des festgelegten Heimnetzes gilt als zu Hause")
	}
	if anfrageVon("127.0.0.1:1") {
		t.Fatal("Lumen selbst gilt als unterwegs")
	}
	// Wer das Gateway ausdrücklich hineinschreibt, bekommt es.
	mitGateways(t, "172.17.0.1", "172.17.0.1")
	if anfrageVon("172.17.0.1:1") {
		t.Fatal("ausdrücklich festgelegtes Gateway gilt als unterwegs")
	}
}

func TestGatewayAusRoute(t *testing.T) {
	if ip := gatewayAusRoute("eth0\t00000000\t010011AC\t0003\t0\t0\t0\t00000000"); ip.String() != "172.17.0.1" {
		t.Fatalf("gelesen: %v", ip)
	}
	if gatewayAusRoute("eth0\t000011AC\t00000000\t0001") != nil {
		t.Fatal("Route ohne Gateway ergibt ein Gateway")
	}
	_, n, _ := net.ParseCIDR("172.18.0.5/16")
	n.IP = net.ParseIP("172.18.0.5")
	if ip := ersteAdresse(n); ip.String() != "172.18.0.1" {
		t.Fatalf("erste Adresse: %v", ip)
	}
}

func mitSicht(t *testing.T, s netzSicht) {
	t.Helper()
	alt := sichtFinden
	sichtFinden = func() netzSicht { return s }
	netzeNeuLesen()
	t.Cleanup(func() { sichtFinden = alt; netzeNeuLesen() })
}

func netzVon(t *testing.T, cidr string) *net.IPNet {
	t.Helper()
	_, n, err := net.ParseCIDR(cidr)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// Lumen im Netz des NAS (network_mode: host): Die Geräte im Heimnetz sind
// zu Hause, aber nichts, was vom NAS selbst kommt — dort kann ein
// Reverse-Proxy Anfragen aus dem Internet ohne X-Forwarded-For
// weiterreichen —, und nichts aus anderen Containern.
func TestHostNetzStreng(t *testing.T) {
	mitGateways(t, "", "192.168.10.1")
	mitSicht(t, netzSicht{
		eigene:         map[string]bool{"192.168.10.10": true, "172.17.0.1": true, "fd00::10": true},
		containerNetze: []*net.IPNet{netzVon(t, "172.17.0.0/16"), netzVon(t, "172.18.0.0/16")},
		hostNetz:       true,
	})
	if anfrageVon("192.168.10.23:5000") {
		t.Fatal("Gerät im Heimnetz gilt als unterwegs")
	}
	for _, a := range []string{"127.0.0.1:1", "[::1]:1", "192.168.10.10:1", "172.17.0.1:1", "[fd00::10]:1",
		"172.17.0.5:1", "172.18.0.3:1", "192.168.10.1:1", "203.0.113.9:1", "[2001:db8::5]:1"} {
		if !anfrageVon(a) {
			t.Fatalf("%s gilt als zu Hause", a)
		}
	}
	// Ein ganzes Netz in LUMEN_HEIMNETZ macht den NAS selbst nicht zu
	// Hause, eine einzeln eingetragene Adresse schon.
	mitGateways(t, "192.168.10.0/24", "192.168.10.1")
	if !anfrageVon("192.168.10.10:1") || !anfrageVon("127.0.0.1:1") {
		t.Fatal("NAS selbst gilt über ein ganzes Netz als zu Hause")
	}
	if anfrageVon("192.168.10.23:1") {
		t.Fatal("festgelegtes Heimnetz gilt als unterwegs")
	}
	mitGateways(t, "192.168.10.0/24,127.0.0.1", "192.168.10.1")
	if anfrageVon("127.0.0.1:1") {
		t.Fatal("einzeln festgelegtes 127.0.0.1 gilt als unterwegs")
	}
}

// Im eigenen Container-Netz ist 127.0.0.1 Lumen selbst (Gesundheitsprüfung),
// die eigene Adresse des Containers aber nicht.
func TestBrueckeSelbst(t *testing.T) {
	mitGateways(t, "", "172.18.0.1")
	mitSicht(t, netzSicht{eigene: map[string]bool{"172.18.0.2": true}, bruecke: true})
	if anfrageVon("127.0.0.1:1") {
		t.Fatal("127.0.0.1 im eigenen Container gilt als unterwegs")
	}
	if !anfrageVon("172.18.0.2:1") || !anfrageVon("172.18.0.1:1") {
		t.Fatal("Container selbst oder Gateway gilt als zu Hause")
	}
}

// Weitergereichte Angaben und der Vermittler bleiben unterwegs — auch von
// einer Adresse, die sonst zu Hause wäre, und im Netz des NAS.
func TestWeitergereichtBleibtUnterwegs(t *testing.T) {
	mitGateways(t, "", "192.168.10.1")
	mitSicht(t, netzSicht{eigene: map[string]bool{"192.168.10.10": true}, hostNetz: true})
	for _, k := range weitergereichtKoepfe {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = "192.168.10.23:1"
		r.Header.Set(k, "192.168.10.23")
		h := herkunftVon(r)
		if h.ZuHause || h.Grund != grundWeitergereicht || h.Kopf != k {
			t.Fatalf("%s: %+v", k, h)
		}
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "192.168.10.23:1"
	var gesehen herkunft
	VonAussen(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		gesehen = herkunftVon(r)
	})).ServeHTTP(httptest.NewRecorder(), r)
	if gesehen.ZuHause || gesehen.Grund != grundVermittler {
		t.Fatalf("Vermittler: %+v", gesehen)
	}
}

// Die Gründe kommen so in der Verwaltung an, und Anfragen über ein
// Gateway werden gemerkt.
func TestHerkunftGruende(t *testing.T) {
	mitGateways(t, "", "172.17.0.1")
	mitSicht(t, netzSicht{eigene: map[string]bool{"172.17.0.2": true}, bruecke: true})
	_, _, vorher := gatewayGesehen()
	faelle := map[string]string{
		"192.168.1.20:1": grundPrivat,
		"172.17.0.1:1":   grundGateway,
		"172.17.0.2:1":   grundRechnerSelbst,
		"127.0.0.1:1":    grundLumenSelbst,
		"8.8.8.8:1":      grundOeffentlich,
		"kaputt":         grundUnbekannt,
	}
	for adr, grund := range faelle {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = adr
		if h := herkunftVon(r); h.Grund != grund {
			t.Fatalf("%s: %+v, erwartet %s", adr, h, grund)
		}
	}
	adr, _, nachher := gatewayGesehen()
	if nachher != vorher+1 || adr != "172.17.0.1" {
		t.Fatalf("Gateway nicht gemerkt: %s %d→%d", adr, vorher, nachher)
	}
}

// Verwaltung › Netzwerk: nur für die Verwaltung, mit Adresse und Grund.
func TestAdminNetz(t *testing.T) {
	h := newHarness(t)
	resp, raw := h.do("GET", "/api/v1/admin/netz", "", "")
	if resp.StatusCode == 200 {
		t.Fatal("ohne Anmeldung lesbar")
	}
	resp, raw = h.do("GET", "/api/v1/admin/netz", "", h.tok)
	if resp.StatusCode != 200 {
		t.Fatalf("%d %s", resp.StatusCode, raw)
	}
	var out struct {
		Geraet herkunft `json:"geraet"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out.Geraet.Adresse != "127.0.0.1" || !out.Geraet.ZuHause || out.Geraet.Grund != grundLumenSelbst {
		t.Fatalf("%+v", out.Geraet)
	}
}
