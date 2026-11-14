// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"net"
	"testing"
)

func ifc(t *testing.T, name string, cidrs ...string) schnittstelle {
	t.Helper()
	x := schnittstelle{name: name}
	for _, c := range cidrs {
		ip, n, err := net.ParseCIDR(c)
		if err != nil {
			t.Fatal(err)
		}
		n.IP = ip
		x.netze = append(x.netze, n)
	}
	return x
}

func pruefeHerkunft(t *testing.T, faelle map[string]string) {
	t.Helper()
	for adr, grund := range faelle {
		zuHause, g := heimnetzPruefen(net.ParseIP(adr))
		if g != grund {
			t.Errorf("%s: zuHause=%v grund=%s, erwartet %s", adr, zuHause, g, grund)
		}
	}
}

// Bridge-Betrieb: Ein anderer Container im eigenen Compose-Netz (etwa ein
// Reverse-Proxy) galt als zu Hause. Lumen selbst über 127.0.0.1 bleibt
// zu Hause (Gesundheitsprüfung).
func TestBrueckeEigenesNetzIstContainerNetz(t *testing.T) {
	mitGateways(t, "", "172.18.0.1")
	lo := ifc(t, "lo", "127.0.0.1/8", "::1/128")
	eth0 := ifc(t, "eth0", "172.18.0.3/16")
	for _, arten := range []map[string]string{{"eth0": "veth"}, nil} {
		mitSicht(t, sichtAus([]schnittstelle{lo, eth0}, true, arten))
		pruefeHerkunft(t, map[string]string{
			"172.18.0.5":   grundContainerNetz,
			"172.18.0.3":   grundRechnerSelbst,
			"172.18.0.1":   grundGateway,
			"127.0.0.1":    grundLumenSelbst,
			"192.168.1.20": grundPrivat,
		})
	}
	// macvlan: Der Container hat eine eigene Adresse im Heimnetz — eth0
	// ist dann das Heimnetz.
	mitGateways(t, "", "192.168.1.1")
	mitSicht(t, sichtAus([]schnittstelle{lo, ifc(t, "eth0", "192.168.1.50/24")}, true, map[string]string{"eth0": "macvlan"}))
	pruefeHerkunft(t, map[string]string{
		"192.168.1.20": grundPrivat,
		"192.168.1.1":  grundGateway,
		"127.0.0.1":    grundLumenSelbst,
	})
}

// Ohne Container (Lumen als Dienst auf dem Rechner): 127.0.0.1 kann jeder
// Dienst dort sein, etwa ein Reverse-Proxy ohne X-Forwarded-For — wie im
// Netz des Rechners gilt das als unterwegs, ausser es steht einzeln in
// LUMEN_HEIMNETZ.
func TestNativ127IstRechnerSelbst(t *testing.T) {
	mitGateways(t, "", "192.168.1.1")
	mitSicht(t, sichtAus([]schnittstelle{
		ifc(t, "lo", "127.0.0.1/8", "::1/128"),
		ifc(t, "eth0", "192.168.1.10/24"),
	}, false, nil))
	pruefeHerkunft(t, map[string]string{
		"127.0.0.1":    grundRechnerSelbst,
		"::1":          grundRechnerSelbst,
		"192.168.1.10": grundRechnerSelbst,
		"192.168.1.20": grundPrivat,
	})
	mitGateways(t, "192.168.1.0/24,127.0.0.1", "192.168.1.1")
	pruefeHerkunft(t, map[string]string{
		"127.0.0.1":    grundFestgelegt,
		"192.168.1.10": grundRechnerSelbst,
	})
}

// Im Netz des Rechners und nativ: Eine globale IPv6-Adresse aus dem
// On-Link-Präfix einer eigenen Schnittstelle ist ein Gerät zu Hause.
// Nicht: andere Präfixe, Präfixe von Container-Brücken, zu kurze Präfixe
// und alles im Bridge-Betrieb.
func TestIPv6AusEigenemPraefixIstZuHause(t *testing.T) {
	mitGateways(t, "", "192.168.1.1")
	lo := ifc(t, "lo", "127.0.0.1/8", "::1/128")
	eth0 := ifc(t, "eth0", "192.168.1.10/24", "2a02:1234:5678:9a00::10/64", "fe80::1/64")
	docker0 := ifc(t, "docker0", "172.17.0.1/16", "2a02:1234:5678:9aff::1/64")
	kurz := ifc(t, "wg0", "2a02:9999::1/16")
	for _, container := range []bool{false, true} { // nativ / network_mode: host
		mitSicht(t, sichtAus([]schnittstelle{lo, eth0, docker0, kurz}, container, nil))
		pruefeHerkunft(t, map[string]string{
			"2a02:1234:5678:9a00::42": grundPrivat,
			"2a02:1234:5678:9a00::10": grundRechnerSelbst,
			"2a02:1234:5678:9b00::1":  grundOeffentlich,
			"2a02:1234:5678:9aff::5":  grundOeffentlich,
			"2a02:9999::5":            grundOeffentlich,
			"2001:db8::5":             grundOeffentlich,
			"203.0.113.9":             grundOeffentlich,
			"fe80::2":                 grundPrivat,
		})
	}
	// Bridge-Betrieb: Das Präfix des Containers ist nicht das Heimnetz.
	mitSicht(t, sichtAus([]schnittstelle{lo, ifc(t, "eth0", "172.18.0.3/16", "2a02:1234:5678:9a00::10/64")}, true, nil))
	pruefeHerkunft(t, map[string]string{"2a02:1234:5678:9a00::42": grundOeffentlich})
}
