// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

import Foundation
import Observation

// Am Stück: was nach dem Ende eines Titels kommt.
//
// In einer Liste der nächste Eintrag, bei einer einzelnen Folge die Folge
// danach — sofern „Folgen automatisch weiter" an ist. Die Web-App macht
// es genauso (app.js, Abschnitt „Am Stück"); wer zwischen Browser und
// Fernseher wechselt, soll keinen Unterschied merken.
//
// Die Entscheidung steht als reine Rechnung in `AmStueck`, ohne Netz.
// So lässt sie sich prüfen, und iPhone, Mac und Fernseher entscheiden
// gleich. Das Laden und Abspielen erledigt `Abspielfolge`.

public enum AmStueck {

    /// Der nächste Schritt in einer Liste.
    ///
    /// `laden` heisst: bei dieser Serie ist noch nicht bekannt, welche
    /// Folgen sie hat. Erst laden, dann nochmals fragen — so muss die
    /// Rechnung selbst nie ins Netz.
    public enum Schritt: Equatable, Sendable {
        case spielen(index: Int, item: Item)
        case laden(serieId: String)
        case ende
    }

    /// Der Schalter „Folgen automatisch weiter". Fehlt er, ist er an —
    /// so stellt ihn auch der Server ein. Nur „0" heisst aus.
    public static func weiterAn(_ prefs: [String: String]?) -> Bool {
        prefs?["autoplay"] != "0"
    }

    /// Staffeln nach Nummer, Folgen nach Staffel und Folge — dieselbe
    /// Reihenfolge wie auf dem Server (store.folgenOrdnung) und in allen
    /// anderen Apps: die Specials (Staffel 0) zuletzt, Folgen ohne Nummer
    /// am Ende ihrer Staffel. Bei Gleichstand bleibt es bei der Reihenfolge
    /// des Servers — `sorted` allein verspricht das nicht.
    public static func geordnet(_ titel: [Item]) -> [Item] {
        // 0 oder fehlend zählt hinter jeder Nummer.
        func rang(_ n: Int?) -> Int {
            if let n, n > 0 { return n }
            return Int.max
        }
        return titel.enumerated().sorted { a, b in
            let sa = rang(a.element.seasonNo), sb = rang(b.element.seasonNo)
            if sa != sb { return sa < sb }
            let ea = rang(a.element.episodeNo), eb = rang(b.element.episodeNo)
            if ea != eb { return ea < eb }
            return a.offset < b.offset
        }.map { $0.element }
    }

    /// Die erste Folge, die noch nicht fertig geschaut ist. Ist alles
    /// gesehen, die allererste — eine Liste, die man nochmals schaut,
    /// beginnt die Serie von vorn, wie die Filme darin auch.
    ///
    /// Specials zählen dabei nur, wenn die Serie sonst keine Folgen hat —
    /// wie „Als Nächstes" auf dem Server: Ein Making-of soll sich nicht
    /// zwischen zwei Folgen schieben.
    public static func ersteOffene(in folgen: [Item]) -> Item? {
        let alle = geordnet(folgen)
        let regulaer = alle.filter(istRegulaer)
        let reihe = regulaer.isEmpty ? alle : regulaer
        return reihe.first { !$0.isSeen } ?? reihe.first
    }

    /// Die Folge nach dieser, in der Reihenfolge von Staffel und Folge.
    /// Von einer regulären Staffel geht es nicht von selbst in die Specials.
    public static func folgeDanach(_ id: String, in folgen: [Item]) -> Item? {
        let reihe = geordnet(folgen)
        guard let i = reihe.firstIndex(where: { $0.id == id }), i + 1 < reihe.count
        else { return nil }
        if istRegulaer(reihe[i]) && !istRegulaer(reihe[i + 1]) { return nil }
        return reihe[i + 1]
    }

    /// Eine Folge aus einer regulären Staffel, kein Special.
    static func istRegulaer(_ folge: Item) -> Bool { (folge.seasonNo ?? 0) > 0 }

    /// Was nach dem Eintrag `index` als Nächstes läuft. nil heisst: von
    /// vorn, also der erste Eintrag selbst.
    ///
    /// Filme und Folgen spielen sich selbst. Eine Serie spielt ihre erste
    /// offene Folge — eine einzige, dann geht es zum nächsten Eintrag.
    /// Sonst bliebe die Liste an der ersten Serie hängen, bis sie zu Ende
    /// geschaut ist. Eine Serie ohne Folgen und alles andere (Staffeln)
    /// wird übersprungen.
    public static func naechster(nach index: Int?, in eintraege: [Item],
                                 folgen: [String: [Item]]) -> Schritt {
        var i = max(0, (index ?? -1) + 1)
        while i < eintraege.count {
            let eintrag = eintraege[i]
            switch eintrag.kind {
            case "movie", "episode":
                return .spielen(index: i, item: eintrag)
            case "series":
                guard let bekannt = folgen[eintrag.id] else {
                    return .laden(serieId: eintrag.id)
                }
                if let folge = ersteOffene(in: bekannt) {
                    return .spielen(index: i, item: folge)
                }
            default:
                break
            }
            i += 1
        }
        return .ende
    }

    /// Was in „Als Nächstes" steht. Bei einer Folge aus einer Liste der
    /// Name der Serie, sonst der Name der Folge.
    public static func anzeige(_ ziel: Item, eintrag: Item?) -> String {
        guard ziel.kind == "episode", let s = ziel.seasonNo, let e = ziel.episodeNo else {
            return ziel.title
        }
        if let eintrag, eintrag.kind == "series" {
            return String(localized: "\(eintrag.title) · S\(s) F\(e)", bundle: .lumenKit, locale: .lumen)
        }
        return String(localized: "S\(s) F\(e) · \(ziel.title)", bundle: .lumenKit, locale: .lumen)
    }
}

// MARK: - Die laufende Folge

/// Spielt einen Titel oder eine ganze Liste und kündigt an, was danach
/// kommt.
///
/// Die Ansicht zeigt nur an: das Bild aus `playback`, und solange
/// `naechster` gesetzt ist, die Ankündigung mit `sekunden`. Ist `fertig`
/// wahr, gibt es nichts mehr zu spielen, und die Ansicht schliesst sich.
@MainActor
@Observable
public final class Abspielfolge {

    public let playback: Playback

    /// Was gerade läuft — bei einer Serie die Folge, nicht die Serie.
    public private(set) var aktuell: Item?
    /// Was nach dem Countdown startet. nil, solange nichts angekündigt ist.
    public private(set) var naechster: Item?
    public private(set) var naechsterTitel = ""
    public private(set) var sekunden = 0
    /// Nichts mehr zu spielen, oder abgebrochen.
    public private(set) var fertig = false
    /// Warum gar nichts startet — etwa eine Liste nur mit leeren Serien.
    public private(set) var hinweis: String?
    /// Die Ankündigung steht schon, während der Abspann noch läuft. Dann
    /// zählt die Zeit des Films, nicht die Uhr: wer pausiert, bekommt
    /// mehr Zeit; wer springt, weniger.
    public private(set) var imAbspann = false

    /// Wie lange „Als Nächstes" stehen bleibt. Lang genug, um abzubrechen,
    /// kurz genug, dass niemand zur Fernbedienung greifen muss.
    public static let wartezeit = 5
    /// Im Abspann steht die Ankündigung mindestens so lange — auch wenn
    /// das Kapitel kürzer ist.
    public static let mindestensImAbspann = 10

    private struct Ziel {
        let index: Int
        let item: Item
        let titel: String
    }

    private let client: APIClient
    private let eintraege: [Item]
    private let istListe: Bool
    private let weiter: Bool
    private let start: Int
    private var index: Int?
    @ObservationIgnored private var vorgemerkt: Ziel?
    @ObservationIgnored private var folgen: [String: [Item]] = [:]
    @ObservationIgnored private var warten: Task<Void, Never>?
    /// „Abbrechen" im Abspann: der Titel läuft zu Ende, danach ist Schluss.
    @ObservationIgnored private var abgelehnt = false
    /// „Von vorn": nur der erste Titel beginnt bei 0 statt am gemerkten
    /// Stand. Alles danach läuft wie gewohnt.
    @ObservationIgnored private var ersterVonVorn: Bool
    /// Die gewählte Fassung des ersten Titels — „In welcher
    /// Qualität?". Gilt nur für ihn; alles danach nimmt den Standard.
    @ObservationIgnored private var ersteDatei: String?
    /// Eine heruntergeladene Datei statt des Servers.
    @ObservationIgnored private let offlineStart: OfflineStart?
    /// Nach `stop` startet nichts mehr — auch kein Titel, dessen Laden
    /// schon unterwegs war.
    @ObservationIgnored private var gestoppt = false

    /// Was es braucht, um ohne Server zu spielen.
    public struct OfflineStart: Sendable {
        public let datei: URL
        public let ab: Double
        public let dauer: Double
        public let melder: Playback.Offline
        public init(datei: URL, ab: Double, dauer: Double, melder: Playback.Offline) {
            self.datei = datei
            self.ab = ab
            self.dauer = dauer
            self.melder = melder
        }
    }

    /// Ein einzelner Titel. Ist er eine Folge und `weiter` an, kommt nach
    /// dem Ende die nächste. Eine Serie beginnt bei ihrer ersten offenen
    /// Folge. `prefs` sind die Einstellungen des Profils — Sprachen,
    /// Vorspann überspringen; die Wiedergabe richtet sich danach. Mit
    /// `vonVorn` beginnt der Titel am Anfang, nicht am gemerkten Stand.
    public init(client: APIClient, item: Item, weiter: Bool, prefs: [String: String] = [:],
                vonVorn: Bool = false, datei: String? = nil) {
        self.client = client
        self.playback = Playback(client: client)
        self.playback.prefs = prefs
        self.eintraege = [item]
        self.istListe = false
        self.weiter = weiter
        self.start = 0
        self.ersterVonVorn = vonVorn
        self.ersteDatei = datei
        self.offlineStart = nil
    }

    /// Ein heruntergeladener Titel, ohne Server. Danach kommt nichts —
    /// welche Folge folgt, wüsste nur der Server.
    public init(client: APIClient, item: Item, offline: OfflineStart, prefs: [String: String] = [:],
                vonVorn: Bool = false) {
        self.client = client
        self.playback = Playback(client: client)
        self.playback.prefs = prefs
        self.eintraege = [item]
        self.istListe = false
        self.weiter = false
        self.start = 0
        self.ersterVonVorn = vonVorn
        self.ersteDatei = nil
        self.offlineStart = offline
    }

    /// Eine ganze Liste, ab dem Eintrag `ab`. Hier gilt der Schalter für
    /// Folgen nicht: wer eine Liste abspielt, hat „am Stück" ausdrücklich
    /// gewählt.
    public init(client: APIClient, liste: [Item], ab: Int = 0, prefs: [String: String] = [:]) {
        self.client = client
        self.playback = Playback(client: client)
        self.playback.prefs = prefs
        self.eintraege = liste
        self.istListe = true
        self.weiter = true
        self.start = max(0, ab)
        self.ersterVonVorn = false
        self.ersteDatei = nil
        self.offlineStart = nil
    }

    /// Nur in einer Liste gibt es etwas, zu dem man springen kann, wenn
    /// ein Titel sich nicht abspielen lässt.
    public var kannUeberspringen: Bool { istListe }

    public func beginne() async {
        guard !gestoppt else { return }
        playback.onEnd = { [weak self] in
            Task { await self?.amEnde() }
        }
        playback.onCredits = { [weak self] in
            Task { await self?.imAbspannAnkuendigen() }
        }
        if let offlineStart, let item = eintraege.first {
            await spieleOffline(item, offlineStart)
            return
        }
        guard let erstes = await suche(nach: start - 1) else {
            hinweis = istListe
                ? String(localized: "In dieser Liste ist nichts, was sich abspielen lässt.", bundle: .lumenKit, locale: .lumen)
                : String(localized: "Zu diesem Titel gibt es keine Folge.", bundle: .lumenKit, locale: .lumen)
            return
        }
        await spiele(erstes)
    }

    /// Den angekündigten Titel sofort starten.
    public func jetzt() async {
        warten?.cancel()
        warten = nil
        guard let ziel = vorgemerkt else { return }
        vorgemerkt = nil
        naechster = nil
        naechsterTitel = ""
        imAbspann = false
        await spiele(ziel)
    }

    /// Nicht weiterschauen. Läuft der Abspann noch, läuft er zu Ende und
    /// danach schliesst sich die Ansicht; sonst schliesst sie sich gleich.
    public func abbrechen() {
        warten?.cancel()
        warten = nil
        vorgemerkt = nil
        naechster = nil
        naechsterTitel = ""
        if imAbspann, playback.laeuft {
            abgelehnt = true
            imAbspann = false
        } else {
            imAbspann = false
            fertig = true
        }
    }

    /// Lässt sich ein Eintrag nicht abspielen, gleich zum nächsten —
    /// ohne Countdown, denn es lief ja nichts.
    public func ueberspringen() async {
        guard istListe, let ziel = await suche(nach: index) else {
            fertig = true
            return
        }
        await spiele(ziel)
    }

    /// „Nächste Folge" im Player: die Folge nach der laufenden sofort
    /// starten — in derselben Staffel, sonst die erste einer späteren.
    /// false, wenn das die letzte war oder gar keine Folge läuft.
    @discardableResult
    public func naechsteFolge() async -> Bool {
        guard let jetzt = aktuell, jetzt.kind == "episode",
              let danach = await folgeNach(jetzt) else { return false }
        await springe(zu: danach)
        return true
    }

    /// Eine bestimmte Folge sofort starten — aus dem Folgen-Feld. Was
    /// angekündigt war, fällt weg; in einer Liste bleibt es beim selben
    /// Eintrag, danach geht es dort weiter.
    public func springe(zu ziel: Item) async {
        warten?.cancel()
        warten = nil
        vorgemerkt = nil
        naechster = nil
        naechsterTitel = ""
        imAbspann = false
        await spiele(Ziel(index: index ?? 0, item: ziel,
                          titel: AmStueck.anzeige(ziel, eintrag: nil)))
    }

    public func stop() async {
        gestoppt = true
        warten?.cancel()
        warten = nil
        vorgemerkt = nil
        naechster = nil
        imAbspann = false
        abgelehnt = false
        playback.onEnd = nil
        playback.onCredits = nil
        await playback.stop()
    }

    // MARK: - Ablauf

    private func spiele(_ ziel: Ziel) async {
        // Geschlossen, während der nächste Titel noch gesucht wurde: dann
        // bleibt es still.
        guard !gestoppt, !Task.isCancelled else { return }
        index = ziel.index
        aktuell = ziel.item
        abgelehnt = false
        imAbspann = false
        let vonVorn = ersterVonVorn
        ersterVonVorn = false
        // Die gewählte Fassung gehört zum ersten Titel — nur wenn er es ist.
        let datei = ersteDatei
        ersteDatei = nil
        await playback.start(item: ziel.item, from: vonVorn ? 0 : nil,
                             datei: ziel.item.id == eintraege.first?.id ? datei : nil)
    }

    private func spieleOffline(_ item: Item, _ o: OfflineStart) async {
        guard !gestoppt, !Task.isCancelled else { return }
        index = 0
        aktuell = item
        let vonVorn = ersterVonVorn
        ersterVonVorn = false
        await playback.startOffline(item: item, datei: o.datei, ab: vonVorn ? 0 : o.ab,
                                    dauer: o.dauer, offline: o.melder)
    }

    /// Der Titel ist zu Ende und bereits als gesehen markiert — das hat
    /// `Playback` erledigt, bevor es hierher meldet.
    private func amEnde() async {
        guard aktuell != nil, !fertig, !gestoppt else { return }
        // Im Abspann abgelehnt: jetzt ist Schluss.
        if abgelehnt {
            abgelehnt = false
            fertig = true
            return
        }
        // Schon im Abspann angekündigt: ohne weiteres Warten los.
        if vorgemerkt != nil {
            await jetzt()
            return
        }
        guard let ziel = await naechstesZiel() else {
            fertig = true
            return
        }
        kuendigeAn(ziel)
    }

    /// Was nach dem laufenden Titel käme — in einer Liste der nächste
    /// Eintrag, bei einer Folge die danach (wenn „weiter" an ist).
    private func naechstesZiel() async -> Ziel? {
        guard let ende = aktuell else { return nil }
        if istListe {
            return await suche(nach: index)
        }
        if weiter, ende.kind == "episode", let danach = await folgeNach(ende) {
            return Ziel(index: 0, item: danach, titel: AmStueck.anzeige(danach, eintrag: nil))
        }
        return nil
    }

    /// Der Abspann beginnt: „Als Nächstes" steht schon da und zählt bis
    /// zum Ende herunter, mindestens zehn Sekunden. Am Ende startet der
    /// nächste Titel von selbst — oder, nach „Abbrechen", gar nichts.
    private func imAbspannAnkuendigen() async {
        guard !fertig, !abgelehnt, vorgemerkt == nil, playback.laeuft else { return }
        guard let ziel = await naechstesZiel() else { return }
        // Zwischenzeitlich zu Ende oder gewechselt: dann regelt es `amEnde`.
        guard playback.laeuft, aktuell?.id == playback.item?.id else { return }
        warten?.cancel()
        vorgemerkt = ziel
        naechster = ziel.item
        naechsterTitel = ziel.titel
        imAbspann = true
        sekunden = max(Abspielfolge.mindestensImAbspann, Int(playback.verbleibend.rounded()))
        warten = Task { [weak self] in
            while !Task.isCancelled {
                try? await Task.sleep(for: .seconds(1))
                if Task.isCancelled { return }
                guard let self, self.playback.laeuft else { return }
                self.sekunden = max(0, Int(self.playback.verbleibend.rounded()))
            }
        }
    }

    private func kuendigeAn(_ ziel: Ziel) {
        warten?.cancel()
        vorgemerkt = ziel
        naechster = ziel.item
        naechsterTitel = ziel.titel
        imAbspann = false
        sekunden = Abspielfolge.wartezeit
        warten = Task { [weak self] in
            for _ in 0..<Abspielfolge.wartezeit {
                try? await Task.sleep(for: .seconds(1))
                if Task.isCancelled { return }
                self?.sekunden -= 1
            }
            if Task.isCancelled { return }
            // Erst austragen, dann starten: `jetzt` bricht `warten` ab, und
            // ein abgebrochener Task brächte auch das Laden des nächsten
            // Titels zum Scheitern.
            self?.warten = nil
            await self?.jetzt()
        }
    }

    private func suche(nach index: Int?) async -> Ziel? {
        while true {
            switch AmStueck.naechster(nach: index, in: eintraege, folgen: folgen) {
            case .spielen(let i, let item):
                return Ziel(index: i, item: item,
                            titel: AmStueck.anzeige(item, eintrag: eintraege[i]))
            case .laden(let serieId):
                // Auch ein Fehlschlag trägt etwas ein (eine leere Liste),
                // sonst fragte die Schleife ewig nach derselben Serie.
                folgen[serieId] = await ladeFolgen(serie: serieId)
            case .ende:
                return nil
            }
        }
    }

    /// Die Folgen einer Serie, Staffel für Staffel — aber nur bis zur
    /// ersten Staffel mit einer offenen Folge. Weiter muss niemand laden.
    private func ladeFolgen(serie id: String) async -> [Item] {
        guard let detail = try? await client.item(id) else { return [] }
        var geladen: [Item] = []
        for staffel in AmStueck.geordnet(detail.seasons ?? []) {
            let teil = (try? await client.children(of: staffel.id)) ?? []
            geladen += teil
            if teil.contains(where: { !$0.isSeen }) { break }
        }
        return geladen
    }

    /// Die Folge nach dieser: zuerst in derselben Staffel, sonst die erste
    /// Folge einer späteren Staffel.
    private func folgeNach(_ folge: Item) async -> Item? {
        guard let staffelId = folge.parentId else { return nil }
        let hier = (try? await client.children(of: staffelId)) ?? []
        if let danach = AmStueck.folgeDanach(folge.id, in: hier) { return danach }

        guard let staffel = try? await client.item(staffelId),
              let serieId = staffel.item.parentId,
              let serie = try? await client.item(serieId)
        else { return nil }
        let staffeln = AmStueck.geordnet(serie.seasons ?? [])
        guard let j = staffeln.firstIndex(where: { $0.id == staffelId }) else { return nil }
        for spaeter in staffeln[(j + 1)...] {
            let teil = AmStueck.geordnet((try? await client.children(of: spaeter.id)) ?? [])
            if let erste = teil.first { return erste }
        }
        return nil
    }
}

// MARK: - Folgen im Player

/// Das Folgen-Feld im Player: Staffeln als Auswahl, darunter die Folgen
/// der gewählten mit Bild, Haken und Balken. Geladen beim ersten Öffnen,
/// wie im Browser (app.js: folgenPanel). Haken und Balken sind der eigene
/// Stand — der Server schickt sie nur an dieses Profil.
@MainActor
@Observable
public final class Folgenwahl {
    public private(set) var staffeln: [Item] = []
    /// Die gezeigte Staffel.
    public private(set) var staffel: String?
    public private(set) var folgen: [Item] = []
    public private(set) var laedt = false
    /// Die Folgen je Staffel, soweit geladen — der Fernseher zeigt alle
    /// Staffeln auf einmal als Menüs (`allesLaden`).
    public private(set) var jeStaffel: [String: [Item]] = [:]
    /// Zu welcher Serie die Staffeln gehören.
    @ObservationIgnored private var serieGeladen: String?

    private let client: APIClient

    public init(client: APIClient) { self.client = client }

    /// Staffeln und die Folgen der Staffel, in der `folge` liegt. Die
    /// Serie kommt aus der Wiedergabe (`Playback.seriesId`); fehlt sie,
    /// geht es über die Staffel nach oben.
    public func laden(fuer folge: Item, serieId: String?) async {
        guard folge.kind == "episode", let staffelId = folge.parentId else { return }
        laedt = true
        defer { laedt = false }
        var serie = serieId
        if serie == nil {
            serie = (try? await client.item(staffelId))?.item.parentId
        }
        if let serie, serie != serieGeladen {
            let detail = try? await client.item(serie)
            staffeln = AmStueck.geordnet(detail?.seasons ?? [])
            serieGeladen = serie
        }
        await zeige(staffelId)
    }

    /// Wie `laden`, und dazu die Folgen aller übrigen Staffeln. Die
    /// Staffel der laufenden Folge wird jedes Mal neu geholt, damit Haken
    /// und Balken stimmen; die anderen nur einmal.
    public func allesLaden(fuer folge: Item, serieId: String?) async {
        await laden(fuer: folge, serieId: serieId)
        for st in staffeln where jeStaffel[st.id] == nil {
            jeStaffel[st.id] = AmStueck.geordnet((try? await client.children(of: st.id)) ?? [])
        }
    }

    /// Eine andere Staffel zeigen.
    public func waehleStaffel(_ id: String) async {
        guard id != staffel else { return }
        laedt = true
        defer { laedt = false }
        await zeige(id)
    }

    private func zeige(_ id: String) async {
        staffel = id
        let neu = AmStueck.geordnet((try? await client.children(of: id)) ?? [])
        jeStaffel[id] = neu
        // Zwischenzeitlich eine andere Staffel gewählt: deren Folgen gelten.
        if staffel == id { folgen = neu }
    }

    /// „Staffel 1" — so wie der Server sie nennt; Staffel 0 heisst Specials.
    public nonisolated static func name(_ staffel: Item) -> String {
        (staffel.seasonNo ?? 0) == 0 ? String(localized: "Specials", bundle: .lumenKit, locale: .lumen) : staffel.title
    }
}
