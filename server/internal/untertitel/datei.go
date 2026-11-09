// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package untertitel

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Hash berechnet den OpenSubtitles-Moviehash: die Dateigrösse plus die
// Summe der ersten und der letzten 64 KiB, gelesen als 64-Bit-Zahlen
// (little-endian), mit Überlauf. So erkennt OpenSubtitles genau diese
// Datei — ohne dass Lumen sie hochladen müsste. Gelesen werden nur
// 128 KiB, auch bei einem 50-GB-Film.
func Hash(pfad string) (string, error) {
	f, err := os.Open(pfad)
	if err != nil {
		return "", err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return "", err
	}
	const block = 65536
	groesse := st.Size()
	if groesse < 2*block {
		return "", errors.New("die Datei ist für den Hash zu klein")
	}
	h := uint64(groesse)
	puffer := make([]byte, block)
	for _, ab := range []int64{0, groesse - block} {
		if _, err := f.ReadAt(puffer, ab); err != nil && !errors.Is(err, io.EOF) {
			return "", err
		}
		for i := 0; i < block; i += 8 {
			h += binary.LittleEndian.Uint64(puffer[i:])
		}
	}
	return fmt.Sprintf("%016x", h), nil
}

var reSprache = regexp.MustCompile(`^[a-z]{2,3}(-[a-z]{2})?$`)

// SpracheGueltig: nur Kürzel wie „de", „pt-br", „zho" — alles andere
// käme sonst in einen Dateinamen.
func SpracheGueltig(s string) bool { return reSprache.MatchString(strings.ToLower(s)) }

// GeholtJeSprache: so viele geholte Untertitel nimmt Lumen je Datei,
// Sprache und Art (normal, SDH, erzwungen) höchstens an.
const GeholtJeSprache = 3

// ErrZuVieleGeholt: Für diese Datei und Sprache liegen schon genug.
var ErrZuVieleGeholt = errors.New("für diese Sprache liegen schon genug geholte Untertitel da")

// festeZeit: das Datum, das geholte Untertitel und ihr Ordner tragen.
var festeZeit = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)

// AblegenGeholt schreibt geholte Untertitel ins Datenverzeichnis, nicht
// neben das Video: „<ordner>/<datei>.de.srt", für Hörgeschädigte
// „….de.sdh.srt", erzwungene „….de.forced.srt", weitere „….de.2.srt".
//
// Früher lagen sie neben dem Film. Das schrieb in die Medienordner (die
// oft anderen gehören oder gesichert werden) und änderte die
// Änderungszeit des Filmordners — die verriet, wann jemand Untertitel für
// diesen Film gesucht und ihn damit ungefähr geschaut hat. Hier tragen
// Datei und Ordner ein festes Datum.
//
// datei ist die Kennung der Mediendatei (nur [A-Za-z0-9_-]). Gibt es
// schon GeholtJeSprache Dateien dieser Art, kommt ErrZuVieleGeholt.
func AblegenGeholt(ordner, datei, sprache string, hoergeschaedigt, erzwungen bool, inhalt []byte) (string, error) {
	sprache = strings.ToLower(sprache)
	if !SpracheGueltig(sprache) {
		return "", fmt.Errorf("unbekannte Sprache")
	}
	if !kennungGueltig(datei) {
		return "", fmt.Errorf("unbekannte Datei")
	}
	if err := os.MkdirAll(ordner, 0o700); err != nil {
		return "", fmt.Errorf("der Ordner für Untertitel ist nicht beschreibbar")
	}
	zusatz := ""
	switch {
	case erzwungen:
		zusatz = ".forced"
	case hoergeschaedigt:
		zusatz = ".sdh"
	}
	for n := 1; n <= GeholtJeSprache; n++ {
		nr := ""
		if n > 1 {
			nr = fmt.Sprintf(".%d", n)
		}
		ziel := filepath.Join(ordner, datei+"."+sprache+zusatz+nr+".srt")
		// O_EXCL: Gibt es die Datei schon (auch durch eine gleichzeitige
		// Anfrage), schlägt das Anlegen fehl, und es geht zur nächsten Nummer.
		f, err := os.OpenFile(ziel, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("der Ordner für Untertitel ist nicht beschreibbar")
		}
		_, werr := f.Write(inhalt)
		cerr := f.Close()
		if werr != nil || cerr != nil {
			_ = os.Remove(ziel)
			return "", fmt.Errorf("die Untertiteldatei liess sich nicht schreiben")
		}
		_ = os.Chtimes(ziel, festeZeit, festeZeit)
		_ = os.Chtimes(ordner, festeZeit, festeZeit)
		return ziel, nil
	}
	return "", ErrZuVieleGeholt
}

func kennungGueltig(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}
