// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
	"time"

	"lumen/internal/store"
)

// Ein hochgeladenes Bild wird quadratisch, klein und als JPEG ausgeliefert —
// auch ohne Anmeldung, für die Profilauswahl.
func TestProfilbildHochladen(t *testing.T) {
	h := newHarness(t)
	img := image.NewRGBA(image.Rect(0, 0, 900, 500))
	for y := 0; y < 500; y++ {
		for x := 0; x < 900; x++ {
			img.Set(x, y, color.RGBA{uint8(x / 4), uint8(y / 2), 90, 255})
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	req, _ := http.NewRequest("POST", h.srv.URL+"/api/v1/me/avatar", &buf)
	req.Header.Set("Authorization", "Bearer "+h.tok)
	req.Header.Set("Content-Type", "image/png")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var me struct {
		Account struct{ Avatar string }
	}
	_ = json.NewDecoder(resp.Body).Decode(&me)
	if resp.StatusCode != 200 || me.Account.Avatar != "bild" {
		t.Fatalf("hochladen: %d %+v", resp.StatusCode, me)
	}
	r2, raw := h.do("GET", "/api/v1/avatar/"+h.ownerID(), "", "")
	if r2.StatusCode != 200 || r2.Header.Get("Content-Type") != "image/jpeg" {
		t.Fatalf("bild: %d %s", r2.StatusCode, r2.Header.Get("Content-Type"))
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || cfg.Width != avatarKante || cfg.Height != avatarKante {
		t.Fatalf("Grösse: %v %+v", err, cfg)
	}
	// Unsinn als Profilbild wird abgelehnt, eine Figur angenommen.
	r3, _ := h.do("PATCH", "/api/v1/me", `{"avatar":"javascript:alert(1)"}`, h.tok)
	if r3.StatusCode != 400 {
		t.Errorf("unbekanntes Profilbild: %d", r3.StatusCode)
	}
	r4, _ := h.do("PATCH", "/api/v1/me", `{"avatar":"fig-fuchs"}`, h.tok)
	if r4.StatusCode != 200 {
		t.Errorf("Figur: %d", r4.StatusCode)
	}
}

// Neuigkeiten entstehen aus dem eigenen Stand: eine neue Folge in einer
// Serie, die ich angefangen habe — für jemanden, der sie nie geschaut hat,
// gibt es nichts zu melden. Und der Haken „gelesen" gilt nur für mich.
func TestNeuigkeitenSindPrivat(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()
	h.film("Nordlicht", 1)
	serie := &store.Item{LibraryID: h.lib.ID, Kind: "series", Title: "Tiefsee", MatchState: "matched"}
	_ = h.st.UpsertItem(ctx, serie)
	staffel := &store.Item{LibraryID: h.lib.ID, Kind: "season", ParentID: serie.ID, Title: "Staffel 1", SeasonNo: 1, MatchState: "matched"}
	_ = h.st.UpsertItem(ctx, staffel)
	alt := &store.Item{LibraryID: h.lib.ID, Kind: "episode", ParentID: staffel.ID, Title: "Eins", SeasonNo: 1, EpisodeNo: 1, MatchState: "matched",
		AddedAt: time.Now().Add(-10 * 24 * time.Hour)}
	_ = h.st.UpsertItem(ctx, alt)
	// Alex hat Folge 1 vor drei Tagen gesehen.
	_ = h.st.MarkFinished(ctx, h.ownerID(), alt.ID, 0)
	time.Sleep(1100 * time.Millisecond) // added_at zählt in Sekunden
	neu := &store.Item{LibraryID: h.lib.ID, Kind: "episode", ParentID: staffel.ID, Title: "Zwei", SeasonNo: 1, EpisodeNo: 2, MatchState: "matched"}
	_ = h.st.UpsertItem(ctx, neu)

	jonas, _ := h.st.CreateAccount(ctx, store.NewAccount{Name: "Jonas", Grants: []string{h.lib.ID}})
	jtok, _ := h.st.IssueToken(ctx, jonas.ID, "iPhone", "phone")

	lesen := func(tok string) (arten []string, ungelesen int) {
		_, raw := h.do("GET", "/api/v1/news", "", tok)
		var out struct {
			Eintraege []struct{ Art, Titel string }
			Ungelesen int
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("%s", raw)
		}
		for _, e := range out.Eintraege {
			arten = append(arten, e.Art+":"+e.Titel)
		}
		return arten, out.Ungelesen
	}
	arten, ungelesen := lesen(h.tok)
	if !strings.Contains(strings.Join(arten, ","), "folgen:Tiefsee") || ungelesen < 1 {
		t.Errorf("Alex: %v (%d ungelesen)", arten, ungelesen)
	}
	jArten, _ := lesen(jtok)
	if strings.Contains(strings.Join(jArten, ","), "folgen:") {
		t.Errorf("Jonas sieht Alex' Serie: %v", jArten)
	}
	// /me zählt mit; nach „gesehen" steht es auf null — nur bei Alex.
	_, raw := h.do("GET", "/api/v1/me", "", h.tok)
	if !strings.Contains(string(raw), `"neuigkeiten":`) || strings.Contains(string(raw), `"neuigkeiten":0`) {
		t.Errorf("me: %s", raw)
	}
	h.do("POST", "/api/v1/news/seen", "", h.tok)
	if _, u := lesen(h.tok); u != 0 {
		t.Errorf("nach gesehen: %d ungelesen", u)
	}
}

// Ein winziges PNG, das behauptet, riesig zu sein, darf den Server nicht
// umwerfen — es wird abgelehnt, bevor Speicher angefordert wird.
func TestProfilbildBombe(t *testing.T) {
	h := newHarness(t)
	var b bytes.Buffer
	b.Write([]byte("\x89PNG\r\n\x1a\n"))
	ihdr := []byte{0, 0, 0xEA, 0x60, 0, 0, 0xEA, 0x60, 8, 2, 0, 0, 0} // 60000 × 60000
	_ = binary.Write(&b, binary.BigEndian, uint32(len(ihdr)))
	b.WriteString("IHDR")
	b.Write(ihdr)
	crc := crc32.NewIEEE()
	crc.Write([]byte("IHDR"))
	crc.Write(ihdr)
	_ = binary.Write(&b, binary.BigEndian, crc.Sum32())
	resp, _ := h.do("POST", "/api/v1/me/avatar", b.String(), h.tok)
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("riesiges Bild: %d, erwartet 413", resp.StatusCode)
	}
}

// pngKopf baut eine PNG-Datei, die nur aus dem Kopf besteht und die
// angegebene Grösse behauptet.
func pngKopf(breite, hoehe uint32) []byte {
	var b bytes.Buffer
	b.Write([]byte("\x89PNG\r\n\x1a\n"))
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], breite)
	binary.BigEndian.PutUint32(ihdr[4:], hoehe)
	ihdr[8], ihdr[9] = 8, 2
	_ = binary.Write(&b, binary.BigEndian, uint32(len(ihdr)))
	b.WriteString("IHDR")
	b.Write(ihdr)
	crc := crc32.NewIEEE()
	crc.Write([]byte("IHDR"))
	crc.Write(ihdr)
	_ = binary.Write(&b, binary.BigEndian, crc.Sum32())
	return b.Bytes()
}

// Ein Profilbild über 4096 Punkte je Seite wird am Kopf abgelehnt — auch
// als Formular. Ein Rumpf über der Grenze kommt gar nicht erst an.
func TestProfilbildGrenzen(t *testing.T) {
	h := newHarness(t)
	if resp, _ := h.do("POST", "/api/v1/me/avatar", string(pngKopf(5000, 3000)), h.tok); resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("5000 × 3000: %d, erwartet 413", resp.StatusCode)
	}
	// Als Formular, mit einem Rumpf von 40 MB: abgewiesen, ohne alles zu lesen.
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	fw, _ := mw.CreateFormFile("bild", "gross.png")
	_, _ = fw.Write(bytes.Repeat([]byte{0}, 40<<20))
	_ = mw.Close()
	req, _ := http.NewRequest("POST", h.srv.URL+"/api/v1/me/avatar", &b)
	req.Header.Set("Authorization", "Bearer "+h.tok)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err == nil {
		resp.Body.Close()
		if resp.StatusCode != http.StatusRequestEntityTooLarge {
			t.Fatalf("40 MB als Formular: %d, erwartet 413", resp.StatusCode)
		}
	}
}

// Sind beide Plätze zum Dekodieren belegt, wartet eine Anfrage nur kurz
// und bekommt dann 503 — statt dass beliebig viele Bilder gleichzeitig
// im Speicher liegen.
func TestBildDekoderRiegel(t *testing.T) {
	alt := bildDekoderWarten
	bildDekoderWarten = 50 * time.Millisecond
	defer func() { bildDekoderWarten = alt }()
	for i := 0; i < cap(bildDekoder); i++ {
		bildDekoder <- struct{}{}
	}
	defer func() {
		for i := 0; i < cap(bildDekoder); i++ {
			<-bildDekoder
		}
	}()
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	err := bildDekodieren(t.Context(), buf.Bytes(), bildGrenzen{seite: 100}, func(image.Image) error { return nil })
	var af *apiFehler
	if !errors.As(err, &af) || af.status != http.StatusServiceUnavailable {
		t.Fatalf("erwartet 503, bekommen %v", err)
	}
	// Ein zu grosses Bild scheitert am Kopf, ohne auf den Riegel zu warten.
	err = bildDekodieren(t.Context(), pngKopf(6000, 6000), bildGrenzen{seite: 12000}, func(image.Image) error { return nil })
	if !errors.As(err, &af) || af.status != http.StatusRequestEntityTooLarge {
		t.Fatalf("36 MP: erwartet 413, bekommen %v", err)
	}
}
