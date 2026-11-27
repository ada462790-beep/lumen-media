// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"lumen/internal/store"
	"lumen/internal/stream"
	"lumen/internal/umpacken"
)

// altFilm legt eine echte kleine Datei im alten Behälter an (per FFmpeg)
// und hängt sie an einen neuen Film.
func altFilm(t *testing.T, h *harness, name, endung string, args ...string) (itemID string, f *store.MediaFile) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ohne ffmpeg")
	}
	if h.lib == nil {
		h.film("Platzhalter", 1)
	}
	ordner := filepath.Join(h.root, name)
	_ = os.MkdirAll(ordner, 0o755)
	p := filepath.Join(ordner, name+endung)
	a := []string{"-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc=size=176x144:rate=25:duration=3",
		"-f", "lavfi", "-i", "sine=duration=3"}
	a = append(a, args...)
	a = append(a, p)
	if out, err := exec.Command("ffmpeg", a...).CombinedOutput(); err != nil {
		t.Skipf("ffmpeg: %v %s", err, out)
	}
	info, _ := os.Stat(p)
	it := &store.Item{LibraryID: h.lib.ID, Kind: "movie", Title: name, Year: 1999, MatchState: "matched"}
	if err := h.st.UpsertItem(t.Context(), it); err != nil {
		t.Fatal(err)
	}
	f = &store.MediaFile{ItemID: it.ID, Path: p, SizeBytes: info.Size(), MTime: info.ModTime().Unix(),
		Container: strings.TrimPrefix(endung, "."), DurationMS: 3000,
		Spuren: []store.Track{{Index: 1, Kind: "audio", Codec: "mp3", Language: "deu"}}}
	if err := h.st.UpsertFile(t.Context(), f); err != nil {
		t.Fatal(err)
	}
	return it.ID, f
}

func mkvStandLesen(t *testing.T, h *harness) mkvStand {
	t.Helper()
	resp, raw := h.do("GET", "/api/v1/admin/cleanup/mkv/stand", "", h.tok)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Stand: %d %s", resp.StatusCode, raw)
	}
	var st mkvStand
	if err := json.Unmarshal(raw, &st); err != nil {
		t.Fatal(err)
	}
	return st
}

// mkvWarten wartet, bis die Warteschlange leer ist, und gibt das Ergebnis
// für die Datei zurück.
func mkvWarten(t *testing.T, h *harness, fileID string) mkvZeile {
	t.Helper()
	var z mkvZeile
	bisWahr(t, "Ende des Umpackens", func() bool {
		st := mkvStandLesen(t, h)
		if st.Laeuft {
			return false
		}
		for _, e := range st.Ergebnisse {
			if e.FileID == fileID {
				z = e
				return true
			}
		}
		return false
	})
	return z
}

func mkvStarten(t *testing.T, h *harness, body string) {
	t.Helper()
	resp, raw := h.do("POST", "/api/v1/admin/cleanup/mkv", body, h.tok)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("Start: %d %s", resp.StatusCode, raw)
	}
}

func gibtEs(p string) bool { _, err := os.Lstat(p); return err == nil }

// Der ganze Weg: AVI → MKV, Original weg, die Datei behält ihre Kennung
// (Stand eines Profils, gewählte Fassung, Vorschaubilder bleiben), die
// Spuren sind neu gemessen, Untertitel daneben passen weiter.
func TestMkvUmpackenAVI(t *testing.T) {
	h := newHarness(t)
	itemID, f := altFilm(t, h, "Alt (1999)", ".avi", "-c:v", "mpeg4", "-vtag", "XVID", "-c:a", "libmp3lame")
	ctx := t.Context()
	srt := strings.TrimSuffix(f.Path, ".avi") + ".de.srt"
	_ = os.WriteFile(srt, []byte("1\n00:00:01,000 --> 00:00:02,000\nHoi\n"), 0o644)

	// Ein Profil hat angefangen zu schauen; der Besitzer hat die Fassung gewählt.
	gast, _ := h.st.CreateAccount(ctx, store.NewAccount{Name: "Mara", LockKind: "pin", Secret: "4711"})
	if err := h.st.SaveProgress(ctx, gast.ID, itemID, 61000, 3000_000); err != nil {
		t.Fatal(err)
	}
	if err := h.st.FassungWaehlen(ctx, itemID, f.ID); err != nil {
		t.Fatal(err)
	}
	// Vorschaubilder dieser Fassung liegen schon da.
	tp := h.api.trickplayOrdner(f.ID)
	_ = os.MkdirAll(tp, 0o755)
	_ = os.WriteFile(filepath.Join(tp, stream.TrickplayQuelle), []byte(dateiKennung(f)), 0o644)

	// Die Liste nennt die Datei und zählt das Format.
	resp, raw := h.do("GET", "/api/v1/admin/cleanup/mkv", "", h.tok)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(raw), `"endung":".avi"`) ||
		!strings.Contains(string(raw), f.ID) || !strings.Contains(string(raw), `"inAllen":true`) {
		t.Fatalf("Liste: %d %s", resp.StatusCode, raw)
	}

	mkvStarten(t, h, `{"fileIds":["`+f.ID+`"]}`)
	if z := mkvWarten(t, h, f.ID); z.Ergebnis != "fertig" {
		t.Fatalf("Ergebnis %+v", z)
	}
	ziel := strings.TrimSuffix(f.Path, ".avi") + ".mkv"
	if !gibtEs(ziel) || gibtEs(f.Path) || gibtEs(umpacken.Temp(f.Path)) {
		t.Fatalf("Dateien: mkv=%v avi=%v tmp=%v", gibtEs(ziel), gibtEs(f.Path), gibtEs(umpacken.Temp(f.Path)))
	}
	if gibtEs(filepath.Join(filepath.Dir(f.Path), mkvOriginalOrdner)) {
		t.Error(".lumen-original blieb liegen")
	}
	if gibtEs(h.api.mkvVermerk()) {
		t.Error("Vermerk blieb liegen")
	}
	neu, err := h.st.File(ctx, f.ID)
	if err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(ziel)
	if neu.Path != ziel || neu.Container != "mkv" || neu.SizeBytes != info.Size() || neu.ItemID != itemID {
		t.Errorf("Datei in der Datenbank: %+v", neu)
	}
	dateien, _ := h.st.FilesFor(ctx, itemID)
	if len(dateien) != 1 || !dateien[0].Bevorzugt {
		t.Errorf("Dateien am Titel: %+v", dateien)
	}
	if p, err := h.st.Progress(ctx, gast.ID, itemID); err != nil || p == nil || p.PositionMS != 61000 {
		t.Errorf("Stand verloren: %+v %v", p, err)
	}
	spuren, _ := h.st.Spuren(ctx, f.ID)
	ton, extern := 0, 0
	for _, s := range spuren {
		if s.Kind == "audio" && s.Codec == "mp3" {
			ton++
		}
		if s.External {
			extern++
		}
	}
	if ton != 1 || extern != 1 {
		t.Errorf("Spuren neu gemessen? %+v", spuren)
	}
	if b, _ := os.ReadFile(filepath.Join(tp, stream.TrickplayQuelle)); string(b) != dateiKennung(neu) {
		t.Errorf("Vorschaubilder gelten nicht mehr: %q statt %q", b, dateiKennung(neu))
	}
	// Protokoll ohne Titel und Pfad.
	log, _ := h.st.RecentLog(ctx, "", 50)
	gefunden := false
	for _, l := range log {
		if strings.HasPrefix(l.Message, "In MKV umgepackt") {
			gefunden = true
			if strings.Contains(l.Message, "Alt") || strings.Contains(l.Message, "/") {
				t.Errorf("Protokoll verrät den Titel: %q", l.Message)
			}
		}
	}
	if !gefunden {
		t.Error("keine Protokollzeile")
	}
	// Nichts mehr umzuwandeln.
	if _, raw := h.do("GET", "/api/v1/admin/cleanup/mkv", "", h.tok); !strings.Contains(string(raw), `"dateien":[]`) {
		t.Errorf("noch in der Liste: %s", raw)
	}
}

// „Original behalten": Es wandert nach .lumen-original (der Scan liest den
// Ordner nicht). MOV mit mov_text: Der Untertitel wird SubRip.
func TestMkvUmpackenOriginalBehalten(t *testing.T) {
	h := newHarness(t)
	srt := filepath.Join(t.TempDir(), "u.srt")
	_ = os.WriteFile(srt, []byte("1\n00:00:00,500 --> 00:00:02,000\nHallo\n"), 0o644)
	itemID, f := altFilm(t, h, "Bewahrt (2001)", ".mov", "-i", srt, "-map", "0:v", "-map", "1:a", "-map", "2:s",
		"-c:v", "mpeg4", "-c:a", "aac", "-c:s", "mov_text")
	mkvStarten(t, h, `{"itemId":"`+itemID+`","original":"behalten"}`)
	if z := mkvWarten(t, h, f.ID); z.Ergebnis != "fertig" {
		t.Fatalf("Ergebnis %+v", z)
	}
	ziel := strings.TrimSuffix(f.Path, ".mov") + ".mkv"
	original := filepath.Join(filepath.Dir(f.Path), mkvOriginalOrdner, filepath.Base(f.Path))
	if !gibtEs(ziel) || gibtEs(f.Path) || !gibtEs(original) {
		t.Fatalf("mkv=%v mov=%v beiseite=%v", gibtEs(ziel), gibtEs(f.Path), gibtEs(original))
	}
	spuren, _ := h.st.Spuren(t.Context(), f.ID)
	untertitel := false
	for _, s := range spuren {
		untertitel = untertitel || (s.Kind == "subtitle" && s.Codec == "subrip")
	}
	if !untertitel {
		t.Errorf("Untertitel: %+v", spuren)
	}
}

// Scheitert die Prüfung, bleibt das Original unberührt, die halbe MKV
// verschwindet, und die Meldung sagt es.
func TestMkvPruefungScheitert(t *testing.T) {
	h := newHarness(t)
	_, f := altFilm(t, h, "Heikel (2003)", ".avi", "-c:v", "mpeg4", "-c:a", "libmp3lame")
	alt := mkvPruefen
	mkvPruefen = func(ctx context.Context, ffprobe string, p *umpacken.Plan, mkv string, n int64) error {
		return &umpacken.Abweichung{Art: "spuren", Beschreibung: "test"}
	}
	t.Cleanup(func() { mkvPruefen = alt })
	vorher, _ := os.ReadFile(f.Path)

	mkvStarten(t, h, `{"fileIds":["`+f.ID+`"]}`)
	z := mkvWarten(t, h, f.ID)
	if z.Ergebnis != "fehler" || !strings.Contains(strings.ToLower(z.Fehler), "original bleibt unverändert") {
		t.Fatalf("Ergebnis %+v", z)
	}
	nachher, _ := os.ReadFile(f.Path)
	if string(vorher) != string(nachher) {
		t.Error("Original verändert")
	}
	ziel := strings.TrimSuffix(f.Path, ".avi") + ".mkv"
	if gibtEs(ziel) || gibtEs(umpacken.Temp(f.Path)) || gibtEs(filepath.Join(filepath.Dir(f.Path), mkvOriginalOrdner)) {
		t.Error("Reste liegen herum")
	}
	if neu, _ := h.st.File(t.Context(), f.ID); neu.Path != f.Path {
		t.Errorf("Datenbank verändert: %s", neu.Path)
	}
}

// Reicht der Platz nicht, beginnt nichts.
func TestMkvZuWenigPlatz(t *testing.T) {
	h := newHarness(t)
	_, f := altFilm(t, h, "Gross (2004)", ".avi", "-c:v", "mpeg4", "-c:a", "libmp3lame")
	alt := freierPlatz
	freierPlatz = func(string) uint64 { return 1 << 20 }
	t.Cleanup(func() { freierPlatz = alt })
	mkvStarten(t, h, `{"fileIds":["`+f.ID+`"]}`)
	z := mkvWarten(t, h, f.ID)
	if z.Ergebnis != "fehler" || !strings.Contains(strings.ToLower(z.Fehler), "zu wenig platz") {
		t.Fatalf("Ergebnis %+v", z)
	}
	if !gibtEs(f.Path) || gibtEs(umpacken.Temp(f.Path)) {
		t.Error("Original weg oder Reste da")
	}
}

// Liegt die MKV schon da, wird nichts überschrieben.
func TestMkvZielSchonDa(t *testing.T) {
	h := newHarness(t)
	_, f := altFilm(t, h, "Doppelt (2005)", ".avi", "-c:v", "mpeg4", "-c:a", "libmp3lame")
	ziel := strings.TrimSuffix(f.Path, ".avi") + ".mkv"
	_ = os.WriteFile(ziel, []byte("schon da"), 0o644)
	mkvStarten(t, h, `{"fileIds":["`+f.ID+`"]}`)
	if z := mkvWarten(t, h, f.ID); z.Ergebnis != "fehler" || !strings.Contains(strings.ToLower(z.Fehler), "schon") {
		t.Fatalf("Ergebnis %+v", z)
	}
	if b, _ := os.ReadFile(ziel); string(b) != "schon da" || !gibtEs(f.Path) {
		t.Error("überschrieben oder Original weg")
	}
}

// „Alle umwandeln" lässt MP4 aus; der Mitverwalter sieht den Weg nicht.
func TestMkvAlleOhneMP4UndRechte(t *testing.T) {
	h := newHarness(t)
	_, avi := altFilm(t, h, "Eins (2006)", ".avi", "-c:v", "mpeg4", "-c:a", "libmp3lame")
	_, mp4 := altFilm(t, h, "Zwei (2007)", ".mp4", "-c:v", "mpeg4", "-c:a", "aac")
	ctok, _ := h.mitverwalter("Carla")
	if resp, _ := h.do("GET", "/api/v1/admin/cleanup/mkv", "", ctok); resp.StatusCode != http.StatusNotFound && resp.StatusCode != http.StatusForbidden {
		t.Errorf("Mitverwalter Liste: %d", resp.StatusCode)
	}
	if resp, _ := h.do("POST", "/api/v1/admin/cleanup/mkv", `{"alle":true}`, ctok); resp.StatusCode != http.StatusNotFound && resp.StatusCode != http.StatusForbidden {
		t.Errorf("Mitverwalter Start: %d", resp.StatusCode)
	}
	if resp, _ := h.do("POST", "/api/v1/admin/cleanup/mkv", `{"alle":true,"original":"weg"}`, h.tok); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("unbekannte Wahl: %d", resp.StatusCode)
	}
	mkvStarten(t, h, `{"alle":true}`)
	if z := mkvWarten(t, h, avi.ID); z.Ergebnis != "fertig" {
		t.Fatalf("AVI: %+v", z)
	}
	if !gibtEs(mp4.Path) {
		t.Error("MP4 wurde mit „Alle“ umgewandelt")
	}
	for _, e := range mkvStandLesen(t, h).Ergebnisse {
		if e.FileID == mp4.ID {
			t.Error("MP4 in den Ergebnissen")
		}
	}
}

// Nach einem Absturz liegt die halbe MKV noch da: Der Start räumt sie
// weg — aber nur sie, nie eine andere Datei aus dem Vermerk.
func TestMkvVerwaisteAufraeumen(t *testing.T) {
	h := newHarness(t)
	dir := t.TempDir()
	tmp := umpacken.Temp(filepath.Join(dir, "Film.avi"))
	fremd := filepath.Join(dir, "Film.avi")
	_ = os.WriteFile(tmp, []byte("halb"), 0o644)
	_ = os.WriteFile(fremd, []byte("film"), 0o644)
	_ = os.WriteFile(h.api.mkvVermerk(), []byte(tmp+"\n"+fremd+"\nrelativ.lumen-tmp.mkv\n"), 0o600)
	h.api.mkvVerwaisteAufraeumen()
	if gibtEs(tmp) || !gibtEs(fremd) || gibtEs(h.api.mkvVermerk()) {
		t.Errorf("tmp=%v fremd=%v vermerk=%v", gibtEs(tmp), gibtEs(fremd), gibtEs(h.api.mkvVermerk()))
	}
}
