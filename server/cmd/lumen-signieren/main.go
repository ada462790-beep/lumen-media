// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

// lumen-signieren unterschreibt pruefsummen.txt einer Veröffentlichung.
//
// Läuft nur im Veröffentlichungs-Ablauf auf GitHub. Der geheime Schlüssel
// kommt aus der Umgebung (LUMEN_SIGNIER_SCHLUESSEL, die 32 Byte des
// Ed25519-Seeds als Base64) und liegt nirgends sonst. Der öffentliche
// Gegenpart ist in internal/update fest eingebaut: Ein Server setzt nur
// ein, was mit diesem Schlüssel unterschrieben ist — auch wer Releases
// anlegen kann, bringt ohne ihn nichts auf fremde Server.
//
//	lumen-signieren pruefsummen.txt                  → schreibt pruefsummen.txt.sig
//	lumen-signieren -pruefen pruefsummen.txt v0.1.20 → prüft wie ein Server
//	lumen-signieren -neu                             → erzeugt ein neues Schlüsselpaar
//
// -pruefen hält die Unterschrift gegen den öffentlichen Schlüssel, der in
// internal/update eingebaut ist, und verlangt die Fassungszeile
// („# lumen-version: v0.1.20"). Genau so prüft ein Server beim
// Selbst-Update. Der Veröffentlichungs-Ablauf ruft das nach dem
// Unterschreiben auf: Passt das Geheimnis nicht zum eingebauten Schlüssel,
// scheitert die Veröffentlichung hier — und nicht still auf jedem Server.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"

	"lumen/internal/update"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "-neu" {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			fehler(err)
		}
		fmt.Println("geheim (LUMEN_SIGNIER_SCHLUESSEL):", base64.StdEncoding.EncodeToString(priv.Seed()))
		fmt.Println("öffentlich (internal/update):    ", base64.StdEncoding.EncodeToString(pub))
		return
	}
	if len(os.Args) == 4 && os.Args[1] == "-pruefen" {
		summen, err := os.ReadFile(os.Args[2])
		if err != nil {
			fehler(err)
		}
		sig, err := os.ReadFile(os.Args[2] + ".sig")
		if err != nil {
			fehler(err)
		}
		if err := update.PruefeSummen(summen, sig, os.Args[3]); err != nil {
			fehler(err)
		}
		fmt.Println("geprüft: Unterschrift passt zum eingebauten Schlüssel, Fassung", update.SignierteFassung(summen))
		return
	}
	if len(os.Args) != 2 {
		fehler(fmt.Errorf("aufruf: lumen-signieren <datei>"))
	}
	seed, err := base64.StdEncoding.DecodeString(os.Getenv("LUMEN_SIGNIER_SCHLUESSEL"))
	if err != nil || len(seed) != ed25519.SeedSize {
		fehler(fmt.Errorf("LUMEN_SIGNIER_SCHLUESSEL fehlt oder ist kein Ed25519-Seed"))
	}
	inhalt, err := os.ReadFile(os.Args[1])
	if err != nil {
		fehler(err)
	}
	sig := ed25519.Sign(ed25519.NewKeyFromSeed(seed), inhalt)
	if err := os.WriteFile(os.Args[1]+".sig", []byte(base64.StdEncoding.EncodeToString(sig)+"\n"), 0o644); err != nil {
		fehler(err)
	}
	fmt.Println("unterschrieben:", os.Args[1]+".sig")
}

func fehler(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
