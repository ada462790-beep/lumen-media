// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

import SwiftUI
import LumenKit

/// Verwaltung — für den Besitzer und für Mitverwalter (Issue #6), also
/// alle, bei denen der Server `isAdmin` setzt.
///
/// Was hier zu sehen ist, reicht, um den Server zu betreiben: Bibliotheken
/// prüfen, laufende Wiedergaben beenden, ins Protokoll schauen. Was nicht
/// zu sehen ist: welcher Titel gerade läuft. Der Server gibt es nicht
/// heraus, also kann die App es auch nicht zeigen — dem Besitzer nicht
/// und einem Mitverwalter erst recht nicht.
///
/// Jede Anfrage hier läuft über `session.verwaltet`: hat die Person eine
/// Verwaltungssperre, erscheint erst der Entsperr-Dialog.
struct AdminView: View {
    @Environment(Session.self) private var session
    #if os(macOS)
    /// Freier Platz und letzte Sicherung. Ein älterer Server schickt das
    /// nicht — dann fehlen die Zeilen, sonst nichts.
    @State private var uebersicht: Uebersicht?
    /// „1.4.0 verfügbar" oder „1.3.2 · aktuell" — nur für den Besitzer.
    @State private var updateKurz: String?
    #endif

    var body: some View {
        List {
            #if os(macOS)
            serverZustand
            #endif
            Section("Mediathek") {
                NavigationLink { LibrariesView() } label: {
                    Label("Bibliotheken", systemImage: "folder")
                }
                // Die Verwaltung gehört an den Schreibtisch: was neu dazu
                // kam, gibt es nur auf dem Mac (und im Browser).
                #if os(macOS)
                NavigationLink { AdminWuenscheView() } label: {
                    Label("Wünsche", systemImage: "plus.circle")
                }
                NavigationLink { AufraeumenView() } label: {
                    Label("Aufräumen", systemImage: "sparkles")
                }
                #endif
            }
            Section("System") {
                NavigationLink { SessionsView() } label: {
                    Label("Aktive Wiedergaben", systemImage: "play.circle")
                }
                NavigationLink { LogView() } label: {
                    Label("Protokoll", systemImage: "list.bullet.rectangle")
                }
                #if os(macOS)
                NavigationLink { KontenView() } label: {
                    Label("Konten und Rechte", systemImage: "person.2")
                }
                NavigationLink { ServerEinstellungenView() } label: {
                    Label("Name, Sprache, Metadaten, Fernzugriff", systemImage: "server.rack")
                }
                // Gruppen, Updates und Sicherung nur für den Besitzer: der
                // Server nimmt sie von Mitverwaltern gar nicht an.
                if session.isOwner {
                    NavigationLink { GruppenView() } label: {
                        Label("Gruppen", systemImage: "person.3")
                    }
                    NavigationLink { UpdatesView() } label: {
                        LabeledContent {
                            Text(updateKurz ?? "").foregroundStyle(Theme.label2)
                        } label: {
                            Label("Updates", systemImage: "arrow.down.circle")
                        }
                    }
                    NavigationLink { SicherungView() } label: {
                        Label("Sicherung", systemImage: "externaldrive.badge.timemachine")
                    }
                }
                #endif
            }
            Section {
                NavigationLink { AdminLockView() } label: {
                    Label("Verwaltungssperre", systemImage: "lock.shield")
                }
            } footer: {
                Text("Ein eigener PIN, ein Passwort oder ein Code aus einer Authenticator-App vor der Verwaltung — damit niemand, der dein Gerät in der Hand hat, am Server herumstellt.")
            }
            Section {
                EmptyView()
            } footer: {
                Label("Lumen zeigt nicht, wer was schaut. Verlauf und Bewertungen gehören jeder Person selbst.", systemImage: "lock")
                    .font(.caption)
            }
        }
        .navigationTitle("Verwaltung")
        #if os(macOS)
        .task {
            uebersicht = try? await session.verwaltet { try await session.client.adminOverview() }
            if session.isOwner {
                updateKurz = (try? await session.verwaltet { try await session.client.adminUpdate() })?.kurz
            }
        }
        #endif
    }

    #if os(macOS)
    /// Was ein Betreiber auf einen Blick wissen will, bevor es knallt: ob
    /// die Platte reicht und ob die Sicherung läuft.
    @ViewBuilder
    private var serverZustand: some View {
        if let u = uebersicht, u.platz != nil || u.sicherung != nil {
            Section("Server") {
                if let p = u.platz {
                    LabeledContent("Freier Platz") {
                        Text(platzText(p))
                            .foregroundStyle(p.knapp ? Theme.warn : Theme.label2)
                    }
                    if p.knapp {
                        Label("Weniger als 5 GB frei. Läuft die Platte voll, scheitern Umwandlungen und die nächtliche Sicherung.",
                              systemImage: "exclamationmark.triangle")
                            .font(.caption).foregroundStyle(Theme.warn)
                    }
                }
                if let tag = u.sicherung {
                    LabeledContent("Sicherung") {
                        Text(tag.isEmpty ? "noch keine" : "zuletzt \(Sicherung.tagText(tag))")
                            .foregroundStyle(Theme.label2)
                    }
                }
            }
        }
    }

    /// Liegen Datenbank und Zwischendateien auf derselben Platte, ist das
    /// eine Zahl. Sonst beide — knapp wird es auf der kleineren.
    private func platzText(_ p: FreierPlatz) -> String {
        if p.datenMB == p.umwandlungMB { return FreierPlatz.text(p.datenMB) }
        let daten = FreierPlatz.text(p.datenMB), umwandlung = FreierPlatz.text(p.umwandlungMB)
        return String(localized: "\(daten) für Daten · \(umwandlung) für Umwandlungen", bundle: .lumen, locale: .lumen)
    }
    #endif
}

// MARK: - Bibliotheken

struct LibrariesView: View {
    @Environment(Session.self) private var session
    @State private var libraries: [Library] = []
    @State private var loading = true
    @State private var note: String?
    /// Die Bibliothek, für die gerade die Rückfrage offen ist.
    @State private var zuLoeschen: Library?
    /// Läuft irgendwo ein Durchlauf, wird im Takt nachgefragt. Ein
    /// Kreisel, der nichts sagt, ist keine Auskunft.
    @State private var takt: Task<Void, Never>?
    #if os(macOS)
    /// Was im Menü „Von selbst nachsehen" gerade gewählt ist, bis der
    /// Server es bestätigt hat — sonst spränge das Menü beim Nachfragen
    /// kurz zurück.
    @State private var taktWahl: [String: String] = [:]
    @State private var taktZeit: [String: String] = [:]
    #endif

    var body: some View {
        List {
            ForEach(libraries) { library in
                VStack(alignment: .leading, spacing: 5) {
                    HStack {
                        Text(library.name).font(.body.weight(.medium))
                        Spacer()
                        Button(library.stand?.laeuft == true ? "läuft …"
                               : library.wartet == true ? "wartet …" : "Prüfen") {
                            Task {
                                try? await session.verwaltet {
                                    try await session.client.adminScan(libraryId: library.id)
                                }
                                await load()
                            }
                        }
                        .buttonStyle(QuietButtonStyle())
                        .disabled(library.stand?.laeuft == true || library.wartet == true)

                        Button(role: .destructive) {
                            zuLoeschen = library
                        } label: {
                            Image(systemName: "trash").imageScale(.small)
                        }
                        .buttonStyle(.borderless)
                        .accessibilityLabel("\(library.name) löschen")
                    }
                    Text(library.path)
                        .font(.system(.caption, design: .monospaced))
                        .foregroundStyle(Theme.label3).lineLimit(1)
                    Text(infoZeile(library))
                        .font(.caption).foregroundStyle(Theme.label2)

                    if let stand = library.stand, stand.laeuft {
                        fortschritt(stand)
                    } else if library.wartet == true {
                        HStack(spacing: 8) {
                            ProgressView().controlSize(.small)
                            Text("Angenommen — wartet, bis die andere Arbeit fertig ist, dann geht es von selbst los.")
                                .font(.caption).foregroundStyle(Theme.label2)
                        }
                        .padding(.top, 2)
                    } else if let m = library.metadaten, m.laeuft {
                        metadatenFortschritt(m)
                    } else if library.erreichbar == false {
                        Label(library.problem ?? String(localized: "Der Ordner ist nicht erreichbar.", bundle: .lumen, locale: .lumen),
                              systemImage: "exclamationmark.triangle")
                            .font(.caption).foregroundStyle(.orange)
                    } else if library.scannedAt != nil, library.count == 0 {
                        // Geprüft und nichts gefunden: fast immer steht die
                        // Art falsch oder der Ordner eine Ebene daneben.
                        Label("Geprüft, aber nichts gefunden. Liegen die Dateien wirklich hier — und sind es Videodateien? Unterordner liest Lumen mit.",
                              systemImage: "exclamationmark.triangle")
                            .font(.caption).foregroundStyle(.orange)
                    }

                    #if os(macOS)
                    taktZeile(library)
                    #endif
                }
                .padding(.vertical, 4)
            }

            if let note {
                Section { Text(note).font(.caption).foregroundStyle(Theme.label2) }
            }

            #if os(macOS)
            if !libraries.isEmpty {
                Section {
                    Button("Alle Metadaten neu laden") { Task { await metadatenNeu(nil) } }
                } footer: {
                    Text("Holt Beschreibungen, Besetzung und Bilder für alle Titel noch einmal. Von Hand zugewiesene bleiben.")
                }
            }
            #endif

            Section {
                EmptyView()
            } footer: {
                Text("Neue Bibliotheken legst du im Browser an — dort lässt sich der Ordner anklicken, statt den Pfad abzutippen.")
            }
        }
        .overlay { if loading { ProgressView() } }
        .navigationTitle("Bibliotheken")
        .task { await load() }
        .refreshable { await load() }
        .onDisappear { takt?.cancel(); takt = nil }
        .confirmationDialog("Bibliothek löschen?",
                            isPresented: Binding(get: { zuLoeschen != nil },
                                                 set: { if !$0 { zuLoeschen = nil } }),
                            presenting: zuLoeschen) { lib in
            Button("\(lib.name) löschen", role: .destructive) {
                Task {
                    do {
                        try await session.verwaltet { try await session.client.adminDeleteLibrary(lib.id) }
                        note = String(localized: "\(lib.name) ist weg. Die Dateien auf der Platte sind geblieben.", bundle: .lumen, locale: .lumen)
                    } catch {
                        note = error.localizedDescription
                    }
                    zuLoeschen = nil
                    await load()
                }
            }
            Button("Abbrechen", role: .cancel) { zuLoeschen = nil }
        } message: { lib in
            Text("Lumen vergisst, was in \(lib.name) liegt. Die Dateien selbst bleiben unberührt.")
        }
    }

    /// „120 Titel · zuletzt geprüft … · von selbst: 3 neu"
    private func infoZeile(_ library: Library) -> String {
        var text = String(localized: "\(library.count) Titel", bundle: .lumen, locale: .lumen) + " · " + (library.scannedAt.map {
            let wann = $0.formatted(Date.FormatStyle(date: .abbreviated, time: .shortened).locale(.lumen))
            return String(localized: "zuletzt geprüft \(wann)", bundle: .lumen, locale: .lumen)
        } ?? String(localized: "noch nie geprüft", bundle: .lumen, locale: .lumen))
        if let e = library.lastResult { text += " · " + e.text }
        return text
    }

    @ViewBuilder
    private func fortschritt(_ stand: ScanFortschritt) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            if let anteil = stand.anteil {
                ProgressView(value: anteil)
                Text(stand.datei.isEmpty
                     ? "\(stand.fertig) von \(stand.gesamt) eingelesen"
                     : "\(stand.fertig) von \(stand.gesamt) eingelesen · \(stand.datei)")
                    .font(.caption).foregroundStyle(Theme.label2).lineLimit(1)
            } else {
                ProgressView()
                Text("sucht nach Videodateien …")
                    .font(.caption).foregroundStyle(Theme.label2)
            }
        }
        .padding(.top, 2)
    }

    private func metadatenFortschritt(_ m: MetadatenStand) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            ProgressView(value: m.anteil)
            Text((m.titel ?? "").isEmpty
                 ? "Metadaten: \(m.fertig) von \(m.gesamt)"
                 : "Metadaten: \(m.fertig) von \(m.gesamt) · \(m.titel ?? "")")
                .font(.caption).foregroundStyle(Theme.label2).lineLimit(1)
        }
        .padding(.top, 2)
    }

    #if os(macOS)
    /// Wie oft Lumen von selbst nachsieht — in Minuten, oder täglich zu
    /// einer festen Uhrzeit. Dieselbe Auswahl wie im Browser.
    private static let taktWerte = ["0", "15", "30", "60", "120", "360", "720", "1440", "zeit"]

    private static func taktName(_ wert: String) -> String {
        switch wert {
        case "0": return String(localized: "nur auf Knopfdruck", bundle: .lumen, locale: .lumen)
        case "60": return String(localized: "jede Stunde", bundle: .lumen, locale: .lumen)
        case "zeit": return String(localized: "täglich um …", bundle: .lumen, locale: .lumen)
        default:
            let minuten = Int(wert) ?? 0
            let stunden = minuten / 60
            return minuten % 60 == 0 ? String(localized: "alle \(stunden) Stunden", bundle: .lumen, locale: .lumen) : String(localized: "alle \(minuten) Minuten", bundle: .lumen, locale: .lumen)
        }
    }

    private func taktZeile(_ library: Library) -> some View {
        let wahl = Binding<String>(
            get: { taktWahl[library.id] ?? library.taktWahl },
            set: { neu in
                taktWahl[library.id] = neu
                Task { await taktSpeichern(library) }
            })
        let zeit = Binding<Date>(
            get: { Uhrzeit.datum(taktZeit[library.id] ?? library.scanAt.flatMap { $0.isEmpty ? nil : $0 } ?? "04:00") },
            set: { neu in
                taktZeit[library.id] = Uhrzeit.text(neu)
                Task { await taktSpeichern(library) }
            })
        return HStack(spacing: 10) {
            Picker("Von selbst nachsehen", selection: wahl) {
                ForEach(Self.taktWerte, id: \.self) { wert in
                    Text(Self.taktName(wert)).tag(wert)
                }
            }
            .fixedSize()
            if wahl.wrappedValue == "zeit" {
                DatePicker("Uhrzeit", selection: zeit, displayedComponents: .hourAndMinute)
                    .labelsHidden()
                    .fixedSize()
            }
            Spacer()
            Button("Metadaten neu laden") { Task { await metadatenNeu(library) } }
        }
        .controlSize(.small)
        .padding(.top, 4)
    }

    private func taktSpeichern(_ library: Library) async {
        let wahl = taktWahl[library.id] ?? library.taktWahl
        let fest = wahl == "zeit"
        let zeit = taktZeit[library.id] ?? library.scanAt.flatMap { $0.isEmpty ? nil : $0 } ?? "04:00"
        let minuten = fest ? 1440 : (Int(wahl) ?? 0)
        do {
            try await session.verwaltet {
                try await session.client.adminSetLibraryTakt(library.id, intervalMin: minuten,
                                                            scanAt: fest ? zeit : "")
            }
            note = fest ? String(localized: "\(library.name): Lumen sieht täglich um \(zeit) nach.", bundle: .lumen, locale: .lumen)
                : minuten > 0 ? String(localized: "\(library.name): Lumen sieht von selbst nach.", bundle: .lumen, locale: .lumen)
                : String(localized: "\(library.name): nur noch auf Knopfdruck.", bundle: .lumen, locale: .lumen)
        } catch {
            note = Session.text(error)
        }
        await load()
        taktWahl[library.id] = nil
        taktZeit[library.id] = nil
    }

    private func metadatenNeu(_ library: Library?) async {
        do {
            let auftrag = try await session.verwaltet {
                try await session.client.adminMetadataReload(libraryId: library?.id)
            }
            let anzahl: Int = auftrag.titel
            note = String(localized: "\(anzahl) Titel werden neu geholt — das kann dauern.", bundle: .lumen, locale: .lumen)
        } catch {
            note = Session.text(error)
        }
        await load()
    }
    #endif

    private func load() async {
        libraries = (try? await session.verwaltet { try await session.client.adminLibraries() }) ?? []
        loading = false
        taktSetzen()
    }

    /// Solange etwas läuft — ein Durchlauf, ein wartender, Metadaten —,
    /// alle anderthalb Sekunden nachfragen, und sonst gar nicht.
    private func taktSetzen() {
        let laeuft = libraries.contains { $0.beschaeftigt }
        takt?.cancel()
        takt = nil
        guard laeuft else { return }
        takt = Task {
            try? await Task.sleep(for: .milliseconds(1500))
            if Task.isCancelled { return }
            await load()
        }
    }
}

// MARK: - Aktive Wiedergaben

struct SessionsView: View {
    @Environment(Session.self) private var session
    @State private var data: Sessions?
    @State private var loading = true
    /// Ein Fehler ist nicht „gerade läuft nichts" — er bekommt seine eigene
    /// Anzeige mit „Nochmal versuchen".
    @State private var error: String?

    var body: some View {
        List {
            if let data, !data.sessions.isEmpty {
                // Bewusst ohne Person, Gerät, Ort und Startzeit: nur die Last.
                ForEach(Array(data.sessions.enumerated()), id: \.element.id) { nr, live in
                    HStack(alignment: .top, spacing: 12) {
                        Image(systemName: "film")
                            .frame(width: 36, height: 36)
                            .background(Circle().fill(Theme.label2.opacity(0.15)))
                        VStack(alignment: .leading, spacing: 4) {
                            HStack(spacing: 8) {
                                Text("Wiedergabe \(nr + 1)")
                                    .font(.subheadline.weight(.medium))
                                Text(live.mode == "direct" ? "direkt" : "wird umgewandelt")
                                    .font(.caption2.weight(.medium))
                                    .foregroundStyle(live.mode == "direct" ? Theme.good : Theme.accent)
                                    .padding(.horizontal, 7).padding(.vertical, 3)
                                    .background(Capsule().fill(
                                        (live.mode == "direct" ? Theme.good : Theme.accent).opacity(0.16)))
                            }
                        }
                        Spacer()
                        // Ohne Kennung (der Server gibt sie nicht mehr
                        // heraus) lässt sich nichts gezielt beenden.
                        if let kennung = live.kennung {
                            Button("Beenden", role: .destructive) {
                                Task {
                                    do {
                                        try await session.verwaltet {
                                            try await session.client.adminStopSession(kennung)
                                        }
                                    } catch {
                                        self.error = Session.text(error)
                                    }
                                    await load()
                                }
                            }
                            .buttonStyle(QuietButtonStyle())
                        }
                    }
                    .padding(.vertical, 5)
                }
            }

            if let note = data?.note {
                Section {
                    EmptyView()
                } footer: {
                    Label(note, systemImage: "lock").font(.caption)
                }
            }
        }
        .overlay {
            if loading && data == nil { ProgressView() }
            else if let error, data?.sessions.isEmpty != false {
                Loading(loading: false, error: error, retry: load)
            } else if data?.sessions.isEmpty != false {
                ContentUnavailableView("Gerade läuft nichts", systemImage: "play.slash")
            }
        }
        .navigationTitle("Aktive Wiedergaben")
        .task { await load() }
        .refreshable { await load() }
    }

    private func load() async {
        do {
            data = try await session.verwaltet { try await session.client.adminSessions() }
            error = nil
        } catch {
            if APIClient.istAbbruch(error) { return }
            self.error = Session.text(error)
        }
        loading = false
    }
}

// MARK: - Protokoll

struct LogView: View {
    @Environment(Session.self) private var session
    @State private var lines: [LogLine] = []
    @State private var level = "all"
    @State private var loading = true

    var body: some View {
        List {
            Picker("Stufe", selection: $level) {
                Text("Alles").tag("all")
                Text("Fehler").tag("error")
                Text("Warnung").tag("warn")
                Text("Info").tag("info")
            }
            .pickerStyle(.segmented)
            .onChange(of: level) { _, _ in Task { await load() } }

            ForEach(lines) { line in
                VStack(alignment: .leading, spacing: 3) {
                    HStack(spacing: 8) {
                        Text(line.at.formatted(Date.FormatStyle(date: .omitted, time: .standard).locale(.lumen)))
                            .font(.system(.caption2, design: .monospaced))
                            .foregroundStyle(Theme.label3)
                        Text(line.level.uppercased())
                            .font(.caption2.weight(.bold))
                            .foregroundStyle(color(line.level))
                        Text(line.area).font(.caption2).foregroundStyle(Theme.label3)
                    }
                    Text(line.message)
                        .font(.system(.caption, design: .monospaced))
                        .foregroundStyle(line.level == "info" ? Theme.label2 : color(line.level))
                }
                .padding(.vertical, 2)
            }
        }
        .overlay { if loading { ProgressView() } }
        .navigationTitle("Protokoll")
        .task { await load() }
        .refreshable { await load() }
    }

    private func color(_ level: String) -> Color {
        switch level {
        case "error": Theme.bad
        case "warn": Theme.warn
        default: Theme.label2
        }
    }

    private func load() async {
        lines = (try? await session.verwaltet { try await session.client.adminLog(level: level) }) ?? []
        loading = false
    }
}
