// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package meta

// Folgen staffelweise holen.
//
// Früher kostete jede Folge eine eigene Anfrage bei TMDb
// (/tv/{id}/season/{n}/episode/{e}). Bei einer Mediathek mit ein paar
// tausend Folgen hiess das: ein Durchgang von einer halben Stunde, „50
// Titel geprüft · 50 zugeordnet" alle paar Sekunden, und die NAS die
// ganze Zeit beschäftigt. TMDb liefert mit /tv/{id}/season/{n} alle
// Folgen einer Staffel samt Titel, Inhalt, Sendedatum, Laufzeit und
// Standbild — eine Anfrage für 10 bis 25 Folgen.
//
// Die Staffel bleibt eine Weile im Speicher dieser TMDb-Verbindung (nicht
// global: ein neuer Schlüssel oder eine andere Sprache fängt frisch an).
// Kennt die Staffel eine Folge nicht oder scheitert die Staffel-Anfrage,
// fragt FolgeAusStaffel wie früher nach der einzelnen Folge.

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// staffelHaltbar: so lange gilt eine geholte Staffel. Ein Durchgang über
// eine Serie dauert Sekunden bis Minuten; neue Folgen kommen beim nächsten.
const staffelHaltbar = 15 * time.Minute

type staffelFolgen struct {
	geholt time.Time
	fertig chan struct{} // zu, sobald folgen/err feststehen
	folgen map[int]*Candidate
	err    error
}

// staffelHolen liefert alle Folgen einer Staffel, je Staffel höchstens
// eine Anfrage gleichzeitig.
func (t *TMDb) staffelHolen(ctx context.Context, seriesID string, staffel int) (map[int]*Candidate, error) {
	schluessel := fmt.Sprintf("%s/%d", seriesID, staffel)
	t.staffelMu.Lock()
	if t.staffelDaten == nil {
		t.staffelDaten = map[string]*staffelFolgen{}
	}
	if e, ok := t.staffelDaten[schluessel]; ok && time.Since(e.geholt) < staffelHaltbar {
		t.staffelMu.Unlock()
		select {
		case <-e.fertig:
			return e.folgen, e.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	// Abgelaufene Einträge wegräumen, damit der Speicher nicht wächst.
	for k, e := range t.staffelDaten {
		if time.Since(e.geholt) >= staffelHaltbar {
			delete(t.staffelDaten, k)
		}
	}
	e := &staffelFolgen{geholt: time.Now(), fertig: make(chan struct{})}
	t.staffelDaten[schluessel] = e
	t.staffelMu.Unlock()

	e.folgen, e.err = t.staffelAbfragen(ctx, seriesID, staffel)
	if e.err != nil && ctx.Err() != nil {
		// Abgebrochen (Herunterfahren): nicht als Ergebnis merken.
		t.staffelMu.Lock()
		delete(t.staffelDaten, schluessel)
		t.staffelMu.Unlock()
	}
	close(e.fertig)
	return e.folgen, e.err
}

func (t *TMDb) staffelAbfragen(ctx context.Context, seriesID string, staffel int) (map[int]*Candidate, error) {
	if err := tmdbKennung(seriesID); err != nil {
		return nil, err
	}
	var d struct {
		Episodes []struct {
			Number    int    `json:"episode_number"`
			Name      string `json:"name"`
			Overview  string `json:"overview"`
			AirDate   string `json:"air_date"`
			Runtime   int    `json:"runtime"`
			StillPath string `json:"still_path"`
		} `json:"episodes"`
	}
	path := fmt.Sprintf("/tv/%s/season/%d", url.PathEscape(seriesID), staffel)
	if err := t.get(ctx, path, url.Values{"language": {t.Language}}, &d); err != nil {
		return nil, err
	}
	out := make(map[int]*Candidate, len(d.Episodes))
	for _, e := range d.Episodes {
		out[e.Number] = &Candidate{
			Source: "tmdb", Title: e.Name, Overview: e.Overview,
			Premiered: e.AirDate, Year: yearOf(e.AirDate),
			RuntimeMS: int64(e.Runtime) * 60_000,
			PosterURL: t.image(e.StillPath, "w780"),
		}
	}
	return out, nil
}

// FolgeAusStaffel holt eine Folge über die Daten ihrer Staffel — und nur
// wenn die nichts taugen, einzeln (Episode).
func (t *TMDb) FolgeAusStaffel(ctx context.Context, seriesID string, staffel, folge int) (*Candidate, error) {
	folgen, err := t.staffelHolen(ctx, seriesID, staffel)
	if err != nil && ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err == nil {
		if c, ok := folgen[folge]; ok {
			x := *c // eine Kopie: der Aufrufer ergänzt Doppelfolgen darin
			return &x, nil
		}
	}
	// Die Staffel kennt die Folge nicht (oder die Anfrage scheiterte, etwa
	// weil TMDb die Staffel anders zählt): einzeln nachfragen. „Nicht
	// gefunden" kommt dann von dort.
	if err != nil && strings.Contains(err.Error(), "bremst") {
		return nil, err
	}
	return t.Episode(ctx, seriesID, staffel, folge)
}

// Die Einzelheiten einer Serie fragten bisher drei Nachträge nacheinander
// ab (Zeitraum, Altersfreigabe, IMDb-Kennung) — dreimal dieselbe Anfrage.
// Für kurze Zeit gilt die erste.
const serienHaltbar = 5 * time.Minute

type serienEintrag struct {
	geholt time.Time
	c      *Candidate
}

// SeriesDetails holt die Einzelheiten einer Serie (Zeitraum, Freigabe,
// Kennungen, Besetzung) — innerhalb von serienHaltbar nur einmal.
func (t *TMDb) SeriesDetails(ctx context.Context, seriesID string) (*Candidate, error) {
	t.staffelMu.Lock()
	if e, ok := t.serienDaten[seriesID]; ok && time.Since(e.geholt) < serienHaltbar {
		t.staffelMu.Unlock()
		x := *e.c
		return &x, nil
	}
	t.staffelMu.Unlock()
	c, err := t.seriesDetailsAbfragen(ctx, seriesID)
	if err != nil {
		return nil, err
	}
	t.staffelMu.Lock()
	if t.serienDaten == nil {
		t.serienDaten = map[string]serienEintrag{}
	}
	for k, e := range t.serienDaten {
		if time.Since(e.geholt) >= serienHaltbar {
			delete(t.serienDaten, k)
		}
	}
	t.serienDaten[seriesID] = serienEintrag{geholt: time.Now(), c: c}
	t.staffelMu.Unlock()
	x := *c
	return &x, nil
}
