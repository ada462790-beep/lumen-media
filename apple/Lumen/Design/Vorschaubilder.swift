// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

import SwiftUI
import ImageIO
import LumenKit

// Vorschaubilder beim Spulen (Trickplay).
//
// Der Server legt je Datei ein paar Blätter an, auf jedem ein Raster aus
// kleinen Bildern — eins alle zehn Sekunden. Wer spult, sieht so, wo er
// landet, bevor er loslässt. Welches Bild an welcher Stelle liegt, rechnet
// `Trickplay.kachel` in LumenKit aus; hier werden die Blätter geladen und
// die Kachel ausgeschnitten.
//
// Die Steuerung von AVKit lässt sich kein eigenes Bild unterschieben. Darum
// legt Lumen eine eigene, schmale Leiste über das Bild, wenn es Vorschau-
// bilder gibt — `VorschauLeiste`. Auf iPhone, iPad und Mac; auf dem
// Fernseher siehe TVPlayer.

/// Lädt die Blätter einer Datei und schneidet Kacheln daraus.
@MainActor
@Observable
final class Vorschaubilder {
    let info: Trickplay
    private let client: APIClient
    private let fileId: String
    private(set) var blaetter: [Int: CGImage] = [:]
    @ObservationIgnored private var laufend: Set<Int> = []

    init(client: APIClient, fileId: String, info: Trickplay) {
        self.client = client
        self.fileId = fileId
        self.info = info
    }

    /// Das Bild für diese Stelle. Ist sein Blatt noch nicht da, wird es
    /// geholt, und bis dahin gibt es keins.
    func bild(bei sekunden: Double) -> CGImage? {
        guard let k = info.kachel(bei: sekunden) else { return nil }
        guard let blatt = blaetter[k.blatt] else {
            laden(k.blatt)
            return nil
        }
        return blatt.cropping(to: CGRect(x: k.x, y: k.y, width: k.breite, height: k.hoehe))
    }

    /// Die Blätter um diese Stelle schon einmal holen — beim Öffnen der
    /// Leiste, damit das erste Bild nicht auf sich warten lässt.
    func vorladen(bei sekunden: Double) {
        guard let k = info.kachel(bei: sekunden) else { return }
        laden(k.blatt)
    }

    private func laden(_ nr: Int) {
        guard blaetter[nr] == nil, !laufend.contains(nr) else { return }
        laufend.insert(nr)
        Task { [weak self, client, fileId] in
            let daten = try? await client.trickplayBlatt(fileId: fileId, nr: nr)
            guard let self else { return }
            self.laufend.remove(nr)
            if let daten, let bild = Self.bild(aus: daten) { self.blaetter[nr] = bild }
        }
    }

    nonisolated static func bild(aus daten: Data) -> CGImage? {
        guard let quelle = CGImageSourceCreateWithData(daten as CFData, nil) else { return nil }
        return CGImageSourceCreateImageAtIndex(quelle, 0, nil)
    }
}

#if !os(tvOS)
/// Eine schmale Zeitleiste mit Vorschaubild über dem Finger (oder Zeiger).
/// Loslassen springt an die Stelle. Nicht auf dem Fernseher: dort gibt es
/// kein Ziehen, und die Leiste des Systems ist die gewohnte.
struct VorschauLeiste: View {
    let playback: Playback
    let vorschau: Vorschaubilder
    /// Wo gerade hingezogen wird, 0 bis 1 — nil, solange niemand zieht.
    @State private var ziel: Double?

    var body: some View {
        TimelineView(.periodic(from: .now, by: 0.5)) { _ in
            GeometryReader { geo in
                let breite = max(1, geo.size.width)
                let anteil = ziel ?? jetzt
                ZStack(alignment: .bottomLeading) {
                    Capsule().fill(Color.white.opacity(0.25)).frame(height: 5)
                    Capsule().fill(Color.white).frame(width: breite * anteil, height: 5)
                    if let ziel {
                        vorschauBild(bei: ziel * playback.duration)
                            .position(x: min(max(80, breite * ziel), breite - 80), y: -62)
                    }
                }
                .frame(height: 28, alignment: .bottom)
                .contentShape(Rectangle())
                .gesture(
                    DragGesture(minimumDistance: 0)
                        .onChanged { wert in
                            if ziel == nil { vorschau.vorladen(bei: jetzt * playback.duration) }
                            ziel = min(1, max(0, wert.location.x / breite))
                        }
                        .onEnded { _ in
                            if let ziel, playback.duration > 0 { playback.seek(to: ziel * playback.duration) }
                            ziel = nil
                        }
                )
            }
            .frame(height: 28)
        }
        .accessibilityElement()
        .accessibilityLabel("Spulen")
        .accessibilityValue(Text(verbatim: "\(Playback.clock(playback.currentTime)) / \(Playback.clock(playback.duration))"))
        .accessibilityAdjustableAction { richtung in
            switch richtung {
            case .increment: playback.skip(10)
            case .decrement: playback.skip(-10)
            @unknown default: break
            }
        }
    }

    private var jetzt: Double {
        guard playback.duration > 0 else { return 0 }
        return min(1, max(0, playback.currentTime / playback.duration))
    }

    private func vorschauBild(bei sekunden: Double) -> some View {
        VStack(spacing: 4) {
            Group {
                if let bild = vorschau.bild(bei: sekunden) {
                    Image(decorative: bild, scale: 1)
                        .resizable()
                        .aspectRatio(contentMode: .fit)
                } else {
                    Color.black.opacity(0.6)
                }
            }
            .frame(width: 160, height: 90)
            .clipShape(RoundedRectangle(cornerRadius: 6, style: .continuous))
            .overlay(RoundedRectangle(cornerRadius: 6, style: .continuous).strokeBorder(Color.white.opacity(0.6)))
            Text(verbatim: Playback.clock(sekunden))
                .font(.caption.monospacedDigit().weight(.semibold))
                .foregroundStyle(.white)
                .shadow(color: .black, radius: 2)
        }
        .allowsHitTesting(false)
    }
}
#endif
