// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

import XCTest
@testable import LumenKit

// Jahre bei Serien (#13), Favoriten (#10), Sortieren und Buchstaben (#11),
// die Personenseite (#9) und Fassungen und Teile beim Aufräumen (#8).
//
// Die Adressen prüft ein `URLProtocol`, das jede Anfrage mitschreibt und
// selbst antwortet: so sieht der Test genau, was der Client dem Server
// schicken würde — ohne Server.

// MARK: - Mitschnitt

/// Was der Client geschickt hat. Mit Schloss, weil URLSession die Anfragen
/// auf eigenen Fäden abarbeitet.
final class Mitschnitt: @unchecked Sendable {
    private let schloss = NSLock()
    private var anfragen: [URLRequest] = []

    func merken(_ r: URLRequest) {
        schloss.lock(); defer { schloss.unlock() }
        anfragen.append(r)
    }

    func leeren() {
        schloss.lock(); defer { schloss.unlock() }
        anfragen = []
    }

    var alle: [URLRequest] {
        schloss.lock(); defer { schloss.unlock() }
        return anfragen
    }
}

/// Antwortet auf jede Anfrage wie der Server — nur mit festen Daten.
final class Aufzeichner: URLProtocol {
    static let mitschnitt = Mitschnitt()

    static func client(token: String = "GEHEIM") -> APIClient {
        let aufbau = URLSessionConfiguration.ephemeral
        aufbau.protocolClasses = [Aufzeichner.self]
        return APIClient(baseURL: URL(string: "http://nas:8097")!, token: token,
                         session: URLSession(configuration: aufbau))
    }

    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }

    override func startLoading() {
        Self.mitschnitt.merken(request)
        let (status, text) = Self.antwort(request)
        let http = HTTPURLResponse(url: request.url!, statusCode: status, httpVersion: "HTTP/1.1",
                                   headerFields: ["Content-Type": "application/json"])!
        client?.urlProtocol(self, didReceive: http, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: Data(text.utf8))
        client?.urlProtocolDidFinishLoading(self)
    }

    override func stopLoading() {}

    private static let beispiel = #"{"id":"it_1","libraryId":"lib_1","kind":"movie","title":"Alien","matchState":"matched","addedAt":"2026-10-23T13:35:52+02:00","favorite":true}"#

    private static func antwort(_ r: URLRequest) -> (Int, String) {
        let pfad = r.url?.path ?? ""
        if pfad == "/api/v1/items/letters" { return (200, "{\"A\":12,\"#\":3,\"B\":0}") }
        if pfad == "/api/v1/items" {
            return (200, #"{"items":[\#(beispiel)],"gesamt":1,"offset":0,"weiter":false,"genres":["Drama"]}"#)
        }
        if pfad.hasPrefix("/api/v1/persons/") {
            return (200, #"{"name":"Tom Hanks","bild":"/api/v1/persons/Tom%20Hanks/bild"}"#)
        }
        if pfad.hasSuffix("/teile-zusammenfuehren") {
            return (202, #"{"status":"läuft — das kann ein paar Minuten dauern"}"#)
        }
        return (204, "")
    }
}

/// Der Rumpf einer Anfrage. URLSession reicht ihn an ein URLProtocol oft
/// als Strom statt als Daten weiter.
private func rumpf(_ r: URLRequest) -> Data? {
    if let body = r.httpBody { return body }
    guard let strom = r.httpBodyStream else { return nil }
    strom.open(); defer { strom.close() }
    var daten = Data()
    var puffer = [UInt8](repeating: 0, count: 1024)
    while strom.hasBytesAvailable {
        let n = strom.read(&puffer, maxLength: puffer.count)
        if n <= 0 { break }
        daten.append(puffer, count: n)
    }
    return daten
}

private func titel(_ extra: String, kind: String = "series") throws -> Item {
    let json = #"{"id":"x","libraryId":"l","kind":"\#(kind)","title":"X","matchState":"matched","addedAt":"2026-10-23T13:35:52+02:00"\#(extra)}"#
    return try JSONDecoder.lumen.decode(Item.self, from: Data(json.utf8))
}

// MARK: - Jahre und Favoriten

final class JahreUndFavoritenTests: XCTestCase {

    func testJahresTextBeiSerien() throws {
        XCTAssertEqual(try titel(#","year":2014,"laeuftNoch":true"#).jahresText, "seit 2014")
        XCTAssertEqual(try titel(#","year":2014,"endYear":2020,"laeuftNoch":false"#).jahresText, "2014–2020")
        // Beendet im selben Jahr, oder ohne Endjahr: nur das eine Jahr.
        XCTAssertEqual(try titel(#","year":2014,"endYear":2014,"laeuftNoch":false"#).jahresText, "2014")
        XCTAssertEqual(try titel(#","year":2014,"laeuftNoch":false"#).jahresText, "2014")
        // Weiss der Server nicht, ob sie läuft, bleibt es beim Startjahr.
        XCTAssertEqual(try titel(#","year":2014,"endYear":2020"#).jahresText, "2014")
        XCTAssertNil(try titel("").jahresText)
    }

    func testJahresTextBeiFilmen() throws {
        // Ein Film ist nie „seit" — auch wenn ein Feld verirrt mitkäme.
        XCTAssertEqual(try titel(#","year":2019,"laeuftNoch":true"#, kind: "movie").jahresText, "2019")
        XCTAssertEqual(try titel(#","year":2019,"runtimeMs":7080000"#, kind: "movie").subtitleLine,
                       "2019 · 1 Std. 58 Min.")
    }

    func testUnterzeileNimmtDieSpanne() throws {
        let serie = try titel(#","year":2008,"endYear":2013,"laeuftNoch":false,"genres":["Drama"]"#)
        XCTAssertEqual(serie.subtitleLine, "2008–2013 · Drama")
    }

    func testFavorit() throws {
        XCTAssertFalse(try titel("").isFavorite, "fehlt das Feld, ist es kein Favorit")
        XCTAssertTrue(try titel(#","favorite":true"#).isFavorite)
    }
}

// MARK: - Personenseite

final class PersonTests: XCTestCase {

    func testPersonVollstaendig() throws {
        let json = """
        {"name":"Ethan Hawke","bio":"Ethan Green Hawke is an American actor.","bioSprache":"en",
         "geboren":"1970-11-06","geburtsort":"Austin, Texas",
         "quellen":[{"name":"TMDb","url":"https://www.themoviedb.org/person/569"},
                    {"name":"Wikipedia","url":"https://en.wikipedia.org/wiki/Ethan_Hawke"},
                    {"name":"Böse","url":"javascript:alert(1)"}],
         "bild":"/api/v1/persons/Ethan%20Hawke/bild"}
        """
        let p = try JSONDecoder.lumen.decode(PersonInfo.self, from: Data(json.utf8))
        XCTAssertEqual(p.lebensdaten, "geboren 6.11.1970 in Austin, Texas")
        XCTAssertTrue(p.bioNichtDeutsch)
        XCTAssertEqual(p.quellenText, "TMDb, Wikipedia, Böse")
        XCTAssertEqual(p.quellen?[0].link?.host, "www.themoviedb.org")
        XCTAssertNil(p.quellen?[2].link, "nur http und https werden ein Link")
    }

    func testPersonMitTodestag() throws {
        let json = #"{"name":"X","geboren":"1968-12-23","geburtsort":"Queens","gestorben":"2020-02-01","bio":"Text","bioSprache":"de"}"#
        let p = try JSONDecoder.lumen.decode(PersonInfo.self, from: Data(json.utf8))
        XCTAssertEqual(p.lebensdaten, "geboren 23.12.1968 in Queens · gestorben 1.2.2020")
        XCTAssertFalse(p.bioNichtDeutsch)
    }

    /// Kennt TMDb die Person nicht, schickt der Server nur den Namen.
    func testPersonNurName() throws {
        let p = try JSONDecoder.lumen.decode(PersonInfo.self, from: Data(#"{"name":"Niemand"}"#.utf8))
        XCTAssertEqual(p.lebensdaten, "")
        XCTAssertNil(p.biografie)
        XCTAssertFalse(p.bioNichtDeutsch)
        XCTAssertEqual(p.quellenText, "")
        XCTAssertEqual(PersonInfo.datum("1970"), "1970")
    }

    func testPfadTeil() {
        XCTAssertEqual(APIClient.pfadTeil("Tom Hanks"), "Tom%20Hanks")
        XCTAssertEqual(APIClient.pfadTeil("AC/DC"), "AC%2FDC")
        XCTAssertEqual(APIClient.pfadTeil("Jürgen Prochnow"), "J%C3%BCrgen%20Prochnow")
        XCTAssertEqual(APIClient.pfadTeil("Zendaya"), "Zendaya")
    }

    func testPersonBildMitSchluessel() async throws {
        let client = APIClient(baseURL: URL(string: "http://nas:8097")!, token: "GEHEIM")
        let mitBild = try JSONDecoder.lumen.decode(PersonInfo.self, from: Data(
            #"{"name":"Tom Hanks","bild":"/api/v1/persons/Tom%20Hanks/bild"}"#.utf8))
        let ohneBild = try JSONDecoder.lumen.decode(PersonInfo.self, from: Data(#"{"name":"Tom Hanks"}"#.utf8))

        let vorher = await client.personBildURL(mitBild)
        XCTAssertNil(vorher, "ohne Medienschlüssel keine Adresse")
        await client.setMedienSchluessel("K")
        let url = await client.personBildURL(mitBild)?.absoluteString
        XCTAssertEqual(url, "http://nas:8097/api/v1/persons/Tom%20Hanks/bild?key=K")
        XCTAssertFalse(url?.contains("GEHEIM") ?? true, "das Token steht nie in einer Adresse")
        let keins = await client.personBildURL(ohneBild)
        XCTAssertNil(keins)
    }

    func testPersonAnfrage() async throws {
        Aufzeichner.mitschnitt.leeren()
        let p = try await Aufzeichner.client().person("AC/DC")
        XCTAssertEqual(p.name, "Tom Hanks")
        let url = try XCTUnwrap(Aufzeichner.mitschnitt.alle.last?.url)
        XCTAssertEqual(url.absoluteString, "http://nas:8097/api/v1/persons/AC%2FDC")
    }
}

// MARK: - Raster, Merkliste, Favoriten

final class RasterAnfragenTests: XCTestCase {

    private func abfrage(_ r: URLRequest?) -> [String: String] {
        guard let url = r?.url, let teile = URLComponents(url: url, resolvingAgainstBaseURL: false) else { return [:] }
        var q: [String: String] = [:]
        for i in teile.queryItems ?? [] { q[i.name] = i.value ?? "" }
        return q
    }

    func testSortierungen() {
        XCTAssertEqual(Sortierung.allCases.map(\.rawValue), ["title", "added", "year", "year-asc"])
        XCTAssertEqual(Sortierung.titel.name(kind: "movie"), "A–Z")
        XCTAssertEqual(Sortierung.neueste.name(kind: "movie"), "Neueste Filme")
        XCTAssertEqual(Sortierung.aelteste.name(kind: "series"), "Älteste Serien")
        XCTAssertEqual(Buchstaben.alle.count, 27)
        XCTAssertEqual(Buchstaben.alle.first, Buchstaben.raute)
        XCTAssertEqual(Buchstaben.alle.last, "Z")
        XCTAssertEqual(Buchstaben.vorlesen(Buchstaben.raute, anzahl: 3), "Zahlen und Zeichen, 3 Titel")
        XCTAssertEqual(Buchstaben.vorlesen("Q", anzahl: 0), "Buchstabe Q")
        XCTAssertEqual(Raster.zeile(gesamt: 12, genre: "Drama", buchstabe: "B"), "12 Titel · Drama · B")
    }

    func testBuchstabeRauteWirdKodiert() async throws {
        Aufzeichner.mitschnitt.leeren()
        _ = try await Aufzeichner.client().seite(kind: "movie", sort: "title", buchstabe: Buchstaben.raute)
        let r = Aufzeichner.mitschnitt.alle.last
        let roh = try XCTUnwrap(r?.url?.absoluteString)
        XCTAssertTrue(roh.contains("buchstabe=%23"), roh)
        XCTAssertNil(r?.url?.fragment, "eine rohe Raute wäre der Anker der Adresse")
        XCTAssertEqual(abfrage(r)["buchstabe"], Buchstaben.raute)
        XCTAssertEqual(abfrage(r)["sort"], "title")
        XCTAssertEqual(r?.value(forHTTPHeaderField: "Authorization"), "Bearer GEHEIM")
    }

    func testBuchstabenAntwort() async throws {
        Aufzeichner.mitschnitt.leeren()
        let zahlen = try await Aufzeichner.client().buchstaben(kind: "movie", genre: "Drama")
        XCTAssertEqual(zahlen["A"], 12)
        XCTAssertEqual(zahlen[Buchstaben.raute], 3)
        let r = Aufzeichner.mitschnitt.alle.last
        XCTAssertEqual(r?.url?.path, "/api/v1/items/letters")
        XCTAssertEqual(abfrage(r), ["kind": "movie", "genre": "Drama"])
    }

    func testMerklisteUndFavoritenFragenDenServer() async throws {
        Aufzeichner.mitschnitt.leeren()
        let client = Aufzeichner.client()
        let fav = try await client.favoriten(kind: "series")
        XCTAssertTrue(fav.items.first?.isFavorite ?? false)
        let merk = try await client.merkliste()
        XCTAssertEqual(merk.gesamt, 1)

        let anfragen = Aufzeichner.mitschnitt.alle
        XCTAssertEqual(anfragen.count, 2)
        let a = abfrage(anfragen[0])
        XCTAssertEqual(a["liste"], "favoriten")
        XCTAssertEqual(a["kind"], "series")
        XCTAssertEqual(a["seite"], "1")
        XCTAssertEqual(a["limit"], "200")
        XCTAssertNil(a["sort"], "die Reihenfolge der Liste wählt der Server")
        let b = abfrage(anfragen[1])
        XCTAssertEqual(b["liste"], "merkliste")
        XCTAssertNil(b["kind"])
    }

    func testHerzSetzenUndNehmen() async throws {
        Aufzeichner.mitschnitt.leeren()
        let client = Aufzeichner.client()
        try await client.addFavorite("it_1")
        try await client.removeFavorite("it_1")
        let anfragen = Aufzeichner.mitschnitt.alle
        XCTAssertEqual(anfragen.map { $0.httpMethod ?? "" }, ["PUT", "DELETE"])
        XCTAssertEqual(anfragen.map { $0.url?.path ?? "" }, ["/api/v1/favorites/it_1", "/api/v1/favorites/it_1"])
    }

    @MainActor
    func testRasterSortiertUndSpringt() async throws {
        Aufzeichner.mitschnitt.leeren()
        let raster = Raster(client: Aufzeichner.client(), kind: "movie")
        await raster.laden(genre: nil)
        XCTAssertTrue(raster.zeigtBuchstaben)
        XCTAssertEqual(raster.anzahl("A"), 12)
        XCTAssertEqual(raster.anzahl("Q"), 0)
        XCTAssertEqual(Aufzeichner.mitschnitt.alle.map { $0.url?.path ?? "" },
                       ["/api/v1/items", "/api/v1/items/letters"])

        // Ein Buchstabe fragt nur die Titel neu, nicht die Zahlen.
        Aufzeichner.mitschnitt.leeren()
        await raster.springe(zu: "A")
        XCTAssertEqual(Aufzeichner.mitschnitt.alle.count, 1)
        XCTAssertEqual(abfrage(Aufzeichner.mitschnitt.alle.first)["buchstabe"], "A")
        XCTAssertEqual(raster.zeile, "1 Titel · A")

        // Anders sortiert: kein Buchstabe mehr, keine Leiste.
        Aufzeichner.mitschnitt.leeren()
        await raster.sortieren(.hinzugefuegt)
        XCTAssertNil(raster.buchstabe)
        XCTAssertFalse(raster.zeigtBuchstaben)
        let q = abfrage(Aufzeichner.mitschnitt.alle.first)
        XCTAssertEqual(q["sort"], "added")
        XCTAssertNil(q["buchstabe"])
        XCTAssertEqual(Aufzeichner.mitschnitt.alle.count, 1, "ohne A–Z keine Zahlen")
    }
}

// MARK: - Aufräumen: Fassungen und Teile

final class FassungenTests: XCTestCase {

    private func datei(_ id: String, teil: Int, extra: String = "") -> String {
        #"{"id":"\#(id)","itemId":"it_1","path":"/medien/Filme/\#(id).mkv","sizeBytes":4200000000,"partNo":\#(teil)\#(extra)}"#
    }

    private func duplikat(_ dateien: [String]) throws -> Duplikat {
        let json = #"{"itemId":"it_1","title":"Apocalypse Now","year":1979,"files":[\#(dateien.joined(separator: ","))]}"#
        return try JSONDecoder.lumen.decode(Duplikat.self, from: Data(json.utf8))
    }

    func testFassungMitWahl() throws {
        let d = try duplikat([
            datei("a", teil: 1, extra: #","height":1080,"container":"mkv","preferred":true"#),
            datei("b", teil: 1, extra: #","height":2160,"container":"mp4""#),
        ])
        XCTAssertFalse(d.sindTeile)
        XCTAssertTrue(d.files[0].isPreferred)
        XCTAssertFalse(d.files[1].isPreferred)
        XCTAssertEqual(d.files[0].formatZeile, "1080p · MKV · 4.2 GB")
        XCTAssertEqual(d.files[1].path, "/medien/Filme/b.mkv")
    }

    func testTeileOhneLuecke() throws {
        // Der Server schickt sie nicht zwingend der Reihe nach.
        XCTAssertTrue(try duplikat([datei("c", teil: 3), datei("a", teil: 1), datei("b", teil: 2)]).sindTeile)
        XCTAssertEqual(try duplikat([datei("b", teil: 2), datei("a", teil: 1)]).dateienGeordnet.map(\.id), ["a", "b"])
        XCTAssertFalse(try duplikat([datei("a", teil: 1), datei("c", teil: 3)]).sindTeile, "Lücke")
        XCTAssertFalse(try duplikat([datei("a", teil: 1)]).sindTeile, "eine Datei ist kein Stapel")
    }

    func testGroesse() {
        XCTAssertEqual(MediaFile.groesse(830_000_000), "830 MB")
        XCTAssertEqual(MediaFile.groesse(4_200_000_000), "4.2 GB")
    }

    func testVerwaltungsAnfragen() async throws {
        Aufzeichner.mitschnitt.leeren()
        let client = Aufzeichner.client()
        try await client.adminFassungWaehlen(itemId: "it_1", fileId: "f_2")
        try await client.adminDateiAbtrennen("f_3")
        let auftrag = try await client.adminTeileZusammenfuehren("it_1")
        XCTAssertTrue(auftrag.status.hasPrefix("läuft"))

        let anfragen = Aufzeichner.mitschnitt.alle
        XCTAssertEqual(anfragen.map { "\($0.httpMethod ?? "") \($0.url?.path ?? "")" }, [
            "PUT /api/v1/admin/items/it_1/fassung",
            "POST /api/v1/admin/files/f_3/abtrennen",
            "POST /api/v1/admin/items/it_1/teile-zusammenfuehren",
        ])
        let body = try XCTUnwrap(rumpf(anfragen[0]))
        let gelesen = try JSONDecoder().decode([String: String].self, from: body)
        XCTAssertEqual(gelesen, ["fileId": "f_2"])
    }
}
