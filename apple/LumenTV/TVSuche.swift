// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

import SwiftUI
import LumenKit

// Die Suche auf dem Fernseher: oben die Tastatur des Systems, darunter wie
// im Browser die Filmreihen und Sammlungen, die Personen, die passen, die
// Genres, eine Zeile, wie die Eingabe gedeutet wurde („12 Treffer · mit Tom Hanks · 1990–1999"), und
// die Titel. Findet sich nichts, ist der Wunsch einen Klick entfernt.

struct TVSucheView: View {
    @Environment(Session.self) private var session
    @State private var eingabe = ""
    @State private var ergebnis: Suchergebnis?
    @State private var geladenFuer = ""
    @State private var error: String?
    @State private var playing: Abspielstart?
    @State private var gewuenscht: String?
    @State private var wunschFehler: String?

    private var sauber: String { eingabe.trimmingCharacters(in: .whitespacesAndNewlines) }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 34) {
                if sauber.isEmpty {
                    Text("Titel, Schauspieler, Genre, Jahr — auch „Komödie 2019“, „Filme mit Tom Hanks“ oder „Thriller 80er“.")
                        .font(.title3).foregroundStyle(Theme.label2)
                        .frame(maxWidth: 1200, alignment: .leading)
                } else if let error {
                    Text(error).foregroundStyle(Theme.bad)
                } else if let ergebnis, geladenFuer == sauber {
                    treffer(ergebnis)
                } else {
                    ProgressView().frame(maxWidth: .infinity).padding(.top, 60)
                }
            }
            .padding(.horizontal, 60)
            .padding(.vertical, 40)
        }
        .searchable(text: $eingabe, prompt: "Titel, Schauspieler, Genre, Jahr …")
        // Kurz warten, bis nicht mehr getippt wird — sonst ginge jede
        // Taste als Anfrage an den Server.
        .task(id: sauber) {
            guard !sauber.isEmpty else { ergebnis = nil; return }
            try? await Task.sleep(for: .milliseconds(350))
            if Task.isCancelled { return }
            await suche(sauber)
        }
        .fullScreenCover(item: $playing) { s in
            TVPlayerView(item: s.item, vonVorn: s.vonVorn).environment(session)
        }
    }

    @ViewBuilder
    private func treffer(_ e: Suchergebnis) -> some View {
        // Filmreihen und Sammlungen, deren Name passt — „Harry Potter"
        // findet die Reihe, nicht nur die einzelnen Filme.
        if e.hatReihenOderSammlungen {
            VStack(alignment: .leading, spacing: 14) {
                Text("Filmreihen und Sammlungen").font(.title2.weight(.semibold))
                ScrollView(.horizontal, showsIndicators: false) {
                    LazyHStack(spacing: 40) {
                        ForEach(e.reihen ?? []) { reihe in
                            NavigationLink {
                                TVCollectionView(reiheId: reihe.id, name: reihe.name)
                            } label: {
                                TVStapelLabel(titel: reihe.name, zeile: reihe.anzahlText,
                                              bild: reihe.items.first, reihe: reihe)
                            }
                            .buttonStyle(.card)
                        }
                        ForEach(e.sammlungen ?? []) { sammlung in
                            NavigationLink {
                                TVSammlungView(sammlungId: sammlung.id, name: sammlung.name)
                            } label: {
                                TVStapelLabel(titel: sammlung.name, zeile: sammlung.anzahlText,
                                              bild: sammlung.items.first, sammlung: sammlung)
                            }
                            .buttonStyle(.card)
                        }
                    }
                    .padding(.vertical, 24)
                }
            }
        }
        if let personen = e.personen, !personen.isEmpty {
            VStack(alignment: .leading, spacing: 14) {
                Text("Personen").font(.title2.weight(.semibold))
                ScrollView(.horizontal, showsIndicators: false) {
                    HStack(alignment: .top, spacing: 30) {
                        ForEach(personen) { p in
                            TVPersonKarte(name: p.name, zeile: p.anzahlText,
                                          itemId: p.itemId, art: tvBildArt(p.bild))
                        }
                    }
                    .padding(.vertical, 20)
                }
            }
        }
        if let genres = e.genres, !genres.isEmpty {
            ScrollView(.horizontal, showsIndicators: false) {
                HStack(spacing: 20) {
                    ForEach(genres, id: \.self) { g in
                        // Wie im Browser: das Raster „Filme" mit diesem Genre.
                        NavigationLink {
                            TVRasterView(kind: "movie", title: String(localized: "Filme", bundle: .lumen, locale: .lumen), startGenre: g)
                        } label: {
                            Text(g).font(.callout)
                        }
                    }
                }
                .padding(.vertical, 20)
            }
        }
        if !e.items.isEmpty {
            Text(e.trefferzeile).font(.title3).foregroundStyle(Theme.label2)
            TVPosterGrid(items: e.items, abspielen: { playing = $0 })
        } else if !e.leer {
            Text(e.deutung?.gedeutet == true
                 ? "Kein Titel passt genau — gesucht wurde \(e.deutung?.beschreibung ?? "")."
                 : "Kein Titel passt genau — aber die Personen und Genres oben.")
                .font(.title3).foregroundStyle(Theme.label2)
        } else {
            nichtsGefunden
        }
    }

    /// Nichts gefunden: gleich wünschen. Der Besitzer sieht den Titel und
    /// wer ihn gewünscht hat — sonst nichts.
    private var nichtsGefunden: some View {
        VStack(alignment: .leading, spacing: 18) {
            Text("Nichts gefunden für „\(sauber)“.").font(.title2)
            Text("Du kannst dir den Titel wünschen — der Besitzer sieht den Wunsch und sonst nichts.")
                .font(.title3).foregroundStyle(Theme.label2)
            if gewuenscht == sauber {
                Label("Wunsch an den Besitzer geschickt", systemImage: "checkmark.circle.fill")
                    .font(.title3.weight(.medium)).foregroundStyle(Theme.good)
            } else {
                Button("„\(sauber)“ wünschen") {
                    let titel = sauber
                    Task { await wuenschen(titel) }
                }
            }
            if let wunschFehler {
                Text(wunschFehler).foregroundStyle(Theme.bad)
            }
        }
        .frame(maxWidth: 1200, alignment: .leading)
    }

    private func suche(_ text: String) async {
        error = nil
        do {
            let neu = try await session.client.search(text)
            if Task.isCancelled { return }
            ergebnis = neu
            geladenFuer = text
        } catch {
            if Task.isCancelled { return }
            self.error = Session.text(error)
        }
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
}
