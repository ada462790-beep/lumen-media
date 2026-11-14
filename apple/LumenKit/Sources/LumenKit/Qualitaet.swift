// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

import Foundation

/// „Qualität" im Player — dieselben Stufen wie im Browser (app.js,
/// QUALITAETS_STUFEN, qualitaetParameter) und auf Android (Qualitaet.kt):
/// Original, 4K · 20 Mbit/s, 1080p · 8, 720p · 4, 480p · 2, 360p · 1 Mbit/s
/// und Auto.
///
/// Angeboten wird, was nicht über der Quelle liegt; „Original" und „Auto"
/// gibt es immer. Die Wahl gilt für dieses Gerät (`UserDefaults`), nicht
/// fürs Konto: Am iPhone unterwegs will man etwas anderes als am Fernseher.
public enum Qualitaet {

    public static let original = "original" // sprache: fest
    public static let auto = "auto" // sprache: fest

    /// Eine Stufe fürs Umwandeln: Obergrenzen für Höhe und Bitrate.
    public struct Stufe: Sendable, Hashable, Identifiable {
        public let id: String
        public let hoehe: Int
        public let bitrate: Int
    }

    public static let stufen: [Stufe] = [
        Stufe(id: "2160", hoehe: 2160, bitrate: 20_000_000),
        Stufe(id: "1080", hoehe: 1080, bitrate: 8_000_000),
        Stufe(id: "720", hoehe: 720, bitrate: 4_000_000),
        Stufe(id: "480", hoehe: 480, bitrate: 2_000_000),
        Stufe(id: "360", hoehe: 360, bitrate: 1_000_000),
    ]

    /// „Original" hebt die Grenze des Bildschirms auf — wie im Browser.
    public static let originalHoehe = 4320

    /// Die Stufen bis zur Quelle. Unbekannte Höhe: so tun, als wäre es 1080p.
    public static func stufen(fuer quellHoehe: Int?) -> [Stufe] {
        let h = (quellHoehe ?? 0) > 0 ? quellHoehe! : 1080
        return stufen.filter { $0.hoehe <= h }
    }

    /// Was für diese Quelle gilt: die gemerkte Stufe, wenn es sie hier
    /// gibt — sonst „Auto" (480p gemerkt, die Datei hat 400p: dann eben
    /// die Datei).
    public static func wirksam(_ gemerkt: String?, quellHoehe: Int?) -> String {
        if gemerkt == original { return original }
        if let gemerkt, stufen(fuer: quellHoehe).contains(where: { $0.id == gemerkt }) { return gemerkt }
        return auto
    }

    /// Das Geräteprofil mit den Grenzen der Stufe — für Plan und Strom
    /// (`maxHeight`, `maxBitrate`). „Auto" lässt das Profil, wie es ist.
    public static func profil(_ gemerkt: String?, quellHoehe: Int?,
                              basis: DeviceProfile = .current) -> DeviceProfile {
        let id = wirksam(gemerkt, quellHoehe: quellHoehe)
        if id == original { return basis.mitGrenzen(maxHeight: originalHoehe, maxBitrate: 0) }
        guard let s = stufen.first(where: { $0.id == id }) else { return basis }
        return basis.mitGrenzen(maxHeight: s.hoehe, maxBitrate: s.bitrate)
    }

    /// „Original", „1080p · 8 Mbit/s", „4K · 20 Mbit/s", „Auto".
    public static func name(_ id: String) -> String {
        if id == original { return String(localized: "Original", bundle: .lumenKit, locale: .lumen) }
        guard let s = stufen.first(where: { $0.id == id }) else {
            return String(localized: "Auto", bundle: .lumenKit, locale: .lumen)
        }
        let hoehe = s.hoehe == 2160 ? "4K" : "\(s.hoehe)p"
        let mbit: Int = s.bitrate / 1_000_000
        return hoehe + " · " + String(localized: "\(mbit) Mbit/s", bundle: .lumenKit, locale: .lumen)
    }

    // MARK: - Für dieses Gerät merken

    private static let schluessel = "lumen.qualitaet"

    /// Die gemerkte Stufe dieses Geräts; ohne Wahl „Auto".
    public static var gemerkt: String {
        UserDefaults.standard.string(forKey: schluessel) ?? auto
    }

    public static func merken(_ id: String) {
        UserDefaults.standard.set(id, forKey: schluessel)
    }
}
