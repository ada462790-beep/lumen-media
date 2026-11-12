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
//  2. Der Wächter sieht alle 15 Sekunden nach, wann sich die Wiedergaben
//     jedes Kinderprofils zuletzt gemeldet haben (Segmente oder Daten
//     abgeholt, oder ein Fortschritt, der sich bewegt hat), und zählt die
//     Sekunden von Meldung zu Meldung — je Profil einmal, egal auf wie
//     vielen Geräten: Bildschirmzeit ist Uhrzeit. Nach der letzten Meldung
//     zählt nichts mehr nach; eine Lücke von höchstens bzLuecke (kurze
//     Pause, Puffer voll) gilt als durchgehend.
//     Dazu zählt die ausgelieferte Filmzeit: Ein Kinderprofil mit
//     Bildschirmzeit bekommt Direktstrom, Browser-Strom und HLS-Segmente
//     höchstens etwa 1,25-fach schneller als Echtzeit (Kinderdrossel), und
//     was schneller ankam als die Uhr lief, zählt nach Filmzeit. Ein
//     eigenes Programm, das nur Segmente holt, kommt so nicht mehr mit
//     null Minuten an einen ganzen Film.
//  3. Ist die Zeit um, während etwas läuft, endet die Wiedergabe nach einer
//     kurzen Nachfrist. Die nächste Anfrage des Geräts (Segment,
//     Bereichsanfrage, /browser/grund) bekommt dieselbe Meldung.
//
// Gezählt wird nur, was über diese Wege läuft: Video.
// Was von einem Download läuft, sieht der Server nicht — darum lässt sich
// mit einer Tagesgrenze gar nichts herunterladen (download.go), und mit
// einem blossen Zeitfenster nur innerhalb des Fensters.

import (
	"context"
	"errors"
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
	// bzLuecke: so lange darf es zwischen zwei Meldungen still sein, und
	// die Zeit dazwischen zählt trotzdem. Ein Gerät holt alle paar Sekunden
	// ein Segment, ein direkter Strom meldet sich alle zehn Sekunden
	// (meldeAbstand), der Browser sichert alle zehn Sekunden seine Stelle;
	// ist der Puffer voll, kann es etwas länger dauern. Was länger still
	// ist, war eine Pause — sie zählt nicht.
	bzLuecke = 45 * time.Second
	// bzWarnAbstand: so selten steht eine unlesbare Regel im Protokoll.
	bzWarnAbstand = 10 * time.Minute
	// bzNachfrist: so lange läuft eine Wiedergabe über das Ende hinaus —
	// genug, damit eine Szene zu Ende gehen kann, ohne dass es sich lohnt.
	bzNachfrist = time.Minute
)

// bzWaechter hält, was der Wächter zwischen zwei Takten braucht. Alles
// nur im Arbeitsspeicher: Konto-Kennungen und Zeiten, kein Titel.
type bzWaechter struct {
	mu sync.Mutex
	// gezaehlt: bis zu welchem Augenblick je Konto gezählt ist (die letzte
	// Meldung, die schon zählte).
	gezaehlt map[string]time.Time
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
	// gewarnt: wann zuletzt eine unlesbare Regel gemeldet wurde.
	gewarnt time.Time
	// geliefert: seit dem letzten Takt ausgelieferte Sekunden Film je
	// Konto (Kinderdrossel, stream.NeueKinderDrossel).
	geliefert map[string]float64
	// sitzung: je Konto, solange es ohne längere Pause schaut: wie viel
	// Uhrzeit und wie viel ausgelieferte Filmzeit bisher zusammenkamen
	// und wie viel davon schon gezählt ist.
	sitzung map[string]*bzSitzung
}

// bzSitzung: eine Strecke ohne längere Pause.
type bzSitzung struct {
	wand, film, gezaehlt float64
	zuletzt              time.Time
}

// bzFilmVorsprung: so viel mehr Filmzeit als Uhrzeit darf ausgeliefert
// sein, bevor sie zählt — ein Player lädt etwas vor, und nach einem
// Sprung holt er neu. Was darüber hinausgeht, kam schneller als in
// Echtzeit und zählt als geschaut.
const bzFilmVorsprung = 30.0

// geliefertMelden: die Kinderdrossel hat sek Sekunden Film ausgeliefert.
func (b *bzWaechter) geliefertMelden(kontoID string, sek float64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.init()
	b.geliefert[kontoID] += sek
}

func (b *bzWaechter) init() {
	if b.gezaehlt == nil {
		b.gezaehlt = map[string]time.Time{}
		b.bewegt = map[string]time.Time{}
		b.stand = map[string]int64{}
		b.ueberSeit = map[string]time.Time{}
		b.nachfrist = bzNachfrist
	}
	if b.geliefert == nil {
		b.geliefert = map[string]float64{}
		b.sitzung = map[string]*bzSitzung{}
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
	// tag: der Schautag, unter dem gezählt wird (store.Schautag).
	tag string
	// fehler: Regel oder Zähler liessen sich nicht lesen. Dann ist
	// gesperrt — lieber einmal zu streng als still ohne Grenze.
	fehler error
}

// bildschirmzeitLage: false, wenn für dieses Konto keine Bildschirmzeit
// gilt (kein Kinderprofil, Verwaltung, keine wirksame Regel). Lässt sich
// die Regel oder der Zähler nicht lesen, gilt sie — und sperrt.
func (s *Server) bildschirmzeitLage(ctx context.Context, acc *store.Account) (bzLage, bool) {
	if acc == nil || !acc.IsKid || acc.IsAdmin {
		return bzLage{}, false
	}
	jetzt := s.uhr()
	gesperrt := func(err error) (bzLage, bool) {
		s.bzWarnen(err)
		return bzLage{urteil: store.Urteil{Grund: "fehler"}, jetzt: jetzt, fehler: err}, true
	}
	regel, err := s.st.BildschirmzeitRegel(ctx, acc.ID)
	if err != nil {
		return gesperrt(err)
	}
	if !regel.Wirksam() {
		return bzLage{}, false
	}
	tag := regel.Schautag(jetzt)
	g, gs, err := s.st.BildschirmzeitHeute(ctx, acc.ID, tag)
	if err != nil {
		return gesperrt(err)
	}
	return bzLage{regel: regel, urteil: regel.Pruefen(jetzt, g, gs), genutzt: g, geschenkt: gs, jetzt: jetzt, tag: tag}, true
}

// bzWarnen schreibt höchstens alle zehn Minuten eine Zeile ins Protokoll —
// ohne Namen und ohne Konto: Die Verwaltung soll nicht ablesen können, wer
// wann schauen wollte.
func (s *Server) bzWarnen(err error) {
	b := &s.bz
	b.mu.Lock()
	faellig := time.Since(b.gewarnt) >= bzWarnAbstand
	if faellig {
		b.gewarnt = time.Now()
	}
	b.mu.Unlock()
	if !faellig || s.log == nil {
		return
	}
	if errors.Is(err, store.ErrRegelUnlesbar) {
		s.log.Warn("Bildschirmzeit: eine Regel lässt sich nicht lesen — das Profil bleibt gesperrt, bis sie neu gespeichert ist")
		return
	}
	s.log.Warn("Bildschirmzeit: Datenbank nicht lesbar — Kinderprofile mit Bildschirmzeit bleiben solange gesperrt", "err", err)
}

// bildschirmzeitText: der Satz für das Kind, mit der Zeit, ab der es
// weitergeht.
func bildschirmzeitText(w http.ResponseWriter, l bzLage) string {
	if l.fehler != nil {
		return tr(w, "Die Bildschirmzeit lässt sich gerade nicht prüfen. Bitte später nochmals versuchen.")
	}
	weiter := l.urteil.WeiterAb
	zeit := weiter.Format("15:04")
	switch {
	case weiter.IsZero():
	case l.regel.Schautag(weiter) == l.tag:
		return tr(w, "Gerade ist Pause — ab {zeit} geht es weiter.", "zeit", zeit)
	case store.Tag(weiter) == store.Tag(l.jetzt):
		// Nach Mitternacht, am Ende eines Nachtfensters.
		return tr(w, "Für heute ist Schluss — ab {zeit} geht es weiter.", "zeit", zeit)
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

	// Wann sich jedes Konto zuletzt gemeldet hat: Wiedergaben und
	// Fortschritte, die sich bewegt haben.
	zuletzt := s.live.LetzteMeldungen()
	b.mu.Lock()
	b.init()
	for id, t := range b.bewegt {
		if echt.Sub(t) > bzLuecke {
			delete(b.bewegt, id)
			continue
		}
		if t.After(zuletzt[id]) {
			zuletzt[id] = t
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

	// Zählen: die Sekunden von der zuletzt gezählten Meldung bis zur
	// neuesten — je Konto einmal, wie viele Geräte auch laufen. Nach einer
	// längeren Stille (Pause) beginnt die Zählung neu bei der Meldung.
	//
	// Dazu die ausgelieferte Filmzeit: Holt ein Programm schneller als in
	// Echtzeit (die Kinderdrossel lässt höchstens etwa 1,25-fach zu), zählt
	// die Filmzeit statt der Uhrzeit. Gezählt wird je Strecke ohne Pause
	// das Grössere von beidem (mit bzFilmVorsprung Spielraum fürs
	// Vorladen) — nie doppelt.
	b.mu.Lock()
	geliefert := b.geliefert
	b.geliefert = map[string]float64{}
	b.mu.Unlock()
	konten := map[string]bool{}
	for id := range zuletzt {
		konten[id] = true
	}
	for id := range geliefert {
		konten[id] = true
	}
	for id := range konten {
		letzte := zuletzt[id]
		film := geliefert[id]
		b.mu.Lock()
		bis := b.gezaehlt[id]
		b.mu.Unlock()
		neueMeldung := letzte.After(bis)
		if !neueMeldung && film <= 0 {
			continue
		}
		l, gilt := lage(id)
		if !gilt || l.fehler != nil {
			continue
		}
		var dauer time.Duration
		if neueMeldung {
			dauer = letzte.Sub(bis)
			if bis.IsZero() || dauer > bzLuecke {
				dauer = 0 // neu angefangen: erst ab hier
			}
		}
		b.mu.Lock()
		if neueMeldung {
			b.gezaehlt[id] = letzte
		}
		si := b.sitzung[id]
		if si == nil || echt.Sub(si.zuletzt) > bzLuecke {
			si = &bzSitzung{}
			b.sitzung[id] = si
		}
		si.zuletzt = echt
		si.wand += dauer.Seconds()
		si.film += film
		soll := max(si.wand, si.film-bzFilmVorsprung)
		sek := int(soll - si.gezaehlt)
		if sek > 0 {
			si.gezaehlt += float64(sek)
		}
		b.mu.Unlock()
		if sek > 0 {
			_ = s.st.BildschirmzeitZaehlen(ctx, id, l.tag, sek)
		}
	}
	b.mu.Lock()
	for id, bis := range b.gezaehlt {
		if echt.Sub(bis) > bzLuecke {
			delete(b.gezaehlt, id)
		}
	}
	for id, si := range b.sitzung {
		if echt.Sub(si.zuletzt) > bzLuecke {
			delete(b.sitzung, id)
		}
	}
	b.mu.Unlock()

	// Beenden, was über der Zeit läuft — nach der Nachfrist.
	laufend := s.live.KontenMitWiedergabe()
	b.mu.Lock()
	for id := range b.ueberSeit {
		// Ohne Wiedergabe keine Nachfrist mehr: Wer neu ansetzt, bekommt
		// beim nächsten Überziehen wieder die ganze.
		if !laufend[id] {
			delete(b.ueberSeit, id)
		}
	}
	b.mu.Unlock()
	for id := range laufend {
		l, gilt := lage(id)
		b.mu.Lock()
		// Liess sich die Datenbank kurz nicht lesen, endet nichts — nur
		// eine unlesbare Regel sperrt auch, was schon läuft.
		if !gilt || l.urteil.Erlaubt || (l.fehler != nil && !errors.Is(l.fehler, store.ErrRegelUnlesbar)) {
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

	// Ein neuer Kalendertag: die Zahlen von vorgestern sind weg. Gestern
	// bleibt, bis das Profil wieder zählt — nach Mitternacht kann ein
	// Nachtfenster noch zu gestern gehören.
	jetzt := s.uhr()
	tag := store.Tag(jetzt)
	b.mu.Lock()
	neuerTag := b.letzterTag != tag
	b.letzterTag = tag
	if neuerTag {
		b.stand = map[string]int64{}
	}
	b.mu.Unlock()
	if neuerTag {
		_ = s.st.BildschirmzeitAufraeumen(ctx, store.Tag(jetzt.AddDate(0, 0, -1)))
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
	if l.fehler != nil {
		ok(w, map[string]any{"aktiv": true, "erlaubt": false, "genutztMinuten": 0,
			"meldung": bildschirmzeitText(w, l)})
		return
	}
	out := map[string]any{"aktiv": true, "erlaubt": l.urteil.Erlaubt,
		"genutztMinuten": int(l.genutzt / time.Minute)}
	heute := l.regel.Regel(l.regel.SchautagBeginn(l.jetzt))
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
// Kinderprofils sieht — die Regel und ungefähr, wie viel heute verbraucht
// ist. Was in diesen Minuten lief, steht nirgends.
//
// Die Minuten sind auf Viertelstunden abgerundet (bzStufe): Wer die
// Kontenliste immer wieder lädt, sähe sonst Minute für Minute, wann das
// Kind zu schauen beginnt und aufhört.
type bzVerwaltung struct {
	Regel store.Bildschirmzeit `json:"regel"`
	// GenutztMinuten: heute verbraucht, abgerundet auf Viertelstunden.
	GenutztMinuten int `json:"genutztMinuten"`
	// GenutztStufe: auf so viele Minuten ist GenutztMinuten abgerundet.
	GenutztStufe  int `json:"genutztStufe"`
	BudgetMinuten int `json:"budgetMinuten"` // heute, samt Geschenktem; 0 = keine Grenze
	Geschenkt     int `json:"geschenkt"`
}

// bzStufe: auf so viele Minuten rundet die Verwaltung ab.
const bzStufe = 15

func (s *Server) bildschirmzeitVerwaltung(ctx context.Context, acc *store.Account) *bzVerwaltung {
	if acc == nil || !acc.IsKid || acc.IsAdmin {
		return nil
	}
	regel, err := s.st.BildschirmzeitRegel(ctx, acc.ID)
	if errors.Is(err, store.ErrRegelUnlesbar) {
		// Gesperrt, bis die Verwaltung neu speichert: den Vorschlag zeigen,
		// eingeschaltet.
		regel, err = store.StandardBildschirmzeit(), nil
		regel.Aktiv = true
	}
	if err != nil {
		return nil
	}
	if regel == (store.Bildschirmzeit{}) {
		// Noch nie eingestellt: der Vorschlag, ausgeschaltet.
		regel = store.StandardBildschirmzeit()
	}
	jetzt := s.uhr()
	g, gs, _ := s.st.BildschirmzeitHeute(ctx, acc.ID, regel.Schautag(jetzt))
	min := int(g / time.Minute)
	out := &bzVerwaltung{Regel: regel, GenutztMinuten: min - min%bzStufe, GenutztStufe: bzStufe, Geschenkt: gs}
	if m := regel.Regel(regel.SchautagBeginn(jetzt)).Minuten; m > 0 {
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
	regel, err := s.st.BildschirmzeitRegel(r.Context(), ziel.ID)
	if err != nil {
		failErr(w, err)
		return
	}
	if _, err := s.st.BildschirmzeitSchenken(r.Context(), ziel.ID, regel.Schautag(s.uhr()), in.Minuten); err != nil {
		failErr(w, err)
		return
	}
	ok(w, s.bildschirmzeitVerwaltung(r.Context(), ziel))
}
