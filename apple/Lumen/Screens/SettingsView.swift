// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

import SwiftUI
import PhotosUI
import LumenKit
#if canImport(UIKit)
import UIKit
#elseif canImport(AppKit)
import AppKit
#endif

/// Profil, Privatsphäre, Geräte.
struct SettingsView: View {
    @Environment(Session.self) private var session
    @State private var devices: [Device] = []
    /// Was gerade in diesem Profil läuft — auf welchem Gerät, nicht was.
    @State private var wiedergaben: [EigeneWiedergabe] = []
    @State private var confirmSignOutAll = false
    @State private var confirmClearHistory = false
    @State private var fotoWahl: PhotosPickerItem?
    @State private var bildMeldung: String?
    @State private var laedtBild = false
    @State private var geraeteFehler: String?
    @AppStorage(Darstellung.schluessel) private var darstellung = Darstellung.dunkel.rawValue
    @State private var spracheFehler: String?

    private var account: Account? { session.me?.account }

    /// Die Sprache des Profils: "" heisst „wie der Server". Die Wahl geht
    /// sofort an den Server; die App baut sich danach in ihr neu auf.
    private var appSprache: Binding<String> {
        Binding(
            get: { AppSprache.gewaehlt(session) },
            set: { neu in
                Task { spracheFehler = await session.spracheWaehlen(neu.isEmpty ? nil : neu) }
            })
    }
    private var prefs: [String: String] { session.me?.prefs ?? [:] }

    var body: some View {
        List {
            if let account {
                Section {
                    HStack(spacing: 14) {
                        ProfilAvatar(name: account.name, avatar: account.avatar, accountId: account.id, size: 54)
                        VStack(alignment: .leading, spacing: 2) {
                            Text(account.name).font(.title3.weight(.semibold))
                            Text(role(account)).font(.caption).foregroundStyle(Theme.label2)
                        }
                    }
                    .padding(.vertical, 6)
                    NavigationLink { ProfilSchutzView() } label: {
                        LabeledContent {
                            Text(Schutzart(account.lockKind).name)
                        } label: {
                            Label("Profil schützen", systemImage: "lock")
                        }
                    }
                }
            }

            Section("Wiedergabe") {
                Toggle("Folgen automatisch weiter", isOn: pref("autoplay", default: true))
                Toggle("Vorspann überspringen", isOn: pref("skipIntro", default: false))
                if let limits = session.me?.limits {
                    LabeledContent("Qualität unterwegs", value: limits.remoteQuality)
                    LabeledContent("Gleichzeitig schauen", value: String(localized: "\(limits.maxStreams) Geräte", bundle: .lumen, locale: .lumen))
                }
            }

            Section {
                Picker("Ton", selection: sprache(Sprachwahl.prefTon, leer: "")) {
                    ForEach(Sprachwahl.ton) { w in Text(w.name).tag(w.wert) }
                }
                Picker("Untertitel", selection: sprache(Sprachwahl.prefUntertitel, leer: "aus")) {
                    ForEach(Sprachwahl.untertitel) { w in Text(w.name).tag(w.wert) }
                }
            } header: {
                Text("Sprachen")
            } footer: {
                Text("Was beim Start vorgewählt ist. Wählst du im Player etwas anderes, merkt sich Lumen das — im Browser und in allen Apps.")
            }

            if let account {
                profilbildSection(account)
            }

            Section {
                Picker("Grösse", selection: untertitelGroesse) {
                    ForEach(UntertitelStil.Groesse.allCases, id: \.self) { g in
                        Text(g.name).tag(g)
                    }
                }
                .pickerStyle(.segmented)
                Toggle("Dunkler Hintergrund hinter dem Text",
                       isOn: pref(UntertitelStil.prefHintergrund, default: true))
            } header: {
                Text("Untertitel")
            } footer: {
                Text("Gilt im Browser und in den Apps.")
            }

            Section {
                Picker("Sprache", selection: appSprache) {
                    ForEach(AppSprache.optionen(session)) { w in Text(verbatim: w.name).tag(w.wert) }
                }
                if let spracheFehler {
                    Text(spracheFehler).font(.caption).foregroundStyle(Theme.bad)
                }
            } header: {
                Text("Sprache")
            } footer: {
                Text("Gilt für dein Profil — in allen Apps und im Browser.")
            }

            Section {
                Picker("Darstellung", selection: $darstellung) {
                    ForEach(Darstellung.allCases) { d in Text(d.name).tag(d.rawValue) }
                }
                .pickerStyle(.segmented)
            } header: {
                Text("Darstellung")
            } footer: {
                Text("Gilt für dieses Gerät.")
            }

            Section {
                Toggle("Bewertungen im Haushalt zeigen", isOn: pref("shareRatings", default: false))
            } header: {
                Text("Privatsphäre")
            } footer: {
                Text("Dein Verlauf und deine Bewertungen gehören dir. Auch der Besitzer des Servers kann nicht sehen, wann oder wie lange du etwas schaust.")
            }

            Section("Verlauf") {
                Button("Verlauf löschen", role: .destructive) { confirmClearHistory = true }
            }

            // Für Besitzer und Mitverwalter — nur in der Mac-App:
            // Die Verwaltung gibt es nur dort und im Browser.
            if session.verwaltungHier {
                Section {
                    NavigationLink { AdminView() } label: {
                        Label("Verwaltung", systemImage: "gearshape")
                    }
                    NavigationLink { AdminLockView() } label: {
                        LabeledContent {
                            Text(Verwaltungssperre.name(account?.adminLock ?? "none"))
                        } label: {
                            Label("Verwaltungssperre", systemImage: "lock.shield")
                        }
                    }
                } header: {
                    Text("Verwaltung")
                } footer: {
                    Text("Auch in der Verwaltung zeigt Lumen nicht, wer was schaut.")
                }
            }

            // Ein Fernseher, der noch läuft, obwohl niemand mehr davor sitzt:
            // hier lässt er sich beenden. Nur, wenn überhaupt etwas läuft.
            if !wiedergaben.isEmpty {
                Section {
                    ForEach(wiedergaben) { w in
                        HStack(spacing: 12) {
                            VStack(alignment: .leading, spacing: 3) {
                                Text(w.deviceName)
                                Text(w.zeile).font(.caption).foregroundStyle(Theme.label2)
                            }
                            Spacer()
                            Button("Beenden", role: .destructive) {
                                Task { await wiedergabeBeenden(w) }
                            }
                            .buttonStyle(QuietButtonStyle())
                        }
                    }
                } header: {
                    Text("Läuft gerade in deinem Profil")
                }
            }

            Section {
                ForEach(devices, id: \.self) { device in
                    LabeledContent(device.name, value: GeraeteText.art(device.kind))
                }
                if let geraeteFehler {
                    Text(geraeteFehler).font(.caption).foregroundStyle(Theme.bad)
                }
                Button("Überall abmelden", role: .destructive) { confirmSignOutAll = true }
            } header: {
                Text("Geräte")
            }

            // Einen Fernseher mit dem eigenen Profil anmelden: er zeigt
            // einen Code, hier wird er eingegeben.
            Section {
                NavigationLink { FernseherAnmeldenView() } label: {
                    Label("Fernseher anmelden", systemImage: "tv")
                }
            } footer: {
                Text("Zeigt ein Fernseher einen Code, meldest du ihn hier mit deinem Profil an.")
            }

            Section {
                Button("Profil wechseln") { Task { await session.signOut() } }
            }

            Section {
                if let aktiv = session.aktiverServer {
                    LabeledContent("Verbunden mit", value: aktiv.name)
                }
                Button("Server wechseln") { session.serverWechseln() }
            } header: {
                Text("Server")
            } footer: {
                Text("Du bleibst auf jedem Server angemeldet und kannst jederzeit zurückwechseln.")
            }

            Section {
                LabeledContent("Fassung", value: session.serverVersion)
                if !session.license.isEmpty {
                    LabeledContent("Lizenz", value: session.license)
                }
                if let src = session.sourceURL {
                    Link("Quelltext ansehen", destination: src)
                }
            } header: {
                Text("Über Lumen")
            } footer: {
                Text(String(localized: "Lumen ist freie Software. Du darfst sie benutzen, verändern und weitergeben — wer sie verändert und betreibt, muss seine Änderungen ebenfalls offenlegen.", bundle: .lumen, locale: .lumen)
                     + (session.hasFFmpeg ? "" : "\n\n" + String(localized: "FFmpeg fehlt auf dem Server, darum kann er nicht umwandeln.", bundle: .lumen, locale: .lumen)))
            }
        }
        .navigationTitle("Profil")
        .task {
            do {
                devices = try await session.client.devices()
                geraeteFehler = nil
            } catch {
                if !APIClient.istAbbruch(error) { geraeteFehler = Session.text(error) }
            }
            await wiedergabenLaden()
        }
        .confirmationDialog("Verlauf löschen?", isPresented: $confirmClearHistory) {
            Button("Löschen", role: .destructive) {
                Task { try? await session.client.clearHistory() }
            }
        } message: {
            Text("Alle Fortschritte und Haken werden entfernt. Die Dateien bleiben.")
        }
        .confirmationDialog("Überall abmelden?", isPresented: $confirmSignOutAll) {
            Button("Abmelden", role: .destructive) {
                Task {
                    do {
                        try await session.client.signOutEverywhere()
                        await session.signOut()
                    } catch {
                        // Hat der Server es nicht getan, gelten die anderen
                        // Geräte noch — das soll man sehen, statt zu glauben,
                        // es sei erledigt.
                        geraeteFehler = Session.text(error)
                    }
                }
            }
        } message: {
            Text("Alle Geräte müssen sich neu anmelden — dieses auch.")
        }
    }

    // MARK: - Profilbild

    /// Eine Farbe, eine Figur oder ein eigenes Bild — wie im Browser. Das
    /// Bild verkleinert der Server auf 256 Pixel.
    @ViewBuilder
    private func profilbildSection(_ account: Account) -> some View {
        Section {
            LazyVGrid(columns: [GridItem(.adaptive(minimum: 46, maximum: 54), spacing: 10)], spacing: 10) {
                ForEach(Profilbild.farben, id: \.self) { farbe in
                    wahlKnopf(farbe, gewaehlt: account.avatar == farbe, name: String(localized: "Farbe \(String(farbe.dropFirst(3)))", bundle: .lumen, locale: .lumen)) {
                        Avatar(initial: account.initial, colorKey: farbe, size: 44)
                    }
                }
                ForEach(Figuren.geordnet, id: \.name) { figur in
                    wahlKnopf("fig-" + figur.name, gewaehlt: account.avatar == "fig-" + figur.name,
                              name: figur.name.capitalized) {
                        FigurBild(figur: figur, size: 44)
                    }
                }
                PhotosPicker(selection: $fotoWahl, matching: .images) {
                    ZStack {
                        if account.avatar == "bild" {
                            ProfilAvatar(name: account.name, avatar: "bild", accountId: account.id, size: 44)
                        } else {
                            Circle().fill(Theme.fill).frame(width: 44, height: 44)
                            Image(systemName: laedtBild ? "hourglass" : "plus")
                                .font(.body.weight(.semibold))
                                .foregroundStyle(Theme.label2)
                        }
                    }
                    .overlay(Circle().strokeBorder(account.avatar == "bild" ? Theme.accent : .clear, lineWidth: 2.5)
                        .frame(width: 50, height: 50))
                    .frame(width: 50, height: 50)
                }
                .buttonStyle(.plain)
                .accessibilityLabel("Eigenes Bild hochladen")
                .disabled(laedtBild)
            }
            .padding(.vertical, 6)
            if let bildMeldung {
                Text(bildMeldung).font(.caption).foregroundStyle(Theme.bad)
            }
        } header: {
            Text("Profilbild")
        } footer: {
            Text("Eine Farbe, eine Figur oder ein eigenes Bild — Lumen verkleinert es auf 256 Pixel.")
        }
        .onChange(of: fotoWahl) { _, neu in
            guard let neu else { return }
            Task { await hochladen(neu) }
        }
    }

    private func wahlKnopf<Inhalt: View>(_ wert: String, gewaehlt: Bool, name: String,
                                         @ViewBuilder inhalt: () -> Inhalt) -> some View {
        Button {
            Task {
                bildMeldung = await session.avatarSetzen(wert)
            }
        } label: {
            inhalt()
                .overlay(Circle().strokeBorder(gewaehlt ? Theme.accent : .clear, lineWidth: 2.5)
                    .frame(width: 50, height: 50))
                .frame(width: 50, height: 50)
        }
        .buttonStyle(.plain)
        .accessibilityLabel(name)
        .accessibilityAddTraits(gewaehlt ? .isSelected : [])
    }

    /// Das gewählte Foto als JPEG hochladen. Fotos vom iPhone sind oft HEIC,
    /// das der Server nicht liest — darum hier umrechnen, und gleich auf
    /// höchstens 1024 Pixel, damit nicht ein 12-MB-Foto übers Netz geht.
    private func hochladen(_ auswahl: PhotosPickerItem) async {
        laedtBild = true
        defer { laedtBild = false; fotoWahl = nil }
        guard let roh = try? await auswahl.loadTransferable(type: Data.self),
              let jpeg = Self.alsJPEG(roh, kante: 1024) else {
            bildMeldung = String(localized: "Dieses Bild liess sich nicht lesen.", bundle: .lumen, locale: .lumen)
            return
        }
        bildMeldung = await session.avatarHochladen(jpeg)
    }

    /// Ein Bild beliebigen Formats als JPEG, die längere Seite höchstens `kante`.
    static func alsJPEG(_ daten: Data, kante: CGFloat) -> Data? {
        #if canImport(UIKit)
        guard let bild = UIImage(data: daten) else { return nil }
        let gross = max(bild.size.width, bild.size.height)
        let faktor = gross > kante ? kante / gross : 1
        let ziel = CGSize(width: (bild.size.width * faktor).rounded(), height: (bild.size.height * faktor).rounded())
        let format = UIGraphicsImageRendererFormat()
        format.scale = 1
        let klein = UIGraphicsImageRenderer(size: ziel, format: format).image { _ in
            bild.draw(in: CGRect(origin: .zero, size: ziel))
        }
        return klein.jpegData(compressionQuality: 0.88)
        #elseif canImport(AppKit)
        var gelesen = NSBitmapImageRep(data: daten)
        if gelesen == nil, let tiff = NSImage(data: daten)?.tiffRepresentation {
            gelesen = NSBitmapImageRep(data: tiff)
        }
        guard let quelle = gelesen else { return nil }
        let breite = CGFloat(quelle.pixelsWide), hoehe = CGFloat(quelle.pixelsHigh)
        guard breite > 0, hoehe > 0 else { return nil }
        let faktor = max(breite, hoehe) > kante ? kante / max(breite, hoehe) : 1
        let zb = Int((breite * faktor).rounded()), zh = Int((hoehe * faktor).rounded())
        guard let ziel = NSBitmapImageRep(bitmapDataPlanes: nil, pixelsWide: zb, pixelsHigh: zh,
                                          bitsPerSample: 8, samplesPerPixel: 4, hasAlpha: true,
                                          isPlanar: false, colorSpaceName: .deviceRGB,
                                          bytesPerRow: 0, bitsPerPixel: 0) else { return nil }
        NSGraphicsContext.saveGraphicsState()
        NSGraphicsContext.current = NSGraphicsContext(bitmapImageRep: ziel)
        _ = quelle.draw(in: NSRect(x: 0, y: 0, width: zb, height: zh))
        NSGraphicsContext.restoreGraphicsState()
        return ziel.representation(using: .jpeg, properties: [.compressionFactor: 0.88])
        #else
        return nil
        #endif
    }

    /// Die Grösse der Untertitel — „klein", „normal" oder „gross" als Pref.
    private var untertitelGroesse: Binding<UntertitelStil.Groesse> {
        Binding(
            get: { UntertitelStil(prefs: prefs).groesse },
            set: { neu in
                Task {
                    _ = try? await session.client.updatePrefs([UntertitelStil.prefGroesse: neu.rawValue])
                    await session.refreshMe()
                }
            })
    }

    private func wiedergabenLaden() async {
        wiedergaben = (try? await session.client.eigeneWiedergaben()) ?? []
    }

    /// Läuft sie schon nicht mehr (404), ist das Ziel auch erreicht — in
    /// jedem Fall danach neu laden.
    private func wiedergabeBeenden(_ w: EigeneWiedergabe) async {
        try? await session.client.eigeneWiedergabeBeenden(w.id)
        await wiedergabenLaden()
    }

    private func role(_ account: Account) -> String {
        if account.isOwner { return String(localized: "Besitzer dieses Servers", bundle: .lumen, locale: .lumen) }
        // Die Gruppe steht dabei, wenn es eine gibt — sie entscheidet über
        // Bibliotheken, Grenzen und ob die Person mitverwaltet.
        let gruppe = account.groupName.map { " · " + String(localized: "Gruppe „\($0)“", bundle: .lumen, locale: .lumen) } ?? ""
        if account.darfVerwalten { return String(localized: "Mitverwalter", bundle: .lumen, locale: .lumen) + gruppe }
        if account.isKid { return String(localized: "Kinderprofil", bundle: .lumen, locale: .lumen) + gruppe }
        return String(localized: "Haushalt", bundle: .lumen, locale: .lumen) + gruppe
    }

    /// Eine Sprachwahl, die sofort beim Server landet. Kennt die Liste
    /// den gemerkten Wert nicht (etwa „und"), steht er trotzdem da.
    private func sprache(_ key: String, leer: String) -> Binding<String> {
        Binding(
            get: {
                let wert = prefs[key] ?? leer
                return wert.isEmpty ? leer : wert
            },
            set: { neu in
                Task { await session.prefsSetzen([key: neu]) }
            })
    }

    /// Ein Schalter, der sofort beim Server landet.
    private func pref(_ key: String, default fallback: Bool) -> Binding<Bool> {
        Binding(
            get: { prefs[key].map { $0 == "1" } ?? fallback },
            set: { value in
                Task {
                    _ = try? await session.client.updatePrefs([key: value ? "1" : "0"])
                    await session.refreshMe()
                }
            })
    }
}

// MARK: - Wünsche

struct WishesView: View {
    @Environment(Session.self) private var session
    @State private var wishes: [Wish] = []
    @State private var newTitle = ""
    @State private var loading = true

    var body: some View {
        List {
            Section {
                HStack {
                    TextField("Titel wünschen …", text: $newTitle)
                        .textFieldStyle(.plain)
                        .onSubmit(send)
                    Button("Wünschen", action: send)
                        .disabled(newTitle.trimmingCharacters(in: .whitespaces).isEmpty)
                }
            } footer: {
                Text("Der Besitzer sieht den Titel und wer ihn gewünscht hat — sonst nichts.")
            }

            ForEach(wishes) { wish in
                VStack(alignment: .leading, spacing: 3) {
                    HStack {
                        Text(wish.title + (wish.year.map { " (\($0))" } ?? ""))
                        Spacer()
                        Text(wish.stateLabel)
                            .font(.caption.weight(.medium))
                            .foregroundStyle(color(for: wish.state))
                    }
                    Text("von \(wish.by)" + (wish.mine ? " (du)" : ""))
                        .font(.caption).foregroundStyle(Theme.label2)
                }
                .padding(.vertical, 3)
            }
        }
        .overlay {
            if !loading && wishes.isEmpty {
                ContentUnavailableView("Noch keine Wünsche", systemImage: "plus.circle")
            }
        }
        .navigationTitle("Wünsche")
        .task { await load() }
        .refreshable { await load() }
    }

    private func color(for state: String) -> Color {
        switch state {
        case "working": Theme.accent
        case "done": Theme.good
        case "declined": Theme.bad
        default: Theme.label2
        }
    }

    private func load() async {
        wishes = (try? await session.client.wishes()) ?? []
        loading = false
    }

    private func send() {
        let title = newTitle.trimmingCharacters(in: .whitespaces)
        guard !title.isEmpty else { return }
        newTitle = ""
        Task {
            _ = try? await session.client.wish(title: title)
            await load()
        }
    }
}
