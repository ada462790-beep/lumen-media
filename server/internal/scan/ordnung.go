// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package scan

// Entscheidungen, die ein einzelner Dateiname nicht hergibt, wohl aber
// der Ordner drumherum: welche Folgen zu derselben Serie gehören, ob
// „Film - Teil 2" ein Dateiteil ist, welche Datei einer BluRay der Film
// ist.

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"lumen/internal/store"
)

// serienSchluessel gruppiert Folgen zu Serien. Der Titel allein reichte
// nicht: „Battlestar Galactica (1978)" und „(2003)", „The Office (US)"
// und „(UK)" wurden eine Serie. Jahr und Land gehören dazu.
func serienSchluessel(g Guess) string {
	return strings.ToLower(g.Title) + "\x00" + g.Land + "\x00" + strconv.Itoa(g.Year)
}

// gruppenZusammenlegen: Folgen ohne Jahr (oder ohne Land) gehören zu der
// gleichnamigen Gruppe mit Jahr (oder Land), wenn es genau eine gibt —
// „Fallout (2024)/Staffel 1/…" und ein Release-Ordner „Fallout.S02E01…"
// sind dieselbe Serie. Gibt es zwei Jahrgänge, bleibt die Gruppe ohne
// Jahr für sich: raten wäre schlimmer.
func gruppenZusammenlegen(ep map[string][]episodeHit) {
	type basis struct{ titel, land string }
	// Erst das Jahr, dann das Land.
	for _, nachLand := range []bool{false, true} {
		kandidaten := map[basis][]string{}
		for k, hits := range ep {
			g := hits[0].guess
			b := basis{strings.ToLower(g.Title), g.Land}
			genau := g.Year > 0
			if nachLand {
				b = basis{strings.ToLower(g.Title), strconv.Itoa(g.Year)}
				genau = g.Land != ""
			}
			if genau {
				kandidaten[b] = append(kandidaten[b], k)
			}
		}
		for k, hits := range ep {
			g := hits[0].guess
			b := basis{strings.ToLower(g.Title), g.Land}
			offen := g.Year == 0
			if nachLand {
				b = basis{strings.ToLower(g.Title), strconv.Itoa(g.Year)}
				offen = g.Land == ""
			}
			if !offen {
				continue
			}
			if ks := kandidaten[b]; len(ks) == 1 && ks[0] != k {
				ziel := ep[ks[0]][0].guess
				for i := range hits {
					hits[i].guess.Year, hits[i].guess.Land = ziel.Year, ziel.Land
				}
				ep[ks[0]] = append(ep[ks[0]], hits...)
				delete(ep, k)
			}
		}
	}
}

// passtZuSerie: Darf eine bestehende Serie diese Folgen aufnehmen? Nicht,
// wenn ihr Jahr mehr als eins daneben liegt (TMDb nennt manchmal das Jahr
// der Ausstrahlung im Ursprungsland, der Ordner das hiesige) oder ihr
// Land ein anderes ist.
func (s *Scanner) passtZuSerie(ctx context.Context, it *store.Item, jahr int, land string) bool {
	if jahr > 0 && it.Year > 0 && math.Abs(float64(it.Year-jahr)) > 1 {
		return false
	}
	if land != "" {
		if l := s.st.ZusatzZu(ctx, it.ID).Land; l != "" && l != land {
			return false
		}
	}
	return true
}

// festerTitel: Der Name dieses Titels kommt nicht (mehr) aus dem
// Dateinamen — von TMDb, von Hand oder aus einer NFO. Dann benennt der
// Scanner ihn nicht nach dem Dateinamen um.
func (s *Scanner) festerTitel(ctx context.Context, it *store.Item) bool {
	if store.IstErkannt(it.MatchState) {
		return true
	}
	_, ok := s.st.LokaleNFOZu(ctx, it.ID)
	return ok
}

// ---------------------------------------------------------------- Teile

// teilOrdner zählt je Ordner, welche „Film - Teil N"-Dateien es gibt.
type teilOrdner map[string]map[string]int // Ordner → Basis → Zahl der Teile

// teileKlaeren entscheidet über „Film - Teil 2": Liegen im selben Ordner
// mindestens zwei Dateien mit derselben Basis („Film - Teil 1", „Film -
// Teil 2"), sind es Teile eines Films. Sonst bleibt „Teil 2" im Titel —
// „Zurück in die Zukunft Teil 2" ist ein eigener Film.
func teileKlaeren(g Guess, path string, dirs []string, art string, cache teilOrdner) Guess {
	if g.Kind != "movie" || g.TeilKandidat == 0 || g.TitelOhneTeil == "" {
		return g
	}
	dir := filepath.Dir(path)
	zahl, ok := cache[dir]
	if !ok {
		zahl = map[string]int{}
		if eintraege, err := os.ReadDir(dir); err == nil {
			for _, e := range eintraege {
				if !e.Type().IsRegular() || !IsVideo(e.Name()) || IsJunkFile(e.Name()) {
					continue
				}
				x := ParseFuer(filepath.Join(dir, e.Name()), dirs, art)
				if x.Kind == "movie" && x.TeilKandidat > 0 && x.TitelOhneTeil != "" {
					zahl[teilBasis(x)]++
				}
			}
		}
		if cache != nil {
			cache[dir] = zahl
		}
	}
	if zahl[teilBasis(g)] >= 2 {
		g.Title, g.PartNo = g.TitelOhneTeil, g.TeilKandidat
		g.Confident = g.Title != "" && (g.Year > 0 || len(strings.Fields(g.Title)) >= 2)
	}
	return g
}

func teilBasis(g Guess) string {
	return strings.ToLower(g.TitelOhneTeil) + "\x00" + strconv.Itoa(g.Year)
}

// ---------------------------------------------------------------- BluRay und DVD

// discWahl merkt sich je Ordner, welche Datei einer BluRay- oder
// DVD-Struktur der Film ist.
type discWahl map[string]map[string]bool // Ordner → Dateiname → nehmen

// nimmt sagt, ob eine Datei unter BDMV oder VIDEO_TS eingelesen wird:
//
//   - „BDMV/STREAM/*.m2ts": nur die grösste — der Hauptfilm. Die übrigen
//     sind Menüs, Trailer, Extras; jede als eigener Film wäre Unsinn.
//   - „VIDEO_TS/VTS_NN_K.VOB": nur der Titelsatz NN mit den meisten
//     Bytes, ohne den Menüteil K=0. Die Teile K=1,2,… sind Teile eines
//     Films (je höchstens 1 GB) und lassen sich in der Verwaltung zu einer
//     Datei zusammenführen. „VIDEO_TS.VOB" ist das Hauptmenü.
//
// FFmpeg liest beides direkt. ISO-Abbilder dagegen nicht — die bleiben
// draussen (siehe VideoExt).
func (w discWahl) nimmt(path string, dirs []string) bool {
	k := discOrdner(dirs)
	if k < 0 {
		// Ein VOB ausserhalb von VIDEO_TS ist ein Bruchstück, kein Film.
		return !strings.EqualFold(filepath.Ext(path), ".vob")
	}
	dir := filepath.Dir(path)
	wahl, ok := w[dir]
	if !ok {
		wahl = discAuswahl(dir, strings.EqualFold(dirs[k], "VIDEO_TS"))
		w[dir] = wahl
	}
	return wahl[filepath.Base(path)]
}

func discAuswahl(dir string, dvd bool) map[string]bool {
	out := map[string]bool{}
	eintraege, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	if !dvd {
		groesste, name := int64(-1), ""
		for _, e := range eintraege {
			if !e.Type().IsRegular() || !strings.EqualFold(filepath.Ext(e.Name()), ".m2ts") {
				continue
			}
			if info, err := e.Info(); err == nil && (info.Size() > groesste ||
				(info.Size() == groesste && e.Name() < name)) {
				groesste, name = info.Size(), e.Name()
			}
		}
		if name != "" {
			out[name] = true
		}
		return out
	}
	saetze := map[string]int64{}
	namen := map[string][]string{}
	for _, e := range eintraege {
		if !e.Type().IsRegular() {
			continue
		}
		stamm := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
		m := reVTS.FindStringSubmatch(stamm)
		if m == nil || m[2] == "0" || !strings.EqualFold(filepath.Ext(e.Name()), ".vob") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		saetze[m[1]] += info.Size()
		namen[m[1]] = append(namen[m[1]], e.Name())
	}
	bester := ""
	for satz, groesse := range saetze {
		if bester == "" || groesse > saetze[bester] || (groesse == saetze[bester] && satz < bester) {
			bester = satz
		}
	}
	for _, n := range namen[bester] {
		out[n] = true
	}
	return out
}

// ---------------------------------------------------------------- Schreibweise

// kleineWoerter bleiben klein, ausser am Anfang.
var kleineWoerter = map[string]bool{
	"a": true, "an": true, "and": true, "of": true, "the": true, "in": true, "on": true,
	"at": true, "to": true, "for": true, "or": true, "vs": true,
	"der": true, "die": true, "das": true, "des": true, "dem": true, "den": true,
	"und": true, "im": true, "in der": true, "von": true, "vom": true, "zum": true,
	"zur": true, "mit": true, "auf": true, "aus": true, "am": true, "ein": true, "eine": true,
}

// huebsch: „pulp fiction" aus „pulp_fiction_1994_1080p.mkv" wird „Pulp
// Fiction". Nur, wenn der Name ganz klein geschrieben ist — wer
// „iCarly" oder „eXistenZ" schreibt, meint es so. Das ist nur der Name,
// bis die Metadaten den richtigen bringen.
func huebsch(t string) string {
	hatBuchstaben := false
	for _, r := range t {
		if unicode.IsUpper(r) {
			return t
		}
		if unicode.IsLetter(r) {
			hatBuchstaben = true
		}
	}
	if !hatBuchstaben {
		return t
	}
	woerter := strings.Split(t, " ")
	for i, w := range woerter {
		if w == "" || (i > 0 && kleineWoerter[w]) {
			continue
		}
		r := []rune(w)
		r[0] = unicode.ToUpper(r[0])
		woerter[i] = string(r)
	}
	return strings.Join(woerter, " ")
}
