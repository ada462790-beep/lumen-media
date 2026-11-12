// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package meta

// Ob TMDb überhaupt helfen kann. Viele, die von Emby oder Jellyfin
// kommen, haben keinen TMDb-Schlüssel — ihre Titel sind mit NFO-Dateien
// und Bildern aber längst beschrieben. Ohne Schlüssel (oder mit einem,
// den TMDb ablehnt) wartet darum kein Titel mit vollständiger NFO auf
// eine Zuordnung, die nie kommt. Die Kennungen bleiben gespeichert; kommt
// ein gültiger Schlüssel dazu, holt die Zuordnung diese Titel nach.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"lumen/internal/lokal"
	"lumen/internal/store"
)

const (
	// EinstellungTMDbSchluessel: der Schlüssel selbst (die Verwaltung
	// schreibt ihn).
	EinstellungTMDbSchluessel = "meta.tmdb_key"
	// EinstellungTMDbAbgelehnt: „1", seit TMDb den Schlüssel mit 401
	// abgelehnt hat. Ein neuer Schlüssel oder eine angenommene Anfrage
	// löscht das wieder.
	EinstellungTMDbAbgelehnt = "meta.tmdb_abgelehnt"
	// einstellungNachholen: „1", solange Titel mit Kennung, die ohne
	// Schlüssel als „lokal" galten, noch nicht nachgeholt wurden.
	einstellungNachholen = "meta.lokal_nachholen"
)

// SchluesselStand sagt, wie es um den TMDb-Schlüssel steht: "fehlt",
// "abgelehnt" oder "ok".
func SchluesselStand(ctx context.Context, st *store.Store) string {
	switch {
	case st.Setting(ctx, EinstellungTMDbSchluessel, "") == "":
		return "fehlt"
	case st.Setting(ctx, EinstellungTMDbAbgelehnt, "") == "1":
		return "abgelehnt"
	}
	return "ok"
}

// QuelleBereit: Es gibt einen Schlüssel, und TMDb hat ihn (soweit
// bekannt) nicht abgelehnt.
func QuelleBereit(ctx context.Context, st *store.Store) bool {
	return SchluesselStand(ctx, st) == "ok"
}

// schluesselAbgelehnt merkt sich ein 401 von TMDb — einmal, nicht je Titel.
// Gibt true zurück, wenn es neu ist (dann gehört es einmal ins Protokoll).
func schluesselAbgelehnt(ctx context.Context, st *store.Store) bool {
	if st.Setting(ctx, EinstellungTMDbAbgelehnt, "") == "1" {
		return false
	}
	_ = st.SetSetting(ctx, EinstellungTMDbAbgelehnt, "1")
	_ = st.SetSetting(ctx, einstellungNachholen, "1")
	return true
}

// SchluesselAngenommen: TMDb hat eine Anfrage mit dem Schlüssel
// beantwortet (oder die Verwaltung hat einen neuen geprüft).
func SchluesselAngenommen(ctx context.Context, st *store.Store) {
	if st.Setting(ctx, EinstellungTMDbAbgelehnt, "") != "" {
		_ = st.SetSetting(ctx, EinstellungTMDbAbgelehnt, "")
	}
}

// OhneSchluesselErkannt merkt sich, dass Titel ohne Schlüssel als „lokal"
// erkannt wurden — ein späterer Schlüssel soll sie nachholen.
func OhneSchluesselErkannt(ctx context.Context, st *store.Store) {
	if st.Setting(ctx, einstellungNachholen, "") != "1" {
		_ = st.SetSetting(ctx, einstellungNachholen, "1")
	}
}

// OhneQuelleAbschliessen setzt Titel, die nur noch auf TMDb warten, auf
// „lokal", wenn ihre NFO sie vollständig beschreibt — ohne Schlüssel (oder
// mit einem abgelehnten) kommt diese Zuordnung sonst nie. Folgen zählen
// mit, wenn ihre eigene NFO vollständig ist. Gibt die Anzahl zurück.
func OhneQuelleAbschliessen(ctx context.Context, st *store.Store) int {
	zeilen, err := st.AusstehendMitNFO(ctx)
	if err != nil {
		return 0
	}
	var ids []string
	for _, z := range zeilen {
		var n lokal.NFO
		if json.Unmarshal([]byte(z.Daten), &n) != nil || !n.Vollstaendig() {
			continue
		}
		if z.Kind == "episode" || n.HatKennung() {
			ids = append(ids, z.ID)
		}
	}
	if len(ids) == 0 {
		return 0
	}
	k, err := st.ZuordnungsZustandSetzen(ctx, ids, "lokal")
	if err != nil {
		return 0
	}
	OhneSchluesselErkannt(ctx, st)
	return int(k)
}

// ---------------------------------------------------------------- vorgemerkte Kennungen

// einstellungVorgemerkt: Titel, deren Kennung jemand ohne gültigen
// Schlüssel eingegeben (oder aus Emby übernommen) hat. Die Zuordnung holt
// sie später genau über diese Kennung statt über die Suche. Eine Liste in
// den Einstellungen statt einer neuen Spalte: Es sind wenige, und sie
// verschwinden, sobald sie geholt sind.
const einstellungVorgemerkt = "meta.kennung_vorgemerkt"

func vorgemerkt(ctx context.Context, st *store.Store) map[string]bool {
	out := map[string]bool{}
	_ = json.Unmarshal([]byte(st.Setting(ctx, einstellungVorgemerkt, "{}")), &out)
	return out
}

func vormerkenSetzen(ctx context.Context, st *store.Store, m map[string]bool) {
	b, _ := json.Marshal(m)
	_ = st.SetSetting(ctx, einstellungVorgemerkt, string(b))
}

// VorgemerktZahl: wie viele Titel mit Kennung auf einen Schlüssel warten.
func VorgemerktZahl(ctx context.Context, st *store.Store) int { return len(vorgemerkt(ctx, st)) }

// KennungenVormerken speichert IMDb-, TMDb- und TVDb-Kennung eines Films
// oder einer Serie, ohne TMDb zu fragen: Die Links auf der Titelseite und
// der Umzug funktionieren sofort, Poster und Beschreibungen kommen, wenn
// ein gültiger Schlüssel da ist. Der Titel gilt bis dahin als „lokal"
// (er wartet nicht in „Aufräumen"); von Hand oder von TMDb erkannte
// Titel behalten ihren Zustand.
func KennungenVormerken(ctx context.Context, st *store.Store, itemID string, k lokal.Kennungen) error {
	it, err := st.Item(ctx, itemID)
	if err != nil {
		return err
	}
	if it.Kind != "movie" && it.Kind != "series" {
		return errors.New("kennungen gibt es nur für filme und serien")
	}
	if k.TMDb != "" {
		if err := st.SetSourceID(ctx, it.ID, "tmdb", k.TMDb); err != nil {
			return err
		}
	}
	if k.TVDb != "" {
		if err := st.SetSourceID(ctx, it.ID, "tvdb", k.TVDb); err != nil {
			return err
		}
	}
	if k.IMDb != "" {
		if err := st.SetSourceID(ctx, it.ID, "imdb", k.IMDb); err != nil {
			return err
		}
	}
	if !store.IstErkannt(it.MatchState) || it.MatchState == "lokal" {
		if _, err := st.ZuordnungsZustandSetzen(ctx, []string{it.ID}, "lokal"); err != nil {
			return err
		}
		m := vorgemerkt(ctx, st)
		m[it.ID] = true
		vormerkenSetzen(ctx, st, m)
		OhneSchluesselErkannt(ctx, st)
	}
	return nil
}

// vorgemerkteKennung: die gespeicherten Kennungen eines vorgemerkten
// Titels — sonst nichts (dann wird wie gewohnt gesucht).
func (r *Resolver) vorgemerkteKennung(ctx context.Context, it *store.Item, kind string) (*Candidate, string) {
	if !vorgemerkt(ctx, r.st)[it.ID] {
		return nil, ""
	}
	k := lokal.Kennungen{IMDb: it.IMDbID}
	k.TMDb, _ = r.st.SourceID(ctx, it.ID, "tmdb")
	k.TVDb, _ = r.st.SourceID(ctx, it.ID, "tvdb")
	if k == (lokal.Kennungen{}) {
		return nil, ""
	}
	return r.kennungenHolen(ctx, kind, k)
}

// vormerkungErledigt: Der Titel ist zugeordnet.
func (r *Resolver) vormerkungErledigt(ctx context.Context, itemID string) {
	m := vorgemerkt(ctx, r.st)
	if m[itemID] {
		delete(m, itemID)
		vormerkenSetzen(ctx, r.st, m)
	}
}

// nachholen: Ein Schlüssel wird angenommen. Was ohne ihn als „lokal" galt
// und eine Kennung hat, kommt einmal dran.
func (r *Resolver) nachholen(ctx context.Context) {
	if r.st.Setting(ctx, einstellungNachholen, "") != "1" {
		return
	}
	n, err := r.st.LokaleMitKennungNachholen(ctx)
	if err != nil {
		return
	}
	_ = r.st.SetSetting(ctx, einstellungNachholen, "")
	if n > 0 {
		r.st.Log(ctx, "info", "metadaten", fmt.Sprintf(
			"%d Titel mit Kennung holt Lumen jetzt mit dem TMDb-Schlüssel nach", n))
	}
}

// schluesselPruefen fragt TMDb vor einem Durchgang einmal, ob der Schlüssel
// gilt. Lehnt TMDb ihn ab, steht das einmal im Protokoll — nicht einmal
// je Titel —, und die Titel mit vollständiger NFO gelten als erkannt.
func (r *Resolver) schluesselPruefen(ctx context.Context) error {
	err := r.TMDbPruefen(ctx)
	if errors.Is(err, ErrTMDbSchluessel) {
		r.schluesselFehlt(ctx)
		return err
	}
	SchluesselAngenommen(ctx, r.st)
	r.nachholen(ctx)
	return nil
}

// schluesselFehlt: TMDb hat den Schlüssel abgelehnt.
func (r *Resolver) schluesselFehlt(ctx context.Context) {
	if schluesselAbgelehnt(ctx, r.st) {
		r.st.Log(ctx, "warn", "metadaten",
			"TMDb lehnt den Schlüssel ab — bitte unter „Poster und Beschreibungen“ den langen API Read Access Token eintragen")
	}
	OhneQuelleAbschliessen(ctx, r.st)
}
