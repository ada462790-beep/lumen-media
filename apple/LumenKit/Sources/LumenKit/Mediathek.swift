// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

import Foundation
import Observation

// Was iPhone, iPad, Mac und Fernseher auf den Seiten der Mediathek gleich
// rechnen: das Raster mit Genre-Knöpfen, das seitenweise nachlädt, die
// Beschriftung der Knöpfe auf der Titelseite, die Sprachen in den
// Einstellungen. Der Browser macht es genauso (app.js: viewGrid,
// mehrLadenEinrichten, viewDetail, viewCollection) — wer zwischen den
// Geräten wechselt, soll keinen Unterschied merken.

// MARK: - Raster

/// Wie das Raster sortiert ist — dieselben vier Wege wie im
/// Browser (app.js: SORTIERUNGEN). Der Rohwert ist, was der Server als
/// `sort` versteht.
public enum Sortierung: String, CaseIterable, Identifiable, Sendable {
    case titel = "title"
    case hinzugefuegt = "added"
    case neueste = "year"
    case aelteste = "year-asc"

    public var id: String { rawValue }

    /// „A–Z", „Neu hinzugefügt", „Neueste Filme", „Älteste Serien" — bei
    /// Serien hiesse „Neueste Filme" etwas anderes, als gemeint ist.
    public func name(kind: String) -> String {
        let serien = kind == "series"
        switch self {
        case .titel: return String(localized: "A–Z", bundle: .lumenKit, locale: .lumen)
        case .hinzugefuegt: return String(localized: "Neu hinzugefügt", bundle: .lumenKit, locale: .lumen)
        case .neueste: return serien ? String(localized: "Neueste Serien", bundle: .lumenKit, locale: .lumen) : String(localized: "Neueste Filme", bundle: .lumenKit, locale: .lumen)
        case .aelteste: return serien ? String(localized: "Älteste Serien", bundle: .lumenKit, locale: .lumen) : String(localized: "Älteste Filme", bundle: .lumenKit, locale: .lumen)
        }
    }

    /// Das Symbol im Menü.
    public var symbol: String {
        switch self {
        case .titel: "textformat"
        case .hinzugefuegt: "clock"
        case .neueste: "arrow.down"
        case .aelteste: "arrow.up"
        }
    }
}

/// Die Leiste „# A B C …" über dem Raster.
public enum Buchstaben {
    /// Die Raute steht für alles, was nicht mit einem Buchstaben anfängt — Zahlen
    /// und Zeichen, wie „2001" oder „…und dann kam Polly".
    public static let alle: [String] = [raute] + "ABCDEFGHIJKLMNOPQRSTUVWXYZ".map { String($0) }

    /// Die Raute. Als Unicode-Escape geschrieben, weil `tools/schnittstelle_pruefen.py`
    /// eine Raute vor einem Anführungszeichen für den Anfang eines
    /// Swift-Rohtexts hält und dann die Klammern falsch zählt.
    public static let raute = "\u{23}"

    /// „Buchstabe B, 12 Titel" — für VoiceOver, wie im Browser.
    public static func vorlesen(_ buchstabe: String, anzahl: Int) -> String {
        let name = buchstabe == raute ? String(localized: "Zahlen und Zeichen", bundle: .lumenKit, locale: .lumen) : String(localized: "Buchstabe \(buchstabe)", bundle: .lumenKit, locale: .lumen)
        return anzahl > 0 ? String(localized: "\(name), \(anzahl) Titel", bundle: .lumenKit, locale: .lumen) : name
    }
}

/// Das Raster „Filme" oder „Serien": eine Seite nach der anderen, mit den
/// Genres des Servers als Filter.
///
/// Früher holten die Apps die ersten zweihundert Titel und rechneten die
/// Genres daraus — wer mehr Filme hatte, sah den Rest nie, und ein Genre,
/// das erst weiter hinten vorkam, fehlte bei den Knöpfen. Jetzt fragt das
/// Raster wie der Browser seitenweise (`APIClient.seite`), und der Server
/// nennt die Genres über alle Titel der Art.
///
/// Dazu kommen Sortierung und Buchstabe. Die Leiste der
/// Buchstaben gibt es nur bei A–Z: nach „Neu hinzugefügt" sortiert,
/// spränge ein Buchstabe ins Leere.
@MainActor
@Observable
public final class Raster {

    /// movie | series
    public let kind: String
    /// Das gewählte Genre. nil heisst: alle.
    public private(set) var genre: String?
    public private(set) var sortierung: Sortierung = .titel
    /// Der gewählte Anfangsbuchstabe („A" … „Z", oder die Raute). nil heisst: alle.
    public private(set) var buchstabe: String?
    /// Wie viele Titel mit welchem Buchstaben anfangen — für das gewählte
    /// Genre. Leer, solange nichts geladen ist oder nicht nach A–Z sortiert.
    public private(set) var buchstaben: [String: Int] = [:]
    public private(set) var items: [Item] = []
    /// Die Genres aller Titel dieser Art — sie bleiben stehen, auch wenn
    /// eines gewählt ist.
    public private(set) var genres: [String] = []
    public private(set) var gesamt = 0
    public private(set) var weiter = false
    public private(set) var laedt = false
    /// Mindestens einmal fertig geladen — erst dann heisst „leer" wirklich leer.
    public private(set) var geladen = false
    public private(set) var error: String?

    /// Wie viele Titel eine Seite hat — dieselbe Zahl wie im Browser.
    public static let seitengroesse = 60
    /// So viele Titel vor dem Ende wird schon die nächste Seite geholt.
    public static let vorlauf = 12

    private let client: APIClient
    /// Zählt jedes neue Laden hoch. Kommt eine alte Antwort zu spät — ein
    /// anderes Genre war schneller gewählt —, wird sie verworfen.
    @ObservationIgnored private var stand = 0

    public init(client: APIClient, kind: String, genre: String? = nil) {
        self.client = client
        self.kind = kind
        self.genre = genre
    }

    /// Ob die Leiste der Buchstaben gezeigt wird.
    public var zeigtBuchstaben: Bool { sortierung == .titel && !buchstaben.isEmpty }

    /// Wie viele Titel mit diesem Buchstaben anfangen. 0 heisst: Knopf grau.
    public func anzahl(_ buchstabe: String) -> Int { buchstaben[buchstabe] ?? 0 }

    /// Von vorn laden, mit diesem Genre (nil: alle). Ein anderes Genre hebt
    /// den Buchstaben auf — dort gibt es ihn vielleicht gar nicht.
    public func laden(genre: String?) async {
        if genre != self.genre { buchstabe = nil }
        self.genre = genre
        await vonVorn(buchstabenAuch: true)
    }

    /// Anders sortieren. Der Buchstabe gilt danach nicht mehr.
    public func sortieren(_ neu: Sortierung) async {
        guard neu != sortierung else { return }
        sortierung = neu
        buchstabe = nil
        await vonVorn(buchstabenAuch: true)
    }

    /// Zu einem Buchstaben springen — nil zeigt wieder alle. Die Zahlen an
    /// den Buchstaben bleiben dabei dieselben.
    public func springe(zu neu: String?) async {
        guard neu != buchstabe else { return }
        buchstabe = neu
        await vonVorn(buchstabenAuch: false)
    }

    /// Mit Genre, Sortierung und Buchstabe neu laden — etwa beim Herunterziehen.
    public func neu() async { await vonVorn(buchstabenAuch: true) }

    private func vonVorn(buchstabenAuch: Bool) async {
        stand += 1
        let meiner = stand
        laedt = true
        error = nil
        do {
            let seite = try await client.seite(kind: kind, genre: genre, sort: sortierung.rawValue,
                                               buchstabe: buchstabe,
                                               limit: Self.seitengroesse, offset: 0)
            guard meiner == stand else { return }
            items = seite.items
            if let neu = seite.genres { genres = neu }
            gesamt = seite.gesamt
            weiter = seite.weiter
        } catch {
            guard meiner == stand else { return }
            self.error = Session.text(error)
        }
        // Die Zahlen an den Buchstaben: ein Fehlschlag kostet nur die
        // Leiste, nicht das Raster.
        if buchstabenAuch {
            if sortierung == .titel {
                let zahlen = (try? await client.buchstaben(kind: kind, genre: genre)) ?? [:]
                guard meiner == stand else { return }
                buchstaben = zahlen
            } else {
                buchstaben = [:]
            }
        }
        laedt = false
        geladen = true
    }

    /// Die nächste Seite anhängen, falls es eine gibt.
    public func mehr() async {
        guard weiter, !laedt else { return }
        let meiner = stand
        laedt = true
        do {
            let seite = try await client.seite(kind: kind, genre: genre, sort: sortierung.rawValue,
                                               buchstabe: buchstabe,
                                               limit: Self.seitengroesse, offset: items.count)
            guard meiner == stand else { return }
            // Kam inzwischen ein Titel dazu, verschiebt sich die Seite um
            // einen — dann stünde er doppelt da. Lieber einmal.
            let bekannt = Set(items.map(\.id))
            items += seite.items.filter { !bekannt.contains($0.id) }
            gesamt = seite.gesamt
            weiter = seite.weiter && !seite.items.isEmpty
        } catch {
            guard meiner == stand else { return }
            self.error = Session.text(error)
        }
        laedt = false
    }

    /// Eine Karte ist erschienen: liegt sie nah am Ende, die nächste Seite
    /// holen. So lädt das Raster beim Scrollen nach, ohne Knopf.
    public func erscheint(_ item: Item) async {
        guard weiter, !laedt,
              let i = items.firstIndex(where: { $0.id == item.id }),
              i >= items.count - Self.vorlauf
        else { return }
        await mehr()
    }

    /// „248 Titel · Drama · B" — die Zeile unter der Überschrift.
    public var zeile: String { Self.zeile(gesamt: gesamt, genre: genre, buchstabe: buchstabe) }

    public nonisolated static func zeile(gesamt: Int, genre: String?, buchstabe: String? = nil) -> String {
        var text = String(localized: "\(gesamt) Titel", bundle: .lumenKit, locale: .lumen)
        if let genre, !genre.isEmpty { text += " · " + genre }
        if let buchstabe, !buchstabe.isEmpty { text += " · " + buchstabe }
        return text
    }
}

// MARK: - Sprachen in den Einstellungen

/// Welche Sprache beim Ton vorgewählt wird und welche Untertitel laufen —
/// dieselben Prefs, die der Player beim Wählen ohnehin merkt
/// (`tonSprache`, `untertitel`). In den Einstellungen lassen sie sich hier
/// auch vorab setzen, am Fernseher wie am iPhone.
public enum Sprachwahl {

    public struct Wahl: Hashable, Identifiable, Sendable {
        public let wert: String
        public let name: String
        public var id: String { wert }

        public init(wert: String, name: String) {
            self.wert = wert
            self.name = name
        }
    }

    public static let prefTon = "tonSprache"
    public static let prefUntertitel = "untertitel"

    /// Die häufigsten zuerst, der Rest nach dem Namen.
    public static var sprachen: [Wahl] {
        let vorn = ["de", "en", "fr", "it"]
        let rest = Spuren.sprachen.keys.filter { !vorn.contains($0) }
            .sorted { Spuren.sprachName($0) < Spuren.sprachName($1) }
        return (vorn + rest).map { Wahl(wert: $0, name: Spuren.sprachName($0)) }
    }

    /// Leer heisst: die erste Tonspur, so wie die Datei sie mitbringt.
    public static var ton: [Wahl] { [Wahl(wert: "", name: String(localized: "Wie in der Datei", bundle: .lumenKit, locale: .lumen))] + sprachen }

    /// „aus" heisst aus — erzwungene Untertitel laufen trotzdem mit.
    public static var untertitel: [Wahl] { [Wahl(wert: "aus", name: String(localized: "Aus", bundle: .lumenKit, locale: .lumen))] + sprachen }

    public static func tonName(_ wert: String?) -> String {
        guard let wert, !wert.isEmpty else { return String(localized: "Wie in der Datei", bundle: .lumenKit, locale: .lumen) }
        return Spuren.sprachName(wert)
    }

    public static func untertitelName(_ wert: String?) -> String {
        guard let wert, !wert.isEmpty, wert != "aus" else { return String(localized: "Aus", bundle: .lumenKit, locale: .lumen) }
        return Spuren.sprachName(wert)
    }
}

// MARK: - Titelseite

/// Die Beschriftungen und Reihenfolgen auf der Titelseite.
public enum Titelseite {

    /// Ab wann „Weiter bei …" und „Von vorn" erscheinen — wie im Browser
    /// erst nach einer Minute, sonst lohnt es nicht.
    public static let weiterAb: Double = 60

    /// Staffeln der Reihe nach, die Specials (Staffel 0) ans Ende — so wie
    /// der Browser die Reiter ordnet.
    public static func staffeln(_ staffeln: [Item]) -> [Item] {
        let geordnet = AmStueck.geordnet(staffeln)
        return geordnet.filter { ($0.seasonNo ?? 0) != 0 } + geordnet.filter { ($0.seasonNo ?? 0) == 0 }
    }

    /// „Weiter bei 12:34" oder „Ansehen" — für Filme und Folgen.
    public static func abspielen(_ item: Item) -> String {
        item.resumeSeconds > weiterAb
            ? String(localized: "Weiter bei \(Playback.clock(item.resumeSeconds))", bundle: .lumenKit, locale: .lumen)
            : String(localized: "Ansehen", bundle: .lumenKit, locale: .lumen)
    }

    /// Bei einer Serie: schon angefangen? Dann „Weiterschauen".
    public static func serieAbspielen(_ serie: Item) -> String {
        (serie.gesehen ?? 0) > 0 || serie.percent > 1
            ? String(localized: "Weiterschauen", bundle: .lumenKit, locale: .lumen) : String(localized: "Ansehen", bundle: .lumenKit, locale: .lumen)
    }

    /// „Von vorn" nur, wenn es einen gemerkten Stand gibt.
    public static func kannVonVorn(_ item: Item) -> Bool { item.resumeSeconds > weiterAb }

    /// Der Knopf an einer Folge: „Weiter", wenn sie angefangen ist.
    public static func folge(_ folge: Item) -> String {
        folge.percent > 1 && !folge.isSeen ? String(localized: "Weiter", bundle: .lumenKit, locale: .lumen) : String(localized: "Ansehen", bundle: .lumenKit, locale: .lumen)
    }
}

/// Was der Player bekommt, wenn ein einzelner Titel startet: der Titel
/// und ob von vorn. Eine eigene Kennung je Start, damit derselbe Knopf
/// auch zweimal hintereinander wirkt.
public struct Abspielstart: Identifiable, Sendable {
    public let id = UUID()
    public let item: Item
    public let vonVorn: Bool
    /// Die gewählte Fassung — aus „In welcher Qualität?". nil
    /// heisst: die erste, so wie der Server sie ordnet.
    public let datei: String?

    public init(item: Item, vonVorn: Bool = false, datei: String? = nil) {
        self.item = item
        self.vonVorn = vonVorn
        self.datei = datei
    }
}

// MARK: - Filmreihe

extension Reihe {
    /// Wie viele Filme dieses Profil daraus gesehen hat — nur der eigene Stand.
    public var gesehenAnzahl: Int { items.filter(\.isSeen).count }

    /// Der erste Film, der noch nicht gesehen ist; sonst der erste.
    public var naechsterFilm: Item? { items.first { !$0.isSeen } ?? items.first }

    /// „Weiter mit Teil 2" mitten in der Reihe, sonst „Ansehen".
    public var abspielText: String {
        let n = gesehenAnzahl
        if n > 0, n < anzahl, let film = naechsterFilm { return String(localized: "Weiter mit \(film.title)", bundle: .lumenKit, locale: .lumen) }
        return String(localized: "Ansehen", bundle: .lumenKit, locale: .lumen)
    }

    /// „3 Filme · 1 gesehen · zusammengestellt nach TMDb"
    public var unterzeile: String {
        var teile = [anzahlText]
        if gesehenAnzahl > 0 { teile.append(String(localized: "\(gesehenAnzahl) gesehen", bundle: .lumenKit, locale: .lumen)) }
        teile.append(source == "tmdb" ? String(localized: "zusammengestellt nach TMDb", bundle: .lumenKit, locale: .lumen)
                                      : String(localized: "von Lumen am Titel erkannt", bundle: .lumenKit, locale: .lumen))
        return teile.joined(separator: " · ")
    }
}
