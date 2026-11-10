// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

// Gruppen: der Besitzer legt Gruppen an — „Familie",
// „Gäste", „Mitverwalter" — und steckt Konten hinein. Eine Gruppe sagt,
// welche Bibliotheken ihre Mitglieder sehen, welche Grenzen gelten und
// ob sie in die Verwaltung dürfen.
//
// Wer in einer Gruppe ist, bekommt deren Bibliotheken und Grenzen; die
// eigenen Einträge am Konto ruhen solange. Nimmt man das Konto wieder
// heraus, gelten sie wieder. So geht beim Umsortieren nichts verloren.
//
// Verwaltung heisst: dieselbe Verwaltung wie der Besitzer — mit derselben
// Grenze. Auch ein Mitverwalter sieht nicht, wer was schaut.
//
// Dazu die Verwaltungssperre: jede Person mit Zugang zur Verwaltung kann
// sich einen eigenen PIN, ein Passwort oder einen Code aus einer
// Authenticator-App (TOTP) setzen, ohne den die Verwaltung zu bleibt.
// Über E-Mail oder SMS läuft nichts.

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"crypto/pbkdf2"
)

type Gruppe struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Verwaltung bool   `json:"verwaltung"`
	// Vollzugriff: Verwaltung mit allen Rechten des Besitzers — Updates,
	// Gruppen, Bibliotheken anlegen, alle Bibliotheken, keine
	// Altersgrenze. Nur das Konto des Besitzers selbst bleibt unantastbar.
	// In der Spalte verwaltung steht dann 2 (0 nein, 1 Mitverwalten).
	Vollzugriff bool     `json:"vollzugriff"`
	Libraries   []string `json:"libraries"`
	Limits      Limits   `json:"limits"`
	Mitglieder  int      `json:"mitglieder"`
	// Rolle: bei den zwei festen Gruppen hinter „Konten und Rechte"
	// („verwalten" oder „voll"). Sie sind keine Gruppen, die jemand
	// anlegt: Name und Grenzen stehen fest, auflösen geht nicht – Rechte
	// vergibt man am Konto.
	Rolle string `json:"rolle,omitempty"`
}

// RollenGruppe sagt, ob id eine der festen Gruppen einer Rolle ist, und
// welche Rolle.
func RollenGruppe(id string) (rolle string, ok bool) {
	switch id {
	case GruppeMitverwalter:
		return "verwalten", true
	case GruppeVollzugriff:
		return "voll", true
	}
	return "", false
}

// rolleEintragen: Rolle und die Grenzen, die für Verwaltende ohnehin
// gelten (VerwaltungsLimits) – damit nirgends „höchstens 4 gleichzeitig"
// steht, wo in Wahrheit keine Grenze gilt.
func (g *Gruppe) rolleEintragen() {
	if r, ok := RollenGruppe(g.ID); ok {
		g.Rolle, g.Limits = r, VerwaltungsLimits()
	}
}

// stufe: 0 nur schauen, 1 mitverwalten, 2 Vollzugriff.
func (g Gruppe) stufe() int {
	switch {
	case g.Vollzugriff:
		return 2
	case g.Verwaltung:
		return 1
	}
	return 0
}

func (s *Store) Gruppen(ctx context.Context) ([]Gruppe, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT g.id, g.name, g.verwaltung, g.limits,
		        (SELECT count(*) FROM account a WHERE a.group_id = g.id AND a.disabled = 0)
		   FROM gruppe g`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Gruppe{}
	for rows.Next() {
		var g Gruppe
		var verw int
		var lim string
		if err := rows.Scan(&g.ID, &g.Name, &verw, &lim, &g.Mitglieder); err != nil {
			return nil, err
		}
		g.Verwaltung, g.Vollzugriff = verw >= 1, verw == 2
		g.Limits = limitsAus(lim)
		g.rolleEintragen()
		out = append(out, g)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	nachNamenSortieren(out, func(g *Gruppe) string { return g.Name }, func(g *Gruppe) string { return g.ID }, nil)
	for i := range out {
		if out[i].Libraries, err = s.gruppenBibliotheken(ctx, out[i].ID); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *Store) Gruppe(ctx context.Context, id string) (*Gruppe, error) {
	var g Gruppe
	var verw int
	var lim string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, verwaltung, limits FROM gruppe WHERE id = ?`, id).
		Scan(&g.ID, &g.Name, &verw, &lim)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	g.Verwaltung, g.Vollzugriff = verw >= 1, verw == 2
	g.Limits = limitsAus(lim)
	g.rolleEintragen()
	g.Libraries, err = s.gruppenBibliotheken(ctx, id)
	return &g, err
}

func (s *Store) gruppenBibliotheken(ctx context.Context, id string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT library_id FROM gruppe_bibliothek WHERE group_id = ? ORDER BY library_id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func limitsAus(raw string) Limits {
	l := DefaultLimits(false)
	if raw != "" {
		_ = json.Unmarshal([]byte(raw), &l)
	}
	if l.MaxStreams == 0 {
		l.MaxStreams = 2
	}
	return l
}

// GruppeAnlegen legt eine Gruppe an; GruppeAendern schreibt sie neu.
func (s *Store) GruppeAnlegen(ctx context.Context, g Gruppe) (*Gruppe, error) {
	g.Name = strings.TrimSpace(g.Name)
	if g.Name == "" || len(g.Name) > 60 {
		return nil, errors.New("name fehlt oder ist zu lang")
	}
	g.ID = newID("grp")
	if g.Limits.MaxStreams == 0 {
		g.Limits = DefaultLimits(false)
	}
	lim, _ := json.Marshal(g.Limits)
	err := s.tx(ctx, func(t *sql.Tx) error {
		if _, err := t.ExecContext(ctx,
			`INSERT INTO gruppe (id, name, verwaltung, limits) VALUES (?,?,?,?)`,
			g.ID, g.Name, g.stufe(), string(lim)); err != nil {
			return err
		}
		return gruppeBibliothekenSetzen(ctx, t, g.ID, g.Libraries)
	})
	if err != nil {
		return nil, err
	}
	return s.Gruppe(ctx, g.ID)
}

func (s *Store) GruppeAendern(ctx context.Context, g Gruppe) error {
	g.Name = strings.TrimSpace(g.Name)
	if g.Name == "" || len(g.Name) > 60 {
		return errors.New("name fehlt oder ist zu lang")
	}
	if g.Limits.MaxStreams == 0 {
		g.Limits = DefaultLimits(false)
	}
	// Bekommt die Gruppe Verwaltung, muss jedes Mitglied sie haben dürfen.
	if g.stufe() > 0 {
		if err := s.mitgliederDuerfenVerwalten(ctx, g.ID); err != nil {
			return err
		}
	}
	lim, _ := json.Marshal(g.Limits)
	return s.tx(ctx, func(t *sql.Tx) error {
		res, err := t.ExecContext(ctx,
			`UPDATE gruppe SET name = ?, verwaltung = ?, limits = ? WHERE id = ?`,
			g.Name, g.stufe(), string(lim), g.ID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		return gruppeBibliothekenSetzen(ctx, t, g.ID, g.Libraries)
	})
}

func gruppeBibliothekenSetzen(ctx context.Context, t *sql.Tx, id string, libs []string) error {
	if _, err := t.ExecContext(ctx, `DELETE FROM gruppe_bibliothek WHERE group_id = ?`, id); err != nil {
		return err
	}
	for _, l := range libs {
		if _, err := t.ExecContext(ctx,
			`INSERT OR IGNORE INTO gruppe_bibliothek (group_id, library_id) VALUES (?,?)`, id, l); err != nil {
			return err
		}
	}
	return nil
}

// GruppeLoeschen löst die Gruppe auf. Ihre Mitglieder bekommen dabei je
// die strengere Hälfte aus ihren ruhenden eigenen Einträgen und denen der
// Gruppe (gruppeVerlassen) — auflösen öffnet nie mehr, als vorher galt.
func (s *Store) GruppeLoeschen(ctx context.Context, id string) error {
	return s.tx(ctx, func(t *sql.Tx) error {
		rows, err := t.QueryContext(ctx, `SELECT id FROM account WHERE group_id = ?`, id)
		if err != nil {
			return err
		}
		mitglieder, err := collectStrings(rows)
		rows.Close()
		if err != nil {
			return err
		}
		for _, m := range mitglieder {
			if err := gruppeVerlassen(ctx, t, m, id); err != nil {
				return err
			}
		}
		if _, err := t.ExecContext(ctx, `UPDATE account SET group_id = NULL WHERE group_id = ?`, id); err != nil {
			return err
		}
		if _, err := t.ExecContext(ctx, `DELETE FROM gruppe_bibliothek WHERE group_id = ?`, id); err != nil {
			return err
		}
		res, err := t.ExecContext(ctx, `DELETE FROM gruppe WHERE id = ?`, id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// GruppeZuweisen steckt ein Konto in eine Gruppe (leer = in keine). Der
// Besitzer gehört in keine — er darf ohnehin alles.
func (s *Store) GruppeZuweisen(ctx context.Context, accountID, groupID string) error {
	acc, err := s.Account(ctx, accountID)
	if errors.Is(err, ErrNotFound) || (err == nil && acc.IsOwner) {
		return errors.New("konto nicht gefunden oder es ist der besitzer")
	}
	if err != nil {
		return err
	}
	if groupID != "" {
		g, err := s.Gruppe(ctx, groupID)
		if err != nil {
			return err
		}
		if g.stufe() > 0 {
			if err := verwaltungErlaubt(acc); err != nil {
				return err
			}
		}
	}
	if acc.GroupID == groupID {
		return nil
	}
	var gid any
	if groupID != "" {
		gid = groupID
	}
	return s.tx(ctx, func(t *sql.Tx) error {
		// Wer eine Gruppe verlässt, behält deren Grenzen, wo sie strenger
		// waren als die eigenen (siehe gruppeVerlassen).
		if acc.GroupID != "" {
			if err := gruppeVerlassen(ctx, t, accountID, acc.GroupID); err != nil {
				return err
			}
		}
		res, err := t.ExecContext(ctx,
			`UPDATE account SET group_id = ? WHERE id = ? AND is_owner = 0`, gid, accountID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return errors.New("konto nicht gefunden oder es ist der besitzer")
		}
		return nil
	})
}

// mitgliederDuerfenVerwalten prüft vor dem Hochstufen einer Gruppe, ob
// jedes Mitglied Verwaltung bekommen darf.
func (s *Store) mitgliederDuerfenVerwalten(ctx context.Context, groupID string) error {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM account WHERE group_id = ?`, groupID)
	if err != nil {
		return err
	}
	ids, err := collectStrings(rows)
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		acc, err := s.Account(ctx, id)
		if err != nil {
			return err
		}
		if err := verwaltungErlaubt(acc); err != nil {
			return fmt.Errorf("%s: %w", acc.Name, err)
		}
	}
	return nil
}

// ---------------------------------------------------------- Verwaltungssperre

// VerwaltungsSperre sagt, womit diese Person die Verwaltung schützt:
// none, pin, password oder totp.
func (s *Store) VerwaltungsSperre(ctx context.Context, accountID string) (string, error) {
	var kind string
	err := s.db.QueryRowContext(ctx,
		`SELECT admin_lock_kind FROM account WHERE id = ?`, accountID).Scan(&kind)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if kind == "" {
		kind = "none"
	}
	return kind, err
}

// VerwaltungsSperreSetzen legt PIN oder Passwort fest; für TOTP steht in
// secret das Geheimnis (Base32), geprüft hat der Aufrufer es schon.
func (s *Store) VerwaltungsSperreSetzen(ctx context.Context, accountID, kind, secret string) error {
	switch kind {
	case "none":
		_, err := s.db.ExecContext(ctx,
			`UPDATE account SET admin_lock_kind='none', admin_hash=NULL, admin_salt=NULL, admin_totp=NULL WHERE id=?`,
			accountID)
		return err
	case "pin":
		if len(secret) < 4 || strings.Trim(secret, "0123456789") != "" {
			return errors.New("pin braucht mindestens vier ziffern")
		}
	case "password":
		if len(secret) < 8 {
			return errors.New("passwort braucht mindestens acht zeichen")
		}
	case "totp":
		if secret == "" {
			return errors.New("geheimnis fehlt")
		}
		_, err := s.db.ExecContext(ctx,
			`UPDATE account SET admin_lock_kind='totp', admin_hash=NULL, admin_salt=NULL, admin_totp=? WHERE id=?`,
			secret, accountID)
		return err
	default:
		return errors.New("unbekannte sperre")
	}
	salt, hash, err := hashSecret(secret)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`UPDATE account SET admin_lock_kind=?, admin_hash=?, admin_salt=?, admin_totp=NULL WHERE id=?`,
		kind, hash, salt, accountID)
	return err
}

// VerwaltungsGeheimnis gibt das TOTP-Geheimnis zurück — nur für die
// Prüfung eines Codes, nie für eine Antwort nach draussen.
func (s *Store) VerwaltungsGeheimnis(ctx context.Context, accountID string) (string, error) {
	var totp sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT admin_totp FROM account WHERE id = ?`, accountID).Scan(&totp)
	return totp.String, err
}

// VerwaltungsSperrePruefen prüft PIN oder Passwort in gleichbleibender Zeit.
func (s *Store) VerwaltungsSperrePruefen(ctx context.Context, accountID, secret string) error {
	var kind string
	var salt, want []byte
	err := s.db.QueryRowContext(ctx,
		`SELECT admin_lock_kind, admin_salt, admin_hash FROM account WHERE id = ?`, accountID).
		Scan(&kind, &salt, &want)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if kind == "none" || kind == "" {
		return nil
	}
	if kind == "totp" {
		return errors.New("diese sperre prüft man mit einem code")
	}
	got, err := pbkdf2.Key(sha256.New, secret, salt, pbkdf2Rounds, 32)
	if err != nil {
		return err
	}
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return ErrLocked
	}
	return nil
}

// Die zwei festen Gruppen für Rechte, die man direkt am Konto vergibt
// („Konten und Rechte" › Rechte). Wer nur jemandem die Verwaltung geben
// will, muss so nicht erst selbst eine Gruppe bauen.
const (
	GruppeMitverwalter = "grp_mitverwalter"
	GruppeVollzugriff  = "grp_vollzugriff"
)

// RolleSetzen gibt einem Konto Rechte: "schauen", "verwalten" oder
// "voll". Für die beiden Verwaltungsstufen wird das Konto in die feste
// Gruppe gesteckt (die bei Bedarf entsteht, mit allen Bibliotheken).
// "schauen" nimmt es nur aus einer Gruppe mit Verwaltung heraus; eine
// gewöhnliche Gruppe (Familie, Gäste) bleibt.
func (s *Store) RolleSetzen(ctx context.Context, accountID, rolle string) error {
	acc, err := s.Account(ctx, accountID)
	if err != nil {
		return err
	}
	if acc.IsOwner {
		return errors.New("der besitzer hat ohnehin alle rechte")
	}
	switch rolle {
	case "schauen":
		if !acc.IsAdmin {
			return nil
		}
		return s.GruppeZuweisen(ctx, accountID, "")
	case "verwalten", "voll":
		if err := verwaltungErlaubt(acc); err != nil {
			return err
		}
		id, name, voll := GruppeMitverwalter, "Mitverwalter", false
		if rolle == "voll" {
			id, name, voll = GruppeVollzugriff, "Vollzugriff", true
		}
		if _, err := s.Gruppe(ctx, id); errors.Is(err, ErrNotFound) {
			libs, err := s.Libraries(ctx)
			if err != nil {
				return err
			}
			ids := make([]string, 0, len(libs))
			for _, l := range libs {
				ids = append(ids, l.ID)
			}
			limJSON, _ := json.Marshal(VerwaltungsLimits())
			g := Gruppe{ID: id, Verwaltung: true, Vollzugriff: voll}
			if err := s.tx(ctx, func(t *sql.Tx) error {
				if _, err := t.ExecContext(ctx,
					`INSERT INTO gruppe (id, name, verwaltung, limits) VALUES (?,?,?,?)`,
					id, name, g.stufe(), string(limJSON)); err != nil {
					return err
				}
				return gruppeBibliothekenSetzen(ctx, t, id, ids)
			}); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		return s.GruppeZuweisen(ctx, accountID, id)
	}
	return errors.New("unbekannte rolle")
}
