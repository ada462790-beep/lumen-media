// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package lokal

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Ordner ist der Inhalt eines Ordners, einmal gelesen. Gesucht wird ohne
// Rücksicht auf Gross- und Kleinschreibung — auf einem NAS liegt mal
// „Poster.JPG", mal „poster.jpg" —, und für jeden Ordner nur einmal: Eine
// Serie mit 200 Folgen in einem Ordner soll ihn nicht 200-mal lesen.
type Ordner struct {
	Pfad  string
	namen map[string]string // klein → wie auf der Platte
}

// LiesOrdner liest einen Ordner. Nur gewöhnliche Dateien zählen: Ein
// Verweis (Symlink) könnte auf jede Datei des Rechners zeigen, und Lumen
// kopierte sie in seinen Bilderordner.
func LiesOrdner(pfad string) *Ordner {
	o := &Ordner{Pfad: pfad, namen: map[string]string{}}
	eintraege, err := os.ReadDir(pfad)
	if err != nil {
		return o
	}
	for _, e := range eintraege {
		if e.Type().IsRegular() {
			o.namen[strings.ToLower(e.Name())] = e.Name()
		}
	}
	return o
}

// Namen nennt die Dateien im Ordner, wie sie auf der Platte heissen.
func (o *Ordner) Namen() []string {
	if o == nil {
		return nil
	}
	out := make([]string, 0, len(o.namen))
	for _, n := range o.namen {
		out = append(out, n)
	}
	return out
}

// Finde gibt den vollen Pfad einer Datei im Ordner zurück, oder "".
func (o *Ordner) Finde(name string) string {
	if o == nil {
		return ""
	}
	if n, ok := o.namen[strings.ToLower(name)]; ok {
		return filepath.Join(o.Pfad, n)
	}
	return ""
}

// bildEndungen in der Reihenfolge, in der sie gelten, wenn mehrere da sind.
var bildEndungen = []string{".jpg", ".jpeg", ".png", ".webp"}

// FindeBild sucht „<stamm>.jpg", „.jpeg", „.png", „.webp".
func (o *Ordner) FindeBild(staemme ...string) string {
	for _, s := range staemme {
		for _, e := range bildEndungen {
			if p := o.Finde(s + e); p != "" {
				return p
			}
		}
	}
	return ""
}

// NFOFuerFilm: „<Dateiname>.nfo" neben der Datei, sonst „movie.nfo" — das
// nur, wenn der Ordner diesem Film allein gehört.
func (o *Ordner) NFOFuerFilm(stamm string, eigenerOrdner bool) string {
	if p := o.Finde(stamm + ".nfo"); p != "" {
		return p
	}
	if eigenerOrdner {
		return o.Finde("movie.nfo")
	}
	return ""
}

// BilderFuerFilm sucht Poster, Hintergrund, Querformat und Logo eines
// Films. Die Namen, die nur den Ordner meinen („poster.jpg",
// „folder.jpg"), gelten nur, wenn der Ordner dem Film allein gehört —
// sonst bekäme jeder Film in „Filme/" dasselbe Poster.
func (o *Ordner) BilderFuerFilm(stamm string, eigenerOrdner bool) map[string]string {
	out := map[string]string{}
	setze := func(art string, mitStamm []string, ohne []string) {
		var staemme []string
		for _, s := range mitStamm {
			staemme = append(staemme, stamm+s)
		}
		if eigenerOrdner {
			staemme = append(staemme, ohne...)
		}
		if p := o.FindeBild(staemme...); p != "" {
			out[art] = p
		}
	}
	setze("poster", []string{"-poster", ""}, []string{"poster", "folder", "cover", "movie"})
	setze("backdrop", []string{"-fanart", "-backdrop", "-background", "-fanart1", "-backdrop1"},
		[]string{"fanart", "backdrop", "background", "fanart1", "backdrop1", "fanart-1"})
	setze("landscape", []string{"-landscape", "-thumb"}, []string{"landscape", "thumb"})
	setze("logo", []string{"-logo", "-clearlogo"}, []string{"logo", "clearlogo"})
	return out
}

// BilderFuerSerie: dieselben Namen im Serienordner.
func (o *Ordner) BilderFuerSerie() map[string]string {
	out := map[string]string{}
	for art, staemme := range map[string][]string{
		"poster":    {"poster", "folder", "cover", "show"},
		"backdrop":  {"fanart", "backdrop", "background", "fanart1", "backdrop1", "fanart-1"},
		"landscape": {"landscape", "thumb"},
		"logo":      {"logo", "clearlogo"},
	} {
		if p := o.FindeBild(staemme...); p != "" {
			out[art] = p
		}
	}
	return out
}

// PosterFuerStaffel: „season01-poster.jpg" (oder „season-specials-poster"
// für Staffel 0) im Serienordner, sonst „poster.jpg"/„folder.jpg" im
// Staffelordner selbst.
func PosterFuerStaffel(serie, staffel *Ordner, nr int) string {
	staemme := []string{fmt.Sprintf("season%02d-poster", nr), fmt.Sprintf("season%d-poster", nr)}
	if nr == 0 {
		staemme = append([]string{"season-specials-poster"}, staemme...)
	}
	if p := serie.FindeBild(staemme...); p != "" {
		return p
	}
	if staffel != nil && serie != nil && staffel.Pfad != serie.Pfad {
		return staffel.FindeBild("poster", "folder", "cover")
	}
	return ""
}

// VorschauFuerFolge: „<Folgendatei>-thumb.jpg" (Kodi), „<Folgendatei>.jpg".
func (o *Ordner) VorschauFuerFolge(stamm string) string {
	return o.FindeBild(stamm+"-thumb", stamm)
}

// VorschauFuerFolgeIn sucht zuerst neben der Folge, dann in „metadata/"
// darunter — dort legt Emby seine Folgenbilder ab („metadata/<Folge>.jpg").
func VorschauFuerFolgeIn(o, metadata *Ordner, stamm string) string {
	if p := o.VorschauFuerFolge(stamm); p != "" {
		return p
	}
	return metadata.FindeBild(stamm, stamm+"-thumb")
}
