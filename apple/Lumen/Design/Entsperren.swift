// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

import SwiftUI
import LumenKit
#if canImport(UIKit)
import UIKit
#elseif canImport(AppKit)
import AppKit
#endif

// Die Verwaltungssperre (GitHub-Issue #6), soweit sie auf allen Geräten
// gleich aussieht. Diese Datei liegt in Design/, weil project.yml genau
// diesen Ordner in beide Ziele einbindet — iPhone, iPad, Mac und Apple TV
// zeigen denselben Dialog, nur in anderer Grösse.

/// Der Entsperr-Dialog. Erscheint, wenn der Server auf eine
/// Verwaltungs-Anfrage mit „gesperrt" (423) geantwortet hat; welche Sperre
/// die Person hat, steht in `session.adminSperre` und entscheidet über das
/// Eingabefeld: Ziffern für den PIN, ein verdecktes Feld fürs Passwort,
/// sechs Ziffern aus der Authenticator-App.
///
/// Stimmt der Code, schliesst sich der Dialog von selbst, und die Anfrage,
/// die auf ihn gewartet hat, läuft weiter. „Abbrechen" lässt sie scheitern.
struct AdminUnlockView: View {
    @Environment(Session.self) private var session
    @State private var code = ""
    @State private var fehler: String?
    @State private var laeuft = false

    private var art: String { session.adminSperre ?? "password" }

    #if os(tvOS)
    private let breite: CGFloat = 520
    #else
    private let breite: CGFloat = 320
    #endif

    var body: some View {
        VStack(spacing: 22) {
            Image(systemName: "lock.shield")
                .font(.lumen(.largeTitle, tv: 44))
                .foregroundStyle(Theme.label2)
                .padding(.top, 8)
            Text("Verwaltung entsperren")
                .font(.title2.weight(.semibold))
            Text(hinweis)
                .font(.callout)
                .foregroundStyle(Theme.label2)
                .multilineTextAlignment(.center)
                .frame(maxWidth: breite + 160)

            feld
                .frame(width: breite)
                .disabled(laeuft)

            if let fehler {
                Text(fehler)
                    .font(.callout)
                    .foregroundStyle(Theme.bad)
                    .multilineTextAlignment(.center)
            }

            Button {
                Task { await entsperren() }
            } label: {
                if laeuft {
                    ProgressView()
                } else {
                    Text("Entsperren")
                }
            }
            .hauptknopf()
            .disabled(!bereit || laeuft)

            Button("Abbrechen") { session.adminEntsperrenAbbrechen() }
                .nebenknopf()
        }
        .padding(40)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(Theme.background)
    }

    /// Das Eingabefeld, passend zur Sperre.
    @ViewBuilder
    private var feld: some View {
        switch art {
        case "pin":
            SecureField("PIN", text: $code)
                .onSubmit { Task { await entsperren() } }
                .ziffern()
        case "totp":
            TextField("Sechsstelliger Code", text: $code)
                .onSubmit { Task { await entsperren() } }
                .ziffern()
                .einmalcode()
        default:
            SecureField("Passwort", text: $code)
                .onSubmit { Task { await entsperren() } }
                .feldstil()
        }
    }

    private var hinweis: String {
        switch art {
        case "pin": String(localized: "Gib den PIN deiner Verwaltungssperre ein.", bundle: .lumen, locale: .lumen)
        case "totp": String(localized: "Gib den sechsstelligen Code aus deiner Authenticator-App ein.", bundle: .lumen, locale: .lumen)
        default: String(localized: "Gib das Passwort deiner Verwaltungssperre ein.", bundle: .lumen, locale: .lumen)
        }
    }

    /// Nicht schon bei einem Zeichen abschicken: ein PIN hat mindestens
    /// vier Ziffern, ein Code genau sechs. Das spart Fehlversuche — und
    /// davon erlaubt der Server nur fünf in fünf Minuten.
    private var bereit: Bool {
        let t = code.trimmingCharacters(in: .whitespaces)
        switch art {
        case "pin": return t.count >= 4
        case "totp": return t.count == 6 && t.allSatisfy(\.isNumber)
        default: return !t.isEmpty
        }
    }

    private func entsperren() async {
        guard bereit, !laeuft else { return }
        laeuft = true
        fehler = await session.adminEntsperren(code: code.trimmingCharacters(in: .whitespaces))
        laeuft = false
        // Ein Code aus der App gilt nur eine halbe Minute — nach einem
        // Fehlversuch ist ohnehin ein neuer fällig.
        if fehler != nil, art == "totp" { code = "" }
    }
}

// MARK: - Bestätigung

/// „Rechte ändern": Gruppen anlegen, ändern, auflösen, jemanden einer
/// Gruppe zuordnen oder seine Grenzen ändern verlangt PIN, Passwort oder
/// Code noch einmal — auch wenn die Verwaltung gerade offen ist. Wie der
/// Dialog im Browser (app.js: api.bestaetigen).
///
/// Erscheint, wenn der Server „bestaetigen" geantwortet hat. „Bestätigen"
/// schickt die wartende Anfrage mit dem Code noch einmal; stimmt er nicht,
/// kommt der Dialog wieder, mit „Das stimmt nicht".
struct BestaetigenView: View {
    @Environment(Session.self) private var session
    @State private var code = ""

    private var bereit: Bool { !code.trimmingCharacters(in: .whitespaces).isEmpty }

    var body: some View {
        VStack(spacing: 18) {
            Image(systemName: "person.badge.key")
                .font(.lumen(.largeTitle, tv: 44))
                .foregroundStyle(Theme.label2)
                .padding(.top, 6)
            Text("Rechte ändern")
                .font(.title2.weight(.semibold))
            Text(session.bestaetigungFalsch
                 ? String(localized: "Das stimmt nicht. Noch einmal:", bundle: .lumen, locale: .lumen)
                 : String(localized: "Zur Sicherheit noch einmal deinen PIN, dein Passwort oder den Code aus der Authenticator-App.", bundle: .lumen, locale: .lumen))
                .font(.callout)
                .foregroundStyle(session.bestaetigungFalsch ? Theme.bad : Theme.label2)
                .multilineTextAlignment(.center)
                .frame(maxWidth: 380)

            SecureField("PIN, Passwort oder Code", text: $code)
                .onSubmit { senden() }
                .feldstil()
                .frame(width: 300)
                .disabled(session.bestaetigungPrueft)

            HStack(spacing: 12) {
                Button("Abbrechen") { session.bestaetigenAbbrechen() }
                    .nebenknopf()
                Button("Bestätigen") { senden() }
                    .hauptknopf()
                    .disabled(!bereit || session.bestaetigungPrueft)
                if session.bestaetigungPrueft {
                    ProgressView()
                }
            }
        }
        .padding(36)
        .frame(minWidth: 420)
        .background(Theme.background)
        // Ein abgelehnter Versuch: das Feld leer, damit niemand im alten
        // Code weitertippt.
        .onChange(of: session.bestaetigungVersuch) { _, _ in code = "" }
        // Wegwischen heisst abbrechen — die wartende Anfrage darf nie hängen.
        .interactiveDismissDisabled(session.bestaetigungPrueft)
    }

    private func senden() {
        guard bereit, !session.bestaetigungPrueft else { return }
        session.bestaetigen(code: code.trimmingCharacters(in: .whitespaces))
    }
}

// MARK: - Felder

extension View {
    /// Das gemeinsame Aussehen der Eingabefelder. Den runden Rahmen gibt
    /// es auf dem Fernseher nicht — dort öffnet ein Feld ohnehin die
    /// grosse Tastatur.
    func feldstil() -> some View {
        #if os(tvOS)
        return textFieldStyle(.plain)
        #else
        return textFieldStyle(.roundedBorder)
        #endif
    }

    /// Nur Ziffern: die Zifferntastatur, wo es eine gibt.
    func ziffern() -> some View {
        #if os(macOS)
        return feldstil()
        #else
        return feldstil().keyboardType(.numberPad)
        #endif
    }

    /// Ein Einmalcode — iOS bietet dann an, ihn aus der Zwischenablage
    /// oder einer Authenticator-App zu übernehmen.
    func einmalcode() -> some View {
        #if os(macOS)
        return self
        #else
        return textContentType(.oneTimeCode)
        #endif
    }

    /// Die Hauptaktion eines Dialogs. Auf dem Fernseher bleibt es beim
    /// Stil des Systems: nur der zeigt, welcher Knopf den Fokus hat.
    func hauptknopf() -> some View {
        #if os(tvOS)
        return self
        #else
        return buttonStyle(PrimaryButtonStyle())
        #endif
    }

    func nebenknopf() -> some View {
        #if os(tvOS)
        return self
        #else
        return buttonStyle(QuietButtonStyle())
        #endif
    }
}

// MARK: - Bild aus Daten

/// Ein Bild, das als Daten vorliegt — der QR-Code zur Authenticator-App
/// kommt so vom Server, als PNG in einer `data:`-Adresse.
///
/// Ohne Glättung: ein QR-Code besteht aus scharfen Kästchen, und ein
/// weichgezeichneter liest sich schlechter.
struct DatenBild: View {
    let daten: Data?

    var body: some View {
        if let daten, let bild = Self.bild(daten) {
            bild
                .resizable()
                .interpolation(.none)
                .scaledToFit()
        } else {
            RoundedRectangle(cornerRadius: 8, style: .continuous)
                .fill(Theme.surface)
                .overlay(
                    Text("Kein Bild")
                        .font(.caption)
                        .foregroundStyle(Theme.label3))
        }
    }

    static func bild(_ daten: Data) -> Image? {
        #if canImport(UIKit)
        guard let ui = UIImage(data: daten) else { return nil }
        return Image(uiImage: ui)
        #elseif canImport(AppKit)
        guard let ns = NSImage(data: daten) else { return nil }
        return Image(nsImage: ns)
        #else
        return nil
        #endif
    }
}

// MARK: - Wo es die Verwaltung gibt

extension Session {
    /// Ob dieses Gerät die Verwaltung anbietet. Vorgabe des Besitzers: nur
    /// im Browser und in der Mac-App — iPhone, iPad und Fernseher sind zum
    /// Schauen da. Was der eigenen Person gehört (Geräte, eigene
    /// Wiedergaben, Einstellungen), bleibt überall.
    var verwaltungHier: Bool {
        #if os(macOS)
        return isAdmin
        #else
        return false
        #endif
    }
}
