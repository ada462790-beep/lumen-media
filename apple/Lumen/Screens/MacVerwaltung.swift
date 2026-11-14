// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

import SwiftUI
import LumenKit
#if os(macOS)
import AppKit
#endif

// Was die Verwaltung auf dem Mac zusätzlich kann: Wünsche bearbeiten,
// aufräumen, Konten und ihre Rechte, Gruppen, Updates, Sicherung und einen
// Titel neu zuweisen — dieselben Seiten wie im Browser (app.js:
// viewAdminWishes, viewCleanup, viewAccounts, viewAdminGroups,
// viewAdminUpdate, viewAdminBackups, zuweisenPanel). Nur auf dem Mac: die
// Verwaltung gehört an den Schreibtisch, auf iPhone, iPad und Fernseher
// nicht.
//
// Was hier NICHT steht: wer wann was geschaut hat. Ein Wunsch ist, was
// jemand ausdrücklich äussert; ein Konto nennt seine Rechte und wie viele
// Geräte es hat. Alles andere bleibt bei der Person.

#if os(macOS)

// MARK: - Wünsche

struct AdminWuenscheView: View {
    @Environment(Session.self) private var session
    @State private var wuensche: [Wish] = []
    @State private var loading = true
    @State private var error: String?

    var body: some View {
        List {
            gruppe(String(localized: "Offen", bundle: .lumen, locale: .lumen), ["open"])
            gruppe(String(localized: "In Arbeit", bundle: .lumen, locale: .lumen), ["working"])
            gruppe(String(localized: "Erledigt", bundle: .lumen, locale: .lumen), ["done", "declined"])
            Section {
                EmptyView()
            } footer: {
                Label("Ein Wunsch ist das, was jemand dir ausdrücklich sagt. Was er sonst schaut, steht hier nicht — und nirgends sonst.", systemImage: "lock")
                    .font(.caption)
            }
        }
        .overlay {
            if !loading && error == nil && wuensche.isEmpty {
                ContentUnavailableView("Noch hat sich niemand etwas gewünscht", systemImage: "plus.circle")
            } else {
                Loading(loading: loading, error: error, retry: load)
            }
        }
        .navigationTitle("Wünsche")
        .task { await load() }
    }

    @ViewBuilder
    private func gruppe(_ titel: String, _ zustaende: [String]) -> some View {
        let teil = wuensche.filter { zustaende.contains($0.state) }
        if !teil.isEmpty {
            Section("\(titel) · \(teil.count)") {
                ForEach(teil) { w in zeile(w) }
            }
        }
    }

    private func zeile(_ w: Wish) -> some View {
        HStack(spacing: 12) {
            ProfilAvatar(name: w.by, avatar: w.avatar, size: 30)
            VStack(alignment: .leading, spacing: 2) {
                Text(w.title + (w.year.map { " (\($0))" } ?? ""))
                Text(w.mine
                     ? "von \(w.by) (du) · \(w.createdAt.formatted(.dateTime.day().month().year().locale(.lumen)))"
                     : "von \(w.by) · \(w.createdAt.formatted(.dateTime.day().month().year().locale(.lumen)))")
                    .font(.caption).foregroundStyle(Theme.label2)
                if let note = w.note, !note.isEmpty {
                    Text("Notiz: \(note)").font(.caption).foregroundStyle(Theme.label2)
                }
            }
            Spacer()
            Text(w.stateLabel).font(.caption.weight(.medium)).foregroundStyle(farbe(w.state))
            if w.state == "open" { knopf(w, "working", String(localized: "Annehmen", bundle: .lumen, locale: .lumen)) }
            if w.state != "done" { knopf(w, "done", String(localized: "Erfüllt", bundle: .lumen, locale: .lumen)) }
            if w.state != "declined" && w.state != "done" { knopf(w, "declined", String(localized: "Ablehnen", bundle: .lumen, locale: .lumen)) }
            if w.state == "done" || w.state == "declined" { knopf(w, "open", String(localized: "Wieder offen", bundle: .lumen, locale: .lumen)) }
        }
        .padding(.vertical, 4)
    }

    private func knopf(_ w: Wish, _ zu: String, _ text: String) -> some View {
        Button(text) {
            Task {
                do {
                    try await session.verwaltet { try await session.client.adminSetWishState(w.id, state: zu) }
                    await load()
                } catch {
                    self.error = Session.text(error)
                }
            }
        }
        .controlSize(.small)
    }

    private func farbe(_ state: String) -> Color {
        switch state {
        case "working": Theme.accent
        case "done": Theme.good
        case "declined": Theme.bad
        default: Theme.label2
        }
    }

    private func load() async {
        error = nil
        // Wer verwalten darf, bekommt hier alle Wünsche des Haushalts.
        do { wuensche = try await session.client.wishes() }
        catch { self.error = Session.text(error) }
        loading = false
    }
}

// MARK: - Aufräumen

/// Wie im Browser (app.js: viewCleanup): Filme in Teilen, dieselbe Datei
/// mehrfach oder verschiedene Filme an einem Eintrag, fehlende Dateien,
/// nicht erkannte Titel und aufgelöste Filmreihen. Echte Fassungen —
/// derselbe Film in zwei Qualitäten — stehen hier nicht: die wählt man im
/// Player unter „Qualität".
struct AufraeumenView: View {
    @Environment(Session.self) private var session
    @State private var daten: Aufraeumen?
    @State private var reihen: [ReiheZeile] = []
    @State private var loading = true
    @State private var error: String?
    /// Was der Server zu einer Aktion gesagt hat — etwa „läuft — das kann
    /// ein paar Minuten dauern" beim Zusammenführen.
    @State private var meldung: String?
    /// Der Titel, an dem gerade gearbeitet wird.
    @State private var beschaeftigt: String?

    private var aufgeloest: [ReiheZeile] { reihen.filter(\.hidden) }
    /// Nur Filme in Teilen (a/b/c, CD1/CD2) — Teil 1, 2, 3 ohne Lücke.
    private var teile: [Duplikat] { (daten?.duplicates ?? []).filter(\.sindTeile) }
    /// Dieselbe Datei mehrfach oder verschiedene Filme an einem Eintrag.
    private var doppelt: [Duplikat] { (daten?.duplicates ?? []).filter(\.doppeltOderFalsch) }

    var body: some View {
        List {
            if let meldung {
                Section {
                    Label(meldung, systemImage: "info.circle")
                }
            }
            if let d = daten {
                if !teile.isEmpty {
                    Section {
                        ForEach(teile) { x in teileZeile(x) }
                    } header: {
                        Text("Filme in Teilen · \(teile.count)")
                    } footer: {
                        Text("Ein Film, auf mehrere Dateien verteilt (a/b/c, CD1/CD2). Die Teile laufen nacheinander; zusammengeführt läuft der Film überall am Stück. Zusammenführen hängt die Teile ohne Neurechnen aneinander; die Teile wandern in den Ordner „.lumen-teile“.")
                    }
                }
                if !doppelt.isEmpty {
                    Section {
                        ForEach(doppelt) { x in doppeltZeile(x) }
                    } header: {
                        Text("Doppelt oder falsch zugeordnet · \(doppelt.count)")
                    } footer: {
                        Text("„Dieselbe Datei mehrfach“: gleiche Grösse und Laufzeit in zwei Ordnern – eine Kopie kann weg (Lumen löscht keine Dateien). „Verschiedene Filme“: Die Dateien heissen nach verschiedenen Filmen – trenne die falsche ab. Echte Fassungen (Sprache, Qualität) stehen hier nicht; man wählt sie im Player unter „Qualität“.")
                    }
                }
                if let fehlt = d.missing, !fehlt.isEmpty {
                    Section {
                        ForEach(fehlt) { f in
                            Label(f.path, systemImage: "exclamationmark.triangle")
                                .font(.system(.caption, design: .monospaced))
                                .foregroundStyle(Theme.label2)
                                .textSelection(.enabled)
                        }
                    } header: {
                        Text("Fehlende Dateien · \(fehlt.count)")
                    } footer: {
                        Text("Die Titel bleiben stehen — eine abgehängte Platte soll die Mediathek nicht leeren.")
                    }
                }
                if let offen = d.unmatched, !offen.isEmpty {
                    Section {
                        ForEach(offen) { i in
                            // Zum Titel: dort unter „Anderen Titel zuweisen"
                            // suchen oder einen Link einfügen.
                            NavigationLink { DetailView(itemId: i.id) } label: {
                                HStack {
                                    VStack(alignment: .leading, spacing: 2) {
                                        Text(i.title)
                                        Text("\(i.year.map(String.init) ?? "ohne Jahr") · \(artName(i.kind))")
                                            .font(.caption).foregroundStyle(Theme.label2)
                                    }
                                    Spacer()
                                    Text("Zuordnen").font(.caption).foregroundStyle(Theme.accent)
                                }
                            }
                        }
                    } header: {
                        Text("Nicht erkannt · \(offen.count)")
                    } footer: {
                        Text("Hier war die Zuordnung zu unsicher. Lieber nichts als der falsche Film. Auf der Titelseite unter „Anderen Titel zuweisen“ suchen — oder den Link von TMDb oder IMDb einfügen.")
                    }
                }
                if !aufgeloest.isEmpty {
                    Section {
                        ForEach(aufgeloest) { r in
                            HStack {
                                VStack(alignment: .leading, spacing: 2) {
                                    Text(r.name)
                                    Text(r.anzahl == 1 ? "1 Film" : "\(r.anzahl) Filme")
                                        .font(.caption).foregroundStyle(Theme.label2)
                                }
                                Spacer()
                                Button("Wiederherstellen") { Task { await zurueckholen(r) } }
                            }
                        }
                    } header: {
                        Text("Aufgelöste Filmreihen · \(aufgeloest.count)")
                    } footer: {
                        Text("Lumen bildet sie nicht wieder, bis du sie zurückholst.")
                    }
                }
            }
        }
        .overlay {
            if !loading, error == nil, alleLeer {
                ContentUnavailableView("Hier ist alles in Ordnung", systemImage: "checkmark.circle")
            } else {
                Loading(loading: loading, error: error, retry: load)
            }
        }
        .navigationTitle("Aufräumen")
        .task { await load() }
    }

    private func artName(_ kind: String) -> String {
        switch kind {
        case "movie": String(localized: "Film", bundle: .lumen, locale: .lumen)
        case "series": String(localized: "Serie", bundle: .lumen, locale: .lumen)
        default: kind
        }
    }

    /// Ein Film in Teilen: jede Datei mit vollem Pfad, Teilnummer, Format
    /// und Grösse. Zusammenführen darf nur der Besitzer, weil es Dateien
    /// auf der Platte anlegt und verschiebt.
    private func teileZeile(_ x: Duplikat) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(alignment: .firstTextBaseline) {
                Text(x.title + ((x.year ?? 0) > 0 ? " (\(x.year ?? 0))" : "")).font(.headline)
                Text("\(x.files.count) Teile eines Films")
                    .font(.caption).foregroundStyle(Theme.label2)
                Spacer()
                if session.isOwner {
                    Button(beschaeftigt == x.itemId ? "Einen Moment …" : "Zu einer Datei zusammenführen") {
                        Task { await zusammenfuehren(x) }
                    }
                    .disabled(beschaeftigt != nil)
                }
            }
            ForEach(x.dateienGeordnet) { f in
                VStack(alignment: .leading, spacing: 2) {
                    Text(f.path)
                        .font(.system(.caption, design: .monospaced))
                        .foregroundStyle(Theme.label)
                        .lineLimit(2).truncationMode(.middle)
                        .textSelection(.enabled)
                    Text(dateiZeile(f))
                        .font(.caption).foregroundStyle(Theme.label2)
                }
            }
        }
        .controlSize(.small)
        .padding(.vertical, 4)
    }

    /// Dieselbe Datei mehrfach oder verschiedene Filme an einem Eintrag —
    /// wie im Browser (app.js, doppelZeile): je Datei Pfad und Format, bei
    /// Zwillingen „doppelt — eine kann weg", dazu „Diese nehmen" (nicht bei
    /// verschiedenen Filmen) und „Abtrennen" (nicht bei Zwillingen).
    private func doppeltZeile(_ x: Duplikat) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(alignment: .firstTextBaseline) {
                Text(x.title + ((x.year ?? 0) > 0 ? " (\(x.year ?? 0))" : "")).font(.headline)
                Text(x.art == "verschieden"
                     ? String(localized: "· verschiedene Filme an einem Eintrag", bundle: .lumen, locale: .lumen)
                     : String(localized: "· dieselbe Datei mehrfach", bundle: .lumen, locale: .lumen))
                    .font(.caption).foregroundStyle(Theme.label2)
                Spacer()
            }
            ForEach(x.files) { f in
                HStack(alignment: .center, spacing: 10) {
                    VStack(alignment: .leading, spacing: 2) {
                        Text(f.path)
                            .font(.system(.caption, design: .monospaced))
                            .foregroundStyle(Theme.label)
                            .lineLimit(2).truncationMode(.middle)
                            .textSelection(.enabled)
                        Text(f.formatZeile)
                            .font(.caption).foregroundStyle(Theme.label2)
                    }
                    Spacer(minLength: 8)
                    if x.istDoppelt(f) {
                        Text("doppelt — eine kann weg")
                            .font(.caption.weight(.medium))
                            .foregroundStyle(Theme.bad)
                    }
                    if x.art != "verschieden" {
                        if f.isPreferred {
                            Text("gewählt").font(.caption.weight(.medium)).foregroundStyle(Theme.good)
                        } else {
                            Button("Diese nehmen") { Task { await nehmen(x, f) } }
                                .disabled(beschaeftigt != nil)
                        }
                    }
                    if x.art != "identisch" {
                        Button("Abtrennen") { Task { await abtrennen(f) } }
                            .disabled(beschaeftigt != nil)
                            .help(String(localized: "Ist ein anderer Film — als eigenen Titel führen", bundle: .lumen, locale: .lumen))
                    }
                }
            }
        }
        .controlSize(.small)
        .padding(.vertical, 4)
    }

    /// „Diese nehmen": die Datei für alle zum Standard des Titels machen.
    private func nehmen(_ x: Duplikat, _ f: MediaFile) async {
        beschaeftigt = x.itemId
        defer { beschaeftigt = nil }
        do {
            try await session.verwaltet { try await session.client.adminFassungWaehlen(itemId: x.itemId, fileId: f.id) }
            error = nil
        } catch {
            meldung = Session.text(error)
            return
        }
        await load()
    }

    /// „Abtrennen": die Datei wird ein eigener Titel, mit dem Namen, den
    /// ihr Dateiname hergibt.
    private func abtrennen(_ f: MediaFile) async {
        beschaeftigt = f.itemId
        defer { beschaeftigt = nil }
        do {
            try await session.verwaltet { try await session.client.adminDateiAbtrennen(f.id) }
            error = nil
        } catch {
            meldung = Session.text(error)
            return
        }
        await load()
    }

    /// „Teil 2 · 1080p · MKV · 4,2 GB", bei fehlender Datei mit „fehlt".
    private func dateiZeile(_ f: MediaFile) -> String {
        var teile = [String(localized: "Teil \(f.partNo)", bundle: .lumen, locale: .lumen)]
        if !f.formatZeile.isEmpty { teile.append(f.formatZeile) }
        if f.isMissing { teile.append(String(localized: "fehlt", bundle: .lumen, locale: .lumen)) }
        return teile.joined(separator: " · ")
    }

    private func zusammenfuehren(_ x: Duplikat) async {
        beschaeftigt = x.itemId
        defer { beschaeftigt = nil }
        do {
            let auftrag = try await session.verwaltet { try await session.client.adminTeileZusammenfuehren(x.itemId) }
            meldung = auftrag.status
            error = nil
        } catch {
            meldung = Session.text(error)
            return
        }
        await load()
    }

    private var alleLeer: Bool {
        guard let d = daten else { return true }
        return teile.isEmpty && doppelt.isEmpty && (d.missing ?? []).isEmpty
            && (d.unmatched ?? []).isEmpty && aufgeloest.isEmpty
    }

    private func load() async {
        error = nil
        do {
            daten = try await session.verwaltet { try await session.client.adminCleanup() }
            reihen = (try? await session.verwaltet { try await session.client.adminCollections() }) ?? []
        } catch {
            self.error = Session.text(error)
        }
        loading = false
    }

    private func zurueckholen(_ r: ReiheZeile) async {
        do {
            try await session.verwaltet { try await session.client.adminRestoreCollection(r.id) }
            await load()
        } catch {
            self.error = Session.text(error)
        }
    }
}

// MARK: - Konten und Einladungen

struct KontenView: View {
    @Environment(Session.self) private var session
    @State private var konten: [KontoEintrag] = []
    @State private var loading = true
    @State private var error: String?
    @State private var link: String?
    @State private var code: String?
    @State private var laedtEinladung = false

    var body: some View {
        List {
            Section {
                ForEach(konten) { k in
                    // Der Besitzer hat keine Grenzen; alle anderen lassen
                    // sich öffnen: Bibliotheken und Grenzen am Konto.
                    if k.isOwner {
                        kontoZeile(k)
                    } else {
                        NavigationLink {
                            KontoRechteView(konto: k) { await load() }
                        } label: {
                            kontoZeile(k)
                        }
                    }
                }
            } footer: {
                Label("Du legst fest, was jemand sehen darf. Was er davon schaut, siehst du nicht.",
                      systemImage: "lock")
                    .font(.caption)
            }

            Section {
                Button(laedtEinladung ? "Einen Moment …" : "Einladungslink erzeugen") {
                    Task { await einladen() }
                }
                .disabled(laedtEinladung)
                if let link {
                    LabeledContent("Link") {
                        HStack {
                            Text(link).font(.system(.caption, design: .monospaced)).textSelection(.enabled)
                            Button("Kopieren") { kopieren(link) }
                        }
                    }
                }
                if let code {
                    LabeledContent("Oder nur der Code") {
                        HStack {
                            Text(code).font(.system(.body, design: .monospaced)).textSelection(.enabled)
                            Button("Kopieren") { kopieren(code) }
                        }
                    }
                }
            } header: {
                Text("Jemanden einladen")
            } footer: {
                Text("Gilt sieben Tage und nur einmal. Was jemand sehen darf, stellst du danach am Konto ein — oder für mehrere zusammen unter „Gruppen“.")
            }
        }
        .overlay { if konten.isEmpty { Loading(loading: loading, error: error, retry: load) } }
        .navigationTitle("Konten und Rechte")
        .task { await load() }
    }

    private func kontoZeile(_ k: KontoEintrag) -> some View {
        HStack(spacing: 12) {
            ProfilAvatar(name: k.name, avatar: k.avatar, accountId: k.id, size: 32)
            VStack(alignment: .leading, spacing: 2) {
                Text(k.name)
                Text(zeile(k)).font(.caption).foregroundStyle(Theme.label2)
            }
        }
        .padding(.vertical, 3)
    }

    /// „Haushalt · Gruppe „Familie" · 2 Bibliotheken · 3 Geräte · höchstens 2 gleichzeitig"
    private func zeile(_ k: KontoEintrag) -> String {
        var teile = [k.isOwner ? String(localized: "Besitzer", bundle: .lumen, locale: .lumen) : k.isKid ? String(localized: "Kinderprofil", bundle: .lumen, locale: .lumen) : String(localized: "Haushalt", bundle: .lumen, locale: .lumen)]
        if let g = k.groupName, !g.isEmpty {
            teile.append(k.isAdmin == true ? String(localized: "Gruppe „\(g)“ (verwaltet mit)", bundle: .lumen, locale: .lumen) : String(localized: "Gruppe „\(g)“", bundle: .lumen, locale: .lumen))
        }
        // Einzahl und Mehrzahl stehen im Katalog.
        let libs = (k.libraries ?? []).count
        teile.append(String(localized: "\(libs) Bibliotheken", bundle: .lumen, locale: .lumen))
        teile.append(String(localized: "\(k.devices) Geräte", bundle: .lumen, locale: .lumen))
        teile.append(String(localized: "höchstens \(k.limits.maxStreams) gleichzeitig", bundle: .lumen, locale: .lumen))
        return teile.joined(separator: " · ")
    }

    private func load() async {
        error = nil
        do { konten = try await session.verwaltet { try await session.client.adminAccounts() } }
        catch { self.error = Session.text(error) }
        loading = false
    }

    /// Wie im Browser: eine Einladung zu allen Bibliotheken; was jemand
    /// genau sehen darf, stellt man danach am Konto ein.
    private func einladen() async {
        laedtEinladung = true
        defer { laedtEinladung = false }
        do {
            let libs = try await session.verwaltet { try await session.client.adminLibraries() }.map(\.id)
            let inv = try await session.verwaltet { try await session.client.adminCreateInvite(libraries: libs) }
            code = inv.code
            let basis = await session.client.baseURL
            var teile = URLComponents(url: basis, resolvingAgainstBaseURL: false)
            teile?.path = "/"
            teile?.queryItems = [URLQueryItem(name: "einladung", value: inv.code)]
            link = teile?.url?.absoluteString
        } catch {
            self.error = Session.text(error)
        }
    }

    private func kopieren(_ text: String) {
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(text, forType: .string)
    }
}

// MARK: - Sicherung

/// Die zwei Sicherungen der Datenbank (sicherung.go): ansehen, jetzt
/// sichern, eine zurückholen. Nur für den Besitzer — der Server lässt
/// Mitverwalter hier gar nicht erst hinein, wie bei den Updates.
///
/// Wiederherstellen ersetzt den ganzen Stand: Konten, Listen, wo jemand im
/// Film steht. Darum die Rückfrage, und darum startet der Server danach
/// neu. Die Ansicht wartet das ab, statt in der Zwischenzeit „nicht
/// erreichbar" zu melden.
struct SicherungView: View {
    @Environment(Session.self) private var session
    @State private var daten: Sicherungen?
    @State private var loading = true
    @State private var error: String?
    @State private var sichertGerade = false
    /// Die Sicherung, für die gerade die Rückfrage offen ist.
    @State private var zuWiederherstellen: Sicherung?
    /// Was der Server zum Wiederherstellen sagt, und danach, ob er zurück ist.
    @State private var status: String?
    @State private var neustart: Task<Void, Never>?

    private var startetNeu: Bool { neustart != nil }

    var body: some View {
        List {
            if let status {
                Section {
                    HStack(spacing: 10) {
                        if startetNeu { ProgressView().controlSize(.small) }
                        Text(status)
                    }
                }
            }

            if let d = daten {
                Section {
                    if d.liste.isEmpty {
                        Text("Noch keine Sicherung. Lumen sichert jede Nacht nach halb fünf — oder jetzt, mit dem Knopf unten.")
                            .foregroundStyle(Theme.label2)
                    }
                    ForEach(d.liste) { s in
                        HStack {
                            VStack(alignment: .leading, spacing: 2) {
                                Text(s.name)
                                Text("\(s.tagText()) · \(s.groesse)")
                                    .font(.caption).foregroundStyle(Theme.label2)
                            }
                            Spacer()
                            Button("Wiederherstellen") { zuWiederherstellen = s }
                                .disabled(startetNeu || sichertGerade)
                        }
                        .padding(.vertical, 3)
                    }
                } header: {
                    Text("Sicherungen")
                } footer: {
                    if let h = d.hinweis, !h.isEmpty { Text(h) }
                }

                Section {
                    if let f = d.fehler, !f.isEmpty {
                        Label("Die letzte Sicherung ist gescheitert: \(f)",
                              systemImage: "exclamationmark.triangle")
                            .foregroundStyle(Theme.bad)
                    }
                    if let z = d.zuletzt, !z.isEmpty {
                        LabeledContent("Zuletzt gesichert", value: Sicherung.tagText(z))
                    }
                    Button(sichertGerade ? "Sichert …" : "Jetzt sichern") {
                        Task { await jetztSichern() }
                    }
                    .disabled(sichertGerade || startetNeu)
                }
            }
        }
        .overlay {
            if daten == nil { Loading(loading: loading, error: error, retry: load) }
        }
        .navigationTitle("Sicherung")
        .task { await load() }
        // Den Neustart nicht weiter abwarten, wenn niemand mehr hinschaut.
        // Der Server startet trotzdem; beim nächsten Öffnen ist er da.
        .onDisappear { neustart?.cancel(); neustart = nil }
        .confirmationDialog("Sicherung wiederherstellen?",
                            isPresented: Binding(get: { zuWiederherstellen != nil },
                                                 set: { if !$0 { zuWiederherstellen = nil } }),
                            presenting: zuWiederherstellen) { s in
            Button("Stand vom \(s.tagText()) wiederherstellen", role: .destructive) {
                zuWiederherstellen = nil
                Task { await wiederherstellen(s) }
            }
            Button("Abbrechen", role: .cancel) { zuWiederherstellen = nil }
        } message: { s in
            Text("Lumen startet dafür neu und ist ein paar Sekunden nicht erreichbar. Der jetzige Stand wird durch den vom \(s.tagText()) ersetzt — was seitdem dazukam (Konten, Listen, wo jemand im Film steht), ist danach weg. Laufende Wiedergaben brechen ab.")
        }
    }

    private func load() async {
        error = nil
        do { daten = try await session.verwaltet { try await session.client.adminBackups() } }
        catch { self.error = Session.text(error) }
        loading = false
    }

    private func jetztSichern() async {
        sichertGerade = true
        defer { sichertGerade = false }
        do {
            daten = try await session.verwaltet { try await session.client.adminBackupNow() }
        } catch {
            // Scheitert die Sicherung, antwortet der Server mit 500 und
            // merkt sich den Fehler. Neu laden zeigt ihn in der roten Zeile,
            // und ist er nicht erreichbar, sagt der Status es.
            status = Session.text(error)
            await load()
        }
    }

    private func wiederherstellen(_ s: Sicherung) async {
        do {
            let antwort = try await session.verwaltet { try await session.client.adminRestoreBackup(s.nr) }
            status = antwort.status
        } catch {
            status = Session.text(error)
            return
        }
        neustart = Task { await neustartAbwarten() }
    }

    /// Der Server antwortet, wartet eine Sekunde und startet neu. Erst
    /// abwarten, bis er weg ist — sonst hielte die App den alten Prozess
    /// für den neuen —, dann, bis er wieder da ist. `hello` braucht weder
    /// Anmeldung noch Verwaltungssperre und eignet sich darum zum Klopfen.
    private func neustartAbwarten() async {
        let client = session.client
        // Höchstens acht Sekunden auf das Verschwinden warten. Wer schneller
        // neu startet, als wir klopfen, ist danach einfach gleich wieder da.
        for _ in 0..<16 {
            if Task.isCancelled { return }
            if (try? await client.hello()) == nil { break }
            try? await Task.sleep(for: .milliseconds(500))
        }
        // Dann bis zu anderthalb Minuten auf das Zurückkommen. Eine grosse
        // Datenbank prüft der Server beim Start, das kann dauern.
        var zurueck = false
        for _ in 0..<60 {
            if Task.isCancelled { return }
            if (try? await client.hello()) != nil { zurueck = true; break }
            try? await Task.sleep(for: .milliseconds(1500))
        }
        if Task.isCancelled { return }
        neustart = nil
        if zurueck {
            status = String(localized: "Lumen ist wieder da und arbeitet mit der Sicherung.", bundle: .lumen, locale: .lumen)
            // Nach dem Neustart kann die Verwaltungssperre wieder gelten —
            // dann fragt `verwaltet` beim Laden ganz normal nach dem Code.
            await load()
        } else {
            status = String(localized: "Lumen antwortet noch nicht. Läuft der Server? Öffne die Seite in einer Minute noch einmal.", bundle: .lumen, locale: .lumen)
        }
    }
}

// MARK: - Uhrzeiten

/// Der Server schreibt Uhrzeiten als „HH:MM"; ein DatePicker will ein
/// Datum. Nur Stunde und Minute zählen — der Tag ist heute und egal.
enum Uhrzeit {
    static func datum(_ text: String) -> Date {
        let teile = text.split(separator: ":").compactMap { Int($0) }
        let stunde = teile.count > 0 ? teile[0] : 4
        let minute = teile.count > 1 ? teile[1] : 0
        return Calendar.current.date(bySettingHour: min(max(stunde, 0), 23),
                                     minute: min(max(minute, 0), 59),
                                     second: 0, of: Date()) ?? Date()
    }

    static func text(_ datum: Date) -> String {
        let z = Calendar.current.dateComponents([.hour, .minute], from: datum)
        return String(format: "%02d:%02d", z.hour ?? 0, z.minute ?? 0)
    }

    /// Ein „HH:MM"-Feld als Datum für einen DatePicker.
    static func bindung(_ wert: Binding<String>) -> Binding<Date> {
        Binding(get: { datum(wert.wrappedValue) }, set: { wert.wrappedValue = text($0) })
    }
}

/// „12. Okt. 2026, 04:01" — oder ein Strich, wenn es noch nichts gibt.
private func zeitText(_ datum: Date?) -> String {
    datum?.formatted(Date.FormatStyle(date: .abbreviated, time: .shortened).locale(.lumen)) ?? "–"
}

// MARK: - Grenzen

/// Die Grenzen eines Kontos oder einer Gruppe zum Bearbeiten.
struct GrenzenEntwurf: Equatable {
    var maxStreams = 2
    var maxAge = 18
    var maxDownloads = 20
    var allowUnrated = true
    var allowTranscode = true
    var allowDownload = true
    /// Wird nicht hier eingestellt, aber beim Speichern mitgeschickt.
    var remoteQuality = "1080p"

    init() {}

    init(_ l: Limits) {
        maxStreams = l.maxStreams
        maxAge = l.maxAge
        maxDownloads = l.maxDownloads
        allowUnrated = l.allowUnrated
        allowTranscode = l.allowTranscode
        allowDownload = l.allowDownload
        remoteQuality = l.remoteQuality.isEmpty ? "1080p" : l.remoteQuality
    }

    var limits: Limits {
        Limits(maxAge: maxAge, allowUnrated: allowUnrated, maxStreams: maxStreams,
               allowTranscode: allowTranscode, allowDownload: allowDownload,
               maxDownloads: maxDownloads, remoteQuality: remoteQuality)
    }
}

/// Dieselben Felder wie im Browser: gleichzeitig, Altersfreigabe,
/// Downloads, und was jemand darf.
struct GrenzenFelder: View {
    @Binding var entwurf: GrenzenEntwurf

    var body: some View {
        Stepper("Gleichzeitig: \(entwurf.maxStreams)", value: $entwurf.maxStreams, in: 1...20)
        // -1 heisst „ohne Grenze"; 0 ist „nur FSK 0" (früher hiess 0 „alle").
        Picker("Bis Altersfreigabe", selection: $entwurf.maxAge) {
            ForEach([-1, 0, 6, 12, 16, 18], id: \.self) { n in
                Text(n < 0 ? "ohne Grenze" : "bis FSK \(n)").tag(n)
            }
        }
        Stepper("Downloads: \(entwurf.maxDownloads)", value: $entwurf.maxDownloads, in: 0...500, step: 5)
        Toggle("Ungeprüfte Titel", isOn: $entwurf.allowUnrated)
        Toggle("Umwandeln", isOn: $entwurf.allowTranscode)
        Toggle("Herunterladen", isOn: $entwurf.allowDownload)
    }
}

// MARK: - Rechte eines Kontos

/// Bibliotheken und Grenzen eines einzelnen Kontos. Steckt es in einer
/// Gruppe, ruhen diese Einträge, solange es drin ist. Speichern verlangt
/// eine Bestätigung — der Dialog dazu kommt von selbst.
struct KontoRechteView: View {
    @Environment(Session.self) private var session
    @Environment(\.dismiss) private var dismiss
    let konto: KontoEintrag
    let fertig: @MainActor () async -> Void

    @State private var bibliotheken: [Library] = []
    @State private var auswahl: Set<String>
    @State private var grenzen: GrenzenEntwurf
    @State private var fehler: String?
    @State private var speichert = false
    @State private var rolle: String

    init(konto: KontoEintrag, fertig: @escaping @MainActor () async -> Void) {
        self.konto = konto
        self.fertig = fertig
        _rolle = State(initialValue: konto.rolle)
        _auswahl = State(initialValue: Set(konto.libraries ?? []))
        _grenzen = State(initialValue: GrenzenEntwurf(konto.limits))
    }

    var body: some View {
        Form {
            if session.isOwner {
                Section {
                    Picker("Rechte", selection: $rolle) {
                        Text("Nur schauen").tag("schauen")
                        Text("Verwaltet mit").tag("verwalten")
                        Text("Vollzugriff").tag("voll")
                    }
                    .onChange(of: rolle) { _, neu in
                        guard neu != konto.rolle else { return }
                        Task {
                            do {
                                _ = try await session.verwaltet {
                                    try await session.client.adminSetRolle(konto.id, rolle: neu)
                                }
                                await fertig()
                            } catch {
                                fehler = Session.text(error)
                                rolle = konto.rolle
                            }
                        }
                    }
                } footer: {
                    Text("Verwaltet mit: Bibliotheken prüfen, Konten ohne Verwaltung, Wünsche, Protokoll. Vollzugriff: alles wie der Besitzer — auch Gruppen, Rechte, Updates und Sicherungen. Wer was schaut, sieht niemand.")
                        .font(.caption)
                }
            }
            if let g = konto.groupName, !g.isEmpty {
                Section {
                    Label("Steckt in der Gruppe „\(g)“. Solange gelten deren Bibliotheken und Grenzen; was hier steht, ruht.", systemImage: "person.3")
                        .foregroundStyle(Theme.label2)
                }
            }
            Section("Bibliotheken") {
                if bibliotheken.isEmpty {
                    Text("Noch keine Bibliotheken.").foregroundStyle(Theme.label2)
                }
                ForEach(bibliotheken) { l in
                    Toggle(l.name, isOn: Binding(
                        get: { auswahl.contains(l.id) },
                        set: { an in if an { auswahl.insert(l.id) } else { auswahl.remove(l.id) } }))
                }
            }
            Section("Grenzen") {
                GrenzenFelder(entwurf: $grenzen)
            }
            // Den eigenen Schutz ändert jede Person selbst (Profil →
            // Profil schützen); der des Besitzers lässt sich hier nicht
            // zurücksetzen.
            if !konto.isOwner, konto.id != session.me?.account.id {
                SperreZuruecksetzen(konto: konto)
            }
            if let fehler {
                Section { Text(fehler).foregroundStyle(Theme.bad) }
            }
            Section {
                HStack {
                    Spacer()
                    Button(speichert ? "Speichert …" : "Speichern") { Task { await speichern() } }
                        .buttonStyle(.borderedProminent)
                        .keyboardShortcut(.defaultAction)
                        .disabled(speichert)
                }
            } footer: {
                Label("Du legst fest, was \(konto.name) sehen darf. Was davon geschaut wird, siehst du nicht.",
                      systemImage: "lock")
                    .font(.caption)
            }
        }
        .formStyle(.grouped)
        .navigationTitle(konto.name)
        .task {
            bibliotheken = (try? await session.verwaltet { try await session.client.adminLibraries() }) ?? []
        }
    }

    private func speichern() async {
        speichert = true
        defer { speichert = false }
        // In der Reihenfolge der Bibliotheken, nicht in der der Klicks.
        let libs = bibliotheken.map(\.id).filter { auswahl.contains($0) }
        let limits = grenzen.limits
        let id = konto.id
        do {
            try await session.verwaltet {
                try await session.client.adminSetAccountLimits(id, limits: limits, libraries: libs)
            }
            fehler = nil
            await fertig()
            dismiss()
        } catch {
            fehler = Session.text(error)
        }
    }
}

// MARK: - Gruppen

/// Gruppen bündeln Rechte (app.js: viewAdminGroups). Nur der Besitzer.
/// Anlegen, Ändern, Auflösen und Zuordnen verlangen eine Bestätigung —
/// PIN, Passwort oder Code noch einmal; den Dialog bringt
/// `session.verwaltet` von selbst.
struct GruppenView: View {
    @Environment(Session.self) private var session
    @State private var gruppen: [Gruppe] = []
    @State private var bibliotheken: [Library] = []
    @State private var konten: [KontoEintrag] = []
    @State private var loading = true
    @State private var error: String?
    @State private var meldung: String?

    /// Die Gruppe, die gerade bearbeitet wird — nil: eine neue.
    @State private var bearbeitet: Gruppe?
    @State private var name = ""
    /// "schauen", "verwalten" oder "voll".
    @State private var rechte = "schauen"
    @State private var auswahl: Set<String> = []
    @State private var grenzen = GrenzenEntwurf()
    @State private var speichert = false
    /// Was im Menü einer Person gewählt ist, bis der Server es bestätigt.
    @State private var zuordnung: [String: String] = [:]

    private var andere: [KontoEintrag] { konten.filter { !$0.isOwner } }

    var body: some View {
        Form {
            Section {
                Label("Wer in einer Gruppe ist, bekommt deren Bibliotheken und Grenzen; die eigenen Einträge am Konto ruhen solange. Was jemand schaut, sieht keine Gruppe — auch keine mit Verwaltung.", systemImage: "lock")
                    .font(.callout)
                    .foregroundStyle(Theme.label2)
            }
            if let meldung {
                Section { Label(meldung, systemImage: "info.circle") }
            }
            Section("Gruppen") {
                if gruppen.isEmpty {
                    Text("Noch keine Gruppe.").foregroundStyle(Theme.label2)
                }
                ForEach(gruppen) { g in gruppenZeile(g) }
            }
            formular
            if !andere.isEmpty {
                Section("Wer gehört wohin") {
                    ForEach(andere) { k in zuordnungZeile(k) }
                }
            }
        }
        .formStyle(.grouped)
        .overlay {
            if gruppen.isEmpty && konten.isEmpty {
                Loading(loading: loading, error: error, retry: load)
            }
        }
        .navigationTitle("Gruppen")
        .task { await load() }
    }

    private func gruppenZeile(_ g: Gruppe) -> some View {
        HStack(spacing: 12) {
            VStack(alignment: .leading, spacing: 2) {
                HStack(spacing: 8) {
                    Text(g.name)
                    if g.vollzugriff == true {
                        Text("Vollzugriff")
                            .font(.caption2.weight(.medium))
                            .foregroundStyle(.orange)
                            .padding(.horizontal, 7).padding(.vertical, 2)
                            .background(Capsule().fill(Color.orange.opacity(0.18)))
                    } else if g.verwaltung {
                        Text("Verwaltung")
                            .font(.caption2.weight(.medium))
                            .foregroundStyle(Theme.accent)
                            .padding(.horizontal, 7).padding(.vertical, 2)
                            .background(Capsule().fill(Theme.accent.opacity(0.16)))
                    }
                }
                Text(g.zeile).font(.caption).foregroundStyle(Theme.label2)
            }
            Spacer()
            Button("Bearbeiten") { bearbeiten(g) }
            Button("Auflösen", role: .destructive) { Task { await aufloesen(g) } }
        }
        .controlSize(.small)
        .padding(.vertical, 3)
    }

    @ViewBuilder
    private var formular: some View {
        Section {
            TextField("Name", text: $name, prompt: Text("z. B. Familie, Gäste, Mitverwalter"))
            Picker("Rechte in der Verwaltung", selection: $rechte) {
                Text("Keine — nur schauen").tag("schauen")
                Text("Verwaltet mit — alles ausser Gruppen, Rechten, Updates").tag("verwalten")
                Text("Vollzugriff — alles wie der Besitzer").tag("voll")
            }
            Text("Wer was schaut, sieht auch mit Vollzugriff niemand.")
                .font(.caption).foregroundStyle(Theme.label2)
        } header: {
            Text(bearbeitet.map { String(localized: "„\($0.name)“ bearbeiten", bundle: .lumen, locale: .lumen) } ?? String(localized: "Neue Gruppe", bundle: .lumen, locale: .lumen))
        }
        Section("Bibliotheken") {
            if bibliotheken.isEmpty {
                Text("Noch keine Bibliotheken.").foregroundStyle(Theme.label2)
            }
            ForEach(bibliotheken) { l in
                Toggle(l.name, isOn: Binding(
                    get: { auswahl.contains(l.id) },
                    set: { an in if an { auswahl.insert(l.id) } else { auswahl.remove(l.id) } }))
            }
        }
        Section("Grenzen") {
            GrenzenFelder(entwurf: $grenzen)
        }
        Section {
            HStack {
                Spacer()
                if bearbeitet != nil {
                    Button("Abbrechen") { neu() }
                }
                Button(speichert ? "Speichert …" : bearbeitet == nil ? "Anlegen" : "Speichern") {
                    Task { await speichern() }
                }
                .buttonStyle(.borderedProminent)
                .disabled(speichert || name.trimmingCharacters(in: .whitespaces).isEmpty)
            }
        }
    }

    private func zuordnungZeile(_ k: KontoEintrag) -> some View {
        let wahl = Binding<String>(
            get: { zuordnung[k.id] ?? k.groupId ?? "" },
            set: { neu in
                zuordnung[k.id] = neu
                Task { await zuordnen(k, neu) }
            })
        return Picker(selection: wahl) {
            Text("keine Gruppe").tag("")
            ForEach(gruppen) { g in
                Text(g.name).tag(g.id)
            }
        } label: {
            HStack(spacing: 10) {
                ProfilAvatar(name: k.name, avatar: k.avatar, accountId: k.id, size: 26)
                Text(k.name)
            }
        }
    }

    private func bearbeiten(_ g: Gruppe) {
        bearbeitet = g
        name = g.name
        rechte = g.vollzugriff == true ? "voll" : g.verwaltung ? "verwalten" : "schauen"
        auswahl = Set(g.bibliotheken)
        grenzen = GrenzenEntwurf(g.limits)
    }

    /// Das Formular leeren — für eine neue Gruppe.
    private func neu() {
        bearbeitet = nil
        name = ""
        rechte = "schauen"
        auswahl = []
        grenzen = GrenzenEntwurf()
    }

    private func speichern() async {
        let n = name.trimmingCharacters(in: .whitespaces)
        guard !n.isEmpty else { return }
        speichert = true
        defer { speichert = false }
        let libs = bibliotheken.map(\.id).filter { auswahl.contains($0) }
        let limits = grenzen.limits
        let verw = rechte != "schauen"
        let voll = rechte == "voll"
        let id = bearbeitet?.id
        do {
            if let id {
                _ = try await session.verwaltet {
                    try await session.client.adminUpdateGroup(id, name: n, verwaltung: verw, vollzugriff: voll,
                                                              libraries: libs, limits: limits)
                }
            } else {
                _ = try await session.verwaltet {
                    try await session.client.adminCreateGroup(name: n, verwaltung: verw, vollzugriff: voll,
                                                              libraries: libs, limits: limits)
                }
            }
            meldung = "Gespeichert."
            neu()
            await load()
        } catch {
            meldung = Session.text(error)
        }
    }

    private func aufloesen(_ g: Gruppe) async {
        do {
            try await session.verwaltet { try await session.client.adminDeleteGroup(g.id) }
            meldung = String(localized: "Gruppe aufgelöst — die Konten behalten ihre eigenen Einträge.", bundle: .lumen, locale: .lumen)
            if bearbeitet?.id == g.id { neu() }
            await load()
        } catch {
            meldung = Session.text(error)
        }
    }

    private func zuordnen(_ k: KontoEintrag, _ gruppe: String) async {
        do {
            try await session.verwaltet { try await session.client.adminSetAccountGroup(k.id, groupId: gruppe) }
            meldung = gruppe.isEmpty ? String(localized: "\(k.name) ist aus der Gruppe genommen.", bundle: .lumen, locale: .lumen) : String(localized: "\(k.name) ist zugeordnet.", bundle: .lumen, locale: .lumen)
        } catch {
            meldung = Session.text(error)
        }
        // Abgebrochen oder geklappt: was gilt, sagt der Server.
        await load()
        zuordnung[k.id] = nil
    }

    private func load() async {
        error = nil
        do {
            gruppen = try await session.verwaltet { try await session.client.adminGroups() }
            bibliotheken = try await session.verwaltet { try await session.client.adminLibraries() }
            konten = try await session.verwaltet { try await session.client.adminAccounts() }
        } catch {
            self.error = Session.text(error)
        }
        loading = false
    }
}

// MARK: - Updates

/// Neue Fassungen von Lumen — nachsehen, einsetzen, planen (app.js:
/// viewAdminUpdate). Nur der Besitzer. Von den Wiedergaben
/// zählt nur die Zahl, keine Namen.
struct UpdatesView: View {
    @Environment(Session.self) private var session
    @State private var stand: UpdateStand?
    @State private var loading = true
    @State private var error: String?
    @State private var meldung: String?
    @State private var arbeitet = false
    /// Laufen Wiedergaben, fragt die Ansicht vor dem Aktualisieren nach.
    @State private var trotzdemFrage = false

    // Die Einstellungen, wie sie im Formular stehen.
    @State private var pruefen = "taeglich"
    @State private var pruefZeit = "04:00"
    @State private var auto = "aus"
    @State private var fensterVon = "03:00"
    @State private var fensterBis = "05:00"
    @State private var token = ""

    var body: some View {
        Form {
            if let meldung {
                Section { Label(meldung, systemImage: "info.circle") }
            }
            if let u = stand {
                Section("Stand") {
                    standZeile(u)
                    HStack(spacing: 10) {
                        Button("Jetzt prüfen") { Task { await pruefenJetzt() } }
                            .disabled(arbeitet)
                        if let url = URL(string: u.releaseSeite) {
                            Link("Alle Veröffentlichungen", destination: url)
                        }
                        Spacer()
                        Text(wiedergabenText(u.wiedergaben))
                            .font(.caption).foregroundStyle(Theme.label2)
                    }
                    .controlSize(.small)
                }

                Section {
                    HStack {
                        Picker("Täglich nachsehen", selection: $pruefen) {
                            Text("aus").tag("aus")
                            Text("täglich um").tag("taeglich")
                        }
                        DatePicker("Uhrzeit", selection: Uhrzeit.bindung($pruefZeit),
                                   displayedComponents: .hourAndMinute)
                            .labelsHidden()
                            .fixedSize()
                            .disabled(pruefen == "aus")
                    }
                    HStack {
                        Picker("Von selbst aktualisieren", selection: $auto) {
                            Text("aus").tag("aus")
                            Text("nur im Zeitfenster").tag("fenster")
                        }
                        Text("von").foregroundStyle(Theme.label2)
                        DatePicker("von", selection: Uhrzeit.bindung($fensterVon),
                                   displayedComponents: .hourAndMinute)
                            .labelsHidden()
                            .fixedSize()
                        Text("bis").foregroundStyle(Theme.label2)
                        DatePicker("bis", selection: Uhrzeit.bindung($fensterBis),
                                   displayedComponents: .hourAndMinute)
                            .labelsHidden()
                            .fixedSize()
                    }
                    .disabled(u.container)
                } header: {
                    Text("Von selbst")
                } footer: {
                    Text(u.container
                         ? "Läuft im Fenster eine Wiedergabe, wartet Lumen und versucht es später nochmal. Im Container aktualisiert man das Abbild von aussen — darum ist das hier aus."
                         : "Läuft im Fenster eine Wiedergabe, wartet Lumen und versucht es später nochmal.")
                }

                Section {
                    if u.tokenUngueltig {
                        Text("GitHub nimmt das Token nicht an — meist ist es abgelaufen. Trag hier ein neues ein, dann kommen die Updates wieder durch. Bist du nicht Besitzer des Repositorys, nimm ein klassisches Token mit dem Bereich „repo“ — feingranulare gelten nur für eigene Repositories.")
                            .foregroundStyle(Theme.bad)
                    }
                    SecureField("GitHub-Token", text: $token,
                                prompt: Text(u.tokenGesetzt ? "neues Token, ghp_… oder github_pat_…"
                                                            : "ghp_… oder github_pat_…"))
                    Text(tokenHinweis(u)).font(.caption).foregroundStyle(Theme.label2)
                    if u.tokenQuelle == "verwaltung" {
                        Button("Token entfernen", role: .destructive) {
                            Task { await speichern(token: "") }
                        }
                        .disabled(arbeitet)
                    }
                } header: {
                    Text("Zugang")
                }

                Section {
                    HStack {
                        Spacer()
                        Button("Speichern") {
                            let t = token.trimmingCharacters(in: .whitespacesAndNewlines)
                            Task { await speichern(token: t.isEmpty ? nil : t) }
                        }
                        .buttonStyle(.borderedProminent)
                        .disabled(arbeitet)
                    }
                }

                Section("Protokoll") {
                    let protokoll = u.protokoll ?? []
                    if protokoll.isEmpty {
                        Text("Noch keine Aktualisierung.").foregroundStyle(Theme.label2)
                    }
                    ForEach(protokoll) { p in protokollZeile(p) }
                }
            }
        }
        .formStyle(.grouped)
        .overlay {
            if stand == nil { Loading(loading: loading, error: error, retry: load) }
        }
        .navigationTitle("Updates")
        .task { await load() }
        .confirmationDialog(wiedergabenFrage, isPresented: $trotzdemFrage, titleVisibility: .visible) {
            Button("Trotzdem") { Task { await aktualisieren(trotzdem: true) } }
            Button("Lieber später", role: .cancel) {}
        } message: {
            Text("Beim Neustart bricht sie ab. Trotzdem jetzt aktualisieren?")
        }
    }

    @ViewBuilder
    private func standZeile(_ u: UpdateStand) -> some View {
        if u.gibtNeueres, let n = u.neueste {
            HStack(alignment: .top, spacing: 12) {
                Image(systemName: "exclamationmark.triangle.fill").foregroundStyle(Theme.warn)
                VStack(alignment: .leading, spacing: 4) {
                    Text("Lumen \(n.version) ist da — du hast \(u.installiert)")
                    HStack(spacing: 6) {
                        Text("veröffentlicht \(zeitText(n.published))")
                        if let url = URL(string: n.url) {
                            Link("Was sich geändert hat", destination: url)
                        }
                    }
                    .font(.caption).foregroundStyle(Theme.label2)
                    if u.container, let anleitung = u.anleitung {
                        Text(anleitung)
                            .font(.caption).foregroundStyle(Theme.label2)
                            .textSelection(.enabled)
                    }
                }
                Spacer()
                // Im Container tauscht man das Abbild von aussen; die
                // Anleitung steht oben.
                if !u.container {
                    Button(u.laeuft ? "Läuft …" : "Jetzt aktualisieren") {
                        Task { await aktualisieren(trotzdem: false) }
                    }
                    .buttonStyle(.borderedProminent)
                    .disabled(u.laeuft || arbeitet)
                }
            }
        } else {
            HStack(alignment: .top, spacing: 12) {
                Image(systemName: u.fehler != nil ? "exclamationmark.triangle.fill" : "checkmark.circle.fill")
                    .foregroundStyle(u.fehler != nil ? Theme.bad : Theme.good)
                VStack(alignment: .leading, spacing: 4) {
                    Text(standText(u))
                    Text(u.fehler ?? String(localized: "zuletzt geprüft \(zeitText(u.geprueft))", bundle: .lumen, locale: .lumen))
                        .font(.caption).foregroundStyle(Theme.label2)
                }
            }
        }
    }

    private func standText(_ u: UpdateStand) -> String {
        guard u.fehler == nil, let n = u.neueste else { return "Lumen \(u.installiert)" } // sprache: fest
        if u.installiert == "dev" {
            return String(localized: "Lumen dev — aus dem Quelltext gebaut, neueste Veröffentlichung ist \(n.version)", bundle: .lumen, locale: .lumen)
        }
        return String(localized: "Lumen \(u.installiert) — auf dem neuesten Stand", bundle: .lumen, locale: .lumen)
    }

    private func tokenHinweis(_ u: UpdateStand) -> String {
        if u.tokenQuelle == "umgebung" {
            return String(localized: "Kommt aus LUMEN_GITHUB_TOKEN (docker-compose.yml). Ein Token hier hat Vorrang — so lässt sich ein abgelaufenes ersetzen, ohne den Container anzufassen.", bundle: .lumen, locale: .lumen)
        }
        if u.tokenGesetzt {
            return String(localized: "Ist eingetragen. Ein neues ersetzt das alte; es wird gespeichert, aber nie wieder angezeigt.", bundle: .lumen, locale: .lumen)
        }
        return String(localized: "Optional — nur nötig, wenn die Updates aus einem privaten Repository kommen. Wird gespeichert, aber nie wieder angezeigt.", bundle: .lumen, locale: .lumen)
    }

    private func protokollZeile(_ p: UpdateEintrag) -> some View {
        let farbe: Color = p.ergebnis == "ok" ? Theme.good : p.ergebnis == "fehler" ? Theme.bad : Theme.label2
        return VStack(alignment: .leading, spacing: 3) {
            HStack(spacing: 10) {
                Text(zeitText(p.at))
                    .font(.caption).monospacedDigit().foregroundStyle(Theme.label2)
                    .frame(width: 140, alignment: .leading)
                Text("\(p.von) → \(p.nach)")
                if p.auto {
                    Text("von selbst").font(.caption).foregroundStyle(Theme.label2)
                }
                Spacer()
                Text(p.ergebnisText)
                    .font(.caption2.weight(.medium))
                    .foregroundStyle(farbe)
                    .padding(.horizontal, 7).padding(.vertical, 3)
                    .background(Capsule().fill(farbe.opacity(0.16)))
            }
            if let notiz = p.notiz, !notiz.isEmpty {
                Text(notiz).font(.caption).foregroundStyle(Theme.label2)
                    .padding(.leading, 150)
            }
        }
        .padding(.vertical, 2)
    }

    private func wiedergabenText(_ n: Int) -> String {
        switch n {
        case 0: String(localized: "gerade schaut niemand", bundle: .lumen, locale: .lumen)
        default: String(localized: "\(n) Wiedergaben laufen", bundle: .lumen, locale: .lumen)
        }
    }

    private var wiedergabenFrage: String {
        let n = stand?.wiedergaben ?? 0
        return n >= 1 ? String(localized: "\(n) Wiedergaben laufen gerade", bundle: .lumen, locale: .lumen) : String(localized: "Es laufen Wiedergaben", bundle: .lumen, locale: .lumen)
    }

    /// Den Stand zeigen und das Formular damit füllen.
    private func uebernehmen(_ u: UpdateStand) {
        stand = u
        pruefen = u.einstellung.pruefen
        pruefZeit = u.einstellung.pruefZeit
        auto = u.einstellung.auto
        fensterVon = u.einstellung.fensterVon
        fensterBis = u.einstellung.fensterBis
    }

    private func load() async {
        error = nil
        do {
            uebernehmen(try await session.verwaltet { try await session.client.adminUpdate() })
        } catch {
            // Steht schon ein Stand da, bleibt er — etwa während Lumen nach
            // dem Aktualisieren neu startet.
            if stand == nil { self.error = Session.text(error) } else { meldung = Session.text(error) }
        }
        loading = false
    }

    private func pruefenJetzt() async {
        arbeitet = true
        defer { arbeitet = false }
        meldung = String(localized: "Sieht bei GitHub nach …", bundle: .lumen, locale: .lumen)
        do {
            uebernehmen(try await session.verwaltet { try await session.client.adminUpdateCheck() })
            meldung = nil
        } catch {
            meldung = Session.text(error)
        }
    }

    /// `token`: nil lässt es, wie es ist; leer entfernt es.
    private func speichern(token neu: String?) async {
        arbeitet = true
        defer { arbeitet = false }
        let p = pruefen, pz = pruefZeit, a = auto, von = fensterVon, bis = fensterBis
        do {
            let u = try await session.verwaltet {
                try await session.client.adminUpdateSettings(pruefen: p, pruefZeit: pz, auto: a,
                                                             fensterVon: von, fensterBis: bis, token: neu)
            }
            uebernehmen(u)
            token = ""
            meldung = neu == "" ? String(localized: "Token entfernt.", bundle: .lumen, locale: .lumen) : "Gespeichert."
        } catch {
            meldung = Session.text(error)
        }
    }

    private func aktualisieren(trotzdem: Bool) async {
        arbeitet = true
        defer { arbeitet = false }
        do {
            let lauf = try await session.verwaltet { try await session.client.adminUpdateRun(trotzdem: trotzdem) }
            meldung = String(localized: "Lumen \(lauf.nach) wird eingesetzt — gleich startet es neu.", bundle: .lumen, locale: .lumen)
            try? await Task.sleep(for: .seconds(8))
            await load()
        } catch LumenError.server(_, 409) {
            // Es laufen Wiedergaben: die frische Zahl holen, dann fragen.
            await load()
            trotzdemFrage = true
        } catch {
            meldung = Session.text(error)
        }
    }
}

// MARK: - Anderen Titel zuweisen

/// Wenn Lumen danebenlag: bei der Quelle suchen und den richtigen Eintrag
/// wählen (app.js: zuweisenPanel). Die Suche versteht auch einen Link von
/// TMDb oder IMDb. Klappt auf der Titelseite unter den Knöpfen auf, statt
/// als Blatt — über einem Blatt käme der Entsperr-Dialog nicht hoch.
struct TitelZuweisenPanel: View {
    @Environment(Session.self) private var session
    let itemId: String
    let schliessen: @MainActor () -> Void
    let fertig: @MainActor () async -> Void

    @State private var suche: String
    @State private var jahr = ""
    @State private var treffer: [TitelTreffer] = []
    @State private var sucht = true
    @State private var fehler: String?
    @State private var weist = false

    init(itemId: String, suche: String,
         schliessen: @escaping @MainActor () -> Void,
         fertig: @escaping @MainActor () async -> Void) {
        self.itemId = itemId
        self.schliessen = schliessen
        self.fertig = fertig
        _suche = State(initialValue: suche)
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack(spacing: 8) {
                Image(systemName: "magnifyingglass").foregroundStyle(Theme.label2)
                TextField("Titel suchen, oder Link von TMDb/IMDb einfügen …", text: $suche)
                    .textFieldStyle(.roundedBorder)
                    .onSubmit { Task { await suchen() } }
                TextField("Jahr", text: $jahr)
                    .textFieldStyle(.roundedBorder)
                    .frame(width: 64)
                    .onSubmit { Task { await suchen() } }
                Button("Suchen") { Task { await suchen() } }
                Button("Zu") { schliessen() }
            }
            .controlSize(.small)
            .padding(12)
            Divider().overlay(Theme.hairline)

            if sucht {
                HStack(spacing: 8) {
                    ProgressView().controlSize(.small)
                    Text("Suche …").font(.callout).foregroundStyle(Theme.label2)
                }
                .padding(12)
            } else if let fehler {
                Text(fehler).font(.callout).foregroundStyle(Theme.label2).padding(12)
            } else if treffer.isEmpty {
                Text("Nichts gefunden. Anders schreiben, oder das Jahr weglassen.")
                    .font(.callout).foregroundStyle(Theme.label2).padding(12)
            } else {
                VStack(alignment: .leading, spacing: 0) {
                    ForEach(treffer) { t in
                        Button { Task { await nehmen(t) } } label: { trefferZeile(t) }
                            .buttonStyle(.plain)
                            .disabled(weist)
                        if t.id != treffer.last?.id {
                            Divider().overlay(Theme.hairline)
                        }
                    }
                }
            }
        }
        .frame(maxWidth: 640, alignment: .leading)
        .background(RoundedRectangle(cornerRadius: 12, style: .continuous).fill(Theme.surface))
        .task { await suchen() }
    }

    private func trefferZeile(_ t: TitelTreffer) -> some View {
        HStack(spacing: 12) {
            AsyncImage(url: t.poster.flatMap { URL(string: $0) }) { bild in
                bild.resizable().scaledToFill()
            } placeholder: {
                Theme.fill
            }
            .frame(width: 40, height: 60)
            .clipShape(RoundedRectangle(cornerRadius: 6, style: .continuous))
            VStack(alignment: .leading, spacing: 3) {
                HStack(spacing: 6) {
                    Text(t.title).lineLimit(1)
                    if let y = t.year, y > 0 {
                        Text("(\(String(y)))").foregroundStyle(Theme.label2)
                    }
                    if let o = t.original, !o.isEmpty, o != t.title {
                        Text("· \(o)").font(.caption).foregroundStyle(Theme.label2).lineLimit(1)
                    }
                }
                if let ueber = t.overview, !ueber.isEmpty {
                    Text(ueber).font(.caption).foregroundStyle(Theme.label2).lineLimit(2)
                }
            }
            Spacer()
            Text("Nehmen").font(.caption).foregroundStyle(Theme.accent)
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 8)
        .contentShape(Rectangle())
    }

    private func suchen() async {
        sucht = true
        fehler = nil
        let q = suche, j = jahr, id = itemId
        do {
            treffer = try await session.verwaltet { try await session.client.adminItemSearch(id, query: q, year: j) }
        } catch {
            fehler = Session.text(error)
        }
        sucht = false
    }

    private func nehmen(_ t: TitelTreffer) async {
        weist = true
        defer { weist = false }
        let id = itemId
        do {
            try await session.verwaltet { try await session.client.adminItemAssign(id, sourceId: t.sourceId) }
            await fertig()
        } catch {
            fehler = Session.text(error)
        }
    }
}

#endif
