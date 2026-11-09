// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

import SwiftUI
import LumenKit

// Neuigkeiten und Wünsche auf dem Fernseher.
//
// Beides gehört dem angemeldeten Profil: die Neuigkeiten kommen aus dem
// eigenen Stand, und ein Wunsch ist das, was jemand ausdrücklich äussert.
// Was jemand sonst schaut, steht hier nicht — und nirgends sonst.

// MARK: - Neuigkeiten

/// Neue Folgen deiner Serien, neue Filme deiner Reihen, neu
/// Dazugekommenes. Öffnen heisst gelesen, wie im Browser: erst wird die
/// Liste geholt (damit das Neue noch als neu markiert ist), dann gilt
/// alles bis jetzt als gesehen, und der Zähler in der Leiste verschwindet.
struct TVNeuigkeitenView: View {
    @Environment(Session.self) private var session
    @State private var daten: Neuigkeiten?
    @State private var error: String?

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 30) {
                TVSeitenkopf(titel: String(localized: "Neuigkeiten", bundle: .lumen, locale: .lumen),
                             zeile: String(localized: "Neue Folgen deiner Serien, neue Filme deiner Reihen, neu Dazugekommenes — nur für dich.", bundle: .lumen, locale: .lumen))
                    .padding(.top, 40)

                if let error {
                    TVFehler(text: error) { await load() }
                } else if let daten {
                    if daten.eintraege.isEmpty {
                        Text("Nichts Neues — Lumen meldet sich, sobald eine Serie, die du schaust, neue Folgen bekommt.")
                            .font(.title3).foregroundStyle(Theme.label2)
                            .frame(maxWidth: 1200, alignment: .leading)
                    } else {
                        VStack(alignment: .leading, spacing: 16) {
                            ForEach(Array(daten.eintraege.enumerated()), id: \.offset) { _, n in
                                zeile(n, neu: n.istNeu(gelesenBis: daten.gelesenBis))
                            }
                        }
                        .frame(maxWidth: 1300, alignment: .leading)
                    }
                } else {
                    ProgressView().frame(maxWidth: .infinity).padding(.top, 80)
                }
            }
            .padding(.horizontal, 60)
            .padding(.bottom, 60)
        }
        .onAppear { Task { await load() } }
    }

    @ViewBuilder
    private func zeile(_ n: Neuigkeit, neu: Bool) -> some View {
        let inhalt = HStack(spacing: 24) {
            Image(systemName: n.symbol)
                .font(.system(size: 28, weight: .medium))
                .foregroundStyle(neu ? Theme.accent : Theme.label2)
                .frame(width: 64, height: 64)
                .background(RoundedRectangle(cornerRadius: 16, style: .continuous)
                    .fill(neu ? Theme.accent.opacity(0.18) : Theme.fill))
            VStack(alignment: .leading, spacing: 4) {
                Text(n.titel).font(.body.weight(neu ? .semibold : .regular)).lineLimit(1)
                Text("\(n.text) · \(n.at.formatted(.dateTime.day().month(.abbreviated).locale(.lumen)))")
                    .font(.callout).foregroundStyle(Theme.label2).lineLimit(1)
            }
            Spacer(minLength: 12)
            if neu {
                Circle().fill(Theme.bad).frame(width: 14, height: 14)
                    .accessibilityLabel("neu")
            }
        }
        .padding(.horizontal, 24)
        .padding(.vertical, 14)
        .frame(maxWidth: .infinity, alignment: .leading)

        if let itemId = n.itemId, !itemId.isEmpty {
            NavigationLink { TVDetailView(itemId: itemId) } label: { inhalt }
                .buttonStyle(.card)
        } else {
            // Ohne Ziel trotzdem erreichbar, damit der Fokus nicht springt.
            Button {} label: { inhalt }
                .buttonStyle(.card)
        }
    }

    private func load() async {
        error = nil
        do {
            let neu = try await session.client.news()
            daten = neu
            if neu.ungelesen > 0 { await session.neuigkeitenGelesen() }
        } catch {
            self.error = Session.text(error)
        }
    }
}

// MARK: - Wünsche

/// Einen Titel wünschen und sehen, wie es um die eigenen Wünsche steht.
/// Angenommen und abgelehnt wird im Browser oder auf dem Mac.
struct TVWuenscheView: View {
    @Environment(Session.self) private var session
    @State private var wuensche: [Wish] = []
    @State private var titel = ""
    @State private var geladen = false
    @State private var meldung: String?
    @State private var fehler: String?

    private var sauber: String { titel.trimmingCharacters(in: .whitespacesAndNewlines) }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 30) {
                TVSeitenkopf(titel: String(localized: "Wünsche", bundle: .lumen, locale: .lumen),
                             zeile: session.isAdmin ? String(localized: "Was sich der Haushalt wünscht", bundle: .lumen, locale: .lumen)
                                                    : String(localized: "Was du dir gewünscht hast, und wie es steht", bundle: .lumen, locale: .lumen))
                    .padding(.top, 40)

                HStack(spacing: 30) {
                    TextField("Titel wünschen …", text: $titel)
                        .frame(width: 900)
                        .onSubmit { Task { await senden() } }
                    Button("Wünschen") { Task { await senden() } }
                        .disabled(sauber.isEmpty)
                }
                Text("Der Besitzer sieht den Titel und wer ihn gewünscht hat — sonst nichts.")
                    .font(.callout).foregroundStyle(Theme.label2)
                if let meldung {
                    Label(meldung, systemImage: "checkmark.circle.fill").foregroundStyle(Theme.good)
                }
                if let fehler {
                    Text(fehler).foregroundStyle(Theme.bad)
                }

                if geladen && wuensche.isEmpty {
                    Text("Noch keine Wünsche.").font(.title3).foregroundStyle(Theme.label2)
                } else {
                    VStack(alignment: .leading, spacing: 16) {
                        ForEach(wuensche) { w in
                            // Ein Knopf ohne Wirkung, damit sich die Liste
                            // mit der Fernbedienung durchblättern lässt.
                            Button {} label: { zeile(w) }
                                .buttonStyle(.card)
                        }
                    }
                    .frame(maxWidth: 1300, alignment: .leading)
                }
            }
            .padding(.horizontal, 60)
            .padding(.bottom, 60)
        }
        .onAppear { Task { await load() } }
    }

    private func zeile(_ w: Wish) -> some View {
        HStack(spacing: 24) {
            VStack(alignment: .leading, spacing: 4) {
                Text(w.title + (w.year.map { " (\($0))" } ?? "")).font(.body)
                Text("von \(w.by)" + (w.mine ? " (du)" : "")).font(.callout).foregroundStyle(Theme.label2)
            }
            Spacer(minLength: 12)
            Text(w.stateLabel)
                .font(.callout.weight(.semibold))
                .foregroundStyle(farbe(w.state))
                .padding(.horizontal, 16).padding(.vertical, 6)
                .background(Capsule().fill(farbe(w.state).opacity(0.16)))
        }
        .padding(.horizontal, 24)
        .padding(.vertical, 14)
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private func farbe(_ state: String) -> Color {
        switch state {
        case "working": Theme.accent
        case "done": Theme.good
        case "declined": Theme.bad
        default: Theme.label2
        }
    }

    private func load() async {
        wuensche = (try? await session.client.wishes()) ?? []
        geladen = true
    }

    private func senden() async {
        let t = sauber
        guard !t.isEmpty else { return }
        fehler = nil
        meldung = nil
        do {
            _ = try await session.client.wish(title: t)
            titel = ""
            meldung = String(localized: "Wunsch an den Besitzer geschickt", bundle: .lumen, locale: .lumen)
        } catch {
            fehler = Session.text(error)
        }
        await load()
    }
}
