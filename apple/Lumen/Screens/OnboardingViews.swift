// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

import SwiftUI
import LumenKit

// Die Schritte bis zur Mediathek: Server eintragen, einrichten oder
// einlösen, Profil wählen, entsperren.

// MARK: - Server eintragen

struct ServerAddressView: View {
    @Environment(Session.self) private var session
    let message: String?

    @State private var address = ""
    @State private var fingerabdruck = ""
    @State private var vonAussen = false
    @State private var working = false
    @FocusState private var focused: Bool

    var body: some View {
        VStack(spacing: 26) {
            VStack(spacing: 10) {
                Text("Lumen")
                    .font(.largeTitle.weight(.bold))
                Text("Gib die Adresse deines Servers ein. Die steht in der Zeile, die Lumen beim Starten ausgibt — oft etwas wie 192.168.1.20.")
                    .font(.callout)
                    .foregroundStyle(Theme.label2)
                    .multilineTextAlignment(.center)
                    .frame(maxWidth: 420)
            }

            VStack(alignment: .leading, spacing: 10) {
                TextField("lumen.local:8097", text: $address)
                    .textFieldStyle(.plain)
                    .font(.body)
                    .padding(.horizontal, 15).padding(.vertical, 13)
                    .background(RoundedRectangle(cornerRadius: 12).fill(Theme.fill))
                    .focused($focused)
                    #if !os(macOS)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                    .keyboardType(.URL)
                    #endif
                    .onSubmit(connect)

                // Von unterwegs läuft die Verbindung verschlüsselt, und ein
                // Heimserver stellt sein Zertifikat meist selbst aus. Dann
                // braucht die App einmal dessen Fingerabdruck — der steht
                // in der Verwaltung unter „Von aussen erreichbar".
                DisclosureGroup("Von unterwegs verbinden", isExpanded: $vonAussen) {
                    VStack(alignment: .leading, spacing: 8) {
                        Text("Trag die Adresse deines Vermittlers ein und darunter den Fingerabdruck, den der Server in der Verwaltung anzeigt. Er sorgt dafür, dass sich kein anderer dazwischenschiebt.")
                            .font(.footnote)
                            .foregroundStyle(Theme.label2)
                            .fixedSize(horizontal: false, vertical: true)

                        TextField("Fingerabdruck (64 Zeichen)", text: $fingerabdruck,
                                  axis: .vertical)
                            .textFieldStyle(.plain)
                            .font(.system(.footnote, design: .monospaced))
                            .lineLimit(2...3)
                            .padding(.horizontal, 15).padding(.vertical, 11)
                            .background(RoundedRectangle(cornerRadius: 12).fill(Theme.fill))
                            #if !os(macOS)
                            .textInputAutocapitalization(.never)
                            .autocorrectionDisabled()
                            #endif

                        if !fingerabdruck.isEmpty && Pinnen.saubern(fingerabdruck) == nil {
                            Text("Das sind nicht 64 Zeichen aus 0–9 und a–f. Am einfachsten in der Verwaltung kopieren.")
                                .font(.caption)
                                .foregroundStyle(Theme.bad)
                                .fixedSize(horizontal: false, vertical: true)
                        }
                    }
                    .padding(.top, 6)
                }
                .font(.footnote)
                .tint(Theme.label2)

                if let message {
                    Text(message)
                        .font(.footnote)
                        .foregroundStyle(Theme.bad)
                        .fixedSize(horizontal: false, vertical: true)
                }
            }
            .frame(maxWidth: 420)

            Button(action: connect) {
                if working { ProgressView().controlSize(.small) } else { Text("Verbinden") }
            }
            .buttonStyle(PrimaryButtonStyle())
            .disabled(address.trimmingCharacters(in: .whitespaces).isEmpty || working)
        }
        .padding(40)
        // Sind schon Server gespeichert, führt ein Pfeil zurück zur Liste —
        // wer „Server hinzufügen" nur aus Versehen getippt hat, oder wessen
        // Server gerade nicht antwortet, nimmt einen anderen.
        .overlay(alignment: .topLeading) {
            if !session.server.isEmpty {
                Button {
                    session.zurueckZurServerliste()
                } label: {
                    Image(systemName: "chevron.left").font(.callout.weight(.semibold))
                        .frame(width: 42, height: 42)
                        .background(Circle().fill(Theme.fill))
                }
                .buttonStyle(.plain)
                .accessibilityLabel("Zur Liste der Server")
                .padding(28)
            }
        }
        .onAppear {
            focused = true
            if address.isEmpty, let vorschlag = session.adressVorschlag { address = vorschlag }
        }
    }

    private func connect() {
        guard !working else { return }
        working = true
        Task {
            await session.useServer(address: address,
                                    fingerabdruck: Pinnen.saubern(fingerabdruck))
            working = false
        }
    }
}

// MARK: - Ersteinrichtung

struct FirstRunView: View {
    @Environment(Session.self) private var session
    @State private var name = ""
    @State private var password = ""
    @State private var working = false
    @State private var showInvite = false

    var body: some View {
        VStack(spacing: 24) {
            VStack(spacing: 10) {
                Text("Willkommen bei Lumen").font(.title.weight(.bold))
                Text("Du richtest diesen Server ein und wirst sein Besitzer. Später lädst du andere mit einem Link dazu ein — jede Person bekommt ein eigenes Profil.")
                    .font(.callout).foregroundStyle(Theme.label2)
                    .multilineTextAlignment(.center).frame(maxWidth: 440)
            }

            VStack(spacing: 14) {
                Labeled(String(localized: "Dein Name", bundle: .lumen, locale: .lumen)) {
                    TextField("", text: $name).textFieldStyle(.plain)
                }
                Labeled(String(localized: "Passwort", bundle: .lumen, locale: .lumen), hint: String(localized: "Mindestens acht Zeichen. Damit meldest du dich auf allen Geräten an.", bundle: .lumen, locale: .lumen)) {
                    SecureField("", text: $password).textFieldStyle(.plain)
                }
                if let error = session.lastError {
                    Text(error).font(.footnote).foregroundStyle(Theme.bad)
                }
            }
            .frame(maxWidth: 420)

            Button {
                working = true
                Task { await session.setUpOwner(name: name, password: password); working = false }
            } label: {
                if working { ProgressView().controlSize(.small) } else { Text("Server einrichten") }
            }
            .buttonStyle(PrimaryButtonStyle())
            .disabled(name.isEmpty || password.count < 8 || working)

            Button("Du wurdest eingeladen? Einladung einlösen") { showInvite = true }
                .font(.footnote).foregroundStyle(Theme.accent).buttonStyle(.plain)
        }
        .padding(40)
        .sheet(isPresented: $showInvite) { InviteView() }
    }
}

// MARK: - Einladung einlösen

struct InviteView: View {
    @Environment(Session.self) private var session
    @Environment(\.dismiss) private var dismiss

    @State private var code = ""
    @State private var name = ""
    @State private var pin = ""

    var body: some View {
        VStack(spacing: 22) {
            Text("Einladung einlösen").font(.title2.weight(.semibold))
            VStack(spacing: 14) {
                Labeled(String(localized: "Code aus dem Link", bundle: .lumen, locale: .lumen)) {
                    TextField("", text: $code).textFieldStyle(.plain)
                }
                Labeled(String(localized: "Dein Name", bundle: .lumen, locale: .lumen)) {
                    TextField("", text: $name).textFieldStyle(.plain)
                }
                Labeled("PIN", hint: String(localized: "Vier Ziffern, damit niemand sonst dein Profil öffnet. Kann leer bleiben.", bundle: .lumen, locale: .lumen)) {
                    SecureField("", text: $pin).textFieldStyle(.plain)
                }
                if let error = session.lastError {
                    Text(error).font(.footnote).foregroundStyle(Theme.bad)
                }
            }
            Button("Loslegen") {
                Task {
                    await session.redeemInvite(code: code, name: name, pin: pin)
                    if session.me != nil { dismiss() }
                }
            }
            .buttonStyle(PrimaryButtonStyle())
            .disabled(code.isEmpty || name.isEmpty)
        }
        .padding(34)
        .frame(minWidth: 380)
        .background(Theme.background)
        .onAppear {
            if let pending = session.pendingInvite { code = pending; session.pendingInvite = nil }
        }
    }
}

// MARK: - Profilwahl

struct ProfilesView: View {
    @Environment(Session.self) private var session
    let profiles: [ProfileCard]
    @State private var mitCode = false

    private var columns: [GridItem] {
        [GridItem(.adaptive(minimum: 130, maximum: 180), spacing: 34)]
    }

    var body: some View {
        VStack(spacing: 40) {
            Text("Wer schaut?").font(.title.weight(.bold))

            LazyVGrid(columns: columns, spacing: 30) {
                ForEach(profiles) { profile in
                    Button {
                        Task { await session.chooseProfile(profile) }
                    } label: {
                        VStack(spacing: 12) {
                            ProfilAvatar(name: profile.name, avatar: profile.avatar, accountId: profile.id, size: 96)
                            HStack(spacing: 5) {
                                Text(profile.name).font(.body)
                                if profile.needsCode {
                                    Image(systemName: "lock.fill")
                                        .font(.caption2)
                                        .foregroundStyle(Theme.label3)
                                }
                            }
                        }
                    }
                    .buttonStyle(.plain)
                }
            }
            .frame(maxWidth: 620)

            if let error = session.lastError {
                Text(error).font(.footnote).foregroundStyle(Theme.bad)
            }

            // Ohne Passwort tippen: ein anderes Gerät, auf dem man schon
            // angemeldet ist, gibt einen Code frei.
            ViewThatFits {
                HStack(spacing: 12) { andereWege }
                VStack(spacing: 12) { andereWege }
            }
        }
        .padding(40)
        .sheet(isPresented: $mitCode) { CodeAnmeldenView().environment(session) }
    }

    @ViewBuilder
    private var andereWege: some View {
        Button {
            mitCode = true
        } label: {
            Label("Mit Code von einem anderen Gerät", systemImage: "number")
        }
        .buttonStyle(QuietButtonStyle())

        Button {
            session.serverWechseln()
        } label: {
            Label(session.server.count > 1 ? "Server wechseln" : "Anderer Server",
                  systemImage: "server.rack")
        }
        .buttonStyle(QuietButtonStyle())
    }
}

// MARK: - Entsperren

struct UnlockView: View {
    @Environment(Session.self) private var session
    let profile: ProfileCard

    @State private var secret = ""
    @State private var working = false
    @FocusState private var focused: Bool

    var body: some View {
        VStack(spacing: 24) {
            ProfilAvatar(name: profile.name, avatar: profile.avatar, accountId: profile.id, size: 88)
            Text(profile.name).font(.title3.weight(.semibold))

            SecureField(profile.lockKind == "pin" ? "••••" : "Passwort", text: $secret)
                .textFieldStyle(.plain)
                .multilineTextAlignment(.center)
                .font(.title3.weight(.medium))
                .padding(.vertical, 13)
                .frame(width: 240)
                .background(RoundedRectangle(cornerRadius: 12).fill(Theme.fill))
                .focused($focused)
                .onSubmit(unlock)
                #if !os(macOS)
                .keyboardType(profile.lockKind == "pin" ? .numberPad : .default)
                #endif

            if let error = session.lastError {
                Text(error).font(.footnote).foregroundStyle(Theme.bad)
            }

            Button(action: unlock) {
                if working { ProgressView().controlSize(.small) } else { Text("Weiter") }
            }
            .buttonStyle(PrimaryButtonStyle())
            .disabled(secret.isEmpty || working)
        }
        .padding(40)
        .overlay(alignment: .topLeading) {
            Button {
                Task { await session.signOut() }
            } label: {
                Image(systemName: "chevron.left").font(.callout.weight(.semibold))
                    .frame(width: 42, height: 42)
                    .background(Circle().fill(Theme.fill))
            }
            .buttonStyle(.plain)
            .accessibilityLabel("Zurück zur Profilauswahl")
            .padding(28)
        }
        .onAppear { focused = true }
    }

    private func unlock() {
        guard !working else { return }
        working = true
        Task { await session.unlock(profile, secret: secret); working = false }
    }
}

// MARK: - Von unterwegs anmelden

/// Von unterwegs gibt der Server keine Profilauswahl heraus — sie verriete
/// die Namen im Haushalt, oft auch die der Kinder. Angemeldet wird dann mit
/// Profilname und PIN oder Passwort, oder mit einem Code von einem Gerät,
/// das schon angemeldet ist. Was der Server dazu sagt (falsch, zu viele
/// Versuche, Profil ohne PIN), steht unverändert darunter.
struct UnterwegsAnmeldenView: View {
    @Environment(Session.self) private var session

    @State private var name = ""
    @State private var secret = ""
    @State private var working = false
    @State private var mitCode = false
    @FocusState private var feld: Feld?

    private enum Feld { case name, secret }

    var body: some View {
        VStack(spacing: 24) {
            VStack(spacing: 10) {
                Text("Von unterwegs anmelden").font(.title.weight(.bold))
                Text("Von ausserhalb deines Heimnetzes zeigt Lumen keine Profile. Gib den Namen deines Profils und deinen PIN oder dein Passwort ein.")
                    .font(.callout).foregroundStyle(Theme.label2)
                    .multilineTextAlignment(.center).frame(maxWidth: 440)
                    .fixedSize(horizontal: false, vertical: true)
            }

            VStack(spacing: 14) {
                Labeled(String(localized: "Profilname", bundle: .lumen, locale: .lumen)) {
                    TextField("", text: $name)
                        .textFieldStyle(.plain)
                        .textContentType(.username)
                        .focused($feld, equals: .name)
                        .onSubmit { feld = .secret }
                        #if !os(macOS)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                        #endif
                }
                Labeled(String(localized: "PIN oder Passwort", bundle: .lumen, locale: .lumen),
                        hint: String(localized: "Ein Profil ohne PIN lässt sich nur zu Hause öffnen.", bundle: .lumen, locale: .lumen)) {
                    SecureField("", text: $secret)
                        .textFieldStyle(.plain)
                        .textContentType(.password)
                        .focused($feld, equals: .secret)
                        .onSubmit(anmelden)
                }
                if let error = session.lastError {
                    Text(error).font(.footnote).foregroundStyle(Theme.bad)
                        .fixedSize(horizontal: false, vertical: true)
                }
            }
            .frame(maxWidth: 420)

            Button(action: anmelden) {
                if working { ProgressView().controlSize(.small) } else { Text("Anmelden") }
            }
            .buttonStyle(PrimaryButtonStyle())
            .disabled(name.trimmingCharacters(in: .whitespaces).isEmpty || secret.isEmpty || working)

            // Ohne Tippen: ein Gerät, auf dem man schon angemeldet ist,
            // gibt einen Code frei — das geht auch von unterwegs.
            ViewThatFits {
                HStack(spacing: 12) { andereWege }
                VStack(spacing: 12) { andereWege }
            }
        }
        .padding(40)
        .sheet(isPresented: $mitCode) { CodeAnmeldenView().environment(session) }
        .onAppear { feld = .name }
    }

    @ViewBuilder
    private var andereWege: some View {
        Button {
            mitCode = true
        } label: {
            Label("Mit Code von einem anderen Gerät", systemImage: "number")
        }
        .buttonStyle(QuietButtonStyle())

        Button {
            session.serverWechseln()
        } label: {
            Label(session.server.count > 1 ? "Server wechseln" : "Anderer Server",
                  systemImage: "server.rack")
        }
        .buttonStyle(QuietButtonStyle())
    }

    private func anmelden() {
        guard !working, !name.trimmingCharacters(in: .whitespaces).isEmpty, !secret.isEmpty
        else { return }
        working = true
        Task {
            await session.unlockUnterwegs(name: name, secret: secret)
            working = false
            // Nach einem Fehlversuch neu tippen — der Name bleibt stehen.
            if session.me == nil { secret = "" }
        }
    }
}

// MARK: - Kleiner Baustein

struct Labeled<Content: View>: View {
    let title: String
    var hint: String?
    @ViewBuilder let content: Content

    init(_ title: String, hint: String? = nil, @ViewBuilder content: () -> Content) {
        self.title = title
        self.hint = hint
        self.content = content()
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 7) {
            Text(title).font(.footnote).foregroundStyle(Theme.label2)
            content
                .font(.callout)
                .padding(.horizontal, 15).padding(.vertical, 12)
                .background(RoundedRectangle(cornerRadius: 12).fill(Theme.fill))
            if let hint {
                Text(hint).font(.caption).foregroundStyle(Theme.label3)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}
