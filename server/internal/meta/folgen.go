// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package meta

// Folgen, die nicht nach Staffel und Nummer heissen: durchgezählte
// (Anime: „One Piece - 1071") und Tagesfolgen („Tagesschau 2024-03-15").
// Der Dateiname sagt nur die durchgehende Nummer oder das Sendedatum;
// welche Staffel und Folge das bei TMDb ist, steht in den Staffeldaten.

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// StaffelInfo ist eine Staffel, wie TMDb sie in den Einzelheiten einer
// Serie aufzählt.
type StaffelInfo struct {
	Nr     int
	Folgen int
	Datum  string // Erstausstrahlung der Staffel
	Name   string
}

// FolgeInfo ist eine Folge aus den Staffeldaten.
type FolgeInfo struct {
	Nr    int
	Datum string
}

// Die Staffeln einer Serie ändern sich selten; während eines Durchlaufs
// über 500 Folgen soll TMDb nicht 500-mal dieselbe Liste schicken.
var staffelSpeicher sync.Map // tmdb-Kennung → staffelEintrag

type staffelEintrag struct {
	geholt   time.Time
	staffeln []StaffelInfo
}

// Staffeln holt die Staffeln einer Serie, nach Nummer sortiert.
func (t *TMDb) Staffeln(ctx context.Context, seriesID string) ([]StaffelInfo, error) {
	if e, ok := staffelSpeicher.Load(seriesID); ok {
		if x := e.(staffelEintrag); time.Since(x.geholt) < time.Hour {
			return x.staffeln, nil
		}
	}
	var d struct {
		Seasons []struct {
			Number  int    `json:"season_number"`
			Count   int    `json:"episode_count"`
			AirDate string `json:"air_date"`
			Name    string `json:"name"`
		} `json:"seasons"`
	}
	if err := t.get(ctx, "/tv/"+url.PathEscape(seriesID), url.Values{"language": {t.Language}}, &d); err != nil {
		return nil, err
	}
	out := make([]StaffelInfo, 0, len(d.Seasons))
	for _, s := range d.Seasons {
		out = append(out, StaffelInfo{Nr: s.Number, Folgen: s.Count, Datum: s.AirDate, Name: s.Name})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Nr < out[j].Nr })
	staffelSpeicher.Store(seriesID, staffelEintrag{geholt: time.Now(), staffeln: out})
	return out, nil
}

// StaffelFolgen holt die Folgen einer Staffel mit ihrem Sendedatum.
func (t *TMDb) StaffelFolgen(ctx context.Context, seriesID string, staffel int) ([]FolgeInfo, error) {
	var d struct {
		Episodes []struct {
			Number  int    `json:"episode_number"`
			AirDate string `json:"air_date"`
		} `json:"episodes"`
	}
	path := fmt.Sprintf("/tv/%s/season/%d", url.PathEscape(seriesID), staffel)
	if err := t.get(ctx, path, url.Values{"language": {t.Language}}, &d); err != nil {
		return nil, err
	}
	out := make([]FolgeInfo, 0, len(d.Episodes))
	for _, e := range d.Episodes {
		out = append(out, FolgeInfo{Nr: e.Number, Datum: e.AirDate})
	}
	return out, nil
}

// FindTVDb sucht zu einer TVDb-Kennung die TMDb-Kennung einer Serie.
func (t *TMDb) FindTVDb(ctx context.Context, tvdbID string) ([]string, error) {
	_, serien, err := t.FindExtern(ctx, tvdbID, "tvdb_id")
	return serien, err
}

// AbsolutAbbilden rechnet eine durchgezählte Folgennummer in Staffel und
// Folge um: Die Folgen der Staffeln ab 1 werden aufaddiert, bis die
// Nummer erreicht ist. Specials (Staffel 0) zählen nicht mit — so zählen
// auch die Fansub-Gruppen.
func AbsolutAbbilden(staffeln []StaffelInfo, absolut int) (staffel, folge int, ok bool) {
	if absolut < 1 {
		return 0, 0, false
	}
	vorher := 0
	for _, s := range staffeln {
		if s.Nr < 1 || s.Folgen < 1 {
			continue
		}
		if absolut <= vorher+s.Folgen {
			return s.Nr, absolut - vorher, true
		}
		vorher += s.Folgen
	}
	return 0, 0, false
}

// absolutAufloesen findet die Folge zu einer durchgezählten Nummer. TMDb
// führt manche Anime auch selbst durchgezählt (Staffel 2 beginnt mit
// Folge 25) — darum wird, wenn die umgerechnete Nummer fehlt, auch die
// durchgehende in derselben Staffel versucht.
func (r *Resolver) absolutAufloesen(ctx context.Context, src string, absolut int) (*Candidate, int, int, error) {
	staffeln, err := r.tmdb.Staffeln(ctx, src)
	if err != nil {
		return nil, 0, 0, err
	}
	s, f, ok := AbsolutAbbilden(staffeln, absolut)
	if !ok {
		return nil, 0, 0, fmt.Errorf("bei TMDb nicht gefunden")
	}
	e, err := r.tmdb.Episode(ctx, src, s, f)
	if err == nil {
		return e, s, f, nil
	}
	if e2, err2 := r.tmdb.Episode(ctx, src, s, absolut); err2 == nil {
		return e2, s, absolut, nil
	}
	return nil, 0, 0, err
}

// datumAufloesen findet die Folge, die an einem Tag lief. Gesucht wird in
// den Staffeln, die in diesem Jahr oder kurz davor begannen (Late-Night-
// Sendungen zählen Staffeln oft nach Jahren, manche über den Jahreswechsel)
// — höchstens drei, damit eine Sendung mit 30 Staffeln TMDb nicht flutet.
func (r *Resolver) datumAufloesen(ctx context.Context, src, datum string) (*Candidate, int, int, error) {
	if len(datum) < 10 {
		return nil, 0, 0, fmt.Errorf("bei TMDb nicht gefunden")
	}
	jahr, _ := strconv.Atoi(datum[:4])
	staffeln, err := r.tmdb.Staffeln(ctx, src)
	if err != nil {
		return nil, 0, 0, err
	}
	var wahl []int
	for _, s := range staffeln {
		if s.Nr < 1 {
			continue
		}
		j := yearOf(s.Datum)
		if j == jahr || strings.Contains(s.Name, strconv.Itoa(jahr)) {
			wahl = append([]int{s.Nr}, wahl...)
		} else if j == jahr-1 {
			wahl = append(wahl, s.Nr)
		}
	}
	// Eine Sendung mit nur einer Staffel über viele Jahre (oder ohne
	// Daten an den Staffeln): dann eben die letzte, die vor dem Datum
	// begann.
	if len(wahl) == 0 {
		for i := len(staffeln) - 1; i >= 0; i-- {
			if s := staffeln[i]; s.Nr >= 1 && (s.Datum == "" || s.Datum <= datum) {
				wahl = append(wahl, s.Nr)
				break
			}
		}
	}
	if len(wahl) > 3 {
		wahl = wahl[:3]
	}
	for _, nr := range wahl {
		folgen, err := r.tmdb.StaffelFolgen(ctx, src, nr)
		if err != nil {
			continue
		}
		for _, f := range folgen {
			if f.Datum == datum {
				e, err := r.tmdb.Episode(ctx, src, nr, f.Nr)
				if err != nil {
					return nil, 0, 0, err
				}
				return e, nr, f.Nr, nil
			}
		}
	}
	return nil, 0, 0, fmt.Errorf("bei TMDb nicht gefunden")
}

// doppelfolgeErgaenzen holt zu einer Doppelfolge („S01E16E17") die
// weiteren Folgen und hängt Titel und Inhalt an: „Titel A / Titel B".
// Fehlt eine, bleibt es bei dem, was da ist.
func (r *Resolver) doppelfolgeErgaenzen(ctx context.Context, src string, staffel, von, bis int, e *Candidate) {
	if bis <= von || bis-von > 3 {
		return
	}
	titel, inhalt := []string{e.Title}, []string{e.Overview}
	for n := von + 1; n <= bis; n++ {
		x, err := r.tmdb.FolgeAusStaffel(ctx, src, staffel, n)
		if err != nil {
			continue
		}
		titel = append(titel, x.Title)
		inhalt = append(inhalt, x.Overview)
		e.RuntimeMS += x.RuntimeMS
	}
	e.Title = strings.Join(ohneLeere(titel), " / ")
	e.Overview = strings.Join(ohneLeere(inhalt), "\n\n")
}

func ohneLeere(xs []string) []string {
	out := xs[:0:0]
	for _, x := range xs {
		if strings.TrimSpace(x) != "" {
			out = append(out, x)
		}
	}
	return out
}
