// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package stream

import (
	"context"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Ein Gerät, das „hls" meldet (Apple TV), bekommt für eine MKV mit
// passendem Bild eine Kopie — früher wurde jedes Bild neu gerechnet.
func TestHLSKopiertPassendesBild(t *testing.T) {
	apple := Device{Codecs: []string{"h264", "hevc"}, Containers: []string{"mp4", "hls"},
		Audio: []string{"aac", "ac3", "eac3"}, MaxHeight: 2160, HDR: true}
	p := Decide(FileInfo("hevc", "mkv", 2160, 40_000_000, "HDR").MitTon("dts"), apple, 0)
	if p.Direct || p.VideoCodec != "copy" {
		t.Fatalf("DTS allein darf das Bild nicht neu rechnen lassen: %+v", p)
	}
	p = Decide(FileInfo("hevc", "mkv", 2160, 40_000_000, "").MitTon("ac3"), apple, 0).FuerHLS()
	if p.VideoCodec != "copy" {
		t.Fatalf("über HLS muss kopiert werden: %+v", p)
	}
	// Bitratengrenze unterwegs: dann wirklich neu rechnen.
	p = Decide(FileInfo("h264", "mkv", 1080, 20_000_000, ""), apple, 5_000_000)
	if p.VideoCodec != "h264" || p.Bitrate != 5_000_000 {
		t.Fatalf("Grenze ignoriert: %+v", p)
	}
	// Einbrennen zwingt zum Neurechnen.
	p = Decide(FileInfo("h264", "mp4", 1080, 8_000_000, "").MitEinbrennen(), apple, 0)
	if p.Direct || p.VideoCodec != "h264" {
		t.Fatalf("Einbrennen ohne Neurechnen: %+v", p)
	}
}

func TestCodecKennung(t *testing.T) {
	for _, f := range []struct {
		c, p string
		l    int
		soll string
	}{
		{"h264", "High", 41, "avc1.640029"},
		{"h264", "Main", 40, "avc1.4D0028"},
		{"h264", "Constrained Baseline", 31, "avc1.42E01F"},
		{"hevc", "Main 10", 153, "hvc1.2.4.L153.B0"},
		{"hevc", "Main", 120, "hvc1.1.6.L120.B0"},
	} {
		if g := CodecKennung(f.c, f.p, f.l); g != f.soll {
			t.Errorf("%s %s %d: %s, erwartet %s", f.c, f.p, f.l, g, f.soll)
		}
	}
	if TonKennung("eac3") != "ec-3" || TonKennung("ac3") != "ac-3" || TonKennung("aac") != "mp4a.40.2" {
		t.Error("Tonkennungen stimmen nicht")
	}
}

// Die Master-Liste nennt, was wirklich kommt: Seitenverhältnis, Codec, HDR.
func TestMasterAusEchtenWerten(t *testing.T) {
	tr := newTr(t)
	j := Job{Plan: Plan{VideoCodec: "copy", Bitrate: 30_000_000}, QuellCodec: "hevc",
		TonKopieren: false,
		Details:     &BildDetails{Codec: "hevc", Profil: "Main 10", Level: 153, Breite: 3840, Hoehe: 1606, FPS: 23.976, Transfer: "smpte2084"}}
	m := tr.Master(j, "media.m3u8", "")
	for _, soll := range []string{"RESOLUTION=3840x1606", `CODECS="hvc1.2.4.L153.B0,mp4a.40.2"`, "VIDEO-RANGE=PQ", "#EXT-X-VERSION:7"} {
		if !strings.Contains(m, soll) {
			t.Errorf("%q fehlt:\n%s", soll, m)
		}
	}
	// Neu gerechnet auf 720p: 2,4:1 bleibt 2,4:1, die Grenze bestimmt BANDWIDTH.
	j = Job{Plan: Plan{VideoCodec: "h264", Height: 720, Bitrate: 2_000_000},
		Details: &BildDetails{Breite: 1920, Hoehe: 800, FPS: 25}}
	m = tr.Master(j, "media.m3u8", "")
	if !strings.Contains(m, "RESOLUTION=1728x720") {
		t.Errorf("Auflösung falsch:\n%s", m)
	}
	if !strings.Contains(m, "BANDWIDTH=2160000") || !strings.Contains(m, "VIDEO-RANGE=SDR") {
		t.Errorf("Bandbreite oder Bereich falsch:\n%s", m)
	}
}

func TestSegmentGrenzen(t *testing.T) {
	var k []float64
	for i := 0; i <= 60; i++ {
		k = append(k, float64(i)) // jede Sekunde ein Schlüsselbild
	}
	g := SegmentGrenzen(k, 60.5, 4)
	if len(g) != 15 || g[1] != 4 || g[2] != 8 {
		t.Fatalf("Grenzen: %v", g)
	}
	// Schlüsselbilder alle 3 s: lieber 3 als 6.
	k = []float64{0, 3, 6, 9, 12, 15}
	if g := SegmentGrenzen(k, 16, 4); len(g) < 4 || g[1] != 3 {
		t.Fatalf("3er-Abstand: %v", g)
	}
	// Zu grosse Lücken: nil (dann wird neu gerechnet).
	if g := SegmentGrenzen([]float64{0, 30}, 60, 4); g != nil {
		t.Fatalf("Lücke nicht erkannt: %v", g)
	}
}

func TestRasterGrenzenUndListe(t *testing.T) {
	tr := newTr(t)
	pl := tr.Playlist(Job{DurationMS: 10_500})
	if strings.Count(pl, ".ts") != 3 || !strings.Contains(pl, "#EXTINF:2.500000,\nseg00002.ts") {
		t.Fatalf("Liste:\n%s", pl)
	}
	j := Job{DurationMS: 10_000, Plan: Plan{VideoCodec: "copy"}, QuellCodec: "hevc", Grenzen: []float64{0, 3.5, 7}}
	pl = tr.Playlist(j)
	if !strings.Contains(pl, `#EXT-X-MAP:URI="init.mp4"`) || !strings.Contains(pl, "#EXTINF:3.500000,\nseg00001.m4s") {
		t.Fatalf("fMP4-Liste:\n%s", pl)
	}
}

// Die Grenzen aus dem MKV-Verzeichnis stimmen mit dem überein, was ffprobe
// als Schlüsselbilder sieht.
func TestMKVSchluesselbilder(t *testing.T) {
	src := testFilmMitGOP(t, 20, 50) // 25 fps, alle 2 s ein Schlüsselbild
	k, err := MKVSchluesselbilder(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(k) != 10 || k[0] != 0 || k[1] != 2 {
		t.Fatalf("Schlüsselbilder: %v", k)
	}
}

func testFilmMitGOP(t *testing.T, sek, gop int) string {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ohne FFmpeg nicht prüfbar")
	}
	p := filepath.Join(t.TempDir(), "gop.mkv")
	out, err := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "testsrc=size=320x180:rate=25:duration="+strconv.Itoa(sek),
		"-f", "lavfi", "-i", "sine=frequency=440:duration="+strconv.Itoa(sek),
		"-c:v", "libx264", "-preset", "ultrafast", "-g", strconv.Itoa(gop), "-keyint_min", strconv.Itoa(gop),
		"-sc_threshold", "0", "-c:a", "ac3", p, "-y").CombinedOutput()
	if err != nil {
		t.Fatalf("%v %s", err, out)
	}
	return p
}

func startZeit(t *testing.T, pfad string) float64 {
	t.Helper()
	out, err := exec.Command("ffprobe", "-v", "error", "-select_streams", "v:0",
		"-show_entries", "stream=start_time", "-of", "csv=p=0", pfad).Output()
	if err != nil {
		t.Fatal(err)
	}
	v, _ := strconv.ParseFloat(strings.TrimSpace(strings.Split(string(out), "\n")[0]), 64)
	return v
}

// Nach einem Sprung tragen die Segmente die Zeit der Datei, nicht „ab
// Neustart bei null" — beim Neurechnen wie beim Kopieren.
func TestZeitstempelNachSprung(t *testing.T) {
	src := testFilmMitGOP(t, 60, 50)
	for _, kopie := range []bool{false, true} {
		tr := newTr(t)
		j := Job{SessionID: "z", SourcePath: src, DurationMS: 60_000, AudioIndex: -1,
			Plan: Plan{VideoCodec: "h264", Height: 180}, QuellCodec: "h264"}
		if kopie {
			j.Plan.VideoCodec = "copy"
			j.Grenzen = KopierGrenzen(src, 60_000, 0)
			if j.Grenzen == nil {
				t.Fatal("keine Grenzen")
			}
		}
		g := j.SegmentGrenzen()
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		p0, err := tr.Segment(ctx, j, 0)
		if err != nil {
			t.Fatal(err)
		}
		basis := startZeit(t, p0) // MPEG-TS beginnt mit einem festen Versatz
		ziel := len(g) - 3
		p, err := tr.Segment(ctx, j, ziel)
		cancel()
		if err != nil {
			t.Fatalf("Sprung (kopie=%v): %v", kopie, err)
		}
		if d := startZeit(t, p) - basis - g[ziel]; d < -0.05 || d > 0.05 {
			t.Errorf("kopie=%v: Segment %d beginnt %.3f s neben %.3f", kopie, ziel, d, g[ziel])
		}
	}
}

// Weit vorspulen startet sofort neu, statt bis zu 45 s zu warten.
func TestWeitVorspulenStartetNeu(t *testing.T) {
	tr := newTr(t)
	src := testFilm(t, 120)
	j := Job{SessionID: "v", SourcePath: src, DurationMS: 120_000, AudioIndex: -1,
		Plan: Plan{VideoCodec: "h264", Height: 360}}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if _, err := tr.Segment(ctx, j, 0); err != nil {
		t.Fatal(err)
	}
	tr.mu.Lock()
	erster := tr.jobs["v"]
	tr.mu.Unlock()
	if _, err := tr.Segment(ctx, j, 6); err != nil {
		t.Fatal(err)
	}
	tr.mu.Lock()
	zweiter := tr.jobs["v"]
	tr.mu.Unlock()
	if erster == zweiter || zweiter.fromSeg != 6 {
		t.Fatalf("kein Neustart bei Segment 6 (fromSeg=%d)", zweiter.fromSeg)
	}
}

// Zwei gleichzeitige Anfragen derselben Wiedergabe starten nur einen Lauf.
func TestGleichzeitigeAnfragen(t *testing.T) {
	tr := newTr(t)
	src := testFilm(t, 60)
	j := Job{SessionID: "g", SourcePath: src, DurationMS: 60_000, AudioIndex: -1,
		Plan: Plan{VideoCodec: "h264", Height: 360}}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	fehler := make(chan error, 2)
	for _, i := range []int{10, 11} {
		go func(i int) { _, err := tr.Segment(ctx, j, i); fehler <- err }(i)
	}
	for range 2 {
		if err := <-fehler; err != nil {
			t.Fatal(err)
		}
	}
	if tr.Active() != 1 {
		t.Fatalf("%d Läufe", tr.Active())
	}
}

// Ohne Hardware bleibt die Bildkette bei libx264; HDR wird umgerechnet und
// als BT.709 gekennzeichnet.
func TestBildketteSoftware(t *testing.T) {
	ff := NewFFmpeg("", "")
	b := ff.BildBefehl(BildAuftrag{Quelle: &BildDetails{Breite: 3840, Hoehe: 2160, Transfer: "smpte2084"},
		ZielHoehe: 1080, Tonemap: true, Einbrennen: 5})
	text := strings.Join(b.Kodierer, " ")
	if b.Verfahren != "" || !strings.Contains(text, "libx264") || !strings.Contains(text, "-color_trc bt709") {
		t.Errorf("Kodierer: %s", text)
	}
	if !strings.Contains(b.Filter, "scale=1920:1080") || !strings.Contains(b.Filter, "[0:5]") ||
		!strings.Contains(b.Filter, "overlay") || !strings.HasSuffix(b.Filter, "[v]") {
		t.Errorf("Filter: %s", b.Filter)
	}
}

// Mit (vorgetäuschter) Hardware: voll im Videoteil, aber gemischt, wenn
// eingebrannt werden muss; Software, wenn zurückgefallen.
func TestBildketteHardware(t *testing.T) {
	ff := NewFFmpeg("", "")
	ff.HW = NeueHardware(ff, HWAuto, "/dev/dri/renderD128")
	ff.HW.setzeErkannt(HWVAAPI, true)
	q := &BildDetails{Breite: 1920, Hoehe: 1080}
	b := ff.BildBefehl(BildAuftrag{Quelle: q, Einbrennen: -1})
	if b.Verfahren != HWVAAPI || b.Stufe != StufeVoll || !strings.Contains(strings.Join(b.VorEingang, " "), "-hwaccel_output_format vaapi") {
		t.Errorf("voll: %+v", b)
	}
	b = ff.BildBefehl(BildAuftrag{Quelle: q, Einbrennen: 3})
	if b.Stufe != StufeGemischt || !strings.Contains(b.Filter, "hwupload") || !strings.Contains(b.Filter, "overlay") {
		t.Errorf("gemischt: %+v", b)
	}
	b = ff.BildBefehl(BildAuftrag{Quelle: q, Einbrennen: -1, Stufe: StufeSoftware})
	if b.Verfahren != "" || !strings.Contains(strings.Join(b.Kodierer, " "), "libx264") {
		t.Errorf("Rückfall: %+v", b)
	}
	// Dreimal gescheitert: das Verfahren ruht.
	for range 3 {
		ff.HW.Fehlschlag(HWVAAPI)
	}
	if ff.HW.Aktiv() != "" {
		t.Error("gescheitertes Verfahren ist noch aktiv")
	}
	ff.HW.Setzen(HWAus)
	if ff.HW.Aktiv() != "" {
		t.Error("„aus\" nimmt trotzdem Hardware")
	}
}

// Eine Direktwiedergabe mit vielen Bereichsanfragen ist eine Wiedergabe.
func TestBeitretenEineWiedergabe(t *testing.T) {
	l := NewLive()
	neu := func() *Session {
		return &Session{AccountID: "a", DeviceName: "TV", FileID: "f", Mode: "direct"}
	}
	s1, _ := l.Beitreten(neu(), 1)
	s2, _ := l.Beitreten(neu(), 1)
	if s1 == nil || s2 == nil || s1.ID != s2.ID || l.CountFor("a") != 1 {
		t.Fatal("Spulen zählt als zweite Wiedergabe")
	}
	// Anderes Gerät, Grenze 1: abgelehnt.
	anders := neu()
	anders.DeviceName = "Handy"
	if s, n := l.Beitreten(anders, 1); s != nil || n != 1 {
		t.Fatal("Grenze nicht geprüft")
	}
	// Offene Anfrage: nie verwaist; ohne: nach der kurzen Frist.
	zu, _ := l.Oeffnen(s1.ID)
	if len(l.VerwaisteMit(0, 0)) != 0 {
		t.Fatal("offene Anfrage als verwaist gemeldet")
	}
	zu()
	if len(l.VerwaisteMit(time.Hour, 0)) != 1 {
		t.Fatal("geschlossene Direktwiedergabe bleibt")
	}
	l.EndMit(s1.ID, GrundAbgelaufen)
	if l.Grund(s1.ID) != GrundAbgelaufen {
		t.Fatal("Grund vergessen")
	}
}

// Vorschaubilder: ein Blatt mit 10 × 10 Kacheln, auch bei einem kurzen Film.
func TestTrickplay(t *testing.T) {
	src := testFilmMitGOP(t, 25, 25)
	ff := NewFFmpeg("", "")
	ziel := filepath.Join(t.TempDir(), "tp")
	d, _ := ff.Feinmessung(context.Background(), src)
	info, err := ff.TrickplayErzeugen(context.Background(), src, 25_000, d, ziel)
	if err != nil {
		t.Fatal(err)
	}
	if info.Bilder != 3 || info.Blaetter != 1 || info.Hoehe != 180 {
		t.Fatalf("%+v", info)
	}
	gelesen, err := TrickplayLesen(ziel)
	if err != nil || *gelesen != *info {
		t.Fatalf("info.json: %v %+v", err, gelesen)
	}
	out, _ := exec.Command("ffprobe", "-v", "error", "-show_entries", "stream=width,height", "-of", "csv=p=0",
		filepath.Join(ziel, "0.jpg")).Output()
	if strings.TrimSpace(string(out)) != "3200,1800" {
		t.Fatalf("Blatt: %s", out)
	}
}
