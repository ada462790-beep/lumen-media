// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

import Foundation

/// Eine Sprache, wie der Server sie anbietet (GET /hello → `sprachen`).
public struct SprachAngebot: Codable, Hashable, Sendable, Identifiable {
    public let id: String
    public let name: String

    public init(id: String, name: String) {
        self.id = id
        self.name = name
    }
}

/// Welche Sprache die Apps sprechen — und woher sie die Texte dafür holen.
///
/// Die Apps folgen der Sprache des Profils auf dem Server, nicht bloss der
/// des Geräts: wer am Fernseher der Familie sein Profil wählt, liest seine
/// Sprache. Die Reihenfolge:
///
///   1. die Sprache des Profils (GET /me → `sprache`),
///   2. die Vorgabe des Servers (GET /hello → `sprache`), vor der Anmeldung,
///   3. die Sprache des Geräts, solange der Server keine nennt,
///   4. Deutsch (Schweiz), wenn das Gerät keine der fünf spricht.
///
/// Die Ansichten bekommen die Wahl als `.environment(\.locale, …)`; das
/// genügt für `Text("…")`. Alles, was ausserhalb einer Ansicht zu Text wird
/// — Fehlermeldungen, „3 neue Folgen" aus einem Modell —, holt ihn über
/// `String(localized: "…", bundle: .lumen, locale: .lumen)` aus dem Ordner
/// der gewählten Sprache. `Bundle.main` allein nähme die Sprache des Geräts.
public enum Sprache {

    /// Die Sprache, in der der Code geschrieben ist — Schweizer Hochdeutsch
    /// mit „ss". Auch die Schlüssel der Kataloge stehen in ihr.
    public static let quelle = "de-CH"

    /// Die fünf Sprachen, jede in ihrer eigenen Sprache benannt — so findet
    /// man seine auch, wenn man die eingestellte nicht lesen kann.
    public static let alle: [SprachAngebot] = [
        SprachAngebot(id: "de-CH", name: "Deutsch (Schweiz)"), // sprache: fest
        SprachAngebot(id: "de-DE", name: "Deutsch (Deutschland)"), // sprache: fest
        SprachAngebot(id: "de-AT", name: "Deutsch (Österreich)"), // sprache: fest
        SprachAngebot(id: "en-US", name: "English (United States)"), // sprache: fest
        SprachAngebot(id: "en-GB", name: "English (United Kingdom)"), // sprache: fest
    ]

    /// Wo die Wahl zwischen zwei Starts liegt: so erscheint schon der erste
    /// Bildschirm in der Sprache, die zuletzt galt — noch bevor der Server
    /// geantwortet hat.
    public static let schluessel = "lumen.sprache"

    // MARK: Wählen

    /// Bringt eine Angabe des Servers oder des Geräts auf eine der fünf:
    /// „de_CH", „de-ch" → „de-CH"; „de" → „de-DE"; „en" → „en-US".
    /// nil, wenn sie leer ist oder keine davon meint.
    public static func bekannt(_ angabe: String?) -> String? {
        guard let angabe else { return nil }
        let t = angabe.trimmingCharacters(in: .whitespaces).replacingOccurrences(of: "_", with: "-")
        guard !t.isEmpty else { return nil }
        if let genau = alle.first(where: { $0.id.caseInsensitiveCompare(t) == .orderedSame }) {
            return genau.id
        }
        switch t.lowercased() {
        case "de": return "de-DE"
        case "en": return "en-US"
        default: return nil
        }
    }

    /// Die Sprache des Geräts als eine der fünf. Geht die bevorzugten
    /// Sprachen der Reihe nach durch; spricht das Gerät weder Deutsch noch
    /// Englisch, bleibt es bei Deutsch (Schweiz).
    public static func ausGeraet(_ bevorzugt: [String] = Locale.preferredLanguages) -> String {
        for tag in bevorzugt {
            let teile = tag.replacingOccurrences(of: "_", with: "-").split(separator: "-").map(String.init)
            guard let sprache = teile.first?.lowercased() else { continue }
            // Die Region ist der zweibuchstabige Teil in Grossbuchstaben;
            // eine Schrift wie „Latn" steht davor und zählt nicht.
            let region = teile.dropFirst().first { $0.count == 2 }?.uppercased()
            switch sprache {
            case "de", "gsw":
                switch region {
                case "CH", "LI": return "de-CH"
                case "AT": return "de-AT"
                default: return sprache == "gsw" ? "de-CH" : "de-DE"
                }
            case "en":
                // Wo man britisch schreibt, „colour" und „programme".
                let britisch: Set<String> = ["GB", "IE", "AU", "NZ", "IN", "ZA", "SG", "HK",
                                             "MT", "CY", "GI", "IM", "JE", "GG", "KE", "NG"]
                return region.map { britisch.contains($0) } == true ? "en-GB" : "en-US"
            default:
                continue
            }
        }
        return quelle
    }

    /// Welche Sprache gilt: Profil, sonst Server, sonst Gerät.
    public static func wirksam(profil: String?, server: String?,
                               geraet: [String] = Locale.preferredLanguages) -> String {
        bekannt(profil) ?? bekannt(server) ?? ausGeraet(geraet)
    }

    /// Der Name einer Sprache, wie die Auswahl ihn zeigt.
    public static func name(_ id: String, angebot: [SprachAngebot] = alle) -> String {
        angebot.first { $0.id == id }?.name ?? alle.first { $0.id == id }?.name ?? id
    }

    // MARK: Zustand

    /// Die gewählte Sprache und die schon gefundenen Ordner. Hinter einem
    /// Schloss, weil Texte auch abseits des Hauptfadens entstehen — eine
    /// Fehlermeldung im APIClient etwa.
    private final class Zustand: @unchecked Sendable {
        let schloss = NSLock()
        var id = Sprache.quelle
        var ordner: [String: Bundle] = [:]

        func mit<T>(_ arbeit: (Zustand) -> T) -> T {
            schloss.lock()
            defer { schloss.unlock() }
            return arbeit(self)
        }
    }

    private static let zustand = Zustand()

    /// Die Sprache, in der die App gerade spricht („de-CH" … „en-GB").
    /// Bis zur ersten Wahl die Quellsprache — so bleiben auch die Tests
    /// unabhängig davon, wie der Rechner eingestellt ist.
    public static var aktuell: String { zustand.mit { $0.id } }

    /// Für Zahlen, Daten und Dauern in der gewählten Sprache.
    public static var locale: Locale { Locale(identifier: aktuell) }

    /// Die zuletzt gewählte Sprache, falls es eine gab.
    public static var gemerkt: String? {
        bekannt(UserDefaults.standard.string(forKey: schluessel))
    }

    /// Die Sprache für den Start: was zuletzt galt, sonst das Gerät.
    /// Die App setzt sie, bevor sie ihre `Session` anlegt:
    ///
    ///     Sprache.setzen(Sprache.beimStart, merken: false)
    ///
    /// Die Tests tun das nicht — sie sprechen darum immer die Quellsprache.
    public static var beimStart: String { gemerkt ?? ausGeraet() }

    /// Eine Sprache wählen. Gibt zurück, ob sich etwas geändert hat.
    ///
    /// `merken`: auch für den nächsten Start festhalten. Dann trägt die App
    /// die Sprache zusätzlich als ihre bevorzugte beim System ein
    /// (`AppleLanguages`) — beim nächsten Öffnen sprechen damit auch die
    /// Teile, die das System selbst beschriftet, die Sprache des Profils.
    @discardableResult
    public static func setzen(_ id: String, merken: Bool = true) -> Bool {
        let neu = bekannt(id) ?? quelle
        let geaendert = zustand.mit { z -> Bool in
            guard z.id != neu else { return false }
            z.id = neu
            return true
        }
        if merken {
            let d = UserDefaults.standard
            if d.string(forKey: schluessel) != neu {
                d.set(neu, forKey: schluessel)
                d.set([neu], forKey: "AppleLanguages")
            }
        }
        return geaendert
    }

    // MARK: Texte nachschlagen

    /// Die Ordnernamen, unter denen Xcode die Übersetzungen einer Sprache
    /// ablegt, in der Reihenfolge, in der sie zählen. Deutschland und die
    /// USA heissen dort schlicht „de" und „en" — so, wie Apple es selbst hält.
    static func ordnerNamen(_ id: String) -> [String] {
        switch id {
        case "de-CH": ["de-CH"]
        case "de-DE": ["de", "de-DE"]
        case "de-AT": ["de-AT", "de"]
        case "en-US": ["en", "en-US"]
        case "en-GB": ["en-GB", "en"]
        default: [id]
        }
    }

    /// Das Teilbündel mit den Übersetzungen der gewählten Sprache.
    ///
    /// Fehlt der Ordner — etwa für die Quellsprache, wenn Xcode für sie
    /// keinen anlegt —, kommt ein Bündel ohne Übersetzungen zurück: dann
    /// steht der Schlüssel selbst da, und der ist Schweizer Hochdeutsch.
    /// Nie das Hauptbündel, denn das spräche die Sprache des Geräts.
    public static func buendel(fuer basis: Bundle) -> Bundle {
        let id = aktuell
        let merk = basis.bundleURL.path + "|" + id
        if let fertig = zustand.mit({ $0.ordner[merk] }) { return fertig }

        let vorhanden = basis.localizations
        var gefunden: Bundle?
        for name in ordnerNamen(id) {
            let echt = vorhanden.first {
                $0.replacingOccurrences(of: "_", with: "-").caseInsensitiveCompare(name) == .orderedSame
            } ?? name
            if let pfad = basis.path(forResource: echt, ofType: "lproj"),
               let b = Bundle(path: pfad) {
                gefunden = b
                break
            }
        }
        let ergebnis = gefunden ?? ohneUebersetzung
        zustand.mit { $0.ordner[merk] = ergebnis }
        return ergebnis
    }

    /// Ein Bündel, in dem kein Text steht: das Nachschlagen liefert den
    /// Schlüssel, also die Quellsprache.
    private static let ohneUebersetzung: Bundle = {
        let ordner = FileManager.default.temporaryDirectory
            .appendingPathComponent("lumen-ohne-uebersetzung", isDirectory: true)
        try? FileManager.default.createDirectory(at: ordner, withIntermediateDirectories: true)
        return Bundle(url: ordner) ?? Bundle(for: Merkmal.self)
    }()

    private final class Merkmal {}

    // MARK: Zahlen

    /// Eine Zahl mit höchstens `stellen` Nachkommastellen, so geschrieben,
    /// wie man es in der gewählten Sprache tut: „4.2" in der Schweiz und
    /// auf Englisch, „4,2" in Deutschland und Österreich.
    public static func zahl(_ wert: Double, stellen: Int = 1) -> String {
        wert.formatted(.number.precision(.fractionLength(0...stellen)).locale(locale))
    }

    /// Ein Tag als Zahlen, wie man ihn in der gewählten Sprache schreibt:
    /// „23.12.1968" auf Deutsch, „12/23/1968" in den USA, „23/12/1968" in
    /// Grossbritannien. `zweistellig`: „02.11.2026" statt „2.11.2026".
    /// Gerechnet in UTC — es ist ein Kalendertag, keine Uhrzeit.
    public static func datum(jahr: Int, monat: Int, tag: Int, zweistellig: Bool = false) -> String {
        var kalender = Calendar(identifier: .gregorian)
        let utc = TimeZone(identifier: "UTC") ?? .current
        kalender.timeZone = utc
        guard let tagDatum = kalender.date(from: DateComponents(year: jahr, month: monat, day: tag)) else {
            return "\(tag).\(monat).\(jahr)"
        }
        let stil = Date.FormatStyle(locale: locale, calendar: kalender, timeZone: utc)
        return zweistellig
            ? tagDatum.formatted(stil.day(.twoDigits).month(.twoDigits).year())
            : tagDatum.formatted(stil.day(.defaultDigits).month(.defaultDigits).year())
    }

    /// Eine ganze Zahl mit Tausendertrennung („12’345", „12.345", „12,345").
    public static func zahl(_ wert: Int) -> String {
        wert.formatted(.number.locale(locale))
    }
}

extension Bundle {
    /// Die Texte der App (Hauptbündel) in der Sprache des Profils.
    public static var lumen: Bundle { Sprache.buendel(fuer: .main) }

    /// Die Texte von LumenKit selbst in der Sprache des Profils.
    static var lumenKit: Bundle { Sprache.buendel(fuer: .module) }
}

extension Locale {
    /// Die gewählte Sprache, für Zahlen, Daten und Dauern.
    public static var lumen: Locale { Sprache.locale }
}
