// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

import Foundation
import Observation
#if canImport(AVFoundation)
import AVFoundation
#endif

/// Die Wiedergabe.
///
/// Auf Apple-Geräten ist das angenehm kurz: AVPlayer versteht HLS von Haus
/// aus, und weil der Server die vollständige Abspielliste vorab ausliefert,
/// funktioniert Springen damit sofort — auch mitten in einen Film, der
/// gerade erst umgewandelt wird. Die Web-App muss sich dafür mehr Mühe
/// geben; hier genügt `seek`.
///
/// Dazu kommen Tonspur und Untertitel, der Vorspann und der Abspann:
/// - Die Tonspur steckt im Strom. Eine andere heisst: denselben Titel an
///   derselben Stelle neu öffnen, mit `?spur=`.
/// - Untertitel holt die App als WebVTT und legt sie selbst über das Bild
///   (`untertitelText`). Nur wo AVPlayer sie von sich aus anbietet — HLS
///   auf dem Fernseher — übernimmt er das (`nativeUntertitel`).
/// - Liegt die Stelle im Vorspann, ist `imVorspann` wahr; wer das in den
///   Einstellungen so will, springt von selbst. Beginnt der Abspann, meldet
///   `onCredits` — `Abspielfolge` kündigt dann an, was kommt.
@MainActor
@Observable
public final class Playback {

    public private(set) var item: Item?
    public private(set) var file: MediaFile?
    public private(set) var plan: PlaybackPlan?
    public private(set) var isPreparing = false
    public var error: String?

    /// Länge in Sekunden. Massgeblich ist die gemessene Datei — gespielt
    /// wird die Datei, nicht der Eintrag in den Metadaten. Bei einem Film
    /// in Teilen die Länge des laufenden Teils.
    public private(set) var duration: Double = 0

    // MARK: Fassung und Teile

    /// Ein Film in Teilen („… a.avi", „… b.avi"): alle Teile, und welcher
    /// gerade läuft. Bei einer einzigen Datei ist `teile` eins lang.
    public private(set) var teile: FilmTeile?
    public private(set) var teil = 0
    /// Die Dauer der Teile vor dem laufenden — der Stand geht als
    /// Gesamtstelle an den Server (vorher + Stelle im Teil), damit „Weiter
    /// bei" auf allen Geräten dasselbe meint.
    private var vorher: Double = 0
    /// Die gewählte Fassung — sie gilt auch für die weiteren Teile.
    private var gewaehlteDatei: String?

    /// „Teil 2 von 3" — bei einer einzelnen Datei nil.
    public var teilBeschriftung: String? { teile?.beschriftung(teil) }

    /// Ob dies der letzte (oder einzige) Teil ist. Erst danach ist der Film
    /// gesehen, und erst dort beginnt der Abspann.
    public var letzterTeil: Bool { teile?.istLetzter(teil) ?? true }

    /// Die Dauer, die der Server bekommt: die aller Teile, sonst die der Datei.
    private var gesamtDauer: Double {
        if let teile, teile.mehrere, teile.gesamtSekunden > 0 { return teile.gesamtSekunden }
        return duration
    }

    // MARK: Spuren

    public private(set) var tonspuren: [Track] = []
    public private(set) var textspuren: [Track] = []
    /// Die gewählte Tonspur. nil heisst: die erste, so wie die Datei sie
    /// mitbringt — dann kann sie auch direkt laufen, ohne HLS.
    public private(set) var tonspur: Int?
    /// Die gewählte Untertitelspur. nil heisst aus.
    public private(set) var untertitelSpur: Int?
    /// Was gerade an Untertitel im Bild steht — für die eigene Anzeige.
    public private(set) var untertitelText: String?
    /// Läuft der Strom über HLS? Dann hat AVPlayer die Untertitel des
    /// Servers als Spurgruppe und kann sie selbst anbieten.
    public private(set) var laeuftUeberHLS = false
    /// Der Fernseher lässt bei HLS AVPlayer die Untertitel zeigen — sein
    /// Menü ist dort das, was alle kennen. Dann zeichnet Lumen keinen Text
    /// darüber, merkt sich aber, was gewählt wurde.
    public var nativeUntertitel = false

    // MARK: Vorspann und Abspann

    public private(set) var intro: Vorspann?
    public private(set) var creditsMs: Int64?
    /// Die Serie, zu der eine Folge gehört — dort wird der Vorspann markiert.
    public private(set) var seriesId: String?
    /// Die Stelle liegt im Vorspann: Zeit für „Vorspann überspringen".
    public private(set) var imVorspann = false
    /// Der gemerkte Beginn beim Markieren („Vorspann beginnt hier").
    public private(set) var introMarke: Double?
    private var introUebersprungen = false
    private var abspannGemeldet = false

    /// Die Einstellungen des Profils — Sprachen, Vorspann überspringen.
    /// Was hier geändert wird, geht zugleich an den Server.
    public var prefs: [String: String] = [:]

    #if canImport(AVFoundation)
    public private(set) var player: AVPlayer?
    @ObservationIgnored private var timeObserver: Any?
    /// Der Player, an dem `timeObserver` hängt — abmelden geht nur dort.
    @ObservationIgnored private var beobachteterPlayer: AVPlayer?
    @ObservationIgnored private var endObserver: NSObjectProtocol?
    @ObservationIgnored private var selectionObserver: NSObjectProtocol?
    @ObservationIgnored private var failObserver: NSObjectProtocol?
    @ObservationIgnored private var logObserver: NSObjectProtocol?
    #endif
    /// Die laufende HLS-Sitzung des Servers, sobald AVPlayer sie geholt hat.
    @ObservationIgnored private var hlsSitzung: HLSSitzung?
    /// Zählt jeden Start und jedes `stop` — siehe `start`.
    @ObservationIgnored private var lauf = 0

    /// Spielt gerade eine heruntergeladene Datei? Dann geht der Stand nicht
    /// an den Server, sondern an `stand` — die Downloads merken ihn sich
    /// und reichen ihn beim nächsten Kontakt nach.
    public struct Offline: Sendable {
        public let stand: @MainActor @Sendable (_ itemId: String, _ position: Double, _ dauer: Double) -> Void
        public init(stand: @escaping @MainActor @Sendable (String, Double, Double) -> Void) {
            self.stand = stand
        }
    }
    @ObservationIgnored private var offline: Offline?
    public var spieltOffline: Bool { offline != nil }

    /// Die Vorschaubilder fürs Spulen, falls der Server welche hat.
    public private(set) var trickplay: Trickplay?
    /// Ob nach einem Abbruch mitten im Film schon einmal mit frischem
    /// Medienschlüssel neu geöffnet wurde. Einmal je Titel reicht — scheitert
    /// es dann noch, liegt es nicht am Schlüssel.
    private var neuVersucht = false

    /// Wird gerufen, wenn ein Titel bis zum Ende gelaufen ist — erst
    /// nachdem er als gesehen gilt. `Abspielfolge` hängt sich hier ein, um
    /// den nächsten Titel anzukündigen. Nicht beobachtet: die Ansicht muss
    /// nicht neu zeichnen, nur weil jemand zuhört.
    @ObservationIgnored public var onEnd: (@MainActor () -> Void)?
    /// Wird einmal gerufen, wenn der Abspann beginnt — laut Kapitel, sonst
    /// in den letzten 40 Sekunden. `Abspielfolge` zeigt dann schon, was
    /// als Nächstes kommt.
    @ObservationIgnored public var onCredits: (@MainActor () -> Void)?

    private let client: APIClient
    private var lastSaved: Double = 0
    private var reportedFinished = false
    @ObservationIgnored private var cues: [Untertitel] = []
    @ObservationIgnored private var cuesFuer: Int?

    public init(client: APIClient) { self.client = client }

    /// Ab wann ein Film als gesehen gilt. Wer den Abspann überspringt, hat
    /// ihn trotzdem gesehen — bei Serien macht die Vorschau am Ende gern
    /// fünf Prozent aus.
    // Ohne `nonisolated`: eine statische Konstante eines Typs, der sich
    // gefahrlos teilen lässt, ist ohnehin von überall lesbar. Das
    // Schlüsselwort brächte hier nichts und ist je nach Fassung des
    // Übersetzers an gespeicherten Eigenschaften heikel.
    public static let finishedAt = 0.92

    /// Ohne Kapitel gilt: der Abspann beginnt 40 Sekunden vor dem Ende.
    public static let abspannOhneKapitel: Double = 40

    // MARK: - Starten

    /// Einen Titel abspielen. `from` ist bei einem Film in Teilen die
    /// Gesamtstelle über alle Teile; `datei` die gewählte Fassung (nil:
    /// die erste, wie der Server ordnet).
    public func start(item: Item, from requested: Double? = nil, datei: String? = nil) async {
        // Eine neue Nummer für diesen Start. Jeder spätere Start und jedes
        // `stop` zählt weiter — so merkt dieser Start nach jedem `await`,
        // ob er noch gemeint ist. Ohne das legte ein Start, der unterwegs
        // war, als der Player geschlossen wurde, danach trotzdem einen
        // AVPlayer an: Ton aus dem Nichts, und der Server wandelt weiter um.
        lauf &+= 1
        let meiner = lauf
        await aufraeumen()
        guard gilt(meiner) else { return await verwerfen(meiner) }
        gewaehlteDatei = datei
        isPreparing = true
        error = nil
        reportedFinished = false
        neuVersucht = false
        offline = nil
        self.item = item

        do {
            let detail = try await client.item(item.id)
            guard gilt(meiner) else { return await verwerfen(meiner) }
            // Die gewählte Fassung, sonst die erste — und dazu ihre Teile.
            guard let erste = Fassungen.datei(detail.files, gewaehlt: datei) else {
                error = String(localized: "Zu diesem Titel gibt es keine Datei.", bundle: .lumenKit, locale: .lumen)
                isPreparing = false
                return
            }
            let alle = Fassungen.teile(detail.files, erste: erste)
            // Aus der Gesamtstelle der Teil und die Stelle darin.
            let stelle = alle.stelle(requested ?? item.resumeSeconds)
            let file = alle.dateien[stelle.teil]
            self.file = file
            teile = alle
            teil = stelle.teil
            vorher = stelle.vorher

            let planResponse = try await client.plan(fileId: file.id)
            guard gilt(meiner) else { return await verwerfen(meiner) }
            self.plan = planResponse.plan

            let length = file.durationSeconds > 0 ? file.durationSeconds : item.runtimeSeconds
            duration = length

            // Spuren und Vorbelegung nach den gemerkten Sprachen.
            tonspuren = file.tonspuren
            textspuren = file.textspuren
            let vor = Spuren.vorbelegung(ton: tonspuren, text: textspuren, prefs: prefs)
            tonspur = vor.ton == tonspuren.first?.index ? nil : vor.ton
            untertitelSpur = vor.untertitel

            // Der Vorspann liegt im ersten Teil, der Abspann im letzten —
            // in den anderen wären die Marken falsch.
            intro = stelle.teil == 0 ? detail.intro : nil
            creditsMs = alle.istLetzter(stelle.teil) ? detail.creditsMs : nil
            seriesId = detail.seriesId

            var start = stelle.imTeil
            // Ein Stand hinter dem Ende passt nicht mehr zur Datei — etwa
            // weil sie durch eine kürzere Fassung ersetzt wurde. Dann von
            // vorn, statt im Leeren zu landen.
            if length > 0, start > length - 5 { start = 0 }

            try await schluesselFrisch()
            guard gilt(meiner) else { return await verwerfen(meiner) }
            let url = await client.streamURL(for: file, plan: planResponse.plan, spur: tonspur)
            guard gilt(meiner) else { return await verwerfen(meiner) }
            laeuftUeberHLS = APIClient.istHLS(url)
            try await play(url: url, from: start, lauf: meiner)
            guard gilt(meiner) else { return await verwerfen(meiner) }
            if let spur = untertitelSpur { ladeUntertitel(spur) }
            trickplayLaden(fileId: file.id, lauf: meiner)
        } catch {
            guard gilt(meiner) else { return await verwerfen(meiner) }
            self.error = Session.text(error)
        }
        isPreparing = false
    }

    /// Eine heruntergeladene Datei abspielen — ohne Server. Der Stand geht
    /// an `offline.stand`; die Downloads reichen ihn nach, sobald der Server
    /// wieder erreichbar ist. Ton- und Untertitelwahl gibt es hier nicht:
    /// die Datei spielt, wie sie ist.
    public func startOffline(item: Item, datei: URL, ab: Double, dauer: Double, offline: Offline) async {
        lauf &+= 1
        let meiner = lauf
        await aufraeumen()
        guard gilt(meiner) else { return await verwerfen(meiner) }
        isPreparing = true
        error = nil
        reportedFinished = false
        // Kein Neuversuch mit frischem Schlüssel: es gibt keinen Server.
        neuVersucht = true
        gewaehlteDatei = nil
        self.offline = offline
        self.item = item
        duration = dauer > 0 ? dauer : item.runtimeSeconds
        laeuftUeberHLS = false
        var start = ab
        if duration > 0, start > duration - 5 { start = 0 }
        do {
            try await play(url: datei, from: start, lauf: meiner)
        } catch {
            guard gilt(meiner) else { return await verwerfen(meiner) }
            self.error = Session.text(error)
        }
        isPreparing = false
    }

    /// Die Vorschaubilder fürs Spulen nebenbei holen. Fehlen sie, oder
    /// kennt der Server den Weg noch nicht, bleibt es still beim Balken.
    private func trickplayLaden(fileId: String, lauf meiner: Int) {
        Task { [weak self, client] in
            let info = try? await client.trickplay(fileId: fileId)
            guard let self, self.lauf == meiner, self.file?.id == fileId else { return }
            self.trickplay = info ?? nil
        }
    }

    /// Gilt dieser Start noch? Nein, wenn inzwischen ein anderer begonnen
    /// hat, `stop` kam oder die Aufgabe abgebrochen wurde (die Ansicht ist
    /// weg).
    private func gilt(_ meiner: Int) -> Bool { meiner == lauf && !Task.isCancelled }

    /// Ein Start, der nicht mehr gilt, hinterlässt nichts. Hat ihn nur der
    /// Abbruch der Aufgabe erwischt — kein neuer Start, kein `stop` —, wird
    /// hier aufgeräumt; sonst hat das der Nachfolger schon getan.
    private func verwerfen(_ meiner: Int) async {
        guard meiner == lauf else { return }
        await stop()
    }

    #if canImport(AVFoundation)
    private func play(url: URL, from start: Double, lauf meiner: Int) async throws {
        // Was vorher lief, ist ganz weg, bevor der neue Player kommt —
        // samt Beobachtern. Sonst hinge ein Zeitbeobachter am alten Player
        // und meldete dessen Stelle weiter.
        wiedergabeAbbauen()

        let asset = AVURLAsset(url: url)
        let playerItem = AVPlayerItem(asset: asset)
        let player = AVPlayer(playerItem: playerItem)
        player.automaticallyWaitsToMinimizeStalling = true

        if nativeUntertitel, laeuftUeberHLS {
            // AVPlayer wählt die Untertitel: in der gemerkten Sprache, sonst
            // wie das System es will (erzwungene laufen von selbst mit).
            player.appliesMediaSelectionCriteriaAutomatically = true
            if let sprache = prefs["untertitel"], !sprache.isEmpty, sprache != "aus" {
                player.setMediaSelectionCriteria(
                    AVPlayerMediaSelectionCriteria(preferredLanguages: [sprache],
                                                   preferredMediaCharacteristics: nil),
                    forMediaCharacteristic: .legible)
            }
        } else {
            // Lumen zeigt die Untertitel selbst — AVPlayer soll nicht
            // zugleich welche einblenden.
            player.appliesMediaSelectionCriteriaAutomatically = false
        }
        self.player = player

        if start > 1 {
            await player.seek(to: CMTime(seconds: start, preferredTimescale: 600),
                              toleranceBefore: .zero, toleranceAfter: .positiveInfinity)
        }
        // Während des Springens geschlossen oder abgelöst: dieser Player
        // darf nie anfangen zu spielen.
        guard gilt(meiner) else {
            player.pause()
            player.replaceCurrentItem(with: nil)
            if self.player === player { self.player = nil }
            return
        }

        if nativeUntertitel, laeuftUeberHLS {
            selectionObserver = NotificationCenter.default.addObserver(
                forName: AVPlayerItem.mediaSelectionDidChangeNotification,
                object: playerItem, queue: .main
            ) { [weak self] _ in
                Task { @MainActor in await self?.nativeAuswahlMerken() }
            }
        }

        // Viermal die Sekunde hereinschauen — für Untertitel und den
        // Vorspann-Knopf —, aber höchstens alle fünf Sekunden speichern:
        // der Server soll nicht dauernd schreiben.
        timeObserver = player.addPeriodicTimeObserver(
            forInterval: CMTime(seconds: 0.25, preferredTimescale: 600), queue: .main
        ) { [weak self] time in
            let seconds = time.seconds
            Task { @MainActor in self?.tick(at: seconds) }
        }
        beobachteterPlayer = player

        endObserver = NotificationCenter.default.addObserver(
            forName: .AVPlayerItemDidPlayToEndTime, object: playerItem, queue: .main
        ) { [weak self] _ in
            Task { @MainActor in await self?.reachedEnd() }
        }

        // Bricht der Strom mitten im Film ab, ist der häufigste Grund ein
        // Neustart des Servers: Danach gilt kein Medienschlüssel mehr, und
        // jedes weitere Segment kommt mit 401 zurück. Einmal mit frischem
        // Schlüssel an derselben Stelle neu öffnen.
        failObserver = NotificationCenter.default.addObserver(
            forName: .AVPlayerItemFailedToPlayToEndTime, object: playerItem, queue: .main
        ) { [weak self] _ in
            Task { @MainActor in await self?.nachAbbruch() }
        }

        // Die Kennung der HLS-Sitzung steht nur in der Abspielliste, die
        // AVPlayer selbst holt. Das Zugriffsprotokoll verrät sie — damit
        // lässt sich die Umwandlung beim Schliessen sofort beenden, statt
        // dass der Server zwei Minuten lang weiterrechnet.
        if laeuftUeberHLS, let fileId = file?.id {
            logObserver = NotificationCenter.default.addObserver(
                forName: AVPlayerItem.newAccessLogEntryNotification,
                object: playerItem, queue: .main
            ) { [weak self, weak playerItem] _ in
                let adressen = playerItem?.accessLog()?.events.compactMap(\.uri) ?? []
                Task { @MainActor in self?.hlsSitzungMerken(adressen, fileId: fileId) }
            }
        }

        Self.tonFuerFilmeEinschalten()
        player.play()
    }

    /// Die Audio-Sitzung auf „Film" stellen, bevor das Bild läuft.
    ///
    /// Ohne das gilt die Vorgabe des Systems (`.soloAmbient`): Mit dem
    /// Stummschalter am iPhone bliebe der Film stumm, beim Sperren hielte
    /// er an, und Bild-in-Bild ginge gar nicht — trotz `UIBackgroundModes:
    /// audio` in der Info.plist. Auf dem Apple TV schadet es nicht und gibt
    /// den Ton richtig an AirPlay und HomePod weiter. Der Mac kennt keine
    /// Audio-Sitzung.
    private static func tonFuerFilmeEinschalten() {
        #if os(iOS) || os(tvOS)
        let sitzung = AVAudioSession.sharedInstance()
        do {
            try sitzung.setCategory(.playback, mode: .moviePlayback)
            try sitzung.setActive(true)
        } catch {
            // Geht es nicht, spielt der Film trotzdem — nur eben mit dem
            // Verhalten von vorher. Kein Grund, die Wiedergabe abzubrechen.
        }
        #endif
    }

    /// Nach dem Schliessen: die Sitzung wieder freigeben, damit etwa die
    /// Musik weiterspielt, die vor dem Film lief.
    private static func tonFuerFilmeAusschalten() {
        #if os(iOS) || os(tvOS)
        try? AVAudioSession.sharedInstance().setActive(false, options: .notifyOthersOnDeactivation)
        #endif
    }

    private func hlsSitzungMerken(_ adressen: [String], fileId: String) {
        guard file?.id == fileId else { return }
        for adresse in adressen.reversed() {
            if let kennung = HLSSitzung.kennung(in: adresse, fileId: fileId) {
                hlsSitzung = HLSSitzung(fileId: fileId, kennung: kennung)
                return
            }
        }
    }

    private func tick(at seconds: Double) {
        guard seconds.isFinite, seconds >= 0 else { return }
        if duration > 0, letzterTeil, !reportedFinished,
           (vorher + seconds) / gesamtDauer >= Self.finishedAt {
            reportedFinished = true
            Task { await markFinished() }
        }
        untertitelZeigen(at: seconds)
        vorspannPruefen(at: seconds)
        abspannPruefen(at: seconds)
        // Betrag statt Differenz: nach dem Zurückspulen ist die Differenz
        // negativ, und der Stand bliebe stehen, bis die alte Stelle wieder
        // erreicht ist.
        guard abs(seconds - lastSaved) >= 5 || lastSaved == 0 else { return }
        lastSaved = seconds
        Task { await save(position: seconds) }
    }

    public var currentTime: Double {
        player?.currentTime().seconds ?? 0
    }

    public func seek(to seconds: Double) {
        guard let player else { return }
        let target = max(0, min(seconds, duration > 0 ? duration - 1 : seconds))
        player.seek(to: CMTime(seconds: target, preferredTimescale: 600),
                    toleranceBefore: .zero, toleranceAfter: .zero)
    }

    public func skip(_ delta: Double) { seek(to: currentTime + delta) }

    public func togglePlay() {
        guard let player else { return }
        player.rate == 0 ? player.play() : player.pause()
    }

    public var isPlaying: Bool { (player?.rate ?? 0) > 0 }

    /// Ob überhaupt etwas offen ist — auch angehalten. Nach dem Ende eines
    /// Titels ist das falsch, bis der nächste beginnt.
    public var laeuft: Bool { player != nil }

    /// Schliessen: der Stand geht an den Server, der Player ist sofort
    /// still, und eine laufende Umwandlung wird beendet.
    public func stop() async {
        lauf &+= 1
        isPreparing = false
        await aufraeumen()
        Self.tonFuerFilmeAusschalten()
    }

    /// Den Stand jetzt sichern — die App geht in den Hintergrund, und ob
    /// sie zurückkommt, weiss niemand.
    public func standSichern() async {
        guard let player, item != nil else { return }
        let seconds = player.currentTime().seconds
        guard seconds.isFinite, seconds > 0 else { return }
        lastSaved = seconds
        await save(position: seconds)
    }

    /// Alles abbauen, was zu einer Wiedergabe gehört. Erst still, dann
    /// speichern: der Stand wird vorher festgehalten, damit ein langsamer
    /// Server den Ton nicht weiterlaufen lässt.
    private func aufraeumen() async {
        var stand: (id: String, position: Double, dauer: Double)?
        if let player, let item, duration > 0 {
            let seconds = player.currentTime().seconds
            if seconds.isFinite, seconds > 0 {
                stand = (item.id, vorher + seconds, gesamtDauer)
            }
        }
        let warOffline = offline
        wiedergabeAbbauen()
        item = nil
        file = nil
        plan = nil
        duration = 0
        lastSaved = 0
        teile = nil
        teil = 0
        vorher = 0
        offline = nil
        trickplay = nil
        spurenZuruecksetzen()
        guard let stand else { return }
        if let warOffline {
            warOffline.stand(stand.id, stand.position, stand.dauer)
        } else {
            try? await client.saveProgress(itemId: stand.id, position: stand.position, duration: stand.dauer)
        }
    }

    /// Player, Beobachter und HLS-Sitzung weg — ohne zu warten.
    private func wiedergabeAbbauen() {
        beobachterEntfernen()
        player?.pause()
        player?.replaceCurrentItem(with: nil)
        player = nil
        if let sitzung = hlsSitzung {
            hlsSitzung = nil
            // Losgelöst: auch wenn die Ansicht schon weg ist und ihre
            // Aufgabe abgebrochen wurde, soll der Server Bescheid bekommen.
            let client = self.client
            Task.detached { try? await client.hlsBeenden(fileId: sitzung.fileId, sitzung: sitzung.kennung) }
        }
    }

    private func beobachterEntfernen() {
        if let timeObserver { beobachteterPlayer?.removeTimeObserver(timeObserver) }
        if let endObserver { NotificationCenter.default.removeObserver(endObserver) }
        if let selectionObserver { NotificationCenter.default.removeObserver(selectionObserver) }
        if let failObserver { NotificationCenter.default.removeObserver(failObserver) }
        if let logObserver { NotificationCenter.default.removeObserver(logObserver) }
        timeObserver = nil
        beobachteterPlayer = nil
        endObserver = nil
        selectionObserver = nil
        failObserver = nil
        logObserver = nil
    }

    private func nachAbbruch() async {
        guard !neuVersucht, player != nil else { return }
        neuVersucht = true
        await neuOeffnen()
    }

    /// Denselben Titel an derselben Stelle neu öffnen — nach einem Wechsel
    /// der Tonspur, die im Strom steckt.
    private func neuOeffnen() async {
        guard offline == nil, let file, let plan, let player else { return }
        let meiner = lauf
        let stelle = player.currentTime().seconds
        wiedergabeAbbauen()
        do {
            try await schluesselFrisch()
            guard gilt(meiner) else { return }
            let url = await client.streamURL(for: file, plan: plan, spur: tonspur)
            guard gilt(meiner) else { return }
            laeuftUeberHLS = APIClient.istHLS(url)
            try await play(url: url, from: stelle.isFinite ? stelle : 0, lauf: meiner)
        } catch {
            guard gilt(meiner) else { return }
            self.error = Session.text(error)
        }
    }

    /// Was AVPlayer an Untertiteln gewählt hat, als Sprache merken — damit
    /// das nächste Mal dasselbe läuft, auch im Browser. Erzwungene zählen
    /// als „aus": sie laufen ohnehin mit.
    private func nativeAuswahlMerken() async {
        guard nativeUntertitel, laeuftUeberHLS, let playerItem = player?.currentItem else { return }
        guard let gruppe = try? await playerItem.asset.loadMediaSelectionGroup(for: .legible) else { return }
        var wert = "aus"
        if let option = playerItem.currentMediaSelection.selectedMediaOption(in: gruppe),
           !option.hasMediaCharacteristic(.containsOnlyForcedSubtitles) {
            wert = option.extendedLanguageTag.map { String($0.prefix(2)).lowercased() } ?? "und"
        }
        if prefs["untertitel"] != wert { merken("untertitel", wert) }
    }
    #else
    private func play(url: URL, from start: Double, lauf meiner: Int) async throws {}
    public var currentTime: Double { 0 }
    public func seek(to seconds: Double) {}
    public func skip(_ delta: Double) {}
    public func togglePlay() {}
    public var isPlaying: Bool { false }
    public var laeuft: Bool { false }
    public func stop() async {
        lauf &+= 1
        isPreparing = false
        item = nil; file = nil; plan = nil; teile = nil; teil = 0; vorher = 0
        offline = nil; trickplay = nil
        spurenZuruecksetzen()
    }
    public func standSichern() async {}
    private func aufraeumen() async {
        item = nil; file = nil; plan = nil; teile = nil; teil = 0; vorher = 0
        offline = nil; trickplay = nil
        spurenZuruecksetzen()
    }
    private func neuOeffnen() async {}
    #endif

    private func spurenZuruecksetzen() {
        tonspuren = []
        textspuren = []
        tonspur = nil
        untertitelSpur = nil
        untertitelText = nil
        cues = []
        cuesFuer = nil
        laeuftUeberHLS = false
        intro = nil
        creditsMs = nil
        seriesId = nil
        imVorspann = false
        introMarke = nil
        introUebersprungen = false
        abspannGemeldet = false
    }

    /// Vor jeder Wiedergabe einen frischen Medienschlüssel: Ein Film dauert
    /// Stunden, und die Adresse, die AVPlayer bekommt, behält ihren
    /// Schlüssel bis zum Schluss. Klappt das Holen nicht, aber es gibt noch
    /// einen, wird es mit dem versucht — fehlt einer ganz, hätte die
    /// Wiedergabe keine Chance.
    private func schluesselFrisch() async throws {
        do {
            try await client.medienSchluesselHolen(erzwingen: true)
        } catch {
            if await client.medienSchluessel == nil { throw error }
        }
    }

    // MARK: - Ton und Untertitel

    /// Gibt es überhaupt etwas zu wählen?
    public var hatSpurwahl: Bool { tonspuren.count > 1 || !textspuren.isEmpty }

    /// Die Tonspur, die gerade läuft — die gewählte, sonst die erste.
    public var tonspurAktiv: Int? { tonspur ?? tonspuren.first?.index }

    /// Eine andere Tonspur. Der Ton steckt im Strom, also wird der Titel
    /// an derselben Stelle neu geöffnet; die Sprache wird gemerkt.
    public func waehleTon(_ index: Int) async {
        guard index != tonspurAktiv else { return }
        tonspur = index == tonspuren.first?.index ? nil : index
        if let t = tonspuren.first(where: { $0.index == index }) { merken("tonSprache", t.sprache) }
        await neuOeffnen()
    }

    /// Untertitel ein (Index) oder aus (nil). Gemerkt wird die Sprache,
    /// nicht der Index — der gilt nur für diese eine Datei.
    public func waehleUntertitel(_ index: Int?) {
        untertitelSpur = index
        untertitelText = nil
        if let index, let t = textspuren.first(where: { $0.index == index }) {
            merken("untertitel", t.sprache)
            ladeUntertitel(index)
        } else {
            merken("untertitel", "aus")
        }
    }

    /// Ob Lumen die Untertitel selbst über das Bild legt. Bei HLS auf dem
    /// Fernseher macht das AVPlayer; dann bleibt `untertitelText` leer und
    /// das Menü zeigt keine Untertitel an.
    public var zeigtUntertitelSelbst: Bool { !(nativeUntertitel && laeuftUeberHLS) }

    private func ladeUntertitel(_ index: Int) {
        guard zeigtUntertitelSelbst, let file, cuesFuer != index else { return }
        Task { [weak self] in
            guard let self else { return }
            do {
                let vtt = try await self.client.untertitel(fileId: file.id, spur: index)
                guard self.untertitelSpur == index, self.file?.id == file.id else { return }
                self.cues = Untertitel.lesen(vtt)
                self.cuesFuer = index
            } catch {
                // Lassen sich die Untertitel nicht lesen, bleibt es still
                // aus — ein Fehler, der das Bild überdeckt, wäre schlimmer.
                guard self.untertitelSpur == index else { return }
                self.untertitelSpur = nil
                self.untertitelText = nil
            }
        }
    }

    private func untertitelZeigen(at seconds: Double) {
        guard let spur = untertitelSpur, cuesFuer == spur, zeigtUntertitelSelbst else {
            if untertitelText != nil { untertitelText = nil }
            return
        }
        let neu = Untertitel.text(at: seconds, in: cues)
        if neu != untertitelText { untertitelText = neu }
    }

    /// Eine Einstellung merken — hier und auf dem Server. Scheitert das
    /// Speichern, bleibt die Wahl für diese Wiedergabe trotzdem gültig.
    private func merken(_ key: String, _ wert: String) {
        guard prefs[key] != wert else { return }
        prefs[key] = wert
        Task { _ = try? await client.updatePrefs([key: wert]) }
    }

    // MARK: - Vorspann

    private func vorspannPruefen(at seconds: Double) {
        let drin = intro.map { !introUebersprungen && $0.enthaelt(seconds) } ?? false
        if drin != imVorspann { imVorspann = drin }
        if drin, prefs["skipIntro"] == "1" { vorspannUeberspringen() }
    }

    /// Hinter den Vorspann springen. Danach bleibt der Knopf weg, auch wenn
    /// jemand zurückspult — einmal reicht.
    public func vorspannUeberspringen() {
        guard let intro else { return }
        introUebersprungen = true
        imVorspann = false
        seek(to: intro.bis)
    }

    /// Wer Verwaltung darf, markiert bei einer Folge den Vorspann für die
    /// ganze Serie. Dafür muss bekannt sein, welche Serie das ist.
    public var kannVorspannMarkieren: Bool { item?.kind == "episode" && seriesId != nil }

    /// Erster Schritt: die Stelle merken.
    public func vorspannBeginntHier() { introMarke = currentTime }

    public func vorspannMarkeVerwerfen() { introMarke = nil }

    /// Zweiter Schritt: bis hier, und an den Server. Die Ansicht ruft das
    /// über `Session.verwaltet`, damit die Verwaltungssperre greift.
    public func vorspannEndetHier() async throws {
        guard let seriesId, let marke = introMarke else { return }
        let von = Int64((marke * 1000).rounded())
        let bis = Int64((currentTime * 1000).rounded())
        try await client.adminSetIntro(seriesId: seriesId, vonMs: von, bisMs: bis)
        intro = Vorspann(vonMs: von, bisMs: bis, quelle: "serie")
        introMarke = nil
        introUebersprungen = false
    }

    /// Die Markierung an der Serie entfernen. Ein Kapitel in der Datei
    /// bleibt — das kommt nicht von uns.
    public func vorspannEntfernen() async throws {
        guard let seriesId else { return }
        try await client.adminClearIntro(seriesId: seriesId)
        if intro?.vonSerie == true { intro = nil }
        imVorspann = false
    }

    // MARK: - Abspann

    /// Ab wann „Als Nächstes" dasteht: der Beginn des Abspanns laut
    /// Kapitel, sonst die letzten 40 Sekunden.
    public var abspannAb: Double {
        if let creditsMs, creditsMs > 0 { return Double(creditsMs) / 1000 }
        return max(0, duration - Self.abspannOhneKapitel)
    }

    private func abspannPruefen(at seconds: Double) {
        guard !abspannGemeldet, letzterTeil, duration > 0, seconds >= abspannAb, seconds <= duration - 2 else { return }
        abspannGemeldet = true
        onCredits?()
    }

    /// Wie viel noch läuft, in Sekunden.
    public var verbleibend: Double { max(0, duration - currentTime) }

    // MARK: - Stand sichern

    private func save(position: Double) async {
        guard let item, duration > 0 else { return }
        if let offline {
            offline.stand(item.id, vorher + position, gesamtDauer)
            return
        }
        try? await client.saveProgress(itemId: item.id, position: vorher + position, duration: gesamtDauer)
    }

    private func markFinished() async {
        guard let item else { return }
        if let offline {
            // Gesehen heisst offline: der Stand steht am Ende. Der Server
            // macht daraus beim Nachreichen einen Haken (über 92 %).
            offline.stand(item.id, gesamtDauer, gesamtDauer)
            return
        }
        try? await client.markFinished(item.id)
    }

    private func reachedEnd() async {
        guard let item else { return }
        // Noch ein Teil übrig: gleich weiter, ohne Countdown und ohne
        // „Als Nächstes" — es ist derselbe Film. Gesehen ist er erst nach
        // dem letzten Teil.
        if let teile, !teile.istLetzter(teil) {
            await start(item: item, from: teile.weiterAb(teil), datei: gewaehlteDatei)
            return
        }
        await markFinished()
        await stop()
        onEnd?()
    }

    // MARK: - Anzeige

    /// „1:24:07“ oder „7:12“
    public nonisolated static func clock(_ seconds: Double) -> String {
        guard seconds.isFinite, seconds >= 0 else { return "0:00" }
        let total = Int(seconds)
        let h = total / 3600, m = (total / 60) % 60, s = total % 60
        return h > 0
            ? String(format: "%d:%02d:%02d", h, m, s)
            : String(format: "%d:%02d", m, s)
    }

    public var remainingLabel: String {
        guard duration > 0 else { return "" }
        return "−" + Self.clock(max(0, duration - currentTime))
    }

    /// Wird gerade umgewandelt? Das darf man ruhig sehen — es erklärt, warum
    /// der Server gerade arbeitet.
    public var isTranscoding: Bool { plan?.direct == false }
}
