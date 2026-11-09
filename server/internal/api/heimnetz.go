// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

// Was als Heimnetz zählt.
//
// Eine private Absenderadresse heisst nicht immer „zu Hause". Läuft Lumen
// in Docker im Bridge-Betrieb, kann der echte Absender unterwegs durch das
// Gateway des Container-Netzes ersetzt werden (172.17.0.1 o. ä.): beim
// Zugriff über IPv6 (docker-proxy), auf einem NAS ohne iptables-DNAT oder
// hinter einem Reverse-Proxy auf demselben NAS, der kein X-Forwarded-For
// setzt. Dann sähe eine Anfrage aus dem Internet aus wie eine aus dem
// Wohnzimmer — mit Profilliste, offenen Profilen und Einrichten.
//
// Darum:
//   - Gateway-Adressen der eigenen Netze (die Standardroute und die erste
//     Adresse jedes eigenen Netzes, etwa x.x.x.1) zählen nie als zu Hause.
//     Wer in so einem Aufbau trotzdem die Profilauswahl will, legt das
//     Heimnetz ausdrücklich fest.
//   - LUMEN_HEIMNETZ (Liste von Netzen, z. B. „192.168.1.0/24,fd00::/8")
//     legt ausdrücklich fest, was zu Hause ist. Ist es gesetzt, gilt nur
//     das — auch eine Gateway-Adresse, wenn man sie hineinschreibt.

import (
	"bufio"
	"encoding/binary"
	"encoding/hex"
	"net"
	"os"
	"strings"
	"sync"
	"time"
)

// heimnetzAus liest eine Liste von Netzen („10.0.0.0/8, 192.168.1.5").
// Einzelne Adressen gelten als /32 bzw. /128. Unlesbares fällt weg.
func heimnetzAus(roh string) []*net.IPNet {
	var out []*net.IPNet
	for _, teil := range strings.Split(roh, ",") {
		teil = strings.TrimSpace(teil)
		if teil == "" {
			continue
		}
		if !strings.Contains(teil, "/") {
			if ip := net.ParseIP(teil); ip != nil {
				if ip.To4() != nil {
					teil += "/32"
				} else {
					teil += "/128"
				}
			}
		}
		if _, n, err := net.ParseCIDR(teil); err == nil {
			out = append(out, n)
		}
	}
	return out
}

// netzStand hält, was über die eigenen Netze bekannt ist. Die Adressen
// ändern sich selten (DHCP, neues Container-Netz), darum wird alle paar
// Minuten neu nachgesehen statt bei jeder Anfrage.
type netzStand struct {
	mu         sync.Mutex
	bis        time.Time
	gateways   map[string]bool
	heimnetz   []*net.IPNet
	festgelegt bool
}

var netze netzStand

// gatewaysFinden ist austauschbar, damit Tests eigene Gateways setzen.
var gatewaysFinden = eigeneGateways

func (n *netzStand) aktuell() (map[string]bool, []*net.IPNet, bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if time.Now().After(n.bis) {
		n.gateways = map[string]bool{}
		for _, ip := range gatewaysFinden() {
			n.gateways[ip.String()] = true
		}
		roh := os.Getenv("LUMEN_HEIMNETZ")
		n.festgelegt = strings.TrimSpace(roh) != ""
		n.heimnetz = heimnetzAus(roh)
		n.bis = time.Now().Add(5 * time.Minute)
	}
	return n.gateways, n.heimnetz, n.festgelegt
}

// netzeNeuLesen vergisst den Stand (für Tests und nach Änderungen).
func netzeNeuLesen() {
	netze.mu.Lock()
	netze.bis = time.Time{}
	netze.mu.Unlock()
}

// imHeimnetz entscheidet für eine Absenderadresse, ob sie zu Hause ist.
func imHeimnetz(ip net.IP) bool {
	if ip == nil {
		return false
	}
	gateways, heim, festgelegt := netze.aktuell()
	if festgelegt {
		for _, n := range heim {
			if n.Contains(ip) {
				return true
			}
		}
		// Lumen selbst (Gesundheitsprüfung, ein Browser auf dem NAS)
		// bleibt zu Hause, auch wenn das Heimnetz anders festgelegt ist.
		return ip.IsLoopback()
	}
	if ip.IsLoopback() {
		return true
	}
	if !(ip.IsPrivate() || ip.IsLinkLocalUnicast()) {
		return false
	}
	return !gateways[ip.String()]
}

// eigeneGateways sammelt die Gateway-Adressen der eigenen Netze: die
// Standardroute (Linux: /proc/net/route) und die erste Adresse jedes
// eigenen Netzes — dort sitzt bei Docker die Brücke, im Heimnetz der
// Router. Keiner von beiden ist eine Person vor einem Bildschirm.
func eigeneGateways() []net.IP {
	var out []net.IP
	if f, err := os.Open("/proc/net/route"); err == nil {
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			if ip := gatewayAusRoute(sc.Text()); ip != nil {
				out = append(out, ip)
			}
		}
		f.Close()
	}
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, a := range addrs {
			n, ok := a.(*net.IPNet)
			if !ok || n.IP.IsLoopback() {
				continue
			}
			if ip := ersteAdresse(n); ip != nil {
				out = append(out, ip)
			}
		}
	}
	return out
}

// gatewayAusRoute liest eine Zeile aus /proc/net/route. Das Gateway steht
// dort hexadezimal in der Byte-Reihenfolge des Rechners (fast immer
// Little Endian): „010011AC" ist 172.17.0.1.
func gatewayAusRoute(zeile string) net.IP {
	f := strings.Fields(zeile)
	if len(f) < 3 || f[0] == "Iface" {
		return nil
	}
	b, err := hex.DecodeString(f[2])
	if err != nil || len(b) != 4 {
		return nil
	}
	v := binary.LittleEndian.Uint32(b)
	if v == 0 {
		return nil
	}
	ip := make(net.IP, 4)
	binary.BigEndian.PutUint32(ip, v)
	return ip
}

// ersteAdresse ist die Netzadresse plus eins (192.168.1.0/24 → .1).
func ersteAdresse(n *net.IPNet) net.IP {
	ip := n.IP.Mask(n.Mask)
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	if ip == nil {
		return nil
	}
	out := make(net.IP, len(ip))
	copy(out, ip)
	out[len(out)-1]++
	if out.Equal(n.IP) {
		// Die eigene Adresse ist die erste des Netzes: dann ist hier kein
		// Gateway, sondern Lumen selbst.
		return nil
	}
	return out
}
