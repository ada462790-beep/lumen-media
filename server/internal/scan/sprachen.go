// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package scan

// sprachKuerzel kennt die gängigen Sprachkürzel in Dateinamen von
// Untertiteln — zweistellig (ISO 639-1, wie OpenSubtitles sie vergibt)
// und dreistellig (ISO 639-2, wie MKV-Werkzeuge sie schreiben). Vorher
// kannte Lumen nur Deutsch und Englisch; „Film.fr.srt" stand dann als
// „Unbekannt" im Spurmenü.
var sprachKuerzel = map[string]string{
	"fr": "fr", "fre": "fr", "fra": "fr", "french": "fr",
	"it": "it", "ita": "it", "italian": "it",
	"es": "es", "spa": "es", "spanish": "es",
	"pt": "pt", "por": "pt", "portuguese": "pt",
	"nl": "nl", "nld": "nl", "dut": "nl", "dutch": "nl",
	"ru": "ru", "rus": "ru", "russian": "ru",
	"pl": "pl", "pol": "pl", "polish": "pl",
	"tr": "tr", "tur": "tr", "turkish": "tr",
	"sv": "sv", "swe": "sv", "swedish": "sv",
	"da": "da", "dan": "da", "danish": "da",
	"no": "no", "nor": "no", "nb": "no", "nob": "no", "norwegian": "no",
	"fi": "fi", "fin": "fi", "finnish": "fi",
	"ja": "ja", "jpn": "ja", "japanese": "ja",
	"ko": "ko", "kor": "ko", "korean": "ko",
	"zh": "zh", "zho": "zh", "chi": "zh", "chinese": "zh",
	"ar": "ar", "ara": "ar", "arabic": "ar",
	"cs": "cs", "ces": "cs", "cze": "cs", "czech": "cs",
	"hu": "hu", "hun": "hu", "hungarian": "hu",
	"el": "el", "ell": "el", "gre": "el", "greek": "el",
	"he": "he", "heb": "he", "hebrew": "he",
	"ro": "ro", "ron": "ro", "rum": "ro", "romanian": "ro",
	"hr": "hr", "hrv": "hr", "croatian": "hr",
	"sr": "sr", "srp": "sr", "serbian": "sr",
	"uk": "uk", "ukr": "uk", "ukrainian": "uk",
}
