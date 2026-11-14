// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

//go:build !linux

package api

// schnittstellenArten: ausserhalb von Linux unbekannt (Container laufen
// dort ohnehin in einer Linux-VM).
func schnittstellenArten() map[string]string { return nil }
