// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package scan

import (
	"path/filepath"
	"strings"
)

// untertitelSprachen: was in Dateinamen für eine Sprache steht — Kürzel
// nach ISO 639-1 und -2 (beide Formen, „ger" und „deu") und die Namen,
// wie Release-Gruppen und Programme wie Subtitle Edit sie schreiben.
var untertitelSprachen = map[string]string{
	"de": "de", "deu": "de", "ger": "de", "german": "de", "deutsch": "de",
	"en": "en", "eng": "en", "english": "en", "englisch": "en",
	"fr": "fr", "fre": "fr", "fra": "fr", "french": "fr", "francais": "fr", "français": "fr", "französisch": "fr",
	"it": "it", "ita": "it", "italian": "it", "italiano": "it", "italienisch": "it",
	"es": "es", "spa": "es", "spanish": "es", "espanol": "es", "español": "es", "spanisch": "es",
	"nl": "nl", "dut": "nl", "nld": "nl", "dutch": "nl", "nederlands": "nl",
	"pt": "pt", "por": "pt", "portuguese": "pt",
	"ru": "ru", "rus": "ru", "russian": "ru",
	"pl": "pl", "pol": "pl", "polish": "pl",
	"sv": "sv", "swe": "sv", "swedish": "sv",
	"da": "da", "dan": "da", "danish": "da",
	"no": "no", "nor": "no", "nob": "no", "norwegian": "no",
	"fi": "fi", "fin": "fi", "finnish": "fi",
	"tr": "tr", "tur": "tr", "turkish": "tr",
	"ja": "ja", "jpn": "ja", "japanese": "ja",
	"ko": "ko", "kor": "ko", "korean": "ko",
	"zh": "zh", "chi": "zh", "zho": "zh", "chinese": "zh",
	"ar": "ar", "ara": "ar", "arabic": "ar",
	"he": "he", "heb": "he", "hebrew": "he",
	"cs": "cs", "cze": "cs", "ces": "cs", "czech": "cs",
	"hu": "hu", "hun": "hu", "hungarian": "hu",
	"el": "el", "gre": "el", "ell": "el", "greek": "el",
}

// UntertitelDaneben prüft, ob die Datei name ein Untertitel zum Film mit
// dem Namensstamm stem ist, und liest Sprache, „forced" und „für
// Hörgeschädigte" aus dem Rest des Namens: „Film.de.forced.srt",
// „Film.ger.srt", „Film.German.SDH.ass".
//
// Gelesen werden .srt, .ass, .ssa, .vtt und .sub. Eine .sub mit einer
// .idx daneben ist VobSub — Bilder statt Text. Daraus lässt sich kein
// WebVTT machen, und einbrennen kann Lumen (noch) nicht; sie wäre in der
// Liste, liesse sich aber nicht zeigen. Darum bleibt sie weg. Eine .sub
// ohne .idx ist MicroDVD-Text; den wandelt FFmpeg. gibt sagt, ob es im
// Ordner eine Datei dieses Namens gibt.
func UntertitelDaneben(name, stem string, gibt func(string) bool) (Track, bool) {
	ext := strings.ToLower(filepath.Ext(name))
	if !SubtitleExt[ext] || !strings.HasPrefix(name, stem) {
		return Track{}, false
	}
	basis := strings.TrimSuffix(name, filepath.Ext(name))
	if ext == ".sub" && gibt != nil && gibt(basis+".idx") {
		return Track{}, false
	}
	rest := strings.ToLower(strings.TrimPrefix(basis, stem))
	// „Film.mkv" und „Film 2.srt": Der Rest muss mit einem Trenner
	// beginnen, sonst gehört der Untertitel zu einem anderen Film.
	if rest != "" && !strings.ContainsRune(".-_ ", rune(rest[0])) {
		return Track{}, false
	}
	t := Track{Kind: "subtitle", Codec: strings.TrimPrefix(ext, "."), Language: "und"}
	for _, teil := range strings.FieldsFunc(rest, func(r rune) bool {
		return r == '.' || r == '_' || r == '-' || r == ' ' || r == '(' || r == ')' || r == '[' || r == ']'
	}) {
		if l, ok := untertitelSprachen[teil]; ok && t.Language == "und" {
			t.Language = l
		} else if l, ok := sprachKuerzel[teil]; ok && t.Language == "und" {
			// Weitere Sprachen aus sprachen.go.
			t.Language = l
			continue
		}
		switch teil {
		case "forced", "erzwungen", "foreign":
			t.Forced = true
		case "sdh", "cc", "hi", "hoh":
			t.HearingImpaired = true
		}
	}
	return t, true
}
