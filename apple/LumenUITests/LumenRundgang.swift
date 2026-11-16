// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

import XCTest

/// Ein Gang durch iPhone-, iPad- und Mac-App mit Bildschirmfotos
/// (Simulator-Ablauf, .github/workflows/simulator.yml).
final class LumenRundgang: Rundgang {

    /// Am iPhone die Reiter unten, an iPad und Mac die Seitenleiste.
    private var telefon: Bool {
        #if os(iOS)
        return UIDevice.current.userInterfaceIdiom == .phone
        #else
        return false
        #endif
    }

    private func bereich(_ name: String, _ muster: String) -> Bool {
        #if os(iOS)
        if telefon {
            let p = NSPredicate(format: "label MATCHES[c] %@", muster)
            let knopf = app.tabBars.buttons.matching(p).firstMatch
            if knopf.waitForExistence(timeout: 5) {
                knopf.tap()
                Thread.sleep(forTimeInterval: 3)
                foto(name)
                return true
            }
        }
        #endif
        return schritt(name, muster)
    }

    func testRundgang() {
        #if os(iOS)
        geraet = telefon ? "iphone" : "ipad"
        if !telefon { XCUIDevice.shared.orientation = .landscapeLeft }
        #elseif os(macOS)
        geraet = "mac"
        #endif
        starten()

        let start = warte("^(Startseite|Home|Weiterschauen|Continue Watching)$", 45)
        Thread.sleep(forTimeInterval: 4)
        foto("startseite", start == nil ? "keine Startseite nach der Anmeldung" : "", ok: start != nil)
        nachUnten()
        foto("startseite-weiter-unten")
        nachUnten(2)
        foto("startseite-ganz-unten")
        nachOben(4)

        // Filme, Filmreihen
        if bereich("filme", "^(Filme|Movies)$") {
            if schritt("film-inception", "^Inception.*") {
                nachUnten()
                foto("film-inception-unten")
                nachUnten()
                foto("film-inception-ganz-unten")
                zurueck()
            }
            if schritt("film-avatar", "^Avatar.*") {
                if let fassung = warte("(?i).*(Fassung|Version).*", 3) {
                    druecken(fassung)
                    Thread.sleep(forTimeInterval: 1.5)
                    foto("film-avatar-fassung")
                    #if os(iOS)
                    app.swipeDown()
                    #else
                    app.typeKey(.escape, modifierFlags: [])
                    #endif
                }
                zurueck()
            }
            if schritt("film-der-pate", "^Der Pate.*") { zurueck() }
        }
        // Filmreihen: am iPhone über die Zeile auf der Startseite, an iPad
        // und Mac in der Seitenleiste.
        if telefon {
            if bereich("startseite-filmreihen", "^(Startseite|Home)$"),
               schritt("filmreihen", "^(Filmreihen|Franchises|Collections).*") {
                if schritt("filmreihe-dark-knight", ".*Dark Knight Collection.*") { zurueck() }
                zurueck()
            }
        } else if bereich("filmreihen", "^(Filmreihen|Franchises|Collections)$") {
            if schritt("filmreihe-dark-knight", ".*Dark Knight Collection.*") { zurueck() }
        }

        // Serien mit Staffelwahl
        if bereich("serien", "^(Serien|Shows|TV Shows)$") {
            if schritt("serie-breaking-bad", "^Breaking Bad.*") {
                staffelDerNaechstenFolge()
                nachUnten()
                foto("serie-folgen")
                if schritt("serie-staffelwahl", "^(Staffel 1|Season 1|Staffel 01).*") {
                    if schritt("serie-specials", "^(Specials|Staffel 0|Season 0|Extras).*") {
                        _ = schritt("serie-staffelwahl-2", "^(Specials|Staffel 0|Season 0|Extras).*", warten: 1.5)
                    }
                    _ = schritt("serie-staffel-2", "^(Staffel 2|Season 2).*")
                }
                zurueck()
            }
        }

        // Suche
        if bereich("suche", "^(Suchen|Suche|Search)$") {
            let feld = app.searchFields.firstMatch.exists ? app.searchFields.firstMatch : app.textFields.firstMatch
            if feld.waitForExistence(timeout: 5) {
                druecken(feld)
                feld.typeText("Dark")
                Thread.sleep(forTimeInterval: 3)
                foto("suche-dark")
                #if os(iOS)
                // Die Tastatur wieder weg — sie lag sonst über der Leiste
                // unten, und „Ich" liess sich nicht mehr tippen.
                feld.typeText("\n")
                Thread.sleep(forTimeInterval: 1)
                #endif
            }
        }

        // Profil und Einstellungen
        if bereich("ich", "^(Ich|Me|Profil|Profile)$") {
            nachUnten()
            foto("ich-unten")
            nachUnten(2)
            foto("ich-ganz-unten")
            // Am iPhone steht „Einstellungen" unten unter „Ich"; an iPad
            // und Mac ist „Profil" schon die Einstellungsseite — dort geht
            // es eine Ebene tiefer, zu „Profil schützen".
            let ziel = telefon ? "^(Einstellungen|Settings)$" : "^(Profil schützen|Protect profile).*"
            if schritt("einstellungen", ziel) { zurueck() }
        }

        // Wiedergabe aus „Weiterschauen"
        if bereich("startseite-wiedergabe", "^(Startseite|Home)$") {
            if schritt("film-interstellar", "^Interstellar.*") {
                if schritt("wiedergabe", "^(Weiterschauen|Fortsetzen|Weiter bei.*|Abspielen|Ansehen|Play|Resume.*|Continue.*)$",
                           warten: 12) {
                    #if os(iOS)
                    app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.5)).tap()
                    #endif
                    Thread.sleep(forTimeInterval: 1)
                    foto("wiedergabe-steuerung")
                }
            }
        }
        berichtAnhaengen()
    }
}
