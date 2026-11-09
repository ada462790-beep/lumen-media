// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

import XCTest
@testable import LumenKit

// Die Liste der Server. Geprüft wird vor allem, was niemand merken soll:
// dass eine App, die bisher genau einen Server kannte, nach dem Update
// immer noch angemeldet ist — und dass die Tokens zweier Server nie
// durcheinandergeraten.
//
// Ohne Schlüsselbund: auf einem Prüfrechner kann er gesperrt sein. Das
// Geheimfach im Speicher verhält sich für diese Fragen genauso.

final class ServerListeTests: XCTestCase {

    private var suite = ""
    private var defaults: UserDefaults!

    override func setUp() {
        super.setUp()
        suite = "lumen.test." + UUID().uuidString
        defaults = UserDefaults(suiteName: suite)!
    }

    override func tearDown() {
        defaults.removePersistentDomain(forName: suite)
        defaults = nil
        super.tearDown()
    }

    private func url(_ text: String) -> URL { URL(string: text)! }

    // MARK: - Übernahme

    func testAlteEinzelDatenWerdenUebernommen() {
        let abdruck = String(repeating: "ab", count: 32)
        defaults.set(url("http://nas.local:8097"), forKey: "lumen.server")
        defaults.set(abdruck, forKey: "lumen.fingerabdruck")
        let fach = SpeicherGeheimfach(["lumen.token": "geheim-1"])

        let store = TokenStore(fach: fach, defaults: defaults)
        let liste = store.server

        XCTAssertEqual(liste.count, 1)
        let e = liste[0]
        XCTAssertEqual(e.adresse, url("http://nas.local:8097"))
        XCTAssertEqual(e.fingerabdruck, abdruck)
        XCTAssertEqual(e.name, "nas")
        // Der zuletzt benutzte ist der übernommene — der Start geht dorthin.
        XCTAssertEqual(store.zuletztBenutzt, e.id)
        // Das Token ist umgezogen, niemand muss sich neu anmelden.
        XCTAssertEqual(store.token(fuer: e.id), "geheim-1")
        XCTAssertNil(fach.lesen("lumen.token"), "das alte Token bleibt nicht doppelt liegen")
        // Die alten Einstellungen sind aufgeräumt.
        XCTAssertNil(defaults.object(forKey: "lumen.server"))
        XCTAssertNil(defaults.object(forKey: "lumen.fingerabdruck"))
    }

    func testUebernahmeNurEinmal() {
        defaults.set(url("http://192.168.1.20:8097"), forKey: "lumen.server")
        let fach = SpeicherGeheimfach(["lumen.token": "t"])
        let erster = TokenStore(fach: fach, defaults: defaults).server
        // Ein zweiter Zugriff — etwa nach einem Neustart — legt nichts dazu.
        let zweiter = TokenStore(fach: fach, defaults: defaults).server
        XCTAssertEqual(erster, zweiter)
        XCTAssertEqual(zweiter.count, 1)
        XCTAssertEqual(zweiter[0].name, "192.168.1.20")
    }

    func testOhneAlteDatenLeereListe() {
        let store = TokenStore(fach: SpeicherGeheimfach(), defaults: defaults)
        XCTAssertTrue(store.server.isEmpty)
        XCTAssertNil(store.zuletztBenutzt)
    }

    func testAlteAdresseOhneToken() {
        defaults.set(url("http://lumen.local:8097"), forKey: "lumen.server")
        let store = TokenStore(fach: SpeicherGeheimfach(), defaults: defaults)
        XCTAssertEqual(store.server.count, 1)
        XCTAssertEqual(store.server[0].name, "Lumen")
        XCTAssertNil(store.server[0].fingerabdruck)
        XCTAssertNil(store.token(fuer: store.server[0].id))
    }

    /// War der Schlüsselbund bei der Übernahme nicht lesbar, liegt das
    /// Token noch unter dem alten Namen — es wird später nachgeholt.
    func testTokenWirdNachgeholt() {
        defaults.set(url("http://nas:8097"), forKey: "lumen.server")
        let fach = SpeicherGeheimfach()
        let store = TokenStore(fach: fach, defaults: defaults)
        let id = store.server[0].id
        XCTAssertNil(store.token(fuer: id))

        // Jetzt ist es da (der Schlüsselbund ist wieder offen).
        fach.schreiben("spaet", konto: "lumen.token")
        XCTAssertEqual(store.token(fuer: id), "spaet")
        XCTAssertEqual(fach.konten, ["lumen.token." + id])
    }

    // MARK: - Token je Server

    func testTokensJeServerGetrennt() {
        let fach = SpeicherGeheimfach()
        let store = TokenStore(fach: fach, defaults: defaults)
        let a = ServerEintrag(name: "Zuhause", adresse: url("http://nas:8097"))
        let b = ServerEintrag(name: "Eltern", adresse: url("https://vermittler.example:8443"),
                              fingerabdruck: String(repeating: "0f", count: 32))
        store.speichern(a)
        store.speichern(b)

        store.setzeToken("ta", fuer: a.id)
        store.setzeToken("tb", fuer: b.id)
        XCTAssertEqual(store.token(fuer: a.id), "ta")
        XCTAssertEqual(store.token(fuer: b.id), "tb")

        // Abmelden beim einen lässt den anderen in Ruhe.
        store.setzeToken(nil, fuer: a.id)
        XCTAssertNil(store.token(fuer: a.id))
        XCTAssertEqual(store.token(fuer: b.id), "tb")

        XCTAssertEqual(store.server.map(\.name), ["Zuhause", "Eltern"])
        XCTAssertFalse(store.server[0].vonUnterwegs)
        XCTAssertTrue(store.server[1].vonUnterwegs)
    }

    func testEntfernenNimmtDasTokenMit() {
        let fach = SpeicherGeheimfach()
        let store = TokenStore(fach: fach, defaults: defaults)
        let a = ServerEintrag(name: "A", adresse: url("http://a:8097"))
        let b = ServerEintrag(name: "B", adresse: url("http://b:8097"))
        store.speichern(a)
        store.speichern(b)
        store.setzeToken("ta", fuer: a.id)
        store.setzeToken("tb", fuer: b.id)
        store.zuletztBenutzt = a.id

        store.entfernen(id: a.id)

        XCTAssertEqual(store.server.map(\.id), [b.id])
        XCTAssertNil(store.token(fuer: a.id))
        XCTAssertEqual(store.token(fuer: b.id), "tb")
        XCTAssertNil(store.zuletztBenutzt, "ein vergessener Server ist nicht mehr der letzte")
        XCTAssertEqual(fach.konten, ["lumen.token." + b.id])
    }

    func testEntfernenDesUebernommenenRaeumtAuf() {
        defaults.set(url("http://nas:8097"), forKey: "lumen.server")
        let fach = SpeicherGeheimfach()
        let store = TokenStore(fach: fach, defaults: defaults)
        let id = store.server[0].id
        // Das alte Token taucht erst nach der Übernahme auf …
        fach.schreiben("alt", konto: "lumen.token")
        // … und darf nach dem Entfernen nicht liegen bleiben.
        store.entfernen(id: id)
        XCTAssertTrue(fach.konten.isEmpty)
    }

    func testUmbenennen() {
        let store = TokenStore(fach: SpeicherGeheimfach(), defaults: defaults)
        let a = ServerEintrag(name: "nas", adresse: url("http://nas:8097"))
        store.speichern(a)
        store.umbenennen(id: a.id, name: "  Wohnzimmer  ")
        XCTAssertEqual(store.server[0].name, "Wohnzimmer")
        // Leer heisst: so lassen.
        store.umbenennen(id: a.id, name: "   ")
        XCTAssertEqual(store.server[0].name, "Wohnzimmer")
    }

    /// Der Name vom Server gilt, bis der Nutzer selbst umbenennt.
    func testNameVomServer() {
        let store = TokenStore(fach: SpeicherGeheimfach(), defaults: defaults)
        let a = ServerEintrag(name: "nas", adresse: url("http://nas:8097"))
        store.speichern(a)
        XCTAssertTrue(store.nameVomServer(id: a.id, name: "Keller"))
        XCTAssertEqual(store.server[0].name, "Keller")
        XCTAssertFalse(store.nameVomServer(id: a.id, name: ""), "leer ändert nichts")
        XCTAssertFalse(store.nameVomServer(id: a.id, name: "Keller"), "gleich ändert nichts")
        store.umbenennen(id: a.id, name: "Bei Oma")
        XCTAssertFalse(store.nameVomServer(id: a.id, name: "Dachboden"))
        XCTAssertEqual(store.server[0].name, "Bei Oma")
    }

    /// Eine Liste aus der Fassung ohne `selbstBenannt` bleibt lesbar.
    func testAlteListeOhneSelbstBenannt() throws {
        let alt = #"[{"id":"s1","name":"nas","adresse":"http://nas:8097"}]"#
        defaults.set(Data(alt.utf8), forKey: "lumen.serverliste")
        let store = TokenStore(fach: SpeicherGeheimfach(), defaults: defaults)
        XCTAssertEqual(store.server.map(\.id), ["s1"])
        XCTAssertFalse(store.server[0].selbstBenannt)
    }

    func testSpeichernErsetztGleicheKennung() {
        let store = TokenStore(fach: SpeicherGeheimfach(), defaults: defaults)
        var a = ServerEintrag(name: "A", adresse: url("http://a:8097"))
        store.speichern(a)
        a.fingerabdruck = String(repeating: "11", count: 32)
        store.speichern(a)
        XCTAssertEqual(store.server.count, 1)
        XCTAssertNotNil(store.server[0].fingerabdruck)
    }

    // MARK: - Namen und Anzeige

    func testNamensvorschlag() {
        XCTAssertEqual(ServerEintrag.vorschlag(fuer: url("http://nas.local:8097")), "nas")
        XCTAssertEqual(ServerEintrag.vorschlag(fuer: url("http://lumen.local:8097")), "Lumen")
        XCTAssertEqual(ServerEintrag.vorschlag(fuer: url("http://10.0.0.5:8097")), "10.0.0.5")
        // „Lumen" aus /hello unterscheidet nichts, ein eigener Name schon.
        XCTAssertEqual(ServerEintrag.vorschlag(fuer: url("http://nas:8097"), name: "Lumen"), "nas")
        XCTAssertEqual(ServerEintrag.vorschlag(fuer: url("http://nas:8097"), name: ""), "nas")
        XCTAssertEqual(ServerEintrag.vorschlag(fuer: url("http://nas:8097"), name: "Keller"), "Keller")
    }

    func testAdresseKurz() {
        XCTAssertEqual(ServerEintrag(name: "x", adresse: url("http://nas:8097")).adresseKurz, "nas:8097")
        XCTAssertEqual(ServerEintrag(name: "x", adresse: url("https://v.example:8443")).adresseKurz,
                       "https://v.example:8443")
    }

    // MARK: - Code

    func testAnmeldecode() {
        XCTAssertEqual(Anmeldecode.lesbar("482913"), "482 913")
        XCTAssertEqual(Anmeldecode.nurZiffern("482 913"), "482913")
        XCTAssertEqual(Anmeldecode.nurZiffern("48-29-13-77"), "482913")
        XCTAssertTrue(Anmeldecode.vollstaendig("482 913"))
        XCTAssertFalse(Anmeldecode.vollstaendig("48291"))
        let jetzt = Date()
        XCTAssertEqual(Anmeldecode.restzeit(bis: jetzt.addingTimeInterval(277), jetzt: jetzt), "4:37")
        XCTAssertEqual(Anmeldecode.restzeit(bis: jetzt.addingTimeInterval(-5), jetzt: jetzt), "0:00")
    }

    func testCodeAntwortenLesen() throws {
        let start = try JSONDecoder.lumen.decode(CodeStart.self, from: Data(
            #"{"code":"004211","geheim":"abc_-x","gueltigSek":300}"#.utf8))
        XCTAssertEqual(start.code, "004211")
        XCTAssertEqual(start.gueltigSek, 300)

        let wartet = try JSONDecoder.lumen.decode(CodeStand.self, from: Data(#"{"status":"wartet"}"#.utf8))
        XCTAssertNil(wartet.token)

        let fertig = try JSONDecoder.lumen.decode(CodeStand.self, from: Data(
            #"{"token":"tok","account":{"id":"a1","name":"Mara"}}"#.utf8))
        XCTAssertEqual(fertig.token, "tok")

        let frei = try JSONDecoder.lumen.decode(CodeFreigabe.self, from: Data(#"{"geraet":"Apple TV"}"#.utf8))
        XCTAssertEqual(frei.geraet, "Apple TV")
    }
}
