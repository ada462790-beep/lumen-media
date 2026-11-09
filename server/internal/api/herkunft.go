// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

// Woher eine Anfrage kommt: von zu Hause oder von unterwegs.
//
// Zu Hause ist eine Anfrage nur, wenn alles dafür spricht: Sie kommt
// direkt (nicht über den Vermittler) von einer Adresse im Heimnetz, und
// kein Reverse-Proxy hat sie weitergereicht. Alles andere gilt als „von
// unterwegs" — lieber einmal zu streng als ein Kinderprofil im Internet.
//
// Von unterwegs gilt:
//   - keine Liste der Profile (sie verriete die Namen im Haushalt),
//   - anmelden nur mit Profilname und PIN oder Passwort, mit Code vom
//     Fernseher oder mit einer Einladung — ein Profil ohne PIN lässt sich
//     von aussen nicht öffnen,
//   - kein Einrichten des Servers.
//
// Wer schon angemeldet ist, merkt davon nichts: Ein Gerät, das sich zu
// Hause angemeldet hat, funktioniert unterwegs genauso.

import (
	"context"
	"net"
	"net/http"
	"os"
	"strings"
)

type ctxHerkunft int

const ctxVonAussen ctxHerkunft = 0

// VonAussen markiert alle Anfragen, die über diesen Handler laufen, als
// „von unterwegs". Der Eingang für den Vermittler läuft hier durch.
func VonAussen(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxVonAussen, true)))
	})
}

// vonAussen sagt, ob eine Anfrage von unterwegs kommt.
func vonAussen(r *http.Request) bool {
	if v, _ := r.Context().Value(ctxVonAussen).(bool); v {
		return true
	}
	// Ein Reverse-Proxy davor: Was er weiterreicht, kann von überall
	// kommen. Seinen Angaben, woher, glauben wir nicht.
	for _, k := range []string{"X-Forwarded-For", "Forwarded", "X-Real-Ip", "X-Forwarded-Host", "Cf-Connecting-Ip"} {
		if r.Header.Get(k) != "" {
			return true
		}
	}
	// Private Adresse allein genügt nicht: Gateways der eigenen Netze und
	// alles ausserhalb von LUMEN_HEIMNETZ zählen als unterwegs (heimnetz.go).
	return !imHeimnetz(absenderIP(r))
}

func absenderIP(r *http.Request) net.IP {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return net.ParseIP(host)
}

// quelle ist der Schlüssel des Absenders für die Bremse. Über den
// Vermittler kommen alle mit derselben Adresse an; dann zählt „unterwegs"
// als ein Absender.
func quelle(r *http.Request) string {
	if v, _ := r.Context().Value(ctxVonAussen).(bool); v {
		return "quelle:unterwegs"
	}
	if ip := absenderIP(r); ip != nil {
		return "quelle:" + ip.String()
	}
	return "quelle:?"
}

// ---------------------------------------------------------------- Hostnamen

// Gegen DNS-Rebinding: Eine fremde Webseite kann ihren Namen auf die
// Adresse des NAS umbiegen und dann Anfragen stellen, als käme sie von
// Lumen selbst. Am Hostnamen erkennt man sie trotzdem. Für die Wege ohne
// Anmeldung (Einrichten, Profile, Anmelden, Einladung) muss der Name darum
// einer sein, den ein Heimnetz benutzt — eine IP-Adresse, „localhost", ein
// Name ohne Punkt, ein Name im Heimnetz (.local, .lan, .home …) — oder
// einer, der ausdrücklich erlaubt ist (LUMEN_HOSTS, Name des Zertifikats,
// Adresse des Vermittlers).

var heimnetzEndungen = []string{".local", ".lan", ".home", ".internal", ".home.arpa",
	".localdomain", ".fritz.box", ".box", ".intranet", ".private"}

// SetzeErlaubteHosts trägt weitere Namen ein, unter denen Lumen erreichbar
// ist (etwa der Vermittler oder der Name im Zertifikat).
func (s *Server) SetzeErlaubteHosts(namen ...string) {
	for _, n := range namen {
		n = strings.ToLower(strings.TrimSpace(n))
		if h, _, err := net.SplitHostPort(n); err == nil {
			n = h
		}
		if n != "" {
			s.erlaubteHosts = append(s.erlaubteHosts, n)
		}
	}
}

func (s *Server) hostErlaubt(r *http.Request) bool {
	host := strings.ToLower(r.Host)
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.TrimSuffix(strings.Trim(host, "[]"), ".")
	if host == "" {
		return false
	}
	if net.ParseIP(host) != nil || host == "localhost" || !strings.Contains(host, ".") {
		return true
	}
	for _, e := range heimnetzEndungen {
		if strings.HasSuffix(host, e) {
			return true
		}
	}
	for _, n := range s.erlaubteHosts {
		if host == n {
			return true
		}
	}
	for _, n := range strings.Split(os.Getenv("LUMEN_HOSTS"), ",") {
		if n = strings.ToLower(strings.TrimSpace(n)); n != "" && host == n {
			return true
		}
	}
	return false
}

// nurBekannterHost schützt die Wege ohne Anmeldung vor DNS-Rebinding.
func (s *Server) nurBekannterHost(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.hostErlaubt(r) {
			fail(w, http.StatusMisdirectedRequest,
				"Lumen kennt den Namen „"+r.Host+"“ nicht. Wer Lumen unter einem eigenen Namen "+
					"erreicht, trägt ihn in LUMEN_HOSTS ein.")
			return
		}
		h(w, r)
	}
}

// zuHauseOffen: zu Hause und unter einem bekannten Namen ohne Anmeldung,
// sonst nur angemeldet.
func (s *Server) zuHauseOffen(h http.HandlerFunc) http.Handler {
	mitAnmeldung := s.requireAuth(h)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !vonAussen(r) && s.hostErlaubt(r) {
			h(w, r)
			return
		}
		mitAnmeldung.ServeHTTP(w, r)
	})
}
