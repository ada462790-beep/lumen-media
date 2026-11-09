// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

import XCTest
@testable import LumenKit

/// Welche Sprache gilt — Profil, Server, Gerät — und wie Zahlen und Tage
/// in ihr aussehen.
final class SpracheTests: XCTestCase {

    override func tearDown() {
        // Die übrigen Tests erwarten die Quellsprache.
        Sprache.setzen(Sprache.quelle, merken: false)
        super.tearDown()
    }

    func testBekannt() {
        XCTAssertEqual(Sprache.bekannt("de-CH"), "de-CH")
        XCTAssertEqual(Sprache.bekannt("de_ch"), "de-CH")
        XCTAssertEqual(Sprache.bekannt("en-gb"), "en-GB")
        XCTAssertEqual(Sprache.bekannt("de"), "de-DE")
        XCTAssertEqual(Sprache.bekannt("en"), "en-US")
        XCTAssertNil(Sprache.bekannt(""))
        XCTAssertNil(Sprache.bekannt(nil))
        XCTAssertNil(Sprache.bekannt("fr-FR"))
    }

    func testGeraet() {
        XCTAssertEqual(Sprache.ausGeraet(["de-CH"]), "de-CH")
        XCTAssertEqual(Sprache.ausGeraet(["de-AT"]), "de-AT")
        XCTAssertEqual(Sprache.ausGeraet(["de-DE"]), "de-DE")
        XCTAssertEqual(Sprache.ausGeraet(["de"]), "de-DE")
        XCTAssertEqual(Sprache.ausGeraet(["en-GB"]), "en-GB")
        XCTAssertEqual(Sprache.ausGeraet(["en-AU"]), "en-GB")
        XCTAssertEqual(Sprache.ausGeraet(["en"]), "en-US")
        XCTAssertEqual(Sprache.ausGeraet(["en-US"]), "en-US")
        // Erst eine Sprache, die Lumen nicht kann — dann zählt die nächste.
        XCTAssertEqual(Sprache.ausGeraet(["fr-CH", "en-GB"]), "en-GB")
        // Nichts davon: Deutsch (Schweiz).
        XCTAssertEqual(Sprache.ausGeraet(["fr-FR", "it-IT"]), "de-CH")
        XCTAssertEqual(Sprache.ausGeraet(["zh-Hans-CN"]), "de-CH")
    }

    func testReihenfolge() {
        // Das Profil schlägt den Server, der Server das Gerät.
        XCTAssertEqual(Sprache.wirksam(profil: "en-GB", server: "de-DE", geraet: ["de-AT"]), "en-GB")
        XCTAssertEqual(Sprache.wirksam(profil: "", server: "de-DE", geraet: ["de-AT"]), "de-DE")
        XCTAssertEqual(Sprache.wirksam(profil: nil, server: nil, geraet: ["de-AT"]), "de-AT")
        // Ein älterer Server ohne Sprache, ein Gerät auf Französisch.
        XCTAssertEqual(Sprache.wirksam(profil: nil, server: nil, geraet: ["fr-FR"]), "de-CH")
    }

    func testSetzen() {
        XCTAssertEqual(Sprache.aktuell, Sprache.quelle)
        XCTAssertTrue(Sprache.setzen("en-GB", merken: false))
        XCTAssertEqual(Sprache.aktuell, "en-GB")
        XCTAssertFalse(Sprache.setzen("en-GB", merken: false))
        // Unbekanntes fällt auf die Quellsprache zurück.
        Sprache.setzen("xx", merken: false)
        XCTAssertEqual(Sprache.aktuell, "de-CH")
    }

    func testZahlenJeSprache() {
        Sprache.setzen("de-CH", merken: false)
        XCTAssertEqual(MediaFile.groesse(4_200_000_000), "4.2 GB")
        Sprache.setzen("de-DE", merken: false)
        XCTAssertEqual(MediaFile.groesse(4_200_000_000), "4,2 GB")
        XCTAssertEqual(Fassungen.groesse(7_900_000_000), "7,9 GB")
        Sprache.setzen("en-US", merken: false)
        XCTAssertEqual(Fassungen.groesse(7_900_000_000), "7.9 GB")
        XCTAssertEqual(Fassungen.groesse(8_000_000_000), "8 GB")
    }

    func testTageJeSprache() {
        Sprache.setzen("de-AT", merken: false)
        XCTAssertEqual(PersonInfo.datum("1968-12-23"), "23.12.1968")
        Sprache.setzen("en-US", merken: false)
        XCTAssertEqual(PersonInfo.datum("1968-12-23"), "12/23/1968")
        Sprache.setzen("en-GB", merken: false)
        XCTAssertEqual(PersonInfo.datum("1968-12-23"), "23/12/1968")
        // Nur ein Jahr bleibt ein Jahr.
        XCTAssertEqual(PersonInfo.datum("1970"), "1970")
    }
}
