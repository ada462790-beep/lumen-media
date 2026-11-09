// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

import SwiftUI
import LumenKit

// Die Titelseite auf dem Fernseher (GitHub-Issue #1).
//
// Bisher spielte ein Klick auf eine Karte sofort. Das war schnell, liess
// aber keinen Platz für alles andere: zur Liste legen, merken, als
// gesehen markieren. Jetzt öffnet ein Klick diese Seite; „Abspielen"
// steht als Erstes und hat den Fokus, der schnelle Weg ist also einen
// Klick länger — und der lange Druck auf eine Karte spielt weiterhin
// sofort.
//
// Was hier steht, gehört dem angemeldeten Profil: Fortschritt, Haken,
// Merkliste. Niemand sonst sieht es, auch der Besitzer nicht.

// MARK: - Titelseite

struct TVDetailView: View {
    @Environment(Session.self) private var session
    let itemId: String
    /// Was die Karte schon wusste — damit die Seite nicht leer beginnt.
    var vorschau: Item? = nil

    @State private var detail: ItemDetail?
    @State private var error: String?
    @State private var playing: Abspielstart?
    @State private var nebenFokus: Item?
    /// Ob der Fernseher die Datei direkt abspielt — für die Technik-Zeile.
    @State private var plan: PlaybackPlan?
    /// Staffeln (Specials hinten), die gezeigte und ihre Folgen.
    @State private var staffeln: [Item] = []
    @State private var staffel: String?
    @State private var folgen: [Item] = []
    /// „In welcher Qualität?" — offen, und ob danach von vorn.
    @State private var qualitaetWahl = false
    @State private var qualitaetVonVorn = false
    /// Ob der Server „gesehen" für eine Staffel kennt — sonst kein Knopf.
    @State private var staffelGesehenGeht = true

    private var item: Item? { detail?.item ?? vorschau }

    var body: some View {
        ZStack(alignment: .topLeading) {
            TVBackdrop(item: item)

            ScrollView {
                VStack(alignment: .leading, spacing: 40) {
                    if let item {
                        kopf(item)
                        knoepfe(item)

                        if let cast = detail?.cast, !cast.isEmpty {
                            besetzung(item, cast)
                        }

                        if !staffeln.isEmpty {
                            staffelTeil
                        }

                        bewertung(item)

                        // Die Reihe, in der ein Film steht — sie führt weg
                        // von diesem Titel, darum weiter unten. Der Knopf
                        // darüber öffnet die ganze Reihe.
                        if let reihe = detail?.collection, !reihe.items.isEmpty {
                            VStack(alignment: .leading, spacing: 10) {
                                NavigationLink {
                                    TVCollectionView(reiheId: reihe.id, name: reihe.name)
                                } label: {
                                    Label("\(reihe.name) · \(reihe.anzahlText)", systemImage: "square.stack")
                                }
                                TVRail(title: String(localized: "Aus dieser Reihe", bundle: .lumen, locale: .lumen), items: reihe.items, focused: $nebenFokus,
                                       menu: { film in tvKartenMenue(film, session: session) { playing = $0 } })
                            }
                        }

                        // Ähnliche Titel — der Server wählt, aus dem, was
                        // dieses Profil sehen darf.
                        if let aehnlich = detail?.similar, !aehnlich.isEmpty {
                            TVRail(title: String(localized: "Ähnliche Titel", bundle: .lumen, locale: .lumen), items: aehnlich, focused: $nebenFokus,
                                   menu: { titel in tvKartenMenue(titel, session: session) { playing = $0 } })
                        }
                    } else if error == nil {
                        ProgressView().padding(.top, 200)
                    } else if let error {
                        TVFehler(text: error) { await load() }.padding(.top, 200)
                    }
                }
                .padding(.horizontal, 60)
                .padding(.bottom, 60)
            }
        }
        .task { await load() }
        .fullScreenCover(item: $playing) { s in
            TVPlayerView(item: s.item, vonVorn: s.vonVorn, datei: s.datei).environment(session)
        }
        // Zurück aus dem Player: Stand, Haken und „Weiter bei" auffrischen.
        .onChange(of: playing == nil) { _, zu in
            if zu { Task { await load() } }
        }
    }

    // MARK: Kopf

    private func kopf(_ item: Item) -> some View {
        VStack(alignment: .leading, spacing: 12) {
            Text(item.title)
                .font(.system(size: 62, weight: .bold)).lineLimit(2)
            if let original = item.originalTitle,
               original != item.title, !original.isEmpty {
                Text(original).font(.title3).foregroundStyle(Theme.label2)
            }
            Text(zeile(item)).font(.title3).foregroundStyle(Theme.label2)
            // 4K, Dolby Vision, 5.1, Sprachen, Untertitel —
            // kleine umrandete Abzeichen wie im Player.
            TechnikZeile(abzeichen: Technik.zeile(item: item, file: detail?.playable, plan: plan),
                         gross: true)
                .padding(.top, 4)
            if let overview = item.overview, !overview.isEmpty {
                Text(overview).font(.body).foregroundStyle(Theme.label2)
                    .lineLimit(6).frame(maxWidth: 1000, alignment: .leading)
            }
            if let error {
                Text(error).foregroundStyle(Theme.bad)
            }
        }
        .padding(.top, 60)
    }

    /// „2019 · 1 Std. 58 Min. · ab 16 · Drama · IMDb 7,8"
    private func zeile(_ item: Item) -> String {
        var parts: [String] = []
        if !item.subtitleLine.isEmpty { parts.append(item.subtitleLine) }
        if let rating = item.imdbRating, rating > 0 {
            parts.append("IMDb " + rating.formatted(.number.precision(.fractionLength(1)).locale(.lumen)))
        }
        if let t = item.tomatenText { parts.append(t) }
        return parts.joined(separator: " · ")
    }

    /// Abspielen, von vorn, zur Liste, merken, gesehen. Alles in einer
    /// Zeile, damit der Fokus nur nach rechts und links muss.
    @ViewBuilder
    private func knoepfe(_ item: Item) -> some View {
        // Mehrere Fassungen desselben Films (Issue #8): „Abspielen" fragt
        // zuerst nach der Qualität — als Dialog, durch den der Fokus geht.
        let fassungen = Fassungen.liste(detail?.files ?? [])
        HStack(spacing: 24) {
            // Eine Serie hat selbst keine Datei — der Player sucht ihre
            // erste offene Folge. Ein Film braucht eine, die da ist.
            if item.kind == "series" {
                Button {
                    playing = Abspielstart(item: item)
                } label: {
                    Label(Titelseite.serieAbspielen(item), systemImage: "play.fill")
                }
            } else if detail?.playable != nil {
                Button {
                    if fassungen.count > 1 {
                        qualitaetVonVorn = false
                        qualitaetWahl = true
                    } else {
                        playing = Abspielstart(item: item)
                    }
                } label: {
                    Label(Titelseite.abspielen(item), systemImage: "play.fill")
                }
                if Titelseite.kannVonVorn(item) {
                    Button {
                        if fassungen.count > 1 {
                            qualitaetVonVorn = true
                            qualitaetWahl = true
                        } else {
                            playing = Abspielstart(item: item, vonVorn: true)
                        }
                    } label: {
                        Label("Von vorn", systemImage: "backward.end")
                    }
                }
            }

            // Listen nehmen Filme und Serien auf, keine einzelnen Folgen.
            if item.kind == "movie" || item.kind == "series" {
                NavigationLink {
                    TVListenwahlView(itemId: item.id, title: item.title)
                } label: {
                    Label("Zur Liste", systemImage: "text.badge.plus")
                }
            }

            Button {
                Task { await merken(item) }
            } label: {
                Label(item.isOnList ? "Aus der Merkliste" : "Merken",
                      systemImage: item.isOnList ? "bookmark.fill" : "bookmark")
            }

            // Das Herz (Issue #10): was einem gefallen hat — anders als die
            // Merkliste, die sammelt, was man noch sehen will.
            if item.kind == "movie" || item.kind == "series" {
                Button {
                    Task { await favorit(item) }
                } label: {
                    // Auf drei Meter Abstand sagt das gefüllte Herz mehr als
                    // eine Farbe; der Text bleibt, damit der Knopf einen Namen hat.
                    Label("Favorit", systemImage: item.isFavorite ? "heart.fill" : "heart")
                }
                .accessibilityLabel(item.isFavorite ? "Aus den Favoriten" : "Zu den Favoriten")
                .accessibilityAddTraits(item.isFavorite ? .isSelected : [])
            }

            Button {
                Task { await gesehen(item) }
            } label: {
                // Bei einer Serie heisst „gesehen": alle Folgen.
                Label(item.kind == "series"
                        ? (item.isSeen ? "Serie nicht gesehen" : "Ganze Serie gesehen")
                        : (item.isSeen ? "Nicht gesehen" : "Gesehen"),
                      systemImage: item.isSeen ? "checkmark.circle.fill" : "checkmark.circle")
            }
        }
        .font(.title3.weight(.semibold))
        // Jede Fassung als Zeile („1080p · MKV · 7,9 GB"), der Fokus steht
        // auf der ersten: dem Standard, so wie der Server ordnet.
        .confirmationDialog("In welcher Qualität?", isPresented: $qualitaetWahl,
                            titleVisibility: .visible) {
            ForEach(Array(fassungen.enumerated()), id: \.element.id) { nr, f in
                Button(Fassungen.name(f) + (nr == 0 ? " · " + String(localized: "Standard", bundle: .lumen, locale: .lumen) : "")) {
                    playing = Abspielstart(item: item, vonVorn: qualitaetVonVorn, datei: f.id)
                }
            }
            Button("Abbrechen", role: .cancel) {}
        }
    }

    // MARK: Besetzung

    /// Die Besetzung als Reihe runder Bilder. Ein Klick öffnet „alles mit …".
    private func besetzung(_ item: Item, _ leute: [Mitwirkender]) -> some View {
        VStack(alignment: .leading, spacing: 10) {
            Text("Besetzung").font(.title2.weight(.semibold))
            ScrollView(.horizontal, showsIndicators: false) {
                LazyHStack(alignment: .top, spacing: 30) {
                    ForEach(leute) { p in
                        TVPersonKarte(name: p.name, zeile: p.rolle, itemId: item.id, art: p.bildArt)
                    }
                }
                .padding(.vertical, 20)
            }
        }
    }

    // MARK: Staffeln und Folgen

    /// Staffeln als Knöpfe, darunter die Folgen der gewählten als breite
    /// Karten mit Bild, Nummer, Haken und Balken. Ein Klick spielt.
    private var staffelTeil: some View {
        VStack(alignment: .leading, spacing: 14) {
            Text("Folgen").font(.title2.weight(.semibold))
            if staffeln.count > 1 {
                ScrollView(.horizontal, showsIndicators: false) {
                    HStack(spacing: 20) {
                        ForEach(staffeln) { st in
                            Button {
                                Task { await zeigeStaffel(st.id) }
                            } label: {
                                HStack(spacing: 10) {
                                    if staffel == st.id { Image(systemName: "checkmark") }
                                    Text(Folgenwahl.name(st))
                                }
                                .font(.callout.weight(staffel == st.id ? .semibold : .regular))
                            }
                        }
                    }
                    .padding(.vertical, 16)
                }
            }
            if folgen.isEmpty {
                Text("In dieser Staffel liegt noch keine Folge.")
                    .font(.callout).foregroundStyle(Theme.label2)
            } else {
                if staffelGesehenGeht, let gewaehlt = staffeln.first(where: { $0.id == staffel }),
                   gewaehlt.seasonNo != nil {
                    let alle = folgen.allSatisfy(\.isSeen)
                    Button {
                        Task { await staffelGesehen(gewaehlt, gesehen: !alle) }
                    } label: {
                        Label(alle ? "Staffel nicht gesehen" : "Staffel gesehen",
                              systemImage: alle ? "checkmark.circle.fill" : "checkmark.circle")
                    }
                    .font(.callout.weight(.semibold))
                }
                ScrollView(.horizontal, showsIndicators: false) {
                    LazyHStack(alignment: .top, spacing: 40) {
                        ForEach(folgen) { f in
                            // Ein langer Druck: die Folge als gesehen
                            // markieren, ohne sie abzuspielen.
                            TVFolgenKarte(folge: f) { playing = Abspielstart(item: f) }
                                .contextMenu {
                                    Button {
                                        Task { await gesehen(f) }
                                    } label: {
                                        Label(f.isSeen ? "Nicht gesehen" : "Gesehen",
                                              systemImage: f.isSeen ? "checkmark.circle.fill" : "checkmark.circle")
                                    }
                                }
                        }
                    }
                    .padding(.vertical, 24)
                }
            }
        }
    }

    // MARK: Bewertung

    /// Fünf Sterne, nur für dieses Profil — und darunter, was andere im
    /// Haushalt vergeben haben, sofern sie es zeigen.
    private func bewertung(_ item: Item) -> some View {
        VStack(alignment: .leading, spacing: 14) {
            Text("Deine Bewertung").font(.title2.weight(.semibold))
            HStack(spacing: 16) {
                ForEach(1...5, id: \.self) { n in
                    Button {
                        Task { await bewerten(item, sterne: n) }
                    } label: {
                        Image(systemName: (item.myRating ?? 0) >= n ? "star.fill" : "star")
                            .foregroundStyle((item.myRating ?? 0) >= n ? Theme.star : Theme.label2)
                    }
                    .accessibilityLabel("\(n) von 5")
                }
                if (item.myRating ?? 0) > 0 {
                    Button("Löschen") { Task { await bewerten(item, sterne: 0) } }
                }
            }
            Text("Nur für dich sichtbar.").font(.callout).foregroundStyle(Theme.label2)

            if let geteilt = detail?.sharedRatings, !geteilt.isEmpty {
                Text("Im Haushalt").font(.title3.weight(.semibold)).padding(.top, 10)
                ForEach(geteilt, id: \.name) { r in
                    HStack(spacing: 16) {
                        ProfilAvatar(name: r.name, avatar: r.avatar, size: 44)
                        Text(r.name)
                        Text(String(repeating: "★", count: r.stars)).foregroundStyle(Theme.star)
                    }
                }
            }
        }
    }

    // MARK: Laden und Ändern

    private func load() async {
        error = nil
        do {
            let neu = try await session.client.item(itemId)
            detail = neu
            if plan == nil, let file = neu.playable {
                plan = (try? await session.client.plan(fileId: file.id))?.plan
            }
            let geordnet = Titelseite.staffeln(neu.seasons ?? [])
            staffeln = geordnet
            let ziel = geordnet.first { $0.id == staffel }?.id ?? geordnet.first?.id
            if let ziel { await zeigeStaffel(ziel) }
        } catch { self.error = Session.text(error) }
    }

    private func zeigeStaffel(_ id: String) async {
        staffel = id
        let neu = AmStueck.geordnet((try? await session.client.children(of: id)) ?? [])
        if staffel == id { folgen = neu }
    }

    /// Eine Änderung an den Server — und was schiefging, steht danach im
    /// Kopf der Seite, statt verschluckt zu werden.
    private func aendern(_ arbeit: () async throws -> Void) async {
        do {
            try await arbeit()
        } catch {
            await load()
            self.error = Session.text(error)
            return
        }
        await load()
    }

    private func merken(_ item: Item) async {
        TVMerker.shared.vergessen(item.id)
        await aendern {
            if item.isOnList { try await session.client.removeFromWatchlist(item.id) }
            else { try await session.client.addToWatchlist(item.id) }
        }
    }

    private func favorit(_ item: Item) async {
        await aendern {
            if item.isFavorite { try await session.client.removeFavorite(item.id) }
            else { try await session.client.addFavorite(item.id) }
        }
    }

    private func gesehen(_ item: Item) async {
        await aendern {
            if item.isSeen { try await session.client.clearFinished(item.id) }
            else { try await session.client.markFinished(item.id) }
        }
    }

    /// Eine ganze Staffel. Kennt der Server das noch nicht (404), fällt
    /// der Knopf still weg.
    private func staffelGesehen(_ staffel: Item, gesehen: Bool) async {
        guard let serie = detail?.item, let nr = staffel.seasonNo else { return }
        do {
            if gesehen { try await session.client.markFinished(serie.id, staffel: nr) }
            else { try await session.client.clearFinished(serie.id, staffel: nr) }
            await load()
        } catch {
            if LumenError.fehltAufServer(error) {
                staffelGesehenGeht = false
                await load()
            } else {
                await load()
                self.error = Session.text(error)
            }
        }
    }

    private func bewerten(_ item: Item, sterne: Int) async {
        let teilen = session.me?.prefs["shareRatings"] == "1"
        await aendern { try await session.client.rate(item.id, stars: sterne, shared: teilen) }
    }
}

/// Eine Folge als breite Karte: Standbild mit Balken und Haken, darunter
/// Nummer und Name, Länge oder „Weiter".
struct TVFolgenKarte: View {
    @Environment(Session.self) private var session
    let folge: Item
    let spielen: () -> Void
    @State private var bild: URL?

    var body: some View {
        Button(action: spielen) {
            VStack(alignment: .leading, spacing: 10) {
                Artwork(url: bild, title: folge.title, corner: 10)
                    .frame(width: 400, height: 225)
                    .overlay(alignment: .bottomTrailing) { if folge.isSeen { SeenBadge() } }
                    .overlay(alignment: .bottom) {
                        if folge.percent > 1 && !folge.isSeen { ProgressStripe(percent: folge.percent) }
                    }
                Text("\(folge.episodeNo ?? 0). \(folge.title)")
                    .font(.body).lineLimit(1)
                    .frame(width: 400, alignment: .leading)
                Text(unterzeile)
                    .font(.caption).foregroundStyle(Theme.label2).lineLimit(1)
                    .frame(width: 400, alignment: .leading)
            }
        }
        .buttonStyle(.card)
        .accessibilityLabel(folge.isSeen
                            ? "Folge \(folge.episodeNo ?? 0): \(folge.title), gesehen"
                            : "Folge \(folge.episodeNo ?? 0): \(folge.title)")
        .task(id: folge.id) {
            bild = await session.client.artworkURL(for: folge, wide: true)
        }
    }

    private var unterzeile: String {
        var teile: [String] = []
        // Dieselbe Bedingung wie `Titelseite.folge` — nicht deren Text
        // vergleichen, der ist übersetzt.
        if folge.percent > 1 && !folge.isSeen { teile.append(String(localized: "Weiter", bundle: .lumen, locale: .lumen)) }
        if let ms = folge.runtimeMs, ms > 0 { teile.append(Item.duration(ms: ms)) }
        if let tag = folge.premiered, !tag.isEmpty {
            // „2024-03-07" → „7.3.2024", auf Englisch „3/7/2024" bzw. „7/3/2024".
            let t = tag.prefix(10).split(separator: "-")
            if t.count == 3, let j = Int(t[0]), let m = Int(t[1]), let d = Int(t[2]) {
                teile.append(Sprache.datum(jahr: j, monat: m, tag: d))
            }
        }
        return teile.joined(separator: " · ")
    }
}

// MARK: - Zur Liste

/// In welche Listen dieser Titel gehört — die eigenen und die geteilten,
/// bei denen alle mitschreiben. Ein Klick legt hinein oder nimmt heraus;
/// der Haken sagt, was gilt. Ganz unten lässt sich eine neue Liste anlegen,
/// der Titel landet gleich darin.
///
/// Die Rechnung dahinter ist `Listenwahl` in LumenKit, dieselbe wie auf dem
/// iPhone. Hier steht nur die Oberfläche für die Fernbedienung.
struct TVListenwahlView: View {
    @Environment(Session.self) private var session
    let itemId: String
    let title: String

    @State private var wahl: Listenwahl?
    @State private var neuerName = ""

    var body: some View {
        VStack(alignment: .leading, spacing: 24) {
            Text("Zur Liste").font(.system(size: 54, weight: .bold))
            Text("„\(title)“ in eine Liste legen oder herausnehmen.")
                .font(.title3).foregroundStyle(Theme.label2)

            if let wahl {
                if let fehler = wahl.error ?? wahl.problem {
                    Text(fehler).foregroundStyle(Theme.bad)
                }
                List {
                    Section {
                        ForEach(wahl.listen) { liste in
                            Button {
                                Task { await wahl.toggle(liste) }
                            } label: {
                                HStack {
                                    VStack(alignment: .leading, spacing: 4) {
                                        Text(liste.name)
                                        if !liste.meine {
                                            Text("von \(liste.von.name) · alle schreiben mit")
                                                .font(.caption).foregroundStyle(Theme.label2)
                                        }
                                    }
                                    Spacer()
                                    if wahl.busy == liste.id {
                                        ProgressView()
                                    } else if liste.hasItem {
                                        Image(systemName: "checkmark")
                                            .font(.body.weight(.semibold))
                                    }
                                }
                            }
                            .disabled(wahl.isBusy)
                        }
                        if !wahl.loading, wahl.listen.isEmpty {
                            Text("Du hast noch keine Liste. Lege unten eine an.")
                                .foregroundStyle(Theme.label2)
                        }
                    } header: {
                        Text("Listen")
                    }

                    Section {
                        TextField("Name der neuen Liste", text: $neuerName)
                        Button("Anlegen und hineinlegen") {
                            Task {
                                await wahl.createAndAdd(name: neuerName)
                                neuerName = ""
                            }
                        }
                        .disabled(wahl.isBusy
                                  || neuerName.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
                    } header: {
                        Text("Neue Liste")
                    } footer: {
                        Text("Deine Listen sieht nur du — ausser du zeigst eine davon im Haushalt. Das stellst du auf der Seite der Liste ein.")
                    }
                }
            } else {
                ProgressView()
            }
        }
        .padding(60)
        .task {
            let neu = Listenwahl(client: session.client, itemId: itemId)
            wahl = neu
            await neu.load()
        }
    }
}
