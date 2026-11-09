// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package stream

// Was das FFmpeg auf diesem Rechner kann.
//
// FFmpeg kommt auf jedem NAS anders gebaut: das aus dem Docker-Abbild
// bringt zscale mit, das aus dem Paketmanager eines Synology vielleicht
// nicht, ein selbst gebautes hat womöglich gar keinen tonemap-Filter.
// Statt von einer Fassung auszugehen, fragt Lumen beim ersten Bedarf nach
// und wählt danach den besten Weg, den es gibt.

import (
	"context"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Faehigkeiten sind die Filter und Kodierer, die dieses FFmpeg kennt.
type Faehigkeiten struct {
	Filter   map[string]bool
	Kodierer map[string]bool
	Gelesen  bool
}

var (
	faehigMu sync.Mutex
	faehig   = map[string]*Faehigkeiten{}
)

// Faehigkeiten liest einmal je FFmpeg aus, was es kann.
func (f *FFmpeg) Faehigkeiten() *Faehigkeiten {
	faehigMu.Lock()
	defer faehigMu.Unlock()
	if c, ok := faehig[f.FFmpegPath]; ok {
		return c
	}
	c := &Faehigkeiten{Filter: map[string]bool{}, Kodierer: map[string]bool{}}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, f.FFmpegPath, "-hide_banner", "-filters").Output(); err == nil {
		c.Filter = namenSpalte(string(out))
		c.Gelesen = true
	}
	if out, err := exec.CommandContext(ctx, f.FFmpegPath, "-hide_banner", "-encoders").Output(); err == nil {
		c.Kodierer = namenSpalte(string(out))
	}
	faehig[f.FFmpegPath] = c
	return c
}

// namenSpalte nimmt aus der Liste von FFmpeg die zweite Spalte — dort
// steht der Name; davor die Kennbuchstaben.
func namenSpalte(s string) map[string]bool {
	out := map[string]bool{}
	for _, z := range strings.Split(s, "\n") {
		felder := strings.Fields(z)
		if len(felder) >= 2 && !strings.HasSuffix(felder[0], ":") && felder[0] != "------" {
			out[felder[1]] = true
		}
	}
	return out
}

// ---------------------------------------------------------------- Farbe

// TonemapKette ist die Filterkette, die ein HDR-Bild für einen gewöhnlichen
// Bildschirm umrechnet, mit dem besten Werkzeug, das da ist:
//
//   - zscale und tonemap: in lineares Licht, Farbraum BT.709, Helligkeit
//     mit Hable zusammendrücken, zurück. So machen es alle, die es richtig
//     machen; das Bild sieht danach aus wie die SDR-Fassung einer Blu-ray.
//   - nur colorspace: wenigstens die Farben stimmen (BT.2020 → BT.709),
//     die Helligkeit nicht ganz — besser als das blasse, grünstichige Bild,
//     das ohne jede Umrechnung herauskommt.
//   - nichts davon: nur auf 8 Bit bringen.
//
// Das Ergebnis endet immer in yuv420p.
func (c *Faehigkeiten) TonemapKette(hlg bool) string {
	switch {
	case c.Filter["zscale"] && c.Filter["tonemap"]:
		eingang := "zscale=t=linear:npl=100"
		if hlg {
			// HLG trägt keine Helligkeitsangabe; ohne tin= rät zscale.
			eingang = "zscale=tin=arib-std-b67:t=linear:npl=100"
		}
		return eingang + ",format=gbrpf32le,zscale=p=bt709," +
			"tonemap=tonemap=hable:desat=0,zscale=t=bt709:m=bt709:r=tv,format=yuv420p"
	case c.Filter["colorspace"]:
		return "colorspace=all=bt709:iall=bt2020:itrc=bt2020-10:fast=1:format=yuv420p,format=yuv420p"
	}
	return "format=yuv420p"
}

// FarbKennzeichnung sind die Angaben, mit denen ein SDR-Bild ausdrücklich
// als BT.709 gekennzeichnet wird. Ohne sie raten Fernseher — und nach
// einem Tonemapping raten sie oft falsch (die Quelle sagte ja BT.2020).
func FarbKennzeichnung() []string {
	return []string{"-color_primaries", "bt709", "-color_trc", "bt709", "-colorspace", "bt709"}
}
