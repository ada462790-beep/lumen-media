// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

import Foundation

// Tonspuren und Untertitel: wie sie heissen, welche vorbelegt wird, und
// wie aus einer WebVTT-Datei die einzelnen Einträge werden.
//
// Alles hier ist reine Rechnung ohne Netz und ohne AVFoundation, damit es
// sich prüfen lässt und iPhone, Mac und Fernseher gleich entscheiden. Die
// Web-App macht es genauso (app.js: spurName, spurenVorbelegen, vttLesen).

public enum Spuren {

    /// Die Sprachen ausgeschrieben — dieselbe Tabelle wie im Server, damit
    /// das Menü der App und die HLS-Liste denselben Namen zeigen.
    ///
    /// Berechnet statt fest: die Namen stehen in der Sprache des Profils.
    public static var sprachen: [String: String] {
        [
            "de": String(localized: "Deutsch", bundle: .lumenKit, locale: .lumen),
            "en": String(localized: "Englisch", bundle: .lumenKit, locale: .lumen),
            "fr": String(localized: "Französisch", bundle: .lumenKit, locale: .lumen),
            "it": String(localized: "Italienisch", bundle: .lumenKit, locale: .lumen),
            "es": String(localized: "Spanisch", bundle: .lumenKit, locale: .lumen),
            "ja": String(localized: "Japanisch", bundle: .lumenKit, locale: .lumen),
            "ko": String(localized: "Koreanisch", bundle: .lumenKit, locale: .lumen),
            "pt": String(localized: "Portugiesisch", bundle: .lumenKit, locale: .lumen),
            "nl": String(localized: "Niederländisch", bundle: .lumenKit, locale: .lumen),
            "ru": String(localized: "Russisch", bundle: .lumenKit, locale: .lumen),
            "pl": String(localized: "Polnisch", bundle: .lumenKit, locale: .lumen),
            "tr": String(localized: "Türkisch", bundle: .lumenKit, locale: .lumen),
            "sv": String(localized: "Schwedisch", bundle: .lumenKit, locale: .lumen),
            "da": String(localized: "Dänisch", bundle: .lumenKit, locale: .lumen),
            "zh": String(localized: "Chinesisch", bundle: .lumenKit, locale: .lumen),
        ]
    }

    /// „Deutsch", „EN" für Unbekanntes mit Kürzel, „Unbekannt" ohne.
    public static func sprachName(_ code: String?) -> String {
        guard let code, !code.isEmpty, code != "und" else { return String(localized: "Unbekannt", bundle: .lumenKit, locale: .lumen) }
        return sprachen[code] ?? code.uppercased()
    }

    /// Der Anzeigename einer Spur: Sprache, dann in Klammern, was
    /// dransteht — Titel, „erzwungen", „für Hörgeschädigte", die Kanäle
    /// beim Ton, „Datei" bei Untertiteln neben dem Film.
    public static func name(_ t: Track) -> String {
        let name = sprachName(t.language)
        var zusatz: [String] = []
        if let title = t.title, !title.isEmpty, title.lowercased() != name.lowercased() {
            zusatz.append(title)
        }
        if t.isForced { zusatz.append(String(localized: "erzwungen", bundle: .lumenKit, locale: .lumen)) }
        if t.isHearingImpaired { zusatz.append(String(localized: "für Hörgeschädigte", bundle: .lumenKit, locale: .lumen)) }
        if t.isAudio, let channels = t.channels, channels > 2 { zusatz.append(String(channels - 1) + ".1") }
        if t.isExternal { zusatz.append(String(localized: "Datei", bundle: .lumenKit, locale: .lumen)) }
        return zusatz.isEmpty ? name : name + " (" + zusatz.joined(separator: ", ") + ")"
    }

    /// Was beim Start läuft, nach den gemerkten Sprachen.
    ///
    /// `ton` ist der Index der Tonspur in der gewünschten Sprache
    /// (`prefs.tonSprache`) — oder nil, wenn es sie nicht gibt; dann
    /// bleibt es bei der ersten. `untertitel` ist der Index der
    /// Untertitelspur in der gewünschten Sprache (`prefs.untertitel`,
    /// „aus" heisst aus). Will niemand Untertitel, laufen erzwungene
    /// Untertitel in der Sprache des Tons trotzdem mit — dafür sind sie da.
    public static func vorbelegung(ton: [Track], text: [Track],
                                   prefs: [String: String]) -> (ton: Int?, untertitel: Int?) {
        var tonIndex: Int?
        if let sprache = prefs["tonSprache"], !sprache.isEmpty,
           let t = ton.first(where: { $0.sprache == sprache }) {
            tonIndex = t.index
        }
        var untertitel: Int?
        if let sprache = prefs["untertitel"], !sprache.isEmpty, sprache != "aus" {
            let u = text.first { $0.sprache == sprache && !$0.isForced }
                ?? text.first { $0.sprache == sprache }
            untertitel = u?.index
        } else {
            let aktuell = ton.first { $0.index == tonIndex } ?? ton.first
            if let aktuell, let erzwungen = text.first(where: { $0.isForced && $0.sprache == aktuell.sprache }) {
                untertitel = erzwungen.index
            }
        }
        return (tonIndex, untertitel)
    }
}

// MARK: - WebVTT

/// Ein Eintrag aus einer Untertitel-Datei: von wann bis wann, und der
/// Text ohne Auszeichnungen.
public struct Untertitel: Hashable, Sendable {
    public let von: Double
    public let bis: Double
    public let text: String

    public init(von: Double, bis: Double, text: String) {
        self.von = von
        self.bis = bis
        self.text = text
    }

    /// Liest WebVTT: Blöcke, getrennt durch Leerzeilen, jeder mit einer
    /// Zeile „hh:mm:ss.mmm --> hh:mm:ss.mmm" und dem Text darunter.
    /// Kennungen vor der Zeitzeile, Einstellungen dahinter und Tags im
    /// Text (<i>, <c.farbe>, <00:01.000>) fallen weg. Windows-Zeilenenden
    /// und eine BOM stören nicht.
    public static func lesen(_ vtt: String) -> [Untertitel] {
        var quelle = vtt.replacingOccurrences(of: "\r\n", with: "\n")
            .replacingOccurrences(of: "\r", with: "\n")
        if quelle.hasPrefix("\u{FEFF}") { quelle.removeFirst() }

        var out: [Untertitel] = []
        for block in quelle.components(separatedBy: "\n\n") {
            let zeilen = block.split(separator: "\n", omittingEmptySubsequences: false).map(String.init)
            guard let i = zeilen.firstIndex(where: { $0.contains("-->") }) else { continue }
            let teile = zeilen[i].components(separatedBy: "-->")
            guard teile.count >= 2, let von = zeit(teile[0]), let bis = zeit(teile[1]), bis > von
            else { continue }
            let text = ohneTags(zeilen[(i + 1)...].joined(separator: "\n"))
                .trimmingCharacters(in: .whitespacesAndNewlines)
            if !text.isEmpty { out.append(Untertitel(von: von, bis: bis, text: text)) }
        }
        return out.sorted { $0.von < $1.von }
    }

    /// „01:02:03.456" oder „02:03.456" — die Stunden sind wahlfrei. Hinter
    /// der Endzeit dürfen Einstellungen stehen („align:start"); die werden
    /// mit dem ersten Leerzeichen abgeschnitten.
    public static func zeit(_ roh: String) -> Double? {
        let text = roh.trimmingCharacters(in: .whitespaces)
            .split(separator: " ", maxSplits: 1).first.map(String.init) ?? ""
        let teile = text.split(separator: ":").map(String.init)
        guard teile.count == 2 || teile.count == 3 else { return nil }
        let sekundenTeil = teile.last!.replacingOccurrences(of: ",", with: ".")
        guard let sekunden = Double(sekundenTeil) else { return nil }
        var ganze: [Int] = []
        for t in teile.dropLast() {
            guard let n = Int(t) else { return nil }
            ganze.append(n)
        }
        let stunden = ganze.count == 2 ? ganze[0] : 0
        let minuten = ganze.count == 2 ? ganze[1] : ganze[0]
        return Double(stunden) * 3600 + Double(minuten) * 60 + sekunden
    }

    /// Tags wie <i>, </b>, <c.yellow> und Zeitmarken <00:01:02.000> weg;
    /// die drei Entitäten, die in Untertiteln vorkommen, zurückwandeln.
    public static func ohneTags(_ text: String) -> String {
        var out = ""
        var inTag = false
        for zeichen in text {
            if zeichen == "<" { inTag = true; continue }
            if zeichen == ">" { inTag = false; continue }
            if !inTag { out.append(zeichen) }
        }
        return out
            .replacingOccurrences(of: "&amp;", with: "&")
            .replacingOccurrences(of: "&lt;", with: "<")
            .replacingOccurrences(of: "&gt;", with: ">")
            .replacingOccurrences(of: "&nbsp;", with: " ")
    }

    /// Der Text, der an dieser Stelle steht — mehrere Einträge zugleich
    /// werden untereinander gezeigt. nil, wenn gerade nichts dasteht.
    public static func text(at sekunden: Double, in eintraege: [Untertitel]) -> String? {
        let aktiv = eintraege.filter { $0.von <= sekunden && sekunden < $0.bis }
        guard !aktiv.isEmpty else { return nil }
        return aktiv.map(\.text).joined(separator: "\n")
    }
}
