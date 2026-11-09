// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

// Bilder dekodieren — an einer Stelle, mit Grenzen.
//
// Ein Bild zu dekodieren kostet Speicher nach seiner Grösse in Punkten,
// nicht nach der Grösse der Datei: Ein PNG von wenigen Kilobyte kann
// 12000 × 12000 Punkte behaupten und braucht dann über 500 MB. Darum:
//
//   - Erst der Kopf (image.DecodeConfig), dann die Grenzen, erst danach
//     das eigentliche Dekodieren.
//   - Höchstens bildPixelHoechstens Punkte; jeder Weg kann weniger
//     verlangen (Profilbilder: 4096 × 4096).
//   - Höchstens zwei Bilder gleichzeitig im ganzen Server (bildDekoder).
//     Zwanzig gleichzeitige Uploads zu je 25 MP hätten sonst zwei
//     Gigabyte belegt — genug, um einen kleinen NAS abstürzen zu lassen.

import (
	"bytes"
	"context"
	"image"
	"net/http"
	"strings"
	"time"
)

// bildPixelHoechstens: so viele Bildpunkte dekodiert Lumen höchstens
// (25 MP, als RGBA rund 100 MB). Früher 60 MP.
const bildPixelHoechstens = 25_000_000

// bildDekoder: wie viele Bilder gleichzeitig dekodiert werden dürfen.
var bildDekoder = make(chan struct{}, 2)

// bildDekoderWarten: so lange wartet eine Anfrage auf einen freien Platz.
// Eine Variable nur für Tests.
var bildDekoderWarten = 20 * time.Second

// bildGrenzen: was ein Weg annimmt.
type bildGrenzen struct {
	seite  int // längste Seite
	punkte int // Breite × Höhe; 0 = bildPixelHoechstens
	klein  int // kürzeste Seite mindestens; 0 = egal
}

// bildDekodieren prüft den Kopf gegen die Grenzen und dekodiert erst
// danach — unter dem gemeinsamen Riegel bildDekoder. weiter läuft noch
// unter dem Riegel: Solange das grosse Bild im Speicher liegt (beim
// Verkleinern und Kodieren), zählt es mit.
func bildDekodieren(ctx context.Context, raw []byte, g bildGrenzen, weiter func(image.Image) error) error {
	punkte := g.punkte
	if punkte <= 0 || punkte > bildPixelHoechstens {
		punkte = bildPixelHoechstens
	}
	kopf, art, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return &apiFehler{status: http.StatusUnsupportedMediaType, text: "nur JPEG, PNG oder GIF"}
	}
	if kopf.Width <= 0 || kopf.Height <= 0 || kopf.Width > g.seite || kopf.Height > g.seite ||
		int64(kopf.Width)*int64(kopf.Height) > int64(punkte) {
		return &apiFehler{status: http.StatusRequestEntityTooLarge,
			text:  "das Bild hat zu viele Bildpunkte (höchstens {n} Megapixel, {seite} Punkte je Seite)",
			werte: []any{"n", punkte / 1_000_000, "seite", g.seite}}
	}
	if g.klein > 0 && (kopf.Width < g.klein || kopf.Height < g.klein) {
		return &apiFehler{status: http.StatusBadRequest, text: "das Bild ist zu klein"}
	}

	warte := time.NewTimer(bildDekoderWarten)
	defer warte.Stop()
	select {
	case bildDekoder <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	case <-warte.C:
		return &apiFehler{status: http.StatusServiceUnavailable,
			text: "Gerade werden viele Bilder verarbeitet. Bitte gleich noch einmal."}
	}
	defer func() { <-bildDekoder }()

	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return &apiFehler{status: http.StatusUnsupportedMediaType, text: "das {art}-Bild ist beschädigt",
			werte: []any{"art", strings.ToUpper(art)}}
	}
	return weiter(img)
}
