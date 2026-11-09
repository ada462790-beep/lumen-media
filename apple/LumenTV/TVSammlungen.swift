// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

import SwiftUI
import LumenKit

// Filmreihen und Listen auf dem Fernseher.
//
// Listen lassen sich hier anlegen, umbenennen, löschen und füllen
// — mit der Fernbedienung, über die grosse Tastatur des
// Systems. Das Umstellen der Reihenfolge bleibt dem iPhone, dem Mac und
// dem Browser: Ziehen gibt es auf dem Fernseher nicht, und eine Reihe
// „nach oben / nach unten"-Knöpfe stünde dem Fokus nur im Weg.
//
// Was jemand schaut, steht auch hier nirgends. Eine geteilte Liste zeigt
// ihre Titel und wem sie gehört, mehr nicht.

// MARK: - Reihen auf der Startseite

struct TVListenRail: View {
    @Environment(Session.self) private var session
    let listen: [Liste]

    var body: some View {
        VStack(alignment: .leading, spacing: 18) {
            Text("Listen").font(.title2.weight(.semibold))
            ScrollView(.horizontal, showsIndicators: false) {
                LazyHStack(spacing: 40) {
                    ForEach(listen) { liste in
                        NavigationLink {
                            TVPlaylistView(listeId: liste.id, name: liste.name)
                        } label: {
                            TVStapelLabel(titel: liste.name, zeile: zeile(liste),
                                          bild: liste.vorschau?.first)
                        }
                        .buttonStyle(.card)
                    }
                    // Ganz hinten, nach den eigenen und geteilten Listen:
                    // eine neue. Vorn stünde sie jedem im Weg, der nur
                    // schauen will.
                    NavigationLink {
                        TVNameView(titel: String(localized: "Neue Liste", bundle: .lumen, locale: .lumen), vorgabe: "", knopf: String(localized: "Anlegen", bundle: .lumen, locale: .lumen)) { name in
                            _ = try await session.client.createPlaylist(name: name)
                        }
                    } label: {
                        TVNeuLabel(titel: String(localized: "Neue Liste", bundle: .lumen, locale: .lumen))
                    }
                    .buttonStyle(.card)
                }
                .padding(.vertical, 24)
            }
        }
    }

    /// Bei fremden Listen, wem sie gehört — sonst wüsste man nicht, warum
    /// sie hier steht.
    private func zeile(_ liste: Liste) -> String {
        liste.meine ? liste.anzahlText : String(localized: "\(liste.anzahlText) · von \(liste.von.name)", bundle: .lumen, locale: .lumen)
    }
}

struct TVReihenRail: View {
    let reihen: [Reihe]

    var body: some View {
        VStack(alignment: .leading, spacing: 18) {
            Text("Filmreihen").font(.title2.weight(.semibold))
            ScrollView(.horizontal, showsIndicators: false) {
                LazyHStack(spacing: 40) {
                    ForEach(reihen) { reihe in
                        NavigationLink {
                            TVCollectionView(reiheId: reihe.id, name: reihe.name)
                        } label: {
                            TVStapelLabel(titel: reihe.name, zeile: reihe.anzahlText,
                                          bild: reihe.items.first, reihe: reihe)
                        }
                        .buttonStyle(.card)
                    }
                }
                .padding(.vertical, 24)
            }
        }
    }
}

/// Eine Karte für eine Reihe oder Liste: das Poster des ersten Titels,
/// darunter Name und Anzahl. So gross wie die Karten der Filme, damit die
/// Zeilen beim Durchblättern nicht springen. Bei einer Reihe zuerst ihr
/// eigenes Bild, sonst das des ersten Films.
struct TVStapelLabel: View {
    @Environment(Session.self) private var session
    let titel: String
    let zeile: String
    let bild: Item?
    var reihe: Reihe? = nil
    @State private var url: URL?

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            Artwork(url: url, title: titel, corner: 10)
                .frame(width: 260, height: 390)
            Text(titel).font(.body).lineLimit(1)
                .frame(width: 260, alignment: .leading)
            Text(zeile).font(.caption).foregroundStyle(Theme.label2).lineLimit(1)
                .frame(width: 260, alignment: .leading)
        }
        .task(id: reihe?.id ?? bild?.id) {
            if let reihe {
                url = await session.client.reiheBildURL(reihe)
                return
            }
            guard let bild else { url = nil; return }
            url = await session.client.artworkURL(for: bild)
        }
    }
}

/// Die Karte „Neue Liste": so gross wie die anderen, damit die Reihe
/// nicht springt, aber ohne Bild — ein Plus statt eines Posters.
struct TVNeuLabel: View {
    let titel: String

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            RoundedRectangle(cornerRadius: 10, style: .continuous)
                .fill(Theme.surface)
                .frame(width: 260, height: 390)
                .overlay(
                    Image(systemName: "plus")
                        .font(.system(size: 64, weight: .light))
                        .foregroundStyle(Theme.label2))
            Text(titel).font(.body).lineLimit(1)
                .frame(width: 260, alignment: .leading)
            Text(" ").font(.caption)
                .frame(width: 260, alignment: .leading)
        }
    }
}

// MARK: - Einen Namen eingeben

/// Eine Seite für genau einen Namen: eine neue Liste anlegen oder eine
/// bestehende umbenennen. Das Feld öffnet die grosse Tastatur des
/// Fernsehers; „Anlegen" bzw. „Speichern" ruft `aktion` und geht zurück.
/// Scheitert sie, bleibt die Seite und zeigt, warum.
struct TVNameView: View {
    @Environment(\.dismiss) private var dismiss
    let titel: String
    let vorgabe: String
    let knopf: String
    let aktion: (String) async throws -> Void

    @State private var name = ""
    @State private var fehler: String?
    @State private var laeuft = false

    private var sauber: String { name.trimmingCharacters(in: .whitespacesAndNewlines) }

    var body: some View {
        VStack(spacing: 30) {
            Text(titel).font(.system(size: 54, weight: .bold))
            TextField("Name", text: $name)
                .textFieldStyle(.plain)
                .frame(width: 620)
                .onSubmit { Task { await ausfuehren() } }
            if let fehler {
                Text(fehler).font(.callout).foregroundStyle(Theme.bad)
            }
            Button(knopf) { Task { await ausfuehren() } }
                .disabled(sauber.isEmpty || laeuft)
        }
        .padding(80)
        .onAppear { if name.isEmpty { name = vorgabe } }
    }

    private func ausfuehren() async {
        guard !sauber.isEmpty, !laeuft else { return }
        laeuft = true
        fehler = nil
        do {
            try await aktion(sauber)
            dismiss()
        } catch {
            fehler = Session.text(error)
        }
        laeuft = false
    }
}

// MARK: - Eine Filmreihe

/// Die Filme einer Reihe, in ihrer Reihenfolge. Ein Klick öffnet, wie auf
/// der Startseite auch, die Titelseite; ein langer Druck spielt sofort.
struct TVCollectionView: View {
    @Environment(Session.self) private var session
    let reiheId: String
    /// Der Name aus der Übersicht, damit der Titel nicht erst nach dem
    /// Laden erscheint.
    var name: String = ""

    @State private var reihe: Reihe?
    @State private var focused: Item?
    @State private var playing: Abspielstart?
    @State private var error: String?

    var body: some View {
        ZStack(alignment: .topLeading) {
            TVBackdrop(item: focused ?? reihe?.items.first)

            ScrollView {
                VStack(alignment: .leading, spacing: 40) {
                    VStack(alignment: .leading, spacing: 12) {
                        Text(reihe?.name ?? name)
                            .font(.system(size: 62, weight: .bold)).lineLimit(2)
                        if let reihe {
                            Text(reihe.unterzeile).font(.title3).foregroundStyle(Theme.label2)
                        }
                        if let overview = reihe?.overview, !overview.isEmpty {
                            Text(overview).font(.body).foregroundStyle(Theme.label2)
                                .lineLimit(4).frame(maxWidth: 900, alignment: .leading)
                        }
                        if let focused {
                            Text(zeile(focused)).font(.title3).foregroundStyle(Theme.label2)
                                .lineLimit(1)
                        }
                        if let error {
                            TVFehler(text: error) { await load() }
                        }
                    }
                    .padding(.top, 60)

                    // Wie im Browser: die Reihe von vorn, oder weiter mit
                    // dem ersten Film, der noch nicht gesehen ist.
                    if let reihe, let film = reihe.naechsterFilm {
                        Button {
                            playing = Abspielstart(item: film)
                        } label: {
                            Label(reihe.abspielText, systemImage: "play.fill")
                                .font(.title3.weight(.semibold))
                                .padding(.horizontal, 20)
                        }
                    }

                    if let reihe, !reihe.items.isEmpty {
                        TVRail(title: String(localized: "Filme", bundle: .lumen, locale: .lumen), items: reihe.items, focused: $focused, menu: { film in
                            tvKartenMenue(film, session: session) { playing = $0 }
                        })
                    }
                }
                .padding(.horizontal, 60)
                .padding(.bottom, 60)
            }
        }
        .task { await load() }
        .fullScreenCover(item: $playing) { s in
            TVPlayerView(item: s.item, vonVorn: s.vonVorn).environment(session)
        }
        // Zurück aus dem Player: Haken und „Weiter mit" auffrischen.
        .onChange(of: playing == nil) { _, zu in
            if zu { Task { await load() } }
        }
    }

    /// „Die Bucht 2 · 2022 · 1 Std. 58 Min." — welcher Film gerade den
    /// Fokus hat, denn die Überschrift ist die der Reihe.
    private func zeile(_ item: Item) -> String {
        [item.title, item.subtitleLine].filter { !$0.isEmpty }.joined(separator: " · ")
    }

    private func load() async {
        error = nil
        do { reihe = try await session.client.collection(reiheId) }
        catch { self.error = Session.text(error) }
    }
}

// MARK: - Eine Liste

/// Was der Player bekommt, wenn eine Liste läuft. Eine eigene Kennung je
/// Start, damit derselbe Knopf auch zweimal hintereinander wirkt.
struct TVListenStart: Identifiable {
    let id = UUID()
    let items: [Item]
    let ab: Int
    let erstes: Item
}

/// Eine Liste: Name, wem sie gehört, ein grosses „Abspielen" und die
/// Titel. „Abspielen" spielt alles am Stück von vorn; ein Titel spielt
/// die Liste ab diesem Titel weiter. Ein langer Druck auf einen Titel
/// öffnet seine Seite oder nimmt ihn heraus.
///
/// Wem die Liste gehört, sieht darunter ihre Einstellungen: teilen,
/// umbenennen, löschen. Der Server prüft das ebenfalls — hier geht es nur
/// darum, niemandem Schalter zu zeigen, die nicht wirken.
struct TVPlaylistView: View {
    @Environment(Session.self) private var session
    @Environment(\.dismiss) private var dismiss
    let listeId: String
    var name: String = ""

    @State private var liste: Liste?
    @State private var items: [Item] = []
    @State private var focused: Item?
    @State private var start: TVListenStart?
    @State private var zeige: Item?
    @State private var loading = true
    @State private var error: String?
    @State private var problem: String?
    @State private var confirmDelete = false

    private var darf: Bool { liste?.darfAendern == true }

    private var hatAbspielbares: Bool {
        items.contains { $0.kind == "movie" || $0.kind == "series" || $0.kind == "episode" }
    }

    var body: some View {
        ZStack(alignment: .topLeading) {
            TVBackdrop(item: focused ?? items.first)

            ScrollView {
                VStack(alignment: .leading, spacing: 40) {
                    VStack(alignment: .leading, spacing: 12) {
                        Text(liste?.name ?? name)
                            .font(.system(size: 62, weight: .bold)).lineLimit(2)
                        if let liste {
                            Text(subline(liste)).font(.title3).foregroundStyle(Theme.label2)
                        }
                        if let error {
                            TVFehler(text: error) { await load() }
                        } else if let problem {
                            Text(problem).foregroundStyle(Theme.bad)
                        } else if !loading && items.isEmpty {
                            Text("Diese Liste ist noch leer. Auf der Seite eines Titels legst du mit „Zur Liste“ etwas hierher.")
                                .font(.body).foregroundStyle(Theme.label2)
                                .frame(maxWidth: 900, alignment: .leading)
                        }
                    }
                    .padding(.top, 60)

                    if hatAbspielbares, let erstes = items.first {
                        Button {
                            start = TVListenStart(items: items, ab: 0, erstes: erstes)
                        } label: {
                            Label("Abspielen", systemImage: "play.fill")
                                .font(.title3.weight(.semibold))
                                .padding(.horizontal, 20)
                        }
                    }

                    if !items.isEmpty {
                        TVRail(title: String(localized: "Titel", bundle: .lumen, locale: .lumen), items: items, focused: $focused,
                               play: { item in starte(ab: item) },
                               menu: { item in
                            Button { zeige = item } label: {
                                Label("Titelseite", systemImage: "info.circle")
                            }
                            if darf {
                                Button(role: .destructive) {
                                    Task { await entfernen(item) }
                                } label: {
                                    Label("Aus der Liste nehmen", systemImage: "minus.circle")
                                }
                            }
                        })
                    }

                    if let liste, liste.meine {
                        einstellungen(liste)
                    }
                }
                .padding(.horizontal, 60)
                .padding(.bottom, 60)
            }
        }
        .task { await load() }
        // Zurück vom Umbenennen oder von einer Titelseite: der Name oder
        // die Titel können sich geändert haben.
        .onAppear { if !loading { Task { await load() } } }
        .fullScreenCover(item: $start) { s in
            TVPlayerView(item: s.erstes, liste: s.items, ab: s.ab).environment(session)
        }
        .navigationDestination(item: $zeige) { item in
            TVDetailView(itemId: item.id, vorschau: item)
        }
        .confirmationDialog("Liste löschen?", isPresented: $confirmDelete) {
            Button("Löschen", role: .destructive) { Task { await loeschen() } }
            Button("Abbrechen", role: .cancel) {}
        } message: {
            Text("Die Titel bleiben in der Mediathek. Nur die Liste verschwindet — auch für alle, die sie sehen.")
        }
    }

    // MARK: - Teile

    /// Nur für die Person, der die Liste gehört.
    private func einstellungen(_ liste: Liste) -> some View {
        VStack(alignment: .leading, spacing: 18) {
            Text("Einstellungen").font(.title2.weight(.semibold))
            Toggle("Im Haushalt zeigen", isOn: Binding(
                get: { liste.geteilt },
                // Wer die Liste wieder für sich behält, nimmt den anderen
                // auch das Mitschreiben — wie auf dem iPhone.
                set: { neu in Task { await aendern(geteilt: neu, gemeinsam: neu ? nil : false) } }))
            Toggle("Alle dürfen mitschreiben", isOn: Binding(
                get: { liste.geteilt && liste.gemeinsam },
                set: { neu in Task { await aendern(geteilt: nil, gemeinsam: neu) } }))
            HStack(spacing: 24) {
                NavigationLink {
                    TVNameView(titel: String(localized: "Liste umbenennen", bundle: .lumen, locale: .lumen), vorgabe: liste.name, knopf: String(localized: "Speichern", bundle: .lumen, locale: .lumen)) { neu in
                        try await session.client.updatePlaylist(listeId, name: neu)
                    }
                } label: {
                    Label("Umbenennen", systemImage: "pencil")
                }
                Button(role: .destructive) {
                    confirmDelete = true
                } label: {
                    Label("Liste löschen", systemImage: "trash")
                }
            }
            Text("Im Haushalt sieht jedes Profil diese Liste — mit den Titeln, die es ohnehin sehen darf. Wer mitschreiben darf, kann Titel hinzufügen und herausnehmen.")
                .font(.caption).foregroundStyle(Theme.label2)
                .frame(maxWidth: 900, alignment: .leading)
        }
        .frame(maxWidth: 900, alignment: .leading)
    }

    private func subline(_ liste: Liste) -> String {
        if !liste.meine { return liste.anzahlText + " · " + String(localized: "von \(liste.von.name)", bundle: .lumen, locale: .lumen) }
        return liste.anzahlText + " · " + (liste.geteilt ? String(localized: "im Haushalt sichtbar", bundle: .lumen, locale: .lumen) : String(localized: "nur für dich", bundle: .lumen, locale: .lumen))
    }

    private func starte(ab item: Item) {
        guard let i = items.firstIndex(where: { $0.id == item.id }) else { return }
        start = TVListenStart(items: items, ab: i, erstes: item)
    }

    // MARK: - Laden und Ändern

    private func load() async {
        error = nil
        do {
            let fresh = try await session.client.playlist(listeId)
            liste = fresh
            items = fresh.items ?? []
        } catch {
            self.error = Session.text(error)
        }
        loading = false
    }

    private func entfernen(_ item: Item) async {
        problem = nil
        do { try await session.client.removeFromPlaylist(listeId, itemId: item.id) }
        catch { problem = Session.text(error) }
        await load()
    }

    private func aendern(geteilt: Bool?, gemeinsam: Bool?) async {
        problem = nil
        do { try await session.client.updatePlaylist(listeId, geteilt: geteilt, gemeinsam: gemeinsam) }
        catch { problem = Session.text(error) }
        await load()
    }

    private func loeschen() async {
        do {
            try await session.client.deletePlaylist(listeId)
            dismiss()
        } catch {
            problem = Session.text(error)
        }
    }
}
