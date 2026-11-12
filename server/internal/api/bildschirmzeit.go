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
	mrand "math/rand/v2"
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
	ueberSeit map[string]time.Time
	// ueberZufall: der zufällige Aufschlag auf die Nachfrist je Konto.
	ueberZufall map[string]time.Duration
	letzterTag  string
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
	// zuletztAktiv: wann für ein Konto zuletzt gezählt wurde (für den
	// Stand der Verwaltung, bzVerwaltungsStand).
	zuletztAktiv map[string]time.Time
	// verwaltung: der Stand, den die Verwaltung zuletzt sah.
	verwaltung map[string]bzStand
	// verwaltungSofort: Tests sehen den Stand ohne Verzögerung.
	verwaltungSofort bool
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
		b.zuletztAktiv = map[string]time.Time{}
		b.verwaltung = map[string]bzStand{}
		b.ueberZufall = map[string]time.Duration{}
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
	// verlaengertBis: das Fenster ist heute bis dahin verlängert
	// („heute einmalig mehr"); Nullzeit: nicht.
	verlaengertBis time.Time
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
	bis := s.st.BildschirmzeitVerlaengerung(ctx, acc.ID, tag)
	return bzLage{regel: regel, urteil: regel.PruefenMit(jetzt, g, gs, bis), genutzt: g, geschenkt: gs,
		jetzt: jetzt, tag: tag, verlaengertBis: bis}, true
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
// weitergeht. Für ein Kind von acht Jahren: „morgen ab 00:00" heisst
// „morgen", und nach dem Ende eines Fensters über Mitternacht ist es
// keine „Pause", sondern Schluss bis heute Abend.
func bildschirmzeitText(w http.ResponseWriter, l bzLage) string {
	if l.fehler != nil {
		return tr(w, "Die Bildschirmzeit lässt sich gerade nicht prüfen. Bitte später nochmals versuchen.")
	}
	weiter := l.urteil.WeiterAb
	zeit := weiter.Format("15:04")
	heute := !weiter.IsZero() && store.Tag(weiter) == store.Tag(l.jetzt)
	morgen := !weiter.IsZero() && store.Tag(weiter) == store.Tag(l.jetzt.AddDate(0, 0, 1))
	switch {
	case heute && l.urteil.Grund == "fenster" && !l.regel.NachtfensterVorbei(l.jetzt):
		// Vor dem Fenster am selben Tag (früh am Morgen).
		return tr(w, "Gerade ist Pause — heute ab {zeit} geht es weiter.", "zeit", zeit)
	case heute:
		return tr(w, "Für heute ist Schluss — heute ab {zeit} geht es weiter.", "zeit", zeit)
	case morgen && zeit == "00:00":
		return tr(w, "Für heute ist Schluss — morgen geht es weiter.")
	case morgen:
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
		b.zuletztAktiv[id] = echt
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
			// Mit zufälligem Aufschlag (bis eine Minute): Stellt die
			// Verwaltung die Zeit auf null, soll die Zahl der Wiedergaben
			// nicht auf die Sekunde genau eine Minute später fallen — das
			// verriete, dass genau dieses Kind gerade schaute.
			b.ueberZufall[id] = time.Duration(mrand.Int64N(int64(time.Minute)))
		}
		frist := b.nachfrist
		if frist > 0 {
			frist += b.ueberZufall[id]
		}
		faellig := echt.Sub(seit) >= frist
		if faellig {
			delete(b.ueberSeit, id)
			delete(b.ueberZufall, id)
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
	if !l.verlaengertBis.IsZero() && l.jetzt.Before(l.verlaengertBis) {
		out["verlaengertBis"] = l.verlaengertBis.Format("15:04")
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
// Die Minuten sind auf Viertelstunden abgerundet (bzStufe) und zudem ein
// älterer Stand (bzVerwaltungsStand): Früher sprang die Zahl genau dann
// von 0 auf 15, wenn das Kind gerade schaute — wer die Kontenliste alle
// zehn Sekunden lud, wusste so auf die Sekunde, wann es schaute.
type bzVerwaltung struct {
	Regel store.Bildschirmzeit `json:"regel"`
	// GenutztMinuten: heute verbraucht, abgerundet auf Viertelstunden,
	// Stand nach bzVerwaltungsStand.
	GenutztMinuten int `json:"genutztMinuten"`
	// GenutztStufe: auf so viele Minuten ist GenutztMinuten abgerundet.
	GenutztStufe  int `json:"genutztStufe"`
	BudgetMinuten int `json:"budgetMinuten"` // heute, samt Geschenktem; 0 = keine Grenze
	Geschenkt     int `json:"geschenkt"`
	// VerlaengertBis: das Fenster ist heute bis dahin verlängert ("15:04").
	VerlaengertBis string `json:"verlaengertBis,omitempty"`
	// FensterHeute: heute gilt ein Zeitfenster („heute einmalig mehr"
	// verlängert es).
	FensterHeute bool `json:"fensterHeute,omitempty"`
}

// bzStufe: auf so viele Minuten rundet die Verwaltung ab.
const bzStufe = 15

// Wann die Verwaltung einen neuen Stand sieht: erst eine Weile, nachdem
// das Kind aufgehört hat (bzStandRuhe, mit zufälligem Aufschlag bis
// bzStandRuheZufall) — oder spätestens nach bzStandAlter, auch wenn es
// noch schaut. So zeigt die Zahl, wie viel heute zusammenkam, aber nicht,
// wann.
const (
	bzStandRuhe       = 10 * time.Minute
	bzStandRuheZufall = 15 * time.Minute
	bzStandAlter      = time.Hour
)

// bzStand: der Stand, den die Verwaltung zuletzt sehen durfte.
type bzStand struct {
	tag     string
	minuten int // schon abgerundet
	am      time.Time
	ruhe    time.Duration // diese Ruhe muss vor dem nächsten Stand sein
}

// bzVerwaltungsStand: die Minuten, die die Verwaltung jetzt sieht.
func (s *Server) bzVerwaltungsStand(kontoID, tag string, genutzt time.Duration) int {
	b := &s.bz
	echt := time.Now()
	min := int(genutzt / time.Minute)
	min -= min % bzStufe
	b.mu.Lock()
	defer b.mu.Unlock()
	b.init()
	alt, da := b.verwaltung[kontoID]
	aktiv := echt.Sub(b.zuletztAktiv[kontoID])
	if da && alt.tag == tag && alt.minuten == min {
		return min
	}
	if !da || alt.tag != tag || aktiv >= alt.ruhe || echt.Sub(alt.am) >= bzStandAlter || b.verwaltungSofort {
		ruhe := bzStandRuhe + time.Duration(mrand.Int64N(int64(bzStandRuheZufall)))
		b.verwaltung[kontoID] = bzStand{tag: tag, minuten: min, am: echt, ruhe: ruhe}
		return min
	}
	return alt.minuten
}

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
	tag := regel.Schautag(jetzt)
	g, gs, _ := s.st.BildschirmzeitHeute(ctx, acc.ID, tag)
	out := &bzVerwaltung{Regel: regel, GenutztMinuten: s.bzVerwaltungsStand(acc.ID, tag, g),
		GenutztStufe: bzStufe, Geschenkt: gs}
	heute := regel.Regel(regel.SchautagBeginn(jetzt))
	if m := heute.Minuten; m > 0 {
		out.BudgetMinuten = m + gs
	}
	out.FensterHeute = heute.Von != ""
	if bis := s.st.BildschirmzeitVerlaengerung(ctx, acc.ID, tag); !bis.IsZero() && jetzt.Before(bis) {
		out.VerlaengertBis = bis.Format("15:04")
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
// {"minuten": 30} — heute einmalig mehr. Mit Tagesgrenze kommen die
// Minuten dazu; mit Zeitfenster geht das Fenster heute so viel länger —
// ab seinem Ende, oder ab jetzt, wenn es schon zu ist („darf ich noch eine
// Folge?" um 22:15). Früher verlängerte das nur die Tagesgrenze, und die
// Oberfläche meldete Erfolg, während das Kind ausserhalb des Fensters
// weiter vor „Für heute ist Schluss" stand.
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
	if in.Minuten <= 0 || in.Minuten > store.MaxGeschenkt {
		fail(w, http.StatusBadRequest, "So viele Minuten lassen sich nicht dazugeben.")
		return
	}
	jetzt := s.uhr()
	tag := regel.Schautag(jetzt)
	heute := regel.Regel(regel.SchautagBeginn(jetzt))
	if heute.Minuten > 0 {
		if _, err := s.st.BildschirmzeitSchenken(r.Context(), ziel.ID, tag, in.Minuten); err != nil {
			failErr(w, err)
			return
		}
	}
	if heute.Von != "" {
		ab := regel.FensterEnde(jetzt)
		if alt := s.st.BildschirmzeitVerlaengerung(r.Context(), ziel.ID, tag); alt.After(ab) {
			ab = alt
		}
		if jetzt.After(ab) {
			ab = jetzt
		}
		bis := ab.Add(time.Duration(in.Minuten) * time.Minute)
		if grenze := jetzt.Add(time.Duration(store.MaxGeschenkt) * time.Minute); bis.After(grenze) {
			bis = grenze
		}
		if err := s.st.BildschirmzeitVerlaengern(r.Context(), ziel.ID, tag, bis); err != nil {
			failErr(w, err)
			return
		}
	}
	ok(w, s.bildschirmzeitVerwaltung(r.Context(), ziel))
}

// adminBildschirmzeitGeschenkZurueck: DELETE …/bildschirmzeit/geschenk —
// nimmt zurück, was heute dazugegeben wurde (Minuten und Verlängerung).
func (s *Server) adminBildschirmzeitGeschenkZurueck(w http.ResponseWriter, r *http.Request) {
	ziel, gilt := s.bzZiel(w, r)
	if !gilt {
		return
	}
	regel, err := s.st.BildschirmzeitRegel(r.Context(), ziel.ID)
	if err != nil {
		failErr(w, err)
		return
	}
	if err := s.st.BildschirmzeitGeschenkZurueck(r.Context(), ziel.ID, regel.Schautag(s.uhr())); err != nil {
		failErr(w, err)
		return
	}
	ok(w, s.bildschirmzeitVerwaltung(r.Context(), ziel))
}
