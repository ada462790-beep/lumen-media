// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package meta

// Abgleich: wie Lumen einen Titel bei TMDb findet, wenn die erste Suche
// nichts Sicheres bringt.
//
// Die Leiter geht vom Billigen zum Teuren und hört auf, sobald ein
// Treffer sicher ist:
//
//  1. Der Titel, wie der Scanner ihn gelesen hat — mit Jahr und ohne.
//  2. Gleichwertige Schreibweisen: ohne Fassungswörter („Director's
//     Cut", „Kinofassung", „3D"), „Herr der Ringe, Der" umgestellt,
//     „&" als „und"/„and", römische Zahlen als Ziffern und umgekehrt,
//     ohne Satzzeichen, „oe" als „ö" und „ö" als „oe".
//  3. Dieselben Titel mit englischen Titeln in den Treffern — die Datei
//     heisst oft „Spirited Away", nicht „Chihiros Reise ins Zauberland".
//  4. Das Jahr eins daneben (Kinostart gegen Heimkino).
//  5. Nur der Teil vor dem Untertitel („Star Trek II - …" → „Star Trek
//     II"). Das bringt nur Kandidaten; bewertet wird weiter mit dem
//     ganzen Titel.
//
// Bewertet wird jeder Kandidat mit allen gleichwertigen Schreibweisen
// (Score, die beste zählt); sicher ist ein Treffer erst über minSure und
// mit Abstand zum zweiten (Best). Ist danach keiner sicher, holt Lumen
// für die zwei besten die Alternativtitel von TMDb — eine Anfrage je
// Kandidat, nur in diesem Fall.
//
// Scheitert eine Anfrage (kein Netz, TMDb bremst), bricht die Leiter ab
// und der Titel bleibt „ausstehend": Er kommt beim nächsten Durchgang
// wieder dran und landet nicht als „nicht erkannt" in der Verwaltung.

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

// Variante ist eine Schreibweise, unter der gesucht wird.
type Variante struct {
	Text string
	// Gleichwertig: dieselbe Bezeichnung, nur anders geschrieben — zählt
	// auch beim Bewerten. Ein gekürzter Titel (ohne Untertitel) nicht.
	Gleichwertig bool
}

var (
	// Fassungs- und Ausgabewörter: Sie stehen im Dateinamen, aber nie im
	// Titel bei TMDb.
	reFassung      = regexp.MustCompile(`(?i)(?:^|[\s._\-(\[])(director'?s[\s._-]*cut|extended(?:[\s._-]*(?:cut|edition|version))?|unrated|uncut|ungeschnitten|remastered|restored|theatrical(?:[\s._-]*cut)?|final[\s._-]*cut|special[\s._-]*edition|collector'?s[\s._-]*edition|limited[\s._-]*edition|anniversary[\s._-]*edition|ultimate[\s._-]*(?:cut|edition)|kinofassung|kinoversion|langfassung|imax|3d|hsbs|hou|sbs)(?:$|[\s._\-)\]])`)
	reMehrfachRaum = regexp.MustCompile(`\s+`)
	reUntertitel   = regexp.MustCompile(`^(.{3,}?)\s*(?::|\s-\s|\s–\s)\s*.+$`)
	reRoemEnde     = regexp.MustCompile(`(?i)^(.*\s)(ii|iii|iv|v|vi|vii|viii|ix|x)(\s*(?::.*)?)$`)
	reUndAnd       = regexp.MustCompile(`(?i)\s(?:und|and)\s`)
	reZifferEnde   = regexp.MustCompile(`^(.*\s)([2-9]|10)(\s*(?::.*)?)$`)
)

// Suchvarianten liefert die Schreibweisen eines Titels in der Reihenfolge,
// in der gesucht wird — ohne doppelte, der Titel selbst zuerst.
func Suchvarianten(titel string) []Variante {
	titel = strings.TrimSpace(titel)
	var out []Variante
	gesehen := map[string]bool{}
	dazu := func(s string, gleich bool) {
		s = strings.TrimSpace(reMehrfachRaum.ReplaceAllString(s, " "))
		s = strings.Trim(s, " -:,")
		k := strings.ToLower(s)
		if len([]rune(s)) < 2 || gesehen[k] {
			return
		}
		gesehen[k] = true
		out = append(out, Variante{Text: s, Gleichwertig: gleich})
	}
	dazu(titel, true)

	ohneFassung := titel
	for i := 0; i < 4; i++ { // „Extended Collector's Edition 3D"
		neu := reFassung.ReplaceAllString(ohneFassung, " ")
		if neu == ohneFassung {
			break
		}
		ohneFassung = neu
	}
	dazu(ohneFassung, true)
	basis := ohneFassung

	dazu(artikelNachVorneGross(basis), true)
	if strings.Contains(basis, "&") {
		dazu(strings.ReplaceAll(basis, "&", "und"), true)
		dazu(strings.ReplaceAll(basis, "&", "and"), true)
	} else if r := reUndAnd.ReplaceAllString(basis, " & "); r != basis {
		dazu(r, true)
	}
	if m := reRoemEnde.FindStringSubmatch(basis); m != nil {
		dazu(m[1]+roemisch[strings.ToLower(m[2])]+m[3], true)
	}
	if m := reZifferEnde.FindStringSubmatch(basis); m != nil {
		dazu(m[1]+ziffernRoemisch[m[2]]+m[3], true)
	}
	dazu(ohneSatzzeichen(basis), true)
	if u := umlauteSetzen(basis); u != basis {
		dazu(u, true)
	}
	if u := umlauteAufloesen(basis); u != basis {
		dazu(u, true)
	}
	if m := reUntertitel.FindStringSubmatch(basis); m != nil {
		dazu(m[1], false)
	}
	return out
}

var ziffernRoemisch = map[string]string{"2": "II", "3": "III", "4": "IV", "5": "V",
	"6": "VI", "7": "VII", "8": "VIII", "9": "IX", "10": "X"}

// artikelNachVorneGross: wie artikelNachVorne, aber mit der Schreibweise
// des Titels (für die Suche, nicht nur zum Vergleichen).
func artikelNachVorneGross(s string) string {
	m := reArtikelHinten.FindStringSubmatch(s)
	if m == nil {
		return s
	}
	rest := strings.TrimSpace(m[3])
	if rest != "" && !strings.HasPrefix(rest, ":") && !strings.HasPrefix(rest, "-") && !strings.HasPrefix(rest, "(") {
		return s
	}
	art := m[2]
	if art != "" {
		art = strings.ToUpper(art[:1]) + art[1:]
	}
	if strings.HasPrefix(rest, ":") {
		return strings.TrimSpace(art + " " + m[1] + rest)
	}
	return strings.TrimSpace(art + " " + m[1] + " " + rest)
}

func ohneSatzzeichen(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '\'':
			b.WriteRune(r)
		default:
			b.WriteRune(' ')
		}
	}
	return b.String()
}

// umlauteSetzen: „Fack ju Goehte" → „Fack ju Göhte". Nur, wenn der Titel
// noch keine Umlaute hat. Ein falsches „Blü" statt „Blue" schadet nicht:
// Die Suche findet dann eben nichts, bewertet wird ohne Umlaute.
func umlauteSetzen(s string) string {
	if strings.ContainsAny(s, "äöüÄÖÜ") {
		return s
	}
	return strings.NewReplacer("ae", "ä", "oe", "ö", "ue", "ü", "Ae", "Ä", "Oe", "Ö", "Ue", "Ü").Replace(s)
}

func umlauteAufloesen(s string) string {
	return strings.NewReplacer("ä", "ae", "ö", "oe", "ü", "ue", "Ä", "Ae", "Ö", "Oe", "Ü", "Ue", "ß", "ss").Replace(s)
}

// ScoreVarianten bewertet einen Kandidaten mit der besten gleichwertigen
// Schreibweise.
func ScoreVarianten(varianten []Variante, jahr int, c Candidate) float64 {
	best := 0.0
	for _, v := range varianten {
		if !v.Gleichwertig {
			continue
		}
		if s := Score(v.Text, jahr, c); s > best {
			best = s
		}
	}
	return best
}

// BestVarianten wählt wie Best, aber mit allen gleichwertigen
// Schreibweisen. Dafür wird für jede Schreibweise einmal Best gerechnet
// und das sicherste Ergebnis genommen — die Regeln für „zu nah
// beieinander" gelten so unverändert.
func BestVarianten(varianten []Variante, jahr int, cands []Candidate) Match {
	var best Match
	erster := true
	for _, v := range varianten {
		if !v.Gleichwertig {
			continue
		}
		m := Best(v.Text, jahr, cands)
		if erster || (m.Sure && !best.Sure) || (m.Sure == best.Sure && m.Score > best.Score) {
			best, erster = m, false
		}
	}
	if erster {
		return Match{Why: "keine Treffer"}
	}
	return best
}

// ---------------------------------------------------------------- Leiter

// suchSchritt ist eine Anfrage bei TMDb.
type suchSchritt struct {
	text    string
	jahr    int // 0 = ohne Jahr
	sprache string
}

// maxAnfragen begrenzt die Leiter je Titel. Ein Titel, der nach so vielen
// Versuchen nicht sicher ist, gehört in die Hand des Besitzers.
const maxAnfragen = 14

// Gesucht beschreibt, was die Leiter versucht hat — für die Verwaltung.
type Gesucht struct {
	Anfragen int
	Texte    []string
}

// finden geht die Leiter für einen Film oder eine Serie hinauf. land
// filtert Serien nach Herkunftsland („The Office (US)").
func (r *Resolver) finden(ctx context.Context, kind, titel string, jahr int, land string) (Match, Gesucht, []Candidate, error) {
	varianten := Suchvarianten(titel)
	alle := map[string]*Candidate{}
	var reihenfolge []string
	var g Gesucht
	englisch := "en-US"
	if strings.HasPrefix(strings.ToLower(r.tmdb.Language), "en") {
		englisch = ""
	}

	frage := func(s suchSchritt) error {
		if g.Anfragen >= maxAnfragen {
			return nil
		}
		g.Anfragen++
		var cands []Candidate
		var err error
		if kind == "series" {
			cands, err = r.tmdb.SearchSeriesSprache(ctx, s.text, s.jahr, s.sprache)
		} else {
			cands, err = r.tmdb.SearchMovieSprache(ctx, s.text, s.jahr, s.sprache)
		}
		if err != nil {
			return err
		}
		if !enthaeltText(g.Texte, s.text) {
			g.Texte = append(g.Texte, s.text)
		}
		for _, c := range cands {
			if alt, da := alle[c.SourceID]; da {
				// Derselbe Eintrag in einer anderen Sprache: die Titel
				// kommen als Alternativtitel dazu.
				for _, t := range []string{c.Title, c.Original} {
					if t != "" && !strings.EqualFold(t, alt.Title) && !strings.EqualFold(t, alt.Original) && !enthaeltText(alt.Alternativ, t) {
						alt.Alternativ = append(alt.Alternativ, t)
					}
				}
				continue
			}
			c := c
			if s.sprache != "" && s.sprache != r.tmdb.Language {
				// Aus der englischen Suche: Titel in der eigenen Sprache
				// kennen wir (noch) nicht. Der englische zählt als
				// Alternativtitel, angezeigt wird später der aus den
				// Einzelheiten.
				c.Alternativ = append(c.Alternativ, c.Title)
			}
			alle[c.SourceID] = &c
			reihenfolge = append(reihenfolge, c.SourceID)
		}
		return nil
	}
	stand := func() ([]Candidate, Match) {
		cands := make([]Candidate, 0, len(reihenfolge))
		for _, id := range reihenfolge {
			cands = append(cands, *alle[id])
		}
		if kind == "series" {
			cands = nachLand(cands, land)
		}
		return cands, BestVarianten(varianten, jahr, cands)
	}

	var schritte []suchSchritt
	mitJahr := func(text, sprache string) {
		if jahr > 0 {
			schritte = append(schritte, suchSchritt{text, jahr, sprache})
		}
		schritte = append(schritte, suchSchritt{text, 0, sprache})
	}
	// 1. und 2.: alle Schreibweisen in der eigenen Sprache.
	for _, v := range varianten {
		if v.Gleichwertig {
			mitJahr(v.Text, "")
		}
	}
	// 3. Englisch: der Titel selbst und ohne Fassungswörter.
	if englisch != "" {
		for i, v := range varianten {
			if v.Gleichwertig && i < 2 {
				schritte = append(schritte, suchSchritt{v.Text, jahr, englisch})
			}
		}
	}
	// 4. Das Jahr eins daneben.
	if jahr > 0 {
		schritte = append(schritte, suchSchritt{varianten[min(1, len(varianten)-1)].Text, jahr - 1, ""},
			suchSchritt{varianten[min(1, len(varianten)-1)].Text, jahr + 1, ""})
	}
	// 5. Ohne Untertitel.
	for _, v := range varianten {
		if !v.Gleichwertig {
			mitJahr(v.Text, "")
		}
	}

	var m Match
	var cands []Candidate
	for i, s := range schritte {
		if err := frage(s); err != nil {
			return Match{}, g, nil, err
		}
		// Nach jedem Paar (mit/ohne Jahr) nachsehen, ob es schon reicht —
		// die meisten Titel sind nach der ersten oder zweiten Anfrage klar.
		if i+1 < len(schritte) && schritte[i+1].text == s.text && schritte[i+1].sprache == s.sprache && s.jahr != 0 && schritte[i+1].jahr == 0 {
			continue
		}
		cands, m = stand()
		if m.Sure {
			return m, g, cands, nil
		}
		if g.Anfragen >= maxAnfragen {
			break
		}
	}
	cands, m = stand()

	// Nicht sicher, aber es gab Kandidaten: die Alternativtitel der zwei
	// besten nachschlagen. TMDb findet Titel auch über Alternativtitel,
	// nennt sie in der Suche aber nicht — „Les mamies font pas dans la
	// dentelle" findet „Die Herbstzeitlosen", und erst die
	// Alternativtitel zeigen, dass das stimmt.
	if !m.Sure && len(cands) > 0 {
		sortiert := append([]Candidate(nil), cands...)
		sortiereNachScore(sortiert, varianten, jahr)
		for i := 0; i < len(sortiert) && i < 2; i++ {
			alt, err := r.tmdb.Alternativtitel(ctx, kind, sortiert[i].SourceID)
			if err != nil {
				// Nicht schlimm: dann eben ohne.
				continue
			}
			ziel := alle[sortiert[i].SourceID]
			for _, a := range alt {
				if !enthaeltText(ziel.Alternativ, a) {
					ziel.Alternativ = append(ziel.Alternativ, a)
				}
			}
		}
		cands, m = stand()
	}
	return m, g, cands, nil
}

func sortiereNachScore(c []Candidate, v []Variante, jahr int) {
	for i := 1; i < len(c); i++ {
		for j := i; j > 0 && ScoreVarianten(v, jahr, c[j]) > ScoreVarianten(v, jahr, c[j-1]); j-- {
			c[j], c[j-1] = c[j-1], c[j]
		}
	}
}

func enthaeltText(l []string, s string) bool {
	for _, x := range l {
		if strings.EqualFold(x, s) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------- Gründe

// Grund hält fest, warum ein Titel nicht zugeordnet wurde — für die Liste
// in der Verwaltung. Er liegt nur im Arbeitsspeicher: Nach einem Neustart
// prüft der nächste Durchgang ohnehin neu.
type Grund struct {
	// Art: keine_treffer | unsicher | zu_nah | fehler | wartet
	Art       string   `json:"art"`
	Gesucht   []string `json:"gesucht,omitempty"`
	Anfragen  int      `json:"anfragen,omitempty"`
	Vorschlag string   `json:"vorschlag,omitempty"` // bester Kandidat: „Titel (Jahr)"
	Wert      float64  `json:"wert,omitempty"`      // dessen Übereinstimmung 0–1
	Zeit      int64    `json:"zeit"`                // Unix-Sekunden
}

var gruende = struct {
	sync.Mutex
	je map[string]Grund
}{je: map[string]Grund{}}

func grundMerken(itemID string, g Grund) {
	g.Zeit = time.Now().Unix()
	gruende.Lock()
	defer gruende.Unlock()
	// Eine Obergrenze, damit eine riesige Sammlung ohne Netz den Speicher
	// nicht füllt.
	if len(gruende.je) > 20000 {
		gruende.je = map[string]Grund{}
	}
	gruende.je[itemID] = g
}

func grundVergessen(itemID string) {
	gruende.Lock()
	delete(gruende.je, itemID)
	gruende.Unlock()
}

// GrundZu sagt, warum ein Titel nicht zugeordnet ist (ok=false: unbekannt,
// etwa seit dem Neustart nicht mehr geprüft).
func GrundZu(itemID string) (Grund, bool) {
	gruende.Lock()
	defer gruende.Unlock()
	g, ok := gruende.je[itemID]
	return g, ok
}

func grundAus(m Match, g Gesucht) Grund {
	gr := Grund{Gesucht: g.Texte, Anfragen: g.Anfragen}
	switch {
	case m.Candidate == nil:
		gr.Art = "keine_treffer"
	case m.Why == "zwei Treffer liegen zu nah beieinander":
		gr.Art = "zu_nah"
	default:
		gr.Art = "unsicher"
	}
	if m.Candidate != nil {
		gr.Vorschlag = m.Candidate.Title
		if m.Candidate.Year > 0 {
			gr.Vorschlag += " (" + strconv.Itoa(m.Candidate.Year) + ")"
		}
		gr.Wert = float64(int(m.Score*100)) / 100
	}
	return gr
}
