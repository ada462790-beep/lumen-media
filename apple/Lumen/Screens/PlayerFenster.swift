// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

import SwiftUI
import LumenKit

// Wo der Player erscheint.
//
// Auf iPhone und iPad bedeckt er alles (`fullScreenCover`). Auf dem Mac
// bekommt er ein eigenes Fenster: das lässt sich verschieben, in den
// Vollbildmodus schalten und neben der Mediathek offen halten — ein Blatt
// über dem Hauptfenster konnte nichts davon.
//
// Ein Fenster bekommt seinen Inhalt als Wert, der sich speichern lässt
// (für das Wiederherstellen nach einem Neustart). Darum `PlayerAuftrag`:
// alles, was der Player zum Start braucht, und nichts, was nur im Speicher
// lebt.

/// Was ein Player zum Start braucht.
struct PlayerAuftrag: Codable, Hashable, Identifiable {
    var id = UUID()
    let item: Item
    /// Eine ganze Liste am Stück, ab `ab`.
    var liste: [Item]? = nil
    var ab: Int = 0
    /// „Von vorn": der erste Titel beginnt am Anfang, nicht am Stand.
    var vonVorn = false
    /// Die gewählte Fassung; nil heisst die erste.
    var datei: String? = nil
    /// Ein heruntergeladener Titel: diese Datei, ohne Server.
    var offlineDatei: String? = nil
}

extension Notification.Name {
    /// Ein Player hat sich geschlossen — die Titelseite frischt Stand und
    /// Haken auf. Am Mac nötig, weil das Fenster nicht über ein Binding
    /// der Titelseite hängt.
    static let lumenPlayerZu = Notification.Name("org.lumen.player.zu")
    /// ⌘F aus dem Menü des Mac.
    static let lumenSuchen = Notification.Name("org.lumen.suchen")
    /// ⌘1 … ⌘6 aus dem Menü „Gehe zu" des Mac; `object` ist das Ziel.
    static let lumenGeheZu = Notification.Name("org.lumen.gehezu")
}

/// Die Kennung des Player-Fensters am Mac.
let playerFensterKennung = "player"

extension View {
    /// Den Player zeigen, sobald `item` gesetzt ist: auf iPhone und iPad
    /// als Vollbild, am Mac in einem eigenen Fenster.
    func playerZeigen<T: Identifiable>(item: Binding<T?>,
                                       auftrag: @escaping (T) -> PlayerAuftrag) -> some View {
        modifier(PlayerZeigen(item: item, auftrag: auftrag))
    }
}

private struct PlayerZeigen<T: Identifiable>: ViewModifier {
    @Environment(Session.self) private var session
    @Binding var item: T?
    let auftrag: (T) -> PlayerAuftrag
    #if os(macOS)
    @Environment(\.openWindow) private var openWindow
    #endif

    func body(content: Content) -> some View {
        #if os(macOS)
        content.onChange(of: item?.id) { _, neu in
            guard neu != nil, let wert = item else { return }
            openWindow(id: playerFensterKennung, value: auftrag(wert))
            // Das Fenster lebt für sich; hier gibt es nichts mehr offen zu
            // halten. Beim Schliessen meldet es `lumenPlayerZu`.
            item = nil
        }
        #else
        content.fullScreenCover(item: $item) { wert in
            PlayerView(auftrag: auftrag(wert)).environment(session)
        }
        #endif
    }
}

// MARK: - Was das Menü des Mac über den Player wissen muss

#if os(macOS)
private struct WiedergabeSchluessel: FocusedValueKey {
    typealias Value = Playback
}

extension FocusedValues {
    /// Die Wiedergabe im Fenster, das gerade vorne ist — für „Abspielen/
    /// Pause" mit der Leertaste. Ohne Player-Fenster vorne ist es nil, der
    /// Menüpunkt aus, und die Leertaste tippt wieder Leerzeichen.
    var lumenWiedergabe: Playback? {
        get { self[WiedergabeSchluessel.self] }
        set { self[WiedergabeSchluessel.self] = newValue }
    }
}

/// Die Menübefehle des Mac.
struct LumenBefehle: Commands {
    @FocusedValue(\.lumenWiedergabe) private var wiedergabe

    // Die Menüleiste beschriftet AppKit — sie kennt die Sprache des
    // Profils nicht und spräche sonst die des Macs („Go", „Playback").
    // Darum hier fertige Texte statt Schlüssel.
    var body: some Commands {
        CommandGroup(after: .textEditing) {
            Button(String(localized: "Suchen", bundle: .lumen, locale: .lumen)) {
                NotificationCenter.default.post(name: .lumenSuchen, object: nil)
            }
            .keyboardShortcut("f")
        }
        // Wie die Seitenleiste, mit der Tastatur: ⌘1 Startseite, ⌘2 Filme …
        CommandMenu(String(localized: "Gehe zu", bundle: .lumen, locale: .lumen)) {
            geheZu(Text(verbatim: String(localized: "Startseite", bundle: .lumen, locale: .lumen)), "home", "1")
            geheZu(Text(verbatim: String(localized: "Filme", bundle: .lumen, locale: .lumen)), "movies", "2")
            geheZu(Text(verbatim: String(localized: "Serien", bundle: .lumen, locale: .lumen)), "series", "3")
            geheZu(Text(verbatim: String(localized: "Merkliste", bundle: .lumen, locale: .lumen)), "watchlist", "4")
            geheZu(Text(verbatim: String(localized: "Filmreihen", bundle: .lumen, locale: .lumen)), "collections", "5")
            geheZu(Text(verbatim: String(localized: "Suchen", bundle: .lumen, locale: .lumen)), "search", "6")
        }
        CommandMenu(String(localized: "Wiedergabe", bundle: .lumen, locale: .lumen)) {
            Button(String(localized: "Abspielen/Pause", bundle: .lumen, locale: .lumen)) { wiedergabe?.togglePlay() }
                .keyboardShortcut(.space, modifiers: [])
                .disabled(wiedergabe == nil)
            Button(String(localized: "10 Sekunden zurück", bundle: .lumen, locale: .lumen)) { wiedergabe?.skip(-10) }
                .keyboardShortcut(.leftArrow, modifiers: [])
                .disabled(wiedergabe == nil)
            Button(String(localized: "10 Sekunden vor", bundle: .lumen, locale: .lumen)) { wiedergabe?.skip(10) }
                .keyboardShortcut(.rightArrow, modifiers: [])
                .disabled(wiedergabe == nil)
        }
    }

    private func geheZu(_ titel: Text, _ ziel: String, _ taste: KeyEquivalent) -> some View {
        Button {
            NotificationCenter.default.post(name: .lumenGeheZu, object: ziel)
        } label: {
            titel
        }
        .keyboardShortcut(taste)
    }
}
#endif
