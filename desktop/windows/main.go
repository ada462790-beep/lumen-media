// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

// Lumen für Windows — ein Fenster für die Web-App eines Lumen-Servers.
//
// Die Oberfläche bleibt die des Servers: Was dort neu dazukommt, hat die
// App ohne eigenes Update. Die Hülle selbst kümmert sich nur um das, was
// ein Browser-Tab nicht kann — mehrere Server merken und wechseln, ein
// eigenes Fenster mit Symbol in der Taskleiste, Vollbild über das ganze
// Fenster, Medientasten, Links im Standardbrowser und ein Hinweis, wenn
// es eine neue Fassung gibt.
//
// Gezeichnet wird mit WebView2, dem Chromium von Windows, das auf jedem
// Windows 10/11 ohnehin bereitliegt. Darum ist das Programm klein: kein
// eigener Browser, kein Electron.
package main

import (
	"flag"
	"fmt"
	"os"
)

// version kommt beim Bauen aus dem Tag der Veröffentlichung:
//
//	go build -ldflags "-X main.version=0.4.0"
//
// „dev" heisst: selbst gebaut. Dann fragt Lumen nie nach Updates.
var version = "dev"

// Wo die Fassungen liegen (Update-Hinweis).
const repository = "ada462790-beep/lumen-media"

// Standardanschluss eines Lumen-Servers.
const standardPort = "8097"

func main() {
	zeigeVersion := flag.Bool("version", false, "Fassung ausgeben und beenden")
	selbsttest := flag.String("selbsttest", "", "Fenster öffnen, Bilder in diesen Ordner schreiben, beenden (für die Prüfung beim Bauen)")
	debug := flag.Bool("debug", false, "Entwicklerwerkzeuge (F12) erlauben")
	flag.Parse()

	if *zeigeVersion {
		fmt.Println("Lumen für Windows", version)
		return
	}
	os.Exit(starten(optionen{selbsttest: *selbsttest, debug: *debug}))
}

type optionen struct {
	selbsttest string
	debug      bool
}
