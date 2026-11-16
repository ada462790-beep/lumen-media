// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

import Foundation

/// Ein Link auf die Seite eines Titels bei IMDb oder TMDb (`links` in
/// GET /items/{id}). Der Server schickt nur, was er sicher kennt.
public struct TitelLink: Codable, Hashable, Sendable {
    public let name: String
    public let url: String

    public init(name: String, url: String) {
        self.name = name
        self.url = url
    }

    /// Nur https — ein Link, der anders aussieht, wird nicht geöffnet.
    public var adresse: URL? {
        guard url.hasPrefix("https://") else { return nil }
        return URL(string: url)
    }
}

/// Eine Wertung unter dem Titel: IMDb, Rotten Tomatoes oder TMDb.
public struct Wertung: Hashable, Sendable, Identifiable {
    public enum Quelle: String, Sendable {
        /// IMDb mit Schriftzug, Stern, Note und Stimmen.
        case imdb
        /// Eine Note aus einer NFO ohne Quelle: nur „★ 7.4", ohne Schriftzug.
        case neutral
        /// Rotten Tomatoes: Prozent und frisch oder faul.
        case rottenTomatoes
        /// TMDb: Nutzerwertung in Prozent.
        case tmdb
    }

    public let quelle: Quelle
    /// Was gross dasteht: „7.8", „93 %", „75 %".
    public let wert: String
    /// Was klein daneben steht: die Stimmen bei IMDb, „Rotten Tomatoes".
    public let zusatz: String?
    /// Nur bei Rotten Tomatoes: ab 60 % frisch (Tomate), darunter faul.
    public let frisch: Bool?
    /// Für VoiceOver: „IMDb: 7.8 von 10, 12’345 Stimmen".
    public let barrierefrei: String
    /// Die Seite des Titels bei der Quelle, wenn der Server sie kennt.
    public let link: URL?

    public var id: String { quelle.rawValue }
}

/// Die Wertungen unter dem Titel — dieselben Regeln wie im Browser
/// (app.js: wertungsReihe, note): IMDb mit genau einer Nachkommastelle
/// und den Stimmen, Rotten Tomatoes ab 60 % frisch, darunter faul, TMDb in
/// Prozent. Eine Note aus einer NFO ohne Quelle (`bewertungQuelle` „nfo")
/// steht neutral da, ohne IMDb-Schriftzug und ohne Link.
public enum Wertungen {

    /// Ab diesem Anteil guter Kritiken gilt ein Titel bei Rotten Tomatoes
    /// als frisch — wie im Browser und bei Rotten Tomatoes selbst.
    public static let frischAb = 60

    public static func frisch(_ prozent: Int) -> Bool { prozent >= frischAb }

    /// Eine Note mit genau einer Nachkommastelle in der Schreibweise des
    /// Profils: „6.8" in der Schweiz, „6,8" in Deutschland — nie „6.799",
    /// nie „7".
    public static func note(_ wert: Double) -> String {
        wert.formatted(.number.precision(.fractionLength(1)).locale(.lumen))
    }

    /// „★ 6.8" — kurz, für Karten und Zeilen. nil ohne Note.
    public static func kurz(_ item: Item) -> String? {
        guard let r = item.imdbRating, r > 0 else { return nil }
        return "★ " + note(r)
    }

    public static func liste(_ item: Item, links: [TitelLink]? = nil, neutral: Bool = false) -> [Wertung] {
        func link(_ name: String) -> URL? { (links ?? []).first { $0.name == name }?.adresse }
        var out: [Wertung] = []
        if let r = item.imdbRating, r > 0 {
            let n = note(r)
            if neutral {
                out.append(Wertung(quelle: .neutral, wert: n, zusatz: nil, frisch: nil,
                                   barrierefrei: String(localized: "Wertung: \(n) von 10", bundle: .lumenKit, locale: .lumen),
                                   link: nil))
            } else {
                let stimmen = (item.imdbVotes ?? 0) > 0 ? Sprache.zahl(item.imdbVotes ?? 0) : nil
                let text = stimmen.map { String(localized: "IMDb: \(n) von 10, \($0) Stimmen", bundle: .lumenKit, locale: .lumen) }
                    ?? String(localized: "IMDb: \(n) von 10", bundle: .lumenKit, locale: .lumen)
                out.append(Wertung(quelle: .imdb, wert: n, zusatz: stimmen, frisch: nil,
                                   barrierefrei: text, link: link("IMDb")))
            }
        }
        if let p = item.rottenTomatoes {
            let gut = frisch(p)
            let text = gut ? String(localized: "Rotten Tomatoes: \(p) % (frisch)", bundle: .lumenKit, locale: .lumen)
                : String(localized: "Rotten Tomatoes: \(p) % (faul)", bundle: .lumenKit, locale: .lumen)
            out.append(Wertung(quelle: .rottenTomatoes, wert: "\(p) %", zusatz: "Rotten Tomatoes", frisch: gut,
                               barrierefrei: text, link: nil))
        }
        if let p = item.tmdbRating, p > 0 {
            out.append(Wertung(quelle: .tmdb, wert: "\(p) %", zusatz: nil, frisch: nil,
                               barrierefrei: String(localized: "TMDb: \(p) % der Nutzer", bundle: .lumenKit, locale: .lumen),
                               link: link("TheMovieDb")))
        }
        return out
    }
}

// MARK: - Heimnetz

/// So sieht der Server das Gerät, von dem die Anfrage kommt
/// (GET /admin/netz) — wie im Browser unter Verwaltung › Dieses Gerät.
public struct NetzStand: Codable, Sendable {
    public struct Geraet: Codable, Sendable {
        public let adresse: String
        public let zuHause: Bool
        public let grund: String
        public let kopf: String?
    }
    public struct Gateway: Codable, Sendable {
        public let adresse: String
        public let docker: Bool?
    }
    public let geraet: Geraet
    public let hostNetz: Bool?
    public let bruecke: Bool?
    public let gateway: Gateway?

    /// Ob Docker die echten Adressen verdeckt: dann gilt jedes Gerät als
    /// unterwegs (keine Profilauswahl, Qualitätsgrenze).
    public var dockerVerdeckt: Bool {
        gateway?.docker == true || (geraet.grund == "gateway" && bruecke == true)
    }

    /// Warum das Gerät als zu Hause oder unterwegs gilt — dieselben Sätze
    /// wie im Browser (netzGrund).
    public var grundText: String {
        switch geraet.grund {
        case "festgelegt": return String(localized: "Die Adresse steht in LUMEN_HEIMNETZ.", bundle: .lumenKit, locale: .lumen)
        case "nichtFestgelegt": return String(localized: "Die Adresse steht nicht in LUMEN_HEIMNETZ.", bundle: .lumenKit, locale: .lumen)
        case "lumenSelbst": return String(localized: "Die Anfrage kommt aus dem Container von Lumen selbst.", bundle: .lumenKit, locale: .lumen)
        case "rechnerSelbst": return String(localized: "Die Anfrage kommt vom Rechner, auf dem Lumen läuft – etwa über einen Reverse-Proxy. Das zählt nur als zu Hause, wenn die Adresse einzeln in LUMEN_HEIMNETZ steht.", bundle: .lumenKit, locale: .lumen)
        case "containerNetz": return String(localized: "Die Anfrage kommt aus einem anderen Container auf dem NAS, etwa einem Reverse-Proxy.", bundle: .lumenKit, locale: .lumen)
        case "gateway": return String(localized: "Die Adresse ist das Gateway eines eigenen Netzes. Dahinter kann jedes Gerät stecken, auch eines aus dem Internet.", bundle: .lumenKit, locale: .lumen)
        case "oeffentlich": return String(localized: "Keine Adresse aus dem Heimnetz.", bundle: .lumenKit, locale: .lumen)
        case "privat": return String(localized: "Private Adresse im Heimnetz.", bundle: .lumenKit, locale: .lumen)
        case "vermittler": return String(localized: "Über den Vermittler (Fernzugriff).", bundle: .lumenKit, locale: .lumen)
        case "weitergereicht":
            let kopf = geraet.kopf ?? ""
            return String(localized: "Ein Reverse-Proxy hat die Anfrage weitergereicht (\(kopf)).", bundle: .lumenKit, locale: .lumen)
        default: return String(localized: "Die Adresse ist nicht lesbar.", bundle: .lumenKit, locale: .lumen)
        }
    }
}
