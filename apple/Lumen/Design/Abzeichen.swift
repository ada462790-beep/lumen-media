// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

import SwiftUI
import AVFoundation
import LumenKit
#if canImport(UIKit)
import UIKit
#elseif canImport(AppKit)
import AppKit
#endif

// Was auf Karten und Titelseiten liegt, auf allen Geräten gleich: die
// Abzeichen („Neu", „3 neue Folgen", 4K, HDR), die Technik-Zeile und die
// bewegte Vorschau. Liegt in Design/, weil project.yml diesen Ordner
// auch ins Ziel für den Fernseher einbindet.

// MARK: - Abzeichen auf der Karte

/// Links oben „Neu" oder „3 neue Folgen" in Rot, rechts oben 4K und HDR
/// dunkel hinterlegt — nur, was die Daten hergeben, sonst nichts. Wie im
/// Browser (app.js: abzeichen).
struct KartenAbzeichen: View {
    let item: Item
    /// Auf dem Fernseher grösser, man sitzt drei Meter weg.
    var gross = false

    var body: some View {
        let links = item.abzeichenNeu
        let rechts = item.abzeichenTechnik
        if links != nil || !rechts.isEmpty {
            HStack(alignment: .top, spacing: 4) {
                if let links {
                    marke(links, hintergrund: Color(red: 1.0, green: 0.27, blue: 0.23))
                }
                Spacer(minLength: 4)
                if !rechts.isEmpty {
                    VStack(alignment: .trailing, spacing: 4) {
                        ForEach(rechts, id: \.self) { marke($0, hintergrund: .black.opacity(0.6)) }
                    }
                }
            }
            .padding(gross ? 10 : 7)
            .allowsHitTesting(false)
            .accessibilityHidden(true)
        }
    }

    private func marke(_ text: String, hintergrund: Color) -> some View {
        Text(text)
            .font(gross ? .lumen(.headline, tv: 17, weight: .bold) : .lumen(.caption2, tv: 11, weight: .bold))
            .tracking(0.3)
            .lineLimit(1)
            .foregroundStyle(.white)
            .padding(.horizontal, gross ? 10 : 7)
            .frame(height: gross ? 30 : 20)
            .background(RoundedRectangle(cornerRadius: gross ? 8 : 6, style: .continuous).fill(hintergrund))
    }
}

// MARK: - Technik-Zeile

/// Kleine umrandete Abzeichen wie im Player des Apple TV: 4K, Dolby
/// Vision, 5.1, DE · EN, Untertitel, und ob die Datei direkt läuft.
struct TechnikZeile: View {
    let abzeichen: [Technik.Abzeichen]
    var gross = false

    var body: some View {
        if !abzeichen.isEmpty {
            FlussLayout(abstand: gross ? 12 : 8) {
                ForEach(abzeichen) { a in marke(a) }
            }
            .accessibilityElement(children: .combine)
            .accessibilityLabel("Technische Daten: \(abzeichen.map(\.text).joined(separator: ", "))")
        }
    }

    @ViewBuilder
    private func marke(_ a: Technik.Abzeichen) -> some View {
        let form = RoundedRectangle(cornerRadius: gross ? 8 : 6, style: .continuous)
        HStack(spacing: 5) {
            if a.art == .untertitel {
                Image(systemName: "captions.bubble").font(gross ? .lumen(.title3, tv: 18, weight: .semibold) : .lumen(.caption2, tv: 11, weight: .semibold))
            }
            Text(a.text)
                .font(gross ? .lumen(.title3, tv: 20, weight: .semibold) : .lumen(.caption, tv: 12, weight: .semibold))
                .tracking(0.3)
                .lineLimit(1)
        }
        .foregroundStyle(vorne(a.art))
        .padding(.horizontal, gross ? 12 : 9)
        .frame(height: gross ? 36 : 24)
        .background(form.fill(hinten(a.art)))
        .overlay(form.strokeBorder(a.art == .normal || a.art == .untertitel ? Color.white.opacity(0.35) : .clear,
                                   lineWidth: 1))
    }

    private func vorne(_ art: Technik.Abzeichen.Art) -> Color {
        switch art {
        case .gut: Theme.good
        case .blau: Color(red: 0.29, green: 0.64, blue: 1.0)
        // Die Zeile steht immer über einem Bild — weiss, auch wenn die
        // App hell eingestellt ist.
        default: .white
        }
    }

    private func hinten(_ art: Technik.Abzeichen.Art) -> Color {
        switch art {
        case .gut: Theme.good.opacity(0.16)
        case .blau: Theme.accent.opacity(0.18)
        default: .clear
        }
    }
}

/// Legt Kinder nebeneinander und bricht um, wenn die Zeile voll ist.
struct FlussLayout: Layout {
    var abstand: CGFloat = 8

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        let breite = proposal.width ?? .infinity
        var x: CGFloat = 0, y: CGFloat = 0, zeile: CGFloat = 0, weiteste: CGFloat = 0
        for v in subviews {
            let g = v.sizeThatFits(.unspecified)
            if x > 0 && x + g.width > breite {
                y += zeile + abstand; x = 0; zeile = 0
            }
            x += g.width + abstand
            zeile = max(zeile, g.height)
            weiteste = max(weiteste, x - abstand)
        }
        return CGSize(width: weiteste, height: y + zeile)
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        var x = bounds.minX, y = bounds.minY, zeile: CGFloat = 0
        for v in subviews {
            let g = v.sizeThatFits(.unspecified)
            if x > bounds.minX && x + g.width > bounds.maxX {
                y += zeile + abstand; x = bounds.minX; zeile = 0
            }
            v.place(at: CGPoint(x: x, y: y), proposal: ProposedViewSize(g))
            x += g.width + abstand
            zeile = max(zeile, g.height)
        }
    }
}

// MARK: - Bewegte Vorschau

/// Zwölf stumme Sekunden aus dem Film, in Schleife, im Kartenbild — auf
/// dem Mac beim Drüberfahren mit dem Zeiger, auf dem Fernseher nach einer
/// Sekunde Fokus. Auf iPhone und iPad gibt es sie nicht: dort wäre es
/// Akku und Datenvolumen für nichts.
///
/// Bis das erste Bild da ist, bleibt die Ebene durchsichtig — gibt es
/// keine Vorschau (404), sieht man einfach weiter das Poster.
struct StummeVorschau: View {
    let url: URL

    var body: some View {
        VorschauEbene(url: url)
            .allowsHitTesting(false)
            .accessibilityHidden(true)
    }
}

#if os(macOS)
private struct VorschauEbene: NSViewRepresentable {
    let url: URL
    func makeNSView(context: Context) -> VorschauView { VorschauView(url: url) }
    func updateNSView(_ view: VorschauView, context: Context) { view.zeige(url) }
    static func dismantleNSView(_ view: VorschauView, coordinator: ()) { view.anhalten() }
}
#else
private struct VorschauEbene: UIViewRepresentable {
    let url: URL
    func makeUIView(context: Context) -> VorschauView { VorschauView(url: url) }
    func updateUIView(_ view: VorschauView, context: Context) { view.zeige(url) }
    static func dismantleUIView(_ view: VorschauView, coordinator: ()) { view.anhalten() }
}
#endif

#if os(macOS)
private typealias PlattformView = NSView
#else
private typealias PlattformView = UIView
#endif

/// Eine Ebene mit AVQueuePlayer und AVPlayerLooper: stumm, ohne
/// Bedienelemente, füllt den Rahmen und schneidet über.
private final class VorschauView: PlattformView {
    private let ebene = AVPlayerLayer()
    private var player: AVQueuePlayer?
    private var looper: AVPlayerLooper?
    private var beobachter: NSKeyValueObservation?
    private var aktuell: URL?

    init(url: URL) {
        super.init(frame: .zero)
        #if os(macOS)
        wantsLayer = true
        layer?.addSublayer(ebene)
        #else
        layer.addSublayer(ebene)
        isUserInteractionEnabled = false
        #endif
        ebene.videoGravity = .resizeAspectFill
        ebene.opacity = 0
        zeige(url)
    }

    required init?(coder: NSCoder) { nil }

    func zeige(_ url: URL) {
        guard url != aktuell else { return }
        anhalten()
        aktuell = url
        let vorlage = AVPlayerItem(url: url)
        let p = AVQueuePlayer()
        p.isMuted = true
        p.preventsDisplaySleepDuringVideoPlayback = false
        looper = AVPlayerLooper(player: p, templateItem: vorlage)
        player = p
        ebene.player = p
        // Erst einblenden, wenn ein Bild da ist — sonst stünde kurz ein
        // schwarzes Rechteck über dem Poster.
        beobachter = ebene.observe(\.isReadyForDisplay, options: [.new]) { ebene, _ in
            let bereit = ebene.isReadyForDisplay
            DispatchQueue.main.async {
                CATransaction.begin()
                CATransaction.setAnimationDuration(0.35)
                ebene.opacity = bereit ? 1 : 0
                CATransaction.commit()
            }
        }
        p.play()
    }

    func anhalten() {
        beobachter?.invalidate()
        beobachter = nil
        player?.pause()
        looper?.disableLooping()
        looper = nil
        player = nil
        ebene.player = nil
        ebene.opacity = 0
        aktuell = nil
    }

    #if os(macOS)
    override func layout() {
        super.layout()
        ebene.frame = bounds
    }
    #else
    override func layoutSubviews() {
        super.layoutSubviews()
        ebene.frame = bounds
    }
    #endif
}
