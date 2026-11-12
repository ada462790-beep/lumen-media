// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

// Package meta holt Beschreibungen, Poster und Wertungen.
//
// Der Besitzer richtet die Quellen einmal ein, dann gelten sie für alle —
// niemand im Haushalt braucht ein eigenes Konto bei TMDb oder IMDb.
//
// Der schwierigste Teil ist nicht das Abfragen, sondern das Zuordnen: der
// Scanner hat „Die Bucht, 2019" geraten, die Quelle liefert fünf Treffer,
// und einer davon ist der richtige. Lieber ehrlich „nicht erkannt" melden
// als still den falschen Film in die Mediathek stellen — ein falscher
// Treffer ist schlimmer als gar keiner, weil ihn niemand mehr nachprüft.
package meta

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// Candidate ist ein Treffer einer Quelle.
type Candidate struct {
	SourceID string
	Source   string // tmdb | tvdb | omdb
	Title    string
	Original string
	// Sprache: die Originalsprache („en", „fr" …) — nach ihr wird
	// sortiert, wenn der Titel der Originaltitel ist.
	Sprache   string
	Year      int
	Overview  string
	Premiered string
	RuntimeMS int64
	Genres    []string
	AgeRating string
	AgeYears  *int
	IMDbID    string
	// TVDBID: Kennung bei TheTVDB (nur Serien, kommt über TMDb) — für den
	// Link auf der Titelseite.
	TVDBID      string
	IMDbRating  float64
	IMDbVotes   int
	PosterURL   string
	BackdropURL string
	Popularity  float64
	// Die Filmreihe, zu der TMDb den Film zählt — leer, wenn keine.
	ReiheID   string
	ReiheName string
	// Die Besetzung, die wichtigsten zuerst.
	Cast []CastEintrag
	// Serien: Jahr der letzten Folge und ob noch neue kommen. Nur aus den
	// Einzelheiten, nicht aus der Suche (ZeitraumBekannt sagt, ob gesetzt).
	EndYear         int
	LaeuftNoch      bool
	ZeitraumBekannt bool
	// Laender: origin_country einer Serie („US", „GB") — trennt „The
	// Office" aus den USA von dem aus Grossbritannien.
	Laender []string
	// Alternativ: weitere Titel desselben Eintrags — der englische aus
	// einer zweiten Suche, die Alternativtitel von TMDb. Sie zählen beim
	// Vergleich wie Titel und Originaltitel.
	Alternativ []string
}

type CastEintrag struct {
	Name    string
	Rolle   string
	BildURL string
}

// Source ist das, was Lumen von einer Quelle braucht. Mehr nicht — dann
// lässt sich eine weitere Quelle in einer Datei nachrüsten.
type Source interface {
	Name() string
	SearchMovie(ctx context.Context, title string, year int) ([]Candidate, error)
	SearchSeries(ctx context.Context, title string) ([]Candidate, error)
	Episode(ctx context.Context, seriesID string, season, episode int) (*Candidate, error)
}

// ---------------------------------------------------------------- Zuordnen

// Match ist das Ergebnis einer Zuordnung samt Begründung.
type Match struct {
	Candidate *Candidate
	Score     float64
	Sure      bool
	Why       string
}

// minSure ist die Schwelle, ab der Lumen einen Treffer ohne Rückfrage
// übernimmt. Darunter landet der Titel in „nicht erkannt", wo der Besitzer
// ihn in Ruhe zuordnen kann.
const minSure = 0.74

// Best wählt aus den Treffern den passenden — oder keinen.
func Best(guessTitle string, guessYear int, cands []Candidate) Match {
	if len(cands) == 0 {
		return Match{Why: "keine Treffer"}
	}
	best := Match{}
	var runnerUp float64
	var zweiter *Candidate

	for i := range cands {
		s := Score(guessTitle, guessYear, cands[i])
		if s > best.Score {
			runnerUp, zweiter = best.Score, best.Candidate
			best = Match{Candidate: &cands[i], Score: s}
		} else if s > runnerUp {
			runnerUp, zweiter = s, &cands[i]
		}
	}

	// Gleich gut, aber einer ist weit bekannter: „Arrow" ohne Jahr ist
	// fast immer die Serie von 2012, nicht die gleichnamige aus 1990, die
	// kaum jemand kennt. Emby und Plex nehmen dann den bekannteren — Lumen
	// auch, aber nur bei einem deutlichen Abstand (fünffach). Sonst bleibt
	// es bei der Rückfrage.
	klarBekannter := zweiter != nil && best.Candidate.Popularity > 0 &&
		best.Candidate.Popularity >= 5*max(zweiter.Popularity, 0.2)
	if !klarBekannter && zweiter != nil && zweiter.Popularity >= 5*max(best.Candidate.Popularity, 0.2) &&
		best.Score-runnerUp < 0.08 {
		best.Candidate, best.Score, runnerUp = zweiter, runnerUp, best.Score
		klarBekannter = true
	}

	best.Sure = best.Score >= minSure
	switch {
	case !best.Sure:
		best.Why = "zu unsicher"
	case best.Score-runnerUp < 0.08 && runnerUp >= minSure && klarBekannter:
		best.Why = "zwei passende Treffer, der deutlich bekanntere gewinnt"
	case best.Score-runnerUp < 0.08 && runnerUp >= minSure:
		// Zwei fast gleich gute Treffer: typisch für Neuverfilmungen. Lieber
		// nachfragen, als zu raten.
		best.Sure = false
		best.Why = "zwei Treffer liegen zu nah beieinander"
	default:
		best.Why = "eindeutig"
	}
	return best
}

// Score bewertet einen Treffer zwischen 0 und 1.
//
// Der Titel wiegt am schwersten, das Jahr entscheidet die knappen Fälle.
// Stimmt das Jahr grob, aber der Titel kaum, ist es trotzdem der falsche
// Film — darum multipliziert das Jahr den Titelwert, statt ihn zu ersetzen.
func Score(guessTitle string, guessYear int, c Candidate) float64 {
	g := normalize(guessTitle)
	title := similarity(g, normalize(c.Title))
	if orig := similarity(g, normalize(c.Original)); orig > title {
		title = orig
	}
	for _, a := range c.Alternativ {
		if alt := similarity(g, normalize(a)); alt > title {
			title = alt
		}
	}
	if title < 0.3 {
		return title * 0.3 // hoffnungslos daneben
	}

	year := 1.0
	switch {
	case guessYear == 0 || c.Year == 0:
		// Ohne Jahr bleibt nur der Titel — dann reicht es nur bei einem
		// sehr klaren Treffer.
		year = 0.82
	case guessYear == c.Year:
		year = 1.0
	case abs(guessYear-c.Year) == 1:
		// Ein Jahr daneben ist normal: Kinostart gegen Heimkino, oder ein
		// Land später.
		year = 0.94
	case abs(guessYear-c.Year) <= 3:
		year = 0.7
	default:
		year = 0.35
	}

	// Ein sehr bekannter Titel ist bei gleichem Namen der wahrscheinlichere.
	// Das ist nur ein Hauch Gewicht, kein Ausschlag.
	fame := 1.0
	if c.Popularity > 0 {
		fame = 1.0 + min(c.Popularity/500, 0.03)
	}
	return min(title*year*fame, 1.0)
}

// normalize macht Titel vergleichbar: klein, ohne Umlautunterschiede, ohne
// Satzzeichen, ohne führenden Artikel.
func normalize(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	// „Herr der Ringe, Der" ist „Der Herr der Ringe" — auch mit einem
	// Untertitel dahinter („Bucht, Die: Teil 2").
	s = artikelNachVorne(s)
	s = strings.NewReplacer(
		"ä", "ae", "ö", "oe", "ü", "ue", "ß", "ss",
		"á", "a", "à", "a", "â", "a", "é", "e", "è", "e", "ê", "e",
		"í", "i", "ì", "i", "ó", "o", "ò", "o", "ô", "o", "ú", "u", "ù", "u",
		"ñ", "n", "ç", "c", "&", "und",
	).Replace(s)

	var b strings.Builder
	lastSpace := true
	for _, r := range s {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			lastSpace = false
		case !lastSpace:
			b.WriteRune(' ')
			lastSpace = true
		}
	}
	out := strings.TrimSpace(b.String())
	// „Teil II" und „Teil 2", „Rocky IV" und „Rocky 4" sind dasselbe;
	// „and" und „und" ebenso („Fast & Furious" kommt als „und").
	woerter := strings.Fields(out)
	for i, w := range woerter {
		if n, ok := roemisch[w]; ok {
			woerter[i] = n
		} else if w == "and" {
			woerter[i] = "und"
		}
	}
	out = strings.Join(woerter, " ")
	for _, a := range []string{"der ", "die ", "das ", "the ", "ein ", "eine ", "a ", "an "} {
		if strings.HasPrefix(out, a) {
			return strings.TrimPrefix(out, a)
		}
	}
	return out
}

// roemisch: Zahlen bis zwanzig, wie sie in Filmtiteln vorkommen. Auch
// „i" gehört dazu: „I, Robot" wird auf beiden Seiten zu „1 robot", der
// Vergleich bleibt gleich.
var roemisch = map[string]string{
	"i": "1", "ii": "2", "iii": "3", "iv": "4", "v": "5", "vi": "6", "vii": "7",
	"viii": "8", "ix": "9", "x": "10", "xi": "11", "xii": "12", "xiii": "13",
	"xiv": "14", "xv": "15", "xvi": "16", "xvii": "17", "xviii": "18", "xix": "19", "xx": "20",
}

var reArtikelHinten = regexp.MustCompile(`(?i)^(.+?),\s*(der|die|das|the|a|an|ein|eine|le|la|les|el|il)\b(.*)$`)

// artikelNachVorne: „Bucht, Die" → „Die Bucht", „Herr der Ringe, Der:
// Die Gefährten" → „Der Herr der Ringe: Die Gefährten".
func artikelNachVorne(s string) string {
	m := reArtikelHinten.FindStringSubmatch(s)
	if m == nil {
		return s
	}
	rest := strings.TrimSpace(m[3])
	// Hinter dem Artikel darf nur ein Untertitel kommen, kein weiterer
	// Satzteil: „Ich, die Frau" bleibt, wie es ist.
	if rest != "" && !strings.HasPrefix(rest, ":") && !strings.HasPrefix(rest, "-") && !strings.HasPrefix(rest, "(") {
		return s
	}
	return strings.TrimSpace(m[2] + " " + m[1] + " " + rest)
}

// similarity vergleicht zwei Titel. Grundlage ist die Editierdistanz; wenn
// einer im anderen steckt, zählt das zusätzlich — „Nordlicht" und
// „Nordlicht — Die Rückkehr" gehören oft zusammen.
func similarity(a, b string) float64 {
	if a == "" || b == "" {
		return 0
	}
	if a == b {
		return 1
	}
	d := levenshtein(a, b)
	longer := max(len(a), len(b))
	base := 1 - float64(d)/float64(longer)

	if strings.HasPrefix(b, a) || strings.HasPrefix(a, b) {
		// Ein Präfix-Treffer ist aussagekräftiger als die reine Distanz
		// nahelegt — aber nur, wenn der kürzere Teil Substanz hat.
		shorter := min(len(a), len(b))
		if shorter >= 5 {
			prefix := 0.6 + 0.4*float64(shorter)/float64(longer)
			if prefix > base {
				base = prefix
			}
		}
	}
	if base < 0 {
		return 0
	}
	return base
}

func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(min(cur[j-1]+1, prev[j]+1), prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}

func abs(i int) int {
	if i < 0 {
		return -i
	}
	return i
}

// ---------------------------------------------------------------- Altersfreigabe

// AgeYears übersetzt die Kennzeichen der Länder in eine Zahl, mit der sich
// rechnen lässt. Ohne das liesse sich keine Grenze „höchstens ab 12" setzen.
//
// Die Kennzeichen kommen in vielen Schreibweisen: von TMDb („16"), aus
// Emby- und Jellyfin-NFOs („DE-16", „CH-16", „AT-14", „GB-15",
// „US-PG-13"), von Kodi („de:FSK 16"), von tinyMediaManager („US:R / DE:16")
// und von Hand („FSK16", „ab 12 Jahren", „16+"). Was Lumen nicht sicher
// lesen kann, ergibt nil — „ungeprüft", nie „ab 0": Ein unbekanntes
// Kennzeichen darf einen Film nicht für Kinder freigeben.
func AgeYears(rating string) *int {
	r := strings.ToUpper(strings.TrimSpace(rating))
	if r == "" {
		return nil
	}
	// Mehrere Länder: das deutschsprachige zuerst, sonst das erste, das
	// sich lesen lässt.
	teile := freigabeTeile(r)
	if len(teile) > 1 {
		for _, t := range teile {
			if land, rest := landAbtrennen(t); land == "DE" || land == "CH" || land == "AT" {
				if v := alterAus(rest); v != nil {
					return v
				}
			}
		}
		for _, t := range teile {
			if v := alterAus(t); v != nil {
				return v
			}
		}
		return nil
	}
	if len(teile) == 1 {
		r = teile[0]
	}
	return alterAus(r)
}

// freigabeTeile trennt „US:R / DE:16" in seine Länder. „DE/16" (so legt
// Emby es mitunter ab) ist dagegen ein Land mit seinem Kennzeichen.
func freigabeTeile(r string) []string {
	if !strings.Contains(r, "/") {
		return []string{r}
	}
	teile := strings.Split(r, "/")
	if len(teile) == 2 {
		if _, ok := laender[strings.TrimSpace(teile[0])]; ok {
			return []string{strings.TrimSpace(teile[0]) + "-" + strings.TrimSpace(teile[1])}
		}
	}
	out := make([]string, 0, len(teile))
	for _, t := range teile {
		if t = strings.TrimSpace(t); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// laender sind die Vorsätze, die vor einem Kennzeichen stehen können —
// das Land und, was Emby für Deutschland schreibt, „FSK".
var laender = map[string]string{
	"DE": "DE", "DEU": "DE", "GER": "DE", "GERMANY": "DE", "DEUTSCHLAND": "DE",
	"CH": "CH", "CHE": "CH", "SWITZERLAND": "CH", "SCHWEIZ": "CH",
	"AT": "AT", "AUT": "AT", "AUSTRIA": "AT", "ÖSTERREICH": "AT",
	"GB": "GB", "UK": "GB", "US": "US", "USA": "US",
	"FR": "FR", "NL": "NL", "IT": "IT", "ES": "ES", "CA": "CA", "AU": "AU",
}

var reLandVorsatz = regexp.MustCompile(`^([A-ZÄÖÜ]{2,11})\s*[-:_ ]\s*(.+)$`)

// landAbtrennen: „DE-16" → „DE", „16"; „US-PG-13" → „US", „PG-13".
func landAbtrennen(r string) (land, rest string) {
	r = strings.TrimSpace(r)
	if m := reLandVorsatz.FindStringSubmatch(r); m != nil {
		if l, ok := laender[m[1]]; ok {
			return l, strings.TrimSpace(m[2])
		}
	}
	return "", r
}

var (
	// „FSK 16", „FSK-16", „FSK16", „FSK: 16", „FSK ab 16"
	reFSK = regexp.MustCompile(`^(?:FSK|FSF|JMK|SPIO/JK)\s*[-: ]?\s*(?:AB\s*)?(\d{1,2})\b`)
	// „ab 12", „ab 12 Jahren", „freigegeben ab 12 Jahren", „12 Jahre"
	reAbJahren = regexp.MustCompile(`^(?:FREIGEGEBEN\s+)?(?:AB\s*)?(\d{1,2})\s*(?:\+|J\.?|JAHREN?)?$`)
)

// alterTabelle: Kennzeichen ohne Zahl (oder mit einer Zahl, die nicht das
// Alter ist). Zahlen allein liest alterAus selbst.
var alterTabelle = map[string]int{
	// USA (Kino und Fernsehen)
	"G": 0, "PG": 6, "PG-13": 12, "PG13": 12, "R": 16, "NC-17": 18, "NC17": 18, "X": 18,
	"TV-Y": 0, "TV-G": 0, "TV-Y7": 7, "TV-Y7-FV": 7, "TV-PG": 6, "TV-14": 14, "TV-MA": 18,
	// Grossbritannien
	"U": 0, "UC": 0, "12A": 12, "R18": 18,
	// Deutschland: „ohne Altersbeschränkung", „keine Jugendfreigabe"
	"O.AL.": 0, "OHNE ALTERSBESCHRÄNKUNG": 0, "KJ": 18, "KEINE JUGENDFREIGABE": 18,
	"JUGENDFREI": 0, "ALL": 0,
	// Frankreich, Niederlande
	"TP": 0, "AL": 0,
}

// alterAus liest ein einzelnes Kennzeichen, mit oder ohne Land davor.
func alterAus(r string) *int {
	r = strings.TrimSpace(r)
	r = strings.TrimSpace(strings.TrimPrefix(r, "RATED "))
	// Land vorne weg, auch zweimal („DE-FSK 16").
	for i := 0; i < 2; i++ {
		if l, rest := landAbtrennen(r); l != "" {
			r = rest
		}
	}
	r = strings.TrimSpace(strings.TrimPrefix(r, "RATED "))
	if v, ok := alterTabelle[r]; ok {
		return &v
	}
	zahl := func(s string) *int {
		n, err := strconv.Atoi(s)
		if err != nil || n < 0 || n > 21 {
			return nil
		}
		return &n
	}
	if m := reFSK.FindStringSubmatch(r); m != nil {
		return zahl(m[1])
	}
	if m := reAbJahren.FindStringSubmatch(r); m != nil {
		return zahl(m[1])
	}
	return nil
}
