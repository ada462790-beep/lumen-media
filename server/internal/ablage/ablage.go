// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

// Package ablage legt Dateien so ab, dass nie eine halbe entsteht und zwei
// gleichzeitige Schreiber sich nicht in die Quere kommen.
//
// Früher schrieb jeder in „<ziel>.teil" und benannte danach um. Holten
// zwei dasselbe Bild zugleich (ein Durchgang und „Neu laden", oder zwei
// Durchgänge), schrieben beide in dieselbe Zwischendatei; der erste
// benannte sie um, der zweite fand beim Umbenennen nichts mehr: „rename
// …-poster.jpg.teil …-poster.jpg: no such file or directory". Jetzt hat
// jede Zwischendatei einen eigenen Namen (os.CreateTemp im selben Ordner,
// damit das Umbenennen auf demselben Laufwerk bleibt), und Einmal sorgt
// dafür, dass dasselbe Ziel nur einmal gleichzeitig geholt wird.
package ablage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

// Endung der Zwischendateien. Aufräumer lassen Dateien mit „.teil" im
// Namen liegen — sie gehören einem Schreiber, der gerade arbeitet.
const Endung = ".teil"

// ErrZuGross: Die Quelle lieferte mehr als die erlaubte Grösse.
var ErrZuGross = errors.New("datei grösser als erlaubt")

// Schreiben legt den Inhalt von quelle unter ziel ab: erst in eine eigene
// Zwischendatei daneben, dann umbenennen. grenze > 0 begrenzt die Grösse;
// ist die Quelle grösser, bleibt nichts liegen und es kommt ErrZuGross.
func Schreiben(ziel string, quelle io.Reader, grenze int64) error {
	f, err := os.CreateTemp(filepath.Dir(ziel), filepath.Base(ziel)+".*"+Endung)
	if err != nil {
		return err
	}
	tmp := f.Name()
	weg := func(e error) error {
		_ = os.Remove(tmp)
		return e
	}
	var n int64
	if grenze > 0 {
		n, err = io.Copy(f, io.LimitReader(quelle, grenze+1))
		if err == nil && n > grenze {
			err = fmt.Errorf("%w (%d MB)", ErrZuGross, grenze>>20)
		}
	} else {
		_, err = io.Copy(f, quelle)
	}
	cerr := f.Close()
	if err != nil {
		return weg(err)
	}
	if cerr != nil {
		return weg(cerr)
	}
	if err := os.Chmod(tmp, 0o644); err != nil {
		return weg(err)
	}
	if err := os.Rename(tmp, ziel); err != nil {
		return weg(err)
	}
	return nil
}

// Einmal führt fn für einen Schlüssel (meist das Ziel) nur einmal
// gleichzeitig aus. Wer während eines laufenden Aufrufs mit demselben
// Schlüssel kommt, wartet und bekommt dessen Ergebnis — statt dasselbe
// Bild ein zweites Mal herunterzuladen.
//
// Endet der laufende Aufruf, weil sein Kontext abgebrochen wurde (die
// Anfrage, die ihn auslöste, ist weg), übernimmt der Wartende das nicht:
// Er versucht es selbst, mit seinem eigenen fn und damit seinem eigenen
// Kontext. Vorher bekamen alle Wartenden „context canceled", obwohl ihre
// eigene Anfrage noch lief.
func Einmal(schluessel string, fn func() error) error {
	for {
		einmalMu.Lock()
		if l, ok := laufend[schluessel]; ok {
			einmalMu.Unlock()
			l.wg.Wait()
			if abgebrochen(l.err) {
				continue
			}
			return l.err
		}
		l := &lauf{}
		l.wg.Add(1)
		laufend[schluessel] = l
		einmalMu.Unlock()

		func() {
			defer func() {
				einmalMu.Lock()
				delete(laufend, schluessel)
				einmalMu.Unlock()
				l.wg.Done()
			}()
			l.err = fn()
		}()
		return l.err
	}
}

// abgebrochen: Endete ein Aufruf nur, weil sein Kontext zu Ende war?
func abgebrochen(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

type lauf struct {
	wg  sync.WaitGroup
	err error
}

var (
	einmalMu sync.Mutex
	laufend  = map[string]*lauf{}
)
