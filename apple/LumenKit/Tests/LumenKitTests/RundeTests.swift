// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

import XCTest
@testable import LumenKit

// Was mit dieser Runde dazukam: die neuen Wege (Profilschutz, gesehen für
// Staffeln, Downloads, Vorschaubilder), das Verhalten bei 401 mitten in der
// Sitzung, der Bestätigungs-Dialog, der offen bleibt, und die Buchhaltung
// der Downloads.

// MARK: - Ein Server, der sagt, was der Test will

final class AntwortServer: URLProtocol {
    struct Antwort { let status: Int; let rumpf: String }

    private static let lock = NSLock()
    nonisolated(unsafe) private static var regel: (URLRequest) -> Antwort = { _ in Antwort(status: 204, rumpf: "") }
    nonisolated(unsafe) private static var anfragen: [URLRequest] = []

    static func antworte(_ f: @escaping (URLRequest) -> Antwort) {
        lock.lock(); defer { lock.unlock() }
        regel = f
        anfragen = []
    }

    static var letzte: URLRequest? {
        lock.lock(); defer { lock.unlock() }
        return anfragen.last
    }

    static func client(token: String? = "GEHEIM") -> APIClient {
        let aufbau = URLSessionConfiguration.ephemeral
        aufbau.protocolClasses = [AntwortServer.self]
        return APIClient(baseURL: URL(string: "http://nas:8097")!, token: token,
                         session: URLSession(configuration: aufbau))
    }

    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }

    override func startLoading() {
        Self.lock.lock()
        Self.anfragen.append(request)
        let f = Self.regel
        Self.lock.unlock()
        let a = f(request)
        let http = HTTPURLResponse(url: request.url!, statusCode: a.status, httpVersion: "HTTP/1.1",
                                   headerFields: ["Content-Type": "application/json"])!
        client?.urlProtocol(self, didReceive: http, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: Data(a.rumpf.utf8))
        client?.urlProtocolDidFinishLoading(self)
    }

    override func stopLoading() {}
}

/// Ein Zähler, den ein @Sendable-Abschluss hochzählen darf.
final class Zaehler: @unchecked Sendable {
    private let lock = NSLock()
    private var n = 0
    func plus() { lock.lock(); n += 1; lock.unlock() }
    var wert: Int { lock.lock(); defer { lock.unlock() }; return n }
}

// MARK: - Neue Wege

final class SchnittstellenTests: XCTestCase {

    func testSperreAendernSchicktAltArtNeu() async throws {
        AntwortServer.antworte { _ in .init(status: 204, rumpf: "") }
        let client = AntwortServer.client()
        try await client.sperreAendern(alt: "1234", art: "pin", neu: "9876")
        let r = try XCTUnwrap(AntwortServer.letzte)
        XCTAssertEqual(r.httpMethod, "PUT")
        XCTAssertEqual(r.url?.path, "/api/v1/me/sperre")
        let rumpf = try XCTUnwrap(Self.rumpf(r))
        let json = try XCTUnwrap(JSONSerialization.jsonObject(with: rumpf) as? [String: String])
        XCTAssertEqual(json, ["alt": "1234", "art": "pin", "neu": "9876"])
    }

    func testFalscherPinKommtAlsEigenerFehler() async {
        AntwortServer.antworte { _ in .init(status: 403, rumpf: #"{"error":"das stimmt nicht","code":"falsch"}"#) }
        do {
            try await AntwortServer.client().sperreAendern(alt: "0000", art: "none", neu: "")
            XCTFail("darf nicht durchgehen")
        } catch let e as LumenError {
            XCTAssertEqual(e.code, "falsch")
            XCTAssertEqual(e.status, 403)
            XCTAssertEqual(e.errorDescription, "Das stimmt nicht.")
        } catch {
            XCTFail("falscher Fehler: \(error)")
        }
    }

    func testVerwaltungBrauchtSperre() async {
        AntwortServer.antworte { _ in
            .init(status: 409, rumpf: #"{"error":"wer verwaltet, braucht eine sperre","code":"verwaltung_braucht_sperre"}"#)
        }
        do {
            try await AntwortServer.client().sperreAendern(alt: "1234", art: "none", neu: "")
            XCTFail("darf nicht durchgehen")
        } catch let e as LumenError {
            XCTAssertEqual(e.code, "verwaltung_braucht_sperre")
            XCTAssertFalse(LumenError.fehltAufServer(e))
        } catch {
            XCTFail("falscher Fehler: \(error)")
        }
    }

    /// 409 ohne bekannte Kennung bleibt `server` — der Fänger beim Update
    /// („es laufen Wiedergaben") soll sich nicht ändern.
    func testAndere409BleibenServerfehler() async {
        AntwortServer.antworte { _ in .init(status: 409, rumpf: #"{"error":"es laufen wiedergaben"}"#) }
        do {
            _ = try await AntwortServer.client().adminUpdateRun()
            XCTFail("darf nicht durchgehen")
        } catch LumenError.server(_, let status) {
            XCTAssertEqual(status, 409)
        } catch {
            XCTFail("falscher Fehler: \(error)")
        }
    }

    func testFehlenderWegBlendetAus() async {
        AntwortServer.antworte { _ in .init(status: 404, rumpf: "404 page not found") }
        do {
            _ = try await AntwortServer.client().downloadInfo(itemId: "it_1")
            XCTFail("darf nicht durchgehen")
        } catch {
            XCTAssertTrue(LumenError.fehltAufServer(error))
        }
    }

    func testGesehenFuerEineStaffel() async throws {
        AntwortServer.antworte { _ in .init(status: 204, rumpf: "") }
        let client = AntwortServer.client()
        try await client.markFinished("ser_1", staffel: 2)
        var r = try XCTUnwrap(AntwortServer.letzte)
        XCTAssertEqual(r.httpMethod, "POST")
        XCTAssertEqual(r.url?.path, "/api/v1/items/ser_1/finished")
        XCTAssertEqual(r.url?.query, "staffel=2")

        try await client.clearFinished("ser_1", staffel: nil)
        r = try XCTUnwrap(AntwortServer.letzte)
        XCTAssertEqual(r.httpMethod, "DELETE")
        XCTAssertNil(r.url?.query)
    }

    func testDownloadInfo() async throws {
        AntwortServer.antworte { _ in
            .init(status: 200, rumpf: """
            {"erlaubt":true,"dateien":[{"fileId":"f_1","dateiname":"Film (2019).mkv","groesse":7900000000,"hoehe":1080},
             {"fileId":"f_2","dateiname":"Film (2019).avi","groesse":700000000,"hoehe":400}]}
            """)
        }
        let info = try await AntwortServer.client().downloadInfo(itemId: "it_1")
        XCTAssertTrue(info.erlaubt)
        XCTAssertEqual(info.dateien.map(\.fileId), ["f_1", "f_2"])
        XCTAssertEqual(info.dateien[0].beschriftung, "1080p · MKV · 7.9 GB")
        XCTAssertEqual(AntwortServer.letzte?.url?.path, "/api/v1/items/it_1/download/info")
    }

    func testDownloadInfoOhneDateien() throws {
        let info = try JSONDecoder.lumen.decode(DownloadInfo.self, from: Data(#"{"erlaubt":false,"dateien":null}"#.utf8))
        XCTAssertFalse(info.erlaubt)
        XCTAssertTrue(info.dateien.isEmpty)
    }

    /// Das Token reist im Kopf, nie in der Adresse.
    func testDownloadAnfrageMitTokenImKopf() async {
        let r = await AntwortServer.client().downloadAnfrage(fileId: "f_1")
        XCTAssertEqual(r.url?.absoluteString, "http://nas:8097/api/v1/stream/f_1/download")
        XCTAssertEqual(r.value(forHTTPHeaderField: "Authorization"), "Bearer GEHEIM")
        XCTAssertNil(r.url?.query)
    }

    func testTrickplayBereitInArbeitOderNicht() async throws {
        AntwortServer.antworte { _ in .init(status: 202, rumpf: "") }
        var info = try await AntwortServer.client().trickplay(fileId: "f_1")
        XCTAssertNil(info)

        AntwortServer.antworte { _ in .init(status: 404, rumpf: "") }
        info = try await AntwortServer.client().trickplay(fileId: "f_1")
        XCTAssertNil(info)

        AntwortServer.antworte { _ in
            .init(status: 200, rumpf: """
            {"intervall":10,"breite":320,"hoehe":180,"spalten":10,"zeilen":10,"bilder":250,
             "blaetter":["/api/v1/stream/f_1/trickplay/0.jpg","/api/v1/stream/f_1/trickplay/1.jpg","/api/v1/stream/f_1/trickplay/2.jpg"]}
            """)
        }
        info = try await AntwortServer.client().trickplay(fileId: "f_1")
        XCTAssertEqual(info?.bilder, 250)
        XCTAssertEqual(AntwortServer.letzte?.url?.path, "/api/v1/stream/f_1/trickplay")
    }

    func testHLSBeenden() async throws {
        AntwortServer.antworte { _ in .init(status: 204, rumpf: "") }
        try await AntwortServer.client().hlsBeenden(fileId: "f_1", sitzung: "s_9")
        XCTAssertEqual(AntwortServer.letzte?.httpMethod, "DELETE")
        XCTAssertEqual(AntwortServer.letzte?.url?.path, "/api/v1/stream/f_1/hls/s_9")
    }

    func testAdminSperreZuruecksetzen() async throws {
        AntwortServer.antworte { _ in .init(status: 204, rumpf: "") }
        try await AntwortServer.client().adminSperreSetzen("acc_2", art: "none", neu: "")
        XCTAssertEqual(AntwortServer.letzte?.httpMethod, "PUT")
        XCTAssertEqual(AntwortServer.letzte?.url?.path, "/api/v1/admin/accounts/acc_2/sperre")
    }

    func testFernzugriffUndQuellen() throws {
        let fern = try JSONDecoder.lumen.decode(Fernzugriff.self, from: Data("""
        {"an":true,"verbunden":true,"vermittler":"relay.example:443","versuche":0,
         "eigenerFingerabdruck":"ab:cd:ef"}
        """.utf8))
        XCTAssertEqual(fern.eigenerFingerabdruck, "ab:cd:ef")
        let aus = try JSONDecoder.lumen.decode(Fernzugriff.self, from: Data(#"{"an":false,"hinweis":"nicht eingerichtet"}"#.utf8))
        XCTAssertFalse(aus.an)

        let q = try JSONDecoder.lumen.decode(MetadatenQuellen.self, from: Data("""
        {"tmdb":{"configured":true,"name":"TMDb","what":"Filme"},"imdb":{"configured":false,"enabled":true},
         "language":"de-DE","region":"CH","ready":true,"note":"Gilt für alle Profile."}
        """.utf8))
        XCTAssertEqual(q.tmdb?.configured, true)
        XCTAssertEqual(q.imdb?.enabled, true)
        XCTAssertEqual(q.region, "CH")
    }

    static func rumpf(_ r: URLRequest) -> Data? {
        if let d = r.httpBody { return d }
        guard let s = r.httpBodyStream else { return nil }
        s.open(); defer { s.close() }
        var daten = Data()
        var puffer = [UInt8](repeating: 0, count: 1024)
        while s.hasBytesAvailable {
            let n = s.read(&puffer, maxLength: puffer.count)
            if n <= 0 { break }
            daten.append(puffer, count: n)
        }
        return daten
    }
}

// MARK: - Abmeldung mitten in der Sitzung

final class AbmeldungTests: XCTestCase {

    func test401RuftDieSitzung() async {
        AntwortServer.antworte { _ in .init(status: 401, rumpf: #"{"error":"nicht angemeldet"}"#) }
        let client = AntwortServer.client()
        let zaehler = Zaehler()
        await client.setAbmeldeHandler { zaehler.plus() }
        do {
            _ = try await client.me()
            XCTFail("darf nicht durchgehen")
        } catch LumenError.signedOut {
        } catch {
            XCTFail("falscher Fehler: \(error)")
        }
        XCTAssertEqual(zaehler.wert, 1)
        let token = await client.token
        XCTAssertNil(token)
    }

    /// Ohne Token ist 401 eine falsche Eingabe, keine Abmeldung.
    func test401OhneTokenIstKeineAbmeldung() async {
        AntwortServer.antworte { _ in .init(status: 401, rumpf: #"{"error":"pin falsch"}"#) }
        let client = AntwortServer.client(token: nil)
        let zaehler = Zaehler()
        await client.setAbmeldeHandler { zaehler.plus() }
        do {
            _ = try await client.unlock(profileId: "acc_1", secret: "0000", deviceName: "Test")
            XCTFail("darf nicht durchgehen")
        } catch LumenError.server(let text, let status) {
            XCTAssertEqual(status, 401)
            XCTAssertEqual(text, "pin falsch")
        } catch {
            XCTFail("falscher Fehler: \(error)")
        }
        XCTAssertEqual(zaehler.wert, 0)
    }

    func testAbbruchIstNichtUnerreichbar() {
        XCTAssertTrue(APIClient.istAbbruch(URLError(.cancelled)))
        XCTAssertTrue(APIClient.istAbbruch(CancellationError()))
        XCTAssertFalse(APIClient.istAbbruch(URLError(.timedOut)))
    }
}

// MARK: - Der Bestätigungs-Dialog bleibt offen

@MainActor
final class BestaetigungsDialogTests: XCTestCase {

    private func warte(_ bedingung: @MainActor () -> Bool, _ datei: StaticString = #filePath, _ zeile: UInt = #line) async {
        for _ in 0..<200 {
            if bedingung() { return }
            try? await Task.sleep(nanoseconds: 10_000_000)
        }
        XCTFail("Bedingung nicht erreicht", file: datei, line: zeile)
    }

    private func sitzung() -> Session {
        let defaults = UserDefaults(suiteName: "lumen.test.\(UUID().uuidString)")!
        return Session(store: TokenStore(fach: SpeicherGeheimfach(), defaults: defaults))
    }

    func testFalscherCodeLaesstDenDialogOffen() async throws {
        let session = sitzung()
        let client = BestaetigungsServer.client()
        let arbeit = Task { @MainActor in
            try await session.verwaltet { try await client.adminDeleteGroup("g_1") }
        }
        await warte { session.bestaetigungOffen }
        XCTAssertFalse(session.bestaetigungFalsch)

        session.bestaetigen(code: "0000")
        XCTAssertTrue(session.bestaetigungOffen, "beim Prüfen bleibt der Dialog stehen")
        await warte { session.bestaetigungVersuch == 1 }
        XCTAssertTrue(session.bestaetigungOffen)
        XCTAssertTrue(session.bestaetigungFalsch)

        session.bestaetigen(code: "1234")
        try await arbeit.value
        XCTAssertFalse(session.bestaetigungOffen)
        XCTAssertFalse(session.bestaetigungPrueft)
    }

    func testAbbrechenWecktDieAnfrage() async {
        let session = sitzung()
        let client = BestaetigungsServer.client()
        let arbeit = Task { @MainActor in
            try await session.verwaltet { try await client.adminDeleteGroup("g_1") }
        }
        await warte { session.bestaetigungOffen }
        session.bestaetigenAbbrechen()
        do {
            try await arbeit.value
            XCTFail("abgebrochen darf nicht durchgehen")
        } catch {
            XCTAssertEqual(Session.text(error), "Abgebrochen.")
        }
        XCTAssertFalse(session.bestaetigungOffen)
    }
}

// MARK: - Was sich jetzt lesen lassen muss

final class TolerantesLesenTests: XCTestCase {

    func testMeMitBibliothekenNull() throws {
        let json = """
        {"account":{"id":"acc_1","name":"Mia","avatar":"av-b","isOwner":false,"isKid":false,"lockKind":"pin",
         "disabled":false,"createdAt":"2026-01-01T10:00:00Z"},
         "limits":{"maxAge":18,"allowUnrated":true,"maxStreams":2,"allowTranscode":true,"allowDownload":true,
                   "maxDownloads":20,"remoteQuality":"1080p"},
         "prefs":{},"libraries":null}
        """
        do {
            let me = try JSONDecoder.lumen.decode(Me.self, from: Data(json.utf8))
            XCTAssertEqual(me.libraries, [])
        } catch {
            // Fehlt am Konto hier ein Pflichtfeld, sagt der Fehler welches.
            XCTFail("\(error)")
        }
    }

    func testAktiveWiedergabenOhneKennungUndLast() throws {
        let json = #"{"sessions":[{"mode":"transcode","detail":"4K → 1080p"},{"mode":"direct"}]}"#
        let s = try JSONDecoder.lumen.decode(Sessions.self, from: Data(json.utf8))
        XCTAssertEqual(s.sessions.count, 2)
        XCTAssertNil(s.sessions[0].kennung)
        XCTAssertNotEqual(s.sessions[0].id, s.sessions[1].id)
        XCTAssertEqual(s.sessions[1].mode, "direct")
        XCTAssertNil(s.load)
    }
}

// MARK: - Wiedergabe: Hilfen

final class WiedergabeHilfenTests: XCTestCase {

    func testHLSKennungAusDerMedienliste() {
        XCTAssertEqual(HLSSitzung.kennung(
            in: "http://nas:8097/api/v1/stream/f_1/hls/s_42/media.m3u8?key=abc&spur=1", fileId: "f_1"), "s_42")
        XCTAssertEqual(HLSSitzung.kennung(in: "/api/v1/stream/f_1/hls/s_42/17.ts", fileId: "f_1"), "s_42")
        // Die Hauptliste hat keine Kennung; eine andere Datei zählt nicht.
        XCTAssertNil(HLSSitzung.kennung(in: "http://nas:8097/api/v1/stream/f_1/hls/master.m3u8", fileId: "f_1"))
        XCTAssertNil(HLSSitzung.kennung(in: "http://nas:8097/api/v1/stream/f_2/hls/s_42/media.m3u8", fileId: "f_1"))
        XCTAssertNil(HLSSitzung.kennung(in: "http://nas:8097/api/v1/stream/f_1", fileId: "f_1"))
    }

    func testTrickplayKachel() throws {
        let t = Trickplay(intervall: 10, breite: 320, hoehe: 180, spalten: 10, zeilen: 10, bilder: 250, blaetter: [])
        XCTAssertEqual(t.kachel(bei: 0), .init(blatt: 0, x: 0, y: 0, breite: 320, hoehe: 180))
        // Bild 13: Zeile 1, Spalte 3.
        XCTAssertEqual(t.kachel(bei: 135), .init(blatt: 0, x: 960, y: 180, breite: 320, hoehe: 180))
        // Bild 100 ist das erste des zweiten Blatts.
        XCTAssertEqual(t.kachel(bei: 1000)?.blatt, 1)
        XCTAssertEqual(t.kachel(bei: 1000)?.x, 0)
        // Hinter dem Ende das letzte Bild, vor dem Anfang das erste.
        XCTAssertEqual(t.kachel(bei: 99_999)?.blatt, 2)
        XCTAssertEqual(t.kachel(bei: -5)?.blatt, 0)
    }

    func testTrickplayInMillisekunden() {
        let t = Trickplay(intervall: 10_000, breite: 320, hoehe: 180, spalten: 5, zeilen: 5, bilder: 30, blaetter: [])
        XCTAssertEqual(t.intervallSekunden, 10)
        XCTAssertEqual(t.kachel(bei: 250)?.blatt, 1)
        XCTAssertNil(Trickplay(intervall: 0, breite: 1, hoehe: 1, spalten: 1, zeilen: 1, bilder: 1, blaetter: []).kachel(bei: 3))
    }
}

// MARK: - Downloads: die Buchhaltung

final class DownloadKatalogTests: XCTestCase {

    private func titel(_ id: String, kind: String = "movie", laenge: Int64 = 6_000_000) throws -> Item {
        let json = """
        {"id":"\(id)","libraryId":"lib_1","kind":"\(kind)","title":"Titel \(id)","matchState":"matched",
         "addedAt":"2026-01-01T10:00:00Z","runtimeMs":\(laenge)}
        """
        return try JSONDecoder.lumen.decode(Item.self, from: Data(json.utf8))
    }

    private func eintrag(_ fileId: String, item: Item, server: String = "srv", konto: String = "acc",
                         groesse: Int64 = 1000) -> DownloadEintrag {
        DownloadEintrag(fileId: fileId, item: item, serverId: server, accountId: konto,
                        dateiname: "film.mkv", groesse: groesse, hoehe: 1080)
    }

    func testAufnehmenNurEinmal() throws {
        var k = DownloadKatalog()
        let it = try titel("it_1")
        XCTAssertTrue(k.aufnehmen(eintrag("f_1", item: it)))
        XCTAssertFalse(k.aufnehmen(eintrag("f_1", item: it)))
        k.gescheitert("f_1", fehler: "weg")
        // Ein gescheiterter darf neu beginnen.
        XCTAssertTrue(k.aufnehmen(eintrag("f_1", item: it)))
        XCTAssertEqual(k["f_1"]?.zustand, .wartet)
    }

    func testFortschrittUndFertig() throws {
        var k = DownloadKatalog()
        k.aufnehmen(eintrag("f_1", item: try titel("it_1"), groesse: 2000))
        k.fortschritt("f_1", geladen: 500)
        XCTAssertEqual(k["f_1"]?.anteil ?? 0, 0.25, accuracy: 0.001)
        XCTAssertEqual(k.nochNoetig, 1500)
        k.fertig("f_1", lokalerName: "f_1.mkv", groesse: 2000)
        XCTAssertEqual(k["f_1"]?.zustand, .fertig)
        XCTAssertEqual(k.belegt, 2000)
        XCTAssertEqual(k.nochNoetig, 0)
        XCTAssertEqual(k.fuer(itemId: "it_1")?.fileId, "f_1")
    }

    func testOfflineStandNurFuerDenRichtigenServerUndDasRichtigeProfil() throws {
        var k = DownloadKatalog()
        k.aufnehmen(eintrag("f_1", item: try titel("it_1")))
        k.aufnehmen(eintrag("f_2", item: try titel("it_2"), konto: "acc_anders"))
        k.standOffline("it_1", position: 1200, dauer: 6000)
        k.standOffline("it_2", position: 300, dauer: 6000)
        XCTAssertEqual(k.offeneStaende(serverId: "srv", accountId: "acc").map(\.fileId), ["f_1"])
        XCTAssertTrue(k.offeneStaende(serverId: "anderer", accountId: "acc").isEmpty)

        k.standGemeldet("it_1", position: 1200)
        XCTAssertTrue(k.offeneStaende(serverId: "srv", accountId: "acc").isEmpty)
        XCTAssertEqual(k["f_1"]?.stand, 1200)
    }

    /// Wer weiterschaut, während der alte Stand unterwegs ist, behält den
    /// neuen als offen.
    func testNeuererStandBleibtOffen() throws {
        var k = DownloadKatalog()
        k.aufnehmen(eintrag("f_1", item: try titel("it_1")))
        k.standOffline("it_1", position: 1200, dauer: 6000)
        k.standOffline("it_1", position: 1500, dauer: 6000)
        k.standGemeldet("it_1", position: 1200)
        XCTAssertEqual(k.offeneStaende(serverId: "srv", accountId: "acc").count, 1)
    }

    func testVorschlagNimmtDieGroessteDieNochPasst() {
        let gross = DownloadDatei(fileId: "a", dateiname: "a.mkv", groesse: 8_000, hoehe: 2160)
        let mittel = DownloadDatei(fileId: "b", dateiname: "b.mkv", groesse: 3_000, hoehe: 1080)
        let klein = DownloadDatei(fileId: "c", dateiname: "c.avi", groesse: 700, hoehe: 400)
        XCTAssertEqual(DownloadKatalog.vorschlag([klein, gross, mittel], frei: nil)?.fileId, "a")
        XCTAssertEqual(DownloadKatalog.vorschlag([klein, gross, mittel], frei: 5_000)?.fileId, "b")
        XCTAssertNil(DownloadKatalog.vorschlag([gross], frei: 10))
    }

    func testLokalerNameBehaeltDieEndung() {
        XCTAssertEqual(DownloadKatalog.lokalerName(fileId: "f_1", dateiname: "Film (2019).MKV"), "f_1.mkv")
        XCTAssertEqual(DownloadKatalog.lokalerName(fileId: "../f/1", dateiname: "x"), "f1")
    }

    func testKatalogUeberlebtSpeichern() throws {
        var k = DownloadKatalog()
        k.aufnehmen(eintrag("f_1", item: try titel("it_1")))
        k.fertig("f_1", lokalerName: "f_1.mkv", groesse: 1000)
        let daten = try JSONEncoder().encode(k)
        let zurueck = try JSONDecoder().decode(DownloadKatalog.self, from: daten)
        XCTAssertEqual(zurueck, k)
    }
}
