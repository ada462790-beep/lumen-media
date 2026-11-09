// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

import SwiftUI
import LumenKit

/// Die Titelseite: Bild, Beschreibung, abspielen, merken, bewerten.
/// Bei Serien darunter Staffeln und Folgen, bei Filmen einer Reihe die
/// übrigen Filme daraus — dieselben Teile wie im Browser (app.js:
/// viewDetail), angeordnet, wie es auf dem Gerät üblich ist.
struct DetailView: View {
    @Environment(Session.self) private var session
    let itemId: String

    @State private var detail: ItemDetail?
    @State private var staffeln: [Item] = []
    @State private var staffel: String?
    @State private var episodes: [Item] = []
    @State private var loading = true
    @State private var error: String?
    @State private var backdrop: URL?
    @State private var start: Abspielstart?
    @State private var showLists = false
    /// „In welcher Qualität?" — offen, und ob danach von vorn.
    @State private var qualitaetWahl = false
    @State private var qualitaetVonVorn = false
    /// Ob dieses Gerät die Datei direkt abspielt — für die Technik-Zeile.
    @State private var plan: PlaybackPlan?
    /// Was beim Merken, Gesehen-Markieren oder Bewerten schiefging — als
    /// Meldung, statt es zu verschlucken.
    @State private var aktionsFehler: String?
    /// Ob der Server „gesehen" für ganze Staffeln kennt. Antwortet er mit
    /// 404, verschwindet der Staffelknopf still. (Ein Server ganz ohne
    /// diese Runde liest `?staffel=` nicht und setzt den Haken nur an der
    /// Serie selbst — das lässt sich von hier aus nicht erkennen.)
    @State private var staffelGesehenGeht = true
    #if os(macOS)
    /// Verwaltung am Titel: „Anderen Titel zuweisen" offen, und was zuletzt
    /// geschah („wird neu geholt …", „zugewiesen").
    @State private var zuweisen = false
    @State private var verwaltungsMeldung: String?
    @State private var holtNeu = false
    #endif

    private var item: Item? { detail?.item }

    var body: some View {
        ScrollView {
            if let item, let detail {
                VStack(alignment: .leading, spacing: 0) {
                    header(item, detail)
                    details(item, detail)
                    // Die Reihe steht ganz unten und in voller Breite: sie
                    // führt weg von diesem Titel, nicht tiefer hinein. Die
                    // Überschrift öffnet die ganze Reihe; der Film selbst
                    // ist darin hervorgehoben.
                    if let reihe = detail.collection, !reihe.items.isEmpty {
                        VStack(alignment: .leading, spacing: 12) {
                            NavigationLink {
                                CollectionView(reiheId: reihe.id, name: reihe.name)
                            } label: {
                                HStack(alignment: .firstTextBaseline, spacing: 8) {
                                    Text(reihe.name).font(.title3.weight(.semibold))
                                    Image(systemName: "chevron.right").font(.subheadline.weight(.bold))
                                        .foregroundStyle(Theme.label2)
                                    Text(reihe.anzahlText).font(.subheadline).foregroundStyle(Theme.label2)
                                }
                            }
                            .buttonStyle(.plain)
                            .padding(.horizontal, 20)
                            RailInhalt(items: reihe.items, hier: item.id)
                        }
                        .padding(.bottom, 32)
                    }
                    // Ähnliche Titel: dieselbe Reihe, geteilte Besetzung,
                    // gleiche Genres — der Server wählt, nur aus dem, was
                    // dieses Profil sehen darf.
                    if let aehnlich = detail.similar, !aehnlich.isEmpty {
                        Rail(title: String(localized: "Ähnliche Titel", bundle: .lumen, locale: .lumen), items: aehnlich)
                            .padding(.bottom, 32)
                    }
                }
            }
        }
        .overlay { Loading(loading: loading, error: error, retry: load) }
        .ignoresSafeArea(edges: .top)
        .navigationTitle(item?.title ?? "")
        #if !os(macOS)
        .navigationBarTitleDisplayMode(.inline)
        #endif
        .task { await load() }
        .playerZeigen(item: $start) { s in
            PlayerAuftrag(item: s.item, vonVorn: s.vonVorn, datei: s.datei)
        }
        // Zurück aus dem Player: Stand, Haken und „Weiter bei" auffrischen.
        #if os(macOS)
        // Am Mac läuft der Player in einem eigenen Fenster; es meldet sich,
        // wenn es zugeht.
        .onReceive(NotificationCenter.default.publisher(for: .lumenPlayerZu)) { _ in
            Task { await load() }
        }
        #else
        .onChange(of: start == nil) { _, zu in
            if zu { Task { await load() } }
        }
        #endif
        .alert("Das hat nicht geklappt", isPresented: Binding(
            get: { aktionsFehler != nil }, set: { if !$0 { aktionsFehler = nil } })) {
            Button("OK", role: .cancel) {}
        } message: {
            Text(aktionsFehler ?? "")
        }
        .sheet(isPresented: $showLists) {
            AddToPlaylistSheet(itemId: itemId, title: item?.title ?? "")
                .environment(session)
        }
    }

    // MARK: - Kopf

    /// Der Kopf: das breite Bild als Grund, darauf Titel, Angaben, die
    /// Technik-Zeile und die Knöpfe. Die Höhe richtet sich nach dem Inhalt
    /// — auf dem iPhone brechen die Knöpfe um, dann wächst das Bild mit.
    private func header(_ item: Item, _ detail: ItemDetail) -> some View {
        VStack(alignment: .leading, spacing: 10) {
            Text(item.title).font(.largeTitle.weight(.bold)).lineLimit(2)
            if let original = item.originalTitle, original != item.title, !original.isEmpty {
                Text(original).font(.subheadline).foregroundStyle(.white.opacity(0.72))
            }
            Text(metaLine(item)).font(.subheadline).foregroundStyle(.white.opacity(0.72))
            TechnikZeile(abzeichen: Technik.zeile(item: item, file: detail.playable, plan: plan))
                .padding(.top, 2)
            actions(item, detail)
        }
        // Über dem Bild immer hell auf dunkel, auch in „Hell".
        .foregroundStyle(.white)
        .padding(22)
        .padding(.top, 150)
        .frame(maxWidth: .infinity, minHeight: 400, alignment: .bottomLeading)
        .background {
            Artwork(url: backdrop, title: "", corner: 0)
                .scrim(0.9)
        }
        .clipped()
    }

    private func metaLine(_ item: Item) -> String {
        var parts: [String] = []
        if let jahr = item.jahresText { parts.append(jahr) }
        if let age = item.ageYears { parts.append(String(localized: "ab \(age)", bundle: .lumen, locale: .lumen)) }
        if let ms = item.runtimeMs, ms > 0 { parts.append(Item.duration(ms: ms)) }
        if let genres = item.genres, !genres.isEmpty { parts.append(genres.joined(separator: " · ")) }
        if let rating = item.imdbRating, rating > 0 {
            var text = "IMDb " + rating.formatted(.number.precision(.fractionLength(1)).locale(.lumen))
            if let votes = item.imdbVotes, votes > 0 {
                text += " (" + votes.formatted(.number.locale(.lumen)) + ")"
            }
            parts.append(text)
        }
        if let t = item.tomatenText { parts.append(t) }
        return parts.joined(separator: " · ")
    }

    /// Die Knöpfe im Kopf. Auf dem iPhone ist die Zeile schmal — dann
    /// bricht sie um, statt Knöpfe abzuschneiden.
    @ViewBuilder
    private func actions(_ item: Item, _ detail: ItemDetail) -> some View {
        // Mehrere Fassungen desselben Films (MKV in 1080p, AVI in 400p):
        // „Ansehen" fragt dann zuerst nach der Qualität. Teile
        // eines Films (a/b/c) sind keine Fassungen; die laufen nacheinander.
        let fassungen = Fassungen.liste(detail.files)
        FlussLayout(abstand: 10) {
            if detail.playable != nil {
                Button {
                    if fassungen.count > 1 {
                        qualitaetVonVorn = false
                        qualitaetWahl = true
                    } else {
                        start = Abspielstart(item: item)
                    }
                } label: {
                    Label(fassungen.count > 1
                            ? Titelseite.abspielen(item) + " · " + String(localized: "\(fassungen.count) Qualitäten", bundle: .lumen, locale: .lumen)
                            : Titelseite.abspielen(item),
                          systemImage: "play.fill")
                }
                .buttonStyle(KopfHauptknopf())
            }
            // Eine Serie hat selbst keine Datei: ihre erste offene Folge.
            if item.kind == "series", !staffeln.isEmpty {
                Button {
                    start = Abspielstart(item: item)
                } label: {
                    Label(Titelseite.serieAbspielen(item), systemImage: "play.fill")
                }
                .buttonStyle(KopfHauptknopf())
            }
            if detail.playable != nil, Titelseite.kannVonVorn(item) {
                Button("Von vorn") {
                    if fassungen.count > 1 {
                        qualitaetVonVorn = true
                        qualitaetWahl = true
                    } else {
                        start = Abspielstart(item: item, vonVorn: true)
                    }
                }
                .buttonStyle(KopfKnopf())
            }

            Button {
                Task { await toggleList(item) }
            } label: {
                Label(item.isOnList ? "Aus der Merkliste" : "Merken",
                      systemImage: item.isOnList ? "bookmark.fill" : "bookmark")
            }
            .buttonStyle(KopfKnopf())

            // Das Herz gleich neben „Merken": gemerkt ist, was
            // man noch sehen will, ein Favorit, was einem gefallen hat.
            if item.kind == "movie" || item.kind == "series" {
                Button {
                    Task { await toggleFavorit(item) }
                } label: {
                    // Mit Wort wie im Browser: ein Herz allein lässt offen,
                    // ob ein Tipp es setzt oder ob es schon gesetzt ist.
                    if item.isFavorite {
                        Label {
                            Text("Favorit")
                        } icon: {
                            Image(systemName: "heart.fill")
                        }
                        .foregroundStyle(Theme.herz)
                    } else {
                        Label("Zu Favoriten", systemImage: "heart")
                    }
                }
                .buttonStyle(KopfKnopf())
                .accessibilityLabel(item.isFavorite ? "Aus den Favoriten" : "Zu den Favoriten")
                .accessibilityAddTraits(item.isFavorite ? .isSelected : [])
            }

            // Listen nehmen nur Filme und Serien auf, keine einzelnen Folgen.
            if item.kind == "movie" || item.kind == "series" {
                Button {
                    showLists = true
                } label: {
                    Label("Zur Liste", systemImage: "text.badge.plus")
                }
                .buttonStyle(KopfKnopf())
            }

            Button(gesehenText(item)) {
                Task { await toggleSeen(item) }
            }
            .buttonStyle(KopfKnopf())

            #if !os(tvOS)
            // Mitnehmen: nur wenn der Server es für dieses Profil erlaubt
            // und den Weg kennt — sonst gibt es den Knopf nicht.
            if item.kind == "movie" || item.kind == "episode" {
                DownloadKnopf(item: item)
            }
            #endif
        }
        .padding(.top, 6)
        // Jede Fassung als Zeile: „1080p · MKV · 7,9 GB". Die erste ist der
        // Standard — der Server ordnet: bevorzugte, dann höchste Auflösung.
        .confirmationDialog("In welcher Qualität?", isPresented: $qualitaetWahl,
                            titleVisibility: .visible) {
            ForEach(Array(fassungen.enumerated()), id: \.element.id) { nr, f in
                Button(Fassungen.name(f) + (nr == 0 ? " · " + String(localized: "Standard", bundle: .lumen, locale: .lumen) : "")) {
                    start = Abspielstart(item: item, vonVorn: qualitaetVonVorn, datei: f.id)
                }
            }
            Button("Abbrechen", role: .cancel) {}
        }
    }

    // MARK: - Rumpf

    @ViewBuilder
    private func details(_ item: Item, _ detail: ItemDetail) -> some View {
        VStack(alignment: .leading, spacing: 26) {
            #if os(macOS)
            if session.verwaltungHier, item.kind == "movie" || item.kind == "series" {
                verwaltung(item)
            }
            #endif
            if let overview = item.overview, !overview.isEmpty {
                Text(overview).font(.body).lineSpacing(3).frame(maxWidth: 680, alignment: .leading)
            }

            if let cast = detail.cast, !cast.isEmpty {
                BesetzungsReihe(itemId: item.id, leute: cast)
            }

            if !staffeln.isEmpty {
                staffelTeil
            }

            RatingBar(item: item) { stars in await rate(item, stars: stars) }

            if let shared = detail.sharedRatings, !shared.isEmpty {
                VStack(alignment: .leading, spacing: 8) {
                    Text("Im Haushalt").font(.headline)
                    ForEach(shared, id: \.name) { entry in
                        HStack(spacing: 10) {
                            // Nur Name und Bild-Art — ein eigenes Bild
                            // bleibt hier beim Buchstaben.
                            ProfilAvatar(name: entry.name, avatar: entry.avatar, size: 26)
                            Text(entry.name)
                            Spacer()
                            Text(String(repeating: "★", count: entry.stars))
                                .foregroundStyle(Theme.star)
                        }
                    }
                }
            }

            if !detail.files.isEmpty {
                VStack(alignment: .leading, spacing: 8) {
                    Text("Dateien").font(.headline)
                    ForEach(detail.files) { file in
                        HStack {
                            Text((file.path as NSString).lastPathComponent)
                                .font(.system(.caption, design: .monospaced))
                                .foregroundStyle(Theme.label2).lineLimit(1)
                            Spacer()
                            Text(file.isMissing ? String(localized: "fehlt", bundle: .lumen, locale: .lumen) : file.techLine)
                                .font(.caption)
                                .foregroundStyle(file.isMissing ? Theme.warn : Theme.label3)
                        }
                    }
                }
            }
        }
        .padding(22)
    }

    /// Staffeln als Reiter, darunter die Folgen der gewählten — wie im
    /// Browser. Die erste Staffel ist gleich offen; Specials stehen hinten.
    private var staffelTeil: some View {
        VStack(alignment: .leading, spacing: 12) {
            if staffeln.count > 1 {
                ScrollView(.horizontal, showsIndicators: false) {
                    HStack(spacing: 8) {
                        ForEach(staffeln) { st in
                            Chip(label: Folgenwahl.name(st), selected: staffel == st.id) {
                                Task { await zeigeStaffel(st.id) }
                            }
                        }
                    }
                }
            } else if let einzige = staffeln.first {
                Text(Folgenwahl.name(einzige)).font(.headline)
            }
            if staffelGesehenGeht, let gewaehlt = staffeln.first(where: { $0.id == staffel }),
               gewaehlt.seasonNo != nil, !episodes.isEmpty {
                let alleGesehen = episodes.allSatisfy(\.isSeen)
                Button {
                    Task { await staffelGesehen(gewaehlt, gesehen: !alleGesehen) }
                } label: {
                    Label(alleGesehen ? "Staffel nicht mehr als gesehen" : "Staffel als gesehen markieren",
                          systemImage: alleGesehen ? "checkmark.circle.fill" : "checkmark.circle")
                }
                .buttonStyle(QuietButtonStyle())
            }
            if episodes.isEmpty {
                Text("In dieser Staffel liegt noch keine Folge.")
                    .font(.callout).foregroundStyle(Theme.label2)
            } else {
                VStack(alignment: .leading, spacing: 0) {
                    ForEach(episodes) { episode in
                        EpisodeRow(episode: episode,
                                   play: { start = Abspielstart(item: episode) },
                                   gesehen: { Task { await folgeGesehen(episode) } })
                        if episode.id != episodes.last?.id {
                            Divider().overlay(Theme.hairline)
                        }
                    }
                }
            }
        }
    }

    #if os(macOS)
    // MARK: - Verwaltung

    /// Nur für Besitzer und Mitverwalter, nur am Mac — wie im Browser unter
    /// den Knöpfen: Metadaten neu holen, oder Lumen sagen, welcher Film es
    /// wirklich ist.
    private func verwaltung(_ item: Item) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 10) {
                Text("Verwaltung:").font(.caption).foregroundStyle(Theme.label2)
                Button(holtNeu ? "Wird neu geholt …" : "Metadaten neu laden") {
                    Task { await metadatenNeu(item) }
                }
                .disabled(holtNeu)
                Button("Anderen Titel zuweisen") { zuweisen.toggle() }
                if item.matchState == "manual" {
                    Text("von Hand zugewiesen").font(.caption).foregroundStyle(Theme.label2)
                } else if item.matchState == "unmatched" {
                    Text("nicht erkannt — Lumen war sich nicht sicher")
                        .font(.caption).foregroundStyle(Theme.warn)
                }
            }
            .controlSize(.small)
            if let verwaltungsMeldung {
                Text(verwaltungsMeldung).font(.caption).foregroundStyle(Theme.label2)
            }
            if zuweisen {
                TitelZuweisenPanel(itemId: item.id, suche: item.title,
                                   schliessen: { zuweisen = false },
                                   fertig: {
                                       zuweisen = false
                                       verwaltungsMeldung = "Zugewiesen."
                                       await load()
                                   })
                    .padding(.top, 6)
            }
        }
    }

    private func metadatenNeu(_ item: Item) async {
        holtNeu = true
        defer { holtNeu = false }
        do {
            try await session.verwaltet { try await session.client.adminItemReload(item.id) }
            verwaltungsMeldung = String(localized: "Neu geholt.", bundle: .lumen, locale: .lumen)
            await load()
        } catch {
            verwaltungsMeldung = Session.text(error)
        }
    }
    #endif

    // MARK: - Laden und Ändern

    private func load() async {
        error = nil
        do {
            let fresh = try await session.client.item(itemId)
            detail = fresh
            backdrop = await session.client.artworkURL(for: fresh.item, wide: true)
            if let file = fresh.playable {
                plan = (try? await session.client.plan(fileId: file.id))?.plan
            }
            let geordnet = Titelseite.staffeln(fresh.seasons ?? [])
            staffeln = geordnet
            // Die gewählte Staffel bleibt beim Neuladen stehen.
            let ziel = geordnet.first { $0.id == staffel }?.id ?? geordnet.first?.id
            if let ziel { await zeigeStaffel(ziel) }
        } catch { self.error = Session.text(error) }
        loading = false
    }

    private func zeigeStaffel(_ id: String) async {
        staffel = id
        let folgen = AmStueck.geordnet((try? await session.client.children(of: id)) ?? [])
        if staffel == id { episodes = folgen }
    }

    private func toggleList(_ item: Item) async {
        do {
            if item.isOnList { try await session.client.removeFromWatchlist(item.id) }
            else { try await session.client.addToWatchlist(item.id) }
        } catch {
            aktionsFehler = Session.text(error)
        }
        await load()
    }

    private func toggleFavorit(_ item: Item) async {
        do {
            if item.isFavorite { try await session.client.removeFavorite(item.id) }
            else { try await session.client.addFavorite(item.id) }
        } catch {
            aktionsFehler = Session.text(error)
        }
        await load()
    }

    /// Bei einer Serie heisst „gesehen": alle Folgen.
    private func gesehenText(_ item: Item) -> LocalizedStringKey {
        if item.kind == "series" {
            return item.isSeen ? "Serie nicht mehr als gesehen" : "Ganze Serie als gesehen markieren"
        }
        return item.isSeen ? "Nicht mehr als gesehen" : "Als gesehen markieren"
    }

    private func toggleSeen(_ item: Item) async {
        do {
            if item.isSeen { try await session.client.clearFinished(item.id) }
            else { try await session.client.markFinished(item.id) }
        } catch {
            aktionsFehler = Session.text(error)
        }
        await load()
    }

    /// Eine ganze Staffel als gesehen markieren — oder den Haken an allen
    /// ihren Folgen wieder wegnehmen.
    private func staffelGesehen(_ staffel: Item, gesehen: Bool) async {
        guard let serie = item, let nr = staffel.seasonNo else { return }
        do {
            if gesehen { try await session.client.markFinished(serie.id, staffel: nr) }
            else { try await session.client.clearFinished(serie.id, staffel: nr) }
        } catch {
            if LumenError.fehltAufServer(error) {
                staffelGesehenGeht = false
            } else {
                aktionsFehler = Session.text(error)
            }
        }
        await load()
    }

    private func folgeGesehen(_ folge: Item) async {
        do {
            if folge.isSeen { try await session.client.clearFinished(folge.id) }
            else { try await session.client.markFinished(folge.id) }
        } catch {
            aktionsFehler = Session.text(error)
        }
        await load()
    }

    private func rate(_ item: Item, stars: Int) async {
        let shares = session.me?.prefs["shareRatings"] == "1"
        do {
            try await session.client.rate(item.id, stars: stars, shared: shares)
        } catch {
            aktionsFehler = Session.text(error)
        }
        await load()
    }
}

/// Der Hauptknopf über dem Bild: weiss, dunkle Schrift — unabhängig
/// davon, ob die App hell oder dunkel eingestellt ist.
struct KopfHauptknopf: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.callout.weight(.semibold))
            .foregroundStyle(.black)
            .padding(.horizontal, 20)
            .padding(.vertical, 11)
            .background(Capsule().fill(.white))
            .opacity(configuration.isPressed ? 0.8 : 1)
    }
}

/// Die übrigen Knöpfe über dem Bild: Glas, helle Schrift.
struct KopfKnopf: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.subheadline.weight(.medium))
            .foregroundStyle(.white)
            .padding(.horizontal, 16)
            .padding(.vertical, 10)
            .background(Capsule().fill(.ultraThinMaterial))
            .environment(\.colorScheme, .dark)
            .opacity(configuration.isPressed ? 0.7 : 1)
    }
}

// MARK: - Besetzung

/// Die Besetzung als Reihe runder Bilder. Ein Tipp öffnet „alles mit …" —
/// dieselbe Seite wie hinter den Personen in der Suche.
struct BesetzungsReihe: View {
    let itemId: String
    let leute: [Mitwirkender]

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text("Besetzung").font(.headline)
            ScrollView(.horizontal, showsIndicators: false) {
                HStack(alignment: .top, spacing: 14) {
                    ForEach(leute) { person in
                        NavigationLink { PersonView(name: person.name) } label: {
                            VStack(spacing: 6) {
                                MitwirkenderBild(itemId: itemId, person: person, size: 72)
                                Text(person.name)
                                    .font(.caption.weight(.medium))
                                    .lineLimit(1)
                                if let rolle = person.rolle, !rolle.isEmpty {
                                    Text(rolle).font(.caption2).foregroundStyle(Theme.label2).lineLimit(1)
                                }
                            }
                            .frame(width: 88)
                        }
                        .buttonStyle(.plain)
                        .accessibilityLabel("Alles mit \(person.name)")
                    }
                }
            }
        }
    }
}

/// Das runde Bild einer Person aus der Besetzung, sonst ihr Anfangsbuchstabe.
struct MitwirkenderBild: View {
    @Environment(Session.self) private var session
    let itemId: String
    let person: Mitwirkender
    var size: CGFloat = 72
    @State private var url: URL?

    var body: some View {
        ZStack {
            Circle().fill(Theme.fill)
            Text(person.initial)
                .font(.system(size: size * 0.4, weight: .medium))
                .foregroundStyle(Theme.label2)
            if let url {
                MedienBild(url: url)
            }
        }
        .frame(width: size, height: size)
        .clipShape(Circle())
        .task(id: person.bild) {
            guard let art = person.bildArt else { url = nil; return }
            url = await session.client.artworkURL(itemId: itemId, kind: art)
        }
    }
}

// MARK: - Folge

struct EpisodeRow: View {
    @Environment(Session.self) private var session
    let episode: Item
    let play: () -> Void
    /// Den Haken an dieser Folge setzen oder wegnehmen.
    var gesehen: (() -> Void)? = nil
    @State private var thumb: URL?

    var body: some View {
        HStack(alignment: .top, spacing: 14) {
            Artwork(url: thumb, title: episode.title, corner: 8)
                .frame(width: 128, height: 72)
                .overlay(alignment: .bottom) {
                    if episode.percent > 1 && !episode.isSeen { ProgressStripe(percent: episode.percent) }
                }
                .overlay {
                    // Ein Tipp aufs Bild spielt die Folge, wie im Browser.
                    Button(action: play) {
                        Image(systemName: "play.fill")
                            .font(.title3.weight(.semibold))
                            .foregroundStyle(.white)
                            .shadow(color: .black.opacity(0.6), radius: 4)
                            .frame(maxWidth: .infinity, maxHeight: .infinity)
                            .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    .accessibilityLabel("\(episode.title) abspielen")
                }
            VStack(alignment: .leading, spacing: 3) {
                HStack(spacing: 6) {
                    Text("\(episode.episodeNo ?? 0). \(episode.title)")
                        .font(.subheadline.weight(.medium))
                    if episode.isSeen {
                        Image(systemName: "checkmark").font(.caption2.weight(.bold))
                            .foregroundStyle(Theme.label3)
                    }
                }
                if let ms = episode.runtimeMs, ms > 0 {
                    Text(Item.duration(ms: ms)).font(.caption).foregroundStyle(Theme.label3)
                }
                if let overview = episode.overview, !overview.isEmpty {
                    Text(overview).font(.caption).foregroundStyle(Theme.label2).lineLimit(2)
                }
            }
            Spacer()
            Button(Titelseite.folge(episode), action: play).buttonStyle(QuietButtonStyle())
            if let gesehen {
                Button(action: gesehen) {
                    Image(systemName: episode.isSeen ? "checkmark.circle.fill" : "checkmark.circle")
                        .font(.title3)
                        .foregroundStyle(episode.isSeen ? Theme.label : Theme.label3)
                }
                .buttonStyle(.plain)
                .help(episode.isSeen ? "Nicht mehr als gesehen" : "Als gesehen markieren")
                .accessibilityLabel(episode.isSeen ? "Folge nicht mehr als gesehen" : "Folge als gesehen markieren")
            }
        }
        .padding(.vertical, 8)
        .task {
            thumb = await session.client.artworkURL(for: episode, wide: true)
        }
    }
}

// MARK: - Bewertung

struct RatingBar: View {
    let item: Item
    let rate: (Int) async -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Deine Bewertung").font(.headline)
            HStack(spacing: 6) {
                ForEach(1...5, id: \.self) { n in
                    Button {
                        Task { await rate(n) }
                    } label: {
                        Image(systemName: (item.myRating ?? 0) >= n ? "star.fill" : "star")
                            .font(.title3)
                            .foregroundStyle((item.myRating ?? 0) >= n ? Theme.star : Theme.label3)
                    }
                    .buttonStyle(.plain)
                    // Für VoiceOver: was der Knopf setzt, und welcher gilt.
                    .accessibilityLabel(Text("\(n) von 5 Sternen"))
                    .accessibilityAddTraits((item.myRating ?? 0) == n ? .isSelected : [])
                }
                if (item.myRating ?? 0) > 0 {
                    Button("Löschen") { Task { await rate(0) } }
                        .font(.caption).foregroundStyle(Theme.label2).buttonStyle(.plain)
                        .padding(.leading, 8)
                }
            }
            Text("Nur für dich sichtbar.").font(.caption).foregroundStyle(Theme.label3)
        }
    }
}
