// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

import SwiftUI
import LumenKit

// Die Leiste oben, wie in der TV-App von Apple: ein Reiter je Bereich,
// dieselben wie in der Seitenleiste des Browsers. Jeder Reiter hat seinen
// eigenen Stapel — die Menü-Taste führt darin zurück und, ganz oben
// angekommen, in die Leiste.
//
// Bewusst die klassische Leiste und nicht die Seitenleiste von tvOS 18:
// Lumen läuft ab tvOS 17, und die Leiste oben kennt jeder vom Apple TV.
// Eine Verwaltung gibt es auf dem Fernseher nicht — die steht im Browser
// und auf dem Mac.

struct TVShell: View {
    @Environment(Session.self) private var session
    @State private var reiter: Reiter = TVShell.zuletzt

    /// Der zuletzt offene Reiter — damit ein Sprachwechsel, nach dem sich
    /// die App neu aufbaut, nicht auf die Startseite zurückspringt.
    private static var zuletzt: Reiter = .home
    private var merker: TVMerker { TVMerker.shared }

    // Sechs Punkte, nicht elf: eine Leiste, die breiter ist als der
    // Bildschirm, kennt auf dem Apple TV niemand, und der Fokus müsste
    // weit wandern. Was seltener gebraucht wird — Favoriten, Filmreihen,
    // Listen, Neuigkeiten, Wünsche, Geräte —, steht unter dem eigenen
    // Namen ganz rechts.
    enum Reiter: Hashable {
        case home, filme, serien, merkliste, suche, ich
    }

    var body: some View {
        TabView(selection: $reiter) {
            NavigationStack { TVHomeView() }
                .tabItem { Text("Startseite") }
                .tag(Reiter.home)
            NavigationStack { TVRasterView(kind: "movie", title: String(localized: "Filme", bundle: .lumen, locale: .lumen)) }
                .tabItem { Text("Filme") }
                .tag(Reiter.filme)
            NavigationStack { TVRasterView(kind: "series", title: String(localized: "Serien", bundle: .lumen, locale: .lumen)) }
                .tabItem { Text("Serien") }
                .tag(Reiter.serien)
            NavigationStack { TVMerklisteView() }
                .tabItem { Text("Merkliste") }
                .tag(Reiter.merkliste)
            NavigationStack { TVSucheView() }
                .tabItem { Label("Suche", systemImage: "magnifyingglass") }
                .tag(Reiter.suche)
            NavigationStack { TVProfilView() }
                // Der Zähler der Neuigkeiten steht am eigenen Namen — dort
                // liegen sie jetzt. Nur die eigenen, ungelesenen.
                .tabItem { Text(ichTitel) }
                .tag(Reiter.ich)
        }
        .onChange(of: reiter) { Self.zuletzt = reiter }
        // Merken aus dem Menü einer Karte: klappt es nicht, steht es hier.
        .alert("Das hat nicht geklappt", isPresented: Binding(
            get: { merker.meldung != nil }, set: { if !$0 { merker.meldung = nil } })) {
            Button("OK", role: .cancel) {}
        } message: {
            Text(merker.meldung ?? "")
        }
    }

    private var ichTitel: String {
        let name = session.me?.account.name ?? String(localized: "Profil", bundle: .lumen, locale: .lumen)
        let n = session.me?.ungelesen ?? 0
        if n == 0 { return name }
        return name + " · " + (n > 9 ? "9+" : String(n))
    }
}

// MARK: - Seitenkopf

/// Die grosse Überschrift einer Seite, darunter eine Zeile in Grau.
struct TVSeitenkopf: View {
    let titel: String
    var zeile: String? = nil

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(titel).font(.system(size: 62, weight: .bold)).lineLimit(2)
            if let zeile, !zeile.isEmpty {
                Text(zeile).font(.title3).foregroundStyle(Theme.label2)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

// MARK: - Das Menü an einer Karte

/// Was ein langer Druck auf eine Karte anbietet: abspielen, von vorn,
/// merken — im Browser die Karte, die beim Drüberfahren aufklappt.
@MainActor
@ViewBuilder
func tvKartenMenue(_ item: Item, session: Session,
                   abspielen: @escaping (Abspielstart) -> Void) -> some View {
    if ["movie", "series", "episode"].contains(item.kind) {
        Button {
            abspielen(Abspielstart(item: item))
        } label: {
            Label(item.kind == "series" ? Titelseite.serieAbspielen(item) : Titelseite.abspielen(item),
                  systemImage: "play.fill")
        }
        if item.kind != "series", Titelseite.kannVonVorn(item) {
            Button {
                abspielen(Abspielstart(item: item, vonVorn: true))
            } label: {
                Label("Von vorn", systemImage: "backward.end")
            }
        }
    }
    if item.kind == "movie" || item.kind == "series" {
        // Über `TVMerker`: so heisst der Eintrag beim nächsten Öffnen
        // richtig, und ein Fehler wird gezeigt statt verschluckt.
        let gemerkt = TVMerker.shared.istGemerkt(item)
        Button {
            Task { await TVMerker.shared.umschalten(item, client: session.client) }
        } label: {
            Label(gemerkt ? "Aus der Merkliste" : "Merken",
                  systemImage: gemerkt ? "bookmark.fill" : "bookmark")
        }
    }
}

// MARK: - Raster aus Karten

/// Karten in Spalten, für Filme, Serien, Merkliste, Suche und Personen.
/// Ein Klick öffnet die Titelseite, ein langer Druck das Menü. Die Karte
/// im Fokus zeigt nach einer Sekunde ihre bewegte Vorschau.
struct TVPosterGrid: View {
    @Environment(Session.self) private var session
    let items: [Item]
    /// Wird für jede Karte gerufen, die erscheint — zum Nachladen.
    var erscheint: ((Item) -> Void)? = nil
    let abspielen: (Abspielstart) -> Void
    @State private var fokusId: String?

    private let spalten = [GridItem(.adaptive(minimum: 260, maximum: 280), spacing: 40, alignment: .top)]

    var body: some View {
        LazyVGrid(columns: spalten, alignment: .leading, spacing: 50) {
            ForEach(items) { item in
                TVCard(item: item, vorschauAn: fokusId == item.id,
                       menu: { tvKartenMenue(item, session: session, abspielen: abspielen) })
                    .onFocusChangeCompat { jetzt in
                        if jetzt { fokusId = item.id } else if fokusId == item.id { fokusId = nil }
                    }
                    .onAppear { erscheint?(item) }
            }
        }
    }
}

// MARK: - Runde Bilder für Personen

/// Ein rundes Bild über die eigene Bilderroute des Servers, sonst der
/// Anfangsbuchstabe — für die Besetzung und die Personen in der Suche.
struct TVRundBild: View {
    @Environment(Session.self) private var session
    let name: String
    /// Der Titel, an dem das Bild hängt, und die Bildart („cast03").
    let itemId: String?
    let art: String?
    var size: CGFloat = 150
    @State private var url: URL?

    var body: some View {
        ZStack {
            Circle().fill(Theme.fill)
            Text(String(name.prefix(1)).uppercased())
                .font(.system(size: size * 0.4, weight: .medium))
                .foregroundStyle(Theme.label2)
            if let url {
                MedienBild(url: url)
            }
        }
        .frame(width: size, height: size)
        .clipShape(Circle())
        .task(id: "\(itemId ?? "")|\(art ?? "")") {
            guard let itemId, let art else { url = nil; return }
            url = await session.client.artworkURL(itemId: itemId, kind: art)
        }
    }
}

/// Eine Person als Karte: rundes Bild, Name, darunter Rolle oder Anzahl.
/// Ein Klick öffnet „alles mit …".
struct TVPersonKarte: View {
    let name: String
    let zeile: String?
    let itemId: String?
    let art: String?

    var body: some View {
        NavigationLink {
            TVPersonView(name: name)
        } label: {
            VStack(spacing: 12) {
                TVRundBild(name: name, itemId: itemId, art: art, size: 150)
                Text(name).font(.callout.weight(.medium)).lineLimit(1)
                if let zeile, !zeile.isEmpty {
                    Text(zeile).font(.caption).foregroundStyle(Theme.label2).lineLimit(1)
                }
            }
            .frame(width: 200)
            .padding(.vertical, 12)
        }
        .buttonStyle(.borderless)
        .accessibilityLabel("Alles mit \(name)")
    }
}

/// Die Bildart aus einem Pfad wie /api/v1/art/{id}/cast03.
func tvBildArt(_ pfad: String?) -> String? {
    guard let pfad, let letzte = pfad.split(separator: "/").last, !letzte.isEmpty else { return nil }
    return String(letzte)
}
