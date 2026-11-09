// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

import SwiftUI
import LumenKit

// Mehrere Server und das Anmelden mit Code, für iPhone, iPad und Mac.
//
// Die Liste zeigt nur, was dieses Gerät selbst weiss: Name, Adresse und
// ob es dort angemeldet ist. Wer wann was schaut, steht nirgends.

// MARK: - Liste der Server

struct ServerListeView: View {
    @Environment(Session.self) private var session

    @State private var umbenennen: ServerEintrag?
    @State private var neuerName = ""
    @State private var entfernen: ServerEintrag?

    var body: some View {
        ScrollView {
            VStack(spacing: 28) {
                VStack(spacing: 8) {
                    Text("Server").font(.title.weight(.bold))
                    Text("Wähle, mit welchem Lumen du dich verbindest.")
                        .font(.callout)
                        .foregroundStyle(Theme.label2)
                        .multilineTextAlignment(.center)
                }

                VStack(spacing: 0) {
                    ForEach(Array(session.server.enumerated()), id: \.element.id) { nr, eintrag in
                        if nr > 0 {
                            Divider().padding(.leading, 66)
                        }
                        zeile(eintrag)
                    }
                }
                .background(RoundedRectangle(cornerRadius: 14, style: .continuous).fill(Theme.surface))
                .frame(maxWidth: 520)

                Button {
                    session.serverHinzufuegen()
                } label: {
                    Label("Server hinzufügen", systemImage: "plus")
                }
                .buttonStyle(QuietButtonStyle())

                if let error = session.lastError {
                    Text(error).font(.footnote).foregroundStyle(Theme.bad)
                }
            }
            .padding(.horizontal, 24)
            .padding(.vertical, 48)
            .frame(maxWidth: .infinity)
        }
        .alert("Server umbenennen",
               isPresented: Binding(get: { umbenennen != nil },
                                    set: { if !$0 { umbenennen = nil } }),
               presenting: umbenennen) { eintrag in
            TextField("Name", text: $neuerName)
            Button("Sichern") { session.serverUmbenennen(eintrag, in: neuerName) }
            Button("Abbrechen", role: .cancel) {}
        } message: { _ in
            Text("So heisst der Server nur auf diesem Gerät.")
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
            Text("„\(eintrag.name)“ verschwindet von diesem Gerät, und das Gerät meldet sich dort ab. Ist der Server gerade nicht erreichbar, steht es dort weiter unter „Geräte“.")
        }
    }

    private func zeile(_ eintrag: ServerEintrag) -> some View {
        let aktiv = session.aktiverServer?.id == eintrag.id
        return HStack(spacing: 12) {
            Button {
                Task { await session.serverWaehlen(eintrag) }
            } label: {
                HStack(spacing: 14) {
                    Image(systemName: eintrag.vonUnterwegs ? "globe" : "server.rack")
                        .font(.body.weight(.medium))
                        .foregroundStyle(Theme.accent)
                        .frame(width: 38, height: 38)
                        .background(RoundedRectangle(cornerRadius: 9, style: .continuous).fill(Theme.fill))
                    VStack(alignment: .leading, spacing: 2) {
                        Text(eintrag.name)
                            .font(.body.weight(.medium))
                            .foregroundStyle(Theme.label)
                            .lineLimit(1)
                        Text(untertitel(eintrag))
                            .font(.caption)
                            .foregroundStyle(Theme.label2)
                            .lineLimit(1)
                    }
                    Spacer(minLength: 8)
                    if aktiv {
                        Image(systemName: "checkmark")
                            .font(.subheadline.weight(.semibold))
                            .foregroundStyle(Theme.accent)
                            .accessibilityLabel("Zuletzt verbunden")
                    }
                }
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)

            Menu {
                aktionen(eintrag)
            } label: {
                Image(systemName: "ellipsis.circle")
                    .font(.title3)
                    .foregroundStyle(Theme.label2)
                    .frame(width: 32, height: 32)
            }
            #if os(macOS)
            .menuStyle(.borderlessButton)
            #endif
            .menuIndicator(.hidden)
            .fixedSize()
            .accessibilityLabel("Mehr zu \(eintrag.name)")
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 11)
        .contextMenu { aktionen(eintrag) }
    }

    @ViewBuilder
    private func aktionen(_ eintrag: ServerEintrag) -> some View {
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

    private func untertitel(_ eintrag: ServerEintrag) -> String {
        var teile = [eintrag.adresseKurz]
        if eintrag.vonUnterwegs { teile.append(String(localized: "von unterwegs", bundle: .lumen, locale: .lumen)) }
        if session.angemeldeteServer.contains(eintrag.id) { teile.append(String(localized: "angemeldet", bundle: .lumen, locale: .lumen)) }
        return teile.joined(separator: " · ")
    }
}

// MARK: - Mit Code anmelden (dieses Gerät)

/// Dieses Gerät zeigt einen Code; ein anderes, schon angemeldetes gibt
/// ihn frei. Auf dem iPhone selten nötig, aber praktisch, wenn das
/// Profil ein langes Passwort hat und das Handy des anderen danebenliegt.
struct CodeAnmeldenView: View {
    @Environment(Session.self) private var session
    @Environment(\.dismiss) private var dismiss
    @State private var anmeldung = CodeAnmeldung()

    var body: some View {
        VStack(spacing: 22) {
            Text("Mit Code anmelden").font(.title2.weight(.semibold))

            Text("Öffne Lumen auf einem Gerät, auf dem du schon angemeldet bist — Profil → Fernseher anmelden — und gib dort diesen Code ein.")
                .font(.callout)
                .foregroundStyle(Theme.label2)
                .multilineTextAlignment(.center)
                .frame(maxWidth: 360)
                .fixedSize(horizontal: false, vertical: true)

            Group {
                if let code = anmeldung.codeLesbar {
                    Text(code)
                        .font(.system(.largeTitle, design: .rounded, weight: .bold))
                        .monospacedDigit()
                        .kerning(3)
                        .textSelection(.enabled)
                        .accessibilityLabel("Code \(code)")
                } else {
                    ProgressView().controlSize(.large)
                }
            }
            .frame(height: 76)
            .frame(maxWidth: 320)
            .padding(.vertical, 8)
            .background(RoundedRectangle(cornerRadius: 16, style: .continuous).fill(Theme.surface))

            if let bis = anmeldung.bis {
                TimelineView(.periodic(from: .now, by: 1)) { kontext in
                    Text("Gilt noch \(Anmeldecode.restzeit(bis: bis, jetzt: kontext.date)) — danach kommt von selbst ein neuer.")
                        .font(.footnote.monospacedDigit())
                        .foregroundStyle(Theme.label3)
                }
            }

            if let fehler = anmeldung.fehler ?? session.lastError {
                Text(fehler).font(.footnote).foregroundStyle(Theme.bad)
                    .multilineTextAlignment(.center)
            }

            Button("Abbrechen") { dismiss() }
                .buttonStyle(QuietButtonStyle())
        }
        .padding(34)
        .frame(minWidth: 380)
        .background(Theme.background)
        .task { await anmeldung.laufen(session) }
        .onChange(of: session.me != nil) { _, drin in
            if drin { dismiss() }
        }
    }
}

// MARK: - Fernseher anmelden (anderes Gerät)

/// Der Fernseher zeigt sechs Ziffern; hier gibt man sie ein und meldet ihn
/// damit mit dem eigenen Profil an. Er bekommt ein eigenes Token, das
/// unter „Geräte" steht und sich dort wie jedes andere abmelden lässt.
struct FernseherAnmeldenView: View {
    @Environment(Session.self) private var session

    @State private var code = ""
    @State private var arbeitet = false
    @State private var erfolg: String?
    @State private var fehler: String?
    @FocusState private var fokus: Bool

    var body: some View {
        Form {
            Section {
                TextField("000 000", text: Binding(
                    get: { Anmeldecode.lesbar(code) },
                    set: { code = Anmeldecode.nurZiffern($0) }))
                    .font(.system(.largeTitle, design: .rounded, weight: .semibold).monospacedDigit())
                    .multilineTextAlignment(.center)
                    .focused($fokus)
                    .onSubmit(freigeben)
                    #if os(iOS)
                    .keyboardType(.numberPad)
                    .textContentType(.oneTimeCode)
                    #endif
                    .padding(.vertical, 6)
            } header: {
                Text("Code vom Fernseher")
            } footer: {
                Text("Öffne Lumen auf dem Fernseher und wähle „Mit Code anmelden“. Er wird mit deinem Profil angemeldet und erscheint danach unter „Geräte“.")
            }

            Section {
                Button(action: freigeben) {
                    HStack {
                        Text("Anmelden")
                        if arbeitet {
                            Spacer()
                            ProgressView().controlSize(.small)
                        }
                    }
                }
                .disabled(!Anmeldecode.vollstaendig(code) || arbeitet)
            }

            if let erfolg {
                Section {
                    Label(erfolg, systemImage: "checkmark.circle.fill")
                        .foregroundStyle(Theme.good)
                }
            }
            if let fehler {
                Section {
                    Text(fehler).foregroundStyle(Theme.bad)
                }
            }
        }
        .formStyle(.grouped)
        .navigationTitle("Fernseher anmelden")
        .onAppear { fokus = true }
        .onChange(of: code) { _, _ in
            // Wer weitertippt, will den nächsten Fernseher anmelden.
            if !code.isEmpty { erfolg = nil }
            fehler = nil
        }
    }

    private func freigeben() {
        guard Anmeldecode.vollstaendig(code), !arbeitet else { return }
        arbeitet = true
        fehler = nil
        erfolg = nil
        let eingabe = code
        Task {
            do {
                let antwort = try await session.client.codeFreigeben(eingabe)
                code = ""
                erfolg = String(localized: "\(antwort.geraet) ist angemeldet.", bundle: .lumen, locale: .lumen)
            } catch {
                fehler = Self.satz(Session.text(error))
            }
            arbeitet = false
        }
    }

    /// Der Server schreibt seine Meldungen klein — hier steht sie allein.
    static func satz(_ text: String) -> String {
        guard let erstes = text.first else { return text }
        return erstes.uppercased() + text.dropFirst()
    }
}
