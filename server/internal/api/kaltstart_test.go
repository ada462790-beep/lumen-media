// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lumen/internal/store"
)

// startseitenTitel: die Titel der Reihe „Filme" auf der Startseite.
func (h *harness) startseitenTitel() (titel []string, ausSpeicher bool) {
	h.t.Helper()
	resp, raw := h.do("GET", "/api/v1/home", "", h.tok)
	if resp.StatusCode != 200 {
		h.t.Fatalf("home: %d %s", resp.StatusCode, raw)
	}
	var b struct {
		Rows []struct {
			Key   string
			Items []struct{ Title string }
		}
	}
	_ = json.Unmarshal(raw, &b)
	for _, r := range b.Rows {
		if r.Key == "movies" {
			for _, it := range r.Items {
				titel = append(titel, it.Title)
			}
		}
	}
	return titel, strings.HasPrefix(resp.Header.Get("Server-Timing"), "db;dur=0.0,")
}

// Schreibt nur die Arbeit im Hintergrund (ein Durchlauf, Metadaten),
// kommt die gemerkte Startseite sofort — und wird daneben neu gerechnet.
// Vorher leerte jedes solche Schreiben den Speicher, und wer abends
// öffnete, wartete auf die kalte Platte. Was jemand selbst tut, gilt
// weiterhin sofort (TestAntwortspeicherFolgtAenderungen).
func TestAntwortspeicherNachArbeitImHintergrund(t *testing.T) {
	alt := antwortNachrechnen
	antwortNachrechnen = 0
	t.Cleanup(func() { antwortNachrechnen = alt })
	h := newHarness(t)
	id, _ := h.film("Vorher", 1)
	if got, _ := h.startseitenTitel(); strings.Join(got, ",") != "Vorher" {
		t.Fatalf("Startseite: %v", got)
	}
	// Metadaten im Hintergrund: neuer Titel.
	it, err := h.st.Item(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	it.Title = "Nachher"
	hart := store.StandHart()
	if err := h.st.UpsertItem(store.Hintergrund(t.Context()), it); err != nil {
		t.Fatal(err)
	}
	if store.StandHart() != hart {
		t.Fatal("Schreiben im Hintergrund hat StandHart weitergerückt")
	}
	got, ausSpeicher := h.startseitenTitel()
	if strings.Join(got, ",") != "Vorher" || !ausSpeicher {
		t.Fatalf("nach Arbeit im Hintergrund: %v (aus dem Speicher: %v), erwartet sofort die gemerkte", got, ausSpeicher)
	}
	// Daneben neu gerechnet: Bald kommt der neue Titel, wieder aus dem
	// Speicher.
	bis := time.Now().Add(5 * time.Second)
	for time.Now().Before(bis) {
		if got, ausSpeicher := h.startseitenTitel(); strings.Join(got, ",") == "Nachher" && ausSpeicher {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	got, _ = h.startseitenTitel()
	t.Fatalf("die Startseite wurde nicht im Hintergrund neu gerechnet: %v", got)
}

// Die Reihe „Filmreihen" der Startseite (?limit=) zeigt nur Reihen mit
// mindestens zwei Filmen; auf der Seite Filmreihen bleiben Reihen aus
// Emby oder einer NFO schon ab einem Film.
func TestFilmreihenStartseiteErstAbZweiFilmen(t *testing.T) {
	h := newHarness(t)
	a, _ := h.film("Allein", 1)
	b, _ := h.film("Teil Eins", 1)
	c, _ := h.film("Teil Zwei", 1)
	if _, err := h.st.EmbyReiheAnlegen(t.Context(), "Einzelreihe", "", []string{a}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.st.EmbyReiheAnlegen(t.Context(), "Doppelreihe", "", []string{b, c}); err != nil {
		t.Fatal(err)
	}
	namen := func(weg string) string {
		t.Helper()
		resp, raw := h.do("GET", weg, "", h.tok)
		if resp.StatusCode != 200 {
			t.Fatalf("%s: %d %s", weg, resp.StatusCode, raw)
		}
		var rs []struct{ Name string }
		_ = json.Unmarshal(raw, &rs)
		var n []string
		for _, r := range rs {
			n = append(n, r.Name)
		}
		return strings.Join(n, ",")
	}
	if got := namen("/api/v1/collections"); got != "Doppelreihe,Einzelreihe" {
		t.Fatalf("Seite Filmreihen: %s", got)
	}
	if got := namen("/api/v1/collections?limit=12"); got != "Doppelreihe" {
		t.Fatalf("Startseite: %s, erwartet nur Doppelreihe", got)
	}
	if got := namen("/api/v1/collections?limit=12&mindestens=1"); got != "Doppelreihe,Einzelreihe" {
		t.Fatalf("mit mindestens=1: %s", got)
	}
}

// Liegt die verkleinerte Fassung schon da, kommt sie, ohne dass das
// Original gelesen wird — vorher las jede Anfrage erst das ganze Original
// (auf einer kalten Platte über eine Sekunde je Poster).
func TestKleineFassungOhneOriginalZuLesen(t *testing.T) {
	h := newHarness(t)
	id, _ := h.film("Poster", 1)
	ordner := h.api.meta.ArtDir()
	name := id + "-poster.jpg"
	img := image.NewRGBA(image.Rect(0, 0, 1000, 1500))
	for y := 0; y < 1500; y++ {
		for x := 0; x < 1000; x++ {
			img.Set(x, y, color.RGBA{30, 120, 200, 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	pfad := filepath.Join(ordner, name)
	if err := os.WriteFile(pfad, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := h.st.SetArtwork(t.Context(), id, "poster", name, ""); err != nil {
		t.Fatal(err)
	}
	breite := func() int {
		t.Helper()
		resp, raw := h.do("GET", "/api/v1/art/"+id+"/poster?w=342", "", h.tok)
		if resp.StatusCode != 200 {
			t.Fatalf("Bild: %d", resp.StatusCode)
		}
		cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
		if err != nil {
			t.Fatalf("kein Bild: %v", err)
		}
		return cfg.Width
	}
	if b := breite(); b != 342 {
		t.Fatalf("erstes Mal: %d Punkte", b)
	}
	// Das Original unlesbar machen (gleiche Grösse und Zeit — für den
	// Speicher dasselbe Bild), den Speicher leeren: Die kleine Fassung
	// muss trotzdem kommen.
	info, _ := os.Stat(pfad)
	if err := os.WriteFile(pfad, make([]byte, info.Size()), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = os.Chtimes(pfad, info.ModTime(), info.ModTime())
	bilderImSpeicher.mu.Lock()
	bilderImSpeicher.m, bilderImSpeicher.wege, bilderImSpeicher.groesse = nil, nil, 0
	bilderImSpeicher.mu.Unlock()
	if b := breite(); b != 342 {
		t.Fatalf("zweites Mal: %d Punkte — das Original wurde gelesen", b)
	}
}

// Die Poster der Startseite werden im Hintergrund vorab verkleinert.
func TestPosterDerStartseiteVorab(t *testing.T) {
	h := newHarness(t)
	id, _ := h.film("Vorab", 1)
	name := id + "-poster.jpg"
	img := image.NewRGBA(image.Rect(0, 0, 900, 1350))
	var buf bytes.Buffer
	_ = jpeg.Encode(&buf, img, nil)
	if err := os.WriteFile(filepath.Join(h.api.meta.ArtDir(), name), buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := h.st.SetArtwork(t.Context(), id, "poster", name, ""); err != nil {
		t.Fatal(err)
	}
	h.startseitenTitel()
	bis := time.Now().Add(5 * time.Second)
	for time.Now().Before(bis) {
		_, e1 := os.Stat(filepath.Join(h.daten, kleinOrdner, "342", name))
		_, e2 := os.Stat(filepath.Join(h.daten, kleinOrdner, "500", name))
		if e1 == nil && e2 == nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("die Poster der Startseite wurden nicht vorab verkleinert")
}
