// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package cert

import (
	"crypto/x509"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEigenesZertifikatBleibtUeberNeustartsGleich(t *testing.T) {
	ordner := t.TempDir()

	erst, woher, err := Laden("", "", ordner, []string{"nas", "192.168.1.20"})
	if err != nil {
		t.Fatal(err)
	}
	if !woher.NeuAngelegt || !woher.SelbstAusgestellt {
		t.Fatalf("beim ersten Mal muss eines angelegt werden, bekommen %+v", woher)
	}

	zweit, nochmal, err := Laden("", "", ordner, []string{"nas", "192.168.1.20"})
	if err != nil {
		t.Fatal(err)
	}
	if nochmal.NeuAngelegt {
		t.Error("beim zweiten Mal darf keines angelegt werden")
	}
	if !nochmal.SelbstAusgestellt {
		t.Error("ein wiedergefundenes eigenes Zertifikat ist immer noch ein eigenes")
	}

	// Das ist der Punkt: der Fingerabdruck steht in den Apps und in der
	// Verwaltung. Änderte er sich bei jedem Neustart, müsste ihn jeder im
	// Haushalt jedes Mal neu eintragen.
	if Fingerabdruck(erst) != Fingerabdruck(zweit) {
		t.Error("der Fingerabdruck hat sich über einen Neustart geändert")
	}
}

func TestZertifikatGiltFuerDieAngegebenenNamen(t *testing.T) {
	ordner := t.TempDir()
	paar, _, err := Anlegen(ordner+"/c.pem", ordner+"/k.pem",
		[]string{"nas", "lumen.local", "192.168.1.20", "127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	x, err := x509.ParseCertificate(paar.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"nas", "lumen.local"} {
		if !hat(x.DNSNames, name) {
			t.Errorf("%q fehlt in den Namen: %v", name, x.DNSNames)
		}
	}
	for _, roh := range []string{"192.168.1.20", "127.0.0.1"} {
		if !hatIP(x.IPAddresses, roh) {
			t.Errorf("%q fehlt in den Adressen: %v", roh, x.IPAddresses)
		}
	}
	// Wer die Adresse eintippt, soll kein zweites Zertifikat brauchen.
	if err := x.VerifyHostname("192.168.1.20"); err != nil {
		t.Errorf("die eigene Adresse wird nicht anerkannt: %v", err)
	}
}

func TestSchluesselLiegtNichtOffen(t *testing.T) {
	ordner := t.TempDir()
	keyPfad := filepath.Join(ordner, "k.pem")
	if _, _, err := Anlegen(filepath.Join(ordner, "c.pem"), keyPfad, nil); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(keyPfad)
	if err != nil {
		t.Fatal(err)
	}
	// Der private Schlüssel geht keinen anderen Benutzer auf derselben
	// Maschine etwas an.
	if mode := st.Mode().Perm(); mode != 0o600 {
		t.Errorf("der Schlüssel steht auf %o, erwartet 600", mode)
	}
}

func TestAblaufendesZertifikatWirdErneuert(t *testing.T) {
	ordner := t.TempDir()
	certPfad := filepath.Join(ordner, "eigenes-zertifikat.pem")
	keyPfad := filepath.Join(ordner, "eigener-schluessel.pem")

	// Ein Zertifikat, das in zwei Wochen abläuft. Das Erneuern muss
	// vorher greifen — sonst steht man an dem Tag da, an dem nichts mehr
	// geht, und niemand weiss warum.
	bald, _, err := anlegenMit(certPfad, keyPfad, []string{"nas"}, 14*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if nochGueltig(bald) {
		t.Fatal("ein Zertifikat, das in zwei Wochen abläuft, gilt noch als gut")
	}

	// Laden muss das bemerken und ein neues anlegen.
	neu, woher, err := Laden("", "", ordner, []string{"nas"})
	if err != nil {
		t.Fatal(err)
	}
	if !woher.NeuAngelegt {
		t.Error("das ablaufende Zertifikat wurde nicht erneuert")
	}
	if Fingerabdruck(neu) == Fingerabdruck(bald) {
		t.Error("es kam dasselbe Zertifikat zurück")
	}
	if !nochGueltig(neu) {
		t.Error("das frische Zertifikat gilt sofort als ablaufend")
	}
}

func TestFingerabdruckIstEinSHA256InHex(t *testing.T) {
	ordner := t.TempDir()
	paar, _, err := Anlegen(ordner+"/c.pem", ordner+"/k.pem", nil)
	if err != nil {
		t.Fatal(err)
	}
	fp := Fingerabdruck(paar)
	if len(fp) != 64 {
		t.Fatalf("Länge %d, erwartet 64: %q", len(fp), fp)
	}
	if strings.ToLower(fp) != fp {
		t.Error("der Fingerabdruck muss klein geschrieben sein — die Apps "+
			"vergleichen ihn klein", fp)
	}
	for _, r := range fp {
		if !strings.ContainsRune("0123456789abcdef", r) {
			t.Fatalf("kein Hex: %q", fp)
		}
	}

	// Lesbar macht daraus etwas, das man vorlesen kann, ohne die Stelle
	// zu verlieren — und beides muss ineinander überführbar bleiben.
	lesbar := Lesbar(fp)
	if strings.Count(lesbar, ":") != 31 {
		t.Errorf("erwartet 31 Doppelpunkte, bekommen %d: %s",
			strings.Count(lesbar, ":"), lesbar)
	}
	if strings.ReplaceAll(lesbar, ":", "") != fp {
		t.Error("aus der lesbaren Fassung lässt sich der Fingerabdruck nicht zurückgewinnen")
	}
}

func TestZertifikatOhneSchluesselWirdAbgelehnt(t *testing.T) {
	// Nur eines von beiden anzugeben ist immer ein Versehen. Still auf das
	// selbst ausgestellte zurückzufallen wäre schlimmer als ein Fehler:
	// der Betreiber glaubte, sein Zertifikat sei in Benutzung.
	if _, _, err := Laden("/pfad/zu/cert.pem", "", t.TempDir(), nil); err == nil {
		t.Error("ein Zertifikat ohne Schlüssel wurde angenommen")
	}
	if _, _, err := Laden("", "/pfad/zu/key.pem", t.TempDir(), nil); err == nil {
		t.Error("ein Schlüssel ohne Zertifikat wurde angenommen")
	}
}

// ---------------------------------------------------------------- Hilfen

func hat(liste []string, was string) bool {
	for _, s := range liste {
		if s == was {
			return true
		}
	}
	return false
}

func hatIP(liste []net.IP, was string) bool {
	ziel := net.ParseIP(was)
	for _, ip := range liste {
		if ip.Equal(ziel) {
			return true
		}
	}
	return false
}

// Ein mitgebrachtes Zertifikat, das erneuert wird, gilt ohne Neustart.
func TestNachladerLiestErneuertesZertifikat(t *testing.T) {
	dir := t.TempDir()
	c, k := filepath.Join(dir, "c.pem"), filepath.Join(dir, "k.pem")
	alt, _, err := anlegenMit(c, k, []string{"nas"}, 90*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	n := NeuerNachlader(c, k, alt)
	_, _, _ = anlegenMit(c, k, []string{"nas"}, 90*24*time.Hour)
	spaeter := time.Now().Add(time.Hour)
	_ = os.Chtimes(c, spaeter, spaeter)
	n.geprueft = time.Now().Add(-2 * time.Minute)
	neu, _ := n.Zertifikat(nil)
	if Fingerabdruck(*neu) == Fingerabdruck(alt) {
		t.Fatal("das erneuerte Zertifikat wurde nicht gelesen")
	}
}
