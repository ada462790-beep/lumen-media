// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

import XCTest
#if os(iOS)
import UIKit
#endif

/// Gemeinsames für die Rundgänge im Simulator-Ablauf
/// (.github/workflows/simulator.yml): anmelden lassen, Elemente finden,
/// Bildschirmfotos machen.
///
/// Ein Rundgang ist kein Test, der etwas behauptet, sondern ein Gang durch
/// die Seiten mit einem Foto nach jedem Schritt. Was nicht gefunden wird,
/// steht im Bericht (`bericht.txt`) — der Rundgang geht trotzdem weiter.
/// Neben jedem Foto liegt der Elementbaum als Text, damit sich ein Schritt
/// ohne das Bild verbessern lässt.
class Rundgang: XCTestCase {

    let app = XCUIApplication()
    private var nummer = 0
    private var bericht: [String] = []
    /// Präfix für die Dateinamen, etwa „iphone" oder „ipad".
    var geraet = "geraet"

    override func setUp() {
        super.setUp()
        continueAfterFailure = true
    }

    /// Server, Name und Passwort kommen über die Umgebung des Testlaufs
    /// (TEST_RUNNER_LUMEN_TEST_SERVER usw., siehe simulator.yml) und gehen
    /// an die App weiter — die meldet sich damit selbst an (TestAnmeldung).
    func starten() {
        let umgebung = ProcessInfo.processInfo.environment
        for schluessel in ["LUMEN_TEST_SERVER", "LUMEN_TEST_NAME", "LUMEN_TEST_PASSWORT"] {
            if let wert = umgebung[schluessel] { app.launchEnvironment[schluessel] = wert }
        }
        if app.launchEnvironment["LUMEN_TEST_SERVER"] == nil {
            app.launchEnvironment["LUMEN_TEST_SERVER"] = "http://127.0.0.1:8097"
        }
        // Das Gerät spricht Schweizer Hochdeutsch, wie bei den meisten, die
        // Lumen nutzen — sonst stünden die Teile, die das System selbst
        // beschriftet (Suchfeld: „Cancel", „Clear text"), auf Englisch da.
        app.launchArguments += ["-AppleLanguages", "(de-CH)", "-AppleLocale", "de_CH"]
        app.launch()
    }

    // MARK: - Fotos

    func foto(_ name: String, _ notiz: String = "", ok: Bool = true) {
        nummer += 1
        let datei = String(format: "%02d-%@-%@", nummer, geraet, name)
        Thread.sleep(forTimeInterval: 0.8)
        #if os(macOS)
        let bild = app.windows.firstMatch.exists ? app.windows.firstMatch.screenshot() : XCUIScreen.main.screenshot()
        let png = bild.pngRepresentation
        let anhang = XCTAttachment(screenshot: bild)
        #elseif os(iOS)
        let bild = XCUIScreen.main.screenshot()
        let (png, aufrecht) = Self.aufrecht(bild)
        let anhang = XCTAttachment(image: aufrecht)
        #else
        let bild = XCUIScreen.main.screenshot()
        let png = bild.pngRepresentation
        let anhang = XCTAttachment(screenshot: bild)
        #endif
        anhang.name = datei
        anhang.lifetime = .keepAlways
        add(anhang)
        let baum = app.debugDescription
        let text = XCTAttachment(string: baum)
        text.name = datei + ".txt"
        text.lifetime = .keepAlways
        add(text)
        let zeile = "[\(ok ? "ok" : "--")] \(datei) \(notiz)"
        bericht.append(zeile)
        print(zeile)
        if let ordner = ProcessInfo.processInfo.environment["SIM_FOTOS"] {
            let basis = URL(fileURLWithPath: ordner)
            try? FileManager.default.createDirectory(at: basis, withIntermediateDirectories: true)
            try? png.write(to: basis.appendingPathComponent(datei + ".png"))
            try? baum.write(to: basis.appendingPathComponent(datei + ".txt"), atomically: true, encoding: .utf8)
            try? bericht.joined(separator: "\n")
                .write(to: basis.appendingPathComponent("bericht-\(geraet).txt"), atomically: true, encoding: .utf8)
        }
    }

    #if os(iOS)
    /// Im Querformat liefert der Simulator das Bild hochkant — das iPad lag
    /// in den Fotos auf der Seite. Hier wird es aufgerichtet.
    static func aufrecht(_ bild: XCUIScreenshot) -> (Data, UIImage) {
        let img = bild.image
        let lage = XCUIDevice.shared.orientation
        guard lage.isLandscape, img.size.height > img.size.width else { return (bild.pngRepresentation, img) }
        let groesse = CGSize(width: img.size.height, height: img.size.width)
        let format = UIGraphicsImageRendererFormat()
        format.scale = img.scale
        let neu = UIGraphicsImageRenderer(size: groesse, format: format).image { ctx in
            let c = ctx.cgContext
            c.translateBy(x: groesse.width / 2, y: groesse.height / 2)
            // Querformat links: die Oberseite des Bilds zeigt nach rechts.
            c.rotate(by: lage == .landscapeLeft ? -.pi / 2 : .pi / 2)
            img.draw(in: CGRect(x: -img.size.width / 2, y: -img.size.height / 2,
                                width: img.size.width, height: img.size.height))
        }
        return (neu.pngData() ?? bild.pngRepresentation, neu)
    }
    #endif

    func berichtAnhaengen() {
        let a = XCTAttachment(string: bericht.joined(separator: "\n"))
        a.name = "bericht-\(geraet).txt"
        a.lifetime = .keepAlways
        add(a)
    }

    // MARK: - Finden

    /// Alle Elemente, deren Beschriftung oder Kennung zum Muster passt
    /// (regulärer Ausdruck über den ganzen Text, ohne Gross/klein).
    func alle(_ muster: String, in wurzel: XCUIElement? = nil) -> XCUIElementQuery {
        // Nicht über `value`: am Mac liess das die Abfrage in eine Zeitüberschreitung
        // laufen (die Bedienungshilfen der App hingen danach).
        let p = NSPredicate(format: "label MATCHES[c] %@ OR identifier MATCHES[c] %@ OR title MATCHES[c] %@",
                            muster, muster, muster)
        return (wurzel ?? app).descendants(matching: .any).matching(p)
    }

    /// Das erste Element zum Muster — Knöpfe zuerst. Bewusst nur
    /// `firstMatch`: jede weitere Abfrage liest den ganzen Elementbaum neu,
    /// und auf dem Intel-Mac kostet das Sekunden.
    func element(_ muster: String, in wurzel: XCUIElement? = nil) -> XCUIElement? {
        let p = NSPredicate(format: "label MATCHES[c] %@ OR identifier MATCHES[c] %@", muster, muster)
        let knopf = (wurzel ?? app).buttons.matching(p).firstMatch
        if knopf.exists { return knopf }
        let e = alle(muster, in: wurzel).firstMatch
        return e.exists ? e : nil
    }

    @discardableResult
    func warte(_ muster: String, _ sekunden: TimeInterval = 15, in wurzel: XCUIElement? = nil) -> XCUIElement? {
        let ende = Date().addingTimeInterval(sekunden)
        repeat {
            if let e = element(muster, in: wurzel) { return e }
            Thread.sleep(forTimeInterval: 1)
        } while Date() < ende
        return nil
    }

    func druecken(_ e: XCUIElement) {
        #if os(macOS)
        // Nicht `click()` auf ein Element ausserhalb des Fensters: das
        // bricht den ganzen Rundgang ab („Unable to find hit point").
        // Erst hinscrollen, dann über Koordinaten klicken.
        let fenster = app.windows.firstMatch
        var n = 0
        while e.exists && !imFenster(e) && n < 6 {
            fensterScrollen(e.frame.midY > fenster.frame.midY ? -300 : 300)
            n += 1
        }
        if e.exists && !e.frame.isEmpty && imFenster(e) {
            fenster.coordinate(withNormalizedOffset: .zero)
                .withOffset(CGVector(dx: e.frame.midX - fenster.frame.minX, dy: e.frame.midY - fenster.frame.minY))
                .click()
        }
        #elseif os(tvOS)
        _ = fokussieren(e)
        XCUIRemote.shared.press(.select)
        #else
        // Liegt es ausserhalb des Bildschirms: hinscrollen. Nicht über
        // `isHittable` — das lässt den Test scheitern, wenn das Element
        // gerade keinen gültigen Rahmen hat (auf dem iPad gesehen).
        var n = 0
        while !sichtbar(e) && n < 4 {
            if e.exists && e.frame.minY > app.frame.midY { app.swipeUp() } else { app.swipeDown() }
            n += 1
        }
        if e.exists && !e.frame.isEmpty {
            app.coordinate(withNormalizedOffset: .zero)
                .withOffset(CGVector(dx: e.frame.midX, dy: e.frame.midY)).tap()
        }
        #endif
    }

    #if os(macOS)
    /// Ob die Mitte des Elements im Fenster liegt (unter der Titelleiste).
    func imFenster(_ e: XCUIElement) -> Bool {
        let f = app.windows.firstMatch.frame.insetBy(dx: 0, dy: 30)
        let r = e.frame
        return !r.isEmpty && f.contains(CGPoint(x: r.midX, y: r.midY))
    }

    /// Scrollen mit dem Mausrad über dem Inhalt rechts der Seitenleiste —
    /// über eine Koordinate, nicht über ein Element: `scroll` an Fenster
    /// oder ScrollView sucht einen Trefferpunkt und brach den Rundgang ab.
    func fensterScrollen(_ delta: CGFloat) {
        let fenster = app.windows.firstMatch
        guard fenster.exists else { return }
        fenster.coordinate(withNormalizedOffset: CGVector(dx: 0.62, dy: 0.55))
            .scroll(byDeltaX: 0, deltaY: delta)
        Thread.sleep(forTimeInterval: 0.4)
    }
    #endif

    /// Ob die Mitte des Elements auf dem Bildschirm liegt.
    func sichtbar(_ e: XCUIElement) -> Bool {
        guard e.exists else { return false }
        let f = e.frame
        return !f.isEmpty && app.frame.contains(CGPoint(x: f.midX, y: f.midY))
    }

    /// Ein Schritt: finden, drücken, warten, Foto.
    @discardableResult
    func schritt(_ name: String, _ muster: String, in wurzel: XCUIElement? = nil,
                 warten: TimeInterval = 3) -> Bool {
        guard let e = warte(muster, 8, in: wurzel) else {
            foto(name, "nicht gefunden: \(muster)", ok: false)
            return false
        }
        druecken(e)
        Thread.sleep(forTimeInterval: warten)
        foto(name)
        return true
    }

    /// Breaking Bad direkt aus den Serien geöffnet (nicht von einer Folge
    /// aus): S2 F1 ist angefangen (tools/simulator/einrichten.py), also soll
    /// Staffel 2 gewählt sein — wie im Browser, nicht Staffel 1. Die Folge
    /// S2 F1 („Sieben Dreissig Sieben") gibt es nur in Staffel 2. Am
    /// Fernseher dazu: hinunter auf die gewählte Staffel, dann auf die
    /// nächste Folge — danach wieder hinauf zu den Knöpfen.
    func staffelDerNaechstenFolge() {
        Thread.sleep(forTimeInterval: 2)
        let staffel2 = "^(Staffel 2|Season 2|2)$"
        let folge = ".*(Sieben Dreissig Sieben|Seven Thirty-Seven).*"
        let gewaehlt = alle(staffel2).matching(NSPredicate(format: "selected == true")).firstMatch.exists
        let folgeDa = alle(folge).firstMatch.exists
        foto("serie-direkt-staffel", "Staffel 2 gewählt: \(gewaehlt), Folge S2 F1 da: \(folgeDa)",
             ok: gewaehlt || folgeDa)
        #if os(tvOS)
        XCUIRemote.shared.press(.down)
        Thread.sleep(forTimeInterval: 1.2)
        let aufStaffel = fokus().map { $0.label.range(of: staffel2, options: .regularExpression) != nil } ?? false
        foto("serie-direkt-fokus-staffel", "Fokus: \(fokus()?.label ?? "keiner")", ok: aufStaffel)
        // Weiter hinunter — über „Staffel gesehen" hinweg — bis in die Folgen.
        var hinunter = 1
        var aufFolge = false
        for _ in 0..<3 {
            XCUIRemote.shared.press(.down)
            hinunter += 1
            Thread.sleep(forTimeInterval: 1.2)
            let name = fokus()?.label ?? ""
            aufFolge = name.range(of: folge, options: .regularExpression) != nil
            if aufFolge || name.range(of: "(?i)gesehen|watched", options: .regularExpression) == nil { break }
        }
        foto("serie-direkt-fokus-folge", "Fokus: \(fokus()?.label ?? "keiner")", ok: aufFolge)
        for _ in 0..<hinunter {
            XCUIRemote.shared.press(.up)
            Thread.sleep(forTimeInterval: 0.6)
        }
        #endif
    }

    func zurueck() {
        #if os(tvOS)
        XCUIRemote.shared.press(.menu)
        #elseif os(macOS)
        app.typeKey("[", modifierFlags: .command)
        #else
        // Der Zurück-Knopf — am iPad ist der erste Knopf der Leiste der für
        // die Seitenleiste.
        let knopf = app.navigationBars.buttons.matching(NSPredicate(
            format: "identifier == 'BackButton' OR label MATCHES[c] %@",
            "^(Zurück|Back|Filme|Serien|Startseite|Suchen|Ich|Filmreihen|Movies|Shows|Home|Search)$")).firstMatch
        if sichtbar(knopf) {
            knopf.tap()
        } else {
            // Wischen vom linken Rand, wie mit dem Daumen.
            let start = app.coordinate(withNormalizedOffset: CGVector(dx: 0.01, dy: 0.5))
            start.press(forDuration: 0.05, thenDragTo: app.coordinate(withNormalizedOffset: CGVector(dx: 0.8, dy: 0.5)))
        }
        #endif
        Thread.sleep(forTimeInterval: 1.5)
    }

    func nachUnten(_ mal: Int = 1) {
        for _ in 0..<mal {
            #if os(tvOS)
            XCUIRemote.shared.press(.down)
            #elseif os(macOS)
            fensterScrollen(-400)
            // Falls das Mausrad nicht ankommt: Bild ab.
            app.typeKey(.pageDown, modifierFlags: [])
            #else
            app.swipeUp()
            #endif
            Thread.sleep(forTimeInterval: 1)
        }
    }

    func nachOben(_ mal: Int = 1) {
        for _ in 0..<mal {
            #if os(tvOS)
            XCUIRemote.shared.press(.up)
            #elseif os(macOS)
            fensterScrollen(400)
            app.typeKey(.pageUp, modifierFlags: [])
            #else
            app.swipeDown()
            #endif
            Thread.sleep(forTimeInterval: 0.8)
        }
    }

    // MARK: - Fernbedienung (tvOS)

    #if os(tvOS)
    func fokus() -> XCUIElement? {
        let f = app.descendants(matching: .any).matching(NSPredicate(format: "hasFocus == true"))
        return f.count > 0 ? f.element(boundBy: f.count - 1) : nil
    }

    /// Mit den Pfeiltasten zum Element, wie mit der Fernbedienung.
    @discardableResult
    func fokussieren(_ ziel: XCUIElement, schritte: Int = 30) -> Bool {
        var letzter = CGRect.null
        var gleich = 0
        for _ in 0..<schritte {
            guard ziel.exists else { return false }
            if ziel.hasFocus { return true }
            guard let f = fokus() else {
                XCUIRemote.shared.press(.down)
                continue
            }
            let z = ziel.frame, ff = f.frame
            if ff.contains(CGPoint(x: z.midX, y: z.midY)) || z.contains(CGPoint(x: ff.midX, y: ff.midY)) {
                return true
            }
            let dx = z.midX - ff.midX, dy = z.midY - ff.midY
            var richtung: XCUIRemote.Button =
                abs(dy) > max(40, ff.height / 2) ? (dy > 0 ? .down : .up) : (dx > 0 ? .right : .left)
            if ff == letzter {
                gleich += 1
                if gleich >= 3 {
                    switch richtung {
                    case .down: richtung = .right
                    case .up: richtung = .left
                    case .right: richtung = .down
                    default: richtung = .up
                    }
                    gleich = 0
                }
            } else {
                gleich = 0
            }
            letzter = ff
            XCUIRemote.shared.press(richtung)
            Thread.sleep(forTimeInterval: 0.4)
        }
        return false
    }
    #endif
}
