// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

// Package relay macht Lumen von aussen erreichbar, ohne dass im Router ein
// Port offen stehen muss.
//
// Der Weg ist umgekehrt zu dem, was man erwartet: nicht der Besucher kommt
// ins Heimnetz hinein, sondern der Heimserver wählt sich nach draussen und
// hält die Leitung offen. Kommt ein Besucher beim Vermittler an, sagt der
// über diese Leitung Bescheid, und der Heimserver baut von sich aus eine
// zweite Verbindung auf, durch die dann die Daten laufen.
//
//	Besucher ──► Vermittler ◄── Heimserver ──► Lumen
//	            (öffentlich)   (wählt sich     (127.0.0.1)
//	                            nach aussen)
//
// # Was der Vermittler nicht sieht
//
// Er reicht Bytes durch, sonst nichts. Lumen verschlüsselt selbst, der
// Vermittler entschlüsselt nicht — er hat den Schlüssel gar nicht. Was
// durch ihn läuft, sind für ihn Zufallszahlen: keine Adressen, keine
// Titel, keine Namen. Das ist kein Versprechen, sondern eine Folge der
// Bauweise, und relay_test.go prüft es nach.
//
// Das ist dieselbe Regel wie überall sonst in Lumen. Der Besitzer sieht
// nicht, wer was schaut; wer den Vermittler betreibt, erst recht nicht —
// selbst wenn er es wollte.
//
// # Das Protokoll
//
// Klein genug, um es hier hinzuschreiben. Zeilen in UTF-8, abgeschlossen
// mit \n; danach rohe Bytes.
//
//	Leitung (Heimserver ──► Vermittler, über TLS)
//	  →  LUMEN-TUNNEL 1
//	  →  KONTROLLE <geheimnis>
//	  ←  BEREIT
//	  ←  OEFFNE <kennung>        sobald ein Besucher da ist
//	  ←  PULS                    alle 30 Sekunden
//	  →  PULS                    Antwort darauf
//
//	Datenweg (Heimserver ──► Vermittler, über TLS, je Besucher einer)
//	  →  LUMEN-TUNNEL 1
//	  →  DATEN <geheimnis> <kennung>
//	  ←  BEREIT
//	  →  ◄ rohe Bytes in beide Richtungen
//
// Geht etwas schief, kommt statt BEREIT eine Zeile ABGELEHNT <grund> und
// die Verbindung wird geschlossen.
package relay

import (
	"bufio"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// Gruss ist die erste Zeile jeder Verbindung. Die Ziffer dahinter ist
	// die Fassung des Protokolls — ändert sich etwas Grundsätzliches, zählt
	// sie hoch, und alte Gegenstellen bekommen eine klare Absage statt
	// unverständlicher Bytes.
	Gruss = "LUMEN-TUNNEL 1"

	// Begruessungsfrist: so lange darf eine Gegenstelle für ihre ersten
	// zwei Zeilen brauchen. Wer länger schweigt, belegt nur einen Platz.
	Begruessungsfrist = 10 * time.Second

	// Pulsabstand: in diesem Takt fragt der Vermittler nach, ob die Leitung
	// noch steht. Ein Heimanschluss hinter NAT vergisst stille Verbindungen
	// gern nach ein paar Minuten.
	Pulsabstand = 30 * time.Second

	// Pulsfrist: bleibt die Antwort so lange aus, gilt die Leitung als tot.
	Pulsfrist = 90 * time.Second

	// Abholfrist: so lange wartet ein Besucher darauf, dass der Heimserver
	// seinen Datenweg aufbaut. Danach bekommt er die Tür vor der Nase zu —
	// besser als ewig zu hängen.
	Abholfrist = 15 * time.Second

	// maxZeile begrenzt, was eine Gegenstelle vor dem Händedruck schicken
	// darf. Ohne das könnte jemand Speicher vollschreiben, indem er eine
	// endlose Zeile sendet.
	maxZeile = 512

	// Ruhefrist: so lange darf ein durchgereichtes Paar in beiden
	// Richtungen schweigen, bevor es geschlossen wird. Lang genug für eine
	// angehaltene Wiedergabe mit Kaffeepause, kurz genug, dass vergessene
	// oder absichtlich offen gehaltene Verbindungen nicht ewig Plätze
	// belegen. Solange Daten fliessen — auch nur in eine Richtung, wie bei
	// einem laufenden Film —, greift sie nie.
	Ruhefrist = 10 * time.Minute
)

// Fehler, die beide Seiten unterscheiden müssen.
var (
	ErrFremdesProtokoll = errors.New("das ist kein Lumen-Tunnel")
	ErrAbgelehnt        = errors.New("abgelehnt")
	ErrGeheimnisFehlt   = errors.New("ohne Geheimnis geht es nicht")
)

// Rolle ist, wofür eine Verbindung gedacht ist.
type Rolle string

const (
	Kontrolle Rolle = "KONTROLLE" // die eine lange Leitung
	Daten     Rolle = "DATEN"     // einer je Besucher
)

// Anmeldung ist, was eine Gegenstelle nach dem Gruss über sich sagt.
type Anmeldung struct {
	Rolle     Rolle
	Geheimnis string
	Kennung   string // nur bei Daten
}

// ---------------------------------------------------------------- Zeilen

// zeileLesen holt eine Zeile und bricht ab, wenn sie unmässig lang wird.
//
// Bewusst Byte für Byte und nicht mit ReadString: ReadString liest erst
// die ganze Zeile in den Speicher und lässt uns danach nachmessen. Wer vor
// dem Händedruck einfach Bytes ohne Zeilenende schickt, hätte bis zum
// Ablauf der Begrüssungsfrist beliebig viel Speicher belegt. So ist nach
// maxZeile Bytes Schluss, egal wie viel noch nachkäme. Langsam ist das
// nicht — die Bytes liegen ohnehin schon im Puffer des bufio.Reader.
func zeileLesen(r *bufio.Reader) (string, error) {
	var b strings.Builder
	for {
		c, err := r.ReadByte()
		if err != nil {
			return "", err
		}
		if c == '\n' {
			break
		}
		if b.Len() >= maxZeile {
			return "", ErrFremdesProtokoll
		}
		b.WriteByte(c)
	}
	return strings.TrimRight(b.String(), "\r"), nil
}

func zeileSchreiben(w io.Writer, format string, args ...any) error {
	_, err := fmt.Fprintf(w, format+"\n", args...)
	return err
}

// ---------------------------------------------------------------- Händedruck

// AnmeldungLesen liest Gruss und Anmeldung — die Seite des Vermittlers.
func AnmeldungLesen(c net.Conn, r *bufio.Reader) (Anmeldung, error) {
	_ = c.SetDeadline(time.Now().Add(Begruessungsfrist))
	defer func() { _ = c.SetDeadline(time.Time{}) }()

	gruss, err := zeileLesen(r)
	if err != nil {
		return Anmeldung{}, err
	}
	if gruss != Gruss {
		return Anmeldung{}, ErrFremdesProtokoll
	}

	zeile, err := zeileLesen(r)
	if err != nil {
		return Anmeldung{}, err
	}
	teile := strings.Fields(zeile)
	if len(teile) < 2 {
		return Anmeldung{}, ErrFremdesProtokoll
	}

	a := Anmeldung{Rolle: Rolle(teile[0]), Geheimnis: teile[1]}
	switch a.Rolle {
	case Kontrolle:
		if len(teile) != 2 {
			return Anmeldung{}, ErrFremdesProtokoll
		}
	case Daten:
		if len(teile) != 3 {
			return Anmeldung{}, ErrFremdesProtokoll
		}
		a.Kennung = teile[2]
	default:
		return Anmeldung{}, ErrFremdesProtokoll
	}
	return a, nil
}

// AnmeldungSenden schickt Gruss und Anmeldung — die Seite des Heimservers —
// und wartet auf das BEREIT.
func AnmeldungSenden(c net.Conn, r *bufio.Reader, a Anmeldung) error {
	_ = c.SetDeadline(time.Now().Add(Begruessungsfrist))
	defer func() { _ = c.SetDeadline(time.Time{}) }()

	if err := zeileSchreiben(c, "%s", Gruss); err != nil {
		return err
	}
	var zeile string
	if a.Rolle == Daten {
		zeile = fmt.Sprintf("%s %s %s", Daten, a.Geheimnis, a.Kennung)
	} else {
		zeile = fmt.Sprintf("%s %s", Kontrolle, a.Geheimnis)
	}
	if err := zeileSchreiben(c, "%s", zeile); err != nil {
		return err
	}

	antwort, err := zeileLesen(r)
	if err != nil {
		return err
	}
	if antwort == "BEREIT" {
		return nil
	}
	if grund, ok := strings.CutPrefix(antwort, "ABGELEHNT "); ok {
		return fmt.Errorf("%w: %s", ErrAbgelehnt, grund)
	}
	return ErrFremdesProtokoll
}

// Bereit und Ablehnen sind die zwei möglichen Antworten des Vermittlers.
func Bereit(w io.Writer) error { return zeileSchreiben(w, "BEREIT") }

func Ablehnen(w io.Writer, grund string) error {
	// Der Grund sagt, was formal nicht stimmte — nie, ob ein Geheimnis
	// „fast" richtig war. Daraus liesse sich sonst raten.
	return zeileSchreiben(w, "ABGELEHNT %s", grund)
}

// ---------------------------------------------------------------- Geheimnis

// GeheimnisStimmt vergleicht in gleichbleibender Zeit. Ein gewöhnlicher
// Vergleich bricht beim ersten falschen Zeichen ab; wer die Zeit misst,
// könnte das Geheimnis Zeichen für Zeichen erraten.
func GeheimnisStimmt(erwartet, bekommen string) bool {
	a := sha256.Sum256([]byte(erwartet))
	b := sha256.Sum256([]byte(bekommen))
	return subtle.ConstantTimeCompare(a[:], b[:]) == 1
}

// ---------------------------------------------------------------- Durchreichen

// Durchreichen verbindet zwei Verbindungen und kopiert in beide Richtungen,
// bis eine Seite aufhört — oder bis Ruhefrist lang in keiner Richtung
// mehr etwas kam.
//
// Hier steht bewusst ein blosses Kopieren und nichts weiter. Kein
// Mitlesen, kein Zählen nach Inhalt, kein Protokollieren — was durchläuft,
// interessiert diese Funktion nicht. Sie gibt zurück, wie viele Bytes in
// welche Richtung gingen; mehr lässt sich über eine Wiedergabe hier nicht
// sagen, und mehr soll sich auch nicht sagen lassen.
func Durchreichen(a, b net.Conn) (hin, zurueck int64) {
	return durchreichen(a, b, Ruhefrist)
}

func durchreichen(a, b net.Conn, ruhe time.Duration) (hin, zurueck int64) {
	// zuletzt ist der Zeitpunkt des letzten Bytes, gleich in welche
	// Richtung. Beide Richtungen teilen ihn: bei einem Film fliesst stundenlang
	// nur etwas vom Heimserver zum Besucher, der Rückweg schweigt. Hätte jede
	// Richtung ihre eigene Frist, risse der stille Rückweg die laufende
	// Wiedergabe nach zehn Minuten ab.
	var zuletzt atomic.Int64
	zuletzt.Store(time.Now().UnixNano())

	// Bricht eine Richtung mit einem Fehler ab (Ruhefrist, Abriss), ist
	// das Paar als Ganzes tot. Dann beide sofort schliessen, statt die
	// andere Richtung noch bis zu ihrer eigenen Frist warten zu lassen.
	var einmal sync.Once
	kappen := func() {
		einmal.Do(func() {
			_ = a.Close()
			_ = b.Close()
		})
	}

	fertig := make(chan struct{}, 2)
	go func() {
		var err error
		hin, err = kopieren(a, b, ruhe, &zuletzt)
		if err != nil {
			kappen()
		} else {
			halbSchliessen(a)
		}
		fertig <- struct{}{}
	}()
	go func() {
		var err error
		zurueck, err = kopieren(b, a, ruhe, &zuletzt)
		if err != nil {
			kappen()
		} else {
			halbSchliessen(b)
		}
		fertig <- struct{}{}
	}()

	<-fertig
	<-fertig
	return hin, zurueck
}

// kopieren ist io.Copy mit einer Ruhefrist. Jede Leserunde bekommt eine
// frische Frist; läuft sie ab, wird nur dann aufgegeben, wenn auch die
// Gegenrichtung so lange still war. Gibt nil zurück, wenn src sauber zu
// Ende ist.
func kopieren(dst, src net.Conn, ruhe time.Duration, zuletzt *atomic.Int64) (int64, error) {
	puffer := make([]byte, 32*1024)
	var n int64
	for {
		_ = src.SetReadDeadline(time.Now().Add(ruhe))
		k, err := src.Read(puffer)
		if k > 0 {
			zuletzt.Store(time.Now().UnixNano())
			// Auch das Schreiben bekommt eine Frist: ein Besucher, der
			// nichts mehr abnimmt, soll den Weg nicht ewig festhalten.
			_ = dst.SetWriteDeadline(time.Now().Add(ruhe))
			w, werr := dst.Write(puffer[:k])
			n += int64(w)
			if werr != nil {
				return n, werr
			}
			zuletzt.Store(time.Now().UnixNano())
		}
		if err == nil {
			continue
		}
		if errors.Is(err, io.EOF) {
			return n, nil
		}
		if errors.Is(err, os.ErrDeadlineExceeded) &&
			time.Since(time.Unix(0, zuletzt.Load())) < ruhe {
			// Hier war es still, drüben nicht — weiter warten. Ein
			// abgelaufenes Lesen lässt TCP wie TLS heil; es geht einfach
			// mit der nächsten Runde weiter.
			continue
		}
		return n, err
	}
}

// halbSchliessen sagt der Gegenseite „von mir kommt nichts mehr", lässt sie
// aber zu Ende senden. Ohne das bricht eine Antwort ab, die noch unterwegs
// ist — bei einer Wiedergabe wäre das ein abgeschnittenes Segment.
func halbSchliessen(c net.Conn) {
	type halb interface{ CloseWrite() error }
	if h, ok := c.(halb); ok {
		_ = h.CloseWrite()
		return
	}
	_ = c.Close()
}
