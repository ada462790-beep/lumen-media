// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

import XCTest

/// Ein Gang durch die Apple-TV-App, nur mit der Fernbedienung
/// (Simulator-Ablauf, .github/workflows/simulator.yml).
final class LumenTVRundgang: Rundgang {

    private let fernbedienung = XCUIRemote.shared

    private func taste(_ knopf: XCUIRemote.Button, _ mal: Int = 1) {
        for _ in 0..<mal {
            fernbedienung.press(knopf)
            Thread.sleep(forTimeInterval: 0.6)
        }
    }

    /// Zu einem Reiter oben: erst hinauf zur Leiste, dann hin.
    @discardableResult
    private func reiter(_ name: String, _ muster: String) -> Bool {
        taste(.menu)
        Thread.sleep(forTimeInterval: 1)
        let p = NSPredicate(format: "label MATCHES[c] %@", muster)
        let knopf = app.tabBars.buttons.matching(p).firstMatch
        let ziel = knopf.waitForExistence(timeout: 4) ? knopf : element(muster)
        guard let ziel else {
            foto(name, "Reiter nicht gefunden: \(muster)", ok: false)
            return false
        }
        let angekommen = fokussieren(ziel)
        taste(.select)
        Thread.sleep(forTimeInterval: 2)
        // Vom Reiter hinunter in den Inhalt.
        taste(.down)
        Thread.sleep(forTimeInterval: 1.5)
        foto(name, angekommen ? "" : "Fokus kam nicht beim Reiter an", ok: angekommen)
        return true
    }

    func testRundgang() {
        geraet = "appletv"
        starten()
        let start = warte("^(Startseite|Home|Weiterschauen|Continue Watching)$", 45)
        Thread.sleep(forTimeInterval: 4)
        foto("startseite", start == nil ? "keine Startseite nach der Anmeldung" : "", ok: start != nil)
        taste(.down)
        foto("startseite-zeile-2")
        taste(.down, 2)
        foto("startseite-zeile-4")
        taste(.right, 2)
        foto("startseite-rechts")

        if reiter("filme", "^(Filme|Movies)$") {
            if schritt("film-inception", "^Inception.*") {
                taste(.down, 2)
                foto("film-inception-unten")
                zurueck()
            }
            if schritt("film-avatar", "^Avatar.*") {
                if let fassung = warte(".*(Fassung|Version).*", 3) {
                    druecken(fassung)
                    Thread.sleep(forTimeInterval: 1.5)
                    foto("film-avatar-fassung")
                    zurueck()
                }
                zurueck()
            }
        }

        if reiter("serien", "^(Serien|Shows|TV Shows)$") {
            if schritt("serie-breaking-bad", "^Breaking Bad.*") {
                staffelDerNaechstenFolge()
                taste(.down)
                foto("serie-runter")
                _ = schritt("serie-specials", "^(Specials|Staffel 0|Season 0|Extras).*")
                _ = schritt("serie-staffel-2", "^(Staffel 2|Season 2).*")
                taste(.down)
                foto("serie-folgen")
                zurueck()
            }
        }

        if reiter("suche", "^(Suche|Suchen|Search)$") {
            // Tippen mit der Bildschirmtastatur lässt der Rundgang aus;
            // das Foto zeigt, ob Suchfeld und Tastatur erreichbar sind.
            taste(.down)
            foto("suche-tastatur")
        }

        if reiter("ich", "^(Ich|Me|Profil|Profile|Admin.*)$") {
            taste(.down, 3)
            foto("ich-runter")
            if schritt("filmreihen", "^(Filmreihen|Franchises|Collections)$") {
                _ = schritt("filmreihe-dark-knight", ".*Dark Knight.*")
                zurueck()
                zurueck()
            }
            wuensche()
        }

        if reiter("startseite-wiedergabe", "^(Startseite|Home)$") {
            if schritt("film-interstellar", "^Interstellar.*") {
                if schritt("wiedergabe", "^(Weiterschauen|Fortsetzen|Weiter bei.*|Abspielen|Ansehen|Play|Resume.*|Continue.*)$",
                           warten: 8) {
                    taste(.select)
                    foto("wiedergabe-steuerung")
                    taste(.down)
                    foto("wiedergabe-spuren")
                    zurueck()
                }
            }
        }
        berichtAnhaengen()
    }

    /// Wünsche: die Seite, dann „Inception" wünschen. Das steht in der
    /// Bibliothek, also antwortet der Server mit 409 „schon_da", und die
    /// Seite zeigt „Ansehen" — das geht ohne Netz. Vorschläge von TMDb
    /// gibt es im Simulator nicht (kein Schlüssel); dann wünscht „Fertig"
    /// den Text gleich.
    private func wuensche() {
        guard schritt("wuensche", "^(Wünsche|Wishes|Requests)$") else { return }
        let feld = app.textFields.firstMatch
        guard feld.waitForExistence(timeout: 5) else {
            foto("wuensche-feld", "kein Eingabefeld", ok: false)
            zurueck()
            return
        }
        druecken(feld)
        Thread.sleep(forTimeInterval: 2)
        app.typeText("Inception")
        Thread.sleep(forTimeInterval: 1)
        foto("wuensche-tastatur")
        app.typeText("\n")
        Thread.sleep(forTimeInterval: 3)
        let ansehen = warte("^(Ansehen|View|Watch)$", 8)
        foto("wuensche-schon-da", ansehen == nil ? "kein „Ansehen“ nach dem Wunsch" : "", ok: ansehen != nil)
        if let ansehen {
            druecken(ansehen)
            Thread.sleep(forTimeInterval: 3)
            foto("wuensche-ansehen")
            zurueck()
        }
        zurueck()
    }
}
