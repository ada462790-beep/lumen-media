// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

import Foundation
import Observation

/// „Zur Liste": in welche Listen ein Titel gehört.
///
/// Die Rechnung hinter der Auswahl auf der Titelseite — auf dem iPhone,
/// dem Mac und dem Fernseher dieselbe (GitHub-Issue #1). Gezeigt werden
/// nur Listen, in die man schreiben darf: die eigenen und geteilte, bei
/// denen alle mitschreiben. Ein Tipp legt hinein oder nimmt heraus; was
/// gerade gilt, steht an jeder Liste (`hasItem`).
///
/// Die Ansichten zeigen nur an und rufen `toggle` und `createAndAdd`.
@MainActor
@Observable
public final class Listenwahl {

    public let itemId: String
    public private(set) var listen: [Liste] = []
    public private(set) var loading = true
    /// Warum das Laden scheiterte.
    public private(set) var error: String?
    /// Warum eine Änderung nicht durchging — etwa weil die Liste inzwischen
    /// nicht mehr geteilt (403) oder gelöscht ist (404). Die Ansicht zeigt
    /// es an und setzt es auf nil zurück.
    public var problem: String?
    /// Die Liste, die gerade geändert wird; "" beim Anlegen. Solange ist
    /// alles andere gesperrt, damit kein zweiter Tipp den ersten überholt.
    public private(set) var busy: String?

    private let client: APIClient

    public init(client: APIClient, itemId: String) {
        self.client = client
        self.itemId = itemId
    }

    public var isBusy: Bool { busy != nil }

    public func load() async {
        error = nil
        do { listen = try await client.playlists(itemId: itemId).filter(\.darfAendern) }
        catch { self.error = Session.text(error) }
        loading = false
    }

    /// Hineinlegen, wenn der Titel nicht drin ist — sonst herausnehmen.
    public func toggle(_ liste: Liste) async {
        busy = liste.id
        do {
            if liste.hasItem {
                try await client.removeFromPlaylist(liste.id, itemId: itemId)
            } else {
                try await client.addToPlaylist(liste.id, itemId: itemId)
            }
        } catch {
            problem = Session.text(error)
        }
        await load()
        busy = nil
    }

    /// Eine neue Liste anlegen und den Titel gleich hineinlegen. Ein leerer
    /// Name tut nichts.
    public func createAndAdd(name: String) async {
        let name = name.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !name.isEmpty else { return }
        busy = ""
        do {
            let neu = try await client.createPlaylist(name: name)
            try await client.addToPlaylist(neu.id, itemId: itemId)
        } catch {
            problem = Session.text(error)
        }
        await load()
        busy = nil
    }
}
