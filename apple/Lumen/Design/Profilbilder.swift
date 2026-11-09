// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

import SwiftUI
import LumenKit

// Profilbilder auf allen Geräten: Farbe mit Anfangsbuchstabe, eine der
// zwölf eingebauten Figuren oder ein eigenes Bild. Die Zeichnungen der
// Figuren stehen in LumenKit (`Figuren`) als dieselben Pfade wie im
// Browser; hier werden sie nur gezeichnet. Liegt in Design/, weil
// project.yml diesen Ordner auch ins Ziel für den Fernseher einbindet.

/// Ein Profilbild, das sich die Adresse eines eigenen Bilds selbst holt.
/// Ohne `accountId` (etwa bei geteilten Bewertungen, die nur Name und
/// Bild-Art kennen) bleibt es beim Buchstaben.
struct ProfilAvatar: View {
    @Environment(Session.self) private var session
    let name: String
    let avatar: String
    var accountId: String? = nil
    var size: CGFloat = 40
    @State private var bild: URL?

    var body: some View {
        Avatar(initial: String(name.prefix(1)).uppercased(), colorKey: avatar, size: size, bild: bild)
            .task(id: "\(accountId ?? "")|\(avatar)|\(session.avatarStand)") {
                guard avatar == "bild", let accountId else { bild = nil; return }
                bild = await session.avatarURL(accountId: accountId)
            }
    }
}

/// Eine Figur: farbiger Kreis, weisse Strichzeichnung darauf.
struct FigurBild: View {
    let figur: Figuren.Figur
    var size: CGFloat = 40

    var body: some View {
        let f = figur.farbe
        // Die Zeichnung füllt wie im Browser 62 % des Kreises.
        let kante = size * 0.62
        let strich = 1.6 * kante / 24
        ZStack {
            Circle().fill(Color(red: f.r, green: f.g, blue: f.b))
            ZStack {
                FigurPfad(schritte: figur.linien)
                    .stroke(.white, style: StrokeStyle(lineWidth: strich, lineCap: .round, lineJoin: .round))
                FigurPfad(schritte: figur.flaechen)
                    .fill(.white)
            }
            .frame(width: kante, height: kante)
        }
        .frame(width: size, height: size)
        .accessibilityLabel(figur.name.capitalized)
    }
}

/// Die Linienzüge einer Figur, vom Raster 24 × 24 auf den Rahmen gerechnet.
struct FigurPfad: Shape {
    let schritte: [[Figuren.PfadSchritt]]

    func path(in rect: CGRect) -> Path {
        let s = min(rect.width, rect.height) / 24
        let ox = rect.minX + (rect.width - 24 * s) / 2
        let oy = rect.minY + (rect.height - 24 * s) / 2
        func p(_ x: Double, _ y: Double) -> CGPoint { CGPoint(x: ox + x * s, y: oy + y * s) }
        var path = Path()
        for zug in schritte {
            for schritt in zug {
                switch schritt {
                case .move(let x, let y): path.move(to: p(x, y))
                case .line(let x, let y): path.addLine(to: p(x, y))
                case .curve(let ax, let ay, let bx, let by, let x, let y):
                    path.addCurve(to: p(x, y), control1: p(ax, ay), control2: p(bx, by))
                case .close: path.closeSubpath()
                }
            }
        }
        return path
    }
}
