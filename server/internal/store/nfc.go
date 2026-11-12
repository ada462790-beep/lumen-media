// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

// NFC: Umlaute in einer Schreibweise.
//
// macOS schreibt Dateinamen über SMB oft zerlegt (NFD): „ö" als „o" und
// ein nachgestelltes Trema (U+0308). So gespeichert fand die Suche nach
// „Schöne" den Film „Die Schöne und das Biest" nicht, die Buchstabenleiste
// und das Zusammenlegen von Fassungen sahen zwei verschiedene Titel.
// golang.org/x/text ist nicht eingebunden; für Titel genügt das
// Zusammensetzen der lateinischen Buchstaben mit ihren Akzenten (Latin-1,
// Latin Extended-A/B und Latin Extended Additional, erzeugt mit Pythons
// unicodedata). Andere Schriften bleiben, wie sie sind.

import "strings"

// nfcTabelle: je drei Zeichen — Grundbuchstabe, Akzent, zusammengesetzt.
const nfcTabelle = "A\u0300ÀA\u0301ÁA\u0302ÂA\u0303ÃA\u0308ÄA\u030aÅC\u0327ÇE\u0300ÈE\u0301ÉE\u0302ÊE\u0308ËI\u0300ÌI\u0301ÍI\u0302ÎI\u0308ÏN\u0303ÑO\u0300ÒO\u0301ÓO\u0302ÔO\u0303ÕO\u0308ÖU\u0300ÙU\u0301ÚU\u0302ÛU\u0308ÜY\u0301Ýa\u0300àa\u0301áa\u0302âa\u0303ãa\u0308äa\u030aåc\u0327çe\u0300èe\u0301ée\u0302êe\u0308ëi\u0300ìi\u0301íi\u0302îi\u0308ïn\u0303ño\u0300òo\u0301óo\u0302ôo\u0303õo\u0308öu\u0300ùu\u0301úu\u0302ûu\u0308üy\u0301ýy\u0308ÿA\u0304Āa\u0304āA\u0306Ăa\u0306ăA\u0328Ąa\u0328ąC\u0301Ć" +
	"c\u0301ćC\u0302Ĉc\u0302ĉC\u0307Ċc\u0307ċC\u030cČc\u030cčD\u030cĎd\u030cďE\u0304Ēe\u0304ēE\u0306Ĕe\u0306ĕE\u0307Ėe\u0307ėE\u0328Ęe\u0328ęE\u030cĚe\u030cěG\u0302Ĝg\u0302ĝG\u0306Ğg\u0306ğG\u0307Ġg\u0307ġG\u0327Ģg\u0327ģH\u0302Ĥh\u0302ĥI\u0303Ĩi\u0303ĩI\u0304Īi\u0304īI\u0306Ĭi\u0306ĭI\u0328Įi\u0328įI\u0307İJ\u0302Ĵj\u0302ĵK\u0327Ķk\u0327ķL\u0301Ĺl\u0301ĺL\u0327Ļl\u0327ļL\u030cĽl\u030cľN\u0301Ńn\u0301ńN\u0327Ņn\u0327ņN\u030cŇn\u030cňO\u0304Ōo\u0304ōO\u0306Ŏo\u0306ŏO\u030bŐo\u030bő" +
	"R\u0301Ŕr\u0301ŕR\u0327Ŗr\u0327ŗR\u030cŘr\u030cřS\u0301Śs\u0301śS\u0302Ŝs\u0302ŝS\u0327Şs\u0327şS\u030cŠs\u030cšT\u0327Ţt\u0327ţT\u030cŤt\u030cťU\u0303Ũu\u0303ũU\u0304Ūu\u0304ūU\u0306Ŭu\u0306ŭU\u030aŮu\u030aůU\u030bŰu\u030bűU\u0328Ųu\u0328ųW\u0302Ŵw\u0302ŵY\u0302Ŷy\u0302ŷY\u0308ŸZ\u0301Źz\u0301źZ\u0307Żz\u0307żZ\u030cŽz\u030cžO\u031bƠo\u031bơU\u031bƯu\u031bưA\u030cǍa\u030cǎI\u030cǏi\u030cǐO\u030cǑo\u030cǒU\u030cǓu\u030cǔÜ\u0304Ǖü\u0304ǖÜ\u0301Ǘü\u0301ǘÜ\u030cǙü\u030cǚÜ\u0300Ǜ" +
	"ü\u0300ǜÄ\u0304Ǟä\u0304ǟȦ\u0304Ǡȧ\u0304ǡÆ\u0304Ǣæ\u0304ǣG\u030cǦg\u030cǧK\u030cǨk\u030cǩO\u0328Ǫo\u0328ǫǪ\u0304Ǭǫ\u0304ǭƷ\u030cǮʒ\u030cǯj\u030cǰG\u0301Ǵg\u0301ǵN\u0300Ǹn\u0300ǹÅ\u0301Ǻå\u0301ǻÆ\u0301Ǽæ\u0301ǽØ\u0301Ǿø\u0301ǿA\u030fȀa\u030fȁA\u0311Ȃa\u0311ȃE\u030fȄe\u030fȅE\u0311Ȇe\u0311ȇI\u030fȈi\u030fȉI\u0311Ȋi\u0311ȋO\u030fȌo\u030fȍO\u0311Ȏo\u0311ȏR\u030fȐr\u030fȑR\u0311Ȓr\u0311ȓU\u030fȔu\u030fȕU\u0311Ȗu\u0311ȗS\u0326Șs\u0326șT\u0326Țt\u0326țH\u030cȞh\u030cȟA\u0307Ȧa\u0307ȧ" +
	"E\u0327Ȩe\u0327ȩÖ\u0304Ȫö\u0304ȫÕ\u0304Ȭõ\u0304ȭO\u0307Ȯo\u0307ȯȮ\u0304Ȱȯ\u0304ȱY\u0304Ȳy\u0304ȳA\u0325Ḁa\u0325ḁB\u0307Ḃb\u0307ḃB\u0323Ḅb\u0323ḅB\u0331Ḇb\u0331ḇÇ\u0301Ḉç\u0301ḉD\u0307Ḋd\u0307ḋD\u0323Ḍd\u0323ḍD\u0331Ḏd\u0331ḏD\u0327Ḑd\u0327ḑD\u032dḒd\u032dḓĒ\u0300Ḕē\u0300ḕĒ\u0301Ḗē\u0301ḗE\u032dḘe\u032dḙE\u0330Ḛe\u0330ḛȨ\u0306Ḝȩ\u0306ḝF\u0307Ḟf\u0307ḟG\u0304Ḡg\u0304ḡH\u0307Ḣh\u0307ḣH\u0323Ḥh\u0323ḥH\u0308Ḧh\u0308ḧH\u0327Ḩh\u0327ḩH\u032eḪh\u032eḫI\u0330Ḭi\u0330ḭÏ\u0301Ḯï\u0301ḯ" +
	"K\u0301Ḱk\u0301ḱK\u0323Ḳk\u0323ḳK\u0331Ḵk\u0331ḵL\u0323Ḷl\u0323ḷḶ\u0304Ḹḷ\u0304ḹL\u0331Ḻl\u0331ḻL\u032dḼl\u032dḽM\u0301Ḿm\u0301ḿM\u0307Ṁm\u0307ṁM\u0323Ṃm\u0323ṃN\u0307Ṅn\u0307ṅN\u0323Ṇn\u0323ṇN\u0331Ṉn\u0331ṉN\u032dṊn\u032dṋÕ\u0301Ṍõ\u0301ṍÕ\u0308Ṏõ\u0308ṏŌ\u0300Ṑō\u0300ṑŌ\u0301Ṓō\u0301ṓP\u0301Ṕp\u0301ṕP\u0307Ṗp\u0307ṗR\u0307Ṙr\u0307ṙR\u0323Ṛr\u0323ṛṚ\u0304Ṝṛ\u0304ṝR\u0331Ṟr\u0331ṟS\u0307Ṡs\u0307ṡS\u0323Ṣs\u0323ṣŚ\u0307Ṥś\u0307ṥŠ\u0307Ṧš\u0307ṧṢ\u0307Ṩṣ\u0307ṩT\u0307Ṫt\u0307ṫ" +
	"T\u0323Ṭt\u0323ṭT\u0331Ṯt\u0331ṯT\u032dṰt\u032dṱU\u0324Ṳu\u0324ṳU\u0330Ṵu\u0330ṵU\u032dṶu\u032dṷŨ\u0301Ṹũ\u0301ṹŪ\u0308Ṻū\u0308ṻV\u0303Ṽv\u0303ṽV\u0323Ṿv\u0323ṿW\u0300Ẁw\u0300ẁW\u0301Ẃw\u0301ẃW\u0308Ẅw\u0308ẅW\u0307Ẇw\u0307ẇW\u0323Ẉw\u0323ẉX\u0307Ẋx\u0307ẋX\u0308Ẍx\u0308ẍY\u0307Ẏy\u0307ẏZ\u0302Ẑz\u0302ẑZ\u0323Ẓz\u0323ẓZ\u0331Ẕz\u0331ẕh\u0331ẖt\u0308ẗw\u030aẘy\u030aẙſ\u0307ẛA\u0323Ạa\u0323ạA\u0309Ảa\u0309ảÂ\u0301Ấâ\u0301ấÂ\u0300Ầâ\u0300ầÂ\u0309Ẩâ\u0309ẩÂ\u0303Ẫâ\u0303ẫẠ\u0302Ậ" +
	"ạ\u0302ậĂ\u0301Ắă\u0301ắĂ\u0300Ằă\u0300ằĂ\u0309Ẳă\u0309ẳĂ\u0303Ẵă\u0303ẵẠ\u0306Ặạ\u0306ặE\u0323Ẹe\u0323ẹE\u0309Ẻe\u0309ẻE\u0303Ẽe\u0303ẽÊ\u0301Ếê\u0301ếÊ\u0300Ềê\u0300ềÊ\u0309Ểê\u0309ểÊ\u0303Ễê\u0303ễẸ\u0302Ệẹ\u0302ệI\u0309Ỉi\u0309ỉI\u0323Ịi\u0323ịO\u0323Ọo\u0323ọO\u0309Ỏo\u0309ỏÔ\u0301Ốô\u0301ốÔ\u0300Ồô\u0300ồÔ\u0309Ổô\u0309ổÔ\u0303Ỗô\u0303ỗỌ\u0302Ộọ\u0302ộƠ\u0301Ớơ\u0301ớƠ\u0300Ờơ\u0300ờƠ\u0309Ởơ\u0309ởƠ\u0303Ỡơ\u0303ỡƠ\u0323Ợơ\u0323ợU\u0323Ụu\u0323ụU\u0309Ủu\u0309ủƯ\u0301Ứ" +
	"ư\u0301ứƯ\u0300Ừư\u0300ừƯ\u0309Ửư\u0309ửƯ\u0303Ữư\u0303ữƯ\u0323Ựư\u0323ựY\u0300Ỳy\u0300ỳY\u0323Ỵy\u0323ỵY\u0309Ỷy\u0309ỷY\u0303Ỹy\u0303ỹ"

var nfcPaare = func() map[[2]rune]rune {
	m := map[[2]rune]rune{}
	r := []rune(nfcTabelle)
	for i := 0; i+2 < len(r); i += 3 {
		m[[2]rune{r[i], r[i+1]}] = r[i+2]
	}
	return m
}()

// istAkzent: kombinierende diakritische Zeichen (U+0300–U+036F).
func istAkzent(r rune) bool { return r >= 0x300 && r <= 0x36f }

// NFC setzt Buchstaben und nachgestellte Akzente zusammen („o\u0308" →
// „ö"). Ohne kombinierende Zeichen kommt die Zeichenkette unverändert
// zurück — ohne neue Speicherbelegung.
func NFC(s string) string {
	hat := false
	for i := 0; i < len(s); i++ {
		// Kombinierende Zeichen U+0300–U+036F beginnen in UTF-8 mit 0xCC oder 0xCD.
		if s[i] == 0xcc || s[i] == 0xcd {
			hat = true
			break
		}
	}
	if !hat {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	var vorher rune = -1
	for _, r := range s {
		if istAkzent(r) && vorher >= 0 {
			if z, ok := nfcPaare[[2]rune{vorher, r}]; ok {
				vorher = z
				continue
			}
		}
		if vorher >= 0 {
			b.WriteRune(vorher)
		}
		vorher = r
	}
	if vorher >= 0 {
		b.WriteRune(vorher)
	}
	return b.String()
}
