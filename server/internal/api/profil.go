// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

// Profilbilder und Neuigkeiten.
//
// Ein Profilbild ist entweder eine Farbe („av-a"), eine eingebaute Figur
// („fig-fuchs") oder ein eigenes Bild („bild"), das unter
// <data>/avatare/<konto>.jpg liegt. Das Bild wird beim Hochladen auf
// 256 × 256 beschnitten und verkleinert — mehr braucht kein Gerät, und
// so landet kein 12-MB-Foto in der Profilauswahl.
//
// Neuigkeiten kommen aus dem eigenen Stand (store.Neuigkeiten); gelesen
// heisst: der Zeitpunkt des letzten Öffnens steht als Pref am Konto.

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"lumen/internal/stream"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"lumen/internal/store"
)

const avatarKante = 256

// avatarHoechstens: so gross darf die hochgeladene Datei sein.
const avatarHoechstens = 16 << 20

// avatarSeite: so viele Punkte je Seite darf das Bild haben.
const avatarSeite = 4096

// Figuren: die eingebauten Profilbilder. Die Zeichnung dazu liegt in
// den Apps; der Server kennt nur die Namen, damit niemand Beliebiges
// als „Figur" einträgt.
var Figuren = []string{"fuchs", "eule", "katze", "roboter", "rakete", "stern", "wal", "blume", "pilz", "geist", "drache", "pinguin"}

func figurBekannt(name string) bool {
	for _, f := range Figuren {
		if "fig-"+f == name {
			return true
		}
	}
	return false
}

// avatarPfad: wo das eigene Bild eines Kontos liegt.
func (s *Server) avatarPfad(accountID string) string {
	return filepath.Join(s.dataDir, "avatare", filepath.Base(accountID)+".jpg")
}

// meAvatarSetzen nimmt ein Bild entgegen (JPEG, PNG, GIF; roh im Body
// oder als Formularfeld „bild") und macht daraus das Profilbild.
func (s *Server) meAvatarSetzen(w http.ResponseWriter, r *http.Request) {
	acc := accountOf(r)
	// Die Obergrenze gilt für den ganzen Rumpf, bevor irgendetwas gelesen
	// wird: ParseMultipartForm legt sonst Teile über 16 MB in temporären
	// Dateien ab — beliebig viele, beliebig gross.
	r.Body = http.MaxBytesReader(w, r.Body, avatarHoechstens+1<<20)
	var quelle io.Reader = r.Body
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
		if err := r.ParseMultipartForm(avatarHoechstens); err != nil {
			fail(w, http.StatusRequestEntityTooLarge, "das bild ist zu gross (höchstens 16 MB)")
			return
		}
		defer func() { _ = r.MultipartForm.RemoveAll() }()
		f, _, err := r.FormFile("bild")
		if err != nil {
			fail(w, http.StatusBadRequest, "kein bild im feld „bild“")
			return
		}
		defer f.Close()
		quelle = f
	}
	raw, err := io.ReadAll(io.LimitReader(quelle, avatarHoechstens+1))
	if err != nil || len(raw) == 0 || len(raw) > avatarHoechstens {
		fail(w, http.StatusBadRequest, "das bild liess sich nicht lesen (höchstens 16 MB)")
		return
	}
	// Ein Profilbild wird 256 × 256 gross; mehr als 4096 Punkte je Seite
	// braucht kein Foto dafür. Kopf zuerst, dann dekodieren — beides in
	// bildDekodieren, mit dem gemeinsamen Riegel für alle Bildwege.
	var out bytes.Buffer
	err = bildDekodieren(r.Context(), raw, bildGrenzen{seite: avatarSeite, punkte: avatarSeite * avatarSeite},
		func(img image.Image) error {
			return jpeg.Encode(&out, quadratVerkleinern(img, avatarKante), &jpeg.Options{Quality: 86})
		})
	if err != nil {
		failErr(w, err)
		return
	}
	pfad := s.avatarPfad(acc.ID)
	_ = os.MkdirAll(filepath.Dir(pfad), 0o755)
	if err := os.WriteFile(pfad, out.Bytes(), 0o644); err != nil {
		failErr(w, err)
		return
	}
	if err := s.st.RenameAccount(r.Context(), acc.ID, acc.Name, "bild"); err != nil {
		failErr(w, err)
		return
	}
	s.me(w, s.mitFrischemKonto(r))
}

// mitFrischemKonto lädt das Konto neu in die Anfrage — nach einer
// Änderung zeigt die Antwort sonst noch den alten Stand.
func (s *Server) mitFrischemKonto(r *http.Request) *http.Request {
	acc := accountOf(r)
	if neu, err := s.st.Account(r.Context(), acc.ID); err == nil {
		return r.WithContext(context.WithValue(r.Context(), ctxAccount, neu))
	}
	return r
}

// avatarBild liefert das eigene Bild eines Kontos. Ohne Anmeldung: die
// Profilauswahl zeigt es, bevor jemand angemeldet ist — es ist ohnehin
// das, was jeder im Haushalt auf der Startseite sieht.
func (s *Server) avatarBild(w http.ResponseWriter, r *http.Request) {
	pfad := s.avatarPfad(r.PathValue("id"))
	st, err := os.Stat(pfad)
	if err != nil {
		fail(w, http.StatusNotFound, "kein bild")
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "private, max-age=300")
	w.Header().Set("ETag", `"`+strconv.FormatInt(st.ModTime().Unix(), 10)+`"`)
	http.ServeFile(w, r, pfad)
}

// quadratVerkleinern schneidet die Mitte quadratisch aus und rechnet auf
// kante × kante herunter — mit Mittelwert über das Quellgebiet, damit
// nichts flimmert. Ohne Fremdbibliothek: Lumen hat keine für Bilder,
// und für ein Profilbild braucht es keine.
func quadratVerkleinern(src image.Image, kante int) *image.RGBA {
	b := src.Bounds()
	seite := b.Dx()
	if b.Dy() < seite {
		seite = b.Dy()
	}
	x0 := b.Min.X + (b.Dx()-seite)/2
	y0 := b.Min.Y + (b.Dy()-seite)/2
	dst := image.NewRGBA(image.Rect(0, 0, kante, kante))
	if seite <= 0 {
		return dst
	}
	for y := 0; y < kante; y++ {
		sy0 := y0 + y*seite/kante
		sy1 := y0 + (y+1)*seite/kante
		if sy1 <= sy0 {
			sy1 = sy0 + 1
		}
		for x := 0; x < kante; x++ {
			sx0 := x0 + x*seite/kante
			sx1 := x0 + (x+1)*seite/kante
			if sx1 <= sx0 {
				sx1 = sx0 + 1
			}
			var r, g, bl, n uint64
			for sy := sy0; sy < sy1; sy++ {
				for sx := sx0; sx < sx1; sx++ {
					cr, cg, cb, _ := src.At(sx, sy).RGBA()
					r += uint64(cr >> 8)
					g += uint64(cg >> 8)
					bl += uint64(cb >> 8)
					n++
				}
			}
			dst.Set(x, y, color.RGBA{uint8(r / n), uint8(g / n), uint8(bl / n), 255})
		}
	}
	return dst
}

// ---------------------------------------------------------------- Neuigkeiten

func (s *Server) neuigkeiten(w http.ResponseWriter, r *http.Request) {
	acc := accountOf(r)
	q, err := s.scope(r)
	if err != nil {
		failErr(w, err)
		return
	}
	liste, err := s.st.Neuigkeiten(r.Context(), q, acc.ID)
	if err != nil {
		failErr(w, err)
		return
	}
	if acc.Besitzerrechte() {
		liste = append(s.serverMeldungen(r.Context()), liste...)
	}
	liste = append(s.eigeneMeldungen(r.Context(), acc.ID), liste...)
	// In die Sprache des Profils (siehe store.Vorlage).
	for i := range liste {
		v := liste[i].Vorlage
		if v.Titel != "" {
			liste[i].Titel = tr(w, v.Titel, v.Werte...)
		}
		if v.Text != "" {
			werte := v.Werte
			if liste[i].Art == "server" {
				// Bei Meldungen des Servers sind auch die Werte Wörter
				// von Lumen („Datenbank"), keine Titel aus der Mediathek.
				werte = append([]any(nil), werte...)
				for j := 1; j < len(werte); j += 2 {
					if s, ok := werte[j].(string); ok {
						werte[j] = tr(w, s)
					}
				}
			}
			liste[i].Text = tr(w, v.Text, werte...)
		}
	}
	gelesen := s.neuigkeitenGelesen(r, acc.ID)
	ungelesen := 0
	for _, n := range liste {
		if n.At.After(gelesen) {
			ungelesen++
		}
	}
	ok(w, map[string]any{"eintraege": liste, "ungelesen": ungelesen, "gelesenBis": gelesen})
}

func (s *Server) neuigkeitenGelesen(r *http.Request, accountID string) time.Time {
	prefs, _ := s.st.Prefs(r.Context(), accountID)
	if v, err := strconv.ParseInt(prefs["newsSeen"], 10, 64); err == nil {
		return time.Unix(v, 0)
	}
	return time.Time{}
}

// neuigkeitenGesehen: alles bis jetzt gilt als gelesen.
func (s *Server) neuigkeitenGesehen(w http.ResponseWriter, r *http.Request) {
	if err := s.st.SetPref(r.Context(), accountOf(r).ID, "newsSeen", strconv.FormatInt(time.Now().Unix(), 10)); err != nil {
		failErr(w, err)
		return
	}
	// Der Hinweis auf den zurückgesetzten PIN ist damit gelesen und geht
	// aus der Liste — er soll einmal erscheinen, nicht für immer.
	_ = s.st.PrefLoeschen(r.Context(), accountOf(r).ID, store.PrefPinZurueckgesetzt)
	w.WriteHeader(http.StatusNoContent)
}

// ungeleseneNeuigkeiten zählt für /me — damit die Seitenleiste den Punkt
// zeigen kann, ohne die ganze Liste zu holen.
func (s *Server) ungeleseneNeuigkeiten(r *http.Request, acc *store.Account) int {
	q, err := s.scope(r)
	if err != nil {
		return 0
	}
	liste, err := s.st.Neuigkeiten(r.Context(), q, acc.ID)
	if err != nil {
		return 0
	}
	liste = append(s.eigeneMeldungen(r.Context(), acc.ID), liste...)
	gelesen := s.neuigkeitenGelesen(r, acc.ID)
	n := 0
	for _, x := range liste {
		if x.At.After(gelesen) {
			n++
		}
	}
	return n
}

// eigeneMeldungen sind Neuigkeiten über das eigene Profil — bisher nur
// eine: Die Verwaltung hat den PIN zurückgesetzt und dabei den Verlauf
// gelöscht (store/vergessen.go). Als Neuigkeit erreicht das jede App,
// auch die, die den Hinweis beim Anmelden nicht kennen.
func (s *Server) eigeneMeldungen(ctx context.Context, accountID string) []store.Neuigkeit {
	wann, da := s.pinHinweis(ctx, accountID)
	if !da {
		return nil
	}
	return []store.Neuigkeit{{Art: "server", Titel: pinHinweisTitelText, Text: pinHinweisText, At: wann,
		Vorlage: store.Vorlage{Titel: pinHinweisTitelText, Text: pinHinweisText}}}
}

// serverMeldungen sind Neuigkeiten nur für den Besitzer — und nur über den
// Server selbst: Platte fast voll, Sicherung gescheitert. Nie etwas über
// Wiedergaben oder Personen.
func (s *Server) serverMeldungen(ctx context.Context) []store.Neuigkeit {
	var out []store.Neuigkeit
	heute := time.Now().Truncate(24 * time.Hour)
	if s.updateTokenUngueltig() {
		out = append(out, store.Neuigkeit{Art: "server", Titel: "Updates kommen nicht durch",
			Text: "GitHub nimmt das Token nicht mehr an — unter Verwaltung › Updates ein neues eintragen", At: heute,
			Vorlage: store.Vorlage{Titel: "Updates kommen nicht durch",
				Text: "GitHub nimmt das Token nicht mehr an — unter Verwaltung › Updates ein neues eintragen"}})
	}
	if f := s.st.Setting(ctx, "sicherung.fehler", ""); f != "" {
		out = append(out, store.Neuigkeit{Art: "server", Titel: "Sicherung gescheitert",
			Text: f, At: heute, Vorlage: store.Vorlage{Titel: "Sicherung gescheitert"}})
	}
	for _, o := range []struct{ name, pfad string }{
		{"Datenbank", s.dataDir}, {"Umwandlung", orElse(s.umwandlungDir, s.dataDir)},
	} {
		if frei := stream.FreierPlatz(o.pfad); frei > 0 && frei < 5<<30 {
			out = append(out, store.Neuigkeit{Art: "server", Titel: "Platz wird knapp",
				Text: fmt.Sprintf("Nur noch %.1f GB frei (%s)", float64(frei)/(1<<30), o.name), At: heute,
				Vorlage: store.Vorlage{Titel: "Platz wird knapp", Text: "Nur noch {gb} GB frei ({ort})",
					Werte: []any{"gb", fmt.Sprintf("%.1f", float64(frei)/(1<<30)), "ort", o.name}}})
			break
		}
	}
	return out
}
