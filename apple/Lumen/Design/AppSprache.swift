// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

import SwiftUI
import LumenKit

/// Die Wahl der Sprache unter Ich › Einstellungen — auf iPhone, iPad, Mac
/// und Apple TV dieselbe: „wie der Server" oder eine der fünf. Die Wahl
/// gehört zum Profil, nicht zum Gerät; sie gilt darum auch im Browser.
@MainActor
enum AppSprache {

    /// Der Eintrag für „wie der Server", mit der Sprache, die das heisst:
    /// „Wie der Server · Deutsch (Schweiz)". Kennt der Server noch keine
    /// Sprache, gilt die des Geräts.
    static func vorgabeName(_ session: Session) -> String {
        if let server = session.serverSprache {
            let name = Sprache.name(server, angebot: session.sprachen)
            return String(localized: "Wie der Server · \(name)", bundle: .lumen, locale: .lumen)
        }
        let name = Sprache.name(Sprache.ausGeraet(), angebot: session.sprachen)
        return String(localized: "Wie das Gerät · \(name)", bundle: .lumen, locale: .lumen)
    }

    /// Alle Einträge der Auswahl; der Wert "" heisst „wie der Server".
    /// Die Sprachen stehen in ihrer eigenen Sprache da — so findet man
    /// seine auch, wenn man die eingestellte nicht lesen kann.
    static func optionen(_ session: Session) -> [Sprachwahl.Wahl] {
        [Sprachwahl.Wahl(wert: "", name: vorgabeName(session))]
            + session.sprachen.map { Sprachwahl.Wahl(wert: $0.id, name: $0.name) }
    }

    /// Was gerade gewählt ist, als Wert der Auswahl.
    static func gewaehlt(_ session: Session) -> String {
        session.eigeneSprache ?? ""
    }

    /// Der Name der gewählten Einstellung, für die Zeile in der Liste.
    static func anzeige(_ session: Session) -> String {
        guard let eigene = session.eigeneSprache else { return vorgabeName(session) }
        return Sprache.name(eigene, angebot: session.sprachen)
    }
}
