// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

import SwiftUI
import LumenKit

/// Neuigkeiten: neue Folgen der Serien, die ich schaue oder gemerkt habe,
/// neue Filme in Reihen, aus denen ich etwas gesehen habe, und was in der
/// Woche dazukam. Alles aus dem eigenen Stand — die Liste sieht niemand
/// sonst, und sie verrät nichts über den Rest des Haushalts.
///
/// Öffnen heisst gelesen, wie im Browser: erst wird die Liste geholt
/// (damit das Neue noch als neu markiert ist), dann gilt alles bis jetzt
/// als gesehen, und der Punkt in der Navigation verschwindet.
struct NeuigkeitenView: View {
    @Environment(Session.self) private var session
    @State private var daten: Neuigkeiten?
    @State private var loading = true
    @State private var error: String?

    var body: some View {
        List {
            Section {
                ForEach(Array((daten?.eintraege ?? []).enumerated()), id: \.offset) { _, n in
                    zeile(n)
                }
            } footer: {
                Text("Nur für dich — aus dem, was du schaust und gemerkt hast.")
            }
        }
        .overlay {
            if let error {
                ContentUnavailableView {
                    Label("Das hat nicht geklappt", systemImage: "exclamationmark.triangle")
                } description: { Text(error) }
            } else if loading {
                ProgressView().controlSize(.large)
            } else if (daten?.eintraege ?? []).isEmpty {
                ContentUnavailableView("Nichts Neues", systemImage: "bell",
                    description: Text("Lumen meldet sich, sobald eine Serie, die du schaust, neue Folgen bekommt."))
            }
        }
        .navigationTitle("Neuigkeiten")
        .task { await load() }
        .refreshable { await load() }
    }

    @ViewBuilder
    private func zeile(_ n: Neuigkeit) -> some View {
        let neu = n.istNeu(gelesenBis: daten?.gelesenBis)
        let inhalt = HStack(spacing: 14) {
            Image(systemName: n.symbol)
                .font(.callout.weight(.medium))
                .foregroundStyle(neu ? Theme.accent : Theme.label2)
                .frame(width: 38, height: 38)
                .background(RoundedRectangle(cornerRadius: 12, style: .continuous)
                    .fill(neu ? Theme.accent.opacity(0.18) : Theme.fill))
            VStack(alignment: .leading, spacing: 2) {
                Text(n.titel)
                    .font(.body.weight(neu ? .semibold : .regular))
                    .lineLimit(1)
                Text("\(n.text) · \(n.at.formatted(.dateTime.day().month(.abbreviated).locale(.lumen)))")
                    .font(.caption).foregroundStyle(Theme.label2).lineLimit(1)
            }
            Spacer(minLength: 8)
            if neu {
                Circle().fill(Theme.bad).frame(width: 9, height: 9)
                    .accessibilityLabel("neu")
            }
        }
        .padding(.vertical, 4)

        if let itemId = n.itemId, !itemId.isEmpty {
            NavigationLink { DetailView(itemId: itemId) } label: { inhalt }
        } else {
            inhalt
        }
    }

    private func load() async {
        error = nil
        do {
            let neu = try await session.client.news()
            daten = neu
            if neu.ungelesen > 0 { await session.neuigkeitenGelesen() }
        } catch {
            self.error = Session.text(error)
        }
        loading = false
    }
}
