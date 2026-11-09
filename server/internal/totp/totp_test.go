// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package totp

import (
	"testing"
	"time"
)

// Der Prüfvektor aus RFC 6238 (Anhang B), Geheimnis „12345678901234567890".
func TestRFC6238(t *testing.T) {
	const geheimnis = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	for _, c := range []struct {
		sek  int64
		want string
	}{{59, "287082"}, {1111111109, "081804"}, {1234567890, "005924"}, {2000000000, "279037"}} {
		got, err := Code(geheimnis, time.Unix(c.sek, 0))
		if err != nil || got != c.want {
			t.Errorf("t=%d: %s (%v), erwartet %s", c.sek, got, err, c.want)
		}
	}
	jetzt := time.Unix(1111111109, 0)
	if !Stimmt(geheimnis, "081804", jetzt) || !Stimmt(geheimnis, "081804", jetzt.Add(25*time.Second)) {
		t.Error("gültiger Code abgelehnt")
	}
	if Stimmt(geheimnis, "000000", jetzt) || Stimmt(geheimnis, "08180", jetzt) {
		t.Error("falscher Code angenommen")
	}
}

func TestGeheimnisUndQR(t *testing.T) {
	g, err := NeuesGeheimnis()
	if err != nil || len(g) != 32 {
		t.Fatalf("Geheimnis %q %v", g, err)
	}
	png, err := QR(Adresse(g, "Alex"))
	if err != nil || len(png) < 100 || string(png[1:4]) != "PNG" {
		t.Fatalf("QR: %d Byte, %v", len(png), err)
	}
}
