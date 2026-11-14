// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package scheibe

// ISO-Abbilder liest 7-Zip (Alpine-Paket „7zip", Programm „7zz").
//
// Warum 7-Zip: Eine Video-DVD ist meist ein „UDF-Bridge"-Abbild (UDF und
// ISO 9660), eine Blu-ray dagegen reines UDF 2.50 — ohne ISO-9660-Teil.
// bsdtar (libarchive) und xorriso lesen nur ISO 9660, an einer Blu-ray-ISO
// scheitern sie. 7-Zip liest beides, ist 1,7 MB gross und hängt nur an
// musl und libstdc++. Alpine baut es ohne den unfreien RAR-Teil.
//
// Ausgepackt wird in einen Arbeitsordner unter Lumens Datenordner, nie in
// den Medienordner: erst die kleinen Steuerdateien (IFO, Playlists), dann
// nur die Dateien des Hauptfilms.

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

// SiebenZip sucht 7-Zip ("" wenn keins da ist).
func SiebenZip() string {
	for _, n := range []string{"7zz", "7z", "7za"} {
		if p, err := exec.LookPath(n); err == nil {
			return p
		}
	}
	return ""
}

// IsoEintrag ist eine Datei im Abbild.
type IsoEintrag struct {
	Pfad    string // mit „/", wie im Abbild
	Groesse int64
}

// IsoInhalt: die Dateien eines Abbilds, wie 7-Zip sie auflistet.
type IsoInhalt struct {
	Art       string // DVD oder BluRay
	Eintraege []IsoEintrag
}

// IsoLesen listet ein Abbild auf und erkennt, ob es eine DVD (VIDEO_TS)
// oder eine Blu-ray (BDMV) ist.
func IsoLesen(ctx context.Context, siebenZip, iso string) (*IsoInhalt, error) {
	cmd := exec.CommandContext(ctx, siebenZip, "l", "-slt", "-ba", "--", iso)
	var fehl bytes.Buffer
	cmd.Stderr = &fehl
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("7-zip kann das abbild nicht lesen: %s", letzteZeile(fehl.String(), err))
	}
	inhalt := &IsoInhalt{Eintraege: listeLesen(out, iso)}
	for _, e := range inhalt.Eintraege {
		teile := strings.Split(strings.ToUpper(e.Pfad), "/")
		if len(teile) == 2 && teile[0] == "VIDEO_TS" && teile[1] == "VIDEO_TS.IFO" {
			inhalt.Art = DVD
		}
		if len(teile) >= 3 && teile[0] == "BDMV" && inhalt.Art == "" &&
			(teile[1] == "PLAYLIST" || teile[1] == "STREAM") {
			inhalt.Art = BluRay
		}
	}
	if inhalt.Art == "" {
		// Ein VIDEO_TS ohne VIDEO_TS.IFO ist auch eine DVD — nur ohne Plan.
		for _, e := range inhalt.Eintraege {
			if reVOB.MatchString(path.Base(e.Pfad)) && strings.EqualFold(path.Dir(e.Pfad), "VIDEO_TS") {
				inhalt.Art = DVD
				break
			}
		}
	}
	if inhalt.Art == "" {
		return nil, errors.New("in diesem abbild steckt keine dvd und keine blu-ray")
	}
	return inhalt, nil
}

// listeLesen liest die Ausgabe von „7z l -slt": Blöcke aus „Schlüssel = Wert".
func listeLesen(out []byte, iso string) []IsoEintrag {
	var liste []IsoEintrag
	var pfad string
	var groesse int64 = -1
	ordner := false
	fertig := func() {
		if pfad != "" && groesse >= 0 && !ordner && !filepath.IsAbs(pfad) && pfad != iso {
			p := strings.ReplaceAll(pfad, `\`, "/")
			p = strings.TrimPrefix(path.Clean("/"+p), "/")
			if p != "" && p != "." {
				liste = append(liste, IsoEintrag{Pfad: p, Groesse: groesse})
			}
		}
		pfad, groesse, ordner = "", -1, false
	}
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		z := sc.Text()
		k, v, ok := strings.Cut(z, " = ")
		if !ok {
			if strings.TrimSpace(z) == "" {
				fertig()
			}
			continue
		}
		switch k {
		case "Path":
			fertig()
			pfad = v
		case "Size":
			groesse, _ = strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		case "Folder":
			ordner = strings.TrimSpace(v) == "+"
		case "Attributes":
			if strings.HasPrefix(strings.TrimSpace(v), "D") {
				ordner = true
			}
		}
	}
	fertig()
	return liste
}

// Ordner: die Dateien direkt in einem Ordner des Abbilds („VIDEO_TS",
// „BDMV/STREAM"), mit dem Namen ohne Ordner.
func (i *IsoInhalt) Ordner(name string) []Datei {
	var out []Datei
	for _, e := range i.Eintraege {
		if strings.EqualFold(path.Dir(e.Pfad), name) {
			out = append(out, Datei{Name: path.Base(e.Pfad), Groesse: e.Groesse})
		}
	}
	return out
}

// Pfad: der Pfad im Abbild zu einer Datei eines Ordners.
func (i *IsoInhalt) Pfad(ordner, name string) string {
	for _, e := range i.Eintraege {
		if strings.EqualFold(path.Dir(e.Pfad), ordner) && path.Base(e.Pfad) == name {
			return e.Pfad
		}
	}
	return ordner + "/" + name
}

// Steuerdateien: was vor dem Plan ausgepackt sein muss — die IFO-Dateien
// der DVD bzw. die Playlists der Blu-ray. Klein (Kilobytes).
func (i *IsoInhalt) Steuerdateien() []string {
	var out []string
	for _, e := range i.Eintraege {
		gross := strings.ToUpper(e.Pfad)
		switch {
		case i.Art == DVD && strings.HasPrefix(gross, "VIDEO_TS/") && strings.HasSuffix(gross, ".IFO"):
			out = append(out, e.Pfad)
		case i.Art == BluRay && strings.HasPrefix(gross, "BDMV/PLAYLIST/") && strings.HasSuffix(gross, ".MPLS"):
			out = append(out, e.Pfad)
		}
	}
	return out
}

// Auspacken packt die genannten Dateien (Pfade im Abbild) in den Ordner
// ziel aus. Wildcards sind aus (-spd): Ein Name mit „*" oder „[" ist ein Name.
func Auspacken(ctx context.Context, siebenZip, iso, ziel string, pfade []string) error {
	if len(pfade) == 0 {
		return nil
	}
	args := []string{"x", "-y", "-bd", "-bso0", "-bsp0", "-spd", "-o" + ziel, "--", iso}
	args = append(args, pfade...)
	cmd := exec.CommandContext(ctx, siebenZip, args...)
	var fehl bytes.Buffer
	cmd.Stderr = &fehl
	cmd.Stdout = &fehl
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("7-zip: %s", letzteZeile(fehl.String(), err))
	}
	return nil
}

func letzteZeile(s string, err error) string {
	zeilen := strings.Split(strings.TrimSpace(s), "\n")
	for i := len(zeilen) - 1; i >= 0; i-- {
		if z := strings.TrimSpace(zeilen[i]); z != "" {
			return z
		}
	}
	return err.Error()
}
