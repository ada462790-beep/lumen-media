// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

// Package cert legt bei Bedarf ein eigenes Zertifikat an.
//
// Ein Heimserver hat selten einen Namen, für den eine öffentliche Stelle
// ein Zertifikat ausstellen würde — er heisst „nas" und steht unter
// 192.168.1.20. Also stellt Lumen sich selbst eines aus.
//
// Das ist weniger wert als ein öffentliches, aber nicht wertlos: die
// Verbindung ist genauso verschlüsselt. Was fehlt, ist der Beweis, dass
// am anderen Ende der Richtige sitzt — und den liefert hier der
// Fingerabdruck, den man einmal vergleicht. Die Apps merken ihn sich; im
// Browser klickt man einmal durch die Warnung.
//
// Wer es warnungsfrei will, legt ein richtiges Zertifikat daneben; Lumen
// nimmt dann das.
package cert

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Gueltigkeit: zehn Jahre. Ein selbst ausgestelltes Zertifikat kurz
// laufen zu lassen, bringt nichts — es gibt keine Stelle, die es
// zurückziehen könnte, und ein Ablauf bedeutete nur, dass alle den
// Fingerabdruck neu vergleichen müssten.
const Gueltigkeit = 10 * 365 * 24 * time.Hour

// Herkunft sagt, woher das Zertifikat kommt.
//
// Zwei verschiedene Fragen, die anfangs beide an einem einzigen bool
// hingen — und prompt verwechselt wurden: ein wiedergefundenes eigenes
// Zertifikat meldete sich als frisch angelegt, und der Vermittler erklärte
// bei jedem Neustart, er habe eines erzeugt.
type Herkunft struct {
	// SelbstAusgestellt: nicht von einer öffentlichen Stelle. Die Apps
	// brauchen dann den Fingerabdruck.
	SelbstAusgestellt bool
	// NeuAngelegt: gerade eben erzeugt, nicht wiedergefunden. Nur dann
	// lohnt es, dem Betreiber den Fingerabdruck gross hinzuschreiben.
	NeuAngelegt bool
}

// Laden holt das Zertifikatspaar aus certPfad/keyPfad. Sind beide leer,
// wird eines im Ordner angelegt und beim nächsten Start wiederverwendet.
//
// namen sind die Rechnernamen und Adressen, unter denen der Server
// erreichbar sein soll.
func Laden(certPfad, keyPfad, ordner string, namen []string) (tls.Certificate, Herkunft, error) {
	if certPfad != "" || keyPfad != "" {
		if certPfad == "" || keyPfad == "" {
			return tls.Certificate{}, Herkunft{},
				fmt.Errorf("zertifikat und schlüssel gehören zusammen — eines fehlt")
		}
		c, err := tls.LoadX509KeyPair(certPfad, keyPfad)
		return c, Herkunft{}, err
	}

	certPfad = filepath.Join(ordner, "eigenes-zertifikat.pem")
	keyPfad = filepath.Join(ordner, "eigener-schluessel.pem")

	if c, err := tls.LoadX509KeyPair(certPfad, keyPfad); err == nil && nochGueltig(c) {
		return c, Herkunft{SelbstAusgestellt: true}, nil
	}
	return Anlegen(certPfad, keyPfad, namen)
}

func nochGueltig(c tls.Certificate) bool {
	if len(c.Certificate) == 0 {
		return false
	}
	x, err := x509.ParseCertificate(c.Certificate[0])
	if err != nil {
		return false
	}
	// Einen Monat vor Ablauf lieber neu — sonst steht man an dem Tag da,
	// an dem nichts mehr geht.
	return time.Now().Before(x.NotAfter.Add(-30 * 24 * time.Hour))
}

// Anlegen schreibt ein frisches Paar und gibt es zurück.
func Anlegen(certPfad, keyPfad string, namen []string) (tls.Certificate, Herkunft, error) {
	return anlegenMit(certPfad, keyPfad, namen, Gueltigkeit)
}

// anlegenMit ist dasselbe mit frei wählbarer Laufzeit. Nur die Tests
// brauchen das: um zu prüfen, dass ein bald ablaufendes Zertifikat
// rechtzeitig erneuert wird, muss man eines herstellen können, das bald
// abläuft.
func anlegenMit(certPfad, keyPfad string, namen []string,
	gueltig time.Duration) (tls.Certificate, Herkunft, error) {

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, Herkunft{}, err
	}

	seriennummer, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, Herkunft{}, err
	}

	vorlage := x509.Certificate{
		SerialNumber:          seriennummer,
		Subject:               pkix.Name{CommonName: "Lumen", Organization: []string{"Lumen"}},
		NotBefore:             time.Now().Add(-time.Hour), // falls die Uhr leicht nachgeht
		NotAfter:              time.Now().Add(gueltig),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	for _, n := range namen {
		if ip := net.ParseIP(n); ip != nil {
			vorlage.IPAddresses = append(vorlage.IPAddresses, ip)
		} else if n != "" {
			vorlage.DNSNames = append(vorlage.DNSNames, n)
		}
	}
	if len(vorlage.DNSNames) == 0 && len(vorlage.IPAddresses) == 0 {
		vorlage.DNSNames = []string{"localhost"}
	}

	roh, err := x509.CreateCertificate(rand.Reader, &vorlage, &vorlage, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, Herkunft{}, err
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: roh})
	keyRoh, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return tls.Certificate{}, Herkunft{}, err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyRoh})

	if err := os.MkdirAll(filepath.Dir(certPfad), 0o755); err != nil {
		return tls.Certificate{}, Herkunft{}, err
	}
	if err := os.WriteFile(certPfad, certPEM, 0o644); err != nil {
		return tls.Certificate{}, Herkunft{}, err
	}
	// Der private Schlüssel geht niemanden sonst etwas an — auch keinen
	// anderen Benutzer auf derselben Maschine.
	if err := os.WriteFile(keyPfad, keyPEM, 0o600); err != nil {
		return tls.Certificate{}, Herkunft{}, err
	}

	c, err := tls.X509KeyPair(certPEM, keyPEM)
	return c, Herkunft{SelbstAusgestellt: true, NeuAngelegt: true}, err
}

// Fingerabdruck ist der SHA-256 des Zertifikats in Hex. Das ist die Zahl,
// die man einmal vergleicht — und danach nie wieder.
func Fingerabdruck(c tls.Certificate) string {
	if len(c.Certificate) == 0 {
		return ""
	}
	s := sha256.Sum256(c.Certificate[0])
	return hex.EncodeToString(s[:])
}

// Lesbar bricht den Fingerabdruck in Zweiergruppen, damit man ihn
// vorlesen und vergleichen kann, ohne die Stelle zu verlieren.
func Lesbar(fp string) string {
	var b []byte
	for i := 0; i < len(fp); i += 2 {
		if i > 0 {
			b = append(b, ':')
		}
		b = append(b, fp[i:min(i+2, len(fp))]...)
	}
	return string(b)
}

// Nachlader liefert ein mitgebrachtes Zertifikat und liest es neu, sobald
// sich die Dateien ändern. Let's Encrypt erneuert alle 60 bis 90 Tage;
// las Lumen das Zertifikat nur beim Start, lief es nach der Erneuerung
// mit dem alten weiter, bis es ablief — und dann ging nichts mehr.
type Nachlader struct {
	certPfad, keyPfad string

	mu       sync.Mutex
	aktuell  *tls.Certificate
	stand    time.Time // Änderungszeit der Dateien beim letzten Lesen
	geprueft time.Time
}

// NeuerNachlader beginnt mit einem schon geladenen Zertifikat.
func NeuerNachlader(certPfad, keyPfad string, start tls.Certificate) *Nachlader {
	n := &Nachlader{certPfad: certPfad, keyPfad: keyPfad, aktuell: &start, geprueft: time.Now()}
	n.stand = n.aenderungszeit()
	return n
}

func (n *Nachlader) aenderungszeit() time.Time {
	var neuste time.Time
	for _, p := range []string{n.certPfad, n.keyPfad} {
		if fi, err := os.Stat(p); err == nil && fi.ModTime().After(neuste) {
			neuste = fi.ModTime()
		}
	}
	return neuste
}

// Zertifikat passt zu tls.Config.GetCertificate. Höchstens einmal je
// Minute wird nachgesehen; ein halb geschriebenes Paar (Lesen scheitert)
// lässt das alte stehen.
func (n *Nachlader) Zertifikat(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if time.Since(n.geprueft) >= time.Minute {
		n.geprueft = time.Now()
		if t := n.aenderungszeit(); !t.Equal(n.stand) {
			if c, err := tls.LoadX509KeyPair(n.certPfad, n.keyPfad); err == nil {
				n.aktuell, n.stand = &c, t
			}
		}
	}
	return n.aktuell, nil
}
