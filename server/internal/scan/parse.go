// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

// Package scan liest Ordner ein und deutet Dateinamen.
//
// Daran wird ein Medienserver im Alltag gemessen: eine Sammlung ist über
// Jahre gewachsen, und kaum zwei Dateien heissen gleich. Was hier
// herauskommt, ist eine Vermutung — die Metadaten-Quellen bestätigen sie
// später. Lieber ehrlich „nicht erkannt" melden als still etwas Falsches
// in die Mediathek stellen.
package scan

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Guess ist, was aus einem Dateinamen herauszulesen war.
type Guess struct {
	Kind      string // movie | episode
	Title     string
	Year      int
	SeasonNo  int
	EpisodeNo int
	EpisodeTo int // bei Doppelfolgen: S01E05E06
	// Der Name der Folge aus dem Dateinamen: „Serie - S01E01 - Pilot" →
	// „Pilot". Ohne Metadaten-Quelle ist das alles, was es gibt — und
	// „Folge 1" hilft beim Suchen nicht.
	EpisodeTitle string
	Edition      string // Director's Cut, Extended …
	PartNo       int    // CD1 / part2
	Quality      string // 2160p, 1080p …
	HDR          string // HDR, DV, HDR10+
	Language     string
	Confident    bool // false → als „nicht erkannt" melden
	// NurNummer: Der Dateiname ist nur eine Nummer („1.mkv", „Teil 2.avi").
	// Der Titel kommt dann aus dem Ordner. Ob das Folgen einer Miniserie
	// sind oder Teile eines Films, entscheidet der Scanner nach der Zahl
	// der Dateien im Ordner.
	NurNummer bool
	// Land trennt gleichnamige Serien: „The Office (US)" und „The Office
	// (UK)" sind zwei Serien. Als Ländercode, wie TMDb ihn bei
	// origin_country führt („US", „GB").
	Land string
	// Absolut: Die Folgennummer ist über alle Staffeln durchgezählt
	// („One Piece - 1071"), wie bei Anime üblich. Auf Staffel und Folge
	// abgebildet wird erst bei der Zuordnung — nur die Quelle weiss, wie
	// viele Folgen jede Staffel hat.
	Absolut bool
	// Datum: Sendedatum (JJJJ-MM-TT) bei Sendungen, die nach dem Tag
	// benannt sind („Tagesschau 2024-03-15"). Die Staffel ist dann das
	// Jahr, die Folge Monat und Tag (315) — so sortiert es sich richtig.
	Datum string
	// TeilKandidat: „Film - Teil 2" ohne Jahr dahinter. Allein ist das ein
	// Titel („Kill Bill Vol 2"); liegen daneben Dateien mit derselben Basis
	// und anderen Teilnummern, sind es Teile eines Films. Das entscheidet
	// der Scanner, der den Ordner sieht. TitelOhneTeil ist dann der Titel.
	TeilKandidat  int
	TitelOhneTeil string
	// TeilNr: eine „Teil 2"/„Part 2"-Angabe überhaupt — in einer
	// Serienbibliothek ist das die Folge einer Miniserie.
	TeilNr int
}

var (
	// S01E02, 1x02, S01.E02, s01_e02 — und Doppelfolgen in allen
	// Schreibweisen: S01E01E02, S01E01-E02, S01E01-02, S01E01-S01E02.
	//
	// Die Grenzen sind von Hand gebaut statt mit \b: Für \b ist der
	// Unterstrich ein Buchstabe, und „HIMYM_S02E01" fand keine Folge.
	reSeasonEp = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])(?:s|season[\s._-]*|staffel[\s._-]*)(\d{1,2})[\s._-]*(?:e|ep|episode|folge|x)[\s._-]*(\d{1,3})(?:[\s._-]*(?:e|-)[\s._-]*(?:s\d{1,2}[\s._-]*e)?(\d{1,3}))?(?:[^a-z0-9]|$)`)
	// Kreuzform ohne Buchstaben davor: 2x07, als Doppelfolge 1x16-1x17
	// oder 1x01+1x02.
	reCrossEp = regexp.MustCompile(`(?i)(?:^|[\s._-])(\d{1,2})x(\d{1,3})(?:[-+_](?:\d{1,2}x)?(\d{1,3}))?\b`)
	// „Folge 5" ohne Staffelordner: Emby legt so etwas im Serienordner als
	// Staffel 1 ab. Nur „Folge" — „Episode 4" steht auch in Filmtiteln.
	reFolgeAllein = regexp.MustCompile(`(?i)(?:^|[\s._-])folge[\s._-]*(\d{1,3})\b`)
	// „101" als Staffel 1 Folge 01 — nur in Ordnern, die nach Staffel aussehen
	reBareEp = regexp.MustCompile(`(?i)(?:^|[\s._-])(\d)(\d{2})(?:[\s._-]|$)`)
	// „1001" als Staffel 10 Folge 01 — dasselbe für zweistellige Staffeln
	// („Die Simpsons/Staffel 10/Die Simpsons - 1001 - Lisa.mkv").
	reBareEp2 = regexp.MustCompile(`(?i)(?:^|[\s._-])(\d{2})(\d{2})(?:[\s._-]|$)`)
	// „[SubsPlease] One Piece - 1071 (1080p)", „Frieren - 05", „- 12v2":
	// durchgezählte Folgen, wie Fansub-Gruppen und Anime-Sammlungen sie
	// benennen. Das „v2" ist eine korrigierte Fassung derselben Folge.
	reAbsolut = regexp.MustCompile(`(?i)[\s_]-[\s_]+(\d{1,4})(?:v\d{1,2})?(?:$|[\s_.]*[\[\(]|[\s_]+-[\s_]+|[\s_]+)`)
	// Ein Gruppen-Tag vorne: „[SubsPlease] …"
	reGruppeVorne = regexp.MustCompile(`^\s*\[[^\]]+\][\s._]*`)
	// Sendedatum: „2024.03.15", „2024-03-15" — und deutsch „15.03.2024".
	reDatum   = regexp.MustCompile(`(?:^|[\s._-])((?:19|20)\d{2})[\s._-](0[1-9]|1[0-2])[\s._-](0[1-9]|[12]\d|3[01])(?:[\s._-]|$)`)
	reDatumDE = regexp.MustCompile(`(?:^|[\s._-])(0[1-9]|[12]\d|3[01])\.(0[1-9]|1[0-2])\.((?:19|20)\d{2})(?:[\s._-]|$)`)
	// Ein Ländercode am Serientitel: „The Office (US)", „The.Office.US".
	reLandKlammer = regexp.MustCompile(`(?i)[\s._-]*[\(\[](US|UK|GB|AU|NZ|CA|IE|DE|FR|SE|DK|NO|ES|IT|NL|JP|KR)[\)\]]`)
	reLandEnde    = regexp.MustCompile(`[\s._-](US|UK|GB|AU|NZ|CA|IE)[\s._-]*$`)
	// Nur eine Folgennummer, wenn der Ordner die Staffel schon sagt
	reEpOnly = regexp.MustCompile(`(?i)(?:^|[\s._-])(?:e|ep|episode|folge)[\s._-]*(\d{1,3})\b`)
	// „Stafel" mit einem f kommt in echten Sammlungen vor, „Saison" in
	// französischen Releases. Beides wurde sonst selbst zur Serie.
	//
	// „Series 1" sagen die Briten, „Book 1"/„Buch 1" heissen die Staffeln
	// von „Avatar", „Temporada", „Seizoen", „Stagione" kommen aus spanischen,
	// niederländischen und italienischen Sammlungen. „Teil 1" als Ordner
	// gilt nur in Serienbibliotheken (siehe staffelOrdner) — in einer
	// Filmbibliothek ist das eher ein Film einer Reihe.
	reSeasonDir  = regexp.MustCompile(`(?i)^(?:s|season|saison|staffel|stafel|series|book|buch|temporada|seizoen|stagione)[\s._-]*(\d{1,2})$`)
	reTeilOrdner = regexp.MustCompile(`(?i)^teil[\s._-]*(\d{1,2})$`)
	// Ein Staffelordner mit Namen davor oder Technik dahinter:
	// „Greys Anatomy - Staffel 06", „Star Trek - Raumschiff Voyager -
	// Staffel 01", „The.Blacklist.S01.German.1080p". Vorher galt nur der
	// nackte „Staffel 1" als Staffel — alles andere wurde selbst zur
	// Serie, und aus einer Serie wurden sieben.
	reStaffelMitName = regexp.MustCompile(`(?i)^(.*?)(?:^|[\s._-])(?:season|saison|staffel|stafel|temporada|seizoen|stagione|s)[\s._-]*(\d{1,2})(?:[\s._-]+(.*))?$`)
	// „Doctor Who - Specials": Staffel 0 mit dem Seriennamen davor — wie
	// „Greys Anatomy - Staffel 06", nur ohne Nummer.
	reSpecialsMitName = regexp.MustCompile(`(?i)^(.+?)[\s._-]+(?:specials?|sonderfolgen)$`)
	// „(1982) Star Trek II - Der Zorn des Khan" — das Jahr steht vorne.
	reJahrVorne = regexp.MustCompile(`^\s*[\(\[]((?:19|20)\d{2})[\)\]][\s._-]*(.+)$`)
	// „The Office 2024" — ein Jahr am Ende eines Serientitels.
	reJahrAmEnde = regexp.MustCompile(`^(.+?)[\s._-]+((?:19|20)\d{2})$`)
	// „S.H.I.E.L.D.", „P.I.", „S.W.A.T." — Abkürzungen mit Punkten. Die
	// Punkte gehören zum Titel; sonst wird aus „Agents of S.H.I.E.L.D." ein
	// „Agents of S H I E L D", das keine Quelle findet.
	reAbkuerzung     = regexp.MustCompile(`\b(?:[A-Za-z]\.){2,}`)
	reAbkuerzungEnde = regexp.MustCompile(`\b(?:[A-Za-z]\.)+[A-Za-z]$`)
	// In einem Staffelordner: eine Nummer am Anfang („01 - Pilot", „1. Pilot")
	// oder frei stehend („Serie - 01 - Pilot") ist die Folgennummer. Das
	// ist eine der häufigsten Ablagen überhaupt — und bisher wurden diese
	// Dateien zu Filmen namens „01: Pilot".
	reEpLead = regexp.MustCompile(`^(\d{1,3})(?:[\s._-]+|$)`)
	reEpLose = regexp.MustCompile(`(?:^|[\s._-])(\d{1,3})(?:[\s._-]+|$)`)
	// Ein Ordner, der „Specials" heisst, ist Staffel 0.
	reSpecialsDir = regexp.MustCompile(`(?i)^(?:specials?|extras?|sonderfolgen)$`)
	// Jahr in Klammern wiegt schwerer als ein Jahr irgendwo im Namen
	reYearParen = regexp.MustCompile(`[\(\[](19\d{2}|20\d{2})[\)\]]`)
	// „Apocalypse Now 1979a" — Jahr und dahinter ein Buchstabe als Teil.
	reJahrTeil        = regexp.MustCompile(`^(.*?)[\s._-]*((?:19|20)\d{2})([a-h])$`)
	reJahrKlammerTeil = regexp.MustCompile(`^(.*[\(\[](?:19|20)\d{2}[\)\]])[\s._-]*([a-hA-H])$`)
	// „beforemidnight2013" — das Jahr klebt am Namen.
	reJahrAngeklebt = regexp.MustCompile(`^(.*[A-Za-zäöüÄÖÜ])((?:19|20)\d{2})$`)
	// „1", „01", „Teil 2", „Part 3", „Folge 4", „Episode 5", „Ep. 6"
	reNurNummer = regexp.MustCompile(`(?i)^(?:(?:teil|part|folge|episode|ep\.?)[\s._-]*)?(\d{1,3})$`)
	reYearLoose = regexp.MustCompile(`(?:^|[\s._-])(19\d{2}|20\d{2})(?:[\s._-]|$)`)
	// CD1, Disc 2: immer ein Dateiteil.
	rePart = regexp.MustCompile(`(?i)(?:^|[\s._-])(?:cd|disc|disk)[\s._-]*(\d)\b`)
	// „Teil 2", „Part 2", „pt2": mal Dateiteil, mal Teil des Titels
	// („Zurück in die Zukunft Teil 2 (1989)", „Dune Part 2 (2024)").
	// Was es ist, entscheidet teilLesen am Rest des Namens.
	reTeilWort = regexp.MustCompile(`(?i)(?:^|[\s._-])(part|teil|pt)([\s._-]*)(\d{1,2})\b`)
	// „Film - Teil 2" mit Bindestrich davor — die Schreibweise der Dateiteile.
	reTeilMitStrich = regexp.MustCompile(`(?i)^(.*?)[\s._]+-[\s._]+(?:part|teil|pt)[\s._-]*(\d{1,2})[\s._-]*$`)
	// VIDEO_TS: „VTS_01_2.VOB" ist der zweite Teil des ersten Titelsatzes.
	reVTS      = regexp.MustCompile(`(?i)^vts_(\d{2})_(\d{1,2})$`)
	reEdition  = regexp.MustCompile(`(?i)\b(director'?s[\s._-]?cut|extended(?:[\s._-]cut)?|unrated|theatrical|remastered|uncut|final[\s._-]cut|special[\s._-]edition)\b`)
	reQuality  = regexp.MustCompile(`(?i)\b(2160p|1080p|720p|480p|4k|uhd)\b`)
	reHDR      = regexp.MustCompile(`(?i)\b(hdr10\+|hdr10|hdr|dolby[\s._-]?vision|dv)\b`)
	reLang     = regexp.MustCompile(`(?i)\b(german|deutsch|english|multi|dl|ger|eng)\b`)
	reBrackets = regexp.MustCompile(`[\(\[\{][^\)\]\}]*[\)\]\}]`)
	reSpace    = regexp.MustCompile(`\s+`)
)

// junk sind die Wortbrocken, die Release-Gruppen anhängen und die nie Teil
// eines Titels sind. Alles ab dem ersten Treffer fliegt weg.
var junk = []string{
	"bluray", "blu-ray", "bdrip", "brrip", "dvdrip", "dvdscr", "webrip", "web-dl",
	"webdl", "hdtv", "hdrip", "remux", "x264", "x265", "h264", "h265", "hevc",
	"avc", "xvid", "divx", "aac", "ac3", "eac3", "dts", "dtshd", "truehd",
	"atmos", "ddp", "dd5", "flac", "10bit", "8bit", "repack", "proper",
	"internal", "limited", "complete", "imax", "hybrid", "sdr",
}

// Parse deutet einen Dateinamen. dirs sind die Ordner darüber, von aussen
// nach innen — sie helfen bei Serien enorm weiter. Die Art der Bibliothek
// kennt Parse hier nicht; der Scanner nimmt ParseFuer.
func Parse(path string, dirs []string) Guess { return ParseFuer(path, dirs, "") }

// ParseFuer deutet einen Dateinamen für eine Bibliothek der Art art
// („movie", „series" oder "" für unbekannt).
//
// Die Art ist kein Befehl, sondern ein Hinweis für die Fälle, die sich am
// Namen allein nicht entscheiden lassen: „Chernobyl.Part.1" ist in einer
// Serienbibliothek die erste Folge einer Miniserie, in einer
// Filmbibliothek ein Film, der so heisst. „S01E02" bleibt überall eine
// Folge.
func ParseFuer(path string, dirs []string, art string) Guess {
	base := filepath.Base(path)
	name := strings.TrimSuffix(base, filepath.Ext(base))

	g := Guess{Kind: "movie", PartNo: 1}

	// BluRay- und DVD-Ordner: „Matrix (1999)/BDMV/STREAM/00000.m2ts" und
	// „Matrix (1999)/VIDEO_TS/VTS_01_1.VOB". Der Film heisst wie der Ordner
	// über BDMV bzw. VIDEO_TS; welche Datei der Film ist (die grösste, der
	// grösste Titelsatz), wählt der Scanner.
	if k := discOrdner(dirs); k >= 0 {
		if m := reVTS.FindStringSubmatch(name); m != nil {
			g.PartNo, _ = strconv.Atoi(m[2])
		}
		dirs = dirs[:k]
		name = "movie"
	}

	// Technische Angaben zuerst einsammeln, solange der Name noch vollständig ist.
	if m := reQuality.FindStringSubmatch(name); m != nil {
		g.Quality = strings.ToLower(m[1])
		if g.Quality == "4k" || g.Quality == "uhd" {
			g.Quality = "2160p"
		}
	}
	if m := reHDR.FindStringSubmatch(name); m != nil {
		switch strings.ToLower(strings.NewReplacer(".", "", "_", "", "-", "", " ", "").Replace(m[1])) {
		case "dolbyvision", "dv":
			g.HDR = "DV"
		case "hdr10+":
			g.HDR = "HDR10+"
		default:
			g.HDR = "HDR"
		}
	}
	if m := reLang.FindStringSubmatch(name); m != nil {
		switch strings.ToLower(m[1]) {
		case "german", "deutsch", "ger":
			g.Language = "de"
		case "english", "eng":
			g.Language = "en"
		case "multi", "dl":
			g.Language = "multi"
		}
	}
	if m := reEdition.FindStringSubmatch(name); m != nil {
		g.Edition = tidyEdition(m[1])
	}
	if m := rePart.FindStringSubmatch(name); m != nil {
		g.PartNo, _ = strconv.Atoi(m[1])
	}
	if m := reTeilWort.FindStringSubmatch(name); m != nil {
		g.TeilNr, _ = strconv.Atoi(m[3])
	}

	// Der Serientitel samt Land: Jede Stelle, die unten einen Serientitel
	// bestimmt, geht hier durch, damit das Land nicht verloren geht.
	land := ""
	serie := func(name, marker string) string {
		t, l := serieMitLand(name, marker, dirs, art)
		land = l
		return t
	}

	// Serie oder Film?
	seasonFromDir, inSeasonDir := 0, false
	for i := len(dirs) - 1; i >= 0; i-- {
		d := strings.TrimSpace(dirs[i])
		if n, _, ok := staffelOrdner(d, art); ok {
			seasonFromDir = n
			inSeasonDir = true
			break
		}
		if reSpecialsDir.MatchString(d) {
			inSeasonDir = true
			break
		}
	}

	epMatch := reSeasonEp.FindStringSubmatch(name)
	if epMatch != nil {
		epMatch[0] = ohneRandVorne(epMatch[0])
	}
	if epMatch == nil {
		if m := reCrossEp.FindStringSubmatch(name); m != nil {
			epMatch = []string{strings.TrimLeft(m[0], " ._-"), m[1], m[2], m[3]}
		}
	}
	datum, datumMarker := "", ""
	if epMatch == nil && (art == "series" || inSeasonDir) {
		datum, datumMarker = datumLesen(name)
	}
	if m := epMatch; m != nil {
		g.Kind = "episode"
		g.SeasonNo, _ = strconv.Atoi(m[1])
		g.EpisodeNo, _ = strconv.Atoi(m[2])
		if m[3] != "" {
			g.EpisodeTo, _ = strconv.Atoi(m[3])
			// „S01E05 - 03 Uhr nachts" ist keine Doppelfolge: das Ende muss
			// hinter dem Anfang liegen.
			if g.EpisodeTo <= g.EpisodeNo {
				g.EpisodeTo = 0
			}
		}
		g.Title = serie(name, m[0])
		g.EpisodeTitle = episodeTitle(name, m[0])
		if g.EpisodeTitle == "" {
			g.EpisodeTitle = folgentitelAusOrdner(dirs, g.SeasonNo, g.EpisodeNo)
		}
		g.Confident = g.Title != ""
	} else if datum != "" {
		// „Tagesschau 2024-03-15": Staffel ist das Jahr, Folge Monat und
		// Tag. Welche Folge das bei der Quelle ist, sagt das Sendedatum.
		j, _ := strconv.Atoi(datum[:4])
		mo, _ := strconv.Atoi(datum[5:7])
		ta, _ := strconv.Atoi(datum[8:10])
		g.Kind, g.Datum = "episode", datum
		g.SeasonNo, g.EpisodeNo = j, mo*100+ta
		g.Title = serie(name, datumMarker)
		g.EpisodeTitle = episodeTitle(name, datumMarker)
		if g.EpisodeTitle == "" {
			g.EpisodeTitle = fmt.Sprintf("%02d.%02d.%d", ta, mo, j)
		}
		g.Confident = g.Title != ""
	} else if inSeasonDir {
		// Der Ordner sagt die Staffel, der Dateiname nur die Folge.
		if m := reEpOnly.FindStringSubmatch(name); m != nil {
			g.Kind = "episode"
			g.SeasonNo = seasonFromDir
			g.EpisodeNo, _ = strconv.Atoi(m[1])
			g.Title = serie(name, m[0])
			g.EpisodeTitle = episodeTitle(name, m[0])
			g.Confident = g.Title != ""
		} else if m := reBareEp.FindStringSubmatch(name); m != nil &&
			func() bool { s, _ := strconv.Atoi(m[1]); return s == seasonFromDir }() {
			e, _ := strconv.Atoi(m[2])
			g.Kind = "episode"
			g.SeasonNo, g.EpisodeNo = seasonFromDir, e
			g.Title = serie(name, m[0])
			g.Confident = g.Title != ""
		} else if m := reBareEp2.FindStringSubmatch(name); m != nil && seasonFromDir >= 10 &&
			func() bool { s, _ := strconv.Atoi(m[1]); return s == seasonFromDir }() {
			// „Die Simpsons - 1001 - Lisa" in „Staffel 10".
			e, _ := strconv.Atoi(m[2])
			g.Kind = "episode"
			g.SeasonNo, g.EpisodeNo = seasonFromDir, e
			g.Title = serie(name, m[0])
			g.EpisodeTitle = episodeTitle(name, ohneRandVorne(m[0]))
			g.Confident = g.Title != ""
		} else if m := reEpLead.FindStringSubmatch(name); m != nil {
			// „01 - Pilot.mkv"
			g.Kind = "episode"
			g.SeasonNo = seasonFromDir
			g.EpisodeNo, _ = strconv.Atoi(m[1])
			g.Title = serie("", "")
			g.EpisodeTitle = clean(name[len(m[0]):])
			g.Confident = g.Title != ""
		} else if m := reEpLose.FindStringSubmatch(name); m != nil && !istJahr(m[1]) {
			// „Serie - 01 - Pilot.mkv"
			g.Kind = "episode"
			g.SeasonNo = seasonFromDir
			g.EpisodeNo, _ = strconv.Atoi(m[1])
			g.Title = serie(name, m[0])
			g.EpisodeTitle = episodeTitle(name, m[0])
			g.Confident = g.Title != ""
		} else {
			// Gar keine Nummer, aber im Staffelordner: eine Folge, deren
			// Nummer der Scanner nach der Reihenfolge der Dateinamen
			// vergibt. Als Film wäre sie falscher.
			g.Kind = "episode"
			g.SeasonNo = seasonFromDir
			g.Title = serie("", "")
			g.EpisodeTitle = clean(name)
			g.Confident = false
		}
	}

	// „Serie/Serie - Folge 05.avi": keine Staffel, aber eindeutig eine
	// Folge. Ein Name, der nur „Folge 5" ist, bleibt dem Fall unten
	// überlassen — dort entscheidet der Scanner zwischen Miniserie und
	// Film in Teilen. Mit Jahr in Klammern ist es eher ein Film.
	if g.Kind == "movie" && len(dirs) > 0 && !reNurNummer.MatchString(strings.TrimSpace(name)) &&
		reYearParen.FindString(name) == "" {
		if m := reFolgeAllein.FindStringSubmatch(name); m != nil {
			if t := serie(name, m[0]); t != "" {
				g.Kind, g.SeasonNo = "episode", 1
				g.EpisodeNo, _ = strconv.Atoi(m[1])
				g.Title = t
				g.EpisodeTitle = episodeTitle(name, m[0])
				g.Confident = true
			}
		}
	}

	// „[SubsPlease] One Piece - 1071 (1080p)", „Frieren - 05": eine
	// durchgezählte Folge. In einer Serienbibliothek genügt „Titel - 05"
	// ausserhalb eines Staffelordners; anderswo nur mit dem Gruppen-Tag
	// vorne, an dem man Fansub-Dateien sicher erkennt — „Terminator 2 -
	// Tag der Abrechnung" soll ein Film bleiben.
	if g.Kind == "movie" && !inSeasonDir && reYearParen.FindString(name) == "" &&
		!reNurNummer.MatchString(strings.TrimSpace(name)) &&
		(art == "series" || reGruppeVorne.MatchString(name)) {
		if vorne, nr, ok := absolutLesen(name); ok {
			t := serie("", "")
			if t == "" {
				t, land = landUndTitel(vorne)
			}
			if t != "" {
				g.Kind, g.SeasonNo, g.EpisodeNo, g.Absolut = "episode", 1, nr, true
				g.Title, g.Confident = t, true
			}
		}
	}

	// Nur in Serienbibliotheken: „Spartacus Gods of the Arena/…E01.mkv"
	// ohne Staffelordner ist Staffel 1 (so hält es Emby auch), und
	// „Chernobyl.Part.1", „Planet Erde II - Teil 1" sind Folgen einer
	// Miniserie. In einer Filmbibliothek stehen „Episode 4" und „Teil 2"
	// eher im Titel eines Films.
	if g.Kind == "movie" && art == "series" && !inSeasonDir &&
		!reNurNummer.MatchString(strings.TrimSpace(name)) {
		if m := reEpOnly.FindStringSubmatch(name); m != nil {
			if t := serie(name, ohneRandVorne(m[0])); t != "" {
				g.Kind, g.SeasonNo = "episode", 1
				g.EpisodeNo, _ = strconv.Atoi(m[1])
				g.Title, g.EpisodeTitle = t, episodeTitle(name, ohneRandVorne(m[0]))
				g.Confident = true
			}
		} else if m := reTeilWort.FindStringSubmatch(name); m != nil {
			if t := serie(name, ohneRandVorne(m[0])); t != "" {
				g.Kind, g.SeasonNo = "episode", 1
				g.EpisodeNo, _ = strconv.Atoi(m[3])
				g.Title, g.EpisodeTitle = t, episodeTitle(name, ohneRandVorne(m[0]))
				g.PartNo = 1
				g.Confident = true
			}
		}
	}

	// „The Office 2024": Das Jahr gehört nicht in den Titel, sondern ist
	// das Jahr der Serie — und hilft, sie von der älteren zu trennen.
	if g.Kind == "episode" {
		if t, j := ohneJahrAmEnde(g.Title); j > 0 {
			g.Title, g.Year = t, j
		}
		g.Land = land
	}
	// Das Jahr der Serie steht am Serienordner: „Serie (2008)/Staffel 1/…".
	if g.Kind == "episode" && g.Year == 0 {
		for i := len(dirs) - 1; i >= 0; i-- {
			if m := reYearParen.FindStringSubmatch(dirs[i]); m != nil {
				g.Year, _ = strconv.Atoi(m[1])
				break
			}
		}
	}

	// Nur eine Nummer als Name, im Ordner eines Titels: eine Folge einer
	// Miniserie oder ein Teil eines Films — der Name steht am Ordner.
	if g.Kind == "movie" && len(dirs) > 0 {
		if m := reNurNummer.FindStringSubmatch(strings.TrimSpace(name)); m != nil {
			d := dirs[len(dirs)-1]
			titel, jahr := clean(d), 0
			if ym := reYearParen.FindStringSubmatchIndex(d); ym != nil {
				jahr, _ = strconv.Atoi(d[ym[2]:ym[3]])
				titel = clean(d[:ym[0]])
			}
			if titel != "" && !isGenericName(titel) {
				n, _ := strconv.Atoi(m[1])
				g.Kind, g.SeasonNo, g.EpisodeNo = "episode", 1, n
				g.Title, g.Year, g.NurNummer = titel, jahr, true
				// „Teil 3" war oben als Dateiteil gelesen worden. Als Folge
				// ist es die Folgennummer; wird daraus doch ein Film in
				// Teilen, setzt der Scanner den Teil aus der Folge.
				g.PartNo = 1
				g.Confident = true
				return g
			}
		}
	}

	if g.Kind == "movie" {
		// „(1982) Star Trek II - Der Zorn des Khan.avi": Das Jahr steht
		// vorne. Bisher blieb davor nichts als Titel übrig, und der Film
		// hiess wie sein Ordner — alle Teile einer Reihe „Star Trek".
		jahrVorne := false
		if m := reJahrVorne.FindStringSubmatch(name); m != nil {
			g.Year, _ = strconv.Atoi(m[1])
			name, jahrVorne = m[2], true
		}
		// „Apocalypse Now 1979a.avi", „…1979b.avi": ein Film in Teilen.
		if m := reJahrTeil.FindStringSubmatch(name); m != nil && reYearParen.FindString(name) == "" {
			name = m[1] + " " + m[2]
			g.PartNo = int(m[3][0]-'a') + 1
		} else if m := reJahrKlammerTeil.FindStringSubmatch(name); m != nil {
			// „Apocalypse Now (1979) a.avi" — dasselbe mit Klammern.
			name = m[1]
			g.PartNo = int(strings.ToLower(m[2])[0]-'a') + 1
		}
		// „beforemidnight2013" → „beforemidnight", 2013.
		if m := reJahrAngeklebt.FindStringSubmatch(name); m != nil && reYearLoose.FindString(name) == "" &&
			plausiblesJahr(m[2]) {
			name = m[1] + " " + m[2]
		}
		// „Teil 2", „Part 2": Dateiteil oder Teil des Titels?
		var teilKandidat bool
		name, teilKandidat = g.teilLesen(name, jahrVorne, len(dirs) > 0 && reYearParen.MatchString(dirs[len(dirs)-1]))
		// Jahr in Klammern ist das verlässlichste Zeichen für einen Film.
		cut := len(name)
		if jahrVorne {
			// Das Jahr ist schon da; eine Zahl im Titel („2012") bleibt Titel.
		} else if m := jahrInKlammern(name); m != nil {
			g.Year, _ = strconv.Atoi(name[m[2]:m[3]])
			cut = m[0]
		} else if start, jahr := letztesJahr(name); jahr > 0 {
			g.Year, cut = jahr, start
		}
		g.Title = clean(name[:cut])
		// „Dl (2020)", „Multi (2019)", „Hdr (2021)": Der Titel heisst wie
		// ein Technik-Wort. Bleibt nach dem Aufräumen nichts übrig, war das
		// Wort der Titel — bei einem Jahr dahinter ist das sicher.
		if g.Title == "" && g.Year > 0 {
			g.Title = cleanRoh(name[:cut])
		}
		if isGenericName(g.Title) {
			g.Title = ""
		}
		if teilKandidat && g.TitelOhneTeil == "" {
			g.TeilKandidat = 0
		}
		// Steht das Jahr nicht im Dateinamen, aber im Ordner darüber, dann
		// gilt das Jahr aus dem Ordner. „Sommerhaus (1998)/Sommerhaus CD1.avi"
		// ist eben doch von 1998.
		if g.Year == 0 && len(dirs) > 0 {
			if m := reYearParen.FindStringSubmatch(dirs[len(dirs)-1]); m != nil {
				g.Year, _ = strconv.Atoi(m[1])
			}
		}
		// „Inception.2010.German…/inception.2010.german….mkv": Liegt der
		// Film in einem Ordner gleichen Namens, gilt die Schreibweise des
		// Ordners — die ist meist die gepflegte. Emby nimmt bei Filmen im
		// eigenen Ordner ohnehin den Ordnernamen.
		if g.Title != "" && len(dirs) > 0 {
			d := dirs[len(dirs)-1]
			if o := Parse(d+".mkv", nil); o.Title != g.Title && strings.EqualFold(o.Title, g.Title) {
				g.Title = o.Title
			}
		}
		if g.Title == "" {
			// Ein Ordner wie „Nordlicht (2025)" rettet Dateien, die nur
			// „movie.mkv" heissen.
			if len(dirs) > 0 {
				d := dirs[len(dirs)-1]
				if m := reYearParen.FindStringSubmatchIndex(d); m != nil {
					g.Year, _ = strconv.Atoi(d[m[2]:m[3]])
					g.Title = clean(d[:m[0]])
				} else {
					g.Title = clean(d)
				}
			}
		}
		// Ohne Jahr ist die Zuordnung ein Ratespiel — aber ein Titel allein
		// reicht oft, wenn er klar ist.
		g.Confident = g.Title != "" && (g.Year > 0 || len(strings.Fields(g.Title)) >= 2)
	}

	return g
}

// teilLesen entscheidet, ob „Teil 2"/„Part 2" im Namen ein Dateiteil ist
// oder zum Titel gehört — und gibt den Namen zurück, wie er weiter
// gelesen werden soll (ohne das „Teil 2", wenn es ein Dateiteil ist).
//
//   - „Film.part1" (das Wort klebt an der Zahl, direkt vor der Endung):
//     Dateiteil, so benennen Packprogramme und Rekorder ihre Stücke.
//   - Steht dahinter ein Jahr („Zurück in die Zukunft Teil 2 (1989)",
//     „Dune Part 2 (2024)"): Titel.
//   - Steht davor ein Jahr („Es (1990) Teil 1"): Dateiteil — der Titel war
//     schon zu Ende.
//   - Sonst Titel („Kill Bill Vol 2" ist dasselbe ohne „Teil"). Ein
//     „Film - Teil 2" mit Bindestrich oder im Ordner eines Films mit Jahr
//     ist aber vielleicht doch ein Dateiteil: Ob daneben weitere Teile
//     derselben Basis liegen, prüft der Scanner (TeilKandidat).
func (g *Guess) teilLesen(name string, jahrVorne, ordnerMitJahr bool) (string, bool) {
	m := reTeilWort.FindStringSubmatchIndex(name)
	if m == nil {
		return name, false
	}
	nr, _ := strconv.Atoi(name[m[6]:m[7]])
	vorne, hinten := name[:m[0]], name[m[1]:]
	klebt := m[4] == m[5] // kein Trenner zwischen „part" und der Zahl
	if klebt && strings.TrimSpace(hinten) == "" {
		g.PartNo = nr
		return vorne, false
	}
	if jahrInKlammern(hinten) != nil {
		return name, false
	}
	for _, j := range reYearLoose.FindAllStringSubmatch(hinten, -1) {
		if plausiblesJahr(j[1]) {
			return name, false
		}
	}
	if jahrVorne || jahrInKlammern(vorne) != nil {
		g.PartNo = nr
		return vorne + hinten, false
	}
	if _, j := letztesJahr(vorne); j > 0 {
		g.PartNo = nr
		return vorne + hinten, false
	}
	if mm := reTeilMitStrich.FindStringSubmatch(name); mm != nil || ordnerMitJahr {
		basis := vorne
		if mm != nil {
			basis = mm[1]
		}
		if t := clean(basis); t != "" {
			g.TeilKandidat, g.TitelOhneTeil = nr, t
			return name, true
		}
	}
	return name, false
}

// discOrdner findet „BDMV" oder „VIDEO_TS" unter den Ordnern und gibt
// die Stelle zurück (-1: keiner).
func discOrdner(dirs []string) int {
	for i, d := range dirs {
		switch strings.ToUpper(strings.TrimSpace(d)) {
		case "BDMV", "VIDEO_TS":
			return i
		}
	}
	return -1
}

// absolutLesen liest „[Gruppe] Titel - 1071 (1080p)": den Titel davor und
// die Nummer. Eine vierstellige Zahl, die ein Jahr sein kann, ist keine
// Folge („Serie - 2019").
func absolutLesen(name string) (vorne string, nr int, ok bool) {
	ohne := reGruppeVorne.ReplaceAllString(name, "")
	m := reAbsolut.FindStringSubmatchIndex(ohne)
	if m == nil {
		return "", 0, false
	}
	ziffern := ohne[m[2]:m[3]]
	nr, _ = strconv.Atoi(ziffern)
	if len(ziffern) == 4 && istJahr(ziffern) {
		return "", 0, false
	}
	vorne = ohne[:m[0]]
	if clean(vorne) == "" {
		return "", 0, false
	}
	return vorne, nr, true
}

// datumLesen findet ein Sendedatum im Namen und gibt es als JJJJ-MM-TT
// zurück, dazu die Stelle im Namen (für Serientitel und Folgentitel).
func datumLesen(name string) (datum, marker string) {
	if m := reDatum.FindStringSubmatch(name); m != nil {
		return m[1] + "-" + m[2] + "-" + m[3], ohneRandVorne(m[0])
	}
	if m := reDatumDE.FindStringSubmatch(name); m != nil {
		return m[3] + "-" + m[2] + "-" + m[1], ohneRandVorne(m[0])
	}
	return "", ""
}

// plausiblesJahr: Ein Jahr, das noch nicht war, ist keins. „Blade Runner
// 2049" ist ein Titel; nächstes Jahr darf es sein (Vorabfassungen).
func plausiblesJahr(s string) bool {
	n, err := strconv.Atoi(s)
	return err == nil && n >= 1900 && n <= time.Now().Year()+1
}

// jahrInKlammern ist reYearParen ohne Jahre aus der Zukunft.
func jahrInKlammern(s string) []int {
	for _, m := range reYearParen.FindAllStringSubmatchIndex(s, -1) {
		if plausiblesJahr(s[m[2]:m[3]]) {
			return m
		}
	}
	return nil
}

// seriesTitle holt den Serientitel.
//
// Der Ordner zählt mehr als der Dateiname: In
// „Ahsoka/Star Wars - Ahsok - S01E01.mkv" ist der Tippfehler im Namen,
// nicht im Ordner, und „The German/The German - S01E01.mkv" verlor im
// Dateinamen sein „German" als Sprachangabe. Vor allem aber hängen so alle
// Dateien eines Ordners an derselben Serie, auch wenn sie mal
// „The.Blacklist", mal „The Blacklist" heissen.
//
// Gesucht wird von innen nach aussen: Über einem Staffelordner steht die
// Serie; ohne Staffelordner ist es der Ordner, in dem die Datei liegt.
// Liegt die Datei direkt in der Bibliothek, bleibt nur der Dateiname.
func seriesTitle(name, marker string, dirs []string, art string) string {
	t, _ := serieMitLand(name, marker, dirs, art)
	return t
}

// serieMitLand ist seriesTitle samt Ländercode: „The Office (US)" ergibt
// „The Office" und „US". Bisher fiel das „(US)" mit den anderen Klammern
// weg — und die amerikanische und die britische Fassung wurden eine Serie.
func serieMitLand(name, marker string, dirs []string, art string) (string, string) {
	for i := len(dirs) - 1; i >= 0; i-- {
		d := strings.TrimSpace(dirs[i])
		if _, vorne, ok := staffelOrdner(d, art); ok {
			if i > 0 {
				if t, l := ordnerTitelLand(dirs[i-1]); t != "" {
					return t, l
				}
			}
			if t, l := ordnerTitelLand(vorne); t != "" {
				return t, l
			}
			break
		}
		if reSpecialsDir.MatchString(d) || IsExtrasDir(d) {
			continue
		}
		// Ein Release-Ordner pro Folge oder für ein paar Folgen: „FBI
		// S07E19 720p - Cosmo71", „Incorporated S01E07 German DL 1080p".
		// Die Serie ist, was vor der Folge steht — der ganze Ordnername
		// machte aus jeder Folge eine eigene Serie. Steht davor nichts
		// („S01E01.German…"), sagt vielleicht der Ordner darüber mehr.
		if m := reSeasonEp.FindStringIndex(d); m != nil {
			i := m[1] - len(ohneRandVorne(d[m[0]:m[1]]))
			if t, l := ordnerTitelLand(d[:i]); t != "" {
				return t, l
			}
			continue
		}
		if t, l := ordnerTitelLand(d); t != "" {
			return t, l
		}
		break
	}
	if i := strings.Index(name, marker); i > 0 {
		if t, l := landUndTitel(name[:i]); t != "" {
			return t, l
		}
	}
	return "", ""
}

// landAbtrennen nimmt einen Ländercode vom Titel: „The Office (US)",
// „Shameless (UK)", „The.Office.US.". Nur die Länder, für die es
// gleichnamige Fassungen einer Serie gibt, und nur in Grossbuchstaben,
// wenn keine Klammern stehen — „This Is Us" bleibt, wie es ist. UK heisst
// bei TMDb GB.
func landAbtrennen(s string) (string, string) {
	land := ""
	if m := reLandKlammer.FindStringSubmatchIndex(s); m != nil {
		land = strings.ToUpper(s[m[2]:m[3]])
		s = s[:m[0]] + " " + s[m[1]:]
	} else if m := reLandEnde.FindStringSubmatchIndex(s); m != nil && strings.ToUpper(s) != s {
		land = s[m[2]:m[3]]
		s = s[:m[0]]
	}
	if land == "UK" {
		land = "GB"
	}
	return s, land
}

func landUndTitel(s string) (string, string) {
	s, land := landAbtrennen(s)
	t := clean(s)
	if t == "" {
		return "", ""
	}
	return t, land
}

func ordnerTitelLand(d string) (string, string) {
	d, land := landAbtrennen(d)
	t := ordnerTitel(d)
	if t == "" {
		return "", ""
	}
	return t, land
}

// staffelOrdner erkennt „Staffel 1", „Season 02", „S03", aber auch
// „Greys Anatomy - Staffel 06" und „The.Blacklist.S01.German.1080p".
// vorne ist, was vor der Staffel steht — oft der Name der Serie.
func staffelOrdner(d, art string) (nr int, vorne string, ok bool) {
	d = strings.TrimSpace(d)
	if art == "series" {
		if m := reTeilOrdner.FindStringSubmatch(d); m != nil {
			nr, _ = strconv.Atoi(m[1])
			return nr, "", true
		}
	}
	// Ein Ordner mit Folgen-Kennzeichen ist ein Release-Ordner, keine
	// Staffel — auch wenn er auf „Special" endet („Solar.Opposites.S03E00
	// Ein … Halloween-Special"). Sonst wurde daraus Staffel 0 und die
	// Serie hiess wie der Staffelordner darüber.
	if reSeasonEp.MatchString(d) {
		return 0, "", false
	}
	if m := reSeasonDir.FindStringSubmatch(d); m != nil {
		nr, _ = strconv.Atoi(m[1])
		return nr, "", true
	}
	if m := reSpecialsMitName.FindStringSubmatch(d); m != nil {
		return 0, m[1], true
	}
	m := reStaffelMitName.FindStringSubmatch(d)
	if m == nil {
		return 0, "", false
	}
	// Dahinter darf nur Beiwerk stehen — ein Jahr, Technik, Sprache. Steht
	// dort eine Folge („S01E01") oder ein richtiger Name, ist das kein
	// Staffelordner.
	rest := strings.TrimSpace(m[3])
	if rest != "" {
		if r := rest[0]; r == 'e' || r == 'E' {
			if len(rest) > 1 && rest[1] >= '0' && rest[1] <= '9' {
				return 0, "", false
			}
		}
		if t := clean(stripYear(rest)); t != "" && !istJahr(t) && !nurTechnik(rest) {
			return 0, "", false
		}
	}
	nr, _ = strconv.Atoi(m[2])
	return nr, m[1], true
}

// nurTechnik: „German.1080p.WEB" — beginnt mit Sprache, Qualität oder
// einem Release-Brocken, dann ist der Rest Beiwerk.
func nurTechnik(s string) bool {
	teile := strings.FieldsFunc(s, func(r rune) bool {
		return r == ' ' || r == '.' || r == '_' || r == '-'
	})
	if len(teile) == 0 {
		return true
	}
	erstes := strings.ToLower(teile[0])
	if reLang.MatchString(erstes) || reQuality.MatchString(erstes) || reHDR.MatchString(erstes) {
		return true
	}
	for _, j := range junk {
		if erstes == j {
			return true
		}
	}
	return false
}

// ordnerTitel macht aus einem Ordnernamen einen Serientitel. Ein Ordner,
// der nur eine Nummer oder nichtssagend ist, taugt nicht.
func ordnerTitel(d string) string {
	t := clean(stripYear(d))
	if isGenericName(t) || reNurNummer.MatchString(t) {
		return ""
	}
	return t
}

// ohneJahrAmEnde trennt „The Office 2024" in „The Office" und 2024. Ein
// Titel, der nur aus dem Jahr besteht („1883"), bleibt, wie er ist.
func ohneJahrAmEnde(t string) (string, int) {
	m := reJahrAmEnde.FindStringSubmatch(t)
	if m == nil {
		return t, 0
	}
	j, _ := strconv.Atoi(m[2])
	if j < 1950 || j > time.Now().Year()+1 {
		return t, 0
	}
	t = strings.TrimRight(m[1], " :-")
	// „S.W.A.T.2017": Der Punkt vor dem Jahr gehört zur Abkürzung.
	if reAbkuerzungEnde.MatchString(t) {
		t += "."
	}
	return t, j
}

// episodeTitle ist, was nach dem Folgen-Kennzeichen noch steht: bei
// „Serie - S01E01 - Pilot" das „Pilot". Ein Rest, der nur aus Technik
// besteht („1080p.BluRay"), bleibt leer.
func episodeTitle(name, marker string) string {
	i := strings.Index(name, marker)
	if i < 0 {
		return ""
	}
	rest := name[i+len(marker):]
	t := clean(rest)
	if isGenericName(t) || len([]rune(t)) < 2 || nurBeiwerk(t) {
		return ""
	}
	// „Jon Stewart WEB": Beiwerk am Ende gehört nicht zum Folgentitel.
	w := strings.Fields(t)
	for len(w) > 1 && nurBeiwerk(w[len(w)-1]) {
		w = w[:len(w)-1]
	}
	return strings.Join(w, " ")
}

// nurBeiwerk: Was nach dem Aufräumen eines Release-Namens übrig bleibt
// („….s03e00.german.dl.720p.web.h264-wvf" → „web"), ist kein Folgentitel.
func nurBeiwerk(t string) bool {
	for _, w := range strings.Fields(strings.ToLower(t)) {
		switch w {
		case "web", "dl", "hdtv", "webdl", "web-dl", "proper", "repack", "internal", "h264", "h265", "x264", "x265":
		default:
			return false
		}
	}
	return true
}

// folgentitelAusOrdner: Steht der Titel nicht im Dateinamen, aber im
// Release-Ordner der Folge („Solar.Opposites.S03E00 Ein finsteres,
// grusliges Halloween-…-Special/solar.opposites.s03e00….mkv"), kommt er
// von dort.
//
// Nur, wenn der Ordner genau diese Folge nennt: Ein Ordner „S01E06 …" mit
// den Dateien S01E07 bis S01E14 darin gäbe sonst allen acht Folgen
// denselben Titel.
func folgentitelAusOrdner(dirs []string, staffel, folge int) string {
	if len(dirs) == 0 {
		return ""
	}
	d := strings.TrimSpace(dirs[len(dirs)-1])
	m := reSeasonEp.FindStringSubmatchIndex(d)
	if m == nil {
		return ""
	}
	st, _ := strconv.Atoi(d[m[2]:m[3]])
	fo, _ := strconv.Atoi(d[m[4]:m[5]])
	if st != staffel || fo != folge {
		return ""
	}
	t := clean(d[m[1]:])
	if isGenericName(t) || len([]rune(t)) < 2 || nurBeiwerk(t) {
		return ""
	}
	return t
}

// letztesJahr findet die letzte Jahreszahl, vor der noch ein Titel steht.
// „1917.2019.German" ist 1917 von 2019, „Blade.Runner.2049.2017" ist
// „Blade Runner 2049" von 2017 — so halten es Emby und Plex auch. Die
// erste Jahreszahl zu nehmen, liess bei „1917" gar keinen Titel übrig.
func letztesJahr(name string) (start, jahr int) {
	start = -1
	for i := 0; i+4 <= len(name); i++ {
		if i > 0 && !istTrenner(name[i-1]) {
			continue
		}
		if i+4 < len(name) && !istTrenner(name[i+4]) {
			continue
		}
		s := name[i : i+4]
		if !(strings.HasPrefix(s, "19") || strings.HasPrefix(s, "20")) {
			continue
		}
		n, err := strconv.Atoi(s)
		if err != nil || !plausiblesJahr(s) {
			// „Blade Runner 2049": ein Jahr, das noch nicht war, ist Titel.
			continue
		}
		if clean(name[:i]) == "" {
			// Vorne steht nichts: das ist der Titel („1917"), kein Jahr.
			continue
		}
		start, jahr = i, n
		if i > 0 {
			start = i - 1
		}
	}
	if start < 0 {
		return 0, 0
	}
	return start, jahr
}

// ohneRandVorne nimmt das Trennzeichen vorne von einem Folgen-Kennzeichen
// ab. Der Ausdruck muss es mitlesen (siehe reSeasonEp), aber zum Titel
// davor gehört es: „S.H.I.E.L.D.S05E01" verlöre sonst den letzten Punkt.
func ohneRandVorne(m string) string {
	if m != "" && !isWordChar(m[0]) {
		return m[1:]
	}
	return m
}

func istTrenner(b byte) bool {
	return b == ' ' || b == '.' || b == '_' || b == '-'
}

func istJahr(s string) bool {
	n, _ := strconv.Atoi(s)
	return n >= 1900 && n <= 2100
}

func stripYear(s string) string {
	if m := reYearParen.FindStringIndex(s); m != nil {
		return s[:m[0]]
	}
	return s
}

// clean macht aus „Die.Bucht.2019.German.1080p.BluRay.x264-GRP" den Titel
// „Die Bucht".
func clean(s string) string {
	t := cleanMit(s, true)
	// „The German" ohne Sprachangabe wäre „The". Bleibt nur ein Artikel
	// übrig, war das Wort Teil des Titels.
	if istArtikel(t) {
		if mit := cleanMit(s, false); mit != "" {
			return mit
		}
	}
	return t
}

func istArtikel(s string) bool {
	switch strings.ToLower(s) {
	case "the", "a", "an", "der", "die", "das", "ein", "eine", "le", "la", "les", "el", "il":
		return true
	}
	return false
}

func cleanMit(s string, ohneSprache bool) string { return cleanWeg(s, ohneSprache, true) }

// cleanRoh räumt nur Trennzeichen, Klammern und Release-Brocken weg und
// lässt Technik-Wörter stehen — für Titel wie „Dl (2020)" oder „Hdr".
func cleanRoh(s string) string { return cleanWeg(s, false, false) }

func cleanWeg(s string, ohneSprache, ohneTechnik bool) string {
	s = reBrackets.ReplaceAllString(s, " ")

	// Technische Angaben gehören nicht in den Titel — sie stehen schon in
	// eigenen Feldern. „Sommerhaus CD1" ist der Film „Sommerhaus", Teil 1.
	var weg []*regexp.Regexp
	if ohneTechnik {
		weg = []*regexp.Regexp{rePart, reEdition, reQuality, reHDR}
	}
	if ohneSprache {
		weg = append(weg, reLang)
	}
	for _, re := range weg {
		s = re.ReplaceAllString(s, " ")
	}
	// Punkte in Abkürzungen schützen, bevor Punkte zu Leerzeichen werden.
	s = reAbkuerzung.ReplaceAllStringFunc(s, func(a string) string {
		return strings.ReplaceAll(a, ".", "\x00")
	})

	// Trennzeichen zu Leerzeichen — aber ein Bindestrich mitten im Wort
	// bleibt („Spider-Man"), während „ - " ein Trenner ist.
	s = strings.ReplaceAll(s, "_", " ")
	// „ - " steht in Dateinamen fast immer für den Doppelpunkt, den kein
	// Dateisystem mag: „Der Herr der Ringe - Die zwei Türme" heisst
	// eigentlich „Der Herr der Ringe: Die zwei Türme". So soll er auch
	// dastehen — und nur so erkennt Lumen ohne TMDb, dass er zu einer
	// Reihe gehört.
	s = strings.ReplaceAll(s, " - ", ": ")
	// „Die.Bucht" ist ein Trenner-Name, „Mr. Nobody" nicht: Punkte lösen wir
	// auf, sobald der Name sonst keine Leerzeichen hat oder reichlich Punkte.
	if strings.Count(s, ".") >= 2 || (strings.Contains(s, ".") && !strings.Contains(s, " ")) {
		s = strings.ReplaceAll(s, ".", " ")
	}

	// Alles ab dem ersten Release-Brocken wegschneiden.
	low := strings.ToLower(s)
	cut := len(s)
	for _, j := range junk {
		if i := indexWord(low, j); i >= 0 && i < cut {
			cut = i
		}
	}
	s = s[:cut]

	// Ein angehängtes „-GRUPPE" fällt weg — aber nur, wenn es wirklich wie ein
	// Gruppenkürzel aussieht. „Spider-Man" darf nicht zu „Spider" werden.
	if i := strings.LastIndex(s, "-"); i > 0 && !strings.Contains(s[i:], " ") {
		if tail := s[i+1:]; looksLikeGroup(tail) {
			s = s[:i]
		}
	}

	s = reSpace.ReplaceAllString(s, " ")
	s = strings.ReplaceAll(s, " : ", ": ")
	s = strings.TrimSpace(strings.Trim(s, " .-_:"))
	return strings.ReplaceAll(s, "\x00", ".")
}

// indexWord findet ein Wort nur an Wortgrenzen, damit „dl" nicht in
// „Waldrand" zuschlägt.
func indexWord(hay, needle string) int {
	from := 0
	for {
		i := strings.Index(hay[from:], needle)
		if i < 0 {
			return -1
		}
		i += from
		okBefore := i == 0 || !isWordChar(hay[i-1])
		end := i + len(needle)
		okAfter := end >= len(hay) || !isWordChar(hay[end])
		if okBefore && okAfter {
			return i
		}
		from = i + 1
	}
}

// looksLikeGroup: Release-Gruppen schreiben sich gross oder hängen Ziffern
// an. Ein normal geschriebenes Wort ist keins.
func looksLikeGroup(s string) bool {
	if len(s) < 2 || len(s) > 12 {
		return false
	}
	hasLower := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' {
			hasLower = true
		} else if !(c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
			return false
		}
	}
	return !hasLower
}

func isWordChar(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9'
}

func tidyEdition(s string) string {
	s = strings.NewReplacer(".", " ", "_", " ", "-", " ").Replace(strings.ToLower(s))
	switch {
	case strings.HasPrefix(s, "director"):
		return "Director's Cut"
	case strings.HasPrefix(s, "extended"):
		return "Extended"
	case strings.HasPrefix(s, "final"):
		return "Final Cut"
	case strings.HasPrefix(s, "special"):
		return "Special Edition"
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// isGenericName erkennt Dateinamen, die nichts aussagen — dann hilft nur
// der Ordner darüber weiter.
func isGenericName(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "movie", "film", "video", "main", "index", "title", "untitled",
		"vts 01 1", "vts_01_1", "video ts", "fullmovie", "full":
		return true
	}
	return false
}

// VideoExt sind die Endungen, die der Scanner als Film ansieht.
var VideoExt = map[string]bool{
	".mkv": true, ".mp4": true, ".m4v": true, ".avi": true, ".mov": true,
	".wmv": true, ".ts": true, ".m2ts": true, ".mpg": true, ".mpeg": true,
	".webm": true, ".flv": true, ".vob": true,
	// ISO-Abbilder liest FFmpeg nicht; .strm-Dateien (eine Adresse statt
	// eines Films) liesse Lumen auf beliebige Adressen zugreifen — beides
	// bleibt bewusst draussen.
	".iso": false, ".strm": false,
}

// SubtitleExt sind Untertitel, die neben der Datei liegen.
var SubtitleExt = map[string]bool{
	".srt": true, ".ass": true, ".ssa": true, ".sub": true, ".vtt": true,
}

// IsVideo sagt, ob eine Datei überhaupt in Frage kommt.
func IsVideo(path string) bool {
	return VideoExt[strings.ToLower(filepath.Ext(path))]
}

// IsJunkFile filtert Beiwerk heraus: Trailer, Samples, Extras und die
// Dateien, die macOS und Windows überall hinterlassen.
//
// Früher reichte ein „trailer" oder „sample" irgendwo im Namen — dann
// fehlten „Trailer Park Boys" und „The Sample (2020)" in der Mediathek,
// und jede Datei einer Release-Gruppe mit „rarbg" im Namen auch. Jetzt
// zählen nur die Muster, die Emby, Plex und Kodi für Beiwerk festlegen:
// eine Endung wie „-trailer" oder „ - featurette" hinter dem Namen oder
// ein Name, der nur „sample" oder „trailer" ist. Beiwerk in eigenen
// Ordnern („Trailers", „Featurettes" …) fängt IsExtrasDir ab.
func IsJunkFile(path string) bool {
	base := strings.ToLower(filepath.Base(path))
	if strings.HasPrefix(base, ".") {
		return true
	}
	return IstBeiwerkName(strings.TrimSuffix(base, filepath.Ext(base)))
}

// beiwerkEndungen stehen hinter einem Bindestrich am Ende des Namens:
// „Inception (2010) - featurette", „film-behindthescenes". „short" und
// „scene" nur so — „The Big Short" ist ein Film.
var beiwerkEndungen = []string{
	"trailer", "sample", "featurette", "behindthescenes", "deleted", "deletedscene",
	"deletedscenes", "interview", "scene", "short", "extra", "other",
}

// IstBeiwerkName prüft einen Dateinamen ohne Endung.
func IstBeiwerkName(stem string) bool {
	stem = strings.ToLower(strings.TrimSpace(stem))
	switch stem {
	case "sample", "trailer", "rarbg", "readme":
		return true
	}
	for _, e := range beiwerkEndungen {
		if !strings.HasSuffix(stem, e) {
			continue
		}
		vor := strings.TrimRight(stem[:len(stem)-len(e)], " ")
		if strings.HasSuffix(vor, "-") {
			return true
		}
		// „film.sample", „film_trailer" — so schreiben Release-Gruppen
		// ihre Beigaben. Nur für Trailer und Samples, ein „.short" am
		// Ende kann ein Titel sein.
		if (e == "trailer" || e == "sample") && (strings.HasSuffix(vor, ".") || strings.HasSuffix(vor, "_")) {
			return true
		}
	}
	return false
}

// IsExtrasDir erkennt Ordner, die Beiwerk enthalten — die gehören nicht in
// die Mediathek, sondern auf die Titelseite unter „Extras". Einzahl und
// Mehrzahl, wie Emby und Plex sie kennen.
func IsExtrasDir(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "extras", "extra", "featurettes", "featurette", "behind the scenes",
		"deleted scenes", "deleted scene", "interviews", "interview", "scenes", "scene",
		"shorts", "short", "trailers", "trailer", "samples", "sample", "other", "others",
		"specials", "bonus":
		return true
	}
	return false
}

// IstSystemOrdner erkennt Ordner, die das NAS oder das Betriebssystem
// anlegen: den Papierkorb von Synology („#recycle") und Windows
// („$RECYCLE.BIN"), Schnappschüsse, Vorschaubilder („@eaDir",
// „.@__thumb"). Was darin liegt, ist gelöscht oder ein Abbild — in die
// Mediathek gehört es nicht, auch wenn es aussieht wie ein Film.
func IstSystemOrdner(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	if strings.HasPrefix(n, ".") || strings.HasPrefix(n, "@") {
		return true
	}
	switch n {
	case "#recycle", "#snapshot", "$recycle.bin", "recycler", "recycled",
		"system volume information", "lost+found", "$extend", "#recycle.bin":
		return true
	}
	return false
}

// IstSystemPfad: liegt irgendwo auf dem Weg ein solcher Ordner?
func IstSystemPfad(rel string) bool {
	for _, d := range strings.Split(filepath.ToSlash(filepath.Dir(rel)), "/") {
		if d != "." && d != "" && IstSystemOrdner(d) {
			return true
		}
	}
	return false
}
