// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

import SwiftUI
import LumenKit

// MARK: - Alle Filmreihen

/// Die Filmreihen als Raster: je ein Poster, Name und Anzahl.
///
/// Welche Filme zu einer Reihe gehören, weiss der Server. Er schickt nur
/// Reihen, aus denen dieses Profil mindestens zwei Filme sehen darf —
/// darum muss hier nichts mehr gefiltert werden.
struct CollectionsView: View {
    @Environment(Session.self) private var session
    @State private var reihen: [Reihe] = []
    /// Die eigenen Sammlungen — oben umschaltbar, wie im Browser. Ein
    /// älterer Server kennt sie nicht; dann bleibt es bei den Reihen.
    @State private var sammlungen: [Sammlung] = []
    @State private var zeigtSammlungen = false
    @State private var loading = true
    @State private var error: String?

    private var columns: [GridItem] {
        [GridItem(.adaptive(minimum: Theme.posterWidth, maximum: 220), spacing: 18)]
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 18) {
                if !sammlungen.isEmpty {
                    Picker("Filmreihen oder Sammlungen", selection: $zeigtSammlungen) {
                        Text("Filmreihen").tag(false)
                        Text("Sammlungen").tag(true)
                    }
                    .pickerStyle(.segmented)
                    .labelsHidden()
                    .frame(maxWidth: 360)
                }
                if zeigtSammlungen {
                    SammlungenRaster(sammlungen: sammlungen)
                } else {
                    LazyVGrid(columns: columns, spacing: 24) {
                        ForEach(reihen) { reihe in
                            NavigationLink {
                                CollectionView(reiheId: reihe.id, name: reihe.name)
                            } label: {
                                CollectionCard(reihe: reihe)
                            }
                            .buttonStyle(.plain)
                        }
                    }
                }
            }
            .padding(20)
        }
        .overlay {
            if !loading && error == nil && reihen.isEmpty && sammlungen.isEmpty {
                ContentUnavailableView("Noch keine Filmreihen",
                    systemImage: "square.stack",
                    description: Text("Sobald zwei Filme einer Reihe in der Mediathek liegen, erscheinen sie hier."))
            } else {
                Loading(loading: loading, error: error, retry: load)
            }
        }
        .navigationTitle(zeigtSammlungen ? Text("Sammlungen") : Text("Filmreihen"))
        .task { await load() }
        .refreshable { await load() }
    }

    private func load() async {
        error = nil
        do { reihen = try await session.client.collections() }
        catch { self.error = Session.text(error) }
        // Beiwerk: fehlen die Sammlungen (älterer Server), bleibt es still.
        sammlungen = (try? await session.client.sammlungen()) ?? []
        if sammlungen.isEmpty { zeigtSammlungen = false }
        else if reihen.isEmpty { zeigtSammlungen = true }
        loading = false
    }
}

/// Eine Reihe im Raster. Als Bild dient das Poster der Reihe selbst, sonst
/// das des ersten Films — das ist meist das, woran man sie wiedererkennt.
struct CollectionCard: View {
    @Environment(Session.self) private var session
    let reihe: Reihe
    @State private var url: URL?

    var body: some View {
        VStack(alignment: .leading, spacing: 9) {
            Artwork(url: url, title: reihe.name)
                .frame(width: Theme.posterWidth, height: Theme.posterWidth / Theme.posterRatio)
            Text(reihe.name).font(.subheadline).lineLimit(1)
            Text(reihe.anzahlText).font(.caption).foregroundStyle(Theme.label2).lineLimit(1)
        }
        .frame(width: Theme.posterWidth, alignment: .leading)
        .task(id: reihe.id) {
            url = await session.client.reiheBildURL(reihe)
        }
    }
}

// MARK: - Eine Filmreihe

/// Die Filme einer Reihe, in ihrer Reihenfolge. Ein Tipp öffnet die
/// gewohnte Titelseite.
struct CollectionView: View {
    @Environment(Session.self) private var session
    @Environment(\.dismiss) private var dismiss
    @Environment(\.abspielen) private var abspielen
    let reiheId: String
    /// Der Name aus der Übersicht, damit der Titel nicht erst nach dem
    /// Laden erscheint.
    var name: String = ""

    @State private var reihe: Reihe?
    @State private var loading = true
    @State private var error: String?
    @State private var confirmDissolve = false

    private var columns: [GridItem] {
        [GridItem(.adaptive(minimum: Theme.posterWidth, maximum: 220), spacing: 18)]
    }

    var body: some View {
        ScrollView {
            if let reihe {
                VStack(alignment: .leading, spacing: 18) {
                    Text(reihe.unterzeile).font(.subheadline).foregroundStyle(Theme.label2)
                    // Wie im Browser: die Reihe von vorn, oder weiter mit
                    // dem ersten Film, der noch nicht gesehen ist.
                    if let film = reihe.naechsterFilm, let abspielen {
                        Button {
                            abspielen(Abspielstart(item: film))
                        } label: {
                            Label(reihe.abspielText, systemImage: "play.fill")
                        }
                        .buttonStyle(PrimaryButtonStyle())
                    }
                    if let overview = reihe.overview, !overview.isEmpty {
                        Text(overview).font(.body).lineSpacing(3)
                            .frame(maxWidth: 680, alignment: .leading)
                    }
                    PosterRaster(items: reihe.items)
                }
                .padding(20)
            }
        }
        .overlay { Loading(loading: loading, error: error, retry: load) }
        .navigationTitle(reihe?.name ?? name)
        .toolbar {
            // Auflösen dürfen Besitzer und Mitverwalter — sie pflegen die
            // Mediathek. Das gehört zur Verwaltung, also nur am Mac; für
            // alle anderen bleibt der Platz leer.
            ToolbarItem(placement: .primaryAction) {
                if session.verwaltungHier {
                    Menu {
                        Button("Reihe auflösen", role: .destructive) { confirmDissolve = true }
                    } label: {
                        Label("Mehr", systemImage: "ellipsis.circle")
                    }
                }
            }
        }
        .confirmationDialog("Reihe auflösen?", isPresented: $confirmDissolve) {
            Button("Auflösen", role: .destructive) {
                Task { await dissolve() }
            }
        } message: {
            Text("Die Filme bleiben in der Mediathek. Es verschwindet nur die Gruppierung.")
        }
        .task { await load() }
        .refreshable { await load() }
    }

    private func load() async {
        error = nil
        do { reihe = try await session.client.collection(reiheId) }
        catch { self.error = Session.text(error) }
        loading = false
    }

    private func dissolve() async {
        do {
            // Über `verwaltet`: ist die Verwaltung gesperrt, kommt erst
            // der Entsperr-Dialog, dann läuft es weiter.
            try await session.verwaltet { try await session.client.adminDissolveCollection(reiheId) }
            dismiss()
        } catch {
            self.error = Session.text(error)
        }
    }
}
