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
// Der Tag beginnt um Mitternacht Ortszeit, die Fenster gelten in Ortszeit.
// Die Uhr eines Geräts zählt nie mit.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
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

// Regel für den Kalendertag von t.
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

// tagUm: der Kalendertag von t um die Minute min (darf 1440 sein).
func tagUm(t time.Time, min int) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, min/60, min%60, 0, 0, t.Location())
}

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
		return true, tagUm(mitternacht(t).AddDate(0, 0, 1), bis)
	}
	// Nach Mitternacht kann noch das Fenster von gestern laufen.
	gestern := mitternacht(t).AddDate(0, 0, -1)
	rg := b.Regel(gestern)
	if rg.Von != "" {
		gv, _ := uhrzeit(rg.Von, false)
		gb, _ := uhrzeit(rg.Bis, true)
		if gb <= gv {
			if ende := tagUm(t, gb); t.Before(ende) {
				return true, ende
			}
		}
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
// ist die heute schon geschaute Zeit, geschenkt die heute zusätzlich
// erlaubten Minuten (sie verlängern nur die Tagesgrenze, nicht das Fenster).
func (b Bildschirmzeit) Pruefen(jetzt time.Time, genutzt time.Duration, geschenkt int) Urteil {
	if !b.Wirksam() {
		return Urteil{Erlaubt: true, Rest: -1}
	}
	u := Urteil{Budget: b.budget(jetzt, geschenkt)}
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

func (b Bildschirmzeit) budget(t time.Time, geschenkt int) time.Duration {
	r := b.Regel(t)
	if r.Minuten <= 0 {
		return 0
	}
	return time.Duration(r.Minuten+geschenkt) * time.Minute
}

// naechsterBeginn sucht den frühesten Zeitpunkt nach jetzt, an dem es
// wieder geht. Erlaubte Zeit beginnt immer an einer Von-Zeit oder um
// Mitternacht (dann ist die Tagesgrenze wieder frei) — darum genügt es,
// diese Stellen der nächsten Tage durchzugehen.
func (b Bildschirmzeit) naechsterBeginn(jetzt time.Time, genutzt time.Duration, geschenkt int) time.Time {
	heute := mitternacht(jetzt)
	var best time.Time
	for d := 0; d <= 8; d++ {
		tag := heute.AddDate(0, 0, d)
		kandidaten := []time.Time{tag}
		if r := b.Regel(tag); r.Von != "" {
			von, _ := uhrzeit(r.Von, false)
			kandidaten = append(kandidaten, tagUm(tag, von))
		}
		for _, k := range kandidaten {
			if !k.After(jetzt) || (!best.IsZero() && !k.Before(best)) {
				continue
			}
			g, gs := time.Duration(0), 0
			if Tag(k) == Tag(jetzt) {
				g, gs = genutzt, geschenkt
			}
			drin, _ := b.imFenster(k)
			bud := b.budget(k, gs)
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

// BildschirmzeitRegel liest die Regel eines Profils (ohne Regel: aus).
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
		return Bildschirmzeit{}, nil
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

// BildschirmzeitAufraeumen löscht die Zahlen vergangener Tage aller
// Profile — Wartung, liest nichts.
func (s *Store) BildschirmzeitAufraeumen(ctx context.Context, tag string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM bildschirmzeit_tag WHERE tag != ?`, tag)
	return err
}
