// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

#if DEBUG
import Foundation
import LumenKit

/// Nur in Debug-Bauten und nur für die UI-Tests des Simulator-Ablaufs
/// (.github/workflows/simulator.yml): Stehen Server, Name und Passwort in
/// der Umgebung, meldet sich die App beim Start selbst an — am Apple TV
/// liesse sich die Adresse sonst nur mühsam mit der Fernbedienung tippen.
///
///     LUMEN_TEST_SERVER=http://127.0.0.1:8097
///     LUMEN_TEST_NAME=Admin
///     LUMEN_TEST_PASSWORT=…
///
/// Ohne diese Umgebung (also immer, ausser im Test) tut es nichts. In
/// einem Release-Bau gibt es den Code gar nicht.
enum TestAnmeldung {
    @MainActor
    static func versuchen(_ session: Session) async -> Bool {
        let umgebung = ProcessInfo.processInfo.environment
        guard let adresse = umgebung["LUMEN_TEST_SERVER"], !adresse.isEmpty else { return false }
        let name = umgebung["LUMEN_TEST_NAME"] ?? "Admin"  // sprache: fest
        let geheim = umgebung["LUMEN_TEST_PASSWORT"] ?? ""
        await session.useServer(address: adresse)
        switch session.stage {
        case .choosingProfile(let karten):
            if let karte = karten.first(where: { $0.name == name }) {
                await session.unlock(karte, secret: geheim)
            }
        case .unlocking(let karte):
            await session.unlock(karte, secret: geheim)
        case .unterwegs:
            await session.unlockUnterwegs(name: name, secret: geheim)
        default:
            break
        }
        return true
    }
}
#endif
