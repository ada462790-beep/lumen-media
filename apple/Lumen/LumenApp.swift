// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

import SwiftUI
import LumenKit

@main
struct LumenApp: App {
    @State private var session: Session

    init() {
        // Zuerst die Sprache, die zuletzt galt — so spricht schon der erste
        // Bildschirm sie, bevor der Server geantwortet hat.
        Sprache.setzen(Sprache.beimStart, merken: false)
        _session = State(initialValue: Session())
    }
    /// Hell, dunkel oder wie das Gerät — wie im Browser gilt die Wahl für
    /// dieses Gerät, nicht fürs Konto.
    @AppStorage(Darstellung.schluessel) private var darstellung = Darstellung.dunkel.rawValue

    var body: some Scene {
        WindowGroup {
            RootView()
                // Die Sprache des Profils: Text("…") folgt der Locale, und
                // was ausserhalb der Ansichten zu Text wurde („3 neue
                // Folgen"), entsteht beim Neuaufbau in der neuen Sprache.
                // Die `.task` darunter hängt nicht an dieser Kennung und
                // läuft darum nicht noch einmal.
                .id(session.sprache)
                .environment(\.locale, session.locale)
                .environment(session)
                .preferredColorScheme((Darstellung(rawValue: darstellung) ?? .dunkel).schema)
                .tint(Theme.accent)
                .task {
                    #if !os(tvOS)
                    // Die Hintergrund-Downloads wieder aufnehmen: was fertig
                    // wurde, während die App ruhte, liefert das System jetzt ab.
                    Downloads.shared.vorbereiten()
                    #endif
                    await session.start()
                }
                // Ein Einladungslink öffnet die App und füllt den Code aus.
                .onOpenURL { url in
                    if let code = Self.inviteCode(in: url) {
                        session.pendingInvite = code
                    }
                }
        }
        #if os(macOS)
        .defaultSize(width: 1280, height: 820)
        .commands { LumenBefehle() }
        #endif

        #if os(macOS)
        // Der Player in einem eigenen Fenster: verschiebbar, in den
        // Vollbildmodus zu schalten (grüner Knopf oder ⌃⌘F), und die
        // Mediathek bleibt daneben offen.
        WindowGroup("Wiedergabe", id: playerFensterKennung, for: PlayerAuftrag.self) { $auftrag in
            if let auftrag {
                PlayerView(auftrag: auftrag)
                    .environment(\.locale, session.locale)
                    .environment(session)
                    .tint(Theme.accent)
                    .frame(minWidth: 640, minHeight: 360)
            }
        }
        .defaultSize(width: 1280, height: 720)
        .windowResizability(.contentMinSize)

        // ⌘, öffnet die Einstellungen wie in jeder Mac-App: dieselbe
        // Ansicht wie „Profil" in der Seitenleiste.
        Settings {
            EinstellungenFenster()
                .id(session.sprache)
                .environment(\.locale, session.locale)
                .environment(session)
                .preferredColorScheme((Darstellung(rawValue: darstellung) ?? .dunkel).schema)
                .tint(Theme.accent)
        }
        #endif
    }

    /// `lumen://einladung/ABC` oder `https://…/?einladung=ABC`
    static func inviteCode(in url: URL) -> String? {
        if url.scheme == "lumen", url.host == "einladung" {
            let code = url.lastPathComponent
            return code.isEmpty ? nil : code
        }
        return URLComponents(url: url, resolvingAgainstBaseURL: false)?
            .queryItems?.first { $0.name == "einladung" }?.value
    }
}

#if os(macOS)
/// Die Einstellungen am Mac (⌘,). Ohne Anmeldung gibt es nichts
/// einzustellen — das Profil gehört zur Anmeldung.
struct EinstellungenFenster: View {
    @Environment(Session.self) private var session

    var body: some View {
        Group {
            if session.me != nil {
                NavigationStack { SettingsView() }
            } else {
                ContentUnavailableView("Nicht angemeldet", systemImage: "person.crop.circle",
                    description: Text("Wähle zuerst im Hauptfenster ein Profil."))
            }
        }
        .frame(minWidth: 520, idealWidth: 560, minHeight: 560, idealHeight: 720)
    }
}
#endif

/// Wie die App aussieht. Voreingestellt bleibt dunkel — so sieht ein Kino
/// aus, und so kennen es alle, die Lumen schon haben (app.js: darstellung).
enum Darstellung: String, CaseIterable, Identifiable {
    case dunkel, hell, auto

    static let schluessel = "lumen.darstellung"

    var id: String { rawValue }

    var name: String {
        switch self {
        case .dunkel: String(localized: "Dunkel", bundle: .lumen, locale: .lumen)
        case .hell: String(localized: "Hell", bundle: .lumen, locale: .lumen)
        case .auto: String(localized: "Wie das Gerät", bundle: .lumen, locale: .lumen)
        }
    }

    /// nil heisst: dem Gerät folgen.
    var schema: ColorScheme? {
        switch self {
        case .dunkel: .dark
        case .hell: .light
        case .auto: nil
        }
    }
}

/// Entscheidet, was zu sehen ist: Server eintragen, einrichten, Profil
/// wählen, oder die Mediathek.
struct RootView: View {
    @Environment(Session.self) private var session

    var body: some View {
        ZStack {
            Theme.background.ignoresSafeArea()

            switch session.stage {
            case .startingUp:
                ProgressView().controlSize(.large)

            case .needsServer(let message):
                ServerAddressView(message: message)

            case .choosingServer:
                ServerListeView()

            case .needsSetup:
                FirstRunView()

            case .choosingProfile(let profiles):
                ProfilesView(profiles: profiles)

            case .unlocking(let profile):
                UnlockView(profile: profile)

            case .unterwegs:
                UnterwegsAnmeldenView()

            case .signedIn:
                LibraryShell()
            }
        }
        .animation(.easeInOut(duration: 0.25), value: session.stage)
    }
}
