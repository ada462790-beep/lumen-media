// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

import SwiftUI
import LumenKit

// Mehrere Server und das Anmelden mit Code auf dem Apple TV.
//
// Mit der Fernbedienung ein Passwort zu tippen, ist eine Qual. Darum ist
// hier der Code der Hauptweg: der Fernseher zeigt sechs Ziffern, jemand
// gibt sie am Handy oder im Browser ein, fertig. Die Profilauswahl mit
// PIN bleibt daneben — für Profile ohne Schloss ist sie ein Klick.

// MARK: - Anmelden: Code oder Profil

struct TVAnmeldenView: View {
    let profiles: [ProfileCard]
    /// Welchen Weg dieses Gerät zuletzt genommen hat. Wer einmal „Profil
    /// hier wählen" gewählt hat, landet beim nächsten Mal gleich dort.
    @AppStorage("lumen.tv.anmeldeweg") private var weg = "code"

    var body: some View {
        if weg == "profil" {
            TVProfilesView(profiles: profiles, mitCode: { weg = "code" })
        } else {
            TVCodeAnmeldenView(profilHier: { weg = "profil" })
        }
    }
}

struct TVCodeAnmeldenView: View {
    @Environment(Session.self) private var session
    let profilHier: () -> Void
    /// Was auf dem Knopf für den anderen Weg steht — zu Hause die
    /// Profilauswahl, von unterwegs Name und PIN.
    var profilHierTitel = String(localized: "Profil hier wählen", bundle: .lumen, locale: .lumen)
    var profilHierSymbol = "person.2"
    @State private var anmeldung = CodeAnmeldung()

    var body: some View {
        HStack(alignment: .center, spacing: 110) {
            VStack(alignment: .leading, spacing: 34) {
                Text("Mit Code anmelden")
                    .font(.system(size: 54, weight: .bold))

                Text("Öffne Lumen auf dem Handy oder im Browser → Profil → Fernseher anmelden und gib den Code ein.")
                    .font(.title3)
                    .foregroundStyle(Theme.label2)
                    .frame(maxWidth: 860, alignment: .leading)
                    .fixedSize(horizontal: false, vertical: true)

                ZStack {
                    if let code = anmeldung.codeLesbar {
                        Text(code)
                            .font(.system(size: 150, weight: .bold, design: .rounded))
                            .monospacedDigit()
                            .kerning(14)
                            .contentTransition(.numericText())
                            .accessibilityLabel("Code \(code)")
                    } else {
                        ProgressView()
                    }
                }
                .frame(width: 760, height: 220)
                .background(RoundedRectangle(cornerRadius: 30, style: .continuous).fill(Theme.surface))
                .animation(.easeInOut(duration: 0.3), value: anmeldung.code)

                if let bis = anmeldung.bis {
                    TimelineView(.periodic(from: .now, by: 1)) { kontext in
                        Text("Gilt noch \(Anmeldecode.restzeit(bis: bis, jetzt: kontext.date)) — danach erscheint von selbst ein neuer.")
                            .font(.callout.monospacedDigit())
                            .foregroundStyle(Theme.label3)
                    }
                }

                if let fehler = anmeldung.fehler ?? session.lastError {
                    Text(fehler).font(.callout).foregroundStyle(Theme.bad)
                        .frame(maxWidth: 860, alignment: .leading)
                }
            }

            VStack(alignment: .leading, spacing: 28) {
                Text("Oder hier am Fernseher")
                    .font(.headline)
                    .foregroundStyle(Theme.label2)
                Button(action: profilHier) {
                    Label(profilHierTitel, systemImage: profilHierSymbol)
                        .frame(width: 420, alignment: .leading)
                }
                Button {
                    session.serverWechseln()
                } label: {
                    Label("Server wechseln", systemImage: "server.rack")
                        .frame(width: 420, alignment: .leading)
                }
                if let aktiv = session.aktiverServer {
                    Text("Verbunden mit \(aktiv.name)")
                        .font(.caption)
                        .foregroundStyle(Theme.label3)
                }
            }
        }
        .padding(80)
        .task { await anmeldung.laufen(session) }
    }
}

// MARK: - Von unterwegs

/// Von unterwegs gibt der Server keine Profilauswahl heraus. Zuerst der
/// Code (am Handy freigeben), daneben Profilname und PIN — wer will, tippt.
struct TVUnterwegsView: View {
    @State private var mitName = false

    var body: some View {
        if mitName {
            TVUnterwegsNameView(zumCode: { mitName = false })
        } else {
            TVCodeAnmeldenView(profilHier: { mitName = true },
                               profilHierTitel: String(localized: "Mit Profilname und PIN", bundle: .lumen, locale: .lumen),
                               profilHierSymbol: "person.text.rectangle")
        }
    }
}

struct TVUnterwegsNameView: View {
    @Environment(Session.self) private var session
    let zumCode: () -> Void
    @State private var name = ""
    @State private var secret = ""
    @State private var working = false

    var body: some View {
        VStack(spacing: 30) {
            Text("Von unterwegs anmelden").font(.system(size: 54, weight: .bold))
            Text("Ausserhalb deines Heimnetzes zeigt Lumen keine Profile. Gib den Namen deines Profils und deinen PIN oder dein Passwort ein.")
                .font(.title3).foregroundStyle(Theme.label2)
                .multilineTextAlignment(.center).frame(maxWidth: 900)

            TextField("Profilname", text: $name)
                .frame(width: 620)
                .autocorrectionDisabled()
            SecureField("PIN oder Passwort", text: $secret)
                .frame(width: 620)
                .onSubmit(anmelden)

            if let error = session.lastError {
                Text(error).foregroundStyle(Theme.bad)
                    .multilineTextAlignment(.center).frame(maxWidth: 900)
            }

            HStack(spacing: 40) {
                Button(action: anmelden) {
                    if working { ProgressView() } else { Text("Anmelden") }
                }
                .disabled(name.trimmingCharacters(in: .whitespaces).isEmpty || secret.isEmpty || working)

                Button(action: zumCode) {
                    Label("Mit Code anmelden", systemImage: "number")
                }
                Button {
                    session.serverWechseln()
                } label: {
                    Label("Server wechseln", systemImage: "server.rack")
                }
            }
        }
        .padding(80)
    }

    private func anmelden() {
        guard !working, !name.trimmingCharacters(in: .whitespaces).isEmpty, !secret.isEmpty
        else { return }
        working = true
        Task {
            await session.unlockUnterwegs(name: name, secret: secret)
            working = false
            if session.me == nil { secret = "" }
        }
    }
}

// MARK: - Liste der Server

struct TVServerListeView: View {
    @Environment(Session.self) private var session

    @State private var umbenennen: ServerEintrag?
    @State private var neuerName = ""
    @State private var entfernen: ServerEintrag?

    var body: some View {
        VStack(alignment: .leading, spacing: 40) {
            VStack(alignment: .leading, spacing: 12) {
                Text("Server").font(.system(size: 54, weight: .bold))
                Text("Wähle, mit welchem Lumen sich dieser Fernseher verbindet. Lange drücken zum Umbenennen oder Entfernen.")
                    .font(.title3)
                    .foregroundStyle(Theme.label2)
            }
            .padding(.horizontal, 80)

            ScrollView(.horizontal, showsIndicators: false) {
                LazyHStack(spacing: 50) {
                    ForEach(session.server) { eintrag in
                        Button {
                            Task { await session.serverWaehlen(eintrag) }
                        } label: {
                            karte(eintrag)
                        }
                        .buttonStyle(.card)
                        .contextMenu {
                            Button {
                                neuerName = eintrag.name
                                umbenennen = eintrag
                            } label: {
                                Label("Umbenennen", systemImage: "pencil")
                            }
                            Button(role: .destructive) {
                                entfernen = eintrag
                            } label: {
                                Label("Entfernen", systemImage: "trash")
                            }
                        }
                    }

                    Button {
                        session.serverHinzufuegen()
                    } label: {
                        VStack(spacing: 18) {
                            Image(systemName: "plus")
                                .font(.system(size: 54, weight: .medium))
                            Text("Server hinzufügen").font(.title3)
                        }
                        .foregroundStyle(Theme.label2)
                        .frame(width: 400, height: 260)
                    }
                    .buttonStyle(.card)
                }
                .padding(.horizontal, 80)
                .padding(.vertical, 40)
            }

            if let error = session.lastError {
                Text(error).foregroundStyle(Theme.bad).padding(.horizontal, 80)
            }
        }
        .alert("Server umbenennen",
               isPresented: Binding(get: { umbenennen != nil },
                                    set: { if !$0 { umbenennen = nil } }),
               presenting: umbenennen) { eintrag in
            TextField("Name", text: $neuerName)
            Button("Sichern") { session.serverUmbenennen(eintrag, in: neuerName) }
            Button("Abbrechen", role: .cancel) {}
        } message: { _ in
            Text("So heisst der Server nur auf diesem Fernseher.")
        }
        .confirmationDialog("Server entfernen?",
                            isPresented: Binding(get: { entfernen != nil },
                                                 set: { if !$0 { entfernen = nil } }),
                            titleVisibility: .visible,
                            presenting: entfernen) { eintrag in
            Button("Entfernen", role: .destructive) {
                Task { await session.serverEntfernen(eintrag) }
            }
            Button("Abbrechen", role: .cancel) {}
        } message: { eintrag in
            Text("„\(eintrag.name)“ verschwindet von diesem Fernseher, und er meldet sich dort ab.")
        }
    }

    private func karte(_ eintrag: ServerEintrag) -> some View {
        let aktiv = session.aktiverServer?.id == eintrag.id
        let angemeldet = session.angemeldeteServer.contains(eintrag.id)
        return VStack(alignment: .leading, spacing: 10) {
            HStack {
                Image(systemName: eintrag.vonUnterwegs ? "globe" : "server.rack")
                    .font(.system(size: 40, weight: .medium))
                    .foregroundStyle(Theme.accent)
                Spacer()
                if aktiv {
                    Image(systemName: "checkmark.circle.fill")
                        .font(.system(size: 30))
                        .foregroundStyle(Theme.accent)
                }
            }
            Spacer(minLength: 0)
            Text(eintrag.name)
                .font(.title3.weight(.semibold))
                .lineLimit(1)
            Text(eintrag.adresseKurz)
                .font(.caption)
                .foregroundStyle(Theme.label2)
                .lineLimit(1)
            Text(angemeldet ? "Angemeldet" : "Nicht angemeldet")
                .font(.caption)
                .foregroundStyle(angemeldet ? Theme.good : Theme.label3)
        }
        .padding(28)
        .frame(width: 400, height: 260, alignment: .leading)
    }
}
