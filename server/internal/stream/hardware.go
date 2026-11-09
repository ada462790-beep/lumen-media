// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package stream

// Hardware-Beschleunigung beim Umwandeln.
//
// Ein NAS mit Celeron schafft in Software kaum einen 1080p-Film in
// Echtzeit, einen 4K-HDR-Film gar nicht. Fast jeder solche Prozessor hat
// aber einen Videoteil, der das nebenbei erledigt: Intel und AMD über
// VAAPI (/dev/dri — so bei Synology), Intel zusätzlich über Quick Sync,
// NVIDIA über NVENC.
//
// Ob es geht, weiss man erst, wenn man es versucht: Treiber fehlen, der
// Container sieht /dev/dri nicht, die Gruppe stimmt nicht. Darum probiert
// Lumen jedes Verfahren beim Start mit einem winzigen Testbild aus, statt
// nach Dateien im System zu raten. Und weil auch ein Verfahren, das beim
// Test lief, an einer bestimmten Datei scheitern kann (ein Codec, den der
// Videoteil nicht kennt), fällt eine Wiedergabe dann von selbst auf
// Software zurück — der Film läuft, nur langsamer.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Die Verfahren. „aus" heisst Software, „auto" nimmt das erste, das geht.
const (
	HWAus   = "aus"
	HWAuto  = "auto"
	HWVAAPI = "vaapi"
	HWQSV   = "qsv"
	HWNVENC = "nvenc"
)

// HWVerfahren sind alle wählbaren Verfahren, in der Reihenfolge, in der
// „auto" sie versucht. NVENC zuerst: wer eine NVIDIA-Karte durchreicht,
// will sie nutzen. Dann VAAPI vor Quick Sync, weil es auf Intel und AMD
// mit demselben Treiber läuft und auf einem Synology meist das einzige
// ist, was ohne weitere Bibliotheken geht.
var HWVerfahren = []string{HWNVENC, HWVAAPI, HWQSV}

// GueltigeWahl sagt, ob eine Einstellung erlaubt ist.
func GueltigeWahl(w string) bool {
	switch w {
	case HWAus, HWAuto, HWVAAPI, HWQSV, HWNVENC:
		return true
	}
	return false
}

// Hardware hält, was erkannt wurde und was gewählt ist.
type Hardware struct {
	ff *FFmpeg

	mu         sync.Mutex
	wahl       string
	geraet     string // Render-Knoten für VAAPI/QSV
	geprueft   bool
	pruefung   bool // läuft gerade
	geht       map[string]bool
	tonemap    map[string]bool // Tonemapping im Videoteil möglich
	fehlFolge  map[string]int  // Fehlschläge hintereinander
	gesperrt   map[string]time.Time
	letzteMeld string
}

// NeueHardware legt die Erkennung an. geraet darf leer sein, dann wird der
// erste Render-Knoten unter /dev/dri genommen.
func NeueHardware(ff *FFmpeg, wahl, geraet string) *Hardware {
	if !GueltigeWahl(wahl) {
		wahl = HWAuto
	}
	if geraet == "" {
		geraet = renderKnoten()
	}
	return &Hardware{ff: ff, wahl: wahl, geraet: geraet, geht: map[string]bool{},
		tonemap: map[string]bool{}, fehlFolge: map[string]int{}, gesperrt: map[string]time.Time{}}
}

func renderKnoten() string {
	treffer, _ := filepath.Glob("/dev/dri/renderD*")
	sort.Strings(treffer)
	if len(treffer) > 0 {
		return treffer[0]
	}
	return "/dev/dri/renderD128"
}

// Setzen ändert die Wahl. Gesperrte Verfahren bekommen eine neue Chance —
// wer umstellt, hat meist gerade etwas repariert.
func (h *Hardware) Setzen(wahl string) {
	if h == nil || !GueltigeWahl(wahl) {
		return
	}
	h.mu.Lock()
	h.wahl = wahl
	h.fehlFolge = map[string]int{}
	h.gesperrt = map[string]time.Time{}
	h.mu.Unlock()
}

// Wahl ist die eingestellte Wahl.
func (h *Hardware) Wahl() string {
	if h == nil {
		return HWAus
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.wahl
}

// Pruefen probiert jedes Verfahren aus. Dauert ein paar Sekunden; läuft
// beim Start im Hintergrund und auf Knopfdruck in der Verwaltung.
func (h *Hardware) Pruefen(ctx context.Context) {
	if h == nil || h.ff == nil || !h.ff.Available() {
		return
	}
	h.mu.Lock()
	if h.pruefung {
		h.mu.Unlock()
		return
	}
	h.pruefung = true
	geraet := h.geraet
	h.mu.Unlock()

	kann := h.ff.Faehigkeiten()
	geht := map[string]bool{}
	tm := map[string]bool{}
	for _, v := range HWVerfahren {
		if !kann.Kodierer["h264_"+v] {
			continue
		}
		if (v == HWVAAPI || v == HWQSV) && !gibtEs(geraet) {
			continue
		}
		if probeKodieren(ctx, h.ff.FFmpegPath, probeArgs(v, geraet, false)) {
			geht[v] = true
			if v != HWNVENC && kann.Filter["tonemap_vaapi"] {
				tm[v] = probeKodieren(ctx, h.ff.FFmpegPath, probeArgs(v, geraet, true))
			}
		}
	}
	h.mu.Lock()
	h.geht, h.tonemap, h.geprueft, h.pruefung = geht, tm, true, false
	h.fehlFolge = map[string]int{}
	h.gesperrt = map[string]time.Time{}
	h.mu.Unlock()
}

func gibtEs(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// probeArgs: ein Bruchteil einer Sekunde Testbild, umgewandelt ins Nichts.
func probeArgs(v, geraet string, tonemap bool) []string {
	quelle := "testsrc2=size=320x240:rate=25:duration=0.4"
	if tonemap {
		quelle += ",format=p010le,setparams=color_primaries=bt2020:color_trc=smpte2084:colorspace=bt2020nc"
	}
	a := []string{"-hide_banner", "-loglevel", "error", "-nostdin"}
	switch v {
	case HWVAAPI:
		a = append(a, "-init_hw_device", "vaapi=va:"+geraet, "-filter_hw_device", "va",
			"-f", "lavfi", "-i", quelle)
		if tonemap {
			a = append(a, "-vf", "hwupload,tonemap_vaapi=format=nv12:t=bt709:m=bt709:p=bt709")
		} else {
			a = append(a, "-vf", "format=nv12,hwupload")
		}
		a = append(a, "-c:v", "h264_vaapi")
	case HWQSV:
		a = append(a, "-init_hw_device", "vaapi=va:"+geraet, "-init_hw_device", "qsv=qs@va",
			"-filter_hw_device", "va", "-f", "lavfi", "-i", quelle)
		if tonemap {
			a = append(a, "-vf", "hwupload,tonemap_vaapi=format=nv12:t=bt709:m=bt709:p=bt709,hwmap=derive_device=qsv,format=qsv")
		} else {
			a = append(a, "-vf", "format=nv12,hwupload,hwmap=derive_device=qsv,format=qsv")
		}
		a = append(a, "-c:v", "h264_qsv")
	case HWNVENC:
		a = append(a, "-f", "lavfi", "-i", quelle, "-pix_fmt", "yuv420p", "-c:v", "h264_nvenc")
	}
	return append(a, "-frames:v", "5", "-f", "null", "-")
}

func probeKodieren(ctx context.Context, ffmpeg string, args []string) bool {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, ffmpeg, args...).Run() == nil
}

// Aktiv ist das Verfahren, mit dem jetzt umgewandelt wird — "" heisst
// Software.
func (h *Hardware) Aktiv() string {
	if h == nil {
		return ""
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.aktivGesperrt()
}

func (h *Hardware) aktivGesperrt() string {
	frei := func(v string) bool {
		bis, ok := h.gesperrt[v]
		return h.geht[v] && (!ok || time.Now().After(bis))
	}
	switch h.wahl {
	case HWAus:
		return ""
	case HWAuto:
		for _, v := range HWVerfahren {
			if frei(v) {
				return v
			}
		}
		return ""
	default:
		if frei(h.wahl) {
			return h.wahl
		}
		return ""
	}
}

// TonemapImVideoteil sagt, ob das Verfahren HDR selbst umrechnen kann.
func (h *Hardware) TonemapImVideoteil(v string) bool {
	if h == nil {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.tonemap[v]
}

// Geraet ist der Render-Knoten für VAAPI und Quick Sync.
func (h *Hardware) Geraet() string {
	if h == nil {
		return ""
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.geraet
}

// Fehlschlag merkt sich, dass ein Verfahren an einer Datei gescheitert
// ist. Drei hintereinander, und es ruht eine Stunde: dann ist eher der
// Treiber kaputt als die Datei, und jede Wiedergabe liefe sonst erst
// gegen die Wand, bevor sie auf Software zurückfällt. Gibt eine Meldung
// fürs Protokoll zurück, wenn das Verfahren gerade gesperrt wurde.
func (h *Hardware) Fehlschlag(v string) string {
	if h == nil || v == "" {
		return ""
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.fehlFolge[v]++
	if h.fehlFolge[v] >= 3 {
		h.fehlFolge[v] = 0
		h.gesperrt[v] = time.Now().Add(time.Hour)
		return "Die Hardware-Beschleunigung (" + strings.ToUpper(v) +
			") ist dreimal hintereinander gescheitert und ruht für eine Stunde — umgewandelt wird so lange in Software"
	}
	return ""
}

// Erfolg setzt die Zählung zurück.
func (h *Hardware) Erfolg(v string) {
	if h == nil || v == "" {
		return
	}
	h.mu.Lock()
	h.fehlFolge[v] = 0
	h.mu.Unlock()
}

// HardwareZustand ist, was die Verwaltung anzeigt.
type HardwareZustand struct {
	Wahl       string          `json:"wahl"`
	Aktiv      string          `json:"aktiv"` // "" = Software
	Verfuegbar []string        `json:"verfuegbar"`
	Tonemap    map[string]bool `json:"tonemapHardware"`
	Geprueft   bool            `json:"geprueft"`
	Pruefung   bool            `json:"pruefungLaeuft"`
	Geraet     string          `json:"geraet"`
	Gesperrt   []string        `json:"ruht,omitempty"`
}

// Zustand fasst zusammen, was erkannt wurde.
func (h *Hardware) Zustand() HardwareZustand {
	if h == nil {
		return HardwareZustand{Wahl: HWAus, Verfuegbar: []string{}, Tonemap: map[string]bool{}}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	z := HardwareZustand{Wahl: h.wahl, Aktiv: h.aktivGesperrt(), Geprueft: h.geprueft,
		Pruefung: h.pruefung, Geraet: h.geraet, Verfuegbar: []string{}, Tonemap: map[string]bool{}}
	for _, v := range HWVerfahren {
		if h.geht[v] {
			z.Verfuegbar = append(z.Verfuegbar, v)
			z.Tonemap[v] = h.tonemap[v]
		}
		if bis, ok := h.gesperrt[v]; ok && time.Now().Before(bis) {
			z.Gesperrt = append(z.Gesperrt, v)
		}
	}
	return z
}

// setzeErkannt ist für Tests: so tun, als liefe ein Verfahren.
func (h *Hardware) setzeErkannt(v string, tonemap bool) {
	h.mu.Lock()
	h.geht[v] = true
	h.tonemap[v] = tonemap
	h.geprueft = true
	h.mu.Unlock()
}
