// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

// ressourcen legt aus dem Lumen-Symbol an, was Windows an einer .exe
// sehen will, und das Symbol für den Installer:
//
//	go run ./werkzeuge/ressourcen -version 0.4.0
//
// Es schreibt
//
//	rsrc_windows_amd64.syso  Symbol (Nummer 1), Manifest, Versionsangaben —
//	                         go build bindet die Datei von selbst ein
//	installer/lumen.ico      dasselbe Symbol für den Installer (NSIS)
//
// Beides entsteht beim Bauen und steht nicht im Repository: Eine
// Binärdatei im Quelltext kann niemand nachlesen.
//
// Das Manifest sagt Windows: Lumen kann mit hoher Auflösung je Bildschirm
// umgehen (sonst wäre alles unscharf hochgerechnet), braucht keine
// Administratorrechte und ist für Windows 10 und neuer gebaut.
package main

import (
	"flag"
	"fmt"
	"image/png"
	"os"
	"strings"

	"github.com/tc-hib/winres"
	"github.com/tc-hib/winres/version"
)

func main() {
	fassung := flag.String("version", "0.0.0", "Fassung (ohne v)")
	symbol := flag.String("symbol", "seite/symbol.png", "Lumen-Symbol als PNG (512×512)")
	syso := flag.String("syso", "rsrc_windows_amd64.syso", "Ausgabe für go build")
	ico := flag.String("ico", "installer/lumen.ico", "Ausgabe für den Installer")
	flag.Parse()
	if err := bauen(*fassung, *symbol, *syso, *ico); err != nil {
		fmt.Fprintln(os.Stderr, "ressourcen:", err)
		os.Exit(1)
	}
}

func bauen(fassung, symbolPfad, sysoPfad, icoPfad string) error {
	f, err := os.Open(symbolPfad)
	if err != nil {
		return err
	}
	defer f.Close()
	bild, err := png.Decode(f)
	if err != nil {
		return err
	}
	// Die Grössen, die Windows tatsächlich anfragt: Taskleiste und
	// Startmenü (bis 48 bei 200 %), Explorer-Ansichten bis 256.
	symbol, err := winres.NewIconFromResizedImage(bild, []int{256, 128, 64, 48, 40, 32, 24, 20, 16})
	if err != nil {
		return err
	}

	rs := &winres.ResourceSet{}
	if err := rs.SetIcon(winres.ID(1), symbol); err != nil {
		return err
	}

	v := zahlen(fassung)
	rs.SetManifest(winres.AppManifest{
		Identity:            winres.AssemblyIdentity{Name: "Lumen.Desktop", Version: v},
		Description:         "Lumen",
		Compatibility:       winres.Win10AndAbove,
		ExecutionLevel:      winres.AsInvoker,
		DPIAwareness:        winres.DPIPerMonitorV2,
		UseCommonControlsV6: true,
		LongPathAware:       true,
	})

	vi := version.Info{FileVersion: v, ProductVersion: v}
	for k, w := range map[string]string{
		version.ProductName:      "Lumen",
		version.FileDescription:  "Lumen",
		version.CompanyName:      "Die Lumen-Autoren",
		version.LegalCopyright:   "© 2026 Die Lumen-Autoren · AGPL-3.0-only",
		version.OriginalFilename: "Lumen.exe",
		version.InternalName:     "Lumen",
		version.ProductVersion:   fassung,
		version.FileVersion:      fassung,
	} {
		if err := vi.Set(version.LangDefault, k, w); err != nil {
			return err
		}
	}
	rs.SetVersionInfo(vi)

	out, err := os.Create(sysoPfad)
	if err != nil {
		return err
	}
	if err := rs.WriteObject(out, winres.ArchAMD64); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}

	ico, err := os.Create(icoPfad)
	if err != nil {
		return err
	}
	if err := symbol.SaveICO(ico); err != nil {
		ico.Close()
		return err
	}
	return ico.Close()
}

// zahlen macht aus „0.4.0" oder „dev" vier Zahlen für Windows.
func zahlen(fassung string) [4]uint16 {
	var v [4]uint16
	for i, t := range strings.SplitN(strings.TrimPrefix(fassung, "v"), ".", 4) {
		var n uint16
		fmt.Sscanf(t, "%d", &n)
		v[i] = n
	}
	return v
}
