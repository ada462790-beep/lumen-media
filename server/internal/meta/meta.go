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
	"strings"
	"unicode"
)

// Candidate ist ein Treffer einer Quelle.
type Candidate struct {
	SourceID  string
	Source    string // tmdb | tvdb | omdb
	Title     string
	Original  string
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
	for _, a := range []string{"der ", "die ", "das ", "the ", "ein ", "eine ", "a ", "an "} {
		if strings.HasPrefix(out, a) {
			return strings.TrimPrefix(out, a)
		}
	}
	return out
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
func AgeYears(rating string) *int {
	r := strings.ToUpper(strings.TrimSpace(rating))
	r = strings.TrimPrefix(r, "FSK ")
	r = strings.TrimPrefix(r, "FSK-")

	table := map[string]int{
		// Deutschland, Österreich, Schweiz
		"0": 0, "6": 6, "12": 12, "16": 16, "18": 18,
		"AB 0": 0, "AB 6": 6, "AB 12": 12, "AB 16": 16, "AB 18": 18,
		// USA
		"G": 0, "TV-Y": 0, "TV-G": 0, "PG": 6, "TV-PG": 6,
		"PG-13": 12, "TV-14": 14, "R": 16, "TV-MA": 18, "NC-17": 18,
		// Grossbritannien
		"U": 0, "12A": 12, "15": 15,
	}
	if v, ok := table[r]; ok {
		return &v
	}
	return nil
}
