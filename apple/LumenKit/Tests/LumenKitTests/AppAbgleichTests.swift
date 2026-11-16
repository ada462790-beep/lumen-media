// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

import XCTest
@testable import LumenKit

// Was im Browser gelöst war und die Apps nachgezogen haben: Wertungen,
// Sprachfassungen, Staffel-Leiste, Filmreihen der Startseite, Bilder mit
// Breite, Stand und Bildschlüssel.

final class AppAbgleichTests: XCTestCase {

    private func titel(_ extra: String, kind: String = "movie") throws -> Item {
        let json = #"{"id":"it_1","libraryId":"lib_1","kind":"\#(kind)","title":"X","matchState":"matched","addedAt":"2026-10-23T13:35:52+02:00"\#(extra)}"#
        return try JSONDecoder.lumen.decode(Item.self, from: Data(json.utf8))
    }

    private func datei(_ json: String) throws -> MediaFile {
        try JSONDecoder.lumen.decode(MediaFile.self, from: Data(json.utf8))
    }

    // MARK: Wertungen

    /// Genau eine Nachkommastelle: 6.8 statt 6.799, 7.0 statt 7.
    func testNoteMitEinerNachkommastelle() {
        XCTAssertEqual(Wertungen.note(6.799), Sprache.zahl(6.8, stellen: 1))
        let sieben = Wertungen.note(7)
        XCTAssertEqual(sieben.count, 3, sieben)
        XCTAssertTrue(sieben.hasPrefix("7") && sieben.hasSuffix("0"), sieben)
    }

    /// Rotten Tomatoes: ab 60 % frisch, darunter faul — nach dem echten Wert.
    func testRottenTomatoesSchwelle() throws {
        XCTAssertTrue(Wertungen.frisch(60))
        XCTAssertFalse(Wertungen.frisch(59))
        let faul = try titel(#","rottenTomatoes":42"#)
        let rt = try XCTUnwrap(Wertungen.liste(faul).first)
        XCTAssertEqual(rt.quelle, .rottenTomatoes)
        XCTAssertEqual(rt.frisch, false)
        XCTAssertEqual(rt.wert, "42 %")
        let frisch = try titel(#","rottenTomatoes":93"#)
        XCTAssertEqual(Wertungen.liste(frisch).first?.frisch, true)
    }

    /// IMDb mit Stimmen und Link, TMDb mit Link; Reihenfolge wie im Browser.
    func testWertungsReihe() throws {
        let item = try titel(#","imdbRating":7.849,"imdbVotes":12345,"rottenTomatoes":80,"tmdbRating":75"#, kind: "series")
        let links = [TitelLink(name: "IMDb", url: "https://www.imdb.com/title/tt1/"),
                     TitelLink(name: "TheMovieDb", url: "https://www.themoviedb.org/tv/1"),
                     TitelLink(name: "Böse", url: "javascript:alert(1)")]
        let liste = Wertungen.liste(item, links: links)
        XCTAssertEqual(liste.map(\.quelle), [.imdb, .rottenTomatoes, .tmdb])
        XCTAssertEqual(liste[0].wert, Sprache.zahl(7.8, stellen: 1))
        XCTAssertEqual(liste[0].zusatz, Sprache.zahl(12345))
        XCTAssertEqual(liste[0].link?.absoluteString, "https://www.imdb.com/title/tt1/")
        XCTAssertNil(liste[1].link)
        XCTAssertEqual(liste[2].link?.absoluteString, "https://www.themoviedb.org/tv/1")
        XCTAssertNil(TitelLink(name: "x", url: "http://unsicher").adresse)
        XCTAssertEqual(Wertungen.kurz(item), "★ " + Sprache.zahl(7.8, stellen: 1))
    }

    /// Eine Note aus einer NFO ohne Quelle steht neutral da, ohne Link.
    func testNeutraleNote() throws {
        let json = #"{"item":{"id":"it_1","libraryId":"lib_1","kind":"movie","title":"X","matchState":"matched","addedAt":"2026-10-23T13:35:52+02:00","imdbRating":7.4},"files":[],"bewertungQuelle":"nfo","links":[{"name":"IMDb","url":"https://www.imdb.com/title/tt1/"}]}"#
        let detail = try JSONDecoder.lumen.decode(ItemDetail.self, from: Data(json.utf8))
        XCTAssertEqual(detail.wertungen.map(\.quelle), [.neutral])
        XCTAssertNil(detail.wertungen.first?.link)
        // Ein älterer Server ohne die neuen Felder: wie bisher.
        let alt = try JSONDecoder.lumen.decode(ItemDetail.self, from: Data(
            #"{"item":{"id":"it_1","libraryId":"lib_1","kind":"movie","title":"X","matchState":"matched","addedAt":"2026-10-23T13:35:52+02:00"},"files":[]}"#.utf8))
        XCTAssertTrue(alt.wertungen.isEmpty)
        XCTAssertNil(alt.links)
    }

    // MARK: Sprachfassungen

    /// Police Academy: deutsche und englische Fassung als eigene Dateien —
    /// ein Film, zwei Sprachen, beide wählbar und verschieden beschriftet.
    func testSprachfassungen() throws {
        let de = try datei(#"{"id":"de","itemId":"m","path":"/f/_Police Academy/Police Academy (1984) (de).mkv","sizeBytes":4000000000,"partNo":1,"height":800,"width":1920,"container":"mkv","tracks":[{"index":1,"kind":"audio","language":"de"}]}"#)
        let en = try datei(#"{"id":"en","itemId":"m","path":"/f/_Police Academy/Police Academy (1984) (en).mkv","sizeBytes":4100000000,"partNo":1,"height":800,"width":1920,"container":"mkv","tracks":[{"index":1,"kind":"audio","language":"en"}]}"#)
        let fassungen = Fassungen.liste([de, en])
        XCTAssertEqual(fassungen.count, 2)
        let namen = Fassungen.namen(fassungen)
        XCTAssertNotEqual(namen[0], namen[1])
        XCTAssertTrue(namen[0].hasPrefix("1080p · "), "Breitformat zählt nach der Breite: \(namen[0])")
        XCTAssertTrue(namen[0].contains(Spuren.sprachName("de")), namen[0])
        XCTAssertTrue(namen[1].contains(Spuren.sprachName("en")), namen[1])
        // Nur die Sprache verschieden: „2 Sprachen", nicht „2 Qualitäten".
        let hinweis = try XCTUnwrap(Fassungen.hinweis(fassungen))
        XCTAssertTrue(hinweis.hasPrefix("2 "), hinweis)
        XCTAssertNotEqual(hinweis, Fassungen.hinweis([de, try datei(#"{"id":"x","itemId":"m","path":"/f/x.avi","sizeBytes":1,"partNo":1,"height":400,"container":"mkv","tracks":[{"index":1,"kind":"audio","language":"de"}]}"#)]))
        XCTAssertNil(Fassungen.hinweis([de]))
    }

    /// Alle Tonsprachen einer Datei, und gleiche Kopien nach dem Ordner.
    func testFassungsNamenAlleSprachenUndOrdner() throws {
        let zwei = try datei(#"{"id":"a","itemId":"m","path":"/f/A/x.mkv","sizeBytes":5,"partNo":1,"height":1080,"container":"mkv","tracks":[{"index":1,"kind":"audio","language":"de"},{"index":2,"kind":"audio","language":"en"},{"index":3,"kind":"audio","language":"de"}]}"#)
        let kopie = try datei(#"{"id":"b","itemId":"m","path":"/f/B/x.mkv","sizeBytes":5,"partNo":1,"height":1080,"container":"mkv","tracks":[{"index":1,"kind":"audio","language":"de"},{"index":2,"kind":"audio","language":"en"}]}"#)
        XCTAssertEqual(Fassungen.tonsprachen(zwei), ["de", "en"])
        let namen = Fassungen.namen([zwei, kopie])
        XCTAssertTrue(namen[0].hasSuffix(" · A"), namen[0])
        XCTAssertTrue(namen[1].hasSuffix(" · B"), namen[1])
        XCTAssertEqual(Fassungen.aufloesung(hoehe: 536, breite: 1280), "720p")
        XCTAssertEqual(Fassungen.aufloesung(hoehe: 1600, breite: 3840), "4K")
    }

    // MARK: Staffeln

    func testStaffelleiste() throws {
        func staffel(_ nr: Int?, _ titel: String = "Staffel") throws -> Item {
            let no = nr.map { #","seasonNo":\#($0)"# } ?? ""
            return try JSONDecoder.lumen.decode(Item.self, from: Data(
                #"{"id":"s\#(nr ?? -1)","libraryId":"lib_1","kind":"season","title":"\#(titel)","matchState":"matched","addedAt":"2026-10-23T13:35:52+02:00"\#(no)}"#.utf8))
        }
        // Specials: Staffel 0 und Staffel ohne Nummer (season_no NULL).
        XCTAssertTrue(Staffelleiste.istSpecial(try staffel(0)))
        XCTAssertTrue(Staffelleiste.istSpecial(try staffel(nil)))
        XCTAssertFalse(Staffelleiste.istSpecial(try staffel(1)))

        let wenige = try [staffel(1, "Staffel 1"), staffel(2, "Staffel 2"), staffel(nil, "Specials")]
        XCTAssertEqual(Staffelleiste.art(wenige), .kapseln)
        let geordnet = Titelseite.staffeln(wenige)
        XCTAssertEqual(geordnet.map(\.seasonNo), [1, 2, nil], "Specials hinten, aber da")
        XCTAssertEqual(Staffelleiste.knopf(wenige[0], art: .kapseln), "Staffel 1")

        let viele = try (1...8).map { try staffel($0) } + [staffel(0)]
        XCTAssertEqual(Staffelleiste.art(viele), .zahlen, "Specials zählen nicht mit")
        XCTAssertEqual(Staffelleiste.knopf(viele[4], art: .zahlen), "5")
        XCTAssertEqual(Staffelleiste.knopf(viele[8], art: .zahlen), Staffelleiste.name(viele[8]))

        let sehrViele = try (1...16).map { try staffel($0) }
        XCTAssertEqual(Staffelleiste.art(sehrViele), .auswahl)
        XCTAssertEqual(Staffelleiste.art([]), .kapseln)
    }

    // MARK: Bilder

    /// Stand (`?v=`) aus der Adresse am Titel, Breite als Stufe, der
    /// Bildschlüssel statt des wechselnden Medienschlüssels.
    func testBildadressen() async throws {
        let item = try titel(#","poster":"/api/v1/art/it_1/poster?v=abc","backdrop":"/api/v1/art/it_1/backdrop""#)
        XCTAssertEqual(item.bildStand("poster"), "abc")
        XCTAssertNil(item.bildStand("backdrop"))
        XCTAssertNil(item.bildStand("frame"))

        let client = APIClient(baseURL: URL(string: "http://nas:8097")!, token: "T")
        await client.setMedienSchluessel("K")
        let ohneBild = await client.artworkURL(for: item, breite: 500)?.absoluteString
        XCTAssertEqual(ohneBild, "http://nas:8097/api/v1/art/it_1/poster?key=K&v=abc&w=500")

        await client.setBildSchluessel("b.BILD")
        let mitBild = await client.artworkURL(for: item, wide: true, breite: Bildbreite.kopf)?.absoluteString
        XCTAssertEqual(mitBild, "http://nas:8097/api/v1/art/it_1/backdrop?key=b.BILD&w=1920")
        // Die bewegte Vorschau ist ein Video: dort nie der Bildschlüssel.
        let vorschau = await client.vorschauURL(itemId: "it_1")?.absoluteString
        XCTAssertEqual(vorschau, "http://nas:8097/api/v1/art/it_1/vorschau?key=K")
        // Ein Bildschlüssel, der bald abläuft, gilt nicht mehr.
        await client.setBildSchluessel("b.ALT", gueltigBis: Date().addingTimeInterval(60))
        let alt = await client.artworkURL(itemId: "it_1")?.absoluteString
        XCTAssertEqual(alt, "http://nas:8097/api/v1/art/it_1/poster?key=K")
        // Mit dem Token fällt auch der Bildschlüssel.
        await client.setBildSchluessel("b.BILD")
        await client.setToken("ANDERS")
        let weg = await client.bildSchluessel
        XCTAssertNil(weg)

        XCTAssertEqual(Bildbreite.stufe(300), 342)
        XCTAssertEqual(Bildbreite.stufe(5000), 1920)
        XCTAssertTrue(APIClient.istBildWeg("/api/v1/art/it_1/poster"))
        XCTAssertTrue(APIClient.istBildWeg("/api/v1/persons/Tom%20Hanks/bild"))
        XCTAssertFalse(APIClient.istBildWeg("/api/v1/stream/f_1/hls/main.m3u8"))
    }

    func testMedienschluesselMitBildschluessel() throws {
        let k = try JSONDecoder.lumen.decode(Medienschluessel.self, from: Data(
            #"{"key":"abc","gueltigBis":"2026-11-03T18:00:00+01:00","gueltigSek":21600,"bildKey":"b.xyz","bildGueltigSek":604800}"#.utf8))
        XCTAssertEqual(k.bildKey, "b.xyz")
        XCTAssertEqual(k.bildGueltigSek, 604800)
    }

    // MARK: Startseite

    func testStartseiteOhneWiederholung() throws {
        func t(_ id: String, neu: Bool = false) throws -> Item {
            try JSONDecoder.lumen.decode(Item.self, from: Data(
                #"{"id":"\#(id)","libraryId":"lib_1","kind":"movie","title":"T","neu":\#(neu),"matchState":"matched","addedAt":"2026-10-23T13:35:52+02:00"}"#.utf8))
        }
        func reihe(_ key: String, _ ids: [String], wide: Bool = false, neu: Bool = false) throws -> HomeRow {
            let items = try ids.map { try t($0, neu: neu) }
            return HomeRow(key: key, title: key, wide: wide, items: items)
        }
        let acht = (1...8).map { "m\($0)" }
        let rows = try [reihe("resume", ["m1"], wide: true),
                        reihe("new", acht, neu: true),
                        reihe("top", acht),
                        reihe("drama", acht + ["x1", "x2", "x3", "x4"]),
                        reihe("leer", [])]
        let fertig = Startseite.aufbereitet(rows)
        XCTAssertEqual(fertig.map(\.key), ["resume", "new", "drama"], "„top“ wiederholt nur, leere fällt weg")
        XCTAssertTrue(fertig[1].items.allSatisfy { $0.neu != true }, "in „Neu dazugekommen“ keine Plakette")
        let wenigNeu = try HomeRow(key: "x", title: "x", wide: false, items: [t("a", neu: true), t("b"), t("c"), t("d")])
        XCTAssertEqual(Startseite.neuGedaempft(wenigNeu).filter { $0.neu == true }.count, 1)
    }

    // MARK: Folgen davor

    func testDavorOffen() throws {
        func folge(_ id: String, _ staffel: Int, _ nr: Int, gesehen: Bool) throws -> Item {
            try JSONDecoder.lumen.decode(Item.self, from: Data(
                #"{"id":"\#(id)","libraryId":"lib_1","kind":"episode","title":"F","seasonNo":\#(staffel),"episodeNo":\#(nr),"finished":\#(gesehen),"matchState":"matched","addedAt":"2026-10-23T13:35:52+02:00"}"#.utf8))
        }
        let folgen = try [folge("a", 1, 1, gesehen: false), folge("b", 1, 2, gesehen: true), folge("c", 1, 3, gesehen: false)]
        XCTAssertTrue(Titelseite.davorOffen(folgen[2], in: folgen))
        XCTAssertFalse(Titelseite.davorOffen(folgen[0], in: folgen))
        let special = try folge("s", 0, 5, gesehen: false)
        XCTAssertFalse(Titelseite.davorOffen(special, in: folgen + [special]), "Specials fragen nie")
    }

    // MARK: Heimnetz

    func testNetzStand() throws {
        let n = try JSONDecoder.lumen.decode(NetzStand.self, from: Data(
            #"{"geraet":{"adresse":"172.17.0.1","zuHause":false,"grund":"gateway"},"container":true,"hostNetz":false,"bruecke":true,"festgelegt":false}"#.utf8))
        XCTAssertFalse(n.geraet.zuHause)
        XCTAssertTrue(n.dockerVerdeckt)
        XCTAssertFalse(n.grundText.isEmpty)
        let zuHause = try JSONDecoder.lumen.decode(NetzStand.self, from: Data(
            #"{"geraet":{"adresse":"192.168.1.5","zuHause":true,"grund":"privat"},"hostNetz":true}"#.utf8))
        XCTAssertFalse(zuHause.dockerVerdeckt)
    }

    // MARK: Meldungen

    func testMeldungBeginntGross() {
        XCTAssertEqual(Session.grossAnfang("nicht gefunden"), "Nicht gefunden")
        XCTAssertEqual(Session.grossAnfang(""), "")
        XCTAssertEqual(Session.text(LumenError.server("über 9000", 500)), "Über 9000")
    }
}
