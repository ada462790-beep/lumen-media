// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

// Tonspuren und Untertitel.
//
// Die Tonspur wählt das Gerät mit ?spur=<index> am Strom (browser.mp4 und
// HLS) — gemappt wird dann genau diese Spur. Untertitel liefert Lumen als
// WebVTT, das jeder Browser und jede App versteht: eingebettete Textspuren
// zieht FFmpeg heraus, .srt-Dateien daneben wandelt Lumen selbst um (auch
// ohne FFmpeg). Bild-Untertitel (PGS, DVD) gehen nicht als Text; sie
// werden beim Umwandeln ins Bild gerechnet (?untertitel=<index>, und
// erzwungene in der Sprache der Tonspur von selbst — wiedergabe.go).
//
// Dateien, die vor dieser Fassung eingelesen wurden, kennen ihre Spuren
// noch nicht; sie werden beim ersten Öffnen des Titels nachgemessen.

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"lumen/internal/scan"
	"lumen/internal/store"
	"lumen/internal/stream"
)

// mitSpuren füllt Spuren und Kapitel der Dateien eines Titels — aus der
// Datenbank, oder, wenn noch nie gemessen, jetzt.
func (s *Server) mitSpuren(ctx context.Context, files []store.MediaFile) {
	for i := range files {
		f := &files[i]
		if f.Missing {
			continue
		}
		if !s.st.SpurenGelesen(ctx, f.ID) {
			s.spurenNachmessen(ctx, f)
		}
		f.Spuren, _ = s.st.Spuren(ctx, f.ID)
		f.Kapitel, _ = s.st.Kapitel(ctx, f.ID)
	}
}

func (s *Server) spurenNachmessen(ctx context.Context, f *store.MediaFile) {
	var tracks []store.Track
	var chapters []store.Chapter
	if s.ff != nil && s.ff.Available() {
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		if p, err := s.ff.Probe(ctx, f.Path); err == nil {
			tracks, chapters = scan.SpurenFuerStore(p.Tracks), scan.KapitelFuerStore(p.Chapters)
		} else {
			// Lässt sich die Datei nicht messen, bleiben wenigstens die
			// Untertitel daneben — und beim nächsten Mal wird nicht wieder gemessen.
			s.log.Warn("spuren messen", "err", stream.Bereinigt(err.Error()))
			tracks = scan.SpurenFuerStore(scan.SidecarSubtitles(f.Path))
		}
	} else {
		tracks = scan.SpurenFuerStore(scan.SidecarSubtitles(f.Path))
	}
	if err := s.st.SpurenSetzen(ctx, f.ID, tracks, chapters); err != nil {
		s.log.Warn("spuren speichern", "err", err)
	}
}

// untertitel liefert eine Untertitelspur als WebVTT.
func (s *Server) untertitel(w http.ResponseWriter, r *http.Request) {
	f, _, err := s.fileForRequest(r)
	if err != nil {
		failErr(w, err)
		return
	}
	idx, err := strconv.Atoi(strings.TrimSuffix(r.PathValue("spur"), ".vtt"))
	if err != nil {
		fail(w, http.StatusNotFound, "nicht gefunden")
		return
	}
	if !s.st.SpurenGelesen(r.Context(), f.ID) {
		s.spurenNachmessen(r.Context(), f)
	}
	t, err := s.st.Spur(r.Context(), f.ID, idx)
	if err != nil || t.Kind != "subtitle" || t.Bitmap {
		fail(w, http.StatusNotFound, "diese untertitelspur gibt es nicht")
		return
	}

	var inhalt []byte
	if t.ExternalPath == "" {
		// Eingebettet: aus dem Vorrat, dem Zwischenspeicher oder im
		// Hintergrund gezogen (untertitel_ziehen.go).
		if inhalt = s.eingebetteteUntertitel(w, r, f, idx); inhalt == nil {
			return
		}
	} else {
		// Einmal gewandelt, dann aus dem Zwischenspeicher im Arbeitsspeicher.
		// Bewusst nicht auf der Platte: Eine Datei „<Datei>-<Spur>.vtt", die
		// beim ersten Abspielen entsteht, hielte mit ihrem Zeitstempel fest,
		// wann jemand diesen Film angefangen hat.
		schluessel := fmt.Sprintf("%s-%d", f.ID, idx)
		var da bool
		inhalt, da = vttZwischen.holen(schluessel)
		if !da {
			inhalt, err = s.vttErzeugen(r.Context(), f.Path, t)
			if err != nil {
				s.log.Warn("untertitel", "err", stream.Bereinigt(err.Error()))
				fail(w, http.StatusUnprocessableEntity, "diese untertitel lassen sich nicht lesen")
				return
			}
			vttZwischen.ablegen(schluessel, inhalt)
		}
	}
	w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
	w.Header().Set("Cache-Control", "private, max-age=86400")
	_, _ = w.Write(inhalt)
}

// vttZwischen hält gewandelte Untertitel im Arbeitsspeicher, höchstens
// 64 Spuren oder 32 MB. Nach einem Neustart ist er leer.
var vttZwischen = &vttSpeicher{inhalt: map[string][]byte{}}

type vttSpeicher struct {
	mu     sync.Mutex
	inhalt map[string][]byte
	bytes  int
}

func (v *vttSpeicher) holen(k string) ([]byte, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	b, ok := v.inhalt[k]
	return b, ok
}

func (v *vttSpeicher) ablegen(k string, b []byte) {
	v.mu.Lock()
	defer v.mu.Unlock()
	for k2, alt := range v.inhalt {
		if len(v.inhalt) < 64 && v.bytes+len(b) <= 32<<20 {
			break
		}
		v.bytes -= len(alt)
		delete(v.inhalt, k2)
	}
	if len(b) > 32<<20 {
		return
	}
	v.inhalt[k] = b
	v.bytes += len(b)
}

// vttErzeugen wandelt eine Spur in WebVTT. Die Zwischendatei hat einen
// zufälligen Namen und ist gleich wieder weg.
func (s *Server) vttErzeugen(ctx context.Context, video string, t *store.Track) ([]byte, error) {
	tmp, err := os.CreateTemp(s.dataDir, "vtt-*.tmp")
	if err != nil {
		return nil, err
	}
	name := tmp.Name()
	tmp.Close()
	defer os.Remove(name)
	ziel := name + ".vtt"
	defer os.Remove(ziel)
	if err := s.vttSchreiben(ctx, video, t, ziel); err != nil {
		return nil, err
	}
	return os.ReadFile(ziel)
}

func (s *Server) vttSchreiben(ctx context.Context, video string, t *store.Track, ziel string) error {
	tmp := ziel + ".tmp"
	defer os.Remove(tmp)
	// .srt daneben: ohne FFmpeg, in Go — das Format ist einfach genug.
	if t.ExternalPath != "" {
		// Auch beim Ausliefern: nur eine gewöhnliche Datei, kein Verweis.
		if fi, err := os.Lstat(t.ExternalPath); err != nil || !fi.Mode().IsRegular() {
			return fmt.Errorf("untertitel-datei ist keine gewöhnliche datei")
		}
	}
	if t.ExternalPath != "" && strings.EqualFold(filepath.Ext(t.ExternalPath), ".srt") {
		if err := srtNachVTT(t.ExternalPath, tmp); err != nil {
			return err
		}
		return os.Rename(tmp, ziel)
	}
	if s.ff == nil || !s.ff.Available() {
		return fmt.Errorf("ohne FFmpeg nur .srt-Dateien")
	}
	quelle, mapping := video, fmt.Sprintf("0:%d", t.Index)
	if t.ExternalPath != "" {
		quelle, mapping = t.ExternalPath, "0:s:0"
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, s.ff.FFmpegPath, "-hide_banner", "-loglevel", "error", "-nostdin",
		"-y", "-i", quelle, "-map", mapping, "-f", "webvtt", tmp)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ffmpeg: %s", strings.TrimSpace(string(out)))
	}
	return os.Rename(tmp, ziel)
}

var reSRTZeit = regexp.MustCompile(`^(\d+):(\d+):(\d+)[,.](\d+)\s*-->\s*(\d+):(\d+):(\d+)[,.](\d+)`)

// srtNachVTT wandelt SubRip in WebVTT: Kopfzeile, Punkt statt Komma in
// den Zeiten, Nummern weglassen. Windows-Zeilenenden und eine BOM
// verträgt es; Latin-1 wird als UTF-8 gelesen, so gut es geht.
func srtNachVTT(von, nach string) error {
	in, err := os.Open(von)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(nach)
	if err != nil {
		return err
	}
	defer out.Close()
	w := bufio.NewWriter(out)
	w.WriteString("WEBVTT\n\n")
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	erste := true
	for sc.Scan() {
		z := strings.TrimRight(sc.Text(), "\r")
		if erste {
			z = strings.TrimPrefix(z, "\ufeff")
			erste = false
		}
		if m := reSRTZeit.FindStringSubmatch(z); m != nil {
			fmt.Fprintf(w, "%s:%s:%s.%s --> %s:%s:%s.%s\n", m[1], m[2], m[3], pad3(m[4]), m[5], m[6], m[7], pad3(m[8]))
			continue
		}
		// Die laufende Nummer vor der Zeitzeile ist in VTT nur ein
		// Hinweis — weglassen, damit sie nicht als Text erscheint.
		if _, err := strconv.Atoi(strings.TrimSpace(z)); err == nil && z != "" {
			continue
		}
		w.WriteString(z + "\n")
	}
	if err := sc.Err(); err != nil {
		return err
	}
	return w.Flush()
}

func pad3(s string) string {
	for len(s) < 3 {
		s += "0"
	}
	return s[:3]
}

// hlsUntertitel ist die Abspielliste einer Untertitelspur für HLS: ein
// einziges WebVTT-Segment über die ganze Laufzeit.
func (s *Server) hlsUntertitel(w http.ResponseWriter, r *http.Request) {
	f, _, err := s.fileForRequest(r)
	if err != nil {
		failErr(w, err)
		return
	}
	idx := strings.TrimSuffix(r.PathValue("spur"), ".m3u8")
	if n, err := strconv.Atoi(idx); err != nil || n < 0 {
		fail(w, http.StatusNotFound, "nicht gefunden")
		return
	}
	sek := float64(f.DurationMS) / 1000
	if sek <= 0 {
		sek = 3600 * 4
	}
	body := fmt.Sprintf("#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:%d\n#EXT-X-MEDIA-SEQUENCE:0\n"+
		"#EXT-X-PLAYLIST-TYPE:VOD\n#EXTINF:%.3f,\n/api/v1/stream/%s/subtitles/%s.vtt?warten=55&%s\n#EXT-X-ENDLIST\n",
		int(sek)+1, sek, f.ID, idx, r.URL.RawQuery)
	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(body))
}

// hlsSpurenZeilen baut die EXT-X-MEDIA-Zeilen für die Master-Liste: alle
// Text-Untertitel als wählbare Spuren, damit Apple TV und iPhone sie im
// Player anbieten.
func (s *Server) hlsSpurenZeilen(ctx context.Context, f *store.MediaFile, query string) string {
	if !s.st.SpurenGelesen(ctx, f.ID) {
		s.spurenNachmessen(ctx, f)
	}
	spuren, _ := s.st.Spuren(ctx, f.ID)
	var b strings.Builder
	for _, t := range spuren {
		if t.Kind != "subtitle" || t.Bitmap {
			continue
		}
		name := spurName(t)
		lang := t.Language
		if lang == "" || lang == "und" {
			lang = "und"
		}
		fmt.Fprintf(&b, "#EXT-X-MEDIA:TYPE=SUBTITLES,GROUP-ID=\"subs\",NAME=\"%s\",LANGUAGE=\"%s\",DEFAULT=NO,AUTOSELECT=YES,FORCED=%s,URI=\"/api/v1/stream/%s/untertitel/%d.m3u8?%s\"\n",
			strings.ReplaceAll(name, `"`, "'"), lang, jaNein(t.Forced), f.ID, t.Index, query)
	}
	return b.String()
}

func jaNein(b bool) string {
	if b {
		return "YES"
	}
	return "NO"
}

// spurName ist der Anzeigename einer Spur: Sprache, dann was dransteht.
func spurName(t store.Track) string {
	sprachen := map[string]string{"de": "Deutsch", "en": "Englisch", "fr": "Französisch", "it": "Italienisch",
		"es": "Spanisch", "ja": "Japanisch", "ko": "Koreanisch", "pt": "Portugiesisch", "nl": "Niederländisch",
		"ru": "Russisch", "pl": "Polnisch", "tr": "Türkisch", "sv": "Schwedisch", "da": "Dänisch", "zh": "Chinesisch"}
	name := sprachen[t.Language]
	if name == "" {
		if t.Language != "" && t.Language != "und" {
			name = strings.ToUpper(t.Language)
		} else {
			name = "Unbekannt"
		}
	}
	var zusatz []string
	if t.Title != "" && !strings.EqualFold(t.Title, name) {
		zusatz = append(zusatz, t.Title)
	}
	if t.Forced {
		zusatz = append(zusatz, "erzwungen")
	}
	if t.HearingImpaired {
		zusatz = append(zusatz, "für Hörgeschädigte")
	}
	if t.Kind == "audio" && t.Channels > 2 {
		zusatz = append(zusatz, fmt.Sprintf("%d.1", t.Channels-1))
	}
	if t.External {
		zusatz = append(zusatz, "Datei")
	}
	if len(zusatz) > 0 {
		name += " (" + strings.Join(zusatz, ", ") + ")"
	}
	return name
}

// ---------------------------------------------------------------- Vorspann und Abspann

// vorspannUndAbspann sagt dem Player, wo bei einer Folge der Vorspann
// liegt und wo der Abspann beginnt — aus den Kapiteln der Datei, oder
// der Vorspann aus der Markierung an der Serie.
func (s *Server) vorspannUndAbspann(ctx context.Context, it *store.Item, files []store.MediaFile) (vor *store.Vorspann, abspannAbMS int64) {
	for _, f := range files {
		if f.Missing {
			continue
		}
		for _, c := range f.Kapitel {
			switch c.Kind {
			case "intro":
				if vor == nil {
					vor = &store.Vorspann{VonMS: c.StartMS, BisMS: c.EndMS, Quelle: "kapitel"}
				}
			case "credits":
				if abspannAbMS == 0 || c.StartMS < abspannAbMS {
					abspannAbMS = c.StartMS
				}
			}
		}
		break
	}
	if vor == nil && it.Kind == "episode" {
		// Die Serie steht zwei Stufen über der Folge (Folge → Staffel → Serie).
		if staffel, err := s.st.Item(ctx, it.ParentID); err == nil && staffel.ParentID != "" {
			vor, _ = s.st.VorspannDerSerie(ctx, staffel.ParentID)
		}
	}
	return vor, abspannAbMS
}

// adminVorspannSetzen markiert den Vorspann einer Serie — wer die
// Verwaltung darf, darf das; es gilt für alle, die die Serie schauen.
func (s *Server) adminVorspannSetzen(w http.ResponseWriter, r *http.Request) {
	it, err := s.visibleItem(r, r.PathValue("id")) // nur in Mediatheken, die man verwalten darf
	if err != nil {
		failErr(w, err)
		return
	}
	if it.Kind != "series" {
		fail(w, http.StatusBadRequest, "der vorspann wird an der serie markiert")
		return
	}
	var in struct {
		VonMS int64 `json:"vonMs"`
		BisMS int64 `json:"bisMs"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if err := s.st.VorspannSetzen(r.Context(), it.ID, in.VonMS, in.BisMS); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) adminVorspannLoeschen(w http.ResponseWriter, r *http.Request) {
	if err := s.st.VorspannLoeschen(r.Context(), r.PathValue("id")); err != nil {
		failErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
