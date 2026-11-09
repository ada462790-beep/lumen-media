// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

import SwiftUI
import LumenKit
#if canImport(UIKit)
import UIKit
#elseif canImport(AppKit)
import AppKit
#endif

/// Das Aussehen. Dieselben Werte wie in der Web-App, damit sich Lumen auf
/// jedem Gerät gleich anfühlt.
///
/// Auf Apple-Geräten kommt die Schrift vom System: San Francisco ist genau
/// die Schrift, der die Web-Fassung nacheifert. Nichts wird nachgeladen.
public enum Theme {

    // MARK: - Farben
    //
    // Dunkel ist die Voreinstellung — so sieht ein Kino aus. Wer in den
    // Einstellungen „Hell" oder „Wie das Gerät" wählt, bekommt dieselben
    // Flächen in Hell, wie im Browser (ds-shared.css, `.lumen.light`). Auf dem
    // Fernseher bleibt es dunkel; dort ist alles über Bildern.

    public static let background = dynamisch(hell: (0.949, 0.949, 0.969, 1), dunkel: (0.020, 0.020, 0.027, 1))
    public static let surface    = dynamisch(hell: (0, 0, 0, 0.04), dunkel: (1, 1, 1, 0.06))
    public static let fill       = dynamisch(hell: (0.47, 0.47, 0.50, 0.10), dunkel: (1, 1, 1, 0.09))
    public static let hairline   = dynamisch(hell: (0.235, 0.235, 0.263, 0.16), dunkel: (1, 1, 1, 0.08))

    public static let label      = dynamisch(hell: (0, 0, 0, 0.88), dunkel: (1, 1, 1, 1))
    public static let label2     = dynamisch(hell: (0.235, 0.235, 0.263, 0.74), dunkel: (1, 1, 1, 0.62))
    public static let label3     = dynamisch(hell: (0.235, 0.235, 0.263, 0.40), dunkel: (1, 1, 1, 0.38))

    /// Der Grund hinter einem fehlenden Bild — oben heller, unten dunkler.
    public static let platzhalterOben  = dynamisch(hell: (0.86, 0.86, 0.88, 1), dunkel: (0.11, 0.11, 0.11, 1))
    public static let platzhalterUnten = dynamisch(hell: (0.78, 0.78, 0.81, 1), dunkel: (0.05, 0.05, 0.05, 1))

    /// Eine Farbe, die sich nach Hell und Dunkel richtet. Je ein Tupel aus
    /// Rot, Grün, Blau und Deckkraft zwischen 0 und 1.
    public static func dynamisch(hell: (Double, Double, Double, Double),
                                 dunkel: (Double, Double, Double, Double)) -> Color {
        #if os(macOS)
        return Color(nsColor: NSColor(name: nil) { erscheinung in
            let istDunkel = erscheinung.bestMatch(from: [.aqua, .darkAqua]) == .darkAqua
            let w = istDunkel ? dunkel : hell
            return NSColor(srgbRed: w.0, green: w.1, blue: w.2, alpha: w.3)
        })
        #else
        return Color(uiColor: UIColor { merkmale in
            let w = merkmale.userInterfaceStyle == .light ? hell : dunkel
            return UIColor(red: w.0, green: w.1, blue: w.2, alpha: w.3)
        })
        #endif
    }

    /// Der Akzent färbt genau eine Hauptaktion pro Ansicht — mehr davon,
    /// und nichts sticht mehr hervor.
    public static let accent     = Color(red: 0.039, green: 0.518, blue: 1.0)
    public static let good       = Color(red: 0.188, green: 0.820, blue: 0.345)
    public static let warn       = Color(red: 1.0,   green: 0.624, blue: 0.039)
    public static let bad        = Color(red: 1.0,   green: 0.412, blue: 0.380)
    public static let star       = Color(red: 1.0,   green: 0.839, blue: 0.039)
    /// Das gefüllte Herz der Favoriten — dasselbe Rosa wie im Browser.
    public static let herz       = Color(red: 1.0,   green: 0.216, blue: 0.373)

    // MARK: - Masse

    public static let posterWidth: CGFloat = 164
    public static let posterRatio: CGFloat = 2.0 / 3.0
    public static let wideWidth: CGFloat = 300
    public static let corner: CGFloat = 12
    public static let railSpacing: CGFloat = 18

    // MARK: - Profilbilder

    /// Die Farben der Profilkreise — dieselbe Reihe wie im Entwurf.
    public static func avatarColors(_ key: String) -> [Color] {
        switch key {
        case "av-b": [Color(red: 0.90, green: 0.49, blue: 0.33), Color(red: 0.78, green: 0.35, blue: 0.29)]
        case "av-c": [Color(red: 0.66, green: 0.52, blue: 0.91), Color(red: 0.51, green: 0.38, blue: 0.82)]
        case "av-d": [Color(red: 0.62, green: 0.83, blue: 0.85), Color(red: 0.25, green: 0.56, blue: 0.60)]
        case "av-k": [Color(red: 0.49, green: 0.78, blue: 0.53), Color(red: 0.33, green: 0.65, blue: 0.42)]
        default:     [Color(red: 0.42, green: 0.62, blue: 0.93), Color(red: 0.26, green: 0.47, blue: 0.86)]
        }
    }
}

// MARK: - Bausteine

/// Ein Kreis mit dem Anfangsbuchstaben — oder eine der eingebauten
/// Figuren, oder ein eigenes Bild (`bild`), wenn es eins gibt. Lädt das
/// Bild nicht, bleibt der Buchstabe stehen.
///
/// `colorKey` ist der Wert am Konto: „av-b", „fig-fuchs" oder „bild".
/// Wer ein eigenes Bild zeigen will, nimmt `ProfilAvatar` — der holt
/// die Adresse selbst.
public struct Avatar: View {
    let initial: String
    let colorKey: String
    var size: CGFloat = 40
    var bild: URL? = nil

    public init(initial: String, colorKey: String, size: CGFloat = 40, bild: URL? = nil) {
        self.initial = initial
        self.colorKey = colorKey
        self.size = size
        self.bild = bild
    }

    public var body: some View {
        ZStack {
            switch Profilbild(colorKey) {
            case .figur(let name):
                if let figur = Figuren.figur(name) {
                    FigurBild(figur: figur, size: size)
                } else {
                    buchstabe(Theme.avatarColors("av-a"))
                }
            case .bild:
                buchstabe([Color(white: 0.22), Color(white: 0.16)])
                if let bild {
                    MedienBild(url: bild)
                }
            case .farbe(let key):
                buchstabe(Theme.avatarColors(key))
            }
        }
        .frame(width: size, height: size)
        .clipShape(Circle())
    }

    private func buchstabe(_ farben: [Color]) -> some View {
        Circle()
            .fill(LinearGradient(colors: farben, startPoint: .topLeading, endPoint: .bottomTrailing))
            .overlay(
                Text(initial)
                    .font(.system(size: size * 0.42, weight: .medium))
                    .foregroundStyle(.white)
            )
    }
}

/// Ein Bild vom Server — Poster, Standbild, Profilbild, Gesicht aus der
/// Besetzung. Wie `AsyncImage`, nur mit einem zweiten Versuch: Schlägt das
/// Laden fehl, weil der Medienschlüssel in der Adresse nicht mehr gilt
/// (etwa nach einem Neustart des Servers), holt der Client einmal einen
/// neuen, und das Bild lädt mit ihm. Ein Bild, das es nicht gibt, bleibt
/// weg — dafür wird kein Schlüssel geholt (`APIClient.nachFehlschlag`).
///
/// Ohne Sitzung in der Umgebung (Vorschauen) bleibt es beim ersten Versuch.
public struct MedienBild: View {
    @Environment(Session.self) private var session: Session?
    let url: URL
    @State private var adresse: URL?
    @State private var versucht = false

    public init(url: URL) { self.url = url }

    public var body: some View {
        AsyncImage(url: adresse ?? url) { phase in
            switch phase {
            case .success(let image):
                image.resizable().scaledToFill()
            case .failure:
                // Unsichtbar und ohne Platz — nur, damit der zweite Versuch
                // einen Ort hat, an dem er beginnt.
                Color.clear.frame(width: 0, height: 0)
                    .task { await nochEinmal() }
            default:
                EmptyView()
            }
        }
        .onChange(of: url) {
            adresse = nil
            versucht = false
        }
    }

    @MainActor
    private func nochEinmal() async {
        guard !versucht, let session else { return }
        versucht = true
        if let neu = await session.client.nachFehlschlag(adresse ?? url) {
            adresse = neu
        }
    }
}

/// Das Bild eines Titels. Gibt es keins, steht der Titel selbst da — das
/// ist ehrlicher als ein graues Rechteck und hilft beim Wiederfinden.
public struct Artwork: View {
    let url: URL?
    let title: String
    var corner: CGFloat = Theme.corner

    public init(url: URL?, title: String, corner: CGFloat = Theme.corner) {
        self.url = url
        self.title = title
        self.corner = corner
    }

    public var body: some View {
        ZStack {
            placeholder
            if let url {
                MedienBild(url: url)
            }
        }
        .clipShape(RoundedRectangle(cornerRadius: corner, style: .continuous))
    }

    private var placeholder: some View {
        LinearGradient(colors: [Theme.platzhalterOben, Theme.platzhalterUnten],
                       startPoint: .topLeading, endPoint: .bottomTrailing)
            .overlay(
                Text(title)
                    .font(.footnote.weight(.semibold))
                    .foregroundStyle(Theme.label3)
                    .multilineTextAlignment(.center)
                    .lineLimit(4)
                    .padding(8)
            )
    }
}

/// Der Fortschrittsbalken unten auf einem Poster.
public struct ProgressStripe: View {
    let percent: Double
    public init(percent: Double) { self.percent = percent }

    public var body: some View {
        GeometryReader { geo in
            ZStack(alignment: .leading) {
                Rectangle().fill(Color.black.opacity(0.45))
                Rectangle().fill(Color.white)
                    .frame(width: geo.size.width * min(1, max(0, percent / 100)))
            }
        }
        .frame(height: 3)
    }
}

/// Der Haken auf einem gesehenen Titel.
public struct SeenBadge: View {
    public init() {}
    public var body: some View {
        Image(systemName: "checkmark")
            .font(.lumen(.caption2, tv: 10, weight: .bold))
            .foregroundStyle(.black)
            .frame(width: 20, height: 20)
            .background(Circle().fill(.white))
            .padding(7)
    }
}

/// Die eine Hauptaktion einer Ansicht: hell auf Dunkel, dunkel auf Hell.
public struct PrimaryButtonStyle: ButtonStyle {
    public init() {}
    public func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.lumen(.callout, tv: 16, weight: .semibold))
            .foregroundStyle(Theme.background)
            .padding(.horizontal, 22)
            .padding(.vertical, 12)
            .background(Capsule().fill(Theme.label))
            .opacity(configuration.isPressed ? 0.8 : 1)
    }
}

/// Alle übrigen Aktionen — zurückhaltend, damit die Hauptaktion steht.
public struct QuietButtonStyle: ButtonStyle {
    public init() {}
    public func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.lumen(.subheadline, tv: 15, weight: .medium))
            .foregroundStyle(Theme.label)
            .padding(.horizontal, 18)
            .padding(.vertical, 11)
            .background(Capsule().fill(Theme.fill))
            .opacity(configuration.isPressed ? 0.7 : 1)
    }
}

extension View {
    /// Der dunkle Verlauf über einem Bild, damit Text darauf lesbar bleibt.
    public func scrim(_ strength: Double = 0.85) -> some View {
        overlay(
            LinearGradient(
                stops: [
                    .init(color: .black.opacity(strength), location: 0),
                    .init(color: .black.opacity(strength * 0.45), location: 0.35),
                    .init(color: .clear, location: 1),
                ],
                startPoint: .bottom, endPoint: .top)
        )
    }
}

// MARK: - Schrift

extension Font {
    /// Ein Textstil, der mit der eingestellten Schriftgrösse mitwächst —
    /// auf dem Fernseher dagegen eine feste Grösse. Dort gibt es keine
    /// dynamische Schrift, und die Textstile sind für drei Meter Abstand
    /// gedacht: ein „caption2" wäre dort doppelt so gross wie gemeint.
    static func lumen(_ stil: Font.TextStyle, tv: CGFloat, weight: Font.Weight = .regular) -> Font {
        #if os(tvOS)
        return .system(size: tv, weight: weight)
        #else
        return .system(stil, weight: weight)
        #endif
    }
}
