// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package scan

// Was der letzte Durchlauf mit Absicht ausgelassen hat — damit sich in der
// Verwaltung nachsehen lässt, warum ein Film fehlt: Er heisst wie ein
// Trailer („…-trailer.mkv"), liegt in einem Extras-Ordner, hat eine
// Endung, die Lumen nicht abspielt (.iso, .rmvb), oder liess sich nicht
// lesen.
//
// Die Liste liegt nur im Arbeitsspeicher, je Bibliothek, und wird bei
// jedem vollständigen Durchlauf ersetzt. Ins Protokoll kommt davon
// nichts — dort stehen keine Pfade.

import (
	"path/filepath"
	"sort"
	"strings"
)

// Gründe, warum eine Datei nicht in die Mediathek kam.
const (
	AuslassBeiwerk = "beiwerk"        // Name wie ein Trailer, Sample, Extra
	AuslassOrdner  = "beiwerk_ordner" // Extras-, Trailer-, Featurettes-Ordner
	AuslassEndung  = "endung"         // ein Video, das Lumen nicht abspielt
	AuslassFehler  = "unlesbar"       // Ordner oder Datei liess sich nicht lesen
	// Ein Verweis auf einen Ordner, dem der Durchlauf nicht folgt: zurück
	// in die Bibliothek (Schleife), in Lumens Datenordner, schon gelesen.
	AuslassVerweis = "verweis"
)

// maxAuslassPfade: so viele Pfade je Grund werden gezeigt; gezählt wird
// alles.
const maxAuslassPfade = 200

// Auslassung ist ein Grund mit Anzahl und (höchstens 200) Pfaden.
type Auslassung struct {
	Grund  string   `json:"grund"`
	Anzahl int      `json:"anzahl"`
	Pfade  []string `json:"pfade"`
}

// unbekannteEndungen sind Videos, die der Scanner bewusst nicht aufnimmt
// (siehe VideoExt) oder nicht kennt. Andere Dateien (Bilder, Untertitel,
// NFO) sind kein Fall für die Liste.
var unbekannteEndungen = map[string]bool{
	".iso": true, ".img": true, ".strm": true, ".rmvb": true, ".rm": true, ".3gp": true,
	".divx": true, ".ogm": true, ".ogv": true, ".asf": true, ".f4v": true, ".mts": true,
	".mxf": true, ".wtv": true, ".dvr-ms": true, ".m2v": true, ".mod": true, ".tod": true,
}

type auslassLauf struct {
	je map[string]*Auslassung
}

func neuerAuslassLauf() *auslassLauf { return &auslassLauf{je: map[string]*Auslassung{}} }

func (a *auslassLauf) merken(grund, pfad string) {
	x := a.je[grund]
	if x == nil {
		x = &Auslassung{Grund: grund, Pfade: []string{}}
		a.je[grund] = x
	}
	x.Anzahl++
	if len(x.Pfade) < maxAuslassPfade {
		x.Pfade = append(x.Pfade, pfad)
	}
}

// datei prüft eine Datei, die der Durchlauf nicht nimmt, und merkt sie,
// wenn sie ein Video ist. Versteckte Dateien („._film.mkv" von macOS)
// sind kein Fall.
func (a *auslassLauf) datei(pfad string) {
	base := filepath.Base(pfad)
	if strings.HasPrefix(base, ".") {
		return
	}
	ext := strings.ToLower(filepath.Ext(base))
	switch {
	case IsVideo(pfad) && IsJunkFile(pfad):
		a.merken(AuslassBeiwerk, pfad)
	case unbekannteEndungen[ext]:
		a.merken(AuslassEndung, pfad)
	}
}

func (a *auslassLauf) liste() []Auslassung {
	out := make([]Auslassung, 0, len(a.je))
	for _, x := range a.je {
		sort.Strings(x.Pfade)
		out = append(out, *x)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Grund < out[j].Grund })
	return out
}

// Ausgelassen nennt, was der letzte vollständige Durchlauf einer
// Bibliothek ausgelassen hat (nil: seit dem Start noch keiner).
func (s *Scanner) Ausgelassen(libraryID string) []Auslassung {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, ok := s.ausgelassen[libraryID]
	if !ok {
		return nil
	}
	out := make([]Auslassung, len(l))
	for i, x := range l {
		out[i] = x
		out[i].Pfade = append([]string(nil), x.Pfade...)
	}
	return out
}

func (s *Scanner) auslassenSetzen(libraryID string, a *auslassLauf) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ausgelassen == nil {
		s.ausgelassen = map[string][]Auslassung{}
	}
	s.ausgelassen[libraryID] = a.liste()
}
