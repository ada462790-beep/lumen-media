// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestOffeneEinladungenUndZurueckziehen(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	besitzer, err := st.CreateAccount(ctx, NewAccount{Name: "Besitzer", IsOwner: true, LockKind: "password", Secret: "geheimgeheim"})
	if err != nil {
		t.Fatal(err)
	}
	helfer, _ := st.CreateAccount(ctx, NewAccount{Name: "Helfer"})
	a, err := st.CreateInvite(ctx, besitzer.ID, time.Hour, map[string]any{"libraries": []string{"x", "y"}, "kinderprofil": true})
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Code) != 16 {
		t.Fatalf("Code %q: erwartet 16 Zeichen", a.Code)
	}
	b, _ := st.CreateInvite(ctx, helfer.ID, time.Hour, map[string]any{})

	alle, err := st.OffeneEinladungen(ctx, besitzer.ID, true)
	if err != nil || len(alle) != 2 {
		t.Fatalf("alle: %v %v", alle, err)
	}
	for _, e := range alle {
		if strings.Contains(e.Kennung, a.Code) || strings.Contains(e.Kennung, b.Code) {
			t.Fatal("der Code darf nicht in der Kennung stehen")
		}
	}
	eigene, _ := st.OffeneEinladungen(ctx, helfer.ID, false)
	if len(eigene) != 1 || eigene[0].Ende != b.Code[12:] || eigene[0].Von != "Helfer" {
		t.Fatalf("eigene: %+v", eigene)
	}
	// Der Helfer darf die Einladung des Besitzers nicht zurückziehen.
	var kennungA string
	for _, e := range alle {
		if e.Ende == a.Code[12:] {
			kennungA = e.Kennung
			if !e.Kinderprofil || e.Bibliotheken != 2 {
				t.Fatalf("Angaben falsch: %+v", e)
			}
		}
	}
	if err := st.EinladungZurueckziehen(ctx, kennungA, helfer.ID, false); err == nil {
		t.Fatal("fremde Einladung zurückgezogen")
	}
	if err := st.EinladungZurueckziehen(ctx, kennungA, besitzer.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := st.RedeemInvite(ctx, a.Code, "Lina", "none", ""); err == nil {
		t.Fatal("zurückgezogene Einladung liess sich einlösen")
	}
	// Getippt mit Bindestrichen, klein und mit Verwechslern: geht trotzdem.
	getippt := strings.ToLower(b.Code[:4] + "-" + b.Code[4:8] + " " + b.Code[8:])
	getippt = strings.NewReplacer("o", "0", "i", "1").Replace(getippt)
	if _, err := st.RedeemInvite(ctx, getippt, "Noah", "none", ""); err != nil {
		t.Fatalf("getippter Code: %v", err)
	}
	if rest, _ := st.OffeneEinladungen(ctx, besitzer.ID, true); len(rest) != 0 {
		t.Fatalf("noch offen: %+v", rest)
	}
}
