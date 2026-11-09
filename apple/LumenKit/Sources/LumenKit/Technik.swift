// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

import Foundation

// Die Technik-Zeile auf der Titelseite: Auflösung, HDR, Ton, Sprachen,
// Untertitel und ob dieses Gerät die Datei direkt abspielt — kleine
// umrandete Abzeichen, wie der Player des Apple TV sie zeigt. Die
// Rechnung steht hier ohne Oberfläche, damit iPhone, Mac und Fernseher
// dasselbe sagen wie der Browser (app.js: viewDetail, „technik").

public enum Technik {

    /// Ein Abzeichen. `art` färbt: gewöhnlich umrandet, „gut" grün für
    /// direkt abspielbar, „blau" für umgewandelt.
    public struct Abzeichen: Hashable, Sendable, Identifiable {
        public enum Art: Hashable, Sendable { case normal, untertitel, gut, blau }
        public let text: String
        public let art: Art
        public var id: String { text }

        public init(_ text: String, art: Art = .normal) {
            self.text = text
            self.art = art
        }
    }

    /// „DV" heisst Dolby Vision; alles andere steht so da, wie der Server
    /// es nennt (HDR10, HLG …). Leer heisst: kein HDR.
    public static func hdrName(_ hdr: String?) -> String? {
        guard let hdr, !hdr.isEmpty else { return nil }
        return hdr == "DV" ? "Dolby Vision" : hdr
    }

    /// SD, HD oder 4K aus der Bildhöhe — dieselben Grenzen wie
    /// `store.Qualitaet` im Server.
    public static func qualitaet(hoehe: Int?) -> String? {
        guard let hoehe, hoehe > 0 else { return nil }
        if hoehe >= 2000 { return "4K" }
        if hoehe >= 1000 { return "HD" }
        return "SD"
    }

    /// „5.1", „7.1", „Stereo" — Mono und Unbekanntes bleiben weg.
    public static func tonName(kanaele: Int) -> String? {
        if kanaele > 2 { return String(kanaele - 1) + ".1" }
        if kanaele == 2 { return String(localized: "Stereo", bundle: .lumenKit, locale: .lumen) }
        return nil
    }

    /// Die Sprachen einer Liste von Spuren als Kürzel, in der Reihenfolge
    /// der Datei, ohne Doppelte und ohne „und".
    public static func sprachen(_ spuren: [Track]) -> [String] {
        var gesehen = Set<String>()
        var out: [String] = []
        for spur in spuren {
            let kurz = spur.sprache.uppercased()
            if kurz == "UND" || gesehen.contains(kurz) { continue }
            gesehen.insert(kurz)
            out.append(kurz)
        }
        return out
    }

    /// Die ganze Zeile. Ohne Datei und ohne Angaben am Titel: leer.
    ///
    /// Die Datei geht vor, wo sie etwas weiss — sie ist das, was gespielt
    /// wird. Am Titel steht, was der Server für die Karten zusammenfasst
    /// (bei Serien: die beste Datei ihrer Folgen).
    public static func zeile(item: Item, file: MediaFile?, plan: PlaybackPlan?) -> [Abzeichen] {
        if file == nil && (item.qualitaet ?? "").isEmpty { return [] }
        var out: [Abzeichen] = []

        let q = item.qualitaet.flatMap { $0.isEmpty ? nil : $0 } ?? qualitaet(hoehe: file?.height)
        if let q { out.append(Abzeichen(q)) }
        if let hdr = hdrName(item.hdr) ?? hdrName(file?.hdr) { out.append(Abzeichen(hdr)) }

        let ton = file?.tonspuren ?? []
        let meisteKanaele = ton.map { $0.channels ?? 0 }.max() ?? 0
        let kanaele = meisteKanaele > 0 ? meisteKanaele : (item.kanaele ?? 0)
        if let t = tonName(kanaele: kanaele) { out.append(Abzeichen(t)) }

        let tonSprachen = sprachen(ton)
        if !tonSprachen.isEmpty { out.append(Abzeichen(tonSprachen.joined(separator: " · "))) }

        let text = file?.textspuren ?? []
        if !text.isEmpty {
            let ut = sprachen(text)
            out.append(Abzeichen(ut.isEmpty ? String(localized: "Untertitel", bundle: .lumenKit, locale: .lumen)
                                             : String(localized: "Untertitel \(ut.joined(separator: " · "))", bundle: .lumenKit, locale: .lumen),
                                 art: .untertitel))
        }
        if let plan {
            out.append(plan.direct ? Abzeichen(String(localized: "Direkt abspielbar", bundle: .lumenKit, locale: .lumen), art: .gut)
                                   : Abzeichen(String(localized: "Wird umgewandelt", bundle: .lumenKit, locale: .lumen), art: .blau))
        }
        return out
    }
}

// MARK: - Untertitel-Darstellung

/// Wie die eigenen Untertitel aussehen: Grösse und ob ein dunkler
/// Hintergrund hinter dem Text liegt. Steht in den Prefs des Kontos
/// (`utGroesse`, `utHintergrund`), dieselben wie im Browser — wer es dort
/// einstellt, hat es auch am Fernseher.
public struct UntertitelStil: Equatable, Sendable {
    public enum Groesse: String, CaseIterable, Sendable {
        case klein, normal, gross

        public var name: String {
            switch self {
            case .klein: String(localized: "Klein", bundle: .lumenKit, locale: .lumen)
            case .normal: String(localized: "Normal", bundle: .lumenKit, locale: .lumen)
            case .gross: String(localized: "Gross", bundle: .lumenKit, locale: .lumen)
            }
        }

        /// Der Faktor auf die gewöhnliche Schriftgrösse — im Browser
        /// liegen klein, normal und gross bei 22, 30 und 42 Pixel.
        public var faktor: Double {
            switch self {
            case .klein: 0.75
            case .normal: 1
            case .gross: 1.4
            }
        }
    }

    public let groesse: Groesse
    public let hintergrund: Bool

    public init(groesse: Groesse = .normal, hintergrund: Bool = true) {
        self.groesse = groesse
        self.hintergrund = hintergrund
    }

    /// Aus den Prefs. Unbekanntes heisst „normal"; der Hintergrund ist an,
    /// solange nicht ausdrücklich „0" dasteht.
    public init(prefs: [String: String]?) {
        self.groesse = Groesse(rawValue: prefs?["utGroesse"] ?? "") ?? .normal
        self.hintergrund = prefs?["utHintergrund"] != "0"
    }

    public static let prefGroesse = "utGroesse"
    public static let prefHintergrund = "utHintergrund"
}
