// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

import Foundation

/// Fassungen und Teile eines Films — dieselben Regeln wie im
/// Browser (app.js: fassungName, fassungsWahl, startPlayer) und in
/// lumen-core auf Android (Fassungen.kt).
///
/// Zwei Fälle, die beide mehrere Dateien zu einem Titel bedeuten:
///
/// - **Fassungen**: mehrere Dateien mit Teil 1 — derselbe Film als MKV in
///   1080p und als AVI in 400p, oder eine deutsche und eine englische
///   Datei. „Ansehen" spielt gleich die erste; eine andere wählt man im
///   Player (Zahnrad) — die frühere Frage „In welcher Qualität?" vor dem
///   Abspielen gibt es nicht mehr. Der Server liefert sie schon geordnet:
///   die bevorzugte vorn, dann die mit der höchsten Auflösung.
/// - **Teile**: Dateien mit Teil 1, 2, 3 … („Apocalypse Now (1979) a.avi",
///   „b.avi", „c.avi") — ein Film, der nacheinander läuft.
///
/// Steht in LumenKit, damit iPhone, Mac und Fernseher dasselbe zeigen und
/// die Tests jeden Fall ohne Gerät nachstellen können.
public enum Fassungen {

    /// Die Fassungen: jede vorhandene, nicht defekte Datei, die als Teil 1 gilt — in der
    /// Reihenfolge des Servers.
    public static func liste(_ dateien: [MediaFile]) -> [MediaFile] {
        dateien.filter { $0.nutzbar && $0.partNo <= 1 }
    }

    /// Ob es mehr als eine Fassung gibt.
    public static func mehrere(_ dateien: [MediaFile]) -> Bool { liste(dateien).count > 1 }

    /// Die Auflösung, wie man sie kennt: 4K, 1080p, 720p — sonst die
    /// Zeilen, etwa „400p".
    public static func aufloesung(_ hoehe: Int?) -> String? {
        aufloesung(hoehe: hoehe, breite: nil)
    }

    /// Wie oben, aber Breitformat zählt nach der Breite: 1920×800 ist ein
    /// 1080p-Film, 1280×536 einer in 720p — wie im Browser (aufloesungVon).
    public static func aufloesung(hoehe: Int?, breite: Int?) -> String? {
        let h = hoehe ?? 0, w = breite ?? 0
        if h >= 2000 || w >= 3400 { return "4K" }
        if h >= 1000 || w >= 1800 { return "1080p" }
        if h >= 700 || w >= 1200 { return "720p" }
        if h > 0 { return "\(h)p" }
        return nil
    }

    /// Die Auflösung einer Datei, Breitformat nach der Breite.
    public static func aufloesung(_ f: MediaFile) -> String? {
        aufloesung(hoehe: f.height, breite: f.width)
    }

    /// Die Grösse wie im Browser (höchstens eine Nachkommastelle, in der
    /// Schreibweise der gewählten Sprache): „7.9 GB" in der Schweiz, „7,9 GB"
    /// in Deutschland, „8 GB" — unter einem GB in MB, mindestens „1 MB".
    public static func groesse(_ bytes: Int64) -> String? {
        guard bytes > 0 else { return nil }
        if bytes < 1_000_000_000 {
            return Sprache.zahl(max(1, Int((Double(bytes) / 1_000_000).rounded()))) + " MB"
        }
        let zehntel = (Double(bytes) / 100_000_000).rounded()
        return Sprache.zahl(zehntel / 10, stellen: 1) + " GB"
    }

    /// Eine Fassung in der Auswahl: „1080p · MKV · 7,9 GB".
    public static func name(_ f: MediaFile) -> String {
        var teile: [String] = []
        if let a = aufloesung(f) { teile.append(a) }
        if let c = f.container, !c.isEmpty { teile.append(c.uppercased()) }
        if let g = groesse(f.sizeBytes) { teile.append(g) }
        if teile.isEmpty { return (f.path as NSString).lastPathComponent }
        return teile.joined(separator: " · ")
    }

    /// Alle Tonsprachen einer Datei, ohne „und", in der Reihenfolge der
    /// Datei und jede nur einmal: „Deutsch, Englisch".
    public static func tonsprachen(_ f: MediaFile) -> [String] {
        var codes: [String] = []
        for t in f.tonspuren {
            guard let c = t.language, !c.isEmpty, c != "und", !codes.contains(c) else { continue }
            codes.append(c)
        }
        return codes
    }

    /// Die Fassungen beschriftet wie im Player des Browsers (app.js,
    /// fassungNamen): Auflösung, alle Tonsprachen, Format und
    /// gegebenenfalls die Edition — „1080p · Deutsch · MKV". Zwei gleich
    /// beschriftete unterscheidet die Grösse, gleiche Kopien der Ordner
    /// (sonst „Kopie 2"); ohne alles „Fassung 2".
    ///
    /// Eine deutsche und eine englische Datei desselben Films (Police
    /// Academy) heissen so „1080p · Deutsch · MKV" und „1080p · Englisch ·
    /// MKV" — vorher stand nur die erste Tonspur da.
    public static func namen(_ fassungen: [MediaFile]) -> [String] {
        var namen: [String] = fassungen.map { f in
            var teile: [String] = []
            if let a = aufloesung(f) { teile.append(a) }
            let sprachen = tonsprachen(f).map { Spuren.sprachName($0) }
            if !sprachen.isEmpty { teile.append(sprachen.joined(separator: ", ")) }
            if let c = f.container, !c.isEmpty { teile.append(c.uppercased()) }
            if let e = f.edition, !e.isEmpty { teile.append(e) }
            return teile.joined(separator: " · ")
        }
        func doppelt(_ liste: [String], _ i: Int) -> Bool { liste.filter { $0 == liste[i] }.count > 1 }
        let mitGroesse: [String] = namen.indices.map { i in
            guard !namen[i].isEmpty, doppelt(namen, i), let g = groesse(fassungen[i].sizeBytes) else { return namen[i] }
            return namen[i] + " · " + g
        }
        namen = mitGroesse
        let mitOrdner: [String] = namen.indices.map { i in
            guard !namen[i].isEmpty, doppelt(namen, i) else { return namen[i] }
            let teile = fassungen[i].path.split(whereSeparator: { $0 == "/" || $0 == "\\" })
            if teile.count >= 2 { return namen[i] + " · " + String(teile[teile.count - 2]) }
            let nummer: Int = i + 1
            return namen[i] + " · " + String(localized: "Kopie \(nummer)", bundle: .lumenKit, locale: .lumen)
        }
        return mitOrdner.enumerated().map { i, n in
            let nummer: Int = i + 1
            return n.isEmpty ? String(localized: "Fassung \(nummer)", bundle: .lumenKit, locale: .lumen) : n
        }
    }

    /// Der Hinweis auf „Ansehen", wenn es mehrere Fassungen gibt: „2
    /// Sprachen", wenn sie sich nur in der Tonsprache unterscheiden, „2
    /// Qualitäten", wenn nur in der Auflösung, sonst „2 Fassungen" — wie im
    /// Browser (fassungenHinweis). Vorher hiess eine deutsche und eine
    /// englische Datei „2 Qualitäten". nil bei höchstens einer Fassung.
    public static func hinweis(_ fassungen: [MediaFile]) -> String? {
        let n = fassungen.count
        guard n > 1 else { return nil }
        func verschieden(_ wert: (MediaFile) -> String) -> Bool { Set(fassungen.map(wert)).count > 1 }
        let sprache = verschieden { $0.tonspuren.first?.language ?? "" }
        let aufl = verschieden { aufloesung($0) ?? "" }
        let sonst = verschieden { ($0.container ?? "").lowercased() + "|" + ($0.edition ?? "") }
        if sprache && !aufl && !sonst { return String(localized: "\(n) Sprachen", bundle: .lumenKit, locale: .lumen) }
        if aufl && !sprache && !sonst { return String(localized: "\(n) Qualitäten", bundle: .lumenKit, locale: .lumen) }
        return String(localized: "\(n) Fassungen", bundle: .lumenKit, locale: .lumen)
    }

    /// Welche Datei spielt: die gewählte, solange es sie noch gibt — sonst
    /// die erste vorhandene, wie bisher. Eine Kennung aus einer älteren
    /// Auswahl, die inzwischen fehlt, soll nicht ins Leere führen.
    public static func datei(_ dateien: [MediaFile], gewaehlt: String?) -> MediaFile? {
        if let gewaehlt, let f = dateien.first(where: { $0.id == gewaehlt && $0.nutzbar }) {
            return f
        }
        return dateien.first { $0.nutzbar && $0.partNo <= 1 } ?? dateien.first { $0.nutzbar }
    }

    /// Die Teile, die zu `erste` gehören: Teil 1 ist die gewählte Datei
    /// (sofern sie Teil 1 ist), dann Teil 2, 3 … der Reihe nach, bis einer
    /// fehlt. Wie im Browser: Fehlt Teil 3, läuft der Film bis Teil 2.
    public static func teile(_ dateien: [MediaFile], erste: MediaFile) -> FilmTeile {
        var liste: [MediaFile] = []
        var nr = 1
        while true {
            let t: MediaFile? = nr == 1 && erste.partNo <= 1
                ? erste
                : dateien.first(where: { $0.nutzbar && max(1, $0.partNo) == nr })
            guard let t else { break }
            liste.append(t)
            nr += 1
        }
        // Eine Datei, die nicht Teil 1 ist und keinen Teil 1 neben sich hat,
        // spielt trotzdem — allein, wie bisher.
        return FilmTeile(dateien: liste.isEmpty ? [erste] : liste)
    }
}

/// Ein Film in Teilen. Der gespeicherte Stand zählt über alle Teile: die
/// Position ist die Dauer der Teile davor plus die Stelle im Teil, die
/// Dauer die Summe aller Teile — genau wie im Browser, damit „Weiter bei"
/// auf allen Geräten dasselbe meint.
///
/// Bei nur einer Datei ist alles wie bisher: Teil 0, nichts davor.
public struct FilmTeile: Sendable, Equatable {

    /// Wo eine Gesamtstelle liegt: in welchem Teil, was davor kommt, und
    /// wo darin.
    public struct Stelle: Sendable, Equatable {
        public let teil: Int
        public let vorher: Double
        public let imTeil: Double

        public init(teil: Int, vorher: Double, imTeil: Double) {
            self.teil = teil
            self.vorher = vorher
            self.imTeil = imTeil
        }
    }

    public let dateien: [MediaFile]

    public init(dateien: [MediaFile]) { self.dateien = dateien }

    public var anzahl: Int { dateien.count }
    public var mehrere: Bool { dateien.count > 1 }

    /// Die Dauer aller Teile zusammen, in Sekunden.
    public var gesamtSekunden: Double { dateien.reduce(0) { $0 + $1.durationSeconds } }

    /// Die Dauer der Teile vor `teil`, in Sekunden.
    public func vorher(_ teil: Int) -> Double {
        dateien.prefix(max(0, min(teil, dateien.count))).reduce(0) { $0 + $1.durationSeconds }
    }

    public func istLetzter(_ teil: Int) -> Bool { teil >= dateien.count - 1 }

    /// Aus einer Gesamtstelle der Teil und die Stelle darin. Ein Stand
    /// hinter dem Ende (fünf Sekunden Luft) heisst: von vorn — wie im
    /// Browser, damit ein fertig geschauter Film nicht im Leeren beginnt.
    public func stelle(_ gesamt: Double) -> Stelle {
        guard mehrere else { return Stelle(teil: 0, vorher: 0, imTeil: gesamt) }
        let summe = gesamtSekunden
        var start = gesamt
        if summe > 0, start > summe - 5 { start = 0 }
        var teil = 0
        var vorher = 0.0
        while teil < dateien.count - 1, start >= vorher + dateien[teil].durationSeconds {
            vorher += dateien[teil].durationSeconds
            teil += 1
        }
        return Stelle(teil: teil, vorher: vorher, imTeil: start - vorher)
    }

    /// Die Gesamtstelle, mit der der Teil nach `teil` beginnt — eine halbe
    /// Sekunde hinter der Grenze, damit `stelle` sicher im nächsten Teil
    /// landet (wie im Browser in amEnde).
    public func weiterAb(_ teil: Int) -> Double {
        let dauer = dateien.indices.contains(teil) ? dateien[teil].durationSeconds : 0
        return vorher(teil) + dauer + 0.5
    }

    /// Was im Player steht: „Teil 2 von 3" — bei einer Datei nichts.
    public func beschriftung(_ teil: Int) -> String? {
        mehrere ? String(localized: "Teil \(teil + 1) von \(anzahl)", bundle: .lumenKit, locale: .lumen) : nil
    }
}
