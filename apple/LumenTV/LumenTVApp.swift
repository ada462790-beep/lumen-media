// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

import SwiftUI
import AVKit
import LumenKit

// Apple TV.
//
// Dasselbe LumenKit, eine eigene Oberfläche — und das ist kein Luxus,
// sondern nötig: auf dem Fernseher gibt es keinen Zeiger, sondern einen
// Fokus, der von Element zu Element springt. Alles ist grösser, weil man
// drei Meter entfernt sitzt, und alles Wichtige muss ohne Zielen
// erreichbar sein.

@main
struct LumenTVApp: App {
    @State private var session: Session

    init() {
        // Zuerst die Sprache, die zuletzt galt (siehe `Sprache`).
        Sprache.setzen(Sprache.beimStart, merken: false)
        _session = State(initialValue: Session())
    }

    var body: some Scene {
        WindowGroup {
            TVRootView()
                // Die Sprache des Profils: Text("…") folgt der Locale, und
                // was ausserhalb der Ansichten zu Text wurde, entsteht beim
                // Neuaufbau neu (siehe `Sprache`).
                .id(session.sprache)
                .environment(\.locale, session.locale)
                .environment(session)
                .preferredColorScheme(.dark)
                .task { await session.start() }
        }
    }
}

struct TVRootView: View {
    @Environment(Session.self) private var session

    var body: some View {
        ZStack {
            Theme.background.ignoresSafeArea()

            switch session.stage {
            case .startingUp:
                ProgressView()
            case .needsServer(let message):
                TVServerView(message: message)
            case .choosingServer:
                TVServerListeView()
            case .needsSetup:
                TVMessageView(
                    title: String(localized: "Dieser Server ist noch nicht eingerichtet", bundle: .lumen, locale: .lumen),
                    text: String(localized: "Richte ihn im Browser ein — am Fernseher einen Namen und ein Passwort einzutippen, möchte niemand.", bundle: .lumen, locale: .lumen))
            case .choosingProfile(let profiles):
                // Am Fernseher zuerst der Code: tippen mit der Fernbedienung
                // möchte niemand. Die Profilauswahl bleibt daneben.
                TVAnmeldenView(profiles: profiles)
            case .unlocking(let profile):
                TVUnlockView(profile: profile)
            case .unterwegs:
                // Von unterwegs: keine Profilauswahl. Am Fernseher zuerst
                // der Code — Name und PIN mit der Fernbedienung zu tippen,
                // geht, ist aber mühsam.
                TVUnterwegsView()
            case .signedIn:
                // Die Leiste oben mit allen Bereichen, jeder mit seinem
                // eigenen Stapel. Die Menü-Taste der Fernbedienung führt
                // von selbst zurück.
                TVShell()
                    // Die Verwaltungssperre (Issue #6): meldet der Server
                    // „gesperrt", erscheint hier der Entsperr-Dialog. Die
                    // Sperre selbst richtet man im Browser oder am iPhone
                    // ein — der Fernseher braucht nur den Schlüssel.
                    .sheet(isPresented: Binding(
                        get: { session.adminSperre != nil },
                        set: { if !$0 { session.adminEntsperrenAbbrechen() } })) {
                        AdminUnlockView().environment(session)
                    }
            }
        }
    }
}

// MARK: - Server eintragen

struct TVServerView: View {
    @Environment(Session.self) private var session
    let message: String?
    @State private var address = ""

    var body: some View {
        VStack(spacing: 30) {
            Text("Lumen").font(.system(size: 64, weight: .bold))
            Text("Gib die Adresse deines Servers ein.")
                .font(.title3).foregroundStyle(Theme.label2)

            TextField("192.168.1.20:8097", text: $address)
                .textFieldStyle(.plain)
                .frame(width: 620)

            if let message {
                Text(message).font(.callout).foregroundStyle(Theme.bad)
            }

            HStack(spacing: 40) {
                Button("Verbinden") {
                    Task { await session.useServer(address: address) }
                }
                .disabled(address.isEmpty)

                // Schon Server gespeichert: zurück zur Liste.
                if !session.server.isEmpty {
                    Button("Gespeicherte Server") { session.zurueckZurServerliste() }
                }
            }
        }
        .padding(80)
        .onAppear {
            if address.isEmpty, let vorschlag = session.adressVorschlag { address = vorschlag }
        }
    }
}

struct TVMessageView: View {
    let title: String
    let text: String

    var body: some View {
        VStack(spacing: 20) {
            Text(title).font(.system(size: 44, weight: .bold)).multilineTextAlignment(.center)
            Text(text).font(.title3).foregroundStyle(Theme.label2)
                .multilineTextAlignment(.center).frame(maxWidth: 760)
        }
        .padding(80)
    }
}

// MARK: - Profile

struct TVProfilesView: View {
    @Environment(Session.self) private var session
    let profiles: [ProfileCard]
    /// Zurück zum Code. nil: kein solcher Knopf.
    var mitCode: (() -> Void)? = nil
    @State private var einladung = false

    var body: some View {
        VStack(spacing: 60) {
            Text("Wer schaut?").font(.system(size: 54, weight: .bold))
            HStack(spacing: 60) {
                ForEach(profiles) { profile in
                    Button {
                        Task { await session.chooseProfile(profile) }
                    } label: {
                        VStack(spacing: 18) {
                            ProfilAvatar(name: profile.name, avatar: profile.avatar, accountId: profile.id, size: 160)
                            Text(profile.name).font(.title3)
                        }
                    }
                    .buttonStyle(.card)
                }
            }
            if let error = session.lastError {
                Text(error).foregroundStyle(Theme.bad)
            }

            HStack(spacing: 40) {
                if let mitCode {
                    Button(action: mitCode) {
                        Label("Mit Code anmelden", systemImage: "number")
                    }
                }
                Button {
                    einladung = true
                } label: {
                    Label("Einladung einlösen", systemImage: "envelope.open")
                }
                Button {
                    session.serverWechseln()
                } label: {
                    Label("Server wechseln", systemImage: "server.rack")
                }
            }
        }
        .fullScreenCover(isPresented: $einladung) {
            TVEinladungView().environment(session)
        }
    }
}

struct TVUnlockView: View {
    @Environment(Session.self) private var session
    let profile: ProfileCard
    @State private var secret = ""

    var body: some View {
        VStack(spacing: 30) {
            ProfilAvatar(name: profile.name, avatar: profile.avatar, accountId: profile.id, size: 140)
            Text(profile.name).font(.system(size: 40, weight: .semibold))
            SecureField(profile.lockKind == "pin" ? "PIN" : "Passwort", text: $secret)
                .frame(width: 460)
                .onSubmit { Task { await session.unlock(profile, secret: secret) } }
            if let error = session.lastError {
                Text(error).foregroundStyle(Theme.bad)
            }
            Button("Weiter") { Task { await session.unlock(profile, secret: secret) } }
                .disabled(secret.isEmpty)
        }
    }
}

// MARK: - Startseite

struct TVHomeView: View {
    @Environment(Session.self) private var session
    @State private var rows: [HomeRow] = []
    @State private var reihen: [Reihe] = []
    @State private var listen: [Liste] = []
    @State private var focused: Item?
    @State private var playing: Abspielstart?
    @State private var geladen = false
    @State private var fehler: String?

    var body: some View {
        ZStack(alignment: .topLeading) {
            // Das Bild des gerade fokussierten Titels im Hintergrund — so
            // sieht man schon beim Durchblättern, worum es geht.
            TVBackdrop(item: focused ?? rows.first?.items.first)

            ScrollView {
                VStack(alignment: .leading, spacing: 50) {
                    if let item = focused ?? rows.first?.items.first {
                        VStack(alignment: .leading, spacing: 12) {
                            Text(item.title).font(.system(size: 62, weight: .bold)).lineLimit(2)
                            Text(item.subtitleLine).font(.title3).foregroundStyle(Theme.label2)
                            if let overview = item.overview, !overview.isEmpty {
                                Text(overview).font(.body).foregroundStyle(Theme.label2)
                                    .lineLimit(3).frame(maxWidth: 900, alignment: .leading)
                            }
                        }
                        .padding(.top, 60)
                    }

                    // Ein Klick öffnet die Titelseite; wer gleich schauen
                    // will, drückt lang und wählt „Abspielen".
                    // Wie im Browser: die erste Reihe des Servers, dann die
                    // Filmreihen, dann der Rest.
                    ForEach(rows.prefix(1)) { row in
                        TVRail(title: row.title, items: row.items, focused: $focused, menu: { item in
                            tvKartenMenue(item, session: session) { playing = $0 }
                        })
                    }
                    if !reihen.isEmpty {
                        TVReihenRail(reihen: reihen)
                    }
                    ForEach(rows.dropFirst()) { row in
                        TVRail(title: row.title, items: row.items, focused: $focused, menu: { item in
                            tvKartenMenue(item, session: session) { playing = $0 }
                        })
                    }

                    // Nach den Reihen des Servers: die eigenen und geteilten
                    // Listen. Sie öffnen eine eigene Seite, statt gleich
                    // abzuspielen — eine Liste ist keine einzelne Datei. Die
                    // Listen stehen immer da: dort lässt sich auch eine neue
                    // anlegen (Issue #1).
                    if geladen {
                        TVListenRail(listen: listen)
                    }

                    // Laden, Fehler, leer — jeweils mit etwas, das den
                    // Fokus bekommen kann, sonst stünde man vor einer
                    // leeren Fläche ohne Ausweg.
                    if let fehler, rows.isEmpty {
                        TVFehler(text: fehler) { await laden() }
                            .padding(.top, 80)
                    } else if !geladen {
                        ProgressView().frame(maxWidth: .infinity).padding(.top, 160)
                    } else if rows.isEmpty, reihen.isEmpty {
                        VStack(alignment: .leading, spacing: 16) {
                            Text("Noch nichts da").font(.title2.weight(.semibold))
                            Text("Sobald auf dem Server Filme oder Serien liegen, erscheinen sie hier.")
                                .font(.title3).foregroundStyle(Theme.label2)
                            Button("Nochmal versuchen") { Task { await laden() } }
                        }
                        .padding(.top, 80)
                    }
                }
                .padding(.horizontal, 60)
                .padding(.bottom, 60)
            }
        }
        // Nicht `?? Home(rows: [])`: der Erbauer, den Swift einer Struktur
        // von selbst mitgibt, ist nur innerhalb ihres Moduls sichtbar —
        // auch wenn alle Felder öffentlich sind. Von hier aus, aus einem
        // anderen Ziel, wäre er unerreichbar. Die leere Liste tut es auch.
        .task { await laden() }
        // Zurück von einer Liste oder einer Titelseite: dort kann eine
        // Liste entstanden, umbenannt oder gelöscht worden sein.
        .onAppear {
            if geladen { Task { await ladeListen() } }
        }
        .fullScreenCover(item: $playing) { s in
            TVPlayerView(item: s.item, vonVorn: s.vonVorn).environment(session)
        }
    }

    private func ladeListen() async {
        listen = (try? await session.client.playlists()) ?? []
    }

    private func laden() async {
        fehler = nil
        do {
            // Leere Reihen gar nicht erst zeigen: eine Überschrift ohne
            // Karten ist ein Fokus-Loch.
            rows = try await session.client.home().rows.filter { !$0.items.isEmpty }
        } catch {
            if !APIClient.istAbbruch(error) { fehler = Session.text(error) }
        }
        await ladeListen()
        reihen = (try? await session.client.collections()) ?? []
        geladen = true
        // Der Zähler der Neuigkeiten in der Leiste — wie im Browser
        // nebenbei aufgefrischt.
        await session.refreshMe()
    }
}

struct TVBackdrop: View {
    @Environment(Session.self) private var session
    let item: Item?
    @State private var url: URL?

    var body: some View {
        ZStack {
            Theme.background
            if let url {
                MedienBild(url: url)
                    .ignoresSafeArea()
                    .overlay(LinearGradient(
                        colors: [.black.opacity(0.95), .black.opacity(0.55), .black.opacity(0.9)],
                        startPoint: .leading, endPoint: .trailing))
            }
        }
        .ignoresSafeArea()
        .animation(.easeInOut(duration: 0.4), value: url)
        .task(id: item?.id) {
            guard let item else { url = nil; return }
            url = await session.client.artworkURL(for: item, wide: true)
        }
    }
}

/// Eine Reihe von Karten. Ein Klick öffnet die Titelseite — ausser die
/// Reihe gibt mit `play` etwas anderes vor, etwa „die Liste ab hier
/// spielen". Ein langer Druck auf die Fernbedienung öffnet `menu`, je
/// Titel: dort steht, was sonst einen Umweg bräuchte (Issue #1).
struct TVRail<Menu: View>: View {
    let title: String
    let items: [Item]
    @Binding var focused: Item?
    var play: ((Item) -> Void)? = nil
    @ViewBuilder var menu: (Item) -> Menu
    /// Welche Karte dieser Reihe gerade den Fokus hat — für die bewegte
    /// Vorschau. Anders als `focused` wird das beim Verlassen wieder leer.
    @State private var fokusId: String?

    var body: some View {
        VStack(alignment: .leading, spacing: 18) {
            Text(title).font(.title2.weight(.semibold))
            ScrollView(.horizontal, showsIndicators: false) {
                LazyHStack(spacing: 40) {
                    ForEach(items) { item in
                        // Kein `.focusable()` darum: die Karte ist schon ein
                        // Knopf mit `.card`-Stil, und ein zweiter Fokus-
                        // Halter darüber schluckte den Klick.
                        TVCard(item: item, play: play == nil ? nil : { if let play { play(item) } },
                               vorschauAn: fokusId == item.id,
                               menu: { menu(item) })
                            .onFocusChangeCompat { jetzt in
                                if jetzt {
                                    focused = item
                                    fokusId = item.id
                                } else if fokusId == item.id {
                                    fokusId = nil
                                }
                            }
                    }
                }
                .padding(.vertical, 24)
            }
        }
    }
}

struct TVCard<Menu: View>: View {
    @Environment(Session.self) private var session
    let item: Item
    /// Was ein Klick tut. nil: die Titelseite öffnen.
    var play: (() -> Void)? = nil
    /// Die Karte hat den Fokus: nach einer Sekunde läuft im Bild eine
    /// stumme Vorschau, wie auf dem Apple TV üblich.
    var vorschauAn = false
    @ViewBuilder var menu: () -> Menu
    @State private var url: URL?
    @State private var vorschau: URL?

    var body: some View {
        Group {
            if let play {
                Button(action: play) { karte }
            } else {
                NavigationLink {
                    TVDetailView(itemId: item.id, vorschau: item)
                } label: {
                    karte
                }
            }
        }
        .buttonStyle(.card)
        .contextMenu { menu() }
        .task { url = await session.client.artworkURL(for: item) }
        .task(id: vorschauAn) {
            guard vorschauAn else { vorschau = nil; return }
            try? await Task.sleep(for: .seconds(1))
            if Task.isCancelled { return }
            vorschau = await session.client.vorschauURL(itemId: item.id)
        }
    }

    private var karte: some View {
        VStack(alignment: .leading, spacing: 10) {
            Artwork(url: url, title: item.title, corner: 10)
                .frame(width: 260, height: 390)
                .overlay {
                    if let vorschau {
                        StummeVorschau(url: vorschau)
                            .clipShape(RoundedRectangle(cornerRadius: 10, style: .continuous))
                    }
                }
                .overlay(alignment: .top) { KartenAbzeichen(item: item, gross: true) }
                .overlay(alignment: .bottomTrailing) { if item.isSeen { SeenBadge() } }
                .overlay(alignment: .bottom) {
                    // Bei Serien zählt der Balken die gesehenen Folgen.
                    if balken > 1 && !item.isSeen {
                        ProgressStripe(percent: balken)
                    }
                }
            Text(item.title).font(.body).lineLimit(1).frame(width: 260, alignment: .leading)
            // „4 von 10 Folgen" — nur bei Serien mit Stand, nur für dieses Profil.
            if let stand = item.folgenStand {
                Text(stand).font(.caption).foregroundStyle(Theme.label2)
                    .lineLimit(1).frame(width: 260, alignment: .leading)
            }
        }
    }

    private var balken: Double {
        item.kind == "series" ? max(item.percent, item.folgenAnteil) : item.percent
    }
}

// MARK: - Fokus

extension View {
    /// `onFocusChange` heisst je nach Fassung anders; dieser Umweg hält die
    /// Ansichten frei von Versionsabfragen.
    @ViewBuilder
    func onFocusChangeCompat(_ action: @escaping (Bool) -> Void) -> some View {
        modifier(FocusReporter(action: action))
    }
}

private struct FocusReporter: ViewModifier {
    let action: (Bool) -> Void
    @FocusState private var focused: Bool

    func body(content: Content) -> some View {
        content
            .focused($focused)
            .onChange(of: focused) { _, now in action(now) }
    }
}
