// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

import SwiftUI
import LumenKit

/// „Ich" auf dem iPhone: alles, was dem eigenen Profil gehört und nicht in
/// die fünf Reiter passt — Merkliste und Gesehen, Favoriten, Listen,
/// Filmreihen, Neuigkeiten, Wünsche, Downloads und die Einstellungen.
///
/// Eine Liste mit Links im selben Navigationsstapel wie der Reiter. Das ist
/// der Unterschied zu „Mehr", das iOS bei mehr als fünf Reitern selbst
/// anlegt: dort steckte jeder Reiter in einem zweiten Stapel, und die
/// Titelleisten doppelten sich.
struct IchView: View {
    @Environment(Session.self) private var session

    private var ungelesen: Int { session.me?.ungelesen ?? 0 }

    var body: some View {
        List {
            if let account = session.me?.account {
                Section {
                    NavigationLink { SettingsView() } label: {
                        HStack(spacing: 14) {
                            ProfilAvatar(name: account.name, avatar: account.avatar, accountId: account.id, size: 48)
                            VStack(alignment: .leading, spacing: 2) {
                                Text(account.name).font(.title3.weight(.semibold))
                                Text("Einstellungen, Geräte, Profilbild")
                                    .font(.caption).foregroundStyle(Theme.label2)
                            }
                        }
                        .padding(.vertical, 4)
                    }
                }
            }

            Section {
                NavigationLink { WatchlistView() } label: {
                    Label("Merkliste und Gesehen", systemImage: "bookmark")
                }
                NavigationLink { FavoritenView() } label: {
                    Label("Favoriten", systemImage: "heart")
                }
                NavigationLink { PlaylistsView() } label: {
                    Label("Listen", systemImage: "list.bullet.rectangle")
                }
                NavigationLink { CollectionsView() } label: {
                    Label("Filmreihen", systemImage: "square.stack")
                }
            } header: {
                Text("Meine Sammlungen")
            }

            Section {
                NavigationLink { NeuigkeitenView() } label: {
                    Label("Neuigkeiten", systemImage: "bell")
                        .badge(ungelesen)
                }
                NavigationLink { WishesView() } label: {
                    Label("Wünsche", systemImage: "plus.circle")
                }
                NavigationLink { DownloadsView() } label: {
                    Label("Downloads", systemImage: "arrow.down.circle")
                }
            }

            Section {
                NavigationLink { ProfilSchutzView() } label: {
                    Label("Profil schützen", systemImage: "lock")
                }
                NavigationLink { SettingsView() } label: {
                    Label("Einstellungen", systemImage: "gearshape")
                }
            }
        }
        .navigationTitle("Ich")
        // Den Zähler der Neuigkeiten auffrischen, wenn man hereinschaut.
        .task { await session.refreshMe() }
    }
}
