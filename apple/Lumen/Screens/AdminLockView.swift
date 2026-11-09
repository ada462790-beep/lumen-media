// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

import SwiftUI
import LumenKit

/// Die eigene Verwaltungssperre setzen.
///
/// Jede Person mit Zugang zur Verwaltung schützt sie für sich: mit einem
/// PIN, einem Passwort oder einem Code aus einer Authenticator-App. Nur
/// die eigene — niemand setzt die Sperre einer anderen Person, und der
/// Server prüft das ebenfalls.
///
/// Setzen und Aufheben sind selbst Verwaltungs-Anfragen: ist die Sperre
/// gerade zu, erscheint erst der Entsperr-Dialog. Danach bringt der Server
/// ein frisches Token mit, damit die Person nicht gleich aussperrt ist.
struct AdminLockView: View {
    @Environment(Session.self) private var session

    @State private var stand: Verwaltungssperre?
    @State private var wahl = "none"
    @State private var geheimnis = ""
    @State private var wiederholung = ""
    @State private var einrichtung: TOTPEinrichtung?
    @State private var code = ""
    @State private var meldung: String?
    @State private var problem: String?
    @State private var laeuft = false

    var body: some View {
        Form {
            Section {
                LabeledContent("Aktuelle Sperre",
                               value: Verwaltungssperre.name(stand?.sperre ?? "none"))
                if let stand, stand.aktiv {
                    if stand.entsperrt {
                        Button("Jetzt sperren") { Task { await sperren() } }
                    } else {
                        Text("Gesperrt — die nächste Verwaltungs-Anfrage fragt nach dem Code.")
                            .font(.caption).foregroundStyle(Theme.label2)
                    }
                }
                if let meldung {
                    Text(meldung).font(.caption).foregroundStyle(Theme.good)
                }
            } footer: {
                Text("Nach dem Entsperren bleibt die Verwaltung eine Viertelstunde offen, solange du darin arbeitest. Beim Beenden der App ist sie wieder zu.")
            }

            Section("Neue Sperre") {
                Picker("Art", selection: $wahl) {
                    Text("Keine").tag("none")
                    Text("PIN").tag("pin")
                    Text("Passwort").tag("password")
                    Text("Authenticator-App").tag("totp")
                }
                .onChange(of: wahl) { _, _ in
                    geheimnis = ""
                    wiederholung = ""
                    code = ""
                    einrichtung = nil
                }
            }

            switch wahl {
            case "pin":
                geheimnisFelder(titel: "PIN", mindestens: 4, ziffern: true)
            case "password":
                geheimnisFelder(titel: String(localized: "Passwort", bundle: .lumen, locale: .lumen), mindestens: 8, ziffern: false)
            case "totp":
                totpAbschnitt
            default:
                Section {
                    Button("Sperre aufheben", role: .destructive) {
                        Task { await setzen(kind: "none") }
                    }
                    .disabled(laeuft || stand?.aktiv != true)
                } footer: {
                    Text("Ohne Sperre öffnet sich die Verwaltung für dich sofort — wie bisher.")
                }
            }
        }
        .navigationTitle("Verwaltungssperre")
        .problemAlert($problem)
        .task { await laden() }
    }

    // MARK: - PIN und Passwort

    private func geheimnisFelder(titel: String, mindestens: Int, ziffern: Bool) -> some View {
        Section {
            if ziffern {
                SecureField("\(titel) (mindestens \(mindestens) Ziffern)", text: $geheimnis)
                    .ziffern()
                SecureField("\(titel) wiederholen", text: $wiederholung)
                    .ziffern()
            } else {
                SecureField("\(titel) (mindestens \(mindestens) Zeichen)", text: $geheimnis)
                    .feldstil()
                SecureField("\(titel) wiederholen", text: $wiederholung)
                    .feldstil()
            }
            Button("\(titel) setzen") {
                Task { await setzen(kind: wahl, secret: geheimnis) }
            }
            .disabled(laeuft || !geheimnisPasst(mindestens: mindestens, ziffern: ziffern))
        } footer: {
            if !geheimnis.isEmpty, geheimnis != wiederholung {
                Text("Die beiden Eingaben stimmen nicht überein.")
            } else {
                Text("Das ist nicht der Code deines Profils, sondern ein zweiter, nur für die Verwaltung.")
            }
        }
    }

    private func geheimnisPasst(mindestens: Int, ziffern: Bool) -> Bool {
        guard geheimnis.count >= mindestens, geheimnis == wiederholung else { return false }
        return !ziffern || geheimnis.allSatisfy(\.isNumber)
    }

    // MARK: - Authenticator-App

    @ViewBuilder
    private var totpAbschnitt: some View {
        if let einrichtung {
            Section {
                HStack(alignment: .top, spacing: 18) {
                    DatenBild(daten: einrichtung.qrPNG)
                        .frame(width: 180, height: 180)
                        .background(Color.white)
                        .clipShape(RoundedRectangle(cornerRadius: 8, style: .continuous))
                    VStack(alignment: .leading, spacing: 8) {
                        Text("Mit der App abfotografieren — oder das Geheimnis abtippen:")
                            .font(.caption).foregroundStyle(Theme.label2)
                        Text(gruppiert(einrichtung.geheimnis))
                            .font(.system(.callout, design: .monospaced))
                            .textSelection(.enabled)
                        Text("Geht mit \(einrichtung.app)")
                            .font(.caption).foregroundStyle(Theme.label3)
                    }
                }
                .padding(.vertical, 6)
            } header: {
                Text("1. In der App hinzufügen")
            }
            Section {
                TextField("Sechsstelliger Code", text: $code)
                    .ziffern()
                    .einmalcode()
                Button("Bestätigen und einschalten") {
                    Task { await setzen(kind: "totp", code: code) }
                }
                .disabled(laeuft || !(code.count == 6 && code.allSatisfy(\.isNumber)))
            } header: {
                Text("2. Code aus der App eingeben")
            } footer: {
                Text("Erst wenn ein Code stimmt, gilt die Sperre. Stimmt die Uhrzeit auf dem Handy nicht, stimmt auch kein Code.")
            }
        } else {
            Section {
                Button("Authenticator-App einrichten") { Task { await totpBeginnen() } }
                    .disabled(laeuft)
            } footer: {
                Text("Lumen zeigt dir einen QR-Code für Google Authenticator, Aegis, 1Password und ähnliche Apps. Kein E-Mail, kein SMS — der Code entsteht auf deinem Handy.")
            }
        }
    }

    /// „JBSW Y3DP EHPK 3PXP" liest sich leichter ab als eine Zeile ohne Pause.
    private func gruppiert(_ geheimnis: String) -> String {
        var out = ""
        for (i, zeichen) in geheimnis.enumerated() {
            if i > 0, i % 4 == 0 { out.append(" ") }
            out.append(zeichen)
        }
        return out
    }

    // MARK: - Laden und Ändern

    private func laden() async {
        do {
            let s = try await session.client.adminLock()
            stand = s
            wahl = s.sperre.isEmpty ? "none" : s.sperre
        } catch {
            problem = Session.text(error)
        }
    }

    private func totpBeginnen() async {
        laeuft = true
        do {
            einrichtung = try await session.verwaltet { try await session.client.adminLockTOTPBegin() }
        } catch {
            problem = Session.text(error)
        }
        laeuft = false
    }

    private func setzen(kind: String, secret: String = "", code: String = "") async {
        laeuft = true
        meldung = nil
        do {
            _ = try await session.verwaltet {
                try await session.client.adminSetLock(kind: kind, secret: secret, code: code)
            }
            meldung = kind == "none"
                ? String(localized: "Die Sperre ist aufgehoben.", bundle: .lumen, locale: .lumen)
                : String(localized: "Die Sperre ist gesetzt: \(Verwaltungssperre.name(kind)).", bundle: .lumen, locale: .lumen)
            geheimnis = ""
            wiederholung = ""
            self.code = ""
            einrichtung = nil
            // Das Konto trägt die Sperre (`adminLock`) — neu laden, damit
            // die Einstellungen sie richtig nennen.
            await session.refreshMe()
            await laden()
        } catch {
            problem = Session.text(error)
        }
        laeuft = false
    }

    private func sperren() async {
        await session.adminSperren()
        meldung = String(localized: "Die Verwaltung ist wieder gesperrt.", bundle: .lumen, locale: .lumen)
        await laden()
    }
}
