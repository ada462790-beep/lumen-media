// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

// Poster und Hintergründe in der Grösse, in der sie gezeigt werden.
//
// Lokale Bilder aus der Sammlung („poster.jpg" neben dem Film, aus Emby
// oder Kodi) sind oft 2000 × 3000 Punkte und über ein Megabyte gross. Die
// Startseite zeigt sie 170 bis 250 Punkte breit — und lud früher vierzig
// davon in voller Grösse: mehrere Megabyte, jedes Bild bis zu anderthalb
// Sekunden.
//
// Jetzt sagt die Oberfläche mit ?w=…, wie breit sie das Bild braucht
// (srcset, nach Kachel und Bildschirmdichte). Lumen rechnet es einmal
// herunter, legt es neben die anderen Bilder (bilder-klein/) und behält
// die zuletzt gelieferten im Speicher (höchstens bildSpeicherHoechstens).
// Ohne ?w gibt es eine vernünftige Obergrenze (Poster 780, Hintergründe
// 1920 Punkte breit) — die Apps fragen ohne und bekommen so ebenfalls
// keine Riesenbilder mehr. Logos bleiben, wie sie sind (durchsichtig).

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"lumen/internal/ablage"
	"lumen/internal/store"
)

// bildBreiten: die Breiten, die es gibt. Eine andere Angabe wird auf die
// nächstgrössere gerundet — sonst liesse sich mit w=1, w=2 … beliebig
// viel Rechenzeit und Platz verbrauchen.
var bildBreiten = []int{185, 342, 500, 780, 1280, 1920}

const (
	// bildSpeicherHoechstens: so viel Arbeitsspeicher für Bilder.
	bildSpeicherHoechstens = 64 << 20
	// bildSpeicherEinzeln: grössere Bilder kommen nicht in den Speicher.
	bildSpeicherEinzeln = 2 << 20
	// bildQualitaet: JPEG-Qualität der verkleinerten Bilder.
	bildQualitaet = 84
	// kleinOrdner: wo die verkleinerten Bilder liegen (im Datenordner).
	kleinOrdner = "bilder-klein"
)

// bildZielBreite: auf welche Breite ein Bild dieser Art höchstens kommt.
// 0 = so, wie es ist.
func bildZielBreite(kind string, w int) int {
	if kind == "logo" {
		return 0
	}
	if w <= 0 {
		switch {
		case kind == "poster" || kind == "thumb" || strings.HasPrefix(kind, "cast"):
			return 780
		case kind == "backdrop" || kind == "landscape" || kind == "banner":
			return 1920
		}
		return 0
	}
	for _, b := range bildBreiten {
		if b >= w {
			return b
		}
	}
	return bildBreiten[len(bildBreiten)-1]
}

// ---------------------------------------------------------------- Speicher

type bildEintrag struct {
	inhalt  []byte
	art     string // Content-Type
	zeit    time.Time
	etag    string
	benutzt time.Time
}

type bildSpeicher struct {
	mu      sync.Mutex
	m       map[string]*bildEintrag
	groesse int
	// wege: Anfrage (Konto, Titel, Art, Breite) → Eintrag in m, gültig,
	// solange der Stand der Datenbank gleich ist. So braucht ein Bild der
	// Startseite beim zweiten Mal weder die Datenbank noch die Platte.
	wege map[string]bildMerkWeg
}

type bildMerkWeg struct {
	stand      int64
	schluessel string
}

var bilderImSpeicher bildSpeicher

func (b *bildSpeicher) holen(k string) *bildEintrag {
	b.mu.Lock()
	defer b.mu.Unlock()
	e := b.m[k]
	if e != nil {
		e.benutzt = time.Now()
	}
	return e
}

func (b *bildSpeicher) ablegen(k string, e *bildEintrag) {
	if len(e.inhalt) > bildSpeicherEinzeln {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.m == nil {
		b.m = map[string]*bildEintrag{}
	}
	if alt := b.m[k]; alt != nil {
		b.groesse -= len(alt.inhalt)
	}
	e.benutzt = time.Now()
	b.m[k] = e
	b.groesse += len(e.inhalt)
	for b.groesse > bildSpeicherHoechstens && len(b.m) > 1 {
		var weg string
		var t time.Time
		for kk, x := range b.m {
			if weg == "" || x.benutzt.Before(t) {
				weg, t = kk, x.benutzt
			}
		}
		b.groesse -= len(b.m[weg].inhalt)
		delete(b.m, weg)
	}
}

func (b *bildSpeicher) weg(k string) (string, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	w, ok := b.wege[k]
	if !ok || w.stand != store.Stand() {
		return "", false
	}
	return w.schluessel, true
}

func (b *bildSpeicher) wegMerken(k string, stand int64, schluessel string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.wege == nil || len(b.wege) > 20000 {
		b.wege = map[string]bildMerkWeg{}
	}
	b.wege[k] = bildMerkWeg{stand: stand, schluessel: schluessel}
}

// bildWegSchluessel: wer fragt nach welchem Bild in welcher Breite.
func bildWegSchluessel(r *http.Request) string {
	konto := ""
	if a := accountOf(r); a != nil {
		konto = a.ID
	}
	return konto + "\x00" + r.PathValue("item") + "\x00" + r.PathValue("kind") + "\x00" + r.URL.Query().Get("w")
}

// bildAusSpeicher liefert ein Bild ohne Datenbank und Platte, wenn
// dieselbe Anfrage schon einmal beantwortet wurde und sich seither nichts
// geändert hat. true = erledigt.
func (s *Server) bildAusSpeicher(w http.ResponseWriter, r *http.Request) bool {
	k, ok := bilderImSpeicher.weg(bildWegSchluessel(r))
	if !ok {
		return false
	}
	e := bilderImSpeicher.holen(k)
	if e == nil {
		return false
	}
	bildEintragSenden(w, r, e)
	return true
}

func bildEintragSenden(w http.ResponseWriter, r *http.Request, e *bildEintrag) {
	w.Header().Set("ETag", e.etag)
	w.Header().Set("Cache-Control", bildHaltbarkeit(r))
	w.Header().Set("Content-Type", e.art)
	http.ServeContent(w, r, "", e.zeit, bytes.NewReader(e.inhalt))
}

// ---------------------------------------------------------------- Senden

// bildSenden liefert die Datei name aus ordner — verkleinert, wenn sie
// breiter ist als nötig. stand: der Stand der Datenbank, als die Anfrage
// begann (für bildAusSpeicher).
func (s *Server) bildSenden(w http.ResponseWriter, r *http.Request, ordner, name, kind string, stand int64) {
	path := filepath.Join(ordner, name)
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		fail(w, http.StatusNotFound, "kein bild")
		return
	}
	ziel := bildZielBreite(kind, qInt(r, "w", 0))
	endung := strings.TrimPrefix(strings.ToLower(filepath.Ext(name)), ".")
	if endung == "jpg" {
		endung = "jpeg" // „image/jpg" gibt es nicht
	}
	art := "image/" + endung
	kennung := name + "\x00" + strconv.FormatInt(info.Size(), 10) + "\x00" + strconv.FormatInt(info.ModTime().UnixNano(), 10)
	schluessel := kennung + "\x00" + strconv.Itoa(ziel)

	if e := bilderImSpeicher.holen(schluessel); e != nil {
		bilderImSpeicher.wegMerken(bildWegSchluessel(r), stand, schluessel)
		bildEintragSenden(w, r, e)
		return
	}
	inhalt, neueArt, err := s.bildInBreite(r.Context(), path, name, info, ziel)
	if err != nil {
		if ctx := r.Context(); ctx.Err() != nil {
			return
		}
		// Lässt es sich nicht verkleinern (unbekanntes Format, zu gross):
		// dann eben das Original, wie früher.
		inhalt, err = os.ReadFile(path)
		if err != nil {
			fail(w, http.StatusNotFound, "kein bild")
			return
		}
		neueArt = ""
	}
	if neueArt != "" {
		art = neueArt
	}
	h := sha256.Sum256([]byte(schluessel))
	e := &bildEintrag{inhalt: inhalt, art: art, zeit: info.ModTime(),
		etag: `"` + base64.RawURLEncoding.EncodeToString(h[:12]) + `"`}
	bilderImSpeicher.ablegen(schluessel, e)
	bilderImSpeicher.wegMerken(bildWegSchluessel(r), stand, schluessel)
	bildEintragSenden(w, r, e)
}

// bildInBreite gibt das Bild in höchstens ziel Punkten Breite. Ist es
// schon schmal genug (oder ziel 0), kommt das Original. art ist leer,
// wenn es das Original ist.
func (s *Server) bildInBreite(ctx context.Context, path, name string, info os.FileInfo, ziel int) ([]byte, string, error) {
	roh, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	if ziel <= 0 {
		return roh, "", nil
	}
	kopf, _, err := image.DecodeConfig(bytes.NewReader(roh))
	if err != nil {
		return roh, "", nil
	}
	if kopf.Width <= ziel {
		return roh, "", nil
	}
	// Schon einmal verkleinert? Die Datei trägt die Änderungszeit des
	// Originals: Ändert sich das Original, passt sie nicht mehr.
	klein := filepath.Join(s.dataDir, kleinOrdner, strconv.Itoa(ziel), strings.TrimSuffix(name, filepath.Ext(name))+".jpg")
	if ki, err := os.Stat(klein); err == nil && ki.ModTime().Equal(info.ModTime()) {
		if b, err := os.ReadFile(klein); err == nil {
			return b, "image/jpeg", nil
		}
	}
	var out bytes.Buffer
	err = bildDekodieren(ctx, roh, bildGrenzen{seite: 20000}, func(img image.Image) error {
		hoehe := int(int64(kopf.Height) * int64(ziel) / int64(kopf.Width))
		if hoehe < 1 {
			hoehe = 1
		}
		return jpeg.Encode(&out, verkleinern(img, ziel, hoehe), &jpeg.Options{Quality: bildQualitaet})
	})
	if err != nil {
		return nil, "", err
	}
	if err := os.MkdirAll(filepath.Dir(klein), 0o755); err == nil {
		if ablage.Schreiben(klein, bytes.NewReader(out.Bytes()), int64(out.Len())+1) == nil {
			_ = os.Chtimes(klein, info.ModTime(), info.ModTime())
		}
	}
	return out.Bytes(), "image/jpeg", nil
}

// kleineFassungenWeg löscht die verkleinerten Fassungen eines Bildes
// (bilder-klein/<Breite>/<Name>.jpg), wenn es nicht mehr gilt — sonst
// blieben sie nach jedem neuen Poster liegen.
func (s *Server) kleineFassungenWeg(name string) {
	if name == "" || strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") || s.dataDir == "" {
		return
	}
	stamm := strings.TrimSuffix(name, filepath.Ext(name)) + ".jpg"
	for _, b := range bildBreiten {
		_ = os.Remove(filepath.Join(s.dataDir, kleinOrdner, strconv.Itoa(b), stamm))
	}
}

// verwaisteKleineWeg löscht verkleinerte Bilder, deren Original es im
// Bilderordner nicht mehr gibt (Hausmeister nach dem Durchlauf).
func (s *Server) verwaisteKleineWeg() int {
	if s.meta == nil || s.dataDir == "" {
		return 0
	}
	originale, err := os.ReadDir(s.meta.ArtDir())
	if err != nil {
		return 0
	}
	da := map[string]bool{}
	for _, e := range originale {
		da[strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))] = true
	}
	n := 0
	breiten, _ := os.ReadDir(filepath.Join(s.dataDir, kleinOrdner))
	for _, b := range breiten {
		if !b.IsDir() {
			continue
		}
		ordner := filepath.Join(s.dataDir, kleinOrdner, b.Name())
		eintraege, _ := os.ReadDir(ordner)
		for _, e := range eintraege {
			if !e.Type().IsRegular() || da[strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))] {
				continue
			}
			if os.Remove(filepath.Join(ordner, e.Name())) == nil {
				n++
			}
		}
	}
	return n
}

// verkleinern rechnet src auf b × h herunter: Jeder Zielpunkt ist der
// Mittelwert seines Quellgebiets (mit anteiligen Rändern), damit nichts
// flimmert. Erst waagrecht, dann senkrecht — so bleibt es auch bei
// 3000 Punkten Höhe schnell.
func verkleinern(src image.Image, b, h int) *image.RGBA {
	var sb, sh int
	// Waagrecht zuerst: sh Zeilen, b Spalten, je vier Kanäle. Bei JPEG
	// (YCbCr) wird in YCbCr gemittelt und erst am Ende umgerechnet — die
	// Umrechnung ist linear, das Ergebnis dasselbe, aber viel billiger.
	var zw []float32
	ycc := false
	if s, ok := src.(*image.YCbCr); ok && s.Rect.Min == (image.Point{}) {
		ycc = true
		sb, sh = s.Rect.Dx(), s.Rect.Dy()
		zw = make([]float32, sh*b*4)
		gewichte := flaechen(sb, b)
		// Wie weit die Farbebenen zusammengefasst sind (4:2:0 usw.).
		cxTeiler, cyTeiler := 1, 1
		switch s.SubsampleRatio {
		case image.YCbCrSubsampleRatio422:
			cxTeiler = 2
		case image.YCbCrSubsampleRatio420:
			cxTeiler, cyTeiler = 2, 2
		case image.YCbCrSubsampleRatio440:
			cyTeiler = 2
		case image.YCbCrSubsampleRatio411:
			cxTeiler = 4
		case image.YCbCrSubsampleRatio410:
			cxTeiler, cyTeiler = 4, 2
		}
		for y := 0; y < sh; y++ {
			yz := s.Y[y*s.YStride:]
			cz := y / cyTeiler * s.CStride
			cbz, crz := s.Cb[cz:], s.Cr[cz:]
			o := y * b * 4
			for x := 0; x < b; x++ {
				var k0, k1, k2 float32
				for _, gw := range gewichte[x] {
					ci := gw.i / cxTeiler
					k0 += float32(yz[gw.i]) * gw.w
					k1 += float32(cbz[ci]) * gw.w
					k2 += float32(crz[ci]) * gw.w
				}
				zw[o], zw[o+1], zw[o+2], zw[o+3] = k0, k1, k2, 255
				o += 4
			}
		}
	} else {
		rgba := alsRGBA(src)
		sb, sh = rgba.Rect.Dx(), rgba.Rect.Dy()
		zw = make([]float32, sh*b*4)
		gewichte := flaechen(sb, b)
		for y := 0; y < sh; y++ {
			zeile := rgba.Pix[y*rgba.Stride:]
			o := y * b * 4
			for x := 0; x < b; x++ {
				var k0, k1, k2, k3 float32
				for _, gw := range gewichte[x] {
					p := zeile[gw.i*4 : gw.i*4+4]
					k0 += float32(p[0]) * gw.w
					k1 += float32(p[1]) * gw.w
					k2 += float32(p[2]) * gw.w
					k3 += float32(p[3]) * gw.w
				}
				zw[o], zw[o+1], zw[o+2], zw[o+3] = k0, k1, k2, k3
				o += 4
			}
		}
	}
	dst := image.NewRGBA(image.Rect(0, 0, b, h))
	gewichte := flaechen(sh, h)
	for y := 0; y < h; y++ {
		for x := 0; x < b; x++ {
			var k0, k1, k2, k3 float32
			for _, gw := range gewichte[y] {
				o := (gw.i*b + x) * 4
				k0 += zw[o] * gw.w
				k1 += zw[o+1] * gw.w
				k2 += zw[o+2] * gw.w
				k3 += zw[o+3] * gw.w
			}
			o := y*dst.Stride + x*4
			if ycc {
				r, g, bl := ycbcrNachRGB(runden(k0), runden(k1), runden(k2))
				dst.Pix[o], dst.Pix[o+1], dst.Pix[o+2], dst.Pix[o+3] = r, g, bl, 255
				continue
			}
			dst.Pix[o], dst.Pix[o+1], dst.Pix[o+2], dst.Pix[o+3] = runden(k0), runden(k1), runden(k2), runden(k3)
		}
	}
	return dst
}

type gewicht struct {
	i int
	w float32
}

// flaechen: für jeden der n Zielpunkte die Quellpunkte (von quelle) mit
// ihrem Anteil. Die Anteile eines Zielpunkts ergeben zusammen 1.
func flaechen(quelle, n int) [][]gewicht {
	out := make([][]gewicht, n)
	schritt := float64(quelle) / float64(n)
	for x := 0; x < n; x++ {
		von, bis := float64(x)*schritt, float64(x+1)*schritt
		var ws []gewicht
		for i := int(von); i < quelle && float64(i) < bis; i++ {
			a, e := float64(i), float64(i+1)
			if a < von {
				a = von
			}
			if e > bis {
				e = bis
			}
			if e > a {
				ws = append(ws, gewicht{i, float32((e - a) / schritt)})
			}
		}
		out[x] = ws
	}
	return out
}

func runden(v float32) uint8 {
	v += 0.5
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}

// alsRGBA bringt ein Bild in die eine Form, die verkleinern liest.
func alsRGBA(src image.Image) *image.RGBA {
	if r, ok := src.(*image.RGBA); ok && r.Rect.Min == (image.Point{}) {
		return r
	}
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			r, g, bl, a := src.At(b.Min.X+x, b.Min.Y+y).RGBA()
			o := y*dst.Stride + x*4
			dst.Pix[o], dst.Pix[o+1], dst.Pix[o+2], dst.Pix[o+3] = uint8(r>>8), uint8(g>>8), uint8(bl>>8), uint8(a>>8)
		}
	}
	return dst
}

func ycbcrNachRGB(y, cb, cr uint8) (uint8, uint8, uint8) {
	yy := int32(y) * 0x10101
	cb1 := int32(cb) - 128
	cr1 := int32(cr) - 128
	r := yy + 91881*cr1
	g := yy - 22554*cb1 - 46802*cr1
	b := yy + 116130*cb1
	k := func(v int32) uint8 {
		if uint32(v)&0xff000000 == 0 {
			return uint8(v >> 16)
		}
		if v < 0 {
			return 0
		}
		return 255
	}
	return k(r), k(g), k(b)
}
