// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

// Kurzlebige Zwischendateien beim Abspielen.
//
// Lieder, die ein Gerät nicht kann (FLAC im Safari, ALAC im Firefox), und
// Handyvideos in HEVC werden beim Abspielen umgewandelt. Früher landete
// das Ergebnis dauerhaft unter musik-umgewandelt/<titel>-….m4a — das
// Anlegedatum der Datei (ctime, das sich nicht zurückstellen lässt) und
// die Änderungszeit des Ordners ergaben zusammen eine Liste, wann welches
// Lied gehört wurde. Genau das soll Lumen nicht festhalten.
//
// Jetzt:
//
//   - Die Umwandlung kommt in <Umwandlungsordner>/kurzlebig unter einem
//     Zufallsnamen. Welcher Titel dahintersteckt, weiss nur der
//     Arbeitsspeicher.
//   - Nach kurzlebigFrist ohne Abruf wird sie gelöscht; beim Start räumt
//     Lumen den Ordner ganz leer (und die alten Ordner mit Titelnamen weg).
//   - Datei und Ordner bekommen danach das feste Datum (zeitlos).
//
// Was bleibt: Solange eine Datei existiert, sieht root, dass gerade etwas
// umgewandelt wurde, und könnte sie anhören. Das steht in DATENSCHUTZ.md.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// kurzlebigFrist: so lange bleibt eine Umwandlung nach dem letzten Abruf.
// Lang genug zum Spulen und für die Wiederholung eines Lieds, kurz genug,
// dass auf der Platte keine Sammlung entsteht.
const kurzlebigFrist = 20 * time.Minute

// kurzlebigHoechstens: mehr belegen die kurzlebigen Dateien zusammen nicht.
const kurzlebigHoechstens = 4 << 30

type kurzEintrag struct {
	pfad    string
	zuletzt time.Time
	fertig  chan struct{}
	err     error
	offen   int // Anfragen, die gerade daraus senden
}

type kurzlebigeDateien struct {
	mu     sync.Mutex
	ordner string
	je     map[string]*kurzEintrag // Schlüssel (Titel, Fassung, Art) → Datei
	ende   <-chan struct{}
}

// kurzlebigOrdner: wo die Dateien liegen. Im Umwandlungsordner, weil der
// für Zwischendateien gedacht ist (oft eine schnelle, eigene Platte).
func (s *Server) kurzlebigOrdner() string {
	return filepath.Join(orElse(s.umwandlungDir, s.dataDir), "kurzlebig")
}

// kurzlebigEinrichten räumt beim ersten Gebrauch auf: Reste vom letzten
// Lauf und die alten, dauerhaften Ordner mit Titelkennungen im Namen.
func (s *Server) kurzlebigEinrichten() *kurzlebigeDateien {
	s.kurzEinmal.Do(func() {
		k := &kurzlebigeDateien{ordner: s.kurzlebigOrdner(), je: map[string]*kurzEintrag{},
			ende: s.hintergrund().Done()}
		_ = os.RemoveAll(k.ordner)
		for _, alt := range []string{"musik-umgewandelt", "video-umgewandelt"} {
			_ = os.RemoveAll(s.mediathekOrdner(alt))
		}
		// Auch die Elternordner: Ihre Änderungszeit sagte sonst, wann zum
		// ersten Mal etwas umgewandelt wurde.
		zeitlos(filepath.Dir(s.mediathekOrdner("x")))
		s.kurz = k
		go k.aufraeumenLaufend()
	})
	return s.kurz
}

// KurzlebigAufraeumen löscht beim Start, was ein früherer Lauf
// hinterlassen hat — auch wenn danach nie etwas umgewandelt wird.
func (s *Server) KurzlebigAufraeumen() { s.kurzlebigEinrichten() }

// kurzlebigeDatei liefert die Umwandlung zu schluessel — vorhanden oder
// frisch gerechnet. fertig muss nach dem Senden aufgerufen werden; bis
// dahin wird die Datei nicht gelöscht.
func (s *Server) kurzlebigeDatei(ctx context.Context, schluessel, endung string,
	rechnen func(ziel string) error) (pfad string, fertig func(), err error) {
	kurz := s.kurzlebigEinrichten()
	kurz.mu.Lock()
	e := kurz.je[schluessel]
	if e == nil {
		e = &kurzEintrag{fertig: make(chan struct{})}
		kurz.je[schluessel] = e
		kurz.mu.Unlock()
		e.err = kurz.erzeugen(ctx, e, endung, rechnen)
		kurz.mu.Lock()
		if e.err != nil {
			delete(kurz.je, schluessel)
		}
		close(e.fertig)
		kurz.mu.Unlock()
	} else {
		kurz.mu.Unlock()
	}
	select {
	case <-e.fertig:
	case <-ctx.Done():
		return "", func() {}, ctx.Err()
	}
	if e.err != nil {
		return "", func() {}, e.err
	}
	kurz.mu.Lock()
	e.offen++
	e.zuletzt = time.Now()
	kurz.mu.Unlock()
	var einmal sync.Once
	return e.pfad, func() {
		einmal.Do(func() {
			kurz.mu.Lock()
			e.offen--
			e.zuletzt = time.Now()
			kurz.mu.Unlock()
		})
	}, nil
}

func (k *kurzlebigeDateien) erzeugen(ctx context.Context, e *kurzEintrag, endung string, rechnen func(string) error) error {
	select {
	case mediathekPlaetze <- struct{}{}:
		defer func() { <-mediathekPlaetze }()
	case <-ctx.Done():
		return ctx.Err()
	}
	if _, err := os.Stat(k.ordner); err != nil {
		if err := os.MkdirAll(k.ordner, 0o700); err != nil {
			return err
		}
		zeitlos(filepath.Dir(k.ordner))
	}
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	ziel := filepath.Join(k.ordner, hex.EncodeToString(b)+"."+endung)
	// Weiterrechnen, auch wenn die Anfrage abbricht: Das Gerät fragt
	// meist gleich noch einmal (mit Range), und dann ist es da.
	if err := rechnen(ziel); err != nil {
		_ = os.Remove(ziel)
		zeitlos(k.ordner)
		return err
	}
	zeitlos(ziel)
	zeitlos(k.ordner)
	e.pfad = ziel
	e.zuletzt = time.Now()
	return nil
}

// aufraeumenLaufend löscht, was länger als kurzlebigFrist niemand
// abgerufen hat, und hält die Summe unter kurzlebigHoechstens.
func (k *kurzlebigeDateien) aufraeumenLaufend() {
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for {
		select {
		case <-tick.C:
			k.aufraeumen(time.Now(), kurzlebigFrist)
		case <-k.ende:
			// Beim Herunterfahren alles weg — beim nächsten Start wäre es
			// ohnehin verloren, und so bleibt nichts liegen.
			k.aufraeumen(time.Now(), -1)
			return
		}
	}
}

func (k *kurzlebigeDateien) aufraeumen(jetzt time.Time, frist time.Duration) {
	k.mu.Lock()
	var weg []string
	var summe int64
	type kandidat struct {
		schluessel string
		e          *kurzEintrag
		groesse    int64
	}
	var uebrig []kandidat
	for sch, e := range k.je {
		select {
		case <-e.fertig:
		default:
			continue // wird gerade gerechnet
		}
		if e.offen > 0 {
			if st, err := os.Stat(e.pfad); err == nil {
				summe += st.Size()
			}
			continue
		}
		if jetzt.Sub(e.zuletzt) > frist {
			weg = append(weg, e.pfad)
			delete(k.je, sch)
			continue
		}
		if st, err := os.Stat(e.pfad); err == nil {
			summe += st.Size()
			uebrig = append(uebrig, kandidat{sch, e, st.Size()})
		}
	}
	// Über der Grenze: die am längsten nicht abgerufenen zuerst.
	for summe > kurzlebigHoechstens && len(uebrig) > 0 {
		alt := 0
		for i := range uebrig {
			if uebrig[i].e.zuletzt.Before(uebrig[alt].e.zuletzt) {
				alt = i
			}
		}
		weg = append(weg, uebrig[alt].e.pfad)
		delete(k.je, uebrig[alt].schluessel)
		summe -= uebrig[alt].groesse
		uebrig = append(uebrig[:alt], uebrig[alt+1:]...)
	}
	ordner := k.ordner
	k.mu.Unlock()
	for _, p := range weg {
		_ = os.Remove(p)
	}
	if len(weg) > 0 && ordner != "" {
		zeitlos(ordner)
	}
}
