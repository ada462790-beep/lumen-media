// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package scan

// Messen mit ein paar Helfern.
//
// Beim ersten Durchlauf lief FFprobe streng nacheinander: eine Datei, dann
// die nächste. FFprobe kostet vor allem den Prozessstart und das Warten
// auf die Platte; die Datenbank hat in der Zeit nichts zu tun. Jetzt
// messen zwei (bei mehr als vier Kernen bis zu vier) Helfer voraus, mit
// niedriger Priorität (stream.Niedrig), und eingetragen wird weiter der
// Reihe nach von einem einzigen Schreiber — an der Datenbank ändert sich
// nichts. Auf einem NAS mit HDD bringt der zweite Helfer vor allem, dass
// die Platte nicht zwischen zwei Messungen leerläuft.

import (
	"context"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"

	"lumen/internal/store"
)

// filmAuftrag: eine Filmdatei, gefunden beim Durchgehen der Ordner.
type filmAuftrag struct {
	g     Guess
	path  string
	dirs  []string
	size  int64
	mtime int64
}

// messHelfer: wie viele Messungen gleichzeitig laufen.
func messHelfer() int {
	n := runtime.NumCPU()
	switch {
	case n <= 4:
		return 2
	case n/2 > 4:
		return 4
	}
	return n / 2
}

// dateiSkelett: die Datei, wie sie der Durchlauf vor dem Messen kennt.
func dateiSkelett(g Guess, path string, size, mtime int64) *store.MediaFile {
	return &store.MediaFile{
		Path: path, SizeBytes: size, MTime: mtime,
		PartNo: g.PartNo, Edition: g.Edition, HDR: g.HDR,
		Container: strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), "."),
	}
}

// vorausMessen misst die Dateien mit messHelfer Helfern, in der
// Reihenfolge der Liste. gemessen(i) wartet, bis die i-te fertig ist, und
// gibt sie zurück (gemessen in der Liste selbst, fillFromProbe). Endet
// ctx, misst niemand mehr; gemessen kommt dann sofort zurück.
func (s *Scanner) vorausMessen(ctx context.Context, dateien []*store.MediaFile) (gemessen func(i int) *store.MediaFile) {
	fertig := make([]chan struct{}, len(dateien))
	for i := range fertig {
		fertig[i] = make(chan struct{})
	}
	helfer := messHelfer()
	if s.probe == nil {
		helfer = 1 // ohne FFmpeg gibt es nichts zu warten
	}
	var naechste atomic.Int64
	for h := 0; h < helfer && h < len(dateien); h++ {
		go func() {
			for {
				i := int(naechste.Add(1) - 1)
				if i >= len(dateien) {
					return
				}
				if ctx.Err() == nil {
					s.fillFromProbe(ctx, dateien[i])
				}
				close(fertig[i])
			}
		}()
	}
	return func(i int) *store.MediaFile {
		<-fertig[i]
		return dateien[i]
	}
}

// messungNehmen übernimmt eine Messung aus vorausMessen in f — oder misst
// jetzt, wenn es keine gibt.
func (s *Scanner) messungNehmen(ctx context.Context, f, m *store.MediaFile) {
	if m == nil || m.Path != f.Path {
		s.fillFromProbe(ctx, f)
		return
	}
	f.DurationMS, f.Bitrate = m.DurationMS, m.Bitrate
	f.Width, f.Height, f.VideoCodec = m.Width, m.Height, m.VideoCodec
	f.HDR, f.Container, f.Defekt = m.HDR, m.Container, m.Defekt
	f.Spuren, f.Kapitel = m.Spuren, m.Kapitel
}
