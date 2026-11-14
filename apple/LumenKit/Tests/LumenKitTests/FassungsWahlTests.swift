// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

import XCTest
@testable import LumenKit

// Fassungen und Teile eines Films — dieselben Fälle wie in
// lumen-core (Tests.kt: fassungenTests), damit iPhone, Fernseher, Android
// und Browser dasselbe zeigen und denselben Stand speichern.

final class FassungsWahlTests: XCTestCase {

    private func datei(_ json: String) throws -> MediaFile {
        try JSONDecoder.lumen.decode(MediaFile.self, from: Data(json.utf8))
    }

    /// „American Psycho (2000)": MKV in 1080p und AVI in 400p — zwei
    /// Fassungen. Der Server schickt sie schon geordnet, die beste vorn.
    func testFassungen() throws {
        let mkv = try datei(#"{"id":"mkv","itemId":"m","path":"/f/American Psycho (2000).mkv","sizeBytes":7900000000,"partNo":1,"container":"mkv","height":1080,"durationMs":6120000,"preferred":true}"#)
        let avi = try datei(#"{"id":"avi","itemId":"m","path":"/f/American Psycho (2000).avi","sizeBytes":700000000,"partNo":1,"container":"avi","height":400,"durationMs":6120000}"#)
        let weg = try datei(#"{"id":"weg","itemId":"m","path":"/f/x.mkv","sizeBytes":0,"partNo":1,"height":2160,"missing":true}"#)

        XCTAssertEqual(Fassungen.liste([mkv, avi, weg]).map(\.id), ["mkv", "avi"])
        XCTAssertTrue(Fassungen.mehrere([mkv, avi]))
        XCTAssertFalse(Fassungen.mehrere([mkv, weg]))
        XCTAssertEqual(Fassungen.name(mkv), "1080p · MKV · 7.9 GB")
        XCTAssertEqual(Fassungen.name(avi), "400p · AVI · 700 MB")
        XCTAssertEqual(Fassungen.aufloesung(2160), "4K")
        XCTAssertEqual(Fassungen.aufloesung(720), "720p")
        XCTAssertNil(Fassungen.aufloesung(nil))
        XCTAssertEqual(Fassungen.groesse(8_000_000_000), "8 GB")
        XCTAssertEqual(Fassungen.groesse(1234), "1 MB")

        XCTAssertEqual(Fassungen.datei([mkv, avi], gewaehlt: "avi")?.id, "avi")
        XCTAssertEqual(Fassungen.datei([mkv, avi], gewaehlt: nil)?.id, "mkv")
        XCTAssertEqual(Fassungen.datei([weg, mkv], gewaehlt: "weg")?.id, "mkv")

        // Fassungen sind keine Teile: eine Datei, die Stelle bleibt.
        let einzeln = Fassungen.teile([mkv, avi], erste: avi)
        XCTAssertFalse(einzeln.mehrere)
        XCTAssertEqual(einzeln.stelle(300), FilmTeile.Stelle(teil: 0, vorher: 0, imTeil: 300))
        XCTAssertNil(einzeln.beschriftung(0))
    }

    /// Defekte Dateien (leer, gesperrt, kaputt) sind keine Fassung und
    /// kein Standard — auch wenn ein älterer Server sie vorn schickt.
    func testDefekteDateien() throws {
        let kaputt = try datei(#"{"id":"kaputt","itemId":"m","path":"/f/k.mkv","sizeBytes":1,"partNo":1,"height":2160,"defekt":"kaputt"}"#)
        let mkv = try datei(#"{"id":"mkv","itemId":"m","path":"/f/m.mkv","sizeBytes":1,"partNo":1,"height":1080}"#)
        let avi = try datei(#"{"id":"avi","itemId":"m","path":"/f/a.avi","sizeBytes":1,"partNo":1,"height":400}"#)
        XCTAssertEqual(kaputt.defekt, "kaputt")
        XCTAssertNil(mkv.defekt)
        XCTAssertFalse(kaputt.nutzbar)
        XCTAssertTrue(mkv.nutzbar)
        XCTAssertEqual(Fassungen.liste([kaputt, mkv, avi]).map(\.id), ["mkv", "avi"])
        XCTAssertFalse(Fassungen.mehrere([kaputt, avi]))
        XCTAssertEqual(Fassungen.datei([kaputt, mkv], gewaehlt: nil)?.id, "mkv")
        XCTAssertEqual(Fassungen.datei([kaputt, mkv], gewaehlt: "kaputt")?.id, "mkv")
    }

    /// „Apocalypse Now (1979) a/b/c.avi": ein Film in drei Teilen. Der
    /// Stand zählt über alle Teile — wie im Browser.
    func testTeile() throws {
        let a = try datei(#"{"id":"a","itemId":"m","path":"/f/a.avi","sizeBytes":1,"partNo":1,"durationMs":3000000}"#)
        let b = try datei(#"{"id":"b","itemId":"m","path":"/f/b.avi","sizeBytes":1,"partNo":2,"durationMs":2000000}"#)
        let c = try datei(#"{"id":"c","itemId":"m","path":"/f/c.avi","sizeBytes":1,"partNo":3,"durationMs":1000000}"#)

        let teile = Fassungen.teile([c, a, b], erste: a)
        XCTAssertEqual(teile.dateien.map(\.id), ["a", "b", "c"])
        XCTAssertEqual(teile.gesamtSekunden, 6000)
        XCTAssertEqual(teile.stelle(0), FilmTeile.Stelle(teil: 0, vorher: 0, imTeil: 0))
        XCTAssertEqual(teile.stelle(2999), FilmTeile.Stelle(teil: 0, vorher: 0, imTeil: 2999))
        XCTAssertEqual(teile.stelle(3000), FilmTeile.Stelle(teil: 1, vorher: 3000, imTeil: 0))
        XCTAssertEqual(teile.stelle(5600), FilmTeile.Stelle(teil: 2, vorher: 5000, imTeil: 600))
        // Hinter dem Ende: von vorn.
        XCTAssertEqual(teile.stelle(5998), FilmTeile.Stelle(teil: 0, vorher: 0, imTeil: 0))
        XCTAssertEqual(teile.weiterAb(0), 3000.5)
        XCTAssertEqual(teile.stelle(teile.weiterAb(0)).teil, 1)
        XCTAssertEqual(teile.stelle(teile.weiterAb(1)).teil, 2)
        XCTAssertEqual(teile.beschriftung(1), "Teil 2 von 3")
        XCTAssertTrue(teile.istLetzter(2))
        XCTAssertFalse(teile.istLetzter(1))
        XCTAssertEqual(teile.vorher(2), 5000)

        // Fehlt Teil 2, läuft nur Teil 1; wer mit Teil 2 beginnt, spielt ihn allein.
        XCTAssertEqual(Fassungen.teile([a, c], erste: a).dateien.map(\.id), ["a"])
        XCTAssertEqual(Fassungen.teile([b], erste: b).dateien.map(\.id), ["b"])
    }

    /// „Qualität" im Player: dieselben Stufen wie im Browser und auf Android.
    func testQualitaet() {
        XCTAssertEqual(Qualitaet.stufen(fuer: 1080).map(\.id), ["1080", "720", "480", "360"])
        XCTAssertEqual(Qualitaet.stufen(fuer: 2160).count, 5)
        XCTAssertEqual(Qualitaet.stufen(fuer: 400).map(\.id), ["360"])
        XCTAssertEqual(Qualitaet.stufen(fuer: nil).count, 4)
        XCTAssertEqual(Qualitaet.wirksam("720", quellHoehe: 1080), "720")
        XCTAssertEqual(Qualitaet.wirksam("1080", quellHoehe: 400), "auto")
        XCTAssertEqual(Qualitaet.wirksam("original", quellHoehe: 400), "original")
        XCTAssertEqual(Qualitaet.wirksam(nil, quellHoehe: 1080), "auto")

        let basis = DeviceProfile(name: "Test", kind: "phone", codecs: ["h264"], containers: ["mp4", "hls"],
                                  audio: ["aac"], maxHeight: 1080, hdr: false)
        let p720 = Qualitaet.profil("720", quellHoehe: 1080, basis: basis)
        XCTAssertEqual(p720.query()["maxHeight"], "720")
        XCTAssertEqual(p720.query()["maxBitrate"], "4000000")
        XCTAssertEqual(Qualitaet.profil("auto", quellHoehe: 1080, basis: basis).query(), basis.query())
        XCTAssertNil(basis.query()["maxBitrate"])
        let original = Qualitaet.profil("original", quellHoehe: 1080, basis: basis)
        XCTAssertEqual(original.query()["maxHeight"], "4320")
        XCTAssertNil(original.query()["maxBitrate"])
    }

    /// Fassungsnamen wie im Player des Browsers: Auflösung, Tonsprache,
    /// Format, Edition — gleiche Namen mit Grösse.
    func testFassungsNamen() throws {
        let en = try datei(#"{"id":"en","itemId":"m","path":"/f/a.mkv","sizeBytes":8000000000,"partNo":1,"height":1080,"container":"mkv","tracks":[{"index":1,"kind":"audio","language":"en","channels":6}]}"#)
        let zwilling = try datei(#"{"id":"z","itemId":"m","path":"/f/b.mkv","sizeBytes":4000000000,"partNo":1,"height":1080,"container":"mkv","tracks":[{"index":1,"kind":"audio","language":"en","channels":2}]}"#)
        let dc = try datei(#"{"id":"dc","itemId":"m","path":"/f/c.mp4","sizeBytes":1,"partNo":1,"height":720,"container":"mp4","edition":"Director's Cut"}"#)
        let namen = Fassungen.namen([en, zwilling, dc])
        XCTAssertTrue(namen[0].hasPrefix("1080p · "), namen[0])
        XCTAssertTrue(namen[0].hasSuffix(" · MKV · 8 GB"), namen[0])
        XCTAssertTrue(namen[1].hasSuffix(" · MKV · 4 GB"), namen[1])
        XCTAssertEqual(namen[2], "720p · MP4 · Director's Cut")
    }

    /// Ton und Untertitel nach Sprache in die neue Fassung übernehmen.
    func testSpurenUebernehmen() throws {
        func spur(_ index: Int, _ kind: String, _ sprache: String) throws -> Track {
            try JSONDecoder.lumen.decode(Track.self, from: Data(#"{"index":\#(index),"kind":"\#(kind)","language":"\#(sprache)"}"#.utf8))
        }
        let neuTon = try [spur(0, "audio", "en"), spur(5, "audio", "de")]
        let neuText = try [spur(7, "subtitle", "en"), spur(8, "subtitle", "fr")]
        let tonEn = try spur(2, "audio", "en")
        let utEn = try spur(4, "subtitle", "en")
        let utDe = try spur(3, "subtitle", "de")
        let r1 = Spuren.uebernehmen(alterTon: tonEn, alterUntertitel: nil, untertitelAus: true,
                                    ton: neuTon, text: neuText, vorbelegt: (ton: 5, untertitel: 8))
        XCTAssertEqual(r1.ton, 0)
        XCTAssertNil(r1.untertitel)
        let r2 = Spuren.uebernehmen(alterTon: nil, alterUntertitel: utEn, untertitelAus: false,
                                    ton: neuTon, text: neuText, vorbelegt: (ton: nil, untertitel: nil))
        XCTAssertEqual(r2.untertitel, 7)
        let r3 = Spuren.uebernehmen(alterTon: nil, alterUntertitel: utDe, untertitelAus: false,
                                    ton: neuTon, text: neuText, vorbelegt: (ton: nil, untertitel: 8))
        XCTAssertEqual(r3.untertitel, 8)
    }

    /// Specials ohne eigene Nummer (Staffel 0 ab Folge 1000) heissen
    /// „Special"; echte S00Exx behalten ihre Nummer.
    func testSpecialOhneNummer() throws {
        func folge(_ staffel: Int, _ nr: Int) throws -> Item {
            let json = #"{"id":"e","libraryId":"lib_1","kind":"episode","title":"The Last Watch","seasonNo":\#(staffel),"episodeNo":\#(nr),"matchState":"matched","addedAt":"2026-01-01T00:00:00Z"}"#
            return try JSONDecoder.lumen.decode(Item.self, from: Data(json.utf8))
        }
        let special = try folge(0, 1001)
        XCTAssertTrue(special.specialOhneNummer)
        XCTAssertEqual(special.folgenNummer, "")
        XCTAssertEqual(special.folgenListenName, "The Last Watch")
        let echt = try folge(0, 2)
        XCTAssertFalse(echt.specialOhneNummer)
        XCTAssertEqual(echt.folgenListenName, "2. The Last Watch")
        XCTAssertFalse(try folge(1, 1001).specialOhneNummer)
    }
}
