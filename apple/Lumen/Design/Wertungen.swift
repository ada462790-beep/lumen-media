// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

import SwiftUI
import LumenKit

/// Die Wertungen unter dem Titel — wie im Browser (app.js: wertungsReihe):
/// IMDb als gelber Schriftzug mit Stern, Note und Stimmen, Rotten Tomatoes
/// mit Tomate (frisch, ab 60 %) oder Klecks (faul), TMDb in Prozent. Je eine
/// Kapsel, auf iPhone, iPad und Mac ein Link auf die Seite des Titels,
/// wenn der Server sie kennt. Vorher standen die Noten klein in der grauen
/// Eckdatenzeile, und „faul" hiess ein grüner Kreis.
///
/// Steht über dem Bild: immer hell auf dunkel.
struct WertungsReihe: View {
    let wertungen: [Wertung]
    var gross = false

    var body: some View {
        if !wertungen.isEmpty {
            FlussLayout(abstand: gross ? 14 : 8) {
                ForEach(wertungen) { w in kapsel(w) }
            }
            .accessibilityElement(children: .contain)
            .accessibilityLabel("Wertungen")
        }
    }

    @ViewBuilder
    private func kapsel(_ w: Wertung) -> some View {
        #if os(tvOS)
        // Auf dem Fernseher gibt es keinen Browser: keine Links, und die
        // Kapseln nehmen keinen Fokus.
        inhalt(w)
        #else
        if let link = w.link {
            Link(destination: link) { inhalt(w) }
                .buttonStyle(.plain)
                .help(w.barrierefrei)
        } else {
            inhalt(w)
        }
        #endif
    }

    private var schrift: Font { gross ? .system(size: 24, weight: .bold) : .lumen(.subheadline, tv: 15, weight: .bold) }
    private var klein: Font { gross ? .system(size: 18) : .lumen(.caption, tv: 12) }
    private var symbol: CGFloat { gross ? 26 : 16 }

    private func inhalt(_ w: Wertung) -> some View {
        HStack(spacing: gross ? 10 : 6) {
            switch w.quelle {
            case .imdb:
                Text(verbatim: "IMDb")
                    .font(gross ? .system(size: 18, weight: .heavy) : .lumen(.caption, tv: 12, weight: .heavy))
                    .foregroundStyle(.black)
                    .padding(.horizontal, gross ? 8 : 5)
                    .padding(.vertical, gross ? 3 : 2)
                    .background(RoundedRectangle(cornerRadius: gross ? 6 : 4, style: .continuous).fill(Self.imdbGelb))
                stern
                Text(w.wert).font(schrift).monospacedDigit()
                if let z = w.zusatz { Text(z).font(klein).foregroundStyle(.white.opacity(0.7)).monospacedDigit() }
            case .neutral:
                stern
                Text(w.wert).font(schrift).monospacedDigit()
            case .rottenTomatoes:
                if w.frisch == true {
                    Text(verbatim: "🍅").font(.system(size: symbol))
                } else {
                    RTKlecks().fill(Self.faulBraun).frame(width: symbol, height: symbol)
                }
                Text(w.wert).font(schrift).monospacedDigit()
                if let z = w.zusatz { Text(z).font(klein).foregroundStyle(.white.opacity(0.7)) }
            case .tmdb:
                Text(verbatim: "TMDb")
                    .font(gross ? .system(size: 18, weight: .heavy) : .lumen(.caption, tv: 12, weight: .heavy))
                    .foregroundStyle(LinearGradient(colors: [Self.tmdbGruen, Self.tmdbBlau],
                                                    startPoint: .leading, endPoint: .trailing))
                Text(w.wert).font(schrift).monospacedDigit()
            }
        }
        .foregroundStyle(.white)
        .lineLimit(1)
        .padding(.leading, gross ? 10 : 6)
        .padding(.trailing, gross ? 16 : 12)
        .frame(minHeight: gross ? 48 : 32)
        .background(Capsule().fill(Color.black.opacity(0.38)))
        .overlay(Capsule().strokeBorder(Color.white.opacity(0.14), lineWidth: 0.5))
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(Text(w.barrierefrei))
    }

    private var stern: some View {
        Image(systemName: "star.fill")
            .font(.system(size: gross ? 18 : 11, weight: .bold))
            .foregroundStyle(Self.imdbGelb)
    }

    static let imdbGelb = Color(red: 0.96, green: 0.77, blue: 0.09)
    static let faulBraun = Color(red: 0.77, green: 0.54, blue: 0.29)
    static let tmdbGruen = Color(red: 0.56, green: 0.81, blue: 0.63)
    static let tmdbBlau = Color(red: 0.0, green: 0.70, blue: 0.90)
}

/// Der Klecks für „faul" bei Rotten Tomatoes — dieselbe Form wie im
/// Browser (rt-faul, auf 24 × 24 gezeichnet).
struct RTKlecks: Shape {
    func path(in rect: CGRect) -> Path {
        let sx = rect.width / 24, sy = rect.height / 24
        func q(_ x: CGFloat, _ y: CGFloat) -> CGPoint { CGPoint(x: rect.minX + x * sx, y: rect.minY + y * sy) }
        var p = Path()
        p.move(to: q(12.0, 3.0))
        p.addCurve(to: q(16.6, 6.4), control1: q(13.2, 5.4), control2: q(15.6, 4.6))
        p.addCurve(to: q(17.4, 11.0), control1: q(17.6, 8.2), control2: q(16.0, 9.4))
        p.addCurve(to: q(21.0, 14.6), control1: q(18.8, 12.6), control2: q(21.0, 12.4))
        p.addCurve(to: q(17.4, 18.4), control1: q(21.0, 16.8), control2: q(18.4, 16.8))
        p.addCurve(to: q(15.2, 22.0), control1: q(16.4, 20.0), control2: q(17.6, 22.0))
        p.addCurve(to: q(12.0, 20.0), control1: q(12.8, 22.0), control2: q(13.2, 20.0))
        p.addCurve(to: q(8.6, 21.6), control1: q(10.8, 20.0), control2: q(10.8, 22.0))
        p.addCurve(to: q(6.2, 18.0), control1: q(6.4, 21.2), control2: q(7.6, 19.0))
        p.addCurve(to: q(2.0, 15.2), control1: q(4.8, 17.0), control2: q(2.0, 17.4))
        p.addCurve(to: q(5.0, 11.4), control1: q(2.0, 13.0), control2: q(4.6, 13.2))
        p.addCurve(to: q(4.2, 6.6), control1: q(5.4, 9.6), control2: q(2.8, 8.2))
        p.addCurve(to: q(9.0, 5.8), control1: q(5.6, 5.0), control2: q(7.6, 6.8))
        p.addCurve(to: q(12.0, 3.0), control1: q(10.4, 4.8), control2: q(10.8, 3.0))
        p.closeSubpath()
        return p
    }
}
