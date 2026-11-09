// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

import SwiftUI
import LumenKit

// Listen sind das Einzige, was Leute im Haushalt voneinander zu sehen
// bekommen — und auch nur, weil die Person, der die Liste gehört, sie
// ausdrücklich teilt. Was jemand schaut, steht nirgends: weder wer eine
// Liste geöffnet hat noch wie weit jemand darin ist.

// MARK: - Alle Listen

struct PlaylistsView: View {
    @Environment(Session.self) private var session
    @State private var listen: [Liste] = []
    @State private var loading = true
    @State private var error: String?
    @State private var problem: String?
    @State private var askName = false
    @State private var newName = ""

    private var meine: [Liste] { listen.filter(\.meine) }
    private var geteilte: [Liste] { listen.filter { !$0.meine } }

    var body: some View {
        List {
            Section {
                ForEach(meine) { liste in
                    NavigationLink { PlaylistView(listeId: liste.id, name: liste.name) } label: {
                        PlaylistRow(liste: liste)
                    }
                }
                Button {
                    newName = ""
                    askName = true
                } label: {
                    Label("Neue Liste", systemImage: "plus")
                }
            } header: {
                Text("Meine Listen")
            } footer: {
                Text("Deine Listen sieht nur du — ausser du zeigst eine davon im Haushalt.")
            }

            if !geteilte.isEmpty {
                Section("Im Haushalt geteilt") {
                    ForEach(geteilte) { liste in
                        NavigationLink { PlaylistView(listeId: liste.id, name: liste.name) } label: {
                            PlaylistRow(liste: liste)
                        }
                    }
                }
            }
        }
        .overlay { Loading(loading: loading, error: error, retry: load) }
        .navigationTitle("Listen")
        .alert("Neue Liste", isPresented: $askName) {
            TextField("Name", text: $newName)
            Button("Anlegen") { Task { await create() } }
            Button("Abbrechen", role: .cancel) {}
        }
        .problemAlert($problem)
        .task { await load() }
        .refreshable { await load() }
    }

    private func load() async {
        error = nil
        do { listen = try await session.client.playlists() }
        catch { self.error = Session.text(error) }
        loading = false
    }

    private func create() async {
        let name = newName.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !name.isEmpty else { return }
        do { _ = try await session.client.createPlaylist(name: name) }
        catch { problem = Session.text(error) }
        await load()
    }
}

/// Eine Liste in der Übersicht: das Bild des ersten Titels, Name, Anzahl
/// und — bei fremden Listen — wem sie gehört.
struct PlaylistRow: View {
    @Environment(Session.self) private var session
    let liste: Liste
    @State private var url: URL?

    var body: some View {
        HStack(spacing: 14) {
            Artwork(url: url, title: "", corner: 6)
                .frame(width: 44, height: 66)
            VStack(alignment: .leading, spacing: 3) {
                Text(liste.name).font(.body).lineLimit(1)
                Text(detailLine).font(.caption).foregroundStyle(Theme.label2).lineLimit(1)
            }
        }
        .padding(.vertical, 2)
        .task(id: liste.vorschau?.first?.id) {
            guard let first = liste.vorschau?.first else { url = nil; return }
            url = await session.client.artworkURL(for: first)
        }
    }

    private var detailLine: String {
        var parts = [liste.anzahlText]
        if !liste.meine {
            parts.append(String(localized: "von \(liste.von.name)", bundle: .lumen, locale: .lumen))
        } else if liste.geteilt && liste.gemeinsam {
            parts.append(String(localized: "im Haushalt, alle schreiben mit", bundle: .lumen, locale: .lumen))
        } else if liste.geteilt {
            parts.append(String(localized: "im Haushalt sichtbar", bundle: .lumen, locale: .lumen))
        }
        return parts.joined(separator: " · ")
    }
}

// MARK: - Eine Liste

struct PlaylistView: View {
    @Environment(Session.self) private var session
    @Environment(\.dismiss) private var dismiss
    let listeId: String
    /// Der Name aus der Übersicht, damit der Titel nicht erst nach dem
    /// Laden erscheint.
    var name: String = ""

    @State private var liste: Liste?
    /// Eine eigene Kopie der Einträge: Verschieben soll sofort sichtbar
    /// sein, nicht erst, wenn der Server geantwortet hat.
    @State private var items: [Item] = []
    @State private var loading = true
    @State private var error: String?
    @State private var problem: String?
    @State private var abspielen: ListenStart?
    @State private var askName = false
    @State private var newName = ""
    @State private var confirmDelete = false

    private var darf: Bool { liste?.darfAendern == true }

    /// Abspielen spielt die ganze Liste am Stück. Filme laufen, wie sie
    /// sind; bei einer Serie sucht der Player die erste offene Folge — das
    /// entscheidet `Abspielfolge` in LumenKit, nicht diese Ansicht.
    private var hatAbspielbares: Bool {
        items.contains { $0.kind == "movie" || $0.kind == "series" || $0.kind == "episode" }
    }

    var body: some View {
        List {
            if let liste {
                Section {
                    header(liste)
                }

                Section {
                    ForEach(items) { item in
                        row(item, geteilt: liste.geteilt)
                    }
                    .onMove(perform: darf ? moveItems : nil)
                } footer: {
                    if items.isEmpty {
                        Text("Noch leer. Auf einer Titelseite legst du mit „Zur Liste“ etwas hierher.")
                    }
                }

                if liste.meine {
                    settings(liste)
                }
            }
        }
        .overlay { Loading(loading: loading, error: error, retry: load) }
        .navigationTitle(liste?.name ?? name)
        #if os(iOS)
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                if darf && items.count > 1 { EditButton() }
            }
        }
        #endif
        .playerZeigen(item: $abspielen) { start in
            PlayerAuftrag(item: start.erstes, liste: start.items, ab: start.ab)
        }
        .alert("Liste umbenennen", isPresented: $askName) {
            TextField("Name", text: $newName)
            Button("Speichern") { Task { await rename() } }
            Button("Abbrechen", role: .cancel) {}
        }
        .confirmationDialog("Liste löschen?", isPresented: $confirmDelete) {
            Button("Löschen", role: .destructive) { Task { await delete() } }
        } message: {
            Text("Die Titel bleiben in der Mediathek. Nur die Liste verschwindet — auch für alle, die sie sehen.")
        }
        .problemAlert($problem)
        .task { await load() }
        .refreshable { await load() }
    }

    // MARK: - Teile

    private func header(_ liste: Liste) -> some View {
        VStack(alignment: .leading, spacing: 12) {
            Text(subline(liste)).font(.subheadline).foregroundStyle(Theme.label2)
            if hatAbspielbares, let erstes = items.first {
                Button {
                    abspielen = ListenStart(items: items, ab: 0, erstes: erstes)
                } label: {
                    Label("Abspielen", systemImage: "play.fill")
                }
                .buttonStyle(PrimaryButtonStyle())
            }
        }
        .padding(.vertical, 6)
    }

    /// Ein Eintrag samt dem, was man mit ihm tun darf: wischen zum
    /// Herausnehmen, und im Kontextmenü auch verschieben — auf dem Mac und
    /// für alle, die nicht ziehen mögen.
    private func row(_ item: Item, geteilt: Bool) -> some View {
        NavigationLink { DetailView(itemId: item.id) } label: {
            PlaylistItemRow(item: item, showWho: geteilt)
        }
        .swipeActions(edge: .trailing) {
            if darf {
                Button("Aus der Liste nehmen", role: .destructive) {
                    Task { await remove(item) }
                }
            }
        }
        .contextMenu {
            if darf {
                Button("Nach oben") { Task { await shift(item, by: -1) } }
                    .disabled(item.id == items.first?.id)
                Button("Nach unten") { Task { await shift(item, by: 1) } }
                    .disabled(item.id == items.last?.id)
                Button("Aus der Liste nehmen", role: .destructive) {
                    Task { await remove(item) }
                }
            }
        }
    }

    private func subline(_ liste: Liste) -> String {
        if !liste.meine { return liste.anzahlText + " · " + String(localized: "von \(liste.von.name)", bundle: .lumen, locale: .lumen) }
        return liste.anzahlText + " · " + (liste.geteilt ? String(localized: "im Haushalt sichtbar", bundle: .lumen, locale: .lumen) : String(localized: "nur für dich", bundle: .lumen, locale: .lumen))
    }

    /// Nur für die Person, der die Liste gehört: teilen, umbenennen,
    /// löschen. Der Server prüft das ebenfalls — hier geht es nur darum,
    /// niemandem Schalter zu zeigen, die nicht wirken.
    private func settings(_ liste: Liste) -> some View {
        Section {
            Toggle("Im Haushalt zeigen", isOn: Binding(
                get: { liste.geteilt },
                // Wer die Liste wieder für sich behält, nimmt den anderen
                // auch das Mitschreiben — sonst stünde es beim nächsten
                // Teilen unbemerkt wieder offen.
                set: { neu in Task { await update(geteilt: neu, gemeinsam: neu ? nil : false) } }))
            Toggle("Alle dürfen mitschreiben", isOn: Binding(
                get: { liste.geteilt && liste.gemeinsam },
                set: { neu in Task { await update(geteilt: nil, gemeinsam: neu) } }))
            Button("Umbenennen") {
                newName = liste.name
                askName = true
            }
            Button("Liste löschen", role: .destructive) { confirmDelete = true }
        } header: {
            Text("Einstellungen")
        } footer: {
            Text("Im Haushalt sieht jedes Profil diese Liste — mit den Titeln, die es ohnehin sehen darf. Wer mitschreiben darf, kann Titel hinzufügen, herausnehmen und umstellen.")
        }
    }

    // MARK: - Laden und Ändern

    private func load() async {
        error = nil
        do {
            let fresh = try await session.client.playlist(listeId)
            liste = fresh
            items = fresh.items ?? []
        } catch { self.error = Session.text(error) }
        loading = false
    }

    private func remove(_ item: Item) async {
        do { try await session.client.removeFromPlaylist(listeId, itemId: item.id) }
        catch { problem = Session.text(error) }
        await load()
    }

    private func moveItems(from source: IndexSet, to destination: Int) {
        items.move(fromOffsets: source, toOffset: destination)
        Task { await saveOrder() }
    }

    private func shift(_ item: Item, by offset: Int) async {
        guard let i = items.firstIndex(where: { $0.id == item.id }) else { return }
        let j = i + offset
        guard items.indices.contains(j) else { return }
        items.swapAt(i, j)
        await saveOrder()
    }

    /// Geht das Speichern schief, gilt wieder, was der Server hat — sonst
    /// zeigte die App eine Reihenfolge, die es nirgends gibt.
    private func saveOrder() async {
        do { try await session.client.orderPlaylist(listeId, itemIds: items.map(\.id)) }
        catch {
            problem = Session.text(error)
            await load()
        }
    }

    private func rename() async {
        let neu = newName.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !neu.isEmpty else { return }
        do { try await session.client.updatePlaylist(listeId, name: neu) }
        catch { problem = Session.text(error) }
        await load()
    }

    private func update(geteilt: Bool?, gemeinsam: Bool?) async {
        do { try await session.client.updatePlaylist(listeId, geteilt: geteilt, gemeinsam: gemeinsam) }
        catch { problem = Session.text(error) }
        await load()
    }

    private func delete() async {
        do {
            try await session.client.deletePlaylist(listeId)
            dismiss()
        } catch {
            problem = Session.text(error)
        }
    }
}

/// Was der Player bekommt, wenn eine Liste läuft. Eine eigene Kennung je
/// Start, damit derselbe Knopf auch zweimal hintereinander wirkt.
/// `erstes` ist nur für die Anzeige, bis der erste Titel geladen ist.
struct ListenStart: Identifiable {
    let id = UUID()
    let items: [Item]
    let ab: Int
    let erstes: Item
}

/// Ein Titel in einer Liste. „von X" steht nur in geteilten Listen — in
/// der eigenen weiss man, wer etwas hineingelegt hat.
struct PlaylistItemRow: View {
    @Environment(Session.self) private var session
    let item: Item
    var showWho = false
    @State private var url: URL?

    var body: some View {
        HStack(spacing: 14) {
            Artwork(url: url, title: item.title, corner: 6)
                .frame(width: 48, height: 72)
                .overlay(alignment: .bottom) {
                    if item.percent > 1 && !item.isSeen { ProgressStripe(percent: item.percent) }
                }
            VStack(alignment: .leading, spacing: 3) {
                Text(item.title).font(.body).lineLimit(2)
                if let jahr = item.jahresText {
                    Text(jahr).font(.caption).foregroundStyle(Theme.label2)
                }
                if showWho, let wer = item.hinzugefuegtVon, !wer.isEmpty {
                    Text("von \(wer)").font(.caption).foregroundStyle(Theme.label3)
                }
            }
            Spacer()
            if item.isSeen {
                Image(systemName: "checkmark").font(.caption.weight(.bold))
                    .foregroundStyle(Theme.label3)
            }
        }
        .padding(.vertical, 2)
        .task(id: item.id) { url = await session.client.artworkURL(for: item) }
    }
}

// MARK: - Zur Liste

/// Die Auswahl auf der Titelseite: in welche Listen dieser Titel gehört.
///
/// Gezeigt werden nur Listen, in die man schreiben darf — die eigenen und
/// geteilte, bei denen alle mitschreiben. Ein Tipp legt hinein oder nimmt
/// heraus; der Haken sagt, was gerade gilt. Die Rechnung dahinter ist
/// `Listenwahl` in LumenKit — dieselbe wie auf dem Fernseher.
struct AddToPlaylistSheet: View {
    @Environment(Session.self) private var session
    @Environment(\.dismiss) private var dismiss
    let itemId: String
    let title: String

    @State private var wahl: Listenwahl?
    @State private var askName = false
    @State private var newName = ""

    var body: some View {
        NavigationStack {
            List {
                if let wahl {
                    Section {
                        ForEach(wahl.listen) { liste in
                            Button {
                                Task { await wahl.toggle(liste) }
                            } label: {
                                HStack {
                                    VStack(alignment: .leading, spacing: 2) {
                                        Text(liste.name).foregroundStyle(Theme.label)
                                        if !liste.meine {
                                            Text("von \(liste.von.name)")
                                                .font(.caption).foregroundStyle(Theme.label2)
                                        }
                                    }
                                    Spacer()
                                    if wahl.busy == liste.id {
                                        ProgressView().controlSize(.small)
                                    } else if liste.hasItem {
                                        Image(systemName: "checkmark")
                                            .font(.body.weight(.semibold))
                                            .foregroundStyle(Theme.accent)
                                    }
                                }
                                .contentShape(Rectangle())
                            }
                            .buttonStyle(.plain)
                            .disabled(wahl.isBusy)
                        }
                        Button {
                            newName = ""
                            askName = true
                        } label: {
                            Label("Neue Liste …", systemImage: "plus")
                        }
                        .disabled(wahl.isBusy)
                    } footer: {
                        if !title.isEmpty {
                            Text("Tippe auf eine Liste, um „\(title)“ hineinzulegen oder herauszunehmen.")
                        }
                    }
                }
            }
            .overlay {
                Loading(loading: wahl?.loading ?? true, error: wahl?.error,
                        retry: { if let wahl { await wahl.load() } })
            }
            .navigationTitle("Zur Liste")
            #if !os(macOS)
            .navigationBarTitleDisplayMode(.inline)
            #endif
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button("Fertig") { dismiss() }
                }
            }
            .alert("Neue Liste", isPresented: $askName) {
                TextField("Name", text: $newName)
                Button("Anlegen") { Task { await wahl?.createAndAdd(name: newName) } }
                Button("Abbrechen", role: .cancel) {}
            }
            .problemAlert(Binding(
                get: { wahl?.problem },
                set: { wahl?.problem = $0 }))
            .task {
                let neu = Listenwahl(client: session.client, itemId: itemId)
                wahl = neu
                await neu.load()
            }
        }
        #if os(macOS)
        .frame(minWidth: 360, minHeight: 380)
        #endif
    }
}

// MARK: - Meldung

extension View {
    /// Eine Meldung, wenn eine Änderung nicht durchging — etwa weil die
    /// Liste inzwischen nicht mehr geteilt (403) oder gelöscht ist (404).
    /// Der Text kommt vom Server und ist für Menschen geschrieben.
    func problemAlert(_ problem: Binding<String?>) -> some View {
        alert(String(localized: "Das hat nicht geklappt", bundle: .lumen, locale: .lumen),
              isPresented: Binding(
                get: { problem.wrappedValue != nil },
                set: { if !$0 { problem.wrappedValue = nil } })) {
            Button("OK", role: .cancel) {}
        } message: {
            Text(problem.wrappedValue ?? "")
        }
    }
}
