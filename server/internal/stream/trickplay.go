// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package stream

// Vorschaubilder beim Spulen („Trickplay").
//
// Wer über die Zeitleiste fährt, soll sehen, wo er landet. Dafür braucht
// es ein Bild alle zehn Sekunden — bei einem Zweistundenfilm 720 Stück.
// Einzeln wären das 720 Dateien und 720 Anfragen; stattdessen kommen je
// hundert auf ein Blatt (10 × 10), wie bei Emby und Jellyfin. Das Gerät
// lädt ein Blatt und schneidet sich das Bild selbst heraus.
//
// Gerechnet wird nur aus Schlüsselbildern (-skip_frame nokey): Das ist
// ein Bruchteil der Arbeit eines vollen Decodierens, und auf zehn Sekunden
// genau braucht es niemand. HDR wird auch hier für SDR umgerechnet, sonst
// wären die Bildchen blass.

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Die Masse der Vorschaubilder.
const (
	TrickplayIntervall = 10 // Sekunden zwischen zwei Bildern
	TrickplayBreite    = 320
	TrickplaySpalten   = 10
	TrickplayZeilen    = 10
)

// TrickplayInfo beschreibt fertige Vorschaubilder.
type TrickplayInfo struct {
	Intervall int `json:"intervall"`
	Breite    int `json:"breite"`
	Hoehe     int `json:"hoehe"`
	Spalten   int `json:"spalten"`
	Zeilen    int `json:"zeilen"`
	Bilder    int `json:"bilder"`
	Blaetter  int `json:"blaetter"`
}

// TrickplayHoehe rechnet die Höhe eines Bildchens aus dem Seitenverhältnis
// (gerade, damit JPEG und Kachelung nicht stolpern).
func TrickplayHoehe(breite, hoehe int) int {
	if breite <= 0 || hoehe <= 0 {
		return 180
	}
	h := int(math.Round(float64(TrickplayBreite)*float64(hoehe)/float64(breite)/2)) * 2
	if h < 40 {
		h = 40
	}
	if h > 640 {
		h = 640
	}
	return h
}

// TrickplayLesen liest die Beschreibung fertiger Vorschaubilder. Fehlt sie,
// sind sie (noch) nicht fertig.
func TrickplayLesen(ordner string) (*TrickplayInfo, error) {
	b, err := os.ReadFile(filepath.Join(ordner, "info.json"))
	if err != nil {
		return nil, err
	}
	var i TrickplayInfo
	if err := json.Unmarshal(b, &i); err != nil {
		return nil, err
	}
	return &i, nil
}

// TrickplayQuelle: In dieser Datei im Ordner steht, aus welcher Fassung
// der Quelldatei die Bilder stammen (Kennung, Grösse, Änderungszeit). Wird
// eine Datei am selben Pfad ersetzt, passen die Bilder nicht mehr.
const TrickplayQuelle = "quelle"

// TrickplayKennung liest, aus welcher Fassung der Quelle die Bilder sind;
// leer bei Ordnern älterer Fassungen von Lumen.
func TrickplayKennung(ordner string) string {
	b, err := os.ReadFile(filepath.Join(ordner, TrickplayQuelle))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// TrickplayOptionen: was ein Aufrufer zusätzlich mitgeben kann.
type TrickplayOptionen struct {
	// Kennung der Quelldatei (siehe TrickplayQuelle).
	Kennung string
	// Warten wird zwischen zwei Bildern aufgerufen (nur beim Springen):
	// Dort darf der Aufrufer anhalten, etwa solange jemand abspielt.
	// Ein Fehler bricht ab.
	Warten func(context.Context) error
	// Springen erzwingen (true) oder verbieten (false); nil: nach Grösse.
	Springen *bool
}

// trickplaySpringenAb: Ab so vielen Byte je Vorschaubild springt Lumen zu
// jedem Bild, statt die Datei ganz zu lesen. Bei einem Remux (30 GB, zwei
// Stunden, 720 Bilder) sind das rund 40 MB je Bild — die ganze Datei zu
// lesen hiesse auf einer Festplatte Minuten Dauerlast je Film, Springen
// liest nur die 720 Schlüsselbilder. Bei kleinen Dateien ist ein einziger
// Durchgang billiger als 720 Aufrufe von FFmpeg.
const trickplaySpringenAb = 4 << 20

// TrickplayErzeugen rechnet die Blätter in einen frischen Ordner und
// benennt ihn erst um, wenn alles da ist — ein halb fertiger Ordner wird
// nie ausgeliefert. Läuft mit niedrigster Priorität.
func (f *FFmpeg) TrickplayErzeugen(ctx context.Context, quelle string, dauerMS int64,
	d *BildDetails, ziel string) (*TrickplayInfo, error) {
	return f.TrickplayErzeugenMit(ctx, quelle, dauerMS, d, ziel, TrickplayOptionen{})
}

// TrickplayErzeugenMit ist TrickplayErzeugen mit Optionen.
func (f *FFmpeg) TrickplayErzeugenMit(ctx context.Context, quelle string, dauerMS int64,
	d *BildDetails, ziel string, opt TrickplayOptionen) (*TrickplayInfo, error) {
	if dauerMS <= 0 {
		return nil, fmt.Errorf("laufzeit unbekannt")
	}
	breite, hoehe := 0, 0
	if d != nil {
		breite, hoehe = d.Breite, d.Hoehe
	}
	h := TrickplayHoehe(breite, hoehe)
	info := &TrickplayInfo{Intervall: TrickplayIntervall, Breite: TrickplayBreite, Hoehe: h,
		Spalten: TrickplaySpalten, Zeilen: TrickplayZeilen}
	info.Bilder = int(math.Ceil(float64(dauerMS) / 1000 / TrickplayIntervall))
	if info.Bilder < 1 {
		info.Bilder = 1
	}
	proBlatt := TrickplaySpalten * TrickplayZeilen
	info.Blaetter = (info.Bilder + proBlatt - 1) / proBlatt

	if err := os.MkdirAll(filepath.Dir(ziel), 0o755); err != nil {
		return nil, err
	}
	tmp, err := os.MkdirTemp(filepath.Dir(ziel), ".entsteht-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)

	// eof_action=pass: Sonst fällt bei kurzen Dateien das letzte (oder
	// einzige) Bild weg.
	filter := []string{fmt.Sprintf("fps=1/%d:eof_action=pass", TrickplayIntervall),
		fmt.Sprintf("scale=%d:%d", TrickplayBreite, h)}
	if d.HDR() {
		filter = append(filter, f.Faehigkeiten().TonemapKette(d.HLG()))
	} else {
		filter = append(filter, "format=yuvj420p")
	}

	springen := false
	if opt.Springen != nil {
		springen = *opt.Springen
	} else if st, err := os.Stat(quelle); err == nil && st.Size()/int64(info.Bilder) > trickplaySpringenAb {
		springen = true
	}
	if springen {
		// Ohne fps: Jeder Aufruf liefert genau ein Bild.
		if err := f.trickplaySpringen(ctx, quelle, info, strings.Join(filter[1:], ","), tmp, opt.Warten); err != nil {
			return nil, err
		}
	} else {
		filter = append(filter, fmt.Sprintf("tile=%dx%d", TrickplaySpalten, TrickplayZeilen))
		cmd := exec.CommandContext(ctx, f.FFmpegPath, "-hide_banner", "-loglevel", "error", "-nostdin",
			"-skip_frame", "nokey", "-i", quelle,
			"-map", "0:v:0", "-an", "-sn", "-dn",
			"-vf", strings.Join(filter, ","),
			"-q:v", "5", "-start_number", "0", "-y",
			filepath.Join(tmp, "%d.jpg"))
		if out, err := Niedrig(cmd, true); err != nil {
			return nil, fmt.Errorf("ffmpeg: %s", Bereinigt(lastLine(string(out))))
		}
	}
	// Wie viele Blätter es wirklich wurden: Ist die Datei kürzer als ihre
	// Laufzeit vermuten liess, eben weniger.
	n := 0
	for ; ; n++ {
		if _, err := os.Stat(filepath.Join(tmp, fmt.Sprintf("%d.jpg", n))); err != nil {
			break
		}
	}
	if n == 0 {
		return nil, fmt.Errorf("kein einziges vorschaubild")
	}
	if n < info.Blaetter {
		info.Blaetter = n
		if max := n * proBlatt; info.Bilder > max {
			info.Bilder = max
		}
	}
	b, _ := json.Marshal(info)
	if err := os.WriteFile(filepath.Join(tmp, "info.json"), b, 0o644); err != nil {
		return nil, err
	}
	if opt.Kennung != "" {
		if err := os.WriteFile(filepath.Join(tmp, TrickplayQuelle), []byte(opt.Kennung), 0o644); err != nil {
			return nil, err
		}
	}
	_ = os.RemoveAll(ziel)
	if err := os.Rename(tmp, ziel); err != nil {
		return nil, err
	}
	return info, nil
}

// trickplaySpringen holt jedes Bild einzeln: zur Stelle springen (-ss vor
// -i), das Schlüsselbild davor nehmen (-noaccurate_seek, -copyts), nur
// Schlüsselbilder decodieren. Gelesen wird so nur, was um die Stellen
// herum liegt. Danach setzt ein letzter Aufruf je hundert Bilder zu einem
// Blatt zusammen. Geht ein einzelnes Bild nicht (etwa ganz am Ende), steht
// dort das vorige — die Kacheln bleiben an ihrem Platz.
func (f *FFmpeg) trickplaySpringen(ctx context.Context, quelle string, info *TrickplayInfo,
	filter, tmp string, warten func(context.Context) error) error {
	einzeln := filepath.Join(tmp, "einzeln")
	if err := os.MkdirAll(einzeln, 0o755); err != nil {
		return err
	}
	bild := func(i int) string { return filepath.Join(einzeln, fmt.Sprintf("%06d.jpg", i)) }
	gelungen := 0
	var letzterFehler string
	for i := 0; i < info.Bilder; i++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if warten != nil {
			if err := warten(ctx); err != nil {
				return err
			}
		}
		stelle := fmt.Sprintf("%d", i*TrickplayIntervall)
		cmd := exec.CommandContext(ctx, f.FFmpegPath, "-hide_banner", "-loglevel", "error", "-nostdin",
			"-skip_frame", "nokey", "-noaccurate_seek", "-ss", stelle, "-i", quelle,
			// -copyts: Das Schlüsselbild vor der Stelle hätte sonst eine
			// negative Zeit, FFmpeg verwürfe es und läse bis zum nächsten —
			// bei einem Remux viele Megabyte je Bild.
			"-copyts",
			"-map", "0:v:0", "-an", "-sn", "-dn", "-frames:v", "1",
			"-vf", filter, "-q:v", "5", "-update", "1", "-y", bild(i))
		out, err := Niedrig(cmd, true)
		if st, serr := os.Stat(bild(i)); err == nil && serr == nil && st.Size() > 0 {
			gelungen++
			continue
		}
		if err != nil {
			letzterFehler = lastLine(string(out))
		}
		_ = os.Remove(bild(i))
		if i > 0 {
			if b, err := os.ReadFile(bild(i - 1)); err == nil {
				_ = os.WriteFile(bild(i), b, 0o644)
			}
		}
	}
	if gelungen == 0 {
		return fmt.Errorf("ffmpeg: %s", Bereinigt(letzterFehler))
	}
	// Fehlt das allererste Bild, nimmt es das erste gelungene.
	if _, err := os.Stat(bild(0)); err != nil {
		for i := 1; i < info.Bilder; i++ {
			if b, err := os.ReadFile(bild(i)); err == nil {
				for j := 0; j < i; j++ {
					_ = os.WriteFile(bild(j), b, 0o644)
				}
				break
			}
		}
	}
	proBlatt := TrickplaySpalten * TrickplayZeilen
	for blatt := 0; blatt*proBlatt < info.Bilder; blatt++ {
		n := min(proBlatt, info.Bilder-blatt*proBlatt)
		cmd := exec.CommandContext(ctx, f.FFmpegPath, "-hide_banner", "-loglevel", "error", "-nostdin",
			"-start_number", fmt.Sprintf("%d", blatt*proBlatt), "-i", filepath.Join(einzeln, "%06d.jpg"),
			"-vf", fmt.Sprintf("trim=end_frame=%d,tile=%dx%d", n, TrickplaySpalten, TrickplayZeilen), "-frames:v", "1",
			"-q:v", "5", "-update", "1", "-y", filepath.Join(tmp, fmt.Sprintf("%d.jpg", blatt)))
		if out, err := Niedrig(cmd, true); err != nil {
			return fmt.Errorf("ffmpeg: %s", Bereinigt(lastLine(string(out))))
		}
	}
	return os.RemoveAll(einzeln)
}
