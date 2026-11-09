// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

import SwiftUI
import LumenKit

// Was unter dem eigenen Namen in der Leiste steht, ausser den
// Einstellungen: die übrigen Bereiche (Favoriten, Filmreihen, Listen,
// Neuigkeiten, Wünsche), die eigenen Geräte und das Einlösen einer
// Einladung.

/// Die Bereiche, die nicht in die Leiste passen — als erster Abschnitt
/// des Profils.
struct TVIchBereiche: View {
    @Environment(Session.self) private var session

    var body: some View {
        Section {
            NavigationLink { TVFavoritenView() } label: {
                Label("Favoriten", systemImage: "heart")
            }
            NavigationLink { TVFilmreihenView() } label: {
                Label("Filmreihen", systemImage: "square.stack")
            }
            NavigationLink { TVListenView() } label: {
                Label("Listen", systemImage: "list.bullet.rectangle")
            }
            NavigationLink { TVNeuigkeitenView() } label: {
                LabeledContent {
                    if let n = session.me?.ungelesen, n > 0 { Text(verbatim: n > 99 ? "99+" : String(n)) }
                } label: {
                    Label("Neuigkeiten", systemImage: "bell")
                }
            }
            NavigationLink { TVWuenscheView() } label: {
                Label("Wünsche", systemImage: "plus.circle")
            }
        } header: {
            Text("Mehr")
        }
        Section {
            NavigationLink { ProfilSchutzView() } label: {
                LabeledContent {
                    Text(Schutzart(session.me?.account.lockKind).name)
                } label: {
                    Label("Profil schützen", systemImage: "lock")
                }
            }
            NavigationLink { TVGeraeteView() } label: {
                Label("Meine Geräte", systemImage: "tv.and.mediabox")
            }
        } header: {
            Text("Sicherheit")
        }
    }
}

/// Die eigenen Geräte — und „Überall abmelden", wenn eines verloren ging.
struct TVGeraeteView: View {
    @Environment(Session.self) private var session
    @State private var geraete: [Device] = []
    @State private var geladen = false
    @State private var fehler: String?
    @State private var fragen = false

    var body: some View {
        List {
            if let fehler {
                Section {
                    TVFehler(text: fehler) { await laden() }
                }
            }
            Section {
                if !geladen {
                    ProgressView()
                }
                ForEach(geraete, id: \.self) { g in
                    // Ein Knopf ohne Wirkung, damit die Zeile den Fokus
                    // bekommen kann — sonst liesse sich die Liste mit der
                    // Fernbedienung nicht durchgehen.
                    Button {} label: {
                        HStack(spacing: 24) {
                            Image(systemName: GeraeteText.symbol(g.kind))
                                .frame(width: 50)
                                .accessibilityHidden(true)
                            VStack(alignment: .leading, spacing: 4) {
                                Text(g.name)
                                Text(GeraeteText.art(g.kind)).font(.caption).foregroundStyle(Theme.label2)
                            }
                            Spacer()
                            Text(g.lastSeen.formatted(Date.FormatStyle(date: .abbreviated, time: .omitted).locale(.lumen)))
                                .font(.caption).foregroundStyle(Theme.label2)
                        }
                    }
                }
            } header: {
                Text("Angemeldet mit deinem Profil")
            }
            Section {
                Button(role: .destructive) {
                    fragen = true
                } label: {
                    Text("Überall abmelden")
                }
            } footer: {
                Text("Alle Geräte müssen sich danach neu anmelden — dieses auch.")
            }
        }
        .navigationTitle("Meine Geräte")
        .task { await laden() }
        .confirmationDialog("Überall abmelden?", isPresented: $fragen) {
            Button("Abmelden", role: .destructive) {
                Task {
                    do {
                        try await session.client.signOutEverywhere()
                        await session.signOut()
                    } catch {
                        fehler = Session.text(error)
                    }
                }
            }
            Button("Abbrechen", role: .cancel) {}
        }
    }

    private func laden() async {
        fehler = nil
        do {
            geraete = try await session.client.devices()
        } catch {
            if !APIClient.istAbbruch(error) { fehler = Session.text(error) }
        }
        geladen = true
    }
}

/// Eine Einladung einlösen: Code aus der Nachricht, eigener Name, auf
/// Wunsch ein PIN. Danach ist man gleich angemeldet.
struct TVEinladungView: View {
    @Environment(Session.self) private var session
    @Environment(\.dismiss) private var dismiss
    @State private var code = ""
    @State private var name = ""
    @State private var pin = ""
    @State private var arbeitet = false

    private var bereit: Bool {
        !code.trimmingCharacters(in: .whitespaces).isEmpty
            && !name.trimmingCharacters(in: .whitespaces).isEmpty
            && (pin.isEmpty || (pin.count >= 4 && pin.allSatisfy(\.isNumber)))
    }

    var body: some View {
        VStack(spacing: 30) {
            Text("Einladung einlösen").font(.largeTitle.weight(.bold))
            Text("Den Code findest du in der Einladung. Ein PIN ist freiwillig — mit ihm kann niemand sonst dein Profil öffnen.")
                .font(.title3).foregroundStyle(Theme.label2)
                .multilineTextAlignment(.center).frame(maxWidth: 1000)
            TextField("Code", text: $code)
                .frame(width: 600)
                .autocorrectionDisabled()
                .textInputAutocapitalization(.characters)
            TextField("Dein Name", text: $name)
                .frame(width: 600)
            SecureField("PIN (freiwillig, mindestens 4 Ziffern)", text: $pin)
                .frame(width: 600)
                .keyboardType(.numberPad)
            if let fehler = session.lastError {
                Text(fehler).foregroundStyle(Theme.bad)
            }
            HStack(spacing: 40) {
                Button("Abbrechen") { dismiss() }
                Button(arbeitet ? "Einen Moment …" : "Einlösen") { Task { await einloesen() } }
                    .disabled(!bereit || arbeitet)
            }
        }
        .padding(80)
        .onAppear { if let offen = session.pendingInvite { code = offen; session.pendingInvite = nil } }
    }

    private func einloesen() async {
        arbeitet = true
        defer { arbeitet = false }
        await session.redeemInvite(code: code.trimmingCharacters(in: .whitespaces),
                                   name: name.trimmingCharacters(in: .whitespaces), pin: pin)
        if session.me != nil { dismiss() }
    }
}
