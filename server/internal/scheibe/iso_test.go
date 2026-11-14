// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package scheibe

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// Echte Ausgabe von 7-Zip 24.09 („7zz l -slt -ba"), gekürzt.
const siebenZipListe = `Path = VIDEO_TS
Folder = +
Size = 0
Packed Size = 0
Modified = 2004-03-01 12:00:00

Path = VIDEO_TS/VIDEO_TS.IFO
Folder = -
Size = 12288
Packed Size = 12288
Modified = 2004-03-01 12:00:00

Path = VIDEO_TS/VTS_01_1.VOB
Folder = -
Size = 1073709056
Packed Size = 1073709056
Modified = 2004-03-01 12:00:00

Path = AUDIO_TS
Folder = +
Size = 0
`

func TestListeLesen(t *testing.T) {
	l := listeLesen([]byte(siebenZipListe), "/x/film.iso")
	if len(l) != 2 || l[0].Pfad != "VIDEO_TS/VIDEO_TS.IFO" || l[1].Groesse != 1073709056 {
		t.Fatalf("Liste %+v", l)
	}
	// Mit Kopf (ohne -ba) und Windows-Trennern.
	l = listeLesen([]byte("Path = /x/film.iso\nType = Udf\nPhysical Size = 99\n\n----------\nPath = BDMV\\STREAM\\00001.m2ts\nSize = 5\n"), "/x/film.iso")
	if len(l) != 1 || l[0].Pfad != "BDMV/STREAM/00001.m2ts" {
		t.Fatalf("Liste %+v", l)
	}
}

// falschesSiebenZip: ein Skript, das sich wie 7-Zip verhält, aber statt
// eines Abbilds den Ordner „<abbild>.d" liest. Echtes 7-Zip fehlt auf dem
// Testrechner; im Docker-Abbild ist es das Alpine-Paket „7zip".
func falschesSiebenZip(t *testing.T) string {
	t.Helper()
	skript := filepath.Join(t.TempDir(), "7zz")
	inhalt := `#!/bin/sh
cmd=$1; shift
ziel=""
while [ $# -gt 0 ]; do
  case "$1" in
    -o*) ziel=${1#-o}; shift;;
    --) shift; break;;
    *) shift;;
  esac
done
iso=$1; shift
[ -d "$iso.d" ] || { echo "ERROR: $iso : Can not open the file as archive" >&2; exit 2; }
case "$cmd" in
  l) cd "$iso.d" && find . -mindepth 1 | sed 's|^\./||' | sort | while read -r p; do
       if [ -d "$p" ]; then printf 'Path = %s\nFolder = +\nSize = 0\n\n' "$p";
       else printf 'Path = %s\nFolder = -\nSize = %s\n\n' "$p" "$(wc -c < "$p" | tr -d ' ')"; fi
     done;;
  x) for p in "$@"; do mkdir -p "$ziel/$(dirname "$p")"; cp "$iso.d/$p" "$ziel/$p" || exit 2; done;;
esac
`
	if err := os.WriteFile(skript, []byte(inhalt), 0o755); err != nil {
		t.Fatal(err)
	}
	return skript
}

func TestIsoDVDAuspackenUndUmwandeln(t *testing.T) {
	ffmpeg, ffprobe := werkzeug(t, "ffmpeg"), werkzeug(t, "ffprobe")
	werkzeug(t, "sh")
	sz := falschesSiebenZip(t)
	iso := filepath.Join(t.TempDir(), "Film (2001).iso")
	os.WriteFile(iso, []byte("attrappe"), 0o644)
	vt := filepath.Join(iso+".d", "VIDEO_TS")
	os.MkdirAll(vt, 0o755)
	os.MkdirAll(filepath.Join(iso+".d", "AUDIO_TS"), 0o755)
	dvdOrdnerBauen(t, ffmpeg, vt)

	ctx := context.Background()
	inhalt, err := IsoLesen(ctx, sz, iso)
	if err != nil {
		t.Fatal(err)
	}
	if inhalt.Art != DVD {
		t.Fatalf("Art %q", inhalt.Art)
	}
	arbeit := t.TempDir()
	if err := Auspacken(ctx, sz, iso, arbeit, inhalt.Steuerdateien()); err != nil {
		t.Fatal(err)
	}
	dir, ok := Unterordner(arbeit, "VIDEO_TS")
	if !ok {
		t.Fatal("VIDEO_TS nicht ausgepackt")
	}
	p, err := PlanDVD(dir, inhalt.Ordner("VIDEO_TS"))
	if err != nil || p.Ausweg {
		t.Fatalf("Plan %+v %v", p, err)
	}
	// Nur die Dateien des Hauptfilms auspacken — nicht Menü, nicht Titel 1.
	var pfade []string
	for _, d := range p.Dateien() {
		pfade = append(pfade, inhalt.Pfad("VIDEO_TS", d))
	}
	if len(pfade) != 2 {
		t.Fatalf("Dateien %v", pfade)
	}
	if err := Auspacken(ctx, sz, iso, arbeit, pfade); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "VTS_01_1.VOB")); err == nil {
		t.Error("Titel 1 ausgepackt, obwohl nicht gebraucht")
	}
	gefunden, err := SpurenFinden(ctx, ffprobe, p)
	if err != nil {
		t.Fatal(err)
	}
	ziel := filepath.Join(t.TempDir(), "Film (2001).mkv")
	if err := Umwandeln(ctx, ffmpeg, p, Auswahl(p, gefunden), "", ziel, nil, nil); err != nil {
		t.Fatal(err)
	}
	if e := proben(t, ffprobe, ziel); len(e.Streams) != 3 {
		t.Errorf("Spuren %+v", e.Streams)
	}
}

func TestIsoOhneFilm(t *testing.T) {
	werkzeug(t, "sh")
	sz := falschesSiebenZip(t)
	iso := filepath.Join(t.TempDir(), "Daten.iso")
	os.WriteFile(iso, nil, 0o644)
	os.MkdirAll(iso+".d/Fotos", 0o755)
	os.WriteFile(iso+".d/Fotos/a.jpg", []byte("x"), 0o644)
	if _, err := IsoLesen(context.Background(), sz, iso); err == nil {
		t.Fatal("Daten-ISO als Film erkannt")
	}
	if _, err := IsoLesen(context.Background(), sz, filepath.Join(t.TempDir(), "fehlt.iso")); err == nil {
		t.Fatal("fehlendes Abbild ohne Fehler")
	}
}
