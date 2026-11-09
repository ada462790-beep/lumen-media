// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

import SwiftUI
import LumenKit

// Was auf dem Fernseher an vielen Stellen gleich aussehen soll: ein
// Fehler mit „Nochmal versuchen", und das Merken aus dem Menü einer Karte.

/// Ein Fehler, und darunter ein Knopf zum Wiederholen. Der Knopf ist das
/// Wichtige: ein Text allein bekommt auf dem Fernseher keinen Fokus, und
/// wer nur die Fernbedienung hat, käme sonst nicht weiter.
struct TVFehler: View {
    let text: String
    let nochmal: () async -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 24) {
            Label {
                Text(text)
            } icon: {
                Image(systemName: "exclamationmark.triangle")
            }
            .font(.title3)
            .foregroundStyle(Theme.bad)
            .frame(maxWidth: 1200, alignment: .leading)
            Button("Nochmal versuchen") { Task { await nochmal() } }
        }
        .padding(.vertical, 20)
    }
}

/// Merken aus dem Menü einer Karte. Die Karte hat ihren Titel als Wert,
/// nicht als Zustand — damit „Merken" danach „Aus der Merkliste" heisst,
/// merkt sich dieser Zähler die Änderungen, bis die Seite neu lädt.
/// Scheitert es, sagt es die Leiste oben (`TVShell`), statt es zu
/// verschlucken.
@MainActor
@Observable
final class TVMerker {
    static let shared = TVMerker()

    /// Was seit dem letzten Laden geändert wurde: Titel → gemerkt.
    private(set) var geaendert: [String: Bool] = [:]
    /// Zählt jede Änderung — die Merkliste lädt daran neu.
    private(set) var stand = 0
    var meldung: String?

    private init() {}

    func istGemerkt(_ item: Item) -> Bool { geaendert[item.id] ?? item.isOnList }

    /// Auf der Titelseite geändert: dort lädt der Titel neu, und was hier
    /// stand, wäre ab jetzt falsch.
    func vergessen(_ itemId: String) {
        if geaendert.removeValue(forKey: itemId) != nil { stand &+= 1 }
    }

    func umschalten(_ item: Item, client: APIClient) async {
        let vorher = istGemerkt(item)
        do {
            if vorher { try await client.removeFromWatchlist(item.id) }
            else { try await client.addToWatchlist(item.id) }
            geaendert[item.id] = !vorher
            stand &+= 1
        } catch {
            meldung = Session.text(error)
        }
    }
}
