// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

// Einstellungen rund ums Umwandeln: Hardware-Beschleunigung, Vorschau-
// bilder beim Spulen, Untertitel vorbereiten, Vorspann erkennen. Nur der Besitzer, nur in der
// Verwaltung (Desktop-Browser und Mac-App).

import (
	"context"
	"net/http"
	"os"
	"strings"

	"lumen/internal/stream"
)

// Schlüssel in der Tabelle setting.
const (
	einstHardware   = "umwandlung.hardware"
	einstTrickplay  = "umwandlung.trickplay"
	einstUntertitel = "umwandlung.untertitel_vorbereiten"
)

// hardwareAusUmgebung: LUMEN_HARDWARE legt das Verfahren fest. Steht es
// dort, gilt es — die Verwaltung zeigt es an, ändern lässt es sich nur
// im Container. Wer seine Hardware in docker-compose.yml einträgt, will
// nicht, dass ein Klick es wieder ausschaltet.
func hardwareAusUmgebung() string {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("LUMEN_HARDWARE")))
	if stream.GueltigeWahl(v) {
		return v
	}
	return ""
}

// UmwandlungEinrichten verdrahtet Hardware-Erkennung, Protokoll und die
// Arbeit im Hintergrund. Einmal beim Start.
func (s *Server) UmwandlungEinrichten(ctx context.Context) {
	if s.ff == nil {
		return
	}
	wahl := hardwareAusUmgebung()
	if wahl == "" {
		wahl = s.st.Setting(ctx, einstHardware, stream.HWAuto)
	}
	s.ff.HW = stream.NeueHardware(s.ff, wahl, os.Getenv("LUMEN_VAAPI_GERAET"))
	if s.tr != nil {
		s.tr.Protokoll = func(stufe, text string) {
			s.st.Log(context.Background(), stufe, "umwandlung", text)
		}
	}
	if !s.ff.Available() {
		return
	}
	// begleitet: Das Herunterfahren wartet darauf (begleitet.go).
	s.begleitetMit(ctx, func(ctx context.Context) {
		s.ff.HW.Pruefen(ctx)
		s.hardwareMelden(ctx)
	})
	s.begleitetMit(ctx, s.hintergrundArbeit)
}

// hardwareMelden schreibt ins Protokoll, womit umgewandelt wird.
func (s *Server) hardwareMelden(ctx context.Context) {
	z := s.ff.HW.Zustand()
	switch {
	case z.Aktiv != "":
		s.st.Log(ctx, "info", "umwandlung", "Umgewandelt wird mit Hardware-Beschleunigung ("+strings.ToUpper(z.Aktiv)+")")
	case z.Wahl != stream.HWAus && len(z.Verfuegbar) == 0:
		s.st.Log(ctx, "info", "umwandlung", "Keine Hardware-Beschleunigung gefunden — umgewandelt wird in Software")
	}
}

func (s *Server) trickplayAn(ctx context.Context) bool {
	return s.st.Setting(ctx, einstTrickplay, "1") == "1"
}

func (s *Server) untertitelVorbereitenAn(ctx context.Context) bool {
	return s.st.Setting(ctx, einstUntertitel, "1") == "1"
}

// tonemapWeg nennt, womit HDR umgerechnet wird — zur Anzeige.
func (s *Server) tonemapWeg() string {
	if s.ff == nil || !s.ff.Available() {
		return "keins"
	}
	k := s.ff.Faehigkeiten()
	switch {
	case k.Filter["zscale"] && k.Filter["tonemap"]:
		return "zscale"
	case k.Filter["colorspace"]:
		return "colorspace"
	}
	return "keins"
}

// adminUmwandlung: GET /admin/umwandlung.
func (s *Server) adminUmwandlung(w http.ResponseWriter, r *http.Request) {
	_, maxAusUmgebung := os.LookupEnv("LUMEN_MAX_UMWANDLUNGEN")
	ok(w, map[string]any{
		"hardware":              s.ff.HW.Zustand(),
		"hardwareAusUmgebung":   hardwareAusUmgebung() != "",
		"tonemapping":           s.tonemapWeg(),
		"maxUmwandlungen":       s.maxUmwandlungen(),
		"maxAusUmgebung":        maxAusUmgebung,
		"trickplay":             s.trickplayAn(r.Context()),
		"untertitelVorbereiten": s.untertitelVorbereitenAn(r.Context()),
		"vorrat":                s.vorratZaehlen(),
		"vorspannErkennung":     s.erkennungZustand(r.Context()),
	})
}

// adminUmwandlungSetzen: PUT /admin/umwandlung — nur, was mitkommt.
func (s *Server) adminUmwandlungSetzen(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Hardware              *string `json:"hardware"`
		Trickplay             *bool   `json:"trickplay"`
		UntertitelVorbereiten *bool   `json:"untertitelVorbereiten"`
		VorspannErkennen      *bool   `json:"vorspannErkennen"`
		VorspannNurNachts     *bool   `json:"vorspannNurNachts"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	ctx := r.Context()
	if in.Hardware != nil {
		v := strings.ToLower(strings.TrimSpace(*in.Hardware))
		if !stream.GueltigeWahl(v) {
			fail(w, http.StatusBadRequest, "unbekanntes verfahren — erlaubt sind auto, aus, vaapi, qsv und nvenc")
			return
		}
		if hardwareAusUmgebung() != "" {
			fail(w, http.StatusConflict, "das verfahren ist über LUMEN_HARDWARE festgelegt und lässt sich nur dort ändern")
			return
		}
		if err := s.st.SetSetting(ctx, einstHardware, v); err != nil {
			failErr(w, err)
			return
		}
		s.ff.HW.Setzen(v)
	}
	janein := func(b bool) string {
		if b {
			return "1"
		}
		return "0"
	}
	if in.Trickplay != nil {
		if err := s.st.SetSetting(ctx, einstTrickplay, janein(*in.Trickplay)); err != nil {
			failErr(w, err)
			return
		}
	}
	if in.UntertitelVorbereiten != nil {
		if err := s.st.SetSetting(ctx, einstUntertitel, janein(*in.UntertitelVorbereiten)); err != nil {
			failErr(w, err)
			return
		}
	}
	if in.VorspannErkennen != nil {
		if err := s.st.SetSetting(ctx, einstErkennung, janein(*in.VorspannErkennen)); err != nil {
			failErr(w, err)
			return
		}
	}
	if in.VorspannNurNachts != nil {
		if err := s.st.SetSetting(ctx, einstErkennungNachts, janein(*in.VorspannNurNachts)); err != nil {
			failErr(w, err)
			return
		}
	}
	if (in.Trickplay != nil && *in.Trickplay) || (in.UntertitelVorbereiten != nil && *in.UntertitelVorbereiten) ||
		(in.VorspannErkennen != nil && *in.VorspannErkennen) {
		s.hintergrundAnstossen()
	}
	s.adminUmwandlung(w, r)
}

// adminUmwandlungPruefen: POST /admin/umwandlung/pruefen — die Hardware
// noch einmal ausprobieren (nach einem Treiber-Update, nach dem
// Durchreichen von /dev/dri). Läuft im Hintergrund; die Verwaltung fragt
// danach den Zustand ab.
func (s *Server) adminUmwandlungPruefen(w http.ResponseWriter, r *http.Request) {
	if s.ff == nil || !s.ff.Available() {
		fail(w, http.StatusServiceUnavailable, "ohne FFmpeg gibt es nichts zu prüfen")
		return
	}
	go func() {
		ctx := context.Background()
		s.ff.HW.Pruefen(ctx)
		s.hardwareMelden(ctx)
	}()
	w.WriteHeader(http.StatusAccepted)
}
