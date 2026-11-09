// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

import XCTest
@testable import LumenKit

// Diese Tests laufen ohne Server und ohne Gerät: `swift test` im Ordner
// apple/LumenKit genügt. Sie prüfen genau die Stellen, an denen eine App
// still kaputtgeht — Datumsformate, Adressen, Feldnamen.

final class DecodingTests: XCTestCase {

    /// Go schreibt Zeiten mal mit Bruchteilen von Sekunden, mal ohne.
    /// Die eingebaute ISO-8601-Behandlung kann nur eine der beiden Formen.
    /// Ginge das schief, wäre *jede* Antwort unlesbar — darum zuerst.
    func testBothTimeFormatsFromGo() throws {
        let withFraction = #"{"at":"2026-10-23T13:35:52.837650058+02:00","level":"info","area":"scan","message":"ok"}"#
        let without = #"{"at":"2026-10-23T13:35:52+02:00","level":"info","area":"scan","message":"ok"}"#

        let a = try JSONDecoder.lumen.decode(LogLine.self, from: Data(withFraction.utf8))
        let b = try JSONDecoder.lumen.decode(LogLine.self, from: Data(without.utf8))

        XCTAssertEqual(a.at.timeIntervalSince1970, b.at.timeIntervalSince1970, accuracy: 1.0)
        XCTAssertEqual(a.message, "ok")
    }

    func testUnknownTimeFormatFailsClearly() {
        let bad = #"{"at":"gestern","level":"info","area":"x","message":"y"}"#
        XCTAssertThrowsError(try JSONDecoder.lumen.decode(LogLine.self, from: Data(bad.utf8)))
    }

    /// Ein Titel, wie ihn der Server wirklich schickt — mit allem, was
    /// „omitempty" weglassen kann.
    func testItemWithEverythingMissing() throws {
        let minimal = """
        {"id":"it_1","libraryId":"lib_1","kind":"movie","title":"Nordlicht",
         "matchState":"matched","addedAt":"2026-10-23T13:35:52.1+02:00"}
        """
        let item = try JSONDecoder.lumen.decode(Item.self, from: Data(minimal.utf8))
        XCTAssertEqual(item.title, "Nordlicht")
        XCTAssertNil(item.year)
        XCTAssertNil(item.genres)
        XCTAssertFalse(item.isSeen)
        XCTAssertFalse(item.isOnList)
        XCTAssertEqual(item.percent, 0)
    }

    func testItemWithPersonalState() throws {
        let full = """
        {"id":"it_1","libraryId":"lib_1","kind":"movie","title":"Die Bucht",
         "year":2019,"runtimeMs":7080000,"ageYears":16,"genres":["Drama","Mystery"],
         "imdbRating":7.8,"imdbVotes":48213,"matchState":"matched",
         "addedAt":"2026-10-23T13:35:52.1+02:00",
         "progress":42.5,"finished":false,"myRating":4,"onList":true,"resumeMs":1800000,
         "poster":"/api/v1/art/it_1/poster"}
        """
        let item = try JSONDecoder.lumen.decode(Item.self, from: Data(full.utf8))
        XCTAssertEqual(item.percent, 42.5, accuracy: 0.01)
        XCTAssertTrue(item.isOnList)
        XCTAssertEqual(item.myRating, 4)
        XCTAssertEqual(item.resumeSeconds, 1800, accuracy: 0.01)
        XCTAssertEqual(item.subtitleLine, "2019 · 1 Std. 58 Min. · ab 16 · Drama · Mystery")
    }

    /// Was der Besitzer über eine laufende Wiedergabe sieht. Dass hier kein
    /// Titelfeld steht, ist der Kern der Zusage — der Server hat dafür einen
    /// eigenen Test, und dies ist die Gegenprobe auf der App-Seite.
    func testLiveSessionCarriesNoTitle() throws {
        let json = """
        {"id":"9f2c4e","mode":"transcode"}
        """
        let live = try JSONDecoder.lumen.decode(LiveSession.self, from: Data(json.utf8))
        XCTAssertEqual(live.mode, "transcode")

        let mirror = Mirror(reflecting: live)
        let verraeterisch = ["item", "title", "media", "content", "film", "path", "file",
                             "person", "avatar", "device", "started", "ort", "account"]
        for child in mirror.children {
            let name = (child.label ?? "").lowercased()
            for wort in verraeterisch {
                XCTAssertFalse(name.contains(wort),
                    "LiveSession.\(name) verrät womöglich den Inhalt")
            }
        }
    }

    // MARK: - Filmreihen und Listen

    /// Eine Reihe, wie `/collections` sie schickt. `overview` und `hidden`
    /// fehlen, wenn sie leer oder falsch sind.
    func testReiheWithoutOptionalFields() throws {
        let json = """
        {"id":"rh_1","name":"Die Bucht – Reihe","source":"auto","anzahl":2,
         "items":[
          {"id":"it_1","libraryId":"lib_1","kind":"movie","title":"Die Bucht","year":2019,
           "matchState":"matched","addedAt":"2026-10-23T13:35:52.1+02:00"},
          {"id":"it_2","libraryId":"lib_1","kind":"movie","title":"Die Bucht 2","year":2022,
           "matchState":"matched","addedAt":"2026-10-23T13:35:52+02:00",
           "frame":"/api/v1/art/it_2/frame"}
         ]}
        """
        let reihe = try JSONDecoder.lumen.decode(Reihe.self, from: Data(json.utf8))
        XCTAssertEqual(reihe.name, "Die Bucht – Reihe")
        XCTAssertNil(reihe.overview)
        XCTAssertFalse(reihe.isHidden)
        XCTAssertEqual(reihe.items.map(\.id), ["it_1", "it_2"], "die Reihenfolge zählt")
        XCTAssertEqual(reihe.anzahlText, "2 Filme")
        XCTAssertEqual(reihe.items[1].frame, "/api/v1/art/it_2/frame")
    }

    /// Die Titelseite eines Films trägt seine Reihe mit — und eine ohne
    /// Reihe muss weiterhin lesbar sein.
    func testDetailWithAndWithoutCollection() throws {
        let item = #"{"id":"it_1","libraryId":"lib_1","kind":"movie","title":"X","matchState":"matched","addedAt":"2026-10-23T13:35:52+02:00"}"#
        let ohne = #"{"item":\#(item),"files":[]}"#
        let mit = #"{"item":\#(item),"files":[],"collection":{"id":"rh_1","name":"X-Reihe","source":"tmdb","hidden":true,"anzahl":1,"items":[\#(item)]}}"#

        let ohneReihe = try JSONDecoder.lumen.decode(ItemDetail.self, from: Data(ohne.utf8))
        XCTAssertNil(ohneReihe.collection)
        let mitReihe = try JSONDecoder.lumen.decode(ItemDetail.self, from: Data(mit.utf8))
        let reihe = try XCTUnwrap(mitReihe.collection)
        XCTAssertEqual(reihe.source, "tmdb")
        XCTAssertTrue(reihe.isHidden)
        XCTAssertEqual(reihe.anzahlText, "1 Film")
    }

    /// Eine Liste aus der Übersicht: ohne `enthaelt`, ohne `items`, die
    /// Zeit ohne Bruchteile.
    func testListeFromOverview() throws {
        let json = """
        {"id":"pl_1","name":"Filmabend","von":{"id":"acc_2","name":"Mara","avatar":"av-b"},
         "meine":false,"geteilt":true,"gemeinsam":false,"darfAendern":false,"anzahl":3,
         "erstelltAm":"2026-10-23T13:35:52+02:00",
         "vorschau":[{"id":"it_1","libraryId":"lib_1","kind":"series","title":"Nordlicht",
                      "matchState":"matched","addedAt":"2026-10-23T13:35:52.1+02:00"}]}
        """
        let liste = try JSONDecoder.lumen.decode(Liste.self, from: Data(json.utf8))
        XCTAssertEqual(liste.von.name, "Mara")
        XCTAssertEqual(liste.von.initial, "M")
        XCTAssertFalse(liste.meine)
        XCTAssertFalse(liste.hasItem, "fehlt enthaelt, ist der Titel nicht drin")
        XCTAssertNil(liste.items)
        XCTAssertEqual(liste.vorschau?.count, 1)
        XCTAssertEqual(liste.anzahlText, "3 Titel")
        // 13:35:52 in Zürich ist 11:35:52 UTC.
        XCTAssertEqual(liste.erstelltAm.timeIntervalSince1970, 1_792_755_352, accuracy: 1)
    }

    /// Eine einzelne Liste mit Einträgen. Wer etwas hineingelegt hat, steht
    /// am Eintrag — und wo der Server es nicht weiss, fehlt das Feld ganz.
    func testListeWithItems() throws {
        let json = """
        {"id":"pl_1","name":"Weihnachten","von":{"id":"acc_1","name":"Alex","avatar":"av-a"},
         "meine":true,"geteilt":true,"gemeinsam":true,"darfAendern":true,"anzahl":2,
         "erstelltAm":"2026-10-23T13:35:52.837650058+02:00","enthaelt":true,
         "items":[
          {"id":"it_1","libraryId":"lib_1","kind":"movie","title":"A","matchState":"matched",
           "addedAt":"2026-10-23T13:35:52+02:00","hinzugefuegtVon":"Mara"},
          {"id":"it_2","libraryId":"lib_1","kind":"movie","title":"B","matchState":"matched",
           "addedAt":"2026-10-23T13:35:52+02:00"}
         ]}
        """
        let liste = try JSONDecoder.lumen.decode(Liste.self, from: Data(json.utf8))
        XCTAssertTrue(liste.hasItem)
        XCTAssertTrue(liste.darfAendern)
        XCTAssertEqual(liste.items?.first?.hinzugefuegtVon, "Mara")
        XCTAssertNil(liste.items?.last?.hinzugefuegtVon)
    }

    func testServerErrorMessageIsShownAsIs() {
        // Der Server schreibt seine Meldungen für Menschen; die App soll sie
        // nicht durch eine eigene ersetzen.
        let error = LumenError.server("du schaust schon auf 2 Geräten", 429)
        XCTAssertEqual(error.errorDescription, "du schaust schon auf 2 Geräten")
    }

    /// Ein Mitverwalter (Issue #6): nicht Besitzer, aber `isAdmin`, mit
    /// Gruppe und eigener Verwaltungssperre. Und ein Konto von einem
    /// älteren Server, der die Felder noch nicht kennt — dann zählt wie
    /// früher nur `isOwner`.
    func testAccountMitVerwaltung() throws {
        let mit = """
        {"id":"acc_1","name":"Mara","avatar":"av-b","isOwner":false,"isKid":false,
         "lockKind":"pin","disabled":false,"createdAt":"2026-10-23T13:35:52+02:00",
         "groupId":"grp_1","groupName":"Eltern","isAdmin":true,"adminLock":"totp"}
        """
        let a = try JSONDecoder.lumen.decode(Account.self, from: Data(mit.utf8))
        XCTAssertFalse(a.isOwner)
        XCTAssertTrue(a.darfVerwalten)
        XCTAssertTrue(a.hatVerwaltungssperre)
        XCTAssertEqual(a.groupName, "Eltern")

        let alt = """
        {"id":"acc_2","name":"Jo","avatar":"av-a","isOwner":true,"isKid":false,
         "lockKind":"none","disabled":false,"createdAt":"2026-10-23T13:35:52+02:00"}
        """
        let b = try JSONDecoder.lumen.decode(Account.self, from: Data(alt.utf8))
        XCTAssertTrue(b.darfVerwalten)
        XCTAssertFalse(b.hatVerwaltungssperre)
    }

    /// Die Antworten rund um die Verwaltungssperre: Entsperren bringt ein
    /// Token und seine Dauer, Setzen ein Token und die Art; der QR-Code
    /// kommt als Daten-Adresse und muss sich zu PNG-Bytes entpacken lassen.
    func testVerwaltungssperreAntworten() throws {
        let stand = try JSONDecoder.lumen.decode(Verwaltungssperre.self,
            from: Data(#"{"sperre":"pin","entsperrt":false}"#.utf8))
        XCTAssertTrue(stand.aktiv)
        XCTAssertEqual(Verwaltungssperre.name(stand.sperre), "PIN")

        let frei = try JSONDecoder.lumen.decode(Entsperrung.self,
            from: Data(#"{"token":"abc","gueltigMin":15}"#.utf8))
        XCTAssertEqual(frei.gueltigMin, 15)
        XCTAssertNil(frei.sperre)

        let gesetzt = try JSONDecoder.lumen.decode(Entsperrung.self,
            from: Data(#"{"sperre":"totp","token":"xyz"}"#.utf8))
        XCTAssertEqual(gesetzt.sperre, "totp")

        // "iVBORw0KGgo=" ist der Anfang jeder PNG-Datei.
        let totp = try JSONDecoder.lumen.decode(TOTPEinrichtung.self, from: Data("""
        {"geheimnis":"JBSWY3DPEHPK3PXP","qr":"data:image/png;base64,iVBORw0KGgo=","app":"Aegis"}
        """.utf8))
        XCTAssertEqual(totp.qrPNG?.prefix(4), Data([0x89, 0x50, 0x4E, 0x47]))

        XCTAssertEqual(LumenError.locked("pin").errorDescription, "Die Verwaltung ist gesperrt.")
    }
}

final class AddressTests: XCTestCase {

    /// Leute tippen Adressen so, wie es ihnen einfällt. Alles davon soll
    /// gehen — sonst scheitert die Einrichtung am ersten Schritt.
    func testEveryWayPeopleTypeAnAddress() {
        let faelle: [(String, String)] = [
            ("192.168.1.20",             "http://192.168.1.20:8097"),
            ("192.168.1.20:8097",        "http://192.168.1.20:8097"),
            ("lumen.local",              "http://lumen.local:8097"),
            ("  lumen.local:8097  ",     "http://lumen.local:8097"),
            ("http://nas:9000",          "http://nas:9000"),
            ("https://lumen.example.com", "https://lumen.example.com"),
            ("http://nas:8097/irgendwo", "http://nas:8097"),
        ]
        for (eingabe, erwartet) in faelle {
            XCTAssertEqual(Session.normalize(eingabe)?.absoluteString, erwartet,
                           "Eingabe: \(eingabe)")
        }
    }

    func testNonsenseIsRejected() {
        for eingabe in ["", "   ", "http://"] {
            XCTAssertNil(Session.normalize(eingabe), "Eingabe: \(eingabe)")
        }
    }
}

final class PlaybackTests: XCTestCase {

    func testClock() {
        XCTAssertEqual(Playback.clock(0), "0:00")
        XCTAssertEqual(Playback.clock(72), "1:12")
        XCTAssertEqual(Playback.clock(3600), "1:00:00")
        XCTAssertEqual(Playback.clock(5047), "1:24:07")
        XCTAssertEqual(Playback.clock(.nan), "0:00")
        XCTAssertEqual(Playback.clock(.infinity), "0:00")
    }

    func testDurationLabel() {
        XCTAssertEqual(Item.duration(ms: 7_080_000), "1 Std. 58 Min.")
        XCTAssertEqual(Item.duration(ms: 2_880_000), "48 Min.")
    }

    /// AVPlayer spielt MP4 und MOV unmittelbar ab, MKV nicht. Würde die App
    /// MKV für direkt abspielbar halten, bliebe der Bildschirm schwarz.
    func testOnlyRealDirectFormatsCountAsDirect() {
        XCTAssertTrue(DeviceProfile.playsDirectly(container: "mp4"))
        XCTAssertTrue(DeviceProfile.playsDirectly(container: "MOV"))
        XCTAssertFalse(DeviceProfile.playsDirectly(container: "mkv"))
        XCTAssertFalse(DeviceProfile.playsDirectly(container: "avi"))
        XCTAssertFalse(DeviceProfile.playsDirectly(container: nil))
    }

    func testDeviceProfileAlwaysOffersSomething() {
        let profile = DeviceProfile.current
        XCTAssertFalse(profile.codecs.isEmpty, "ohne Codec bekäme das Gerät nichts")
        XCTAssertTrue(profile.containers.contains("hls"),
                      "HLS ist der Weg für alles, was nicht direkt geht")
        XCTAssertGreaterThan(profile.maxHeight, 0)
        let query = profile.query()
        XCTAssertNotNil(query["codecs"])
        XCTAssertNotNil(query["maxHeight"])
    }
}

final class AmStueckTests: XCTestCase {

    /// Ein Titel, wie der Server ihn schickt — nur mit dem, worauf es beim
    /// Weiterspielen ankommt. Der Titel ist gleich der Kennung, damit die
    /// Prüfungen lesbar bleiben.
    private func titel(_ id: String, _ kind: String, staffel: Int? = nil,
                       folge: Int? = nil, fertig: Bool = false) throws -> Item {
        var extra = ""
        if let staffel { extra += #","seasonNo":\#(staffel)"# }
        if let folge { extra += #","episodeNo":\#(folge)"# }
        if fertig { extra += #","finished":true"# }
        let json = #"{"id":"\#(id)","libraryId":"lib_1","kind":"\#(kind)","title":"\#(id)","matchState":"matched","addedAt":"2026-10-23T13:35:52+02:00"\#(extra)}"#
        return try JSONDecoder.lumen.decode(Item.self, from: Data(json.utf8))
    }

    /// Der Server schickt Folgen nicht zwingend geordnet. Gespielt wird
    /// nach Staffel und Folge, und zwar die erste, die nicht fertig ist.
    func testErsteOffeneFolge() throws {
        let folgen = [
            try titel("s2f1", "episode", staffel: 2, folge: 1),
            try titel("s1f2", "episode", staffel: 1, folge: 2),
            try titel("s1f1", "episode", staffel: 1, folge: 1, fertig: true),
        ]
        XCTAssertEqual(AmStueck.geordnet(folgen).map(\.id), ["s1f1", "s1f2", "s2f1"])
        XCTAssertEqual(AmStueck.ersteOffene(in: folgen)?.id, "s1f2")

        // Alles gesehen: von vorn, wie bei einem Film, den man nochmals schaut.
        let gesehen = [
            try titel("b", "episode", staffel: 1, folge: 2, fertig: true),
            try titel("a", "episode", staffel: 1, folge: 1, fertig: true),
        ]
        XCTAssertEqual(AmStueck.ersteOffene(in: gesehen)?.id, "a")
        XCTAssertNil(AmStueck.ersteOffene(in: []), "ohne Folgen gibt es nichts zu spielen")
    }

    /// Die Folge danach — auch über das Ende einer Staffel hinweg.
    func testFolgeDanach() throws {
        let folgen = [
            try titel("s2f1", "episode", staffel: 2, folge: 1),
            try titel("s1f1", "episode", staffel: 1, folge: 1),
            try titel("s1f2", "episode", staffel: 1, folge: 2),
        ]
        XCTAssertEqual(AmStueck.folgeDanach("s1f1", in: folgen)?.id, "s1f2")
        XCTAssertEqual(AmStueck.folgeDanach("s1f2", in: folgen)?.id, "s2f1")
        XCTAssertNil(AmStueck.folgeDanach("s2f1", in: folgen), "nach der letzten Folge ist Schluss")
        XCTAssertNil(AmStueck.folgeDanach("gibts_nicht", in: folgen))
    }

    /// Eine Liste am Stück: Filme spielen sich selbst, eine Serie ihre
    /// erste offene Folge, eine Serie ohne Folgen und eine Staffel werden
    /// übersprungen. Die Schleife unten tut, was `Abspielfolge` tut — nur
    /// ohne Netz.
    func testListeAmStueck() throws {
        let eintraege = [
            try titel("film_a", "movie"),
            try titel("serie_x", "series"),
            try titel("serie_leer", "series"),
            try titel("staffel", "season"),
            try titel("film_b", "movie", fertig: true),
        ]
        let vorrat: [String: [Item]] = [
            "serie_x": [
                try titel("x2", "episode", staffel: 1, folge: 2),
                try titel("x1", "episode", staffel: 1, folge: 1, fertig: true),
            ],
        ]

        // Von vorn beginnt es beim ersten Eintrag.
        XCTAssertEqual(AmStueck.naechster(nach: nil, in: eintraege, folgen: [:]),
                       .spielen(index: 0, item: eintraege[0]))
        // Bei einer Serie, deren Folgen noch niemand kennt, erst laden.
        XCTAssertEqual(AmStueck.naechster(nach: 0, in: eintraege, folgen: [:]),
                       .laden(serieId: "serie_x"))

        var folgen: [String: [Item]] = [:]
        var gespielt: [String] = []
        var stand: Int?
        var runden = 0
        schleife: while runden < 20 {
            runden += 1
            switch AmStueck.naechster(nach: stand, in: eintraege, folgen: folgen) {
            case .spielen(let i, let item):
                gespielt.append(item.id)
                stand = i
            case .laden(let serieId):
                folgen[serieId] = vorrat[serieId] ?? []
            case .ende:
                break schleife
            }
        }
        // Ein gesehener Film läuft trotzdem: wer die Liste abspielt, will
        // sie ganz sehen.
        XCTAssertEqual(gespielt, ["film_a", "x2", "film_b"])
        XCTAssertLessThan(runden, 20, "die Schleife muss enden")
        XCTAssertEqual(AmStueck.naechster(nach: 4, in: eintraege, folgen: folgen), .ende)
        XCTAssertEqual(AmStueck.naechster(nach: 99, in: eintraege, folgen: folgen), .ende)
        XCTAssertEqual(AmStueck.naechster(nach: nil, in: [], folgen: [:]), .ende)
    }

    /// „Folgen automatisch weiter" ist an, solange es nicht ausdrücklich
    /// ausgeschaltet ist — genau wie im Server und im Browser.
    func testFolgenAutomatischWeiter() {
        XCTAssertTrue(AmStueck.weiterAn(nil))
        XCTAssertTrue(AmStueck.weiterAn([:]))
        XCTAssertTrue(AmStueck.weiterAn(["autoplay": "1"]))
        XCTAssertFalse(AmStueck.weiterAn(["autoplay": "0"]))
        XCTAssertTrue(AmStueck.weiterAn(["skipIntro": "0"]), "ein anderer Schalter zählt nicht")
    }

    func testAnzeigeAlsNaechstes() throws {
        let serie = try titel("Nordlicht", "series")
        let folge = try titel("Der Anfang", "episode", staffel: 1, folge: 2)
        let film = try titel("Die Bucht", "movie")
        XCTAssertEqual(AmStueck.anzeige(folge, eintrag: serie), "Nordlicht · S1 F2")
        XCTAssertEqual(AmStueck.anzeige(folge, eintrag: nil), "S1 F2 · Der Anfang")
        XCTAssertEqual(AmStueck.anzeige(film, eintrag: film), "Die Bucht")
    }
}

final class URLTests: XCTestCase {

    /// Die Adressen, die AVPlayer bekommt. Ein Tippfehler hier hiesse:
    /// nichts spielt, und die Meldung wäre ein nacktes 404.
    func testStreamURLs() async {
        let client = APIClient(baseURL: URL(string: "http://nas:8097")!, token: "GEHEIM")
        await client.setMedienSchluessel("SCHLUESSEL")

        let file = MediaFile(
            id: "f_1", itemId: "it_1", path: "/medien/x.mkv", sizeBytes: 1,
            partNo: 1, edition: nil, container: "mkv", durationMs: 7_080_000,
            bitrate: nil, width: nil, height: nil, videoCodec: "h264",
            hdr: nil, missing: nil, preferred: nil, tracks: nil, chapters: nil)

        // MKV geht nie direkt — auch wenn der Plan „direkt" sagt, kann
        // AVPlayer den Behälter nicht. Dann HLS.
        let direkt = PlaybackPlan(direct: true, reason: nil, videoCodec: "copy",
                                  audioCodec: "copy", height: 1080, bitrate: 0)
        let hls = await client.streamURL(for: file, plan: direkt).absoluteString
        XCTAssertTrue(hls.contains("/api/v1/stream/f_1/hls/master.m3u8"), hls)
        XCTAssertTrue(hls.contains("key=SCHLUESSEL"), "ohne Medienschlüssel lädt AVPlayer nichts")
        XCTAssertFalse(hls.contains("GEHEIM"), "das Token steht nie in einer Adresse")
        XCTAssertFalse(hls.contains("token="), hls)

        // MP4 mit „direkt" geht wirklich direkt.
        let mp4 = MediaFile(
            id: "f_2", itemId: "it_1", path: "/medien/x.mp4", sizeBytes: 1,
            partNo: 1, edition: nil, container: "mp4", durationMs: 1,
            bitrate: nil, width: nil, height: nil, videoCodec: "h264",
            hdr: nil, missing: nil, preferred: nil, tracks: nil, chapters: nil)
        let roh = await client.streamURL(for: mp4, plan: direkt)
        XCTAssertTrue(roh.absoluteString.contains("/api/v1/stream/f_2?"), roh.absoluteString)
        XCTAssertTrue(roh.absoluteString.contains("key=SCHLUESSEL"), roh.absoluteString)
        XCTAssertFalse(roh.absoluteString.contains("GEHEIM"), roh.absoluteString)

        // Eine andere Tonspur steckt im Strom: dann auch für MP4 über HLS,
        // mit der Spur in der Anfrage.
        let spur = await client.streamURL(for: mp4, plan: direkt, spur: 2)
        XCTAssertTrue(spur.path().contains("hls/master.m3u8"), spur.absoluteString)
        XCTAssertTrue(spur.absoluteString.contains("spur=2"), spur.absoluteString)
        XCTAssertTrue(APIClient.istHLS(spur))
        XCTAssertFalse(APIClient.istHLS(roh))

        // Nur im Pfad nachsehen, nicht in der ganzen Adresse. Die Anfrage
        // trägt die Liste der Behälter mit, die das Gerät abspielen kann,
        // und darin steht „hls" — zu Recht. Die erste Fassung dieses Tests
        // suchte in der ganzen Zeichenkette und schlug deshalb an, obwohl
        // die Adresse völlig richtig war.
        XCTAssertFalse(roh.path().contains("hls"),
                       "eine direkte Wiedergabe darf nicht über HLS laufen: \(roh.path())")
    }

    func testArtworkNeedsAToken() async {
        let ohne = APIClient(baseURL: URL(string: "http://nas:8097")!, token: nil)
        let keins = await ohne.artworkURL(itemId: "it_1")
        XCTAssertNil(keins, "ohne Anmeldung darf keine Bildadresse entstehen")

        let mit = APIClient(baseURL: URL(string: "http://nas:8097")!, token: "T")
        await mit.setMedienSchluessel("K")
        let url = await mit.artworkURL(itemId: "it_1", kind: "backdrop")?.absoluteString
        XCTAssertEqual(url, "http://nas:8097/api/v1/art/it_1/backdrop?key=K")
    }

    /// Der Medienschlüssel gehört zu genau einem Token und einem Server.
    /// Wechselt eins davon, ist er weg — sonst trüge eine Bildadresse den
    /// Schlüssel der vorigen Person.
    func testMedienschluesselFaelltMitDemToken() async {
        let client = APIClient(baseURL: URL(string: "http://nas:8097")!, token: "T")
        await client.setMedienSchluessel("K")
        var faellig = await client.medienSchluesselBaldFaellig
        XCTAssertFalse(faellig, "ein frischer Schlüssel gilt sechs Stunden")

        await client.setMedienSchluessel("K", gueltigBis: Date().addingTimeInterval(30 * 60))
        faellig = await client.medienSchluesselBaldFaellig
        XCTAssertTrue(faellig, "unter einer Stunde Restzeit wird erneuert")

        await client.setToken("ANDERS")
        let weg = await client.medienSchluessel
        XCTAssertNil(weg)
        let keinBild = await client.artworkURL(itemId: "it_1")
        XCTAssertNil(keinBild, "ohne Schlüssel keine Bildadresse — schon gar nicht mit dem Token")

        await client.setMedienSchluessel("K2")
        await client.setBaseURL(URL(string: "http://anderer:8097")!)
        let auchWeg = await client.medienSchluessel
        XCTAssertNil(auchWeg)
    }

    func testMedienschluesselAntwort() throws {
        let k = try JSONDecoder.lumen.decode(Medienschluessel.self, from: Data(
            #"{"key":"abc-_","gueltigBis":"2026-11-03T18:00:00.123456789+01:00","gueltigSek":21600}"#.utf8))
        XCTAssertEqual(k.key, "abc-_")
        XCTAssertEqual(k.gueltigSek, 21600)
        XCTAssertNotNil(k.gueltigBis)
    }

    /// Von unterwegs: Die Meldung des Servers kommt unverändert an.
    func testUnterwegsMeldung() {
        let e = LumenError.unterwegs("von unterwegs gibt es keine profilauswahl")
        XCTAssertEqual(e.errorDescription, "von unterwegs gibt es keine profilauswahl")
    }

    /// Fehlt ein Bild, springt das Standbild ein — hochkant nur für das
    /// Poster, breit vor dem Poster. Gibt es alles, bleibt es beim Alten.
    func testFrameFillsInForMissingArtwork() async throws {
        func titel(_ extra: String) throws -> Item {
            let json = #"{"id":"it_1","libraryId":"lib_1","kind":"movie","title":"X","matchState":"matched","addedAt":"2026-10-23T13:35:52+02:00"\#(extra)}"#
            return try JSONDecoder.lumen.decode(Item.self, from: Data(json.utf8))
        }
        let alles = try titel(#","poster":"/p","backdrop":"/b""#)
        XCTAssertEqual(alles.posterArt, "poster")
        XCTAssertEqual(alles.wideArt, "backdrop")

        let ohnePoster = try titel(#","backdrop":"/b","frame":"/f""#)
        XCTAssertEqual(ohnePoster.posterArt, "frame")
        XCTAssertEqual(ohnePoster.wideArt, "backdrop")

        let ohneHintergrund = try titel(#","poster":"/p","frame":"/f""#)
        XCTAssertEqual(ohneHintergrund.posterArt, "poster")
        XCTAssertEqual(ohneHintergrund.wideArt, "frame")

        // Ein älterer Server schickt kein `frame` — dann wie bisher.
        let nackt = try titel("")
        XCTAssertEqual(nackt.posterArt, "poster")
        XCTAssertEqual(nackt.wideArt, "poster")

        let client = APIClient(baseURL: URL(string: "http://nas:8097")!, token: "T")
        await client.setMedienSchluessel("K")
        let url = await client.artworkURL(for: ohnePoster)?.absoluteString
        XCTAssertEqual(url, "http://nas:8097/api/v1/art/it_1/frame?key=K",
                       "auch das Standbild braucht den Medienschlüssel")
        let breit = await client.artworkURL(for: ohneHintergrund, wide: true)?.absoluteString
        XCTAssertEqual(breit, "http://nas:8097/api/v1/art/it_1/frame?key=K")
    }

    // MARK: - Lizenz

    /// Der Server nennt die Fundstelle seines Quelltexts — §13 der AGPL. Was
    /// dort steht, trägt der Betreiber ein, und daraus macht die App einen
    /// anklickbaren Link. Also muss geprüft sein, was da steht.
    func testSourceLinkOnlyAcceptsWebAddresses() {
        XCTAssertEqual(Session.sourceLink("https://github.com/jemand/lumen")?.absoluteString,
                       "https://github.com/jemand/lumen")
        XCTAssertEqual(Session.sourceLink("http://192.168.1.9:3000/lumen.git")?.absoluteString,
                       "http://192.168.1.9:3000/lumen.git")
        XCTAssertEqual(Session.sourceLink("  https://git.example.org/x  ")?.absoluteString,
                       "https://git.example.org/x", "Leerzeichen stören nicht")

        XCTAssertNil(Session.sourceLink("javascript:alert(1)"))
        XCTAssertNil(Session.sourceLink("data:text/html,<b>x"))
        XCTAssertNil(Session.sourceLink("file:///etc/passwd"))
        XCTAssertNil(Session.sourceLink("https:///pfad"), "ohne Rechnernamen")
        XCTAssertNil(Session.sourceLink(""))
        XCTAssertNil(Session.sourceLink(nil))
    }

    // MARK: - Pinnen

    /// Der Fingerabdruck wird abgetippt oder aus der Verwaltung kopiert —
    /// mit Doppelpunkten, in Grossbuchstaben, mit Leerzeichen. Alles soll
    /// gehen, aber nichts Falsches soll durchrutschen.
    func testFingerabdruckWirdSauberGelesen() {
        let echt = "60:62:a4:8f:38:e8:ea:99:e9:62:26:ef:27:d8:f4:d1:"
                 + "eb:91:ea:94:85:17:3c:45:7e:4a:4d:03:bc:91:f2:81"
        let blank = echt.replacingOccurrences(of: ":", with: "")

        XCTAssertEqual(Pinnen.saubern(echt), blank, "Doppelpunkte fallen weg")
        XCTAssertEqual(Pinnen.saubern(echt.uppercased()), blank, "Grossbuchstaben werden klein")
        XCTAssertEqual(Pinnen.saubern("  \(echt)  "), blank, "Leerzeichen stören nicht")

        XCTAssertNil(Pinnen.saubern(nil))
        XCTAssertNil(Pinnen.saubern(""))
        XCTAssertNil(Pinnen.saubern("60:62:a4"), "zu kurz")
        XCTAssertNil(Pinnen.saubern(String(repeating: "z", count: 64)), "kein Hex")
    }

    func testVergleichMerktJedenUnterschied() {
        let a = String(repeating: "ab", count: 32)
        XCTAssertTrue(Pinnen.gleichLang(a, a))
        XCTAssertFalse(Pinnen.gleichLang(a, String(a.dropLast()) + "0"))
        XCTAssertFalse(Pinnen.gleichLang(a, String(a.dropLast(2))), "verschiedene Längen")
    }

    /// Derselbe SHA-256, den der Server anzeigt und den Kotlin berechnet —
    /// sonst passte der Fingerabdruck aus der Verwaltung nicht zur App.
    func testFingerabdruckIstEinSHA256InHex() {
        let fp = Pinnen.fingerabdruck(Data("lumen".utf8))
        XCTAssertEqual(fp.count, 64)
        XCTAssertTrue(fp.allSatisfy { $0.isHexDigit && !$0.isUppercase })
        // Derselbe Wert steht im Kotlin-Test. Weichen die beiden ab, passt
        // ein Fingerabdruck aus der Verwaltung auf einem der beiden
        // Geräte nicht — und niemand käme darauf, warum.
        //   echo -n lumen | sha256sum
        XCTAssertEqual(fp, "456f45fddc2e725c2948d03317e2262c2ec8e86745af28dc7b2221987b858b50")
    }
}

final class SpurenTests: XCTestCase {

    private func spur(_ json: String) throws -> Track {
        try JSONDecoder.lumen.decode(Track.self, from: Data(json.utf8))
    }

    /// Dieselben Namen wie im Server (spurName) und im Browser.
    func testSpurNamen() throws {
        XCTAssertEqual(try spur(#"{"index":1,"kind":"audio","language":"de","channels":6}"#).name, "Deutsch (5.1)")
        XCTAssertEqual(try spur(#"{"index":2,"kind":"audio","language":"en","channels":2}"#).name, "Englisch")
        XCTAssertEqual(try spur(#"{"index":3,"kind":"subtitle","language":"de","forced":true}"#).name,
                       "Deutsch (erzwungen)")
        XCTAssertEqual(try spur(#"{"index":4,"kind":"subtitle","language":"en","hearingImpaired":true,"title":"SDH"}"#).name,
                       "Englisch (SDH, für Hörgeschädigte)")
        XCTAssertEqual(try spur(#"{"index":1000,"kind":"subtitle","language":"und","external":true}"#).name,
                       "Unbekannt (Datei)")
        XCTAssertEqual(try spur(#"{"index":5,"kind":"subtitle","language":"fi"}"#).name, "FI")
        // Ein Titel, der nur die Sprache wiederholt, steht nicht doppelt da.
        XCTAssertEqual(try spur(#"{"index":6,"kind":"audio","language":"de","title":"deutsch"}"#).name, "Deutsch")
    }

    /// Bild-Untertitel bleiben weg; die Aufteilung nach Art stimmt.
    func testTextspurenOhneBitmap() throws {
        let json = """
        {"id":"f_1","itemId":"it_1","path":"/x.mkv","sizeBytes":1,"partNo":1,
         "tracks":[{"index":1,"kind":"audio","language":"de"},
                   {"index":2,"kind":"subtitle","language":"de"},
                   {"index":3,"kind":"subtitle","language":"en","codec":"hdmv_pgs_subtitle","bitmap":true}]}
        """
        let file = try JSONDecoder.lumen.decode(MediaFile.self, from: Data(json.utf8))
        XCTAssertEqual(file.tonspuren.map(\.index), [1])
        XCTAssertEqual(file.textspuren.map(\.index), [2], "PGS lässt sich nicht als Text zeigen")
    }

    /// Vorbelegung nach den gemerkten Sprachen — und erzwungene Untertitel
    /// in der Tonsprache, wenn niemand Untertitel will.
    func testVorbelegung() throws {
        let ton = [try spur(#"{"index":1,"kind":"audio","language":"en"}"#),
                   try spur(#"{"index":2,"kind":"audio","language":"de"}"#)]
        let text = [try spur(#"{"index":3,"kind":"subtitle","language":"de","forced":true}"#),
                    try spur(#"{"index":4,"kind":"subtitle","language":"de"}"#),
                    try spur(#"{"index":5,"kind":"subtitle","language":"en"}"#)]

        // „Aus" heisst: keine vollen Untertitel. Erzwungene in der Tonsprache
        // laufen trotzdem mit — dafür sind sie da (fremdsprachige Sätze
        // im Film). So macht es auch der Browser.
        let deutsch = Spuren.vorbelegung(ton: ton, text: text, prefs: ["tonSprache": "de", "untertitel": "aus"])
        XCTAssertEqual(deutsch.ton, 2)
        XCTAssertEqual(deutsch.untertitel, 3)

        // Englischer Ton, keine Wahl bei den Untertiteln: die erzwungenen
        // laufen nicht mit, denn sie sind deutsch — nicht die Tonsprache.
        let englisch = Spuren.vorbelegung(ton: ton, text: text, prefs: ["tonSprache": "en"])
        XCTAssertEqual(englisch.ton, 1)
        XCTAssertNil(englisch.untertitel)

        // Deutscher Ton ohne Untertitelwunsch: die erzwungenen deutschen laufen mit.
        let erzwungen = Spuren.vorbelegung(ton: ton, text: text, prefs: ["tonSprache": "de"])
        XCTAssertEqual(erzwungen.untertitel, 3)

        // Untertitel gewünscht: die volle Spur, nicht die erzwungene.
        let voll = Spuren.vorbelegung(ton: ton, text: text, prefs: ["untertitel": "de"])
        XCTAssertNil(voll.ton, "keine Tonsprache gemerkt: es bleibt bei der ersten")
        XCTAssertEqual(voll.untertitel, 4)

        // Eine Sprache, die es nicht gibt, ändert nichts.
        let fremd = Spuren.vorbelegung(ton: ton, text: text, prefs: ["tonSprache": "ja", "untertitel": "ja"])
        XCTAssertNil(fremd.ton)
        XCTAssertNil(fremd.untertitel)
    }

    /// WebVTT lesen: Kopf, Kennungen, Einstellungen, Tags, Windows-Zeilen.
    func testVTTLesen() {
        let vtt = "\u{FEFF}WEBVTT\r\n\r\n1\r\n00:00:01.000 --> 00:00:02.500 align:start\r\nHallo <i>Welt</i>\r\n\r\n"
            + "00:03.000 --> 00:04.000\nZwei&amp;\nZeilen\n\n"
            + "NOTE ein Kommentar\n\n"
            + "00:00:05,000 --> 00:00:04,000\nrückwärts\n"
        let cues = Untertitel.lesen(vtt)
        XCTAssertEqual(cues.count, 2)
        XCTAssertEqual(cues[0].von, 1.0)
        XCTAssertEqual(cues[0].bis, 2.5)
        XCTAssertEqual(cues[0].text, "Hallo Welt")
        XCTAssertEqual(cues[1].von, 3.0)
        XCTAssertEqual(cues[1].text, "Zwei&\nZeilen")

        XCTAssertEqual(Untertitel.text(at: 1.5, in: cues), "Hallo Welt")
        XCTAssertNil(Untertitel.text(at: 2.7, in: cues))
        XCTAssertNil(Untertitel.text(at: 0, in: []))
    }

    func testVTTZeiten() {
        XCTAssertEqual(Untertitel.zeit("01:02:03.500"), 3723.5)
        XCTAssertEqual(Untertitel.zeit(" 02:03.250 "), 123.25)
        XCTAssertEqual(Untertitel.zeit("00:00:01,000"), 1.0)
        XCTAssertNil(Untertitel.zeit("gestern"))
        XCTAssertNil(Untertitel.zeit("1:2:3:4"))
    }

    /// Vorspann und Abspann aus der Antwort zu einer Folge.
    func testVorspannAusDerAntwort() throws {
        let item = #"{"id":"ep_1","libraryId":"lib_1","kind":"episode","title":"X","matchState":"matched","addedAt":"2026-10-23T13:35:52+02:00"}"#
        let json = #"{"item":\#(item),"files":[],"intro":{"vonMs":5000,"bisMs":65000,"quelle":"serie"},"creditsMs":2400000,"seriesId":"se_1"}"#
        let detail = try JSONDecoder.lumen.decode(ItemDetail.self, from: Data(json.utf8))
        let intro = try XCTUnwrap(detail.intro)
        XCTAssertTrue(intro.vonSerie)
        XCTAssertFalse(intro.enthaelt(5.2), "die ersten Augenblicke zählen nicht")
        XCTAssertTrue(intro.enthaelt(30))
        XCTAssertFalse(intro.enthaelt(64.5), "kurz vor dem Ende auch nicht mehr")
        XCTAssertEqual(detail.creditsMs, 2_400_000)
        XCTAssertEqual(detail.seriesId, "se_1")

        // Ein Film ohne all das — wie von einem älteren Server.
        let ohne = try JSONDecoder.lumen.decode(ItemDetail.self, from: Data(#"{"item":\#(item),"files":[]}"#.utf8))
        XCTAssertNil(ohne.intro)
        XCTAssertNil(ohne.creditsMs)
    }
}

final class SucheTests: XCTestCase {

    func testSuchergebnis() throws {
        let json = """
        {"items":[],"personen":[{"name":"Tom Hanks","anzahl":3,"itemId":"it_1","bild":"/api/v1/art/it_1/cast02"}],
         "genres":["Komödie"],"deutung":{"person":"Tom Hanks","jahrVon":1990,"jahrBis":1999}}
        """
        let e = try JSONDecoder.lumen.decode(Suchergebnis.self, from: Data(json.utf8))
        XCTAssertFalse(e.leer)
        XCTAssertEqual(e.personen?.first?.initial, "T")
        XCTAssertEqual(e.personen?.first?.anzahlText, "3 Titel")
        XCTAssertEqual(e.deutung?.beschreibung, "mit Tom Hanks · 1990–1999")
        XCTAssertEqual(e.trefferzeile, "0 Treffer · mit Tom Hanks · 1990–1999")

        // Leere Eingabe: der Server schickt nur die drei Listen.
        let leer = try JSONDecoder.lumen.decode(Suchergebnis.self, from: Data(#"{"items":[],"personen":[],"genres":[]}"#.utf8))
        XCTAssertTrue(leer.leer)
        XCTAssertNil(leer.deutung)
        XCTAssertEqual(leer.trefferzeile, "0 Treffer")

        let einJahr = Deutung(text: "bucht", genre: "Drama", person: nil, jahrVon: 2019, jahrBis: 2019)
        XCTAssertEqual(einJahr.beschreibung, "Drama · 2019 · „bucht“")
    }

    /// Der Stand einer Serie — nur eigene Folgen, nur für dieses Profil.
    func testSerienStand() throws {
        func serie(_ extra: String) throws -> Item {
            let json = #"{"id":"se_1","libraryId":"lib_1","kind":"series","title":"X","matchState":"matched","addedAt":"2026-10-23T13:35:52+02:00"\#(extra)}"#
            return try JSONDecoder.lumen.decode(Item.self, from: Data(json.utf8))
        }
        XCTAssertEqual(try serie(#","gesehen":4,"folgen":10"#).folgenStand, "4 von 10 Folgen")
        XCTAssertEqual(try serie(#","gesehen":4,"folgen":10"#).folgenAnteil, 40)
        XCTAssertEqual(try serie(#","gesehen":10,"folgen":10,"finished":true"#).folgenStand, "alle 10 Folgen gesehen")
        XCTAssertNil(try serie(#","folgen":10"#).folgenStand, "noch nichts gesehen: nichts zu sagen")
        XCTAssertNil(try serie("").folgenStand, "ein älterer Server schickt keinen Stand")
    }
}

// MARK: - Abzeichen, Technik, Neuigkeiten, Profilbilder

final class NeuheitenTests: XCTestCase {

    private func titel(_ extra: String) throws -> Item {
        let json = """
        {"id":"it_1","libraryId":"lib_1","kind":"series","title":"Nordlicht",
         "matchState":"matched","addedAt":"2026-10-23T13:35:52+02:00"\(extra)}
        """
        return try JSONDecoder.lumen.decode(Item.self, from: Data(json.utf8))
    }

    /// „Neu" schlägt „3 neue Folgen" — eine ganz neue Serie ist neu, wie im
    /// Browser. Rechts nur 4K und HDR, nie HD.
    func testAbzeichen() throws {
        let neu = try titel(#","neu":true,"neueFolgen":12,"qualitaet":"4K","hdr":"DV""#)
        XCTAssertEqual(neu.abzeichenNeu, "Neu")
        XCTAssertEqual(neu.abzeichenTechnik, ["4K", "Dolby Vision"])

        let folgen = try titel(#","neueFolgen":3,"qualitaet":"HD","hdr":"HDR10""#)
        XCTAssertEqual(folgen.abzeichenNeu, "3 neue Folgen")
        XCTAssertEqual(folgen.abzeichenTechnik, ["HDR10"])

        XCTAssertEqual(try titel(#","neueFolgen":1"#).abzeichenNeu, "Neue Folge")
        let nichts = try titel("")
        XCTAssertNil(nichts.abzeichenNeu)
        XCTAssertEqual(nichts.abzeichenTechnik, [])
    }

    func testTechnikZeile() throws {
        let item = try titel(#","qualitaet":"4K","hdr":"DV","kanaele":6"#)
        let datei = """
        {"id":"f1","itemId":"it_1","path":"/m/a.mkv","sizeBytes":1,"partNo":1,"height":2160,
         "tracks":[{"index":1,"kind":"audio","language":"de","channels":6},
                   {"index":2,"kind":"audio","language":"en","channels":2},
                   {"index":3,"kind":"audio","language":"de","channels":2},
                   {"index":4,"kind":"subtitle","language":"de"},
                   {"index":5,"kind":"subtitle","language":"en","bitmap":true}]}
        """
        let file = try JSONDecoder.lumen.decode(MediaFile.self, from: Data(datei.utf8))
        let plan = try JSONDecoder.lumen.decode(PlaybackPlan.self, from: Data(
            #"{"direct":true,"videoCodec":"hevc","audioCodec":"eac3","height":2160,"bitrate":0}"#.utf8))

        let zeile = Technik.zeile(item: item, file: file, plan: plan).map(\.text)
        XCTAssertEqual(zeile, ["4K", "Dolby Vision", "5.1", "DE · EN", "Untertitel DE", "Direkt abspielbar"])

        // Ohne Datei und ohne Angaben: nichts.
        XCTAssertEqual(Technik.zeile(item: try titel(""), file: nil, plan: nil), [])
        // Nur am Titel: was der Server zusammengefasst hat.
        XCTAssertEqual(Technik.zeile(item: try titel(#","qualitaet":"HD","kanaele":2"#), file: nil, plan: nil)
                        .map(\.text), ["HD", "Stereo"])
        XCTAssertEqual(Technik.qualitaet(hoehe: 1080), "HD")
        XCTAssertEqual(Technik.qualitaet(hoehe: 2160), "4K")
        XCTAssertEqual(Technik.qualitaet(hoehe: 576), "SD")
        XCTAssertNil(Technik.qualitaet(hoehe: 0))
        XCTAssertEqual(Technik.tonName(kanaele: 8), "7.1")
        XCTAssertNil(Technik.tonName(kanaele: 1))
    }

    func testUntertitelStil() {
        XCTAssertEqual(UntertitelStil(prefs: nil), UntertitelStil(groesse: .normal, hintergrund: true))
        XCTAssertEqual(UntertitelStil(prefs: ["utGroesse": "gross", "utHintergrund": "0"]),
                       UntertitelStil(groesse: .gross, hintergrund: false))
        XCTAssertEqual(UntertitelStil(prefs: ["utGroesse": "riesig", "utHintergrund": "1"]).groesse, .normal)
        XCTAssertLessThan(UntertitelStil.Groesse.klein.faktor, UntertitelStil.Groesse.gross.faktor)
    }

    /// Die Nullzeit von Go heisst „nie gelesen" — und darf die Antwort nicht
    /// unlesbar machen.
    func testNeuigkeiten() throws {
        let nie = """
        {"eintraege":[{"art":"folgen","itemId":"it_1","titel":"Nordlicht","text":"2 neue Folgen",
                       "at":"2026-10-28T10:00:00Z","anzahl":2},
                      {"art":"neu","titel":"12 neue Titel","text":"diese Woche","at":"2026-10-27T10:00:00Z"}],
         "ungelesen":2,"gelesenBis":"0001-01-01T00:00:00Z"}
        """
        let a = try JSONDecoder.lumen.decode(Neuigkeiten.self, from: Data(nie.utf8))
        XCTAssertEqual(a.eintraege.count, 2)
        XCTAssertNil(a.gelesenBis)
        XCTAssertTrue(a.eintraege[0].istNeu(gelesenBis: a.gelesenBis))
        XCTAssertEqual(a.eintraege[0].symbol, "tv")
        XCTAssertNil(a.eintraege[1].itemId)

        let gelesen = """
        {"eintraege":[{"art":"reihe","itemId":"it_2","titel":"Dune","text":"Neu in der Reihe",
                       "at":"2026-10-20T10:00:00Z"}],
         "ungelesen":0,"gelesenBis":"2026-10-25T10:00:00.5+02:00"}
        """
        let b = try JSONDecoder.lumen.decode(Neuigkeiten.self, from: Data(gelesen.utf8))
        XCTAssertNotNil(b.gelesenBis)
        XCTAssertFalse(b.eintraege[0].istNeu(gelesenBis: b.gelesenBis))
    }

    func testMeMitNeuigkeitenUndFiguren() throws {
        let json = """
        {"account":{"id":"a1","name":"Mara","avatar":"fig-eule","isOwner":false,"isKid":false,
                    "lockKind":"none","disabled":false,"createdAt":"2026-10-23T13:35:52+02:00"},
         "limits":{"maxAge":0,"allowUnrated":true,"maxStreams":2,"allowTranscode":true,
                   "allowDownload":false,"maxDownloads":0,"remoteQuality":"1080p"},
         "prefs":{},"libraries":[],"neuigkeiten":3,
         "figuren":["fuchs","eule","katze","roboter","rakete","stern","wal","blume","pilz","geist","drache","pinguin"]}
        """
        let me = try JSONDecoder.lumen.decode(Me.self, from: Data(json.utf8))
        XCTAssertEqual(me.ungelesen, 3)
        // Jede Figur, die der Server kennt, hat hier eine Zeichnung.
        for name in me.figuren ?? [] {
            let figur = try XCTUnwrap(Figuren.figur(name), name)
            XCTAssertFalse(figur.linien.isEmpty && figur.flaechen.isEmpty, name)
        }
        XCTAssertEqual(Figuren.namen, me.figuren)
    }

    func testProfilbildLesen() {
        XCTAssertEqual(Profilbild("av-b"), .farbe("av-b"))
        XCTAssertEqual(Profilbild(""), .farbe("av-a"))
        XCTAssertEqual(Profilbild(nil), .farbe("av-a"))
        XCTAssertEqual(Profilbild("fig-fuchs"), .figur("fuchs"))
        XCTAssertEqual(Profilbild("fig-einhorn"), .farbe("fig-einhorn"))
        XCTAssertEqual(Profilbild("bild"), .bild)
        XCTAssertEqual(Profilbild.figur("wal").wert, "fig-wal")
    }

    /// Der kleine SVG-Leser: relative Befehle, Zahlen ohne Trenner, Bögen.
    func testSVGPfade() throws {
        XCTAssertEqual(SVGLesen.zeichen("M5.6.8-4 3.9.9"), ["M", "5.6", ".8", "-4", "3.9", ".9"])

        let dreieck = SVGLesen.pfad("M12 14l-1 2h2z")
        XCTAssertEqual(dreieck, [.move(12, 14), .line(11, 16), .line(13, 16), .close])

        // Das Kinn der Katze: ein Halbkreis von (18,15) nach (6,15) unten herum.
        let kinn = SVGLesen.pfad("M18 15a6 6 0 0 1-12 0")
        guard case .curve(_, _, _, _, let x1, let y1)? = kinn.dropFirst().first,
              case .curve(_, _, _, _, let x2, let y2)? = kinn.last else {
            return XCTFail("keine Kurven: \(kinn)")
        }
        XCTAssertEqual(x1, 12, accuracy: 0.001)
        XCTAssertEqual(y1, 21, accuracy: 0.001)
        XCTAssertEqual(x2, 6, accuracy: 0.001)
        XCTAssertEqual(y2, 15, accuracy: 0.001)

        // Augen sind gefüllt, der Kopf gezogen.
        let fuchs = try XCTUnwrap(Figuren.figur("fuchs"))
        XCTAssertEqual(fuchs.linien.count, 1)
        XCTAssertEqual(fuchs.flaechen.count, 2)
        XCTAssertEqual(fuchs.farbe.r, 0xf5 / 255.0, accuracy: 0.001)
    }

    func testDetailMitBesetzungUndAehnlichen() throws {
        let json = """
        {"item":{"id":"it_1","libraryId":"lib_1","kind":"movie","title":"Dune",
                 "matchState":"matched","addedAt":"2026-10-23T13:35:52+02:00"},
         "files":[],
         "cast":[{"name":"Zendaya","rolle":"Chani","bild":"/api/v1/art/it_1/cast01"},{"name":"Niemand"}],
         "similar":[{"id":"it_2","libraryId":"lib_1","kind":"movie","title":"Dune 2",
                     "matchState":"matched","addedAt":"2026-10-23T13:35:52+02:00","neu":true}]}
        """
        let d = try JSONDecoder.lumen.decode(ItemDetail.self, from: Data(json.utf8))
        XCTAssertEqual(d.cast?.first?.bildArt, "cast01")
        XCTAssertNil(d.cast?.last?.bildArt)
        XCTAssertEqual(d.similar?.first?.abzeichenNeu, "Neu")
    }

    func testVorschauUndAvatarAdressen() async {
        let ohne = APIClient(baseURL: URL(string: "http://lumen.local:8097")!)
        let keine = await ohne.vorschauURL(itemId: "it_1")
        XCTAssertNil(keine, "ohne Anmeldung keine Vorschau")
        // Das eigene Bild braucht zu Hause keine Anmeldung — die
        // Profilauswahl zeigt es vor der Anmeldung.
        let bild = await ohne.avatarURL(accountId: "a1").absoluteString
        XCTAssertEqual(bild, "http://lumen.local:8097/api/v1/avatar/a1")
        let frisch = await ohne.avatarURL(accountId: "a1", stand: 3).absoluteString
        XCTAssertEqual(frisch, "http://lumen.local:8097/api/v1/avatar/a1?v=3")

        let mit = APIClient(baseURL: URL(string: "http://lumen.local:8097")!, token: "t")
        await mit.setMedienSchluessel("k")
        let vorschau = await mit.vorschauURL(itemId: "it_1")?.absoluteString
        XCTAssertEqual(vorschau, "http://lumen.local:8097/api/v1/art/it_1/vorschau?key=k")
        // Angemeldet (von unterwegs nötig) trägt auch das Profilbild den
        // Schlüssel — nach `v`, die Abfrage ist sortiert.
        let eigenes = await mit.avatarURL(accountId: "a1", stand: 2).absoluteString
        XCTAssertEqual(eigenes, "http://lumen.local:8097/api/v1/avatar/a1?key=k&v=2")
    }
}

final class MediathekTests: XCTestCase {

    private func titel(_ id: String, _ kind: String = "movie", extra: String = "") throws -> Item {
        let json = #"{"id":"\#(id)","libraryId":"lib_1","kind":"\#(kind)","title":"\#(id)","matchState":"matched","addedAt":"2026-10-23T13:35:52+02:00"\#(extra)}"#
        return try JSONDecoder.lumen.decode(Item.self, from: Data(json.utf8))
    }

    /// Eine Seite des Rasters, wie GET /items?seite=1 sie schickt — und
    /// eine, in der Go die Genres als `null` schreibt.
    func testItemSeite() throws {
        let item = #"{"id":"it_1","libraryId":"lib_1","kind":"movie","title":"X","matchState":"matched","addedAt":"2026-10-23T13:35:52+02:00"}"#
        let mit = #"{"items":[\#(item)],"gesamt":61,"offset":0,"weiter":true,"genres":["Drama","Komödie"]}"#
        let ohne = #"{"items":[],"gesamt":0,"offset":0,"weiter":false,"genres":null}"#

        let a = try JSONDecoder.lumen.decode(ItemSeite.self, from: Data(mit.utf8))
        XCTAssertEqual(a.items.count, 1)
        XCTAssertEqual(a.gesamt, 61)
        XCTAssertTrue(a.weiter)
        XCTAssertEqual(a.genres ?? [], ["Drama", "Komödie"])
        let b = try JSONDecoder.lumen.decode(ItemSeite.self, from: Data(ohne.utf8))
        XCTAssertNil(b.genres)
        XCTAssertFalse(b.weiter)
    }

    func testRasterZeile() {
        XCTAssertEqual(Raster.zeile(gesamt: 248, genre: nil), "248 Titel")
        XCTAssertEqual(Raster.zeile(gesamt: 12, genre: "Drama"), "12 Titel · Drama")
    }

    func testSprachwahl() {
        XCTAssertEqual(Sprachwahl.ton.first?.wert, "")
        XCTAssertEqual(Sprachwahl.untertitel.first?.wert, "aus")
        XCTAssertEqual(Array(Sprachwahl.sprachen.prefix(2).map(\.wert)), ["de", "en"])
        XCTAssertEqual(Sprachwahl.sprachen.count, Spuren.sprachen.count, "jede Sprache genau einmal")
        XCTAssertEqual(Sprachwahl.tonName(nil), "Wie in der Datei")
        XCTAssertEqual(Sprachwahl.tonName("fr"), "Französisch")
        XCTAssertEqual(Sprachwahl.untertitelName("aus"), "Aus")
        XCTAssertEqual(Sprachwahl.untertitelName(""), "Aus")
        XCTAssertEqual(Sprachwahl.untertitelName("de"), "Deutsch")
    }

    /// Specials ans Ende, der Rest nach Nummer — wie die Reiter im Browser.
    func testStaffelnSpecialsZuletzt() throws {
        let staffeln = [
            try titel("s2", "season", extra: #","seasonNo":2"#),
            try titel("s0", "season", extra: #","seasonNo":0"#),
            try titel("s1", "season", extra: #","seasonNo":1"#),
        ]
        XCTAssertEqual(Titelseite.staffeln(staffeln).map(\.id), ["s1", "s2", "s0"])
    }

    func testKnopfBeschriftungen() throws {
        let neu = try titel("a")
        let angefangen = try titel("b", extra: #","resumeMs":754000,"progress":12"#)
        XCTAssertEqual(Titelseite.abspielen(neu), "Ansehen")
        XCTAssertEqual(Titelseite.abspielen(angefangen), "Weiter bei 12:34")
        XCTAssertFalse(Titelseite.kannVonVorn(neu))
        XCTAssertTrue(Titelseite.kannVonVorn(angefangen))
        XCTAssertEqual(Titelseite.folge(angefangen), "Weiter")

        let serie = try titel("c", "series", extra: #","gesehen":3,"folgen":10"#)
        XCTAssertEqual(Titelseite.serieAbspielen(serie), "Weiterschauen")
        XCTAssertEqual(Titelseite.serieAbspielen(try titel("d", "series")), "Ansehen")
    }

    func testReiheWeiterMit() throws {
        let json = """
        {"id":"rh_1","name":"Bucht","source":"tmdb","anzahl":3,
         "items":[
          {"id":"a","libraryId":"l","kind":"movie","title":"Die Bucht","matchState":"matched","addedAt":"2026-10-23T13:35:52+02:00","finished":true},
          {"id":"b","libraryId":"l","kind":"movie","title":"Die Bucht 2","matchState":"matched","addedAt":"2026-10-23T13:35:52+02:00"},
          {"id":"c","libraryId":"l","kind":"movie","title":"Die Bucht 3","matchState":"matched","addedAt":"2026-10-23T13:35:52+02:00"}
         ]}
        """
        let reihe = try JSONDecoder.lumen.decode(Reihe.self, from: Data(json.utf8))
        XCTAssertEqual(reihe.naechsterFilm?.id, "b")
        XCTAssertEqual(reihe.abspielText, "Weiter mit Die Bucht 2")
        XCTAssertEqual(reihe.unterzeile, "3 Filme · 1 gesehen · zusammengestellt nach TMDb")
    }

    func testAbspielstartHatJeEigeneKennung() throws {
        let t = try titel("x")
        XCTAssertNotEqual(Abspielstart(item: t).id, Abspielstart(item: t).id)
        XCTAssertFalse(Abspielstart(item: t).vonVorn)
    }
}

/// Sicherung und Übersicht in der Mac-Verwaltung: genau die Antworten, die
/// sicherung.go und adminOverview schicken — und ein älterer Server, der
/// die neuen Felder noch nicht kennt.
final class SicherungTests: XCTestCase {

    func testSicherungenWieVomServer() throws {
        let json = """
        {"sicherungen":[{"nr":2,"tag":"2026-11-01","mb":0},{"nr":1,"tag":"2026-11-02","mb":12}],
         "zuletzt":"2026-11-02","fehler":"","generationen":2,
         "hinweis":"Lumen behält genau zwei Sicherungen."}
        """
        let s = try JSONDecoder.lumen.decode(Sicherungen.self, from: Data(json.utf8))
        XCTAssertEqual(s.liste.map(\.nr), [1, 2])
        XCTAssertEqual(s.liste[0].name, "Neueste")
        XCTAssertEqual(s.liste[1].name, "Davor")
        XCTAssertEqual(s.liste[0].groesse, "12 MB")
        XCTAssertEqual(s.liste[1].groesse, "unter 1 MB")
        XCTAssertEqual(s.fehler, "")
        XCTAssertEqual(s.generationen, 2)
    }

    /// Ohne Sicherungen schickt Go `null` statt einer leeren Liste.
    func testNochKeineSicherung() throws {
        let json = #"{"sicherungen":null,"zuletzt":"","fehler":"datenbank kaputt","generationen":2,"hinweis":""}"#
        let s = try JSONDecoder.lumen.decode(Sicherungen.self, from: Data(json.utf8))
        XCTAssertTrue(s.liste.isEmpty)
        XCTAssertEqual(s.fehler, "datenbank kaputt")
    }

    func testWiederherstellung() throws {
        let json = #"{"status":"Lumen startet neu und nimmt die Sicherung"}"#
        let w = try JSONDecoder.lumen.decode(Wiederherstellung.self, from: Data(json.utf8))
        XCTAssertEqual(w.status, "Lumen startet neu und nimmt die Sicherung")
    }

    func testTagText() throws {
        var kalender = Calendar(identifier: .gregorian)
        kalender.timeZone = .current
        let heute = try XCTUnwrap(kalender.date(from: DateComponents(year: 2026, month: 11, day: 3, hour: 12)))
        XCTAssertEqual(Sicherung.tagText("2026-11-03", heute: heute), "heute")
        XCTAssertEqual(Sicherung.tagText("2026-11-02", heute: heute), "02.11.2026")
        XCTAssertEqual(Sicherung.tagText("unsinn", heute: heute), "unsinn")
    }

    func testUebersichtMitPlatzUndSicherung() throws {
        let json = """
        {"version":"1.4.0","uptimeMs":1000,"libraries":[],"accounts":2,
         "load":{"active":0,"transcoding":0,"directPlay":0,"cpuPercent":3},
         "cleanup":{"duplicates":0,"missing":0,"unmatched":0,"pending":0},
         "ffmpeg":true,"platz":{"datenMB":12700,"umwandlungMB":4000},
         "sicherung":"2026-11-03","memoryMB":40,"goroutines":12,
         "source":{"url":"https://example.org","eigen":false},"privacyNote":"x"}
        """
        let u = try JSONDecoder.lumen.decode(Uebersicht.self, from: Data(json.utf8))
        XCTAssertEqual(u.platz?.datenMB, 12700)
        XCTAssertEqual(u.platz?.umwandlungMB, 4000)
        XCTAssertEqual(u.platz?.knapp, true)
        XCTAssertEqual(u.sicherung, "2026-11-03")
    }

    /// Ein Server von vor der Sicherung: weder `platz` noch `sicherung`.
    /// Die Übersicht muss sich trotzdem lesen lassen.
    func testUebersichtVonAlterServer() throws {
        let json = #"{"version":"1.3.0","uptimeMs":1000,"accounts":1,"ffmpeg":false}"#
        let u = try JSONDecoder.lumen.decode(Uebersicht.self, from: Data(json.utf8))
        XCTAssertNil(u.platz)
        XCTAssertNil(u.sicherung)
        XCTAssertEqual(u.version, "1.3.0")
    }

    func testPlatzText() {
        XCTAssertEqual(FreierPlatz.text(830), "830 MB")
        XCTAssertEqual(FreierPlatz.text(1024), "1.0 GB")
        XCTAssertEqual(FreierPlatz.text(12700), "12.4 GB")
        XCTAssertFalse(FreierPlatz(datenMB: 5120, umwandlungMB: 9000).knapp)
        XCTAssertTrue(FreierPlatz(datenMB: 9000, umwandlungMB: 5119).knapp)
    }
}
