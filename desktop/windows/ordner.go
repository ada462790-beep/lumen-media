// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Zwei Ordner, wie unter Windows üblich:
//
//	%APPDATA%\Lumen       Einstellungen: Serverliste, Fenster, Update-Stand.
//	                      Wandert bei Roaming-Profilen mit.
//	%LOCALAPPDATA%\Lumen  WebView2 (Anmeldung, Cookies, Zwischenspeicher)
//	                      und geladene Installer. Bleibt auf dem Rechner.
//
// LUMEN_DESKTOP_DATEN legt beides unter einen Ordner (Selbsttest).
func einstellungsOrdner() string {
	if d := os.Getenv("LUMEN_DESKTOP_DATEN"); d != "" {
		return filepath.Join(d, "einstellungen")
	}
	if d, err := os.UserConfigDir(); err == nil {
		return filepath.Join(d, "Lumen")
	}
	return filepath.Join(os.TempDir(), "Lumen")
}

func lokalerOrdner() string {
	if d := os.Getenv("LUMEN_DESKTOP_DATEN"); d != "" {
		return filepath.Join(d, "lokal")
	}
	// Unter Windows ist das %LOCALAPPDATA%.
	if d, err := os.UserCacheDir(); err == nil {
		return filepath.Join(d, "Lumen")
	}
	return filepath.Join(os.TempDir(), "Lumen")
}

// jsonLesen liest eine Datei in v. Fehlt sie, bleibt v, wie es ist.
func jsonLesen(pfad string, v any) error {
	b, err := os.ReadFile(pfad)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// jsonSchreiben schreibt erst daneben und benennt dann um: Stürzt der
// Rechner mittendrin ab, bleibt die alte Datei ganz statt halb.
func jsonSchreiben(pfad string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(pfad), 0o700); err != nil {
		return err
	}
	tmp := pfad + ".neu"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, pfad)
}
