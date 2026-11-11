// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

import SwiftUI
import LumenKit

/// Wie viel Bildschirmzeit heute noch bleibt — ein Abschnitt für „Ich" auf
/// iPhone, iPad, Mac und Apple TV. Erscheint nur, wenn für dieses Profil
/// eine Regel gilt (Kinderprofile); sonst bleibt er ganz weg, ebenso bei
/// einem älteren Server, der die Route nicht kennt. Die Grenze selbst
/// zieht der Server — hier steht nur, wie es steht. Wie im Browser
/// (bildschirmzeit.js: bzIchZeile).
///
/// Geladen wird in der Ansicht darüber (`laden`), nicht hier: ein Abschnitt,
/// der noch nichts zeigt, ist leer — und an Leerem läuft kein `.task`.
struct BildschirmzeitAbschnitt: View {
    let stand: Bildschirmzeit?

    var body: some View {
        if let stand, stand.aktiv {
            Section {
                VStack(alignment: .leading, spacing: 4) {
                    Label(stand.hauptzeile, systemImage: "hourglass")
                    if !stand.nebenzeile.isEmpty {
                        Text(stand.nebenzeile)
                            .font(.caption).foregroundStyle(Theme.label2)
                    }
                }
                .padding(.vertical, 2)
            } header: {
                Text("Bildschirmzeit")
            }
        }
    }

    /// Der Stand für dieses Profil — nil bei Profilen ohne Regel (die
    /// fragen gar nicht erst) und wenn der Server nicht antwortet.
    @MainActor
    static func laden(_ session: Session) async -> Bildschirmzeit? {
        guard session.me?.account.isKid == true else { return nil }
        return try? await session.client.bildschirmzeit()
    }
}
