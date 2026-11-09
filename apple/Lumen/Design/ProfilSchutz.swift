// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

import SwiftUI
import LumenKit

// Profil schützen: PIN oder Passwort ändern, setzen oder entfernen.
//
// Gehört der Person selbst, also auf jedem Gerät — auch auf dem Fernseher,
// wo man sonst nur mit dem Telefon an die Einstellungen käme. Darum liegt
// die Ansicht hier bei den gemeinsamen Teilen.
//
// Das alte Geheimnis braucht es immer, wenn es eins gibt: ein Kind am
// offenen iPad soll den PIN der Eltern nicht einfach ändern können.

/// Was für ein Schutz.
enum Schutzart: String, CaseIterable, Identifiable {
    case none, pin, password

    var id: String { rawValue }

    var name: LocalizedStringKey {
        switch self {
        case .none: "Kein Schutz"
        case .pin: "PIN"
        case .password: "Passwort"
        }
    }

    init(_ lockKind: String?) {
        self = Schutzart(rawValue: lockKind ?? "none") ?? .none
    }
}

/// Ob ein neues Geheimnis taugt — dieselben Regeln, die der Server prüft,
/// damit die Meldung schon vor dem Absenden kommt.
enum SchutzPruefung {
    static func fehler(art: Schutzart, neu: String, wiederholt: String) -> String? {
        switch art {
        case .none:
            return nil
        case .pin:
            if neu.count < 4 || !neu.allSatisfy(\.isNumber) {
                return String(localized: "Ein PIN hat mindestens vier Ziffern.", bundle: .lumen, locale: .lumen)
            }
        case .password:
            if neu.count < 8 { return String(localized: "Ein Passwort hat mindestens acht Zeichen.", bundle: .lumen, locale: .lumen) }
        }
        if neu != wiederholt { return String(localized: "Die beiden Eingaben stimmen nicht überein.", bundle: .lumen, locale: .lumen) }
        return nil
    }
}

struct ProfilSchutzView: View {
    @Environment(Session.self) private var session
    @Environment(\.dismiss) private var dismiss

    @State private var art: Schutzart = .pin
    @State private var alt = ""
    @State private var neu = ""
    @State private var wiederholt = ""
    @State private var fehler: String?
    @State private var erledigt = false
    @State private var speichert = false
    /// Der Server kennt den Weg noch nicht (404): dann sagt die Ansicht
    /// das, statt einen Fehler zu zeigen.
    @State private var nichtVerfuegbar = false

    private var bisher: Schutzart { Schutzart(session.me?.account.lockKind) }

    var body: some View {
        Form {
            Section {
                LabeledContent {
                    Text(bisher.name)
                } label: {
                    Text("Jetzt")
                }
            } footer: {
                Text("Mit PIN oder Passwort kann niemand sonst im Haushalt dein Profil öffnen. Von unterwegs geht die Anmeldung nur mit Schutz.")
            }

            if nichtVerfuegbar {
                Section {
                    Label("Dein Server kann das noch nicht. Nach dem nächsten Update geht es hier.",
                          systemImage: "clock.arrow.circlepath")
                        .foregroundStyle(Theme.label2)
                }
            } else {
                if bisher != .none {
                    Section {
                        SecureField(bisher == .pin ? "Bisheriger PIN" : "Bisheriges Passwort", text: $alt)
                            .kennwortFeld(neu: false)
                    } header: {
                        Text("Zur Sicherheit")
                    }
                }

                Section {
                    Picker("Schutz", selection: $art) {
                        ForEach(Schutzart.allCases) { a in Text(a.name).tag(a) }
                    }
                    if art != .none {
                        SecureField(art == .pin ? "Neuer PIN" : "Neues Passwort", text: $neu)
                            .kennwortFeld(neu: true)
                        SecureField("Noch einmal", text: $wiederholt)
                            .kennwortFeld(neu: true)
                    }
                } header: {
                    Text("Neu")
                } footer: {
                    if art == .none, session.me?.account.darfVerwalten == true {
                        Text("Wer verwalten darf, braucht einen PIN oder ein Passwort.")
                    }
                }

                if let fehler {
                    Section { Text(fehler).foregroundStyle(Theme.bad) }
                }
                if erledigt {
                    Section { Label("Gespeichert.", systemImage: "checkmark.circle").foregroundStyle(Theme.good) }
                }

                Section {
                    Button(speichert ? "Speichert …" : "Speichern") { Task { await speichern() } }
                        .disabled(speichert || (bisher != .none && alt.isEmpty))
                }
            }
        }
        .navigationTitle("Profil schützen")
        .onAppear { art = bisher == .none ? .pin : bisher }
    }

    private func speichern() async {
        fehler = nil
        erledigt = false
        if let problem = SchutzPruefung.fehler(art: art, neu: neu, wiederholt: wiederholt) {
            fehler = problem
            return
        }
        speichert = true
        defer { speichert = false }
        do {
            try await session.client.sperreAendern(alt: alt, art: art.rawValue, neu: art == .none ? "" : neu)
            alt = ""; neu = ""; wiederholt = ""
            erledigt = true
            await session.refreshMe()
        } catch {
            if LumenError.fehltAufServer(error) {
                nichtVerfuegbar = true
            } else {
                // „falsch": das bisherige Feld leeren, damit man neu tippt.
                if (error as? LumenError)?.code == "falsch" { alt = "" }
                fehler = Session.text(error)
            }
        }
    }
}

extension View {
    /// Damit der Schlüsselbund ein Passwort anbietet oder ein neues
    /// vorschlägt. Am Mac gibt es die Art „neues Passwort" nicht.
    func kennwortFeld(neu: Bool) -> some View {
        #if os(macOS)
        return textContentType(.password)
        #else
        return textContentType(neu ? .newPassword : .password)
        #endif
    }
}
