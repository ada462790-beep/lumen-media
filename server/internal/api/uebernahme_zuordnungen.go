// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

// Zuordnungen aus Emby oder Jellyfin übernehmen — als Teil des Umzugs.
//
// Emby kennt zu jedem Film und jeder Serie die Kennungen bei TMDb, IMDb
// und TheTVDB, oft von Hand berichtigt („Identifizieren"). Lumen gleicht
// die Einträge über die Datei ab (uebernahme.ZuordnungenPlanen) und weist
// jeden gefundenen Titel zu wie von Hand: Er gilt danach als „von Hand
// zugewiesen", kein Durchlauf hängt ihn mehr um, Metadaten und Bilder
// kommen neu, bei Serien danach alle Folgen.
//
// Regeln:
//   - Nur Konten mit Verwaltungsrechten, nur Bibliotheken, die sie
//     verwalten (bei Mitverwaltern mit Auswahl nur diese).
//   - Titel, die in Lumen schon von Hand zugewiesen sind, bleiben — es
//     sei denn, „überschreiben" ist gewählt.
//   - Ins Protokoll kommt nur die Anzahl, kein Titel, kein Konto.

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"

	"lumen/internal/lokal"
	"lumen/internal/meta"
	"lumen/internal/store"
	"lumen/internal/uebernahme"
)

// zuordnungsAuftrag: was beim Start verlangt und erlaubt wurde.
type zuordnungsAuftrag struct {
	// bibliotheken, die das Konto verwaltet; nil = alle.
	bibliotheken map[string]bool
}

// zuordnungsVorschau: was das Übernehmen ändern würde.
type zuordnungsVorschau struct {
	Neu           int      `json:"neu"`
	SchonRichtig  int      `json:"schonRichtig"`
	Gesperrt      int      `json:"gesperrt"` // in Lumen von Hand zugewiesen
	NichtGefunden int      `json:"nichtGefunden"`
	OhneKennung   int      `json:"ohneKennung"`
	Fehlend       []string `json:"fehlend"`
	// OhneQuelle: In Lumen ist kein (gültiger) TMDb-Schlüssel — dann merkt
	// sich Lumen die Kennungen nur vor; Poster und Beschreibungen kommen,
	// sobald ein Schlüssel da ist. Schluessel sagt, warum (fehlt,
	// abgelehnt).
	OhneQuelle bool   `json:"ohneQuelle,omitempty"`
	Schluessel string `json:"schluessel,omitempty"`
}

// zuordnungsStand: wie weit das Übernehmen ist, und was es gebracht hat.
type zuordnungsStand struct {
	Laeuft       bool `json:"laeuft"`
	Gesamt       int  `json:"gesamt"`
	Fertig       int  `json:"fertig"`
	Uebernommen  int  `json:"uebernommen"`
	SchonRichtig int  `json:"schonRichtig"`
	Gesperrt     int  `json:"gesperrt"`
	Fehler       int  `json:"fehler"`
	// Vorgemerkt: ohne gültigen TMDb-Schlüssel gespeichert, wartet auf ihn.
	Vorgemerkt int `json:"vorgemerkt"`
	// Grund, warum nur vorgemerkt wurde: „fehlt" oder „abgelehnt".
	Grund string `json:"grund,omitempty"`
}

// zuordnungLaeuft: höchstens ein Übernehmen gleichzeitig auf dem Server.
var zuordnungLaeuft atomic.Bool

// zuordnungenPlanen gleicht die Emby-Einträge mit den Filmen und Serien
// der erlaubten Bibliotheken ab und zählt, was sich ändern würde.
func (s *Server) zuordnungenPlanen(ctx context.Context, emby []uebernahme.Eintrag, katalog []store.KatalogTitel,
	dateien []store.KatalogDatei, darf map[string]bool) (*zuordnungsVorschau, []uebernahme.Zuordnung, error) {
	titel := map[string]store.KatalogTitel{}
	orte := map[string]*uebernahme.LumenOrt{}
	serieVon := map[string]string{} // Folge → Serie
	for _, t := range katalog {
		switch t.Kind {
		case "movie", "series":
			if darf != nil && !darf[t.LibraryID] {
				continue
			}
			titel[t.ID] = t
			orte[t.ID] = &uebernahme.LumenOrt{ItemID: t.ID, Kind: t.Kind}
		case "episode":
			serieVon[t.ID] = t.SerieID
		}
	}
	for _, d := range dateien {
		id := d.ItemID
		if sid, ist := serieVon[id]; ist {
			id = sid
		}
		if o := orte[id]; o != nil {
			o.Pfade = append(o.Pfade, d.Pfad)
			o.Groessen = append(o.Groessen, d.Groesse)
		}
	}
	liste := make([]uebernahme.LumenOrt, 0, len(orte))
	for _, o := range orte {
		if len(o.Pfade) > 0 {
			liste = append(liste, *o)
		}
	}
	plan := uebernahme.ZuordnungenPlanen(emby, liste)
	zustand, err := s.st.ZuordnungsZustaende(ctx)
	if err != nil {
		return nil, nil, err
	}
	v := &zuordnungsVorschau{NichtGefunden: plan.NichtGefunden, OhneKennung: plan.OhneKennung,
		Fehlend: plan.Fehlend, OhneQuelle: !s.tmdbBereit(ctx)}
	if v.OhneQuelle {
		v.Schluessel = meta.SchluesselStand(ctx, s.st)
	}
	if v.Fehlend == nil {
		v.Fehlend = []string{}
	}
	for _, z := range plan.Zuordnungen {
		t := titel[z.ItemID]
		switch {
		case schonRichtig(t, zustand[z.ItemID], z):
			v.SchonRichtig++
		case zustand[z.ItemID] == "manual":
			v.Gesperrt++
		default:
			v.Neu++
		}
	}
	return v, plan.Zuordnungen, nil
}

// tmdbBereit: Es gibt eine Quelle mit einem Schlüssel, den TMDb (soweit
// bekannt) annimmt.
func (s *Server) tmdbBereit(ctx context.Context) bool {
	return s.meta != nil && s.meta.Ready() && meta.SchluesselStand(ctx, s.st) != "abgelehnt"
}

// schonRichtig: Der Titel ist in Lumen schon genau diesem Eintrag
// zugeordnet — erkannt, von Hand zugewiesen oder (aus der NFO, ohne
// Schlüssel) mit genau dieser Kennung gespeichert.
func schonRichtig(t store.KatalogTitel, zustand string, z uebernahme.Zuordnung) bool {
	switch zustand {
	case "matched", "manual", "lokal", "pending":
	default:
		return false
	}
	switch {
	case z.TMDb != "":
		return t.TMDb == z.TMDb
	case z.IMDb != "":
		return t.IMDb == z.IMDb
	case z.TVDb != "":
		return t.TVDb == z.TVDb
	}
	return false
}

// zuordnungenAnwenden weist die Titel nacheinander zu. Läuft im
// Hintergrund; der Stand steht im Lauf (GET /me/uebernahme).
func (s *Server) zuordnungenAnwenden(ctx context.Context, lauf *uebernahmeLauf, plan []uebernahme.Zuordnung,
	ueberschreiben bool, darf map[string]bool) {
	melden := func(f func(z *zuordnungsStand)) {
		lauf.setzen(func(l *uebernahmeLauf) {
			if l.ZuordnungStand == nil {
				l.ZuordnungStand = &zuordnungsStand{}
			}
			f(l.ZuordnungStand)
		})
	}
	if !zuordnungLaeuft.CompareAndSwap(false, true) {
		melden(func(z *zuordnungsStand) { z.Laeuft, z.Fehler = false, len(plan) })
		return
	}
	defer zuordnungLaeuft.Store(false)

	for _, z := range plan {
		if ctx.Err() != nil {
			break
		}
		ergebnis := s.zuordnungUebernehmen(ctx, z, ueberschreiben, darf)
		melden(func(st *zuordnungsStand) {
			st.Fertig++
			switch ergebnis {
			case "uebernommen":
				st.Uebernommen++
			case "schon":
				st.SchonRichtig++
			case "gesperrt":
				st.Gesperrt++
			case "vorgemerkt":
				st.Vorgemerkt++
				if st.Grund == "" {
					st.Grund = meta.SchluesselStand(ctx, s.st)
				}
			default:
				st.Fehler++
			}
		})
	}
	var erg zuordnungsStand
	melden(func(z *zuordnungsStand) { z.Laeuft = false; erg = *z })
	if erg.Vorgemerkt > 0 {
		s.st.Log(ctx, "info", "metadaten", fmt.Sprintf(
			"Zuordnungen aus Emby/Jellyfin: %d Kennungen ohne gültigen TMDb-Schlüssel vorgemerkt", erg.Vorgemerkt))
	}
	if erg.Uebernommen > 0 {
		s.st.Log(ctx, "info", "metadaten", fmt.Sprintf(
			"Zuordnungen aus Emby/Jellyfin: %d übernommen, %d schon richtig, %d von Hand belassen, %d Fehler",
			erg.Uebernommen, erg.SchonRichtig, erg.Gesperrt, erg.Fehler))
		// Die Folgen der neu zugeordneten Serien und die Filmreihen.
		s.Nacheinander(ctx, s.ReihenBilden)
		s.metadatenImHintergrund("")
	}
}

// zuordnungUebernehmen weist einen Titel zu. Ergebnis: uebernommen |
// schon | gesperrt | vorgemerkt | fehler.
func (s *Server) zuordnungUebernehmen(ctx context.Context, z uebernahme.Zuordnung, ueberschreiben bool, darf map[string]bool) string {
	it, err := s.st.Item(ctx, z.ItemID)
	if err != nil || (darf != nil && !darf[it.LibraryID]) || it.Kind != z.Kind {
		return "fehler"
	}
	aktuell := store.KatalogTitel{IMDb: it.IMDbID}
	aktuell.TMDb, _ = s.st.SourceID(ctx, it.ID, "tmdb")
	aktuell.TVDb, _ = s.st.SourceID(ctx, it.ID, "tvdb")
	if schonRichtig(aktuell, it.MatchState, z) {
		return "schon"
	}
	if it.MatchState == "manual" && !ueberschreiben {
		return "gesperrt"
	}
	vormerken := func() string {
		if meta.KennungenVormerken(ctx, s.st, it.ID, lokal.Kennungen{TMDb: z.TMDb, IMDb: z.IMDb, TVDb: z.TVDb}) != nil {
			return "fehler"
		}
		return "vorgemerkt"
	}
	if !s.tmdbBereit(ctx) {
		return vormerken()
	}
	// Die TMDb-Kennung: direkt, sonst über IMDb oder TVDb (TMDb /find).
	var kandidaten []meta.Eingabe
	if z.TMDb != "" {
		kandidaten = append(kandidaten, meta.Eingabe{Quelle: "tmdb", ID: z.TMDb})
	}
	if z.IMDb != "" {
		kandidaten = append(kandidaten, meta.Eingabe{Quelle: "imdb", ID: z.IMDb})
	}
	if z.TVDb != "" {
		kandidaten = append(kandidaten, meta.Eingabe{Quelle: "tvdb", ID: z.TVDb})
	}
	for _, e := range kandidaten {
		id, err := s.meta.TMDbKennungZu(ctx, it.Kind, e)
		if errors.Is(err, meta.ErrTMDbSchluessel) {
			meta.SchluesselAbgelehntMerken(ctx, s.st)
			return vormerken()
		}
		if err != nil {
			continue
		}
		var zerr error
		s.Nacheinander(ctx, func(ctx context.Context) {
			zerr = s.meta.ZuweisenOhneFolgen(ctx, it, id)
		})
		if zerr == nil {
			return "uebernommen"
		}
		if errors.Is(zerr, meta.ErrTMDbSchluessel) {
			meta.SchluesselAbgelehntMerken(ctx, s.st)
			return vormerken()
		}
		if errors.Is(zerr, context.Canceled) {
			break
		}
		// Eine veraltete TMDb-Kennung (bei TMDb gelöscht): die nächste
		// Kennung versuchen.
	}
	return "fehler"
}
