// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package untertitel

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lumen/internal/untertitel/nachbau"
)

const srt = "1\n00:00:01,000 --> 00:00:02,500\nHallo Welt\n"

// Der Hash folgt der Vorschrift von OpenSubtitles: Grösse plus die Summe
// der 64-Bit-Wörter am Anfang und am Ende.
func TestHash(t *testing.T) {
	dir := t.TempDir()
	null := filepath.Join(dir, "null.mkv")
	_ = os.WriteFile(null, make([]byte, 131072), 0o644)
	if h, err := Hash(null); err != nil || h != "0000000000020000" {
		t.Fatalf("Hash einer leeren Datei: %s %v", h, err)
	}
	// Ein Wort mit 1 am Anfang, eines mit 2 am Ende, 200 KiB gross.
	b := make([]byte, 204800)
	binary.LittleEndian.PutUint64(b[0:], 1)
	binary.LittleEndian.PutUint64(b[len(b)-8:], 2)
	voll := filepath.Join(dir, "voll.mkv")
	_ = os.WriteFile(voll, b, 0o644)
	if h, _ := Hash(voll); h != fmt.Sprintf("%016x", 204800+3) {
		t.Fatalf("Hash: %s", h)
	}
	klein := filepath.Join(dir, "klein.mkv")
	_ = os.WriteFile(klein, make([]byte, 1000), 0o644)
	if _, err := Hash(klein); err == nil {
		t.Fatal("zu kleine Datei gehasht")
	}
}

// Ins Datenverzeichnis, nie überschreiben, festes Datum für Datei und
// Ordner, höchstens GeholtJeSprache je Sprache und Art.
func TestAblegenGeholt(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "untertitel-geholt")
	p, err := AblegenGeholt(dir, "datei_1", "de", false, false, []byte(srt))
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(p) != "datei_1.de.srt" {
		t.Fatalf("Name: %s", filepath.Base(p))
	}
	p2, _ := AblegenGeholt(dir, "datei_1", "de", false, false, []byte("zwei"))
	if filepath.Base(p2) != "datei_1.de.2.srt" {
		t.Fatalf("zweite: %s", p2)
	}
	if b, _ := os.ReadFile(p); string(b) != srt {
		t.Fatal("vorhandene Datei überschrieben")
	}
	for _, x := range []string{p, dir} {
		if st, _ := os.Stat(x); !st.ModTime().Equal(festeZeit) {
			t.Fatalf("%s: Zeitstempel %v", x, st.ModTime())
		}
	}
	if p, _ := AblegenGeholt(dir, "datei_1", "en", true, false, []byte(srt)); filepath.Base(p) != "datei_1.en.sdh.srt" {
		t.Fatalf("SDH: %s", p)
	}
	_, _ = AblegenGeholt(dir, "datei_1", "de", false, false, []byte("drei"))
	if _, err := AblegenGeholt(dir, "datei_1", "de", false, false, []byte("vier")); !errors.Is(err, ErrZuVieleGeholt) {
		t.Fatalf("vierte angenommen: %v", err)
	}
	if _, err := AblegenGeholt(dir, "datei_1", "../../etc", false, false, []byte(srt)); err == nil {
		t.Fatal("Pfad als Sprache angenommen")
	}
	if _, err := AblegenGeholt(dir, "../x", "de", false, false, []byte(srt)); err == nil {
		t.Fatal("Pfad als Datei angenommen")
	}
}

func TestAlsSRT(t *testing.T) {
	latin1 := []byte("1\n00:00:01,000 --> 00:00:02,000\nF\xfcr dich\n")
	got, err := AlsSRT(latin1)
	if err != nil || !strings.Contains(string(got), "Für dich") {
		t.Fatalf("Latin-1: %q %v", got, err)
	}
	if _, err := AlsSRT([]byte("<html>Fehler</html>")); err == nil {
		t.Fatal("HTML als Untertitel angenommen")
	}
}

func TestSuchenUndHerunterladen(t *testing.T) {
	n := nachbau.Neu()
	defer n.Close()
	n.Untertitel = []nachbau.Untertitel{
		{DateiID: 1, Sprache: "de", IMDb: "133093", Name: "Matrix", Downloads: 900, Inhalt: srt},
		{DateiID: 2, Sprache: "de", IMDb: "133093", Name: "Matrix", Downloads: 10, Hash: "00000000000abcde", Inhalt: srt},
		{DateiID: 3, Sprache: "de", IMDb: "133093", Name: "Matrix", Downloads: 5000, Maschinell: true, Inhalt: srt},
		{DateiID: 4, Sprache: "en", IMDb: "133093", Name: "Matrix", Downloads: 1, Inhalt: srt},
	}
	ctx := context.Background()

	// Ohne Schlüssel geht nichts.
	if _, err := Neu("", "", "", n.Adresse()).Suchen(ctx, Suche{Name: "x"}); !errors.Is(err, ErrKeinSchluessel) {
		t.Fatalf("ohne Schlüssel: %v", err)
	}
	if _, err := Neu("falsch", "", "", n.Adresse()).Suchen(ctx, Suche{Name: "x"}); !errors.Is(err, ErrSchluessel) {
		t.Fatalf("falscher Schlüssel: %v", err)
	}
	if _, err := Neu(n.Schluessel, "alex", "falsch", n.Adresse()).Suchen(ctx, Suche{Name: "x"}); !errors.Is(err, ErrAnmeldung) {
		t.Fatalf("falsches Passwort: %v", err)
	}

	c := Neu(n.Schluessel, "alex", "os-geheim", n.Adresse())
	tr, err := c.Suchen(ctx, Suche{Hash: "00000000000abcde", IMDbID: "tt0133093", Sprachen: []string{"de"}})
	if err != nil {
		t.Fatal(err)
	}
	// Hash zuerst, dann Menschen vor Maschinen, dann Downloads.
	if len(tr) != 3 || tr[0].DateiID != 2 || tr[1].DateiID != 1 || tr[2].DateiID != 3 {
		t.Fatalf("Reihenfolge: %+v", tr)
	}
	if n.UnsortierteAnfrage {
		t.Fatal("Parameter nicht alphabetisch — OpenSubtitles würde umleiten")
	}
	inhalt, k, err := c.Herunterladen(ctx, 2)
	if err != nil || string(inhalt) != srt || k.Rest != 4 {
		t.Fatalf("Download: %q %+v %v", inhalt, k, err)
	}
	// Die Adresse aus der Anmeldung gilt nur für die offizielle Schnittstelle.
	if c.basisURL() != n.Adresse() {
		t.Fatalf("Basis gewechselt: %s", c.basisURL())
	}
	n.Rest = 0
	if _, _, err := c.Herunterladen(ctx, 1); !errors.Is(err, ErrKontingent) {
		t.Fatalf("Kontingent: %v", err)
	}
	if kont, err := c.Pruefen(ctx); err != nil || kont.Erlaubt != 20 {
		t.Fatalf("Prüfen: %+v %v", kont, err)
	}
}
