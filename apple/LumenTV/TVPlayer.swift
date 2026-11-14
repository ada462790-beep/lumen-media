// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

import SwiftUI
import AVKit
import LumenKit

// Der Player auf dem Fernseher.
//
// Darunter steckt AVPlayerViewController, und zwar absichtlich der aus
// UIKit statt `VideoPlayer`: nur er hat die Leiste unten mit eigenen
// Menüs (`transportBarCustomMenuItems`) und die Knöpfe rechts unten
// (`contextualActions`), die jeder vom Apple TV kennt — „Vorspann
// überspringen" gehört genau dorthin.
//
// Untertitel: bei HLS trägt der Server sie als Spurgruppe ein, und der
// Player bietet sie in seinem eigenen Menü an — Sprechblase, dunkles Feld,
// Haken. Lumen merkt sich nur, was gewählt wurde. Läuft die Datei direkt,
// gibt es diese Gruppe nicht; dann hat Lumen ein eigenes Menü in derselben
// Optik und legt den Text selbst über das Bild. Der Ton steckt immer im
// Strom, darum hat er immer ein eigenes Menü: eine andere Spur öffnet den
// Titel an derselben Stelle neu.
//
// Mit `liste` spielt er eine ganze Liste am Stück, ab dem Eintrag `ab`;
// sonst den einen Titel, und bei einer Folge danach die nächste, wenn
// „Folgen automatisch weiter" an ist. Was als Nächstes kommt, entscheidet
// `Abspielfolge` in LumenKit — gleich wie auf dem iPhone.
struct TVPlayerView: View {
    @Environment(Session.self) private var session
    @Environment(\.dismiss) private var dismiss
    @Environment(\.scenePhase) private var scenePhase
    let item: Item
    var liste: [Item]? = nil
    var ab: Int = 0
    /// „Von vorn": der erste Titel beginnt am Anfang, nicht am Stand.
    var vonVorn = false
    /// Die gewählte Fassung; nil heisst die erste.
    var datei: String? = nil

    @State private var folge: Abspielfolge?
    @State private var hinweis: String?
    /// Staffeln und Folgen für das Menü „Folgen" in der Leiste.
    @State private var folgenwahl: Folgenwahl?

    private var playback: Playback? { folge?.playback }

    var body: some View {
        ZStack {
            Color.black.ignoresSafeArea()
            if let playback, let player = playback.player {
                TVPlayerController(player: player,
                                   menue: menue(playback),
                                   aktionen: aktionen(playback))
                    .ignoresSafeArea()
                einblendungen(playback)
            } else if folge?.naechster == nil {
                ProgressView()
            }
            if let error = folge?.hinweis ?? folge?.playback.error {
                VStack(spacing: 20) {
                    Text("Das lässt sich nicht abspielen").font(.title2)
                    Text(error).foregroundStyle(Theme.label2).frame(maxWidth: 700)
                    HStack(spacing: 30) {
                        if folge?.kannUeberspringen == true, folge?.hinweis == nil {
                            Button("Überspringen") {
                                Task { await folge?.ueberspringen() }
                            }
                        }
                        Button("Zurück") { close() }
                    }
                }
            }
            if let folge, folge.naechster != nil, !folge.imAbspann {
                upNext(folge)
            }
        }
        .overlay(alignment: .top) {
            if let hinweis {
                Text(hinweis)
                    .font(.title3.weight(.medium))
                    .padding(.horizontal, 24).padding(.vertical, 12)
                    .background(Capsule().fill(.black.opacity(0.65)))
                    .padding(.top, 40)
                    .transition(.opacity)
            }
        }
        .task {
            let coordinator: Abspielfolge
            let prefs = session.me?.prefs ?? [:]
            if let liste {
                coordinator = Abspielfolge(client: session.client, liste: liste, ab: ab, prefs: prefs)
            } else {
                coordinator = Abspielfolge(client: session.client, item: item,
                                           weiter: AmStueck.weiterAn(prefs), prefs: prefs,
                                           vonVorn: vonVorn, datei: datei)
            }
            // Bei HLS zeigt AVPlayer die Untertitel — sein Menü ist hier
            // das, was alle kennen.
            coordinator.playback.nativeUntertitel = true
            folge = coordinator
            await coordinator.beginne()
        }
        .onChange(of: folge?.fertig ?? false) { _, done in
            if done { close() }
        }
        // Ein Film in Teilen: bei jedem Teil kurz „Teil 2 von 3" oben im Bild.
        .onChange(of: folge?.playback.file?.id) { _, _ in
            if let teil = folge?.playback.teilBeschriftung { Task { await melde(teil) } }
        }
        // Bei jeder neuen Folge die Staffeln fürs Menü „Folgen" auffrischen —
        // die Haken der eben gesehenen sollen stimmen.
        .task(id: folge?.playback.item?.id) {
            guard let folge, let jetzt = folge.playback.item, jetzt.kind == "episode" else { return }
            let wahl = folgenwahl ?? Folgenwahl(client: session.client)
            folgenwahl = wahl
            await wahl.allesLaden(fuer: jetzt, serieId: folge.playback.seriesId)
        }
        .onDisappear { Task { await folge?.stop() } }
        // Die TV-Taste oder der Bildschirmschoner: Stand jetzt sichern.
        .onChange(of: scenePhase) { _, phase in
            if phase != .active, let playback = folge?.playback { Task { await playback.standSichern() } }
        }
        // Die Menü-Taste der Fernbedienung beendet die Wiedergabe.
        .onExitCommand { close() }
        // Die Verwaltungssperre: antwortet der Server beim Markieren des
        // Vorspanns mit „gesperrt", erscheint der Entsperr-Dialog hier,
        // über dem Player — der unter der Startseite käme nicht nach oben.
        .sheet(isPresented: Binding(
            get: { session.adminSperre != nil },
            set: { if !$0 { session.adminEntsperrenAbbrechen() } })) {
            AdminUnlockView().environment(session)
        }
    }

    // MARK: - Menüs in der Leiste

    /// Die Menüs rechts in der Leiste: Ton, bei direkter Wiedergabe die
    /// Untertitel, und für die Verwaltung der Vorspann. Jedes öffnet das
    /// dunkle Feld mit Überschrift und Haken — das baut tvOS selbst.
    private func menue(_ playback: Playback) -> [UIMenuElement] {
        var elemente: [UIMenuElement] = []

        // Bei Folgen zuerst „Folgen": je Staffel ein Untermenü, die
        // laufende Folge mit Haken.
        if playback.item?.kind == "episode", let wahl = folgenwahl, !wahl.staffeln.isEmpty {
            elemente.append(folgenMenue(wahl, laufend: playback.item?.id))
        }

        if playback.tonspuren.count > 1 {
            let aktionen = playback.tonspuren.map { spur in
                UIAction(title: spur.name,
                         state: playback.tonspurAktiv == spur.index ? .on : .off) { _ in
                    Task { await playback.waehleTon(spur.index) }
                }
            }
            elemente.append(UIMenu(title: String(localized: "Tonspur", bundle: .lumen, locale: .lumen), image: UIImage(systemName: "waveform"),
                                   children: aktionen))
        }

        if playback.zeigtUntertitelSelbst, !playback.textspuren.isEmpty {
            var aktionen = [UIAction(title: String(localized: "Aus", bundle: .lumen, locale: .lumen), state: playback.untertitelSpur == nil ? .on : .off) { _ in
                Task { @MainActor in playback.waehleUntertitel(nil) }
            }]
            aktionen += playback.textspuren.map { spur in
                UIAction(title: spur.name,
                         state: playback.untertitelSpur == spur.index ? .on : .off) { _ in
                    Task { @MainActor in playback.waehleUntertitel(spur.index) }
                }
            }
            elemente.append(UIMenu(title: String(localized: "Untertitel", bundle: .lumen, locale: .lumen), image: UIImage(systemName: "captions.bubble"),
                                   children: aktionen))
        }

        // „Qualität" wie im Browser: Fassungen und Stufen fürs Umwandeln.
        elemente.append(qualitaetMenue(playback))

        // Den Vorspann markieren gehört zur Verwaltung — die gibt es nur im
        // Browser und in der Mac-App; auf dem Fernseher ist das immer falsch.
        if session.verwaltungHier, playback.kannVorspannMarkieren {
            elemente.append(vorspannMenue(playback))
        }
        return elemente
    }

    /// „Qualität" (Zahnrad): oben die Fassungen des Titels, darunter die
    /// Stufen fürs Umwandeln — Original, 1080p · 8 Mbit/s … und Auto —,
    /// für die Verwaltung „Diese Fassung als Standard nehmen". Wie im
    /// Browser (qualitaetPanel). Eine andere Fassung läuft an derselben
    /// Stelle weiter, mit derselben Ton- und Untertitelsprache.
    private func qualitaetMenue(_ playback: Playback) -> UIMenu {
        var kinder: [UIMenuElement] = []
        let fassungen = playback.fassungen
        let standard = playback.standardId ?? fassungen.first?.id
        if fassungen.count > 1 {
            let namen = Fassungen.namen(fassungen)
            let standardText = String(localized: "Standard", bundle: .lumen, locale: .lumen)
            var aktionen: [UIMenuElement] = []
            for (i, f) in fassungen.enumerated() {
                let id = f.id
                let titel = id == standard ? namen[i] + " · " + standardText : namen[i]
                aktionen.append(UIAction(title: titel, state: playback.fassungId == id ? .on : .off) { _ in
                    Task { await playback.waehleFassung(id) }
                })
            }
            kinder.append(UIMenu(title: String(localized: "Fassung", bundle: .lumen, locale: .lumen),
                                 options: .displayInline, children: aktionen))
        }
        let quelle = playback.file?.height
        let jetzt = playback.stufeWirksam
        var stufen: [UIMenuElement] = []
        let herkunft = Fassungen.aufloesung(quelle).map { " · " + $0 } ?? ""
        stufen.append(UIAction(title: Qualitaet.name(Qualitaet.original) + herkunft,
                               state: jetzt == Qualitaet.original ? .on : .off) { _ in
            Task { await playback.waehleQualitaet(Qualitaet.original) }
        })
        for st in Qualitaet.stufen(fuer: quelle) {
            let id = st.id
            stufen.append(UIAction(title: Qualitaet.name(id), state: jetzt == id ? .on : .off) { _ in
                Task { await playback.waehleQualitaet(id) }
            })
        }
        stufen.append(UIAction(title: Qualitaet.name(Qualitaet.auto),
                               state: jetzt == Qualitaet.auto ? .on : .off) { _ in
            Task { await playback.waehleQualitaet(Qualitaet.auto) }
        })
        kinder.append(UIMenu(title: String(localized: "Qualität", bundle: .lumen, locale: .lumen),
                             options: .displayInline, children: stufen))
        if session.verwaltungHier, fassungen.count > 1, playback.fassungId != standard {
            kinder.append(UIMenu(title: String(localized: "Für alle", bundle: .lumen, locale: .lumen),
                                 options: .displayInline, children: [
                UIAction(title: String(localized: "Diese Fassung als Standard nehmen", bundle: .lumen, locale: .lumen)) { _ in
                    Task {
                        await verwaltet {
                            try await playback.fassungAlsStandard()
                            return String(localized: "Gilt jetzt für alle", bundle: .lumen, locale: .lumen)
                        }
                    }
                },
            ]))
        }
        return UIMenu(title: String(localized: "Qualität", bundle: .lumen, locale: .lumen),
                      image: UIImage(systemName: "gearshape"), children: kinder)
    }

    /// „Folgen": je Staffel ein Menü mit ihren Folgen. Gesehene tragen
    /// den Kreis mit Haken, die laufende den Haken des Menüs; daneben die
    /// Länge oder „läuft". Eine andere Folge startet sofort.
    private func folgenMenue(_ wahl: Folgenwahl, laufend: String?) -> UIMenu {
        let folge = self.folge
        let staffeln: [UIMenuElement] = wahl.staffeln.map { st in
            let folgen = wahl.jeStaffel[st.id] ?? []
            let aktionen: [UIMenuElement] = folgen.map { f in
                let aktuell = f.id == laufend
                var zusatz = aktuell ? String(localized: "läuft", bundle: .lumen, locale: .lumen) : (f.runtimeMs.map { Item.duration(ms: $0) } ?? "")
                if !aktuell, !f.isSeen, f.percent > 1 { zusatz += (zusatz.isEmpty ? "" : " · ") + "\(Int(f.percent)) %" }
                return UIAction(title: f.folgenListenName,
                                subtitle: zusatz.isEmpty ? nil : zusatz,
                                image: f.isSeen ? UIImage(systemName: "checkmark.circle") : nil,
                                state: aktuell ? .on : .off) { _ in
                    guard !aktuell else { return }
                    Task { await folge?.springe(zu: f) }
                }
            }
            let enthaeltLaufende = folgen.contains { $0.id == laufend }
            return UIMenu(title: Folgenwahl.name(st),
                          subtitle: enthaeltLaufende ? String(localized: "läuft", bundle: .lumen, locale: .lumen) : nil,
                          children: aktionen.isEmpty
                            ? [UIAction(title: String(localized: "Keine Folgen", bundle: .lumen, locale: .lumen), attributes: .disabled) { _ in } as UIMenuElement]
                            : aktionen)
        }
        return UIMenu(title: String(localized: "Folgen", bundle: .lumen, locale: .lumen), image: UIImage(systemName: "list.bullet"), children: staffeln)
    }

    /// Der Vorspann einer Serie, für alle: Beginn merken, Ende setzen —
    /// oder die Markierung entfernen. Der Stand steht als Zeile ohne
    /// Wirkung darüber.
    private func vorspannMenue(_ playback: Playback) -> UIMenu {
        var kinder: [UIMenuElement] = []
        if let intro = playback.intro {
            let text = intro.vonSerie
                ? String(localized: "markiert: \(Playback.clock(intro.von))–\(Playback.clock(intro.bis))", bundle: .lumen, locale: .lumen)
                : String(localized: "aus den Kapiteln: \(Playback.clock(intro.von))–\(Playback.clock(intro.bis))", bundle: .lumen, locale: .lumen)
            let stand = UIAction(title: text, attributes: .disabled) { _ in }
            kinder.append(stand)
        }
        if let marke = playback.introMarke {
            kinder.append(UIAction(title: String(localized: "Beginn bei \(Playback.clock(marke)) gemerkt", bundle: .lumen, locale: .lumen), attributes: .disabled) { _ in })
            kinder.append(UIAction(title: String(localized: "… und endet hier", bundle: .lumen, locale: .lumen)) { _ in
                Task {
                    await verwaltet {
                        try await playback.vorspannEndetHier()
                        return String(localized: "Vorspann für die Serie markiert.", bundle: .lumen, locale: .lumen)
                    }
                }
            })
            kinder.append(UIAction(title: String(localized: "Abbrechen", bundle: .lumen, locale: .lumen)) { _ in
                Task { @MainActor in playback.vorspannMarkeVerwerfen() }
            })
        } else {
            kinder.append(UIAction(title: String(localized: "Vorspann beginnt hier", bundle: .lumen, locale: .lumen)) { _ in
                Task { @MainActor in playback.vorspannBeginntHier() }
            })
        }
        if playback.intro?.vonSerie == true {
            kinder.append(UIAction(title: String(localized: "Markierung entfernen", bundle: .lumen, locale: .lumen), attributes: .destructive) { _ in
                Task {
                    await verwaltet {
                        try await playback.vorspannEntfernen()
                        return String(localized: "Markierung entfernt.", bundle: .lumen, locale: .lumen)
                    }
                }
            })
        }
        return UIMenu(title: String(localized: "Vorspann (für alle)", bundle: .lumen, locale: .lumen), image: UIImage(systemName: "scissors"), children: kinder)
    }

    /// Die Knöpfe rechts unten: „Vorspann überspringen", und im Abspann
    /// „Jetzt abspielen" und „Abbrechen" zur Ankündigung.
    private func aktionen(_ playback: Playback) -> [UIAction] {
        var out: [UIAction] = []
        if playback.imVorspann {
            out.append(UIAction(title: String(localized: "Vorspann überspringen", bundle: .lumen, locale: .lumen),
                                image: UIImage(systemName: "forward.fill")) { _ in
                Task { @MainActor in playback.vorspannUeberspringen() }
            })
        }
        if let folge, folge.imAbspann, folge.naechster != nil {
            out.append(UIAction(title: String(localized: "Jetzt abspielen", bundle: .lumen, locale: .lumen), image: UIImage(systemName: "play.fill")) { _ in
                Task { await folge.jetzt() }
            })
            out.append(UIAction(title: String(localized: "Abbrechen", bundle: .lumen, locale: .lumen)) { _ in
                Task { @MainActor in folge.abbrechen() }
            })
        } else if let folge, playback.item?.kind == "episode" {
            // „Nächste Folge" — sofort, ohne auf den Abspann zu warten.
            out.append(UIAction(title: String(localized: "Nächste Folge", bundle: .lumen, locale: .lumen), image: UIImage(systemName: "forward.end.fill")) { _ in
                Task {
                    if !(await folge.naechsteFolge()) { await melde(String(localized: "Das war die letzte Folge.", bundle: .lumen, locale: .lumen)) }
                }
            })
        }
        return out
    }

    // MARK: - Über dem Bild

    /// Untertitel bei direkter Wiedergabe und die Ankündigung im Abspann.
    /// Beides zeigt nur an — bedient wird über die Knöpfe des Players.
    @ViewBuilder
    private func einblendungen(_ playback: Playback) -> some View {
        if let text = playback.untertitelText {
            let stil = UntertitelStil(prefs: session.me?.prefs)
            VStack {
                Spacer()
                Text(text)
                    .font(.system(size: 38 * stil.groesse.faktor, weight: .medium))
                    .multilineTextAlignment(.center)
                    .foregroundStyle(.white)
                    .shadow(color: .black, radius: 2)
                    .shadow(color: .black.opacity(0.9), radius: 6, y: 2)
                    .padding(.horizontal, 20).padding(.vertical, 10)
                    .background(RoundedRectangle(cornerRadius: 10, style: .continuous)
                        .fill(.black.opacity(stil.hintergrund ? 0.55 : 0)))
                    .frame(maxWidth: 1400)
                    .padding(.bottom, 150)
            }
            .allowsHitTesting(false)
        }
        if let folge, folge.imAbspann, let naechster = folge.naechster {
            VStack {
                HStack {
                    Spacer()
                    TVUpNextCard(item: naechster, titel: folge.naechsterTitel, sekunden: folge.sekunden)
                        .padding(60)
                }
                Spacer()
            }
            .allowsHitTesting(false)
            .transition(.opacity)
        }
    }

    /// „Als Nächstes" bildschirmfüllend, wenn der Titel schon zu Ende ist:
    /// auf drei Meter Abstand muss man lesen können, was kommt. Der Fokus
    /// springt von selbst auf die Knöpfe — sonst gibt es nichts.
    private func upNext(_ folge: Abspielfolge) -> some View {
        ZStack(alignment: .bottomLeading) {
            TVBackdrop(item: folge.naechster)
            VStack(alignment: .leading, spacing: 24) {
                Text("Als Nächstes: \(folge.naechsterTitel)")
                    .font(.system(size: 48, weight: .bold))
                    .lineLimit(2)
                Text("startet in \(folge.sekunden) s")
                    .monospacedDigit()
                    .font(.title3)
                    .foregroundStyle(Theme.label2)
                HStack(spacing: 30) {
                    Button {
                        Task { await folge.jetzt() }
                    } label: {
                        Label("Jetzt abspielen", systemImage: "play.fill")
                    }
                    Button("Abbrechen") { folge.abbrechen() }
                }
                .padding(.top, 10)
            }
            .frame(maxWidth: 1100, alignment: .leading)
            .padding(80)
        }
    }

    // MARK: - Ablauf

    /// Eine Verwaltungs-Aktion aus dem Menü: über die Sperre der Sitzung,
    /// mit Rückmeldung oben im Bild.
    private func verwaltet(_ arbeit: @escaping () async throws -> String) async {
        do {
            let meldung = try await session.verwaltet { try await arbeit() }
            await melde(meldung)
        } catch {
            await melde(Session.text(error))
        }
    }

    private func melde(_ text: String) async {
        withAnimation { hinweis = text }
        try? await Task.sleep(for: .seconds(3))
        if hinweis == text { withAnimation { hinweis = nil } }
    }

    private func close() {
        Task {
            await folge?.stop()
            // Sprachen und Markierungen können sich geändert haben.
            await session.refreshMe()
            dismiss()
        }
    }
}

// MARK: - AVPlayerViewController

/// Der Player aus UIKit, damit die Leiste unten eigene Menüs bekommt und
/// rechts unten Knöpfe erscheinen können. Beides wird bei jeder Änderung
/// neu gesetzt — der Player zeichnet nur, was sich geändert hat.
struct TVPlayerController: UIViewControllerRepresentable {
    let player: AVPlayer
    let menue: [UIMenuElement]
    let aktionen: [UIAction]

    func makeUIViewController(context: Context) -> AVPlayerViewController {
        let controller = AVPlayerViewController()
        controller.player = player
        controller.transportBarCustomMenuItems = menue
        controller.contextualActions = aktionen
        return controller
    }

    func updateUIViewController(_ controller: AVPlayerViewController, context: Context) {
        if controller.player !== player { controller.player = player }
        controller.transportBarCustomMenuItems = menue
        controller.contextualActions = aktionen
    }
}

/// Die Karte im Abspann: Standbild aus der nächsten Folge, Titel und wie
/// lange noch. Bedient wird sie über die Knöpfe rechts unten.
struct TVUpNextCard: View {
    @Environment(Session.self) private var session
    let item: Item
    let titel: String
    let sekunden: Int
    @State private var bild: URL?

    var body: some View {
        HStack(spacing: 20) {
            Artwork(url: bild, title: item.title, corner: 12)
                .frame(width: 240, height: 135)
            VStack(alignment: .leading, spacing: 8) {
                Text("Als Nächstes · in \(sekunden) s")
                    .monospacedDigit()
                    .font(.callout.weight(.medium))
                    .foregroundStyle(Theme.label2)
                Text(titel)
                    .font(.title3.weight(.semibold))
                    .lineLimit(2)
            }
        }
        .padding(22)
        .frame(maxWidth: 760, alignment: .leading)
        .background(
            RoundedRectangle(cornerRadius: 20, style: .continuous)
                .fill(Color(white: 0.11).opacity(0.92))
        )
        .task(id: item.id) {
            bild = await session.client.artworkURL(itemId: item.id, kind: "frame")
        }
    }
}
