// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func scheibenStandLesen(t *testing.T, h *harness) scheibenStand {
	t.Helper()
	resp, raw := h.do("GET", "/api/v1/admin/cleanup/umwandeln", "", h.tok)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Stand: %d %s", resp.StatusCode, raw)
	}
	var st scheibenStand
	if err := json.Unmarshal(raw, &st); err != nil {
		t.Fatal(err)
	}
	return st
}

func scheibenAuftragWarten(t *testing.T, h *harness) scheibenStand {
	t.Helper()
	var st scheibenStand
	bisWahr(t, "Ende des Umwandelns", func() bool {
		st = scheibenStandLesen(t, h)
		return !st.Laeuft && st.Ergebnis != ""
	})
	return st
}

// dvdVOB: ein kurzer DVD-Titel (MPEG-PS mit 2048-Byte-Paketen).
func dvdVOB(t *testing.T, ziel string, sek string) {
	t.Helper()
	_ = os.MkdirAll(filepath.Dir(ziel), 0o755)
	if out, err := exec.Command("ffmpeg", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc=size=176x144:rate=25:duration="+sek,
		"-f", "lavfi", "-i", "sine=duration="+sek,
		"-c:v", "mpeg2video", "-c:a", "ac3", "-ar", "48000", "-f", "dvd", ziel).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
}

// Ein DVD-Ordner (ohne IFO: dann der grösste Titelsatz) wird zur MKV im
// Filmordner. Danach erkennt der Durchlauf die MKV, und die Scheibe
// steht nicht mehr doppelt da.
func TestScheibeDVDOrdnerUmwandeln(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ohne ffmpeg")
	}
	h := newHarness(t)
	h.film("Platzhalter", 1) // legt die Bibliothek an
	film := filepath.Join(h.root, "Heidi (2015)")
	dvdVOB(t, filepath.Join(film, "VIDEO_TS", "VTS_01_0.VOB"), "1")
	dvdVOB(t, filepath.Join(film, "VIDEO_TS", "VTS_01_1.VOB"), "3")
	dvdVOB(t, filepath.Join(film, "VIDEO_TS", "VIDEO_TS.VOB"), "1")

	resp, raw := h.do("POST", "/api/v1/admin/cleanup/umwandeln", `{"pfad":`+jsonText(film)+`}`, h.tok)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("Start: %d %s", resp.StatusCode, raw)
	}
	st := scheibenAuftragWarten(t, h)
	if st.Ergebnis != "fertig" || st.Ziel != "Heidi (2015).mkv" || st.Anteil != 1 {
		t.Fatalf("Stand %+v", st)
	}
	ziel := filepath.Join(film, "Heidi (2015).mkv")
	if _, err := os.Stat(ziel); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(ziel + ".teil"); err == nil {
		t.Error(".teil liegt noch da")
	}
	if _, err := os.Stat(filepath.Join(film, "VIDEO_TS", "VTS_01_1.VOB")); err != nil {
		t.Error("der DVD-Ordner wurde verändert")
	}
	// Der Durchlauf danach liest die MKV.
	bisWahr(t, "MKV eingelesen", func() bool {
		id, _ := h.st.ItemZuPfad(t.Context(), ziel)
		return id != ""
	})
	// Protokoll: mit Beschreibung, ohne Pfad und Titel.
	log, _ := h.st.RecentLog(t.Context(), "", 50)
	gefunden := false
	for _, l := range log {
		if strings.HasPrefix(l.Message, "Hauptfilm einer Scheibe als MKV abgelegt") {
			gefunden = true
			if strings.Contains(l.Message, "Heidi") || strings.Contains(l.Message, "/") {
				t.Errorf("Protokoll verrät den Titel: %q", l.Message)
			}
		}
	}
	if !gefunden {
		t.Error("keine Protokollzeile")
	}
	// Ein zweites Mal: Das Ziel gibt es schon.
	if resp, _ := h.do("POST", "/api/v1/admin/cleanup/umwandeln", `{"pfad":`+jsonText(film)+`}`, h.tok); resp.StatusCode != http.StatusConflict {
		t.Errorf("zweites Umwandeln: %d", resp.StatusCode)
	}
}

func jsonText(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// Wer nicht Besitzer ist, sieht den Weg nicht; Pfade ausserhalb der
// Bibliotheken und Unsinn werden abgelehnt.
func TestScheibeRechteUndPfade(t *testing.T) {
	h := newHarness(t)
	h.film("Platzhalter", 1)
	film := filepath.Join(h.root, "Film (2001)")
	_ = os.MkdirAll(filepath.Join(film, "VIDEO_TS"), 0o755)
	_ = os.WriteFile(filepath.Join(film, "VIDEO_TS", "VTS_01_1.VOB"), []byte("x"), 0o644)

	ctok, _ := h.mitverwalter("Carla")
	if resp, _ := h.do("POST", "/api/v1/admin/cleanup/umwandeln", `{"pfad":`+jsonText(film)+`}`, ctok); resp.StatusCode != http.StatusNotFound {
		t.Errorf("Mitverwalter: %d", resp.StatusCode)
	}
	if resp, _ := h.do("GET", "/api/v1/admin/cleanup/umwandeln", "", ctok); resp.StatusCode != http.StatusNotFound {
		t.Errorf("Mitverwalter Stand: %d", resp.StatusCode)
	}
	aussen := t.TempDir()
	_ = os.WriteFile(filepath.Join(aussen, "x.iso"), []byte("x"), 0o644)
	for pfad, want := range map[string]int{
		filepath.Join(aussen, "x.iso"):           http.StatusNotFound,   // keine Bibliothek
		filepath.Join(h.root, "..", "x.iso"):     http.StatusNotFound,   // hinaus
		"relativ/x.iso":                          http.StatusBadRequest, // kein absoluter Pfad
		filepath.Join(h.root, "Platzhalter.mkv"): http.StatusBadRequest, // keine Scheibe
		filepath.Join(h.root, "fehlt.iso"):       http.StatusNotFound,
	} {
		if resp, raw := h.do("POST", "/api/v1/admin/cleanup/umwandeln", `{"pfad":`+jsonText(pfad)+`}`, h.tok); resp.StatusCode != want {
			t.Errorf("%s: %d %s, erwartet %d", pfad, resp.StatusCode, raw, want)
		}
	}
	// Läuft schon einer, kommt kein zweiter dazu.
	scheibenAuftrag.setzen(func(l *scheibenLauf) { l.laeuft = true })
	defer scheibenAuftrag.setzen(func(l *scheibenLauf) { l.laeuft = false })
	resp, raw := h.do("POST", "/api/v1/admin/cleanup/umwandeln", `{"pfad":`+jsonText(filepath.Join(film, "VIDEO_TS"))+`}`, h.tok)
	if resp.StatusCode != http.StatusConflict || !strings.Contains(string(raw), "läuft schon") {
		t.Errorf("Doppelstart: %d %s", resp.StatusCode, raw)
	}
}

// Quelle aus den Formen, die in der Liste stehen können.
func TestScheibeFinden(t *testing.T) {
	h := newHarness(t)
	h.film("Platzhalter", 1)
	dvd := filepath.Join(h.root, "Film (2001)")
	bd := filepath.Join(h.root, "Avatar (2009)")
	for _, f := range []string{"Film (2001)/video_ts/vts_01_1.vob", "Avatar (2009)/BDMV/STREAM/00001.m2ts", "Heidi.iso"} {
		p := filepath.Join(h.root, f)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		_ = os.WriteFile(p, []byte("x"), 0o644)
	}
	for ein, want := range map[string][2]string{
		dvd:                            {"dvd", filepath.Join(dvd, "Film (2001).mkv")},
		filepath.Join(dvd, "video_ts"): {"dvd", filepath.Join(dvd, "Film (2001).mkv")},
		filepath.Join(dvd, "video_ts", "vts_01_1.vob"): {"dvd", filepath.Join(dvd, "Film (2001).mkv")},
		bd: {"bluray", filepath.Join(bd, "Avatar (2009).mkv")},
		filepath.Join(bd, "BDMV", "STREAM", "00001.m2ts"): {"bluray", filepath.Join(bd, "Avatar (2009).mkv")},
		filepath.Join(h.root, "Heidi.iso"):                {"iso", filepath.Join(h.root, "Heidi.mkv")},
	} {
		q, err := h.api.scheibeFinden(t.Context(), ein)
		if err != nil {
			t.Errorf("%s: %v", ein, err)
			continue
		}
		if q.art != want[0] || q.ziel != want[1] {
			t.Errorf("%s: %s → %s", ein, q.art, q.ziel)
		}
	}
}

// Schreibgeschützt und zu wenig Platz: klare Meldungen.
func TestScheibeOrdnerUndPlatz(t *testing.T) {
	for _, err := range []error{&fs.PathError{Op: "open", Path: "/m/x", Err: syscall.EROFS},
		&fs.PathError{Op: "open", Path: "/m/x", Err: syscall.EACCES}} {
		var af *apiFehler
		if e := ordnerFehler(err); !errors.As(e, &af) || af.status != http.StatusForbidden ||
			!strings.Contains(af.text, "schreibgeschützt") {
			t.Errorf("%v → %v", err, e)
		}
	}
	if err := platzPruefen(t.TempDir(), 1<<60, platzMedienText); err == nil {
		t.Error("ein Exabyte passt nicht")
	}
	if err := platzPruefen(t.TempDir(), 1, platzMedienText); err != nil {
		t.Errorf("ein Byte: %v", err)
	}
	// Schon da: kein Überschreiben.
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "Film.mkv"), []byte("x"), 0o644)
	if err := zielPruefen(filepath.Join(dir, "Film.mkv")); err == nil {
		t.Error("vorhandenes Ziel nicht bemerkt")
	}
	if err := zielPruefen(filepath.Join(dir, "Neu.mkv")); err != nil {
		t.Errorf("freies Ziel: %v", err)
	}
}

// Eine ISO: 7-Zip (hier ein Ersatz, der hängt) — Abbrechen beendet den
// Auftrag, Arbeitsordner und halbe Datei sind weg, die ISO bleibt.
func TestScheibeIsoAbbrechen(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ohne ffmpeg")
	}
	bin := t.TempDir()
	// Listet eine DVD, hängt beim Auspacken.
	skript := "#!/bin/sh\ncase \"$1\" in\n l) printf 'Path = VIDEO_TS/VIDEO_TS.IFO\\nSize = 10\\n\\nPath = VIDEO_TS/VTS_01_1.VOB\\nSize = 4096\\n\\n';;\n x) exec sleep 30;;\nesac\n"
	if err := os.WriteFile(filepath.Join(bin, "7zz"), []byte(skript), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	h := newHarness(t)
	h.film("Platzhalter", 1)
	iso := filepath.Join(h.root, "Film (2001).iso")
	_ = os.WriteFile(iso, []byte("iso"), 0o644)

	resp, raw := h.do("POST", "/api/v1/admin/cleanup/umwandeln", `{"pfad":`+jsonText(iso)+`}`, h.tok)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("Start: %d %s", resp.StatusCode, raw)
	}
	bisWahr(t, "Arbeitsordner angelegt", func() bool {
		_, err := os.Stat(h.api.scheibenArbeitsordner())
		return err == nil
	})
	if resp, _ := h.do("DELETE", "/api/v1/admin/cleanup/umwandeln", "", h.tok); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("Abbrechen: %d", resp.StatusCode)
	}
	st := scheibenAuftragWarten(t, h)
	if st.Ergebnis != "abgebrochen" {
		t.Fatalf("Stand %+v", st)
	}
	if _, err := os.Stat(h.api.scheibenArbeitsordner()); err == nil {
		t.Error("Arbeitsordner nicht aufgeräumt")
	}
	if _, err := os.Stat(iso); err != nil {
		t.Error("ISO verschwunden")
	}
	if m, _ := filepath.Glob(filepath.Join(h.root, "*.mkv*")); len(m) != 1 { // nur Platzhalter.mkv
		t.Errorf("Reste: %v", m)
	}
	if resp, _ := h.do("DELETE", "/api/v1/admin/cleanup/umwandeln", "", h.tok); resp.StatusCode != http.StatusNotFound {
		t.Errorf("zweites Abbrechen: %d", resp.StatusCode)
	}
}

// Eine kaputte ISO: Fehler mit Grund, im Stand übersetzt und im
// Protokoll ohne Pfad.
func TestScheibeIsoFehler(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ohne ffmpeg")
	}
	bin := t.TempDir()
	skript := "#!/bin/sh\necho \"ERROR: $4 : Can not open the file as archive\" >&2\nexit 2\n"
	_ = os.WriteFile(filepath.Join(bin, "7zz"), []byte(skript), 0o755)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	h := newHarness(t)
	h.film("Platzhalter", 1)
	iso := filepath.Join(h.root, "Geheim (2001).iso")
	_ = os.WriteFile(iso, []byte("kaputt"), 0o644)
	if resp, raw := h.do("POST", "/api/v1/admin/cleanup/umwandeln", `{"pfad":`+jsonText(iso)+`}`, h.tok); resp.StatusCode != http.StatusAccepted {
		t.Fatalf("Start: %d %s", resp.StatusCode, raw)
	}
	st := scheibenAuftragWarten(t, h)
	if st.Ergebnis != "fehler" || !strings.HasPrefix(st.Fehler, "Die iso-datei lässt sich nicht lesen") {
		t.Fatalf("Stand %+v", st)
	}
	log, _ := h.st.RecentLog(t.Context(), "", 50)
	for _, l := range log {
		if strings.HasPrefix(l.Message, "Umwandeln in MKV gescheitert") && strings.Contains(l.Message, "Geheim") {
			t.Errorf("Protokoll verrät den Titel: %q", l.Message)
		}
	}
}
