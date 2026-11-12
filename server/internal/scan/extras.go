// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package scan

// Extras eines Films: Trailer, Featurettes, Interviews … wie Emby, Plex
// und Kodi sie ablegen — in Ordnern neben dem Film („trailers/",
// „behind the scenes/") oder mit einer Endung hinter dem Namen
// („Inception (2010)-trailer.mkv"). Der Durchlauf nimmt sie bewusst nicht
// als Filme auf; die Titelseite bietet sie hier zum Abspielen an.
//
// Nichts davon steht in der Datenbank: Die Liste entsteht beim Aufruf aus
// dem Ordner des Films. Das hält sie aktuell und kostet einen Blick in
// einen einzelnen Ordner.

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Extra ist eine Datei, die zu einem Film gehört, aber keiner ist.
type Extra struct {
	Pfad  string
	Titel string // leer: heisst wie der Film (dann zählt die Art)
	Art   string // trailer | featurette | behindthescenes | deleted | interview | scene | short | clip | other
}

// extraOrdnerArt: welche Art ein Extras-Ordner enthält. Samples und
// „Specials" (Staffel 0 einer Serie) zählen nicht.
func extraOrdnerArt(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "trailers", "trailer":
		return "trailer"
	case "featurettes", "featurette":
		return "featurette"
	case "behind the scenes", "behindthescenes":
		return "behindthescenes"
	case "deleted scenes", "deleted scene", "deleted":
		return "deleted"
	case "interviews", "interview":
		return "interview"
	case "scenes", "scene":
		return "scene"
	case "shorts", "short":
		return "short"
	case "clips", "clip":
		return "clip"
	case "extras", "extra", "other", "others", "bonus":
		return "other"
	}
	return ""
}

// extraEndungArt: „…-trailer" → trailer. "" heisst: kein Extra (oder ein
// Sample, das niemand ansehen will).
func extraEndungArt(stem string) (vor, art string) {
	s := strings.ToLower(strings.TrimSpace(stem))
	for _, e := range []struct{ endung, art string }{
		{"trailer", "trailer"}, {"featurette", "featurette"}, {"behindthescenes", "behindthescenes"},
		{"deletedscenes", "deleted"}, {"deletedscene", "deleted"}, {"deleted", "deleted"},
		{"interview", "interview"}, {"scene", "scene"}, {"short", "short"}, {"clip", "clip"},
		{"extra", "other"}, {"other", "other"},
	} {
		if !strings.HasSuffix(s, e.endung) {
			continue
		}
		v := strings.TrimRight(stem[:len(s)-len(e.endung)], " ")
		if strings.HasSuffix(v, "-") {
			return strings.TrimSpace(strings.TrimSuffix(v, "-")), e.art
		}
	}
	return "", ""
}

// ExtrasZu sucht die Extras zu den Dateien eines Films. Ordner-Extras
// gelten nur, wenn der Film einen eigenen Ordner hat (nicht direkt in
// der Bibliothek liegt); Endungs-Extras in der Bibliothek selbst nur,
// wenn der Name vor der Endung der des Films ist.
func ExtrasZu(dateien []string, bibliothek string) []Extra {
	var out []Extra
	gesehen := map[string]bool{}
	ordner := map[string]bool{}
	staemme := map[string]bool{}
	for _, d := range dateien {
		ordner[filepath.Dir(d)] = true
		staemme[strings.ToLower(strings.TrimSuffix(filepath.Base(d), filepath.Ext(d)))] = true
	}
	add := func(pfad, art string) {
		if gesehen[pfad] || len(out) >= 100 {
			return
		}
		gesehen[pfad] = true
		stem := strings.TrimSuffix(filepath.Base(pfad), filepath.Ext(pfad))
		titel := ""
		if v, _ := extraEndungArt(stem); v != "" {
			// „Inception (2010)-trailer": Der Name ist der des Films — als
			// Titel sagt er nichts; die Titelseite zeigt dann die Art.
			if !staemme[strings.ToLower(v)] {
				titel = clean(v)
			}
		} else if titel = clean(stem); titel == "" {
			titel = stem
		}
		out = append(out, Extra{Pfad: pfad, Titel: titel, Art: art})
	}
	wurzel := filepath.Clean(bibliothek)
	for dir := range ordner {
		eigen := filepath.Clean(dir) != wurzel
		eintraege, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range eintraege {
			name := e.Name()
			if strings.HasPrefix(name, ".") {
				continue
			}
			if e.IsDir() {
				art := extraOrdnerArt(name)
				if art == "" || !eigen {
					continue
				}
				unter, err := os.ReadDir(filepath.Join(dir, name))
				if err != nil {
					continue
				}
				for _, u := range unter {
					if u.Type().IsRegular() && IsVideo(u.Name()) && !strings.HasPrefix(u.Name(), ".") {
						add(filepath.Join(dir, name, u.Name()), art)
					}
				}
				continue
			}
			if !e.Type().IsRegular() || !IsVideo(name) {
				continue
			}
			vor, art := extraEndungArt(strings.TrimSuffix(name, filepath.Ext(name)))
			if art == "" {
				continue
			}
			if !eigen && !staemme[strings.ToLower(vor)] {
				continue
			}
			add(filepath.Join(dir, name), art)
		}
	}
	reihe := map[string]int{"trailer": 0, "featurette": 1, "behindthescenes": 2, "interview": 3,
		"deleted": 4, "scene": 5, "clip": 6, "short": 7, "other": 8}
	sort.SliceStable(out, func(i, j int) bool {
		if reihe[out[i].Art] != reihe[out[j].Art] {
			return reihe[out[i].Art] < reihe[out[j].Art]
		}
		return strings.ToLower(out[i].Titel) < strings.ToLower(out[j].Titel)
	})
	return out
}
