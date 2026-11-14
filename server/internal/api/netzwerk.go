// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

// So sieht Lumen dieses Gerät: Adresse, zu Hause oder unterwegs, und
// warum. Für die Verwaltung — wer „geht nur zu Hause" liest, obwohl er auf
// dem Sofa sitzt, soll sehen, woran es liegt.
//
// Häufigster Grund: Docker im Bridge-Betrieb reicht auf manchen NAS (auch
// Synology) jede Verbindung über das Gateway des Container-Netzes weiter
// (172.x.0.1). Dann sieht Lumen für alle Geräte dieselbe Adresse und kann
// zu Hause nicht von unterwegs unterscheiden — sicherheitshalber gilt
// alles als unterwegs. Die Lösung steht in der docker-compose.yml:
// „network_mode: host".

import (
	"net/http"
	"time"
)

func (s *Server) adminNetz(w http.ResponseWriter, r *http.Request) {
	h := herkunftVon(r)
	_, _, festgelegt, sicht := netze.aktuell()
	out := map[string]any{
		"geraet":     h,
		"container":  imContainer(),
		"hostNetz":   sicht.hostNetz,
		"bruecke":    sicht.bruecke,
		"festgelegt": festgelegt,
	}
	// Kamen in letzter Zeit Anfragen über ein Gateway? Im Bridge-Betrieb
	// heisst das: Docker verdeckt die echten Adressen.
	if adr, wann, n := gatewayGesehen(); n > 0 && time.Since(wann) < 24*time.Hour {
		out["gateway"] = map[string]any{
			"adresse": adr,
			"zuletzt": wann.UTC().Format(time.RFC3339),
			"anzahl":  n,
			"docker":  sicht.bruecke,
		}
	}
	ok(w, out)
}
