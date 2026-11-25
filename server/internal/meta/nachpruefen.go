// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package meta

// Nachprüfen: „Metadaten neu laden" für einen Film, den Lumen schon
// einmal selbst zugeordnet hat.
//
// Nach dem Zuordnen trägt der Film den Titel von TMDb. Lud man die
// Metadaten neu, suchte Lumen mit genau diesem Titel — und fand natürlich
// wieder denselben Eintrag, auch wenn der falsch war: Aus „The Animatrix
// - a.m2ts" einmal „Der Letzte Flug der Osiris" geworden, suchte jeder
// weitere Lauf nach „Der Letzte Flug der Osiris". Der Fehler bestätigte
// sich selbst.
//
// Darum sucht Lumen beim Neuladen eines selbst zugeordneten Films mit
// dem Titel, den die Dateien nach ihrem Namen haben. Unangetastet bleibt,
// was jemand bewusst entschieden hat:
//
//   - von Hand zugewiesen oder aus Emby/Jellyfin übernommen („manual"):
//     Neuladen frischt nur die Angaben dieses Eintrags auf (NeuLaden);
//   - ein von Hand geänderter Titel (Schloss am Titel);
//   - eine NFO neben der Datei (sie bestimmt Titel oder Kennung);
//   - Dateien mit verschiedenen Namen (Sprachfassungen) — welcher Name
//     gälte, wäre geraten.
//
// Ist die Suche nach dem Dateinamen nicht sicher, gilt die Suche mit dem
// bisherigen Titel wie bisher: Ein erkannter Film wird durch das
// Neuladen nie „nicht erkannt".

import (
	"context"
	"path/filepath"
	"strings"

	"lumen/internal/store"
)

// DateititelLesen liefert Titel und Jahr, die eine Datei nach ihrem Namen
// hätte — dieselbe Lesart wie beim Durchlauf, samt Teilen („Film - a",
// „Film - b"). Der Scanner trägt sie ein (scan kennt meta, nicht
// umgekehrt); ohne bleibt es beim gespeicherten Titel.
var DateititelLesen func(lib store.Library, pfad string) (titel string, jahr int)

// nachDateiname: Titel und Jahr, unter denen ein schon einmal selbst
// zugeordneter Film neu gesucht wird — ok=false, wenn es beim
// gespeicherten Titel bleibt.
func (r *Resolver) nachDateiname(ctx context.Context, it *store.Item) (string, int, bool) {
	lesen := DateititelLesen
	if lesen == nil || it.Kind != "movie" || it.MatchState == "manual" {
		return "", 0, false
	}
	// Nur, was Lumen schon einmal zugeordnet hat: Ein neuer Titel trägt
	// ohnehin den Namen aus der Datei.
	if id, _ := r.st.SourceID(ctx, it.ID, "tmdb"); id == "" {
		return "", 0, false
	}
	if r.st.SperreGesetzt(ctx, it.ID, store.FeldTitel) {
		return "", 0, false
	}
	if _, ok := r.st.LokaleNFOZu(ctx, it.ID); ok {
		return "", 0, false
	}
	var lib *store.Library
	if libs, err := r.st.Libraries(ctx); err == nil {
		for i := range libs {
			if libs[i].ID == it.LibraryID {
				lib = &libs[i]
				break
			}
		}
	}
	if lib == nil {
		return "", 0, false
	}
	dateien, err := r.st.FilesFor(ctx, it.ID)
	if err != nil || len(dateien) == 0 {
		return "", 0, false
	}
	titel, jahr := "", 0
	for _, f := range dateien {
		if rel, err := filepath.Rel(lib.Path, f.Path); err != nil || strings.HasPrefix(rel, "..") {
			return "", 0, false
		}
		t, j := lesen(*lib, f.Path)
		t = strings.TrimSpace(t)
		if t == "" {
			return "", 0, false
		}
		if titel == "" {
			titel, jahr = t, j
			continue
		}
		if !strings.EqualFold(normalize(t), normalize(titel)) {
			return "", 0, false // Sprachfassungen o. ä.: kein eindeutiger Name
		}
		if jahr == 0 {
			jahr = j
		}
	}
	if strings.EqualFold(normalize(titel), normalize(it.Title)) && (jahr == 0 || jahr == it.Year) {
		return "", 0, false // derselbe Titel: nichts gewonnen
	}
	return titel, jahr, true
}

// filmFinden sucht einen Film: bei einem schon selbst zugeordneten Film
// zuerst nach dem Dateinamen (nachDateiname), sonst und als Rückfall mit
// Titel und Jahr des Eintrags.
func (r *Resolver) filmFinden(ctx context.Context, it *store.Item) (Match, Gesucht, error) {
	if t, j, ok := r.nachDateiname(ctx, it); ok {
		m, g, _, err := r.finden(ctx, "movie", t, j, "")
		if err != nil {
			return m, g, err
		}
		if m.Sure {
			return m, g, nil
		}
	}
	m, g, _, err := r.finden(ctx, "movie", it.Title, it.Year, "")
	return m, g, err
}
