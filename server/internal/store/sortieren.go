// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

// Sortieren nach Titel — so, wie jemand im Regal sucht.
//
// Der Schlüssel, den Sortierschluessel baut, steht in item.sort_title (und
// bei Musik in titel_sort, name_sort …). SQLite vergleicht ihn Byte für
// Byte; darum muss er schon alles enthalten, was die Reihenfolge ausmacht:
//
//   - Artikel am Anfang fallen weg, aber nur die der Sprache des Titels:
//     „The Matrix" steht unter M, „Der Herr der Ringe" unter H — „Die Hard"
//     als englischer Titel aber unter D.
//   - Satzzeichen am Anfang zählen nicht: „...und dann kam Polly" steht
//     unter U, nicht unter „#".
//   - Umlaute und Akzente zählen wie der Grundbuchstabe (DIN 5007-1, die
//     Lexikon-Ordnung): „Ötzi" steht bei „Otto", nicht bei „Oetker". Für
//     die Suche gilt weiter fold() mit ä → ae — dort soll „Aerger" auch
//     „Ärger" finden.
//   - Zahlen zählen als Zahlen: „Teil 2" vor „Teil 10".

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

// artikel: je Sprache die Artikel, die am Anfang eines Titels wegfallen.
// Englische Titel bleiben in jedem Land oft unübersetzt („The Dark
// Knight" auch im deutschen Kino) — darum gelten die englischen Artikel
// zusätzlich überall, wo die Sprache nur die der Bibliothek ist. Ist die
// Sprache unbekannt, gelten deutsche und englische.
var artikel = map[string][]string{
	"de": {"der", "die", "das", "ein", "eine", "einen", "einem"},
	"en": {"the", "a", "an"},
	"fr": {"le", "la", "les", "l'", "un", "une"},
	"it": {"il", "lo", "la", "i", "gli", "le", "l'", "un", "una"},
	"es": {"el", "la", "los", "las", "un", "una"},
}

// artikelFuer nennt die Artikel, die für eine Sprache wegfallen.
func artikelFuer(sprache string) []string {
	s := strings.ToLower(strings.TrimSpace(sprache))
	if i := strings.IndexAny(s, "-_"); i > 0 {
		s = s[:i] // „de-CH" → „de"
	}
	switch s {
	case "en":
		return artikel["en"]
	case "de", "fr", "it", "es":
		return append(append([]string{}, artikel[s]...), artikel["en"]...)
	default:
		return append(append([]string{}, artikel["de"]...), artikel["en"]...)
	}
}

// SortierSprache entscheidet, nach welcher Sprache ein Titel sortiert
// wird: Ist der Titel der Originaltitel und die Originalsprache bekannt,
// nach dieser — „Die Hard" (englisch) behält sein „Die". Sonst ist der
// Titel in der Sprache der Bibliothek geholt worden.
func SortierSprache(bibliothek, original, titel, originaltitel string) string {
	if original != "" && originaltitel != "" && titelKlein(titel) == titelKlein(originaltitel) {
		return original
	}
	return bibliothek
}

// Sortierschluessel macht aus einem Titel den Schlüssel, nach dem sortiert
// wird. sprache ist ein Sprachkürzel („de", „en", „fr" …) oder leer.
func Sortierschluessel(titel, sprache string) string {
	t := strings.TrimSpace(titel)
	t = ohneVorspann(t)
	t = ohneArtikel(t, artikelFuer(sprache))
	t = dinFalten(t)
	return zahlenAuffuellen(t)
}

// ohneVorspann wirft alles vor dem ersten Buchstaben oder der ersten
// Ziffer weg — Anführungszeichen, Punkte, Klammern. Besteht der Titel nur
// aus Zeichen, bleibt er, wie er ist.
func ohneVorspann(t string) string {
	for i, r := range t {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return t[i:]
		}
	}
	return t
}

// ohneArtikel lässt genau einen Artikel am Anfang weg. Danach muss ein
// Leerzeichen kommen („A-Team" bleibt), bei „l'" ein Buchstabe
// („L'Odyssée"). Bliebe nichts übrig („The"), bleibt der Artikel.
func ohneArtikel(t string, liste []string) string {
	klein := strings.ToLower(strings.ReplaceAll(t, "’", "'"))
	for _, a := range liste {
		if !strings.HasPrefix(klein, a) {
			continue
		}
		rest := klein[len(a):]
		if strings.HasSuffix(a, "'") {
			r, _ := utf8.DecodeRuneInString(rest)
			if !unicode.IsLetter(r) {
				continue
			}
		} else {
			if !strings.HasPrefix(rest, " ") {
				continue
			}
		}
		// Gleiche Länge in Bytes: ’ ist länger als ', darum über die
		// Zahl der Zeichen gehen, nicht über die Bytes.
		ohne := string([]rune(t)[utf8.RuneCountInString(a):])
		ohne = ohneVorspann(strings.TrimSpace(ohne))
		if strings.TrimSpace(ohne) == "" || !hatBuchstabeOderZiffer(ohne) {
			continue
		}
		return ohne
	}
	return t
}

func hatBuchstabeOderZiffer(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

// dinTabelle: Buchstaben, die nach DIN 5007-1 wie ihr Grundbuchstabe
// zählen, und Ligaturen, die ausgeschrieben werden. Kleinbuchstaben
// genügen: dinFalten schreibt vorher klein.
var dinTabelle = map[rune]string{
	'ß': "ss", 'æ': "ae", 'œ': "oe", 'ø': "o", 'ł': "l", 'đ': "d", 'ð': "d", 'þ': "th",
	'ı': "i", 'ŀ': "l", 'ŉ': "n", 'ĳ': "ij", 'ſ': "s",
	'à': "a", 'á': "a", 'â': "a", 'ã': "a", 'ä': "a", 'å': "a", 'ā': "a", 'ă': "a", 'ą': "a",
	'ç': "c", 'ć': "c", 'ĉ': "c", 'ċ': "c", 'č': "c",
	'ď': "d",
	'è': "e", 'é': "e", 'ê': "e", 'ë': "e", 'ē': "e", 'ĕ': "e", 'ė': "e", 'ę': "e", 'ě': "e",
	'ĝ': "g", 'ğ': "g", 'ġ': "g", 'ģ': "g",
	'ĥ': "h", 'ħ': "h",
	'ì': "i", 'í': "i", 'î': "i", 'ï': "i", 'ĩ': "i", 'ī': "i", 'ĭ': "i", 'į': "i",
	'ĵ': "j", 'ķ': "k",
	'ĺ': "l", 'ļ': "l", 'ľ': "l",
	'ñ': "n", 'ń': "n", 'ņ': "n", 'ň': "n",
	'ò': "o", 'ó': "o", 'ô': "o", 'õ': "o", 'ö': "o", 'ō': "o", 'ŏ': "o", 'ő': "o",
	'ŕ': "r", 'ŗ': "r", 'ř': "r",
	'ś': "s", 'ŝ': "s", 'ş': "s", 'š': "s", 'ș': "s",
	'ţ': "t", 'ť': "t", 'ŧ': "t", 'ț': "t",
	'ù': "u", 'ú': "u", 'û': "u", 'ü': "u", 'ũ': "u", 'ū': "u", 'ŭ': "u", 'ů': "u", 'ű': "u", 'ų': "u",
	'ŵ': "w",
	'ý': "y", 'ÿ': "y", 'ŷ': "y",
	'ź': "z", 'ż': "z", 'ž': "z",
}

// dinFalten schreibt klein und ersetzt Buchstaben mit Zeichen durch ihren
// Grundbuchstaben. Zerlegt geschriebene Zeichen („a" + Trema als eigenes
// Zeichen, wie es macOS in Dateinamen schreibt) verlieren ihr Zeichen.
func dinFalten(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range strings.ToLower(s) {
		if unicode.Is(unicode.Mn, r) {
			continue // Akzent als eigenes Zeichen
		}
		if e, ok := dinTabelle[r]; ok {
			b.WriteString(e)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// zahlenStellen: so viele Stellen bekommt jede Zahl im Schlüssel.
const zahlenStellen = 10

// zahlenAuffuellen füllt jede Ziffernfolge vorn mit Nullen auf, damit
// „2" vor „10" kommt. Längere Zahlen bleiben, wie sie sind.
func zahlenAuffuellen(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 8)
	for i := 0; i < len(s); {
		if s[i] < '0' || s[i] > '9' {
			b.WriteByte(s[i])
			i++
			continue
		}
		j := i
		for j < len(s) && s[j] >= '0' && s[j] <= '9' {
			j++
		}
		for k := j - i; k < zahlenStellen; k++ {
			b.WriteByte('0')
		}
		b.WriteString(s[i:j])
		i = j
	}
	return b.String()
}

// Buchstabe nennt, unter welchem Buchstaben der Leiste ein Schlüssel
// steht: „A" … „Z", sonst „#" (Zahlen und andere Schriften).
func Buchstabe(schluessel string) string {
	if schluessel != "" && schluessel[0] >= 'a' && schluessel[0] <= 'z' {
		return strings.ToUpper(schluessel[:1])
	}
	return "#"
}

// sortierungSetzen rechnet vor dem Schreiben den Schlüssel eines Titels:
// aus dem eigenen Sortiertitel, sonst aus dem Titel, in der Sprache, die
// SortierSprache wählt. Bringt der Aufrufer keinen eigenen Sortiertitel
// und keine Originalsprache mit, gelten die gespeicherten — der Scanner
// kennt beide nicht und soll sie nicht löschen.
func sortierungSetzen(ctx context.Context, q abfrager, it *Item) error {
	var bib, eigen, sprache sql.NullString
	err := q.QueryRowContext(ctx,
		`SELECT l.language, i.sort_eigen, i.sprache_original
		   FROM library l LEFT JOIN item i ON i.id = ?
		  WHERE l.id = ?`, it.ID, it.LibraryID).Scan(&bib, &eigen, &sprache)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if strings.TrimSpace(it.SortEigen) == "" {
		it.SortEigen = eigen.String
	}
	if it.Sprache == "" {
		it.Sprache = sprache.String
	}
	it.SortTitle = schluesselFuer(it, bib.String)
	return nil
}

// schluesselFuer: der Schlüssel eines Titels in einer Bibliothek dieser
// Sprache.
func schluesselFuer(it *Item, bibliothek string) string {
	basis := strings.TrimSpace(it.SortEigen)
	if basis == "" {
		basis = it.Title
	}
	return Sortierschluessel(basis, SortierSprache(bibliothek, it.Sprache, it.Title, it.OriginalTitle))
}

// sortierungNachrechnen setzt den Schlüssel eines gespeicherten Titels
// neu — nach einer Bearbeitung von Hand.
func sortierungNachrechnen(ctx context.Context, t *sql.Tx, itemID string) error {
	it, err := scanItem(t.QueryRowContext(ctx, `SELECT `+itemCols+` FROM item WHERE id = ?`, itemID))
	if err != nil {
		return err
	}
	var bib string
	_ = t.QueryRowContext(ctx, `SELECT language FROM library WHERE id = ?`, it.LibraryID).Scan(&bib)
	_, err = t.ExecContext(ctx, `UPDATE item SET sort_title = ? WHERE id = ?`, schluesselFuer(it, bib), itemID)
	return err
}

// sortierungUmbauen rechnet für alle Titel und alle Musik die Schlüssel
// neu (Umbau auf Stand 9). Ein von Hand eingetragener Sortiertitel stand
// bisher nur in feld_sperre.anzeige; er wird zu sort_eigen.
//
// In Häppchen zu tausend Zeilen gelesen, aber in einer Transaktion
// geschrieben: Bricht der Umbau ab, bleibt alles beim Alten, und beim
// nächsten Start beginnt er von vorn.
func sortierungUmbauen(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`UPDATE item SET sort_eigen = (SELECT x.anzeige FROM feld_sperre x
	                        WHERE x.item_id = item.id AND x.feld = 'sortTitle')
	                       WHERE sort_eigen IS NULL AND EXISTS (SELECT 1 FROM feld_sperre x
	                        WHERE x.item_id = item.id AND x.feld = 'sortTitle' AND COALESCE(x.anzeige,'') != '')`); err != nil {
		return err
	}
	sprachen := map[string]string{}
	if err := zeilenLesen(tx, `SELECT id, language FROM library`, func(r *sql.Rows) error {
		var id, l string
		if err := r.Scan(&id, &l); err != nil {
			return err
		}
		sprachen[id] = l
		return nil
	}); err != nil {
		return err
	}

	type neu struct{ id, schluessel string }
	letzte := ""
	for {
		var stapel []neu
		err := zeilenLesen(tx, `SELECT id, library_id, title, COALESCE(original_title,''),
		       COALESCE(sort_eigen,''), COALESCE(sprache_original,'')
		  FROM item WHERE id > ? ORDER BY id LIMIT 1000`, func(r *sql.Rows) error {
			var it Item
			if err := r.Scan(&it.ID, &it.LibraryID, &it.Title, &it.OriginalTitle, &it.SortEigen, &it.Sprache); err != nil {
				return err
			}
			stapel = append(stapel, neu{it.ID, schluesselFuer(&it, sprachen[it.LibraryID])})
			return nil
		}, letzte)
		if err != nil {
			return err
		}
		if len(stapel) == 0 {
			break
		}
		for _, n := range stapel {
			if _, err := tx.Exec(`UPDATE item SET sort_title = ? WHERE id = ?`, n.schluessel, n.id); err != nil {
				return err
			}
		}
		letzte = stapel[len(stapel)-1].id
	}
	if err := musikSortierungUmbauen(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// zeilenLesen liest eine Abfrage ganz und schliesst sie wieder, bevor
// in derselben Transaktion geschrieben wird.
func zeilenLesen(tx *sql.Tx, q string, fn func(*sql.Rows) error, args ...any) error {
	rows, err := tx.Query(q, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := fn(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}
