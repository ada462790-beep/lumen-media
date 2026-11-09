// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

import Foundation

// Profilbilder.
//
// Drei Arten, wie im Browser (app.js: avatar, FIGUREN): eine Farbe
// („av-a" …), eine eingebaute Figur („fig-fuchs" …) oder ein eigenes Bild
// („bild"), das der Server unter /avatar/{konto} ausliefert. Die Figuren
// sind kleine Strichzeichnungen — eigene, keine fremden Marken. Sie
// stehen hier als dieselben SVG-Pfade wie im Browser und werden in
// Linienzüge übersetzt, die jede Oberfläche zeichnen kann.

public enum Profilbild: Equatable, Sendable {
    case farbe(String)
    case figur(String)
    case bild

    /// Die Farben, die der Server kennt.
    public static let farben = ["av-a", "av-b", "av-c", "av-d", "av-k"]

    public init(_ wert: String?) {
        let a = (wert ?? "").isEmpty ? "av-a" : wert!
        if a == "bild" {
            self = .bild
        } else if a.hasPrefix("fig-"), Figuren.quellen[String(a.dropFirst(4))] != nil {
            self = .figur(String(a.dropFirst(4)))
        } else {
            self = .farbe(a)
        }
    }

    /// Der Wert, wie er am Konto steht.
    public var wert: String {
        switch self {
        case .farbe(let f): f
        case .figur(let f): "fig-" + f
        case .bild: "bild"
        }
    }
}

// MARK: - Figuren

public enum Figuren {

    /// Eine Figur: Hintergrundfarbe und Zeichnung im Raster 24 × 24.
    public struct Figur: Sendable {
        public let name: String
        /// Hintergrund als RGB, 0…1.
        public let farbe: RGB
        /// Was gezogen wird (weisse Linie) und was gefüllt (die Augen).
        public let linien: [[PfadSchritt]]
        public let flaechen: [[PfadSchritt]]
    }

    /// Eine Farbe als Rot, Grün, Blau zwischen 0 und 1.
    public struct RGB: Equatable, Sendable {
        public let r: Double
        public let g: Double
        public let b: Double
    }

    /// Ein Schritt eines Linienzugs, in Koordinaten des 24er-Rasters.
    public enum PfadSchritt: Equatable, Sendable {
        case move(Double, Double)
        case line(Double, Double)
        case curve(Double, Double, Double, Double, Double, Double)   // c1, c2, Ziel
        case close
    }

    /// Dieselbe Reihenfolge wie im Server (`Figuren` in profil.go).
    public static let namen = ["fuchs", "eule", "katze", "roboter", "rakete", "stern",
                               "wal", "blume", "pilz", "geist", "drache", "pinguin"]

    /// Die SVG-Quellen, wörtlich aus app.js.
    static let quellen: [String: (farbe: String, svg: String)] = [
        "fuchs":   ("#f59e5b", ##"<path d="M6 7l3 4h6l3-4-2 9c-1 2-3 3-4 3s-3-1-4-3z"/><circle cx="9.5" cy="13" r=".8" fill="#fff"/><circle cx="14.5" cy="13" r=".8" fill="#fff"/>"##),
        "eule":    ("#8b7bd1", #"<circle cx="12" cy="13" r="7"/><circle cx="9.5" cy="12" r="2"/><circle cx="14.5" cy="12" r="2"/><path d="M12 14l-1 2h2z M7 7l2 2M17 7l-2 2"/>"#),
        "katze":   ("#e07aa0", #"<path d="M6 6l2 4h8l2-4v9a6 6 0 0 1-12 0z"/><path d="M9 13h1M14 13h1M10 17c1 1 3 1 4 0"/>"#),
        "roboter": ("#6fb0e0", #"<rect x="6" y="8" width="12" height="10" rx="2"/><circle cx="9.5" cy="12.5" r="1"/><circle cx="14.5" cy="12.5" r="1"/><path d="M12 4v4M9 16h6"/>"#),
        "rakete":  ("#ef6b6b", #"<path d="M12 3c3 3 4 7 3 11l-3 3-3-3c-1-4 0-8 3-11z"/><path d="M9 14l-3 2 1-5M15 14l3 2-1-5M12 17v3"/>"#),
        "stern":   ("#f2c84b", #"<path d="M12 4l2.4 5 5.6.8-4 3.9.9 5.5-4.9-2.6-4.9 2.6.9-5.5-4-3.9 5.6-.8z"/>"#),
        "wal":     ("#4fa3c7", #"<path d="M4 13c2-5 9-6 14-3l2-3v7c-3 4-9 5-14 2z"/><path d="M15 12h.01M4 13l-1 3"/>"#),
        "blume":   ("#7fcf8b", #"<circle cx="12" cy="10" r="2.5"/><circle cx="12" cy="5" r="2"/><circle cx="17" cy="10" r="2"/><circle cx="7" cy="10" r="2"/><circle cx="12" cy="15" r="2"/><path d="M12 17v4"/>"#),
        "pilz":    ("#d9775b", #"<path d="M4 12c0-5 4-8 8-8s8 3 8 8z"/><path d="M9 12v6a3 3 0 0 0 6 0v-6"/>"#),
        "geist":   ("#b5b5c8", ##"<path d="M6 20V11a6 6 0 0 1 12 0v9l-2-2-2 2-2-2-2 2-2-2z"/><circle cx="9.5" cy="11" r=".8" fill="#fff"/><circle cx="14.5" cy="11" r=".8" fill="#fff"/>"##),
        "drache":  ("#5fb49c", #"<path d="M5 15c2-6 7-9 13-8l-2 3 3 2-4 1c0 4-4 6-8 5z"/><path d="M8 18l-2 3M12 18l0 3"/>"#),
        "pinguin": ("#5c6b8a", #"<ellipse cx="12" cy="13" rx="5" ry="7"/><path d="M10 12l2 2 2-2M7 12l-2 4M17 12l2 4"/>"#),
    ]

    /// Alle Figuren, einmal übersetzt.
    public static let alle: [String: Figur] = {
        var out: [String: Figur] = [:]
        for (name, quelle) in quellen {
            let (linien, flaechen) = SVGLesen.elemente(quelle.svg)
            out[name] = Figur(name: name, farbe: Figuren.farbe(quelle.farbe), linien: linien, flaechen: flaechen)
        }
        return out
    }()

    /// „#f59e5b" als RGB. Unlesbares wird grau.
    public static func farbe(_ hex: String) -> RGB {
        let s = hex.hasPrefix("#") ? String(hex.dropFirst()) : hex
        guard s.count == 6, let v = UInt32(s, radix: 16) else { return RGB(r: 0.5, g: 0.5, b: 0.5) }
        return RGB(r: Double((v >> 16) & 0xff) / 255, g: Double((v >> 8) & 0xff) / 255, b: Double(v & 0xff) / 255)
    }

    /// Die Figur zu einem Namen, in der Reihenfolge des Servers.
    public static func figur(_ name: String) -> Figur? { alle[name] }

    /// Alle Figuren in der Reihenfolge des Servers.
    public static var geordnet: [Figur] { namen.compactMap { alle[$0] } }
}

// MARK: - Ein kleiner SVG-Leser

/// Liest genau das, was die Figuren brauchen: `path` (M L H V C S A Z,
/// gross und klein), `circle`, `ellipse` und `rect` mit `rx`. Bögen
/// werden zu Bézierkurven, damit am Ende nur vier Schrittarten bleiben.
enum SVGLesen {
    typealias Schritt = Figuren.PfadSchritt

    /// Alle Elemente einer Zeichnung; mit `fill` gefüllt, sonst gezogen.
    static func elemente(_ svg: String) -> (linien: [[Schritt]], flaechen: [[Schritt]]) {
        var linien: [[Schritt]] = []
        var flaechen: [[Schritt]] = []
        let muster = try! NSRegularExpression(pattern: #"<(path|circle|ellipse|rect)\b([^>]*)/?>"#)
        let ns = svg as NSString
        for m in muster.matches(in: svg, range: NSRange(location: 0, length: ns.length)) {
            let tag = ns.substring(with: m.range(at: 1))
            let attr = attribute(ns.substring(with: m.range(at: 2)))
            let zahl = { (k: String) -> Double in Double(attr[k] ?? "") ?? 0 }
            var schritte: [Schritt] = []
            switch tag {
            case "path": schritte = pfad(attr["d"] ?? "")
            case "circle": schritte = ellipse(zahl("cx"), zahl("cy"), zahl("r"), zahl("r"))
            case "ellipse": schritte = ellipse(zahl("cx"), zahl("cy"), zahl("rx"), zahl("ry"))
            case "rect": schritte = rechteck(zahl("x"), zahl("y"), zahl("width"), zahl("height"), zahl("rx"))
            default: break
            }
            if schritte.isEmpty { continue }
            if let f = attr["fill"], f != "none" { flaechen.append(schritte) } else { linien.append(schritte) }
        }
        return (linien, flaechen)
    }

    static func attribute(_ text: String) -> [String: String] {
        var out: [String: String] = [:]
        let muster = try! NSRegularExpression(pattern: #"([a-zA-Z-]+)="([^"]*)""#)
        let ns = text as NSString
        for m in muster.matches(in: text, range: NSRange(location: 0, length: ns.length)) {
            out[ns.substring(with: m.range(at: 1))] = ns.substring(with: m.range(at: 2))
        }
        return out
    }

    /// Zerlegt „M6 7l3 4h6c-1 2-3 3-4 3s-3-1-4-3z" in Befehle und Zahlen.
    static func zeichen(_ d: String) -> [String] {
        var out: [String] = []
        var zahl = ""
        var hatPunkt = false
        func fertig() { if !zahl.isEmpty { out.append(zahl); zahl = ""; hatPunkt = false } }
        for c in d {
            if c.isLetter && c != "e" {
                fertig(); out.append(String(c))
            } else if c == "-" {
                // Ein Minus beginnt eine neue Zahl — ausser nach einem Exponenten.
                if zahl.hasSuffix("e") { zahl.append(c) } else { fertig(); zahl = "-" }
            } else if c == "." {
                if hatPunkt { fertig() }
                zahl.append(c); hatPunkt = true
            } else if c.isNumber || c == "e" {
                zahl.append(c)
            } else {
                fertig()
            }
        }
        fertig()
        return out
    }

    static func pfad(_ d: String) -> [Schritt] {
        let teile = zeichen(d)
        var i = 0
        var out: [Schritt] = []
        var x = 0.0, y = 0.0, startX = 0.0, startY = 0.0
        var letzteC2: (Double, Double)?
        var befehl: Character = "M"

        func zahl() -> Double? {
            guard i < teile.count, let v = Double(teile[i]) else { return nil }
            i += 1
            return v
        }
        func istZahl() -> Bool { i < teile.count && Double(teile[i]) != nil }

        while i < teile.count {
            if !istZahl() {
                befehl = Character(teile[i]); i += 1
                if befehl == "z" || befehl == "Z" {
                    out.append(.close); x = startX; y = startY; letzteC2 = nil
                    continue
                }
            }
            let rel = befehl.isLowercase
            let dx = rel ? x : 0, dy = rel ? y : 0
            switch befehl.lowercased() {
            case "m":
                guard let a = zahl(), let b = zahl() else { return out }
                x = dx + a; y = dy + b; startX = x; startY = y
                out.append(.move(x, y)); letzteC2 = nil
                // Weitere Paare nach einem M sind Linien.
                befehl = rel ? "l" : "L"
            case "l":
                guard let a = zahl(), let b = zahl() else { return out }
                x = dx + a; y = dy + b; out.append(.line(x, y)); letzteC2 = nil
            case "h":
                guard let a = zahl() else { return out }
                x = dx + a; out.append(.line(x, y)); letzteC2 = nil
            case "v":
                guard let b = zahl() else { return out }
                y = (rel ? y : 0) + b; out.append(.line(x, y)); letzteC2 = nil
            case "c":
                guard let a = zahl(), let b = zahl(), let c = zahl(), let d = zahl(),
                      let e = zahl(), let f = zahl() else { return out }
                out.append(.curve(dx + a, dy + b, dx + c, dy + d, dx + e, dy + f))
                letzteC2 = (dx + c, dy + d); x = dx + e; y = dy + f
            case "s":
                guard let c = zahl(), let d = zahl(), let e = zahl(), let f = zahl() else { return out }
                // Der erste Haltepunkt spiegelt den letzten der Kurve davor.
                let c1 = letzteC2.map { (2 * x - $0.0, 2 * y - $0.1) } ?? (x, y)
                out.append(.curve(c1.0, c1.1, dx + c, dy + d, dx + e, dy + f))
                letzteC2 = (dx + c, dy + d); x = dx + e; y = dy + f
            case "a":
                guard let rx = zahl(), let ry = zahl(), let rot = zahl(), let gross = zahl(),
                      let richtung = zahl(), let ex = zahl(), let ey = zahl() else { return out }
                out += bogen(von: (x, y), nach: (dx + ex, dy + ey), rx: rx, ry: ry,
                             drehung: rot, gross: gross != 0, imUhrzeigersinn: richtung != 0)
                x = dx + ex; y = dy + ey; letzteC2 = nil
            default:
                // Unbekannter Befehl: den Rest nicht raten.
                return out
            }
        }
        return out
    }

    /// Ein Kreis oder eine Ellipse aus vier Bézierkurven.
    static func ellipse(_ cx: Double, _ cy: Double, _ rx: Double, _ ry: Double) -> [Schritt] {
        guard rx > 0, ry > 0 else { return [] }
        let k = 0.5522847498
        return [
            .move(cx + rx, cy),
            .curve(cx + rx, cy + k * ry, cx + k * rx, cy + ry, cx, cy + ry),
            .curve(cx - k * rx, cy + ry, cx - rx, cy + k * ry, cx - rx, cy),
            .curve(cx - rx, cy - k * ry, cx - k * rx, cy - ry, cx, cy - ry),
            .curve(cx + k * rx, cy - ry, cx + rx, cy - k * ry, cx + rx, cy),
            .close,
        ]
    }

    /// Ein Rechteck mit runden Ecken.
    static func rechteck(_ x: Double, _ y: Double, _ w: Double, _ h: Double, _ r0: Double) -> [Schritt] {
        guard w > 0, h > 0 else { return [] }
        let r = min(max(0, r0), w / 2, h / 2)
        if r == 0 {
            return [.move(x, y), .line(x + w, y), .line(x + w, y + h), .line(x, y + h), .close]
        }
        let k = 0.5522847498 * r
        return [
            .move(x + r, y),
            .line(x + w - r, y),
            .curve(x + w - r + k, y, x + w, y + r - k, x + w, y + r),
            .line(x + w, y + h - r),
            .curve(x + w, y + h - r + k, x + w - r + k, y + h, x + w - r, y + h),
            .line(x + r, y + h),
            .curve(x + r - k, y + h, x, y + h - r + k, x, y + h - r),
            .line(x, y + r),
            .curve(x, y + r - k, x + r - k, y, x + r, y),
            .close,
        ]
    }

    /// Ein SVG-Bogen als Bézierkurven — die Umrechnung aus dem Anhang
    /// F.6 der SVG-Beschreibung, in Stücke von höchstens 90 Grad.
    static func bogen(von p0: (Double, Double), nach p1: (Double, Double), rx rx0: Double, ry ry0: Double,
                      drehung: Double, gross: Bool, imUhrzeigersinn: Bool) -> [Schritt] {
        var rx = abs(rx0), ry = abs(ry0)
        if rx == 0 || ry == 0 || (p0.0 == p1.0 && p0.1 == p1.1) { return [.line(p1.0, p1.1)] }
        let phi = drehung * .pi / 180
        let cosP = cos(phi), sinP = sin(phi)
        let dx2 = (p0.0 - p1.0) / 2, dy2 = (p0.1 - p1.1) / 2
        let x1 = cosP * dx2 + sinP * dy2
        let y1 = -sinP * dx2 + cosP * dy2
        // Zu kleine Halbachsen vergrössern.
        let lambda = (x1 * x1) / (rx * rx) + (y1 * y1) / (ry * ry)
        if lambda > 1 { rx *= lambda.squareRoot(); ry *= lambda.squareRoot() }
        let zaehler = rx * rx * ry * ry - rx * rx * y1 * y1 - ry * ry * x1 * x1
        let nenner = rx * rx * y1 * y1 + ry * ry * x1 * x1
        var faktor = nenner == 0 ? 0 : (max(0, zaehler) / nenner).squareRoot()
        if gross == imUhrzeigersinn { faktor = -faktor }
        let cxs = faktor * rx * y1 / ry
        let cys = -faktor * ry * x1 / rx
        let cx = cosP * cxs - sinP * cys + (p0.0 + p1.0) / 2
        let cy = sinP * cxs + cosP * cys + (p0.1 + p1.1) / 2

        func winkel(_ ux: Double, _ uy: Double, _ vx: Double, _ vy: Double) -> Double {
            let a = atan2(ux * vy - uy * vx, ux * vx + uy * vy)
            return a
        }
        let theta1 = winkel(1, 0, (x1 - cxs) / rx, (y1 - cys) / ry)
        var delta = winkel((x1 - cxs) / rx, (y1 - cys) / ry, (-x1 - cxs) / rx, (-y1 - cys) / ry)
        if !imUhrzeigersinn && delta > 0 { delta -= 2 * .pi }
        if imUhrzeigersinn && delta < 0 { delta += 2 * .pi }

        let stuecke = max(1, Int((abs(delta) / (.pi / 2)).rounded(.up)))
        let schritt = delta / Double(stuecke)
        let t = 4.0 / 3.0 * tan(schritt / 4)
        var out: [Schritt] = []
        var a = theta1
        func punkt(_ w: Double) -> (Double, Double) {
            let ex = rx * cos(w), ey = ry * sin(w)
            return (cosP * ex - sinP * ey + cx, sinP * ex + cosP * ey + cy)
        }
        func ableitung(_ w: Double) -> (Double, Double) {
            let ex = -rx * sin(w), ey = ry * cos(w)
            return (cosP * ex - sinP * ey, sinP * ex + cosP * ey)
        }
        for n in 0..<stuecke {
            let b = a + schritt
            let pa = punkt(a), pb = n == stuecke - 1 ? p1 : punkt(b)
            let da = ableitung(a), db = ableitung(b)
            out.append(.curve(pa.0 + t * da.0, pa.1 + t * da.1,
                              pb.0 - t * db.0, pb.1 - t * db.1,
                              pb.0, pb.1))
            a = b
        }
        return out
    }
}
