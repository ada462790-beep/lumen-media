// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"crypto/pbkdf2"
)

// pbkdf2Rounds ist bewusst hoch genug, dass sich ein geratener PIN nicht
// millionenfach pro Sekunde durchprobieren lässt, aber niedrig genug, dass
// ein NAS die Anmeldung in unter einer Zehntelsekunde schafft.
const pbkdf2Rounds = 210_000

var ErrLocked = errors.New("falscher code")

type Account struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Avatar    string     `json:"avatar"`
	IsOwner   bool       `json:"isOwner"`
	IsKid     bool       `json:"isKid"`
	LockKind  string     `json:"lockKind"` // none | pin | password
	Disabled  bool       `json:"disabled"`
	CreatedAt time.Time  `json:"createdAt"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
	// Gruppe, falls das Konto in einer steckt.
	GroupID   string `json:"groupId,omitempty"`
	GroupName string `json:"groupName,omitempty"`
	// IsAdmin: darf in die Verwaltung — der Besitzer immer, sonst über
	// die Gruppe. Dieselbe Verwaltung, dieselbe Grenze: auch ein
	// Mitverwalter sieht nicht, wer was schaut.
	IsAdmin bool `json:"isAdmin"`
	// Vollzugriff: über die Gruppe dieselben Rechte wie der Besitzer.
	// IsOwner bleibt dem einen echten Besitzer vorbehalten.
	Vollzugriff bool `json:"vollzugriff,omitempty"`
	// AdminLock: womit die Person die Verwaltung schützt (none | pin |
	// password | totp). Nur für sie selbst interessant.
	AdminLock string `json:"adminLock,omitempty"`
}

// Besitzerrechte: der Besitzer selbst oder jemand mit Vollzugriff.
func (a *Account) Besitzerrechte() bool { return a != nil && (a.IsOwner || a.Vollzugriff) }

type Limits struct {
	MaxAge         int    `json:"maxAge"`
	AllowUnrated   bool   `json:"allowUnrated"`
	MaxStreams     int    `json:"maxStreams"`
	AllowTranscode bool   `json:"allowTranscode"`
	AllowDownload  bool   `json:"allowDownload"`
	MaxDownloads   int    `json:"maxDownloads"`
	RemoteQuality  string `json:"remoteQuality"`
}

// VerwaltungsLimits gelten für Besitzer, Vollzugriff und Mitverwalter:
// alles erlaubt, Original-Qualität auch unterwegs.
func VerwaltungsLimits() Limits {
	return Limits{MaxAge: OhneAltersgrenze, AllowUnrated: true, MaxStreams: 20,
		AllowTranscode: true, AllowDownload: true, MaxDownloads: 1000,
		RemoteQuality: "original"}
}

func DefaultLimits(kid bool) Limits {
	l := Limits{MaxAge: 18, MaxStreams: 2, AllowTranscode: true,
		AllowDownload: true, MaxDownloads: 20, RemoteQuality: "1080p",
		// Erwachsene sehen auch Titel, für die noch keine Freigabe
		// hinterlegt ist — sonst wäre eine frische Mediathek für alle
		// ausser dem Besitzer leer, bis die Metadaten da sind.
		AllowUnrated: true}
	if kid {
		// Bei einem Kinderprofil gilt das Gegenteil: was nicht geprüft ist,
		// bleibt draussen.
		l.MaxAge = 12
		l.AllowDownload = false
		l.AllowUnrated = false
	}
	return l
}

// ---------------------------------------------------------------- anlegen

type NewAccount struct {
	Name     string
	Avatar   string
	IsOwner  bool
	IsKid    bool
	LockKind string
	Secret   string // PIN oder Passwort im Klartext; wird sofort gehasht
	Grants   []string
	Limits   Limits
}

// Was in ein Profil darf. Alles, was ein Gast selbst wählt, landet später
// in fremden Browsern (Profilauswahl) — darum nur bekannte Werte.
var (
	reAvatar       = regexp.MustCompile(`^(bild|av-[a-z0-9-]{1,20}|fig-[a-z0-9-]{1,20})$`)
	sperrArten     = map[string]bool{"none": true, "pin": true, "password": true}
	maxNamenLaenge = 40
)

// PruefeProfil prüft Name, Bild und Sperrart, bevor sie gespeichert werden.
func PruefeProfil(name, avatar, lockKind string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("name fehlt")
	}
	if utf8.RuneCountInString(name) > maxNamenLaenge {
		return errors.New("der name ist zu lang (höchstens 40 zeichen)")
	}
	if avatar != "" && !reAvatar.MatchString(avatar) {
		return errors.New("unbekanntes profilbild")
	}
	if lockKind != "" && !sperrArten[lockKind] {
		return errors.New("unbekannte sperre")
	}
	return nil
}

func (s *Store) CreateAccount(ctx context.Context, in NewAccount) (*Account, error) {
	in.Name = strings.TrimSpace(in.Name)
	if err := PruefeProfil(in.Name, in.Avatar, in.LockKind); err != nil {
		return nil, err
	}
	if in.LockKind == "" {
		in.LockKind = "none"
	}
	if in.LockKind != "none" && in.Secret == "" {
		return nil, errors.New("code fehlt")
	}
	if in.LockKind == "pin" && len(in.Secret) < 4 {
		return nil, errors.New("pin braucht mindestens vier ziffern")
	}
	if in.LockKind == "pin" && strings.Trim(in.Secret, "0123456789") != "" {
		return nil, errors.New("eine pin besteht nur aus ziffern")
	}
	if in.LockKind == "password" && len(in.Secret) < 8 {
		return nil, errors.New("passwort braucht mindestens acht zeichen")
	}

	var salt, hash []byte
	if in.LockKind != "none" {
		var err error
		if salt, hash, err = hashSecret(in.Secret); err != nil {
			return nil, err
		}
	}
	ohneBild := in.Avatar == ""
	if ohneBild {
		in.Avatar = "av-a"
	}

	acc := &Account{
		ID: newID("acc"), Name: in.Name, Avatar: in.Avatar,
		IsOwner: in.IsOwner, IsKid: in.IsKid, LockKind: in.LockKind,
		CreatedAt: time.Now(),
	}
	lim := in.Limits
	if lim.MaxStreams == 0 {
		lim = DefaultLimits(in.IsKid)
	}
	if in.IsKid {
		// Ein Kinderprofil entsteht nie mit Erwachsenen-Grenzen, egal
		// was mitkam.
		if err := PruefeKinderGrenzen(lim); err != nil {
			return nil, err
		}
	}

	err := s.tx(ctx, func(t *sql.Tx) error {
		// Im selben Schritt wie das Anlegen, damit zwei gleichzeitige
		// Einladungen nicht beide „Alex" bekommen.
		if vergeben, err := nameVergeben(ctx, t, acc.Name, ""); err != nil {
			return err
		} else if vergeben {
			return ErrNameVergeben
		}
		// Ohne gewähltes Bild: die Farbe, die im Haushalt am seltensten
		// vorkommt — sonst waren auf „Wer schaut?" alle Kreise gleich blau.
		if ohneBild {
			f, err := seltensteFarbe(ctx, t)
			if err != nil {
				return err
			}
			acc.Avatar = f
		}
		_, err := t.ExecContext(ctx,
			`INSERT INTO account (id, name, avatar, is_owner, is_kid, lock_kind,
			   secret_hash, secret_salt, created_at)
			 VALUES (?,?,?,?,?,?,?,?,?)`,
			acc.ID, acc.Name, acc.Avatar, b2i(acc.IsOwner), b2i(acc.IsKid),
			acc.LockKind, hash, salt, acc.CreatedAt.Unix())
		if err != nil {
			if strings.Contains(err.Error(), "account_one_owner") {
				return errors.New("es gibt schon einen besitzer")
			}
			return err
		}
		if _, err = t.ExecContext(ctx,
			`INSERT INTO account_limit (account_id, max_age, allow_unrated, max_streams,
			   allow_transcode, allow_download, max_downloads, remote_quality)
			 VALUES (?,?,?,?,?,?,?,?)`,
			acc.ID, lim.MaxAge, b2i(lim.AllowUnrated), lim.MaxStreams,
			b2i(lim.AllowTranscode), b2i(lim.AllowDownload), lim.MaxDownloads,
			lim.RemoteQuality); err != nil {
			return err
		}
		for _, lib := range in.Grants {
			if _, err = t.ExecContext(ctx,
				`INSERT OR IGNORE INTO grant_library (account_id, library_id) VALUES (?,?)`,
				acc.ID, lib); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return acc, nil
}

// ---------------------------------------------------------------- lesen

const accCols = `a.id, a.name, a.avatar, a.is_owner, a.is_kid, a.lock_kind, a.disabled, a.created_at, a.expires_at,
	COALESCE(a.group_id, ''), COALESCE(g.name, ''), COALESCE(g.verwaltung, 0), COALESCE(a.admin_lock_kind, 'none')`

// accVon ist die FROM-Klausel zu accCols: das Konto mit seiner Gruppe.
const accVon = ` FROM account a LEFT JOIN gruppe g ON g.id = a.group_id `

func scanAccount(sc interface{ Scan(...any) error }) (*Account, error) {
	var a Account
	var owner, kid, disabled, verw int
	var created int64
	var expires sql.NullInt64
	if err := sc.Scan(&a.ID, &a.Name, &a.Avatar, &owner, &kid, &a.LockKind,
		&disabled, &created, &expires, &a.GroupID, &a.GroupName, &verw, &a.AdminLock); err != nil {
		return nil, err
	}
	a.IsOwner, a.IsKid, a.Disabled = owner == 1, kid == 1, disabled == 1
	a.IsAdmin = a.IsOwner || verw >= 1
	a.Vollzugriff = !a.IsOwner && verw == 2
	a.CreatedAt = time.Unix(created, 0)
	if expires.Valid {
		t := time.Unix(expires.Int64, 0)
		a.ExpiresAt = &t
	}
	return &a, nil
}

// Accounts liefert die Profilauswahl. Das ist die einzige Liste, die jeder
// sehen darf — Name und Bild, sonst nichts.
func (s *Store) Accounts(ctx context.Context) ([]Account, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+accCols+accVon+` WHERE a.disabled = 0`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Account{}
	for rows.Next() {
		a, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Der Besitzer zuerst, dann nach Namen (sortieren.go).
	nachNamenSortieren(out, func(a *Account) string { return a.Name }, func(a *Account) string { return a.ID },
		func(a *Account) bool { return a.IsOwner })
	return out, nil
}

func (s *Store) Account(ctx context.Context, id string) (*Account, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+accCols+accVon+` WHERE a.id = ?`, id)
	a, err := scanAccount(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return a, err
}

func (s *Store) HasOwner(ctx context.Context) bool {
	var n int
	_ = s.db.QueryRowContext(ctx, `SELECT count(*) FROM account WHERE is_owner = 1`).Scan(&n)
	return n > 0
}

func (s *Store) Limits(ctx context.Context, id string) (Limits, error) {
	// Wer den Server verwaltet, bekommt keine Grenzen: Er könnte sie sich
	// ohnehin selbst aufheben, und „höchstens 2 gleichzeitig" für den
	// Besitzer wirkte nur wie ein Fehler. Kinder können nie verwalten
	// (schutz.go), darum öffnet das keinem Kind etwas.
	var besitzer, verwaltung int
	if err := s.db.QueryRowContext(ctx,
		`SELECT a.is_owner, COALESCE(g.verwaltung, 0) FROM account a
		   LEFT JOIN gruppe g ON g.id = a.group_id WHERE a.id = ?`, id).
		Scan(&besitzer, &verwaltung); err == nil && (besitzer == 1 || verwaltung > 0) {
		return VerwaltungsLimits(), nil
	}
	// Steckt das Konto in einer Gruppe, gelten deren Grenzen.
	var lim sql.NullString
	var kind int
	if err := s.db.QueryRowContext(ctx,
		`SELECT g.limits, a.is_kid FROM account a JOIN gruppe g ON g.id = a.group_id WHERE a.id = ?`, id).
		Scan(&lim, &kind); err == nil && lim.Valid {
		gruppe := limitsAus(lim.String)
		if kind == 1 {
			// Ein Kinderprofil behält seine eigenen Grenzen auch in einer
			// Gruppe: Es gilt jeweils die strengere Hälfte. Eine Gruppe
			// „Familie" ohne Altersgrenze soll ein Kind nicht öffnen.
			eigene, err := s.eigeneLimits(ctx, id)
			if err != nil {
				return Limits{}, err
			}
			return Strengste(eigene, gruppe), nil
		}
		return gruppe, nil
	}
	return s.eigeneLimits(ctx, id)
}

// EigeneLimits sind die am Konto selbst — die ruhen, solange es in einer
// Gruppe steckt, und die Verwaltung zeigt sie trotzdem.
func (s *Store) EigeneLimits(ctx context.Context, id string) (Limits, error) {
	return s.eigeneLimits(ctx, id)
}

func (s *Store) eigeneLimits(ctx context.Context, id string) (Limits, error) {
	var l Limits
	var unrated, trans, down int
	err := s.db.QueryRowContext(ctx,
		`SELECT max_age, allow_unrated, max_streams, allow_transcode,
		        allow_download, max_downloads, remote_quality
		   FROM account_limit WHERE account_id = ?`, id).
		Scan(&l.MaxAge, &unrated, &l.MaxStreams, &trans, &down, &l.MaxDownloads, &l.RemoteQuality)
	if errors.Is(err, sql.ErrNoRows) {
		return DefaultLimits(false), nil
	}
	l.AllowUnrated, l.AllowTranscode, l.AllowDownload = unrated == 1, trans == 1, down == 1
	return l, err
}

func (s *Store) SetLimits(ctx context.Context, id string, l Limits) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO account_limit (account_id, max_age, allow_unrated, max_streams,
		   allow_transcode, allow_download, max_downloads, remote_quality)
		 VALUES (?,?,?,?,?,?,?,?)
		 ON CONFLICT (account_id) DO UPDATE SET
		   max_age=excluded.max_age, allow_unrated=excluded.allow_unrated,
		   max_streams=excluded.max_streams, allow_transcode=excluded.allow_transcode,
		   allow_download=excluded.allow_download, max_downloads=excluded.max_downloads,
		   remote_quality=excluded.remote_quality`,
		id, l.MaxAge, b2i(l.AllowUnrated), l.MaxStreams, b2i(l.AllowTranscode),
		b2i(l.AllowDownload), l.MaxDownloads, l.RemoteQuality)
	return err
}

// GrantedLibraries nennt die Bibliotheken, die dieses Konto sehen darf.
// Der Besitzer sieht immer alle.
func (s *Store) GrantedLibraries(ctx context.Context, id string) ([]string, error) {
	acc, err := s.Account(ctx, id)
	if err != nil {
		return nil, err
	}
	q := `SELECT library_id FROM grant_library WHERE account_id = ?`
	args := []any{id}
	if acc.Besitzerrechte() {
		q, args = `SELECT id FROM library`, nil
	} else if acc.GroupID != "" {
		q, args = `SELECT library_id FROM gruppe_bibliothek WHERE group_id = ?`, []any{acc.GroupID}
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
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

func (s *Store) SetGrants(ctx context.Context, id string, libs []string) error {
	return s.tx(ctx, func(t *sql.Tx) error {
		if _, err := t.ExecContext(ctx,
			`DELETE FROM grant_library WHERE account_id = ?`, id); err != nil {
			return err
		}
		for _, l := range libs {
			if _, err := t.ExecContext(ctx,
				`INSERT OR IGNORE INTO grant_library (account_id, library_id) VALUES (?,?)`,
				id, l); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Store) RenameAccount(ctx context.Context, id, name, avatar string) error {
	name = strings.TrimSpace(name)
	if err := PruefeProfil(name, avatar, ""); err != nil {
		return err
	}
	if vergeben, err := nameVergeben(ctx, s.db, name, id); err != nil {
		return err
	} else if vergeben {
		return ErrNameVergeben
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE account SET name = ?, avatar = ? WHERE id = ?`, name, avatar, id)
	return err
}

func (s *Store) SetAccountDisabled(ctx context.Context, id string, off bool) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE account SET disabled = ? WHERE id = ? AND is_owner = 0`, b2i(off), id)
	return err
}

func (s *Store) DeleteAccount(ctx context.Context, id string) error {
	// Was die Person in die gemeinsamen Listen anderer gelegt hat, bleibt
	// dort — es gehört jetzt der Person, der die Liste gehört. Sonst nähme
	// das Löschen eines Kontos den anderen still Titel aus ihren Listen.
	if _, err := s.db.ExecContext(ctx,
		`UPDATE playlist_item SET added_by =
		   (SELECT p.account_id FROM playlist p WHERE p.id = playlist_item.playlist_id)
		 WHERE added_by = ? AND playlist_id IN (SELECT id FROM playlist WHERE account_id != ?)`,
		id, id); err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM account WHERE id = ? AND is_owner = 0`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errors.New("konto nicht gefunden oder es ist der besitzer")
	}
	return nil
}

// ---------------------------------------------------------------- Entsperren

func hashSecret(secret string) (salt, hash []byte, err error) {
	salt = make([]byte, 16)
	if _, err = rand.Read(salt); err != nil {
		return nil, nil, err
	}
	hash, err = pbkdf2.Key(sha256.New, secret, salt, pbkdf2Rounds, 32)
	return salt, hash, err
}

// Blindrechnen kostet so viel Zeit wie eine echte Prüfung, prüft aber
// nichts. Für Anfragen ohne Profil oder ohne Sperre: Sonst verriete die
// kürzere Antwortzeit, welche Profilnamen es gibt und welche einen PIN haben.
func Blindrechnen(secret string) {
	_, _ = pbkdf2.Key(sha256.New, secret, []byte("lumen-blind-salz"), pbkdf2Rounds, 32)
}

// CheckSecret prüft PIN oder Passwort in gleichbleibender Zeit.
func (s *Store) CheckSecret(ctx context.Context, id, secret string) error {
	var kind string
	var salt, want []byte
	err := s.db.QueryRowContext(ctx,
		`SELECT lock_kind, secret_salt, secret_hash FROM account WHERE id = ? AND disabled = 0`,
		id).Scan(&kind, &salt, &want)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if kind == "none" {
		return nil
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

func (s *Store) SetSecret(ctx context.Context, id, kind, secret string) error {
	sp, err := s.sperreVorbereiten(ctx, id, kind, secret)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`UPDATE account SET lock_kind=?, secret_hash=?, secret_salt=? WHERE id=?`,
		sp.art, sp.hash, sp.salt, id)
	return err
}

// neueSperre ist ein geprüfter, schon gehashter Riegel, der nur noch
// geschrieben werden muss. Getrennt vom Schreiben, damit das Zurücksetzen
// durch die Verwaltung (vergessen.go) erst prüfen und dann in einer
// einzigen Transaktion schreiben und löschen kann — ein zu kurzer PIN
// soll nicht erst auffallen, nachdem der Verlauf schon weg ist.
type neueSperre struct {
	art        string
	hash, salt []byte // nil bei „none"
}

func (s *Store) sperreVorbereiten(ctx context.Context, id, kind, secret string) (neueSperre, error) {
	if !sperrArten[kind] {
		return neueSperre{}, errors.New("unbekannte sperre")
	}
	if kind == "pin" && len(secret) < 4 {
		return neueSperre{}, errors.New("pin braucht mindestens vier ziffern")
	}
	if kind == "password" && len(secret) < 8 {
		return neueSperre{}, errors.New("passwort braucht mindestens acht zeichen")
	}
	if kind == "pin" && strings.Trim(secret, "0123456789") != "" {
		return neueSperre{}, errors.New("eine pin besteht nur aus ziffern")
	}
	if kind == "none" {
		// Wer in die Verwaltung darf, braucht einen Riegel vor dem Profil —
		// sonst öffnet zu Hause jeder mit einem Klick die Verwaltung.
		acc, err := s.Account(ctx, id)
		if err != nil {
			return neueSperre{}, err
		}
		if acc.IsAdmin {
			return neueSperre{}, ErrSperreNoetig
		}
		return neueSperre{art: "none"}, nil
	}
	salt, hash, err := hashSecret(secret)
	if err != nil {
		return neueSperre{}, err
	}
	return neueSperre{art: kind, hash: hash, salt: salt}, nil
}

// ---------------------------------------------------------------- Token

type Device struct {
	TokenHash []byte    `json:"-"`
	AccountID string    `json:"accountId"`
	Name      string    `json:"name"`
	Kind      string    `json:"kind"`
	CreatedAt time.Time `json:"createdAt"`
	LastSeen  time.Time `json:"lastSeen"`
}

// IssueToken gibt ein frisches Token heraus. Gespeichert wird nur sein
// SHA-256 — wer die Datenbank liest, kann sich damit nicht anmelden.
func (s *Store) IssueToken(ctx context.Context, accountID, deviceName, deviceKind string) (string, error) {
	tok := rand.Text() + rand.Text() // 2 × 26 Zeichen aus crypto/rand
	sum := sha256.Sum256([]byte(tok))
	now := heute()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO token (hash, account_id, device_name, device_kind, created_at, last_seen_at)
		 VALUES (?,?,?,?,?,?)`,
		sum[:], accountID, deviceName, deviceKind, now, now)
	if err != nil {
		return "", err
	}
	return tok, nil
}

// LookupToken gibt das Konto zum Token zurück und merkt sich den Zeitpunkt
// des letzten Zugriffs — das ist Gerätepflege, kein Wiedergabe-Verlauf.
func (s *Store) LookupToken(ctx context.Context, token string) (*Account, *Device, error) {
	sum := sha256.Sum256([]byte(token))
	return s.LookupTokenHash(ctx, sum)
}

// LookupTokenHash ist LookupToken für den, der nur den Prüfwert kennt —
// der Medienschlüssel merkt sich den, nicht das Token selbst.
func (s *Store) LookupTokenHash(ctx context.Context, sum [32]byte) (*Account, *Device, error) {
	var d Device
	var created, seen int64
	err := s.db.QueryRowContext(ctx,
		`SELECT account_id, device_name, device_kind, created_at, last_seen_at
		   FROM token WHERE hash = ?`, sum[:]).
		Scan(&d.AccountID, &d.Name, &d.Kind, &created, &seen)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, ErrNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	d.TokenHash = sum[:]
	d.CreatedAt, d.LastSeen = time.Unix(created, 0), time.Unix(seen, 0)

	acc, err := s.Account(ctx, d.AccountID)
	if err != nil {
		return nil, nil, err
	}
	if acc.Disabled {
		return nil, nil, errors.New("konto gesperrt")
	}
	if acc.ExpiresAt != nil && acc.ExpiresAt.Before(time.Now()) {
		return nil, nil, errors.New("zugang abgelaufen")
	}
	// Nur der Tag, und nur einmal am Tag geschrieben: „Leas iPad zuletzt
	// 01:47" soll nirgends stehen.
	if tag := heute(); seen != tag {
		_, _ = s.db.ExecContext(ctx,
			`UPDATE token SET last_seen_at = ? WHERE hash = ?`, tag, sum[:])
	}
	return acc, &d, nil
}

func (s *Store) Devices(ctx context.Context, accountID string) ([]Device, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT hash, device_name, device_kind, created_at, last_seen_at
		   FROM token WHERE account_id = ? ORDER BY last_seen_at DESC`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Device{}
	for rows.Next() {
		var d Device
		var c, l int64
		if err := rows.Scan(&d.TokenHash, &d.Name, &d.Kind, &c, &l); err != nil {
			return nil, err
		}
		d.AccountID = accountID
		d.CreatedAt, d.LastSeen = time.Unix(c, 0), time.Unix(l, 0)
		out = append(out, d)
	}
	return out, rows.Err()
}

// RevokeToken meldet genau dieses eine Gerät ab.
func (s *Store) RevokeToken(ctx context.Context, tokenHash []byte) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM token WHERE hash = ?`, tokenHash)
	return err
}

// GeraeteKennung ist der Name, unter dem die Oberfläche ein Gerät
// anspricht — etwa „dieses abmelden". Abgeleitet vom Prüfwert des Tokens,
// aber nicht der Prüfwert selbst: mit dem lässt sich ein Medienschlüssel
// nachprüfen, und er soll den Server nicht verlassen.
func GeraeteKennung(tokenHash []byte) string {
	sum := sha256.Sum256(append([]byte("lumen-geraet:"), tokenHash...))
	return hex.EncodeToString(sum[:12])
}

// GeraetAbmelden meldet ein Gerät des eigenen Kontos über seine Kennung
// ab. Nur Tokens dieses Kontos kommen in Frage — wer eine fremde Kennung
// rät, trifft nichts. Gibt ErrNotFound, wenn es die Kennung nicht gibt.
func (s *Store) GeraetAbmelden(ctx context.Context, accountID, kennung string) error {
	geraete, err := s.Devices(ctx, accountID)
	if err != nil {
		return err
	}
	for _, d := range geraete {
		if GeraeteKennung(d.TokenHash) == kennung {
			return s.RevokeToken(ctx, d.TokenHash)
		}
	}
	return ErrNotFound
}

func (s *Store) RevokeAllTokens(ctx context.Context, accountID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM token WHERE account_id = ?`, accountID)
	return err
}

// ---------------------------------------------------------------- Einladungen

type Invite struct {
	Code      string         `json:"code"`
	ExpiresAt time.Time      `json:"expiresAt"`
	Used      bool           `json:"used"`
	Preset    map[string]any `json:"preset"`
}

func (s *Store) CreateInvite(ctx context.Context, ownerID string, ttl time.Duration, preset map[string]any) (*Invite, error) {
	raw, err := json.Marshal(preset)
	if err != nil {
		return nil, err
	}
	// 16 Zeichen (80 Bit) statt 26: am Fernseher mit der Fernbedienung
	// einzutippen, und gegen Raten sicher genug für eine Einladung, die
	// nach Tagen abläuft und nur einmal gilt (das Einlösen ist gebremst).
	inv := &Invite{Code: rand.Text()[:16], ExpiresAt: time.Now().Add(ttl), Preset: preset}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO invite (code, created_by, expires_at, preset) VALUES (?,?,?,?)`,
		inv.Code, ownerID, inv.ExpiresAt.Unix(), string(raw))
	return inv, err
}

// EinladungsCode bringt einen getippten Code in die gespeicherte Form:
// Grossbuchstaben, ohne Leerzeichen und Bindestriche, und mit den
// Verwechslern 0→O, 1→I, 8→B (das Alphabet kennt nur A–Z und 2–7).
func EinladungsCode(code string) string {
	code = strings.TrimSpace(code)
	// Ein alter Code mit Kleinbuchstaben? rand.Text liefert nur Grossbuchstaben.
	var b strings.Builder
	for _, r := range strings.ToUpper(code) {
		switch r {
		case ' ', '-', '\u00a0', '\u2009':
			continue
		case '0':
			r = 'O'
		case '1':
			r = 'I'
		case '8':
			r = 'B'
		}
		b.WriteRune(r)
	}
	return b.String()
}

// OffeneEinladung ist eine Einladung, die noch niemand eingelöst hat und
// die noch gilt. Der Code selbst geht nicht mit hinaus — nur eine Kennung
// zum Zurückziehen und seine letzten vier Zeichen zum Wiedererkennen.
type OffeneEinladung struct {
	Kennung      string    `json:"kennung"`
	Ende         string    `json:"ende"`
	ExpiresAt    time.Time `json:"expiresAt"`
	Kinderprofil bool      `json:"kinderprofil"`
	Bibliotheken int       `json:"bibliotheken"`
	Von          string    `json:"von"`
}

func einladungsKennung(code string) string {
	h := sha256.Sum256([]byte("einladung:" + code))
	return hex.EncodeToString(h[:8])
}

// OffeneEinladungen listet die offenen Einladungen — alle oder nur die,
// die accountID erzeugt hat.
func (s *Store) OffeneEinladungen(ctx context.Context, accountID string, alle bool) ([]OffeneEinladung, error) {
	q := `SELECT i.code, i.expires_at, i.preset, COALESCE(a.name, '') FROM invite i
	      LEFT JOIN account a ON a.id = i.created_by
	      WHERE i.used_at IS NULL AND i.expires_at >= ?`
	args := []any{time.Now().Unix()}
	if !alle {
		q += ` AND i.created_by = ?`
		args = append(args, accountID)
	}
	rows, err := s.db.QueryContext(ctx, q+` ORDER BY i.expires_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []OffeneEinladung{}
	for rows.Next() {
		var code, presetRaw, von string
		var ablauf int64
		if err := rows.Scan(&code, &ablauf, &presetRaw, &von); err != nil {
			return nil, err
		}
		var preset struct {
			Libraries    []string `json:"libraries"`
			Kinderprofil bool     `json:"kinderprofil"`
		}
		_ = json.Unmarshal([]byte(presetRaw), &preset)
		ende := code
		if len(ende) > 4 {
			ende = ende[len(ende)-4:]
		}
		out = append(out, OffeneEinladung{Kennung: einladungsKennung(code), Ende: ende,
			ExpiresAt: time.Unix(ablauf, 0), Kinderprofil: preset.Kinderprofil,
			Bibliotheken: len(preset.Libraries), Von: von})
	}
	return out, rows.Err()
}

// EinladungZurueckziehen löscht eine offene Einladung. Ohne alle nur eine,
// die accountID selbst erzeugt hat.
func (s *Store) EinladungZurueckziehen(ctx context.Context, kennung, accountID string, alle bool) error {
	offen, err := s.OffeneEinladungen(ctx, accountID, alle)
	if err != nil {
		return err
	}
	for _, e := range offen {
		if e.Kennung != kennung {
			continue
		}
		rows, err := s.db.QueryContext(ctx, `SELECT code FROM invite WHERE used_at IS NULL`)
		if err != nil {
			return err
		}
		var code string
		for rows.Next() {
			var c string
			if rows.Scan(&c) == nil && einladungsKennung(c) == kennung {
				code = c
			}
		}
		rows.Close()
		if code == "" {
			return ErrNotFound
		}
		_, err = s.db.ExecContext(ctx, `DELETE FROM invite WHERE code = ? AND used_at IS NULL`, code)
		return err
	}
	return ErrNotFound
}

// RedeemInvite löst eine Einladung ein. Jede Einladung gilt genau einmal.
func (s *Store) RedeemInvite(ctx context.Context, code, name, lockKind, secret string) (*Account, error) {
	code = EinladungsCode(code)
	// Erst prüfen, ob es sie gibt (für eine verständliche Meldung) …
	var presetRaw string
	var expires int64
	var used sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		`SELECT preset, expires_at, used_at FROM invite WHERE code = ?`, code).
		Scan(&presetRaw, &expires, &used)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("einladung unbekannt")
	}
	if err != nil {
		return nil, err
	}
	if used.Valid {
		return nil, errors.New("einladung wurde schon benutzt")
	}
	if time.Now().Unix() > expires {
		return nil, errors.New("einladung ist abgelaufen")
	}
	if err := PruefeProfil(name, "", lockKind); err != nil {
		return nil, err
	}

	// … dann in einem Schritt beanspruchen. Kommen zwei Anfragen
	// gleichzeitig, gewinnt genau eine; die andere sieht „schon benutzt".
	res, err := s.db.ExecContext(ctx,
		`UPDATE invite SET used_at = ? WHERE code = ? AND used_at IS NULL AND expires_at >= ?`,
		time.Now().Unix(), code, time.Now().Unix())
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return nil, errors.New("einladung wurde schon benutzt")
	}

	var preset struct {
		Libraries    []string `json:"libraries"`
		Limits       Limits   `json:"limits"`
		Kinderprofil bool     `json:"kinderprofil"`
	}
	_ = json.Unmarshal([]byte(presetRaw), &preset)
	if preset.Limits.MaxStreams == 0 {
		preset.Limits = DefaultLimits(preset.Kinderprofil)
	}

	acc, err := s.CreateAccount(ctx, NewAccount{
		Name: name, LockKind: lockKind, Secret: secret, IsKid: preset.Kinderprofil,
		Grants: preset.Libraries, Limits: preset.Limits,
	})
	if err != nil {
		// Profil liess sich nicht anlegen (etwa PIN zu kurz): Die
		// Einladung bleibt gültig, damit man es noch einmal versuchen kann.
		_, _ = s.db.ExecContext(ctx, `UPDATE invite SET used_at = NULL WHERE code = ?`, code)
		return nil, err
	}
	return acc, nil
}

// ---------------------------------------------------------------- Hilfen

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func newID(prefix string) string {
	return fmt.Sprintf("%s_%s", prefix, strings.ToLower(rand.Text()[:16]))
}

// AlteGeraeteAbmelden löscht Anmeldungen, die länger als dauer nicht
// benutzt wurden.
func (s *Store) AlteGeraeteAbmelden(ctx context.Context, dauer time.Duration) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM token WHERE last_seen_at < ?`,
		time.Now().Add(-dauer).Unix())
	return err
}

// ProfilFarben sind die Farben, die ein neues Profil ohne eigenes Bild
// bekommt, in dieser Reihenfolge.
var ProfilFarben = []string{"av-a", "av-b", "av-c", "av-d", "av-k"}

// seltensteFarbe wählt aus ProfilFarben die, die bisher am wenigsten
// vergeben ist (bei Gleichstand die vordere).
func seltensteFarbe(ctx context.Context, t *sql.Tx) (string, error) {
	zahl := map[string]int{}
	rows, err := t.QueryContext(ctx, `SELECT avatar, COUNT(*) FROM account GROUP BY avatar`)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	for rows.Next() {
		var a string
		var n int
		if err := rows.Scan(&a, &n); err != nil {
			return "", err
		}
		zahl[a] = n
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	best := ProfilFarben[0]
	for _, f := range ProfilFarben[1:] {
		if zahl[f] < zahl[best] {
			best = f
		}
	}
	return best, nil
}
