// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

import SwiftUI
import LumenKit

/// Wie ein Gerät in der Liste der eigenen Geräte heisst. Der Server
/// schickt die Art als Kennung („phone", „tv" …), nicht als Wort.
enum GeraeteText {
    static func art(_ kind: String) -> String {
        switch kind {
        case "phone": String(localized: "Telefon", bundle: .lumen, locale: .lumen)
        case "tablet": String(localized: "Tablet", bundle: .lumen, locale: .lumen)
        case "desktop": String(localized: "Computer", bundle: .lumen, locale: .lumen)
        case "tv": String(localized: "Fernseher", bundle: .lumen, locale: .lumen)
        case "browser", "web": String(localized: "Browser", bundle: .lumen, locale: .lumen)
        default: kind
        }
    }

    static func symbol(_ kind: String) -> String {
        switch kind {
        case "phone": "iphone"
        case "tablet": "ipad"
        case "desktop": "desktopcomputer"
        case "tv": "tv"
        default: "globe"
        }
    }
}
