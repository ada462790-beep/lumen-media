// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

// Bildschirmzeit: wann und wie lange ein Profil schauen darf.
//
// Das Modell ist absichtlich klein, damit es sich in der Verwaltung mit
// einem Blick einstellen lässt: zwei Tagesarten — Schultage (Montag bis
// Freitag) und Wochenende (Samstag und Sonntag) — mit je einem Zeitfenster
// („07:00 bis 19:30") und einer Tagesgrenze in Minuten („90"). Ein
// Zeitfenster darf über Mitternacht gehen („18:00 bis 02:00"); die Zeit
// nach Mitternacht gehört dann noch zum Fenster des Vortags.
//
// Gezählt wird Video (Filme, Serien), und nur solange es wirklich läuft.
// Laufen auf zwei Geräten gleichzeitig Videos desselben Profils, zählt
// die Minute einmal: Bildschirmzeit ist Uhrzeit, nicht Summe der Geräte.
//
// Datenschutz: Gespeichert wird je Profil eine einzige Zahl für heute
// (Sekunden) und allenfalls die heute geschenkten Minuten — kein Titel,
// keine Uhrzeit, kein Gerät, kein Verlauf. Die Zeile von gestern
// verschwindet beim nächsten Zählen. Die Verwaltung darf „heute 47 von
// 90 Minuten" sehen; was in diesen Minuten lief, sieht sie nicht.
//
// Die Uhr ist die des Servers (Zeitzone aus TZ, siehe zeitzoneMelden):
// Der Tag beginnt um Mitternacht Ortszeit — ausser mitten in einem
// Nachtfenster, das zählt bis zu seinem Ende zum Vortag (Schautag). Die
// Fenster gelten in Ortszeit.
// Die Uhr eines Geräts zählt nie mit.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Tagesregel gilt für eine Tagesart.
type Tagesregel struct {
	// Von und Bis als „HH:MM". Beide leer: den ganzen Tag. Bis darf
	// „24:00" sein; liegt Bis vor Von, geht das Fenster über Mitternacht.
	Von string `json:"von"`
	Bis string `json:"bis"`
	// Minuten am Tag höchstens; 0 = ohne Tagesgrenze.
	Minuten int `json:"minuten"`
}

// Bildschirmzeit ist die Regel eines Profils.
type Bildschirmzeit struct {
	Aktiv      bool       `json:"aktiv"`
	Schultage  Tagesregel `json:"schultage"`  // Montag bis Freitag
	Wochenende Tagesregel `json:"wochenende"` // Samstag und Sonntag
}

// StandardBildschirmzeit ist der Vorschlag, wenn die Verwaltung sie zum
// ersten Mal einschaltet.
func StandardBildschirmzeit() Bildschirmzeit {
	return Bildschirmzeit{
		Schultage:  Tagesregel{Von: "07:00", Bis: "19:30", Minuten: 90},
		Wochenende: Tagesregel{Von: "08:00", Bis: "20:30", Minuten: 120},
	}
}

// MaxGeschenkt: so viele Minuten lassen sich an einem Tag dazugeben.
const MaxGeschenkt = 600

// uhrzeit liest „HH:MM" als Minuten nach Mitternacht. bisErlaubt lässt
// „24:00" zu.
func uhrzeit(s string, bisErlaubt bool) (int, bool) {
	var h, m int
	if len(s) != 5 || s[2] != ':' {
		return 0, false
	}
	if _, err := fmt.Sscanf(s, "%02d:%02d", &h, &m); err != nil {
		return 0, false
	}
	if m < 0 || m > 59 || h < 0 || h > 24 || (h == 24 && (m != 0 || !bisErlaubt)) {
		return 0, false
	}
	return h*60 + m, true
}

// PruefeBildschirmzeit prüft eine Regel und bringt sie in die Form, in
// der sie gespeichert wird (00:00–24:00 heisst „ganzer Tag").
func PruefeBildschirmzeit(b Bildschirmzeit) (Bildschirmzeit, error) {
	for _, r := range []*Tagesregel{&b.Schultage, &b.Wochenende} {
		r.Von, r.Bis = strings.TrimSpace(r.Von), strings.TrimSpace(r.Bis)
		if (r.Von == "") != (r.Bis == "") {
			return b, errors.New("Für das Zeitfenster braucht es von und bis — oder beides leer für den ganzen Tag.")
		}
		if r.Von != "" {
			von, ok1 := uhrzeit(r.Von, false)
			bis, ok2 := uhrzeit(r.Bis, true)
			if !ok1 || !ok2 {
				return b, errors.New("Bitte die Uhrzeiten als 07:30 schreiben.")
			}
			if von == bis || (von == 0 && bis == 24*60) {
				if von == bis && von != 0 {
					return b, errors.New("Von und bis dürfen nicht gleich sein.")
				}
				r.Von, r.Bis = "", ""
			}
		}
		if r.Minuten < 0 || r.Minuten > 24*60 {
			return b, errors.New("Die Tagesgrenze liegt zwischen 0 und 1440 Minuten.")
		}
	}
	return b, nil
}

// Wirksam: die Regel schränkt überhaupt etwas ein.
func (b Bildschirmzeit) Wirksam() bool {
	if !b.Aktiv {
		return false
	}
	for _, r := range []Tagesregel{b.Schultage, b.Wochenende} {
		if r.Von != "" || r.Minuten > 0 {
			return true
		}
	}
	return false
}

// Regel für den Kalendertag von t. Wer wissen will, welche Regel gerade
// gilt, fragt mit dem Schautag (SchautagBeginn), nicht mit der Uhrzeit.
func (b Bildschirmzeit) Regel(t time.Time) Tagesregel {
	switch t.Weekday() {
	case time.Saturday, time.Sunday:
		return b.Wochenende
	}
	return b.Schultage
}

// Tag ist der Kalendertag in Ortszeit, wie er gespeichert wird.
func Tag(t time.Time) string { return t.Format("2006-01-02") }

func mitternacht(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

// tagUm: der Kalendertag von t um die Minute min (darf 1440 sein), als
// Uhrzeit an der Wand. Fällt die Zeit in die Lücke der Umstellung auf
// Sommerzeit (02:30 gibt es dann nicht), gilt der Augenblick der
// Umstellung — sonst liefe ein Fenster bis 02:30 eine Stunde zu lang, bis
// 03:30.
func tagUm(t time.Time, min int) time.Time {
	y, m, d := t.Date()
	k := time.Date(y, m, d, min/60, min%60, 0, 0, t.Location())
	if min >= 24*60 || k.Hour()*60+k.Minute() == min {
		return k
	}
	// In der Lücke: zurück bis zur ersten Minute nach der Umstellung.
	for i := 0; i < 180; i++ {
		v := k.Add(-time.Minute)
		if v.Day() != d || v.Hour()*60+v.Minute() < min {
			break
		}
		k = v
	}
	return k
}

// nachtfensterEnde: Geht das Fenster des Kalendertags tag über
// Mitternacht, das Ende am Folgetag; sonst Nullzeit.
func (b Bildschirmzeit) nachtfensterEnde(tag time.Time) time.Time {
	r := b.Regel(tag)
	if r.Von == "" {
		return time.Time{}
	}
	von, _ := uhrzeit(r.Von, false)
	bis, _ := uhrzeit(r.Bis, true)
	if bis > von {
		return time.Time{}
	}
	return tagUm(mitternacht(tag).AddDate(0, 0, 1), bis)
}

// SchautagBeginn: Mitternacht des Schautags von t. Das ist der
// Kalendertag — ausser nach Mitternacht in einem Fenster, das am Vortag
// begonnen hat („18:00 bis 02:00"): Dann gehört die Zeit noch zum Vortag,
// mit dessen Tagesgrenze und dessen Zähler. Sonst gäbe es um Mitternacht
// mitten im Fenster ein zweites Tagesbudget.
func (b Bildschirmzeit) SchautagBeginn(t time.Time) time.Time {
	heute := mitternacht(t)
	gestern := heute.AddDate(0, 0, -1)
	if ende := b.nachtfensterEnde(gestern); !ende.IsZero() && t.Before(ende) {
		return gestern
	}
	return heute
}

// Schautag: der Schlüssel, unter dem gezählt wird (siehe SchautagBeginn).
func (b Bildschirmzeit) Schautag(t time.Time) string { return Tag(b.SchautagBeginn(t)) }

// imFenster sagt, ob t im Zeitfenster liegt, und bis wann es dauert
// (Nullzeit: ohne Ende, der ganze Tag ist frei).
func (b Bildschirmzeit) imFenster(t time.Time) (bool, time.Time) {
	r := b.Regel(t)
	if r.Von == "" {
		return true, time.Time{}
	}
	von, _ := uhrzeit(r.Von, false)
	bis, _ := uhrzeit(r.Bis, true)
	start := tagUm(t, von)
	if bis > von {
		ende := tagUm(t, bis)
		if !t.Before(start) && t.Before(ende) {
			return true, ende
		}
	} else if !t.Before(start) {
		// Über Mitternacht: bis zur Bis-Zeit des nächsten Tages.
		return true, b.nachtfensterEnde(t)
	}
	// Nach Mitternacht kann noch das Fenster von gestern laufen.
	if ende := b.nachtfensterEnde(mitternacht(t).AddDate(0, 0, -1)); !ende.IsZero() && t.Before(ende) {
		return true, ende
	}
	return false, time.Time{}
}

// Urteil: darf jetzt geschaut werden, und wie lange noch?
type Urteil struct {
	Erlaubt bool
	// Grund, wenn nicht: "fenster" (ausserhalb der Zeit) oder "budget"
	// (Tagesgrenze erreicht).
	Grund string
	// Rest bis zum Ende von Fenster oder Tagesgrenze; < 0 = ohne Ende.
	Rest time.Duration
	// WeiterAb: wann es wieder geht (nur, wenn nicht erlaubt).
	WeiterAb time.Time
	// Budget heute samt Geschenktem; 0 = keine Tagesgrenze.
	Budget time.Duration
}

// Pruefen beurteilt den Zeitpunkt jetzt (in Ortszeit des Servers). genutzt
// ist die am Schautag (siehe Schautag) schon geschaute Zeit, geschenkt die
// an diesem Tag zusätzlich erlaubten Minuten (sie verlängern nur die
// Tagesgrenze, nicht das Fenster).
func (b Bildschirmzeit) Pruefen(jetzt time.Time, genutzt time.Duration, geschenkt int) Urteil {
	if !b.Wirksam() {
		return Urteil{Erlaubt: true, Rest: -1}
	}
	u := Urteil{Budget: b.budget(b.SchautagBeginn(jetzt), geschenkt)}
	drin, ende := b.imFenster(jetzt)
	budgetFrei := u.Budget == 0 || genutzt < u.Budget
	if drin && budgetFrei {
		u.Erlaubt, u.Rest = true, -1
		if !ende.IsZero() {
			u.Rest = ende.Sub(jetzt)
		}
		if u.Budget > 0 && (u.Rest < 0 || u.Budget-genutzt < u.Rest) {
			u.Rest = u.Budget - genutzt
		}
		return u
	}
	u.Grund = "fenster"
	if !budgetFrei {
		u.Grund = "budget"
	}
	u.WeiterAb = b.naechsterBeginn(jetzt, genutzt, geschenkt)
	return u
}

// PruefenMit wie Pruefen, dazu eine Verlängerung des Fensters für heute:
// Bis verlaengertBis (Nullzeit: keine) gilt das Fenster als offen, auch
// ausserhalb der Regel — „heute einmalig 30 Min. mehr" um 22:15, wenn das
// Fenster um 19:30 zu ging. Die Tagesgrenze gilt weiter (samt
// Geschenktem).
func (b Bildschirmzeit) PruefenMit(jetzt time.Time, genutzt time.Duration, geschenkt int, verlaengertBis time.Time) Urteil {
	u := b.Pruefen(jetzt, genutzt, geschenkt)
	if verlaengertBis.IsZero() || !jetzt.Before(verlaengertBis) || !b.Wirksam() {
		return u
	}
	if u.Erlaubt {
		// Schon offen: Die Verlängerung kann das Ende hinausschieben.
		if u.Rest >= 0 && verlaengertBis.Sub(jetzt) > u.Rest {
			r := verlaengertBis.Sub(jetzt)
			if u.Budget > 0 && u.Budget-genutzt < r {
				r = u.Budget - genutzt
			}
			u.Rest = r
		}
		return u
	}
	if u.Grund != "fenster" {
		return u // Tagesgrenze erreicht: die gilt
	}
	r := verlaengertBis.Sub(jetzt)
	if u.Budget > 0 {
		if genutzt >= u.Budget {
			return u
		}
		if u.Budget-genutzt < r {
			r = u.Budget - genutzt
		}
	}
	return Urteil{Erlaubt: true, Rest: r, Budget: u.Budget}
}

// FensterEnde: wann das Fenster des Schautags von t endet (Nullzeit: kein
// Fenster, der ganze Tag ist frei).
func (b Bildschirmzeit) FensterEnde(t time.Time) time.Time {
	tag := b.SchautagBeginn(t)
	r := b.Regel(tag)
	if r.Von == "" {
		return time.Time{}
	}
	if e := b.nachtfensterEnde(tag); !e.IsZero() {
		return e
	}
	bis, _ := uhrzeit(r.Bis, true)
	return tagUm(tag, bis)
}

// NachtfensterVorbei: Heute nach Mitternacht ist ein Fenster des Vortags
// (über Mitternacht) schon zu Ende gegangen — für die Texte: Dann ist es
// keine „Pause", sondern Schluss bis zum nächsten Fenster.
func (b Bildschirmzeit) NachtfensterVorbei(t time.Time) bool {
	e := b.nachtfensterEnde(mitternacht(t).AddDate(0, 0, -1))
	return !e.IsZero() && !t.Before(e)
}

// budget: die Tagesgrenze des Schautags, der um tag (Mitternacht) beginnt.
func (b Bildschirmzeit) budget(tag time.Time, geschenkt int) time.Duration {
	r := b.Regel(tag)
	if r.Minuten <= 0 {
		return 0
	}
	return time.Duration(r.Minuten+geschenkt) * time.Minute
}

// naechsterBeginn sucht den frühesten Zeitpunkt nach jetzt, an dem es
// wieder geht. Erlaubte Zeit beginnt immer an einer Von-Zeit, um
// Mitternacht oder am Ende eines Nachtfensters (dann beginnt ein neuer
// Schautag mit freier Tagesgrenze) — darum genügt es, diese Stellen der
// nächsten Tage durchzugehen.
func (b Bildschirmzeit) naechsterBeginn(jetzt time.Time, genutzt time.Duration, geschenkt int) time.Time {
	heute := mitternacht(jetzt)
	schautag := b.Schautag(jetzt)
	var best time.Time
	for d := 0; d <= 8; d++ {
		tag := heute.AddDate(0, 0, d)
		kandidaten := []time.Time{tag}
		if r := b.Regel(tag); r.Von != "" {
			von, _ := uhrzeit(r.Von, false)
			kandidaten = append(kandidaten, tagUm(tag, von))
		}
		if ende := b.nachtfensterEnde(tag.AddDate(0, 0, -1)); !ende.IsZero() {
			kandidaten = append(kandidaten, ende)
		}
		for _, k := range kandidaten {
			if !k.After(jetzt) || (!best.IsZero() && !k.Before(best)) {
				continue
			}
			g, gs := time.Duration(0), 0
			if b.Schautag(k) == schautag {
				g, gs = genutzt, geschenkt
			}
			drin, _ := b.imFenster(k)
			bud := b.budget(b.SchautagBeginn(k), gs)
			if drin && (bud == 0 || g < bud) {
				best = k
			}
		}
		if !best.IsZero() && best.Before(heute.AddDate(0, 0, d+1)) {
			break
		}
	}
	return best
}

// ---------------------------------------------------------------- Speicher

// bildschirmzeitNachruesten legt die Tabellen an (Schema-Stand 12).
// Eigene Funktion, damit sie sich beim Zusammenführen mit anderen
// Schemaänderungen leicht umnummerieren lässt.
func bildschirmzeitNachruesten(db *sql.DB) error {
	for _, stmt := range []string{
		// Die Regel je Profil, als JSON (Bildschirmzeit).
		`CREATE TABLE IF NOT EXISTS bildschirmzeit_regel (
		   account_id TEXT PRIMARY KEY REFERENCES account(id) ON DELETE CASCADE,
		   regel      TEXT NOT NULL)`,
		// Heute geschaut, in Sekunden — eine Zeile je Profil und Tag, und
		// nur für heute (ältere löscht das Zählen). Kein Titel, keine Uhrzeit.
		`CREATE TABLE IF NOT EXISTS bildschirmzeit_tag (
		   account_id TEXT NOT NULL REFERENCES account(id) ON DELETE CASCADE,
		   tag        TEXT NOT NULL,
		   sekunden   INTEGER NOT NULL DEFAULT 0,
		   geschenkt  INTEGER NOT NULL DEFAULT 0,
		   PRIMARY KEY (account_id, tag))`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

// ErrRegelUnlesbar: Für das Profil steht eine Regel da, die sich nicht
// lesen lässt. Dann gilt nicht „keine Regel", sondern: gesperrt.
var ErrRegelUnlesbar = errors.New("Die Regel für die Bildschirmzeit liess sich nicht lesen. Bitte neu speichern.")

// BildschirmzeitRegel liest die Regel eines Profils (ohne Regel: aus).
// Eine Zeile, die sich nicht lesen lässt, meldet ErrRegelUnlesbar.
func (s *Store) BildschirmzeitRegel(ctx context.Context, accountID string) (Bildschirmzeit, error) {
	var raw string
	err := s.db.QueryRowContext(ctx,
		`SELECT regel FROM bildschirmzeit_regel WHERE account_id = ?`, accountID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return Bildschirmzeit{}, nil
	}
	if err != nil {
		return Bildschirmzeit{}, err
	}
	var b Bildschirmzeit
	if err := json.Unmarshal([]byte(raw), &b); err != nil {
		return Bildschirmzeit{}, ErrRegelUnlesbar
	}
	if _, err := PruefeBildschirmzeit(b); err != nil {
		return Bildschirmzeit{}, ErrRegelUnlesbar
	}
	return b, nil
}

// SetBildschirmzeit speichert die Regel (vorher PruefeBildschirmzeit).
func (s *Store) SetBildschirmzeit(ctx context.Context, accountID string, b Bildschirmzeit) error {
	b, err := PruefeBildschirmzeit(b)
	if err != nil {
		return err
	}
	raw, _ := json.Marshal(b)
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO bildschirmzeit_regel (account_id, regel) VALUES (?, ?)
		 ON CONFLICT (account_id) DO UPDATE SET regel = excluded.regel`, accountID, string(raw))
	return err
}

// BildschirmzeitHeute: heute geschaut und heute geschenkt.
func (s *Store) BildschirmzeitHeute(ctx context.Context, accountID, tag string) (time.Duration, int, error) {
	var sek, geschenkt int
	err := s.db.QueryRowContext(ctx,
		`SELECT sekunden, geschenkt FROM bildschirmzeit_tag WHERE account_id = ? AND tag = ?`,
		accountID, tag).Scan(&sek, &geschenkt)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, nil
	}
	return time.Duration(sek) * time.Second, geschenkt, err
}

// BildschirmzeitZaehlen rechnet Sekunden auf den Tag an. Die Zeile von
// gestern verschwindet dabei — es bleibt nie mehr als heute stehen.
func (s *Store) BildschirmzeitZaehlen(ctx context.Context, accountID, tag string, sekunden int) error {
	return s.tx(ctx, func(t *sql.Tx) error {
		if _, err := t.ExecContext(ctx,
			`DELETE FROM bildschirmzeit_tag WHERE account_id = ? AND tag != ?`, accountID, tag); err != nil {
			return err
		}
		_, err := t.ExecContext(ctx,
			`INSERT INTO bildschirmzeit_tag (account_id, tag, sekunden) VALUES (?, ?, ?)
			 ON CONFLICT (account_id, tag) DO UPDATE SET sekunden = sekunden + excluded.sekunden`,
			accountID, tag, sekunden)
		return err
	})
}

// BildschirmzeitSchenken gibt heute Minuten dazu (höchstens MaxGeschenkt
// insgesamt) und meldet, wie viele es jetzt sind.
func (s *Store) BildschirmzeitSchenken(ctx context.Context, accountID, tag string, minuten int) (int, error) {
	if minuten <= 0 || minuten > MaxGeschenkt {
		return 0, errors.New("So viele Minuten lassen sich nicht dazugeben.")
	}
	var neu int
	err := s.tx(ctx, func(t *sql.Tx) error {
		if _, err := t.ExecContext(ctx,
			`DELETE FROM bildschirmzeit_tag WHERE account_id = ? AND tag != ?`, accountID, tag); err != nil {
			return err
		}
		var alt int
		err := t.QueryRowContext(ctx,
			`SELECT geschenkt FROM bildschirmzeit_tag WHERE account_id = ? AND tag = ?`,
			accountID, tag).Scan(&alt)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		neu = alt + minuten
		if neu > MaxGeschenkt {
			neu = MaxGeschenkt
		}
		_, err = t.ExecContext(ctx,
			`INSERT INTO bildschirmzeit_tag (account_id, tag, geschenkt) VALUES (?, ?, ?)
			 ON CONFLICT (account_id, tag) DO UPDATE SET geschenkt = excluded.geschenkt`,
			accountID, tag, neu)
		return err
	})
	return neu, err
}

// Verlängerung des Fensters für heute: als Einstellung
// „bz.verlaengert:<konto>" = „<schautag> <unix-ende>". Eine Handlung der
// Verwaltung, nichts über das Schauen; sie gilt nur am selben Schautag.

// BildschirmzeitVerlaengerung: bis wann das Fenster am Schautag tag
// verlängert ist (Nullzeit: nicht).
func (s *Store) BildschirmzeitVerlaengerung(ctx context.Context, accountID, tag string) time.Time {
	v := s.Setting(ctx, "bz.verlaengert:"+accountID, "")
	t, rest, ok := strings.Cut(v, " ")
	if !ok || t != tag {
		return time.Time{}
	}
	n, err := strconv.ParseInt(rest, 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.Unix(n, 0)
}

// BildschirmzeitVerlaengern setzt das Ende der Verlängerung für heute.
func (s *Store) BildschirmzeitVerlaengern(ctx context.Context, accountID, tag string, bis time.Time) error {
	return s.SetSetting(ctx, "bz.verlaengert:"+accountID, tag+" "+strconv.FormatInt(bis.Unix(), 10))
}

// BildschirmzeitGeschenkZurueck nimmt alles zurück, was heute dazugegeben
// wurde: die Minuten und die Verlängerung des Fensters.
func (s *Store) BildschirmzeitGeschenkZurueck(ctx context.Context, accountID, tag string) error {
	if _, err := s.db.ExecContext(ctx,
		`UPDATE bildschirmzeit_tag SET geschenkt = 0 WHERE account_id = ? AND tag = ?`, accountID, tag); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM setting WHERE key = ?`, "bz.verlaengert:"+accountID)
	return err
}

// BildschirmzeitAufraeumen löscht die Zahlen aller Profile, die älter als
// behaltenAb sind — Wartung, liest nichts. Der Aufrufer gibt gestern an:
// Nach Mitternacht kann ein Nachtfenster noch zum Vortag zählen; die Zeile
// von gestern löscht dann das nächste Zählen des Profils selbst.
func (s *Store) BildschirmzeitAufraeumen(ctx context.Context, behaltenAb string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM bildschirmzeit_tag WHERE tag < ?`, behaltenAb)
	return err
}
