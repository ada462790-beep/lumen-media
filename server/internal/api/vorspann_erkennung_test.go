// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"lumen/internal/store"
	"lumen/internal/vorspann"
)

// folgeMitDatei hängt eine Datei an eine Folge.
func (h *harness) folgeMitDatei(folgeID, name string, dauerMS int64) *store.MediaFile {
	h.t.Helper()
	p := filepath.Join(h.root, name)
	if _, err := os.Stat(p); err != nil {
		if err := os.WriteFile(p, make([]byte, 1024), 0o644); err != nil {
			h.t.Fatal(err)
		}
	}
	fi, _ := os.Stat(p)
	f := &store.MediaFile{ItemID: folgeID, Path: p, SizeBytes: fi.Size(), MTime: fi.ModTime().Unix(),
		Container: "mkv", DurationMS: dauerMS}
	if err := h.st.UpsertFile(h.t.Context(), f); err != nil {
		h.t.Fatal(err)
	}
	// Spuren als gemessen eintragen: sonst misst der Titelabruf mit FFmpeg nach.
	if err := h.st.SpurenSetzen(h.t.Context(), f.ID, nil, nil); err != nil {
		h.t.Fatal(err)
	}
	return f
}

type introAntwort struct {
	Intro *struct {
		VonMs, BisMs int64
		Quelle       string
	}
	CreditsMs int64
}

func (h *harness) introVon(folgeID string) introAntwort {
	h.t.Helper()
	_, raw := h.do("GET", "/api/v1/items/"+folgeID, "", h.tok)
	var a introAntwort
	if err := json.Unmarshal(raw, &a); err != nil {
		h.t.Fatalf("%v: %s", err, raw)
	}
	return a
}

// Von Hand an der Folge > Kapitel > von Hand an der Serie > automatisch;
// automatisch nur ab der Mindestsicherheit und nur für die Datei, wie sie
// untersucht wurde.
func TestVorspannVorrang(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()
	serie, _, folgen := h.serie()
	folge := folgen[0]
	f := h.folgeMitDatei(folge, "s01e01.mkv", 1_800_000)

	if a := h.introVon(folge); a.Intro != nil || a.CreditsMs != 0 {
		t.Fatalf("ohne alles: %+v", a)
	}
	erkannt := func(sicher float64) {
		if err := h.st.ErkennungSetzen(ctx, store.Erkennung{FileID: f.ID, Groesse: f.SizeBytes, MTime: f.MTime,
			Fassung: vorspann.Fassung, Stand: store.ErkanntFertig, Partner: 4,
			VorspannVonMS: 10_000, VorspannBisMS: 50_000, VorspannSich: sicher,
			AbspannAbMS: 1_700_000, AbspannSich: sicher}); err != nil {
			t.Fatal(err)
		}
	}
	// Unter der Schwelle: gespeichert, nicht benutzt.
	erkannt(0.3)
	if a := h.introVon(folge); a.Intro != nil || a.CreditsMs != 0 {
		t.Fatalf("unsicheres Ergebnis benutzt: %+v", a)
	}
	erkannt(0.9)
	a := h.introVon(folge)
	if a.Intro == nil || a.Intro.Quelle != "automatisch" || a.Intro.VonMs != 10_000 || a.Intro.BisMs != 50_000 || a.CreditsMs != 1_700_000 {
		t.Fatalf("automatisch: %+v", a)
	}
	// Von Hand an der Serie schlägt das Erkannte; der Abspann bleibt erkannt.
	if r, b := h.do("PUT", "/api/v1/admin/items/"+serie+"/intro", `{"vonMs":5000,"bisMs":65000}`, h.tok); r.StatusCode != 204 {
		t.Fatalf("Serie markieren: %d %s", r.StatusCode, b)
	}
	if a := h.introVon(folge); a.Intro == nil || a.Intro.Quelle != "serie" || a.CreditsMs != 1_700_000 {
		t.Fatalf("Serie: %+v", a)
	}
	// Kapitel der Datei schlagen Serie und Erkanntes.
	if err := h.st.SpurenSetzen(ctx, f.ID, nil, []store.Chapter{
		{Index: 0, StartMS: 0, EndMS: 30_000, Kind: "intro"},
		{Index: 1, StartMS: 30_000, EndMS: 1_650_000, Kind: "normal"},
		{Index: 2, StartMS: 1_650_000, EndMS: 1_800_000, Kind: "credits"}}); err != nil {
		t.Fatal(err)
	}
	if a := h.introVon(folge); a.Intro == nil || a.Intro.Quelle != "kapitel" || a.CreditsMs != 1_650_000 {
		t.Fatalf("Kapitel: %+v", a)
	}
	// Von Hand an der Folge schlägt alles.
	if r, b := h.do("PUT", "/api/v1/admin/items/"+folge+"/intro", `{"vonMs":61000,"bisMs":99000}`, h.tok); r.StatusCode != 204 {
		t.Fatalf("Folge markieren: %d %s", r.StatusCode, b)
	}
	if a := h.introVon(folge); a.Intro == nil || a.Intro.Quelle != "folge" || a.Intro.VonMs != 61_000 {
		t.Fatalf("Folge: %+v", a)
	}
	// Eine Staffel lässt sich nicht markieren.
	_, staffeln, _ := h.serie()
	if r, _ := h.do("PUT", "/api/v1/admin/items/"+staffeln[0]+"/intro", `{"vonMs":0,"bisMs":9000}`, h.tok); r.StatusCode != 400 {
		t.Errorf("Staffel markiert: %d", r.StatusCode)
	}

	// Ändert sich die Datei, gilt das Erkannte nicht mehr.
	folge2 := folgen[1]
	f2 := h.folgeMitDatei(folge2, "s01e02.mkv", 1_800_000)
	if err := h.st.ErkennungSetzen(ctx, store.Erkennung{FileID: f2.ID, Groesse: f2.SizeBytes, MTime: f2.MTime,
		Fassung: vorspann.Fassung, Stand: store.ErkanntFertig, VorspannVonMS: 1000, VorspannBisMS: 40_000, VorspannSich: 1}); err != nil {
		t.Fatal(err)
	}
	if err := h.st.VorspannLoeschen(ctx, serie); err != nil {
		t.Fatal(err)
	}
	if a := h.introVon(folge2); a.Intro == nil || a.Intro.Quelle != "automatisch" {
		t.Fatalf("zweite Folge: %+v", a)
	}
	f2.SizeBytes++
	if err := h.st.UpsertFile(ctx, f2); err != nil {
		t.Fatal(err)
	}
	if a := h.introVon(folge2); a.Intro != nil {
		t.Fatalf("Erkanntes gilt trotz geänderter Datei: %+v", a)
	}
}

// Was offen ist, wird einmal untersucht; „einsam" kommt wieder dran, wenn
// die Serie eine Folge dazubekommt; eine neue Fassung des Verfahrens macht
// alles wieder offen.
func TestErkennungOffenUndZwischenspeicher(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()
	serie, _, folgen := h.serie()
	f := h.folgeMitDatei(folgen[0], "a.mkv", 1_500_000)
	h.folgeMitDatei(folgen[1], "kurz.mkv", 60_000) // zu kurz, kommt nie dran

	offen := func(fassung int) []string {
		t.Helper()
		s, err := h.st.ErkennungOffeneSerien(ctx, fassung, 10)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	if s := offen(vorspann.Fassung); len(s) != 1 || s[0] != serie {
		t.Fatalf("offen: %v", s)
	}
	n, _ := h.st.FolgenDerSerie(ctx, serie)
	if n != 6 {
		t.Fatalf("Folgen der Serie: %d", n)
	}
	if err := h.st.ErkennungSetzen(ctx, store.Erkennung{FileID: f.ID, Groesse: f.SizeBytes, MTime: f.MTime,
		Fassung: vorspann.Fassung, Stand: store.ErkanntEinsam, Folgen: n}); err != nil {
		t.Fatal(err)
	}
	if s := offen(vorspann.Fassung); len(s) != 0 {
		t.Fatalf("nach dem Untersuchen noch offen: %v", s)
	}
	if s := offen(vorspann.Fassung + 1); len(s) != 1 {
		t.Fatalf("neue Fassung macht nicht wieder offen: %v", s)
	}
	z, err := h.st.ErkennungZaehlen(ctx, vorspann.Fassung, vorspann.Mindestsicherheit)
	if err != nil || z.Untersucht != 1 || z.Offen != 0 {
		t.Fatalf("Zählung: %+v %v", z, err)
	}
	// Eine Folge mehr: „einsam" ist wieder offen.
	staffel, _ := h.st.Item(ctx, folgen[0])
	e := &store.Item{LibraryID: h.lib.ID, Kind: "episode", ParentID: staffel.ParentID, Title: "Folge", SeasonNo: 1, EpisodeNo: 9, MatchState: "matched"}
	if err := h.st.UpsertItem(ctx, e); err != nil {
		t.Fatal(err)
	}
	if s := offen(vorspann.Fassung); len(s) != 1 {
		t.Fatalf("einsam nach neuer Folge nicht offen: %v", s)
	}
	// Die Datei verschwindet aus der Datenbank: die Zeile mit ihr.
	if _, err := h.st.DB().Exec(`DELETE FROM media_file WHERE id = ?`, f.ID); err != nil {
		t.Fatal(err)
	}
	var rest int
	_ = h.st.DB().QueryRow(`SELECT COUNT(*) FROM vorspann_erkannt`).Scan(&rest)
	if rest != 0 {
		t.Errorf("Ergebnis überlebt die Datei: %d", rest)
	}
}

func TestErkennungNachbarn(t *testing.T) {
	var v []store.ErkennungsDatei
	for s := 1; s <= 2; s++ {
		for f := 1; f <= 5; f++ {
			v = append(v, store.ErkennungsDatei{FileID: fmt.Sprintf("s%de%d", s, f), StaffelID: fmt.Sprint(s)})
		}
	}
	v = append(v, store.ErkennungsDatei{FileID: "s3e1", StaffelID: "3"})
	ids := func(x []store.ErkennungsDatei) []string {
		var out []string
		for _, d := range x {
			out = append(out, d.FileID)
		}
		return out
	}
	if got := fmt.Sprint(ids(erkennungNachbarn(v, 0, "1"))); got != "[s1e2 s1e3 s1e4 s1e5]" {
		t.Errorf("Anfang der Staffel: %s", got)
	}
	if got := fmt.Sprint(ids(erkennungNachbarn(v, 6, "2"))); got != "[s2e1 s2e3 s2e4 s2e5]" {
		t.Errorf("Mitte: %s", got)
	}
	// Staffel 3 hat nur eine Folge: Nachbarn aus der Serie.
	if got := fmt.Sprint(ids(erkennungNachbarn(v, 10, "3"))); got != "[s2e5 s2e4 s2e3 s2e2]" {
		t.Errorf("einsame Staffel: %s", got)
	}
	if got := erkennungNachbarn(v[:1], 0, "1"); len(got) != 0 {
		t.Errorf("einzige Folge: %v", got)
	}
}

// Mit echtem FFmpeg: drei Folgen aus FFmpeg-Quellen (Rauschen als
// Handlung, eine feste Tonfolge als Vorspann an verschiedenen Stellen,
// eine andere als Abspann), als AAC in MKV — also verlustbehaftet, wie im
// Leben. Der ganze Weg: Durchgang, Datenbank, Titelabruf.
func TestErkennungMitFFmpeg(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("kein FFmpeg")
	}
	if testing.Short() {
		t.Skip("baut Videos")
	}
	h := newHarness(t)
	ctx := t.Context()
	if h.lib == nil {
		h.film("Nordlicht", 1)
	}
	se := &store.Item{LibraryID: h.lib.ID, Kind: "series", Title: "Tiefsee", MatchState: "matched"}
	_ = h.st.UpsertItem(ctx, se)
	st := &store.Item{LibraryID: h.lib.ID, Kind: "season", ParentID: se.ID, Title: "Staffel", SeasonNo: 1, MatchState: "matched"}
	_ = h.st.UpsertItem(ctx, st)

	const dauer, vorspannSek, abspannSek = 360.0, 30.0, 25.0
	melodie := func(saat int) string {
		return fmt.Sprintf("aevalsrc=exprs='0.25*sin(2*PI*t*(280+90*mod(floor(3*t)*%d\\,17)))+0.1*sin(2*PI*t*(140+40*mod(floor(t)*5\\,7)))':s=22050", saat)
	}
	kalt := []float64{0, 41.3, 77.7}
	var folgen []string
	var dateien []*store.MediaFile
	start := time.Now()
	for i, k := range kalt {
		p := filepath.Join(h.root, fmt.Sprintf("folge%d.mkv", i))
		rest := dauer - k - vorspannSek - abspannSek
		var graph string
		n := 0
		if k > 0 {
			graph += fmt.Sprintf("anoisesrc=d=%.3f:c=pink:seed=%d:a=0.3:r=22050[k];", k, 10+i)
			n++
		}
		graph += fmt.Sprintf("%s:d=%.0f[v];anoisesrc=d=%.3f:c=pink:seed=%d:a=0.3:r=22050[r];%s:d=%.0f[e];",
			melodie(7), vorspannSek, rest, 20+i, melodie(11), abspannSek)
		if k > 0 {
			graph += "[k]"
		}
		graph += fmt.Sprintf("[v][r][e]concat=n=%d:v=0:a=1[a]", n+3)
		cmd := exec.CommandContext(ctx, "ffmpeg", "-hide_banner", "-loglevel", "error", "-y",
			"-f", "lavfi", "-i", fmt.Sprintf("color=c=black:s=32x32:r=1:d=%.0f", dauer),
			"-filter_complex", graph, "-map", "0:v", "-map", "[a]",
			"-c:v", "libx264", "-preset", "ultrafast", "-c:a", "aac", "-b:a", "96k", "-shortest", p)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("FFmpeg kann die Testvideos nicht bauen: %v %s", err, out)
		}
		e := &store.Item{LibraryID: h.lib.ID, Kind: "episode", ParentID: st.ID, Title: "Folge", SeasonNo: 1, EpisodeNo: i + 1, MatchState: "matched"}
		_ = h.st.UpsertItem(ctx, e)
		folgen = append(folgen, e.ID)
		dateien = append(dateien, h.folgeMitDatei(e.ID, filepath.Base(p), int64(dauer*1000)))
	}
	t.Logf("Testvideos: %v", time.Since(start).Round(time.Millisecond))

	start = time.Now()
	h.api.erkennungDurchgang(ctx, 0)
	t.Logf("Erkennung: %v für %d Folgen", time.Since(start).Round(time.Millisecond), len(folgen))

	for i, f := range dateien {
		e, err := h.st.Erkannt(ctx, f.ID, f.SizeBytes, f.MTime, vorspann.Fassung)
		if err != nil || e == nil {
			t.Fatalf("Folge %d: nicht untersucht (%v)", i, err)
		}
		t.Logf("Folge %d: Vorspann %d–%d ms (%.2f), Abspann ab %d ms (%.2f), %d Partner",
			i, e.VorspannVonMS, e.VorspannBisMS, e.VorspannSich, e.AbspannAbMS, e.AbspannSich, e.Partner)
		a := h.introVon(folgen[i])
		if a.Intro == nil || a.Intro.Quelle != "automatisch" {
			t.Errorf("Folge %d: kein automatischer Vorspann: %+v", i, a)
			continue
		}
		if d := math.Abs(float64(a.Intro.VonMs)/1000 - kalt[i]); d > 1.0 {
			t.Errorf("Folge %d: Vorspann beginnt %.2f s daneben", i, d)
		}
		if d := math.Abs(float64(a.Intro.BisMs)/1000 - (kalt[i] + vorspannSek)); d > 1.0 {
			t.Errorf("Folge %d: Vorspann endet %.2f s daneben", i, d)
		}
		if d := math.Abs(float64(a.CreditsMs)/1000 - (dauer - abspannSek)); d > 1.0 {
			t.Errorf("Folge %d: Abspann beginnt %.2f s daneben (%d)", i, d, a.CreditsMs)
		}
	}
	// Ein zweiter Durchgang liest nichts mehr.
	if s, _ := h.st.ErkennungOffeneSerien(ctx, vorspann.Fassung, 10); len(s) != 0 {
		t.Errorf("nach dem Durchgang noch offen: %v", s)
	}
}
