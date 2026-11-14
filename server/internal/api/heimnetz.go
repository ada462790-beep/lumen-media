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
//   - Der Rechner selbst (seine Adressen, und 127.0.0.1, wenn Lumen im
//     Netz des Rechners läuft) und andere Container auf ihm zählen nicht
//     als zu Hause: Dort sitzt vielleicht ein Reverse-Proxy, der Anfragen
//     aus dem Internet weiterreicht, ohne es zu sagen.
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
	sicht      netzSicht
}

// netzSicht: was Lumen von den Schnittstellen des Rechners sieht, auf dem
// es läuft (im Container: die des Containers).
type netzSicht struct {
	// eigene: die eigenen Adressen ausser 127.0.0.1/::1.
	eigene map[string]bool
	// containerNetze: Netze von Brücken für Container (docker0, br-…).
	// Wer von dort kommt, ist ein anderer Container auf demselben Rechner
	// — etwa ein Reverse-Proxy —, keine Person im Wohnzimmer.
	containerNetze []*net.IPNet
	// hostNetz: Lumen läuft im Container, aber im Netz des Rechners
	// (Docker „network_mode: host"). Dann kommen über 127.0.0.1 und die
	// Adressen des NAS auch Dienste des NAS selbst, etwa sein Reverse-Proxy.
	hostNetz bool
	// bruecke: Lumen läuft im Container mit eigenem Netz (Bridge-Betrieb).
	// Nur dann ist 127.0.0.1 sicher Lumen selbst (Gesundheitsprüfung);
	// im Netz des Rechners und ohne Container (nativ) kann es jeder Dienst
	// auf dem Rechner sein, etwa ein Reverse-Proxy.
	bruecke bool
	// heimPraefixe: die On-Link-Präfixe der globalen IPv6-Adressen eigener
	// Schnittstellen (keine Container-Brücken), etwa 2a02:…:9a00::/64.
	// Geräte im Heimnetz haben mit IPv6 meist nur solche Adressen — sie
	// sind nicht „privat", aber zu Hause. Nur im Netz des Rechners bzw.
	// ohne Container bekannt; im Bridge-Betrieb sieht Lumen das Heimnetz
	// nicht.
	heimPraefixe []*net.IPNet
}

var netze netzStand

// gatewaysFinden und sichtFinden sind austauschbar, damit Tests eigene
// Netze setzen.
var (
	gatewaysFinden = eigeneGateways
	sichtFinden    = eigeneSicht
)

func (n *netzStand) aktuell() (map[string]bool, []*net.IPNet, bool, netzSicht) {
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
		n.sicht = sichtFinden()
		n.bis = time.Now().Add(5 * time.Minute)
	}
	return n.gateways, n.heimnetz, n.festgelegt, n.sicht
}

// netzeNeuLesen vergisst den Stand (für Tests und nach Änderungen).
func netzeNeuLesen() {
	netze.mu.Lock()
	netze.bis = time.Time{}
	netze.mu.Unlock()
}

// Gründe, warum eine Adresse als zu Hause oder unterwegs gilt. Die
// Verwaltung zeigt sie an (Verwaltung › Netzwerk), damit man sieht, woran
// es liegt, statt zu raten.
const (
	grundFestgelegt      = "festgelegt"      // zu Hause: steht in LUMEN_HEIMNETZ
	grundNichtFestgelegt = "nichtFestgelegt" // unterwegs: nicht in LUMEN_HEIMNETZ
	grundLumenSelbst     = "lumenSelbst"     // zu Hause: 127.0.0.1 im eigenen Container
	grundRechnerSelbst   = "rechnerSelbst"   // unterwegs: der Rechner selbst (Proxy?)
	grundContainerNetz   = "containerNetz"   // unterwegs: anderer Container
	grundGateway         = "gateway"         // unterwegs: Gateway eines eigenen Netzes
	grundOeffentlich     = "oeffentlich"     // unterwegs: keine private Adresse
	grundPrivat          = "privat"          // zu Hause: private Adresse im Heimnetz
	grundUnbekannt       = "unbekannt"       // unterwegs: Adresse nicht lesbar
	grundVermittler      = "vermittler"      // unterwegs: über den Vermittler
	grundWeitergereicht  = "weitergereicht"  // unterwegs: Reverse-Proxy davor
)

// imHeimnetz entscheidet für eine Absenderadresse, ob sie zu Hause ist.
func imHeimnetz(ip net.IP) bool {
	zuHause, _ := heimnetzPruefen(ip)
	return zuHause
}

// heimnetzPruefen sagt, ob eine Adresse zu Hause ist, und warum.
func heimnetzPruefen(ip net.IP) (bool, string) {
	if ip == nil {
		return false, grundUnbekannt
	}
	gateways, heim, festgelegt, sicht := netze.aktuell()
	// Der Rechner selbst: Im eigenen Container ist 127.0.0.1 Lumen selbst
	// (Gesundheitsprüfung). Im Netz des Rechners dagegen und von den
	// eigenen Adressen aus kann es jeder Dienst auf dem NAS sein — auch ein
	// Reverse-Proxy, der Anfragen aus dem Internet ohne X-Forwarded-For
	// weiterreicht. Das bleibt unterwegs, ausser die Adresse steht einzeln
	// in LUMEN_HEIMNETZ.
	//
	// Ohne Container (Lumen nativ als Dienst) gilt dasselbe wie im Netz
	// des Rechners: Ein Reverse-Proxy auf demselben Rechner (nginx gibt
	// ohne eigene Zeile kein X-Forwarded-For mit) reichte sonst Anfragen
	// aus dem Internet als „zu Hause" durch — mit Profilliste und
	// Einrichten. Wer auf dem Rechner selbst einrichten will, öffnet Lumen
	// von einem anderen Gerät im Heimnetz oder setzt LUMEN_HEIMNETZ=127.0.0.1.
	selbst := ip.IsLoopback() || sicht.eigene[ip.String()]
	if selbst {
		if ip.IsLoopback() && sicht.bruecke {
			return true, grundLumenSelbst
		}
		if festgelegt && einzelnFestgelegt(heim, ip) {
			return true, grundFestgelegt
		}
		return false, grundRechnerSelbst
	}
	if festgelegt {
		for _, n := range heim {
			if n.Contains(ip) {
				return true, grundFestgelegt
			}
		}
		return false, grundNichtFestgelegt
	}
	if gateways[ip.String()] {
		return false, grundGateway
	}
	if !(ip.IsPrivate() || ip.IsLinkLocalUnicast()) {
		// Globale IPv6 aus einem Präfix, das direkt an einer eigenen
		// Schnittstelle hängt: ein Gerät im selben Netz.
		if ip.To4() == nil {
			for _, n := range sicht.heimPraefixe {
				if n.Contains(ip) {
					return true, grundPrivat
				}
			}
		}
		return false, grundOeffentlich
	}
	for _, n := range sicht.containerNetze {
		if n.Contains(ip) {
			return false, grundContainerNetz
		}
	}
	return true, grundPrivat
}

// einzelnFestgelegt: steht genau diese Adresse (als /32 bzw. /128) in
// LUMEN_HEIMNETZ? Ein ganzes Netz genügt für den Rechner selbst nicht.
func einzelnFestgelegt(heim []*net.IPNet, ip net.IP) bool {
	for _, n := range heim {
		eins, bits := n.Mask.Size()
		if eins == bits && n.Contains(ip) {
			return true
		}
	}
	return false
}

// containerSchnittstelle: Namen, unter denen Docker, Podman und Co. ihre
// Brücken und virtuellen Kabel anlegen.
func containerSchnittstelle(name string) bool {
	for _, p := range []string{"docker", "br-", "veth", "cni", "podman", "virbr", "lxcbr", "lxdbr"} {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// eigeneSicht liest die Schnittstellen. Sieht Lumen im Container Brücken
// wie docker0, läuft es im Netz des Rechners — im eigenen Container-Netz
// gibt es nur lo und eth0.
//
// Im Bridge-Betrieb hängt eth0 als virtuelles Kabel (veth) am Netz des
// Containers. Wer aus diesem Netz kommt, ist ein anderer Container auf
// demselben Rechner (etwa ein Reverse-Proxy im selben Compose-Netz) —
// vorher zählte er als zu Hause. Hängt der Container dagegen mit
// macvlan/ipvlan direkt im Heimnetz, ist eth0 das Heimnetz und bleibt es.
func eigeneSicht() netzSicht {
	ifs, err := net.Interfaces()
	if err != nil {
		return netzSicht{eigene: map[string]bool{}}
	}
	var liste []schnittstelle
	for _, ifc := range ifs {
		addrs, err := ifc.Addrs()
		if err != nil {
			continue
		}
		x := schnittstelle{name: ifc.Name}
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok {
				x.netze = append(x.netze, n)
			}
		}
		liste = append(liste, x)
	}
	return sichtAus(liste, imContainer(), schnittstellenArten())
}

// schnittstelle: Name und Adressen (mit Netzmaske) einer Schnittstelle.
type schnittstelle struct {
	name  string
	netze []*net.IPNet
}

// sichtAus baut die Sicht aus den Schnittstellen; arten aus
// schnittstellenArten (nil = unbekannt).
func sichtAus(liste []schnittstelle, container bool, arten map[string]string) netzSicht {
	s := netzSicht{eigene: map[string]bool{}}
	containerBruecken := false
	for _, x := range liste {
		if containerSchnittstelle(x.name) {
			containerBruecken = true
		}
	}
	if container {
		s.hostNetz = containerBruecken
		s.bruecke = !containerBruecken
	}
	for _, x := range liste {
		istContainer := containerSchnittstelle(x.name)
		// Im Bridge-Betrieb ist das eigene Netz ein Container-Netz — ausser
		// die Schnittstelle hängt direkt im Heimnetz (macvlan, ipvlan).
		// Ist die Art unbekannt, gilt lieber „Container-Netz": zu streng
		// heisst Anmelden mit PIN, zu locker Profilliste im Internet.
		eigenesContainerNetz := s.bruecke && x.name != "lo" && !direktImHeimnetz(arten[x.name])
		for _, n := range x.netze {
			if n.IP.IsLoopback() {
				continue
			}
			s.eigene[n.IP.String()] = true
			if (istContainer || eigenesContainerNetz) && !n.IP.IsLinkLocalUnicast() {
				s.containerNetze = append(s.containerNetze, &net.IPNet{IP: n.IP.Mask(n.Mask), Mask: n.Mask})
				continue
			}
			// IPv6-Präfixe des Heimnetzes: nur global, nur ein echtes
			// On-Link-Präfix (/48 bis /128) — nie ::/0 oder ein ganzer
			// Provider-Block —, und nicht im Bridge-Betrieb.
			if !s.bruecke && !istContainer && n.IP.To4() == nil && n.IP.IsGlobalUnicast() &&
				!n.IP.IsPrivate() {
				if eins, bits := n.Mask.Size(); bits == 128 && eins >= 48 && eins < 128 {
					s.heimPraefixe = append(s.heimPraefixe, &net.IPNet{IP: n.IP.Mask(n.Mask), Mask: n.Mask})
				}
			}
		}
	}
	return s
}

// direktImHeimnetz: Schnittstellenarten, mit denen ein Container eine
// eigene Adresse im echten Netz hat.
func direktImHeimnetz(art string) bool {
	switch art {
	case "macvlan", "macvtap", "ipvlan", "ipvtap":
		return true
	}
	return false
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
