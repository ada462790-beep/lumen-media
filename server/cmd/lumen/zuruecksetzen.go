// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package main

// „lumen zuruecksetzen": PIN oder Passwort eines Profils von der
// Kommandozeile neu setzen — für den Fall, dass der Besitzer seinen eigenen
// vergessen hat. In der App setzt nur die Verwaltung zurück, und die
// braucht genau diesen Riegel; ohne diesen Weg wäre ein vergessener
// Besitzer-PIN eine Sackgasse.
//
// Wer hier tippen kann, hat ohnehin Zugriff auf den Server und seine
// Datenbank. Trotzdem gelten dieselben Regeln wie in der App: Setzt man
// das Profil eines anderen zurück, verschwindet dessen privater Verlauf
// (store.PinZuruecksetzen) — sonst wäre dieser Weg die Hintertür, um zu
// sehen, was jemand schaut. Nur beim Besitzer selbst bleibt alles stehen:
// es ist sein eigener Verlauf.

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"lumen/internal/store"
)

func zuruecksetzenBefehl(args []string, ein io.Reader, aus io.Writer) error {
	fs := flag.NewFlagSet("zuruecksetzen", flag.ContinueOnError)
	fs.SetOutput(aus)
	dataDir := fs.String("data", envOr("LUMEN_DATA", defaultDataDir()), "Ordner mit der Datenbank")
	konto := fs.String("konto", "", "Name des Profils (ohne Angabe: Liste zeigen)")
	art := fs.String("art", "pin", "pin oder password")
	verwaltung := fs.Bool("verwaltung", false, "auch die Verwaltungssperre (Verwaltungs-PIN) aufheben")
	if err := fs.Parse(args); err != nil {
		return err
	}
	pfad := filepath.Join(*dataDir, "lumen.db")
	if _, err := os.Stat(pfad); err != nil {
		return fmt.Errorf("keine Datenbank unter %s — mit --data den Datenordner angeben", pfad)
	}
	// Als root (docker exec) auf den Besitzer der Datenbank wechseln: Neue
	// Hilfsdateien (-wal, -shm) gehörten sonst root, und Lumen selbst
	// könnte die Datenbank danach nicht mehr öffnen.
	if err := alsBesitzerDer(pfad); err != nil {
		return err
	}
	st, err := store.Open(pfad)
	if err != nil {
		return err
	}
	defer st.Close()
	ctx := context.Background()

	konten, err := st.Accounts(ctx)
	if err != nil {
		return err
	}
	if *konto == "" {
		fmt.Fprintln(aus, "Profile auf diesem Server:")
		for _, a := range konten {
			rolle := "Haushalt"
			switch {
			case a.IsOwner:
				rolle = "Besitzer"
			case a.Vollzugriff:
				rolle = "Vollzugriff"
			case a.IsAdmin:
				rolle = "Mitverwalter"
			case a.IsKid:
				rolle = "Kinderprofil"
			}
			fmt.Fprintf(aus, "  %-24s %s\n", a.Name, rolle)
		}
		fmt.Fprintln(aus, "\nZurücksetzen mit:  lumen zuruecksetzen --konto NAME [--art pin|password] [--verwaltung]")
		return nil
	}

	var ziel *store.Account
	for i := range konten {
		if strings.EqualFold(konten[i].Name, *konto) {
			ziel = &konten[i]
			break
		}
	}
	if ziel == nil {
		return fmt.Errorf("kein Profil namens %q", *konto)
	}
	if *art != "pin" && *art != "password" {
		return errors.New("--art muss pin oder password sein")
	}

	zeilen := bufio.NewScanner(ein)
	frage := func(text string) (string, error) {
		fmt.Fprint(aus, text)
		if !zeilen.Scan() {
			return "", errors.New("abgebrochen")
		}
		return strings.TrimSpace(zeilen.Text()), nil
	}
	if !ziel.IsOwner {
		fmt.Fprintf(aus, "Achtung: Der Verlauf, die Merkliste und die Bewertungen von %s werden dabei gelöscht.\n", ziel.Name)
		ok, err := frage("Fortfahren? (ja/nein) ")
		if err != nil {
			return err
		}
		if ok != "ja" {
			return errors.New("nichts geändert")
		}
	}
	wort := "PIN"
	if *art == "password" {
		wort = "Passwort"
	}
	neu, err := frage("Neue(s) " + wort + ": ")
	if err != nil {
		return err
	}
	nochmal, err := frage("Noch einmal: ")
	if err != nil {
		return err
	}
	if neu != nochmal {
		return errors.New("die beiden Eingaben stimmen nicht überein — nichts geändert")
	}

	if ziel.IsOwner {
		if err := st.SetSecret(ctx, ziel.ID, *art, neu); err != nil {
			return err
		}
		// Wer den Riegel nicht mehr wusste, will auch, dass niemand mit dem
		// alten weiter angemeldet bleibt.
		if err := st.RevokeAllTokens(ctx, ziel.ID); err != nil {
			return err
		}
	} else if err := st.PinZuruecksetzen(ctx, ziel.ID, *art, neu); err != nil {
		return err
	}
	if *verwaltung {
		if err := st.VerwaltungsSperreSetzen(ctx, ziel.ID, "none", ""); err != nil {
			return err
		}
	}
	fmt.Fprintf(aus, "Erledigt: %s hat einen neuen Riegel; alle Geräte dieses Profils sind abgemeldet.\n", ziel.Name)
	if *verwaltung {
		fmt.Fprintln(aus, "Die Verwaltungssperre ist aufgehoben — in der Verwaltung gleich eine neue setzen.")
	}
	return nil
}
