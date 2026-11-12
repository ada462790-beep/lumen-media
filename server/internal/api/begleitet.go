// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"sync"
)

// Arbeit im Hintergrund, auf die das Herunterfahren wartet.
//
// ArbeitAbwarten wartete nur auf die Arbeitssperre (Durchläufe,
// Metadaten) und die Nachträge. Der Hintergrund-Durchgang (Vorschaubilder,
// Untertitel), das Zusammenführen von Teilen und der Durchlauf danach
// liefen beim Herunterfahren weiter — und schrieben in die Datenbank, die
// main gerade geschlossen hatte („spuren speichern: sql: database is
// closed"). Wer so etwas startet, startet es über begleitet.

// begleitZaehler zählt die laufenden Goroutinen eines Servers (Feld
// begleiter in Server). Der Nullwert ist bereit.
type begleitZaehler struct {
	mu   sync.Mutex
	n    int
	leer chan struct{} // geht zu, wenn n wieder 0 ist; nil = nichts lief je
}

func (z *begleitZaehler) dazu() {
	z.mu.Lock()
	if z.n == 0 {
		z.leer = make(chan struct{})
	}
	z.n++
	z.mu.Unlock()
}

func (z *begleitZaehler) fertig() {
	z.mu.Lock()
	z.n--
	if z.n == 0 && z.leer != nil {
		close(z.leer)
	}
	z.mu.Unlock()
}

// alleFertig: ein Kanal, der zugeht, sobald nichts mehr läuft.
func (z *begleitZaehler) alleFertig() <-chan struct{} {
	z.mu.Lock()
	defer z.mu.Unlock()
	if z.n == 0 {
		c := make(chan struct{})
		close(c)
		return c
	}
	return z.leer
}

// begleitet startet fn in einer eigenen Goroutine am Hintergrund-Kontext
// (endet beim Herunterfahren) und meldet sie beim Herunterfahren an.
func (s *Server) begleitet(fn func(ctx context.Context)) {
	s.begleitetMit(s.hintergrund(), fn)
}

// begleitetMit ist begleitet mit einem eigenen Kontext.
func (s *Server) begleitetMit(ctx context.Context, fn func(ctx context.Context)) {
	s.begleiter.dazu()
	go func() {
		defer s.begleiter.fertig()
		fn(ctx)
	}()
}
