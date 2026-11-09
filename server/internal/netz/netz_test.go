// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package netz

import (
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestIPErlaubt(t *testing.T) {
	for _, f := range []struct {
		ip             string
		heim, internet bool
	}{
		{"192.168.1.20", true, false},
		{"10.0.0.5", true, false},
		{"127.0.0.1", true, false},
		{"::1", true, false},
		{"fd00::1", true, false},
		{"100.64.1.1", true, false},
		{"169.254.169.254", false, false},
		{"fe80::1", false, false},
		{"0.0.0.0", false, false},
		{"224.0.0.1", false, false},
		{"255.255.255.255", false, false},
		{"93.184.216.34", true, true},
		{"2606:4700::1111", true, true},
		// Metadaten-Dienste: nie, auch nicht im Heimnetz.
		{"fd00:ec2::254", false, false},
		{"100.100.100.200", false, false},
		{"::ffff:169.254.169.254", false, false},
		{"64:ff9b::a9fe:a9fe", false, false}, // NAT64 auf 169.254.169.254
		{"::169.254.169.254", false, false},  // IPv4-kompatibel
		{"0.1.2.3", false, false},
		{"fec0::1", false, false},
		// IANA-Sonderbereiche: im Heimnetz-Modus egal, ins Internet nie.
		{"192.0.0.8", true, false},
		{"198.18.0.1", true, false},
		{"192.0.2.1", true, false},
		{"203.0.113.9", true, false},
		{"240.0.0.1", true, false},
		{"2001:db8::1", true, false},
		{"2001::1", true, false},           // Teredo
		{"2002:a00:1::1", true, false},     // 6to4 mit 10.0.0.1
		{"64:ff9b::a00:1", true, false},    // NAT64 auf 10.0.0.1
		{"64:ff9b::5db8:d822", true, true}, // NAT64 auf 93.184.216.34
		{"64:ff9b:1::1", true, false},
		{"::ffff:10.0.0.1", true, false},
		{"::10.0.0.1", true, false},
		{"::ffff:93.184.216.34", true, true},
		{"fc00::1", true, false},
		{"100::1", true, false}, // nicht in 2000::/3
	} {
		ip := net.ParseIP(f.ip)
		if got := IPErlaubt(ip, true); got != f.heim {
			t.Errorf("%s im Heimnetz-Modus: %v, erwartet %v", f.ip, got, f.heim)
		}
		if got := IPErlaubt(ip, false); got != f.internet {
			t.Errorf("%s im Internet-Modus: %v, erwartet %v", f.ip, got, f.internet)
		}
	}
}

func TestAdressePruefen(t *testing.T) {
	for _, roh := range []string{"ftp://x", "file:///etc/passwd", "http://nutzer:pw@nas:8920", "nas:8920", "", "gopher://a"} {
		if _, err := AdressePruefen(roh); err == nil {
			t.Errorf("%q wurde angenommen", roh)
		}
	}
	if u, err := AdressePruefen(" http://192.168.1.5:8920/emby "); err != nil || u.Host != "192.168.1.5:8920" {
		t.Fatalf("gültige Adresse abgelehnt: %v", err)
	}
}

// Ein Server im Heimnetz darf nicht auf einen anderen Rechner weiterleiten.
func TestWeiterleitungNurGleicherHost(t *testing.T) {
	ziel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("geheim"))
	}))
	defer ziel.Close()
	quelle := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// localhost statt 127.0.0.1: anderer Host, gleiche Maschine.
		http.Redirect(w, r, strings.Replace(ziel.URL, "127.0.0.1", "localhost", 1), http.StatusFound)
	}))
	defer quelle.Close()

	c := Client(Regeln{Heimnetz: true, GleicherHost: true, Zeitlimit: 5 * time.Second})
	_, err := c.Get(quelle.URL)
	if err == nil || !errors.Is(err, ErrAdresse) {
		t.Fatalf("Weiterleitung auf anderen Host ging durch: %v", err)
	}
}

// Ohne Heimnetz-Erlaubnis kommt keine Verbindung zum eigenen Rechner zustande.
func TestInternetModusSperrtEigenenRechner(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	c := Client(Regeln{Zeitlimit: 5 * time.Second})
	if _, err := c.Get(srv.URL); err == nil || !errors.Is(err, ErrAdresse) {
		t.Fatalf("Verbindung zu 127.0.0.1 im Internet-Modus: %v", err)
	}
	c = Client(Regeln{Heimnetz: true, Zeitlimit: 5 * time.Second})
	if resp, err := c.Get(srv.URL); err != nil {
		t.Fatalf("im Heimnetz-Modus abgelehnt: %v", err)
	} else {
		resp.Body.Close()
	}
}

func TestLesenGrenze(t *testing.T) {
	if _, err := Lesen(strings.NewReader("12345"), 4); !errors.Is(err, ErrZuGross) {
		t.Fatalf("zu grosse Antwort nicht erkannt: %v", err)
	}
	if b, err := Lesen(strings.NewReader("1234"), 4); err != nil || string(b) != "1234" {
		t.Fatalf("passende Antwort: %q %v", b, err)
	}
}

// ImHeimnetz: nur der Rechner selbst und private Netze — für die
// Übernahme aus Emby. Öffentliche Adressen und die Metadaten nie.
func TestImHeimnetz(t *testing.T) {
	for ip, soll := range map[string]bool{
		"192.168.1.20": true, "10.1.2.3": true, "172.16.0.9": true, "127.0.0.1": true, "::1": true,
		"fd12::5": true, "100.70.1.2": true, "::ffff:192.168.1.1": true,
		"93.184.216.34": false, "2606:4700::1111": false, "100.100.100.200": false,
		"169.254.169.254": false, "fd00:ec2::254": false, "0.0.0.0": false, "8.8.8.8": false,
		"64:ff9b::a00:1": false,
	} {
		if got := ImHeimnetz(net.ParseIP(ip)); got != soll {
			t.Errorf("%s: %v, erwartet %v", ip, got, soll)
		}
	}
}

// Mit NurHeimnetz kommt keine Verbindung zu einer öffentlichen Adresse
// zustande — geprüft beim Verbinden, nicht am Namen.
func TestNurHeimnetzSperrtInternet(t *testing.T) {
	c := Client(Regeln{NurHeimnetz: true, Zeitlimit: 2 * time.Second})
	_, err := c.Get("http://93.184.216.34:9/")
	if err == nil || !errors.Is(err, ErrAdresse) {
		t.Fatalf("öffentliche Adresse nicht gesperrt: %v", err)
	}
	ziel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer ziel.Close()
	if resp, err := c.Get(ziel.URL); err != nil {
		t.Fatalf("der Rechner selbst gesperrt: %v", err)
	} else {
		resp.Body.Close()
	}
}
