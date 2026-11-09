// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

import SwiftUI
import AVKit
import LumenKit

/// Der Player.
///
/// Darunter steckt AVPlayer: Vollbild, AirPlay, Bild-in-Bild und die
/// Steuerung auf dem Sperrbildschirm kommen damit umsonst. Eine eigene
/// Steuerung nachzubauen hiesse, all das nachzubauen — darum übernimmt
/// `VideoPlayer` die Bedienung, und Lumen legt nur die Angaben darüber,
/// die das System nicht kennt:
///
/// - rechts unten, über der Zeitleiste, zwei Symbole wie im Player des
///   Apple TV: Sprechblase für Untertitel, Wellen für den Ton. Jedes öffnet
///   ein dunkles Feld am rechten Rand (`SpurenFeld`);
/// - „Vorspann überspringen", solange die Stelle im Vorspann liegt;
/// - „Als Nächstes" im Abspann und nach dem Ende;
/// - die Untertitel selbst, als Text unten im Bild — die direkte Datei hat
///   keine, und so sehen HLS und direkt gleich aus.
///
/// Mit `liste` spielt er eine ganze Liste am Stück; sonst den einen Titel,
/// und bei einer Folge danach die nächste, wenn „Folgen automatisch
/// weiter" an ist. Was als Nächstes kommt, entscheidet `Abspielfolge` in
/// LumenKit — hier steht nur, wie es aussieht.
struct PlayerView: View {
    @Environment(Session.self) private var session
    @Environment(\.dismiss) private var dismiss
    @Environment(\.scenePhase) private var scenePhase
    let item: Item
    var liste: [Item]? = nil
    var ab: Int = 0
    /// „Von vorn": der erste Titel beginnt am Anfang, nicht am Stand.
    var vonVorn = false
    /// Die gewählte Fassung (Issue #8); nil heisst die erste.
    var datei: String? = nil
    /// Ein heruntergeladener Titel: die Datei dieses Downloads, ohne Server.
    var offlineDatei: String? = nil
    /// Die Vorschaubilder fürs Spulen, sobald der Server welche hat.
    @State private var vorschau: Vorschaubilder?
    /// Schon geschlossen — ein zweites Schliessen (Knopf und Fenster
    /// zugleich) soll nichts doppelt tun.
    @State private var geschlossen = false

    @State private var folge: Abspielfolge?
    @State private var showInfo = true
    @State private var feld: SpurenFeld.Art?
    @State private var hinweis: String?
    /// Staffeln und Folgen fürs Folgen-Feld — geladen beim ersten Öffnen.
    @State private var folgenwahl: Folgenwahl?
    /// Ein Download, der fehlt — dann gibt es nichts abzuspielen.
    @State private var hinweisOffline: String?

    private var playback: Playback? { folge?.playback }

    /// Wie weit die eigenen Einblendungen über dem unteren Rand liegen —
    /// dort sitzt die Steuerung von AVKit.
    private var steuerungHoehe: CGFloat {
        #if os(macOS)
        86
        #else
        96
        #endif
    }

    var body: some View {
        ZStack {
            Color.black.ignoresSafeArea()

            if let playback, let player = playback.player {
                VideoPlayer(player: player)
                    .ignoresSafeArea()
            } else if folge?.naechster == nil, hinweisOffline == nil {
                VStack(spacing: 16) {
                    ProgressView().controlSize(.large)
                    Text(folge?.aktuell?.title ?? item.title)
                        .font(.headline).foregroundStyle(Theme.label2)
                }
            }

            if let error = hinweisOffline ?? folge?.hinweis ?? playback?.error {
                VStack(spacing: 16) {
                    Text("Das lässt sich nicht abspielen").font(.headline)
                    Text(error).font(.callout).foregroundStyle(Theme.label2)
                        .multilineTextAlignment(.center).frame(maxWidth: 420)
                    HStack(spacing: 10) {
                        if folge?.kannUeberspringen == true, folge?.hinweis == nil {
                            Button("Überspringen") {
                                Task { await folge?.ueberspringen() }
                            }
                            .buttonStyle(QuietButtonStyle())
                        }
                        Button("Zurück") { close() }.buttonStyle(QuietButtonStyle())
                    }
                }
                .padding(40)
                .background(.black.opacity(0.75))
            }

            if let playback, playback.laeuft {
                einblendungen(playback)
            }

            if let folge, folge.naechster != nil, !folge.imAbspann {
                UpNextCard(folge: folge, kompakt: false)
                    .padding(24)
                    .transition(.opacity)
            }
        }
        .overlay(alignment: .topLeading) { closeButton }
        .overlay(alignment: .topTrailing) { transcodeBadge }
        .overlay(alignment: .top) { hinweisZeile }
        // Ein Tipp aufs Bild holt die eigenen Einblendungen zurück —
        // samt Schliessen-Knopf, sonst käme man auf dem iPhone nicht mehr
        // aus dem Player.
        .simultaneousGesture(TapGesture().onEnded { einblenden() })
        // VoiceOver: das Wischen mit zwei Fingern (Z) schliesst, auch wenn
        // der Knopf gerade ausgeblendet ist.
        .accessibilityAction(.escape) { close() }
        .task { await begin() }
        .task(id: EinblendeStand(an: showInfo, bild: playback?.player != nil)) {
            // Nach ein paar Sekunden tritt die Einblendung wieder zurück.
            guard showInfo, playback?.player != nil else { return }
            try? await Task.sleep(for: .seconds(4))
            if Task.isCancelled { return }
            withAnimation { showInfo = false }
        }
        .task(id: playback?.trickplay) {
            if let info = playback?.trickplay, let fileId = playback?.file?.id {
                vorschau = Vorschaubilder(client: session.client, fileId: fileId, info: info)
            } else {
                vorschau = nil
            }
        }
        // In den Hintergrund: Stand jetzt sichern. Ob die App zurückkommt,
        // weiss niemand — und iOS lässt ihr dafür nur Sekunden.
        .onChange(of: scenePhase) { _, phase in
            if phase == .background { standImHintergrundSichern() }
        }
        .onChange(of: folge?.fertig ?? false) { _, done in
            if done { close() }
        }
        // Ein Film in Teilen: bei jedem Teil kurz „Teil 2 von 3" oben im Bild.
        .onChange(of: playback?.file?.id) { _, _ in
            if let teil = playback?.teilBeschriftung { Task { await melde(teil) } }
        }
        .onDisappear {
            Task { await folge?.stop() }
            #if os(macOS)
            NotificationCenter.default.post(name: .lumenPlayerZu, object: nil)
            #endif
        }
        #if os(macOS)
        // Für das Menü: Leertaste und Pfeile wirken auf diesen Player,
        // solange sein Fenster vorne ist.
        .focusedSceneValue(\.lumenWiedergabe, playback)
        .navigationTitle(folge?.aktuell?.title ?? item.title)
        #endif
        // Die Verwaltungssperre: antwortet der Server beim Markieren des
        // Vorspanns mit „gesperrt", erscheint der Entsperr-Dialog hier,
        // über dem Player — der im Gerüst darunter käme nicht nach oben.
        .sheet(isPresented: Binding(
            get: { session.adminSperre != nil },
            set: { if !$0 { session.adminEntsperrenAbbrechen() } })) {
            AdminUnlockView().environment(session)
        }
        #if os(iOS)
        .statusBarHidden()
        .persistentSystemOverlays(.hidden)
        #endif
        // Der Player ist immer dunkel — auch wenn die App hell eingestellt ist.
        .preferredColorScheme(.dark)
    }

    // MARK: - Über dem Bild

    /// Alles, was während der Wiedergabe über dem Bild liegt: Untertitel,
    /// die beiden Symbole, der Vorspann-Knopf, die Ankündigung im Abspann
    /// und das offene Feld.
    @ViewBuilder
    private func einblendungen(_ playback: Playback) -> some View {
        if let text = playback.untertitelText {
            VStack {
                Spacer()
                UntertitelText(text: text, stil: UntertitelStil(prefs: session.me?.prefs))
                    .padding(.bottom, steuerungHoehe)
                    .padding(.horizontal, 40)
            }
            .allowsHitTesting(false)
        }

        if showInfo, feld == nil, let vorschau {
            // Eigene Leiste mit Vorschaubildern, über der von AVKit.
            VStack {
                Spacer()
                VorschauLeiste(playback: playback, vorschau: vorschau)
                    .padding(.horizontal, 40)
                    .padding(.bottom, steuerungHoehe + 64)
            }
            .transition(.opacity)
        }

        VStack(alignment: .trailing, spacing: 12) {
            Spacer()
            if let folge, folge.imAbspann, folge.naechster != nil {
                UpNextCard(folge: folge, kompakt: true)
            }
            if playback.imVorspann {
                Button {
                    playback.vorspannUeberspringen()
                } label: {
                    Label("Vorspann überspringen", systemImage: "forward.fill")
                        .font(.subheadline.weight(.semibold))
                        .foregroundStyle(.black)
                        .padding(.horizontal, 16).padding(.vertical, 10)
                        .background(Capsule().fill(.white))
                }
                .buttonStyle(.plain)
                .transition(.opacity)
            }
            if feld == nil {
                spurSymbole(playback)
            }
        }
        .padding(.trailing, 20)
        .padding(.bottom, steuerungHoehe + 8)
        .frame(maxWidth: .infinity, alignment: .trailing)

        if let art = feld {
            // Ein Tipp neben das Feld schliesst es.
            Color.black.opacity(0.001)
                .ignoresSafeArea()
                .onTapGesture { withAnimation(.easeOut(duration: 0.15)) { feld = nil } }
            HStack {
                Spacer()
                if art == .folgen, let folgenwahl, let folge {
                    FolgenFeld(wahl: folgenwahl, laufend: playback.item?.id,
                               schliessen: { withAnimation(.easeOut(duration: 0.15)) { feld = nil } },
                               waehle: { ziel in Task { await folge.springe(zu: ziel) } })
                        .padding(.trailing, 20)
                        .padding(.vertical, 60)
                } else {
                    SpurenFeld(art: art, playback: playback, istAdmin: session.verwaltungHier,
                               schliessen: { withAnimation(.easeOut(duration: 0.15)) { feld = nil } },
                               verwaltet: { arbeit in await verwaltet(arbeit) })
                        .padding(.trailing, 20)
                        .padding(.vertical, 60)
                }
            }
            .transition(.move(edge: .trailing).combined(with: .opacity))
        }
    }

    /// Die beiden Symbole wie im Player des Apple TV: Sprechblase für
    /// Untertitel, Wellen für den Ton — und für die Verwaltung die Schere,
    /// mit der sich der Vorspann markieren lässt.
    private func spurSymbole(_ playback: Playback) -> some View {
        HStack(spacing: 10) {
            // Bei Folgen: „Nächste Folge" und das Folgen-Feld, wie im Browser.
            // Offline weiss nur der Server, welche Folge danach kommt.
            if playback.item?.kind == "episode", !playback.spieltOffline {
                Button {
                    Task { await naechsteFolge() }
                } label: {
                    Label("Nächste Folge", systemImage: "forward.end.fill")
                        .font(.subheadline.weight(.semibold))
                        .foregroundStyle(.white)
                        .padding(.horizontal, 14)
                        .frame(height: 40)
                        .background(Capsule().fill(.black.opacity(0.45)))
                }
                .buttonStyle(.plain)
                symbol("list.bullet", hilfe: String(localized: "Folgen", bundle: .lumen, locale: .lumen), aktiv: false) {
                    folgenOeffnen(playback)
                }
            }
            // Den Vorspann markieren gehört zur Verwaltung — nur am Mac.
            if session.verwaltungHier, playback.kannVorspannMarkieren {
                symbol("scissors", hilfe: String(localized: "Vorspann markieren", bundle: .lumen, locale: .lumen), aktiv: playback.introMarke != nil) {
                    oeffne(.vorspann)
                }
            }
            if !playback.textspuren.isEmpty {
                symbol("captions.bubble", hilfe: String(localized: "Untertitel", bundle: .lumen, locale: .lumen), aktiv: playback.untertitelSpur != nil) {
                    oeffne(.untertitel)
                }
            }
            if playback.tonspuren.count > 1 {
                symbol("waveform", hilfe: String(localized: "Ton", bundle: .lumen, locale: .lumen), aktiv: playback.tonspur != nil) {
                    oeffne(.ton)
                }
            }
        }
    }

    private func symbol(_ name: String, hilfe: String, aktiv: Bool, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            Image(systemName: name)
                .font(.callout.weight(.semibold))
                .foregroundStyle(.white)
                .frame(width: 40, height: 40)
                .background(Circle().fill(aktiv ? Theme.accent.opacity(0.75) : .black.opacity(0.45)))
        }
        .buttonStyle(.plain)
        .help(hilfe)
        .accessibilityLabel(hilfe)
    }

    /// Das Folgen-Feld öffnen und dabei die Staffel der laufenden Folge laden.
    private func folgenOeffnen(_ playback: Playback) {
        let wahl = folgenwahl ?? Folgenwahl(client: session.client)
        folgenwahl = wahl
        if let jetzt = playback.item {
            Task { await wahl.laden(fuer: jetzt, serieId: playback.seriesId) }
        }
        oeffne(.folgen)
    }

    private func naechsteFolge() async {
        guard let folge else { return }
        if !(await folge.naechsteFolge()) {
            await melde(String(localized: "Das war die letzte Folge.", bundle: .lumen, locale: .lumen))
        }
    }

    private func oeffne(_ art: SpurenFeld.Art) {
        withAnimation(.easeOut(duration: 0.18)) { feld = feld == art ? nil : art }
    }

    private var closeButton: some View {
        let sichtbar = showInfo || playback?.player == nil
        return Button(action: close) {
            Image(systemName: "chevron.down")
                .font(.subheadline.weight(.semibold))
                .foregroundStyle(.white)
                .frame(width: 42, height: 42)
                .background(Circle().fill(.black.opacity(0.45)))
        }
        .buttonStyle(.plain)
        .padding(18)
        .accessibilityLabel("Player schliessen")
        // Ohne laufendes Bild gibt es keine Steuerung von AVKit, die
        // zurückführt — dann bleibt der Knopf sichtbar. Unsichtbar ist er
        // auch für VoiceOver nicht da (dort schliesst das Z-Wischen).
        .opacity(sichtbar ? 1 : 0)
        .allowsHitTesting(sichtbar)
        .accessibilityHidden(!sichtbar)
    }

    private func einblenden() {
        guard !showInfo else { return }
        withAnimation(.easeOut(duration: 0.15)) { showInfo = true }
    }

    @ViewBuilder
    private var transcodeBadge: some View {
        if playback?.isTranscoding == true, showInfo {
            Text("wird umgewandelt")
                .font(.caption.weight(.medium))
                .foregroundStyle(Theme.accent)
                .padding(.horizontal, 10).padding(.vertical, 5)
                .background(Capsule().fill(Theme.accent.opacity(0.16)))
                .padding(18)
                .transition(.opacity)
        }
    }

    /// Eine kurze Meldung oben — etwa „Vorspann markiert".
    @ViewBuilder
    private var hinweisZeile: some View {
        if let hinweis {
            Text(hinweis)
                .font(.callout.weight(.medium))
                .foregroundStyle(.white)
                .padding(.horizontal, 14).padding(.vertical, 8)
                .background(Capsule().fill(.black.opacity(0.6)))
                .padding(.top, 24)
                .transition(.opacity)
        }
    }

    // MARK: - Ablauf

    private func begin() async {
        let coordinator: Abspielfolge
        let prefs = session.me?.prefs ?? [:]
        #if !os(tvOS)
        if let offlineDatei {
            let downloads = Downloads.shared
            downloads.vorbereiten()
            guard let eintrag = downloads.katalog[offlineDatei], let datei = downloads.lokaleDatei(eintrag) else {
                hinweisOffline = String(localized: "Dieser Download ist nicht mehr auf dem Gerät.", bundle: .lumen, locale: .lumen)
                return
            }
            let ab = eintrag.stand > 0 ? eintrag.stand : eintrag.item.resumeSeconds
            coordinator = Abspielfolge(
                client: session.client, item: eintrag.item,
                offline: .init(datei: datei, ab: ab, dauer: eintrag.dauer, melder: downloads.offlineMelder),
                prefs: prefs, vonVorn: vonVorn)
            folge = coordinator
            await coordinator.beginne()
            return
        }
        #endif
        if let liste {
            coordinator = Abspielfolge(client: session.client, liste: liste, ab: ab, prefs: prefs)
        } else {
            coordinator = Abspielfolge(client: session.client, item: item,
                                       weiter: AmStueck.weiterAn(prefs), prefs: prefs,
                                       vonVorn: vonVorn, datei: datei)
        }
        folge = coordinator
        await coordinator.beginne()
    }

    /// Den Stand sichern, wenn die App in den Hintergrund geht. Auf dem
    /// iPhone mit einer Hintergrund-Aufgabe: ohne sie friert iOS die App
    /// womöglich ein, bevor die Anfrage beim Server ist.
    private func standImHintergrundSichern() {
        guard let playback else { return }
        #if os(iOS)
        let aufgabe = UIApplication.shared.beginBackgroundTask(withName: "Lumen: Stand sichern")
        Task {
            await playback.standSichern()
            if aufgabe != .invalid { UIApplication.shared.endBackgroundTask(aufgabe) }
        }
        #else
        Task { await playback.standSichern() }
        #endif
    }

    /// Eine Verwaltungs-Aktion aus dem Feld: über die Sperre der Sitzung,
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
        guard !geschlossen else { return }
        geschlossen = true
        Task {
            await folge?.stop()
            // Sprachen und Markierungen können sich geändert haben.
            await session.refreshMe()
            dismiss()
        }
    }
}

/// Wann die Einblendung zurücktritt: erst, wenn ein Bild läuft.
private struct EinblendeStand: Equatable {
    let an: Bool
    let bild: Bool
}

extension PlayerView {
    /// Aus einem Auftrag — so starten Vollbild (iPhone, iPad) und Fenster
    /// (Mac) denselben Player.
    init(auftrag: PlayerAuftrag) {
        self.init(item: auftrag.item, liste: auftrag.liste, ab: auftrag.ab,
                  vonVorn: auftrag.vonVorn, datei: auftrag.datei, offlineDatei: auftrag.offlineDatei)
    }
}

// MARK: - Untertitel im Bild

/// Der Untertitel-Text unten im Bild: weiss mit dunklem Rand, wie ihn
/// jeder Fernseher zeigt, damit er auf hellen Szenen lesbar bleibt.
/// Grösse und Hintergrund kommen aus den Prefs (`UntertitelStil`) —
/// dieselbe Einstellung wie im Browser.
struct UntertitelText: View {
    let text: String
    var stil = UntertitelStil()

    var body: some View {
        Text(text)
            .font(.system(size: schrift * stil.groesse.faktor, weight: .medium))
            .multilineTextAlignment(.center)
            .foregroundStyle(.white)
            .shadow(color: .black, radius: 1.5, x: 0, y: 0)
            .shadow(color: .black.opacity(0.9), radius: stil.hintergrund ? 4 : 6, x: 0, y: 1)
            .padding(.horizontal, 12).padding(.vertical, 6)
            .background(RoundedRectangle(cornerRadius: 6, style: .continuous)
                .fill(.black.opacity(stil.hintergrund ? 0.55 : 0)))
            .frame(maxWidth: 820)
            .accessibilityLabel("Untertitel: \(text)")
    }

    private var schrift: CGFloat {
        #if os(iOS)
        UIDevice.current.userInterfaceIdiom == .phone ? 17 : 24
        #else
        22
        #endif
    }
}

// MARK: - Das Feld für Ton, Untertitel und Vorspann

/// Das dunkle Feld am rechten Rand, wie es der Player des Apple TV zeigt:
/// eine Überschrift in Grossbuchstaben, darunter die Einträge, der Haken
/// rechts am gewählten, und der gewählte Eintrag als heller Balken mit
/// dunkler Schrift.
struct SpurenFeld: View {
    enum Art: Equatable { case ton, untertitel, vorspann, folgen }

    let art: Art
    let playback: Playback
    let istAdmin: Bool
    let schliessen: () -> Void
    /// Eine Verwaltungs-Aktion ausführen; sie gibt die Meldung zurück,
    /// die danach kurz im Bild steht.
    let verwaltet: (@escaping () async throws -> String) async -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            switch art {
            case .ton:
                ueberschrift(String(localized: "Tonspur", bundle: .lumen, locale: .lumen))
                ForEach(playback.tonspuren) { spur in
                    zeile(spur.name, gewaehlt: playback.tonspurAktiv == spur.index) {
                        schliessen()
                        Task { await playback.waehleTon(spur.index) }
                    }
                }
            case .untertitel:
                ueberschrift(String(localized: "Untertitel", bundle: .lumen, locale: .lumen))
                zeile(String(localized: "Aus", bundle: .lumen, locale: .lumen), gewaehlt: playback.untertitelSpur == nil) {
                    playback.waehleUntertitel(nil)
                    schliessen()
                }
                ForEach(playback.textspuren) { spur in
                    zeile(spur.name, gewaehlt: playback.untertitelSpur == spur.index) {
                        playback.waehleUntertitel(spur.index)
                        schliessen()
                    }
                }
            case .vorspann:
                vorspann
            case .folgen:
                // Steht in einem eigenen Feld (`FolgenFeld`).
                EmptyView()
            }
        }
        .padding(.vertical, 10)
        .frame(width: 300)
        .background(
            RoundedRectangle(cornerRadius: 18, style: .continuous)
                .fill(Color(white: 0.11).opacity(0.92))
        )
        .overlay(
            RoundedRectangle(cornerRadius: 18, style: .continuous)
                .strokeBorder(Color.white.opacity(0.08))
        )
        .shadow(color: .black.opacity(0.4), radius: 20, x: 0, y: 8)
    }

    /// Der Vorspann einer Serie, für alle: zwei Schritte — Beginn merken,
    /// Ende setzen —, und falls schon markiert, entfernen.
    @ViewBuilder
    private var vorspann: some View {
        ueberschrift(String(localized: "Vorspann (für alle)", bundle: .lumen, locale: .lumen))
        if let intro = playback.intro {
            hinweis(intro.vonSerie
                ? String(localized: "markiert: \(Playback.clock(intro.von))–\(Playback.clock(intro.bis))", bundle: .lumen, locale: .lumen)
                : String(localized: "aus den Kapiteln der Datei: \(Playback.clock(intro.von))–\(Playback.clock(intro.bis))", bundle: .lumen, locale: .lumen))
        }
        if let marke = playback.introMarke {
            hinweis(String(localized: "Beginn bei \(Playback.clock(marke)) gemerkt", bundle: .lumen, locale: .lumen))
            zeile(String(localized: "… und endet hier", bundle: .lumen, locale: .lumen), gewaehlt: false) {
                schliessen()
                Task {
                    await verwaltet {
                        try await playback.vorspannEndetHier()
                        return String(localized: "Vorspann für die Serie markiert.", bundle: .lumen, locale: .lumen)
                    }
                }
            }
            zeile(String(localized: "Abbrechen", bundle: .lumen, locale: .lumen), gewaehlt: false) {
                playback.vorspannMarkeVerwerfen()
                schliessen()
            }
        } else {
            zeile(String(localized: "Vorspann beginnt hier", bundle: .lumen, locale: .lumen), gewaehlt: false) {
                playback.vorspannBeginntHier()
                schliessen()
            }
        }
        if playback.intro?.vonSerie == true {
            zeile(String(localized: "Markierung entfernen", bundle: .lumen, locale: .lumen), gewaehlt: false) {
                schliessen()
                Task {
                    await verwaltet {
                        try await playback.vorspannEntfernen()
                        return String(localized: "Markierung entfernt.", bundle: .lumen, locale: .lumen)
                    }
                }
            }
        }
    }

    private func ueberschrift(_ text: String) -> some View {
        Text(text.uppercased())
            .font(.caption.weight(.semibold))
            .tracking(0.8)
            .foregroundStyle(Theme.label2)
            .padding(.horizontal, 22)
            .padding(.top, 8)
            .padding(.bottom, 10)
    }

    private func hinweis(_ text: String) -> some View {
        Text(text)
            .font(.footnote)
            .foregroundStyle(Theme.label2)
            .padding(.horizontal, 22)
            .padding(.bottom, 8)
    }

    /// Ein Eintrag: Name links, Haken rechts; gewählt als heller Balken.
    private func zeile(_ name: String, gewaehlt: Bool, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            HStack(spacing: 12) {
                Text(name)
                    .font(.subheadline.weight(gewaehlt ? .semibold : .regular))
                    .lineLimit(1)
                Spacer(minLength: 8)
                if gewaehlt {
                    Image(systemName: "checkmark")
                        .font(.footnote.weight(.bold))
                }
            }
            .foregroundStyle(gewaehlt ? Color.black : Color.white)
            .padding(.horizontal, 16)
            .padding(.vertical, 11)
            .background(
                RoundedRectangle(cornerRadius: 10, style: .continuous)
                    .fill(gewaehlt ? Color.white : Color.clear)
            )
            .padding(.horizontal, 8)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }
}

// MARK: - Das Feld für die Folgen

/// Die Folgen im Player, im Stil des Spurenfelds: oben die Staffeln, darunter
/// die Folgen mit Bild, Nummer, Haken und Balken. Die laufende ist hell
/// hinterlegt; ein Tipp auf eine andere startet sie sofort.
struct FolgenFeld: View {
    let wahl: Folgenwahl
    let laufend: String?
    let schliessen: () -> Void
    let waehle: (Item) -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            Text("FOLGEN")
                .font(.caption.weight(.semibold))
                .tracking(0.8)
                .foregroundStyle(Theme.label2)
                .padding(.horizontal, 22)
                .padding(.top, 8)
                .padding(.bottom, 10)

            if wahl.staffeln.count > 1 {
                ScrollView(.horizontal, showsIndicators: false) {
                    HStack(spacing: 6) {
                        ForEach(wahl.staffeln) { st in
                            let an = wahl.staffel == st.id
                            Button {
                                Task { await wahl.waehleStaffel(st.id) }
                            } label: {
                                Text(Folgenwahl.name(st))
                                    .font(.footnote.weight(an ? .semibold : .regular))
                                    .foregroundStyle(an ? Color.black : Color.white)
                                    .padding(.horizontal, 12).padding(.vertical, 6)
                                    .background(Capsule().fill(an ? Color.white : Color.white.opacity(0.12)))
                            }
                            .buttonStyle(.plain)
                        }
                    }
                    .padding(.horizontal, 16)
                }
                .padding(.bottom, 8)
            }

            if wahl.folgen.isEmpty {
                Group {
                    if wahl.laedt { ProgressView() } else {
                        Text("Keine Folgen").font(.footnote).foregroundStyle(Theme.label2)
                    }
                }
                .frame(maxWidth: .infinity)
                .padding(.vertical, 24)
            } else {
                ScrollViewReader { leser in
                    ScrollView {
                        VStack(spacing: 2) {
                            ForEach(wahl.folgen) { f in
                                FolgenZeile(folge: f, aktuell: f.id == laufend) {
                                    if f.id != laufend {
                                        schliessen()
                                        waehle(f)
                                    }
                                }
                                .id(f.id)
                            }
                        }
                        .padding(.horizontal, 8)
                    }
                    .onAppear { if let laufend { leser.scrollTo(laufend, anchor: .center) } }
                    .onChange(of: wahl.folgen.count) { _, _ in
                        if let laufend { leser.scrollTo(laufend, anchor: .center) }
                    }
                }
            }
        }
        .padding(.vertical, 10)
        .frame(width: 380)
        .frame(maxHeight: 560)
        .background(
            RoundedRectangle(cornerRadius: 18, style: .continuous)
                .fill(Color(white: 0.11).opacity(0.92))
        )
        .overlay(
            RoundedRectangle(cornerRadius: 18, style: .continuous)
                .strokeBorder(Color.white.opacity(0.08))
        )
        .shadow(color: .black.opacity(0.4), radius: 20, x: 0, y: 8)
    }
}

/// Eine Folge im Folgen-Feld.
struct FolgenZeile: View {
    @Environment(Session.self) private var session
    let folge: Item
    let aktuell: Bool
    let action: () -> Void
    @State private var bild: URL?

    var body: some View {
        Button(action: action) {
            HStack(spacing: 12) {
                Artwork(url: bild, title: folge.title, corner: 6)
                    .frame(width: 104, height: 58)
                    .overlay(alignment: .bottom) {
                        if folge.percent > 1 && !folge.isSeen { ProgressStripe(percent: folge.percent) }
                    }
                VStack(alignment: .leading, spacing: 2) {
                    HStack(spacing: 6) {
                        Text("\(folge.episodeNo ?? 0)").monospacedDigit().opacity(0.6)
                        Text(folge.title).lineLimit(1)
                        if folge.isSeen {
                            Image(systemName: "checkmark").font(.caption2.weight(.bold))
                        }
                    }
                    .font(.subheadline.weight(aktuell ? .semibold : .regular))
                    Text(aktuell ? String(localized: "läuft", bundle: .lumen, locale: .lumen) : (folge.runtimeMs.map { Item.duration(ms: $0) } ?? ""))
                        .font(.caption)
                        .opacity(0.6)
                }
                Spacer(minLength: 0)
            }
            .foregroundStyle(aktuell ? Color.black : Color.white)
            .padding(8)
            .background(
                RoundedRectangle(cornerRadius: 10, style: .continuous)
                    .fill(aktuell ? Color.white : Color.clear)
            )
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityLabel(String(localized: "Folge \(folge.episodeNo ?? 0): \(folge.title)", bundle: .lumen, locale: .lumen)
                            + (folge.isSeen ? ", " + String(localized: "gesehen", bundle: .lumen, locale: .lumen) : "")
                            + (aktuell ? ", " + String(localized: "läuft", bundle: .lumen, locale: .lumen) : ""))
        .task(id: folge.id) {
            bild = await session.client.artworkURL(for: folge, wide: true)
        }
    }
}

// MARK: - Als Nächstes

/// „Als Nächstes" — klein rechts unten, solange der Abspann läuft, und
/// gross mitten auf dem Schwarz, wenn der Titel schon zu Ende ist.
struct UpNextCard: View {
    @Environment(Session.self) private var session
    let folge: Abspielfolge
    let kompakt: Bool
    @State private var bild: URL?

    var body: some View {
        HStack(alignment: .center, spacing: 14) {
            if let naechster = folge.naechster {
                Artwork(url: bild, title: naechster.title, corner: 10)
                    .frame(width: kompakt ? 128 : 200, height: kompakt ? 72 : 112)
            }
            VStack(alignment: .leading, spacing: 6) {
                Text("Als Nächstes · in \(folge.sekunden) s")
                    .monospacedDigit()
                    .font(.caption.weight(.medium))
                    .foregroundStyle(Theme.label2)
                Text(folge.naechsterTitel)
                    .font(kompakt ? .subheadline.weight(.semibold) : .title3.weight(.semibold))
                    .lineLimit(2)
                HStack(spacing: 8) {
                    Button {
                        Task { await folge.jetzt() }
                    } label: {
                        Label("Jetzt", systemImage: "play.fill")
                            .font(.subheadline.weight(.semibold))
                            .foregroundStyle(.black)
                            .padding(.horizontal, 14).padding(.vertical, 8)
                            .background(Capsule().fill(.white))
                    }
                    .buttonStyle(.plain)
                    Button {
                        folge.abbrechen()
                    } label: {
                        Text("Abbrechen")
                            .font(.subheadline.weight(.medium))
                            .foregroundStyle(.white)
                            .padding(.horizontal, 14).padding(.vertical, 8)
                            .background(Capsule().fill(Color.white.opacity(0.14)))
                    }
                    .buttonStyle(.plain)
                }
                .padding(.top, 2)
            }
        }
        .padding(kompakt ? 14 : 22)
        .frame(maxWidth: kompakt ? 420 : 560, alignment: .leading)
        .background(
            RoundedRectangle(cornerRadius: 16, style: .continuous)
                .fill(Color(white: 0.11).opacity(0.92))
        )
        .overlay(
            RoundedRectangle(cornerRadius: 16, style: .continuous)
                .strokeBorder(Color.white.opacity(0.08))
        )
        .task(id: folge.naechster?.id) {
            guard let naechster = folge.naechster else { bild = nil; return }
            // Ein Standbild aus der Folge selbst, nicht das Poster der Serie.
            bild = await session.client.artworkURL(itemId: naechster.id, kind: "frame")
        }
    }
}
