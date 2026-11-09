// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

import SwiftUI
import LumenKit

/// Das Gerüst nach der Anmeldung.
///
/// Auf dem iPhone unten eine Leiste mit fünf Reitern — mehr fasst sie
/// nicht, ohne dass iOS den Rest unter „Mehr" versteckt (und dort jeden
/// Reiter in einen zweiten Navigationsstapel steckt). Was seltener der
/// erste Griff ist, steht unter „Ich". Auf iPad und Mac die Seitenleiste
/// links, dort ist Platz für alles. Der Inhalt ist derselbe.
struct LibraryShell: View {
    @Environment(Session.self) private var session
    @State private var tab: Tab = LibraryShell.zuletzt

    /// Der zuletzt offene Reiter. Wechselt die Sprache, baut sich die App
    /// neu auf (siehe `LumenApp`) — ohne das hier stünde man danach wieder
    /// auf der Startseite statt bei den Einstellungen, wo man sie gewählt hat.
    private static var zuletzt: Tab = .home

    /// Der eine Player für alles, was eine Karte direkt abspielt (das
    /// Kontextmenü, `titelMenue`). Titelseiten haben ihren eigenen.
    @State private var abspielstart: Abspielstart?

    enum Tab: Hashable, CaseIterable {
        case home, movies, series, search, watchlist, favorites, collections, playlists, news, wishes
        case downloads, ich, verwaltung

        var title: LocalizedStringKey {
            switch self {
            case .home: "Startseite"
            case .movies: "Filme"
            case .series: "Serien"
            case .search: "Suchen"
            case .watchlist: "Merkliste"
            case .favorites: "Favoriten"
            case .collections: "Filmreihen"
            case .playlists: "Listen"
            case .wishes: "Wünsche"
            case .news: "Neuigkeiten"
            case .downloads: "Downloads"
            case .ich: "Ich"
            case .verwaltung: "Verwaltung"
            }
        }
        var symbol: String {
            switch self {
            case .home: "house"
            case .movies: "film"
            case .series: "tv"
            case .search: "magnifyingglass"
            case .watchlist: "bookmark"
            case .favorites: "heart"
            case .collections: "square.stack"
            case .playlists: "list.bullet.rectangle"
            case .wishes: "plus.circle"
            case .news: "bell"
            case .downloads: "arrow.down.circle"
            case .ich: "person.crop.circle"
            case .verwaltung: "gearshape"
            }
        }

        /// Die fünf Reiter des iPhones.
        static let telefon: [Tab] = [.home, .movies, .series, .search, .ich]
        /// Die Seitenleiste von iPad und Mac, oberer Teil.
        static let seitenleiste: [Tab] = [.home, .movies, .series, .search, .watchlist, .favorites,
                                          .collections, .playlists, .news, .wishes, .downloads]
    }

    var body: some View {
        Group {
            #if os(iOS)
            if UIDevice.current.userInterfaceIdiom == .phone {
                phoneLayout
            } else {
                splitLayout
            }
            #else
            splitLayout
            #endif
        }
        // Die Verwaltungssperre (Issue #6): antwortet der Server auf eine
        // Verwaltungs-Anfrage mit „gesperrt", merkt sich die Sitzung das,
        // und hier erscheint der Dialog — egal, aus welcher Ansicht die
        // Anfrage kam. Nach dem Entsperren läuft sie von selbst weiter.
        //
        // Derselbe Platz für die Bestätigung beim Ändern von Rechten
        // (Gruppen, Grenzen eines Kontos): ein Blatt, nicht zwei — zwei
        // Blätter an derselben Ansicht vertragen sich nicht immer. Das
        // Blatt bleibt offen, bis die Anfrage durch ist oder abgebrochen
        // wird; schliesst es sich von aussen, wird die wartende Anfrage
        // abgebrochen, damit sie nie hängen bleibt.
        .sheet(isPresented: Binding(
            get: { session.adminSperre != nil || session.bestaetigungOffen },
            set: { offen in
                guard !offen else { return }
                if session.adminSperre != nil { session.adminEntsperrenAbbrechen() }
                if session.bestaetigungOffen { session.bestaetigenAbbrechen() }
            })) {
            if session.adminSperre != nil {
                AdminUnlockView().environment(session)
            } else {
                BestaetigenView().environment(session)
            }
        }
        // Abspielen aus dem Kontextmenü einer Karte, von überall her.
        .environment(\.abspielen, { start in abspielstart = start })
        .playerZeigen(item: $abspielstart) { start in
            PlayerAuftrag(item: start.item, vonVorn: start.vonVorn)
        }
        // Offline entstandener Stand geht an den Server, sobald er wieder
        // erreichbar ist — beim Hineinkommen und bei jedem Wechsel des Profils.
        .task(id: session.me?.account.id) { await standNachreichen() }
        .onChange(of: tab) { Self.zuletzt = tab }
        #if os(macOS)
        // ⌘F aus dem Menü: zur Suche.
        .onReceive(NotificationCenter.default.publisher(for: .lumenSuchen)) { _ in tab = .search }
        #endif
    }

    private func standNachreichen() async {
        #if !os(tvOS)
        guard let me = session.me, let server = session.aktiverServer else { return }
        await Downloads.shared.nachreichen(client: session.client, serverId: server.id,
                                           accountId: me.account.id)
        #endif
    }

    // MARK: - iPhone

    #if os(iOS)
    private var phoneLayout: some View {
        TabView(selection: $tab) {
            ForEach(Tab.telefon, id: \.self) { item in
                NavigationStack { screen(for: item) }
                    .tabItem { Label(item.title, systemImage: item.symbol) }
                    // Der Zähler der Neuigkeiten hängt an „Ich", wo sie
                    // jetzt stehen — nur die eigenen.
                    .badge(item == .ich ? ungelesen : 0)
                    .tag(item)
            }
        }
    }
    #endif

    // MARK: - iPad und Mac

    private var splitLayout: some View {
        NavigationSplitView {
            // `List(selection:)` will ein wahlfreies Binding — in einer
            // Liste kann nichts ausgewählt sein. `TabView(selection:)`
            // oben will das Gegenteil, denn ein Reiter ist immer aktiv.
            // Derselbe Zustand bedient beides, nur eben nicht unverändert.
            List(selection: Binding(
                get: { Optional(tab) },
                set: { if let neu = $0 { tab = neu } })) {
                Section {
                    ForEach(Tab.seitenleiste, id: \.self) { item in
                        Label(item.title, systemImage: item.symbol)
                            .badge(item == .news ? ungelesen : 0)
                            .tag(item)
                    }
                }
                Section {
                    // Besitzer und Mitverwalter — nicht nur der Besitzer.
                    // Nur am Mac: das iPad ist zum Schauen da. Ein echter
                    // Eintrag der Auswahl, damit er markiert bleibt und
                    // nicht über die Seitenleiste geschoben wird.
                    if session.verwaltungHier {
                        Label(Tab.verwaltung.title, systemImage: Tab.verwaltung.symbol).tag(Tab.verwaltung)
                    }
                    Label("Profil", systemImage: Tab.ich.symbol).tag(Tab.ich)
                }
            }
            .navigationTitle("Lumen")
            #if os(macOS)
            .listStyle(.sidebar)
            #endif
        } detail: {
            // Je Eintrag ein eigener Stapel: wer in „Filme" drei Titel tief
            // ist und zu „Serien" wechselt, landet oben in „Serien" — nicht
            // im Stapel des vorigen Eintrags.
            NavigationStack { screen(for: tab) }
                .id(tab)
        }
        // Kein Admin mehr (Rechte entzogen, Profil gewechselt): nicht in der
        // Verwaltung stehen bleiben.
        .onChange(of: session.verwaltungHier) { _, darf in
            if !darf, tab == .verwaltung { tab = .home }
        }
    }

    /// Wie viele Neuigkeiten noch ungelesen sind. 0 zeigt keinen Punkt.
    private var ungelesen: Int { session.me?.ungelesen ?? 0 }

    @ViewBuilder
    private func screen(for tab: Tab) -> some View {
        switch tab {
        case .home:      HomeView()
        case .movies:    BrowseView(kind: "movie", title: String(localized: "Filme", bundle: .lumen, locale: .lumen))
        case .series:    BrowseView(kind: "series", title: String(localized: "Serien", bundle: .lumen, locale: .lumen))
        case .search:    SuchenView()
        case .watchlist: WatchlistView()
        case .favorites: FavoritenView()
        case .collections: CollectionsView()
        case .playlists: PlaylistsView()
        case .wishes:    WishesView()
        case .news:      NeuigkeitenView()
        case .downloads: DownloadsView()
        case .ich:
            #if os(iOS)
            if UIDevice.current.userInterfaceIdiom == .phone { IchView() } else { SettingsView() }
            #else
            SettingsView()
            #endif
        case .verwaltung:
            if session.verwaltungHier { AdminView() } else { HomeView() }
        }
    }
}

// MARK: - Startseite

struct HomeView: View {
    @Environment(Session.self) private var session
    @State private var rows: [HomeRow] = []
    @State private var reihen: [Reihe] = []
    @State private var loading = true
    @State private var error: String?

    var body: some View {
        // Gesucht wird unter „Suchen" — ein Feld, nicht eins je Seite.
        ScrollView {
            LazyVStack(alignment: .leading, spacing: 30) {
                if let first = rows.first?.items.first {
                    HeroBanner(item: first)
                }
                // Wie im Browser: die erste Reihe des Servers, dann die
                // Filmreihen, dann der Rest.
                ForEach(rows.prefix(1)) { row in
                    Rail(title: row.title, items: row.items, wide: row.isWide)
                }
                if !reihen.isEmpty {
                    ReihenRail(reihen: Array(reihen.prefix(12)))
                }
                ForEach(rows.dropFirst()) { row in
                    Rail(title: row.title, items: row.items, wide: row.isWide)
                }
            }
            .padding(.bottom, 40)
        }
        .overlay {
            if !loading, error == nil, rows.isEmpty {
                ContentUnavailableView("Noch nichts da", systemImage: "film.stack",
                    description: Text(leerText))
            } else if rows.isEmpty {
                Loading(loading: loading, error: error, retry: load)
            }
        }
        .navigationTitle("Startseite")
        #if !os(macOS)
        .navigationBarTitleDisplayMode(.inline)
        #endif
        .task { await load() }
        .refreshable { await load() }
    }

    /// Was ein Admin tun kann, hängt vom Gerät ab: am Mac gibt es die
    /// Verwaltung, auf iPhone und iPad nicht.
    private var leerText: LocalizedStringKey {
        if session.verwaltungHier {
            return "Lege unter „Verwaltung“ eine Bibliothek an und zeige Lumen, wo deine Filme liegen."
        }
        if session.isAdmin {
            return "Lege in der Mac-App oder im Browser eine Bibliothek an und zeige Lumen, wo deine Filme liegen."
        }
        return "Sobald der Besitzer Filme hinzufügt, erscheinen sie hier."
    }

    private func load() async {
        error = nil
        // Alle Reihen so, wie der Server sie schickt — keine fest
        // verdrahteten Schlüssel. Neue Reihen erscheinen ohne neue App.
        do { rows = try await session.client.home().rows.filter { !$0.items.isEmpty } }
        catch {
            if APIClient.istAbbruch(error) { return }
            self.error = Session.text(error)
        }
        reihen = (try? await session.client.collections()) ?? []
        loading = false
        // Den Zähler der Neuigkeiten nebenbei auffrischen, wie im Browser —
        // sonst bliebe der Punkt bis zur nächsten Anmeldung alt.
        await session.refreshMe()
    }
}

/// Das grosse Bild oben: der erste Titel der ersten Reihe. Auf dem Mac
/// läuft nach einer Sekunde Zeiger darüber stumm ein Schnipsel aus dem
/// Film, wie im Browser; auf iPhone und iPad nicht — dort wäre es Akku
/// und Datenvolumen für nichts.
struct HeroBanner: View {
    @Environment(Session.self) private var session
    let item: Item
    @State private var url: URL?
    @State private var vorschau: URL?
    @State private var zeigerDrauf = false

    var body: some View {
        NavigationLink { DetailView(itemId: item.id) } label: {
            Artwork(url: url, title: item.title, corner: 0)
                .aspectRatio(16.0 / 9.0, contentMode: .fill)
                .frame(maxWidth: .infinity)
                .frame(maxHeight: 420)
                .overlay { if let vorschau { StummeVorschau(url: vorschau) } }
                .clipped()
                .scrim()
                .overlay(alignment: .bottomLeading) {
                    VStack(alignment: .leading, spacing: 8) {
                        Text(item.title)
                            .font(.largeTitle.weight(.bold))
                            .lineLimit(2)
                            .minimumScaleFactor(0.7)
                        Text(heroZeile)
                            .font(.subheadline).foregroundStyle(.white.opacity(0.72))
                        if let overview = item.overview, !overview.isEmpty {
                            Text(overview)
                                .font(.callout).foregroundStyle(.white.opacity(0.72))
                                .lineLimit(2).frame(maxWidth: 520, alignment: .leading)
                        }
                        Label("Ansehen", systemImage: "play.fill")
                            .font(.subheadline.weight(.semibold))
                            .foregroundStyle(.black)
                            .padding(.horizontal, 18).padding(.vertical, 9)
                            .background(Capsule().fill(.white))
                            .padding(.top, 6)
                    }
                    // Über dem Bild ist es immer dunkel — auch wenn die
                    // App hell eingestellt ist. Darum fest weiss.
                    .foregroundStyle(.white)
                    .padding(24)
                }
        }
        .buttonStyle(.plain)
        .titelMenue(item)
        #if os(macOS)
        .onHover { zeigerDrauf = $0 }
        .task(id: zeigerDrauf) {
            guard zeigerDrauf else { vorschau = nil; return }
            try? await Task.sleep(for: .seconds(1))
            if Task.isCancelled { return }
            vorschau = await session.client.vorschauURL(itemId: item.id)
        }
        #endif
        .task {
            url = await session.client.artworkURL(for: item, wide: true)
        }
    }

    /// „2019 · 1 Std. 58 Min. · Drama · Mystery · ★ 7,8" — wie im Browser.
    private var heroZeile: String {
        var teile = [item.subtitleLine]
        if let r = item.imdbRating, r > 0 {
            teile.append("★ " + r.formatted(.number.precision(.fractionLength(1)).locale(.lumen)))
        }
        return teile.filter { !$0.isEmpty }.joined(separator: " · ")
    }
}

/// Eine waagerechte Reihe. `hier` hebt einen Titel hervor — auf der
/// Titelseite eines Films den Film selbst in seiner Reihe.
struct Rail: View {
    let title: String
    let items: [Item]
    var wide = false
    var hier: String? = nil

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text(title).font(.title3.weight(.semibold)).padding(.horizontal, 20)
            RailInhalt(items: items, wide: wide, hier: hier)
        }
    }
}

/// Die Karten einer Reihe, ohne Überschrift — für Reihen, deren
/// Überschrift selbst ein Link ist.
struct RailInhalt: View {
    let items: [Item]
    var wide = false
    var hier: String? = nil

    var body: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            LazyHStack(alignment: .top, spacing: Theme.railSpacing) {
                ForEach(items) { item in
                    NavigationLink { DetailView(itemId: item.id) } label: {
                        PosterCard(item: item, wide: wide, hier: item.id == hier)
                    }
                    .buttonStyle(.plain)
                    .titelMenue(item)
                }
            }
            .padding(.horizontal, 20)
        }
    }
}

/// Die Filmreihen als Reihe auf der Startseite. Die Überschrift führt zu
/// allen Reihen.
struct ReihenRail: View {
    let reihen: [Reihe]

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            NavigationLink { CollectionsView() } label: {
                HStack(spacing: 6) {
                    Text("Filmreihen").font(.title3.weight(.semibold))
                    Image(systemName: "chevron.right").font(.subheadline.weight(.bold))
                        .foregroundStyle(Theme.label2)
                }
            }
            .buttonStyle(.plain)
            .padding(.horizontal, 20)
            ScrollView(.horizontal, showsIndicators: false) {
                LazyHStack(alignment: .top, spacing: Theme.railSpacing) {
                    ForEach(reihen) { reihe in
                        NavigationLink {
                            CollectionView(reiheId: reihe.id, name: reihe.name)
                        } label: {
                            CollectionCard(reihe: reihe)
                        }
                        .buttonStyle(.plain)
                    }
                }
                .padding(.horizontal, 20)
            }
        }
    }
}

/// Ein Poster mit Haken, Fortschritt und Abzeichen. Auf dem Mac läuft
/// nach einer knappen Sekunde Zeiger darüber eine stumme Vorschau im Bild.
struct PosterCard: View {
    @Environment(Session.self) private var session
    let item: Item
    var wide = false
    /// Der Titel, auf dessen Seite man gerade ist — hervorgehoben.
    var hier = false
    @State private var url: URL?
    @State private var vorschau: URL?
    @State private var zeigerDrauf = false

    private var width: CGFloat { wide ? Theme.wideWidth : Theme.posterWidth }
    private var height: CGFloat { wide ? Theme.wideWidth * 9 / 16 : Theme.posterWidth / Theme.posterRatio }

    var body: some View {
        VStack(alignment: .leading, spacing: 9) {
            Artwork(url: url, title: item.title)
                .frame(width: width, height: height)
                .overlay {
                    if let vorschau {
                        StummeVorschau(url: vorschau)
                            .clipShape(RoundedRectangle(cornerRadius: Theme.corner, style: .continuous))
                    }
                }
                .overlay(alignment: .top) { KartenAbzeichen(item: item) }
                .overlay(alignment: .bottomTrailing) { if item.isSeen { SeenBadge() } }
                .overlay(alignment: .bottom) {
                    // Bei Serien zählt der Balken die gesehenen Folgen.
                    if balken > 1 && !item.isSeen {
                        ProgressStripe(percent: balken)
                    }
                }
                .overlay {
                    if hier {
                        RoundedRectangle(cornerRadius: Theme.corner, style: .continuous)
                            .strokeBorder(Theme.label, lineWidth: 2.5)
                    }
                }
                #if os(macOS)
                .onHover { zeigerDrauf = $0 }
                .task(id: zeigerDrauf) {
                    guard zeigerDrauf else { vorschau = nil; return }
                    try? await Task.sleep(for: .milliseconds(900))
                    if Task.isCancelled { return }
                    vorschau = await session.client.vorschauURL(itemId: item.id)
                }
                #endif
            // Zwei Zeilen für den Titel, immer reserviert: „Der Herr der
            // Ringe: Die Gefährten" passt sonst nicht, und die Karten einer
            // Reihe stehen trotzdem auf gleicher Höhe.
            Text(item.title).font(.subheadline).lineLimit(2, reservesSpace: true)
            Text(subtitle).font(.caption).foregroundStyle(Theme.label2).lineLimit(1)
        }
        .frame(width: width, alignment: .leading)
        .accessibilityLabel(zugaenglich)
        .task {
            url = await session.client.artworkURL(for: item, wide: wide)
        }
    }

    private var balken: Double {
        item.kind == "series" ? max(item.percent, item.folgenAnteil) : item.percent
    }

    private var zugaenglich: String {
        if item.isSeen { return String(localized: "\(item.title), gesehen", bundle: .lumen, locale: .lumen) }
        if let stand = item.folgenStand { return String(localized: "\(item.title), \(stand) gesehen", bundle: .lumen, locale: .lumen) }
        return item.title
    }

    /// „4 von 10 Folgen" bei Serien mit Stand; sonst Jahr und Wertung.
    private var subtitle: String {
        if let stand = item.folgenStand { return stand }
        if item.kind == "episode", let s = item.seasonNo, let e = item.episodeNo {
            return "S\(s) F\(e)"
        }
        var parts: [String] = []
        if let jahr = item.jahresText { parts.append(jahr) }
        if let rating = item.imdbRating, rating > 0 {
            parts.append("★ " + rating.formatted(.number.precision(.fractionLength(1)).locale(.lumen)))
        }
        return parts.joined(separator: " · ")
    }
}

// MARK: - Mediathek und Merkliste

/// Ein Raster aus Postern — Filme, Serien, Merkliste, Suche, Person.
struct PosterRaster: View {
    let items: [Item]
    /// Wird für jede Karte gerufen, die erscheint — zum Nachladen.
    var erscheint: ((Item) -> Void)? = nil

    private var columns: [GridItem] {
        [GridItem(.adaptive(minimum: Theme.posterWidth, maximum: 220), spacing: 18)]
    }

    var body: some View {
        LazyVGrid(columns: columns, spacing: 24) {
            ForEach(items) { item in
                NavigationLink { DetailView(itemId: item.id) } label: { PosterCard(item: item) }
                    .buttonStyle(.plain)
                    .titelMenue(item)
                    .onAppear { erscheint?(item) }
            }
        }
    }
}

/// „Filme" und „Serien": oben die Genres des Servers als Knöpfe, darunter
/// das Raster, das beim Herunterscrollen nachlädt — wie im Browser.
///
/// Sortiert wird über das Menü oben rechts (Issue #11), wie in den Apps
/// von Apple; bei A–Z steht über dem Raster die Leiste der Buchstaben.
struct BrowseView: View {
    @Environment(Session.self) private var session
    let kind: String
    let title: String
    /// Ein Genre, das schon gewählt sein soll — aus der Suche.
    var startGenre: String? = nil

    @State private var raster: Raster?

    var body: some View {
        ScrollView {
            if let raster {
                VStack(alignment: .leading, spacing: 14) {
                    if raster.geladen {
                        Text(raster.zeile)
                            .font(.subheadline).foregroundStyle(Theme.label2)
                            .padding(.horizontal, 20)
                    }
                    if !raster.genres.isEmpty {
                        ScrollView(.horizontal, showsIndicators: false) {
                            HStack(spacing: 8) {
                                Chip(label: String(localized: "Alle", bundle: .lumen, locale: .lumen), selected: raster.genre == nil) {
                                    Task { await raster.laden(genre: nil) }
                                }
                                ForEach(raster.genres, id: \.self) { g in
                                    Chip(label: g, selected: raster.genre == g) {
                                        Task { await raster.laden(genre: g) }
                                    }
                                }
                            }
                            .padding(.horizontal, 20)
                        }
                    }
                    if raster.zeigtBuchstaben {
                        BuchstabenLeiste(raster: raster)
                    }
                    PosterRaster(items: raster.items) { item in
                        Task { await raster.erscheint(item) }
                    }
                    .padding(.horizontal, 20)
                    .padding(.top, 6)
                    if raster.weiter {
                        ProgressView().frame(maxWidth: .infinity).padding(.vertical, 24)
                    }
                }
                .padding(.bottom, 40)
            }
        }
        .overlay {
            if let raster, raster.items.isEmpty {
                Loading(loading: !raster.geladen, error: raster.error, empty: raster.geladen,
                        retry: { await raster.neu() })
            }
        }
        .navigationTitle(title)
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                if let raster { sortierMenue(raster) }
            }
        }
        .task {
            guard raster == nil else { return }
            let neu = Raster(client: session.client, kind: kind, genre: startGenre)
            raster = neu
            await neu.laden(genre: startGenre)
        }
        .refreshable { await raster?.neu() }
    }

    /// Die vier Sortierungen als Auswahl mit Haken.
    private func sortierMenue(_ raster: Raster) -> some View {
        Menu {
            Picker("Sortieren", selection: Binding(
                get: { raster.sortierung },
                set: { neu in Task { await raster.sortieren(neu) } })) {
                ForEach(Sortierung.allCases) { art in
                    Label(art.name(kind: kind), systemImage: art.symbol).tag(art)
                }
            }
            .pickerStyle(.inline)
        } label: {
            Label("Sortieren", systemImage: "arrow.up.arrow.down")
        }
        .accessibilityLabel("Sortieren: \(raster.sortierung.name(kind: kind))")
    }
}

/// „Alle # A B C … Z" — springt zu den Titeln mit diesem Anfang. Ein
/// Buchstabe ohne Titel bleibt grau und lässt sich nicht tippen. Waagerecht
/// zum Wischen statt eines Index am Rand: auf dem iPhone läge der sonst
/// genau dort, wo der Daumen das Raster scrollt.
struct BuchstabenLeiste: View {
    let raster: Raster

    var body: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 2) {
                knopf(nil, text: String(localized: "Alle", bundle: .lumen, locale: .lumen))
                ForEach(Buchstaben.alle, id: \.self) { b in
                    knopf(b, text: b)
                }
            }
            .padding(.horizontal, 20)
        }
    }

    private func knopf(_ buchstabe: String?, text: String) -> some View {
        let gewaehlt = raster.buchstabe == buchstabe
        let anzahl = buchstabe.map { raster.anzahl($0) } ?? raster.gesamt
        let leer = buchstabe != nil && anzahl == 0
        return Button {
            Task { await raster.springe(zu: buchstabe) }
        } label: {
            Text(text)
                .font(.footnote.weight(gewaehlt ? .bold : .medium))
                .foregroundStyle(gewaehlt ? Theme.background : (leer ? Theme.label3 : Theme.label))
                .frame(minWidth: 26, minHeight: 28)
                .padding(.horizontal, buchstabe == nil ? 8 : 0)
                .background(Capsule().fill(gewaehlt ? Theme.label : .clear))
                .contentShape(Capsule())
        }
        .buttonStyle(.plain)
        .disabled(leer)
        .accessibilityLabel(buchstabe.map { Buchstaben.vorlesen($0, anzahl: anzahl) } ?? String(localized: "Alle Buchstaben", bundle: .lumen, locale: .lumen))
        .accessibilityAddTraits(gewaehlt ? .isSelected : [])
    }
}

struct WatchlistView: View {
    @Environment(Session.self) private var session
    /// „merkliste" oder „gesehen" — wie die Reiter im Browser.
    @State private var liste = "merkliste"
    @State private var items: [Item] = []
    @State private var loading = true
    /// Ein Fehler ist nicht „noch nichts gemerkt" — er bekommt seine
    /// eigene Anzeige mit „Nochmal versuchen".
    @State private var error: String?

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 14) {
                Picker("Liste", selection: $liste) {
                    Text("Merkliste").tag("merkliste")
                    Text("Gesehen").tag("gesehen")
                }
                .pickerStyle(.segmented)
                .labelsHidden()
                .frame(maxWidth: 320)
                if !items.isEmpty {
                    Text(liste == "gesehen"
                         ? "Fertig geschaut, zuletzt Beendetes zuerst · nur du siehst das"
                         : "Nur du siehst diese Liste")
                        .font(.subheadline).foregroundStyle(Theme.label2)
                }
                PosterRaster(items: items)
            }
            .padding(20)
        }
        .overlay {
            if let error, items.isEmpty {
                Loading(loading: false, error: error, retry: load)
            } else if !loading && items.isEmpty {
                if liste == "gesehen" {
                    ContentUnavailableView("Noch nichts zu Ende geschaut",
                        systemImage: "checkmark.circle",
                        description: Text("Filme landen hier, wenn du sie fertig geschaut hast, Serien nach der letzten Folge."))
                } else {
                    ContentUnavailableView("Noch nichts gemerkt",
                        systemImage: "bookmark",
                        description: Text("Auf einer Titelseite legst du etwas hierher. Nur du siehst diese Liste."))
                }
            } else if loading {
                ProgressView().controlSize(.large)
            }
        }
        .navigationTitle(liste == "gesehen" ? "Gesehen" : "Merkliste")
        .task(id: liste) { await load() }
        .refreshable { await load() }
    }

    /// Der Server liefert nur die eigene Liste — auch die Titel, die weit
    /// hinten in der Mediathek stehen.
    private func load() async {
        let gewollt = liste
        loading = true
        do {
            let seite = gewollt == "gesehen"
                ? try await session.client.gesehen()
                : try await session.client.merkliste()
            guard gewollt == liste else { return }
            items = seite.items
            error = nil
        } catch {
            guard gewollt == liste, !APIClient.istAbbruch(error) else { return }
            items = []
            self.error = Session.text(error)
        }
        loading = false
    }
}

/// Die Favoriten (Issue #10): was einem richtig gut gefallen hat, getrennt
/// nach Filmen und Serien — wie im Browser (app.js: viewFavorites). Nur
/// dieses Profil sieht sie.
struct FavoritenView: View {
    @Environment(Session.self) private var session
    @State private var art = "movie"
    @State private var items: [Item] = []
    @State private var loading = true
    @State private var geladen = false
    @State private var error: String?

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                Picker("Art", selection: $art) {
                    Text("Filme").tag("movie")
                    Text("Serien").tag("series")
                }
                .pickerStyle(.segmented)
                .labelsHidden()
                .frame(maxWidth: 320)
                Text("Was dir besonders gefallen hat · nur du siehst das")
                    .font(.subheadline).foregroundStyle(Theme.label2)
                PosterRaster(items: items)
            }
            .padding(20)
        }
        .overlay {
            if let error {
                Loading(loading: false, error: error, retry: load)
            } else if loading && items.isEmpty {
                ProgressView().controlSize(.large)
            } else if items.isEmpty {
                ContentUnavailableView(art == "movie" ? "Noch keine Filme mit Herz" : "Noch keine Serien mit Herz",
                    systemImage: "heart",
                    description: Text("Auf einer Titelseite setzt du eins. Nur du siehst deine Favoriten."))
            }
        }
        .navigationTitle("Favoriten")
        .task(id: art) { await load() }
        // Zurück von einer Titelseite: dort kann ein Herz dazugekommen
        // oder weggefallen sein.
        .onAppear { if geladen { Task { await load() } } }
        .refreshable { await load() }
    }

    private func load() async {
        let gewollt = art
        error = nil
        do {
            let seite = try await session.client.favoriten(kind: gewollt)
            // Inzwischen auf die andere Art gewechselt: diese Antwort nicht.
            guard gewollt == art else { return }
            items = seite.items
        } catch {
            guard gewollt == art else { return }
            self.error = Session.text(error)
        }
        loading = false
        geladen = true
    }
}

// MARK: - Kleinteile

struct Chip: View {
    let label: String
    let selected: Bool
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            Text(label)
                .font(.subheadline.weight(selected ? .semibold : .regular))
                .foregroundStyle(selected ? Theme.label : Theme.label2)
                .padding(.horizontal, 14).padding(.vertical, 7)
                .background(Capsule().fill(selected ? Theme.fill : .clear))
        }
        .buttonStyle(.plain)
    }
}

/// Lädt, Fehler oder nichts gefunden — an einer Stelle statt in jeder Ansicht.
struct Loading: View {
    let loading: Bool
    var error: String?
    var empty = false
    var retry: (() async -> Void)?

    var body: some View {
        if let error {
            ContentUnavailableView {
                Label("Das hat nicht geklappt", systemImage: "exclamationmark.triangle")
            } description: {
                Text(error)
            } actions: {
                if let retry {
                    Button("Nochmal versuchen") { Task { await retry() } }
                        .buttonStyle(QuietButtonStyle())
                }
            }
        } else if loading {
            ProgressView().controlSize(.large)
        } else if empty {
            ContentUnavailableView("Hier ist noch nichts", systemImage: "film.stack")
        }
    }
}
