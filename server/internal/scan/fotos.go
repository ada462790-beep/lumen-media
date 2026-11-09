// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package scan

// Fotobibliotheken einlesen: Fotos und kurze Videos, Ordner als Alben.

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"lumen/internal/store"
)

// BildExt und FotoVideoExt: was in einer Fotobibliothek zählt.
var BildExt = map[string]string{
	".jpg": "jpeg", ".jpeg": "jpeg", ".png": "png", ".heic": "heic", ".heif": "heic",
	".webp": "webp", ".gif": "gif",
}
var FotoVideoExt = map[string]string{".mp4": "mp4", ".mov": "mov", ".m4v": "mp4"}

// VideoInfo: was ffprobe über ein Handyvideo sagt.
type VideoInfo struct {
	Aufnahme    time.Time
	DauerMS     int64
	Breite      int
	Hoehe       int
	Codec       string
	Kamera      string
	Breitengrad *float64
	Laengengrad *float64
}

// FotoProber misst Videos aus und, wo der eigene EXIF-Leser die Masse
// nicht findet, auch Bilder.
type FotoProber interface {
	VideoInfo(ctx context.Context, path string) (*VideoInfo, error)
}

// fotoArt: „bild", „video" oder "" (gehört nicht in die Fotobibliothek).
func fotoArt(path string) (art, format string) {
	ext := strings.ToLower(filepath.Ext(path))
	if f, ok := BildExt[ext]; ok {
		return "bild", f
	}
	if f, ok := FotoVideoExt[ext]; ok {
		return "video", f
	}
	return "", ""
}

// reDatumImNamen: „IMG_20240713_180211", „PXL_20240713_180211123",
// „2024-07-13 18.02.11", „Screenshot 2024-07-13 at 18.02.11".
var reDatumImNamen = regexp.MustCompile(`(?:^|\D)((?:19|20)\d{2})-?(\d{2})-?(\d{2})(?:[ _T.-]+(?:at )?(\d{2})[.:-]?(\d{2})[.:-]?(\d{2}))?`)

// DatumAusNamen liest ein Aufnahmedatum aus dem Dateinamen, wie es
// Handys und Bildschirmfotos hineinschreiben.
func DatumAusNamen(name string) (time.Time, bool) {
	m := reDatumImNamen.FindStringSubmatch(name)
	if m == nil {
		return time.Time{}, false
	}
	n := func(s string) int { v, _ := strconv.Atoi(s); return v }
	j, mo, t := n(m[1]), n(m[2]), n(m[3])
	h, mi, s := n(m[4]), n(m[5]), n(m[6])
	if mo < 1 || mo > 12 || t < 1 || t > 31 || h > 23 || mi > 59 || s > 59 {
		return time.Time{}, false
	}
	z := time.Date(j, time.Month(mo), t, h, mi, s, 0, time.UTC)
	if z.Day() != t || z.After(time.Now().Add(48*time.Hour)) {
		return time.Time{}, false
	}
	return z, true
}

// scanFotos liest eine Fotobibliothek ein.
func (s *Scanner) scanFotos(ctx context.Context, lib store.Library, res *Result) error {
	fp, _ := s.probe.(FotoProber)
	bekannt, err := s.st.FotoDateien(ctx, lib.ID)
	if err != nil {
		return err
	}
	gesamt := 0
	_ = filepath.WalkDir(lib.Path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != lib.Path && mediathekOrdnerUeberspringen(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if a, _ := fotoArt(p); a != "" && !mediathekDateiUeberspringen(d.Name()) {
			gesamt++
		}
		return nil
	})
	s.melden(lib.ID, func(f *Fortschritt) { f.Gesamt = gesamt })

	alben := map[string]string{} // Ordner → Album
	gesehen := map[string]bool{}
	walkErr := filepath.WalkDir(lib.Path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			s.st.Log(ctx, "warn", "scan", fmt.Sprintf("übersprungen: %s (%v)", p, err))
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() {
			if p != lib.Path && mediathekOrdnerUeberspringen(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		art, format := fotoArt(p)
		if art == "" || mediathekDateiUeberspringen(d.Name()) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		gesehen[p] = true
		s.melden(lib.ID, func(f *Fortschritt) { f.Gefunden++; f.Datei = d.Name() })
		defer s.melden(lib.ID, func(f *Fortschritt) { f.Fertig++ })

		if alt, ok := bekannt[p]; ok && alt.Groesse == info.Size() && alt.MTime == info.ModTime().Unix() {
			if alt.Fehlt {
				_ = s.st.FotoWiederDa(ctx, alt.ID)
			}
			return nil
		}
		if s.Bremse != nil && s.Bremse() {
			select {
			case <-time.After(200 * time.Millisecond):
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		// Das Album: der Ordner, relativ zur Bibliothek. Fotos direkt in
		// der Wurzel gehören in kein Album, nur in die Mediathek.
		dir := filepath.Dir(p)
		albumID, ok := alben[dir]
		if !ok && dir != lib.Path {
			rel, _ := filepath.Rel(lib.Path, dir)
			rel = filepath.ToSlash(rel)
			albumID, err = s.st.FotoAlbumSichern(ctx, lib.ID, rel, filepath.Base(dir))
			if err != nil {
				return nil
			}
			alben[dir] = albumID
		}

		f := FotoAus(ctx, fp, p, art, format, info)
		f.LibraryID, f.AlbumID = lib.ID, albumID
		neu, err := s.st.FotoSpeichern(ctx, f)
		if err != nil {
			s.st.Log(ctx, "error", "scan", fmt.Sprintf("%s: %v", d.Name(), err))
			return nil
		}
		if neu {
			res.Added++
		} else {
			res.Updated++
		}
		return nil
	})
	if walkErr != nil && ctx.Err() == nil {
		return walkErr
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if len(gesehen) == 0 {
		s.st.Log(ctx, "warn", "scan", fmt.Sprintf(
			"%s: kein einziges Foto gefunden — nichts als verschwunden vermerkt.", lib.Name))
		return nil
	}
	if n, err := s.st.FotoFehlende(ctx, lib.ID, gesehen); err == nil {
		res.Missing = n
	}
	return nil
}

// FotoAus liest, was sich aus einer Datei sagen lässt: Datum, Masse,
// Ausrichtung, Kamera, Ort. Was fehlt, bleibt leer — das Datum fällt auf
// den Dateinamen und zuletzt auf das Änderungsdatum zurück.
func FotoAus(ctx context.Context, fp FotoProber, p, art, format string, info fs.FileInfo) *store.Foto {
	f := &store.Foto{Pfad: p, Groesse: info.Size(), MTime: info.ModTime().Unix(), Art: art, Format: format,
		Ausrichtung: 1}
	if art == "bild" {
		if fh, err := os.Open(p); err == nil {
			if e, err := ExifLesen(fh, info.Size()); err == nil && e != nil {
				if !e.Aufnahme.IsZero() {
					f.Aufnahme, f.DatumQuelle = e.Aufnahme, "exif"
				}
				f.Kamera, f.Breitengrad, f.Laengengrad = e.Kamera, e.Breitengrad, e.Laengengrad
				f.Breite, f.Hoehe = e.Breite, e.Hoehe
				if format == "heic" {
					// HEIC dreht über den Container; die EXIF-Angabe ist
					// dort nur Beiwerk und würde doppelt drehen.
					if e.HeifDrehung == 90 || e.HeifDrehung == 270 {
						f.Breite, f.Hoehe = f.Hoehe, f.Breite
					}
				} else if e.Ausrichtung >= 1 && e.Ausrichtung <= 8 {
					f.Ausrichtung = e.Ausrichtung
					if e.Ausrichtung >= 5 {
						f.Breite, f.Hoehe = f.Hoehe, f.Breite
					}
				}
			}
			fh.Close()
		}
	} else if fp != nil {
		if v, err := fp.VideoInfo(ctx, p); err == nil && v != nil {
			if !v.Aufnahme.IsZero() {
				f.Aufnahme, f.DatumQuelle = v.Aufnahme, "video"
			}
			f.DauerMS, f.Breite, f.Hoehe, f.Videocodec = v.DauerMS, v.Breite, v.Hoehe, v.Codec
			f.Kamera, f.Breitengrad, f.Laengengrad = v.Kamera, v.Breitengrad, v.Laengengrad
		}
	}
	if f.Aufnahme.IsZero() {
		if t, ok := DatumAusNamen(filepath.Base(p)); ok {
			f.Aufnahme, f.DatumQuelle = t, "name"
		} else {
			// Das Änderungsdatum als Uhrzeit des Servers gelesen und so
			// aufgeschrieben, wie EXIF es täte: ohne Zone.
			m := info.ModTime().Local()
			f.Aufnahme = time.Date(m.Year(), m.Month(), m.Day(), m.Hour(), m.Minute(), m.Second(), 0, time.UTC)
			f.DatumQuelle = "datei"
		}
	}
	return f
}
