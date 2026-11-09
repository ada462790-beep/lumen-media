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
}
