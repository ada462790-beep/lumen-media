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

    var body: some Commands {
        CommandGroup(after: .textEditing) {
            Button("Suchen") {
                NotificationCenter.default.post(name: .lumenSuchen, object: nil)
            }
            .keyboardShortcut("f")
        }
        CommandMenu("Wiedergabe") {
            Button("Abspielen/Pause") { wiedergabe?.togglePlay() }
                .keyboardShortcut(.space, modifiers: [])
                .disabled(wiedergabe == nil)
            Button("10 Sekunden zurück") { wiedergabe?.skip(-10) }
                .keyboardShortcut(.leftArrow, modifiers: [])
                .disabled(wiedergabe == nil)
            Button("10 Sekunden vor") { wiedergabe?.skip(10) }
                .keyboardShortcut(.rightArrow, modifiers: [])
                .disabled(wiedergabe == nil)
        }
    }
}
#endif
