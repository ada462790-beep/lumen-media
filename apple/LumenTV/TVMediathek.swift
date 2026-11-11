// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

import SwiftUI
import LumenKit

// Die Reiter der Mediathek auf dem Fernseher: Filme und Serien mit ihren
// Genres, die Merkliste, alles mit einer Person, alle Filmreihen und alle
// Listen. Dieselben Seiten wie im Browser (app.js: viewGrid,
// viewWatchlist, viewPerson, viewCollections, viewPlaylists), gebaut für
// die Fernbedienung: grosse Karten, ein Fokus, der springt.

// MARK: - Filme und Serien

/// Oben die Genres des Servers als Knöpfe, darunter die Sortierung und —
/// bei A–Z — eine Reihe der Buchstaben, darunter die Karten.
/// Beim Herunterblättern lädt das Raster die nächste Seite nach.
struct TVRasterView: View {
    @Environment(Session.self) private var session
    let kind: String
    let title: String
    /// Ein Genre, das schon gewählt sein soll — aus der Suche.
    var startGenre: String? = nil

    @State private var raster: Raster?
    @State private var playing: Abspielstart?

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 34) {
                TVSeitenkopf(titel: title, zeile: raster?.geladen == true ? raster?.zeile : nil)
                    .padding(.top, 40)

                if let raster {
                    if !raster.genres.isEmpty {
                        ScrollView(.horizontal, showsIndicators: false) {
                            HStack(spacing: 20) {
                                wahlKnopf(String(localized: "Alle", bundle: .lumen, locale: .lumen), gewaehlt: raster.genre == nil) {
                                    Task { await raster.laden(genre: nil) }
                                }
                                ForEach(raster.genres, id: \.self) { g in
                                    wahlKnopf(g, gewaehlt: raster.genre == g) {
                                        Task { await raster.laden(genre: g) }
                                    }
                                }
                            }
                            .padding(.vertical, 20)
                        }
                    }

                    ScrollView(.horizontal, showsIndicators: false) {
                        HStack(spacing: 20) {
                            ForEach(Sortierung.faelle(kind: kind)) { art in
                                wahlKnopf(art.name(kind: kind), gewaehlt: raster.sortierung == art) {
                                    Task { await raster.sortieren(art) }
                                }
                            }
                            // Die Richtung als eigener Knopf; bei „Zufällig" neu mischen.
                            Button {
                                Task { await raster.richtungUmkehren() }
                            } label: {
                                Image(systemName: raster.sortierung == .zufall ? "shuffle"
                                      : (raster.absteigend ? "arrow.down" : "arrow.up"))
                                    .font(.callout.weight(.semibold))
                            }
                            .accessibilityLabel(raster.sortierung == .zufall
                                                ? String(localized: "Neu mischen", bundle: .lumen, locale: .lumen)
                                                : richtungName(absteigend: raster.absteigend))
                        }
                        .padding(.vertical, 20)
                    }

                    // Eine Reihe zum Durchwischen statt eines Index am Rand:
                    // die Fernbedienung kennt nur links, rechts, hoch, runter.
                    if raster.zeigtBuchstaben {
                        ScrollView(.horizontal, showsIndicators: false) {
                            HStack(spacing: 12) {
                                buchstabenKnopf(nil, raster)
                                ForEach(raster.buchstabenReihe, id: \.self) { b in
                                    buchstabenKnopf(b, raster)
                                }
                            }
                            .padding(.vertical, 20)
                        }
                    }

                    if let fehler = raster.error, raster.items.isEmpty {
                        TVFehler(text: fehler) { await raster.neu() }
                    } else if raster.geladen, raster.items.isEmpty {
                        Text("Hier ist noch nichts.").font(.title3).foregroundStyle(Theme.label2)
                    } else if !raster.geladen {
                        ProgressView().frame(maxWidth: .infinity).padding(.top, 80)
                    }

                    TVPosterGrid(items: raster.items,
                                 erscheint: { item in Task { await raster.erscheint(item) } },
                                 abspielen: { playing = $0 })

                    if raster.weiter {
                        ProgressView().frame(maxWidth: .infinity).padding(.vertical, 30)
                    }
                }
            }
            .padding(.horizontal, 60)
            .padding(.bottom, 60)
        }
        .task {
            guard raster == nil else { return }
            let neu = Raster(client: session.client, kind: kind, genre: startGenre,
                             gemerkt: session.me?.prefs[Sortierung.prefSchluessel(kind: kind)],
                             speicher: .standard,
                             merken: { schluessel, wert in
                                 Task { await session.prefsSetzen([schluessel: wert]) }
                             })
            raster = neu
            await neu.laden(genre: startGenre)
        }
        .fullScreenCover(item: $playing) { s in
            TVPlayerView(item: s.item, vonVorn: s.vonVorn).environment(session)
        }
    }

    /// Ein Knopf für Genre oder Sortierung. Der gewählte trägt einen Haken —
    /// die Farbe allein sähe man auf drei Meter Abstand nicht sicher.
    private func wahlKnopf(_ name: String, gewaehlt: Bool, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            HStack(spacing: 10) {
                if gewaehlt { Image(systemName: "checkmark") }
                Text(name)
            }
            .font(.callout.weight(gewaehlt ? .semibold : .regular))
        }
        .accessibilityAddTraits(gewaehlt ? .isSelected : [])
    }

    /// Ein Buchstabe. Ohne Titel lässt er sich nicht wählen — dann springt
    /// der Fokus über ihn hinweg. Der gewählte ist fett und unterstrichen.
    private func buchstabenKnopf(_ buchstabe: String?, _ raster: Raster) -> some View {
        let gewaehlt = raster.buchstabe == buchstabe
        let anzahl = buchstabe.map { raster.anzahl($0) } ?? raster.gesamt
        let leer = buchstabe != nil && anzahl == 0
        return Button {
            Task { await raster.springe(zu: buchstabe) }
        } label: {
            Text(buchstabe ?? String(localized: "Alle", bundle: .lumen, locale: .lumen))
                .font(.callout.weight(gewaehlt ? .bold : .regular))
                .underline(gewaehlt)
                .frame(minWidth: 40)
        }
        .disabled(leer)
        .accessibilityLabel(buchstabe.map { Buchstaben.vorlesen($0, anzahl: anzahl) } ?? String(localized: "Alle Buchstaben", bundle: .lumen, locale: .lumen))
        .accessibilityAddTraits(gewaehlt ? .isSelected : [])
    }
}

// MARK: - Merkliste

struct TVMerklisteView: View {
    @Environment(Session.self) private var session
    /// „merkliste" oder „gesehen" — wie die Reiter im Browser.
    @State private var liste = "merkliste"
    @State private var items: [Item] = []
    @State private var geladen = false
    @State private var playing: Abspielstart?
    /// Ein Fehler ist nicht „noch nichts gemerkt".
    @State private var fehler: String?

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 34) {
                TVSeitenkopf(titel: liste == "gesehen" ? String(localized: "Gesehen", bundle: .lumen, locale: .lumen) : String(localized: "Merkliste", bundle: .lumen, locale: .lumen),
                             zeile: liste == "gesehen" ? String(localized: "Fertig geschaut · nur du siehst das", bundle: .lumen, locale: .lumen) : String(localized: "Nur du siehst diese Liste", bundle: .lumen, locale: .lumen))
                    .padding(.top, 40)
                HStack(spacing: 20) {
                    listenKnopf("merkliste", String(localized: "Merkliste", bundle: .lumen, locale: .lumen))
                    listenKnopf("gesehen", String(localized: "Gesehen", bundle: .lumen, locale: .lumen))
                }
                .padding(.vertical, 10)
                if let fehler, items.isEmpty {
                    TVFehler(text: fehler) { await load() }
                } else if geladen && items.isEmpty {
                    Text(liste == "gesehen"
                         ? "Noch nichts zu Ende geschaut."
                         : "Noch nichts gemerkt. Auf der Seite eines Titels legst du etwas hierher.")
                        .font(.title3).foregroundStyle(Theme.label2)
                } else if !geladen {
                    ProgressView().frame(maxWidth: .infinity).padding(.top, 80)
                }
                TVPosterGrid(items: items, abspielen: { playing = $0 })
            }
            .padding(.horizontal, 60)
            .padding(.bottom, 60)
        }
        // Bei jedem Öffnen frisch — auf einer Titelseite kann sich etwas
        // geändert haben.
        .onAppear { Task { await load() } }
        // Aus dem Menü einer Karte gemerkt oder entfernt: neu laden.
        .onChange(of: TVMerker.shared.stand) { _, _ in Task { await load() } }
        .fullScreenCover(item: $playing) { s in
            TVPlayerView(item: s.item, vonVorn: s.vonVorn).environment(session)
        }
    }

    /// Nur die eigene Liste, vom Server.
    private func load() async {
        let gewollt = liste
        do {
            let seite = gewollt == "gesehen"
                ? try await session.client.gesehen()
                : try await session.client.merkliste()
            guard gewollt == liste else { return }
            items = seite.items
            fehler = nil
        } catch {
            guard gewollt == liste, !APIClient.istAbbruch(error) else { return }
            fehler = Session.text(error)
        }
        geladen = true
    }

    private func listenKnopf(_ wert: String, _ name: String) -> some View {
        let gewaehlt = liste == wert
        return Button {
            guard !gewaehlt else { return }
            liste = wert
            geladen = false
            items = []
            Task { await load() }
        } label: {
            HStack(spacing: 10) {
                if gewaehlt { Image(systemName: "checkmark") }
                Text(name)
            }
            .font(.callout.weight(gewaehlt ? .semibold : .regular))
        }
        .accessibilityAddTraits(gewaehlt ? .isSelected : [])
    }
}

// MARK: - Favoriten

/// Was einem besonders gefallen hat, getrennt nach Filmen und
/// Serien. Nur dieses Profil sieht es.
struct TVFavoritenView: View {
    @Environment(Session.self) private var session
    @State private var art = "movie"
    @State private var items: [Item] = []
    @State private var geladen = false
    @State private var error: String?
    @State private var playing: Abspielstart?

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 34) {
                TVSeitenkopf(titel: String(localized: "Favoriten", bundle: .lumen, locale: .lumen), zeile: String(localized: "Was dir besonders gefallen hat · nur du siehst das", bundle: .lumen, locale: .lumen))
                    .padding(.top, 40)
                HStack(spacing: 20) {
                    artKnopf("movie", String(localized: "Filme", bundle: .lumen, locale: .lumen))
                    artKnopf("series", String(localized: "Serien", bundle: .lumen, locale: .lumen))
                }
                .padding(.vertical, 10)
                if let error {
                    TVFehler(text: error) { await load() }
                } else if geladen && items.isEmpty {
                    Text(art == "movie"
                         ? "Noch keine Filme mit Herz. Auf der Seite eines Titels setzt du eins."
                         : "Noch keine Serien mit Herz. Auf der Seite eines Titels setzt du eins.")
                        .font(.title3).foregroundStyle(Theme.label2)
                } else if !geladen {
                    ProgressView().frame(maxWidth: .infinity).padding(.top, 80)
                }
                TVPosterGrid(items: items, abspielen: { playing = $0 })
            }
            .padding(.horizontal, 60)
            .padding(.bottom, 60)
        }
        // Bei jedem Öffnen frisch — auf einer Titelseite kann ein Herz
        // dazugekommen oder weggefallen sein.
        .onAppear { Task { await load() } }
        .fullScreenCover(item: $playing) { s in
            TVPlayerView(item: s.item, vonVorn: s.vonVorn).environment(session)
        }
    }

    private func artKnopf(_ wert: String, _ name: String) -> some View {
        let gewaehlt = art == wert
        return Button {
            guard !gewaehlt else { return }
            art = wert
            Task { await load() }
        } label: {
            HStack(spacing: 10) {
                if gewaehlt { Image(systemName: "checkmark") }
                Text(name)
            }
            .font(.callout.weight(gewaehlt ? .semibold : .regular))
        }
        .accessibilityAddTraits(gewaehlt ? .isSelected : [])
    }

    private func load() async {
        let gewollt = art
        error = nil
        do {
            let seite = try await session.client.favoriten(kind: gewollt)
            guard gewollt == art else { return }
            items = seite.items
        } catch {
            guard gewollt == art else { return }
            self.error = Session.text(error)
        }
        geladen = true
    }
}

// MARK: - Alles mit einer Person

/// Die Personenseite: Foto, Lebensdaten, Biografie, Quellen —
/// darunter die Titel, in denen die Person mitspielt, soweit das Profil
/// sie sehen darf. Hinter der Besetzung und den Personen in der Suche.
///
/// Die Quellen stehen nur als Text da: einen Link kann der Fernseher
/// nicht öffnen.
struct TVPersonView: View {
    @Environment(Session.self) private var session
    let name: String

    @State private var items: [Item] = []
    @State private var info: PersonInfo?
    @State private var bild: URL?
    @State private var bioGanz = false
    @State private var geladen = false
    @State private var error: String?
    @State private var playing: Abspielstart?

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 34) {
                TVSeitenkopf(titel: name, zeile: geladen
                    ? String(localized: "\(items.count) Titel in deiner Mediathek", bundle: .lumen, locale: .lumen)
                    : nil)
                    .padding(.top, 40)
                if let info { angaben(info) }
                if let error {
                    TVFehler(text: error) { await load() }
                } else if geladen && items.isEmpty {
                    Text("Nichts dabei, was du sehen darfst.").font(.title3).foregroundStyle(Theme.label2)
                } else if !geladen {
                    ProgressView().frame(maxWidth: .infinity).padding(.top, 80)
                }
                TVPosterGrid(items: items, abspielen: { playing = $0 })
            }
            .padding(.horizontal, 60)
            .padding(.bottom, 60)
        }
        .task { await load() }
        .fullScreenCover(item: $playing) { s in
            TVPlayerView(item: s.item, vonVorn: s.vonVorn).environment(session)
        }
    }

    private func angaben(_ info: PersonInfo) -> some View {
        HStack(alignment: .top, spacing: 40) {
            if let bild {
                Artwork(url: bild, title: "", corner: 16)
                    .frame(width: 240, height: 320)
                    .accessibilityHidden(true)
            }
            VStack(alignment: .leading, spacing: 16) {
                if !info.lebensdaten.isEmpty {
                    Text(info.lebensdaten).font(.title3).foregroundStyle(Theme.label2)
                }
                if let bio = info.biografie {
                    Text(bio)
                        .font(.body)
                        .lineLimit(bioGanz ? nil : 6)
                    // Ein Knopf, kein Tippen auf den Text: nur was den Fokus
                    // nehmen kann, erreicht die Fernbedienung.
                    if bio.count > 400 {
                        Button(bioGanz ? "Weniger" : "Mehr lesen") { bioGanz.toggle() }
                            .font(.callout)
                    }
                }
                if let zeile = quellenZeile(info) {
                    Text(zeile).font(.caption).foregroundStyle(Theme.label2)
                }
            }
            .frame(maxWidth: 1100, alignment: .leading)
        }
    }

    /// „Quelle: TMDb, Wikipedia · Biografie nur auf Englisch verfügbar"
    private func quellenZeile(_ info: PersonInfo) -> String? {
        var teile: [String] = []
        if !info.quellenText.isEmpty { teile.append("Quelle: \(info.quellenText)") }
        if info.bioNichtDeutsch { teile.append(String(localized: "Biografie nur auf Englisch verfügbar", bundle: .lumen, locale: .lumen)) }
        return teile.isEmpty ? nil : teile.joined(separator: " · ")
    }

    private func load() async {
        // Fehlen die Angaben zur Person (404, kein TMDb), bleibt es bei den Titeln.
        let client = session.client
        let wer = name
        async let angaben = try? client.person(wer)
        do { items = try await session.client.items(person: name, sort: "title", limit: 200) }
        catch { self.error = Session.text(error) }
        if let neu = await angaben {
            info = neu
            bild = await session.client.personBildURL(neu)
        }
        geladen = true
    }
}

// MARK: - Alle Filmreihen

struct TVFilmreihenView: View {
    @Environment(Session.self) private var session
    @State private var reihen: [Reihe] = []
    /// Die eigenen Sammlungen, unter den Reihen. Ein älterer Server kennt
    /// sie nicht; dann bleibt es bei den Reihen.
    @State private var sammlungen: [Sammlung] = []
    @State private var geladen = false
    @State private var error: String?

    private let spalten = [GridItem(.adaptive(minimum: 260, maximum: 280), spacing: 40, alignment: .top)]

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 34) {
                TVSeitenkopf(titel: String(localized: "Filmreihen", bundle: .lumen, locale: .lumen), zeile: reihen.isEmpty ? nil
                    : String(localized: "\(reihen.count) Reihen · Lumen stellt sie selbst zusammen", bundle: .lumen, locale: .lumen))
                    .padding(.top, 40)
                if let error {
                    TVFehler(text: error) { await load() }
                } else if geladen && reihen.isEmpty && sammlungen.isEmpty {
                    Text("Noch keine Reihen. Lumen erkennt sie von selbst, sobald zwei Filme einer Reihe da sind.")
                        .font(.title3).foregroundStyle(Theme.label2)
                        .frame(maxWidth: 1100, alignment: .leading)
                } else if !geladen {
                    ProgressView().frame(maxWidth: .infinity).padding(.top, 80)
                }
                LazyVGrid(columns: spalten, alignment: .leading, spacing: 50) {
                    ForEach(reihen) { reihe in
                        NavigationLink {
                            TVCollectionView(reiheId: reihe.id, name: reihe.name)
                        } label: {
                            TVStapelLabel(titel: reihe.name, zeile: reihe.anzahlText, bild: reihe.items.first, reihe: reihe)
                        }
                        .buttonStyle(.card)
                    }
                }
                if !sammlungen.isEmpty {
                    Text("Sammlungen").font(.title2.weight(.semibold))
                        .padding(.top, 20)
                    LazyVGrid(columns: spalten, alignment: .leading, spacing: 50) {
                        ForEach(sammlungen) { sammlung in
                            NavigationLink {
                                TVSammlungView(sammlungId: sammlung.id, name: sammlung.name)
                            } label: {
                                TVStapelLabel(titel: sammlung.name, zeile: sammlung.anzahlText,
                                              bild: sammlung.items.first, sammlung: sammlung)
                            }
                            .buttonStyle(.card)
                        }
                    }
                }
            }
            .padding(.horizontal, 60)
            .padding(.bottom, 60)
        }
        .task { await load() }
    }

    private func load() async {
        error = nil
        do { reihen = try await session.client.collections() }
        catch { if !APIClient.istAbbruch(error) { self.error = Session.text(error) } }
        // Beiwerk: ohne Sammlungen (älterer Server) bleibt es still.
        sammlungen = (try? await session.client.sammlungen()) ?? []
        geladen = true
    }
}

// MARK: - Alle Listen

/// Die eigenen Listen, dahinter „Neue Liste", darunter die im Haushalt
/// geteilten. Eine Liste sieht nur, wer sie angelegt hat — bis sie geteilt
/// wird; auch der Besitzer des Servers sieht sie vorher nicht.
struct TVListenView: View {
    @Environment(Session.self) private var session
    @State private var listen: [Liste] = []
    @State private var geladen = false
    @State private var error: String?

    private let spalten = [GridItem(.adaptive(minimum: 260, maximum: 280), spacing: 40, alignment: .top)]

    private var meine: [Liste] { listen.filter(\.meine) }
    private var andere: [Liste] { listen.filter { !$0.meine } }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 34) {
                TVSeitenkopf(titel: String(localized: "Listen", bundle: .lumen, locale: .lumen),
                             zeile: String(localized: "Eigene Zusammenstellungen — für dich oder mit anderen im Haushalt", bundle: .lumen, locale: .lumen))
                    .padding(.top, 40)
                if let error {
                    TVFehler(text: error) { await load() }
                }

                Text("Meine Listen").font(.title2.weight(.semibold))
                LazyVGrid(columns: spalten, alignment: .leading, spacing: 50) {
                    ForEach(meine) { liste in
                        listenKarte(liste)
                    }
                    NavigationLink {
                        TVNameView(titel: String(localized: "Neue Liste", bundle: .lumen, locale: .lumen), vorgabe: "", knopf: String(localized: "Anlegen", bundle: .lumen, locale: .lumen)) { name in
                            _ = try await session.client.createPlaylist(name: name)
                        }
                    } label: {
                        TVNeuLabel(titel: String(localized: "Neue Liste", bundle: .lumen, locale: .lumen))
                    }
                    .buttonStyle(.card)
                }

                if !andere.isEmpty {
                    Text("Im Haushalt geteilt").font(.title2.weight(.semibold)).padding(.top, 20)
                    LazyVGrid(columns: spalten, alignment: .leading, spacing: 50) {
                        ForEach(andere) { liste in
                            listenKarte(liste)
                        }
                    }
                }

                Label("Eine Liste sieht nur, wer sie angelegt hat — bis sie geteilt wird. Auch der Besitzer des Servers sieht sie vorher nicht.", systemImage: "lock")
                    .font(.callout).foregroundStyle(Theme.label2)
                    .padding(.top, 20)
            }
            .padding(.horizontal, 60)
            .padding(.bottom, 60)
        }
        // Zurück vom Anlegen, Umbenennen oder Löschen: frisch laden.
        .onAppear { Task { await load() } }
    }

    private func listenKarte(_ liste: Liste) -> some View {
        NavigationLink {
            TVPlaylistView(listeId: liste.id, name: liste.name)
        } label: {
            TVStapelLabel(titel: liste.name,
                          zeile: liste.meine ? liste.anzahlText : String(localized: "\(liste.anzahlText) · von \(liste.von.name)", bundle: .lumen, locale: .lumen),
                          bild: liste.vorschau?.first)
        }
        .buttonStyle(.card)
    }

    private func load() async {
        error = nil
        do { listen = try await session.client.playlists() }
        catch { self.error = Session.text(error) }
        geladen = true
    }
}
