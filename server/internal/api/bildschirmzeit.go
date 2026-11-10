// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

// Bildschirmzeit für Kinderprofile — durchgesetzt hier, nicht in den Apps.
//
// Die Regel (Zeitfenster und Tagesgrenze je Tagesart) steht in
// store/bildschirmzeit.go. Hier passiert dreierlei:
//
//  1. Jeder Einstieg in ein Video — Plan, Direktwiedergabe, HLS,
//     Browser-Strom, Herunterladen — fragt vorher, ob jetzt geschaut
//     werden darf. Wenn nicht: 403 mit dem Code „bildschirmzeit", einem
//     freundlichen Satz („Für heute ist Schluss — morgen ab 07:00 geht es
//     weiter.") und „weiterAb" als Zeitpunkt.
//  2. Der Wächter zählt alle 15 Sekunden, welche Kinderprofile gerade
//     etwas laufen haben (Segmente oder Daten abgeholt, oder ein
//     Fortschritt, der sich bewegt hat). Jede Minute zählt je Profil
//     einmal, egal auf wie vielen Geräten — Bildschirmzeit ist Uhrzeit.
//  3. Ist die Zeit um, während etwas läuft, endet die Wiedergabe nach einer
//     kurzen Nachfrist. Die nächste Anfrage des Geräts (Segment,
//     Bereichsanfrage, /browser/grund) bekommt dieselbe Meldung.
//
// Gezählt wird nur, was über diese Wege läuft: Video.
// Was offline von einem Download läuft, sieht der Server nicht —
// Herunterladen ist für Kinderprofile ab Werk aus, und ausserhalb der Zeit
// lässt sich auch kein Download beginnen.

import (
	"context"
	"math"
	"net/http"
	"sync"
	"time"

	"lumen/internal/store"
	"lumen/internal/stream"
)

const (
	// bzTakt: so oft sieht der Wächter nach.
	bzTakt = 15 * time.Second
	// bzAktiv: so lange gilt eine Wiedergabe nach ihrer letzten Meldung
	// als laufend. Ein Gerät holt alle paar Sekunden ein Segment, ein
	// direkter Strom meldet sich alle zehn Sekunden (meldeAbstand).
	bzAktiv = 70 * time.Second
	// bzNachfrist: so lange läuft eine Wiedergabe über das Ende hinaus —
	// genug, damit eine Szene zu Ende gehen kann, ohne dass es sich lohnt.
	bzNachfrist = time.Minute
)

// bzWaechter hält, was der Wächter zwischen zwei Takten braucht. Alles
// nur im Arbeitsspeicher: Konto-Kennungen und Zeiten, kein Titel.
type bzWaechter struct {
	mu sync.Mutex
	// gezaehlt: die letzte gezählte Minute je Konto (Unix-Minute).
	gezaehlt map[string]int64
	// bewegt: wann sich ein Fortschritt dieses Kontos zuletzt bewegt hat.
	bewegt map[string]time.Time
	// stand: letzte gemeldete Stelle je Konto und Titel — nur um zu sehen,
	// ob sich etwas bewegt. Wird beim Aufräumen geleert.
	stand map[string]int64
	// ueberSeit: seit wann ein Konto über der Zeit ist, während noch etwas
	// läuft.
	ueberSeit  map[string]time.Time
	letzterTag string
	// nachfrist: bzNachfrist; Tests setzen sie auf null.
	nachfrist time.Duration
}

func (b *bzWaechter) init() {
	if b.gezaehlt == nil {
		b.gezaehlt = map[string]int64{}
		b.bewegt = map[string]time.Time{}
		b.stand = map[string]int64{}
		b.ueberSeit = map[string]time.Time{}
		b.nachfrist = bzNachfrist
	}
}

// fortschritt merkt sich einen Fortschritt. Bewegt er sich gegenüber dem
// letzten, läuft gerade etwas — auch wenn der Browser so weit vorgeladen
// hat, dass er gerade nichts abholt.
func (b *bzWaechter) fortschritt(kontoID, itemID string, pos int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.init()
	k := kontoID + "\x00" + itemID
	if alt, da := b.stand[k]; da && alt != pos {
		b.bewegt[kontoID] = time.Now()
	}
	if len(b.stand) > 2000 {
		b.stand = map[string]int64{}
	}
	b.stand[k] = pos
}

// uhr: die Uhr für Kalender und Fenster (Ortszeit des Servers). Tests
// stellen sie über s.jetzt.
func (s *Server) uhr() time.Time {
	if s.jetzt != nil {
		return s.jetzt()
	}
	return time.Now()
}

// bzLage: Regel und Urteil eines Profils für jetzt.
type bzLage struct {
	regel     store.Bildschirmzeit
	urteil    store.Urteil
	genutzt   time.Duration
	geschenkt int
	jetzt     time.Time
}

// bildschirmzeitLage: false, wenn für dieses Konto keine Bildschirmzeit
// gilt (kein Kinderprofil, Verwaltung, keine wirksame Regel).
func (s *Server) bildschirmzeitLage(ctx context.Context, acc *store.Account) (bzLage, bool) {
	if acc == nil || !acc.IsKid || acc.IsAdmin {
		return bzLage{}, false
	}
	regel, err := s.st.BildschirmzeitRegel(ctx, acc.ID)
	if err != nil || !regel.Wirksam() {
		return bzLage{}, false
	}
	jetzt := s.uhr()
	g, gs, _ := s.st.BildschirmzeitHeute(ctx, acc.ID, store.Tag(jetzt))
	return bzLage{regel: regel, urteil: regel.Pruefen(jetzt, g, gs), genutzt: g, geschenkt: gs, jetzt: jetzt}, true
}

// bildschirmzeitText: der Satz für das Kind, mit der Zeit, ab der es
// weitergeht.
func bildschirmzeitText(w http.ResponseWriter, l bzLage) string {
	weiter := l.urteil.WeiterAb
	zeit := weiter.Format("15:04")
	switch {
	case weiter.IsZero():
	case store.Tag(weiter) == store.Tag(l.jetzt):
		return tr(w, "Gerade ist Pause — ab {zeit} geht es weiter.", "zeit", zeit)
	case store.Tag(weiter) == store.Tag(l.jetzt.AddDate(0, 0, 1)):
		return tr(w, "Für heute ist Schluss — morgen ab {zeit} geht es weiter.", "zeit", zeit)
	}
	return tr(w, "Für heute ist Schluss mit Schauen.")
}

// bildschirmzeitFehler antwortet mit 403 und dem Code „bildschirmzeit".
func bildschirmzeitFehler(w http.ResponseWriter, l bzLage) {
	body := map[string]string{"error": bildschirmzeitText(w, l), "code": stream.GrundBildschirmzeit}
	if !l.urteil.WeiterAb.IsZero() {
		body["weiterAb"] = l.urteil.WeiterAb.Format(time.RFC3339)
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Lumen-Grund", stream.GrundBildschirmzeit)
	writeJSON(w, http.StatusForbidden, body)
}

// bildschirmzeitGesperrt: true, wenn jetzt nicht geschaut werden darf —
// dann ist die Antwort schon geschrieben.
func (s *Server) bildschirmzeitGesperrt(w http.ResponseWriter, r *http.Request) bool {
	l, gilt := s.bildschirmzeitLage(r.Context(), accountOf(r))
	if !gilt || l.urteil.Erlaubt {
		return false
	}
	bildschirmzeitFehler(w, l)
	return true
}

// ---------------------------------------------------------------- Wächter

// BildschirmzeitWaechter zählt und beendet, bis der Zusammenhang endet.
func (s *Server) BildschirmzeitWaechter(ctx context.Context) {
	t := time.NewTicker(bzTakt)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.bildschirmzeitTakt(ctx)
		}
	}
}

func (s *Server) bildschirmzeitTakt(ctx context.Context) {
	b := &s.bz
	echt := time.Now()

	aktiv := s.live.AktiveKonten(echt.Add(-bzAktiv))
	b.mu.Lock()
	b.init()
	for id, t := range b.bewegt {
		if echt.Sub(t) < bzAktiv {
			aktiv[id] = true
		} else {
			delete(b.bewegt, id)
		}
	}
	b.mu.Unlock()

	lage := func(id string) (bzLage, bool) {
		acc, err := s.st.Account(ctx, id)
		if err != nil {
			return bzLage{}, false
		}
		return s.bildschirmzeitLage(ctx, acc)
	}

	// Zählen: je Konto und Minute einmal.
	minute := echt.Unix() / 60
	for id := range aktiv {
		l, gilt := lage(id)
		if !gilt {
			continue
		}
		b.mu.Lock()
		neu := b.gezaehlt[id] != minute
		b.gezaehlt[id] = minute
		b.mu.Unlock()
		if neu {
			_ = s.st.BildschirmzeitZaehlen(ctx, id, store.Tag(l.jetzt), 60)
		}
	}

	// Beenden, was über der Zeit läuft — nach der Nachfrist.
	for id := range s.live.KontenMitWiedergabe() {
		l, gilt := lage(id)
		b.mu.Lock()
		if !gilt || l.urteil.Erlaubt {
			delete(b.ueberSeit, id)
			b.mu.Unlock()
			continue
		}
		seit, da := b.ueberSeit[id]
		if !da {
			seit = echt
			b.ueberSeit[id] = echt
		}
		faellig := echt.Sub(seit) >= b.nachfrist
		if faellig {
			delete(b.ueberSeit, id)
		}
		b.mu.Unlock()
		if faellig {
			// Bewusst kein Eintrag ins Protokoll: Die Verwaltung soll nicht
			// ablesen können, bis wann ein Kind geschaut hat.
			for _, sid := range s.live.EndeKontoMit(id, stream.GrundBildschirmzeit) {
				if s.tr != nil {
					s.tr.Stop(sid)
				}
			}
		}
	}

	// Ein neuer Tag: die Zahlen von gestern sind weg.
	tag := store.Tag(s.uhr())
	b.mu.Lock()
	neuerTag := b.letzterTag != tag
	b.letzterTag = tag
	if neuerTag {
		b.stand = map[string]int64{}
	}
	b.mu.Unlock()
	if neuerTag {
		_ = s.st.BildschirmzeitAufraeumen(ctx, tag)
	}
}

// ---------------------------------------------------------------- Kind

// meineBildschirmzeit: GET /api/v1/me/bildschirmzeit — wie viel heute noch
// geht. Für Profile ohne Bildschirmzeit nur {"aktiv": false}.
func (s *Server) meineBildschirmzeit(w http.ResponseWriter, r *http.Request) {
	l, gilt := s.bildschirmzeitLage(r.Context(), accountOf(r))
	if !gilt {
		ok(w, map[string]any{"aktiv": false})
		return
	}
	out := map[string]any{"aktiv": true, "erlaubt": l.urteil.Erlaubt,
		"genutztMinuten": int(l.genutzt / time.Minute)}
	heute := l.regel.Regel(l.jetzt)
	if heute.Von != "" {
		out["von"], out["bis"] = heute.Von, heute.Bis
	}
	if l.urteil.Budget > 0 {
		out["budgetMinuten"] = int(l.urteil.Budget / time.Minute)
	}
	if l.urteil.Erlaubt && l.urteil.Rest >= 0 {
		out["restMinuten"] = int(math.Ceil(l.urteil.Rest.Minutes()))
	}
	if !l.urteil.Erlaubt {
		out["meldung"] = bildschirmzeitText(w, l)
		if !l.urteil.WeiterAb.IsZero() {
			out["weiterAb"] = l.urteil.WeiterAb.Format(time.RFC3339)
		}
	}
	ok(w, out)
}

// ---------------------------------------------------------------- Verwaltung

// bzVerwaltung: was die Verwaltung über die Bildschirmzeit eines
// Kinderprofils sieht — die Regel und wie viele Minuten heute verbraucht
// sind. Was in diesen Minuten lief, steht nirgends.
type bzVerwaltung struct {
	Regel          store.Bildschirmzeit `json:"regel"`
	GenutztMinuten int                  `json:"genutztMinuten"`
	BudgetMinuten  int                  `json:"budgetMinuten"` // heute, samt Geschenktem; 0 = keine Grenze
	Geschenkt      int                  `json:"geschenkt"`
}

func (s *Server) bildschirmzeitVerwaltung(ctx context.Context, acc *store.Account) *bzVerwaltung {
	if acc == nil || !acc.IsKid || acc.IsAdmin {
		return nil
	}
	regel, err := s.st.BildschirmzeitRegel(ctx, acc.ID)
	if err != nil {
		return nil
	}
	if regel == (store.Bildschirmzeit{}) {
		// Noch nie eingestellt: der Vorschlag, ausgeschaltet.
		regel = store.StandardBildschirmzeit()
	}
	jetzt := s.uhr()
	g, gs, _ := s.st.BildschirmzeitHeute(ctx, acc.ID, store.Tag(jetzt))
	out := &bzVerwaltung{Regel: regel, GenutztMinuten: int(g / time.Minute), Geschenkt: gs}
	if m := regel.Regel(jetzt).Minuten; m > 0 {
		out.BudgetMinuten = m + gs
	}
	return out
}

// bzZiel: das Kinderprofil, um das es geht — mit den Rechten des Fragenden.
func (s *Server) bzZiel(w http.ResponseWriter, r *http.Request) (*store.Account, bool) {
	if err := s.mitverwalterDarf(r, r.PathValue("id"), nil); err != nil {
		fail(w, http.StatusForbidden, err.Error())
		return nil, false
	}
	ziel, err := s.st.Account(r.Context(), r.PathValue("id"))
	if err != nil {
		failErr(w, err)
		return nil, false
	}
	if !ziel.IsKid || ziel.IsAdmin {
		fail(w, http.StatusBadRequest, "Bildschirmzeit gibt es nur für Kinderprofile.")
		return nil, false
	}
	return ziel, true
}

// adminBildschirmzeit: PUT /api/v1/admin/accounts/{id}/bildschirmzeit
// mit der Regel → der neue Stand.
func (s *Server) adminBildschirmzeit(w http.ResponseWriter, r *http.Request) {
	var in store.Bildschirmzeit
	if !readJSON(w, r, &in) {
		return
	}
	ziel, gilt := s.bzZiel(w, r)
	if !gilt {
		return
	}
	if err := s.st.SetBildschirmzeit(r.Context(), ziel.ID, in); err != nil {
		failErr(w, err)
		return
	}
	ok(w, s.bildschirmzeitVerwaltung(r.Context(), ziel))
}

// adminBildschirmzeitGeschenk: POST …/bildschirmzeit/geschenk
// {"minuten": 30} — heute einmalig mehr.
func (s *Server) adminBildschirmzeitGeschenk(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Minuten int `json:"minuten"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	ziel, gilt := s.bzZiel(w, r)
	if !gilt {
		return
	}
	if _, err := s.st.BildschirmzeitSchenken(r.Context(), ziel.ID, store.Tag(s.uhr()), in.Minuten); err != nil {
		failErr(w, err)
		return
	}
	ok(w, s.bildschirmzeitVerwaltung(r.Context(), ziel))
}
