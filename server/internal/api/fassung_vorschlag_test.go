// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"lumen/internal/store"
)

// „Das Boot (1981)" auf Deutsch und „The Boat (1981)" auf Englisch, im
// selben Ordner und gleich lang, ohne TMDb: zwei Einträge — Aufräumen
// schlägt sie als Fassungen eines Films vor, „Als Fassungen
// zusammenlegen" macht einen daraus, und nach „Abtrennen" kommt der
// Vorschlag nicht wieder.
func TestFassungenVorschlagen(t *testing.T) {
	h := newHarness(t)
	h.film("Platzhalter", 1)
	ctx := t.Context()
	ordner := filepath.Join(h.root, "Das Boot (1981)")
	_ = os.MkdirAll(ordner, 0o755)
	film := func(titel, datei, sprache string, dauer int64) (string, string) {
		it := &store.Item{LibraryID: h.lib.ID, Kind: "movie", Title: titel, Year: 1981, MatchState: "pending"}
		_ = h.st.UpsertItem(ctx, it)
		p := filepath.Join(ordner, datei)
		_ = os.WriteFile(p, []byte("x"), 0o644)
		f := &store.MediaFile{ItemID: it.ID, Path: p, SizeBytes: 1, Container: "mkv", DurationMS: dauer,
			Spuren: []store.Track{{Index: 1, Kind: "audio", Language: sprache}}}
		if err := h.st.UpsertFile(ctx, f); err != nil {
			t.Fatal(err)
		}
		return it.ID, f.ID
	}
	de, _ := film("Das Boot", "Das Boot (1981).mkv", "de", 8_940_000)
	en, fEn := film("The Boat", "The Boat (1981).mkv", "en", 8_940_000*24/25) // PAL: 4 % kürzer
	film("Anderer Film", "Anderer Film (1981).mkv", "en", 5_000_000)          // passt nicht (Laufzeit)

	vorschlaege := func() []fassungKandidat {
		t.Helper()
		_, raw := h.do("GET", "/api/v1/admin/cleanup", "", h.tok)
		var b struct {
			FassungKandidaten []fassungKandidat `json:"fassungKandidaten"`
		}
		if err := json.Unmarshal(raw, &b); err != nil {
			t.Fatalf("%v: %s", err, raw)
		}
		return b.FassungKandidaten
	}
	v := vorschlaege()
	if len(v) != 1 || len(v[0].Items) != 2 {
		t.Fatalf("Vorschläge: %+v", v)
	}
	body, _ := json.Marshal(map[string]any{"itemIds": []string{v[0].Items[0].ItemID, v[0].Items[1].ItemID}})
	if resp, raw := h.do("POST", "/api/v1/admin/cleanup/fassungen-zusammenlegen", string(body), h.tok); resp.StatusCode != http.StatusOK {
		t.Fatalf("zusammenlegen: %d %s", resp.StatusCode, raw)
	}
	_, errDe := h.st.Item(ctx, de)
	_, errEn := h.st.Item(ctx, en)
	if (errDe == nil) == (errEn == nil) {
		t.Fatalf("nach dem Zusammenlegen: de %v, en %v — erwartet genau einen Eintrag", errDe, errEn)
	}
	ziel := de
	if errDe != nil {
		ziel = en
	}
	if d, _ := h.st.FilesFor(ctx, ziel); len(d) != 2 {
		t.Fatalf("%d Dateien am Film", len(d))
	}
	if !h.st.ZuordnungGesperrt(ctx, ziel) {
		t.Error("Zuordnung nicht gesperrt — der nächste Durchlauf trennte sie wieder")
	}
	if len(vorschlaege()) != 0 {
		t.Error("nach dem Zusammenlegen noch vorgeschlagen")
	}
	// Abtrennen: kein neuer Vorschlag.
	if resp, raw := h.do("POST", "/api/v1/admin/files/"+fEn+"/abtrennen", "", h.tok); resp.StatusCode != http.StatusOK {
		t.Fatalf("abtrennen: %d %s", resp.StatusCode, raw)
	}
	if v := vorschlaege(); len(v) != 0 {
		t.Errorf("nach dem Abtrennen wieder vorgeschlagen: %+v", v)
	}
}
