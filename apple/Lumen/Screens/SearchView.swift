// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

import SwiftUI
import LumenKit

// Die Suche.
//
// Der Server deutet die Eingabe: „Komödie 90er" wird zu Genre und Jahren,
// „Filme mit Tom Hanks" zu einer Person. Die Seite zeigt oben die
// Personen, die zur Eingabe passen, darunter Genres als Chips, darunter
// die Titel — und sagt, wie gesucht wurde („12 Treffer · mit Tom Hanks").
// Die Web-App macht es genauso (app.js: viewSearch, viewPerson).

/// Der Platzhalter im Suchfeld — überall derselbe.
let suchPlatzhalter: LocalizedStringKey = "Titel, Schauspieler, Genre, Jahr …"

/// Die Ergebnisse zu einer Eingabe. Steht in den Ansichten, die ein
/// Suchfeld haben, anstelle ihres gewöhnlichen Inhalts.
struct SuchErgebnisse: View {
    @Environment(Session.self) private var session
    let query: String

    @State private var ergebnis: Suchergebnis?
    @State private var loading = false
    @State private var error: String?
    @State private var geladenFuer = ""
    /// Was gewünscht wurde — der Knopf wird danach zur Bestätigung.
    @State private var gewuenscht: String?
    @State private var wunschFehler: String?

    var body: some View {
        VStack(alignment: .leading, spacing: 22) {
            if let ergebnis, geladenFuer == query.trimmingCharacters(in: .whitespacesAndNewlines) {
                if let personen = ergebnis.personen, !personen.isEmpty {
                    PersonenReihe(personen: personen)
                }
                if let genres = ergebnis.genres, !genres.isEmpty {
                    ScrollView(.horizontal, showsIndicators: false) {
                        HStack(spacing: 8) {
                            ForEach(genres, id: \.self) { genre in
                                NavigationLink {
                                    GenreView(kind: "movie", genre: genre)
                                } label: {
                                    Text(genre)
                                        .font(.subheadline.weight(.medium))
                                        .foregroundStyle(Theme.label)
                                        .padding(.horizontal, 14).padding(.vertical, 7)
                                        .background(Capsule().fill(Theme.fill))
                                }
                                .buttonStyle(.plain)
                            }
                        }
                        .padding(.horizontal, 20)
                    }
                }
                if !ergebnis.items.isEmpty {
                    Text(ergebnis.trefferzeile)
                        .font(.subheadline).foregroundStyle(Theme.label2)
                        .padding(.horizontal, 20)
                    PosterRaster(items: ergebnis.items)
                        .padding(.horizontal, 20)
                } else if !ergebnis.leer {
                    Text(ergebnis.deutung?.gedeutet == true
                         ? "Kein Titel passt genau — gesucht wurde \(ergebnis.deutung?.beschreibung ?? "")."
                         : "Kein Titel passt genau — aber die Personen und Genres oben.")
                        .font(.callout).foregroundStyle(Theme.label2)
                        .padding(.horizontal, 20)
                } else {
                    nichtsGefunden
                }
            } else if let error {
                ContentUnavailableView {
                    Label("Das hat nicht geklappt", systemImage: "exclamationmark.triangle")
                } description: {
                    Text(error)
                }
            } else {
                ProgressView().controlSize(.large)
                    .frame(maxWidth: .infinity)
                    .padding(.top, 60)
            }
        }
        .padding(.bottom, 40)
        // Kurz warten, bis nicht mehr getippt wird — sonst geht jede Taste
        // als Anfrage an den Server.
        .task(id: query) {
            try? await Task.sleep(for: .milliseconds(300))
            if Task.isCancelled { return }
            await load()
        }
    }

    /// Nichts gefunden: der Wunsch ist nur einen Tipp entfernt — wie im
    /// Browser geht er gleich an den Besitzer, ohne Umweg.
    private var nichtsGefunden: some View {
        let eingabe = query.trimmingCharacters(in: .whitespacesAndNewlines)
        return VStack(alignment: .leading, spacing: 10) {
            Text("Nichts gefunden für „\(eingabe)“.")
                .font(.body)
            Text("Du kannst dir den Titel wünschen — der Besitzer sieht den Wunsch und sonst nichts.")
                .font(.callout).foregroundStyle(Theme.label2)
            if gewuenscht == eingabe {
                Label("Wunsch an den Besitzer geschickt", systemImage: "checkmark.circle.fill")
                    .font(.callout.weight(.medium)).foregroundStyle(Theme.good)
            } else {
                Button("„\(eingabe)“ wünschen") { Task { await wuenschen(eingabe) } }
                    .buttonStyle(PrimaryButtonStyle())
            }
            if let wunschFehler {
                Text(wunschFehler).font(.caption).foregroundStyle(Theme.bad)
            }
        }
        .frame(maxWidth: 460, alignment: .leading)
        .padding(.horizontal, 20)
    }

    private func wuenschen(_ titel: String) async {
        wunschFehler = nil
        do {
            _ = try await session.client.wish(title: titel)
            gewuenscht = titel
        } catch {
            wunschFehler = Session.text(error)
        }
    }

    private func load() async {
        let eingabe = query.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !eingabe.isEmpty else { return }
        error = nil
        do {
            let neu = try await session.client.search(eingabe)
            if Task.isCancelled { return }
            ergebnis = neu
            geladenFuer = eingabe
        } catch {
            if Task.isCancelled { return }
            self.error = Session.text(error)
        }
    }
}

/// Die Personen oben in der Suche: rund, Bild oder Anfangsbuchstabe,
/// Name, „3 Titel". Ein Tipp öffnet „alles mit …".
struct PersonenReihe: View {
    let personen: [PersonTreffer]

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text("Personen").font(.title3.weight(.semibold)).padding(.horizontal, 20)
            ScrollView(.horizontal, showsIndicators: false) {
                HStack(alignment: .top, spacing: 16) {
                    ForEach(personen) { person in
                        NavigationLink { PersonView(name: person.name) } label: {
                            VStack(spacing: 6) {
                                PersonBild(person: person, size: 76)
                                Text(person.name)
                                    .font(.caption.weight(.medium))
                                    .lineLimit(1)
                                Text(person.anzahlText)
                                    .font(.caption2).foregroundStyle(Theme.label2)
                            }
                            .frame(width: 92)
                        }
                        .buttonStyle(.plain)
                    }
                }
                .padding(.horizontal, 20)
            }
        }
    }
}

/// Das runde Bild einer Person — oder ihr Anfangsbuchstabe, wenn es
/// keins gibt. Das Bild kommt über die eigene Bilderroute des Servers.
struct PersonBild: View {
    @Environment(Session.self) private var session
    let person: PersonTreffer
    var size: CGFloat = 76
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
        .task {
            // `bild` ist ein Pfad wie /api/v1/art/{id}/cast03 — die Art
            // steht hinter dem letzten Schrägstrich.
            guard let bild = person.bild, let itemId = person.itemId,
                  let art = bild.split(separator: "/").last else { url = nil; return }
            url = await session.client.artworkURL(itemId: itemId, kind: String(art))
        }
    }
}

/// Die Personenseite (Issue #9): oben Foto, Lebensdaten, Biografie und
/// Quellen, darunter die Titel, in denen die Person mitspielt — soweit das
/// Profil sie sehen darf. Wie im Browser (app.js: viewPerson).
///
/// Kennt der Server die Person nicht (404) oder ist TMDb nicht
/// eingerichtet, bleibt es beim Namen und den Titeln — die Seite soll
/// deswegen nicht scheitern.
struct PersonView: View {
    @Environment(Session.self) private var session
    let name: String

    @State private var items: [Item] = []
    @State private var info: PersonInfo?
    @State private var bild: URL?
    @State private var bioGanz = false
    @State private var loading = true
    @State private var error: String?
    #if os(iOS)
    @Environment(\.horizontalSizeClass) private var breite
    private var schmal: Bool { breite == .compact }
    #else
    private var schmal: Bool { false }
    #endif

    /// Ab dieser Länge wird die Biografie auf sechs Zeilen gekürzt — etwa
    /// so viel, wie auf dem iPhone in sechs Zeilen passt.
    private static let kurzBis = 320

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 18) {
                if let info { kopf(info) }
                Text(items.count == 1 ? "1 Titel in deiner Mediathek" : "\(items.count) Titel in deiner Mediathek")
                    .font(.subheadline).foregroundStyle(Theme.label2)
                    .padding(.horizontal, 20)
                PosterRaster(items: items)
                    .padding(.horizontal, 20)
            }
            .padding(.top, 8)
            .padding(.bottom, 40)
        }
        .overlay { Loading(loading: loading, error: error, empty: items.isEmpty && info == nil, retry: load) }
        .navigationTitle(name)
        .task { await load() }
    }

    /// Foto links, daneben Daten und Text; auf dem schmalen iPhone bricht
    /// es um, damit die Biografie nicht in einer Spalte von zwei Wörtern
    /// steht.
    private func kopf(_ info: PersonInfo) -> some View {
        let anordnung = schmal
            ? AnyLayout(VStackLayout(alignment: .leading, spacing: 16))
            : AnyLayout(HStackLayout(alignment: .top, spacing: 22))
        return anordnung {
            foto
            angaben(info).frame(maxWidth: 680, alignment: .leading)
        }
        .padding(.horizontal, 20)
        .padding(.bottom, 8)
    }

    @ViewBuilder
    private var foto: some View {
        if let bild {
            Artwork(url: bild, title: "", corner: 16)
                .frame(width: 150, height: 200)
                .accessibilityHidden(true)
        }
    }

    private func angaben(_ info: PersonInfo) -> some View {
        VStack(alignment: .leading, spacing: 10) {
            if !info.lebensdaten.isEmpty {
                Text(info.lebensdaten).font(.subheadline).foregroundStyle(Theme.label2)
            }
            if let bio = info.biografie {
                Text(bio)
                    .font(.body).lineSpacing(3)
                    .lineLimit(bioGanz ? nil : 6)
                    .textSelection(.enabled)
                if !bioGanz, bio.count > Self.kurzBis {
                    Button("Mehr lesen") { bioGanz = true }
                        .buttonStyle(.plain)
                        .font(.subheadline.weight(.medium))
                        .foregroundStyle(Theme.accent)
                }
            }
            if !info.quellenText.isEmpty || info.bioNichtDeutsch {
                Text(quellenZeile(info))
                    .font(.caption).foregroundStyle(Theme.label2)
                    .tint(Theme.accent)
            }
        }
    }

    /// „Quelle: TMDb, Wikipedia · Biografie nur auf Englisch verfügbar" —
    /// die Namen als Links, aber nur bei http und https (`PersonQuelle.link`).
    private func quellenZeile(_ info: PersonInfo) -> AttributedString {
        var zeile = AttributedString()
        let quellen = (info.quellen ?? []).filter { !$0.name.isEmpty }
        if !quellen.isEmpty {
            zeile += AttributedString("Quelle: ")
            for (i, q) in quellen.enumerated() {
                if i > 0 { zeile += AttributedString(", ") }
                var teil = AttributedString(q.name)
                if let link = q.link { teil.link = link }
                zeile += teil
            }
        }
        if info.bioNichtDeutsch {
            if !quellen.isEmpty { zeile += AttributedString(" · ") }
            zeile += AttributedString(String(localized: "Biografie nur auf Englisch verfügbar", bundle: .lumen, locale: .lumen))
        }
        return zeile
    }

    private func load() async {
        error = nil
        // Die Angaben zur Person sind Beiwerk: fehlen sie, zählen nur die
        // Titel. Beides zugleich holen; der Client ist ein Actor und darf
        // aus der Nebenaufgabe heraus benutzt werden.
        let client = session.client
        let wer = name
        async let angaben = try? client.person(wer)
        do { items = try await session.client.items(person: name, sort: "title", limit: 200) }
        catch { self.error = Session.text(error) }
        if let neu = await angaben {
            info = neu
            bild = await session.client.personBildURL(neu)
        }
        loading = false
    }
}

/// Alle Filme eines Genres — hinter den Chips in der Suche. Wie im
/// Browser das Raster „Filme" mit diesem Genre gewählt; die anderen
/// Genres stehen daneben zum Wechseln.
struct GenreView: View {
    let kind: String
    let genre: String

    var body: some View {
        BrowseView(kind: kind, title: kind == "series" ? String(localized: "Serien", bundle: .lumen, locale: .lumen) : String(localized: "Filme", bundle: .lumen, locale: .lumen), startGenre: genre)
    }
}

// MARK: - Die Suche als eigener Reiter

/// Der Reiter „Suchen": das Suchfeld, und solange nichts drinsteht, ein
/// Hinweis, was alles geht.
/// Am Mac kommt man mit ⌘F hierher — dann soll man gleich tippen können.
/// Das Feld lässt sich erst ab macOS 15 und iOS 18 in den Fokus setzen;
/// davor bleibt es beim Wechsel zur Suche.
private struct SuchfeldFokus: ViewModifier {
    @FocusState private var fokus: Bool

    func body(content: Content) -> some View {
        #if os(macOS)
        if #available(macOS 15, *) {
            content
                .searchFocused($fokus)
                .onAppear { fokus = true }
                .onReceive(NotificationCenter.default.publisher(for: .lumenSuchen)) { _ in fokus = true }
        } else {
            content
        }
        #else
        content
        #endif
    }
}

struct SuchenView: View {
    @State private var search = ""

    private var sucht: Bool { !search.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty }

    var body: some View {
        ScrollView {
            if sucht {
                SuchErgebnisse(query: search)
            } else {
                VStack(alignment: .leading, spacing: 10) {
                    Text("Tippe los — auch „Komödie 2019“, „Filme mit Tom Hanks“ oder „Thriller 80er“.")
                        .font(.callout).foregroundStyle(Theme.label2)
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(20)
            }
        }
        .searchable(text: $search, placement: suchfeldOrt, prompt: suchPlatzhalter)
        .modifier(SuchfeldFokus())
        .navigationTitle("Suchen")
    }

    /// Auf iPhone und iPad steht das Feld fest oben, statt sich beim
    /// Scrollen wegzuschieben.
    private var suchfeldOrt: SearchFieldPlacement {
        #if os(iOS)
        .navigationBarDrawer(displayMode: .always)
        #else
        .automatic
        #endif
    }
}
