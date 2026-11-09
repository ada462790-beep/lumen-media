// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

// Schutz der Profile: Altersgrenzen, Kinderprofile, eindeutige Namen und
// die Regel, dass Verwaltung nur bekommt, wer sein Profil abschliesst.
//
// Hier liegt alles, was dafür sorgt, dass Rechte nie aus Versehen lockerer
// werden — beim Einladen, beim Umsortieren in Gruppen und beim Auflösen
// einer Gruppe.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// OhneAltersgrenze steht in Limits.MaxAge für „alles, auch ab 18".
//
// Früher hiess 0 „ohne Grenze" — dann liess sich „nur FSK 0" gar nicht
// einstellen, und wer im Menü „0" wählte, öffnete ungewollt alles. Eine
// negative Zahl kann keine echte Freigabe sein und ist darum eindeutig.
const OhneAltersgrenze = -1

// Kinderstufen sind die Altersgrenzen, die ein Kinderprofil haben darf.
var Kinderstufen = []int{0, 6, 12, 16}

// erlaubteAltersgrenzen: was in Limits.MaxAge stehen darf.
var erlaubteAltersgrenzen = map[int]bool{OhneAltersgrenze: true, 0: true, 6: true, 12: true, 16: true, 18: true}

// qualitaetsStufe ordnet die Qualität für unterwegs; je kleiner, desto
// strenger. Unbekannt oder leer zählt wie 1080p, die Vorgabe.
var qualitaetsStufe = map[string]int{"480p": 1, "720p": 2, "1080p": 3, "original": 4}

var (
	// ErrNameVergeben: ein anderes Profil heisst schon so (ohne Rücksicht
	// auf Gross- und Kleinschreibung). Sonst könnte sich jemand „alex"
	// nennen, und die Anmeldung von unterwegs über den Namen fände kein
	// eindeutiges Profil mehr — der Besitzer wäre ausgesperrt.
	ErrNameVergeben = errors.New("diesen namen gibt es schon — bitte einen anderen wählen")
	// ErrSperreNoetig: ein Konto mit Verwaltungsrecht braucht PIN oder
	// Passwort. Ein offenes Profil gäbe zu Hause jedem die Verwaltung.
	ErrSperreNoetig = errors.New("ein profil mit verwaltung braucht eine pin oder ein passwort")
	// ErrKindOhneVerwaltung: Kinderprofile bekommen keine Verwaltung.
	ErrKindOhneVerwaltung = errors.New("ein kinderprofil bekommt keine verwaltung")
)

// PruefeGrenzen weist Werte zurück, die es nicht geben kann. Sonst
// hiesse etwa maxStreams 0 „gar nicht schauen", wüsste aber niemand, und
// eine Altersgrenze 7 wäre weder FSK noch „ohne".
func PruefeGrenzen(l Limits) error {
	if l.MaxStreams < 1 || l.MaxStreams > 20 {
		return errors.New("gleichzeitig: zwischen 1 und 20")
	}
	if !erlaubteAltersgrenzen[l.MaxAge] {
		return errors.New("unbekannte altersgrenze")
	}
	if l.MaxDownloads < 0 || l.MaxDownloads > 500 {
		return errors.New("downloads: zwischen 0 und 500")
	}
	if l.RemoteQuality != "" && qualitaetsStufe[l.RemoteQuality] == 0 {
		return errors.New("unbekannte qualität für unterwegs")
	}
	return nil
}

// KinderGrenzen sind die Grenzen eines neuen Kinderprofils mit dieser
// Altersstufe: keine ungeprüften Titel, kein Herunterladen.
func KinderGrenzen(alter int) (Limits, error) {
	if !istKinderstufe(alter) {
		return Limits{}, errors.New("ein kinderprofil braucht eine altersgrenze von 0, 6, 12 oder 16")
	}
	l := DefaultLimits(true)
	l.MaxAge = alter
	return l, nil
}

func istKinderstufe(alter int) bool {
	for _, k := range Kinderstufen {
		if k == alter {
			return true
		}
	}
	return false
}

// PruefeKinderGrenzen: Ein Kinderprofil bleibt bei einer Kinderstufe und
// ohne ungeprüfte Titel — auch wenn jemand später daran dreht.
func PruefeKinderGrenzen(l Limits) error {
	if !istKinderstufe(l.MaxAge) {
		return errors.New("ein kinderprofil braucht eine altersgrenze von 0, 6, 12 oder 16")
	}
	if l.AllowUnrated {
		return errors.New("ein kinderprofil sieht keine ungeprüften titel")
	}
	return nil
}

// alterStrenge macht Altersgrenzen vergleichbar: „ohne" ist die lockerste.
func alterStrenge(a int) int {
	if a == OhneAltersgrenze {
		return 1 << 20
	}
	return a
}

// Strengste nimmt von zwei Grenzen jeweils die strengere Hälfte. Damit
// werden Rechte beim Umsortieren nie lockerer, als sie vorher waren.
func Strengste(a, b Limits) Limits {
	out := a
	if alterStrenge(b.MaxAge) < alterStrenge(a.MaxAge) {
		out.MaxAge = b.MaxAge
	}
	out.AllowUnrated = a.AllowUnrated && b.AllowUnrated
	out.MaxStreams = min(a.MaxStreams, b.MaxStreams)
	if out.MaxStreams < 1 {
		out.MaxStreams = 1
	}
	out.AllowTranscode = a.AllowTranscode && b.AllowTranscode
	out.AllowDownload = a.AllowDownload && b.AllowDownload
	out.MaxDownloads = min(a.MaxDownloads, b.MaxDownloads)
	out.RemoteQuality = a.RemoteQuality
	if qualitaetStufe(b.RemoteQuality) < qualitaetStufe(a.RemoteQuality) {
		out.RemoteQuality = b.RemoteQuality
	}
	if out.RemoteQuality == "" {
		out.RemoteQuality = "1080p"
	}
	return out
}

func qualitaetStufe(q string) int {
	if n := qualitaetsStufe[q]; n > 0 {
		return n
	}
	return qualitaetsStufe["1080p"]
}

// nameVergeben prüft, ob schon ein anderes Profil so heisst. Verglichen
// wird in Go mit EqualFold — SQLite kennt Gross/Klein nur für ASCII, und
// „Ändi" gegen „ändi" soll genauso zählen.
func nameVergeben(ctx context.Context, q interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, name, ausserID string) (bool, error) {
	rows, err := q.QueryContext(ctx, `SELECT id, name FROM account`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	name = strings.TrimSpace(name)
	for rows.Next() {
		var id, n string
		if err := rows.Scan(&id, &n); err != nil {
			return false, err
		}
		if id != ausserID && strings.EqualFold(strings.TrimSpace(n), name) {
			return true, nil
		}
	}
	return false, rows.Err()
}

// NameFrei sagt, ob ein Name noch zu haben ist (für die Oberfläche).
func (s *Store) NameFrei(ctx context.Context, name, ausserID string) bool {
	vergeben, err := nameVergeben(ctx, s.db, name, ausserID)
	return err == nil && !vergeben
}

// SetKinderprofil macht ein Konto zum Kinderprofil oder nimmt es zurück.
// Wird es eines, gelten sofort Kinder-Grenzen (mindestens so streng wie
// bisher), und ein Verwaltungsrecht fällt weg.
func (s *Store) SetKinderprofil(ctx context.Context, id string, kind bool) error {
	acc, err := s.Account(ctx, id)
	if err != nil {
		return err
	}
	if acc.IsOwner {
		return errors.New("der besitzer ist kein kinderprofil")
	}
	if kind && acc.IsAdmin {
		return ErrKindOhneVerwaltung
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE account SET is_kid = ? WHERE id = ?`, b2i(kind), id); err != nil {
		return err
	}
	if kind && !acc.IsKid {
		eigene, err := s.eigeneLimits(ctx, id)
		if err != nil {
			return err
		}
		kinder, _ := KinderGrenzen(12)
		neu := Strengste(eigene, kinder)
		if !istKinderstufe(neu.MaxAge) {
			neu.MaxAge = 12
		}
		return s.SetLimits(ctx, id, neu)
	}
	return nil
}

// gruppeVerlassen schreibt einem Konto, das eine Gruppe verlässt, die
// strengere Hälfte aus seinen ruhenden eigenen Grenzen und denen der
// Gruppe ins Konto — und als Bibliotheken nur, was es in beiden sehen
// durfte. Früher galten danach einfach wieder die ruhenden Einträge, und
// die waren oft „alles": Wer ein Kind aus der Gruppe „Kinder" nahm oder
// die Gruppe auflöste, öffnete ihm die ganze Mediathek.
func gruppeVerlassen(ctx context.Context, t *sql.Tx, accountID, groupID string) error {
	var limRoh string
	if err := t.QueryRowContext(ctx, `SELECT limits FROM gruppe WHERE id = ?`, groupID).Scan(&limRoh); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}
	gruppe := limitsAus(limRoh)

	eigene := DefaultLimits(false)
	var unrated, trans, down int
	err := t.QueryRowContext(ctx,
		`SELECT max_age, allow_unrated, max_streams, allow_transcode,
		        allow_download, max_downloads, remote_quality
		   FROM account_limit WHERE account_id = ?`, accountID).
		Scan(&eigene.MaxAge, &unrated, &eigene.MaxStreams, &trans, &down, &eigene.MaxDownloads, &eigene.RemoteQuality)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		eigene = gruppe
	case err != nil:
		return err
	default:
		eigene.AllowUnrated, eigene.AllowTranscode, eigene.AllowDownload = unrated == 1, trans == 1, down == 1
	}
	neu := Strengste(eigene, gruppe)
	if _, err := t.ExecContext(ctx,
		`INSERT INTO account_limit (account_id, max_age, allow_unrated, max_streams,
		   allow_transcode, allow_download, max_downloads, remote_quality)
		 VALUES (?,?,?,?,?,?,?,?)
		 ON CONFLICT (account_id) DO UPDATE SET
		   max_age=excluded.max_age, allow_unrated=excluded.allow_unrated,
		   max_streams=excluded.max_streams, allow_transcode=excluded.allow_transcode,
		   allow_download=excluded.allow_download, max_downloads=excluded.max_downloads,
		   remote_quality=excluded.remote_quality`,
		accountID, neu.MaxAge, b2i(neu.AllowUnrated), neu.MaxStreams, b2i(neu.AllowTranscode),
		b2i(neu.AllowDownload), neu.MaxDownloads, neu.RemoteQuality); err != nil {
		return err
	}
	// Bibliotheken: nur, was in der Gruppe und am Konto freigegeben war.
	_, err = t.ExecContext(ctx,
		`DELETE FROM grant_library WHERE account_id = ?
		   AND library_id NOT IN (SELECT library_id FROM gruppe_bibliothek WHERE group_id = ?)`,
		accountID, groupID)
	return err
}

// verwaltungErlaubt prüft, ob ein Konto in eine Gruppe mit Verwaltung
// darf: nicht als Kinderprofil und nur mit PIN oder Passwort.
func verwaltungErlaubt(a *Account) error {
	if a.IsKid {
		return ErrKindOhneVerwaltung
	}
	if a.LockKind == "" || a.LockKind == "none" {
		return ErrSperreNoetig
	}
	return nil
}

// altersgrenzeUmstellen bringt bestehende Daten auf die neue Darstellung:
// was bisher 0 („ohne Grenze") war, wird OhneAltersgrenze — an Konten, in
// Gruppen und in noch offenen Einladungen. Sonst sähe nach dem Update
// jemand mit „alle" plötzlich nur noch FSK 0.
func altersgrenzeUmstellen(db *sql.DB) error {
	if _, err := db.Exec(`UPDATE account_limit SET max_age = ? WHERE max_age = 0`, OhneAltersgrenze); err != nil {
		return fmt.Errorf("altersgrenze an konten: %w", err)
	}
	for _, t := range []struct{ tabelle, schluessel, spalte string }{
		{"gruppe", "id", "limits"},
		{"invite", "code", "preset"},
	} {
		rows, err := db.Query(`SELECT ` + t.schluessel + `, ` + t.spalte + ` FROM ` + t.tabelle)
		if err != nil {
			return fmt.Errorf("altersgrenze in %s: %w", t.tabelle, err)
		}
		neu := map[string]string{}
		for rows.Next() {
			var id, roh string
			if err := rows.Scan(&id, &roh); err != nil {
				rows.Close()
				return err
			}
			if umgestellt, geaendert := altersgrenzeImJSON(roh, t.tabelle == "invite"); geaendert {
				neu[id] = umgestellt
			}
		}
		rows.Close()
		for id, roh := range neu {
			if _, err := db.Exec(`UPDATE `+t.tabelle+` SET `+t.spalte+` = ? WHERE `+t.schluessel+` = ?`, roh, id); err != nil {
				return err
			}
		}
	}
	return nil
}

// kinderErkennen kennzeichnet bestehende Konten als Kinderprofil, wenn
// ihre wirksame Altersgrenze 0, 6, 12 oder 16 ist und sie nicht verwalten.
// Ältere Fassungen kannten kein Kinderprofil; wer dort eine Grenze unter 18
// bekam, war praktisch immer ein Kind. Läuft nach altersgrenzeUmstellen —
// 0 heisst dann wirklich „nur ab 0".
func kinderErkennen(db *sql.DB) error {
	rows, err := db.Query(`SELECT a.id, COALESCE(g.verwaltung, 0), COALESCE(g.limits, ''),
	                              COALESCE(l.max_age, 18), a.group_id IS NOT NULL AND a.group_id != ''
	                         FROM account a
	                         LEFT JOIN gruppe g ON g.id = a.group_id
	                         LEFT JOIN account_limit l ON l.account_id = a.id
	                        WHERE a.is_owner = 0 AND a.is_kid = 0`)
	if err != nil {
		return err
	}
	var kinder []string
	for rows.Next() {
		var id, grenzen string
		var verwaltung, eigene int
		var inGruppe bool
		if err := rows.Scan(&id, &verwaltung, &grenzen, &eigene, &inGruppe); err != nil {
			rows.Close()
			return err
		}
		if verwaltung > 0 {
			continue
		}
		alter := eigene
		if inGruppe && grenzen != "" {
			alter = limitsAus(grenzen).MaxAge
		}
		switch alter {
		case 0, 6, 12, 16:
			kinder = append(kinder, id)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range kinder {
		if _, err := db.Exec(`UPDATE account SET is_kid = 1 WHERE id = ?`, id); err != nil {
			return err
		}
	}
	return nil
}

// altersgrenzeImJSON setzt maxAge 0 auf OhneAltersgrenze. Bei Einladungen
// stecken die Grenzen unter "limits"; eine Einladung ohne Grenzen
// (maxStreams 0) bekommt ohnehin die Vorgabe und bleibt, wie sie ist.
func altersgrenzeImJSON(roh string, eingepackt bool) (string, bool) {
	if strings.TrimSpace(roh) == "" {
		return roh, false
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(roh), &obj); err != nil {
		return roh, false
	}
	lim := obj
	if eingepackt {
		l, ok := obj["limits"].(map[string]any)
		if !ok {
			return roh, false
		}
		if n, _ := l["maxStreams"].(float64); n == 0 {
			return roh, false
		}
		lim = l
	}
	alter, ok := lim["maxAge"].(float64)
	if !ok || alter != 0 {
		return roh, false
	}
	lim["maxAge"] = OhneAltersgrenze
	out, err := json.Marshal(obj)
	if err != nil {
		return roh, false
	}
	return string(out), true
}
