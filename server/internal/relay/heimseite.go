// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package relay

import (
	"bufio"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"
)

// Heimseite hält die Leitung zum Vermittler und baut auf dessen Zuruf hin
// den Weg zu Lumen auf.
//
// Das Wählen geht immer von hier aus. Darum braucht der Router keine
// Portfreigabe: für ihn ist das eine ganz gewöhnliche ausgehende
// Verbindung, wie sie jeder Browser aufbaut.
type Heimseite struct {
	// Vermittler ist Rechner und Port der Gegenstelle, etwa
	// "meinserver.example:8443".
	Vermittler string

	// Geheimnis muss auf beiden Seiten dasselbe sein.
	Geheimnis string

	// Ziel ist, wohin die Daten im Heimnetz gehen — normalerweise Lumen
	// selbst, also "127.0.0.1:8097".
	Ziel string

	// Fingerabdruck pinnt das Zertifikat des Vermittlers (SHA-256 des
	// Zertifikats, in Hex). Leer heisst: nach den üblichen Regeln prüfen,
	// also gegen die Wurzelzertifikate des Systems.
	//
	// Für einen selbst aufgesetzten Vermittler ist das Pinnen der kürzere
	// Weg: er braucht dann kein Zertifikat von einer Stelle, der die ganze
	// Welt vertraut — es genügt, dass dieser eine Heimserver es kennt.
	Fingerabdruck string

	// EigenerFingerabdruck ist der des eigenen TLS-Zertifikats. Er wird
	// nur weitergereicht, damit die Verwaltung ihn anzeigen kann.
	EigenerFingerabdruck string

	// MaxDatenwege begrenzt, wie viele Datenwege gleichzeitig offen sind.
	// Jeder kostet hier zwei Verbindungen — eine zum Vermittler, eine zu
	// Lumen. Der Vermittler hat eigene Grenzen, aber auf die allein soll
	// sich der Heimserver nicht verlassen: ein Vermittler, der aus dem
	// Tritt ist oder dem jemand anderes die Leitung abgenommen hat, könnte
	// sonst mit OEFFNE-Zeilen beliebig viele Verbindungen aus dem Heimnetz
	// heraus auslösen. Null heisst: VorgabeMaxDatenwege.
	MaxDatenwege int

	Log *slog.Logger

	mu      sync.Mutex
	zustand Zustand

	plaetzeEinmal sync.Once
	plaetze       chan struct{}
}

// VorgabeMaxDatenwege entspricht der Besuchergrenze des Vermittlers: mehr
// kann er von sich aus ohnehin nicht gleichzeitig anfordern.
const VorgabeMaxDatenwege = VorgabeMaxBesucher

func (h *Heimseite) datenwegPlaetze() chan struct{} {
	h.plaetzeEinmal.Do(func() {
		h.plaetze = make(chan struct{}, oder(h.MaxDatenwege, VorgabeMaxDatenwege))
	})
	return h.plaetze
}

// Zustand ist, was die Verwaltung über den Fernzugriff anzeigt.
type Zustand struct {
	An         bool      `json:"an"`
	Verbunden  bool      `json:"verbunden"`
	Vermittler string    `json:"vermittler"`
	Seit       time.Time `json:"seit,omitzero"`
	Letzter    string    `json:"letzterFehler,omitempty"`
	Versuche   int       `json:"versuche"`

	// EigenerFingerabdruck ist das Zertifikat, mit dem sich dieser Server
	// nach aussen ausweist. Es ist meist selbst ausgestellt, und die Apps
	// brauchen genau diese Zahl, um es wiederzuerkennen. Sie ist kein
	// Geheimnis — jeder, der sich verbindet, bekommt das Zertifikat
	// ohnehin zu sehen.
	EigenerFingerabdruck string `json:"eigenerFingerabdruck,omitempty"`
}

func (h *Heimseite) Zustand() Zustand {
	h.mu.Lock()
	defer h.mu.Unlock()
	z := h.zustand
	z.An = true
	z.Vermittler = h.Vermittler
	z.EigenerFingerabdruck = h.EigenerFingerabdruck
	return z
}

func (h *Heimseite) merken(f func(*Zustand)) {
	h.mu.Lock()
	f(&h.zustand)
	h.mu.Unlock()
}

// Laufen hält die Leitung, bis ctx endet. Fällt sie aus, wird neu gewählt —
// mit wachsendem Abstand, damit ein Vermittler, der gerade neu startet,
// nicht von Versuchen überrollt wird.
func (h *Heimseite) Laufen(ctx context.Context) {
	if h.Log == nil {
		h.Log = slog.Default()
	}
	warte := time.Second

	for ctx.Err() == nil {
		err := h.einmalVerbinden(ctx)

		switch {
		case ctx.Err() != nil:
			return
		case err != nil:
			h.merken(func(z *Zustand) {
				z.Verbunden = false
				z.Letzter = kurz(err)
				z.Versuche++
			})
			// Ein falsches Geheimnis wird durch Wiederholen nicht richtig.
			if errors.Is(err, ErrAbgelehnt) {
				h.Log.Error("Vermittler lehnt ab — stimmt das Geheimnis?", "grund", err)
				warte = 5 * time.Minute
			} else {
				h.Log.Warn("Fernzugriff unterbrochen", "grund", kurz(err),
					"nächster Versuch in", warte)
			}
		default:
			h.merken(func(z *Zustand) { z.Verbunden = false })
			h.Log.Info("Fernzugriff beendet")
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(warte):
		}
		if warte < 2*time.Minute {
			warte *= 2
		}
	}
}

func (h *Heimseite) einmalVerbinden(ctx context.Context) error {
	c, err := h.waehlen(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()

	r := bufio.NewReader(c)
	if err := AnmeldungSenden(c, r, Anmeldung{
		Rolle: Kontrolle, Geheimnis: h.Geheimnis,
	}); err != nil {
		return err
	}

	h.merken(func(z *Zustand) {
		z.Verbunden = true
		z.Seit = time.Now()
		z.Letzter = ""
		z.Versuche = 0
	})
	h.Log.Info("Fernzugriff steht", "vermittler", h.Vermittler)
	defer h.Log.Info("Fernzugriff abgebaut")

	// Endet ctx, reisst das Schliessen den Leser aus dem Warten.
	fertig := make(chan struct{})
	defer close(fertig)
	go func() {
		select {
		case <-ctx.Done():
			_ = c.Close()
		case <-fertig:
		}
	}()

	for {
		_ = c.SetReadDeadline(time.Now().Add(Pulsfrist))
		zeile, err := zeileLesen(r)
		if err != nil {
			return err
		}
		switch {
		case zeile == "PULS":
			_ = c.SetWriteDeadline(time.Now().Add(Begruessungsfrist))
			if err := zeileSchreiben(c, "PULS"); err != nil {
				return err
			}
		case strings.HasPrefix(zeile, "OEFFNE "):
			kennung := strings.TrimPrefix(zeile, "OEFFNE ")
			// Jeder Besucher bekommt seinen eigenen Weg. Dass das hier
			// nebenher läuft, ist der Grund, warum mehrere gleichzeitig
			// schauen können. Der Platz wird belegt, bevor die Goroutine
			// startet — sonst wären die Goroutinen selbst unbegrenzt.
			select {
			case h.datenwegPlaetze() <- struct{}{}:
				go func() {
					defer func() { <-h.datenwegPlaetze() }()
					h.datenwegBauen(ctx, kennung)
				}()
			default:
				// Alles belegt: dieser Besucher bekommt keinen Weg und
				// beim Vermittler nach Abholfrist eine geschlossene Tür.
				h.Log.Warn("Zu viele Datenwege gleichzeitig — Besucher abgewiesen",
					"grenze", cap(h.datenwegPlaetze()))
			}
		default:
			// Eine Zeile, die wir nicht kennen: still übergehen. Ein
			// neuerer Vermittler darf mehr sagen, als diese Fassung
			// versteht.
		}
	}
}

// datenwegBauen wählt ein zweites Mal hinaus und hängt die Verbindung an
// Lumen im Heimnetz.
func (h *Heimseite) datenwegBauen(ctx context.Context, kennung string) {
	c, err := h.waehlen(ctx)
	if err != nil {
		h.Log.Warn("Datenweg kam nicht zustande", "grund", kurz(err))
		return
	}
	defer func() { _ = c.Close() }()

	r := bufio.NewReader(c)
	if err := AnmeldungSenden(c, r, Anmeldung{
		Rolle: Daten, Geheimnis: h.Geheimnis, Kennung: kennung,
	}); err != nil {
		h.Log.Warn("Datenweg abgelehnt", "grund", kurz(err))
		return
	}

	d := net.Dialer{Timeout: 10 * time.Second}
	lumen, err := d.DialContext(ctx, "tcp", h.Ziel)
	if err != nil {
		h.Log.Error("Lumen selbst nicht erreichbar", "ziel", h.Ziel, "grund", kurz(err))
		return
	}
	defer func() { _ = lumen.Close() }()

	// Ab hier ist das hier nur noch Rohr. Was durchläuft, wird nicht
	// angesehen — auch nicht auf dieser Seite.
	Durchreichen(c, lumen)
}

// waehlen baut die TLS-Verbindung zum Vermittler auf.
func (h *Heimseite) waehlen(ctx context.Context) (net.Conn, error) {
	rechner, _, err := net.SplitHostPort(h.Vermittler)
	if err != nil {
		return nil, fmt.Errorf("adresse des vermittlers: %w", err)
	}

	auf := &tls.Config{ServerName: rechner, MinVersion: tls.VersionTLS12}
	if h.Fingerabdruck != "" {
		// Gepinnt: die übliche Prüfung wird abgeschaltet und durch eine
		// strengere ersetzt — genau dieses eine Zertifikat, kein anderes.
		auf.InsecureSkipVerify = true
		auf.VerifyConnection = pruefeFingerabdruck(h.Fingerabdruck)
	}

	d := tls.Dialer{NetDialer: &net.Dialer{Timeout: 15 * time.Second}, Config: auf}
	c, err := d.DialContext(ctx, "tcp", h.Vermittler)
	if err != nil {
		return nil, err
	}
	if t, ok := c.(*tls.Conn).NetConn().(*net.TCPConn); ok {
		_ = t.SetKeepAlive(true)
		_ = t.SetKeepAlivePeriod(2 * time.Minute)
	}
	return c, nil
}

func pruefeFingerabdruck(erwartet string) func(tls.ConnectionState) error {
	soll := strings.ToLower(strings.NewReplacer(":", "", " ", "").Replace(erwartet))
	return func(st tls.ConnectionState) error {
		if len(st.PeerCertificates) == 0 {
			return errors.New("der vermittler hat kein zertifikat geschickt")
		}
		ist := Fingerabdruck(st.PeerCertificates[0])
		if subtleGleich(soll, ist) {
			return nil
		}
		return fmt.Errorf("der vermittler meldet sich mit einem anderen zertifikat "+
			"(erwartet %s, bekommen %s)", soll, ist)
	}
}

// Fingerabdruck ist der SHA-256 eines Zertifikats in Hex — das, was der
// Vermittler beim Start ausgibt und was in die Einstellungen gehört.
func Fingerabdruck(c *x509.Certificate) string {
	s := sha256.Sum256(c.Raw)
	return hex.EncodeToString(s[:])
}

func subtleGleich(a, b string) bool { return GeheimnisStimmt(a, b) }

// kurz macht aus einer Fehlermeldung etwas, das in eine Zeile passt und
// keine Pfade oder Kennungen mitschleppt.
func kurz(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	if i := strings.Index(s, "\n"); i >= 0 {
		s = s[:i]
	}
	if len(s) > 160 {
		s = s[:157] + "…"
	}
	return s
}
