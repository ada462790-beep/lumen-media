// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

// Eigene Bilder: Poster und Hintergründe von Hand, Bilder für Sammlungen.
//
// Ein hochgeladenes Bild wird nie so abgelegt, wie es kommt. Lumen liest
// es, prüft Art und Grösse, rechnet es auf ein vernünftiges Mass herunter
// und schreibt es als frisches JPEG. So landet keine Datei im
// Bilderordner, die sich nur als Bild ausgibt (etwa ein Skript mit
// GIF-Kopf), und kein Foto mit 40 Megapixeln, das jedes Handy beim
// Blättern ausbremst. Nebenbei verschwinden Metadaten wie der Ort, an
// dem ein Foto aufgenommen wurde.

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"lumen/internal/netz"
)

// bildHoechstens: grösser nimmt Lumen kein Bild an.
const bildHoechstens = 20 << 20

// bildClient holt Bilder von einer Adresse. Nur aus dem Internet — ein
// Bild aus dem Heimnetz lädt man hoch. Tests setzen einen eigenen ein.
var bildClient = netz.Client(netz.Regeln{Zeitlimit: 25 * time.Second})

// bildLesen nimmt ein Bild aus der Anfrage: roh im Body, als
// Formularfeld „bild" oder als JSON {"url": "…"}.
func bildLesen(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	ct := r.Header.Get("Content-Type")
	switch {
	case strings.HasPrefix(ct, "multipart/"):
		r.Body = http.MaxBytesReader(w, r.Body, bildHoechstens+1<<20)
		if err := r.ParseMultipartForm(8 << 20); err != nil {
			return nil, &apiFehler{status: http.StatusRequestEntityTooLarge, text: "das Bild ist zu gross (höchstens 20 MB)"}
		}
		defer func() { _ = r.MultipartForm.RemoveAll() }()
		f, _, err := r.FormFile("bild")
		if err != nil {
			return nil, &apiFehler{status: http.StatusBadRequest, text: "kein Bild im Feld „bild“"}
		}
		defer f.Close()
		return bildRoh(f)
	case strings.HasPrefix(ct, "application/json"):
		var in struct {
			URL string `json:"url"`
		}
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&in); err != nil || strings.TrimSpace(in.URL) == "" {
			return nil, &apiFehler{status: http.StatusBadRequest, text: "keine Adresse angegeben"}
		}
		return bildHolen(r.Context(), in.URL)
	default:
		return bildRoh(http.MaxBytesReader(w, r.Body, bildHoechstens+1))
	}
}

func bildRoh(rd io.Reader) ([]byte, error) {
	raw, err := netz.Lesen(rd, bildHoechstens)
	if err != nil || len(raw) == 0 {
		return nil, &apiFehler{status: http.StatusRequestEntityTooLarge, text: "das Bild liess sich nicht lesen (höchstens 20 MB)"}
	}
	return raw, nil
}

// bildHolen lädt ein Bild von einer Adresse im Internet.
func bildHolen(ctx context.Context, adresse string) ([]byte, error) {
	u, err := netz.AdressePruefen(adresse)
	if err != nil {
		return nil, &apiFehler{status: http.StatusBadRequest, text: err.Error()}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, &apiFehler{status: http.StatusBadRequest, text: "keine gültige Adresse"}
	}
	req.Header.Set("Accept", "image/*")
	req.Header.Set("User-Agent", "Lumen")
	resp, err := bildClient.Do(req)
	if err != nil {
		if errors.Is(err, netz.ErrAdresse) {
			return nil, &apiFehler{status: http.StatusBadRequest, text: "Bilder aus dem Heimnetz bitte hochladen statt verlinken"}
		}
		return nil, &apiFehler{status: http.StatusBadGateway, text: "das Bild liess sich nicht laden"}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &apiFehler{status: http.StatusBadGateway, text: "die Adresse antwortete mit {status}", werte: []any{"status", resp.StatusCode}}
	}
	if resp.ContentLength > bildHoechstens {
		return nil, &apiFehler{status: http.StatusRequestEntityTooLarge, text: "das Bild ist zu gross (höchstens 20 MB)"}
	}
	raw, err := netz.Lesen(resp.Body, bildHoechstens)
	if err != nil {
		return nil, &apiFehler{status: http.StatusRequestEntityTooLarge, text: "das Bild ist zu gross (höchstens 20 MB)"}
	}
	return raw, nil
}

// bildNeuKodieren prüft und rechnet um. maxB × maxH ist der Rahmen, in
// den das Bild passen muss; kleiner wird nichts aufgeblasen. Dekodiert
// wird über bildDekodieren (Kopf zuerst, höchstens 25 MP, höchstens zwei
// gleichzeitig).
func bildNeuKodieren(ctx context.Context, raw []byte, maxB, maxH int) ([]byte, error) {
	var out bytes.Buffer
	err := bildDekodieren(ctx, raw, bildGrenzen{seite: 12000, klein: 32}, func(img image.Image) error {
		return jpeg.Encode(&out, einpassen(img, maxB, maxH), &jpeg.Options{Quality: 88})
	})
	if err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// einpassen rechnet das Bild in den Rahmen herunter, Seitenverhältnis
// bleibt. Mittelwert über das Quellgebiet, damit nichts flimmert — ohne
// Fremdbibliothek, wie quadratVerkleinern.
func einpassen(src image.Image, maxB, maxH int) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= maxB && h <= maxH {
		// Trotzdem neu zeichnen: Ein Bild mit Palette oder Transparenz
		// wird so zu einem gewöhnlichen, deckenden Bild.
		dst := image.NewRGBA(image.Rect(0, 0, w, h))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				dst.Set(x, y, deckend(src.At(b.Min.X+x, b.Min.Y+y)))
			}
		}
		return dst
	}
	f := min(float64(maxB)/float64(w), float64(maxH)/float64(h))
	nw, nh := max(1, int(float64(w)*f)), max(1, int(float64(h)*f))
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	for y := 0; y < nh; y++ {
		sy0, sy1 := b.Min.Y+y*h/nh, b.Min.Y+(y+1)*h/nh
		if sy1 <= sy0 {
			sy1 = sy0 + 1
		}
		for x := 0; x < nw; x++ {
			sx0, sx1 := b.Min.X+x*w/nw, b.Min.X+(x+1)*w/nw
			if sx1 <= sx0 {
				sx1 = sx0 + 1
			}
			var r, g, bl, n uint64
			for sy := sy0; sy < sy1; sy++ {
				for sx := sx0; sx < sx1; sx++ {
					c := deckend(src.At(sx, sy))
					r += uint64(c.R)
					g += uint64(c.G)
					bl += uint64(c.B)
					n++
				}
			}
			dst.Set(x, y, color.RGBA{uint8(r / n), uint8(g / n), uint8(bl / n), 255})
		}
	}
	return dst
}

// deckend legt Durchsichtiges auf Schwarz — JPEG kennt keine Transparenz.
// RGBA() liefert vormultiplizierte Werte; ohne Deckkraft ergibt das
// genau die Farbe auf schwarzem Grund.
func deckend(c color.Color) color.RGBA {
	r, g, b, _ := c.RGBA()
	return color.RGBA{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), 255}
}

// bildAblegen schreibt ein fertiges Bild in den Bilderordner, unter einem
// neuen Namen — ein Browser, der das alte noch zwischengespeichert hat,
// soll nicht das alte zeigen, sobald sich die Adresse ändert.
func bildAblegen(ordner, praefix string, inhalt []byte) (string, error) {
	var zufall [6]byte
	_, _ = rand.Read(zufall[:])
	name := fmt.Sprintf("%s-hand-%s.jpg", filepath.Base(praefix), hex.EncodeToString(zufall[:]))
	ziel := filepath.Join(ordner, name)
	tmp := ziel + ".teil"
	if err := os.WriteFile(tmp, inhalt, 0o644); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, ziel); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	return name, nil
}

// eigenesBildWeg räumt ein von Hand gesetztes Bild weg. Nur solche: Die
// Bilder von TMDb bleiben liegen, damit sie nach dem Aufheben der Sperre
// ohne neuen Download wieder greifen.
func eigenesBildWeg(ordner, name string) {
	if name == "" || !strings.Contains(name, "-hand-") || strings.ContainsAny(name, `/\`) {
		return
	}
	_ = os.Remove(filepath.Join(ordner, name))
}
