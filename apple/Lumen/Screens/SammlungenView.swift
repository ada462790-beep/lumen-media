// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

import SwiftUI
import LumenKit

// Eigene Sammlungen: von der Verwaltung zusammengestellt („Weihnachtsfilme",
// „Marvel in Reihenfolge"), für alle sichtbar — jedes Profil sieht darin
// nur, was es ohnehin sehen darf. Sie stehen neben den Filmreihen, wie im
// Browser (sammlungen.js): für die Leute im Haushalt ist beides dasselbe,
// Titel, die zusammengehören. Zusammenstellen geht nur im Browser.

// MARK: - Karte

/// Eine Sammlung im Raster: ihr eigenes Bild, sonst das Poster des ersten
/// Titels — darunter Name und Anzahl. So gross wie eine Filmreihe.
struct SammlungCard: View {
    @Environment(Session.self) private var session
    let sammlung: Sammlung
    @State private var url: URL?

    var body: some View {
        VStack(alignment: .leading, spacing: 9) {
            Artwork(url: url, title: sammlung.name)
                .frame(width: Theme.posterWidth, height: Theme.posterWidth / Theme.posterRatio)
            Text(sammlung.name).font(.subheadline).lineLimit(1)
            Text(sammlung.anzahlText).font(.caption).foregroundStyle(Theme.label2).lineLimit(1)
        }
        .frame(width: Theme.posterWidth, alignment: .leading)
        .task(id: sammlung.id) {
            url = await session.client.sammlungBildURL(sammlung)
        }
    }
}

// MARK: - Alle Sammlungen

/// Die Sammlungen als Raster — der zweite Teil von „Filmreihen".
struct SammlungenRaster: View {
    let sammlungen: [Sammlung]

    private var columns: [GridItem] {
        [GridItem(.adaptive(minimum: Theme.posterWidth, maximum: 220), spacing: 18)]
    }

    var body: some View {
        LazyVGrid(columns: columns, spacing: 24) {
            ForEach(sammlungen) { sammlung in
                NavigationLink {
                    SammlungView(sammlungId: sammlung.id, name: sammlung.name)
                } label: {
                    SammlungCard(sammlung: sammlung)
                }
                .buttonStyle(.plain)
            }
        }
    }
}

// MARK: - Eine Sammlung

/// Die Titel einer Sammlung, in der Reihenfolge, die die Verwaltung
/// gewählt hat. Ein Tipp öffnet die gewohnte Titelseite.
struct SammlungView: View {
    @Environment(Session.self) private var session
    let sammlungId: String
    /// Der Name aus der Übersicht, damit der Titel nicht erst nach dem
    /// Laden erscheint.
    var name: String = ""

    @State private var sammlung: Sammlung?
    @State private var loading = true
    @State private var error: String?

    var body: some View {
        ScrollView {
            if let sammlung {
                VStack(alignment: .leading, spacing: 18) {
                    Text(sammlung.anzahlText).font(.subheadline).foregroundStyle(Theme.label2)
                    if let text = sammlung.beschreibungText {
                        Text(text).font(.body).lineSpacing(3)
                            .frame(maxWidth: 680, alignment: .leading)
                    }
                    PosterRaster(items: sammlung.items)
                }
                .padding(20)
            }
        }
        .overlay { Loading(loading: loading, error: error, retry: load) }
        .navigationTitle(sammlung?.name ?? name)
        .task { await load() }
        .refreshable { await load() }
    }

    private func load() async {
        error = nil
        do { sammlung = try await session.client.sammlung(sammlungId) }
        catch { if !APIClient.istAbbruch(error) { self.error = Session.text(error) } }
        loading = false
    }
}

// MARK: - In der Suche

/// „Filmreihen und Sammlungen" oben in der Suche: was zum Namen passt,
/// als Reihe von Karten. „Harry Potter" findet so die Reihe, nicht nur die
/// einzelnen Filme — wie im Browser.
struct ReihenUndSammlungenReihe: View {
    let reihen: [Reihe]
    let sammlungen: [Sammlung]

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text("Filmreihen und Sammlungen").font(.title3.weight(.semibold)).padding(.horizontal, 20)
            ScrollView(.horizontal, showsIndicators: false) {
                HStack(alignment: .top, spacing: 18) {
                    ForEach(reihen) { reihe in
                        NavigationLink {
                            CollectionView(reiheId: reihe.id, name: reihe.name)
                        } label: {
                            CollectionCard(reihe: reihe)
                        }
                        .buttonStyle(.plain)
                    }
                    ForEach(sammlungen) { sammlung in
                        NavigationLink {
                            SammlungView(sammlungId: sammlung.id, name: sammlung.name)
                        } label: {
                            SammlungCard(sammlung: sammlung)
                        }
                        .buttonStyle(.plain)
                    }
                }
                .padding(.horizontal, 20)
            }
        }
    }
}
