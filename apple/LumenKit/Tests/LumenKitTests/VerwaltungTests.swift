// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

import XCTest
@testable import LumenKit

// Die Verwaltung am Mac: Updates, Gruppen, Takt und Metadaten der
// Bibliotheken, Zuweisen eines Titels — und die Bestätigung, die der
// Server beim Ändern von Rechten verlangt.

// MARK: - Ein Server, der eine Bestätigung verlangt

/// Antwortet wie `bestaetigt` in gruppen.go: ohne Kopf 428, mit falschem
/// Code 403, mit „1234" 204 — jeweils mit `code: "bestaetigen"`.
final class BestaetigungsServer: URLProtocol {
    static let mitschnitt = Mitschnitt()

    static func client() -> APIClient {
        let aufbau = URLSessionConfiguration.ephemeral
        aufbau.protocolClasses = [BestaetigungsServer.self]
        return APIClient(baseURL: URL(string: "http://nas:8097")!, token: "GEHEIM",
                         session: URLSession(configuration: aufbau))
    }

    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }

    override func startLoading() {
        Self.mitschnitt.merken(request)
        let status: Int
        let text: String
        switch request.value(forHTTPHeaderField: APIClient.bestaetigungHeader) {
        case nil:
            status = 428
            text = #"{"error":"bitte zur bestätigung pin, passwort oder code eingeben","code":"bestaetigen"}"#
        case "1234":
            status = 204
            text = ""
        default:
            status = 403
            text = #"{"error":"das stimmt nicht","code":"bestaetigen"}"#
        }
        let http = HTTPURLResponse(url: request.url!, statusCode: status, httpVersion: "HTTP/1.1",
                                   headerFields: ["Content-Type": "application/json"])!
        client?.urlProtocol(self, didReceive: http, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: Data(text.utf8))
        client?.urlProtocolDidFinishLoading(self)
    }

    override func stopLoading() {}
}

final class BestaetigungTests: XCTestCase {

    func testOhneCodeFragtDerServerNach() async {
        BestaetigungsServer.mitschnitt.leeren()
        let client = BestaetigungsServer.client()
        do {
            try await client.adminSetAccountGroup("acc_1", groupId: "g_1")
            XCTFail("ohne Bestätigung darf das nicht durchgehen")
        } catch LumenError.bestaetigen(let falsch) {
            XCTAssertFalse(falsch)
        } catch {
            XCTFail("falscher Fehler: \(error)")
        }
        let r = BestaetigungsServer.mitschnitt.alle.last
        XCTAssertNil(r?.value(forHTTPHeaderField: APIClient.bestaetigungHeader))
        XCTAssertEqual(r?.url?.path, "/api/v1/admin/accounts/acc_1/group")
        XCTAssertEqual(r?.httpMethod, "PUT")
    }

    func testFalscherCodeHeisstNochEinmal() async {
        let client = BestaetigungsServer.client()
        do {
            try await APIClient.bestaetigung.withValue("0000") {
                try await client.adminDeleteGroup("g_1")
            }
            XCTFail("ein falscher Code darf nicht durchgehen")
        } catch LumenError.bestaetigen(let falsch) {
            XCTAssertTrue(falsch)
            XCTAssertEqual(LumenError.bestaetigen(falsch: true).errorDescription, "Das stimmt nicht.")
        } catch {
            XCTFail("falscher Fehler: \(error)")
        }
    }

    func testCodeGehtNurMitDerEinenAnfrage() async throws {
        BestaetigungsServer.mitschnitt.leeren()
        let client = BestaetigungsServer.client()
        try await APIClient.bestaetigung.withValue("1234") {
            try await client.adminDeleteGroup("g_1")
        }
        let mit = BestaetigungsServer.mitschnitt.alle.last
        XCTAssertEqual(mit?.value(forHTTPHeaderField: APIClient.bestaetigungHeader), "1234")
        XCTAssertEqual(mit?.url?.path, "/api/v1/admin/groups/g_1")
        XCTAssertEqual(mit?.httpMethod, "DELETE")

        // Danach wieder ohne: der Code hängt an der Aufgabe, nicht am Client.
        do {
            try await client.adminDeleteGroup("g_2")
            XCTFail("ohne Bestätigung darf das nicht durchgehen")
        } catch LumenError.bestaetigen(let falsch) {
            XCTAssertFalse(falsch)
        }
        XCTAssertNil(BestaetigungsServer.mitschnitt.alle.last?.value(forHTTPHeaderField: APIClient.bestaetigungHeader))
    }
}

// MARK: - Was die Verwaltung liest

final class VerwaltungDecodingTests: XCTestCase {

    func testBibliothekMitTaktErgebnisUndMetadaten() throws {
        let json = """
        {"id":"lib_1","name":"Filme","kind":"movie","path":"/volume1/video/_filme","language":"de",
         "scannedAt":"2026-11-07T04:00:12+01:00","count":812,"intervalMin":1440,"scanAt":"04:00",
         "lastResult":{"at":"2026-11-07T04:00:12+01:00","added":3,"updated":0,"missing":1,
                       "unmatched":0,"tookMs":5321,"auto":true},
         "scanning":false,
         "stand":{"laeuft":false,"gesamt":0,"gefunden":0,"fertig":0,"datei":"","seitMs":0},
         "erreichbar":true,
         "metadaten":{"laeuft":true,"library":"lib_1","gesamt":40,"fertig":10,"titel":"Alien"},
         "wartet":false}
        """
        let lib = try JSONDecoder.lumen.decode(Library.self, from: Data(json.utf8))
        XCTAssertEqual(lib.taktWahl, "zeit")
        XCTAssertEqual(lib.lastResult?.text, "von selbst: 3 neu, 1 fehlt")
        XCTAssertEqual(lib.metadaten?.anteil ?? 0, 0.25, accuracy: 0.001)
        XCTAssertTrue(lib.beschaeftigt)
    }

    func testAlteBibliothekOhneNeueFelder() throws {
        let json = """
        {"id":"lib_1","name":"Serien","kind":"series","path":"/s","language":"de","count":0}
        """
        let lib = try JSONDecoder.lumen.decode(Library.self, from: Data(json.utf8))
        XCTAssertEqual(lib.taktWahl, "0")
        XCTAssertNil(lib.lastResult)
        XCTAssertFalse(lib.beschaeftigt)
    }

    func testScanErgebnisOhneNeues() throws {
        let json = #"{"at":"2026-11-07T04:00:12Z","added":0,"updated":0,"missing":0,"unmatched":0,"tookMs":1,"auto":false}"#
        let e = try JSONDecoder.lumen.decode(ScanErgebnis.self, from: Data(json.utf8))
        XCTAssertEqual(e.text, "auf Knopfdruck: nichts Neues")
    }

    func testUpdateStandMitNeuererFassung() throws {
        let json = """
        {"installiert":"1.3.0","container":false,
         "einstellung":{"pruefen":"taeglich","pruefZeit":"04:00","auto":"fenster","fensterVon":"03:00","fensterBis":"05:00"},
         "protokoll":[{"at":"2026-11-06T03:10:00+01:00","von":"1.2.0","nach":"1.3.0","ergebnis":"ok",
                       "notiz":"eingesetzt, Neustart","auto":true}],
         "laeuft":false,"wiedergaben":2,"tokenGesetzt":true,"tokenQuelle":"verwaltung",
         "tokenUngueltig":false,"releaseSeite":"https://github.com/ada462790-beep/lumen-media/releases",
         "geprueft":"2026-11-07T04:00:01.5+01:00",
         "neueste":{"version":"1.4.0","tag":"v1.4.0","url":"https://github.com/ada462790-beep/lumen-media/releases/tag/v1.4.0",
                    "published":"2026-11-05T12:00:00Z"},
         "neuer":true}
        """
        let u = try JSONDecoder.lumen.decode(UpdateStand.self, from: Data(json.utf8))
        XCTAssertTrue(u.gibtNeueres)
        XCTAssertEqual(u.kurz, "1.4.0 verfügbar")
        XCTAssertEqual(u.einstellung.auto, "fenster")
        XCTAssertEqual(u.protokoll?.first?.ergebnisText, "eingesetzt")
        XCTAssertNil(u.anleitung)
    }

    func testUpdateStandImContainerOhnePruefung() throws {
        let json = """
        {"installiert":"1.3.0","container":true,
         "einstellung":{"pruefen":"aus","pruefZeit":"04:00","auto":"aus","fensterVon":"03:00","fensterBis":"05:00"},
         "protokoll":[],"laeuft":false,"wiedergaben":0,"tokenGesetzt":false,"tokenQuelle":"",
         "tokenUngueltig":true,"releaseSeite":"https://github.com/ada462790-beep/lumen-media/releases",
         "fehler":"github: 401","anleitung":"docker compose pull && docker compose up -d"}
        """
        let u = try JSONDecoder.lumen.decode(UpdateStand.self, from: Data(json.utf8))
        XCTAssertFalse(u.gibtNeueres)
        XCTAssertEqual(u.kurz, "Prüfen klappte nicht")
        XCTAssertTrue(u.tokenUngueltig)
        XCTAssertNotNil(u.anleitung)
    }

    func testGruppeUndKontoMitGruppe() throws {
        let gruppe = """
        {"id":"g_1","name":"Familie","verwaltung":true,"libraries":["lib_1","lib_2"],
         "limits":{"maxAge":16,"allowUnrated":false,"maxStreams":3,"allowTranscode":true,
                   "allowDownload":true,"maxDownloads":20,"remoteQuality":"1080p"},"mitglieder":1}
        """
        let g = try JSONDecoder.lumen.decode(Gruppe.self, from: Data(gruppe.utf8))
        XCTAssertEqual(g.zeile, "1 Person · 2 Bibliotheken · höchstens 3 gleichzeitig")

        // Go schickt eine Gruppe ohne Bibliotheken als null.
        let leer = gruppe.replacingOccurrences(of: #"["lib_1","lib_2"]"#, with: "null")
        XCTAssertEqual(try JSONDecoder.lumen.decode(Gruppe.self, from: Data(leer.utf8)).bibliotheken, [])

        let konto = """
        {"id":"acc_2","name":"Mia","avatar":"fuchs","isOwner":false,"isKid":false,"lockKind":"pin",
         "disabled":false,"createdAt":"2026-10-01T10:00:00Z","isAdmin":true,"groupId":"g_1",
         "groupName":"Familie","limits":{"maxAge":16,"allowUnrated":false,"maxStreams":3,
         "allowTranscode":true,"allowDownload":true,"maxDownloads":20,"remoteQuality":"1080p"},
         "libraries":["lib_1"],"devices":2}
        """
        let k = try JSONDecoder.lumen.decode(KontoEintrag.self, from: Data(konto.utf8))
        XCTAssertEqual(k.groupId, "g_1")
    }

    /// Die Grenzen gehen mit genau den Namen zum Server, die er liest.
    func testGrenzenGehenMitDenNamenDesServers() throws {
        let daten = try JSONEncoder().encode(Limits.vorgabe)
        let feld = try XCTUnwrap(try JSONSerialization.jsonObject(with: daten) as? [String: Any])
        XCTAssertEqual(Set(feld.keys), ["maxAge", "allowUnrated", "maxStreams", "allowTranscode",
                                        "allowDownload", "maxDownloads", "remoteQuality"])
        XCTAssertEqual(feld["maxStreams"] as? Int, 2)
    }

    func testTrefferDerSuche() throws {
        let json = """
        [{"sourceId":"tmdb:348","title":"Alien","original":"Alien","year":1979,
          "overview":"Die Nostromo …","poster":"https://image.tmdb.org/t/p/w185/x.jpg"},
         {"sourceId":"tmdb:1","title":"Ohne Jahr"}]
        """
        let t = try JSONDecoder.lumen.decode([TitelTreffer].self, from: Data(json.utf8))
        XCTAssertEqual(t.map(\.id), ["tmdb:348", "tmdb:1"])
        XCTAssertNil(t[1].year)
    }
}
