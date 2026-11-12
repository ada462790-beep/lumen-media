// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package stream

// Die aktiven Wiedergaben.
//
// Hier steckt die zweite Hälfte des Datenschutz-Versprechens. Der Server
// muss wissen, welche Datei er gerade ausliefert — sonst könnte er sie nicht
// ausliefern. Aber was der Besitzer in der Verwaltung zu sehen bekommt, ist
// eine eigene Struktur: AdminSession. Darin steht nur, ob eine Wiedergabe
// direkt läuft oder umgewandelt wird. Kein Titel, keine Datei, keine
// Person, kein Gerät, kein Ort, keine Startzeit — und auch keine Gründe
// („Gerät kann kein HDR", „Bandbreite") oder Qualitätsstufen mehr: Mit den
// Grenzen je Konto zusammen liess sich daraus erraten, wer schaut.
//
// Die Kennung zum Beenden gilt nur für die eine Abfrage und zwei Minuten.
// Fragt die Verwaltung wieder, bekommt jede Wiedergabe eine neue. Früher
// blieb die Kennung stehen, solange die Wiedergabe lief — wer regelmässig
// nachsah, hatte Beginn, Ende und Dauer jeder einzelnen Wiedergabe.
//
// Zwei Dinge sichern das ab:
//   - AdminSession und Session sind getrennte Typen, und From() ist die
//     einzige Stelle, die von einem zum anderen übersetzt.
//   - live_test.go geht mit Reflexion über AdminSession und schlägt fehl,
//     sobald ein Feld auftaucht, das nach Inhalt klingt.
//
// Nichts davon wird in die Datenbank geschrieben. Endet die Wiedergabe,
// ist die Sitzung weg.

import (
	"crypto/rand"
	"encoding/hex"
	"sort"
	"sync"
	"time"
)

// Session ist, was der Server intern wissen muss.
type Session struct {
	ID         string
	AccountID  string
	Name       string // Anzeigename des Profils
	Avatar     string
	DeviceName string
	DeviceKind string
	// Geraet: woran ein Gerät erkannt wird — seine Anmeldung, nicht sein
	// Name. Zwei iPads oder zwei Browser heissen oft gleich („Chrome ·
	// Linux") und verdrängten sich früher gegenseitig. Leer: der Name.
	Geraet string

	// FileID ist die Datei, die diese Wiedergabe ausliefern darf. Nur
	// damit lässt sich ein Segment abholen — eine fremde Kennung in der
	// Adresse führt ins Leere.
	FileID string

	// Drossel: gesetzt, wenn das Profil nicht herunterladen darf
	// (drossel.go). Alle Anfragen dieser Wiedergabe teilen sie sich.
	Drossel *Drossel

	// Diese verlassen das Paket nie Richtung Besitzer.
	itemID  string
	path    string
	adminID string

	Mode       string // direct | transcode
	Reason     string
	Bitrate    int
	FromCodec  string
	ToCodec    string
	FromHeight int
	ToHeight   int
	StartedAt  time.Time

	// zuletzt ist, wann dieses Gerät sich das letzte Mal gemeldet hat. Eine
	// umgewandelte Wiedergabe meldet sich alle paar Sekunden mit dem
	// nächsten Segment; bleibt sie weg, ist niemand mehr da.
	zuletzt time.Time

	// offen zählt Anfragen, die gerade ausliefern (Direktwiedergabe,
	// Browser-Strom). Solange eine offen ist, schaut jemand — auch wenn
	// in der Pause gerade kein Byte fliesst, weil das Gerät nicht liest.
	offen int
	// ueberAnfragen: Diese Wiedergabe läuft über Anfragen, die offen
	// bleiben (nicht über Segmente). Für sie gilt die kurze Frist.
	ueberAnfragen bool

	stop chan struct{}
}

// AdminSession ist die Sicht des Besitzers. Was hier nicht steht, sieht er
// nicht — und es gibt keinen zweiten Weg zu den Sitzungen.
type AdminSession struct {
	ID   string `json:"id"`   // Einmalkennung, nur für „Beenden" (siehe ForOwner)
	Mode string `json:"mode"` // direct | transcode
}

// From ist die einzige Übersetzung von innen nach aussen. Wer dem Besitzer
// mehr zeigen will, muss hier ein Feld hinzufügen — und bricht damit den
// Test, der nur genau diese Felder erlaubt. Die Kennung setzt ForOwner.
func (s *Session) From() AdminSession {
	return AdminSession{Mode: s.Mode}
}

// einmalGueltig: so lange lässt sich eine Wiedergabe über die Kennung aus
// einer Liste der Verwaltung beenden.
const einmalGueltig = 2 * time.Minute

type einmalKennung struct {
	sessionID string
	bis       time.Time
}

// GrobeBitrate rundet auf eine von wenigen Stufen auf (bis 2, 5, 10, 20,
// 40, 80 Mbit/s, darüber 100). Die genaue Bitrate einer Datei ist fast so
// eindeutig wie ihr Name: Wer die Bitraten aller Filme kennt, wüsste sonst
// aus der Verwaltung, welcher gerade läuft.
func GrobeBitrate(b int) int {
	if b <= 0 {
		return 0
	}
	for _, stufe := range []int{2, 5, 10, 20, 40, 80} {
		if b <= stufe*1_000_000 {
			return stufe * 1_000_000
		}
	}
	return 100_000_000
}

// EigeneWiedergabe ist, was eine Person über ihre eigenen laufenden
// Wiedergaben sieht (unter „Ich → Geräte"), damit sie einen vergessenen
// Fernseher selbst beenden kann. Nur das eigene Profil bekommt das.
type EigeneWiedergabe struct {
	ID         string `json:"id"`
	DeviceName string `json:"deviceName"`
	DeviceKind string `json:"deviceKind"`
	Mode       string `json:"mode"`
	Detail     string `json:"detail"`
}

// detail beschreibt die technische Last — Auflösung und Codec, nie den Titel.
func (s *Session) detail() string {
	if s.Mode == "direct" {
		return label(s.FromHeight) + " " + s.FromCodec
	}
	return label(s.FromHeight) + " " + s.FromCodec + " → " +
		label(s.ToHeight) + " " + s.ToCodec
}

func label(h int) string {
	switch {
	case h >= 2000:
		return "4K"
	case h >= 1000:
		return "1080p"
	case h >= 700:
		return "720p"
	case h > 0:
		return "SD"
	}
	return ""
}

// ---------------------------------------------------------------- Verwaltung

type Live struct {
	mu   sync.RWMutex
	byID map[string]*Session
	// einmal: Kennungen aus den Listen der Verwaltung → Sitzung.
	einmal map[string]einmalKennung

	// vorbei merkt sich für eine Weile, warum eine Wiedergabe endete —
	// nur die Zufallskennung und den Grund, keine Datei, kein Konto. Damit
	// kann ein Gerät, das nach einer langen Pause weiterspielen will, eine
	// klare Antwort bekommen („abgelaufen, setz neu an") statt nur 410.
	vorbei map[string]vorbeiEintrag
}

type vorbeiEintrag struct {
	grund string
	wann  time.Time
}

// Gründe, aus denen eine Wiedergabe endet.
const (
	// GrundBeendet: ausdrücklich beendet (vom Gerät, in der Verwaltung,
	// unter „Ich → Geräte"). Nicht von selbst neu ansetzen.
	GrundBeendet = "wiedergabe_beendet"
	// GrundAbgelaufen: zu lange nichts gehört (Pause, Netz weg). Das
	// Gerät darf einfach neu ansetzen.
	GrundAbgelaufen = "wiedergabe_abgelaufen"
	// GrundErsetzt: Dasselbe Gerät spielt inzwischen etwas anderes.
	GrundErsetzt = "wiedergabe_ersetzt"
	// GrundBildschirmzeit: Das Zeitfenster oder die Tagesgrenze des
	// Profils ist erreicht. Nicht neu ansetzen, sondern die Meldung zeigen.
	GrundBildschirmzeit = "bildschirmzeit"
)

func NewLive() *Live {
	return &Live{byID: map[string]*Session{}, einmal: map[string]einmalKennung{}, vorbei: map[string]vorbeiEintrag{}}
}

// Grund sagt, warum eine Wiedergabe nicht (mehr) läuft. "" = unbekannt
// (nie gegeben oder zu lange her).
func (l *Live) Grund(id string) string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.vorbei[id].grund
}

// geraet: die Kennung des Geräts (Geraet, sonst der Name).
func (s *Session) geraet() string {
	if s.Geraet != "" {
		return s.Geraet
	}
	return s.DeviceName
}

// ReplaceFor beendet eine laufende Wiedergabe desselben Kontos auf
// demselben Gerät. Ein Gerät spielt immer nur eine Sache — springt jemand
// im Film, ist das kein zweiter Zuschauer. Ohne das zählte die Grenze
// „gleichzeitig schauen" gegen den eigenen Player.
// deviceName: die Gerätekennung (Session.Geraet) oder der Name.
func (l *Live) ReplaceFor(accountID, deviceName string) {
	l.mu.Lock()
	var ids []string
	for id, s := range l.byID {
		if s.AccountID == accountID && s.geraet() == deviceName {
			ids = append(ids, id)
		}
	}
	l.mu.Unlock()
	for _, id := range ids {
		l.EndMit(id, GrundErsetzt)
	}
}

// ReplaceForAusser ersetzt wie ReplaceFor, lässt aber eine Wiedergabe
// stehen (die, die gerade weiterläuft).
func (l *Live) ReplaceForAusser(accountID, deviceName, behalten string) {
	l.mu.Lock()
	var ids []string
	for id, s := range l.byID {
		if s.AccountID == accountID && s.geraet() == deviceName && id != behalten {
			ids = append(ids, id)
		}
	}
	l.mu.Unlock()
	for _, id := range ids {
		l.EndMit(id, GrundErsetzt)
	}
}

// Finden sucht die laufende Wiedergabe eines Geräts für eine Datei in
// einer Art (direct/transcode). Eine Direktwiedergabe besteht aus vielen
// Bereichsanfragen — jede Sprungstelle ist eine —, aber sie ist eine
// Wiedergabe, nicht zwanzig.
func (l *Live) Finden(accountID, deviceName, fileID, mode string) string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	for id, s := range l.byID {
		if s.AccountID == accountID && s.geraet() == deviceName && s.FileID == fileID && s.Mode == mode {
			return id
		}
	}
	return ""
}

// Oeffnen vermerkt eine Anfrage, die für diese Wiedergabe ausliefert. Die
// zurückgegebene Funktion schliesst sie wieder. Gibt false zurück, wenn
// es die Wiedergabe nicht (mehr) gibt.
func (l *Live) Oeffnen(id string) (func(), bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	s, ok := l.byID[id]
	if !ok {
		return func() {}, false
	}
	s.offen++
	s.ueberAnfragen = true
	s.zuletzt = time.Now()
	var einmal sync.Once
	return func() {
		einmal.Do(func() {
			l.mu.Lock()
			defer l.mu.Unlock()
			if s, ok := l.byID[id]; ok {
				s.offen--
				s.zuletzt = time.Now()
			}
		})
	}, true
}

// zufall liefert eine Kennung ohne Bezug zu Zeit, Person oder Titel.
func zufall() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (l *Live) Start(s *Session) *Session {
	l.mu.Lock()
	defer l.mu.Unlock()
	s.ID = zufall()
	s.adminID = zufall()
	s.StartedAt = time.Now()
	s.zuletzt = s.StartedAt
	s.stop = make(chan struct{})
	l.byID[s.ID] = s
	return s
}

func (l *Live) End(id string) { l.EndMit(id, GrundBeendet) }

// EndMit beendet eine Wiedergabe und merkt sich den Grund.
func (l *Live) EndMit(id, grund string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.endGesperrt(id, grund)
}

func (l *Live) endGesperrt(id, grund string) {
	if s, ok := l.byID[id]; ok {
		select {
		case <-s.stop:
		default:
			close(s.stop)
		}
		delete(l.byID, id)
		l.vorbei[id] = vorbeiEintrag{grund: grund, wann: time.Now()}
	}
	// Alte Gründe vergessen: nach einer Stunde fragt keiner mehr nach.
	if len(l.vorbei) > 64 {
		for k, v := range l.vorbei {
			if time.Since(v.wann) > time.Hour {
				delete(l.vorbei, k)
			}
		}
	}
}

// Beitreten ist der Einstieg einer Direktwiedergabe: Läuft auf diesem
// Gerät schon eine für dieselbe Datei, wird sie weiterbenutzt (jede
// Bereichsanfrage beim Spulen gehört dazu). Sonst endet, was das Gerät
// vorher spielte, und es beginnt eine neue — sofern das Konto unter
// seiner Grenze bleibt. Alles unter einer Sperre: zwei gleichzeitige
// Bereichsanfragen (Player holen gern Anfang und Ende parallel) dürfen
// nicht zwei Wiedergaben anlegen oder sich gegenseitig beenden.
//
// Gibt die Wiedergabe zurück, oder nil und die Zahl der laufenden, wenn
// die Grenze erreicht ist.
func (l *Live) Beitreten(neu *Session, max int) (*Session, int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, s := range l.byID {
		if s.AccountID == neu.AccountID && s.geraet() == neu.geraet() &&
			s.FileID == neu.FileID && s.Mode == neu.Mode {
			s.zuletzt = time.Now()
			return s, 0
		}
	}
	var alt []string
	for id, s := range l.byID {
		if s.AccountID == neu.AccountID && s.geraet() == neu.geraet() {
			alt = append(alt, id)
		}
	}
	for _, id := range alt {
		l.endGesperrt(id, GrundErsetzt)
	}
	n := 0
	for _, s := range l.byID {
		if s.AccountID == neu.AccountID {
			n++
		}
	}
	if n >= max {
		return nil, n
	}
	neu.ID = zufall()
	neu.adminID = zufall()
	neu.StartedAt = time.Now()
	neu.zuletzt = neu.StartedAt
	neu.stop = make(chan struct{})
	l.byID[neu.ID] = neu
	return neu, n
}

// EndAdmin beendet eine Wiedergabe über die Kennung, die die Verwaltung
// kennt. Sie meldet, ob es die Wiedergabe gab.
func (l *Live) EndAdmin(kennung string) bool {
	l.mu.Lock()
	e, ok := l.einmal[kennung]
	delete(l.einmal, kennung)
	l.mu.Unlock()
	if !ok || time.Now().After(e.bis) || !l.Laeuft(e.sessionID) {
		return false
	}
	l.End(e.sessionID)
	return true
}

// Gehoert prüft, ob eine Wiedergabe läuft, diesem Konto gehört und diese
// Datei ausliefert. Nur dann darf ein Segment dazu abgeholt werden.
func (l *Live) Gehoert(id, accountID, fileID string) bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	s, ok := l.byID[id]
	return ok && s.AccountID == accountID && s.FileID == fileID
}

// ForAccount sind die laufenden Wiedergaben eines Kontos — für das Konto
// selbst, nie für jemand anderen.
func (l *Live) ForAccount(accountID string) []EigeneWiedergabe {
	l.mu.RLock()
	defer l.mu.RUnlock()
	out := []EigeneWiedergabe{}
	for _, s := range l.byID {
		if s.AccountID == accountID {
			out = append(out, EigeneWiedergabe{ID: s.adminID, DeviceName: s.DeviceName,
				DeviceKind: s.DeviceKind, Mode: s.Mode, Detail: s.detail()})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DeviceName < out[j].DeviceName })
	return out
}

// EndEigene beendet eine eigene Wiedergabe. Fremde bleiben unberührt.
func (l *Live) EndEigene(accountID, adminID string) bool {
	l.mu.RLock()
	id := ""
	for sid, s := range l.byID {
		if s.adminID == adminID && s.AccountID == accountID {
			id = sid
			break
		}
	}
	l.mu.RUnlock()
	if id == "" {
		return false
	}
	l.End(id)
	return true
}

// EndeKonto beendet alle Wiedergaben eines Kontos — wenn die Verwaltung
// dessen PIN zurücksetzt. Sonst liefe auf dem Gerät des Profils noch ein
// Film weiter, obwohl seine Anmeldung schon gelöscht ist. Gibt die Zahl
// zurück (nur für Tests; nach aussen geht sie nicht).
func (l *Live) EndeKonto(accountID string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for id, s := range l.byID {
		if s.AccountID == accountID {
			l.endGesperrt(id, GrundBeendet)
			n++
		}
	}
	return n
}

// EndeKontoMit beendet alle Wiedergaben eines Kontos mit einem Grund und
// gibt ihre Kennungen zurück (damit Umwandlungen mit enden).
func (l *Live) EndeKontoMit(accountID, grund string) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var ids []string
	for id, s := range l.byID {
		if s.AccountID == accountID {
			ids = append(ids, id)
		}
	}
	for _, id := range ids {
		l.endGesperrt(id, grund)
	}
	return ids
}

// KontenMitWiedergabe: die Konten, die gerade etwas laufen haben — nur
// die Kennungen, für die Bildschirmzeit. Nichts davon geht nach aussen.
func (l *Live) KontenMitWiedergabe() map[string]bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	out := map[string]bool{}
	for _, s := range l.byID {
		out[s.AccountID] = true
	}
	return out
}

// LetzteMeldungen: je Konto, wann sich seine Wiedergaben zuletzt gemeldet
// haben (ein Segment geholt, Daten abgeholt). Nur für die Bildschirmzeit;
// nichts davon geht nach aussen.
func (l *Live) LetzteMeldungen() map[string]time.Time {
	l.mu.RLock()
	defer l.mu.RUnlock()
	out := map[string]time.Time{}
	for _, s := range l.byID {
		if s.zuletzt.After(out[s.AccountID]) {
			out[s.AccountID] = s.zuletzt
		}
	}
	return out
}

// DrosselVon: die Drossel einer laufenden Wiedergabe (nil = keine).
func (l *Live) DrosselVon(id string) *Drossel {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if s, ok := l.byID[id]; ok {
		return s.Drossel
	}
	return nil
}

// Melden hält eine Wiedergabe am Leben. Jedes Segment, jede Abspielliste
// ruft das auf — damit ist „läuft noch" eine Beobachtung und keine Annahme.
func (l *Live) Melden(id string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if s, ok := l.byID[id]; ok {
		s.zuletzt = time.Now()
	}
}

// Verwaiste gibt die Wiedergaben zurück, von denen seit frist nichts mehr
// kam.
//
// Das ist nötig, weil ein Gerät nicht immer tschüss sagt: ein Handy
// verliert unterwegs das Netz, ein Fenster wird zugeklappt, der Akku ist
// leer. Ohne Aufräumen bliebe die Wiedergabe für immer stehen — der
// Besitzer sähe ein Gerät, das längst weg ist, und schlimmer: die eigene
// Grenze „gleichzeitig schauen" wäre für diese Person dauerhaft belegt.
//
// Solange eine Anfrage offen ist (Oeffnen), gilt eine Wiedergabe nie als
// verwaist. Eine Wiedergabe über offene Anfragen (Direktwiedergabe,
// Browser-Strom) endet ohne offene Anfrage schon nach
// fristDirekt: Ihre Anfragen kommen dicht hintereinander, und wer
// weiterspielt, bekommt mit der nächsten Bereichsanfrage nahtlos eine neue.
func (l *Live) Verwaiste(frist time.Duration) []string {
	return l.VerwaisteMit(frist, frist)
}

// VerwaisteMit ist Verwaiste mit eigener Frist für Direktwiedergaben.
func (l *Live) VerwaisteMit(frist, fristDirekt time.Duration) []string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	jetzt := time.Now()
	var alt []string
	for id, s := range l.byID {
		if s.offen > 0 {
			continue
		}
		f := frist
		if s.ueberAnfragen && fristDirekt < frist {
			f = fristDirekt
		}
		if s.zuletzt.Before(jetzt.Add(-f)) {
			alt = append(alt, id)
		}
	}
	return alt
}

// Stopped meldet, wenn der Besitzer die Wiedergabe beendet hat.
func (l *Live) Stopped(id string) <-chan struct{} {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if s, ok := l.byID[id]; ok {
		return s.stop
	}
	closed := make(chan struct{})
	close(closed)
	return closed
}

// ForOwner ist die Liste für die Verwaltung. Sie gibt AdminSession zurück,
// nie Session — der innere Typ verlässt dieses Paket nicht.
func (l *Live) ForOwner() []AdminSession {
	l.mu.Lock()
	defer l.mu.Unlock()
	jetzt := time.Now()
	for k, e := range l.einmal {
		if jetzt.After(e.bis) {
			delete(l.einmal, k)
		}
	}
	out := make([]AdminSession, 0, len(l.byID))
	for id, s := range l.byID {
		a := s.From()
		// Jede Abfrage neue Kennungen: Zwei Listen nacheinander lassen sich
		// nicht Zeile für Zeile aufeinander legen.
		a.ID = zufall()
		l.einmal[a.ID] = einmalKennung{sessionID: id, bis: jetzt.Add(einmalGueltig)}
		out = append(out, a)
	}
	// Erst die Umwandlungen (die kosten), sonst nach der Zufallskennung:
	// Die Reihenfolge verrät weder, wer zuerst angefangen hat, noch was
	// zusammengehört.
	sort.Slice(out, func(i, j int) bool {
		if out[i].Mode != out[j].Mode {
			return out[i].Mode == "transcode"
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// CountFor zählt die laufenden Wiedergaben eines Kontos, damit die Grenze
// „gleichzeitig schauen" greift. Das Konto fragt hier nach sich selbst —
// der Besitzer bekommt darüber keine fremden Zahlen zu sehen.
func (l *Live) CountFor(accountID string) int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	n := 0
	for _, s := range l.byID {
		if s.AccountID == accountID {
			n++
		}
	}
	return n
}

// Load sagt, wie viel der Server gerade zu tun hat. Zahlen über alle
// Sitzungen sind unbedenklich: daraus lässt sich nicht ablesen, wer was
// schaut, und der Besitzer braucht sie, um den Server zu betreiben. Die
// Summe der Bitraten fällt weg — bei einer einzigen Wiedergabe war sie
// deren Bitrate.
type Load struct {
	Total      int `json:"total"`
	Transcodes int `json:"transcodes"`
	// Last: grob, wie sehr der Server rechnet — gering, mittel, hoch.
	Last string `json:"last"`
}

func (l *Live) Load() Load {
	l.mu.RLock()
	defer l.mu.RUnlock()
	var out Load
	for _, s := range l.byID {
		out.Total++
		if s.Mode == "transcode" {
			out.Transcodes++
		}
	}
	out.Last = LastStufe(out.Transcodes)
	return out
}

// LastStufe: Umwandeln kostet, direkt Abspielen kaum. Ein NAS schafft
// meist eine oder zwei Umwandlungen ohne Mühe; ab drei wird es eng.
func LastStufe(umwandlungen int) string {
	switch {
	case umwandlungen == 0:
		return "gering"
	case umwandlungen <= 2:
		return "mittel"
	}
	return "hoch"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	buf := [12]byte{}
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// Laeuft sagt, ob es die Wiedergabe noch gibt.
func (l *Live) Laeuft(id string) bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	_, ok := l.byID[id]
	return ok
}
