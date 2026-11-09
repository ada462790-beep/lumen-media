// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

// Package netz baut HTTP-Clients für Adressen, die jemand eintippt.
//
// Lumen fragt an mehreren Stellen Adressen ab, die nicht fest im Code
// stehen: ein Bild „von einer Adresse", der eigene Emby- oder Jellyfin-
// Server für die Übernahme. Ohne Vorsicht wird daraus ein Werkzeug, mit
// dem man den Server Dinge im Netz abfragen lässt, die er selbst
// erreicht, man selbst aber nicht (SSRF). Darum gilt hier:
//
//   - nur http und https, keine Anmeldedaten in der Adresse;
//   - geprüft wird die IP-Adresse beim Verbinden, nicht der Name — sonst
//     liesse sich die Prüfung mit einem DNS-Eintrag umgehen, der zuerst
//     auf eine harmlose und dann auf eine interne Adresse zeigt;
//   - Weiterleitungen nur auf denselben Host (wo verlangt) und höchstens
//     ein paar;
//   - feste Zeitgrenze und eine Obergrenze für die Antwort.
package netz

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"
)

// Regeln legt fest, was ein Client darf.
type Regeln struct {
	// Heimnetz erlaubt private Adressen (192.168.…, 10.…, fd00::/8) und
	// den Rechner selbst. Für Emby/Jellyfin nötig — die laufen fast immer
	// im selben Netz. Für Bilder aus dem Internet nicht.
	Heimnetz bool
	// NurHeimnetz: ausschliesslich der Rechner selbst und Adressen im
	// Heimnetz (ImHeimnetz) — für die Übernahme aus Emby/Jellyfin. Schliesst
	// Heimnetz ein.
	NurHeimnetz bool
	// GleicherHost: Weiterleitungen nur zum selben Host und Port. Ein
	// Emby-Server leitet nicht auf fremde Rechner weiter; tut er es doch,
	// ist etwas faul.
	GleicherHost bool
	// Zeitlimit für die ganze Anfrage samt Antwort.
	Zeitlimit time.Duration
}

// ErrAdresse heisst: Diese Adresse fragt Lumen nicht ab.
var ErrAdresse = errors.New("diese Adresse ist nicht erlaubt")

// AdressePruefen prüft eine eingegebene Adresse auf ihre Form — die IP
// wird erst beim Verbinden geprüft.
func AdressePruefen(roh string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(roh))
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("%w: keine gültige Adresse", ErrAdresse)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("%w: nur http:// oder https://", ErrAdresse)
	}
	if u.User != nil {
		return nil, fmt.Errorf("%w: Anmeldedaten gehören nicht in die Adresse", ErrAdresse)
	}
	return u, nil
}

// IPErlaubt sagt, ob Lumen sich mit dieser Adresse verbinden darf.
//
// Nie erlaubt sind Adressen, hinter denen kein gewöhnlicher Dienst steht
// (unbestimmt, Multicast, Broadcast, link-lokal) und die bekannten
// Metadaten-Dienste von Cloud-Rechnern (169.254.169.254, fd00:ec2::254,
// 100.100.100.200): Dort liegen Zugangsschlüssel — dorthin soll nie eine
// Anfrage gehen, auch nicht „im Heimnetz".
//
// Im Internet-Modus (heimnetz=false) gilt eine Erlaubnisliste statt einer
// Sperrliste: nur gewöhnliche öffentliche Adressen (IPv4 ausserhalb der
// IANA-Sonderbereiche, IPv6 nur aus 2000::/3 ohne dessen Sonderbereiche).
// Eine Sperrliste übersah früher Schreibweisen wie ::ffff:10.0.0.1,
// ::10.0.0.1 oder 64:ff9b::a9fe:a9fe (NAT64 auf die Metadaten), die
// auf ein Ziel im Heimnetz hinauslaufen.
func IPErlaubt(ip net.IP, heimnetz bool) bool {
	if ip == nil || nieErlaubt(ip) {
		return false
	}
	if heimnetz {
		return true
	}
	return oeffentlich(ip)
}

// ImHeimnetz: der Rechner selbst, private IPv4-Netze, Carrier-Grade-NAT
// (100.64.0.0/10 — darüber laufen etwa Tailscale-Netze) und lokale
// IPv6-Adressen (fc00::/7). Für die Übernahme aus Emby/Jellyfin: Die
// laufen im Heimnetz, und nur dorthin darf Lumen fragen — sonst wäre die
// Übernahme ein Werkzeug, um beliebige Rechner im Internet abzutasten.
func ImHeimnetz(ip net.IP) bool {
	if ip == nil || nieErlaubt(ip) {
		return false
	}
	if v4 := ip.To4(); v4 != nil {
		return v4.IsLoopback() || v4.IsPrivate() || in(v4, cgnat)
	}
	return ip.IsLoopback() || ip.IsPrivate()
}

func netze(cidrs ...string) []*net.IPNet {
	out := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			panic(err)
		}
		out = append(out, n)
	}
	return out
}

func in(ip net.IP, liste []*net.IPNet) bool {
	for _, n := range liste {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

var (
	cgnat = netze("100.64.0.0/10")
	// Metadaten-Dienste der Cloud-Anbieter, die nicht schon link-lokal
	// sind (169.254.169.254 fällt unter link-lokal).
	metadaten = []net.IP{net.ParseIP("fd00:ec2::254"), net.ParseIP("100.100.100.200")}

	// IPv4-Sonderbereiche (IANA Special-Purpose Address Registry), die
	// im Internet-Modus nie ein Ziel sind.
	v4Sonder = netze(
		"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16",
		"172.16.0.0/12", "192.0.0.0/24", "192.0.2.0/24", "192.31.196.0/24", "192.52.193.0/24",
		"192.88.99.0/24", "192.168.0.0/16", "192.175.48.0/24", "198.18.0.0/15",
		"198.51.100.0/24", "203.0.113.0/24", "224.0.0.0/4", "240.0.0.0/4",
	)
	// IPv6: nur 2000::/3 ist gewöhnlicher öffentlicher Raum, und darin
	// gibt es Sonderbereiche: Protokoll-Zuweisungen samt Teredo
	// (2001::/23), Dokumentation (2001:db8::/32, 3fff::/20), 6to4
	// (2002::/16 — trägt eine IPv4-Adresse in sich), SRv6 (5f00::/16).
	v6Global = netze("2000::/3")
	v6Sonder = netze("2001::/23", "2001:db8::/32", "2002::/16", "3fff::/20", "5f00::/16")
	// NAT64 (64:ff9b::/96): trägt eine IPv4-Adresse in den letzten vier
	// Bytes und zählt wie diese. 64:ff9b:1::/48 ist für lokale Übersetzer.
	nat64      = netze("64:ff9b::/96")
	nat64Lokal = netze("64:ff9b:1::/48")
	siteLokal  = netze("fec0::/10")
)

// nieErlaubt: in keinem Modus ein Ziel.
func nieErlaubt(ip net.IP) bool {
	// Eine IPv4-Adresse in IPv6-Schreibweise zählt wie die IPv4-Adresse:
	// ::ffff:a.b.c.d (abgebildet), ::a.b.c.d (veraltet „kompatibel") und
	// 64:ff9b::a.b.c.d (NAT64).
	if v4 := eingebettet(ip); v4 != nil {
		ip = v4
	}
	if ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() || ip.Equal(net.IPv4bcast) {
		return true
	}
	for _, m := range metadaten {
		if ip.Equal(m) {
			return true
		}
	}
	if v4 := ip.To4(); v4 != nil && v4[0] == 0 {
		return true // 0.0.0.0/8: „dieses Netz", landet oft beim Rechner selbst
	}
	if ip.To4() == nil && in(ip, siteLokal) {
		return true // site-lokal, veraltet — niemand braucht es, Router deuten es verschieden
	}
	return false
}

// eingebettet holt eine IPv4-Adresse aus einer IPv6-Schreibweise, die
// eine trägt; sonst nil.
func eingebettet(ip net.IP) net.IP {
	if len(ip) != net.IPv6len {
		return nil
	}
	if v4 := ip.To4(); v4 != nil {
		return v4 // ::ffff:a.b.c.d
	}
	// ::a.b.c.d — die ersten zwölf Bytes null, ausser :: und ::1 selbst.
	null := true
	for _, b := range ip[:12] {
		if b != 0 {
			null = false
			break
		}
	}
	if null && !ip.Equal(net.IPv6unspecified) && !ip.Equal(net.IPv6loopback) {
		return net.IPv4(ip[12], ip[13], ip[14], ip[15]).To4()
	}
	if in(ip, nat64) {
		return net.IPv4(ip[12], ip[13], ip[14], ip[15]).To4()
	}
	return nil
}

// oeffentlich: eine gewöhnliche Adresse im Internet.
func oeffentlich(ip net.IP) bool {
	if v4 := eingebettet(ip); v4 != nil {
		ip = v4
	}
	if v4 := ip.To4(); v4 != nil {
		return !in(v4, v4Sonder)
	}
	if in(ip, nat64Lokal) {
		return false
	}
	return in(ip, v6Global) && !in(ip, v6Sonder)
}

// Client baut einen HTTP-Client nach den Regeln.
func Client(r Regeln) *http.Client {
	if r.Zeitlimit <= 0 {
		r.Zeitlimit = 20 * time.Second
	}
	dialer := &net.Dialer{
		Timeout: 8 * time.Second,
		// Control läuft für jede Verbindung mit der schon aufgelösten
		// Adresse — auch nach einer Weiterleitung.
		Control: func(network, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return ErrAdresse
			}
			ip := net.ParseIP(host)
			if r.NurHeimnetz && !ImHeimnetz(ip) {
				return fmt.Errorf("%w (%s)", ErrAdresse, beschreibe(ip))
			}
			if !IPErlaubt(ip, r.Heimnetz || r.NurHeimnetz) {
				return fmt.Errorf("%w (%s)", ErrAdresse, beschreibe(net.ParseIP(host)))
			}
			return nil
		},
	}
	tr := &http.Transport{
		// Kein Proxy aus der Umgebung: Die Prüfung oben gälte sonst dem
		// Proxy statt dem Ziel.
		Proxy:                 nil,
		DialContext:           dialer.DialContext,
		TLSHandshakeTimeout:   8 * time.Second,
		ResponseHeaderTimeout: r.Zeitlimit,
		MaxIdleConns:          4,
		IdleConnTimeout:       30 * time.Second,
	}
	return &http.Client{
		Transport: tr,
		Timeout:   r.Zeitlimit,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return errors.New("zu viele Weiterleitungen")
			}
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return fmt.Errorf("%w: Weiterleitung auf %s", ErrAdresse, req.URL.Scheme)
			}
			if r.GleicherHost && !strings.EqualFold(req.URL.Host, via[0].URL.Host) {
				return fmt.Errorf("%w: Weiterleitung auf einen anderen Rechner", ErrAdresse)
			}
			return nil
		},
	}
}

func beschreibe(ip net.IP) string {
	switch {
	case ip == nil:
		return "unbekannt"
	case ip.IsLoopback():
		return "dieser Rechner"
	case ip.IsPrivate():
		return "Heimnetz"
	case ip.IsLinkLocalUnicast():
		return "link-lokal"
	}
	return "gesperrter Bereich"
}

// ErrZuGross: Die Antwort war grösser als erlaubt.
var ErrZuGross = errors.New("die Antwort ist zu gross")

// Lesen liest höchstens max Bytes. Ist die Antwort grösser, kommt
// ErrZuGross — abgeschnittene Daten sollen nicht als vollständig gelten.
func Lesen(r io.Reader, max int64) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > max {
		return nil, ErrZuGross
	}
	return b, nil
}

// Ohne gibt einen Kontext zurück, der nicht mit der Anfrage endet, aber
// eine eigene Zeitgrenze hat — für Arbeit, die im Hintergrund weiterläuft.
func Ohne(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), d)
}
