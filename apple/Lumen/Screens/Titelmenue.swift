// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

import SwiftUI
import LumenKit

// Das Menü an jeder Karte — im Browser die Karte, die beim Drüberfahren
// aufklappt (app.js: hoverKarteZeigen) mit „Abspielen", „Merken" und
// „Info". Hier ist es das, was auf dem jeweiligen Gerät üblich ist: auf
// dem iPhone und iPad ein langer Druck, auf dem Mac ein Rechtsklick.
// „Info" ist der gewöhnliche Tipp auf die Karte.
//
// Abgespielt wird über das Gerüst (`LibraryShell`): es hält den einen
// Player, den jede Karte über `\.abspielen` aufruft — so braucht nicht
// jede Karte ihr eigenes Vollbild.

private struct AbspielenSchluessel: EnvironmentKey {
    static let defaultValue: (@MainActor (Abspielstart) -> Void)? = nil
}

extension EnvironmentValues {
    /// Einen Titel sofort abspielen — gesetzt vom Gerüst. Eine Serie
    /// beginnt bei ihrer ersten offenen Folge.
    var abspielen: (@MainActor (Abspielstart) -> Void)? {
        get { self[AbspielenSchluessel.self] }
        set { self[AbspielenSchluessel.self] = newValue }
    }
}

extension View {
    /// Das Kontextmenü einer Karte: abspielen, merken.
    func titelMenue(_ item: Item) -> some View {
        modifier(TitelMenue(item: item))
    }
}

private struct TitelMenue: ViewModifier {
    @Environment(Session.self) private var session
    @Environment(\.abspielen) private var abspielen
    let item: Item
    /// Nach dem Merken hier weiterzählen, bis die Seite neu lädt — sonst
    /// stünde im Menü bis dahin das Falsche.
    @State private var gemerkt: Bool?

    private var aufDerListe: Bool { gemerkt ?? item.isOnList }
    private var spielbar: Bool { ["movie", "series", "episode"].contains(item.kind) }

    func body(content: Content) -> some View {
        content.contextMenu {
            if spielbar, let abspielen {
                Button {
                    abspielen(Abspielstart(item: item))
                } label: {
                    Label(item.kind == "series" ? Titelseite.serieAbspielen(item) : Titelseite.abspielen(item),
                          systemImage: "play.fill")
                }
                if item.kind != "series", Titelseite.kannVonVorn(item) {
                    Button {
                        abspielen(Abspielstart(item: item, vonVorn: true))
                    } label: {
                        Label("Von vorn", systemImage: "backward.end")
                    }
                }
            }
            if item.kind == "movie" || item.kind == "series" {
                Button {
                    Task { await merken() }
                } label: {
                    Label(aufDerListe ? "Aus der Merkliste" : "Merken",
                          systemImage: aufDerListe ? "bookmark.fill" : "bookmark")
                }
            }
        }
    }

    private func merken() async {
        let vorher = aufDerListe
        do {
            if vorher { try await session.client.removeFromWatchlist(item.id) }
            else { try await session.client.addToWatchlist(item.id) }
            gemerkt = !vorher
        } catch {
            // Ging es nicht, bleibt es, wie es war — die Titelseite sagt
            // beim nächsten Öffnen, was gilt.
        }
    }
}
