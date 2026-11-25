// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

import SwiftUI
import LumenKit

// Titelvorschläge beim Wünschen auf dem Fernseher — dieselben wie auf
// iPhone, iPad, Mac und im Browser (GET /wishes/vorschlaege, von TMDb).
//
// Auf dem Fernseher verdeckt die Bildschirmtastatur die Seite, solange
// getippt wird. Darum stehen die Vorschläge nicht in einer Liste unter dem
// Feld, sondern als Reihe von Postern darunter: Nach „Fertig" geht der
// Fokus auf den ersten, mit links und rechts durch die Titel, OK wünscht
// genau diesen. Was schon da ist, öffnet mit OK die Titelseite; was schon
// gewünscht ist, bleibt erreichbar, tut aber nichts.

enum TVWunschVorschlaege {
    /// Die Vorschläge zur Eingabe (ab zwei Zeichen), höchstens zehn. nil,
    /// wenn es keine geben kann: der Server hat keinen TMDb-Schlüssel oder
    /// kennt den Weg noch nicht — dann wünscht „Fertig" gleich den Text.
    static func laden(_ session: Session, _ text: String) async -> [WunschVorschlag]? {
        guard text.count >= 2 else { return [] }
        do {
            let antwort = try await session.client.wunschVorschlaege(text)
            if antwort.ohneSchluessel == true { return nil }
            return Array(antwort.vorschlaege.prefix(10))
        } catch {
            return LumenError.fehltAufServer(error) ? nil : []
        }
    }
}

/// Eine Reihe Vorschläge mit Überschrift.
struct TVWunschVorschlagReihe: View {
    let vorschlaege: [WunschVorschlag]
    /// Wohin der Fokus gehen soll, wenn die Reihe nach der Eingabe kommt.
    var fokus: FocusState<String?>.Binding
    let waehlen: (WunschVorschlag) -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text("Vorschläge").font(.title3.weight(.semibold))
            ScrollView(.horizontal, showsIndicators: false) {
                LazyHStack(alignment: .top, spacing: 40) {
                    ForEach(vorschlaege) { v in
                        TVWunschVorschlagKarte(vorschlag: v, waehlen: waehlen)
                            .focused(fokus, equals: v.id)
                    }
                }
                .padding(.vertical, 30)
                .padding(.horizontal, 10)
            }
        }
    }
}

/// Ein Vorschlag: Poster, Titel, „2021 · Film" — und oben am Poster, ob er
/// schon da oder schon gewünscht ist.
struct TVWunschVorschlagKarte: View {
    @Environment(Session.self) private var session
    let vorschlag: WunschVorschlag
    let waehlen: (WunschVorschlag) -> Void
    @State private var url: URL?

    var body: some View {
        Group {
            if vorschlag.schonDa, let itemId = vorschlag.itemId, !itemId.isEmpty {
                NavigationLink { TVDetailView(itemId: itemId) } label: { karte }
            } else {
                Button {
                    if !vorschlag.schonDa && !vorschlag.schonGewuenscht { waehlen(vorschlag) }
                } label: { karte }
            }
        }
        .buttonStyle(.card)
        .task(id: vorschlag.id) { url = await session.client.wunschPosterURL(vorschlag) }
    }

    private var karte: some View {
        VStack(alignment: .leading, spacing: 8) {
            Artwork(url: url, title: vorschlag.titel, corner: 10)
                .frame(width: 200, height: 300)
                .overlay(alignment: .topTrailing) { abzeichen.padding(10) }
            Text(vorschlag.titel).font(.callout.weight(.medium)).lineLimit(2)
                .frame(width: 200, alignment: .leading)
            Text(vorschlag.zeile).font(.caption).foregroundStyle(Theme.label2).lineLimit(1)
                .frame(width: 200, alignment: .leading)
        }
        .padding(.bottom, 8)
    }

    @ViewBuilder
    private var abzeichen: some View {
        if vorschlag.schonDa {
            Text("Schon da")
                .font(.caption.weight(.semibold)).foregroundStyle(.white)
                .padding(.horizontal, 12).padding(.vertical, 5)
                .background(Capsule().fill(Theme.good))
        } else if vorschlag.schonGewuenscht {
            Text("Schon gewünscht")
                .font(.caption.weight(.semibold)).foregroundStyle(.white)
                .padding(.horizontal, 12).padding(.vertical, 5)
                .background(Capsule().fill(.black.opacity(0.65)))
        } else {
            Image(systemName: "plus.circle.fill")
                .font(.system(size: 34)).symbolRenderingMode(.palette)
                .foregroundStyle(.white, Theme.accent)
        }
    }
}

/// „Steht schon in der Bibliothek" — nach einem Wunsch, den der Server mit
/// 409 „schon_da" beantwortet hat. „Ansehen" öffnet den Titel.
struct TVSchonDaHinweis: View {
    let itemId: String
    let meldung: String

    var body: some View {
        HStack(spacing: 30) {
            Label(meldung, systemImage: "checkmark.circle.fill")
                .font(.title3.weight(.medium)).foregroundStyle(Theme.good)
            NavigationLink { TVDetailView(itemId: itemId) } label: {
                Text("Ansehen")
            }
        }
    }
}
