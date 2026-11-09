// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package stream

// Schlüsselbilder für HLS ohne Neurechnen.
//
// Wird das Bild nur kopiert (-c:v copy), kann FFmpeg ein Segment nur an
// einem Schlüsselbild der Quelle beginnen lassen — nicht alle vier
// Sekunden, wie beim Umwandeln. Die Abspielliste muss aber vorher
// vollständig dastehen, und nach einem Sprung muss ein neu gestarteter
// FFmpeg genau dieselben Segmentgrenzen treffen wie der alte. Beides geht
// nur, wenn Lumen die Schlüsselbilder vorher kennt.
//
// MKV — der Normalfall für alles, was über HLS läuft — führt sie am Ende
// der Datei in einem Verzeichnis (Cues). Das liest Lumen selbst: wenige
// Kilobyte statt der ganzen Datei, auch bei einem 60-GB-Remux in einem
// Augenblick. Fehlt das Verzeichnis oder ist es unbrauchbar, gibt es keine
// Grenzen — und die Wiedergabe rechnet das Bild neu, wie bisher.

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"sync"
	"time"
)

// EBML-Kennungen, soweit sie hier gebraucht werden.
const (
	ebmlKopf       = 0x1A45DFA3
	mkvSegment     = 0x18538067
	mkvSeekHead    = 0x114D9B74
	mkvSeek        = 0x4DBB
	mkvSeekID      = 0x53AB
	mkvSeekPos     = 0x53AC
	mkvInfo        = 0x1549A966
	mkvZeitmass    = 0x2AD7B1
	mkvTracks      = 0x1654AE6B
	mkvTrackEntry  = 0xAE
	mkvTrackNummer = 0xD7
	mkvTrackArt    = 0x83
	mkvCues        = 0x1C53BB6B
	mkvCuePoint    = 0xBB
	mkvCueZeit     = 0xB3
	mkvCuePos      = 0xB7
	mkvCueTrack    = 0xF7
	mkvCluster     = 0x1F43B675
)

var errKeineCues = errors.New("kein verzeichnis der schlüsselbilder")

// MKVSchluesselbilder liest die Zeiten der Schlüsselbilder der ersten
// Bildspur aus dem Verzeichnis einer MKV-Datei, in Sekunden, aufsteigend.
func MKVSchluesselbilder(pfad string) ([]float64, error) {
	f, err := os.Open(pfad)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	l := &ebmlLeser{r: f, groesse: fi.Size()}

	id, groesse, kopf, err := l.element(0)
	if err != nil || id != ebmlKopf {
		return nil, fmt.Errorf("keine mkv-datei")
	}
	pos := kopf + groesse
	id, groesse, kopf, err = l.element(pos)
	if err != nil || id != mkvSegment {
		return nil, fmt.Errorf("kein mkv-segment")
	}
	segStart := pos + kopf
	segEnde := fi.Size()
	if groesse >= 0 && segStart+groesse < segEnde {
		segEnde = segStart + groesse
	}

	// Die oberste Ebene bis zum ersten Cluster durchgehen: dort stehen
	// SeekHead, Info und Tracks. Die Cues liegen meist am Ende und werden
	// über den SeekHead gefunden.
	var cuesPos, infoPos, tracksPos int64 = -1, -1, -1
	for p := segStart; p < segEnde; {
		id, groesse, kopf, err := l.element(p)
		if err != nil || groesse < 0 {
			break
		}
		switch id {
		case mkvSeekHead:
			daten, err := l.lesen(p+kopf, groesse, 1<<20)
			if err != nil {
				return nil, err
			}
			for _, s := range kinder(daten, mkvSeek) {
				var sid, spos uint64
				for _, k := range unterElemente(s) {
					switch k.id {
					case mkvSeekID:
						sid = beUint(k.daten)
					case mkvSeekPos:
						spos = beUint(k.daten)
					}
				}
				switch sid {
				case mkvCues:
					cuesPos = segStart + int64(spos)
				case mkvInfo:
					infoPos = segStart + int64(spos)
				case mkvTracks:
					tracksPos = segStart + int64(spos)
				}
			}
		case mkvInfo:
			infoPos = p
		case mkvTracks:
			tracksPos = p
		case mkvCues:
			cuesPos = p
		}
		if id == mkvCluster {
			break
		}
		p += kopf + groesse
	}
	if cuesPos < 0 || tracksPos < 0 {
		return nil, errKeineCues
	}

	zeitmass := uint64(1_000_000)
	if infoPos >= 0 {
		if daten, err := l.inhalt(infoPos, mkvInfo, 1<<20); err == nil {
			for _, k := range unterElemente(daten) {
				if k.id == mkvZeitmass {
					if v := beUint(k.daten); v > 0 {
						zeitmass = v
					}
				}
			}
		}
	}

	daten, err := l.inhalt(tracksPos, mkvTracks, 4<<20)
	if err != nil {
		return nil, err
	}
	bildspur := uint64(0)
	for _, t := range kinder(daten, mkvTrackEntry) {
		var nr, art uint64
		for _, k := range unterElemente(t) {
			switch k.id {
			case mkvTrackNummer:
				nr = beUint(k.daten)
			case mkvTrackArt:
				art = beUint(k.daten)
			}
		}
		if art == 1 {
			bildspur = nr
			break
		}
	}
	if bildspur == 0 {
		return nil, fmt.Errorf("keine bildspur")
	}

	daten, err = l.inhalt(cuesPos, mkvCues, 64<<20)
	if err != nil {
		return nil, err
	}
	var zeiten []float64
	for _, cp := range kinder(daten, mkvCuePoint) {
		var zeit uint64
		hatZeit, passt := false, false
		for _, k := range unterElemente(cp) {
			switch k.id {
			case mkvCueZeit:
				zeit, hatZeit = beUint(k.daten), true
			case mkvCuePos:
				for _, kk := range unterElemente(k.daten) {
					if kk.id == mkvCueTrack && beUint(kk.daten) == bildspur {
						passt = true
					}
				}
			}
		}
		if hatZeit && passt {
			zeiten = append(zeiten, float64(zeit)*float64(zeitmass)/1e9)
		}
	}
	if len(zeiten) == 0 {
		return nil, errKeineCues
	}
	sort.Float64s(zeiten)
	// Doppelte weg.
	out := zeiten[:1]
	for _, z := range zeiten[1:] {
		if z-out[len(out)-1] > 0.0005 {
			out = append(out, z)
		}
	}
	return out, nil
}

// ---------------------------------------------------------------- Segmentgrenzen

// MaxKopierSegment: Liegen Schlüsselbilder weiter auseinander, wird das
// Bild lieber neu gerechnet. Ein Segment von 20 Sekunden lässt ein Gerät
// nach jedem Sprung zu lange warten, und manche Player mögen es gar nicht.
const MaxKopierSegment = 12.0

// SegmentGrenzen wählt aus den Schlüsselbildern die Segmentanfänge: das
// erste bei null, dann jeweils das erste Schlüsselbild, das mindestens
// ungefähr ziel Sekunden später liegt. Ein Rest unter einer Sekunde am
// Ende wird dem letzten Segment zugeschlagen. Gibt nil zurück, wenn sich
// so keine vernünftigen Segmente bilden lassen.
func SegmentGrenzen(schluessel []float64, dauer, ziel float64) []float64 {
	if len(schluessel) < 2 || dauer <= 0 {
		return nil
	}
	// Das erste Schlüsselbild muss am Anfang liegen — sonst stimmt das
	// Verzeichnis nicht mit dem Film überein.
	if schluessel[0] > 1.0 {
		return nil
	}
	// Gewählt wird je das Schlüsselbild, das ziel Sekunden nach dem
	// letzten Anfang am nächsten liegt — aber nicht näher als die Hälfte
	// davon, sonst entstünden Schnipsel.
	grenzen := []float64{0}
	vorher := -1.0
	for _, k := range schluessel[1:] {
		if k >= dauer-1.0 {
			break
		}
		letzte := grenzen[len(grenzen)-1]
		if k-letzte >= ziel {
			wahl := k
			if vorher-letzte >= ziel/2 && letzte+ziel-vorher < k-(letzte+ziel) {
				wahl = vorher
			}
			grenzen = append(grenzen, wahl)
			if wahl == vorher && k-wahl >= ziel {
				// Das aktuelle könnte schon das nächste Segment beginnen.
				grenzen = append(grenzen, k)
			}
		}
		vorher = k
	}
	// Prüfen: kein Segment zu lang (auch das letzte bis zum Ende).
	for i := range grenzen {
		ende := dauer
		if i+1 < len(grenzen) {
			ende = grenzen[i+1]
		}
		if ende-grenzen[i] > MaxKopierSegment {
			return nil
		}
	}
	return grenzen
}

// RasterGrenzen sind die Grenzen beim Neurechnen: alle SegmentSeconds.
func RasterGrenzen(dauer float64) []float64 {
	n := int(dauer / SegmentSeconds)
	rest := dauer - float64(n)*SegmentSeconds
	if rest > 0.1 {
		n++
	}
	if n < 1 {
		n = 1
	}
	g := make([]float64, n)
	for i := range g {
		g[i] = float64(i * SegmentSeconds)
	}
	return g
}

// Zwischenspeicher der Grenzen je Datei — das Verzeichnis ändert sich nur,
// wenn sich die Datei ändert.
type grenzEintrag struct {
	grenzen []float64
	groesse int64
	mtime   time.Time
}

var (
	grenzMu    sync.Mutex
	grenzCache = map[string]grenzEintrag{}
)

// KopierGrenzen liefert die Segmentgrenzen für eine Datei, deren Bild
// kopiert werden soll, oder nil, wenn das nicht sauber geht. start ist der
// Beginn der Zeitachse (BildDetails.Start): Die Zeiten im Verzeichnis sind
// absolut, -ss zählt ab dem Beginn.
func KopierGrenzen(pfad string, dauerMS int64, start float64) []float64 {
	fi, err := os.Stat(pfad)
	if err != nil {
		return nil
	}
	grenzMu.Lock()
	if e, ok := grenzCache[pfad]; ok && e.groesse == fi.Size() && e.mtime.Equal(fi.ModTime()) {
		grenzMu.Unlock()
		return e.grenzen
	}
	grenzMu.Unlock()
	var g []float64
	if k, err := MKVSchluesselbilder(pfad); err == nil {
		if start > 0 {
			for i := range k {
				k[i] = math.Max(0, k[i]-start)
			}
		}
		g = SegmentGrenzen(k, float64(dauerMS)/1000, SegmentSeconds)
	}
	grenzMu.Lock()
	if len(grenzCache) > 500 {
		grenzCache = map[string]grenzEintrag{}
	}
	grenzCache[pfad] = grenzEintrag{grenzen: g, groesse: fi.Size(), mtime: fi.ModTime()}
	grenzMu.Unlock()
	return g
}

// ---------------------------------------------------------------- EBML

type ebmlLeser struct {
	r       io.ReaderAt
	groesse int64
}

// element liest Kennung und Grösse an einer Stelle. groesse ist -1, wenn
// sie unbekannt ist (Live-Aufnahmen schreiben das so).
func (l *ebmlLeser) element(pos int64) (id uint64, groesse int64, kopf int64, err error) {
	var b [12]byte
	n, err := l.r.ReadAt(b[:], pos)
	if n < 2 {
		if err == nil {
			err = io.ErrUnexpectedEOF
		}
		return 0, 0, 0, err
	}
	id, idLen, ok := vint(b[:n], true)
	if !ok {
		return 0, 0, 0, fmt.Errorf("kaputte kennung")
	}
	g, gLen, ok := vint(b[idLen:n], false)
	if !ok {
		return 0, 0, 0, fmt.Errorf("kaputte grösse")
	}
	groesse = int64(g)
	if g == math.MaxUint64 {
		groesse = -1
	}
	return id, groesse, int64(idLen + gLen), nil
}

func (l *ebmlLeser) lesen(pos, n, grenze int64) ([]byte, error) {
	if n < 0 || n > grenze || pos+n > l.groesse {
		return nil, fmt.Errorf("element zu gross oder ausserhalb der datei")
	}
	b := make([]byte, n)
	if _, err := l.r.ReadAt(b, pos); err != nil {
		return nil, err
	}
	return b, nil
}

// inhalt liest ein Element an pos, wenn es die erwartete Kennung hat.
func (l *ebmlLeser) inhalt(pos int64, erwartet uint64, grenze int64) ([]byte, error) {
	id, groesse, kopf, err := l.element(pos)
	if err != nil {
		return nil, err
	}
	if id != erwartet {
		return nil, fmt.Errorf("an der stelle steht etwas anderes")
	}
	return l.lesen(pos+kopf, groesse, grenze)
}

// vint liest eine Zahl variabler Länge. Bei einer Kennung bleibt das
// Längenbit stehen (so sind die Kennungen definiert), bei einer Grösse
// nicht. Lauter Einsen bei einer Grösse heisst „unbekannt".
func vint(b []byte, kennung bool) (uint64, int, bool) {
	if len(b) == 0 || b[0] == 0 {
		return 0, 0, false
	}
	laenge := 1
	for maske := byte(0x80); b[0]&maske == 0; maske >>= 1 {
		laenge++
	}
	if laenge > 8 || laenge > len(b) || (kennung && laenge > 4) {
		return 0, 0, false
	}
	var v uint64
	for i := 0; i < laenge; i++ {
		v = v<<8 | uint64(b[i])
	}
	if kennung {
		return v, laenge, true
	}
	v &^= uint64(0x80>>(laenge-1)) << (8 * (laenge - 1))
	if v == (uint64(1)<<(7*laenge))-1 {
		return math.MaxUint64, laenge, true
	}
	return v, laenge, true
}

type ebmlTeil struct {
	id    uint64
	daten []byte
}

// unterElemente zerlegt den Inhalt eines Elements in seine Kinder.
func unterElemente(b []byte) []ebmlTeil {
	var out []ebmlTeil
	for len(b) > 0 {
		id, idLen, ok := vint(b, true)
		if !ok {
			break
		}
		g, gLen, ok := vint(b[idLen:], false)
		if !ok || g == math.MaxUint64 {
			break
		}
		start := idLen + gLen
		if uint64(len(b)-start) < g {
			break
		}
		out = append(out, ebmlTeil{id: id, daten: b[start : start+int(g)]})
		b = b[start+int(g):]
	}
	return out
}

func kinder(b []byte, id uint64) [][]byte {
	var out [][]byte
	for _, k := range unterElemente(b) {
		if k.id == id {
			out = append(out, k.daten)
		}
	}
	return out
}

func beUint(b []byte) uint64 {
	if len(b) > 8 {
		return 0
	}
	var buf [8]byte
	copy(buf[8-len(b):], b)
	return binary.BigEndian.Uint64(buf[:])
}
