// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"net"
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
