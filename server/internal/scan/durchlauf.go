// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package scan

// Was ein Durchlauf sich merkt, solange er läuft.
//
// Ein Durchlauf über eine Sammlung, an der sich nichts geändert hat, soll
// die Platten so gut wie nicht anfassen — weder die mit den Filmen noch
// die mit der Datenbank. Früher las er jede Datei sechs- bis achtmal aus
// der Datenbank (zweimal dieselbe Messung, dann Titel, Dateien …),
// las für jede Folge den ganzen Staffelordner neu (Untertitel daneben)
// und schrieb am Ende jede Datei zurück, auch wenn sich nichts geändert
// hatte. Bei 120 000 Dateien hiess das: 120 000 Schreibvorgänge in die
// Datenbank bei jedem nächtlichen Durchlauf.
//
// Jetzt wird die letzte Messung einer Datei einmal gelesen und bis zum
// Schreiben behalten, der zuletzt gelesene Ordner wird nicht noch einmal
// gelesen, und eine Datei, deren Werte sich nicht geändert haben, wird
// gar nicht geschrieben.
//
// Der Merker hängt am Kontext des einen Durchlaufs: Zwei Bibliotheken,
// die gleichzeitig geprüft werden, kommen sich nicht in die Quere, und
// nach dem Durchlauf ist alles wieder weg.

import (
	"context"
	"os"
	"path/filepath"
	"sync"

	"lumen/internal/store"
)

type durchlaufKey struct{}

type durchlaufMerker struct {
	mu sync.Mutex
	// gemessen: die letzte Messung je Pfad, bis die Datei geschrieben
	// (oder als unverändert erkannt) ist.
	gemessen map[string]*store.Gemessen
	// Der zuletzt gelesene Ordner. Einer genügt: Der Durchlauf arbeitet
	// Ordner für Ordner, und die Folgen einer Staffel liegen beieinander.
	ordner       string
	ordnerInhalt []os.DirEntry
	ordnerFehler error
	// neuDefekt: Dateien, die sich in diesem Durchlauf nicht ausmessen
	// liessen — am Ende eine Zeile im Protokoll statt einer je Datei.
	neuDefekt int
	// lesefehler: Dateien, die beim Messen auf einmal nicht mehr da waren
	// (Freigabe setzt aus) — zählen wie Lesefehler beim Durchgehen.
	lesefehler int
	// neuePfade: Dateien, die Lumen vor diesem Durchlauf nicht kannte —
	// Kandidaten für umbenannte Dateien (umbenannt.go).
	neuePfade []string
	// neueTitel und aktualisiert: für das Ergebnis (scan.go, zaehlen).
	neueTitel    map[string]bool
	aktualisiert map[string]bool
}

func mitDurchlauf(ctx context.Context) context.Context {
	return context.WithValue(ctx, durchlaufKey{}, &durchlaufMerker{gemessen: map[string]*store.Gemessen{},
		neueTitel: map[string]bool{}, aktualisiert: map[string]bool{}})
}

func merkerVon(ctx context.Context) *durchlaufMerker {
	m, _ := ctx.Value(durchlaufKey{}).(*durchlaufMerker)
	return m
}

// gemessen liefert die letzte Messung einer Datei (nil = unbekannt) —
// innerhalb eines Durchlaufs nur einmal aus der Datenbank.
func (s *Scanner) gemessen(ctx context.Context, path string) *store.Gemessen {
	m := merkerVon(ctx)
	if m == nil {
		return s.st.GemessenZuPfad(ctx, path)
	}
	m.mu.Lock()
	g, ok := m.gemessen[path]
	m.mu.Unlock()
	if ok {
		return g
	}
	g = s.st.GemessenZuPfad(ctx, path)
	m.mu.Lock()
	m.gemessen[path] = g
	m.mu.Unlock()
	return g
}

// itemZuPfad: der Titel, an dem eine Datei schon hängt, oder "".
func (s *Scanner) itemZuPfad(ctx context.Context, path string) string {
	if g := s.gemessen(ctx, path); g != nil {
		return g.Datei.ItemID
	}
	return ""
}

// ordnerLesen liest einen Ordner — denselben nicht zweimal hintereinander.
func ordnerLesen(ctx context.Context, dir string) ([]os.DirEntry, error) {
	m := merkerVon(ctx)
	if m == nil {
		return os.ReadDir(dir)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ordner != dir {
		m.ordner = dir
		m.ordnerInhalt, m.ordnerFehler = os.ReadDir(dir)
	}
	return m.ordnerInhalt, m.ordnerFehler
}

// untertitelDaneben ist SidecarSubtitles mit gemerktem Ordner.
func untertitelDaneben(ctx context.Context, videoPath string) []Track {
	entries, err := ordnerLesen(ctx, filepath.Dir(videoPath))
	if err != nil {
		return nil
	}
	return sidecarAus(videoPath, entries)
}

// dateiSchreiben speichert eine Datei — aber nur, wenn sich gegenüber der
// letzten Messung etwas geändert hat. Ein Durchlauf ohne Änderungen
// schreibt so nichts in die Datenbank, und deren Platte darf schlafen.
//
// st ist der Store, über den geschrieben wird — s.st oder der eines
// Stapels (store.Stapel).
func (s *Scanner) dateiSchreiben(ctx context.Context, st *store.Store, f *store.MediaFile) error {
	var alt *store.Gemessen
	gemerkt := false
	if m := merkerVon(ctx); m != nil {
		m.mu.Lock()
		alt, gemerkt = m.gemessen[f.Path]
		// Ab hier gilt, was gleich in der Datenbank steht — nicht mehr
		// die gemerkte Messung.
		delete(m.gemessen, f.Path)
		m.mu.Unlock()
	}
	if alt != nil && f.Spuren == nil && f.Kapitel == nil && gleicheDatei(&alt.Datei, f) {
		f.ID = alt.Datei.ID
		return nil
	}
	if m := merkerVon(ctx); m != nil {
		m.mu.Lock()
		switch {
		case alt == nil && gemerkt:
			m.neuePfade = append(m.neuePfade, f.Path)
		}
		m.mu.Unlock()
	}
	return st.UpsertFile(ctx, f)
}

// gleicheDatei vergleicht alles, was UpsertFile schreiben würde. Steht
// eine Datei als fehlend da, ist sie nicht gleich: Sie ist ja wieder da,
// und das muss in die Datenbank.
func gleicheDatei(alt, neu *store.MediaFile) bool {
	teil := neu.PartNo
	if teil == 0 {
		teil = 1
	}
	return !alt.Missing && alt.Defekt == neu.Defekt &&
		alt.ItemID == neu.ItemID && alt.SizeBytes == neu.SizeBytes && alt.MTime == neu.MTime &&
		alt.PartNo == teil && alt.Edition == neu.Edition && alt.Container == neu.Container &&
		alt.DurationMS == neu.DurationMS && alt.Bitrate == neu.Bitrate &&
		alt.Width == neu.Width && alt.Height == neu.Height &&
		alt.VideoCodec == neu.VideoCodec && alt.HDR == neu.HDR
}
