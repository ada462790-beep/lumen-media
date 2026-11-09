// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package uebernahme

// Zuordnung: Welcher Lumen-Titel ist der Emby-Titel?
//
// Die Reihenfolge geht vom Sichersten zum Unsichersten:
//
//  1. Kennungen von TMDb, IMDb, TVDb — stimmen sie, ist es derselbe Titel.
//  2. Die Datei: gleicher Dateiname und gleiche Grösse. Der Pfad selbst
//     taugt nicht, denn Emby und Lumen sehen dieselbe Platte meist unter
//     verschiedenen Pfaden (/mnt/media gegen /volume1/video).
//  3. Titel und Jahr, wenn genau ein Lumen-Titel passt.
//
// Folgen: zuerst über die Datei, sonst über die Serie (wie oben) und
// dann Staffel- und Folgennummer.

import (
	"path"
	"strconv"
	"strings"
	"unicode"
)

// LumenTitel ist ein Film, eine Serie oder eine Folge aus Lumen.
type LumenTitel struct {
	ID       string
	Kind     string // movie | series | episode
	Titel    string
	Original string
	Jahr     int
	TMDb     string
	IMDb     string
	TVDb     string
	// Nur Folgen:
	SerieID string
	Staffel int
	Folge   int
	// Laufzeit in ms, für den Fortschrittsbalken.
	LaengeMS int64
}

// LumenDatei ist eine Datei aus Lumen.
type LumenDatei struct {
	ItemID  string
	Pfad    string
	Groesse int64
}

// Index hält die Lumen-Seite für die Zuordnung bereit.
type Index struct {
	titel       map[string]*LumenTitel
	nachKennung map[string]string   // "movie|tmdb|603" → ID
	nachDatei   map[string][]string // "name.mkv" → Kennungen mit Grösse "ID|grösse"
	nachName    map[string][]string // "movie|matrix" → IDs
	nachFolge   map[string]string   // "serie|staffel|folge" → ID
}

// NeuerIndex baut den Index. Nur Titel, die die Person sehen darf, gehören
// hinein — was sie nicht sehen darf, gilt als „nicht gefunden".
func NeuerIndex(titel []LumenTitel, dateien []LumenDatei) *Index {
	ix := &Index{titel: map[string]*LumenTitel{}, nachKennung: map[string]string{},
		nachDatei: map[string][]string{}, nachName: map[string][]string{}, nachFolge: map[string]string{}}
	for i := range titel {
		t := &titel[i]
		ix.titel[t.ID] = t
		if t.Kind == "episode" {
			ix.nachFolge[folgenSchluessel(t.SerieID, t.Staffel, t.Folge)] = t.ID
			continue
		}
		for quelle, v := range map[string]string{"tmdb": t.TMDb, "imdb": t.IMDb, "tvdb": t.TVDb} {
			if v != "" {
				ix.nachKennung[t.Kind+"|"+quelle+"|"+strings.ToLower(v)] = t.ID
			}
		}
		for _, n := range []string{t.Titel, t.Original} {
			if k := normal(n); k != "" {
				schl := t.Kind + "|" + k
				if !enthaelt(ix.nachName[schl], t.ID) {
					ix.nachName[schl] = append(ix.nachName[schl], t.ID)
				}
			}
		}
	}
	for _, d := range dateien {
		if _, da := ix.titel[d.ItemID]; !da {
			continue
		}
		n := dateiName(d.Pfad)
		ix.nachDatei[n] = append(ix.nachDatei[n], d.ItemID+"|"+itoa(d.Groesse))
	}
	return ix
}

// Titel gibt einen Lumen-Titel aus dem Index.
func (ix *Index) Titel(id string) *LumenTitel { return ix.titel[id] }

// Zuordnen findet den Lumen-Titel zu einem Emby-Eintrag. wie sagt, woran
// es erkannt wurde ("kennung", "datei", "name", "folge"); "" heisst:
// nicht gefunden.
func (ix *Index) Zuordnen(e Eintrag, serien map[string]Eintrag) (id, wie string) {
	switch e.Typ {
	case "Movie":
		return ix.titelFinden("movie", e)
	case "Series":
		return ix.titelFinden("series", e)
	case "Episode":
		if id := ix.dateiFinden(e, "episode"); id != "" {
			return id, "datei"
		}
		serie, da := serien[e.SerieID]
		if !da {
			serie = Eintrag{Typ: "Series", Name: e.SerieName}
		}
		sid, _ := ix.titelFinden("series", serie)
		if sid == "" {
			return "", ""
		}
		if id := ix.nachFolge[folgenSchluessel(sid, e.Staffel, e.Folge)]; id != "" {
			return id, "folge"
		}
	}
	return "", ""
}

func (ix *Index) titelFinden(kind string, e Eintrag) (string, string) {
	for _, quelle := range []string{"tmdb", "imdb", "tvdb"} {
		if v := e.Kennungen[quelle]; v != "" {
			if id := ix.nachKennung[kind+"|"+quelle+"|"+strings.ToLower(v)]; id != "" {
				return id, "kennung"
			}
		}
	}
	if kind == "movie" {
		if id := ix.dateiFinden(e, kind); id != "" {
			return id, "datei"
		}
	}
	// Titel und Jahr. Mehrere gleichnamige Titel ohne passendes Jahr sind
	// zu unsicher — dann lieber „nicht gefunden".
	kandidaten := ix.nachName[kind+"|"+normal(e.Name)]
	var passend []string
	for _, id := range kandidaten {
		t := ix.titel[id]
		if e.Jahr == 0 || t.Jahr == 0 || absInt(t.Jahr-e.Jahr) <= 1 {
			passend = append(passend, id)
		}
	}
	if len(passend) > 1 && e.Jahr > 0 {
		var genau []string
		for _, id := range passend {
			if ix.titel[id].Jahr == e.Jahr {
				genau = append(genau, id)
			}
		}
		passend = genau
	}
	if len(passend) == 1 {
		return passend[0], "name"
	}
	return "", ""
}

// dateiFinden: gleicher Dateiname, gleiche Grösse. Kennt Emby die Grösse
// nicht, reicht der Name — aber nur, wenn es ihn genau einmal gibt.
func (ix *Index) dateiFinden(e Eintrag, kind string) string {
	if e.Pfad == "" {
		return ""
	}
	treffer := ix.nachDatei[dateiName(e.Pfad)]
	var ohneGroesse []string
	for _, x := range treffer {
		id, groesse, _ := strings.Cut(x, "|")
		if ix.titel[id] == nil || ix.titel[id].Kind != kind {
			continue
		}
		if e.Groesse > 0 && groesse == itoa(e.Groesse) {
			return id
		}
		ohneGroesse = append(ohneGroesse, id)
	}
	if e.Groesse == 0 && len(ohneGroesse) == 1 {
		return ohneGroesse[0]
	}
	return ""
}

// dateiName nimmt den letzten Teil eines Pfads — mit / oder \, denn ein
// Emby unter Windows schreibt Pfade anders.
func dateiName(p string) string {
	p = strings.ReplaceAll(p, `\`, "/")
	return strings.ToLower(path.Base(p))
}

func folgenSchluessel(serie string, staffel, folge int) string {
	return serie + "|" + itoa(int64(staffel)) + "|" + itoa(int64(folge))
}

// normal macht Titel vergleichbar: klein, ohne Akzente, nur Buchstaben und
// Ziffern. „Amélie" = „amelie", „Star Wars: Episode IV" = „starwarsepisodeiv".
func normal(s string) string {
	s = strings.ToLower(s)
	s = strings.NewReplacer("ä", "ae", "ö", "oe", "ü", "ue", "ß", "ss", "&", "und",
		"é", "e", "è", "e", "ê", "e", "à", "a", "á", "a", "â", "a", "ó", "o", "ò", "o",
		"ô", "o", "í", "i", "ì", "i", "ú", "u", "ù", "u", "ñ", "n", "ç", "c").Replace(s)
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func enthaelt(l []string, x string) bool {
	for _, y := range l {
		if y == x {
			return true
		}
	}
	return false
}

func absInt(i int) int {
	if i < 0 {
		return -i
	}
	return i
}

func itoa(i int64) string { return strconv.FormatInt(i, 10) }
